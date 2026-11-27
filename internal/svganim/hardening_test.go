// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim_test

import (
	"encoding/xml"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ironpark/ggfx/internal/svganim"
)

type benchScene struct {
	tags    []string
	ids     []string
	classes [][]string
	parents []svganim.ElemID
}

func (s *benchScene) Elems() int                                    { return len(s.tags) }
func (s *benchScene) Tag(e svganim.ElemID) string                   { return s.tags[e] }
func (s *benchScene) ID(e svganim.ElemID) string                    { return s.ids[e] }
func (s *benchScene) Classes(e svganim.ElemID) []string             { return s.classes[e] }
func (s *benchScene) Parent(e svganim.ElemID) svganim.ElemID        { return s.parents[e] }
func (s *benchScene) ByID(string) []svganim.ElemID                  { return nil }
func (s *benchScene) RawAttr(svganim.ElemID, string) (string, bool) { return "", false }

// D-Q5: indexed CSS cascade must preserve specificity, source order, multi-selector
// deduplication (highest matching selector specificity wins), and !important ordering.
func TestCSSCascadeIndexedOrder(t *testing.T) {
	css := `
* { opacity: 0.1; }
rect { fill: red; opacity: 0.2; }
.a, #target { stroke: blue; }
g > rect.a { fill: green; }
#target { fill: yellow; opacity: 0.9 !important; }
.a { opacity: 0.5 !important; }
`
	sc := &benchScene{
		tags:    []string{"g", "rect"},
		ids:     []string{"", "target"},
		classes: [][]string{nil, {"a", "a"}},
		parents: []svganim.ElemID{-1, 0},
	}
	bld := svganim.NewBuilder()
	bld.Stylesheet(css)
	_, sheet, _ := bld.Compile(sc)

	decls := sheet.DeclsFor(sc, 1)
	// Verify every expected declaration appears in exact cascade order and multi-selector rule appears once.
	want := []svganim.Decl{
		{Name: "opacity", Value: "0.1"},
		{Name: "fill", Value: "red"},
		{Name: "opacity", Value: "0.2"},
		{Name: "fill", Value: "green"},
		{Name: "stroke", Value: "blue"},
		{Name: "fill", Value: "yellow"},
		{Name: "opacity", Value: "0.5", Important: true},
		{Name: "opacity", Value: "0.9", Important: true},
	}
	if len(decls) != len(want) {
		t.Fatalf("got %d decls (%v), want %d (%v)", len(decls), decls, len(want), want)
	}
	for i := range want {
		if decls[i] != want[i] {
			t.Errorf("decl[%d] = %+v, want %+v", i, decls[i], want[i])
		}
	}
}

// D-Q5 benchmark: 4,000 rules `g g g .cN{}` matched against 4,000 elements.
func BenchmarkCSSCascade(b *testing.B) {
	const n = 4000
	var css strings.Builder
	for i := range n {
		fmt.Fprintf(&css, "g g g .c%d{fill:#%06x}\n", i, i&0xffffff)
	}
	sc := &benchScene{
		tags:    make([]string, 0, n+3),
		ids:     make([]string, 0, n+3),
		classes: make([][]string, 0, n+3),
		parents: make([]svganim.ElemID, 0, n+3),
	}
	for i := range 3 {
		sc.tags = append(sc.tags, "g")
		sc.ids = append(sc.ids, "")
		sc.classes = append(sc.classes, nil)
		sc.parents = append(sc.parents, svganim.ElemID(i-1))
	}
	for i := range n {
		sc.tags = append(sc.tags, "rect")
		sc.ids = append(sc.ids, "")
		sc.classes = append(sc.classes, []string{fmt.Sprintf("c%d", i)})
		sc.parents = append(sc.parents, 2)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		bld := svganim.NewBuilder()
		bld.Stylesheet(css.String())
		_, sheet, _ := bld.Compile(sc)
		for i := range sc.Elems() {
			_ = sheet.DeclsFor(sc, svganim.ElemID(i))
		}
	}
}

// D-Q6: periodic SMIL interval lookup must terminate immediately even when t/periodDur >= 2^53 or +Inf.
func TestPeriodicIntervalExtremeTime(t *testing.T) {
	sc := &benchScene{
		tags:    []string{"rect"},
		ids:     []string{"r"},
		classes: [][]string{nil},
		parents: []svganim.ElemID{-1},
	}
	bld := svganim.NewBuilder()
	// Two syncbase-chained tracks that form a tiny periodic loop (period = 2ms).
	bld.SMILElement(xml.StartElement{
		Name: xml.Name{Local: "animate"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "id"}, Value: "a"},
			{Name: xml.Name{Local: "attributeName"}, Value: "opacity"},
			{Name: xml.Name{Local: "from"}, Value: "0"},
			{Name: xml.Name{Local: "to"}, Value: "1"},
			{Name: xml.Name{Local: "dur"}, Value: "1ms"},
			{Name: xml.Name{Local: "begin"}, Value: "0s; b.end"},
		},
	}, 0, false)
	bld.SMILElement(xml.StartElement{
		Name: xml.Name{Local: "animate"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "id"}, Value: "b"},
			{Name: xml.Name{Local: "attributeName"}, Value: "opacity"},
			{Name: xml.Name{Local: "from"}, Value: "1"},
			{Name: xml.Name{Local: "to"}, Value: "0"},
			{Name: xml.Name{Local: "dur"}, Value: "1ms"},
			{Name: xml.Name{Local: "begin"}, Value: "a.end"},
		},
	}, 0, false)

	animSet, _, _ := bld.Compile(sc)
	base := svganim.Value{Kind: svganim.ValNumber, Num: 0.5}
	ctx := &svganim.EvalCtx{}
	for _, ts := range []float64{1e15, 1e18, 1e30, 1e308, math.Inf(1)} {
		done := make(chan struct{})
		go func(tVal float64) {
			_, _ = animSet.Eval(0, svganim.PropOpacity, tVal, base, ctx)
			_, _ = animSet.NextChange(tVal)
			close(done)
		}(ts)
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-done:
			timer.Stop()
		case <-timer.C:
			t.Fatalf("extreme time %g stalled past 2s deadline", ts)
		}
	}
}

