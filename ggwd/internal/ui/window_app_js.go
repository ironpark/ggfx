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

//go:build js

package ui

import (
	"errors"
	"syscall/js"
)

// jsWindow is the AppWindow of the canvas. A browser page has one.
type jsWindow struct {
	nullWindow

	ui      *UserInterface
	context *eventContext
	handle  any
}

var _ AppWindow = (*jsWindow)(nil)

// RunApp runs the app on the page's canvas.
func (u *UserInterface) RunApp(app App, options *RunOptions) error {
	u.startApp(app, options)
	return u.runLoop(options, func() error {
		if err := app.HandleEvent(StartEvent{}); err != nil {
			return err
		}
		if u.context == nil {
			return RegularTermination
		}
		return nil
	})
}

// NewWindow binds the app to the canvas. A page has one canvas, so a second call fails.
func (u *UserInterface) NewWindow(o *WindowOptions, handle any) (AppWindow, error) {
	if u.app == nil {
		return nil, errors.New("ui: NewWindow can be called only while RunApp runs")
	}
	if u.context != nil {
		return nil, errors.New("ui: a browser page has one window")
	}
	w := &jsWindow{ui: u, handle: handle}
	w.context = newEventContext(u.app, w)
	w.context.setSurface(u.surface)
	if o.Title != "" {
		document.Set("title", o.Title)
	}
	u.mu.Lock()
	u.context = w.context
	u.mu.Unlock()

	ow, oh := u.outsideSize()
	u.pushEvent(ResizeEvent{Window: w, Width: ow, Height: oh, Scale: theMonitor.DeviceScaleFactor()})
	u.scheduleRendering()
	return w, nil
}

// scheduleRendering makes the next animation frame run an iteration, so that queued events are
// handled and a requested frame is drawn.
func (u *UserInterface) scheduleRendering() {
	u.renderingScheduled = true
}

func (w *jsWindow) Close() {
	w.ui.closeRequested.Store(true)
	w.ui.scheduleRendering()
}

func (w *jsWindow) RequestFrame() {
	w.context.requestFrame()
	w.ui.scheduleRendering()
}

func (w *jsWindow) IsFocused() bool {
	return w.ui.isFocused()
}

func (w *jsWindow) Focus() {
	w.ui.FocusCanvas()
}

func (w *jsWindow) IsFullscreen() bool {
	return w.ui.IsFullscreen()
}

func (w *jsWindow) SetFullscreen(fullscreen bool) {
	w.ui.SetFullscreen(fullscreen)
}

func (w *jsWindow) CursorShape() CursorShape {
	return w.ui.CursorShape()
}

func (w *jsWindow) SetCursorShape(shape CursorShape) {
	w.ui.SetCursorShape(shape)
}

func (w *jsWindow) CursorMode() CursorMode {
	return w.ui.CursorMode()
}

func (w *jsWindow) SetCursorMode(mode CursorMode) {
	w.ui.SetCursorMode(mode)
}

func (w *jsWindow) DeviceScaleFactor() float64 {
	return theMonitor.DeviceScaleFactor()
}

func (w *jsWindow) Monitor() *Monitor {
	return theMonitor
}

func (w *jsWindow) NativeHandle() uintptr {
	return 0
}

func (w *jsWindow) Handle() any {
	return w.handle
}

func (w *jsWindow) SetHandle(handle any) {
	w.handle = handle
}

func (w *jsWindow) SetTitle(title string) {
	document.Set("title", title)
}

func (w *jsWindow) Size() (int, int) {
	ow, oh := w.ui.outsideSize()
	return int(ow), int(oh)
}

// appWindow returns the AppWindow of the canvas, or nil before its context exists.
func (u *UserInterface) appWindow() AppWindow {
	if u.context == nil {
		return nil
	}
	return u.context.window
}

// pushFocusEvent queues a FocusEvent for the canvas, if an app runs.
func (u *UserInterface) pushFocusEvent(focused bool) {
	if aw := u.appWindow(); aw != nil {
		u.pushEvent(FocusEvent{Window: aw, Focused: focused})
		u.scheduleRendering()
	}
}

// clientPositionInDIP converts a DOM client position to device-independent pixels of the canvas.
// The canvas fills the page, so they are the same.
func clientPositionInDIP(e js.Value) (float64, float64) {
	return e.Get("clientX").Float(), e.Get("clientY").Float()
}
