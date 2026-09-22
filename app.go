// Copyright 2026 The ggfx Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ggfx

import (
	"errors"
	"image"
	"io/fs"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/ui"
)

// Handler receives the events of an app run by Run.
type Handler interface {
	// HandleEvent handles one event. Returning Termination ends Run without an error.
	HandleEvent(ev Event) error
}

// HandlerFunc is a Handler made of one function.
type HandlerFunc func(ev Event) error

// HandleEvent implements Handler.
func (f HandlerFunc) HandleEvent(ev Event) error {
	return f(ev)
}

// Run starts the event loop and blocks until it ends. The handler receives StartEvent first and
// creates its windows there with NewWindow. Run ends when the handler returns Termination or an
// error, or when the last window closes.
//
// Run must be called on the main thread, and only once in a process.
//
// Run must be called on the main goroutine.
func Run(h Handler, options *RunOptions) error {
	defer runEnded.Store(true)

	op := toUIRunOptions(options)

	if err := ui.Get().RunApp(&appForUI{handler: h}, op); err != nil {
		if errors.Is(err, Termination) {
			return nil
		}
		return err
	}
	return nil
}

// appForUI adapts a Handler to ui.App.
type appForUI struct {
	handler Handler
}

func (a *appForUI) NewScreenImage(window ui.AppWindow, width, height int, surface graphicsdriver.Surface) *ui.Image {
	w := window.Handle().(*Window)
	if w.screen != nil {
		w.screen.Deallocate()
	}
	w.screen = newScreenImage(width, height, surface)
	return w.screen.image
}

func (a *appForUI) HandleEvent(ev ui.Event) error {
	return a.handler.HandleEvent(eventFromUI(ev))
}

func windowFromUI(w ui.AppWindow) *Window {
	if w == nil {
		return nil
	}
	return w.Handle().(*Window)
}

func eventFromUI(ev ui.Event) Event {
	switch ev := ev.(type) {
	case ui.StartEvent:
		return StartEvent{}
	case ui.FrameEvent:
		w := windowFromUI(ev.Window)
		return FrameEvent{Window: w, Screen: w.screen, Scale: ev.Scale}
	case ui.ResizeEvent:
		return ResizeEvent{Window: windowFromUI(ev.Window), Width: ev.Width, Height: ev.Height, Scale: ev.Scale}
	case ui.FocusEvent:
		return FocusEvent{Window: windowFromUI(ev.Window), Focused: ev.Focused}
	case ui.CloseEvent:
		return CloseEvent{Window: windowFromUI(ev.Window), keepOpen: ev.KeepOpen}
	case ui.KeyEvent:
		return KeyEvent{Window: windowFromUI(ev.Window), Key: Key(ev.Key), Pressed: ev.Pressed, Repeat: ev.Repeat, Modifiers: KeyModifiers(ev.Modifiers)}
	case ui.TextEvent:
		return TextEvent{Window: windowFromUI(ev.Window), Text: ev.Text, ReplacementStart: ev.ReplacementStart, ReplacementEnd: ev.ReplacementEnd, HasReplacement: ev.HasReplacement}
	case ui.CompositionEvent:
		return CompositionEvent{Window: windowFromUI(ev.Window), Text: ev.Text, Start: ev.Start, End: ev.End, Done: ev.Done}
	case ui.DragEvent:
		return DragEvent{Window: windowFromUI(ev.Window), Phase: DragPhase(ev.Phase), X: ev.X, Y: ev.Y, Files: ev.Files}
	case ui.MouseMoveEvent:
		return MouseMoveEvent{Window: windowFromUI(ev.Window), X: ev.X, Y: ev.Y}
	case ui.MouseButtonEvent:
		return MouseButtonEvent{Window: windowFromUI(ev.Window), Button: MouseButton(ev.Button), Pressed: ev.Pressed, X: ev.X, Y: ev.Y}
	case ui.ScrollEvent:
		return ScrollEvent{Window: windowFromUI(ev.Window), X: ev.X, Y: ev.Y}
	case ui.TouchEvent:
		return TouchEvent{Window: windowFromUI(ev.Window), ID: TouchID(ev.ID), Phase: TouchPhase(ev.Phase), X: ev.X, Y: ev.Y}
	case ui.DropEvent:
		return DropEvent{Window: windowFromUI(ev.Window), Files: ev.Files}
	case ui.GamepadConnectEvent:
		return GamepadConnectEvent{ID: GamepadID(ev.ID), Name: ev.Name, SDLID: ev.SDLID, Standard: ev.Standard}
	case ui.GamepadDisconnectEvent:
		return GamepadDisconnectEvent{ID: GamepadID(ev.ID)}
	case ui.GamepadButtonEvent:
		return GamepadButtonEvent{ID: GamepadID(ev.ID), Button: GamepadButton(ev.Button), Pressed: ev.Pressed}
	case ui.GamepadAxisEvent:
		return GamepadAxisEvent{ID: GamepadID(ev.ID), Axis: GamepadAxisType(ev.Axis), Value: ev.Value}
	case ui.GamepadStandardButtonEvent:
		return GamepadStandardButtonEvent{ID: GamepadID(ev.ID), Button: StandardGamepadButton(ev.Button), Pressed: ev.Pressed, Value: ev.Value}
	case ui.GamepadStandardAxisEvent:
		return GamepadStandardAxisEvent{ID: GamepadID(ev.ID), Axis: StandardGamepadAxis(ev.Axis), Value: ev.Value}
	default:
		panic("ggfx: unknown ui event")
	}
}

