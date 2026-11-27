// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/ironpark/ggfx/internal/rasterx"
	"github.com/ironpark/ggfx/internal/svganim"
)

type patchBits uint64

const (
	pbFill patchBits = 1 << iota
	pbFillInherit
	pbStroke
	pbStrokeInherit
	pbColor
	pbOpacity
	pbFillOpacity
	pbStrokeOpacity
	pbStrokeWidth
	pbDash
	pbDashOffset
	pbLineCap
	pbLeadLineCap
	pbLineGap
	pbLineJoin
	pbMiterLimit
	pbFillRule
	pbClipRule
	pbDisplay
	pbVisibility
	pbClipPath
	pbMask
	pbMarkerStart
	pbMarkerMid
	pbMarkerEnd
	pbTransform
	pbTransformOrigin
	pbTransformBox
)

type transformBoxKind uint8

const (
	tbViewBox transformBoxKind = iota
	tbFillBox
	tbStrokeBox
)

type stylePatch struct {
	bits patchBits

	fill        paint
	stroke      paint
	opacity     float64
	strokeWidth float64

	nonZeroFill bool
	nonZeroClip bool
	displayNone bool
	hidden      bool

	// extra holds less frequently set properties behind a pointer so common
	// elements (plain fills/strokes or empty groups) pay minimal memory.
	extra *stylePatchExtra
}

type stylePatchExtra struct {
	fillGradID    string
	strokeGradID  string
	color         color.Color
	fillOpacity   float64
	strokeOpacity float64

	dash        []float64
	dashOffset  float64
	lineCap     rasterx.CapFunc
	leadLineCap rasterx.CapFunc
	lineGap     rasterx.GapFunc
	lineJoin    rasterx.JoinMode
	miterLimit  float64

	clipID  string
	maskID  string
	markers [3]string

	xform       rasterx.Matrix2D
	xformVal    svganim.Value
	hasXformVal bool
	xformHasPct bool
	origin      [2]svganim.OriginAxis
	tbox        transformBoxKind
}

func (p *stylePatch) ensureExtra() *stylePatchExtra {
	if p.extra == nil {
		p.extra = &stylePatchExtra{}
	}
	return p.extra
}

func resolveElementStylePatch(sc *scene, eid svganim.ElemID, fallbackClasses map[string][]svganim.Decl) {
	e := &sc.elems[eid]
	var p stylePatch

	var styleAttr string
	// 1. Presentation attributes (lowest priority in cascade).
	for _, attr := range e.attrs {
		k := strings.ToLower(attr.Name.Local)
		switch k {
		case "style":
			styleAttr = attr.Value
		case "pathlength":
			if pl, err := parseFloat(attr.Value, 64); err == nil && pl > 0 && !math.IsNaN(pl) && !math.IsInf(pl, 0) {
				e.pathLen = pl
				sc.hasStaticSceneOnly = true
			}
		case "class", "d", "points", "id", "viewbox", "x", "y", "width", "height",
			"cx", "cy", "r", "rx", "ry", "x1", "y1", "x2", "y2", "href", "offset",
			"gradienttransform", "gradientunits", "spreadmethod",
			"refx", "refy", "markerwidth", "markerheight", "orient", "markerunits",
			"clippathunits", "maskunits", "maskcontentunits", "xmlns":
		default:
			applyStyleDecl(&p, sc, k, strings.TrimSpace(attr.Value), false)
		}
	}

	// 2. Stylesheet rules (normal declarations first, !important saved for step 4).
	var cssImportant []svganim.Decl
	if sc.stylesheet != nil {
		for _, d := range sc.stylesheet.DeclsFor(sc, eid) {
			if d.Important {
				cssImportant = append(cssImportant, d)
				sc.hasStaticSceneOnly = true
				continue
			}
			applyStyleDecl(&p, sc, strings.ToLower(d.Name), strings.TrimSpace(d.Value), true)
		}
	} else if len(e.classes) > 0 && len(fallbackClasses) > 0 {
		for _, cls := range e.classes {
			for _, d := range fallbackClasses[cls] {
				if d.Important {
					cssImportant = append(cssImportant, d)
					sc.hasStaticSceneOnly = true
					continue
				}
				applyStyleDecl(&p, sc, strings.ToLower(d.Name), strings.TrimSpace(d.Value), true)
			}
		}
	}

	// 3. Inline style="..." normal declarations.
	var inlineImportant []svganim.Decl
	if styleAttr != "" {
		for _, d := range parseInlineDecls(styleAttr) {
			k := strings.ToLower(strings.TrimSpace(d.Name))
			v := strings.TrimSpace(d.Value)
			if d.Important {
				inlineImportant = append(inlineImportant, svganim.Decl{Name: k, Value: v, Important: true})
				sc.hasStaticSceneOnly = true
				continue
			}
			applyStyleDecl(&p, sc, k, v, true)
		}
	}

	// 4. Stylesheet !important declarations.
	for _, d := range cssImportant {
		applyStyleDecl(&p, sc, strings.ToLower(d.Name), strings.TrimSpace(d.Value), true)
	}

	// 5. Inline !important declarations (highest static priority).
	for _, d := range inlineImportant {
		applyStyleDecl(&p, sc, d.Name, d.Value, true)
	}

	// Track whether static scene-only features are active.
	if (p.bits&pbDisplay != 0 && p.displayNone) ||
		(p.bits&pbVisibility != 0 && p.hidden) ||
		(p.extra != nil && ((p.bits&pbTransformOrigin != 0 && (p.extra.origin[0].Val != 0 || p.extra.origin[1].Val != 0)) ||
			(p.bits&pbTransformBox != 0 && p.extra.tbox != tbViewBox) ||
			p.extra.xformHasPct)) {
		sc.hasStaticSceneOnly = true
	}

	if p.bits != 0 {
		cp := p
		e.spec = &cp
	}
}

