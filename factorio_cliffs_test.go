package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

type factorioCliffFieldOracle struct {
	Positions []naturalOraclePoint `json:"positions"`
	Cases     []struct {
		Seed   uint32    `json:"seed"`
		Values []float64 `json:"values"`
	} `json:"cases"`
}

type factorioCliffEntityOracle struct {
	Region struct {
		X0 float64 `json:"x0"`
		Y0 float64 `json:"y0"`
		X1 float64 `json:"x1"`
		Y1 float64 `json:"y1"`
	} `json:"region"`
	Cases []struct {
		Seed   uint32               `json:"seed"`
		Cliffs []naturalOraclePoint `json:"cliffs"`
	} `json:"cases"`
}

func TestFactorioCliffFieldsMatchReferenceOracles(t *testing.T) {
	elevationFixture := readNaturalOracle[factorioCliffFieldOracle](t, "oracle-cliff-elevation.seed123456.json")
	for _, testCase := range elevationFixture.Cases {
		settings := defaultFactorioNaturalSettings(testCase.Seed)
		evaluator := newFactorioCliffEvaluator(settings, newFactorioNauvisEvaluator(settings))
		worst := 0.0
		for index, position := range elevationFixture.Positions {
			got := evaluator.cliffElevationAt(position.X, position.Y)
			want := testCase.Values[index]
			delta := math.Abs(got - want)
			worst = math.Max(worst, delta)
			if delta >= math.Max(1, 1e-2*math.Abs(want)) {
				t.Errorf("cliff elevation seed %d at (%g,%g) = %g, want %g", testCase.Seed, position.X, position.Y, got, want)
			}
		}
		if worst >= 10 {
			t.Errorf("cliff elevation seed %d worst residual = %g", testCase.Seed, worst)
		}
	}

	cliffinessFixture := readNaturalOracle[factorioCliffFieldOracle](t, "oracle-cliffiness.seed123456.json")
	for _, testCase := range cliffinessFixture.Cases {
		settings := defaultFactorioNaturalSettings(testCase.Seed)
		evaluator := newFactorioCliffEvaluator(settings, newFactorioNauvisEvaluator(settings))
		mismatches := 0
		for index, position := range cliffinessFixture.Positions {
			if got, want := evaluator.cliffinessAt(position.X, position.Y), testCase.Values[index]; got != want {
				mismatches++
				if mismatches <= 8 {
					t.Logf("cliffiness seed %d at (%g,%g) = %g, want %g", testCase.Seed, position.X, position.Y, got, want)
				}
			}
		}
		if mismatches != 0 {
			t.Errorf("cliffiness seed %d mismatches = %d/%d", testCase.Seed, mismatches, len(cliffinessFixture.Positions))
		}
	}
}

func TestFactorioCliffPlacementMatchesEntityOracle(t *testing.T) {
	fixture := readNaturalOracle[factorioCliffEntityOracle](t, "oracle-cliff-entities.seed123456.json")
	for _, testCase := range fixture.Cases {
		settings := defaultFactorioNaturalSettings(testCase.Seed)
		evaluator := newFactorioCliffEvaluator(settings, newFactorioNauvisEvaluator(settings))
		placed, err := evaluator.placedCells(
			context.Background(),
			fixture.Region.X0,
			fixture.Region.Y0,
			fixture.Region.X1,
			fixture.Region.Y1,
		)
		if err != nil {
			t.Fatalf("place cliffs for seed %d: %v", testCase.Seed, err)
		}
		predicted := make(map[factorioPoint]bool, len(placed))
		for _, point := range placed {
			predicted[point] = true
			if positiveMod(point.x, factorioCliffGridSize) != factorioCliffCellCenterX ||
				positiveMod(point.y, factorioCliffGridSize) != factorioCliffCellCenterY {
				t.Errorf("predicted cliff is off lattice: (%g,%g)", point.x, point.y)
			}
		}
		matched := 0
		for _, point := range testCase.Cliffs {
			if positiveMod(point.X, factorioCliffGridSize) != factorioCliffCellCenterX ||
				positiveMod(point.Y, factorioCliffGridSize) != factorioCliffCellCenterY {
				t.Errorf("oracle cliff is off lattice: (%g,%g)", point.X, point.Y)
			}
			if predicted[factorioPoint{x: point.X, y: point.Y}] {
				matched++
			}
		}
		matchFraction := float64(matched) / float64(len(testCase.Cliffs))
		if matchFraction < 0.85 {
			t.Errorf("cliff placement seed %d matched %d/%d (%.1f%%), want >= 85%%", testCase.Seed, matched, len(testCase.Cliffs), matchFraction*100)
		}
		t.Logf("cliff placement seed %d matched %d/%d (%.1f%%), predicted %d", testCase.Seed, matched, len(testCase.Cliffs), matchFraction*100, len(placed))
	}
}

