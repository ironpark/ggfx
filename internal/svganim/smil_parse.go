// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"encoding/xml"
	"errors"
	"math"
	"strconv"
	"strings"
)

type animOrigin uint8

const (
	originSMIL animOrigin = iota
	originCSS
)

type calcMode uint8

const (
	calcLinear calcMode = iota
	calcDiscrete
	calcSpline
)

type animForm uint8

const (
	formValues animForm = iota
	formFromTo
	formFromBy
	formBy
	formTo
	formSet
)

type keyframe struct {
	off      float64
	v        Value
	fromBase bool // CSS missing 0% or 100% synthesized from base at eval time
	ease     easing
}

type animTrack struct {
	elem       ElemID
	prop       PropID
	origin     animOrigin
	docOrder   int
	timing     *trackTiming
	kf         []keyframe
	calc       calcMode
	additive   bool
	accumulate bool
	form       animForm
}

type rawSMILElem struct {
	tag      string
	attrs    []xml.Attr
	parent   ElemID
	inDefs   bool
	docOrder int
}

func compileSMIL(raws []rawSMILElem, sv SceneView) ([]*animTrack, bool, []Diag) {
	var tracks []*animTrack
	var timings []*trackTiming
	var diags []Diag
	hasAny := len(raws) > 0

	for _, r := range raws {
		switch r.tag {
		case "animateMotion", "mpath", "discard":
			diags = append(diags, Diag{
				Elem:    r.parent,
				Message: "unsupported SMIL element <" + r.tag + ">",
			})
			continue
		case "animate", "animateColor", "set", "animateTransform":
		default:
			diags = append(diags, Diag{
				Elem:    r.parent,
				Message: "unsupported SMIL element <" + r.tag + ">",
			})
			continue
		}

		tr, ds := parseSMILTrack(r, sv)
		diags = append(diags, ds...)
		if tr != nil {
			tracks = append(tracks, tr)
			timings = append(timings, tr.timing)
		}
	}

	resolveSMILTimings(timings)
	return tracks, hasAny, diags
}

func getAttr(attrs []xml.Attr, name string) (string, bool) {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value, true
		}
	}
	return "", false
}

