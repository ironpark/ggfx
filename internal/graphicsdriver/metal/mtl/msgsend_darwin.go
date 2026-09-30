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

package mtl

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// objc.ID.Send boxes its arguments in interfaces and calls through reflection, which allocates
// several objects per call. The methods that run for every draw call objc_msgSend with msgSend
// instead.
var objcMsgSend uintptr

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

// msgSend sends sel to id with args, and returns the integer or pointer result. args must be
// integers, pointers, or structs that the ABI passes in integer registers or by reference;
// floating-point arguments are not supported.
//
// A pointer converted to uintptr in the call's arguments stays valid until msgSend returns.
//
//go:uintptrescapes
func msgSend(id objc.ID, sel objc.SEL, args ...uintptr) uintptr {
	var buf [8]uintptr
	all := append(append(buf[:0], uintptr(id), uintptr(sel)), args...)
	r, _, _ := purego.SyscallN(objcMsgSend, all...)
	return r
}
