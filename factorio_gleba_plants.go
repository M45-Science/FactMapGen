// SPDX-License-Identifier: AGPL-3.0-or-later
package main

// Probability expressions from Factorio 2.1.23's final plant prototypes.
// Tile restrictions and collision masks are applied by the placement pass.
var glebaPlantNames = [...]string{"yumako-tree", "jellystem", "sunnycomb", "boompuff", "stingfrond", "lickmaw", "teflilly", "funneltrunk", "slipstack", "hairyclubnub", "cuttlepop", "water-cane"}

func glebaPlantProbabilities(s factorioGlebaSample, size, decal float32) [12]float32 {
	var out [12]float32
	waterRamp := glebaClamp((.8-s.moisture)*20, 0, 1)
	{ // yumako-tree

		out[0] = min(0.2, 0.3*(1-s.plants)*size)
	}
	{ // jellystem

		out[1] = min(0.2, 0.3*(1-s.plants)*size)
	}
	{ // sunnycomb
		main_box := glebaSelect(s.moisture, 0, 0.25, 0.01, -10, 1) - 1
		main_probability := min(0.08, 0.15*(main_box+s.plantsB-0.45)*size)
		invasion_tall_box := glebaSelect(s.moisture, 0, 0.35, 0.01, -10, 1) - 1
		invasion_tall_probability := min(0.05, 0.15*(invasion_tall_box+s.plantsB-0.4)*size)
		out[2] = max(main_probability, invasion_tall_probability)
	}
	{ // boompuff
		main_box := min(glebaSelect(s.aux, 0.5, 1, 0.05, -10, 1), glebaSelect(s.moisture, 0.25, 0.5, 0.01, -10, 1)) - 1
		main_probability := min(0.1, 0.15*(main_box+s.plants-0.45)*size)
		invasion_tall_box := min(glebaSelect(s.aux, 0.55, 1, 0.15, -10, 1), glebaSelect(s.moisture, 0.1, 0.6, 0.01, -10, 1)) - 1
		invasion_tall_probability := min(0.05, 0.1*(invasion_tall_box+s.plants-0.55)*size)
		out[3] = max(main_probability, invasion_tall_probability)
	}
	{ // stingfrond
		main_box := min(glebaSelect(s.aux, 0, 0.5, 0.15, -10, 1), glebaSelect(s.moisture, 0.25, 0.5, 0.01, -10, 1)) - 1
		main_probability := min(0.05, 0.1*(main_box+s.plants-0.25)*size)
		invasion_top_box := min(glebaSelect(s.aux, 0, 0.5, 0.15, -10, 1), glebaSelect(s.moisture, 0.1, 0.25, 0.01, -10, 1)) - 1
		invasion_top_probability := min(0.05, 0.1*(invasion_top_box+s.plants-0.55)*size)
		out[4] = max(main_probability, invasion_top_probability)
	}
	{ // lickmaw
		main_box := min(glebaSelect(s.aux, 0.7, 1, 0.15, -10, 1), glebaSelect(s.moisture, 0.5, 0.75, 0.01, -10, 1)) - 1
		main_probability := min(0.02, 0.1*(main_box+s.plants-0.2)*size)
		out[5] = main_probability
	}
	{ // teflilly
		main_box := min(glebaSelect(s.aux, 0.3, 0.7, 0.15, -10, 1), glebaSelect(s.moisture, 0.5, 0.75, 0.01, -10, 1)) - 1
		main_probability := min(0.1, 0.15*(main_box+s.plantsB-0.3)*size)
		invasion_middle_box := min(glebaSelect(s.aux, 0.55, 0.85, 0.15, -10, 1), glebaSelect(s.moisture, 0.4, 0.7, 0.01, -10, 1)) - 1
		invasion_middle_probability := min(0.05, 0.15*(invasion_middle_box+s.plantsB-0.55)*size)
		out[6] = max(main_probability, invasion_middle_probability)
	}
	{ // funneltrunk
		main_box := min(glebaSelect(s.aux, 0, 0.4, 0.05, -10, 1), glebaSelect(s.moisture, 0.5, 0.75, 0.01, -10, 1)) - 1
		main_probability := min(0.08, 0.15*(main_box+s.plantsB-0.25)*size)
		invasion_middle_box := min(glebaSelect(s.aux, 0, 0.55, 0.05, -10, 1), glebaSelect(s.moisture, 0.4, 0.6, 0.01, -10, 1)) - 1
		invasion_middle_probability := min(0.01, 0.1*(invasion_middle_box+s.plantsB-0.5)*size)
		out[7] = max(main_probability, invasion_middle_probability)
	}
	{ // slipstack
		main_box := glebaSelect(s.aux, 0.7, 1, 0.1, -10, 1) - 1
		main_probability := min(0.02, waterRamp*0.2*(main_box+s.plants-0.2)*size)
		out[8] = main_probability
	}
	{ // hairyclubnub
		main_box := glebaSelect(s.aux, 0, 0.3, 0.15, -10, 1) - 1
		main_probability := min(0.02, waterRamp*0.2*(main_box+s.plants-0.2)*size)
		out[9] = main_probability
	}
	{ // cuttlepop
		main_box := glebaSelect(s.aux, 0.35, 0.65, 0.1, -10, 1) - 1
		main_probability := min(0.03, waterRamp*0.2*(main_box+s.plantsB-0.2)*size)
		out[10] = main_probability
	}
	{ // water-cane

		out[11] = min(0.8, (min(1, 1.5*waterRamp)+0.5*decal-s.plants-0.5*glebaSelect(s.aux, 0.45, 0.55, 0.2, 0, 1)-0.7)*size)
	}
	return out
}
