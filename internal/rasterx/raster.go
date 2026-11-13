// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The line rasterization and accumulation here are golang.org/x/image/vector's
// (vector.go, raster_fixed.go, raster_floating.go and the order of the sums
// in acc_amd64.s), changed to write only a window of the mask.

package rasterx

import (
	"image"
	"math"
)

// floatingPointMathThreshold is the width or height above which vector's
// Rasterizer accumulates in floating point rather than fixed point.
const floatingPointMathThreshold = 512

// rasterizer computes the coverage mask of a path exactly as a
// vector.Rasterizer of size does, but only in the window win of it, so
// that a path costs its window's area, not size's. Around a window that
// holds the path, vector's mask is the coverage's rounding error, which
// accumulate checks is too faint to change a pixel.
//
// vector's mask is one buffer, row after row, summed from its start: a
// cell past a row's end is the next row's first. So the window keeps its
// cells, and, apart, those of column 0 for the rows in and just under it
// when win does not hold it, where a path past the right edge leaves the
// coverage it took from the row.
type rasterizer struct {
	size     image.Point
	floating bool
	evenOdd  bool // fill by the even-odd rule, not the non-zero one
	win      image.Rectangle
	u32      []uint32  // the window's cells, row by row, or its mask
	f32      []float32 // the window's cells when floating
	left     []uint32  // column 0 of rows win.Min.Y to win.Max.Y, if win does not hold it
	leftF    []float32
	stray    bool // a cell outside win and left was written
}

// reset clears the window win of a mask of size, filled by the even-odd
// rule or not.
func (z *rasterizer) reset(size image.Point, win image.Rectangle, evenOdd bool) {
	z.size = size
	z.evenOdd = evenOdd
	z.floating = size.X > floatingPointMathThreshold || size.Y > floatingPointMathThreshold
	z.win = win
	z.stray = false
	n := win.Dx() * win.Dy()
	z.u32 = grow(z.u32, n)
	if z.floating {
		z.f32 = grow(z.f32, n)
		z.leftF = grow(z.leftF, win.Dy()+1)
	} else {
		z.left = grow(z.left, win.Dy()+1)
	}
}

func grow[T uint32 | float32](s []T, n int) []T {
	if n > cap(s) {
		return make([]T, n)
	}
	s = s[:n]
	clear(s)
	return s
}

func clamp(i, width int32) uint {
	if i < 0 {
		return 0
	}
	if i < width {
		return uint(i)
	}
	return uint(width)
}

// row is the window's row y of cells, empty if it has none.
func row[T uint32 | float32](z *rasterizer, cells []T, y int32) []T {
	r := int(y) - z.win.Min.Y
	if r < 0 || r >= z.win.Dy() {
		return nil
	}
	w := z.win.Dx()
	return cells[r*w : r*w+w]
}

// add adds v to the cell i, from 0 to the width, of row y, whose cells in
// the window are buf; the window's are cells, and column 0's beside it
// left.
func add[T uint32 | float32](z *rasterizer, cells, left, buf []T, y int32, i uint, v T) {
	if j := i - uint(z.win.Min.X); j < uint(len(buf)) {
		buf[j] += v
		return
	}
	addOutside(z, cells, left, int(y), int(i), v)
}

// addOutside is add of a cell outside its row's window: in the window of
// the next row, past the row's end, or in column 0 kept apart. It drops a
// cell past the mask's end, as vector does, and marks one it cannot keep
// stray.
func addOutside[T uint32 | float32](z *rasterizer, cells, left []T, y, i int, v T) {
	if i == z.size.X {
		if y, i = y+1, 0; y >= z.size.Y {
			return
		}
	}
	switch k, in := z.locate(y, i); in {
	case inWindow:
		cells[k] += v
	case inLeft:
		left[k] += v
	default:
		z.stray = true
	}
}

// Where locate finds a cell.
const (
	nowhere = iota
	inWindow
	inLeft
)

