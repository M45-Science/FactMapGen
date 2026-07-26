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
	resp, err := srv.renderPreview(context.Background(), "default:Default", previewRequest{
		Size:     256,
		Planet:   "nauvis",
		Seed:     "12345",
		Lossless: true,
	}, previewPriorityGuest)
	if err != nil {
		t.Fatalf("renderPreview fast fallback: %v", err)
	}
	if resp.Engine != previewEngineFast {
		t.Fatalf("preview engine = %q, want %q", resp.Engine, previewEngineFast)
	}
	if resp.Size != 256 || resp.Planet != "nauvis" {
		t.Fatalf("preview response = %#v", resp)
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
}

func TestRenderPreviewExactEngineStillRequiresFactorio(t *testing.T) {
	srv := &server{previewer: &previewer{}}
	_, err := srv.renderPreview(context.Background(), "Default", previewRequest{Engine: previewEngineFactorio, Size: 256, Planet: "nauvis"}, previewPriorityGuest)
	if !errors.Is(err, errPreviewUnavailable) {
		t.Fatalf("renderPreview exact error = %v, want errPreviewUnavailable", err)
	}
}
