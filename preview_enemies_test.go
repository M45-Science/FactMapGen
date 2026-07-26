package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const enemyPreviewParityEnv = "FACTMAPGEN_ENEMY_PREVIEW_PARITY"

type enemyPreviewStats struct {
	Seed                 string    `json:"seed"`
	TilesPerPixel        float64   `json:"tilesPerPixel"`
	RegionCorrelation    float64   `json:"regionCorrelation"`
	FastToFactorioPixels float64   `json:"fastToFactorioPixels"`
	FactorioPixels       int       `json:"factorioPixels"`
	FastPixels           int       `json:"fastPixels"`
	FactorioNearest      float64   `json:"factorioNearest"`
	FastNearest          float64   `json:"fastNearest"`
	RadialEdges          []float64 `json:"radialEdges"`
	FactorioRadialPixels []int     `json:"factorioRadialPixels"`
	FastRadialPixels     []int     `json:"fastRadialPixels"`
}

func TestFastEnemyLayersMatchFactorioPreviewRegions(t *testing.T) {
	if os.Getenv(enemyPreviewParityEnv) != "1" {
		t.Skipf("set %s=1 to compare enemy-base regions against Factorio", enemyPreviewParityEnv)
	}
	if testing.Short() {
		t.Skip("skipping enemy-layer Factorio integration test in short mode")
	}
	const (
		outputSize    = 1024
		tilesPerPixel = 1.0
	)
	factorioBin := ""
	store := newTestStore(t)
	_, sourcePath := previewParityProfile(t, store, "default:Default")
	mapGenPath := enemyLayerMapGenPath(t, sourcePath)

	for _, seed := range []string{"123456"} {
		t.Run("seed-"+seed, func(t *testing.T) {
			factorioImg, _ := cachedFactorioEnemyPreview(
				t, &factorioBin, mapGenPath, seed, outputSize, tilesPerPixel,
			)
			fastImg := renderFastEnemyLayer(t, mapGenPath, seed, outputSize, tilesPerPixel)
			stats := measureEnemyPreview(seed, tilesPerPixel, factorioImg, fastImg)
			writeEnemyPreviewArtifacts(t, seed, factorioImg, fastImg, stats)

			if stats.FactorioPixels == 0 || stats.FastPixels == 0 {
				t.Fatalf("enemy mask is empty: Factorio=%d fast=%d", stats.FactorioPixels, stats.FastPixels)
			}
			if stats.RegionCorrelation < 0.85 ||
				stats.FastToFactorioPixels < 0.65 || stats.FastToFactorioPixels > 1.6 {
				t.Errorf(
					"enemy regions diverged: correlation=%.3f fast/game pixels=%.3f",
					stats.RegionCorrelation,
					stats.FastToFactorioPixels,
				)
			}
			if math.Abs(stats.FastNearest-stats.FactorioNearest) > 25 {
				t.Errorf(
					"enemy starting-area edge diverged: Factorio=%.1f fast=%.1f",
					stats.FactorioNearest,
					stats.FastNearest,
				)
			}
			for ring := range stats.FactorioRadialPixels {
				if stats.FactorioRadialPixels[ring] < 25 {
					continue
				}
				ratio := previewCountRatio(stats.FastRadialPixels[ring], stats.FactorioRadialPixels[ring])
				if ratio < 0.65 || ratio > 1.6 {
					t.Errorf(
						"enemy radial population ring %d diverged: fast/game=%.3f pixels=%d/%d",
						ring, ratio, stats.FastRadialPixels[ring], stats.FactorioRadialPixels[ring],
					)
				}
			}
			t.Logf(
				"enemies correlation=%.3f fast/game pixels=%.3f nearest Factorio=%.1f fast=%.1f radial Factorio=%v fast=%v",
				stats.RegionCorrelation,
				stats.FastToFactorioPixels,
				stats.FactorioNearest,
				stats.FastNearest,
				stats.FactorioRadialPixels,
				stats.FastRadialPixels,
			)
		})
	}
}

func enemyLayerMapGenPath(t *testing.T, sourcePath string) string {
	t.Helper()
	body, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read enemy-layer map settings: %v", err)
	}
	body, err = enemyLayersMapGenJSON(body)
	if err != nil {
		t.Fatalf("build enemy-layer map settings: %v", err)
	}
	path := filepath.Join(t.TempDir(), "enemies-map-gen-settings.json")
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write enemy-layer map settings: %v", err)
	}
	return path
}

func enemyLayersMapGenJSON(body []byte) ([]byte, error) {
	body, err := naturalLayersMapGenJSON(body, false, false)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	controls := fastMap(root["autoplace_controls"])
	controls["enemy-base"] = map[string]any{"frequency": 1, "size": 1, "richness": 1}
	root["autoplace_controls"] = controls
	root["no_enemies_mode"] = false
	root["peaceful_mode"] = false
	autoplace := fastMap(root["autoplace_settings"])
	autoplace["entity"] = map[string]any{
		"treat_missing_as_default": true,
		"settings": map[string]any{
			"fish": map[string]any{"frequency": 0, "size": 0, "richness": 0},
		},
	}
	root["autoplace_settings"] = autoplace
	return json.MarshalIndent(root, "", "  ")
}