// Gamepad events are delivered only when [RunOptions.Gamepads] is set. A gamepad belongs to the
// process rather than to a window, so they carry no Window.

// GamepadConnectEvent reports a gamepad that appeared. Standard reports whether it has a standard
// layout, and so whether GamepadStandardButtonEvent and GamepadStandardAxisEvent are delivered for
// it.
type GamepadConnectEvent struct {
	ID       GamepadID
	Name     string
	SDLID    string
	Standard bool
}

// GamepadDisconnectEvent reports a gamepad that went away.
type GamepadDisconnectEvent struct {
	ID GamepadID
}

// GamepadButtonEvent reports one of a gamepad's raw buttons changing. Buttons at and above the real
// button count are the hats' four directions.
type GamepadButtonEvent struct {
	ID      GamepadID
	Button  GamepadButton
	Pressed bool
}

// GamepadAxisEvent reports one of a gamepad's raw axes moving, in -1..1.
type GamepadAxisEvent struct {
	ID    GamepadID
	Axis  GamepadAxisType
	Value float64
}

// GamepadStandardButtonEvent reports a standard-layout button changing. Value is 0..1 for an
// analog button such as a trigger; Pressed applies the dead zone an analog button needs, so a
// resting trigger is not pressed even when its value is not quite zero.
type GamepadStandardButtonEvent struct {
	ID      GamepadID
	Button  StandardGamepadButton
	Pressed bool
	Value   float64
}

// GamepadStandardAxisEvent reports a standard-layout axis moving, in -1..1.
type GamepadStandardAxisEvent struct {
	ID    GamepadID
	Axis  StandardGamepadAxis
	Value float64
}

func (GamepadConnectEvent) isEvent()        {}
func (GamepadDisconnectEvent) isEvent()     {}
func (GamepadButtonEvent) isEvent()         {}
func (GamepadAxisEvent) isEvent()           {}
func (GamepadStandardButtonEvent) isEvent() {}
func (GamepadStandardAxisEvent) isEvent()   {}

// Event is what a Handler receives. Every event but StartEvent and the gamepad events belongs to a
// Window.
type Event interface {
	isEvent()
}

// StartEvent is the first event. The handler creates its windows here.
type StartEvent struct{}

// FrameEvent asks the handler to draw Screen. Screen is in physical pixels; Scale converts
// device-independent pixels to physical pixels. Screen is valid during the event only.
type FrameEvent struct {
	Window *Window
	Screen *Image
	Scale  float64
}

// ResizeEvent reports that the client area size or the scale changed. Width and Height are in
// device-independent pixels.
type ResizeEvent struct {
	Window *Window
	Width  float64
	Height float64
	Scale  float64
}

// FocusEvent reports that the window gained or lost the keyboard focus.
type FocusEvent struct {
	Window  *Window
	Focused bool
}

// CloseEvent reports that the user asked to close the window. The window closes after the event
// is handled unless KeepOpen is called.
type CloseEvent struct {
	Window   *Window
	keepOpen func()
}

