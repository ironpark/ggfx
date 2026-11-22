// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/colornames"

	"github.com/ironpark/ggfx/internal/rasterx"
)

const defaultFontSize = 16.0

func propByNameImpl(name string) PropID {
	s := strings.TrimSpace(name)
	if s == "" {
		return PropNone
	}
	// Exact match first, then lowercase fallback.
	if p := lookupPropName(s); p != PropNone {
		return p
	}
	return lookupPropName(strings.ToLower(s))
}

func lookupPropName(s string) PropID {
	switch s {
	case "fill":
		return PropFill
	case "stroke":
		return PropStroke
	case "color":
		return PropColor
	case "opacity":
		return PropOpacity
	case "fill-opacity":
		return PropFillOpacity
	case "stroke-opacity":
		return PropStrokeOpacity
	case "stroke-width":
		return PropStrokeWidth
	case "stroke-dasharray":
		return PropStrokeDasharray
	case "stroke-dashoffset":
		return PropStrokeDashoffset
	case "stroke-linecap":
		return PropStrokeLinecap
	case "stroke-linejoin":
		return PropStrokeLinejoin
	case "stroke-miterlimit":
		return PropStrokeMiterlimit
	case "fill-rule":
		return PropFillRule
	case "clip-rule":
		return PropClipRule
	case "visibility":
		return PropVisibility
	case "display":
		return PropDisplay
	case "transform":
		return PropTransform
	case "transform-origin":
		return PropTransformOrigin
	case "transform-box":
		return PropTransformBox
	case "x":
		return PropX
	case "y":
		return PropY
	case "width":
		return PropWidth
	case "height":
		return PropHeight
	case "rx":
		return PropRx
	case "ry":
		return PropRy
	case "r":
		return PropR
	case "cx":
		return PropCx
	case "cy":
		return PropCy
	case "x1":
		return PropX1
	case "y1":
		return PropY1
	case "x2":
		return PropX2
	case "y2":
		return PropY2
	case "points":
		return PropPoints
	case "d":
		return PropD
	default:
		return PropNone
	}
}

func isDiscreteProp(p PropID) bool {
	switch p {
	case PropStrokeLinecap, PropStrokeLinejoin, PropFillRule, PropClipRule,
		PropVisibility, PropDisplay, PropTransformBox, PropD:
		return true
	default:
		return false
	}
}

func parseColorImpl(raw string) ([4]float64, PaintKind, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return [4]float64{}, PaintNone, errors.New("empty color")
	}
	low := strings.ToLower(s)
	switch low {
	case "none":
		return [4]float64{}, PaintNone, nil
	case "inherit":
		return [4]float64{}, PaintInherit, nil
	case "currentcolor":
		return [4]float64{}, PaintCurrentColor, nil
	case "transparent":
		return [4]float64{0, 0, 0, 0}, PaintColor, nil
	}

	if strings.HasPrefix(low, "url(") {
		if _, ok := extractURLID(s); ok {
			return [4]float64{}, PaintURL, nil
		}
		return [4]float64{}, PaintNone, errors.New("invalid url() paint: " + raw)
	}

	if s[0] == '#' {
		col, err := parseHexColor(s[1:])
		if err != nil {
			return [4]float64{}, PaintNone, err
		}
		return col, PaintColor, nil
	}

	if strings.HasPrefix(low, "rgb(") || strings.HasPrefix(low, "rgba(") {
		col, err := parseRGBFunc(s)
		if err != nil {
			return [4]float64{}, PaintNone, err
		}
		return col, PaintColor, nil
	}

	if strings.HasPrefix(low, "hsl(") || strings.HasPrefix(low, "hsla(") {
		col, err := parseHSLFunc(s)
		if err != nil {
			return [4]float64{}, PaintNone, err
		}
		return col, PaintColor, nil
	}

	if c, ok := lookupNamedColor(low); ok {
		return c, PaintColor, nil
	}

	return [4]float64{}, PaintNone, errors.New("unknown color: " + raw)
}

func lookupNamedColor(low string) ([4]float64, bool) {
	if c, ok := colornames.Map[low]; ok {
		return [4]float64{
			float64(c.R) / 255.0,
			float64(c.G) / 255.0,
			float64(c.B) / 255.0,
			float64(c.A) / 255.0,
		}, true
	}
	if low == "rebeccapurple" {
		return [4]float64{0x66 / 255.0, 0x33 / 255.0, 0x99 / 255.0, 1.0}, true
	}
	return [4]float64{}, false
}

func parseHexColor(hex string) ([4]float64, error) {
	hexDigit := func(b byte) (byte, bool) {
		switch {
		case b >= '0' && b <= '9':
			return b - '0', true
		case b >= 'a' && b <= 'f':
			return b - 'a' + 10, true
		case b >= 'A' && b <= 'F':
			return b - 'A' + 10, true
		default:
			return 0, false
		}
	}
	hexByte := func(hi, lo byte) (float64, bool) {
		h, ok1 := hexDigit(hi)
		l, ok2 := hexDigit(lo)
		if !ok1 || !ok2 {
			return 0, false
		}
		return float64(h*16+l) / 255.0, true
	}

	switch len(hex) {
	case 3:
		r, ok1 := hexByte(hex[0], hex[0])
		g, ok2 := hexByte(hex[1], hex[1])
		b, ok3 := hexByte(hex[2], hex[2])
		if !ok1 || !ok2 || !ok3 {
			return [4]float64{}, errors.New("invalid #rgb color")
		}
		return [4]float64{r, g, b, 1}, nil
	case 4:
		r, ok1 := hexByte(hex[0], hex[0])
		g, ok2 := hexByte(hex[1], hex[1])
		b, ok3 := hexByte(hex[2], hex[2])
		a, ok4 := hexByte(hex[3], hex[3])
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return [4]float64{}, errors.New("invalid #rgba color")
		}
		return [4]float64{r, g, b, a}, nil
	case 6:
		r, ok1 := hexByte(hex[0], hex[1])
		g, ok2 := hexByte(hex[2], hex[3])
		b, ok3 := hexByte(hex[4], hex[5])
		if !ok1 || !ok2 || !ok3 {
			return [4]float64{}, errors.New("invalid #rrggbb color")
		}
		return [4]float64{r, g, b, 1}, nil
	case 8:
		r, ok1 := hexByte(hex[0], hex[1])
		g, ok2 := hexByte(hex[2], hex[3])
		b, ok3 := hexByte(hex[4], hex[5])
		a, ok4 := hexByte(hex[6], hex[7])
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return [4]float64{}, errors.New("invalid #rrggbbaa color")
		}
		return [4]float64{r, g, b, a}, nil
	default:
		return [4]float64{}, errors.New("invalid hex color length")
	}
}

func splitColorFuncArgs(raw string) ([]string, string, error) {
	open := strings.IndexByte(raw, '(')
	if open < 0 || !strings.HasSuffix(raw, ")") {
		return nil, "", errors.New("malformed color function: " + raw)
	}
	inner := strings.TrimSpace(raw[open+1 : len(raw)-1])
	if inner == "" {
		return nil, "", errors.New("empty color function")
	}
	if strings.Contains(inner, ",") {
		if strings.Contains(inner, "/") {
			return nil, "", errors.New("cannot mix comma and slash in color function")
		}
		parts := strings.Split(inner, ",")
		if len(parts) != 3 && len(parts) != 4 {
			return nil, "", errors.New("color function requires 3 or 4 args")
		}
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if len(parts) == 4 {
			return parts[:3], parts[3], nil
		}
		return parts[:3], "", nil
	}

	// Space / slash CSS Color 4 syntax
	var alpha string
	channels := inner
	if before, after, ok := strings.Cut(inner, "/"); ok {
		if strings.Contains(after, "/") {
			return nil, "", errors.New("multiple slashes in color function")
		}
		channels = strings.TrimSpace(before)
		alpha = strings.TrimSpace(after)
		if alpha == "" {
			return nil, "", errors.New("missing alpha after slash")
		}
	}
	fields := strings.Fields(channels)
	if len(fields) != 3 {
		return nil, "", errors.New("color function requires 3 channels")
	}
	return fields, alpha, nil
}

