// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The line rasterization here is golang.org/x/image/vector's (raster_fixed.go
// and raster_floating.go), changed to write only a window of the mask, each
// row of it on its own.

package rasterx

import (
	"image"
	"math"
)

// floatingPointMathThreshold is the width or height above which vector's
// Rasterizer, and so this one, accumulates in floating point rather than
// fixed point.
const floatingPointMathThreshold = 512

// rasterizer computes the coverage mask of a path in a window win of a
// target of size, in vector's fixed or floating point as size chooses.
// Each row of cells is summed on its own, from 0 at its start, in one
// fixed order: the mask is the same on every architecture. A cell left of
// the window is its first, which the row's sum starts from; one right of
// it is a spare past its end, which the window does not draw. So the
// window's mask is the whole target's, whatever part of the path it
// holds; in floating point, but for the order the cells left of it are
// added in.
type rasterizer struct {
	floating bool
	evenOdd  bool // fill by the even-odd rule, not the non-zero one
	win      image.Rectangle
	fixed    []int2ϕ   // the window's rows, win.Dx()+1 cells each, in fixed point
	float    []float32 // or in floating point
	alphas   []uint32  // a row's mask
}

// reset clears the window win of a target of size, filled by the
// even-odd rule or not.
func (z *rasterizer) reset(size image.Point, win image.Rectangle, evenOdd bool) {
	z.floating = size.X > floatingPointMathThreshold || size.Y > floatingPointMathThreshold
	z.evenOdd = evenOdd
	z.win = win
	n := (win.Dx() + 1) * win.Dy()
	if z.floating {
		z.float = grow(z.float, n)
	} else {
		z.fixed = grow(z.fixed, n)
	}
	z.alphas = grow(z.alphas, win.Dx())
}

func grow[T int2ϕ | float32 | uint32](s []T, n int) []T {
	if n > cap(s) {
		return make([]T, n)
	}
	s = s[:n]
	clear(s)
	return s
}

// row is the window's row y of cells, its spare last.
func row[T int2ϕ | float32](z *rasterizer, cells []T, y int32) []T {
	w := z.win.Dx() + 1
	r := int(y) - z.win.Min.Y
	return cells[r*w : r*w+w : r*w+w]
}

// add adds v to the cell of column x in buf, a row of the window.
func add[T int2ϕ | float32](z *rasterizer, buf []T, x int32, v T) {
	i := int(x) - z.win.Min.X
	switch {
	case i < 0:
		i = 0
	case i >= len(buf):
		i = len(buf) - 1
	}
	buf[i] += v
}

func (z *rasterizer) lineTo(ax, ay, bx, by float32) {
	if z.floating {
		z.floatingLineTo(ax, ay, bx, by)
	} else {
		z.fixedLineTo(ax, ay, bx, by)
	}
}

const (
	// ϕ is the number of binary digits after the fixed point.
	ϕ = 9

	fxOne          int1ϕ = 1 << ϕ
	fxOneAndAHalf  int1ϕ = 1<<ϕ + 1<<(ϕ-1)
	fxOneMinusIota int1ϕ = 1<<ϕ - 1 // Used for rounding up.
)

// int1ϕ is a signed fixed-point number with 1*ϕ binary digits after the fixed
// point.
type int1ϕ int32

// int2ϕ is a signed fixed-point number with 2*ϕ binary digits after the fixed
// point.
type int2ϕ int32

func fixedFloor(x int1ϕ) int32 { return int32(x >> ϕ) }
func fixedCeil(x int1ϕ) int32  { return int32((x + fxOneMinusIota) >> ϕ) }

