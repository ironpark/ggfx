#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Usage: scripts/test-zig-vulkan-docker.sh [go test binary flags]

Runs the internal/graphicsdriver/zigdriver tests against the Zig driver's Vulkan
backend, in a Linux container with Mesa's lavapipe (software Vulkan). For hosts
that cannot run the Vulkan backend themselves, such as macOS.

The driver and the test binary are cross-compiled on the host for the
architecture Docker runs, so the container needs neither Zig nor Go.

lavapipe has no window system here, so presenting to a surface is not covered,
and timings say nothing about a real GPU.

Arguments are passed to the test binary, for example:
  scripts/test-zig-vulkan-docker.sh -test.v -test.run TestDriverConformance
  scripts/test-zig-vulkan-docker.sh -test.run '^$' -test.bench WritePixels

Environment:
  GGFX_LAVAPIPE_IMAGE   Image to run in (default: ggfx-lavapipe:local, built on first use).
USAGE
  exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
IMAGE="${GGFX_LAVAPIPE_IMAGE:-ggfx-lavapipe:local}"

case "$(docker info --format '{{.Architecture}}')" in
  aarch64|arm64)
    ZIG_ARCH=aarch64
    GO_ARCH=arm64
    ;;
  x86_64|amd64)
    ZIG_ARCH=x86_64
    GO_ARCH=amd64
    ;;
  *)
    echo "Unsupported Docker architecture: $(docker info --format '{{.Architecture}}')" >&2
    exit 1
    ;;
esac

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "==> Building $IMAGE..."
  docker build -t "$IMAGE" - <<'DOCKERFILE'
FROM ubuntu:24.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends libvulkan1 mesa-vulkan-drivers \
 && rm -rf /var/lib/apt/lists/*
DOCKERFILE
fi

# Under zig-out, which is ignored, and inside the one directory the container mounts.
OUT_REL="zig/zig-out/lavapipe"

echo "==> Building Zig driver ($ZIG_ARCH-linux-gnu, Debug)..."
# No allocation stack traces: Zig's unwinder panics and hangs in some sandboxed
# container runtimes (see zig/src/alloc.zig). Safety checks and leak detection stay on.
(cd "$ROOT_DIR/zig" && zig build -Dtarget="$ZIG_ARCH-linux-gnu" -Doptimize=Debug \
  -Dalloc-trace-frames=0 --prefix "$ROOT_DIR/$OUT_REL")

echo "==> Building zigdriver tests (linux/$GO_ARCH)..."
(cd "$ROOT_DIR" && CGO_ENABLED=0 GOOS=linux GOARCH="$GO_ARCH" \
  go test -c -o "$OUT_REL/zigdriver.test" ./internal/graphicsdriver/zigdriver)

echo "==> Running on lavapipe..."
# GGFX_DRIVER_STRICT makes an unusable Vulkan device a failure instead of a skip.
docker run --rm \
  -v "$ROOT_DIR":/src:ro \
  -w /src/internal/graphicsdriver/zigdriver \
  -e GGFX_DRIVER=zig \
  -e GGFX_DRIVER_STRICT=1 \
  -e GGFX_DRIVER_LIB="/src/$OUT_REL/lib/libggfx_driver.so" \
  "$IMAGE" \
  timeout -s KILL 600 "/src/$OUT_REL/zigdriver.test" -test.shuffle=on "$@"
