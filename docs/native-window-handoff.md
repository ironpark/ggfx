# Handoff: step 2, native window hooks

This is the brief for the next piece of work on ggfx and ggui: make ggfx own
the native window so that ggui stops reaching around it. It records what is
true today, what the goal is, what to build, and how to check it. Read
`docs/window.md` and `FORK.md` first; this document assumes them.

## Implementation status (2026-09-22)

The implementation below is now in ggfx and the adjacent ggui checkout:

- Dialogs use the initiating App's ggfx window handle; the Cocoa/Win32 window
  enumeration helpers are gone.
- DragEvent is wired through Cocoa and Windows IDropTarget. ggui consumes these
  events, with no class patching or drag-position polling.
- Accessibility uses ggfx's owned macOS container and Windows WM_GETOBJECT hook.
  ggui's element/provider objects resolve their own bridge, not a process-global
  current bridge; the custom container and HWND subclass were removed.
- CompositionEvent, caret/context setters and committed-text replacement ranges
  feed ggui's per-window session queues on macOS and Windows. The old ggui IME view
  and Tick dependency are gone. X11/browser keep their exp/textinput backends.
- KeyEvent carries modifier state at the event. This also fixes native shortcuts
  when a modifier's separate key event is absent or its release precedes a frame.

The final API is documented in `docs/window.md`. Two additions to the suggested
shape were necessary: SetAccessibilityHandlers supplies a lazy platform tree
without modifying the owned view class, and SetTextInputContext plus TextEvent
replacement offsets preserves macOS accent-menu edits to surrounding text.

Validation performed:

- ggfx's full Metal and OpenGL test suites; Windows amd64 and js/wasm builds.
- A cgo-disabled macOS build and cgo-disabled Windows builds.
- The two-window, 30-frame close example.
- `examples/nativehooks -smoke` with two Metal windows and one OpenGL window:
  real Cocoa method calls check composition/commit ordering, UTF-16 ranges,
  replacement offsets, caret rectangle size, drag phases/coordinates and the
  owned accessibility container. This is deterministic injection, not a human
  typing with a system IME.
- ggui's full test suite and Windows/wasm builds through go.work; targeted
  textinput/a11y race tests.
- Gallery and Settings launched and rendered. Settings accepted keyboard input,
  Cmd+A and Korean clipboard text; its accessibility tree exposed the controls
  and accepted a Korean text-field value through the accessibility API. An
  accessibility-enabled idle Settings process reported 0.0% CPU.

Still requiring platform/manual validation: real Korean/Japanese IME candidate
interaction, a Finder drag in and out without dropping, VoiceOver speech,
sqlite Cmd+O sheet ownership, and Windows OLE/IMM/UIA runtime behavior. Windows
was cross-built only. The original brief below remains as design history.

## Where things stand

ggfx (this repo) is an ebiten fork. The window layer is `internal/glfw`,
which is *already a Go port of GLFW*: macOS is Cocoa through
`ebitengine/purego/objc` (`cocoa_window_darwin.go`, 2.1k lines), Windows is
Win32 through lazily loaded DLLs (`win32_window_windows.go`,
`api_windows.go`). No cgo on either. Linux and the BSDs still use X11 through
cgo. So "replace GLFW with purego Cocoa/Win32" does **not** mean writing a
new platform layer from scratch. It means ggfx should expose, from the
platform layer it already owns, the hooks ggui needs, and drop the GLFW
shape where it gets in the way.

`internal/ui` is split into process state (`UserInterface`) and per-window
state (`glfwBackend`, `desktopWindow`, `appWindow`), with an event-driven
loop (`loop_glfw.go`, `event.go`, `window_app_glfw.go`). The public API is
`ggfx.Run(Handler)`, `ggfx.NewWindow`, `*ggfx.Window`, and the event types
in `app.go`. `Window.NativeHandle()` returns the NSWindow or HWND.

ggui (`../ggui`) runs on that API (`app.go`, `App` implements
`ggfx.Handler`). It has four pieces of native code that work around ggfx
rather than through it. All are purego, none cgo:

