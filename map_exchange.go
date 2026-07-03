// Portions of the binary map exchange parser are adapted from
// rfvgyhn/factorio-exchange-string-parser.
//
// # MIT License
//
// # Copyright (c) 2024 rfvgyhn
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"sort"
	"strings"
	"unicode"
)

// MapExchangeData contains the JSON payloads Factorio accepts as map settings.
type MapExchangeData struct {
	Version        [4]uint16
	MapGenSettings map[string]interface{}
	MapSettings    map[string]interface{}
	Checksum       uint32
	ChecksumOK     bool
}

// ParseMapExchangeString converts a Factorio map exchange string into the two
// JSON settings tables expected by Factorio's --map-gen-settings and
// --map-settings command line flags.
//
// The binary field order follows Factorio's documented exchange string format
// and the MIT-licensed converter at:
// https://github.com/rfvgyhn/factorio-exchange-string-parser
func ParseMapExchangeString(input string) (*MapExchangeData, error) {
	trimmed := strings.TrimSpace(input)
	if strings.HasPrefix(trimmed, "{") {
		return parseMapExchangeJSON(trimmed)
	}

	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, trimmed)

	if !strings.HasPrefix(compact, ">>>") || !strings.HasSuffix(compact, "<<<") {
		return nil, fmt.Errorf("invalid map exchange string: expected >>>...<<<")
	}

	encoded := strings.TrimSuffix(strings.TrimPrefix(compact, ">>>"), "<<<")
	decoded, err := decodeMapExchangeBase64(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid map exchange base64: %w", err)
	}

	zr, err := zlib.NewReader(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("invalid or unsupported map exchange compression: %w", err)
	}
	raw, err := io.ReadAll(zr)
	closeErr := zr.Close()
	if err != nil {
		return nil, fmt.Errorf("unable to decompress map exchange string: %w", err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("unable to close map exchange decoder: %w", closeErr)
	}
	if len(raw) < 4 {
		return nil, fmt.Errorf("map exchange data is too short")
	}

	p := newMapExchangeParser(raw)
	version := p.readVersion()
	atLeastV2 := versionAtLeast(version, 2, 0, 0, 0)

	data := &MapExchangeData{
		Version:        version,
		MapGenSettings: nil,
		MapSettings:    nil,
	}
	_ = p.readUint8() // Unknown exchange-format byte.
	data.MapGenSettings = p.readMapGenSettings(atLeastV2)
	data.MapSettings = p.readMapSettings(atLeastV2)
	data.Checksum = p.readUint32()

	if p.err != nil {
		return nil, p.err
	}
	if p.pos != len(raw) {
		return nil, fmt.Errorf("unexpected data after map exchange payload: %d bytes", len(raw)-p.pos)
	}

	crcIndex := len(raw) - 4
	actual := binary.LittleEndian.Uint32(raw[crcIndex:])
	expected := crc32.ChecksumIEEE(raw[:crcIndex])
	data.ChecksumOK = actual == expected
	if !data.ChecksumOK {
	}

	return data, nil
}

func parseMapExchangeJSON(input string) (*MapExchangeData, error) {
	var parsed struct {
		MapSettings       map[string]interface{} `json:"map_settings"`
		MapGenSettings    map[string]interface{} `json:"map_gen_settings"`
		MapSettingsCamel  map[string]interface{} `json:"mapSettings"`
		MapGenSettingsCam map[string]interface{} `json:"mapGenSettings"`
	}

	dec := json.NewDecoder(strings.NewReader(input))
	dec.UseNumber()
	if err := dec.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("invalid map exchange JSON: %w", err)
	}

	if parsed.MapSettings == nil {
		parsed.MapSettings = parsed.MapSettingsCamel
	}
	if parsed.MapGenSettings == nil {
		parsed.MapGenSettings = parsed.MapGenSettingsCam
	}
	if parsed.MapSettings == nil || parsed.MapGenSettings == nil {
		return nil, fmt.Errorf("map exchange JSON must contain map_settings and map_gen_settings")
	}

	return &MapExchangeData{
		MapSettings:    parsed.MapSettings,
		MapGenSettings: parsed.MapGenSettings,
		ChecksumOK:     true,
	}, nil
}

func decodeMapExchangeBase64(encoded string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err == nil {
		return decoded, nil
	}

	padded := encoded
	switch len(padded) % 4 {
	case 2:
		padded += "=="
	case 3:
		padded += "="
	}
	if padded != encoded {
		if decoded, padErr := base64.StdEncoding.DecodeString(padded); padErr == nil {
			return decoded, nil
		}
	}

	decoded, rawErr := base64.RawStdEncoding.DecodeString(encoded)
	if rawErr == nil {
		return decoded, nil
	}
	return nil, err
}

type mapExchangeParser struct {
	data         []byte
	pos          int
	err          error
	lastPosition mapExchangePosition
}

type mapExchangePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func newMapExchangeParser(data []byte) *mapExchangeParser {
	return &mapExchangeParser{data: data}
}

func (p *mapExchangeParser) setErr(format string, args ...interface{}) {
	if p.err == nil {
		p.err = fmt.Errorf(format, args...)
	}
}

func (p *mapExchangeParser) readBytes(n int) []byte {
	if p.err != nil {
		return nil
	}
	if n < 0 || p.pos+n > len(p.data) {
		p.setErr("map exchange data ended unexpectedly at byte %d", p.pos)
		return nil
	}
	out := p.data[p.pos : p.pos+n]
	p.pos += n
	return out
}

func (p *mapExchangeParser) readBool() bool {
	return p.readUint8() != 0
}

func (p *mapExchangeParser) readUint8() uint8 {
	b := p.readBytes(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (p *mapExchangeParser) readInt16() int16 {
	b := p.readBytes(2)
	if b == nil {
		return 0
	}
	return int16(binary.LittleEndian.Uint16(b))
}

func (p *mapExchangeParser) readUint16() uint16 {
	b := p.readBytes(2)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint16(b)
}

func (p *mapExchangeParser) readInt32() int32 {
	b := p.readBytes(4)
	if b == nil {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b))
}

func (p *mapExchangeParser) readUint32() uint32 {
	b := p.readBytes(4)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint32(b)
}

func (p *mapExchangeParser) readUint32SO() uint32 {
	value := p.readUint8()
	if value == 0xff {
		return p.readUint32()
	}
	return uint32(value)
}

func (p *mapExchangeParser) readFloat() float64 {
	return float64(math.Float32frombits(p.readUint32()))
}

func (p *mapExchangeParser) readDouble() float64 {
	b := p.readBytes(8)
	if b == nil {
		return 0
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b))
}

func (p *mapExchangeParser) readString() string {
	size := p.readUint32SO()
	if size > uint32(len(p.data)-p.pos) {
		p.setErr("map exchange string length %d exceeds remaining data", size)
		return ""
	}
	b := p.readBytes(int(size))
	if b == nil {
		return ""
	}
	return string(b)
}

func (p *mapExchangeParser) readVersion() [4]uint16 {
	return [4]uint16{p.readUint16(), p.readUint16(), p.readUint16(), p.readUint16()}
}

func versionAtLeast(version [4]uint16, major, minor, patch, dev uint16) bool {
	target := [4]uint16{major, minor, patch, dev}
	for i := 0; i < len(version); i++ {
		if version[i] > target[i] {
			return true
		}
		if version[i] < target[i] {
			return false
		}
	}
	return true
}

func (p *mapExchangeParser) readOptional(readValue func() interface{}) interface{} {
	if !p.readBool() {
		return nil
	}
	return readValue()
}

func (p *mapExchangeParser) readArray(readItem func() interface{}) []interface{} {
	size := p.readUint32SO()
	if size > 100000 {
		p.setErr("map exchange array length %d is too large", size)
		return nil
	}
	out := make([]interface{}, 0, int(size))
	for i := uint32(0); i < size && p.err == nil; i++ {
		out = append(out, readItem())
	}
	return out
}

func (p *mapExchangeParser) readStringArray() []interface{} {
	return p.readArray(func() interface{} {
		return p.readString()
	})
}

func (p *mapExchangeParser) readStringDict(readValue func() interface{}) map[string]interface{} {
	size := p.readUint32SO()
	if size > 100000 {
		p.setErr("map exchange dictionary length %d is too large", size)
		return nil
	}
	out := make(map[string]interface{}, int(size))
	for i := uint32(0); i < size && p.err == nil; i++ {
		key := p.readString()
		out[key] = readValue()
	}
	return out
}

func (p *mapExchangeParser) readFrequencySizeRichness() interface{} {
	return map[string]interface{}{
		"frequency": p.readFloat(),
		"size":      p.readFloat(),
		"richness":  p.readFloat(),
	}
}

func (p *mapExchangeParser) readAutoplaceSetting() interface{} {
	return map[string]interface{}{
		"treat_missing_as_default": p.readBool(),
		"settings": p.readStringDict(func() interface{} {
			return p.readFrequencySizeRichness()
		}),
	}
}

func (p *mapExchangeParser) readMapPosition() interface{} {
	var x, y float64
	xDiff := float64(p.readInt16()) / 256
	if xDiff == float64(0x7fff)/256 {
		x = float64(p.readInt32()) / 256
		y = float64(p.readInt32()) / 256
	} else {
		yDiff := float64(p.readInt16()) / 256
		x = p.lastPosition.X + xDiff
		y = p.lastPosition.Y + yDiff
	}

	p.lastPosition.X = x
	p.lastPosition.Y = y

	return map[string]interface{}{
		"x": x,
		"y": y,
	}
}

