// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"math"
)

type elemTracks struct {
	mask PropMask
	smil map[PropID][]*animTrack
	css  map[PropID][]*animTrack
}

type animSetImpl struct {
	byElem           map[ElemID]*elemTracks
	allTracks        []*animTrack
	hasAnyAnim       bool
	usesCurrentColor bool
	tl               Timeline
}

func animatedImpl(a *AnimSet) bool {
	impl, ok := a.impl.(*animSetImpl)
	if !ok {
		return false
	}
	return impl.hasAnyAnim
}

func usesCurrentColorImpl(a *AnimSet) bool {
	impl, ok := a.impl.(*animSetImpl)
	if !ok {
		return false
	}
	return impl.usesCurrentColor
}

func parseDeclsImpl(raw string) []Decl {
	return parseDeclarations(raw)
}

func animatedPropsImpl(a *AnimSet, e ElemID) PropMask {
	impl, ok := a.impl.(*animSetImpl)
	if !ok {
		return PropMask{}
	}
	if et := impl.byElem[e]; et != nil {
		return et.mask
	}
	return PropMask{}
}

func timelineImpl(a *AnimSet) Timeline {
	impl, ok := a.impl.(*animSetImpl)
	if !ok {
		return Timeline{}
	}
	return impl.tl
}

func evalImpl(a *AnimSet, e ElemID, p PropID, t float64, base Value, ctx *EvalCtx) (Value, bool) {
	impl, ok := a.impl.(*animSetImpl)
	if !ok {
		return Value{}, false
	}
	et := impl.byElem[e]
	if et == nil || !et.mask.Has(p) {
		return Value{}, false
	}

	cur := base
	modified := false

	// 1. SMIL sandwich: sort active SMIL tracks by (activeBegin asc, docOrder asc).
	if smilList := et.smil[p]; len(smilList) > 0 {
		type activeSMIL struct {
			tr    *animTrack
			state sampleState
		}
		var stackBuf [8]activeSMIL
		active := stackBuf[:0]
		for _, tr := range smilList {
			st := tr.timing.sample(t)
			if st.active {
				active = append(active, activeSMIL{tr: tr, state: st})
			}
		}
		// Insertion sort (tiny slice, zero allocs).
		for i := 1; i < len(active); i++ {
			curItem := active[i]
			j := i - 1
			for j >= 0 && smilBefore(curItem.state.activeBegin, curItem.tr.docOrder, active[j].state.activeBegin, active[j].tr.docOrder) {
				active[j+1] = active[j]
				j--
			}
			active[j+1] = curItem
		}

		for _, item := range active {
			tv := evalTrackValue(item.tr, item.state, cur, ctx)
			if item.tr.additive && item.tr.form != formTo && item.tr.form != formSet {
				cur = addValues(cur, tv, p, ctx)
			} else {
				cur = tv
			}
			modified = true
		}
	}

	// 2. CSS animations on top of SMIL, in animation-name declaration order.
	if cssList := et.css[p]; len(cssList) > 0 {
		for _, tr := range cssList {
			st := tr.timing.sample(t)
			if !st.active {
				continue
			}
			cur = evalTrackValue(tr, st, cur, ctx)
			modified = true
		}
	}

	if !modified {
		return Value{}, false
	}

	// Post-composition clamping.
	switch p {
	case PropOpacity, PropFillOpacity, PropStrokeOpacity:
		if cur.Kind == ValNumber {
			cur.Num = clamp01(cur.Num)
		}
	case PropStrokeWidth:
		if cur.Kind == ValLength && cur.Num < 0 {
			cur.Num = 0
		}
	}

	return cur, true
}

func smilBefore(beginA float64, orderA int, beginB float64, orderB int) bool {
	if math.Abs(beginA-beginB) > 1e-12 {
		return beginA < beginB
	}
	return orderA < orderB
}

