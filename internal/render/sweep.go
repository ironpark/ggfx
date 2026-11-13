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

package render

import (
	"sync"
	"sync/atomic"
)

// The frame sweepers give back what a cache kept for a while unused. A cache that ages only when it
// is used would otherwise keep its memory for as long as the app stops using it, which may be
// forever; Flush runs the sweepers at the end of every frame, used or not.

var (
	sweepersM sync.Mutex
	// sweepers is replaced, never changed in place, so that Flush reads it without a lock.
	sweepers atomic.Pointer[[]func(now int64)]
)

func init() {
	AddFrameSweeper(dropUnusedMipmapLevels)
}

// AddFrameSweeper makes fn run at the end of every frame, with the frame clock's new time. It runs
// on the goroutine calling Flush, outside a frame, with no lock of render or of any image held; an
// image it deallocates is deallocated at the next BeginFrame. fn is for the life of the program.
func AddFrameSweeper(fn func(now int64)) {
	sweepersM.Lock()
	defer sweepersM.Unlock()
	var fns []func(int64)
	if p := sweepers.Load(); p != nil {
		fns = append(fns, *p...)
	}
	fns = append(fns, fn)
	sweepers.Store(&fns)
}

// sweep runs the frame sweepers.
func sweep(now int64) {
	p := sweepers.Load()
	if p == nil {
		return
	}
	for _, fn := range *p {
		fn(now)
	}
}
