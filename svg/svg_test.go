// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/svgx"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

// The engine is svgx's, and tested there. These tests are of what this
// package adds: the GeoM, the nil options that mean the static rendering, and
// the errors.

const animated = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="2 3 8 8">
<rect id="r" x="2" y="3" width="8" height="8" fill="#f00"><set attributeName="fill" to="#00f" begin="0s"/></rect>
</svg>`

var (
	red  = color.RGBA{R: 0xff, A: 0xff}
	blue = color.RGBA{B: 0xff, A: 0xff}
)

func parse(t *testing.T, src string) *svg.Document {
	t.Helper()
	d, err := svg.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func fit() ggfx.GeoM {
	var g ggfx.GeoM
	g.Translate(-2, -3)
	return g
}

func TestParseError(t *testing.T) {
	_, err := svg.Parse(strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg"><rect fill="notacolor"/></svg>`))
	if err == nil || !strings.HasPrefix(err.Error(), "svg: ") {
		t.Errorf("Parse error = %v, want one that begins with svg:", err)
	}

	var deep strings.Builder
	deep.WriteString(`<svg xmlns="http://www.w3.org/2000/svg">`)
	for range 300 {
		deep.WriteString(`<g>`)
	}
	_, err = svg.Parse(strings.NewReader(deep.String()))
	if !errors.Is(err, svg.ErrNestingDepth) {
		t.Errorf("Parse error = %v, want ErrNestingDepth", err)
	}
}

func TestViewBox(t *testing.T) {
	d := parse(t, animated)
	if x, y, w, h := d.ViewBox(); x != 2 || y != 3 || w != 8 || h != 8 {
		t.Errorf("ViewBox = %v %v %v %v, want 2 3 8 8", x, y, w, h)
	}
}

// Nil options are the static rendering; options of any kind are the document
// at their time.
func TestNilOptionsAreStatic(t *testing.T) {
	d := parse(t, animated)
	r := d.NewRenderer()
	defer r.Release()

	for _, tc := range []struct {
		name string
		draw func(dst *image.RGBA)
		want color.RGBA
	}{
		{"Draw", func(dst *image.RGBA) { d.Draw(dst, fit()) }, red},
		{"DrawErr", func(dst *image.RGBA) { check(t, d.DrawErr(dst, fit())) }, red},
		{"DrawWith(nil)", func(dst *image.RGBA) { d.DrawWith(dst, fit(), nil) }, red},
		{"DrawWithErr(nil)", func(dst *image.RGBA) { check(t, d.DrawWithErr(dst, fit(), nil)) }, red},
		{"DrawWith(zero)", func(dst *image.RGBA) { d.DrawWith(dst, fit(), &svg.DrawOptions{}) }, blue},
		{"DrawWith(Static)", func(dst *image.RGBA) { d.DrawWith(dst, fit(), &svg.DrawOptions{Static: true}) }, red},
		{"DrawWithErr(time)", func(dst *image.RGBA) {
			check(t, d.DrawWithErr(dst, fit(), &svg.DrawOptions{Time: time.Second}))
		}, blue},
		{"Renderer.Draw(nil)", func(dst *image.RGBA) { r.Draw(dst, fit(), nil) }, red},
		{"Renderer.DrawErr(zero)", func(dst *image.RGBA) { check(t, r.DrawErr(dst, fit(), &svg.DrawOptions{})) }, blue},
	} {
		dst := image.NewRGBA(image.Rect(0, 0, 8, 8))
		tc.draw(dst)
		if got := dst.RGBAAt(4, 4); got != tc.want {
			t.Errorf("%s: pixel = %v, want %v", tc.name, got, tc.want)
		}
	}

	if _, ok := d.Bounds("r", nil); !ok {
		t.Error("Bounds(nil) found nothing")
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Error(err)
	}
}

