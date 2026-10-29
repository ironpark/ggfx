# Splitting the window system out of ggfx

ggfx is to become a renderer: it draws images, shaders, text, vectors and SVG, and presents them
on surfaces that someone else's windows own. The window system it carries today moves to a module
of its own ([ggwd](https://github.com/ironpark/ggwd)), which uses ggfx's public API alone, as ggip's `ggfxinput` does.
ggui joins the two.

## What goes and what stays

Stays in ggfx:

- `Image`, `Shader`, `Uniforms`, `GeoM`, `ColorScale`, `Blend`, the draw options, `text/v2`,
  `vector`, `svg`.
- The graphics drivers, the atlas, the command queue and the render thread.
- A new device and surface API (below).

Goes to ggwd:

- `Run`, `NewWindow`, `Window`, `WindowOptions`, `RunOptions` and every event type in `app.go`.
- Keys, mouse buttons, touch, cursor, clipboard, monitor, color mode, `KeyName`.
- `RunOnMainThread`, `Wake`, `Termination`.
- `exp/textinput`, IME composition, drag and drop, accessibility, native hooks.
- `internal/ui` (less what moves to `internal/render`), `internal/glfw`, `internal/cocoa`'s window
  parts, `internal/hook`, `internal/windowsystem`, `internal/exeicon`, `internal/microsoftgdk`,
  `internal/winver` where only windows use it.

## The renderer API

The atlas and the command queue are process-wide, so the renderer is too. Package `host` has it,
apart from the drawing API an app uses:

```go
func Init(o *Options) error     // GraphicsLibrary, ColorSpace, SingleThread, MainThread
func Terminate()

func CurrentDriver() Driver     // Library, OpenGLES (which context to create), DirectComposition
func SetGLContext(c GLContext)  // the hidden context OpenGL draws in on the desktop

type SurfaceTarget interface{ /* sealed */ }
type NSView uintptr             // Metal's target; the view becomes the layer's alone
type HWND uintptr               // DirectX's target
type GLWindow struct{ Context GLPresenter } // OpenGL's on the desktop
type Canvas js.Value            // WebGL's; the first surface creates the driver for it

func NewSurface(target SurfaceTarget, transparent bool) (*Surface, error)
func (s *Surface) FramebufferSize() (int, int, bool)
func (s *Surface) Dispose()

func BeginFrame() (Frame, error) // also reports the error a read of pixels ran into
func (f Frame) Screen(s *Surface, width, height int) *ggfx.Image // cleared where needed
func (f Frame) End() error
func Flush(present bool) (FlushResult, error) // runs the frames' commands, presents; Occluded
func FinishForcedFrame() error  // after a frame drawn during a live resize
```

Pacing, occlusion, live resize and which windows draw are the window system's: the frame pacer,
which tells a present that waits for the display from one that does not, moved back to
`internal/ui` on top of `Flush` and its `Occluded`. The GL context is set after `Init` rather
than passed to it, because which context to create depends on the driver `Init` chose
(`Driver.OpenGLES`). The API was first in ggfx itself, and moved to `host` once ggwd was out, so
that an app drawing with ggfx does not see it.

`internal/rendertest` drives this API with no window, on Metal, which renders without a surface.

## Steps

Each step builds and passes the tests on macOS (Metal and OpenGL) and cross-builds for Windows,
Linux and wasm, and is a commit of its own.

1. Done. Move the renderer core out of `internal/ui` into `internal/render`: `Image`, `Shader`,
   the uniform packing, the white image, the error state, reading pixels inside a frame, driver
   creation, the frame pacer and the tick. `internal/ui` drives it. The render thread is still
   started by the loop, which owns the main thread it shares a goroutine group with; step 2
   moves it.
2. Done. Add the public API above to ggfx, start the render thread at `Init`, and make Metal
   take an NSView. `internal/ui` uses nothing from `internal/render` that the API does not
   expose, and no other renderer package: what is left is to spell `render.X` as `ggfx.X`.
3. Done. Move the window system into `ggfx/ggwd` with its own internal packages, importing
   nothing from `ggfx/internal`; `TestNoRendererInternals` fails if it does. ggfx's tests run in
   a frame of the renderer with no window, OpenGL on macOS in a CGL context of their own, and
   ggwd's run in a window.
4. Done. Move `ggfx/ggwd` to its own repository, github.com/ironpark/ggwd, with its history
   since the move; point ggui and ggip's ggfxinput at it, and move `docs/window.md` and the window
   sections of `FORK.md` there. Go's internal rule now keeps ggwd off ggfx's internals, so the
   import test went.
5. Done. Count the tick in flushes inside the renderer. `ggfx.Tick` and `host.AdvanceTick` are
   gone: the caches that expire against the tick are the renderer's, and a loop flushes once an
   iteration anyway. ggwd counts its own iterations for the monitor cache and `exp/textinput`.
6. Done. Drop what the renderer kept for windows and consoles it no longer has:
   `internal/microsoftgdk` and the DirectX 12 driver's Xbox paths, and `internal/windowsystem`,
   which only decided whether the Linux OpenGL loader tried OpenGL as well as OpenGL ES.

## Open questions

- The browser has one canvas and its own frame loop through `requestAnimationFrame`; that loop
  belongs to ggwd, and the WebGL driver's single surface stays a renderer limit.
- `RunOptions.ColorSpace` and `GraphicsLibrary` become `InitOptions`; ggwd passes them through.
- A live resize on macOS draws from the main thread, in the resize callback, and only GLFW's
  path was run; the NSView switch did not change it, but no resize was dragged after it.
