// SPDX-License-Identifier: AGPL-3.0-or-later
// Spot-cone arithmetic follows FactorioMapWebUI spot_field.rs; see
// docs/upstream-rendering.md for provenance. Placement is verified separately
// against the installed Factorio 2.1.23 binary and captured preview images.
package main

import (
	"context"
	"image"
	"image/color"
	"math"
)

const (
	factorioEnemyRegionSize     = int64(512)
	factorioEnemyCandidateCount = 100
	// The compiler takes half sqrt(integer region_area / candidate_count),
	// then stores the default spacing as a float. Here: float32(sqrt(2621)/2).
	factorioEnemyCandidateSpacing = 25.597850799560547
	factorioEnemyNoiseSeed        = uint32(123)
	factorioEnemyCullRadius       = 128.0
	factorioEnemySpawnerCell      = int64(20)
	factorioEnemyWormCell         = int64(8)
)

var factorioEnemyMapColor = color.RGBA{R: 255, G: 25, B: 25, A: 255}

type factorioEnemySpot struct {
	x      float64
	y      float64
	radius float64
	peak   float64
}

type factorioEnemyEvaluator struct {
	seed               uint32
	grid               bool
	control            fastControl
	starts             []factorioPoint
	startingAreaRadius float64
	noiseTables        factorioBasisTables
	regionCache        map[[2]int64][]factorioEnemySpot
}

func newFactorioEnemyEvaluator(settings fastPreviewSettings, grid ...bool) *factorioEnemyEvaluator {
	starts := settings.startingPositions
	if len(starts) == 0 {
		starts = []factorioPoint{{}}
	}
	return &factorioEnemyEvaluator{
		seed:               settings.seed,
		grid:               len(grid) > 0 && grid[0],
		control:            settings.enemyBases,
		starts:             starts,
		startingAreaRadius: factorioEnemyStartingAreaRadius(settings.startingArea),
		noiseTables:        factorioBasisTablesFromSeed(settings.seed, factorioEnemyNoiseSeed),
		regionCache:        make(map[[2]int64][]factorioEnemySpot),
	}
}

func factorioEnemyStartingAreaRadius(startingArea float64) float64 {
	return 150 * math.Sqrt(clampFloat(positiveOr(startingArea, 1), 1.0/6.0, 6))
}

func (e *factorioEnemyEvaluator) intensityAtDistance(distance float64) float64 {
	return clampFloat(distance, 0, 2400) / 325
}

func (e *factorioEnemyEvaluator) radiusAtDistance(distance float64) float64 {
	return math.Sqrt(max(0, e.control.size)) * (15 + 4*e.intensityAtDistance(distance))
}

func (e *factorioEnemyEvaluator) frequencyAtDistance(distance float64) float64 {
	return (0.00001 + 0.000003*e.intensityAtDistance(distance)) * max(0, e.control.frequency)
}

func (e *factorioEnemyEvaluator) populationSelectionsAtDistance(distance float64) int {
	return min(3, 1+int(e.intensityAtDistance(distance)/3))
}

func (e *factorioEnemyEvaluator) spotQuantityAtDistance(distance float64) float64 {
	radius := e.radiusAtDistance(distance)
	return math.Pi / 90 * radius * radius * radius
}

func (e *factorioEnemyEvaluator) baseProbabilityAt(x, y float64) float64 {
	probability, _ := e.baseProbabilityAndDistanceAt(x, y)
	return probability
}

func (e *factorioEnemyEvaluator) baseProbabilityAndDistanceAt(x, y float64) (float64, float64) {
	fx, fy := float32(x), float32(y)
	distance := float32(math.Inf(1))
	for _, p := range e.starts {
		dx, dy := fx-float32(p.x), fy-float32(p.y)
		distance = min(distance, noiseSqrt(float32(dx*dx)+float32(dy*dy)))
	}
	radius := float32(math.Sqrt(max(0, e.control.size))) * (15 + 4*(min(distance, 2400)/325))
	if radius <= 0 {
		return -1000, float64(distance)
	}
	blobs := factorioBasisNoise32(fx*(1.0/8), fy*(1.0/8), &e.noiseTables, e.grid) +
		factorioBasisNoise32(fx*(1.0/24), fy*(1.0/24), &e.noiseTables, e.grid) +
		2*factorioBasisNoise32(fx*(1.0/64), fy*(1.0/64), &e.noiseTables, e.grid)
	blobTerm := (blobs - 0.5) * radius / 150 *
		(0.1 + 0.9*min(distance/3000, 1))
	startingArea := min(0, float32(20/e.startingAreaRadius)*distance-20)
	return float64(float32(e.spotFieldAt(x, y)) + blobTerm - .3 + startingArea), float64(distance)
}

