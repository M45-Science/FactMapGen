package main

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestFastPreviewSameWorldRendersWhileAnotherEvaluatorIsHeld(t *testing.T) {
	if fastPreviewRenderConcurrency() < 2 {
		t.Skip("requires two render slots")
	}
	_, _, _, settings := benchmarkPreviewSettings()
	cache := newFastPreviewCache(8<<20, 2)
	key := fastPreviewWorldKey{0x71}
	entry := cache.acquireWorld(key, settings)
	defer cache.releaseWorld(entry)
	if err := entry.acquireRender(context.Background()); err != nil {
		t.Fatal(err)
	}
	held := entry.takeRenderWorld()
	defer func() { entry.returnRenderWorld(held); entry.releaseRender() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The first render's evaluator remains leased. A second same-world request
	// must render, rather than waiting for that first request to finish.
	got, err := cache.render(ctx, key, settings, 128, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	want, err := newFastPreviewWorld(settings).render(ctx, 128, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertFastPreviewImagesEqual(t, got, want)
	other := entry.takeRenderWorld()
	defer entry.returnRenderWorld(other)
	if other == held || other.resources == held.resources || other.natural.resources == held.natural.resources {
		t.Fatal("concurrent renders shared mutable evaluator state")
	}
	if other.natural.chunks.shared != &entry.world.natural.chunks {
		t.Fatal("pooled evaluator did not share the bounded entity LRU")
	}
	if got, want := other.natural.chunks.shared.maxBytes, cache.entityCacheBytes; got != want {
		t.Fatalf("entity budget=%d want=%d", got, want)
	}
}

func TestFastPreviewConcurrentPlanetsMatchSerial(t *testing.T) {
	for _, planet := range []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo"} {
		t.Run(planet, func(t *testing.T) {
			_, _, _, settings := benchmarkPreviewSettings()
			settings.planet = planet
			cache := newFastPreviewCache(16<<20, 2)
			key := fastPreviewWorldKey{0x72}
			entry := cache.acquireWorld(key, settings)
			// Force two evaluator leases even on one-CPU CI; admission is still bounded.
			first := entry.takeRenderWorld()
			second := entry.takeRenderWorld()
			if first == second {
				t.Fatal("duplicate evaluator lease")
			}
			entry.returnRenderWorld(first)
			entry.returnRenderWorld(second)
			cache.releaseWorld(entry)
			var workers sync.WaitGroup
			points := [][2]float64{{-128, -128}, {128, -128}, {-128, 128}, {128, 128}}
			for _, point := range points {
				workers.Go(func() {
					ctx := context.Background()
					got, err := cache.render(ctx, key, settings, 128, 1, point[0], point[1])
					if err != nil {
						t.Error(err)
						return
					}
					want, err := newFastPreviewWorld(settings).render(ctx, 128, 1, point[0], point[1])
					if err != nil {
						t.Error(err)
						return
					}
					assertFastPreviewImagesEqual(t, got, want)
				})
			}
			workers.Wait()
			entry.poolMu.Lock()
			poolSize := len(entry.idleWorlds)
			entry.poolMu.Unlock()
			if poolSize > max(2, min(len(points), fastPreviewRenderConcurrency())) {
				t.Fatalf("evaluator pool grew to %d", poolSize)
			}
		})
	}
}

func BenchmarkFastPreviewEightTiles(b *testing.B) {
	_, _, _, settings := benchmarkPreviewSettings()
	points := [][2]float64{{-768, -256}, {-256, -256}, {256, -256}, {768, -256}, {-768, 256}, {-256, 256}, {256, 256}, {768, 256}}
	for _, mode := range []struct {
		name  string
		slots int
		warm  bool
	}{
		{"serial-cold", 1, false}, {"two-cold", 2, false}, {"four-cold", 4, false}, {"eight-cold", 8, false}, {"cpu-count-cold", runtime.NumCPU(), false}, {"serial-warm", 1, true}, {"cpu-count-warm", runtime.NumCPU(), true},
	} {
		b.Run(mode.name, func(b *testing.B) {
			if mode.slots > fastPreviewRenderConcurrency() {
				b.Skip("requested concurrency exceeds available CPUs")
			}
			var firstTotal, lastTotal time.Duration
			for range b.N {
				b.StopTimer()
				cache := newFastPreviewCache(32<<20, 2)
				key := fastPreviewWorldKey{0x73}
				entry := cache.acquireWorld(key, settings)
				entry.renderGate = make(chan struct{}, mode.slots)
				cache.releaseWorld(entry)
				render := func(point [2]float64) {
					if _, err := cache.render(context.Background(), key, settings, 512, 1, point[0], point[1]); err != nil {
						b.Error(err)
					}
				}
				if mode.warm {
					for _, point := range points {
						render(point)
					}
				}
				b.StartTimer()
				start := time.Now()
				done := make(chan time.Duration, len(points))
				for _, point := range points {
					go func() { render(point); done <- time.Since(start) }()
				}
				firstTotal += <-done
				var last time.Duration
				for i := 1; i < len(points); i++ {
					last = <-done
				}
				lastTotal += last
			}
			b.ReportMetric(float64(firstTotal.Microseconds())/float64(b.N)/1000, "first-ms")
			b.ReportMetric(float64(lastTotal.Microseconds())/float64(b.N)/1000, "all-ms")
			b.ReportMetric(float64(runtime.GOMAXPROCS(0)), "procs")
		})
	}
}
