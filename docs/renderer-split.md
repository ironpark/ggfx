# Splitting the window system out of ggfx

ggfx is to become a renderer: it draws images, shaders, text, vectors and SVG, and presents them
on surfaces that someone else's windows own. The window system it carries today moves to a module
of its own (working name `ggwin`), which uses ggfx's public API alone, as ggip's `ggfxinput` does.
ggui joins the two.

## What goes and what stays

Stays in ggfx:

- `Image`, `Shader`, `Uniforms`, `GeoM`, `ColorScale`, `Blend`, the draw options, `text/v2`,
  `vector`, `svg`.
- The graphics drivers, the atlas, the command queue and the render thread.
- A new device and surface API (below).

Goes to ggwin:

- `Run`, `NewWindow`, `Window`, `WindowOptions`, `RunOptions` and every event type in `app.go`.
- Keys, mouse buttons, touch, cursor, clipboard, monitor, color mode, `KeyName`.
- `RunOnMainThread`, `Wake`, `Termination`.
- `exp/textinput`, IME composition, drag and drop, accessibility, native hooks.
- `internal/ui` (less what moves to `internal/render`), `internal/glfw`, `internal/cocoa`'s window
  parts, `internal/hook`, `internal/windowsystem`, `internal/exeicon`, `internal/microsoftgdk`,
  `internal/winver` where only windows use it.

## The renderer API

The atlas and the command queue are process-wide, so the device is too.

```go
// Init creates the graphics device and starts the render thread. It is called once, on the
// main thread.
func Init(o *InitOptions) error

type InitOptions struct {
	GraphicsLibrary GraphicsLibrary
	ColorSpace      ColorSpace
	SingleThread    bool
	// GLContext is the context the OpenGL driver draws in on the desktop. Every surface's
	// context shares objects with it.
	GLContext GLContext
	// MainThread runs a function on the OS main thread and waits for it. Metal and OpenGL
	// need it for their main-thread calls.
	MainThread func(func())
}

type GLContext interface{ MakeContextCurrent() error }

// GLPresenter is a window's own OpenGL context, which a frame is copied to and swapped on.
type GLPresenter interface {
	MakeContextCurrent() error
	SwapInterval(int) error
	SwapBuffers() error
}

// NewSurface makes a presentation target: a CAMetalLayer's host NSView or NSWindow on macOS,
// an HWND on Windows, a GLPresenter for desktop OpenGL, a canvas in the browser.
func NewSurface(target any, o *SurfaceOptions) (*Surface, error)

// Screen returns the image presented on s, sized width by height. A size other than the
// last one replaces the image.
func (s *Surface) Screen(width, height int) *Image
func (s *Surface) Dispose()

// BeginFrame and EndFrame bracket the draws to screens. EndFrame presents every surface whose
// screen was drawn to when present is set.
func BeginFrame() error
func EndFrame(present bool) error

// Tick advances the clock the image GC and the glyph caches expire against.
func Tick()
```

Pacing, occlusion, live resize and which windows draw are the window system's: it calls
`BeginFrame`, draws, and `EndFrame` from wherever its loop or its resize callback is.

## Steps

Each step builds and passes the tests on macOS (Metal and OpenGL) and cross-builds for Windows,
Linux and wasm, and is a commit of its own.

1. Done. Move the renderer core out of `internal/ui` into `internal/render`: `Image`, `Shader`,
   the uniform packing, the white image, the error state, reading pixels inside a frame, driver
   creation, the frame pacer and the tick. `internal/ui` drives it. The render thread is still
   started by the loop, which owns the main thread it shares a goroutine group with; step 2
   moves it.
2. Add the public API above to ggfx, and make `internal/ui` use only what it exposes plus the
   window internals.
3. Move the window system into `ggfx/ggwin` with its own internal packages, importing nothing from
   `ggfx/internal`. A test fails the build if it does. Rebuild `internal/testing` on the renderer
   API: Metal renders without a surface, and OpenGL needs a context that the test harness gets
   from ggwin, so the root package's tests run on Metal alone and ggwin runs the OpenGL ones.
4. Move `ggfx/ggwin` to its own repository, point ggui at it, and drop `docs/window.md` and the
   window sections of `FORK.md` from ggfx.

## Open questions

- Metal's surface takes an NSWindow and owns its view's resize handling and display link. Taking
  the NSView instead would let a window system that is not GLFW host it.
- The browser has one canvas and its own frame loop through `requestAnimationFrame`; that loop
  belongs to ggwin, and the WebGL driver's single surface stays a renderer limit.
- `RunOptions.ColorSpace` and `GraphicsLibrary` become `InitOptions`; ggwin passes them through.
