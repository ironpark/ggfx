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

package render

import (
	"math"
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
)

func TestMipmapLevelFromDistance(t *testing.T) {
	testCases := []struct {
		name string
		dx0  float32
		dy0  float32
		dx1  float32
		dy1  float32
		sx0  float32
		sy0  float32
		sx1  float32
		sy1  float32
		want int
	}{
		{
			name: "same size",
			dx0:  0, dy0: 0, dx1: 100, dy1: 100,
			sx0: 0, sy0: 0, sx1: 100, sy1: 100,
			want: 0,
		},
		{
			name: "scale 0.5",
			dx0:  0, dy0: 0, dx1: 50, dy1: 50,
			sx0: 0, sy0: 0, sx1: 100, sy1: 100,
			want: 0,
		},
		{
			name: "scale 0.01",
			dx0:  0, dy0: 0, dx1: 10, dy1: 10,
			sx0: 0, sy0: 0, sx1: 100, sy1: 100,
			want: 3,
		},
		{
			name: "extremely small scale",
			dx0:  0, dy0: 0, dx1: 1e-7, dy1: 1e-7,
			sx0: 0, sy0: 0, sx1: 4096, sy1: 4096,
			want: 6,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := mipmapLevelFromDistance(tc.dx0, tc.dy0, tc.dx1, tc.dy1, tc.sx0, tc.sy0, tc.sx1, tc.sy1)
			if got != tc.want {
				t.Errorf("mipmapLevelFromDistance: got: %d, want: %d", got, tc.want)
			}
		})
	}
}

func TestQuadUsesMipmap(t *testing.T) {
	cos45 := float32(math.Cos(math.Pi / 4))
	sin45 := float32(math.Sin(math.Pi / 4))
	testCases := []struct {
		name       string
		w, h       float32
		a, b, c, d float32
		tx, ty     float32
		want       bool
	}{
		{"identity", 64, 64, 1, 0, 0, 1, 0, 0, false},
		{"scale 0.9", 64, 64, 0.9, 0, 0, 0.9, 0, 0, false},
		{"scale 0.6", 64, 64, 0.6, 0, 0, 0.6, 0, 0, false},
		{"scale 0.5 no snap shrink", 64, 64, 0.5, 0, 0, 0.5, 0, 0, false},
		{"3x3 scale 0.5 translate 0.25 snaps to level 1", 3, 3, 0.5, 0, 0, 0.5, 0.25, 0.25, true},
		{"16x16 scale 0.49 snaps up to 8px (level 0)", 16, 16, 0.49, 0, 0, 0.49, 0, 0, false},
		{"scale 0.49", 64, 64, 0.49, 0, 0, 0.49, 0, 0, true},
		{"scale 0.25", 64, 64, 0.25, 0, 0, 0.25, 0, 0, true},
		{"non-uniform 0.4x1.0", 64, 64, 0.4, 0, 0, 1.0, 0, 0, false},
		{"non-uniform 1.0x0.4", 64, 64, 1.0, 0, 0, 0.4, 0, 0, false},
		{"non-uniform 0.4x0.45", 64, 64, 0.4, 0, 0, 0.45, 0, 0, true},
		{"rotated 45 deg at 0.6", 64, 64, 0.6 * cos45, -0.6 * sin45, 0.6 * sin45, 0.6 * cos45, 0, 0, false},
		{"rotated 45 deg at 0.49", 64, 64, 0.49 * cos45, -0.49 * sin45, 0.49 * sin45, 0.49 * cos45, 0, 0, true},
		{"reflected at 0.6", 64, 64, -0.6, 0, 0, 0.6, 64, 0, false},
		{"reflected at 0.49", 64, 64, -0.49, 0, 0, 0.49, 64, 0, true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var vs [4 * graphics.VertexFloatCount]float32
			graphics.QuadVerticesFromSrcAndMatrix(vs[:], 0, 0, tc.w, tc.h, tc.a, tc.b, tc.c, tc.d, tc.tx, tc.ty, 1, 1, 1, 1)
			if got := QuadUsesMipmap(vs[:]); got != tc.want {
				t.Errorf("QuadUsesMipmap(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
