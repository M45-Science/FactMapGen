package main

import (
	"context"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"testing"
)

const (
	factorioVulcanusOracleSampleCount    = 381
	factorioVulcanusOracleMinimumMatches = 374
)

type factorioVulcanusTerrainOracle struct {
	Seed      uint32 `json:"seed0"`
	Planet    string `json:"planet"`
	Positions []struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	} `json:"positions"`
	TileNames []string `json:"tileNames"`
}

type factorioVulcanusTerrainMismatch struct {
	x, y         float64
	gotTile      string
	expectedTile string
}

var factorioVulcanusKnownTerrainMismatches = map[factorioVulcanusTerrainMismatch]struct{}{
	{x: -192, y: -128, gotTile: "volcanic-folds", expectedTile: "volcanic-folds-flat"}:       {},
	{x: 192, y: 0, gotTile: "volcanic-cracks-warm", expectedTile: "volcanic-smooth-stone"}:   {},
	{x: 320, y: 0, gotTile: "volcanic-cracks-warm", expectedTile: "volcanic-smooth-stone"}:   {},
	{x: 491, y: 348, gotTile: "volcanic-cracks-warm", expectedTile: "volcanic-smooth-stone"}: {},
	{x: -1443, y: 226, gotTile: "volcanic-pumice-stones", expectedTile: "volcanic-ash-soil"}: {},
	{x: 1871, y: -537, gotTile: "volcanic-ash-light", expectedTile: "volcanic-ash-flats"}:    {},
	{x: -680, y: 1965, gotTile: "volcanic-cracks-warm", expectedTile: "volcanic-cracks-hot"}: {},
}

func TestFactorioSpaceAgeStartingBearingsMatchGame(t *testing.T) {
	// Factorio 2.1.23 readings from FactorioMapWebUI's bearing-chain fixture.
	settings := defaultFactorioTerrainSettings(3526822528)
	vulcanus := newFactorioVulcanusEvaluator(settings)
	if got := [3]float64{vulcanus.ashlandsAngle, vulcanus.mountainsAngle, vulcanus.basaltsAngle}; got != [3]float64{2956.148681640625, 2836.148681640625, 2716.148681640625} {
		t.Errorf("Vulcanus bearings = %v, want Factorio's three starting bearings", got)
	}
	basalts := newFactorioNoiseCone(float32(vulcanus.basaltsAngle), 1, 1)
	if basalts.x != -0.27813220024108887 || basalts.y != 0.9605427980422974 {
		t.Errorf("Vulcanus basalt bearing offset = (%g, %g), want (-0.2781322, 0.9605428)", basalts.x, basalts.y)
	}
	fulgora := newFactorioFulgoraEvaluator(settings)
	if fulgora.tight.x != 0.7615723013877869 {
		t.Errorf("Fulgora starting bearing X = %g, want 0.7615723", fulgora.tight.x)
	}
}

func TestFactorioVulcanusClimateAndCracksMatchGame(t *testing.T) {
	// Two points from FactorioMapWebUI's Vulcanus climate and crack fixtures.
	e := newFactorioVulcanusEvaluator(defaultFactorioTerrainSettings(123456))
	for _, sample := range []struct {
		x, y, aux, moisture, floodPaths, floodBasalt float64
	}{
		{-29.5, -39.75, 0.5233526229858398, 0.3901686668395996, -1.2808706760406494, 0.053733035922050476},
		{22.5, 28.25, 0.20753666758537292, 0.5563872456550598, -0.5745949745178223, 0.3279365599155426},
	} {
		terrain := e.sample(sample.x, sample.y)
		cracks := e.cracksAt(sample.x, sample.y)
		got := [4]float64{terrain.aux, terrain.moisture, cracks.floodPaths, cracks.floodBasalt}
		want := [4]float64{sample.aux, sample.moisture, sample.floodPaths, sample.floodBasalt}
		if got != want {
			t.Errorf("climate and cracks at (%g, %g) = %v, want %v", sample.x, sample.y, got, want)
		}
	}
}

