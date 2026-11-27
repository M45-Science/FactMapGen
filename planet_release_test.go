package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type planetReleaseOracle struct {
	FactorioVersion string
	MapSeed         uint32
	Positions       [][2]float64
	Cases           []struct {
		Planet   string
		Seed     uint32
		Settings json.RawMessage
		Fields   map[string][]float64
		Cliffs   json.RawMessage
	}
}

func TestPlanetStandaloneHoldout(t *testing.T) {
	if testing.Short() {
		t.Skip("full five-planet holdout comparison")
	}
	data, err := os.ReadFile("testdata/planets-2.1.23/holdouts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		FactorioVersion string
		Cases           []struct {
			Planet   string
			Size     int
			Center   [2]float64
			Settings json.RawMessage
			SHA256   string
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fixture.FactorioVersion, "2.1.23") || len(fixture.Cases) != 10 {
		t.Fatal("incomplete holdout captures")
	}
	cache := newFastPreviewCache(defaultFastPreviewCacheBytes, defaultFastPreviewCacheWorlds)
	for _, c := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/%d", c.Planet, c.Size), func(t *testing.T) {
			s, err := parseFastPreviewSettingsForPlanet(c.Settings, "", c.Planet)
			if err != nil {
				t.Fatal(err)
			}
			key, err := fastPreviewCacheKeyForPlanet(c.Settings, s.seed, c.Planet)
			if err != nil {
				t.Fatal(err)
			}
			x, y := factorioPreviewChunkCenter(c.Center[0]), factorioPreviewChunkCenter(c.Center[1])
			img, err := cache.renderFrame(context.Background(), key, s, c.Size, x, y)
			if err != nil {
				t.Fatal(err)
			}
			if digest := fmt.Sprintf("%x", sha256.Sum256(img.Pix)); digest != c.SHA256 {
				t.Errorf("preview hash %s, want %s from Factorio", digest, c.SHA256)
			}
		})
	}
}

func TestPlanetReleaseAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("full five-planet image comparison")
	}
	cache := newFastPreviewCache(defaultFastPreviewCacheBytes, defaultFastPreviewCacheWorlds)
	for _, seed := range []uint32{123456, 987654321} {
		path := fmt.Sprintf("testdata/planets-2.1.23/fields.seed%d.json", seed)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var ref planetReleaseOracle
		if err = json.Unmarshal(b, &ref); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(ref.FactorioVersion, "2.1.23") || len(ref.Cases) != 5 || len(ref.Positions) != 1089 {
			t.Fatal("incomplete oracle")
		}
		for _, c := range ref.Cases {
			t.Run(fmt.Sprintf("%s/%d", c.Planet, seed), func(t *testing.T) {
				s, err := parseFastPreviewSettingsForPlanet(c.Settings, "", c.Planet)
				if err != nil {
					t.Fatal(err)
				}
				s.seed = c.Seed
				var terrain func(float64, float64) color.RGBA
				var space *factorioSpaceAgeEvaluator
				var cliff *factorioCliffEvaluator
				if c.Planet == "nauvis" {
					n := newFactorioNauvisEvaluator(s)
					terrain = func(x, y float64) color.RGBA { return n.terrainTileFast(n.sample(x, y), x, y).color }
					cliff = newFactorioCliffEvaluator(s, n)
				} else {
					space = newFactorioSpaceAgeEvaluator(s)
					switch c.Planet {
					case "vulcanus":
						terrain = func(x, y float64) color.RGBA { return space.vulcanus.sample(x, y).tile.color }
					case "gleba":
						terrain = func(x, y float64) color.RGBA { return space.gleba.sample(x, y).tile.color }
					case "fulgora":
						terrain = func(x, y float64) color.RGBA { return space.fulgora.sample(x, y).tile.color }
					case "aquilo":
						terrain = func(x, y float64) color.RGBA { return space.aquilo.sample(x, y).tile.color }
					}
				}
				img := image.NewRGBA(image.Rect(0, 0, 512, 512))
				for y := 0; y < 512; y++ {
					for x := 0; x < 512; x++ {
						img.SetRGBA(x, y, terrain(float64(x-256), float64(y-256)))
					}
				}
				compare := func(layer string, img image.Image) {
					f, err := os.Open(fmt.Sprintf("testdata/planets-2.1.23/%s-%s.seed%d.png", c.Planet, layer, seed))
					if err != nil {
						t.Fatal(err)
					}
					want, err := png.Decode(f)
					f.Close()
					if err != nil {
						t.Fatal(err)
					}
					if want.Bounds() != img.Bounds() {
						t.Fatal("oracle dimensions differ")
					}
					stats, diff, _ := previewImageDiff(want, img)
					if layer == "full" && (c.Planet == "nauvis" || c.Planet == "gleba") {
						_, changed := factorioEnemyPixelDifferences(want, img, 4)
						if changed != 0 {
							t.Errorf("enemy pixels differ at %d interior positions", changed)
						}
					}
					t.Logf("%s: total RGB error %d, average %.6f", layer, stats.TotalAbsDelta, stats.MeanAbsDelta)
					if stats.TotalAbsDelta != 0 {
						t.Errorf("%s total RGB error %d, want zero", layer, stats.TotalAbsDelta)
					}
					if os.Getenv("FACTMAPGEN_ACCURACY_ARTIFACTS") == "1" {
						dir := filepath.Join("test-output", "accuracy-2.1.23", fmt.Sprint(seed))
						if err := os.MkdirAll(dir, 0755); err != nil {
							t.Fatal(err)
						}
						writePNGArtifact(t, filepath.Join(dir, c.Planet+"-"+layer+".png"), img)
						writePNGArtifact(t, filepath.Join(dir, c.Planet+"-"+layer+"-diff.png"), diff)
					}
				}
				var cells []factorioCliffCell
				if cliff != nil {
					cells, err = cliff.placedCells(context.Background(), -256, -256, 256, 256)
				} else {
					cells, err = space.cliffCells(context.Background(), -256, -256, 256, 256)
				}
				if err != nil {
					t.Fatal(err)
				}
				var real []struct{ X, Y float64 }
				if string(c.Cliffs) != "{}" {
					if err = json.Unmarshal(c.Cliffs, &real); err != nil {
						t.Fatal(err)
					}
				}
				seen := map[factorioPoint]bool{}
				for _, v := range cells {
					seen[v.factorioPoint] = true
				}
				matched := 0
				for _, v := range real {
					if seen[factorioPoint{x: v.X, y: v.Y}] {
						matched++
					}
				}
				t.Logf("world cliff positions %d/%d, predicted %d", matched, len(real), len(cells))
				if c.Planet == "nauvis" || c.Planet == "fulgora" || c.Planet == "aquilo" {
					if matched != len(real) || len(cells) != matched {
						t.Error("cliff position regression")
					}
				} else if len(real) == 0 || float64(matched)/float64(len(real)) < .99 {
					t.Error("cliff recall fell below 99%")
				}

				compare("terrain", img)
				s.entityBounds = fastPreviewChunkBounds(512, 1, 0, 0)
				if cliff != nil {
					if err := renderFactorioCliffs(context.Background(), img, s, cliff, -256, -256, 1); err != nil {
						t.Fatal(err)
					}
				} else {
					space.settings.entityBounds = s.entityBounds
					pixels, err := space.cliffPixels(context.Background(), 512, 512, -256, -256, 1)
					if err != nil {
						t.Fatal(err)
					}
					for i, on := range pixels {
						if on {
							img.SetRGBA(i%512, i/512, factorioCliffMapColor)
						}
					}
				}
				compare("cliffs", img)
				key, err := fastPreviewCacheKeyForPlanet(c.Settings, c.Seed, c.Planet)
				if err != nil {
					t.Fatal(err)
				}
				full, err := cache.renderFrame(context.Background(), key, s, 512, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				compare("full", full)
			})
		}
	}
}
