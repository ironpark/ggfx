// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"
	"math"
	"slices"
	"sync"

	"github.com/ironpark/ggfx/internal/rasterx"
	"github.com/ironpark/ggfx/internal/svganim"
)

var framePool = sync.Pool{
	New: func() any {
		return &frame{}
	},
}

func renderFrame(p *Painter, fr *frame, masksBuf *[]*image.Alpha, opacity float64, t rasterx.Matrix2D) {
	nScopes := len(fr.scopes)
	var masks []*image.Alpha
	if nScopes > 0 {
		if masksBuf != nil {
			if cap(*masksBuf) < nScopes {
				*masksBuf = make([]*image.Alpha, nScopes)
			} else {
				*masksBuf = (*masksBuf)[:nScopes]
				clear(*masksBuf)
			}
			masks = *masksBuf
		} else {
			masks = make([]*image.Alpha, nScopes)
		}
	}

	prevCC := p.currentColor
	defer func() {
		p.currentColor = prevCC
		if masksBuf != nil && len(*masksBuf) > 0 {
			clear(*masksBuf)
		}
	}()

	for i := range fr.items {
		it := &fr.items[i]
		if !it.visible {
			continue
		}
		var mask *image.Alpha
		if it.scope >= 0 {
			mask = scopeMask(p, fr, masks, it.scope, t)
		}
		p.currentColor = it.style.color
		svgp := SvgPath{PathStyle: it.style, Path: it.path}
		svgp.drawThroughTrim(p, opacity, t, mask, it.trim, it.lens)
	}
}

// Renderer is a reusable, stateful renderer bound to a single SvgIcon.
// It is NOT goroutine-safe; create one per consumer and call Release when done.
type Renderer struct {
	icon    *SvgIcon
	painter *Painter
	fr      frame
	masks   []*image.Alpha
}

// NewRenderer creates a reusable Renderer for s.
func (s *SvgIcon) NewRenderer() *Renderer {
	return &Renderer{
		icon:    s,
		painter: NewPainter(0, 0, nil, image.Rectangle{}),
	}
}

// Draw draws the icon onto dst with geoM and opts using the renderer's pooled state.
func (r *Renderer) Draw(dst *image.RGBA, geoM rasterx.Matrix2D, opts *EvalOptions) {
	if r == nil || r.icon == nil || dst == nil {
		return
	}
	b := dst.Bounds()
	if b.Empty() {
		return
	}
	if r.painter == nil {
		r.painter = NewPainter(0, 0, nil, image.Rectangle{})
	}
	p := r.painter
	p.Scanner.Dest, p.Scanner.Targ = dst, b
	p.Dasher.SetBounds(b.Dx(), b.Dy())
	defer func() {
		// Drop reference to caller's dst image while keeping caches warm.
		p.Scanner.Dest = nil
		p.Scanner.SetPaint(rasterx.Paint{})
		p.currentColor = nil
		if p.clips != nil {
			p.clips.Scanner.Dest = nil
			p.clips.Scanner.SetPaint(rasterx.Paint{})
			p.clips.currentColor = nil
		}
		r.fr.reset()
	}()

	r.icon.drawWithPainter(p, &r.fr, &r.masks, 1, geoM, opts)
}

// Release drops cached painters and scratch buffers held by r.
func (r *Renderer) Release() {
	if r == nil {
		return
	}
	if r.painter != nil {
		r.painter.Release()
		r.painter.gradCache = nil
		r.painter = nil
	}
	r.fr = frame{}
	r.masks = nil
}

// DrawWith draws s using p under transform t with opts. Safe for concurrent calls
// on the same SvgIcon when each caller passes its own Painter.
func (s *SvgIcon) DrawWith(p *Painter, t rasterx.Matrix2D, opts *EvalOptions) {
	s.drawWithPainter(p, nil, nil, 1, t, opts)
}

func (s *SvgIcon) drawWithPainter(p *Painter, fr *frame, masksBuf *[]*image.Alpha, opacity float64, t rasterx.Matrix2D, opts *EvalOptions) {
	if s.canUseFastPath(opts) {
		prevCC := p.currentColor
		if opts != nil {
			p.currentColor = opts.CurrentColor
		} else {
			p.currentColor = nil
		}
		s.drawLegacyTransformed(p, opacity, t)
		p.currentColor = prevCC
		return
	}
	if s.scene == nil {
		// Unmarshaled binary icon has no scene; overrides are no-ops, currentColor works on fast path.
		prevCC := p.currentColor
		if opts != nil {
			p.currentColor = opts.CurrentColor
		} else {
			p.currentColor = nil
		}
		s.drawLegacyTransformed(p, opacity, t)
		p.currentColor = prevCC
		return
	}
	pooled := false
	if fr == nil {
		fr = framePool.Get().(*frame)
		pooled = true
	}
	s.scene.evaluate(fr, opts)
	renderFrame(p, fr, masksBuf, opacity, t)
	if pooled {
		fr.reset()
		framePool.Put(fr)
	}
}

