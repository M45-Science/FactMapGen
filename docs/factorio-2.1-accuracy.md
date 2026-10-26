# Factorio 2.1.23 accuracy audit

The reference is Factorio **2.1.23, build 87750**, the latest stable and experimental release reported by Factorio's release endpoint when checked on 2026-10-25. The Steam installation reports the same release. Captures use the matching headless build in an isolated temporary profile; they do not modify Steam, player saves, or mods. Rendering remains native Go.

## Captured coverage

`testdata/planets-2.1.23` contains two map seeds (`123456`, `987654321`) for all five planets. Each seed has a manifest recording the actual surface seed, map settings, 1,089 field positions per planet, an entity census, and a separately generated cliff-only census. Thirty 512×512 PNGs cover terrain, terrain plus cliffs, and full previews at one tile per pixel, centred on the origin. Terrain captures disable the cliff prototype by name: zero richness alone does not disable Gleba's rockpool cliffs.

Reproduce using a local Space Age installation:

```sh
python3 scripts/capture-planet-oracles.py /path/to/factorio testdata/planets-2.1.23 --previews
python3 scripts/capture-gleba-oracle.py /path/to/factorio testdata/gleba/oracle-features-2.1.23.json --features
python3 scripts/capture-chart-oracle.py /path/to/factorio testdata/chart-2.1.23
python3 scripts/capture-enemy-oracle.py /path/to/factorio testdata/enemies-2.1.23 --previews
FACTMAPGEN_ACCURACY_ARTIFACTS=1 go test -run 'TestPlanetReleaseAccuracy|TestAquiloReleaseCandidates|TestGlebaFeatureFields|TestFactorioChartReleaseOracle|TestFactorioEnemyRelease' -v .
```

The scripts reject binaries outside the 2.1 series and record the exact version. They do not automatically establish that a later release is equivalent. The tests require the pinned 2.1.23 fixtures. The generated comparison and difference images go under `test-output/accuracy-2.1.23`.

## Measured results

Percent of pixels differing from Factorio, shown as seed 123456 / seed 987654321. These are sampled regression results, not all-seed guarantees. Full previews include entity differences that terrain-only measurements conceal.

| Planet | Terrain | Terrain plus cliffs | Full preview |
| --- | ---: | ---: | ---: |
| Nauvis | 0.00114 / 0.00114 | 0.00114 / 0.00267 | 0.04005 / 0.07057 |
| Vulcanus | 0.03853 / 0.14381 | 0.18806 / 0.27313 | 0.24223 / 0.37918 |
| Gleba | 0 / 0 | 0.02747 / 0.02480 | 0.04616 / 0.03357 |
| Fulgora | 0.00267 / 0.00229 | 0.02518 / 0.02899 | 0.09689 / 0.05684 |
| Aquilo | 0 / 0 | 0 / 0 | 0.00420 / 0.00229 |

The full-preview thresholds intentionally allow the measured residuals. Passing them means no regression beyond those bounds; it does not mean exact parity.

This follow-up reduces the previous full-preview differences of 0.14839 / 0.11673% on Nauvis, 0.44289 / 0.66261% on Vulcanus, 0.29907 / 0.19798% on Gleba and 0.25864 / 0.14000% on Fulgora. Aquilo is unchanged. Nauvis now differs at only three interior pixels in each image; the other 102 / 182 pixels are in the outer eight-pixel border. Full-image metrics above retain these border differences.

Enemy placement has additional independent coverage at seed **424242**, with six cases: Nauvis and Gleba at spawn, at (4096, 0), and with enemy frequency/size both doubled. All six have **zero enemy-marker pixel differences** across the complete 512×512 target area when compared with the central crop of a native 640×640 capture. They contain 17,812 reference enemy pixels in total. The corresponding native 512×512 captures differ at 0 / 134 / 0 pixels on Nauvis and 0 / 86 / 12 on Gleba, demonstrating the CLI capture's boundary behavior without introducing viewport-dependent placement into Go. These checks establish parity for the captured cases, not all seeds or zoom levels.

Before the chart-rendering corrections, full-preview differences were 11.09428 / 12.80060% on Nauvis, 9.22012 / 9.78394% on Vulcanus, 10.45532 / 9.77097% on Gleba, 5.58968 / 3.02238% on Fulgora, and 0.23117 / 0.32349% on Aquilo. The terrain and cliff-only results did not change in that pass.

