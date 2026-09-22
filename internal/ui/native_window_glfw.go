// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

//go:build !android && !ios && !js && !nintendosdk && !playstation5

package ui

import (
	"github.com/ironpark/ggfx/internal/file"
	"github.com/ironpark/ggfx/internal/glfw"
	"io/fs"
)

func (w *appWindow) withNativeWindow(f func(*glfw.Window)) {
	b := w.backend
	if b.isTerminated() {
		return
	}
	b.mainThread.Call(func() {
		if !b.isTerminated() && !b.closed {
			f(b.window)
		}
	})
}

func (w *appWindow) SetTextInputEnabled(enabled bool) {
	w.withNativeWindow(func(n *glfw.Window) { n.SetTextInputEnabled(enabled) })
}

func (w *appWindow) SetTextInputRect(x, y, width, height float64) {
	w.withNativeWindow(func(n *glfw.Window) {
		s := 1.0
		if m, err := w.backend.currentMonitor(); err == nil && m != nil {
			s = m.DeviceScaleFactor()
		}
		n.SetTextInputRect(dipToGLFWPixel(x, s), dipToGLFWPixel(y, s), dipToGLFWPixel(width, s), dipToGLFWPixel(height, s))
	})
}

func (w *appWindow) AccessibilityView() (view uintptr) {
	w.withNativeWindow(func(n *glfw.Window) { view = n.AccessibilityView() })
	return
}

func (w *appWindow) SetAccessibilityHandlers(children func() uintptr, hitTest func(x, y float64) uintptr) {
	w.withNativeWindow(func(n *glfw.Window) { n.SetAccessibilityHandlers(children, hitTest) })
}

func (w *appWindow) SetGetObjectHandler(f func(wparam, lparam uintptr) (uintptr, bool)) {
	w.withNativeWindow(func(n *glfw.Window) { n.SetGetObjectHandler(f) })
}

func (u *glfwBackend) registerNativeCallbacks() error {
	aw := u.appWindow()
	if aw == nil {
		return nil
	}
	u.window.SetTextInputEnabled(true)
	u.window.SetTextCallback(func(text string, start, end int, replacement bool) {
		u.pushEvent(TextEvent{Window: aw, Text: text, ReplacementStart: start, ReplacementEnd: end, HasReplacement: replacement})
		u.context.(*eventContext).requestFrame()
	})
	u.window.SetCompositionCallback(func(text string, start, end int, done bool) {
		u.pushEvent(CompositionEvent{Window: aw, Text: text, Start: start, End: end, Done: done})
		// Already on the main thread: do not enter RequestFrame's thread hop.
		u.context.(*eventContext).requestFrame()
	})
	return u.window.SetDragCallback(func(phase int, x, y float64, paths []string) {
		x, y = u.cursorPositionInDIP(x, y)
		var files fs.FS
		if len(paths) != 0 {
			var err error
			files, err = file.NewVirtualFS(paths)
			if err != nil {
				u.setError(err)
				return
			}
		}
		u.pushEvent(DragEvent{Window: aw, Phase: phase, X: x, Y: y, Files: files})
		u.context.(*eventContext).requestFrame()
	})
}

func (w *appWindow) SetTextInputContext(before, after string) {
	w.withNativeWindow(func(n *glfw.Window) { n.SetTextInputContext(before, after) })
}
