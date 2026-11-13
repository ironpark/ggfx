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

package ggfx_test

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/trig"
)

func TestGeoMInit(t *testing.T) {
	var m ggfx.GeoM
	for i := range ggfx.GeoMDim - 1 {
		for j := range ggfx.GeoMDim {
			got := m.Element(i, j)
			var want float64
			if i == j {
				want = 1
			}
			if want != got {
				t.Errorf("m.Element(%d, %d) = %f, want %f", i, j, got, want)
			}
		}
	}
}

func TestGeoMAssign(t *testing.T) {
	var m ggfx.GeoM
	m.SetElement(0, 0, 1)
	m2 := m
	m.SetElement(0, 0, 0)
	got := m2.Element(0, 0)
	want := 1.0
	if want != got {
		t.Errorf("m2.Element(%d, %d) = %f, want %f", 0, 0, got, want)
	}
}

func TestGeoMConcat(t *testing.T) {
	var matrix1 ggfx.GeoM
	matrix1.Scale(2, 2)
	var matrix2 ggfx.GeoM
	matrix2.Translate(1, 1)

	matrix3 := matrix1
	matrix3.Concat(matrix2)
	expected := [][]float64{
		{2, 0, 1},
		{0, 2, 1},
	}
	for i := range 2 {
		for j := range 3 {
			got := matrix3.Element(i, j)
			want := expected[i][j]
			if want != got {
				t.Errorf("matrix3.Element(%d, %d) = %f,"+
					" want %f",
					i, j, got, want)
			}
		}
	}

	matrix4 := matrix2
	matrix4.Concat(matrix1)
	expected = [][]float64{
		{2, 0, 2},
		{0, 2, 2},
	}
	for i := range 2 {
		for j := range 3 {
			got := matrix4.Element(i, j)
			want := expected[i][j]
			if want != got {
				t.Errorf("matrix4.Element(%d, %d) = %f, want %f",
					i, j, got, want)
			}
		}
	}
}

func TestGeoMConcatSelf(t *testing.T) {
	var m ggfx.GeoM
	m.SetElement(0, 0, 1)
	m.SetElement(0, 1, 2)
	m.SetElement(0, 2, 3)
	m.SetElement(1, 0, 4)
	m.SetElement(1, 1, 5)
	m.SetElement(1, 2, 6)
	m.Concat(m)
	expected := [][]float64{
		{9, 12, 18},
		{24, 33, 48},
	}
	for i := range 2 {
		for j := range 3 {
			got := m.Element(i, j)
			want := expected[i][j]
			if want != got {
				t.Errorf("m.Element(%d, %d) = %f, want %f",
					i, j, got, want)
			}
		}
	}
}

func geoMToString(g ggfx.GeoM) string {
	a := g.Element(0, 0)
	b := g.Element(0, 1)
	c := g.Element(1, 0)
	d := g.Element(1, 1)
	tx := g.Element(0, 2)
	ty := g.Element(1, 2)
	return fmt.Sprintf("{a: %f, b: %f, c: %f, d: %f, tx: %f, ty: %f}", a, b, c, d, tx, ty)
}

func TestGeoMApply(t *testing.T) {
	var trans ggfx.GeoM
	trans.Translate(1, 2)

	var scale ggfx.GeoM
	scale.Scale(1.5, 2.5)

	var cpx ggfx.GeoM
	cpx.Rotate(math.Pi)
	cpx.Scale(1.5, 2.5)
	cpx.Translate(-2, -3)

	cases := []struct {
		GeoM ggfx.GeoM
		InX  float64
		InY  float64
		OutX float64
		OutY float64
	}{
		{
			GeoM: ggfx.GeoM{},
			InX:  3.14159,
			InY:  2.81828,
			OutX: 3.14159,
			OutY: 2.81828,
		},
		{
			GeoM: trans,
			InX:  3.14159,
			InY:  2.81828,
			OutX: 4.14159,
			OutY: 4.81828,
		},
		{
			GeoM: scale,
			InX:  3.14159,
			InY:  2.81828,
			OutX: 4.71239,
			OutY: 7.04570,
		},
		{
			GeoM: cpx,
			InX:  3.14159,
			InY:  2.81828,
			OutX: -6.71239,
			OutY: -10.04570,
		},
	}

	const delta = 0.00001

	for _, c := range cases {
		rx, ry := c.GeoM.Apply(c.InX, c.InY)
		if math.Abs(rx-c.OutX) > delta || math.Abs(ry-c.OutY) > delta {
			t.Errorf("%s.Apply(%f, %f) = (%f, %f), want (%f, %f)", geoMToString(c.GeoM), c.InX, c.InY, rx, ry, c.OutX, c.OutY)
		}
	}
}

