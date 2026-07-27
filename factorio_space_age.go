package main

import (
	"context"
	"image"
	"image/color"
	"math"
)

type factorioSpaceAgeEvaluator struct {
	settings fastPreviewSettings
	terrain  [8]func(float64, float64) float64
	resource map[string]func(float64, float64) float64
}

type factorioSpaceAgeSample struct {
	elevation float64
	moisture  float64
	aux       float64
	zone      int
	land      bool
	strength  float64
	code      float64
}

var fastVulcanusTerrainColors = [...]color.RGBA{
	{R: 43, G: 43, B: 43, A: 255},
	{R: 48, G: 51, B: 43, A: 255},
	{R: 53, G: 53, B: 53, A: 255},
	{R: 58, G: 48, B: 43, A: 255},
	{R: 129, G: 105, B: 78, A: 255},
	{R: 144, G: 119, B: 87, A: 255},
	{R: 50, G: 50, B: 58, A: 255},
	{R: 67, G: 67, B: 67, A: 255},
}

var fastGlebaTerrainColors = [...]color.RGBA{
	{R: 46, G: 68, B: 48, A: 255},
	{R: 66, G: 82, B: 11, A: 255},
	{R: 114, G: 86, B: 40, A: 255},
	{R: 61, G: 57, B: 30, A: 255},
	{R: 54, G: 15, B: 24, A: 255},
	{R: 115, G: 53, B: 66, A: 255},
	{R: 95, G: 93, B: 88, A: 255},
	{R: 52, G: 55, B: 48, A: 255},
}

var fastFulgoraTerrainColors = [...]color.RGBA{
	{R: 131, G: 85, B: 66, A: 255},
	{R: 120, G: 94, B: 67, A: 255},
	{R: 112, G: 65, B: 50, A: 255},
	{R: 125, G: 71, B: 59, A: 255},
	{R: 114, G: 75, B: 65, A: 255},
}

var fastAquiloTerrainColors = [...]color.RGBA{
	{R: 100, G: 135, B: 177, A: 255},
	{R: 168, G: 188, B: 211, A: 255},
	{R: 179, G: 185, B: 192, A: 255},
	{R: 166, G: 174, B: 185, A: 255},
	{R: 156, G: 166, B: 181, A: 255},
	{R: 190, G: 194, B: 197, A: 255},
}

