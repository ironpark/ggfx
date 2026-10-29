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
| Go | 1.27 or later; the fork uses generic methods. |

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
- The rest of the deprecated API: `Image.Size`, `Dispose` (use `Deallocate`)
  and `ReplacePixels`; `DrawTriangles32` and `DrawTrianglesShader32`; the
  `FillRule` and `AntiAlias` draw options with `stencilbuffer.go`, which
  emulated them (use `vector.FillPath`); `MaxIndicesCount`, `MaxIndicesNum`
  and `MaxVerticesCount`; the pre-v2.1 key names like `KeyUp` and `KeyKP0`;
  `vector.DrawFilledRect`, `DrawFilledCircle` and the
  `Path.AppendVerticesAndIndicesFor*` methods with the flattening they used
  alone; `text.Advance` (use `AdvanceAt`) and `GoTextFace.Script`, which the
  `Language` decides.
- `exp/textinput`'s deprecated `Field`. `Composer` is the only text-input API.
  Its focus tracking went with it, and so did what the backends kept for a
  focused `Field` alone: macOS's line view over the field's text, and the
  browser's whole-value path, which sent the textarea when no session was
  active. A platform state with no active session is now dropped.
- Console and mobile platforms: Nintendo Switch, PlayStation 5, Xbox GDK,
  Android and iOS sources. The browser (`js`/`wasm`) target is kept. `internal/microsoftgdk`
  survived as a stub whose `IsXbox` was always false until the window system
  left; the DirectX 12 driver's Xbox paths went with it then.
- `graphicsdriver.Graphics.SetTransparent` and, on the desktop,
  `RunOptions.ScreenTransparent`. Transparency is a property of a surface, so
  it is passed to `NewSurface` and the window's own `Transparent` option
  decides it. Metal sets the layer's opacity from it as it always did, OpenGL
  ignores it because the GLFW framebuffer hint decides it, and DirectX returns
  an error rather than silently presenting an opaque window. `NewWindow` no
  longer cross-checks a process-wide knob against a per-window one, and
  `IsScreenTransparentAvailable`, which nothing called, is gone.
  `RunOptions.ScreenTransparent` survives for the browser, which has one
  canvas, like `RunOptions.InitUnfocused`.
