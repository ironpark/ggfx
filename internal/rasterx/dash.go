// Copyright 2017 by the rasterx Authors. All rights reserved.
//_
// created: 2017 by S.R.Wiley

package rasterx

import (
	"math"

	"golang.org/x/image/math/fixed"
)

const (
	// minDashCycle is the shortest full dash cycle (1/4 px in 26.6 fixed point)
	// that is dashed individually; shorter patterns render once with coverage
	// scaled by their cap-adjusted on/off duty cycle.
	minDashCycle = 16
	// dashBudgetPerPixel budgets actual dash transitions per draw at 4·W·H:
	// every pixel in a W×H target can be crossed by two orthogonal 1-px-period
	// dashed strokes (2 transitions/px × 2 axes = 4·W·H), giving 1,048,576
	// transitions on 512×512 (>15× a 135-row 512-wide 1-px-dash grid) while
	// keeping per-draw dash memory proportional to target area.
	//
	// Dashes wholly right, above, or below the expanded window are always
	// skipped arithmetically (they provably touch no visible pixel). Left-offscreen
	// dashes are emitted and counted as actual transitions against this same
	// per-draw budget so ordinary paths match exact column-0 float32 accumulation
	// bit-for-bit; only once the budget threshold is reached are remaining
	// left-offscreen dashes skipped arithmetically (an accepted approximation of
	// at most a last-bit coverage difference on extreme offscreen segments).
	dashBudgetPerPixel = 4
	// minDashBudget provides transition headroom on small targets.
	minDashBudget = 16384
)

type dashFallbackOp struct {
	p    fixed.Point26_6
	move bool
}

// dashDrawState holds the shared per-draw dash transition counter and budget
// across a main Dasher and any auxiliary clip/mask Dashers used during a draw.
type dashDrawState struct {
	count  int
	budget int
}

// capExtProbe measures whether a CapFunc extends past the endpoint along the
// stroke tangent (e.g. RoundCap, SquareCap, CubicCap, QuadraticCap vs ButtCap).
type capExtProbe struct {
	maxAbsX fixed.Int26_6
}

func (p *capExtProbe) record(pt fixed.Point26_6) {
	ax := pt.X
	if ax < 0 {
		ax = -ax
	}
	if ax > p.maxAbsX {
		p.maxAbsX = ax
	}
}

func (p *capExtProbe) Start(a fixed.Point26_6)            { p.record(a) }
func (p *capExtProbe) Line(b fixed.Point26_6)             { p.record(b) }
func (p *capExtProbe) QuadBezier(b, c fixed.Point26_6)    { p.record(b); p.record(c) }
func (p *capExtProbe) CubeBezier(b, c, d fixed.Point26_6) { p.record(b); p.record(c); p.record(d) }
func (p *capExtProbe) Stop(bool)                          {}

func capHasExtension(capFn CapFunc, halfWidth fixed.Int26_6) bool {
	if capFn == nil || halfWidth <= 0 {
		return false
	}
	var probe capExtProbe
	capFn(&probe, fixed.Point26_6{}, fixed.Point26_6{Y: halfWidth})
	return probe.maxAbsX > 0
}

// Dasher struct extends the Stroker and can draw
// dashed lines with end capping
type Dasher struct {
	Stroker
	Dashes                    []fixed.Int26_6
	dashPlace                 int
	firstDashIsGap, dashIsGap bool
	deltaDash, DashOffset     fixed.Int26_6
	dashCycle                 int64
	dashTotalF                float64
	dashOnF                   float64
	dashNumOn                 int
	dashBudget                int
	ownDrawState              dashDrawState
	drawState                 *dashDrawState
	subpixelDash              bool
	inDutyFallback            bool
	fallbackPts               []dashFallbackOp
	sgm                       Rasterx
	// sgm allows us to switch between dashing
	// and non-dashing rasterizers in the SetStroke function.
}

func (r *Dasher) effectiveBudget() int {
	if r.dashBudget > 0 {
		return r.dashBudget
	}
	return minDashBudget
}

