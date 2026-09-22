// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin || freebsd || linux || netbsd || windows

package glfw

// Native hooks belong to the window and are accessed only on the main thread.
type nativeWindowState struct {
	text                         func(text string, start, end int, replacement bool)
	before, after                string
	drag                         func(phase int, x, y float64, paths []string)
	composition                  func(text string, start, end int, done bool)
	getObject                    func(wparam, lparam uintptr) (uintptr, bool)
	accessibilityChildren        func() uintptr
	accessibilityHitTest         func(x, y float64) uintptr
	accessibilityView            uintptr
	textEnabled                  bool
	textConfigured               bool
	textRect                     [4]float64
	selectionStart, selectionEnd int
	composing                    bool
}

const (
	DragEntered = iota
	DragMoved
	DragExited
	DragEnded
)

func (w *Window) SetDragCallback(f func(phase int, x, y float64, paths []string)) error {
	w.native.drag = f
	return w.platformEnableDrag()
}

func (w *Window) SetCompositionCallback(f func(text string, start, end int, done bool)) {
	w.native.composition = f
}

func (w *Window) inputComposition(text string, start, end int, done bool) {
	w.native.composing = !done && text != ""
	w.native.selectionStart, w.native.selectionEnd = start, end
	if f := w.native.composition; f != nil {
		f(text, start, end, done)
	}
}

func (w *Window) SetTextInputEnabled(enabled bool) {
	if w.native.textConfigured && w.native.textEnabled == enabled {
		return
	}
	w.native.textConfigured = true
	w.native.textEnabled = enabled
	w.platformSetTextInputEnabled(enabled)
}

func (w *Window) SetTextInputRect(x, y, width, height float64) {
	if w.native.textRect == [4]float64{x, y, width, height} {
		return
	}
	w.native.textRect = [4]float64{x, y, width, height}
	w.platformSetTextInputRect()
}

func (w *Window) SetGetObjectHandler(f func(wparam, lparam uintptr) (uintptr, bool)) {
	w.native.getObject = f
}

// The children callback returns an autoreleased NSArray, the hit test an
// NSAccessibilityElement. Both callbacks run synchronously on the main thread.
func (w *Window) SetAccessibilityHandlers(children func() uintptr, hitTest func(x, y float64) uintptr) {
	w.native.accessibilityChildren = children
	w.native.accessibilityHitTest = hitTest
}

func (w *Window) AccessibilityView() uintptr { return w.platformAccessibilityView() }

// utf16ByteOffset clamps a UTF-16 offset to a UTF-8 character boundary.
func utf16ByteOffset(text string, offset int) int {
	units := 0
	for i, r := range text {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > offset {
			return i
		}
		units += n
	}
	return len(text)
}

func (w *Window) SetTextInputContext(before, after string) {
	w.native.before, w.native.after = before, after
}
func (w *Window) SetTextCallback(f func(string, int, int, bool)) { w.native.text = f }
func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
