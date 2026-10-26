// SPDX-License-Identifier: AGPL-3.0-or-later
// Adapted from FactorioMapWebUI; see docs/upstream-rendering.md for provenance.

package main

import "math"

// Single-precision kernels for Space Age expressions. The older Nauvis path
// keeps its existing arithmetic; these kernels round at the game's operation
// boundaries, including the small 17.17 multioctave offset. See
// docs/upstream-rendering.md for the measured upstream references.
func noiseAbs(v float32) float32  { return float32(math.Abs(float64(v))) }
func noiseSqrt(v float32) float32 { return float32(math.Sqrt(float64(v))) }
func noiseBool(v bool) float32 {
	if v {
		return 1
	}
	return 0
}
func noiseLerp(a, b, t float32) float32 { return a + float32(float32(b-a)*t) }
func noiseLog2(v float32) float32 {
	bits := math.Float32bits(v)
	m := math.Float32frombits(bits&0x007fffff | 0x3f000000)
	a := float32(float64(bits)/8388608) + math.Float32frombits(0xc2f87377)
	a += float32(m * math.Float32frombits(0xbfbfbf75))
	return a + float32(math.Float32frombits(0xbfdce9a3)/float32(m+math.Float32frombits(0x3eb444f9)))
}
func noiseExp2(v float32) float32 {
	v = max(v, -126)
	z := v - float32(math.Trunc(float64(v)))
	if v < 0 {
		z++
	}
	a := v + math.Float32frombits(0x42f28c51)
	a += float32(math.Float32frombits(0x41ddd2fe) / float32(math.Float32frombits(0x409af5f8)-z))
	a += float32(z * math.Float32frombits(0xbfbebc8d))
	return math.Float32frombits(uint32(int64(float64(a) * 8388608)))
}
func noiseSliderLinear(s, low, high float32) float32 {
	if s <= 0 {
		return low
	}
	r := float32(math.Log2(float64(s))) / float32(math.Log2(6))
	if r < 0 {
		return -r * low
	}
	return r * high
}
func noiseSliderRescale(s, extent float32) float32 {
	if s <= 0 {
		return 0
	}
	r := float32(math.Log2(float64(s))) / float32(math.Log2(6))
	return float32(math.Exp2(float64(float32(r * float32(math.Log2(float64(extent)))))))
}

func factorioBasisNoise32(x, y float32, tables *factorioBasisTables, grid ...bool) float32 {
	ix, iy := int64(math.Floor(float64(x))), int64(math.Floor(float64(y)))
	fx, fy := x-float32(ix), y-float32(iy)
	var corners [4]float32
	for i := range corners {
		cx, cy := int64(i&1), int64(i>>1)
		dx, dy := fx-float32(cx), fy-float32(cy)
		d := float32(dx*dx) + float32(dy*dy)
		if d >= 1 {
			continue
		}
		t := 1 - d
		if len(grid) > 0 && grid[0] {
			t = max(0, float32(1-float32(dy*dy))-float32(dx*dx))
		}
		falloff := t * float32(t*t)
		h := tables.a[uint8(ix+cx)] ^ tables.b[uint8(iy+cy)]
		dot := float32(dx*factorioGradient32X[tables.sigma[h]]) + float32(dy*factorioGradient32Y[tables.sigma[h]])
		corners[i] = falloff * dot
	}
	if len(grid) > 0 && grid[0] {
		return (corners[0] + corners[2]) + (corners[1] + corners[3])
	}
	return (corners[0] + corners[1]) + (corners[2] + corners[3])
}

func makeFactorioMultioctaveNoise32(seed, salt uint32, octaves int, persistence, scale, output float32, grid ...bool) func(float32, float32) float32 {
	baseScale := scale
	tables := factorioBasisTablesFromSeed(seed, salt)
	inv := float32(1) / persistence
	amp := output
	if inv == 1 {
		amp = float32(float64(output) / math.Sqrt(float64(octaves)))
	} else if inv != 0 {
		inv2 := inv * inv
		ratio := (inv2 - 1) / (noiseExp2(float32(noiseLog2(inv2)*float32(octaves))) - 1)
		amp = float32(math.Sqrt(float64(ratio)) * float64(output))
	}
	scales, amps := make([]float32, octaves), make([]float32, octaves)
	for i := range scales {
		scales[i], amps[i] = scale, amp
		scale *= 0.5
		amp *= inv
	}
	return func(x, y float32) float32 {
		var sum float32
		for i, scale := range scales {
			nx := float32(float64(float32(x*scale)) + float64(i)*17.17)
			ny := float32(y * scale)
			if len(grid) > 0 && grid[0] {
				step := float32(math.Ldexp(1, -i))
				cx, cy := float32(math.Floor(float64(x)/32)*32), float32(math.Floor(float64(y)/32)*32)
				origin := float32(float64(i)*17.17/float64(baseScale) + float64(float32(cx*step)))
				nx = float32(float32(float32(x-cx)*step)+origin) * baseScale
				ny = float32(float32(float32(y-cy)*step)+float32(cy*step)) * baseScale
			}
			sum += float32(amps[i] * factorioBasisNoise32(nx, ny, &tables, grid...))
		}
		return sum
	}
}

type factorioNoiseCone struct{ x, y, radius float32 }

func newFactorioNoiseCone(angle, distance, radius float32) factorioNoiseCone {
	radians := float32(angle/180) * float32(math.Pi)
	return factorioNoiseCone{distance * float32(math.Sin(float64(radians))), -distance * float32(math.Cos(float64(radians))), radius}
}
func (c factorioNoiseCone) at(x, y, dx, dy float32) float32 {
	x = float32(c.x-x) + dx
	y = float32(c.y-y) + dy
	return 1 - noiseSqrt(float32(x*x)+float32(y*y))/c.radius
}

func makeFactorioQuickNoise32(seed, salt uint32, octaves int, scale, amplitude, scaleMultiplier, amplitudeMultiplier, offset float32, grid bool) func(float32, float32) float32 {
	tables := make([]factorioBasisTables, octaves)
	scales, amps := make([]float32, octaves), make([]float32, octaves)
	for i := range tables {
		tables[i] = factorioBasisTablesFromSeed(seed+uint32(i), salt)
		scales[i], amps[i] = scale, amplitude
		scale *= scaleMultiplier
		amplitude *= amplitudeMultiplier
	}
	return func(x, y float32) float32 {
		var sum float32
		for i, scale := range scales {
			sum += float32(amps[i] * factorioBasisNoise32(float32(x+offset)*scale, y*scale, &tables[i], grid))
		}
		return sum
	}
}
func makeFactorioAmplitudeNoise32(seed, salt uint32, octaves int, scale, offset, persistence float32, grid bool) func(float32, float32) float32 {
	tables := factorioBasisTablesFromSeed(seed, salt)
	twoN := float32(math.Ldexp(1, octaves))
	series := 1 - float32(math.Pow(float64(persistence), float64(octaves)))
	gain := float32(float32(float32(1-persistence)/twoN)/series) * twoN
	scales := make([]float32, octaves)
	scale *= 0.5
	for i := range scales {
		scales[i] = scale
		scale *= 0.5
	}
	return func(x, y float32) float32 {
		var sum float32
		for i, scale := range scales {
			sum += factorioBasisNoise32(float32(x+offset)*scale, y*scale, &tables, grid)
			if i < len(scales)-1 {
				sum *= persistence
			}
		}
		return sum * gain
	}
}
