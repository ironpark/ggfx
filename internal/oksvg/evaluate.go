// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image/color"
	"math"
	"slices"
	"strings"

	"github.com/ironpark/ggfx/internal/rasterx"
	"github.com/ironpark/ggfx/internal/svganim"
)

type drawItem struct {
	elem     svganim.ElemID
	world    rasterx.Matrix2D
	style    PathStyle
	path     rasterx.Path
	lens     *lengthTable
	trim     trimSpec
	scope    int32
	isMarker bool
	visible  bool
	ancStart int32
	ancLen   int32
}

type clipScope struct {
	defElem   svganim.ElemID
	isMask    bool
	parent    int32
	itemStart int32
	itemCount int32
}

type clipScopeKey struct {
	defElem     svganim.ElemID
	isMask      bool
	refWorld    rasterx.Matrix2D
	parentScope int32
}

type frame struct {
	items       []drawItem
	clipItems   []drawItem
	scopes      []clipScope
	clipScopes  map[clipScopeKey]int32
	ancPool     []svganim.ElemID
	pathScratch rasterx.Path
	dashScratch []float64
}

func (f *frame) reset() {
	clear(f.items)
	f.items = f.items[:0]
	clear(f.clipItems)
	f.clipItems = f.clipItems[:0]
	f.scopes = f.scopes[:0]
	clear(f.clipScopes)
	f.ancPool = f.ancPool[:0]
	f.pathScratch = f.pathScratch[:0]
	f.dashScratch = f.dashScratch[:0]
}

type evalState struct {
	world         rasterx.Matrix2D
	style         PathStyle
	accOpacity    float64
	fillOpacity   float64
	strokeOpacity float64
	color         color.Color
	visible       bool
	trim          trimSpec
	scope         int32
}

func defaultEvalState(world rasterx.Matrix2D, rootColor color.Color, scope int32) evalState {
	if rootColor == nil {
		rootColor = color.NRGBA{0, 0, 0, 0xff}
	}
	return evalState{
		world:         world,
		style:         defaultStyle,
		accOpacity:    1.0,
		fillOpacity:   1.0,
		strokeOpacity: 1.0,
		color:         rootColor,
		visible:       true,
		scope:         scope,
	}
}

type evaluator struct {
	sc          *scene
	opts        *EvalOptions
	fr          *frame
	destItems   *[]drawItem
	ancStack    []svganim.ElemID
	depth       int
	expanded    int
	err         error
	clipDepth   int
	markerDepth int
	inClip      bool
	inMarker    bool
}

func (sc *scene) evaluate(fr *frame, opts *EvalOptions) error {
	fr.reset()
	if opts == nil {
		opts = &EvalOptions{}
	}
	rootColor := opts.CurrentColor
	if rootColor == nil {
		rootColor = color.NRGBA{0, 0, 0, 0xff}
	}
	ev := evaluator{
		sc:        sc,
		opts:      opts,
		fr:        fr,
		destItems: &fr.items,
	}
	rootState := defaultEvalState(rasterx.Identity, rootColor, -1)
	for _, rid := range sc.roots {
		ev.evalElem(rid, rootState, false)
		if ev.err != nil {
			fr.reset()
			return ev.err
		}
	}
	return nil
}

