package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

var benchmarkPreviewImage *image.RGBA
var benchmarkPreviewBytes []byte

func BenchmarkRenderFastPreview1024(b *testing.B) {
	base, trees, resources, full := benchmarkPreviewSettings()
	for _, test := range []struct {
		name     string
		settings fastPreviewSettings
	}{
		{name: "terrain", settings: base},
		{name: "trees", settings: trees},
		{name: "resources-oil-rocks", settings: resources},
		{name: "full", settings: full},
	} {
		b.Run(test.name, func(b *testing.B) {
			zoom := previewZoom{mode: "normal", tilesPerPixel: 1, renderSize: 1024}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				img, _, err := renderFastMapPreview(context.Background(), test.settings, 1024, zoom)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkPreviewImage = img
			}
		})
	}
}

func TestFastPreviewPixelGolden(t *testing.T) {
	_, _, _, full := benchmarkPreviewSettings()
	for _, test := range []struct {
		name          string
		tilesPerPixel float64
		want          string
	}{
		{name: "one-tile-per-pixel", tilesPerPixel: 1, want: "c38cafeedbebe60daa2cdbb898568dfc72122390931e278d86cae277a1d5da89"},
		{name: "two-tiles-per-pixel", tilesPerPixel: 2, want: "bd009a2b3762ce833bc29387eee9ab97d3ef3ae66b8a793fbbdbe813987004fc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			img, _, err := renderFastMapPreview(
				context.Background(), full, 256,
				previewZoom{mode: "normal", tilesPerPixel: test.tilesPerPixel, renderSize: 256},
			)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(img.Pix)
			got := hex.EncodeToString(sum[:])
			if got != test.want {
				t.Errorf("pixel hash = %s, want %s", got, test.want)
			}
		})
	}
}

func benchmarkPreviewSettings() (
	base, trees, resources, full fastPreviewSettings,
) {
	base = defaultFactorioTerrainSettings(123456)
	trees = base
	trees.trees = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	resources = base
	resources.resourceControls = benchmarkResourceControls()
	resources.rocks = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	full = resources
	full.trees = trees.trees
	full.cliffs = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	full.cliffElevation0 = 10
	full.cliffElevationInterval = 40
	full.cliffRichness = 1
	return base, trees, resources, full
}

