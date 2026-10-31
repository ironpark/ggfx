// Copyright 2019 The Ebiten Authors
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

package metal

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/ironpark/ggfx/internal/cocoa"
	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/ca"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/mtl"
)

// maximumDrawableCount is the maximum number of drawable objects.
//
// Each is a whole screen's memory, 23 MB for a window 3000 by 2000 pixels. A game drawing every
// refresh wants 3 (#2880, #2883, #3278); a UI, which draws a frame now and then and mostly a part
// of one, keeps up with 2, one on the screen and one drawn in, at 120 frames a second.
const maximumDrawableCount = 2

type view struct {
	// windowLiveResizing and windowFullscreen are what the window system last told of the view's
	// window: whether the user is resizing it, and whether it is in the fullscreen mode. They are
	// written on the window system's goroutine and taken at updatePresentationState.
	windowLiveResizing atomic.Bool
	windowFullscreen   atomic.Bool
	windowHidden       atomic.Bool

	// drawablesDropped reports whether the layer let go of its drawables while its window was
	// hidden, and must be given its drawable size again before the next drawable.
	drawablesDropped atomic.Bool

	// liveResizing reports whether the window is being resized by the user on macOS, as of the
	// current frame. This is written on the rendering thread, and can be read from the thread for
	// CAMetalDisplayLink. This is always false on iOS.
	liveResizing atomic.Bool

	// released reports whether the view was released. The display link is then invalidated and
	// never created again.
	released atomic.Bool

	device mtl.Device
	ml     ca.MetalLayer

	// pixelFormat is the layer's pixel format, which the view sets once. A draw to the screen
	// asks for it, and asking the layer is a message to it, allocating each time.
	pixelFormat mtl.PixelFormat

	once sync.Once

	caDisplayLink    uintptr
	metalDisplayLink uintptr

	// queuedPresents is the number of drawables that are queued for presentation and not presented yet.
	// This is used to wait for all the queued presentations before switching to transaction-synced
	// presentation (see updatePresentationState).
	// queuedPresents is incremented on the rendering thread and decremented on Metal's presentation thread.
	queuedPresents atomic.Int32

	// presentedHandler is the handler called when a drawable is presented.
	// presentedHandler is created at most once and reused for all the drawables.
	// A zero presentedHandler means the handler is not created yet or not available.
	presentedHandler objc.Block

	// presentedHandlerOnce guards the creation of presentedHandler.
	presentedHandlerOnce sync.Once

	// presentsWithTransaction is the last value set for the layer's presentsWithTransaction property.
	// This is true only while the window is being resized by the user on macOS, and always false on iOS.
	presentsWithTransaction bool

	// drawableWidth and drawableHeight are the last drawable size set at setDrawableSize.
	// These are used only on the rendering thread, and only on macOS.
	drawableWidth  int
	drawableHeight int

	// drawableSizeDirty reports whether the drawable size recorded at setDrawableSize is not
	// applied to the layer yet. This is used only on the rendering thread, and only on macOS.
	drawableSizeDirty bool

	// runOnMainThread runs a function on the main thread synchronously.
	// This is set at most once at the initialization on macOS, and nil on iOS.
	runOnMainThread func(f func())

	// The following members are used only with CAMetalDisplayLink.
	drawableCh               chan ca.MetalDrawable
	drawableDoneCh           chan struct{}
	drawableTimer            *time.Timer
	drawableFromDisplayLink  bool
	metalDisplayLinkRunLoop  cocoa.NSRunLoop
	metalDisplayLinkDelegate objc.ID

	// The following members are used only with CADisplayLink.
	handleToSelf viewHandle
	fence        *fence
	// linkStopped is whether the display link stopped for want of frames (see
	// waitForDisplayLinkOutputCallback), and linkIdleTimer stops it; linkMu guards them and
	// caDisplayLink, used on the rendering thread and the timer's.
	linkMu        sync.Mutex
	linkStopped   bool
	linkIdleTimer *time.Timer
}

func (v *view) getMTLDevice() mtl.Device {
	return v.device
}

// setWindowState records what the view's window is doing, for the next frame to take. A window
// just hidden makes the layer let go of its drawables.
func (v *view) setWindowState(liveResizing, fullscreen, hidden bool) {
	v.windowLiveResizing.Store(liveResizing)
	v.windowFullscreen.Store(fullscreen)
	if v.windowHidden.Swap(hidden) != hidden && hidden {
		v.dropDrawables()
	}
}

func (v *view) colorPixelFormat() mtl.PixelFormat {
	return v.pixelFormat
}

func (v *view) initialize(device mtl.Device, colorSpace color.ColorSpace) error {
	v.device = device

	ml, err := ca.NewMetalLayer(colorSpace)
	if err != nil {
		return fmt.Errorf("metal: ca.NewMetalLayer failed: %w", err)
	}
	v.ml = ml
	v.ml.SetDevice(v.device)
	// https://developer.apple.com/documentation/quartzcore/cametallayer/1478155-pixelformat
	//
	// The pixel format for a Metal layer must be MTLPixelFormatBGRA8Unorm,
	// MTLPixelFormatBGRA8Unorm_sRGB, MTLPixelFormatRGBA16Float, MTLPixelFormatBGRA10_XR, or
	// MTLPixelFormatBGRA10_XR_sRGB.
	v.pixelFormat = mtl.PixelFormatBGRA8UNorm
	v.ml.SetPixelFormat(v.pixelFormat)

	// Presents wait for the display. The layer's default might differ (#1364).
	v.ml.SetDisplaySyncEnabled(true)
	// Not framebuffer only: the next frame copies the drawable presented last (see Kept).
	v.ml.SetFramebufferOnly(false)

	// presentsWithTransaction is disabled at first, and updated at every frame on macOS
	// (see updatePresentationState). This is always disabled on iOS.
	v.ml.SetPresentsWithTransaction(false)

	v.ml.SetMaximumDrawableCount(maximumDrawableCount)

	if err := v.initializeOS(); err != nil {
		return err
	}

	return nil
}

// viewHandle is a cgo-free replacement for cgo.Handle to pass *view through C callbacks.
type viewHandle uintptr

var (
	viewHandleMu      sync.Mutex
	viewHandleMap     = map[viewHandle]*view{}
	viewHandleCounter viewHandle
)

func newViewHandle(v *view) viewHandle {
	viewHandleMu.Lock()
	defer viewHandleMu.Unlock()
	viewHandleCounter++
	h := viewHandleCounter
	viewHandleMap[h] = v
	return h
}

func deleteViewHandle(h viewHandle) {
	viewHandleMu.Lock()
	defer viewHandleMu.Unlock()
	delete(viewHandleMap, h)
}

func (h viewHandle) Value() *view {
	viewHandleMu.Lock()
	defer viewHandleMu.Unlock()
	return viewHandleMap[h]
}

type fence struct {
	value     uint64
	lastValue uint64
	cond      *sync.Cond
}

func newFence() *fence {
	return &fence{
		cond: sync.NewCond(&sync.Mutex{}),
	}
}

func (f *fence) wait() {
	f.cond.L.Lock()
	defer f.cond.L.Unlock()
	for f.lastValue >= f.value {
		f.cond.Wait()
	}
	f.lastValue = f.value
}

func (f *fence) advance() {
	f.cond.L.Lock()
	defer f.cond.L.Unlock()
	f.value++
	f.cond.Broadcast()
}