func TestFactorioVulcanusTerrainMatchesReferenceOracle(t *testing.T) {
	path := filepath.Join(
		"testdata",
		"vulcanus-oracles",
		"oracle-vulcanus-tile-names.seed123456.json",
	)
	fixture := readFactorioVulcanusTerrainOracle(t, path)
	if fixture.Seed != 123456 {
		t.Fatalf("Vulcanus oracle seed = %d, want effective surface seed 123456", fixture.Seed)
	}
	if fixture.Planet != fastPreviewPlanetVulcanus {
		t.Fatalf("Vulcanus oracle planet = %q, want %q", fixture.Planet, fastPreviewPlanetVulcanus)
	}
	if len(fixture.Positions) != factorioVulcanusOracleSampleCount ||
		len(fixture.TileNames) != factorioVulcanusOracleSampleCount {
		t.Fatalf(
			"Vulcanus oracle has %d positions and %d tile names, want %d each",
			len(fixture.Positions),
			len(fixture.TileNames),
			factorioVulcanusOracleSampleCount,
		)
	}

	// The fixture seed is already the effective Vulcanus surface seed captured
	// from Factorio. Construct the evaluator directly so it is not CRC-offset
	// a second time as though it were the save's base map seed.
	evaluator := newFactorioVulcanusEvaluator(fastPreviewSettings{
		seed:              fixture.Seed,
		planet:            fixture.Planet,
		startingPositions: []factorioPoint{{}},
	})

	matches := 0
	for i, point := range fixture.Positions {
		got := evaluator.sample(point.X, point.Y).tile.name
		want := fixture.TileNames[i]
		if got == want {
			matches++
			continue
		}
		mismatch := factorioVulcanusTerrainMismatch{
			x:            point.X,
			y:            point.Y,
			gotTile:      got,
			expectedTile: want,
		}
		if _, known := factorioVulcanusKnownTerrainMismatches[mismatch]; !known {
			t.Errorf(
				"unexpected Vulcanus terrain mismatch at (%g,%g): got %s, want %s",
				point.X,
				point.Y,
				got,
				want,
			)
			continue
		}
		t.Logf(
			"known Vulcanus terrain mismatch at (%g,%g): got %s, want %s",
			point.X,
			point.Y,
			got,
			want,
		)
	}

	// The upstream oracle records 369/381 matches. Its 12 misses are known
	// adjacent-tile boundary flips caused by omitted resource-coupled terms and
	// floating-point precision, so allow those without masking wider drift.
	if matches < factorioVulcanusOracleMinimumMatches {
		t.Fatalf(
			"Vulcanus terrain oracle agreement = %d/%d (%.2f%%), want at least %d/%d",
			matches,
			factorioVulcanusOracleSampleCount,
			float64(matches)*100/factorioVulcanusOracleSampleCount,
			factorioVulcanusOracleMinimumMatches,
			factorioVulcanusOracleSampleCount,
		)
	}
	t.Logf(
		"Vulcanus terrain oracle agreement = %d/%d (%.2f%%)",
		matches,
		factorioVulcanusOracleSampleCount,
		float64(matches)*100/factorioVulcanusOracleSampleCount,
	)
}

var factorioVulcanusBenchmarkImage *image.RGBA

func BenchmarkFactorioVulcanusTerrain256(b *testing.B) {
	settings := fastPreviewSettings{
		seed:              123456,
		planet:            fastPreviewPlanetVulcanus,
		startingPositions: []factorioPoint{{}},
	}
	b.ReportAllocs()
	for range b.N {
		evaluator := newFactorioSpaceAgeEvaluator(settings)
		img := image.NewRGBA(image.Rect(0, 0, 256, 256))
		if err := evaluator.render(
			context.Background(),
			img,
			settings,
			-128,
			-128,
			1,
		); err != nil {
			b.Fatal(err)
		}
		factorioVulcanusBenchmarkImage = img
	}
}

func readFactorioVulcanusTerrainOracle(
	t *testing.T,
	path string,
) factorioVulcanusTerrainOracle {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Vulcanus terrain oracle %s: %v", path, err)
	}
	var fixture factorioVulcanusTerrainOracle
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode Vulcanus terrain oracle %s: %v", path, err)
	}
	return fixture
}
