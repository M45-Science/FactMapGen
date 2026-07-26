package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

type factorioResourceOracle struct {
	Positions []naturalOraclePoint         `json:"positions"`
	Cases     []factorioResourceOracleCase `json:"cases"`
}

type factorioResourceOracleCase struct {
	Resource string    `json:"resource"`
	Seed     uint32    `json:"seed"`
	Values   []float64 `json:"values"`
}

type factorioSpotCandidateOracle struct {
	Cases []struct {
		Seed0      uint32      `json:"seed0"`
		Seed1      uint32      `json:"seed1"`
		RegionX    int64       `json:"regionX"`
		RegionY    int64       `json:"regionY"`
		RegionSize int64       `json:"regionSize"`
		Candidates [][]float64 `json:"candidates"`
	} `json:"cases"`
}

type factorioRawSpotStreamOracle struct {
	Draws [][]uint64 `json:"candidate_index_to_Vx_Vy"`
}

type factorioSpotExpressionOracle struct {
	Kind   string  `json:"kind"`
	Value  float64 `json:"value"`
	Base   float64 `json:"base"`
	Scale  float64 `json:"scale"`
	Offset float64 `json:"offset"`
}

type factorioSpotSelectionOracle struct {
	Cases []struct {
		Name         string                       `json:"name"`
		Seed0        uint32                       `json:"seed0"`
		Seed1        uint32                       `json:"seed1"`
		RegionX      int64                        `json:"regionX"`
		RegionY      int64                        `json:"regionY"`
		RegionSize   int64                        `json:"regionSize"`
		Count        int                          `json:"count"`
		Spacing      float64                      `json:"spacing"`
		SkipSpan     int                          `json:"skipSpan"`
		SkipOffset   int                          `json:"skipOffset"`
		Hard         bool                         `json:"hard"`
		Density      factorioSpotExpressionOracle `json:"density"`
		Quantity     factorioSpotExpressionOracle `json:"quantity"`
		Favorability factorioSpotExpressionOracle `json:"favorability"`
		Spots        [][]float64                  `json:"spots"`
	} `json:"cases"`
}

func TestFactorioRegularResourceFieldsMatchOracle(t *testing.T) {
	fixture := readResourceOracle[factorioResourceOracle](t, "oracle-resource-regular.seed123456.json")
	control := fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	for _, oracleCase := range fixture.Cases {
		oracleCase := oracleCase
		t.Run(oracleCase.Resource+"-seed-"+formatUint32(oracleCase.Seed), func(t *testing.T) {
			params := factorioResourceParamsNamed(t, oracleCase.Resource)
			field := newFactorioRegularResourceField(params, control, oracleCase.Seed, nil, 1, 0)
			worstAbsolute := 0.0
			worstRelative := 0.0
			for index, position := range fixture.Positions {
				got := field.fieldAt(position.X, position.Y)
				want := oracleCase.Values[index]
				delta := math.Abs(got - want)
				worstAbsolute = math.Max(worstAbsolute, delta)
				worstRelative = math.Max(worstRelative, delta/math.Max(1, math.Abs(want)))
			}
			if worstAbsolute >= 1 || worstRelative >= 1e-2 {
				t.Fatalf("regular resource oracle residuals: absolute=%g relative=%g", worstAbsolute, worstRelative)
			}
		})
	}
}

func TestFactorioResourceRegionCachesAreTrimmed(t *testing.T) {
	settings := defaultFactorioTerrainSettings(123456)
	settings.resourceControls = make(map[string]fastControl, len(factorioResourceCatalog))
	for _, resource := range factorioResourceCatalog {
		settings.resourceControls[resource.name] = fastControl{
			frequency: 1,
			size:      1,
			richness:  1,
			enabled:   true,
		}
	}
	evaluator := newFactorioResourceEvaluator(settings, newFactorioNauvisEvaluator(settings))
	const limit = 64
	for fieldIndex := range evaluator.fields {
		field := &evaluator.fields[fieldIndex]
		for region := 0; region <= limit; region++ {
			key := [2]int64{int64(region), int64(fieldIndex)}
			field.regular.regionCache[key] = nil
			if field.starting != nil {
				field.starting.regionCache[key] = nil
			}
		}
	}
	if evaluator.cachedRegionCount() == 0 {
		t.Fatal("test did not populate resource region caches")
	}
	evaluator.trimRegionCaches(limit)
	if got := evaluator.cachedRegionCount(); got != 0 {
		t.Fatalf("cached resource regions after trim = %d, want 0", got)
	}
}