func (ev *evaluator) evalElem(eid svganim.ElemID, parent evalState, forceRender bool) {
	if ev.err != nil {
		return
	}
	sc := ev.sc
	if eid < 0 || int(eid) >= len(sc.elems) {
		return
	}
	e := &sc.elems[eid]

	if !forceRender {
		if e.kind != kindRendered {
			return
		}
	} else {
		if e.kind == kindGradient || e.kind == kindNonRendered ||
			e.tag == "defs" || e.tag == "clipPath" || e.tag == "mask" || e.tag == "marker" {
			return
		}
	}
	ev.depth++
	if ev.depth > MaxNestingDepth {
		ev.depth--
		ev.err = ErrNestingDepth
		return
	}

	ev.ancStack = append(ev.ancStack, eid)
	defer func() {
		ev.ancStack = ev.ancStack[:len(ev.ancStack)-1]
		ev.depth--
	}()

	st := parent
	sp := e.spec

	// Non-inherited local properties.
	elemOpacity := 1.0
	displayNone := false
	localM := rasterx.Identity
	var origin [2]svganim.OriginAxis
	hasOrigin := false
	var tbox transformBoxKind
	clipID := ""
	maskID := ""

	// 1. Apply static cascade stylePatch onto inherited state.
	if sp != nil {
		ex := sp.extra
		if ex != nil && sp.bits&pbColor != 0 && ex.color != nil {
			st.color = ex.color
		}
		if sp.bits&pbFill != 0 {
			if ex != nil && ex.fillGradID != "" {
				if g, ok := sc.grads[ex.fillGradID]; ok {
					st.style.fillerColor = paint{grad: localizeGradIfStopClrNil(g, parent.style.fillerColor), gradID: ex.fillGradID}
				}
			} else {
				st.style.fillerColor = sp.fill
			}
		}
		if sp.bits&pbStroke != 0 {
			if ex != nil && ex.strokeGradID != "" {
				if g, ok := sc.grads[ex.strokeGradID]; ok {
					st.style.linerColor = paint{grad: localizeGradIfStopClrNil(g, parent.style.linerColor), gradID: ex.strokeGradID}
				}
			} else {
				st.style.linerColor = sp.stroke
			}
		}
		if sp.bits&pbFillRule != 0 {
			st.style.UseNonZeroWinding = sp.nonZeroFill
		}
		if sp.bits&pbClipRule != 0 {
			st.style.nonZeroClip = sp.nonZeroClip
		}
		if sp.bits&pbStrokeWidth != 0 {
			st.style.LineWidth = sp.strokeWidth
		}
		if sp.bits&pbVisibility != 0 {
			st.visible = !sp.hidden
		}
		if sp.bits&pbOpacity != 0 {
			elemOpacity = sp.opacity
		}
		displayNone = sp.bits&pbDisplay != 0 && sp.displayNone

		if ex != nil {
			if sp.bits&pbLineGap != 0 {
				st.style.LineGap = ex.lineGap
			}
			if sp.bits&pbLeadLineCap != 0 {
				st.style.LeadLineCap = ex.leadLineCap
			}
			if sp.bits&pbLineCap != 0 {
				st.style.LineCap = ex.lineCap
			}
			if sp.bits&pbLineJoin != 0 {
				st.style.LineJoin = ex.lineJoin
			}
			if sp.bits&pbMiterLimit != 0 {
				st.style.MiterLimit = ex.miterLimit
			}
			if sp.bits&pbDash != 0 {
				st.style.Dash = ex.dash
			}
			if sp.bits&pbDashOffset != 0 {
				st.style.DashOffset = ex.dashOffset
			}
			// Quirk q1: fill-opacity and stroke-opacity multiply down the tree statically.
			if sp.bits&pbFillOpacity != 0 {
				st.fillOpacity *= ex.fillOpacity
			}
			if sp.bits&pbStrokeOpacity != 0 {
				st.strokeOpacity *= ex.strokeOpacity
			}
			if sp.bits&pbMarkerStart != 0 {
				st.style.markers[0] = ex.markers[0]
			}
			if sp.bits&pbMarkerMid != 0 {
				st.style.markers[1] = ex.markers[1]
			}
			if sp.bits&pbMarkerEnd != 0 {
				st.style.markers[2] = ex.markers[2]
			}
			if sp.bits&pbTransform != 0 && !ex.xformHasPct {
				localM = ex.xform
			}
			origin = ex.origin
			hasOrigin = sp.bits&pbTransformOrigin != 0
			tbox = ex.tbox
			if sp.bits&pbClipPath != 0 {
				clipID = ex.clipID
			}
			if sp.bits&pbMask != 0 {
				maskID = ex.maskID
			}
		}
	}

	// Track whether geometry or transform was modified by animations.
	geom := e.getGeom()
	geomAnimated := false
	xformAnimated := false

	// 2. Apply SMIL & CSS animations via svganim.AnimSet.
	if ev.opts.Animate && sc.animSet != nil {
		if mask := sc.animSet.AnimatedProps(eid); mask.Any() {
			ev.applyAnimations(eid, e, mask, &st, parent, &elemOpacity, &displayNone,
				&localM, &xformAnimated, &origin, &hasOrigin, &tbox, &geom, &geomAnimated)
		}
	}
	if !xformAnimated && sp != nil && sp.extra != nil && sp.bits&pbTransform != 0 && sp.extra.hasXformVal && sp.extra.xformHasPct {
		localM = sc.resolveTransformValue(eid, sp.extra.xformVal, tbox)
	}

	// 3. Apply programmatic NodeOverride (highest precedence).
	var ov *NodeOverride
	if e.id != "" && len(ev.opts.Overrides) > 0 {
		if o, ok := ev.opts.Overrides[e.id]; ok {
			ov = &o
			applyNodeOverrideState(ov, &st, parent, &elemOpacity, &displayNone, true)
		}
	}

	if displayNone {
		return
	}
	if forceRender {
		ev.expanded++
		if ev.expanded > MaxExpandedElements {
			ev.err = ErrExpansionLimit
			return
		}
	}

	// 4. Compose transform: M_node = O · T(origin) · M_local · T(-origin).
	mBase := localM
	if hasOrigin && mBase != rasterx.Identity {
		ox, oy := sc.resolveOrigin(eid, origin, tbox)
		if ox != 0 || oy != 0 {
			mBase = rasterx.Identity.Translate(ox, oy).Mult(mBase).Translate(-ox, -oy)
		}
	}
	mNode := mBase
	if ov != nil {
		mNode = applyOverrideTransform(sc, eid, mBase, ov)
	}
	if mNode != rasterx.Identity {
		st.world = parent.world.Mult(mNode)
	}
	st.accOpacity = parent.accOpacity * elemOpacity

	// 5. Evaluate clip-path and mask scopes in the element's evaluated world space.
	if !ev.inClip && ev.clipDepth < 8 {
		if clipID != "" {
			if cElem, ok := sc.clips[clipID]; ok && sc.elems[cElem].tag == "clipPath" {
				st.scope = ev.pushClipScope(cElem, false, st.world, st.scope)
			}
		}
		if maskID != "" {
			if mElem, ok := sc.clips[maskID]; ok && sc.elems[mElem].tag == "mask" {
				st.scope = ev.pushClipScope(mElem, true, st.world, st.scope)
			}
		}
	}

	// 6. Expand <use> at evaluate time.
	if geom.kind == geomUse {
		if !strings.HasPrefix(geom.href, "#") {
			return
		}
		targets := sc.byID[geom.href[1:]]
		if len(targets) == 0 {
			return
		}
		target := targets[0]
		if slices.Contains(ev.ancStack, target) {
			return // cyclic <use>
		}
		useState := st
		if geom.x != 0 || geom.y != 0 {
			useState.world = st.world.Translate(geom.x, geom.y)
		}
		ev.evalElem(target, useState, true)
		return
	}

	// 7. Emit shape drawItem (if this element has geometry).
	if geom.kind != geomNone {
		var path rasterx.Path
		var lt *lengthTable
		if geomAnimated {
			if geom.kind == geomPath {
				if e.geom != nil && geom.d == e.geom.d {
					path = e.path
				} else {
					path = sc.compiledPath(geom.d)
				}
			} else {
				startIdx := len(ev.fr.pathScratch)
				geom.buildInto(&ev.fr.pathScratch)
				path = ev.fr.pathScratch[startIdx:]
			}
		} else {
			path = e.path
		}
		if len(path) > 0 {
			itemStyle := st.style
			itemStyle.m = st.world
			itemStyle.FillOpacity = st.accOpacity * st.fillOpacity
			itemStyle.LineOpacity = st.accOpacity * st.strokeOpacity
			itemStyle.color = st.color
			if itemStyle.fillerColor.current {
				itemStyle.fillerColor = paint{color: st.color}
			}
			if itemStyle.linerColor.current {
				itemStyle.linerColor = paint{color: st.color}
			}
			// Scale dasharray / dashoffset when pathLength > 0.
			if e.pathLen > 0 && !ev.inClip && !itemStyle.linerColor.none() && (len(itemStyle.Dash) > 0 || itemStyle.DashOffset != 0) {
				if !geomAnimated {
					lt = e.getLengthTable()
				} else {
					lt = buildLengthTable(path)
				}
				if lt.total > 0 {
					factor := lt.total / e.pathLen
					if len(itemStyle.Dash) > 0 {
						dStart := len(ev.fr.dashScratch)
						for _, d := range itemStyle.Dash {
							ev.fr.dashScratch = append(ev.fr.dashScratch, d*factor)
						}
						itemStyle.Dash = ev.fr.dashScratch[dStart:]
					}
					itemStyle.DashOffset *= factor
				}
			}
			itemTrim := st.trim
			if itemTrim.set && lt == nil && !geomAnimated {
				lt = e.getLengthTable()
			}
			if ev.inClip {
				itemStyle.fillerColor = paint{color: color.NRGBA{0xff, 0xff, 0xff, 0xff}}
				itemStyle.linerColor = paint{}
				itemStyle.FillOpacity = 1
				itemStyle.UseNonZeroWinding = st.style.nonZeroClip
				itemTrim = trimSpec{}
			}
			ancStart := int32(len(ev.fr.ancPool))
			ev.fr.ancPool = append(ev.fr.ancPool, ev.ancStack...)
			*ev.destItems = append(*ev.destItems, drawItem{
				elem:     eid,
				world:    st.world,
				style:    itemStyle,
				path:     path,
				lens:     lt,
				trim:     itemTrim,
				scope:    st.scope,
				isMarker: ev.inMarker,
				visible:  st.visible,
				ancStart: ancStart,
				ancLen:   int32(len(ev.ancStack)),
			})

			// 8. Draw-time markers (follow shape's display & visibility; inherit shape's scope).
			if !ev.inClip && st.visible && st.style.markers != [3]string{} && markerShapes[e.tag] && ev.markerDepth < 4 {
				ev.emitMarkers(e, path, geomAnimated, st.world, st.style.LineWidth, st.style.markers, st.scope)
			}
		}
	}

	// 9. Recurse into children.
	for _, cid := range e.children {
		ev.evalElem(cid, st, forceRender)
		if ev.err != nil {
			return
		}
	}
}

