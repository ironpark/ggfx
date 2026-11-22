// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

type easingKind uint8

const (
	easeLinear easingKind = iota
	easeCubicBezier
	easeSteps
)

type stepPos uint8

const (
	stepJumpEnd stepPos = iota
	stepJumpStart
	stepJumpNone
	stepJumpBoth
)

type easing struct {
	kind    easingKind
	x1, y1  float64
	x2, y2  float64
	steps   int
	stepPos stepPos
}

var (
	easingLinear    = easing{kind: easeLinear}
	easingEase      = easing{kind: easeCubicBezier, x1: 0.25, y1: 0.1, x2: 0.25, y2: 1.0}
	easingEaseIn    = easing{kind: easeCubicBezier, x1: 0.42, y1: 0.0, x2: 1.0, y2: 1.0}
	easingEaseOut   = easing{kind: easeCubicBezier, x1: 0.0, y1: 0.0, x2: 0.58, y2: 1.0}
	easingEaseInOut = easing{kind: easeCubicBezier, x1: 0.42, y1: 0.0, x2: 0.58, y2: 1.0}
	easingStepStart = easing{kind: easeSteps, steps: 1, stepPos: stepJumpStart}
	easingStepEnd   = easing{kind: easeSteps, steps: 1, stepPos: stepJumpEnd}
)

// parseCSSEasing parses a CSS animation-timing-function value.
func parseCSSEasing(raw string) (easing, error) {
	s := strings.TrimSpace(raw)
	low := strings.ToLower(s)
	switch low {
	case "linear":
		return easingLinear, nil
	case "ease":
		return easingEase, nil
	case "ease-in":
		return easingEaseIn, nil
	case "ease-out":
		return easingEaseOut, nil
	case "ease-in-out":
		return easingEaseInOut, nil
	case "step-start":
		return easingStepStart, nil
	case "step-end":
		return easingStepEnd, nil
	}

	open := strings.IndexByte(low, '(')
	if open < 0 || !strings.HasSuffix(low, ")") {
		return easing{}, errors.New("invalid easing: " + raw)
	}
	fn := strings.TrimSpace(low[:open])
	inner := strings.TrimSpace(s[open+1 : len(s)-1])

	switch fn {
	case "cubic-bezier":
		parts := strings.Split(inner, ",")
		if len(parts) != 4 {
			return easing{}, errors.New("cubic-bezier requires 4 arguments")
		}
		var nums [4]float64
		for i, p := range parts {
			v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return easing{}, errors.New("invalid cubic-bezier argument")
			}
			nums[i] = v
		}
		return easing{
			kind: easeCubicBezier,
			x1:   clamp01(nums[0]),
			y1:   nums[1],
			x2:   clamp01(nums[2]),
			y2:   nums[3],
		}, nil

	case "steps":
		parts := strings.Split(inner, ",")
		if len(parts) < 1 || len(parts) > 2 {
			return easing{}, errors.New("steps requires 1 or 2 arguments")
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || n < 1 {
			return easing{}, errors.New("steps count must be integer >= 1")
		}
		pos := stepJumpEnd
		if len(parts) == 2 {
			switch strings.ToLower(strings.TrimSpace(parts[1])) {
			case "jump-start", "start":
				pos = stepJumpStart
			case "jump-end", "end":
				pos = stepJumpEnd
			case "jump-none":
				if n < 2 {
					return easing{}, errors.New("steps(n, jump-none) requires n >= 2")
				}
				pos = stepJumpNone
			case "jump-both":
				pos = stepJumpBoth
			default:
				return easing{}, errors.New("invalid steps position: " + parts[1])
			}
		}
		return easing{kind: easeSteps, steps: n, stepPos: pos}, nil

	case "linear":
		return easing{}, errors.New("unsupported CSS linear() easing")

	default:
		return easing{}, errors.New("unknown easing function: " + fn)
	}
}

