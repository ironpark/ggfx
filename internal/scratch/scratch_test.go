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

package scratch_test

import (
	"testing"

	"github.com/ironpark/ggfx/internal/scratch"
)

func TestResize(t *testing.T) {
	s := make([]int, 2, 8)
	got := scratch.Resize(s, 6)
	if len(got) != 6 || &got[0] != &s[0] {
		t.Errorf("Resize within capacity must reuse the array")
	}
	got = scratch.Resize(s, 9)
	if len(got) != 9 || &got[0] == &s[0] {
		t.Errorf("Resize beyond capacity must allocate")
	}
}