func (ev *evaluator) pushClipScope(defElem svganim.ElemID, isMask bool, refWorld rasterx.Matrix2D, parentScope int32) int32 {
	if ev.err != nil {
		return parentScope
	}
	key := clipScopeKey{
		defElem:     defElem,
		isMask:      isMask,
		refWorld:    refWorld,
		parentScope: parentScope,
	}
	if idx, ok := ev.fr.clipScopes[key]; ok {
		return idx
	}
	sc := ev.sc
	def := &sc.elems[defElem]
	sp := def.spec

	scopeIdx := int32(len(ev.fr.scopes))
	ev.fr.scopes = append(ev.fr.scopes, clipScope{
		defElem: defElem,
		isMask:  isMask,
		parent:  parentScope,
	})
	if ev.fr.clipScopes == nil {
		ev.fr.clipScopes = make(map[clipScopeKey]int32, 4)
	}
	ev.fr.clipScopes[key] = scopeIdx

	// Evaluate the clipPath/mask element's own transform, rules, animations, and overrides.
	displayNone := false
	localM := rasterx.Identity
	var origin [2]svganim.OriginAxis
	hasOrigin := false
	var tbox transformBoxKind

	rootColor := ev.opts.CurrentColor
	if rootColor == nil {
		rootColor = color.NRGBA{0, 0, 0, 0xff}
	}
	clipState := defaultEvalState(refWorld, rootColor, -1)
	elemOpacity := 1.0
	if sp != nil {
		displayNone = sp.bits&pbDisplay != 0 && sp.displayNone
		if sp.bits&pbFillRule != 0 {
			clipState.style.UseNonZeroWinding = sp.nonZeroFill
		}
		if sp.bits&pbClipRule != 0 {
			clipState.style.nonZeroClip = sp.nonZeroClip
		}
		if isMask && sp.bits&pbOpacity != 0 {
			elemOpacity = sp.opacity
		}
		if ex := sp.extra; ex != nil {
			if sp.bits&pbTransform != 0 && !ex.xformHasPct {
				localM = ex.xform
			}
			origin = ex.origin
			hasOrigin = sp.bits&pbTransformOrigin != 0
			tbox = ex.tbox
		}
	}

	var dummyGeom geometry
	var dummyAnim bool
	xformAnimated := false
	baseClipState := clipState
	if ev.opts.Animate && sc.animSet != nil {
		if mask := sc.animSet.AnimatedProps(defElem); mask.Any() {
			ev.applyAnimations(defElem, def, mask, &clipState, baseClipState, &elemOpacity, &displayNone,
				&localM, &xformAnimated, &origin, &hasOrigin, &tbox, &dummyGeom, &dummyAnim)
		}
	}
	if !xformAnimated && sp != nil && sp.extra != nil && sp.bits&pbTransform != 0 && sp.extra.hasXformVal && sp.extra.xformHasPct {
		localM = sc.resolveTransformValue(defElem, sp.extra.xformVal, tbox)
	}

	var ov *NodeOverride
	if def.id != "" && len(ev.opts.Overrides) > 0 {
		if o, ok := ev.opts.Overrides[def.id]; ok {
			ov = &o
			applyNodeOverrideState(ov, &clipState, baseClipState, &elemOpacity, &displayNone, isMask)
		}
	}

	if displayNone {
		return scopeIdx // empty clip/mask -> lets nothing through
	}

	mBase := localM
	if hasOrigin && mBase != rasterx.Identity {
		ox, oy := sc.resolveOrigin(defElem, origin, tbox)
		if ox != 0 || oy != 0 {
			mBase = rasterx.Identity.Translate(ox, oy).Mult(mBase).Translate(-ox, -oy)
		}
	}
	mNode := mBase
	if ov != nil {
		mNode = applyOverrideTransform(sc, defElem, mBase, ov)
	}
	if mNode != rasterx.Identity {
		clipState.world = refWorld.Mult(mNode)
	}
	if isMask {
		clipState.accOpacity = elemOpacity
	}

	oldDest := ev.destItems
	oldInClip := ev.inClip
	oldAnc := ev.ancStack
	ev.depth++
	if ev.depth > MaxNestingDepth {
		ev.depth--
		ev.err = ErrNestingDepth
		return scopeIdx
	}
	var ownBuf [4]drawItem
	ownItems := ownBuf[:0]
	ev.destItems = &ownItems
	ev.inClip = !isMask
	ev.clipDepth++
	ev.ancStack = append(ev.ancStack, defElem)

	for _, cid := range def.children {
		ev.evalElem(cid, clipState, true)
		if ev.err != nil {
			break
		}
	}

	ev.ancStack = oldAnc
	ev.clipDepth--
	ev.inClip = oldInClip
	ev.destItems = oldDest
	ev.depth--

	start := len(ev.fr.clipItems)
	ev.fr.clipItems = append(ev.fr.clipItems, ownItems...)
	ev.fr.scopes[scopeIdx].itemStart = int32(start)
	ev.fr.scopes[scopeIdx].itemCount = int32(len(ownItems))
	return scopeIdx
}

