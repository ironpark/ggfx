// Copyright 2026 The Ebitengine Authors
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

import "fmt"

// Conversions between 1-byte alpha pixels and 4-byte premultiplied RGBA pixels. An alpha pixel a is
// the RGBA pixel (a, a, a, a): premultiplied white of alpha a.

// ExpandAlpha writes the RGBA pixels of the alpha pixels src into dst, len(dst) = 4 * len(src).
func ExpandAlpha(dst, src []byte) {
	if len(dst) != 4*len(src) {
		panic(fmt.Sprintf("graphics: len(dst) must be %d but %d at ExpandAlpha", 4*len(src), len(dst)))
	}
	for i, a := range src {
		d := dst[4*i : 4*i+4 : 4*i+4]
		d[0], d[1], d[2], d[3] = a, a, a, a
	}
}

// ExtractChannel writes channel c (0 for red, 3 for alpha) of the RGBA pixels src into dst,
// len(src) = 4 * len(dst).
//
// The value an alpha image keeps of an RGBA pixel depends on where it comes from: a draw into one
// keeps the red channel, which the GPU writes, and pixels a caller gives keep their alpha.
func ExtractChannel(dst, src []byte, c int) {
	if len(src) != 4*len(dst) {
		panic(fmt.Sprintf("graphics: len(src) must be %d but %d at ExtractChannel", 4*len(dst), len(src)))
	}
	for i := range dst {
		dst[i] = src[4*i+c]
	}
}
