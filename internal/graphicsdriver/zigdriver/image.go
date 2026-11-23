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

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Image implements graphicsdriver.Image backed by a native handle ID.
type Image struct {
	g              *Graphics
	id             uint32
	width          int
	height         int
	internalWidth  int
	internalHeight int
	format         graphicsdriver.PixelFormat
	screen         bool
	disposed       bool
}

var _ graphicsdriver.Image = (*Image)(nil)

// NewImage implements graphicsdriver.Graphics.
func (g *Graphics) NewImage(width, height int, format graphicsdriver.PixelFormat) (graphicsdriver.Image, error) {
	if g.closed.Load() {
		return nil, ErrClosed
	}

	var abiFmt uint16
	switch format {
	case graphicsdriver.PixelFormatRGBA8:
		abiFmt = uint16(PixelFormatRGBA8)
	case graphicsdriver.PixelFormatA8:
		if !g.SupportsPixelFormat(graphicsdriver.PixelFormatA8) {
			return nil, fmt.Errorf("%w: PixelFormatA8 is not supported by this backend", ErrUnsupported)
		}
		abiFmt = uint16(PixelFormatA8)
	default:
		return nil, fmt.Errorf("%w: unknown pixel format %v", ErrInvalidArgument, format)
	}

	graphicsdriver.CheckSize("zigdriver", width, height, g.MaxImageSize())

	iw := graphics.InternalImageSize(width)
	ih := graphics.InternalImageSize(height)
	id := g.imageIDs.alloc()
	g.ring.cur().encodeNewImage(id, uint32(width), uint32(height), uint32(iw), uint32(ih), abiFmt)
	return &Image{
		g:              g,
		id:             id,
		width:          width,
		height:         height,
		internalWidth:  iw,
		internalHeight: ih,
		format:         format,
	}, nil
}

// Dispose implements graphicsdriver.Image.
func (i *Image) Dispose() {
	if i.disposed || i.g.closed.Load() {
		return
	}
	i.disposed = true
	i.g.ring.cur().encodeDisposeImage(i.id)
	i.g.imageIDs.deferRelease(i.id)
}

func validatePixelRegion(r image.Rectangle, width, height, bpp, pixLen int, op string) error {
	if r.Min.X < 0 || r.Min.Y < 0 || r.Max.X < r.Min.X || r.Max.Y < r.Min.Y || r.Max.X > width || r.Max.Y > height {
		return fmt.Errorf("%w: %s region %v out of image bounds %dx%d", ErrInvalidArgument, op, r, width, height)
	}
	want := bpp * r.Dx() * r.Dy()
	if pixLen != want {
		return fmt.Errorf("%w: %s pixels length %d != expected %d", ErrInvalidArgument, op, pixLen, want)
	}
	return nil
}

// WritePixels implements graphicsdriver.Image.
func (i *Image) WritePixels(args []graphicsdriver.PixelsArgs) error {
	if i.g.closed.Load() {
		return ErrClosed
	}
	if i.disposed {
		return ErrUnknownHandle
	}
	if i.screen {
		return fmt.Errorf("%w: cannot write pixels to a screen image", ErrInvalidArgument)
	}
	if len(args) == 0 {
		return nil
	}

	bpp := i.format.BytesPerPixel()
	for _, a := range args {
		if err := validatePixelRegion(a.Region, i.internalWidth, i.internalHeight, bpp, len(a.Pixels), "WritePixels"); err != nil {
			return err
		}
	}

	if i.g.ring.cur().encodeWritePixels(i.id, args) {
		return nil
	}

	// Flush pending commands and retry once with an empty slot (rotating the shared
	// upload arena if the remaining bytes cannot hold the full write).
	if i.g.ring.cur().cmdCount > 0 {
		if err := i.g.submitCurrent(FlushIntermediate); err != nil {
			return err
		}
	}

	cur := i.g.ring.cur()
	if i.g.shared.enabled && !i.g.shared.canFitWritePixels(args) {
		totalNeed, ok := i.g.shared.writePixelsNeed(args)
		if !ok || !i.g.acquireSharedArena(totalNeed) {
			if len(args) == 1 {
				cur.forceHeap = true
			}
		}
	}
	if cur.encodeWritePixels(i.id, args) {
		return nil
	}

	// If multiple regions combined exceed the arena or slot payload limit, stream them one by one.
	for idx := range args {
		single := args[idx : idx+1]
		if i.g.ring.cur().encodeWritePixels(i.id, single) {
			continue
		}
		if i.g.ring.cur().cmdCount > 0 {
			if err := i.g.submitCurrent(FlushIntermediate); err != nil {
				return err
			}
		}
		cur = i.g.ring.cur()
		if i.g.shared.enabled && !i.g.shared.canFitWritePixels(single) {
			if !i.g.acquireSharedArena(uint64(len(args[idx].Pixels))) {
				cur.forceHeap = true
			}
		}
		if !cur.encodeWritePixels(i.id, single) {
			if !cur.forceHeap && i.g.shared.enabled {
				cur.forceHeap = true
				if cur.encodeWritePixels(i.id, single) {
					continue
				}
			}
			if cur.cmdCount == 0 && cur.payLen == 0 {
				cur.forceHeap = false
			}
			return fmt.Errorf("%w: WritePixels region (%d bytes) exceeds slot payload limit", ErrOutOfMemory, len(args[idx].Pixels))
		}
	}
	return nil
}

// ReadPixels implements graphicsdriver.Image.
func (i *Image) ReadPixels(args []graphicsdriver.PixelsArgs) error {
	if i.g.closed.Load() {
		return ErrClosed
	}
	if i.disposed {
		return ErrUnknownHandle
	}
	if i.screen {
		return fmt.Errorf("%w: cannot read pixels from a screen image", ErrInvalidArgument)
	}
	if len(args) == 0 {
		return nil
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	bpp := i.format.BytesPerPixel()
	i.g.readRegions = i.g.readRegions[:0]
	for _, a := range args {
		if err := validatePixelRegion(a.Region, i.internalWidth, i.internalHeight, bpp, len(a.Pixels), "ReadPixels"); err != nil {
			return err
		}
		var dstPtr uintptr
		if len(a.Pixels) > 0 {
			pinner.Pin(&a.Pixels[0])
			dstPtr = uintptr(unsafe.Pointer(&a.Pixels[0]))
		}
		i.g.readRegions = append(i.g.readRegions, ReadRegion{
			Rect: Rect{
				X0: int32(a.Region.Min.X),
				Y0: int32(a.Region.Min.Y),
				X1: int32(a.Region.Max.X),
				Y1: int32(a.Region.Max.Y),
			},
			Dst:    dstPtr,
			DstLen: uint64(len(a.Pixels)),
		})
	}

	// Commands still queued must run before the readback, so they ride along.
	cur := i.g.ring.cur()
	var pendingPtr *Batch
	var use batchUse
	if cur.cmdCount > 0 {
		use = i.g.stageBatch(cur)
		pendingPtr = &i.g.batch
	}

	i.g.errBuf[0] = 0
	st := i.g.lib.readPixels(
		i.g.dev,
		pendingPtr,
		i.id,
		&i.g.readRegions[0],
		uint32(len(i.g.readRegions)),
		&i.g.errBuf[0],
		uintptr(len(i.g.errBuf)),
	)
	runtime.KeepAlive(args)
	runtime.KeepAlive(cur)
	runtime.KeepAlive(&i.g.batch)
	runtime.KeepAlive(i.g.readRegions)

	if pendingPtr != nil {
		i.g.retireBatch(cur, use, st)
	}

	return statusToError("read_pixels", st, i.g.errBuf[:])
}
