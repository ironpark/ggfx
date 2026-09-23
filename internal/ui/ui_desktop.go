// Copyright 2026 The Ebitengine Authors
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
	"sync"
	"sync/atomic"
	"time"

	"github.com/ironpark/ggfx/internal/glfw"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// uiBackend is the platform UI implementation for the desktop build.
//
// A backend is created at Run and consumes the settings buffered in
// userInterfaceImpl during its initialization. All the methods except run
// are called only while the game runs.
type uiBackend interface {
	updateIconIfNeeded() error
	IsFocused() bool
	IsFullscreen() bool
	SetFullscreen(fullscreen bool)
	ScheduleFrame()
	CursorMode() CursorMode
	SetCursorMode(mode CursorMode)
	applyCursorShape()
	Window() backendWindow
	Monitor() *Monitor
	RunOnMainThread(f func())
	KeyName(key Key) string
}

// backendWindow is the part of Window that a backend implements.
// The methods are called only while the game runs. The remaining part of
// Window is answered by desktopWindow from the settings in userInterfaceImpl.
//
// An applyX method takes no value and applies the setting currently stored in
// userInterfaceImpl or desktopWindow, rather than a value passed by the caller.
// The store and this apply run as two steps, so a value-carrying apply could let
// two concurrent setters leave the store and the window disagreeing; applying the
// stored value instead keeps them in agreement (#3481).
type backendWindow interface {
	IsDecorated() bool
	SetDecorated(decorated bool)
	IsVisible() bool
	SetVisible(visible bool)
	applyResizingMode()
	SetMonitor(monitor *Monitor)
	Position() (int, int)
	SetPosition(x, y int)
	Size() (int, int)
	SetSize(width, height int)
	SetSizeLimits(minw, minh, maxw, maxh int)
	IsFloating() bool
	SetFloating(floating bool)
	Maximize()
	IsMaximized() bool
	Minimize()
	IsMinimized() bool
	applyTitle()
	applyColorMode()
	Restore()
	applyClosingHandled()
	SetMousePassthrough(enabled bool)
	IsMousePassthrough() bool
	RequestAttention()
}

var _ uiBackend = (*glfwBackend)(nil)

type userInterfaceImpl struct {
	// backend is the backend serving the running game.
	//
	// backend is non-nil only while the game runs: a backend publishes itself
	// via setRunningBackend when it gets ready to serve calls, and
	// unpublishes itself when the game stops. The settings issued while
	// backend is nil are buffered, and a backend consumes them at its
	// initialization.
	backend atomic.Pointer[uiBackend]

	graphicsDriver graphicsdriver.Graphics

	// glContextWindow is the hidden window whose context the OpenGL driver draws in, and nil with
	// the other drivers. It lives until GLFW terminates.
	glContextWindow *glfw.Window

	// renderOnMainThread reports whether frames are rendered on the main thread, as in the single
	// thread mode.
	renderOnMainThread bool

	// windows are the open windows, the primary window first. windows must be accessed from
	// the main thread.
	windows []*glfwBackend

	// primary is the window the process-level settings and functions address. It is set once
	// at the initialization and never cleared.
	primary atomic.Pointer[glfwBackend]

	// pollingEvents reports whether the main thread is polling events for the loop.
	// pollingEvents must be accessed from the main thread.
	pollingEvents bool

	// forcingFrame reports whether a frame is being rendered from an event callback.
	// forcingFrame must be accessed from the main thread.
	forcingFrame bool

	unfocusedNextWake time.Time

	// The atomic fields below hold the settings that can be set before the
	// backend exists. The backend consumes the init* fields at its
	// initialization, and reads the other fields whenever it needs them.
	// The window settings are held by desktopWindow.
	runnableOnUnfocused atomic.Bool
	cursorShape         atomic.Int32

	initMonitor    atomic.Pointer[Monitor]
	initFullscreen atomic.Bool
	initCursorMode atomic.Int32

	desktopWindow desktopWindow

	glfwInitOnce sync.Once
}

func (u *UserInterface) init() error {
	u.runnableOnUnfocused.Store(true)
	u.initCursorMode.Store(int32(CursorModeVisible))

	u.desktopWindow.ui = u
	u.desktopWindow.init()

	return nil
}

// setRunningBackend publishes the backend that serves the running window, or
// unpublishes the current backend when b is nil. The running state is updated
// accordingly. A backend calls setRunningBackend with itself when it gets
// ready to serve calls, and with nil when the window closes.
func (u *UserInterface) setRunningBackend(b uiBackend) {
	// The backend and the running state are updated non-atomically. The update
	// orders guarantee that a backend is published whenever the running state
	// is true.
	if b == nil {
		u.setRunning(false)
		u.backend.Store(nil)
		return
	}
	u.backend.Store(&b)
	u.setRunning(true)
}

