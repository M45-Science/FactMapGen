package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestFailedChannelSwitchKeepsPreviousChannel(t *testing.T) {
	latest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer latest.Close()
	manager := newFactorioManager(filepath.Join(t.TempDir(), "factorio"), nil, true, factorioReleaseExperimental)
	manager.latestURL = latest.URL
	manager.latestCache = factorioLatestCache{version: "2.1.23"}
	before := manager.latestCache
	status, err := manager.installFresh(context.Background(), factorioReleaseStable)
	if err == nil {
		t.Fatal("install succeeded despite unavailable release server")
	}
	if status.ReleaseChannel != "experimental" || manager.downloadURL != factorioExperimentalDownloadURL || manager.latestCache != before {
		t.Fatalf("failed install changed channel or cache: %+v", status)
	}
}
