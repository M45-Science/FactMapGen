// SPDX-License-Identifier: AGPL-3.0-or-later
// See docs/gleba-rendering.md for game-data provenance.

package main

import "image/color"

// Terrain tile probabilities and map colors from Factorio 2.1.23,
// space-age/prototypes/tile/tiles-gleba.lua, in prototype order.
var factorioGlebaTiles = [...]factorioVulcanusTile{
	{"natural-yumako-soil", color.RGBA{185, 166, 5, 255}},
	{"natural-jellynut-soil", color.RGBA{185, 5, 166, 255}},
	{"lowland-olive-blubber", color.RGBA{66, 82, 11, 255}},
	{"lowland-olive-blubber-2", color.RGBA{66, 82, 11, 255}},
	{"lowland-olive-blubber-3", color.RGBA{66, 82, 11, 255}},
	{"lowland-brown-blubber", color.RGBA{66, 82, 11, 255}},
	{"lowland-pale-green", color.RGBA{66, 82, 11, 255}},
	{"lowland-cream-cauliflower-2", color.RGBA{95, 93, 88, 255}},
	{"lowland-cream-cauliflower", color.RGBA{95, 93, 88, 255}},
	{"lowland-dead-skin", color.RGBA{95, 93, 88, 255}},
	{"lowland-dead-skin-2", color.RGBA{95, 93, 88, 255}},
	{"lowland-cream-red", color.RGBA{95, 83, 78, 255}},
	{"lowland-red-vein-2", color.RGBA{115, 53, 66, 255}},
	{"lowland-red-vein", color.RGBA{115, 53, 66, 255}},
	{"lowland-red-vein-3", color.RGBA{115, 53, 66, 255}},
	{"lowland-red-vein-4", color.RGBA{115, 53, 66, 255}},
	{"lowland-red-vein-dead", color.RGBA{115, 53, 66, 255}},
	{"lowland-red-infection", color.RGBA{115, 53, 66, 255}},
	{"midland-cracked-lichen", color.RGBA{81, 77, 44, 255}},
	{"midland-cracked-lichen-dull", color.RGBA{71, 67, 40, 255}},
	{"midland-cracked-lichen-dark", color.RGBA{61, 57, 30, 255}},
	{"midland-turquoise-bark-2", color.RGBA{46, 68, 48, 255}},
	{"midland-turquoise-bark", color.RGBA{46, 68, 48, 255}},
	{"midland-yellow-crust-3", color.RGBA{114, 86, 40, 255}},
	{"midland-yellow-crust-2", color.RGBA{114, 86, 40, 255}},
	{"midland-yellow-crust", color.RGBA{114, 86, 40, 255}},
	{"midland-yellow-crust-4", color.RGBA{114, 86, 40, 255}},
	{"highland-dark-rock", color.RGBA{52, 55, 48, 255}},
	{"highland-dark-rock-2", color.RGBA{52, 55, 48, 255}},
	{"highland-yellow-rock", color.RGBA{52, 55, 48, 255}},
	{"pit-rock", color.RGBA{50, 50, 58, 255}},
	{"wetland-yumako", color.RGBA{132, 119, 7, 255}},
	{"wetland-jellynut", color.RGBA{132, 7, 119, 255}},
	{"wetland-dead-skin", color.RGBA{46, 45, 51, 255}},
	{"wetland-light-dead-skin", color.RGBA{51, 50, 56, 255}},
	{"wetland-green-slime", color.RGBA{25, 53, 25, 255}},
	{"wetland-light-green-slime", color.RGBA{35, 63, 35, 255}},
	{"wetland-red-tentacle", color.RGBA{54, 15, 24, 255}},
	{"wetland-pink-tentacle", color.RGBA{64, 19, 34, 255}},
	{"wetland-blue-slime", color.RGBA{25, 49, 58, 255}},
	{"gleba-deep-lake", color.RGBA{18, 37, 51, 255}},
}

