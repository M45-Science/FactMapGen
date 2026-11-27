# Upstream renderer attribution

FactMapGen’s Fast renderer adapts noise, terrain, and cliff algorithms from FactoryGameFan/FactorioMapWebUI, pinned to commit `562e015405bd1b68ffb4dd72ec0d2befa5474540`. [Upstream source](https://github.com/FactoryGameFan/FactorioMapWebUI/tree/562e015405bd1b68ffb4dd72ec0d2befa5474540)

FactorioMapWebUI and the adapted Go code are licensed under AGPL-3.0-or-later. Keep this attribution and the upstream license with derived distributions. [Upstream fixture license](../testdata/space-age-upstream/LICENSE)

The port draws on `crates/fmw-noise/src/`, `docs/noise/`, `docs/rust-wasm-port.md`, and `test/fixtures/basis-gradient-table.json`. Factorio game data retains its own provenance and license. [Gleba game-data sources](gleba-rendering.md)

Later upstream changes informed Fulgora’s tile scoring, Nauvis’s enemy starting radius and cliff rounding, and the starting angles, climate, and cracks on Space Age planets. [Fulgora tile scoring](https://github.com/FactoryGameFan/FactorioMapWebUI/pull/537) · [Enemy starting radius](https://github.com/FactoryGameFan/FactorioMapWebUI/pull/540) · [Nauvis cliff rounding](https://github.com/FactoryGameFan/FactorioMapWebUI/pull/538) · [Starting angles](https://github.com/FactoryGameFan/FactorioMapWebUI/pull/534) · [Vulcanus rounding](https://github.com/FactoryGameFan/FactorioMapWebUI/pull/529)

The Nauvis temperature expression and Vulcanus starting resource cones also use the game’s single-precision arithmetic. [Upstream rounding follow-up](https://github.com/FactoryGameFan/FactorioMapWebUI/pull/533)

Further parity work adapted upstream details for Nauvis terrain scoring and Vulcanus volcano placement. [Nauvis tile scores](https://github.com/FactoryGameFan/FactorioMapWebUI/pull/522) · [Nauvis terrain expressions](https://github.com/FactoryGameFan/FactorioMapWebUI/pull/523) · [Vulcanus volcano expressions](https://github.com/FactoryGameFan/FactorioMapWebUI/blob/e359553a29179bb58fd83813d985658c7f883ed5/crates/fmw-noise/src/expressions/vulcanus_biomes.rs)

## Reference fixtures

The upstream JSON and PNG fixtures are unmodified copies of `test/fixtures/`. Their metadata records the original Factorio versions and capture settings. The 1024×1024 terrain images use map seed `123456` at one tile per pixel with entities disabled. Space Age surfaces derive their own seeds from the map seed. Point samples use the surface seed `123456` directly. [Upstream fixtures in this repository](../testdata/space-age-upstream)

Run the upstream terrain comparison from the repository root.

```sh
go test -run '^TestUpstreamExistingTerrainPreview$' -v .
```

See the [Factorio 2.1.23 accuracy report](factorio-2.1-accuracy.md) for full previews across all five planets.
