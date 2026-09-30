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
	"unsafe"
)

// On arm64, a struct larger than 16 bytes that is not a homogeneous aggregate of at most four
// floats is passed by reference. Viewport (six doubles) and ScissorRect (four integers) are such
// structs, so they are passed as pointers.

// SetViewport sets the viewport used for transformations and clipping.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515527-setviewport?language=objc.
func (rce RenderCommandEncoder) SetViewport(viewport Viewport) {
	msgSend(rce.commandEncoder, sel_setViewport, uintptr(unsafe.Pointer(&viewport)))
}

// SetScissorRect sets the scissor rectangle for a fragment scissor test.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515583-setscissorrect?language=objc.
func (rce RenderCommandEncoder) SetScissorRect(scissorRect ScissorRect) {
	msgSend(rce.commandEncoder, sel_setScissorRect, uintptr(unsafe.Pointer(&scissorRect)))
}
