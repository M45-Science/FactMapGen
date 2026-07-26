# FactMapGen

FactMapGen is a small Go web tool for creating and editing Factorio `map-gen-settings.json` and `map-settings.json` files. Map presets are stored on the server as folders; the browser edits those server-side files directly.

![FactMapGen fast Nauvis preview with terrain, resources, trees, and cliffs at an arbitrary 2.75 meters per pixel](docs/preview.png)

## Run

```sh
go run . -addr :8080 -presets presets
```

The Fast renderer keeps up to 2 GiB of reusable tile pixels by default. Change the startup budget in MiB with, for example:

```sh
go run . -addr :8080 -presets presets -fast-preview-cache-mib 4096
```

Open `http://localhost:8080`.

On the first run, FactMapGen creates an `admin` account and prints a generated password to the server log. User accounts, sessions, and audit entries are stored in `factmapgen-auth.db` by default. Use `-auth-db path/to/auth.db` to choose a different SQLite file.

The `-data` flag still works as a backwards-compatible alias for `-presets`.

## Build

```sh
go build -o factmapgen .
./factmapgen -addr :8080 -presets presets
```

## Authentication

Guests can browse presets, create or import a transient working preset, edit settings, generate Fast previews, export exchange strings, and download ZIP files without logging in. Exact Factorio previews require sign-in. Guest work never writes preset folders on disk. Persisting server presets by creating, importing, saving, duplicating, renaming, or deleting generators requires a logged-in user. Admin users can add, edit, and delete accounts from the Admin panel in the browser. Passwords are stored as PBKDF2-SHA256 hashes in SQLite, and sessions use HttpOnly cookies.

Preset create, import, update, duplicate, rename, and delete actions write audit log entries with the acting user, action, target, detail, and timestamp. Account changes are audited too.

## Server Files

Each preset is saved as:

```text
presets/
  Preset Name/
    map-gen-settings.json
    map-settings.json
```

The UI focuses on the visual map generator. Frequently tuned settings use sliders, toggles, selects, and named per-setting presets for world size up to 16K, terrain shape, cliffs, evolution, expansion, pollution, pathfinding pressure, and resources. New presets are created from a modal dialog so the main toolbar stays focused on editing. The built-in per-setting preset buttons are derived from Factorio's bundled `map-gen-presets.lua` and `autoplace-controls.lua` data where those values can be represented directly. Space Age-only autoplace controls are kept in a separate Space Age tab by planet, based on the installed `space-age/prototypes/autoplace-controls.lua` and planet map-gen data. Keys not exposed visually are preserved when saving, as long as the preset already contains them.

In `map-gen-settings.json`, `seed: null` follows Factorio's public example file and is the default. It tells Factorio to choose a unique randomized map seed each time a new map is generated. The editor also treats older `seed: 0` presets as random. Use a positive seed value only when you want repeatable generation.

## Map Previews

FactMapGen includes a fast built-in Go preview renderer for Nauvis-style maps. Its default Nauvis path ports Factorio-compatible seeded noise, elevation, climate fields, and all 21 terrain-tile probability expressions. Forest regions use the 15 Nauvis tree-species probability expressions with seed-stable discrete placement and Factorio's quantized chart blending; they reproduce the forest texture without claiming exact per-tree entity rolls. Cliffs use the deterministic Nauvis cliff fields, four-tile placement lattice, and crossing rules. Iron, copper, coal, stone, and uranium use Factorio's resource candidate stream, patch selection, starting-area placement, blob noise, and control levers. Crude oil uses the same validated patch field, Factorio's chunk-ordered random-penalty stream, and the oil well's chart footprint; only the final entity-autoplace roll remains approximated because it shares state with other entity autoplacers. Rocks use the Nauvis rock-density and climate expressions. Enemy bases use Factorio's distance-scaled spot quantities, 512-tile candidate regions, three seeded blob-noise scales, starting-area exclusion, spawner and worm distance tiers, and chunk-ordered random penalties. Their final entity collision order is represented with global deterministic spacing cells, preserving nest regions and chart texture without claiming exact individual entities. Ore and rock chart pixels use a stable world-position dither because Factorio's exact per-entity placement roll shares a chunk RNG stream with every other entity autoplacer. For signed-in users, the preview toolbar also offers an Exact engine that generates real Factorio map preview PNGs when a Factorio/headless binary is available. Install the latest experimental Linux headless package into the default discovery path with:

```sh
./scripts/install-factorio-headless.sh
```

The standalone installer accepts the release channel as its second argument; use stable explicitly when needed:

