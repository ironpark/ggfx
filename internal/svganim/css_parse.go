// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

type specificity struct {
	id    int
	class int
	typ   int
}

func (a specificity) less(b specificity) bool {
	if a.id != b.id {
		return a.id < b.id
	}
	if a.class != b.class {
		return a.class < b.class
	}
	return a.typ < b.typ
}

type combinator uint8

const (
	combNone       combinator = iota
	combDescendant            // ' '
	combChild                 // '>'
)

type compoundSelector struct {
	universal bool
	tag       string
	id        string
	classes   []string
}

type selectorPart struct {
	comb combinator // combinator between previous part and this part
	comp compoundSelector
}

type cssSelector struct {
	parts []selectorPart
	spec  specificity
}

type cssRule struct {
	selectors []cssSelector
	decls     []Decl
	order     int
}

type rawKeyframeStop struct {
	offset float64 // [0, 1]
	decls  []Decl  // normalized declarations (!important dropped)
}

type cssKeyframesRule struct {
	name  string
	stops []rawKeyframeStop
}

type mediaKind uint8

const (
	mediaReducedMotion mediaKind = iota
	mediaColorScheme
)

type mediaBlock struct {
	kind      mediaKind
	value     string // e.g. "reduce", "no-preference", "dark", "light"
	rules     []cssRule
	keyframes map[string]*cssKeyframesRule
}

type stylesheetImpl struct {
	rules       []cssRule
	keyframes   map[string]*cssKeyframesRule
	mediaBlocks []mediaBlock
	nextOrder   int
}

func newStylesheetImpl() *stylesheetImpl {
	return &stylesheetImpl{
		keyframes: make(map[string]*cssKeyframesRule),
	}
}

