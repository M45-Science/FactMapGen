package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
)

func checkV21Expansion(t *testing.T, raw []byte) {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	expansion := asMap(settings["enemy_expansion"])
	for key, want := range map[string]float64{"min_expansion_distance": 3, "evolution_group_size_factor": 8, "build_base_unit_dispatch_cooldown": 1800} {
		if got := expansion[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

func TestPresetsContainRequiredV21MapSettings(t *testing.T) {
	for _, preset := range bundledPresetProfiles {
		t.Run(preset.Key, func(t *testing.T) {
			_, settings, err := presetDocuments(preset.Key)
			if err != nil {
				t.Fatal(err)
			}
			checkV21Expansion(t, settings)
		})
	}
}

func TestLegacyProfileSaveAndZipUpgradeMapSettings(t *testing.T) {
	st := newTestStore(t)
	raw := json.RawMessage(`{"enemy_expansion":{"enabled":false,"settler_group_max_size":50},"custom_extension":{"value":123}}`)
	doc, err := st.createProfileFromDocuments("Legacy", []byte(`{}`), raw)
	if err != nil {
		t.Fatal(err)
	}
	checkV21Expansion(t, doc.MapSettings)
	var saved map[string]any
	if err := json.Unmarshal(doc.MapSettings, &saved); err != nil {
		t.Fatal(err)
	}
	if asMap(saved["enemy_expansion"])["enabled"] != false || asMap(saved["enemy_expansion"])["settler_group_max_size"] != float64(50) || asMap(saved["custom_extension"])["value"] != float64(123) {
		t.Fatalf("changed existing settings: %s", doc.MapSettings)
	}
	recorder := httptest.NewRecorder()
	if err := writeProfileZip(recorder, "Legacy", []byte(`{}`), raw); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range zr.File {
		if file.Name == mapSettingsFile {
			found = true
			r, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(r)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			checkV21Expansion(t, body)
		}
	}
	if !found {
		t.Fatal("ZIP omitted map settings")
	}
}

func TestMapSettingsKeepExplicitV21Values(t *testing.T) {
	raw, err := normalizeMapSettingsJSON([]byte(`{"enemy_expansion":{"min_expansion_distance":0,"evolution_group_size_factor":4,"build_base_unit_dispatch_cooldown":300}}`))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	json.Unmarshal(raw, &settings)
	expansion := asMap(settings["enemy_expansion"])
	if expansion["min_expansion_distance"] != float64(0) || expansion["evolution_group_size_factor"] != float64(4) || expansion["build_base_unit_dispatch_cooldown"] != float64(300) {
		t.Fatalf("overwrote tuning: %s", raw)
	}
}
