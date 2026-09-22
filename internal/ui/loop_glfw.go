// Copyright 2024 The Ebitengine Authors
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
	stdcontext "context"
	"errors"
	"runtime"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ironpark/ggfx/internal/colormode"
	"github.com/ironpark/ggfx/internal/gamepad"
	"github.com/ironpark/ggfx/internal/glfw"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/hook"
	"github.com/ironpark/ggfx/internal/thread"
)

// errWindowClosed is returned by updateWindow when the window asked to close.
var errWindowClosed = errors.New("ui: window closed")

func (u *UserInterface) run(game Game, options *RunOptions) error {
	if options.SingleThread || buildTagSingleThread || runtime.GOOS == "js" {
		return u.runSingleThread(game, options)
	}
	return u.runMultiThread(game, options)
}

func (u *UserInterface) runMultiThread(game Game, options *RunOptions) error {
	u.mainThread = thread.NewOSThread()
	graphicscommand.SetOSThreadAsRenderThread()

	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	var wg errgroup.Group

	// Run the render thread.
	wg.Go(func() error {
		defer cancel()

		graphicscommand.LoopRenderThread(ctx)
		return nil
	})

	// Run the game thread.
	wg.Go(func() error {
		defer cancel()

		var err error
		u.mainThread.Call(func() {
			err = u.initOnMainThread(game, options)
		})
		if err != nil {
			return err
		}

		// The backend is published at the window creation in initOnMainThread.
		defer u.setRunningBackend(nil)

		return u.loopGame()
	})

	// Run the main thread. The loop is the thread's whole life, so a call arriving after
	// it ends is a no-op rather than a block forever.
	_ = u.mainThread.LoopAndStop(ctx)
	return wg.Wait()
}

func (u *UserInterface) runSingleThread(game Game, options *RunOptions) error {
	// Initialize the main thread first so the thread is available at u.run (#809).
	u.mainThread = thread.NewNoopThread()

	// The backend is published at the window creation in initOnMainThread.
	defer u.setRunningBackend(nil)

	if err := u.initOnMainThread(game, options); err != nil {
		return err
	}

	if err := u.loopGame(); err != nil {
		return err
	}

	return nil
}

// initOnMainThread initializes GLFW and the graphics driver, and creates the primary window for
// the game. It must be called on the main thread.
func (u *UserInterface) initOnMainThread(game Game, options *RunOptions) error {
	if err := u.initGraphicsOnMainThread(options); err != nil {
		return err
	}

	// Center the window on the monitor if the position was not explicitly set.
	if !options.WindowPositionSet {
		m := u.getInitMonitor()
		if m != nil {
			sw, sh := m.sizeInDIP()
			x, y := InitialWindowPosition(int(sw), int(sh), options.InitWindowWidthInDIP, options.InitWindowHeightInDIP)
			u.Window().SetPosition(x, y)
		}
	}

	w := newGLFWBackend(u)
	w.context = newContext(game, options.ScreenTransparent)
	u.primary.Store(w)
	if err := w.createWindowOnMainThread(options); err != nil {
		return err
	}
	u.windows = append(u.windows, w)
	return nil
}

// initGraphicsOnMainThread initializes GLFW and creates the graphics driver. It must be called on
// the main thread, once.
func (u *UserInterface) initGraphicsOnMainThread(options *RunOptions) error {
	if err := u.ensureGLFWInit(); err != nil {
		return err
	}

	setApplePressAndHoldEnabled(options.ApplePressAndHoldEnabled)

	g, lib, err := newGraphicsDriver(&graphicsDriverCreatorImpl{
		transparent: options.ScreenTransparent,
		colorSpace:  options.ColorSpace,
	}, options.GraphicsLibrary)
	if err != nil {
		return err
	}
	u.graphicsDriver = g
	u.setGraphicsLibrary(lib)
	u.graphicsDriver.SetTransparent(options.ScreenTransparent)

	if g, ok := u.graphicsDriver.(interface{ SetMainThreadRunner(func(func())) }); ok {
		g.SetMainThreadRunner(u.mainThread.Call)
	}
	return nil
}

