// SPDX-License-Identifier: AGPL-3.0-or-later
// Adapted from FactorioMapWebUI; see docs/upstream-rendering.md for provenance.

package main

import (
	"math"
	"math/bits"
)

// Immutable Voronoi fields are safe to share across tile workers. Each sample
// keeps its small search window on the stack, avoiding an unbounded point cache.
type factorioVoronoi struct {
	seed         uint32
	grid, jitter float32
	metric       int
	ring         int
}

const (
	voronoiChebyshev = iota
	voronoiManhattan
	voronoiEuclidean
	voronoiMinkowski
)

type noisePoint [2]float32
type factorioVoronoiSample struct{ id, spot, facet, pyramid float32 }

func newFactorioVoronoi(seed, salt uint32, grid, jitter float32, metric int) factorioVoronoi {
	ring := 1
	if metric == voronoiManhattan && jitter > 0.5 || metric == voronoiEuclidean && jitter > float32(0.66) || metric == voronoiMinkowski && jitter > 0.75 {
		ring = 2
	}
	return factorioVoronoi{seed + salt, max(1, float32(math.Trunc(float64(grid)))), jitter, metric, ring}
}
func factorioWangHash(a uint32) uint32 {
	a += 0x7ed55d16 + (a << 12)
	a ^= 0xc761c23c ^ (a >> 19)
	a += 0x165667b1 + (a << 5)
	a = (a + 0xd3a2646c) ^ (a << 9)
	a += 0xfd7046c5 + (a << 3)
	return a ^ 0xb55a4f09 ^ (a >> 16)
}
func (v factorioVoronoi) point(cx, cy int32) (noisePoint, float32) {
	w := v.seed ^ factorioWangHash(uint32(cx)) ^ factorioWangHash(bits.RotateLeft32(uint32(cy), 16))
	draw := func(i uint32) float32 { return float32(float64(factorioWangHash(w+i)) / 4294967296) }
	base := float32(1-v.jitter) * 0.5
	return noisePoint{float32(v.jitter*draw(0)) + base, float32(v.jitter*draw(1)) + base}, draw(2)
}
func (v factorioVoronoi) distance(p noisePoint) float32 {
	x, y := noiseAbs(p[0]), noiseAbs(p[1])
	switch v.metric {
	case voronoiChebyshev:
		return max(x, y)
	case voronoiManhattan:
		return x + y
	case voronoiMinkowski:
		s := float32(float32(x*x)*x) + float32(float32(y*y)*y)
		if s == 0 {
			return 0
		}
		return noiseExp2(float32(noiseLog2(s) * float32(1.0/3)))
	default:
		return noiseSqrt(float32(x*x) + float32(y*y))
	}
}
func (v factorioVoronoi) sample(x, y float32, pyramid bool) factorioVoronoiSample {
	ux, uy := x/v.grid, y/v.grid
	cx, cy := int32(math.Floor(float64(ux))), int32(math.Floor(float64(uy)))
	frac := noisePoint{ux - float32(cx), uy - float32(cy)}
	var points [25]noisePoint
	var ids [25]float32
	count, winner := 0, 0
	first, second := float32(math.MaxFloat32), float32(math.MaxFloat32)
	for j := -v.ring; j <= v.ring; j++ {
		for i := -v.ring; i <= v.ring; i++ {
			p, id := v.point(cx+int32(i), cy+int32(j))
			p = noisePoint{float32(p[0]+float32(i)) - frac[0], float32(p[1]+float32(j)) - frac[1]}
			points[count], ids[count] = p, id
			d := v.distance(p)
			if d < first {
				second, first, winner = first, d, count
			} else if d < second {
				second = d
			}
			count++
		}
	}
	result := factorioVoronoiSample{id: ids[winner], spot: first, facet: second - first}
	if !pyramid {
		return result
	}
	result.pyramid = math.MaxFloat32
	a := points[winner]
	for i := 0; i < count; i++ {
		if i == winner {
			continue
		}
		b := points[i]
		var d float32
		if v.metric == voronoiEuclidean {
			n := noisePoint{b[0] - a[0], b[1] - a[1]}
			length := noiseSqrt(float32(n[0]*n[0]) + float32(n[1]*n[1]))
			if length == 0 {
				continue
			}
			d = float32(float32(float32(a[0]+b[0])*0.5)*float32(n[0]/length)) + float32(float32(float32(a[1]+b[1])*0.5)*float32(n[1]/length))
		} else {
			pa, pb, s := noisePoint{frac[0] - a[0], frac[1] - a[1]}, noisePoint{frac[0] - b[0], frac[1] - b[1]}, frac
			if v.metric == voronoiChebyshev {
				rotate := func(p noisePoint) noisePoint { x, y := p[0]*0.75, p[1]*0.75; return noisePoint{x + y, y - x} }
				pa, pb, s = rotate(pa), rotate(pb), rotate(s)
			}
			d = noiseL1Bisector(pa, pb, s)
		}
		result.pyramid = min(result.pyramid, d)
	}
	return result
}

// An L1 bisector consists of a diagonal segment and two axis-aligned rays.
func noiseL1Bisector(a, b, s noisePoint) float32 {
	major := 0
	if noiseAbs(b[1]-a[1]) > noiseAbs(b[0]-a[0]) {
		major = 1
	}
	minor := 1 - major
	mid := noisePoint{float32(a[0]+b[0]) * 0.5, float32(a[1]+b[1]) * 0.5}
	direction := float32(1)
	if b[major] <= a[major] {
		direction = -1
	}
	p, q := a, b
	p[major] = mid[major] + float32(direction*noiseAbs(a[minor]-mid[minor]))
	q[major] = mid[major] - float32(direction*noiseAbs(b[minor]-mid[minor]))
	dx, dy := q[0]-p[0], q[1]-p[1]
	denom := float32(dx*dx) + float32(dy*dy)
	t := float32(0)
	if denom != 0 {
		t = min(1, max(0, (float32(dx*float32(s[0]-p[0]))+float32(dy*float32(s[1]-p[1])))/denom))
	}
	foot := noisePoint{p[0] + float32(dx*t), p[1] + float32(dy*t)}
	sign := float32(1)
	if a[minor] < b[minor] {
		sign = -1
	}
	p[minor] += float32(sign * max(0, float32(sign*float32(s[minor]-p[minor]))))
	q[minor] -= float32(sign * max(0, -float32(sign*float32(s[minor]-q[minor]))))
	squared := func(p noisePoint) float32 { x, y := p[0]-s[0], p[1]-s[1]; return float32(x*x) + float32(y*y) }
	return noiseSqrt(min(squared(foot), squared(p), squared(q)))
}