func (r *Dasher) activeState() *dashDrawState {
	if r.drawState != nil {
		return r.drawState
	}
	if r.ownDrawState.budget <= 0 {
		r.ownDrawState.budget = r.effectiveBudget()
	}
	return &r.ownDrawState
}

// BeginDraw starts a draw-wide dash transition budget across all stroked paths
// (including auxiliary clip/mask painters linked via ShareDrawBudget) until
// EndDraw is called.
func (r *Dasher) BeginDraw() {
	r.ownDrawState = dashDrawState{count: 0, budget: r.effectiveBudget()}
	r.drawState = &r.ownDrawState
}

// EndDraw ends the draw-wide dash budget started by BeginDraw.
func (r *Dasher) EndDraw() {
	r.drawState = nil
	r.ownDrawState.count = 0
}

// ShareDrawBudget binds r to parent's active per-draw transition counter and
// budget so auxiliary clip/mask rasterization shares one budget with the draw.
func (r *Dasher) ShareDrawBudget(parent *Dasher) {
	if parent != nil && parent.drawState != nil {
		r.drawState = parent.drawState
	}
}

// SetBounds updates the target dimensions and scales the per-draw dash
// transition budget proportionally to the target pixel area.
func (r *Dasher) SetBounds(width, height int) {
	r.Stroker.SetBounds(width, height)
	area := int64(max(width, 0)) * int64(max(height, 0))
	budget := area * dashBudgetPerPixel
	if budget < minDashBudget {
		budget = minDashBudget
	}
	if budget > math.MaxInt32 {
		budget = math.MaxInt32
	}
	r.dashBudget = int(budget)
	if r.drawState == nil {
		r.ownDrawState.budget = r.dashBudget
	}
}

// Clear resets the dasher path state and underlying scanner.
func (r *Dasher) Clear() {
	if r.drawState == nil {
		r.ownDrawState.count = 0
	}
	r.inDutyFallback = false
	r.fallbackPts = r.fallbackPts[:0]
	r.Stroker.Clear()
}

// dutyCoverage computes the averaged stroke coverage in [0, 1] for the active
// dash pattern, including cap footprints for extending caps (round/square/etc.)
// so zero-length dot patterns retain visible coverage while butt caps use pure
// centreline duty cycle.
func (r *Dasher) dutyCoverage() float64 {
	if r.dashTotalF <= 0 {
		return 1
	}
	var capExt float64
	if r.u > 0 {
		if capHasExtension(r.CapL, r.u) {
			capExt += float64(r.u) / 64.0
		}
		if capHasExtension(r.CapT, r.u) {
			capExt += float64(r.u) / 64.0
		}
	}
	cov := (r.dashOnF + float64(r.dashNumOn)*capExt) / r.dashTotalF
	return max(0, min(1, cov))
}

// Draw renders the accumulated stroke, applying cap-adjusted duty-cycle coverage
// scaling for sub-quarter-pixel dash patterns or any segments deferred after the
// per-draw transition budget was exhausted.
func (r *Dasher) Draw() {
	cs, hasCS := r.Scanner.(interface{ SetCoverageScale(float64) })
	if r.subpixelDash {
		if hasCS {
			defer cs.SetCoverageScale(1)
			cs.SetCoverageScale(r.dutyCoverage())
		}
		r.Stroker.Draw()
		return
	}
	r.Stroker.Draw()
	if len(r.fallbackPts) > 0 {
		r.Scanner.Clear()
		for _, op := range r.fallbackPts {
			if op.move {
				r.Scanner.Start(op.p)
			} else {
				r.Scanner.Line(op.p)
			}
		}
		r.fallbackPts = r.fallbackPts[:0]
		r.inDutyFallback = false
		if hasCS {
			defer cs.SetCoverageScale(1)
			cs.SetCoverageScale(r.dutyCoverage())
		}
		r.Scanner.Draw()
	}
}

func (r *Dasher) fbOut(p fixed.Point26_6, move bool) {
	if ma, ok := r.Out.(*MatrixAdder); ok {
		p = ma.M.TFixed(p)
	}
	r.fallbackPts = append(r.fallbackPts, dashFallbackOp{p: p, move: move})
}

