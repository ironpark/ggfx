// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"container/heap"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

type direction uint8

const (
	dirNormal direction = iota
	dirReverse
	dirAlternate
	dirAlternateReverse
)

type restartMode uint8

const (
	restartAlways restartMode = iota
	restartWhenNotActive
	restartNever
)

type beginKind uint8

const (
	beginOffset beginKind = iota
	beginSyncBegin
	beginSyncEnd
)

type beginSpec struct {
	kind   beginKind
	offset float64
	syncID string
}

type timeInterval struct {
	begin float64
	end   float64 // +Inf if indefinite
}

type trackTiming struct {
	id            string
	begins        []beginSpec
	ends          []beginSpec
	simpleDur     float64 // NaN = unset, +Inf = indefinite
	repeatCount   float64 // NaN = unset, +Inf = indefinite
	repeatDur     float64 // NaN = unset, +Inf = indefinite
	minDur        float64 // NaN = unset
	maxDur        float64 // NaN = unset
	direction     direction
	fillBackwards bool
	fillForwards  bool
	restart       restartMode
	paused        bool
	isCSS         bool

	// Resolved active intervals sorted ascending by begin.
	intervals []timeInterval
	// Periodic syncbase loop summary (when syncbase cycle detected).
	periodic      bool
	periodStart   float64
	periodDur     float64
	periodPattern []timeInterval // relative to periodStart
}

type sampleState struct {
	active      bool
	progress    float64 // directed progress in [0, 1]
	rawProgress float64 // undirected progress in [0, 1]
	iteration   int
	frozen      bool
	before      bool
	activeBegin float64
	activeEnd   float64
}

// parseSMILClock parses a SMIL clock value into seconds.
// Supports: 1.5s, 200ms, 2min, 1h, 01:30, 00:01:30.5, bare seconds, and optional +/- sign.
func parseSMILClock(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, errors.New("empty clock value")
	}
	sign := 1.0
	if s[0] == '+' {
		s = strings.TrimSpace(s[1:])
	} else if s[0] == '-' {
		sign = -1.0
		s = strings.TrimSpace(s[1:])
	}
	if s == "" {
		return 0, errors.New("invalid clock value: " + raw)
	}

	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		if len(parts) != 2 && len(parts) != 3 {
			return 0, errors.New("invalid clock format: " + raw)
		}
		var h, m, sec float64
		var err error
		if len(parts) == 3 {
			h, err = strconv.ParseFloat(parts[0], 64)
			if err != nil || h < 0 || math.Floor(h) != h {
				return 0, errors.New("invalid clock hours: " + raw)
			}
			m, err = strconv.ParseFloat(parts[1], 64)
			if err != nil || m < 0 || m >= 60 || math.Floor(m) != m {
				return 0, errors.New("invalid clock minutes: " + raw)
			}
			sec, err = strconv.ParseFloat(parts[2], 64)
			if err != nil || sec < 0 || sec >= 60 {
				return 0, errors.New("invalid clock seconds: " + raw)
			}
		} else {
			m, err = strconv.ParseFloat(parts[0], 64)
			if err != nil || m < 0 || math.Floor(m) != m {
				return 0, errors.New("invalid clock minutes: " + raw)
			}
			sec, err = strconv.ParseFloat(parts[1], 64)
			if err != nil || sec < 0 || sec >= 60 {
				return 0, errors.New("invalid clock seconds: " + raw)
			}
		}
		total := h*3600 + m*60 + sec
		if math.IsNaN(total) || math.IsInf(total, 0) {
			return 0, errors.New("invalid clock value: " + raw)
		}
		return sign * total, nil
	}

	low := strings.ToLower(s)
	mult := 1.0
	numStr := s
	switch {
	case strings.HasSuffix(low, "ms"):
		mult = 0.001
		numStr = s[:len(s)-2]
	case strings.HasSuffix(low, "min"):
		mult = 60.0
		numStr = s[:len(s)-3]
	case strings.HasSuffix(low, "h"):
		mult = 3600.0
		numStr = s[:len(s)-1]
	case strings.HasSuffix(low, "s"):
		mult = 1.0
		numStr = s[:len(s)-1]
	}
	v, err := strconv.ParseFloat(numStr, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, errors.New("invalid clock value: " + raw)
	}
	return sign * v * mult, nil
}

