package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type fastPreviewSettings struct {
	seed                          uint32
	planet                        string
	mapType                       string
	width                         float64
	height                        float64
	startingArea                  float64
	water                         fastControl
	trees                         fastControl
	rocks                         fastControl
	cliffs                        fastControl
	cliffElevation0               float64
	cliffElevationInterval        float64
	cliffRichness                 float64
	enemyBases                    fastControl
	noEnemies                     bool
	moistureFrequency             float64
	moistureBias                  float64
	auxFrequency                  float64
	auxBias                       float64
	temperatureBias               float64
	temperatureFreq               float64
	startingAreaMoistureSize      float64
	startingAreaMoistureFrequency float64
	startingPositions             []factorioPoint
	resourceControls              map[string]fastControl
	autoplaceControls             map[string]fastControl
	propertyExpression            map[string]any
}

type fastControl struct {
	frequency float64
	size      float64
	richness  float64
	enabled   bool
}

type fastPreviewWorld struct {
	settings  fastPreviewSettings
	nauvis    *factorioNauvisEvaluator
	spaceAge  *factorioSpaceAgeEvaluator
	trees     *factorioTreeEvaluator
	resources *factorioResourceEvaluator
	rocks     *factorioRockEvaluator
	cliffs    *factorioCliffEvaluator
	enemies   *factorioEnemyEvaluator
}

const (
	fastPreviewPlanetNauvis   = "nauvis"
	fastPreviewPlanetVulcanus = "vulcanus"
	fastPreviewPlanetGleba    = "gleba"
	fastPreviewPlanetFulgora  = "fulgora"
	fastPreviewPlanetAquilo   = "aquilo"
)

var fastPreviewPlanets = map[string]struct{}{
	fastPreviewPlanetNauvis:   {},
	fastPreviewPlanetVulcanus: {},
	fastPreviewPlanetGleba:    {},
	fastPreviewPlanetFulgora:  {},
	fastPreviewPlanetAquilo:   {},
}

var fastResourceNames = []string{
	"iron-ore",
	"copper-ore",
	"coal",
	"stone",
	"crude-oil",
	"uranium-ore",
}

var fastSpaceAgeControlNames = []string{
	"vulcanus_coal",
	"sulfuric_acid_geyser",
	"tungsten_ore",
	"calcite",
	"vulcanus_volcanism",
	"gleba_stone",
	"gleba_water",
	"gleba_plants",
	"gleba_cliff",
	"gleba_enemy_base",
	"scrap",
	"fulgora_islands",
	"fulgora_cliff",
	"aquilo_crude_oil",
	"lithium_brine",
	"fluorine_vent",
}

var fastScaleValues = map[string]float64{
	"none":       0,
	"very-low":   0.25,
	"very-small": 0.25,
	"low":        0.5,
	"small":      0.5,
	"normal":     1,
	"regular":    1,
	"high":       2,
	"big":        2,
	"good":       2,
	"very-high":  3,
	"very-big":   3,
	"very-good":  3,
}

