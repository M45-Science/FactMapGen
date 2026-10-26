package main

import (
	"encoding/json"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"
)

func TestFactorioFulgoraUpstreamFields(t *testing.T) {
	e := newFactorioFulgoraEvaluator(fastPreviewSettings{seed: 123456, planet: fastPreviewPlanetFulgora})
	for _, layer := range []string{"cells", "elevation", "scrap"} {
		t.Run(layer, func(t *testing.T) {
			data, err := os.ReadFile("testdata/space-age-upstream/oracle-fulgora-" + layer + ".seed123456.json")
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct{ Positions []struct{ X, Y float64 } }
			if err = json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			getters := map[string]func(factorioFulgoraSample) float32{
				"fulgora_cells":             func(s factorioFulgoraSample) float32 { return s.cells },
				"fulgora_pyramids":          func(s factorioFulgoraSample) float32 { return s.pyramids },
				"fulgora_spots":             func(s factorioFulgoraSample) float32 { return s.spots },
				"fulgora_natural":           func(s factorioFulgoraSample) float32 { return s.natural },
				"fulgora_mix_oil":           func(s factorioFulgoraSample) float32 { return s.mixOil },
				"fulgora_elevation":         func(s factorioFulgoraSample) float32 { return s.elevation },
				"fulgora_scrap_probability": func(s factorioFulgoraSample) float32 { return s.scrapProbability },
			}
			for key, get := range getters {
				raw, ok := fields[key]
				if !ok {
					continue
				}
				var want []float64
				if err := json.Unmarshal(raw, &want); err != nil {
					t.Fatal(err)
				}
				if len(want) != len(fixture.Positions) || len(want) == 0 {
					t.Fatalf("invalid %s fixture lengths", key)
				}
				worst := 0.0
				for i, p := range fixture.Positions {
					got := float64(get(e.sample(p.X, p.Y)))
					worst = math.Max(worst, math.Abs(got-want[i]))
				}
				t.Logf("%s worst residual %g", key, worst)
				if worst != 0 {
					t.Errorf("%s residual %g", key, worst)
				}
			}
		})
	}
}
func TestFactorioFulgoraUpstreamTiles(t *testing.T) {
	data, err := os.ReadFile("testdata/space-age-upstream/oracle-fulgora-tiles.seed123456.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Positions []struct{ X, Y float64 }
		TileNames []string
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	e := newFactorioFulgoraEvaluator(fastPreviewSettings{seed: 123456, planet: fastPreviewPlanetFulgora})
	matches := 0
	for i, p := range f.Positions {
		if strings.TrimSuffix(e.sample(p.X, p.Y).tile.name, "-2") == strings.TrimSuffix(f.TileNames[i], "-2") {
			matches++
		}
	}
	t.Logf("Fulgora placed tiles: %d/%d", matches, len(f.TileNames))
	if len(f.Positions) != 5057 || len(f.TileNames) != 5057 || matches < 4915 {
		t.Fatalf("only %d matching tiles", matches)
	}
}

func TestFactorioFulgoraPreviewOracle(t *testing.T) {
	f, err := os.Open("testdata/space-age-upstream/oracle-preview-fulgora-terrain.seed123456.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ref, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Bounds().Dx() != 1024 || ref.Bounds().Dy() != 1024 {
		t.Fatalf("unexpected oracle dimensions: %v", ref.Bounds())
	}
	e := newFactorioFulgoraEvaluator(fastPreviewSettings{seed: fastPreviewSurfaceSeed(123456, fastPreviewPlanetFulgora), planet: fastPreviewPlanetFulgora, autoplaceControls: map[string]fastControl{"scrap": {enabled: false}}}, true)
	differences := 0
	for y := 0; y < ref.Bounds().Dy(); y++ {
		for x := 0; x < ref.Bounds().Dx(); x++ {
			got := e.sample(float64(x-ref.Bounds().Dx()/2), float64(y-ref.Bounds().Dy()/2)).tile.color
			r, g, b, _ := ref.At(x, y).RGBA()
			if got.R != uint8(r>>8) || got.G != uint8(g>>8) || got.B != uint8(b>>8) {
				differences++
			}
		}
	}
	t.Logf("Fulgora preview differences: %d/%d", differences, ref.Bounds().Dx()*ref.Bounds().Dy())
	if differences > 20 {
		t.Fatalf("too many differences: %d", differences)
	}
}
