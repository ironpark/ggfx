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

type gradTableKey struct {
	grad    *rasterx.Gradient
	opacity float64
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
	// gradTables caches decoded stop tables per (gradient, opacity).
	gradTables map[gradTableKey]*rasterx.GradTable
	// gradSpan is reused across gradient shapes to avoid per-shape allocations.
	gradSpan rasterx.GradSpan

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
	// drawn is what luminance masks draw into, kept for its pixels.
	drawn *image.RGBA
	// alphaPool holds reusable alpha mask buffers for clipPath/mask scopes
	// within a single draw call, avoiding per-draw allocations.
	alphaPool []image.Alpha
	alphaUsed int
	// clipMasks caches resolved clipResults for legacy drawLegacyTransformed.
	clipMasks map[*clipUse]clipResult
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
	p.Scanner.DestAlpha = nil
	p.Scanner.SetAlphaMask(nil)
	p.Scanner.SetClip(image.Rectangle{})
	p.Scanner.SetCoverageScale(1)
	p.Scanner.SetPaint(rasterx.Paint{})
	p.Dasher.EndDraw()
	p.currentColor = nil
	p.alphaUsed = 0
	if len(p.clipMasks) > 256 {
		p.clipMasks = nil
	} else {
		clear(p.clipMasks)
	}
	if len(p.gradCache) > 256 {
		p.gradCache = nil
	} else {
		clear(p.gradCache)
	}
	if len(p.gradTables) > 256 {
		p.gradTables = nil
	}
	const maxRetainedDrawnBytes = 4 * 1024 * 1024
	if p.drawn != nil && cap(p.drawn.Pix) > maxRetainedDrawnBytes {
		p.drawn = nil
	}
	var retainedAlpha int
	for i := range p.alphaPool {
		c := cap(p.alphaPool[i].Pix)
		if retainedAlpha+c > maxRetainedDrawnBytes {
			p.alphaPool[i] = image.Alpha{}
		} else {
			retainedAlpha += c
		}
	}
	if cap(p.dash) > 1024 {
		p.dash = nil
	}
	if p.clips != nil {
		p.clips.Release()
	}
}

// gradTable returns a cached decoded stop table for g at opacity.
func (p *Painter) gradTable(g *rasterx.Gradient, opacity float64) *rasterx.GradTable {
	if len(g.Stops) < 2 {
		return nil
	}
	key := gradTableKey{grad: g, opacity: opacity}
	if p != nil && p.gradTables != nil {
		if t, ok := p.gradTables[key]; ok {
			return t
		}
	}
	t := g.BuildGradTable(opacity)
	if p != nil {
		if p.gradTables == nil {
			p.gradTables = make(map[gradTableKey]*rasterx.GradTable, 8)
		} else if len(p.gradTables) >= 256 {
			clear(p.gradTables)
		}
		p.gradTables[key] = t
	}
	return t
}

// allocAlpha returns a reusable alpha buffer sized to r from p's pool.
// If zero is true, its pixels are cleared before returning.
func (p *Painter) allocAlpha(r image.Rectangle, zero bool) *image.Alpha {
	n := r.Dx() * r.Dy()
	idx := p.alphaUsed
	p.alphaUsed++
	if idx >= len(p.alphaPool) {
		p.alphaPool = append(p.alphaPool, image.Alpha{})
	}
	m := &p.alphaPool[idx]
	if cap(m.Pix) < n {
		m.Pix = make([]uint8, n)
	} else {
		m.Pix = m.Pix[:n]
		if zero {
			clear(m.Pix)
		}
	}
	m.Stride = r.Dx()
	m.Rect = r
	return m
}

func (p *Painter) resetAlphaPool() {
	p.alphaUsed = 0
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
