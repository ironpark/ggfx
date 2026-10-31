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

// InternalImageSize returns a nearest appropriate size as an internal image.
//
// Where every GPU ggfx draws with takes a texture of any size, a desktop's,
// that is the size rounded up to a multiple of textureGrain: a texture the
// size of a 5046×2458 window takes 50 MB rather than the 128 MB a power of
// two, 8192×4096, did. ggfx clamps and builds its own mipmaps, so it never
// asks for what a size other than a power of two is denied on the GPUs
// that deny it; the atlas's pages are powers of two by their own making.
// Elsewhere (see math_pot.go) it is the next power of two, as it was.
func InternalImageSize(x int) int {
	// minInternalImageSize is the minimum size of internal images (texture/framebuffer).
	//
	// For example, the image size less than 15 is not supported on some iOS devices.
	// See also: https://stackoverflow.com/questions/15935651
	const minInternalImageSize = 16

	if x <= 0 {
		panic("graphics: x must be positive")
	}
	if x < minInternalImageSize {
		return minInternalImageSize
	}
	if !powerOfTwoTextures {
		return (x + textureGrain - 1) / textureGrain * textureGrain
	}
	r := 1
	for r < x {
		r <<= 1
	}
	return r
}

// textureGrain is what a texture's sides are rounded up to where any size
// will do.
const textureGrain = 16
