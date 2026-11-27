#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Usage: scripts/test-zig-vulkan-docker.sh [--validate] [go test binary flags]

Runs the internal/graphicsdriver/zigdriver tests against the Zig driver's Vulkan
backend, in a Linux container with Mesa's lavapipe (software Vulkan). For hosts
that cannot run the Vulkan backend themselves, such as macOS.

The driver and the test binary are cross-compiled on the host for the
architecture Docker runs, so the container needs neither Zig nor Go.

lavapipe has no window system here, so presenting to a surface is not covered,
and timings say nothing about a real GPU.

Options:
  --validate            Install/enable VK_LAYER_KHRONOS_validation with
                        synchronization validation and fail if any validation
                        message is emitted (also enabled by GGFX_VULKAN_VALIDATE=1).

Arguments are passed to the test binary, for example:
  scripts/test-zig-vulkan-docker.sh -test.v -test.run TestDriverConformance
  scripts/test-zig-vulkan-docker.sh --validate
  scripts/test-zig-vulkan-docker.sh -test.run '^$' -test.bench WritePixels

Environment:
  GGFX_LAVAPIPE_IMAGE   Image to run in (default: ggfx-lavapipe:local, or
                        ggfx-lavapipe-validation:local with --validate).
  GGFX_VULKAN_VALIDATE  Set to 1 to enable Vulkan validation + sync validation.
  ZIG                   Zig binary to use (default: zig on PATH).
USAGE
  exit 0
fi

VALIDATE="${GGFX_VULKAN_VALIDATE:-0}"
if [[ "${1:-}" == "--validate" ]]; then
  VALIDATE=1
  shift
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
ZIG_BIN="${ZIG:-zig}"
if [[ "$VALIDATE" == "1" ]]; then
  IMAGE="${GGFX_LAVAPIPE_IMAGE:-ggfx-lavapipe-validation:local}"
else
  IMAGE="${GGFX_LAVAPIPE_IMAGE:-ggfx-lavapipe:local}"
fi

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
  if [[ "$VALIDATE" == "1" ]]; then
    docker build -t "$IMAGE" - <<'DOCKERFILE'
FROM ubuntu:24.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends libvulkan1 mesa-vulkan-drivers vulkan-validationlayers \
 && rm -rf /var/lib/apt/lists/*
DOCKERFILE
  else
    docker build -t "$IMAGE" - <<'DOCKERFILE'
FROM ubuntu:24.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends libvulkan1 mesa-vulkan-drivers \
 && rm -rf /var/lib/apt/lists/*
DOCKERFILE
  fi
fi

# Under zig-out, which is ignored, and inside the one directory the container mounts.
OUT_REL="zig/zig-out/lavapipe"

echo "==> Building Zig driver ($ZIG_ARCH-linux-gnu, Debug)..."
# No allocation stack traces: Zig's unwinder panics and hangs in some sandboxed
# container runtimes (see zig/src/alloc.zig). Safety checks and leak detection stay on.
(cd "$ROOT_DIR/zig" && "$ZIG_BIN" build -Dtarget="$ZIG_ARCH-linux-gnu" -Doptimize=Debug \
  -Dalloc-trace-frames=0 --prefix "$ROOT_DIR/$OUT_REL")

echo "==> Building zigdriver tests (linux/$GO_ARCH)..."
(cd "$ROOT_DIR" && CGO_ENABLED=0 GOOS=linux GOARCH="$GO_ARCH" \
  go test -c -o "$OUT_REL/zigdriver.test" ./internal/graphicsdriver/zigdriver)

DOCKER_ARGS=(
  --rm
  -v "$ROOT_DIR":/src:ro
  -w /src/internal/graphicsdriver/zigdriver
  -e GGFX_DRIVER=zig
  -e GGFX_DRIVER_STRICT=1
  -e GGFX_DRIVER_LIB="/src/$OUT_REL/lib/libggfx_driver.so"
)

if [[ "$VALIDATE" == "1" ]]; then
  echo "==> Running on lavapipe with VK_LAYER_KHRONOS_validation + synchronization validation..."
  # Ensure the image actually contains VK_LAYER_KHRONOS_validation so --validate never passes vacuously.
  if ! docker run --rm "$IMAGE" test -f /usr/share/vulkan/explicit_layer.d/VkLayer_khronos_validation.json; then
    echo "FAIL: VK_LAYER_KHRONOS_validation manifest not found in $IMAGE" >&2
    exit 1
  fi
  DOCKER_ARGS+=(
    -e VK_INSTANCE_LAYERS=VK_LAYER_KHRONOS_validation
    -e VK_LAYER_ENABLES=VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT
    -e VK_LAYER_REPORT_FLAGS=error,warn
  )
  LOG_FILE="$(mktemp)"
  trap 'rm -f "$LOG_FILE"' EXIT

  set +e
  docker run "${DOCKER_ARGS[@]}" \
    "$IMAGE" \
    timeout -s KILL 600 "/src/$OUT_REL/zigdriver.test" -test.shuffle=on "$@" 2>&1 | tee "$LOG_FILE"
  TEST_STATUS="${PIPESTATUS[0]}"
  set -e

  VAL_COUNT="$(grep -cE 'Validation (Error|Warning):|VUID-|SYNC-|UNASSIGNED-' "$LOG_FILE" || true)"
  echo "==> Vulkan validation messages: $VAL_COUNT"
  if [[ "$TEST_STATUS" -ne 0 ]]; then
    exit "$TEST_STATUS"
  fi
  if [[ "$VAL_COUNT" -ne 0 ]]; then
    echo "FAIL: Vulkan validation reported $VAL_COUNT message(s)" >&2
    exit 1
  fi
else
  echo "==> Running on lavapipe..."
  # GGFX_DRIVER_STRICT makes an unusable Vulkan device a failure instead of a skip.
  docker run "${DOCKER_ARGS[@]}" \
    "$IMAGE" \
    timeout -s KILL 600 "/src/$OUT_REL/zigdriver.test" -test.shuffle=on "$@"
fi