func (p *mapExchangeParser) readBoundingBox() interface{} {
	return map[string]interface{}{
		"left_top":     p.readMapPosition(),
		"right_bottom": p.readMapPosition(),
		"orientation": map[string]interface{}{
			"x": p.readInt16(),
			"y": p.readInt16(),
		},
	}
}

func (p *mapExchangeParser) readCliffSettings(atLeastV2 bool) interface{} {
	settings := map[string]interface{}{
		"name": p.readString(),
	}
	if atLeastV2 {
		_ = p.readUint8() // New 2.x field not represented in JSON settings.
	}
	settings["cliff_elevation_0"] = p.readFloat()
	settings["cliff_elevation_interval"] = p.readFloat()
	settings["richness"] = p.readFloat()
	if atLeastV2 {
		settings["cliff_smoothing"] = p.readFloat()
	}
	return settings
}

func (p *mapExchangeParser) readTerritorySettings() interface{} {
	return map[string]interface{}{
		"units":                          p.readStringArray(),
		"territory_index_expression":     p.readString(),
		"territory_variation_expression": p.readString(),
		"minimum_territory_size":         p.readUint32(),
	}
}

func (p *mapExchangeParser) readMapGenSettings(atLeastV2 bool) map[string]interface{} {
	terrainSegmentation := float64(0)
	water := float64(0)
	if !atLeastV2 {
		terrainSegmentation = p.readFloat()
		water = p.readFloat()
	}

	settings := map[string]interface{}{
		"autoplace_controls": p.readStringDict(func() interface{} {
			return p.readFrequencySizeRichness()
		}),
		"autoplace_settings": p.readStringDict(func() interface{} {
			return p.readAutoplaceSetting()
		}),
		"default_enable_all_autoplace_controls": p.readBool(),
		"seed":                                  p.readUint32(),
		"width":                                 p.readUint32(),
		"height":                                p.readUint32(),
		"area_to_generate_at_start":             p.readBoundingBox(),
		"starting_area":                         p.readFloat(),
		"peaceful_mode":                         p.readBool(),
		"starting_points":                       nil,
		"property_expression_names":             nil,
		"cliff_settings":                        nil,
	}
	if atLeastV2 {
		settings["no_enemies_mode"] = p.readBool()
	}
	settings["starting_points"] = p.readArray(func() interface{} {
		return p.readMapPosition()
	})
	settings["property_expression_names"] = p.readStringDict(func() interface{} {
		return p.readString()
	})
	settings["cliff_settings"] = p.readCliffSettings(atLeastV2)
	if atLeastV2 {
		territorySettings := p.readOptional(func() interface{} {
			return p.readTerritorySettings()
		})
		if territorySettings != nil {
			settings["territory_settings"] = territorySettings
		}
	} else {
		settings["terrain_segmentation"] = terrainSegmentation
		settings["water"] = water
	}
	return settings
}

func (p *mapExchangeParser) readPollution() interface{} {
	return map[string]interface{}{
		"enabled":                                     p.readOptional(func() interface{} { return p.readBool() }),
		"diffusion_ratio":                             p.readOptional(func() interface{} { return p.readDouble() }),
		"min_to_diffuse":                              p.readOptional(func() interface{} { return p.readDouble() }),
		"ageing":                                      p.readOptional(func() interface{} { return p.readDouble() }),
		"expected_max_per_chunk":                      p.readOptional(func() interface{} { return p.readDouble() }),
		"min_to_show_per_chunk":                       p.readOptional(func() interface{} { return p.readDouble() }),
		"min_pollution_to_damage_trees":               p.readOptional(func() interface{} { return p.readDouble() }),
		"pollution_with_max_forest_damage":            p.readOptional(func() interface{} { return p.readDouble() }),
		"pollution_per_tree_damage":                   p.readOptional(func() interface{} { return p.readDouble() }),
		"pollution_restored_per_tree_damage":          p.readOptional(func() interface{} { return p.readDouble() }),
		"max_pollution_to_restore_trees":              p.readOptional(func() interface{} { return p.readDouble() }),
		"enemy_attack_pollution_consumption_modifier": p.readOptional(func() interface{} { return p.readDouble() }),
	}
}

func (p *mapExchangeParser) readRealSteering() interface{} {
	return map[string]interface{}{
		"radius":                         p.readOptional(func() interface{} { return p.readDouble() }),
		"separation_factor":              p.readOptional(func() interface{} { return p.readDouble() }),
		"separation_force":               p.readOptional(func() interface{} { return p.readDouble() }),
		"force_unit_fuzzy_goto_behavior": p.readOptional(func() interface{} { return p.readBool() }),
	}
}

func (p *mapExchangeParser) readSteering() interface{} {
	return map[string]interface{}{
		"default": p.readRealSteering(),
		"moving":  p.readRealSteering(),
	}
}