func parseSMILTrack(r rawSMILElem, sv SceneView) (*animTrack, []Diag) {
	var diags []Diag

	// Target resolution: href / xlink:href else parent.
	target := r.parent
	if href, ok := getAttr(r.attrs, "href"); ok && strings.TrimSpace(href) != "" {
		id := strings.TrimSpace(href)
		id = strings.TrimPrefix(id, "#")
		if sv != nil {
			ids := sv.ByID(id)
			if len(ids) > 0 {
				target = ids[0]
			} else {
				diags = append(diags, Diag{Elem: r.parent, Message: "SMIL target not found: #" + id})
				return nil, diags
			}
		} else {
			diags = append(diags, Diag{Elem: r.parent, Message: "SMIL target not found: #" + id})
			return nil, diags
		}
	}
	if target < 0 {
		diags = append(diags, Diag{Elem: r.parent, Message: "SMIL element has no target"})
		return nil, diags
	}

	// Property resolution.
	var prop PropID
	tfType := "translate"
	if r.tag == "animateTransform" {
		attrName, ok := getAttr(r.attrs, "attributeName")
		if ok && strings.TrimSpace(attrName) != "" {
			prop = propByNameImpl(attrName)
			if prop != PropTransform {
				diags = append(diags, Diag{Elem: target, Message: "unsupported animateTransform attributeName: " + attrName})
				return nil, diags
			}
		} else {
			prop = PropTransform
		}
		if tAttr, ok := getAttr(r.attrs, "type"); ok && strings.TrimSpace(tAttr) != "" {
			tfType = strings.TrimSpace(tAttr)
		}
	} else {
		attrName, ok := getAttr(r.attrs, "attributeName")
		if !ok || strings.TrimSpace(attrName) == "" {
			diags = append(diags, Diag{Elem: target, Message: "SMIL element missing attributeName"})
			return nil, diags
		}
		prop = propByNameImpl(attrName)
		if prop == PropNone {
			diags = append(diags, Diag{Elem: target, Message: "unsupported SMIL attributeName: " + attrName})
			return nil, diags
		}
	}

	// Parse timing attributes.
	tm := &trackTiming{
		simpleDur:   math.NaN(),
		repeatCount: math.NaN(),
		repeatDur:   math.NaN(),
		minDur:      math.NaN(),
		maxDur:      math.NaN(),
		restart:     restartAlways,
	}
	if id, ok := getAttr(r.attrs, "id"); ok {
		tm.id = strings.TrimSpace(id)
	}

	if beginStr, ok := getAttr(r.attrs, "begin"); ok && strings.TrimSpace(beginStr) != "" {
		specs, ds := parseBeginEndList(beginStr, false, target)
		diags = append(diags, ds...)
		tm.begins = specs
	} else {
		tm.begins = []beginSpec{{kind: beginOffset, offset: 0}}
	}

	if endStr, ok := getAttr(r.attrs, "end"); ok && strings.TrimSpace(endStr) != "" {
		specs, ds := parseBeginEndList(endStr, true, target)
		diags = append(diags, ds...)
		tm.ends = specs
	}

	if durStr, ok := getAttr(r.attrs, "dur"); ok && strings.TrimSpace(durStr) != "" {
		low := strings.ToLower(strings.TrimSpace(durStr))
		if low == "indefinite" || low == "media" {
			tm.simpleDur = math.Inf(1)
		} else {
			d, err := parseSMILClock(durStr)
			if err != nil || d < 0 {
				diags = append(diags, Diag{Elem: target, Message: "invalid SMIL dur: " + durStr})
				return nil, diags
			}
			tm.simpleDur = d
		}
	}

	if rcStr, ok := getAttr(r.attrs, "repeatCount"); ok && strings.TrimSpace(rcStr) != "" {
		low := strings.ToLower(strings.TrimSpace(rcStr))
		if low == "indefinite" {
			tm.repeatCount = math.Inf(1)
		} else {
			rc, err := strconv.ParseFloat(low, 64)
			if err != nil || math.IsNaN(rc) || math.IsInf(rc, 0) || rc <= 0 {
				diags = append(diags, Diag{Elem: target, Message: "invalid SMIL repeatCount: " + rcStr})
				return nil, diags
			}
			tm.repeatCount = rc
		}
	}

	if rdStr, ok := getAttr(r.attrs, "repeatDur"); ok && strings.TrimSpace(rdStr) != "" {
		low := strings.ToLower(strings.TrimSpace(rdStr))
		if low == "indefinite" {
			tm.repeatDur = math.Inf(1)
		} else {
			rd, err := parseSMILClock(rdStr)
			if err != nil || rd <= 0 {
				diags = append(diags, Diag{Elem: target, Message: "invalid SMIL repeatDur: " + rdStr})
				return nil, diags
			}
			tm.repeatDur = rd
		}
	}

	if minStr, ok := getAttr(r.attrs, "min"); ok && strings.TrimSpace(minStr) != "" {
		low := strings.ToLower(strings.TrimSpace(minStr))
		if low != "media" {
			if d, err := parseSMILClock(minStr); err == nil && d >= 0 {
				tm.minDur = d
			}
		}
	}

	if maxStr, ok := getAttr(r.attrs, "max"); ok && strings.TrimSpace(maxStr) != "" {
		low := strings.ToLower(strings.TrimSpace(maxStr))
		if low == "indefinite" || low == "media" {
			tm.maxDur = math.Inf(1)
		} else if d, err := parseSMILClock(maxStr); err == nil && d >= 0 {
			tm.maxDur = d
		}
	}

	if restartStr, ok := getAttr(r.attrs, "restart"); ok {
		switch strings.ToLower(strings.TrimSpace(restartStr)) {
		case "whennotactive":
			tm.restart = restartWhenNotActive
		case "never":
			tm.restart = restartNever
		default:
			tm.restart = restartAlways
		}
	}

	if fillStr, ok := getAttr(r.attrs, "fill"); ok {
		switch strings.ToLower(strings.TrimSpace(fillStr)) {
		case "freeze":
			tm.fillForwards = true
		case "remove":
			tm.fillForwards = false
		}
	}

	parseVal := func(raw string) (Value, error) {
		if r.tag == "animateTransform" {
			return parseAnimateTransformVal(tfType, raw)
		}
		return parseValueImpl(prop, raw, SyntaxSVGAttr)
	}

	// <set> is simple: only uses `to`.
	if r.tag == "set" {
		toStr, ok := getAttr(r.attrs, "to")
		if !ok || strings.TrimSpace(toStr) == "" {
			diags = append(diags, Diag{Elem: target, Message: "<set> missing to attribute"})
			return nil, diags
		}
		toVal, err := parseVal(toStr)
		if err != nil {
			diags = append(diags, Diag{Elem: target, Message: "invalid <set> to value: " + err.Error()})
			return nil, diags
		}
		return &animTrack{
			elem:     target,
			prop:     prop,
			origin:   originSMIL,
			docOrder: r.docOrder,
			timing:   tm,
			kf:       []keyframe{{off: 0, v: toVal, ease: easingLinear}},
			calc:     calcDiscrete,
			form:     formSet,
		}, diags
	}

	// calcMode
	cm := calcLinear
	if cmStr, ok := getAttr(r.attrs, "calcMode"); ok && strings.TrimSpace(cmStr) != "" {
		switch strings.ToLower(strings.TrimSpace(cmStr)) {
		case "linear":
			cm = calcLinear
		case "discrete":
			cm = calcDiscrete
		case "spline":
			cm = calcSpline
		case "paced":
			cm = calcLinear
			diags = append(diags, Diag{Elem: target, Message: "SMIL calcMode=paced treated as linear"})
		default:
			diags = append(diags, Diag{Elem: target, Message: "unknown SMIL calcMode: " + cmStr})
		}
	}
	if isDiscreteProp(prop) {
		cm = calcDiscrete
	}

	// additive / accumulate
	additive := false
	if addStr, ok := getAttr(r.attrs, "additive"); ok {
		if strings.EqualFold(strings.TrimSpace(addStr), "sum") {
			additive = true
		}
	}
	accumulate := false
	if accStr, ok := getAttr(r.attrs, "accumulate"); ok {
		if strings.EqualFold(strings.TrimSpace(accStr), "sum") {
			accumulate = true
		}
	}

	// Parse values / from / to / by.
	var (
		form animForm
		vals []Value
	)
	valuesStr, hasValues := getAttr(r.attrs, "values")
	fromStr, hasFrom := getAttr(r.attrs, "from")
	toStr, hasTo := getAttr(r.attrs, "to")
	byStr, hasBy := getAttr(r.attrs, "by")

	if hasValues && strings.TrimSpace(valuesStr) != "" {
		form = formValues
		parts := splitSemicolonTrimmed(valuesStr)
		if len(parts) == 0 {
			diags = append(diags, Diag{Elem: target, Message: "empty SMIL values attribute"})
			return nil, diags
		}
		vals = make([]Value, 0, len(parts))
		for _, p := range parts {
			v, err := parseVal(p)
			if err != nil {
				diags = append(diags, Diag{Elem: target, Message: "invalid SMIL values item: " + err.Error()})
				return nil, diags
			}
			vals = append(vals, v)
		}
	} else if hasFrom && hasTo {
		form = formFromTo
		v0, err := parseVal(fromStr)
		if err != nil {
			diags = append(diags, Diag{Elem: target, Message: "invalid SMIL from value: " + err.Error()})
			return nil, diags
		}
		v1, err := parseVal(toStr)
		if err != nil {
			diags = append(diags, Diag{Elem: target, Message: "invalid SMIL to value: " + err.Error()})
			return nil, diags
		}
		vals = []Value{v0, v1}
	} else if hasFrom && hasBy {
		form = formFromBy
		v0, err := parseVal(fromStr)
		if err != nil {
			diags = append(diags, Diag{Elem: target, Message: "invalid SMIL from value: " + err.Error()})
			return nil, diags
		}
		vBy, err := parseVal(byStr)
		if err != nil {
			diags = append(diags, Diag{Elem: target, Message: "invalid SMIL by value: " + err.Error()})
			return nil, diags
		}
		vals = []Value{v0, vBy}
	} else if hasBy {
		form = formBy
		vBy, err := parseVal(byStr)
		if err != nil {
			diags = append(diags, Diag{Elem: target, Message: "invalid SMIL by value: " + err.Error()})
			return nil, diags
		}
		additive = true
		vZero := zeroValueFor(vBy)
		vEnd := vBy
		if vBy.Kind == ValTransformList {
			vEnd = addByTransformValue(vZero, vBy)
		}
		vals = []Value{vZero, vEnd}
	} else if hasTo {
		form = formTo
		v1, err := parseVal(toStr)
		if err != nil {
			diags = append(diags, Diag{Elem: target, Message: "invalid SMIL to value: " + err.Error()})
			return nil, diags
		}
		additive = false
		accumulate = false
		vals = []Value{v1}
	} else {
		diags = append(diags, Diag{Elem: target, Message: "SMIL animation missing values/from/to/by"})
		return nil, diags
	}

	if cm == calcDiscrete {
		accumulate = false
	}

	// Build keyframes and keyTimes.
	nVals := len(vals)
	kfs := make([]keyframe, nVals)
	for i, v := range vals {
		kfs[i] = keyframe{v: v, ease: easingLinear}
	}

	if ktStr, ok := getAttr(r.attrs, "keyTimes"); ok && strings.TrimSpace(ktStr) != "" && form == formValues {
		kts, err := parseKeyTimes(ktStr, nVals, cm == calcDiscrete)
		if err != nil {
			diags = append(diags, Diag{Elem: target, Message: "invalid SMIL keyTimes: " + err.Error()})
			return nil, diags
		}
		for i, kt := range kts {
			kfs[i].off = kt
		}
	} else {
		assignDefaultKeyTimes(kfs, cm == calcDiscrete)
	}

	// keySplines
	if cm == calcSpline {
		if form == formTo {
			// to-animation has 1 segment (base -> to), expects 1 keySpline.
			ksStr, ok := getAttr(r.attrs, "keySplines")
			if !ok {
				diags = append(diags, Diag{Elem: target, Message: "SMIL calcMode=spline missing keySplines"})
				return nil, diags
			}
			splines, err := parseKeySplines(ksStr)
			if err != nil || len(splines) != 1 {
				diags = append(diags, Diag{Elem: target, Message: "invalid SMIL keySplines for to-animation"})
				return nil, diags
			}
			kfs[0].ease = splines[0]
		} else if nVals >= 2 {
			ksStr, ok := getAttr(r.attrs, "keySplines")
			if !ok {
				diags = append(diags, Diag{Elem: target, Message: "SMIL calcMode=spline missing keySplines"})
				return nil, diags
			}
			splines, err := parseKeySplines(ksStr)
			if err != nil || len(splines) != nVals-1 {
				diags = append(diags, Diag{Elem: target, Message: "SMIL keySplines count mismatch"})
				return nil, diags
			}
			for i, sp := range splines {
				kfs[i].ease = sp
			}
		}
	}

	return &animTrack{
		elem:       target,
		prop:       prop,
		origin:     originSMIL,
		docOrder:   r.docOrder,
		timing:     tm,
		kf:         kfs,
		calc:       cm,
		additive:   additive,
		accumulate: accumulate,
		form:       form,
	}, diags
}

