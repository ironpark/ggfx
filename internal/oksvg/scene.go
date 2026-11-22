// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"encoding/xml"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/ironpark/ggfx/internal/rasterx"
	"github.com/ironpark/ggfx/internal/svganim"
)

type elemKind uint8

const (
	kindRendered elemKind = iota
	kindDefs
	kindClipContent
	kindMaskContent
	kindMarkerContent
	kindGradient
	kindNonRendered
)

type element struct {
	id       string
	tag      string
	classes  []string
	parent   svganim.ElemID
	children []svganim.ElemID
	attrs    []xml.Attr
	kind     elemKind
	geom     geometry
	path     rasterx.Path
	pathLen  float64 // SVG pathLength attribute; <= 0 means unset
	spec     stylePatch

	// Lazily populated under sync.Once; immutable after ReadIconStream returns.
	lensOnce   sync.Once
	lens       *lengthTable
	vertsOnce  sync.Once
	verts      []vertex
	boundsOnce sync.Once
	localBox   svganim.Rect
	hasBounds  bool
}

func (e *element) getLengthTable() *lengthTable {
	e.lensOnce.Do(func() {
		e.lens = buildLengthTable(e.path)
	})
	return e.lens
}

func (e *element) getVertices() []vertex {
	e.vertsOnce.Do(func() {
		e.verts = vertices(e.path)
	})
	return e.verts
}

type scene struct {
	icon       *SvgIcon
	elems      []element
	roots      []svganim.ElemID
	byID       map[string][]svganim.ElemID
	idOrder    []string
	clips      map[string]svganim.ElemID
	markers    map[string]svganim.ElemID
	grads      map[string]*rasterx.Gradient
	animSet    *svganim.AnimSet
	stylesheet *svganim.Stylesheet
	diags      []svganim.Diag

	usesCurrentColor    bool
	gradHasCurrentColor bool
	hasStaticSceneOnly  bool
}

// Implement svganim.SceneView on *scene.

func (sc *scene) Elems() int { return len(sc.elems) }

func (sc *scene) Tag(e svganim.ElemID) string {
	if e < 0 || int(e) >= len(sc.elems) {
		return ""
	}
	return sc.elems[e].tag
}

func (sc *scene) ID(e svganim.ElemID) string {
	if e < 0 || int(e) >= len(sc.elems) {
		return ""
	}
	return sc.elems[e].id
}

func (sc *scene) Classes(e svganim.ElemID) []string {
	if e < 0 || int(e) >= len(sc.elems) {
		return nil
	}
	return sc.elems[e].classes
}

func (sc *scene) Parent(e svganim.ElemID) svganim.ElemID {
	if e < 0 || int(e) >= len(sc.elems) {
		return -1
	}
	return sc.elems[e].parent
}

func (sc *scene) ByID(id string) []svganim.ElemID {
	return sc.byID[id]
}

func (sc *scene) RawAttr(e svganim.ElemID, name string) (string, bool) {
	if e < 0 || int(e) >= len(sc.elems) {
		return "", false
	}
	localName := name
	if strings.EqualFold(name, "xlink:href") {
		localName = "href"
	}
	for _, a := range sc.elems[e].attrs {
		if a.Name.Local == localName {
			return a.Value, true
		}
	}
	return "", false
}

// elemLocalBounds computes the static fill-box of e in e's own local user space.
func (sc *scene) elemLocalBounds(eid svganim.ElemID) (svganim.Rect, bool) {
	if eid < 0 || int(eid) >= len(sc.elems) {
		return svganim.Rect{}, false
	}
	e := &sc.elems[eid]
	e.boundsOnce.Do(func() {
		var visited [16]svganim.ElemID
		r, ok := sc.computeLocalBounds(eid, visited[:0])
		e.localBox = r
		e.hasBounds = ok
	})
	return e.localBox, e.hasBounds
}

func (sc *scene) computeLocalBounds(eid svganim.ElemID, stack []svganim.ElemID) (svganim.Rect, bool) {
	if eid < 0 || int(eid) >= len(sc.elems) {
		return svganim.Rect{}, false
	}
	e := &sc.elems[eid]
	if e.kind == kindGradient || e.kind == kindNonRendered {
		return svganim.Rect{}, false
	}
	if e.tag == "defs" {
		return svganim.Rect{}, false
	}
	if e.spec.bits&pbDisplay != 0 && e.spec.displayNone {
		return svganim.Rect{}, false
	}
	if e.geom.kind == geomUse {
		if !strings.HasPrefix(e.geom.href, "#") {
			return svganim.Rect{}, false
		}
		targets := sc.byID[e.geom.href[1:]]
		if len(targets) == 0 {
			return svganim.Rect{}, false
		}
		target := targets[0]
		if slices.Contains(stack, target) {
			return svganim.Rect{}, false
		}
		stack = append(stack, eid, target)
		m := rasterx.Identity.Translate(e.geom.x, e.geom.y).Mult(sc.staticLocalMatrix(target))
		minX, minY, maxX, maxY, ok := sc.subtreeBounds(target, m, stack)
		if !ok {
			return svganim.Rect{}, false
		}
		return svganim.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}, true
	}
	if e.geom.kind != geomNone {
		minX, minY, maxX, maxY, ok := pathBoundsTransformed(e.path, rasterx.Identity)
		if !ok {
			return svganim.Rect{}, false
		}
		return svganim.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}, true
	}
	// Container (<g>, <svg>, <clipPath>, <mask>, <marker>): union of children in this element's local space.
	stack = append(stack, eid)
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	found := false
	for _, cid := range e.children {
		c := &sc.elems[cid]
		if c.kind == kindDefs || c.kind == kindClipContent || c.kind == kindMaskContent ||
			c.kind == kindMarkerContent || c.kind == kindGradient || c.kind == kindNonRendered {
			if e.kind != kindDefs && e.kind != kindClipContent && e.kind != kindMaskContent && e.kind != kindMarkerContent {
				continue
			}
		}
		if c.tag == "defs" || c.tag == "clipPath" || c.tag == "mask" || c.tag == "marker" ||
			c.kind == kindGradient || c.kind == kindNonRendered {
			continue
		}
		if c.spec.bits&pbDisplay != 0 && c.spec.displayNone {
			continue
		}
		cm := sc.staticLocalMatrix(cid)
		cx0, cy0, cx1, cy1, ok := sc.subtreeBounds(cid, cm, stack)
		if ok {
			minX = min(minX, cx0)
			minY = min(minY, cy0)
			maxX = max(maxX, cx1)
			maxY = max(maxY, cy1)
			found = true
		}
	}
	if !found {
		return svganim.Rect{}, false
	}
	return svganim.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}, true
}

