package main

import (
	"image/color"
	"math"
	"sync"
)

const (
	factorioVulcanusStartingAreaRadius = 0.7 * 0.75
	factorioVulcanusCracksScale        = 0.325
	factorioVulcanusBiomeContrast      = 2
	factorioVulcanusVolcanoRegionSize  = int64(256)
	factorioVulcanusAshlandsScale      = 3
)

type factorioVulcanusTile struct {
	name  string
	color color.RGBA
}

type factorioVulcanusSample struct {
	tile                 factorioVulcanusTile
	elevation            float64
	temperature          float64
	moisture             float64
	aux                  float64
	mountainsBiome       float64
	ashlandsBiome        float64
	basaltsBiome         float64
	mountainVolcanoSpots float64
}

type factorioVulcanusWobbles struct {
	x, y           float64
	largeX, largeY float64
	hugeX, hugeY   float64
}

type factorioVulcanusSpawnSample struct {
	distance       float64
	xFromStart     float64
	yFromStart     float64
	ashlandsStart  float64
	basaltsStart   float64
	mountainsStart float64
	startingArea   float64
}

type factorioVulcanusPreVolcanoSample struct {
	spawn                  factorioVulcanusSpawnSample
	wobbles                factorioVulcanusWobbles
	ashlandsRaw            float64
	basaltsRaw             float64
	mountainsRawPreVolcano float64
	mountainsBiomeFullPre  float64
	volcanoArea            float64
}

type factorioVulcanusCrackSample struct {
	hairline    float64
	floodA      float64
	floodB      float64
	floodPaths  float64
	floodBasalt float64
}

// factorioVulcanusEvaluator is a topological port of Factorio 2.1.12's
// default Vulcanus terrain expression graph. settings.seed must already be the
// effective surface seed; all state is immutable except the bounded,
// mutex-protected volcano-region cache.
type factorioVulcanusEvaluator struct {
	settings fastPreviewSettings
	seed     uint32
	starts   []factorioPoint

	scaleMultiplier   float64
	startingDirection float64
	ashlandsAngle     float64
	mountainsAngle    float64
	basaltsAngle      float64
	volcanism         float64
	volcanoRadius     float64
	volcanoSpacing    float64
	volcanismSquared  float64

	wobbleX      func(float64, float64) float64
	wobbleY      func(float64, float64) float64
	wobbleLargeX func(float64, float64) float64
	wobbleLargeY func(float64, float64) float64
	wobbleHugeX  func(float64, float64) float64
	wobbleHugeY  func(float64, float64) float64

	mountainsNear func(float64, float64) float64
	mountainsFar  func(float64, float64) float64
	ashlandsNear  func(float64, float64) float64
	ashlandsFar   func(float64, float64) float64
	basaltsNear   func(float64, float64) float64
	basaltsFar    func(float64, float64) float64

	hairlineNoise func(float64, float64) float64
	crackA1       func(float64, float64) float64
	crackA2       func(float64, float64) float64
	crackAMix     func(float64, float64) float64
	crackB1       func(float64, float64) float64
	crackB2       func(float64, float64) float64
	crackBMix     func(float64, float64) float64
	pathPlasma    func(float64, float64) float64
	pathDetail    func(float64, float64) float64

	auxNoise       func(float64, float64) float64
	moistureNoiseA func(float64, float64) float64
	moistureNoiseB func(float64, float64) float64

	mountainBasisNoise func(float64, float64) float64
	ashlandsBasisNoise func(float64, float64) float64
	mountainPlasma     func(float64, float64) float64
	mountainElevPlasma func(float64, float64) float64
	basaltDetail837    func(float64, float64) float64
	basaltDetail234    func(float64, float64) float64
	basaltDetail643    func(float64, float64) float64
	mountainLavaPlasma func(float64, float64) float64
	rockNoise          func(float64, float64) float64

	regionMu    sync.RWMutex
	regionSpots map[[2]int64][]factorioSelectedSpot
}