func TestGeoMIsInvert(t *testing.T) {
	var zero ggfx.GeoM
	zero.Scale(0, 0)

	var trans ggfx.GeoM
	trans.Translate(1, 2)

	var scale ggfx.GeoM
	scale.Scale(1.5, 2.5)

	var cpx ggfx.GeoM
	cpx.Rotate(math.Pi)
	cpx.Scale(1.5, 2.5)
	cpx.Translate(-2, -3)

	var cpx2 ggfx.GeoM
	cpx2.Scale(2, 3)
	cpx2.Rotate(0.234)
	cpx2.Translate(100, 100)

	var skew ggfx.GeoM
	skew.Skew(1, 1)

	cases := []struct {
		GeoM       ggfx.GeoM
		Invertible bool
	}{
		{
			GeoM:       zero,
			Invertible: false,
		},
		{
			GeoM:       ggfx.GeoM{},
			Invertible: true,
		},
		{
			GeoM:       trans,
			Invertible: true,
		},
		{
			GeoM:       scale,
			Invertible: true,
		},
		{
			GeoM:       cpx,
			Invertible: true,
		},
		{
			GeoM:       cpx2,
			Invertible: true,
		},
		{
			GeoM:       skew,
			Invertible: true,
		},
	}

	pts := []struct {
		X float64
		Y float64
	}{
		{
			X: 0,
			Y: 0,
		},
		{
			X: 1,
			Y: 1,
		},
		{
			X: 3.14159,
			Y: 2.81828,
		},
		{
			X: -1000,
			Y: 1000,
		},
	}

	const delta = 0.001

	for _, c := range cases {
		if c.GeoM.IsInvertible() != c.Invertible {
			t.Errorf("%s.IsInvertible(): got: %t, want: %t", geoMToString(c.GeoM), c.GeoM.IsInvertible(), c.Invertible)
		}
		if !c.GeoM.IsInvertible() {
			continue
		}
		invGeoM := c.GeoM
		invGeoM.Invert()
		for _, p := range pts {
			x, y := p.X, p.Y
			gotX, gotY := invGeoM.Apply(c.GeoM.Apply(x, y))
			if math.Abs(gotX-x) > delta || math.Abs(gotY-y) > delta {
				t.Errorf("%s.Apply(%s.Apply(%f, %f)): got: (%f, %f), want: (%f, %f)", geoMToString(invGeoM), geoMToString(c.GeoM), x, y, gotX, gotY, x, y)
			}
		}
	}
}

func newGeoM(a, b, c, d, tx, ty float64) ggfx.GeoM {
	var outp ggfx.GeoM
	outp.SetElement(0, 0, a)
	outp.SetElement(0, 1, b)
	outp.SetElement(0, 2, tx)
	outp.SetElement(1, 0, c)
	outp.SetElement(1, 1, d)
	outp.SetElement(1, 2, ty)
	return outp
}

