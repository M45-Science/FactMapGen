// SPDX-License-Identifier: AGPL-3.0-or-later
package main

import (
	"math"
	"sync"
)

type glebaFeatureFields struct {
	terrain                                              *factorioGlebaEvaluator
	pointTerrain                                         *factorioGlebaEvaluator
	scrubA, scrubB, knockout, decalA, decalB, cliffNoise func(float32, float32) float32
	stoneStart                                           factorioNoiseCone
	stoneControl                                         fastControl
	stoneRadius                                          float32
	stoneRegionSize                                      int64
	stoneSpacing                                         float64
	stoneMu                                              sync.Mutex
	stoneRegions                                         map[[2]int64][]factorioSelectedSpot
}

type glebaFeatureSample struct {
	plants                                      [12]float32
	stromatolites                               [2]float32
	stoneProbability, stoneRichness, cliffiness float32
}

func newGlebaFeatureFields(e *factorioGlebaEvaluator, grid bool) *glebaFeatureFields {
	n := func(salt uint32, scale, out float32) func(float32, float32) float32 {
		return makeFactorioMultioctaveNoise32(e.settings.seed, salt, 2, .7, scale, out, grid)
	}
	c := fastSpaceAgeControl(e.settings, "gleba_stone")
	size := float32(math.Sqrt(max(0, c.size)))
	spacing := float32(80 / math.Sqrt(positiveOr(c.frequency, 1)))
	f := &glebaFeatureFields{
		terrain: e, pointTerrain: e, stoneControl: c, stoneRadius: 6 * size, stoneSpacing: float64(spacing), stoneRegionSize: int64(spacing * 5), stoneRegions: make(map[[2]int64][]factorioSelectedSpot),
		scrubA: n(7500, 1.0/8, .5), scrubB: n(8500, 1.0/8, 1), knockout: n(1300000, 1.0/3, 1), decalA: n(1500, 1.0/16, .5), decalB: n(2500, 1.0/20, 1),
		cliffNoise: makeFactorioQuickNoise32(e.settings.seed, 123, 2, 1.0/32, 1, 1.0/3, 1, 0, grid),
		stoneStart: newFactorioNoiseCone(e.angle+85*e.direction, 80*.7, 7*size),
	}
	if grid {
		f.pointTerrain = newFactorioGlebaEvaluator(e.settings, false)
	}
	return f
}

func (f *glebaFeatureFields) stoneSpots(rx, ry int64) []factorioSelectedSpot {
	f.stoneMu.Lock()
	defer f.stoneMu.Unlock()
	key := [2]int64{rx, ry}
	if s, ok := f.stoneRegions[key]; ok {
		return s
	}
	highland := func(x, y float64) float64 {
		return float64(glebaSelect(f.pointTerrain.sample(x, y).elevation, 120, 1000, .5, 0, 1))
	}
	s := factorioSelectSpots(factorioSpotRegionKey{seed0: f.terrain.settings.seed, seed1: 1000, regionX: rx, regionY: ry}, factorioSpotSelectionParams{
		regionSize: f.stoneRegionSize, candidateSpotCount: 22, spacing: f.stoneSpacing, skipSpan: 1, skipOffset: 1,
		density: highland, favorability: highland, quantity: func(float64, float64) float64 { return float64(f.stoneRadius * f.stoneRadius) },
	})
	if len(f.stoneRegions) >= 128 {
		clear(f.stoneRegions)
	}
	f.stoneRegions[key] = s
	return s
}

func (f *glebaFeatureFields) stoneSpotAt(x, y float32) float32 {
	r := f.stoneRadius
	if r <= 0 || f.stoneRegionSize <= 0 {
		return -1
	}
	best := float32(-1)
	peak := float32(float64(r*r) * 3 / (float64(r) * 3.1416 * float64(r)))
	index := func(v float32) int64 {
		return int64(math.Floor((float64(v) + float64(f.stoneRegionSize)/2) / float64(f.stoneRegionSize)))
	}
	for ry := index(y - r*2); ry <= index(y+r*2); ry++ {
		for rx := index(x - r*2); rx <= index(x+r*2); rx++ {
			for _, p := range f.stoneSpots(rx, ry) {
				dx, dy := x-float32(p.x), y-float32(p.y)
				if dx*dx+dy*dy > (r*2)*(r*2) {
					continue
				}
				best = max(best, peak-noiseSqrt(dx*dx+dy*dy)*(peak/r))
			}
		}
	}
	return best
}

func (f *glebaFeatureFields) sample(s factorioGlebaSample, px, py float64) glebaFeatureSample {
	x, y := float32(px), float32(py)
	e := f.terrain
	size := float32(fastSpaceAgeControl(e.settings, "gleba_plants").size)
	decal := .6*f.decalA(x, y) + .3 - noiseAbs(f.decalB(x, y))
	scrub := .6*f.scrubA(x, y) + .3 - noiseAbs(f.scrubB(x, y))
	knockout := glebaClamp(f.knockout(x, y), 0, 1)
	out := glebaFeatureSample{plants: glebaPlantProbabilities(s, size, decal), stromatolites: [2]float32{
		glebaSelect(glebaSelect(s.aux, .6, 2, .1, -10, 1)-1-scrub-knockout, 1.3, 2, .2, 0, 1),
		glebaSelect(glebaSelect(s.aux, -1, .4, .1, -10, 1)-1-scrub-knockout, 1.3, 2, .2, 0, 1),
	}}
	richness := float32(e.settings.cliffRichness * e.settings.cliffs.size)
	if richness > 0 {
		out.cliffiness = max(.95*(glebaClamp(float32(.5*math.Log2(float64(richness)))+f.cliffNoise(x, y), 0, 1)+.5), .99*noiseBool(s.rockpools > 0))
	}
	if f.stoneControl.size <= 0 || f.stoneControl.frequency <= 0 {
		return out
	}
	sx, sy := x, y
	best := float32(math.MaxFloat32)
	for _, p := range e.settings.startingPositions {
		dx, dy := x-float32(p.x), y-float32(p.y)
		d := dx*dx + dy*dy
		if d < best {
			best, sx, sy = d, dx, dy
		}
	}
	start := f.stoneStart.at(sx, sy, e.wobbleX(x, y)*8, e.wobbleX(x, y)*8)
	spot := f.stoneSpotAt(x+e.smallX(x, y)*.5*f.stoneRadius, y+e.smallY(x, y)*.5*f.stoneRadius)
	land := max(glebaSelect(s.elevation, 120, 1000, .5, 0, 1), glebaSelect(s.elevation, 20, 120, .5, 0, 1)*glebaSelect(s.aux, .45, .55, .0025, 0, 1))
	out.stoneRichness = 4000 * max(start, spot*land) * float32(f.stoneControl.richness) / float32(f.stoneControl.size)
	out.stoneProbability = noiseBool(out.stoneRichness > 1)
	return out
}
