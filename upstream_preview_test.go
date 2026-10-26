package main

import (
	"image/color"
	"image/png"
	"os"
	"testing"
)

func TestUpstreamExistingTerrainPreview(t *testing.T) {
	for _, planet := range []string{"nauvis", "vulcanus"} {
		t.Run(planet, func(t *testing.T) {
			f, err := os.Open("testdata/space-age-upstream/oracle-preview-" + planet + "-terrain.seed123456.png")
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
			settings := defaultFactorioTerrainSettings(fastPreviewSurfaceSeed(123456, planet))
			settings.planet = planet
			if planet == "vulcanus" {
				settings.autoplaceControls = map[string]fastControl{}
				for _, name := range factorioVulcanusResourceNames {
					settings.autoplaceControls[name] = fastControl{frequency: 1, size: 0, richness: 1}
				}
			}
			var sample func(float64, float64) color.RGBA
			if planet == "nauvis" {
				e := newFactorioNauvisEvaluator(settings)
				sample = func(x, y float64) color.RGBA { return e.terrainTileFast(e.sample(x, y), x, y).color }
			} else {
				e := newFactorioVulcanusEvaluator(settings, true)
				sample = func(x, y float64) color.RGBA { return e.sample(x, y).tile.color }
			}
			diff := 0
			for y := 0; y < ref.Bounds().Dy(); y++ {
				for x := 0; x < ref.Bounds().Dx(); x++ {
					got := sample(float64(x-ref.Bounds().Dx()/2), float64(y-ref.Bounds().Dy()/2))
					r, g, b, _ := ref.At(x, y).RGBA()
					if got.R != uint8(r>>8) || got.G != uint8(g>>8) || got.B != uint8(b>>8) {
						diff++
					}
				}
			}
			t.Logf("%s preview differences: %d/%d", planet, diff, ref.Bounds().Dx()*ref.Bounds().Dy())
			limit := map[string]int{"nauvis": 10, "vulcanus": 1}[planet]
			if diff > limit {
				t.Errorf("too many differences %d", diff)
			}
		})
	}
}