func assignDefaultKeyTimes(kfs []keyframe, discrete bool) {
	n := len(kfs)
	if n == 0 {
		return
	}
	if n == 1 {
		kfs[0].off = 0
		return
	}
	if discrete {
		// Spec §3: discrete without keyTimes splits into n equal intervals (not n-1).
		for i := range n {
			kfs[i].off = float64(i) / float64(n)
		}
		return
	}
	for i := range n {
		kfs[i].off = float64(i) / float64(n-1)
	}
}

func parseKeyTimes(raw string, expected int, discrete bool) ([]float64, error) {
	parts := splitSemicolonTrimmed(raw)
	if len(parts) != expected {
		return nil, errors.New("keyTimes length must match values length")
	}
	out := make([]float64, len(parts))
	prev := -1.0
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return nil, errors.New("keyTime out of [0, 1]")
		}
		if v < prev {
			return nil, errors.New("keyTimes must be monotonically non-decreasing")
		}
		out[i] = v
		prev = v
	}
	if out[0] != 0 {
		return nil, errors.New("first keyTime must be 0")
	}
	if !discrete && expected > 1 && out[expected-1] != 1 {
		return nil, errors.New("last keyTime must be 1 for non-discrete calcMode")
	}
	return out, nil
}

func parseAnimateTransformVal(tfType, raw string) (Value, error) {
	parts := splitSpaceOrComma(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return Value{}, errors.New("empty animateTransform value")
	}
	nums := make([]float64, len(parts))
	for i, p := range parts {
		v, _, err := parseLength(p)
		if err != nil {
			return Value{}, err
		}
		nums[i] = v
	}

	switch strings.ToLower(tfType) {
	case "translate":
		if len(nums) != 1 && len(nums) != 2 {
			return Value{}, errors.New("animateTransform translate requires 1 or 2 values")
		}
		ty := 0.0
		if len(nums) == 2 {
			ty = nums[1]
		}
		return Value{
			Kind: ValTransformList,
			Xf:   []TransformFn{{Op: TfTranslate, Args: [6]float64{nums[0], ty}, N: 2}},
			Raw:  raw,
		}, nil

	case "scale":
		if len(nums) != 1 && len(nums) != 2 {
			return Value{}, errors.New("animateTransform scale requires 1 or 2 values")
		}
		sy := nums[0]
		if len(nums) == 2 {
			sy = nums[1]
		}
		return Value{
			Kind: ValTransformList,
			Xf:   []TransformFn{{Op: TfScale, Args: [6]float64{nums[0], sy}, N: 2}},
			Raw:  raw,
		}, nil

	case "rotate":
		if len(nums) == 1 {
			return Value{
				Kind: ValTransformList,
				Xf:   []TransformFn{{Op: TfRotate, Args: [6]float64{nums[0]}, Units: [6]UnitKind{UnitDeg}, N: 1}},
				Raw:  raw,
			}, nil
		}
		if len(nums) == 3 {
			return Value{
				Kind: ValTransformList,
				Xf:   []TransformFn{{Op: TfRotate, Args: [6]float64{nums[0], nums[1], nums[2]}, Units: [6]UnitKind{UnitDeg}, N: 3}},
				Raw:  raw,
			}, nil
		}
		return Value{}, errors.New("animateTransform rotate requires 1 or 3 values")

	case "skewx":
		if len(nums) != 1 {
			return Value{}, errors.New("animateTransform skewX requires 1 value")
		}
		return Value{
			Kind: ValTransformList,
			Xf:   []TransformFn{{Op: TfSkewX, Args: [6]float64{nums[0]}, Units: [6]UnitKind{UnitDeg}, N: 1}},
			Raw:  raw,
		}, nil

	case "skewy":
		if len(nums) != 1 {
			return Value{}, errors.New("animateTransform skewY requires 1 value")
		}
		return Value{
			Kind: ValTransformList,
			Xf:   []TransformFn{{Op: TfSkewY, Args: [6]float64{nums[0]}, Units: [6]UnitKind{UnitDeg}, N: 1}},
			Raw:  raw,
		}, nil

	default:
		return Value{}, errors.New("unsupported animateTransform type: " + tfType)
	}
}
