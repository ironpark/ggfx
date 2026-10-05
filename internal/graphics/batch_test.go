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
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

// batchCounts covers the counts around the SIMD threshold and the SIMD block sizes.
var batchCounts = []int{0, 1, 2, 3, 7, 8, 9, 15, 16, 17, 23, 24, 31, 32, 33, 100, 1000, 1027}

// randomFloat32 returns a float32 that is sometimes a special value, so that the SIMD and the Go
// implementations are compared on signed zeros, infinities, and NaNs too.
func randomFloat32(r *rand.Rand) float32 {
	switch r.IntN(16) {
	case 0:
		return 0
	case 1:
		return negZero
	case 2:
		return float32(math.Inf(1))
	case 3:
		return float32(math.NaN())
	case 4:
		return math.Float32frombits(r.Uint32())
	}
	return (r.Float32() - 0.5) * 2048
}

func randomFloat32s(r *rand.Rand, n int) []float32 {
	s := make([]float32, n)
	for i := range s {
		s[i] = randomFloat32(r)
	}
	return s
}

func sameFloat32Bits(a, b []float32) bool {
	return slices.EqualFunc(a, b, func(x, y float32) bool {
		if math.IsNaN(float64(x)) && math.IsNaN(float64(y)) {
			// NaN payloads can differ between the implementations.
			return true
		}
		return math.Float32bits(x) == math.Float32bits(y)
	})
}

func TestConvertVertices(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	offsets := [][4]float32{
		{0, 0, 0, 0},
		{negZero, negZero, negZero, negZero},
		{-3, 5, 0.5, -1024},
	}
	for _, n := range batchCounts {
		for _, flags := range []ConvertVerticesFlags{0, ConvertVerticesPremultiplyAlpha, ConvertVerticesCopyCustom, ConvertVerticesPremultiplyAlpha | ConvertVerticesCopyCustom} {
			for _, o := range offsets {
				t.Run(fmt.Sprintf("n=%d/flags=%d/offset=%v", n, flags, o), func(t *testing.T) {
					src := randomFloat32s(r, n*VertexFloatCount)
					init := randomFloat32s(r, n*VertexFloatCount+1)

					got := slices.Clone(init)
					ConvertVertices(got, src, o[0], o[1], o[2], o[3], flags)
					want := slices.Clone(init)
					convertVerticesGeneric(want, src, o[0], o[1], o[2], o[3], flags)
					if !sameFloat32Bits(got, want) {
						t.Errorf("got: %v, want: %v", got, want)
					}
				})
			}
		}
	}
}

func TestTranslateVertices(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for _, n := range batchCounts {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			vs := randomFloat32s(r, n*VertexFloatCount)
			o := randomFloat32s(r, 4)

			got := slices.Clone(vs)
			TranslateVertices(got, o[0], o[1], o[2], o[3])
			want := slices.Clone(vs)
			translateVerticesGeneric(want, o[0], o[1], o[2], o[3])
			if !sameFloat32Bits(got, want) {
				t.Errorf("got: %v, want: %v", got, want)
			}
		})
	}
}

func TestWidenIndices(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for _, n := range batchCounts {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			src := make([]uint16, n)
			for i := range src {
				src[i] = uint16(r.Uint32())
			}
			// A sentinel after the last element must be kept.
			got := make([]uint32, n+1)
			got[n] = 0xdeadbeef
			WidenIndices(got, src)
			want := make([]uint32, n+1)
			want[n] = 0xdeadbeef
			widenIndicesGeneric(want, src)
			if !slices.Equal(got, want) {
				t.Errorf("got: %v, want: %v", got, want)
			}
		})
	}
}

