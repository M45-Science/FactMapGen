package main

import (
	"encoding/json"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

type factorioTerrainOracle struct {
	Seed      uint32 `json:"seed0"`
	Positions []struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"positions"`
	TileNames []string `json:"tileNames"`
}

func TestFactorioTerrainTilesMatchReferenceOracles(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "terrain-oracles", "oracle-tile-names.*.json"))
	if err != nil {
		t.Fatalf("find terrain oracle fixtures: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("terrain oracle fixture count = %d, want 3", len(paths))
	}

	total := 0
	matches := 0
	for _, path := range paths {
		fixture := readFactorioTerrainOracle(t, path)
		if len(fixture.Positions) != len(fixture.TileNames) {
			t.Fatalf("%s has %d positions and %d tile names", path, len(fixture.Positions), len(fixture.TileNames))
		}
		evaluator := newFactorioNauvisEvaluator(defaultFactorioTerrainSettings(fixture.Seed))
		for i, point := range fixture.Positions {
			sample := evaluator.sample(point.X, point.Y)
			got := evaluator.terrainTile(sample, point.X, point.Y).name
			total++
			if got == fixture.TileNames[i] {
				matches++
			} else {
				t.Logf("%s (%g,%g): got %s, want %s", filepath.Base(path), point.X, point.Y, got, fixture.TileNames[i])
			}
		}
	}

	matchPercent := float64(matches) * 100 / float64(total)
	if matchPercent < 90 {
		t.Fatalf("terrain oracle agreement = %d/%d (%.1f%%), want at least 90%%", matches, total, matchPercent)
	}
	t.Logf("terrain oracle agreement = %d/%d (%.1f%%)", matches, total, matchPercent)
}

func TestFactorioTerrainCatalogHasStableNamesAndColors(t *testing.T) {
	if len(factorioTerrainTiles) != 21 || len(factorioTerrainNoiseSeeds) != 19 {
		t.Fatalf("terrain catalog has %d tiles and %d land noise seeds", len(factorioTerrainTiles), len(factorioTerrainNoiseSeeds))
	}
	seen := make(map[string]bool, len(factorioTerrainTiles))
	for i, tile := range factorioTerrainTiles {
		if tile.name == "" || seen[tile.name] {
			t.Fatalf("terrain tile %d has empty or duplicate name %q", i, tile.name)
		}
		seen[tile.name] = true
		if tile.color.A != 255 {
			t.Fatalf("terrain tile %s alpha = %d, want 255", tile.name, tile.color.A)
		}
		if tile.water != (i < 2) {
			t.Fatalf("terrain tile %s water = %v at index %d", tile.name, tile.water, i)
		}
	}
	if factorioTerrainTiles[0].color != (color.RGBA{R: 38, G: 64, B: 73, A: 255}) {
		t.Fatalf("deepwater color = %#v", factorioTerrainTiles[0].color)
	}
}

func readFactorioTerrainOracle(t *testing.T, path string) factorioTerrainOracle {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read terrain oracle %s: %v", path, err)
	}
	var fixture factorioTerrainOracle
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode terrain oracle %s: %v", path, err)
	}
	return fixture
}

func defaultFactorioTerrainSettings(seed uint32) fastPreviewSettings {
	return fastPreviewSettings{
		seed:                          seed,
		mapType:                       "nauvis",
		water:                         fastControl{frequency: 1, size: 1, richness: 1, enabled: true},
		moistureFrequency:             1,
		auxFrequency:                  1,
		temperatureFreq:               1,
		startingAreaMoistureSize:      1,
		startingAreaMoistureFrequency: 1,
		startingPositions:             []factorioPoint{{}},
	}
}
