// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"encoding/xml"
	"errors"
	"slices"
	"strings"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// svgFunc defines function interface to use as drawing implementation.
type svgFunc func(c *iconCursor, attrs []xml.Attr) error

var (
	drawFuncs = map[string]svgFunc{
		"svg":            svgF,
		"g":              gF,
		"line":           lineF,
		"stop":           stopF,
		"rect":           rectF,
		"circle":         circleF,
		"ellipse":        circleF, //circleF handles ellipse also
		"polyline":       polylineF,
		"polygon":        polygonF,
		"path":           pathF,
		"desc":           gF, // title and desc are read past
		"defs":           defsF,
		"style":          styleF,
		"title":          gF,
		"linearGradient": linearGradientF,
		"radialGradient": radialGradientF,
	}

	svgF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		c.icon.ViewBox.X = 0
		c.icon.ViewBox.Y = 0
		c.icon.ViewBox.W = 0
		c.icon.ViewBox.H = 0
		var width, height float64
		var err error
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "viewBox":
				err = c.GetPoints(attr.Value)
				if len(c.points) != 4 {
					return errParamMismatch
				}
				c.icon.ViewBox.X = c.points[0]
				c.icon.ViewBox.Y = c.points[1]
				c.icon.ViewBox.W = c.points[2]
				c.icon.ViewBox.H = c.points[3]
			case "width":
				width, err = parseFloat(attr.Value, 64)
			case "height":
				height, err = parseFloat(attr.Value, 64)
			}
			if err != nil {
				return err
			}
		}
		if c.icon.ViewBox.W == 0 {
			c.icon.ViewBox.W = width
		}
		if c.icon.ViewBox.H == 0 {
			c.icon.ViewBox.H = height
		}
		return nil
	}
	gF    svgFunc = func(*iconCursor, []xml.Attr) error { return nil } // g does nothing but push the style
	rectF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		var x, y, w, h, rx, ry float64
		var setRx, setRy bool
		var err error
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "x":
				x, err = parseFloat(attr.Value, 64)
			case "y":
				y, err = parseFloat(attr.Value, 64)
			case "width":
				w, err = parseFloat(attr.Value, 64)
			case "height":
				h, err = parseFloat(attr.Value, 64)
			case "rx":
				rx, err = parseFloat(attr.Value, 64)
				setRx = true
			case "ry":
				ry, err = parseFloat(attr.Value, 64)
				setRy = true
			}
			if err != nil {
				return err
			}
		}
		buildRect(&c.Path, x, y, w, h, rx, ry, setRx, setRy)
		return nil
	}
	circleF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		var cx, cy, rx, ry float64
		var err error
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "cx":
				cx, err = parseFloat(attr.Value, 64)
			case "cy":
				cy, err = parseFloat(attr.Value, 64)
			case "r":
				rx, err = parseFloat(attr.Value, 64)
				ry = rx
			case "rx":
				rx, err = parseFloat(attr.Value, 64)
			case "ry":
				ry, err = parseFloat(attr.Value, 64)
			}
			if err != nil {
				return err
			}
		}
		buildEllipse(&c.Path, cx, cy, rx, ry)
		return nil
	}
	lineF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		var x1, x2, y1, y2 float64
		var err error
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "x1":
				x1, err = parseFloat(attr.Value, 64)
			case "x2":
				x2, err = parseFloat(attr.Value, 64)
			case "y1":
				y1, err = parseFloat(attr.Value, 64)
			case "y2":
				y2, err = parseFloat(attr.Value, 64)
			}
			if err != nil {
				return err
			}
		}
		buildLine(&c.Path, x1, y1, x2, y2)
		return nil
	}
	polylineF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		c.points = c.points[:0]
		var err error
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "points":
				err = c.GetPoints(attr.Value)
				if len(c.points)%2 != 0 {
					return errors.New("polygon has odd number of points")
				}
			}
			if err != nil {
				return err
			}
		}
		buildPoly(&c.Path, c.points, false)
		return nil
	}
	polygonF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		err := polylineF(c, attrs)
		if len(c.points) >= 4 {
			c.Path.Stop(true)
		}
		return err
	}
	pathF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		var err error
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "d":
				err = c.CompilePath(attr.Value)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	defsF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		c.inDefs = true
		return nil
	}
	styleF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		c.inDefsStyle = true
		return nil
	}
	linearGradientF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		var err error
		c.inGrad = true
		c.grad = &rasterx.Gradient{Points: [5]float64{0, 0, 1, 0, 0},
			IsRadial: false, Bounds: c.icon.ViewBox, Matrix: rasterx.Identity}
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "id":
				if attr.Value != "" {
					c.icon.Grads[attr.Value] = c.grad
				}
			case "x1":
				c.grad.Points[0], err = readFraction(attr.Value)
			case "y1":
				c.grad.Points[1], err = readFraction(attr.Value)
			case "x2":
				c.grad.Points[2], err = readFraction(attr.Value)
			case "y2":
				c.grad.Points[3], err = readFraction(attr.Value)
			default:
				err = c.ReadGradAttr(attr)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	radialGradientF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		c.inGrad = true
		c.grad = &rasterx.Gradient{Points: [5]float64{0.5, 0.5, 0.5, 0.5, 0.5},
			IsRadial: true, Bounds: c.icon.ViewBox, Matrix: rasterx.Identity}
		var setFx, setFy bool
		var err error
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "id":
				if attr.Value != "" {
					c.icon.Grads[attr.Value] = c.grad
				}
			case "r":
				c.grad.Points[4], err = readFraction(attr.Value)
			case "cx":
				c.grad.Points[0], err = readFraction(attr.Value)
			case "cy":
				c.grad.Points[1], err = readFraction(attr.Value)
			case "fx":
				setFx = true
				c.grad.Points[2], err = readFraction(attr.Value)
			case "fy":
				setFy = true
				c.grad.Points[3], err = readFraction(attr.Value)
			default:
				err = c.ReadGradAttr(attr)
			}
			if err != nil {
				return err
			}
		}
		if !setFx { // set fx to cx by default
			c.grad.Points[2] = c.grad.Points[0]
		}
		if !setFy { // set fy to cy by default
			c.grad.Points[3] = c.grad.Points[1]
		}
		return nil
	}
	stopF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		var err error
		if c.inGrad {
			stop := rasterx.GradStop{Opacity: 1.0}
			for _, attr := range attrs {
				switch attr.Name.Local {
				case "offset":
					stop.Offset, err = readFraction(attr.Value)
				case "stop-color":
					if strings.EqualFold(strings.TrimSpace(attr.Value), "currentcolor") {
						stop.StopColor = currentColorMark
						c.icon.usesCurrentColor = true
						c.icon.gradHasCurrentColor = true
					} else {
						stop.StopColor, err = parseColorFlexible(attr.Value)
					}
				case "stop-opacity":
					stop.Opacity, err = parseOpacityVal(attr.Value)
				}
				if err != nil {
					return err
				}
			}
			// An offset is in [0, 1], and none is less than the one before
			// it: the stops keep the document's order, as SVG has it.
			stop.Offset = max(0, min(stop.Offset, 1))
			if n := len(c.grad.Stops); n > 0 {
				stop.Offset = max(stop.Offset, c.grad.Stops[n-1].Offset)
			}
			c.grad.Stops = append(c.grad.Stops, stop)
		}
		return nil
	}
	useF svgFunc = func(c *iconCursor, attrs []xml.Attr) error {
		var (
			href string
			x, y float64
			err  error
		)
		for _, attr := range attrs {
			switch attr.Name.Local {
			case "href":
				href = attr.Value
			case "x":
				x, err = parseFloat(attr.Value, 64)
			case "y":
				y, err = parseFloat(attr.Value, 64)
			}
			if err != nil {
				return err
			}
		}
		// Translate the Style adder matrix by use's x and y
		c.StyleStack[len(c.StyleStack)-1].m =
			c.StyleStack[len(c.StyleStack)-1].m.Translate(x, y)
		if href == "" {
			return errors.New("only use tags with href is supported")
		}
		if !strings.HasPrefix(href, "#") {
			return errors.New("only the ID CSS selector is supported")
		}
		id := href[1:]
		if slices.Contains(c.useStack, id) {
			return nil // cyclic <use>
		}
		defs, ok := c.icon.Defs[id]
		if !ok {
			return errors.New("href ID in use statement was not found in saved defs")
		}
		c.useStack = append(c.useStack, id)
		defer func() {
			c.useStack = c.useStack[:len(c.useStack)-1]
		}()
		return c.expandDefs(defs)
	}
)

