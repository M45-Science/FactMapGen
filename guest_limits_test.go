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
	body := `{"maxPreviewSize":2048,"allowZoom":true,"allowExact":true,"allowPNG":true}`
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
	if err != nil || limits != (guestLimits{2048, true, true, true}) {
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
		{guestLimits{1024, true, false, false}, previewEngineFast, "4", "avif"},
		{guestLimits{1024, false, true, false}, previewEngineFactorio, "1", "avif"},
		{guestLimits{1024, false, false, true}, previewEngineFast, "1", "png"},
		{guestLimits{1024, true, true, true}, previewEngineFactorio, "4", "png"},
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
