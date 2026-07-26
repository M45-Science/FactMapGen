package main

import (
	"math"
	"testing"
)

var benchmarkFactorioBasisNoise float64

func factorioBasisNoiseReference(x, y float64, tables *factorioBasisTables) float64 {
	ix := int64(math.Floor(x))
	iy := int64(math.Floor(y))
	fx := x - float64(ix)
	fy := y - float64(iy)
	value := 0.0
	for cornerY := int64(0); cornerY <= 1; cornerY++ {
		for cornerX := int64(0); cornerX <= 1; cornerX++ {
			dx := fx - float64(cornerX)
			dy := fy - float64(cornerY)
			distanceSquared := dx*dx + dy*dy
			if distanceSquared >= 1 {
				continue
			}
			ax := tables.a[uint8(ix+cornerX)]
			by := tables.b[uint8(iy+cornerY)]
			gradient := tables.sigma[ax^by]
			falloff := 1 - distanceSquared
			falloff *= falloff * falloff
			value += falloff * 4.2 * (dx*factorioGradientX[gradient] + dy*factorioGradientY[gradient])
		}
	}
	return value
}

func TestFactorioBasisNoiseMatchesLoopExactly(t *testing.T) {
	coordinates := [][2]float64{
		{0, 0},
		{math.Copysign(0, -1), math.Copysign(0, -1)},
		{math.Nextafter(0, math.Inf(-1)), math.Nextafter(0, math.Inf(1))},
		{math.Nextafter(1, math.Inf(-1)), math.Nextafter(-1, math.Inf(1))},
		{1, -1},
		{-0.5, 0.5},
		{255.99999999999997, -256.00000000000006},
		{256, -256},
		{-13763.35546875 * 0.125, -6886.4921875 * 0.125},
		{225.984375 * 0.125, 71.328125 * 0.125},
		{float64(int64(1)<<40) + 0.25, -float64(int64(1)<<40) + 0.75},
	}

	state := uint64(0x6a09e667f3bcc909)
	next := func() uint64 {
		state ^= state << 13
		state ^= state >> 7
		state ^= state << 17
		return state
	}
	for range 4096 {
		x := float64(int64(next()%2_000_001)-1_000_000) +
			float64(next()&0xffff)/65536
		y := float64(int64(next()%2_000_001)-1_000_000) +
			float64(next()&0xffff)/65536
		coordinates = append(coordinates, [2]float64{x, y})
	}

	for _, seeds := range [][2]uint32{
		{0, 0},
		{1, 1},
		{341, 256},
		{342, 257},
		{123456, 0},
		{123456, 5000},
		{math.MaxUint32, math.MaxUint32},
	} {
		tables := factorioBasisTablesFromSeed(seeds[0], seeds[1])
		for _, coordinate := range coordinates {
			x, y := coordinate[0], coordinate[1]
			want := factorioBasisNoiseReference(x, y, &tables)
			got := factorioBasisNoise(x, y, &tables)
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf(
					"basis seed=(%d,%d) at (%g,%g) bits = %016x, want %016x (%.17g vs %.17g)",
					seeds[0], seeds[1], x, y,
					math.Float64bits(got), math.Float64bits(want), got, want,
				)
			}
		}
	}
}

func BenchmarkFactorioBasisNoise(b *testing.B) {
	tables := factorioBasisTablesFromSeed(123456, 5000)
	coordinates := make([][2]float64, 256)
	for index := range coordinates {
		coordinates[index] = [2]float64{
			-2048 + float64(index)*15.875,
			1024 - float64(index)*7.9375,
		}
	}
	for _, benchmark := range []struct {
		name string
		run  func(float64, float64, *factorioBasisTables) float64
	}{
		{name: "specialized", run: factorioBasisNoise},
		{name: "loop-reference", run: factorioBasisNoiseReference},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			sum := 0.0
			for index := 0; index < b.N; index++ {
				coordinate := coordinates[index&(len(coordinates)-1)]
				sum += benchmark.run(coordinate[0], coordinate[1], &tables)
			}
			benchmarkFactorioBasisNoise = sum
		})
	}
}