func (p *previewer) renderFast(ctx context.Context, ref profileRef, mapGen json.RawMessage, req previewRequest) (previewResponse, error) {
	size := req.Size
	if size == 0 {
		size = 768
	}
	if size < 256 || size > maxPreviewOutputSize {
		return previewResponse{}, invalidPreviewRequest(fmt.Errorf("preview size must be between 256 and %d pixels", maxPreviewOutputSize))
	}
	planet, err := normalizeFastPreviewPlanet(req.Planet)
	if err != nil {
		return previewResponse{}, invalidPreviewRequest(err)
	}
	zoom, err := fastPreviewZoomSpec(req.Zoom, size)
	if err != nil {
		return previewResponse{}, invalidPreviewRequest(err)
	}
	centerX, centerY, err := normalizedPreviewCenter(req.CenterX, req.CenterY, zoom.tilesPerPixel)
	if err != nil {
		return previewResponse{}, invalidPreviewRequest(err)
	}
	if err := validatePreviewSeedOverride(req.Seed); err != nil {
		return previewResponse{}, invalidPreviewRequest(err)
	}
	settings, err := parseFastPreviewSettingsForPlanet(mapGen, req.Seed, planet)
	if err != nil {
		return previewResponse{}, invalidPreviewRequest(err)
	}
	cacheKey, err := fastPreviewCacheKeyForPlanet(mapGen, settings.seed, planet)
	if err != nil {
		return previewResponse{}, invalidPreviewRequest(err)
	}
	tpp := fastTilesPerPixel(zoom)
	img, err := p.fastCache().render(ctx, cacheKey, settings, size, tpp, centerX, centerY)
	if err != nil {
		return previewResponse{}, err
	}
	data, contentType, ext, err := encodePNGPreviewImage(img)
	if err != nil {
		return previewResponse{}, err
	}
	previewName, err := p.storePreviewImage(data, contentType, ext)
	if err != nil {
		return previewResponse{}, err
	}

	generatedAt := time.Now().UTC().Format(time.RFC3339)
	return previewResponse{
		URL:           "/api/previews/" + url.PathEscape(previewName) + "?ts=" + url.QueryEscape(generatedAt),
		GeneratedAt:   generatedAt,
		Size:          size,
		Planet:        planet,
		Engine:        previewEngineFast,
		CenterX:       centerX,
		CenterY:       centerY,
		TilesPerPixel: tpp,
		Output: fmt.Sprintf(
			"Fast Go preview rendered %s at %.6g tiles/pixel centered on %.6g, %.6g for %s.",
			planet, tpp, centerX, centerY, ref.id(),
		),
	}, nil
}

func normalizeFastPreviewPlanet(value string) (string, error) {
	planet := strings.ToLower(strings.TrimSpace(value))
	if planet == "" {
		planet = fastPreviewPlanetNauvis
	}
	if _, ok := fastPreviewPlanets[planet]; !ok {
		return "", fmt.Errorf(
			"fast preview planet %q is not supported; choose nauvis, vulcanus, gleba, fulgora, or aquilo",
			planet,
		)
	}
	return planet, nil
}

func (p *previewer) fastCache() *fastPreviewCache {
	p.fastCacheOnce.Do(func() {
		if p.fastPreviewCache == nil {
			cacheBytes := p.fastPreviewCacheBytes
			if cacheBytes <= 0 {
				cacheBytes = defaultFastPreviewCacheBytes
			}
			p.fastPreviewCache = newFastPreviewCache(cacheBytes, defaultFastPreviewCacheWorlds)
		}
	})
	return p.fastPreviewCache
}

func parseFastPreviewSettings(raw json.RawMessage, seedOverride string) (fastPreviewSettings, error) {
	return parseFastPreviewSettingsForPlanet(raw, seedOverride, fastPreviewPlanetNauvis)
}

