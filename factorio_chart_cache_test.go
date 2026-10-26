package main

import (
	"context"
	"errors"
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

func TestFactorioChartChunkCacheBudgetAndLRU(t *testing.T) {
	entities := make([]factorioChartEntity, 1, 4)
	entryBytes := int64(cap(entities))*int64(unsafe.Sizeof(factorioChartEntity{})) + 128
	cache := factorioChartChunkCache{maxBytes: 3 * entryBytes}
	for i := range 3 {
		cache.store([2]int{i, 0}, entities)
	}
	if cache.usedBytes != cache.maxBytes {
		t.Fatalf("charged %d bytes, want %d (including slice capacity)", cache.usedBytes, cache.maxBytes)
	}
	if _, ok := cache.get([2]int{0, 0}); !ok {
		t.Fatal("oldest chunk missing before eviction")
	}
	cache.store([2]int{3, 0}, entities)
	if _, ok := cache.get([2]int{1, 0}); ok {
		t.Fatal("least recently used chunk survived eviction")
	}
	for _, i := range []int{0, 2, 3} {
		if _, ok := cache.get([2]int{i, 0}); !ok {
			t.Fatalf("chunk %d was discarded with the eviction victim", i)
		}
	}
	cache.store([2]int{0, 0}, entities)
	if cache.usedBytes != cache.maxBytes || len(cache.entries) != 3 {
		t.Fatal("duplicate store changed cache accounting")
	}
	cache.store([2]int{4, 0}, make([]factorioChartEntity, 100))
	if _, ok := cache.get([2]int{4, 0}); ok || len(cache.entries) != 3 {
		t.Fatal("oversized entry displaced reusable chunks")
	}
	cache.store([2]int{5, 0}, nil)
	if got, ok := cache.get([2]int{5, 0}); !ok || got != nil {
		t.Fatal("empty chunk was not cached")
	}
	if cache.usedBytes > cache.maxBytes {
		t.Fatal("cache exceeded byte budget")
	}
}

func TestFactorioChartChunkCacheRetainsMoreThan1024Chunks(t *testing.T) {
	var cache factorioChartChunkCache
	var workers sync.WaitGroup
	for worker := range 8 {
		workers.Go(func() {
			for i := range 256 {
				key := [2]int{worker, i}
				cache.store(key, nil)
				cache.get(key)
			}
		})
	}
	workers.Wait()
	if len(cache.entries) != 2048 || cache.usedBytes != 2048*128 {
		t.Fatalf("retained %d chunks / %d bytes, want 2048 / %d", len(cache.entries), cache.usedBytes, 2048*128)
	}
	for worker := range 8 {
		for i := range 256 {
			if _, ok := cache.get([2]int{worker, i}); !ok {
				t.Fatal("cache discarded earlier chunks below its memory budget")
			}
		}
	}
}

func TestRenderFactorioChartChunksConcurrentAndOrdered(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	var active, peak atomic.Int32
	var drawn [][2]int
	err := renderFactorioChartChunks(ctx, image.NewRGBA(image.Rect(0, 0, 320, 240)), -17, -33, 1,
		func(ctx context.Context, x, y int) ([]factorioChartEntity, error) {
			current := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); current > old; old = peak.Load() {
				if peak.CompareAndSwap(old, current) {
					break
				}
			}
			if x == -1 && y == -2 {
				close(firstStarted)
				select {
				case <-secondStarted:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			if x == 0 && y == -2 {
				select {
				case <-firstStarted:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				close(secondStarted)
			}
			return []factorioChartEntity{{x: float64(x), y: float64(y)}}, nil
		}, func(entities []factorioChartEntity) {
			drawn = append(drawn, [2]int{int(entities[0].x), int(entities[0].y)})
		})
	if err != nil {
		t.Fatal(err)
	}
	if peak.Load() < 2 || peak.Load() > 4 {
		t.Fatalf("peak workers = %d, want 2 through 4", peak.Load())
	}
	if len(drawn) != 99 {
		t.Fatalf("drew %d chunks, want 99", len(drawn))
	}
	for i, key := range drawn {
		if want := [2]int{-1 + i%11, -2 + i/11}; key != want {
			t.Fatalf("draw %d = %v, want %v", i, key, want)
		}
	}
}

func TestRenderFactorioChartChunksErrorsAndCancellation(t *testing.T) {
	for _, when := range []string{"before", "during", "error"} {
		t.Run(when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := context.Canceled
			if when == "before" {
				cancel()
			}
			if when == "error" {
				want = errors.New("chunk failed")
			}
			var calls atomic.Int32
			drawn := 0
			err := renderFactorioChartChunks(ctx, image.NewRGBA(image.Rect(0, 0, 512, 512)), 0, 0, 1,
				func(ctx context.Context, _, _ int) ([]factorioChartEntity, error) {
					calls.Add(1)
					if when == "during" {
						cancel()
					}
					return nil, want
				}, func([]factorioChartEntity) { drawn++ })
			if !errors.Is(err, want) || drawn != 0 {
				t.Fatalf("error = %v, drawn = %d; want %v, 0", err, drawn, want)
			}
			if when == "before" && calls.Load() != 0 {
				t.Fatal("started work after cancellation")
			}
		})
	}
}

func TestFastPreviewParallelEntitiesMatchSerial(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	_, _, _, settings := benchmarkPreviewSettings()
	for _, planet := range []string{"nauvis", "gleba", "fulgora", "vulcanus", "aquilo"} {
		for _, scale := range []float64{.4, 1, 2} {
			t.Run(planet+"/"+formatPreviewCenter(scale), func(t *testing.T) {
				settings.planet = planet
				runtime.GOMAXPROCS(1)
				serial, err := newFastPreviewWorld(settings).render(context.Background(), 96, scale, 288, -192)
				if err != nil {
					t.Fatal(err)
				}
				runtime.GOMAXPROCS(4)
				parallel, err := newFastPreviewWorld(settings).render(context.Background(), 96, scale, 288, -192)
				if err != nil {
					t.Fatal(err)
				}
				assertFastPreviewImagesEqual(t, parallel, serial)
			})
		}
	}
}

func TestFastPreviewEntityCacheBudgetConfiguration(t *testing.T) {
	p := &previewer{fastPreviewEntityCacheBytes: 7 << 20}
	cache := p.fastCache()
	_, _, _, settings := benchmarkPreviewSettings()
	for _, planet := range []string{"nauvis", "gleba", "fulgora", "vulcanus"} {
		settings.planet = planet
		entry := cache.acquireWorld(fastPreviewWorldKey{byte(len(planet))}, settings)
		var chunks *factorioChartChunkCache
		switch planet {
		case "nauvis":
			chunks = &entry.world.natural.chunks
		case "gleba":
			chunks = &entry.world.spaceAge.glebaChart.chunks
		case "fulgora":
			chunks = &entry.world.spaceAge.fulgoraChart.chunks
		case "vulcanus":
			chunks = &entry.world.spaceAge.vulcanusChart.chunks
		}
		if chunks.maxBytes != 7<<20 {
			t.Fatalf("%s entity budget = %d, want 7 MiB", planet, chunks.maxBytes)
		}
		cache.releaseWorld(entry)
	}
}
