// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"encoding/xml"
	"slices"
	"strings"

	"github.com/srwiley/rasterx"
)

// captured is an element whose content is not drawn where it is, but
// where something names it: a clipPath or a mask, through which shapes are
// drawn, or a marker, drawn at a shape's vertices. Its elements are kept as
// a definition's are, for expandDefs.
type captured struct {
	mask  bool       // a mask, not a clipPath; for a marker, false
	attrs []xml.Attr // the element's own
	defs  []definition
}

// capturing reports whether se is kept rather than drawn: inside a
// clipPath, a mask or a marker being read, or one beginning.
func (c *IconCursor) capturing(se xml.StartElement) bool {
	if c.capture != nil {
		c.captureDepth++
		c.capture.defs = append(c.capture.defs, definition{Tag: se.Name.Local, Attrs: se.Attr})
		return true
	}
	id := ""
	for _, a := range se.Attr {
		if a.Name.Local == "id" {
			id = a.Value
		}
	}
	switch se.Name.Local {
	case "clipPath", "mask":
		c.capture = &captured{mask: se.Name.Local == "mask", attrs: se.Attr}
		if id != "" {
			c.icon.clips[id] = c.capture
		}
	case "marker":
		c.capture = &captured{attrs: se.Attr}
		if id != "" {
			c.icon.markers[id] = c.capture
		}
	default:
		return false
	}
	c.captureDepth = 0
	return true
}

// endCapture reports whether se ends an element kept by capturing, or the
// element it began.
func (c *IconCursor) endCapture(se xml.EndElement) bool {
	if c.capture == nil {
		return false
	}
	if c.captureDepth == 0 {
		c.capture = nil
		return true
	}
	c.captureDepth--
	if se.Name.Local == "g" {
		c.capture.defs = append(c.capture.defs, definition{Tag: "endg"})
	}
	return true
}

// urlID is the id a url(#id) names, "" for none.
func urlID(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "url(") || !strings.HasSuffix(v, ")") {
		return ""
	}
	id, ok := strings.CutPrefix(strings.Trim(strings.TrimSpace(v[4:len(v)-1]), `'"`), "#")
	if !ok {
		return ""
	}
	return id
}

// expandCaptured draws def's elements as shapes of their own, apart from
// the document's, under the transform m and the default style: what a clip
// or mask covers, or a marker's shapes. A marker's are added to the
// document's paths too, as add says; a clip's are returned.
func (c *IconCursor) expandCaptured(def *captured, m rasterx.Matrix2D, add bool) ([]SvgPath, error) {
	start := len(c.icon.SVGPaths)
	stack := c.StyleStack
	base := DefaultStyle
	base.mAdder.M = m
	c.StyleStack = []PathStyle{base}
	// The element's own transform, as a clipPath's, places what it holds.
	if err := c.PushStyle(transformOnly(def.attrs)); err != nil {
		c.StyleStack = stack
		return nil, err
	}
	err := c.expandDefs(def.defs)
	c.StyleStack = stack
	if add || err != nil {
		return nil, err
	}
	paths := slices.Clone(c.icon.SVGPaths[start:])
	c.icon.SVGPaths = c.icon.SVGPaths[:start]
	return paths, nil
}

// transformOnly is attrs' transform, alone.
func transformOnly(attrs []xml.Attr) []xml.Attr {
	for _, a := range attrs {
		if a.Name.Local == "transform" {
			return []xml.Attr{a}
		}
	}
	return nil
}
