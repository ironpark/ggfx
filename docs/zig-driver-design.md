# Zig `graphicsdriver` Backend Design (Plan A)

> Status: Deferred design document. Complete the Go-side architecture & quality refactoring first (see Prerequisites below), then implement in phases.

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

1. **Consolidate side-channel type assertions**: Replace the 12 ad-hoc structural interface assertions spread across `internal/render` and `internal/graphicscommand` (`SetMainThreadRunner`, `SetDrawingContext`, `IsES`, `SupportsDirectComposition`, `IsOccluded`, `FinishForcedFrame`, `ScreenFramebufferSize`, `Resetter`, `Kept`, `SetDamage`, `SetWindowState`, `Readable`) with explicit `DriverCaps` / `SurfaceCaps` structs and documented methods/optional interfaces in `internal/graphicsdriver`.
2. **Add `Graphics.Close()`**: Provide deterministic teardown so Zig backends can release GPU devices/pipelines and run allocator leak detection in tests.
3. **Decouple `NewShader` from Go `*naga/ir.Module`**: Pass backend-ready shader payloads (compiled MSL/HLSL/GLSL/SPIR-V source or bytecode + reflection metadata: `TextureCount`, `UniformDwordCount`, `FragmentReadsInternalUniforms`) rather than a Go-only IR pointer.
4. **Typed native surface descriptors**: Replace `NewSurface(target any, transparent bool)` with a typed union/descriptor supporting `NSView`, `HWND`, X11 (`Display`, `Window`), Wayland (`wl_display`, `wl_surface`), WebGL `Canvas`, and `RemoteTarget`.
5. **Centralize `InternalImageSize` rounding & `InternalUniforms` POD layout**: Remove duplicated 16-pixel rounding and 72-dword offset knowledge across drivers.
6. **Enforce strict thread-confinement contracts**: Document and enforce which calls run on the render thread vs. main/caller thread (eliminating current races on `surfaces`, `damage`, and `Surface.Dispose`).

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

## 7. Phased Rollout Plan

1. **Phase 0 — Go Refactoring (current focus)**: Fix audited defects, unify driver contracts & optional capability interfaces, add `Graphics.Close()`, decouple `NewShader` from `*naga/ir.Module`, and add a shared driver conformance test suite.
2. **Phase 1 — ABI & macOS Metal PoC**:
   - Implement `internal/graphicsdriver/zigdriver` (Go encoder + `purego` loader) and `zig/` Metal backend.
   - Run the full `ggfx` test suite and `internal/rendertest/bench_darwin_test.go` comparing Zig Metal vs. Go Metal (pixel-exactness, CPU time, allocations, leak check).
3. **Phase 2 — Windows DX12 & Linux Vulkan**:
   - Implement DX12 and Vulkan inside the same Zig command replayer, sharing buffer/handle/uniform-version state tracking across all backends.
   - Enable native `PixelFormatA8` across macOS, Windows, and Linux.
4. **Phase 3 — Re-evaluate Plan B**:
   - Profile real `ggui` workloads after Phase 2. Only consider moving `graphicscommand`, `atlas`, or `vector` into Zig if profiling proves Go CPU/GC overhead in those layers is a primary bottleneck.
