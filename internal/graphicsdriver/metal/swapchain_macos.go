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

//go:build darwin && !ios

package metal

import (
	"errors"
	"os"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/ca"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal/mtl"
)

// A CAMetalLayer keeps its drawables, two at the least, each a whole window of memory: 59 MB
// for a window filling a 5K display. A UI shows the same frame for minutes, and needs only the
// one on the screen meanwhile. The swap chain draws into IOSurfaces of its own instead, two, and
// shows them as a plain layer's contents; once no frame has come for swapIdle, the one not on
// the screen is made purgeable, out of the process's footprint, until the next frame takes it
// back. What it held is no loss: a frame starts from a copy of the one on the screen (see Kept).

// swapIdle is how long the swap chain waits for a frame before it lets the buffer not on the
// screen go.
const swapIdle = 250 * time.Millisecond

// useSwapChain reports whether surfaces draw into a swap chain of their own rather than a
// CAMetalLayer's drawables. GGFX_METAL_SWAPCHAIN=0 turns it off.
var useSwapChain = os.Getenv("GGFX_METAL_SWAPCHAIN") != "0"

// swapBuffer is one IOSurface of a swap chain and the texture drawn into it.
type swapBuffer struct {
	surface  uintptr // IOSurfaceRef, 0 when not made
	tex      mtl.Texture
	volatile bool // made purgeable while idle
}

// swapChain is the IOSurfaces a surface draws into, and the layer that shows them. acquire and
// present run on the rendering thread, idle on a timer's goroutine and drop on the main thread;
// mu orders them.
type swapChain struct {
	layer      objc.ID // a CALayer
	device     mtl.Device
	colorSpace uintptr // the CFString naming the IOSurfaces' color space

	mu            sync.Mutex
	width, height int
	bufs          [2]swapBuffer
	front         int // the buffer on the screen, or -1
	drawing       int // the buffer the frame draws into, or -1
	idleTimer     *time.Timer
}

var (
	class_CALayer       = objc.GetClass("CALayer")
	class_CATransaction = objc.GetClass("CATransaction")

	sel_layer             = objc.RegisterName("layer")
	sel_retain            = objc.RegisterName("retain")
	sel_setContents       = objc.RegisterName("setContents:")
	sel_setOpaque         = objc.RegisterName("setOpaque:")
	sel_begin             = objc.RegisterName("begin")
	sel_commit            = objc.RegisterName("commit")
	sel_setDisableActions = objc.RegisterName("setDisableActions:")
)

// ioSurface holds what the swap chain calls in IOSurface and Core Foundation.
var ioSurface struct {
	once sync.Once
	err  error

	create, isInUse, setPurgeable, setValue   uintptr
	cfRelease, dictCreate, dictSet, numCreate uintptr
	keyCallbacks, valueCallbacks              uintptr
	kWidth, kHeight, kBytesPerElement         uintptr
	kPixelFormat, kColorSpace                 uintptr
	kSRGB, kDisplayP3                         uintptr
}

const (
	purgeableNonVolatile = 0
	purgeableVolatile    = 1
	pixelFormatBGRA      = 0x42475241 // 'BGRA'
	cfNumberSInt32Type   = 3
)

