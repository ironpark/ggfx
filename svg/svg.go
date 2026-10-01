// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

// Package svg draws SVG documents with the rasterizer text/v2 draws color
// glyphs with. It reads a subset of SVG 1.1: paths, shapes, groups, use,
// transforms, gradients, opacity and presentation attributes. What it leaves
// out, such as clipPath, mask, filter and image, is listed in
// internal/oksvg/README.md.
package svg

import (
	"fmt"
	"image"
	"io"
	"sync"

	"github.com/srwiley/rasterx"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/oksvg"
)

// Document is a parsed SVG document. Its methods can be called from any
// goroutine.
type Document struct {
	mu   sync.Mutex // drawing transforms the paths in place
	icon *oksvg.SvgIcon
}

// Parse reads an SVG document. It fails on what it cannot draw rather than
// drawing without it.
func Parse(r io.Reader) (*Document, error) {
	icon, err := oksvg.ReadIconStream(r, oksvg.StrictErrorMode)
	if err != nil {
		return nil, fmt.Errorf("svg: %w", err)
	}
	return &Document{icon: icon}, nil
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
	scanner := rasterx.NewScannerGV(b.Dx(), b.Dy(), dst, b)
	painter := rasterx.NewDasher(b.Dx(), b.Dy(), scanner)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.icon.Transform = rasterx.Matrix2D{
		A: geoM.Element(0, 0), C: geoM.Element(0, 1), E: geoM.Element(0, 2),
		B: geoM.Element(1, 0), D: geoM.Element(1, 1), F: geoM.Element(1, 2),
	}
	d.icon.Draw(painter, 1)
}