func TestFactorioStartingResourceFieldsMatchOracle(t *testing.T) {
	fixture := readResourceOracle[factorioResourceOracle](t, "oracle-resource-starting.seed123456.json")
	control := fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	for _, oracleCase := range fixture.Cases {
		oracleCase := oracleCase
		t.Run(oracleCase.Resource+"-seed-"+formatUint32(oracleCase.Seed), func(t *testing.T) {
			params := factorioResourceParamsNamed(t, oracleCase.Resource)
			settings := defaultFactorioTerrainSettings(oracleCase.Seed)
			nauvis := newFactorioNauvisEvaluator(settings)
			field := newFactorioResourceField(params, control, oracleCase.Seed, nil, nauvis, 1, 0, 1, 0)
			offenders := 0
			worstAbsolute := 0.0
			worstRelative := 0.0
			for index, position := range fixture.Positions {
				got := field.fieldAt(position.X, position.Y)
				want := oracleCase.Values[index]
				delta := math.Abs(got - want)
				relative := delta / math.Max(1, math.Abs(want))
				worstAbsolute = math.Max(worstAbsolute, delta)
				worstRelative = math.Max(worstRelative, relative)
				if delta >= 1 && relative >= 1e-2 {
					offenders++
				}
			}
			if offenders != 0 {
				t.Fatalf("%d points fail both tolerance gates; worst absolute=%g relative=%g", offenders, worstAbsolute, worstRelative)
			}
		})
	}
}

func TestFactorioSpotCandidatesMatchOracle(t *testing.T) {
	fixture := readResourceOracle[factorioSpotCandidateOracle](t, "spot-candidates.game.json")
	for caseIndex, oracleCase := range fixture.Cases {
		points := factorioSpotCandidatePoints(
			factorioSpotRegionKey{
				seed0: oracleCase.Seed0, seed1: oracleCase.Seed1,
				regionX: oracleCase.RegionX, regionY: oracleCase.RegionY,
			},
			oracleCase.RegionSize,
			len(oracleCase.Candidates),
		)
		sort.Slice(points, func(i, j int) bool {
			return points[i].x < points[j].x || points[i].x == points[j].x && points[i].y < points[j].y
		})
		for index, point := range points {
			want := oracleCase.Candidates[index]
			if point.x != want[0] || point.y != want[1] {
				t.Fatalf("case %d candidate %d = (%g,%g), want (%g,%g)", caseIndex, index, point.x, point.y, want[0], want[1])
			}
		}
	}
}

func TestFactorioSpotCandidateRawStreamMatchesOracle(t *testing.T) {
	fixture := readResourceOracle[factorioRawSpotStreamOracle](t, "spot-candidate-stream.seed123456.json")
	key := factorioSpotRegionKey{seed0: 123456, seed1: 0}
	if word := factorioSpotSeedWord(key); word != 0x3e5c6c {
		t.Fatalf("spot seed word = %#x, want 0x3e5c6c", word)
	}
	const regionSize = int64(1) << 32
	points := factorioSpotCandidatePoints(key, regionSize, len(fixture.Draws))
	for _, draw := range fixture.Draws {
		index := int(draw[0])
		gotX := uint64(points[index].x + float64(regionSize/2))
		gotY := uint64(points[index].y + float64(regionSize/2))
		if gotX != draw[1] || gotY != draw[2] {
			t.Fatalf("candidate %d raw draws = (%d,%d), want (%d,%d)", index, gotX, gotY, draw[1], draw[2])
		}
	}
}

