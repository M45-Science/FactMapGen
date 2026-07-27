# Factorio vs. FactMapGen fast-render comparison

Generated July 26, 2026 with Factorio headless 2.0.77 and the current
FactMapGen `custom-render` branch.

The comparison uses ten deterministic seeds. Each gallery places the Factorio
reference on the left, the FactMapGen fast Go render in the middle, and an
amplified RGB difference image on the right.

## Results

| Layer set | Comparison | Result |
| --- | --- | --- |
| Terrain only | 256 px at 1 meter per pixel | Mean changed pixels: **0.00061%**; worst seed: **0.00153%**; water-mask differences: **0%** |
| Terrain, trees, and cliffs | 512 px at 2 meters per pixel | Mean changed pixels: **14.63%**; mean absolute channel delta: **2.27 / 255**; mean water-mask difference: **0.000038%** |
| Terrain, resources, oil, and rocks | 512 px at 2 meters per pixel | Mean ore-region correlation: **0.837**; mean ore coverage ratio: **0.978x**; mean rock-region correlation: **0.895**; mean rock coverage ratio: **1.105x**; isolated fixed-seed oil correlation: **0.781**; recall: **0.531**; precision: **0.427**; coverage: **1.219x** |
| Terrain and enemy bases | 1024 px at 1 meter per pixel, isolated seed 123456 | Enemy-region correlation: **0.925**; coverage ratio: **1.093x**; starting-area edge delta: **0.9 tiles** |

## Interpretation

- Terrain is effectively pixel-identical. Six of ten seeds are exactly
  identical; the other four differ in one of 65,536 pixels. All ten water masks
  are identical.
- Tree pixels use discrete, seed-stable placement and Factorio's 40% quantized
  chart blend rather than a smooth expected-density wash. On the two isolated
  validation seeds, forest-region correlation is 0.981 and 0.984, ink coverage
  is 0.967x and 1.009x, and changed-pixel extent is 0.990x and 1.028x. One
  representative isolated render uses 71 colors and changes 29,791 pixels,
  versus Factorio's 88 colors and 30,078 pixels.
- Ore regions match well across the sample. FactMapGen renders 97.8% of
  Factorio's ore ink on average, and the weakest regional correlation is 0.746.
- Rock regions have stronger average regional correlation than ores. Sparse
  entity counts make per-seed coverage ratios noisier; the largest rock ratio is
  1.792x.
- Oil placement follows the validated patch field and Factorio's 32x32,
  row-major random-penalty stream. Wells now use the prototype's 3x3 chart
  footprint and reject water or existing ore; only the final shared
  entity-autoplace roll remains approximated. On isolated seed 123456, regional
  correlation is 0.781, one-pixel recall is 0.531, precision is 0.427, and
  coverage is 117 pixels versus Factorio's 96.
- Enemy nests now use Factorio's oracle-validated 45.2548-tile spot spacing and
  150-tile default starting-area radius. Spawner/worm selections also grow in
  distance bands instead of staying capped at one per grid cell, preserving the
  game's outward population ramp. The exact cross-prototype collision stream
  remains approximated.

## Reproduce the galleries

Install or configure Factorio headless, then run:

```sh
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio \
FACTMAPGEN_PREVIEW_GALLERY=1 \
FACTMAPGEN_NATURAL_PREVIEW_GALLERY=1 \
FACTMAPGEN_RESOURCE_PREVIEW_GALLERY=1 \
go test -run 'TestPreviewGallery(DefaultSeeds|NaturalLayersDefaultSeeds|ResourceLayersDefaultSeeds)$' -v
```

The generated HTML galleries, source PNGs, amplified difference PNGs, and
per-seed JSON statistics are written beneath `test-output/preview-gallery/`.
That directory is intentionally ignored by Git.

Run the fixed-seed isolated comparisons with `FACTMAPGEN_OIL_PREVIEW_PARITY=1 go test -run TestFastOilLayerMatchesFactorioPreview -v` and `FACTMAPGEN_ENEMY_PREVIEW_PARITY=1 go test -run TestFastEnemyLayersMatchFactorioPreviewRegions -v`.
