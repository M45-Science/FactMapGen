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

func TestDefaultPresetUsesPublicRandomSeedNull(t *testing.T) {
	mapGenRaw, _, err := presetDocuments("default")
	if err != nil {
		t.Fatalf("presetDocuments: %v", err)
	}
	var mapGen map[string]any
	if err := json.Unmarshal(mapGenRaw, &mapGen); err != nil {
		t.Fatalf("unmarshal map gen: %v", err)
	}
	if _, exists := mapGen["seed"]; !exists || mapGen["seed"] != nil {
		t.Fatalf("default seed = %#v, want nil", mapGen["seed"])
	}
}

func TestNormalizeJSONDoesNotInventSeed(t *testing.T) {
	mapGen := map[string]any{}
	if _, exists := mapGen["seed"]; exists {
		t.Fatal("test setup unexpectedly had a seed")
	}
	mapGenRaw, err := json.Marshal(mapGen)
	if err != nil {
		t.Fatalf("marshal map gen: %v", err)
	}
	normalized, err := normalizeJSON(mapGenRaw)
	if err != nil {
		t.Fatalf("normalizeJSON: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(normalized, &decoded); err != nil {
		t.Fatalf("unmarshal normalized: %v", err)
	}
	if _, exists := decoded["seed"]; exists {
		t.Fatalf("normalizeJSON should not invent a seed, got %#v", decoded["seed"])
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

func TestEmptySandboxPresetKeepsResourceFrequencyPreviewSafe(t *testing.T) {
	mapGenRaw, mapSettingsRaw, err := presetDocuments("empty-sandbox")
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

	controls := mapGen["autoplace_controls"].(map[string]any)
	for _, name := range []string{"coal", "stone", "copper-ore", "iron-ore", "uranium-ore", "crude-oil"} {
		control := controls[name].(map[string]any)
		if control["frequency"].(float64) <= 0 {
			t.Fatalf("%s frequency = %#v, want positive for Factorio preview", name, control["frequency"])
		}
		if control["size"].(float64) != 0 || control["richness"].(float64) != 0 {
			t.Fatalf("%s autoplace = %#v, want size/richness 0", name, control)
		}
	}
	for _, name := range []string{"water", "trees"} {
		control := controls[name].(map[string]any)
		if control["frequency"].(float64) <= 0 {
			t.Fatalf("%s frequency = %#v, want positive for Factorio preview", name, control["frequency"])
		}
		if control["size"].(float64) != 0 {
			t.Fatalf("%s autoplace = %#v, want size 0", name, control)
		}
	}

	if richness := mapGen["cliff_settings"].(map[string]any)["richness"]; richness != float64(0) {
		t.Fatalf("cliff richness = %#v, want 0", richness)
	}
	if pollution := mapSettings["pollution"].(map[string]any)["enabled"]; pollution != false {
		t.Fatalf("pollution enabled = %#v, want false", pollution)
	}
}

func TestCoolPresetsHaveDistinctShape(t *testing.T) {
	tests := []struct {
		preset string
		check  func(t *testing.T, mapGen, mapSettings map[string]any)
	}{
		{"marathon-frontier", func(t *testing.T, mapGen, mapSettings map[string]any) {
			if got := mapSettings["difficulty_settings"].(map[string]any)["technology_price_multiplier"]; got != float64(4) {
				t.Fatalf("technology multiplier = %#v, want 4", got)
			}
			enemyBase := mapGen["autoplace_controls"].(map[string]any)["enemy-base"].(map[string]any)
			if enemyBase["frequency"].(float64) <= 1 {
				t.Fatalf("enemy-base frequency = %#v, want > 1", enemyBase["frequency"])
			}
		}},
		{"dense-forest", func(t *testing.T, mapGen, mapSettings map[string]any) {
			trees := mapGen["autoplace_controls"].(map[string]any)["trees"].(map[string]any)
			if trees["frequency"].(float64) < 2 || trees["size"].(float64) < 2 {
				t.Fatalf("trees autoplace = %#v, want dense forest", trees)
			}
		}},
		{"desert-scarcity", func(t *testing.T, mapGen, mapSettings map[string]any) {
			expressions := mapGen["property_expression_names"].(map[string]any)
			if expressions["control:moisture:bias"] != "-0.75" {
				t.Fatalf("moisture bias = %#v, want -0.75", expressions["control:moisture:bias"])
			}
			water := mapGen["autoplace_controls"].(map[string]any)["water"].(map[string]any)
			if water["frequency"].(float64) >= 0.5 {
				t.Fatalf("water frequency = %#v, want scarce", water["frequency"])
			}
		}},
		{"cliffside-lakes", func(t *testing.T, mapGen, mapSettings map[string]any) {
			cliffs := mapGen["cliff_settings"].(map[string]any)
			if cliffs["richness"].(float64) < 3 {
				t.Fatalf("cliff richness = %#v, want high", cliffs["richness"])
			}
			water := mapGen["autoplace_controls"].(map[string]any)["water"].(map[string]any)
			if water["size"].(float64) < 1.5 {
				t.Fatalf("water size = %#v, want large lakes", water["size"])
			}
		}},
		{"oil-baron", func(t *testing.T, mapGen, mapSettings map[string]any) {
			oil := mapGen["autoplace_controls"].(map[string]any)["crude-oil"].(map[string]any)
			if oil["richness"].(float64) < 4 {
				t.Fatalf("oil richness = %#v, want oil boom", oil["richness"])
			}
		}},
		{"tiny-death-spiral", func(t *testing.T, mapGen, mapSettings map[string]any) {
			if mapGen["width"].(float64) != 768 || mapGen["height"].(float64) != 768 {
				t.Fatalf("map dimensions = %#v x %#v, want 768 x 768", mapGen["width"], mapGen["height"])
			}
			enemyBase := mapGen["autoplace_controls"].(map[string]any)["enemy-base"].(map[string]any)
			if enemyBase["size"].(float64) < 2 {
				t.Fatalf("enemy-base size = %#v, want severe", enemyBase["size"])
			}
		}},
	}

	for _, test := range tests {
		mapGenRaw, mapSettingsRaw, err := presetDocuments(test.preset)
		if err != nil {
			t.Fatalf("presetDocuments(%q): %v", test.preset, err)
		}
		var mapGen map[string]any
		if err := json.Unmarshal(mapGenRaw, &mapGen); err != nil {
			t.Fatalf("unmarshal map gen for %s: %v", test.preset, err)
		}
		var mapSettings map[string]any
		if err := json.Unmarshal(mapSettingsRaw, &mapSettings); err != nil {
			t.Fatalf("unmarshal map settings for %s: %v", test.preset, err)
		}
		t.Run(test.preset, func(t *testing.T) {
			test.check(t, mapGen, mapSettings)
		})
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
