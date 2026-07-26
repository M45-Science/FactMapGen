# Factorio vs. FactMapGen fast-render comparison

Generated July 26, 2026 with Factorio headless 2.0.77 and the current
FactMapGen `custom-render` branch.

The comparison uses ten deterministic seeds. Each gallery places the Factorio
reference on the left, the FactMapGen fast Go render in the middle, and an
amplified RGB difference image on the right.

## Results

| Layer set | Comparison | Ten-seed result |
| --- | --- | --- |
| Terrain only | 256 px at 1 meter per pixel | Mean changed pixels: **0.00061%**; worst seed: **0.00153%**; water-mask differences: **0%** |
| Terrain, trees, and cliffs | 512 px at 2 meters per pixel | Mean changed pixels: **14.63%**; mean absolute channel delta: **2.27 / 255**; mean water-mask difference: **0.000038%** |
| Terrain, resources, oil, and rocks | 512 px at 2 meters per pixel | Mean ore-region correlation: **0.837**; mean ore coverage ratio: **0.978x**; mean rock-region correlation: **0.895**; mean rock coverage ratio: **1.105x**; meaningful-seed oil-region correlation: **0.914**; aggregate oil coverage ratio: **1.032x** |
| Terrain and enemy bases | 1024 px at 1 meter per pixel, two isolated validation seeds | Enemy-region correlation: **0.927**, **0.887**; coverage ratio: **1.064x**, **1.082x**; starting-area edge delta: **6.1**, **2.7 tiles** |

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
  row-major random-penalty stream, then uses a map-seeded approximation only for
  the final shared entity-autoplace roll. Across the nine seeds with at least
  eight reference pixels, regional correlation averages 0.914, one-pixel recall
  is 0.642, and one-pixel precision is 0.727. Aggregate coverage is 292 pixels
  versus Factorio's 283.
- Enemy nests use the same distance-scaled spot field, blob noise, starting-area
  exclusion, and random-penalty structure as Factorio. The exact cross-prototype
  entity collision stream remains approximated, but isolated nest regions,
  coverage, and the biter-free starting-area edge closely match both seeds.

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

Run the two-seed enemy comparison separately with `FACTMAPGEN_ENEMY_PREVIEW_PARITY=1 go test -run TestFastEnemyLayersMatchFactorioPreviewRegions -v`.