func newFactorioVulcanusEvaluator(settings fastPreviewSettings) *factorioVulcanusEvaluator {
	starts := settings.startingPositions
	if len(starts) == 0 {
		starts = []factorioPoint{{}}
	}
	volcanismControl := fastSpaceAgeControl(settings, "vulcanus_volcanism")
	frequency := positiveOr(volcanismControl.frequency, 1)
	size := positiveOr(volcanismControl.size, 1)
	scaleMultiplier := factorioVulcanusSliderRescale(frequency, 3)
	volcanism := 0.3 +
		0.7*factorioVulcanusSliderRescale(size, 3)/
			factorioVulcanusSliderRescale(scaleMultiplier, 3)
	normalizedSeed := float64(float32(float64(settings.seed) / 4294967296.0))
	startingDirection := float64(-1 + 2*int(settings.seed&1))
	ashlandsAngle := normalizedSeed * 3600

	e := &factorioVulcanusEvaluator{
		settings:          settings,
		seed:              settings.seed,
		starts:            starts,
		scaleMultiplier:   scaleMultiplier,
		startingDirection: startingDirection,
		ashlandsAngle:     ashlandsAngle,
		mountainsAngle:    ashlandsAngle + 120*startingDirection,
		basaltsAngle:      ashlandsAngle + 240*startingDirection,
		volcanism:         volcanism,
		volcanoRadius:     factorioFloat32(200 * volcanism),
		volcanoSpacing:    1500 * volcanism,
		volcanismSquared:  volcanism * volcanism,
		regionSpots:       make(map[[2]int64][]factorioSelectedSpot),
	}

	e.wobbleX = e.detailNoise(10, 1.0/8, 2, 4)
	e.wobbleY = e.detailNoise(1010, 1.0/8, 2, 4)
	e.wobbleLargeX = e.detailNoise(20, 1.0/2, 2, 50)
	e.wobbleLargeY = e.detailNoise(1020, 1.0/2, 2, 50)
	e.wobbleHugeX = e.detailNoise(30, 2, 2, 800)
	e.wobbleHugeY = e.detailNoise(1030, 2, 2, 800)

	e.mountainsNear = e.biomeNoise(342, 60*0.5)
	e.mountainsFar = e.biomeNoise(342+1000, 60)
	e.ashlandsNear = e.biomeNoise(12416, 40*0.5)
	e.ashlandsFar = e.biomeNoise(12416+1000, 40)
	e.basaltsNear = e.biomeNoise(42416, 80*0.5)
	e.basaltsFar = e.biomeNoise(42416+1000, 80)

	cs := factorioVulcanusCracksScale
	e.hairlineNoise = e.plasma(15223, 0.3*cs, 0.6*cs, 0.6, 1)
	e.crackA1 = e.plasma(7543, 2.5*cs, 4*cs, 0.5, 1)
	e.crackA2 = e.plasma(7443, 1.5*cs, 3.5*cs, 0.5, 1)
	e.crackAMix = e.detailNoise(241, 2*cs, 2, 0.25)
	e.crackB1 = e.plasma(12223, 2*cs, 3*cs, 0.5, 1)
	e.crackB2 = e.plasma(152, cs, 1.5*cs, 0.25, 0.5)
	e.crackBMix = e.detailNoise(821, 6*cs, 2, 0.5)
	e.pathPlasma = e.plasma(1543, 1.5*cs, 3*cs, 0.5, 1)
	e.pathDetail = e.detailNoise(121, cs*4, 2, 0.5)

	e.auxNoise = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: e.seed, seed1: 2, octaves: 5, persistence: 0.6,
		inputScale: 0.2, outputScale: 0.6,
	})
	e.moistureNoiseA = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: e.seed, seed1: 4, octaves: 2, persistence: 0.6,
		inputScale: 0.025, outputScale: 0.25,
	})
	e.moistureNoiseB = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: e.seed, seed1: 400, octaves: 3, persistence: 0.62,
		inputScale: 0.051144353, outputScale: 0.25,
	})

	e.mountainBasisNoise = e.basisNoise(13423, 1.0/500, 250)
	e.ashlandsBasisNoise = e.basisNoise(
		12643,
		e.scaleMultiplier/50/factorioVulcanusAshlandsScale,
		150,
	)
	e.mountainPlasma = e.plasma(102, 2.5, 10, 125, 625)
	e.mountainElevPlasma = e.plasma(13, 2.5, 10, 0.15, 0.75)
	e.basaltDetail837 = e.detailNoise(837, 1.0/40, 4, 1.25)
	e.basaltDetail234 = e.detailNoise(234, 1.0/50, 4, 1)
	e.basaltDetail643 = e.detailNoise(643, 1.0/70, 4, 0.7)
	e.mountainLavaPlasma = e.plasma(17453, 0.2, 0.4, 10, 20)
	e.rockNoise = makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: e.seed, seed1: 137, octaves: 4, persistence: 0.65,
		inputScale: 0.1, outputScale: 0.4,
	})
	return e
}