func (r *Dasher) emitFallbackQuad(a, b, bnorm fixed.Point26_6) {
	r.fbOut(a.Add(bnorm), true)
	r.fbOut(a.Sub(bnorm), false)
	r.fbOut(b.Sub(bnorm), false)
	r.fbOut(b.Add(bnorm), false)
	r.fbOut(a.Add(bnorm), false)
}

func (r *Dasher) enterDutyFallback() {
	if r.inDutyFallback {
		return
	}
	if !r.dashIsGap {
		ButtCap(r.out(), r.a, r.ln)
		r.dashIsGap = true
	}
	r.inDutyFallback = true
}

// joinF overides stroker joinF during dashed stroking, because we need to slightly modify
// the the call as below to handle the case of the join being in a dash gap.
func (r *Dasher) joinF() {
	if len(r.Dashes) == 0 || !r.inStroke || (!r.dashIsGap && !r.inDutyFallback) {
		r.Stroker.joinF()
	}
}

// Start starts a dashed line
func (r *Dasher) Start(a fixed.Point26_6) {
	// Advance dashPlace to the dashOffset start point and set deltaDash
	if len(r.Dashes) > 0 {
		st := r.activeState()
		if st.count >= st.budget {
			r.firstDashIsGap = true
			r.dashIsGap = true
			r.inDutyFallback = true
			r.Stroker.Start(a)
			return
		}
		r.inDutyFallback = false
		var period int64
		for _, d := range r.Dashes {
			period += int64(d)
		}
		if period > 0 {
			cycle := period
			if len(r.Dashes)%2 != 0 {
				cycle *= 2
			}
			off := int64(r.DashOffset) % cycle
			if off < 0 {
				off += cycle
			}
			r.deltaDash = fixed.Int26_6(off)
		} else {
			r.deltaDash = 0
		}
		r.dashIsGap = false
		r.dashPlace = 0
		for r.deltaDash > r.Dashes[r.dashPlace] {
			r.deltaDash -= r.Dashes[r.dashPlace]
			r.dashIsGap = !r.dashIsGap
			r.dashPlace++
			if r.dashPlace == len(r.Dashes) {
				r.dashPlace = 0
			}
		}
		r.firstDashIsGap = r.dashIsGap
	}
	r.Stroker.Start(a)
}

// clipSegToBox clips the segment (ax,ay)->(bx,by) against [minX,maxX]×[minY,maxY]
// using Liang-Barsky, returning the visible parameter interval [t0, t1] ⊆ [0, 1].
func clipSegToBox(ax, ay, bx, by, minX, minY, maxX, maxY float64) (t0, t1 float64, visible bool) {
	t0, t1 = 0, 1
	dx, dy := bx-ax, by-ay
	clip := func(p, q float64) bool {
		if p == 0 {
			return q >= 0
		}
		rv := q / p
		if p < 0 {
			if rv > t1 {
				return false
			}
			if rv > t0 {
				t0 = rv
			}
		} else {
			if rv < t0 {
				return false
			}
			if rv < t1 {
				t1 = rv
			}
		}
		return true
	}
	if !math.IsInf(minX, -1) && !clip(-dx, ax-minX) {
		return 0, 0, false
	}
	if !clip(dx, maxX-ax) || !clip(-dy, ay-minY) || !clip(dy, maxY-ay) {
		return 0, 0, false
	}
	return t0, t1, true
}

type segClipInfo struct {
	// rtbStart..rtbEnd is the Dasher-space distance interval clipped against
	// right/top/bottom only ([-Inf, W+mx]×[-my, H+my]); dashes outside this
	// interval provably touch no visible pixel.
	hasRTB   bool
	rtbStart int64
	rtbEnd   int64
	// fullStart..fullEnd is clipped against all 4 sides ([-mx, W+mx]×[-my, H+my]).
	// Left-offscreen regions outside fullStart..fullEnd are emitted up to the
	// per-draw budget threshold, then skipped arithmetically.
	hasLeft   bool
	fullVis   bool
	fullStart int64
	fullEnd   int64
}

