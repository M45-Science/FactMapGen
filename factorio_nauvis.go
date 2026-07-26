package main

import (
	"image/color"
	"math"
)

type factorioPoint struct {
	x float64
	y float64
}

type factorioNauvisSample struct {
	elevation   float64
	aux         float64
	moisture    float64
	temperature float64
	hills       float64
	bridge      float64
	forestPath  float64
}

type factorioTerrainTile struct {
	name  string
	color color.RGBA
	water bool
}

var factorioTerrainTiles = [...]factorioTerrainTile{
	{name: "deepwater", color: color.RGBA{R: 38, G: 64, B: 73, A: 255}, water: true},
	{name: "water", color: color.RGBA{R: 51, G: 83, B: 95, A: 255}, water: true},
	{name: "grass-1", color: color.RGBA{R: 55, G: 53, B: 11, A: 255}},
	{name: "grass-2", color: color.RGBA{R: 66, G: 57, B: 15, A: 255}},
	{name: "grass-3", color: color.RGBA{R: 65, G: 52, B: 28, A: 255}},
	{name: "grass-4", color: color.RGBA{R: 59, G: 40, B: 18, A: 255}},
	{name: "dry-dirt", color: color.RGBA{R: 94, G: 66, B: 37, A: 255}},
	{name: "dirt-1", color: color.RGBA{R: 141, G: 104, B: 60, A: 255}},
	{name: "dirt-2", color: color.RGBA{R: 136, G: 96, B: 59, A: 255}},
	{name: "dirt-3", color: color.RGBA{R: 133, G: 92, B: 53, A: 255}},
	{name: "dirt-4", color: color.RGBA{R: 103, G: 72, B: 43, A: 255}},
	{name: "dirt-5", color: color.RGBA{R: 91, G: 63, B: 38, A: 255}},
	{name: "dirt-6", color: color.RGBA{R: 80, G: 55, B: 31, A: 255}},
	{name: "dirt-7", color: color.RGBA{R: 80, G: 54, B: 28, A: 255}},
	{name: "sand-1", color: color.RGBA{R: 138, G: 103, B: 58, A: 255}},
	{name: "sand-2", color: color.RGBA{R: 128, G: 93, B: 52, A: 255}},
	{name: "sand-3", color: color.RGBA{R: 115, G: 83, B: 47, A: 255}},
	{name: "red-desert-0", color: color.RGBA{R: 103, G: 70, B: 32, A: 255}},
	{name: "red-desert-1", color: color.RGBA{R: 116, G: 81, B: 39, A: 255}},
	{name: "red-desert-2", color: color.RGBA{R: 116, G: 84, B: 43, A: 255}},
	{name: "red-desert-3", color: color.RGBA{R: 128, G: 93, B: 52, A: 255}},
}

var factorioTerrainNoiseSeeds = [...]uint32{
	19, 20, 21, 22, 13, 6, 7, 8, 9, 10, 11, 12, 36, 37, 38, 30, 31, 32, 33,
}

type factorioNauvisEvaluator struct {
	segmentation float64
	waterLevel   float64
	starts       []factorioPoint
	lakes        []factorioPoint

	hillsNoise        func(float64, float64) float64
	cliffLevelTables  factorioBasisTables
	bridgeNoise       func(float64, float64) float64
	forestPathNoise   func(float64, float64) float64
	detailNoise       func(float64, float64, float64) float64
	persistenceNoise  func(float64, float64) float64
	macroA            func(float64, float64) float64
	macroB            func(float64, float64) float64
	startingLakeNoise func(float64, float64) float64
	auxNoise          func(float64, float64) float64
	moistureNoise     func(float64, float64) float64
	temperatureNoise  func(float64, float64) float64
	terrainNoise      [19]func(float64, float64) float64

	moistureBias             float64
	startingAreaMoistureSize float64
	startingAreaMoistureFreq float64
	auxBias                  float64
	temperatureBias          float64
}

