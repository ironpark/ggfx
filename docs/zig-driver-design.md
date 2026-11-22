# Zig `graphicsdriver` Backend Design (Plan A)

> Status: **Phase 1 + Phase 2 implemented** (opt-in via `GGFX_DRIVER=zig`, `GGFX_DRIVER_LIB`, or `GGFX_GRAPHICS_LIBRARY=zig|vulkan`). Go remains the default driver when unconfigured; browser (`js/wasm`) stays pure-Go WebGL.

## 1. Motivation & Scope

`ggfx` currently implements its GPU backends (`metal`, `directx` 11/12, `opengl`) in Go via `ebitengine/purego` and Win32 syscalls. While this avoids `cgo`, it carries significant complexity:

- Manual ABI workarounds for passing C/ObjC structs by value (e.g. `internal/graphicsdriver/metal/mtl/structarg_darwin_arm64.go`) and COM vtable bindings (`internal/graphicsdriver/directx/`).
- Dozens to hundreds of per-frame `purego` / syscall boundary crossings inside the driver hot path.
- High effort to add modern desktop backends (such as Vulkan on Linux, replacing OpenGL/GLFW quirks) or maintain native `PixelFormatA8` support uniformly.

**Plan A** moves **only the `internal/graphicsdriver` execution layer** into a Zig 0.17.1 C-ABI shared library (`libggfx_driver.{dylib,so,dll}`), loaded from pure Go via `purego`:

| Layer | Language | Responsibility |
|---|---|---|
| `ggfx`, `host`, `vector`, `svg` | Go | Public API, geometry, vector tessellation, CPU SVG rasterizer |
| `internal/render`, `internal/atlas`, `internal/graphicscommand` | Go | Frame lifecycle, pixel cache, mipmaps, texture atlas packing, draw-call merging, vertex/index/uniform batching |
| `internal/shader` (`gogpu/naga`) | Go | WGSL parsing, validation, uniform reflection, target codegen (MSL/HLSL/GLSL/SPIR-V) |
| `internal/graphicsdriver/zigdriver` | Go (thin adapter) | Pure-Go `purego` loader, command-buffer encoder, handle mapping, sync round-trips |
| `libggfx_driver` (Metal / DX12 / Vulkan / GL) | **Zig 0.17.1** | Shared-memory command-buffer replay, GPU resource & pipeline management, swap chains & presentation |
| `internal/graphicsdriver/opengl` (WebGL) | Go (`js/wasm`) | Browser target stays pure-Go behind the same `graphicsdriver.Graphics` interface |

Keeping `atlas`, `graphicscommand`, `naga`, and `js/wasm` WebGL in Go lets the Zig backend drop in behind `graphicsdriver.Graphics` incrementally, with zero disruption to public APIs or WebAssembly builds.

---

## 2. Prerequisites (Go-Side Refactoring Before Implementation)

Before introducing the Zig driver, clean up the current `graphicsdriver` boundary:

1. **Consolidate side-channel type assertions** *(done in `internal/graphicsdriver/capabilities.go`)*: Optional `Graphics` and `Surface` capability interfaces (`MainThreadRunner`, `GLContextSetter`, `ESReporter`, `DirectCompositionReporter`, `OcclusionReporter`, `ForcedFrameFinisher`, `FramebufferSizer`, `Closer`, `KeptSurface`, `DamagedSurface`, `WindowStateSurface`, `ReadableSurface`) are now named and documented in `internal/graphicsdriver`.
2. **Add `Graphics.Close()`** *(done via `graphicsdriver.Closer`)*: `render.Terminate()` invokes `Close()` after draining the render thread so native/Zig backends can release GPU handles cleanly.
3. **Shared driver conformance suite** *(done in `internal/graphicsdriver/drivertest`)*: Reusable contract tests verify pixel round-trips (RGBA8 and A8), shader drawing, blend modes, scissor clipping, and uniform version caching against any `graphicsdriver.Graphics`.
4. **Decouple `NewShader` from Go `*naga/ir.Module`** *(done via `graphicsdriver.ShaderDescriptor` + `ShaderSource`)*: `NewShader` accepts backend-ready compiled stage payloads (MSL/HLSL/GLSL/SPIR-V source or bytecode + reflection metadata: `TextureCount`, `UniformDwordCount`, `FragmentReadsInternalUniforms`) so `internal/graphicsdriver/zigdriver` has zero dependency on `naga`.
5. **Typed native surface descriptors** *(done via `graphicsdriver.SurfaceDescriptor`)*: `NewSurface` takes a typed descriptor supporting `NSView`, `HWND`, X11 (`Display`, `Window`), Wayland (`Display`, `Surface`), WebGL `Canvas`, and `RemoteTarget`.
6. **Centralize `InternalImageSize` rounding & `InternalUniforms` POD layout** *(POD layout compile-time assertions added in `internal/graphicsdriver/capabilities.go`)*.
7. **Enforce strict thread-confinement contracts** *(driver races on `surfaces`, `damage`, and `Surface.Dispose` fixed in Wave 2)*.