func (r *Dasher) segmentClipInfo(a, b fixed.Point26_6, segLen int64) segClipInfo {
	if segLen <= 0 || r.bounds.X <= 0 || r.bounds.Y <= 0 {
		return segClipInfo{}
	}
	rad := float64(max(2*r.u, 0)) / 64.0
	var sax, say, sbx, sby, marginX, marginY float64
	if r.Out == nil {
		sax, say = float64(a.X)/64, float64(a.Y)/64
		sbx, sby = float64(b.X)/64, float64(b.Y)/64
		marginX, marginY = rad+2.0, rad+2.0
	} else if ma, ok := r.Out.(*MatrixAdder); ok {
		sax, say = ma.M.Transform(float64(a.X)/64, float64(a.Y)/64)
		sbx, sby = ma.M.Transform(float64(b.X)/64, float64(b.Y)/64)
		marginX = rad*math.Hypot(ma.M.A, ma.M.C) + 2.0
		marginY = rad*math.Hypot(ma.M.B, ma.M.D) + 2.0
	} else {
		return segClipInfo{}
	}

	w, h := float64(r.bounds.X), float64(r.bounds.Y)
	maxX := w + marginX
	minY := -marginY
	maxY := h + marginY

	// Fast path: both endpoints lie inside the expanded target box.
	if sax >= -marginX && sax <= maxX && say >= minY && say <= maxY &&
		sbx >= -marginX && sbx <= maxX && sby >= minY && sby <= maxY {
		return segClipInfo{}
	}

	t0, t1, visRTB := clipSegToBox(sax, say, sbx, sby, math.Inf(-1), minY, maxX, maxY)
	if !visRTB {
		return segClipInfo{hasRTB: true, rtbStart: segLen, rtbEnd: 0}
	}

	var info segClipInfo
	if t0 > 0 || t1 < 1 {
		info.hasRTB = true
		info.rtbStart = max(0, min(int64(math.Floor(t0*float64(segLen))), segLen))
		info.rtbEnd = max(0, min(int64(math.Ceil(t1*float64(segLen))), segLen))
	}
	if sax < -marginX || sbx < -marginX {
		info.hasLeft = true
		ft0, ft1, visFull := clipSegToBox(sax, say, sbx, sby, -marginX, minY, maxX, maxY)
		info.fullVis = visFull
		if visFull {
			info.fullStart = max(0, min(int64(math.Floor(ft0*float64(segLen))), segLen))
			info.fullEnd = max(0, min(int64(math.Ceil(ft1*float64(segLen))), segLen))
		} else {
			info.fullStart = segLen
			info.fullEnd = 0
		}
	}
	return info
}

// skipDashesBefore advances the dash phase past all transitions strictly before
// limit (where limit <= nlt+rem), skipping whole cycles in O(1).
func (r *Dasher) skipDashesBefore(nlt, rem, limit int64) (int64, int64, bool) {
	nl := int64(r.Dashes[r.dashPlace]) - int64(r.deltaDash)
	if nlt+nl >= limit {
		return nlt, rem, false
	}
	nlt += nl
	rem -= nl
	r.deltaDash = 0
	r.dashIsGap = !r.dashIsGap
	r.dashPlace++
	if r.dashPlace == len(r.Dashes) {
		r.dashPlace = 0
	}

	if r.dashCycle > 0 {
		dist := limit - nlt
		if dist > r.dashCycle {
			k := (dist - 1) / r.dashCycle
			skip := k * r.dashCycle
			nlt += skip
			rem -= skip
		}
	}

	for {
		nl = int64(r.Dashes[r.dashPlace])
		if nlt+nl >= limit {
			break
		}
		nlt += nl
		rem -= nl
		r.dashIsGap = !r.dashIsGap
		r.dashPlace++
		if r.dashPlace == len(r.Dashes) {
			r.dashPlace = 0
		}
	}
	return nlt, rem, true
}

