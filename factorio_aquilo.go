// SPDX-License-Identifier: AGPL-3.0-or-later
// Adapted from FactorioMapWebUI; see docs/upstream-rendering.md for provenance.

package main

import (
	"image/color"
	"math"
	"sync"
)

type factorioAquiloWellField struct {
	seed        uint32
	regionSize  int64
	count, skip int
	radius      float32
	mu          sync.Mutex
	regions     map[[2]int64][]factorioSelectedSpot
}

func (f *factorioAquiloWellField) spots(rx, ry int64) []factorioSelectedSpot {
	key := [2]int64{rx, ry}
	f.mu.Lock()
	defer f.mu.Unlock()
	if spots, ok := f.regions[key]; ok {
		return spots
	}
	q := float64(f.radius * f.radius)
	spots := factorioSelectSpots(factorioSpotRegionKey{seed0: f.seed, seed1: 567, regionX: rx, regionY: ry}, factorioSpotSelectionParams{regionSize: f.regionSize, candidateSpotCount: f.count, spacing: 128, skipSpan: 3, skipOffset: f.skip, density: func(float64, float64) float64 { return 1 }, quantity: func(float64, float64) float64 { return q }, favorability: func(float64, float64) float64 { return 1 }})
	if len(f.regions) >= 64 {
		clear(f.regions)
	}
	f.regions[key] = spots
	return spots
}
func (f *factorioAquiloWellField) at(x, y float32) float32 {
	if f.radius <= 0 {
		return -1
	}
	reach := f.radius * 2
	half := float64(f.regionSize / 2)
	index := func(v float32) int64 { return int64(math.Floor((float64(v) + half) / float64(f.regionSize))) }
	best := float32(-1)
	peak := float32(float64(f.radius*f.radius) * 3 / (float64(f.radius) * 3.1416 * float64(f.radius)))
	for ry := index(y - reach); ry <= index(y+reach); ry++ {
		for rx := index(x - reach); rx <= index(x+reach); rx++ {
			for _, p := range f.spots(rx, ry) {
				dx, dy := x-float32(p.x), y-float32(p.y)
				d := noiseSqrt(dx*dx + dy*dy)
				if d <= reach {
					best = max(best, peak-d*(peak/f.radius))
				}
			}
		}
	}
	return best
}

type factorioAquiloEvaluator struct {
	settings                                            fastPreviewSettings
	wobbleX, wobbleY, aux, snow, variant, peaks, random func(float32, float32) float32
	billows                                             [3]func(float32, float32) float32
	large, small                                        factorioVoronoi
	cones                                               [3]factorioNoiseCone
	tiny                                                factorioNoiseCone
	wells                                               [3]*factorioAquiloWellField
}
type factorioAquiloSample struct {
	tile                                      factorioVulcanusTile
	elevation, aux, ice, variant, islandPeaks float32
	wells                                     [3]float32
	iceberg                                   float32
}

var factorioAquiloControls = [3]string{"aquilo_crude_oil", "lithium_brine", "fluorine_vent"}

