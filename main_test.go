package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestSanitizeProfileName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"  My Map  ", "My Map"},
		{"../escape", "escape"},
		{"bad/name\\here", "bad-name-here"},
		{"name:with*symbols?", "name-with-symbols"},
		{"spaces\tand\nlines", "spaces and lines"},
		{"---", ""},
		{strings.Repeat("a", 80), strings.Repeat("a", maxProfileNameLength)},
	}

	for _, test := range tests {
		if got := sanitizeProfileName(test.name); got != test.want {
			t.Fatalf("sanitizeProfileName(%q) = %q, want %q", test.name, got, test.want)
		}
		if test.want != "" {
			if err := validateProfileName(test.want); err != nil {
				t.Fatalf("sanitized name %q did not validate: %v", test.want, err)
			}
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

	docForImport, err := st.readProfile("Guest Copy")
	if err != nil {
		t.Fatalf("read guest copy for local import: %v", err)
	}
	exchangeString, err := EncodeMapExchangeString(docForImport.MapGen, docForImport.MapSettings)
	if err != nil {
		t.Fatalf("encode exchange string for local import: %v", err)
	}
	importBody, _ := json.Marshal(importExchangeStringRequest{Name: "../Guest/Import??", ExchangeString: exchangeString})
	importExchange := httptest.NewRecorder()
	srv.handleProfile(importExchange, httptest.NewRequest(http.MethodPost, "/api/profiles/import-exchange", bytes.NewReader(importBody)))
	if importExchange.Code != http.StatusOK {
		t.Fatalf("guest local import status = %d body=%s", importExchange.Code, importExchange.Body.String())
	}
	var imported profileDocument
	if err := json.Unmarshal(importExchange.Body.Bytes(), &imported); err != nil {
		t.Fatalf("decode guest local import response: %v", err)
	}
	if imported.ID != "local:Guest-Import" || imported.Source != profileSourceLocal || imported.ReadOnly {
		t.Fatalf("guest local import doc = %#v, want local Guest-Import", imported)
	}
	if _, err := os.Stat(filepath.Join(st.customRoot, "Guest-Import")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("guest local import wrote a preset directory err=%v, want not exist", err)
	}

	rename := httptest.NewRecorder()
	srv.handleProfile(rename, httptest.NewRequest(http.MethodPost, "/api/profiles/Guest%20Copy/rename", strings.NewReader(`{"name":"Blocked rename"}`)))
	if rename.Code != http.StatusUnauthorized {
		t.Fatalf("guest rename status = %d body=%s", rename.Code, rename.Body.String())
	}

	deleteRec := httptest.NewRecorder()
	srv.handleProfile(deleteRec, httptest.NewRequest(http.MethodDelete, "/api/profiles/Guest%20Copy", nil))
	if deleteRec.Code != http.StatusUnauthorized {
		t.Fatalf("guest delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
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

	saved, err := st.readProfile("Guest Zip")
	if err != nil {
		t.Fatalf("read saved profile after current-settings download: %v", err)
	}
	if bytes.Contains(saved.MapGen, []byte(`"width": 321`)) || bytes.Contains(saved.MapSettings, []byte(`"enabled": false`)) {
		t.Fatalf("posted download settings were written to saved profile: mapGen=%s mapSettings=%s", saved.MapGen, saved.MapSettings)
	}

	localReq := httptest.NewRequest(http.MethodPost, "/api/profiles/local%3AGuest%20Zip/download.zip", strings.NewReader(`{"name":"Guest Zip Local","mapGen":{"width":123},"mapSettings":{"pollution":{"enabled":false}}}`))
	localRec := httptest.NewRecorder()
	srv.handleProfile(localRec, localReq)
	if localRec.Code != http.StatusOK {
		t.Fatalf("local download current settings status = %d body=%s", localRec.Code, localRec.Body.String())
	}
	if got := localRec.Header().Get("Content-Disposition"); !strings.Contains(got, "Guest-Zip-Local-") {
		t.Fatalf("local Content-Disposition = %q, want Guest-Zip-Local filename", got)
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

	doc.MapGen = json.RawMessage(`{"width": 0, "height": null, "starting_area": 1}`)
	saved, err = st.saveProfile("Peaceful", doc.MapGen, doc.MapSettings)
	if err != nil {
		t.Fatalf("saveProfile zero dimensions: %v", err)
	}
	if bytes.Contains(saved.MapGen, []byte(`"width"`)) || bytes.Contains(saved.MapGen, []byte(`"height"`)) {
		t.Fatalf("saved map gen retained implicit map dimensions: %s", saved.MapGen)
	}
}

func TestStoreRenameProfile(t *testing.T) {
	st := newTestStore(t)
	doc, err := st.createProfile(" ../Old/Name?? ", "default")
	if err != nil {
		t.Fatalf("createProfile: %v", err)
	}
	if doc.Name != "Old-Name" || doc.ID != "custom:Old-Name" {
		t.Fatalf("sanitized created profile = %q/%q, want Old-Name/custom:Old-Name", doc.Name, doc.ID)
	}
	saved, err := st.saveProfile(doc.ID, json.RawMessage(`{"width": 512, "height": 256}`), doc.MapSettings)
	if err != nil {
		t.Fatalf("saveProfile: %v", err)
	}

	renamed, err := st.renameProfile(saved.ID, " ../New/Name:?? ")
	if err != nil {
		t.Fatalf("renameProfile: %v", err)
	}
	if renamed.ID != "custom:New-Name" || renamed.Name != "New-Name" || renamed.ReadOnly {
		t.Fatalf("renamed profile = %#v, want custom New-Name", renamed)
	}
	if !bytes.Contains(renamed.MapGen, []byte(`"width": 512`)) {
		t.Fatalf("renamed profile lost map-gen contents: %s", renamed.MapGen)
	}
	if _, err := os.Stat(filepath.Join(st.customRoot, "Old-Name")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old profile dir stat err = %v, want not exist", err)
	}
	if _, err := os.Stat(filepath.Join(st.customRoot, "New-Name", mapGenFile)); err != nil {
		t.Fatalf("new profile map-gen missing: %v", err)
	}
	if _, err := st.readProfile("Old-Name"); !errors.Is(err, errProfileNotFound) {
		t.Fatalf("read old profile err = %v, want errProfileNotFound", err)
	}

	if _, err := st.createProfile("Taken", "default"); err != nil {
		t.Fatalf("create taken profile: %v", err)
	}
	if _, err := st.renameProfile("New-Name", "Taken"); !errors.Is(err, errProfileExists) {
		t.Fatalf("rename conflict err = %v, want errProfileExists", err)
	}
	if _, err := st.renameProfile("New-Name", "////"); !errors.Is(err, errInvalidProfileName) {
		t.Fatalf("rename invalid err = %v, want errInvalidProfileName", err)
	}
	if _, err := st.renameProfile("default:Default", "Renamed Default"); !errors.Is(err, errReadOnlyProfile) {
		t.Fatalf("rename default err = %v, want errReadOnlyProfile", err)
	}
}

func TestProfileRenameRoute(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.createProfile("Before", "default"); err != nil {
		t.Fatalf("createProfile: %v", err)
	}
	auth, password := newTestAuthStore(t)
	srv := &server{store: st, auth: auth}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
	login := httptest.NewRecorder()
	srv.handleSession(login, loginReq)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a cookie")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/profiles/Before/rename", strings.NewReader(`{"name":"After"}`))
	req.AddCookie(cookies[0])
	rec := httptest.NewRecorder()
	srv.handleProfile(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d body=%s", rec.Code, rec.Body.String())
	}
	var doc profileDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode rename response: %v", err)
	}
	if doc.ID != "custom:After" || doc.Name != "After" {
		t.Fatalf("renamed doc id/name = %q/%q, want custom:After/After", doc.ID, doc.Name)
	}
	if _, err := st.readProfile("After"); err != nil {
		t.Fatalf("read renamed profile: %v", err)
	}
	if _, err := st.readProfile("Before"); !errors.Is(err, errProfileNotFound) {
		t.Fatalf("read old route profile err = %v, want errProfileNotFound", err)
	}
}

func TestDecodeNativeFactorioExchangeString(t *testing.T) {
	input := `>>>eNp1VDGIE0EUnbkYcuYuGiQIwnFGuDYW6oGFZEcbETlru3Wymc0Nbnbi7EzktDDoFRaCjc3ZaGtjpYXdgY2CgigIWp0cgoWFh1EshDizm9nMbuLA//v2vz///zc77BwAYE0ZWKria5IGzPW4bBOX0QCAgWOs5OHAo4LYsX0ew5mkssd6PcIbjGfy9scVG7mKZRKS7kajhSOVDFBiA6fiB5JxGhK3T0Jhb6j4Mugwjl0voL5vMwcNQ6MAh+3I5hY6AWnN2FNN4vEQbjLEhFxMyJ6qJmZViwQLyYz4dSwIt+PzlLMwfx6VgIp1KrtuS+vM9A2x7NNoetoiZ97VzCTFyOO4Z0cORwJzQcOOiznBbpfRSMhs5+LU4LVIBr7k1HOxR9tuh2xEWQVFwQnJdF4UMuxEgoRuTteC5DhUuqb09mXg4VAqXbkLcyhl+kwDGnUzvafOE8Cty+T2YHMZaBvdAvXRSJtCO+oGaQNwoK6SyoYqaK/6WWXnJpUgvFl7ev7LjQcOTBKOozHYGUe2WyZywYBL6L/UigGnrDon4/XTAklToVqMs+bRBCTkpiYh3Fvfvfv8z7AJ/z7Ze7/WuuLA/p3K8NexZ01FlrTSudQ93NLrhZECrBES6pMD377R67sDi3pHTTt0WrntiwUAqwcUenxPufoSMKM1TZkagn68fhsluwZ8cPI61EGc0cWXtXulXdwwnQwmEN1HEB017JFJitp/AtgztCcKX5u2L63+uUGmP4StIxdZQTM+Q1k3bKfuWyGdRp3nu5J5Q48QLGigs4YqlryNf2dxqeRZRfFxF9K7+MMxT5gCXeTj56+r/wBeqDbe<<<`
	mapGen, mapSettings, err := decodeExchangeString(input)
	if err != nil {
		t.Fatalf("decode native Factorio exchange string: %v", err)
	}
	if !bytes.Contains(mapGen, []byte(`"autoplace_controls"`)) {
		t.Fatalf("native map-gen JSON missing autoplace controls: %s", mapGen)
	}
	if !bytes.Contains(mapSettings, []byte(`"pollution"`)) {
		t.Fatalf("native map-settings JSON missing pollution: %s", mapSettings)
	}
}

func TestProfileExchangeStringRoutes(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.createProfile("Source", "default"); err != nil {
		t.Fatalf("create source profile: %v", err)
	}
	srv := &server{store: st}

	exportRec := httptest.NewRecorder()
	srv.handleProfile(exportRec, httptest.NewRequest(http.MethodGet, "/api/profiles/Source/exchange-string", nil))
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export exchange status = %d body=%s", exportRec.Code, exportRec.Body.String())
	}
	var exported exchangeStringResponse
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exported); err != nil {
		t.Fatalf("decode export response: %v", err)
	}
	if !strings.HasPrefix(exported.ExchangeString, ">>>") || !strings.HasSuffix(exported.ExchangeString, "<<<") {
		t.Fatalf("exchange string = %q, want native Factorio wrapper", exported.ExchangeString)
	}
	decodedExport, err := ParseMapExchangeString(exported.ExchangeString)
	if err != nil {
		t.Fatalf("parse exported native exchange string: %v", err)
	}
	if decodedExport.Version != [4]uint16{2, 0, 10, 0} {
		t.Fatalf("exported exchange version = %v, want 2.0.10-0", decodedExport.Version)
	}
	if _, ok := decodedExport.MapGenSettings["width"]; ok {
		t.Fatalf("exported exchange string included implicit width 0")
	}
	if _, ok := decodedExport.MapGenSettings["height"]; ok {
		t.Fatalf("exported exchange string included implicit height 0")
	}
	controls := asMap(decodedExport.MapGenSettings["autoplace_controls"])
	for _, key := range []string{"coal", "copper-ore", "iron-ore", "stone"} {
		if _, ok := controls[key]; ok {
			t.Fatalf("exported exchange string included default autoplace control %q", key)
		}
	}
	if autoplaceSettings := asMap(decodedExport.MapGenSettings["autoplace_settings"]); len(autoplaceSettings) != 0 {
		t.Fatalf("exported exchange string included default autoplace settings: %v", autoplaceSettings)
	}
	exportedMapGen, exportedMapSettings, err := decodeExchangeString(exported.ExchangeString)
	if err != nil {
		t.Fatalf("decode exported exchange string: %v", err)
	}
	if bytes.Contains(exportedMapGen, []byte(`"nauvis_cliff"`)) || bytes.Contains(exportedMapGen, []byte(`"deepwater"`)) {
		t.Fatalf("decoded exported map-gen JSON included default runtime settings: mapGen=%s", exportedMapGen)
	}
	if !bytes.Contains(exportedMapSettings, []byte(`"pollution"`)) {
		t.Fatalf("decoded exported map-settings JSON missing pollution settings: mapSettings=%s", exportedMapSettings)
	}

	doc, err := st.readProfile("Source")
	if err != nil {
		t.Fatalf("read source profile: %v", err)
	}
	var currentMapGen map[string]interface{}
	if err := json.Unmarshal(doc.MapGen, &currentMapGen); err != nil {
		t.Fatalf("decode source map gen: %v", err)
	}
	currentMapGen["width"] = 77
	currentMapGenRaw, err := json.Marshal(currentMapGen)
	if err != nil {
		t.Fatalf("marshal edited map gen: %v", err)
	}
	postBody, err := json.Marshal(exchangeStringRequest{MapGen: currentMapGenRaw, MapSettings: doc.MapSettings})
	if err != nil {
		t.Fatalf("marshal exchange request: %v", err)
	}
	postRec := httptest.NewRecorder()
	postReq := httptest.NewRequest(http.MethodPost, "/api/profiles/Source/exchange-string", bytes.NewReader(postBody))
	srv.handleProfile(postRec, postReq)
	if postRec.Code != http.StatusOK {
		t.Fatalf("export posted exchange status = %d body=%s", postRec.Code, postRec.Body.String())
	}
	var posted exchangeStringResponse
	if err := json.Unmarshal(postRec.Body.Bytes(), &posted); err != nil {
		t.Fatalf("decode posted export response: %v", err)
	}
	mapGen, mapSettings, err := decodeExchangeString(posted.ExchangeString)
	if err != nil {
		t.Fatalf("decode posted exchange string: %v", err)
	}
	if !bytes.Contains(mapGen, []byte(`"width": 77`)) {
		t.Fatalf("posted exchange did not include current map gen settings: mapGen=%s mapSettings=%s", mapGen, mapSettings)
	}

	auth, password := newTestAuthStore(t)
	srv.auth = auth
	loginReq := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(`{"username":"admin","password":"`+password+`"}`))
	login := httptest.NewRecorder()
	srv.handleSession(login, loginReq)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a cookie")
	}

	importBody, _ := json.Marshal(importExchangeStringRequest{Name: "Imported", ExchangeString: exported.ExchangeString})
	importReq := httptest.NewRequest(http.MethodPost, "/api/profiles/import-exchange", bytes.NewReader(importBody))
	importReq.AddCookie(cookies[0])
	importRec := httptest.NewRecorder()
	srv.handleProfile(importRec, importReq)
	if importRec.Code != http.StatusCreated {
		t.Fatalf("import exchange status = %d body=%s", importRec.Code, importRec.Body.String())
	}
	if _, err := st.readProfile("Imported"); err != nil {
		t.Fatalf("read imported profile: %v", err)
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

func TestStoreRejectsInvalidJSONAndSanitizesTraversal(t *testing.T) {
	st := newTestStore(t)
	doc, err := st.createProfile("../Escape", "default")
	if err != nil {
		t.Fatalf("createProfile sanitized traversal name: %v", err)
	}
	if doc.Name != "Escape" || doc.ID != "custom:Escape" {
		t.Fatalf("sanitized traversal profile = %q/%q, want Escape/custom:Escape", doc.Name, doc.ID)
	}
	if _, err := os.Stat(filepath.Join(st.customRoot, "Escape", mapGenFile)); err != nil {
		t.Fatalf("sanitized traversal profile was not written inside custom root: %v", err)
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

func TestPreviewRequestForUserLimitsGuestPreviewOptions(t *testing.T) {
	guest := previewRequestForUser(previewRequest{Size: 4096, Zoom: "out-4", Lossless: true}, nil)
	if guest.Lossless {
		t.Fatal("guest preview retained lossless output")
	}
	if guest.Size != guestMaxPreviewSize {
		t.Fatalf("guest preview size = %d, want %d", guest.Size, guestMaxPreviewSize)
	}
	if guest.Zoom != "1" {
		t.Fatalf("guest preview zoom = %q, want normal", guest.Zoom)
	}

	defaultSizedGuest := previewRequestForUser(previewRequest{}, nil)
	if defaultSizedGuest.Size != guestMaxPreviewSize {
		t.Fatalf("default guest preview size = %d, want %d", defaultSizedGuest.Size, guestMaxPreviewSize)
	}

	signedIn := previewRequestForUser(previewRequest{Size: 4096, Zoom: "out-4", Lossless: true}, &authUser{ID: 1, Username: "user"})
	if !signedIn.Lossless {
		t.Fatal("signed-in preview lost lossless output")
	}
	if signedIn.Size != 4096 || signedIn.Zoom != "out-4" {
		t.Fatalf("signed-in preview request = %#v, want unchanged", signedIn)
	}
}

func TestPreviewZoomSpec(t *testing.T) {
	tests := []struct {
		name       string
		zoom       string
		outputSize int
		wantMode   string
		wantFactor int
		wantRender int
		wantErr    bool
	}{
		{name: "normal", zoom: "", outputSize: 1024, wantMode: "normal", wantFactor: 1, wantRender: 1024},
		{name: "zoom out max", zoom: "out-4", outputSize: 4096, wantMode: "out", wantFactor: 4, wantRender: 16384},
		{name: "zoom out too large", zoom: "out-4", outputSize: 4097, wantErr: true},
		{name: "zoom in divisible", zoom: "in-3", outputSize: 768, wantMode: "in", wantFactor: 3, wantRender: 768},
		{name: "zoom in not divisible", zoom: "in-3", outputSize: 1024, wantErr: true},
		{name: "bad", zoom: "sideways-2", outputSize: 1024, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := previewZoomSpec(test.zoom, test.outputSize)
			if test.wantErr {
				if err == nil {
					t.Fatalf("previewZoomSpec(%q, %d) succeeded: %#v", test.zoom, test.outputSize, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("previewZoomSpec(%q, %d): %v", test.zoom, test.outputSize, err)
			}
			if got.mode != test.wantMode || got.factor != test.wantFactor || got.renderSize != test.wantRender {
				t.Fatalf("previewZoomSpec(%q, %d) = %#v, want mode=%s factor=%d render=%d", test.zoom, test.outputSize, got, test.wantMode, test.wantFactor, test.wantRender)
			}
		})
	}
}

func TestEncodeAVIFImageWithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 24), G: uint8(y * 24), B: 120, A: 255})
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := encodeAVIFImage(ctx, img)
	if err != nil {
		t.Fatalf("encodeAVIFImage: %v", err)
	}
	if len(data) < 16 || string(data[4:12]) != "ftypavif" {
		t.Fatalf("encoded AVIF header = % x", data[:min(len(data), 16)])
	}
}