// A GeoM draws as the svgx.Matrix of the same elements does.
func TestGeoM(t *testing.T) {
	const src = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16">
<path d="M2 2 L14 4 L8 14 Z" fill="#083" stroke="#208" stroke-width="1.5"/></svg>`
	var g ggfx.GeoM
	g.Scale(1.5, 0.75)
	g.Rotate(0.3)
	g.Skew(0.1, 0)
	g.Translate(5, 2)

	m := svg.Matrix(g)
	for _, p := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {3, 7}} {
		gx, gy := g.Apply(p[0], p[1])
		mx, my := m.Apply(p[0], p[1])
		if gx != mx || gy != my {
			t.Errorf("Apply(%v): GeoM (%v, %v), Matrix (%v, %v)", p, gx, gy, mx, my)
		}
	}

	got := image.NewRGBA(image.Rect(0, 0, 32, 32))
	parse(t, src).Draw(got, g)

	doc, err := svgx.Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	want := image.NewRGBA(image.Rect(0, 0, 32, 32))
	if err := doc.Draw(want, m, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Error("the document drew differently through this package than through svgx")
	}
	if bytes.Equal(got.Pix, make([]byte, len(got.Pix))) {
		t.Error("nothing was drawn")
	}
}

func TestOverrides(t *testing.T) {
	d := parse(t, animated)
	green := color.RGBA{G: 0xff, A: 0xff}
	var shift ggfx.GeoM
	shift.Translate(100, 0)

	dst := image.NewRGBA(image.Rect(0, 0, 8, 8))
	d.DrawWith(dst, fit(), &svg.DrawOptions{Overrides: svg.Overrides{
		"r": svg.NodeOverride{}.Fill(svg.ColorPaint(green)),
	}})
	if got := dst.RGBAAt(4, 4); got != green {
		t.Errorf("Fill override: pixel = %v, want %v", got, green)
	}

	dst = image.NewRGBA(image.Rect(0, 0, 8, 8))
	d.DrawWith(dst, fit(), &svg.DrawOptions{Overrides: svg.Overrides{
		"r": svg.NodeOverride{}.Matrix(svg.Matrix(shift)),
	}})
	if got := dst.RGBAAt(4, 4); got != (color.RGBA{}) {
		t.Errorf("Matrix override: pixel = %v, want the shape moved away", got)
	}
}

func TestBinary(t *testing.T) {
	d := parse(t, animated)
	data, err := d.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var back svg.Document
	if err := back.UnmarshalBinary(data); err != nil {
		t.Fatal(err)
	}
	want := image.NewRGBA(image.Rect(0, 0, 8, 8))
	got := image.NewRGBA(image.Rect(0, 0, 8, 8))
	d.DrawWith(want, fit(), &svg.DrawOptions{})
	back.DrawWith(got, fit(), &svg.DrawOptions{})
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Error("the document read back draws differently")
	}
	if n, ok := back.Node("r"); !ok || n.Tag() != "rect" {
		t.Error("the document read back has lost its node")
	}

	err = back.UnmarshalBinary([]byte{0xff})
	if err == nil || !strings.HasPrefix(err.Error(), "svg: ") {
		t.Errorf("UnmarshalBinary error = %v, want one that begins with svg:", err)
	}
	if !back.Animated() {
		t.Error("a failed UnmarshalBinary changed the document")
	}
}

// The zero and nil Documents draw nothing and answer nothing.
func TestZeroDocument(t *testing.T) {
	var zero svg.Document
	for _, d := range []*svg.Document{&zero, nil} {
		dst := image.NewRGBA(image.Rect(0, 0, 4, 4))
		d.Draw(dst, ggfx.GeoM{})
		check(t, d.DrawWithErr(dst, ggfx.GeoM{}, &svg.DrawOptions{}))
		check(t, d.DrawErr(nil, ggfx.GeoM{}))
		r := d.NewRenderer()
		check(t, r.DrawErr(dst, ggfx.GeoM{}, nil))
		r.Release()
		if !bytes.Equal(dst.Pix, make([]byte, len(dst.Pix))) {
			t.Error("an empty document drew something")
		}
		if x, y, w, h := d.ViewBox(); x != 0 || y != 0 || w != 0 || h != 0 {
			t.Error("an empty document has a view box")
		}
		if d.Animated() || d.UsesCurrentColor() || len(d.NodeIDs()) != 0 {
			t.Error("an empty document answers as if it held something")
		}
		if _, ok := d.Node("r"); ok {
			t.Error("an empty document has a node")
		}
		if _, ok := d.NextChange(0); ok {
			t.Error("an empty document has a next change")
		}
	}
}
