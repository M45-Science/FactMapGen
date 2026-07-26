package main

import (
	"context"
	"image/color"
	"math"
)

const (
	factorioResourceDoubleDensityDistance = 1300.0
	factorioResourceFadeInDistance        = 300.0
	factorioStartingResourceRadius        = 150.0
	factorioRegularResourceSpacing        = 45.254833995939045
	factorioRegularResourceRegionSize     = int64(1024)
	factorioRegularResourceCullRadius     = 128.0
	factorioRegularResourceMaxRadius      = 32.0
	factorioStartingResourceSpacing       = 48.0
	factorioStartingResourceRegionSize    = int64(450)
	factorioStartingResourceCandidates    = 32
	factorioRegularResourceSkipSpan       = 6
	factorioStartingResourceSkipSpan      = 4
	factorioResourceBlobAmplitude         = 1.0 / 8.0
	factorioStartingResourceSplit         = 0.5
	factorioChunkSize                     = int64(32)
	factorioOilChartRadius                = 0.5
)

type factorioResourceParams struct {
	name                     string
	patchSetIndex            int
	baseDensity              float64
	baseSpotsPerSquareKM     float64
	candidateSpotCount       int
	regularRQFactor          float64
	startingRQFactor         float64
	seed1                    uint32
	randomProbability        float64
	randomSpotSizeMin        float64
	randomSpotSizeMax        float64
	hasStartingAreaPlacement bool
	mapColor                 color.RGBA
}

var factorioResourceCatalog = [...]factorioResourceParams{
	{
		name: "iron-ore", patchSetIndex: 0, baseDensity: 10,
		baseSpotsPerSquareKM: 2.5, candidateSpotCount: 22,
		regularRQFactor: 0.11, startingRQFactor: 1.5 / 7, seed1: 100,
		randomProbability: 1, randomSpotSizeMin: 0.25, randomSpotSizeMax: 2,
		hasStartingAreaPlacement: true,
		mapColor:                 color.RGBA{R: 105, G: 133, B: 147, A: 255},
	},
	{
		name: "copper-ore", patchSetIndex: 1, baseDensity: 8,
		baseSpotsPerSquareKM: 2.5, candidateSpotCount: 22,
		regularRQFactor: 0.11, startingRQFactor: 1.2 / 7, seed1: 100,
		randomProbability: 1, randomSpotSizeMin: 0.25, randomSpotSizeMax: 2,
		hasStartingAreaPlacement: true,
		mapColor:                 color.RGBA{R: 204, G: 98, B: 54, A: 255},
	},
	{
		name: "coal", patchSetIndex: 2, baseDensity: 8,
		baseSpotsPerSquareKM: 2.5, candidateSpotCount: 21,
		regularRQFactor: 0.1, startingRQFactor: 1.1 / 7, seed1: 100,
		randomProbability: 1, randomSpotSizeMin: 0.25, randomSpotSizeMax: 2,
		hasStartingAreaPlacement: true,
		mapColor:                 color.RGBA{A: 255},
	},
	{
		name: "stone", patchSetIndex: 3, baseDensity: 4,
		baseSpotsPerSquareKM: 2.5, candidateSpotCount: 21,
		regularRQFactor: 0.1, startingRQFactor: 1.1 / 7, seed1: 100,
		randomProbability: 1, randomSpotSizeMin: 0.25, randomSpotSizeMax: 2,
		hasStartingAreaPlacement: true,
		mapColor:                 color.RGBA{R: 175, G: 155, B: 108, A: 255},
	},
	{
		name: "crude-oil", patchSetIndex: 4, baseDensity: 8.2,
		baseSpotsPerSquareKM: 1.8, candidateSpotCount: 21,
		regularRQFactor: 0.1, startingRQFactor: 1.0 / 7, seed1: 100,
		randomProbability: 1.0 / 48.0, randomSpotSizeMin: 1, randomSpotSizeMax: 1,
		mapColor: color.RGBA{R: 198, G: 51, B: 196, A: 255},
	},
	{
		name: "uranium-ore", patchSetIndex: 5, baseDensity: 0.9,
		baseSpotsPerSquareKM: 1.25, candidateSpotCount: 21,
		regularRQFactor: 0.1, startingRQFactor: 1.0 / 7, seed1: 100,
		randomProbability: 1, randomSpotSizeMin: 2, randomSpotSizeMax: 4,
		mapColor: color.RGBA{G: 178, A: 255},
	},
}

