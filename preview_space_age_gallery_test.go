package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const spaceAgePreviewGalleryEnv = "FACTMAPGEN_SPACE_AGE_PREVIEW_GALLERY"

type spaceAgePreviewGalleryRow struct {
	Planet         string
	Label          string
	MapSeed        string
	SurfaceSeed    uint32
	FastPath       string
	FactorioPath   string
	DiffPath       string
	ChangedPercent string
	MaxDelta       int
	Reference      string
}

func TestSpaceAgePreviewGallery(t *testing.T) {
	if os.Getenv(spaceAgePreviewGalleryEnv) != "1" {
		t.Skipf(
			"set %s=1 to render the Fast-vs-Factorio Space Age gallery",
			spaceAgePreviewGalleryEnv,
		)
	}
	if testing.Short() {
		t.Skip("skipping Space Age preview gallery in short mode")
	}

	factorioBin := requirePreviewParityFactorio(t)
	const (
		mapSeed = "123456"
		size    = 384
	)
	outDir := filepath.Join(
		"test-output",
		"preview-gallery",
		"space-age-fast-vs-factorio-seed-"+mapSeed,
	)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create Space Age preview gallery: %v", err)
	}

	planets := []struct {
		name          string
		label         string
		cliffSettings map[string]any
	}{
		{
			name:  fastPreviewPlanetVulcanus,
			label: "Vulcanus",
			cliffSettings: map[string]any{
				"name":                     "cliff-vulcanus",
				"cliff_elevation_0":        70,
				"cliff_elevation_interval": 120,
			},
		},
		{
			name:  fastPreviewPlanetGleba,
			label: "Gleba",
			cliffSettings: map[string]any{
				"name":                     "cliff-gleba",
				"control":                  "gleba_cliff",
				"cliff_elevation_0":        40,
				"cliff_elevation_interval": 60,
				"richness":                 0.8,
				"cliff_smoothing":          0,
			},
		},
		{
			name:  fastPreviewPlanetFulgora,
			label: "Fulgora",
			cliffSettings: map[string]any{
				"name":                     "cliff-fulgora",
				"control":                  "fulgora_cliff",
				"cliff_elevation_0":        80,
				"cliff_elevation_interval": 40,
				"richness":                 0.95,
				"cliff_smoothing":          0,
			},
		},
		{name: fastPreviewPlanetAquilo, label: "Aquilo"},
	}
	rows := make([]spaceAgePreviewGalleryRow, 0, len(planets))
	for _, planet := range planets {
		mapGen, mapGenPath := spaceAgePreviewMapGen(
			t,
			mapSeed,
			planet.name,
			planet.cliffSettings,
		)
		factorioName := planet.name + "-factorio.png"
		fastName := planet.name + "-fast.png"
		diffName := planet.name + "-diff.png"
		factorioPath := filepath.Join(outDir, factorioName)
		factorioImg := decodePNG(t, renderFactorioPlanetPreviewPNG(
			t,
			factorioBin,
			mapGenPath,
			size,
			mapSeed,
			planet.name,
		))
		writePNGArtifact(t, factorioPath, factorioImg)

		settings, err := parseFastPreviewSettingsForPlanet(mapGen, mapSeed, planet.name)
		if err != nil {
			t.Fatalf("parse %s Fast preview settings: %v", planet.name, err)
		}
		fastImg, _, err := renderFastMapPreview(
			context.Background(),
			settings,
			size,
			previewZoom{mode: "normal", tilesPerPixel: 1, renderSize: size},
		)
		if err != nil {
			t.Fatalf("render %s Fast preview: %v", planet.name, err)
		}
		stats, diffImg, _ := previewImageDiff(factorioImg, fastImg)
		writePNGArtifact(t, filepath.Join(outDir, fastName), fastImg)
		writePNGArtifact(t, filepath.Join(outDir, diffName), diffImg)

		rows = append(rows, spaceAgePreviewGalleryRow{
			Planet:         planet.name,
			Label:          planet.label,
			MapSeed:        mapSeed,
			SurfaceSeed:    settings.seed,
			FastPath:       fastName,
			FactorioPath:   factorioName,
			DiffPath:       diffName,
			ChangedPercent: fmt.Sprintf("%.2f", stats.ChangedPercent),
			MaxDelta:       stats.MaxChannelDelta,
			Reference:      "rendered",
		})
	}

	writeSpaceAgePreviewGalleryHTML(t, filepath.Join(outDir, "index.html"), rows)
	writeSpaceAgePreviewPanel(t, outDir, size, rows)
	t.Logf("wrote Space Age Fast-vs-Factorio panel to %s", outDir)
}