| ggui file | what it does | how it finds its way in |
|---|---|---|
| `internal/platform/cocoa/cocoa.go` | shared AppKit helpers; `AppWindow()` | walks `NSApp.windows` and picks the one whose content view `isKindOfClass:` **`GLFWContentView`** (class name lookup) |
| `internal/platform/win32/win32.go` | `AppWindow()` | `EnumWindows`, matches GLFW's window class name `_GLFW_WNDCLASSNAME`, visible one wins |
| `internal/platform/drag/drag_darwin.go` | reports a file drag *before* the drop (`DragOver`/`DragExit` for `ggui.DragHandler`) | `class_addMethod` of `draggingUpdated:`, `draggingExited:`, `draggingEnded:` onto **GLFW's** `GLFWContentView` class, once |
| `internal/textinput/textinput_darwin.go` | IME: marked text, composition, caret rect (`NSTextInputClient`) | adds its own NSView as a subview of the window's `contentView` and `makeFirstResponder:` on it; polls through ggfx `Tick`/before-update hooks |
| `a11y/a11y_darwin.go` | accessibility bridge; every painted node is an `NSAccessibilityElement` child of a container view | adds an invisible container NSView over the window's content view; refuses the mouse |
| `a11y/a11y_windows.go` | UI Automation provider | `SetWindowSubclass` on the HWND, answers `WM_GETOBJECT` |
| `runtime/dialog_darwin.go`, `dialog_windows.go` | open/save panels as sheets on the app window | need the NSWindow / HWND; run on the main thread |

They reach the main thread with `ggfx.RunOnMainThread` (7 call sites) and
time with `ggfx.Tick` (2). Nothing else of ggfx is used from these files.