func (sc *scene) subtreeBounds(eid svganim.ElemID, m rasterx.Matrix2D, stack []svganim.ElemID) (minX, minY, maxX, maxY float64, ok bool) {
	e := &sc.elems[eid]
	if e.spec.bits&pbDisplay != 0 && e.spec.displayNone {
		return 0, 0, 0, 0, false
	}
	if e.geom.kind == geomUse {
		if !strings.HasPrefix(e.geom.href, "#") {
			return 0, 0, 0, 0, false
		}
		targets := sc.byID[e.geom.href[1:]]
		if len(targets) == 0 {
			return 0, 0, 0, 0, false
		}
		target := targets[0]
		if slices.Contains(stack, target) {
			return 0, 0, 0, 0, false
		}
		stack = append(stack, eid, target)
		tm := m.Translate(e.geom.x, e.geom.y).Mult(sc.staticLocalMatrix(target))
		return sc.subtreeBounds(target, tm, stack)
	}
	if e.geom.kind != geomNone {
		return pathBoundsTransformed(e.path, m)
	}
	stack = append(stack, eid)
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, cid := range e.children {
		c := &sc.elems[cid]
		if c.tag == "defs" || c.tag == "clipPath" || c.tag == "mask" || c.tag == "marker" ||
			c.kind == kindGradient || c.kind == kindNonRendered {
			continue
		}
		if c.spec.bits&pbDisplay != 0 && c.spec.displayNone {
			continue
		}
		cm := m.Mult(sc.staticLocalMatrix(cid))
		cx0, cy0, cx1, cy1, cok := sc.subtreeBounds(cid, cm, stack)
		if cok {
			minX = min(minX, cx0)
			minY = min(minY, cy0)
			maxX = max(maxX, cx1)
			maxY = max(maxY, cy1)
			ok = true
		}
	}
	return minX, minY, maxX, maxY, ok
}

func (sc *scene) staticRawTransform(eid svganim.ElemID) rasterx.Matrix2D {
	e := &sc.elems[eid]
	if e.spec.bits&pbTransform == 0 {
		return rasterx.Identity
	}
	if e.spec.hasXformVal && e.spec.xformHasPct {
		return sc.resolveTransformValue(eid, e.spec.xformVal, e.spec.tbox)
	}
	return e.spec.xform
}

func (sc *scene) resolveTransformValue(eid svganim.ElemID, v svganim.Value, tbox transformBoxKind) rasterx.Matrix2D {
	vb := sc.icon.ViewBox
	ctx := svganim.EvalCtx{
		Viewport: svganim.Rect{X: 0, Y: 0, W: vb.W, H: vb.H},
		FontSize: 16,
	}
	if tbox == tbFillBox || tbox == tbStrokeBox {
		ctx.FillBox = func() svganim.Rect { r, _ := sc.elemLocalBounds(eid); return r }
	}
	return v.ToMatrix(&ctx)
}

func (sc *scene) staticLocalMatrix(eid svganim.ElemID) rasterx.Matrix2D {
	e := &sc.elems[eid]
	if e.spec.bits&pbTransform == 0 {
		return rasterx.Identity
	}
	m := sc.staticRawTransform(eid)
	if e.spec.bits&pbTransformOrigin != 0 {
		ox, oy := sc.resolveOrigin(eid, e.spec.origin, e.spec.tbox)
		if ox != 0 || oy != 0 {
			m = rasterx.Identity.Translate(ox, oy).Mult(m).Translate(-ox, -oy)
		}
	}
	return m
}

func (sc *scene) resolveOrigin(eid svganim.ElemID, origin [2]svganim.OriginAxis, tbox transformBoxKind) (ox, oy float64) {
	var box svganim.Rect
	switch tbox {
	case tbFillBox, tbStrokeBox:
		box, _ = sc.elemLocalBounds(eid)
	default: // tbViewBox: CSS Transforms 1 defines reference box origin (0,0), size viewBox W×H.
		box = svganim.Rect{X: 0, Y: 0, W: sc.icon.ViewBox.W, H: sc.icon.ViewBox.H}
	}
	if origin[0].Unit == svganim.UnitPct {
		ox = box.X + (origin[0].Val/100)*box.W
	} else {
		ox = box.X + origin[0].Val
	}
	if origin[1].Unit == svganim.UnitPct {
		oy = box.Y + (origin[1].Val/100)*box.H
	} else {
		oy = box.Y + origin[1].Val
	}
	return ox, oy
}