// parseCSSTime parses a CSS <time> value (s, ms, or unitless 0) into seconds.
func parseCSSTime(raw string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, errors.New("empty CSS time")
	}
	low := strings.ToLower(s)
	switch {
	case strings.HasSuffix(low, "ms"):
		v, err := strconv.ParseFloat(s[:len(s)-2], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, errors.New("invalid CSS time: " + raw)
		}
		return v * 0.001, nil
	case strings.HasSuffix(low, "s"):
		v, err := strconv.ParseFloat(s[:len(s)-1], 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, errors.New("invalid CSS time: " + raw)
		}
		return v, nil
	default:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v != 0 {
			return 0, errors.New("CSS time requires unit: " + raw)
		}
		return 0, nil
	}
}

// parseBeginEndList parses a SMIL begin or end semicolon-separated list.
// Returns parsed specs plus any diagnostics for unsupported entries (indefinite, events, repeat, etc.).
func parseBeginEndList(raw string, isEnd bool, elem ElemID) ([]beginSpec, []Diag) {
	parts := splitSemicolonTrimmed(raw)
	var specs []beginSpec
	var diags []Diag
	for _, p := range parts {
		if p == "" {
			continue
		}
		low := strings.ToLower(p)
		if low == "indefinite" {
			if !isEnd {
				diags = append(diags, Diag{Elem: elem, Message: "unsupported SMIL begin=indefinite"})
			}
			continue
		}
		if strings.HasPrefix(low, "wallclock(") || strings.HasPrefix(low, "accesskey(") || strings.Contains(low, "repeat(") {
			diags = append(diags, Diag{Elem: elem, Message: "unsupported SMIL timing value: " + p})
			continue
		}
		// Try plain offset clock value first.
		if sec, err := parseSMILClock(p); err == nil {
			specs = append(specs, beginSpec{kind: beginOffset, offset: sec})
			continue
		}
		// Try syncbase: id.begin[±offset] or id.end[±offset]
		if spec, ok := parseSyncbase(p); ok {
			specs = append(specs, spec)
			continue
		}
		// Event-based or unknown begin/end.
		diags = append(diags, Diag{Elem: elem, Message: "unsupported SMIL timing value: " + p})
	}
	return specs, diags
}

func parseSyncbase(s string) (beginSpec, bool) {
	// Look for ".begin" or ".end" followed by end-of-string, whitespace, '+', or '-'.
	dot := strings.IndexByte(s, '.')
	for dot >= 0 {
		id := strings.TrimSpace(s[:dot])
		rest := s[dot+1:]
		var kind beginKind
		var after string
		matched := false
		if len(rest) >= 5 && strings.EqualFold(rest[:5], "begin") {
			after = rest[5:]
			if after == "" || after[0] == '+' || after[0] == '-' || after[0] == ' ' || after[0] == '\t' {
				kind = beginSyncBegin
				matched = true
			}
		} else if len(rest) >= 3 && strings.EqualFold(rest[:3], "end") {
			after = rest[3:]
			if after == "" || after[0] == '+' || after[0] == '-' || after[0] == ' ' || after[0] == '\t' {
				kind = beginSyncEnd
				matched = true
			}
		}
		if matched && id != "" {
			after = strings.TrimSpace(after)
			var off float64
			if after != "" {
				if after[0] != '+' && after[0] != '-' {
					return beginSpec{}, false
				}
				var err error
				off, err = parseSMILClock(after)
				if err != nil {
					return beginSpec{}, false
				}
			}
			return beginSpec{kind: kind, offset: off, syncID: id}, true
		}
		next := strings.IndexByte(s[dot+1:], '.')
		if next < 0 {
			break
		}
		dot = dot + 1 + next
	}
	return beginSpec{}, false
}