// lineF overides stroker lineF to modify the the call as below
// while performing the join in a dashed stroke.
func (r *Dasher) lineF(b fixed.Point26_6) {
	var bnorm fixed.Point26_6
	a := r.a // Copy local a since r.a is going to change during stroke operation
	ba := b.Sub(a)
	segLen := int64(Length(ba))
	if b == r.leadPoint.P { // End of segment
		bnorm = r.leadPoint.TNorm // Use more accurate leadPoint tangent
	} else {
		bnorm = turnPort90(ToLength(ba, r.u)) // Intra segment normal
	}

	st := r.activeState()
	if r.inDutyFallback || st.count >= st.budget {
		r.enterDutyFallback()
		r.emitFallbackQuad(a, b, bnorm)
		r.a = b
		r.ln = bnorm
		return
	}

	// Reserve a slice of the draw budget for visible dashes so an extreme
	// left-offscreen segment skips remaining offscreen dashes arithmetically
	// rather than consuming the entire visible budget.
	leftLimit := st.budget - min(st.budget/4, 4096)

	var nlt int64
	rem := segLen
	ci := r.segmentClipInfo(a, b, segLen)

	// Always skip dashes strictly right/above/below before entering the window.
	if ci.hasRTB && ci.rtbStart > 0 {
		if !r.dashIsGap {
			nl := int64(r.Dashes[r.dashPlace]) - int64(r.deltaDash)
			if nl < ci.rtbStart {
				nlt += nl
				r.dashLineStrokeBit(a.Add(ToLength(ba, fixed.Int26_6(nlt))), bnorm, false)
				r.dashIsGap = true
				rem -= nl
				r.deltaDash = 0
				r.dashPlace++
				if r.dashPlace == len(r.Dashes) {
					r.dashPlace = 0
				}
			}
		}
		if r.dashIsGap {
			var skipped bool
			nlt, rem, skipped = r.skipDashesBefore(nlt, rem, ci.rtbStart)
			if skipped {
				pt := a.Add(ToLength(ba, fixed.Int26_6(nlt)))
				if !r.dashIsGap {
					r.CapT(r.out(), pt, Invert(bnorm))
				}
				r.a = pt
				r.ln = bnorm
			}
		}
	}

	for int64(r.Dashes[r.dashPlace])-int64(r.deltaDash) < rem {
		// Always skip dashes strictly right/above/below after leaving the window.
		if ci.hasRTB && nlt >= ci.rtbEnd && r.dashIsGap {
			var skipped bool
			nlt, rem, skipped = r.skipDashesBefore(nlt, rem, segLen)
			if skipped {
				pt := a.Add(ToLength(ba, fixed.Int26_6(nlt)))
				if !r.dashIsGap {
					r.CapT(r.out(), pt, Invert(bnorm))
				}
				r.a = pt
				r.ln = bnorm
			}
			break
		}

		// Left-offscreen dashes emit normally and count against st.count until
		// st.count reaches leftLimit; beyond that, skip remaining left-offscreen
		// dashes arithmetically so visible dashes still render accurately.
		if ci.hasLeft && st.count >= leftLimit && r.dashIsGap {
			if !ci.fullVis || nlt >= ci.fullEnd {
				var skipped bool
				nlt, rem, skipped = r.skipDashesBefore(nlt, rem, segLen)
				if skipped {
					pt := a.Add(ToLength(ba, fixed.Int26_6(nlt)))
					if !r.dashIsGap {
						r.CapT(r.out(), pt, Invert(bnorm))
					}
					r.a = pt
					r.ln = bnorm
				}
				break
			}
			if nlt < ci.fullStart {
				var skipped bool
				nlt, rem, skipped = r.skipDashesBefore(nlt, rem, ci.fullStart)
				if skipped {
					pt := a.Add(ToLength(ba, fixed.Int26_6(nlt)))
					if !r.dashIsGap {
						r.CapT(r.out(), pt, Invert(bnorm))
					}
					r.a = pt
					r.ln = bnorm
					continue
				}
			}
		}

		if st.count >= st.budget {
			r.enterDutyFallback()
			r.emitFallbackQuad(r.a, b, bnorm)
			r.a = b
			r.ln = bnorm
			return
		}
		nl := int64(r.Dashes[r.dashPlace]) - int64(r.deltaDash)
		nlt += nl
		r.dashLineStrokeBit(a.Add(ToLength(ba, fixed.Int26_6(nlt))), bnorm, false)
		r.dashIsGap = !r.dashIsGap
		rem -= nl
		r.deltaDash = 0
		r.dashPlace++
		if r.dashPlace == len(r.Dashes) {
			r.dashPlace = 0
		}
		st.count++
	}
	r.deltaDash += fixed.Int26_6(rem)
	r.dashLineStrokeBit(b, bnorm, true)
}