---

## 3. Command Transport: Frame-Slot Ring of Linear Arenas

Instead of a raw byte-stream ring buffer (which suffers from wrap-around splitting of contiguous vertex/pixel arrays and mid-frame overflow/deadlock hazards), use a **2- or 3-slot frame ring where each slot owns growable linear arenas**:

```
Go (Producer)                                Zig (Consumer / GPU Driver)
┌──────────────────────────────────┐         ┌──────────────────────────────┐
│ Frame Slot Ring [0..N-1] (N=2|3) │         │                              │
│  Slot k:                         │ 1 FFI   │ ggfx_submit(dev, &batchDesc) │
│   ├─ cmds[]    (aligned opcodes) │ ──────► │   linear walk of cmds[]      │
│   ├─ vertices  ([]float32)       │  call/  │   zero-copy upload to GPU    │
│   ├─ indices   ([]uint32)        │  flush  │   state-cached draw loop     │
│   └─ payload[] (16B-aligned:     │         │   present damaged surfaces   │
│        pixels, uniforms, blobs)  │         │                              │
└──────────────────────────────────┘         └──────────────────────────────┘
```

### Why this layout works best for `ggfx`

- **Zero wrap-around copies**: Every vertex array, index array, uniform block, and `WritePixels` rect is guaranteed contiguous in memory, allowing direct `memcpy` into GPU staging/upload rings.
- **Arbitrary spike sizes**: Large frames (e.g., 4K texture uploads or high-density glyph batches) grow the slot's linear buffer cleanly and shrink back via `recent.Peak` across frames, avoiding fixed-ring overflow deadlocks.
- **1 FFI call per flush**: `commandQueue` already batches commands per frame (`BeginFrame` → record → `Flush`) and `FlushModePresent` is synchronous to prevent vsync latency. While `ggfx_submit` executes synchronously, Go can even pass the `commandQueue`'s existing `vertices` and `indices` pointers directly without copying them into a second buffer!
- **Reuse of `internal/graphicsdriver/remote` vocabulary**: The command stream closely mirrors `remote`'s proven opcodes (`SetVertices`, `NewImage`, `NewScreenImage`, `DisposeImage`, `NewShader`, `DisposeShader`, `WritePixels`, `DrawTriangles`, `SetDamage`, `Present`), minus cross-process untrusted validation overhead and with native-endian fixed-alignment POD structs.

### Wire / Memory Layout Rules (C ABI)

- All integers are native little-endian; all structs are Zig `extern struct` verified with comptime `@sizeOf` / `@offsetOf` assertions and matching Go compile-time size checks.
- **Opaque `u32` handle IDs** (`0` = none/null) allocated monotonically by Go for Images, Shaders, and Surfaces. Zig stores native GPU objects in dense slot tables indexed/hashed by `u32`. **No Go pointers are ever retained by Zig across calls.**
- Command records in `cmds[]` are 8-byte aligned:

```c
typedef struct {
    uint8_t  op;       // GgfxOp enum
    uint8_t  flags;
    uint16_t aux;      // e.g. dst_region_count, write_count, pixel_format
    uint32_t size;     // total record byte size (multiple of 8)
} GgfxCmdHeader;
```