// locate finds the cell x of row y: in the window, at k in its cells; in
// column 0 kept apart, at k in left; or nowhere kept.
func (z *rasterizer) locate(y, x int) (k, in int) {
	r := y - z.win.Min.Y
	switch {
	case r < 0 || r > z.win.Dy():
		return 0, nowhere
	case r < z.win.Dy() && x >= z.win.Min.X && x < z.win.Max.X:
		return r*z.win.Dx() + x - z.win.Min.X, inWindow
	case x != 0 || (z.win.Min.X == 0 && r < z.win.Dy()):
		return 0, nowhere
	}
	return r, inLeft
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

func (z *rasterizer) lineTo(ax, ay, bx, by float32) {
	if z.floating {
		z.floatingLineTo(ax, ay, bx, by)
	} else {
		z.fixedLineTo(ax, ay, bx, by)
	}
}

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
	yMax := fixedCeil(byϕ)
	if yMax > int32(z.size.Y) {
		yMax = int32(z.size.Y)
	}
	width := int32(z.size.X)
	cells, left := z.u32, z.left

	for ; y < yMax; y++ {
		dy := min(int1ϕ(y+1)<<ϕ, byϕ) - max(int1ϕ(y)<<ϕ, ayϕ)
		xNext := x + int1ϕ(float32(dy)*dxdy)
		if y < 0 {
			x = xNext
			continue
		}
		buf := row(z, cells, y)
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
			add(z, cells, left, buf, y, clamp(x0i+0, width), uint32(d*(fxOne-xmf)))
			add(z, cells, left, buf, y, clamp(x0i+1, width), uint32(d*xmf))
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
				add(z, cells, left, buf, y, clamp(x0i, width), uint32(D))
			}

			if x1i == x0i+2 {
				// In ideal math: buf[i] += uint32(d * (fxOne - a0 - am))
				D := twoOverS<<ϕ - oneMinusX0fSquared - x1fSquared // D ranges up to ±1<<(2*ϕ+2).
				D *= d                                             // D ranges up to ±1<<(3*ϕ+2).
				D /= twoOverS
				add(z, cells, left, buf, y, clamp(x0i+1, width), uint32(D))
			} else {
				// See vector's raster_fixed.go for the derivations of
				// these.
				{
					D := (fxOneAndAHalf-x0f)<<(ϕ+1) - oneMinusX0fSquared // D ranges up to ±1<<(2*ϕ+2).
					D *= d                                               // D ranges up to ±1<<(3*ϕ+2).
					D /= twoOverS
					add(z, cells, left, buf, y, clamp(x0i+1, width), uint32(D))
				}
				dTimesS := uint32((d << (2 * ϕ)) / oneOverS)
				for xi := x0i + 2; xi < x1i-1; xi++ {
					add(z, cells, left, buf, y, clamp(xi, width), dTimesS)
				}
				{
					const C = 1<<(ϕ+2) - fxOneAndAHalf<<1
					D := x1f<<1 + C // D ranges up to ±1<<(1*ϕ+2).
					D <<= ϕ         // D ranges up to ±1<<(2*ϕ+2).
					D -= x1fSquared // D ranges up to ±1<<(2*ϕ+3).
					D *= d          // D ranges up to ±1<<(3*ϕ+3).
					D /= twoOverS
					add(z, cells, left, buf, y, clamp(x1i-1, width), uint32(D))
				}
			}

			// In ideal math: buf[i] += uint32(d * am), where
			// am := ((x1f * x1f) >> 1) / oneOverS
			{
				D := x1fSquared // D ranges up to ±1<<(2*ϕ).
				D *= d          // D ranges up to ±1<<(3*ϕ).
				D /= twoOverS
				add(z, cells, left, buf, y, clamp(x1i, width), uint32(D))
			}
		}

		x = xNext
	}
}

func floatingFloor(x float32) int32 { return int32(math.Floor(float64(x))) }
func floatingCeil(x float32) int32  { return int32(math.Ceil(float64(x))) }

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
	yMax := floatingCeil(by)
	if yMax > int32(z.size.Y) {
		yMax = int32(z.size.Y)
	}
	width := int32(z.size.X)
	cells, left := z.f32, z.leftF

	for ; y < yMax; y++ {
		dy := min(float32(y+1), by) - max(float32(y), ay)

		// The "float32" in expressions like "float32(foo*bar)" here and below
		// look redundant, since foo and bar already have type float32, but are
		// explicit in order to disable the compiler's Fused Multiply Add (FMA)
		// instruction selection, which can improve performance but can result
		// in different rounding errors in floating point computations.
		xNext := x + float32(dy*dxdy)
		if y < 0 {
			x = xNext
			continue
		}
		buf := row(z, cells, y)
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
			add(z, cells, left, buf, y, clamp(x0i+0, width), d-float32(d*xmf))
			add(z, cells, left, buf, y, clamp(x0i+1, width), float32(d*xmf))
		} else {
			s := 1 / (x1 - x0)
			x0f := x0 - x0Floor
			oneMinusX0f := 1 - x0f
			a0 := float32(0.5 * s * oneMinusX0f * oneMinusX0f)
			x1f := x1 - x1Ceil + 1
			am := float32(0.5 * s * x1f * x1f)

			add(z, cells, left, buf, y, clamp(x0i, width), float32(d*a0))

			if x1i == x0i+2 {
				add(z, cells, left, buf, y, clamp(x0i+1, width), float32(d*(1-a0-am)))
			} else {
				a1 := float32(s * (1.5 - x0f))
				add(z, cells, left, buf, y, clamp(x0i+1, width), float32(d*(a1-a0)))
				dTimesS := float32(d * s)
				for xi := x0i + 2; xi < x1i-1; xi++ {
					add(z, cells, left, buf, y, clamp(xi, width), dTimesS)
				}
				a2 := a1 + float32(s*float32(x1i-x0i-3))
				add(z, cells, left, buf, y, clamp(x1i-1, width), float32(d*(1-a2-am)))
			}

			add(z, cells, left, buf, y, clamp(x1i, width), float32(d*am))
		}

		x = xNext
	}
}

