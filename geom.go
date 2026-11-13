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

	"github.com/ironpark/ggfx/internal/trig"
)

// GeoMDim is a dimension of a GeoM.
const GeoMDim = 3

// A GeoM represents a matrix to transform geometry when rendering an image.
//
// The initial value is identity.
//
// Its methods round as amd64 and wasm do on every architecture: each
// product that is added is converted to float64, which keeps the compiler
// from fusing it with the addition as it would on arm64, and Rotate and
// Skew take their sines, cosines and tangents from internal/trig, not the
// math package, whose own the compiler fuses.
type GeoM struct {
	a_1 float64 // The actual 'a' value minus 1
	b   float64
	c   float64
	d_1 float64 // The actual 'd' value minus 1
	tx  float64
	ty  float64
}

// String returns a string representation of GeoM.
func (g *GeoM) String() string {
	return fmt.Sprintf("[[%f, %f, %f], [%f, %f, %f]]", g.a_1+1, g.b, g.tx, g.c, g.d_1+1, g.ty)
}

// Reset resets the GeoM as identity.
func (g *GeoM) Reset() {
	g.a_1 = 0
	g.b = 0
	g.c = 0
	g.d_1 = 0
	g.tx = 0
	g.ty = 0
}

// Apply pre-multiplies a vector (x, y, 1) by the matrix.
// In other words, Apply calculates GeoM * (x, y, 1)^T.
// The return value is x and y values of the result vector.
func (g *GeoM) Apply(x, y float64) (float64, float64) {
	return float64((g.a_1+1)*x) + float64(g.b*y) + g.tx, float64(g.c*x) + float64((g.d_1+1)*y) + g.ty
}

func (g *GeoM) elements32() (a, b, c, d, tx, ty float32) {
	return float32(g.a_1) + 1, float32(g.b), float32(g.c), float32(g.d_1) + 1, float32(g.tx), float32(g.ty)
}

// Element returns a value of a matrix at (i, j).
func (g *GeoM) Element(i, j int) float64 {
	switch {
	case i == 0 && j == 0:
		return g.a_1 + 1
	case i == 0 && j == 1:
		return g.b
	case i == 0 && j == 2:
		return g.tx
	case i == 1 && j == 0:
		return g.c
	case i == 1 && j == 1:
		return g.d_1 + 1
	case i == 1 && j == 2:
		return g.ty
	default:
		panic("ggfx: i or j is out of index")
	}
}

// Concat multiplies a geometry matrix with the other geometry matrix.
// This is same as multiplying the matrix other and the matrix g in this order.
func (g *GeoM) Concat(other GeoM) {
	a := float64((other.a_1+1)*(g.a_1+1)) + float64(other.b*g.c)
	b := float64((other.a_1+1)*g.b) + float64(other.b*(g.d_1+1))
	tx := float64((other.a_1+1)*g.tx) + float64(other.b*g.ty) + other.tx
	c := float64(other.c*(g.a_1+1)) + float64((other.d_1+1)*g.c)
	d := float64(other.c*g.b) + float64((other.d_1+1)*(g.d_1+1))
	ty := float64(other.c*g.tx) + float64((other.d_1+1)*g.ty) + other.ty

	g.a_1 = a - 1
	g.b = b
	g.c = c
	g.d_1 = d - 1
	g.tx = tx
	g.ty = ty
}

// Scale scales the matrix by (x, y).
func (g *GeoM) Scale(x, y float64) {
	// Converted, so that a-1 and d-1 are not fused; assigned in place, so
	// that Scale is still inlined.
	g.a_1 = float64((g.a_1+1)*x) - 1
	g.b *= x
	g.tx *= x
	g.c *= y
	g.d_1 = float64((g.d_1+1)*y) - 1
	g.ty *= y
}

// Translate translates the matrix by (tx, ty).
func (g *GeoM) Translate(tx, ty float64) {
	g.tx += tx
	g.ty += ty
}

// Rotate rotates the matrix clockwise by theta.
// The unit is radian.
func (g *GeoM) Rotate(theta float64) {
	if theta == 0 {
		return
	}

	sin, cos := trig.Sincos(theta)

	a := float64(cos*(g.a_1+1)) - float64(sin*g.c)
	b := float64(cos*g.b) - float64(sin*(g.d_1+1))
	tx := float64(cos*g.tx) - float64(sin*g.ty)
	c := float64(sin*(g.a_1+1)) + float64(cos*g.c)
	d := float64(sin*g.b) + float64(cos*(g.d_1+1))
	ty := float64(sin*g.tx) + float64(cos*g.ty)

	g.a_1 = a - 1
	g.b = b
	g.c = c
	g.d_1 = d - 1
	g.tx = tx
	g.ty = ty
}

// Skew skews the matrix by (skewX, skewY). The unit is radian.
func (g *GeoM) Skew(skewX, skewY float64) {
	sx := trig.Tan(skewX)
	sy := trig.Tan(skewY)

	a := (g.a_1 + 1) + float64(g.c*sx)
	b := g.b + float64((g.d_1+1)*sx)
	c := float64((g.a_1+1)*sy) + g.c
	d := float64(g.b*sy) + (g.d_1 + 1)
	tx := g.tx + float64(g.ty*sx)
	ty := g.ty + float64(g.tx*sy)

	g.a_1 = a - 1
	g.b = b
	g.c = c
	g.d_1 = d - 1
	g.tx = tx
	g.ty = ty
}

func (g *GeoM) det2x2() float64 {
	return float64((g.a_1+1)*(g.d_1+1)) - float64(g.b*g.c)
}

// IsInvertible returns a boolean value indicating
// whether the matrix g is invertible or not.
func (g *GeoM) IsInvertible() bool {
	return g.det2x2() != 0
}

// Invert inverts the matrix.
// If g is not invertible, Invert panics.
func (g *GeoM) Invert() {
	det := g.det2x2()
	if det == 0 {
		panic("ggfx: g is not invertible")
	}

	a := (g.d_1 + 1) / det
	b := -g.b / det
	c := -g.c / det
	d := (g.a_1 + 1) / det
	tx := (float64(-(g.d_1+1)*g.tx) + float64(g.b*g.ty)) / det
	ty := (float64(g.c*g.tx) + float64(-(g.a_1+1)*g.ty)) / det

	g.a_1 = a - 1
	g.b = b
	g.c = c
	g.d_1 = d - 1
	g.tx = tx
	g.ty = ty
}

// SetElement sets an element at (i, j).
func (g *GeoM) SetElement(i, j int, element float64) {
	e := element
	switch {
	case i == 0 && j == 0:
		g.a_1 = e - 1
	case i == 0 && j == 1:
		g.b = e
	case i == 0 && j == 2:
		g.tx = e
	case i == 1 && j == 0:
		g.c = e
	case i == 1 && j == 1:
		g.d_1 = e - 1
	case i == 1 && j == 2:
		g.ty = e
	default:
		panic("ggfx: i or j is out of index")
	}
}
