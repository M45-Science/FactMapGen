# FactMapGen

Preview Factorio and Space Age maps in your browser. Pick a seed, adjust the map settings, and share a preview link or export a map exchange string to use in Factorio.

[Try FactMapGen](https://mapgen.m45sci.xyz/)

## Fast preview accuracy

Standalone Fast previews matched Factorio 2.1.23 pixel for pixel on all five planets in two 512×512 tests per planet at one tile per pixel. [Full accuracy report](docs/factorio-2.1-accuracy.md)

| Planet | Total RGB error |
| --- | ---: |
| Nauvis | 0 |
| Vulcanus | 0 |
| Gleba | 0 |
| Fulgora | 0 |
| Aquilo | 0 |

Fast previews are available to guests. Exact previews use Factorio itself and are available when enabled by the server.

![FactMapGen preview of Nauvis](docs/preview.png)

## License

Copyright © 2026 Carl Frank Otto III for original contributions. [AGPL-3.0-or-later](LICENSE)

The renderer includes work adapted from FactorioMapWebUI under the same license. [Upstream attribution](docs/upstream-rendering.md) · [Upstream fixture license](testdata/space-age-upstream/LICENSE)
