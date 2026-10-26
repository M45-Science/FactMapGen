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
	cache := make(map[[2]int]factorioCliffCorner)
	raw := func(x, y int) factorioCliffCorner {
		if len(cache) > 4096 {
			clear(cache)
		}
		key := [2]int{x, y}
		if v, ok := cache[key]; ok {
			return v
		}
		v := field(float64(x)*4, float64(y)*4)
		cache[key] = v
		return v
	}
	corner := func(x, y int) factorioCliffCorner {
		v := raw(x, y)
		if smoothing != 0 {
			lx, hx, tx := factorioCliffSmoothingKnots(x)
			ly, hy, ty := factorioCliffSmoothingKnots(y)
			a := lerpFloat(raw(lx, ly).elevation, raw(hx, ly).elevation, tx)
			b := lerpFloat(raw(lx, hy).elevation, raw(hx, hy).elevation, tx)
			v.elevation = lerpFloat(v.elevation, lerpFloat(a, b, ty), smoothing)
		}
		return v
	}
	cross := func(a, b factorioCliffCorner) int {
		return factorioCrossesCliff(a.elevation, b.elevation, (a.cliffiness+b.cliffiness)/2, elevation0, interval)
	}
	var cells []factorioCliffCell
	for cy := int(math.Floor(float64(iy0) / 8)); cy <= int(math.Floor(float64(iy1)/8)); cy++ {
		for cx := int(math.Floor(float64(ix0) / 8)); cx <= int(math.Floor(float64(ix1)/8)); cx++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			var corners [81]factorioCliffCorner
			for y := 0; y < 9; y++ {
				for x := 0; x < 9; x++ {
					corners[y*9+x] = corner(cx*8+x, cy*8+y)
				}
			}
			var v, h [72]int
			for y := 0; y < 8; y++ {
				for x := 0; x < 9; x++ {
					v[y*9+x] = cross(corners[y*9+x], corners[(y+1)*9+x])
				}
			}
			for y := 0; y < 9; y++ {
				for x := 0; x < 8; x++ {
					h[y*8+x] = cross(corners[y*9+x], corners[y*9+x+1])
				}
			}
			factorioRepairCliffChunk(&v, &h)
			cell := func(x, y int) factorioCliffCell {
				return factorioCliffCell{factorioPoint: factorioPoint{x: float64(cx*32+x*4) + 2, y: float64(cy*32+y*4) + 2.5}, left: v[y*9+x], right: v[y*9+x+1], top: h[y*8+x], bottom: h[(y+1)*8+x]}
			}
			// Collect before mutating: rejection changes neighbouring orientations,
			// but does not recursively reject the changed neighbours.
			if reject != nil {
				var rejected [64]bool
				for y := 0; y < 8; y++ {
					for x := 0; x < 8; x++ {
						c := cell(x, y)
						rejected[y*8+x] = factorioCliffCodePlaces(factorioCliffCellCode(c.left, c.right, c.top, c.bottom)) && reject(c)
					}
				}
				for y := 0; y < 8; y++ {
					for x := 0; x < 8; x++ {
						if rejected[y*8+x] {
							v[y*9+x] = 0
							v[y*9+x+1] = 0
							h[y*8+x] = 0
							h[(y+1)*8+x] = 0
						}
					}
				}
			}
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					c := cell(x, y)
					if c.x >= x0 && c.x < x1 && c.y >= y0 && c.y < y1 && factorioCliffCodePlaces(factorioCliffCellCode(c.left, c.right, c.top, c.bottom)) {
						cells = append(cells, c)
					}
				}
			}
		}
	}
	return cells, nil
}
