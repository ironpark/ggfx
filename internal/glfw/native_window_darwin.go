// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package glfw

import (
	"github.com/ebitengine/purego/objc"
	"github.com/ironpark/ggfx/internal/cocoa"
	"sync"
)

func (w *Window) platformEnableDrag() error { return nil }

func (w *Window) platformSetTextInputEnabled(enabled bool) {
	if !enabled {
		w.platform.view.Send(objc.RegisterName("inputContext")).Send(objc.RegisterName("discardMarkedText"))
		w.platform.view.Send(sel_unmarkText)
	} else {
		w.platform.object.Send(sel_makeFirstResponder, w.platform.view)
	}
}

func (w *Window) platformSetTextInputRect() {
	w.platform.view.Send(objc.RegisterName("inputContext")).Send(objc.RegisterName("invalidateCharacterCoordinates"))
}

func (w *Window) textInputRect() cocoa.NSRect {
	r := w.native.textRect
	bounds := objc.Send[cocoa.NSRect](w.platform.view, sel_bounds)
	rect := cocoa.NSRect{Origin: cocoa.NSPoint{X: r[0], Y: bounds.Size.Height - r[1] - r[3]}, Size: cocoa.CGSize{Width: r[2], Height: r[3]}}
	rect = objc.Send[cocoa.NSRect](w.platform.view, objc.RegisterName("convertRect:toView:"), rect, objc.ID(0))
	return objc.Send[cocoa.NSRect](w.platform.object, objc.RegisterName("convertRectToScreen:"), rect)
}

func cocoaDrag(self, sender objc.ID, phase int) uintptr {
	w := getGoWindow(self)
	if w == nil {
		return 0
	}
	r := objc.Send[cocoa.NSRect](self, sel_bounds)
	p := objc.Send[cocoa.NSPoint](sender, objc.RegisterName("draggingLocation"))
	p = objc.Send[cocoa.NSPoint](self, objc.RegisterName("convertPoint:fromView:"), p, objc.ID(0))
	paths := cocoaDragPaths(sender)
	if f := w.native.drag; f != nil {
		f(phase, p.X, r.Size.Height-p.Y, paths)
	}
	if len(paths) == 0 {
		return 0
	}
	return 1 // NSDragOperationCopy
}

func cocoaDragPaths(sender objc.ID) []string {
	pasteboard := sender.Send(sel_draggingPasteboard)
	classes := objc.ID(class_NSArray).Send(sel_arrayWithObject, objc.ID(class_NSURL))
	yes := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithBool:"), true)
	options := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionaryWithObject:forKey:"), uintptr(yes), uintptr(nsPasteboardURLReadingFileURLsOnlyKey))
	urls := pasteboard.Send(sel_readObjectsForClasses_options, classes, uintptr(options))
	var paths []string
	for i, n := 0, int(urls.Send(sel_count)); i < n; i++ {
		url := urls.Send(sel_objectAtIndex, i)
		if p := url.Send(objc.RegisterName("fileSystemRepresentation")); p != 0 {
			paths = append(paths, goStringFromCString(uintptr(p)))
		}
	}
	return paths
}

var accessibilityClass = sync.OnceValue(func() objc.Class {
	c, err := objc.RegisterClass("GGFXAccessibilityView", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{
		{Cmd: objc.RegisterName("hitTest:"), Fn: func(objc.ID, objc.SEL, cocoa.NSPoint) objc.ID { return 0 }},
		{Cmd: objc.RegisterName("isAccessibilityElement"), Fn: func(objc.ID, objc.SEL) bool { return false }},
		{Cmd: objc.RegisterName("accessibilityRole"), Fn: func(objc.ID, objc.SEL) objc.ID {
			return cocoa.NSString_alloc().InitWithUTF8String("AXGroup").ID.Send(objc.RegisterName("autorelease"))
		}},
		{Cmd: objc.RegisterName("accessibilityChildren"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
			if w := getGoWindow(self); w != nil && w.native.accessibilityChildren != nil {
				return objc.ID(w.native.accessibilityChildren())
			}
			return objc.ID(class_NSArray).Send(objc.RegisterName("array"))
		}},
		{Cmd: objc.RegisterName("accessibilityHitTest:"), Fn: func(self objc.ID, _ objc.SEL, p cocoa.NSPoint) objc.ID {
			if w := getGoWindow(self); w != nil && w.native.accessibilityHitTest != nil {
				return objc.ID(w.native.accessibilityHitTest(p.X, p.Y))
			}
			return 0
		}},
	})
	if err != nil {
		panic(err)
	}
	return c
})

func (w *Window) platformAccessibilityView() uintptr {
	if w.native.accessibilityView != 0 {
		return w.native.accessibilityView
	}
	v := objc.ID(accessibilityClass()).Send(sel_alloc).Send(sel_init)
	v.Send(objc.RegisterName("setFrame:"), objc.Send[cocoa.NSRect](w.platform.view, sel_bounds))
	v.Send(objc.RegisterName("setAutoresizingMask:"), uint(2|16))
	theGoWindows[v] = w
	w.platform.view.Send(objc.RegisterName("addSubview:"), v)
	v.Send(sel_release) // The content view owns its lifetime.
	w.native.accessibilityView = uintptr(v)
	return uintptr(v)
}

func (w *Window) insertNativeText(text string, replacement nsRange) {
	// Control characters and function-key private-use characters remain KeyEvents.
	for _, r := range text {
		if r < 0x20 || (r >= 0xf700 && r <= 0xf7ff) {
			return
		}
	}
	before, after := w.native.before, w.native.after
	marked := ""
	if w.platform.markedText != 0 {
		marked = (cocoa.NSString{ID: w.platform.markedText.Send(sel_string)}).String()
	}
	start, end, replace := 0, 0, replacement.Location < uintptr(^uint(0)>>1)
	if replace {
		prefix := utf16Length(before)
		mark := utf16Length(marked)
		offset := func(pos int, ending bool) int {
			if pos <= prefix {
				return utf16ByteOffset(before, pos) - len(before)
			}
			if pos >= prefix+mark {
				return utf16ByteOffset(after, pos-prefix-mark)
			}
			return 0
		}
		start = offset(int(replacement.Location), false)
		end = offset(int(replacement.Location+replacement.Length), true)
	}
	w.platform.view.Send(sel_unmarkText)
	w.native.text(text, start, end, replace)
	// AppKit can issue a commit and the next marked text in a single key event.
	// Keep its surrounding-text view current before the app drains the events.
	if replace {
		combined := before + after
		lo, hi := max(0, min(len(combined), len(before)+start)), max(0, min(len(combined), len(before)+end))
		w.native.before, w.native.after = combined[:lo]+text, combined[max(lo, hi):]
	} else {
		w.native.before += text
	}
}