The controlled chart fixture independently captures known overlapping trees, rocks and ores against a constant background. It matches exactly, including integer alpha accumulation, the ore checkerboard phase, collision-box footprints, generation-chunk clipping and draw order. Magnifying that captured chart at 0.4 and 0.75 tiles per pixel also repeats its complete world tiles exactly. The detailed new placement paths apply through two tiles per pixel; broader zooms still use bounded approximations and are not covered by these one-tile accuracy percentages.

## Corrections in this pass

- **Shared cliff placement:** corners use the bare four-tile lattice; the half-tile Y offset belongs only to entity centres. Whole-chunk repair clears impossible edge combinations in the game's order, including its retry behaviour. Queries retain chunk context when rendering adjacent viewports.
- **Planet cliff settings:** Vulcanus uses elevation 70, interval 120 and smoothing 1; Gleba uses 40, 60 and richness 0.8; Fulgora uses 80, 40 and richness 0.95. Vulcanus has no Nauvis cliff control. Aquilo places no cliffs.
- **Cliff fields and collisions:** Vulcanus multisample offsets use the cliff consumer's four-tile grid. Gleba uses its rockpool cliffiness term; Fulgora uses its road and starting-area expressions. Fulgora's cliff mask allows oil oceans. Rejection clears shared crossings without recursively rejecting neighbours.
- **Cliff drawing:** the preview draws the AABB of each orientation's rotated collision box, with fixed-point quantization and floor/ceil raster bounds. Rectangles are now clipped to the cliff's own 32-tile generation chunk, fixing ink that spilled into neighboring chunks.
- **Gleba features:** twelve plant probability fields, both stromatolites, stone probability/richness and cliffiness have direct 2.1.23 oracle coverage at 2,959 positions. Stone, stromatolites and cliffiness have zero residual; maximum plant residual is 0.00000178814. Both spawner fields now share this coverage, with maximum nonnegative-probability residual 0.000000121072. Plants, stromatolites, stone and spawners use chunk-order candidate rolls, tile restrictions and real chart boxes.
- **Enemy fields:** omitted candidate spacing is derived by the native compiler from integer region area / candidate count, producing 25.5978508 rather than the previous 45.254834. Negative cone tails extend to the 128-tile cull radius. Float32 basis/cone arithmetic and the engine's spot-cone constant 3.1416 are retained. Raw base fields are checked at 2,113 positions for both planets and two seeds, with maximum residual 0.00000190735. Distant worm multipliers amplify small base residuals; their probability regression tolerance is 0.00002.
- **Enemy placement and drawing:** random penalties consume draws only for positive sources and seed from the translated batch origin. Nauvis spawners/worms and both Gleba nests use the shared chunk candidate stream, actual tile restrictions and map-generation collision boxes. Accepted enemies are collision-filtered in reverse order and painted after vegetation, matching the native deferred chart pass. Gleba includes its starting nests, safe starting nest, fertile coastal term and deep-water exclusion.
- **Nauvis resources:** solid ores, oil and uranium now participate in the native candidate stream at detailed zooms. Oil no longer uses a hashed acceptance approximation there. Same-order resource ties compare richness; native resource boxes, chart clipping and placement rolls are used.
- **Shared spot selection:** a skip offset equal to the span is valid and skips the initial candidate; it must not reject every candidate. Gleba stone exercises this case.
- **Shared candidate placement:** candidates share one chunk RNG across distinct autoplace-order groups, with reverse tile traversal. Off-grid candidates consume two additional draws and use sixteenth-tile positions. Allowed tiles still consume a roll at zero probability. Nauvis includes fish's otherwise invisible water pass; Vulcanus includes the crater-cliff pass before its other scenery. These passes change the positions of subsequent entities.
- **Chart compositing:** draw cliffs first, then entity candidates in reverse autoplace order. The preview does not perform the generated world's final tree/rock collision pass. Collision boxes are quantized to 1/256 tile, rounded to integer chart rectangles and clipped to the entity's own 32-tile chunk. Alpha blends accumulate for every overlapping rectangle. Nauvis's original tree color and alpha were correct; its single-pixel footprints and collapsed species rolls were not. Solid ore's two-tile checkerboard phase was inverted, and rocks incorrectly inherited ore dithering.
- **Planet scenery:** Nauvis uses all twenty tree prototypes and its rock groups. Fulgora includes ruins, fulgurites, attractors, sunken ruins, rocks and scrap, with prototype colors and alpha. Vulcanus includes chimneys, rocks, ash lichens, ores and vents. Aquilo now draws real well and iceberg rectangles without the inappropriate final-world collision pass.
- **Fractional zoom:** rectangle bounds use the terrain sampler's coordinate convention to prevent floating-point division from splitting a single world tile across two colors.