func spaceAgePreviewMapGen(
	t *testing.T,
	mapSeed, planet string,
	cliffSettings map[string]any,
) ([]byte, string) {
	t.Helper()
	settings := map[string]any{
		"seed":               json.Number(mapSeed),
		"starting_area":      1,
		"autoplace_controls": map[string]any{},
	}
	if cliffSettings != nil {
		settings["cliff_settings"] = cliffSettings
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s comparison map-gen settings: %v", planet, err)
	}
	body = append(body, '\n')
	path := filepath.Join(t.TempDir(), planet+"-map-gen-settings.json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s comparison map-gen settings: %v", planet, err)
	}
	return body, path
}

func writeSpaceAgePreviewPanel(
	t *testing.T,
	outDir string,
	size int,
	rows []spaceAgePreviewGalleryRow,
) {
	t.Helper()
	montage, err := exec.LookPath("montage")
	if err != nil {
		t.Fatalf("ImageMagick montage is required to build the comparison panel: %v", err)
	}
	args := []string{
		"-background", "#171912",
		"-fill", "#f1ead9",
		"-font", "DejaVu-Sans-Bold",
		"-pointsize", "18",
	}
	for _, row := range rows {
		args = append(
			args,
			"-label", "Fast - "+row.Label, row.FastPath,
			"-label", "Factorio - "+row.Label, row.FactorioPath,
		)
	}
	args = append(
		args,
		"-tile", "2x4",
		"-geometry", fmt.Sprintf("%dx%d+14+18", size, size),
		"-depth", "8",
		"panel.png",
	)
	cmd := exec.Command(montage, args...)
	cmd.Dir = outDir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build Space Age comparison panel: %v: %s", err, output)
	}
}

func writeSpaceAgePreviewGalleryHTML(
	t *testing.T,
	path string,
	rows []spaceAgePreviewGalleryRow,
) {
	t.Helper()
	const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Space Age Fast vs Factorio</title>
  <style>
    body { margin: 24px; font-family: system-ui, sans-serif; background: #171912; color: #f1ead9; }
    h1, th { color: #ffca68; }
    .meta { color: #c9bea4; }
    .panel { max-width: 100%; height: auto; box-shadow: 0 0 0 1px #4e5742; }
    table { margin-top: 24px; border-collapse: collapse; }
    th, td { padding: 8px; border-bottom: 1px solid #3f4636; vertical-align: top; text-align: left; }
    td img { width: 384px; height: 384px; box-shadow: 0 0 0 1px #4e5742; }
    code { color: #d7e9bd; }
  </style>
</head>
<body>
  <h1>Space Age: FactMapGen Fast vs Factorio</h1>
  <p class="meta">Map seed <code>123456</code>, center <code>(0, 0)</code>, one tile per pixel. Fast is left; local headless Factorio is right. The Fast renderer applies Factorio's per-planet CRC32 surface-seed offset before rendering.</p>
  <p><a href="panel.png"><img class="panel" src="panel.png" alt="Four-planet Fast versus Factorio comparison panel"></a></p>
  <table>
    <thead><tr><th>Planet</th><th>Fast</th><th>Factorio</th><th>Amplified diff</th><th>Details</th></tr></thead>
    <tbody>
      {{range .}}
      <tr>
        <td><strong>{{.Label}}</strong></td>
        <td><a href="{{.FastPath}}"><img src="{{.FastPath}}" alt="Fast {{.Label}} preview"></a></td>
        <td><a href="{{.FactorioPath}}"><img src="{{.FactorioPath}}" alt="Factorio {{.Label}} preview"></a></td>
        <td><a href="{{.DiffPath}}"><img src="{{.DiffPath}}" alt="{{.Label}} amplified diff"></a></td>
        <td class="meta">map seed {{.MapSeed}}<br>Fast surface seed {{.SurfaceSeed}}<br>{{.ChangedPercent}}% pixels differ<br>max channel delta {{.MaxDelta}}<br>Factorio reference {{.Reference}}</td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>
`
	tmpl, err := template.New("space-age-gallery").Parse(page)
	if err != nil {
		t.Fatalf("parse Space Age gallery template: %v", err)
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create Space Age gallery HTML: %v", err)
	}
	defer out.Close()
	if err := tmpl.Execute(out, rows); err != nil {
		t.Fatalf("write Space Age gallery HTML: %v", err)
	}
}
