package main

import (
	"context"
	"image"
	"image/color"
	"math"
)

const (
	factorioTreeSmallNoiseSeed = 2343395516
	factorioTreeBasisAbsMax    = 1.8
	factorioTreeMaxAlpha       = 0.4
	factorioTreePlacementSalt  = 0x54524545
)

var factorioTreeMapColor = color.RGBA{R: 48, G: 99, B: 48, A: 255}

type factorioTreeSpecies struct {
	name          string
	seed          uint32
	cap           float64
	temperature   [4]float64
	moisture      [4]float64
	inputScaleDiv float64
	outputScale   float64
	sizeOffset    float64
}

var factorioTreeSpeciesCatalog = [...]factorioTreeSpecies{
	{name: "tree_01", seed: 545692666, cap: 0.45, temperature: [4]float64{0, 10, 14, 15}, moisture: [4]float64{0.6, 0.7, 1, 2}, inputScaleDiv: 25, outputScale: 0.8, sizeOffset: 0.5},
	{name: "tree_04", seed: 1357672309, cap: 0.45, temperature: [4]float64{13, 14, 16, 17}, moisture: [4]float64{0.7, 0.9, 1, 2}, inputScaleDiv: 30, outputScale: 0.8, sizeOffset: 0.5},
	{name: "tree_05", seed: 669736931, cap: 0.45, temperature: [4]float64{15, 16, 35, 45}, moisture: [4]float64{0.6, 0.7, 1, 2}, inputScaleDiv: 40, outputScale: 0.8, sizeOffset: 0.45},
	{name: "tree_02", seed: 3113208384, cap: 0.4, temperature: [4]float64{0, 10, 14, 15}, moisture: [4]float64{0.4, 0.5, 0.7, 0.8}, inputScaleDiv: 25, outputScale: 0.75, sizeOffset: 0.5},
	{name: "tree_03", seed: 3465083606, cap: 0.4, temperature: [4]float64{15, 16, 35, 45}, moisture: [4]float64{0.4, 0.5, 0.7, 0.8}, inputScaleDiv: 35, outputScale: 0.75, sizeOffset: 0.5},
	{name: "tree_07", seed: 3387244239, cap: 0.4, temperature: [4]float64{13, 14, 16, 17}, moisture: [4]float64{0.5, 0.6, 0.9, 1}, inputScaleDiv: 40, outputScale: 0.75, sizeOffset: 0.45},
	{name: "tree_02_red", seed: 2142693989, cap: 0.3, temperature: [4]float64{0, 10, 14, 15}, moisture: [4]float64{0.2, 0.3, 0.5, 0.6}, inputScaleDiv: 25, outputScale: 0.7, sizeOffset: 0.5},
	{name: "tree_08", seed: 1499079518, cap: 0.3, temperature: [4]float64{13, 14, 16, 17}, moisture: [4]float64{0.3, 0.4, 0.6, 0.7}, inputScaleDiv: 30, outputScale: 0.7, sizeOffset: 0.5},
	{name: "tree_09", seed: 777851848, cap: 0.3, temperature: [4]float64{15, 16, 35, 45}, moisture: [4]float64{0.2, 0.3, 0.5, 0.6}, inputScaleDiv: 25, outputScale: 0.7, sizeOffset: 0.5},
	{name: "tree_06", seed: 3202485849, cap: 0.2, temperature: [4]float64{0, 10, 14, 15}, moisture: [4]float64{0.1, 0.2, 0.3, 0.4}, inputScaleDiv: 22, outputScale: 0.6, sizeOffset: 0.5},
	{name: "tree_08_brown", seed: 3606254248, cap: 0.2, temperature: [4]float64{13, 14, 16, 17}, moisture: [4]float64{0.2, 0.3, 0.4, 0.5}, inputScaleDiv: 30, outputScale: 0.6, sizeOffset: 0.5},
	{name: "tree_09_brown", seed: 1887705372, cap: 0.2, temperature: [4]float64{15, 16, 35, 45}, moisture: [4]float64{0.1, 0.2, 0.3, 0.4}, inputScaleDiv: 25, outputScale: 0.6, sizeOffset: 0.5},
	{name: "tree_06_brown", seed: 2261543413, cap: 0.1, temperature: [4]float64{0, 10, 14, 15}, moisture: [4]float64{0, 0.1, 0.2, 0.3}, inputScaleDiv: 22, outputScale: 0.5, sizeOffset: 0.5},
	{name: "tree_08_red", seed: 889647812, cap: 0.1, temperature: [4]float64{13, 14, 16, 17}, moisture: [4]float64{0.1, 0.2, 0.3, 0.4}, inputScaleDiv: 30, outputScale: 0.5, sizeOffset: 0.5},
	{name: "tree_09_red", seed: 140958580, cap: 0.1, temperature: [4]float64{15, 16, 35, 45}, moisture: [4]float64{0, 0.1, 0.2, 0.3}, inputScaleDiv: 25, outputScale: 0.5, sizeOffset: 0.5},
}