func TestFactorioBasisNoiseMatchesOracle(t *testing.T) {
	tests := []struct {
		seed0 uint32
		seed1 uint32
		x     float64
		y     float64
		want  float64
	}{
		{123456, 0, -13763.35546875 * 0.125, -6886.4921875 * 0.125, 0.21834702789783478},
		{123456, 256, 225.984375 * 0.125, 71.328125 * 0.125, -0.5777009129524231},
		{123456, 1, -14847.2109375 * 0.125, 7967.62109375 * 0.125, -0.07329501956701279},
		{1000, 5000, 1016.06640625 * 0.125, -13199.62109375 * 0.125, 0.03892615810036659},
	}
	for _, test := range tests {
		tables := factorioBasisTablesFromSeed(test.seed0, test.seed1)
		got := factorioBasisNoise(test.x, test.y, &tables)
		if delta := math.Abs(got - test.want); delta >= 1e-5 {
			t.Errorf("basis seed=(%d,%d) at (%g,%g) = %.12g, want %.12g (delta %.3g)",
				test.seed0, test.seed1, test.x, test.y, got, test.want, delta)
		}
	}
}

func TestFactorioBasisNoiseSeedClampAndDeadBit(t *testing.T) {
	fingerprint := func(seed uint32) float64 {
		tables := factorioBasisTablesFromSeed(seed, 0)
		return factorioBasisNoise(17.5, 3.125, &tables)
	}
	wantClamp := fingerprint(341)
	for _, seed := range []uint32{0, 1, 128, 256, 320, 340, 341} {
		if got := fingerprint(seed); got != wantClamp {
			t.Errorf("seed %d escaped the Factorio minimum seed-word clamp", seed)
		}
	}
	if fingerprint(342) == wantClamp {
		t.Fatal("seed 342 should be the first seed outside the minimum seed-word clamp")
	}
	for _, seed := range []uint32{1000, 2468, 200000} {
		if fingerprint(seed) != fingerprint(seed+1) {
			t.Errorf("Factorio basis-noise dead low bit differs for seeds %d and %d", seed, seed+1)
		}
	}
}

func TestFactorioStartingLakePositionMatchesOracle(t *testing.T) {
	got := factorioStartingLakePositions(123456, []factorioPoint{{}})
	if len(got) != 1 || got[0] != (factorioPoint{x: 45, y: -59}) {
		t.Fatalf("starting lake = %#v, want (45,-59)", got)
	}
}

func TestFactorioNauvisFieldsMatchOracle(t *testing.T) {
	settings := fastPreviewSettings{
		seed:                          123456,
		mapType:                       "nauvis",
		water:                         fastControl{frequency: 1, size: 1, richness: 1, enabled: true},
		moistureFrequency:             1,
		auxFrequency:                  1,
		temperatureFreq:               1,
		startingAreaMoistureSize:      1,
		startingAreaMoistureFrequency: 1,
		startingPositions:             []factorioPoint{{}},
	}
	evaluator := newFactorioNauvisEvaluator(settings)
	tests := []struct {
		x         float64
		y         float64
		elevation float64
		aux       float64
		moisture  float64
		tolerance float64
	}{
		{-10.5, -12.75, 19.576610565185547, 0.7239349484443665, 0.5215939283370972, 1e-4},
		{0.5, 0.25, 15.650215148925781, 0.7178451418876648, 0.44999998807907104, 1e-4},
		{11.5, 13.25, 13.212127685546875, 0.7685894966125488, 0.43650296330451965, 1e-4},
		{2200.5, 0.25, 9.823979377746582, 0.16310417652130127, 0.12952005863189697, 8e-3},
		{-1555.1349186104048, -1555.3849186104044, -17.09878158569336, 0.6443109512329102, 0.6691744327545166, 8e-3},
		{12345.75, 6789.125, 31.841218948364258, 0.3556360900402069, 0.6360214352607727, 8e-3},
	}
	for _, test := range tests {
		got := evaluator.sample(test.x, test.y)
		for name, values := range map[string][2]float64{
			"elevation": {got.elevation, test.elevation},
			"aux":       {got.aux, test.aux},
			"moisture":  {got.moisture, test.moisture},
		} {
			if delta := math.Abs(values[0] - values[1]); delta >= test.tolerance {
				t.Errorf("%s at (%g,%g) = %.12g, want %.12g (delta %.3g)",
					name, test.x, test.y, values[0], values[1], delta)
			}
		}
	}
}
