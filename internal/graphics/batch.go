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

package graphics

import (
	"math"
)

// The functions in this file process vertex and index batches, which grow with what an
// application draws in a frame. They have SIMD implementations on some architectures; a batch
// shorter than simdMinCount stays in Go, where a call into assembly costs more than it saves.
const simdMinCount = 16

// negZero is -0, which is the identity of float32 addition: x + -0 is x for every x including -0,
// while x + 0 turns -0 into 0.
var negZero = float32(math.Copysign(0, -1))

// ConvertVerticesFlags specifies what ConvertVertices does besides translating positions.
type ConvertVerticesFlags int

const (
	// ConvertVerticesPremultiplyAlpha multiplies the RGB components of colors by their alpha.
	ConvertVerticesPremultiplyAlpha ConvertVerticesFlags = 1 << iota

	// ConvertVerticesCopyCustom copies the custom values. Without it, they are left as they are in dst.
	ConvertVerticesCopyCustom
)

// ConvertVertices converts vertices in src into dst, both of which are laid out in
// VertexFloatCount floats per vertex: a destination position, a source position, a color, and
// custom values.
//
// The destination position is translated by (dx, dy) and the source position is translated by
// (sx, sy). Pass -0 instead of 0 to leave a position untouched bit for bit.
//
// len(dst) must be equal to or greater than len(src).
func ConvertVertices(dst, src []float32, dx, dy, sx, sy float32, flags ConvertVerticesFlags) {
	if len(src)%VertexFloatCount != 0 {
		panic("graphics: len(src) must be a multiple of VertexFloatCount")
	}
	dst = dst[:len(src)]
	if n := len(src) / VertexFloatCount; useSIMD && n >= simdMinCount {
		offset := [4]float32{dx, dy, sx, sy}
		convertVerticesSIMD(&dst[0], &src[0], n, &offset, flags)
		return
	}
	convertVerticesGeneric(dst, src, dx, dy, sx, sy, flags)
}

func convertVerticesGeneric(dst, src []float32, dx, dy, sx, sy float32, flags ConvertVerticesFlags) {
	for i := 0; i < len(src); i += VertexFloatCount {
		// Create temporary slices to reduce boundary checks.
		d := dst[i : i+VertexFloatCount]
		s := src[i : i+VertexFloatCount]
		d[0] = s[0] + dx
		d[1] = s[1] + dy
		d[2] = s[2] + sx
		d[3] = s[3] + sy
		if flags&ConvertVerticesPremultiplyAlpha != 0 {
			d[4] = s[4] * s[7]
			d[5] = s[5] * s[7]
			d[6] = s[6] * s[7]
		} else {
			d[4] = s[4]
			d[5] = s[5]
			d[6] = s[6]
		}
		d[7] = s[7]
		if flags&ConvertVerticesCopyCustom != 0 {
			d[8] = s[8]
			d[9] = s[9]
			d[10] = s[10]
			d[11] = s[11]
		}
	}
}

// TranslateVertices translates the destination positions of vertices by (dx, dy) and the source
// positions by (sx, sy) in place. Pass -0 instead of 0 to leave a position untouched bit for bit.
func TranslateVertices(vertices []float32, dx, dy, sx, sy float32) {
	if len(vertices)%VertexFloatCount != 0 {
		panic("graphics: len(vertices) must be a multiple of VertexFloatCount")
	}
	if n := len(vertices) / VertexFloatCount; useSIMD && n >= simdMinCount {
		offset := [4]float32{dx, dy, sx, sy}
		translateVerticesSIMD(&vertices[0], n, &offset)
		return
	}
	translateVerticesGeneric(vertices, dx, dy, sx, sy)
}

func translateVerticesGeneric(vertices []float32, dx, dy, sx, sy float32) {
	for i := 0; i < len(vertices); i += VertexFloatCount {
		v := vertices[i : i+4]
		v[0] += dx
		v[1] += dy
		v[2] += sx
		v[3] += sy
	}
}

// WidenIndices converts uint16 indices in src into uint32 indices in dst.
//
// len(dst) must be equal to or greater than len(src).
func WidenIndices(dst []uint32, src []uint16) {
	dst = dst[:len(src)]
	if useSIMD && len(src) >= simdMinCount {
		widenIndicesSIMD(&dst[0], &src[0], len(src))
		return
	}
	widenIndicesGeneric(dst, src)
}

func widenIndicesGeneric(dst []uint32, src []uint16) {
	dst = dst[:len(src)]
	for i, idx := range src {
		dst[i] = uint32(idx)
	}
}

// OffsetIndices adds offset to every index in place.
func OffsetIndices(indices []uint32, offset uint32) {
	if offset == 0 {
		return
	}
	if useSIMD && len(indices) >= simdMinCount {
		offsetIndicesSIMD(&indices[0], len(indices), offset)
		return
	}
	offsetIndicesGeneric(indices, offset)
}

func offsetIndicesGeneric(indices []uint32, offset uint32) {
	for i := range indices {
		indices[i] += offset
	}
}

// MaxIndex returns the maximum of indices, or 0 when indices is empty.
func MaxIndex(indices []uint32) uint32 {
	if useSIMD && len(indices) >= simdMinCount {
		return maxIndexSIMD(&indices[0], len(indices))
	}
	return maxIndexGeneric(indices)
}

func maxIndexGeneric(indices []uint32) uint32 {
	var m uint32
	for _, idx := range indices {
		m = max(m, idx)
	}
	return m
}
