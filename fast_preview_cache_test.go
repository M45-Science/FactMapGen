package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"math"
	"runtime"
	"sync"
	"testing"
	"time"
)

func assertFastPreviewImagesEqual(t *testing.T, got, want *image.RGBA) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("image bounds = %v, want %v", got.Bounds(), want.Bounds())
	}
	if bytes.Equal(got.Pix, want.Pix) {
		return
	}
	width := want.Bounds().Dx()
	for offset := 0; offset < len(want.Pix); offset += 4 {
		if bytes.Equal(got.Pix[offset:offset+4], want.Pix[offset:offset+4]) {
			continue
		}
		x := (offset / 4) % width
		y := (offset / 4) / width
		t.Fatalf("pixel %d,%d = %v, want %v", x, y, got.Pix[offset:offset+4], want.Pix[offset:offset+4])
	}
	t.Fatal("image pixel storage differs")
}

func waitForFastPreviewCondition(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		runtime.Gosched()
		time.Sleep(time.Millisecond)
	}
}

func TestFastPreviewTileCacheMatchesDirectRenderer(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	const size = 192
	for _, tilesPerPixel := range []float64{0.4, 1, 2, 2.75, 8} {
		t.Run(formatPreviewCenter(tilesPerPixel), func(t *testing.T) {
			centerX := 37 * tilesPerPixel
			centerY := -51 * tilesPerPixel
			direct, err := newFastPreviewWorld(settings).render(
				context.Background(), size, tilesPerPixel, centerX, centerY,
			)
			if err != nil {
				t.Fatalf("direct render: %v", err)
			}

			cache := newFastPreviewCache(8<<20, 2)
			key := fastPreviewWorldKey{byte(math.Float64bits(tilesPerPixel))}
			cached, err := cache.render(
				context.Background(), key, settings, size, tilesPerPixel, centerX, centerY,
			)
			if err != nil {
				t.Fatalf("cached render: %v", err)
			}
			assertFastPreviewImagesEqual(t, cached, direct)
		})
	}
}

func TestFastPreviewTileCacheAlignedStandardColdAndWarm(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	const size = 1024
	const tilesPerPixel = 1.0
	direct, err := newFastPreviewWorld(settings).render(
		context.Background(), size, tilesPerPixel, 0, 0,
	)
	if err != nil {
		t.Fatalf("direct render: %v", err)
	}

	cache := newFastPreviewCache(16<<20, 2)
	key := fastPreviewWorldKey{0x31}
	cold, err := cache.render(context.Background(), key, settings, size, tilesPerPixel, 0, 0)
	if err != nil {
		t.Fatalf("cold cached render: %v", err)
	}
	assertFastPreviewImagesEqual(t, cold, direct)
	coldStats := cache.stats()
	if coldStats.Misses != 64 || coldStats.Tiles != 64 {
		t.Fatalf("cold aligned cache stats = %#v, want 64 misses and tiles", coldStats)
	}

	warm, err := cache.render(context.Background(), key, settings, size, tilesPerPixel, 0, 0)
	if err != nil {
		t.Fatalf("warm cached render: %v", err)
	}
	assertFastPreviewImagesEqual(t, warm, direct)
	warmStats := cache.stats()
	if warmStats.Misses != coldStats.Misses || warmStats.Hits-coldStats.Hits != 64 {
		t.Fatalf("warm aligned cache stats = %#v after %#v, want 64 hits and no misses", warmStats, coldStats)
	}
}

func TestFastPreviewTileCacheMinimumScaleHistorySeams(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	const size = 256
	const tilesPerPixel = minPreviewTilesPerPixel
	centerX := float64(64) * tilesPerPixel
	centerY := float64(-64) * tilesPerPixel
	if padding := fastPreviewTileHistoryPadding(tilesPerPixel); padding != 64 {
		t.Fatalf("minimum-scale history padding = %d, want 64", padding)
	}
	direct, err := newFastPreviewWorld(settings).render(
		context.Background(), size, tilesPerPixel, centerX, centerY,
	)
	if err != nil {
		t.Fatalf("direct render: %v", err)
	}

	cache := newFastPreviewCache(16<<20, 2)
	key := fastPreviewWorldKey{0x32}
	for _, phase := range []string{"cold", "warm"} {
		cached, err := cache.render(
			context.Background(), key, settings, size, tilesPerPixel, centerX, centerY,
		)
		if err != nil {
			t.Fatalf("%s cached render: %v", phase, err)
		}
		assertFastPreviewImagesEqual(t, cached, direct)
	}
}

