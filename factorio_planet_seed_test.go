package main

import "testing"

func TestParseFastPreviewSettingsUsesFactorioPlanetSeedOffsets(t *testing.T) {
	const mapSeed = uint32(246813579)
	mapGen := []byte(`{"seed": 246813579}`)
	tests := []struct {
		planet     string
		wantPlanet string
		wantSeed   uint32
	}{
		{planet: fastPreviewPlanetNauvis, wantPlanet: fastPreviewPlanetNauvis, wantSeed: mapSeed},
		{planet: " VULCANUS ", wantPlanet: fastPreviewPlanetVulcanus, wantSeed: 1496626370},
		{planet: fastPreviewPlanetGleba, wantPlanet: fastPreviewPlanetGleba, wantSeed: 3461896550},
		{planet: fastPreviewPlanetFulgora, wantPlanet: fastPreviewPlanetFulgora, wantSeed: 3214392589},
		{planet: fastPreviewPlanetAquilo, wantPlanet: fastPreviewPlanetAquilo, wantSeed: 3358613451},
	}
	for _, test := range tests {
		t.Run(test.wantPlanet, func(t *testing.T) {
			settings, err := parseFastPreviewSettingsForPlanet(mapGen, "", test.planet)
			if err != nil {
				t.Fatalf("parse %s Fast preview settings: %v", test.wantPlanet, err)
			}
			if settings.planet != test.wantPlanet {
				t.Fatalf("planet = %q, want %q", settings.planet, test.wantPlanet)
			}
			if settings.seed != test.wantSeed {
				t.Fatalf("%s surface seed = %d, want %d", test.wantPlanet, settings.seed, test.wantSeed)
			}
		})
	}
}

func TestFastPreviewPlanetSeedOffsetUsesOverrideBeforeOffset(t *testing.T) {
	settings, err := parseFastPreviewSettingsForPlanet(
		[]byte(`{"seed": 1}`),
		"246813579",
		fastPreviewPlanetVulcanus,
	)
	if err != nil {
		t.Fatalf("parse overridden Vulcanus Fast preview settings: %v", err)
	}
	if settings.seed != 1496626370 {
		t.Fatalf("overridden Vulcanus surface seed = %d, want 1496626370", settings.seed)
	}
}

func TestFastPreviewSurfaceSeedWrapsUint32(t *testing.T) {
	tests := []struct {
		name    string
		mapSeed uint32
		planet  string
		want    uint32
	}{
		{
			name:    "maximum map seed",
			mapSeed: ^uint32(0),
			planet:  fastPreviewPlanetVulcanus,
			want:    1249812790,
		},
		{
			name:    "wrapped surface seed can be zero",
			mapSeed: 3045154505,
			planet:  fastPreviewPlanetVulcanus,
			want:    0,
		},
		{
			name:    "Nauvis keeps maximum map seed",
			mapSeed: ^uint32(0),
			planet:  fastPreviewPlanetNauvis,
			want:    ^uint32(0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := fastPreviewSurfaceSeed(test.mapSeed, test.planet); got != test.want {
				t.Fatalf(
					"fastPreviewSurfaceSeed(%d, %q) = %d, want %d",
					test.mapSeed,
					test.planet,
					got,
					test.want,
				)
			}
		})
	}
}
