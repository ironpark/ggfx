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

// Package scratch provides helpers for reusable temporary buffers.
package scratch

// Resize returns a slice of length n backed by s's array when it is big enough.
// Otherwise it allocates a new slice without copying s's contents,
// so it is only for buffers whose contents need not be preserved.
func Resize[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]T, n)
}
