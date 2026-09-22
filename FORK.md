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
- The built-in, vector stencil and test shaders are rewritten in
  WGSL. The texel unit (`//kage:unit texels`) is gone; all positions are
  pixels.
- `vector/stencilshader.go` and `stencilbuffer.go` changed with it, so those
  files no longer merge cleanly from upstream.

## What was removed and why

ggui targets desktop and the browser and uses a small part of the engine: images and
shaders, `text/v2`, `vector`, `exp/textinput`, `inpututil`, input, cursor,
monitor and window APIs. Everything outside that was dropped.

- The polling input API: `input.go`'s key, mouse, touch and gamepad state
  functions, `DroppedFiles`, `inpututil` and `internal/inputstate`. Input
  arrives as events. What is left in `input.go` asks what a device is, not what
  it is doing. `exp/textinput`'s console backend went with the consoles.
- `internal/ui.InputState` and everything that filled it: `glfwInput`, the
  browser's `inputState`, `readInputState`, `updateInputStateForFrame`,
  `InputTime`, `LockKeyState` and the per-backend `syncModKeysFromOS` and
  `syncLockKeysFromOS`. Its only reader went with the Game frame driver, so
  every write had been unobservable since. Three things went with it that had
  therefore already stopped working, and are gaps rather than removals:
  Caps/Num lock state, which no event carries; the cursor position restored
  across a fullscreen transition with a disabled cursor; and the browser's
  accumulated pointer-lock cursor movement, so a captured cursor there reports
  the frozen `clientX`. The desktop frame loop also stopped asking the main
  thread for the cursor position once per window per frame to feed the dead
  path.
- `audio`, `mobile`, `cmd` (ebitenmobile), `examples`, `misc`, `skills`,
  `ebitenutil`, `colorm`, old `text` v1, `vibrate`.
- The deprecated color matrix and composite mode: `ColorM`, `ColorMDim`,
  `CompositeMode` and its constants, the `ColorM`/`CompositeMode` fields on
  every draw option, `internal/affine` and `internal/colormshader`. `ColorScale`
  and `Blend` are the only ways to tint and blend. Without a color matrix
  `builtinShader` has one shader per filter and address rather than two, and
  `DrawTriangles` no longer scales every vertex colour by a matrix-derived
  factor that was always 1.
- Console and mobile platforms: Nintendo Switch, PlayStation 5, Xbox GDK,
  Android and iOS sources. The browser (`js`/`wasm`) target is kept. `internal/microsoftgdk`
  survives as a stub whose `IsXbox` is always false so the glfw and DirectX
  drivers stay untouched.
- The `frameDriver` interface, which had one implementation. `eventContext` is
  now named directly. Its outside-size parameters were never read, so
  `layoutSizes`, `updateWindow` and `forceUpdateFrameDuringPollEvents` return
  and take the rendering destination's pixel size alone, and `outsideSizeInDIP`
  — the size for the game's `Layout` — is gone. Its client/logical position
  converters were identities, so `LogicalPositionToClientPositionInNativePixels`
  is now the scale conversion alone.
- The legacy `Game`/`RunGame` API: `Game`, `LayoutFer`, `FinalScreen`,
  `FinalScreenDrawer`, `RunGame`, `RunGameWithOptions`, `ScreenSize`, the TPS
  and FPS-mode functions, the image dumper and its screenshot environment
  variables, and the root-level window, cursor, fullscreen and monitor
  functions that addressed the primary window. `Run`, `NewWindow` and
  `*Window` replace them. `RunGameOptions` is now `RunOptions`. Inside
  `internal/ui` the game frame driver, its entry points and the app-versus-game
  branches went with it; `internal/ui.Game` no longer exists. The virtual
  keyboard's screen shift lived in the game's letterbox transform and has no
  consumer now: the state is still recorded for a per-window shift to use.
- The VM guest/host remote rendering backend (`exp/vmhost`,
  `internal/vmguest`, `internal/vmprotocol`, `internal/graphicsdriver/remote`)
  and the `RunGameOptions.VMGuestEndpoint` option.
- The Linux framebuffer backend (`internal/fbdev`).
- The mobile key tables and templates in `genkeys.go`: it no longer generates
  `mobile/ebitenmobileview/keys_android.go` or `keys_ios.go`.
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
The test suite runs on it too: `internal/testing.MainWithRunLoop` opens one
window and runs the tests in its first frame. The legacy `Game`/`RunGame` API
is gone from the public surface, along with the root-level functions that
addressed the game's primary window; `*Window` carries them per window.

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
- Waking the loop from the macOS and Linux gamepad connection callbacks, instead
  of the one-second detection poll (`docs/window.md`).
- `RunOptions.ScreenTransparent` and `graphicsdriver.SetTransparent` are a
  process-wide knob for something every retained driver decides per surface, so
  `NewWindow` has to reject a transparent window when they disagree. The deeper
  fix is to pass transparency to surface creation and delete both. The same
  shape applies to `RunOptions.InitUnfocused` versus `WindowOptions.Unfocused`.
- The FPS-mode machinery in `internal/ui` can only hold `FPSModeVsyncOn` since
  the root-level setters went.
- X11 text input through `exp/textinput` lost its `AppendInputChars` seed, which
  had stopped reporting anything under `Run` anyway. It needs to take committed
  text from `TextEvent` instead, which is the same gap as IME composition there.
- The `!android && !ios && !js && !nintendosdk && !playstation5` build
  constraint is still spelled out across `internal/ui`, where `!js` would do.
- The deprecated v2.1 key aliases (`KeyDown`, `Key0`, ...) that `genkeys.go`
  emits into `keys.go`.