func (ev *evaluator) emitMarkers(e *element, path rasterx.Path, geomAnimated bool, shapeWorld rasterx.Matrix2D, lineWidth float64, markers [3]string, shapeScope int32) {
	sc := ev.sc
	var vs []vertex
	if !geomAnimated {
		vs = e.getVertices()
	} else {
		vs = vertices(path)
	}
	rootColor := ev.opts.CurrentColor
	if rootColor == nil {
		rootColor = color.NRGBA{0, 0, 0, 0xff}
	}

	ev.markerDepth++
	oldInMarker := ev.inMarker
	ev.inMarker = true
	defer func() {
		ev.inMarker = oldInMarker
		ev.markerDepth--
	}()

	for i, v := range vs {
		which, first := 1, false
		switch i {
		case 0:
			which, first = 0, true
		case len(vs) - 1:
			which = 2
		}
		id := markers[which]
		if id == "" {
			continue
		}
		mElem, ok := sc.markers[id]
		if !ok {
			continue
		}
		def := &sc.elems[mElem]
		sp := def.spec
		displayNone := false
		localM := rasterx.Identity
		var origin [2]svganim.OriginAxis
		hasOrigin := false
		var tbox transformBoxKind

		pm := sc.markerSpecs[mElem]
		mWorld := pm.place(shapeWorld, v, first, lineWidth)
		mState := defaultEvalState(mWorld, rootColor, shapeScope)
		if sp != nil {
			displayNone = sp.bits&pbDisplay != 0 && sp.displayNone
			if sp.bits&pbFillRule != 0 {
				mState.style.UseNonZeroWinding = sp.nonZeroFill
			}
			if sp.bits&pbClipRule != 0 {
				mState.style.nonZeroClip = sp.nonZeroClip
			}
			if ex := sp.extra; ex != nil {
				if sp.bits&pbTransform != 0 && !ex.xformHasPct {
					localM = ex.xform
				}
				origin = ex.origin
				hasOrigin = sp.bits&pbTransformOrigin != 0
				tbox = ex.tbox
			}
		}

		elemOpacity := 1.0
		var dummyGeom geometry
		var dummyAnim bool
		xformAnimated := false
		baseMState := mState
		if ev.opts.Animate && sc.animSet != nil {
			if mask := sc.animSet.AnimatedProps(mElem); mask.Any() {
				ev.applyAnimations(mElem, def, mask, &mState, baseMState, &elemOpacity, &displayNone,
					&localM, &xformAnimated, &origin, &hasOrigin, &tbox, &dummyGeom, &dummyAnim)
			}
		}
		if !xformAnimated && sp != nil && sp.extra != nil && sp.bits&pbTransform != 0 && sp.extra.hasXformVal && sp.extra.xformHasPct {
			localM = sc.resolveTransformValue(mElem, sp.extra.xformVal, tbox)
		}
		var ov *NodeOverride
		if def.id != "" && len(ev.opts.Overrides) > 0 {
			if o, ok := ev.opts.Overrides[def.id]; ok {
				ov = &o
				applyNodeOverrideState(ov, &mState, baseMState, &elemOpacity, &displayNone, true)
			}
		}
		if displayNone {
			continue
		}
		mBase := localM
		if hasOrigin && mBase != rasterx.Identity {
			ox, oy := sc.resolveOrigin(mElem, origin, tbox)
			if ox != 0 || oy != 0 {
				mBase = rasterx.Identity.Translate(ox, oy).Mult(mBase).Translate(-ox, -oy)
			}
		}
		mNode := mBase
		if ov != nil {
			mNode = applyOverrideTransform(sc, mElem, mBase, ov)
		}
		if mNode != rasterx.Identity {
			mState.world = mWorld.Mult(mNode)
		}
		mState.accOpacity = elemOpacity

		ev.depth++
		if ev.depth > MaxNestingDepth {
			ev.depth--
			ev.err = ErrNestingDepth
			return
		}
		oldAnc := ev.ancStack
		ev.ancStack = append(ev.ancStack, mElem)
		for _, cid := range def.children {
			ev.evalElem(cid, mState, true)
			if ev.err != nil {
				ev.ancStack = oldAnc
				ev.depth--
				return
			}
		}
		ev.ancStack = oldAnc
		ev.depth--
	}
}