func parseInlineDecls(raw string) []svganim.Decl {
	if decls := svganim.ParseDecls(raw); decls != nil {
		return decls
	}
	cleaned := stripCSSComments(raw)
	var out []svganim.Decl
	for decl := range strings.SplitSeq(cleaned, ";") {
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		v, imp := stripImportant(strings.TrimSpace(v))
		out = append(out, svganim.Decl{Name: k, Value: v, Important: imp})
	}
	return out
}

func stripImportant(v string) (string, bool) {
	lv := strings.ToLower(strings.TrimSpace(v))
	if strings.HasSuffix(lv, "!important") {
		rest := strings.TrimSpace(v[:len(v)-len("!important")])
		return rest, true
	}
	// Handle whitespace between '!' and 'important', e.g. "! important".
	if idx := strings.LastIndexByte(v, '!'); idx >= 0 {
		if strings.EqualFold(strings.TrimSpace(v[idx+1:]), "important") {
			return strings.TrimSpace(v[:idx]), true
		}
	}
	return v, false
}

func applyStyleDecl(p *stylePatch, sc *scene, k, v string, fromCSS bool) {
	if v == "" && k != "fill" && k != "stroke" {
		return
	}
	switch k {
	case "fill":
		lv := strings.ToLower(v)
		switch lv {
		case "inherit":
			p.bits = (p.bits &^ pbFill) | pbFillInherit
			p.fill = paint{}
			if p.extra != nil {
				p.extra.fillGradID = ""
			}
		case "currentcolor":
			p.bits = (p.bits &^ pbFillInherit) | pbFill
			p.fill = paint{current: true}
			if p.extra != nil {
				p.extra.fillGradID = ""
			}
			sc.usesCurrentColor = true
		default:
			if id := urlID(v); id != "" {
				if _, ok := sc.grads[id]; ok {
					p.bits = (p.bits &^ pbFillInherit) | pbFill
					p.fill = paint{}
					p.ensureExtra().fillGradID = id
					return
				}
			}
			if col, err := parseColorFlexible(v); err == nil {
				p.bits = (p.bits &^ pbFillInherit) | pbFill
				p.fill = paint{color: col}
				if p.extra != nil {
					p.extra.fillGradID = ""
				}
			}
		}
	case "stroke":
		lv := strings.ToLower(v)
		switch lv {
		case "inherit":
			p.bits = (p.bits &^ pbStroke) | pbStrokeInherit
			p.stroke = paint{}
			if p.extra != nil {
				p.extra.strokeGradID = ""
			}
		case "currentcolor":
			p.bits = (p.bits &^ pbStrokeInherit) | pbStroke
			p.stroke = paint{current: true}
			if p.extra != nil {
				p.extra.strokeGradID = ""
			}
			sc.usesCurrentColor = true
		default:
			if id := urlID(v); id != "" {
				if _, ok := sc.grads[id]; ok {
					p.bits = (p.bits &^ pbStrokeInherit) | pbStroke
					p.stroke = paint{}
					p.ensureExtra().strokeGradID = id
					return
				}
			}
			if col, err := parseColorFlexible(v); err == nil {
				p.bits = (p.bits &^ pbStrokeInherit) | pbStroke
				p.stroke = paint{color: col}
				if p.extra != nil {
					p.extra.strokeGradID = ""
				}
			}
		}
	case "color":
		lv := strings.ToLower(v)
		if lv == "inherit" || lv == "currentcolor" {
			p.bits &^= pbColor
			if p.extra != nil {
				p.extra.color = nil
			}
			return
		}
		if col, err := parseColorFlexible(v); err == nil && col != nil {
			p.bits |= pbColor
			p.ensureExtra().color = col
		}
	case "fill-rule":
		switch strings.ToLower(v) {
		case "nonzero":
			p.bits |= pbFillRule
			p.nonZeroFill = true
		case "evenodd":
			p.bits |= pbFillRule
			p.nonZeroFill = false
		case "inherit":
			p.bits &^= pbFillRule
		}
	case "clip-rule":
		switch strings.ToLower(v) {
		case "nonzero":
			p.bits |= pbClipRule
			p.nonZeroClip = true
		case "evenodd":
			p.bits |= pbClipRule
			p.nonZeroClip = false
		case "inherit":
			p.bits &^= pbClipRule
		}
	case "stroke-linegap":
		switch strings.ToLower(v) {
		case "flat":
			p.bits |= pbLineGap
			p.ensureExtra().lineGap = rasterx.FlatGap
		case "round":
			p.bits |= pbLineGap
			p.ensureExtra().lineGap = rasterx.RoundGap
		case "cubic":
			p.bits |= pbLineGap
			p.ensureExtra().lineGap = rasterx.CubicGap
		case "quadratic":
			p.bits |= pbLineGap
			p.ensureExtra().lineGap = rasterx.QuadraticGap
		}
	case "stroke-leadlinecap":
		if capFn := parseCapFunc(v); capFn != nil {
			p.bits |= pbLeadLineCap
			p.ensureExtra().leadLineCap = capFn
		}
	case "stroke-linecap":
		if strings.EqualFold(v, "inherit") {
			p.bits &^= pbLineCap
		} else if capFn := parseCapFunc(v); capFn != nil {
			p.bits |= pbLineCap
			p.ensureExtra().lineCap = capFn
		}
	case "stroke-linejoin":
		if strings.EqualFold(v, "inherit") {
			p.bits &^= pbLineJoin
		} else if jm, ok := parseJoinMode(v); ok {
			p.bits |= pbLineJoin
			p.ensureExtra().lineJoin = jm
		}
	case "stroke-miterlimit":
		if ml, err := parseFloat(v, 64); err == nil {
			p.bits |= pbMiterLimit
			p.ensureExtra().miterLimit = ml
		}
	case "stroke-width":
		if w, err := parseFloat(v, 64); err == nil {
			p.bits |= pbStrokeWidth
			p.strokeWidth = w
		}
	case "stroke-dashoffset":
		if off, err := parseFloat(v, 64); err == nil {
			p.bits |= pbDashOffset
			p.ensureExtra().dashOffset = off
		}
	case "stroke-dasharray":
		if strings.EqualFold(v, "inherit") {
			p.bits &^= pbDash
			return
		}
		if strings.EqualFold(v, "none") {
			p.bits |= pbDash
			p.ensureExtra().dash = nil
			return
		}
		parts := splitOnCommaOrSpace(v)
		dList := make([]float64, 0, len(parts))
		for _, dstr := range parts {
			d, err := parseFloat(strings.TrimSpace(dstr), 64)
			if err != nil {
				return
			}
			dList = append(dList, d)
		}
		p.bits |= pbDash
		p.ensureExtra().dash = dList
	case "opacity":
		if op, err := parseOpacityVal(v); err == nil {
			p.bits |= pbOpacity
			p.opacity = op
		}
	case "fill-opacity":
		if op, err := parseOpacityVal(v); err == nil {
			p.bits |= pbFillOpacity
			p.ensureExtra().fillOpacity = op
		}
	case "stroke-opacity":
		if op, err := parseOpacityVal(v); err == nil {
			p.bits |= pbStrokeOpacity
			p.ensureExtra().strokeOpacity = op
		}
	case "display":
		p.bits |= pbDisplay
		p.displayNone = strings.EqualFold(v, "none")
	case "visibility":
		switch strings.ToLower(v) {
		case "hidden", "collapse":
			p.bits |= pbVisibility
			p.hidden = true
		case "visible":
			p.bits |= pbVisibility
			p.hidden = false
		case "inherit":
			p.bits &^= pbVisibility
		}
	case "transform":
		if strings.EqualFold(v, "none") {
			p.bits |= pbTransform
			ex := p.ensureExtra()
			ex.xform = rasterx.Identity
			ex.xformVal = svganim.Value{}
			ex.hasXformVal = false
			ex.xformHasPct = false
			return
		}
		syn1, syn2 := svganim.SyntaxSVGAttr, svganim.SyntaxCSS
		if fromCSS {
			syn1, syn2 = svganim.SyntaxCSS, svganim.SyntaxSVGAttr
		}
		sv, err := svganim.ParseValue(svganim.PropTransform, v, syn1)
		if err != nil || sv.Kind != svganim.ValTransformList {
			sv, err = svganim.ParseValue(svganim.PropTransform, v, syn2)
		}
		if err == nil && sv.Kind == svganim.ValTransformList {
			p.bits |= pbTransform
			ex := p.ensureExtra()
			ex.xformVal = sv
			ex.hasXformVal = true
			ex.xformHasPct = transformHasPercent(sv)
			if ex.xformHasPct {
				// Defer %-dependent resolution until element children and transform-box are finalized.
				ex.xform = rasterx.Identity
			} else {
				ctx := svganim.EvalCtx{
					Viewport: svganim.Rect{X: 0, Y: 0, W: sc.icon.ViewBox.W, H: sc.icon.ViewBox.H},
					FontSize: 16,
				}
				ex.xform = sv.ToMatrix(&ctx)
			}
			return
		}
		if m, err := parseTransformCSSRef(v, sc.icon.ViewBox.W, sc.icon.ViewBox.H); err == nil {
			p.bits |= pbTransform
			ex := p.ensureExtra()
			ex.xform = m
			ex.xformVal = svganim.Value{}
			ex.hasXformVal = false
			ex.xformHasPct = false
		}
	case "transform-origin":
		if orig, ok := parseTransformOrigin(v); ok {
			p.bits |= pbTransformOrigin
			p.ensureExtra().origin = orig
		}
	case "transform-box":
		if tb, ok := parseTransformBox(v); ok {
			p.bits |= pbTransformBox
			p.ensureExtra().tbox = tb
		}
	case "clip-path":
		if strings.EqualFold(v, "none") {
			p.bits &^= pbClipPath
			if p.extra != nil {
				p.extra.clipID = ""
			}
		} else if id := urlID(v); id != "" {
			p.bits |= pbClipPath
			p.ensureExtra().clipID = id
		}
	case "mask":
		if strings.EqualFold(v, "none") {
			p.bits &^= pbMask
			if p.extra != nil {
				p.extra.maskID = ""
			}
		} else if id := urlID(v); id != "" {
			p.bits |= pbMask
			p.ensureExtra().maskID = id
		}
	case "marker-start":
		p.bits |= pbMarkerStart
		p.ensureExtra().markers[0] = urlID(v)
	case "marker-mid":
		p.bits |= pbMarkerMid
		p.ensureExtra().markers[1] = urlID(v)
	case "marker-end":
		p.bits |= pbMarkerEnd
		p.ensureExtra().markers[2] = urlID(v)
	case "marker":
		id := urlID(v)
		p.bits |= pbMarkerStart | pbMarkerMid | pbMarkerEnd
		p.ensureExtra().markers = [3]string{id, id, id}
	}
}

