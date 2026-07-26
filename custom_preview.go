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
	trees     *factorioTreeEvaluator
	resources *factorioResourceEvaluator
	rocks     *factorioRockEvaluator
	cliffs    *factorioCliffEvaluator
	enemies   *factorioEnemyEvaluator
}

type fastResourceDef struct {
	name        string
	salt        uint32
	color       color.RGBA
	cellSize    float64
	radius      float64
	chance      float64
	starting    bool
	startingX   float64
	startingY   float64
	startingRad float64
}

var fastResourceDefs = []fastResourceDef{
	{name: "iron-ore", salt: 101, color: color.RGBA{R: 105, G: 133, B: 147, A: 255}, cellSize: 210, radius: 30, chance: 0.62, starting: true, startingX: -66, startingY: -24, startingRad: 34},
	{name: "copper-ore", salt: 102, color: color.RGBA{R: 204, G: 98, B: 54, A: 255}, cellSize: 215, radius: 30, chance: 0.60, starting: true, startingX: 70, startingY: 22, startingRad: 34},
	{name: "coal", salt: 103, color: color.RGBA{A: 255}, cellSize: 195, radius: 29, chance: 0.62, starting: true, startingX: -30, startingY: 78, startingRad: 32},
	{name: "stone", salt: 104, color: color.RGBA{R: 175, G: 155, B: 108, A: 255}, cellSize: 240, radius: 25, chance: 0.54, starting: true, startingX: 58, startingY: -78, startingRad: 28},
	{name: "crude-oil", salt: 105, color: color.RGBA{R: 199, G: 51, B: 196, A: 255}, cellSize: 520, radius: 18, chance: 0.24},
	{name: "uranium-ore", salt: 106, color: color.RGBA{R: 0, G: 179, B: 0, A: 255}, cellSize: 560, radius: 20, chance: 0.20},
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
		return previewResponse{}, fmt.Errorf("preview size must be between 256 and %d pixels", maxPreviewOutputSize)
	}
	planet := strings.TrimSpace(req.Planet)
	if planet == "" {
		planet = "nauvis"
	}
	if planet != "nauvis" {
		return previewResponse{}, errors.New("fast preview currently supports the Nauvis surface only; use exact Factorio preview for Space Age planets")
	}
	zoom, err := previewZoomSpec(req.Zoom, size)
	if err != nil {
		return previewResponse{}, err
	}
	centerX, centerY, err := normalizedPreviewCenter(req.CenterX, req.CenterY, zoom.tilesPerPixel)
	if err != nil {
		return previewResponse{}, err
	}
	settings, err := parseFastPreviewSettings(mapGen, req.Seed)
	if err != nil {
		return previewResponse{}, err
	}
	cacheKey, err := fastPreviewCacheKey(mapGen, settings.seed)
	if err != nil {
		return previewResponse{}, err
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
			settings.mapType, tpp, centerX, centerY, ref.id(),
		),
	}, nil
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
	var root map[string]any
	if err := decodeObject(raw, &root); err != nil {
		return fastPreviewSettings{}, err
	}
	props := fastMap(root["property_expression_names"])
	cliffSettings := fastMap(root["cliff_settings"])
	settings := fastPreviewSettings{
		seed:                          fastPreviewSeed(root, seedOverride),
		mapType:                       fastMapType(props),
		width:                         fastNumber(root["width"], 0),
		height:                        fastNumber(root["height"], 0),
		startingArea:                  fastNumber(root["starting_area"], 1),
		water:                         fastAutoplaceControl(root, "water"),
		trees:                         fastAutoplaceControl(root, "trees"),
		rocks:                         fastAutoplaceControl(root, "rocks"),
		cliffs:                        fastAutoplaceControl(root, "nauvis_cliff"),
		cliffElevation0:               fastNumber(cliffSettings["cliff_elevation_0"], 10),
		cliffElevationInterval:        fastNumber(cliffSettings["cliff_elevation_interval"], 40),
		cliffRichness:                 fastNumber(cliffSettings["richness"], 1),
		enemyBases:                    fastAutoplaceControl(root, "enemy-base"),
		noEnemies:                     fastBool(root["no_enemies_mode"]),
		moistureFrequency:             fastNumber(props["control:moisture:frequency"], 1),
		moistureBias:                  fastNumber(props["control:moisture:bias"], 0),
		auxFrequency:                  fastNumber(props["control:aux:frequency"], 1),
		auxBias:                       fastNumber(props["control:aux:bias"], 0),
		temperatureFreq:               fastNumber(props["control:temperature:frequency"], 1),
		temperatureBias:               fastNumber(props["control:temperature:bias"], 0),
		startingAreaMoistureSize:      fastNumber(props["control:starting_area_moisture:size"], 1),
		startingAreaMoistureFrequency: fastNumber(props["control:starting_area_moisture:frequency"], 1),
		startingPositions:             fastStartingPositions(root["starting_points"]),
		resourceControls:              map[string]fastControl{},
		propertyExpression:            props,
	}
	for _, resource := range fastResourceDefs {
		settings.resourceControls[resource.name] = fastAutoplaceControl(root, resource.name)
	}
	if settings.startingArea <= 0 {
		settings.startingArea = 1
	}
	return settings, nil
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
		if v < 1 {
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
	if settings.mapType != "nauvis" {
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
			var c color.RGBA
			if w.nauvis != nil {
				nauvisPoint := w.nauvis.sample(wx, wy)
				tile := w.nauvis.terrainTile(nauvisPoint, wx, wy)
				c = tile.color
			} else {
				c, _ = fastTerrainPixel(settings, wx, wy)
			}
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
				settings, w.nauvis, w.trees, w.rocks, w.resources, nil,
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
					settings, nauvis, trees, rocks, resources, oilMask,
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
	settings fastPreviewSettings,
	nauvis *factorioNauvisEvaluator,
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
	if nauvis == nil {
		base = fastBlendTrees(settings, base, wx, wy)
	} else if sample != nil && trees != nil && trees.placedAtSample(wx, wy, *sample) {
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
	} else if nauvis == nil {
		base = fastBlendRocks(settings, base, wx, wy)
	}
	if resources != nil {
		if entityDither {
			if resource, ok := resources.resourceAt(wx, wy); ok {
				base = resource.mapColor
			}
		}
	} else if resource, ok := fastResourcePixel(settings, wx, wy); ok {
		base = resource
	}
	if len(oilMask) > 0 && oilMask[pixelIndex] {
		base = factorioResourceCatalog[4].mapColor
	}
	if nauvis == nil {
		if enemy, ok := fastEnemyPixel(settings, wx, wy); ok {
			base = enemy
		}
	}
	if nauvis == nil {
		if cliff, ok := fastCliffPixel(settings, wx, wy); ok {
			base = cliff
		}
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

func fastTerrainPixel(settings fastPreviewSettings, wx, wy float64) (color.RGBA, bool) {
	landScore := fastLandScore(settings, wx, wy)
	if landScore < 0 {
		if landScore < -0.28 {
			return color.RGBA{R: 38, G: 64, B: 73, A: 255}, true
		}
		return color.RGBA{R: 51, G: 83, B: 95, A: 255}, true
	}

	moisture := fastClimate(settings.seed, 31, wx, wy, settings.moistureFrequency, settings.moistureBias)
	aux := fastClimate(settings.seed, 32, wx, wy, settings.auxFrequency, settings.auxBias)
	jitter := fastFBM(settings.seed, 33, wx, wy, 22, 2, 0.55) * 0.08
	if landScore < 0.08 && aux < 0.72 {
		return fastJitterColor(color.RGBA{R: 138, G: 103, B: 58, A: 255}, jitter), false
	}
	if moisture > 0.72 && aux < 0.55 {
		return fastJitterColor(color.RGBA{R: 55, G: 53, B: 11, A: 255}, jitter), false
	}
	if moisture > 0.56 && aux < 0.68 {
		return fastJitterColor(color.RGBA{R: 66, G: 57, B: 15, A: 255}, jitter), false
	}
	if moisture < 0.22 && aux < 0.50 {
		return fastJitterColor(color.RGBA{R: 128, G: 93, B: 52, A: 255}, jitter), false
	}
	if aux > 0.80 {
		return fastJitterColor(color.RGBA{R: 128, G: 93, B: 52, A: 255}, jitter), false
	}
	if aux > 0.66 {
		return fastJitterColor(color.RGBA{R: 116, G: 81, B: 39, A: 255}, jitter), false
	}
	if moisture < 0.38 {
		return fastJitterColor(color.RGBA{R: 141, G: 104, B: 60, A: 255}, jitter), false
	}
	return fastJitterColor(color.RGBA{R: 91, G: 63, B: 38, A: 255}, jitter), false
}

func fastLandScore(settings fastPreviewSettings, wx, wy float64) float64 {
	water := settings.water
	if !water.enabled {
		return 1
	}
	frequency := clampFloat(water.frequency, 0.1, 6)
	size := clampFloat(water.size, 0.05, 8)
	scale := 230 / math.Sqrt(frequency)
	large := fastFBM(settings.seed, 11, wx, wy, scale, 4, 0.55)
	small := fastFBM(settings.seed, 12, wx+4100, wy-2600, scale*0.42, 3, 0.52)
	elevation := large*0.78 + small*0.22
	threshold := clampFloat(-0.10+0.19*math.Log2(size), -0.58, 0.42)

	switch settings.mapType {
	case "lakes":
		threshold += 0.15
	case "island":
		radius := 315 * math.Sqrt(clampFloat(settings.startingArea, 0.5, 6))
		dist := math.Hypot(wx, wy)
		elevation -= clampFloat((dist-radius)/115, -0.18, 1.45)
		threshold += 0.02
	}
	return elevation - threshold
}

func fastResourcePixel(settings fastPreviewSettings, wx, wy float64) (color.RGBA, bool) {
	var best fastResourceDef
	bestScore := 0.0
	for _, def := range fastResourceDefs {
		control := settings.resourceControls[def.name]
		if !control.enabled {
			continue
		}
		score := fastResourceScore(settings, def, control, wx, wy)
		if score > bestScore {
			bestScore = score
			best = def
		}
	}
	if bestScore <= 0 {
		return color.RGBA{}, false
	}
	return best.color, true
}

func fastResourceScore(settings fastPreviewSettings, def fastResourceDef, control fastControl, wx, wy float64) float64 {
	score := fastStartingResourceScore(settings, def, control, wx, wy)
	frequency := clampFloat(control.frequency, 0.1, 6)
	size := clampFloat(control.size, 0.05, 6)
	cell := def.cellSize / math.Sqrt(frequency)
	patchChance := clampFloat(def.chance*(0.5+0.35*math.Sqrt(frequency)), 0.03, 0.92)
	cx := int64(math.Floor(wx / cell))
	cy := int64(math.Floor(wy / cell))
	for dy := int64(-1); dy <= 1; dy++ {
		for dx := int64(-1); dx <= 1; dx++ {
			ix := cx + dx
			iy := cy + dy
			if fastHashUnit(settings.seed, def.salt, ix, iy) > patchChance {
				continue
			}
			centerX := (float64(ix) + fastHashUnit(settings.seed, def.salt+17, ix, iy)) * cell
			centerY := (float64(iy) + fastHashUnit(settings.seed, def.salt+29, ix, iy)) * cell
			radius := def.radius * math.Sqrt(size) * (0.75 + fastHashUnit(settings.seed, def.salt+41, ix, iy)*0.72)
			dist := math.Hypot(wx-centerX, wy-centerY)
			if dist >= radius {
				continue
			}
			local := 1 - dist/radius
			if local > score {
				score = local
			}
		}
	}
	return score
}

func fastStartingResourceScore(settings fastPreviewSettings, def fastResourceDef, control fastControl, wx, wy float64) float64 {
	if !def.starting {
		return 0
	}
	scale := math.Sqrt(clampFloat(settings.startingArea, 0.35, 6))
	radius := def.startingRad * math.Sqrt(clampFloat(control.size, 0.2, 6)) * scale
	dist := math.Hypot(wx-def.startingX*scale, wy-def.startingY*scale)
	if dist >= radius {
		return 0
	}
	edge := 1 - dist/radius
	shape := fastFBM(settings.seed, def.salt+61, wx, wy, 18, 2, 0.6)*0.18 + 0.82
	return edge * shape
}

func fastBlendTrees(settings fastPreviewSettings, base color.RGBA, wx, wy float64) color.RGBA {
	if !settings.trees.enabled {
		return base
	}
	moisture := fastClimate(settings.seed, 31, wx, wy, settings.moistureFrequency, settings.moistureBias)
	temperature := fastClimate(settings.seed, 34, wx, wy, settings.temperatureFreq, settings.temperatureBias)
	return fastBlendTreesWithClimate(settings, base, wx, wy, moisture, temperature)
}

func fastBlendTreesWithClimate(settings fastPreviewSettings, base color.RGBA, wx, wy, moisture, temperature float64) color.RGBA {
	if !settings.trees.enabled {
		return base
	}
	frequency := clampFloat(settings.trees.frequency, 0.1, 6)
	size := clampFloat(settings.trees.size, 0.05, 6)
	density := fastFBM(settings.seed, 71, wx, wy, 78/math.Sqrt(frequency), 3, 0.58)*0.55 + moisture*0.55 + (1-math.Abs(temperature-0.52))*0.18
	threshold := 0.72 - 0.13*math.Log2(size)
	if density <= threshold {
		return base
	}
	alpha := clampFloat((density-threshold)*0.75, 0.10, 0.42)
	return fastMixColor(base, color.RGBA{R: 60, G: 106, B: 45, A: 255}, alpha)
}

func fastBlendRocks(settings fastPreviewSettings, base color.RGBA, wx, wy float64) color.RGBA {
	if !settings.rocks.enabled {
		return base
	}
	control := settings.rocks
	cell := 38 / math.Sqrt(clampFloat(control.frequency, 0.1, 6))
	cx := int64(math.Floor(wx / cell))
	cy := int64(math.Floor(wy / cell))
	if fastHashUnit(settings.seed, 82, cx, cy) > 0.12*clampFloat(control.size, 0.05, 4) {
		return base
	}
	centerX := (float64(cx) + fastHashUnit(settings.seed, 83, cx, cy)) * cell
	centerY := (float64(cy) + fastHashUnit(settings.seed, 84, cx, cy)) * cell
	if math.Hypot(wx-centerX, wy-centerY) > 1.7+control.size {
		return base
	}
	return fastMixColor(base, color.RGBA{R: 131, G: 125, B: 112, A: 255}, 0.55)
}

func fastEnemyPixel(settings fastPreviewSettings, wx, wy float64) (color.RGBA, bool) {
	if settings.noEnemies || !settings.enemyBases.enabled {
		return color.RGBA{}, false
	}
	if math.Hypot(wx, wy) < 185*math.Sqrt(clampFloat(settings.startingArea, 0.5, 6)) {
		return color.RGBA{}, false
	}
	control := settings.enemyBases
	cell := 360 / math.Sqrt(clampFloat(control.frequency, 0.1, 6))
	cx := int64(math.Floor(wx / cell))
	cy := int64(math.Floor(wy / cell))
	best := 0.0
	for dy := int64(-1); dy <= 1; dy++ {
		for dx := int64(-1); dx <= 1; dx++ {
			ix := cx + dx
			iy := cy + dy
			if fastHashUnit(settings.seed, 91, ix, iy) > 0.30*clampFloat(control.frequency, 0.1, 3) {
				continue
			}
			centerX := (float64(ix) + fastHashUnit(settings.seed, 92, ix, iy)) * cell
			centerY := (float64(iy) + fastHashUnit(settings.seed, 93, ix, iy)) * cell
			radius := 18 + 32*math.Sqrt(clampFloat(control.size, 0.05, 6))
			dist := math.Hypot(wx-centerX, wy-centerY)
			if dist < radius {
				best = math.Max(best, 1-dist/radius)
			}
		}
	}
	if best <= 0 {
		return color.RGBA{}, false
	}
	return color.RGBA{R: 156, G: 44, B: 42, A: 255}, true
}

func fastCliffPixel(settings fastPreviewSettings, wx, wy float64) (color.RGBA, bool) {
	if !settings.cliffs.enabled {
		return color.RGBA{}, false
	}
	frequency := clampFloat(settings.cliffs.frequency, 0.1, 6)
	continuity := clampFloat(settings.cliffs.size, 0.05, 8)
	ridge := math.Abs(fastFBM(settings.seed, 121, wx, wy, 110/math.Sqrt(frequency), 3, 0.55))
	gate := fastFBM(settings.seed, 122, wx, wy, 280, 2, 0.55)
	if ridge < 0.026*math.Sqrt(continuity) && gate > -0.05 {
		return color.RGBA{R: 154, G: 140, B: 120, A: 255}, true
	}
	return color.RGBA{}, false
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

func fastClimate(seed uint32, salt uint32, wx, wy, frequency, bias float64) float64 {
	frequency = clampFloat(frequency, 0.1, 6)
	value := 0.5 + fastFBM(seed, salt, wx, wy, 165/frequency, 4, 0.55)*0.38 + bias*0.35
	return clampFloat(value, 0, 1)
}

func fastJitterColor(c color.RGBA, amount float64) color.RGBA {
	scale := 1 + amount
	return color.RGBA{
		R: uint8(clampFloat(float64(c.R)*scale, 0, 255)),
		G: uint8(clampFloat(float64(c.G)*scale, 0, 255)),
		B: uint8(clampFloat(float64(c.B)*scale, 0, 255)),
		A: 255,
	}
}

func fastMixColor(base, over color.RGBA, alpha float64) color.RGBA {
	alpha = clampFloat(alpha, 0, 1)
	return color.RGBA{
		R: uint8(float64(base.R)*(1-alpha) + float64(over.R)*alpha),
		G: uint8(float64(base.G)*(1-alpha) + float64(over.G)*alpha),
		B: uint8(float64(base.B)*(1-alpha) + float64(over.B)*alpha),
		A: 255,
	}
}

func fastFBM(seed uint32, salt uint32, wx, wy, scale float64, octaves int, persistence float64) float64 {
	if scale < 1 {
		scale = 1
	}
	amp := 1.0
	sum := 0.0
	norm := 0.0
	for i := 0; i < octaves; i++ {
		sum += fastValueNoise(seed, salt+uint32(i)*13, wx, wy, scale) * amp
		norm += amp
		amp *= persistence
		scale *= 0.5
		if scale < 1 {
			scale = 1
		}
	}
	if norm == 0 {
		return 0
	}
	return sum / norm
}

func fastValueNoise(seed uint32, salt uint32, wx, wy, scale float64) float64 {
	x := wx / scale
	y := wy / scale
	x0 := math.Floor(x)
	y0 := math.Floor(y)
	tx := fastSmooth(x - x0)
	ty := fastSmooth(y - y0)
	ix := int64(x0)
	iy := int64(y0)
	a := fastHashSigned(seed, salt, ix, iy)
	b := fastHashSigned(seed, salt, ix+1, iy)
	c := fastHashSigned(seed, salt, ix, iy+1)
	d := fastHashSigned(seed, salt, ix+1, iy+1)
	return lerpFloat(lerpFloat(a, b, tx), lerpFloat(c, d, tx), ty)
}

func fastSmooth(t float64) float64 {
	return t * t * (3 - 2*t)
}

func fastHashSigned(seed uint32, salt uint32, x, y int64) float64 {
	return fastHashUnit(seed, salt, x, y)*2 - 1
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

func fastStartingPositions(value any) []factorioPoint {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return []factorioPoint{{}}
	}
	points := make([]factorioPoint, 0, len(items))
	for _, item := range items {
		point := fastMap(item)
		points = append(points, factorioPoint{
			x: fastNumber(point["x"], 0),
			y: fastNumber(point["y"], 0),
		})
	}
	if len(points) == 0 {
		return []factorioPoint{{}}
	}
	return points
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
	switch v := value.(type) {
	case json.Number:
		n, err := v.Float64()
		if err == nil {
			return n
		}
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		s := strings.TrimSpace(v)
		if n, ok := fastScaleValues[s]; ok {
			return n
		}
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return n
		}
	}
	return fallback
}
