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

package graphics_test

import (
	"testing"

	"github.com/ironpark/ggfx/internal/graphics"
)

func TestUnmanagedInternalImageSize(t *testing.T) {
	for _, tc := range []struct {
		x, maxSize, want int
	}{
		{1, 4096, 16},
		{15, 4096, 16},
		{16, 4096, 32},
		{31, 4096, 32},
		{32, 4096, 48},
		{4095, 4096, 4096},
		{4096, 4096, 4096},
		{16, 0, 32},
		{128, 128, 128},
	} {
		if got := graphics.UnmanagedInternalImageSize(tc.x, tc.maxSize); got != tc.want {
			t.Errorf("UnmanagedInternalImageSize(%d, %d) = %d, want %d", tc.x, tc.maxSize, got, tc.want)
		}
	}
}