func parseCapFunc(v string) rasterx.CapFunc {
	switch strings.ToLower(v) {
	case "butt":
		return rasterx.ButtCap
	case "round":
		return rasterx.RoundCap
	case "square":
		return rasterx.SquareCap
	case "cubic":
		return rasterx.CubicCap
	case "quadratic":
		return rasterx.QuadraticCap
	default:
		return nil
	}
}

func parseJoinMode(v string) (rasterx.JoinMode, bool) {
	switch strings.ToLower(v) {
	case "miter":
		return rasterx.Miter, true
	case "miter-clip":
		return rasterx.MiterClip, true
	case "arc-clip":
		return rasterx.ArcClip, true
	case "round":
		return rasterx.Round, true
	case "arc":
		return rasterx.Arc, true
	case "bevel":
		return rasterx.Bevel, true
	default:
		return 0, false
	}
}

func transformHasPercent(v svganim.Value) bool {
	if v.Kind != svganim.ValTransformList {
		return false
	}
	for _, fn := range v.Xf {
		if fn.Op == svganim.TfTranslate {
			if fn.Units[0] == svganim.UnitPct || fn.Units[1] == svganim.UnitPct {
				return true
			}
		}
	}
	return false
}

func parseOpacityVal(v string) (float64, error) {
	v = strings.TrimSpace(v)
	if before, ok := strings.CutSuffix(v, "%"); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, errParamMismatch
		}
		return f / 100, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errParamMismatch
	}
	return f, nil
}

