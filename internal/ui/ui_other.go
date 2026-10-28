// Copyright 2023 The Ebitengine Authors
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

//go:build js

package ui

import (
	"github.com/ironpark/ggfx/internal/render"
	"github.com/ironpark/ggfx/internal/thread"
)

// runLoop runs the loop on the calling goroutine, the only one the browser has. start, if any,
// runs after the initialization and before the first iteration.
func (u *UserInterface) runLoop(options *RunOptions, start func() error) error {
	return u.runSingleThread(options, start)
}

func (u *UserInterface) runSingleThread(options *RunOptions, start func() error) error {
	// Initialize the main thread first so the thread is available at u.run (#809).
	u.mainThread = thread.NewNoopThread()

	u.setRunning(true)
	defer u.setRunning(false)
	defer render.Terminate()

	if err := u.initOnMainThread(options); err != nil {
		return err
	}

	if start != nil {
		if err := start(); err != nil {
			return err
		}
	}

	if err := u.loopFrames(); err != nil {
		return err
	}

	return nil
}