func (s factorioGlebaSample) tileScores(temperature, pitrock float32) [41]float32 {
	gleba_aux, gleba_elevation, elevation := s.aux, s.elevation, s.elevation
	gleba_moisture, gleba_temperature_normalised := s.moisture, temperature
	gleba_fertile_solid, gleba_fertile_spots_coastal := s.fertileSolid, s.fertile
	gleba_biome_mask_green, gleba_biome_mask_red := noiseBool(s.aux > .375), noiseBool(s.aux < .625)
	gleba_above_deep_water_mask := noiseBool(s.elevation > -10)
	gleba_plants_noise, gleba_plants_noise_b := s.plants, s.plantsB
	gleba_lowland, gleba_midland, gleba_highland := glebaSelect(s.elevation, 1, 20, .5, 0, 1), glebaSelect(s.elevation, 20, 120, .5, 0, 1), glebaSelect(s.elevation, 120, 1000, .5, 0, 1)
	gleba_rockpools_pitrock := pitrock
	gleba_rockpools_shallow := glebaSelect(s.rockpools, .6, .8, .1, 0, 1)
	gleba_rockpools_bluewater := glebaSelect(s.rockpools, .8, 1.1, .1, 0, 1)
	gleba_rockpools_deepwater := glebaSelect(s.rockpools, 1.1, 2, .1, 0, 1)
	gleba_shallows, gleba_shallows_edge := glebaSelect(s.elevation, -5, -2, .5, 0, 1), glebaSelect(s.elevation, -2, 2, .5, 0, 1)
	gleba_shallows_aux_1, gleba_shallows_aux_2, gleba_shallows_aux_3 := gleba_shallows*glebaSelect(s.aux, 0, .3, .005, 0, 1), gleba_shallows*glebaSelect(s.aux, .3, .7, .005, 0, 1), gleba_shallows*glebaSelect(s.aux, .7, 1, .005, 0, 1)
	gleba_shallows_edge_aux_1, gleba_shallows_edge_aux_2, gleba_shallows_edge_aux_3 := gleba_shallows_edge*glebaSelect(s.aux, 0, .3, .005, 0, 1), gleba_shallows_edge*glebaSelect(s.aux, .3, .7, .005, 0, 1), gleba_shallows_edge*glebaSelect(s.aux, .7, 1, .005, 0, 1)
	const gleba_deep_water_level = -10
	return [41]float32{
		gleba_fertile_solid*50000 - 40000 - gleba_biome_mask_green*1000000,                                                                                              // natural-yumako-soil
		gleba_fertile_solid*50000 - 40000 - gleba_biome_mask_red*1000000,                                                                                                // natural-jellynut-soil
		gleba_lowland*glebaSelect(gleba_aux, 0, 0.4, 0.005, 0, 1) + 0.1*glebaSelect(gleba_temperature_normalised, 0.3, 0.8, 0.005, 0, 1),                                // lowland-olive-blubber
		gleba_lowland*glebaSelect(gleba_aux, 0, 0.2, 0.005, 0, 1) + 0.1*glebaSelect(gleba_temperature_normalised, 0, 0.4, 0.005, 0, 1),                                  // lowland-olive-blubber-2
		gleba_lowland*glebaSelect(gleba_aux, 0, 0.4, 0.005, 0, 1) + 0.1*glebaSelect(gleba_temperature_normalised, 0.8, 1, 0.005, 0, 1),                                  // lowland-olive-blubber-3
		gleba_lowland*glebaSelect(gleba_aux, 0, 0.4, 0.005, 0, 1) + 0.1*(gleba_plants_noise_b-0.8),                                                                      // lowland-brown-blubber
		gleba_lowland*glebaSelect(gleba_aux, 0.2, 0.4, 0.005, 0, 1) + 0.1*glebaSelect(gleba_temperature_normalised, 0, 0.3, 0.005, 0, 1),                                // lowland-pale-green
		gleba_lowland*glebaSelect(gleba_aux, 0.3, 0.45, 0.005, 0, 1) + 0.1*glebaSelect(gleba_temperature_normalised, -0.2, 1, 0.005, 0, 1),                              // lowland-cream-cauliflower-2
		gleba_lowland*glebaSelect(gleba_aux, 0.45, 0.65, 0.005, 0, 1) + 0.1*glebaSelect(gleba_temperature_normalised, -0.2, 1, 0.005, 0, 1),                             // lowland-cream-cauliflower
		gleba_lowland*glebaSelect(gleba_aux, 0.45, 0.65, 0.005, 0, 1) + 0.1*glebaSelect(gleba_temperature_normalised, -1, -0.2, 0.005, 0, 1),                            // lowland-dead-skin
		gleba_lowland*glebaSelect(gleba_aux, 0.3, 0.45, 0.005, 0, 1) + 0.1*glebaSelect(gleba_temperature_normalised, -1, -0.2, 0.005, 0, 1),                             // lowland-dead-skin-2
		gleba_lowland * glebaSelect(gleba_aux, 0.62, 0.77, 0.005, 0, 1) * glebaSelect(gleba_temperature_normalised, 0.25, 1, 0.005, 0, 1),                               // lowland-cream-red
		gleba_lowland*glebaSelect(gleba_aux, 0.75, 1, 0.005, 0, 1) + 0.1*(gleba_plants_noise_b-1),                                                                       // lowland-red-vein-2
		gleba_lowland * glebaSelect(gleba_aux, 0.7, 1, 0.005, 0, 1) * glebaSelect(gleba_temperature_normalised, 0.45, 0.75, 0.005, 0, 1),                                // lowland-red-vein
		gleba_lowland * glebaSelect(gleba_aux, 0.7, 1, 0.005, 0, 1) * glebaSelect(gleba_temperature_normalised, 0.25, 0.45, 0.005, 0, 1),                                // lowland-red-vein-3
		gleba_lowland * glebaSelect(gleba_aux, 0.7, 1, 0.005, 0, 1) * glebaSelect(gleba_temperature_normalised, 0.75, 1, 0.005, 0, 1),                                   // lowland-red-vein-4
		gleba_lowland * glebaSelect(gleba_aux, 0.6, 1, 0.005, 0, 1) * glebaSelect(gleba_temperature_normalised, 0, 0.25, 0.005, 0, 1),                                   // lowland-red-vein-dead
		gleba_lowland*glebaSelect(gleba_aux, 0.7, 1, 0.05, 0, 1) + 0.1*(gleba_plants_noise-1),                                                                           // lowland-red-infection
		gleba_midland*glebaSelect(gleba_aux, 0.55, 0.65, 0.15, 0, 1) - 0.2*gleba_temperature_normalised,                                                                 // midland-cracked-lichen
		gleba_midland*glebaSelect(gleba_aux, 0.45, 0.55, 0.15, 0, 1) + 0.2*gleba_temperature_normalised,                                                                 // midland-cracked-lichen-dull
		max(1.08*gleba_midland*glebaSelect(gleba_aux, 0, 0.55, 0.1, 0, 1), 1.15*gleba_highland) + 0.3*(gleba_plants_noise_b-0.7),                                        // midland-cracked-lichen-dark
		gleba_midland*glebaSelect(gleba_aux, 0.05, 0.4, 0.2, 0, 1) - 0.2*gleba_temperature_normalised,                                                                   // midland-turquoise-bark-2
		gleba_midland*glebaSelect(gleba_aux, 0, 0.35, 0.2, 0, 1) + 0.2*gleba_temperature_normalised,                                                                     // midland-turquoise-bark
		gleba_midland * glebaSelect(gleba_aux, 0.65, 1, 0.1, 0, 1),                                                                                                      // midland-yellow-crust-3
		gleba_midland * glebaSelect(gleba_aux, 0.8, 1, 0.15, 0, 1) * (0.93 + 0.1*glebaSelect(gleba_moisture, 0.5, 0.5, 0.25, 0, 1) - 0.1*gleba_temperature_normalised),  // midland-yellow-crust-2
		gleba_midland * glebaSelect(gleba_aux, 0.7, 1, 0.1, 0, 1) * (1 + 0.1*(gleba_aux-0.75) + 0.05*gleba_plants_noise_b),                                              // midland-yellow-crust
		gleba_midland * glebaSelect(gleba_aux, 0.8, 1, 0.15, 0, 1) * (0.9 + 0.1*glebaSelect(gleba_moisture, 0.25, 0.25, 0.25, 0, 1) + 0.1*gleba_temperature_normalised), // midland-yellow-crust-4
		gleba_highland * max(0, 1.05+0.1*(1-gleba_aux)+0.2*gleba_temperature_normalised),                                                                                // highland-dark-rock
		gleba_highland * max(0, 1.13+0.1*(min(gleba_aux+0.1, 1-gleba_aux))),                                                                                             // highland-dark-rock-2
		gleba_highland * max(0, 1+0.1*gleba_aux-0.2*gleba_temperature_normalised),                                                                                       // highland-yellow-rock
		2 * gleba_rockpools_pitrock, // pit-rock
		gleba_fertile_spots_coastal * 5000 * (1 - gleba_biome_mask_green) * gleba_above_deep_water_mask,           // wetland-yumako
		gleba_fertile_spots_coastal * 5000 * (1 - gleba_biome_mask_red) * gleba_above_deep_water_mask,             // wetland-jellynut
		gleba_shallows_aux_2 + 2*min(glebaSelect(gleba_aux, 0.3, 0.7, 0.005, 0, 1), gleba_rockpools_shallow),      // wetland-dead-skin
		gleba_shallows_edge_aux_2 + 2*min(glebaSelect(gleba_aux, 0.3, 0.7, 0.005, 0, 1), gleba_rockpools_shallow), // wetland-light-dead-skin
		gleba_shallows_aux_1 + 2*min(glebaSelect(gleba_aux, 0, 0.3, 0.005, 0, 1), gleba_rockpools_shallow),        // wetland-green-slime
		gleba_shallows_edge_aux_1 + 2*min(glebaSelect(gleba_aux, 0, 0.3, 0.005, 0, 1), gleba_rockpools_shallow),   // wetland-light-green-slime
		gleba_shallows_aux_3 + 2*min(glebaSelect(gleba_aux, 0.7, 2, 0.005, 0, 1), gleba_rockpools_shallow),        // wetland-red-tentacle
		gleba_shallows_edge_aux_3, // wetland-pink-tentacle
		6*glebaSelect(gleba_elevation, gleba_deep_water_level, -4, 0.5, 0, 1) + 5*gleba_rockpools_bluewater, // wetland-blue-slime
		10*glebaSelect(elevation, -500, gleba_deep_water_level, 0.5, 0, 1) + 10*gleba_rockpools_deepwater,   // gleba-deep-lake
	}
}
