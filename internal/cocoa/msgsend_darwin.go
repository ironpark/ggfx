// Copyright 2026 The ggfx Authors
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

package cocoa

import (
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// objc.ID.Send boxes its arguments in interfaces and calls through reflection, which allocates
// several objects per call. The methods that run every frame call objc_msgSend with MsgSend
// instead.
var objcMsgSend uintptr

// msgSendArgs holds the arguments of a MsgSend call. purego.SyscallN lets its arguments escape,
// so a buffer on the stack would be moved to the heap on every call.
var msgSendArgs = sync.Pool{New: func() any { return new([maxMsgSendArgs]uintptr) }}

// maxMsgSendArgs is the most arguments purego.SyscallN takes.
const maxMsgSendArgs = 15

func init() {
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	objcMsgSend, err = purego.Dlsym(lib, "objc_msgSend")
	if err != nil {
		panic(err)
	}
}

// MsgSend0 to MsgSend5 send sel to id with as many arguments, and return the integer or pointer
// result. The arguments must be integers, pointers, or structs that the ABI passes in integer
// registers or by reference; floating-point arguments are not supported. Each arity has a
// function of its own, as a variadic slice of arguments would escape to the heap on every call.
//
// A pointer converted to uintptr in the call's arguments stays valid until the function returns.

//go:uintptrescapes
func MsgSend0(id objc.ID, sel objc.SEL) uintptr {
	return msgSend(2, uintptr(id), uintptr(sel), 0, 0, 0, 0, 0)
}

//go:uintptrescapes
func MsgSend1(id objc.ID, sel objc.SEL, a1 uintptr) uintptr {
	return msgSend(3, uintptr(id), uintptr(sel), a1, 0, 0, 0, 0)
}

//go:uintptrescapes
func MsgSend2(id objc.ID, sel objc.SEL, a1, a2 uintptr) uintptr {
	return msgSend(4, uintptr(id), uintptr(sel), a1, a2, 0, 0, 0)
}

//go:uintptrescapes
func MsgSend3(id objc.ID, sel objc.SEL, a1, a2, a3 uintptr) uintptr {
	return msgSend(5, uintptr(id), uintptr(sel), a1, a2, a3, 0, 0)
}

//go:uintptrescapes
func MsgSend4(id objc.ID, sel objc.SEL, a1, a2, a3, a4 uintptr) uintptr {
	return msgSend(6, uintptr(id), uintptr(sel), a1, a2, a3, a4, 0)
}

//go:uintptrescapes
func MsgSend5(id objc.ID, sel objc.SEL, a1, a2, a3, a4, a5 uintptr) uintptr {
	return msgSend(7, uintptr(id), uintptr(sel), a1, a2, a3, a4, a5)
}

// msgSend calls objc_msgSend with the first n of id, sel and a1 to a5.
func msgSend(n int, id, sel, a1, a2, a3, a4, a5 uintptr) uintptr {
	buf := msgSendArgs.Get().(*[maxMsgSendArgs]uintptr)
	buf[0], buf[1], buf[2], buf[3], buf[4], buf[5], buf[6] = id, sel, a1, a2, a3, a4, a5
	r, _, _ := purego.SyscallN(objcMsgSend, buf[:n]...)
	msgSendArgs.Put(buf)
	return r
}
