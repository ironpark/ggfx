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
	"fmt"
	"image"
	"image/color"
	"image/draw"

	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

var _ draw.RGBA64Image = (*Image)(nil)

// Clear resets the pixels of the image into 0.
//
// When the image is disposed, Clear does nothing.
func (i *Image) Clear() {
	i.Fill(color.Transparent)
}

// Fill fills the image with a solid color.
//
// An image of [PixelFormatAlpha] is filled with the color's alpha: its pixels then read as
// (a, a, a, a).
//
// When the image is disposed, Fill does nothing.
func (i *Image) Fill(clr color.Color) {
	i.copyCheck()
	if i.isDisposed() {
		return
	}

	i.flushPending()

	i.updateAccessTime()

	var crf, cgf, cbf, caf float32
	cr, cg, cb, ca := clr.RGBA()
	crf = float32(cr) / 0xffff
	cgf = float32(cg) / 0xffff
	cbf = float32(cb) / 0xffff
	caf = float32(ca) / 0xffff
	i.image.Fill(crf, cgf, cbf, caf, i.adjustedBounds())
}

// ReadPixels reads the image's pixels from the image.
//
// The given pixels represent RGBA pre-multiplied alpha values.
//
// ReadPixels loads pixels from GPU to system memory if necessary, which means that ReadPixels can be slow.
//
// ReadPixels panics if the image is disposed.
//
// len(pixels) must be 4 * (bounds width) * (bounds height).
// If len(pixels) is not correct, ReadPixels panics.
//
// For an image of [PixelFormatAlpha], each pixel of alpha a reads as (a, a, a, a); len(pixels)
// may instead be (bounds width) * (bounds height), and then ReadPixels reads one alpha byte a pixel.
//
// ReadPixels also works on a sub-image.
//
// Note that an important logic should not rely on values returned by ReadPixels, since
// the returned values can include very slight differences between some machines.
//
// ReadPixels can be called inside or between frames once the graphics driver is initialized
// (after host.Init, host.SetGLContext on desktop OpenGL, or the first host.NewSurface in a
// browser, and at least one frame). When host.Options.SingleThread is set, ReadPixels must be
// called on the renderer's thread, like host.BeginFrame and host.Flush.
//
// With a synchronous host.Options.MainThread callback on macOS, make pixel reads off the main
// thread while a live-resize present can be in flight. When Metal uses CAMetalLayer, also make
// reads off the main thread during live resize or fullscreen transitions.
func (i *Image) ReadPixels(pixels []byte) {
	i.copyCheck()
	b := i.Bounds()
	i.updateAccessTime()
	n := b.Dx() * b.Dy()
	if i.image.Format() == graphicsdriver.PixelFormatA8 {
		switch len(pixels) {
		case n:
			i.flushPending()
			i.image.ReadPixels(pixels, i.adjustedBounds())
		case 4 * n:
			i.flushPending()
			a := make([]byte, n)
			i.image.ReadPixels(a, i.adjustedBounds())
			graphics.ExpandAlpha(pixels, a)
		default:
			panic(fmt.Sprintf("ggfx: len(pixels) must be %d or %d but %d at ReadPixels", 4*n, n, len(pixels)))
		}
		return
	}
	if got, want := len(pixels), 4*n; got != want {
		panic(fmt.Sprintf("ggfx: len(pixels) must be %d but %d at ReadPixels", want, got))
	}

	i.flushPending()

	i.image.ReadPixels(pixels, i.adjustedBounds())
}

// At returns the color of the image at (x, y).
//
// At implements the standard image.Image's At.
//
// At loads pixels from GPU to system memory if necessary, which means that At can be slow.
//
// At always returns a transparent color if the image is disposed.
//
// Note that an important logic should not rely on values returned by At, since
// the returned values can include very slight differences between some machines.
//
// At can be called inside or between frames once the graphics driver is initialized
// (after host.Init, host.SetGLContext on desktop OpenGL, or the first host.NewSurface in a
// browser, and at least one frame). When host.Options.SingleThread is set, At must be called on
// the renderer's thread, like host.BeginFrame and host.Flush.
//
// With a synchronous host.Options.MainThread callback on macOS, make pixel reads off the main
// thread while a live-resize present can be in flight. When Metal uses CAMetalLayer, also make
// reads off the main thread during live resize or fullscreen transitions.
func (i *Image) At(x, y int) color.Color {
	r, g, b, a := i.at(x, y)
	return color.RGBA{R: r, G: g, B: b, A: a}
}

