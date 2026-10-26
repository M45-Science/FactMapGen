package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

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
				candidates, err := e.entityCandidates(context.Background(), -256, -256, 256, 256)
				if err != nil {
					t.Fatal(err)
				}
				var entities []struct {
					Name string
					X, Y float64
				}
				if err = json.Unmarshal(c.Entities, &entities); err != nil {
					t.Fatal(err)
				}
				lookup := map[factorioAquiloEntity]bool{}
				for _, p := range candidates {
					lookup[p] = true
				}
				totals, matched := map[string]int{}, map[string]int{}
				for _, p := range entities {
					for k, n := range factorioAquiloEntityNames {
						if n == p.Name {
							totals[n]++
							if lookup[factorioAquiloEntity{factorioPoint: factorioPoint{x: p.X, y: p.Y}, kind: k}] {
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
