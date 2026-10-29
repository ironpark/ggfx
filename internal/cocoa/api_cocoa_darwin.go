// Copyright 2022 The Ebitengine Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package cocoa is the part of Foundation and AppKit the Metal driver uses: strings, errors,
// autorelease pools, run loops and the view a layer is set on.
package cocoa

import (
	"fmt"
	"structs"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"
)

var (
	class_NSAutoreleasePool objc.Class
	class_NSString          objc.Class
	class_NSRunLoop         objc.Class
	class_NSMachPort        objc.Class
)

func init() {
	// Load Foundation and AppKit frameworks to ensure ObjC classes are available.
	// With CGO_ENABLED=0, frameworks are not loaded automatically by the linker,
	// so objc.GetClass would return 0 for classes like NSString.
	if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_LAZY|purego.RTLD_GLOBAL); err != nil {
		panic(fmt.Errorf("cocoa: failed to dlopen Foundation: %w", err))
	}

	// AppKit may not be available (e.g. on iOS), so ignore errors.
	_, _ = purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_LAZY|purego.RTLD_GLOBAL)

	class_NSAutoreleasePool = objc.GetClass("NSAutoreleasePool")
	class_NSString = objc.GetClass("NSString")
	class_NSRunLoop = objc.GetClass("NSRunLoop")
	class_NSMachPort = objc.GetClass("NSMachPort")

	NSRunLoopCommonModes = NSRunLoopMode(NSString_alloc().InitWithUTF8String("kCFRunLoopCommonModes"))
	NSDefaultRunLoopMode = NSRunLoopMode(NSString_alloc().InitWithUTF8String("kCFRunLoopDefaultMode"))
}

var (
	sel_alloc                        = objc.RegisterName("alloc")
	sel_new                          = objc.RegisterName("new")
	sel_release                      = objc.RegisterName("release")
	sel_initWithUTF8String           = objc.RegisterName("initWithUTF8String:")
	sel_setLayer                     = objc.RegisterName("setLayer:")
	sel_setWantsLayer                = objc.RegisterName("setWantsLayer:")
	sel_setLayerContentsRedrawPolicy = objc.RegisterName("setLayerContentsRedrawPolicy:")
	sel_mainRunLoop                  = objc.RegisterName("mainRunLoop")
	sel_currentRunLoop               = objc.RegisterName("currentRunLoop")
	sel_run                          = objc.RegisterName("run")
	sel_performBlock                 = objc.RegisterName("performBlock:")
	sel_port                         = objc.RegisterName("port")
	sel_addPort_forMode              = objc.RegisterName("addPort:forMode:")
)

type CGFloat = float64

type CGSize struct {
	_             structs.HostLayout
	Width, Height CGFloat
}

type NSInteger = int

type NSError struct {
	objc.ID
}

type NSView struct {
	objc.ID
}

func (v NSView) SetLayer(layer uintptr) {
	v.Send(sel_setLayer, layer)
}

type NSViewLayerContentsRedrawPolicy NSInteger

const (
	NSViewLayerContentsRedrawNever NSViewLayerContentsRedrawPolicy = 0
)

func (v NSView) SetLayerContentsRedrawPolicy(policy NSViewLayerContentsRedrawPolicy) {
	v.Send(sel_setLayerContentsRedrawPolicy, int(policy))
}

func (v NSView) SetWantsLayer(wantsLayer bool) {
	v.Send(sel_setWantsLayer, wantsLayer)
}

type NSAutoreleasePool struct {
	objc.ID
}

func NSAutoreleasePool_new() NSAutoreleasePool {
	return NSAutoreleasePool{objc.ID(class_NSAutoreleasePool).Send(sel_new)}
}

func (p NSAutoreleasePool) Release() {
	p.Send(sel_release)
}

type NSString struct {
	objc.ID
}

func NSString_alloc() NSString {
	return NSString{objc.ID(class_NSString).Send(sel_alloc)}
}

func (s NSString) InitWithUTF8String(utf8 string) NSString {
	return NSString{s.Send(sel_initWithUTF8String, utf8)}
}

func (s NSString) String() string {
	return cstrings.NSStringToString(s.ID)
}

type NSRunLoop struct {
	objc.ID
}

func NSRunLoop_mainRunLoop() NSRunLoop {
	return NSRunLoop{objc.ID(class_NSRunLoop).Send(sel_mainRunLoop)}
}

func NSRunLoop_currentRunLoop() NSRunLoop {
	return NSRunLoop{objc.ID(class_NSRunLoop).Send(sel_currentRunLoop)}
}

func (r NSRunLoop) AddPort(port NSMachPort, mode NSRunLoopMode) {
	r.Send(sel_addPort_forMode, port.ID, mode)
}

func (r NSRunLoop) Run() {
	r.Send(sel_run)
}

func (r NSRunLoop) PerformBlock(block objc.Block) {
	r.Send(sel_performBlock, block)
}

type NSRunLoopMode NSString

var (
	NSRunLoopCommonModes NSRunLoopMode
	NSDefaultRunLoopMode NSRunLoopMode
)

type NSMachPort struct {
	objc.ID
}

func NSMachPort_port() NSMachPort {
	return NSMachPort{objc.ID(class_NSMachPort).Send(sel_port)}
}
