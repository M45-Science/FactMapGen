# Gleba terrain

The Fast renderer uses Factorio 2.1.23 (build 87750) as its reference for Gleba’s 41 terrain tiles, climate, starting area, terraces, rockpools, and fertile soil.

## Sources

The terrain rules come from Wube’s `planet/planet-gleba-map-gen.lua` and `tile/tiles-gleba.lua` under `space-age/prototypes/`. The plant rules come from `entity/plants.lua` in the same directory. [Wube’s Factorio data](https://github.com/wube/factorio-data)

The Go noise implementation builds on the pinned FactorioMapWebUI work. Its original Gleba field and terrain fixtures are stored alongside the captures from Factorio 2.1.23. [Upstream attribution](upstream-rendering.md) · [Gleba fixtures](../testdata/gleba)

Gleba’s terrain calculations use single precision. Dynamic sine and cosine use the game’s polynomial with double-precision intermediate values and a single-precision result. Sine subtracts a quarter cycle before rounding. Tied tile scores pick the tile whose name comes first alphabetically.

## Terrain checks

Both 512×512 terrain samples match Factorio 2.1.23 pixel for pixel. The sample for seed `987654321` uses half the default water frequency, twice the water size, and three times the plant frequency. Across four seeds and settings, the selected tile matched at all 1,792 sampled positions. [Full preview results](factorio-2.1-accuracy.md)

Run the Gleba field and preview checks from the repository root.

```sh
go test -run '^TestFactorioGleba' -v .
```
