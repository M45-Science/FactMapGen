# Factorio vs. FactMapGen fast-render comparison

Generated July 25, 2026 with Factorio headless 2.0.77 and the current
FactMapGen `custom-render` branch.

The comparison uses ten deterministic seeds. Each gallery places the Factorio
reference on the left, the FactMapGen fast Go render in the middle, and an
amplified RGB difference image on the right.

## Results

| Layer set | Comparison | Ten-seed result |
| --- | --- | --- |
| Terrain only | 256 px at 1 meter per pixel | Mean changed pixels: **0.00061%**; worst seed: **0.00153%**; water-mask differences: **0%** |
| Terrain, trees, and cliffs | 512 px at 2 meters per pixel | Mean changed pixels: **33.00%**; mean absolute channel delta: **2.12 / 255**; mean water-mask difference: **0.000038%** |
| Terrain, resources, oil, and rocks | 512 px at 2 meters per pixel | Mean ore-region correlation: **0.836**; mean ore coverage ratio: **0.981x**; mean rock-region correlation: **0.895**; mean rock coverage ratio: **1.105x**; mean oil coverage ratio: **1.251x** |

## Interpretation

- Terrain is effectively pixel-identical. Six of ten seeds are exactly
  identical; the other four differ in one of 65,536 pixels. All ten water masks
  are identical.
- Tree pixels are intentionally an expected-density overlay rather than
  Factorio's exact individual entity rolls. The raw changed-pixel percentage is
  therefore high even when the forest regions match. The low mean channel delta
  and nearly identical water masks are more useful whole-image indicators.
- Ore regions match well across the sample. FactMapGen renders 98.1% of
  Factorio's ore ink on average, and the weakest regional correlation is 0.746.
- Rock regions have stronger average regional correlation than ores. Sparse
  entity counts make per-seed coverage ratios noisier; the largest rock ratio is
  1.792x.
- Oil is the loosest approximation. Its average coverage is 1.251x Factorio,
  with a per-seed range of 0.718x to 2.389x.

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