- The fixed-TPS clock and the FPS modes. `internal/clock`, which decided how
  many `Update` calls a frame owed the real time, lost its only caller with
  `Update`: `UpdateFrame`, `SinkTick` and `SetTPS` had none left, so
  `ActualFPS` had been returning 0 and `TPS` a constant 60. The 60 survives as
  `monitorCacheTicks`, how long the current monitor stays cached. `FPSModeType`
  could only ever hold `FPSModeVsyncOn` once the root-level setters went, so it
  goes with `SetFPSMode`, `applyFPSMode`, `setFPSMode` and the browser's
  `forceUpdateOnMinimumFPSMode`; the frame pacer no longer asks whether vsync
  is on, and `graphicsdriver.Graphics.SetVsyncEnabled` is gone with the
  command queue's vsync state: every driver starts with vsync on and keeps it.
  OpenGL re-asserts an interval of 1 on every swap, as it did.
  `setFPSMode` also set GLFW's sticky keys and mouse buttons, which only a
  polling reader would have seen. `SetScreenClearedEveryFrame` and
  `IsScreenClearedEveryFrame` stored a flag no renderer read: the screen is
  cleared when `graphicsdriver.NeedsClearingScreen` says so.
  `Tick` stayed then, counting loop iterations; it is the renderer's own now
  (see "The renderer core").
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
- Gamepads: `internal/gamepad`, `internal/gamepaddb`, the gamepad tracker in
  `internal/ui`, `RunOptions.Gamepads`, the `Gamepad*Event` types and the
  `Gamepad*`, `StandardGamepad*` and `UpdateStandardGamepadLayoutMappings`
  API. They moved to [ggip](https://github.com/ironpark/ggip), which an app
  polls itself and wires to the loop through `RunOnMainThread`,
  `Window.NativeHandle` and `Wake`, as its `ggfxinput` module does; ggfx does
  not depend on it. The loop no
  longer wakes on a timer for them, and `cachedNativeWindow`, which only the
  DirectInput backend asked for, went with them. Upstream commits to the
  gamepad code are cherry-picked into ggip.
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
`text/v2`, `vector` (except the stencil shaders and the batched fill in
`fill.go` and `atlas.go`). The oksvg fork moved from `text/v2/internal/oksvg` to
`internal/oksvg`, so that the `svg` package can draw SVG documents with it; a commit to it
needs its paths rewritten. The Metal driver's per-draw calls also diverge;
see "Draw call cost" below. The shader stack and `internal/render` diverge and are not
expected to merge; the windowing and the run loop are ggwd's.

## Generic draw methods and uniform blocks

`DrawTriangles` and `DrawTrianglesShader` take `[]uint16` or `[]uint32`
indices through a type parameter, which needs Go 1.27's generic methods.
uint32 indices are passed on without the conversion `DrawTriangles` does
for uint16. A `nil` index slice needs the type spelled out, as in
`DrawTriangles[uint16]`.

`Shader.NewUniforms` returns a `Uniforms` block that the draw options take
as `UniformBlock`, beside the `Uniforms` map. Its generic setters write
values in the shader's layout ahead of the draw, so a draw skips the map
lookups and the reflection. See `docs/shaders.md`.

These change `image.go` and the shader files, so upstream commits to them
no longer merge cleanly.

## The window system moved to ggwd

ggfx is a renderer. The window system moved to its own repository,
[ggwd](https://github.com/ironpark/ggwd): `Run`, `Window`, the events, keys, mouse buttons,
cursor, clipboard, monitors, color mode, `RunOnMainThread`, `Wake`, `exp/textinput`,
`internal/ui`, `internal/glfw`, `internal/hook`, `internal/file`, `internal/colormode` and
`internal/exeicon`. It keeps copies of `internal/thread` and `internal/winver`, which the drivers
use too, and has `internal/cocoa`, `internal/microsoftgdk` and `internal/windowsystem` to itself:
ggfx's `internal/cocoa` is cut down to what the Metal driver calls, the renderer dropped the Xbox
paths, and the OpenGL loader on Linux and the BSDs tries OpenGL ES and then OpenGL whether or not
a display server is up. It uses ggfx through its public API alone, as Go's internal rule makes it. Its FORK.md carries what was
changed in the window system against upstream; the removals below that name its packages
happened before the move. Upstream commits to `internal/ui` and `internal/glfw` go to ggwd.

ggfx's own tests run inside one renderer frame with no window (`internal/testing.MainInFrame`); on
macOS OpenGL draws in a CGL context the harness makes. ggwd's tests run in a window, as ggfx's did.

## The renderer core

`internal/render` holds what draws, apart from what shows it: `Image`, `Shader` and the uniform
packing, the white image fills draw from, the error state, reads of pixels that wait for a frame,
driver creation and the tick. It was the drawing half of `internal/ui`. The tick counts flushes,
and the image GC and `text/v2`'s glyph caches expire against it; it is not exported, so a window
system keeps a clock of its own for what it caches, as ggwd does for the current monitor and
`exp/textinput`'s state queue. A window system drives it
through package `host`: `Init`, `Terminate`, `NewSurface`, `BeginFrame`, `Frame.Screen`,
`Frame.End`, `Flush` and the rest, so that an app drawing with ggfx does not see them. A surface's
target is typed (`NSView`, `HWND`, `GLWindow`, `Canvas`), and the WebGL driver is created for the
canvas of the first surface, as WebGL draws to the canvas it was created for. The driver's
particulars are one value, `CurrentDriver()`, and whether a present was occluded comes back from
`Flush`. The render thread starts at `Init`; pacing frames is the window system's. ggfx's own tests run inside one frame with no window
(`internal/testing.MainInFrame`); on macOS, OpenGL draws in a CGL context the harness makes. See
`docs/renderer-split.md` for how the split went.

A Metal surface is made for an NSView rather than an NSWindow, and the driver asks nothing of the
view's window: the window system tells the surface whether the window is in a live resize or
fullscreen, with `host.Surface.SetWindowState` before each frame, and the frame's `Begin` takes it
where it used to ask AppKit. The view becomes the layer's alone; GLFW's windows pass their content
view.

The drivers' vsync-off paths went once nothing could turn vsync off: Metal's `vsyncDisabled`, the
frame skip and display-link branches that read it, and the tearing branch of DirectX's `present`.
DirectX still makes its swap chains with `DXGI_SWAP_CHAIN_FLAG_ALLOW_TEARING` where it is
supported.

## Draw call merging

The command queue merges consecutive draws that share destination, sources,
shader, blend and uniforms. The internal uniform block carries the destination
and source regions, which differ from draw to draw, so a shader that never
reads them would still keep every draw apart. `shader.Program.FilterInternalUniforms`
zeroes the dwords the program cannot reach (computed from the compacted naga
IR once per program), which is what ebiten's `FilterUniformVariables` did for
Kage. Without it the gallery example issued seven times the draw calls, which
WebGL feels.

## Draw call cost

The Metal driver calls `objc_msgSend` directly through `purego.SyscallN` for
the calls made on every draw (`mtl/msgsend_darwin.go`), instead of
`objc.ID.Send`, which boxes its arguments and dispatches through reflection
at several allocations per call. On arm64 the struct arguments of
`setViewport:` and `setScissorRect:` are passed by reference; amd64 passes
them on the stack, so it keeps `Send` for those two. The driver also tracks
the state set on the current render command encoder and sets only what
changed, so the pipeline is set once per command rather than once per region.

`vector.FillPath` with antialiasing drew every path to its own stencil
sub-image once per sample: eight draws, eight more for curves, and one to
color it. A path contained in its bounds now joins one draw per atlas image
for each stencil pass, and paths drawn to the destination itself share the
coloring draw. This is sound because a contained path's points lie in its
stencil buffer and the sample offsets are under half a pixel, so no triangle
covers a pixel center of another buffer. Clipped paths and paths drawn to a
sub-image keep the old per-path draws. `vector/batch_test.go` checks that
both ways render the same pixels.

## Not done yet

- More than one canvas on WebGL (ggwd's `docs/window.md`). Metal and macOS OpenGL
  ran several windows; DirectX 11/12 and OpenGL on Linux and Windows accept
  several surfaces but were cross-compiled only.
- CI (`.github/workflows/test.yml`) runs the suite with no window on macOS Metal, on macOS
  OpenGL through Apple's software renderer, and on DirectX 11 and 12 through WARP, so naga's HLSL
  has been compiled and drawn with; no GPU's DirectX driver nor the debug layer has seen it, and
  WebGL and Linux OpenGL are built only, the harness having no context for them.
- On Apple's software OpenGL renderer the batched vector fill does not match the unbatched one
  pixel for pixel (`TestFillPathBatchingIsExact`, `TestFillPathBatchingWithGeneratedPaths`), with
  antialiasing and without: the soundness argument under "Draw call cost" leans on how a GPU
  rasterizes, which that renderer does differently. CI skips the two tests there. To do: find
  whether a GPU's driver differs too (Mesa's llvmpipe, other vendors), and either make the
  batching exact for any rasterizer that follows the pixel-center rules or batch only where it is.
- To do: make an OpenGL context for the tests on Linux, with EGL's surfaceless platform
  (`EGL_MESA_platform_surfaceless`) and Mesa's llvmpipe, in `internal/testing` beside
  `glcontext_darwin.go`, so that CI runs the drawing tests on Linux OpenGL and OpenGL ES too.
- `TestImageBlendFactor` makes ten thousand blend pipelines, and the CI runners' virtual Metal GPU
  fails to compile one of them now and then; CI skips it on Metal.
- Metal's live-resize and fullscreen presentation now hangs on the window state ggwd sets, and
  no resize was dragged nor fullscreen entered since; the path is the one AppKit's answers took
  before, at the frame's start rather than at each query.
- DirectX's refusal of a transparent surface is returned but never seen here;
  only Metal and OpenGL ran a transparent window.
