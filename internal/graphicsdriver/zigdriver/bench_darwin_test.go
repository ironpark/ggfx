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

//go:build darwin && !ios && (amd64 || arm64)

package zigdriver

import (
	"testing"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/metal"
)

func newGoBenchDriver(b *testing.B) (graphicsdriver.Graphics, bool) {
	b.Helper()
	g, err := metal.NewGraphics(color.ColorSpaceSRGB)
	if err != nil {
		return nil, false
	}
	if err := g.Initialize(); err != nil {
		return nil, false
	}
	return g, true
}
