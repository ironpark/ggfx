// Copyright 2019 The Ebiten Authors
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

package gl

import (
	"syscall/js"
	"unsafe"
)

var (
	object      = js.Global().Get("Object")
	arrayBuffer = js.Global().Get("ArrayBuffer")
	uint8Array  = js.Global().Get("Uint8Array")
)

var (
	tmpArrayBufferByteLength = 16

	// tmpArrayBuffer is a temporary buffer used at gl.readPixels or gl.texSubImage2D.
	// The read data is converted to Go's byte slice as soon as possible.
	// To avoid often allocating ArrayBuffer, reuse the buffer whenever possible.
	tmpArrayBuffer = arrayBuffer.New(tmpArrayBufferByteLength)

	// tmpUint8Array is a Uint8ArrayBuffer whose underlying buffer is always temporaryArrayBuffer.
	tmpUint8Array = uint8Array.New(tmpArrayBuffer)
)

func ensureTemporaryArrayBufferSize(byteLength int) {
	if bufl := tmpArrayBufferByteLength; bufl < byteLength {
		for bufl < byteLength {
			bufl *= 2
		}
		tmpArrayBufferByteLength = bufl
		tmpArrayBuffer = arrayBuffer.New(bufl)
		tmpUint8Array = uint8Array.New(tmpArrayBuffer)
	}
}

// tmpUint8ArrayFromSlice returns a Uint8Array whose byte length is at least minLength elements of T.
// Be careful that the length can exceed the given byte length.
// data is copied to the head of the array for initialization, or nil if you don't need initialization.
// Pass T explicitly for nil, e.g. tmpUint8ArrayFromSlice[uint8](n, nil).
func tmpUint8ArrayFromSlice[T numeric](minLength int, data []T) js.Value {
	ensureTemporaryArrayBufferSize(minLength * int(unsafe.Sizeof(T(0))))
	copySliceToTemporaryArrayBuffer(data)
	return tmpUint8Array
}
