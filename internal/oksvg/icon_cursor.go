// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"encoding/xml"
	"errors"
	"math"
	"strings"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// defFrame tracks an element with an id being recorded inside <defs>.
type defFrame struct {
	id    string
	depth int
	defs  []definition
}

// iconCursor is used while parsing SVG files.
type iconCursor struct {
	pathCursor
	icon                        *SvgIcon
	StyleStack                  []PathStyle
	grad                        *rasterx.Gradient
	inGrad, inDefs, inDefsStyle bool
	defStack                    []defFrame
	defsDepth                   int
	useExpansions               int
	capture                     *captured // the clip, mask or marker being read
	captureDepth                int
}

// ReadGradURL reads an SVG format gradient url
// Since the context of the gradient can affect the colors
// the current fill or line color is passed in and used in
// the case of a nil stopClor value
func (c *iconCursor) ReadGradURL(v string, defaultColor paint) (grad *rasterx.Gradient, ok bool) {
	if id := urlID(v); id != "" {
		var g *rasterx.Gradient
		g, ok = c.icon.Grads[id]
		if ok {
			grad = localizeGradIfStopClrNil(g, defaultColor)
		}
	}
	return
}

// ReadGradAttr reads an SVG gradient attribute
func (c *iconCursor) ReadGradAttr(attr xml.Attr) (err error) {
	switch attr.Name.Local {
	case "gradientTransform":
		c.grad.Matrix, err = c.parseTransformFrom(rasterx.Identity, attr.Value)
	case "gradientUnits":
		switch strings.TrimSpace(attr.Value) {
		case "userSpaceOnUse":
			c.grad.Units = rasterx.UserSpaceOnUse
		case "objectBoundingBox":
			c.grad.Units = rasterx.ObjectBoundingBox
		}
	case "spreadMethod":
		switch strings.TrimSpace(attr.Value) {
		case "pad":
			c.grad.Spread = rasterx.PadSpread
		case "reflect":
			c.grad.Spread = rasterx.ReflectSpread
		case "repeat":
			c.grad.Spread = rasterx.RepeatSpread
		}
	}
	return
}

// PushStyle parses the style element, and push it on the style stack. Only color and opacity are supported
// for fill. Note that this parses both the contents of a style attribute plus
// direct fill and opacity attributes.
func (c *iconCursor) PushStyle(attrs []xml.Attr) error {
	// Make a copy of the top style
	prevClip := c.StyleStack[len(c.StyleStack)-1].clip
	curStyle := c.StyleStack[len(c.StyleStack)-1]

	var styleAttr string
	var className string
	// Apply in CSS/SVG cascade order: presentation attributes < CSS classes < inline style.
	for _, attr := range attrs {
		k := strings.ToLower(attr.Name.Local)
		switch k {
		case "style":
			styleAttr = attr.Value
		case "class":
			className = attr.Value
		case "d", "points", "id", "viewbox", "x", "y", "width", "height",
			"cx", "cy", "r", "rx", "ry", "x1", "y1", "x2", "y2", "href", "offset":
			// Non-style geometry/structural attributes; skip without copying.
		default:
			if err := c.readStyleAttr(&curStyle, k, strings.TrimSpace(attr.Value)); err != nil {
				return err
			}
		}
	}
	if err := c.adaptClasses(&curStyle, className); err != nil {
		return err
	}
	if styleAttr != "" {
		for _, d := range parseInlineDecls(styleAttr) {
			k := strings.ToLower(strings.TrimSpace(d.Name))
			v := strings.TrimSpace(d.Value)
			if err := c.readStyleAttr(&curStyle, k, v); err != nil {
				return err
			}
		}
	}
	// Any clip or mask named here is placed in the element's user space, its
	// own transform read with the rest of its style.
	for u := curStyle.clip; u != nil && u != prevClip; u = u.parent {
		if err := c.compileClip(u, curStyle.mAdder.M); err != nil {
			return err
		}
	}
	c.StyleStack = append(c.StyleStack, curStyle) // Push style onto stack
	return nil
}

func (c *iconCursor) readTransformAttr(m1 rasterx.Matrix2D, k string) (rasterx.Matrix2D, error) {
	ln := len(c.points)
	switch k {
	case "rotate":
		if ln == 1 {
			m1 = m1.Rotate(c.points[0] * math.Pi / 180)
		} else if ln == 3 {
			m1 = m1.Translate(c.points[1], c.points[2]).
				Rotate(c.points[0]*math.Pi/180).
				Translate(-c.points[1], -c.points[2])
		} else {
			return m1, errParamMismatch
		}
	case "translate":
		if ln == 1 {
			m1 = m1.Translate(c.points[0], 0)
		} else if ln == 2 {
			m1 = m1.Translate(c.points[0], c.points[1])
		} else {
			return m1, errParamMismatch
		}
	case "skewx":
		if ln == 1 {
			m1 = m1.SkewX(c.points[0] * math.Pi / 180)
		} else {
			return m1, errParamMismatch
		}
	case "skewy":
		if ln == 1 {
			m1 = m1.SkewY(c.points[0] * math.Pi / 180)
		} else {
			return m1, errParamMismatch
		}
	case "scale":
		if ln == 1 {
			m1 = m1.Scale(c.points[0], c.points[0])
		} else if ln == 2 {
			m1 = m1.Scale(c.points[0], c.points[1])
		} else {
			return m1, errParamMismatch
		}
	case "matrix":
		if ln == 6 {
			m1 = m1.Mult(rasterx.Matrix2D{
				A: c.points[0],
				B: c.points[1],
				C: c.points[2],
				D: c.points[3],
				E: c.points[4],
				F: c.points[5]})
		} else {
			return m1, errParamMismatch
		}
	default:
		return m1, errParamMismatch
	}
	return m1, nil
}