// parseKeySplines parses a SMIL keySplines attribute (semicolon-separated 4-tuples in [0, 1]).
func parseKeySplines(raw string) ([]easing, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("empty keySplines")
	}
	groups := splitSemicolonTrimmed(s)
	if len(groups) == 0 {
		return nil, errors.New("empty keySplines")
	}
	out := make([]easing, 0, len(groups))
	for _, g := range groups {
		fields := splitSpaceOrComma(g)
		if len(fields) != 4 {
			return nil, errors.New("keySplines group must have 4 numbers")
		}
		var v [4]float64
		for i, f := range fields {
			n, err := strconv.ParseFloat(f, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1 {
				return nil, errors.New("keySplines values must be in [0, 1]")
			}
			v[i] = n
		}
		out = append(out, easing{
			kind: easeCubicBezier,
			x1:   v[0],
			y1:   v[1],
			x2:   v[2],
			y2:   v[3],
		})
	}
	return out, nil
}

// eval evaluates the easing curve at progress t in [0, 1].
func (e easing) eval(t float64) float64 {
	switch e.kind {
	case easeLinear:
		return t
	case easeSteps:
		return e.evalSteps(t)
	case easeCubicBezier:
		return e.evalCubicBezier(t)
	default:
		return t
	}
}

func (e easing) evalSteps(t float64) float64 {
	n := e.steps
	if n <= 0 {
		return t
	}
	if t < 0 {
		return 0
	}
	jumps := n
	switch e.stepPos {
	case stepJumpNone:
		jumps = n - 1
		if jumps < 1 {
			jumps = 1
		}
	case stepJumpBoth:
		jumps = n + 1
	}

	cur := math.Floor(t * float64(n))
	if e.stepPos == stepJumpStart || e.stepPos == stepJumpBoth {
		cur += 1
	}
	if cur < 0 {
		cur = 0
	}
	if cur > float64(jumps) {
		cur = float64(jumps)
	}
	return cur / float64(jumps)
}

func (e easing) evalCubicBezier(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	if e.x1 == e.y1 && e.x2 == e.y2 {
		return t
	}

	cx := 3 * e.x1
	bx := 3*(e.x2-e.x1) - cx
	ax := 1 - cx - bx

	cy := 3 * e.y1
	by := 3*(e.y2-e.y1) - cy
	ay := 1 - cy - by

	sampleX := func(u float64) float64 { return ((ax*u+bx)*u + cx) * u }
	sampleDX := func(u float64) float64 { return (3*ax*u+2*bx)*u + cx }
	sampleY := func(u float64) float64 { return ((ay*u+by)*u + cy) * u }

	// Newton-Raphson first
	u := t
	converged := false
	for range 8 {
		err := sampleX(u) - t
		if math.Abs(err) < 1e-7 {
			converged = true
			break
		}
		dx := sampleDX(u)
		if math.Abs(dx) < 1e-6 {
			break
		}
		next := u - err/dx
		if next < 0 || next > 1 {
			break
		}
		u = next
	}

	// Bisection fallback for guaranteed convergence
	if !converged {
		lo, hi := 0.0, 1.0
		u = t
		for range 60 {
			x := sampleX(u)
			if math.Abs(x-t) < 1e-7 || hi-lo < 1e-12 {
				break
			}
			if x < t {
				lo = u
			} else {
				hi = u
			}
			u = 0.5 * (lo + hi)
		}
	}

	return sampleY(u)
}

// nextStepBoundary returns the next progress boundary strictly > t in (0, 1] for stepped easings.
func (e easing) nextStepBoundary(t float64) (float64, bool) {
	if e.kind != easeSteps || e.steps <= 0 {
		return 0, false
	}
	if t >= 1 {
		return 0, false
	}
	if t < 0 {
		return 0, true
	}
	n := float64(e.steps)
	next := (math.Floor(t*n+1e-12) + 1) / n
	if next > 1 {
		next = 1
	}
	return next, true
}

// prevStepBoundary returns the next step boundary strictly < t in [0, 1) when moving in reverse.
func (e easing) prevStepBoundary(t float64) (float64, bool) {
	if e.kind != easeSteps || e.steps <= 0 {
		return 0, false
	}
	if t <= 0 {
		return 0, false
	}
	if t > 1 {
		return 1, true
	}
	n := float64(e.steps)
	prev := (math.Ceil(t*n-1e-12) - 1) / n
	if prev < 0 {
		prev = 0
	}
	return prev, true
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func splitSemicolonTrimmed(s string) []string {
	raw := strings.Split(s, ";")
	out := make([]string, 0, len(raw))
	for i, p := range raw {
		p = strings.TrimSpace(p)
		if p == "" && i == len(raw)-1 {
			continue
		}
		out = append(out, p)
	}
	return out
}

func splitSpaceOrComma(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}
