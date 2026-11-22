// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"encoding/xml"
	"fmt"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/colornames"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// ReadIconStream reads the Icon from the given io.Reader.
// This only supports a sub-set of SVG, but
// is enough to draw many icons. It fails on an element or a path command
// it does not handle.
func ReadIconStream(stream io.Reader) (*SvgIcon, error) {
	icon := &SvgIcon{Defs: make(map[string][]definition), Grads: make(map[string]*rasterx.Gradient), Transform: rasterx.Identity,
		clips: map[string]*captured{}, markers: map[string]*captured{}}
	cursor := &iconCursor{StyleStack: []PathStyle{defaultStyle}, icon: icon}
	var classInfo strings.Builder
	decoder := xml.NewDecoder(stream)
	for {
		t, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return icon, err
		}
		// Inspect the type of the XML token
		switch se := t.(type) {
		case xml.StartElement:
			// Inside <defs> or a captured <clipPath>/<mask>/<marker>, elements are stored
			// for later expansion (which runs PushStyle), so avoid parsing style attributes
			// prematurely (and keep StyleStack balanced for EndElement).
			if cursor.inDefs || cursor.capture != nil {
				cursor.StyleStack = append(cursor.StyleStack, cursor.StyleStack[len(cursor.StyleStack)-1])
			} else {
				err = cursor.PushStyle(se.Attr)
				if err != nil {
					return icon, err
				}
			}
			err = cursor.readStartElement(se)
			if err != nil {
				return icon, err
			}
		case xml.EndElement:
			// pop style
			cursor.StyleStack = cursor.StyleStack[:len(cursor.StyleStack)-1]
			if cursor.endCapture(se) {
				continue
			}
			if se.Name.Local == "style" && cursor.inDefsStyle {
				parsed, err := parseClasses(classInfo.String())
				classInfo.Reset()
				cursor.inDefsStyle = false
				if err != nil {
					return icon, err
				}
				if icon.classes == nil {
					icon.classes = parsed
				} else {
					for cls, attrs := range parsed {
						if icon.classes[cls] == nil {
							icon.classes[cls] = attrs
							continue
						}
						for k, v := range attrs {
							icon.classes[cls][k] = v
						}
					}
				}
				continue
			}
			if se.Name.Local == "radialGradient" || se.Name.Local == "linearGradient" {
				cursor.inGrad = false
				continue
			}
			if cursor.inDefs && !cursor.inGrad {
				if se.Name.Local == "defs" && cursor.defsDepth == 0 {
					cursor.defStack = cursor.defStack[:0]
					cursor.inDefs = false
					continue
				}
				if cursor.defsDepth > 0 {
					cursor.defsDepth--
					if se.Name.Local == "g" {
						for i := range cursor.defStack {
							cursor.defStack[i].defs = append(cursor.defStack[i].defs, definition{Tag: "endg"})
						}
					}
					for len(cursor.defStack) > 0 && cursor.defStack[len(cursor.defStack)-1].depth == cursor.defsDepth {
						top := cursor.defStack[len(cursor.defStack)-1]
						cursor.defStack = cursor.defStack[:len(cursor.defStack)-1]
						cursor.icon.Defs[top.id] = top.defs
					}
				}
			}
		case xml.CharData:
			if cursor.inDefsStyle {
				classInfo.Write(se)
			}
		}
	}
	return icon, nil
}

// parseSVGColorNum reads the SVG color string e.g. #FBD9BD
func parseSVGColorNum(colorStr string) (r, g, b uint8, err error) {
	colorStr = strings.TrimPrefix(colorStr, "#")
	var t uint64
	if len(colorStr) != 6 {
		if len(colorStr) != 3 {
			err = fmt.Errorf("color string %s is not length 3 or 6 as required by SVG specification",
				colorStr)
			return
		}
		// SVG specs say duplicate characters in case of 3 digit hex number
		colorStr = string([]byte{colorStr[0], colorStr[0],
			colorStr[1], colorStr[1], colorStr[2], colorStr[2]})
	}
	for _, v := range []struct {
		c *uint8
		s string
	}{
		{&r, colorStr[0:2]},
		{&g, colorStr[2:4]},
		{&b, colorStr[4:6]}} {
		t, err = strconv.ParseUint(v.s, 16, 8)
		if err != nil {
			return
		}
		*v.c = uint8(t)
	}
	return
}

