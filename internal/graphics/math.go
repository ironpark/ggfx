// Copyright 2014 Hajime Hoshi
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

package graphics

// InternalImageSize returns a nearest appropriate size as an internal image: x rounded up
// to a multiple of 16, the least a texture's side is on some iOS devices
// (https://stackoverflow.com/questions/15935651).
//
// It was the next power of two, for GPUs that take no other size, but every one ggfx draws
// with takes any: it asks for OpenGL ES 3 and WebGL 2 at least, and Metal and Direct3D always
// did. A texture the size of a 5046×2458 window now takes 50 MB rather than 128 MB at
// 8192×4096. The atlas's pages are powers of two by their own making.
func InternalImageSize(x int) int {
	const grain = 16
	if x <= 0 {
		panic("graphics: x must be positive")
	}
	return (x + grain - 1) / grain * grain
}
