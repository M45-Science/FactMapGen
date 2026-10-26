package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGuestLimitsPersistencePermissionsAndConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.db")
	auth, password, err := openAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = auth.close() })
	srv := &server{auth: auth}
	limits, err := srv.loadGuestLimits()
	if err != nil || limits != defaultGuestLimits() {
		t.Fatalf("defaults: %+v, %v", limits, err)
	}
	admin, err := auth.authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	regular, err := auth.createUser("reader", "password123", false)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, _, err := auth.createSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	regularToken, _, err := auth.createSession(regular.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"maxPreviewSize":2048,"maxViewSize":16384,"allowZoom":true,"allowExact":true,"allowPNG":true}`
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {regularToken, 403}, {adminToken, 200}} {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/guest-limits", strings.NewReader(body))
		if tc.token != "" {
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tc.token})
		}
		rec := httptest.NewRecorder()
		srv.handleGuestLimits(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
		}
	}
	// A separate database connection reads the policy saved on disk.
	reopened, _, err := openAuthStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.close() })
	restarted := &server{auth: reopened}
	limits, err = restarted.loadGuestLimits()
	if err != nil || limits != (guestLimits{MaxPreviewSize: 2048, MaxViewSize: 16384, AllowZoom: true, AllowExact: true, AllowPNG: true}) {
		t.Fatalf("saved limits: %+v, %v", limits, err)
	}
	rec := httptest.NewRecorder()
	restarted.handleConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfg appConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil || cfg.GuestLimits != limits {
		t.Fatalf("public config=%s err=%v", rec.Body.String(), err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/guest-limits", strings.NewReader(`{"maxPreviewSize":4097}`))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: adminToken})
	rec = httptest.NewRecorder()
	srv.handleGuestLimits(rec, req)
	if rec.Code != 400 {
		t.Fatalf("invalid status=%d", rec.Code)
	}
	unchanged, err := srv.loadGuestLimits()
	if err != nil || unchanged != limits {
		t.Fatalf("invalid update changed policy: %+v, %v", unchanged, err)
	}
	entries, err := auth.listAudit(10)
	if err != nil || len(entries) != 1 || entries[0].TargetID != "guest_limits" {
		t.Fatalf("audit=%+v err=%v", entries, err)
	}
}

func TestPreviewRequestConfigurableGuestLimits(t *testing.T) {
	req := previewRequest{Engine: previewEngineFactorio, Size: 4096, Zoom: "4", Format: "png"}
	for _, tc := range []struct {
		limits               guestLimits
		engine, zoom, format string
	}{
		{defaultGuestLimits(), previewEngineFast, "1", "avif"},
		{guestLimits{MaxPreviewSize: 1024, MaxViewSize: 8192, AllowZoom: true, AllowExact: false, AllowPNG: false}, previewEngineFast, "4", "avif"},
		{guestLimits{MaxPreviewSize: 1024, MaxViewSize: 8192, AllowZoom: false, AllowExact: true, AllowPNG: false}, previewEngineFactorio, "1", "avif"},
		{guestLimits{MaxPreviewSize: 1024, MaxViewSize: 8192, AllowZoom: false, AllowExact: false, AllowPNG: true}, previewEngineFast, "1", "png"},
		{guestLimits{MaxPreviewSize: 1024, MaxViewSize: 8192, AllowZoom: true, AllowExact: true, AllowPNG: true}, previewEngineFactorio, "4", "png"},
	} {
		got := previewRequestWithLimits(req, nil, tc.limits)
		if got.Size != tc.limits.MaxPreviewSize || got.Engine != tc.engine || got.Zoom != tc.zoom || got.Format != tc.format {
			t.Fatalf("limits=%+v request=%+v", tc.limits, got)
		}
		if got := previewRequestWithLimits(req, &authUser{}, tc.limits); !reflect.DeepEqual(got, req) {
			t.Fatalf("signed in request changed: %+v", got)
		}
	}
}

func TestGuestViewSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		zoom string
		cap  int
		want string
	}{
		{"boundary", 512, "16", 8192, "16"},
		{"zoom out", 512, "64", 8192, "16"},
		{"large image", 4096, "4", 8192, "2"},
		{"legacy zoom", 1024, "out-16", 8192, "8"},
		{"zoom in", 1024, "in-4", 8192, "in-4"},
		{"custom cap", 2048, "16", 4096, "2"},
		{"small cap", 4096, "1", 512, "0.125"},
		{"invalid zoom", 512, "NaN", 8192, "NaN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := guestLimits{MaxPreviewSize: 4096, MaxViewSize: tc.cap, AllowZoom: true}
			req := previewRequest{Size: tc.size, Zoom: tc.zoom}
			if got := previewRequestWithLimits(req, nil, limits); got.Zoom != tc.want {
				t.Fatalf("zoom = %q, want %q", got.Zoom, tc.want)
			}
			if got := previewRequestWithLimits(req, &authUser{}, limits); got.Zoom != tc.zoom {
				t.Fatalf("signed-in zoom changed: %q", got.Zoom)
			}
		})
	}
}

func TestGuestViewSizeSettingsValidationAndMigration(t *testing.T) {
	auth, password, err := openAuthStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = auth.close() })
	srv := &server{auth: auth}
	// Older saved policies inherit the new default without losing their settings.
	_, err = auth.db.Exec(`INSERT INTO app_settings(key, value) VALUES ('guest_limits', '{"maxPreviewSize":1024,"allowZoom":true}')`)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := srv.loadGuestLimits()
	if err != nil || limits.MaxViewSize != 8192 || !limits.AllowZoom || limits.MaxPreviewSize != 1024 {
		t.Fatalf("legacy policy: %+v, %v", limits, err)
	}
	admin, err := auth.authenticate("admin", password)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.createSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"maxViewSize":0}`, `{"maxViewSize":511}`, `{"maxViewSize":262145}`} {
		req := httptest.NewRequest(http.MethodPut, "/api/admin/guest-limits", strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
		rec := httptest.NewRecorder()
		srv.handleGuestLimits(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("invalid policy %s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
}
