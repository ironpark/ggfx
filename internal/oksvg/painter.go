// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"
	"image/color"

	"github.com/ironpark/ggfx/internal/rasterx"
)

type gradCacheKey struct {
	grad *rasterx.Gradient
	col  color.NRGBA
}

// Painter is what an icon is drawn with: a Dasher over a ScannerGV, and
// what drawing keeps between paths and drawings so as not to make it
// again. It draws one icon at a time.
type Painter struct {
	Scanner *rasterx.ScannerGV
	Dasher  *rasterx.Dasher

	// currentColor is the resolved CSS color for paint{current: true} and
	// currentColor gradient stops during the active draw call. nil means black.
	currentColor color.Color
	// gradCache caches resolved copies of gradients whose stops reference currentColor.
	gradCache map[gradCacheKey]*rasterx.Gradient

	// adder places a path. One made for each, given to Path.AddTo as an
	// Adder, would be moved to the heap.
	adder rasterx.MatrixAdder
	// outline places a stroke's outline, and dash holds its dashes, when
	// the stroke is made in a space other than the picture's; see
	// drawThrough.
	outline rasterx.MatrixAdder
	dash    []float64
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
	p.currentColor = nil
	if len(p.gradCache) > 256 {
		p.gradCache = nil
	} else {
		clear(p.gradCache)
	}
	const maxRetainedDrawnBytes = 4 * 1024 * 1024
	if p.drawn != nil && cap(p.drawn.Pix) > maxRetainedDrawnBytes {
		p.drawn = nil
	}
	if cap(p.dash) > 1024 {
		p.dash = nil
	}
	if p.clips != nil {
		p.clips.Release()
	}
}

// resolveGrad returns g with any currentColor stops resolved to col.
// If g has no currentColor stops, g is returned unchanged.
func (p *Painter) resolveGrad(g *rasterx.Gradient, col color.Color) *rasterx.Gradient {
	hasCurrent := false
	for _, s := range g.Stops {
		if s.StopColor == currentColorMark {
			hasCurrent = true
			break
		}
	}
	if !hasCurrent {
		return g
	}
	nrgba := toNRGBA(col)
	key := gradCacheKey{grad: g, col: nrgba}
	if p != nil && p.gradCache != nil {
		if cached, ok := p.gradCache[key]; ok {
			return cached
		}
	}
	cp := *g
	stops := make([]rasterx.GradStop, len(g.Stops))
	copy(stops, g.Stops)
	for i := range stops {
		if stops[i].StopColor == currentColorMark {
			stops[i].StopColor = nrgba
		}
	}
	cp.Stops = stops
	res := &cp
	if p != nil {
		if p.gradCache == nil {
			p.gradCache = make(map[gradCacheKey]*rasterx.Gradient, 8)
		} else if len(p.gradCache) >= 256 {
			clear(p.gradCache)
		}
		p.gradCache[key] = res
	}
	return res
}

func toNRGBA(c color.Color) color.NRGBA {
	if c == nil {
		return color.NRGBA{0, 0, 0, 0xff}
	}
	if n, ok := c.(color.NRGBA); ok {
		return n
	}
	return color.NRGBAModel.Convert(c).(color.NRGBA)
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