func (p *mapExchangeParser) readEnemyEvolution() interface{} {
	return map[string]interface{}{
		"enabled":          p.readOptional(func() interface{} { return p.readBool() }),
		"time_factor":      p.readOptional(func() interface{} { return p.readDouble() }),
		"destroy_factor":   p.readOptional(func() interface{} { return p.readDouble() }),
		"pollution_factor": p.readOptional(func() interface{} { return p.readDouble() }),
	}
}

func (p *mapExchangeParser) readEnemyExpansion() interface{} {
	return map[string]interface{}{
		"enabled":                             p.readOptional(func() interface{} { return p.readBool() }),
		"max_expansion_distance":              p.readOptional(func() interface{} { return p.readUint32() }),
		"friendly_base_influence_radius":      p.readOptional(func() interface{} { return p.readUint32() }),
		"enemy_building_influence_radius":     p.readOptional(func() interface{} { return p.readUint32() }),
		"building_coefficient":                p.readOptional(func() interface{} { return p.readDouble() }),
		"other_base_coefficient":              p.readOptional(func() interface{} { return p.readDouble() }),
		"neighbouring_chunk_coefficient":      p.readOptional(func() interface{} { return p.readDouble() }),
		"neighbouring_base_chunk_coefficient": p.readOptional(func() interface{} { return p.readDouble() }),
		"max_colliding_tiles_coefficient":     p.readOptional(func() interface{} { return p.readDouble() }),
		"settler_group_min_size":              p.readOptional(func() interface{} { return p.readUint32() }),
		"settler_group_max_size":              p.readOptional(func() interface{} { return p.readUint32() }),
		"min_expansion_cooldown":              p.readOptional(func() interface{} { return p.readUint32() }),
		"max_expansion_cooldown":              p.readOptional(func() interface{} { return p.readUint32() }),
	}
}

func (p *mapExchangeParser) readUnitGroup() interface{} {
	return map[string]interface{}{
		"min_group_gathering_time":           p.readOptional(func() interface{} { return p.readUint32() }),
		"max_group_gathering_time":           p.readOptional(func() interface{} { return p.readUint32() }),
		"max_wait_time_for_late_members":     p.readOptional(func() interface{} { return p.readUint32() }),
		"max_group_radius":                   p.readOptional(func() interface{} { return p.readDouble() }),
		"min_group_radius":                   p.readOptional(func() interface{} { return p.readDouble() }),
		"max_member_speedup_when_behind":     p.readOptional(func() interface{} { return p.readDouble() }),
		"max_member_slowdown_when_ahead":     p.readOptional(func() interface{} { return p.readDouble() }),
		"max_group_slowdown_factor":          p.readOptional(func() interface{} { return p.readDouble() }),
		"max_group_member_fallback_factor":   p.readOptional(func() interface{} { return p.readDouble() }),
		"member_disown_distance":             p.readOptional(func() interface{} { return p.readDouble() }),
		"tick_tolerance_when_member_arrives": p.readOptional(func() interface{} { return p.readUint32() }),
		"max_gathering_unit_groups":          p.readOptional(func() interface{} { return p.readUint32() }),
		"max_unit_group_size":                p.readOptional(func() interface{} { return p.readUint32() }),
	}
}

