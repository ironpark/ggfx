// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

// Package svg draws SVG documents with the oksvg fork in internal/oksvg, the
// rasterizer that color glyphs were drawn with while ggfx carried text. It
// reads a subset of SVG 1.1: paths, shapes, groups, use, transforms,
// gradients, opacity, presentation attributes, clip paths, masks and
// markers. What it leaves out, such as filter and image, is listed in
// internal/oksvg/README.md.
package svg

import (
	"fmt"
	"image"
	"io"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/oksvg"
	"github.com/ironpark/ggfx/internal/pool"
	"github.com/ironpark/ggfx/internal/rasterx"
)

// Document is a parsed SVG document. Its methods can be called from any
// goroutine.
type Document struct {
	icon *oksvg.SvgIcon
}

// Parse reads an SVG document. It fails on what it cannot draw rather than
// drawing without it.
func Parse(r io.Reader) (*Document, error) {
	icon, err := oksvg.ReadIconStream(r)
	if err != nil {
		return nil, fmt.Errorf("svg: %w", err)
	}
	return &Document{icon: icon}, nil
}

// ErrNoBinaryForm is why MarshalBinary has nothing to return for a document.
var ErrNoBinaryForm = oksvg.ErrNoBinaryForm

// MarshalBinary returns what parsing arrived at, for a document known ahead
// of time, an icon of a library say, to be kept without its source and read
// back by UnmarshalBinary without parsing it. It holds a document of shapes
// painted in plain colors; one with gradients, clip paths, masks or markers
// has no binary form, and the error is ErrNoBinaryForm.
//
// The form is versioned: a later ggfx reads what an earlier one wrote.
func (d *Document) MarshalBinary() ([]byte, error) {
	b, err := d.icon.AppendBinary(nil)
	if err != nil {
		return nil, fmt.Errorf("svg: %w", err)
	}
	return b, nil
}

// UnmarshalBinary makes d the document MarshalBinary wrote as data, which
// draws as the one it was written from does.
func (d *Document) UnmarshalBinary(data []byte) error {
	icon, err := oksvg.ReadBinary(data)
	if err != nil {
		return fmt.Errorf("svg: %w", err)
	}
	d.icon = icon
	return nil
}

// ViewBox returns the rectangle of the document's user space that its
// viewBox attribute names as the picture.
func (d *Document) ViewBox() (x, y, width, height float64) {
	b := d.icon.ViewBox
	return b.X, b.Y, b.W, b.H
}

// Draw draws the document over dst, with geoM mapping its user space to
// dst's pixels.
func (d *Document) Draw(dst *image.RGBA, geoM ggfx.GeoM) {
	b := dst.Bounds()
	p := painters.Get()
	p.Scanner.Dest, p.Scanner.Targ = dst, b
	p.Dasher.SetBounds(b.Dx(), b.Dy())
	d.icon.DrawTransformed(p, 1, rasterx.Matrix2D{
		A: geoM.Element(0, 0), C: geoM.Element(0, 1), E: geoM.Element(0, 2),
		B: geoM.Element(1, 0), D: geoM.Element(1, 1), F: geoM.Element(1, 2),
	})
	// Let go of dst, and of the last paint, which may hold a mask, before
	// the painter is kept.
	p.Release()
	painters.Put(p)
}

// painters are rasterizers and their buffers, kept between drawings,
// which change nothing in a document and so need no lock.
var painters = pool.Pool[*oksvg.Painter]{New: func() *oksvg.Painter {
	return oksvg.NewPainter(0, 0, nil, image.Rectangle{})
}}
