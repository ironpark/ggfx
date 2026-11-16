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

package rendertest_test

import (
	"errors"

	"github.com/ironpark/ggfx/host"
	"github.com/ironpark/ggfx/internal/graphicsdriver/directx"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl"
)

// DirectX draws with no window, with WARP where there is no GPU; OpenGL would need a window for a
// context, so a run that asks for it skips.
const skipNoDriver = true

func initDriver() error {
	if err := host.Init(nil); err != nil {
		return err
	}
	if host.CurrentDriver().Library != host.GraphicsLibraryDirectX {
		host.Terminate()
		return errors.New("the driver is not DirectX")
	}
	return nil
}

// gpuDriverUsed returns the name of a platform driver the process made a device of, if any.
func gpuDriverUsed() string {
	switch {
	case directx.DeviceCreated():
		return "DirectX"
	case opengl.Created():
		return "OpenGL"
	}
	return ""
}