func newFactorioNauvisEvaluator(settings fastPreviewSettings) *factorioNauvisEvaluator {
	segmentation := settings.water.frequency
	if segmentation <= 0 {
		segmentation = 1
	}
	waterSize := settings.water.size
	if waterSize <= 0 {
		waterSize = 1
	}
	nauvisSegmentation := 1.5 * segmentation
	offsetX := 10000 / nauvisSegmentation
	starts := settings.startingPositions
	if len(starts) == 0 {
		starts = []factorioPoint{{}}
	}

	evaluator := &factorioNauvisEvaluator{
		segmentation:             segmentation,
		waterLevel:               10 * math.Log2(waterSize),
		starts:                   starts,
		lakes:                    factorioStartingLakePositions(settings.seed, starts),
		moistureBias:             settings.moistureBias,
		startingAreaMoistureSize: settings.startingAreaMoistureSize,
		startingAreaMoistureFreq: settings.startingAreaMoistureFrequency,
		auxBias:                  settings.auxBias,
		temperatureBias:          settings.temperatureBias,
	}
	evaluator.hillsNoise = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: settings.seed, seed1: 900, octaves: 4, persistence: 0.5,
		inputScale: nauvisSegmentation / 90, outputScale: 1,
	})
	evaluator.cliffLevelTables = factorioBasisTablesFromSeed(settings.seed, 99584)
	evaluator.bridgeNoise = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: settings.seed, seed1: 700, octaves: 4, persistence: 0.5,
		inputScale: nauvisSegmentation / 150, outputScale: 1,
	})
	evaluator.forestPathNoise = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: settings.seed, seed1: 1800, octaves: 4, persistence: 0.5,
		inputScale: nauvisSegmentation / 100, outputScale: 1,
	})
	evaluator.detailNoise = makeFactorioVariablePersistenceNoise(factorioVariablePersistenceParams{
		seed0: settings.seed, seed1: 600, octaves: 5,
		inputScale: nauvisSegmentation / 14, outputScale: 0.03, offsetX: offsetX,
	})
	evaluator.persistenceNoise = makeFactorioAmplitudeCorrectedNoise(factorioAmplitudeCorrectedParams{
		seed0: settings.seed, seed1: 500, octaves: 5,
		inputScale: nauvisSegmentation / 2, offsetX: offsetX,
		persistence: 0.7, amplitude: 0.5,
	})
	evaluator.macroA = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: settings.seed, seed1: 1000, octaves: 2, persistence: 0.6,
		inputScale: nauvisSegmentation / 1600, outputScale: 1,
	})
	evaluator.macroB = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: settings.seed, seed1: 1100, octaves: 1, persistence: 0.6,
		inputScale: nauvisSegmentation / 1600, outputScale: 1,
	})
	evaluator.startingLakeNoise = makeFactorioQuickPersistenceNoise(factorioQuickPersistenceParams{
		seed0: settings.seed, seed1: 14, octaves: 4,
		inputScale: 1.0 / 8, outputScale: 0.8,
		octaveInputScaleMultiplier: 0.5, persistence: 0.68,
	})

	moistureFrequency := positiveOr(settings.moistureFrequency, 1)
	evaluator.moistureNoise = makeFactorioQuickMultioctaveNoise(factorioQuickMultioctaveParams{
		seed0: settings.seed, seed1: 6, octaves: 4,
		inputScale: moistureFrequency / 256, outputScale: 0.125,
		offsetX:                     30000 / moistureFrequency,
		octaveOutputScaleMultiplier: 1.5,
		octaveInputScaleMultiplier:  1.0 / 3,
	})
	auxFrequency := positiveOr(settings.auxFrequency, 1)
	evaluator.auxNoise = makeFactorioQuickMultioctaveNoise(factorioQuickMultioctaveParams{
		seed0: settings.seed, seed1: 7, octaves: 4,
		inputScale: auxFrequency / 2048, outputScale: 0.25,
		offsetX:                     20000 / auxFrequency,
		octaveOutputScaleMultiplier: 0.5,
		octaveInputScaleMultiplier:  3,
	})
	temperatureFrequency := positiveOr(settings.temperatureFreq, 1)
	evaluator.temperatureNoise = makeFactorioQuickMultioctaveNoise(factorioQuickMultioctaveParams{
		seed0: settings.seed, seed1: 5, octaves: 4,
		inputScale: temperatureFrequency / 32, outputScale: 1.0 / 20,
		offsetX:                     40000 / temperatureFrequency,
		octaveOutputScaleMultiplier: 3,
		octaveInputScaleMultiplier:  1.0 / 3,
	})
	for i, seed1 := range factorioTerrainNoiseSeeds {
		evaluator.terrainNoise[i] = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
			seed0: settings.seed, seed1: seed1, octaves: 4, persistence: 0.7,
			inputScale: 1.0 / 6, outputScale: 2.0 / 3,
		})
	}
	return evaluator
}

