# Windows and the event loop

ggfx drives a GUI, not a game. A GUI has any number of windows, redraws only
when something changed, and reacts to input as events rather than by polling a
per-tick snapshot. This document is the contract for that model. The legacy
`Game`/`RunGame` path stays for now, implemented on top of the same machinery
as one window that asks for a frame every iteration, but it has no callers
left: the test suite opens a window through `Run`.

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
| DirectX 11/12 | one | the swap chain is still on the device; a second surface returns an error until `graphicsInfra` is per surface |
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

- `context` is the legacy driver: offscreen image, letterboxing, ticks
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
3. For every window with a frame pending (or, for `context`, always):
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
type HandlerFunc func(Event) error

// Run starts the loop and blocks until it ends. The handler receives
// StartEvent first and creates its windows there. Returning Termination
// ends the loop; so does closing the last window. RunOptions is
// RunGameOptions; the window-related options apply to every window.
func Run(h Handler, options *RunOptions) error

// The zero value is a decorated, visible, fixed-size 640x480 window
// centered on the monitor.
type WindowOptions struct {
    Title              string
    Width, Height      int          // DIP
    Position           *image.Point // DIP on the monitor; nil centers
    Resizable, Undecorated, Floating, Hidden, Maximized, Transparent, Unfocused bool
    MinWidth, MinHeight, MaxWidth, MaxHeight int
}

func NewWindow(o *WindowOptions) (*Window, error) // only while Run runs

