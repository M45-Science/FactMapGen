package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

func defaultFactorioEnemySettings(seed uint32) fastPreviewSettings {
	settings := defaultFactorioTerrainSettings(seed)
	settings.startingArea = 1
	settings.enemyBases = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	settings.noEnemies = false
	return settings
}

func TestFactorioEnemyExpressionParameters(t *testing.T) {
	settings := defaultFactorioEnemySettings(123456)
	evaluator := newFactorioEnemyEvaluator(settings)
	if evaluator.startingAreaRadius != 150 {
		t.Fatalf("starting area radius = %g, want 150", evaluator.startingAreaRadius)
	}
	for _, test := range []struct {
		distance      float64
		wantIntensity float64
		wantRadius    float64
		wantFrequency float64
	}{
		{distance: 0, wantIntensity: 0, wantRadius: 15, wantFrequency: 0.00001},
		{distance: 325, wantIntensity: 1, wantRadius: 19, wantFrequency: 0.000013},
		{distance: 2400, wantIntensity: 2400.0 / 325.0, wantRadius: 15 + 4*2400.0/325.0, wantFrequency: 0.00001 + 0.000003*2400.0/325.0},
		{distance: 4800, wantIntensity: 2400.0 / 325.0, wantRadius: 15 + 4*2400.0/325.0, wantFrequency: 0.00001 + 0.000003*2400.0/325.0},
	} {
		if got := evaluator.intensityAtDistance(test.distance); math.Abs(got-test.wantIntensity) > 1e-12 {
			t.Errorf("intensity at %g = %.12g, want %.12g", test.distance, got, test.wantIntensity)
		}
		if got := evaluator.radiusAtDistance(test.distance); math.Abs(got-test.wantRadius) > 1e-12 {
			t.Errorf("radius at %g = %.12g, want %.12g", test.distance, got, test.wantRadius)
		}
		if got := evaluator.frequencyAtDistance(test.distance); math.Abs(got-test.wantFrequency) > 1e-15 {
			t.Errorf("frequency at %g = %.12g, want %.12g", test.distance, got, test.wantFrequency)
		}
	}
	for _, test := range []struct {
		distance float64
		want     int
	}{
		{distance: 0, want: 1},
		{distance: 974, want: 1},
		{distance: 975, want: 2},
		{distance: 1949, want: 2},
		{distance: 1950, want: 3},
		{distance: 4800, want: 3},
	} {
		if got := evaluator.populationSelectionsAtDistance(test.distance); got != test.want {
			t.Errorf("population selections at %g = %d, want %d", test.distance, got, test.want)
		}
	}
}

func TestFactorioEnemyRegionSpotsAreDeterministicAndControlled(t *testing.T) {
	settings := defaultFactorioEnemySettings(123456)
	a := newFactorioEnemyEvaluator(settings)
	b := newFactorioEnemyEvaluator(settings)
	for regionY := int64(-1); regionY <= 1; regionY++ {
		for regionX := int64(-1); regionX <= 1; regionX++ {
			got := a.regionSpots(regionX, regionY)
			want := b.regionSpots(regionX, regionY)
			if len(got) != len(want) {
				t.Fatalf("region (%d,%d) spot counts = %d and %d", regionX, regionY, len(got), len(want))
			}
			for index := range got {
				if got[index] != want[index] {
					t.Fatalf("region (%d,%d) spot %d differs: %#v vs %#v", regionX, regionY, index, got[index], want[index])
				}
			}
		}
	}

	high := settings
	high.enemyBases.frequency = 3
	highEvaluator := newFactorioEnemyEvaluator(high)
	normalCount := len(a.regionSpots(1, 1))
	highCount := len(highEvaluator.regionSpots(1, 1))
	if highCount <= normalCount {
		t.Fatalf("high-frequency region has %d spots, normal has %d", highCount, normalCount)
	}

	large := settings
	large.enemyBases.size = 4
	largeEvaluator := newFactorioEnemyEvaluator(large)
	if got, want := largeEvaluator.radiusAtDistance(325), 2*a.radiusAtDistance(325); got != want {
		t.Fatalf("size=4 radius = %g, want %g", got, want)
	}
}