func cachedFactorioEnemyPreview(
	t *testing.T,
	factorioBin *string,
	mapGenPath, seed string,
	outputSize int,
	tilesPerPixel float64,
) (image.Image, bool) {
	t.Helper()
	mapGenBody, err := os.ReadFile(mapGenPath)
	if err != nil {
		t.Fatalf("read enemy preview cache settings: %v", err)
	}
	settingsHash := sha256.Sum256(mapGenBody)
	cachePath := filepath.Join(
		"test-output",
		"preview-cache",
		"enemy-layers",
		fmt.Sprintf(
			"seed-%s-enemies-%dpx-%gtpp-%x-factorio.png",
			seed, outputSize, tilesPerPixel, settingsHash[:6],
		),
	)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("create enemy preview cache: %v", err)
	}
	return galleryReferenceImage(t, cachePath, outputSize, func() image.Image {
		if *factorioBin == "" {
			*factorioBin = requirePreviewParityFactorio(t)
		}
		sourceSize := int(math.Ceil(float64(outputSize) * tilesPerPixel))
		body := renderFactorioPreviewPNG(t, *factorioBin, mapGenPath, sourceSize, seed)
		source := decodePNG(t, body)
		if tilesPerPixel == 1 {
			return source
		}
		return scalePreviewImage(source, outputSize, tilesPerPixel)
	})
}

func renderFastEnemyLayer(
	t *testing.T,
	mapGenPath, seed string,
	size int,
	tilesPerPixel float64,
) image.Image {
	t.Helper()
	body, err := readNormalizedMapGenJSON(mapGenPath)
	if err != nil {
		t.Fatalf("read fast enemy-layer settings: %v", err)
	}
	settings, err := parseFastPreviewSettings(body, seed)
	if err != nil {
		t.Fatalf("parse fast enemy-layer settings: %v", err)
	}
	img, _, err := renderFastMapPreview(
		context.Background(),
		settings,
		size,
		previewZoom{mode: "scale", tilesPerPixel: tilesPerPixel, renderSize: size},
	)
	if err != nil {
		t.Fatalf("render fast enemy layer: %v", err)
	}
	return img
}

func measureEnemyPreview(
	seed string,
	tilesPerPixel float64,
	factorioImg, fastImg image.Image,
) enemyPreviewStats {
	factorioMask := previewColorMask(factorioImg, factorioEnemyMapColor)
	fastMask := previewColorMask(fastImg, factorioEnemyMapColor)
	factorioPixels := previewMaskCount(factorioMask)
	fastPixels := previewMaskCount(fastMask)
	bounds := factorioImg.Bounds()
	radialEdges := []float64{256, 384, 512, 1024}
	factorioRadial := enemyMaskRadialPixels(factorioMask, bounds.Dx(), bounds.Dy(), radialEdges)
	fastRadial := enemyMaskRadialPixels(fastMask, bounds.Dx(), bounds.Dy(), radialEdges)
	return enemyPreviewStats{
		Seed:                 seed,
		TilesPerPixel:        tilesPerPixel,
		RegionCorrelation:    previewPearsonCorrelation(enemyMaskBlocks(factorioMask, bounds.Dx(), bounds.Dy(), 32), enemyMaskBlocks(fastMask, bounds.Dx(), bounds.Dy(), 32)),
		FastToFactorioPixels: previewCountRatio(fastPixels, factorioPixels),
		FactorioPixels:       factorioPixels,
		FastPixels:           fastPixels,
		FactorioNearest:      enemyMaskNearest(factorioMask, bounds.Dx(), bounds.Dy()),
		FastNearest:          enemyMaskNearest(fastMask, bounds.Dx(), bounds.Dy()),
		RadialEdges:          radialEdges,
		FactorioRadialPixels: factorioRadial,
		FastRadialPixels:     fastRadial,
	}
}

func enemyMaskRadialPixels(mask []bool, width, height int, edges []float64) []int {
	counts := make([]int, len(edges))
	centerX := float64(width) / 2
	centerY := float64(height) / 2
	for index, marked := range mask {
		if !marked {
			continue
		}
		distance := math.Hypot(float64(index%width)-centerX, float64(index/width)-centerY)
		for ring, edge := range edges {
			if distance < edge {
				counts[ring]++
				break
			}
		}
	}
	return counts
}

func enemyMaskBlocks(mask []bool, width, height, blockSize int) []float64 {
	blocksX := (width + blockSize - 1) / blockSize
	blocksY := (height + blockSize - 1) / blockSize
	blocks := make([]float64, blocksX*blocksY)
	for index, marked := range mask {
		if marked {
			blocks[(index/width/blockSize)*blocksX+(index%width)/blockSize]++
		}
	}
	return blocks
}

func enemyMaskNearest(mask []bool, width, height int) float64 {
	centerX := float64(width) / 2
	centerY := float64(height) / 2
	nearest := math.Inf(1)
	for index, marked := range mask {
		if !marked {
			continue
		}
		x := float64(index%width) - centerX
		y := float64(index/width) - centerY
		nearest = min(nearest, math.Hypot(x, y))
	}
	return nearest
}

func writeEnemyPreviewArtifacts(
	t *testing.T,
	seed string,
	factorioImg, fastImg image.Image,
	stats enemyPreviewStats,
) {
	t.Helper()
	dir := filepath.Join("test-output", "preview-diffs", "enemies-seed-"+seed)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create enemy preview artifacts: %v", err)
	}
	writePNGArtifact(t, filepath.Join(dir, "factorio.png"), factorioImg)
	writePNGArtifact(t, filepath.Join(dir, "fast.png"), fastImg)
	_, diff, _ := previewImageDiff(factorioImg, fastImg)
	writePNGArtifact(t, filepath.Join(dir, "diff.png"), diff)
	body, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatalf("marshal enemy preview stats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stats.json"), append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write enemy preview stats: %v", err)
	}
}
