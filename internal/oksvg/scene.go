// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"encoding/xml"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

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

type elemLazy struct {
	lensOnce   sync.Once
	lens       *lengthTable
	vertsOnce  sync.Once
	verts      []vertex
	boundsOnce sync.Once
	boundsDone atomic.Bool
	localBox   svganim.Rect
	hasBounds  bool
}

type element struct {
	id       string
	tag      string
	classes  []string
	children []svganim.ElemID
	attrs    []xml.Attr
	path     rasterx.Path
	pathLen  float64 // SVG pathLength attribute; <= 0 means unset
	geom     *geometry
	spec     *stylePatch
	lazy     atomic.Pointer[elemLazy]
	parent   svganim.ElemID
	kind     elemKind
}

func (e *element) getLazy() *elemLazy {
	if lz := e.lazy.Load(); lz != nil {
		return lz
	}
	lz := &elemLazy{}
	if e.lazy.CompareAndSwap(nil, lz) {
		return lz
	}
	return e.lazy.Load()
}

func (e *element) getLengthTable() *lengthTable {
	lz := e.getLazy()
	lz.lensOnce.Do(func() {
		lz.lens = buildLengthTable(e.path)
	})
	return lz.lens
}

func (e *element) getVertices() []vertex {
	lz := e.getLazy()
	lz.vertsOnce.Do(func() {
		lz.verts = vertices(e.path)
	})
	return lz.verts
}

func (e *element) getGeom() geometry {
	if e.geom == nil {
		return geometry{}
	}
	return *e.geom
}

func (e *element) isDisplayNone() bool {
	return e.spec != nil && e.spec.bits&pbDisplay != 0 && e.spec.displayNone
}

type scene struct {
	icon        *SvgIcon
	elems       []element
	roots       []svganim.ElemID
	byID        map[string][]svganim.ElemID
	idOrder     []string
	clips       map[string]svganim.ElemID
	markers     map[string]svganim.ElemID
	markerSpecs map[svganim.ElemID]parsedMarker
	grads       map[string]*rasterx.Gradient
	animSet     *svganim.AnimSet
	stylesheet  *svganim.Stylesheet
	diags       []svganim.Diag

	// Memoized compiled paths for animated `d` keyframes (guarded by pathCacheMu).
	pathCacheMu    sync.RWMutex
	pathCache      map[string]rasterx.Path
	pathCacheBytes int

	usesCurrentColor    bool
	gradHasCurrentColor bool
	hasStaticSceneOnly  bool
}

const (
	maxPathCacheEntries = 256
	maxPathCacheBytes   = 4 << 20 // 4 MiB retained ceiling
)