// stripCSSComments removes /* ... */ comments while respecting quoted strings.
func stripCSSComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	n := len(src)
	for i < n {
		c := src[i]
		if c == '"' || c == '\'' {
			q := c
			b.WriteByte(c)
			i++
			for i < n {
				ch := src[i]
				b.WriteByte(ch)
				i++
				if ch == '\\' && i < n {
					b.WriteByte(src[i])
					i++
					continue
				}
				if ch == q {
					break
				}
			}
			continue
		}
		if c == '/' && i+1 < n && src[i+1] == '*' {
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2
			} else {
				i = n
			}
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// findMatchingBrace starts at src[openIdx] == '{' and returns the index of the matching '}'.
// Respects quoted strings and nested braces.
func findMatchingBrace(src string, openIdx int) int {
	n := len(src)
	depth := 0
	for i := openIdx; i < n; i++ {
		c := src[i]
		if c == '"' || c == '\'' {
			q := c
			i++
			for i < n {
				if src[i] == '\\' && i+1 < n {
					i += 2
					continue
				}
				if src[i] == q {
					break
				}
				i++
			}
			continue
		}
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return n
}

// parseCSSInto parses a CSS text block into rules, keyframes, and stored @media blocks.
func (ss *stylesheetImpl) parseCSSInto(raw string, rulesDst *[]cssRule, kfDst map[string]*cssKeyframesRule, diags *[]Diag) {
	src := stripCSSComments(raw)
	n := len(src)
	i := 0

	for i < n {
		// Skip whitespace and stray semicolons/closing braces.
		for i < n && (isCSSSpace(src[i]) || src[i] == ';' || src[i] == '}') {
			i++
		}
		if i >= n {
			break
		}

		// At-rule
		if src[i] == '@' {
			atStart := i
			i++
			nameStart := i
			for i < n && !isCSSSpace(src[i]) && src[i] != '{' && src[i] != ';' && src[i] != '(' {
				i++
			}
			atName := strings.ToLower(src[nameStart:i])

			// Scan prelude until '{' or ';' (respecting strings and parens).
			preludeStart := i
			parenDepth := 0
			for i < n {
				c := src[i]
				if c == '"' || c == '\'' {
					i = skipCSSString(src, i)
					continue
				}
				if c == '(' {
					parenDepth++
				} else if c == ')' && parenDepth > 0 {
					parenDepth--
				} else if parenDepth == 0 && (c == '{' || c == ';') {
					break
				}
				i++
			}
			prelude := strings.TrimSpace(src[preludeStart:i])

			if i >= n || src[i] == ';' {
				if i < n {
					i++ // skip ';'
				}
				_ = atStart
				continue
			}

			// Block at-rule: src[i] == '{'
			closeIdx := findMatchingBrace(src, i)
			body := src[i+1 : closeIdx]
			if closeIdx < n {
				i = closeIdx + 1
			} else {
				i = n
			}

			switch atName {
			case "keyframes", "-webkit-keyframes":
				kf := parseKeyframesBlock(prelude, body)
				if kf != nil && kf.name != "" {
					kfDst[kf.name] = kf
				}
			case "media":
				if kind, val, ok := parseStoredMediaQuery(prelude); ok {
					mb := mediaBlock{
						kind:      kind,
						value:     val,
						keyframes: make(map[string]*cssKeyframesRule),
					}
					ss.parseCSSInto(body, &mb.rules, mb.keyframes, diags)
					ss.mediaBlocks = append(ss.mediaBlocks, mb)
				}
				// Other @media blocks are skipped leniently.
			default:
				// @supports, @font-face, etc. skipped leniently.
			}
			continue
		}

		// Qualified style rule: <selector-list> { <declarations> }
		selStart := i
		for i < n && src[i] != '{' && src[i] != '}' {
			if src[i] == '"' || src[i] == '\'' {
				i = skipCSSString(src, i)
				continue
			}
			i++
		}
		if i >= n || src[i] != '{' {
			if i < n {
				i++
			}
			continue
		}
		selText := strings.TrimSpace(src[selStart:i])
		closeIdx := findMatchingBrace(src, i)
		body := src[i+1 : closeIdx]
		if closeIdx < n {
			i = closeIdx + 1
		} else {
			i = n
		}

		sels, selDiags := parseSelectorList(selText)
		*diags = append(*diags, selDiags...)
		if len(sels) == 0 {
			continue
		}
		decls := parseDeclarations(body)
		if len(decls) == 0 {
			continue
		}
		*rulesDst = append(*rulesDst, cssRule{
			selectors: sels,
			decls:     decls,
			order:     ss.nextOrder,
		})
		ss.nextOrder++
	}
}

func skipCSSString(src string, start int) int {
	n := len(src)
	q := src[start]
	i := start + 1
	for i < n {
		if src[i] == '\\' && i+1 < n {
			i += 2
			continue
		}
		if src[i] == q {
			return i + 1
		}
		i++
	}
	return n
}

func isCSSSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

// parseStoredMediaQuery checks for (prefers-reduced-motion...) or (prefers-color-scheme...).
func parseStoredMediaQuery(prelude string) (mediaKind, string, bool) {
	s := strings.TrimSpace(strings.ToLower(prelude))
	// Strip optional leading "screen and " / "all and ".
	if after, ok := strings.CutPrefix(s, "screen"); ok {
		s = strings.TrimSpace(after)
		s = strings.TrimSpace(strings.TrimPrefix(s, "and"))
	} else if after, ok := strings.CutPrefix(s, "all"); ok {
		s = strings.TrimSpace(after)
		s = strings.TrimSpace(strings.TrimPrefix(s, "and"))
	}
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return 0, "", false
	}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	// Reject nested compound conditions with additional parens for now.
	if strings.ContainsAny(inner, "()") {
		return 0, "", false
	}
	feat, val, hasColon := strings.Cut(inner, ":")
	feat = strings.TrimSpace(feat)
	val = strings.TrimSpace(val)
	switch feat {
	case "prefers-reduced-motion":
		if !hasColon {
			val = "reduce"
		}
		if val == "reduce" || val == "no-preference" {
			return mediaReducedMotion, val, true
		}
	case "prefers-color-scheme":
		if val == "dark" || val == "light" {
			return mediaColorScheme, val, true
		}
	}
	return 0, "", false
}

func parseKeyframesBlock(prelude, body string) *cssKeyframesRule {
	name := strings.TrimSpace(prelude)
	if len(name) >= 2 && ((name[0] == '"' && name[len(name)-1] == '"') || (name[0] == '\'' && name[len(name)-1] == '\'')) {
		name = name[1 : len(name)-1]
	}
	if name == "" || strings.EqualFold(name, "none") {
		return nil
	}

	// Map offset -> merged declaration map (preserving order).
	type stopAccum struct {
		offset float64
		decls  []Decl
	}
	var stops []stopAccum

	n := len(body)
	i := 0
	for i < n {
		for i < n && (isCSSSpace(body[i]) || body[i] == ';' || body[i] == '}') {
			i++
		}
		if i >= n {
			break
		}
		selStart := i
		for i < n && body[i] != '{' && body[i] != '}' {
			if body[i] == '"' || body[i] == '\'' {
				i = skipCSSString(body, i)
				continue
			}
			i++
		}
		if i >= n || body[i] != '{' {
			if i < n {
				i++
			}
			continue
		}
		selText := strings.TrimSpace(body[selStart:i])
		closeIdx := findMatchingBrace(body, i)
		declBody := body[i+1 : closeIdx]
		if closeIdx < n {
			i = closeIdx + 1
		} else {
			i = n
		}

		offsets := parseKeyframeSelectors(selText)
		if len(offsets) == 0 {
			continue
		}
		rawDecls := parseDeclarations(declBody)
		// Spec §3: !important inside @keyframes is ignored.
		var filtered []Decl
		for _, d := range rawDecls {
			if !d.Important {
				filtered = append(filtered, d)
			}
		}
		if len(filtered) == 0 {
			continue
		}

		for _, off := range offsets {
			found := false
			for idx := range stops {
				if math.Abs(stops[idx].offset-off) < 1e-9 {
					stops[idx].decls = mergeKeyframeDecls(stops[idx].decls, filtered)
					found = true
					break
				}
			}
			if !found {
				stops = append(stops, stopAccum{offset: off, decls: mergeKeyframeDecls(nil, filtered)})
			}
		}
	}

	sort.Slice(stops, func(i, j int) bool {
		return stops[i].offset < stops[j].offset
	})
	out := make([]rawKeyframeStop, len(stops))
	for i, s := range stops {
		out[i] = rawKeyframeStop(s)
	}
	return &cssKeyframesRule{name: name, stops: out}
}

func mergeKeyframeDecls(base, add []Decl) []Decl {
	for _, d := range add {
		replaced := false
		for i := range base {
			if base[i].Name == d.Name {
				base[i] = d
				replaced = true
				break
			}
		}
		if !replaced {
			base = append(base, d)
		}
	}
	return base
}

func parseKeyframeSelectors(selText string) []float64 {
	parts := strings.Split(selText, ",")
	var out []float64
	for _, p := range parts {
		s := strings.ToLower(strings.TrimSpace(p))
		switch s {
		case "from":
			out = append(out, 0)
		case "to":
			out = append(out, 1)
		default:
			if before, ok := strings.CutSuffix(s, "%"); ok {
				v, err := strconv.ParseFloat(strings.TrimSpace(before), 64)
				if err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 100 {
					out = append(out, v/100.0)
				}
			}
		}
	}
	return out
}

// parseDeclarations parses a CSS declaration list (body of a rule or inline style="...").
// Respects comments, strings, and parentheses (e.g. url(...), cubic-bezier(...)).
func parseDeclarations(body string) []Decl {
	if strings.Contains(body, "/*") {
		body = stripCSSComments(body)
	}
	var decls []Decl
	n := len(body)
	i := 0
	for i < n {
		// Split on top-level ';'
		start := i
		paren := 0
		for i < n {
			c := body[i]
			if c == '"' || c == '\'' {
				i = skipCSSString(body, i)
				continue
			}
			if c == '(' {
				paren++
			} else if c == ')' && paren > 0 {
				paren--
			} else if c == ';' && paren == 0 {
				break
			}
			i++
		}
		chunk := strings.TrimSpace(body[start:i])
		if i < n && body[i] == ';' {
			i++
		}
		if chunk == "" {
			continue
		}

		// Find top-level ':'
		colon := -1
		for j := 0; j < len(chunk); j++ {
			c := chunk[j]
			if c == '"' || c == '\'' {
				j = skipCSSString(chunk, j) - 1
				continue
			}
			if c == ':' {
				colon = j
				break
			}
		}
		if colon <= 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(chunk[:colon]))
		val := strings.TrimSpace(chunk[colon+1:])
		if name == "" || val == "" {
			continue
		}
		val, important := stripImportant(val)
		if val == "" {
			continue
		}
		decls = append(decls, Decl{
			Name:      name,
			Value:     val,
			Important: important,
		})
	}
	return decls
}