Fresh cliff-position recall is 12/12 and 8/8 on Nauvis, 902/902 and 663/663 on Fulgora, 1,610/1,616 and 1,907/1,912 on Vulcanus, and 516/519 and 401/403 on Gleba. Predicted counts are 12/8, 902/663, 1,614/1,910 and 518/401 respectively. World generation and preview generation differ in cliff collision/orientation handling; the world census is not a substitute for the separate PNG checks.

Aquilo's earlier generated-world census reported exact recall for all 13 fluorine vents and 12 lithium-brine wells, but lower recall for oil and icebergs. That census is not a direct preview-placement oracle: world generation and preview generation apply different entity collision rules. The separate preview PNGs now differ at only 11 / 6 pixels, all in the outer two-pixel margin; their 508×508 interiors match exactly. Keep the complete-image counts above: removing image-border candidates just to match the CLI capture would make rendering depend on the viewport and risk seams between adjacent tiles.

## Remaining inconsistencies

| Area | Unresolved work |
| --- | --- |
| Nauvis | Three interior terrain pixels per captured image, plus image-edge residuals. Other seeds/settings can still expose field rounding differences. |
| Vulcanus | Terrain and cliff residuals, crater-cliff segment geometry, ore-driven cliff removal, vent richness rejection, demolisher territories. |
| Gleba | Remaining cliff and image-edge residuals; enemies match the additional padded captures. |
| Fulgora | Remaining terrain, cliff and image-edge residuals; rare probability/richness tie handling is not modeled. |
| Aquilo | Captured full-preview differences are confined to image edges; other seeds and settings still need equivalent coverage. |
| Cross-cutting | More seeds, non-default controls, shifted starting positions and native previews at other zoom levels need equivalent coverage. Broad zooms retain approximate placement. Arbitrary mod expression evaluation is not implemented. |

## Evidence and provenance

The shared cliff repair and smoothing algorithms adapt the pinned FactorioMapWebUI reference described in [upstream-rendering.md](upstream-rendering.md). The binary investigation independently checked these 2.1.23 Linux symbols:

- `EntityMapGenerationTask::generateEntities(NoiseCache&)` (`0x1989690`): chunk seed, order-group flush, probability/richness tie handling, reverse tile traversal and off-grid draws.
- `MapPreviewGenerator::Worker::computeEntities(MapPreviewChunk&)` (`0x20ab0a0`): oriented cliff box conversion, reverse candidate drawing and enemy-specific collision handling.
- `ChartingInterface::drawEntity(...)` (`0x20acbd0`): entity chart footprints and the ore checkerboard phase.
- `BoundingBox::getAABB()` (`0x16a8370`) and `getRotatedVertices(...)` (`0x16a8a50`): fixed-point rotation and AABB construction.
- `ChartingInterface::drawRectangle(...)` (`0x20ad620`): lower floor, exclusive upper ceil and integer colour blending.
- `NoiseExpressions::SpotNoise::compile(...)` (`0x306f740`): omitted candidate-spacing calculation.
- `ChartingInterface::renderAllDeferredEntities()` (`0x20ac8a0`): hostile entity rectangles are painted after ordinary entities.

Addresses are build-specific evidence, not runtime dependencies. No binary code or disassembly is shipped, and Go does not invoke Factorio to render Fast previews.

## Validation of this change

The complete Go suite passes (85.884 s), including all thirty five-planet image comparisons, six additional enemy previews with both normal and padded captures, the native enemy field oracle, the controlled chart oracle, fractional zoom, API behavior and existing cache tests. The detailed enemy image checks require exact marker masks against the padded references. Targeted race tests pass (144.188 s), covering the enemy reference fields/previews, Gleba fields, Space Age rendering, cache/direct equivalence, client tile stitching, concurrent requests and both Nauvis golden renders. `go vet ./...` and both web test files pass. The Nauvis one- and two-tiles-per-pixel golden hashes are updated for the independently verified placement corrections; the renderer cache revision is v8.