// compiledPath returns the compiled rasterx.Path for SVG path data d,
// memoizing results across frames for discrete `d` animations under both
// entry-count and retained-byte limits (clearing on full so later keys cache).
func (sc *scene) compiledPath(d string) rasterx.Path {
	if d == "" {
		return nil
	}
	sc.pathCacheMu.RLock()
	p, ok := sc.pathCache[d]
	sc.pathCacheMu.RUnlock()
	if ok {
		return p
	}
	var pc pathCursor
	_ = pc.CompilePath(d)
	var compiled rasterx.Path
	if len(pc.Path) > 0 {
		compiled = slices.Clone(pc.Path)
	}
	entryBytes := len(d) + len(compiled)*4
	if entryBytes <= maxPathCacheBytes {
		sc.pathCacheMu.Lock()
		if sc.pathCache == nil {
			sc.pathCache = make(map[string]rasterx.Path, 4)
		}
		if len(sc.pathCache) >= maxPathCacheEntries || sc.pathCacheBytes+entryBytes > maxPathCacheBytes {
			clear(sc.pathCache)
			sc.pathCacheBytes = 0
		}
		if _, exists := sc.pathCache[d]; !exists {
			sc.pathCache[d] = compiled
			sc.pathCacheBytes += entryBytes
		}
		sc.pathCacheMu.Unlock()
	}
	return compiled
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

type boundsCtx struct {
	work    int
	aborted bool
}

// elemLocalBounds computes the static fill-box of e in e's own local user space.
// If the traversal aborts due to MaxNestingDepth or MaxExpandedElements, the
// unavailable result (zero rect, false) is remembered by boundsOnce rather than
// caching any partial rectangle.
func (sc *scene) elemLocalBounds(eid svganim.ElemID) (svganim.Rect, bool) {
	if eid < 0 || int(eid) >= len(sc.elems) {
		return svganim.Rect{}, false
	}
	e := &sc.elems[eid]
	lz := e.getLazy()
	lz.boundsOnce.Do(func() {
		var visited [16]svganim.ElemID
		var ctx boundsCtx
		r, ok := sc.computeLocalBounds(eid, visited[:0], 0, false, &ctx)
		if ctx.aborted {
			lz.localBox = svganim.Rect{}
			lz.hasBounds = false
		} else {
			lz.localBox = r
			lz.hasBounds = ok
		}
		lz.boundsDone.Store(true)
	})
	return lz.localBox, lz.hasBounds
}

// elemLocalBoundsCtx computes eid's local fill-box within an in-flight bounds
// query, reusing an already-cached top-level result if available and otherwise
// threading the caller's depth and work budget without writing into eid's cache
// (so a caller-depth abort cannot poison an otherwise valid child).
func (sc *scene) elemLocalBoundsCtx(eid svganim.ElemID, depth int, amplified bool, ctx *boundsCtx) (svganim.Rect, bool) {
	if ctx == nil {
		return sc.elemLocalBounds(eid)
	}
	if eid < 0 || int(eid) >= len(sc.elems) {
		return svganim.Rect{}, false
	}
	if lz := sc.elems[eid].lazy.Load(); lz != nil && lz.boundsDone.Load() {
		return lz.localBox, lz.hasBounds
	}
	var visited [16]svganim.ElemID
	r, ok := sc.computeLocalBounds(eid, visited[:0], depth, amplified, ctx)
	if ctx.aborted {
		return svganim.Rect{}, false
	}
	return r, ok
}

func (sc *scene) computeLocalBounds(eid svganim.ElemID, stack []svganim.ElemID, depth int, amplified bool, ctx *boundsCtx) (svganim.Rect, bool) {
	if ctx.aborted {
		return svganim.Rect{}, false
	}
	depth++
	if depth > MaxNestingDepth {
		ctx.aborted = true
		return svganim.Rect{}, false
	}
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
	if e.isDisplayNone() {
		return svganim.Rect{}, false
	}
	if amplified {
		ctx.work++
		if ctx.work > MaxExpandedElements {
			ctx.aborted = true
			return svganim.Rect{}, false
		}
	}
	if e.geom != nil && e.geom.kind == geomUse {
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
		tm := sc.staticLocalMatrixCtx(target, depth, true, ctx)
		if ctx.aborted {
			return svganim.Rect{}, false
		}
		m := rasterx.Identity.Translate(e.geom.x, e.geom.y).Mult(tm)
		minX, minY, maxX, maxY, ok := sc.subtreeBounds(target, m, stack, depth, true, ctx)
		if ctx.aborted || !ok {
			return svganim.Rect{}, false
		}
		return svganim.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}, true
	}
	if e.geom != nil && e.geom.kind != geomNone {
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
		if ctx.aborted {
			return svganim.Rect{}, false
		}
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
		if c.isDisplayNone() {
			continue
		}
		cm := sc.staticLocalMatrixCtx(cid, depth, amplified, ctx)
		if ctx.aborted {
			return svganim.Rect{}, false
		}
		cx0, cy0, cx1, cy1, ok := sc.subtreeBounds(cid, cm, stack, depth, amplified, ctx)
		if ctx.aborted {
			return svganim.Rect{}, false
		}
		if ok {
			minX = min(minX, cx0)
			minY = min(minY, cy0)
			maxX = max(maxX, cx1)
			maxY = max(maxY, cy1)
			found = true
		}
	}
	if !found || ctx.aborted {
		return svganim.Rect{}, false
	}
	return svganim.Rect{X: minX, Y: minY, W: maxX - minX, H: maxY - minY}, true
}

