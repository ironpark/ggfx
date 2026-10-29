// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build freebsd || linux || netbsd

package glfw

import "testing"

func TestPreeditIsTheWindowsComposition(t *testing.T) {
	w := &Window{native: newNativeWindowState()}
	type comp struct {
		text       string
		start, end int
		done       bool
	}
	var got []comp
	w.SetCompositionCallback(func(text string, start, end int, done bool) {
		got = append(got, comp{text, start, end, done})
	})
	w.inputPreedit() // nothing composed: no composition to end
	w.platform.preeditText = []rune("한")
	w.platform.preeditCaret = 1
	w.inputPreedit()
	w.clearPreedit()
	w.inputPreedit()
	want := []comp{{"한", 3, 3, false}, {"", 0, 0, true}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("compositions %+v; want %+v", got, want)
	}
}

func TestTextInputIsActiveWhileEnabled(t *testing.T) {
	w := &Window{native: newNativeWindowState()}
	if !w.textInputActive() {
		t.Fatal("a new window's text input is not active")
	}
	w.SetTextInputEnabled(false)
	if w.textInputActive() {
		t.Fatal("text input is active after it was disabled")
	}
	w.platform.textInputActiveCallback = func(*Window) bool { return true }
	if !w.textInputActive() {
		t.Fatal("the callback does not decide")
	}
}
