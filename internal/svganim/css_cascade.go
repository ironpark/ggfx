// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"slices"
	"sort"
	"strings"
)

// buildIndex partitions every selector by its rightmost simple selector
// (ID, class, tag, or universal) so DeclsFor tests only candidate rules.
func (ss *stylesheetImpl) buildIndex() {
	if ss.indexed {
		return
	}
	ss.indexed = true
	for ri := range ss.rules {
		sels := ss.rules[ri].selectors
		for si := range sels {
			parts := sels[si].parts
			if len(parts) == 0 {
				continue
			}
			rc := parts[len(parts)-1].comp
			ref := selectorRef{ruleIdx: int32(ri), selIdx: int32(si)}
			switch {
			case rc.id != "":
				if ss.byID == nil {
					ss.byID = make(map[string][]selectorRef)
				}
				ss.byID[rc.id] = append(ss.byID[rc.id], ref)
			case len(rc.classes) > 0:
				if ss.byClass == nil {
					ss.byClass = make(map[string][]selectorRef)
				}
				ss.byClass[rc.classes[0]] = append(ss.byClass[rc.classes[0]], ref)
			case rc.tag != "":
				if ss.byTag == nil {
					ss.byTag = make(map[string][]selectorRef)
				}
				key := strings.ToLower(rc.tag)
				ss.byTag[key] = append(ss.byTag[key], ref)
			default:
				ss.universal = append(ss.universal, ref)
			}
		}
	}
}

func declsForImpl(s *Stylesheet, sv SceneView, e ElemID) []Decl {
	if s == nil || s.impl == nil || sv == nil || e < 0 {
		return nil
	}
	ss, ok := s.impl.(*stylesheetImpl)
	if !ok || len(ss.rules) == 0 {
		return nil
	}

	type ruleHit struct {
		ruleIdx int32
		spec    specificity
	}
	var hitBuf [8]ruleHit
	hits := hitBuf[:0]
	var hitMap map[int32]int

	record := func(ruleIdx int32, spec specificity) {
		if hitMap != nil {
			if pos, exists := hitMap[ruleIdx]; exists {
				if hits[pos].spec.less(spec) {
					hits[pos].spec = spec
				}
				return
			}
			hitMap[ruleIdx] = len(hits)
			hits = append(hits, ruleHit{ruleIdx: ruleIdx, spec: spec})
			return
		}
		for i := range hits {
			if hits[i].ruleIdx == ruleIdx {
				if hits[i].spec.less(spec) {
					hits[i].spec = spec
				}
				return
			}
		}
		hits = append(hits, ruleHit{ruleIdx: ruleIdx, spec: spec})
		if len(hits) == 16 {
			hitMap = make(map[int32]int, 32)
			for i, h := range hits {
				hitMap[h.ruleIdx] = i
			}
		}
	}

	testRefs := func(refs []selectorRef) {
		for _, ref := range refs {
			sel := ss.rules[ref.ruleIdx].selectors[ref.selIdx]
			if matchSelector(sel, sv, e) {
				record(ref.ruleIdx, sel.spec)
			}
		}
	}

	if ss.indexed {
		if len(ss.byID) > 0 {
			if id := sv.ID(e); id != "" {
				if refs := ss.byID[id]; len(refs) > 0 {
					testRefs(refs)
				}
			}
		}
		if len(ss.byClass) > 0 {
			elemClasses := sv.Classes(e)
			if len(elemClasses) <= 8 {
				for i, cls := range elemClasses {
					if i > 0 && slices.Contains(elemClasses[:i], cls) {
						continue
					}
					if refs := ss.byClass[cls]; len(refs) > 0 {
						testRefs(refs)
					}
				}
			} else {
				seen := make(map[string]struct{}, len(elemClasses))
				for _, cls := range elemClasses {
					if _, dup := seen[cls]; dup {
						continue
					}
					seen[cls] = struct{}{}
					if refs := ss.byClass[cls]; len(refs) > 0 {
						testRefs(refs)
					}
				}
			}
		}
		if len(ss.byTag) > 0 {
			if tag := sv.Tag(e); tag != "" {
				if refs := ss.byTag[strings.ToLower(tag)]; len(refs) > 0 {
					testRefs(refs)
				}
			}
		}
		if len(ss.universal) > 0 {
			testRefs(ss.universal)
		}
	} else {
		for ri := range ss.rules {
			rule := &ss.rules[ri]
			for _, sel := range rule.selectors {
				if matchSelector(sel, sv, e) {
					record(int32(ri), sel.spec)
				}
			}
		}
	}

	if len(hits) == 0 {
		return nil
	}

	type matchedDecl struct {
		spec      specificity
		ruleOrder int
		declOrder int
		decl      Decl
	}

	var matched []matchedDecl
	for _, h := range hits {
		rule := &ss.rules[h.ruleIdx]
		for di, d := range rule.decls {
			matched = append(matched, matchedDecl{
				spec:      h.spec,
				ruleOrder: rule.order,
				declOrder: di,
				decl:      d,
			})
		}
	}

	// Sort: normal rules before !important, then specificity asc, ruleOrder asc, declOrder asc.
	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if a.decl.Important != b.decl.Important {
			return !a.decl.Important
		}
		if a.spec != b.spec {
			return a.spec.less(b.spec)
		}
		if a.ruleOrder != b.ruleOrder {
			return a.ruleOrder < b.ruleOrder
		}
		return a.declOrder < b.declOrder
	})

	out := make([]Decl, len(matched))
	for i, m := range matched {
		out[i] = m.decl
	}
	return out
}