type factorioRegularResourceField struct {
	params      factorioResourceParams
	control     fastControl
	seed0       uint32
	starts      []factorioPoint
	tables      factorioBasisTables
	basement    float64
	regionCache map[[2]int64][]factorioSelectedSpot
	skipSpan    int
	skipOffset  int
}

type factorioStartingResourceField struct {
	params        factorioResourceParams
	control       fastControl
	seed0         uint32
	starts        []factorioPoint
	nauvis        *factorioNauvisEvaluator
	tables        factorioBasisTables
	basement      float64
	quantity      float64
	maxCullRadius float64
	regionCache   map[[2]int64][]factorioSelectedSpot
	skipSpan      int
	skipOffset    int
}

type factorioResourceField struct {
	params   factorioResourceParams
	regular  *factorioRegularResourceField
	starting *factorioStartingResourceField
}

type factorioResourceEvaluator struct {
	seed             uint32
	fields           []factorioResourceField
	oilPenaltyChunks map[[2]int64][]float64
}

func newFactorioResourceEvaluator(settings fastPreviewSettings, nauvis *factorioNauvisEvaluator) *factorioResourceEvaluator {
	fields := make([]factorioResourceField, 0, len(factorioResourceCatalog))
	for _, params := range factorioResourceCatalog {
		control, ok := settings.resourceControls[params.name]
		if !ok || !control.enabled || control.frequency <= 0 || control.size <= 0 {
			continue
		}
		field := newFactorioResourceField(
			params,
			control,
			settings.seed,
			nauvis.starts,
			nauvis,
			factorioRegularResourceSkipSpan,
			params.patchSetIndex,
			factorioStartingResourceSkipSpan,
			params.patchSetIndex,
		)
		fields = append(fields, field)
	}
	return &factorioResourceEvaluator{
		seed:             settings.seed,
		fields:           fields,
		oilPenaltyChunks: make(map[[2]int64][]float64),
	}
}

func newFactorioResourceField(
	params factorioResourceParams,
	control fastControl,
	seed0 uint32,
	starts []factorioPoint,
	nauvis *factorioNauvisEvaluator,
	regularSkipSpan int,
	regularSkipOffset int,
	startingSkipSpan int,
	startingSkipOffset int,
) factorioResourceField {
	field := factorioResourceField{
		params: params,
		regular: newFactorioRegularResourceField(
			params,
			control,
			seed0,
			starts,
			regularSkipSpan,
			regularSkipOffset,
		),
	}
	if params.hasStartingAreaPlacement {
		field.starting = newFactorioStartingResourceField(
			params,
			control,
			seed0,
			starts,
			nauvis,
			startingSkipSpan,
			startingSkipOffset,
		)
	}
	return field
}

func newFactorioRegularResourceField(
	params factorioResourceParams,
	control fastControl,
	seed0 uint32,
	starts []factorioPoint,
	skipSpan int,
	skipOffset int,
) *factorioRegularResourceField {
	if len(starts) == 0 {
		starts = []factorioPoint{{}}
	}
	return &factorioRegularResourceField{
		params:      params,
		control:     control,
		seed0:       seed0,
		starts:      starts,
		tables:      factorioBasisTablesFromSeed(seed0, params.seed1),
		basement:    factorioResourceBasement(params, control),
		regionCache: make(map[[2]int64][]factorioSelectedSpot),
		skipSpan:    skipSpan,
		skipOffset:  skipOffset,
	}
}

