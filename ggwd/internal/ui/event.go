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

package ui

import (
	"io/fs"
	"sync/atomic"

	"github.com/ironpark/ggfx"
)

// App is what RunApp drives: it receives every event.
type App interface {
	// HandleEvent handles one event. Returning RegularTermination ends the loop without an error.
	HandleEvent(ev Event) error
}

// AppWindow is a window created by NewWindow.
type AppWindow interface {
	Window

	// Close asks the window to close. It closes after the current event is handled.
	Close()

	// RequestFrame asks for one FrameEvent.
	RequestFrame()

	IsFocused() bool
	Focus()
	IsFullscreen() bool
	SetFullscreen(fullscreen bool)
	CursorShape() CursorShape
	SetCursorShape(shape CursorShape)
	CursorMode() CursorMode
	SetCursorMode(mode CursorMode)
	DeviceScaleFactor() float64
	Monitor() *Monitor

	// NativeHandle is the platform's window object: an NSWindow, an HWND or an X11 window.
	NativeHandle() uintptr
	SetTextInputEnabled(bool)
	SetTextInputContext(before, after string)
	SetTextInputRect(x, y, width, height float64)
	AccessibilityView() uintptr
	SetAccessibilityHandlers(children func() uintptr, hitTest func(x, y float64) uintptr)
	SetGetObjectHandler(func(wparam, lparam uintptr) (uintptr, bool))

	// Handle is a value the app attaches to the window, and gets back from the window in events.
	Handle() any
	SetHandle(handle any)
}

// WindowOptions describes a window to create.
type WindowOptions struct {
	Title string

	// Width and Height are the client area size in device-independent pixels. 0 uses 640x480.
	Width  int
	Height int

	// X and Y are the position on the monitor in device-independent pixels, used when
	// PositionSet is true. Otherwise the window is centered.
	X           int
	Y           int
	PositionSet bool

	Decorated   bool
	Resizable   bool
	Floating    bool
	Visible     bool
	Maximized   bool
	Transparent bool
	Unfocused   bool

	MinWidth  int
	MinHeight int
	MaxWidth  int
	MaxHeight int
}

// Event is what an App receives. Every event but StartEvent and WakeEvent belongs to a window.
type Event interface {
	isEvent()
}

// StartEvent is the first event. The app creates its windows here.
type StartEvent struct{}

// WakeEvent is delivered once for every call to Wake. It is for no window.
type WakeEvent struct{}

// FrameEvent asks the app to draw Screen, which is in physical pixels. Scale converts
// device-independent pixels to physical pixels.
type FrameEvent struct {
	Window AppWindow
	Screen *ggfx.Image
	Scale  float64
}

// ResizeEvent reports that the client area size or the scale changed. Width and Height are in
// device-independent pixels.
type ResizeEvent struct {
	Window AppWindow
	Width  float64
	Height float64
	Scale  float64
}

// FocusEvent reports that the window gained or lost the keyboard focus.
type FocusEvent struct {
	Window  AppWindow
	Focused bool
}

// WindowState is whether a window is minimized, maximized or fullscreen.
type WindowState int

const (
	WindowStateNormal WindowState = iota
	WindowStateMinimized
	WindowStateMaximized
	WindowStateFullscreen
)

// WindowStateEvent reports that the window's state changed.
type WindowStateEvent struct {
	Window AppWindow
	State  WindowState
}

func (WindowStateEvent) isEvent() {}

// CloseEvent reports that the user asked to close the window. The window closes after the event
// is handled unless KeepOpen is called.
type CloseEvent struct {
	Window   AppWindow
	keepOpen *bool
}

// KeepOpen keeps the window open.
func (c CloseEvent) KeepOpen() {
	*c.keepOpen = true
}

// KeyEvent reports a key press or release. Repeat is set for the presses the OS generates while
// the key is held.
type KeyEvent struct {
	Window    AppWindow
	Key       Key
	Pressed   bool
	Repeat    bool
	Modifiers KeyModifiers
}

type KeyModifiers struct{ Shift, Control, Alt, Meta bool }

// TextEvent carries the characters the user typed.
type TextEvent struct {
	Window                           AppWindow
	Text                             string
	ReplacementStart, ReplacementEnd int
	HasReplacement                   bool
	Passthrough                      bool
}

type CompositionEvent struct {
	Window     AppWindow
	Text       string
	Start, End int
	Done       bool
}

type DragEvent struct {
	Window AppWindow
	Phase  int
	X, Y   float64
	Files  fs.FS
}

func (CompositionEvent) isEvent() {}
func (DragEvent) isEvent()        {}

// MouseMoveEvent reports the cursor position in device-independent pixels relative to the client
// area.
type MouseMoveEvent struct {
	Window AppWindow
	X      float64
	Y      float64
}