type factorioTreeSpeciesField struct {
	species  factorioTreeSpecies
	noise    func(float64, float64) float64
	sizeTerm float64
	maxNoise float64
}

type factorioTreePoint struct {
	sample       factorioNauvisSample
	smallNoise   float64
	cutoutFaded  float64
	distanceTerm float64
}

type factorioTreeEvaluator struct {
	seed       uint32
	nauvis     *factorioNauvisEvaluator
	starts     []factorioPoint
	smallNoise func(float64, float64) float64
	fields     []factorioTreeSpeciesField
}

func newFactorioTreeEvaluator(settings fastPreviewSettings, nauvis *factorioNauvisEvaluator) *factorioTreeEvaluator {
	if nauvis == nil {
		nauvis = newFactorioNauvisEvaluator(settings)
	}
	treeFrequency := positiveOr(settings.trees.frequency, 1)
	treeSize := positiveOr(settings.trees.size, 1)
	evaluator := &factorioTreeEvaluator{
		seed:   settings.seed,
		nauvis: nauvis,
		starts: settings.startingPositions,
		smallNoise: makeFactorioMultioctaveNoise(factorioMultioctaveParams{
			seed0: settings.seed, seed1: factorioTreeSmallNoiseSeed, octaves: 3,
			persistence: 0.75, inputScale: 0.2, outputScale: 0.5,
		}),
		fields: make([]factorioTreeSpeciesField, 0, len(factorioTreeSpeciesCatalog)),
	}
	if len(evaluator.starts) == 0 {
		evaluator.starts = []factorioPoint{{}}
	}
	for _, species := range factorioTreeSpeciesCatalog {
		evaluator.fields = append(evaluator.fields, factorioTreeSpeciesField{
			species: species,
			noise: makeFactorioMultioctaveNoise(factorioMultioctaveParams{
				seed0: settings.seed, seed1: species.seed, octaves: 3,
				persistence: 0.65,
				inputScale:  treeFrequency / species.inputScaleDiv,
				outputScale: species.outputScale,
			}),
			sizeTerm: -species.sizeOffset + 0.2*treeSize,
			maxNoise: factorioTreeMaxNoise(species.outputScale),
		})
	}
	return evaluator
}

func factorioTreeMaxNoise(outputScale float64) float64 {
	normalization := factorioMultioctaveNormalization(0.65, 3)
	amplitude := normalization
	total := 0.0
	for range 3 {
		total += amplitude
		amplitude /= 0.65
	}
	return outputScale * total * factorioTreeBasisAbsMax
}

func factorioAsymmetricRamps(input float64, ramp [4]float64) float64 {
	return math.Min(
		(input-ramp[1])/(ramp[1]-ramp[0]),
		(ramp[2]-input)/(ramp[3]-ramp[2]),
	)
}

func (e *factorioTreeEvaluator) pointAt(x, y float64) factorioTreePoint {
	sample := e.nauvis.sample(x, y)
	smallNoise := e.smallNoise(x, y)
	forestPathCutout := math.Min(
		(sample.bridge-0.07)*5,
		math.Min((sample.hills-0.1)*3, (sample.forestPath-0.07)*3),
	)
	return factorioTreePoint{
		sample:      sample,
		smallNoise:  smallNoise,
		cutoutFaded: forestPathCutout*0.3 + smallNoise*0.1,
		distanceTerm: math.Min(
			0,
			factorioDistanceFromNearestPoint(x, y, e.starts, math.Inf(1))/20-3,
		),
	}
}

func factorioTreeCheap(field factorioTreeSpeciesField, point factorioTreePoint) float64 {
	climate := math.Min(
		0,
		math.Min(
			factorioAsymmetricRamps(point.sample.temperature, field.species.temperature),
			factorioAsymmetricRamps(point.sample.moisture, field.species.moisture),
		),
	)
	return climate + point.distanceTerm + field.sizeTerm + point.smallNoise*0.1
}

