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

All preset APIs require a logged-in user. Admin users can add, edit, and delete accounts from the Admin panel in the browser. Passwords are stored as PBKDF2-SHA256 hashes in SQLite, and sessions use HttpOnly cookies.

Preset create, update, delete, duplicate, and preview actions write audit log entries with the acting user, action, target, detail, and timestamp. Account changes are audited too.

## Server Files

Each preset is saved as:

```text
presets/
  Preset Name/
    map-gen-settings.json
    map-settings.json
```

The UI focuses on the visual map generator. Keys not exposed visually are preserved when saving, as long as the preset already contains them.

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

Generated previews are stored under:

```text
previews/
  Preset Name/
    preview.png
```

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
- `GET /api/profiles`
- `POST /api/profiles` with `{ "name": "...", "preset": "default" }`
- `GET /api/profiles/{name}`
- `PUT /api/profiles/{name}` with `{ "mapGen": {...}, "mapSettings": {...} }`
- `DELETE /api/profiles/{name}`
- `POST /api/profiles/{name}/duplicate` with `{ "name": "copy name" }`
- `GET /api/profiles/{name}/download.zip`
- `POST /api/profiles/{name}/preview` with `{ "size": 768, "planet": "nauvis", "mapGen": {...} }`; `mapGen` is optional and lets the server preview unsaved edits from a temporary file
- `GET /api/profiles/{name}/preview.png`

Profile names may contain letters, numbers, spaces, dots, underscores, and hyphens.