// ParseSVGColor parses an SVG color string in all forms
// including all SVG1.1 names, obtained from the image.colornames package
func ParseSVGColor(colorStr string) (color.Color, error) {
	v := strings.ToLower(strings.TrimSpace(colorStr))
	if strings.HasPrefix(v, "url(") {
		return nil, fmt.Errorf("oksvg: unresolved paint server %q", colorStr)
	}
	switch v {
	case "none", "":
		// nil signals that the function (fill or stroke) is off;
		// not the same as black
		return nil, nil
	default:
		cn, ok := colornames.Map[v]
		if ok {
			return color.NRGBA{cn.R, cn.G, cn.B, cn.A}, nil
		}
	}
	if cStr, ok := strings.CutPrefix(v, "rgb("); ok {
		cStr, ok = strings.CutSuffix(cStr, ")")
		if !ok {
			return color.NRGBA{}, errParamMismatch
		}
		vals := strings.Split(cStr, ",")
		if len(vals) != 3 {
			return color.NRGBA{}, errParamMismatch
		}
		var cvals [3]uint8
		var err error
		for i := range cvals {
			cvals[i], err = parseColorValue(vals[i])
			if err != nil {
				return nil, err
			}
		}
		return color.NRGBA{cvals[0], cvals[1], cvals[2], 0xFF}, nil
	}

	if cStr, ok := strings.CutPrefix(v, "hsl("); ok {
		cStr, ok = strings.CutSuffix(cStr, ")")
		if !ok {
			return color.NRGBA{}, errParamMismatch
		}
		vals := strings.Split(cStr, ",")
		if len(vals) != 3 {
			return color.NRGBA{}, errParamMismatch
		}

		H, err := strconv.ParseFloat(strings.TrimSpace(vals[0]), 64)
		if err != nil || math.IsNaN(H) || math.IsInf(H, 0) {
			return color.NRGBA{}, fmt.Errorf("invalid hue in hsl: '%s' (%v)", vals[0], err)
		}
		H = math.Mod(H, 360)
		if H < 0 {
			H += 360
		}

		S, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(vals[1]), "%"), 64)
		if err != nil || math.IsNaN(S) || math.IsInf(S, 0) {
			return color.NRGBA{}, fmt.Errorf("invalid saturation in hsl: '%s' (%v)", vals[1], err)
		}
		S = max(0, min(S/100, 1))

		L, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(vals[2]), "%"), 64)
		if err != nil || math.IsNaN(L) || math.IsInf(L, 0) {
			return color.NRGBA{}, fmt.Errorf("invalid lightness in hsl: '%s' (%v)", vals[2], err)
		}
		L = max(0, min(L/100, 1))

		C := (1 - math.Abs((2*L)-1)) * S
		X := C * (1 - math.Abs(math.Mod(H/60, 2)-1))
		m := L - float64(C/2) // not fused, as on arm64

		var rp, gp, bp float64
		if H < 60 {
			rp, gp, bp = float64(C), float64(X), float64(0)
		} else if H < 120 {
			rp, gp, bp = float64(X), float64(C), float64(0)
		} else if H < 180 {
			rp, gp, bp = float64(0), float64(C), float64(X)
		} else if H < 240 {
			rp, gp, bp = float64(0), float64(X), float64(C)
		} else if H < 300 {
			rp, gp, bp = float64(X), float64(0), float64(C)
		} else {
			rp, gp, bp = float64(C), float64(0), float64(X)
		}

		r := max(0, min(math.Round((rp+m)*255), 255))
		g := max(0, min(math.Round((gp+m)*255), 255))
		b := max(0, min(math.Round((bp+m)*255), 255))

		return color.NRGBA{
			uint8(r),
			uint8(g),
			uint8(b),
			0xFF,
		}, nil
	}

	if v[0] == '#' {
		r, g, b, err := parseSVGColorNum(v)
		if err != nil {
			return nil, err
		}
		return color.NRGBA{r, g, b, 0xFF}, nil
	}
	return nil, errParamMismatch
}