func (e *factorioNauvisEvaluator) sample(x, y float64) factorioNauvisSample {
	nauvisSegmentation := 1.5 * e.segmentation
	hills := math.Abs(e.hillsNoise(x, y))
	cliffLevel := clampFloat(
		0.65+0.6*factorioBasisNoise(
			x*nauvisSegmentation/500,
			y*nauvisSegmentation/500,
			&e.cliffLevelTables,
		),
		0.15,
		1.15,
	)
	plateaus := 0.5 + clampFloat((hills-cliffLevel)*10, -0.5, 0.5)
	bridgeBillows := math.Abs(e.bridgeNoise(x, y))
	forestPathBillows := math.Abs(e.forestPathNoise(x, y))
	distance := factorioDistanceFromNearestPoint(x, y, e.starts, math.Inf(1))
	elevation := e.elevationFromShared(
		x,
		y,
		bridgeBillows,
		distance,
		0.1*hills+0.8*plateaus,
	)

	aux := clampFloat(0.5+e.auxBias+0.06*(plateaus-0.4)+e.auxNoise(x, y), 0, 1)

	startingBiasChange := factorioSliderToLinear(e.startingAreaMoistureSize, -0.5, 0.5)
	startingBias := lerpFloat(
		e.moistureBias,
		startingBiasChange,
		math.Abs(2*startingBiasChange)*1.1,
	)
	startingBiasRegion := clampFloat(
		2-(e.startingAreaMoistureFreq/400)*distance,
		0,
		1,
	)
	adjustedBias := lerpFloat(e.moistureBias, startingBias, startingBiasRegion)
	moistureMain := clampFloat(
		0.4+adjustedBias+e.moistureNoise(x, y)-0.08*(plateaus-0.6),
		0,
		1,
	)
	forestPathCutout := min(
		(bridgeBillows-0.07)*5,
		min((hills-0.1)*3, (forestPathBillows-0.07)*3),
	)
	moisture := max(
		min(moistureMain, 0.45),
		moistureMain-0.2*max(0, 1-forestPathCutout*1.5),
	)
	temperature := clampFloat(15+e.temperatureBias+e.temperatureNoise(x, y), -20, 50)

	return factorioNauvisSample{
		elevation: elevation, aux: aux, moisture: moisture, temperature: temperature,
		hills: hills, bridge: bridgeBillows, forestPath: forestPathBillows,
	}
}

func (e *factorioNauvisEvaluator) elevationNoCliff(x, y float64) float64 {
	distance := factorioDistanceFromNearestPoint(x, y, e.starts, math.Inf(1))
	return e.elevationFromShared(x, y, math.Abs(e.bridgeNoise(x, y)), distance, 0)
}

func (e *factorioNauvisEvaluator) elevationFromShared(
	x, y, bridgeBillows, distance, addedCliffElevation float64,
) float64 {
	nauvisSegmentation := 1.5 * e.segmentation
	persistence := clampFloat(e.persistenceNoise(x, y)+0.55, 0.5, 0.65)
	detail := e.detailNoise(x, y, persistence)
	bridges := 1 - 0.1*bridgeBillows - 0.9*max(0, -0.1+bridgeBillows)
	macro := e.macroA(x, y) * max(0, e.macroB(x, y))
	startingMacroMultiplier := clampFloat(distance*nauvisSegmentation/2000, 0, 1)
	mainElevation := 20 * (lerpFloat(
		0.5*addedCliffElevation-0.6,
		1.9*addedCliffElevation+1.6,
		0.1+0.5*bridges,
	) +
		0.25*detail +
		3*macro*startingMacroMultiplier)
	startingIsland := mainElevation + 20*(2.5-distance*e.segmentation/200)
	waterAndLandElevation := max(mainElevation-e.waterLevel*2, startingIsland)
	startingLakeDistance := factorioDistanceFromNearestPoint(x, y, e.lakes, 1024)
	startingLake := (20 * (-3 + (startingLakeDistance+e.startingLakeNoise(x, y))/8)) / 8
	return min(waterAndLandElevation, startingLake)
}

func factorioStartingLakePositions(seed uint32, starts []factorioPoint) []factorioPoint {
	word := seed
	if word < factorioMinSeedWord {
		word = factorioMinSeedWord
	}
	state := newFactorioTaus88State(word)
	lakes := make([]factorioPoint, 0, len(starts))
	for _, start := range starts {
		u := float64(state.next()) * 2.3283064365386963e-10
		turns := float64(float32(u*2*math.Pi)) * 0.15915494309189535
		lakes = append(lakes, factorioPoint{
			x: math.Trunc(start.x + 75*factorioStartingLakeSin(turns)),
			y: math.Trunc(start.y + 75*factorioStartingLakeSin(turns-0.25)),
		})
	}
	return lakes
}