func (c *iconCursor) parseTransformFrom(m1 rasterx.Matrix2D, v string) (rasterx.Matrix2D, error) {
	ts := strings.Split(v, ")")
	for _, t := range ts {
		t = strings.TrimSpace(t)
		if len(t) == 0 {
			continue
		}
		d := strings.Split(t, "(")
		if len(d) != 2 || len(d[1]) < 1 {
			return m1, errParamMismatch // badly formed transformation
		}
		err := c.GetPoints(d[1])
		if err != nil {
			return m1, err
		}
		m1, err = c.readTransformAttr(m1, strings.ToLower(strings.TrimSpace(d[0])))
		if err != nil {
			return m1, err
		}
	}
	return m1, nil
}

func (c *iconCursor) readStyleAttr(curStyle *PathStyle, k, v string) error {
	switch k {
	case "fill":
		if strings.EqualFold(v, "currentcolor") {
			curStyle.fillerColor = paint{current: true}
			c.icon.usesCurrentColor = true
			return nil
		}
		if strings.EqualFold(v, "inherit") {
			return nil
		}
		gradient, ok := c.ReadGradURL(v, curStyle.fillerColor)
		if ok {
			curStyle.fillerColor = paint{grad: gradient, gradID: urlID(v)}
			break
		}
		col, err := parseColorFlexible(v)
		if err != nil {
			return err
		}
		if col != nil {
			curStyle.fillerColor = paint{color: toNRGBA(col)}
		} else {
			curStyle.fillerColor = paint{}
		}
		return nil
	case "stroke":
		if strings.EqualFold(v, "currentcolor") {
			curStyle.linerColor = paint{current: true}
			c.icon.usesCurrentColor = true
			return nil
		}
		if strings.EqualFold(v, "inherit") {
			return nil
		}
		gradient, ok := c.ReadGradURL(v, curStyle.linerColor)
		if ok {
			curStyle.linerColor = paint{grad: gradient, gradID: urlID(v)}
			break
		}
		col, errc := parseColorFlexible(v)
		if errc != nil {
			return errc
		}
		if col != nil {
			curStyle.linerColor = paint{color: toNRGBA(col)}
		} else {
			curStyle.linerColor = paint{}
		}
	case "color":
		lv := strings.ToLower(v)
		if lv != "inherit" && lv != "currentcolor" {
			if col, err := parseColorFlexible(v); err == nil && col != nil {
				curStyle.color = col
			}
		}
	case "fill-rule", "clip-rule":
		var nonZero bool
		switch v {
		case "nonzero":
			nonZero = true
		case "evenodd":
		default: // inherit, or what is not a rule
			return nil
		}
		if k == "fill-rule" {
			curStyle.UseNonZeroWinding = nonZero
		} else {
			curStyle.nonZeroClip = nonZero
		}
	case "stroke-linegap":
		switch v {
		case "flat":
			curStyle.LineGap = rasterx.FlatGap
		case "round":
			curStyle.LineGap = rasterx.RoundGap
		case "cubic":
			curStyle.LineGap = rasterx.CubicGap
		case "quadratic":
			curStyle.LineGap = rasterx.QuadraticGap
		}
	case "stroke-leadlinecap":
		switch v {
		case "butt":
			curStyle.LeadLineCap = rasterx.ButtCap
		case "round":
			curStyle.LeadLineCap = rasterx.RoundCap
		case "square":
			curStyle.LeadLineCap = rasterx.SquareCap
		case "cubic":
			curStyle.LeadLineCap = rasterx.CubicCap
		case "quadratic":
			curStyle.LeadLineCap = rasterx.QuadraticCap
		}
	case "stroke-linecap":
		switch v {
		case "butt":
			curStyle.LineCap = rasterx.ButtCap
		case "round":
			curStyle.LineCap = rasterx.RoundCap
		case "square":
			curStyle.LineCap = rasterx.SquareCap
		case "cubic":
			curStyle.LineCap = rasterx.CubicCap
		case "quadratic":
			curStyle.LineCap = rasterx.QuadraticCap
		}
	case "stroke-linejoin":
		switch v {
		case "miter":
			curStyle.LineJoin = rasterx.Miter
		case "miter-clip":
			curStyle.LineJoin = rasterx.MiterClip
		case "arc-clip":
			curStyle.LineJoin = rasterx.ArcClip
		case "round":
			curStyle.LineJoin = rasterx.Round
		case "arc":
			curStyle.LineJoin = rasterx.Arc
		case "bevel":
			curStyle.LineJoin = rasterx.Bevel
		}
	case "stroke-miterlimit":
		mLimit, err := parseFloat(v, 64)
		if err != nil {
			return err
		}
		curStyle.MiterLimit = mLimit
	case "stroke-width":
		width, err := parseFloat(v, 64)
		if err != nil {
			return err
		}
		curStyle.LineWidth = width
	case "stroke-dashoffset":
		dashOffset, err := parseFloat(v, 64)
		if err != nil {
			return err
		}
		curStyle.DashOffset = dashOffset
	case "stroke-dasharray":
		if v == "none" {
			curStyle.Dash = nil
			break
		}
		dashes := splitOnCommaOrSpace(v)
		dList := make([]float64, len(dashes))
		for i, dstr := range dashes {
			d, err := parseFloat(strings.TrimSpace(dstr), 64)
			if err != nil {
				return err
			}
			dList[i] = d
		}
		curStyle.Dash = dList
	case "opacity", "stroke-opacity", "fill-opacity":
		op, err := parseOpacityVal(v)
		if err != nil {
			return err
		}
		if k != "stroke-opacity" {
			curStyle.FillOpacity *= op
		}
		if k != "fill-opacity" {
			curStyle.LineOpacity *= op
		}
	case "transform":
		m, err := c.parseTransformFrom(curStyle.mAdder.M, v)
		if err != nil {
			// Fallback to CSS transform parser (supports deg/rad/grad/turn/px/%/3d/none).
			if lm, err2 := parseTransformCSSRef(v, c.icon.ViewBox.W, c.icon.ViewBox.H); err2 == nil {
				curStyle.mAdder.M = curStyle.mAdder.M.Mult(lm)
				return nil
			}
			return err
		}
		curStyle.mAdder.M = m
	case "clip-path", "mask":
		if def := c.icon.clips[urlID(v)]; def != nil && def.mask == (k == "mask") {
			curStyle.clip = &clipUse{def: def, parent: curStyle.clip}
		}
	case "marker-start":
		curStyle.markers[0] = urlID(v)
	case "marker-mid":
		curStyle.markers[1] = urlID(v)
	case "marker-end":
		curStyle.markers[2] = urlID(v)
	case "marker":
		id := urlID(v)
		curStyle.markers = [3]string{id, id, id}
	}
	return nil
}

