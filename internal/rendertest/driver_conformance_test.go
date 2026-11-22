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

//go:build (darwin && !ios) || windows || (linux && !android)

package rendertest_test

import (
	"testing"

	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/graphicsdriver/drivertest"
	"github.com/ironpark/ggfx/internal/render"
)

func TestLiveDriverConformance(t *testing.T) {
	g := render.GraphicsDriver()
	if g == nil {
		t.Skip("no live graphics driver")
	}
	drivertest.RunSuite(t, g, drivertest.Options{
		Run: func(fn func(graphicsdriver.Graphics) error) error {
			var runErr error
			frame(t, func() {
				runErr = graphicscommand.RunExternal(g, fn)
			})
			return runErr
		},
		CheckDrawPixels: true,
	})
}
