package main

import (
	"context"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"
)

type factorioTreeOracle struct {
	Seed      uint32               `json:"seed0"`
	Positions []naturalOraclePoint `json:"positions"`
	Values    map[string][]float64 `json:"values"`
}

type factorioTreeControlOracle struct {
	Seed           uint32               `json:"seed0"`
	TreesFrequency float64              `json:"treesFrequency"`
	TreesSize      float64              `json:"treesSize"`
	Positions      []naturalOraclePoint `json:"positions"`
	Values         map[string][]float64 `json:"values"`
}

type naturalOraclePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func TestFactorioTreeFieldsMatchReferenceOracle(t *testing.T) {
	fixture := readNaturalOracle[factorioTreeOracle](t, "oracle-trees.seed123456.json")
	settings := defaultFactorioNaturalSettings(fixture.Seed)
	evaluator := newFactorioTreeEvaluator(settings, newFactorioNauvisEvaluator(settings))

	worstSmall := 0.0
	worstCutout := 0.0
	for index, position := range fixture.Positions {
		point := evaluator.pointAt(position.X, position.Y)
		worstSmall = math.Max(worstSmall, math.Abs(point.smallNoise-fixture.Values["tree_small_noise"][index]))
		worstCutout = math.Max(worstCutout, math.Abs(point.cutoutFaded-fixture.Values["trees_forest_path_cutout_faded"][index]))
	}
	if worstSmall >= 1e-3 || worstCutout >= 1e-3 {
		t.Fatalf("tree shared oracle residuals: small=%g cutout=%g, want both < 1e-3", worstSmall, worstCutout)
	}

	for speciesIndex, field := range evaluator.fields {
		want, ok := fixture.Values[field.species.name]
		if !ok {
			t.Fatalf("tree oracle is missing %s", field.species.name)
		}
		worst := 0.0
		for index, position := range fixture.Positions {
			got := evaluator.speciesValueAt(speciesIndex, position.X, position.Y)
			worst = math.Max(worst, math.Abs(got-want[index]))
		}
		if worst >= 1e-3 {
			t.Errorf("%s oracle residual = %g, want < 1e-3", field.species.name, worst)
		}
	}
}

func TestFactorioTreeDensityMatchesOracleComposition(t *testing.T) {
	fixture := readNaturalOracle[factorioTreeOracle](t, "oracle-trees.seed123456.json")
	settings := defaultFactorioNaturalSettings(fixture.Seed)
	evaluator := newFactorioTreeEvaluator(settings, newFactorioNauvisEvaluator(settings))
	for index, position := range fixture.Positions {
		miss := 1.0
		for _, species := range factorioTreeSpeciesCatalog {
			probability := clampFloat(fixture.Values[species.name][index], 0, 1)
			miss *= 1 - probability
		}
		want := 1 - miss
		got := evaluator.densityAt(position.X, position.Y)
		delta := math.Abs(got - want)
		if delta >= 2e-4 && delta >= 1e-2*math.Abs(want) {
			t.Errorf("tree density at (%g,%g) = %.12g, want %.12g (delta %g)", position.X, position.Y, got, want, delta)
		}
	}
}

func TestFactorioTreeControlsMatchReferenceOracle(t *testing.T) {
	fixture := readNaturalOracle[factorioTreeControlOracle](t, "oracle-trees-controls.seed123456.json")
	settings := defaultFactorioNaturalSettings(fixture.Seed)
	settings.trees.frequency = fixture.TreesFrequency
	settings.trees.size = fixture.TreesSize
	evaluator := newFactorioTreeEvaluator(settings, newFactorioNauvisEvaluator(settings))
	for name, want := range fixture.Values {
		speciesIndex := -1
		for index, field := range evaluator.fields {
			if field.species.name == name {
				speciesIndex = index
				break
			}
		}
		if speciesIndex < 0 {
			t.Fatalf("unknown tree species %s in control oracle", name)
		}
		worst := 0.0
		for index, position := range fixture.Positions {
			got := evaluator.speciesValueAt(speciesIndex, position.X, position.Y)
			worst = math.Max(worst, math.Abs(got-want[index]))
		}
		if worst >= 1e-3 {
			t.Errorf("%s control oracle residual = %g, want < 1e-3", name, worst)
		}
	}
}

func TestRenderFactorioTreesUsesChartBlendAndLeavesWater(t *testing.T) {
	settings := defaultFactorioNaturalSettings(123456)
	nauvis := newFactorioNauvisEvaluator(settings)
	evaluator := newFactorioTreeEvaluator(settings, nauvis)
	baseColor := color.RGBA{R: 99, G: 122, B: 44, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, baseColor)
		}
	}
	if err := renderFactorioTrees(context.Background(), img, settings, evaluator, 0, 0, 1); err != nil {
		t.Fatalf("render trees: %v", err)
	}

	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			alpha := 0.0
			if evaluator.placedAt(float64(x), float64(y)) {
				alpha = factorioTreeMaxAlpha
			}
			alphaByte := int(math.Round(alpha * 255))
			blend := alphaByte + (alphaByte >> 7)
			want := color.RGBA{
				R: uint8(((256-blend)*int(baseColor.R) + blend*int(factorioTreeMapColor.R)) >> 8),
				G: uint8(((256-blend)*int(baseColor.G) + blend*int(factorioTreeMapColor.G)) >> 8),
				B: uint8(((256-blend)*int(baseColor.B) + blend*int(factorioTreeMapColor.B)) >> 8),
				A: 255,
			}
			if got := img.RGBAAt(x, y); got != want {
				t.Fatalf("tree blend at (%d,%d) = %#v, want %#v", x, y, got, want)
			}
		}
	}

	water := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			water.SetRGBA(x, y, factorioTerrainTiles[1].color)
		}
	}
	if err := renderFactorioTrees(context.Background(), water, settings, evaluator, 0, 0, 1); err != nil {
		t.Fatalf("render trees over water: %v", err)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if got := water.RGBAAt(x, y); got != factorioTerrainTiles[1].color {
				t.Fatalf("water changed at (%d,%d): %#v", x, y, got)
			}
		}
	}
}

func TestFactorioTreeCatalogIsComplete(t *testing.T) {
	if len(factorioTreeSpeciesCatalog) != 15 {
		t.Fatalf("tree species count = %d, want 15", len(factorioTreeSpeciesCatalog))
	}
	seen := make(map[string]bool, len(factorioTreeSpeciesCatalog))
	for _, species := range factorioTreeSpeciesCatalog {
		if species.name == "" || seen[species.name] {
			t.Fatalf("empty or duplicate tree species %q", species.name)
		}
		seen[species.name] = true
		wantOffset := 0.5
		if species.name == "tree_05" || species.name == "tree_07" {
			wantOffset = 0.45
		}
		if species.sizeOffset != wantOffset {
			t.Errorf("%s size offset = %g, want %g", species.name, species.sizeOffset, wantOffset)
		}
	}
}

func defaultFactorioNaturalSettings(seed uint32) fastPreviewSettings {
	settings := defaultFactorioTerrainSettings(seed)
	settings.trees = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	settings.cliffs = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	settings.cliffElevation0 = 10
	settings.cliffElevationInterval = 40
	settings.cliffRichness = 1
	return settings
}

func readNaturalOracle[T any](t *testing.T, name string) T {
	t.Helper()
	path := filepath.Join("testdata", "natural-oracles", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read natural oracle %s: %v", path, err)
	}
	var fixture T
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode natural oracle %s: %v", path, err)
	}
	return fixture
}
