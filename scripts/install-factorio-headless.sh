#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_DIR="${1:-"$ROOT_DIR/tools/factorio"}"
RELEASE_CHANNEL="${2:-${FACTORIO_RELEASE_CHANNEL:-experimental}}"
case "$RELEASE_CHANNEL" in
  stable|experimental) ;;
  *)
    echo "release channel must be stable or experimental" >&2
    exit 2
    ;;
esac
DOWNLOAD_URL="${FACTORIO_DOWNLOAD_URL:-https://factorio.com/get-download/$RELEASE_CHANNEL/headless/linux64}"
ARCHIVE_PATH="$INSTALL_DIR/factorio_headless.tar.xz"

command -v curl >/dev/null 2>&1 || {
  echo "curl is required" >&2
  exit 1
}

command -v tar >/dev/null 2>&1 || {
  echo "tar is required" >&2
  exit 1
}

mkdir -p "$INSTALL_DIR"

echo "Downloading Factorio headless from:"
echo "  $DOWNLOAD_URL"
curl -fL "$DOWNLOAD_URL" -o "$ARCHIVE_PATH"

echo "Extracting to:"
echo "  $INSTALL_DIR"
tar -xf "$ARCHIVE_PATH" --strip-components=1 -C "$INSTALL_DIR"
rm -f "$ARCHIVE_PATH"

BIN="$INSTALL_DIR/bin/x64/factorio"
if [[ ! -x "$BIN" ]]; then
  echo "Factorio binary was not found at $BIN" >&2
  exit 1
fi

echo "Installed Factorio headless:"
"$BIN" --version | head -n 1
echo
echo "Start FactMapGen with:"
echo "  go run . -presets presets"