func newFactorioStartingResourceField(
	params factorioResourceParams,
	control fastControl,
	seed0 uint32,
	starts []factorioPoint,
	nauvis *factorioNauvisEvaluator,
	skipSpan int,
	skipOffset int,
) *factorioStartingResourceField {
	if len(starts) == 0 {
		starts = []factorioPoint{{}}
	}
	quantity := factorioStartingResourceQuantity(params, control)
	return &factorioStartingResourceField{
		params:        params,
		control:       control,
		seed0:         seed0,
		starts:        starts,
		nauvis:        nauvis,
		tables:        factorioBasisTablesFromSeed(seed0, params.seed1),
		basement:      factorioResourceBasement(params, control),
		quantity:      quantity,
		maxCullRadius: 2 * params.startingRQFactor * factorioFastCbrt(quantity),
		regionCache:   make(map[[2]int64][]factorioSelectedSpot),
		skipSpan:      skipSpan,
		skipOffset:    skipOffset,
	}
}

func (e *factorioResourceEvaluator) resourceAt(x, y float64) (factorioResourceParams, bool) {
	for index := range e.fields {
		field := &e.fields[index]
		if field.params.randomProbability < 1 {
			continue
		}
		probability := clampFloat(field.fieldAt(x, y), 0, 1)
		if probability >= 0.5 {
			return field.params, true
		}
	}
	return factorioResourceParams{}, false
}

func (e *factorioResourceEvaluator) oilAtTile(tileX, tileY int64) (factorioResourceParams, bool) {
	for index := range e.fields {
		field := &e.fields[index]
		if field.params.randomProbability >= 1 {
			continue
		}
		penalty := e.oilRandomPenaltyAt(tileX, tileY)
		if penalty <= 0 {
			return factorioResourceParams{}, false
		}
		probability := clampFloat(field.fieldAt(float64(tileX), float64(tileY)), 0, 1)
		probability *= math.Max(0, penalty)
		// Factorio performs a second entity-autoplace roll after evaluating the
		// chunk-ordered random-penalty expression. That roll shares state with
		// the other entity autoplacers, so use a map-seeded positional roll here.
		if probability > 0 && fastHashUnit(e.seed, 0x4f494c, tileX, tileY) < probability {
			return field.params, true
		}
	}
	return factorioResourceParams{}, false
}

func (e *factorioResourceEvaluator) hasOil() bool {
	for index := range e.fields {
		if e.fields[index].params.randomProbability < 1 {
			return true
		}
	}
	return false
}

func (e *factorioResourceEvaluator) oilPreviewMask(
	ctx context.Context,
	settings fastPreviewSettings,
	originX, originY, tilesPerPixel float64,
	width, height int,
) ([]bool, error) {
	mask := make([]bool, width*height)
	for py := 0; py < height; py++ {
		if py&31 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		minTileY := int64(math.Floor(originY + float64(py)*tilesPerPixel))
		maxTileY := int64(math.Ceil(originY+float64(py+1)*tilesPerPixel)) - 1
		spanY := max(int64(1), maxTileY-minTileY+1)
		samplesY := min(spanY, int64(4))
		for px := 0; px < width; px++ {
			minTileX := int64(math.Floor(originX + float64(px)*tilesPerPixel))
			maxTileX := int64(math.Ceil(originX+float64(px+1)*tilesPerPixel)) - 1
			spanX := max(int64(1), maxTileX-minTileX+1)
			samplesX := min(spanX, int64(4))
			for sampleY := int64(0); sampleY < samplesY; sampleY++ {
				tileY := minTileY + sampleY*spanY/samplesY
				for sampleX := int64(0); sampleX < samplesX; sampleX++ {
					tileX := minTileX + sampleX*spanX/samplesX
					if fastOutOfMapBounds(settings, float64(tileX), float64(tileY)) {
						continue
					}
					if _, ok := e.oilAtTile(tileX, tileY); !ok {
						continue
					}
					minX := int(math.Floor((float64(tileX) - factorioOilChartRadius - originX) / tilesPerPixel))
					maxX := int(math.Floor((float64(tileX) + factorioOilChartRadius - originX) / tilesPerPixel))
					minY := int(math.Floor((float64(tileY) - factorioOilChartRadius - originY) / tilesPerPixel))
					maxY := int(math.Floor((float64(tileY) + factorioOilChartRadius - originY) / tilesPerPixel))
					for markY := max(0, minY); markY <= min(height-1, maxY); markY++ {
						for markX := max(0, minX); markX <= min(width-1, maxX); markX++ {
							mask[markY*width+markX] = true
						}
					}
				}
			}
		}
	}
	return mask, nil
}

