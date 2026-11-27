// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"
	"image/color"

	"golang.org/x/image/math/fixed"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// A shape drawn through a clipPath shows only where the clip's shapes are;
// through a mask, as much as the mask's shapes are light. The clip's shapes
// are compiled where a style names it, in the naming element's user space,
// and drawn alone once a drawing, into a mask the size of the target; each
// shape under it then has its color scaled at each pixel by the mask's
// there. So a group clipped as a whole is clipped shape by shape, which is
// the same for a clip; for a mask, it differs only where the group's
// shapes overlap where the mask is partly light, as the fork already
// applies a group's opacity shape by shape.

// clipUse is a clip or mask as a style names it: its definition, its shapes
// placed where they apply, and any enclosing ancestor clip or mask.
type clipUse struct {
	def    *captured
	paths  []SvgPath
	parent *clipUse
}

// chainClip returns a clip chain applying inner inside outer.
func chainClip(inner, outer *clipUse) *clipUse {
	if inner == nil {
		return outer
	}
	if outer == nil {
		return inner
	}
	cp := *inner
	cp.parent = chainClip(inner.parent, outer)
	return &cp
}

type compiledClipKey struct {
	def *captured
	m   rasterx.Matrix2D
}

type compiledClipUseKey struct {
	def    *captured
	m      rasterx.Matrix2D
	parent *clipUse
}

// compileClipChain compiles each newly added clipUse from u up to stop under
// transform m, reusing previously compiled (def, m) paths and (def, m, parent)
// clipUse nodes so repeated references to the same clipPath are compiled once.
func (c *iconCursor) compileClipChain(u, stop *clipUse, m rasterx.Matrix2D) (*clipUse, error) {
	if u == nil || u == stop {
		return u, nil
	}
	parent, err := c.compileClipChain(u.parent, stop, m)
	if err != nil {
		return nil, err
	}
	useKey := compiledClipUseKey{def: u.def, m: m, parent: parent}
	if cached, ok := c.compiledClipUses[useKey]; ok {
		return cached, nil
	}
	u.parent = parent
	if err := c.compileClip(u, m); err != nil {
		return nil, err
	}
	if c.compiledClipUses == nil {
		c.compiledClipUses = make(map[compiledClipUseKey]*clipUse, 4)
	}
	c.compiledClipUses[useKey] = u
	return u, nil
}

// compileClip places u's shapes in the user space m of the element naming
// it.
func (c *iconCursor) compileClip(u *clipUse, m rasterx.Matrix2D) error {
	key := compiledClipKey{def: u.def, m: m}
	if cached, ok := c.compiledClips[key]; ok {
		u.paths = cached
		return nil
	}
	paths, err := c.expandCaptured(u.def, m, false)
	if err != nil {
		return err
	}
	if !u.def.mask {
		// A clip is its shapes' geometry: what they cover, whatever their
		// paint.
		for i := range paths {
			paths[i].fillerColor = paint{color: color.NRGBA{0xff, 0xff, 0xff, 0xff}}
			paths[i].linerColor = paint{}
			paths[i].FillOpacity = 1
			paths[i].clip = nil
			// A clip's shapes are filled by its rule, not their fill's.
			paths[i].UseNonZeroWinding = paths[i].nonZeroClip
		}
	}
	if c.compiledClips == nil {
		c.compiledClips = make(map[compiledClipKey][]SvgPath, 4)
	}
	c.compiledClips[key] = paths
	u.paths = paths
	return nil
}

// clipResult is the evaluated clip/mask for a draw call: either empty (lets
// nothing through), a pure integer rectangle (applied via Scanner.SetClip with
// no mask buffer), or an alpha mask (optionally further constrained by rect).
type clipResult struct {
	set      bool
	empty    bool
	rectOnly bool
	rect     image.Rectangle
	mask     *image.Alpha
}

// axisAlignedIntRect reports whether p transformed by m (using the rasterizer's
// exact 26.6 fixed-point TFixed transform) is a proper axis-aligned rectangle
// whose four vertices visit all four distinct bounding-box corners in perimeter
// order and lie exactly on integer pixel coordinates.
func axisAlignedIntRect(p rasterx.Path, m rasterx.Matrix2D) (image.Rectangle, bool) {
	var pts [5]fixed.Point26_6
	n := 0
	for i := 0; i < len(p); {
		switch rasterx.PathCommand(p[i]) {
		case rasterx.PathMoveTo:
			if n != 0 {
				return image.Rectangle{}, false // multiple subpaths
			}
			pts[0] = m.TFixed(fixed.Point26_6{X: p[i+1], Y: p[i+2]})
			n = 1
			i += 3
		case rasterx.PathLineTo:
			if n >= 5 {
				return image.Rectangle{}, false
			}
			pts[n] = m.TFixed(fixed.Point26_6{X: p[i+1], Y: p[i+2]})
			n++
			i += 3
		case rasterx.PathClose:
			i++
			if i != len(p) {
				return image.Rectangle{}, false
			}
		default:
			return image.Rectangle{}, false
		}
	}
	if n == 5 && pts[4] == pts[0] {
		n = 4
	}
	if n != 4 {
		return image.Rectangle{}, false
	}
	// Every vertex must lie on an exact integer pixel (zero 26.6 fractional bits).
	for k := range 4 {
		if pts[k].X&63 != 0 || pts[k].Y&63 != 0 {
			return image.Rectangle{}, false
		}
	}
	minX, maxX := pts[0].X, pts[0].X
	minY, maxY := pts[0].Y, pts[0].Y
	for k := 1; k < 4; k++ {
		minX = min(minX, pts[k].X)
		maxX = max(maxX, pts[k].X)
		minY = min(minY, pts[k].Y)
		maxY = max(maxY, pts[k].Y)
	}
	if minX >= maxX || minY >= maxY {
		return image.Rectangle{}, false
	}
	// Require four distinct bounding-box corners visited in perimeter order
	// (no backtracking, no repeated corners, no diagonal crossings).
	var seen [4]bool
	for k := range 4 {
		a, b := pts[k], pts[(k+1)%4]
		if a == b || (a.X != b.X && a.Y != b.Y) {
			return image.Rectangle{}, false
		}
		var cx, cy int
		switch a.X {
		case minX:
			cx = 0
		case maxX:
			cx = 1
		default:
			return image.Rectangle{}, false
		}
		switch a.Y {
		case minY:
			cy = 0
		case maxY:
			cy = 2
		default:
			return image.Rectangle{}, false
		}
		corner := cx | cy
		if seen[corner] {
			return image.Rectangle{}, false
		}
		seen[corner] = true
	}
	return image.Rect(int(minX>>6), int(minY>>6), int(maxX>>6), int(maxY>>6)), true
}

