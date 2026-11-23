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

package drivertest

import "testing"

func TestAllocVersionsAreDistinctAndNonzero(t *testing.T) {
	v1 := allocVersions()
	v2 := allocVersions()

	seen := map[uint64]bool{}
	for _, v := range []uint64{v1.Internal, v1.User, v2.Internal, v2.User} {
		if v == 0 {
			t.Fatalf("version must not be 0: v1=%+v v2=%+v", v1, v2)
		}
		if seen[v] {
			t.Fatalf("version %d was given twice: v1=%+v v2=%+v", v, v1, v2)
		}
		seen[v] = true
	}
}
