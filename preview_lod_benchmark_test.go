package main

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"testing"
	"time"
)

// Opt-in, reproducible profiling matrix; artifacts stay outside the source tree.
func TestPreviewLODProfile(t *testing.T) {
	dir := os.Getenv("FACTMAPGEN_LOD_PROFILE")
	if dir == "" {
		t.Skip("set FACTMAPGEN_LOD_PROFILE to an artifact directory")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	_, _, _, settings := benchmarkPreviewSettings()
	ctx := context.Background()
	for _, size := range []int{128, 256, 512} {
		for _, scale := range []float64{8, 16, 32, 64} {
			t.Run(fmt.Sprintf("%d/%g", size, scale), func(t *testing.T) {
				runtime.GC()
				stopRSS := startPeakRSSSampler()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				cache := newFastPreviewCache(defaultFastPreviewCacheBytes, defaultFastPreviewCacheWorlds)
				for _, state := range []string{"cold", "warm"} {
					started := time.Now()
					runtime.ReadMemStats(&before)
					var img *image.RGBA
					var err error
					pprof.Do(ctx, pprof.Labels("phase", state), func(ctx context.Context) {
						img, err = cache.render(ctx, fastPreviewWorldKey{1}, settings, size, scale, 0, 0)
					})
					if err != nil {
						t.Fatal(err)
					}
					elapsed := time.Since(started)
					runtime.ReadMemStats(&after)
					t.Logf("%s render=%s allocated=%.2fMiB heap=%.2fMiB", state, elapsed, float64(after.TotalAlloc-before.TotalAlloc)/(1<<20), float64(after.HeapAlloc)/(1<<20))
					started = time.Now()
					encoded, _, _, err := encodePNGPreviewImage(img)
					if err != nil {
						t.Fatal(err)
					}
					t.Logf("%s PNG=%s bytes=%d", state, time.Since(started), len(encoded))
					if state == "cold" {
						f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%d-%g.png", size, scale)))
						if err != nil {
							t.Fatal(err)
						}
						if err = png.Encode(f, img); err != nil {
							t.Fatal(err)
						}
						f.Close()
					}
				}
				t.Logf("peak RSS=%.2fMiB cache=%+v", float64(stopRSS())/1024, cache.stats())
			})
		}
	}
}

func BenchmarkPreviewLODStages(b *testing.B) {
	_, _, _, settings := benchmarkPreviewSettings()
	const size = 128
	for _, scale := range []float64{8, 16, 32, 64} {
		for _, stage := range []string{"terrain", "trees", "resources-rocks", "oil", "enemies", "cliffs", "encoding"} {
			b.Run(fmt.Sprintf("%g/%s", scale, stage), func(b *testing.B) {
				w := newFastPreviewWorld(settings)
				img := image.NewRGBA(image.Rect(0, 0, size, size))
				origin := -float64(size) * scale / 2
				ctx := context.Background()
				terrain := newFastPreviewWorld(defaultFactorioTerrainSettings(settings.seed))
				if err := terrain.renderBase(ctx, img, origin, origin, scale); err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					var err error
					pprof.Do(ctx, pprof.Labels("stage", stage), func(ctx context.Context) {
						switch stage {
						case "terrain":
							err = terrain.renderBase(ctx, img, origin, origin, scale)
						case "trees":
							err = renderFactorioTrees(ctx, img, settings, w.trees, origin, origin, scale)
						case "resources-rocks":
							for y := 0; y < size; y++ {
								for x := 0; x < size; x++ {
									blendFastPreviewEntities(nil, w.rocks, w.resources, nil, img.RGBAAt(x, y), nil, origin+float64(x)*scale, origin+float64(y)*scale, y*size+x, scale)
								}
							}
						case "oil":
							_, err = w.resources.oilPreviewMask(ctx, settings, img, origin, origin, scale)
						case "enemies":
							err = renderFactorioEnemies(ctx, img, settings, w.enemies, origin, origin, scale)
						case "cliffs":
							err = renderFactorioCliffs(ctx, img, settings, w.cliffs, origin, origin, scale)
						case "encoding":
							benchmarkPreviewBytes, _, _, err = encodePNGPreviewImage(img)
						}
					})
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
