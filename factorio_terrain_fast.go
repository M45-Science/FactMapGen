package main

import "math"

// factorioTerrainNoiseAbsMax conservatively exceeds the existing 1.8 basis bound
// across four terrain octaves (approximately 2.2362 after normalization).
const factorioTerrainNoiseAbsMax = 2.25

func (e *factorioNauvisEvaluator) terrainTileFast(
	sample factorioNauvisSample,
	x, y float64,
) factorioTerrainTile {
	winnerIndex := 0
	winnerProbability := factorioWaterBase(sample.elevation, -2, 200)
	waterProbability := factorioWaterBase(sample.elevation, 0, 100)
	if waterProbability > winnerProbability {
		winnerIndex = 1
		winnerProbability = waterProbability
	}
	if waterProbability >= 5 {
		return factorioTerrainTiles[winnerIndex]
	}

	bases := [...]float64{
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.7, 11, 11),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.45, 0.45, 11, 0.8),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.6, 0.65, 0.9),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.5, 0.55, 0.7),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.45, -10, 0.55, 0.35),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.25, 0.45, 0.3),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.4, -10, 0.45, 0.25),
		),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.3, 0.45, 0.35),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.35, 0.55, 0.4),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.55, -10, 0.6, 0.35),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.6, 0.3, 11, 0.35),
		),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.4, 0.55, 0.45),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.45, 0.55, 0.5),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.5, 0.55, 0.55),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, -10, 0.25, 0.15),
			factorioExpressionInRange2(5, math.Inf(1), sample.elevation, sample.aux, -1.5, 0.5, 1.5, 1),
		),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.15, 0.3, 0.2),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.25, -10, 0.3, 0.15),
		),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, -10, 0.2, 0.4, 0.25),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.3, -10, 0.4, 0.2),
		),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.55, 0.35, 11, 0.5),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.6, -10, 0.7, 0.3),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.7, 0.25, 11, 0.3),
		),
		max(
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.7, -10, 0.8, 0.25),
			factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.8, 0.2, 11, 0.25),
		),
		factorioExpressionInRangeBase(sample.aux, sample.moisture, 0.8, -10, 11, 0.2),
	}

	bestBase := 0
	for index := 1; index < len(bases); index++ {
		if bases[index] > bases[bestBase] {
			bestBase = index
		}
	}
	consider := func(index int) {
		probability := bases[index] + e.terrainNoise[index](x, y)
		tileIndex := index + 2
		if probability > winnerProbability ||
			(probability == winnerProbability && tileIndex < winnerIndex) {
			winnerIndex = tileIndex
			winnerProbability = probability
		}
	}
	consider(bestBase)
	for index, base := range bases {
		if index == bestBase || base+factorioTerrainNoiseAbsMax <= winnerProbability {
			continue
		}
		consider(index)
	}
	return factorioTerrainTiles[winnerIndex]
}
