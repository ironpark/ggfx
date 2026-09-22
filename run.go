// Copyright 2014 Hajime Hoshi
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
	"io/fs"
	"runtime"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/clock"
	ecolor "github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/inputstate"
	"github.com/ironpark/ggfx/internal/ui"
)

// ActualFPS returns how many frames were presented in the last second, across every window.
//
// On some environments ActualFPS is unreliable because vsync does not work well there. The value
// is for measurement and debugging; an application should not rely on it.
//
// ActualFPS is concurrent-safe.
func ActualFPS() float64 {
	return clock.ActualFPS()
}

// runEnded reports that [Run] returned, after which no image can be created.
var runEnded atomic.Bool

// SetScreenClearedEveryFrame enables or disables the clearing of the screen at the beginning of each frame.
// The default value is true and the screen is cleared each frame by default.
//
// SetScreenClearedEveryFrame is concurrent-safe.
func SetScreenClearedEveryFrame(cleared bool) {
	ui.Get().SetScreenClearedEveryFrame(cleared)
}

// IsScreenClearedEveryFrame returns true if the screen is cleared at the beginning of each frame.
//
// IsScreenClearedEveryFrame is concurrent-safe.
func IsScreenClearedEveryFrame() bool {
	return ui.Get().IsScreenClearedEveryFrame()
}

// Termination is a special error which indicates termination without error.
var Termination = ui.RegularTermination

// RunOptions are the options of [Run].
type RunOptions struct {
	// GraphicsLibrary is a graphics library Ebitengine will use.
	//
	// The default (zero) value is GraphicsLibraryAuto, which lets Ebitengine choose the graphics library.
	GraphicsLibrary GraphicsLibrary

	// InitUnfocused starts the page without taking focus. On desktops the equivalent is
	// [WindowOptions.Unfocused], which is per window; this applies to the browser, where the
	// decision is made before the canvas is bound.
	//
	// The default (zero) value is false, which means that the page takes focus.
	InitUnfocused bool

	// ScreenTransparent creates a graphics driver that can present transparent windows. The
	// driver is created once, before any window exists, so a window with
	// [WindowOptions.Transparent] needs this, and NewWindow fails without it.
	//
	// ScreenTransparent is valid on desktops and browsers.
	//
	// The default (zero) value is false: windows are opaque.
	ScreenTransparent bool

	// SkipTaskbar indicates whether an application icon is shown on a taskbar or not. It
	// applies to every window.
	//
	// SkipTaskbar is valid only on Windows.
	//
	// The default (zero) value is false, which means that an icon is shown on a taskbar.
	SkipTaskbar bool

	// SingleThread indicates whether the single thread mode is used explicitly or not.
	// The single thread mode disables Ebitengine's thread safety to unlock maximum performance.
	// If you use this you will have to manage threads yourself.
	// Functions like `SetWindowSize` will no longer be concurrent-safe in the single thread mode.
	// They must be called from the main thread or the same goroutine as the given game's callback functions like Update.
	//
	// SingleThread works only with desktops and consoles.
	//
	// If SingleThread is false, and if the build tag `ebitenginesinglethread` is specified,
	// the single thread mode is used.
	//
	// The default (zero) value is false, which means that the single thread mode is disabled.
	SingleThread bool

	// DisableHiDPI indicates whether the rendering for HiDPI is disabled or not.
	// If HiDPI is disabled, the device scale factor is always 1 i.e. Monitor's DeviceScaleFactor always returns 1.
	// This is useful to get a better performance on HiDPI displays, at the expense of rendering quality.
	//
	// DisableHiDPI is available only on browsers.
	//
	// The default (zero) value is false, which means that HiDPI is enabled.
	DisableHiDPI bool

	// ColorSpace indicates the color space of the screen.
	//
	// ColorSpace is available only with some graphics libraries (macOS Metal and WebGL so far).
	// Otherwise, ColorSpace is ignored.
	//
	// The default (zero) value is ColorSpaceDefault, which means that color space depends on the environment.
	ColorSpace ColorSpace

	// ApplePressAndHoldEnabled indicates whether the press-and-hold feature is enabled or not.
	// If true, pressing and holding a key might show a menu to select a character glyph variant.
	// This is useful for GUI applications, but some APIs like [AppendInputChars]'s behavior is changed:
	// for example, pressing and holding Q key would not repeat 'q' by [AppendInputChars].
	// If false, pressing and holding a key repeats the key event.
	//
	// ApplePressAndHoldEnabled is available only on macOS.
	//
	// The default (zero) value is false, which means that the press-and-hold feature is disabled.
	ApplePressAndHoldEnabled bool

	// X11ClassName is a class name in the ICCCM WM_CLASS window property.
	X11ClassName string

	// X11InstanceName is an instance name in the ICCCM WM_CLASS window property.
	X11InstanceName string
}