func TestFactorioEnemyRandomPenaltyMatchesSequentialStream(t *testing.T) {
	for _, point := range [][2]int64{{0, 0}, {31, 31}, {32, -1}, {-37, 68}, {106, -205}} {
		x, y := point[0], point[1]
		chunkX := factorioFloorDiv(x, factorioChunkSize)
		chunkY := factorioFloorDiv(y, factorioChunkSize)
		originX := chunkX * factorioChunkSize
		originY := chunkY * factorioChunkSize
		word := uint32(factorioSpotSeedBase) +
			uint32(int32(originX))*factorioSpotRegionXPrime +
			uint32(int32(originY+1))*factorioSpotRegionYPrime
		if word < factorioMinSeedWord {
			word = factorioMinSeedWord
		}
		index := (y-originY)*factorioChunkSize + x - originX
		draw := int(factorioChunkSize*factorioChunkSize - 1 - index)
		state := newFactorioTaus88State(word)
		var random uint32
		for step := 0; step <= draw; step++ {
			random = state.next()
		}
		want := 0.25 - 0.1*float64(random)/4294967296.0
		if got := factorioRandomPenaltyAt(x, y, 0.25, 0.1); got != want {
			t.Fatalf("random penalty at (%d,%d) = %.17g, want %.17g", x, y, got, want)
		}
	}
}

func TestRenderFactorioEnemiesDeterministicWithStartingArea(t *testing.T) {
	settings := defaultFactorioEnemySettings(123456)
	evaluator := newFactorioEnemyEvaluator(settings)
	render := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
		for y := 0; y < 1024; y++ {
			for x := 0; x < 1024; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 66, G: 57, B: 15, A: 255})
			}
		}
		if err := renderFactorioEnemies(context.Background(), img, settings, evaluator, -512, -512, 1); err != nil {
			t.Fatal(err)
		}
		return img
	}
	first := render()
	second := render()
	enemyPixels := 0
	nearest := math.Inf(1)
	for y := 0; y < 1024; y++ {
		for x := 0; x < 1024; x++ {
			if first.RGBAAt(x, y) != second.RGBAAt(x, y) {
				t.Fatalf("render differs at (%d,%d)", x, y)
			}
			if first.RGBAAt(x, y) != factorioEnemyMapColor {
				continue
			}
			enemyPixels++
			nearest = min(nearest, math.Hypot(float64(x-512), float64(y-512)))
		}
	}
	if enemyPixels == 0 {
		t.Fatal("enemy render is empty")
	}
	if nearest < 175 || nearest > 240 {
		t.Fatalf("nearest enemy radius = %.1f, want 175..240", nearest)
	}

	disabled := settings
	disabled.noEnemies = true
	blank := image.NewRGBA(image.Rect(0, 0, 32, 32))
	if err := renderFactorioEnemies(context.Background(), blank, disabled, evaluator, -16, -16, 1); err != nil {
		t.Fatal(err)
	}
	for _, value := range blank.Pix {
		if value != 0 {
			t.Fatal("disabled enemy renderer changed its image")
		}
	}
}

func TestRenderFactorioEnemyOverviewPanIsStable(t *testing.T) {
	settings := defaultFactorioEnemySettings(123456)
	evaluator := newFactorioEnemyEvaluator(settings)
	landImage := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 256, 256))
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 66, G: 57, B: 15, A: 255})
			}
		}
		return img
	}
	const tilesPerPixel = 8.0
	left := landImage()
	if err := renderFactorioEnemies(
		context.Background(), left, settings, evaluator,
		-1024, -1024, tilesPerPixel,
	); err != nil {
		t.Fatal(err)
	}
	right := landImage()
	if err := renderFactorioEnemies(
		context.Background(), right, settings, evaluator,
		-960, -1024, tilesPerPixel,
	); err != nil {
		t.Fatal(err)
	}
	found := false
	for y := 0; y < 256; y++ {
		for x := 0; x < 248; x++ {
			got := right.RGBAAt(x, y)
			want := left.RGBAAt(x+8, y)
			if got != want {
				t.Fatalf("panned overview pixel %d,%d = %#v, want %#v", x, y, got, want)
			}
			found = found || got == factorioEnemyMapColor
		}
	}
	if !found {
		t.Fatal("overlapping overview contains no enemies")
	}
}