// createWindowOnMainThread sets the window hints from the window's settings and options, creates
// the GLFW window, its surface and its callbacks. It must be called on the main thread after
// initGraphicsOnMainThread.
func (u *glfwBackend) createWindowOnMainThread(options *RunOptions) error {
	if err := glfw.WindowHint(glfw.AutoIconify, glfw.False); err != nil {
		return err
	}

	// Window is shown after the first buffer swap (#2725).
	if err := glfw.WindowHint(glfw.Visible, glfw.False); err != nil {
		return err
	}

	if err := glfw.WindowHintString(glfw.X11ClassName, options.X11ClassName); err != nil {
		return err
	}

	if err := glfw.WindowHintString(glfw.X11InstanceName, options.X11InstanceName); err != nil {
		return err
	}

	// On macOS, window decoration should be initialized once after buffers are swapped (#2600).
	if runtime.GOOS != "darwin" {
		decorated := glfw.False
		if u.desktopWindow.isInitWindowDecorated() {
			decorated = glfw.True
		}
		if err := glfw.WindowHint(glfw.Decorated, decorated); err != nil {
			return err
		}
	}

	glfwTransparent := glfw.False
	if options.ScreenTransparent {
		glfwTransparent = glfw.True
	}
	if err := glfw.WindowHint(glfw.TransparentFramebuffer, glfwTransparent); err != nil {
		return err
	}

	// The OpenGL driver needs a window with a GL context, unlike the other drivers.
	// Set the context-related hints before creating a window.
	lib := u.GraphicsLibrary()
	if lib == GraphicsLibraryOpenGL {
		if err := u.setOpenGLWindowHints(); err != nil {
			return err
		}
	}

	// A window created without a redirection surface shows nothing unless its content is presented
	// through DirectComposition, and only the graphics driver can tell whether that works (#3489).
	noRedirectionBitmap := glfw.False
	if d, ok := u.graphicsDriver.(interface{ SupportsDirectComposition() bool }); ok && d.SupportsDirectComposition() {
		noRedirectionBitmap = glfw.True
	}
	if err := glfw.WindowHint(glfw.Win32NoRedirectionBitmap, noRedirectionBitmap); err != nil {
		return err
	}

	// internal/glfw is customized and the default client API is NoAPI, not OpenGLAPI.
	// Then, glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI) doesn't have to be called.

	// Before creating a window, set it unresizable no matter what u.isInitWindowResizable() is (#1987).
	// Making the window resizable here doesn't work correctly when switching to enable resizing.
	resizable := glfw.False
	if WindowResizingMode(u.desktopWindow.windowResizingMode.Load()) == WindowResizingModeEnabled {
		resizable = glfw.True
	}
	if err := glfw.WindowHint(glfw.Resizable, resizable); err != nil {
		return err
	}

	floating := glfw.False
	if u.desktopWindow.isInitWindowFloating() {
		floating = glfw.True
	}
	if err := glfw.WindowHint(glfw.Floating, floating); err != nil {
		return err
	}

	u.initUnfocused = options.InitUnfocused
	focused := glfw.True
	if options.InitUnfocused {
		focused = glfw.False
	}
	if err := glfw.WindowHint(glfw.FocusOnShow, focused); err != nil {
		return err
	}

	mousePassthrough := glfw.False
	if u.desktopWindow.isInitWindowMousePassthrough() {
		mousePassthrough = glfw.True
	}
	if err := glfw.WindowHint(glfw.MousePassthrough, mousePassthrough); err != nil {
		return err
	}

	if err := u.createWindow(); err != nil {
		return err
	}

	// createWindow has published the backend. A concurrent SetPreferredColorMode thus either
	// applies the color mode by itself, or stores a value that is read here.
	if m := u.PreferredColorMode(); m != colormode.Unknown {
		if err := u.setWindowColorModeImpl(m); err != nil {
			return err
		}
	}

	// Maximizing a window requires a proper size and position. Call Maximize here (#1117).
	if u.desktopWindow.isInitWindowMaximized() {
		if err := u.window.Maximize(); err != nil {
			return err
		}
	}

	if err := u.setWindowResizingModeForOS(WindowResizingMode(u.desktopWindow.windowResizingMode.Load())); err != nil {
		return err
	}

	if options.SkipTaskbar {
		// Ignore the error.
		_ = u.skipTaskbar()
	}

	// The OpenGL driver presents through the GLFW window; the others need the native handle.
	var target any = u.window
	if lib != GraphicsLibraryOpenGL {
		w, err := u.nativeWindow()
		if err != nil {
			return err
		}
		target = w
	}
	surface, err := u.graphicsDriver.NewSurface(target)
	if err != nil {
		return err
	}
	u.surface = surface
	u.context.setSurface(surface)

	// Register callbacks after the window initialization done.
	// The callback might cause swapping frames, that assumes the window is already set (#2137).
	if err := u.registerWindowCloseCallback(); err != nil {
		return err
	}
	if err := u.registerWindowPosCallback(); err != nil {
		return err
	}
	if err := u.registerWindowFramebufferSizeCallback(); err != nil {
		return err
	}
	if err := u.registerInputCallbacks(); err != nil {
		return err
	}
	if err := u.registerDropCallback(); err != nil {
		return err
	}

	return nil
}