func TestGeomSkew(t *testing.T) {
	testSkew := func(skewX, skewY float64, input, expected ggfx.GeoM) {
		input.Skew(skewX, skewY)
		for i := range 2 {
			for j := range 3 {
				got := input.Element(i, j)
				want := expected.Element(i, j)
				if want != got {
					t.Errorf("Skew(%f, %f): got %s, want: %s", skewX, skewY, input.String(), expected.String())
					return
				}
			}
		}
	}
	// skewX = 0.25
	expectedX := newGeoM(1, math.Tan(0.25), math.Tan(0), 1, 0, 0)
	testSkew(0.25, 0, ggfx.GeoM{}, expectedX)

	// skewY = 0.25
	expectedY := newGeoM(1, math.Tan(0), math.Tan(0.5), 1, 0, 0)
	testSkew(0, 0.5, ggfx.GeoM{}, expectedY)

	// skewX, skewY = 0.3, 0.8
	expectedXY := newGeoM(1, math.Tan(0.3), math.Tan(0.8), 1, 0, 0)
	testSkew(0.3, 0.8, ggfx.GeoM{}, expectedXY)

	// skewX, skewY = 0.4, -1.8 ; b, c = 2, 3
	expectedOffDiag := newGeoM(1+3*math.Tan(0.4), 2+math.Tan(0.4), 3+math.Tan(-1.8), 1+2*math.Tan(-1.8), 0, 0)
	inputOffDiag := newGeoM(1, 2, 3, 1, 0, 0)
	testSkew(0.4, -1.8, inputOffDiag, expectedOffDiag)

	// skewX, skewY = -1.5, 1.5 ; tx, ty = 5, 6
	expectedTrn := newGeoM(1, math.Tan(-1.5), math.Tan(1.5), 1, 5+math.Tan(-1.5)*6, 6+5*math.Tan(1.5))
	inputTrn := newGeoM(1, 0, 0, 1, 5, 6)
	testSkew(-1.5, 1.5, inputTrn, expectedTrn)
}

func TestGeoMEquals(t *testing.T) {
	tests := []struct {
		a    ggfx.GeoM
		b    ggfx.GeoM
		want bool
	}{
		{
			a:    ggfx.GeoM{},
			b:    ggfx.GeoM{},
			want: true,
		},
		{
			a:    newGeoM(3, 1, 4, 1, 5, 9),
			b:    newGeoM(3, 1, 4, 1, 5, 9),
			want: true,
		},
		{
			a:    newGeoM(3, 1, 4, 1, 5, 9),
			b:    newGeoM(3, 1, 4, 1, 5, 10),
			want: false,
		},
	}
	for _, test := range tests {
		got := (test.a == test.b)
		want := test.want
		if got != want {
			t.Errorf("%#v == %#v: got %t, want: %t", test.a, test.b, got, want)
		}
	}
}

// refM is a GeoM, kept as GeoM keeps one, composed with each product
// rounded on its own by math/big, which no compiler fuses with an addition.
type refM struct{ a1, b, c, d1, tx, ty float64 }

func mul(x, y float64) float64 {
	z, _ := new(big.Float).Mul(big.NewFloat(x), big.NewFloat(y)).Float64()
	return z
}

// newRefM is the matrix newGeoM makes, as GeoM keeps it.
func newRefM(a, b, c, d, tx, ty float64) refM { return refM{a - 1, b, c, d - 1, tx, ty} }

// elements are m's elements as GeoM.Element gives them.
func (m refM) elements() [2][3]float64 {
	return [2][3]float64{{m.a1 + 1, m.b, m.tx}, {m.c, m.d1 + 1, m.ty}}
}

func (m refM) apply(x, y float64) (float64, float64) {
	return mul(m.a1+1, x) + mul(m.b, y) + m.tx, mul(m.c, x) + mul(m.d1+1, y) + m.ty
}

func (m refM) concat(o refM) refM {
	return refM{
		a1: mul(o.a1+1, m.a1+1) + mul(o.b, m.c) - 1,
		b:  mul(o.a1+1, m.b) + mul(o.b, m.d1+1),
		tx: mul(o.a1+1, m.tx) + mul(o.b, m.ty) + o.tx,
		c:  mul(o.c, m.a1+1) + mul(o.d1+1, m.c),
		d1: mul(o.c, m.b) + mul(o.d1+1, m.d1+1) - 1,
		ty: mul(o.c, m.tx) + mul(o.d1+1, m.ty) + o.ty,
	}
}