func stripImportant(val string) (string, bool) {
	// Look for trailing `!` followed by optional whitespace and `important` (case-insensitive).
	bang := strings.LastIndexByte(val, '!')
	if bang < 0 {
		return val, false
	}
	tail := strings.TrimSpace(val[bang+1:])
	if strings.EqualFold(tail, "important") {
		return strings.TrimSpace(val[:bang]), true
	}
	return val, false
}

// parseSelectorList parses comma-separated selectors, skipping unsupported ones leniently.
func parseSelectorList(raw string) ([]cssSelector, []Diag) {
	parts := splitTopLevelComma(raw)
	var out []cssSelector
	var diags []Diag
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		sel, ok := parseSingleSelector(p)
		if !ok {
			diags = append(diags, Diag{Elem: -1, Message: "unsupported CSS selector: " + p})
			continue
		}
		out = append(out, sel)
	}
	return out, diags
}

func splitTopLevelComma(s string) []string {
	var out []string
	n := len(s)
	start := 0
	paren := 0
	bracket := 0
	for i := 0; i < n; i++ {
		c := s[i]
		if c == '"' || c == '\'' {
			i = skipCSSString(s, i) - 1
			continue
		}
		switch c {
		case '(':
			paren++
		case ')':
			if paren > 0 {
				paren--
			}
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		case ',':
			if paren == 0 && bracket == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

func parseSingleSelector(raw string) (cssSelector, bool) {
	// Unsupported syntax: pseudo-classes/elements, attribute selectors, sibling combinators.
	if strings.ContainsAny(raw, ":[]+~") {
		return cssSelector{}, false
	}

	var parts []selectorPart
	var spec specificity

	i := 0
	n := len(raw)
	nextComb := combNone

	for i < n {
		// Skip whitespace and check for combinators.
		sawSpace := false
		for i < n && isCSSSpace(raw[i]) {
			sawSpace = true
			i++
		}
		if i >= n {
			break
		}
		if raw[i] == '>' {
			if len(parts) == 0 || nextComb == combChild {
				return cssSelector{}, false
			}
			nextComb = combChild
			i++
			for i < n && isCSSSpace(raw[i]) {
				i++
			}
			if i >= n {
				return cssSelector{}, false
			}
		} else if len(parts) > 0 && nextComb == combNone {
			if !sawSpace {
				return cssSelector{}, false
			}
			nextComb = combDescendant
		}

		// Parse one compound selector: optional (tag | *) followed by any number of (.class | #id).
		comp, consumed, ok := parseCompoundSelector(raw[i:])
		if !ok || consumed == 0 {
			return cssSelector{}, false
		}
		i += consumed

		if comp.id != "" {
			spec.id++
		}
		spec.class += len(comp.classes)
		if comp.tag != "" {
			spec.typ++
		}
		parts = append(parts, selectorPart{comb: nextComb, comp: comp})
		nextComb = combNone
	}

	if len(parts) == 0 || nextComb != combNone {
		return cssSelector{}, false
	}
	return cssSelector{parts: parts, spec: spec}, true
}

func parseCompoundSelector(s string) (compoundSelector, int, bool) {
	var comp compoundSelector
	i := 0
	n := len(s)

	// Optional universal '*' or type name at start.
	if i < n && s[i] == '*' {
		comp.universal = true
		i++
	} else if i < n && isCSSIdentStart(s[i]) {
		start := i
		for i < n && isCSSIdentChar(s[i]) {
			i++
		}
		comp.tag = s[start:i]
	}

	// Zero or more .class or #id simple selectors.
	for i < n {
		c := s[i]
		if isCSSSpace(c) || c == '>' {
			break
		}
		if c == '.' {
			i++
			start := i
			for i < n && isCSSIdentChar(s[i]) {
				i++
			}
			if i == start {
				return compoundSelector{}, 0, false
			}
			comp.classes = append(comp.classes, s[start:i])
		} else if c == '#' {
			i++
			start := i
			for i < n && isCSSIdentChar(s[i]) {
				i++
			}
			if i == start {
				return compoundSelector{}, 0, false
			}
			// If multiple #id in same compound, fail if they conflict; otherwise keep.
			id := s[start:i]
			if comp.id != "" && comp.id != id {
				return compoundSelector{}, 0, false
			}
			comp.id = id
		} else {
			return compoundSelector{}, 0, false
		}
	}

	if !comp.universal && comp.tag == "" && comp.id == "" && len(comp.classes) == 0 {
		return compoundSelector{}, 0, false
	}
	return comp, i, true
}

func isCSSIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b == '-' || b >= 0x80
}

func isCSSIdentChar(b byte) bool {
	return isCSSIdentStart(b) || (b >= '0' && b <= '9')
}

// --- CSS animation shorthand & longhand parsing ---

type cssAnimDeclSet struct {
	names      []string
	durations  []string
	timings    []string
	delays     []string
	iterations []string
	directions []string
	fillModes  []string
	playStates []string
}

func (s *cssAnimDeclSet) applyDecl(d Decl) {
	switch d.Name {
	case "animation", "-webkit-animation":
		items := splitTopLevelComma(d.Value)
		s.names = make([]string, 0, len(items))
		s.durations = make([]string, 0, len(items))
		s.timings = make([]string, 0, len(items))
		s.delays = make([]string, 0, len(items))
		s.iterations = make([]string, 0, len(items))
		s.directions = make([]string, 0, len(items))
		s.fillModes = make([]string, 0, len(items))
		s.playStates = make([]string, 0, len(items))
		for _, item := range items {
			sh := parseAnimationShorthandItem(item)
			s.names = append(s.names, sh.name)
			s.durations = append(s.durations, sh.duration)
			s.timings = append(s.timings, sh.timing)
			s.delays = append(s.delays, sh.delay)
			s.iterations = append(s.iterations, sh.iteration)
			s.directions = append(s.directions, sh.direction)
			s.fillModes = append(s.fillModes, sh.fillMode)
			s.playStates = append(s.playStates, sh.playState)
		}
	case "animation-name", "-webkit-animation-name":
		s.names = splitTrimmedComma(d.Value)
	case "animation-duration", "-webkit-animation-duration":
		s.durations = splitTrimmedComma(d.Value)
	case "animation-timing-function", "-webkit-animation-timing-function":
		s.timings = splitTrimmedComma(d.Value)
	case "animation-delay", "-webkit-animation-delay":
		s.delays = splitTrimmedComma(d.Value)
	case "animation-iteration-count", "-webkit-animation-iteration-count":
		s.iterations = splitTrimmedComma(d.Value)
	case "animation-direction", "-webkit-animation-direction":
		s.directions = splitTrimmedComma(d.Value)
	case "animation-fill-mode", "-webkit-animation-fill-mode":
		s.fillModes = splitTrimmedComma(d.Value)
	case "animation-play-state", "-webkit-animation-play-state":
		s.playStates = splitTrimmedComma(d.Value)
	}
}

func splitTrimmedComma(s string) []string {
	parts := splitTopLevelComma(s)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

type animShorthandItem struct {
	name      string
	duration  string
	timing    string
	delay     string
	iteration string
	direction string
	fillMode  string
	playState string
}

// tokenizeCSSValues splits a value string by whitespace while keeping (...) and quoted strings intact.
func tokenizeCSSValues(s string) []string {
	var out []string
	n := len(s)
	i := 0
	for i < n {
		for i < n && isCSSSpace(s[i]) {
			i++
		}
		if i >= n {
			break
		}
		start := i
		paren := 0
		for i < n {
			c := s[i]
			if c == '"' || c == '\'' {
				i = skipCSSString(s, i)
				continue
			}
			if c == '(' {
				paren++
			} else if c == ')' && paren > 0 {
				paren--
			} else if isCSSSpace(c) && paren == 0 {
				break
			}
			i++
		}
		out = append(out, s[start:i])
	}
	return out
}

func parseAnimationShorthandItem(item string) animShorthandItem {
	res := animShorthandItem{
		name:      "none",
		duration:  "0s",
		timing:    "ease",
		delay:     "0s",
		iteration: "1",
		direction: "normal",
		fillMode:  "none",
		playState: "running",
	}
	tokens := tokenizeCSSValues(strings.TrimSpace(item))
	var (
		hasDur, hasDelay, hasTiming, hasIter, hasDir, hasFill, hasPlay, hasName bool
	)

	for i, tok := range tokens {
		low := strings.ToLower(tok)

		// 1. Time (1st = duration, 2nd = delay). Check strict CSS time (s/ms),
		//    plus SVGO-minified unitless 0 when an iteration-count is already set or appears later.
		//    Note: bare nonzero number is iteration-count, NOT time!
		isTime := isCSSTimeToken(low) || (isUnitlessZeroToken(low) && (hasIter || hasLaterIterToken(tokens[i+1:])))
		if !hasDur && isTime {
			res.duration = tok
			hasDur = true
			continue
		}
		if hasDur && !hasDelay && isTime {
			res.delay = tok
			hasDelay = true
			continue
		}

		// 2. Timing function
		if !hasTiming && isTimingFuncToken(low) {
			res.timing = tok
			hasTiming = true
			continue
		}

		// 3. Iteration count
		if !hasIter && (low == "infinite" || isNonNegNumber(low)) {
			res.iteration = tok
			hasIter = true
			continue
		}

		// 4. Direction
		if !hasDir && (low == "normal" || low == "reverse" || low == "alternate" || low == "alternate-reverse") {
			res.direction = low
			hasDir = true
			continue
		}

		// 5. Fill mode
		if !hasFill && (low == "forwards" || low == "backwards" || low == "both") {
			res.fillMode = low
			hasFill = true
			continue
		}

		// 6. Play state
		if !hasPlay && (low == "running" || low == "paused") {
			res.playState = low
			hasPlay = true
			continue
		}

		// 7. Special case: "none" (can be fill-mode or animation-name)
		if low == "none" {
			if !hasFill {
				res.fillMode = "none"
				hasFill = true
			} else if !hasName {
				res.name = "none"
				hasName = true
			}
			continue
		}

		// 8. Animation name (unquote if string)
		if !hasName {
			name := tok
			if len(name) >= 2 && ((name[0] == '"' && name[len(name)-1] == '"') || (name[0] == '\'' && name[len(name)-1] == '\'')) {
				name = name[1 : len(name)-1]
			}
			res.name = name
			hasName = true
		}
	}
	return res
}

func isCSSTimeToken(low string) bool {
	if strings.HasSuffix(low, "ms") {
		v, err := strconv.ParseFloat(low[:len(low)-2], 64)
		return err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	if strings.HasSuffix(low, "s") {
		v, err := strconv.ParseFloat(low[:len(low)-1], 64)
		return err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
	}
	return false
}

func isUnitlessZeroToken(low string) bool {
	v, err := strconv.ParseFloat(low, 64)
	return err == nil && v == 0
}

func hasLaterIterToken(rest []string) bool {
	for _, t := range rest {
		low := strings.ToLower(t)
		if low == "infinite" || isNonNegNumber(low) {
			return true
		}
	}
	return false
}

func isTimingFuncToken(low string) bool {
	switch low {
	case "linear", "ease", "ease-in", "ease-out", "ease-in-out", "step-start", "step-end":
		return true
	}
	return strings.HasPrefix(low, "cubic-bezier(") ||
		strings.HasPrefix(low, "steps(") ||
		strings.HasPrefix(low, "linear(")
}

func isNonNegNumber(s string) bool {
	v, err := strconv.ParseFloat(s, 64)
	return err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}
