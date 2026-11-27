# Fast preview accuracy

We compared FactMapGen’s Fast previews with Factorio 2.1.23 Space Age previews for all five planets. Each comparison used two map seeds (`123456` and `987654321`) and a 512×512 image centered at the map origin, at one tile per pixel.

## Results by planet

Total RGB error adds up the absolute differences in red, green, and blue across the whole image. A small color change contributes less than a large one. Each cell lists seed `123456` first and seed `987654321` second. Full previews include features such as resources, plants, and enemies.

| Planet | Terrain only | Terrain and cliffs | Full preview |
| --- | ---: | ---: | ---: |
| Nauvis | 111 / 66 | 111 / 914 | 4,181 / 8,844 |
| Vulcanus | 5,626 / 47,665 | 85,611 / 117,365 | 109,635 / 170,039 |
| Gleba | 0 / 0 | 9,415 / 10,494 | 12,903 / 11,688 |
| Fulgora | 0 / 0 | 8,125 / 9,489 | 12,814 / 22,172 |
| Aquilo | 0 / 0 | 0 / 0 | 754 / 1,362 |

Fulgora’s terrain also matched a separate 1024×1024 reference pixel for pixel. Aquilo’s full-preview differences were all within two pixels of the image edge. Vulcanus had the largest differences of the five planets, including visible differences in terrain and cliffs.

Separate enemy-marker checks matched Factorio pixel for pixel in all six Nauvis and Gleba samples, including starting areas, distant areas, and settings with doubled enemy size and frequency. These checks used the center of a larger Factorio capture to account for image-edge effects.

These figures describe the sampled preview images. Results can vary with the seed, settings, and zoom. Fast previews approximate feature placement at wider zooms and have limited support for mods.

## Visual comparison

All five comparisons use seed `123456`. A is Factorio’s preview and B is FactMapGen’s Fast preview. DIFF shows the absolute difference between their red, green, and blue values at each pixel. Identical pixels are black.

### Nauvis

![Nauvis A/B/DIFF comparison. Total RGB error is 4,181, mostly along the image edges.](accuracy-demo/nauvis.png)

### Vulcanus

![Vulcanus A/B/DIFF comparison. Total RGB error is 109,635, the highest of the five samples.](accuracy-demo/vulcanus.png)

### Gleba

![Gleba A/B/DIFF comparison. Total RGB error is 12,903.](accuracy-demo/gleba.png)

### Fulgora

![Fulgora A/B/DIFF comparison. Total RGB error is 12,814.](accuracy-demo/fulgora.png)

### Aquilo

![Aquilo A/B/DIFF comparison. Total RGB error is 754, all near the image edges.](accuracy-demo/aquilo.png)

## Run the comparison

From the repository root, run the five-planet test against the [saved Factorio previews](../testdata/planets-2.1.23).

```sh
FACTMAPGEN_ACCURACY_ARTIFACTS=1 go test -run '^TestPlanetReleaseAccuracy$' -v .
```

The test logs total and average RGB error and writes comparison images to `test-output/accuracy-2.1.23/`. For capture commands, renderer changes, and additional checks, see the [original technical audit](https://github.com/M45-Science/FactMapGen/blob/ea36c1a6e5ba9b65e749bb5ac9b58cd00c926ff4/docs/factorio-2.1-accuracy.md).