func (e *factorioVulcanusEvaluator) detailNoise(
	seed1 uint32,
	scale float64,
	octaves int,
	magnitude float64,
) func(float64, float64) float64 {
	return makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: e.seed, seed1: seed1 + 12243, octaves: octaves,
		persistence: 0.6, inputScale: 1 / 50.0 / scale, outputScale: magnitude,
	})
}

func (e *factorioVulcanusEvaluator) biomeNoise(
	seed1 uint32,
	scale float64,
) func(float64, float64) float64 {
	return makeFactorioMultioctaveNoise(factorioMultioctaveParams{
		seed0: e.seed, seed1: seed1, octaves: 5, persistence: 0.65,
		inputScale: e.scaleMultiplier / scale, outputScale: 1,
	})
}

func (e *factorioVulcanusEvaluator) basisNoise(
	seed1 uint32,
	inputScale, outputScale float64,
) func(float64, float64) float64 {
	tables := factorioBasisTablesFromSeed(e.seed, seed1)
	return func(x, y float64) float64 {
		return outputScale * factorioBasisNoise(x*inputScale, y*inputScale, &tables)
	}
}

func (e *factorioVulcanusEvaluator) plasma(
	seed uint32,
	scale, scale2, magnitude1, magnitude2 float64,
) func(float64, float64) float64 {
	tablesA := factorioBasisTablesFromSeed(e.seed, 12643)
	tablesB := factorioBasisTablesFromSeed(e.seed, 13423+seed)
	inputScaleA := 1 / 50.0 / scale
	inputScaleB := 1 / 50.0 / scale2
	return func(x, y float64) float64 {
		a := magnitude1 * factorioBasisNoise(x*inputScaleA, y*inputScaleA, &tablesA)
		b := magnitude2 * factorioBasisNoise(x*inputScaleB, y*inputScaleB, &tablesB)
		return math.Abs(a - b)
	}
}

func factorioVulcanusSliderRescale(value, target float64) float64 {
	if value == 1 {
		return 1
	}
	return math.Exp2(math.Log2(value) / math.Log2(6) * math.Log2(target))
}

func (e *factorioVulcanusEvaluator) wobblesAt(x, y float64) factorioVulcanusWobbles {
	return factorioVulcanusWobbles{
		x:      e.wobbleX(x, y),
		y:      e.wobbleY(x, y),
		largeX: e.wobbleLargeX(x, y),
		largeY: e.wobbleLargeY(x, y),
		hugeX:  e.wobbleHugeX(x, y),
		hugeY:  e.wobbleHugeY(x, y),
	}
}

func (e *factorioVulcanusEvaluator) relativeToNearestStart(
	x, y float64,
) (distance, xFromStart, yFromStart float64) {
	bestSquared := math.Inf(1)
	for _, start := range e.starts {
		startX := math.Round(start.x*256) / 256
		startY := math.Round(start.y*256) / 256
		dx := x - startX
		dy := y - startY
		distanceSquared := dx*dx + dy*dy
		if distanceSquared < bestSquared {
			bestSquared = distanceSquared
			xFromStart = dx
			yFromStart = dy
		}
	}
	return math.Sqrt(bestSquared), xFromStart, yFromStart
}

func factorioVulcanusStartingSpotAtAngle(
	angle, distance, radius,
	xDistortion, yDistortion,
	xFromStart, yFromStart float64,
) float64 {
	angleRadians := angle / 180 * math.Pi
	deltaX := distance*math.Sin(angleRadians) - xFromStart + xDistortion
	deltaY := -distance*math.Cos(angleRadians) - yFromStart + yDistortion
	return 1 - math.Sqrt(deltaX*deltaX+deltaY*deltaY)/radius
}

func (e *factorioVulcanusEvaluator) spawnAt(
	x, y float64,
	wobbles factorioVulcanusWobbles,
) factorioVulcanusSpawnSample {
	r := factorioVulcanusStartingAreaRadius
	distance, xFromStart, yFromStart := e.relativeToNearestStart(x, y)
	wobbleXSum := wobbles.x + wobbles.largeX + wobbles.hugeX
	wobbleYSum := wobbles.y + wobbles.largeY + wobbles.hugeY
	ashlandsStart := 4 * factorioVulcanusStartingSpotAtAngle(
		e.ashlandsAngle,
		170*r,
		350*r,
		0.1*r*wobbleXSum,
		0.1*r*wobbleYSum,
		xFromStart,
		yFromStart,
	)
	basaltsStart := 2 * factorioVulcanusStartingSpotAtAngle(
		e.basaltsAngle,
		250,
		550*r,
		0.1*r*wobbleXSum,
		0.1*r*wobbleYSum,
		xFromStart,
		yFromStart,
	)
	mountainsStart := 2 * factorioVulcanusStartingSpotAtAngle(
		e.mountainsAngle,
		250*r,
		500*r,
		0.05*r*wobbleXSum,
		0.05*r*wobbleYSum,
		xFromStart,
		yFromStart,
	)
	return factorioVulcanusSpawnSample{
		distance:       distance,
		xFromStart:     xFromStart,
		yFromStart:     yFromStart,
		ashlandsStart:  ashlandsStart,
		basaltsStart:   basaltsStart,
		mountainsStart: mountainsStart,
		startingArea: clampFloat(
			max(basaltsStart, mountainsStart, ashlandsStart),
			0,
			1,
		),
	}
}