func (z *rasterizer) fixedLineTo(ax, ay, bx, by float32) {
	dir := int1ϕ(1)
	if ay > by {
		dir, ax, ay, bx, by = -1, bx, by, ax, ay
	}
	// Horizontal line segments yield no change in coverage. Almost horizontal
	// segments would yield some change, in ideal math, but the computation
	// further below, involving 1 / (by - ay), is unstable in fixed point math,
	// so we treat the segment as if it was perfectly horizontal.
	if by-ay <= 0.000001 {
		return
	}
	dxdy := (bx - ax) / (by - ay)

	ayϕ := int1ϕ(ay * float32(fxOne))
	byϕ := int1ϕ(by * float32(fxOne))

	x := int1ϕ(ax * float32(fxOne))
	y := fixedFloor(ayϕ)
	yMax := min(fixedCeil(byϕ), int32(z.win.Max.Y))

	for ; y < yMax; y++ {
		dy := min(int1ϕ(y+1)<<ϕ, byϕ) - max(int1ϕ(y)<<ϕ, ayϕ)
		xNext := x + int1ϕ(float32(dy)*dxdy)
		if y < int32(z.win.Min.Y) {
			// x steps from the line's start, row by row, as vector's
			// does, rounding as it goes.
			x = xNext
			continue
		}
		buf := row(z, z.fixed, y)
		d := dy * dir // d ranges up to ±1<<(1*ϕ).
		x0, x1 := x, xNext
		if x > xNext {
			x0, x1 = x1, x0
		}
		x0i := fixedFloor(x0)
		x0Floor := int1ϕ(x0i) << ϕ
		x1i := fixedCeil(x1)
		x1Ceil := int1ϕ(x1i) << ϕ

		if x1i <= x0i+1 {
			xmf := (x+xNext)>>1 - x0Floor
			add(z, buf, x0i+0, int2ϕ(d*(fxOne-xmf)))
			add(z, buf, x0i+1, int2ϕ(d*xmf))
		} else {
			oneOverS := x1 - x0
			twoOverS := 2 * oneOverS
			x0f := x0 - x0Floor
			oneMinusX0f := fxOne - x0f
			oneMinusX0fSquared := oneMinusX0f * oneMinusX0f
			x1f := x1 - x1Ceil + fxOne
			x1fSquared := x1f * x1f

			// In ideal math: buf[i] += uint32(d * a0), where
			// a0 := ((oneMinusX0f * oneMinusX0f) >> 1) / oneOverS
			{
				D := oneMinusX0fSquared // D ranges up to ±1<<(2*ϕ).
				D *= d                  // D ranges up to ±1<<(3*ϕ).
				D /= twoOverS
				add(z, buf, x0i, int2ϕ(D))
			}

			if x1i == x0i+2 {
				// In ideal math: buf[i] += uint32(d * (fxOne - a0 - am))
				D := twoOverS<<ϕ - oneMinusX0fSquared - x1fSquared // D ranges up to ±1<<(2*ϕ+2).
				D *= d                                             // D ranges up to ±1<<(3*ϕ+2).
				D /= twoOverS
				add(z, buf, x0i+1, int2ϕ(D))
			} else {
				// See vector's raster_fixed.go for the derivations of
				// these.
				{
					D := (fxOneAndAHalf-x0f)<<(ϕ+1) - oneMinusX0fSquared // D ranges up to ±1<<(2*ϕ+2).
					D *= d                                               // D ranges up to ±1<<(3*ϕ+2).
					D /= twoOverS
					add(z, buf, x0i+1, int2ϕ(D))
				}
				dTimesS := int2ϕ((d << (2 * ϕ)) / oneOverS)
				for xi := x0i + 2; xi < x1i-1; xi++ {
					add(z, buf, xi, dTimesS)
				}
				{
					const C = 1<<(ϕ+2) - fxOneAndAHalf<<1
					D := x1f<<1 + C // D ranges up to ±1<<(1*ϕ+2).
					D <<= ϕ         // D ranges up to ±1<<(2*ϕ+2).
					D -= x1fSquared // D ranges up to ±1<<(2*ϕ+3).
					D *= d          // D ranges up to ±1<<(3*ϕ+3).
					D /= twoOverS
					add(z, buf, x1i-1, int2ϕ(D))
				}
			}

			// In ideal math: buf[i] += uint32(d * am), where
			// am := ((x1f * x1f) >> 1) / oneOverS
			{
				D := x1fSquared // D ranges up to ±1<<(2*ϕ).
				D *= d          // D ranges up to ±1<<(3*ϕ).
				D /= twoOverS
				add(z, buf, x1i, int2ϕ(D))
			}
		}

		x = xNext
	}
}

func floatingFloor(x float32) int32 { return int32(math.Floor(float64(x))) }
func floatingCeil(x float32) int32  { return int32(math.Ceil(float64(x))) }

// The float32(...) conversions of products below keep the compiler from
// fusing them with an addition, as it may on arm64 but not on amd64 or
// wasm, which would round differently.

