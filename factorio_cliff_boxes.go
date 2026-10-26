// SPDX-License-Identifier: AGPL-3.0-or-later
// Adapted from FactorioMapWebUI cliffs/catalog.rs; see docs/upstream-rendering.md.
package main

import "math"

// Preview collision boxes are axis aligned and truncated to 1/256 tiles.
var factorioCliffCollisionBoxes = map[int][4]float64{
	240: {-2.0, -1.5, 2.0, 1.5},
	5:   {-1.0, -2.0, 1.0, 2.0},
	80:  {-2.0, -0.5, 2.0, 0.5},
	15:  {-1.0, -2.0, 1.0, 2.0},
	204: {-2.30859375, -2.87109375, -0.1875, 1.37109375},
	52:  {-0.87109375, -1.80859375, 3.37109375, 0.30859375},
	17:  {0.0390625, -0.515625, 1.45703125, 3.015625},
	67:  {-2.515625, 0.5390625, 1.015625, 1.95703125},
	193: {-3.37109375, -0.30859375, 0.87109375, 1.80859375},
	68:  {-1.45703125, -3.015625, -0.0390625, 0.515625},
	28:  {-1.015625, -1.95703125, 2.515625, -0.5390625},
	51:  {0.1875, -1.37109375, 2.30859375, 2.87109375},
	192: {-2.20703125, -1.4140625, -0.7890625, 1.4140625},
	48:  {0.08203125, -0.70703125, 2.9140625, 0.70703125},
	16:  {0.89453125, -0.6640625, 1.6015625, 2.1640625},
	64:  {-2.6640625, 0.3984375, 0.17578125, 1.10546875},
	4:   {-0.9140625, -1.70703125, 1.9140625, -0.2890625},
	1:   {0.14453125, -0.765625, 0.8515625, 2.765625},
	3:   {-2.265625, 0.64453125, 1.265625, 1.3515625},
	12:  {-1.20703125, -2.4140625, 0.20703125, 0.4140625},
}

// MapPreviewGenerator draws the AABB of the rotated collision box. This is
// different from the unrotated tile-collision check in tryToAddCliff. Reproduce
// the binary's 1/256 positions, signed 16-bit rotation and exclusive ceil edge.
func factorioCliffChartRect(c factorioCliffCell) (float64, float64, float64, float64) {
	code := factorioCliffCellCode(c.left, c.right, c.top, c.bottom)
	b := factorioCliffCollisionBoxes[code]
	l, t, r, bottom := int((c.x+b[0])*256), int((c.y+b[1])*256), int((c.x+b[2])*256), int((c.y+b[3])*256)
	if code != 240 && code != 80 && code != 5 && code != 15 {
		cx, cy := (l+r)/2, (t+bottom)/2
		rotation := float64(float32(23170) * float32(1.0/32767))
		rotate := func(x, y int) (int, int) {
			dx, dy := float64(x-cx), float64(y-cy)
			return cx + int(dx*rotation-dy*rotation), cy + int(dx*rotation+dy*rotation)
		}
		x1, y1 := rotate(l, t)
		x2, y2 := rotate(r, t)
		x3, y3, x4, y4 := 2*cx-x2, 2*cy-y2, 2*cx-x1, 2*cy-y1
		l, t, r, bottom = min(x1, x2, x3, x4), min(y1, y2, y3, y4), max(x1, x2, x3, x4), max(y1, y2, y3, y4)
	}
	// Like ordinary entities, each cliff is drawn into its own preview chunk.
	// A rotated box may extend past that chunk, but its ink cannot spill into
	// the neighbouring chunk's independently generated chart.
	cx, cy := math.Floor(c.x/32)*32, math.Floor(c.y/32)*32
	return max(cx, math.Floor(float64(l)/256)), max(cy, math.Floor(float64(t)/256)),
		min(cx+32, math.Ceil(float64(r)/256)), min(cy+32, math.Ceil(float64(bottom)/256))
}