Supported opcodes in `cmds[]`:
1. `OP_SET_VERTICES` — offset/count into `vertices` and `indices` streams
2. `OP_NEW_IMAGE` — `id`, `width`, `height`, `internal_width`, `internal_height`, `pixel_format`
3. `OP_NEW_SCREEN_IMAGE` — `id`, `surface_id`, `width`, `height`
4. `OP_DISPOSE_IMAGE` — `id`
5. `OP_DISPOSE_SHADER` — `id`
6. `OP_DISPOSE_SURFACE` — `surface_id` (ordered cleanly after screen image disposal)
7. `OP_WRITE_PIXELS` — `image_id`, `count`, followed by `count × { rect_i32[4], payload_offset, payload_len }`
8. `OP_DRAW_TRIANGLES` — `dst_id`, `src_ids[4]`, `shader_id`, `index_offset`, `blend[6]`, `internal_ver_u64`, `user_ver_u64`, `uniforms_offset`, `uniforms_dwords`, `region_count`, followed by `region_count × { rect_i32[4], index_count }`
9. `OP_SET_DAMAGE` — `surface_id`, `count` (`0xFFFFFFFF` = full), followed by `count × rect_i32[4]`

---

## 4. C ABI Sketch (`ggfx_driver.h` / Zig `export fn`)

Every exported Zig function returns a `GgfxStatus` (`0 = OK`, non-zero error code) and writes human-readable diagnostics into a caller-supplied error buffer (`char* err_buf, size_t err_cap`), so no heap-allocated error strings cross the ABI.

```c
#define GGFX_DRIVER_ABI_VERSION 1

uint32_t ggfx_abi_version(void);

// Lifecycle (main thread)
GgfxStatus ggfx_device_create(
    const GgfxDeviceOptions* opts,   // backend preference, color space, debug/validation flags
    GgfxDevice** out_dev,
    GgfxDriverCaps* out_caps,        // max_image_size, flips_y, supports_a8, needs_clear, direct_comp, backend_kind
    char* err_buf, size_t err_cap);

void ggfx_device_destroy(GgfxDevice* dev);

// Hot path (render thread): 1 call per Flush
GgfxStatus ggfx_submit(
    GgfxDevice* dev,
    const GgfxBatch* batch,          // ptr+len for cmds, vertices, indices, payload
    uint32_t flush_mode,             // 0=Intermediate, 1=EndFrame, 2=Present
    GgfxSubmitResult* out_res,       // occluded flag, etc.
    char* err_buf, size_t err_cap);

// Synchronous round-trips (render thread)
GgfxStatus ggfx_create_shader(
    GgfxDevice* dev,
    uint32_t shader_id,
    const GgfxShaderDesc* desc,      // stage target format, source/bytecode ptr+len, texture_count, uniform_dwords, flags
    char* err_buf, size_t err_cap);

GgfxStatus ggfx_read_pixels(
    GgfxDevice* dev,
    const GgfxBatch* pending_batch,  // optional commands to flush before readback
    uint32_t image_id,
    const GgfxReadRegion* regions,   // rect_i32[4], dst_ptr, dst_len
    uint32_t region_count,
    char* err_buf, size_t err_cap);

GgfxStatus ggfx_finish_forced_frame(
    GgfxDevice* dev,
    char* err_buf, size_t err_cap);

// Surface control (main thread; thread-safe against render thread inside Zig)
GgfxStatus ggfx_surface_create(
    GgfxDevice* dev,
    uint32_t surface_id,
    const GgfxSurfaceDesc* desc,     // tagged union: NSView / HWND / X11 / Wayland; transparent flag
    GgfxSurfaceCaps* out_caps,       // flips_y, readable, kept_supported
    char* err_buf, size_t err_cap);

void ggfx_surface_set_window_state(
    GgfxDevice* dev,
    uint32_t surface_id,
    uint32_t state_flags);           // bit0=live_resizing, bit1=fullscreen, bit2=hidden (atomic store in Zig)

bool ggfx_surface_kept(
    GgfxDevice* dev,
    uint32_t surface_id);
```

### Key ABI Safety Invariants