// SetStroke set the parameters for stroking a line. width is the width of the line, miterlimit is the miter cutoff
// value for miter, arc, miterclip and arcClip joinModes. CapL and CapT are the capping functions for leading and trailing
// line ends. If one is nil, the other function is used at both ends. gp is the gap function that determines how a
// gap on the convex side of two lines joining is filled. jm is the JoinMode for curve segments. Dashes is the values for
// the dash pattern. Pass in nil or an empty slice for no dashes. dashoffset is the starting offset into the dash array.
func (r *Dasher) SetStroke(width, miterLimit fixed.Int26_6, capL, capT CapFunc, gp GapFunc, jm JoinMode, dashes []float64, dashOffset float64) {
	r.Stroker.SetStroke(width, miterLimit, capL, capT, gp, jm)

	if r.drawState == nil {
		r.ownDrawState.count = 0
	}
	r.dashCycle = 0
	r.dashTotalF = 0
	r.dashOnF = 0
	r.dashNumOn = 0
	r.subpixelDash = false
	r.inDutyFallback = false
	r.fallbackPts = r.fallbackPts[:0]
	r.Dashes = r.Dashes[:0] // clear the dash array
	if len(dashes) == 0 {
		r.sgm = &r.Stroker // This is just plain stroking
		return
	}
	// Record exact float64 on/total lengths and dash count over one full cycle
	// before fixed-point quantization so sub-quarter-pixel patterns and
	// post-budget fallbacks compute accurate cap-adjusted coverage.
	var totalF, onF float64
	for i, v := range dashes {
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
			continue
		}
		totalF += v
		if i%2 == 0 {
			onF += v
		}
	}
	if totalF <= 0 {
		r.sgm = &r.Stroker // All-zero dasharray renders as a solid stroke.
		return
	}
	if len(dashes)%2 != 0 {
		// Odd-length dasharray repeats twice: every entry is on once and off once.
		r.dashTotalF = 2 * totalF
		r.dashOnF = totalF
		r.dashNumOn = len(dashes)
	} else {
		r.dashTotalF = totalF
		r.dashOnF = onF
		r.dashNumOn = len(dashes) / 2
	}

	// Convert the float dash array and offset to fixed point and attach to the Filler
	oneIsPos := false
	var period int64
	for _, v := range dashes {
		var fv fixed.Int26_6
		if !math.IsNaN(v) && !math.IsInf(v, 0) && v > 0 {
			scaled := v * 64
			if scaled >= math.MaxInt32 {
				fv = math.MaxInt32
			} else {
				fv = fixed.Int26_6(scaled)
			}
		}
		if fv <= 0 { // Negatives are considered 0s.
			fv = 0
		} else {
			oneIsPos = true
			period += int64(fv)
		}
		r.Dashes = append(r.Dashes, fv)
	}
	cycle := period
	if len(r.Dashes)%2 != 0 {
		cycle *= 2
	}
	st := r.activeState()
	if !oneIsPos || cycle < minDashCycle || (r.drawState != nil && st.count >= st.budget) {
		r.Dashes = r.Dashes[:0]
		r.subpixelDash = true
		r.sgm = &r.Stroker
		return
	}
	r.dashCycle = cycle
	r.SetPhase(dashOffset)
	r.sgm = r // Use the full dasher
}

