package main

import "math"

const (
	factorioNoiseTableSize = 256
	factorioMinSeedWord    = 0x155
)

type factorioTaus88State struct {
	s1 uint32
	s2 uint32
	s3 uint32
}

func newFactorioTaus88State(word uint32) factorioTaus88State {
	return factorioTaus88State{s1: word, s2: word, s3: word}
}

func (s *factorioTaus88State) next() uint32 {
	s.s1 = ((s.s1 & 0xfffffffe) << 12) ^ (((s.s1 << 13) ^ s.s1) >> 19)
	s.s2 = ((s.s2 & 0xfffffff8) << 4) ^ (((s.s2 << 2) ^ s.s2) >> 25)
	s.s3 = ((s.s3 & 0xfffffff0) << 17) ^ (((s.s3 << 3) ^ s.s3) >> 11)
	return s.s1 ^ s.s2 ^ s.s3
}

type factorioBasisTables struct {
	sigma     [factorioNoiseTableSize]uint8
	gradientX [factorioNoiseTableSize]float64
	gradientY [factorioNoiseTableSize]float64
	a         [factorioNoiseTableSize]uint8
	b         [factorioNoiseTableSize]uint8
}

var (
	factorioGradientX = factorioGradientTable(false)
	factorioGradientY = factorioGradientTable(true)
)

func factorioGradientTable(sine bool) [factorioNoiseTableSize]float64 {
	var values [factorioNoiseTableSize]float64
	for i := range values {
		angle := 2 * math.Pi * float64(i) / factorioNoiseTableSize
		if sine {
			values[i] = math.Sin(angle)
		} else {
			values[i] = math.Cos(angle)
		}
	}
	return values
}

func factorioBasisTablesFromSeed(seed0, seed1 uint32) factorioBasisTables {
	word := seed0 + 7*(seed1>>8)
	if word < factorioMinSeedWord {
		word = factorioMinSeedWord
	}
	state := newFactorioTaus88State(word)
	scratch := factorioShuffleIdentity(&state)
	salt := scratch[seed1&(factorioNoiseTableSize-1)]
	yTable := factorioShuffleIdentity(&state)
	xTable := factorioShuffleIdentity(&state)
	gradient := factorioShuffleIdentity(&state)

	var tables factorioBasisTables
	tables.a = xTable
	tables.b = yTable
	for i := range tables.sigma {
		gradientIndex := gradient[uint8(i)^salt]
		tables.sigma[i] = gradientIndex
		tables.gradientX[i] = factorioGradientX[gradientIndex]
		tables.gradientY[i] = factorioGradientY[gradientIndex]
	}
	return tables
}

func factorioShuffleIdentity(state *factorioTaus88State) [factorioNoiseTableSize]uint8 {
	var values [factorioNoiseTableSize]uint8
	for i := range values {
		values[i] = uint8(i)
	}
	for pos := factorioNoiseTableSize - 1; pos >= 1; pos-- {
		j := int(state.next() % uint32(pos+1))
		values[pos], values[j] = values[j], values[pos]
	}
	return values
}