const (
	// almost256 scales a floating point value in the range [0, 1] to a uint8
	// value in the range [0x00, 0xff]. See vector's raster_floating.go.
	almost256 = 255.99998

	// almost65536 scales a floating point value in the range [0, 1] to a
	// uint16 value in the range [0x0000, 0xffff].
	almost65536 = almost256 * 256
)

// faint is the mask alpha under which drawing a premultiplied color over
// any pixel leaves its bytes as they are; see TestFaintMaskChangesNothing.
const faint = 0x100

func fixedMask(acc int2ϕ) uint32 {
	a := acc
	if a < 0 {
		a = -a
	}
	a >>= 2*ϕ - 16
	if a > 0xffff {
		a = 0xffff
	}
	return uint32(a)
}

func floatingMask(acc float32) uint32 {
	a := acc
	if a < 0 {
		a = -a
	}
	if a > 1 {
		a = 1
	}
	return uint32(almost65536 * a)
}

// The even-odd rule folds the coverage, a sum of signed areas, modulo 2:
// covered twice over is not covered, one and a half times is half. The
// non-zero rule, vector's only, clamps it to 1.

func foldFixed(acc int2ϕ) int2ϕ {
	a := acc
	if a < 0 {
		a = -a
	}
	a &= 1<<(2*ϕ+1) - 1
	if a > 1<<(2*ϕ) {
		a = 1<<(2*ϕ+1) - a
	}
	return a
}

func foldFloating(acc float32) float32 {
	a := acc
	if a < 0 {
		a = -a
	}
	if a > 1 {
		a -= 2 * float32(int32(a/2)) // exact: a/2 and its whole part are
		if a > 1 {
			a = 2 - a
		}
	}
	return a
}

// fixedMaskByRule is the mask of acc by z's fill rule.
func (z *rasterizer) fixedMaskByRule(acc int2ϕ) uint32 {
	if z.evenOdd {
		acc = foldFixed(acc)
	}
	return fixedMask(acc)
}

func (z *rasterizer) floatingMaskByRule(acc float32) uint32 {
	if z.evenOdd {
		// Folded into [0, 1], it needs floatingMask's scaling alone: its
		// clamps cost in floatingAccumulate's loop.
		return uint32(almost65536 * foldFloating(acc))
	}
	return floatingMask(acc)
}

// accumulate turns the window's cells into its mask, alphas from 0 to
// 0xffff in u32, as vector's accumulateMask does its whole buffer. It
// reports false if the mask outside the window, all of vector's that it
// leaves out, would change a pixel.
func (z *rasterizer) accumulate() bool {
	switch {
	case !z.floating:
		return z.fixedAccumulate()
	case haveAccumulateSIMD:
		return z.floatingAccumulateSIMD()
	}
	return z.floatingAccumulate()
}

func (z *rasterizer) fixedAccumulate() bool {
	w, h := z.win.Dx(), z.win.Dy()
	split := z.win.Min.X > 0
	acc := int2ϕ(0)
	for r := 0; r < h; r++ {
		if split {
			acc += int2ϕ(z.left[r])
			if z.fixedMaskByRule(acc) >= faint {
				return false
			}
		}
		row := z.u32[r*w : r*w+w]
		if z.evenOdd {
			for i, v := range row {
				acc += int2ϕ(v)
				row[i] = fixedMask(foldFixed(acc))
			}
		} else {
			for i, v := range row {
				acc += int2ϕ(v)
				row[i] = fixedMask(acc)
			}
		}
		if z.win.Max.X < z.size.X && z.fixedMaskByRule(acc) >= faint {
			return false
		}
	}
	acc += int2ϕ(z.left[h])
	return z.win.Max.Y >= z.size.Y || z.fixedMaskByRule(acc) < faint
}