```sh
./scripts/install-factorio-headless.sh tools/factorio stable
```

Then restart FactMapGen:

```sh
go run . -addr :8080 -presets presets
```

The server auto-discovers `tools/factorio/bin/x64/factorio`. Managed installs and update checks use the experimental channel by default. Select stable at boot with:

```sh
go run . -factorio-channel stable
```

You can also point at another install:

```sh
go run . -factorio-bin /opt/factorio/bin/x64/factorio
```

The main web page shows the detected Factorio binary version and flags when the latest headless release on the configured channel is newer. Version and latest-release checks are cached by the server. The Admin panel shows the active release channel. When the active binary comes from `-factorio-dir`, administrators can delete that managed install and install a fresh headless copy from that channel. The fast renderer supports Nauvis-style maps, including the Lakes and Island elevation options. The Exact engine's preview surface selector offers the known Factorio planets: Nauvis plus the Space Age planets Vulcanus, Gleba, Fulgora, and Aquilo.

Preview scale is an arbitrary numeric value in meters per output pixel from `0.015625` through `64`, with the browser viewer capped at `25` meters per pixel and subject to the Exact engine's 16,384-pixel source-render limit. Values above `1` show more map area and values below `1` zoom into whole map tiles. Legacy API values such as `out-4` and `in-3` remain accepted. Guest previews are locked to 1 meter per pixel by both the browser and server; signed-in users retain the full zoom range. In the browser, drag a rendered preview to pan, use the mouse wheel to zoom around the pointer, and use Reset to return to `(0, 0)` at 1 meter per pixel. Fast previews automatically size their square viewer to the available pane dimensions and hide the whole-image size selector, which remains available for Exact previews. Fast previews use 512x512 client tiles at one canonical source resolution of 1 map tile per image pixel: pan repositions retained tiles immediately and requests only tiles absent from the client cache, while zoom scales those tiles around the pointer immediately in the browser without requesting alternate-resolution copies. Newly exposed world coordinates begin loading their missing canonical source tiles during pan and zoom updates, with up to eight requests in flight and deterministic center-biased jitter instead of a top-left row sweep. After all visible tiles load, the client prefetches one complete one-tile-deep perimeter around the viewport's visible tile bounds. Perimeter counts therefore vary with the viewport dimensions. These requests do not count toward visible progress and are canceled if they become irrelevant to a changed viewport. Shared tile edges are snapped to identical screen pixels to prevent subpixel seams without adding overlap or another rendering pass. Returning to a cached area makes no request, and queued or in-flight tile requests are aborted as soon as they leave the latest viewport. The client LRU has a 512 MiB decoded-pixel target, which retains 512 canonical 512x512 RGBA tiles in addition to browser image and DOM overhead; changing the preset, seed, or map-generation settings clears the prior client tile cache, while a very large viewport can temporarily require more visible tiles. Exact previews remain whole images. Both engines accept world-space `centerX` and `centerY` coordinates; the server snaps each coordinate to the selected scale's pixel lattice so adjacent views align exactly.

Exact Factorio preview generation is queued server-side so only one Factorio process runs at a time. The queue defaults to 8 waiting jobs and can be changed with `-preview-queue`. Each exact render is capped at 60 seconds by default and can be changed with `-preview-timeout`. Admin preview jobs are served before regular signed-in users; jobs with the same priority run in request order. If the queue is full, the exact preview request returns HTTP 429.

Generated response images are transient. Both Fast and Exact previews use lossless PNG; transformed images use Go BestSpeed compression. The browser receives a short-lived `/api/previews/...` URL. These encoded images are kept in memory for up to 30 minutes with a 100-image cap and a default 128 MiB total payload cap. The oldest unpinned response is evicted to satisfy either cap. The pinned startup Default response survives expiration and eviction but still counts toward both caps; if its payload leaves too little capacity for a new response, that request fails instead of exceeding the byte limit. The byte cap counts encoded payload bytes only, excluding store metadata and images or buffers still being rendered or encoded.

