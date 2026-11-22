// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"
)

type builderImpl struct {
	smil   []rawSMILElem
	styles []string
}

func newBuilderImpl() Builder {
	return &builderImpl{}
}

func (b *builderImpl) SMILElement(se xml.StartElement, parent ElemID, inDefs bool) {
	attrs := make([]xml.Attr, len(se.Attr))
	copy(attrs, se.Attr)
	b.smil = append(b.smil, rawSMILElem{
		tag:      se.Name.Local,
		attrs:    attrs,
		parent:   parent,
		inDefs:   inDefs,
		docOrder: len(b.smil),
	})
}

func (b *builderImpl) Stylesheet(text string) {
	if strings.TrimSpace(text) != "" {
		b.styles = append(b.styles, text)
	}
}

func (b *builderImpl) Compile(sv SceneView) (*AnimSet, *Stylesheet, []Diag) {
	var diags []Diag

	// 1. Parse CSS stylesheets.
	ssImpl := newStylesheetImpl()
	for _, text := range b.styles {
		ssImpl.parseCSSInto(text, &ssImpl.rules, ssImpl.keyframes, &diags)
	}
	sheet := &Stylesheet{impl: ssImpl}

	// 2. Compile SMIL animations.
	smilTracks, hasAnySMIL, smilDiags := compileSMIL(b.smil, sv)
	diags = append(diags, smilDiags...)

	// 3. Compile CSS animations for every element in SceneView.
	var cssTracks []*animTrack
	hasAnyCSS := false
	if sv != nil {
		docOrder := len(b.smil)
		for i := range sv.Elems() {
			e := ElemID(i)
			trs, seenCSS, ds := compileCSSAnimsForElem(sheet, ssImpl, sv, e, &docOrder)
			diags = append(diags, ds...)
			if seenCSS {
				hasAnyCSS = true
			}
			cssTracks = append(cssTracks, trs...)
		}
	}

	// 4. Build immutable AnimSet.
	allTracks := make([]*animTrack, 0, len(smilTracks)+len(cssTracks))
	allTracks = append(allTracks, smilTracks...)
	allTracks = append(allTracks, cssTracks...)

	byElem := make(map[ElemID]*elemTracks)
	usesCurrentColor := false
	for _, tr := range allTracks {
		if tr.prop == PropColor {
			usesCurrentColor = true
		} else if !usesCurrentColor {
			for _, kf := range tr.kf {
				if !kf.fromBase && kf.v.Paint == PaintCurrentColor {
					usesCurrentColor = true
					break
				}
			}
		}
		et := byElem[tr.elem]
		if et == nil {
			et = &elemTracks{
				smil: make(map[PropID][]*animTrack),
				css:  make(map[PropID][]*animTrack),
			}
			byElem[tr.elem] = et
		}
		et.mask.Set(tr.prop)
		if tr.origin == originSMIL {
			et.smil[tr.prop] = append(et.smil[tr.prop], tr)
		} else {
			et.css[tr.prop] = append(et.css[tr.prop], tr)
		}
	}

	setImpl := &animSetImpl{
		byElem:           byElem,
		allTracks:        allTracks,
		hasAnyAnim:       hasAnySMIL || hasAnyCSS || len(allTracks) > 0,
		usesCurrentColor: usesCurrentColor,
		tl:               computeTimeline(allTracks),
	}

	return &AnimSet{impl: setImpl}, sheet, diags
}

