// SPDX-License-Identifier: AGPL-3.0-or-later
// Adapted from FactorioMapWebUI; see docs/upstream-rendering.md for provenance.

package main

import (
	"hash/crc32"
	"image/color"
	"math"
)

type factorioFulgoraEvaluator struct {
	settings                                                                             fastPreviewSettings
	grid, size                                                                           float32
	wobbleX, wobbleY, influence, basis, oil, rock, dunes, scrap, walls, paving, subnoise func(float32, float32) float32
	cells, spots, roads, structures                                                      factorioVoronoi
	start, tight, vault                                                                  factorioNoiseCone
}
type factorioFulgoraSample struct {
	distance                                                                                                                    float32
	tile                                                                                                                        factorioVulcanusTile
	elevation, cells, pyramids, spots, natural, mixOil, startingMask, vaults, roadPaving, structure, subnoise, scrapProbability float32
	naturalMask, mesa, artificial, machineDensity, vaultStart, rock, scrap, basisOil, oilMask                                   float32
}

func newFactorioFulgoraEvaluator(settings fastPreviewSettings, gridMode ...bool) *factorioFulgoraEvaluator {
	c := fastSpaceAgeControl(settings, "fulgora_islands")
	grid := float32(175) - noiseSliderLinear(float32(positiveOr(c.frequency, 1)), -50, 50)
	noise := func(name string, n int, p, scale, out float32) func(float32, float32) float32 {
		grid := len(gridMode) > 0 && gridMode[0] && name != "fulgora_basis" && name != "fulgora_basis_oil" && name != "fulgora_structure_subnoise"
		return makeFactorioMultioctaveNoise32(settings.seed, crc32.ChecksumIEEE([]byte(name)), n, p, scale, out, grid)
	}
	angle := float32(settings.seed) / 360
	e := &factorioFulgoraEvaluator{
		settings: settings, grid: grid, size: noiseSliderRescale(float32(max(0, c.size)), 2),
		wobbleX:   noise("fulgora_wobble_x", 4, 0.7, 5/grid, grid*0.07),
		wobbleY:   noise("fulgora_wobble_y", 4, 0.7, 5/grid, grid*0.07),
		influence: makeFactorioMultioctaveNoise32(settings.seed, 1, 3, 0.5, (128/grid)/20, 3, gridMode...),
		basis:     noise("fulgora_basis", 6, 0.5, (128/grid)/7.5, 0.5),
		oil:       noise("fulgora_basis_oil", 4, 0.65, 0.1, 1),
		rock:      noise("fulgora_rock", 4, 0.7, 1.0/3, 1),
		dunes:     noise("fulgora_dunes", 3, 0.7, 1.0/6, 1),
		scrap:     noise("fulgora_scrap_medium", 3, 0.7, 1.0/18, 1),
		walls:     noise("fulgora_ruins_walls", 3, 0.7, 1.0/6, 1),
		paving:    noise("fulgora_ruins_paving", 3, 0.7, 1.0/16, 1),
		subnoise:  noise("fulgora_structure_subnoise", 3, 0.7, 1.0/12, 1),
		start:     newFactorioNoiseCone(angle, grid/30, grid/1.8), tight: newFactorioNoiseCone(angle, 1, grid/4),
		vault: newFactorioNoiseCone(angle+180, grid/1.8, grid/1.8),
	}
	e.cells = newFactorioVoronoi(settings.seed, crc32.ChecksumIEEE([]byte("fulgora_cells")), grid, 0.6, voronoiManhattan)
	e.spots = newFactorioVoronoi(settings.seed, crc32.ChecksumIEEE([]byte("fulgora_cells")), grid, 0.6, voronoiEuclidean)
	e.roads = newFactorioVoronoi(settings.seed, crc32.ChecksumIEEE([]byte("fulgora_road_cells")), grid/3, 1, voronoiChebyshev)
	e.structures = newFactorioVoronoi(settings.seed, crc32.ChecksumIEEE([]byte("fulgora_structure_cells")), grid/8, 0.8, voronoiMinkowski)
	return e
}

