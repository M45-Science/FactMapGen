# FactMapGen

Preview Factorio and Space Age maps in your browser. Pick a seed, adjust the map settings, and share a preview link or export a map exchange string to use in Factorio.

[Try FactMapGen](https://mapgen.m45sci.xyz/)

## Fast preview accuracy

We compared Fast previews with Factorio 2.1.23 using two map seeds per planet at one tile per pixel. Results can vary with the seed, settings, and zoom. [Full accuracy report](docs/factorio-2.1-accuracy.md)

| Planet | Matching pixels |
| --- | ---: |
| Nauvis | 99.929–99.960% |
| Vulcanus | 99.621–99.758% |
| Gleba | 99.954–99.966% |
| Fulgora | 99.903–99.943% |
| Aquilo | 99.996–99.998% |

Fast previews are available to guests. Exact previews use Factorio itself and are available when enabled by the server.

![FactMapGen preview of Nauvis](docs/preview.png)

## License

Copyright © 2026 Carl Frank Otto III for original contributions. [AGPL-3.0-or-later](LICENSE)

The renderer includes work adapted from FactorioMapWebUI under the same license. [Upstream attribution](docs/upstream-rendering.md) · [Upstream fixture license](testdata/space-age-upstream/LICENSE)
