package main

import (
	"context"
	"image"
	"image/color"
	"math"
)

// The order and PRNG consumption below follow generateEntities in the 2.1.23
// binary. Each distinct autoplace order flushes its own tile pass, sharing one
// chunk PRNG. Resources snap to tile centres; off-grid entities consume two
// additional draws and truncate positions to sixteenths of a tile.
type factorioAquiloEntity struct {
	factorioPoint
	kind int
}

var factorioAquiloEntityNames = [...]string{"fluorine-vent", "lithium-brine", "lithium-iceberg-huge", "lithium-iceberg-big", "crude-oil"}
var factorioAquiloEntityHalfBoxes = [...][2]float64{{1.4, 1.4}, {1.4, 1.4}, {1.85, 1.25}, {1, .75}, {1.4, 1.4}}
var factorioAquiloEntityColors = [...]color.RGBA{{0, 204, 255, 255}, {178, 255, 153, 255}, {168, 188, 211, 255}, {168, 188, 211, 255}, {198, 51, 196, 255}}

func (e *factorioAquiloEvaluator) entityCandidates(ctx context.Context, x0, y0, x1, y1 float64) ([]factorioAquiloEntity, error) {
	var result []factorioAquiloEntity
	cache := map[[2]int]factorioAquiloSample{}
	sample := func(x, y int) factorioAquiloSample {
		key := [2]int{x, y}
		if v, ok := cache[key]; ok {
			return v
		}
		v := e.sample(float64(x), float64(y))
		cache[key] = v
		return v
	}
	land := func(x, y int) bool {
		n := sample(x, y).tile.name
		return n != "brash-ice" && n != "ammoniacal-ocean" && n != "ammoniacal-ocean-2"
	}
	for cy := int(math.Floor(y0 / 32)); cy < int(math.Ceil(y1/32)); cy++ {
		for cx := int(math.Floor(x0 / 32)); cx < int(math.Ceil(x1/32)); cx++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			clear(cache)
			word := uint32(int64(factorioSpotSeedBase) + 7919*int64(cx) + 7907*int64(cy))
			rng := newFactorioTaus88State(max(factorioMinSeedWord, word))
			for kind := range factorioAquiloEntityNames {
				for i := 1023; i >= 0; i-- {
					x, y := cx*32+i%32, cy*32+i/32
					if (kind == 2 || kind == 3) && !land(x, y) {
						continue
					}
					v := sample(x, y)
					var prob float32
					switch kind {
					case 0:
						prob = v.wells[2]
					case 1:
						prob = v.wells[1]
					case 2:
						prob = (noiseAbs(e.peaks(float32(x), float32(y))) - .8) * .1
					case 3:
						prob = (noiseAbs(e.peaks(float32(x), float32(y))) - .7) * .8
					case 4:
						prob = v.wells[0]
					}
					if float64(rng.next())/4294967296 >= float64(prob) {
						continue
					}
					px, py := float64(x)+.5, float64(y)+.5
					if kind == 2 || kind == 3 {
						px = float64(x) + float64(rng.next()>>28)/16
						py = float64(y) + float64(rng.next()>>28)/16
					}
					box := factorioAquiloEntityHalfBoxes[kind]
					allowed := true
					for by := int(math.Floor(py - box[1])); by <= int(math.Floor(py+box[1])); by++ {
						for bx := int(math.Floor(px - box[0])); bx <= int(math.Floor(px+box[0])); bx++ {
							if (kind == 2 || kind == 3) && !land(bx, by) {
								allowed = false
							}
						}
					}
					if allowed {
						result = append(result, factorioAquiloEntity{factorioPoint: factorioPoint{x: px, y: py}, kind: kind})
					}
				}
			}
		}
	}
	return result, nil
}

func (e *factorioAquiloEvaluator) renderEntities(ctx context.Context, img *image.RGBA, x0, y0, tpp float64) error {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	entities, err := e.entityCandidates(ctx, x0-32, y0-32, x0+float64(w)*tpp+32, y0+float64(h)*tpp+32)
	if err != nil {
		return err
	}
	// Preview candidates are charted in reverse order without the world's
	// final iceberg/entity collision pass. Use the real collision footprint,
	// clipped to its generation chunk, just as for Nauvis natural entities.
	for i := len(entities) - 1; i >= 0; i-- {
		ent := entities[i]
		if (ent.kind == 2 || ent.kind == 3) && !e.settings.rocks.enabled {
			continue
		}
		half := factorioAquiloEntityHalfBoxes[ent.kind]
		box := [4]float64{-half[0], -half[1], half[0], half[1]}
		paintFactorioChartEntity(img, e.settings, ent.x, ent.y, box, factorioAquiloEntityColors[ent.kind], x0, y0, tpp)
	}
	return nil
}