func factorioBasisNoise(x, y float64, tables *factorioBasisTables) float64 {
	ix := int64(math.Floor(x))
	iy := int64(math.Floor(y))
	fx := x - float64(ix)
	fy := y - float64(iy)
	// Preserve the old loop's 00, 10, 01, 11 arithmetic and accumulation order.
	dx0 := fx - float64(0)
	dx1 := fx - float64(1)
	dy0 := fy - float64(0)
	dy1 := fy - float64(1)
	ax0 := tables.a[uint8(ix)]
	ax1 := tables.a[uint8(ix+1)]
	by0 := tables.b[uint8(iy)]
	by1 := tables.b[uint8(iy+1)]
	value := 0.0

	distanceSquared := dx0*dx0 + dy0*dy0
	if !(distanceSquared >= 1) {
		gradient := ax0 ^ by0
		falloff := 1 - distanceSquared
		falloff *= falloff * falloff
		value += falloff * 4.2 * (dx0*tables.gradientX[gradient] + dy0*tables.gradientY[gradient])
	}

	distanceSquared = dx1*dx1 + dy0*dy0
	if !(distanceSquared >= 1) {
		gradient := ax1 ^ by0
		falloff := 1 - distanceSquared
		falloff *= falloff * falloff
		value += falloff * 4.2 * (dx1*tables.gradientX[gradient] + dy0*tables.gradientY[gradient])
	}

	distanceSquared = dx0*dx0 + dy1*dy1
	if !(distanceSquared >= 1) {
		gradient := ax0 ^ by1
		falloff := 1 - distanceSquared
		falloff *= falloff * falloff
		value += falloff * 4.2 * (dx0*tables.gradientX[gradient] + dy1*tables.gradientY[gradient])
	}

	distanceSquared = dx1*dx1 + dy1*dy1
	if !(distanceSquared >= 1) {
		gradient := ax1 ^ by1
		falloff := 1 - distanceSquared
		falloff *= falloff * falloff
		value += falloff * 4.2 * (dx1*tables.gradientX[gradient] + dy1*tables.gradientY[gradient])
	}
	return value
}

type factorioMultioctaveParams struct {
	seed0       uint32
	seed1       uint32
	octaves     int
	persistence float64
	inputScale  float64
	outputScale float64
}

func makeFactorioMultioctaveNoise(params factorioMultioctaveParams) func(float64, float64) float64 {
	tables := factorioBasisTablesFromSeed(params.seed0, params.seed1)
	normalization := factorioMultioctaveNormalization(params.persistence, params.octaves)
	scales := make([]float64, params.octaves)
	amplitudes := make([]float64, params.octaves)
	scale := params.inputScale
	amplitude := normalization
	for octave := 0; octave < params.octaves; octave++ {
		scales[octave] = scale
		amplitudes[octave] = amplitude
		scale *= 0.5
		amplitude /= params.persistence
	}
	return func(x, y float64) float64 {
		sum := 0.0
		for octave := range scales {
			sum += amplitudes[octave] * factorioBasisNoise(
				x*scales[octave]+float64(octave)*-1774.83,
				y*scales[octave],
				&tables,
			)
		}
		return params.outputScale * sum
	}
}

func factorioMultioctaveNormalization(persistence float64, octaves int) float64 {
	if persistence == 1 {
		return 1 / math.Sqrt(float64(octaves))
	}
	inversePersistenceSquared := 1 / (persistence * persistence)
	return math.Sqrt(
		(inversePersistenceSquared - 1) /
			(factorioFastPow(inversePersistenceSquared, float64(octaves)) - 1),
	)
}

type factorioQuickMultioctaveParams struct {
	seed0                       uint32
	seed1                       uint32
	octaves                     int
	inputScale                  float64
	outputScale                 float64
	octaveOutputScaleMultiplier float64
	octaveInputScaleMultiplier  float64
	offsetX                     float64
}

func makeFactorioQuickMultioctaveNoise(params factorioQuickMultioctaveParams) func(float64, float64) float64 {
	tables := make([]factorioBasisTables, params.octaves)
	scales := make([]float64, params.octaves)
	amplitudes := make([]float64, params.octaves)
	scale := params.inputScale
	amplitude := params.outputScale
	for octave := 0; octave < params.octaves; octave++ {
		tables[octave] = factorioBasisTablesFromSeed(params.seed0+uint32(octave), params.seed1)
		scales[octave] = scale
		amplitudes[octave] = amplitude
		scale *= params.octaveInputScaleMultiplier
		amplitude *= params.octaveOutputScaleMultiplier
	}
	return func(x, y float64) float64 {
		sum := 0.0
		for octave := range scales {
			sum += amplitudes[octave] * factorioBasisNoise(
				(x+params.offsetX)*scales[octave],
				y*scales[octave],
				&tables[octave],
			)
		}
		return sum
	}
}

type factorioQuickPersistenceParams struct {
	seed0                      uint32
	seed1                      uint32
	octaves                    int
	inputScale                 float64
	outputScale                float64
	octaveInputScaleMultiplier float64
	persistence                float64
}

