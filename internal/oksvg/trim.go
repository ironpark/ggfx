// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"math"

	"golang.org/x/image/math/fixed"

	"github.com/ironpark/ggfx/internal/rasterx"
)

type trimSpec struct {
	start, end, offset float64
	set                bool
}

type trimMode uint8

const (
	trimPassThrough trimMode = iota
	trimEmpty
	trimCut
)

func (tr trimSpec) classify() (mode trimMode, a, b float64) {
	if !tr.set || math.IsNaN(tr.start) || math.IsNaN(tr.end) {
		return trimPassThrough, 0, 1
	}
	s := max(0, min(tr.start, 1))
	e := max(0, min(tr.end, 1))
	span := e - s
	if span <= 0 {
		return trimEmpty, 0, 0
	}
	if span >= 1 {
		return trimPassThrough, 0, 1
	}
	off := tr.offset
	if math.IsNaN(off) || math.IsInf(off, 0) {
		off = 0
	}
	a = s + off
	a -= math.Floor(a)
	b = a + span
	return trimCut, a, b
}

// emitTrimmedStroke emits trimmed float64 geometry directly into r (transformed
// by m into stroker space, avoiding 1/64-user-unit quantization) and updates
// dash phase via r.SetPhase so dashes stay anchored to the original path.
func emitTrimmedStroke(lt *lengthTable, a, b float64, m rasterx.Matrix2D, r *rasterx.Dasher, hasDash bool, dashOffsetUser, scale float64) {
	if lt == nil || lt.total <= 0 {
		return
	}
	L := lt.total
	tf := func(x, y float64) fixed.Point26_6 {
		px, py := m.Transform(x, y)
		return fixed.Point26_6{
			X: fixed.Int26_6(px * 64),
			Y: fixed.Int26_6(py * 64),
		}
	}
	emitSeg := func(kind arcSegKind, pts [8]float64) {
		switch kind {
		case arcLine:
			r.Line(tf(pts[2], pts[3]))
		case arcQuad:
			r.QuadBezier(tf(pts[2], pts[3]), tf(pts[4], pts[5]))
		case arcCubic:
			r.CubeBezier(tf(pts[2], pts[3]), tf(pts[4], pts[5]), tf(pts[6], pts[7]))
		}
	}

	// Single closed subpath wrapping across its seam: emit as ONE continuous
	// open piece passing through the start vertex so it gets a join, not caps.
	if b > 1 && len(lt.subpaths) == 1 && lt.subpaths[0].closed && lt.subpaths[0].end > lt.subpaths[0].start {
		sub := &lt.subpaths[0]
		cut1 := a * L
		cut2 := (b - 1) * L
		if hasDash {
			r.SetPhase((dashOffsetUser + (cut1 - sub.start)) * scale)
		}
		started := false
		emitSubpathRange(sub, cut1, sub.end, tf, emitSeg, r, &started)
		emitSubpathRange(sub, sub.start, cut2, tf, emitSeg, r, &started)
		if started {
			r.Stop(false)
		}
		return
	}

	// Build 1 or 2 windows in [0, L].
	var windows [2][2]float64
	nWin := 1
	if b <= 1 {
		windows[0] = [2]float64{a * L, b * L}
	} else {
		windows[0] = [2]float64{a * L, L}
		windows[1] = [2]float64{0, (b - 1) * L}
		nWin = 2
	}

	for w := range nWin {
		w0, w1 := windows[w][0], windows[w][1]
		for si := range lt.subpaths {
			sub := &lt.subpaths[si]
			// Zero-length subpath: preserve round-cap dots iff its position falls in the window.
			if sub.end == sub.start {
				if w0 <= sub.start && sub.start <= w1 {
					if hasDash {
						r.SetPhase(dashOffsetUser * scale)
					}
					p0 := tf(sub.startPt[0], sub.startPt[1])
					r.Start(p0)
					if sub.hasLine {
						r.Line(p0)
					}
					r.Stop(sub.closed)
				}
				continue
			}
			// Fully inside window: emit whole subpath preserving Close join.
			if sub.start >= w0 && sub.end <= w1 {
				if hasDash {
					r.SetPhase(dashOffsetUser * scale)
				}
				r.Start(tf(sub.startPt[0], sub.startPt[1]))
				// Emit all segments of the subpath and close with Stop(sub.closed)
				// so Dasher/Stroker preserves the closing join on closed subpaths.
				nSegs := len(sub.segs)
				for k := range nSegs {
					emitSeg(sub.segs[k].kind, sub.segs[k].pts)
				}
				r.Stop(sub.closed)
				continue
			}
			// Partial overlap: emit open piece [lo, hi].
			if sub.end > w0 && sub.start < w1 {
				lo := max(w0, sub.start)
				hi := min(w1, sub.end)
				if hi <= lo {
					continue
				}
				if hasDash {
					r.SetPhase((dashOffsetUser + (lo - sub.start)) * scale)
				}
				started := false
				emitSubpathRange(sub, lo, hi, tf, emitSeg, r, &started)
				if started {
					r.Stop(false)
				}
			}
		}
	}
}

func emitSubpathRange(
	sub *arcSubpath,
	lo, hi float64,
	tf func(float64, float64) fixed.Point26_6,
	emitSeg func(arcSegKind, [8]float64),
	r *rasterx.Dasher,
	started *bool,
) {
	if hi <= lo {
		return
	}
	prevCum := sub.start
	for i := range sub.segs {
		seg := &sub.segs[i]
		segStart := prevCum
		segEnd := seg.cum
		prevCum = segEnd
		if segEnd <= lo {
			continue
		}
		if segStart >= hi {
			break
		}
		if seg.len <= 0 {
			continue
		}
		var t0, t1 float64
		if lo <= segStart {
			t0 = 0
		} else {
			t0 = seg.paramAtLength(lo - segStart)
		}
		if hi >= segEnd {
			t1 = 1
		} else {
			t1 = seg.paramAtLength(hi - segStart)
		}
		if t1 <= t0 {
			continue
		}
		kind, pts := splitSeg(seg, t0, t1)
		if !*started {
			r.Start(tf(pts[0], pts[1]))
			*started = true
		}
		emitSeg(kind, pts)
	}
}
