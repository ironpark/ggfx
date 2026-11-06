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
		if c = p.Capacity(c, 500, minimum); c != 131072 {
			t.Fatalf("tick %d: got %d, want the capacity kept", i, c)
		}
	}
	// Once the large use is out of the window, it shrinks to the minimum's power of two.
	for range 2 * recent.Window {
		c = p.Capacity(c, 500, minimum)
	}
	if c != minimum {
		t.Errorf("after the window: got %d, want %d", c, minimum)
	}
	// Uses alternating within the window never reallocate.
	var q recent.Peak
	c = q.Capacity(0, 3000, minimum)
	for i := range 10 * recent.Window {
		n := 3000
		if i%2 == 0 {
			n = 100
		}
		if got := q.Capacity(c, n, minimum); got != c {
			t.Fatalf("tick %d: got %d, want %d kept", i, got, c)
		}
	}
}