func parseFastPreviewSettingsForPlanet(
	raw json.RawMessage,
	seedOverride string,
	planet string,
) (fastPreviewSettings, error) {
	var root map[string]any
	if err := decodeObject(raw, &root); err != nil {
		return fastPreviewSettings{}, err
	}
	normalizedPlanet, err := normalizeFastPreviewPlanet(planet)
	if err != nil {
		return fastPreviewSettings{}, err
	}
	props := fastMap(root["property_expression_names"])
	cliffSettings := fastMap(root["cliff_settings"])
	cliffControl := fastPlanetCliffControl(normalizedPlanet)
	waterControl := "water"
	treeControl := "trees"
	enemyControl := "enemy-base"
	if normalizedPlanet == fastPreviewPlanetGleba {
		waterControl = "gleba_water"
		treeControl = "gleba_plants"
		enemyControl = "gleba_enemy_base"
	}
	startingPositions, err := fastStartingPositions(root["starting_points"])
	if err != nil {
		return fastPreviewSettings{}, err
	}
	settings := fastPreviewSettings{
		seed:                          fastPreviewSeed(root, seedOverride),
		planet:                        normalizedPlanet,
		mapType:                       normalizedPlanet,
		width:                         fastNumber(root["width"], 0),
		height:                        fastNumber(root["height"], 0),
		startingArea:                  fastNumber(root["starting_area"], 1),
		water:                         fastAutoplaceControl(root, waterControl),
		trees:                         fastAutoplaceControl(root, treeControl),
		rocks:                         fastAutoplaceControl(root, "rocks"),
		cliffs:                        fastAutoplaceControl(root, cliffControl),
		cliffElevation0:               fastNumber(cliffSettings["cliff_elevation_0"], 10),
		cliffElevationInterval:        fastNumber(cliffSettings["cliff_elevation_interval"], 40),
		cliffRichness:                 fastNumber(cliffSettings["richness"], 1),
		enemyBases:                    fastAutoplaceControl(root, enemyControl),
		noEnemies:                     fastBool(root["no_enemies_mode"]),
		moistureFrequency:             fastNumber(props["control:moisture:frequency"], 1),
		moistureBias:                  fastNumber(props["control:moisture:bias"], 0),
		auxFrequency:                  fastNumber(props["control:aux:frequency"], 1),
		auxBias:                       fastNumber(props["control:aux:bias"], 0),
		temperatureFreq:               fastNumber(props["control:temperature:frequency"], 1),
		temperatureBias:               fastNumber(props["control:temperature:bias"], 0),
		startingAreaMoistureSize:      fastNumber(props["control:starting_area_moisture:size"], 1),
		startingAreaMoistureFrequency: fastNumber(props["control:starting_area_moisture:frequency"], 1),
		startingPositions:             startingPositions,
		resourceControls:              map[string]fastControl{},
		autoplaceControls:             map[string]fastControl{},
		propertyExpression:            props,
	}
	if normalizedPlanet == fastPreviewPlanetNauvis {
		settings.mapType = fastMapType(props)
	}
	for _, resource := range fastResourceNames {
		control := fastAutoplaceControl(root, resource)
		settings.resourceControls[resource] = control
		settings.autoplaceControls[resource] = control
	}
	for _, name := range fastSpaceAgeControlNames {
		settings.autoplaceControls[name] = fastAutoplaceControl(root, name)
	}
	settings.autoplaceControls[waterControl] = settings.water
	settings.autoplaceControls[treeControl] = settings.trees
	settings.autoplaceControls[cliffControl] = settings.cliffs
	settings.autoplaceControls[enemyControl] = settings.enemyBases
	if settings.startingArea <= 0 {
		settings.startingArea = 1
	}
	return settings, nil
}

func fastPlanetCliffControl(planet string) string {
	switch planet {
	case fastPreviewPlanetGleba:
		return "gleba_cliff"
	case fastPreviewPlanetFulgora:
		return "fulgora_cliff"
	default:
		return "nauvis_cliff"
	}
}

func fastPreviewSeed(root map[string]any, override string) uint32 {
	if n, ok := fastUnsignedSeed(override); ok {
		return n
	}
	if n, ok := fastUnsignedSeed(root["seed"]); ok {
		return n
	}
	return 1
}

func fastUnsignedSeed(value any) (uint32, bool) {
	switch v := value.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, false
		}
		n, err := strconv.ParseUint(v, 10, 32)
		return uint32(n), err == nil && n > 0
	case json.Number:
		n, err := strconv.ParseUint(v.String(), 10, 32)
		return uint32(n), err == nil && n > 0
	case float64:
		if v < 1 || v > float64(^uint32(0)) || math.Trunc(v) != v {
			return 0, false
		}
		return uint32(v), true
	case int:
		if v < 1 || uint64(v) > uint64(^uint32(0)) {
			return 0, false
		}
		return uint32(v), true
	default:
		return 0, false
	}
}

func fastMapType(props map[string]any) string {
	switch strings.TrimSpace(fastString(props["elevation"])) {
	case "elevation_island", "island":
		return "island"
	case "elevation_lakes", "lakes":
		return "lakes"
	default:
		return "nauvis"
	}
}

