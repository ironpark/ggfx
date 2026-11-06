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
	"sync"
	"unsafe"

	"github.com/ironpark/ggfx/internal/cocoa"
)

// On arm64, a struct larger than 16 bytes that is not a homogeneous aggregate of at most four
// floats is passed by reference. Viewport (six doubles) and ScissorRect (four integers) are such
// structs, so they are passed as pointers.
//
// MsgSend lets a pointer argument escape, so a struct on the stack would be moved to the heap on
// every call. The structs are copied into pooled ones instead. The callee reads the struct before
// it returns, as the struct is passed by value, so the copy can be reused once the call returns.

var (
	viewportArgs    = sync.Pool{New: func() any { return new(Viewport) }}
	scissorRectArgs = sync.Pool{New: func() any { return new(ScissorRect) }}
)

// SetViewport sets the viewport used for transformations and clipping.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515527-setviewport?language=objc.
func (rce RenderCommandEncoder) SetViewport(viewport Viewport) {
	arg := viewportArgs.Get().(*Viewport)
	*arg = viewport
	cocoa.MsgSend1(rce.commandEncoder, sel_setViewport, uintptr(unsafe.Pointer(arg)))
	viewportArgs.Put(arg)
}

// SetScissorRect sets the scissor rectangle for a fragment scissor test.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515583-setscissorrect?language=objc.
func (rce RenderCommandEncoder) SetScissorRect(scissorRect ScissorRect) {
	arg := scissorRectArgs.Get().(*ScissorRect)
	*arg = scissorRect
	cocoa.MsgSend1(rce.commandEncoder, sel_setScissorRect, uintptr(unsafe.Pointer(arg)))
	scissorRectArgs.Put(arg)
}

// copyFromBufferArgs holds the struct arguments of CopyFromBuffer.
type copyFromBufferArgs struct {
	size   Size
	origin Origin
}

var copyFromBufferArgsPool = sync.Pool{New: func() any { return new(copyFromBufferArgs) }}

// CopyFromBuffer encodes a command that copies image data from a buffer into a texture.
//
// Reference: https://developer.apple.com/documentation/metal/mtlblitcommandencoder/1400752-copyfrombuffer?language=objc.
func (bce BlitCommandEncoder) CopyFromBuffer(sourceBuffer Buffer, sourceOffset, sourceBytesPerRow, sourceBytesPerImage int, sourceSize Size, destinationTexture Texture, destinationSlice, destinationLevel int, destinationOrigin Origin) {
	// MTLSize and MTLOrigin, three integers each, are passed by reference. The arguments past the
	// sixth, the slice, the level and the origin's address, go on the stack, eight bytes each.
	arg := copyFromBufferArgsPool.Get().(*copyFromBufferArgs)
	arg.size = sourceSize
	arg.origin = destinationOrigin
	cocoa.MsgSend9(bce.commandEncoder, sel_copyFromBuffer_sourceOffset_sourceBytesPerRow_sourceBytesPerImage_sourceSize_toTexture_destinationSlice_destinationLevel_destinationOrigin,
		uintptr(sourceBuffer.buffer), uintptr(sourceOffset), uintptr(sourceBytesPerRow), uintptr(sourceBytesPerImage),
		uintptr(unsafe.Pointer(&arg.size)), uintptr(destinationTexture.texture), uintptr(destinationSlice), uintptr(destinationLevel),
		uintptr(unsafe.Pointer(&arg.origin)))
	copyFromBufferArgsPool.Put(arg)
}
