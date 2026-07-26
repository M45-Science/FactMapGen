package main

import (
	"math"
	"testing"
)

type factorioRockOracle struct {
	Seed      uint32               `json:"seed0"`
	Positions []naturalOraclePoint `json:"positions"`
	Values    []float64            `json:"values"`
}

func TestFactorioRockDensityMatchesOracle(t *testing.T) {
	fixture := readResourceOracle[factorioRockOracle](t, "oracle-rock-density.seed123456.json")
	settings := defaultFactorioTerrainSettings(fixture.Seed)
	settings.rocks = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	evaluator := newFactorioRockEvaluator(settings, newFactorioNauvisEvaluator(settings))
	worstAbsolute := 0.0
	worstRelative := 0.0
	for index, position := range fixture.Positions {
		got := evaluator.rockDensityAt(position.X, position.Y)
		want := fixture.Values[index]
		delta := math.Abs(got - want)
		worstAbsolute = math.Max(worstAbsolute, delta)
		worstRelative = math.Max(worstRelative, delta/math.Max(1, math.Abs(want)))
	}
	if worstAbsolute >= 1e-3 && worstRelative >= 1e-2 {
		t.Fatalf("rock density oracle residuals: absolute=%g relative=%g", worstAbsolute, worstRelative)
	}
}

func TestFactorioRockFieldIsDeterministicAndBounded(t *testing.T) {
	settings := defaultFactorioTerrainSettings(123456)
	settings.rocks = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	evaluator := newFactorioRockEvaluator(settings, newFactorioNauvisEvaluator(settings))
	for _, position := range []factorioPoint{{300, -180}, {512, 512}, {-800, 640}, {40, 40}} {
		first := evaluator.densityAt(position.x, position.y)
		second := evaluator.densityAt(position.x, position.y)
		if first != second {
			t.Fatalf("rock density at (%g,%g) is not deterministic: %g != %g", position.x, position.y, first, second)
		}
		if first < 0 || first > 1 {
			t.Fatalf("rock density at (%g,%g) = %g, want [0,1]", position.x, position.y, first)
		}
	}
}

func TestFactorioRockControlDisablesOverlay(t *testing.T) {
	settings := defaultFactorioTerrainSettings(123456)
	settings.rocks = fastControl{frequency: 0, size: 0, richness: 1, enabled: false}
	evaluator := &factorioRockEvaluator{control: settings.rocks}
	if got := evaluator.densityAt(512, 512); got != 0 {
		t.Fatalf("disabled rock density = %g, want 0", got)
	}
}