// baseActiveDur computes a SMIL track's active duration before end/min/max constraints.
func (tm *trackTiming) baseActiveDur() float64 {
	if tm.isCSS {
		if tm.repeatCount == 0 {
			return 0
		}
		if math.IsInf(tm.repeatCount, 1) {
			if tm.simpleDur == 0 {
				return 0
			}
			return math.Inf(1)
		}
		return tm.simpleDur * tm.repeatCount
	}

	sd := tm.simpleDur
	hasRC := !math.IsNaN(tm.repeatCount)
	hasRD := !math.IsNaN(tm.repeatDur)

	if math.IsNaN(sd) || math.IsInf(sd, 1) {
		if hasRD && !math.IsInf(tm.repeatDur, 1) {
			return math.Max(0, tm.repeatDur)
		}
		return math.Inf(1)
	}
	if sd <= 0 {
		return 0
	}
	if !hasRC && !hasRD {
		return sd
	}
	d := math.Inf(1)
	if hasRC {
		if math.IsInf(tm.repeatCount, 1) {
			d = math.Inf(1)
		} else {
			d = math.Max(0, tm.repeatCount) * sd
		}
	}
	if hasRD {
		if !math.IsInf(tm.repeatDur, 1) {
			rd := math.Max(0, tm.repeatDur)
			if rd < d {
				d = rd
			}
		}
	}
	return d
}

// clampMinMax applies SMIL min/max active duration constraints.
func (tm *trackTiming) clampMinMax(ad float64) float64 {
	hasMin := !math.IsNaN(tm.minDur) && tm.minDur >= 0
	hasMax := !math.IsNaN(tm.maxDur) && tm.maxDur >= 0 && !math.IsInf(tm.maxDur, 1)
	if hasMin && hasMax && tm.maxDur < tm.minDur {
		// Per SMIL 3.0, if max < min, both are ignored.
		return ad
	}
	if hasMin && ad < tm.minDur {
		ad = tm.minDur
	}
	if hasMax && ad > tm.maxDur {
		ad = tm.maxDur
	}
	return ad
}

// resolveCSSTiming builds the single active interval for a CSS animation timing.
func (tm *trackTiming) resolveCSSTiming() {
	delay := 0.0
	if len(tm.begins) > 0 {
		delay = tm.begins[0].offset
	}
	ad := tm.baseActiveDur()
	tm.intervals = []timeInterval{{begin: delay, end: delay + ad}}
}

type syncEvent struct {
	t        float64
	trackIdx int
	seq      int
}

type syncEventHeap []syncEvent

func (h syncEventHeap) Len() int { return len(h) }
func (h syncEventHeap) Less(i, j int) bool {
	if h[i].t != h[j].t {
		return h[i].t < h[j].t
	}
	return h[i].seq < h[j].seq
}
func (h syncEventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *syncEventHeap) Push(x any)   { *h = append(*h, x.(syncEvent)) }
func (h *syncEventHeap) Pop() any {
	old := *h
	n := len(old)
	it := old[n-1]
	*h = old[:n-1]
	return it
}

const maxSyncIntervals = 10000

// resolveSMILTimings resolves intervals for all SMIL tracks, including syncbase
// dependencies and cyclic chains (e.g. begin="0;b.end") with 10k bound & period detection.
const (
	maxSyncPops  = maxSyncIntervals * 16
	maxSyncQueue = maxSyncIntervals * 16
)

func resolveSMILTimings(timings []*trackTiming) {
	_, _ = resolveSMILTimingsWithLimit(timings, maxSyncQueue)
}