func parseRGBChannel(s string) (float64, error) {
	if before, ok := strings.CutSuffix(s, "%"); ok {
		v, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, errors.New("invalid rgb percentage")
		}
		return clamp01(v / 100.0), nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("invalid rgb channel")
	}
	return clamp01(v / 255.0), nil
}

func parseAlphaValue(s string) (float64, error) {
	if s == "" {
		return 1.0, nil
	}
	if before, ok := strings.CutSuffix(s, "%"); ok {
		v, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, errors.New("invalid alpha percentage")
		}
		return clamp01(v / 100.0), nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("invalid alpha value")
	}
	return clamp01(v), nil
}

func parseRGBFunc(raw string) ([4]float64, error) {
	ch, alphaStr, err := splitColorFuncArgs(raw)
	if err != nil {
		return [4]float64{}, err
	}
	r, err := parseRGBChannel(ch[0])
	if err != nil {
		return [4]float64{}, err
	}
	g, err := parseRGBChannel(ch[1])
	if err != nil {
		return [4]float64{}, err
	}
	b, err := parseRGBChannel(ch[2])
	if err != nil {
		return [4]float64{}, err
	}
	a, err := parseAlphaValue(alphaStr)
	if err != nil {
		return [4]float64{}, err
	}
	return [4]float64{r, g, b, a}, nil
}

func parseHSLFunc(raw string) ([4]float64, error) {
	ch, alphaStr, err := splitColorFuncArgs(raw)
	if err != nil {
		return [4]float64{}, err
	}
	hDeg, err := parseAngleDeg(ch[0], true)
	if err != nil {
		return [4]float64{}, err
	}
	sVal, err := parsePercentOr100(ch[1])
	if err != nil {
		return [4]float64{}, err
	}
	lVal, err := parsePercentOr100(ch[2])
	if err != nil {
		return [4]float64{}, err
	}
	a, err := parseAlphaValue(alphaStr)
	if err != nil {
		return [4]float64{}, err
	}
	r, g, b := hslToRGB(hDeg, sVal, lVal)
	return [4]float64{r, g, b, a}, nil
}

func parsePercentOr100(s string) (float64, error) {
	if before, ok := strings.CutSuffix(s, "%"); ok {
		v, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, errors.New("invalid percentage")
		}
		return clamp01(v / 100.0), nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("invalid hsl component")
	}
	return clamp01(v / 100.0), nil
}

func hslToRGB(hDeg, s, l float64) (float64, float64, float64) {
	h := math.Mod(hDeg, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r1, g1, b1 float64
	switch {
	case h < 60:
		r1, g1, b1 = c, x, 0
	case h < 120:
		r1, g1, b1 = x, c, 0
	case h < 180:
		r1, g1, b1 = 0, c, x
	case h < 240:
		r1, g1, b1 = 0, x, c
	case h < 300:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	return clamp01(r1 + m), clamp01(g1 + m), clamp01(b1 + m)
}

func extractURLID(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	low := strings.ToLower(s)
	if !strings.HasPrefix(low, "url(") {
		return "", false
	}
	closeIdx := strings.IndexByte(s, ')')
	if closeIdx < 0 {
		return "", false
	}
	inner := strings.TrimSpace(s[4:closeIdx])
	if len(inner) >= 2 && ((inner[0] == '"' && inner[len(inner)-1] == '"') || (inner[0] == '\'' && inner[len(inner)-1] == '\'')) {
		inner = strings.TrimSpace(inner[1 : len(inner)-1])
	}
	inner = strings.TrimPrefix(inner, "#")
	if inner == "" {
		return "", false
	}
	return inner, true
}

func parseValueImpl(p PropID, raw string, syn Syntax) (Value, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Value{}, errors.New("empty value")
	}

	switch p {
	case PropFill, PropStroke:
		return parsePaintValue(s)

	case PropColor:
		col, kind, err := parseColorImpl(s)
		if err != nil {
			return Value{}, err
		}
		switch kind {
		case PaintColor:
			return Value{Kind: ValColor, Color: col, Paint: PaintColor, Raw: s}, nil
		case PaintCurrentColor:
			return Value{Kind: ValColor, Paint: PaintCurrentColor, Raw: s}, nil
		case PaintInherit:
			return Value{Kind: ValKeyword, Keyword: "inherit", Raw: s}, nil
		default:
			return Value{}, errors.New("invalid color property value: " + s)
		}

	case PropOpacity, PropFillOpacity, PropStrokeOpacity:
		v, err := parseOpacity(s)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: ValNumber, Num: v, Raw: s}, nil

	case PropStrokeMiterlimit:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return Value{}, errors.New("invalid stroke-miterlimit: " + s)
		}
		return Value{Kind: ValNumber, Num: v, Raw: s}, nil

	case PropStrokeWidth, PropStrokeDashoffset,
		PropX, PropY, PropWidth, PropHeight,
		PropRx, PropRy, PropR, PropCx, PropCy,
		PropX1, PropY1, PropX2, PropY2:
		num, unit, err := parseLength(s)
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: ValLength, Num: num, Unit: unit, Raw: s}, nil

	case PropStrokeDasharray:
		return parseDasharray(s)

	case PropStrokeLinecap:
		low := strings.ToLower(s)
		switch low {
		case "butt", "round", "square", "inherit":
			return Value{Kind: ValKeyword, Keyword: low, Raw: s}, nil
		default:
			return Value{}, errors.New("invalid stroke-linecap: " + s)
		}

	case PropStrokeLinejoin:
		low := strings.ToLower(s)
		switch low {
		case "miter", "round", "bevel", "inherit":
			return Value{Kind: ValKeyword, Keyword: low, Raw: s}, nil
		default:
			return Value{}, errors.New("invalid stroke-linejoin: " + s)
		}

	case PropFillRule, PropClipRule:
		low := strings.ToLower(s)
		switch low {
		case "nonzero", "evenodd", "inherit":
			return Value{Kind: ValKeyword, Keyword: low, Raw: s}, nil
		default:
			return Value{}, errors.New("invalid rule: " + s)
		}

	case PropVisibility:
		low := strings.ToLower(s)
		switch low {
		case "visible", "hidden", "collapse", "inherit":
			return Value{Kind: ValKeyword, Keyword: low, Raw: s}, nil
		default:
			return Value{}, errors.New("invalid visibility: " + s)
		}

	case PropDisplay:
		low := strings.ToLower(s)
		switch low {
		case "inline", "block", "list-item", "run-in", "compact", "marker",
			"table", "inline-table", "table-row-group", "table-header-group",
			"table-footer-group", "table-row", "table-column-group", "table-column",
			"table-cell", "table-caption", "none", "inherit", "initial", "unset",
			"inline-block", "flex", "inline-flex", "grid", "inline-grid", "contents":
			return Value{Kind: ValKeyword, Keyword: low, Raw: s}, nil
		default:
			return Value{}, errors.New("invalid display: " + s)
		}

	case PropTransformBox:
		low := strings.ToLower(s)
		switch low {
		case "content-box", "border-box", "fill-box", "stroke-box", "view-box":
			return Value{Kind: ValKeyword, Keyword: low, Raw: s}, nil
		default:
			return Value{}, errors.New("invalid transform-box: " + s)
		}

	case PropTransformOrigin:
		return parseTransformOrigin(s)

	case PropTransform:
		return parseTransformList(s, syn)

	case PropPoints:
		return parsePoints(s)

	case PropD:
		return Value{Kind: ValPathData, Raw: s}, nil

	default:
		return Value{}, errors.New("unsupported property")
	}
}

