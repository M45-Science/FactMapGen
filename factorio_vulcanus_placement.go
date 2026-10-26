package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
)

type factorioVulcanusChart struct {
	world    *factorioSpaceAgeEvaluator
	knockout func(float32, float32) float32
	mu       sync.Mutex
	chunks   map[[2]int][]factorioChartEntity
}

var vulcanusChartBoxes = [...][4]float64{
	{-.9, -.7, .9, .7}, {-1.2, -1.2, 1.2, 1.2}, {-1.2, -1.2, 1.2, 1.2}, {-1.2, -1.2, 1.2, 1.2}, {-.9, -.7, .9, .7},
	{-1.5, -1.1, 1.5, 1.1}, {-.75, -.75, .75, .75},
	{-.1, -.1, .1, .1}, {-.1, -.1, .1, .1}, {-.1, -.1, .1, .1},
	{-.5, -.6, .5, .4}, {-.5, -.6, .5, .4}, {-1.4, -1.4, 1.4, 1.4},
}

func (g *factorioVulcanusChart) probabilities(s factorioVulcanusSample, x, y float64) [14]float32 {
	aux, moisture, ash, mountain := float32(s.aux), float32(s.moisture), float32(s.ashlandsBiome), float32(s.mountainsBiome)
	rock := float32(g.world.vulcanus.rockNoise(x, y))
	knockout := g.knockout(float32(x), float32(y))
	common := -2.3 + 1.2*min(aux, 1-moisture) + rock - .5*knockout
	main := 2*(mountain-.5) + common
	penalty := float32(factorioRandomPenaltyAt(int64(x), int64(y), 1, 1))
	sulfur := float32(s.resources[3])
	faded := -penalty + 7*min(.2, main*noiseBool(sulfur < .3))
	tree := min(10*(ash-.9), -1.5+1.5*moisture+.5*noiseBool(moisture > .9)-.5*aux+.5*knockout)
	p := [14]float32{
		max(min(.05, 2*(mountain-.5)-2.1+1.2*min(aux, 1-moisture)+rock-.5*knockout), min(.05, 2*(ash-.5)+common)),
		-penalty + 10*min(.2, glebaClamp(main, .017, .05))*noiseBool(sulfur > 0),
		faded, -penalty + 7*min(.2, main*noiseBool(sulfur < .01)), faded / 2,
		min(.2*(1-.75*ash), -1.2+1.2*min(aux, -.1+1.1*moisture)+rock+.5*knockout),
		min(.2*(1-.5*ash), -1+1.2*min(aux, -.1+1.1*moisture)+rock+.5*knockout),
		0, 0, 0, tree / 16, tree, .025 * (noiseBool(sulfur > 0) + 2*sulfur),
		.5 * (rock + .5*aux - .5*moisture) * (1 - max(float32(s.basaltsBiome), ash)) * factorioPlaceEveryN(float32(x), float32(y), 21, 21, 0, 0),
	}
	resourcePenalty := float32(factorioRandomPenaltyAt(int64(x), int64(y), 1, .1))
	for i, name := range factorioVulcanusResourceNames {
		control := fastSpaceAgeControl(g.world.settings, name)
		if i < 3 {
			if control.enabled && control.size > 0 && control.richness > 0 {
				p[7+i] = 1000 * ((1+float32(s.resources[i]))*resourcePenalty - 1)
			}
		} else if !control.enabled || control.size <= 0 || control.richness <= 0 {
			p[12] = 0
		}
	}
	return p
}
func (g *factorioVulcanusChart) chunk(ctx context.Context, cx, cy int) ([]factorioChartEntity, error) {
	key := [2]int{cx, cy}
	g.mu.Lock()
	cached, ok := g.chunks[key]
	g.mu.Unlock()
	if ok {
		return cached, nil
	}
	var water [36 * 36]bool
	var probs [1024][14]float32
	for dy := -2; dy < 34; dy++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for dx := -2; dx < 34; dx++ {
			x, y := float64(cx*32+dx), float64(cy*32+dy)
			s := g.world.vulcanus.sample(x, y)
			water[(dy+2)*36+dx+2] = strings.HasPrefix(s.tile.name, "lava")
			if dx >= 0 && dx < 32 && dy >= 0 && dy < 32 {
				probs[dy*32+dx] = g.probabilities(s, x, y)
			}
		}
	}
	rng := newFactorioTaus88State(max(factorioMinSeedWord, uint32(int64(factorioSpotSeedBase)+7919*int64(cx)+7907*int64(cy))))
	// Crater cliffs have their own entity-autoplace order before all scenery.
	// Preserve its rolls and off-grid jitter even though crater segment geometry
	// is not yet drawn. Empty crater chunks still consume every land-tile roll.
	for i := 1023; i >= 0; i-- {
		if !water[(i/32+2)*36+i%32+2] && float64(rng.next())/4294967296 < float64(probs[i][13]) {
			rng.next()
			rng.next()
		}
	}
	groups := [][]int{{0}, {1}, {2}, {3}, {4}, {5}, {6}, {7, 8, 9}, {10}, {11}, {12}}
	var entities []factorioChartEntity
	for _, group := range groups {
		if group[0] < 7 && !g.world.settings.rocks.enabled {
			continue
		}
		for i := 1023; i >= 0; i-- {
			if water[(i/32+2)*36+i%32+2] {
				continue
			}
			kind := group[0]
			for _, k := range group[1:] {
				if probs[i][k] > probs[i][kind] {
					kind = k
				}
			}
			if float64(rng.next())/4294967296 >= float64(probs[i][kind]) {
				continue
			}
			x, y := float64(cx*32+i%32)+.5, float64(cy*32+i/32)+.5
			resource := kind >= 7 && kind <= 9 || kind == 12
			if !resource {
				x = float64(cx*32+i%32) + float64(rng.next()>>28)/16
				y = float64(cy*32+i/32) + float64(rng.next()>>28)/16
			}
			box := vulcanusChartBoxes[kind]
			allowed := true
			for by := int(math.Floor(y + math.Trunc(box[1]*256)/256)); by <= int(math.Floor(y+math.Trunc(box[3]*256)/256)); by++ {
				for bx := int(math.Floor(x + math.Trunc(box[0]*256)/256)); bx <= int(math.Floor(x+math.Trunc(box[2]*256)/256)); bx++ {
					if water[(by-cy*32+2)*36+bx-cx*32+2] {
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
func (g *factorioVulcanusChart) render(ctx context.Context, img *image.RGBA, x0, y0, tpp float64) error {
	for cy := int(math.Floor(y0 / 32)); cy < int(math.Ceil((y0+float64(img.Bounds().Dy())*tpp)/32)); cy++ {
		for cx := int(math.Floor(x0 / 32)); cx < int(math.Ceil((x0+float64(img.Bounds().Dx())*tpp)/32)); cx++ {
			entities, err := g.chunk(ctx, cx, cy)
			if err != nil {
				return err
			}
			for i := len(entities) - 1; i >= 0; i-- {
				ent := entities[i]
				ink := factorioRockMapColor
				if ent.kind >= 7 && ent.kind <= 9 {
					if !factorioPreviewEntityDither(ent.x, ent.y, 1) {
						continue
					}
					ink = [3]color.RGBA{{97, 85, 149, 255}, {0, 0, 0, 255}, {204, 178, 178, 255}}[ent.kind-7]
				} else if ent.kind == 10 || ent.kind == 11 {
					ink = color.RGBA{48, 99, 48, 102}
				} else if ent.kind == 12 {
					ink = color.RGBA{198, 198, 25, 255}
				}
				paintFactorioChartEntity(img, g.world.settings, ent.x, ent.y, vulcanusChartBoxes[ent.kind], ink, x0, y0, tpp)
			}
		}
	}
	return nil
}
