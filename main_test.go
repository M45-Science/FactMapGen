package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateProfileName(t *testing.T) {
	tests := []struct {
		name string
		ok   bool
	}{
		{"Default", true},
		{"Rail World_2.0", true},
		{"", false},
		{"../escape", false},
		{".", false},
		{"bad/name", false},
		{"bad\\name", false},
		{" hidden", true},
	}

	for _, test := range tests {
		err := validateProfileName(test.name)
		if test.ok && err != nil {
			t.Fatalf("validateProfileName(%q) returned %v", test.name, err)
		}
		if !test.ok && !errors.Is(err, errInvalidProfileName) {
			t.Fatalf("validateProfileName(%q) = %v, want errInvalidProfileName", test.name, err)
		}
	}
}

func newTestStore(t *testing.T) *store {
	t.Helper()
	root := t.TempDir()
	st := &store{
		defaultRoot: filepath.Join(root, "default-presets"),
		customRoot:  filepath.Join(root, "custom-presets"),
	}
	if err := st.ensure(); err != nil {
		t.Fatalf("ensure store: %v", err)
	}
	return st
}

func TestStoreCreateReadAndSave(t *testing.T) {
	st := newTestStore(t)
	doc, err := st.createProfile("Peaceful", "peaceful-rich")
	if err != nil {
		t.Fatalf("createProfile: %v", err)
	}
	if doc.Name != "Peaceful" {
		t.Fatalf("created profile name = %q", doc.Name)
	}

	var mapGen map[string]any
	if err := json.Unmarshal(doc.MapGen, &mapGen); err != nil {
		t.Fatalf("unmarshal map gen: %v", err)
	}
	if peaceful, ok := mapGen["peaceful_mode"].(bool); !ok || !peaceful {
		t.Fatalf("peaceful-rich preset peaceful_mode = %#v", mapGen["peaceful_mode"])
	}

	if doc.Source != profileSourceCustom || doc.ReadOnly {
		t.Fatalf("created profile source/readOnly = %q/%v, want custom/false", doc.Source, doc.ReadOnly)
	}
	if _, err := os.Stat(filepath.Join(st.customRoot, "Peaceful", mapGenFile)); err != nil {
		t.Fatalf("map-gen file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(st.customRoot, "Peaceful", mapSettingsFile)); err != nil {
		t.Fatalf("map-settings file missing: %v", err)
	}

	doc.MapGen = json.RawMessage(`{"width": 512, "height": 256}`)
	saved, err := st.saveProfile("Peaceful", doc.MapGen, doc.MapSettings)
	if err != nil {
		t.Fatalf("saveProfile: %v", err)
	}
	if !bytes.Contains(saved.MapGen, []byte(`"width": 512`)) {
		t.Fatalf("saved map gen was not normalized: %s", saved.MapGen)
	}
}

func TestStoreWritesProfileZip(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.createProfile("Peaceful", "peaceful-rich"); err != nil {
		t.Fatalf("createProfile: %v", err)
	}

	rec := httptest.NewRecorder()
	if err := st.writeProfileZip(rec, "Peaceful"); err != nil {
		t.Fatalf("writeProfileZip: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	seen := map[string]bool{}
	for _, file := range zr.File {
		seen[file.Name] = true
	}
	for _, want := range []string{mapGenFile, mapSettingsFile} {
		if !seen[want] {
			t.Fatalf("zip missing %s", want)
		}
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", got)
	}
}

func TestStoreRejectsInvalidJSONAndTraversal(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.createProfile("../Escape", "default"); !errors.Is(err, errInvalidProfileName) {
		t.Fatalf("createProfile traversal err = %v, want errInvalidProfileName", err)
	}

	if _, err := st.saveProfile("Bad", json.RawMessage(`{"ok": true} trailing`), json.RawMessage(`{}`)); err == nil {
		t.Fatal("saveProfile accepted invalid JSON")
	}

	if _, err := normalizeJSON(json.RawMessage(`{} {}`)); err == nil {
		t.Fatal("normalizeJSON accepted multiple JSON documents")
	}

	if _, err := normalizeJSON(json.RawMessage(`[]`)); err == nil {
		t.Fatal("normalizeJSON accepted a non-object JSON document")
	}
}

func TestStoreDefaultProfilesAreReadOnly(t *testing.T) {
	st := newTestStore(t)
	doc, err := st.readProfile("default:Default")
	if err != nil {
		t.Fatalf("read default profile: %v", err)
	}
	if doc.Source != profileSourceDefault || !doc.ReadOnly {
		t.Fatalf("default profile source/readOnly = %q/%v, want default/true", doc.Source, doc.ReadOnly)
	}
	if _, err := st.saveProfile(doc.ID, json.RawMessage(`{"width": 1}`), doc.MapSettings); !errors.Is(err, errReadOnlyProfile) {
		t.Fatalf("save default err = %v, want errReadOnlyProfile", err)
	}
	if err := st.deleteProfile(doc.ID); !errors.Is(err, errReadOnlyProfile) {
		t.Fatalf("delete default err = %v, want errReadOnlyProfile", err)
	}
	duplicate, err := st.duplicateProfile(doc.ID, "Default copy")
	if err != nil {
		t.Fatalf("duplicate default profile: %v", err)
	}
	if duplicate.Source != profileSourceCustom || duplicate.ReadOnly {
		t.Fatalf("duplicate source/readOnly = %q/%v, want custom/false", duplicate.Source, duplicate.ReadOnly)
	}
}

func TestPreviewMapGenPathUsesTemporaryRequestJSON(t *testing.T) {
	st := newTestStore(t)
	doc, err := st.createProfile("PreviewTemp", "default")
	if err != nil {
		t.Fatalf("createProfile: %v", err)
	}

	srv := &server{
		store: st,
		previewer: &previewer{
			outputRoot: filepath.Join(t.TempDir(), "previews"),
		},
	}
	ref, err := st.resolveProfile(doc.ID)
	if err != nil {
		t.Fatalf("resolveProfile: %v", err)
	}
	mapGenPath, cleanup, err := srv.previewMapGenPath(ref, previewRequest{
		MapGen: json.RawMessage(`{"width": 123, "height": 456}`),
	})
	if err != nil {
		t.Fatalf("previewMapGenPath: %v", err)
	}
	defer cleanup()

	body, err := os.ReadFile(mapGenPath)
	if err != nil {
		t.Fatalf("read temp map-gen: %v", err)
	}
	if !bytes.Contains(body, []byte(`"width": 123`)) {
		t.Fatalf("temp map-gen did not contain request JSON: %s", body)
	}

	saved, err := os.ReadFile(filepath.Join(st.customRoot, "PreviewTemp", mapGenFile))
	if err != nil {
		t.Fatalf("read saved map-gen: %v", err)
	}
	if bytes.Contains(saved, []byte(`"width": 123`)) {
		t.Fatalf("preview request JSON was written to saved preset: %s", saved)
	}
}

func TestDefaultPresetUsesRandomSeedZero(t *testing.T) {
	mapGenRaw, _, err := presetDocuments("default")
	if err != nil {
		t.Fatalf("presetDocuments: %v", err)
	}
	var mapGen map[string]any
	if err := json.Unmarshal(mapGenRaw, &mapGen); err != nil {
		t.Fatalf("unmarshal map gen: %v", err)
	}
	if seed, ok := mapGen["seed"].(float64); !ok || seed != 0 {
		t.Fatalf("default seed = %#v, want 0", mapGen["seed"])
	}
}

func TestNoBitersPresetDisablesEnemiesAndForcesPeaceful(t *testing.T) {
	mapGenRaw, mapSettingsRaw, err := presetDocuments("no-biters")
	if err != nil {
		t.Fatalf("presetDocuments: %v", err)
	}
	var mapGen map[string]any
	if err := json.Unmarshal(mapGenRaw, &mapGen); err != nil {
		t.Fatalf("unmarshal map gen: %v", err)
	}
	var mapSettings map[string]any
	if err := json.Unmarshal(mapSettingsRaw, &mapSettings); err != nil {
		t.Fatalf("unmarshal map settings: %v", err)
	}

	if peaceful, ok := mapGen["peaceful_mode"].(bool); !ok || !peaceful {
		t.Fatalf("peaceful_mode = %#v, want true", mapGen["peaceful_mode"])
	}
	enemyBase := mapGen["autoplace_controls"].(map[string]any)["enemy-base"].(map[string]any)
	if enemyBase["frequency"].(float64) != 0 || enemyBase["size"].(float64) != 0 {
		t.Fatalf("enemy-base autoplace = %#v, want frequency/size 0", enemyBase)
	}
	if enabled := mapSettings["enemy_evolution"].(map[string]any)["enabled"]; enabled != false {
		t.Fatalf("enemy evolution enabled = %#v, want false", enabled)
	}
	if enabled := mapSettings["enemy_expansion"].(map[string]any)["enabled"]; enabled != false {
		t.Fatalf("enemy expansion enabled = %#v, want false", enabled)
	}
}

func TestFactorioRootFromBin(t *testing.T) {
	got := factorioRootFromBin("/opt/factorio/bin/x64/factorio")
	if got != "/opt/factorio" {
		t.Fatalf("factorioRootFromBin returned %q", got)
	}

	if got := factorioRootFromBin("/usr/local/bin/factorio"); got != "" {
		t.Fatalf("factorioRootFromBin for path install returned %q, want empty", got)
	}
}