func parsePaintValue(s string) (Value, error) {
	col, kind, err := parseColorImpl(s)
	if err != nil {
		return Value{}, err
	}
	v := Value{Kind: ValPaint, Color: col, Paint: kind, Raw: s}
	if kind == PaintURL {
		id, ok := extractURLID(s)
		if !ok {
			return Value{}, errors.New("invalid url paint: " + s)
		}
		v.URL = id
	}
	return v, nil
}

func parseOpacity(s string) (float64, error) {
	if before, ok := strings.CutSuffix(s, "%"); ok {
		v, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, errors.New("invalid opacity: " + s)
		}
		return v / 100.0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("invalid opacity: " + s)
	}
	return v, nil
}

// parseLength parses a numeric length with optional unit.
// Per spec §2.2 (q4) & §3, legacy suffixes cm/mm/pt are stripped without conversion.
func parseLength(s string) (float64, UnitKind, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, UnitNone, errors.New("empty length")
	}
	low := strings.ToLower(s)
	var numStr string
	var unit UnitKind
	switch {
	case strings.HasSuffix(low, "%"):
		numStr, unit = s[:len(s)-1], UnitPct
	case strings.HasSuffix(low, "rem"):
		numStr, unit = s[:len(s)-3], UnitRem
	case strings.HasSuffix(low, "em"):
		numStr, unit = s[:len(s)-2], UnitEm
	case strings.HasSuffix(low, "px"):
		numStr, unit = s[:len(s)-2], UnitPx
	case strings.HasSuffix(low, "cm"), strings.HasSuffix(low, "mm"), strings.HasSuffix(low, "pt"):
		numStr, unit = s[:len(s)-2], UnitNone
	default:
		numStr, unit = s, UnitNone
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, UnitNone, errors.New("invalid length: " + s)
	}
	return v, unit, nil
}

func parseDasharray(s string) (Value, error) {
	low := strings.ToLower(strings.TrimSpace(s))
	if low == "none" || low == "inherit" {
		return Value{Kind: ValKeyword, Keyword: low, Raw: s}, nil
	}
	parts := splitSpaceOrComma(s)
	if len(parts) == 0 {
		return Value{Kind: ValKeyword, Keyword: "none", Raw: s}, nil
	}
	list := make([]float64, 0, len(parts))
	for _, p := range parts {
		n, unit, err := parseLength(p)
		if err != nil || n < 0 {
			return Value{}, errors.New("invalid stroke-dasharray value: " + p)
		}
		if unit == UnitPct || unit == UnitEm || unit == UnitRem {
			// Accept percentages/em as numeric values only if unitless/px/legacy;
			// wait: if % appears, treat as unsupported or numeric? Let's reject unsupported % in dasharray?
			// Actually keeping numeric value or resolving isn't possible without per-entry unit;
			// return error for %/em/rem so lenient animation drops cleanly, or accept px/none.
			return Value{}, errors.New("unsupported unit in stroke-dasharray: " + p)
		}
		list = append(list, n)
	}
	return Value{Kind: ValNumberList, List: list, Raw: s}, nil
}

func parsePoints(s string) (Value, error) {
	parts := splitSpaceOrComma(s)
	if len(parts) == 0 {
		return Value{Kind: ValNumberList, List: nil, Raw: s}, nil
	}
	list := make([]float64, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.ParseFloat(p, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return Value{}, errors.New("invalid point coordinate: " + p)
		}
		list = append(list, n)
	}
	return Value{Kind: ValNumberList, List: list, Raw: s}, nil
}

func parseTransformOrigin(s string) (Value, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 || len(fields) > 3 {
		return Value{}, errors.New("invalid transform-origin: " + s)
	}
	// 3rd value (z-offset) is ignored if it's a valid length.
	if len(fields) == 3 {
		if _, _, err := parseLength(fields[2]); err != nil {
			return Value{}, errors.New("invalid transform-origin z-offset: " + fields[2])
		}
		fields = fields[:2]
	}

	type axisTag uint8
	const (
		axisAny axisTag = iota
		axisX
		axisY
	)
	parseTok := func(tok string) (OriginAxis, axisTag, error) {
		switch strings.ToLower(tok) {
		case "left":
			return OriginAxis{Val: 0, Unit: UnitPct}, axisX, nil
		case "right":
			return OriginAxis{Val: 100, Unit: UnitPct}, axisX, nil
		case "top":
			return OriginAxis{Val: 0, Unit: UnitPct}, axisY, nil
		case "bottom":
			return OriginAxis{Val: 100, Unit: UnitPct}, axisY, nil
		case "center":
			return OriginAxis{Val: 50, Unit: UnitPct}, axisAny, nil
		default:
			v, u, err := parseLength(tok)
			if err != nil {
				return OriginAxis{}, axisAny, err
			}
			return OriginAxis{Val: v, Unit: u}, axisAny, nil
		}
	}

	if len(fields) == 1 {
		ax, tag, err := parseTok(fields[0])
		if err != nil {
			return Value{}, err
		}
		center := OriginAxis{Val: 50, Unit: UnitPct}
		if tag == axisY {
			return Value{Kind: ValOrigin, Origin: [2]OriginAxis{center, ax}, Raw: s}, nil
		}
		return Value{Kind: ValOrigin, Origin: [2]OriginAxis{ax, center}, Raw: s}, nil
	}

	a0, t0, err := parseTok(fields[0])
	if err != nil {
		return Value{}, err
	}
	a1, t1, err := parseTok(fields[1])
	if err != nil {
		return Value{}, err
	}
	if (t0 == axisX && t1 == axisX) || (t0 == axisY && t1 == axisY) {
		return Value{}, errors.New("conflicting transform-origin keywords: " + s)
	}
	if t0 == axisY || t1 == axisX {
		// Swapped order like "top left" or "top 20px" or "30% left"
		return Value{Kind: ValOrigin, Origin: [2]OriginAxis{a1, a0}, Raw: s}, nil
	}
	return Value{Kind: ValOrigin, Origin: [2]OriginAxis{a0, a1}, Raw: s}, nil
}

func parseAngle(s string, allowUnitless bool) (float64, UnitKind, error) {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	var numStr string
	var unit UnitKind
	switch {
	case strings.HasSuffix(low, "grad"):
		numStr, unit = s[:len(s)-4], UnitGrad
	case strings.HasSuffix(low, "turn"):
		numStr, unit = s[:len(s)-4], UnitTurn
	case strings.HasSuffix(low, "deg"):
		numStr, unit = s[:len(s)-3], UnitDeg
	case strings.HasSuffix(low, "rad"):
		numStr, unit = s[:len(s)-3], UnitRad
	default:
		numStr, unit = s, UnitNone
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, UnitNone, errors.New("invalid angle: " + s)
	}
	if unit == UnitNone {
		if !allowUnitless && v != 0 {
			return 0, UnitNone, errors.New("angle requires unit in CSS: " + s)
		}
		unit = UnitDeg
	}
	return v, unit, nil
}

func parseAngleDeg(s string, allowUnitless bool) (float64, error) {
	v, u, err := parseAngle(s, allowUnitless)
	if err != nil {
		return 0, err
	}
	return angleToDeg(v, u), nil
}

func angleToRad(v float64, u UnitKind) float64 {
	switch u {
	case UnitRad:
		return v
	case UnitGrad:
		return v * math.Pi / 200.0
	case UnitTurn:
		return v * 2 * math.Pi
	default: // UnitDeg, UnitNone
		return v * math.Pi / 180.0
	}
}