func (m refM) scale(x, y float64) refM {
	return refM{mul(m.a1+1, x) - 1, mul(m.b, x), mul(m.c, y), mul(m.d1+1, y) - 1, mul(m.tx, x), mul(m.ty, y)}
}

func (m refM) rotate(theta float64) refM {
	sin, cos := trig.Sin(theta), trig.Cos(theta)
	return refM{
		a1: mul(cos, m.a1+1) - mul(sin, m.c) - 1,
		b:  mul(cos, m.b) - mul(sin, m.d1+1),
		tx: mul(cos, m.tx) - mul(sin, m.ty),
		c:  mul(sin, m.a1+1) + mul(cos, m.c),
		d1: mul(sin, m.b) + mul(cos, m.d1+1) - 1,
		ty: mul(sin, m.tx) + mul(cos, m.ty),
	}
}

func (m refM) skew(skewX, skewY float64) refM {
	sx, sy := trig.Tan(skewX), trig.Tan(skewY)
	return refM{
		a1: (m.a1 + 1) + mul(m.c, sx) - 1,
		b:  m.b + mul(m.d1+1, sx),
		c:  mul(m.a1+1, sy) + m.c,
		d1: mul(m.b, sy) + (m.d1 + 1) - 1,
		tx: m.tx + mul(m.ty, sx),
		ty: m.ty + mul(m.tx, sy),
	}
}

func (m refM) invert() refM {
	det := mul(m.a1+1, m.d1+1) - mul(m.b, m.c)
	return refM{
		a1: (m.d1+1)/det - 1,
		b:  -m.b / det,
		c:  -m.c / det,
		d1: (m.a1+1)/det - 1,
		tx: (mul(-(m.d1+1), m.tx) + mul(m.b, m.ty)) / det,
		ty: (mul(m.c, m.tx) + mul(-(m.a1+1), m.ty)) / det,
	}
}

// GeoM rounds each product on its own, as amd64 and wasm do, and not as
// the compiler would fuse it on arm64: a matrix composed with it places
// the same on every architecture.
func TestGeoMIsTheSameEverywhere(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	v := func() float64 { return (r.Float64() - 0.5) * math.Pow(10, float64(r.Intn(5)-2)) }
	check := func(op string, got ggfx.GeoM, want refM) {
		t.Helper()
		w := want.elements()
		for i := range 2 {
			for j := range 3 {
				if g := got.Element(i, j); math.Float64bits(g) != math.Float64bits(w[i][j]) {
					t.Fatalf("%s: element (%d, %d) is %v; want %v", op, i, j, g, w[i][j])
				}
			}
		}
	}
	for range 10000 {
		e := [12]float64{}
		for i := range e {
			e[i] = v()
		}
		m, o := newRefM(e[0], e[1], e[2], e[3], e[4], e[5]), newRefM(e[6], e[7], e[8], e[9], e[10], e[11])
		gm, gom := newGeoM(e[0], e[1], e[2], e[3], e[4], e[5]), newGeoM(e[6], e[7], e[8], e[9], e[10], e[11])
		x, y := v(), v()

		g := gm
		gx, gy := g.Apply(x, y)
		if wx, wy := m.apply(x, y); gx != wx || gy != wy {
			t.Fatalf("Apply(%v, %v): (%v, %v); want (%v, %v)", x, y, gx, gy, wx, wy)
		}

		g = gm
		g.Concat(gom)
		check("Concat", g, m.concat(o))

		g = gm
		g.Scale(x, y)
		check("Scale", g, m.scale(x, y))

		g = gm
		g.Rotate(x)
		check("Rotate", g, m.rotate(x))

		g = gm
		g.Skew(x, y)
		check("Skew", g, m.skew(x, y))

		g = gm
		g.Invert()
		check("Invert", g, m.invert())
	}
}

func BenchmarkGeoM(b *testing.B) {
	var m ggfx.GeoM
	for range b.N {
		m = ggfx.GeoM{}
		m.Translate(10, 20)
		m.Scale(2, 3)
		m.Rotate(math.Pi / 2)
	}
}