What is fragile about that: a class name that belongs to GLFW, a window
found by enumeration (breaks with two windows, and with panels), methods
grafted onto a view class ggfx owns, and a first responder swap ggfx does
not know about (key events for the IME view versus ggfx's key callback).
`docs/window.md` also lists the current limits: no IME events on the event
path, `TextEvent` is committed characters only.

## Goal

ggfx owns the native window and view; ggui asks ggfx for what it needs.
After this step:

- ggui has no class-name lookups and no window enumeration. It gets the
  window from `*ggfx.Window` (`NativeHandle()` or a typed accessor).
- ggui does not add methods to ggfx's classes and does not insert views into
  the content view. Drag-over, IME and accessibility attach through hooks
  ggfx exposes per window.
- Multi-window works for all of them: each hook is per `*ggfx.Window`.
- Nothing in ggui's public API changes. `ggui.DragHandler`, the editor's
  IME behaviour, `a11y` and `runtime` dialogs keep working as they do.

Do not port X11 to purego in this step. Do not rewrite the GLFW port; extend
it.

## Design to implement

Keep the API small and typed, on the root package, mirroring the event
model that already exists. Suggested shape (adjust names to what reads well
next to `app.go`):

```go
// Drag: file drags over the window, before the drop. Positions are DIP.
type DragEvent struct {
    Window *Window
    Phase  DragPhase   // DragEntered, DragMoved, DragExited, DragEnded
    X, Y   float64
    Files  fs.FS       // when the platform can tell early; nil otherwise
}
```

Delivered as an ordinary event through `Handler.HandleEvent`. On macOS the
GLFW port's `GLFWContentView` already implements `draggingEntered:` and
`performDragOperation:`; add `draggingUpdated:`, `draggingExited:` and
`draggingEnded:` to the class registration in `cocoa_window_darwin.go` and
route them to `pushEvent`. On Windows use `IDropTarget` (`RegisterDragDrop`)
or `WM_DROPFILES` plus `DragQueryPoint`; the GLFW port already handles
`WM_DROPFILES`, so `DragEnter`/`DragOver`/`DragLeave` via `IDropTarget` is
the natural addition. Then `ggui/drop.go` stops importing
`internal/platform/drag` and follows `DragEvent`.

```go
// IME: composition on the event path.
type CompositionEvent struct {
    Window *Window
    Text   string  // marked text
    Start, End int // selection within Text, in bytes
    Done   bool    // composition ended; Text is empty then
}
func (w *Window) SetTextInputRect(x, y, w, h float64) // where the candidate window goes
func (w *Window) SetTextInputEnabled(bool)            // first responder on/off
```

On macOS make `GLFWContentView` (or a per-window sibling view ggfx creates)
the `NSTextInputClient`. The methods ggui's `textinput_darwin.go` implements
(`hasMarkedText`, `markedRange`, `selectedRange`, `setMarkedText:`,
`unmarkText`, `insertText:`, `firstRectForCharacterRange:`,
`doCommandBySelector:`) move into the GLFW port and post events. `insertText:`
becomes `TextEvent`; marked text becomes `CompositionEvent`. On Windows use
`WM_IME_COMPOSITION` / `ImmGetCompositionStringW` and
`ImmSetCandidateWindow` for the rect; the GLFW port has an IME stub area in
`win32_window_windows.go` to extend. Then ggui's `internal/textinput` keeps
its `Field`/`session`/`composer` model but its darwin backend is a thin
adapter over the events, and the `Tick`-driven polling goes.

```go
// Accessibility: a place to hang the platform tree.
func (w *Window) AccessibilityView() uintptr   // macOS: an NSView ggfx owns, over the content view
// Windows: WM_GETOBJECT forwarded as a hook.
func (w *Window) SetGetObjectHandler(func(wparam, lparam uintptr) (uintptr, bool))
```

The a11y bridge is large and platform-specific; do not move it into ggfx.
Give it a stable attach point per window and leave the element classes in
ggui. On macOS ggfx creates the container view (mouse-transparent, as ggui's
is now) so that a11y no longer touches the content view. On Windows ggfx
already owns the window procedure; forward `WM_GETOBJECT` instead of letting
ggui subclass the HWND.

```go
// Dialogs need only the handle and the main thread; both exist.
```

`runtime/dialog_*.go` switch from `cocoa.AppWindow()` / `win32.AppWindow()`
to the handle of the window that asked, passed in by the caller.

Main thread: `ggfx.RunOnMainThread` stays. Consider a `Window`-scoped
variant only if a hook needs it.

Delete from ggui once the above lands: `internal/platform/cocoa/AppWindow`,
`internal/platform/win32/AppWindow`, `internal/platform/drag`, the view and
responder code in `internal/textinput/textinput_darwin.go`, the container
view in `a11y/a11y_darwin.go`, the subclassing in `a11y/a11y_windows.go`.
Keep `cocoa.String`/`Array` helpers if still used.

Order of work, each step shippable on its own:

1. Window handle plumbing: ggui gets NSWindow/HWND from `*ggfx.Window`.
   Delete both `AppWindow()` lookups. Smallest step, unblocks multi-window
   dialogs.
2. Drag events. Small, self-contained, and the test is visual.
3. Accessibility attach points. Mechanical; the bridge itself is unchanged.
4. IME events. Largest; touches the editor. Do macOS first, then Windows.

## Ground rules that already apply

- No cgo on macOS or Windows. Everything through purego / syscall.
- Every GLFW call and every AppKit/Win32 call is on the main thread; use
  `mainThread.Call` inside `internal/ui`, `RunOnMainThread` outside.
- Events are pushed with `pushEvent` and drained once per loop iteration;
  a hook that changes what the app should draw must also request a frame
  (`RequestFrame`) or the loop waits.
- DIP everywhere at the API; convert with `cursorPositionInDIP` /
  `clientPositionInDIP` as the input code does.
- `docs/window.md` is the contract. Update its event table and the
  "what the event path does not do" list as items move off it.
- Commit messages in the imperative, one change per commit, as in `git log`.

## How to verify

ggfx:

```
go test -count=1 ./...                          # macOS Metal (default)
EBITENGINE_GRAPHICS_LIBRARY=opengl go test -count=1 ./...
GOOS=windows GOARCH=amd64 go build ./...        # cross-build
GOOS=js GOARCH=wasm go build ./...
go run ./examples/multiwindow -frames 30 -close  # two windows, exits 0
```

ggui (with `go.work` pointing at `../ggfx`, then again with `GOWORK=off`
after pinning):

```
go test -count=1 ./...
go run ./examples/gallery        # drag a file over the drop preview, type Korean into a field, VoiceOver on
go run ./examples/settings
```

Manual checks that have no test today: Korean/Japanese composition in the
gallery's Text preview with the caret rect following the caret; a file
dragged over and out of the drop zone without dropping; VoiceOver reading
the gallery; Cmd+O in the sqlite example opening a sheet attached to the
right window. Watch idle CPU stays at 0% in Activity Monitor after each
change; the last regression here was a hook that requested frames forever.

## Things learned the hard way

- `objc.RegisterClass` for a name that exists panics: register once, keep a
  map from view to Go state (see `displaylink_macos.go`, `delegateViews`).
- The Metal driver must clear a drawable only on its first pass of the
  frame (`Surface.cleared`); apps draw to the screen many times per frame.
- `go get module@main` can resolve to a stale proxy entry. Pin by commit
  hash with `GOPROXY=direct GONOSUMDB=github.com/ironpark`.
- `timeout` does not exist on macOS; background the process and `kill`.
- Screenshots via `screencapture -x` plus a small Go program that counts
  dark pixels are a fast way to bisect a rendering problem.
