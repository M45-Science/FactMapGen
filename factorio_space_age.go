package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
)

type factorioSpaceAgeEvaluator struct {
	settings      fastPreviewSettings
	resource      map[string]func(float64, float64) float64
	vulcanus      *factorioVulcanusEvaluator
	fulgora       *factorioFulgoraEvaluator
	aquilo        *factorioAquiloEvaluator
	gleba         *factorioGlebaEvaluator
	glebaFields   *glebaFeatureFields
	glebaChart    *factorioGlebaChart
	fulgoraChart  *factorioFulgoraChart
	vulcanusChart *factorioVulcanusChart
}

type factorioSpaceAgeSample struct {
	elevation float64
	moisture  float64
	aux       float64
	zone      int
	land      bool
	strength  float64
	code      float64
	resources [4]float64
	gleba     factorioGlebaSample
}

func newFactorioSpaceAgeEvaluator(settings fastPreviewSettings) *factorioSpaceAgeEvaluator {
	evaluator := &factorioSpaceAgeEvaluator{
		settings: settings,
		resource: make(map[string]func(float64, float64) float64),
	}
	if settings.planet == fastPreviewPlanetVulcanus {
		evaluator.vulcanus = newFactorioVulcanusEvaluator(settings, true)
		evaluator.vulcanusChart = &factorioVulcanusChart{world: evaluator, knockout: makeFactorioMultioctaveNoise32(settings.seed, 1300000, 2, .7, 1.0/3, 1, true)}
	}
	if settings.planet == fastPreviewPlanetFulgora {
		evaluator.fulgora = newFactorioFulgoraEvaluator(settings, true)
		evaluator.fulgoraChart = &factorioFulgoraChart{world: evaluator}
	}
	if settings.planet == fastPreviewPlanetAquilo {
		evaluator.aquilo = newFactorioAquiloEvaluator(settings, true)
	}
	if settings.planet == fastPreviewPlanetGleba {
		evaluator.gleba = newFactorioGlebaEvaluator(settings, true)
		evaluator.glebaFields = newGlebaFeatureFields(evaluator.gleba, true)
		evaluator.glebaChart = &factorioGlebaChart{world: evaluator}
	}
	if settings.planet == fastPreviewPlanetGleba {
		control := fastSpaceAgeControl(settings, "gleba_enemy_base")
		evaluator.resource["gleba_enemy_base"] = makeFastSpaceAgeNoise(settings.seed, 0x735000+5*1237, 68/positiveOr(control.frequency, 1))
	}
	return evaluator
}

func fastSpaceAgeControl(settings fastPreviewSettings, name string) fastControl {
	if control, ok := settings.autoplaceControls[name]; ok {
		return control
	}
	return fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
}