// collectElementCSSDecls merges stylesheet declarations and inline style="..." in cascade order:
// stylesheet normal < inline normal < stylesheet !important < inline !important.
func collectElementCSSDecls(sheet *Stylesheet, sv SceneView, e ElemID) []Decl {
	sheetDecls := declsForImpl(sheet, sv, e)
	var inlineDecls []Decl
	if styleAttr, ok := sv.RawAttr(e, "style"); ok && strings.TrimSpace(styleAttr) != "" {
		inlineDecls = parseDeclarations(styleAttr)
	}
	if len(inlineDecls) == 0 {
		return sheetDecls
	}
	if len(sheetDecls) == 0 {
		// Sort inline normal before inline !important.
		out := make([]Decl, 0, len(inlineDecls))
		for _, d := range inlineDecls {
			if !d.Important {
				out = append(out, d)
			}
		}
		for _, d := range inlineDecls {
			if d.Important {
				out = append(out, d)
			}
		}
		return out
	}

	out := make([]Decl, 0, len(sheetDecls)+len(inlineDecls))
	// 1. Stylesheet normal
	for _, d := range sheetDecls {
		if !d.Important {
			out = append(out, d)
		}
	}
	// 2. Inline normal
	for _, d := range inlineDecls {
		if !d.Important {
			out = append(out, d)
		}
	}
	// 3. Stylesheet !important
	for _, d := range sheetDecls {
		if d.Important {
			out = append(out, d)
		}
	}
	// 4. Inline !important
	for _, d := range inlineDecls {
		if d.Important {
			out = append(out, d)
		}
	}
	return out
}

func compileCSSAnimsForElem(sheet *Stylesheet, ss *stylesheetImpl, sv SceneView, e ElemID, docOrder *int) ([]*animTrack, bool, []Diag) {
	decls := collectElementCSSDecls(sheet, sv, e)
	if len(decls) == 0 {
		return nil, false, nil
	}

	var animDecls cssAnimDeclSet
	for _, d := range decls {
		animDecls.applyDecl(d)
	}
	if len(animDecls.names) == 0 {
		return nil, false, nil
	}

	var tracks []*animTrack
	var diags []Diag
	seenCSS := false

	cycle := func(list []string, i int, def string) string {
		if len(list) == 0 {
			return def
		}
		v := strings.TrimSpace(list[i%len(list)])
		if v == "" {
			return def
		}
		return v
	}

	for i, rawName := range animDecls.names {
		name := strings.TrimSpace(rawName)
		if len(name) >= 2 && ((name[0] == '"' && name[len(name)-1] == '"') || (name[0] == '\'' && name[len(name)-1] == '\'')) {
			name = name[1 : len(name)-1]
		}
		if name == "" || strings.EqualFold(name, "none") {
			continue
		}
		seenCSS = true

		kfRule := ss.keyframes[name]
		if kfRule == nil {
			diags = append(diags, Diag{Elem: e, Message: "unknown CSS @keyframes: " + name})
			continue
		}

		// Parse shared timing parameters for this CSS animation instance.
		durStr := cycle(animDecls.durations, i, "0s")
		dur, err := parseCSSTime(durStr)
		if err != nil || dur < 0 {
			diags = append(diags, Diag{Elem: e, Message: "invalid animation-duration: " + durStr})
			continue
		}

		delayStr := cycle(animDecls.delays, i, "0s")
		delay, err := parseCSSTime(delayStr)
		if err != nil {
			diags = append(diags, Diag{Elem: e, Message: "invalid animation-delay: " + delayStr})
			continue
		}

		timingStr := cycle(animDecls.timings, i, "ease")
		defaultEase, err := parseCSSEasing(timingStr)
		if err != nil {
			diags = append(diags, Diag{Elem: e, Message: "unsupported animation-timing-function: " + timingStr})
			continue
		}

		iterStr := strings.ToLower(cycle(animDecls.iterations, i, "1"))
		var repeatCount float64
		if iterStr == "infinite" {
			repeatCount = math.Inf(1)
		} else {
			rc, err := strconv.ParseFloat(iterStr, 64)
			if err != nil || math.IsNaN(rc) || math.IsInf(rc, 0) || rc < 0 {
				diags = append(diags, Diag{Elem: e, Message: "invalid animation-iteration-count: " + iterStr})
				continue
			}
			repeatCount = rc
		}

		dir := dirNormal
		switch strings.ToLower(cycle(animDecls.directions, i, "normal")) {
		case "normal":
			dir = dirNormal
		case "reverse":
			dir = dirReverse
		case "alternate":
			dir = dirAlternate
		case "alternate-reverse":
			dir = dirAlternateReverse
		}

		fillBackwards, fillForwards := false, false
		switch strings.ToLower(cycle(animDecls.fillModes, i, "none")) {
		case "forwards":
			fillForwards = true
		case "backwards":
			fillBackwards = true
		case "both":
			fillBackwards = true
			fillForwards = true
		}

		paused := strings.EqualFold(cycle(animDecls.playStates, i, "running"), "paused")

		tm := &trackTiming{
			begins:        []beginSpec{{kind: beginOffset, offset: delay}},
			simpleDur:     dur,
			repeatCount:   repeatCount,
			repeatDur:     math.NaN(),
			minDur:        math.NaN(),
			maxDur:        math.NaN(),
			direction:     dir,
			fillBackwards: fillBackwards,
			fillForwards:  fillForwards,
			paused:        paused,
			isCSS:         true,
		}
		tm.resolveCSSTiming()

		// Compile one track per animated property present in kfRule.
		propTracks, propDiags := compileKeyframesForElement(e, kfRule, tm, defaultEase, docOrder)
		diags = append(diags, propDiags...)
		tracks = append(tracks, propTracks...)
	}

	return tracks, seenCSS, diags
}

