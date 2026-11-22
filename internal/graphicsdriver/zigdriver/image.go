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
	id := g.allocImageID()
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
	i.g.deferFreeImageID(i.id)
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

	// Flush pending commands and retry once with an empty slot.
	if i.g.ring.cur().cmdCount > 0 {
		if err := i.g.submitCurrent(FlushIntermediate); err != nil {
			return err
		}
	}
	if i.g.ring.cur().encodeWritePixels(i.id, args) {
		return nil
	}

	// If multiple regions combined exceed the slot payload limit, stream them one by one.
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
		if !i.g.ring.cur().encodeWritePixels(i.id, single) {
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

	cur := i.g.ring.cur()
	var pendingPtr *Batch
	var usedCmd, usedPay int
	if cur.cmdCount > 0 {
		i.g.ring.nextSeq++
		cur.seq = i.g.ring.nextSeq
		cur.fillBatch(&i.g.batch)
		pendingPtr = &i.g.batch
		usedCmd = cur.cmdLen
		usedPay = cur.payLen
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
		cur.frameMaxCmd = max(cur.frameMaxCmd, usedCmd)
		cur.frameMaxPay = max(cur.frameMaxPay, usedPay)
		cur.reset()
		if st == StatusOK {
			i.g.releaseDeferredIDs()
		} else {
			i.g.discardDeferredIDs()
		}
	}

	return statusToError("read_pixels", st, i.g.errBuf[:])
}
