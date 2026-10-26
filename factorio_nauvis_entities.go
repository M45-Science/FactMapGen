package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"sync"
)

// generateEntities visits orders separately and tiles in reverse row order.
// Fish (not charted), rocks, twenty tree prototypes, resources and enemies
// share the stream. Hostile chart rectangles are deferred until the end.
// The preview draws these candidates in reverse; it does not apply the world's
// tree/rock entity-collision pass. Tile collisions still reject candidates.
type factorioNauvisNaturalEvaluator struct {
	settings   fastPreviewSettings
	nauvis     *factorioNauvisEvaluator
	trees      *factorioTreeEvaluator
	rocks      *factorioRockEvaluator
	enemies    *factorioEnemyEvaluator
	enemyMu    sync.Mutex
	resources  *factorioResourceEvaluator
	resourceMu sync.Mutex
	mu         sync.Mutex
	chunks     map[[2]int][]factorioChartEntity
}

var factorioNauvisNaturalBoxes = [...][4]float64{
	{-1.5, -1.1, 1.5, 1.1}, {-1, -.9, 1, 1}, {-.75, -.75, .75, .75},
}
var factorioNauvisTreeOrder = [...]int{2, 1, 0, 5, 3, 4, 7, 6, 8, 11, 10, 9, 14, 13, 12, 15, 16, 17, 18, 19}

func factorioNauvisNaturalBox(kind int, mapgen bool) [4]float64 {
	if kind >= 29 {
		if kind == 33 {
			return [4]float64{-1.4, -1.4, 1.4, 1.4}
		}
		return [4]float64{-.1, -.1, .1, .1}
	}
	if kind >= 23 {
		if kind < 25 {
			if mapgen {
				return [4]float64{-3.7, -3.2, 3.7, 3.2}
			}
			return [4]float64{-2.2, -2.2, 2.2, 2.2}
		}
		box := [4][4]float64{{-.9, -.8, .9, .8}, {-1.1, -1, 1.1, 1}, {-1.4, -1.2, 1.4, 1.2}, {-1.4, -1.2, 1.4, 1.2}}[kind-25]
		if mapgen {
			box[0]--
			box[1]--
			box[2]++
			box[3]++
		}
		return box
	}
	if kind < 3 {
		return factorioNauvisNaturalBoxes[kind]
	}
	if kind >= 18 {
		if kind == 22 {
			return [4]float64{-.4, -.8, .4, .2}
		}
		return [4]float64{-.6, -.6, .6, .6}
	}
	if mapgen {
		return [4]float64{-.8, -.8, .8, .8}
	}
	return [4]float64{-.4, -.4, .4, .4}
}

func (e *factorioTreeEvaluator) probabilitiesAtSample(x, y float64, s factorioNauvisSample) [20]float64 {
	var values [20]float64
	p := e.pointAtSample(x, y, s)
	if p.cutoutFaded <= 0 {
		return values
	}
	for i, f := range e.fields {
		cheap := factorioTreeCheap(f, p)
		if cheap+f.maxNoise > 0 {
			values[i] = max(0, min(f.species.cap, p.cutoutFaded, cheap+f.noise(x, y)))
		}
	}
	noise := e.fields[13].noise(x, y)
	size := p.distanceTerm + e.fields[13].sizeTerm
	extended := min(.1, p.cutoutFaded, min(0, factorioAsymmetricRamps(s.temperature, [4]float64{13, 14, 16, 17}), factorioAsymmetricRamps(s.moisture, [4]float64{.05, .15, .3, .4}))+size+noise)
	desert := min(.1, p.cutoutFaded, min(0, factorioAsymmetricRamps(s.moisture, [4]float64{-.2, -.1, .1, .2}))+size+min(noise, e.desertNoise(x, y)+.2))
	dry := .2 * clampFloat(1-p.smallNoise, 0, 2)
	values[15] = dry * max(values[7], values[10], values[13], extended, desert)
	values[16] = dry * max(values[2], values[9], values[12], values[8], values[11], values[14])
	values[17] = dry * max(values[2], values[9], values[12])
	values[18] = max(dry*max(values[15], values[3], values[6], values[4], values[1]), values[15])
	values[19] = dry * max(values[0], values[8], values[11], values[14])
	return values
}