func parseColorFlexible(v string) (color.Color, error) {
	// Try svganim.ParseColor first (supports full CSS Color 4 once implemented).
	if c, kind, err := svganim.ParseColor(v); err == nil {
		switch kind {
		case svganim.PaintNone:
			return nil, nil
		case svganim.PaintColor:
			return rgba4ToNRGBA(c), nil
		}
	}
	lv := strings.ToLower(strings.TrimSpace(v))
	if lv == "transparent" {
		return color.NRGBA{0, 0, 0, 0}, nil
	}
	return ParseSVGColor(v)
}

// parseTransformCSSRef parses SVG/CSS 2D transform lists including angle/px/% units
// and 3D translation/scale (ignoring z), resolving % lengths against (refW, refH).
func parseTransformCSSRef(v string, refW, refH float64) (rasterx.Matrix2D, error) {
	m := rasterx.Identity
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "none") {
		return m, nil
	}
	for len(v) > 0 {
		openIdx := strings.IndexByte(v, '(')
		if openIdx < 0 {
			return m, errParamMismatch
		}
		closeIdx := strings.IndexByte(v[openIdx+1:], ')')
		if closeIdx < 0 {
			return m, errParamMismatch
		}
		fnName := strings.ToLower(strings.TrimSpace(v[:openIdx]))
		argsStr := strings.TrimSpace(v[openIdx+1 : openIdx+1+closeIdx])
		v = strings.TrimSpace(v[openIdx+1+closeIdx+1:])
		if argsStr == "" {
			return m, errParamMismatch
		}
		args := splitOnCommaOrSpace(argsStr)
		switch fnName {
		case "translate":
			if len(args) != 1 && len(args) != 2 {
				return m, errParamMismatch
			}
			tx, err := parseLengthRef(args[0], refW)
			if err != nil {
				return m, err
			}
			var ty float64
			if len(args) == 2 {
				ty, err = parseLengthRef(args[1], refH)
				if err != nil {
					return m, err
				}
			}
			m = m.Translate(tx, ty)
		case "translatex":
			if len(args) != 1 {
				return m, errParamMismatch
			}
			tx, err := parseLengthRef(args[0], refW)
			if err != nil {
				return m, err
			}
			m = m.Translate(tx, 0)
		case "translatey":
			if len(args) != 1 {
				return m, errParamMismatch
			}
			ty, err := parseLengthRef(args[0], refH)
			if err != nil {
				return m, err
			}
			m = m.Translate(0, ty)
		case "translate3d":
			if len(args) != 3 {
				return m, errParamMismatch
			}
			tx, err := parseLengthRef(args[0], refW)
			if err != nil {
				return m, err
			}
			ty, err := parseLengthRef(args[1], refH)
			if err != nil {
				return m, err
			}
			if _, err := parseLengthRef(args[2], 0); err != nil {
				return m, err
			}
			m = m.Translate(tx, ty)
		case "scale":
			if len(args) != 1 && len(args) != 2 {
				return m, errParamMismatch
			}
			sx, err := parseScaleArg(args[0])
			if err != nil {
				return m, err
			}
			sy := sx
			if len(args) == 2 {
				sy, err = parseScaleArg(args[1])
				if err != nil {
					return m, err
				}
			}
			m = m.Scale(sx, sy)
		case "scalex":
			if len(args) != 1 {
				return m, errParamMismatch
			}
			sx, err := parseScaleArg(args[0])
			if err != nil {
				return m, err
			}
			m = m.Scale(sx, 1)
		case "scaley":
			if len(args) != 1 {
				return m, errParamMismatch
			}
			sy, err := parseScaleArg(args[0])
			if err != nil {
				return m, err
			}
			m = m.Scale(1, sy)
		case "scale3d":
			if len(args) != 3 {
				return m, errParamMismatch
			}
			sx, err := parseScaleArg(args[0])
			if err != nil {
				return m, err
			}
			sy, err := parseScaleArg(args[1])
			if err != nil {
				return m, err
			}
			if _, err := parseScaleArg(args[2]); err != nil {
				return m, err
			}
			m = m.Scale(sx, sy)
		case "rotate", "rotatez":
			if len(args) != 1 && len(args) != 3 {
				return m, errParamMismatch
			}
			rad, err := parseAngleRad(args[0])
			if err != nil {
				return m, err
			}
			if len(args) == 1 {
				m = m.Rotate(rad)
			} else {
				cx, err1 := parseLengthRef(args[1], refW)
				cy, err2 := parseLengthRef(args[2], refH)
				if err1 != nil || err2 != nil {
					return m, errParamMismatch
				}
				m = m.Translate(cx, cy).Rotate(rad).Translate(-cx, -cy)
			}
		case "skewx":
			if len(args) != 1 {
				return m, errParamMismatch
			}
			rad, err := parseAngleRad(args[0])
			if err != nil {
				return m, err
			}
			m = m.SkewX(rad)
		case "skewy":
			if len(args) != 1 {
				return m, errParamMismatch
			}
			rad, err := parseAngleRad(args[0])
			if err != nil {
				return m, err
			}
			m = m.SkewY(rad)
		case "skew":
			if len(args) != 1 && len(args) != 2 {
				return m, errParamMismatch
			}
			rx, err := parseAngleRad(args[0])
			if err != nil {
				return m, err
			}
			m = m.SkewX(rx)
			if len(args) == 2 {
				ry, err := parseAngleRad(args[1])
				if err != nil {
					return m, err
				}
				m = m.SkewY(ry)
			}
		case "matrix":
			if len(args) != 6 {
				return m, errParamMismatch
			}
			var vals [6]float64
			for i := range 6 {
				f, err := parseFloat(args[i], 64)
				if err != nil {
					return m, err
				}
				vals[i] = f
			}
			m = m.Mult(rasterx.Matrix2D{
				A: vals[0], B: vals[1], C: vals[2], D: vals[3], E: vals[4], F: vals[5],
			})
		default:
			return m, errParamMismatch
		}
	}
	return m, nil
}

