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

package remote_test

import (
	"image/color"
	"testing"

	"github.com/ironpark/ggfx/host"
)

// TestNoGPUDevice checks that an app drawing through the remote driver never makes a device or
// context of the platform's drivers, though they are linked into it: it opens no GPU, and would
// run on a machine without one.
func TestNoGPUDevice(t *testing.T) {
	frame(t, true, func(f host.Frame) {
		f.Screen(surface, 16, 16).Fill(color.White)
	})
	if d := gpuDriverUsed(); d != "" {
		t.Errorf("the process made a %s device", d)
	}
}