func (e *factorioFulgoraEvaluator) sample(px, py float64) factorioFulgoraSample {
	x, y := float32(px), float32(py)
	wobX, wobY := e.wobbleX(x, y), e.wobbleY(x, y)
	mask := min(1, max(0, e.influence(x, y)+0.6))
	ox, oy := x+e.grid/2, y+e.grid/2
	wx, wy := ox+wobX*mask, oy+wobY*mask
	// Starting spots follow the nearest configured starting point.
	sx, sy := x, y
	if len(e.settings.startingPositions) > 0 {
		best := float32(math.MaxFloat32)
		for _, p := range e.settings.startingPositions {
			dx, dy := x-float32(p.x), y-float32(p.y)
			d := dx*dx + dy*dy
			if d < best {
				best, sx, sy = d, dx, dy
			}
		}
	}
	start := max(0, e.start.at(sx, sy, wobX, wobY), e.tight.at(sx, sy, wobX*0.25, wobY*0.25))
	vaultStart := max(0, e.vault.at(sx, sy, wobX, wobY))
	startingMask, vaultMask := noiseBool(start > vaultStart), noiseBool(vaultStart > start)
	cell := e.cells.sample(wx, wy, true)
	spots := e.spots.sample(ox+wobX/2, oy+wobY/2, false).spot
	mesa := noiseBool(cell.id > 0.75)
	sprawl := noiseBool(cell.id > 0.5) - mesa
	vaults := 1 - noiseBool(cell.id < 0.33) - sprawl - mesa
	vaultsAndStart := max(vaults, vaultMask)
	basis, oil := e.basis(wx, wy), e.oil(x+1.5*wobX, y+1.5*wobY)
	rock, dunes, scrap := float32(0.33)+noiseAbs(e.rock(x, y)), float32(0.66)-noiseAbs(e.dunes(x, y)), e.scrap(x, y)
	natural := (basis*2)*e.size - 0.85
	sprawlPyramids := cell.pyramid * (sprawl + mesa*min(1, noiseAbs(0.9-0.2*oil+0.05*rock)))
	vaultPyramids := max(vaults*cell.pyramid, 0.5*vaultStart, 0.5*start)
	moats := min(0.25, 1.5*max(-0.05-vaultPyramids*2, (vaultPyramids-0.35)*2))
	mixPyramids := min(0.25, (sprawlPyramids-0.185)*4)
	mixNatural := max(natural, mixPyramids)
	mixMoats := noiseLerp(mixNatural, moats, max(vaultsAndStart, startingMask))
	vaultSpots := min(0.25, -10+11.5*max(vaults*(1-spots), vaultMask*(0.5+0.5*vaultStart), startingMask*(0.5+0.5*start)))
	mixSpots := max(mixMoats, vaultSpots) + max(0, start-0.8)
	oilMask := noiseBool(mixSpots < 0)
	mixOil := noiseLerp(mixSpots, min(-0.01, mixSpots-0.4+0.6*oil), oilMask)
	sandBasins := min(mixOil, 0.6-mixOil)
	elevation := sandBasins*60 + 80 + (noiseBool(sandBasins > 0)-0.5)*20
	s := factorioFulgoraSample{elevation: elevation, cells: cell.id, pyramids: cell.pyramid, spots: spots, natural: natural, mixOil: mixOil, startingMask: startingMask, vaults: vaultsAndStart, vaultStart: vaultStart, rock: rock, scrap: scrap, basisOil: oil, oilMask: oilMask, mesa: mesa, distance: noiseSqrt(sx*sx + sy*sy)}
	// The four water probabilities dominate every land score when finite.
	// Skip NaN (0 * -Inf) independently for each candidate.
	waterBase := func(level, bonus float32) float32 {
		if elevation > level {
			return float32(math.Inf(-1))
		}
		return bonus * min(level-elevation, 1)
	}
	water := float32(50) * oilMask * waterBase(80, 1000)
	deep := float32(100) * oilMask * waterBase(20, 2000)
	scores := [4]float32{water * max(-(scrap+dunes), 0), water * max(scrap+dunes, 0), deep, (-min(0, elevation-60)/100 + max(0, dunes-max(0, elevation/100))) * deep}
	best := float32(math.Inf(-1))
	winner := -1
	for i, score := range scores {
		if score > best {
			best, winner = score, i
		}
	}
	if winner >= 0 && best > 0 {
		names := [4]string{"oil-ocean-shallow", "oil-ocean-shallow-2", "oil-ocean-deep", "oil-ocean-deep-2"}
		c := color.RGBA{74, 42, 43, 255}
		if winner >= 2 {
			c = color.RGBA{56, 35, 40, 255}
		}
		s.tile = factorioVulcanusTile{names[winner], c}
		return s
	}
	naturalMask := max(min(noiseBool(natural > mixPyramids), 1-vaultsAndStart), startingMask)
	naturalMesa := max(naturalMask, mesa)
	artificial := 1 - max(oilMask, naturalMesa)
	s.naturalMask, s.artificial = naturalMask, artificial
	road := e.roads.sample(x, y, true)
	structure := e.structures.sample(x, y*0.8, false)
	subnoise := e.subnoise(x+10000*structure.id, y)
	band := func(v float32) float32 { return v - float32(math.Floor(float64(v))) }
	preband := min(spots, (1-vaultStart)/2)*9 + 0.5
	thin := max(noiseBool(road.pyramid < 0.03)*0.9, noiseBool(structure.facet < 0.06)*0.5)
	paving2 := max(noiseBool(road.pyramid < 0.05)*0.9, noiseBool(band(cell.pyramid*8) < 0.1)*0.85*noiseBool(road.id < 0.6)*noiseBool(road.id > 0.25), noiseBool(band(preband) < 0.1)*0.85*noiseBool(road.id < 0.25), noiseBool(structure.facet < 0.1)*0.85*noiseBool(road.id > 0.6))
	paving2 = noiseLerp(paving2, noiseBool(structure.facet < 0.2)*0.9, noiseBool(structure.id > 0.8))
	paving2 = noiseLerp(paving2, noiseBool(preband > 1)*0.9, noiseBool(preband < 1.3))
	roadDust := noiseBool(road.pyramid < 0.08)*0.9 - paving2
	walls, paving := float32(0.66)-noiseAbs(e.walls(x, y)), noiseAbs(e.paving(x, y))
	landScores := [8]float32{
		scrap + max(0, natural, 2*mesa*cell.pyramid)*2 - 0.9 + rock + roadDust*sprawl,
		1 + dunes, 1 - dunes, 0.8 + rock*2 - max(0, mixOil)*6,
		max(naturalMesa*(3*paving*thin-0.5), artificial*(4*paving2+paving-1)),
		max(naturalMesa*(2*walls+paving-0.5), artificial*(0.25*walls+0.25*subnoise-4*structure.facet-paving2+2.5)),
		artificial*(walls+subnoise+2*structure.facet-paving2+0.2+0.3*vaultsAndStart) - paving2,
		artificial*(-walls+1.25*subnoise+2.5*structure.facet-paving2-0.2+0.3*vaultsAndStart+2*noiseBool(preband < 1)) - paving2,
	}
	s.machineDensity = max(landScores[5], 2*landScores[6], 3*landScores[7])
	tiles := [8]factorioVulcanusTile{
		{"fulgoran-dust", color.RGBA{112, 65, 50, 255}}, {"fulgoran-dunes", color.RGBA{125, 71, 59, 255}},
		{"fulgoran-sand", color.RGBA{118, 68, 56, 255}}, {"fulgoran-rock", color.RGBA{131, 85, 66, 255}},
		{"fulgoran-paving", color.RGBA{120, 94, 67, 255}}, {"fulgoran-walls", color.RGBA{114, 75, 65, 255}},
		{"fulgoran-conduit", color.RGBA{100, 79, 68, 255}}, {"fulgoran-machinery", color.RGBA{89, 79, 68, 255}},
	}
	winner = 0
	for i, score := range landScores {
		if score > landScores[winner] {
			winner = i
		}
	}
	s.tile = tiles[winner]
	s.roadPaving = paving2
	s.structure = structure.id
	s.subnoise = subnoise
	c := fastSpaceAgeControl(e.settings, "scrap")
	if c.enabled && c.size > 0 && c.richness > 0 {
		frequency := float32(c.frequency)
		structural := noiseBool(structure.id < min(0.1*frequency, 0.05+0.05*frequency)) * (1 + subnoise) * noiseBool(elevation > 90) * artificial
		vault := noiseBool(preband < 1.2+0.4*noiseSliderLinear(float32(c.size), -1, 1)) * vaultsAndStart * 10
		s.scrapProbability = (1 - startingMask) * min(structural+vault, 0.5) * (1 - paving2)
	}
	return s
}
