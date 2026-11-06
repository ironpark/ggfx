// Copyright 2019 The Ebiten Authors
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

package atlas

import "github.com/ironpark/ggfx/internal/graphicsdriver"

const (
	BaseCountToPutOnSourceBackend = baseCountToPutOnSourceBackend
)

func PutImagesOnSourceBackendForTesting() {
	putImagesOnSourceBackend()
}

var (
	oldMinSourceSize      int
	oldMinDestinationSize int
	oldMaxSize            int
)

func SetImageSizeForTesting(minSource, minDestination, max int) {
	oldMinSourceSize = minSourceSize
	oldMinDestinationSize = minDestinationSize
	oldMaxSize = maxSize

	minSourceSize = minSource
	minDestinationSize = minDestination
	maxSize = max
}

func ResetImageSizeForTesting() {
	minSourceSize = oldMinSourceSize
	minDestinationSize = oldMinDestinationSize
	maxSize = oldMaxSize
}

func (i *Image) PaddingSizeForTesting() int {
	return i.paddingSize()
}

func (i *Image) BackendSizeForTesting() (int, int) {
	backendsM.Lock()
	defer backendsM.Unlock()
	if i.backend == nil {
		return 0, 0
	}
	return i.backend.width, i.backend.height
}

func (i *Image) IsOnSourceBackendForTesting() bool {
	backendsM.Lock()
	defer backendsM.Unlock()
	return i.isOnSourceBackend()
}

func (i *Image) EnsureIsolatedFromSourceForTesting(backends []*backend) {
	backendsM.Lock()
	defer backendsM.Unlock()
	i.ensureIsolatedFromSource(backends)
}

var FlushDeferredForTesting = flushDeferred

var FloorPowerOf2 = floorPowerOf2

func DeferredFuncCountForTesting() int {
	deferredM.Lock()
	defer deferredM.Unlock()
	return len(deferred)
}

func BackendCountForTesting() int {
	backendsM.Lock()
	defer backendsM.Unlock()
	return len(theBackends)
}

func ClearShaderForTesting() *Shader {
	return clearShader
}

func ShaderCountWithInternalShaderForTesting() int {
	theShadersWithInternalShader.m.Lock()
	defer theShadersWithInternalShader.m.Unlock()
	return len(theShadersWithInternalShader.shaders)
}

// SetA8SupportedForTesting sets whether new A8 images are put on A8 backends, as if the driver
// did or did not support A8, and returns a function that restores the previous setting.
func SetA8SupportedForTesting(supported bool) (restore func()) {
	backendsM.Lock()
	defer backendsM.Unlock()
	old := a8Supported
	a8Supported = supported
	return func() {
		backendsM.Lock()
		defer backendsM.Unlock()
		a8Supported = old
	}
}

func A8SupportedForTesting() bool {
	backendsM.Lock()
	defer backendsM.Unlock()
	return a8Supported
}

// BackendFormatForTesting returns the format of the image's backend, or false if the image is not
// allocated.
func (i *Image) BackendFormatForTesting() (graphicsdriver.PixelFormat, bool) {
	backendsM.Lock()
	defer backendsM.Unlock()
	if i.backend == nil {
		return 0, false
	}
	return i.backend.format, true
}

// SharesBackendForTesting reports whether i and j are on the same backend.
func (i *Image) SharesBackendForTesting(j *Image) bool {
	backendsM.Lock()
	defer backendsM.Unlock()
	return i.backend != nil && i.backend == j.backend
}