func compileKeyframesForElement(e ElemID, kfRule *cssKeyframesRule, tm *trackTiming, defaultEase easing, docOrder *int) ([]*animTrack, []Diag) {
	var diags []Diag

	// Collect all animatable properties mentioned across stops in deterministic order.
	var propOrder []PropID
	propSeen := make(map[PropID]bool)
	stopEase := make([]easing, len(kfRule.stops))

	for si, st := range kfRule.stops {
		stopEase[si] = defaultEase
		for _, d := range st.decls {
			if d.Name == "animation-timing-function" || d.Name == "-webkit-animation-timing-function" {
				if es, err := parseCSSEasing(d.Value); err == nil {
					stopEase[si] = es
				} else {
					diags = append(diags, Diag{Elem: e, Message: "unsupported keyframe timing-function: " + d.Value})
				}
				continue
			}
			p := propByNameImpl(d.Name)
			if p == PropNone {
				continue
			}
			if !propSeen[p] {
				propSeen[p] = true
				propOrder = append(propOrder, p)
			}
		}
	}

	var tracks []*animTrack
	for _, p := range propOrder {
		var kfs []keyframe
		validTrack := true
		for si, st := range kfRule.stops {
			for _, d := range st.decls {
				if propByNameImpl(d.Name) != p {
					continue
				}
				val, err := parseValueImpl(p, d.Value, SyntaxCSS)
				if err != nil {
					diags = append(diags, Diag{
						Elem:    e,
						Message: "invalid CSS keyframe value for " + d.Name + ": " + err.Error(),
					})
					validTrack = false
					break
				}
				kfs = append(kfs, keyframe{
					off:  st.offset,
					v:    val,
					ease: stopEase[si],
				})
			}
			if !validTrack {
				break
			}
		}
		if !validTrack || len(kfs) == 0 {
			continue
		}

		// Synthesize missing 0% and/or 100% keyframes from base.
		if kfs[0].off > 1e-9 {
			kfs = append([]keyframe{{off: 0, fromBase: true, ease: defaultEase}}, kfs...)
		}
		if kfs[len(kfs)-1].off < 1.0-1e-9 {
			kfs = append(kfs, keyframe{off: 1, fromBase: true, ease: defaultEase})
		}

		cm := calcLinear
		if isDiscreteProp(p) {
			cm = calcDiscrete
		}

		tracks = append(tracks, &animTrack{
			elem:     e,
			prop:     p,
			origin:   originCSS,
			docOrder: *docOrder,
			timing:   tm,
			kf:       kfs,
			calc:     cm,
			form:     formValues,
		})
		*docOrder++
	}

	return tracks, diags
}