func applyNodeOverrideState(o *NodeOverride, st *evalState, parent evalState, elemOpacity *float64, displayNone *bool, applyOpacity bool) {
	if o.HasHidden {
		*displayNone = o.Hidden
	}
	if o.HasVisible {
		st.visible = o.Visible
	}
	if o.HasColor && o.Color != nil {
		st.color = o.Color
	}
	if o.HasFill {
		st.style.fillerColor = applyPaintOverride(o.Fill, parent.style.fillerColor)
	}
	if o.HasStroke {
		st.style.linerColor = applyPaintOverride(o.Stroke, parent.style.linerColor)
	}
	if applyOpacity && o.HasOpacity {
		*elemOpacity = max(0, min((*elemOpacity)*o.Opacity, 1))
	}
	if o.HasFillOpacity {
		st.fillOpacity = max(0, min(o.FillOpacity, 1))
	}
	if o.HasStrokeOpacity {
		st.strokeOpacity = max(0, min(o.StrokeOpacity, 1))
	}
	if o.HasStrokeWidth {
		st.style.LineWidth = o.StrokeWidth
	}
	if o.HasDash {
		st.style.Dash = o.Dash
	}
	if o.HasDashOffset {
		st.style.DashOffset = o.DashOffset
	}
	if o.HasTrim {
		st.trim = trimSpec{
			start:  o.TrimStart,
			end:    o.TrimEnd,
			offset: o.TrimOffset,
			set:    true,
		}
	}
}

func applyPaintOverride(p PaintOverride, parentPaint paint) paint {
	switch p.Kind {
	case PaintOvNone:
		return paint{}
	case PaintOvColor:
		if p.Color == nil {
			return paint{}
		}
		return paint{color: toNRGBA(p.Color)}
	case PaintOvCurrentColor:
		return paint{current: true}
	case PaintOvInherit:
		return parentPaint
	default:
		return paint{}
	}
}

func applyOverrideTransform(sc *scene, eid svganim.ElemID, mBase rasterx.Matrix2D, ov *NodeOverride) rasterx.Matrix2D {
	if ov.HasMatrix {
		return ov.Matrix.Mult(mBase)
	}
	hasRSS := ov.HasRotate || ov.HasSkew || ov.HasScale
	if !ov.HasTranslate && !hasRSS {
		return mBase
	}
	o := rasterx.Identity
	if ov.HasTranslate {
		o = o.Translate(ov.Tx, ov.Ty)
	}
	if hasRSS {
		var lpx, lpy float64
		if ov.HasPivot {
			if ov.Pivot.Box {
				if box, ok := sc.elemLocalBounds(eid); ok {
					lpx = box.X + ov.Pivot.X*box.W
					lpy = box.Y + ov.Pivot.Y*box.H
				}
			} else {
				lpx, lpy = ov.Pivot.X, ov.Pivot.Y
			}
		}
		// Map local pivot point into parent user space through the node's static+animated transform.
		pvx, pvy := mBase.Transform(lpx, lpy)
		if pvx != 0 || pvy != 0 {
			o = o.Translate(pvx, pvy)
		}
		if ov.HasRotate && ov.Rotate != 0 {
			o = o.Rotate(ov.Rotate)
		}
		if ov.HasSkew {
			if ov.SkewX != 0 {
				o = o.SkewX(ov.SkewX)
			}
			if ov.SkewY != 0 {
				o = o.SkewY(ov.SkewY)
			}
		}
		if ov.HasScale {
			o = o.Scale(ov.Sx, ov.Sy)
		}
		if pvx != 0 || pvy != 0 {
			o = o.Translate(-pvx, -pvy)
		}
	}
	return o.Mult(mBase)
}

