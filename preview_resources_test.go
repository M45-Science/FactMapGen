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
	resourcePreviewParityEnv  = "FACTMAPGEN_RESOURCE_PREVIEW_PARITY"
	resourcePreviewGalleryEnv = "FACTMAPGEN_RESOURCE_PREVIEW_GALLERY"
)

type resourcePreviewStats struct {
	Seed                      string  `json:"seed"`
	TilesPerPixel             float64 `json:"tilesPerPixel"`
	ResourceRegionCorrelation float64 `json:"resourceRegionCorrelation"`
	ResourceFastToGameInk     float64 `json:"resourceFastToGameInk"`
	FactorioResourcePixels    int     `json:"factorioResourcePixels"`
	FastResourcePixels        int     `json:"fastResourcePixels"`
	OilFastToGameInk          float64 `json:"oilFastToGameInk"`
	OilRegionCorrelation      float64 `json:"oilRegionCorrelation"`
	OilRecall                 float64 `json:"oilRecall"`
	OilPrecision              float64 `json:"oilPrecision"`
	FactorioOilPixels         int     `json:"factorioOilPixels"`
	FastOilPixels             int     `json:"fastOilPixels"`
	RockRegionCorrelation     float64 `json:"rockRegionCorrelation"`
	RockFastToGameInk         float64 `json:"rockFastToGameInk"`
	FactorioRockPixels        int     `json:"factorioRockPixels"`
	FastRockPixels            int     `json:"fastRockPixels"`
}

func TestFastResourceLayersMatchFactorioPreviewRegions(t *testing.T) {
	if os.Getenv(resourcePreviewParityEnv) != "1" {
		t.Skipf("set %s=1 to compare resource and rock regions against Factorio", resourcePreviewParityEnv)
	}
	if testing.Short() {
		t.Skip("skipping resource-layer Factorio integration test in short mode")
	}
	const (
		outputSize    = 512
		tilesPerPixel = 2.0
	)
	factorioBin := ""
	store := newTestStore(t)
	_, sourcePath := previewParityProfile(t, store, "default:Default")
	mapGenPath := resourceLayerMapGenPath(t, sourcePath, true)

	for _, seed := range []string{"123456", "777771"} {
		t.Run("seed-"+seed, func(t *testing.T) {
			factorioImg, _ := cachedFactorioResourcePreview(
				t, &factorioBin, mapGenPath, "resources-rocks", seed, outputSize, tilesPerPixel,
			)
			fastImg := renderFastResourceLayer(t, mapGenPath, seed, outputSize, tilesPerPixel)
			stats := measureResourcePreview(seed, tilesPerPixel, factorioImg, fastImg)
			writeResourcePreviewArtifacts(t, seed, factorioImg, fastImg, stats)

			if stats.FactorioResourcePixels == 0 || stats.FastResourcePixels == 0 {
				t.Fatalf("resource mask is empty: Factorio=%d fast=%d", stats.FactorioResourcePixels, stats.FastResourcePixels)
			}
			if stats.ResourceRegionCorrelation < 0.75 ||
				stats.ResourceFastToGameInk < 0.55 || stats.ResourceFastToGameInk > 1.55 {
				t.Errorf(
					"resource regions diverged: correlation=%.3f fast/game ink=%.3f",
					stats.ResourceRegionCorrelation,
					stats.ResourceFastToGameInk,
				)
			}
			if stats.FactorioOilPixels >= 8 &&
				(stats.OilRegionCorrelation < 0.75 || stats.OilRecall < 0.30 || stats.OilPrecision < 0.30 ||
					stats.OilFastToGameInk < 0.35 || stats.OilFastToGameInk > 2.1) {
				t.Errorf("oil regions diverged: correlation=%.3f recall=%.3f precision=%.3f fast/game ink=%.3f", stats.OilRegionCorrelation, stats.OilRecall, stats.OilPrecision, stats.OilFastToGameInk)
			}
			if stats.FactorioRockPixels > 0 && stats.FastRockPixels > 0 &&
				(stats.RockRegionCorrelation < 0.35 || stats.RockFastToGameInk < 0.20 || stats.RockFastToGameInk > 4) {
				t.Errorf(
					"rock regions diverged: correlation=%.3f fast/game ink=%.3f",
					stats.RockRegionCorrelation,
					stats.RockFastToGameInk,
				)
			}
			t.Logf(
				"resources correlation=%.3f fast/game ink=%.3f; oil correlation=%.3f recall=%.3f precision=%.3f fast/game ink=%.3f; rocks correlation=%.3f fast/game ink=%.3f",
				stats.ResourceRegionCorrelation,
				stats.ResourceFastToGameInk,
				stats.OilRegionCorrelation,
				stats.OilRecall,
				stats.OilPrecision,
				stats.OilFastToGameInk,
				stats.RockRegionCorrelation,
				stats.RockFastToGameInk,
			)
		})
	}
}