func (c *iconCursor) readStartElement(se xml.StartElement) error {
	if c.capturing(se) {
		return nil
	}
	if se.Name.Local == "style" {
		c.inDefsStyle = true
		return nil
	}
	var skipDef bool
	if isGradient(se.Name.Local) || c.inGrad {
		skipDef = true
	}
	if c.inDefs && !skipDef {
		var ID string
		for _, attr := range se.Attr {
			if attr.Name.Local == "id" {
				ID = attr.Value
			}
		}
		d := definition{
			ID:    ID,
			Tag:   se.Name.Local,
			Attrs: se.Attr,
		}
		for i := range c.defStack {
			c.defStack[i].defs = append(c.defStack[i].defs, d)
		}
		if ID != "" {
			c.defStack = append(c.defStack, defFrame{
				id:    ID,
				depth: c.defsDepth,
				defs:  []definition{d},
			})
		}
		c.defsDepth++
		return nil
	}
	df, ok := drawFuncs[se.Name.Local]
	if !ok {
		return errors.New("Cannot process svg element " + se.Name.Local)
	}
	// An element that fails to draw is skipped, not an error, as the
	// original had it: it built an error there in strict mode but then
	// discarded it.
	_ = df(c, se.Attr)
	c.addPath(se.Name.Local)
	return nil
}

// addPath keeps the path the element tag drew, if it drew one, under the
// style on top of the stack, and the markers the style puts on it.
func (c *iconCursor) addPath(tag string) {
	if len(c.Path) == 0 {
		return
	}
	pathCopy := make(rasterx.Path, len(c.Path))
	copy(pathCopy, c.Path)
	style := c.StyleStack[len(c.StyleStack)-1]
	// If an explicit CSS color was set on this element or an ancestor, resolve
	// currentColor statically for the legacy path so root CurrentColor doesn't override it.
	if style.color != nil {
		if style.fillerColor.current {
			style.fillerColor = paint{color: toNRGBA(style.color)}
		}
		if style.linerColor.current {
			style.linerColor = paint{color: toNRGBA(style.color)}
		}
	}
	c.icon.SVGPaths = append(c.icon.SVGPaths, SvgPath{style, pathCopy})
	c.Path = c.Path[:0]
	c.addMarkers(tag, pathCopy, style)
}

func (c *iconCursor) adaptClasses(pathStyle *PathStyle, className string) error {
	if className == "" || len(c.icon.classes) == 0 {
		return nil
	}
	for _, cls := range strings.Fields(className) {
		for _, d := range c.icon.classes[cls] {
			if err := c.readStyleAttr(pathStyle, strings.ToLower(d.Name), d.Value); err != nil {
				return err
			}
		}
	}
	return nil
}
