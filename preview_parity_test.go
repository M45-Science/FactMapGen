package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fastPreviewParityEnv = "FACTMAPGEN_FAST_PREVIEW_PARITY"
const previewGalleryEnv = "FACTMAPGEN_PREVIEW_GALLERY"

type previewParityCase struct {
	name    string
	profile string
	seed    string
	size    int
}

type previewDiffStats struct {
	WaterMaskChangedPercent float64 `json:"waterMaskChangedPercent"`
	Width                   int     `json:"width"`
	Height                  int     `json:"height"`
	ChangedPixels           int     `json:"changedPixels"`
	TotalPixels             int     `json:"totalPixels"`
	ChangedPercent          float64 `json:"changedPercent"`
	MaxChannelDelta         int     `json:"maxChannelDelta"`
	MeanAbsDelta            float64 `json:"meanAbsDelta"`
}

func TestExactPreviewMatchesDirectFactorioPreview(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Factorio preview parity integration test in short mode")
	}
	factorioBin := requirePreviewParityFactorio(t)
	st := newTestStore(t)
	for _, tc := range previewParityCases() {
		t.Run(tc.name, func(t *testing.T) {
			ref, mapGenPath := previewParityProfile(t, st, tc.profile)
			wantPNG := renderFactorioPreviewPNG(t, factorioBin, mapGenPath, tc.size, tc.seed)

			p := &previewer{factorioBin: factorioBin, timeout: 90 * time.Second}
			resp, err := p.render(context.Background(), ref, mapGenPath, previewRequest{
				Size:   tc.size,
				Planet: "nauvis",
				Seed:   tc.seed,
				Zoom:   "1",
			}, false)
			if err != nil {
				t.Fatalf("render exact preview: %v", err)
			}
			actual := storedPreviewPNG(t, p, resp.URL)
			assertPreviewImagesEqual(t, tc.name, decodePNG(t, wantPNG), decodePNG(t, actual))
		})
	}
}

func TestFastPreviewMatchesFactorioPreview(t *testing.T) {
	if os.Getenv(fastPreviewParityEnv) != "1" {
		t.Skipf("set %s=1 to run the fast Go renderer against Factorio and write image diffs", fastPreviewParityEnv)
	}
	if testing.Short() {
		t.Skip("skipping fast preview parity integration test in short mode")
	}
	factorioBin := requirePreviewParityFactorio(t)
	st := newTestStore(t)
	for _, tc := range fastTerrainParityCases() {
		t.Run(tc.name, func(t *testing.T) {
			_, mapGenPath := previewParityProfile(t, st, tc.profile)
			terrainMapGenPath := terrainOnlyMapGenPath(t, mapGenPath)
			wantPNG := renderFactorioPreviewPNG(t, factorioBin, terrainMapGenPath, tc.size, tc.seed)
			mapGen, err := readNormalizedMapGenJSON(terrainMapGenPath)
			if err != nil {
				t.Fatalf("read map-gen settings: %v", err)
			}
			settings, err := parseFastPreviewSettings(mapGen, tc.seed)
			if err != nil {
				t.Fatalf("parse fast preview settings: %v", err)
			}
			actual, _, err := renderFastMapPreview(context.Background(), settings, tc.size, previewZoom{mode: "normal", tilesPerPixel: 1, renderSize: tc.size})
			if err != nil {
				t.Fatalf("render fast preview: %v", err)
			}
			assertFastTerrainCorrectness(t, tc.name, decodePNG(t, wantPNG), actual)
		})
	}
}