func (e *factorioVulcanusEvaluator) preVolcanoAt(
	x, y float64,
) factorioVulcanusPreVolcanoSample {
	wobbles := e.wobblesAt(x, y)
	spawn := e.spawnAt(x, y, wobbles)
	distanceBlend := clampFloat(spawn.distance/10000, 0, 1)
	mountainsNoise := lerpFloat(e.mountainsNear(x, y), e.mountainsFar(x, y), distanceBlend)
	ashlandsNoise := lerpFloat(e.ashlandsNear(x, y), e.ashlandsFar(x, y), distanceBlend)
	basaltsNoise := lerpFloat(e.basaltsNear(x, y), e.basaltsFar(x, y), distanceBlend)
	startingBlend := clampFloat(2*spawn.startingArea, 0, 1)
	ashlandsRaw := lerpFloat(
		ashlandsNoise,
		-spawn.mountainsStart+spawn.ashlandsStart-spawn.basaltsStart,
		startingBlend,
	)
	basaltsRaw := lerpFloat(
		basaltsNoise,
		-spawn.mountainsStart-spawn.ashlandsStart+spawn.basaltsStart,
		startingBlend,
	)
	mountainsRawPreVolcano := lerpFloat(
		mountainsNoise,
		spawn.mountainsStart-spawn.ashlandsStart-spawn.basaltsStart,
		startingBlend,
	)
	mountainsBiomeFullPre := mountainsRawPreVolcano - max(ashlandsRaw, basaltsRaw)
	volcanoArea := lerpFloat(mountainsBiomeFullPre, 0, spawn.startingArea)
	return factorioVulcanusPreVolcanoSample{
		spawn:                  spawn,
		wobbles:                wobbles,
		ashlandsRaw:            ashlandsRaw,
		basaltsRaw:             basaltsRaw,
		mountainsRawPreVolcano: mountainsRawPreVolcano,
		mountainsBiomeFullPre:  mountainsBiomeFullPre,
		volcanoArea:            volcanoArea,
	}
}

func factorioVulcanusRegionIndex(coordinate float64) int64 {
	return int64(math.Floor(
		(coordinate + float64(factorioVulcanusVolcanoRegionSize)/2) /
			float64(factorioVulcanusVolcanoRegionSize),
	))
}

func (e *factorioVulcanusEvaluator) spotsForRegion(
	regionX, regionY int64,
) []factorioSelectedSpot {
	key := [2]int64{regionX, regionY}
	e.regionMu.RLock()
	if spots, ok := e.regionSpots[key]; ok {
		e.regionMu.RUnlock()
		return spots
	}
	e.regionMu.RUnlock()

	quantity := factorioFloat32(e.volcanoRadius * e.volcanoRadius)
	spots := factorioSelectSpots(
		factorioSpotRegionKey{
			seed0: e.seed, seed1: 1, regionX: regionX, regionY: regionY,
		},
		factorioSpotSelectionParams{
			regionSize:         factorioVulcanusVolcanoRegionSize,
			candidateSpotCount: 1,
			spacing:            e.volcanoSpacing,
			skipSpan:           1,
			skipOffset:         0,
			density: func(x, y float64) float64 {
				return e.preVolcanoAt(x, y).volcanoArea / e.volcanismSquared
			},
			quantity: func(_, _ float64) float64 {
				return quantity
			},
			favorability: func(x, y float64) float64 {
				return e.preVolcanoAt(x, y).volcanoArea
			},
		},
	)

	e.regionMu.Lock()
	defer e.regionMu.Unlock()
	if cached, ok := e.regionSpots[key]; ok {
		return cached
	}
	if len(e.regionSpots) >= fastPreviewMaxRegionsPerField {
		for oldKey := range e.regionSpots {
			delete(e.regionSpots, oldKey)
			break
		}
	}
	e.regionSpots[key] = spots
	return spots
}