// destroy closes the window and releases what it owns. It must be called on the main thread.
func (u *glfwBackend) destroy() error {
	if u.closed {
		return nil
	}
	u.closed = true
	u.context.dispose()
	if u.surface != nil {
		graphicscommand.DisposeSurface(u.surface)
		u.surface = nil
	}
	if err := u.window.Destroy(); err != nil {
		return err
	}
	for i, w := range u.windows {
		if w == u {
			u.windows = append(u.windows[:i], u.windows[i+1:]...)
			break
		}
	}
	return nil
}

func (u *UserInterface) loopGame() (err error) {
	defer func() {
		graphicscommand.Terminate()
		u.mainThread.Call(func() {
			// Mark the termination before terminating GLFW so that a concurrent-safe API
			// like ScheduleFrame stops touching GLFW's state before it is destroyed.
			u.setTerminated()
			if glfwErr := glfw.Terminate(); glfwErr != nil {
				err = errors.Join(err, glfwErr)
			}
		})
	}()

	for {
		if err := u.updateGame(); err != nil {
			return err
		}
	}
}

// pumpEvents processes the OS event queue once, waiting for an event when the FPS mode says so.
// pumpEvents must be called from the main thread.
func (u *UserInterface) pumpEvents() error {
	u.pollingEvents = true
	defer func() {
		u.pollingEvents = false
	}()
	if FPSModeType(u.fpsMode.Load()) != FPSModeVsyncOffMinimum {
		// TODO: Updating the input can be skipped when clock.Update returns 0 (#1367).
		return glfw.PollEvents()
	}
	return glfw.WaitEvents()
}