func TestPreviewGalleryDefaultSeeds(t *testing.T) {
	if os.Getenv(previewGalleryEnv) != "1" {
		t.Skipf("set %s=1 to render the Factorio-vs-fast preview gallery", previewGalleryEnv)
	}
	if testing.Short() {
		t.Skip("skipping preview gallery integration test in short mode")
	}
	factorioBin := ""
	st := newTestStore(t)
	_, mapGenPath := previewParityProfile(t, st, "default:Default")
	terrainMapGenPath := terrainOnlyMapGenPath(t, mapGenPath)
	mapGen, err := readNormalizedMapGenJSON(terrainMapGenPath)
	if err != nil {
		t.Fatalf("read default map-gen settings: %v", err)
	}

	outDir := filepath.Join("test-output", "preview-gallery", "default-10-seeds-terrain")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create preview gallery dir: %v", err)
	}

	type row struct {
		Index          int
		Seed           string
		FactorioPath   string
		FastPath       string
		DiffPath       string
		ChangedPercent string
		MaxDelta       int
		WaterPercent   string
		Reference      string
	}
	rows := []row{}
	cachedReferences := 0
	for i, seed := range previewGallerySeeds(10) {
		seedText := fmt.Sprint(seed)
		prefix := fmt.Sprintf("seed-%02d-%s", i+1, seedText)
		factorioName := prefix + "-factorio.png"
		fastName := prefix + "-fast.png"
		diffName := prefix + "-diff.png"
		factorioPath := filepath.Join(outDir, factorioName)
		factorioImg, cached := galleryReferenceImage(t, factorioPath, 256, func() image.Image {
			if factorioBin == "" {
				factorioBin = requirePreviewParityFactorio(t)
			}
			factorioPNG := renderFactorioPreviewPNG(t, factorioBin, terrainMapGenPath, 256, seedText)
			return decodePNG(t, factorioPNG)
		})
		reference := "cached"
		if cached {
			cachedReferences++
		} else {
			reference = "rendered"
		}
		settings, err := parseFastPreviewSettings(mapGen, seedText)
		if err != nil {
			t.Fatalf("parse fast settings for seed %s: %v", seedText, err)
		}
		fastImg, _, err := renderFastMapPreview(context.Background(), settings, 256, previewZoom{mode: "normal", tilesPerPixel: 1, renderSize: 256})
		if err != nil {
			t.Fatalf("render fast preview for seed %s: %v", seedText, err)
		}
		stats, diffImg, _ := previewImageDiff(factorioImg, fastImg)
		stats.WaterMaskChangedPercent = previewWaterMaskChangedPercent(factorioImg, fastImg)

		writePNGArtifact(t, filepath.Join(outDir, fastName), fastImg)
		writePNGArtifact(t, filepath.Join(outDir, diffName), diffImg)
		statsJSON, err := json.MarshalIndent(stats, "", "  ")
		if err != nil {
			t.Fatalf("marshal stats for seed %s: %v", seedText, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, prefix+"-stats.json"), append(statsJSON, '\n'), 0o644); err != nil {
			t.Fatalf("write stats for seed %s: %v", seedText, err)
		}

		rows = append(rows, row{
			Index:          i + 1,
			Seed:           seedText,
			FactorioPath:   factorioName,
			FastPath:       fastName,
			DiffPath:       diffName,
			ChangedPercent: fmt.Sprintf("%.4f", stats.ChangedPercent),
			MaxDelta:       stats.MaxChannelDelta,
			WaterPercent:   fmt.Sprintf("%.4f", stats.WaterMaskChangedPercent),
			Reference:      reference,
		})
	}
	writePreviewGalleryHTML(t, filepath.Join(outDir, "index.html"), rows)
	t.Logf("wrote preview gallery to %s; reused %d/10 Factorio references", outDir, cachedReferences)
}

func previewGallerySeeds(n int) []uint32 {
	rng := rand.New(rand.NewSource(20260726))
	seeds := make([]uint32, 0, n)
	for len(seeds) < n {
		seed := rng.Uint32()
		if seed == 0 {
			continue
		}
		seeds = append(seeds, seed)
	}
	return seeds
}

func TestPreviewDiffArtifacts(t *testing.T) {
	want := image.NewRGBA(image.Rect(0, 0, 2, 1))
	want.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	want.SetRGBA(1, 0, color.RGBA{R: 40, G: 50, B: 60, A: 255})
	actual := image.NewRGBA(image.Rect(0, 0, 2, 1))
	actual.SetRGBA(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	actual.SetRGBA(1, 0, color.RGBA{R: 45, G: 50, B: 55, A: 255})

	stats, diff, same := previewImageDiff(want, actual)
	if same {
		t.Fatal("previewImageDiff reported different images as equal")
	}
	if stats.ChangedPixels != 1 || stats.TotalPixels != 2 || stats.MaxChannelDelta != 5 {
		t.Fatalf("diff stats = %#v, want one changed pixel and max delta 5", stats)
	}
	dir := writePreviewDiffArtifacts(t, "artifact-smoke", want, actual, diff, stats)
	defer os.RemoveAll(filepath.Join("test-output", "preview-diffs", safeArtifactName(t.Name())))
	for _, name := range []string{"factorio.png", "actual.png", "diff.png", "stats.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("diff artifact %s missing: %v", name, err)
		}
	}
}

