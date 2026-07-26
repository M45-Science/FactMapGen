package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"
)

const (
	naturalPreviewParityEnv  = "FACTMAPGEN_NATURAL_PREVIEW_PARITY"
	naturalPreviewGalleryEnv = "FACTMAPGEN_NATURAL_PREVIEW_GALLERY"
)

type naturalPreviewStats struct {
	Seed                  string  `json:"seed"`
	TilesPerPixel         float64 `json:"tilesPerPixel"`
	TreeRegionCorrelation float64 `json:"treeRegionCorrelation"`
	TreeFastToGameInk     float64 `json:"treeFastToGameInk"`
	FactorioTreeInk       float64 `json:"factorioTreeInk"`
	FastTreeInk           float64 `json:"fastTreeInk"`
	FactorioCliffPixels   int     `json:"factorioCliffPixels"`
	FastCliffPixels       int     `json:"fastCliffPixels"`
	CliffFastToGameInk    float64 `json:"cliffFastToGameInk"`
	CliffRecall           float64 `json:"cliffRecall"`
	CliffPrecision        float64 `json:"cliffPrecision"`
}

func TestFastNaturalLayersMatchFactorioPreviewRegions(t *testing.T) {
	if os.Getenv(naturalPreviewParityEnv) != "1" {
		t.Skipf("set %s=1 to compare tree and cliff regions against Factorio", naturalPreviewParityEnv)
	}
	if testing.Short() {
		t.Skip("skipping natural-layer Factorio integration test in short mode")
	}
	const (
		outputSize    = 512
		tilesPerPixel = 2.0
	)
	factorioBin := ""
	store := newTestStore(t)
	_, sourcePath := previewParityProfile(t, store, "default:Default")

	for _, seed := range []string{"123456", "777771"} {
		t.Run("seed-"+seed, func(t *testing.T) {
			terrainPath := naturalLayerMapGenPath(t, sourcePath, false, false)
			treesPath := naturalLayerMapGenPath(t, sourcePath, true, false)
			cliffsPath := naturalLayerMapGenPath(t, sourcePath, false, true)

			factorioTerrain, _ := cachedFactorioNaturalPreview(t, &factorioBin, terrainPath, "terrain", seed, outputSize, tilesPerPixel)
			factorioTrees, _ := cachedFactorioNaturalPreview(t, &factorioBin, treesPath, "trees", seed, outputSize, tilesPerPixel)
			factorioCliffs, _ := cachedFactorioNaturalPreview(t, &factorioBin, cliffsPath, "cliffs", seed, outputSize, tilesPerPixel)
			fastTerrain := renderFastNaturalLayer(t, terrainPath, seed, outputSize, tilesPerPixel)
			fastTrees := renderFastNaturalLayer(t, treesPath, seed, outputSize, tilesPerPixel)
			fastCliffs := renderFastNaturalLayer(t, cliffsPath, seed, outputSize, tilesPerPixel)

			factorioTreeBlocks, factorioTreeInk := previewOverlayInk(factorioTerrain, factorioTrees, 16)
			fastTreeBlocks, fastTreeInk := previewOverlayInk(fastTerrain, fastTrees, 16)
			correlation := previewPearsonCorrelation(factorioTreeBlocks, fastTreeBlocks)
			inkRatio := fastTreeInk / factorioTreeInk

			factorioCliffMask := previewColorMask(factorioCliffs, factorioCliffMapColor)
			fastCliffMask := previewColorMask(fastCliffs, factorioCliffMapColor)
			factorioCliffPixels := previewMaskCount(factorioCliffMask)
			fastCliffPixels := previewMaskCount(fastCliffMask)
			if factorioCliffPixels == 0 || fastCliffPixels == 0 {
				t.Fatalf("cliff mask is empty: Factorio=%d fast=%d", factorioCliffPixels, fastCliffPixels)
			}
			cliffInkRatio := float64(fastCliffPixels) / float64(factorioCliffPixels)
			cliffRecall := previewMaskTolerance(factorioCliffMask, fastCliffMask, outputSize, outputSize, 3)
			cliffPrecision := previewMaskTolerance(fastCliffMask, factorioCliffMask, outputSize, outputSize, 3)

			stats := naturalPreviewStats{
				Seed:                  seed,
				TilesPerPixel:         tilesPerPixel,
				TreeRegionCorrelation: correlation,
				TreeFastToGameInk:     inkRatio,
				FactorioTreeInk:       factorioTreeInk,
				FastTreeInk:           fastTreeInk,
				FactorioCliffPixels:   factorioCliffPixels,
				FastCliffPixels:       fastCliffPixels,
				CliffFastToGameInk:    cliffInkRatio,
				CliffRecall:           cliffRecall,
				CliffPrecision:        cliffPrecision,
			}
			writeNaturalPreviewArtifacts(t, seed, factorioTrees, fastTrees, factorioCliffs, fastCliffs, stats)

			if factorioTreeInk == 0 || fastTreeInk == 0 {
				t.Fatalf("tree overlay ink is empty: Factorio=%g fast=%g", factorioTreeInk, fastTreeInk)
			}
			if correlation < 0.90 || inkRatio < 0.70 || inkRatio > 1.10 {
				t.Errorf("tree regions diverged: correlation=%.3f fast/game ink=%.3f", correlation, inkRatio)
			}
			if cliffRecall < 0.90 || cliffPrecision < 0.85 || cliffInkRatio < 0.65 || cliffInkRatio > 1.75 {
				t.Errorf("cliff masks diverged: recall=%.3f precision=%.3f fast/game ink=%.3f", cliffRecall, cliffPrecision, cliffInkRatio)
			}
			t.Logf("trees correlation=%.3f fast/game ink=%.3f; cliffs recall=%.3f precision=%.3f fast/game ink=%.3f", correlation, inkRatio, cliffRecall, cliffPrecision, cliffInkRatio)
		})
	}
}