// RGBA64At implements the standard image.RGBA64Image's RGBA64At.
//
// RGBA64At loads pixels from GPU to system memory if necessary, which means
// that RGBA64At can be slow.
//
// RGBA64At always returns a transparent color if the image is disposed.
//
// Note that an important logic should not rely on values returned by RGBA64At,
// since the returned values can include very slight differences between some machines.
//
// RGBA64At can be called inside or between frames once the graphics driver is initialized
// (after host.Init, host.SetGLContext on desktop OpenGL, or the first host.NewSurface in a
// browser, and at least one frame). When host.Options.SingleThread is set, RGBA64At must be called
// on the renderer's thread, like host.BeginFrame and host.Flush.
//
// With a synchronous host.Options.MainThread callback on macOS, make pixel reads off the main
// thread while a live-resize present can be in flight. When Metal uses CAMetalLayer, also make
// reads off the main thread during live resize or fullscreen transitions.
func (i *Image) RGBA64At(x, y int) color.RGBA64 {
	r, g, b, a := i.at(x, y)
	return color.RGBA64{R: uint16(r) * 0x101, G: uint16(g) * 0x101, B: uint16(b) * 0x101, A: uint16(a) * 0x101}
}

func (i *Image) at(x, y int) (r, g, b, a byte) {
	if i.isDisposed() {
		return 0, 0, 0, 0
	}
	if !image.Pt(x, y).In(i.Bounds()) {
		return 0, 0, 0, 0
	}

	i.flushPending()

	x, y = i.adjustPosition(x, y)
	pix := i.image.ReadPixel(x, y)
	if i.image.Format() == graphicsdriver.PixelFormatA8 {
		return pix[0], pix[0], pix[0], pix[0]
	}
	return pix[0], pix[1], pix[2], pix[3]
}

// Set sets the color at (x, y).
//
// Set implements the standard draw.Image's Set.
//
// If (x, y) is outside the image bounds, Set does nothing.
//
// Even if a result is an invalid color as a premultiplied-alpha color, i.e. an alpha value exceeds other color values,
// the value is kept and is not clamped.
//
// If the image is disposed, Set does nothing.
//
// An image of [PixelFormatAlpha] keeps the color's alpha.
//
// For performance, it is recommended to use WritePixels instead of Set whenever possible.
func (i *Image) Set(x, y int, clr color.Color) {
	i.copyCheck()
	if i.isDisposed() {
		return
	}

	i.flushPending()

	i.updateAccessTime()

	if !image.Pt(x, y).In(i.Bounds()) {
		return
	}
	cr, cg, cb, ca := clr.RGBA()
	i.setPixel(x, y, byte(cr>>8), byte(cg>>8), byte(cb>>8), byte(ca>>8))
}

// SetRGBA64 sets the color at (x, y).
//
// SetRGBA64 implements the standard draw.RGBA64Image's SetRGBA64.
//
// If (x, y) is outside the image bounds, or the image is disposed, SetRGBA64 does nothing.
// An image of [PixelFormatAlpha] keeps the color's alpha.
func (i *Image) SetRGBA64(x, y int, c color.RGBA64) {
	i.copyCheck()
	if i.isDisposed() {
		return
	}

	i.flushPending()

	i.updateAccessTime()

	if !image.Pt(x, y).In(i.Bounds()) {
		return
	}
	i.setPixel(x, y, byte(c.R>>8), byte(c.G>>8), byte(c.B>>8), byte(c.A>>8))
}

func (i *Image) setPixel(x, y int, r, g, b, a byte) {
	if i.isSubImage() {
		i = i.original
	}

	dx, dy := i.adjustPosition(x, y)
	if i.image.Format() == graphicsdriver.PixelFormatA8 {
		i.image.WritePixel(dx, dy, [4]byte{a})
		return
	}
	i.image.WritePixel(dx, dy, [4]byte{r, g, b, a})
}

// WritePixels replaces the pixels of the image.
//
// The given pixels are treated as RGBA pre-multiplied alpha values.
//
// len(pix) must be 4 * (bounds width) * (bounds height).
// If len(pix) is not correct, WritePixels panics.
//
// WritePixels also works on a sub-image.
//
// Even if a result is an invalid color as a premultiplied-alpha color, i.e. an alpha value exceeds other color values,
// the value is kept and is not clamped.
//
// An image of [PixelFormatAlpha] keeps the alpha channel of the given RGBA pixels. len(pix) may
// instead be (bounds width) * (bounds height): one alpha byte a pixel, kept as it is.
//
// When the image is disposed, WritePixels does nothing.
func (i *Image) WritePixels(pixels []byte) {
	i.copyCheck()

	if i.isDisposed() {
		return
	}

	i.flushPending()
	i.updateAccessTime()

	if i.image.Format() == graphicsdriver.PixelFormatA8 {
		b := i.Bounds()
		n := b.Dx() * b.Dy()
		switch len(pixels) {
		case n:
		case 4 * n:
			a := make([]byte, n)
			graphics.ExtractChannel(a, pixels, 3)
			pixels = a
		default:
			panic(fmt.Sprintf("ggfx: len(pixels) must be %d or %d but %d at WritePixels", 4*n, n, len(pixels)))
		}
	}

	// Do not need to copy pixels here: internal/atlas copies them, to make its paddings.
	i.image.WritePixels(pixels, i.adjustedBounds())
}
