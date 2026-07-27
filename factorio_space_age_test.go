package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/draw"
	"strings"
	"testing"
)

var fastPreviewTestPlanets = []string{
	fastPreviewPlanetNauvis,
	fastPreviewPlanetVulcanus,
	fastPreviewPlanetGleba,
	fastPreviewPlanetFulgora,
	fastPreviewPlanetAquilo,
}

var fastPreviewSpaceAgeTestPlanets = []string{
	fastPreviewPlanetVulcanus,
	fastPreviewPlanetGleba,
	fastPreviewPlanetFulgora,
	fastPreviewPlanetAquilo,
}

var fastPreviewSpaceAgeTestMapGen = json.RawMessage(`{
	"seed": 246813579,
	"starting_area": 1,
	"starting_points": [{"x": 0, "y": 0}]
}`)

func TestNormalizeFastPreviewPlanet(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: fastPreviewPlanetNauvis},
		{input: " NAUVIS ", want: fastPreviewPlanetNauvis},
		{input: "VULCANUS", want: fastPreviewPlanetVulcanus},
		{input: " Gleba ", want: fastPreviewPlanetGleba},
		{input: "FULGORA", want: fastPreviewPlanetFulgora},
		{input: " aquilo ", want: fastPreviewPlanetAquilo},
	}
	for _, test := range tests {
		t.Run(strings.TrimSpace(test.input), func(t *testing.T) {
			got, err := normalizeFastPreviewPlanet(test.input)
			if err != nil {
				t.Fatalf("normalizeFastPreviewPlanet(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("normalizeFastPreviewPlanet(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}

	for _, input := range []string{"mars", "gleba-2", "nauvis/moon"} {
		if got, err := normalizeFastPreviewPlanet(input); err == nil {
			t.Fatalf("normalizeFastPreviewPlanet(%q) = %q, nil; want an error", input, got)
		}
	}
}

func TestFastPreviewSpaceAgePlanetsRenderDeterministically(t *testing.T) {
	const size = 96
	hashes := make(map[[sha256.Size]byte]string, len(fastPreviewSpaceAgeTestPlanets))
	for _, planet := range fastPreviewSpaceAgeTestPlanets {
		t.Run(planet, func(t *testing.T) {
			settings := fastPreviewSpaceAgeSettings(t, planet)
			first, err := newFastPreviewWorld(settings).render(context.Background(), size, 1, 0, 0)
			if err != nil {
				t.Fatalf("render first %s preview: %v", planet, err)
			}
			second, err := newFastPreviewWorld(settings).render(context.Background(), size, 1, 0, 0)
			if err != nil {
				t.Fatalf("render second %s preview: %v", planet, err)
			}
			if !bytes.Equal(first.Pix, second.Pix) {
				t.Fatalf("%s preview is not deterministic", planet)
			}

			colors := make(map[[3]byte]struct{})
			for offset := 0; offset < len(first.Pix); offset += 4 {
				if first.Pix[offset+3] != 255 {
					t.Fatalf("%s pixel %d has alpha %d, want 255", planet, offset/4, first.Pix[offset+3])
				}
				colors[[3]byte{first.Pix[offset], first.Pix[offset+1], first.Pix[offset+2]}] = struct{}{}
			}
			if len(colors) < 2 {
				t.Fatalf("%s preview has only %d color, want nontrivial terrain", planet, len(colors))
			}

			hash := sha256.Sum256(first.Pix)
			if other, exists := hashes[hash]; exists {
				t.Fatalf("%s and %s produced the same preview hash %x", planet, other, hash)
			}
			hashes[hash] = planet
		})
	}
}

func TestFastPreviewSpaceAgeControlsAffectOutput(t *testing.T) {
	tests := []struct {
		planet  string
		control string
	}{
		{planet: fastPreviewPlanetVulcanus, control: "vulcanus_coal"},
		{planet: fastPreviewPlanetGleba, control: "gleba_plants"},
		{planet: fastPreviewPlanetFulgora, control: "scrap"},
		{planet: fastPreviewPlanetAquilo, control: "aquilo_crude_oil"},
	}
	for _, test := range tests {
		t.Run(test.planet+"/"+test.control, func(t *testing.T) {
			settings := fastPreviewSpaceAgeSettings(t, test.planet)
			enabled, err := newFastPreviewWorld(settings).render(context.Background(), 256, 1, 0, 0)
			if err != nil {
				t.Fatalf("render enabled control: %v", err)
			}
			control := settings.autoplaceControls[test.control]
			control.enabled = false
			control.frequency = 0
			control.size = 0
			settings.autoplaceControls[test.control] = control
			disabled, err := newFastPreviewWorld(settings).render(context.Background(), 256, 1, 0, 0)
			if err != nil {
				t.Fatalf("render disabled control: %v", err)
			}
			if bytes.Equal(enabled.Pix, disabled.Pix) {
				t.Fatalf("disabling %s did not change the %s preview", test.control, test.planet)
			}
		})
	}
}

func TestFastPreviewCacheKeySeparatesPlanets(t *testing.T) {
	keys := make(map[fastPreviewWorldKey]string, len(fastPreviewTestPlanets))
	for _, planet := range fastPreviewTestPlanets {
		key, err := fastPreviewCacheKeyForPlanet(fastPreviewSpaceAgeTestMapGen, 246813579, planet)
		if err != nil {
			t.Fatalf("fastPreviewCacheKeyForPlanet(%s): %v", planet, err)
		}
		if other, exists := keys[key]; exists {
			t.Fatalf("%s and %s produced the same Fast cache key", planet, other)
		}
		keys[key] = planet
	}

	canonical, err := fastPreviewCacheKeyForPlanet(fastPreviewSpaceAgeTestMapGen, 246813579, "vulcanus")
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := fastPreviewCacheKeyForPlanet(fastPreviewSpaceAgeTestMapGen, 246813579, " VULCANUS ")
	if err != nil {
		t.Fatal(err)
	}
	if canonical != normalized {
		t.Fatal("equivalent Vulcanus names produced different Fast cache keys")
	}
}

func TestFastPreviewSpaceAgeCacheMatchesDirectAndStitches(t *testing.T) {
	const (
		fullSize = 256
		tileSize = fullSize / 2
	)
	for _, planet := range fastPreviewSpaceAgeTestPlanets {
		t.Run(planet, func(t *testing.T) {
			settings := fastPreviewSpaceAgeSettings(t, planet)
			key, err := fastPreviewCacheKeyForPlanet(
				fastPreviewSpaceAgeTestMapGen,
				settings.seed,
				planet,
			)
			if err != nil {
				t.Fatalf("build %s cache key: %v", planet, err)
			}
			direct, err := newFastPreviewWorld(settings).render(
				context.Background(), fullSize, 1, 0, 0,
			)
			if err != nil {
				t.Fatalf("render direct %s preview: %v", planet, err)
			}

			cache := newFastPreviewCache(8<<20, 2)
			stitched := image.NewRGBA(image.Rect(0, 0, fullSize, fullSize))
			for tileY := 0; tileY < 2; tileY++ {
				for tileX := 0; tileX < 2; tileX++ {
					centerX := float64(tileX*2-1) * tileSize / 2
					centerY := float64(tileY*2-1) * tileSize / 2
					tile, renderErr := cache.render(
						context.Background(),
						key,
						settings,
						tileSize,
						1,
						centerX,
						centerY,
					)
					if renderErr != nil {
						t.Fatalf("render %s client tile %d,%d: %v", planet, tileX, tileY, renderErr)
					}
					target := image.Rect(
						tileX*tileSize,
						tileY*tileSize,
						(tileX+1)*tileSize,
						(tileY+1)*tileSize,
					)
					draw.Draw(stitched, target, tile, tile.Bounds().Min, draw.Src)
				}
			}
			assertFastPreviewImagesEqual(t, stitched, direct)

			cached, err := cache.render(context.Background(), key, settings, fullSize, 1, 0, 0)
			if err != nil {
				t.Fatalf("render cached %s preview: %v", planet, err)
			}
			assertFastPreviewImagesEqual(t, cached, direct)
			stats := cache.stats()
			if stats.Tiles != 4 || stats.Hits < 4 {
				t.Fatalf("%s cache stats = %#v, want four retained tiles and at least four hits", planet, stats)
			}
		})
	}
}

func TestRenderFastSupportsEveryBuiltInPlanet(t *testing.T) {
	preview := &previewer{fastPreviewCacheBytes: 32 << 20}
	for _, planet := range fastPreviewTestPlanets {
		t.Run(planet, func(t *testing.T) {
			response, err := preview.renderFast(
				context.Background(),
				profileRef{Source: profileSourceLocal, Name: "All planets"},
				fastPreviewSpaceAgeTestMapGen,
				previewRequest{Size: 256, Planet: planet, Seed: "246813579"},
			)
			if err != nil {
				t.Fatalf("renderFast %s: %v", planet, err)
			}
			if response.Engine != previewEngineFast || response.Planet != planet || response.Size != 256 {
				t.Fatalf("%s response = %#v", planet, response)
			}
			path := strings.SplitN(response.URL, "?", 2)[0]
			name := path[strings.LastIndex(path, "/")+1:]
			stored, ok := preview.getPreviewImage(name)
			if !ok || stored.contentType != "image/png" || len(stored.data) == 0 {
				t.Fatalf("%s stored image = %#v, present=%v", planet, stored, ok)
			}
		})
	}
	if worlds := preview.fastCache().stats().Worlds; worlds != len(fastPreviewTestPlanets) {
		t.Fatalf("Fast cache worlds = %d, want %d", worlds, len(fastPreviewTestPlanets))
	}
}

func TestRenderFastRejectsUnsupportedPlanet(t *testing.T) {
	preview := &previewer{}
	_, err := preview.renderFast(
		context.Background(),
		profileRef{Source: profileSourceLocal, Name: "Unsupported planet"},
		json.RawMessage(`{"seed": 123456}`),
		previewRequest{Size: 256, Planet: "mars"},
	)
	if err == nil {
		t.Fatal("renderFast accepted unsupported planet mars")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not supported") {
		t.Fatalf("renderFast unsupported-planet error = %q", err)
	}
	if preview.fastPreviewCache != nil {
		t.Fatal("renderFast initialized the tile cache before rejecting an unsupported planet")
	}
}

func fastPreviewSpaceAgeSettings(t *testing.T, planet string) fastPreviewSettings {
	t.Helper()
	settings, err := parseFastPreviewSettingsForPlanet(
		fastPreviewSpaceAgeTestMapGen,
		"",
		planet,
	)
	if err != nil {
		t.Fatalf("parse %s Fast preview settings: %v", planet, err)
	}
	if settings.planet != planet {
		t.Fatalf("%s settings planet = %q", planet, settings.planet)
	}
	world := newFastPreviewWorld(settings)
	if world.spaceAge == nil || world.nauvis != nil {
		t.Fatalf("%s world evaluators = spaceAge %v, nauvis %v", planet, world.spaceAge != nil, world.nauvis != nil)
	}
	return settings
}
