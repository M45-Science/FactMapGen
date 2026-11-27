package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

var measuredPreviewBytes []byte

func BenchmarkPerformanceFrames(b *testing.B) {
	for _, planet := range fastPreviewTestPlanets {
		for _, size := range []int{512, 1024} {
			b.Run(fmt.Sprintf("%s/%d", planet, size), func(b *testing.B) {
				raw := json.RawMessage(`{"seed":123456}`)
				settings, err := parseFastPreviewSettingsForPlanet(raw, "", planet)
				if err != nil {
					b.Fatal(err)
				}
				key, err := fastPreviewCacheKeyForPlanet(raw, settings.seed, planet)
				if err != nil {
					b.Fatal(err)
				}
				ctx := context.Background()
				for _, warm := range []bool{false, true} {
					name := "cold"
					if warm {
						name = "warm"
					}
					b.Run(name, func(b *testing.B) {
						var cache *fastPreviewCache
						if warm {
							cache = newFastPreviewCache(defaultFastPreviewCacheBytes, defaultFastPreviewCacheWorlds)
							if _, err := cache.renderFrame(ctx, key, settings, size, 0, 0); err != nil {
								b.Fatal(err)
							}
						}
						b.ReportAllocs()
						b.ResetTimer()
						for range b.N {
							if !warm {
								cache = newFastPreviewCache(defaultFastPreviewCacheBytes, defaultFastPreviewCacheWorlds)
							}
							img, err := cache.renderFrame(ctx, key, settings, size, 0, 0)
							if err != nil {
								b.Fatal(err)
							}
							benchmarkPreviewImage = img
						}
					})
				}
			})
		}
	}
}

func BenchmarkPerformanceBrowser(b *testing.B) {
	raw, err := os.ReadFile("default-presets/Default/" + mapGenFile)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	ref := profileRef{Source: profileSourceDefault, Name: "Default"}
	for _, planet := range fastPreviewTestPlanets {
		for _, zoom := range []string{"1", "4"} {
			b.Run(planet+"/zoom-"+zoom, func(b *testing.B) {
				request := previewRequest{Engine: "fast", Size: 512, Planet: planet, Seed: "123456", Zoom: zoom, CenterX: 256, CenterY: -256}
				for _, warm := range []bool{false, true} {
					name := "cold"
					if warm {
						name = "warm"
					}
					b.Run(name, func(b *testing.B) {
						request.Format = "avif"
						var renderer *previewer
						if warm {
							renderer = &previewer{}
							if _, _, err := renderer.renderFastImage(ctx, ref, raw, request); err != nil {
								b.Fatal(err)
							}
						}
						b.ReportAllocs()
						b.ResetTimer()
						for range b.N {
							if !warm {
								renderer = &previewer{}
							}
							_, image, err := renderer.renderFastImage(ctx, ref, raw, request)
							if err != nil {
								b.Fatal(err)
							}
							measuredPreviewBytes = image.data
						}
					})
				}
			})
		}
	}
}

func BenchmarkPerformanceHTTPWarm(b *testing.B) {
	s := &server{
		store:     &store{defaultRoot: "default-presets", customRoot: b.TempDir()},
		previewer: &previewer{},
	}
	request := func() {
		rec := httptest.NewRecorder()
		body := `{"engine":"fast","planet":"nauvis","size":512,"seed":"123456","zoom":"1","centerX":256,"centerY":-256}`
		req := httptest.NewRequest(http.MethodPost, "/api/profiles/default:Default/preview-image", strings.NewReader(body))
		s.handleProfile(rec, req)
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/avif" {
			b.Fatalf("tile response status=%d type=%q", rec.Code, rec.Header().Get("Content-Type"))
		}
		measuredPreviewBytes = rec.Body.Bytes()
	}
	request()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		request()
	}
}