func (e *factorioNauvisNaturalEvaluator) chunk(ctx context.Context, cx, cy int) ([]factorioChartEntity, error) {
	key := [2]int{cx, cy}
	e.mu.Lock()
	cached, ok := e.chunks[key]
	e.mu.Unlock()
	if ok {
		return cached, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var water [40 * 40]bool
	var probabilities [1024][35]float64
	var richness [1024][6]float64
	var positions [1024]factorioSpotCandidate
	for dy := -4; dy < 36; dy++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for dx := -4; dx < 36; dx++ {
			x, y := float64(cx*32+dx), float64(cy*32+dy)
			s := e.nauvis.sample(x, y)
			wet := e.nauvis.terrainTileFast(s, x, y).water || fastOutOfMapBounds(e.settings, x, y)
			water[(dy+4)*40+dx+4] = wet
			if dx >= 0 && dx < 32 && dy >= 0 && dy < 32 {
				positions[dy*32+dx] = factorioSpotCandidate{x: x, y: y}
			}
			if wet || dx < 0 || dx >= 32 || dy < 0 || dy >= 32 {
				continue
			}
			p := &probabilities[dy*32+dx]
			if e.trees != nil {
				v := e.trees.probabilitiesAtSample(x, y, s)
				copy(p[3:], v[:])
			}
			if e.rocks != nil {
				density := e.rocks.rockDensityAt(x, y)
				band := factorioRangeSelectBase(s.moisture, .35, 1, .2, -10, 0)
				p[0] = .07 * e.settings.rocks.size * (band + density - 1.7)
				p[1] = .17 * e.settings.rocks.size * (band + density - 1.6)
				sand := min(factorioRangeSelectBase(s.aux, .3, 1, .3, -10, 0), factorioRangeSelectBase(s.moisture, 0, .3, .2, -10, 0))
				p[2] = .1 * e.settings.rocks.size * (sand + density - 1.6)
			}
		}
	}
	if e.enemies != nil {
		e.enemyMu.Lock()
		for kind, prototype := range factorioEnemyOverviewPrototypes {
			values := e.enemies.prototypeValues(positions[:], prototype)
			for i, v := range values {
				probabilities[i][kind+23] = v
			}
		}
		e.enemies.trimRegionCache(fastPreviewMaxRegionsPerField)
		e.enemyMu.Unlock()
	}
	var solid, later []int
	if e.resources != nil {
		e.resourceMu.Lock()
		for j := range e.resources.fields {
			f := &e.resources.fields[j]
			kind := 29 + f.params.patchSetIndex
			if kind < 33 {
				solid = append(solid, kind)
			} else {
				later = append(later, kind)
			}
			for i, p := range positions {
				v := f.fieldAt(p.x, p.y)
				probabilities[i][kind] = clampFloat(v, 0, 1)
				richness[i][kind-29] = v * f.regular.control.richness
				if kind == 33 {
					probabilities[i][kind] *= max(0, e.resources.oilRandomPenaltyAt(int64(p.x), int64(p.y)))
					richness[i][4] = (v*48 + 220000) * f.regular.control.richness
				}
			}
		}
		e.resources.trimRegionCaches(fastPreviewMaxRegionsPerField)
		e.resourceMu.Unlock()
	}
	rng := newFactorioTaus88State(max(factorioMinSeedWord, uint32(int64(factorioSpotSeedBase)+7919*int64(cx)+7907*int64(cy))))
	// Fish has an empty order, is restricted to water, and is aligned to the grid.
	if e.settings.fish {
		for i := 1023; i >= 0; i-- {
			if water[(i/32+4)*40+i%32+4] {
				rng.next()
			}
		}
	}
	groups := make([][]int, 0, 22)
	if e.rocks != nil {
		groups = append(groups, []int{0}, []int{1, 2})
	}
	if e.trees != nil {
		for _, v := range factorioNauvisTreeOrder {
			groups = append(groups, []int{v + 3})
		}
	}
	if len(solid) > 0 {
		groups = append(groups, solid)
	}
	if e.enemies != nil {
		groups = append(groups, []int{23, 24}, []int{25, 26, 27, 28})
	}
	if len(later) > 0 {
		groups = append(groups, later)
	}
	var entities []factorioChartEntity
	for _, group := range groups {
		for i := 1023; i >= 0; i-- {
			if water[(i/32+4)*40+i%32+4] {
				continue
			}
			kind := group[0]
			for _, k := range group[1:] {
				if probabilities[i][k] > probabilities[i][kind] || k >= 29 && probabilities[i][k] == probabilities[i][kind] && richness[i][k-29] > richness[i][kind-29] {
					kind = k
				}
			}
			if float64(rng.next())/4294967296 >= probabilities[i][kind] {
				continue
			}
			if kind >= 29 && richness[i][kind-29] <= 0 {
				continue
			}
			x, y := float64(cx*32+i%32)+.5, float64(cy*32+i/32)+.5
			if kind < 23 || kind >= 25 && kind < 29 {
				x = float64(cx*32+i%32) + float64(rng.next()>>28)/16
				y = float64(cy*32+i/32) + float64(rng.next()>>28)/16
			}
			box := factorioNauvisNaturalBox(kind, true)
			allowed := true
			for by := int(math.Floor(y + math.Trunc(box[1]*256)/256)); by <= int(math.Floor(y+math.Trunc(box[3]*256)/256)); by++ {
				for bx := int(math.Floor(x + math.Trunc(box[0]*256)/256)); bx <= int(math.Floor(x+math.Trunc(box[2]*256)/256)); bx++ {
					if water[(by-cy*32+4)*40+bx-cx*32+4] {
						allowed = false
					}
				}
			}
			if allowed {
				entities = append(entities, factorioChartEntity{x, y, kind})
			}
		}
	}
	e.mu.Lock()
	if e.chunks == nil || len(e.chunks) >= factorioChartChunkCacheLimit {
		e.chunks = make(map[[2]int][]factorioChartEntity)
	}
	e.chunks[key] = entities
	e.mu.Unlock()
	return entities, nil
}

