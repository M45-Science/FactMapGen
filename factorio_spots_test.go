package main

import "testing"

func TestFactorioSpotSkipOffsetCanEqualSpan(t *testing.T) {
	params := factorioSpotSelectionParams{regionSize: 400, candidateSpotCount: 22, skipSpan: 1,
		density: func(float64, float64) float64 { return 1 }, quantity: func(float64, float64) float64 { return 1 }, favorability: func(float64, float64) float64 { return 1 }}
	key := factorioSpotRegionKey{seed0: 123456, seed1: 1000}
	all := factorioSelectSpots(key, params)
	params.skipOffset = 1
	skipped := factorioSelectSpots(key, params)
	if len(all) != 22 || len(skipped) != 21 {
		t.Fatalf("counts before/after skip = %d/%d", len(all), len(skipped))
	}
	for i, v := range skipped {
		if v != all[i+1] {
			t.Fatalf("skip changed candidate %d", i)
		}
	}
}