func fastAutoplaceControl(root map[string]any, name string) fastControl {
	control := fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	controls := fastMap(root["autoplace_controls"])
	raw, ok := controls[name]
	if !ok {
		if enabled, explicit := root["default_enable_all_autoplace_controls"]; explicit && !fastBool(enabled) {
			control.enabled = false
		}
		return control
	}
	m := fastMap(raw)
	control.frequency = fastNumber(m["frequency"], 1)
	control.size = fastNumber(m["size"], 1)
	control.richness = fastNumber(m["richness"], 1)
	control.enabled = control.frequency > 0 && control.size > 0
	return control
}

func renderFastMapPreview(ctx context.Context, settings fastPreviewSettings, size int, zoom previewZoom) (*image.RGBA, float64, error) {
	tpp := fastTilesPerPixel(zoom)
	img, err := newFastPreviewWorld(settings).render(ctx, size, tpp, 0, 0)
	return img, tpp, err
}

func renderFastMapPreviewAt(
	ctx context.Context,
	settings fastPreviewSettings,
	size int,
	zoom previewZoom,
	centerX, centerY float64,
) (*image.RGBA, float64, error) {
	tpp := fastTilesPerPixel(zoom)
	img, err := newFastPreviewWorld(settings).render(ctx, size, tpp, centerX, centerY)
	return img, tpp, err
}

func newFastPreviewWorld(settings fastPreviewSettings) *fastPreviewWorld {
	world := &fastPreviewWorld{settings: settings}
	if settings.planet != "" && settings.planet != fastPreviewPlanetNauvis {
		world.spaceAge = newFactorioSpaceAgeEvaluator(settings)
		return world
	}
	world.nauvis = newFactorioNauvisEvaluator(settings)
	if settings.trees.enabled {
		world.trees = newFactorioTreeEvaluator(settings, world.nauvis)
	}
	world.resources = newFactorioResourceEvaluator(settings, world.nauvis)
	if settings.rocks.enabled {
		world.rocks = newFactorioRockEvaluator(settings, world.nauvis)
	}
	if settings.cliffs.enabled {
		world.cliffs = newFactorioCliffEvaluator(settings, world.nauvis)
	}
	if !settings.noEnemies && settings.enemyBases.enabled {
		world.enemies = newFactorioEnemyEvaluator(settings)
	}
	return world
}

func (w *fastPreviewWorld) trimSpatialCaches() {
	if w.resources != nil {
		w.resources.trimRegionCaches(fastPreviewMaxRegionsPerField)
	}
	if w.enemies != nil {
		w.enemies.trimRegionCache(fastPreviewMaxRegionsPerField)
	}
}

func (w *fastPreviewWorld) render(
	ctx context.Context,
	size int,
	tpp float64,
	centerX, centerY float64,
) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	originX := fastPreviewViewportOrigin(centerX, size, tpp)
	originY := fastPreviewViewportOrigin(centerY, size, tpp)
	if err := w.renderBase(ctx, img, originX, originY, tpp); err != nil {
		return nil, err
	}
	if err := w.renderOverlays(ctx, img, originX, originY, tpp); err != nil {
		return nil, err
	}
	return img, nil
}