func matchSelector(sel cssSelector, sv SceneView, e ElemID) bool {
	if len(sel.parts) == 0 || e < 0 {
		return false
	}
	return matchSelectorAt(sel.parts, len(sel.parts)-1, sv, e)
}

func matchSelectorAt(parts []selectorPart, idx int, sv SceneView, e ElemID) bool {
	if e < 0 {
		return false
	}
	if !matchCompound(parts[idx].comp, sv, e) {
		return false
	}
	if idx == 0 {
		return true
	}
	switch parts[idx].comb {
	case combChild:
		p := sv.Parent(e)
		if p < 0 {
			return false
		}
		return matchSelectorAt(parts, idx-1, sv, p)
	case combDescendant:
		for p := sv.Parent(e); p >= 0; p = sv.Parent(p) {
			if matchSelectorAt(parts, idx-1, sv, p) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func matchCompound(c compoundSelector, sv SceneView, e ElemID) bool {
	if c.tag != "" && !strings.EqualFold(sv.Tag(e), c.tag) {
		return false
	}
	if c.id != "" && sv.ID(e) != c.id {
		return false
	}
	if len(c.classes) > 0 {
		elemClasses := sv.Classes(e)
		for _, want := range c.classes {
			if !slices.Contains(elemClasses, want) {
				return false
			}
		}
	}
	return true
}

// classDeclsImpl returns legacy single-.class rules for backward-compatible static parsing.
func classDeclsImpl(s *Stylesheet) map[string][]Decl {
	if s == nil || s.impl == nil {
		return nil
	}
	ss, ok := s.impl.(*stylesheetImpl)
	if !ok || len(ss.rules) == 0 {
		return nil
	}

	var out map[string][]Decl
	for _, rule := range ss.rules {
		for _, sel := range rule.selectors {
			if len(sel.parts) != 1 {
				continue
			}
			c := sel.parts[0].comp
			if c.universal || c.tag != "" || c.id != "" || len(c.classes) != 1 {
				continue
			}
			if out == nil {
				out = make(map[string][]Decl)
			}
			cls := c.classes[0]
			out[cls] = append(out[cls], rule.decls...)
		}
	}
	return out
}
