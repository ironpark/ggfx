// Copyright 2026 The Ebiten Authors
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

// Package bridge carries to ggfx/host what it needs of ggfx that ggfx does not export.
package bridge

import "github.com/ironpark/ggfx/internal/render"

// ScreenImage returns the *ggfx.Image of a surface's screen img: cur, a *ggfx.Image or nil, when
// it already is img's, and a new one when the surface replaced its screen. ggfx sets it.
var ScreenImage func(cur any, img *render.Image) any