func newFactorioAquiloEvaluator(settings fastPreviewSettings, grid bool) *factorioAquiloEvaluator {
	seed := settings.seed
	noise := func(salt uint32, p, scale, out float32) func(float32, float32) float32 {
		return makeFactorioMultioctaveNoise32(seed, salt, 3, p, scale, out, grid)
	}
	angle := float32(float64(seed)/4294967296) * 3600
	e := &factorioAquiloEvaluator{
		settings: settings, wobbleX: noise(12243, 0.65, 1.5/100, 0.35), wobbleY: noise(13243, 0.65, 1.5/100, 0.35),
		aux:  makeFactorioMultioctaveNoise32(seed, 14243, 3, 0.7, 1.5/25, 1),
		snow: noise(1, 0.85, 1.0/12, 1), variant: noise(100, 0.85, 1.0/6, 1), peaks: noise(1, 0.85, 1.0/6, 1),
		large: newFactorioVoronoi(seed, 1475573916, 24, 1, voronoiEuclidean), small: newFactorioVoronoi(seed, 1475573916, 10, 1, voronoiEuclidean),
		cones: [3]factorioNoiseCone{newFactorioNoiseCone(angle, 40, 24), newFactorioNoiseCone(angle+120, 80, 18), newFactorioNoiseCone(angle+240, 160, 18)}, tiny: newFactorioNoiseCone(angle+240, 80, 1),
	}
	segmentation := float32(1) / 25
	e.random = makeFactorioAmplitudeNoise32(seed, 1000, 6, segmentation/1.2, -10000, 0.8, grid)
	for i, scale := range []float32{segmentation / 1.5, segmentation / 1.2, segmentation} {
		e.billows[i] = makeFactorioQuickNoise32(seed, uint32(2000+i*1000), 3, scale, 1, 0.5, 0.75, 10000, grid)
	}
	for i, name := range factorioAquiloControls {
		c := fastSpaceAgeControl(settings, name)
		radius := []float32{30, 36, 45}[i] * noiseSqrt(float32(max(0, c.size)))
		e.wells[i] = &factorioAquiloWellField{seed: seed, regionSize: int64(float32(600) + float32(400)/float32(positiveOr(c.frequency, 1))), radius: radius, count: 4 - i, skip: i, regions: make(map[[2]int64][]factorioSelectedSpot)}
	}
	return e
}
func (e *factorioAquiloEvaluator) sample(px, py float64) factorioAquiloSample {
	x, y := float32(px), float32(py)
	sx, sy := x, y
	if len(e.settings.startingPositions) > 0 {
		best := float32(math.MaxFloat32)
		for _, p := range e.settings.startingPositions {
			dx, dy := x-float32(p.x), y-float32(p.y)
			d := dx*dx + dy*dy
			if d < best {
				best, sx, sy = d, dx, dy
			}
		}
	}
	distance := noiseSqrt(sx*sx + sy*sy)
	startingMask := min(1, max(-1, (distance-300)/40))
	maxSpot := float32(math.Inf(-1))
	var wells [3]float32
	for i := range e.cones {
		start, spot := e.cones[i].at(sx, sy, 0, 0), e.wells[i].at(x, y)
		maxSpot = max(maxSpot, start, spot)
		c := fastSpaceAgeControl(e.settings, factorioAquiloControls[i])
		if c.enabled && c.size > 0 && c.richness > 0 {
			wells[i] = max(start*0.02, min(startingMask, spot)*[]float32{0.015, 0.012, 0.008}[i])
			if i == 2 {
				wells[i] = max(wells[i], noiseBool(e.tiny.at(sx, sy, 0, 0) > 0))
			}
		}
	}
	// Aquilo's central island is tied to the origin; resource starting cones
	// follow the configured start positions, matching the game's expressions.
	originDistance := noiseSqrt(x*x + y*y)
	islands := max(float32(1.7)*(0.3+float32(1-originDistance*0.01)), float32(1.5)*(0.5+maxSpot))
	wx, wy := e.wobbleX(x, y), e.wobbleY(x, y)
	aux := float32(0.5) + e.aux(x+(wx*300)/1.5, y+(wy*300)/1.5)
	large, small := e.large.sample(x+wx*2, y+wy*2, false), e.small.sample(x+wx*2, y+wy*2, false)
	random := noiseAbs(e.random(x, y))
	tri := min(noiseAbs(e.billows[0](x, y)), noiseAbs(e.billows[1](x, y)), noiseAbs(e.billows[2](x, y)))
	clumped := float32(-25) + 12*max(islands, random) + 15*tri
	cracks := noiseSqrt(large.facet*max(0, large.id*1.2-0.2) + (0.5*small.facet)*max(0, aux+0.1))
	broken := float32(-20) + 8*max(islands*1.1, min(0, random-0.2)) + 13*cracks
	elevation := noiseLerp(noiseLerp(clumped, broken, 0.4), max(clumped, broken), 0.4)
	ice, variant := e.snow(x, y), e.variant(x, y)
	base := func(threshold float32) float32 { return 100 * (-1 + 2*noiseBool(elevation > threshold)) }
	scores := [9]float32{
		base(1.5) - (ice + 0.5) - noiseAbs(variant-0.5) + elevation/25 + 1,
		base(1) - noiseAbs(ice-0.2) - noiseAbs(variant-0.2) + elevation/25 + 1.2,
		base(0.5) - noiseAbs(ice+0.1) - noiseAbs(variant+0.3) + elevation/25 + 1.2,
		base(0) - noiseAbs(ice+0.4) - noiseAbs(variant+0.6) + elevation/25 + 1,
		base(-1) + ice - noiseAbs(variant) - elevation/10 + 0.5,
		base(-1) + ice - noiseAbs(variant+1) - elevation/25 + 1,
		0.05 * (elevation/5 - aux/3 + 0.75), 0.01 * (aux - 0.5), -0.01 * (aux - 0.5),
	}
	tiles := [9]factorioVulcanusTile{
		{"snow-flat", color.RGBA{190, 194, 197, 255}}, {"snow-crests", color.RGBA{179, 185, 192, 255}},
		{"snow-lumpy", color.RGBA{166, 174, 185, 255}}, {"snow-patchy", color.RGBA{156, 166, 181, 255}},
		{"ice-rough", color.RGBA{100, 135, 177, 255}}, {"ice-smooth", color.RGBA{100, 135, 177, 255}},
		{"brash-ice", color.RGBA{21, 42, 56, 255}}, {"ammoniacal-ocean", color.RGBA{15, 13, 25, 255}}, {"ammoniacal-ocean-2", color.RGBA{17, 15, 30, 255}},
	}
	winner := 0
	for i, score := range scores {
		if score > scores[winner] {
			winner = i
		}
	}
	iceberg := float32(0)
	if winner < 6 {
		iceberg = max(0, min(1, noiseAbs(e.peaks(x, y)), 2*((elevation/5-aux/3)+1.5)))
	}
	return factorioAquiloSample{tile: tiles[winner], elevation: elevation, aux: aux, ice: ice, variant: variant, islandPeaks: islands, wells: wells, iceberg: iceberg}
}
