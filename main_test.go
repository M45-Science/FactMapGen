package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestGuestCanReadAndDownloadProfilesButNotMutate(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.createProfile("Guest Copy", "default"); err != nil {
		t.Fatalf("createProfile: %v", err)
	}
	srv := &server{store: st}

	list := httptest.NewRecorder()
	srv.handleProfiles(list, httptest.NewRequest(http.MethodGet, "/api/profiles", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("guest list status = %d body=%s", list.Code, list.Body.String())
	}

	read := httptest.NewRecorder()
	srv.handleProfile(read, httptest.NewRequest(http.MethodGet, "/api/profiles/default:Default", nil))
	if read.Code != http.StatusOK {
		t.Fatalf("guest read status = %d body=%s", read.Code, read.Body.String())
	}
	var doc profileDocument
	if err := json.Unmarshal(read.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if doc.ID != "default:Default" || !doc.ReadOnly {
		t.Fatalf("guest read doc id/readOnly = %q/%v, want default:Default/true", doc.ID, doc.ReadOnly)
	}

	download := httptest.NewRecorder()
	srv.handleProfile(download, httptest.NewRequest(http.MethodGet, "/api/profiles/default:Default/download.zip", nil))
	if download.Code != http.StatusOK {
		t.Fatalf("guest download status = %d body=%s", download.Code, download.Body.String())
	}
	if got := download.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("guest download Content-Type = %q, want application/zip", got)
	}

	create := httptest.NewRecorder()
	srv.handleProfiles(create, httptest.NewRequest(http.MethodPost, "/api/profiles", strings.NewReader(`{"name":"Blocked","preset":"default"}`)))
	if create.Code != http.StatusUnauthorized {
		t.Fatalf("guest create status = %d body=%s", create.Code, create.Body.String())
	}

	update := httptest.NewRecorder()
	srv.handleProfile(update, httptest.NewRequest(http.MethodPut, "/api/profiles/Guest%20Copy", strings.NewReader(`{"mapGen":{},"mapSettings":{}}`)))
	if update.Code != http.StatusUnauthorized {
		t.Fatalf("guest update status = %d body=%s", update.Code, update.Body.String())
	}

	duplicate := httptest.NewRecorder()
	srv.handleProfile(duplicate, httptest.NewRequest(http.MethodPost, "/api/profiles/default:Default/duplicate", strings.NewReader(`{"name":"Blocked copy"}`)))
	if duplicate.Code != http.StatusUnauthorized {
		t.Fatalf("guest duplicate status = %d body=%s", duplicate.Code, duplicate.Body.String())
	}
}

func TestDownloadZipCanUsePostedCurrentSettings(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.createProfile("Guest Zip", "default"); err != nil {
		t.Fatalf("createProfile: %v", err)
	}
	srv := &server{store: st}

	req := httptest.NewRequest(http.MethodPost, "/api/profiles/Guest%20Zip/download.zip", strings.NewReader(`{"mapGen":{"width":321,"height":654},"mapSettings":{"pollution":{"enabled":false}}}`))
	rec := httptest.NewRecorder()
	srv.handleProfile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download current settings status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q, want application/zip", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, "Guest-Zip-") || !strings.HasSuffix(got, `.zip"`) {
		t.Fatalf("Content-Disposition = %q, want timestamped Guest-Zip filename", got)
	}

	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}
	files := map[string]string{}
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open zip file %s: %v", file.Name, err)
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read zip file %s: %v", file.Name, err)
		}
		files[file.Name] = string(body)
	}
	if !strings.Contains(files[mapGenFile], `"width": 321`) || !strings.Contains(files[mapGenFile], `"height": 654`) {
		t.Fatalf("map-gen zip entry did not use posted settings: %s", files[mapGenFile])
	}
	if !strings.Contains(files[mapSettingsFile], `"enabled": false`) {
		t.Fatalf("map-settings zip entry did not use posted settings: %s", files[mapSettingsFile])
	}
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
		MapGen: json.RawMessage(`{"width": 123, "height": 456, "autoplace_controls": {"coal": {"frequency": 0, "size": 1, "richness": 1}}}`),
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
	if !bytes.Contains(body, []byte(`"frequency": 0.1`)) {
		t.Fatalf("temp map-gen did not sanitize zero resource frequency: %s", body)
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

func TestParseFactorioVersionOutput(t *testing.T) {
	version, err := parseFactorioVersionOutput("Version: 2.0.72 (build 81234, linux64, headless)\n")
	if err != nil {
		t.Fatalf("parseFactorioVersionOutput: %v", err)
	}
	if version != "2.0.72" {
		t.Fatalf("version = %q, want 2.0.72", version)
	}

	if _, err := parseFactorioVersionOutput("hello\n"); err == nil {
		t.Fatal("parseFactorioVersionOutput accepted output without a version")
	}
}

func TestFactorioVersionNewer(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"2.0.73", "2.0.72", true},
		{"2.1.0", "2.0.99", true},
		{"3.0.0", "2.99.99", true},
		{"2.0.72", "2.0.72", false},
		{"2.0.71", "2.0.72", false},
		{"bad", "2.0.72", false},
	}
	for _, test := range tests {
		if got := factorioVersionNewer(test.candidate, test.current); got != test.want {
			t.Fatalf("factorioVersionNewer(%q, %q) = %v, want %v", test.candidate, test.current, got, test.want)
		}
	}
}

func TestFactorioStatusCachesVersionAndChecksLatest(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "factorio")
	bin := filepath.Join(installDir, "bin", "x64", "factorio")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	counterPath := filepath.Join(installDir, "version-count")
	script := "#!/bin/sh\ncount=$(cat version-count 2>/dev/null || echo 0)\ncount=$((count + 1))\necho $count > version-count\necho 'Version: 2.0.70 (build 81234, linux64, headless)'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake factorio: %v", err)
	}

	latest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"stable":{"headless":"2.0.72"}}`))
	}))
	defer latest.Close()

	preview := &previewer{factorioBin: bin}
	manager := newFactorioManager(installDir, preview, true)
	manager.latestURL = latest.URL

	first := manager.status(context.Background(), true)
	if first.Version != "2.0.70" || first.LatestVersion != "2.0.72" || !first.UpdateAvailable {
		t.Fatalf("first status = %#v, want current 2.0.70 with 2.0.72 update", first)
	}
	second := manager.status(context.Background(), true)
	if second.Version != "2.0.70" || !second.UpdateAvailable {
		t.Fatalf("second status = %#v, want cached current version with update", second)
	}

	counter, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatalf("read counter: %v", err)
	}
	if got := strings.TrimSpace(string(counter)); got != "1" {
		t.Fatalf("factorio --version runs = %s, want 1", got)
	}
}

func TestFactorioInstallManagedDetection(t *testing.T) {
	if !factorioInstallIsManaged("/opt/factorio", "/opt/factorio/bin/x64/factorio") {
		t.Fatal("expected bin under factorio dir to be managed")
	}
	if factorioInstallIsManaged("/opt/factorio", "/usr/local/bin/factorio") {
		t.Fatal("expected PATH-style factorio binary to be unmanaged")
	}
	if !factorioInstallIsManaged("tools/factorio", "") {
		t.Fatal("expected empty binary with configured factorio dir to be installable")
	}
}