func TestOffsetIndices(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for _, n := range batchCounts {
		for _, offset := range []uint32{0, 1, 1 << 16, math.MaxUint32} {
			t.Run(fmt.Sprintf("n=%d/offset=%d", n, offset), func(t *testing.T) {
				is := make([]uint32, n+1)
				for i := range is {
					is[i] = r.Uint32()
				}
				got := slices.Clone(is)
				OffsetIndices(got[:n], offset)
				want := slices.Clone(is)
				offsetIndicesGeneric(want[:n], offset)
				if !slices.Equal(got, want) {
					t.Errorf("got: %v, want: %v", got, want)
				}
			})
		}
	}
}

func TestMaxIndex(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	for _, n := range batchCounts {
		// Put the maximum at each position, including the tail that SIMD does not cover.
		for pos := range max(n, 1) {
			is := make([]uint32, n)
			for i := range is {
				is[i] = r.Uint32N(1 << 20)
			}
			if n > 0 {
				is[pos] = math.MaxUint32 - uint32(r.IntN(2))
			}
			if got, want := MaxIndex(is), maxIndexGeneric(is); got != want {
				t.Errorf("n=%d, pos=%d: got: %d, want: %d", n, pos, got, want)
			}
		}
	}
}

func BenchmarkConvertVertices(b *testing.B) {
	for _, n := range []int{4, 16, 64, 4096} {
		for _, simd := range []bool{false, true} {
			b.Run(fmt.Sprintf("n=%d/simd=%t", n, simd), func(b *testing.B) {
				src := make([]float32, n*VertexFloatCount)
				dst := make([]float32, n*VertexFloatCount)
				b.SetBytes(int64(4 * len(src)))
				for b.Loop() {
					if simd {
						ConvertVertices(dst, src, 1, 2, 3, 4, ConvertVerticesPremultiplyAlpha)
					} else {
						convertVerticesGeneric(dst, src, 1, 2, 3, 4, ConvertVerticesPremultiplyAlpha)
					}
				}
			})
		}
	}
}

func BenchmarkTranslateVertices(b *testing.B) {
	for _, n := range []int{4, 16, 64, 4096} {
		for _, simd := range []bool{false, true} {
			b.Run(fmt.Sprintf("n=%d/simd=%t", n, simd), func(b *testing.B) {
				vs := make([]float32, n*VertexFloatCount)
				b.SetBytes(int64(4 * len(vs)))
				for b.Loop() {
					if simd {
						TranslateVertices(vs, 1, 2, 3, 4)
					} else {
						translateVerticesGeneric(vs, 1, 2, 3, 4)
					}
				}
			})
		}
	}
}

func BenchmarkWidenIndices(b *testing.B) {
	for _, n := range []int{6, 24, 96, 6144} {
		for _, simd := range []bool{false, true} {
			b.Run(fmt.Sprintf("n=%d/simd=%t", n, simd), func(b *testing.B) {
				src := make([]uint16, n)
				dst := make([]uint32, n)
				b.SetBytes(int64(2 * n))
				for b.Loop() {
					if simd {
						WidenIndices(dst, src)
					} else {
						widenIndicesGeneric(dst, src)
					}
				}
			})
		}
	}
}

func BenchmarkOffsetIndices(b *testing.B) {
	for _, n := range []int{6, 24, 96, 6144} {
		for _, simd := range []bool{false, true} {
			b.Run(fmt.Sprintf("n=%d/simd=%t", n, simd), func(b *testing.B) {
				is := make([]uint32, n)
				b.SetBytes(int64(4 * n))
				for b.Loop() {
					if simd {
						OffsetIndices(is, 1)
					} else {
						offsetIndicesGeneric(is, 1)
					}
				}
			})
		}
	}
}

func BenchmarkMaxIndex(b *testing.B) {
	for _, n := range []int{6, 24, 96, 6144} {
		for _, simd := range []bool{false, true} {
			b.Run(fmt.Sprintf("n=%d/simd=%t", n, simd), func(b *testing.B) {
				is := make([]uint32, n)
				b.SetBytes(int64(4 * n))
				for b.Loop() {
					if simd {
						MaxIndex(is)
					} else {
						maxIndexGeneric(is)
					}
				}
			})
		}
	}
}