func toUIRunOptions(options *RunOptions) *ui.RunOptions {
	const (
		defaultX11ClassName    = "Ebitengine-Application"
		defaultX11InstanceName = "ebitengine-application"
	)

	colorSpace := ecolor.ColorSpaceSRGB
	if options != nil && options.ColorSpace != ColorSpaceDefault {
		colorSpace = ecolor.ColorSpace(options.ColorSpace)
	} else if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		// On macOS or iOS, the default color space is Display P3.
		// TODO: Remove this logic at v3. sRGB should be the default in the future (#3349).
		colorSpace = ecolor.ColorSpaceDisplayP3
	}

	if options == nil {
		return &ui.RunOptions{
			ColorSpace:      colorSpace,
			X11ClassName:    defaultX11ClassName,
			X11InstanceName: defaultX11InstanceName,
		}
	}

	if options.X11ClassName == "" {
		options.X11ClassName = defaultX11ClassName
	}
	if options.X11InstanceName == "" {
		options.X11InstanceName = defaultX11InstanceName
	}

	return &ui.RunOptions{
		GraphicsLibrary:          ui.GraphicsLibrary(options.GraphicsLibrary),
		InitUnfocused:            options.InitUnfocused,
		ScreenTransparent:        options.ScreenTransparent,
		SkipTaskbar:              options.SkipTaskbar,
		SingleThread:             options.SingleThread,
		DisableHiDPI:             options.DisableHiDPI,
		ColorSpace:               colorSpace,
		ApplePressAndHoldEnabled: options.ApplePressAndHoldEnabled,
		X11ClassName:             options.X11ClassName,
		X11InstanceName:          options.X11InstanceName,
	}
}

// DroppedFiles returns a virtual file system that includes only dropped files and/or directories
// at its root directory, at the time Update is called.
//
// DroppedFiles works on desktops and browsers.
//
// As of Ebitengine 2.9, the returned value also implements [io/fs.ReadDirFS].
//
// As of Ebitengine 2.10, the returned value also implements [io/fs.ReadFileFS].
//
// As of Ebitengine 2.10, on desktops, the directory entries and the files the returned value
// provides also implement [AbsPather].
//
// DroppedFiles is concurrent-safe.
func DroppedFiles() fs.FS {
	return inputstate.Get().DroppedFiles()
}

// AbsPather is a directory entry or a file that has a path in the real file system.
type AbsPather interface {
	// AbsPath returns the absolute path in the real file system.
	AbsPath() string
}

// Tick returns the current tick count.
// The tick count starts with 0 and is incremented by one on every Update call.
//
// Tick is concurrent-safe.
func Tick() int64 {
	return ui.Get().Tick()
}

// RunOnMainThread runs the given function on the main thread.
// RunOnMainThread executes the function synchronously and returns after the function completes.
//
// If RunOnMainThread is called on the main thread, RunOnMainThread blocks forever.
//
// RunOnMainThread might not run the function e.g. before the game starts or after the game ends.
//
// RunOnMainThread is useful to access platform-specific APIs in a safe way.
//
// RunOnMainThread panics if the platform doesn't support it.
func RunOnMainThread(f func()) {
	ui.Get().RunOnMainThread(f)
}
