// Copyright 2022 The Ebiten Authors
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
	"errors"
	"sync"
	"sync/atomic"

	_ "github.com/ebitengine/hideconsole"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/ggwd/internal/colormode"
	"github.com/ironpark/ggfx/ggwd/internal/thread"
)

// RegularTermination represents a regular termination.
// Run can return this error, and if this error is received,
// the game loop should be terminated as soon as possible.
var RegularTermination = errors.New("regular termination")

type CursorMode int

const (
	CursorModeVisible CursorMode = iota
	CursorModeHidden
	CursorModeCaptured
)

type CursorShape int

const (
	CursorShapeDefault CursorShape = iota
	CursorShapeText
	CursorShapeCrosshair
	CursorShapePointer
	CursorShapeEWResize
	CursorShapeNSResize
	CursorShapeNESWResize
	CursorShapeNWSEResize
	CursorShapeMove
	CursorShapeNotAllowed
)

type WindowResizingMode int

const (
	WindowResizingModeDisabled WindowResizingMode = iota
	WindowResizingModeOnlyFullscreenEnabled
	WindowResizingModeEnabled
)

type UserInterface struct {
	err atomic.Pointer[error]

	running    atomic.Bool
	terminated atomic.Bool

	// preferredColorMode is the color mode the application prefers.
	//
	// preferredColorMode is a property of the application rather than of the window: it is kept
	// even where no window can reflect it.
	preferredColorMode atomic.Int32

	// refreshRate is the refresh rate, in Hz, of the display the game is presented on.
	// It is 0 when the rate is unknown.
	refreshRate atomic.Int32

	mainThread thread.Thread

	// pacer paces the loop when a present does not wait for the display.
	pacer framePacer

	// app is the App driven by RunApp.
	app App

	// runOptions are the process-wide options RunApp was given. Window creation reads the
	// parts of them that are not per window, like the X11 names.
	runOptions *RunOptions

	// events are the events queued for the app, in order.
	eventsMu sync.Mutex
	events   []Event

	userInterfaceImpl
}

func (u *UserInterface) PreferredColorMode() colormode.ColorMode {
	return colormode.ColorMode(u.preferredColorMode.Load())
}

func (u *UserInterface) SetPreferredColorMode(mode colormode.ColorMode) {
	if colormode.ColorMode(u.preferredColorMode.Swap(int32(mode))) == mode {
		return
	}
	u.Window().applyColorMode()
}

var (
	theUI *UserInterface
)

func init() {
	// newUserInterface() must be called in the main goroutine.
	u, err := newUserInterface()
	if err != nil {
		panic(err)
	}
	theUI = u
}

func Get() *UserInterface {
	return theUI
}

// newUserInterface must be called from the main thread.
func newUserInterface() (*UserInterface, error) {
	u := &UserInterface{}
	if err := u.init(); err != nil {
		return nil, err
	}

	return u, nil
}

type RunOptions struct {
	GraphicsLibrary ggfx.GraphicsLibrary
	// InitUnfocused is read by the browser at initialization. Desktop windows use
	// WindowOptions.Unfocused instead.
	InitUnfocused bool
	// ScreenTransparent is read by the browser at initialization. Desktop windows use
	// WindowOptions.Transparent instead.
	ScreenTransparent        bool
	SkipTaskbar              bool
	SingleThread             bool
	DisableHiDPI             bool
	ColorSpace               ggfx.ColorSpace
	ApplePressAndHoldEnabled bool
	X11ClassName             string
	X11InstanceName          string
}

// startApp records what RunApp was given, before either backend's loop starts.
func (u *UserInterface) startApp(app App, options *RunOptions) {
	u.app = app
	u.runOptions = options
}

// InitialWindowPosition returns the position to place a window of size (ww, wh) in a monitor of size (mw, mh).
func InitialWindowPosition(mw, mh, ww, wh int) (x, y int) {
	// The vertical position is visually centered rather than exactly centered: the space below the window
	// is about twice the space above it. This follows the "Positioning Windows" section in the old Apple
	// Human Interface Guidelines.
	// http://web.archive.org/web/20110531113415/http://developer.apple.com/library/mac/#documentation/UserExperience/Conceptual/AppleHIGuidelines/XHIGWindows/XHIGWindows.html
	return (mw - ww) / 2, (mh - wh) / 3
}

// error returns the error the loop stops with. The renderer's errors stop the loop at the frame
// they are reported at.
func (u *UserInterface) error() error {
	if err := u.err.Load(); err != nil {
		return *err
	}
	return nil
}

func (u *UserInterface) setError(err error) {
	if err == nil {
		return
	}
	for {
		oldErr := u.err.Load()
		var newErr error
		if oldErr != nil {
			newErr = errors.Join(*oldErr, err)
		} else {
			newErr = err
		}
		if u.err.CompareAndSwap(oldErr, &newErr) {
			break
		}
	}
}

func (u *UserInterface) setRefreshRate(refreshRate int) {
	u.refreshRate.Store(int32(refreshRate))
}

// RefreshRate returns the refresh rate, in Hz, of the display the game is presented on.
// It returns 0 when the rate is unknown.
func (u *UserInterface) RefreshRate() int {
	return int(u.refreshRate.Load())
}

// IsRunning reports whether the game is running, which is when the graphics driver exists.
func (u *UserInterface) IsRunning() bool {
	return u.isRunning()
}

func (u *UserInterface) isRunning() bool {
	// TODO: Replace the running state with the existence of a published backend
	// for all the platforms, like the desktop build (see setRunningBackend in
	// ui_desktop.go), and remove the running flag.
	return u.running.Load() && !u.isTerminated()
}

func (u *UserInterface) setRunning(running bool) {
	u.running.Store(running)
}

func (u *UserInterface) isTerminated() bool {
	return u.terminated.Load()
}

func (u *UserInterface) setTerminated() {
	u.terminated.Store(true)
}
