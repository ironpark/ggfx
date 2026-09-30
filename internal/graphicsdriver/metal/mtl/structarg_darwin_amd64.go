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

// On amd64, Viewport and ScissorRect are passed on the stack, which msgSend cannot do, so they
// go through objc.ID.Send.

// SetViewport sets the viewport used for transformations and clipping.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515527-setviewport?language=objc.
func (rce RenderCommandEncoder) SetViewport(viewport Viewport) {
	rce.commandEncoder.Send(sel_setViewport, viewport)
}

// SetScissorRect sets the scissor rectangle for a fragment scissor test.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515583-setscissorrect?language=objc.
func (rce RenderCommandEncoder) SetScissorRect(scissorRect ScissorRect) {
	rce.commandEncoder.Send(sel_setScissorRect, scissorRect)
}