func (sc *scene) subtreeBounds(eid svganim.ElemID, m rasterx.Matrix2D, stack []svganim.ElemID, depth int, amplified bool, ctx *boundsCtx) (minX, minY, maxX, maxY float64, ok bool) {
	if ctx.aborted {
		return 0, 0, 0, 0, false
	}
	depth++
	if depth > MaxNestingDepth {
		ctx.aborted = true
		return 0, 0, 0, 0, false
	}
	if eid < 0 || int(eid) >= len(sc.elems) {
		return 0, 0, 0, 0, false
	}
	e := &sc.elems[eid]
	if e.isDisplayNone() {
		return 0, 0, 0, 0, false
	}
	if amplified {
		ctx.work++
		if ctx.work > MaxExpandedElements {
			ctx.aborted = true
			return 0, 0, 0, 0, false
		}
	}
	if e.geom != nil && e.geom.kind == geomUse {
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
		lm := sc.staticLocalMatrixCtx(target, depth, true, ctx)
		if ctx.aborted {
			return 0, 0, 0, 0, false
		}
		tm := m.Translate(e.geom.x, e.geom.y).Mult(lm)
		return sc.subtreeBounds(target, tm, stack, depth, true, ctx)
	}
	if e.geom != nil && e.geom.kind != geomNone {
		return pathBoundsTransformed(e.path, m)
	}
	stack = append(stack, eid)
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, cid := range e.children {
		if ctx.aborted {
			return 0, 0, 0, 0, false
		}
		c := &sc.elems[cid]
		if c.tag == "defs" || c.tag == "clipPath" || c.tag == "mask" || c.tag == "marker" ||
			c.kind == kindGradient || c.kind == kindNonRendered {
			continue
		}
		if c.isDisplayNone() {
			continue
		}
		lm := sc.staticLocalMatrixCtx(cid, depth, amplified, ctx)
		if ctx.aborted {
			return 0, 0, 0, 0, false
		}
		cm := m.Mult(lm)
		cx0, cy0, cx1, cy1, cok := sc.subtreeBounds(cid, cm, stack, depth, amplified, ctx)
		if ctx.aborted {
			return 0, 0, 0, 0, false
		}
		if cok {
			minX = min(minX, cx0)
			minY = min(minY, cy0)
			maxX = max(maxX, cx1)
			maxY = max(maxY, cy1)
			ok = true
		}
	}
	if ctx.aborted {
		return 0, 0, 0, 0, false
	}
	return minX, minY, maxX, maxY, ok
}

func (sc *scene) staticRawTransformCtx(eid svganim.ElemID, depth int, amplified bool, ctx *boundsCtx) rasterx.Matrix2D {
	sp := sc.elems[eid].spec
	if sp == nil || sp.bits&pbTransform == 0 || sp.extra == nil {
		return rasterx.Identity
	}
	ex := sp.extra
	if ex.hasXformVal && ex.xformHasPct {
		return sc.resolveTransformValueCtx(eid, ex.xformVal, ex.tbox, depth, amplified, ctx)
	}
	return ex.xform
}

func (sc *scene) resolveTransformValue(eid svganim.ElemID, v svganim.Value, tbox transformBoxKind) rasterx.Matrix2D {
	return sc.resolveTransformValueCtx(eid, v, tbox, 0, false, nil)
}

