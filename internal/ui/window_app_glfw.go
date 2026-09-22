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

//go:build !android && !ios && !js && !nintendosdk && !playstation5

package ui

import (
	"errors"
	"image"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/glfw"
	"github.com/ironpark/ggfx/internal/windowsystem"
)

// appWindow is an AppWindow on GLFW: the settings of the window plus what the app can do with it.
type appWindow struct {
	*desktopWindow

	backend *glfwBackend
	context *eventContext
	handle  any
}

var _ AppWindow = (*appWindow)(nil)

// RunApp runs the app until it returns RegularTermination, an error, or closes its last window.
func (u *UserInterface) RunApp(app App, options *RunOptions) error {
	if !windowsystem.Available() {
		return errors.New("ui: no window system is available")
	}
	u.app = app
	return u.runLoop(options, func() error {
		return u.initGraphicsOnMainThread(options)
	}, func() error {
		if err := app.HandleEvent(StartEvent{}); err != nil {
			return err
		}
		var n int
		u.mainThread.Call(func() {
			n = len(u.windows)
		})
		if n == 0 {
			return RegularTermination
		}
		return nil
	})
}

// NewWindow creates a window for the app with the given handle attached. It can be called only
// while RunApp runs, from the goroutine handling the events.
func (u *UserInterface) NewWindow(o *WindowOptions, handle any) (AppWindow, error) {
	if u.app == nil {
		return nil, errors.New("ui: NewWindow can be called only while RunApp runs")
	}
	if u.isTerminated() {
		return nil, errors.New("ui: NewWindow cannot be called after the app stopped")
	}

	settings := &desktopWindow{ui: u}
	settings.init()
	settings.title.Store(o.Title)
	if o.Width > 0 && o.Height > 0 {
		settings.setInitWindowSizeInDIP(o.Width, o.Height)
	}
	if o.PositionSet {
		settings.setInitWindowPositionInDIP(o.X, o.Y)
	}
	settings.setInitWindowDecorated(o.Decorated)
	settings.setInitWindowFloating(o.Floating)
	settings.setInitWindowVisible(o.Visible)
	settings.setInitWindowMaximized(o.Maximized)
	if o.Resizable {
		settings.windowResizingMode.Store(int32(WindowResizingModeEnabled))
	}
	minw, minh, maxw, maxh := glfw.DontCare, glfw.DontCare, glfw.DontCare, glfw.DontCare
	if o.MinWidth > 0 {
		minw = o.MinWidth
	}
	if o.MinHeight > 0 {
		minh = o.MinHeight
	}
	if o.MaxWidth > 0 {
		maxw = o.MaxWidth
	}
	if o.MaxHeight > 0 {
		maxh = o.MaxHeight
	}
	settings.setWindowSizeLimitsInDIP(minw, minh, maxw, maxh)

	b := &glfwBackend{
		UserInterface:    u,
		desktopWindow:    settings,
		cursorShapeStore: new(atomic.Int32),
	}
	b.windowToRestore.pos = image.Pt(invalidPos, invalidPos)
	b.windowToRestore.size = image.Pt(invalidSize, invalidSize)
	b.input.clearSavedCursorPos()
	b.backendWindow.ui = b
	settings.backend.Store(b)

	w := &appWindow{
		desktopWindow: settings,
		backend:       b,
		handle:        handle,
	}
	w.context = newEventContext(u.app, w)
	b.context = w.context

	var err error
	u.mainThread.Call(func() {
		if u.isTerminated() {
			err = errors.New("ui: NewWindow cannot be called after the app stopped")
			return
		}
		// The first window is the one the process-level functions address.
		if u.primary.Load() == nil {
			b.primary = true
			u.primary.Store(b)
		}
		if !o.PositionSet {
			if m := u.getInitMonitor(); m != nil {
				sw, sh := m.sizeInDIP()
				ww, wh := settings.getInitWindowSizeInDIP()
				x, y := InitialWindowPosition(int(sw), int(sh), ww, wh)
				settings.setInitWindowPositionInDIP(x, y)
			}
		}
		if err = b.createWindowOnMainThread(&RunOptions{
			ScreenTransparent: o.Transparent,
			InitUnfocused:     o.Unfocused,
		}); err != nil {
			return
		}
		u.windows = append(u.windows, b)

		// Tell the app the initial size, so that it has one path for sizes.
		s := 1.0
		if m, e := b.currentMonitor(); e == nil && m != nil {
			s = m.DeviceScaleFactor()
		}
		u.pushEvent(ResizeEvent{Window: w, Width: float64(b.windowWidthInDIP), Height: float64(b.windowHeightInDIP), Scale: s})
	})
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (w *appWindow) Close() {
	b := w.backend
	b.mainThread.Call(func() {
		if b.isTerminated() || b.closed {
			return
		}
		if err := b.window.SetShouldClose(true); err != nil {
			b.setError(err)
		}
	})
	b.ScheduleFrame()
}

func (w *appWindow) RequestFrame() {
	w.context.requestFrame()
	w.backend.ScheduleFrame()
}

func (w *appWindow) IsFocused() bool {
	return w.backend.IsFocused()
}

func (w *appWindow) Focus() {
	b := w.backend
	b.mainThread.Call(func() {
		if b.isTerminated() || b.closed {
			return
		}
		if err := b.window.Focus(); err != nil {
			b.setError(err)
		}
	})
}

func (w *appWindow) IsFullscreen() bool {
	return w.backend.IsFullscreen()
}

func (w *appWindow) SetFullscreen(fullscreen bool) {
	w.backend.SetFullscreen(fullscreen)
}

func (w *appWindow) CursorShape() CursorShape {
	return w.backend.getCursorShape()
}

func (w *appWindow) SetCursorShape(shape CursorShape) {
	if CursorShape(w.backend.cursorShapeStore.Swap(int32(shape))) == shape {
		return
	}
	w.backend.applyCursorShape()
}

func (w *appWindow) CursorMode() CursorMode {
	return w.backend.CursorMode()
}

func (w *appWindow) SetCursorMode(mode CursorMode) {
	w.backend.SetCursorMode(mode)
}

func (w *appWindow) DeviceScaleFactor() float64 {
	m := w.backend.Monitor()
	if m == nil {
		return 1
	}
	return m.DeviceScaleFactor()
}

func (w *appWindow) Monitor() *Monitor {
	return w.backend.Monitor()
}

func (w *appWindow) NativeHandle() uintptr {
	b := w.backend
	if b.isTerminated() {
		return 0
	}
	var h uintptr
	b.mainThread.Call(func() {
		if b.isTerminated() || b.closed {
			return
		}
		n, err := b.nativeWindow()
		if err != nil {
			b.setError(err)
			return
		}
		h = n
	})
	return h
}

func (w *appWindow) Handle() any {
	return w.handle
}

func (w *appWindow) SetHandle(handle any) {
	w.handle = handle
}

// appWindow returns the AppWindow of this backend, or nil for a game window.
func (u *glfwBackend) appWindow() AppWindow {
	if c, ok := u.context.(*eventContext); ok {
		return c.window
	}
	return nil
}

// cursorPositionInDIP converts a cursor position reported by GLFW to device-independent pixels.
// It must be called on the main thread.
func (u *glfwBackend) cursorPositionInDIP(x, y float64) (float64, float64) {
	m, err := u.currentMonitor()
	if err != nil || m == nil {
		return x, y
	}
	s := m.DeviceScaleFactor()
	return dipFromGLFWPixel(x, s), dipFromGLFWPixel(y, s)
}
