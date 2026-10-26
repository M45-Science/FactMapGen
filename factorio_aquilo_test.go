package main

import (
	"encoding/json"
	"image/png"
	"math"
	"os"
	"strconv"
	"testing"
)

func TestFactorioAquiloPreviewOracle(t *testing.T) {
	f, err := os.Open("testdata/space-age-upstream/oracle-preview-aquilo-terrain.seed123456.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ref, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Bounds().Dx() != 1024 || ref.Bounds().Dy() != 1024 {
		t.Fatalf("unexpected oracle dimensions: %v", ref.Bounds())
	}
	controls := map[string]fastControl{}
	for _, name := range factorioAquiloControls {
		controls[name] = fastControl{frequency: 1, size: 0, richness: 1}
	}
	e := newFactorioAquiloEvaluator(fastPreviewSettings{seed: fastPreviewSurfaceSeed(123456, fastPreviewPlanetAquilo), planet: fastPreviewPlanetAquilo, autoplaceControls: controls}, true)
	differences := 0
	for y := 0; y < ref.Bounds().Dy(); y++ {
		for x := 0; x < ref.Bounds().Dx(); x++ {
			got := e.sample(float64(x-ref.Bounds().Dx()/2), float64(y-ref.Bounds().Dy()/2)).tile.color
			r, g, b, _ := ref.At(x, y).RGBA()
			if got.R != uint8(r>>8) || got.G != uint8(g>>8) || got.B != uint8(b>>8) {
				differences++
			}
		}
	}
	t.Logf("Aquilo preview differences: %d/%d", differences, ref.Bounds().Dx()*ref.Bounds().Dy())
	if differences != 0 {
		t.Fatalf("too many differences: %d", differences)
	}
}

func TestFactorioAquiloPointFields(t *testing.T) {
	data, err := os.ReadFile("testdata/space-age-upstream/oracle-aquilo-fields.seed123456.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Positions [][2]float64
		Climate   map[string][]string
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	e := newFactorioAquiloEvaluator(fastPreviewSettings{seed: 123456, planet: fastPreviewPlanetAquilo}, false)
	for _, field := range []string{"elevation", "aux"} {
		values := fixture.Climate[field]
		if len(values) != len(fixture.Positions) || len(values) == 0 {
			t.Fatalf("invalid %s field length", field)
		}
		worst := 0.0
		for i, p := range fixture.Positions {
			bits, err := strconv.ParseUint(values[i], 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			want := math.Float32frombits(uint32(bits))
			sample := e.sample(p[0], p[1])
			got := sample.elevation
			if field == "aux" {
				got = sample.aux
			}
			worst = math.Max(worst, math.Abs(float64(got-want)))
		}
		t.Logf("%s worst residual %g", field, worst)
		if worst != 0 {
			t.Errorf("%s residual %g", field, worst)
		}
	}
}
