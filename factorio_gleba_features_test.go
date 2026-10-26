package main

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

func TestGlebaFeatureFields(t *testing.T) {
	b, err := os.ReadFile("testdata/gleba/oracle-features-2.1.23.json")
	if err != nil {
		t.Fatal(err)
	}
	var capture struct {
		FactorioVersion string
		Cases           []struct {
			Seed      uint32
			Positions [][2]float64
			Fields    map[string][]float32
		}
	}
	if err = json.Unmarshal(b, &capture); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capture.FactorioVersion, "2.1.23") || len(capture.Cases) != 1 || len(capture.Cases[0].Positions) != 2959 {
		t.Fatal("incomplete or wrong-version feature oracle")
	}
	ref := capture.Cases[0]
	s := fastPreviewSettings{seed: ref.Seed, planet: fastPreviewPlanetGleba, cliffRichness: .8, cliffs: fastControl{frequency: 1, size: 1, enabled: true}}
	e := newFactorioGlebaEvaluator(s, false)
	f := newGlebaFeatureFields(e, false)
	worst := map[string]float64{}
	for i, p := range ref.Positions {
		v := f.sample(e.sample(p[0], p[1]), p[0], p[1])
		got := map[string]float32{"entity:stone:probability": v.stoneProbability, "entity:stone:richness": v.stoneRichness, "cliffiness": v.cliffiness, "entity:iron-stromatolite:probability": v.stromatolites[0], "entity:copper-stromatolite:probability": v.stromatolites[1]}
		for j, name := range glebaPlantNames {
			got["entity:"+name+":probability"] = v.plants[j]
		}
		for k, x := range got {
			if len(ref.Fields[k]) != len(ref.Positions) {
				t.Fatalf("incomplete field %s", k)
			}
			d := math.Abs(float64(x - ref.Fields[k][i]))
			if math.IsNaN(d) || math.IsInf(d, 0) {
				t.Fatalf("nonfinite field %s at %v", k, p)
			}
			worst[k] = max(worst[k], d)
		}
	}
	for k, d := range worst {
		t.Logf("%s residual %g", k, d)
		if d > .000002 {
			t.Errorf("%s residual %g", k, d)
		}
	}
}