func angleToDeg(v float64, u UnitKind) float64 {
	switch u {
	case UnitRad:
		return v * 180.0 / math.Pi
	case UnitGrad:
		return v * 0.9
	case UnitTurn:
		return v * 360.0
	default:
		return v
	}
}

func parseTransformList(raw string, syn Syntax) (Value, error) {
	s := strings.TrimSpace(raw)
	if strings.EqualFold(s, "none") {
		return Value{Kind: ValTransformList, Xf: nil, Raw: s}, nil
	}
	var fns []TransformFn
	i := 0
	n := len(s)
	for i < n {
		// Skip whitespace and optional commas (SVG allows commas between transform functions).
		for i < n && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || (syn == SyntaxSVGAttr && s[i] == ',')) {
			i++
		}
		if i >= n {
			break
		}
		nameStart := i
		for i < n && s[i] != '(' && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			i++
		}
		fnName := strings.ToLower(s[nameStart:i])
		// SVG allows optional whitespace before '(', CSS does not (but being strict on missing '(' is key).
		if syn == SyntaxSVGAttr {
			for i < n && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
				i++
			}
		}
		if i >= n || s[i] != '(' || fnName == "" {
			return Value{}, errors.New("malformed transform: " + raw)
		}
		i++ // skip '('
		argStart := i
		depth := 1
		for i < n && depth > 0 {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth > 0 {
				i++
			}
		}
		if depth != 0 {
			return Value{}, errors.New("unclosed transform function: " + raw)
		}
		argStr := strings.TrimSpace(s[argStart:i])
		i++ // skip ')'

		fn, err := parseTransformFn(fnName, argStr, syn)
		if err != nil {
			return Value{}, err
		}
		fns = append(fns, fn)
	}
	if len(fns) == 0 {
		return Value{}, errors.New("empty transform list")
	}
	return Value{Kind: ValTransformList, Xf: fns, Raw: s}, nil
}

func splitTransformArgs(argStr string, syn Syntax) ([]string, error) {
	if argStr == "" {
		return nil, errors.New("empty transform arguments")
	}
	if syn == SyntaxCSS {
		parts := strings.Split(argStr, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				return nil, errors.New("empty argument in CSS transform")
			}
			// Reject whitespace-separated multiple arguments without commas in CSS.
			if strings.ContainsAny(p, " \t\n\r") {
				return nil, errors.New("CSS transform arguments must be comma-separated")
			}
			out = append(out, p)
		}
		return out, nil
	}
	return splitSpaceOrComma(argStr), nil
}

func parseTransformFn(name, argStr string, syn Syntax) (TransformFn, error) {
	args, err := splitTransformArgs(argStr, syn)
	if err != nil {
		return TransformFn{}, err
	}
	allowUnitlessAngle := syn == SyntaxSVGAttr

	switch name {
	case "translate":
		if len(args) != 1 && len(args) != 2 {
			return TransformFn{}, errors.New("translate requires 1 or 2 args")
		}
		tx, ux, err := parseLength(args[0])
		if err != nil {
			return TransformFn{}, err
		}
		var ty float64
		var uy UnitKind
		if len(args) == 2 {
			ty, uy, err = parseLength(args[1])
			if err != nil {
				return TransformFn{}, err
			}
		}
		return TransformFn{
			Op:    TfTranslate,
			Args:  [6]float64{tx, ty},
			Units: [6]UnitKind{ux, uy},
			N:     2,
		}, nil

	case "translatex":
		if syn == SyntaxSVGAttr || len(args) != 1 {
			return TransformFn{}, errors.New("translateX requires 1 arg in CSS")
		}
		tx, ux, err := parseLength(args[0])
		if err != nil {
			return TransformFn{}, err
		}
		return TransformFn{
			Op:    TfTranslate,
			Args:  [6]float64{tx, 0},
			Units: [6]UnitKind{ux, UnitNone},
			N:     2,
		}, nil

	case "translatey":
		if syn == SyntaxSVGAttr || len(args) != 1 {
			return TransformFn{}, errors.New("translateY requires 1 arg in CSS")
		}
		ty, uy, err := parseLength(args[0])
		if err != nil {
			return TransformFn{}, err
		}
		return TransformFn{
			Op:    TfTranslate,
			Args:  [6]float64{0, ty},
			Units: [6]UnitKind{UnitNone, uy},
			N:     2,
		}, nil

	case "translate3d":
		if syn == SyntaxSVGAttr || len(args) != 3 {
			return TransformFn{}, errors.New("translate3d requires 3 args in CSS")
		}
		tx, ux, err := parseLength(args[0])
		if err != nil {
			return TransformFn{}, err
		}
		ty, uy, err := parseLength(args[1])
		if err != nil {
			return TransformFn{}, err
		}
		if _, _, err := parseLength(args[2]); err != nil {
			return TransformFn{}, err
		}
		return TransformFn{
			Op:    TfTranslate,
			Args:  [6]float64{tx, ty},
			Units: [6]UnitKind{ux, uy},
			N:     2,
		}, nil

	case "scale":
		if len(args) != 1 && len(args) != 2 {
			return TransformFn{}, errors.New("scale requires 1 or 2 args")
		}
		sx, err := parseScaleNum(args[0])
		if err != nil {
			return TransformFn{}, err
		}
		sy := sx
		if len(args) == 2 {
			sy, err = parseScaleNum(args[1])
			if err != nil {
				return TransformFn{}, err
			}
		}
		return TransformFn{
			Op:   TfScale,
			Args: [6]float64{sx, sy},
			N:    2,
		}, nil

	case "scalex":
		if syn == SyntaxSVGAttr || len(args) != 1 {
			return TransformFn{}, errors.New("scaleX requires 1 arg in CSS")
		}
		sx, err := parseScaleNum(args[0])
		if err != nil {
			return TransformFn{}, err
		}
		return TransformFn{Op: TfScale, Args: [6]float64{sx, 1}, N: 2}, nil

	case "scaley":
		if syn == SyntaxSVGAttr || len(args) != 1 {
			return TransformFn{}, errors.New("scaleY requires 1 arg in CSS")
		}
		sy, err := parseScaleNum(args[0])
		if err != nil {
			return TransformFn{}, err
		}
		return TransformFn{Op: TfScale, Args: [6]float64{1, sy}, N: 2}, nil

	case "scale3d":
		if syn == SyntaxSVGAttr || len(args) != 3 {
			return TransformFn{}, errors.New("scale3d requires 3 args in CSS")
		}
		sx, err := parseScaleNum(args[0])
		if err != nil {
			return TransformFn{}, err
		}
		sy, err := parseScaleNum(args[1])
		if err != nil {
			return TransformFn{}, err
		}
		if _, err := parseScaleNum(args[2]); err != nil {
			return TransformFn{}, err
		}
		return TransformFn{Op: TfScale, Args: [6]float64{sx, sy}, N: 2}, nil

	case "rotate":
		if syn == SyntaxCSS {
			if len(args) != 1 {
				return TransformFn{}, errors.New("CSS rotate requires 1 arg")
			}
			a, u, err := parseAngle(args[0], false)
			if err != nil {
				return TransformFn{}, err
			}
			return TransformFn{Op: TfRotate, Args: [6]float64{a}, Units: [6]UnitKind{u}, N: 1}, nil
		}
		if len(args) != 1 && len(args) != 3 {
			return TransformFn{}, errors.New("SVG rotate requires 1 or 3 args")
		}
		a, u, err := parseAngle(args[0], true)
		if err != nil {
			return TransformFn{}, err
		}
		if len(args) == 1 {
			return TransformFn{Op: TfRotate, Args: [6]float64{a}, Units: [6]UnitKind{u}, N: 1}, nil
		}
		cx, _, err := parseLength(args[1])
		if err != nil {
			return TransformFn{}, err
		}
		cy, _, err := parseLength(args[2])
		if err != nil {
			return TransformFn{}, err
		}
		return TransformFn{Op: TfRotate, Args: [6]float64{a, cx, cy}, Units: [6]UnitKind{u}, N: 3}, nil

	case "rotatez":
		if syn == SyntaxSVGAttr || len(args) != 1 {
			return TransformFn{}, errors.New("rotateZ requires 1 arg in CSS")
		}
		a, u, err := parseAngle(args[0], false)
		if err != nil {
			return TransformFn{}, err
		}
		return TransformFn{Op: TfRotate, Args: [6]float64{a}, Units: [6]UnitKind{u}, N: 1}, nil

	case "skewx":
		if len(args) != 1 {
			return TransformFn{}, errors.New("skewX requires 1 arg")
		}
		a, u, err := parseAngle(args[0], allowUnitlessAngle)
		if err != nil {
			return TransformFn{}, err
		}
		return TransformFn{Op: TfSkewX, Args: [6]float64{a}, Units: [6]UnitKind{u}, N: 1}, nil

	case "skewy":
		if len(args) != 1 {
			return TransformFn{}, errors.New("skewY requires 1 arg")
		}
		a, u, err := parseAngle(args[0], allowUnitlessAngle)
		if err != nil {
			return TransformFn{}, err
		}
		return TransformFn{Op: TfSkewY, Args: [6]float64{a}, Units: [6]UnitKind{u}, N: 1}, nil

	case "skew":
		if syn == SyntaxSVGAttr || (len(args) != 1 && len(args) != 2) {
			return TransformFn{}, errors.New("skew requires 1 or 2 args in CSS")
		}
		ax, ux, err := parseAngle(args[0], false)
		if err != nil {
			return TransformFn{}, err
		}
		if len(args) == 1 {
			return TransformFn{Op: TfSkewX, Args: [6]float64{ax}, Units: [6]UnitKind{ux}, N: 1}, nil
		}
		ay, uy, err := parseAngle(args[1], false)
		if err != nil {
			return TransformFn{}, err
		}
		if ay == 0 {
			return TransformFn{Op: TfSkewX, Args: [6]float64{ax}, Units: [6]UnitKind{ux}, N: 1}, nil
		}
		if ax == 0 {
			return TransformFn{Op: TfSkewY, Args: [6]float64{ay}, Units: [6]UnitKind{uy}, N: 1}, nil
		}
		tx := math.Tan(angleToRad(ax, ux))
		ty := math.Tan(angleToRad(ay, uy))
		return TransformFn{
			Op:   TfMatrix,
			Args: [6]float64{1, ty, tx, 1, 0, 0},
			N:    6,
		}, nil

	case "matrix":
		if len(args) != 6 {
			return TransformFn{}, errors.New("matrix requires 6 args")
		}
		var a [6]float64
		for i, s := range args {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return TransformFn{}, errors.New("invalid matrix argument: " + s)
			}
			a[i] = v
		}
		return TransformFn{Op: TfMatrix, Args: a, N: 6}, nil

	default:
		// Includes 3D transforms: rotateX, rotateY, rotate3d, perspective, matrix3d.
		return TransformFn{}, errors.New("unsupported transform function: " + name)
	}
}