func (z *rasterizer) floatingLineTo(ax, ay, bx, by float32) {
	dir := float32(1)
	if ay > by {
		dir, ax, ay, bx, by = -1, bx, by, ax, ay
	}
	// Horizontal line segments yield no change in coverage. Almost horizontal
	// segments would yield some change, in ideal math, but the computation
	// further below, involving 1 / (by - ay), is unstable in floating point
	// math, so we treat the segment as if it was perfectly horizontal.
	if by-ay <= 0.000001 {
		return
	}
	dxdy := (bx - ax) / (by - ay)

	x := ax
	y := floatingFloor(ay)
	yMax := min(floatingCeil(by), int32(z.win.Max.Y))
	for ; y < yMax; y++ {
		dy := min(float32(y+1), by) - max(float32(y), ay)
		xNext := x + float32(dy*dxdy)
		if y < int32(z.win.Min.Y) {
			x = xNext // as fixedLineTo's
			continue
		}
		buf := row(z, z.float, y)
		d := float32(dy * dir)
		x0, x1 := x, xNext
		if x > xNext {
			x0, x1 = x1, x0
		}
		x0i := floatingFloor(x0)
		x0Floor := float32(x0i)
		x1i := floatingCeil(x1)
		x1Ceil := float32(x1i)

		if x1i <= x0i+1 {
			xmf := float32(0.5*(x+xNext)) - x0Floor
			add(z, buf, x0i+0, d-float32(d*xmf))
			add(z, buf, x0i+1, float32(d*xmf))
		} else {
			s := 1 / (x1 - x0)
			x0f := x0 - x0Floor
			oneMinusX0f := 1 - x0f
			a0 := float32(0.5 * s * oneMinusX0f * oneMinusX0f)
			x1f := x1 - x1Ceil + 1
			am := float32(0.5 * s * x1f * x1f)

			add(z, buf, x0i, float32(d*a0))

			if x1i == x0i+2 {
				add(z, buf, x0i+1, float32(d*(1-a0-am)))
			} else {
				a1 := float32(s * (1.5 - x0f))
				add(z, buf, x0i+1, float32(d*(a1-a0)))
				dTimesS := float32(d * s)
				for xi := x0i + 2; xi < x1i-1; xi++ {
					add(z, buf, xi, dTimesS)
				}
				a2 := a1 + float32(s*float32(x1i-x0i-3))
				add(z, buf, x1i-1, float32(d*(1-a2-am)))
			}

			add(z, buf, x1i, float32(d*am))
		}

		x = xNext
	}
}

// almost65536 scales a floating point value in the range [0, 1] to a
// uint16 value in the range [0x0000, 0xffff]. See vector's
// raster_floating.go.
const almost65536 = 255.99998 * 256

// The masks below are of a coverage, a sum of signed areas. The non-zero
// rule clamps it to 1; the even-odd rule folds it modulo 2: covered twice
// over is not covered, one and a half times is half.

func (z *rasterizer) fixedAlpha(acc int2ϕ) uint32 {
	a := acc
	if a < 0 {
		a = -a
	}
	if z.evenOdd {
		a &= 1<<(2*ϕ+1) - 1
		if a > 1<<(2*ϕ) {
			a = 1<<(2*ϕ+1) - a
		}
	}
	a >>= 2*ϕ - 16
	return uint32(min(a, 0xffff))
}

func (z *rasterizer) floatingAlpha(acc float32) uint32 {
	a := acc
	if a < 0 {
		a = -a
	}
	if a > 1 {
		if !z.evenOdd {
			return 0xffff
		}
		a -= 2 * float32(int32(a/2)) // exact: a/2 and its whole part are
		if a > 1 {
			a = 2 - a
		}
	}
	return uint32(almost65536 * a)
}

// rowAlphas is the mask of the window's row y, alphas from 0 to 0xffff.
func (z *rasterizer) rowAlphas(y int) []uint32 {
	out := z.alphas
	if z.floating {
		acc := float32(0)
		for i, v := range row(z, z.float, int32(y))[:len(out)] {
			acc += v
			out[i] = z.floatingAlpha(acc)
		}
		return out
	}
	acc := int2ϕ(0)
	for i, v := range row(z, z.fixed, int32(y))[:len(out)] {
		acc += v
		out[i] = z.fixedAlpha(acc)
	}
	return out
}