func (z *rasterizer) floatingAccumulate() bool {
	w, h := z.win.Dx(), z.win.Dy()
	split := z.win.Min.X > 0
	acc := float32(0)
	for r := 0; r < h; r++ {
		if split {
			acc += z.leftF[r]
			if z.floatingMaskByRule(acc) >= faint {
				return false
			}
		}
		row := z.f32[r*w : r*w+w]
		mask := z.u32[r*w : r*w+w]
		if z.evenOdd {
			for i, v := range row {
				acc += v
				mask[i] = uint32(almost65536 * foldFloating(acc)) // see floatingMaskByRule
			}
		} else {
			for i, v := range row {
				acc += v
				mask[i] = floatingMask(acc)
			}
		}
		if z.win.Max.X < z.size.X && z.floatingMaskByRule(acc) >= faint {
			return false
		}
	}
	acc += z.leftF[h]
	return z.win.Max.Y >= z.size.Y || z.floatingMaskByRule(acc) < faint
}

// floatingAccumulateSIMD is floatingAccumulate summing as vector's amd64
// assembly does: four cells at a time, aligned to the start of the mask,
// and one at a time after the last four. It walks the mask's buffer cell
// by cell where the window's rows, and column 0 beside them, are, and over
// the 0s in between, which leave the sum as it is.
func (z *rasterizer) floatingAccumulateSIMD() bool {
	h := z.win.Dy()
	n := z.size.X * z.size.Y
	n4 := n &^ 3
	c := simdCursor{z: z}
	for r := 0; r <= h; r++ {
		y := z.win.Min.Y + r
		if y >= z.size.Y {
			break
		}
		from, to := z.win.Min.X, z.win.Max.X
		if r == h {
			from, to = 0, 1 // the cell under the window's last row
		} else if z.win.Min.X > 0 && z.leftF[r] != 0 {
			from = 0
		}
		start, end := y*z.size.X+from, y*z.size.X+to
		if start < n4 {
			start &^= 3
		}
		if !c.skip(start) {
			return false
		}
		for c.f() < end {
			if c.f() < n4 {
				s0, s1, s2, s3 := c.cell(0), c.cell(1), c.cell(2), c.cell(3)
				x := [4]float32{s0, s1 + s0, (s2 + s1) + s0, (s3 + s2) + (s1 + s0)}
				for k := range x {
					x[k] += c.offset
				}
				if !c.set(x[:]...) {
					return false
				}
				c.offset = x[3]
			} else {
				c.offset += c.cell(0)
				if !c.set(c.offset) {
					return false
				}
			}
		}
	}
	return c.skip(n)
}

// simdCursor is floatingAccumulateSIMD's place in vector's buffer, at row
// y, column x, and the sum before it.
type simdCursor struct {
	z      *rasterizer
	y, x   int
	offset float32
}

// f is c's place in vector's buffer.
func (c *simdCursor) f() int { return c.y*c.z.size.X + c.x }

// skip moves c to f over 0s, reporting false if they would change a
// pixel.
func (c *simdCursor) skip(f int) bool {
	if f <= c.f() {
		return true
	}
	if c.z.floatingMaskByRule(c.offset) >= faint {
		return false
	}
	c.y, c.x = f/c.z.size.X, f%c.z.size.X
	return true
}

// cell is the cell k after c.
func (c *simdCursor) cell(k int) float32 {
	z := c.z
	x, y := c.x+k, c.y
	for x >= z.size.X {
		x, y = x-z.size.X, y+1
	}
	switch i, in := z.locate(y, x); in {
	case inWindow:
		return z.f32[i]
	case inLeft:
		return z.leftF[i]
	}
	return 0
}

// set sets the mask at c and the cells after it to the sums x, and moves
// c past them. It reports false if one outside the window would change a
// pixel.
func (c *simdCursor) set(x ...float32) bool {
	z := c.z
	for _, v := range x {
		a := z.floatingMaskByRule(v)
		if i, in := z.locate(c.y, c.x); in == inWindow {
			z.u32[i] = a
		} else if a >= faint {
			return false
		}
		if c.x++; c.x == z.size.X {
			c.x, c.y = 0, c.y+1
		}
	}
	return true
}