func (sc *scene) resolveTransformValueCtx(eid svganim.ElemID, v svganim.Value, tbox transformBoxKind, depth int, amplified bool, bctx *boundsCtx) rasterx.Matrix2D {
	vb := sc.icon.ViewBox
	ctx := svganim.EvalCtx{
		Viewport: svganim.Rect{X: 0, Y: 0, W: vb.W, H: vb.H},
		FontSize: 16,
	}
	if tbox == tbFillBox || tbox == tbStrokeBox {
		ctx.FillBox = func() svganim.Rect { r, _ := sc.elemLocalBoundsCtx(eid, depth, amplified, bctx); return r }
	}
	return v.ToMatrix(&ctx)
}

func (sc *scene) staticLocalMatrixCtx(eid svganim.ElemID, depth int, amplified bool, ctx *boundsCtx) rasterx.Matrix2D {
	sp := sc.elems[eid].spec
	if sp == nil || sp.bits&pbTransform == 0 || sp.extra == nil {
		return rasterx.Identity
	}
	m := sc.staticRawTransformCtx(eid, depth, amplified, ctx)
	if ctx != nil && ctx.aborted {
		return rasterx.Identity
	}
	if sp.bits&pbTransformOrigin != 0 {
		ox, oy := sc.resolveOriginCtx(eid, sp.extra.origin, sp.extra.tbox, depth, amplified, ctx)
		if ctx != nil && ctx.aborted {
			return rasterx.Identity
		}
		if ox != 0 || oy != 0 {
			m = rasterx.Identity.Translate(ox, oy).Mult(m).Translate(-ox, -oy)
		}
	}
	return m
}

func (sc *scene) resolveOrigin(eid svganim.ElemID, origin [2]svganim.OriginAxis, tbox transformBoxKind) (ox, oy float64) {
	return sc.resolveOriginCtx(eid, origin, tbox, 0, false, nil)
}

