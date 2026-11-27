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
- **Opaque `u32` handle IDs** (`0` = none/null) managed by Go for Images, Shaders, and Surfaces. Surface IDs are allocated monotonically; Image and Shader IDs are recycled from a free list once the batch carrying their `OP_DISPOSE_*` command has been submitted (`idAllocator` in `graphics.go`). Zig stores native GPU objects in dense slot tables indexed by `u32`. **No Go pointers are ever retained by Zig across calls.**
- **Shared GPU-mapped upload arena** (`ggfx_acquire_upload_arena`): Backends advertising `GGFX_CAP_SHARED_UPLOAD_ARENA` expose a persistently mapped GPU upload buffer to Go across batches. This intentionally inverts the usual pointer direction: Go retains the driver-owned `GgfxUploadArena.ptr` until the arena rotates or the device closes, advancing monotonically within `[batchStart:cursor]` so `WritePixels` and uniform payloads are written directly into GPU-accessible memory with zero Go-heap staging copies. When the backend requires row-pitch alignment (e.g., 256 bytes on D3D12), Go lays out rows directly at `GgfxUploadArena.row_pitch_alignment` and records that alignment in `GgfxCmdWritePixels.hdr.aux`. On rotation, the backend allocates and maps the replacement buffer before retiring the old one; if rotation fails, Go clears its shared-arena pointer and falls back cleanly to the Go-heap payload arena.
- Command records in `cmds[]` are 8-byte aligned:

```c
typedef struct {
    uint8_t  op;       // GgfxOp enum
    uint8_t  flags;
    uint16_t aux;      // e.g. dst_region_count, write_count, pixel_format, or write_pixels row_pitch_align
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

Lifecycle, submission, and resource-management functions return a `GgfxStatus` (`0 = OK`, non-zero error code) and write human-readable diagnostics into a caller-supplied error buffer (`char* err_buf, size_t err_cap`), so no heap-allocated error strings cross the ABI. Lightweight queries and setters (`ggfx_abi_version`, `ggfx_device_set_main_thread_mode`, `ggfx_device_is_occluded`, `ggfx_surface_set_window_state`, `ggfx_surface_kept`, and `ggfx_debug_stats`) omit the error buffer.

```c
#define GGFX_DRIVER_ABI_VERSION 2u

uint32_t ggfx_abi_version(void);

// Lifecycle (main thread)
GgfxStatus ggfx_device_create(
    const GgfxDeviceOptions* opts,   // backend preference, color space, debug/validation flags
    GgfxDevice** out_dev,
    GgfxDriverCaps* out_caps,        // max_image_size, flips_y, supports_a8, needs_clear, direct_comp, backend_kind
    char* err_buf, size_t err_cap);

GgfxStatus ggfx_device_destroy(
    GgfxDevice* dev,
    char* err_buf, size_t err_cap);

// Hot path (render thread): 1 call per Flush
GgfxStatus ggfx_submit(
    GgfxDevice* dev,
    const GgfxBatch* batch,          // ptr+len for cmds, vertices, indices, payload
    uint32_t flush_mode,             // 0=Intermediate, 1=EndFrame, 2=Present
    GgfxSubmitResult* out_res,       // occluded flag, etc.
    char* err_buf, size_t err_cap);