func TestPreviewGalleryResourceLayersDefaultSeeds(t *testing.T) {
	if os.Getenv(resourcePreviewGalleryEnv) != "1" {
		t.Skipf("set %s=1 to render the Factorio-vs-fast resource and rock gallery", resourcePreviewGalleryEnv)
	}
	if testing.Short() {
		t.Skip("skipping resource-layer preview gallery in short mode")
	}
	const (
		outputSize    = 512
		tilesPerPixel = 2.0
	)
	factorioBin := ""
	store := newTestStore(t)
	_, sourcePath := previewParityProfile(t, store, "default:Default")
	mapGenPath := resourceLayerMapGenPath(t, sourcePath, true)
	outDir := filepath.Join("test-output", "preview-gallery", "default-10-seeds-resources-oil-rocks")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create resource preview gallery: %v", err)
	}

	type row struct {
		Index               int
		Seed                string
		FactorioPath        string
		FastPath            string
		DiffPath            string
		ResourceCorrelation string
		ResourceInkRatio    string
		RockCorrelation     string
		RockInkRatio        string
		OilInkRatio         string
		OilCorrelation      string
		OilRecall           string
		OilPrecision        string
		ChangedPercent      string
		Reference           string
	}
	rows := make([]row, 0, 10)
	cachedReferences := 0
	for index, seed := range previewGallerySeeds(10) {
		seedText := fmt.Sprint(seed)
		prefix := fmt.Sprintf("seed-%02d-%s", index+1, seedText)
		factorioName := prefix + "-factorio.png"
		fastName := prefix + "-fast.png"
		diffName := prefix + "-diff.png"
		factorioImg, cached := cachedFactorioResourcePreview(
			t, &factorioBin, mapGenPath, "resources-rocks", seedText, outputSize, tilesPerPixel,
		)
		reference := "rendered"
		if cached {
			reference = "cached"
			cachedReferences++
		}
		fastImg := renderFastResourceLayer(t, mapGenPath, seedText, outputSize, tilesPerPixel)
		stats := measureResourcePreview(seedText, tilesPerPixel, factorioImg, fastImg)
		diffStats, diffImg, _ := previewImageDiff(factorioImg, fastImg)
		if stats.ResourceRegionCorrelation < 0.70 ||
			stats.ResourceFastToGameInk < 0.50 || stats.ResourceFastToGameInk > 1.60 {
			t.Errorf(
				"seed %s resource regions diverged: correlation=%.3f fast/game ink=%.3f",
				seedText,
				stats.ResourceRegionCorrelation,
				stats.ResourceFastToGameInk,
			)
		}
		if stats.FactorioOilPixels >= 8 &&
			(stats.OilRegionCorrelation < 0.75 || stats.OilRecall < 0.30 || stats.OilPrecision < 0.30 ||
				stats.OilFastToGameInk < 0.35 || stats.OilFastToGameInk > 2.1) {
			t.Errorf("seed %s oil regions diverged: correlation=%.3f recall=%.3f precision=%.3f fast/game ink=%.3f", seedText, stats.OilRegionCorrelation, stats.OilRecall, stats.OilPrecision, stats.OilFastToGameInk)
		}
		if stats.FactorioRockPixels > 0 && stats.FastRockPixels > 0 &&
			(stats.RockRegionCorrelation < 0.70 || stats.RockFastToGameInk < 0.20 || stats.RockFastToGameInk > 2.5) {
			t.Errorf(
				"seed %s rock regions diverged: correlation=%.3f fast/game ink=%.3f",
				seedText,
				stats.RockRegionCorrelation,
				stats.RockFastToGameInk,
			)
		}

		writePNGArtifact(t, filepath.Join(outDir, factorioName), factorioImg)
		writePNGArtifact(t, filepath.Join(outDir, fastName), fastImg)
		writePNGArtifact(t, filepath.Join(outDir, diffName), diffImg)
		body, err := json.MarshalIndent(struct {
			Resource resourcePreviewStats `json:"layers"`
			Image    previewDiffStats     `json:"image"`
		}{Resource: stats, Image: diffStats}, "", "  ")
		if err != nil {
			t.Fatalf("marshal resource gallery stats for seed %s: %v", seedText, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, prefix+"-stats.json"), append(body, '\n'), 0o644); err != nil {
			t.Fatalf("write resource gallery stats for seed %s: %v", seedText, err)
		}
		rows = append(rows, row{
			Index:               index + 1,
			Seed:                seedText,
			FactorioPath:        factorioName,
			FastPath:            fastName,
			DiffPath:            diffName,
			ResourceCorrelation: fmt.Sprintf("%.3f", stats.ResourceRegionCorrelation),
			ResourceInkRatio:    fmt.Sprintf("%.3f", stats.ResourceFastToGameInk),
			RockCorrelation:     fmt.Sprintf("%.3f", stats.RockRegionCorrelation),
			RockInkRatio:        fmt.Sprintf("%.3f", stats.RockFastToGameInk),
			OilInkRatio:         fmt.Sprintf("%.3f", stats.OilFastToGameInk),
			OilCorrelation:      fmt.Sprintf("%.3f", stats.OilRegionCorrelation),
			OilRecall:           fmt.Sprintf("%.3f", stats.OilRecall),
			OilPrecision:        fmt.Sprintf("%.3f", stats.OilPrecision),
			ChangedPercent:      fmt.Sprintf("%.3f", diffStats.ChangedPercent),
			Reference:           reference,
		})
	}
	writeResourcePreviewGalleryHTML(t, filepath.Join(outDir, "index.html"), rows)
	t.Logf("wrote resource-layer gallery to %s; reused %d/10 Factorio references", outDir, cachedReferences)
}