func loadIOSurface() error {
	s := &ioSurface
	s.once.Do(func() {
		libs := map[string]uintptr{}
		for _, path := range []string{
			"/System/Library/Frameworks/IOSurface.framework/IOSurface",
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		} {
			lib, err := purego.Dlopen(path, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
			if err != nil {
				s.err = err
				return
			}
			libs[path] = lib
		}
		var errs []error
		fn := func(lib, name string) uintptr {
			p, err := purego.Dlsym(libs["/System/Library/Frameworks/"+lib+".framework/"+lib], name)
			if err != nil {
				errs = append(errs, err)
			}
			return p
		}
		// A constant is a pointer to the CFString, which is what is passed.
		constant := func(lib, name string) uintptr {
			p := fn(lib, name)
			if p == 0 {
				return 0
			}
			return **(**uintptr)(unsafe.Pointer(&p))
		}
		s.create = fn("IOSurface", "IOSurfaceCreate")
		s.isInUse = fn("IOSurface", "IOSurfaceIsInUse")
		s.setPurgeable = fn("IOSurface", "IOSurfaceSetPurgeable")
		s.setValue = fn("IOSurface", "IOSurfaceSetValue")
		s.kWidth = constant("IOSurface", "kIOSurfaceWidth")
		s.kHeight = constant("IOSurface", "kIOSurfaceHeight")
		s.kBytesPerElement = constant("IOSurface", "kIOSurfaceBytesPerElement")
		s.kPixelFormat = constant("IOSurface", "kIOSurfacePixelFormat")
		s.kColorSpace = constant("IOSurface", "kIOSurfaceColorSpace")
		s.cfRelease = fn("CoreFoundation", "CFRelease")
		s.dictCreate = fn("CoreFoundation", "CFDictionaryCreateMutable")
		s.dictSet = fn("CoreFoundation", "CFDictionarySetValue")
		s.numCreate = fn("CoreFoundation", "CFNumberCreate")
		// The callbacks are structs, passed by their address.
		s.keyCallbacks = fn("CoreFoundation", "kCFTypeDictionaryKeyCallBacks")
		s.valueCallbacks = fn("CoreFoundation", "kCFTypeDictionaryValueCallBacks")
		s.kSRGB = constant("CoreGraphics", "kCGColorSpaceSRGB")
		s.kDisplayP3 = constant("CoreGraphics", "kCGColorSpaceDisplayP3")
		s.err = errors.Join(errs...)
	})
	return s.err
}

// newSwapChain makes a swap chain for device, of colorSpace, and its layer.
func newSwapChain(device mtl.Device, colorSpace color.ColorSpace) (*swapChain, error) {
	if err := loadIOSurface(); err != nil {
		return nil, err
	}
	c := &swapChain{device: device, front: -1, drawing: -1}
	switch colorSpace {
	case color.ColorSpaceDisplayP3:
		c.colorSpace = ioSurface.kDisplayP3
	default:
		c.colorSpace = ioSurface.kSRGB
	}
	c.layer = objc.ID(class_CALayer).Send(sel_layer)
	c.layer.Send(sel_retain)
	// The contents fill the layer, as a CAMetalLayer's drawable does: a frame is drawn at the
	// layer's size in pixels, and a frame of another, while the window is being resized, is
	// shown stretched until the next.
	return c, nil
}

// setOpaque tells the layer whether its contents cover it.
func (c *swapChain) setOpaque(opaque bool) {
	c.layer.Send(sel_setOpaque, opaque)
}

// newSurface makes an IOSurface of width by height pixels, and the texture drawn into it.
func (c *swapChain) newSurface(width, height int) (swapBuffer, bool) {
	s := &ioSurface
	num := func(v int32) uintptr {
		r, _, _ := purego.SyscallN(s.numCreate, 0, cfNumberSInt32Type, uintptr(unsafe.Pointer(&v)))
		return r
	}
	dict, _, _ := purego.SyscallN(s.dictCreate, 0, 0, s.keyCallbacks, s.valueCallbacks)
	defer purego.SyscallN(s.cfRelease, dict)
	for _, kv := range [][2]uintptr{
		{s.kWidth, num(int32(width))},
		{s.kHeight, num(int32(height))},
		{s.kBytesPerElement, num(4)},
		{s.kPixelFormat, num(pixelFormatBGRA)},
	} {
		purego.SyscallN(s.dictSet, dict, kv[0], kv[1])
		purego.SyscallN(s.cfRelease, kv[1])
	}
	surface, _, _ := purego.SyscallN(s.create, dict)
	if surface == 0 {
		return swapBuffer{}, false
	}
	purego.SyscallN(s.setValue, surface, s.kColorSpace, c.colorSpace)
	tex := c.device.NewTextureWithIOSurface(mtl.TextureDescriptor{
		TextureType: mtl.TextureType2D,
		PixelFormat: mtl.PixelFormatBGRA8UNorm,
		Width:       width,
		Height:      height,
		StorageMode: storageMode,
		Usage:       mtl.TextureUsageRenderTarget | mtl.TextureUsageShaderRead,
	}, surface, 0)
	if tex == (mtl.Texture{}) {
		purego.SyscallN(s.cfRelease, surface)
		return swapBuffer{}, false
	}
	return swapBuffer{surface: surface, tex: tex}, true
}

// freeLocked lets go of buffer i.
func (c *swapChain) freeLocked(i int) {
	b := &c.bufs[i]
	if b.surface == 0 {
		return
	}
	b.tex.Release()
	purego.SyscallN(ioSurface.cfRelease, b.surface)
	*b = swapBuffer{}
}

// mostInUseWait is how long acquire waits for the window server to finish with the buffer it is
// to draw into, which it may still read for the frame it shows until the next refresh.
const mostInUseWait = 20 * time.Millisecond

// acquire returns the texture the next frame draws into, width by height pixels: the buffer not
// on the screen. It returns a zero texture when there is none to be had.
func (c *swapChain) acquire(width, height int) mtl.Texture {
	c.mu.Lock()
	defer c.mu.Unlock()
	if width <= 0 || height <= 0 {
		return mtl.Texture{}
	}
	if c.width != width || c.height != height {
		// The layer keeps what it shows until the first frame of the new size replaces it.
		c.freeLocked(0)
		c.freeLocked(1)
		c.width, c.height, c.front = width, height, -1
	}
	i := 0
	if c.front == 0 {
		i = 1
	}
	b := &c.bufs[i]
	if b.surface == 0 {
		nb, ok := c.newSurface(width, height)
		if !ok {
			return mtl.Texture{}
		}
		*b = nb
	}
	if b.volatile {
		// Whatever it held, the frame draws or copies over all of it.
		var old uint32
		purego.SyscallN(ioSurface.setPurgeable, b.surface, purgeableNonVolatile, uintptr(unsafe.Pointer(&old)))
		b.volatile = false
	}
	for start := time.Now(); time.Since(start) < mostInUseWait; time.Sleep(250 * time.Microsecond) {
		if r, _, _ := purego.SyscallN(ioSurface.isInUse, b.surface); r&0xff == 0 {
			break
		}
	}
	c.drawing = i
	return b.tex
}

// present shows the buffer the frame drew into once cb, committed, is scheduled, or on the main
// thread once it is completed when the window is being resized, so that the new contents and the
// window's new bounds are shown together.
func (c *swapChain) present(cb mtl.CommandBuffer, onMainThread func(func())) {
	c.mu.Lock()
	i := c.drawing
	c.drawing = -1
	if i < 0 || c.bufs[i].surface == 0 {
		c.mu.Unlock()
		return
	}
	surface := c.bufs[i].surface
	c.front = i
	if c.idleTimer == nil {
		c.idleTimer = time.AfterFunc(swapIdle, c.idle)
	} else {
		c.idleTimer.Reset(swapIdle)
	}
	c.mu.Unlock()

	show := func() {
		objc.ID(class_CATransaction).Send(sel_begin)
		objc.ID(class_CATransaction).Send(sel_setDisableActions, true)
		c.layer.Send(sel_setContents, surface)
		objc.ID(class_CATransaction).Send(sel_commit)
		// The rendering thread has no run loop to commit its implicit transaction.
		ca.FlushTransaction()
	}
	if onMainThread != nil {
		onMainThread(func() {
			cb.WaitUntilCompleted()
			show()
		})
		return
	}
	cb.WaitUntilScheduled()
	show()
}

// idle makes the buffer not on the screen purgeable, out of the footprint, for no frame came
// for swapIdle.
func (c *swapChain) idle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.bufs {
		b := &c.bufs[i]
		if i == c.front || i == c.drawing || b.surface == 0 || b.volatile {
			continue
		}
		var old uint32
		purego.SyscallN(ioSurface.setPurgeable, b.surface, purgeableVolatile, uintptr(unsafe.Pointer(&old)))
		b.volatile = true
	}
}

// drop lets go of both buffers and what the layer shows, for a window no longer shown. It runs
// on the main thread, between frames.
func (c *swapChain) drop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.drawing >= 0 {
		// A frame is being drawn; the window hides after it.
		return
	}
	c.layer.Send(sel_setContents, uintptr(0))
	c.freeLocked(0)
	c.freeLocked(1)
	c.front = -1
}

// release lets go of everything the swap chain holds.
func (c *swapChain) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
	c.freeLocked(0)
	c.freeLocked(1)
	c.front, c.drawing = -1, -1
	if c.layer != 0 {
		c.layer.Send(sel_release)
		c.layer = 0
	}
}