Fast previews also use a process-local LRU of 128x128 base-render tiles, keyed by canonical map-generation settings, effective seed, scale, and world position. This lets repeated views, adjacent pans, and a return to a recent zoom reuse work without changing pixels. The default cache retains up to four settings/seed worlds and 2 GiB of RGBA tile pixels globally; set another budget with `-fast-preview-cache-mib`. Resource-region state is trimmed to 64 regions per field. One fully cached 1024x1024 view occupies exactly 4 MiB of tile pixels, a one-pixel horizontal pan can extend that scale to 4.5 MiB, and another fully retained scale adds 4 MiB. With no overlap, the default pixel budget is enough for 512 complete 1024x1024 views. The 2 GiB value is a limit, not an upfront allocation: startup warms only the built-in Default preset at 1024x1024, 1 meter per pixel, and preview seed `123456`, retaining about 4 MiB of tile pixels. The browser uses that seed for the initial random-seed Default preview so its first request can reuse the warm tiles. The limit covers cached tile pixels, not active output images, evaluator state, temporary buffers, or the separately capped encoded-image payloads, so it is not a total process-memory limit. The cache is cleared when the server restarts.

Exact previews do not use the Fast tile cache: each request runs the queued Factorio process, passes the normalized map offset, and then applies the requested zoom transform to Factorio's temporary PNG. On startup, when Exact previews are configured, the server warms a pinned 512px Nauvis preview for the built-in Default preset so signed-in users can see an immediate exact image without waiting for autosize rendering.

Preview parity tests compare rendered PNG pixels and write visual artifacts. The fast terrain integration cases disable resources, trees, rocks, enemies, cliffs, and decorations so terrain correctness is measured independently. Natural-layer cases isolate trees and cliffs, resource-layer cases isolate ores, oil, and rocks, and enemy-layer cases isolate spawners and worms. These tests measure regional correlation and coverage instead of requiring exact randomized entity pixels:

```sh
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio go test -run TestExactPreviewMatchesDirectFactorioPreview
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_FAST_PREVIEW_PARITY=1 go test -run TestFastPreviewMatchesFactorioPreview
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_NATURAL_PREVIEW_PARITY=1 go test -run TestFastNaturalLayersMatchFactorioPreviewRegions -v
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_RESOURCE_PREVIEW_PARITY=1 go test -run TestFastResourceLayersMatchFactorioPreviewRegions -v
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_OIL_PREVIEW_PARITY=1 go test -run TestFastOilLayerMatchesFactorioPreview -v
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_ENEMY_PREVIEW_PARITY=1 go test -run TestFastEnemyLayersMatchFactorioPreviewRegions -v
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_PREVIEW_GALLERY=1 go test -run TestPreviewGalleryDefaultSeeds -v
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_NATURAL_PREVIEW_GALLERY=1 go test -run TestPreviewGalleryNaturalLayersDefaultSeeds -v
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_RESOURCE_PREVIEW_GALLERY=1 go test -run TestPreviewGalleryResourceLayersDefaultSeeds -v
go test -run '^$' -bench '^BenchmarkFastPreviewCache1024$' -benchmem -benchtime=1x -count=3
FACTMAPGEN_RENDER_SPEED_COMPARISON=fast-only go test -run TestRenderSpeedComparison1024 -count=1 -v
FACTMAPGEN_FACTORIO_BIN=/opt/factorio/bin/x64/factorio FACTMAPGEN_RENDER_SPEED_COMPARISON=1 go test -run TestRenderSpeedComparison1024 -count=1 -v
```

The speed comparison is opt-in and runs one fixed-seed 1024x1024 render per engine. It reports render and PNG wall time, total process CPU time and utilization, and peak resident memory. Use `fast-only` while tuning the Go renderer so Factorio is not launched on every run.

As a representative five-sample local result on a Ryzen 9 7950X, the standard fixed-seed 1024x1024 Fast preview at 1 meter per pixel had medians of 213 ms to render, 236 ms including lossless BestSpeed PNG encoding, 1.71 CPU-seconds, and about 28.3 MiB peak RSS. At the same output size and scale, the tile-cache benchmark measured 233 ms cold, 67 ms for a warm repeat, and 96-98 ms for an adjacent pan; cache timings exclude response-image encoding and are performance snapshots rather than service guarantees.

Diff artifacts are written to `test-output/preview-diffs/` as Factorio, fast-renderer, amplified-diff, and JSON-statistics files. The exact-engine parity test runs automatically when a Factorio binary is discoverable. The fast tests are opt-in and enforce overall terrain and natural-feature correctness budgets rather than requiring exact pixels.

The gallery test writes `test-output/preview-gallery/default-10-seeds-terrain/index.html`, with resource-free Factorio terrain, fast Go terrain, and amplified diff images for ten reproducible pseudo-random default-preset seeds. Existing dimension-compatible Factorio PNGs are reused; subsequent runs regenerate only fast images, diffs, statistics, and HTML.

