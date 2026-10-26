// SPDX-License-Identifier: AGPL-3.0-or-later
// See docs/gleba-rendering.md for game-data and upstream primitive provenance.

package main

import (
	"math"
	"sync"
)

// Gleba's terrain expression graph follows Factorio 2.1.23's
// space-age/prototypes/planet/planet-gleba-map-gen.lua. All dynamic arithmetic
// uses the noise VM's single precision. Constant trig is folded by libm;
// dynamic trig uses the VM polynomial instead (see docs/gleba-rendering.md).
func glebaClamp(v, lo, hi float32) float32 { return min(hi, max(lo, v)) }
func glebaSelect(v, from, to, slope, lo, hi float32) float32 {
	return glebaClamp(min(v-(from-slope), to+slope-v)/slope, lo, hi)
}
func glebaTerrace(v, offset, width, strength float32) float32 {
	t := (v - offset) / width
	f := float32(math.Floor(float64(t)))
	frac, up := t-f, float32(0)
	if strength < frac {
		up = (frac - strength) / (1 - strength)
	}
	return (up+f)*width + offset
}
func glebaVMTrig(v float32, cosine bool) float32 {
	t := float64(v) * 0.15915494309189535
	if !cosine {
		t -= 0.25
	}
	u := 0.25 - math.Abs(t-math.RoundToEven(t))
	z := u * u
	z2 := z * z
	a := float64(z*-41.34167506665737) + 6.283185269630412
	b := float64(z*-76.56887678023256) + 81.60201529595571
	c := float64(z2*b) + a
	d := float64(float64(z2*z2)*39.65735524898863) + c
	return float32(d * u)
}
func glebaRotate(x, y, angle float32) (float32, float32) {
	r := (angle / 180) * float32(math.Pi)
	s, c := glebaVMTrig(r, false), glebaVMTrig(r, true)
	return x*c + y*s, y*c - x*s
}

// Fertile spots are constant-density cones, with a bounded, thread-safe cache.
type glebaFertileSpots struct {
	seed    uint32
	mu      sync.Mutex
	regions map[[2]int64][]factorioSelectedSpot
}

func (f *glebaFertileSpots) region(rx, ry int64) []factorioSelectedSpot {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int64{rx, ry}
	if s, ok := f.regions[key]; ok {
		return s
	}
	s := factorioSelectSpots(factorioSpotRegionKey{seed0: f.seed, seed1: 1, regionX: rx, regionY: ry}, factorioSpotSelectionParams{
		regionSize: 1024, candidateSpotCount: 80, spacing: 128, skipSpan: 1,
		density: func(float64, float64) float64 { return 80 }, quantity: func(float64, float64) float64 { return 1000 }, favorability: func(float64, float64) float64 { return 60 },
	})
	if len(f.regions) >= 128 {
		clear(f.regions)
	}
	f.regions[key] = s
	return s
}
func (f *glebaFertileSpots) at(x, y float32) float32 {
	best := float32(-0.5)
	const peak = float32(3000.0 / (32 * 32 * 3.1416))
	index := func(v float32) int64 { return int64(math.Floor((float64(v) + 512) / 1024)) }
	for ry := index(y - 128); ry <= index(y+128); ry++ {
		for rx := index(x - 128); rx <= index(x+128); rx++ {
			for _, p := range f.region(rx, ry) {
				dx, dy := x-float32(p.x), y-float32(p.y)
				best = max(best, peak-noiseSqrt(dx*dx+dy*dy)*(peak/32))
			}
		}
	}
	return best
}

type factorioGlebaEvaluator struct {
	settings                                                 fastPreviewSettings
	angle, direction, waterSize                              float32
	wobbleX, wobbleY, smallX, smallY                         func(float32, float32) float32
	auxPattern, auxMutator, coverA, coverB, temperature      func(float32, float32) float32
	tri                                                      [3]func(float32, float32) float32
	highFrequency, peaks, ridgesSmall, rockpools, rockMask   func(float32, float32) float32
	mudBasins, mudChannels, fertileX, fertileY, fertileSolid func(float32, float32) float32
	plants                                                   [4]func(float32, float32) float32
	cones                                                    [15]factorioNoiseCone
	fertile                                                  glebaFertileSpots
}
type factorioGlebaSample struct {
	tile                                                                                             factorioVulcanusTile
	elevation, common, aux, moisture, temperature, rockpools, fertile, fertileSolid, plants, plantsB float32
	scores                                                                                           [41]float32
}

