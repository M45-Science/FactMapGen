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
	factorioCliffCornerOffsetY    = 0.5
	factorioCliffMarkRadius       = 2
	factorioCliffLowFrequencySeed = 86883
	factorioNauvisOffsetXSeed     = 593691028
	factorioNauvisOffsetYSeed     = 1415852290
)

var factorioCliffMapColor = color.RGBA{R: 144, G: 119, B: 87, A: 255}

type factorioCliffCorner struct {
	elevation  float64
	cliffiness float64
}

type factorioCliffEvaluator struct {
	nauvis        *factorioNauvisEvaluator
	offsetXTables factorioBasisTables
	offsetYTables factorioBasisTables
	lowTables     factorioBasisTables
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

func factorioCliffCrossingCode(value int) int {
	if value < 0 {
		return 3
	}
	return value
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
) ([]factorioPoint, error) {
	if !e.enabled {
		return nil, nil
	}
	cxMin := int(math.Floor((x0 - factorioCliffCellCenterX) / factorioCliffGridSize))
	cxMax := int(math.Ceil((x1 - factorioCliffCellCenterX) / factorioCliffGridSize))
	cyMin := int(math.Floor((y0 - factorioCliffCellCenterY) / factorioCliffGridSize))
	cyMax := int(math.Ceil((y1 - factorioCliffCellCenterY) / factorioCliffGridSize))
	cornerWidth := cxMax - cxMin + 2
	cornerHeight := cyMax - cyMin + 2
	corners := make([]factorioCliffCorner, cornerWidth*cornerHeight)
	cornerReady := make([]bool, len(corners))
	corner := func(i, j int) factorioCliffCorner {
		index := (j-cyMin)*cornerWidth + i - cxMin
		if cornerReady[index] {
			return corners[index]
		}
		value := e.cornerAt(
			float64(i)*factorioCliffGridSize,
			float64(j)*factorioCliffGridSize+factorioCliffCornerOffsetY,
		)
		corners[index] = value
		cornerReady[index] = true
		return value
	}
	cross := func(a, b factorioCliffCorner) int {
		return factorioCrossesCliff(
			a.elevation,
			b.elevation,
			(a.cliffiness+b.cliffiness)/2,
			e.elevation0,
			e.interval,
		)
	}

	cells := make([]factorioPoint, 0)
	for cy := cyMin; cy <= cyMax; cy++ {
		if cy&31 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		for cx := cxMin; cx <= cxMax; cx++ {
			topLeft := corner(cx, cy)
			bottomLeft := corner(cx, cy+1)
			topRight := corner(cx+1, cy)
			bottomRight := corner(cx+1, cy+1)
			left := cross(topLeft, bottomLeft)
			right := cross(topRight, bottomRight)
			top := cross(topLeft, topRight)
			bottom := cross(bottomLeft, bottomRight)
			code := factorioCliffCrossingCode(left)<<6 |
				factorioCliffCrossingCode(right)<<4 |
				factorioCliffCrossingCode(top)<<2 |
				factorioCliffCrossingCode(bottom)
			if !factorioCliffCodePlaces(code) {
				continue
			}
			worldX := float64(cx)*factorioCliffGridSize + factorioCliffCellCenterX
			worldY := float64(cy)*factorioCliffGridSize + factorioCliffCellCenterY
			if worldX >= x0 && worldX < x1 && worldY >= y0 && worldY < y1 {
				cells = append(cells, factorioPoint{x: worldX, y: worldY})
			}
		}
	}
	return cells, nil
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
	queryPadding := float64(factorioCliffMarkRadius + 1)
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
	for _, cell := range cells {
		minPixelX, maxPixelX := factorioCliffPixelSpan(cell.x, originX, tilesPerPixel)
		minPixelY, maxPixelY := factorioCliffPixelSpan(cell.y, originY, tilesPerPixel)
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
	return nil
}

func factorioCliffPixelSpan(worldCenter, origin, tilesPerPixel float64) (int, int) {
	center := math.Floor(worldCenter)
	lower := center - factorioCliffMarkRadius
	upperExclusive := center + factorioCliffMarkRadius + 1
	return int(math.Ceil((lower - origin) / tilesPerPixel)),
		int(math.Ceil((upperExclusive-origin)/tilesPerPixel)) - 1
}