// runningBackend returns the backend serving the running window, or nil when
// nothing is running.
func (u *UserInterface) runningBackend() uiBackend {
	b := u.backend.Load()
	if b == nil {
		return nil
	}
	return *b
}

func (u *UserInterface) setInitMonitor(m *Monitor) {
	u.initMonitor.Store(m)
}

func (u *UserInterface) getInitMonitor() *Monitor {
	return u.initMonitor.Load()
}

func (u *UserInterface) isInitFullscreen() bool {
	return u.initFullscreen.Load()
}

func (u *UserInterface) setInitFullscreen(initFullscreen bool) {
	u.initFullscreen.Store(initFullscreen)
}

func (u *UserInterface) getInitCursorMode() CursorMode {
	return CursorMode(u.initCursorMode.Load())
}

func (u *UserInterface) setInitCursorMode(mode CursorMode) {
	u.initCursorMode.Store(int32(mode))
}

func (u *UserInterface) getCursorShape() CursorShape {
	return CursorShape(u.cursorShape.Load())
}

func (u *UserInterface) isRunnableOnUnfocused() bool {
	return u.runnableOnUnfocused.Load()
}

func (u *UserInterface) setRunnableOnUnfocused(runnableOnUnfocused bool) {
	u.runnableOnUnfocused.Store(runnableOnUnfocused)
}

func (u *UserInterface) updateIconIfNeeded() error {
	return u.runningBackend().updateIconIfNeeded()
}

func (u *UserInterface) IsFocused() bool {
	b := u.runningBackend()
	if b == nil {
		return false
	}
	return b.IsFocused()
}

func (u *UserInterface) IsFullscreen() bool {
	if u.isTerminated() {
		return false
	}
	b := u.runningBackend()
	if b == nil {
		return u.isInitFullscreen()
	}
	return b.IsFullscreen()
}

func (u *UserInterface) SetFullscreen(fullscreen bool) {
	if u.isTerminated() {
		return
	}
	b := u.runningBackend()
	if b == nil {
		u.setInitFullscreen(fullscreen)
		return
	}
	b.SetFullscreen(fullscreen)
}

func (u *UserInterface) CursorMode() CursorMode {
	if u.isTerminated() {
		return 0
	}
	b := u.runningBackend()
	if b == nil {
		return u.getInitCursorMode()
	}
	return b.CursorMode()
}

func (u *UserInterface) SetCursorMode(mode CursorMode) {
	if u.isTerminated() {
		return
	}
	b := u.runningBackend()
	if b == nil {
		u.setInitCursorMode(mode)
		return
	}
	b.SetCursorMode(mode)
}

func (u *UserInterface) CursorShape() CursorShape {
	return u.getCursorShape()
}

func (u *UserInterface) SetCursorShape(shape CursorShape) {
	if u.isTerminated() {
		return
	}
	if CursorShape(u.cursorShape.Swap(int32(shape))) == shape {
		return
	}
	b := u.runningBackend()
	if b == nil {
		return
	}
	b.applyCursorShape()
}

func (u *UserInterface) Window() Window {
	return &u.desktopWindow
}

// Monitor returns the window's current monitor. Returns nil if there is no current monitor yet.
func (u *UserInterface) Monitor() *Monitor {
	b := u.runningBackend()
	if b == nil {
		// Ensure GLFW is initialized so that the init monitor is available.
		if err := u.ensureGLFWInit(); err != nil {
			return nil
		}
		return u.getInitMonitor()
	}
	return b.Monitor()
}

// monitorAppender is implemented by a backend that provides its own monitor list rather than the
// glfw one.
type monitorAppender interface {
	appendMonitors(monitors []*Monitor) []*Monitor
}

// AppendMonitors appends the current monitors to the passed in mons slice and returns it.
func (u *UserInterface) AppendMonitors(monitors []*Monitor) []*Monitor {
	if a, ok := u.runningBackend().(monitorAppender); ok {
		return a.appendMonitors(monitors)
	}
	// Ensure GLFW is initialized so that the monitor list is available.
	if err := u.ensureGLFWInit(); err != nil {
		return monitors
	}
	return theMonitors.append(monitors)
}

func (u *UserInterface) RunOnMainThread(f func()) {
	b := u.runningBackend()
	if b == nil {
		return
	}
	b.RunOnMainThread(f)
}

func (u *UserInterface) KeyName(key Key) string {
	b := u.runningBackend()
	if b == nil {
		return ""
	}
	return b.KeyName(key)
}