func (w *fastPreviewWorld) renderBase(
	ctx context.Context,
	img *image.RGBA,
	originX, originY, tpp float64,
) error {
	settings := w.settings
	size := img.Bounds().Dx()
	if w.spaceAge != nil {
		return w.spaceAge.render(ctx, img, settings, originX, originY, tpp)
	}
	xAxis := newFastPreviewAxis(originX, tpp)
	yAxis := newFastPreviewAxis(originY, tpp)
	if w.nauvis != nil && tpp == 1 {
		w.resources.prepareForBounds(
			math.Floor(originX),
			math.Floor(originY),
			math.Floor(originX+float64(size-1)),
			math.Floor(originY+float64(size-1)),
		)
		if err := renderFastNauvisOneTileRows(
			ctx, img, settings, w.nauvis, w.trees, w.rocks, w.resources, nil, originX, originY,
		); err != nil {
			return err
		}
		return nil
	}

	lastWorldY := math.Inf(-1)
	for y := 0; y < size; y++ {
		if y&31 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		wy := math.Floor(yAxis.coordinate(y))
		row := y * img.Stride
		if y > 0 && wy == lastWorldY {
			copy(img.Pix[row:row+img.Stride], img.Pix[row-img.Stride:row])
			continue
		}
		lastWorldY = wy
		lastWorldX := math.Inf(-1)
		for x := 0; x < size; x++ {
			wx := math.Floor(xAxis.coordinate(x))
			o := row + x*4
			if x > 0 && wx == lastWorldX {
				copy(img.Pix[o:o+4], img.Pix[o-4:o])
				continue
			}
			lastWorldX = wx
			nauvisPoint := w.nauvis.sample(wx, wy)
			c := w.nauvis.terrainTile(nauvisPoint, wx, wy).color
			outOfMap := fastOutOfMapBounds(settings, wx, wy)
			if outOfMap {
				c = color.RGBA{R: 20, G: 23, B: 20, A: 255}
			}
			img.Pix[o] = c.R
			img.Pix[o+1] = c.G
			img.Pix[o+2] = c.B
			img.Pix[o+3] = 255
		}
	}

	if w.trees != nil {
		if err := renderFactorioTrees(ctx, img, settings, w.trees, originX, originY, tpp); err != nil {
			return err
		}
	}

	lastWorldY = math.Inf(-1)
	for y := 0; y < size; y++ {
		if y&31 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		wy := math.Floor(yAxis.coordinate(y))
		row := y * img.Stride
		if y > 0 && wy == lastWorldY {
			copy(img.Pix[row:row+img.Stride], img.Pix[row-img.Stride:row])
			continue
		}
		lastWorldY = wy
		lastWorldX := math.Inf(-1)
		for x := 0; x < size; x++ {
			wx := math.Floor(xAxis.coordinate(x))
			offset := row + x*4
			if x > 0 && wx == lastWorldX {
				copy(img.Pix[offset:offset+4], img.Pix[offset-4:offset])
				continue
			}
			lastWorldX = wx
			if fastOutOfMapBounds(settings, wx, wy) {
				continue
			}
			base := color.RGBA{R: img.Pix[offset], G: img.Pix[offset+1], B: img.Pix[offset+2], A: 255}
			if factorioPreviewWaterColor(base) {
				continue
			}
			base = blendFastPreviewEntities(
				w.trees, w.rocks, w.resources, nil,
				base, nil, wx, wy, y*size+x, tpp,
			)
			img.Pix[offset] = base.R
			img.Pix[offset+1] = base.G
			img.Pix[offset+2] = base.B
		}
	}

	return nil
}

func (w *fastPreviewWorld) renderOverlays(
	ctx context.Context,
	img *image.RGBA,
	originX, originY, tpp float64,
) error {
	if w.resources != nil && w.resources.hasOil() {
		oilMask, err := w.resources.oilPreviewMask(
			ctx, w.settings, img, originX, originY, tpp,
		)
		if err != nil {
			return err
		}
		applyFastPreviewOilMask(img, oilMask)
	}
	if w.enemies != nil {
		if err := renderFactorioEnemies(ctx, img, w.settings, w.enemies, originX, originY, tpp); err != nil {
			return err
		}
	}
	if w.cliffs != nil {
		if err := renderFactorioCliffs(ctx, img, w.settings, w.cliffs, originX, originY, tpp); err != nil {
			return err
		}
	}
	return nil
}

func applyFastPreviewOilMask(img *image.RGBA, oilMask []bool) {
	width := img.Bounds().Dx()
	for index, marked := range oilMask {
		if !marked {
			continue
		}
		x := index % width
		y := index / width
		offset := y*img.Stride + x*4
		img.Pix[offset] = factorioResourceCatalog[4].mapColor.R
		img.Pix[offset+1] = factorioResourceCatalog[4].mapColor.G
		img.Pix[offset+2] = factorioResourceCatalog[4].mapColor.B
	}
}