func TestFactorioCrossesCliff(t *testing.T) {
	tests := []struct {
		name       string
		a          float64
		b          float64
		cliffiness float64
		want       int
	}{
		{name: "same band", a: 12, b: 20, cliffiness: 10, want: 0},
		{name: "negative elevation", a: -1, b: 60, cliffiness: 10, want: 0},
		{name: "below first band", a: 5, b: 8, cliffiness: 10, want: 0},
		{name: "gate off", a: 45, b: 55, cliffiness: 0.5, want: 0},
		{name: "crossing up", a: 45, b: 55, cliffiness: 10, want: 1},
		{name: "crossing down", a: 55, b: 45, cliffiness: 10, want: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := factorioCrossesCliff(test.a, test.b, test.cliffiness, 10, 40); got != test.want {
				t.Fatalf("crossing = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRenderFactorioCliffsPaintsChartColorAndSkipsWater(t *testing.T) {
	settings := defaultFactorioNaturalSettings(123456)
	evaluator := newFactorioCliffEvaluator(settings, newFactorioNauvisEvaluator(settings))
	landColor := color.RGBA{R: 90, G: 90, B: 90, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, landColor)
		}
	}
	if err := renderFactorioCliffs(context.Background(), img, settings, evaluator, 960, 512, 1); err != nil {
		t.Fatalf("render cliffs: %v", err)
	}
	painted := 0
	adjacent := false
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			if img.RGBAAt(x, y) != factorioCliffMapColor {
				continue
			}
			painted++
			if x+1 < 64 && img.RGBAAt(x+1, y) == factorioCliffMapColor ||
				y+1 < 64 && img.RGBAAt(x, y+1) == factorioCliffMapColor {
				adjacent = true
			}
		}
	}
	if painted == 0 || !adjacent {
		t.Fatalf("painted cliff pixels = %d, adjacent = %v", painted, adjacent)
	}

	water := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			water.SetRGBA(x, y, factorioTerrainTiles[0].color)
		}
	}
	if err := renderFactorioCliffs(context.Background(), water, settings, evaluator, 960, 512, 1); err != nil {
		t.Fatalf("render cliffs over water: %v", err)
	}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			if got := water.RGBAAt(x, y); got != factorioTerrainTiles[0].color {
				t.Fatalf("water changed at (%d,%d): %#v", x, y, got)
			}
		}
	}
}

func TestFactorioCliffsDisableWithContinuityOrRichness(t *testing.T) {
	for _, mutate := range []func(*fastPreviewSettings){
		func(settings *fastPreviewSettings) {
			settings.cliffs.size = 0
			settings.cliffs.enabled = false
		},
		func(settings *fastPreviewSettings) { settings.cliffRichness = 0 },
	} {
		settings := defaultFactorioNaturalSettings(123456)
		mutate(&settings)
		evaluator := newFactorioCliffEvaluator(settings, newFactorioNauvisEvaluator(settings))
		placed, err := evaluator.placedCells(context.Background(), 0, 0, 512, 512)
		if err != nil {
			t.Fatalf("place disabled cliffs: %v", err)
		}
		if len(placed) != 0 {
			t.Fatalf("disabled cliffs placed %d cells", len(placed))
		}
	}
}

func TestFactorioCliffPixelSpanScalesInWorldTiles(t *testing.T) {
	tests := []struct {
		tilesPerPixel float64
		wantMin       int
		wantMax       int
	}{
		{tilesPerPixel: 0.4, wantMin: 20, wantMax: 32},
		{tilesPerPixel: 1, wantMin: 8, wantMax: 12},
		{tilesPerPixel: 2, wantMin: 4, wantMax: 6},
		{tilesPerPixel: 2.5, wantMin: 4, wantMax: 5},
		{tilesPerPixel: 4, wantMin: 2, wantMax: 3},
	}
	for _, test := range tests {
		minPixel, maxPixel := factorioCliffPixelSpan(10.5, 0, test.tilesPerPixel)
		if minPixel != test.wantMin || maxPixel != test.wantMax {
			t.Errorf("span at %g tiles/pixel = [%d,%d], want [%d,%d]", test.tilesPerPixel, minPixel, maxPixel, test.wantMin, test.wantMax)
		}
	}
}

func TestFactorioCliffsAllowSolidContinuity(t *testing.T) {
	settings := defaultFactorioNaturalSettings(123456)
	settings.cliffRichness = 10
	evaluator := newFactorioCliffEvaluator(settings, newFactorioNauvisEvaluator(settings))
	if math.IsNaN(evaluator.cutoff) || math.IsInf(evaluator.cutoff, 0) || evaluator.cutoff != 0 {
		t.Fatalf("solid-continuity cutoff = %g, want 0", evaluator.cutoff)
	}
	if _, err := evaluator.placedCells(context.Background(), -64, -64, 64, 64); err != nil {
		t.Fatalf("place solid-continuity cliffs: %v", err)
	}
}

func positiveMod(value, divisor float64) float64 {
	return math.Mod(math.Mod(value, divisor)+divisor, divisor)
}
