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

//go:build !android

package rendertest_test

import (
	"github.com/ironpark/ggfx/host"
	"github.com/ironpark/ggfx/internal/graphicsdriver/opengl"
	"github.com/ironpark/ggfx/internal/graphicsdriver/zigdriver"
	etesting "github.com/ironpark/ggfx/internal/testing"
)

// OpenGL draws in Mesa's surfaceless EGL context, which a system without Mesa lacks.
const skipNoDriver = true

func initDriver() error {
	if err := host.Init(nil); err != nil {
		return err
	}
	// Only OpenGL draws through a GL context.
	if host.CurrentDriver().Library != host.GraphicsLibraryOpenGL {
		return nil
	}
	c, err := etesting.NewGLContext()
	if err != nil {
		host.Terminate()
		return err
	}
	host.SetGLContext(c)
	return nil
}

// gpuDriverUsed returns the name of a platform driver the process made a device of, if any.
func gpuDriverUsed() string {
	switch {
	case zigdriver.DeviceCreated():
		return "Zig"
	case opengl.Created():
		return "OpenGL"
	}
	return ""
}
