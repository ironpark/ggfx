# Windows and the event loop

ggfx drives a GUI, not a game. A GUI has any number of windows, redraws only
when something changed, and reacts to input as events rather than by polling a
per-tick snapshot. This document is the contract for that model. The legacy
`Game`/`RunGame` path stays for now; it is what the test suite drives, and it
is implemented on top of the same machinery as one window that asks for a
frame every iteration.

## Layers

```
ggfx.Run / ggfx.Window / ggfx.Event         public API (root package)
internal/ui: UserInterface + *window       one process, N windows, one loop
internal/graphicsdriver: Graphics + Surface one device, N presentation targets
```

### Driver: `Surface`

A `Surface` is one presentation target: a CAMetalLayer on macOS, a DXGI swap
chain on Windows, the default framebuffer of a GL context, or the canvas in a
browser. The driver owns one device and any number of surfaces.

```go
type Graphics interface {
    ...
    // NewSurface creates a presentation target for a native window. The
    // argument is what the platform's UI layer has: an NSWindow or HWND
    // handle, or an opengl.Presenter for GL.
    NewSurface(target any) (Surface, error)
}

type Surface interface {
    // NewScreenImage returns the image that is presented on this surface.
    // Calling it again replaces the previous screen image; the surface
    // resizes its backing store as needed.
    NewScreenImage(width, height int) (Image, error)
    Dispose()
}
```

`End(FlushModePresent)` presents every surface whose screen image was drawn
to since the previous flush. A surface that was not drawn is left alone, so a
frame for one window does not disturb the others.

`NewScreenFramebufferImage` on `Graphics` is gone; the UI layer always goes
through a surface.

Per-driver status:

| driver | surfaces | notes |
|---|---|---|
| Metal | many | one `view` (layer, display link, drawable) per surface |
| DirectX 11/12 | many | one `graphicsInfra` swap chain per surface |
| OpenGL desktop | one | GL contexts do not share VAOs/FBOs; a second surface returns an error until the state cache is per-context |
| WebGL | one | the canvas |

### UI: `window`

`internal/ui` splits the former single-window backend into process state and
per-window state.

Process state (`UserInterface`): main thread, graphics driver, monitors,
input time, error, the list of windows, and the loop.

Per-window state (`*window`): the platform handle (`*glfw.Window`, or the
canvas), its `Surface`, the DIP size and position bookkeeping, fullscreen
restore data, cursor shape and mode, the input recorder, and a `frameDriver`
that decides what a frame for this window does.

Two frame drivers exist:

- `gameContext` is the legacy driver: offscreen image, letterboxing, ticks
  and `Update`, draw skipping. It is created for the primary window of
  `Run(game)`.
- `eventContext` is the GUI driver: it hands the screen image to the app as
  a `FrameEvent` and does nothing else. No offscreen, no scaling, no ticks.

### Loop

One iteration of the loop, on the main thread:

1. Pump the OS event queue. `PollEvents` if any window has a frame pending,
   `WaitEvents` otherwise. `ScheduleFrame`/`RequestFrame` post an empty event
   so a wait wakes up.
2. Drain recorded input into the app goroutine as events.
3. For every window with a frame pending (or, for `gameContext`, always):
   compute its outside size and pixel size, run its frame driver between
   `atlas.BeginFrame`/`EndFrame`.
4. `atlas.FlushCommands(present)` once, which presents every drawn surface.
5. Pace: the vsync-ignored detection and unfocused sleep from the legacy loop
   apply once per iteration, not per window.

Window creation, destruction and every GLFW call go through the main thread
as before. Frames are rendered on the render thread in multithread mode; N
windows are drawn sequentially there.

## Public API

```go
type Handler interface {
    HandleEvent(Event) error
}

// Run starts the loop and blocks until it ends. The handler receives
// StartEvent first and creates its windows there. Returning Termination
// ends the loop; so does closing the last window unless
// RunOptions.KeepRunningWithoutWindows is set.
func Run(h Handler, opts *RunOptions) error

type WindowOptions struct {
    Title              string
    Width, Height      int   // DIP; 0 uses 640x480
    Resizable          bool
    Decorated          bool  // default true (zero value means decorated)
    Floating           bool
    Hidden             bool  // create hidden; Show later
    Transparent        bool
    Position           *image.Point // DIP on the monitor; nil centers
    MinWidth, MinHeight, MaxWidth, MaxHeight int
}

func NewWindow(o *WindowOptions) (*Window, error) // only while Run is running

type Window struct{ ... }
func (w *Window) Close()
func (w *Window) RequestFrame()          // ask for one FrameEvent
func (w *Window) SetTitle(string)
func (w *Window) Size() (int, int)       // DIP
func (w *Window) SetSize(int, int)
func (w *Window) Position() (int, int)
func (w *Window) SetPosition(int, int)
func (w *Window) SetSizeLimits(minw, minh, maxw, maxh int)
func (w *Window) SetResizable(bool)
func (w *Window) Show(); Hide(); IsVisible() bool
func (w *Window) Maximize(); Minimize(); Restore()
func (w *Window) SetFullscreen(bool); IsFullscreen() bool
func (w *Window) IsFocused() bool; Focus()
func (w *Window) DeviceScaleFactor() float64
func (w *Window) Monitor() *MonitorType
func (w *Window) SetCursorShape(CursorShapeType); SetCursorMode(CursorModeType)
func (w *Window) SetIcon([]image.Image)
func (w *Window) NativeHandle() uintptr  // NSWindow* / HWND / X11 window
```

Events. Every event has `Window() *Window`; `StartEvent` has a nil window.

| event | fields | when |
|---|---|---|
| `StartEvent` | | once, before anything else |
| `FrameEvent` | `Screen *Image` (pixels), `Scale float64` | after `RequestFrame`, after a resize, after the window is first shown |
| `ResizeEvent` | `Width, Height int` (DIP), `Scale float64` | size or scale changed |
| `FocusEvent` | `Focused bool` | |
| `CloseEvent` | | the user asked to close; the window closes unless the handler calls `w.KeepOpen()` during the event |
| `KeyEvent` | `Key`, `Pressed`, `Repeat bool` | OS key repeat is delivered with `Repeat` set |
| `TextEvent` | `Text string` | committed characters |
| `MouseMoveEvent` | `X, Y float64` (DIP) | |
| `MouseButtonEvent` | `Button`, `Pressed`, `X, Y` | |
| `ScrollEvent` | `X, Y float64` | |
| `TouchEvent` | `ID TouchID`, `Phase`, `X, Y` | browser only for now |
| `DropEvent` | `Files fs.FS` | |

Coordinates in input events are DIP relative to the window's client area;
`FrameEvent.Screen` is in physical pixels and `Scale` converts between them.

## What the event path does not do

- No ticks, no `TPS`, no `inpututil`. `KeyEvent.Repeat` replaces
  `KeyPressDuration`. Apps that need key state keep it from the events.
- No IME on the event path yet. `TextEvent` carries committed characters
  from the platform's character callback. Composition (`exp/textinput`) is
  still tied to the legacy tick hooks.
- One window on OpenGL and WebGL.
- Gamepads are not delivered as events.
- `Monitor()` on the root package still means the primary window's monitor;
  use `Window.Monitor()`.

## Migration

The legacy `Game` path is unchanged for callers. Internally it is one window
with a `gameContext` and a frame every iteration. `ScreenSize`, `Monitor`,
`SetWindowTitle` and the other root-level window functions address that
primary window and are not available to `Run` handlers, which use `*Window`.
