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

package opengl

import "sync/atomic"

var created atomic.Bool

// Created reports whether this process has asked for an OpenGL driver, loading the OpenGL library
// and taking a context to draw with, which a process that draws through the remote driver never
// does; its tests check that it stays so.
func Created() bool {
	return created.Load()
}