// SetPhase sets the starting dash offset for subsequent subpaths (callable
// between subpaths before Start/Draw), keeping the active dash pattern.
func (r *Dasher) SetPhase(off float64) {
	if len(r.Dashes) == 0 {
		r.DashOffset = 0
		return
	}
	var period float64
	for _, d := range r.Dashes {
		period += float64(d) / 64
	}
	if period > 0 && !math.IsNaN(off) && !math.IsInf(off, 0) {
		cycle := period
		if len(r.Dashes)%2 != 0 {
			cycle *= 2
		}
		off = math.Mod(off, cycle)
		if off < 0 {
			off += cycle
		}
	} else {
		off = 0
	}
	r.DashOffset = fixed.Int26_6(off * 64)
}

// Stop terminates a dashed line
func (r *Dasher) Stop(isClosed bool) {
	if len(r.Dashes) == 0 {
		r.Stroker.Stop(isClosed)
		return
	}
	if !r.inStroke {
		return
	}
	if isClosed && r.a != r.firstP.P {
		r.LineSeg(r.sgm, r.firstP.P)
	}
	if r.inDutyFallback {
		if !r.firstDashIsGap {
			r.CapT(r.out(), r.firstP.P, Invert(r.firstP.LNorm))
		}
		r.inDutyFallback = false
		r.inStroke = false
		return
	}
	ra := r.out()
	if isClosed && !r.firstDashIsGap && !r.dashIsGap { // closed connect w/o caps
		a := r.a
		r.firstP.TNorm = r.leadPoint.TNorm
		r.firstP.RT = r.leadPoint.RT
		r.firstP.TTan = r.leadPoint.TTan
		ra.Start(r.firstP.P.Sub(r.firstP.TNorm))
		ra.Line(a.Sub(r.ln))
		ra.Start(a.Add(r.ln))
		ra.Line(r.firstP.P.Add(r.firstP.TNorm))
		r.Joiner(r.firstP)
		r.firstP.blackWidowMark(ra)
	} else { // Cap open ends
		if !r.dashIsGap {
			r.CapL(ra, r.leadPoint.P, r.leadPoint.TNorm)
		}
		if !r.firstDashIsGap {
			r.CapT(ra, r.firstP.P, Invert(r.firstP.LNorm))
		}
	}
	r.inStroke = false
}

// dashLineStrokeBit is a helper function that reduces code redundancey in the
// lineF function.
func (r *Dasher) dashLineStrokeBit(b, bnorm fixed.Point26_6, dontClose bool) {
	if !r.dashIsGap { // Moving from dash to gap
		a := r.a
		ra := r.out()
		ra.Start(b.Sub(bnorm))
		ra.Line(a.Sub(r.ln))
		ra.Start(a.Add(r.ln))
		ra.Line(b.Add(bnorm))
		if !dontClose {
			r.CapL(ra, b, bnorm)
		}
	} else { // Moving from gap to dash
		if !dontClose {
			ra := r.out()
			r.CapT(ra, b, Invert(bnorm))
		}
	}
	r.a = b
	r.ln = bnorm
}

// Line for Dasher is here to pass the dasher sgm to LineP
func (r *Dasher) Line(b fixed.Point26_6) {
	r.LineSeg(r.sgm, b)
}

// QuadBezier for dashing
func (r *Dasher) QuadBezier(b, c fixed.Point26_6) {
	r.quadBezierf(r.sgm, b, c)
}

// CubeBezier starts a stroked cubic bezier.
// It is a low level function exposed for the purposes of callbacks
// and debugging.
func (r *Dasher) CubeBezier(b, c, d fixed.Point26_6) {
	r.cubeBezierf(r.sgm, b, c, d)
}

// NewDasher returns a Dasher ptr with default values.
// A Dasher has all of the capabilities of a Stroker, Filler, and Scanner, plus the ability
// to stroke curves with solid lines. Use SetStroke to configure with non-default
// values.
func NewDasher(width, height int, scanner Scanner) *Dasher {
	r := new(Dasher)
	r.Scanner = scanner
	r.SetBounds(width, height)
	r.SetWinding(true)
	r.SetStroke(1*64, 4*64, ButtCap, nil, FlatGap, MiterClip, nil, 0)
	r.sgm = &r.Stroker
	return r
}