func (ev *evaluator) applyAnimations(
	eid svganim.ElemID,
	e *element,
	mask svganim.PropMask,
	st *evalState,
	parent evalState,
	elemOpacity *float64,
	displayNone *bool,
	localM *rasterx.Matrix2D,
	xformAnimated *bool,
	origin *[2]svganim.OriginAxis,
	hasOrigin *bool,
	tbox *transformBoxKind,
	geom *geometry,
	geomAnimated *bool,
) {
	sc := ev.sc
	a := sc.animSet
	t := ev.opts.Time
	vb := sc.icon.ViewBox
	diag := math.Hypot(vb.W, vb.H) / math.Sqrt2

	ctx := svganim.EvalCtx{
		Viewport: svganim.Rect{X: 0, Y: 0, W: vb.W, H: vb.H},
		Color:    colorToRGBA4(st.color),
		FontSize: 16,
	}
	setCtxFillBox := func() {
		if *tbox == tbFillBox || *tbox == tbStrokeBox {
			ctx.FillBox = func() svganim.Rect { r, _ := sc.elemLocalBounds(eid); return r }
		} else {
			ctx.FillBox = nil
		}
	}
	setCtxFillBox()

	// 1. Color first so currentColor reflects animated color.
	if mask.Has(svganim.PropColor) {
		parentColor4 := colorToRGBA4(parent.color)
		base := svganim.Value{Kind: svganim.ValColor, Color: ctx.Color, Paint: svganim.PaintColor}
		colorCtx := ctx
		colorCtx.Color = parentColor4
		if v, ok := a.Eval(eid, svganim.PropColor, t, base, &colorCtx); ok {
			if v.Paint == svganim.PaintCurrentColor || v.Paint == svganim.PaintInherit ||
				(v.Kind == svganim.ValKeyword && strings.EqualFold(v.Keyword, "inherit")) {
				st.color = parent.color
				ctx.Color = parentColor4
			} else if v.Kind == svganim.ValColor {
				st.color = rgba4ToNRGBA(v.Color)
				ctx.Color = v.Color
			}
		}
	}
	if mask.Has(svganim.PropDisplay) {
		kw := "inline"
		if *displayNone {
			kw = "none"
		}
		if v, ok := a.Eval(eid, svganim.PropDisplay, t, svganim.Value{Kind: svganim.ValKeyword, Keyword: kw}, &ctx); ok {
			*displayNone = strings.EqualFold(v.Keyword, "none")
		}
	}
	if mask.Has(svganim.PropVisibility) {
		kw := "visible"
		if !st.visible {
			kw = "hidden"
		}
		if v, ok := a.Eval(eid, svganim.PropVisibility, t, svganim.Value{Kind: svganim.ValKeyword, Keyword: kw}, &ctx); ok {
			switch strings.ToLower(v.Keyword) {
			case "hidden", "collapse":
				st.visible = false
			case "visible":
				st.visible = true
			case "inherit":
				st.visible = parent.visible
			}
		}
	}
	if mask.Has(svganim.PropOpacity) {
		if v, ok := a.Eval(eid, svganim.PropOpacity, t, svganim.Value{Kind: svganim.ValNumber, Num: *elemOpacity}, &ctx); ok {
			*elemOpacity = max(0, min(v.Num, 1))
		}
	}
	if mask.Has(svganim.PropFillOpacity) {
		if v, ok := a.Eval(eid, svganim.PropFillOpacity, t, svganim.Value{Kind: svganim.ValNumber, Num: st.fillOpacity}, &ctx); ok {
			st.fillOpacity = max(0, min(v.Num, 1))
		}
	}
	if mask.Has(svganim.PropStrokeOpacity) {
		if v, ok := a.Eval(eid, svganim.PropStrokeOpacity, t, svganim.Value{Kind: svganim.ValNumber, Num: st.strokeOpacity}, &ctx); ok {
			st.strokeOpacity = max(0, min(v.Num, 1))
		}
	}
	if mask.Has(svganim.PropFill) {
		base := paintToAnimValue(st.style.fillerColor, ctx.Color)
		if v, ok := a.Eval(eid, svganim.PropFill, t, base, &ctx); ok {
			st.style.fillerColor = animValueToPaint(v, sc, parent.style.fillerColor)
		}
	}
	if mask.Has(svganim.PropStroke) {
		base := paintToAnimValue(st.style.linerColor, ctx.Color)
		if v, ok := a.Eval(eid, svganim.PropStroke, t, base, &ctx); ok {
			st.style.linerColor = animValueToPaint(v, sc, parent.style.linerColor)
		}
	}
	if mask.Has(svganim.PropFillRule) {
		kw := "evenodd"
		if st.style.UseNonZeroWinding {
			kw = "nonzero"
		}
		if v, ok := a.Eval(eid, svganim.PropFillRule, t, svganim.Value{Kind: svganim.ValKeyword, Keyword: kw}, &ctx); ok {
			switch strings.ToLower(v.Keyword) {
			case "nonzero":
				st.style.UseNonZeroWinding = true
			case "evenodd":
				st.style.UseNonZeroWinding = false
			case "inherit":
				st.style.UseNonZeroWinding = parent.style.UseNonZeroWinding
			}
		}
	}
	if mask.Has(svganim.PropClipRule) {
		kw := "evenodd"
		if st.style.nonZeroClip {
			kw = "nonzero"
		}
		if v, ok := a.Eval(eid, svganim.PropClipRule, t, svganim.Value{Kind: svganim.ValKeyword, Keyword: kw}, &ctx); ok {
			switch strings.ToLower(v.Keyword) {
			case "nonzero":
				st.style.nonZeroClip = true
			case "evenodd":
				st.style.nonZeroClip = false
			case "inherit":
				st.style.nonZeroClip = parent.style.nonZeroClip
			}
		}
	}
	if mask.Has(svganim.PropStrokeWidth) {
		if v, ok := a.Eval(eid, svganim.PropStrokeWidth, t, svganim.Value{Kind: svganim.ValLength, Num: st.style.LineWidth}, &ctx); ok {
			st.style.LineWidth = resolveAnimLength(v, diag, ctx.FontSize)
		}
	}
	if mask.Has(svganim.PropStrokeDasharray) {
		var base svganim.Value
		if len(st.style.Dash) == 0 {
			base = svganim.Value{Kind: svganim.ValKeyword, Keyword: "none"}
		} else {
			base = svganim.Value{Kind: svganim.ValNumberList, List: st.style.Dash}
		}
		if v, ok := a.Eval(eid, svganim.PropStrokeDasharray, t, base, &ctx); ok {
			if v.Kind == svganim.ValKeyword {
				switch strings.ToLower(v.Keyword) {
				case "none":
					st.style.Dash = nil
				case "inherit":
					st.style.Dash = parent.style.Dash
				}
			} else if v.Kind == svganim.ValNumberList {
				st.style.Dash = v.List
			}
		}
	}
	if mask.Has(svganim.PropStrokeDashoffset) {
		if v, ok := a.Eval(eid, svganim.PropStrokeDashoffset, t, svganim.Value{Kind: svganim.ValLength, Num: st.style.DashOffset}, &ctx); ok {
			st.style.DashOffset = resolveAnimLength(v, diag, ctx.FontSize)
		}
	}
	if mask.Has(svganim.PropStrokeLinecap) {
		base := svganim.Value{Kind: svganim.ValKeyword, Keyword: capFuncKeyword(st.style.LineCap)}
		if v, ok := a.Eval(eid, svganim.PropStrokeLinecap, t, base, &ctx); ok {
			if strings.EqualFold(v.Keyword, "inherit") {
				st.style.LineCap = parent.style.LineCap
			} else if cf := parseCapFunc(v.Keyword); cf != nil {
				st.style.LineCap = cf
			}
		}
	}
	if mask.Has(svganim.PropStrokeLinejoin) {
		base := svganim.Value{Kind: svganim.ValKeyword, Keyword: joinModeKeyword(st.style.LineJoin)}
		if v, ok := a.Eval(eid, svganim.PropStrokeLinejoin, t, base, &ctx); ok {
			if strings.EqualFold(v.Keyword, "inherit") {
				st.style.LineJoin = parent.style.LineJoin
			} else if jm, ok := parseJoinMode(v.Keyword); ok {
				st.style.LineJoin = jm
			}
		}
	}
	if mask.Has(svganim.PropStrokeMiterlimit) {
		if v, ok := a.Eval(eid, svganim.PropStrokeMiterlimit, t, svganim.Value{Kind: svganim.ValNumber, Num: st.style.MiterLimit}, &ctx); ok {
			st.style.MiterLimit = v.Num
		}
	}
	if mask.Has(svganim.PropTransformBox) {
		base := svganim.Value{Kind: svganim.ValKeyword, Keyword: transformBoxKeyword(*tbox)}
		if v, ok := a.Eval(eid, svganim.PropTransformBox, t, base, &ctx); ok {
			if tb, ok := parseTransformBox(v.Keyword); ok {
				*tbox = tb
				setCtxFillBox()
			}
		}
	}
	if mask.Has(svganim.PropTransformOrigin) {
		base := svganim.Value{Kind: svganim.ValOrigin, Origin: *origin}
		if v, ok := a.Eval(eid, svganim.PropTransformOrigin, t, base, &ctx); ok && v.Kind == svganim.ValOrigin {
			*origin = v.Origin
			*hasOrigin = true
		}
	}
	if mask.Has(svganim.PropTransform) {
		var base svganim.Value
		if e.spec != nil && e.spec.extra != nil && e.spec.extra.hasXformVal {
			base = e.spec.extra.xformVal
		} else if *localM != rasterx.Identity {
			m := *localM
			base = svganim.Value{
				Kind: svganim.ValTransformList,
				Xf: []svganim.TransformFn{{
					Op:   svganim.TfMatrix,
					Args: [6]float64{m.A, m.B, m.C, m.D, m.E, m.F},
					N:    6,
				}},
			}
		} else {
			base = svganim.Value{Kind: svganim.ValTransformList}
		}
		if v, ok := a.Eval(eid, svganim.PropTransform, t, base, &ctx); ok && v.Kind == svganim.ValTransformList {
			*localM = v.ToMatrix(&ctx)
			*xformAnimated = true
		}
	}

	// Geometry attributes.
	evalLen := func(prop svganim.PropID, cur, axis float64) (float64, bool) {
		if !mask.Has(prop) {
			return cur, false
		}
		v, ok := a.Eval(eid, prop, t, svganim.Value{Kind: svganim.ValLength, Num: cur}, &ctx)
		if !ok {
			return cur, false
		}
		return resolveAnimLength(v, axis, ctx.FontSize), true
	}

	switch geom.kind {
	case geomRect:
		if v, ok := evalLen(svganim.PropX, geom.x, vb.W); ok {
			geom.x, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropY, geom.y, vb.H); ok {
			geom.y, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropWidth, geom.w, vb.W); ok {
			geom.w, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropHeight, geom.h, vb.H); ok {
			geom.h, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropRx, geom.rx, vb.W); ok {
			geom.rx, geom.setRx, *geomAnimated = v, true, true
		}
		if v, ok := evalLen(svganim.PropRy, geom.ry, vb.H); ok {
			geom.ry, geom.setRy, *geomAnimated = v, true, true
		}
	case geomCircle:
		if v, ok := evalLen(svganim.PropCx, geom.x, vb.W); ok {
			geom.x, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropCy, geom.y, vb.H); ok {
			geom.y, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropR, geom.rx, diag); ok {
			geom.rx, geom.ry, *geomAnimated = v, v, true
		}
	case geomEllipse:
		if v, ok := evalLen(svganim.PropCx, geom.x, vb.W); ok {
			geom.x, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropCy, geom.y, vb.H); ok {
			geom.y, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropRx, geom.rx, vb.W); ok {
			geom.rx, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropRy, geom.ry, vb.H); ok {
			geom.ry, *geomAnimated = v, true
		}
	case geomLine:
		if v, ok := evalLen(svganim.PropX1, geom.x, vb.W); ok {
			geom.x, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropY1, geom.y, vb.H); ok {
			geom.y, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropX2, geom.w, vb.W); ok {
			geom.w, *geomAnimated = v, true
		}
		if v, ok := evalLen(svganim.PropY2, geom.h, vb.H); ok {
			geom.h, *geomAnimated = v, true
		}
	case geomPolyline, geomPolygon:
		if mask.Has(svganim.PropPoints) {
			if v, ok := a.Eval(eid, svganim.PropPoints, t, svganim.Value{Kind: svganim.ValNumberList, List: geom.points}, &ctx); ok && v.Kind == svganim.ValNumberList {
				geom.points = v.List
				geom.bad = len(v.List)%2 != 0
				*geomAnimated = true
			}
		}
	case geomPath:
		if mask.Has(svganim.PropD) {
			if v, ok := a.Eval(eid, svganim.PropD, t, svganim.Value{Kind: svganim.ValPathData, Raw: geom.d}, &ctx); ok && v.Kind == svganim.ValPathData {
				geom.d = v.Raw
				geom.bad = false
				*geomAnimated = true
			}
		}
	case geomUse:
		if v, ok := evalLen(svganim.PropX, geom.x, vb.W); ok {
			geom.x = v
		}
		if v, ok := evalLen(svganim.PropY, geom.y, vb.H); ok {
			geom.y = v
		}
	}
}