func (e *factorioSpaceAgeEvaluator) render(
	ctx context.Context,
	img *image.RGBA,
	settings fastPreviewSettings,
	originX, originY, tilesPerPixel float64,
) error {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	cliffPixels, err := e.cliffPixels(ctx, width, height, originX, originY, tilesPerPixel)
	if err != nil {
		return err
	}
	xAxis := newFastPreviewAxis(originX, tilesPerPixel)
	yAxis := newFastPreviewAxis(originY, tilesPerPixel)
	renderPixel := func(x, y int, wx, wy float64) {
		offset := y*img.Stride + x*4
		base := e.colorAt(wx, wy, tilesPerPixel, cliffPixels[y*width+x])
		if fastOutOfMapBounds(settings, wx, wy) {
			base = color.RGBA{R: 20, G: 23, B: 20, A: 255}
		}
		img.Pix[offset] = base.R
		img.Pix[offset+1] = base.G
		img.Pix[offset+2] = base.B
		img.Pix[offset+3] = 255
	}
	if tilesPerPixel >= 1 {
		err := parallelFastPreviewRows(ctx, height, func(y int) {
			wy := math.Floor(yAxis.coordinate(y))
			for x := 0; x < width; x++ {
				renderPixel(x, y, math.Floor(xAxis.coordinate(x)), wy)
			}
		})
		if err != nil {
			return err
		}
		if e.aquilo != nil {
			return e.aquilo.renderEntities(ctx, img, originX, originY, tilesPerPixel)
		}
		if e.glebaChart != nil && tilesPerPixel <= 2 {
			return e.glebaChart.render(ctx, img, originX, originY, tilesPerPixel)
		}
		if e.fulgoraChart != nil && tilesPerPixel <= 2 {
			return e.fulgoraChart.render(ctx, img, originX, originY, tilesPerPixel)
		}
		if e.vulcanusChart != nil && tilesPerPixel <= 2 {
			return e.vulcanusChart.render(ctx, img, originX, originY, tilesPerPixel)
		}
		return nil
	}

	lastWorldY := math.Inf(-1)
	for y := 0; y < height; y++ {
		if y&31 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		wy := math.Floor(yAxis.coordinate(y))
		row := y * img.Stride
		if y > 0 && wy == lastWorldY {
			copy(img.Pix[row:row+img.Stride], img.Pix[row-img.Stride:row])
			continue
		}
		lastWorldY = wy
		lastWorldX := math.Inf(-1)
		for x := 0; x < width; x++ {
			wx := math.Floor(xAxis.coordinate(x))
			offset := row + x*4
			if x > 0 && wx == lastWorldX {
				copy(img.Pix[offset:offset+4], img.Pix[offset-4:offset])
				continue
			}
			lastWorldX = wx
			renderPixel(x, y, wx, wy)
		}
	}
	if e.aquilo != nil {
		return e.aquilo.renderEntities(ctx, img, originX, originY, tilesPerPixel)
	}
	if e.glebaChart != nil {
		return e.glebaChart.render(ctx, img, originX, originY, tilesPerPixel)
	}
	if e.fulgoraChart != nil {
		return e.fulgoraChart.render(ctx, img, originX, originY, tilesPerPixel)
	}
	if e.vulcanusChart != nil {
		return e.vulcanusChart.render(ctx, img, originX, originY, tilesPerPixel)
	}
	return nil
}

func (e *factorioSpaceAgeEvaluator) colorAt(x, y, tilesPerPixel float64, cliff ...bool) color.RGBA {
	withCliff := func(base color.RGBA) color.RGBA {
		if len(cliff) > 0 && cliff[0] {
			return factorioCliffMapColor
		}
		return base
	}
	switch e.settings.planet {
	case fastPreviewPlanetVulcanus:
		base, sample := e.vulcanusTerrain(x, y)
		return e.vulcanusFeatures(withCliff(base), sample, x, y, tilesPerPixel)
	case fastPreviewPlanetGleba:
		base, sample := e.glebaTerrain(x, y)
		return e.glebaFeatures(withCliff(base), sample, x, y, tilesPerPixel)
	case fastPreviewPlanetFulgora:
		base, sample := e.fulgoraTerrain(x, y)
		return e.fulgoraFeatures(withCliff(base), sample, x, y, tilesPerPixel)
	case fastPreviewPlanetAquilo:
		return e.aquilo.sample(x, y).tile.color
	default:
		return color.RGBA{R: 20, G: 23, B: 20, A: 255}
	}
}

func (e *factorioSpaceAgeEvaluator) vulcanusTerrain(x, y float64) (color.RGBA, factorioSpaceAgeSample) {
	exact := e.vulcanus.sample(x, y)
	zone := 0
	if exact.ashlandsBiome > exact.mountainsBiome && exact.ashlandsBiome > exact.basaltsBiome {
		zone = 1
	} else if exact.mountainsBiome > exact.basaltsBiome {
		zone = 2
	}
	land := exact.tile.name != "lava" && exact.tile.name != "lava-hot"
	sample := factorioSpaceAgeSample{
		elevation: exact.elevation,
		moisture:  exact.moisture,
		aux:       exact.aux,
		zone:      zone,
		land:      land,
		strength:  exact.mountainVolcanoSpots,
		resources: exact.resources,
	}
	return exact.tile.color, sample
}

