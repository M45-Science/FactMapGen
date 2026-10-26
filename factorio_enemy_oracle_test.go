package main

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func factorioEnemyPixelDifferences(want, got image.Image, margin int) (count, differing int) {
	for y := margin; y < want.Bounds().Dy()-margin; y++ {
		for x := margin; x < want.Bounds().Dx()-margin; x++ {
			a := color.RGBAModel.Convert(want.At(x+want.Bounds().Min.X, y+want.Bounds().Min.Y)) == factorioEnemyMapColor
			b := color.RGBAModel.Convert(got.At(x+got.Bounds().Min.X, y+got.Bounds().Min.Y)) == factorioEnemyMapColor
			if a {
				count++
			}
			if a != b {
				differing++
			}
		}
	}
	return
}

func TestFactorioEnemyReleasePreviews(t *testing.T) {
	if testing.Short() {
		t.Skip("additional native enemy previews")
	}
	const directory = "testdata/enemies-2.1.23"
	data, err := os.ReadFile(filepath.Join(directory, "previews.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		FactorioVersion string
		Size            int
		Cases           []struct {
			Planet, Variant, Image, PaddedImage string
			Center                              [2]float64
			Settings                            json.RawMessage
		}
	}
	if err = json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ref.FactorioVersion, "2.1.23") || len(ref.Cases) != 6 || ref.Size != 512 {
		t.Fatal("incomplete preview oracle")
	}
	for _, c := range ref.Cases {
		t.Run(c.Planet+"/"+c.Variant, func(t *testing.T) {
			s, err := parseFastPreviewSettingsForPlanet(c.Settings, "", c.Planet)
			if err != nil {
				t.Fatal(err)
			}
			var seed struct{ Seed uint32 }
			if err = json.Unmarshal(c.Settings, &seed); err != nil {
				t.Fatal(err)
			}
			s.seed = seed.Seed
			got, _, err := renderFastMapPreviewAt(context.Background(), s, ref.Size, previewZoom{mode: "normal", tilesPerPixel: 1, renderSize: ref.Size}, c.Center[0], c.Center[1])
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(filepath.Join(directory, c.Image))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			want, err := png.Decode(f)
			if err != nil {
				t.Fatal(err)
			}
			if got.Bounds() != want.Bounds() {
				t.Fatal("preview bounds differ")
			}
			_, edges := factorioEnemyPixelDifferences(want, got, 0)
			// A larger independent native capture exposes the same world area
			// without the CLI preview's outer-border candidate truncation.
			p, err := os.Open(filepath.Join(directory, c.PaddedImage))
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			padded, err := png.Decode(p)
			if err != nil {
				t.Fatal(err)
			}
			if padded.Bounds() != image.Rect(0, 0, 640, 640) {
				t.Fatal("wrong padded capture size")
			}
			cropped := padded.(interface {
				SubImage(image.Rectangle) image.Image
			}).SubImage(image.Rect(64, 64, 576, 576))
			count, diff := factorioEnemyPixelDifferences(cropped, got, 0)
			t.Logf("enemy pixels: %d reference, %d differing against padded capture, %d differing against original capture", count, diff, edges)
			if count == 0 || diff != 0 {
				t.Errorf("enemy placement or deferred drawing regression")
			}
			if os.Getenv("FACTMAPGEN_ACCURACY_ARTIFACTS") == "1" {
				dir := filepath.Join("test-output", "enemies-2.1.23")
				if err = os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				writePNGArtifact(t, filepath.Join(dir, c.Image), got)
			}
		})
	}
}

func TestFactorioEnemyReleaseFields(t *testing.T) {
	data, err := os.ReadFile("testdata/enemies-2.1.23/fields.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		FactorioVersion string
		Positions       [][2]float64
		Cases           []struct {
			Planet   string
			Seed     uint32
			Settings json.RawMessage
			Fields   map[string][]float64
		}
	}
	if err = json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ref.FactorioVersion, "2.1.23") || len(ref.Cases) != 4 || len(ref.Positions) != 2113 {
		t.Fatal("incomplete enemy oracle")
	}
	positions := make([]factorioSpotCandidate, len(ref.Positions))
	for i, p := range ref.Positions {
		positions[i] = factorioSpotCandidate{x: p[0], y: p[1]}
	}
	for _, c := range ref.Cases {
		s, err := parseFastPreviewSettingsForPlanet(c.Settings, "", c.Planet)
		if err != nil {
			t.Fatal(err)
		}
		s.seed = c.Seed
		if c.Planet == "gleba" {
			s.enemyBases = fastSpaceAgeControl(s, "gleba_enemy_base")
			s.enemyBases.frequency *= 2
		}
		e := newFactorioEnemyEvaluator(s)
		want := c.Fields["enemy_base_probability"]
		if len(want) != len(positions) {
			t.Fatal("incomplete enemy base field")
		}
		worst := 0.0
		for i, p := range positions {
			got := e.baseProbabilityAt(p.x, p.y)
			d := math.Abs(got - want[i])
			worst = max(worst, d)
			if math.IsNaN(d) || d > .000003 || (got > 0) != (want[i] > 0) {
				t.Fatalf("%s seed %d at %v: base %g, want %g", c.Planet, c.Seed, p, got, want[i])
			}
		}
		t.Logf("%s seed %d: raw base residual %g", c.Planet, c.Seed, worst)
		if c.Planet != "nauvis" {
			continue
		} // Gleba's final fields have a separate terrain-aware oracle.
		names := []string{"biter-spawner", "spitter-spawner", "small-worm-turret", "medium-worm-turret", "big-worm-turret", "behemoth-worm-turret"}
		for j, proto := range factorioEnemyOverviewPrototypes {
			values := e.prototypeValues(positions, proto)
			want := c.Fields["entity:"+names[j]+":probability"]
			if len(want) != len(positions) {
				t.Fatal("incomplete enemy prototype field")
			}
			for i, v := range values {
				// Probability below zero has the same placement behavior as zero.
				d := math.Abs(max(0, v) - max(0, want[i]))
				// Distant worm multipliers amplify the base field's float error.
				if math.IsNaN(d) || d > .00002 {
					t.Fatalf("%s seed %d at %v: probability %g, want %g", names[j], c.Seed, positions[i], v, want[i])
				}
			}
		}
	}
}
