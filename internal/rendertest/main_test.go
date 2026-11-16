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

// Package rendertest drives the renderer through its public API with no window system, as one of
// its own would: with Metal on macOS, DirectX on Windows (GGFX_DIRECTX picks WARP or 12), and
// OpenGL in Mesa's surfaceless EGL context on Linux. Most of its tests are Metal's; the remote
// driver's replay runs on all three.
package rendertest_test

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/ironpark/ggfx/host"
)

func init() {
	// Init must be called on the main thread.
	runtime.LockOSThread()
}

func TestMain(m *testing.M) {
	// The child process of TestRemoteReplayMatchesNative is an app of the remote driver's, which
	// must not initialize the platform's driver: it records instead of running the tests.
	if path := os.Getenv(recordEnv); path != "" {
		if err := recordRemote(path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if err := initDriver(); err != nil {
		if skipNoDriver {
			fmt.Fprintf(os.Stderr, "rendertest: skipped: no driver to draw with here: %v\n", err)
			os.Exit(0)
		}
		panic(err)
	}
	code := m.Run()
	host.Terminate()
	os.Exit(code)
}

// frame runs f in a frame and flushes it, as a window system's loop does.
func frame(t *testing.T, f func()) {
	t.Helper()
	fr, err := host.BeginFrame()
	if err != nil {
		t.Fatal(err)
	}
	f()
	if err := fr.End(); err != nil {
		t.Fatal(err)
	}
	if _, err := host.Flush(false); err != nil {
		t.Fatal(err)
	}
}
