// Copyright 2020 The Ebiten Authors
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

package testing

import (
	"os"
	"testing"

	"github.com/ironpark/ggfx"
)

// window is the window MainWithRunLoop opens, so that a test can ask for its
// size instead of the removed root-level ScreenSize.
var window *ggfx.Window

// Window returns the window the test run loop opened, or nil outside it.
func Window() *ggfx.Window {
	return window
}

// MainWithRunLoop runs m inside a ggfx event loop, which is what makes
// (*Image).At and the rest of the graphics API available to a test. The tests
// run in the window's first frame, between the atlas's begin and end of frame.
func MainWithRunLoop(m *testing.M) {
	code := 1
	err := ggfx.Run(ggfx.HandlerFunc(func(ev ggfx.Event) error {
		switch ev.(type) {
		case ggfx.StartEvent:
			w, err := ggfx.NewWindow(&ggfx.WindowOptions{
				Title:  "ggfx tests",
				Width:  320,
				Height: 240,
			})
			if err != nil {
				return err
			}
			window = w
		case ggfx.FrameEvent:
			code = m.Run()
			return ggfx.Termination
		}
		return nil
	}), nil)
	if err != nil {
		panic(err)
	}
	if code != 0 {
		os.Exit(code)
	}
}