func (e *factorioResourceEvaluator) oilRandomPenaltyAt(tileX, tileY int64) float64 {
	chunkX := factorioFloorDiv(tileX, factorioChunkSize)
	chunkY := factorioFloorDiv(tileY, factorioChunkSize)
	key := [2]int64{chunkX, chunkY}
	penalties, ok := e.oilPenaltyChunks[key]
	if !ok {
		originX := chunkX * factorioChunkSize
		originY := chunkY * factorioChunkSize
		x := uint32(int32(originX))
		y := uint32(int32(originY + 1))
		word := uint32(factorioSpotSeedBase) + x*factorioSpotRegionXPrime + y*factorioSpotRegionYPrime
		if word < factorioMinSeedWord {
			word = factorioMinSeedWord
		}
		state := newFactorioTaus88State(word)
		penalties = make([]float64, factorioChunkSize*factorioChunkSize)
		amplitude := 1 / factorioResourceCatalog[4].randomProbability
		for index := len(penalties) - 1; index >= 0; index-- {
			penalties[index] = 1 - amplitude*float64(state.next())/4294967296.0
		}
		e.oilPenaltyChunks[key] = penalties
	}
	localX := tileX - chunkX*factorioChunkSize
	localY := tileY - chunkY*factorioChunkSize
	return penalties[localY*factorioChunkSize+localX]
}

func factorioFloorDiv(value, divisor int64) int64 {
	quotient := value / divisor
	if value < 0 && value%divisor != 0 {
		quotient--
	}
	return quotient
}

func (e *factorioResourceEvaluator) prepareForBounds(x0, y0, x1, y1 float64) {
	for index := range e.fields {
		field := &e.fields[index]
		regularLowX := factorioResourceRegionIndex(x0-factorioRegularResourceCullRadius, factorioRegularResourceRegionSize)
		regularHighX := factorioResourceRegionIndex(x1+factorioRegularResourceCullRadius, factorioRegularResourceRegionSize)
		regularLowY := factorioResourceRegionIndex(y0-factorioRegularResourceCullRadius, factorioRegularResourceRegionSize)
		regularHighY := factorioResourceRegionIndex(y1+factorioRegularResourceCullRadius, factorioRegularResourceRegionSize)
		for regionY := regularLowY; regionY <= regularHighY; regionY++ {
			for regionX := regularLowX; regionX <= regularHighX; regionX++ {
				field.regular.regionSpots(regionX, regionY)
			}
		}
		if field.starting == nil {
			continue
		}
		startingLowX := factorioResourceRegionIndex(x0-field.starting.maxCullRadius, factorioStartingResourceRegionSize)
		startingHighX := factorioResourceRegionIndex(x1+field.starting.maxCullRadius, factorioStartingResourceRegionSize)
		startingLowY := factorioResourceRegionIndex(y0-field.starting.maxCullRadius, factorioStartingResourceRegionSize)
		startingHighY := factorioResourceRegionIndex(y1+field.starting.maxCullRadius, factorioStartingResourceRegionSize)
		for regionY := startingLowY; regionY <= startingHighY; regionY++ {
			for regionX := startingLowX; regionX <= startingHighX; regionX++ {
				field.starting.regionSpots(regionX, regionY)
			}
		}
	}
}