func parseLengthRef(s string, refSize float64) (float64, error) {
	s = strings.TrimSpace(s)
	if before, ok := strings.CutSuffix(s, "%"); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, errParamMismatch
		}
		return (f / 100) * refSize, nil
	}
	ls := strings.ToLower(s)
	if strings.HasSuffix(ls, "rem") {
		f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-3]), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, errParamMismatch
		}
		return f * 16, nil
	}
	if strings.HasSuffix(ls, "em") {
		f, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-2]), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, errParamMismatch
		}
		return f * 16, nil
	}
	f, err := parseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errParamMismatch
	}
	return f, nil
}

func parseScaleArg(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if before, ok := strings.CutSuffix(s, "%"); ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, errParamMismatch
		}
		return f / 100, nil
	}
	f, err := parseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errParamMismatch
	}
	return f, nil
}

func parseAngleRad(s string) (float64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.HasSuffix(s, "deg"):
		v, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-3]), 64)
		return v * math.Pi / 180, err
	case strings.HasSuffix(s, "grad"):
		v, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-4]), 64)
		return v * math.Pi / 200, err
	case strings.HasSuffix(s, "turn"):
		v, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-4]), 64)
		return v * 2 * math.Pi, err
	case strings.HasSuffix(s, "rad"):
		return strconv.ParseFloat(strings.TrimSpace(s[:len(s)-3]), 64)
	default:
		v, err := strconv.ParseFloat(s, 64)
		return v * math.Pi / 180, err
	}
}

