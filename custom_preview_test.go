package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestFastPreviewRenderIsDeterministic(t *testing.T) {
	raw := []byte(`{
		"seed": 12345,
		"starting_area": 1,
		"property_expression_names": {
			"control:moisture:bias": "0.35",
			"control:aux:bias": "-0.15"
		},
		"autoplace_controls": {
			"water": {"frequency": 1.2, "size": 1.4},
			"iron-ore": {"frequency": 1, "size": 1, "richness": 1},
			"copper-ore": {"frequency": 1, "size": 1, "richness": 1},
			"coal": {"frequency": 1, "size": 1, "richness": 1},
			"stone": {"frequency": 1, "size": 1, "richness": 1}
		}
	}`)
	settings, err := parseFastPreviewSettings(raw, "")
	if err != nil {
		t.Fatalf("parseFastPreviewSettings: %v", err)
	}
	zoom := previewZoom{mode: "normal", tilesPerPixel: 1, renderSize: 256}
	a, tpp, err := renderFastMapPreview(context.Background(), settings, 256, zoom)
	if err != nil {
		t.Fatalf("renderFastMapPreview a: %v", err)
	}
	b, _, err := renderFastMapPreview(context.Background(), settings, 256, zoom)
	if err != nil {
		t.Fatalf("renderFastMapPreview b: %v", err)
	}
	if tpp != 1 {
		t.Fatalf("tiles per pixel = %v, want 1", tpp)
	}
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Fatal("fast preview render is not deterministic")
	}
	if len(a.Pix) != 256*256*4 {
		t.Fatalf("pixel buffer len = %d, want %d", len(a.Pix), 256*256*4)
	}
}

func TestParseFastPreviewSettingsReadsCliffSettings(t *testing.T) {
	settings, err := parseFastPreviewSettings([]byte(`{
		"autoplace_controls": {
			"nauvis_cliff": {"frequency": 0.5, "size": 2.5, "richness": 1}
		},
		"cliff_settings": {
			"cliff_elevation_0": 17,
			"cliff_elevation_interval": 23,
			"richness": 4
		}
	}`), "")
	if err != nil {
		t.Fatalf("parse cliff settings: %v", err)
	}
	if !settings.cliffs.enabled || settings.cliffs.frequency != 0.5 || settings.cliffs.size != 2.5 {
		t.Fatalf("cliff control = %#v", settings.cliffs)
	}
	if settings.cliffElevation0 != 17 || settings.cliffElevationInterval != 23 || settings.cliffRichness != 4 {
		t.Fatalf("cliff settings = elevation0 %g, interval %g, richness %g", settings.cliffElevation0, settings.cliffElevationInterval, settings.cliffRichness)
	}

	defaults, err := parseFastPreviewSettings([]byte(`{}`), "")
	if err != nil {
		t.Fatalf("parse default cliff settings: %v", err)
	}
	if !defaults.cliffs.enabled || defaults.cliffElevation0 != 10 || defaults.cliffElevationInterval != 40 || defaults.cliffRichness != 1 {
		t.Fatalf("default cliff settings = %#v", defaults)
	}
}

func TestRenderPreviewUsesFastEngineWithoutFactorio(t *testing.T) {
	root := t.TempDir()
	st := &store{
		defaultRoot: filepath.Join(root, "defaults"),
		customRoot:  filepath.Join(root, "presets"),
	}
	if err := st.ensure(); err != nil {
		t.Fatalf("store ensure: %v", err)
	}
	srv := &server{
		store:     st,
		previewer: &previewer{},
	}
	req := previewRequest{
		Size:    256,
		Planet:  "nauvis",
		Seed:    "12345",
		CenterX: 12.4,
		CenterY: -8.6,
	}
	resp, err := srv.renderPreview(
		context.Background(), "default:Default", req, previewPriorityGuest,
	)
	if err != nil {
		t.Fatalf("renderPreview fast fallback: %v", err)
	}
	if resp.Engine != previewEngineFast {
		t.Fatalf("preview engine = %q, want %q", resp.Engine, previewEngineFast)
	}
	if resp.Size != 256 || resp.Planet != "nauvis" {
		t.Fatalf("preview response = %#v", resp)
	}
	if resp.CenterX != 12 || resp.CenterY != -9 || resp.TilesPerPixel != 1 {
		t.Fatalf(
			"preview viewport = (%g,%g) at %g, want (12,-9) at 1",
			resp.CenterX, resp.CenterY, resp.TilesPerPixel,
		)
	}
	name := filepath.Base(resp.URL)
	if i := bytes.IndexByte([]byte(name), '?'); i >= 0 {
		name = name[:i]
	}
	img, ok := srv.previewer.getPreviewImage(name)
	if !ok {
		t.Fatalf("stored preview %q not found", name)
	}
	if img.contentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", img.contentType)
	}

	before := srv.previewer.fastCache().stats()
	if _, err := srv.renderPreview(
		context.Background(), "default:Default", req, previewPriorityGuest,
	); err != nil {
		t.Fatalf("repeat cached preview: %v", err)
	}
	after := srv.previewer.fastCache().stats()
	if after.Hits <= before.Hits || after.Misses != before.Misses {
		t.Fatalf("repeat cache stats = %#v after %#v, want hits without misses", after, before)
	}
}

func TestRenderPreviewExactEngineStillRequiresFactorio(t *testing.T) {
	srv := &server{previewer: &previewer{}}
	_, err := srv.renderPreview(context.Background(), "Default", previewRequest{Engine: previewEngineFactorio, Size: 256, Planet: "nauvis"}, previewPriorityGuest)
	if !errors.Is(err, errPreviewUnavailable) {
		t.Fatalf("renderPreview exact error = %v, want errPreviewUnavailable", err)
	}
}