func (p *mapExchangeParser) readPathFinder() interface{} {
	return map[string]interface{}{
		"fwd2bwd_ratio":                                        p.readOptional(func() interface{} { return p.readInt32() }),
		"goal_pressure_ratio":                                  p.readOptional(func() interface{} { return p.readDouble() }),
		"use_path_cache":                                       p.readOptional(func() interface{} { return p.readBool() }),
		"max_steps_worked_per_tick":                            p.readOptional(func() interface{} { return p.readDouble() }),
		"max_work_done_per_tick":                               p.readOptional(func() interface{} { return p.readUint32() }),
		"short_cache_size":                                     p.readOptional(func() interface{} { return p.readUint32() }),
		"long_cache_size":                                      p.readOptional(func() interface{} { return p.readUint32() }),
		"short_cache_min_cacheable_distance":                   p.readOptional(func() interface{} { return p.readDouble() }),
		"short_cache_min_algo_steps_to_cache":                  p.readOptional(func() interface{} { return p.readUint32() }),
		"long_cache_min_cacheable_distance":                    p.readOptional(func() interface{} { return p.readDouble() }),
		"cache_max_connect_to_cache_steps_multiplier":          p.readOptional(func() interface{} { return p.readUint32() }),
		"cache_accept_path_start_distance_ratio":               p.readOptional(func() interface{} { return p.readDouble() }),
		"cache_accept_path_end_distance_ratio":                 p.readOptional(func() interface{} { return p.readDouble() }),
		"negative_cache_accept_path_start_distance_ratio":      p.readOptional(func() interface{} { return p.readDouble() }),
		"negative_cache_accept_path_end_distance_ratio":        p.readOptional(func() interface{} { return p.readDouble() }),
		"cache_path_start_distance_rating_multiplier":          p.readOptional(func() interface{} { return p.readDouble() }),
		"cache_path_end_distance_rating_multiplier":            p.readOptional(func() interface{} { return p.readDouble() }),
		"stale_enemy_with_same_destination_collision_penalty":  p.readOptional(func() interface{} { return p.readDouble() }),
		"ignore_moving_enemy_collision_distance":               p.readOptional(func() interface{} { return p.readDouble() }),
		"enemy_with_different_destination_collision_penalty":   p.readOptional(func() interface{} { return p.readDouble() }),
		"general_entity_collision_penalty":                     p.readOptional(func() interface{} { return p.readDouble() }),
		"general_entity_subsequent_collision_penalty":          p.readOptional(func() interface{} { return p.readDouble() }),
		"extended_collision_penalty":                           p.readOptional(func() interface{} { return p.readDouble() }),
		"max_clients_to_accept_any_new_request":                p.readOptional(func() interface{} { return p.readUint32() }),
		"max_clients_to_accept_short_new_request":              p.readOptional(func() interface{} { return p.readUint32() }),
		"direct_distance_to_consider_short_request":            p.readOptional(func() interface{} { return p.readUint32() }),
		"short_request_max_steps":                              p.readOptional(func() interface{} { return p.readUint32() }),
		"short_request_ratio":                                  p.readOptional(func() interface{} { return p.readDouble() }),
		"min_steps_to_check_path_find_termination":             p.readOptional(func() interface{} { return p.readUint32() }),
		"start_to_goal_cost_multiplier_to_terminate_path_find": p.readOptional(func() interface{} { return p.readDouble() }),
		"overload_levels": p.readOptional(func() interface{} {
			return p.readArray(func() interface{} { return p.readUint32() })
		}),
		"overload_multipliers": p.readOptional(func() interface{} {
			return p.readArray(func() interface{} { return p.readDouble() })
		}),
		"negative_path_cache_delay_interval": p.readOptional(func() interface{} { return p.readUint32() }),
	}
}

func (p *mapExchangeParser) readDifficultySettings(atLeastV2 bool) interface{} {
	if atLeastV2 {
		return map[string]interface{}{
			"technology_price_multiplier": p.readDouble(),
			"spoil_time_modifier":         p.readDouble(),
		}
	}

	recipeDifficulty := p.readUint8()
	technologyDifficulty := p.readUint8()
	technologyPriceMultiplier := p.readDouble()
	researchQueue := []string{"always", "after-victory", "never"}
	queueIndex := int(p.readUint8())
	queueSetting := ""
	if queueIndex >= 0 && queueIndex < len(researchQueue) {
		queueSetting = researchQueue[queueIndex]
	} else {
		p.setErr("invalid research queue setting %d", queueIndex)
	}

	return map[string]interface{}{
		"recipe_difficulty":           recipeDifficulty,
		"technology_difficulty":       technologyDifficulty,
		"technology_price_multiplier": technologyPriceMultiplier,
		"research_queue_setting":      queueSetting,
	}
}

func (p *mapExchangeParser) readAsteroidsSettings() interface{} {
	return map[string]interface{}{
		"spawning_rate":                     p.readOptional(func() interface{} { return p.readDouble() }),
		"max_ray_portals_expanded_per_tick": p.readOptional(func() interface{} { return p.readUint32() }),
	}
}

func (p *mapExchangeParser) readMapSettings(atLeastV2 bool) map[string]interface{} {
	settings := map[string]interface{}{
		"pollution":                 p.readPollution(),
		"steering":                  p.readSteering(),
		"enemy_evolution":           p.readEnemyEvolution(),
		"enemy_expansion":           p.readEnemyExpansion(),
		"unit_group":                p.readUnitGroup(),
		"path_finder":               p.readPathFinder(),
		"max_failed_behavior_count": p.readUint32(),
		"difficulty_settings":       p.readDifficultySettings(atLeastV2),
	}
	if atLeastV2 {
		settings["asteroids"] = p.readAsteroidsSettings()
	}
	return settings
}

