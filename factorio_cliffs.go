package main

import (
	"context"
	"image"
	"image/color"
	"math"
)

const (
	factorioCliffGridSize         = 4
	factorioCliffCellCenterX      = 2
	factorioCliffCellCenterY      = 2.5
	factorioCliffLowFrequencySeed = 86883
	factorioNauvisOffsetXSeed     = 593691028
	factorioNauvisOffsetYSeed     = 1415852290
)

var factorioCliffMapColor = color.RGBA{R: 144, G: 119, B: 87, A: 255}

type factorioCliffCorner struct {
	elevation  float64
	cliffiness float64
}

type factorioCliffCell struct {
	factorioPoint
	left   int
	right  int
	top    int
	bottom int
}

type factorioCliffEvaluator struct {
	nauvis        *factorioNauvisEvaluator
	offsetXTables factorioBasisTables
	offsetYTables factorioBasisTables
	lowTables     factorioBasisTables
	smoothing     float64
	interval      float64
	elevation0    float64
	lowFrequency  float64
	cutoff        float64
	enabled       bool
}

func newFactorioCliffEvaluator(
	settings fastPreviewSettings,
	nauvis *factorioNauvisEvaluator,
) *factorioCliffEvaluator {
	if nauvis == nil {
		nauvis = newFactorioNauvisEvaluator(settings)
	}
	frequency := positiveOr(settings.cliffs.frequency, 1)
	continuity := positiveOr(settings.cliffs.size, 1)
	interval := settings.cliffElevationInterval / frequency
	richness := settings.cliffRichness * continuity
	evaluator := &factorioCliffEvaluator{
		nauvis:        nauvis,
		offsetXTables: factorioBasisTablesFromSeed(settings.seed, factorioNauvisOffsetXSeed),
		offsetYTables: factorioBasisTablesFromSeed(settings.seed, factorioNauvisOffsetYSeed),
		lowTables:     factorioBasisTablesFromSeed(settings.seed, factorioCliffLowFrequencySeed),
		interval:      interval,
		smoothing:     settings.cliffSmoothing,
		elevation0:    settings.cliffElevation0,
		enabled: settings.cliffs.enabled && settings.cliffs.size > 0 &&
			settings.cliffRichness > 0 && interval > 0,
	}
	if !evaluator.enabled {
		return evaluator
	}
	cliffFrequency := 40 / interval
	evaluator.lowFrequency = math.Min(
		factorioSliderToLinear(cliffFrequency, -1.7, 1.7),
		factorioSliderToLinear(richness, -1, 1),
	)
	gap := math.Max(0, 0.5-0.5*factorioSliderToLinear(richness, -1, 1))
	evaluator.cutoff = 2 * math.Pow(gap, 1.5)
	return evaluator
}

func (e *factorioCliffEvaluator) cliffElevationAt(x, y float64) float64 {
	hills := math.Abs(e.nauvis.hillsNoise(x, y))
	return 10 + 30*(hills-e.cliffLevelAt(x, y))
}

func (e *factorioCliffEvaluator) cliffLevelAt(x, y float64) float64 {
	nauvisSegmentation := 1.5 * e.nauvis.segmentation
	return clampFloat(
		0.65+0.6*factorioBasisNoise(
			x*nauvisSegmentation/500,
			y*nauvisSegmentation/500,
			&e.nauvis.cliffLevelTables,
		),
		0.15,
		1.15,
	)
}

func (e *factorioCliffEvaluator) cliffinessAt(x, y float64) float64 {
	if !e.enabled {
		return 0
	}
	hills := math.Abs(e.nauvis.hillsNoise(x, y))
	return e.cliffinessFromHills(x, y, hills)
}

func (e *factorioCliffEvaluator) cliffinessFromHills(x, y, hills float64) float64 {
	nauvisSegmentation := 1.5 * e.nauvis.segmentation
	offsetScale := nauvisSegmentation / 500
	rawX := factorioBasisNoise(x*offsetScale, y*offsetScale, &e.offsetXTables)
	rawY := factorioBasisNoise(x*offsetScale, y*offsetScale, &e.offsetYTables)
	normalizedX := rawX / math.Sqrt(0.001+rawX*rawX+rawY*rawY)
	normalizedY := rawY / math.Sqrt(0.001+rawY*rawY+rawX*rawX)
	hillsOffset := math.Abs(e.nauvis.hillsNoise(x+12*normalizedX, y+12*normalizedY))
	ringbreak := math.Abs(hills - hillsOffset)

	base := (ringbreak - 0.01) * 60
	forest := (math.Abs(e.nauvis.forestPathNoise(x, y)) - 0.03) * 12
	bridge := (math.Abs(e.nauvis.bridgeNoise(x, y)) - 0.05) * 15
	elevation := (e.nauvis.elevationNoCliff(x, y) - 4) / 2
	distance := factorioDistanceFromNearestPoint(x, y, e.nauvis.starts, math.Inf(1))
	startingArea := -2 + distance*e.nauvis.segmentation/120
	low := 1.5 + 0.51*factorioBasisNoise(
		x*nauvisSegmentation/500,
		y*nauvisSegmentation/500,
		&e.lowTables,
	) + e.lowFrequency
	main := math.Min(
		math.Min(base, forest),
		math.Min(math.Min(bridge, elevation), math.Min(startingArea, 4*low)),
	)
	if main >= e.cutoff {
		return 10
	}
	return 0
}