func newFactorioGlebaEvaluator(settings fastPreviewSettings, grid bool) *factorioGlebaEvaluator {
	n := func(salt uint32, octaves int, p, scale, out float32, regular bool) func(float32, float32) float32 {
		return makeFactorioMultioctaveNoise32(settings.seed, salt, octaves, p, scale, out, grid && regular)
	}
	water, plants := fastSpaceAgeControl(settings, "gleba_water"), fastSpaceAgeControl(settings, "gleba_plants")
	wf, pf := float32(water.frequency), float32(plants.frequency)
	e := &factorioGlebaEvaluator{settings: settings, angle: float32(float64(settings.seed)/4294967296) * 3600, direction: -1 + 2*float32(settings.seed&1), waterSize: float32(water.size),
		wobbleX: n(2000, 3, .7, 1.0/20, 1, true), wobbleY: n(3000, 3, .7, 1.0/20, 1, true), smallX: n(2000, 2, .7, 1.0/6, 1, true), smallY: n(3000, 2, .7, 1.0/6, 1, true),
		auxPattern: n(7000, 7, .75, 1.0/70, 1, false), auxMutator: n(8000, 4, .75, 1.0/30, 1, true), coverA: n(3500, 2, .7, 1.0/10, .5, true), coverB: n(4500, 2, .7, 1.0/16, 1, true), temperature: n(18000, 4, .65, 1.0/4, 1, false),
		highFrequency: n(1, 3, .7, 1.0/18, 1, true), peaks: n(1000000, 3, .7, float32(1.0/60)*noiseSliderRescale(noiseSliderRescale(wf, 2), 3), 1, false), ridgesSmall: n(1000000, 3, .7, 1.0/60, 1, true),
		rockpools: n(1000000, 4, .75, 1.0/45, 1, true), rockMask: n(1000000, 3, .7, 1.0/75, 1, true), mudBasins: n(1000000, 3, .7, 1.0/10, 1, true), mudChannels: n(1000000, 3, .5, 1.0/9, 1, true),
		fertileX: n(3000000, 2, .5, 1.0/20, 1, true), fertileY: n(4000000, 2, .5, 1.0/20, 1, true), fertileSolid: n(2000000, 2, .7, 1.0/16, 1, true),
		fertile: glebaFertileSpots{seed: settings.seed, regions: make(map[[2]int64][]factorioSelectedSpot)},
	}
	for i := range e.tri {
		e.tri[i] = n(uint32(i+1)*10000, 3, .65, float32(1.0/300)*wf, 1, false)
	}
	for i, salt := range []uint32{700000, 200000, 750000, 250000} {
		scale := float32(1.0 / 20)
		if i%2 == 1 {
			scale = 1.0 / 6
		}
		e.plants[i] = n(salt, 3, .8, scale*pf, 1, true)
	}
	// Highlands, midlands, lakes, lowlands, fertile patches and enemy clearings.
	for i, c := range [][3]float32{{95, 100, 130}, {40, 130, 65}, {265, 10, 80}, {180, 120, 130}, {340, 100, 80}, {90, 500, 350}, {270, 500, 450}, {265, 50, 50}, {235, 300, 140}, {65, 290, 100}, {235, 310, 40}, {65, 310, 40}, {40, 210, 30}, {210, 410, 30}, {30, 410, 30}} {
		e.cones[i] = newFactorioNoiseCone(e.angle+c[0]*e.direction, c[1]*.7, c[2]*.7)
	}
	return e
}
func (e *factorioGlebaEvaluator) sample(px, py float64) factorioGlebaSample {
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
	wx, wy := e.wobbleX(x, y), e.wobbleY(x, y)
	var cones [15]float32
	for i, c := range e.cones {
		d := wx * 15
		if i >= 12 {
			d = wx * 20
		}
		cones[i] = c.at(sx, sy, d, d)
	}
	bridge := func(angle float32) float32 {
		rx, ry := glebaRotate(sx, sy, angle)
		rx += wx * 10
		ry += wy * 10
		return min(1-noiseAbs(rx/.7-e.direction*glebaVMTrig(ry/64/.7, false)*64*.7)/110, 2-distance/.7/700)
	}
	spiral := e.direction * (distance/.7/16 - 15)
	bridges := max(bridge(e.angle-spiral), bridge(e.angle+spiral))
	a, b, c := 1+e.tri[0](x+wx*6, y+wy*6), 1+e.tri[1](x+wx*6, y+wy*6), 1+e.tri[2](x+wx*6, y+wy*6)
	tri := float32(.5) * (noiseBool(max(b, c) < a)*(a-max(b, c)) + noiseBool(max(a, c) < b)*(b-max(a, c)) + noiseBool(max(a, b) < c)*(c-max(a, b)))
	ridgesSmall := noiseAbs(e.ridgesSmall(x, y))
	ridges := max(-tri*(.6+e.waterSize/3), bridges-.8)*2.4 + .2*ridgesSmall
	peaks := e.peaks(x+wx*10, y+wy*10) + ridges
	terrain := max(glebaTerrace(min(80, 110+ridges*500), 40, 20, .2), glebaTerrace(min(150, -25+peaks*250), 40, 20, .2), 25+22*ridges)
	start := max(-30*glebaClamp(max(cones[5], cones[6], cones[7])*2, 0, 1), 5*min(max(cones[8], cones[9])*2, 1), 130*min(max(cones[0], cones[1])*2, 1), 80*min(max(cones[2], cones[3], cones[4])*2, 1), 70*min((bridges-.5)*3, 1)) + 6*ridgesSmall
	fertileRaw := e.fertile.at(x+e.fertileX(x, y)*15, y+e.fertileY(x, y)*15)
	rockMask := noiseAbs(e.rockMask(x, y))
	rockpools := glebaClamp(1-2*noiseAbs(e.rockpools(x, y)), 0, 1) * glebaSelect(rockMask+min(0, distance/150-2)-max(0, fertileRaw), 1.4, 3, .4, 0, 1) * (1 + (rockMask-1)/10)
	rockElevation := noiseLerp(noiseLerp(20, 80, noiseBool(rockpools < .3)), 250, noiseBool(rockpools < .01))
	common := noiseLerp(min(rockElevation, noiseLerp(terrain, start, glebaClamp(1.7-distance/.7/500, 0, 1)))+3*(.5-noiseAbs(e.highFrequency(x, y))), -1, glebaClamp(max(cones[12], cones[13], cones[14])*2, 0, 1))
	cover := .6*e.coverA(x, y) + .3 - noiseAbs(e.coverB(x, y))
	mutator := e.auxMutator(x, y) + .1*cover
	multiplier := 1 + 4*glebaSelect(mutator, 1.4, 3, .5, 0, 1) - .9*glebaSelect(mutator, -3, -1.4, .5, 0, 1)
	pattern := e.auxPattern(x+wx*15+e.smallX(x, y)*5, y+wy*15+e.smallY(x, y)*5)
	_, rotatedY := glebaRotate(sx, sy, e.angle-e.direction*(distance/20/.7-15))
	startingAux := glebaClamp(-(rotatedY+wx*10)/.7/200, -2, 2)
	aux := glebaClamp(.5+.5*noiseLerp(glebaClamp(pattern*multiplier, -3, 3), startingAux, glebaClamp(1-distance/.7/900, 0, .9))-min(.2, rockpools), 0, 1)
	contrast := glebaClamp(10*(aux-.5)+.5, 0, 1)
	peak := max(0, 1-noiseAbs(contrast-.5)/.5)
	mud := noiseLerp(-8+16*(1-noiseAbs(e.mudBasins(x, y))), -12+16*noiseAbs(e.mudChannels(x, y)), contrast)*(1+.3*peak) + .1*peak
	elevation := noiseLerp(common, glebaClamp(common*.5+mud, -1.5, 19.9)+.15*mud, glebaSelect(common, -5, 20, 1, 0, 1))
	pitrock := glebaSelect(rockpools, .05, .6, .05, 0, 1) * noiseBool(elevation > 20)
	moisture := noiseLerp(glebaClamp(1-min(.25+common/80, .5+(common-20)/400), 0, 1), .35, min(1, pitrock*5))
	temperature := glebaClamp(.8*e.temperature(x+wx*6, y+wy*6), -1, 1)
	fertile := max(min(1, max(cones[10], cones[11])*4), min(distance/.7/150-2.2, fertileRaw)-max(0, -(elevation+2)/5)-max(0, (elevation-10)/5))
	s := factorioGlebaSample{elevation: elevation, common: common, aux: aux, moisture: moisture, temperature: 10 + temperature*10, rockpools: rockpools, fertile: fertile, fertileSolid: 2 * noiseAbs(e.fertileSolid(x, y)) * fertile, plants: noiseAbs(e.plants[0](x, y) * e.plants[1](x, y)), plantsB: noiseAbs(e.plants[2](x, y) * e.plants[3](x, y))}
	s.scores = s.tileScores(temperature, pitrock)
	winner := 0
	for i, v := range s.scores {
		if v > s.scores[winner] || (v == s.scores[winner] && factorioGlebaTiles[i].name < factorioGlebaTiles[winner].name) {
			winner = i
		}
	}
	s.tile = factorioGlebaTiles[winner]
	return s
}