func TestFactorioSpotSelectionMatchesOracle(t *testing.T) {
	fixture := readResourceOracle[factorioSpotSelectionOracle](t, "spot-selection.game.json")
	for _, oracleCase := range fixture.Cases {
		oracleCase := oracleCase
		t.Run(oracleCase.Name, func(t *testing.T) {
			selected := factorioSelectSpots(
				factorioSpotRegionKey{
					seed0: oracleCase.Seed0, seed1: oracleCase.Seed1,
					regionX: oracleCase.RegionX, regionY: oracleCase.RegionY,
				},
				factorioSpotSelectionParams{
					regionSize:               oracleCase.RegionSize,
					candidateSpotCount:       oracleCase.Count,
					spacing:                  oracleCase.Spacing,
					skipSpan:                 oracleCase.SkipSpan,
					skipOffset:               oracleCase.SkipOffset,
					hardRegionTargetQuantity: oracleCase.Hard,
					density:                  factorioDecodeSpotExpression(t, oracleCase.Density),
					quantity:                 factorioDecodeSpotExpression(t, oracleCase.Quantity),
					favorability:             factorioDecodeSpotExpression(t, oracleCase.Favorability),
				},
			)
			type result struct {
				x    float64
				y    float64
				peak float64
			}
			got := make([]result, len(selected))
			for index, spot := range selected {
				radius := 20 * spot.coneScale
				got[index] = result{
					x: spot.x, y: spot.y,
					peak: 3 * spot.quantity / (math.Pi * radius * radius),
				}
			}
			sort.Slice(got, func(i, j int) bool {
				return got[i].x < got[j].x || got[i].x == got[j].x && got[i].y < got[j].y
			})
			if len(got) != len(oracleCase.Spots) {
				t.Fatalf("selected %d spots, want %d", len(got), len(oracleCase.Spots))
			}
			for index, point := range got {
				want := oracleCase.Spots[index]
				if point.x != want[0] || point.y != want[1] {
					t.Fatalf("spot %d = (%g,%g), want (%g,%g)", index, point.x, point.y, want[0], want[1])
				}
				if math.Abs(point.peak-want[2]) >= 0.005 {
					t.Fatalf("spot %d peak = %g, want %g", index, point.peak, want[2])
				}
			}
		})
	}
}

func TestFactorioResourceCatalogMatchesBaseGame(t *testing.T) {
	wantNames := []string{"iron-ore", "copper-ore", "coal", "stone", "crude-oil", "uranium-ore"}
	if len(factorioResourceCatalog) != len(wantNames) {
		t.Fatalf("resource catalog length = %d, want %d", len(factorioResourceCatalog), len(wantNames))
	}
	for index, want := range wantNames {
		params := factorioResourceCatalog[index]
		if params.name != want || params.patchSetIndex != index {
			t.Errorf("resource %d = %q/index %d, want %q/index %d", index, params.name, params.patchSetIndex, want, index)
		}
	}
	if factorioResourceCatalog[4].randomProbability != 1.0/48.0 {
		t.Errorf("crude-oil random probability = %g, want 1/48", factorioResourceCatalog[4].randomProbability)
	}
}

