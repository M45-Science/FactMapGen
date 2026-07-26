package main

import (
	"image/color"
	"math"
)

const (
	factorioRockNoiseSeed          = 137
	factorioRockFootprintThreshold = 0.02
)

var factorioRockMapColor = color.RGBA{R: 129, G: 105, B: 78, A: 255}

type factorioRockEvaluator struct {
	control  fastControl
	starts   []factorioPoint
	nauvis   *factorioNauvisEvaluator
	noise    func(float64, float64) float64
	sizeTerm float64
}

func newFactorioRockEvaluator(settings fastPreviewSettings, nauvis *factorioNauvisEvaluator) *factorioRockEvaluator {
	starts := nauvis.starts
	if len(starts) == 0 {
		starts = []factorioPoint{{}}
	}
	return &factorioRockEvaluator{
		control: settings.rocks,
		starts:  starts,
		nauvis:  nauvis,
		noise: makeFactorioMultioctaveNoise(factorioMultioctaveParams{
			seed0: settings.seed, seed1: factorioRockNoiseSeed,
			octaves: 4, persistence: 0.9,
			inputScale: 0.15 * settings.rocks.frequency, outputScale: 1,
		}),
		sizeTerm: 0.25 + 0.75*(factorioSliderRescale(settings.rocks.size, 1.5)-1),
	}
}

func (e *factorioRockEvaluator) rockDensityAt(x, y float64) float64 {
	distance := factorioDistanceFromNearestPoint(x, y, e.starts, math.Inf(1))
	return e.noise(x, y) + e.sizeTerm - math.Max(0, 1.1-distance/32)
}

func (e *factorioRockEvaluator) densityAt(x, y float64) float64 {
	if !e.control.enabled || e.control.size <= 0 || e.control.frequency <= 0 {
		return 0
	}
	return e.densityAtSample(x, y, e.nauvis.sample(x, y))
}

func (e *factorioRockEvaluator) densityAtSample(x, y float64, climate factorioNauvisSample) float64 {
	if !e.control.enabled || e.control.size <= 0 || e.control.frequency <= 0 {
		return 0
	}
	rockDensity := e.rockDensityAt(x, y)
	moistBand := factorioRangeSelectBase(climate.moisture, 0.35, 1, 0.2, -10, 0)
	huge := 0.07 * e.control.size * (moistBand + rockDensity - 1.7)
	big := 0.17 * e.control.size * (moistBand + rockDensity - 1.6)
	sandBand := math.Min(
		factorioRangeSelectBase(climate.aux, 0.3, 1, 0.3, -10, 0),
		factorioRangeSelectBase(climate.moisture, 0, 0.3, 0.2, -10, 0),
	)
	sand := 0.1 * e.control.size * (sandBand + rockDensity - 1.6)
	return clampFloat(math.Max(huge, math.Max(big, sand)), 0, 1)
}

func (e *factorioRockEvaluator) colorAt(x, y float64) (color.RGBA, bool) {
	if e.densityAt(x, y) < factorioRockFootprintThreshold {
		return color.RGBA{}, false
	}
	return factorioRockMapColor, true
}

func (e *factorioRockEvaluator) colorAtSample(x, y float64, sample factorioNauvisSample) (color.RGBA, bool) {
	if e.densityAtSample(x, y, sample) < factorioRockFootprintThreshold {
		return color.RGBA{}, false
	}
	return factorioRockMapColor, true
}

func factorioRangeSelectBase(input, from, to, slope, minimum, maximum float64) float64 {
	return clampFloat(math.Min(input-from, to-input)/slope, minimum, maximum)
}

func factorioSliderRescale(value, target float64) float64 {
	if value == 1 {
		return 1
	}
	return math.Exp2(math.Log2(value) / math.Log2(6) * math.Log2(target))
}