func makeFactorioQuickPersistenceNoise(params factorioQuickPersistenceParams) func(float64, float64) float64 {
	octaveScale := math.Pow(params.octaveInputScaleMultiplier, float64(params.octaves-1))
	return makeFactorioQuickMultioctaveNoise(factorioQuickMultioctaveParams{
		seed0:                       params.seed0,
		seed1:                       params.seed1,
		octaves:                     params.octaves,
		inputScale:                  params.inputScale * octaveScale,
		outputScale:                 params.outputScale * math.Pow(2, float64(params.octaves-1)),
		octaveOutputScaleMultiplier: params.persistence,
		octaveInputScaleMultiplier:  1 / params.octaveInputScaleMultiplier,
	})
}

type factorioVariablePersistenceParams struct {
	seed0       uint32
	seed1       uint32
	octaves     int
	inputScale  float64
	outputScale float64
	offsetX     float64
}

func makeFactorioVariablePersistenceNoise(params factorioVariablePersistenceParams) func(float64, float64, float64) float64 {
	tables := factorioBasisTablesFromSeed(params.seed0, params.seed1)
	scales := make([]float64, params.octaves)
	scale := params.inputScale * 0.5
	for octave := range scales {
		scales[octave] = scale
		scale *= 0.5
	}
	gain := params.outputScale * math.Pow(2, float64(params.octaves))
	return func(x, y, persistence float64) float64 {
		accumulator := 0.0
		for octave := range scales {
			accumulator += factorioBasisNoise(
				(x+params.offsetX)*scales[octave]+float64(octave)*-7936,
				y*scales[octave],
				&tables,
			)
			if octave < len(scales)-1 {
				accumulator *= persistence
			}
		}
		return gain * accumulator
	}
}

type factorioAmplitudeCorrectedParams struct {
	seed0       uint32
	seed1       uint32
	octaves     int
	inputScale  float64
	offsetX     float64
	persistence float64
	amplitude   float64
}

func makeFactorioAmplitudeCorrectedNoise(params factorioAmplitudeCorrectedParams) func(float64, float64) float64 {
	ratio := 1 / float64(params.octaves)
	if params.persistence != 1 {
		ratio = (1 - params.persistence) /
			(1 - math.Pow(params.persistence, float64(params.octaves)))
	}
	noise := makeFactorioVariablePersistenceNoise(factorioVariablePersistenceParams{
		seed0:       params.seed0,
		seed1:       params.seed1,
		octaves:     params.octaves,
		inputScale:  params.inputScale,
		outputScale: ratio / math.Pow(2, float64(params.octaves)) * params.amplitude,
		offsetX:     params.offsetX,
	})
	return func(x, y float64) float64 {
		return noise(x, y, params.persistence)
	}
}

func factorioFastLog2(value float64) float64 {
	bits := math.Float32bits(float32(value))
	y := float32(float64(bits) * 1.1920928955078125e-7)
	mantissa := math.Float32frombits((bits & 0x007fffff) | 0x3f000000)
	return float64(float32(
		float64(y) - 124.22551499 -
			1.498030302*float64(mantissa) -
			1.72587999/(0.3520887068+float64(mantissa)),
	))
}

func factorioFastPow2(power float64) float64 {
	clipped := power
	if clipped < -126 {
		clipped = -126
	}
	whole := math.Trunc(clipped)
	fraction := float32(clipped - whole)
	if clipped < 0 {
		fraction = float32(float64(fraction) + 1)
	}
	value := float32(
		float64(uint32(1)<<23) *
			float64(float32(
				clipped+121.2740575+
					27.7280233/(4.84252568-float64(fraction))-
					1.49012907*float64(fraction),
			)),
	)
	return float64(math.Float32frombits(uint32(int32(value))))
}

func factorioFastPow(value, power float64) float64 {
	return factorioFastPow2(float64(float32(power * factorioFastLog2(value))))
}

func factorioFastCbrt(value float64) float64 {
	if value <= 0 {
		return 0
	}
	return factorioFastPow(value, 1.0/3.0)
}
