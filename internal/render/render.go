// Copyright 2026 The Ebiten Authors
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

// Package render is the renderer: the graphics driver, the images drawn with it, and
// the frames they are drawn in. It knows nothing of windows. The atlas and the command queue
// under it are process-wide, and so is its state.
package render

import (
	"context"
	"errors"
	"image"
	"sync"
	"sync/atomic"

	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
)

var (
	// driver is the graphics driver. It is set once, before any frame: by Init, or in the browser
	// by the first NewSurface, as WebGL draws to the canvas it is created for.
	driver  graphicsdriver.Graphics
	library atomic.Int32

	// options are what Init was given, kept for a driver NewSurface creates.
	options Options

	// state is where the renderer is in its life: before Init, running, or terminated.
	state atomic.Int32

	// lifecycleM serializes teardown (Terminate) against synchronous between-frame reads and Trim
	// so no admitted read or trim overlaps render-thread shutdown or driver Close.
	lifecycleM sync.RWMutex

	// stopRenderThread stops the render thread Init started, and renderThreadDone is closed
	// when it has stopped. Both are nil when the render thread is the caller's.
	stopRenderThread context.CancelFunc
	renderThreadDone chan struct{}

	theErr atomic.Pointer[error]

	// whiteImage is the source a fill draws from.
	whiteImage *Image
)

const (
	stateNew int32 = iota
	stateRunning
	stateTerminated
)

func init() {
	library.Store(int32(GraphicsLibraryUnknown))

	whiteImage = NewImage(3, 3, atlas.ImageTypeRegular)
	pix := make([]byte, 4*whiteImage.width*whiteImage.height)
	for i := range pix {
		pix[i] = 0xff
	}
	// As a white image is used at Fill, use WritePixels instead.
	whiteImage.WritePixels(pix, image.Rect(0, 0, whiteImage.width, whiteImage.height))
}

// Options are what Init is given.
type Options struct {
	// GraphicsLibrary is the graphics library to draw with. GraphicsLibraryAuto chooses one, and
	// the GGFX_GRAPHICS_LIBRARY environment variable can name one then.
	GraphicsLibrary GraphicsLibrary

	ColorSpace color.ColorSpace

	// SingleThread makes the caller's goroutine run the graphics commands, instead of a thread
	// of the renderer's own. The browser always runs them so.
	SingleThread bool

	// MainThread runs a function on the OS main thread and returns when it has run. Metal sets
	// the layer's properties through it while a window is resized by the user. Nil runs the
	// function on the calling goroutine.
	MainThread func(func())
}

// Init creates the graphics driver and starts the render thread. It is called once, before
// anything is drawn: images can be made and drawn to before, but they are not rendered until a
// frame after Init. Init must be called on the main thread.
//
// In the browser, the driver is created by the first NewSurface instead, for the canvas it is
// given.
func Init(o *Options) error {
	if state.Load() != stateNew {
		return errors.New("render: Init is called twice")
	}
	g, lib, err := newGraphicsDriver(o.GraphicsLibrary, o)
	if err != nil {
		return err
	}
	options = *o
	if g != nil {
		setDriver(g)
	}
	library.Store(int32(lib))

	if !o.SingleThread {
		graphicscommand.SetOSThreadAsRenderThread()
		ctx, cancel := context.WithCancel(context.Background())
		stopRenderThread = cancel
		renderThreadDone = make(chan struct{})
		go func() {
			defer close(renderThreadDone)
			graphicscommand.LoopRenderThread(ctx)
		}()
	}
	state.Store(stateRunning)
	return nil
}

// setDriver makes g the graphics driver.
func setDriver(g graphicsdriver.Graphics) {
	if r, ok := g.(graphicsdriver.MainThreadRunner); ok && options.MainThread != nil {
		r.SetMainThreadRunner(options.MainThread)
	}
	driver = g
}

// Terminate waits for the queued graphics commands to run and stops the render thread. Nothing
// is rendered after it. A window system calls it before it destroys the context or the windows
// the commands draw to.
// Terminate does nothing before Init or when called again.
func Terminate() {
	lifecycleM.Lock()
	defer lifecycleM.Unlock()

	if !state.CompareAndSwap(stateRunning, stateTerminated) {
		return
	}
	graphicscommand.Terminate()
	if stopRenderThread != nil {
		stopRenderThread()
		<-renderThreadDone
	}
	if c, ok := driver.(graphicsdriver.Closer); ok {
		_ = c.Close()
	}
}

// Terminated reports whether Terminate was called.
func Terminated() bool {
	return state.Load() == stateTerminated
}

// SetGLContext sets the OpenGL context everything is drawn in on the desktop. It shares objects
// with every surface's context and is never presented. A window system that uses OpenGL creates
// it after Init, with what IsOpenGLES reports, and sets it before the first frame.
func SetGLContext(c interface{ MakeContextCurrent() error }) {
	if setter, ok := driver.(graphicsdriver.GLContextSetter); ok {
		setter.SetDrawingContext(c)
	}
}

// GraphicsDriver returns the graphics driver, or nil before Init.
func GraphicsDriver() graphicsdriver.Graphics {
	return driver
}

// Library returns the graphics library in use, or GraphicsLibraryUnknown before Init.
func Library() GraphicsLibrary {
	return GraphicsLibrary(library.Load())
}

