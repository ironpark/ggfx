// Copyright 2026 The Ebitengine Authors
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

package render_test

import (
	"testing"

	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/render"
)

type frameDriver struct {
	graphicsdriver.Graphics
	images   int
	frames   int
	presents int
}

func (*frameDriver) Begin() error {
	return nil
}

func (g *frameDriver) NewImage(int, int) (graphicsdriver.Image, error) {
	g.images++
	return nil, nil
}

func (g *frameDriver) End(mode graphicsdriver.FlushMode) error {
	if mode != graphicsdriver.FlushModeIntermediate {
		g.frames++
	}
	if mode == graphicsdriver.FlushModePresent {
		g.presents++
	}
	return nil
}

func TestSkippedPresentationFlushesCommands(t *testing.T) {
	var driver frameDriver
	const frames = 10
	for frame := range frames {
		graphicscommand.NewImage(1, 1, nil, "")
		if err := render.FlushForTesting(&driver, false); err != nil {
			t.Fatal(err)
		}
		if driver.images != frame+1 || driver.frames != frame+1 || driver.presents != 0 {
			t.Errorf("frame %d without presentation: images = %d, frames = %d, presents = %d; want %d, %d, 0", frame, driver.images, driver.frames, driver.presents, frame+1, frame+1)
		}
	}
	graphicscommand.NewImage(1, 1, nil, "")
	if err := render.FlushForTesting(&driver, true); err != nil {
		t.Fatal(err)
	}
	if driver.images != frames+1 || driver.frames != frames+1 || driver.presents != 1 {
		t.Errorf("after presentation resumes: images = %d, frames = %d, presents = %d; want %d, %d, 1", driver.images, driver.frames, driver.presents, frames+1, frames+1)
	}
}