func EncodeMapExchangeString(mapGenRaw, mapSettingsRaw json.RawMessage) (string, error) {
	var mapGen map[string]interface{}
	if err := decodeObject(mapGenRaw, &mapGen); err != nil {
		return "", fmt.Errorf("%s is invalid JSON: %w", mapGenFile, err)
	}
	var mapSettings map[string]interface{}
	if err := decodeObject(mapSettingsRaw, &mapSettings); err != nil {
		return "", fmt.Errorf("%s is invalid JSON: %w", mapSettingsFile, err)
	}

	w := &mapExchangeWriter{}
	w.writeVersion([4]uint16{2, 0, 0, 0})
	w.writeUint8(0)
	w.writeMapGenSettings(mapGen)
	w.writeMapSettings(mapSettings)
	checksum := crc32.ChecksumIEEE(w.buf.Bytes())
	w.writeUint32(checksum)

	var compressed bytes.Buffer
	zw := zlib.NewWriter(&compressed)
	if _, err := zw.Write(w.buf.Bytes()); err != nil {
		_ = zw.Close()
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	return ">>>" + base64.StdEncoding.EncodeToString(compressed.Bytes()) + "<<<", nil
}

type mapExchangeWriter struct {
	buf bytes.Buffer
}

func (w *mapExchangeWriter) writeUint8(v uint8) { w.buf.WriteByte(v) }

func (w *mapExchangeWriter) writeBool(v bool) {
	if v {
		w.writeUint8(1)
	} else {
		w.writeUint8(0)
	}
}

func (w *mapExchangeWriter) writeInt16(v int16)   { _ = binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *mapExchangeWriter) writeUint16(v uint16) { _ = binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *mapExchangeWriter) writeInt32(v int32)   { _ = binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *mapExchangeWriter) writeUint32(v uint32) { _ = binary.Write(&w.buf, binary.LittleEndian, v) }
func (w *mapExchangeWriter) writeDouble(v float64) {
	_ = binary.Write(&w.buf, binary.LittleEndian, math.Float64bits(v))
}
func (w *mapExchangeWriter) writeFloat(v float64) { w.writeUint32(math.Float32bits(float32(v))) }

func (w *mapExchangeWriter) writeUint32SO(v uint32) {
	if v < 0xff {
		w.writeUint8(uint8(v))
		return
	}
	w.writeUint8(0xff)
	w.writeUint32(v)
}

func (w *mapExchangeWriter) writeString(v string) {
	w.writeUint32SO(uint32(len(v)))
	w.buf.WriteString(v)
}

func (w *mapExchangeWriter) writeVersion(v [4]uint16) {
	for _, part := range v {
		w.writeUint16(part)
	}
}

func (w *mapExchangeWriter) writeOptional(value interface{}, writeValue func(interface{})) {
	if value == nil {
		w.writeBool(false)
		return
	}
	w.writeBool(true)
	writeValue(value)
}

func (w *mapExchangeWriter) writeArray(items []interface{}, writeItem func(interface{})) {
	w.writeUint32SO(uint32(len(items)))
	for _, item := range items {
		writeItem(item)
	}
}

func (w *mapExchangeWriter) writeStringArray(items []interface{}) {
	w.writeArray(items, func(item interface{}) { w.writeString(asString(item, "")) })
}

func (w *mapExchangeWriter) writeStringDict(m map[string]interface{}, writeValue func(interface{})) {
	keys := sortedDataKeys(m)
	w.writeUint32SO(uint32(len(keys)))
	for _, key := range keys {
		w.writeString(key)
		writeValue(m[key])
	}
}

func (w *mapExchangeWriter) writeFrequencySizeRichness(v interface{}) {
	m := asMap(v)
	w.writeFloat(asFloat(m["frequency"], 1))
	w.writeFloat(asFloat(m["size"], 1))
	w.writeFloat(asFloat(m["richness"], 0))
}

func (w *mapExchangeWriter) writeAutoplaceSetting(v interface{}) {
	m := asMap(v)
	w.writeBool(asBool(m["treat_missing_as_default"], false))
	w.writeStringDict(asMap(m["settings"]), w.writeFrequencySizeRichness)
}

func (w *mapExchangeWriter) writeMapPosition(v interface{}) {
	m := asMap(v)
	x := int32(math.Round(asFloat(m["x"], 0) * 256))
	y := int32(math.Round(asFloat(m["y"], 0) * 256))
	w.writeInt16(0x7fff)
	w.writeInt32(x)
	w.writeInt32(y)
}

func (w *mapExchangeWriter) writeBoundingBox(v interface{}) {
	m := asMap(v)
	w.writeMapPosition(m["left_top"])
	w.writeMapPosition(m["right_bottom"])
	orientation := asMap(m["orientation"])
	w.writeInt16(int16(asInt(orientation["x"], 0)))
	w.writeInt16(int16(asInt(orientation["y"], 0)))
}

func (w *mapExchangeWriter) writeCliffSettings(v interface{}) {
	m := asMap(v)
	w.writeString(asString(m["name"], "cliff"))
	w.writeUint8(0)
	w.writeFloat(asFloat(m["cliff_elevation_0"], 10))
	w.writeFloat(asFloat(m["cliff_elevation_interval"], 40))
	w.writeFloat(asFloat(m["richness"], 1))
	w.writeFloat(asFloat(m["cliff_smoothing"], 0))
}

func (w *mapExchangeWriter) writeTerritorySettings(v interface{}) {
	m := asMap(v)
	w.writeStringArray(asArray(m["units"]))
	w.writeString(asString(m["territory_index_expression"], ""))
	w.writeString(asString(m["territory_variation_expression"], ""))
	w.writeUint32(uint32(asInt(m["minimum_territory_size"], 0)))
}

func (w *mapExchangeWriter) writeMapGenSettings(settings map[string]interface{}) {
	w.writeStringDict(asMap(settings["autoplace_controls"]), w.writeFrequencySizeRichness)
	w.writeStringDict(asMap(settings["autoplace_settings"]), w.writeAutoplaceSetting)
	w.writeBool(asBool(settings["default_enable_all_autoplace_controls"], false))
	w.writeUint32(uint32(asInt(settings["seed"], 0)))
	w.writeUint32(uint32(asInt(settings["width"], 0)))
	w.writeUint32(uint32(asInt(settings["height"], 0)))
	w.writeBoundingBox(settings["area_to_generate_at_start"])
	w.writeFloat(asFloat(settings["starting_area"], 1))
	w.writeBool(asBool(settings["peaceful_mode"], false))
	w.writeBool(asBool(settings["no_enemies_mode"], false))
	w.writeArray(asArray(settings["starting_points"]), w.writeMapPosition)
	w.writeStringDict(asMap(settings["property_expression_names"]), func(v interface{}) { w.writeString(asString(v, "")) })
	w.writeCliffSettings(settings["cliff_settings"])
	w.writeOptional(settings["territory_settings"], w.writeTerritorySettings)
}

func (w *mapExchangeWriter) writePollution(v interface{}) {
	m := asMap(v)
	for _, key := range []string{"enabled", "diffusion_ratio", "min_to_diffuse", "ageing", "expected_max_per_chunk", "min_to_show_per_chunk", "min_pollution_to_damage_trees", "pollution_with_max_forest_damage", "pollution_per_tree_damage", "pollution_restored_per_tree_damage", "max_pollution_to_restore_trees", "enemy_attack_pollution_consumption_modifier"} {
		if key == "enabled" {
			w.writeOptional(m[key], func(v interface{}) { w.writeBool(asBool(v, false)) })
		} else {
			w.writeOptional(m[key], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
		}
	}
}

func (w *mapExchangeWriter) writeRealSteering(v interface{}) {
	m := asMap(v)
	w.writeOptional(m["radius"], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
	w.writeOptional(m["separation_factor"], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
	w.writeOptional(m["separation_force"], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
	w.writeOptional(m["force_unit_fuzzy_goto_behavior"], func(v interface{}) { w.writeBool(asBool(v, false)) })
}

func (w *mapExchangeWriter) writeSteering(v interface{}) {
	m := asMap(v)
	w.writeRealSteering(m["default"])
	w.writeRealSteering(m["moving"])
}

func (w *mapExchangeWriter) writeEnemyEvolution(v interface{}) {
	m := asMap(v)
	w.writeOptional(m["enabled"], func(v interface{}) { w.writeBool(asBool(v, false)) })
	w.writeOptional(m["time_factor"], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
	w.writeOptional(m["destroy_factor"], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
	w.writeOptional(m["pollution_factor"], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
}

func (w *mapExchangeWriter) writeEnemyExpansion(v interface{}) {
	m := asMap(v)
	for _, key := range []string{"enabled", "max_expansion_distance", "friendly_base_influence_radius", "enemy_building_influence_radius", "building_coefficient", "other_base_coefficient", "neighbouring_chunk_coefficient", "neighbouring_base_chunk_coefficient", "max_colliding_tiles_coefficient", "settler_group_min_size", "settler_group_max_size", "min_expansion_cooldown", "max_expansion_cooldown"} {
		switch key {
		case "enabled":
			w.writeOptional(m[key], func(v interface{}) { w.writeBool(asBool(v, false)) })
		case "max_expansion_distance", "friendly_base_influence_radius", "enemy_building_influence_radius", "settler_group_min_size", "settler_group_max_size", "min_expansion_cooldown", "max_expansion_cooldown":
			w.writeOptional(m[key], func(v interface{}) { w.writeUint32(uint32(asInt(v, 0))) })
		default:
			w.writeOptional(m[key], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
		}
	}
}

func (w *mapExchangeWriter) writeUnitGroup(v interface{}) {
	m := asMap(v)
	for _, key := range []string{"min_group_gathering_time", "max_group_gathering_time", "max_wait_time_for_late_members", "max_group_radius", "min_group_radius", "max_member_speedup_when_behind", "max_member_slowdown_when_ahead", "max_group_slowdown_factor", "max_group_member_fallback_factor", "member_disown_distance", "tick_tolerance_when_member_arrives", "max_gathering_unit_groups", "max_unit_group_size"} {
		switch key {
		case "min_group_gathering_time", "max_group_gathering_time", "max_wait_time_for_late_members", "tick_tolerance_when_member_arrives", "max_gathering_unit_groups", "max_unit_group_size":
			w.writeOptional(m[key], func(v interface{}) { w.writeUint32(uint32(asInt(v, 0))) })
		default:
			w.writeOptional(m[key], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
		}
	}
}

func (w *mapExchangeWriter) writePathFinder(v interface{}) {
	m := asMap(v)
	for _, key := range []string{"fwd2bwd_ratio", "goal_pressure_ratio", "use_path_cache", "max_steps_worked_per_tick", "max_work_done_per_tick", "short_cache_size", "long_cache_size", "short_cache_min_cacheable_distance", "short_cache_min_algo_steps_to_cache", "long_cache_min_cacheable_distance", "cache_max_connect_to_cache_steps_multiplier", "cache_accept_path_start_distance_ratio", "cache_accept_path_end_distance_ratio", "negative_cache_accept_path_start_distance_ratio", "negative_cache_accept_path_end_distance_ratio", "cache_path_start_distance_rating_multiplier", "cache_path_end_distance_rating_multiplier", "stale_enemy_with_same_destination_collision_penalty", "ignore_moving_enemy_collision_distance", "enemy_with_different_destination_collision_penalty", "general_entity_collision_penalty", "general_entity_subsequent_collision_penalty", "extended_collision_penalty", "max_clients_to_accept_any_new_request", "max_clients_to_accept_short_new_request", "direct_distance_to_consider_short_request", "short_request_max_steps", "short_request_ratio", "min_steps_to_check_path_find_termination", "start_to_goal_cost_multiplier_to_terminate_path_find", "overload_levels", "overload_multipliers", "negative_path_cache_delay_interval"} {
		switch key {
		case "fwd2bwd_ratio":
			w.writeOptional(m[key], func(v interface{}) { w.writeInt32(int32(asInt(v, 0))) })
		case "use_path_cache":
			w.writeOptional(m[key], func(v interface{}) { w.writeBool(asBool(v, false)) })
		case "max_work_done_per_tick", "short_cache_size", "long_cache_size", "short_cache_min_algo_steps_to_cache", "cache_max_connect_to_cache_steps_multiplier", "max_clients_to_accept_any_new_request", "max_clients_to_accept_short_new_request", "direct_distance_to_consider_short_request", "short_request_max_steps", "min_steps_to_check_path_find_termination", "negative_path_cache_delay_interval":
			w.writeOptional(m[key], func(v interface{}) { w.writeUint32(uint32(asInt(v, 0))) })
		case "overload_levels":
			w.writeOptional(m[key], func(v interface{}) {
				w.writeArray(asArray(v), func(item interface{}) { w.writeUint32(uint32(asInt(item, 0))) })
			})
		case "overload_multipliers":
			w.writeOptional(m[key], func(v interface{}) {
				w.writeArray(asArray(v), func(item interface{}) { w.writeDouble(asFloat(item, 0)) })
			})
		default:
			w.writeOptional(m[key], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
		}
	}
}

func (w *mapExchangeWriter) writeDifficultySettings(v interface{}) {
	m := asMap(v)
	w.writeDouble(asFloat(m["technology_price_multiplier"], 1))
	w.writeDouble(asFloat(m["spoil_time_modifier"], 1))
}

func (w *mapExchangeWriter) writeAsteroidsSettings(v interface{}) {
	m := asMap(v)
	w.writeOptional(m["spawning_rate"], func(v interface{}) { w.writeDouble(asFloat(v, 0)) })
	w.writeOptional(m["max_ray_portals_expanded_per_tick"], func(v interface{}) { w.writeUint32(uint32(asInt(v, 0))) })
}

func (w *mapExchangeWriter) writeMapSettings(settings map[string]interface{}) {
	w.writePollution(settings["pollution"])
	w.writeSteering(settings["steering"])
	w.writeEnemyEvolution(settings["enemy_evolution"])
	w.writeEnemyExpansion(settings["enemy_expansion"])
	w.writeUnitGroup(settings["unit_group"])
	w.writePathFinder(settings["path_finder"])
	w.writeUint32(uint32(asInt(settings["max_failed_behavior_count"], 0)))
	w.writeDifficultySettings(settings["difficulty_settings"])
	w.writeAsteroidsSettings(settings["asteroids"])
}

func sortedDataKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		if strings.HasPrefix(key, "_") {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func asMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func asArray(v interface{}) []interface{} {
	if a, ok := v.([]interface{}); ok {
		return a
	}
	return nil
}

func asString(v interface{}, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

func asBool(v interface{}, fallback bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}

func asInt(v interface{}, fallback int64) int64 {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i
		}
		f, err := n.Float64()
		if err == nil {
			return int64(f)
		}
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return fallback
}

func asFloat(v interface{}, fallback float64) float64 {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		if err == nil {
			return f
		}
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return fallback
}