func (e *factorioVulcanusEvaluator) rawVolcanoSpotsAt(x, y float64) float64 {
	radius := e.volcanoRadius
	cullSquared := radius * radius
	best := 0.0
	regionXLow := factorioVulcanusRegionIndex(x - radius)
	regionXHigh := factorioVulcanusRegionIndex(x + radius)
	regionYLow := factorioVulcanusRegionIndex(y - radius)
	regionYHigh := factorioVulcanusRegionIndex(y + radius)
	for regionX := regionXLow; regionX <= regionXHigh; regionX++ {
		for regionY := regionYLow; regionY <= regionYHigh; regionY++ {
			for _, spot := range e.spotsForRegion(regionX, regionY) {
				dx := x - spot.x
				dy := y - spot.y
				distanceSquared := dx*dx + dy*dy
				if distanceSquared > cullSquared {
					continue
				}
				peak := factorioFloat32(
					factorioFloat32(3*spot.quantity) /
						factorioFloat32(factorioFloat32(math.Pi*radius)*radius),
				)
				cone := factorioFloat32(
					peak -
						factorioFloat32(
							factorioFloat32(math.Sqrt(distanceSquared))*
								factorioFloat32(peak/radius),
						),
				)
				if cone > best {
					best = cone
				}
			}
		}
	}
	return best
}

func (e *factorioVulcanusEvaluator) mountainVolcanoSpotsAt(
	x, y float64,
	pre factorioVulcanusPreVolcanoSample,
) float64 {
	r := factorioVulcanusStartingAreaRadius
	offX := pre.wobbles.x/2 + pre.wobbles.largeX/12 + pre.wobbles.hugeX/80
	offY := pre.wobbles.y/2 + pre.wobbles.largeY/12 + pre.wobbles.hugeY/80
	startingProtector := clampFloat(
		factorioVulcanusStartingSpotAtAngle(
			e.mountainsAngle+180*e.startingDirection,
			(400*r)/2,
			800*r,
			offX,
			offY,
			pre.spawn.xFromStart,
			pre.spawn.yFromStart,
		),
		0,
		1,
	)
	startingVolcanoSpot := clampFloat(
		factorioVulcanusStartingSpotAtAngle(
			e.mountainsAngle,
			400*r,
			200,
			offX,
			offY,
			pre.spawn.xFromStart,
			pre.spawn.yFromStart,
		),
		0,
		1,
	)
	rawSpots := e.rawVolcanoSpotsAt(x+offX, y+offY)
	return max(startingVolcanoSpot, rawSpots-startingProtector)
}

func (e *factorioVulcanusEvaluator) cracksAt(
	x, y float64,
) factorioVulcanusCrackSample {
	hairline := e.hairlineNoise(x, y)
	floodA := lerpFloat(
		min(e.crackA1(x, y), e.crackA2(x, y)),
		1,
		clampFloat(e.crackAMix(x, y), 0, 1),
	)
	floodB := lerpFloat(
		1,
		min(e.crackB1(x, y), e.crackB2(x, y))-0.5,
		clampFloat(0.2+e.crackBMix(x, y), 0, 1),
	)
	floodPaths := 0.4 - e.pathPlasma(x, y) + min(0, e.pathDetail(x, y))
	floodBasalt := min(max(floodA-0.125, floodPaths), floodB) +
		0.3*min(0.5, hairline)
	return factorioVulcanusCrackSample{
		hairline:    hairline,
		floodA:      floodA,
		floodB:      floodB,
		floodPaths:  floodPaths,
		floodBasalt: floodBasalt,
	}
}

func factorioVulcanusContrast(value, contrast float64) float64 {
	return clampFloat(value, contrast, 1) - contrast
}

func factorioVulcanusThreshold(value, threshold float64) float64 {
	return (value - (1 - threshold)) * (1 / threshold)
}

func (e *factorioVulcanusEvaluator) basaltLakesAt(x, y float64) float64 {
	cracks := e.cracksAt(x, y)
	return e.basaltLakesFromCracks(x, y, cracks)
}

func (e *factorioVulcanusEvaluator) basaltLakesFromCracks(
	x, y float64,
	cracks factorioVulcanusCrackSample,
) float64 {
	resourceCutout := clampFloat(
		factorioVulcanusContrast(e.basaltDetail837(x, y), 0.95)*
			factorioVulcanusContrast(e.basaltDetail234(x, y), 0.95)*
			e.basaltDetail643(x, y),
		0,
		3,
	)
	return min(1, -0.2+cracks.floodBasalt-0.35*resourceCutout)
}