func (e *factorioEnemyEvaluator) autoplaceSourceAt(x, y, distanceFactor float64) float64 {
	probability, distance := e.baseProbabilityAndDistanceAt(x, y)
	return e.autoplaceSourceAtPoint(probability, distance, distanceFactor)
}

func (e *factorioEnemyEvaluator) autoplaceSourceAtPoint(probability, distance, distanceFactor float64) float64 {
	df := float32(distanceFactor)
	distanceRamp := max(float32(0), 1+float32(.002*df)*(-312*df-float32(e.startingAreaRadius)+float32(distance)))
	return float64(min(float32(probability)*distanceRamp, .25+df*.05))
}

func (e *factorioEnemyEvaluator) spotFieldAt(x, y float64) float64 {
	best := -1000.0
	lowX := factorioResourceRegionIndex(x-factorioEnemyCullRadius, factorioEnemyRegionSize)
	highX := factorioResourceRegionIndex(x+factorioEnemyCullRadius, factorioEnemyRegionSize)
	lowY := factorioResourceRegionIndex(y-factorioEnemyCullRadius, factorioEnemyRegionSize)
	highY := factorioResourceRegionIndex(y+factorioEnemyCullRadius, factorioEnemyRegionSize)
	for regionY := lowY; regionY <= highY; regionY++ {
		for regionX := lowX; regionX <= highX; regionX++ {
			for _, spot := range e.regionSpots(regionX, regionY) {
				dx := float32(x) - float32(spot.x)
				dy := float32(y) - float32(spot.y)
				distance := noiseSqrt(float32(dx*dx) + float32(dy*dy))
				if distance > factorioEnemyCullRadius {
					continue
				}
				cone := float32(spot.peak) - float32(distance*float32(float32(spot.peak)/float32(spot.radius)))
				best = max(best, float64(cone))
			}
		}
	}
	return best
}

func (e *factorioEnemyEvaluator) regionSpots(regionX, regionY int64) []factorioEnemySpot {
	key := [2]int64{regionX, regionY}
	if spots, ok := e.regionCache[key]; ok {
		return spots
	}
	selected := factorioSelectSpots(
		factorioSpotRegionKey{
			seed0: e.seed, seed1: factorioEnemyNoiseSeed,
			regionX: regionX, regionY: regionY,
		},
		factorioSpotSelectionParams{
			regionSize:         factorioEnemyRegionSize,
			candidateSpotCount: factorioEnemyCandidateCount,
			spacing:            factorioEnemyCandidateSpacing,
			density: func(x, y float64) float64 {
				distance := factorioDistanceFromNearestPoint(x, y, e.starts, math.Inf(1))
				return e.spotQuantityAtDistance(distance) * e.frequencyAtDistance(distance)
			},
			quantity: func(x, y float64) float64 {
				distance := factorioDistanceFromNearestPoint(x, y, e.starts, math.Inf(1))
				return e.spotQuantityAtDistance(distance)
			},
			favorability: func(float64, float64) float64 { return 1 },
		},
	)
	spots := make([]factorioEnemySpot, 0, len(selected))
	for _, spot := range selected {
		distance := factorioDistanceFromNearestPoint(spot.x, spot.y, e.starts, math.Inf(1))
		radius := float64(float32(min(factorioEnemyCullRadius, e.radiusAtDistance(distance))))
		if radius <= 0 {
			continue
		}
		spots = append(spots, factorioEnemySpot{
			x: spot.x, y: spot.y, radius: radius,
			// The engine's spot cone uses 3.1416, distinct from Lua's pi in
			// the quantity expression; peak and slope are then float32.
			peak: float64(float32(3 * float64(float32(spot.quantity)) / (3.1416 * radius * radius))),
		})
	}
	e.regionCache[key] = spots
	return spots
}

func (e *factorioEnemyEvaluator) trimRegionCache(maxRegions int) {
	if maxRegions < 1 {
		maxRegions = 1
	}
	if len(e.regionCache) > maxRegions {
		e.regionCache = make(map[[2]int64][]factorioEnemySpot)
	}
}

type factorioEnemyPrototype struct {
	distanceFactor float64
	seed           int64
}