func resolveAnimLength(v svganim.Value, axisLen, fontSize float64) float64 {
	switch v.Unit {
	case svganim.UnitPct:
		return (v.Num / 100) * axisLen
	case svganim.UnitEm, svganim.UnitRem:
		return v.Num * fontSize
	default:
		return v.Num
	}
}

func capFuncKeyword(f rasterx.CapFunc) string {
	if idx, ok := funcIndex(binaryCaps, f); ok {
		switch idx {
		case 1:
			return "butt"
		case 2:
			return "square"
		case 3:
			return "round"
		case 4:
			return "cubic"
		case 5:
			return "quadratic"
		}
	}
	return "butt"
}

func joinModeKeyword(j rasterx.JoinMode) string {
	switch j {
	case rasterx.Miter:
		return "miter"
	case rasterx.MiterClip:
		return "miter-clip"
	case rasterx.ArcClip:
		return "arc-clip"
	case rasterx.Round:
		return "round"
	case rasterx.Arc:
		return "arc"
	case rasterx.Bevel:
		return "bevel"
	default:
		return "miter"
	}
}

func transformBoxKeyword(tb transformBoxKind) string {
	switch tb {
	case tbFillBox:
		return "fill-box"
	case tbStrokeBox:
		return "stroke-box"
	default:
		return "view-box"
	}
}