func evalTrackValue(tr *animTrack, st sampleState, base Value, ctx *EvalCtx) Value {
	if len(tr.kf) == 0 {
		return base
	}

	if tr.form == formSet {
		return tr.kf[0].v
	}

	if tr.form == formTo {
		toVal := tr.kf[0].v
		if tr.calc == calcDiscrete {
			if st.progress < 0.5 {
				return base
			}
			return toVal
		}
		p := tr.kf[0].ease.eval(st.progress)
		return interpolateValues(base, toVal, p, tr.prop, false, ctx)
	}

	if tr.form == formFromBy && len(tr.kf) == 2 {
		fromVal := tr.kf[0].v
		var toVal Value
		if fromVal.Kind == ValTransformList && tr.kf[1].v.Kind == ValTransformList {
			toVal = addByTransformValue(fromVal, tr.kf[1].v)
		} else {
			toVal = addValues(fromVal, tr.kf[1].v, tr.prop, ctx)
		}
		var out Value
		if tr.calc == calcDiscrete {
			if st.progress < 0.5 {
				out = fromVal
			} else {
				out = toVal
			}
		} else {
			p := tr.kf[0].ease.eval(st.progress)
			out = interpolateValues(fromVal, toVal, p, tr.prop, false, ctx)
		}
		if tr.accumulate && st.iteration > 0 {
			out = accumulateValue(out, toVal, st.iteration, tr.prop, ctx)
		}
		return out
	}

	// General keyframe evaluation (CSS @keyframes, SMIL values, SMIL from/to, SMIL by).
	var out Value
	if tr.origin == originSMIL && tr.calc == calcDiscrete {
		out = evalDiscreteSMIL(tr.kf, st.progress, base)
	} else {
		out = evalKeyframesContinuous(tr, st.progress, base, ctx)
	}

	if tr.origin == originSMIL && tr.accumulate && st.iteration > 0 && tr.calc != calcDiscrete {
		lastVal := resolveKeyframeValue(tr.kf[len(tr.kf)-1], base)
		out = accumulateValue(out, lastVal, st.iteration, tr.prop, ctx)
	}
	return out
}

func resolveKeyframeValue(kf keyframe, base Value) Value {
	if kf.fromBase {
		return base
	}
	return kf.v
}

func evalDiscreteSMIL(kfs []keyframe, p float64, base Value) Value {
	idx := 0
	for i := range kfs {
		if kfs[i].off <= p+1e-12 {
			idx = i
		} else {
			break
		}
	}
	return resolveKeyframeValue(kfs[idx], base)
}

func evalKeyframesContinuous(tr *animTrack, p float64, base Value, ctx *EvalCtx) Value {
	kfs := tr.kf
	n := len(kfs)
	if n == 1 {
		return resolveKeyframeValue(kfs[0], base)
	}
	if p <= kfs[0].off {
		return resolveKeyframeValue(kfs[0], base)
	}
	if p >= kfs[n-1].off {
		return resolveKeyframeValue(kfs[n-1], base)
	}

	// Find segment [i, i+1] containing p (handling duplicate offsets by picking last kfs[i].off <= p).
	seg := 0
	for i := 0; i < n-1; i++ {
		if kfs[i].off <= p {
			seg = i
		} else {
			break
		}
	}
	// If duplicate offsets exist at the exact same timestamp, advance past zero-width segments.
	for seg < n-1 && kfs[seg+1].off <= kfs[seg].off+1e-12 {
		seg++
	}
	if seg >= n-1 {
		return resolveKeyframeValue(kfs[n-1], base)
	}

	k0 := kfs[seg]
	k1 := kfs[seg+1]
	span := k1.off - k0.off
	local := clamp01((p - k0.off) / span)
	eased := k0.ease.eval(local)

	v0 := resolveKeyframeValue(k0, base)
	v1 := resolveKeyframeValue(k1, base)
	return interpolateValues(v0, v1, eased, tr.prop, tr.origin == originCSS, ctx)
}

func nextChangeImpl(a *AnimSet, t float64) (float64, bool) {
	impl, ok := a.impl.(*animSetImpl)
	if !ok || len(impl.allTracks) == 0 {
		return 0, false
	}

	best := math.Inf(1)
	found := false
	for _, tr := range impl.allTracks {
		nt, has, continuous := trackNextChange(tr, t)
		if continuous {
			// Any continuous animation active right now means the timeline
			// changes continuously after t; contract returns (0, false).
			return 0, false
		}
		if !has || nt <= t+1e-12 {
			continue
		}
		if nt < best {
			best = nt
			found = true
		}
	}
	if !found || math.IsInf(best, 1) {
		return 0, false
	}
	return best, true
}

