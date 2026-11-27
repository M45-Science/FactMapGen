// SPDX-License-Identifier: AGPL-3.0-or-later
// Adapted from FactorioMapWebUI; see docs/upstream-rendering.md for provenance.

package main

import (
	"math"
	"math/bits"
	"sync/atomic"
)

// Nearby samples share blocks of deterministic lattice points. A two-cell
// border keeps every sample's search in one immutable block.
const (
	voronoiBlockSide = 16
	voronoiBlockPad  = 2
	voronoiStride    = voronoiBlockSide + 2*voronoiBlockPad
)

type factorioVoronoi struct {
	seed         uint32
	grid, jitter float32
	metric       int
	ring         int
	blocks       *[256]atomic.Pointer[factorioVoronoiBlock]
}

type factorioVoronoiBlock struct {
	x, y   int32
	points [voronoiStride * voronoiStride]noisePoint
	ids    [voronoiStride * voronoiStride]float32
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
	return factorioVoronoi{
		seed: seed + salt, grid: max(1, float32(math.Trunc(float64(grid)))),
		jitter: jitter, metric: metric, ring: ring,
		blocks: new([256]atomic.Pointer[factorioVoronoiBlock]),
	}
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

func (v factorioVoronoi) block(bx, by int32) *factorioVoronoiBlock {
	slot := &v.blocks[(uint32(bx)+16*uint32(by))%uint32(len(v.blocks))]
	if cached := slot.Load(); cached != nil && cached.x == bx && cached.y == by {
		return cached
	}
	block := &factorioVoronoiBlock{x: bx, y: by}
	startX, startY := bx*voronoiBlockSide-voronoiBlockPad, by*voronoiBlockSide-voronoiBlockPad
	for y := range voronoiStride {
		for x := range voronoiStride {
			index := y*voronoiStride + x
			block.points[index], block.ids[index] = v.point(startX+int32(x), startY+int32(y))
		}
	}
	slot.Store(block)
	return block
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
	block := v.block(cx>>4, cy>>4)
	blockX := int(cx&(voronoiBlockSide-1)) + voronoiBlockPad
	blockY := int(cy&(voronoiBlockSide-1)) + voronoiBlockPad
	if v.metric == voronoiEuclidean && !pyramid {
		return sampleFactorioVoronoiEuclidean(block, frac, blockX, blockY, v.ring)
	}
	var points [25]noisePoint
	count, winner := 0, 0
	winnerIndex := (blockY-v.ring)*voronoiStride + blockX - v.ring
	first, second := float32(math.MaxFloat32), float32(math.MaxFloat32)
	for y := -v.ring; y <= v.ring; y++ {
		row := (blockY+y)*voronoiStride + blockX
		for x := -v.ring; x <= v.ring; x++ {
			index := row + x
			offset := block.points[index]
			p := noisePoint{float32(offset[0]+float32(x)) - frac[0], float32(offset[1]+float32(y)) - frac[1]}
			if pyramid {
				points[count] = p
			}
			d := v.distance(p)
			if d < first {
				second, first, winner = first, d, count
				winnerIndex = index
			} else if d < second {
				second = d
			}
			count++
		}
	}
	result := factorioVoronoiSample{id: block.ids[winnerIndex], spot: first, facet: second - first}
	if !pyramid {
		return result
	}
	result.pyramid = math.MaxFloat32
	a := points[winner]
	if v.metric == voronoiEuclidean {
		for i, b := range points[:count] {
			if i == winner {
				continue
			}
			n := noisePoint{b[0] - a[0], b[1] - a[1]}
			length := noiseSqrt(float32(n[0]*n[0]) + float32(n[1]*n[1]))
			if length == 0 {
				continue
			}
			d := float32(float32(float32(a[0]+b[0])*0.5)*float32(n[0]/length)) + float32(float32(float32(a[1]+b[1])*0.5)*float32(n[1]/length))
			result.pyramid = min(result.pyramid, d)
		}
		return result
	}
	pa, s := noisePoint{frac[0] - a[0], frac[1] - a[1]}, frac
	rotate := v.metric == voronoiChebyshev
	if rotate {
		pa, s = rotateFactorioVoronoiPoint(pa), rotateFactorioVoronoiPoint(s)
	}
	squared := float32(math.MaxFloat32)
	for i, b := range points[:count] {
		if i == winner {
			continue
		}
		pb := noisePoint{frac[0] - b[0], frac[1] - b[1]}
		if rotate {
			pb = rotateFactorioVoronoiPoint(pb)
		}
		squared = min(squared, noiseL1BisectorSquared(pa, pb, s))
	}
	result.pyramid = noiseSqrt(squared)
	return result
}

func sampleFactorioVoronoiEuclidean(block *factorioVoronoiBlock, frac noisePoint, blockX, blockY, ring int) factorioVoronoiSample {
	first, second := float32(math.MaxFloat32), float32(math.MaxFloat32)
	spot, winner := first, (blockY-ring)*voronoiStride+blockX-ring
	for y := -ring; y <= ring; y++ {
		row := (blockY+y)*voronoiStride + blockX
		for x := -ring; x <= ring; x++ {
			index := row + x
			p := block.points[index]
			dx, dy := float32(p[0]+float32(x))-frac[0], float32(p[1]+float32(y))-frac[1]
			squared := float32(dx*dx) + float32(dy*dy)
			if squared < first {
				second, first = first, squared
				// The game's comparisons use rounded distances. A strictly
				// smaller square can still tie the earlier point's distance.
				if d := noiseSqrt(squared); d < spot {
					spot, winner = d, index
				}
			} else if squared < second {
				second = squared
			}
		}
	}
	if first == math.MaxFloat32 {
		return factorioVoronoiSample{id: block.ids[winner], spot: spot}
	}
	return factorioVoronoiSample{id: block.ids[winner], spot: spot, facet: noiseSqrt(second) - spot}
}

func rotateFactorioVoronoiPoint(p noisePoint) noisePoint {
	x, y := p[0]*0.75, p[1]*0.75
	return noisePoint{x + y, y - x}
}

// An L1 bisector consists of a diagonal segment and two axis-aligned rays.
func noiseL1BisectorSquared(a, b, s noisePoint) float32 {
	if noiseAbs(b[1]-a[1]) > noiseAbs(b[0]-a[0]) {
		a[0], a[1] = a[1], a[0]
		b[0], b[1] = b[1], b[0]
		s[0], s[1] = s[1], s[0]
	}
	mid := noisePoint{float32(a[0]+b[0]) * 0.5, float32(a[1]+b[1]) * 0.5}
	direction := float32(1)
	if b[0] <= a[0] {
		direction = -1
	}
	p, q := a, b
	p[0] = mid[0] + float32(direction*noiseAbs(a[1]-mid[1]))
	q[0] = mid[0] - float32(direction*noiseAbs(b[1]-mid[1]))
	dx, dy := q[0]-p[0], q[1]-p[1]
	denom := float32(dx*dx) + float32(dy*dy)
	t := float32(0)
	if denom != 0 {
		t = min(1, max(0, (float32(dx*float32(s[0]-p[0]))+float32(dy*float32(s[1]-p[1])))/denom))
	}
	foot := noisePoint{p[0] + float32(dx*t), p[1] + float32(dy*t)}
	sign := float32(1)
	if a[1] < b[1] {
		sign = -1
	}
	p[1] += float32(sign * max(0, float32(sign*float32(s[1]-p[1]))))
	q[1] -= float32(sign * max(0, -float32(sign*float32(s[1]-q[1]))))
	squared := func(p noisePoint) float32 { x, y := p[0]-s[0], p[1]-s[1]; return float32(x*x) + float32(y*y) }
	return min(squared(foot), squared(p), squared(q))
}