func factorioStartingLakeSin(turns float64) float64 {
	rounded := math.Trunc(turns + math.Copysign(0.5, turns))
	x := 0.25 - math.Abs(turns-rounded)
	x2 := x * x
	x4 := x2 * x2
	x8 := x4 * x4
	polynomial := 6.283185269630412 -
		x2*41.34167506665737 +
		x4*(81.60201529595571-x2*76.56887678023256)
	return x * (x8*39.65735524898863 + polynomial)
}

func factorioDistanceFromNearestPoint(x, y float64, points []factorioPoint, maximum float64) float64 {
	bestSquared := maximum * maximum
	for _, point := range points {
		pointX := math.Round(point.x*256) / 256
		pointY := math.Round(point.y*256) / 256
		dx := x - pointX
		dy := y - pointY
		distanceSquared := dx*dx + dy*dy
		if distanceSquared < bestSquared {
			bestSquared = distanceSquared
		}
	}
	if bestSquared < maximum*maximum {
		return math.Sqrt(bestSquared)
	}
	return maximum
}

func factorioSliderToLinear(value, low, high float64) float64 {
	value = positiveOr(value, 1)
	return low + 0.5*(high-low)*(1+math.Log2(value)/math.Log2(6))
}

func positiveOr(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}

func (e *factorioNauvisEvaluator) terrainTile(sample factorioNauvisSample, x, y float64) factorioTerrainTile {
	winnerIndex := 0
	winnerProbability := factorioWaterBase(sample.elevation, -2, 200)
	waterProbability := factorioWaterBase(sample.elevation, 0, 100)
	if waterProbability > winnerProbability {
		winnerIndex = 1
		winnerProbability = waterProbability
	}
	if waterProbability >= 5 {
		return factorioTerrainTiles[winnerIndex]
	}

	probabilities := [...]float64{
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.7, 11, 11) + e.terrainNoise[0](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.45, 0.45, 11, 0.8) + e.terrainNoise[1](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.6, 0.65, 0.9) + e.terrainNoise[2](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.5, 0.55, 0.7) + e.terrainNoise[3](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.45, -10, 0.55, 0.35) + e.terrainNoise[4](x, y),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.25, 0.45, 0.3),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.4, -10, 0.45, 0.25),
		) + e.terrainNoise[5](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.3, 0.45, 0.35) + e.terrainNoise[6](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.35, 0.55, 0.4) + e.terrainNoise[7](x, y),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.55, -10, 0.6, 0.35),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.6, 0.3, 11, 0.35),
		) + e.terrainNoise[8](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.4, 0.55, 0.45) + e.terrainNoise[9](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.45, 0.55, 0.5) + e.terrainNoise[10](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.5, 0.55, 0.55) + e.terrainNoise[11](x, y),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, -10, 0.25, 0.15),
			factorioExpressionInRange2(5, math.Inf(1), sample.elevation, sample.aux, -1.5, 0.5, 1.5, 1),
		) + e.terrainNoise[12](x, y),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.15, 0.3, 0.2),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.25, -10, 0.3, 0.15),
		) + e.terrainNoise[13](x, y),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.2, 0.4, 0.25),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.3, -10, 0.4, 0.2),
		) + e.terrainNoise[14](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.55, 0.35, 11, 0.5) + e.terrainNoise[15](x, y),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.6, -10, 0.7, 0.3),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.7, 0.25, 11, 0.3),
		) + e.terrainNoise[16](x, y),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.7, -10, 0.8, 0.25),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.8, 0.2, 11, 0.25),
		) + e.terrainNoise[17](x, y),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.8, -10, 11, 0.2) + e.terrainNoise[18](x, y),
	}
	for i, probability := range probabilities {
		if probability > winnerProbability {
			winnerIndex = i + 2
			winnerProbability = probability
		}
	}
	return factorioTerrainTiles[winnerIndex]
}

func factorioWaterBase(elevation, maximum, influence float64) float64 {
	if maximum < elevation {
		return math.Inf(-1)
	}
	return influence * min(maximum-elevation, 1)
}

func factorioExpressionInRangeBase(aux, moisture, auxFrom, moistureFrom, auxTo, moistureTo float64) float64 {
	return factorioExpressionInRange2(20, 1, aux, moisture, auxFrom, moistureFrom, auxTo, moistureTo)
}

func factorioExpressionInRange2(
	peakMultiplier, peakMaximum,
	value1, value2,
	from1, from2,
	to1, to2 float64,
) float64 {
	minimum := min(
		min(value1-from1, to1-value1),
		min(value2-from2, to2-value2),
	)
	return min(peakMaximum, peakMultiplier*minimum)
}
