// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"encoding/xml"
	"slices"
	"strings"

	"github.com/ironpark/ggfx/internal/svganim"
)

type sceneBuilder struct {
	sc                  *scene
	stack               []svganim.ElemID
	defsDepth           int
	fallbackClasses     map[string][]svganim.Decl
	sawRenderedElem     bool
	sawLateStyle        bool
	sawAdvancedCSSRules bool
}

func newSceneBuilder(icon *SvgIcon) *sceneBuilder {
	sc := &scene{
		icon:    icon,
		byID:    make(map[string][]svganim.ElemID),
		clips:   make(map[string]svganim.ElemID),
		markers: make(map[string]svganim.ElemID),
		grads:   icon.Grads,
	}
	return &sceneBuilder{sc: sc}
}

func (b *sceneBuilder) currentElem() svganim.ElemID {
	if len(b.stack) == 0 {
		return -1
	}
	return b.stack[len(b.stack)-1]
}

func (b *sceneBuilder) inDefs() bool {
	return b.defsDepth > 0
}

func (b *sceneBuilder) startElement(se xml.StartElement) svganim.ElemID {
	tag := se.Name.Local
	parent := b.currentElem()

	var id string
	var classes []string
	attrs := slices.Clone(se.Attr)
	for _, a := range attrs {
		switch a.Name.Local {
		case "id":
			id = a.Value
		case "class":
			if f := strings.Fields(a.Value); len(f) > 0 {
				classes = f
			}
		}
	}

	kind := kindRendered
	switch tag {
	case "defs":
		kind = kindDefs
	case "clipPath":
		kind = kindClipContent
	case "mask":
		kind = kindMaskContent
	case "marker":
		kind = kindMarkerContent
	case "linearGradient", "radialGradient", "stop":
		kind = kindGradient
	case "style", "title", "desc":
		kind = kindNonRendered
	default:
		if parent >= 0 {
			pk := b.sc.elems[parent].kind
			if pk != kindRendered {
				kind = pk
			}
		}
	}

	geom, path := parseGeometry(tag, attrs)
	var geomPtr *geometry
	if geom.kind != geomNone {
		gp := geom
		geomPtr = &gp
	}

	eid := svganim.ElemID(len(b.sc.elems))
	b.sc.elems = append(b.sc.elems, element{
		id:      id,
		tag:     tag,
		classes: classes,
		parent:  parent,
		attrs:   attrs,
		kind:    kind,
		geom:    geomPtr,
		path:    path,
	})
	if parent >= 0 {
		b.sc.elems[parent].children = append(b.sc.elems[parent].children, eid)
	} else {
		b.sc.roots = append(b.sc.roots, eid)
	}
	if id != "" {
		if _, exists := b.sc.byID[id]; !exists {
			b.sc.idOrder = append(b.sc.idOrder, id)
		}
		b.sc.byID[id] = append(b.sc.byID[id], eid)
		switch tag {
		case "clipPath", "mask":
			b.sc.clips[id] = eid
		case "marker":
			b.sc.markers[id] = eid
			if b.sc.markerSpecs == nil {
				b.sc.markerSpecs = make(map[svganim.ElemID]parsedMarker, 4)
			}
			b.sc.markerSpecs[eid] = parseMarkerAttrs(attrs)
		}
	}
	if kind == kindRendered && tag != "svg" {
		b.sawRenderedElem = true
	}

	b.stack = append(b.stack, eid)
	if tag == "defs" {
		b.defsDepth++
	}
	return eid
}

func (b *sceneBuilder) endElement(se xml.EndElement) {
	if len(b.stack) == 0 {
		return
	}
	top := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	if b.sc.elems[top].tag == "defs" && b.defsDepth > 0 {
		b.defsDepth--
	}
}

func (b *sceneBuilder) addStylesheet(text string) {
	if b.sawRenderedElem {
		b.sawLateStyle = true
	}
	decls, adv := parseBraceAwareClasses(text)
	if adv {
		b.sawAdvancedCSSRules = true
	}
	if len(decls) > 0 {
		if b.fallbackClasses == nil {
			b.fallbackClasses = decls
		} else {
			for cls, list := range decls {
				b.fallbackClasses[cls] = append(b.fallbackClasses[cls], list...)
			}
		}
	}
}

