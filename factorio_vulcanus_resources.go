// SPDX-License-Identifier: AGPL-3.0-or-later
// Adapted from FactorioMapWebUI; see docs/upstream-rendering.md for provenance.

package main

import (
	"math"
	"sync"
)

var factorioVulcanusResourceNames = [4]string{"tungsten_ore", "vulcanus_coal", "calcite", "sulfuric_acid_geyser"}

type factorioVulcanusResourceField struct {
	evaluator   *factorioVulcanusEvaluator
	index       int
	seed        uint32
	regionSize  int64
	count, skip int
	size        float64
	mu          sync.Mutex
	regions     map[[2]int64][]factorioSelectedSpot
}
type factorioVulcanusResources struct {
	fields [4]*factorioVulcanusResourceField
	cones  [5]factorioNoiseCone
	patch  func(float64, float64) float64
}

func (e *factorioVulcanusEvaluator) initResources() {
	r := &factorioVulcanusResources{}
	sizes := [4]float64{}
	for i, name := range factorioVulcanusResourceNames {
		c := fastSpaceAgeControl(e.settings, name)
		sizes[i] = float64(noiseSliderRescale(float32(c.size), 2))
		base := []float64{500, 400, 400, 450}[i]
		r.fields[i] = &factorioVulcanusResourceField{evaluator: e, index: i, seed: []uint32{789, 782349, 749, 759}[i], regionSize: int64(base + base/positiveOr(c.frequency, 1)), count: []int{15, 12, 12, 9}[i], skip: []int{2, 1, 1, 0}[i], size: sizes[i], regions: make(map[[2]int64][]factorioSelectedSpot)}
	}
	radius := float64(factorioVulcanusStartingAreaRadius)
	angle := func(base, offset float64) float32 {
		return float32(base + float64(float32(offset*e.startingDirection)))
	}
	r.cones = [5]factorioNoiseCone{
		newFactorioNoiseCone(angle(e.basaltsAngle, -10), float32(450*radius), 20),
		newFactorioNoiseCone(angle(e.ashlandsAngle, 15), float32(180*radius), float32(30*sizes[1])),
		newFactorioNoiseCone(angle(e.mountainsAngle, -20), float32(350*radius), float32(float64(float32(35.0/1.5))*sizes[2])),
		newFactorioNoiseCone(angle(e.mountainsAngle, 10), float32(590*radius), 30),
		newFactorioNoiseCone(angle(e.mountainsAngle, 30), float32(200*radius), float32(25*sizes[3])),
	}
	r.patch = e.multioctaveNoise(factorioMultioctaveParams{seed0: e.seed, seed1: 21000, octaves: 2, persistence: 0.7, inputScale: 1.0 / 3, outputScale: 1})
	e.resources = r
}
func vulcanusResourceFavor(index int, full [3]float64, startingArea, volcano float64) float64 {
	outside := 0.0
	if startingArea < 0.01 {
		outside = 1
	}
	which := []int{2, 1, 0, 0}[index]
	buffer := 0.3
	if index == 2 {
		buffer = 0.4
	}
	favor := clampFloat((full[which]*outside-buffer)*2, 0, 1)
	if index == 2 && volcano > 0.78 {
		return 0
	}
	return favor
}
func (e *factorioVulcanusEvaluator) resourceFavor(index int, x, y float64) float64 {
	p := e.preVolcanoAt(x, y)
	v := e.mountainVolcanoSpotsAt(x, y, p)
	raw := 0.5*p.mountainsRawPreVolcano + max(2*v, 10*clampFloat((v-0.33)*3, 0, 1))
	full := [3]float64{raw - max(p.ashlandsRaw, p.basaltsRaw), p.ashlandsRaw - max(raw, p.basaltsRaw), p.basaltsRaw - max(raw, p.ashlandsRaw)}
	return vulcanusResourceFavor(index, full, p.spawn.startingArea, v)
}
func (f *factorioVulcanusResourceField) radiusAt(x, y float64) float64 {
	d, _, _ := f.evaluator.relativeToNearestStart(x, y)
	return f.size * min(1.2, max(1, d/4000)) * 25
}
func (f *factorioVulcanusResourceField) spots(rx, ry int64) []factorioSelectedSpot {
	key := [2]int64{rx, ry}
	f.mu.Lock()
	defer f.mu.Unlock()
	if spots, ok := f.regions[key]; ok {
		return spots
	}
	spots := factorioSelectSpots(factorioSpotRegionKey{seed0: f.evaluator.seed, seed1: f.seed, regionX: rx, regionY: ry}, factorioSpotSelectionParams{
		regionSize: f.regionSize, candidateSpotCount: f.count, spacing: 128, skipSpan: 3, skipOffset: f.skip,
		density:  func(x, y float64) float64 { return f.evaluator.resourceFavor(f.index, x, y) * 4 },
		quantity: func(x, y float64) float64 { r := f.radiusAt(x, y); return r * r },
		favorability: func(x, y float64) float64 {
			if f.evaluator.resourceFavor(f.index, x, y) > 0.9 {
				return 1
			}
			return 0
		},
	})
	if len(f.regions) >= 64 {
		clear(f.regions)
	}
	f.regions[key] = spots
	return spots
}
func (f *factorioVulcanusResourceField) at(x, y float64) float64 {
	if f.size <= 0 {
		return -1
	}
	half := float64(f.regionSize / 2)
	idx := func(v float64) int64 { return int64(math.Floor((v + half) / float64(f.regionSize))) }
	best := float32(-1)
	for ry := idx(y - 128); ry <= idx(y+128); ry++ {
		for rx := idx(x - 128); rx <= idx(x+128); rx++ {
			for _, p := range f.spots(rx, ry) {
				dx, dy := x-p.x, y-p.y
				d2 := dx*dx + dy*dy
				if d2 > 128*128 {
					continue
				}
				radius := float32(f.radiusAt(p.x, p.y))
				if radius <= 0 {
					continue
				}
				peak := float32(3*p.quantity) / float32(float32(math.Pi*float64(radius))*radius)
				cone := peak - float32(float32(math.Sqrt(d2))*float32(peak/radius))
				best = max(best, cone)
			}
		}
	}
	return float64(best)
}
func (e *factorioVulcanusEvaluator) resourceRegions(x, y float64, p factorioVulcanusPreVolcanoSample, full [3]float64, volcano, hairline float64) (regions [4]float64, metal float64) {
	r := e.resources
	wx, wy := p.wobbles.x+0.25*p.wobbles.largeX, p.wobbles.y+0.25*p.wobbles.largeY
	startingCircle := 1 + factorioVulcanusStartingAreaRadius*(300-p.spawn.distance)/50
	for i, f := range r.fields {
		favor := vulcanusResourceFavor(i, full, p.spawn.startingArea, volcano)
		placed := 2*favor - 1
		if i == 0 {
			placed = clampFloat(-1+4*favor, -1, 1)
		}
		if placed > -1 {
			spot := f.at(x+wx, y+wy)
			if i == 0 {
				spot -= hairline / 30000
			}
			placed = min(placed, spot)
		}
		weight := 0.5
		if i == 3 {
			weight = 0.75
		}
		start := float64(r.cones[i].at(float32(x), float32(y), float32(weight*wx), float32(weight*wy)))
		if i == 3 {
			start = max(start, float64(r.cones[4].at(float32(x), float32(y), float32(weight*wx), float32(weight*wy))))
		}
		regions[i] = max(start, min(1-startingCircle, placed))
	}
	regions[3] = (1+regions[3])*(0.5+0.5*0.8*math.Abs(r.patch(x, y))) - 1
	if fastSpaceAgeControl(e.settings, "tungsten_ore").size > 0 {
		metal = max(0, 1000*regions[0])
	}
	return
}