func TestPreviewGalleryNaturalLayersDefaultSeeds(t *testing.T) {
	if os.Getenv(naturalPreviewGalleryEnv) != "1" {
		t.Skipf("set %s=1 to render the Factorio-vs-fast tree and cliff gallery", naturalPreviewGalleryEnv)
	}
	if testing.Short() {
		t.Skip("skipping natural-layer preview gallery in short mode")
	}
	const (
		outputSize    = 512
		tilesPerPixel = 2.0
	)
	factorioBin := ""
	store := newTestStore(t)
	_, sourcePath := previewParityProfile(t, store, "default:Default")
	mapGenPath := naturalLayerMapGenPath(t, sourcePath, true, true)

	outDir := filepath.Join("test-output", "preview-gallery", "default-10-seeds-trees-cliffs")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create natural preview gallery: %v", err)
	}
	type row struct {
		Index          int
		Seed           string
		FactorioPath   string
		FastPath       string
		DiffPath       string
		ChangedPercent string
		WaterPercent   string
		MaxDelta       int
		Reference      string
	}
	rows := make([]row, 0, 10)
	cachedReferences := 0
	for index, seed := range previewGallerySeeds(10) {
		seedText := fmt.Sprint(seed)
		prefix := fmt.Sprintf("seed-%02d-%s", index+1, seedText)
		factorioName := prefix + "-factorio.png"
		fastName := prefix + "-fast.png"
		diffName := prefix + "-diff.png"

		factorioImg, cached := cachedFactorioNaturalPreview(
			t, &factorioBin, mapGenPath, "trees-cliffs", seedText, outputSize, tilesPerPixel,
		)
		reference := "rendered"
		if cached {
			reference = "cached"
			cachedReferences++
		}
		fastImg := renderFastNaturalLayer(t, mapGenPath, seedText, outputSize, tilesPerPixel)
		stats, diffImg, _ := previewImageDiff(factorioImg, fastImg)
		stats.WaterMaskChangedPercent = previewWaterMaskChangedPercent(factorioImg, fastImg)

		writePNGArtifact(t, filepath.Join(outDir, factorioName), factorioImg)
		writePNGArtifact(t, filepath.Join(outDir, fastName), fastImg)
		writePNGArtifact(t, filepath.Join(outDir, diffName), diffImg)
		body, err := json.MarshalIndent(stats, "", "  ")
		if err != nil {
			t.Fatalf("marshal natural gallery stats for seed %s: %v", seedText, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, prefix+"-stats.json"), append(body, '\n'), 0o644); err != nil {
			t.Fatalf("write natural gallery stats for seed %s: %v", seedText, err)
		}

		rows = append(rows, row{
			Index:          index + 1,
			Seed:           seedText,
			FactorioPath:   factorioName,
			FastPath:       fastName,
			DiffPath:       diffName,
			ChangedPercent: fmt.Sprintf("%.4f", stats.ChangedPercent),
			WaterPercent:   fmt.Sprintf("%.4f", stats.WaterMaskChangedPercent),
			MaxDelta:       stats.MaxChannelDelta,
			Reference:      reference,
		})
	}
	writeNaturalPreviewGalleryHTML(t, filepath.Join(outDir, "index.html"), rows)
	t.Logf("wrote natural-layer gallery to %s; reused %d/10 Factorio references", outDir, cachedReferences)
}

