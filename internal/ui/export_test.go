// Copyright 2025 The Ebitengine Authors
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

package ui

import (
	"time"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

func (i *InputState) SetKeyPressed(key Key, t InputTime) {
	i.setKeyPressed(key, t)
}

func (i *InputState) SetKeyReleased(key Key, t InputTime) {
	i.setKeyReleased(key, t)
}

func (i *InputState) SetMouseButtonPressed(button MouseButton, t InputTime) {
	i.setMouseButtonPressed(button, t)
}

func (i *InputState) SetMouseButtonReleased(button MouseButton, t InputTime) {
	i.setMouseButtonReleased(button, t)
}

// VsyncIgnoredForTest reports whether the given successive frame times, measured on a display with
// the given refresh interval, make the loop pace itself instead of relying on the vsync.
func VsyncIgnoredForTest(frameTimes []time.Duration, refreshInterval time.Duration) bool {
	var c framePacer
	var ignored bool
	for _, frameTime := range frameTimes {
		ignored = c.updateVsyncIgnored(frameTime, refreshInterval)
	}
	return ignored
}

func FlushCommandsAndWaitForTesting(driver graphicsdriver.Graphics, present bool) error {
	var c framePacer
	return c.flushCommandsAndWait(present, driver, false, 60)
}
