# FactMapGen

FactMapGen is a small Go web tool for creating and editing Factorio `map-gen-settings.json` and `map-settings.json` files. Map presets are stored on the server as folders; the browser edits those server-side files directly.

## Run

```sh
go run . -addr :8080 -presets presets
```

Open `http://localhost:8080`.

The `-data` flag still works as a backwards-compatible alias for `-presets`.

## Build

```sh
go build -o factmapgen .
./factmapgen -addr :8080 -presets presets
```

## Server Files

Each preset is saved as:

```text
presets/
  Preset Name/
    map-gen-settings.json
    map-settings.json
```

The UI focuses on the visual map generator. Keys not exposed visually are preserved when saving, as long as the preset already contains them.

In `map-gen-settings.json`, `seed: 0` means Factorio should choose a random seed each time a new map is generated. Use a positive seed value when you want repeatable generation.

## Map Previews

FactMapGen can generate real Factorio map preview PNGs when a Factorio/headless binary is available. Install the Linux headless package into the default discovery path with:

```sh
./scripts/install-factorio-headless.sh
```

Then restart FactMapGen:

```sh
go run . -addr :8080 -presets presets
```

The server auto-discovers `tools/factorio/bin/x64/factorio`. You can also point at another install:

```sh
go run . -factorio-bin /opt/factorio/bin/x64/factorio
```

Generated previews are stored under:

```text
previews/
  Preset Name/
    preview.png
```

## Presets

Built-in presets:

- `default`
- `rail-world`
- `death-world`
- `peaceful-rich`
- `island`
- `ribbon-world`
- `empty-sandbox`

The bundled default templates are based on Wube's public `factorio-data` example JSON files.

## API

- `GET /api/config`
- `GET /api/profiles`
- `POST /api/profiles` with `{ "name": "...", "preset": "default" }`
- `GET /api/profiles/{name}`
- `PUT /api/profiles/{name}` with `{ "mapGen": {...}, "mapSettings": {...} }`
- `DELETE /api/profiles/{name}`
- `POST /api/profiles/{name}/duplicate` with `{ "name": "copy name" }`
- `GET /api/profiles/{name}/download.zip`
- `POST /api/profiles/{name}/preview` with `{ "size": 768, "planet": "nauvis" }`
- `GET /api/profiles/{name}/preview.png`

Profile names may contain letters, numbers, spaces, dots, underscores, and hyphens.