func (sc *scene) resolveOriginCtx(eid svganim.ElemID, origin [2]svganim.OriginAxis, tbox transformBoxKind, depth int, amplified bool, ctx *boundsCtx) (ox, oy float64) {
	var box svganim.Rect
	switch tbox {
	case tbFillBox, tbStrokeBox:
		box, _ = sc.elemLocalBoundsCtx(eid, depth, amplified, ctx)
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

type limitClipKey struct {
	defElem svganim.ElemID
	isMask  bool
	world   rasterx.Matrix2D
}

// validateLimits checks that the static scene does not exceed MaxNestingDepth
// or MaxExpandedElements (including forward <use> references, clips, and markers).
func (sc *scene) validateLimits() error {
	if sc == nil || len(sc.roots) == 0 {
		return nil
	}
	var ancStack []svganim.ElemID
	var seenClips map[limitClipKey]struct{}
	expanded := 0
	clipDepth := 0
	markerDepth := 0
	inClip := false

	var walk func(eid svganim.ElemID, world rasterx.Matrix2D, markers [3]string, forceRender bool, depth int) error
	walk = func(eid svganim.ElemID, world rasterx.Matrix2D, markers [3]string, forceRender bool, depth int) error {
		if eid < 0 || int(eid) >= len(sc.elems) {
			return nil
		}
		e := &sc.elems[eid]
		if !forceRender {
			if e.kind != kindRendered {
				return nil
			}
		} else {
			if e.kind == kindGradient || e.kind == kindNonRendered ||
				e.tag == "defs" || e.tag == "clipPath" || e.tag == "mask" || e.tag == "marker" {
				return nil
			}
		}
		depth++
		if depth > MaxNestingDepth {
			return ErrNestingDepth
		}
		if e.isDisplayNone() {
			return nil
		}
		if forceRender {
			expanded++
			if expanded > MaxExpandedElements {
				return ErrExpansionLimit
			}
		}

		ancStack = append(ancStack, eid)
		defer func() { ancStack = ancStack[:len(ancStack)-1] }()

		if sp := e.spec; sp != nil && sp.extra != nil {
			ex := sp.extra
			if sp.bits&pbMarkerStart != 0 {
				markers[0] = ex.markers[0]
			}
			if sp.bits&pbMarkerMid != 0 {
				markers[1] = ex.markers[1]
			}
			if sp.bits&pbMarkerEnd != 0 {
				markers[2] = ex.markers[2]
			}
			if sp.bits&pbTransform != 0 && !ex.xformHasPct && ex.xform != rasterx.Identity {
				world = world.Mult(ex.xform)
			}
			if !inClip && clipDepth < 8 {
				if sp.bits&pbClipPath != 0 && ex.clipID != "" {
					if cElem, ok := sc.clips[ex.clipID]; ok && sc.elems[cElem].tag == "clipPath" {
						key := limitClipKey{defElem: cElem, isMask: false, world: world}
						if _, seen := seenClips[key]; !seen {
							if seenClips == nil {
								seenClips = make(map[limitClipKey]struct{}, 4)
							}
							seenClips[key] = struct{}{}
							if depth+1 > MaxNestingDepth {
								return ErrNestingDepth
							}
							clipDepth++
							inClip = true
							ancStack = append(ancStack, cElem)
							for _, cid := range sc.elems[cElem].children {
								if err := walk(cid, world, [3]string{}, true, depth+1); err != nil {
									return err
								}
							}
							ancStack = ancStack[:len(ancStack)-1]
							inClip = false
							clipDepth--
						}
					}
				}
				if sp.bits&pbMask != 0 && ex.maskID != "" {
					if mElem, ok := sc.clips[ex.maskID]; ok && sc.elems[mElem].tag == "mask" {
						key := limitClipKey{defElem: mElem, isMask: true, world: world}
						if _, seen := seenClips[key]; !seen {
							if seenClips == nil {
								seenClips = make(map[limitClipKey]struct{}, 4)
							}
							seenClips[key] = struct{}{}
							if depth+1 > MaxNestingDepth {
								return ErrNestingDepth
							}
							clipDepth++
							ancStack = append(ancStack, mElem)
							for _, cid := range sc.elems[mElem].children {
								if err := walk(cid, world, [3]string{}, true, depth+1); err != nil {
									return err
								}
							}
							ancStack = ancStack[:len(ancStack)-1]
							clipDepth--
						}
					}
				}
			}
		}

		if e.geom != nil && e.geom.kind == geomUse {
			if !strings.HasPrefix(e.geom.href, "#") {
				return nil
			}
			targets := sc.byID[e.geom.href[1:]]
			if len(targets) == 0 {
				return nil
			}
			target := targets[0]
			if slices.Contains(ancStack, target) {
				return nil
			}
			useWorld := world
			if e.geom.x != 0 || e.geom.y != 0 {
				useWorld = world.Translate(e.geom.x, e.geom.y)
			}
			return walk(target, useWorld, markers, true, depth)
		}

		if e.geom != nil && e.geom.kind != geomNone && len(e.path) > 0 {
			if !inClip && markers != [3]string{} && markerShapes[e.tag] && markerDepth < 4 {
				vs := e.getVertices()
				markerDepth++
				for i := range vs {
					which := 1
					if i == 0 {
						which = 0
					} else if i == len(vs)-1 {
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
					if depth+1 > MaxNestingDepth {
						return ErrNestingDepth
					}
					ancStack = append(ancStack, mElem)
					for _, cid := range sc.elems[mElem].children {
						if err := walk(cid, world, [3]string{}, true, depth+1); err != nil {
							return err
						}
					}
					ancStack = ancStack[:len(ancStack)-1]
				}
				markerDepth--
			}
		}

		for _, cid := range e.children {
			if err := walk(cid, world, markers, forceRender, depth); err != nil {
				return err
			}
		}
		return nil
	}

	for _, rid := range sc.roots {
		if err := walk(rid, rasterx.Identity, [3]string{}, false, 0); err != nil {
			return err
		}
	}
	return nil
}