func parseTransformOrigin(raw string) ([2]svganim.OriginAxis, bool) {
	fields := strings.Fields(strings.ToLower(raw))
	if len(fields) == 0 {
		return [2]svganim.OriginAxis{}, false
	}
	// Ignore 3rd value (Z axis in 3D CSS transforms).
	if len(fields) > 2 {
		fields = fields[:2]
	}
	if len(fields) == 1 {
		switch fields[0] {
		case "left":
			return [2]svganim.OriginAxis{{Val: 0, Unit: svganim.UnitPct}, {Val: 50, Unit: svganim.UnitPct}}, true
		case "right":
			return [2]svganim.OriginAxis{{Val: 100, Unit: svganim.UnitPct}, {Val: 50, Unit: svganim.UnitPct}}, true
		case "center":
			return [2]svganim.OriginAxis{{Val: 50, Unit: svganim.UnitPct}, {Val: 50, Unit: svganim.UnitPct}}, true
		case "top":
			return [2]svganim.OriginAxis{{Val: 50, Unit: svganim.UnitPct}, {Val: 0, Unit: svganim.UnitPct}}, true
		case "bottom":
			return [2]svganim.OriginAxis{{Val: 50, Unit: svganim.UnitPct}, {Val: 100, Unit: svganim.UnitPct}}, true
		default:
			ax, ok := parseOriginAxis(fields[0])
			if !ok {
				return [2]svganim.OriginAxis{}, false
			}
			return [2]svganim.OriginAxis{ax, {Val: 50, Unit: svganim.UnitPct}}, true
		}
	}
	f0, f1 := fields[0], fields[1]
	// Reorder if first is vertical keyword or second is horizontal keyword.
	if f0 == "top" || f0 == "bottom" || f1 == "left" || f1 == "right" {
		f0, f1 = f1, f0
	}
	x, okX := parseOriginAxisX(f0)
	y, okY := parseOriginAxisY(f1)
	if !okX || !okY {
		return [2]svganim.OriginAxis{}, false
	}
	return [2]svganim.OriginAxis{x, y}, true
}

