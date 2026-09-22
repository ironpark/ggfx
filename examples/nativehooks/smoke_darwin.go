// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"math"
	"structs"
	"sync"

	"github.com/ebitengine/purego/objc"
	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/cocoa"
)

type nsRange struct {
	_                structs.HostLayout
	location, length uintptr
}

var dragPasteboards = map[objc.ID]objc.ID{}
var dragClass = sync.OnceValue(func() objc.Class {
	c, err := objc.RegisterClass("GGFXSmokeDraggingInfo", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: objc.RegisterName("draggingPasteboard"), Fn: func(self objc.ID, _ objc.SEL) objc.ID { return dragPasteboards[self] }},
		{Cmd: objc.RegisterName("draggingLocation"), Fn: func(objc.ID, objc.SEL) cocoa.NSPoint { return cocoa.NSPoint{X: 30, Y: 160} }},
	})
	if err != nil {
		panic(err)
	}
	return c
})

func injectNative(w *ggfx.Window) (err error) {
	handle, container := w.NativeHandle(), w.AccessibilityView()
	if handle == 0 || container == 0 {
		return fmt.Errorf("missing native window or accessibility container")
	}
	marker := handle
	w.SetAccessibilityHandlers(func() uintptr { return marker }, func(x, y float64) uintptr { return marker })
	defer w.SetAccessibilityHandlers(nil, nil)
	ggfx.RunOnMainThread(func() {
		win := objc.ID(handle)
		view := win.Send(objc.RegisterName("contentView"))
		ax := objc.ID(container)
		if ax.Send(objc.RegisterName("window")) != win {
			err = fmt.Errorf("accessibility view belongs to a different window")
			return
		}
		if uintptr(ax.Send(objc.RegisterName("accessibilityChildren"))) != marker {
			err = fmt.Errorf("accessibility callback crossed windows")
			return
		}
		if ax.Send(objc.RegisterName("hitTest:"), cocoa.NSPoint{X: 1, Y: 1}) != 0 {
			err = fmt.Errorf("accessibility view intercepted the mouse")
			return
		}
		text := func(s string) objc.ID {
			return cocoa.NSString_alloc().InitWithUTF8String(s).ID.Send(objc.RegisterName("autorelease"))
		}
		noRange := nsRange{location: math.MaxInt}
		view.Send(objc.RegisterName("setMarkedText:selectedRange:replacementRange:"), text("한😀"), nsRange{location: 1, length: 2}, noRange)
		marked := objc.Send[nsRange](view, objc.RegisterName("markedRange"))
		if marked.length != 3 {
			err = fmt.Errorf("markedRange length = %d, want 3", marked.length)
			return
		}
		rect := objc.Send[cocoa.NSRect](view, objc.RegisterName("firstRectForCharacterRange:actualRange:"), noRange, uintptr(0))
		if rect.Size.Width != 2 || rect.Size.Height != 20 {
			err = fmt.Errorf("candidate rectangle: %+v", rect)
			return
		}
		view.Send(objc.RegisterName("insertText:replacementRange:"), text("한"), noRange)
		view.Send(objc.RegisterName("setMarkedText:selectedRange:replacementRange:"), text("ㄱ"), nsRange{location: 1}, noRange)
		view.Send(objc.RegisterName("unmarkText"))
	})
	if err != nil {
		return err
	}
	w.SetTextInputContext("a", "z")
	ggfx.RunOnMainThread(func() {
		view := objc.ID(handle).Send(objc.RegisterName("contentView"))
		text := cocoa.NSString_alloc().InitWithUTF8String("é")
		view.Send(objc.RegisterName("insertText:replacementRange:"), text.ID, nsRange{location: 0, length: 1})
		text.ID.Send(objc.RegisterName("release"))
		pb := objc.ID(objc.GetClass("NSPasteboard")).Send(objc.RegisterName("pasteboardWithUniqueName"))
		path := cocoa.NSString_alloc().InitWithUTF8String("/tmp")
		url := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("fileURLWithPath:"), path.ID)
		path.ID.Send(objc.RegisterName("release"))
		array := objc.ID(objc.GetClass("NSArray")).Send(objc.RegisterName("arrayWithObject:"), url)
		pb.Send(objc.RegisterName("writeObjects:"), array)
		sender := objc.ID(dragClass()).Send(objc.RegisterName("alloc")).Send(objc.RegisterName("init"))
		dragPasteboards[sender] = pb
		for _, sel := range []string{"draggingEntered:", "draggingUpdated:", "draggingExited:", "draggingEntered:", "performDragOperation:", "draggingEnded:"} {
			view.Send(objc.RegisterName(sel), sender)
		}
		delete(dragPasteboards, sender)
		sender.Send(objc.RegisterName("release"))
		pb.Send(objc.RegisterName("releaseGlobally"))
	})
	return err
}