func parseScaleNum(s string) (float64, error) {
	if before, ok := strings.CutSuffix(s, "%"); ok {
		v, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, errors.New("invalid scale percentage: " + s)
		}
		return v / 100.0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("invalid scale factor: " + s)
	}
	return v, nil
}

func evalRefBox(ctx *EvalCtx) Rect {
	if ctx == nil {
		return Rect{}
	}
	if ctx.FillBox != nil {
		return ctx.FillBox()
	}
	return ctx.Viewport
}

func evalFontSize(ctx *EvalCtx) float64 {
	if ctx != nil && ctx.FontSize > 0 {
		return ctx.FontSize
	}
	return defaultFontSize
}

func evalColor(ctx *EvalCtx) [4]float64 {
	if ctx != nil {
		return ctx.Color
	}
	return [4]float64{0, 0, 0, 1}
}

func resolveAxisLength(val float64, unit UnitKind, refSize float64, ctx *EvalCtx) float64 {
	switch unit {
	case UnitPct:
		return val * 0.01 * refSize
	case UnitEm, UnitRem:
		return val * evalFontSize(ctx)
	default:
		return val
	}
}

func resolvePropLength(val float64, unit UnitKind, p PropID, ctx *EvalCtx) float64 {
	if unit == UnitNone || unit == UnitPx {
		return val
	}
	if unit == UnitEm || unit == UnitRem {
		return val * evalFontSize(ctx)
	}
	if unit == UnitPct {
		var vp Rect
		if ctx != nil {
			vp = ctx.Viewport
		}
		switch p {
		case PropX, PropWidth, PropCx, PropRx, PropX1, PropX2:
			return val * 0.01 * vp.W
		case PropY, PropHeight, PropCy, PropRy, PropY1, PropY2:
			return val * 0.01 * vp.H
		default:
			diag := math.Sqrt((vp.W*vp.W + vp.H*vp.H) * 0.5)
			return val * 0.01 * diag
		}
	}
	return val
}

func toMatrixImpl(v Value, ctx *EvalCtx) rasterx.Matrix2D {
	if v.Kind != ValTransformList || len(v.Xf) == 0 {
		return rasterx.Identity
	}
	m := rasterx.Identity
	var refBox Rect
	refBoxLoaded := false
	getRefBox := func() Rect {
		if !refBoxLoaded {
			refBox = evalRefBox(ctx)
			refBoxLoaded = true
		}
		return refBox
	}

	for _, fn := range v.Xf {
		switch fn.Op {
		case TfTranslate:
			tx := fn.Args[0]
			if fn.Units[0] != UnitNone && fn.Units[0] != UnitPx {
				tx = resolveAxisLength(tx, fn.Units[0], getRefBox().W, ctx)
			}
			var ty float64
			if fn.N >= 2 {
				ty = fn.Args[1]
				if fn.Units[1] != UnitNone && fn.Units[1] != UnitPx {
					ty = resolveAxisLength(ty, fn.Units[1], getRefBox().H, ctx)
				}
			}
			m = m.Translate(tx, ty)

		case TfScale:
			sx := fn.Args[0]
			sy := sx
			if fn.N >= 2 {
				sy = fn.Args[1]
			}
			m = m.Scale(sx, sy)

		case TfRotate:
			rad := angleToRad(fn.Args[0], fn.Units[0])
			if fn.N >= 3 {
				cx, cy := fn.Args[1], fn.Args[2]
				m = m.Translate(cx, cy).Rotate(rad).Translate(-cx, -cy)
			} else {
				m = m.Rotate(rad)
			}

		case TfSkewX:
			m = m.SkewX(angleToRad(fn.Args[0], fn.Units[0]))

		case TfSkewY:
			m = m.SkewY(angleToRad(fn.Args[0], fn.Units[0]))

		case TfMatrix:
			m = m.Mult(rasterx.Matrix2D{
				A: fn.Args[0],
				B: fn.Args[1],
				C: fn.Args[2],
				D: fn.Args[3],
				E: fn.Args[4],
				F: fn.Args[5],
			})
		}
	}
	return m
}

