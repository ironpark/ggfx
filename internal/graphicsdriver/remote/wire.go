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

package remote

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"unsafe"
)

// Version is the version of the wire format the package doc describes.
const Version = 1

// The operations of a batch.
const (
	opSetVertices = iota + 1
	opNewImage
	opNewScreenImage
	opDisposeImage
	opNewShader
	opDisposeShader
	opWritePixels
	opDrawTriangles
	opPresent
)

const (
	batchMagic = "GGFX"
	infoMagic  = "GGFI"

	// HeaderSize is the size of a batch's header.
	HeaderSize = 20

	// InfoSize is the size of an encoded Info.
	InfoSize = 12

	// allDamaged is the damage count of a present that changed all of the screen.
	allDamaged = math.MaxUint32
)

// Info is what the viewer's driver answers to the queries an app's driver is asked, so that the
// app draws as it would on that driver: which way a projection goes, how large an image can be,
// which pixel formats there are, and the color space the screen is in.
type Info struct {
	ColorSpace   uint8
	FlipsY       bool
	A8           bool
	MaxImageSize int
}

// MarshalBinary encodes info with the version of this package.
func (info Info) MarshalBinary() ([]byte, error) {
	if info.ColorSpace > 2 {
		return nil, fmt.Errorf("remote: invalid color space %d", info.ColorSpace)
	}
	if info.MaxImageSize <= 0 || info.MaxImageSize > math.MaxInt32 {
		return nil, fmt.Errorf("remote: invalid max image size %d", info.MaxImageSize)
	}
	b := make([]byte, 0, InfoSize)
	b = append(b, infoMagic...)
	b = binary.LittleEndian.AppendUint16(b, Version)
	var flags uint8
	if info.FlipsY {
		flags |= 1
	}
	if info.A8 {
		flags |= 2
	}
	b = append(b, info.ColorSpace, flags)
	b = binary.LittleEndian.AppendUint32(b, uint32(info.MaxImageSize))
	return b, nil
}

// VersionError reports that the two sides speak different versions of the wire format.
type VersionError struct {
	Got, Want int
}

func (e *VersionError) Error() string {
	return fmt.Sprintf("remote: the other side speaks wire format version %d; this one speaks %d", e.Got, e.Want)
}

// UnmarshalBinary decodes an Info. It returns a *VersionError for another version of the format.
func (info *Info) UnmarshalBinary(b []byte) error {
	if len(b) < 6 || string(b[:4]) != infoMagic {
		return errors.New("remote: not a driver info")
	}
	if v := int(binary.LittleEndian.Uint16(b[4:])); v != Version {
		return &VersionError{Got: v, Want: Version}
	}
	if len(b) != InfoSize {
		return fmt.Errorf("remote: a driver info is %d bytes, not %d", InfoSize, len(b))
	}
	flags := b[7]
	if flags&^3 != 0 {
		return fmt.Errorf("remote: unknown driver info flags %#x", flags)
	}
	*info = Info{
		ColorSpace:   b[6],
		FlipsY:       flags&1 != 0,
		A8:           flags&2 != 0,
		MaxImageSize: int(binary.LittleEndian.Uint32(b[8:])),
	}
	if info.ColorSpace > 2 {
		return fmt.Errorf("remote: invalid color space %d", info.ColorSpace)
	}
	if info.MaxImageSize <= 0 || info.MaxImageSize > math.MaxInt32 {
		return fmt.Errorf("remote: invalid max image size %d", info.MaxImageSize)
	}
	return nil
}

// Header is the header of a batch.
type Header struct {
	// Screen is the app's screen image as of the end of the batch, and its size; 0 when the app
	// has none.
	Screen        uint32
	Width, Height int
}

// ParseHeader returns the header of batch. It returns a *VersionError for a batch of another
// version of the format. ParseHeader performs wire-format validation only and does not check
// device MaxImageSize or Replayer resource limits; viewers should call (*Replayer).ScreenSize
// before allocating a screen target.
func ParseHeader(batch []byte) (Header, error) {
	if len(batch) < 6 || string(batch[:4]) != batchMagic {
		return Header{}, errors.New("remote: not a batch")
	}
	if v := int(binary.LittleEndian.Uint16(batch[4:])); v != Version {
		return Header{}, &VersionError{Got: v, Want: Version}
	}
	if len(batch) < HeaderSize {
		return Header{}, errors.New("remote: the batch's header is cut short")
	}
	if binary.LittleEndian.Uint16(batch[6:]) != 0 {
		return Header{}, errors.New("remote: the batch's reserved header field is not 0")
	}
	h := Header{
		Screen: binary.LittleEndian.Uint32(batch[8:]),
		Width:  int(binary.LittleEndian.Uint32(batch[12:])),
		Height: int(binary.LittleEndian.Uint32(batch[16:])),
	}
	if (h.Screen == 0) != (h.Width == 0 && h.Height == 0) || h.Width < 0 || h.Height < 0 ||
		h.Width > math.MaxInt32 || h.Height > math.MaxInt32 {
		return Header{}, fmt.Errorf("remote: invalid screen in the batch's header: %d, %dx%d", h.Screen, h.Width, h.Height)
	}
	if h.Screen != 0 && (h.Width == 0 || h.Height == 0) {
		return Header{}, fmt.Errorf("remote: invalid screen size in the batch's header: %dx%d", h.Width, h.Height)
	}
	return h, nil
}

// enc appends to a batch.
type enc struct{ b []byte }

func (e *enc) u8(v uint8)   { e.b = append(e.b, v) }
func (e *enc) u32(v uint32) { e.b = binary.LittleEndian.AppendUint32(e.b, v) }
func (e *enc) i32(v int)    { e.u32(uint32(int32(v))) }
func (e *enc) u64(v uint64) { e.b = binary.LittleEndian.AppendUint64(e.b, v) }