func resolveSMILTimingsWithLimit(timings []*trackTiming, queueLimit int) (peakQueue, refused int) {
	if len(timings) == 0 {
		return 0, 0
	}

	type depTarget struct {
		trackIdx int
		offset   float64
		isEnd    bool
	}
	// Map SMIL id -> track indices with that id.
	idToTracks := make(map[string][]int)
	for i, tm := range timings {
		if tm.id != "" {
			idToTracks[tm.id] = append(idToTracks[tm.id], i)
		}
	}

	// Build dependency adjacency lists: sourceTrack -> dependent begin/end listeners.
	onBegin := make([][]depTarget, len(timings))
	onEnd := make([][]depTarget, len(timings))
	beginAdj := make([][]int, len(timings))

	// Static end timestamps per track.
	endTimes := make([][]float64, len(timings))
	maxStaticTime := math.Inf(-1)
	hasAnySync := false

	const checkBatch = 64

	var pq syncEventHeap
	seq := 0
	pushEvent := func(t float64, trackIdx int) {
		if pq.Len() >= queueLimit {
			refused++
			return
		}
		heap.Push(&pq, syncEvent{t: t, trackIdx: trackIdx, seq: seq})
		seq++
		if pq.Len() > peakQueue {
			peakQueue = pq.Len()
		}
	}
	for i, tm := range timings {
		for _, b := range tm.begins {
			switch b.kind {
			case beginOffset:
				pushEvent(b.offset, i)
				if b.offset > maxStaticTime {
					maxStaticTime = b.offset
				}
			case beginSyncBegin:
				hasAnySync = true
				for _, src := range idToTracks[b.syncID] {
					onBegin[src] = append(onBegin[src], depTarget{trackIdx: i, offset: b.offset})
					beginAdj[src] = append(beginAdj[src], i)
				}
			case beginSyncEnd:
				hasAnySync = true
				for _, src := range idToTracks[b.syncID] {
					onEnd[src] = append(onEnd[src], depTarget{trackIdx: i, offset: b.offset})
					beginAdj[src] = append(beginAdj[src], i)
				}
			}
		}
		for _, e := range tm.ends {
			switch e.kind {
			case beginOffset:
				endTimes[i] = append(endTimes[i], e.offset)
				if e.offset > maxStaticTime {
					maxStaticTime = e.offset
				}
			case beginSyncBegin:
				hasAnySync = true
				for _, src := range idToTracks[e.syncID] {
					onBegin[src] = append(onBegin[src], depTarget{trackIdx: i, offset: e.offset, isEnd: true})
				}
			case beginSyncEnd:
				hasAnySync = true
				for _, src := range idToTracks[e.syncID] {
					onEnd[src] = append(onEnd[src], depTarget{trackIdx: i, offset: e.offset, isEnd: true})
				}
			}
		}
		sort.Float64s(endTimes[i])
	}

	insertEndTime := func(idx int, t float64) {
		es := endTimes[idx]
		pos := sort.SearchFloat64s(es, t)
		if pos < len(es) && math.Abs(es[pos]-t) < 1e-12 {
			return
		}
		es = append(es, 0)
		copy(es[pos+1:], es[pos:])
		es[pos] = t
		endTimes[idx] = es
		// If the track currently has an active interval covering t, shorten its end!
		tm := timings[idx]
		if len(tm.intervals) > 0 {
			last := &tm.intervals[len(tm.intervals)-1]
			if t >= last.begin && t < last.end {
				ad := tm.clampMinMax(t - last.begin)
				newEnd := last.begin + ad
				if newEnd < last.end {
					last.end = newEnd
					if !math.IsInf(newEnd, 1) {
						for _, d := range onEnd[idx] {
							if d.isEnd {
								// Defer or avoid infinite recursion; handled via queue if needed.
								continue
							}
							pushEvent(newEnd+d.offset, d.trackIdx)
						}
					}
				}
			}
		}
	}

	totalGenerated := 0
	totalPops := 0

	for pq.Len() > 0 && totalGenerated < maxSyncIntervals && totalPops < maxSyncPops {
		ev := heap.Pop(&pq).(syncEvent)
		totalPops++
		idx := ev.trackIdx
		tm := timings[idx]

		// Apply restart rules against the last interval.
		if len(tm.intervals) > 0 {
			last := &tm.intervals[len(tm.intervals)-1]
			// Guard against duplicate begin at the exact same timestamp (zero-dur loops).
			if math.Abs(ev.t-last.begin) < 1e-12 {
				continue
			}
			// Ignore out-of-order earlier begins if already surpassed.
			if ev.t < last.begin {
				continue
			}
			switch tm.restart {
			case restartNever:
				continue
			case restartWhenNotActive:
				if ev.t < last.end-1e-12 {
					continue
				}
			case restartAlways:
				if ev.t < last.end {
					last.end = ev.t
				}
			}
		}

		// Compute active end.
		ad := tm.baseActiveDur()
		end := ev.t + ad
		// Apply earliest end instance >= ev.t.
		if es := endTimes[idx]; len(es) > 0 {
			pos := sort.SearchFloat64s(es, ev.t-1e-12)
			if pos < len(es) && es[pos] < end {
				end = es[pos]
			}
		}
		ad = tm.clampMinMax(end - ev.t)
		end = ev.t + ad

		tm.intervals = append(tm.intervals, timeInterval{begin: ev.t, end: end})
		totalGenerated++

		// Emit syncbase events for begin and end.
		for _, d := range onBegin[idx] {
			if d.isEnd {
				insertEndTime(d.trackIdx, ev.t+d.offset)
			} else {
				pushEvent(ev.t+d.offset, d.trackIdx)
			}
		}
		if !math.IsInf(end, 1) {
			for _, d := range onEnd[idx] {
				if d.isEnd {
					insertEndTime(d.trackIdx, end+d.offset)
				} else {
					pushEvent(end+d.offset, d.trackIdx)
				}
			}
		}

		// Periodically check if all pending cyclic tracks have settled into a repeating period.
		if hasAnySync && totalGenerated >= checkBatch && totalGenerated%checkBatch == 0 && pq.Len() > 0 {
			if pq[0].t > maxStaticTime+1e-9 && detectSyncPeriods(timings, pq, beginAdj) {
				break
			}
		}
	}

	// If we hit the cap without breaking early, try period detection one final time.
	if hasAnySync && pq.Len() > 0 {
		detectSyncPeriods(timings, pq, beginAdj)
	}
	return peakQueue, refused
}