func paintToAnimValue(p paint, curColor [4]float64) svganim.Value {
	if p.current {
		return svganim.Value{Kind: svganim.ValPaint, Paint: svganim.PaintCurrentColor, Color: curColor}
	}
	if p.grad != nil {
		return svganim.Value{Kind: svganim.ValPaint, Paint: svganim.PaintURL, URL: p.gradID}
	}
	if p.color != nil {
		return svganim.Value{Kind: svganim.ValPaint, Paint: svganim.PaintColor, Color: colorToRGBA4(p.color)}
	}
	return svganim.Value{Kind: svganim.ValPaint, Paint: svganim.PaintNone}
}

func animValueToPaint(v svganim.Value, sc *scene, inherited paint) paint {
	if v.Kind == svganim.ValColor {
		return paint{color: rgba4ToNRGBA(v.Color)}
	}
	if v.Kind != svganim.ValPaint {
		return inherited
	}
	switch v.Paint {
	case svganim.PaintNone:
		return paint{}
	case svganim.PaintColor:
		return paint{color: rgba4ToNRGBA(v.Color)}
	case svganim.PaintCurrentColor:
		return paint{current: true}
	case svganim.PaintURL:
		if g, ok := sc.grads[v.URL]; ok {
			return paint{grad: localizeGradIfStopClrNil(g, inherited), gradID: v.URL}
		}
		return paint{}
	case svganim.PaintInherit:
		return inherited
	default:
		return paint{}
	}
}