// interpolateValues interpolates from a to b at progress t (which may overshoot [0,1] for cubic-bezier).
func interpolateValues(a, b Value, t float64, p PropID, isCSS bool, ctx *EvalCtx) Value {
	if isDiscreteProp(p) {
		if isCSS && p == PropVisibility {
			if t <= 0 {
				return a
			}
			if t >= 1 {
				return b
			}
			if a.Keyword == "visible" || b.Keyword == "visible" {
				return Value{Kind: ValKeyword, Keyword: "visible"}
			}
		}
		if t < 0.5 {
			return a
		}
		return b
	}

	switch {
	case a.Kind == ValNumber && b.Kind == ValNumber:
		return Value{Kind: ValNumber, Num: lerp(a.Num, b.Num, t)}

	case a.Kind == ValLength && b.Kind == ValLength:
		if a.Unit == b.Unit {
			return Value{Kind: ValLength, Num: lerp(a.Num, b.Num, t), Unit: a.Unit}
		}
		va := resolvePropLength(a.Num, a.Unit, p, ctx)
		vb := resolvePropLength(b.Num, b.Unit, p, ctx)
		return Value{Kind: ValLength, Num: lerp(va, vb, t), Unit: UnitNone}

	case a.Kind == ValNumber && b.Kind == ValLength:
		vb := resolvePropLength(b.Num, b.Unit, p, ctx)
		return Value{Kind: ValLength, Num: lerp(a.Num, vb, t), Unit: UnitNone}

	case a.Kind == ValLength && b.Kind == ValNumber:
		va := resolvePropLength(a.Num, a.Unit, p, ctx)
		return Value{Kind: ValLength, Num: lerp(va, b.Num, t), Unit: UnitNone}

	case (a.Kind == ValColor || a.Kind == ValPaint) && (b.Kind == ValColor || b.Kind == ValPaint):
		ca, okA := resolveConcreteColor(a, ctx)
		cb, okB := resolveConcreteColor(b, ctx)
		if !okA || !okB {
			if t < 0.5 {
				return a
			}
			return b
		}
		c := lerpPremulSRGB(ca, cb, t)
		if a.Kind == ValPaint || b.Kind == ValPaint {
			return Value{Kind: ValPaint, Paint: PaintColor, Color: c}
		}
		return Value{Kind: ValColor, Paint: PaintColor, Color: c}

	case a.Kind == ValNumberList && b.Kind == ValNumberList:
		if p == PropStrokeDasharray {
			return interpolateDasharray(a, b, t)
		}
		if len(a.List) == len(b.List) && len(a.List) > 0 {
			out := make([]float64, len(a.List))
			for i := range out {
				out[i] = lerp(a.List[i], b.List[i], t)
			}
			return Value{Kind: ValNumberList, List: out}
		}
		if t < 0.5 {
			return a
		}
		return b

	case a.Kind == ValTransformList && b.Kind == ValTransformList:
		return interpolateTransforms(a, b, t, ctx)

	case a.Kind == ValOrigin && b.Kind == ValOrigin:
		ref := evalRefBox(ctx)
		var out [2]OriginAxis
		for axis := range 2 {
			refSz := ref.W
			if axis == 1 {
				refSz = ref.H
			}
			if a.Origin[axis].Unit == b.Origin[axis].Unit {
				out[axis] = OriginAxis{
					Val:  lerp(a.Origin[axis].Val, b.Origin[axis].Val, t),
					Unit: a.Origin[axis].Unit,
				}
			} else {
				va := resolveAxisLength(a.Origin[axis].Val, a.Origin[axis].Unit, refSz, ctx)
				vb := resolveAxisLength(b.Origin[axis].Val, b.Origin[axis].Unit, refSz, ctx)
				out[axis] = OriginAxis{Val: lerp(va, vb, t), Unit: UnitNone}
			}
		}
		return Value{Kind: ValOrigin, Origin: out}

	default:
		if t < 0.5 {
			return a
		}
		return b
	}
}

func lerp(a, b, t float64) float64 {
	return a + (b-a)*t
}

func resolveConcreteColor(v Value, ctx *EvalCtx) ([4]float64, bool) {
	if v.Kind == ValColor {
		if v.Paint == PaintCurrentColor {
			return evalColor(ctx), true
		}
		return v.Color, true
	}
	if v.Kind == ValPaint {
		switch v.Paint {
		case PaintColor:
			return v.Color, true
		case PaintCurrentColor:
			return evalColor(ctx), true
		default:
			return [4]float64{}, false
		}
	}
	return [4]float64{}, false
}

// lerpPremulSRGB interpolates two straight sRGB colors in premultiplied sRGB space,
// returning straight sRGB RGBA clamped to [0, 1].
func lerpPremulSRGB(a, b [4]float64, t float64) [4]float64 {
	p0 := [4]float64{a[0] * a[3], a[1] * a[3], a[2] * a[3], a[3]}
	p1 := [4]float64{b[0] * b[3], b[1] * b[3], b[2] * b[3], b[3]}
	pm := [4]float64{
		lerp(p0[0], p1[0], t),
		lerp(p0[1], p1[1], t),
		lerp(p0[2], p1[2], t),
		lerp(p0[3], p1[3], t),
	}
	alpha := clamp01(pm[3])
	if alpha <= 1e-12 {
		return [4]float64{0, 0, 0, 0}
	}
	return [4]float64{
		clamp01(pm[0] / alpha),
		clamp01(pm[1] / alpha),
		clamp01(pm[2] / alpha),
		alpha,
	}
}