func (f *factorioResourceField) fieldAt(x, y float64) float64 {
	value := f.regular.fieldAt(x, y)
	if f.starting != nil {
		value = math.Max(value, f.starting.fieldAt(x, y))
	}
	return value
}

func (f *factorioRegularResourceField) fieldAt(x, y float64) float64 {
	return f.spotFieldAt(x, y) + f.blobTermAt(x, y)
}

func (f *factorioRegularResourceField) spotFieldAt(x, y float64) float64 {
	best := f.basement
	lowX := factorioResourceRegionIndex(x-factorioRegularResourceCullRadius, factorioRegularResourceRegionSize)
	highX := factorioResourceRegionIndex(x+factorioRegularResourceCullRadius, factorioRegularResourceRegionSize)
	lowY := factorioResourceRegionIndex(y-factorioRegularResourceCullRadius, factorioRegularResourceRegionSize)
	highY := factorioResourceRegionIndex(y+factorioRegularResourceCullRadius, factorioRegularResourceRegionSize)
	for regionY := lowY; regionY <= highY; regionY++ {
		for regionX := lowX; regionX <= highX; regionX++ {
			for _, spot := range f.regionSpots(regionX, regionY) {
				dx := x - spot.x
				dy := y - spot.y
				distanceSquared := dx*dx + dy*dy
				if distanceSquared > factorioRegularResourceCullRadius*factorioRegularResourceCullRadius {
					continue
				}
				radius := math.Min(
					factorioRegularResourceMaxRadius,
					factorioFloat32(f.params.regularRQFactor*factorioFastCbrt(spot.quantity)),
				)
				if radius <= 0 {
					continue
				}
				peak := factorioFloat32(
					factorioFloat32(3*spot.quantity) /
						factorioFloat32(factorioFloat32(math.Pi*radius)*radius),
				)
				cone := factorioFloat32(
					peak - factorioFloat32(factorioFloat32(math.Sqrt(distanceSquared))*factorioFloat32(peak/radius)),
				)
				best = math.Max(best, cone)
			}
		}
	}
	return best
}

func (f *factorioRegularResourceField) blobTermAt(x, y float64) float64 {
	blobs := factorioBasisNoise(x/8, y/8, &f.tables) + factorioBasisNoise(x/24, y/24, &f.tables)
	blobs += 1.5 * factorioBasisNoise(x/64, y/64, &f.tables)
	distance := factorioDistanceFromNearestPoint(x, y, f.starts, math.Inf(1))
	return (blobs - 1.0/3.0) * factorioRegularResourceBlobAmplitude(distance, f.params, f.control)
}

func (f *factorioRegularResourceField) regionSpots(regionX, regionY int64) []factorioSelectedSpot {
	key := [2]int64{regionX, regionY}
	if spots, ok := f.regionCache[key]; ok {
		return spots
	}
	quantityBatch := func(candidates []factorioSpotCandidate) []float64 {
		source := make([]float64, len(candidates))
		for index := range source {
			source[index] = f.params.randomSpotSizeMax
		}
		jitter := factorioRandomPenaltyBatch(
			candidates,
			source,
			1,
			f.params.randomSpotSizeMax-f.params.randomSpotSizeMin,
		)
		quantities := make([]float64, len(candidates))
		for index, candidate := range candidates {
			distance := factorioDistanceFromNearestPoint(candidate.x, candidate.y, f.starts, math.Inf(1))
			base := factorioFloat32(factorioRegularResourceQuantityBase(distance, f.params, f.control))
			quantities[index] = factorioFloat32(jitter[index] * base)
		}
		return quantities
	}
	spots := factorioSelectSpots(
		factorioSpotRegionKey{seed0: f.seed0, seed1: f.params.seed1, regionX: regionX, regionY: regionY},
		factorioSpotSelectionParams{
			regionSize:         factorioRegularResourceRegionSize,
			candidateSpotCount: f.params.candidateSpotCount,
			spacing:            factorioRegularResourceSpacing,
			skipSpan:           f.skipSpan,
			skipOffset:         f.skipOffset,
			density: func(x, y float64) float64 {
				distance := factorioDistanceFromNearestPoint(x, y, f.starts, math.Inf(1))
				return factorioRegularResourceDensity(distance, f.params, f.control)
			},
			quantityBatch: quantityBatch,
			favorability: func(float64, float64) float64 {
				return 1
			},
		},
	)
	f.regionCache[key] = spots
	return spots
}

