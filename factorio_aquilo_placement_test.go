package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestAquiloWideChartOrder(t *testing.T) {
	raw, err := os.ReadFile("default-presets/Default/" + mapGenFile)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := parseFastPreviewSettingsForPlanet(raw, "123456", fastPreviewPlanetAquilo)
	if err != nil {
		t.Fatal(err)
	}
	const scale = 64
	img, err := newFastPreviewWorld(settings).render(context.Background(), 32, scale, 19*scale, -23*scale)
	if err != nil {
		t.Fatal(err)
	}
	// At this zoom, footprints from different chunks share screen pixels.
	const baseline = "bcbcda1947b9f4345b2f47fd65dc896d3d7d8fc5866fafa925a09d0faff95d74"
	if got := fmt.Sprintf("%x", sha256.Sum256(img.Pix)); got != baseline {
		t.Fatalf("wide Aquilo preview hash = %s, want %s", got, baseline)
	}
}

func TestAquiloReleaseCandidates(t *testing.T) {
	for _, seed := range []uint32{123456, 987654321} {
		b, err := os.ReadFile(fmt.Sprintf("testdata/planets-2.1.23/fields.seed%d.json", seed))
		if err != nil {
			t.Fatal(err)
		}
		var ref struct {
			Cases []struct {
				Planet   string
				Seed     uint32
				Settings json.RawMessage
				Entities json.RawMessage
			}
		}
		if err = json.Unmarshal(b, &ref); err != nil {
			t.Fatal(err)
		}
		for _, c := range ref.Cases {
			if c.Planet == "aquilo" {
				s, err := parseFastPreviewSettingsForPlanet(c.Settings, "", c.Planet)
				if err != nil {
					t.Fatal(err)
				}
				s.seed = c.Seed
				e := newFactorioAquiloEvaluator(s, true)
				var entities []struct {
					Name string
					X, Y float64
				}
				if err = json.Unmarshal(c.Entities, &entities); err != nil {
					t.Fatal(err)
				}
				lookup := map[factorioChartEntity]bool{}
				for cy := -8; cy < 8; cy++ {
					for cx := -8; cx < 8; cx++ {
						candidates, err := e.chartChunk(context.Background(), cx, cy, factorioAquiloLand{})
						if err != nil {
							t.Fatal(err)
						}
						for _, p := range candidates {
							lookup[p] = true
						}
					}
				}
				totals, matched := map[string]int{}, map[string]int{}
				for _, p := range entities {
					for k, n := range factorioAquiloEntityNames {
						if n == p.Name {
							totals[n]++
							if lookup[factorioChartEntity{x: p.X, y: p.Y, kind: k}] {
								matched[n]++
							}
						}
					}
				}
				if len(totals) != 5 {
					t.Fatal("incomplete Aquilo entity census")
				}
				for n, count := range totals {
					minimum := map[string]float64{"fluorine-vent": 1, "lithium-brine": 1, "crude-oil": .6, "lithium-iceberg-huge": .18, "lithium-iceberg-big": .18}[n]
					if float64(matched[n])/float64(count) < minimum {
						t.Errorf("%s candidate recall regressed", n)
					}
					t.Logf("seed %d %s exact candidates %d/%d", seed, n, matched[n], count)
				}
			}
		}
	}
}