type fastPreviewAxis struct {
	origin       float64
	step         float64
	rasterOrigin int64
	aligned      bool
}

func newFastPreviewAxis(origin, step float64) fastPreviewAxis {
	rasterOrigin, aligned := fastPreviewRasterCoordinate(origin, step)
	return fastPreviewAxis{
		origin: origin, step: step, rasterOrigin: rasterOrigin, aligned: aligned,
	}
}

func (a fastPreviewAxis) coordinate(pixel int) float64 {
	if a.aligned {
		return float64(a.rasterOrigin+int64(pixel)) * a.step
	}
	return a.origin + float64(pixel)*a.step
}

func fastPreviewViewportOrigin(center float64, size int, tilesPerPixel float64) float64 {
	centerRaster, aligned := fastPreviewRasterCoordinate(center, tilesPerPixel)
	if size%2 == 0 && aligned {
		return float64(centerRaster-int64(size/2)) * tilesPerPixel
	}
	return center - float64(size)*tilesPerPixel/2
}

func renderFastNauvisOneTileRows(
	ctx context.Context,
	img *image.RGBA,
	settings fastPreviewSettings,
	nauvis *factorioNauvisEvaluator,
	trees *factorioTreeEvaluator,
	rocks *factorioRockEvaluator,
	resources *factorioResourceEvaluator,
	oilMask []bool,
	originX, originY float64,
) error {
	size := img.Bounds().Dx()
	return parallelFastPreviewRows(ctx, size, func(y int) {
		wy := math.Floor(originY + float64(y))
		row := y * img.Stride
		for x := 0; x < size; x++ {
			wx := math.Floor(originX + float64(x))
			offset := row + x*4
			sample := nauvis.sample(wx, wy)
			base := nauvis.terrainTileFast(sample, wx, wy).color
			if fastOutOfMapBounds(settings, wx, wy) {
				base = color.RGBA{R: 20, G: 23, B: 20, A: 255}
			} else if !factorioPreviewWaterColor(base) {
				base = blendFastPreviewEntities(
					trees, rocks, resources, oilMask,
					base, &sample, wx, wy, y*size+x, 1,
				)
			}
			img.Pix[offset] = base.R
			img.Pix[offset+1] = base.G
			img.Pix[offset+2] = base.B
			img.Pix[offset+3] = 255
		}
	})
}

const (
	fastPreviewMaxWorkers   = 12
	fastPreviewRowsPerChunk = 8
)

func parallelFastPreviewRows(ctx context.Context, height int, renderRow func(int)) error {
	workerCount := min(runtime.GOMAXPROCS(0), height, fastPreviewMaxWorkers)
	if workerCount <= 1 {
		for y := 0; y < height; y++ {
			if y&7 == 0 && ctx.Err() != nil {
				return ctx.Err()
			}
			renderRow(y)
		}
		return nil
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for firstRow := worker * fastPreviewRowsPerChunk; firstRow < height; firstRow += workerCount * fastPreviewRowsPerChunk {
				lastRow := min(firstRow+fastPreviewRowsPerChunk, height)
				for y := firstRow; y < lastRow; y++ {
					if y == firstRow && ctx.Err() != nil {
						return
					}
					renderRow(y)
				}
			}
		}()
	}
	workers.Wait()
	return ctx.Err()
}

func blendFastPreviewEntities(
	trees *factorioTreeEvaluator,
	rocks *factorioRockEvaluator,
	resources *factorioResourceEvaluator,
	oilMask []bool,
	base color.RGBA,
	sample *factorioNauvisSample,
	wx, wy float64,
	pixelIndex int,
	tilesPerPixel float64,
) color.RGBA {
	if sample != nil && trees != nil && trees.placedAtSample(wx, wy, *sample) {
		base = blendFactorioTrees(base, 1)
	}
	entityDither := (rocks != nil || resources != nil) &&
		factorioPreviewEntityDither(wx, wy, tilesPerPixel)
	if rocks != nil && entityDither {
		var rock color.RGBA
		var ok bool
		if sample != nil {
			rock, ok = rocks.colorAtSample(wx, wy, *sample)
		} else {
			rock, ok = rocks.colorAt(wx, wy)
		}
		if ok {
			base = rock
		}
	}
	if resources != nil && entityDither {
		if resource, ok := resources.resourceAt(wx, wy); ok {
			base = resource.mapColor
		}
	}
	if len(oilMask) > 0 && oilMask[pixelIndex] {
		base = factorioResourceCatalog[4].mapColor
	}
	return base
}

