// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build darwin || freebsd || linux || netbsd || windows

package glfw

import "unicode/utf16"

// Native hooks belong to the window and are accessed only on the main thread.
type nativeWindowState struct {
	text                         func(text string, start, end int, replacement, passthrough bool)
	before, after                string
	drag                         func(phase int, x, y float64, paths []string)
	composition                  func(text string, start, end int, done bool)
	getObject                    func(wparam, lparam uintptr) (uintptr, bool)
	accessibilityChildren        func() uintptr
	accessibilityHitTest         func(x, y float64) uintptr
	accessibilityView            uintptr
	textEnabled                  bool // true from creation; see newNativeWindowState
	textRect                     [4]float64
	selectionStart, selectionEnd int // the marked selection, in the platform's units
	composing                    bool
	dragPaths                    []string // the files of the drag in progress

	// While a key event is interpreted, the IME's text and composition
	// are held in deferred until it is known whether the IME passed the
	// key on as a command, which keyCommand records.
	inKey      bool
	keyCommand bool
	deferred   []func(passthrough bool)
}

// newNativeWindowState is the state of a fresh window: text input on, as a
// window without an app expects the platform's usual key interpretation.
func newNativeWindowState() nativeWindowState { return nativeWindowState{textEnabled: true} }

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
	if f := w.native.composition; f != nil {
		w.emit(func(bool) { f(text, start, end, done) })
	}
}

func (w *Window) SetTextInputEnabled(enabled bool) {
	if w.native.textEnabled == enabled {
		return
	}
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
		n := utf16.RuneLen(r)
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
func (w *Window) SetTextCallback(f func(string, int, int, bool, bool)) { w.native.text = f }

// emit runs fn now, or once the key event being interpreted is, with
// whether the IME passed that key on.
func (w *Window) emit(fn func(passthrough bool)) {
	if w.native.inKey {
		w.native.deferred = append(w.native.deferred, fn)
		return
	}
	fn(false)
}

// interpretKey runs interpret, the platform's interpretation of a key
// event, and then hands on the text and composition it produced, noting
// with the text whether the key also went on as a command: an IME that
// commits and then passes the key on, as the macOS Korean one does with
// Enter, has not consumed it.
func (w *Window) interpretKey(interpret func()) {
	w.native.inKey, w.native.keyCommand = true, false
	interpret()
	w.native.inKey = false
	deferred, pass := w.native.deferred, w.native.keyCommand
	w.native.deferred = nil
	for _, fn := range deferred {
		fn(pass)
	}
}
func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}