func writeNaturalPreviewGalleryHTML(t *testing.T, path string, rows any) {
	t.Helper()
	const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>FactMapGen Trees and Cliffs Preview Gallery</title>
  <style>
    body { margin: 24px; font-family: system-ui, sans-serif; background: #171912; color: #f1ead9; }
    table { border-collapse: collapse; }
    th, td { padding: 8px; border-bottom: 1px solid #3f4636; vertical-align: top; }
    th { text-align: left; color: #ffca68; }
    img { width: 320px; height: 320px; image-rendering: auto; box-shadow: 0 0 0 1px #4e5742; }
    .seed { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; white-space: nowrap; }
    .meta { color: #c9bea4; }
  </style>
</head>
<body>
  <h1>Default Trees + Cliffs Preview Comparison</h1>
  <p class="meta">512px at 2 meters per pixel. Resources, rocks, enemies, fish, and decorations are disabled. Left: cached Factorio headless preview. Middle: FactMapGen fast Go preview. Right: amplified RGB diff.</p>
  <table>
    <thead><tr><th>#</th><th>Seed</th><th>Factorio</th><th>Fast Go</th><th>Diff</th><th>Stats</th></tr></thead>
    <tbody>
      {{range .}}
      <tr>
        <td>{{.Index}}</td>
        <td class="seed">{{.Seed}}</td>
        <td><img src="{{.FactorioPath}}" alt="Factorio preview seed {{.Seed}}"></td>
        <td><img src="{{.FastPath}}" alt="Fast Go preview seed {{.Seed}}"></td>
        <td><img src="{{.DiffPath}}" alt="Diff seed {{.Seed}}"></td>
        <td class="meta">{{.ChangedPercent}}% RGB changed<br>{{.WaterPercent}}% water mask changed<br>max delta {{.MaxDelta}}<br>Factorio: {{.Reference}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>
`
	tmpl, err := template.New("natural-gallery").Parse(page)
	if err != nil {
		t.Fatalf("parse natural gallery template: %v", err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create natural gallery HTML: %v", err)
	}
	defer out.Close()
	if err := tmpl.Execute(out, rows); err != nil {
		t.Fatalf("write natural gallery HTML: %v", err)
	}
}

func naturalLayerMapGenPath(t *testing.T, sourcePath string, trees, cliffs bool) string {
	t.Helper()
	body, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read natural-layer map settings: %v", err)
	}
	body, err = naturalLayersMapGenJSON(body, trees, cliffs)
	if err != nil {
		t.Fatalf("build natural-layer map settings: %v", err)
	}
	path := filepath.Join(t.TempDir(), fmt.Sprintf("natural-%t-%t-map-gen-settings.json", trees, cliffs))
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write natural-layer map settings: %v", err)
	}
	return path
}

func cachedFactorioNaturalPreview(
	t *testing.T,
	factorioBin *string,
	mapGenPath, layer, seed string,
	outputSize int,
	tilesPerPixel float64,
) (image.Image, bool) {
	t.Helper()
	mapGenBody, err := os.ReadFile(mapGenPath)
	if err != nil {
		t.Fatalf("read natural preview cache settings: %v", err)
	}
	settingsHash := sha256.Sum256(mapGenBody)
	cachePath := filepath.Join(
		"test-output",
		"preview-cache",
		"natural-layers",
		fmt.Sprintf("seed-%s-%s-%dpx-%gtpp-%x-factorio.png", seed, layer, outputSize, tilesPerPixel, settingsHash[:6]),
	)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("create natural preview cache: %v", err)
	}
	img, cached := galleryReferenceImage(t, cachePath, outputSize, func() image.Image {
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
	return img, cached
}

func renderFastNaturalLayer(t *testing.T, mapGenPath, seed string, size int, tilesPerPixel float64) image.Image {
	t.Helper()
	body, err := readNormalizedMapGenJSON(mapGenPath)
	if err != nil {
		t.Fatalf("read fast natural-layer settings: %v", err)
	}
	settings, err := parseFastPreviewSettings(body, seed)
	if err != nil {
		t.Fatalf("parse fast natural-layer settings: %v", err)
	}
	img, _, err := renderFastMapPreview(
		context.Background(),
		settings,
		size,
		previewZoom{mode: "scale", tilesPerPixel: tilesPerPixel, renderSize: size},
	)
	if err != nil {
		t.Fatalf("render fast natural layer: %v", err)
	}
	return img
}

func previewOverlayInk(base, overlay image.Image, blockSize int) ([]float64, float64) {
	bounds := base.Bounds()
	blocksX := (bounds.Dx() + blockSize - 1) / blockSize
	blocksY := (bounds.Dy() + blockSize - 1) / blockSize
	blocks := make([]float64, blocksX*blocksY)
	total := 0.0
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			br, bg, bb, _ := base.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			or, og, ob, _ := overlay.At(overlay.Bounds().Min.X+x, overlay.Bounds().Min.Y+y).RGBA()
			delta := float64(absInt(int(br>>8)-int(or>>8)) + absInt(int(bg>>8)-int(og>>8)) + absInt(int(bb>>8)-int(ob>>8)))
			blocks[(y/blockSize)*blocksX+x/blockSize] += delta
			total += delta
		}
	}
	return blocks, total / float64(bounds.Dx()*bounds.Dy()*3)
}

func previewPearsonCorrelation(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	meanA := 0.0
	meanB := 0.0
	for index := range a {
		meanA += a[index]
		meanB += b[index]
	}
	meanA /= float64(len(a))
	meanB /= float64(len(b))
	numerator := 0.0
	denominatorA := 0.0
	denominatorB := 0.0
	for index := range a {
		deltaA := a[index] - meanA
		deltaB := b[index] - meanB
		numerator += deltaA * deltaB
		denominatorA += deltaA * deltaA
		denominatorB += deltaB * deltaB
	}
	if denominatorA == 0 || denominatorB == 0 {
		return 0
	}
	return numerator / math.Sqrt(denominatorA*denominatorB)
}

func previewColorMask(img image.Image, target color.RGBA) []bool {
	bounds := img.Bounds()
	mask := make([]bool, bounds.Dx()*bounds.Dy())
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			pixel := color.RGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.RGBA)
			mask[y*bounds.Dx()+x] = pixel.R == target.R && pixel.G == target.G && pixel.B == target.B
		}
	}
	return mask
}

func previewMaskCount(mask []bool) int {
	count := 0
	for _, value := range mask {
		if value {
			count++
		}
	}
	return count
}

func previewMaskTolerance(want, actual []bool, width, height, radius int) float64 {
	wantCount := previewMaskCount(want)
	if wantCount == 0 {
		return 0
	}
	matched := 0
	for index, value := range want {
		if !value {
			continue
		}
		x := index % width
		y := index / width
		found := false
		for dy := -radius; dy <= radius && !found; dy++ {
			checkY := y + dy
			if checkY < 0 || checkY >= height {
				continue
			}
			for dx := -radius; dx <= radius; dx++ {
				checkX := x + dx
				if checkX >= 0 && checkX < width && actual[checkY*width+checkX] {
					found = true
					break
				}
			}
		}
		if found {
			matched++
		}
	}
	return float64(matched) / float64(wantCount)
}

func writeNaturalPreviewArtifacts(
	t *testing.T,
	seed string,
	factorioTrees, fastTrees, factorioCliffs, fastCliffs image.Image,
	stats naturalPreviewStats,
) {
	t.Helper()
	dir := filepath.Join("test-output", "preview-diffs", safeArtifactName(t.Name()), "seed-"+seed)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create natural preview artifacts: %v", err)
	}
	for name, img := range map[string]image.Image{
		"trees-factorio.png":  factorioTrees,
		"trees-fast.png":      fastTrees,
		"cliffs-factorio.png": factorioCliffs,
		"cliffs-fast.png":     fastCliffs,
	} {
		writePNGArtifact(t, filepath.Join(dir, name), img)
	}
	_, treeDiff, _ := previewImageDiff(factorioTrees, fastTrees)
	_, cliffDiff, _ := previewImageDiff(factorioCliffs, fastCliffs)
	writePNGArtifact(t, filepath.Join(dir, "trees-diff.png"), treeDiff)
	writePNGArtifact(t, filepath.Join(dir, "cliffs-diff.png"), cliffDiff)
	body, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatalf("marshal natural preview stats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stats.json"), append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write natural preview stats: %v", err)
	}
}