// KeepOpen keeps the window open.
func (c CloseEvent) KeepOpen() {
	c.keepOpen()
}

// KeyEvent reports a key press or release. Repeat is set for the presses the OS generates while
// the key is held.
type KeyEvent struct {
	Window    *Window
	Key       Key
	Pressed   bool
	Repeat    bool
	Modifiers KeyModifiers
}

// KeyModifiers captures modifier state at the key event, even if a modifier is
// released before the next frame or the platform did not send its own key event.
type KeyModifiers struct{ Shift, Control, Alt, Meta bool }

// TextEvent carries the characters the user typed.
type TextEvent struct {
	Window *Window
	Text   string
	// A native IME may replace surrounding text (for example the macOS accent
	// menu). When HasReplacement is true, these UTF-8 byte offsets are relative
	// to the caret in the context supplied to SetTextInputContext.
	ReplacementStart, ReplacementEnd int
	HasReplacement                   bool
}

// CompositionEvent reports IME marked text on macOS and Windows. Start and End
// are UTF-8 byte offsets within Text. Done ends the composition and Text is empty;
// committed characters arrive separately as TextEvent.
type CompositionEvent struct {
	Window     *Window
	Text       string
	Start, End int
	Done       bool
}

// DragPhase identifies a stage of a file drag before or after the drop.
type DragPhase int

const (
	DragEntered DragPhase = iota
	DragMoved
	DragExited
	DragEnded
)

// DragEvent reports a native file drag on macOS and Windows. Coordinates are
// client-area DIP. Files is nil when the platform cannot provide the paths.
// DropEvent delivers the files when the user drops them.
type DragEvent struct {
	Window *Window
	Phase  DragPhase
	X, Y   float64
	Files  fs.FS
}

func (CompositionEvent) isEvent() {}
func (DragEvent) isEvent()        {}

// MouseMoveEvent reports the cursor position in device-independent pixels relative to the client
// area.
type MouseMoveEvent struct {
	Window *Window
	X      float64
	Y      float64
}

// MouseButtonEvent reports a button press or release at the cursor position, in device-independent
// pixels.
type MouseButtonEvent struct {
	Window  *Window
	Button  MouseButton
	Pressed bool
	X       float64
	Y       float64
}

// ScrollEvent reports a wheel or trackpad scroll.
type ScrollEvent struct {
	Window *Window
	X      float64
	Y      float64
}

// TouchPhase is what happened to a touch.
type TouchPhase int

const (
	TouchPhaseBegan TouchPhase = TouchPhase(ui.TouchPhaseBegan)
	TouchPhaseMoved TouchPhase = TouchPhase(ui.TouchPhaseMoved)
	TouchPhaseEnded TouchPhase = TouchPhase(ui.TouchPhaseEnded)
)

// TouchEvent reports a touch in device-independent pixels.
type TouchEvent struct {
	Window *Window
	ID     TouchID
	Phase  TouchPhase
	X      float64
	Y      float64
}

// DropEvent reports files dropped on the window.
type DropEvent struct {
	Window *Window
	Files  fs.FS
}

func (StartEvent) isEvent()       {}
func (FrameEvent) isEvent()       {}
func (ResizeEvent) isEvent()      {}
func (FocusEvent) isEvent()       {}
func (CloseEvent) isEvent()       {}
func (KeyEvent) isEvent()         {}
func (TextEvent) isEvent()        {}
func (MouseMoveEvent) isEvent()   {}
func (MouseButtonEvent) isEvent() {}
func (ScrollEvent) isEvent()      {}
func (TouchEvent) isEvent()       {}
func (DropEvent) isEvent()        {}

// WindowOptions describes a window to create with NewWindow. The zero value is a decorated,
// visible, fixed-size 640x480 window centered on the monitor.
type WindowOptions struct {
	Title string

	// Width and Height are the client area size in device-independent pixels. 0 uses 640x480.
	Width  int
	Height int

	// Position is the position on the monitor in device-independent pixels. nil centers the
	// window.
	Position *image.Point

	Resizable   bool
	Undecorated bool
	Floating    bool
	Hidden      bool
	Maximized   bool
	Unfocused   bool

	// Transparent makes the window's framebuffer composite with what is behind the window.
	// DirectX cannot present such a surface, so NewWindow fails with it on a Windows machine
	// that chose the DirectX driver.
	Transparent bool

	// The size limits in device-independent pixels. 0 means no limit.
	MinWidth  int
	MinHeight int
	MaxWidth  int
	MaxHeight int
}

