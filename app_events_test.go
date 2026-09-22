// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package ggfx

import (
	"github.com/ironpark/ggfx/internal/ui"
	"testing"
)

type eventTestWindow struct {
	ui.AppWindow
	public *Window
}

func (w eventTestWindow) Handle() any { return w.public }

func TestNativeEventsKeepWindowAndOffsets(t *testing.T) {
	a, b := new(Window), new(Window)
	wa, wb := eventTestWindow{public: a}, eventTestWindow{public: b}
	comp := eventFromUI(ui.CompositionEvent{Window: wa, Text: "한😀", Start: 3, End: 7}).(CompositionEvent)
	if comp.Window != a || comp.Text != "한😀" || comp.Start != 3 || comp.End != 7 || comp.Done {
		t.Fatalf("composition: %+v", comp)
	}
	text := eventFromUI(ui.TextEvent{Window: wb, Text: "é", HasReplacement: true, ReplacementStart: -1, ReplacementEnd: 0}).(TextEvent)
	if text.Window != b || !text.HasReplacement || text.ReplacementStart != -1 || text.ReplacementEnd != 0 {
		t.Fatalf("text: %+v", text)
	}
	drag := eventFromUI(ui.DragEvent{Window: wa, Phase: int(DragMoved), X: 12.5, Y: 30}).(DragEvent)
	if drag.Window != a || drag.Phase != DragMoved || drag.X != 12.5 || drag.Y != 30 {
		t.Fatalf("drag: %+v", drag)
	}
	key := eventFromUI(ui.KeyEvent{Window: wb, Key: ui.KeyA, Pressed: true, Modifiers: ui.KeyModifiers{Meta: true}}).(KeyEvent)
	if key.Window != b || !key.Modifiers.Meta || key.Modifiers.Control {
		t.Fatalf("key: %+v", key)
	}
}