// Item 9: duplicate class tokens on a single element must stay linear, not quadratic.
func TestCSSCascadeManyDuplicateClasses(t *testing.T) {
	const n = 4000
	classes := make([]string, n)
	for i := range n {
		classes[i] = "dup"
	}
	classes[n-1] = "last"
	sc := &benchScene{
		tags:    []string{"rect"},
		ids:     []string{""},
		classes: [][]string{classes},
		parents: []svganim.ElemID{-1},
	}
	bld := svganim.NewBuilder()
	bld.Stylesheet(`.dup { fill: red; } .last { opacity: 0.5; }`)
	_, sheet, _ := bld.Compile(sc)
	start := time.Now()
	decls := sheet.DeclsFor(sc, 0)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("DeclsFor with %d duplicate classes took %v", n, d)
	}
	if len(decls) != 2 || decls[0].Name != "fill" || decls[1].Name != "opacity" {
		t.Fatalf("unexpected decls: %+v", decls)
	}
}

// Item 8: syncbase queue cap guards initialization and interval-shortening insertions.
func TestSMILSyncQueueBounded(t *testing.T) {
	sc := &benchScene{
		tags:    []string{"rect"},
		ids:     []string{"r"},
		classes: [][]string{nil},
		parents: []svganim.ElemID{-1},
	}
	bld := svganim.NewBuilder()
	// Many static begins + syncbase end shortening listeners.
	var begins strings.Builder
	for i := range 2000 {
		if i > 0 {
			begins.WriteString("; ")
		}
		fmt.Fprintf(&begins, "%dms", i)
	}
	bld.SMILElement(xml.StartElement{
		Name: xml.Name{Local: "animate"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "id"}, Value: "src"},
			{Name: xml.Name{Local: "attributeName"}, Value: "opacity"},
			{Name: xml.Name{Local: "from"}, Value: "0"},
			{Name: xml.Name{Local: "to"}, Value: "1"},
			{Name: xml.Name{Local: "dur"}, Value: "10s"},
			{Name: xml.Name{Local: "begin"}, Value: begins.String()},
			{Name: xml.Name{Local: "end"}, Value: "src.begin+1ms"},
		},
	}, 0, false)
	for i := range 64 {
		bld.SMILElement(xml.StartElement{
			Name: xml.Name{Local: "animate"},
			Attr: []xml.Attr{
				{Name: xml.Name{Local: "id"}, Value: fmt.Sprintf("d%d", i)},
				{Name: xml.Name{Local: "attributeName"}, Value: "opacity"},
				{Name: xml.Name{Local: "from"}, Value: "0"},
				{Name: xml.Name{Local: "to"}, Value: "1"},
				{Name: xml.Name{Local: "dur"}, Value: "1ms"},
				{Name: xml.Name{Local: "begin"}, Value: "src.end"},
			},
		}, 0, false)
	}
	done := make(chan struct{})
	go func() {
		_, _, _ = bld.Compile(sc)
		close(done)
	}()
	timer := time.NewTimer(2 * time.Second)
	select {
	case <-done:
		timer.Stop()
	case <-timer.C:
		t.Fatal("Compile with high-fanout syncbase queue stalled past 2s")
	}
}

// Audit: deeply nested @media and massive @keyframes stop lists must stay bounded.
func TestCSSParserHostileInputBounded(t *testing.T) {
	t.Run("deep-nested-media", func(t *testing.T) {
		const depth = 5000
		var b strings.Builder
		for range depth {
			b.WriteString("@media (prefers-reduced-motion: reduce) {")
		}
		b.WriteString(".a { fill: red; }")
		for range depth {
			b.WriteString("}")
		}
		start := time.Now()
		bld := svganim.NewBuilder()
		bld.Stylesheet(b.String())
		_, _, diags := bld.Compile(nil)
		if d := time.Since(start); d > time.Second {
			t.Fatalf("deep nested @media took %v", d)
		}
		foundDiag := false
		for _, dg := range diags {
			if strings.Contains(dg.Message, "nesting depth") {
				foundDiag = true
				break
			}
		}
		if !foundDiag {
			t.Errorf("expected nesting depth diagnostic, got diags=%+v", diags)
		}
	})

	t.Run("many-keyframe-stops", func(t *testing.T) {
		const n = 5000
		var b strings.Builder
		b.WriteString("@keyframes k {")
		for i := range n {
			fmt.Fprintf(&b, "%.4f%% { opacity: %.4f; }\n", float64(i)*100/float64(n), float64(i)/float64(n))
		}
		b.WriteString("}")
		start := time.Now()
		bld := svganim.NewBuilder()
		bld.Stylesheet(b.String())
		_, _, _ = bld.Compile(nil)
		if d := time.Since(start); d > time.Second {
			t.Fatalf("many keyframe stops took %v", d)
		}
	})
}
