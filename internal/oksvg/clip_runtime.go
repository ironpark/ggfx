// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// scopeClip rasterizes the clipPath or mask scope at scopeIdx into a clipResult
// (cached in clips[scopeIdx]), reusing p's alpha mask pool and turning
// integer axis-aligned rect clipPaths into Scanner.SetClip with no mask.
// b is the target bounds in Dest coordinates (always valid even during bounds passes).
func scopeClip(p *Painter, fr *frame, clips []clipResult, scopeIdx int32, t rasterx.Matrix2D, b image.Rectangle) clipResult {
	if scopeIdx < 0 || int(scopeIdx) >= len(fr.scopes) {
		return clipResult{}
	}
	if clips[scopeIdx].set {
		return clips[scopeIdx]
	}
	sc := &fr.scopes[scopeIdx]
	var parent clipResult
	if sc.parent >= 0 {
		parent = scopeClip(p, fr, clips, sc.parent, t, b)
		if parent.empty {
			clips[scopeIdx] = parent
			return parent
		}
	}
	items := fr.clipItems[sc.itemStart : sc.itemStart+sc.itemCount]
	if len(items) == 0 {
		res := clipResult{set: true, empty: true}
		clips[scopeIdx] = res
		return res
	}

	targetRect := image.Rectangle{Max: b.Size()}

	// Fast path: single visible axis-aligned integer rect clipPath without sub-scope.
	if !sc.isMask && len(items) == 1 && items[0].visible && items[0].scope < 0 {
		if r, ok := axisAlignedIntRect(items[0].path, t.Mult(items[0].style.m)); ok {
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
			clips[scopeIdx] = res
			return res
		}
	}

	// Pre-resolve any nested clip/mask scopes referenced by items in this scope
	// before touching p.clips so recursive scopes never clobber p.clips's
	// Dest/Touched/scratch state or dereference a nil Dest during the bounds pass.
	for i := range items {
		it := &items[i]
		if it.visible && it.scope >= 0 && !clips[it.scope].set {
			scopeClip(p, fr, clips, it.scope, t, b)
		}
	}

	if p.clips == nil {
		p.clips = NewPainter(0, 0, nil, image.Rectangle{})
	}
	c := p.clips
	c.Dasher.SetBounds(b.Dx(), b.Dy())
	c.Dasher.ShareDrawBudget(p.Dasher)
	c.Scanner.Targ = b

	drawItems := func(dstRGBA *image.RGBA, dstAlpha *image.Alpha) {
		c.Scanner.Dest, c.Scanner.DestAlpha, c.Scanner.Touched = dstRGBA, dstAlpha, image.Rectangle{}
		for i := range items {
			it := &items[i]
			if !it.visible {
				continue
			}
			var subClip clipResult
			if it.scope >= 0 {
				subClip = clips[it.scope]
			}
			c.currentColor = it.style.color
			svgp := SvgPath{PathStyle: it.style, Path: it.path}
			svgp.drawThroughTrim(c, 1, t, subClip, it.trim, it.lens)
		}
	}

	// First pass: find touched bounding window without rasterizing.
	drawItems(nil, nil)
	area := c.Scanner.Touched
	if parent.set {
		area = area.Intersect(parent.rect.Add(b.Min))
	}
	if area.Empty() {
		res := clipResult{set: true, empty: true}
		clips[scopeIdx] = res
		return res
	}

	var mask *image.Alpha
	if sc.isMask {
		drawItems(c.scratch(area), nil)
		drawn := c.Scanner.Dest
		c.Scanner.Dest = nil
		mask = p.allocAlpha(area, false)
		for i := range mask.Pix {
			px := drawn.Pix[4*i : 4*i+4 : 4*i+4]
			// Premultiplied sRGB luminance (matches legacy clip.go).
			mask.Pix[i] = uint8((2126*uint32(px[0]) + 7152*uint32(px[1]) + 722*uint32(px[2])) / 10000)
		}
	} else {
		mask = p.allocAlpha(area, true)
		drawItems(nil, mask)
		c.Scanner.DestAlpha = nil
	}

	mask.Rect = mask.Rect.Sub(b.Min)
	if parent.set && parent.mask != nil {
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
	clips[scopeIdx] = res
	return res
}