func fastTilesPerPixel(zoom previewZoom) float64 {
	if zoom.tilesPerPixel > 0 {
		return zoom.tilesPerPixel
	}
	return 1
}

func factorioPreviewEntityDither(wx, wy, tilesPerPixel float64) bool {
	if tilesPerPixel <= 2 {
		return (int64(math.Floor(wx/2))+int64(math.Floor(wy/2)))&1 == 0
	}
	return fastHashUnit(0, 0x43484152, int64(math.Floor(wx)), int64(math.Floor(wy))) < 0.5
}

func fastOutOfMapBounds(settings fastPreviewSettings, wx, wy float64) bool {
	if settings.width > 0 && math.Abs(wx) > settings.width/2 {
		return true
	}
	if settings.height > 0 && math.Abs(wy) > settings.height/2 {
		return true
	}
	return false
}

func fastHashUnit(seed uint32, salt uint32, x, y int64) float64 {
	h := fastHash(seed, salt, x, y)
	return float64(h>>11) * (1.0 / (1 << 53))
}

func fastHash(seed uint32, salt uint32, x, y int64) uint64 {
	z := uint64(seed)*0x9e3779b97f4a7c15 + uint64(salt)*0xbf58476d1ce4e5b9
	z ^= uint64(x) * 0x94d049bb133111eb
	z ^= uint64(y) * 0xd2b74407b1ce6e93
	z += 0x9e3779b97f4a7c15
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func lerpFloat(a, b, t float64) float64 {
	return a + (b-a)*t
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

const maxFastPreviewStartingPoints = 64

func fastStartingPositions(value any) ([]factorioPoint, error) {
	if value == nil {
		return []factorioPoint{{}}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("starting_points must be an array")
	}
	if len(items) == 0 {
		return []factorioPoint{{}}, nil
	}
	if len(items) > maxFastPreviewStartingPoints {
		return nil, fmt.Errorf("starting_points supports at most %d entries", maxFastPreviewStartingPoints)
	}
	points := make([]factorioPoint, 0, len(items))
	for index, item := range items {
		point, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("starting_points[%d] must be an object", index)
		}
		x, xOK := fastFiniteNumber(point["x"], 0)
		y, yOK := fastFiniteNumber(point["y"], 0)
		if !xOK || !yOK || math.Abs(x) > maxPreviewCenter || math.Abs(y) > maxPreviewCenter {
			return nil, fmt.Errorf(
				"starting_points[%d] coordinates must be finite and between -%.0f and %.0f",
				index, maxPreviewCenter, maxPreviewCenter,
			)
		}
		points = append(points, factorioPoint{x: x, y: y})
	}
	return points, nil
}

func fastMap(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func fastBool(value any) bool {
	v, ok := value.(bool)
	return ok && v
}

func fastString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func fastNumber(value any, fallback float64) float64 {
	n, ok := fastFiniteNumber(value, fallback)
	if !ok {
		return fallback
	}
	return n
}

func fastFiniteNumber(value any, fallback float64) (float64, bool) {
	if value == nil {
		return fallback, true
	}
	var n float64
	switch v := value.(type) {
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return fallback, false
		}
		n = parsed
	case float64:
		n = v
	case int:
		n = float64(v)
	case string:
		s := strings.TrimSpace(v)
		if scaled, ok := fastScaleValues[strings.ToLower(s)]; ok {
			n = scaled
		} else {
			parsed, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return fallback, false
			}
			n = parsed
		}
	default:
		return fallback, false
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return fallback, false
	}
	return n, true
}