func parseOriginAxisX(tok string) (svganim.OriginAxis, bool) {
	switch tok {
	case "left":
		return svganim.OriginAxis{Val: 0, Unit: svganim.UnitPct}, true
	case "center":
		return svganim.OriginAxis{Val: 50, Unit: svganim.UnitPct}, true
	case "right":
		return svganim.OriginAxis{Val: 100, Unit: svganim.UnitPct}, true
	default:
		return parseOriginAxis(tok)
	}
}

func parseOriginAxisY(tok string) (svganim.OriginAxis, bool) {
	switch tok {
	case "top":
		return svganim.OriginAxis{Val: 0, Unit: svganim.UnitPct}, true
	case "center":
		return svganim.OriginAxis{Val: 50, Unit: svganim.UnitPct}, true
	case "bottom":
		return svganim.OriginAxis{Val: 100, Unit: svganim.UnitPct}, true
	default:
		return parseOriginAxis(tok)
	}
}

func parseOriginAxis(tok string) (svganim.OriginAxis, bool) {
	if before, ok := strings.CutSuffix(tok, "%"); ok {
		v, err := strconv.ParseFloat(before, 64)
		if err != nil {
			return svganim.OriginAxis{}, false
		}
		return svganim.OriginAxis{Val: v, Unit: svganim.UnitPct}, true
	}
	v, err := parseFloat(tok, 64)
	if err != nil {
		return svganim.OriginAxis{}, false
	}
	return svganim.OriginAxis{Val: v, Unit: svganim.UnitPx}, true
}