func trackNextChange(tr *animTrack, t float64) (next float64, has bool, continuous bool) {
	tm := tr.timing
	if tm.paused {
		return 0, false, false
	}

	// Check if there is a future interval starting > t.
	nextStart := math.Inf(1)
	if nextIV, ok := tm.nextIntervalAfter(t); ok {
		nextStart = nextIV.begin
	}

	// Check if currently inside an active interval [iv.begin, iv.end).
	iv, hasCur := tm.latestIntervalAt(t)
	if !hasCur || t >= iv.end-1e-12 {
		// Not currently inside an active interval.
		if !math.IsInf(nextStart, 1) {
			return nextStart, true, false
		}
		return 0, false, false
	}

	// Inside active interval [iv.begin, iv.end).
	// Special cases where value is constant during the active interval:
	if tr.form == formSet || math.IsNaN(tm.simpleDur) || math.IsInf(tm.simpleDur, 1) || tm.simpleDur <= 0 {
		best := nextStart
		if !tm.fillForwards && !math.IsInf(iv.end, 1) && iv.end > t+1e-12 && iv.end < best {
			best = iv.end
		}
		if !math.IsInf(best, 1) {
			return best, true, false
		}
		return 0, false, false
	}

	elapsed := t - iv.begin
	if elapsed < 0 {
		elapsed = 0
	}
	iter := int(math.Floor(elapsed / tm.simpleDur))
	iterStart := iv.begin + float64(iter)*tm.simpleDur
	iterEnd := iterStart + tm.simpleDur
	rawP := clamp01((elapsed - float64(iter)*tm.simpleDur) / tm.simpleDur)

	// Candidate transition at end of active interval (if remove / not frozen, or next interval starts).
	best := nextStart
	if !tm.fillForwards && !math.IsInf(iv.end, 1) && iv.end > t+1e-12 && iv.end < best {
		best = iv.end
	}

	consider := func(cand float64) {
		if !math.IsInf(iv.end, 1) {
			if cand > iv.end+1e-12 {
				return
			}
			if cand >= iv.end-1e-12 {
				cand = iv.end
			}
		}
		if cand > t+1e-12 && cand < best {
			best = cand
		}
	}

	// SMIL to-animation has a single keyframe interpolating from base -> to.
	if tr.form == formTo {
		if len(tr.kf) == 0 {
			if !math.IsInf(best, 1) {
				return best, true, false
			}
			return 0, false, false
		}
		toVal := tr.kf[0].v
		discreteTo := tr.calc == calcDiscrete || isDiscreteProp(tr.prop) ||
			((tr.prop == PropFill || tr.prop == PropStroke) &&
				(toVal.Paint == PaintNone || toVal.Paint == PaintURL || toVal.Paint == PaintInherit))
		if !discreteTo {
			return 0, false, true
		}
		if rawP < 0.5-1e-12 {
			consider(iterStart + 0.5*tm.simpleDur)
		}
		if iterEnd < iv.end-1e-12 {
			consider(iterEnd)
		}
		if !math.IsInf(best, 1) {
			return best, true, false
		}
		return 0, false, false
	}

	// Next transition inside current iteration for SMIL discrete animations.
	if tr.origin == originSMIL && tr.calc == calcDiscrete {
		// Find first keyframe offset strictly > rawP.
		for _, kf := range tr.kf {
			if kf.off > rawP+1e-12 {
				consider(iterStart + kf.off*tm.simpleDur)
				break
			}
		}
		// Next iteration start (wraps back to kf[0]) if still within active interval.
		if iterEnd < iv.end-1e-12 {
			consider(iterEnd)
		}
		if !math.IsInf(best, 1) {
			return best, true, false
		}
		return 0, false, false
	}

	// Check current segment easing (CSS or SMIL linear/spline/discrete-prop).
	reversed := isDirectionReversed(iter, tm.direction)
	dirP := applyDirection(rawP, iter, tm.direction)
	segIdx := segmentIndexFor(tr.kf, dirP, reversed)
	if segIdx < 0 || segIdx >= len(tr.kf)-1 {
		// Single keyframe or boundary.
		if iterEnd < iv.end-1e-12 {
			consider(iterEnd)
		}
		if !math.IsInf(best, 1) {
			return best, true, false
		}
		return 0, false, false
	}

	k0 := tr.kf[segIdx]
	k1 := tr.kf[segIdx+1]
	segEase := k0.ease
	discreteSeg := tr.calc == calcDiscrete || isDiscreteProp(tr.prop) || isDiscreteKeyframePair(k0, k1, tr.prop)

	if segEase.kind == easeSteps {
		span := k1.off - k0.off
		if span > 1e-12 {
			local := clamp01((dirP - k0.off) / span)
			var nextDirP float64
			if !reversed {
				if nb, ok := segEase.nextStepBoundary(local); ok {
					nextDirP = k0.off + nb*span
				} else {
					nextDirP = k1.off
				}
			} else {
				if pb, ok := segEase.prevStepBoundary(local); ok {
					nextDirP = k0.off + pb*span
				} else {
					nextDirP = k0.off
				}
			}
			var nextRawP float64
			if reversed {
				nextRawP = 1 - nextDirP
			} else {
				nextRawP = nextDirP
			}
			cand := iterStart + nextRawP*tm.simpleDur
			if cand <= t+1e-12 && iterEnd < iv.end-1e-12 {
				cand = iterEnd
			}
			consider(cand)
		}
		if iterEnd < iv.end-1e-12 {
			consider(iterEnd)
		}
		if !math.IsInf(best, 1) {
			return best, true, false
		}
		return 0, false, false
	}

	if discreteSeg {
		// CSS discrete flips at 50% of the eased segment (or endpoint for visibility).
		span := k1.off - k0.off
		if span > 1e-12 {
			mid := k0.off + 0.5*span
			var nextDirP float64
			if !reversed {
				if dirP < mid-1e-12 {
					nextDirP = mid
				} else {
					nextDirP = k1.off
				}
			} else {
				if dirP > mid+1e-12 {
					nextDirP = mid
				} else {
					nextDirP = k0.off
				}
			}
			var nextRawP float64
			if reversed {
				nextRawP = 1 - nextDirP
			} else {
				nextRawP = nextDirP
			}
			consider(iterStart + nextRawP*tm.simpleDur)
		}
		if iterEnd < iv.end-1e-12 {
			consider(iterEnd)
		}
		if !math.IsInf(best, 1) {
			return best, true, false
		}
		return 0, false, false
	}

	// Continuous smooth interpolation active right now.
	return 0, false, true
}

