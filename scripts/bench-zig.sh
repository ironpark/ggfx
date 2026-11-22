#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'EOF'
Usage: scripts/bench-zig.sh [count]

Builds the Zig native graphics driver in ReleaseFast mode (unless GGFX_DRIVER_LIB
is already set) and runs Go vs Zig driver benchmarks for:
  1. internal/graphicsdriver/zigdriver (/go vs /zig)
  2. internal/rendertest (Go driver vs Zig driver)

Arguments:
  count   Number of benchmark iterations passed to -count (default: 5).

Environment:
  GGFX_DRIVER_LIB   Skip building zig/ and benchmark the given shared library.
EOF
  exit 0
fi

COUNT="${1:-5}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

case "$(uname -s)" in
  Darwin*)
    LIB_REL="zig/zig-out/lib/libggfx_driver.dylib"
    ;;
  Linux*)
    LIB_REL="zig/zig-out/lib/libggfx_driver.so"
    ;;
  MINGW*|MSYS*|CYGWIN*)
    LIB_REL="zig/zig-out/bin/ggfx_driver.dll"
    ;;
  *)
    echo "Unsupported OS: $(uname -s)" >&2
    exit 1
    ;;
esac

if [[ -n "${GGFX_DRIVER_LIB:-}" ]]; then
  LIB_PATH="$GGFX_DRIVER_LIB"
  echo "==> Using GGFX_DRIVER_LIB=$LIB_PATH"
else
  echo "==> Building Zig driver (ReleaseFast)..."
  (cd "$ROOT_DIR/zig" && zig build -Doptimize=ReleaseFast)
  LIB_PATH="$ROOT_DIR/$LIB_REL"
  if command -v cygpath >/dev/null 2>&1; then
    LIB_PATH="$(cygpath -w "$LIB_PATH")"
  fi
fi

if [[ ! -f "$LIB_PATH" ]]; then
  echo "error: Zig driver library not found at $LIB_PATH" >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

cd "$ROOT_DIR"

echo ""
echo "==> Running internal/graphicsdriver/zigdriver benchmarks (-count=$COUNT)..."
GGFX_DRIVER_LIB="$LIB_PATH" \
  go test -bench=. -benchmem -count="$COUNT" -run='^$' ./internal/graphicsdriver/zigdriver \
  | tee "$TMP_DIR/zigdriver.txt"

echo ""
echo "==> Running internal/rendertest benchmarks (Go driver, -count=$COUNT)..."
GGFX_DRIVER=go \
  go test -bench=. -benchmem -count="$COUNT" -run='^$' ./internal/rendertest \
  | tee "$TMP_DIR/rendertest_go.txt"

echo ""
echo "==> Running internal/rendertest benchmarks (Zig driver, -count=$COUNT)..."
GGFX_DRIVER=zig GGFX_DRIVER_STRICT=1 GGFX_DRIVER_LIB="$LIB_PATH" \
  go test -bench=. -benchmem -count="$COUNT" -run='^$' ./internal/rendertest \
  | tee "$TMP_DIR/rendertest_zig.txt"

if command -v benchstat >/dev/null 2>&1; then
  echo ""
  echo "==> benchstat: internal/graphicsdriver/zigdriver (go vs zig)"
  awk '
    /^(goos|goarch|pkg|cpu):/ { print; next }
    /^Benchmark[^[:space:]]*\/go(-[0-9]+)?([[:space:]]|$)/ {
      if (match($0, /\/go(-[0-9]+)?([[:space:]]|$)/)) {
        $0 = substr($0, 1, RSTART - 1) substr($0, RSTART + 3)
      }
      print
    }
  ' "$TMP_DIR/zigdriver.txt" > "$TMP_DIR/zigdriver_go.txt"
  awk '
    /^(goos|goarch|pkg|cpu):/ { print; next }
    /^Benchmark[^[:space:]]*\/zig(-[0-9]+)?([[:space:]]|$)/ {
      if (match($0, /\/zig(-[0-9]+)?([[:space:]]|$)/)) {
        $0 = substr($0, 1, RSTART - 1) substr($0, RSTART + 4)
      }
      print
    }
  ' "$TMP_DIR/zigdriver.txt" > "$TMP_DIR/zigdriver_zig.txt"
  if grep -q '^Benchmark' "$TMP_DIR/zigdriver_go.txt" && grep -q '^Benchmark' "$TMP_DIR/zigdriver_zig.txt"; then
    benchstat "$TMP_DIR/zigdriver_go.txt" "$TMP_DIR/zigdriver_zig.txt"
  else
    echo "(skipped: Go baseline sub-benchmarks unavailable on this platform)"
  fi

  if grep -q '^Benchmark' "$TMP_DIR/rendertest_go.txt" && grep -q '^Benchmark' "$TMP_DIR/rendertest_zig.txt"; then
    echo ""
    echo "==> benchstat: internal/rendertest (go vs zig)"
    benchstat "$TMP_DIR/rendertest_go.txt" "$TMP_DIR/rendertest_zig.txt"
  fi
else
  echo ""
  echo "Tip: install benchstat for statistical summaries:"
  echo "  go install golang.org/x/perf/cmd/benchstat@latest"
fi
