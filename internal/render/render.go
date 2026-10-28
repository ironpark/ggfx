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
	"errors"
	"image"
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

	// running reports whether frames run, so that a read of pixels can wait for one.
	running atomic.Bool

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

// InitGraphicsDriver creates the graphics driver for lib and makes it the driver everything is
// drawn with. canvas is the browser's canvas, and is ignored elsewhere.
func InitGraphicsDriver(lib GraphicsLibrary, colorSpace color.ColorSpace, canvas any) (graphicsdriver.Graphics, error) {
	g, lib, err := newGraphicsDriver(newGraphicsDriverCreator(colorSpace, canvas), lib)
	if err != nil {
		return nil, err
	}
	driver = g
	library.Store(int32(lib))
	return g, nil
}

// GraphicsDriver returns the graphics driver, or nil before InitGraphicsDriver.
func GraphicsDriver() graphicsdriver.Graphics {
	return driver
}

// Library returns the graphics library in use, or GraphicsLibraryUnknown before
// InitGraphicsDriver.
func Library() GraphicsLibrary {
	return GraphicsLibrary(library.Load())
}

// MaxImageSize returns the maximum image size the graphics driver supports.
func MaxImageSize() int {
	return graphicscommand.MaxImageSize(driver)
}

// ColorSpace returns the graphics driver's color space.
func ColorSpace() color.ColorSpace {
	return driver.ColorSpace()
}

// SetRunning records whether frames run.
func SetRunning(r bool) {
	running.Store(r)
}

// Error returns the error a draw or a read of pixels ran into, if any.
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

// BeginFrame begins a frame. Images are drawn and their pixels read between BeginFrame and
// EndFrame, which must be called even when what follows BeginFrame fails.
func BeginFrame() error {
	return atlas.BeginFrame(driver)
}

// EndFrame ends the frame BeginFrame began.
func EndFrame() error {
	return atlas.EndFrame(driver)
}

// NewSurface creates a presentation target for target, which is what the platform's window has.
// It must be called on the main thread.
func NewSurface(target any, transparent bool) (graphicsdriver.Surface, error) {
	return driver.NewSurface(target, transparent)
}

// DisposeSurface releases surface once the commands queued before it ran.
func DisposeSurface(surface graphicsdriver.Surface) {
	graphicscommand.DisposeSurface(surface)
}

// FinishForcedFrame completes a frame drawn outside the loop, during a live resize.
func FinishForcedFrame() error {
	return graphicscommand.FinishForcedFrame(driver)
}

// NeedsClearingScreen reports whether a screen image must be cleared at the start of a frame.
func NeedsClearingScreen() bool {
	return driver.NeedsClearingScreen()
}

func readPixels(img *Image, pixels []byte, region image.Rectangle) error {
	if !running.Load() {
		panic("render: ReadPixels cannot be called before frames run")
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

// ProcessFuncsInFrame runs the functions queued to run inside a frame, like a read of pixels that
// missed one. It must be called inside a frame.
func ProcessFuncsInFrame() error {
	var processed bool
	for {
		select {
		case f := <-funcsInFrameCh:
			f()
			processed = true
		default:
			if processed {
				// Catch the error that happened at (*Image).At.
				if err := Error(); err != nil {
					return err
				}
			}
			return nil
		}
	}
}