The natural-layer gallery writes `test-output/preview-gallery/default-10-seeds-trees-cliffs/index.html`, with resources, rocks, enemies, fish, and decorations disabled. Factorio references are content-addressed by their isolated map settings and reused on subsequent runs.

The resource-layer gallery writes `test-output/preview-gallery/default-10-seeds-resources-oil-rocks/index.html`, with trees, enemies, cliffs, fish, and decorations disabled. It includes per-seed ore-region correlation, ore/oil/rock coverage ratios, amplified diffs, and content-addressed Factorio references.

The current ten-seed results and interpretation are documented in
[`docs/render-comparison.md`](docs/render-comparison.md). Generated gallery
artifacts remain local under the ignored `test-output/` directory.

The isolated oil and enemy comparisons each use fixed seed `123456` at 1024x1024 and 1 meter per pixel. They write Factorio, Fast, amplified-diff, and JSON-statistics artifacts beneath `test-output/preview-diffs/oil-seed-123456/` and `test-output/preview-diffs/enemies-seed-123456/`. Factorio references are content-addressed and reused; these artifact directories remain ignored by Git.

## Presets

Built-in presets:

- `default`
- `no-biters`
- `rail-world`
- `death-world`
- `rich-resources`
- `marathon`
- `death-world-marathon`
- `peaceful-rich`
- `lakes`
- `island`
- `ribbon-world`
- `empty-sandbox`
- `marathon-frontier`
- `dense-forest`
- `desert-scarcity`
- `cliffside-lakes`
- `oil-baron`
- `tiny-death-spiral`
- `megabase-plain`
- `waterworld`
- `forest-deathworld`
- `ore-patchwork`
- `archipelago`
- `fragmented-coast`
- `hive-expansion`
- `sparse-rich-desert`
- `island-escape`

The bundled default templates are based on Wube's public `factorio-data` example JSON files. `map-gen-settings.example.json` keeps the public random seed form, `seed: null`.

## API

- `GET /api/config`
- `GET /api/session`
- `POST /api/session` with `{ "username": "admin", "password": "..." }`
- `DELETE /api/session`
- `PUT /api/session/password` with `{ "currentPassword": "...", "newPassword": "..." }`; login required
- `GET /api/users` admin only
- `POST /api/users` admin only, with `{ "username": "...", "password": "...", "isAdmin": true }`
- `PUT /api/users/{id}` admin only
- `DELETE /api/users/{id}` admin only
- `GET /api/audit?limit=200` admin only
- `GET /api/factorio` admin only; returns the configured release channel, cached Factorio binary and version, latest channel release, and install status
- `POST /api/factorio/install` admin only; deletes and reinstalls the managed `-factorio-dir` headless install
- `GET /api/profiles`; public
- `POST /api/profiles` with `{ "name": "...", "preset": "default" }`; login required
- `POST /api/profiles/import-exchange` with `{ "name": "...", "exchangeString": ">>><<<" }`; guests receive a transient `local:` document; logged-in users create a server preset
- `GET /api/profiles/{name}`; public
- `PUT /api/profiles/{name}` with `{ "mapGen": {...}, "mapSettings": {...} }`; login required
- `DELETE /api/profiles/{name}`; login required
- `POST /api/profiles/{name}/rename` with `{ "name": "new name" }`; login required
- `POST /api/profiles/{name}/duplicate` with `{ "name": "copy name" }`; login required
- `GET /api/profiles/{name}/download.zip`; public
- `POST /api/profiles/{name}/preview` with `{ "engine": "fast", "size": 1024, "planet": "nauvis", "zoom": "2.75", "centerX": 256, "centerY": -128, "mapGen": {...} }`; public for Fast previews; Exact previews require sign-in; `engine` may be `fast` for the built-in Go renderer or `factorio` for the Exact Factorio renderer; omitting `engine` selects Fast for guests and selects Exact for signed-in users when Factorio is configured; Exact Factorio jobs are queued; guests are capped at 512 pixels and 1 meter per pixel; all output is lossless PNG; `zoom` is a numeric meters-per-pixel string from `0.015625` through `64`, with legacy `out-N` and `in-N` values accepted; optional numeric `centerX` and `centerY` select the center in world meters, default to `0`, must be between `-1000000000` and `1000000000`, and are normalized to the `zoom` lattice; the response echoes normalized `centerX`, `centerY`, and `tilesPerPixel`; `mapGen` is optional and lets the server preview unsaved edits from a temporary file
- `GET /api/previews/{token}.png`; public, short-lived preview image returned by a preview request

Profile names may contain letters, numbers, spaces, dots, underscores, and hyphens.