func (e *factorioCliffEvaluator) cornerAt(x, y float64) factorioCliffCorner {
	hills := math.Abs(e.nauvis.hillsNoise(x, y))
	return factorioCliffCorner{
		elevation:  10 + 30*(hills-e.cliffLevelAt(x, y)),
		cliffiness: e.cliffinessFromHills(x, y, hills),
	}
}

func factorioCrossesCliff(a, b, cliffAverage, elevation0, interval float64) int {
	if a < 0 || b < 0 {
		return 0
	}
	boundary := elevation0 + interval*math.Floor((math.Max(a, b)-elevation0)/interval)
	if boundary < elevation0 || cliffAverage <= 0.5 {
		return 0
	}
	deltaA := a - boundary
	deltaB := b - boundary
	if deltaA < 0 && deltaB > 0 {
		return 1
	}
	if deltaA > 0 && deltaB < 0 {
		return -1
	}
	return 0
}

func factorioCliffCodePlaces(code int) bool {
	switch code {
	case 1, 3, 4, 5, 12, 15, 16, 17, 28, 48, 51, 52, 64, 67, 68, 80,
		192, 193, 204, 240:
		return true
	default:
		return false
	}
}

func (e *factorioCliffEvaluator) placedCells(
	ctx context.Context,
	x0, y0, x1, y1 float64,
) ([]factorioCliffCell, error) {
	if !e.enabled {
		return nil, nil
	}
	return factorioPlaceCliffs(ctx, x0, y0, x1, y1, e.elevation0, e.interval, e.smoothing, e.cornerAt, nil)
}

func renderFactorioCliffs(
	ctx context.Context,
	img *image.RGBA,
	settings fastPreviewSettings,
	evaluator *factorioCliffEvaluator,
	originX, originY, tilesPerPixel float64,
) error {
	if !evaluator.enabled {
		return nil
	}
	bounds := img.Bounds()
	queryPadding := 4.0
	cells, err := evaluator.placedCells(
		ctx,
		originX-queryPadding,
		originY-queryPadding,
		originX+float64(bounds.Dx())*tilesPerPixel+queryPadding,
		originY+float64(bounds.Dy())*tilesPerPixel+queryPadding,
	)
	if err != nil {
		return err
	}
	paintRect := func(x0, y0, x1, y1 float64) {
		minPixelX, maxPixelX := factorioCliffPixelSpan(x0, x1, originX, tilesPerPixel)
		minPixelY, maxPixelY := factorioCliffPixelSpan(y0, y1, originY, tilesPerPixel)
		for pixelY := minPixelY; pixelY <= maxPixelY; pixelY++ {
			if pixelY < 0 || pixelY >= bounds.Dy() {
				continue
			}
			worldY := math.Floor(originY + float64(pixelY)*tilesPerPixel)
			for pixelX := minPixelX; pixelX <= maxPixelX; pixelX++ {
				if pixelX < 0 || pixelX >= bounds.Dx() {
					continue
				}
				worldX := math.Floor(originX + float64(pixelX)*tilesPerPixel)
				if fastOutOfMapBounds(settings, worldX, worldY) {
					continue
				}
				offset := pixelY*img.Stride + pixelX*4
				base := color.RGBA{R: img.Pix[offset], G: img.Pix[offset+1], B: img.Pix[offset+2], A: 255}
				if factorioPreviewWaterColor(base) {
					continue
				}
				img.Pix[offset] = factorioCliffMapColor.R
				img.Pix[offset+1] = factorioCliffMapColor.G
				img.Pix[offset+2] = factorioCliffMapColor.B
			}
		}
	}
	for _, cell := range cells {
		paintRect(factorioCliffChartRect(cell))
	}
	return nil
}

func factorioCliffPixelSpan(lower, upperExclusive, origin, tilesPerPixel float64) (int, int) {
	minPixel := int(math.Ceil((lower - origin) / tilesPerPixel))
	maxPixel := int(math.Ceil((upperExclusive-origin)/tilesPerPixel)) - 1
	// Division can round an integer boundary just above the integer (for
	// example (-20 - -25.6) / .4). Use the same axis as the terrain sampler
	// to keep every pixel of a world tile inside the same chart rectangle.
	axis := newFastPreviewAxis(origin, tilesPerPixel)
	for axis.coordinate(minPixel) < lower {
		minPixel++
	}
	for axis.coordinate(minPixel-1) >= lower {
		minPixel--
	}
	for axis.coordinate(maxPixel) >= upperExclusive {
		maxPixel--
	}
	for axis.coordinate(maxPixel+1) < upperExclusive {
		maxPixel++
	}
	if minPixel > maxPixel {
		centerPixel := int(math.Floor(((lower+upperExclusive)/2 - origin) / tilesPerPixel))
		return centerPixel, centerPixel
	}
	return minPixel, maxPixel
}
