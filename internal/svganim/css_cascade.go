// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"slices"
	"sort"
	"strings"
)

func declsForImpl(s *Stylesheet, sv SceneView, e ElemID) []Decl {
	if s == nil || s.impl == nil || sv == nil || e < 0 {
		return nil
	}
	ss, ok := s.impl.(*stylesheetImpl)
	if !ok || len(ss.rules) == 0 {
		return nil
	}

	type matchedDecl struct {
		spec      specificity
		ruleOrder int
		declOrder int
		decl      Decl
	}

	var matched []matchedDecl
	for _, rule := range ss.rules {
		bestSpec := specificity{-1, -1, -1}
		hit := false
		for _, sel := range rule.selectors {
			if matchSelector(sel, sv, e) {
				if !hit || bestSpec.less(sel.spec) {
					bestSpec = sel.spec
					hit = true
				}
			}
		}
		if !hit {
			continue
		}
		for di, d := range rule.decls {
			matched = append(matched, matchedDecl{
				spec:      bestSpec,
				ruleOrder: rule.order,
				declOrder: di,
				decl:      d,
			})
		}
	}

	if len(matched) == 0 {
		return nil
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
