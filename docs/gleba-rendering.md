# Gleba terrain in Go

The Fast renderer evaluates Gleba's terrain in Go, including its 41 tile
probabilities, climate, starting-area spirals, terraces, rockpools and fertile
soil spots. It needs no Factorio process, Lua interpreter, Rust runtime or WASM.
The browser receives PNGs from the existing Go preview endpoints. Gleba supports
public Fast previews; explicitly selected Exact previews retain authentication.

## Reference version

Verified on 2026-10-25: the official latest-releases API reports **2.1.23** for
both stable and experimental. The Steam installation reports **2.1.23, build
87750**. Fresh reference fields and preview PNGs were captured with the matching
headless build, using isolated temporary mods, configuration and saves.

The formulas and map colors follow Wube's
`space-age/prototypes/planet/planet-gleba-map-gen.lua`,
`space-age/prototypes/tile/tiles-gleba.lua`, and the two plant-noise expressions
in `space-age/prototypes/entity/plants.lua` from that installation. Wube also
publishes these in [factorio-data](https://github.com/wube/factorio-data).

SHA-256 of the 2.1.23 source files:

- `planet-gleba-map-gen.lua`: `218feb412e5e3975a3adddb0143045500d5e95f1f5b28373b023a1461fb422bb`
- `tiles-gleba.lua`: `d5935f088827d325d640f5ec2785292b93cdfdab860aa47835c2cdd9888944bf`

The primitive work builds on the existing Go noise kernels and the pinned
[FactorioMapWebUI Gleba notes](https://github.com/FactoryGameFan/FactorioMapWebUI/blob/562e015405bd1b68ffb4dd72ec0d2befa5474540/docs/superpowers/specs/2026-10-22-gleba-and-aquilo-design.md).
Its license and attribution are retained in
[upstream-rendering.md](upstream-rendering.md) and
`testdata/space-age-upstream/LICENSE`. The two original upstream Gleba fixtures
are copied unchanged into `testdata/gleba`. The `oracle-release-2.1.23.json`
fixture and the two versioned preview PNGs were captured locally by the included
script. No game binary or Lua source is vendored by this change.

## Arithmetic and tile selection

Dynamic expressions use float32 operations and the existing point/grid noise
paths. Constant starting-spot trigonometry is folded with libm. Dynamic sine and
cosine use Factorio's polynomial with float64 intermediate evaluation and a
float32 result. Binary inspection corrected a detail in the upstream notes:
**sine** subtracts 0.25 cycles before rounding; cosine does not. Terrace arithmetic
stays float32. Equal tile scores select the lexicographically first tile name;
Gleba has many exact ties across tiles with different map colors.

Fertile spot regions have a bounded mutex-protected cache shared by render
workers. Evaluation does not depend on sampling order or chunk scheduling.

## Validation

| Reference | Result |
| --- | --- |
| Upstream 1024×1024 terrain preview, map seed 123456 | 0 / 1,048,576 different pixels |
| Fresh 2.1.23 512×512 preview, map seed 123456 | 0 / 262,144 different pixels |
| Fresh 2.1.23 512×512 preview, map seed 987654321, water frequency 0.5, water size 2, plant frequency 3 | 0 / 262,144 different pixels |
| Fresh sampled fields across four surface seeds and control/start configurations | 1,792 / 1,792 tile winners agree |

The pixel tests include the production Go rendering path. Field tests compare
all four climate fields and all 41 tile probabilities. Observed maximum climate
residuals are 0.0000038147 elevation and 0.0000000596 moisture; aux and temperature
match exactly in these captures. Soil probabilities amplify tiny field residuals
with large multipliers, so score checks use an absolute-plus-relative tolerance.
The regression tests require zero different preview pixels and all sampled tile
winners to match. These captures establish the tested cases, not all-seed parity.

```sh
python3 scripts/capture-gleba-oracle.py /path/to/factorio \
  testdata/gleba/oracle-release-2.1.23.json --previews
go test -run 'TestFactorioGleba|TestGlebaSupports' -v .
go test -race -run 'TestFactorioGlebaReleasePreviews|TestFastPreviewSpaceAge' .
```

## Remaining limits

The exact-match results above concern terrain. The subsequent all-planet pass
adds separate full-preview captures, native plant/stromatolite candidate rolls,
stone placement and chart footprints. Enemy and cliff residuals remain;
Exact remains available for the full Factorio preview.
Arbitrary modded noise expressions and generated-world tile correction are not
implemented here.

The [all-planet accuracy report](factorio-2.1-accuracy.md) records the field checks, shared cliff placement, chart alpha and ordering corrections, current full-preview residuals, and remaining limitations.
