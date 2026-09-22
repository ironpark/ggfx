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
	stdcontext "context"
	"runtime"

	"golang.org/x/sync/errgroup"

	"github.com/ironpark/ggfx/internal/graphicscommand"
	"github.com/ironpark/ggfx/internal/thread"
)

func (u *UserInterface) Run(game Game, options *RunOptions) error {
	u.context = newContext(game, options.ScreenTransparent)
	return u.runLoop(options, nil)
}

// runLoop runs the loop. start, if any, runs on the loop's goroutine after the initialization and
// before the first iteration.
func (u *UserInterface) runLoop(options *RunOptions, start func() error) error {
	if options.SingleThread || buildTagSingleThread || runtime.GOOS == "js" {
		return u.runSingleThread(options, start)
	}
	return u.runMultiThread(options, start)
}

func (u *UserInterface) runMultiThread(options *RunOptions, start func() error) error {
	u.mainThread = thread.NewOSThread()
	graphicscommand.SetOSThreadAsRenderThread()

	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	var wg errgroup.Group

	// Run the render thread.
	wg.Go(func() error {
		defer cancel()

		graphicscommand.LoopRenderThread(ctx)
		return nil
	})

	// Run the game thread.
	wg.Go(func() error {
		defer cancel()

		var err error
		u.mainThread.Call(func() {
			if mainErr := u.initOnMainThread(options); mainErr != nil {
				err = mainErr
			}
		})
		if err != nil {
			return err
		}

		// setRunning(true) should be called in initOnMainThread for each platform.
		defer u.setRunning(false)

		if start != nil {
			if err := start(); err != nil {
				return err
			}
		}

		return u.loopGame()
	})

	// Run the main thread. The loop is the thread's whole life, so a call arriving after
	// it ends is a no-op rather than a block forever.
	_ = u.mainThread.LoopAndStop(ctx)
	return wg.Wait()
}

func (u *UserInterface) runSingleThread(options *RunOptions, start func() error) error {
	// Initialize the main thread first so the thread is available at u.run (#809).
	u.mainThread = thread.NewNoopThread()

	u.setRunning(true)
	defer u.setRunning(false)

	if err := u.initOnMainThread(options); err != nil {
		return err
	}

	if start != nil {
		if err := start(); err != nil {
			return err
		}
	}

	if err := u.loopGame(); err != nil {
		return err
	}

	return nil
}