- **Go GC pinning**: All pointers in `GgfxBatch` and `GgfxReadRegion` are valid only for the duration of the synchronous FFI call (keeping `runtime.KeepAlive` on the Go slices across the `purego` call). Zig never stores any pointer received from Go past function return.
- **No Zig → Go callbacks on the hot path**: Avoid `purego.NewCallback` re-entrancy into Go during rendering. On macOS, Zig uses `dispatch_sync_f(dispatch_get_main_queue(), ...)` directly when Metal needs a main-thread layer update during live resize, or performs main-thread surface operations during explicit `ggfx_surface_*` calls from Go's main thread.

---

## 5. Backend Strategy in Zig 0.17.1

| Platform | Primary Zig Backend | Notes |
|---|---|---|
| macOS (`arm64`, `amd64`) | **Metal** | Direct ObjC runtime / Metal bindings; `MTLStorageModeShared` on unified memory (`hasUnifiedMemory`), `MTLStorageModePrivate` + staging on discrete GPUs; native `R8Unorm` + `RRRR` swizzle for `PixelFormatA8`; `SetFragmentBuffer` fallback for uniform blocks `> 4096` bytes; lock-safe `IOSurface` swap chain. |
| Windows (`amd64`, `arm64`) | **Direct3D 12** (DX11 fallback) | Single `DIRECT` command list (eliminating copy-list stalls), persistently mapped per-frame upload ring, `MipLevels = 1`, root CBVs gated on `UniformVersions`, native `DXGI_FORMAT_R8_UNORM` + SRV swizzle for `PixelFormatA8`, DirectComposition support. |
| Linux (`amd64`, `arm64`) | **Vulkan** | First-class native backend using X11/Wayland surface descriptors from `ggwd`; native `VK_FORMAT_R8_UNORM` + `ComponentMapping(R,R,R,R)` for `PixelFormatA8`; avoids Linux `libGL` vs `libGLESv2` loader fragility. |
| Browser (`js/wasm`) | **Go WebGL** (unchanged) | Stays in Go (`internal/graphicsdriver/opengl`) — no shared library in browsers. |

---

## 6. Build, `purego` Loading & Distribution

### Building with Zig 0.17.1

Pin Zig `0.17.1` in CI and local build scripts (`zig/` directory or companion module):

```sh
zig build -Dtarget=aarch64-macos -Doptimize=ReleaseFast
zig build -Dtarget=x86_64-macos  -Doptimize=ReleaseFast
zig build -Dtarget=x86_64-windows-gnu -Doptimize=ReleaseFast
zig build -Dtarget=x86_64-linux-gnu   -Doptimize=ReleaseFast
zig build -Dtarget=aarch64-linux-gnu  -Doptimize=ReleaseFast
```

Development and CI test builds should also run `-Doptimize=Debug` / `-Doptimize=ReleaseSafe` with Zig's leak-detecting allocator checked in `ggfx_device_destroy`.

### Library Discovery Order in Go (`internal/graphicsdriver/zigdriver`)

1. Explicit override path via `GGFX_DRIVER_LIB` environment variable (useful for `go test` and local dev).
2. Next to the running executable / macOS `.app/Contents/Frameworks/libggfx_driver.dylib` (recommended for production desktop apps such as `ggui`).
3. Standard dynamic linker search path (`dlopen("libggfx_driver.dylib")` / `LoadLibrary("ggfx_driver.dll")`).

> **Packaging note**: Avoid relying on `//go:embed` + extracting shared libraries to `%TEMP%`/`/tmp` in signed production builds — it triggers macOS Notarization / Hardened Runtime restrictions and Windows Defender false positives. Keep embedded extraction optional (e.g. dev-only build tag) if ever needed.

Verify `ggfx_abi_version() == GGFX_DRIVER_ABI_VERSION` immediately after loading; fail cleanly with a clear error (or fall back to a Go driver when configured) on mismatch.

---

## 7. Implemented Architecture & Usage

### Summary of Implementation