func (e *enc) rect(r image.Rectangle) {
	e.i32(r.Min.X)
	e.i32(r.Min.Y)
	e.i32(r.Max.X)
	e.i32(r.Max.Y)
}

// bytes appends an array.
func (e *enc) bytes(p []byte) {
	n := len(e.b)
	pad := (-n) & 3
	total := 4 + pad + len(p)
	e.b = slices.Grow(e.b, total)
	dst := e.b[n : n+total]
	binary.LittleEndian.PutUint32(dst, uint32(len(p)))
	clear(dst[4 : 4+pad])
	copy(dst[4+pad:], p)
	e.b = e.b[:n+total]
}

func (e *enc) f32s(v []float32) {
	if len(v) == 0 {
		e.bytes(nil)
		return
	}
	e.bytes(unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), 4*len(v)))
}

func (e *enc) u32s(v []uint32) {
	if len(v) == 0 {
		e.bytes(nil)
		return
	}
	e.bytes(unsafe.Slice((*byte)(unsafe.Pointer(&v[0])), 4*len(v)))
}

// writeHeader writes h into dst[:HeaderSize] without allocating.
func writeHeader(dst []byte, h Header) {
	_ = dst[HeaderSize-1]
	copy(dst[:4], batchMagic)
	binary.LittleEndian.PutUint16(dst[4:6], Version)
	binary.LittleEndian.PutUint16(dst[6:8], 0)
	binary.LittleEndian.PutUint32(dst[8:12], h.Screen)
	binary.LittleEndian.PutUint32(dst[12:16], uint32(h.Width))
	binary.LittleEndian.PutUint32(dst[16:20], uint32(h.Height))
}

// header starts a batch.
func (e *enc) header(h Header) {
	e.b = append(e.b[:0], batchMagic...)
	e.b = binary.LittleEndian.AppendUint16(e.b, Version)
	e.b = binary.LittleEndian.AppendUint16(e.b, 0)
	e.u32(h.Screen)
	e.u32(uint32(h.Width))
	e.u32(uint32(h.Height))
}

// errShort is the error of a batch that ends in the middle of an operation.
var errShort = errors.New("remote: the batch ends in the middle of an operation")

// dec reads a batch. A read past the end sets err and returns zeros, so that a sequence of reads
// can be checked once.
type dec struct {
	b   []byte
	p   int
	err error
}

func (d *dec) more() bool { return d.err == nil && d.p < len(d.b) }

func (d *dec) need(n int) bool {
	if d.err != nil {
		return false
	}
	if n < 0 || n > len(d.b)-d.p {
		d.err = errShort
		return false
	}
	return true
}

func (d *dec) u8() uint8 {
	if !d.need(1) {
		return 0
	}
	v := d.b[d.p]
	d.p++
	return v
}

func (d *dec) u32() uint32 {
	if !d.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(d.b[d.p:])
	d.p += 4
	return v
}

func (d *dec) u64() uint64 {
	if !d.need(8) {
		return 0
	}
	v := binary.LittleEndian.Uint64(d.b[d.p:])
	d.p += 8
	return v
}

func (d *dec) rect() image.Rectangle {
	x0, y0 := int32(d.u32()), int32(d.u32())
	x1, y1 := int32(d.u32()), int32(d.u32())
	return image.Rectangle{Min: image.Pt(int(x0), int(y0)), Max: image.Pt(int(x1), int(y1))}
}

// count reads a count of items each at least size bytes long, and checks that they can fit in
// what is left, so that a corrupt count cannot make the reader allocate much.
func (d *dec) count(size int) int {
	n := d.u32()
	if d.err != nil {
		return 0
	}
	if uint64(n)*uint64(size) > uint64(len(d.b)-d.p) {
		d.err = errShort
		return 0
	}
	return int(n)
}

// bytes returns the next array, in place.
func (d *dec) bytes() []byte {
	n := d.u32()
	if d.err != nil {
		return nil
	}
	for d.p%4 != 0 {
		if !d.need(1) {
			return nil
		}
		d.p++
	}
	if uint64(n) > uint64(len(d.b)-d.p) {
		d.err = errShort
		return nil
	}
	v := d.b[d.p : d.p+int(n) : d.p+int(n)]
	d.p += int(n)
	return v
}

// f32s returns the next array as float32s: in place when the batch is 4-byte aligned in memory, as
// a Go allocation or a mapped page is, and copied otherwise. Every platform ggfx runs on is
// little-endian, the format's own order, which in place relies on.
func (d *dec) f32s() []float32 {
	b := d.bytes()
	if d.err != nil || len(b) == 0 {
		return nil
	}
	if len(b)%4 != 0 {
		d.err = errors.New("remote: a float32 array's length is not a multiple of 4")
		return nil
	}
	if uintptr(unsafe.Pointer(&b[0]))%4 != 0 {
		v := make([]float32, len(b)/4)
		for i := range v {
			v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
		}
		return v
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4)
}

// u32s is f32s for uint32s.
func (d *dec) u32s() []uint32 {
	b := d.bytes()
	if d.err != nil || len(b) == 0 {
		return nil
	}
	if len(b)%4 != 0 {
		d.err = errors.New("remote: a uint32 array's length is not a multiple of 4")
		return nil
	}
	if uintptr(unsafe.Pointer(&b[0]))%4 != 0 {
		v := make([]uint32, len(b)/4)
		for i := range v {
			v[i] = binary.LittleEndian.Uint32(b[4*i:])
		}
		return v
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(&b[0])), len(b)/4)
}