// detectSyncPeriods checks whether every track reachable from pending events in pq is periodic.
// If so, populates periodic/periodStart/periodDur/periodPattern on those tracks and returns true.
func detectSyncPeriods(timings []*trackTiming, pq syncEventHeap, beginAdj [][]int) bool {
	inQueue := make(map[int]bool)
	var queue []int
	for _, ev := range pq {
		if !inQueue[ev.trackIdx] {
			inQueue[ev.trackIdx] = true
			queue = append(queue, ev.trackIdx)
		}
	}
	if len(inQueue) == 0 {
		return false
	}
	// BFS over syncbase begin dependencies to include all tracks in the active cyclic component.
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, v := range beginAdj[u] {
			if !inQueue[v] {
				inQueue[v] = true
				queue = append(queue, v)
			}
		}
	}

	type foundPeriod struct {
		start   float64
		dur     float64
		pattern []timeInterval
	}
	found := make(map[int]foundPeriod, len(inQueue))

	for idx := range inQueue {
		tm := timings[idx]
		iv := tm.intervals
		n := len(iv)
		if n < 8 {
			return false
		}
		matched := false
		maxK := min(32, n/4)
		for k := 1; k <= maxK; k++ {
			P := iv[n-1].begin - iv[n-1-k].begin
			if P <= 1e-9 || math.IsInf(P, 0) || math.IsNaN(P) {
				continue
			}
			// Verify the last 3*k intervals repeat with period P.
			ok := true
			checkStart := n - 3*k
			for i := checkStart; i < n; i++ {
				db := (iv[i].begin - iv[i-k].begin) - P
				if math.Abs(db) > 1e-6 {
					ok = false
					break
				}
				if math.IsInf(iv[i].end, 1) != math.IsInf(iv[i-k].end, 1) {
					ok = false
					break
				}
				if !math.IsInf(iv[i].end, 1) {
					de := (iv[i].end - iv[i-k].end) - P
					if math.Abs(de) > 1e-6 {
						ok = false
						break
					}
				}
			}
			if ok {
				// Walk backward to find the earliest index where this period started.
				tailStart := checkStart - k
				for tailStart > 0 {
					prev := tailStart - 1
					if math.Abs((iv[prev+k].begin-iv[prev].begin)-P) > 1e-6 {
						break
					}
					if math.IsInf(iv[prev+k].end, 1) != math.IsInf(iv[prev].end, 1) {
						break
					}
					if !math.IsInf(iv[prev].end, 1) && math.Abs((iv[prev+k].end-iv[prev].end)-P) > 1e-6 {
						break
					}
					tailStart--
				}
				pStart := iv[tailStart].begin
				pat := make([]timeInterval, k)
				for j := range k {
					pat[j] = timeInterval{
						begin: iv[tailStart+j].begin - pStart,
						end:   iv[tailStart+j].end - pStart,
					}
				}
				found[idx] = foundPeriod{start: pStart, dur: P, pattern: pat}
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	for idx, fp := range found {
		tm := timings[idx]
		tm.periodic = true
		tm.periodStart = fp.start
		tm.periodDur = fp.dur
		tm.periodPattern = fp.pattern
		// Trim redundant generated intervals beyond periodStart to save memory.
		cut := sort.Search(len(tm.intervals), func(i int) bool {
			return tm.intervals[i].begin >= fp.start-1e-9
		})
		tm.intervals = tm.intervals[:cut]
	}
	return true
}

// latestIntervalAt returns the latest interval with begin <= t, and whether one exists.
func (tm *trackTiming) latestIntervalAt(t float64) (timeInterval, bool) {
	if tm.periodic && tm.periodDur > 0 && t >= tm.periodStart-1e-12 && len(tm.periodPattern) > 0 {
		rel := t - tm.periodStart
		cycle := math.Floor(rel / tm.periodDur)
		if !math.IsNaN(cycle) && !math.IsInf(cycle, 0) {
			// Check current cycle and previous cycle (in case an interval spans across cycle boundary).
			for _, delta := range [2]float64{0, -1} {
				c := cycle + delta
				if c < 0 || (delta != 0 && c == cycle) {
					continue
				}
				base := tm.periodStart + c*tm.periodDur
				for j := len(tm.periodPattern) - 1; j >= 0; j-- {
					iv := timeInterval{
						begin: base + tm.periodPattern[j].begin,
						end:   base + tm.periodPattern[j].end,
					}
					if iv.begin <= t+1e-12 {
						return iv, true
					}
				}
			}
		}
	}
	n := len(tm.intervals)
	if n == 0 {
		return timeInterval{}, false
	}
	// Binary search for first interval with begin > t + eps.
	idx := sort.Search(n, func(i int) bool {
		return tm.intervals[i].begin > t+1e-12
	})
	if idx == 0 {
		return timeInterval{}, false
	}
	return tm.intervals[idx-1], true
}

// nextIntervalAfter returns the earliest interval with begin > t, and whether one exists.
func (tm *trackTiming) nextIntervalAfter(t float64) (timeInterval, bool) {
	// Check static intervals first.
	idx := sort.Search(len(tm.intervals), func(i int) bool {
		return tm.intervals[i].begin > t+1e-12
	})
	if idx < len(tm.intervals) {
		return tm.intervals[idx], true
	}
	if tm.periodic && tm.periodDur > 0 && len(tm.periodPattern) > 0 {
		if t < tm.periodStart-1e-12 {
			return timeInterval{
				begin: tm.periodStart + tm.periodPattern[0].begin,
				end:   tm.periodStart + tm.periodPattern[0].end,
			}, true
		}
		rel := t - tm.periodStart
		cycle := math.Floor(rel / tm.periodDur)
		if !math.IsNaN(cycle) && !math.IsInf(cycle, 0) {
			for _, delta := range [2]float64{0, 1} {
				c := cycle + delta
				if delta != 0 && c == cycle {
					continue
				}
				base := tm.periodStart + c*tm.periodDur
				for _, p := range tm.periodPattern {
					b := base + p.begin
					if b > t+1e-12 {
						return timeInterval{begin: b, end: base + p.end}, true
					}
				}
			}
		}
	}
	return timeInterval{}, false
}

func applyDirection(rawP float64, iter int, d direction) float64 {
	switch d {
	case dirReverse:
		return 1 - rawP
	case dirAlternate:
		if iter%2 == 1 {
			return 1 - rawP
		}
		return rawP
	case dirAlternateReverse:
		if iter%2 == 0 {
			return 1 - rawP
		}
		return rawP
	default:
		return rawP
	}
}

func isDirectionReversed(iter int, d direction) bool {
	switch d {
	case dirReverse:
		return true
	case dirAlternate:
		return iter%2 == 1
	case dirAlternateReverse:
		return iter%2 == 0
	default:
		return false
	}
}

// sample evaluates the timing at document time t (seconds).
func (tm *trackTiming) sample(t float64) sampleState {
	if tm.paused {
		t = 0
	}

	if tm.isCSS {
		return tm.sampleCSS(t)
	}
	return tm.sampleSMIL(t)
}

func (tm *trackTiming) sampleCSS(t float64) sampleState {
	if len(tm.intervals) == 0 {
		return sampleState{}
	}
	iv := tm.intervals[0]
	if t < iv.begin {
		if !tm.fillBackwards {
			return sampleState{}
		}
		return sampleState{
			active:      true,
			progress:    applyDirection(0, 0, tm.direction),
			rawProgress: 0,
			iteration:   0,
			frozen:      true,
			before:      true,
			activeBegin: iv.begin,
			activeEnd:   iv.end,
		}
	}

	if t >= iv.end {
		if !tm.fillForwards {
			return sampleState{}
		}
		iter, rawP := tm.endIterAndProgress(iv.end - iv.begin)
		return sampleState{
			active:      true,
			progress:    applyDirection(rawP, iter, tm.direction),
			rawProgress: rawP,
			iteration:   iter,
			frozen:      true,
			activeBegin: iv.begin,
			activeEnd:   iv.end,
		}
	}

	// Active inside [iv.begin, iv.end)
	elapsed := t - iv.begin
	if tm.simpleDur <= 0 {
		iter, rawP := tm.endIterAndProgress(0)
		return sampleState{
			active:      true,
			progress:    applyDirection(rawP, iter, tm.direction),
			rawProgress: rawP,
			iteration:   iter,
			activeBegin: iv.begin,
			activeEnd:   iv.end,
		}
	}
	iter := int(math.Floor(elapsed / tm.simpleDur))
	rawP := clamp01((elapsed - float64(iter)*tm.simpleDur) / tm.simpleDur)
	return sampleState{
		active:      true,
		progress:    applyDirection(rawP, iter, tm.direction),
		rawProgress: rawP,
		iteration:   iter,
		activeBegin: iv.begin,
		activeEnd:   iv.end,
	}
}

func (tm *trackTiming) endIterAndProgress(activeDur float64) (int, float64) {
	if tm.isCSS {
		if tm.repeatCount == 0 {
			return 0, 0
		}
		if tm.simpleDur <= 0 {
			if math.IsInf(tm.repeatCount, 1) {
				return 0, 1
			}
			rc := tm.repeatCount
			rounded := math.Round(rc)
			if math.Abs(rc-rounded) < 1e-9 && rounded >= 1 {
				return int(rounded) - 1, 1
			}
			return int(math.Floor(rc)), rc - math.Floor(rc)
		}
	}

	if math.IsNaN(tm.simpleDur) || math.IsInf(tm.simpleDur, 1) {
		return 0, 0
	}
	if tm.simpleDur <= 0 {
		return 0, 1
	}
	if activeDur <= 0 {
		return 0, 0
	}
	totalIter := activeDur / tm.simpleDur
	rounded := math.Round(totalIter)
	if math.Abs(totalIter-rounded) < 1e-9 && rounded >= 1 {
		return int(rounded) - 1, 1.0
	}
	return int(math.Floor(totalIter)), clamp01(totalIter - math.Floor(totalIter))
}

func (tm *trackTiming) sampleSMIL(t float64) sampleState {
	iv, ok := tm.latestIntervalAt(t)
	if !ok {
		return sampleState{}
	}
	if t < iv.end {
		elapsed := t - iv.begin
		if elapsed < 0 {
			elapsed = 0
		}
		if math.IsNaN(tm.simpleDur) || math.IsInf(tm.simpleDur, 1) || tm.simpleDur <= 0 {
			return sampleState{
				active:      true,
				progress:    0,
				rawProgress: 0,
				iteration:   0,
				activeBegin: iv.begin,
				activeEnd:   iv.end,
			}
		}
		iter := int(math.Floor(elapsed / tm.simpleDur))
		rawP := clamp01((elapsed - float64(iter)*tm.simpleDur) / tm.simpleDur)
		return sampleState{
			active:      true,
			progress:    rawP,
			rawProgress: rawP,
			iteration:   iter,
			activeBegin: iv.begin,
			activeEnd:   iv.end,
		}
	}

	// t >= iv.end
	if !tm.fillForwards {
		return sampleState{}
	}
	iter, rawP := tm.endIterAndProgress(iv.end - iv.begin)
	return sampleState{
		active:      true,
		progress:    rawP,
		rawProgress: rawP,
		iteration:   iter,
		frozen:      true,
		activeBegin: iv.begin,
		activeEnd:   iv.end,
	}
}

// isInfiniteRepeating reports whether this track repeats indefinitely over time,
// and returns its cycle period (seconds) and the time at which periodicity starts.
func (tm *trackTiming) infiniteCycle() (infinite bool, period float64, loopStart float64) {
	if tm.paused {
		return false, 0, 0
	}
	if tm.periodic && tm.periodDur > 0 {
		return true, tm.periodDur, math.Max(0, tm.periodStart)
	}
	if len(tm.intervals) == 0 {
		return false, 0, 0
	}
	last := tm.intervals[len(tm.intervals)-1]
	if !math.IsInf(last.end, 1) {
		return false, 0, 0
	}
	// Active duration is +Inf. If simpleDur is finite and > 0, it repeats periodically.
	if !math.IsNaN(tm.simpleDur) && !math.IsInf(tm.simpleDur, 1) && tm.simpleDur > 0 {
		p := tm.simpleDur
		if tm.direction == dirAlternate || tm.direction == dirAlternateReverse {
			p *= 2
		}
		return true, p, math.Max(0, last.begin)
	}
	// Indefinite simpleDur (e.g. <set to="red"/> with no end): holds forever after begin, does not repeat.
	return false, 0, 0
}

// finiteEnd returns the final transition time for a non-repeating track.
func (tm *trackTiming) finiteEnd() (float64, bool) {
	if tm.paused || len(tm.intervals) == 0 {
		return 0, false
	}
	last := tm.intervals[len(tm.intervals)-1]
	if math.IsInf(last.end, 1) {
		// Indefinite simpleDur holds forever starting at last.begin.
		return math.Max(0, last.begin), true
	}
	return math.Max(0, last.end), true
}

const maxLoopMicros int64 = 60 * 1_000_000

func gcdInt64(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// computeTimeline summarizes all compiled tracks into a Timeline.
func computeTimeline(tracks []*animTrack) Timeline {
	if len(tracks) == 0 {
		return Timeline{}
	}

	var (
		maxFiniteEnd float64
		hasFinite    bool
		hasInfinite  bool
		maxLoopStart float64
		maxSpan      float64
		loopMicros   int64 = 1
		loopOverflow bool
		loopCount    int
	)

	seenTiming := make(map[*trackTiming]bool, len(tracks))
	for _, tr := range tracks {
		tm := tr.timing
		if seenTiming[tm] {
			continue
		}
		seenTiming[tm] = true

		if inf, period, lStart := tm.infiniteCycle(); inf {
			hasInfinite = true
			if lStart > maxLoopStart {
				maxLoopStart = lStart
			}
			// One complete cycle finishes at lStart + period (or max(period, begin+period)).
			cycleEnd := lStart + period
			if cycleEnd > maxSpan {
				maxSpan = cycleEnd
			}
			us := int64(math.Round(period * 1e6))
			if us <= 0 || us > maxLoopMicros {
				loopOverflow = true
			} else if !loopOverflow {
				g := gcdInt64(loopMicros, us)
				q := us / g
				if loopMicros > maxLoopMicros/q {
					loopOverflow = true
				} else {
					loopMicros *= q
					loopCount++
				}
			}
		} else if end, ok := tm.finiteEnd(); ok {
			hasFinite = true
			if end > maxFiniteEnd {
				maxFiniteEnd = end
			}
			if end > maxLoopStart {
				maxLoopStart = end
			}
			if end > maxSpan {
				maxSpan = end
			}
		}
	}

	tl := Timeline{
		Infinite: hasInfinite,
		Span:     secToDuration(maxSpan),
	}
	if hasFinite {
		tl.End = secToDuration(maxFiniteEnd)
	}
	if hasInfinite && !loopOverflow && loopCount > 0 && loopMicros <= maxLoopMicros {
		tl.Loop = time.Duration(loopMicros) * time.Microsecond
		tl.LoopStart = secToDuration(maxLoopStart)
	}
	return tl
}

func secToDuration(sec float64) time.Duration {
	if sec <= 0 || math.IsNaN(sec) || math.IsInf(sec, 0) {
		return 0
	}
	return time.Duration(math.Round(sec * float64(time.Second)))
}
