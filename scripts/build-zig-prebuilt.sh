#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Usage: scripts/build-zig-prebuilt.sh [--dist DIR] [--all | GOOS/GOARCH ...]

Builds the Zig driver (ReleaseFast, stripped) and writes it gzip-compressed to
internal/graphicsdriver/zigdriver/prebuilt/<goos>_<goarch>/, which is where a
build with `-tags ggfxzig` embeds it from.

With no platform, the driver is built for the host (go env GOOS/GOARCH).
--all builds every platform the driver supports; Zig cross-compiles all of them
from any host.

  --dist DIR   Also copy the uncompressed libraries to DIR/<goos>_<goarch>/.

Examples:
  scripts/build-zig-prebuilt.sh
  scripts/build-zig-prebuilt.sh linux/amd64 windows/arm64
  scripts/build-zig-prebuilt.sh --all --dist dist
USAGE
  exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
PREBUILT_DIR="$ROOT_DIR/internal/graphicsdriver/zigdriver/prebuilt"
ALL_PLATFORMS=(darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64)

DIST_DIR=""
PLATFORMS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --all)
      PLATFORMS=("${ALL_PLATFORMS[@]}")
      ;;
    --dist)
      [[ $# -ge 2 ]] || { echo "error: --dist needs a directory" >&2; exit 2; }
      mkdir -p "$2"
      DIST_DIR="$(cd "$2" && pwd)"
      shift
      ;;
    */*)
      PLATFORMS+=("$1")
      ;;
    *)
      echo "error: unknown argument: $1 (see --help)" >&2
      exit 2
      ;;
  esac
  shift
done
if [[ ${#PLATFORMS[@]} -eq 0 ]]; then
  PLATFORMS=("$(go env GOOS)/$(go env GOARCH)")
fi

OUT_DIR="$(mktemp -d)"
trap 'rm -rf "$OUT_DIR"' EXIT

for platform in "${PLATFORMS[@]}"; do
  goos="${platform%/*}"
  goarch="${platform#*/}"

  case "$goarch" in
    amd64) zig_arch=x86_64 ;;
    arm64) zig_arch=aarch64 ;;
    *) echo "error: unsupported GOARCH: $goarch" >&2; exit 2 ;;
  esac
  case "$goos" in
    darwin)  zig_target="$zig_arch-macos";       built="lib/libggfx_driver.dylib" ;;
    linux)   zig_target="$zig_arch-linux-gnu";   built="lib/libggfx_driver.so" ;;
    windows) zig_target="$zig_arch-windows-gnu"; built="bin/ggfx_driver.dll" ;;
    *) echo "error: unsupported GOOS: $goos" >&2; exit 2 ;;
  esac
  name="$(basename "$built")"

  echo "==> $goos/$goarch ($zig_target)"
  (cd "$ROOT_DIR/zig" && zig build \
    -Dtarget="$zig_target" -Doptimize=ReleaseFast -Dstrip=true \
    --prefix "$OUT_DIR/$goos-$goarch")

  dst="$PREBUILT_DIR/${goos}_${goarch}"
  mkdir -p "$dst"
  # -n leaves the name and the time out of the header, so the same library gives the same bytes.
  gzip -9 -n -c "$OUT_DIR/$goos-$goarch/$built" > "$dst/$name.gz"

  if [[ -n "$DIST_DIR" ]]; then
    mkdir -p "$DIST_DIR/${goos}_${goarch}"
    cp "$OUT_DIR/$goos-$goarch/$built" "$DIST_DIR/${goos}_${goarch}/$name"
  fi

  echo "    $(wc -c < "$OUT_DIR/$goos-$goarch/$built" | tr -d ' ') bytes, $(wc -c < "$dst/$name.gz" | tr -d ' ') compressed"
done