func (e *factorioSpaceAgeEvaluator) vulcanusFeatures(
	base color.RGBA,
	sample factorioSpaceAgeSample,
	x, y, tilesPerPixel float64,
) color.RGBA {
	if tilesPerPixel <= 2 {
		return base
	}
	if !sample.land {
		return base
	}
	colors := [4]color.RGBA{{97, 85, 149, 255}, {0, 0, 0, 255}, {204, 178, 178, 255}, {198, 198, 25, 255}}
	for i, name := range factorioVulcanusResourceNames {
		control := fastSpaceAgeControl(e.settings, name)
		if !control.enabled || control.size <= 0 || control.richness <= 0 || sample.resources[i] <= 0 {
			continue
		}
		// Resource regions follow the terrain expressions. Discrete entity rolls
		// remain seed-stable approximations of Factorio's shared chunk RNG.
		chance := clampFloat(sample.resources[i]*4, 0, 1)
		if i == 3 {
			chance *= 0.05
		} else if !factorioPreviewEntityDither(x, y, tilesPerPixel) {
			continue
		}
		if fastSpaceAgeEntityChance(e.settings.seed, uint32(0x56524500+i), x, y, tilesPerPixel, chance) {
			return colors[i]
		}
	}
	if sample.zone == 1 && sample.moisture > 0.62 &&
		fastSpaceAgeEntityChance(e.settings.seed, 0x5647524e, x, y, tilesPerPixel, 0.045) {
		return color.RGBA{R: 48, G: 81, B: 45, A: 255}
	}
	return base
}

func (e *factorioSpaceAgeEvaluator) glebaTerrain(x, y float64) (color.RGBA, factorioSpaceAgeSample) {
	terrain := e.gleba.sample(x, y)
	zone := 0
	switch {
	case terrain.elevation > 120:
		zone = 3
	case terrain.elevation > 20:
		zone = 2
	case terrain.elevation > 1:
		zone = 1
	}
	return terrain.tile.color, factorioSpaceAgeSample{
		elevation: float64(terrain.elevation), moisture: float64(terrain.moisture), aux: float64(terrain.aux),
		zone: zone, land: !strings.HasPrefix(terrain.tile.name, "wetland-") && terrain.tile.name != "gleba-deep-lake", gleba: terrain,
	}
}

func (e *factorioSpaceAgeEvaluator) glebaFeatures(
	base color.RGBA,
	sample factorioSpaceAgeSample,
	x, y, tilesPerPixel float64,
) color.RGBA {
	if tilesPerPixel <= 2 {
		return e.glebaEnemies(base, x, y, tilesPerPixel)
	}
	fields := e.glebaFields.sample(sample.gleba, x, y)
	stone := fastSpaceAgeControl(e.settings, "gleba_stone")
	if sample.land && stone.enabled && fields.stoneProbability > 0 && factorioPreviewEntityDither(x, y, tilesPerPixel) {
		return color.RGBA{R: 175, G: 155, B: 108, A: 255}
	}
	// Stromatolites use ground_tile collision, so they belong in wetlands.
	if !sample.land && sample.gleba.tile.name != "gleba-deep-lake" && e.settings.rocks.enabled {
		prob := max(fields.stromatolites[0], fields.stromatolites[1])
		if fastSpaceAgeEntityChance(e.settings.seed, 0x5354524f, x, y, tilesPerPixel, float64(prob)) {
			return color.RGBA{129, 105, 78, 255}
		}
	}
	plants := fastSpaceAgeControl(e.settings, "gleba_plants")
	if plants.enabled && base != factorioCliffMapColor {
		winner, probability := -1, float32(0)
		for i, p := range fields.plants {
			allowed := sample.land
			switch {
			case i == 0:
				allowed = strings.Contains(sample.gleba.tile.name, "yumako")
			case i == 1:
				allowed = strings.Contains(sample.gleba.tile.name, "jellynut")
			case i >= 8:
				allowed = !sample.land && sample.gleba.tile.name != "gleba-deep-lake"
			}
			if allowed && p > probability {
				winner, probability = i, p
			}
		}
		if winner >= 0 && fastSpaceAgeEntityChance(e.settings.seed, 0x504c414e, x, y, tilesPerPixel, float64(probability)) {
			if winner < 2 {
				return color.RGBA{255, 255, 255, 255}
			}
			return blendFactorioTrees(base, 1)
		}
	}
	return e.glebaEnemies(base, x, y, tilesPerPixel)
}

