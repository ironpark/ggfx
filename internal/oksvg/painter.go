// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// Painter is what an icon is drawn with: a Dasher over a ScannerGV, and
// what drawing keeps between paths and drawings so as not to make it
// again. It draws one icon at a time.
type Painter struct {
	Scanner *rasterx.ScannerGV
	Dasher  *rasterx.Dasher

	// adder places a path. One made for each, given to Path.AddTo as an
	// Adder, would be moved to the heap.
	adder rasterx.MatrixAdder
	// clips draws clips and masks; see clip.go. It is made when one is
	// first drawn.
	clips *Painter
	// drawn is what clips draws into, kept for its pixels.
	drawn *image.RGBA
}

// NewPainter returns a Painter of a width by height mask, placed at targ
// in dest; see rasterx.ScannerGV.
func NewPainter(width, height int, dest *image.RGBA, targ image.Rectangle) *Painter {
	s := rasterx.NewScannerGV(width, height, dest, targ)
	return &Painter{Scanner: s, Dasher: rasterx.NewDasher(width, height, s)}
}

// Release lets go of the target and of the last paints, which may hold a
// mask, so that p can be kept for another drawing.
func (p *Painter) Release() {
	p.Scanner.Dest = nil
	p.Scanner.SetPaint(rasterx.Paint{})
	if p.clips != nil {
		p.clips.Scanner.SetPaint(rasterx.Paint{})
	}
}

// scratch is a clear image of r, over drawn's pixels if they are enough.
func (p *Painter) scratch(r image.Rectangle) *image.RGBA {
	n := 4 * r.Dx() * r.Dy()
	if p.drawn == nil || cap(p.drawn.Pix) < n {
		p.drawn = image.NewRGBA(r)
		return p.drawn
	}
	m := p.drawn
	m.Pix, m.Stride, m.Rect = m.Pix[:n], 4*r.Dx(), r
	clear(m.Pix)
	return m
}
