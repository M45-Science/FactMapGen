package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
)

type factorioGlebaChart struct {
	world   *factorioSpaceAgeEvaluator
	enemies *glebaEnemyFields
	chunks  factorioChartChunkCache
}

var glebaChartBoxes = [...][4]float64{
	{-.5, -.5, .5, .5}, {-.5, -.5, .5, .5},
	{-.8, -.8, .8, .8}, {-.8, -.8, .8, .8}, {-.7, -.5, .7, .5},
	{-.9, -.4, .9, .8}, {-.6, -.4, .6, .6}, {-.7, -.6, .7, .6},
	{-.4, -.4, .4, .4}, {-.6, -.5, .6, .5}, {-.8, -.6, .8, .6},
	{-.9, -.6, .9, .6}, {-.6, -.8, .6, .8}, {-.1, -.1, .1, .1},
	{-.1, -.1, .1, .1},
	{-2.35, -2.35, 2.35, 2.35}, {-1.35, -1.35, 1.35, 1.35},
}

func glebaChartTileAllowed(kind int, tile string, restrict bool) bool {
	wet := strings.HasPrefix(tile, "wetland-")
	lake := tile == "gleba-deep-lake"
	switch {
	case kind < 2 || kind >= 10 && kind <= 12 || kind >= 15:
		return wet
	case kind == 13:
		return wet || lake
	case kind == 2 && restrict:
		return strings.Contains(tile, "yumako-soil")
	case kind == 3 && restrict:
		return strings.Contains(tile, "jellynut-soil")
	default:
		return !wet && !lake
	}
}

func (g *factorioGlebaChart) chunk(ctx context.Context, cx, cy int, raster *factorioChartRaster) ([]factorioChartEntity, error) {
	e := g.world
	key := [2]int{cx, cy}
	cached, ok := g.chunks.get(key)
	if ok {
		if raster != nil {
			return cached, raster.paintChunk(ctx, cx, cy, func(x, y int) color.RGBA {
				return e.gleba.sample(float64(x), float64(y)).tile.color
			})
		}
		return cached, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var tiles [38 * 38]string
	var fields [1024]glebaFeatureSample
	var enemies [1024][2]float32
	var enemyValues []float64
	if g.enemies != nil {
		var positions [1024]factorioSpotCandidate
		for i := range positions {
			positions[i] = factorioSpotCandidate{x: float64(cx*32 + i%32), y: float64(cy*32 + i/32)}
		}
		enemyValues = g.enemies.probabilities(positions[:])
	}
	baseX, baseY := cx*32, cy*32
	for dy := 0; dy < 32; dy++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for dx := 0; dx < 32; dx++ {
			x, y := baseX+dx, baseY+dy
			s := e.gleba.sample(float64(x), float64(y))
			tiles[(dy+3)*38+dx+3] = s.tile.name
			i := dy*32 + dx
			fields[i] = e.glebaFields.sample(s, float64(x), float64(y))
			if enemyValues != nil {
				enemies[i] = glebaSpawnerProbabilities(s, enemyValues[i])
			}
			if raster != nil {
				raster.paint(x, y, s.tile.color)
			}
		}
	}
	tileAt := func(x, y int) string {
		i := (y-baseY+3)*38 + x - baseX + 3
		if tiles[i] == "" {
			tiles[i] = e.gleba.sample(float64(x), float64(y)).tile.name
		}
		return tiles[i]
	}
	rng := newFactorioTaus88State(max(factorioMinSeedWord, uint32(int64(factorioSpotSeedBase)+7919*int64(cx)+7907*int64(cy))))
	groups := make([][]int, 0, 14)
	if e.settings.rocks.enabled {
		groups = append(groups, []int{0, 1})
	}
	if fastSpaceAgeControl(e.settings, "gleba_plants").enabled {
		for i := 2; i < 14; i++ {
			groups = append(groups, []int{i})
		}
	}
	if fastSpaceAgeControl(e.settings, "gleba_stone").enabled {
		groups = append(groups, []int{14})
	}
	if g.enemies != nil {
		groups = append(groups, []int{15}, []int{16})
	}
	prob := func(i, kind int) float32 {
		if kind >= 15 {
			return enemies[i][kind-15]
		}
		if kind < 2 {
			return fields[i].stromatolites[kind]
		}
		if kind == 14 {
			return fields[i].stoneProbability
		}
		return fields[i].plants[kind-2]
	}
	var entities []factorioChartEntity
	for _, group := range groups {
		for i := 1023; i >= 0; i-- {
			tile := tiles[(i/32+3)*38+i%32+3]
			kind := group[0]
			if !glebaChartTileAllowed(kind, tile, true) {
				continue
			}
			for _, k := range group[1:] {
				if prob(i, k) > prob(i, kind) {
					kind = k
				}
			}
			if float64(rng.next())/4294967296 >= float64(prob(i, kind)) {
				continue
			}
			x, y := float64(cx*32+i%32)+.5, float64(cy*32+i/32)+.5
			if kind != 14 {
				x = float64(cx*32+i%32) + float64(rng.next()>>28)/16
				y = float64(cy*32+i/32) + float64(rng.next()>>28)/16
			}
			box := glebaChartBoxes[kind]
			if kind == 16 {
				box = glebaChartBoxes[15]
			}
			if factorioPreviewExcludesEntity(e.settings.entityBounds, x, y, box) {
				continue
			}
			allowed := true
			for by := int(math.Floor(y + math.Trunc(box[1]*256)/256)); by <= int(math.Floor(y+math.Trunc(box[3]*256)/256)) && allowed; by++ {
				for bx := int(math.Floor(x + math.Trunc(box[0]*256)/256)); bx <= int(math.Floor(x+math.Trunc(box[2]*256)/256)); bx++ {
					if !glebaChartTileAllowed(kind, tileAt(bx, by), false) {
						allowed = false
						break
					}
				}
			}
			if allowed {
				entities = append(entities, factorioChartEntity{x, y, kind})
			}
		}
	}
	g.chunks.store(key, entities)
	return entities, nil
}