- **C ABI v1 (`zig/include/ggfx_driver.h`)**: 14 exported flat-C functions (`ggfx_abi_version`, `ggfx_device_create`, `ggfx_device_destroy`, `ggfx_device_set_main_thread_mode`, `ggfx_submit`, `ggfx_create_shader`, `ggfx_read_pixels`, `ggfx_finish_forced_frame`, `ggfx_present_on_main_thread`, `ggfx_device_is_occluded`, `ggfx_surface_create`, `ggfx_surface_set_window_state`, `ggfx_surface_kept`, `ggfx_debug_stats`) verified by Zig comptime layout checks, `zig cc -std=c11` `_Static_assert` checks (`zig/tests/abi_check.c`), and Go `TestABILayout` / `TestGoldenBatchV1`.
- **Zero-allocation Go FFI (`internal/graphicsdriver/zigdriver`)**: Resolves symbol addresses via `purego.Dlsym` (Unix) / `windows.GetProcAddress` (Windows) and invokes them directly with `purego.SyscallN` using pooled argument buffers, keeping steady-state `Begin`/`SetVertices`/`DrawTriangles`/`End` at **0 allocs/op**.
- **Zig Backends (`zig/src/`)**: Shared linear command replayer, dense handle tables, and pipeline/uniform-version state cache driving:
  - **Metal** (macOS `arm64`/`amd64`): native `R8Unorm` A8 swizzle, unified-memory shared textures, IOSurface swap chain, and configurable main-thread presentation (`inline`, `deferred` via `MainThreadRunner`, or `dispatch` via `GGFX_ZIG_METAL_DISPATCH=1`).
  - **Direct3D 12** (Windows `amd64`/`arm64`): single `DIRECT` command list, persistently mapped upload ring, WARP software fallback (`GGFX_DIRECTX=warp`), native `R8_UNORM` A8, and DirectComposition/HWND swap chains.
  - **Vulkan** (Linux `amd64`/`arm64`): X11 and Wayland swap chains, native `VK_FORMAT_R8_UNORM` A8, and software testing via Mesa lavapipe.
  - **Null** (`-Dbackend=null` / `zig build test`): deterministic software replayer and allocator leak checker used in unit and conformance testing.

### Opt-in Runtime Configuration

| Environment Variable | Effect |
|---|---|
| `GGFX_DRIVER=zig` | Opt in to the Zig driver (falls back to Go driver on failure unless `GGFX_DRIVER_STRICT=1`). Set `GGFX_DRIVER=go` to force Go even if `GGFX_DRIVER_LIB` is set. |
| `GGFX_DRIVER_LIB=/path/to/lib` | Explicit shared-library path override; enables the Zig driver automatically unless `GGFX_DRIVER=go`. |
| `GGFX_DRIVER_STRICT=1` | Fail with an error instead of falling back to the Go driver when Zig initialization fails. |
| `GGFX_GRAPHICS_LIBRARY=zig\|vulkan` | Select the Zig driver (or specifically the Linux Vulkan backend via Zig). |
| `GGFX_DRIVER_DEBUG=1` | Enable backend validation/debug layers (Metal/D3D12/Vulkan). |
| `GGFX_DRIVER_SLOTS=2\|3` | Configure command arena ring slot count (default: `2`). |

### Benchmarking

Build the ReleaseFast shared library and compare Go vs. Zig driver latency and allocations (`internal/graphicsdriver/zigdriver` and `internal/rendertest`, printing `benchstat` diffs when installed):

```sh
./scripts/bench-zig.sh        # default -count=5
./scripts/bench-zig.sh 10     # custom iteration count
```

### Rollout Status

1. **Phase 0 — Go Refactoring**: Complete (`capabilities.go`, `Closer`, `drivertest`, `ShaderDescriptor`, `SurfaceDescriptor`, thread-confinement fixes).
2. **Phase 1 — ABI & macOS Metal**: Complete (`zig/include/ggfx_driver.h`, `internal/graphicsdriver/zigdriver`, Metal backend, 0-alloc `purego.SyscallN` hot path).
3. **Phase 2 — Windows DX12 & Linux Vulkan**: Complete (shared Zig replayer with DX12, Vulkan, and Null backends; CI coverage in `.github/workflows/test.yml`).
4. **Phase 3 — Re-evaluate Plan B**: Deferred until real `ggui` workload profiling after Phase 2 adoption indicates Go CPU/GC overhead in `graphicscommand`, `atlas`, or `vector` is a primary bottleneck.