func writePreviewGalleryHTML(t *testing.T, path string, rows any) {
	t.Helper()
	const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>FactMapGen Preview Gallery</title>
  <style>
    body { margin: 24px; font-family: system-ui, sans-serif; background: #171912; color: #f1ead9; }
    table { border-collapse: collapse; }
    th, td { padding: 8px; border-bottom: 1px solid #3f4636; vertical-align: top; }
    th { text-align: left; color: #ffca68; }
    img { width: 256px; height: 256px; image-rendering: auto; box-shadow: 0 0 0 1px #4e5742; }
    .seed { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; white-space: nowrap; }
    .meta { color: #c9bea4; }
  </style>
</head>
<body>
  <h1>Default Terrain Preview Comparison</h1>
  <p class="meta">Resources, trees, rocks, enemies, cliffs, and decorations are disabled. Left: cached Factorio headless terrain. Middle: FactMapGen fast Go terrain. Right: amplified RGB diff.</p>
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
        <td class="meta">{{.ChangedPercent}}% tile color changed<br>{{.WaterPercent}}% water mask changed<br>max delta {{.MaxDelta}}<br>Factorio: {{.Reference}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>
`
	tmpl, err := template.New("gallery").Parse(page)
	if err != nil {
		t.Fatalf("parse preview gallery template: %v", err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create preview gallery HTML: %v", err)
	}
	defer out.Close()
	if err := tmpl.Execute(out, rows); err != nil {
		t.Fatalf("write preview gallery HTML: %v", err)
	}
}

func fastTerrainParityCases() []previewParityCase {
	return []previewParityCase{
		{name: "default-seed-12345", profile: "default:Default", seed: "12345", size: 256},
		{name: "default-seed-1810015623", profile: "default:Default", seed: "1810015623", size: 256},
		{name: "default-seed-3662935136", profile: "default:Default", seed: "3662935136", size: 256},
		{name: "lakes-seed-98765", profile: "default:Lakes", seed: "98765", size: 256},
		{name: "island-seed-24680", profile: "default:Island", seed: "24680", size: 256},
	}
}

func previewParityCases() []previewParityCase {
	return []previewParityCase{
		{name: "default-seed-12345", profile: "default:Default", seed: "12345", size: 256},
		{name: "lakes-seed-98765", profile: "default:Lakes", seed: "98765", size: 256},
		{name: "island-seed-24680", profile: "default:Island", seed: "24680", size: 256},
		{name: "railworld-seed-13579", profile: "default:Railworld", seed: "13579", size: 256},
		{name: "rich-resources-seed-424242", profile: "default:Rich-Resources", seed: "424242", size: 256},
		{name: "waterworld-seed-314159", profile: "default:Waterworld", seed: "314159", size: 256},
	}
}

func requirePreviewParityFactorio(t *testing.T) string {
	t.Helper()
	if value := strings.TrimSpace(os.Getenv("FACTMAPGEN_FACTORIO_BIN")); value != "" {
		if _, err := os.Stat(value); err != nil {
			t.Fatalf("FACTMAPGEN_FACTORIO_BIN=%q is not usable: %v", value, err)
		}
		return normalizeFactorioBin(value)
	}
	if bin := discoverFactorioBin("tools/factorio"); bin != "" {
		if _, err := os.Stat(bin); err == nil {
			return bin
		}
	}
	t.Skip("Factorio binary not found; set FACTMAPGEN_FACTORIO_BIN to run preview parity tests")
	return ""
}

func previewParityProfile(t *testing.T, st *store, identifier string) (profileRef, string) {
	t.Helper()
	ref, err := st.resolveProfile(identifier)
	if err != nil {
		t.Fatalf("resolve profile %q: %v", identifier, err)
	}
	return ref, absolutePath(filepath.Join(st.profileDir(ref), mapGenFile))
}

type previewProcessUsage struct {
	wall   time.Duration
	user   time.Duration
	system time.Duration
	maxRSS int64
}

func renderFactorioPreviewPNG(t *testing.T, factorioBin, mapGenPath string, size int, seed string) []byte {
	t.Helper()
	body, _ := renderFactorioPreviewPNGWithUsage(t, factorioBin, mapGenPath, size, seed)
	return body
}

func renderFactorioPreviewPNGWithUsage(
	t *testing.T, factorioBin, mapGenPath string, size int, seed string,
) ([]byte, previewProcessUsage) {
	t.Helper()
	outPath := filepath.Join(t.TempDir(), "factorio-preview.png")
	args := []string{
		"--generate-map-preview", outPath,
		"--map-gen-settings", mapGenPath,
		"--map-preview-size", fmt.Sprint(size),
		"--map-preview-planet", "nauvis",
		"--map-gen-seed", seed,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, factorioBin, args...)
	if root := factorioRootFromBin(factorioBin); root != "" {
		cmd.Dir = root
	}
	started := time.Now()
	output, err := cmd.CombinedOutput()
	usage := previewProcessUsage{wall: time.Since(started)}
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("Factorio preview timed out after 90s")
		}
		t.Fatalf("Factorio preview failed: %v: %s", err, clippedOutput(string(output), 4000))
	}
	if state, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		usage.user = timevalDuration(state.Utime)
		usage.system = timevalDuration(state.Stime)
		usage.maxRSS = state.Maxrss
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read Factorio preview: %v", err)
	}
	return body, usage
}

func timevalDuration(value syscall.Timeval) time.Duration {
	return time.Duration(value.Sec)*time.Second + time.Duration(value.Usec)*time.Microsecond
}

func storedPreviewPNG(t *testing.T, p *previewer, rawURL string) []byte {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse preview URL %q: %v", rawURL, err)
	}
	name := path.Base(parsed.Path)
	img, ok := p.getPreviewImage(name)
	if !ok {
		t.Fatalf("stored preview %q not found", name)
	}
	if img.contentType != "image/png" {
		t.Fatalf("stored preview content type = %q, want image/png", img.contentType)
	}
	return img.data
}

func decodePNG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	return img
}

func assertPreviewImagesEqual(t *testing.T, caseName string, want, actual image.Image) {
	t.Helper()
	stats, diff, same := previewImageDiff(want, actual)
	if same {
		return
	}
	dir := writePreviewDiffArtifacts(t, caseName, want, actual, diff, stats)
	t.Fatalf("preview image mismatch for %s: %d/%d pixels changed (%.2f%%), max channel delta %d; wrote diff artifacts to %s",
		caseName, stats.ChangedPixels, stats.TotalPixels, stats.ChangedPercent, stats.MaxChannelDelta, dir)
}

func previewImageDiff(want, actual image.Image) (previewDiffStats, *image.RGBA, bool) {
	wantBounds := want.Bounds()
	actualBounds := actual.Bounds()
	width := wantBounds.Dx()
	height := wantBounds.Dy()
	if actualBounds.Dx() < width {
		width = actualBounds.Dx()
	}
	if actualBounds.Dy() < height {
		height = actualBounds.Dy()
	}
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}

	diff := image.NewRGBA(image.Rect(0, 0, width, height))
	stats := previewDiffStats{Width: width, Height: height, TotalPixels: width * height}
	var totalDelta int64
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			wr, wg, wb, _ := want.At(wantBounds.Min.X+x, wantBounds.Min.Y+y).RGBA()
			ar, ag, ab, _ := actual.At(actualBounds.Min.X+x, actualBounds.Min.Y+y).RGBA()
			dr := absInt(int(wr>>8) - int(ar>>8))
			dg := absInt(int(wg>>8) - int(ag>>8))
			db := absInt(int(wb>>8) - int(ab>>8))
			maxDelta := maxInt(dr, maxInt(dg, db))
			if maxDelta > 0 {
				stats.ChangedPixels++
			}
			stats.MaxChannelDelta = maxInt(stats.MaxChannelDelta, maxDelta)
			totalDelta += int64(dr + dg + db)
			diff.SetRGBA(x, y, color.RGBA{
				R: uint8(clampInt(dr*5, 0, 255)),
				G: uint8(clampInt(dg*5, 0, 255)),
				B: uint8(clampInt(db*5, 0, 255)),
				A: 255,
			})
		}
	}
	if stats.TotalPixels > 0 {
		stats.ChangedPercent = float64(stats.ChangedPixels) * 100 / float64(stats.TotalPixels)
		stats.MeanAbsDelta = float64(totalDelta) / float64(stats.TotalPixels*3)
	}
	same := wantBounds.Dx() == actualBounds.Dx() && wantBounds.Dy() == actualBounds.Dy() && stats.ChangedPixels == 0
	return stats, diff, same
}

func writePreviewDiffArtifacts(t *testing.T, caseName string, want, actual image.Image, diff image.Image, stats previewDiffStats) string {
	t.Helper()
	dir := filepath.Join("test-output", "preview-diffs", safeArtifactName(t.Name()), safeArtifactName(caseName))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create diff artifact dir: %v", err)
	}
	writePNGArtifact(t, filepath.Join(dir, "factorio.png"), want)
	writePNGArtifact(t, filepath.Join(dir, "actual.png"), actual)
	writePNGArtifact(t, filepath.Join(dir, "diff.png"), diff)
	body, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatalf("marshal diff stats: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stats.json"), append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write diff stats: %v", err)
	}
	return dir
}

func writePNGArtifact(t *testing.T, path string, img image.Image) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create PNG artifact: %v", err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		t.Fatalf("write PNG artifact: %v", err)
	}
}

var unsafeArtifactChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeArtifactName(value string) string {
	value = unsafeArtifactChars.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if value == "" {
		return "case"
	}
	return value
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