func (g *factorioGlebaChart) render(ctx context.Context, img *image.RGBA, x0, y0, tpp float64, raster *factorioChartRaster) error {
	chunk := func(ctx context.Context, cx, cy int) ([]factorioChartEntity, error) {
		return g.chunk(ctx, cx, cy, raster)
	}
	return renderFactorioChartChunks(ctx, img, x0, y0, tpp, chunk, func(entities []factorioChartEntity) {
		for i := len(entities) - 1; i >= 0; i-- {
			ent := entities[i]
			if ent.kind >= 15 {
				continue
			}
			ink := color.RGBA{48, 99, 48, 102}
			switch {
			case ent.kind < 2:
				ink = factorioRockMapColor
			case ent.kind < 4:
				ink = color.RGBA{255, 255, 255, 255}
			case ent.kind == 14:
				if !factorioPreviewEntityDither(ent.x, ent.y, 1) {
					continue
				}
				ink = color.RGBA{175, 155, 108, 255}
			}
			paintFactorioChartEntity(img, g.world.settings, ent.x, ent.y, glebaChartBoxes[ent.kind], ink, x0, y0, tpp)
		}
		// Hostile chart rectangles are deferred until all ordinary entities
		// have been drawn, so vegetation cannot tint the enemy markers.
		var accepted [][4]float64
		for i := len(entities) - 1; i >= 0; i-- {
			ent := entities[i]
			if ent.kind < 15 || !factorioEnemyChartAccept(&accepted, ent, glebaChartBoxes[15]) {
				continue
			}
			paintFactorioChartEntity(img, g.world.settings, ent.x, ent.y, glebaChartBoxes[ent.kind], factorioEnemyMapColor, x0, y0, tpp)
		}
	})
}