// ForceSceneDraw draws s unconditionally through the scene graph in static mode.
// Used by differential tests to verify pixel-identical output against legacy Draw.
func (s *SvgIcon) ForceSceneDraw(p *Painter, opacity float64, t rasterx.Matrix2D) {
	if s.scene == nil {
		s.drawLegacyTransformed(p, opacity, t)
		return
	}
	fr := framePool.Get().(*frame)
	s.scene.evaluate(fr, &EvalOptions{Animate: false})
	renderFrame(p, fr, nil, opacity, t)
	fr.reset()
	framePool.Put(fr)
}

func (s *SvgIcon) canUseFastPath(opts *EvalOptions) bool {
	if s.scene == nil {
		return true
	}
	if s.needsSceneForStatic() {
		return false
	}
	if opts == nil {
		return true
	}
	if opts.Animate && s.Animated() {
		return false
	}
	if len(opts.Overrides) > 0 {
		return false
	}
	return true
}

func (s *SvgIcon) needsSceneForStatic() bool {
	if s == nil || s.scene == nil {
		return false
	}
	return s.scene.hasStaticSceneOnly || s.gradHasCurrentColor
}

// ---- Query methods on SvgIcon (immutable, goroutine-safe) ----

// Animated reports whether s contains any parsed SMIL or CSS animations.
func (s *SvgIcon) Animated() bool {
	if s == nil || s.scene == nil || s.scene.animSet == nil {
		return false
	}
	return s.scene.animSet.Animated()
}

// Timeline returns the document's summarized animation timeline.
func (s *SvgIcon) Timeline() svganim.Timeline {
	if s == nil || s.scene == nil || s.scene.animSet == nil {
		return svganim.Timeline{}
	}
	return s.scene.animSet.Timeline()
}

// UsesCurrentColor reports whether s references the SVG currentColor keyword.
func (s *SvgIcon) UsesCurrentColor() bool {
	if s == nil {
		return false
	}
	return s.usesCurrentColor
}

// NextChange returns the next animation change time > t (in seconds).
func (s *SvgIcon) NextChange(t float64) (float64, bool) {
	if s == nil || s.scene == nil || s.scene.animSet == nil {
		return 0, false
	}
	return s.scene.animSet.NextChange(t)
}

// Node returns the index of the first element in document order with the given id.
func (s *SvgIcon) Node(id string) (int32, bool) {
	if s == nil || s.scene == nil || id == "" {
		return -1, false
	}
	ids := s.scene.byID[id]
	if len(ids) == 0 {
		return -1, false
	}
	return int32(ids[0]), true
}

// NodeID returns the id of element idx.
func (s *SvgIcon) NodeID(idx int32) string {
	if s == nil || s.scene == nil || idx < 0 || int(idx) >= len(s.scene.elems) {
		return ""
	}
	return s.scene.elems[idx].id
}

// NodeTag returns the XML tag name of element idx.
func (s *SvgIcon) NodeTag(idx int32) string {
	if s == nil || s.scene == nil || idx < 0 || int(idx) >= len(s.scene.elems) {
		return ""
	}
	return s.scene.elems[idx].tag
}

// NodeLocalBounds returns the static fill bounding box of element idx in its own local space.
func (s *SvgIcon) NodeLocalBounds(idx int32) (svganim.Rect, bool) {
	if s == nil || s.scene == nil || idx < 0 || int(idx) >= len(s.scene.elems) {
		return svganim.Rect{}, false
	}
	return s.scene.elemLocalBounds(svganim.ElemID(idx))
}

// NodeIDs returns a fresh slice of unique element IDs in document order.
func (s *SvgIcon) NodeIDs() []string {
	if s == nil || s.scene == nil || len(s.scene.idOrder) == 0 {
		return nil
	}
	return slices.Clone(s.scene.idOrder)
}

// Bounds returns the bounding box of element(s) with id in document user space
// evaluated at opts (nil means static base state).
func (s *SvgIcon) Bounds(id string, opts *EvalOptions) (svganim.Rect, bool) {
	if s == nil || s.scene == nil || id == "" {
		return svganim.Rect{}, false
	}
	if len(s.scene.byID[id]) == 0 {
		return svganim.Rect{}, false
	}
	fr := framePool.Get().(*frame)
	defer func() {
		fr.reset()
		framePool.Put(fr)
	}()
	s.scene.evaluate(fr, opts)

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	found := false

	for i := range fr.items {
		it := &fr.items[i]
		if it.isMarker {
			continue
		}
		ancs := fr.ancPool[it.ancStart : it.ancStart+it.ancLen]
		matched := false
		for _, aeid := range ancs {
			if s.scene.elems[aeid].id == id {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		x0, y0, x1, y1, ok := pathBoundsTransformed(it.path, it.world)
		if ok {
			minX = min(minX, x0)
			minY = min(minY, y0)
			maxX = max(maxX, x1)
			maxY = max(maxY, y1)
			found = true
		}
	}
	if !found {
		return svganim.Rect{}, false
	}
	return svganim.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}, true
}