var factorioEnemySpawners = [...]factorioEnemyPrototype{
	{distanceFactor: 0, seed: 6},
	{distanceFactor: 0, seed: 7},
}

var factorioEnemyWorms = [...]factorioEnemyPrototype{
	{distanceFactor: 0, seed: 2},
	{distanceFactor: 2, seed: 3},
	{distanceFactor: 5, seed: 4},
	{distanceFactor: 8, seed: 5},
}

var factorioEnemyOverviewPrototypes = [...]factorioEnemyPrototype{
	{distanceFactor: 0, seed: 6},
	{distanceFactor: 0, seed: 7},
	{distanceFactor: 0, seed: 2},
	{distanceFactor: 2, seed: 3},
	{distanceFactor: 5, seed: 4},
	{distanceFactor: 8, seed: 5},
}

type factorioEnemyPlacementCandidate struct {
	value float64
	x     int64
	y     int64
}

// random_penalty is a batch operation: only positive sources consume a draw,
// and the seed uses the first (possibly translated) position of that batch.
func (e *factorioEnemyEvaluator) prototypeValues(positions []factorioSpotCandidate, prototype factorioEnemyPrototype) []float64 {
	sources := make([]float64, len(positions))
	translated := make([]factorioSpotCandidate, len(positions))
	for i, p := range positions {
		sources[i] = e.autoplaceSourceAt(p.x, p.y, prototype.distanceFactor)
		translated[i] = factorioSpotCandidate{x: p.x + float64(prototype.seed), y: p.y}
	}
	return factorioRandomPenaltyBatch(translated, sources, 1, .1)
}

// The preview rejects overlapping enemies in reverse candidate order. These
// bounds use map-generation boxes, whereas the painted rectangles use the
// smaller collision boxes. Touching edges count as a collision.
func factorioEnemyChartAccept(accepted *[][4]float64, ent factorioChartEntity, box [4]float64) bool {
	for i := range box {
		box[i] = math.Trunc(box[i]*256) / 256
		if i&1 == 0 {
			box[i] += ent.x
		} else {
			box[i] += ent.y
		}
	}
	for _, other := range *accepted {
		if box[0] <= other[2] && other[0] <= box[2] && box[1] <= other[3] && other[1] <= box[3] {
			return false
		}
	}
	*accepted = append(*accepted, box)
	return true
}

func (e *factorioEnemyEvaluator) prototypeValueAt(tileX, tileY int64, prototype factorioEnemyPrototype) float64 {
	probability, distance := e.baseProbabilityAndDistanceAt(float64(tileX), float64(tileY))
	return e.prototypeValueAtPoint(tileX, tileY, probability, distance, prototype)
}

func (e *factorioEnemyEvaluator) prototypeValueAtPoint(
	tileX, tileY int64,
	probability, distance float64,
	prototype factorioEnemyPrototype,
) float64 {
	source := e.autoplaceSourceAtPoint(probability, distance, prototype.distanceFactor)
	if source <= 0 {
		return source
	}
	return factorioRandomPenaltyAt(tileX+prototype.seed, tileY, source, 0.1)
}

func factorioRandomPenaltyAt(x, y int64, source, amplitude float64) float64 {
	chunkX := factorioFloorDiv(x, factorioChunkSize)
	chunkY := factorioFloorDiv(y, factorioChunkSize)
	originX := chunkX * factorioChunkSize
	originY := chunkY * factorioChunkSize
	word := uint32(factorioSpotSeedBase) +
		uint32(int32(originX))*factorioSpotRegionXPrime +
		uint32(int32(originY+1))*factorioSpotRegionYPrime
	if word < factorioMinSeedWord {
		word = factorioMinSeedWord
	}
	localX := x - originX
	localY := y - originY
	index := localY*factorioChunkSize + localX
	reverseDraw := int(factorioChunkSize*factorioChunkSize - 1 - index)
	random := factorioTaus88RandomAt(word, reverseDraw)
	return source - amplitude*float64(random)/4294967296.0
}

func renderFactorioEnemies(
	ctx context.Context,
	img *image.RGBA,
	settings fastPreviewSettings,
	evaluator *factorioEnemyEvaluator,
	originX, originY, tilesPerPixel float64,
) error {
	if evaluator == nil || settings.noEnemies || !settings.enemyBases.enabled {
		return nil
	}
	if tilesPerPixel > 2 {
		return renderFactorioEnemyOverview(ctx, img, settings, evaluator, originX, originY, tilesPerPixel)
	}
	if err := renderFactorioEnemyGrid(
		ctx, img, settings, evaluator, originX, originY, tilesPerPixel,
		factorioEnemySpawnerCell, 4, 2, factorioEnemySpawners[:], false,
	); err != nil {
		return err
	}
	return renderFactorioEnemyGrid(
		ctx, img, settings, evaluator, originX, originY, tilesPerPixel,
		factorioEnemyWormCell, 2, 1, factorioEnemyWorms[:], true,
	)
}

