// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The line rasterization here is golang.org/x/image/vector's floating-point
// one (raster_floating.go), changed to write only a window of the mask,
// each row of it on its own, and to find where a line crosses each row
// from the line's start.

package rasterx

import (
	"image"
	"math"
)

// rasterizer computes the coverage mask of a path in a window win of the
// target, in float32 at every size. Each row of cells is summed on its
// own, from 0 at its start, in one fixed order: the mask is the same on
// every architecture. A cell left of the window is its first, which the
// row's sum starts from; one right of it is a spare past its end, which
// the window does not draw. Where a line crosses a row is found from the
// line's start, not stepped to from the row above. So the window's mask is
// the whole target's, whatever part of the path it holds, but for the
// order the cells left of it are added in.
type rasterizer struct {
	evenOdd bool // fill by the even-odd rule, not the non-zero one
	win     image.Rectangle
	cells   []float32 // the window's rows, win.Dx()+1 cells each
	alphas  []uint32  // a row's mask
}

// reset clears the window win, filled by the even-odd rule or not.
func (z *rasterizer) reset(win image.Rectangle, evenOdd bool) {
	z.evenOdd = evenOdd
	z.win = win
	z.cells = grow(z.cells, (win.Dx()+1)*win.Dy())
	z.alphas = grow(z.alphas, win.Dx())
}

func grow[T float32 | uint32](s []T, n int) []T {
	if n > cap(s) {
		return make([]T, n)
	}
	s = s[:n]
	clear(s)
	return s
}

// row is the window's row y of cells, its spare last.
func (z *rasterizer) row(y int32) []float32 {
	w := z.win.Dx() + 1
	r := int(y) - z.win.Min.Y
	return z.cells[r*w : r*w+w : r*w+w]
}

// add adds v to the cell of column x in buf, a row of the window.
func (z *rasterizer) add(buf []float32, x int32, v float32) {
	i := int(x) - z.win.Min.X
	switch {
	case i < 0:
		i = 0
	case i >= len(buf):
		i = len(buf) - 1
	}
	buf[i] += v
}

func floatingFloor(x float32) int32 { return int32(math.Floor(float64(x))) }
func floatingCeil(x float32) int32  { return int32(math.Ceil(float64(x))) }

// The float32(...) conversions of products below keep the compiler from
// fusing them with an addition, as it may on arm64 but not on amd64 or
// wasm, which would round differently.

func (z *rasterizer) lineTo(ax, ay, bx, by float32) {
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

	// Rows above the window are skipped: x is where the line enters a
	// row, and xNext where it leaves it, each found from the line's
	// start, so that rounding does not build up row by row.
	y := max(floatingFloor(ay), int32(z.win.Min.Y))
	yMax := min(floatingCeil(by), int32(z.win.Max.Y))
	top := max(float32(y), ay)
	x := ax + float32((top-ay)*dxdy)
	for ; y < yMax; y++ {
		bottom := min(float32(y+1), by)
		xNext := ax + float32((bottom-ay)*dxdy)
		buf := z.row(y)
		d := float32((bottom - top) * dir)
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
			z.add(buf, x0i+0, d-float32(d*xmf))
			z.add(buf, x0i+1, float32(d*xmf))
		} else {
			s := 1 / (x1 - x0)
			x0f := x0 - x0Floor
			oneMinusX0f := 1 - x0f
			a0 := float32(0.5 * s * oneMinusX0f * oneMinusX0f)
			x1f := x1 - x1Ceil + 1
			am := float32(0.5 * s * x1f * x1f)

			z.add(buf, x0i, float32(d*a0))

			if x1i == x0i+2 {
				z.add(buf, x0i+1, float32(d*(1-a0-am)))
			} else {
				a1 := float32(s * (1.5 - x0f))
				z.add(buf, x0i+1, float32(d*(a1-a0)))
				dTimesS := float32(d * s)
				for xi := x0i + 2; xi < x1i-1; xi++ {
					z.add(buf, xi, dTimesS)
				}
				a2 := a1 + float32(s*float32(x1i-x0i-3))
				z.add(buf, x1i-1, float32(d*(1-a2-am)))
			}

			z.add(buf, x1i, float32(d*am))
		}

		top, x = bottom, xNext
	}
}

// almost65536 scales a floating point value in the range [0, 1] to a
// uint16 value in the range [0x0000, 0xffff]. See vector's
// raster_floating.go.
const almost65536 = 255.99998 * 256

// rowAlphas is the mask of the window's row y, alphas from 0 to 0xffff,
// of its coverage, a running sum of signed areas. The non-zero rule
// clamps it to 1; the even-odd rule folds it modulo 2: covered twice over
// is not covered, one and a half times is half.
func (z *rasterizer) rowAlphas(y int) []uint32 {
	out := z.alphas
	cells := z.row(int32(y))[:len(out)]
	// A cell of 0, as most are inside a shape and out of it, leaves the
	// sum, and so the alpha, as it was.
	acc, a := float32(0), uint32(0)
	if z.evenOdd {
		for i, v := range cells {
			if v != 0 {
				acc += v
				a = evenOddAlpha(acc)
			}
			out[i] = a
		}
		return out
	}
	for i, v := range cells {
		if v != 0 {
			acc += v
			a = uint32(almost65536 * min(abs(acc), 1))
		}
		out[i] = a
	}
	return out
}

// abs is |x|, without a branch.
func abs(x float32) float32 { return math.Float32frombits(math.Float32bits(x) &^ (1 << 31)) }

// evenOddAlpha is the mask of coverage acc by the even-odd rule.
func evenOddAlpha(acc float32) uint32 {
	a := abs(acc)
	if a > 1 {
		a -= 2 * float32(int32(a/2)) // exact: a/2 and its whole part are
		if a > 1 {
			a = 2 - a
		}
	}
	return uint32(almost65536 * a)
}