func TestFactorioResourceEvaluatorHonorsControls(t *testing.T) {
	settings := defaultFactorioTerrainSettings(123456)
	settings.resourceControls = make(map[string]fastControl, len(factorioResourceCatalog))
	for _, params := range factorioResourceCatalog {
		settings.resourceControls[params.name] = fastControl{}
	}
	nauvis := newFactorioNauvisEvaluator(settings)
	if fields := newFactorioResourceEvaluator(settings, nauvis).fields; len(fields) != 0 {
		t.Fatalf("disabled resource evaluator built %d fields, want 0", len(fields))
	}

	settings.resourceControls["iron-ore"] = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	evaluator := newFactorioResourceEvaluator(settings, nauvis)
	if len(evaluator.fields) != 1 || evaluator.fields[0].params.name != "iron-ore" {
		t.Fatalf("enabled fields = %#v, want iron-ore only", evaluator.fields)
	}
	found := false
	for y := -150.0; y <= 150 && !found; y++ {
		for x := -150.0; x <= 150; x++ {
			resource, ok := evaluator.resourceAt(x, y)
			if ok {
				if resource.name != "iron-ore" || resource.mapColor != factorioResourceCatalog[0].mapColor {
					t.Fatalf("resource at (%g,%g) = %q/%#v, want iron-ore", x, y, resource.name, resource.mapColor)
				}
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("did not find the guaranteed starting iron patch")
	}
}

func TestFactorioSharedResourceNoiseMatchesReferenceFields(t *testing.T) {
	settings := defaultFactorioTerrainSettings(123456)
	settings.resourceControls = make(map[string]fastControl, len(factorioResourceCatalog))
	for _, params := range factorioResourceCatalog {
		settings.resourceControls[params.name] = fastControl{
			frequency: 1, size: 1, richness: 1, enabled: true,
		}
	}
	evaluator := newFactorioResourceEvaluator(settings, newFactorioNauvisEvaluator(settings))
	for y := -256.0; y <= 256; y += 17 {
		for x := -256.0; x <= 256; x += 17 {
			var want factorioResourceParams
			wantOK := false
			for index := range evaluator.fields {
				field := &evaluator.fields[index]
				if field.params.randomProbability < 1 {
					continue
				}
				if clampFloat(field.fieldAt(x, y), 0, 1) >= 0.5 {
					want = field.params
					wantOK = true
					break
				}
			}
			got, gotOK := evaluator.resourceAt(x, y)
			if gotOK != wantOK || (gotOK && got.name != want.name) {
				t.Fatalf(
					"resource at (%g,%g) = %q/%v, want %q/%v",
					x, y, got.name, gotOK, want.name, wantOK,
				)
			}
		}
	}
}

func TestFactorioOilPlacementIsSparseAndDeterministic(t *testing.T) {
	settings := defaultFactorioTerrainSettings(123456)
	settings.resourceControls = make(map[string]fastControl, len(factorioResourceCatalog))
	for _, params := range factorioResourceCatalog {
		settings.resourceControls[params.name] = fastControl{}
	}
	settings.resourceControls["crude-oil"] = fastControl{frequency: 1, size: 1, richness: 1, enabled: true}
	evaluator := newFactorioResourceEvaluator(settings, newFactorioNauvisEvaluator(settings))
	placed := 0
	eligible := 0
	for y := int64(512); y < 1536; y++ {
		for x := int64(512); x < 1536; x++ {
			field := clampFloat(evaluator.fields[0].fieldAt(float64(x), float64(y)), 0, 1)
			if field > 0 {
				eligible++
			}
			first, firstOK := evaluator.oilAtTile(x, y)
			second, secondOK := evaluator.oilAtTile(x, y)
			if firstOK != secondOK || first.name != second.name {
				t.Fatalf("oil placement changed at (%d,%d)", x, y)
			}
			if firstOK {
				placed++
			}
		}
	}
	if eligible == 0 || placed == 0 {
		t.Fatalf("oil dither coverage is empty: eligible=%d placed=%d", eligible, placed)
	}
	coverage := float64(placed) / float64(eligible)
	if coverage < 0.005 || coverage >= 0.15 {
		t.Fatalf("oil dither is not sparse: %d/%d eligible samples placed", placed, eligible)
	}
}

func TestFactorioOilPlacementVariesWithMapSeed(t *testing.T) {
	settings := defaultFactorioTerrainSettings(123456)
	settings.resourceControls = map[string]fastControl{
		"crude-oil": {frequency: 1, size: 1, richness: 1, enabled: true},
	}
	first := newFactorioResourceEvaluator(settings, newFactorioNauvisEvaluator(settings))
	settings.seed = 654321
	second := newFactorioResourceEvaluator(settings, newFactorioNauvisEvaluator(settings))

	different := 0
	for y := int64(-512); y < 512; y++ {
		for x := int64(-512); x < 512; x++ {
			_, firstOK := first.oilAtTile(x, y)
			_, secondOK := second.oilAtTile(x, y)
			if firstOK != secondOK {
				different++
			}
		}
	}
	if different == 0 {
		t.Fatal("oil dither placement did not vary with map seed")
	}
}

func TestFactorioTaus88RandomAccessMatchesSequential(t *testing.T) {
	words := make([]uint32, 0, 40)
	for bit := 0; bit < 32; bit++ {
		words = append(words, uint32(1)<<bit)
	}
	words = append(words, 0, factorioMinSeedWord, 0x3fbe2c, 0x01234567, 0xdeadbeef, ^uint32(0))
	for _, word := range words {
		state := newFactorioTaus88State(word)
		for draw := range factorioOilPenaltyRandomColumns {
			want := state.next()
			if got := factorioTaus88RandomAt(word, draw); got != want {
				t.Fatalf("word %#08x draw %d = %#08x, want %#08x", word, draw, got, want)
			}
		}
	}
}

func TestFactorioOilRandomPenaltyUsesChunkRowOrder(t *testing.T) {
	evaluator := &factorioResourceEvaluator{}
	chunks := [][2]int64{
		{0, 0},
		{-1, 2},
		{1, -1},
		{-15_355_650, 0}, // Raw seed word 335 exercises the minimum-word clamp.
		{-4096, -2048},
		{67_108_863, 67_108_863},
		{-67_108_864, -67_108_864},
	}
	for _, chunk := range chunks {
		chunkX, chunkY := chunk[0], chunk[1]
		positions := make([]factorioSpotCandidate, factorioChunkSize*factorioChunkSize)
		source := make([]float64, len(positions))
		for localY := int64(0); localY < factorioChunkSize; localY++ {
			for localX := int64(0); localX < factorioChunkSize; localX++ {
				index := localY*factorioChunkSize + localX
				positions[index] = factorioSpotCandidate{
					x: float64(chunkX*factorioChunkSize + localX),
					y: float64(chunkY*factorioChunkSize + localY),
				}
				source[index] = 1
			}
		}
		want := factorioRandomPenaltyBatch(positions, source, 1, 48)
		for index, position := range positions {
			got := evaluator.oilRandomPenaltyAt(int64(position.x), int64(position.y))
			if got != want[index] {
				t.Fatalf(
					"oil penalty at chunk (%d,%d) index %d = %g, want %g",
					chunkX, chunkY, index, got, want[index],
				)
			}
		}
	}
}

func TestFactorioFloorDiv(t *testing.T) {
	for _, test := range []struct {
		value int64
		want  int64
	}{
		{value: -33, want: -2},
		{value: -32, want: -1},
		{value: -1, want: -1},
		{value: 0, want: 0},
		{value: 31, want: 0},
		{value: 32, want: 1},
	} {
		if got := factorioFloorDiv(test.value, factorioChunkSize); got != test.want {
			t.Errorf("factorioFloorDiv(%d, 32) = %d, want %d", test.value, got, test.want)
		}
	}
}

func TestFactorioPreviewEntityDitherMaintainsCoverageAcrossZoom(t *testing.T) {
	for _, tilesPerPixel := range []float64{0.25, 0.5, 1, 2, 2.75, 4, 8, 16} {
		painted := 0
		const side = 128
		origin := -float64(side) * tilesPerPixel / 2
		for y := 0; y < side; y++ {
			wy := math.Floor(origin + float64(y)*tilesPerPixel)
			for x := 0; x < side; x++ {
				wx := math.Floor(origin + float64(x)*tilesPerPixel)
				if factorioPreviewEntityDither(wx, wy, tilesPerPixel) {
					painted++
				}
			}
		}
		coverage := float64(painted) / float64(side*side)
		if coverage < 0.40 || coverage > 0.60 {
			t.Errorf("dither coverage at %g tiles/pixel = %.3f, want 0.40..0.60", tilesPerPixel, coverage)
		}
	}
}

func factorioDecodeSpotExpression(t *testing.T, expression factorioSpotExpressionOracle) func(float64, float64) float64 {
	t.Helper()
	switch expression.Kind {
	case "const":
		return func(float64, float64) float64 { return expression.Value }
	case "x":
		return func(x, _ float64) float64 { return x }
	case "negx":
		return func(x, _ float64) float64 { return -x }
	case "xminus":
		return func(x, _ float64) float64 { return x - expression.Offset }
	case "xplus":
		return func(x, _ float64) float64 { return expression.Base + x }
	case "x2":
		return func(x, _ float64) float64 { return x * x * expression.Scale }
	case "stepx":
		return func(x, _ float64) float64 {
			if x > 0 {
				return expression.Value
			}
			return 0
		}
	default:
		t.Fatalf("unknown spot expression %q", expression.Kind)
		return nil
	}
}

func factorioResourceParamsNamed(t *testing.T, name string) factorioResourceParams {
	t.Helper()
	for _, params := range factorioResourceCatalog {
		if params.name == name {
			return params
		}
	}
	t.Fatalf("unknown Factorio resource %q", name)
	return factorioResourceParams{}
}

func readResourceOracle[T any](t *testing.T, name string) T {
	t.Helper()
	path := filepath.Join("testdata", "resource-oracles", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read resource oracle %s: %v", path, err)
	}
	var fixture T
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("decode resource oracle %s: %v", path, err)
	}
	return fixture
}

func formatUint32(value uint32) string {
	if value == 0 {
		return "0"
	}
	var digits [10]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