func newFactorioSpaceAgeEvaluator(settings fastPreviewSettings) *factorioSpaceAgeEvaluator {
	evaluator := &factorioSpaceAgeEvaluator{
		settings: settings,
		resource: make(map[string]func(float64, float64) float64),
	}
	scales := [8]float64{220, 180, 145, 58, 34, 52, 40, 24}
	switch settings.planet {
	case fastPreviewPlanetVulcanus:
		frequency := positiveOr(fastSpaceAgeControl(settings, "vulcanus_volcanism").frequency, 1)
		for index := range scales {
			scales[index] /= frequency
		}
	case fastPreviewPlanetGleba:
		scales = [8]float64{175, 145, 190, 48, 30, 55, 36, 23}
		frequency := positiveOr(settings.water.frequency, 1)
		for index := range scales {
			scales[index] /= frequency
		}
	case fastPreviewPlanetFulgora:
		scales = [8]float64{180, 95, 24, 32, 18, 55, 38, 21}
	case fastPreviewPlanetAquilo:
		scales = [8]float64{260, 110, 26, 20, 14, 48, 33, 18}
	}
	for index := range evaluator.terrain {
		evaluator.terrain[index] = makeFastSpaceAgeNoise(
			settings.seed, 0x534100+uint32(index)*977, scales[index],
		)
	}
	resourceNames := []string{
		"vulcanus_coal", "tungsten_ore", "calcite", "sulfuric_acid_geyser",
		"gleba_stone", "gleba_enemy_base", "scrap",
		"aquilo_crude_oil", "lithium_brine", "fluorine_vent",
	}
	for index, name := range resourceNames {
		control := fastSpaceAgeControl(settings, name)
		scale := 68 / positiveOr(control.frequency, 1)
		evaluator.resource[name] = makeFastSpaceAgeNoise(
			settings.seed, 0x735000+uint32(index)*1237, scale,
		)
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
	xAxis := newFastPreviewAxis(originX, tilesPerPixel)
	yAxis := newFastPreviewAxis(originY, tilesPerPixel)
	renderPixel := func(x, y int, wx, wy float64) {
		offset := y*img.Stride + x*4
		base := e.colorAt(wx, wy, tilesPerPixel)
		if fastOutOfMapBounds(settings, wx, wy) {
			base = color.RGBA{R: 20, G: 23, B: 20, A: 255}
		}
		img.Pix[offset] = base.R
		img.Pix[offset+1] = base.G
		img.Pix[offset+2] = base.B
		img.Pix[offset+3] = 255
	}
	if tilesPerPixel >= 1 {
		return parallelFastPreviewRows(ctx, height, func(y int) {
			wy := math.Floor(yAxis.coordinate(y))
			for x := 0; x < width; x++ {
				renderPixel(x, y, math.Floor(xAxis.coordinate(x)), wy)
			}
		})
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
	return nil
}

func (e *factorioSpaceAgeEvaluator) colorAt(x, y, tilesPerPixel float64) color.RGBA {
	switch e.settings.planet {
	case fastPreviewPlanetVulcanus:
		base, sample := e.vulcanusTerrain(x, y)
		return e.vulcanusFeatures(base, sample, x, y, tilesPerPixel)
	case fastPreviewPlanetGleba:
		base, sample := e.glebaTerrain(x, y)
		return e.glebaFeatures(base, sample, x, y, tilesPerPixel)
	case fastPreviewPlanetFulgora:
		base, sample := e.fulgoraTerrain(x, y)
		return e.fulgoraFeatures(base, sample, x, y, tilesPerPixel)
	case fastPreviewPlanetAquilo:
		base, sample := e.aquiloTerrain(x, y)
		return e.aquiloFeatures(base, sample, x, y, tilesPerPixel)
	default:
		return color.RGBA{R: 20, G: 23, B: 20, A: 255}
	}
}

func (e *factorioSpaceAgeEvaluator) vulcanusTerrain(x, y float64) (color.RGBA, factorioSpaceAgeSample) {
	mountains := e.terrain[0](x, y)
	ashlands := e.terrain[1](x, y)
	basalts := e.terrain[2](x, y)
	start := e.startingPoint()
	startAngle := fastSpaceAgeSeedAngle(e.settings.seed)
	startScale := 0.7 * positiveOr(e.settings.startingArea, 1)
	mountains += 2.2 * fastSpaceAgeAngledSpot(x, y, start, startAngle+2*math.Pi/3, 230*startScale, 420*startScale)
	ashlands += 2.6 * fastSpaceAgeAngledSpot(x, y, start, startAngle, 150*startScale, 330*startScale)
	basalts += 2.2 * fastSpaceAgeAngledSpot(x, y, start, startAngle+4*math.Pi/3, 230*startScale, 430*startScale)
	zone := 0
	if ashlands > mountains && ashlands > basalts {
		zone = 1
	} else if mountains > basalts {
		zone = 2
	}

	detail := e.terrain[3](x, y)
	plasma := math.Abs(e.terrain[4](x, y) - e.terrain[5](x, y))
	volcanism := fastSpaceAgeControl(e.settings, "vulcanus_volcanism")
	volcano, code := fastSpaceAgeSpotField(
		e.settings.seed, 0x564f4c43, x, y,
		780/positiveOr(volcanism.frequency, 1),
		0.55, 100*positiveOr(volcanism.size, 1), 210*positiveOr(volcanism.size, 1),
	)
	startVolcano := fastSpaceAgeAngledSpot(x, y, start, startAngle+2*math.Pi/3, 420*startScale, 190*positiveOr(volcanism.size, 1))
	volcano = max(volcano, startVolcano)
	distanceFromStart := math.Hypot(x-start.x, y-start.y)
	lava := zone == 0 && plasma < 0.18+0.05*clampFloat(volcanism.size-1, -0.8, 2)
	lava = lava || zone == 2 && volcano > 0.62 && volcano < 0.93
	if distanceFromStart < 76*startScale {
		lava = false
	}

	sample := factorioSpaceAgeSample{
		elevation: 80 + 90*detail,
		moisture:  clampFloat(0.7-0.6*math.Abs(e.terrain[6](x, y)), 0, 1),
		aux:       clampFloat(0.5+0.5*e.terrain[7](x, y), 0, 1),
		zone:      zone,
		land:      !lava,
		strength:  volcano,
		code:      code,
	}
	if zone == 2 {
		sample.elevation = 160 + 380*math.Abs(detail) + 260*volcano
	} else if zone == 1 {
		sample.elevation = 55 + 35*detail
	}
	if lava {
		if plasma < 0.09 || volcano > 0.72 {
			return color.RGBA{R: 255, G: 138, B: 57, A: 255}, sample
		}
		return color.RGBA{R: 150, G: 49, B: 30, A: 255}, sample
	}

	variant := fastSpaceAgeVariant(detail, 3)
	switch zone {
	case 1:
		return fastVulcanusTerrainColors[3+variant], sample
	case 2:
		return fastVulcanusTerrainColors[6+min(variant, 1)], sample
	default:
		return fastVulcanusTerrainColors[min(variant, 2)], sample
	}
}

func (e *factorioSpaceAgeEvaluator) vulcanusFeatures(
	base color.RGBA,
	sample factorioSpaceAgeSample,
	x, y, tilesPerPixel float64,
) color.RGBA {
	if !sample.land {
		return base
	}
	if e.fastCliffAt(sample.elevation, x, y, 120) {
		base = color.RGBA{R: 144, G: 119, B: 87, A: 255}
	}
	start := e.startingPoint()
	angle := fastSpaceAgeSeedAngle(e.settings.seed)
	if e.spaceAgeResourceAt("tungsten_ore", x, y, tilesPerPixel, sample.zone == 0,
		fastSpaceAgeAngledSpot(x, y, start, angle+4*math.Pi/3-0.18, 230, 24)) {
		return color.RGBA{R: 97, G: 85, B: 149, A: 255}
	}
	if e.spaceAgeResourceAt("calcite", x, y, tilesPerPixel, sample.zone == 2,
		fastSpaceAgeAngledSpot(x, y, start, angle+2*math.Pi/3-0.3, 180, 27)) {
		return color.RGBA{R: 204, G: 178, B: 178, A: 255}
	}
	if e.spaceAgeResourceAt("vulcanus_coal", x, y, tilesPerPixel, sample.zone == 1,
		fastSpaceAgeAngledSpot(x, y, start, angle+0.25, 105, 25)) {
		return color.RGBA{A: 255}
	}
	if e.spaceAgeFluidAt("sulfuric_acid_geyser", x, y, tilesPerPixel, sample.zone == 2,
		fastSpaceAgeAngledSpot(x, y, start, angle+2*math.Pi/3+0.28, 285, 32)) {
		return color.RGBA{R: 198, G: 198, B: 25, A: 255}
	}
	if sample.zone == 1 && sample.moisture > 0.62 &&
		fastSpaceAgeEntityChance(e.settings.seed, 0x5647524e, x, y, tilesPerPixel, 0.045) {
		return color.RGBA{R: 48, G: 81, B: 45, A: 255}
	}
	return base
}

func (e *factorioSpaceAgeEvaluator) glebaTerrain(x, y float64) (color.RGBA, factorioSpaceAgeSample) {
	start := e.startingPoint()
	distance := math.Hypot(x-start.x, y-start.y)
	startRadius := 180 * positiveOr(e.settings.startingArea, 1)
	startLift := clampFloat(1-distance/startRadius, 0, 1)
	elevation := 0.72*e.terrain[0](x, y) + 0.28*e.terrain[3](x, y) + 0.55*startLift
	moisture := clampFloat(0.5+0.55*e.terrain[1](x, y)+0.18*e.settings.moistureBias, 0, 1)
	aux := clampFloat(0.5+0.55*e.terrain[2](x, y)+0.18*e.settings.auxBias, 0, 1)
	water := fastSpaceAgeControl(e.settings, "gleba_water")
	waterline := -0.18 + 0.16*math.Log2(positiveOr(water.size, 1))
	sample := factorioSpaceAgeSample{
		elevation: 65 + elevation*95,
		moisture:  moisture,
		aux:       aux,
		land:      elevation > waterline,
	}
	if !sample.land {
		if elevation < waterline-0.28 {
			return color.RGBA{R: 18, G: 37, B: 51, A: 255}, sample
		}
		return color.RGBA{R: 25, G: 49, B: 58, A: 255}, sample
	}

	detail := e.terrain[4](x, y)
	switch {
	case elevation > 0.5:
		sample.zone = 3
		return fastGlebaTerrainColors[6+fastSpaceAgeVariant(detail, 2)], sample
	case elevation > 0.12:
		sample.zone = 2
		return fastGlebaTerrainColors[2+fastSpaceAgeVariant(aux+detail*0.25, 2)], sample
	case moisture > 0.62 && aux < 0.34:
		sample.zone = 1
		if moisture > 0.78 {
			return color.RGBA{R: 132, G: 119, B: 7, A: 255}, sample
		}
		return fastGlebaTerrainColors[fastSpaceAgeVariant(detail, 2)], sample
	case moisture > 0.58 && aux > 0.66:
		sample.zone = 1
		if moisture > 0.78 {
			return color.RGBA{R: 132, G: 7, B: 119, A: 255}, sample
		}
		return fastGlebaTerrainColors[4+fastSpaceAgeVariant(detail, 2)], sample
	default:
		sample.zone = 0
		return fastGlebaTerrainColors[fastSpaceAgeVariant(moisture+detail*0.2, 2)], sample
	}
}

func (e *factorioSpaceAgeEvaluator) glebaFeatures(
	base color.RGBA,
	sample factorioSpaceAgeSample,
	x, y, tilesPerPixel float64,
) color.RGBA {
	if !sample.land {
		return base
	}
	if e.fastCliffAt(sample.elevation, x, y, 60) {
		base = color.RGBA{R: 144, G: 119, B: 87, A: 255}
	}
	start := e.startingPoint()
	stoneStart := fastSpaceAgeAngledSpot(
		x, y, start, fastSpaceAgeSeedAngle(e.settings.seed)+0.6, 145, 24,
	)
	if e.spaceAgeResourceAt("gleba_stone", x, y, tilesPerPixel, sample.zone >= 2, stoneStart) {
		return color.RGBA{R: 175, G: 155, B: 108, A: 255}
	}
	plants := fastSpaceAgeControl(e.settings, "gleba_plants")
	if plants.enabled && sample.moisture > 0.48 {
		chance := 0.035 * positiveOr(plants.frequency, 1) * positiveOr(plants.size, 1)
		if fastSpaceAgeEntityChance(e.settings.seed, 0x504c414e, x, y, tilesPerPixel, chance) {
			if sample.aux > 0.62 {
				return color.RGBA{R: 185, G: 5, B: 166, A: 255}
			}
			if sample.aux < 0.38 {
				return color.RGBA{R: 185, G: 166, B: 5, A: 255}
			}
			return color.RGBA{R: 46, G: 87, B: 48, A: 255}
		}
	}
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
	control := fastSpaceAgeControl(e.settings, "fulgora_islands")
	frequency := positiveOr(control.frequency, 1)
	size := math.Sqrt(positiveOr(control.size, 1))
	large, largeCode := fastSpaceAgeSpotField(
		e.settings.seed, 0x46554c47, x, y,
		245/frequency, 0.78, 42*size, 92*size,
	)
	small, smallCode := fastSpaceAgeSpotField(
		e.settings.seed, 0x46534d4c, x, y,
		112/frequency, 0.24, 12*size, 35*size,
	)
	start := e.startingPoint()
	startingIsland := fastSpaceAgeRadialSpot(x, y, start.x, start.y, 52*size)
	strength := max(large, small, startingIsland)
	if strength > 0 {
		strength += 0.18 * e.terrain[2](x, y)
	}
	code := largeCode
	if small > large {
		code = smallCode
	}
	sample := factorioSpaceAgeSample{
		elevation: strength * 120,
		land:      strength > 0,
		strength:  strength,
		code:      code,
	}
	if !sample.land {
		if e.terrain[0](x, y) > 0 {
			return color.RGBA{R: 74, G: 42, B: 43, A: 255}, sample
		}
		return color.RGBA{R: 56, G: 35, B: 40, A: 255}, sample
	}

	detail := e.terrain[3](x, y)
	variant := fastSpaceAgeVariant(detail+code*0.3, len(fastFulgoraTerrainColors))
	base := fastFulgoraTerrainColors[variant]
	city := (large > 0.28 && largeCode > 0.43) || startingIsland > 0.35
	if city {
		gridX := fastSpaceAgePositiveMod(int64(math.Floor(x)), 14)
		gridY := fastSpaceAgePositiveMod(int64(math.Floor(y)), 14)
		switch {
		case gridX < 2 || gridY < 2:
			base = color.RGBA{R: 114, G: 102, B: 88, A: 255}
		case (gridX/4+gridY/4)&1 == 0:
			base = color.RGBA{R: 104, G: 99, B: 94, A: 255}
		default:
			base = color.RGBA{R: 114, G: 95, B: 90, A: 255}
		}
		if fastSpaceAgeEntityChance(e.settings.seed, 0x46434f4e, x, y, 1, 0.025) {
			base = color.RGBA{R: 0, G: 96, B: 145, A: 255}
		}
	}
	return base, sample
}

func (e *factorioSpaceAgeEvaluator) fulgoraFeatures(
	base color.RGBA,
	sample factorioSpaceAgeSample,
	x, y, tilesPerPixel float64,
) color.RGBA {
	if !sample.land {
		return base
	}
	if sample.strength < 0.08 && e.settings.cliffs.enabled && e.settings.cliffRichness > 0 {
		base = color.RGBA{R: 144, G: 119, B: 87, A: 255}
	}
	start := e.startingPoint()
	scrapStart := fastSpaceAgeRadialSpot(x, y, start.x+22, start.y-16, 28)
	if e.spaceAgeResourceAt("scrap", x, y, tilesPerPixel, sample.strength > 0.22, scrapStart) {
		return color.RGBA{R: 229, G: 229, B: 229, A: 255}
	}
	return base
}

func (e *factorioSpaceAgeEvaluator) aquiloTerrain(x, y float64) (color.RGBA, factorioSpaceAgeSample) {
	large, code := fastSpaceAgeSpotField(
		e.settings.seed, 0x41515549, x, y,
		650, 0.52, 42, 105,
	)
	small, smallCode := fastSpaceAgeSpotField(
		e.settings.seed, 0x41494345, x, y,
		88, 0.18, 3, 11,
	)
	start := e.startingPoint()
	startingIsland := fastSpaceAgeRadialSpot(x, y, start.x, start.y, 64)
	strength := max(large, small, startingIsland)
	if strength > 0 {
		strength += 0.2 * e.terrain[2](x, y)
	}
	if small > large {
		code = smallCode
	}
	sample := factorioSpaceAgeSample{
		elevation: strength * 40,
		land:      strength > 0,
		strength:  strength,
		code:      code,
	}
	if !sample.land {
		if e.terrain[0](x, y) > 0 {
			return color.RGBA{R: 17, G: 15, B: 30, A: 255}, sample
		}
		return color.RGBA{R: 15, G: 13, B: 25, A: 255}, sample
	}
	if strength < 0.08 {
		return color.RGBA{R: 21, G: 42, B: 56, A: 255}, sample
	}
	variant := fastSpaceAgeVariant(e.terrain[3](x, y)+code*0.25, len(fastAquiloTerrainColors))
	return fastAquiloTerrainColors[variant], sample
}

func (e *factorioSpaceAgeEvaluator) aquiloFeatures(
	base color.RGBA,
	sample factorioSpaceAgeSample,
	x, y, tilesPerPixel float64,
) color.RGBA {
	if !sample.land || sample.strength < 0.12 {
		return base
	}
	start := e.startingPoint()
	angle := fastSpaceAgeSeedAngle(e.settings.seed)
	if e.spaceAgeFluidAt("aquilo_crude_oil", x, y, tilesPerPixel, true,
		fastSpaceAgeAngledSpot(x, y, start, angle, 34, 13)) {
		return color.RGBA{A: 255}
	}
	if e.spaceAgeFluidAt("lithium_brine", x, y, tilesPerPixel, true,
		fastSpaceAgeAngledSpot(x, y, start, angle+2*math.Pi/3, 39, 14)) {
		return color.RGBA{R: 178, G: 255, B: 153, A: 255}
	}
	if e.spaceAgeFluidAt("fluorine_vent", x, y, tilesPerPixel, true,
		fastSpaceAgeAngledSpot(x, y, start, angle+4*math.Pi/3, 43, 14)) {
		return color.RGBA{G: 204, B: 255, A: 255}
	}
	return base
}

func (e *factorioSpaceAgeEvaluator) fastCliffAt(
	elevation, x, y, fallbackInterval float64,
) bool {
	if !e.settings.cliffs.enabled || e.settings.cliffRichness <= 0 {
		return false
	}
	interval := e.settings.cliffElevationInterval
	if interval <= 0 {
		interval = fallbackInterval
	}
	phase := math.Mod(elevation-e.settings.cliffElevation0, interval)
	if phase < 0 {
		phase += interval
	}
	distance := min(phase, interval-phase)
	continuity := 0.5 + 0.5*e.terrain[7](x, y)
	return distance < 0.5*positiveOr(e.settings.cliffRichness, 1) && continuity < 0.42
}

func (e *factorioSpaceAgeEvaluator) spaceAgeResourceAt(
	name string,
	x, y, tilesPerPixel float64,
	favorable bool,
	startingSignal float64,
) bool {
	control := fastSpaceAgeControl(e.settings, name)
	if !control.enabled || (!favorable && startingSignal <= 0) {
		return false
	}
	field := e.resource[name](x, y)
	threshold := 0.62 - 0.09*math.Log2(positiveOr(control.size, 1))
	inPatch := startingSignal+0.28*field > 0.28 || favorable && field > threshold
	if !inPatch {
		return false
	}
	coverage := 0.38 * clampFloat(positiveOr(control.richness, 1), 0.25, 3)
	return fastSpaceAgeEntityChance(
		e.settings.seed, fastSpaceAgeNameSalt(name), x, y, tilesPerPixel, coverage,
	)
}

func (e *factorioSpaceAgeEvaluator) spaceAgeFluidAt(
	name string,
	x, y, tilesPerPixel float64,
	favorable bool,
	startingSignal float64,
) bool {
	control := fastSpaceAgeControl(e.settings, name)
	if !control.enabled || (!favorable && startingSignal <= 0) {
		return false
	}
	field := e.resource[name](x, y)
	threshold := 0.58 - 0.1*math.Log2(positiveOr(control.size, 1))
	if startingSignal+0.22*field <= 0.22 && field <= threshold {
		return false
	}
	chance := 0.006 * positiveOr(control.frequency, 1)
	if startingSignal > 0 {
		chance = 0.035
	}
	return fastSpaceAgeEntityChance(
		e.settings.seed, fastSpaceAgeNameSalt(name), x, y, tilesPerPixel, chance,
	)
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

func fastSpaceAgeVariant(value float64, count int) int {
	if count <= 1 {
		return 0
	}
	normalized := clampFloat(0.5+0.42*value, 0, math.Nextafter(1, 0))
	return min(int(normalized*float64(count)), count-1)
}

func fastSpaceAgeSeedAngle(seed uint32) float64 {
	return 2 * math.Pi * float64(seed) / float64(^uint32(0))
}

func fastSpaceAgeAngledSpot(
	x, y float64,
	start factorioPoint,
	angle, distance, radius float64,
) float64 {
	centerX := start.x + math.Cos(angle)*distance
	centerY := start.y + math.Sin(angle)*distance
	return fastSpaceAgeRadialSpot(x, y, centerX, centerY, radius)
}

func fastSpaceAgeRadialSpot(x, y, centerX, centerY, radius float64) float64 {
	if radius <= 0 {
		return 0
	}
	return clampFloat(1-math.Hypot(x-centerX, y-centerY)/radius, 0, 1)
}

func fastSpaceAgeSpotField(
	seed, salt uint32,
	x, y, spacing, activity, minRadius, maxRadius float64,
) (float64, float64) {
	if spacing <= 0 || maxRadius <= 0 || activity <= 0 {
		return 0, 0
	}
	cellX := int64(math.Floor(x / spacing))
	cellY := int64(math.Floor(y / spacing))
	best := 0.0
	bestCode := 0.0
	for offsetY := int64(-1); offsetY <= 1; offsetY++ {
		for offsetX := int64(-1); offsetX <= 1; offsetX++ {
			cx := cellX + offsetX
			cy := cellY + offsetY
			active := fastHashUnit(seed, salt, cx, cy)
			if active > activity {
				continue
			}
			jitterX := fastHashUnit(seed, salt+1, cx, cy)
			jitterY := fastHashUnit(seed, salt+2, cx, cy)
			code := fastHashUnit(seed, salt+3, cx, cy)
			radius := lerpFloat(minRadius, maxRadius, fastHashUnit(seed, salt+4, cx, cy))
			centerX := (float64(cx) + 0.12 + 0.76*jitterX) * spacing
			centerY := (float64(cy) + 0.12 + 0.76*jitterY) * spacing
			dx := (x - centerX) / radius
			dy := (y - centerY) / (radius * lerpFloat(0.72, 1.25, code))
			strength := 1 - math.Hypot(dx, dy)
			if strength > best {
				best = strength
				bestCode = code
			}
		}
	}
	return max(0, best), bestCode
}

func fastSpaceAgeEntityChance(
	seed, salt uint32,
	x, y, tilesPerPixel, chance float64,
) bool {
	coverage := clampFloat(chance*max(1, tilesPerPixel*tilesPerPixel), 0, 1)
	return fastHashUnit(seed, salt, int64(math.Floor(x)), int64(math.Floor(y))) < coverage
}

func fastSpaceAgeNameSalt(name string) uint32 {
	hash := uint32(2166136261)
	for _, value := range []byte(name) {
		hash ^= uint32(value)
		hash *= 16777619
	}
	return hash
}

func fastSpaceAgePositiveMod(value, divisor int64) int64 {
	remainder := value % divisor
	if remainder < 0 {
		remainder += divisor
	}
	return remainder
}