func (e *factorioNauvisNaturalEvaluator) render(ctx context.Context, img *image.RGBA, originX, originY, tpp float64) error {
	return e.renderLayer(ctx, img, originX, originY, tpp, false)
}

func (e *factorioNauvisNaturalEvaluator) renderEnemies(ctx context.Context, img *image.RGBA, originX, originY, tpp float64) error {
	return e.renderLayer(ctx, img, originX, originY, tpp, true)
}

func (e *factorioNauvisNaturalEvaluator) renderLayer(ctx context.Context, img *image.RGBA, originX, originY, tpp float64, enemies bool) error {
	for cy := int(math.Floor(originY / 32)); cy < int(math.Ceil((originY+float64(img.Bounds().Dy())*tpp)/32)); cy++ {
		for cx := int(math.Floor(originX / 32)); cx < int(math.Ceil((originX+float64(img.Bounds().Dx())*tpp)/32)); cx++ {
			entities, err := e.chunk(ctx, cx, cy)
			if err != nil {
				return err
			}
			var accepted [][4]float64
			for i := len(entities) - 1; i >= 0; i-- {
				ent := entities[i]
				if (ent.kind >= 23 && ent.kind < 29) != enemies {
					continue
				}
				ink := color.RGBA{48, 99, 48, 102}
				if enemies {
					if !factorioEnemyChartAccept(&accepted, ent, factorioNauvisNaturalBox(ent.kind, true)) {
						continue
					}
					ink = factorioEnemyMapColor
				} else if ent.kind >= 29 {
					if ent.kind != 33 && !factorioPreviewEntityDither(ent.x, ent.y, 1) {
						continue
					}
					ink = factorioResourceCatalog[ent.kind-29].mapColor
				} else if ent.kind < 3 {
					ink = factorioRockMapColor
				}
				paintFactorioChartEntity(img, e.settings, ent.x, ent.y, factorioNauvisNaturalBox(ent.kind, false), ink, originX, originY, tpp)
			}
		}
	}
	return nil
}
