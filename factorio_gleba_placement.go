package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
)

type factorioGlebaChart struct {
	world   *factorioSpaceAgeEvaluator
	enemies *glebaEnemyFields
	mu      sync.Mutex
	chunks  map[[2]int][]factorioChartEntity
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

func (g *factorioGlebaChart) chunk(ctx context.Context, cx, cy int) ([]factorioChartEntity, error) {
	key := [2]int{cx, cy}
	g.mu.Lock()
	cached, ok := g.chunks[key]
	g.mu.Unlock()
	if ok {
		return cached, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e := g.world
	var tiles [38 * 38]string
	var fields [1024]glebaFeatureSample
	var samples [1024]factorioGlebaSample
	var positions [1024]factorioSpotCandidate
	for dy := -3; dy < 35; dy++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for dx := -3; dx < 35; dx++ {
			x, y := float64(cx*32+dx), float64(cy*32+dy)
			s := e.gleba.sample(x, y)
			tiles[(dy+3)*38+dx+3] = s.tile.name
			if dx >= 0 && dx < 32 && dy >= 0 && dy < 32 {
				fields[dy*32+dx] = e.glebaFields.sample(s, x, y)
				samples[dy*32+dx] = s
				positions[dy*32+dx] = factorioSpotCandidate{x: x, y: y}
			}
		}
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
	var enemies [1024][2]float32
	if g.enemies != nil {
		values := g.enemies.probabilities(positions[:])
		for i := range enemies {
			enemies[i] = glebaSpawnerProbabilities(samples[i], values[i])
		}
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
			allowed := true
			for by := int(math.Floor(y + math.Trunc(box[1]*256)/256)); by <= int(math.Floor(y+math.Trunc(box[3]*256)/256)); by++ {
				for bx := int(math.Floor(x + math.Trunc(box[0]*256)/256)); bx <= int(math.Floor(x+math.Trunc(box[2]*256)/256)); bx++ {
					if !glebaChartTileAllowed(kind, tiles[(by-cy*32+3)*38+bx-cx*32+3], false) {
						allowed = false
					}
				}
			}
			if allowed {
				entities = append(entities, factorioChartEntity{x, y, kind})
			}
		}
	}
	g.mu.Lock()
	if g.chunks == nil || len(g.chunks) >= factorioChartChunkCacheLimit {
		g.chunks = make(map[[2]int][]factorioChartEntity)
	}
	g.chunks[key] = entities
	g.mu.Unlock()
	return entities, nil
}

func (g *factorioGlebaChart) render(ctx context.Context, img *image.RGBA, x0, y0, tpp float64) error {
	for cy := int(math.Floor(y0 / 32)); cy < int(math.Ceil((y0+float64(img.Bounds().Dy())*tpp)/32)); cy++ {
		for cx := int(math.Floor(x0 / 32)); cx < int(math.Ceil((x0+float64(img.Bounds().Dx())*tpp)/32)); cx++ {
			entities, err := g.chunk(ctx, cx, cy)
			if err != nil {
				return err
			}
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
		}
	}
	return nil
}