func TestRenderSpeedComparison1024(t *testing.T) {
	mode := os.Getenv("FACTMAPGEN_RENDER_SPEED_COMPARISON")
	if mode != "1" && mode != "fast-only" {
		t.Skip("set FACTMAPGEN_RENDER_SPEED_COMPARISON=fast-only for Go metrics or 1 to compare with Factorio")
	}
	const seed = "123456"
	mapGenPath, err := filepath.Abs(filepath.Join("default-presets", "Default", mapGenFile))
	if err != nil {
		t.Fatalf("resolve default map settings: %v", err)
	}
	body, err := readNormalizedMapGenJSON(mapGenPath)
	if err != nil {
		t.Fatalf("read default map settings: %v", err)
	}
	settings, err := parseFastPreviewSettings(body, seed)
	if err != nil {
		t.Fatalf("parse default map settings: %v", err)
	}

	runtime.GC()
	fastUserBefore, fastSystemBefore := selfCPUTime()
	stopRSS := startPeakRSSSampler()
	started := time.Now()
	fast, _, err := renderFastMapPreview(
		context.Background(), settings, 1024,
		previewZoom{mode: "normal", tilesPerPixel: 1, renderSize: 1024},
	)
	if err != nil {
		t.Fatalf("render fast preview: %v", err)
	}
	fastRenderDuration := time.Since(started)
	if fast.Bounds().Dx() != 1024 || fast.Bounds().Dy() != 1024 {
		t.Fatalf("fast preview dimensions = %v, want 1024x1024", fast.Bounds())
	}
	fastPNG, _, _, err := encodePreviewImage(context.Background(), fast, true)
	if err != nil {
		t.Fatalf("encode fast preview PNG: %v", err)
	}
	fastTotalDuration := time.Since(started)
	fastPeakRSS := stopRSS()
	fastUserAfter, fastSystemAfter := selfCPUTime()
	benchmarkPreviewBytes = fastPNG
	fastUsage := previewProcessUsage{
		wall:   fastTotalDuration,
		user:   fastUserAfter - fastUserBefore,
		system: fastSystemAfter - fastSystemBefore,
		maxRSS: fastPeakRSS,
	}
	t.Logf(
		"fast process: render=%s render+PNG=%s CPU=%s (user=%s system=%s, %.0f%%), peak RSS=%.1f MiB",
		fastRenderDuration.Round(time.Millisecond), fastUsage.wall.Round(time.Millisecond),
		(fastUsage.user + fastUsage.system).Round(time.Millisecond), fastUsage.user.Round(time.Millisecond),
		fastUsage.system.Round(time.Millisecond), cpuPercent(fastUsage), float64(fastUsage.maxRSS)/1024,
	)
	if mode == "fast-only" {
		return
	}

	factorioBin := requirePreviewParityFactorio(t)
	factorioPNG, factorioUsage := renderFactorioPreviewPNGWithUsage(t, factorioBin, mapGenPath, 1024, seed)
	factorio := decodePNG(t, factorioPNG)
	if factorio.Bounds().Dx() != 1024 || factorio.Bounds().Dy() != 1024 {
		t.Fatalf("Factorio preview dimensions = %v, want 1024x1024", factorio.Bounds())
	}
	t.Logf(
		"seed %s, 1024x1024 at 1 m/px: fast render=%s, fast render+PNG=%s, Factorio render+PNG=%s, fast-throughput=%.2fx Factorio",
		seed, fastRenderDuration.Round(time.Millisecond), fastUsage.wall.Round(time.Millisecond),
		factorioUsage.wall.Round(time.Millisecond), durationRatio(factorioUsage.wall, fastUsage.wall),
	)
	t.Logf(
		"Factorio process: CPU=%s (user=%s system=%s, %.0f%%), peak RSS=%.1f MiB",
		(factorioUsage.user + factorioUsage.system).Round(time.Millisecond), factorioUsage.user.Round(time.Millisecond),
		factorioUsage.system.Round(time.Millisecond), cpuPercent(factorioUsage), float64(factorioUsage.maxRSS)/1024,
	)
	t.Logf(
		"resource ratios (fast/Factorio): CPU=%.2fx peak-RSS=%.2fx",
		durationRatio(fastUsage.user+fastUsage.system, factorioUsage.user+factorioUsage.system),
		float64(fastUsage.maxRSS)/math.Max(1, float64(factorioUsage.maxRSS)),
	)
}

func selfCPUTime() (user, system time.Duration) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0, 0
	}
	return timevalDuration(usage.Utime), timevalDuration(usage.Stime)
}

func startPeakRSSSampler() func() int64 {
	stop := make(chan struct{})
	result := make(chan int64, 1)
	go func() {
		peak := currentRSSKB()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				peak = max(peak, currentRSSKB())
			case <-stop:
				result <- max(peak, currentRSSKB())
				return
			}
		}
	}()
	return func() int64 {
		close(stop)
		return <-result
	}
}

func currentRSSKB() int64 {
	body, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		value, _ := strconv.ParseInt(fields[1], 10, 64)
		return value
	}
	return 0
}

func cpuPercent(usage previewProcessUsage) float64 {
	if usage.wall <= 0 {
		return 0
	}
	return 100 * float64(usage.user+usage.system) / float64(usage.wall)
}

func durationRatio(numerator, denominator time.Duration) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func benchmarkResourceControls() map[string]fastControl {
	controls := make(map[string]fastControl, len(factorioResourceCatalog))
	for _, resource := range factorioResourceCatalog {
		controls[resource.name] = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	}
	return controls
}