GgfxStatus ggfx_acquire_upload_arena(
    GgfxDevice* dev,
    uint64_t min_bytes,
    GgfxUploadArena* out_arena,      // arena_id, GPU-mapped ptr, capacity, alignment, row_pitch_alignment
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

uint32_t ggfx_surface_kept(
    GgfxDevice* dev,
    uint32_t surface_id);            // returns 1 if kept, 0 otherwise
```

### Key ABI Safety Invariants

- **Strict ABI version handshake**: Both sides enforce `GGFX_DRIVER_ABI_VERSION` (currently `2`) before any struct crosses the ABI. Immediately after loading the shared library (whether via `GGFX_DRIVER_LIB`, linked `LibraryPath`, embedded extraction, or dynamic linker search), Go calls `ggfx_abi_version()` and unloads the library with `ErrABIMismatch` on any difference. On the native side, `ggfx_device_create` validates `opts->abi_version` before inspecting the full `struct_size` or writing to `out_dev`/`out_caps`, refusing mismatched callers with `GGFX_ERR_ABI_MISMATCH` without touching output memory.
- **Terminal device-lost state & teardown**: The shared layer (`root.zig`) latches a device into a terminal lost state (tracked atomically across threads) only for (i) physical device loss reported by the GPU API (`GGFX_ERR_DEVICE_LOST`), (ii) a failure after partial or uncertain submission (command submission failed, or a post-submit wait/reset failed), or (iii) a failure after a screen swapchain image was acquired in the batch and not consumed by a successful submit. Offscreen-only batch aborts reset the command buffer and roll back tracked layouts cleanly. On WSI paths, a first acquire `OUT_OF_DATE` retries once after recreating the swapchain, a present `OUT_OF_DATE` schedules swapchain recreation for the next frame and usually returns `GGFX_OK`, and `SURFACE_LOST` (or a repeated acquire `OUT_OF_DATE` with no unconsumed acquire) returns `GGFX_ERR_SURFACE_LOST` without latching device loss. Once latched, status-returning operations fail immediately with `GGFX_ERR_DEVICE_LOST` without mutating outputs or backend state, void setters become no-ops, scalar queries (`ggfx_device_is_occluded`, `ggfx_surface_kept`) return `0`, `ggfx_debug_stats` continues to report counters, and `ggfx_device_destroy` remains callable. If `ggfx_device_destroy` runs while GPU completion is uncertain and its checked idle wait fails with anything other than API-reported device loss, teardown is abandoned without releasing GPU resources (deliberately leaking the device rather than freeing a partial dependency set while GPU work may still run); clean teardown is not guaranteed in that uncertain state.
- **Post-abort vertex rebinding & deferred disposals**: Any aborted batch invalidates the active vertex/index binding, so a subsequent draw with indices must issue `OP_SET_VERTICES` again or fail with `GGFX_ERR_INVALID_ARGUMENT`. On the Go side, `Image.Dispose`, `Shader.Dispose`, and `Surface.Dispose` keep their native disposals pending until the carrying batch succeeds; on recoverable batch failure, Go re-queues those lifecycle disposals idempotently at the front of the next batch and withholds handle ID recycling until a batch carrying them succeeds.
- **Go GC pinning**: All Go-owned pointers in `GgfxBatch`, `GgfxCmdSetVertices`, `GgfxShaderDesc`, and `GgfxReadRegion` are valid only for the duration of the synchronous FFI call. Go pins `OP_SET_VERTICES` vertex/index backing arrays on the slot with `runtime.Pinner` (unpinned on slot reset after `ggfx_submit` returns) and keeps `GgfxBatch` / `GgfxReadRegion` / `GgfxShaderDesc` backing slices alive across `purego.SyscallN`. Zig never stores any pointer received from Go past function return.
- **Shared upload arena ownership**: For `ggfx_acquire_upload_arena`, ownership is reversed — the native backend owns the GPU-mapped memory and keeps it valid until the next successful `ggfx_acquire_upload_arena` rotation or `ggfx_device_destroy`. Go clears its cached pointer immediately on any failed rotation or device close.
- **No Zig → Go callbacks on the hot path**: Avoid `purego.NewCallback` re-entrancy into Go during rendering. On macOS, Zig uses `dispatch_sync_f(dispatch_get_main_queue(), ...)` directly when Metal needs a main-thread layer update during live resize, or performs main-thread surface operations during explicit `ggfx_surface_*` calls from Go's main thread.

---

## 5. Backend Strategy in Zig 0.17.1

| Platform | Primary Zig Backend | Notes |
|---|---|---|
| macOS (`arm64`, `amd64`) | **Metal** | Direct ObjC runtime / Metal bindings; `MTLStorageModeShared` on unified memory (`hasUnifiedMemory`), `MTLStorageModeManaged` on discrete GPUs; native `R8Unorm` + `RRRR` swizzle for `PixelFormatA8`; `SetFragmentBuffer` fallback for uniform blocks `> 4096` bytes; lock-safe `IOSurface` swap chain. |
| Windows (`amd64`, `arm64`) | **Direct3D 12** | Single `DIRECT` command list (eliminating copy-list stalls), persistently mapped per-frame upload ring and 256-byte row-pitched shared upload arena, `MipLevels = 1`, root CBVs gated on `UniformVersions`, native `DXGI_FORMAT_R8_UNORM` + SRV swizzle for `PixelFormatA8`, DirectComposition support. |
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

1. An exact path: the `GGFX_DRIVER_LIB` environment variable (useful for `go test` and local dev), else the path linked into `zigdriver.LibraryPath`. A failure here is terminal.
2. The library embedded by the `ggfxzig` build tag, written to the user's cache directory.
3. Next to the running executable / macOS `.app/Contents/Frameworks/libggfx_driver.dylib` (recommended for production desktop apps such as `ggui`).
4. Standard dynamic linker search path (`dlopen("libggfx_driver.dylib")` / `LoadLibrary("ggfx_driver.dll")`).

### Build Tags and the Embedded Library

| Build | Default driver | Where the library comes from |
|---|---|---|
| no tag | Go | Not used, unless the environment selects the Zig driver; then steps 1, 3 and 4 above. |
| `-tags ggfxzig` | Zig | Embedded in the program (step 2), unless step 1 names another. |
| `-tags ggfxzig,ggfxnoembed` | Zig | Steps 1, 3 and 4: the program ships the library itself. |

`GGFX_DRIVER=go` chooses the Go driver whatever the tags, and a Zig driver that fails to start still falls back to the Go driver unless `GGFX_DRIVER_STRICT=1`.

The embedded library is the ReleaseFast, stripped build, gzip-compressed: 50–85 KB per platform. It is embedded for the platform being built only. On first use it is written to `<user cache dir>/ggfx/driver-<hash of the library>/` and loaded from there; a file already there with the same bytes is reused, and a temporary directory stands in when the cache directory cannot be written.

A release has the libraries for all six platforms. They are not committed on `main`: in a checkout, build them with Zig 0.17.1, or `-tags ggfxzig` fails to compile for want of the file.

```sh
./scripts/build-zig-prebuilt.sh                 # the host platform
./scripts/build-zig-prebuilt.sh --all           # every platform
go generate ./internal/graphicsdriver/zigdriver # the same as the first
```

With `ggfxnoembed`, the path can be fixed when the program is linked instead of left to the search. A relative path is taken from the directory of the executable:

```sh
go build -tags ggfxzig,ggfxnoembed \
  -ldflags "-X github.com/ironpark/ggfx/internal/graphicsdriver/zigdriver.LibraryPath=lib/libggfx_driver.so"
```

> **Packaging note**: Embedding suits development and unsigned tools. A signed production build should use `ggfxnoembed` and ship the library in its bundle: loading a library written to the cache directory is refused under macOS's Hardened Runtime library validation and can draw Windows Defender false positives.

### Releasing

The `Release` workflow (`.github/workflows/release.yml`) is started by hand with a version. It builds the libraries for every platform from the commit it runs on, commits them on top of that commit, tags the result and publishes a GitHub release with the libraries attached. The commit with the libraries is reachable from the tag only, so `main` carries no binaries while `go get github.com/ironpark/ggfx@<version>` has them.

Verify `ggfx_abi_version() == GGFX_DRIVER_ABI_VERSION` immediately after loading; fail cleanly with a clear error (or fall back to a Go driver when configured) on mismatch.

---

## 7. Implemented Architecture & Usage

### Summary of Implementation

- **C ABI v2 (`zig/include/ggfx_driver.h`)**: 15 exported flat-C functions (`ggfx_abi_version`, `ggfx_device_create`, `ggfx_device_destroy`, `ggfx_device_set_main_thread_mode`, `ggfx_submit`, `ggfx_acquire_upload_arena`, `ggfx_create_shader`, `ggfx_read_pixels`, `ggfx_finish_forced_frame`, `ggfx_present_on_main_thread`, `ggfx_device_is_occluded`, `ggfx_surface_create`, `ggfx_surface_set_window_state`, `ggfx_surface_kept`, `ggfx_debug_stats`) verified by Zig comptime layout checks, `zig cc -std=c11` `_Static_assert` checks (`zig/tests/abi_check.c`), and Go `TestABILayout` / `TestGoldenBatchV1`.
- **Zero-allocation Go FFI (`internal/graphicsdriver/zigdriver`)**: Resolves symbol addresses via `purego.Dlsym` (Unix) / `windows.GetProcAddress` (Windows) and invokes them directly with `purego.SyscallN` using pooled argument buffers, keeping steady-state `Begin`/`SetVertices`/`DrawTriangles`/`End` at **0 allocs/op**.
- **Zig Backends (`zig/src/`)**: Shared linear command replayer, dense handle tables, and pipeline/uniform-version state cache driving:
  - **Metal** (macOS `arm64`/`amd64`): native `R8Unorm` A8 swizzle, unified-memory shared textures, IOSurface swap chain, and configurable main-thread presentation (`inline`, `deferred` via `MainThreadRunner`, or `dispatch` via `GGFX_ZIG_METAL_DISPATCH=1`).
  - **Direct3D 12** (Windows `amd64`/`arm64`): single `DIRECT` command list, persistently mapped upload ring, WARP software fallback (`GGFX_DIRECTX=warp`), native `R8_UNORM` A8, and DirectComposition/HWND swap chains.
  - **Vulkan** (Linux `amd64`/`arm64`): X11 and Wayland swap chains, native `VK_FORMAT_R8_UNORM` A8, and software testing via Mesa lavapipe.
  - **Null** (`-Dbackend=null` / `zig build test`): deterministic software replayer and allocator leak checker used in unit and conformance testing.

### Opt-in Runtime Configuration

| Environment Variable | Effect |
|---|---|
| `GGFX_DRIVER=zig` | Opt in to the Zig driver (falls back to Go driver on failure unless `GGFX_DRIVER_STRICT=1`). Set `GGFX_DRIVER=go` to force Go even if `GGFX_DRIVER_LIB` is set or the program is built with `-tags ggfxzig`. |
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

### Testing the Vulkan Backend Without a Linux Host

Cross-compile the driver and the `zigdriver` tests and run them on Mesa's lavapipe (software Vulkan) in a Linux container. Presenting to a surface is not covered, as the container has no window system:

```sh
./scripts/test-zig-vulkan-docker.sh            # all zigdriver tests
./scripts/test-zig-vulkan-docker.sh -test.v -test.run TestDriverConformance
```

The script builds with `-Dalloc-trace-frames=0`, which stops the debug allocator from recording allocation stack traces. Zig 0.17.1's unwinder panics and then hangs in sandboxed container runtimes whose vDSO has a wrapping load address; leak detection and safety checks stay on.

### Rollout Status

1. **Phase 0 — Go Refactoring**: Complete (`capabilities.go`, `Closer`, `drivertest`, `ShaderDescriptor`, `SurfaceDescriptor`, thread-confinement fixes).
2. **Phase 1 — ABI & macOS Metal**: Complete (`zig/include/ggfx_driver.h`, `internal/graphicsdriver/zigdriver`, Metal backend, 0-alloc `purego.SyscallN` hot path).
3. **Phase 2 — Windows DX12 & Linux Vulkan**: Complete (shared Zig replayer with DX12, Vulkan, and Null backends; CI coverage in `.github/workflows/test.yml`).
4. **Phase 3 — Re-evaluate Plan B**: Deferred until real `ggui` workload profiling after Phase 2 adoption indicates Go CPU/GC overhead in `graphicscommand`, `atlas`, or `vector` is a primary bottleneck.