type Window struct{ ... }
func (w *Window) Close()                 // closes after the current event
func (w *Window) RequestFrame()          // ask for one FrameEvent
func (w *Window) SetTitle(string)
func (w *Window) Size() (int, int)       // DIP
func (w *Window) SetSize(int, int)
func (w *Window) Position() (int, int)
func (w *Window) SetPosition(int, int)
func (w *Window) SetSizeLimits(minw, minh, maxw, maxh int)
func (w *Window) SetResizable(bool); SetDecorated(bool); SetFloating(bool)
func (w *Window) Show(); Hide(); IsVisible() bool
func (w *Window) Maximize(); Minimize(); Restore(); IsMaximized(); IsMinimized()
func (w *Window) SetFullscreen(bool); IsFullscreen() bool
func (w *Window) IsFocused() bool; Focus(); RequestAttention()
func (w *Window) SetIcon([]image.Image); SetMousePassthrough(bool)
func (w *Window) DeviceScaleFactor() float64
func (w *Window) Monitor() *MonitorType
func (w *Window) CursorShape(); SetCursorShape(CursorShapeType)
func (w *Window) CursorMode(); SetCursorMode(CursorModeType)
func (w *Window) NativeHandle() uintptr  // NSWindow* / HWND / X11 window
func (w *Window) SetTextInputEnabled(bool)
func (w *Window) SetTextInputRect(x, y, width, height float64) // caret in client DIP
func (w *Window) SetTextInputContext(before, after string)
func (w *Window) AccessibilityView() uintptr // macOS NSView owned by ggfx; 0 elsewhere
func (w *Window) SetAccessibilityHandlers(children func() uintptr, hitTest func(x, y float64) uintptr)
func (w *Window) SetGetObjectHandler(func(wparam, lparam uintptr) (uintptr, bool))
```

Events. Every event but `StartEvent` has a `Window *Window` field.

| event | fields | when |
|---|---|---|
| `StartEvent` | | once, before anything else |
| `ResizeEvent` | `Width, Height float64` (DIP), `Scale` | right after creation, and whenever the size or scale changes |
| `FrameEvent` | `Screen *Image` (pixels), `Scale float64` | after `RequestFrame`, after a resize, when the window is first shown |
| `FocusEvent` | `Focused bool` | |
| `CloseEvent` | | the user asked to close; the window closes after the event unless `KeepOpen()` was called |
| `KeyEvent` | `Key`, `Pressed`, `Repeat bool`, `Modifiers KeyModifiers` | OS key repeat; modifiers captured at the event, before later releases |
| `TextEvent` | `Text string`, `HasReplacement`, `ReplacementStart, ReplacementEnd int` | committed text; optional UTF-8 byte replacement offsets relative to the supplied context's caret |
| `CompositionEvent` | `Text string`, `Start, End int`, `Done bool` | macOS/Windows marked text; selection is UTF-8 bytes within Text; Done ends the composition with empty Text |
| `MouseMoveEvent` | `X, Y float64` (DIP) | |
| `MouseButtonEvent` | `Button`, `Pressed`, `X, Y` | |
| `ScrollEvent` | `X, Y float64` | |
| `TouchEvent` | `ID TouchID`, `Phase`, `X, Y` | browser only for now |
| `DropEvent` | `Files fs.FS` | |
| `DragEvent` | `Phase DragPhase`, `X, Y float64`, `Files fs.FS` | macOS/Windows file drag entered, moved, exited or ended; Files can be nil |

Coordinates in input events are DIP relative to the window's client area;
`FrameEvent.Screen` is in physical pixels and `Scale` converts between them.
`Screen` is valid during the event only.

`Window.Close()` closes without a `CloseEvent`; the event is for the user's
close button. `Run` returns when the last window closes.

### Native hooks

Native input and accessibility hooks are per window and main-thread marshalled.
Text input is initially enabled. SetTextInputEnabled(false) cancels composition
while leaving plain committed characters and keyboard focus available. Enable it
when an editor gains focus, update the caret rectangle after layout or scrolling,
and supply the surrounding text with SetTextInputContext. Do not disable it
between commits: a Korean IME can commit a syllable and mark the next one in the
same OS event. CompositionEvent and TextEvent retain their native order.

macOS uses the existing content view as its NSTextInputClient. Its native
selection and replacement ranges are converted from UTF-16 to UTF-8. Windows
uses IMM composition/result messages and positions the native candidate panel.
Committed result messages are handled once, without a duplicate WM_CHAR.

macOS accessibility gets a lazily created, mouse-transparent NSView owned and
destroyed by ggfx. SetAccessibilityHandlers supplies that view's children as an
autoreleased NSArray and its hit test as an NSAccessibilityElement; hit-test
coordinates are native screen points. The bridge owns its element objects, not
the view. Windows forwards WM_GETOBJECT through SetGetObjectHandler; false
falls through to the default procedure. These synchronous native callbacks
must answer from a snapshot: do not call Window methods or wait for the event
handler from them. Detach callbacks and retire bridge objects before closing
their window. Unsupported platforms return zero for the view and ignore the
accessibility/IME setters.

File drags use Cocoa dragging callbacks and Windows IDropTarget, with one OLE
registration per window, revoked during teardown. X11 and the browser retain
DropEvent but do not emit pre-drop DragEvent. Native drag and composition
callbacks request a frame when state changes; they do not add a polling loop.
Dialogs use the initiating window's NativeHandle, obtained before entering
RunOnMainThread.

Run `go run ./examples/nativehooks` to exercise text and file drags in two Metal
windows. `-smoke` injects Cocoa callbacks and checks window isolation, UTF-16
selection conversion, Korean commit/next-mark ordering, replacement ranges,
caret rectangle size, drag phases/coordinates and accessibility attachment.
Use `-windows 1` with OpenGL or DirectX. The smoke injection is macOS-only.

## What the event path does not do

- No ticks, no `TPS`, no `inpututil`. `KeyEvent.Repeat` replaces
  `KeyPressDuration`. Apps that need key state keep it from the events.
  `Tick()` still advances once per loop iteration, and the before-update
  hooks run then, so `exp/textinput` keeps working when polled every frame.
- No composition events on X11 or the browser yet. Their `exp/textinput`
  backends remain available; macOS and Windows have per-window CompositionEvent.
- One window on OpenGL, WebGL and DirectX.
- Gamepads are not delivered as events.
- `Monitor()` on the root package still means the primary window's monitor;
  use `Window.Monitor()`.

## Migration

The legacy `Game` path is unchanged for callers. Internally it is one window
with a `gameContext` and a frame every iteration. `ScreenSize`, `Monitor`,
`SetWindowTitle` and the other root-level window functions address that
primary window and are not available to `Run` handlers, which use `*Window`.
`ScreenSize` in particular reports the size `Game.Layout` returned, so under
`Run` it stays zero; ask the window with `Window.Size()`.
