// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin || freebsd || linux || netbsd || windows

package glfw

import "testing"

func TestUTF16ByteOffset(t *testing.T) {
	for _, tt := range []struct{ offset, want int }{{-1, 0}, {0, 0}, {1, 1}, {2, 4}, {3, 4}, {4, 8}, {5, 9}, {99, 9}} {
		if got := utf16ByteOffset("a한😀z", tt.offset); got != tt.want {
			t.Errorf("offset %d: got %d, want %d", tt.offset, got, tt.want)
		}
	}
	if got := utf16Length("a한😀z"); got != 5 {
		t.Fatalf("UTF-16 length = %d", got)
	}
}

func TestCompositionCallbacksBelongToWindow(t *testing.T) {
	var a, b Window
	var gotA, gotB []string
	a.SetCompositionCallback(func(text string, start, end int, done bool) { gotA = append(gotA, text) })
	b.SetCompositionCallback(func(text string, start, end int, done bool) { gotB = append(gotB, text) })
	a.inputComposition("한", 3, 3, false)
	b.inputComposition("日", 0, 3, false)
	a.inputComposition("", 0, 0, true)
	if len(gotA) != 2 || gotA[0] != "한" || gotA[1] != "" || len(gotB) != 1 || gotB[0] != "日" {
		t.Fatalf("callbacks crossed windows: %q / %q", gotA, gotB)
	}
	if a.native.composing || !b.native.composing {
		t.Fatal("ending one composition changed the other window")
	}
}