// Window is a window created by NewWindow.
type Window struct {
	ui     ui.AppWindow
	screen *Image
}

// NewWindow creates a window. It can be called only while Run runs, from the handler.
func NewWindow(o *WindowOptions) (*Window, error) {
	if o == nil {
		o = &WindowOptions{}
	}
	uo := &ui.WindowOptions{
		Title:       o.Title,
		Width:       o.Width,
		Height:      o.Height,
		Decorated:   !o.Undecorated,
		Resizable:   o.Resizable,
		Floating:    o.Floating,
		Visible:     !o.Hidden,
		Maximized:   o.Maximized,
		Transparent: o.Transparent,
		Unfocused:   o.Unfocused,
		MinWidth:    o.MinWidth,
		MinHeight:   o.MinHeight,
		MaxWidth:    o.MaxWidth,
		MaxHeight:   o.MaxHeight,
	}
	if o.Position != nil {
		uo.X, uo.Y, uo.PositionSet = o.Position.X, o.Position.Y, true
	}
	w := &Window{}
	uw, err := ui.Get().NewWindow(uo, w)
	if err != nil {
		return nil, err
	}
	w.ui = uw
	return w, nil
}

// Close asks the window to close. The window closes after the current event is handled.
func (w *Window) Close() {
	w.ui.Close()
}

// RequestFrame asks for one FrameEvent. Frames are not delivered otherwise, except after a
// resize and when the window is first shown.
func (w *Window) RequestFrame() {
	w.ui.RequestFrame()
}

// SetTitle sets the title.
func (w *Window) SetTitle(title string) {
	w.ui.SetTitle(title)
}

// Size returns the client area size in device-independent pixels.
func (w *Window) Size() (int, int) {
	return w.ui.Size()
}

// SetSize sets the client area size in device-independent pixels.
func (w *Window) SetSize(width, height int) {
	w.ui.SetSize(width, height)
}

// Position returns the position on the monitor in device-independent pixels.
func (w *Window) Position() (int, int) {
	return w.ui.Position()
}

// SetPosition sets the position on the monitor in device-independent pixels.
func (w *Window) SetPosition(x, y int) {
	w.ui.SetPosition(x, y)
}

// SetSizeLimits sets the size limits in device-independent pixels. -1 means no limit.
func (w *Window) SetSizeLimits(minw, minh, maxw, maxh int) {
	w.ui.SetSizeLimits(minw, minh, maxw, maxh)
}

// SetResizable sets whether the user can resize the window.
func (w *Window) SetResizable(resizable bool) {
	mode := ui.WindowResizingModeDisabled
	if resizable {
		mode = ui.WindowResizingModeEnabled
	}
	w.ui.SetResizingMode(mode)
}

// Show shows the window.
func (w *Window) Show() {
	w.ui.SetVisible(true)
}

// Hide hides the window.
func (w *Window) Hide() {
	w.ui.SetVisible(false)
}

// IsVisible reports whether the window is shown.
func (w *Window) IsVisible() bool {
	return w.ui.IsVisible()
}

// SetDecorated sets whether the window has a frame.
func (w *Window) SetDecorated(decorated bool) {
	w.ui.SetDecorated(decorated)
}

// SetFloating sets whether the window stays above the others.
func (w *Window) SetFloating(floating bool) {
	w.ui.SetFloating(floating)
}

// Maximize maximizes the window.
func (w *Window) Maximize() {
	w.ui.Maximize()
}

// IsMaximized reports whether the window is maximized.
func (w *Window) IsMaximized() bool {
	return w.ui.IsMaximized()
}

// Minimize minimizes the window.
func (w *Window) Minimize() {
	w.ui.Minimize()
}

// IsMinimized reports whether the window is minimized.
func (w *Window) IsMinimized() bool {
	return w.ui.IsMinimized()
}

// Restore restores a maximized or minimized window.
func (w *Window) Restore() {
	w.ui.Restore()
}

