package main

import "sync"

type glebaEnemyFields struct {
	mu   sync.Mutex
	base *factorioEnemyEvaluator
}

func newGlebaEnemyFields(settings fastPreviewSettings, grid ...bool) *glebaEnemyFields {
	settings.enemyBases = fastSpaceAgeControl(settings, "gleba_enemy_base")
	// Gleba doubles the base frequency, with the same radius/intensity terms.
	settings.enemyBases.frequency *= 2
	return &glebaEnemyFields{base: newFactorioEnemyEvaluator(settings, grid...)}
}

func (f *glebaEnemyFields) probabilities(positions []factorioSpotCandidate) []float64 {
	f.mu.Lock()
	values := f.base.prototypeValues(positions, factorioEnemyPrototype{seed: 8})
	f.base.trimRegionCache(fastPreviewMaxRegionsPerField)
	f.mu.Unlock()
	return values
}

func glebaSpawnerProbabilities(s factorioGlebaSample, base float64) [2]float32 {
	coastal := min(float32(.001), s.fertile*5000-noiseBool(s.aux > .375)*25000)
	regular := min(float32(.02), float32(base))
	water := noiseBool(s.elevation > -10)
	return [2]float32{
		max(.01*s.startingEnemies, max(regular, coastal)*noiseBool(s.distance > 350)) * water,
		max(.02*s.startingEnemies, .02*s.startingEnemiesSafe, regular, coastal) * water,
	}
}
