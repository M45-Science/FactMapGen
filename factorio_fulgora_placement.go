package main

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
)

type factorioFulgoraChart struct {
	world  *factorioSpaceAgeEvaluator
	chunks factorioChartChunkCache
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
	// Scrap richness is proportional to 1 + subnoise. Factorio discards
	// nonpositive amounts, but the chunk still spends its per-tile roll.
	scrap := s.scrapProbability * noiseBool(s.subnoise > -1)
	return [13]float32{
		min(.1, s.naturalMask*max(0, -s.scrap-s.rock-.8)),
		noiseBool(min(s.spots, (1-s.vaultStart)/2) < .015) * min(s.vaults, 1-s.startingMask),
		min(every, .1*a+.2*(d-1.5)) + max(0, 10*(1.5-s.distance)),
		.0001*a + .001*(d-4), .0001*a + .002*(d-3), .0001*a + .003*(d-2),
		.0005*a + .004*(d-1.5),
		.0001*(.1*s.naturalMask) + (.001+.006*(d-.3))*m,
		.0005*(.1*s.naturalMask+s.mesa) + (.005+.005*(d-.1))*m,
		scrap, sunk * (penalty - .5), sunk * (penalty - .4),
		(1 - s.oilMask) * (s.naturalMask + s.mesa) * min(.05, .3-2.4+s.rock-.5*s.mixOil+.7*s.basisOil),
	}
}

func (g *factorioFulgoraChart) chunk(ctx context.Context, cx, cy int, raster *factorioChartRaster) ([]factorioChartEntity, error) {
	e := g.world.fulgora
	key := [2]int{cx, cy}
	cached, ok := g.chunks.get(key)
	if ok {
		if raster != nil {
			return cached, raster.paintChunk(ctx, cx, cy, func(x, y int) color.RGBA {
				return e.sample(float64(x), float64(y)).tile.color
			})
		}
		return cached, nil
	}
	const (
		unknown = iota
		water
		land
	)
	var terrain [46 * 46]uint8
	var probabilities [1024][13]float32
	baseX, baseY := cx*32, cy*32
	for dy := 0; dy < 32; dy++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for dx := 0; dx < 32; dx++ {
			x, y := baseX+dx, baseY+dy
			s := e.sample(float64(x), float64(y))
			if strings.HasPrefix(s.tile.name, "oil-ocean") {
				terrain[(dy+7)*46+dx+7] = water
			} else {
				terrain[(dy+7)*46+dx+7] = land
			}
			probabilities[dy*32+dx] = fulgoraEntityProbabilities(s, float32(x), float32(y))
			if raster != nil {
				raster.paint(x, y, s.tile.color)
			}
		}
	}
	isWater := func(x, y int) bool {
		i := (y-baseY+7)*46 + x - baseX + 7
		if terrain[i] == unknown {
			s := e.sample(float64(x), float64(y))
			if strings.HasPrefix(s.tile.name, "oil-ocean") {
				terrain[i] = water
			} else {
				terrain[i] = land
			}
		}
		return terrain[i] == water
	}
	rng := newFactorioTaus88State(max(factorioMinSeedWord, uint32(int64(factorioSpotSeedBase)+7919*int64(cx)+7907*int64(cy))))
	var entities []factorioChartEntity
	for kind := range fulgoraChartHalfBoxes {
		if kind == 9 && !fastSpaceAgeControl(g.world.settings, "scrap").enabled {
			continue
		}
		for i := 1023; i >= 0; i-- {
			sunk := kind == 10 || kind == 11
			if !sunk && terrain[(i/32+7)*46+i%32+7] == water {
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
			box := [4]float64{-half[0], -half[1], half[0], half[1]}
			if factorioPreviewExcludesEntity(g.world.settings.entityBounds, x, y, box) {
				continue
			}
			hx, hy := math.Trunc(half[0]*256)/256, math.Trunc(half[1]*256)/256
			allowed := true
			if !sunk {
				for by := int(math.Floor(y - hy)); by <= int(math.Floor(y+hy)) && allowed; by++ {
					for bx := int(math.Floor(x - hx)); bx <= int(math.Floor(x+hx)); bx++ {
						if isWater(bx, by) {
							allowed = false
							break
						}
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
func (g *factorioFulgoraChart) render(ctx context.Context, img *image.RGBA, x0, y0, tpp float64, raster *factorioChartRaster) error {
	chunk := func(ctx context.Context, cx, cy int) ([]factorioChartEntity, error) {
		return g.chunk(ctx, cx, cy, raster)
	}
	return renderFactorioChartChunks(ctx, img, x0, y0, tpp, chunk, func(entities []factorioChartEntity) {
		for i := len(entities) - 1; i >= 0; i-- {
			ent := entities[i]
			if ent.kind == 9 && !factorioPreviewEntityDither(ent.x, ent.y, 1) {
				continue
			}
			half := fulgoraChartHalfBoxes[ent.kind]
			paintFactorioChartEntity(img, g.world.settings, ent.x, ent.y, [4]float64{-half[0], -half[1], half[0], half[1]}, fulgoraChartColors[ent.kind], x0, y0, tpp)
		}
	})
}