// MouseButtonEvent reports a button press or release at the cursor position.
type MouseButtonEvent struct {
	Window    AppWindow
	Button    MouseButton
	Pressed   bool
	X         float64
	Y         float64
	Modifiers KeyModifiers
}

// ScrollEvent reports a wheel or trackpad scroll, in lines.
type ScrollEvent struct {
	Window    AppWindow
	X         float64
	Y         float64
	Modifiers KeyModifiers
	Phase     ScrollPhase
}

// ScrollPhase is where a scroll is in the trackpad gesture that makes it.
type ScrollPhase int

const (
	ScrollPhaseNone ScrollPhase = iota
	ScrollPhaseBegan
	ScrollPhaseChanged
	ScrollPhaseEnded
	ScrollPhaseMomentum
	ScrollPhaseMomentumEnded
)

// TouchPhase is what happened to a touch.
type TouchPhase int

const (
	TouchPhaseBegan TouchPhase = iota
	TouchPhaseMoved
	TouchPhaseEnded
)

// TouchEvent reports a touch in device-independent pixels.
type TouchEvent struct {
	Window AppWindow
	ID     TouchID
	Phase  TouchPhase
	X      float64
	Y      float64
}

// DropEvent reports files dropped on the window.
type DropEvent struct {
	Window AppWindow
	Files  fs.FS
}

func (StartEvent) isEvent()       {}
func (WakeEvent) isEvent()        {}
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

// Wake queues a WakeEvent and wakes the loop, which dispatches events on every iteration
// whether or not a window draws. It can be called from any thread.
func (u *UserInterface) Wake() {
	u.pushEvent(WakeEvent{})
	u.wakeLoop()
}

// pushEvent queues an event for the app. It can be called from any thread.
func (u *UserInterface) pushEvent(ev Event) {
	u.eventsMu.Lock()
	u.events = append(u.events, ev)
	u.eventsMu.Unlock()
}

// dispatchEvents hands the queued events to the app, in order. A window whose CloseEvent was not
// kept open is closed.
func (u *UserInterface) dispatchEvents() error {
	for {
		u.eventsMu.Lock()
		events := u.events
		u.events = nil
		u.eventsMu.Unlock()
		if len(events) == 0 {
			return nil
		}
		for _, ev := range events {
			if c, ok := ev.(CloseEvent); ok {
				var keep bool
				c.keepOpen = &keep
				if err := u.app.HandleEvent(c); err != nil {
					return err
				}
				if !keep {
					c.Window.Close()
				}
				continue
			}
			if err := u.app.HandleEvent(ev); err != nil {
				return err
			}
		}
	}
}

// eventContext is the frame driver of an app window: it hands the screen image to the app in a
// FrameEvent when a frame was requested.
type eventContext struct {
	app    App
	window AppWindow

	surface *ggfx.Surface

	frameRequested atomic.Bool
}

func newEventContext(app App, window AppWindow) *eventContext {
	c := &eventContext{app: app, window: window}
	// The first frame shows the window.
	c.frameRequested.Store(true)
	return c
}

func (c *eventContext) requestFrame() {
	c.frameRequested.Store(true)
}

func (c *eventContext) wantsFrame() bool {
	return c.frameRequested.Load()
}

func (c *eventContext) setSurface(surface *ggfx.Surface) {
	c.surface = surface
}

// renderFrame runs one frame if one was requested, or regardless when force is set. It returns
// whether the frame ran; the caller decides whether that frame is presented.
func (c *eventContext) renderFrame(screenWidth, screenHeight int, deviceScaleFactor float64, force bool) (ran bool, err error) {
	if screenWidth == 0 || screenHeight == 0 {
		return false, nil
	}
	if !force && !c.frameRequested.Load() {
		return false, nil
	}
	c.frameRequested.Store(false)

	if err := ggfx.BeginFrame(); err != nil {
		return false, err
	}
	defer func() {
		if renderErr := ggfx.EndFrame(); renderErr != nil {
			ran = false
			err = renderErr
		}
	}()

	if err := c.app.HandleEvent(FrameEvent{
		Window: c.window,
		Screen: c.surface.Screen(screenWidth, screenHeight),
		Scale:  deviceScaleFactor,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (c *eventContext) forceUpdateFrame(screenWidth, screenHeight int, deviceScaleFactor float64, ui *UserInterface) error {
	// The size change is queued as a ResizeEvent; hand it over before the frame.
	if err := ui.dispatchEvents(); err != nil {
		return err
	}
	needsSwapBuffers, err := c.renderFrame(screenWidth, screenHeight, deviceScaleFactor, true)
	if err != nil {
		return err
	}
	if err := ui.pacer.flushCommandsAndWait(needsSwapBuffers, ui.RefreshRate()); err != nil {
		return err
	}
	return ggfx.FinishForcedFrame()
}