// maskImage resolves u under t over p's target into a clipResult, reusing
// p's alpha mask pool and turning integer axis-aligned rect clipPaths into
// pure Scanner.SetClip rectangles.
func (u *clipUse) maskImage(p *Painter, t rasterx.Matrix2D, cache map[*clipUse]clipResult) clipResult {
	if m, ok := cache[u]; ok {
		return m
	}
	var parent clipResult
	if u.parent != nil {
		parent = u.parent.maskImage(p, t, cache)
		if parent.empty {
			if cache != nil {
				cache[u] = parent
			}
			return parent
		}
	}
	b := p.Scanner.Dest.Bounds()
	targetRect := image.Rectangle{Max: b.Size()}

	// Fast path: single axis-aligned integer rect clipPath needs no alpha mask.
	if !u.def.mask && len(u.paths) == 1 {
		if r, ok := axisAlignedIntRect(u.paths[0].Path, t.Mult(u.paths[0].m)); ok {
			r = r.Intersect(targetRect)
			var res clipResult
			if !parent.set {
				if r.Empty() {
					res = clipResult{set: true, empty: true}
				} else {
					res = clipResult{set: true, rectOnly: true, rect: r}
				}
			} else if parent.rectOnly {
				r = r.Intersect(parent.rect)
				if r.Empty() {
					res = clipResult{set: true, empty: true}
				} else {
					res = clipResult{set: true, rectOnly: true, rect: r}
				}
			} else {
				r = r.Intersect(parent.rect)
				if r.Empty() {
					res = clipResult{set: true, empty: true}
				} else {
					res = clipResult{set: true, rect: r, mask: parent.mask}
				}
			}
			if cache != nil {
				cache[u] = res
			}
			return res
		}
	}

	if p.clips == nil {
		p.clips = NewPainter(0, 0, nil, image.Rectangle{})
	}
	c := p.clips
	c.Dasher.SetBounds(b.Dx(), b.Dy())
	c.Dasher.ShareDrawBudget(p.Dasher)
	c.Scanner.Targ = b
	drawRGBA := func(dst *image.RGBA) {
		c.Scanner.Dest, c.Scanner.DestAlpha, c.Scanner.Touched = dst, nil, image.Rectangle{}
		for i := range u.paths {
			u.paths[i].DrawTransformed(c, 1, t)
		}
	}
	// Find the shapes' windows, without rasterizing them, then draw them
	// over just those.
	drawRGBA(nil)
	area := c.Scanner.Touched
	if parent.set {
		area = area.Intersect(parent.rect.Add(b.Min))
	}
	if area.Empty() {
		res := clipResult{set: true, empty: true}
		if cache != nil {
			cache[u] = res
		}
		return res
	}

	var mask *image.Alpha
	if u.def.mask {
		drawRGBA(c.scratch(area))
		drawn := c.Scanner.Dest
		c.Scanner.Dest = nil
		mask = p.allocAlpha(area, false)
		for i := range mask.Pix {
			px := drawn.Pix[4*i : 4*i+4 : 4*i+4]
			// Premultiplied, so the light is already its alpha's.
			mask.Pix[i] = uint8((2126*uint32(px[0]) + 7152*uint32(px[1]) + 722*uint32(px[2])) / 10000)
		}
	} else {
		// clipPath: rasterize coverage directly into single-channel alpha.
		mask = p.allocAlpha(area, true)
		c.Scanner.Dest, c.Scanner.DestAlpha, c.Scanner.Touched = nil, mask, image.Rectangle{}
		for i := range u.paths {
			u.paths[i].DrawTransformed(c, 1, t)
		}
		c.Scanner.DestAlpha = nil
	}
	// The paint is asked its color in the target's own coordinates, from
	// 0 at its corner, not in the image's.
	mask.Rect = mask.Rect.Sub(b.Min)
	if parent.set && parent.mask != nil {
		// area was already intersected with parent.rect + b.Min, so mask.Rect ⊆ parent.mask.Rect.
		w := mask.Rect.Dx()
		for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
			mRow := mask.Pix[mask.PixOffset(mask.Rect.Min.X, y) : mask.PixOffset(mask.Rect.Min.X, y)+w]
			pRow := parent.mask.Pix[parent.mask.PixOffset(mask.Rect.Min.X, y) : parent.mask.PixOffset(mask.Rect.Min.X, y)+w]
			for x := range w {
				mRow[x] = uint8((uint32(mRow[x])*uint32(pRow[x]) + 127) / 255)
			}
		}
	}
	res := clipResult{set: true, rect: mask.Rect, mask: mask}
	if cache != nil {
		cache[u] = res
	}
	return res
}
