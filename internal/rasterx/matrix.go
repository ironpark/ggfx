// Implements SVG style matrix transformations.
// https://developer.mozilla.org/en-US/docs/Web/SVG/Attribute/transform
// Copyright 2018 All rights reserved.

package rasterx

import (
	"golang.org/x/image/math/fixed"

	"github.com/ironpark/ggfx/internal/trig"
)

// Matrix2D represents an SVG style matrix
type Matrix2D struct {
	A, B, C, D, E, F float64
}

// Invert returns the inverse matrix, or the zero matrix if singular.
func (a Matrix2D) Invert() Matrix2D {
	det := float64(a.A*a.D) - float64(a.B*a.C)
	if det == 0 {
		return Matrix2D{}
	}
	return Matrix2D{
		A: a.D / det,
		B: -a.B / det,
		C: -a.C / det,
		D: a.A / det,
		E: (float64(a.C*a.F) - float64(a.D*a.E)) / det,
		F: (float64(a.B*a.E) - float64(a.A*a.F)) / det,
	}
}

// The float64(...) conversions of products here and in the rest of the
// package keep the compiler from fusing them with an addition, as it may
// on arm64 but not on amd64 or wasm, which would round differently.

// Mult returns a*b
func (a Matrix2D) Mult(b Matrix2D) Matrix2D {
	return Matrix2D{
		A: float64(a.A*b.A) + float64(a.C*b.B),
		B: float64(a.B*b.A) + float64(a.D*b.B),
		C: float64(a.A*b.C) + float64(a.C*b.D),
		D: float64(a.B*b.C) + float64(a.D*b.D),
		E: float64(a.A*b.E) + float64(a.C*b.F) + a.E,
		F: float64(a.B*b.E) + float64(a.D*b.F) + a.F}
}

// Identity is the identity matrix
var Identity = Matrix2D{1, 0, 0, 1, 0, 0}

// TFixed transforms a fixed.Point26_6 by the matrix
func (a Matrix2D) TFixed(x fixed.Point26_6) (y fixed.Point26_6) {
	y.X = fixed.Int26_6((float64(float64(x.X)*a.A) + float64(float64(x.Y)*a.C)) + float64(a.E*64))
	y.Y = fixed.Int26_6((float64(float64(x.X)*a.B) + float64(float64(x.Y)*a.D)) + float64(a.F*64))
	return
}

// Transform multiples the input vector by matrix m and outputs the results vector
// components.
func (a Matrix2D) Transform(x1, y1 float64) (x2, y2 float64) {
	x2 = float64(x1*a.A) + float64(y1*a.C) + a.E
	y2 = float64(x1*a.B) + float64(y1*a.D) + a.F
	return
}

// TransformVector is a modidifed version of Transform that ignores the
// translation components.
func (a Matrix2D) TransformVector(x1, y1 float64) (x2, y2 float64) {
	x2 = float64(x1*a.A) + float64(y1*a.C)
	y2 = float64(x1*a.B) + float64(y1*a.D)
	return
}

// Scale matrix in x and y dimensions
func (a Matrix2D) Scale(x, y float64) Matrix2D {
	return a.Mult(Matrix2D{
		A: x,
		B: 0,
		C: 0,
		D: y,
		E: 0,
		F: 0})
}

// SkewY skews the matrix in the Y dimension
func (a Matrix2D) SkewY(theta float64) Matrix2D {
	return a.Mult(Matrix2D{
		A: 1,
		B: trig.Tan(theta),
		C: 0,
		D: 1,
		E: 0,
		F: 0})
}

// SkewX skews the matrix in the X dimension
func (a Matrix2D) SkewX(theta float64) Matrix2D {
	return a.Mult(Matrix2D{
		A: 1,
		B: 0,
		C: trig.Tan(theta),
		D: 1,
		E: 0,
		F: 0})
}

// Translate translates the matrix to the x , y point
func (a Matrix2D) Translate(x, y float64) Matrix2D {
	return a.Mult(Matrix2D{
		A: 1,
		B: 0,
		C: 0,
		D: 1,
		E: x,
		F: y})
}

// Rotate rotate the matrix by theta
func (a Matrix2D) Rotate(theta float64) Matrix2D {
	s, c := trig.Sincos(theta)
	return a.Mult(Matrix2D{
		A: c,
		B: s,
		C: -s,
		D: c,
		E: 0,
		F: 0})
}

// MatrixAdder is an adder that applies matrix M to all points
type MatrixAdder struct {
	Adder
	M Matrix2D
}

// Reset sets the matrix M to identity
func (t *MatrixAdder) Reset() {
	t.M = Identity
}

// Start starts a new path
func (t *MatrixAdder) Start(a fixed.Point26_6) {
	t.Adder.Start(t.M.TFixed(a))
}

// Line adds a linear segment to the current curve.
func (t *MatrixAdder) Line(b fixed.Point26_6) {
	t.Adder.Line(t.M.TFixed(b))
}

// QuadBezier adds a quadratic segment to the current curve.
func (t *MatrixAdder) QuadBezier(b, c fixed.Point26_6) {
	t.Adder.QuadBezier(t.M.TFixed(b), t.M.TFixed(c))
}

// CubeBezier adds a cubic segment to the current curve.
func (t *MatrixAdder) CubeBezier(b, c, d fixed.Point26_6) {
	t.Adder.CubeBezier(t.M.TFixed(b), t.M.TFixed(c), t.M.TFixed(d))
}