// updateWindow brings the window's state up to date for a frame and returns its sizes.
// It returns errWindowClosed when the window asked to close.
//
// updateWindow must be called from the main thread.
func (u *glfwBackend) updateWindow() (outsideWidth, outsideHeight float64, screenWidth, screenHeight int, err error) {
	if err := u.error(); err != nil {
		return 0, 0, 0, 0, err
	}

	sc, err := u.window.ShouldClose()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if sc {
		return 0, 0, 0, 0, errWindowClosed
	}

	// On macOS, one swapping buffers seems required before entering fullscreen (#2599).
	if u.primary && u.isInitFullscreen() && (u.bufferOnceSwapped || runtime.GOOS != "darwin") {
		if err := u.setFullscreen(true); err != nil {
			return 0, 0, 0, 0, err
		}
		u.setInitFullscreen(false)
	}

	if runtime.GOOS == "darwin" && u.bufferOnceSwapped {
		var err error
		u.darwinInitOnce.Do(func() {
			// On macOS, window decoration should be initialized once after buffers are swapped (#2600).
			decorated := glfw.False
			if u.desktopWindow.isInitWindowDecorated() {
				decorated = glfw.True
			}
			if err = u.window.SetAttrib(glfw.Decorated, decorated); err != nil {
				return
			}
		})
		if err != nil {
			return 0, 0, 0, 0, err
		}
	}

	// Showing the window (and the focus and size adjustments that go with it) is skipped when the window
	// is initially invisible, so an application started with SetWindowVisible(false) never shows a window.
	// A later SetWindowVisible(true) shows it through the regular path.
	if u.bufferOnceSwapped && u.desktopWindow.isInitWindowVisible() {
		var err error
		u.showWindowOnce.Do(func() {
			// Show the window after first buffer swap to avoid flash of white especially on Windows.
			if err = u.window.Show(); err != nil {
				return
			}
			if !u.initUnfocused {
				if err = u.window.Focus(); err != nil {
					return
				}
			}

			if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
				return
			}

			// On Linux or UNIX, there is a problematic desktop environment like i3wm
			// where an invisible window size cannot be initialized correctly (#2951).
			// Call SetSize explicitly after the window becomes visible.

			fullscreen, e := u.isFullscreen()
			if e != nil {
				err = e
				return
			}
			if fullscreen {
				return
			}

			m, e := u.currentMonitor()
			if e != nil {
				err = e
				return
			}
			s := m.DeviceScaleFactor()
			newW, newH := windowSizeInGLFWPixels(u.windowWidthInDIP, u.windowHeightInDIP, s)

			// Even though a framebuffer callback is not called, waitForFramebufferSizeCallback returns by timeout,
			// so it is safe to use this.
			if err = u.waitForFramebufferSizeCallback(u.window, func() error {
				return u.window.SetSize(newW, newH)
			}); err != nil {
				return
			}
		})
		if err != nil {
			return 0, 0, 0, 0, err
		}
	}

	// Initialize vsync after SetMonitor is called.
	// Calling this inside setWindowSize didn't work (#1363).
	if !u.fpsModeInited {
		if err := u.setFPSMode(FPSModeType(u.fpsMode.Load())); err != nil {
			return 0, 0, 0, 0, err
		}
	}

	u.syncModKeysFromOS()
	u.syncLockKeysFromOS()

	return u.layoutSizes()
}

// waitWhileUnfocused blocks while no window is focused and the game must not run unfocused.
// waitWhileUnfocused must be called from the main thread.
func (u *UserInterface) waitWhileUnfocused() error {
	// If isRunnableOnUnfocused is false and the window is not focused, wait here.
	// For the first update, skip this check as the window might not be seen yet in some environments like ChromeOS (#3091).
	for !u.isRunnableOnUnfocused() {
		var focused, closing bool
		var swapped bool
		for _, w := range u.windows {
			if !w.bufferOnceSwapped {
				continue
			}
			swapped = true

			// In the initial state on macOS, the window is not shown (#2620).
			visible, err := w.window.GetAttrib(glfw.Visible)
			if err != nil {
				return err
			}
			if visible == glfw.False {
				focused = true
				break
			}

			f, err := w.window.GetAttrib(glfw.Focused)
			if err != nil {
				return err
			}
			if f != glfw.False {
				focused = true
				break
			}

			shouldClose, err := w.window.ShouldClose()
			if err != nil {
				return err
			}
			if shouldClose {
				closing = true
				break
			}
		}
		if !swapped || focused || closing {
			break
		}

		if err := hook.SuspendAudio(); err != nil {
			return err
		}
		// Wait for an arbitrary period to avoid busy loop.
		time.Sleep(time.Second / 60)
		if err := glfw.PollEvents(); err != nil {
			return err
		}
	}

	return hook.ResumeAudio()
}

// windowFrame is what one window needs for a frame, gathered on the main thread.
type windowFrame struct {
	window            *glfwBackend
	present           bool
	outsideWidth      float64
	outsideHeight     float64
	screenWidth       int
	screenHeight      int
	deviceScaleFactor float64
}