func gcdInt(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func lcmInt(a, b int) int {
	if a == 0 || b == 0 {
		return 0
	}
	return (a / gcdInt(a, b)) * b
}

func interpolateDasharray(a, b Value, t float64) Value {
	na, nb := len(a.List), len(b.List)
	if na == 0 || nb == 0 {
		if t < 0.5 {
			return a
		}
		return b
	}
	n := lcmInt(na, nb)
	if n == 0 || n > 64 {
		if t < 0.5 {
			return a
		}
		return b
	}
	out := make([]float64, n)
	for i := range n {
		v := lerp(a.List[i%na], b.List[i%nb], t)
		if v < 0 {
			v = 0
		}
		out[i] = v
	}
	return Value{Kind: ValNumberList, List: out}
}

func identityTransformFor(fn TransformFn) TransformFn {
	switch fn.Op {
	case TfTranslate:
		return TransformFn{Op: TfTranslate, Args: [6]float64{0, 0}, Units: fn.Units, N: 2}
	case TfScale:
		return TransformFn{Op: TfScale, Args: [6]float64{1, 1}, N: 2}
	case TfRotate:
		if fn.N >= 3 {
			return TransformFn{Op: TfRotate, Args: [6]float64{0, fn.Args[1], fn.Args[2]}, Units: fn.Units, N: 3}
		}
		return TransformFn{Op: TfRotate, Args: [6]float64{0}, Units: fn.Units, N: 1}
	case TfSkewX:
		return TransformFn{Op: TfSkewX, Args: [6]float64{0}, Units: fn.Units, N: 1}
	case TfSkewY:
		return TransformFn{Op: TfSkewY, Args: [6]float64{0}, Units: fn.Units, N: 1}
	default:
		return TransformFn{Op: TfMatrix, Args: [6]float64{1, 0, 0, 1, 0, 0}, N: 6}
	}
}

func transformListsMatch(a, b []TransformFn) bool {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := range minLen {
		if a[i].Op != b[i].Op || a[i].Op == TfMatrix {
			return false
		}
	}
	// Also fall back to matrix decomposition if any padded tail function is TfMatrix.
	for i := minLen; i < len(a); i++ {
		if a[i].Op == TfMatrix {
			return false
		}
	}
	for i := minLen; i < len(b); i++ {
		if b[i].Op == TfMatrix {
			return false
		}
	}
	return true
}

func interpolateTransforms(a, b Value, t float64, ctx *EvalCtx) Value {
	if len(a.Xf) == 0 && len(b.Xf) == 0 {
		return Value{Kind: ValTransformList}
	}
	if transformListsMatch(a.Xf, b.Xf) {
		maxLen := len(a.Xf)
		if len(b.Xf) > maxLen {
			maxLen = len(b.Xf)
		}
		out := make([]TransformFn, maxLen)
		ref := evalRefBox(ctx)
		for i := range maxLen {
			var fa, fb TransformFn
			switch {
			case i < len(a.Xf) && i < len(b.Xf):
				fa, fb = a.Xf[i], b.Xf[i]
			case i < len(a.Xf):
				fa = a.Xf[i]
				fb = identityTransformFor(fa)
			default:
				fb = b.Xf[i]
				fa = identityTransformFor(fb)
			}
			out[i] = interpolateTransformFn(fa, fb, t, ref, ctx)
		}
		return Value{Kind: ValTransformList, Xf: out}
	}

	// Fallback: 2D matrix decomposition (CSS Transforms Level 1)
	ma := toMatrixImpl(a, ctx)
	mb := toMatrixImpl(b, ctx)
	m := interpolateMatrices2D(ma, mb, t)
	return Value{
		Kind: ValTransformList,
		Xf: []TransformFn{{
			Op:   TfMatrix,
			Args: [6]float64{m.A, m.B, m.C, m.D, m.E, m.F},
			N:    6,
		}},
	}
}

func interpolateTransformFn(a, b TransformFn, t float64, ref Rect, ctx *EvalCtx) TransformFn {
	switch a.Op {
	case TfTranslate:
		var ax, bx, ay, by float64
		var ux, uy UnitKind
		ax, bx = a.Args[0], b.Args[0]
		if a.Units[0] == b.Units[0] {
			ux = a.Units[0]
		} else {
			ax = resolveAxisLength(ax, a.Units[0], ref.W, ctx)
			bx = resolveAxisLength(bx, b.Units[0], ref.W, ctx)
			ux = UnitNone
		}
		if a.N >= 2 {
			ay = a.Args[1]
		}
		if b.N >= 2 {
			by = b.Args[1]
		}
		if a.Units[1] == b.Units[1] {
			uy = a.Units[1]
		} else {
			ay = resolveAxisLength(ay, a.Units[1], ref.H, ctx)
			by = resolveAxisLength(by, b.Units[1], ref.H, ctx)
			uy = UnitNone
		}
		return TransformFn{
			Op:    TfTranslate,
			Args:  [6]float64{lerp(ax, bx, t), lerp(ay, by, t)},
			Units: [6]UnitKind{ux, uy},
			N:     2,
		}

	case TfScale:
		asx, asy := a.Args[0], a.Args[0]
		if a.N >= 2 {
			asy = a.Args[1]
		}
		bsx, bsy := b.Args[0], b.Args[0]
		if b.N >= 2 {
			bsy = b.Args[1]
		}
		return TransformFn{
			Op:   TfScale,
			Args: [6]float64{lerp(asx, bsx, t), lerp(asy, bsy, t)},
			N:    2,
		}

	case TfRotate:
		aa := angleToDeg(a.Args[0], a.Units[0])
		ba := angleToDeg(b.Args[0], b.Units[0])
		ang := lerp(aa, ba, t)
		if a.N >= 3 || b.N >= 3 {
			var acx, acy, bcx, bcy float64
			if a.N >= 3 {
				acx, acy = a.Args[1], a.Args[2]
			}
			if b.N >= 3 {
				bcx, bcy = b.Args[1], b.Args[2]
			}
			return TransformFn{
				Op:    TfRotate,
				Args:  [6]float64{ang, lerp(acx, bcx, t), lerp(acy, bcy, t)},
				Units: [6]UnitKind{UnitDeg},
				N:     3,
			}
		}
		return TransformFn{
			Op:    TfRotate,
			Args:  [6]float64{ang},
			Units: [6]UnitKind{UnitDeg},
			N:     1,
		}

	case TfSkewX, TfSkewY:
		aa := angleToDeg(a.Args[0], a.Units[0])
		ba := angleToDeg(b.Args[0], b.Units[0])
		return TransformFn{
			Op:    a.Op,
			Args:  [6]float64{lerp(aa, ba, t)},
			Units: [6]UnitKind{UnitDeg},
			N:     1,
		}

	default:
		return a
	}
}

type decomp2D struct {
	tx, ty float64
	sx, sy float64
	skew   float64
	angle  float64
}

// decomposeMatrix2D implements CSS Transforms Level 1 2D matrix decomposition.
func decomposeMatrix2D(m rasterx.Matrix2D) (decomp2D, bool) {
	det := m.A*m.D - m.B*m.C
	if math.Abs(det) < 1e-12 {
		return decomp2D{}, false
	}

	a, b := m.A, m.B
	c, d := m.C, m.D

	sx := math.Hypot(a, b)
	if sx != 0 {
		a /= sx
		b /= sx
	}
	skew := a*c + b*d
	c -= a * skew
	d -= b * skew
	sy := math.Hypot(c, d)
	if sy != 0 {
		c /= sy
		d /= sy
		skew /= sy
	}
	if a*d < b*c {
		a = -a
		b = -b
		skew = -skew
		sx = -sx
	}
	angle := math.Atan2(b, a)
	return decomp2D{
		tx:    m.E,
		ty:    m.F,
		sx:    sx,
		sy:    sy,
		skew:  skew,
		angle: angle,
	}, true
}

func recomposeMatrix2D(d decomp2D) rasterx.Matrix2D {
	m := rasterx.Identity.Translate(d.tx, d.ty).Rotate(d.angle)
	if d.skew != 0 {
		m = m.Mult(rasterx.Matrix2D{A: 1, B: 0, C: d.skew, D: 1, E: 0, F: 0})
	}
	return m.Scale(d.sx, d.sy)
}

func interpolateMatrices2D(a, b rasterx.Matrix2D, t float64) rasterx.Matrix2D {
	da, okA := decomposeMatrix2D(a)
	db, okB := decomposeMatrix2D(b)
	if !okA || !okB {
		if t < 0.5 {
			return a
		}
		return b
	}
	diff := db.angle - da.angle
	for diff > math.Pi {
		diff -= 2 * math.Pi
	}
	for diff < -math.Pi {
		diff += 2 * math.Pi
	}
	return recomposeMatrix2D(decomp2D{
		tx:    lerp(da.tx, db.tx, t),
		ty:    lerp(da.ty, db.ty, t),
		sx:    lerp(da.sx, db.sx, t),
		sy:    lerp(da.sy, db.sy, t),
		skew:  lerp(da.skew, db.skew, t),
		angle: da.angle + diff*t,
	})
}

// zeroValueFor returns the additive zero value matching template v (for SMIL by-animations).
func zeroValueFor(v Value) Value {
	switch v.Kind {
	case ValNumber:
		return Value{Kind: ValNumber, Num: 0}
	case ValLength:
		return Value{Kind: ValLength, Num: 0, Unit: v.Unit}
	case ValColor:
		return Value{Kind: ValColor, Paint: PaintColor, Color: [4]float64{0, 0, 0, 0}}
	case ValPaint:
		return Value{Kind: ValPaint, Paint: PaintColor, Color: [4]float64{0, 0, 0, 0}}
	case ValNumberList:
		return Value{Kind: ValNumberList, List: make([]float64, len(v.List))}
	case ValTransformList:
		out := make([]TransformFn, len(v.Xf))
		for i, fn := range v.Xf {
			out[i] = identityTransformFor(fn)
		}
		return Value{Kind: ValTransformList, Xf: out}
	default:
		return v
	}
}

// addValues computes base + delta for SMIL additive="sum".
// Transforms post-multiply after base (base · delta).
func addValues(base, delta Value, p PropID, ctx *EvalCtx) Value {
	if isDiscreteProp(p) {
		return delta
	}
	switch {
	case base.Kind == ValNumber && delta.Kind == ValNumber:
		return Value{Kind: ValNumber, Num: base.Num + delta.Num}

	case base.Kind == ValLength && delta.Kind == ValLength:
		if base.Unit == delta.Unit {
			return Value{Kind: ValLength, Num: base.Num + delta.Num, Unit: base.Unit}
		}
		vb := resolvePropLength(base.Num, base.Unit, p, ctx)
		vd := resolvePropLength(delta.Num, delta.Unit, p, ctx)
		return Value{Kind: ValLength, Num: vb + vd, Unit: UnitNone}

	case base.Kind == ValNumber && delta.Kind == ValLength:
		vd := resolvePropLength(delta.Num, delta.Unit, p, ctx)
		return Value{Kind: ValLength, Num: base.Num + vd, Unit: UnitNone}

	case base.Kind == ValLength && delta.Kind == ValNumber:
		vb := resolvePropLength(base.Num, base.Unit, p, ctx)
		return Value{Kind: ValLength, Num: vb + delta.Num, Unit: UnitNone}

	case (base.Kind == ValColor || base.Kind == ValPaint) && (delta.Kind == ValColor || delta.Kind == ValPaint):
		cb, okB := resolveConcreteColor(base, ctx)
		cd, okD := resolveConcreteColor(delta, ctx)
		if !okB || !okD {
			return delta
		}
		sum := [4]float64{
			clamp01(cb[0] + cd[0]),
			clamp01(cb[1] + cd[1]),
			clamp01(cb[2] + cd[2]),
			clamp01(cb[3] + cd[3]),
		}
		if base.Kind == ValPaint || delta.Kind == ValPaint {
			return Value{Kind: ValPaint, Paint: PaintColor, Color: sum}
		}
		return Value{Kind: ValColor, Paint: PaintColor, Color: sum}

	case base.Kind == ValNumberList && delta.Kind == ValNumberList:
		if len(base.List) == len(delta.List) {
			out := make([]float64, len(base.List))
			for i := range out {
				out[i] = base.List[i] + delta.List[i]
			}
			return Value{Kind: ValNumberList, List: out}
		}
		return delta

	case base.Kind == ValTransformList && delta.Kind == ValTransformList:
		out := make([]TransformFn, 0, len(base.Xf)+len(delta.Xf))
		out = append(out, base.Xf...)
		out = append(out, delta.Xf...)
		return Value{Kind: ValTransformList, Xf: out}

	case base.Kind == ValNone:
		return delta

	default:
		return delta
	}
}

// accumulateValue computes cur + iter * last for SMIL accumulate="sum".
func accumulateValue(cur, last Value, iter int, p PropID, ctx *EvalCtx) Value {
	if iter <= 0 || isDiscreteProp(p) {
		return cur
	}
	k := float64(iter)
	switch {
	case cur.Kind == ValNumber && last.Kind == ValNumber:
		return Value{Kind: ValNumber, Num: cur.Num + k*last.Num}

	case cur.Kind == ValLength && last.Kind == ValLength:
		if cur.Unit == last.Unit {
			return Value{Kind: ValLength, Num: cur.Num + k*last.Num, Unit: cur.Unit}
		}
		vc := resolvePropLength(cur.Num, cur.Unit, p, ctx)
		vl := resolvePropLength(last.Num, last.Unit, p, ctx)
		return Value{Kind: ValLength, Num: vc + k*vl, Unit: UnitNone}

	case (cur.Kind == ValColor || cur.Kind == ValPaint) && (last.Kind == ValColor || last.Kind == ValPaint):
		cc, okC := resolveConcreteColor(cur, ctx)
		cl, okL := resolveConcreteColor(last, ctx)
		if !okC || !okL {
			return cur
		}
		sum := [4]float64{
			clamp01(cc[0] + k*cl[0]),
			clamp01(cc[1] + k*cl[1]),
			clamp01(cc[2] + k*cl[2]),
			clamp01(cc[3] + k*cl[3]),
		}
		if cur.Kind == ValPaint || last.Kind == ValPaint {
			return Value{Kind: ValPaint, Paint: PaintColor, Color: sum}
		}
		return Value{Kind: ValColor, Paint: PaintColor, Color: sum}

	case cur.Kind == ValNumberList && last.Kind == ValNumberList:
		if len(cur.List) == len(last.List) {
			out := make([]float64, len(cur.List))
			for i := range out {
				out[i] = cur.List[i] + k*last.List[i]
			}
			return Value{Kind: ValNumberList, List: out}
		}
		return cur

	case cur.Kind == ValTransformList && last.Kind == ValTransformList:
		if len(cur.Xf) == len(last.Xf) {
			sameOps := true
			for i := range cur.Xf {
				if cur.Xf[i].Op != last.Xf[i].Op || cur.Xf[i].Op == TfMatrix {
					sameOps = false
					break
				}
			}
			if sameOps {
				out := make([]TransformFn, len(cur.Xf))
				ref := evalRefBox(ctx)
				for i := range cur.Xf {
					out[i] = accumulateTransformFn(cur.Xf[i], last.Xf[i], k, ref, ctx)
				}
				return Value{Kind: ValTransformList, Xf: out}
			}
		}
		return cur

	default:
		return cur
	}
}

// addByTransformValue adds parameter deltas within matching transform ops (for SMIL from+by / by on animateTransform).
func addByTransformValue(fromVal, byVal Value) Value {
	if fromVal.Kind != ValTransformList || byVal.Kind != ValTransformList || len(fromVal.Xf) != len(byVal.Xf) {
		return byVal
	}
	out := make([]TransformFn, len(fromVal.Xf))
	for i := range fromVal.Xf {
		if fromVal.Xf[i].Op == byVal.Xf[i].Op {
			out[i] = accumulateTransformFn(fromVal.Xf[i], byVal.Xf[i], 1, Rect{}, nil)
		} else {
			out[i] = byVal.Xf[i]
		}
	}
	return Value{Kind: ValTransformList, Xf: out}
}

func accumulateTransformFn(cur, last TransformFn, k float64, ref Rect, ctx *EvalCtx) TransformFn {
	out := cur
	switch cur.Op {
	case TfTranslate:
		if cur.Units[0] == last.Units[0] {
			out.Args[0] = cur.Args[0] + k*last.Args[0]
		} else {
			out.Args[0] = resolveAxisLength(cur.Args[0], cur.Units[0], ref.W, ctx) +
				k*resolveAxisLength(last.Args[0], last.Units[0], ref.W, ctx)
			out.Units[0] = UnitNone
		}
		if cur.Units[1] == last.Units[1] {
			out.Args[1] = cur.Args[1] + k*last.Args[1]
		} else {
			out.Args[1] = resolveAxisLength(cur.Args[1], cur.Units[1], ref.H, ctx) +
				k*resolveAxisLength(last.Args[1], last.Units[1], ref.H, ctx)
			out.Units[1] = UnitNone
		}
		out.N = 2
	case TfScale:
		csy := cur.Args[0]
		if cur.N >= 2 {
			csy = cur.Args[1]
		}
		lsy := last.Args[0]
		if last.N >= 2 {
			lsy = last.Args[1]
		}
		out.Args[0] = cur.Args[0] + k*last.Args[0]
		out.Args[1] = csy + k*lsy
		out.N = 2
	case TfRotate:
		ang := angleToDeg(cur.Args[0], cur.Units[0]) + k*angleToDeg(last.Args[0], last.Units[0])
		out.Args[0] = ang
		out.Units[0] = UnitDeg
	case TfSkewX, TfSkewY:
		ang := angleToDeg(cur.Args[0], cur.Units[0]) + k*angleToDeg(last.Args[0], last.Units[0])
		out.Args[0] = ang
		out.Units[0] = UnitDeg
	}
	return out
}