func parseTransformBox(raw string) (transformBoxKind, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "view-box":
		return tbViewBox, true
	case "fill-box", "content-box":
		return tbFillBox, true
	case "stroke-box", "border-box":
		return tbStrokeBox, true
	default:
		return tbViewBox, false
	}
}

// parseBraceAwareClasses extracts simple `.class` rules from CSS text while
// safely skipping comments (/* ... */) and nested at-rules (@keyframes, @media, etc.).
// It preserves declaration order and reports whether any non-simple-class rules were seen.
func parseBraceAwareClasses(data string) (map[string][]svganim.Decl, bool) {
	cleaned := stripCSSComments(data)
	res := map[string][]svganim.Decl{}
	hasAdvanced := false

	i := 0
	n := len(cleaned)
	for i < n {
		// Skip whitespace
		for i < n && (cleaned[i] == ' ' || cleaned[i] == '\t' || cleaned[i] == '\n' || cleaned[i] == '\r') {
			i++
		}
		if i >= n {
			break
		}
		// At-rule (@keyframes, @media, @import, etc.)
		if cleaned[i] == '@' {
			// Scan until ';' or matching '{ ... }'
			for i < n && cleaned[i] != '{' && cleaned[i] != ';' {
				i++
			}
			if i < n && cleaned[i] == ';' {
				i++
				continue
			}
			if i < n && cleaned[i] == '{' {
				depth := 1
				i++
				for i < n && depth > 0 {
					if cleaned[i] == '{' {
						depth++
					} else if cleaned[i] == '}' {
						depth--
					}
					i++
				}
			}
			continue
		}
		// Regular rule: selector { declarations }
		selStart := i
		for i < n && cleaned[i] != '{' && cleaned[i] != '}' {
			i++
		}
		if i >= n || cleaned[i] != '{' {
			break
		}
		selStr := strings.TrimSpace(cleaned[selStart:i])
		i++ // skip '{'
		bodyStart := i
		depth := 1
		for i < n && depth > 0 {
			if cleaned[i] == '{' {
				depth++
			} else if cleaned[i] == '}' {
				depth--
			}
			if depth > 0 {
				i++
			}
		}
		bodyStr := cleaned[bodyStart:i]
		if i < n && cleaned[i] == '}' {
			i++
		}

		var decls []svganim.Decl
		for rawDecl := range strings.SplitSeq(bodyStr, ";") {
			rawDecl = strings.TrimSpace(rawDecl)
			if rawDecl == "" {
				continue
			}
			k, v, ok := strings.Cut(rawDecl, ":")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v, imp := stripImportant(strings.TrimSpace(v))
			if imp {
				hasAdvanced = true
			}
			decls = append(decls, svganim.Decl{Name: k, Value: v, Important: imp})
		}
		if len(decls) == 0 {
			continue
		}

		for sel := range strings.SplitSeq(selStr, ",") {
			sel = strings.TrimSpace(sel)
			if sel == "" {
				continue
			}
			// Only match pure single ".class" selectors in the legacy/fallback map.
			if strings.HasPrefix(sel, ".") && !strings.ContainsAny(sel[1:], " .#>:+~[()") {
				cls := sel[1:]
				res[cls] = append(res[cls], decls...)
			} else {
				hasAdvanced = true
			}
		}
	}
	return res, hasAdvanced
}

func stripCSSComments(s string) string {
	if !strings.Contains(s, "/*") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				break
			}
			i += 2 + end + 2
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func rgba4ToNRGBA(c [4]float64) color.NRGBA {
	clampU8 := func(f float64) uint8 {
		if f <= 0 {
			return 0
		}
		if f >= 1 {
			return 255
		}
		return uint8(math.Round(f * 255))
	}
	return color.NRGBA{
		R: clampU8(c[0]),
		G: clampU8(c[1]),
		B: clampU8(c[2]),
		A: clampU8(c[3]),
	}
}

func colorToRGBA4(c color.Color) [4]float64 {
	n := toNRGBA(c)
	return [4]float64{
		float64(n.R) / 255,
		float64(n.G) / 255,
		float64(n.B) / 255,
		float64(n.A) / 255,
	}
}