func renderFactorioEnemyGrid(
	ctx context.Context,
	img *image.RGBA,
	settings fastPreviewSettings,
	evaluator *factorioEnemyEvaluator,
	originX, originY, tilesPerPixel float64,
	cellSize int64,
	samples int,
	footprintRadius int64,
	prototypes []factorioEnemyPrototype,
	skipOccupied bool,
) error {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	halo := float64(footprintRadius + 1)
	minTileX := int64(math.Floor(originX - halo))
	maxTileX := int64(math.Ceil(originX+float64(width)*tilesPerPixel+halo)) - 1
	minTileY := int64(math.Floor(originY - halo))
	maxTileY := int64(math.Ceil(originY+float64(height)*tilesPerPixel+halo)) - 1
	minCellX := factorioFloorDiv(minTileX, cellSize)
	maxCellX := factorioFloorDiv(maxTileX, cellSize)
	minCellY := factorioFloorDiv(minTileY, cellSize)
	maxCellY := factorioFloorDiv(maxTileY, cellSize)
	for cellY := minCellY; cellY <= maxCellY; cellY++ {
		if cellY&15 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		for cellX := minCellX; cellX <= maxCellX; cellX++ {
			var candidates [4]factorioEnemyPlacementCandidate
			candidateCount := 0
			for sample := 0; sample < samples; sample++ {
				tileX := cellX*cellSize + int64(fastHashUnit(evaluator.seed, uint32(0x454e5800+sample), cellX, cellY)*float64(cellSize))
				tileY := cellY*cellSize + int64(fastHashUnit(evaluator.seed, uint32(0x454e5900+sample), cellX, cellY)*float64(cellSize))
				if fastOutOfMapBounds(settings, float64(tileX), float64(tileY)) {
					continue
				}
				probability, distance := evaluator.baseProbabilityAndDistanceAt(float64(tileX), float64(tileY))
				bestValue := 0.0
				for _, prototype := range prototypes {
					value := evaluator.prototypeValueAtPoint(tileX, tileY, probability, distance, prototype)
					if value > bestValue {
						bestValue = value
					}
				}
				if bestValue > 0 {
					candidates[candidateCount] = factorioEnemyPlacementCandidate{value: bestValue, x: tileX, y: tileY}
					candidateCount++
				}
			}
			if candidateCount == 0 {
				continue
			}
			for index := 1; index < candidateCount; index++ {
				for current := index; current > 0 && candidates[current].value > candidates[current-1].value; current-- {
					candidates[current], candidates[current-1] = candidates[current-1], candidates[current]
				}
			}
			centerX := float64(cellX*cellSize) + float64(cellSize)/2
			centerY := float64(cellY*cellSize) + float64(cellSize)/2
			distance := factorioDistanceFromNearestPoint(centerX, centerY, evaluator.starts, math.Inf(1))
			selections := min(candidateCount, evaluator.populationSelectionsAtDistance(distance))
			for selection := 0; selection < selections; selection++ {
				candidate := candidates[selection]
				if (skipOccupied || selection > 0) && factorioEnemyCenterOccupied(img, originX, originY, tilesPerPixel, candidate.x, candidate.y) {
					continue
				}
				paintFactorioEnemyFootprint(img, originX, originY, tilesPerPixel, candidate.x, candidate.y, footprintRadius)
			}
		}
	}
	return nil
}