func (e *factorioVulcanusEvaluator) sample(x, y float64) factorioVulcanusSample {
	pre := e.preVolcanoAt(x, y)
	mountainVolcanoSpots := e.mountainVolcanoSpotsAt(x, y, pre)
	mountainsRawVolcano := 0.5*pre.mountainsRawPreVolcano + max(
		2*mountainVolcanoSpots,
		10*clampFloat((mountainVolcanoSpots-0.33)*3, 0, 1),
	)
	mountainsBiomeFull := mountainsRawVolcano - max(pre.ashlandsRaw, pre.basaltsRaw)
	ashlandsBiomeFull := pre.ashlandsRaw - max(mountainsRawVolcano, pre.basaltsRaw)
	basaltsBiomeFull := pre.basaltsRaw - max(mountainsRawVolcano, pre.ashlandsRaw)
	mountainsBiome := clampFloat(
		mountainsBiomeFull*factorioVulcanusBiomeContrast,
		0,
		1,
	)
	ashlandsBiome := clampFloat(
		ashlandsBiomeFull*factorioVulcanusBiomeContrast,
		0,
		1,
	)
	basaltsBiome := clampFloat(
		basaltsBiomeFull*factorioVulcanusBiomeContrast,
		0,
		1,
	)

	cracks := e.cracksAt(x, y)
	aux := clampFloat(
		min(math.Abs(e.auxNoise(x, y)), 0.3-0.6*cracks.floodPaths),
		0,
		1,
	)
	moisture := clampFloat(
		1-
			math.Abs(e.moistureNoiseA(x, y))-
			math.Abs(e.moistureNoiseB(x, y))-
			0.2*cracks.floodA,
		0,
		1,
	)

	mountainPlasma := e.mountainPlasma(x, y)
	mountainBasis := e.mountainBasisNoise(x, y)
	mountainElevation := lerpFloat(
		max(clampFloat(mountainPlasma, -100, 10000), mountainBasis),
		mountainPlasma,
		clampFloat(0.7*mountainBasis, 0, 1),
	) * (1 - clampFloat(e.mountainElevPlasma(x, y), 0, 1))
	volcanoInvertedPeak := (0.65 - math.Abs(mountainVolcanoSpots-0.65)) / 0.65
	mountainsFunc := lerpFloat(
		mountainElevation,
		700*volcanoInvertedPeak,
		clampFloat(mountainVolcanoSpots*3, 0, 1),
	) + 200*(aux-0.5)*(mountainVolcanoSpots+0.5)
	ashlandsBasis := e.ashlandsBasisNoise(x, y)
	ashlandsFunc := 300 + 0.001*min(ashlandsBasis, ashlandsBasis)
	basaltLakesMultisample := min(
		e.basaltLakesFromCracks(x, y, cracks),
		e.basaltLakesAt(x+1, y),
		e.basaltLakesAt(x, y+1),
		e.basaltLakesAt(x+1, y+1),
	)
	mountainsBlend := lerpFloat(
		120*basaltLakesMultisample,
		20+mountainsFunc*1.5,
		mountainsBiome,
	)
	elevation := lerpFloat(mountainsBlend, ashlandsFunc, ashlandsBiome)
	temperature := 100 + 100*e.settings.temperatureBias -
		min(elevation, elevation/100) -
		2*moisture -
		aux -
		20*ashlandsBiome +
		200*max(0, mountainVolcanoSpots-0.6)

	mountainThreshold := 0.4 * clampFloat(
		factorioVulcanusThreshold(mountainsBiome, 0.5),
		0,
		1,
	)
	mountainLavaSpots := clampFloat(
		factorioVulcanusThreshold(
			mountainVolcanoSpots*1.95-0.95,
			mountainThreshold,
		)*factorioVulcanusThreshold(
			clampFloat(e.mountainLavaPlasma(x, y)/20, 0, 1),
			1.8,
		),
		0,
		1,
	)

	tile := factorioResolveVulcanusTile(factorioVulcanusTileFields{
		elevation:            elevation,
		aux:                  aux,
		moisture:             moisture,
		mountainsBiome:       mountainsBiome,
		ashlandsBiome:        ashlandsBiome,
		basaltsBiome:         basaltsBiome,
		mountainVolcanoSpots: mountainVolcanoSpots,
		mountainLavaSpots:    mountainLavaSpots,
		rockNoise:            e.rockNoise(x, y),
		distance:             pre.spawn.distance,
	})
	return factorioVulcanusSample{
		tile:                 tile,
		elevation:            max(-500, elevation),
		temperature:          temperature,
		moisture:             moisture,
		aux:                  aux,
		mountainsBiome:       mountainsBiome,
		ashlandsBiome:        ashlandsBiome,
		basaltsBiome:         basaltsBiome,
		mountainVolcanoSpots: mountainVolcanoSpots,
	}
}

