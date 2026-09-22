// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package glfw

import (
	"fmt"
	"golang.org/x/sys/windows"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var dropOLE = windows.NewLazySystemDLL("ole32.dll")
var (
	dropOleInitialize   = dropOLE.NewProc("OleInitialize")
	dropOleUninitialize = dropOLE.NewProc("OleUninitialize")
	dropRegister        = dropOLE.NewProc("RegisterDragDrop")
	dropRevoke          = dropOLE.NewProc("RevokeDragDrop")
	dropReleaseMedium   = dropOLE.NewProc("ReleaseStgMedium")
)

var dropTargets sync.Map                          // uintptr -> *dropTarget; keeps COM instances alive
var windowDropTargets = map[*Window]*dropTarget{} // main thread only

type dropTarget struct {
	vtable *[7]uintptr
	refs   atomic.Uint32
	window *Window
	paths  []string
	x, y   float64
}

var dropVtable = [7]uintptr{
	syscall.NewCallback(dropQueryInterface), syscall.NewCallback(dropAddRef), syscall.NewCallback(dropRelease),
	dropEnterCallback(), dropOverCallback(), syscall.NewCallback(dropLeave), dropCallback(),
}

var dropIID = windows.GUID{Data1: 0x122, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
var unknownIID = windows.GUID{Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}

func dropObject(self uintptr) *dropTarget {
	v, ok := dropTargets.Load(self)
	if !ok {
		return nil
	}
	return v.(*dropTarget)
}

func dropQueryInterface(self uintptr, iid *windows.GUID, result *uintptr) uintptr {
	if result == nil {
		return 0x80004003
	}
	*result = 0
	if iid != nil && (*iid == dropIID || *iid == unknownIID) {
		*result = self
		dropAddRef(self)
		return 0
	}
	return 0x80004002
}

func dropAddRef(self uintptr) uintptr {
	if d := dropObject(self); d != nil {
		return uintptr(d.refs.Add(1))
	}
	return 0
}

func dropRelease(self uintptr) uintptr {
	d := dropObject(self)
	if d == nil {
		return 0
	}
	n := d.refs.Add(^uint32(0))
	if n == 0 {
		dropTargets.Delete(self)
	}
	return uintptr(n)
}

func (w *Window) platformEnableDrag() error {
	if windowDropTargets[w] != nil {
		return nil
	}
	hr, _, _ := dropOleInitialize.Call(0)
	if int32(hr) < 0 {
		return fmt.Errorf("glfw: OleInitialize: HRESULT %#x", uint32(hr))
	}
	d := &dropTarget{vtable: &dropVtable, window: w}
	d.refs.Store(1)
	ptr := uintptr(unsafe.Pointer(d))
	dropTargets.Store(ptr, d)
	hr, _, _ = dropRegister.Call(uintptr(w.platform.handle), ptr)
	if int32(hr) < 0 {
		dropRelease(ptr)
		dropOleUninitialize.Call()
		return fmt.Errorf("glfw: RegisterDragDrop: HRESULT %#x", uint32(hr))
	}
	windowDropTargets[w] = d
	return nil
}

func (w *Window) destroyDropTarget() {
	if d := windowDropTargets[w]; d != nil {
		dropRevoke.Call(uintptr(w.platform.handle))
		delete(windowDropTargets, w)
		d.window = nil
		dropRelease(uintptr(unsafe.Pointer(d)))
		dropOleUninitialize.Call()
	}
}

func dropPaths(data uintptr) []string {
	if data == 0 {
		return nil
	}
	// FORMATETC and STGMEDIUM use pointer-sized alignment on each architecture.
	format := struct {
		format uint16
		target uintptr
		aspect uint32
		index  int32
		medium uint32
	}{format: 15, aspect: 1, index: -1, medium: 1} // CF_HDROP, DVASPECT_CONTENT, TYMED_HGLOBAL
	medium := struct {
		kind   uint32
		handle uintptr
		owner  uintptr
	}{}
	vtable := *(*[12]uintptr)(unsafe.Pointer(*(*uintptr)(unsafe.Pointer(data))))
	hr, _, _ := syscall.SyscallN(vtable[3], data, uintptr(unsafe.Pointer(&format)), uintptr(unsafe.Pointer(&medium)))
	if int32(hr) < 0 {
		return nil
	}
	defer dropReleaseMedium.Call(uintptr(unsafe.Pointer(&medium)))
	if medium.kind != 1 || medium.handle == 0 {
		return nil
	}
	h := _HDROP(medium.handle)
	n := _DragQueryFileW(h, 0xffffffff, nil)
	paths := make([]string, 0, n)
	for i := uint32(0); i < n; i++ {
		buf := make([]uint16, _DragQueryFileW(h, i, nil)+1)
		_DragQueryFileW(h, i, buf)
		paths = append(paths, windows.UTF16ToString(buf))
	}
	return paths
}

func (d *dropTarget) send(phase int, x, y int32, effect *uint32) {
	if effect != nil {
		*effect &= 1
	} // only copying files is supported
	if d.window == nil || len(d.paths) == 0 {
		if effect != nil {
			*effect = 0
		}
		return
	}
	p := _POINT{x: x, y: y}
	if err := _ScreenToClient(d.window.platform.handle, &p); err != nil {
		if effect != nil {
			*effect = 0
		}
		return
	}
	d.x, d.y = float64(p.x), float64(p.y)
	if f := d.window.native.drag; f != nil {
		f(phase, d.x, d.y, d.paths)
	}
}

func dropEnter(self, data uintptr, x, y int32, effect *uint32) uintptr {
	if d := dropObject(self); d != nil {
		d.paths = dropPaths(data)
		d.send(DragEntered, x, y, effect)
	}
	return 0
}

func dropOver(self uintptr, x, y int32, effect *uint32) uintptr {
	if d := dropObject(self); d != nil {
		d.send(DragMoved, x, y, effect)
	}
	return 0
}

func dropLeave(self uintptr) uintptr {
	if d := dropObject(self); d != nil && d.window != nil {
		if f := d.window.native.drag; f != nil && len(d.paths) > 0 {
			f(DragExited, d.x, d.y, nil)
		}
		d.paths = nil
	}
	return 0
}

func dropFiles(self, data uintptr, x, y int32, effect *uint32) uintptr {
	if d := dropObject(self); d != nil && d.window != nil {
		d.paths = dropPaths(data)
		d.send(DragMoved, x, y, effect)
		if len(d.paths) > 0 && effect != nil && *effect != 0 {
			d.window.inputCursorPos(d.x, d.y)
			d.window.inputDrop(d.paths)
		}
		if f := d.window.native.drag; f != nil {
			f(DragEnded, d.x, d.y, nil)
		}
		d.paths = nil
	}
	return 0
}