func TestPreviewImagesRemainAvailableForSaving(t *testing.T) {
	st := newTestStore(t)
	srv := &server{
		store:     st,
		previewer: &previewer{},
	}
	name, err := srv.previewer.storePreviewImage([]byte("jpg"), "image/jpeg", ".jpg")
	if err != nil {
		t.Fatalf("storePreviewImage: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.handlePreviewImage(rec, httptest.NewRequest(http.MethodGet, "/api/previews/"+name, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("Content-Type = %q, want image/jpeg", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `inline; filename="`+name+`"` {
		t.Fatalf("Content-Disposition = %q, want inline filename", got)
	}
	if got := rec.Body.String(); got != "jpg" {
		t.Fatalf("preview body = %q, want jpg", got)
	}

	again := httptest.NewRecorder()
	srv.handlePreviewImage(again, httptest.NewRequest(http.MethodGet, "/api/previews/"+name, nil))
	if again.Code != http.StatusOK {
		t.Fatalf("second preview status = %d body=%s", again.Code, again.Body.String())
	}
	if got := again.Body.String(); got != "jpg" {
		t.Fatalf("second preview body = %q, want jpg", got)
	}

	bad := httptest.NewRecorder()
	srv.handlePreviewImage(bad, httptest.NewRequest(http.MethodGet, "/api/previews/preview-render_123.txt", nil))
	if bad.Code != http.StatusNotFound {
		t.Fatalf("bad preview filename status = %d body=%s", bad.Code, bad.Body.String())
	}
}

func TestPreviewImagesAreCapped(t *testing.T) {
	preview := &previewer{}
	var first string
	for i := 0; i < maxPreviewImages+1; i++ {
		name, err := preview.storePreviewImage([]byte("jpg"), "image/jpeg", ".jpg")
		if err != nil {
			t.Fatalf("storePreviewImage %d: %v", i, err)
		}
		if i == 0 {
			first = name
		}
	}
	if got := len(preview.images); got != maxPreviewImages {
		t.Fatalf("preview image count = %d, want %d", got, maxPreviewImages)
	}
	if _, ok := preview.getPreviewImage(first); ok {
		t.Fatal("oldest preview was retained after exceeding cap")
	}
}

func TestPinnedPreviewImageSurvivesPrune(t *testing.T) {
	preview := &previewer{}
	pinnedName, err := preview.storePinnedPreviewImage([]byte("default"), "image/jpeg", ".jpg")
	if err != nil {
		t.Fatalf("storePinnedPreviewImage: %v", err)
	}

	for i := 0; i < maxPreviewImages+1; i++ {
		if _, err := preview.storePreviewImage([]byte("jpg"), "image/jpeg", ".jpg"); err != nil {
			t.Fatalf("storePreviewImage %d: %v", i, err)
		}
	}

	img, ok := preview.getPreviewImage(pinnedName)
	if !ok {
		t.Fatal("pinned preview was pruned")
	}
	if got := string(img.data); got != "default" {
		t.Fatalf("pinned preview body = %q, want default", got)
	}
}

func TestPreviewMapGenPathUsesTemporaryRequestJSON(t *testing.T) {
	st := newTestStore(t)
	doc, err := st.createProfile("PreviewTemp", "default")
	if err != nil {
		t.Fatalf("createProfile: %v", err)
	}

	srv := &server{
		store:     st,
		previewer: &previewer{},
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

func TestBundledPresetProfilesGenerate(t *testing.T) {
	seenNames := map[string]bool{}
	seenKeys := map[string]bool{}
	for _, preset := range bundledPresetProfiles {
		if preset.Name == "" || preset.Key == "" {
			t.Fatalf("bundled preset has empty name or key: %#v", preset)
		}
		if seenNames[preset.Name] {
			t.Fatalf("duplicate bundled preset name %q", preset.Name)
		}
		if seenKeys[preset.Key] {
			t.Fatalf("duplicate bundled preset key %q", preset.Key)
		}
		seenNames[preset.Name] = true
		seenKeys[preset.Key] = true
		if _, _, err := presetDocuments(preset.Key); err != nil {
			t.Fatalf("presetDocuments(%q): %v", preset.Key, err)
		}
	}
}

func TestExpandedDefaultPresetsHaveDistinctShape(t *testing.T) {
	tests := []struct {
		preset string
		check  func(t *testing.T, mapGen, mapSettings map[string]any)
	}{
		{"rich-resources", func(t *testing.T, mapGen, mapSettings map[string]any) {
			iron := mapGen["autoplace_controls"].(map[string]any)["iron-ore"].(map[string]any)
			if iron["richness"].(float64) < 3 {
				t.Fatalf("iron richness = %#v, want rich resources", iron["richness"])
			}
		}},
		{"marathon", func(t *testing.T, mapGen, mapSettings map[string]any) {
			difficulty := mapSettings["difficulty_settings"].(map[string]any)
			if difficulty["technology_price_multiplier"] != float64(4) {
				t.Fatalf("technology multiplier = %#v, want 4", difficulty["technology_price_multiplier"])
			}
		}},
		{"death-world-marathon", func(t *testing.T, mapGen, mapSettings map[string]any) {
			if mapGen["starting_area"] != float64(0.5) {
				t.Fatalf("starting area = %#v, want small", mapGen["starting_area"])
			}
			enemyBase := mapGen["autoplace_controls"].(map[string]any)["enemy-base"].(map[string]any)
			if enemyBase["frequency"].(float64) < 3 || enemyBase["size"].(float64) < 3 {
				t.Fatalf("enemy-base autoplace = %#v, want death marathon", enemyBase)
			}
			difficulty := mapSettings["difficulty_settings"].(map[string]any)
			if difficulty["technology_price_multiplier"] != float64(4) {
				t.Fatalf("technology multiplier = %#v, want 4", difficulty["technology_price_multiplier"])
			}
			pollution := mapSettings["pollution"].(map[string]any)
			if pollution["enemy_attack_pollution_consumption_modifier"] != float64(0.8) {
				t.Fatalf("attack pollution modifier = %#v, want 0.8", pollution["enemy_attack_pollution_consumption_modifier"])
			}
		}},
		{"lakes", func(t *testing.T, mapGen, mapSettings map[string]any) {
			expressions := mapGen["property_expression_names"].(map[string]any)
			if expressions["elevation"] != "elevation_lakes" {
				t.Fatalf("elevation = %#v, want elevation_lakes", expressions["elevation"])
			}
			cliffs := mapGen["cliff_settings"].(map[string]any)
			if cliffs["cliff_smoothing"] != float64(1) {
				t.Fatalf("cliff smoothing = %#v, want 1", cliffs["cliff_smoothing"])
			}
			trees := mapGen["autoplace_controls"].(map[string]any)["trees"].(map[string]any)
			if trees["size"] != float64(0.5) {
				t.Fatalf("trees size = %#v, want lakes trees", trees["size"])
			}
		}},
		{"megabase-plain", func(t *testing.T, mapGen, mapSettings map[string]any) {
			if mapGen["starting_area"] != float64(4) {
				t.Fatalf("starting area = %#v, want huge", mapGen["starting_area"])
			}
			if richness := mapGen["cliff_settings"].(map[string]any)["richness"]; richness != float64(0) {
				t.Fatalf("cliff richness = %#v, want 0", richness)
			}
			if enabled := mapSettings["enemy_expansion"].(map[string]any)["enabled"]; enabled != false {
				t.Fatalf("enemy expansion = %#v, want false", enabled)
			}
		}},
		{"waterworld", func(t *testing.T, mapGen, mapSettings map[string]any) {
			water := mapGen["autoplace_controls"].(map[string]any)["water"].(map[string]any)
			if water["frequency"].(float64) < 2 || water["size"].(float64) < 2 {
				t.Fatalf("water autoplace = %#v, want waterworld", water)
			}
			expressions := mapGen["property_expression_names"].(map[string]any)
			if expressions["control:moisture:bias"] != "0.65" {
				t.Fatalf("moisture bias = %#v, want wet", expressions["control:moisture:bias"])
			}
		}},
		{"forest-deathworld", func(t *testing.T, mapGen, mapSettings map[string]any) {
			trees := mapGen["autoplace_controls"].(map[string]any)["trees"].(map[string]any)
			if trees["frequency"].(float64) < 3 || trees["size"].(float64) < 2 {
				t.Fatalf("trees autoplace = %#v, want dense hostile forest", trees)
			}
			if maxGroup := mapSettings["unit_group"].(map[string]any)["max_unit_group_size"]; maxGroup != float64(400) {
				t.Fatalf("max unit group size = %#v, want 400", maxGroup)
			}
		}},
		{"ore-patchwork", func(t *testing.T, mapGen, mapSettings map[string]any) {
			iron := mapGen["autoplace_controls"].(map[string]any)["iron-ore"].(map[string]any)
			if iron["frequency"].(float64) < 3 || iron["size"].(float64) >= 1 || iron["richness"].(float64) < 2.5 {
				t.Fatalf("iron autoplace = %#v, want patchwork", iron)
			}
		}},
		{"archipelago", func(t *testing.T, mapGen, mapSettings map[string]any) {
			water := mapGen["autoplace_controls"].(map[string]any)["water"].(map[string]any)
			if water["frequency"].(float64) < 3 || water["size"].(float64) < 1.5 {
				t.Fatalf("water autoplace = %#v, want archipelago", water)
			}
			expressions := mapGen["property_expression_names"].(map[string]any)
			if expressions["elevation"] != "elevation_lakes" {
				t.Fatalf("elevation = %#v, want lakes archipelago", expressions["elevation"])
			}
		}},
		{"fragmented-coast", func(t *testing.T, mapGen, mapSettings map[string]any) {
			water := mapGen["autoplace_controls"].(map[string]any)["water"].(map[string]any)
			if water["frequency"].(float64) < 4 || water["size"].(float64) > 0.5 {
				t.Fatalf("water autoplace = %#v, want fragmented coast", water)
			}
			cliffs := mapGen["cliff_settings"].(map[string]any)
			if cliffs["richness"].(float64) < 3 {
				t.Fatalf("cliff richness = %#v, want fragmented coast", cliffs["richness"])
			}
		}},
		{"hive-expansion", func(t *testing.T, mapGen, mapSettings map[string]any) {
			enemyBase := mapGen["autoplace_controls"].(map[string]any)["enemy-base"].(map[string]any)
			if enemyBase["frequency"].(float64) > 0.2 || enemyBase["size"].(float64) < 4 {
				t.Fatalf("enemy-base autoplace = %#v, want rare huge hives", enemyBase)
			}
			expansion := mapSettings["enemy_expansion"].(map[string]any)
			if expansion["max_expansion_distance"] != float64(20) || expansion["settler_group_max_size"] != float64(50) {
				t.Fatalf("enemy expansion = %#v, want hive expansion", expansion)
			}
		}},
		{"sparse-rich-desert", func(t *testing.T, mapGen, mapSettings map[string]any) {
			iron := mapGen["autoplace_controls"].(map[string]any)["iron-ore"].(map[string]any)
			if iron["frequency"].(float64) > 0.3 || iron["richness"].(float64) < 3 {
				t.Fatalf("iron autoplace = %#v, want sparse rich desert", iron)
			}
			expressions := mapGen["property_expression_names"].(map[string]any)
			if expressions["control:moisture:bias"] != "-0.55" {
				t.Fatalf("moisture bias = %#v, want desert", expressions["control:moisture:bias"])
			}
		}},
		{"island-escape", func(t *testing.T, mapGen, mapSettings map[string]any) {
			expressions := mapGen["property_expression_names"].(map[string]any)
			if expressions["elevation"] != "elevation_island" {
				t.Fatalf("elevation = %#v, want island escape", expressions["elevation"])
			}
			stone := mapGen["autoplace_controls"].(map[string]any)["stone"].(map[string]any)
			if stone["frequency"].(float64) > 0.4 || stone["size"].(float64) > 0.5 {
				t.Fatalf("stone autoplace = %#v, want constrained island escape", stone)
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