func segmentIndexFor(kfs []keyframe, p float64, reversed bool) int {
	n := len(kfs)
	if n < 2 {
		return -1
	}
	if reversed {
		// Moving downward from 1 -> 0: at exact boundary p == kfs[i].off, active segment is [i-1, i].
		for i := n - 1; i >= 1; i-- {
			if p <= kfs[i].off+1e-12 && p > kfs[i-1].off+1e-12 {
				return i - 1
			}
		}
		return 0
	}
	seg := 0
	for i := 0; i < n-1; i++ {
		if kfs[i].off <= p+1e-12 {
			seg = i
		} else {
			break
		}
	}
	if seg >= n-1 {
		seg = n - 2
	}
	return seg
}

func isDiscreteKeyframePair(k0, k1 keyframe, p PropID) bool {
	if k0.fromBase || k1.fromBase {
		return false
	}
	if p == PropFill || p == PropStroke {
		if k0.v.Paint == PaintNone || k0.v.Paint == PaintURL || k0.v.Paint == PaintInherit ||
			k1.v.Paint == PaintNone || k1.v.Paint == PaintURL || k1.v.Paint == PaintInherit {
			return true
		}
	}
	if p == PropStrokeDasharray {
		if k0.v.Kind != ValNumberList || k1.v.Kind != ValNumberList {
			return true
		}
		if lcmInt(len(k0.v.List), len(k1.v.List)) > 64 {
			return true
		}
	}
	return false
}