func (u *UserInterface) updateGame() error {
	var unfocused = true
	var monitorChanged bool
	var frames []windowFrame

	var err error
	if u.mainThread.Call(func() {
		if err = u.pumpEvents(); err != nil {
			return
		}
		if err = u.waitWhileUnfocused(); err != nil {
			return
		}

		// A window can be destroyed in the loop; iterate over a copy.
		windows := append([]*glfwBackend(nil), u.windows...)
		for _, w := range windows {
			// On Windows, the focusing state might be always false (#987).
			// On Windows, even if a window is in another workspace, vsync seems to work.
			// Then let's assume the window is always 'focused' as a workaround.
			if runtime.GOOS == "windows" {
				unfocused = false
			} else {
				a, e := w.window.GetAttrib(glfw.Focused)
				if e != nil {
					err = e
					return
				}
				if a != glfw.False {
					unfocused = false
				}
			}

			visible, e := w.window.GetAttrib(glfw.Visible)
			if e != nil {
				err = e
				return
			}
			occluded, e := w.isWindowOccluded()
			if e != nil && !errors.Is(e, errors.ErrUnsupported) {
				err = e
				return
			}
			present := shouldPresentFrame(visible == glfw.True && !occluded, w.bufferOnceSwapped, w.desktopWindow.isInitWindowVisible())

			ow, oh, sw, sh, e := w.updateWindow()
			if errors.Is(e, errWindowClosed) {
				if w.primary {
					err = RegularTermination
					return
				}
				if e := w.destroy(); e != nil {
					err = e
					return
				}
				continue
			}
			if e != nil {
				err = e
				return
			}

			m, e := w.currentMonitor()
			if e != nil {
				err = e
				return
			}
			if w.primary {
				u.setRefreshRate(m.RefreshRate())
			}
			if m != w.lastFrameMonitor {
				monitorChanged = true
			}
			w.lastFrameMonitor = m

			// Pre-fetch cursor position to avoid a second mainThread.Call round-trip in
			// updateInputStateForFrame.
			cx, cy, e := w.window.GetCursorPos()
			if e != nil {
				err = e
				return
			}
			w.input.setRawCursorPos(cx, cy)

			frames = append(frames, windowFrame{
				window:            w,
				present:           present,
				outsideWidth:      ow,
				outsideHeight:     oh,
				screenWidth:       sw,
				screenHeight:      sh,
				deviceScaleFactor: m.DeviceScaleFactor(),
			})
		}

		if len(u.windows) == 0 {
			err = RegularTermination
			return
		}

		nativeWindow, e := u.windows[0].nativeWindow()
		if e != nil {
			err = e
			return
		}
		if e := gamepad.Update(nativeWindow, nil); e != nil {
			err = e
			return
		}
	}); err != nil {
		return err
	}

	// Whether swapping buffers waits for the display can differ per monitor, e.g. when the monitors
	// are driven by different GPUs. Measure it again on the new monitor.
	if monitorChanged {
		u.pacer.resetVsyncDetection()
	}

	var needsSwapBuffers bool
	for _, f := range frames {
		if !f.window.context.wantsFrame() {
			continue
		}
		n, err := f.window.context.renderFrame(u.graphicsDriver, f.outsideWidth, f.outsideHeight, f.screenWidth, f.screenHeight, f.deviceScaleFactor, u, f.present, false)
		if err != nil {
			return err
		}
		needsSwapBuffers = needsSwapBuffers || n
	}
	if err := u.pacer.flushCommandsAndWait(needsSwapBuffers, u.graphicsDriver, u.FPSMode() == FPSModeVsyncOn, u.RefreshRate()); err != nil {
		return err
	}

	for _, f := range frames {
		w := f.window
		w.bufferOnceSwappedOnce.Do(func() {
			u.mainThread.Call(func() {
				w.bufferOnceSwapped = true
			})
		})
	}

	// When a window is not focused or in another space, SwapBuffers might return immediately and CPU might be busy.
	// Mitigate this by sleeping (#982, #2521).
	if unfocused {
		const wait = time.Second / 60
		now := time.Now()
		if next := u.unfocusedNextWake.Add(wait); next.After(now) {
			u.unfocusedNextWake = next
			time.Sleep(time.Until(next))
		} else {
			u.unfocusedNextWake = now
		}
	}

	return nil
}