// expandDefs draws a definition's elements where it is used, as a use, a
// clip, a mask or a marker expands one, each under the style on top of the
// stack. The expansion is bounded: a cyclic reference recurses until the
// stack overflows, and nested uses expand exponentially.
func (c *iconCursor) expandDefs(defs []definition) error {
	stackLen := len(c.StyleStack)
	baseDepth := c.depth
	defer func() {
		c.StyleStack = c.StyleStack[:stackLen]
		c.depth = baseDepth
	}()
	for _, def := range defs {
		if def.Tag == "endg" {
			// pop style
			if len(c.StyleStack) > stackLen {
				c.StyleStack = c.StyleStack[:len(c.StyleStack)-1]
			}
			if c.depth > baseDepth {
				c.depth--
			}
			continue
		}
		c.depth++
		if c.depth > MaxNestingDepth {
			return ErrNestingDepth
		}
		if err := c.PushStyle(def.Attrs); err != nil {
			return err
		}
		df, ok := drawFuncs[def.Tag]
		if !ok {
			return errors.New("Cannot process svg element " + def.Tag)
		}
		if !c.StyleStack[len(c.StyleStack)-1].displayNone {
			c.useExpansions++
			if c.useExpansions > MaxExpandedElements {
				return ErrExpansionLimit
			}
			if err := df(c, def.Attrs); err != nil {
				return err
			}
			if err := c.addPath(def.Tag); err != nil {
				return err
			}
		}
		if def.Tag != "g" {
			// pop style
			c.StyleStack = c.StyleStack[:len(c.StyleStack)-1]
			c.depth--
		}
	}
	return nil
}

func init() {
	// avoids cyclical static declaration
	// called on package initialization
	drawFuncs["use"] = useF
}
