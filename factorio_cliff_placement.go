// SPDX-License-Identifier: AGPL-3.0-or-later
// Adapted from FactorioMapWebUI cliffs/placement.rs; see docs/upstream-rendering.md.
package main

import (
	"context"
	"math"
)

func factorioCliffCellCode(l, r, t, b int) int {
	return (l&3)<<6 | (r&3)<<4 | (t&3)<<2 | b&3
}

// Repair shared edges once in row order. A retry clears the eight outer
// edges of the corner cells, retaining mutations from the first attempt.
func factorioRepairCliffChunk(v, h *[72]int) {
	for retry := 0; retry < 2; retry++ {
		if retry != 0 {
			for _, i := range []int{0, 8, 63, 71} {
				v[i] = 0
			}
			for _, i := range []int{0, 7, 64, 71} {
				h[i] = 0
			}
		}
		stuck := false
	sweep:
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				l, r, t, b := y*9+x, y*9+x+1, y*8+x, (y+1)*8+x
				for {
					code := factorioCliffCellCode(v[l], v[r], h[t], h[b])
					if code == 0 || factorioCliffCodePlaces(code) {
						break
					}
					switch {
					case v[l] != 0 && x != 0:
						v[l] = 0
					case h[t] != 0 && y != 0:
						h[t] = 0
					case v[r] != 0 && x != 7:
						v[r] = 0
					case h[b] != 0 && y != 7:
						h[b] = 0
					default:
						stuck = true
						break sweep
					}
				}
			}
		}
		if !stuck {
			return
		}
	}
}

func factorioCliffSmoothingKnots(index int) (int, int, float64) {
	i := (index%8 + 8) % 8
	base, lo := index-i, i & ^3
	hi := min(lo+4, 7)
	return base + lo, base + hi, float64(i&3) / float64(hi-lo)
}

// Query whole chunks even for small viewports: repair must not change when
// the same map is rendered as adjacent images. Corners are on the bare 4-tile
// lattice; the half-tile Y offset belongs only to the placed entity centre.
func factorioPlaceCliffs(ctx context.Context, x0, y0, x1, y1, elevation0, interval, smoothing float64,
	field func(float64, float64) factorioCliffCorner, reject func(factorioCliffCell) bool,
) ([]factorioCliffCell, error) {
	if interval <= 0 || x0 >= x1 || y0 >= y1 {
		return nil, nil
	}
	ix0, iy0 := int(math.Ceil((x0-2)/4)), int(math.Ceil((y0-2.5)/4))
	ix1, iy1 := int(math.Ceil((x1-2)/4))-1, int(math.Ceil((y1-2.5)/4))-1
	if ix0 > ix1 || iy0 > iy1 {
		return nil, nil
	}
	minX, minY := int(math.Floor(float64(ix0)/8)), int(math.Floor(float64(iy0)/8))
	maxX, maxY := int(math.Floor(float64(ix1)/8)), int(math.Floor(float64(iy1)/8))
	width := maxX - minX + 1
	count := width * (maxY - minY + 1)
	const batchSize = 64
	edges := make([]factorioCliffChunkEdges, min(batchSize, count))
	var cells []factorioCliffCell
	for first := 0; first < count; first += batchSize {
		batch := edges[:min(batchSize, count-first)]
		err := parallelFastPreviewChunks(ctx, len(batch), func(i int) {
			position := first + i
			cx, cy := minX+position%width, minY+position/width
			batch[i] = factorioCliffEdgesAt(cx, cy, elevation0, interval, smoothing, field)
		})
		if err != nil {
			return nil, err
		}
		// Placement and collision remain in the original row order.
		for i := range batch {
			position := first + i
			cx, cy := minX+position%width, minY+position/width
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					c := batch[i].cell(cx, cy, x, y)
					if c.x >= x0 && c.x < x1 && c.y >= y0 && c.y < y1 &&
						factorioCliffCodePlaces(factorioCliffCellCode(c.left, c.right, c.top, c.bottom)) &&
						(reject == nil || !reject(c)) {
						cells = append(cells, c)
					}
				}
			}
		}
	}
	return cells, ctx.Err()
}

type factorioCliffChunkEdges struct {
	vertical, horizontal [72]int
}

func (e *factorioCliffChunkEdges) cell(cx, cy, x, y int) factorioCliffCell {
	return factorioCliffCell{
		factorioPoint: factorioPoint{x: float64(cx*32+x*4) + 2, y: float64(cy*32+y*4) + 2.5},
		left:          e.vertical[y*9+x], right: e.vertical[y*9+x+1],
		top: e.horizontal[y*8+x], bottom: e.horizontal[(y+1)*8+x],
	}
}

func factorioCliffEdgesAt(cx, cy int, elevation0, interval, smoothing float64, field func(float64, float64) factorioCliffCorner) factorioCliffChunkEdges {
	baseX, baseY := cx*8, cy*8
	// Smoothing may use the next chunk's first two knots on its shared
	// edge. The furthest knot is four lattice steps past that edge.
	const side = 13
	var cached [side * side]factorioCliffCorner
	var seen [side * side]bool
	raw := func(x, y int) factorioCliffCorner {
		i := (y-baseY)*side + x - baseX
		if !seen[i] {
			cached[i] = field(float64(x)*4, float64(y)*4)
			seen[i] = true
		}
		return cached[i]
	}
	var corners [81]factorioCliffCorner
	for y := 0; y < 9; y++ {
		for x := 0; x < 9; x++ {
			ix, iy := baseX+x, baseY+y
			if smoothing == 0 {
				corners[y*9+x] = field(float64(ix)*4, float64(iy)*4)
				continue
			}
			v := raw(ix, iy)
			lx, hx, tx := factorioCliffSmoothingKnots(ix)
			ly, hy, ty := factorioCliffSmoothingKnots(iy)
			a := lerpFloat(raw(lx, ly).elevation, raw(hx, ly).elevation, tx)
			b := lerpFloat(raw(lx, hy).elevation, raw(hx, hy).elevation, tx)
			v.elevation = lerpFloat(v.elevation, lerpFloat(a, b, ty), smoothing)
			corners[y*9+x] = v
		}
	}
	var edges factorioCliffChunkEdges
	cross := func(a, b factorioCliffCorner) int {
		return factorioCrossesCliff(a.elevation, b.elevation, (a.cliffiness+b.cliffiness)/2, elevation0, interval)
	}
	for y := 0; y < 8; y++ {
		for x := 0; x < 9; x++ {
			edges.vertical[y*9+x] = cross(corners[y*9+x], corners[(y+1)*9+x])
		}
	}
	for y := 0; y < 9; y++ {
		for x := 0; x < 8; x++ {
			edges.horizontal[y*8+x] = cross(corners[y*9+x], corners[y*9+x+1])
		}
	}
	factorioRepairCliffChunk(&edges.vertical, &edges.horizontal)
	return edges
}
