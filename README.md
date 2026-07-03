# FactMapGen

FactMapGen is a small Go web tool for creating and editing Factorio `map-gen-settings.json` and `map-settings.json` files. Map presets are stored on the server as folders; the browser edits those server-side files directly.

## Run

```sh
go run . -addr :8080 -presets presets
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

Guests can browse presets, inspect settings, generate previews, and download preset ZIP files without logging in. Creating, saving, duplicating, and deleting presets require a logged-in user. Admin users can add, edit, and delete accounts from the Admin panel in the browser. Passwords are stored as PBKDF2-SHA256 hashes in SQLite, and sessions use HttpOnly cookies.

Preset create, update, delete, and duplicate actions write audit log entries with the acting user, action, target, detail, and timestamp. Account changes are audited too.

## Server Files

Each preset is saved as:

```text
presets/
  Preset Name/
    map-gen-settings.json
    map-settings.json
```

The UI focuses on the visual map generator. Frequently tuned settings use sliders, toggles, selects, and named per-setting presets for world size, terrain shape, cliffs, evolution, expansion, pollution, pathfinding pressure, and resources. New presets are created from a modal dialog so the main toolbar stays focused on editing. The built-in per-setting preset buttons are derived from Factorio's bundled `map-gen-presets.lua` and `autoplace-controls.lua` data where those values can be represented directly. Space Age-only autoplace controls are kept in a separate Space Age tab by planet, based on the installed `space-age/prototypes/autoplace-controls.lua` and planet map-gen data. Keys not exposed visually are preserved when saving, as long as the preset already contains them.

In `map-gen-settings.json`, `seed: null` follows Factorio's public example file and is the default. It tells Factorio to choose a unique randomized map seed each time a new map is generated. The editor also treats older `seed: 0` presets as random. Use a positive seed value only when you want repeatable generation.

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

The main web page shows the detected Factorio binary version and flags when the latest stable headless release is newer. Version and latest-release checks are cached by the server. Admin users can refresh Factorio status from the Admin panel and, when the active binary comes from `-factorio-dir`, delete that managed install and install a fresh stable headless copy. The preview surface selector offers the known Factorio planets: Nauvis plus the Space Age planets Vulcanus, Gleba, Fulgora, and Aquilo.

Preview generation is queued server-side so only one Factorio process runs at a time. The queue defaults to 8 waiting jobs and can be changed with `-preview-queue`. Each preview render is capped at 60 seconds by default and can be changed with `-preview-timeout`. Admin preview jobs are served before regular signed-in users, and signed-in users are served before guests; jobs with the same priority run in request order. If the queue is full, the preview request returns HTTP 429.

Generated preview images are transient. Factorio writes a temporary PNG while a queued job runs, the server applies the requested zoom transform, converts it to AVIF with JPEG fallback unless `lossless` is requested, and the browser receives a short-lived `/api/previews/...` URL. Preview images are kept in memory for up to 30 minutes with a 100-image cap, and are not cached per preset.

## Presets

Built-in presets:

- `default`
- `no-biters`
- `rail-world`
- `death-world`
- `peaceful-rich`
- `island`
- `ribbon-world`
- `empty-sandbox`
- `marathon-frontier`
- `dense-forest`
- `desert-scarcity`
- `cliffside-lakes`
- `oil-baron`
- `tiny-death-spiral`
The bundled default templates are based on Wube's public `factorio-data` example JSON files. `map-gen-settings.example.json` keeps the public random seed form, `seed: null`.

## API

- `GET /api/config`
- `GET /api/session`
- `POST /api/session` with `{ "username": "admin", "password": "..." }`
- `DELETE /api/session`
- `GET /api/users` admin only
- `POST /api/users` admin only, with `{ "username": "...", "password": "...", "isAdmin": true }`
- `PUT /api/users/{id}` admin only
- `DELETE /api/users/{id}` admin only
- `GET /api/audit?limit=200` admin only
- `GET /api/factorio` admin only; returns cached Factorio binary, version, latest stable release, and install status
- `POST /api/factorio/install` admin only; deletes and reinstalls the managed `-factorio-dir` headless install
- `GET /api/profiles`; public
- `POST /api/profiles` with `{ "name": "...", "preset": "default" }`; login required
- `GET /api/profiles/{name}`; public
- `PUT /api/profiles/{name}` with `{ "mapGen": {...}, "mapSettings": {...} }`; login required
- `DELETE /api/profiles/{name}`; login required
- `POST /api/profiles/{name}/duplicate` with `{ "name": "copy name" }`; login required
- `GET /api/profiles/{name}/download.zip`; public
- `POST /api/profiles/{name}/preview` with `{ "size": 768, "planet": "nauvis", "zoom": "1", "lossless": false, "mapGen": {...} }`; public and queued; `zoom` is map scale: `out-4`, `out-3`, and `out-2` show 4, 3, or 2 meters per output pixel; `1` is normal 1 meter per pixel; `in-2`, `in-3`, and `in-4` crop the center to 1/2, 1/3, or 1/4 meter per output pixel with integer scaling; `mapGen` is optional and lets the server preview unsaved edits from a temporary file
- `GET /api/previews/{token}.avif`, `.jpg`, or `.png`; public, short-lived preview image returned by a preview request

Profile names may contain letters, numbers, spaces, dots, underscores, and hyphens.