func (f *factorioStartingResourceField) fieldAt(x, y float64) float64 {
	return f.spotFieldAt(x, y) + f.blobTermAt(x, y)
}

func (f *factorioStartingResourceField) spotFieldAt(x, y float64) float64 {
	best := f.basement
	lowX := factorioResourceRegionIndex(x-f.maxCullRadius, factorioStartingResourceRegionSize)
	highX := factorioResourceRegionIndex(x+f.maxCullRadius, factorioStartingResourceRegionSize)
	lowY := factorioResourceRegionIndex(y-f.maxCullRadius, factorioStartingResourceRegionSize)
	highY := factorioResourceRegionIndex(y+f.maxCullRadius, factorioStartingResourceRegionSize)
	for regionY := lowY; regionY <= highY; regionY++ {
		for regionX := lowX; regionX <= highX; regionX++ {
			for _, spot := range f.regionSpots(regionX, regionY) {
				dx := x - spot.x
				dy := y - spot.y
				distanceSquared := dx*dx + dy*dy
				if distanceSquared > f.maxCullRadius*f.maxCullRadius {
					continue
				}
				baseRadius := factorioFloat32(f.params.startingRQFactor * factorioFastCbrt(f.quantity))
				radius := factorioFloat32(baseRadius * spot.coneScale)
				if radius <= 0 {
					continue
				}
				peak := factorioFloat32(
					factorioFloat32(3*spot.quantity) /
						factorioFloat32(factorioFloat32(math.Pi*radius)*radius),
				)
				cone := factorioFloat32(
					peak - factorioFloat32(factorioFloat32(math.Sqrt(distanceSquared))*factorioFloat32(peak/radius)),
				)
				best = math.Max(best, cone)
			}
		}
	}
	return best
}

func (f *factorioStartingResourceField) blobTermAt(x, y float64) float64 {
	blobs := factorioBasisNoise(x/8, y/8, &f.tables) + factorioBasisNoise(x/24, y/24, &f.tables)
	return (blobs - 0.25) * factorioStartingResourceBlobAmplitude(f.params, f.control)
}

func (f *factorioStartingResourceField) regionSpots(regionX, regionY int64) []factorioSelectedSpot {
	key := [2]int64{regionX, regionY}
	if spots, ok := f.regionCache[key]; ok {
		return spots
	}
	spots := factorioSelectSpots(
		factorioSpotRegionKey{seed0: f.seed0, seed1: f.params.seed1 + 1, regionX: regionX, regionY: regionY},
		factorioSpotSelectionParams{
			regionSize:               factorioStartingResourceRegionSize,
			candidateSpotCount:       factorioStartingResourceCandidates,
			spacing:                  factorioStartingResourceSpacing,
			skipSpan:                 f.skipSpan,
			skipOffset:               f.skipOffset,
			hardRegionTargetQuantity: true,
			density: func(x, y float64) float64 {
				distance := factorioDistanceFromNearestPoint(x, y, f.starts, math.Inf(1))
				return factorioStartingResourceDensity(distance, f.params, f.control)
			},
			quantity: func(float64, float64) float64 {
				return f.quantity
			},
			favorability: func(x, y float64) float64 {
				distance := factorioDistanceFromNearestPoint(x, y, f.starts, math.Inf(1))
				elevation := f.nauvis.sample(x, y).elevation
				return factorioStartingResourceFavorability(distance, elevation)
			},
		},
	)
	f.regionCache[key] = spots
	return spots
}

