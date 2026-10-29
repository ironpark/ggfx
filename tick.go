// Copyright 2014 Hajime Hoshi
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

package ggfx

import "github.com/ironpark/ggfx/internal/render"

// Tick returns the current tick count.
// The tick count starts with 0 and is incremented by one on every iteration of a window system's
// event loop, through ggfx/host's AdvanceTick.
//
// Tick is concurrent-safe.
func Tick() int64 {
	return render.Tick()
}
