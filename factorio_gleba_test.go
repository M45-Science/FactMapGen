package main

import (
	"context"
	"encoding/json"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

type glebaOracleFixture struct {
	Seed      uint32
	Positions [][2]float64
	Climate   map[string][]string
	Tiles     map[string][]string
}

func TestFactorioGlebaReleaseCases(t *testing.T) {
	b, err := os.ReadFile("testdata/gleba/oracle-release-2.1.23.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		FactorioVersion string
		Cases           []struct {
			Seed                                      uint32
			WaterFrequency, WaterSize, PlantFrequency float64
			StartingPoints                            []struct{ X, Y float64 }
			Positions                                 [][2]float64
			Fields                                    map[string][]float32
		}
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.FactorioVersion, "2.1.23") {
		t.Fatal(f.FactorioVersion)
	}
	for _, c := range f.Cases {
		t.Run(strconv.FormatUint(uint64(c.Seed), 10), func(t *testing.T) {
			settings := fastPreviewSettings{seed: c.Seed, planet: fastPreviewPlanetGleba, autoplaceControls: map[string]fastControl{
				"gleba_water":  {frequency: positiveOr(c.WaterFrequency, 1), size: positiveOr(c.WaterSize, 1), richness: 1, enabled: true},
				"gleba_plants": {frequency: positiveOr(c.PlantFrequency, 1), size: 1, richness: 1, enabled: true},
			}}
			for _, p := range c.StartingPoints {
				settings.startingPositions = append(settings.startingPositions, factorioPoint{x: p.X, y: p.Y})
			}
			e := newFactorioGlebaEvaluator(settings, false)
			worst := map[string]float64{}
			winners := 0
			for i, p := range c.Positions {
				s := e.sample(p[0], p[1])
				for name, v := range map[string]float32{"elevation": s.elevation, "aux": s.aux, "moisture": s.moisture, "temperature": s.temperature} {
					d := math.Abs(float64(v - c.Fields[name][i]))
					worst[name] = max(worst[name], d)
				}
				maxWant := float32(math.Inf(-1))
				for j, tile := range factorioGlebaTiles {
					want := c.Fields["tile:"+tile.name+":probability"][i]
					maxWant = max(maxWant, want)
					checkGlebaScore(t, tile.name, i, s.scores[j], want)
				}
				if c.Fields["tile:"+s.tile.name+":probability"][i] == maxWant {
					winners++
				}
			}
			t.Logf("tile winners %d/%d; residuals %v", winners, len(c.Positions), worst)
			if winners != len(c.Positions) {
				t.Errorf("tile winners %d/%d", winners, len(c.Positions))
			}
			for name, v := range worst {
				if v > .00001 {
					t.Errorf("%s residual %g", name, v)
				}
			}
		})
	}
}

func checkGlebaScore(t *testing.T, name string, index int, got, want float32) {
	t.Helper()
	d := math.Abs(float64(got - want))
	// Soil probabilities multiply small field residuals by 50,000 or 1,000,000.
	if math.IsNaN(float64(got)) || d > .005+math.Abs(float64(want))*2e-6 {
		t.Fatalf("%s[%d]: got %.9g want %.9g (residual %g)", name, index, got, want, d)
	}
}

func glebaOracleFloat(t *testing.T, hex string) float32 {
	t.Helper()
	b, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	return math.Float32frombits(uint32(b))
}
func TestFactorioGlebaFields(t *testing.T) {
	b, err := os.ReadFile("testdata/gleba/oracle-gleba-fields.seed123456.json")
	if err != nil {
		t.Fatal(err)
	}
	var f glebaOracleFixture
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	e := newFactorioGlebaEvaluator(fastPreviewSettings{seed: f.Seed, planet: fastPreviewPlanetGleba}, false)
	worst := map[string]float64{}
	winners := 0
	for i, p := range f.Positions {
		s := e.sample(p[0], p[1])
		for name, v := range map[string]float32{"elevation": s.elevation, "aux": s.aux, "moisture": s.moisture, "temperature": s.temperature} {
			d := math.Abs(float64(v - glebaOracleFloat(t, f.Climate[name][i])))
			if d > worst[name] {
				worst[name] = d
			}
		}
		maxWant := float32(math.Inf(-1))
		for j, tile := range factorioGlebaTiles {
			v := glebaOracleFloat(t, f.Tiles[tile.name][i])
			maxWant = max(maxWant, v)
			d := math.Abs(float64(s.scores[j] - v))
			checkGlebaScore(t, tile.name, i, s.scores[j], v)
			if d > worst[tile.name] {
				worst[tile.name] = d
			}
		}
		if glebaOracleFloat(t, f.Tiles[s.tile.name][i]) == maxWant {
			winners++
		}
	}
	t.Logf("winning tiles: %d/%d; residuals: %v", winners, len(f.Positions), worst)
	if winners != len(f.Positions) {
		t.Errorf("tile winners: %d/%d", winners, len(f.Positions))
	}
	for _, name := range []string{"elevation", "aux", "moisture", "temperature"} {
		if worst[name] > .00001 {
			t.Errorf("%s residual %g", name, worst[name])
		}
	}
}
func TestFactorioGlebaPreviewOracle(t *testing.T) {
	f, err := os.Open("testdata/gleba/oracle-preview-gleba-terrain.seed123456.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ref, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	e := newFactorioGlebaEvaluator(fastPreviewSettings{seed: fastPreviewSurfaceSeed(123456, fastPreviewPlanetGleba), planet: fastPreviewPlanetGleba}, true)
	diff := 0
	pairs := map[string]int{}
	for y := 0; y < ref.Bounds().Dy(); y++ {
		for x := 0; x < ref.Bounds().Dx(); x++ {
			s := e.sample(float64(x-ref.Bounds().Dx()/2), float64(y-ref.Bounds().Dy()/2))
			got := s.tile.color
			r, g, b, _ := ref.At(x, y).RGBA()
			if got.R != uint8(r>>8) || got.G != uint8(g>>8) || got.B != uint8(b>>8) {
				diff++
				if diff <= 5 {
					t.Logf("at %d,%d: %s %v -> %d,%d,%d, elevation=%g aux=%g scores=%v", x-ref.Bounds().Dx()/2, y-ref.Bounds().Dy()/2, s.tile.name, got, r>>8, g>>8, b>>8, s.elevation, s.aux, s.scores)
				}
				pairs[s.tile.name]++
			}
		}
	}
	t.Logf("Gleba preview differences: %d/%d; %v", diff, ref.Bounds().Dx()*ref.Bounds().Dy(), pairs)
	if diff != 0 {
		t.Errorf("too many differences: %d", diff)
	}
}

func TestFactorioGlebaReleasePreviews(t *testing.T) {
	for _, c := range []struct {
		seed                    uint32
		frequency, size, plants float64
	}{{123456, 1, 1, 1}, {987654321, .5, 2, 3}} {
		t.Run(strconv.FormatUint(uint64(c.seed), 10), func(t *testing.T) {
			f, err := os.Open("testdata/gleba/oracle-preview-gleba-2.1.23.seed" + strconv.FormatUint(uint64(c.seed), 10) + ".png")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			ref, err := png.Decode(f)
			if err != nil {
				t.Fatal(err)
			}
			settings := fastPreviewSettings{seed: fastPreviewSurfaceSeed(c.seed, fastPreviewPlanetGleba), planet: fastPreviewPlanetGleba, noEnemies: true, autoplaceControls: map[string]fastControl{
				"gleba_water":      {frequency: c.frequency, size: c.size, richness: 1, enabled: true},
				"gleba_plants":     {frequency: c.plants, size: 1, richness: 1, enabled: false},
				"gleba_stone":      {frequency: 1, size: 0, richness: 1, enabled: false},
				"gleba_enemy_base": {frequency: 1, size: 0, richness: 1, enabled: false},
			}}
			got, err := newFastPreviewWorld(settings).render(context.Background(), 512, 1, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			diff := 0
			e := newFactorioGlebaEvaluator(settings, true)
			for y := 0; y < 512; y++ {
				for x := 0; x < 512; x++ {
					a := got.RGBAAt(x, y)
					r, g, b, _ := ref.At(x, y).RGBA()
					if a.R != uint8(r>>8) || a.G != uint8(g>>8) || a.B != uint8(b>>8) {
						diff++
						if diff <= 5 {
							s := e.sample(float64(x-256), float64(y-256))
							t.Logf("%d,%d: %s %v -> %d,%d,%d elev=%g aux=%g scores=%v", x-256, y-256, s.tile.name, a, r>>8, g>>8, b>>8, s.elevation, s.aux, s.scores)
						}
					}
				}
			}
			t.Logf("live 2.1.23 production renderer differences %d/262144", diff)
			if diff != 0 {
				t.Errorf("terrain differences %d", diff)
			}
		})
	}
}