func renderFactorioEnemyOverview(
	ctx context.Context,
	img *image.RGBA,
	settings fastPreviewSettings,
	evaluator *factorioEnemyEvaluator,
	originX, originY, tilesPerPixel float64,
) error {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	rasterOriginX, alignedX := fastPreviewRasterCoordinate(originX, tilesPerPixel)
	rasterOriginY, alignedY := fastPreviewRasterCoordinate(originY, tilesPerPixel)
	step := int64(clampFloat(math.Ceil(tilesPerPixel/2), 1, 8))
	if !alignedX || !alignedY {
		step = 1
		rasterOriginX = 0
		rasterOriginY = 0
	}
	minCellX := factorioFloorDiv(rasterOriginX, step)
	maxCellX := factorioFloorDiv(rasterOriginX+int64(width)-1, step)
	minCellY := factorioFloorDiv(rasterOriginY, step)
	maxCellY := factorioFloorDiv(rasterOriginY+int64(height)-1, step)
	for cellY := minCellY; cellY <= maxCellY; cellY++ {
		if cellY&15 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		for cellX := minCellX; cellX <= maxCellX; cellX++ {
			centerPX := float64(cellX*step-rasterOriginX) + float64(step)/2
			centerPY := float64(cellY*step-rasterOriginY) + float64(step)/2
			centerX := originX + centerPX*tilesPerPixel
			centerY := originY + centerPY*tilesPerPixel
			distance := factorioDistanceFromNearestPoint(centerX, centerY, evaluator.starts, math.Inf(1))
			for sample := 0; sample < evaluator.populationSelectionsAtDistance(distance); sample++ {
				rasterX := cellX*step + int64(fastHashUnit(evaluator.seed, uint32(0x454f5800+sample), cellX, cellY)*float64(step))
				rasterY := cellY*step + int64(fastHashUnit(evaluator.seed, uint32(0x454f5900+sample), cellX, cellY)*float64(step))
				px := int(rasterX - rasterOriginX)
				py := int(rasterY - rasterOriginY)
				if px < 0 || px >= width || py < 0 || py >= height {
					continue
				}
				tileX := int64(math.Floor(originX + (float64(px)+0.5)*tilesPerPixel))
				tileY := int64(math.Floor(originY + (float64(py)+0.5)*tilesPerPixel))
				if fastOutOfMapBounds(settings, float64(tileX), float64(tileY)) {
					continue
				}
				probability, sampleDistance := evaluator.baseProbabilityAndDistanceAt(float64(tileX), float64(tileY))
				for _, prototype := range factorioEnemyOverviewPrototypes {
					if evaluator.prototypeValueAtPoint(tileX, tileY, probability, sampleDistance, prototype) > 0 {
						paintFactorioEnemyPixel(img, px, py)
						break
					}
				}
			}
		}
	}
	return nil
}

func factorioEnemyCenterOccupied(
	img *image.RGBA,
	originX, originY, tilesPerPixel float64,
	tileX, tileY int64,
) bool {
	px := int(math.Floor((float64(tileX) - originX) / tilesPerPixel))
	py := int(math.Floor((float64(tileY) - originY) / tilesPerPixel))
	if px < 0 || px >= img.Bounds().Dx() || py < 0 || py >= img.Bounds().Dy() {
		return false
	}
	offset := py*img.Stride + px*4
	return img.Pix[offset] == factorioEnemyMapColor.R &&
		img.Pix[offset+1] == factorioEnemyMapColor.G &&
		img.Pix[offset+2] == factorioEnemyMapColor.B
}

func paintFactorioEnemyFootprint(
	img *image.RGBA,
	originX, originY, tilesPerPixel float64,
	tileX, tileY, radius int64,
) {
	minWorldX := float64(tileX - radius)
	maxWorldX := float64(tileX + radius + 1)
	minWorldY := float64(tileY - radius)
	maxWorldY := float64(tileY + radius + 1)
	minPX := max(0, int(math.Ceil((minWorldX-originX)/tilesPerPixel)))
	maxPX := min(img.Bounds().Dx()-1, int(math.Ceil((maxWorldX-originX)/tilesPerPixel))-1)
	minPY := max(0, int(math.Ceil((minWorldY-originY)/tilesPerPixel)))
	maxPY := min(img.Bounds().Dy()-1, int(math.Ceil((maxWorldY-originY)/tilesPerPixel))-1)
	for py := minPY; py <= maxPY; py++ {
		for px := minPX; px <= maxPX; px++ {
			paintFactorioEnemyPixel(img, px, py)
		}
	}
}

func paintFactorioEnemyPixel(img *image.RGBA, px, py int) {
	offset := py*img.Stride + px*4
	base := color.RGBA{
		R: img.Pix[offset], G: img.Pix[offset+1], B: img.Pix[offset+2], A: 255,
	}
	if factorioPreviewWaterColor(base) {
		return
	}
	img.Pix[offset] = factorioEnemyMapColor.R
	img.Pix[offset+1] = factorioEnemyMapColor.G
	img.Pix[offset+2] = factorioEnemyMapColor.B
}
