package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

const (
	maxTerrainChangedPercent = 10.0
	maxWaterChangedPercent   = 1.0
)

var terrainOnlyAutoplaceControls = []string{
	"iron-ore",
	"copper-ore",
	"coal",
	"stone",
	"crude-oil",
	"uranium-ore",
	"trees",
	"rocks",
	"enemy-base",
	"nauvis_cliff",
}

func terrainOnlyMapGenPath(t *testing.T, sourcePath string) string {
	t.Helper()
	body, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read map-gen settings for terrain comparison: %v", err)
	}
	body, err = terrainOnlyMapGenJSON(body)
	if err != nil {
		t.Fatalf("build terrain-only map-gen settings: %v", err)
	}
	path := filepath.Join(t.TempDir(), "terrain-only-map-gen-settings.json")
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write terrain-only map-gen settings: %v", err)
	}
	return path
}

func terrainOnlyMapGenJSON(body []byte) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	root["default_enable_all_autoplace_controls"] = false
	controls := fastMap(root["autoplace_controls"])
	for _, name := range terrainOnlyAutoplaceControls {
		controls[name] = map[string]any{"frequency": 0, "size": 0, "richness": 0}
	}
	root["autoplace_controls"] = controls

	autoplace := fastMap(root["autoplace_settings"])
	autoplace["entity"] = map[string]any{"treat_missing_as_default": false, "settings": map[string]any{}}
	autoplace["decorative"] = map[string]any{"treat_missing_as_default": false, "settings": map[string]any{}}
	root["autoplace_settings"] = autoplace
	root["no_enemies_mode"] = true
	root["peaceful_mode"] = true
	cliffs := fastMap(root["cliff_settings"])
	cliffs["richness"] = 0
	root["cliff_settings"] = cliffs
	return json.MarshalIndent(root, "", "  ")
}

func cachedPreviewImage(path string, size int) (image.Image, bool) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	img, err := png.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	bounds := img.Bounds()
	if bounds.Dx() != size || bounds.Dy() != size {
		return nil, false
	}
	return img, true
}

func galleryReferenceImage(t *testing.T, path string, size int, render func() image.Image) (image.Image, bool) {
	t.Helper()
	if img, ok := cachedPreviewImage(path, size); ok {
		return img, true
	}
	img := render()
	writePNGArtifact(t, path, img)
	return img, false
}

func assertFastTerrainCorrectness(t *testing.T, caseName string, want, actual image.Image) {
	t.Helper()
	stats, diff, _ := previewImageDiff(want, actual)
	stats.WaterMaskChangedPercent = previewWaterMaskChangedPercent(want, actual)
	dir := writePreviewDiffArtifacts(t, caseName, want, actual, diff, stats)
	if stats.ChangedPercent > maxTerrainChangedPercent || stats.WaterMaskChangedPercent > maxWaterChangedPercent {
		t.Fatalf(
			"terrain mismatch for %s: %.2f%% tile colors changed (limit %.2f%%), %.2f%% water mask changed (limit %.2f%%); wrote artifacts to %s",
			caseName,
			stats.ChangedPercent,
			maxTerrainChangedPercent,
			stats.WaterMaskChangedPercent,
			maxWaterChangedPercent,
			dir,
		)
	}
	t.Logf(
		"terrain correctness for %s: %.2f%% tile colors changed, %.2f%% water mask changed; artifacts: %s",
		caseName,
		stats.ChangedPercent,
		stats.WaterMaskChangedPercent,
		dir,
	)
}

func previewWaterMaskChangedPercent(want, actual image.Image) float64 {
	wantBounds := want.Bounds()
	actualBounds := actual.Bounds()
	width := min(wantBounds.Dx(), actualBounds.Dx())
	height := min(wantBounds.Dy(), actualBounds.Dy())
	if width <= 0 || height <= 0 {
		return 100
	}
	changed := 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if isFactorioWaterColor(want.At(wantBounds.Min.X+x, wantBounds.Min.Y+y)) !=
				isFactorioWaterColor(actual.At(actualBounds.Min.X+x, actualBounds.Min.Y+y)) {
				changed++
			}
		}
	}
	return float64(changed) * 100 / float64(width*height)
}

func isFactorioWaterColor(value color.Color) bool {
	r, g, b, _ := value.RGBA()
	r8, g8, b8 := uint8(r>>8), uint8(g>>8), uint8(b>>8)
	return r8 == 38 && g8 == 64 && b8 == 73 ||
		r8 == 51 && g8 == 83 && b8 == 95
}

func TestTerrainOnlyMapGenJSONDisablesOverlays(t *testing.T) {
	body, err := terrainOnlyMapGenJSON([]byte(`{
		"autoplace_controls": {"water": {"frequency": 1, "size": 1, "richness": 1}},
		"autoplace_settings": {"tile": {"treat_missing_as_default": true}},
		"cliff_settings": {"richness": 1}
	}`))
	if err != nil {
		t.Fatalf("terrainOnlyMapGenJSON: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		t.Fatalf("decode terrain-only settings: %v", err)
	}
	controls := fastMap(root["autoplace_controls"])
	for _, name := range terrainOnlyAutoplaceControls {
		control := fastMap(controls[name])
		if fastNumber(control["frequency"], -1) != 0 || fastNumber(control["size"], -1) != 0 {
			t.Errorf("control %s was not disabled: %#v", name, control)
		}
	}
	if _, ok := fastMap(root["autoplace_settings"])["tile"]; !ok {
		t.Fatal("terrain tile autoplace settings were not preserved")
	}
	if !fastBool(root["no_enemies_mode"]) || !fastBool(root["peaceful_mode"]) {
		t.Fatal("enemy modes were not disabled")
	}
	if got := fastNumber(fastMap(root["cliff_settings"])["richness"], -1); got != 0 {
		t.Fatalf("cliff richness = %g, want 0", got)
	}
}

func TestGalleryReferenceImageReusesValidFactorioPNG(t *testing.T) {
	path := filepath.Join(t.TempDir(), "factorio.png")
	want := image.NewRGBA(image.Rect(0, 0, 16, 16))
	want.SetRGBA(3, 4, color.RGBA{R: 55, G: 53, B: 11, A: 255})
	writePNGArtifact(t, path, want)

	renderCalls := 0
	got, cached := galleryReferenceImage(t, path, 16, func() image.Image {
		renderCalls++
		return image.NewRGBA(image.Rect(0, 0, 16, 16))
	})
	if !cached || renderCalls != 0 {
		t.Fatalf("cached = %v, render calls = %d; want cached without rendering", cached, renderCalls)
	}
	if color.RGBAModel.Convert(got.At(3, 4)) != color.RGBAModel.Convert(want.At(3, 4)) {
		t.Fatal("cached image content changed")
	}

	missingPath := filepath.Join(t.TempDir(), "missing.png")
	_, cached = galleryReferenceImage(t, missingPath, 16, func() image.Image {
		renderCalls++
		return want
	})
	if cached || renderCalls != 1 {
		t.Fatalf("missing cached = %v, render calls = %d; want one render", cached, renderCalls)
	}
	if _, ok := cachedPreviewImage(missingPath, 16); !ok {
		t.Fatal("rendered gallery reference was not persisted")
	}
}
