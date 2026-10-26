package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
)

// All positions come from a controlled 2.1.23 capture, not from our placement
// implementation. This separately checks footprints, chunk clipping, palette,
// integer alpha accumulation, checkerboard phase, and compositing order.
func TestFactorioChartReleaseOracle(t *testing.T) {
	var ref struct {
		FactorioVersion string
		Origin          [2]float64
		Size            int
		Background      [4]uint8
		Entities        []struct {
			Name string
			X, Y float64
		}
		Prototypes map[string]struct {
			Order  string
			Box    [4]float64
			Color  [4]uint8
			Dither bool
		}
	}
	data, err := os.ReadFile("testdata/chart-2.1.23/census.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ref.FactorioVersion, "2.1.23") || len(ref.Entities) < 280 {
		t.Fatal("incomplete chart oracle")
	}
	got := image.NewRGBA(image.Rect(0, 0, ref.Size, ref.Size))
	for y := 0; y < ref.Size; y++ {
		for x := 0; x < ref.Size; x++ {
			got.SetRGBA(x, y, color.RGBA{ref.Background[0], ref.Background[1], ref.Background[2], ref.Background[3]})
		}
	}
	sort.SliceStable(ref.Entities, func(i, j int) bool {
		return ref.Prototypes[ref.Entities[i].Name].Order > ref.Prototypes[ref.Entities[j].Name].Order
	})
	for _, ent := range ref.Entities {
		proto, ok := ref.Prototypes[ent.Name]
		if !ok {
			t.Fatalf("unknown prototype %s", ent.Name)
		}
		if proto.Dither && !factorioPreviewEntityDither(ent.X, ent.Y, 1) {
			continue
		}
		ink := color.RGBA{proto.Color[0], proto.Color[1], proto.Color[2], proto.Color[3]}
		paintFactorioChartEntity(got, fastPreviewSettings{}, ent.X, ent.Y, proto.Box, ink, ref.Origin[0], ref.Origin[1], 1)
	}
	file, err := os.Open("testdata/chart-2.1.23/chart.png")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	want, err := png.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	stats, _, _ := previewImageDiff(want, got)
	if stats.ChangedPercent != 0 {
		t.Fatalf("chart differs in %.6f%% of pixels, max delta %d", stats.ChangedPercent, stats.MaxChannelDelta)
	}
	// Magnifying the captured chart must repeat complete world tiles, even
	// where a fractional scale puts a rectangle edge near a float boundary.
	for _, scale := range []float64{.4, .75} {
		origin := -float64(ref.Size/2) * scale
		zoomed := image.NewRGBA(got.Bounds())
		for y := 0; y < ref.Size; y++ {
			for x := 0; x < ref.Size; x++ {
				zoomed.SetRGBA(x, y, color.RGBA{ref.Background[0], ref.Background[1], ref.Background[2], ref.Background[3]})
			}
		}
		for _, ent := range ref.Entities {
			proto := ref.Prototypes[ent.Name]
			if proto.Dither && !factorioPreviewEntityDither(ent.X, ent.Y, 1) {
				continue
			}
			paintFactorioChartEntity(zoomed, fastPreviewSettings{}, ent.X, ent.Y, proto.Box,
				color.RGBA{proto.Color[0], proto.Color[1], proto.Color[2], proto.Color[3]}, origin, origin, scale)
		}
		for y := 0; y < ref.Size; y++ {
			for x := 0; x < ref.Size; x++ {
				wx := int(math.Floor(float64(x-ref.Size/2)*scale) - ref.Origin[0])
				wy := int(math.Floor(float64(y-ref.Size/2)*scale) - ref.Origin[1])
				if zoomed.At(x, y) != want.At(wx, wy) {
					// Decode may use a different concrete color type.
					if color.RGBAModel.Convert(zoomed.At(x, y)) != color.RGBAModel.Convert(want.At(wx, wy)) {
						t.Fatalf("chart scale %g pixel (%d,%d) differs from reference tile (%d,%d)", scale, x, y, wx, wy)
					}
				}
			}
		}
	}
}