type factorioVulcanusTileFields struct {
	elevation            float64
	aux                  float64
	moisture             float64
	mountainsBiome       float64
	ashlandsBiome        float64
	basaltsBiome         float64
	mountainVolcanoSpots float64
	mountainLavaSpots    float64
	rockNoise            float64
	distance             float64
}

func factorioVulcanusRangeSelect(
	input, from, to, slope, minimum, maximum float64,
) float64 {
	return clampFloat(min(input-from, to-input)/slope, minimum, maximum)
}

// factorioResolveVulcanusTile preserves prototype order and uses strict
// greater-than comparisons, so the first prototype wins an exact tie just as
// Factorio's tile probability argmax does.
func factorioResolveVulcanusTile(f factorioVulcanusTileFields) factorioVulcanusTile {
	lavaSpawnExcluder := 0.0
	if f.distance > 10 {
		lavaSpawnExcluder = 1
	}
	lavaBasaltsRange := 100 * min(
		f.basaltsBiome*lavaSpawnExcluder*
			factorioVulcanusRangeSelect(f.elevation, -5000, 0, 1, -1000, 1),
		100,
	)
	lavaMountainsRange := 1100 * factorioVulcanusRangeSelect(
		f.mountainLavaSpots,
		0.2,
		10,
		1,
		0,
		1,
	)
	lavaHotBasaltsRange := 200 * min(
		f.basaltsBiome*lavaSpawnExcluder*
			factorioVulcanusRangeSelect(
				f.elevation,
				-5000,
				min(0, 5*(-2+4*f.rockNoise)),
				1,
				-1000,
				1,
			),
		100,
	)
	lavaHotMountainsRange := 1000 * factorioVulcanusRangeSelect(
		f.mountainLavaSpots,
		0.05,
		0.3,
		1,
		0,
		1,
	)
	volcanicCracksHotRange := f.basaltsBiome * factorioVulcanusRangeSelect(
		f.elevation,
		0,
		8,
		1,
		0,
		20,
	)
	volcanicCracksWarmRange := f.basaltsBiome*factorioVulcanusRangeSelect(
		f.elevation,
		8,
		22,
		1,
		0,
		5,
	) + (f.aux - 0.05)
	volcanicCracksColdRange := (0.5-f.ashlandsBiome)*factorioVulcanusRangeSelect(
		f.elevation,
		20,
		100,
		1,
		0,
		1,
	) + (f.aux - 0.3)
	volcanicSmoothStoneWarmRange := f.basaltsBiome*factorioVulcanusRangeSelect(
		f.elevation,
		8,
		20,
		1,
		0,
		5,
	) - (f.aux - 0.05)
	volcanicSmoothStoneRange := (0.5-f.ashlandsBiome)*factorioVulcanusRangeSelect(
		f.elevation,
		20,
		100,
		1,
		0,
		1,
	) - (f.aux - 0.3)
	volcanicFoldsFlatRange := 2*(f.mountainsBiome-0.5) - 0.15*f.mountainVolcanoSpots
	volcanicFoldsRange := 2*(f.mountainsBiome-0.5) +
		(f.aux - 0.5) +
		0.5*(f.mountainVolcanoSpots-0.1)
	volcanicFoldsWarmRange := 2*(f.mountainsBiome-0.5) +
		3*(f.mountainVolcanoSpots-0.85) -
		2*(f.aux-0.5)
	volcanicJaggedGroundRange := 5 * min(
		10,
		factorioVulcanusRangeSelect(f.elevation, 1010, 2000, 2, -10, 1)+
			3*(f.aux-0.5),
	)
	volcanicSoilLightMountains := min(0.8, 4*(f.mountainsBiome-0.25)) -
		0.35*f.mountainVolcanoSpots -
		3*(f.aux-0.2)
	volcanicSoilDarkMountains := min(0.8, 4*(f.mountainsBiome-0.25)) -
		0.35*f.mountainVolcanoSpots -
		(f.aux - 0.5)
	volcanicAshFlatsRange := 2*(f.ashlandsBiome-0.5) -
		1.5*(f.aux-0.25) -
		1.5*(f.moisture-0.6)
	volcanicAshLightRange := 2*(f.ashlandsBiome-0.5) - 1.5*(f.moisture-0.6)
	volcanicAshDarkRange := min(1, 4*(f.ashlandsBiome-0.25)) + max(
		-1.5*(f.aux-0.25),
		0.01-1.5*math.Abs(f.aux-0.5)-1.5*(f.moisture-0.66),
	)
	volcanicPumiceStonesRange := 2*(f.ashlandsBiome-0.5) +
		1.5*(f.aux-0.5) +
		1.5*(f.moisture-0.66)
	volcanicAshCracksRange := min(1, 4*(f.ashlandsBiome-0.25)) +
		1.5*(f.aux-0.5) -
		1.5*(f.moisture-0.66)
	volcanicAshSoilRange := 2 * (f.ashlandsBiome - 0.5)
	volcanicSoilLightAshlands := 2*(f.ashlandsBiome-0.5) + 1.5*(f.moisture-0.8)
	volcanicSoilDarkAshlands := 2*(f.ashlandsBiome-0.5) -
		1.5*(f.aux-0.25) +
		1.5*(f.moisture-0.8)
	volcanicSoilLightRange := max(volcanicSoilLightMountains, volcanicSoilLightAshlands)
	volcanicSoilDarkRange := max(volcanicSoilDarkMountains, volcanicSoilDarkAshlands)

	tiles := [...]struct {
		name        string
		color       color.RGBA
		probability float64
	}{
		{"volcanic-jagged-ground", color.RGBA{R: 58, G: 58, B: 48, A: 255}, volcanicJaggedGroundRange},
		{"lava", color.RGBA{R: 150, G: 49, B: 30, A: 255}, max(lavaBasaltsRange, lavaMountainsRange)},
		{"lava-hot", color.RGBA{R: 255, G: 138, B: 57, A: 255}, max(lavaHotBasaltsRange, lavaHotMountainsRange)},
		{"volcanic-cracks-hot", color.RGBA{R: 58, G: 33, B: 23, A: 255}, volcanicCracksHotRange},
		{"volcanic-cracks-warm", color.RGBA{R: 58, G: 38, B: 33, A: 255}, volcanicCracksWarmRange},
		{"volcanic-cracks", color.RGBA{R: 43, G: 42, B: 43, A: 255}, volcanicCracksColdRange},
		{"volcanic-folds-flat", color.RGBA{R: 44, G: 43, B: 44, A: 255}, volcanicFoldsFlatRange},
		{"volcanic-ash-light", color.RGBA{R: 53, G: 53, B: 53, A: 255}, volcanicAshLightRange},
		{"volcanic-ash-dark", color.RGBA{R: 53, G: 53, B: 53, A: 255}, volcanicAshDarkRange},
		{"volcanic-ash-flats", color.RGBA{R: 53, G: 53, B: 53, A: 255}, volcanicAshFlatsRange},
		{"volcanic-pumice-stones", color.RGBA{R: 46, G: 46, B: 46, A: 255}, volcanicPumiceStonesRange},
		{"volcanic-smooth-stone", color.RGBA{R: 50, G: 50, B: 58, A: 255}, volcanicSmoothStoneRange},
		{"volcanic-smooth-stone-warm", color.RGBA{R: 54, G: 50, B: 50, A: 255}, volcanicSmoothStoneWarmRange},
		{"volcanic-ash-cracks", color.RGBA{R: 67, G: 67, B: 67, A: 255}, volcanicAshCracksRange},
		{"volcanic-folds", color.RGBA{R: 43, G: 43, B: 43, A: 255}, volcanicFoldsRange},
		{"volcanic-folds-warm", color.RGBA{R: 65, G: 45, B: 45, A: 255}, volcanicFoldsWarmRange},
		{"volcanic-soil-dark", color.RGBA{R: 48, G: 51, B: 43, A: 255}, volcanicSoilDarkRange},
		{"volcanic-soil-light", color.RGBA{R: 58, G: 48, B: 43, A: 255}, volcanicSoilLightRange},
		{"volcanic-ash-soil", color.RGBA{R: 48, G: 48, B: 43, A: 255}, volcanicAshSoilRange},
	}
	winner := factorioVulcanusTile{name: tiles[0].name, color: tiles[0].color}
	winnerProbability := tiles[0].probability
	for index := 1; index < len(tiles); index++ {
		if tiles[index].probability > winnerProbability {
			winner = factorioVulcanusTile{name: tiles[index].name, color: tiles[index].color}
			winnerProbability = tiles[index].probability
		}
	}
	return winner
}
