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

package recent_test

import (
	"math"
	"testing"

	"github.com/ironpark/ggfx/internal/recent"
)

func TestCapacity(t *testing.T) {
	const minimum = 1024
	var p recent.Peak

	// A buffer grows to the power of two holding the use.
	c := p.Capacity(0, 100_000, minimum)
	if c != 131072 {
		t.Fatalf("growing: got %d, want 131072", c)
	}
	// Small uses keep it while the large one is in the window.
	for i := range recent.Window {
		recent.Advance()
		if c = p.Capacity(c, 500, minimum); c != 131072 {
			t.Fatalf("frame %d: got %d, want the capacity kept", i, c)
		}
	}
	// Once the large use is out of the window, it shrinks to the minimum's power of two.
	for range 2 * recent.Window {
		recent.Advance()
		c = p.Capacity(c, 500, minimum)
	}
	if c != minimum {
		t.Errorf("after the window: got %d, want %d", c, minimum)
	}
	// Uses alternating within the window never reallocate.
	var q recent.Peak
	c = q.Capacity(0, 3000, minimum)
	for i := range 10 * recent.Window {
		recent.Advance()
		n := 3000
		if i%2 == 0 {
			n = 100
		}
		if got := q.Capacity(c, n, minimum); got != c {
			t.Fatalf("frame %d: got %d, want %d kept", i, got, c)
		}
	}
}

// Uses age by frames, not by how often they are recorded: many small uses in the frame after a
// large one do not age the large one out.
func TestCapacityAgesByFrames(t *testing.T) {
	const minimum = 1024
	var p recent.Peak
	recent.Advance()
	c := p.Capacity(0, 100_000, minimum)
	recent.Advance()
	for i := range 10 * recent.Window {
		if c = p.Capacity(c, 500, minimum); c != 131072 {
			t.Fatalf("use %d in one frame: got %d, want the capacity kept", i, c)
		}
	}
}

func TestNextPowerOf2(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{-1, 1}, {0, 1}, {1, 1}, {2, 2}, {3, 4}, {4, 4}, {5, 8}, {1000, 1024}, {1 << 20, 1 << 20}, {1<<20 + 1, 1 << 21},
		// No power of two of int holds it: x itself.
		{math.MaxInt, math.MaxInt},
	} {
		if got := recent.NextPowerOf2(tc.in); got != tc.want {
			t.Errorf("NextPowerOf2(%d): got %d, want %d", tc.in, got, tc.want)
		}
	}
	if got, want := recent.NextPowerOf2(uintptr(math.MaxUint)), uintptr(math.MaxUint); got != want {
		t.Errorf("NextPowerOf2(uintptr(MaxUint)): got %d, want %d", got, want)
	}
	if got, want := recent.NextPowerOf2(uintptr(1<<40+1)), uintptr(1<<41); got != want {
		t.Errorf("NextPowerOf2(uintptr(1<<40+1)): got %d, want %d", got, want)
	}
}