func resourceLayerMapGenPath(t *testing.T, sourcePath string, rocks bool) string {
	t.Helper()
	body, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read resource-layer map settings: %v", err)
	}
	body, err = resourceLayersMapGenJSON(body, rocks)
	if err != nil {
		t.Fatalf("build resource-layer map settings: %v", err)
	}
	path := filepath.Join(t.TempDir(), fmt.Sprintf("resources-rocks-%t-map-gen-settings.json", rocks))
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write resource-layer map settings: %v", err)
	}
	return path
}

func resourceLayersMapGenJSON(body []byte, rocks bool) ([]byte, error) {
	body, err := naturalLayersMapGenJSON(body, false, false)
	if err != nil {
		return nil, err
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	controls := fastMap(root["autoplace_controls"])
	for _, params := range factorioResourceCatalog {
		controls[params.name] = map[string]any{"frequency": 1, "size": 1, "richness": 1}
	}
	if rocks {
		controls["rocks"] = map[string]any{"frequency": 1, "size": 1, "richness": 1}
	}
	root["autoplace_controls"] = controls
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

func cachedFactorioResourcePreview(
	t *testing.T,
	factorioBin *string,
	mapGenPath, layer, seed string,
	outputSize int,
	tilesPerPixel float64,
) (image.Image, bool) {
	t.Helper()
	mapGenBody, err := os.ReadFile(mapGenPath)
	if err != nil {
		t.Fatalf("read resource preview cache settings: %v", err)
	}
	settingsHash := sha256.Sum256(mapGenBody)
	cachePath := filepath.Join(
		"test-output",
		"preview-cache",
		"resource-layers",
		fmt.Sprintf("seed-%s-%s-%dpx-%gtpp-%x-factorio.png", seed, layer, outputSize, tilesPerPixel, settingsHash[:6]),
	)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("create resource preview cache: %v", err)
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

func renderFastResourceLayer(t *testing.T, mapGenPath, seed string, size int, tilesPerPixel float64) image.Image {
	t.Helper()
	body, err := readNormalizedMapGenJSON(mapGenPath)
	if err != nil {
		t.Fatalf("read fast resource-layer settings: %v", err)
	}
	settings, err := parseFastPreviewSettings(body, seed)
	if err != nil {
		t.Fatalf("parse fast resource-layer settings: %v", err)
	}
	img, _, err := renderFastMapPreview(
		context.Background(),
		settings,
		size,
		previewZoom{mode: "scale", tilesPerPixel: tilesPerPixel, renderSize: size},
	)
	if err != nil {
		t.Fatalf("render fast resource layer: %v", err)
	}
	return img
}

func measureResourcePreview(seed string, tilesPerPixel float64, factorioImg, fastImg image.Image) resourcePreviewStats {
	factorioResourceMask := previewResourceMask(factorioImg)
	fastResourceMask := previewResourceMask(fastImg)
	factorioRockMask := previewColorMask(factorioImg, factorioRockMapColor)
	fastRockMask := previewColorMask(fastImg, factorioRockMapColor)
	factorioOilMask := previewColorMask(factorioImg, factorioResourceCatalog[4].mapColor)
	fastOilMask := previewColorMask(fastImg, factorioResourceCatalog[4].mapColor)
	width := min(factorioImg.Bounds().Dx(), fastImg.Bounds().Dx())
	height := min(factorioImg.Bounds().Dy(), fastImg.Bounds().Dy())
	factorioResourcePixels := previewMaskCount(factorioResourceMask)
	fastResourcePixels := previewMaskCount(fastResourceMask)
	factorioRockPixels := previewMaskCount(factorioRockMask)
	fastRockPixels := previewMaskCount(fastRockMask)
	factorioOilPixels := previewMaskCount(factorioOilMask)
	fastOilPixels := previewMaskCount(fastOilMask)
	return resourcePreviewStats{
		Seed:                      seed,
		TilesPerPixel:             tilesPerPixel,
		ResourceRegionCorrelation: previewPearsonCorrelation(previewMaskBlocks(factorioResourceMask, width, height, 16), previewMaskBlocks(fastResourceMask, width, height, 16)),
		ResourceFastToGameInk:     previewCountRatio(fastResourcePixels, factorioResourcePixels),
		FactorioResourcePixels:    factorioResourcePixels,
		FastResourcePixels:        fastResourcePixels,
		OilFastToGameInk:          previewCountRatio(fastOilPixels, factorioOilPixels),
		OilRegionCorrelation:      previewPearsonCorrelation(previewMaskBlocks(factorioOilMask, width, height, 32), previewMaskBlocks(fastOilMask, width, height, 32)),
		OilRecall:                 previewMaskTolerance(factorioOilMask, fastOilMask, width, height, 1),
		OilPrecision:              previewMaskTolerance(fastOilMask, factorioOilMask, width, height, 1),
		FactorioOilPixels:         factorioOilPixels,
		FastOilPixels:             fastOilPixels,
		RockRegionCorrelation:     previewPearsonCorrelation(previewMaskBlocks(factorioRockMask, width, height, 32), previewMaskBlocks(fastRockMask, width, height, 32)),
		RockFastToGameInk:         previewCountRatio(fastRockPixels, factorioRockPixels),
		FactorioRockPixels:        factorioRockPixels,
		FastRockPixels:            fastRockPixels,
	}
}

func previewResourceMask(img image.Image) []bool {
	bounds := img.Bounds()
	mask := make([]bool, bounds.Dx()*bounds.Dy())
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			pixel := colorRGBA8(img.At(bounds.Min.X+x, bounds.Min.Y+y))
			for _, params := range factorioResourceCatalog {
				if pixel == params.mapColor {
					mask[y*bounds.Dx()+x] = true
					break
				}
			}
		}
	}
	return mask
}

func previewMaskBlocks(mask []bool, width, height, blockSize int) []float64 {
	blocksX := (width + blockSize - 1) / blockSize
	blocksY := (height + blockSize - 1) / blockSize
	blocks := make([]float64, blocksX*blocksY)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if mask[y*width+x] {
				blocks[(y/blockSize)*blocksX+x/blockSize]++
			}
		}
	}
	return blocks
}