// SetFullscreen sets whether the window is fullscreen.
func (w *Window) SetFullscreen(fullscreen bool) {
	w.ui.SetFullscreen(fullscreen)
}

// IsFullscreen reports whether the window is fullscreen.
func (w *Window) IsFullscreen() bool {
	return w.ui.IsFullscreen()
}

// IsFocused reports whether the window has the keyboard focus.
func (w *Window) IsFocused() bool {
	return w.ui.IsFocused()
}

// Focus gives the window the keyboard focus.
func (w *Window) Focus() {
	w.ui.Focus()
}

// RequestAttention asks the user's attention, like bouncing the dock icon.
func (w *Window) RequestAttention() {
	w.ui.RequestAttention()
}

// SetIcon sets the window icon on the platforms that show one.
func (w *Window) SetIcon(iconImages []image.Image) {
	w.ui.SetIcon(iconImages)
}

// SetMousePassthrough sets whether mouse events go through the window.
func (w *Window) SetMousePassthrough(enabled bool) {
	w.ui.SetMousePassthrough(enabled)
}

// DeviceScaleFactor returns the scale from device-independent pixels to physical pixels on the
// window's monitor.
func (w *Window) DeviceScaleFactor() float64 {
	return w.ui.DeviceScaleFactor()
}

// Monitor returns the monitor the window is on.
func (w *Window) Monitor() *MonitorType {
	return (*MonitorType)(w.ui.Monitor())
}

// CursorShape returns the cursor shape over the window.
func (w *Window) CursorShape() CursorShapeType {
	return CursorShapeType(w.ui.CursorShape())
}

// SetCursorShape sets the cursor shape over the window.
func (w *Window) SetCursorShape(shape CursorShapeType) {
	w.ui.SetCursorShape(ui.CursorShape(shape))
}

// CursorMode returns the cursor mode.
func (w *Window) CursorMode() CursorModeType {
	return CursorModeType(w.ui.CursorMode())
}

// SetCursorMode sets the cursor mode.
func (w *Window) SetCursorMode(mode CursorModeType) {
	w.ui.SetCursorMode(ui.CursorMode(mode))
}

// NativeHandle returns the platform's window object: an NSWindow on macOS, an HWND on Windows,
// an X11 window on Linux.
func (w *Window) NativeHandle() uintptr {
	return w.ui.NativeHandle()
}

// SetTextInputEnabled enables native IME input on macOS and Windows. Disabling
// it cancels marked text without removing the window's keyboard focus.
// It has no effect on other platforms. Call it when an editable field gains or
// loses focus, not on each composition commit.
func (w *Window) SetTextInputEnabled(enabled bool) { w.ui.SetTextInputEnabled(enabled) }

// SetTextInputRect positions the IME candidate panel at the caret rectangle in
// client-area DIP. It has no effect on platforms without native IME events.
func (w *Window) SetTextInputRect(x, y, width, height float64) {
	w.ui.SetTextInputRect(x, y, width, height)
}

// AccessibilityView returns a mouse-transparent NSView owned by this window on
// macOS, or zero elsewhere. The view is valid until the window closes.
func (w *Window) AccessibilityView() uintptr { return w.ui.AccessibilityView() }

// SetAccessibilityHandlers supplies the macOS accessibility container's children
// (an autoreleased NSArray) and hit test (an NSAccessibilityElement). Hit test
// coordinates are native screen points. Callbacks run on the main thread and
// must not call window methods or wait for the event handler. Nil detaches them.
// Other platforms ignore these callbacks.
func (w *Window) SetAccessibilityHandlers(children func() uintptr, hitTest func(x, y float64) uintptr) {
	w.ui.SetAccessibilityHandlers(children, hitTest)
}

// SetGetObjectHandler handles Windows WM_GETOBJECT. The callback runs on the
// main thread and must not call window methods or wait for the event handler.
// Return false to use the default window procedure; nil removes the handler.
// Other platforms ignore it.
func (w *Window) SetGetObjectHandler(f func(wparam, lparam uintptr) (uintptr, bool)) {
	w.ui.SetGetObjectHandler(f)
}

// SetTextInputContext supplies the text before and after the editable selection
// for native IME replacement ranges. Update it when the caret or text changes.
func (w *Window) SetTextInputContext(before, after string) { w.ui.SetTextInputContext(before, after) }
