// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package glfw

import (
	"golang.org/x/sys/windows"
	"unsafe"
)

var nativeIMM = windows.NewLazySystemDLL("imm32.dll")
var (
	immGetContext       = nativeIMM.NewProc("ImmGetContext")
	immReleaseContext   = nativeIMM.NewProc("ImmReleaseContext")
	immGetComposition   = nativeIMM.NewProc("ImmGetCompositionStringW")
	immSetCandidate     = nativeIMM.NewProc("ImmSetCandidateWindow")
	immSetComposition   = nativeIMM.NewProc("ImmSetCompositionWindow")
	immAssociateContext = nativeIMM.NewProc("ImmAssociateContextEx")
	immNotify           = nativeIMM.NewProc("ImmNotifyIME")
)

func (w *Window) platformAccessibilityView() uintptr { return 0 }

func (w *Window) platformSetTextInputEnabled(enabled bool) {
	hwnd := uintptr(w.platform.handle)
	if enabled {
		immAssociateContext.Call(hwnd, 0, 0x10) // IACE_DEFAULT
		w.platformSetTextInputRect()
	} else {
		if ctx, _, _ := immGetContext.Call(hwnd); ctx != 0 {
			immNotify.Call(ctx, 0x15, 4, 0) // NI_COMPOSITIONSTR, CPS_CANCEL
			immReleaseContext.Call(hwnd, ctx)
		}
		immAssociateContext.Call(hwnd, 0, 0)
		if w.native.composing {
			w.inputComposition("", 0, 0, true)
		}
	}
}

func (w *Window) platformSetTextInputRect() {
	ctx, _, _ := immGetContext.Call(uintptr(w.platform.handle))
	if ctx == 0 {
		return
	}
	defer immReleaseContext.Call(uintptr(w.platform.handle), ctx)
	r := w.native.textRect
	pt := _POINT{x: int32(r[0]), y: int32(r[1] + r[3])}
	rect := _RECT{left: int32(r[0]), top: int32(r[1]), right: int32(r[0] + r[2]), bottom: int32(r[1] + r[3])}
	candidate := struct {
		index, style uint32
		point        _POINT
		area         _RECT
	}{style: 0x80, point: pt, area: rect} // CFS_EXCLUDE
	immSetCandidate.Call(ctx, uintptr(unsafe.Pointer(&candidate)))
	composition := struct {
		style uint32
		point _POINT
		area  _RECT
	}{style: 2, point: pt} // CFS_POINT
	immSetComposition.Call(ctx, uintptr(unsafe.Pointer(&composition)))
}

func immString(ctx uintptr, index uintptr) string {
	n, _, _ := immGetComposition.Call(ctx, index, 0, 0)
	if int32(n) <= 0 {
		return ""
	}
	buf := make([]uint16, int(n)/2)
	got, _, _ := immGetComposition.Call(ctx, index, uintptr(unsafe.Pointer(&buf[0])), n)
	if int32(got) <= 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:min(len(buf), int(got)/2)])
}

// handleNativeMessage returns before DefWindowProc for handled compositions so
// Windows does not synthesize a second copy of the committed WM_CHAR text.
func (w *Window) handleNativeMessage(msg uint32, wp, lp uintptr) (uintptr, bool) {
	if msg == 0x003d && w.native.getObject != nil {
		return w.native.getObject(wp, lp)
	} // WM_GETOBJECT
	if w.native.composition == nil || !w.native.textEnabled {
		return 0, false
	}
	switch msg {
	case 0x0281: // WM_IME_SETCONTEXT: candidates stay native; marked text is app-drawn.
		return uintptr(_DefWindowProcW(w.platform.handle, msg, _WPARAM(wp), _LPARAM(lp&^0x80000000))), true
	case 0x010d: // WM_IME_STARTCOMPOSITION
		w.platformSetTextInputRect()
		w.inputComposition("", 0, 0, false)
		return 0, true
	case 0x010e: // WM_IME_ENDCOMPOSITION
		w.inputComposition("", 0, 0, true)
		return 0, true
	case 0x010f: // WM_IME_COMPOSITION
		ctx, _, _ := immGetContext.Call(uintptr(w.platform.handle))
		if ctx == 0 {
			return 0, false
		}
		defer immReleaseContext.Call(uintptr(w.platform.handle), ctx)
		if lp&0x800 != 0 { // GCS_RESULTSTR
			text := immString(ctx, 0x800)
			w.inputComposition("", 0, 0, true)
			if w.native.text != nil {
				w.native.text(text, 0, 0, false)
			} else {
				for _, r := range text {
					w.inputChar(r, getKeyMods(), true)
				}
			}
			w.native.before += text
		}
		if lp&(0x8|0x80|0x10) != 0 { // GCS_COMPSTR | GCS_CURSORPOS | GCS_COMPATTR
			text := immString(ctx, 8)
			cursor, _, _ := immGetComposition.Call(ctx, 0x80, 0, 0)
			pos := utf16ByteOffset(text, max(0, int(int32(cursor))))
			start, end := pos, pos
			if n, _, _ := immGetComposition.Call(ctx, 0x10, 0, 0); int32(n) > 0 {
				attrs := make([]byte, int(n))
				got, _, _ := immGetComposition.Call(ctx, 0x10, uintptr(unsafe.Pointer(&attrs[0])), n)
				if int32(got) > 0 {
					for i, attr := range attrs[:min(len(attrs), int(got))] {
						if attr == 1 || attr == 3 { // ATTR_TARGET_CONVERTED / ATTR_TARGET_NOTCONVERTED
							start = utf16ByteOffset(text, i)
							j := i + 1
							for j < int(got) && j < len(attrs) && (attrs[j] == 1 || attrs[j] == 3) {
								j++
							}
							end = utf16ByteOffset(text, j)
							break
						}
					}
				}
			}
			w.inputComposition(text, start, end, false)
		} else if lp == 0 {
			w.inputComposition("", 0, 0, true)
		}
		return 0, true
	}
	return 0, false
}
