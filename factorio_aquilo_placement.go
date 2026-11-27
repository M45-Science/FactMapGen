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
var factorioAquiloEntityNames = [...]string{"fluorine-vent", "lithium-brine", "lithium-iceberg-huge", "lithium-iceberg-big", "crude-oil"}
var factorioAquiloEntityHalfBoxes = [...][2]float64{{1.4, 1.4}, {1.4, 1.4}, {1.85, 1.25}, {1, .75}, {1.4, 1.4}}
var factorioAquiloEntityColors = [...]color.RGBA{{0, 204, 255, 255}, {178, 255, 153, 255}, {168, 188, 211, 255}, {168, 188, 211, 255}, {198, 51, 196, 255}}

// The terrain pass can supply land checks for the visible area at unit zoom.
type factorioAquiloLand struct {
	bounds image.Rectangle
	pixels []bool
}

func (e *factorioAquiloEvaluator) chartChunk(ctx context.Context, cx, cy int, visible factorioAquiloLand) ([]factorioChartEntity, error) {
	key := [2]int{cx, cy}
	if cached, ok := e.chunks.get(key); ok {
		return cached, nil
	}

	const (
		unknown = iota
		water
		land
	)
	var terrain [36 * 36]uint8
	var wells [1024][3]float32
	var peaks [1024]float32
	baseX, baseY := cx*32, cy*32
	terrainAt := func(x, y int, islands float32) bool {
		p := image.Pt(x, y)
		if p.In(visible.bounds) {
			return visible.pixels[(y-visible.bounds.Min.Y)*visible.bounds.Dx()+x-visible.bounds.Min.X]
		}
		return e.terrain(float32(x), float32(y), islands).land
	}
	for dy := 0; dy < 32; dy++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for dx := 0; dx < 32; dx++ {
			i := dy*32 + dx
			x, y := baseX+dx, baseY+dy
			islands, p := e.wellFields(float32(x), float32(y), true)
			wells[i] = p
			if terrainAt(x, y, islands) {
				terrain[(dy+2)*36+dx+2] = land
				peaks[i] = noiseAbs(e.peaks(float32(x), float32(y)))
			} else {
				terrain[(dy+2)*36+dx+2] = water
			}
		}
	}
	isLand := func(x, y int) bool {
		i := (y-baseY+2)*36 + x - baseX + 2
		if terrain[i] == unknown {
			islands, _ := e.wellFields(float32(x), float32(y), false)
			if terrainAt(x, y, islands) {
				terrain[i] = land
			} else {
				terrain[i] = water
			}
		}
		return terrain[i] == land
	}

	word := uint32(int64(factorioSpotSeedBase) + 7919*int64(cx) + 7907*int64(cy))
	rng := newFactorioTaus88State(max(factorioMinSeedWord, word))
	var result []factorioChartEntity
	for kind := range factorioAquiloEntityNames {
		iceberg := kind == 2 || kind == 3
		half := factorioAquiloEntityHalfBoxes[kind]
		box := [4]float64{-half[0], -half[1], half[0], half[1]}
		for i := 1023; i >= 0; i-- {
			x, y := baseX+i%32, baseY+i/32
			if iceberg && !isLand(x, y) {
				continue
			}
			var prob float32
			switch kind {
			case 0:
				prob = wells[i][2]
			case 1:
				prob = wells[i][1]
			case 2:
				prob = (peaks[i] - .8) * .1
			case 3:
				prob = (peaks[i] - .7) * .8
			case 4:
				prob = wells[i][0]
			}
			if float64(rng.next())/4294967296 >= float64(prob) {
				continue
			}
			px, py := float64(x)+.5, float64(y)+.5
			if iceberg {
				px = float64(x) + float64(rng.next()>>28)/16
				py = float64(y) + float64(rng.next()>>28)/16
			}
			if factorioPreviewExcludesEntity(e.settings.entityBounds, px, py, box) {
				continue
			}
			allowed := true
			if iceberg {
				for by := int(math.Floor(py - half[1])); by <= int(math.Floor(py+half[1])) && allowed; by++ {
					for bx := int(math.Floor(px - half[0])); bx <= int(math.Floor(px+half[0])); bx++ {
						if !isLand(bx, by) {
							allowed = false
							break
						}
					}
				}
			}
			if allowed {
				result = append(result, factorioChartEntity{x: px, y: py, kind: kind})
			}
		}
	}
	e.chunks.store(key, result)
	return result, nil
}

func (e *factorioAquiloEvaluator) renderEntities(ctx context.Context, img *image.RGBA, x0, y0, tpp float64, visible factorioAquiloLand) error {
	chunk := func(ctx context.Context, cx, cy int) ([]factorioChartEntity, error) {
		return e.chartChunk(ctx, cx, cy, visible)
	}
	// Preserve the reverse scan across chunks as well as within each chunk.
	// At wide zooms, two clipped footprints can still cover one screen pixel.
	return renderFactorioChartChunksOrdered(ctx, img, x0, y0, tpp, true, chunk, func(entities []factorioChartEntity) {
		for i := len(entities) - 1; i >= 0; i-- {
			ent := entities[i]
			if (ent.kind == 2 || ent.kind == 3) && !e.settings.rocks.enabled {
				continue
			}
			half := factorioAquiloEntityHalfBoxes[ent.kind]
			box := [4]float64{-half[0], -half[1], half[0], half[1]}
			paintFactorioChartEntity(img, e.settings, ent.x, ent.y, box, factorioAquiloEntityColors[ent.kind], x0, y0, tpp)
		}
	})
}