func (e *factorioTreeEvaluator) speciesValueAt(index int, x, y float64) float64 {
	point := e.pointAt(x, y)
	field := e.fields[index]
	return math.Min(
		field.species.cap,
		math.Min(point.cutoutFaded, factorioTreeCheap(field, point)+field.noise(x, y)),
	)
}

func (e *factorioTreeEvaluator) densityAt(x, y float64) float64 {
	point := e.pointAt(x, y)
	miss := 1.0
	for _, field := range e.fields {
		cheap := factorioTreeCheap(field, point)
		if cheap+field.maxNoise <= 0 {
			continue
		}
		value := math.Min(
			field.species.cap,
			math.Min(point.cutoutFaded, cheap+field.noise(x, y)),
		)
		probability := clampFloat(value, 0, 1)
		if probability > 0 {
			miss *= 1 - probability
		}
	}
	return 1 - miss
}

func (e *factorioTreeEvaluator) placedAt(x, y float64) bool {
	tileX := int64(math.Floor(x))
	tileY := int64(math.Floor(y))
	return fastHashUnit(e.seed, factorioTreePlacementSalt, tileX, tileY) < e.densityAt(float64(tileX), float64(tileY))
}

func renderFactorioTrees(
	ctx context.Context,
	img *image.RGBA,
	settings fastPreviewSettings,
	evaluator *factorioTreeEvaluator,
	originX, originY, tilesPerPixel float64,
) error {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	lastWorldY := math.Inf(-1)
	for py := 0; py < height; py++ {
		worldY := math.Floor(originY + float64(py)*tilesPerPixel)
		row := py * img.Stride
		if py > 0 && worldY == lastWorldY {
			copy(img.Pix[row:row+img.Stride], img.Pix[row-img.Stride:row])
			continue
		}
		lastWorldY = worldY
		lastWorldX := math.Inf(-1)
		for px := 0; px < width; px++ {
			worldX := math.Floor(originX + float64(px)*tilesPerPixel)
			offset := row + px*4
			if px > 0 && worldX == lastWorldX {
				copy(img.Pix[offset:offset+4], img.Pix[offset-4:offset])
				continue
			}
			lastWorldX = worldX
			if fastOutOfMapBounds(settings, worldX, worldY) {
				continue
			}
			base := color.RGBA{R: img.Pix[offset], G: img.Pix[offset+1], B: img.Pix[offset+2], A: 255}
			if factorioPreviewWaterColor(base) {
				continue
			}

			minTileX := int64(math.Floor(originX + float64(px)*tilesPerPixel))
			maxTileX := int64(math.Ceil(originX+float64(px+1)*tilesPerPixel)) - 1
			minTileY := int64(math.Floor(originY + float64(py)*tilesPerPixel))
			maxTileY := int64(math.Ceil(originY+float64(py+1)*tilesPerPixel)) - 1
			spanX := max(int64(1), maxTileX-minTileX+1)
			spanY := max(int64(1), maxTileY-minTileY+1)
			samplesX := min(spanX, int64(4))
			samplesY := min(spanY, int64(4))
			placed := int64(0)
			for sampleY := int64(0); sampleY < samplesY; sampleY++ {
				tileY := minTileY + sampleY*spanY/samplesY
				for sampleX := int64(0); sampleX < samplesX; sampleX++ {
					tileX := minTileX + sampleX*spanX/samplesX
					if evaluator.placedAt(float64(tileX), float64(tileY)) {
						placed++
					}
				}
			}
			if placed == 0 {
				continue
			}
			sampleCount := samplesX * samplesY
			tileCount := spanX * spanY
			placed = max(int64(1), int64(math.Round(float64(placed)*float64(tileCount)/float64(sampleCount))))
			alpha := 1 - math.Pow(1-factorioTreeMaxAlpha, float64(placed))
			if alpha <= 0 {
				continue
			}
			alphaByte := int(math.Round(alpha * 255))
			blend := alphaByte + (alphaByte >> 7)
			img.Pix[offset] = uint8(((256-blend)*int(base.R) + blend*int(factorioTreeMapColor.R)) >> 8)
			img.Pix[offset+1] = uint8(((256-blend)*int(base.G) + blend*int(factorioTreeMapColor.G)) >> 8)
			img.Pix[offset+2] = uint8(((256-blend)*int(base.B) + blend*int(factorioTreeMapColor.B)) >> 8)
		}
	}
	return nil
}

func factorioPreviewWaterColor(value color.RGBA) bool {
	return value == factorioTerrainTiles[0].color || value == factorioTerrainTiles[1].color
}