func (e *factorioSpaceAgeEvaluator) glebaEnemies(base color.RGBA, x, y, tilesPerPixel float64) color.RGBA {
	start := e.startingPoint()
	enemy := fastSpaceAgeControl(e.settings, "gleba_enemy_base")
	distance := math.Hypot(x-start.x, y-start.y)
	if !e.settings.noEnemies && enemy.enabled && distance > 220*positiveOr(e.settings.startingArea, 1) {
		field := e.resource["gleba_enemy_base"](x, y)
		if field > 0.68-0.08*math.Log2(positiveOr(enemy.size, 1)) &&
			fastSpaceAgeEntityChance(e.settings.seed, 0x50454e54, x, y, tilesPerPixel, 0.018*enemy.frequency) {
			return color.RGBA{R: 255, G: 25, B: 25, A: 255}
		}
	}
	return base
}

func (e *factorioSpaceAgeEvaluator) fulgoraTerrain(x, y float64) (color.RGBA, factorioSpaceAgeSample) {
	sample := e.fulgora.sample(x, y)
	return sample.tile.color, factorioSpaceAgeSample{elevation: float64(sample.elevation), land: sample.tile.name[:3] != "oil", strength: float64(sample.scrapProbability)}
}

func (e *factorioSpaceAgeEvaluator) fulgoraFeatures(base color.RGBA, sample factorioSpaceAgeSample, x, y, tilesPerPixel float64) color.RGBA {
	if tilesPerPixel <= 2 {
		return base
	}
	if !sample.land {
		return base
	}
	if sample.strength > 0 && factorioPreviewEntityDither(x, y, tilesPerPixel) && fastSpaceAgeEntityChance(e.settings.seed, 0x46534352, x, y, tilesPerPixel, sample.strength) {
		return color.RGBA{229, 229, 229, 255}
	}
	return base
}

func (e *factorioSpaceAgeEvaluator) startingPoint() factorioPoint {
	if len(e.settings.startingPositions) > 0 {
		return e.settings.startingPositions[0]
	}
	return factorioPoint{}
}

func makeFastSpaceAgeNoise(seed, salt uint32, scale float64) func(float64, float64) float64 {
	if scale <= 0 {
		scale = 1
	}
	return func(x, y float64) float64 {
		x /= scale
		y /= scale
		ix := int64(math.Floor(x))
		iy := int64(math.Floor(y))
		fx := x - float64(ix)
		fy := y - float64(iy)
		fx = fx * fx * (3 - 2*fx)
		fy = fy * fy * (3 - 2*fy)
		bottom := lerpFloat(
			2*fastHashUnit(seed, salt, ix, iy)-1,
			2*fastHashUnit(seed, salt, ix+1, iy)-1,
			fx,
		)
		top := lerpFloat(
			2*fastHashUnit(seed, salt, ix, iy+1)-1,
			2*fastHashUnit(seed, salt, ix+1, iy+1)-1,
			fx,
		)
		return lerpFloat(bottom, top, fy)
	}
}

func fastSpaceAgeEntityChance(
	seed, salt uint32,
	x, y, tilesPerPixel, chance float64,
) bool {
	coverage := clampFloat(chance*max(1, tilesPerPixel*tilesPerPixel), 0, 1)
	return fastHashUnit(seed, salt, int64(math.Floor(x)), int64(math.Floor(y))) < coverage
}

func fastSpaceAgePositiveMod(value, divisor int64) int64 {
	remainder := value % divisor
	if remainder < 0 {
		remainder += divisor
	}
	return remainder
}