func TestFastPreviewTileCacheOddSizeFallsBackExactly(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	const size = 257
	direct, err := newFastPreviewWorld(settings).render(context.Background(), size, 1, 0, 0)
	if err != nil {
		t.Fatalf("direct render: %v", err)
	}

	cache := newFastPreviewCache(8<<20, 2)
	cached, err := cache.render(
		context.Background(), fastPreviewWorldKey{0x33}, settings, size, 1, 0, 0,
	)
	if err != nil {
		t.Fatalf("cached fallback render: %v", err)
	}
	assertFastPreviewImagesEqual(t, cached, direct)
	stats := cache.stats()
	if stats.Tiles != 0 || stats.Hits != 0 || stats.Misses != 0 {
		t.Fatalf("odd fallback populated tile cache: %#v", stats)
	}
}

func TestFastPreviewTileCacheReusesAdjacentPan(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	cache := newFastPreviewCache(8<<20, 2)
	key := fastPreviewWorldKey{1}
	ctx := context.Background()

	firstImage, err := cache.render(ctx, key, settings, 256, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := newFastPreviewWorld(settings).render(ctx, 256, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertFastPreviewImagesEqual(t, firstImage, direct)
	first := cache.stats()
	if first.Misses != 4 || first.Tiles != 4 || first.Bytes != 4*fastPreviewTileEdge*fastPreviewTileEdge*4 {
		t.Fatalf("first render stats = %#v, want four cached tiles", first)
	}
	repeatedImage, err := cache.render(ctx, key, settings, 256, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertFastPreviewImagesEqual(t, repeatedImage, direct)
	repeated := cache.stats()
	if repeated.Misses != first.Misses || repeated.Hits-first.Hits != 4 {
		t.Fatalf("repeat stats = %#v after %#v, want four hits and no misses", repeated, first)
	}
	pannedImage, err := cache.render(ctx, key, settings, 256, 1, 64, 0)
	if err != nil {
		t.Fatal(err)
	}
	directPan, err := newFastPreviewWorld(settings).render(ctx, 256, 1, 64, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertFastPreviewImagesEqual(t, pannedImage, directPan)
	panned := cache.stats()
	if panned.Misses-repeated.Misses != 2 || panned.Hits-repeated.Hits != 4 {
		t.Fatalf("pan stats = %#v after %#v, want four reused and two new tiles", panned, repeated)
	}

	negativePanImage, err := cache.render(ctx, key, settings, 256, 1, -64, 0)
	if err != nil {
		t.Fatal(err)
	}
	directNegativePan, err := newFastPreviewWorld(settings).render(ctx, 256, 1, -64, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertFastPreviewImagesEqual(t, negativePanImage, directNegativePan)
	negativePan := cache.stats()
	if negativePan.Misses-panned.Misses != 2 || negativePan.Hits-panned.Hits != 4 {
		t.Fatalf("negative pan stats = %#v after %#v, want four reused and two new tiles", negativePan, panned)
	}
}

func TestFastPreviewTileCacheHonorsByteAndWorldBounds(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	tileBytes := int64(fastPreviewTileEdge * fastPreviewTileEdge * 4)
	cache := newFastPreviewCache(2*tileBytes, 2)
	ctx := context.Background()

	for index := byte(1); index <= 3; index++ {
		worldSettings := settings
		worldSettings.seed += uint32(index)
		if _, err := cache.render(
			ctx, fastPreviewWorldKey{index}, worldSettings, 128, 1, 0, 0,
		); err != nil {
			t.Fatal(err)
		}
	}
	stats := cache.stats()
	if stats.Worlds != 2 {
		t.Fatalf("cached worlds = %d, want 2", stats.Worlds)
	}
	if stats.Bytes > 2*tileBytes || stats.Tiles > 2 {
		t.Fatalf("cache exceeded byte bound: %#v", stats)
	}
	if stats.Evictions == 0 {
		t.Fatalf("cache stats = %#v, want tile evictions", stats)
	}
}

func TestFastPreviewCacheUsesEphemeralWorldWhenAllRetainedWorldsActive(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	cache := newFastPreviewCache(8<<20, 1)
	firstKey := fastPreviewWorldKey{0x41}
	secondKey := fastPreviewWorldKey{0x42}
	first := cache.acquireWorld(firstKey, settings)
	if !first.retained {
		t.Fatal("first world was not retained")
	}

	secondSettings := settings
	secondSettings.seed++
	direct, err := newFastPreviewWorld(secondSettings).render(context.Background(), 128, 1, 0, 0)
	if err != nil {
		cache.releaseWorld(first)
		t.Fatalf("direct render: %v", err)
	}
	ephemeralImage, err := cache.render(
		context.Background(), secondKey, secondSettings, 128, 1, 0, 0,
	)
	if err != nil {
		cache.releaseWorld(first)
		t.Fatalf("ephemeral render: %v", err)
	}
	assertFastPreviewImagesEqual(t, ephemeralImage, direct)
	stats := cache.stats()
	if stats.Worlds != 1 || stats.Tiles != 0 || stats.Hits != 0 || stats.Misses != 0 {
		cache.releaseWorld(first)
		t.Fatalf("ephemeral render changed retained cache state: %#v", stats)
	}
	cache.mu.Lock()
	_, hasFirst := cache.worlds[firstKey]
	_, hasSecond := cache.worlds[secondKey]
	cache.mu.Unlock()
	if !hasFirst || hasSecond {
		cache.releaseWorld(first)
		t.Fatalf("retained worlds after ephemeral render: first=%v second=%v", hasFirst, hasSecond)
	}

	ephemeral := cache.acquireWorld(secondKey, secondSettings)
	if ephemeral.retained {
		cache.releaseWorld(ephemeral)
		cache.releaseWorld(first)
		t.Fatal("distinct world was retained while the only slot was pinned")
	}
	cache.releaseWorld(ephemeral)
	cache.releaseWorld(first)

	second := cache.acquireWorld(secondKey, secondSettings)
	if !second.retained {
		cache.releaseWorld(second)
		t.Fatal("distinct world was not retained after the old world became evictable")
	}
	cache.releaseWorld(second)
	cache.mu.Lock()
	_, hasFirst = cache.worlds[firstKey]
	_, hasSecond = cache.worlds[secondKey]
	cache.mu.Unlock()
	if hasFirst || !hasSecond || cache.stats().Worlds != 1 {
		t.Fatalf("retained-world replacement failed: first=%v second=%v stats=%#v", hasFirst, hasSecond, cache.stats())
	}
}

func TestFastPreviewCacheCanceledSameWorldWaiterReturnsPromptly(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	cache := newFastPreviewCache(8<<20, 2)
	key := fastPreviewWorldKey{0x43}
	entry := cache.acquireWorld(key, settings)
	if err := entry.acquireRender(context.Background()); err != nil {
		cache.releaseWorld(entry)
		t.Fatal(err)
	}
	defer func() {
		entry.releaseRender()
		cache.releaseWorld(entry)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := cache.render(ctx, key, settings, 128, 1, 0, 0)
		done <- err
	}()
	waitForFastPreviewCondition(t, func() bool {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		return entry.references == 2
	}, "same-world waiter did not reach the world gate")
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled same-world waiter returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled same-world waiter remained blocked")
	}
	cache.mu.Lock()
	references := entry.references
	cache.mu.Unlock()
	if references != 1 {
		t.Fatalf("world references after canceled waiter = %d, want 1", references)
	}
}

func TestFastPreviewCacheRenderAdmissionCancellation(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	cache := newFastPreviewCache(8<<20, 2)
	wantSlots := 1
	if runtime.GOMAXPROCS(0) >= 2*fastPreviewMaxWorkers {
		wantSlots = 2
	}
	if slots := cap(cache.renderGate); slots != wantSlots || slots > 2 {
		t.Fatalf("render admission slots = %d, want %d and at most 2", slots, wantSlots)
	}
	heldSlots := 0
	defer func() {
		for heldSlots > 0 {
			releaseFastPreviewGate(cache.renderGate)
			heldSlots--
		}
	}()
	for heldSlots < cap(cache.renderGate) {
		if err := acquireFastPreviewGate(context.Background(), cache.renderGate); err != nil {
			t.Fatal(err)
		}
		heldSlots++
	}

	key := fastPreviewWorldKey{0x44}
	entry := cache.acquireWorld(key, settings)
	cache.releaseWorld(entry)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := cache.render(ctx, key, settings, 128, 1, 0, 0)
		done <- err
	}()
	waitForFastPreviewCondition(t, func() bool {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		return entry.references == 1 && len(entry.renderGate) == 1
	}, "render did not acquire its world gate before waiting for admission")
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("admission waiter returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled admission waiter remained blocked")
	}
	if len(entry.renderGate) != 0 {
		t.Fatal("canceled admission waiter did not release its world gate")
	}
}

func TestFastPreviewCacheWorldEvictionRemovesTilesAndRegenerates(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	cache := newFastPreviewCache(8<<20, 1)
	firstKey := fastPreviewWorldKey{0x45}
	secondKey := fastPreviewWorldKey{0x46}
	firstImage, err := cache.render(context.Background(), firstKey, settings, 256, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	secondSettings := settings
	secondSettings.seed++
	if _, err := cache.render(context.Background(), secondKey, secondSettings, 256, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	wrongSecondTile := false
	for key := range cache.tiles {
		if key.world != secondKey {
			wrongSecondTile = true
			break
		}
	}
	cache.mu.Unlock()
	if wrongSecondTile {
		t.Fatal("first-world tile survived first-world eviction")
	}

	regenerated, err := cache.render(context.Background(), firstKey, settings, 256, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertFastPreviewImagesEqual(t, regenerated, firstImage)
	cache.mu.Lock()
	wrongFirstTile := false
	for key := range cache.tiles {
		if key.world != firstKey {
			wrongFirstTile = true
			break
		}
	}
	cache.mu.Unlock()
	stats := cache.stats()
	tileBytes := int64(fastPreviewTileEdge * fastPreviewTileEdge * 4)
	if wrongFirstTile || stats.Worlds != 1 || stats.Tiles != 4 || stats.Bytes != 4*tileBytes || stats.Misses != 12 {
		t.Fatalf("cache state after world regeneration = %#v, wrong tile=%v", stats, wrongFirstTile)
	}
}

func TestFastPreviewCacheConcurrentSameWorld(t *testing.T) {
	_, _, _, settings := benchmarkPreviewSettings()
	cache := newFastPreviewCache(8<<20, 2)
	key := fastPreviewWorldKey{9}
	const workers = 4
	results := make([][]byte, workers)
	errors := make([]error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for index := range workers {
		go func() {
			defer group.Done()
			img, err := cache.render(context.Background(), key, settings, 128, 1, 0, 0)
			errors[index] = err
			if err == nil {
				results[index] = append([]byte(nil), img.Pix...)
			}
		}()
	}
	group.Wait()
	for index := range workers {
		if errors[index] != nil {
			t.Fatalf("worker %d: %v", index, errors[index])
		}
		if index > 0 && !bytes.Equal(results[0], results[index]) {
			t.Fatalf("worker %d pixels differ", index)
		}
	}
}

func TestFastPreviewCacheKeyCanonicalizesJSONAndEffectiveSeed(t *testing.T) {
	first, err := fastPreviewCacheKey(json.RawMessage(`{"seed": 1, "starting_area": 1}`), 123456)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fastPreviewCacheKey(json.RawMessage(`{ "starting_area": 1.0, "seed": null }`), 123456)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("semantically equivalent settings produced different cache keys")
	}
	different, err := fastPreviewCacheKey(json.RawMessage(`{"starting_area": 1, "seed": 1}`), 654321)
	if err != nil {
		t.Fatal(err)
	}
	if first == different {
		t.Fatal("different effective seeds produced the same cache key")
	}
}

func TestFastPreviewRasterCoordinateUsesULPTolerance(t *testing.T) {
	for _, tilesPerPixel := range []float64{minPreviewTilesPerPixel, 0.4, 1, 2.75, 63, maxPreviewTilesPerPixel} {
		t.Run(formatPreviewCenter(tilesPerPixel), func(t *testing.T) {
			center, _, err := normalizedPreviewCenter(maxPreviewCenter-100, 0, tilesPerPixel)
			if err != nil {
				t.Fatal(err)
			}
			want := int64(math.Round(center / tilesPerPixel))
			got, aligned := fastPreviewRasterCoordinate(center, tilesPerPixel)
			if !aligned || got != want {
				t.Fatalf("normalized center %g at %g m/px = %d, aligned=%v; want %d", center, tilesPerPixel, got, aligned, want)
			}
		})
	}

	if raster, aligned := fastPreviewRasterCoordinate(maxPreviewCenter+0.25, 1); aligned {
		t.Fatalf("large non-lattice center was accepted as raster %d", raster)
	}
}

func TestNormalizedPreviewCenterUsesScaleLattice(t *testing.T) {
	x, y, err := normalizedPreviewCenter(12.4, -8.6, 2.75)
	if err != nil {
		t.Fatal(err)
	}
	if x != 13.75 || y != -8.25 {
		t.Fatalf("normalized center = %g, %g, want 13.75, -8.25", x, y)
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), maxPreviewCenter + 1} {
		if _, _, err := normalizedPreviewCenter(value, 0, 1); err == nil {
			t.Fatalf("normalizedPreviewCenter(%v) succeeded", value)
		}
	}
}