func (b *sceneBuilder) finish(animBuilder svganim.Builder) *scene {
	sc := b.sc
	sc.grads = sc.icon.Grads
	if animBuilder != nil {
		sc.animSet, sc.stylesheet, sc.diags = animBuilder.Compile(sc)
	}
	if b.sawLateStyle || (sc.stylesheet != nil && b.sawAdvancedCSSRules) || hasOverlappingClassDecls(b.fallbackClasses) {
		sc.hasStaticSceneOnly = true
	}
	hasStyleRules := sc.stylesheet != nil || len(b.fallbackClasses) > 0
	for i := range sc.elems {
		if hasStyleRules && len(sc.elems[i].classes) > 1 {
			sc.hasStaticSceneOnly = true
		}
		resolveElementStylePatch(sc, svganim.ElemID(i), b.fallbackClasses)
	}
	if !sc.hasStaticSceneOnly && sc.hasReachableNestedClipOrMask() {
		sc.hasStaticSceneOnly = true
	}
	if sc.icon.usesCurrentColor {
		sc.usesCurrentColor = true
	}
	if sc.animSet != nil && sc.animSet.UsesCurrentColor() {
		sc.usesCurrentColor = true
	}
	if sc.icon.gradHasCurrentColor {
		sc.gradHasCurrentColor = true
		sc.hasStaticSceneOnly = true
	}
	if sc.usesCurrentColor {
		sc.icon.usesCurrentColor = true
	}
	return sc
}

// hasReachableNestedClipOrMask reports whether any <clipPath> or <mask>
// reachable from rendered content contains a descendant with its own
// clip-path, mask, or <use> reference. Unreferenced resources in <defs> are
// ignored so plain documents keep their binary form.
func (sc *scene) hasReachableNestedClipOrMask() bool {
	if len(sc.clips) == 0 {
		return false
	}
	var nestedDefs map[svganim.ElemID]bool
	var hasNestedContent func(eid svganim.ElemID) bool
	hasNestedContent = func(eid svganim.ElemID) bool {
		e := &sc.elems[eid]
		if sp := e.spec; sp != nil && sp.extra != nil &&
			((sp.bits&pbClipPath != 0 && sp.extra.clipID != "") || (sp.bits&pbMask != 0 && sp.extra.maskID != "")) {
			return true
		}
		if e.geom != nil && e.geom.kind == geomUse {
			return true
		}
		for _, cid := range e.children {
			if hasNestedContent(cid) {
				return true
			}
		}
		return false
	}
	for _, defID := range sc.clips {
		for _, cid := range sc.elems[defID].children {
			if hasNestedContent(cid) {
				if nestedDefs == nil {
					nestedDefs = make(map[svganim.ElemID]bool, 4)
				}
				nestedDefs[defID] = true
				break
			}
		}
	}
	if len(nestedDefs) == 0 {
		return false
	}

	// Traverse reachable elements from roots, visiting each ElemID at most once.
	visited := make([]bool, len(sc.elems))
	var queue []svganim.ElemID
	enqueue := func(id svganim.ElemID) {
		if id >= 0 && int(id) < len(sc.elems) && !visited[id] {
			visited[id] = true
			queue = append(queue, id)
		}
	}
	for _, rid := range sc.roots {
		enqueue(rid)
	}
	for len(queue) > 0 {
		eid := queue[0]
		queue = queue[1:]
		if nestedDefs[eid] {
			return true
		}
		e := &sc.elems[eid]
		if sp := e.spec; sp != nil && sp.extra != nil {
			ex := sp.extra
			if sp.bits&pbClipPath != 0 && ex.clipID != "" {
				if cElem, ok := sc.clips[ex.clipID]; ok {
					enqueue(cElem)
				}
			}
			if sp.bits&pbMask != 0 && ex.maskID != "" {
				if mElem, ok := sc.clips[ex.maskID]; ok {
					enqueue(mElem)
				}
			}
			for _, mid := range ex.markers {
				if mid != "" {
					if mElem, ok := sc.markers[mid]; ok {
						enqueue(mElem)
					}
				}
			}
		}
		if e.geom != nil && e.geom.kind == geomUse && strings.HasPrefix(e.geom.href, "#") {
			if targets := sc.byID[e.geom.href[1:]]; len(targets) > 0 {
				enqueue(targets[0])
			}
		}
		for _, cid := range e.children {
			c := &sc.elems[cid]
			if c.kind == kindRendered || e.kind == kindDefs || e.kind == kindClipContent ||
				e.kind == kindMaskContent || e.kind == kindMarkerContent {
				if c.tag != "defs" && c.tag != "clipPath" && c.tag != "mask" && c.tag != "marker" {
					enqueue(cid)
				}
			}
		}
	}
	return false
}

func hasOverlappingClassDecls(classes map[string][]svganim.Decl) bool {
	if len(classes) == 0 {
		return false
	}
	for _, decls := range classes {
		if len(decls) < 2 {
			continue
		}
		seen := make(map[string]struct{}, len(decls))
		for _, d := range decls {
			k := strings.ToLower(d.Name)
			if _, ok := seen[k]; ok {
				return true
			}
			seen[k] = struct{}{}
		}
	}
	return false
}