// DriverImplementation returns the active graphics driver implementation ("go" or "zig"),
// or "" before Init.
func DriverImplementation() string {
	if v, ok := driverImpl.Load().(string); ok {
		return v
	}
	return ""
}

// IsOpenGLES reports whether the OpenGL driver draws with OpenGL ES, which decides the context a
// window system creates for it. It is false for the other drivers.
func IsOpenGLES() bool {
	g, ok := driver.(graphicsdriver.ESReporter)
	return ok && g.IsES()
}

// SupportsDirectComposition reports whether the DirectX driver presents through
// DirectComposition, so that a window can be created without a redirection surface (#3489).
func SupportsDirectComposition() bool {
	d, ok := driver.(graphicsdriver.DirectCompositionReporter)
	return ok && d.SupportsDirectComposition()
}

// Occluded reports whether the last present went to a swap chain nothing of which is visible,
// and so returned without waiting for the display.
func Occluded() bool {
	o, ok := driver.(graphicsdriver.OcclusionReporter)
	return ok && o.IsOccluded()
}

// Flush runs the graphics commands queued in the frames since the last flush, and presents every
// surface whose screen was drawn to when present is set. It ends a frame: it advances the frame
// clock, recent.Now, and runs the frame sweepers.
func Flush(present bool) error {
	sweep(recent.Advance())
	if err := atlas.FlushCommands(driver, present); err != nil {
		return err
	}
	runPostFlushHooks()
	return nil
}

// Trim releases cached resources that age by frame count (mipmap levels, registered frame sweepers
// such as vector stencil caches, atlas pages, command-queue staging buffers, and upload byte pools)
// as if a long idle had elapsed. Must be called outside a frame.
func Trim() error {
	lifecycleM.RLock()
	defer lifecycleM.RUnlock()

	if state.Load() != stateRunning {
		return nil
	}

	drainErr := atlas.DrainForTrim(driver)

	const minAdvance = 2*graphicscommand.BufferPeakWindow + recent.Window
	cur := recent.Now()
	target := ((cur+minAdvance)/recent.Window + 1) * recent.Window
	now := recent.AdvanceBy(target - cur)

	sweep(now)
	trimErr := atlas.Trim(driver, now)
	err := errors.Join(drainErr, trimErr)
	if err != nil {
		setError(err)
	}
	return err
}

// loadError returns the error a read of pixels ran into, if any.
func loadError() error {
	if err := theErr.Load(); err != nil {
		return *err
	}
	return nil
}

// setError records e, joined with the errors recorded before it.
func setError(e error) {
	if e == nil {
		return
	}
	for {
		oldErr := theErr.Load()
		var newErr error
		if oldErr != nil {
			newErr = errors.Join(*oldErr, e)
		} else {
			newErr = e
		}
		if theErr.CompareAndSwap(oldErr, &newErr) {
			break
		}
	}
}

// Tick returns the frame clock, recent.Now: the clock that the image GC, the glyph caches and every
// buffer that gives back unused room age by.
func Tick() int64 {
	return recent.Now()
}

// CompletedFrames returns the number of final (non-intermediate) flushes whose driver End has
// completed without error on the render thread.
func CompletedFrames() uint64 {
	return graphicscommand.CompletedFrames()
}

// drainError returns and clears the error a read of pixels ran into, if any.
func drainError() error {
	if err := theErr.Swap(nil); err != nil {
		return *err
	}
	return nil
}

// BeginFrame begins a frame. Screens are drawn between BeginFrame and EndFrame, which is called
// when BeginFrame succeeded. BeginFrame returns the error a read of pixels ran into, as the read
// itself cannot.
func BeginFrame() error {
	if err := atlas.BeginFrame(driver); err != nil {
		return err
	}
	if err := drainError(); err != nil {
		return errors.Join(err, atlas.EndFrame(driver))
	}
	return nil
}

// DiscardDeferred drops what was drawn outside a frame and waits for the next BeginFrame, without
// running it. See host.DiscardDeferred.
func DiscardDeferred() {
	atlas.DiscardDeferred()
}

// EndFrame ends the frame BeginFrame began.
func EndFrame() error {
	return atlas.EndFrame(driver)
}

// FinishForcedFrame waits for a frame drawn and flushed while the main thread was blocked, as in
// a live resize on macOS, to be presented.
func FinishForcedFrame() error {
	return graphicscommand.FinishForcedFrame(driver)
}

func readPixels(img *Image, pixels []byte, region image.Rectangle) error {
	lifecycleM.RLock()
	defer lifecycleM.RUnlock()

	if state.Load() != stateRunning {
		panic("render: ReadPixels cannot be called before Init or after Terminate")
	}
	if loadError() != nil {
		return nil
	}
	if driver == nil || !atlas.Inited() {
		return errors.New("render: graphics driver is not initialized")
	}
	return img.readPixels(pixels, region)
}

func readPixel(img *Image, x, y int) ([4]byte, error) {
	lifecycleM.RLock()
	defer lifecycleM.RUnlock()

	if state.Load() != stateRunning {
		panic("render: ReadPixels cannot be called before Init or after Terminate")
	}
	if loadError() != nil {
		return [4]byte{}, nil
	}
	if driver == nil || !atlas.Inited() {
		return [4]byte{}, errors.New("render: graphics driver is not initialized")
	}
	return img.readPixel(x, y)
}
