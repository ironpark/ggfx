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

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// App is what RunApp drives: it receives every event and creates the screen images.
type App interface {
	// NewScreenImage creates the image presented on the window's surface. The app owns the image
	// and gets it back in FrameEvent.Screen.
	NewScreenImage(window AppWindow, width, height int, surface graphicsdriver.Surface) *Image

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

// Event is what an App receives. Every event but StartEvent belongs to a window.
type Event interface {
	isEvent()
}

// StartEvent is the first event. The app creates its windows here.
type StartEvent struct{}

// FrameEvent asks the app to draw Screen, which is in physical pixels. Scale converts
// device-independent pixels to physical pixels.
type FrameEvent struct {
	Window AppWindow
	Screen *Image
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
	Window  AppWindow
	Key     Key
	Pressed bool
	Repeat  bool
}

// TextEvent carries the characters the user typed.
type TextEvent struct {
	Window AppWindow
	Text   string
}

// MouseMoveEvent reports the cursor position in device-independent pixels relative to the client
// area.
type MouseMoveEvent struct {
	Window AppWindow
	X      float64
	Y      float64
}

// MouseButtonEvent reports a button press or release at the cursor position.
type MouseButtonEvent struct {
	Window  AppWindow
	Button  MouseButton
	Pressed bool
	X       float64
	Y       float64
}

// ScrollEvent reports a wheel or trackpad scroll.
type ScrollEvent struct {
	Window AppWindow
	X      float64
	Y      float64
}

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

	surface graphicsdriver.Surface
	screen  *Image

	screenWidth  int
	screenHeight int

	frameRequested atomic.Bool
}

var _ frameDriver = (*eventContext)(nil)

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

func (c *eventContext) setSurface(surface graphicsdriver.Surface) {
	c.surface = surface
}

func (c *eventContext) dispose() {
	if c.screen != nil {
		c.screen.Deallocate()
		c.screen = nil
	}
}

func (c *eventContext) renderFrame(graphicsDriver graphicsdriver.Graphics, outsideWidth, outsideHeight float64, screenWidth, screenHeight int, deviceScaleFactor float64, ui *UserInterface, present, force bool) (needsSwapBuffers bool, err error) {
	if screenWidth == 0 || screenHeight == 0 {
		return false, nil
	}
	if !force && !c.frameRequested.Load() {
		return false, nil
	}
	if !present {
		// Keep the request until the window can show the frame.
		return false, nil
	}
	c.frameRequested.Store(false)

	if err := atlas.BeginFrame(graphicsDriver); err != nil {
		return false, err
	}
	defer func() {
		if atlasErr := atlas.EndFrame(graphicsDriver); atlasErr != nil {
			needsSwapBuffers = false
			err = atlasErr
		}
	}()

	if err := ui.processFuncsInFrame(); err != nil {
		return false, err
	}

	if c.screen != nil && (c.screenWidth != screenWidth || c.screenHeight != screenHeight) {
		c.screen.Deallocate()
		c.screen = nil
	}
	if c.screen == nil {
		c.screen = c.app.NewScreenImage(c.window, screenWidth, screenHeight, c.surface)
		c.screenWidth = screenWidth
		c.screenHeight = screenHeight
	}

	if graphicsDriver.NeedsClearingScreen() {
		c.screen.clear()
	}

	if err := c.app.HandleEvent(FrameEvent{
		Window: c.window,
		Screen: c.screen,
		Scale:  deviceScaleFactor,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (c *eventContext) forceUpdateFrame(graphicsDriver graphicsdriver.Graphics, outsideWidth, outsideHeight float64, screenWidth, screenHeight int, deviceScaleFactor float64, ui *UserInterface) error {
	// The size change is queued as a ResizeEvent; hand it over before the frame.
	if err := ui.dispatchEvents(); err != nil {
		return err
	}
	needsSwapBuffers, err := c.renderFrame(graphicsDriver, outsideWidth, outsideHeight, screenWidth, screenHeight, deviceScaleFactor, ui, true, true)
	if err != nil {
		return err
	}
	if err := ui.pacer.flushCommandsAndWait(needsSwapBuffers, graphicsDriver, ui.FPSMode() == FPSModeVsyncOn, ui.RefreshRate()); err != nil {
		return err
	}
	return graphicscommand.FinishForcedFrame(graphicsDriver)
}

// Input positions are already in device-independent pixels of the client area; there is no
// letterboxing on an app window.

func (c *eventContext) clientPositionToLogicalPosition(x, y float64, deviceScaleFactor float64) (float64, float64) {
	return x, y
}

func (c *eventContext) logicalPositionToClientPosition(x, y float64, deviceScaleFactor float64) (float64, float64) {
	return x, y
}