func previewCountRatio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func colorRGBA8(value color.Color) color.RGBA {
	r, g, b, a := value.RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

func writeResourcePreviewArtifacts(
	t *testing.T,
	seed string,
	factorioImg, fastImg image.Image,
	stats resourcePreviewStats,
) {
	t.Helper()
	_, diffImg, _ := previewImageDiff(factorioImg, fastImg)
	dir := filepath.Join("test-output", "preview-diffs", "resources-seed-"+seed)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create resource preview artifacts: %v", err)
	}
	writePNGArtifact(t, filepath.Join(dir, "factorio.png"), factorioImg)
	writePNGArtifact(t, filepath.Join(dir, "fast.png"), fastImg)
	writePNGArtifact(t, filepath.Join(dir, "diff.png"), diffImg)
	body, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatalf("marshal resource preview stats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stats.json"), append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write resource preview stats: %v", err)
	}
}

func writeResourcePreviewGalleryHTML(t *testing.T, path string, rows any) {
	t.Helper()
	const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>FactMapGen Resources, Oil, and Rocks Preview Gallery</title>
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
  <h1>Default Resources + Oil + Rocks Comparison</h1>
  <p class="meta">512px at 2 meters per pixel. Trees, enemies, cliffs, fish, and decorations are disabled. Solid-resource footprints use Factorio-compatible fields. Oil uses Factorio's chunk-ordered random-penalty stream and chart footprint with an approximated final shared-autoplace roll. Left: cached Factorio headless preview. Middle: FactMapGen fast Go preview. Right: amplified RGB diff.</p>
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
        <td class="meta">ore corr {{.ResourceCorrelation}}<br>ore fast/game {{.ResourceInkRatio}}<br>oil corr {{.OilCorrelation}}<br>oil recall {{.OilRecall}}<br>oil precision {{.OilPrecision}}<br>oil fast/game {{.OilInkRatio}}<br>rock corr {{.RockCorrelation}}<br>rock fast/game {{.RockInkRatio}}<br>{{.ChangedPercent}}% RGB changed<br>Factorio: {{.Reference}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>
`
	tmpl, err := template.New("resource-gallery").Parse(page)
	if err != nil {
		t.Fatalf("parse resource gallery template: %v", err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create resource gallery HTML: %v", err)
	}
	defer out.Close()
	if err := tmpl.Execute(out, rows); err != nil {
		t.Fatalf("write resource gallery HTML: %v", err)
	}
}

func TestResourceLayersMapGenJSONEnablesOnlyResourcesAndOptionalRocks(t *testing.T) {
	body, err := resourceLayersMapGenJSON([]byte(`{
		"autoplace_controls": {"water": {"frequency": 1, "size": 1, "richness": 1}},
		"autoplace_settings": {},
		"cliff_settings": {"richness": 1}
	}`), true)
	if err != nil {
		t.Fatalf("resourceLayersMapGenJSON: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("decode resource layer settings: %v", err)
	}
	controls := fastMap(root["autoplace_controls"])
	for _, params := range factorioResourceCatalog {
		control := fastMap(controls[params.name])
		if fastNumber(control["frequency"], 0) != 1 || fastNumber(control["size"], 0) != 1 {
			t.Errorf("resource control %s is not enabled: %#v", params.name, control)
		}
	}
	for _, name := range []string{"trees", "enemy-base", "nauvis_cliff"} {
		control := fastMap(controls[name])
		if fastNumber(control["frequency"], -1) != 0 || fastNumber(control["size"], -1) != 0 {
			t.Errorf("control %s was not disabled: %#v", name, control)
		}
	}
	if rock := fastMap(controls["rocks"]); fastNumber(rock["frequency"], 0) != 1 {
		t.Errorf("rocks control was not enabled: %#v", rock)
	}
}
