package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
)

type factorioFulgoraChart struct {
	world  *factorioSpaceAgeEvaluator
	mu     sync.Mutex
	chunks map[[2]int][]factorioChartEntity
}

// Autoplace order: fulgurite, eight ruins, scrap, two sunk ruins, rock.
var fulgoraChartHalfBoxes = [...][2]float64{{1, .9}, {6.88, 4}, {1.21, 1.21}, {3.47, 2.66}, {2.78, 2.33}, {1.77, 1.49}, {1.56, 1.32}, {1.21, .86}, {.77, .68}, {.1, .1}, {1, 1}, {2.5, 1.72}, {.75, .75}}
var fulgoraChartColors = [...]color.RGBA{{194, 41, 116, 64}, {255, 255, 255, 128}, {0, 96, 145, 255}, {130, 130, 130, 128}, {125, 125, 125, 128}, {120, 120, 120, 128}, {120, 120, 120, 128}, {115, 115, 115, 128}, {110, 110, 110, 128}, {229, 229, 229, 255}, {129, 105, 78, 255}, {129, 105, 78, 255}, {129, 105, 78, 255}}

func fulgoraEntityProbabilities(s factorioFulgoraSample, x, y float32) [13]float32 {
	d, a := s.machineDensity, s.artificial
	m := a + .2*s.mesa*max(0, s.scrap)
	every := factorioPlaceEveryN(x, y, 15, 15, 1, 1)
	penalty := float32(factorioRandomPenaltyAt(int64(x), int64(y), 1, 1))
	sunk := min(.008, s.oilMask*-min(0, s.elevation-10)/80*max(s.scrap-1, 0))
	return [13]float32{
		min(.1, s.naturalMask*max(0, -s.scrap-s.rock-.8)),
		noiseBool(min(s.spots, (1-s.vaultStart)/2) < .015) * min(s.vaults, 1-s.startingMask),
		min(every, .1*a+.2*(d-1.5)) + max(0, 10*(1.5-s.distance)),
		.0001*a + .001*(d-4), .0001*a + .002*(d-3), .0001*a + .003*(d-2),
		.0005*a + .004*(d-1.5),
		.0001*(.1*s.naturalMask) + (.001+.006*(d-.3))*m,
		.0005*(.1*s.naturalMask+s.mesa) + (.005+.005*(d-.1))*m,
		s.scrapProbability, sunk * (penalty - .5), sunk * (penalty - .4),
		(1 - s.oilMask) * (s.naturalMask + s.mesa) * min(.05, .3-2.4+s.rock-.5*s.mixOil+.7*s.basisOil),
	}
}

func (g *factorioFulgoraChart) chunk(ctx context.Context, cx, cy int) ([]factorioChartEntity, error) {
	key := [2]int{cx, cy}
	g.mu.Lock()
	cached, ok := g.chunks[key]
	g.mu.Unlock()
	if ok {
		return cached, nil
	}
	var water [46 * 46]bool
	var probabilities [1024][13]float32
	for dy := -7; dy < 39; dy++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for dx := -7; dx < 39; dx++ {
			x, y := float64(cx*32+dx), float64(cy*32+dy)
			s := g.world.fulgora.sample(x, y)
			water[(dy+7)*46+dx+7] = strings.HasPrefix(s.tile.name, "oil-ocean")
			if dx >= 0 && dx < 32 && dy >= 0 && dy < 32 {
				probabilities[dy*32+dx] = fulgoraEntityProbabilities(s, float32(x), float32(y))
			}
		}
	}
	rng := newFactorioTaus88State(max(factorioMinSeedWord, uint32(int64(factorioSpotSeedBase)+7919*int64(cx)+7907*int64(cy))))
	var entities []factorioChartEntity
	for kind := range fulgoraChartHalfBoxes {
		if kind == 9 && !fastSpaceAgeControl(g.world.settings, "scrap").enabled {
			continue
		}
		for i := 1023; i >= 0; i-- {
			sunk := kind == 10 || kind == 11
			if !sunk && water[(i/32+7)*46+i%32+7] {
				continue
			}
			if float64(rng.next())/4294967296 >= float64(probabilities[i][kind]) {
				continue
			}
			x, y := float64(cx*32+i%32)+.5, float64(cy*32+i/32)+.5
			if kind != 2 && kind != 9 {
				x = float64(cx*32+i%32) + float64(rng.next()>>28)/16
				y = float64(cy*32+i/32) + float64(rng.next()>>28)/16
			}
			half := fulgoraChartHalfBoxes[kind]
			hx, hy := math.Trunc(half[0]*256)/256, math.Trunc(half[1]*256)/256
			allowed := true
			if !sunk {
				for by := int(math.Floor(y - hy)); by <= int(math.Floor(y+hy)); by++ {
					for bx := int(math.Floor(x - hx)); bx <= int(math.Floor(x+hx)); bx++ {
						if water[(by-cy*32+7)*46+bx-cx*32+7] {
							allowed = false
						}
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
func (g *factorioFulgoraChart) render(ctx context.Context, img *image.RGBA, x0, y0, tpp float64) error {
	for cy := int(math.Floor(y0 / 32)); cy < int(math.Ceil((y0+float64(img.Bounds().Dy())*tpp)/32)); cy++ {
		for cx := int(math.Floor(x0 / 32)); cx < int(math.Ceil((x0+float64(img.Bounds().Dx())*tpp)/32)); cx++ {
			entities, err := g.chunk(ctx, cx, cy)
			if err != nil {
				return err
			}
			for i := len(entities) - 1; i >= 0; i-- {
				ent := entities[i]
				if ent.kind == 9 && !factorioPreviewEntityDither(ent.x, ent.y, 1) {
					continue
				}
				half := fulgoraChartHalfBoxes[ent.kind]
				paintFactorioChartEntity(img, g.world.settings, ent.x, ent.y, [4]float64{-half[0], -half[1], half[0], half[1]}, fulgoraChartColors[ent.kind], x0, y0, tpp)
			}
		}
	}
	return nil
}
