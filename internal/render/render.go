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

// Package render is the renderer: the graphics driver, the images and shaders drawn with it, and
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
)

var (
	// driver is the graphics driver. It is set once, before any frame.
	driver  graphicsdriver.Graphics
	library atomic.Int32

	// running reports whether Init was called and Terminate was not, so that a read of pixels
	// can wait for a frame.
	running atomic.Bool

	// stopRenderThread stops the render thread Init started, and renderThreadDone is closed
	// when it has stopped. Both are nil when the render thread is the caller's.
	stopRenderThread context.CancelFunc
	renderThreadDone chan struct{}
	terminateOnce    sync.Once

	theErr atomic.Pointer[error]
	tick   atomic.Int64

	// funcsInFrameCh carries functions that must run inside a frame, like reading pixels.
	funcsInFrameCh = make(chan func())

	// whiteImage is the source a fill draws from.
	whiteImage *Image
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
	// the EBITENGINE_GRAPHICS_LIBRARY environment variable can name one then.
	GraphicsLibrary GraphicsLibrary

	ColorSpace color.ColorSpace

	// SingleThread makes the caller's goroutine run the graphics commands, instead of a thread
	// of the renderer's own. The browser always runs them so.
	SingleThread bool

	// MainThread runs a function on the OS main thread and returns when it has run. Metal sets
	// the layer's properties through it while a window is resized by the user. Nil runs the
	// function on the calling goroutine.
	MainThread func(func())

	// Canvas is the browser's canvas, the only one WebGL draws to. It is ignored elsewhere.
	Canvas any
}

// Init creates the graphics driver and starts the render thread. It is called once, before
// anything is drawn: images can be made and drawn to before, but they are not rendered until a
// frame after Init. Init must be called on the main thread.
func Init(o *Options) error {
	if driver != nil {
		return errors.New("render: Init is called twice")
	}
	g, lib, err := newGraphicsDriver(newGraphicsDriverCreator(o.ColorSpace, o.Canvas), o.GraphicsLibrary)
	if err != nil {
		return err
	}
	if r, ok := g.(interface{ SetMainThreadRunner(func(func())) }); ok && o.MainThread != nil {
		r.SetMainThreadRunner(o.MainThread)
	}
	driver = g
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
	running.Store(true)
	return nil
}

// Terminate waits for the queued graphics commands to run and stops the render thread. Nothing
// is rendered after it. A window system calls it before it destroys the context or the windows
// the commands draw to.
func Terminate() {
	terminateOnce.Do(func() {
		running.Store(false)
		graphicscommand.Terminate()
		if stopRenderThread != nil {
			stopRenderThread()
			<-renderThreadDone
		}
	})
}

// SetGLContext sets the OpenGL context everything is drawn in on the desktop. It shares objects
// with every surface's context and is never presented. A window system that uses OpenGL creates
// it after Init, with what IsOpenGLES reports, and sets it before the first frame.
func SetGLContext(c interface{ MakeContextCurrent() error }) {
	driver.(interface {
		SetDrawingContext(interface{ MakeContextCurrent() error })
	}).SetDrawingContext(c)
}

// GraphicsDriver returns the graphics driver, or nil before Init.
func GraphicsDriver() graphicsdriver.Graphics {
	return driver
}

// Library returns the graphics library in use, or GraphicsLibraryUnknown before Init.
func Library() GraphicsLibrary {
	return GraphicsLibrary(library.Load())
}

// IsOpenGLES reports whether the OpenGL driver draws with OpenGL ES, which decides the context a
// window system creates for it. It is false for the other drivers.
func IsOpenGLES() bool {
	g, ok := driver.(interface{ IsES() bool })
	return ok && g.IsES()
}

// SupportsDirectComposition reports whether the DirectX driver presents through
// DirectComposition, so that a window can be created without a redirection surface (#3489).
func SupportsDirectComposition() bool {
	d, ok := driver.(interface{ SupportsDirectComposition() bool })
	return ok && d.SupportsDirectComposition()
}

// Occluded reports whether the last present went to a swap chain nothing of which is visible,
// and so returned without waiting for the display.
func Occluded() bool {
	o, ok := driver.(interface{ IsOccluded() bool })
	return ok && o.IsOccluded()
}

// Flush runs the graphics commands queued in the frames since the last flush, and presents every
// surface whose screen was drawn to when present is set.
func Flush(present bool) error {
	return atlas.FlushCommands(driver, present)
}

// MaxImageSize returns the maximum image size the graphics driver supports.
func MaxImageSize() int {
	return graphicscommand.MaxImageSize(driver)
}

// ColorSpace returns the graphics driver's color space.
func ColorSpace() color.ColorSpace {
	return driver.ColorSpace()
}

// Error returns the error a read of pixels ran into, if any.
func Error() error {
	if err := theErr.Load(); err != nil {
		return *err
	}
	return nil
}

// SetError records err, joined with the errors recorded before it.
func SetError(e error) {
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

// Tick returns the number of times IncrementTick was called.
func Tick() int64 {
	return tick.Load()
}

// IncrementTick advances the clock that the image GC and the glyph caches expire against.
func IncrementTick() {
	tick.Add(1)
}

// BeginFrame begins a frame. Screens are drawn between BeginFrame and EndFrame, which is called
// when BeginFrame succeeded. A read of pixels that missed the last frame runs at BeginFrame, and
// BeginFrame returns the error a read of pixels ran into, as the read itself cannot.
func BeginFrame() error {
	if err := atlas.BeginFrame(driver); err != nil {
		return err
	}
	processFuncsInFrame()
	if err := Error(); err != nil {
		return errors.Join(err, atlas.EndFrame(driver))
	}
	return nil
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
	if !running.Load() {
		panic("render: ReadPixels cannot be called before Init or after Terminate")
	}

	ok, err := img.readPixels(pixels, region)
	if err != nil {
		return err
	}

	// ReadPixels failed since this was called in between two frames.
	// Try this again at the next frame.
	if !ok {
		// If this function is called from the same sequence as a frame's event handler,
		// this causes a dead lock.
		// This never happens so far, but if handling inputs after EndFrame is implemented,
		// this might be possible (#1704).

		var err error
		runInFrame(func() {
			ok, imgErr := img.readPixels(pixels, region)
			if imgErr != nil {
				err = imgErr
				return
			}
			if !ok {
				// This never reaches since this function must be called in a frame.
				panic("render: ReadPixels unexpectedly failed")
			}
		})
		return err
	}

	return nil
}

// runInFrame runs f inside a frame, on the goroutine running the frame, and waits for it.
func runInFrame(f func()) {
	ch := make(chan struct{})
	funcsInFrameCh <- func() {
		defer close(ch)
		f()
	}
	<-ch
}

// processFuncsInFrame runs the functions queued to run inside a frame, like a read of pixels that
// missed one. It must be called inside a frame.
func processFuncsInFrame() {
	for {
		select {
		case f := <-funcsInFrameCh:
			f()
		default:
			return
		}
	}
}
