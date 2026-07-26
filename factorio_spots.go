package main

import (
	"math"
	"sort"
)

const (
	factorioSpotSeedBase       = 0x3fbe2c
	factorioSpotSeed1Prime     = 7927
	factorioSpotRegionXPrime   = 7919
	factorioSpotRegionYPrime   = 7907
	factorioSpotSpacingSqDecay = 15.0 / 16.0
	factorioSpotMaxCandidates  = 100_000
)

type factorioSpotRegionKey struct {
	seed0   uint32
	seed1   uint32
	regionX int64
	regionY int64
}

type factorioSpotCandidate struct {
	x float64
	y float64
}

type factorioSelectedSpot struct {
	x         float64
	y         float64
	quantity  float64
	coneScale float64
}

type factorioSpotSelectionParams struct {
	regionSize               int64
	candidateSpotCount       int
	spacing                  float64
	skipSpan                 int
	skipOffset               int
	hardRegionTargetQuantity bool
	density                  func(float64, float64) float64
	quantity                 func(float64, float64) float64
	quantityBatch            func([]factorioSpotCandidate) []float64
	favorability             func(float64, float64) float64
}

func factorioSpotSeedWord(key factorioSpotRegionKey) uint32 {
	word := uint32(
		int64(factorioSpotSeedBase)+
			factorioSpotSeed1Prime*int64(key.seed1)+
			factorioSpotRegionXPrime*key.regionX+
			factorioSpotRegionYPrime*key.regionY,
	) ^ key.seed0
	if word < factorioMinSeedWord {
		return factorioMinSeedWord
	}
	return word
}

func factorioSpotCandidatePoints(key factorioSpotRegionKey, regionSize int64, count int) []factorioSpotCandidate {
	state := newFactorioTaus88State(factorioSpotSeedWord(key))
	half := regionSize / 2
	points := make([]factorioSpotCandidate, count)
	for index := range points {
		points[index] = factorioSpotCandidate{
			x: float64(key.regionX*regionSize + int64(uint64(state.next())%uint64(regionSize)) - half),
			y: float64(key.regionY*regionSize + int64(uint64(state.next())%uint64(regionSize)) - half),
		}
	}
	return points
}

func factorioSelectSpots(key factorioSpotRegionKey, params factorioSpotSelectionParams) []factorioSelectedSpot {
	span := params.skipSpan
	if span <= 0 {
		span = 1
	}
	offset := params.skipOffset
	regionSize := params.regionSize
	half := regionSize / 2
	needed := params.candidateSpotCount * span
	state := newFactorioTaus88State(factorioSpotSeedWord(key))

	accepted := make([]factorioSpotCandidate, 0, needed)
	spacingSquared := params.spacing * params.spacing
	// Factorio decays the squared spacing target after each rejected dart.
	for tried := 0; len(accepted) < needed && tried < factorioSpotMaxCandidates; tried++ {
		candidate := factorioSpotCandidate{
			x: float64(key.regionX*regionSize + int64(uint64(state.next())%uint64(regionSize)) - half),
			y: float64(key.regionY*regionSize + int64(uint64(state.next())%uint64(regionSize)) - half),
		}
		valid := true
		for _, previous := range accepted {
			dx := candidate.x - previous.x
			dy := candidate.y - previous.y
			if dx*dx+dy*dy < spacingSquared {
				valid = false
				break
			}
		}
		if valid {
			accepted = append(accepted, candidate)
		} else {
			spacingSquared *= factorioSpotSpacingSqDecay
		}
	}

	mine := make([]factorioSpotCandidate, 0, params.candidateSpotCount)
	// Every resource shares this stream and owns one skip-set partition.
	for index, candidate := range accepted {
		if index%span == offset {
			mine = append(mine, candidate)
		}
	}
	if len(mine) == 0 {
		return nil
	}

	target := 0.0
	// The region budget is the mean sampled density multiplied by region area.
	for _, candidate := range mine {
		target += params.density(candidate.x, candidate.y)
	}
	target = target / float64(len(mine)) * float64(regionSize*regionSize)

	quantities := make([]float64, len(mine))
	if params.quantityBatch != nil {
		copy(quantities, params.quantityBatch(mine))
	} else {
		for index, candidate := range mine {
			quantities[index] = params.quantity(candidate.x, candidate.y)
		}
	}

	type rankedSpot struct {
		factorioSpotCandidate
		index        int
		favorability float64
	}
	ranked := make([]rankedSpot, len(mine))
	for index, candidate := range mine {
		ranked[index] = rankedSpot{
			factorioSpotCandidate: candidate,
			index:                 index,
			favorability:          params.favorability(candidate.x, candidate.y),
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].favorability > ranked[j].favorability
	})

	// Consume the budget in favorability order, shrinking the last hard-target cone.
	selected := make([]factorioSelectedSpot, 0, len(ranked))
	accumulated := 0.0
	for _, candidate := range ranked {
		if accumulated >= target {
			break
		}
		quantity := quantities[candidate.index]
		if quantity <= 0 {
			continue
		}
		coneScale := 1.0
		if params.hardRegionTargetQuantity && accumulated+quantity > target {
			trimmed := target - accumulated
			coneScale = factorioFastCbrt(trimmed / quantity)
			quantity = trimmed
		}
		selected = append(selected, factorioSelectedSpot{
			x:         candidate.x,
			y:         candidate.y,
			quantity:  quantity,
			coneScale: coneScale,
		})
		accumulated += quantity
	}
	return selected
}

func factorioRandomPenaltyBatch(
	positions []factorioSpotCandidate,
	source []float64,
	seed float64,
	amplitude float64,
) []float64 {
	result := make([]float64, len(positions))
	if len(positions) == 0 {
		return result
	}
	x := uint32(int32(math.Trunc(positions[0].x)))
	y := uint32(int32(math.Trunc(positions[0].y + seed)))
	word := uint32(factorioSpotSeedBase) + x*factorioSpotRegionXPrime + y*factorioSpotRegionYPrime
	if word < factorioMinSeedWord {
		word = factorioMinSeedWord
	}
	state := newFactorioTaus88State(word)
	// The noise operation streams from the last batch element to the first.
	for index := len(positions) - 1; index >= 0; index-- {
		if source[index] <= 0 {
			result[index] = source[index]
			continue
		}
		result[index] = source[index] - amplitude*float64(state.next())/4294967296.0
	}
	return result
}
