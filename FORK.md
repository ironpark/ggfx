# Fork notice

ggfx is a fork of [Ebitengine](https://github.com/hajimehoshi/ebiten),
made to become the rendering and windowing runtime of
[ggui](https://github.com/ironpark/ggui). It is not a general-purpose game
engine and does not track Ebitengine releases.

## Provenance

| | |
|---|---|
| Upstream | `https://github.com/hajimehoshi/ebiten` |
| Forked at | tag `v2.10.2`, commit `221aa13ad` |
| Upstream remote | `upstream` (kept for cherry-picks) |
| License | Apache-2.0, unchanged. See `LICENSE` and `NOTICE.md`. |

The module path is `github.com/ironpark/ggfx` and the root package is
`ggfx`. Everything else keeps its upstream file layout so that upstream
commits to the retained packages can be cherry-picked.

## Shaders: WGSL through naga instead of Kage

The Kage shader language and its compiler (`internal/shader`,
`internal/shaderir` and the MSL, HLSL and GLSL backends, `legacyshader`,
`shaderprecomp`) are replaced by WGSL compiled with
[gogpu/naga](https://github.com/gogpu/naga). See `docs/shaders.md` for the
shader contract. What changed for the drivers:

- `internal/shader` now parses, validates and reflects WGSL. Uniform values
  are packed on the host in WGSL's uniform layout and uploaded verbatim; the
  per-driver layout adjusters are gone.
- Every draw binds two uniform blocks: the internal block (texture sizes,
  regions, projection) and the user's block.
- Metal: naga's MSL uses `[[stage_in]]`, so `mtl` gained
  `MTLVertexDescriptor` support.
- OpenGL: uniforms moved from `glUniform*` to uniform buffer objects; the
  `gl` binding gained `BindBufferBase`, `GetUniformBlockIndex` and
  `UniformBlockBinding`. Desktop GL targets GLSL 3.30, WebGL GLSL ES 3.00.
- DirectX: shader model 5.0 (`vs_5_0`/`ps_5_0`), so feature level 11.0 is
  required; feature levels 10.x are dropped. Vertex semantics are `LOC0..3`.
  The projection matrix's Y inversion, which the HLSL uniform adjuster
  used to do, now lives in the driver (`flipProjectionY`), like Metal.
- The built-in, ColorM, vector stencil and test shaders are rewritten in
  WGSL. The texel unit (`//kage:unit texels`) is gone; all positions are
  pixels.
- `vector/stencilshader.go` and `stencilbuffer.go` changed with it, so those
  files no longer merge cleanly from upstream.

## What was removed and why

ggui targets desktop and the browser and uses a small part of the engine: images and
shaders, `text/v2`, `vector`, `exp/textinput`, `inpututil`, input, cursor,
monitor and window APIs. Everything outside that was dropped.

- `audio`, `mobile`, `cmd` (ebitenmobile), `examples`, `misc`, `skills`,
  `ebitenutil`, `colorm`, old `text` v1, `vibrate`.
- Console and mobile platforms: Nintendo Switch, PlayStation 5, Xbox GDK,
  Android and iOS sources. The browser (`js`/`wasm`) target is kept. `internal/microsoftgdk`
  survives as a stub whose `IsXbox` is always false so the glfw and DirectX
  drivers stay untouched.
- The VM guest/host remote rendering backend (`exp/vmhost`,
  `internal/vmguest`, `internal/vmprotocol`, `internal/graphicsdriver/remote`)
  and the `RunGameOptions.VMGuestEndpoint` option.
- The Linux framebuffer backend (`internal/fbdev`).
- Tooling: `internal/vettools`, `internal/processtest`,
  `internal/beforemaintest`, `internal/shadercollector`, `exp/shaderprecomp`.
- Test fonts and images that lived under `examples/resources` moved to
  `internal/testresources`.

Retained platforms: macOS (Metal, OpenGL), Windows (DirectX, OpenGL),
Linux and the BSDs (OpenGL through glfw; cgo required), and the browser
(WebGL through `js`/`wasm`). Only macOS was built and tested in the fork
pass; Windows and wasm were cross-compiled; Linux and BSD were not built.

## Upstream policy

Do not merge upstream wholesale. Cherry-pick fixes to the layers that are
kept close to upstream: `internal/graphicsdriver` (except the shader files),
`internal/graphicscommand`, `internal/atlas`, `internal/restorable`,
`text/v2`, `vector` (except the stencil shaders). The shader stack, the
windowing and the run loop (`internal/ui`, `run.go`, `window.go`,
`input.go`) diverge and are not expected to merge.

## Windows and the event loop

`Run`, `NewWindow`, `Window` and the event types in `app.go` are the API a
GUI uses: any number of windows, frames on request, input as events. The
design and the per-driver status are in [docs/window.md](docs/window.md).
The test suite runs on `Run` too: `internal/testing.MainWithRunLoop` opens one
window and runs the tests in its first frame. `Game`/`RunGame` still work, and
internally a game is one window that asks for a frame every iteration, but
nothing in ggfx or ggui calls it any more.

## Draw call merging

The command queue merges consecutive draws that share destination, sources,
shader, blend and uniforms. The internal uniform block carries the destination
and source regions, which differ from draw to draw, so a shader that never
reads them would still keep every draw apart. `shader.Program.FilterInternalUniforms`
zeroes the dwords the program cannot reach (computed from the compacted naga
IR once per program), which is what ebiten's `FilterUniformVariables` did for
Kage. Without it the gallery example issued seven times the draw calls, which
WebGL feels.

## Not done yet

- More than one window on OpenGL and WebGL (`docs/window.md`). Only Metal
  and the two-window example in `examples/multiwindow` were run; DirectX
  accepts one surface and was cross-compiled only.
- IME composition on the event path outside macOS and Windows. X11 and the
  browser still use `exp/textinput` for composition.
- Only macOS Metal and OpenGL ran the test suite after the shader change;
  DirectX 11/12 and WebGL were cross-compiled only. naga's HLSL was checked
  to need shader model 5.0 without register spaces, but no D3D compiler or
  debug layer has seen it.
- Runtime validation of Windows native hooks (OLE drag-over, IMM composition,
  WM_GETOBJECT); they are implemented and cross-built. macOS native hooks have
  a two-window smoke test in `examples/nativehooks`.
- Deciding whether gamepad support stays.
- `genkeys.go` still generates key tables for the removed mobile platforms.
- The `ebiten:` prefix in error messages and documentation wording.