func factorioResourceRegionIndex(coordinate float64, regionSize int64) int64 {
	return int64(math.Floor((coordinate + float64(regionSize)/2) / float64(regionSize)))
}

func factorioRegularResourceDensity(distance float64, params factorioResourceParams, control fastControl) float64 {
	fadeIn := clampFloat(
		(distance-factorioStartingResourceRadius)/factorioResourceFadeInDistance,
		0,
		1,
	)
	doubleUp := 1 + clampFloat(
		(distance-factorioResourceFadeInDistance)/factorioResourceDoubleDensityDistance,
		0,
		1,
	)
	return params.baseDensity * control.frequency * control.size * fadeIn * doubleUp
}

func factorioRegularResourceQuantityBase(distance float64, params factorioResourceParams, control fastControl) float64 {
	return 1_000_000 / params.baseSpotsPerSquareKM / control.frequency *
		factorioRegularResourceDensity(distance, params, control)
}

func factorioRegularResourceSpotHeight(distance float64, params factorioResourceParams, control fastControl) float64 {
	meanSize := (params.randomSpotSizeMin + params.randomSpotSizeMax) / 2
	quantity := meanSize * factorioRegularResourceQuantityBase(distance, params, control)
	return factorioFastCbrt(quantity) / ((math.Pi / 3) * params.regularRQFactor * params.regularRQFactor)
}

func factorioRegularResourceBlobAmplitude(distance float64, params factorioResourceParams, control fastControl) float64 {
	atMaximum := factorioRegularResourceSpotHeight(
		factorioResourceDoubleDensityDistance+factorioResourceFadeInDistance,
		params,
		control,
	)
	atDistance := factorioRegularResourceSpotHeight(distance, params, control)
	return factorioResourceBlobAmplitude * math.Min(atMaximum, atDistance)
}

func factorioStartingResourceAmount(params factorioResourceParams, control fastControl) float64 {
	return 20000 * params.baseDensity * (control.frequency + 1) * control.size
}

func factorioStartingResourceQuantity(params factorioResourceParams, control fastControl) float64 {
	return factorioStartingResourceAmount(params, control) /
		factorioStartingResourceSplit /
		control.frequency
}

func factorioStartingResourceDensity(distance float64, params factorioResourceParams, control fastControl) float64 {
	if distance >= factorioStartingResourceRadius {
		return 0
	}
	return factorioStartingResourceAmount(params, control) /
		(math.Pi * factorioStartingResourceRadius * factorioStartingResourceRadius)
}

func factorioStartingResourceFavorability(distance, elevation float64) float64 {
	originExcluder := 0.0
	if distance > 40 {
		originExcluder = 1
	}
	modulation := 0.0
	if distance < factorioStartingResourceRadius {
		modulation = 1
	}
	return clampFloat((elevation-1)/10, 0, 1)*modulation*originExcluder*2 -
		math.Min(1, distance/factorioStartingResourceRadius)
}

func factorioStartingResourceBlobAmplitude(params factorioResourceParams, control fastControl) float64 {
	return factorioResourceBlobAmplitude /
		((math.Pi / 3) * params.startingRQFactor * params.startingRQFactor) *
		factorioFastCbrt(factorioStartingResourceQuantity(params, control))
}

func factorioResourceBasement(params factorioResourceParams, control fastControl) float64 {
	regular := factorioRegularResourceBlobAmplitude(
		factorioResourceDoubleDensityDistance+factorioResourceFadeInDistance,
		params,
		control,
	)
	starting := factorioStartingResourceBlobAmplitude(params, control)
	return -6 * math.Max(regular, starting)
}

func factorioFloat32(value float64) float64 {
	return float64(float32(value))
}
