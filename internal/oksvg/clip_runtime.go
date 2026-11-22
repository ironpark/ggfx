// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// scopeMask rasterizes the clipPath or mask scope at scopeIdx into an alpha mask
// (cached in masks[scopeIdx]) and multiplies it with any enclosing parent scope.
func scopeMask(p *Painter, fr *frame, masks []*image.Alpha, scopeIdx int32, t rasterx.Matrix2D) *image.Alpha {
	if scopeIdx < 0 || int(scopeIdx) >= len(fr.scopes) {
		return nil
	}
	if m := masks[scopeIdx]; m != nil {
		return m
	}
	sc := &fr.scopes[scopeIdx]
	var parentMask *image.Alpha
	if sc.parent >= 0 {
		parentMask = scopeMask(p, fr, masks, sc.parent, t)
		if parentMask != nil && parentMask.Rect.Empty() {
			masks[scopeIdx] = parentMask
			return parentMask
		}
	}
	items := fr.clipItems[sc.itemStart : sc.itemStart+sc.itemCount]
	if len(items) == 0 {
		empty := &image.Alpha{}
		masks[scopeIdx] = empty
		return empty
	}

	b := p.Scanner.Dest.Bounds()
	if p.clips == nil {
		p.clips = NewPainter(0, 0, nil, image.Rectangle{})
	}
	c := p.clips
	c.Dasher.SetBounds(b.Dx(), b.Dy())
	c.Scanner.Targ = b

	draw := func(dst *image.RGBA) {
		c.Scanner.Dest, c.Scanner.Touched = dst, image.Rectangle{}
		for i := range items {
			it := &items[i]
			if !it.visible {
				continue
			}
			var subMask *image.Alpha
			if it.scope >= 0 {
				subMask = scopeMask(c, fr, masks, it.scope, t)
			}
			c.currentColor = it.style.color
			svgp := SvgPath{PathStyle: it.style, Path: it.path}
			svgp.drawThroughTrim(c, 1, t, subMask, it.trim, it.lens)
		}
	}

	// First pass: find touched bounding window without rasterizing.
	draw(nil)
	area := c.Scanner.Touched
	if parentMask != nil {
		area = area.Intersect(parentMask.Rect.Add(b.Min))
	}
	if area.Empty() {
		empty := &image.Alpha{}
		masks[scopeIdx] = empty
		return empty
	}

	// Second pass: rasterize clip/mask content over area.
	draw(c.scratch(area))
	drawn := c.Scanner.Dest
	c.Scanner.Dest = nil

	mask := image.NewAlpha(area)
	for i := 0; i < len(mask.Pix); i++ {
		px := drawn.Pix[4*i : 4*i+4 : 4*i+4]
		if sc.isMask {
			// Premultiplied sRGB luminance (matches legacy clip.go).
			mask.Pix[i] = uint8((2126*uint32(px[0]) + 7152*uint32(px[1]) + 722*uint32(px[2])) / 10000)
		} else {
			mask.Pix[i] = px[3]
		}
	}
	mask.Rect = mask.Rect.Sub(b.Min)
	if parentMask != nil {
		w := mask.Rect.Dx()
		for y := mask.Rect.Min.Y; y < mask.Rect.Max.Y; y++ {
			mRow := mask.Pix[mask.PixOffset(mask.Rect.Min.X, y) : mask.PixOffset(mask.Rect.Min.X, y)+w]
			pRow := parentMask.Pix[parentMask.PixOffset(mask.Rect.Min.X, y) : parentMask.PixOffset(mask.Rect.Min.X, y)+w]
			for x := range w {
				mRow[x] = uint8((uint32(mRow[x])*uint32(pRow[x]) + 127) / 255)
			}
		}
	}
	masks[scopeIdx] = mask
	return mask
}
