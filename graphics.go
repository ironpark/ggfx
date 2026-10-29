// Copyright 2014 Hajime Hoshi
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

package ggfx

import (
	"github.com/ironpark/ggfx/internal/atlas"
	"github.com/ironpark/ggfx/internal/builtinshader"
)

// Filter represents the type of texture filter to be used when an image is magnified or minified.
type Filter int

const (
	// FilterNearest represents nearest (crisp-edged) filter
	FilterNearest Filter = Filter(builtinshader.FilterNearest)

	// FilterLinear represents linear filter
	FilterLinear Filter = Filter(builtinshader.FilterLinear)

	// FilterPixelated represents a pixelated filter.
	// FilterPixelated is similar to FilterNearest, but it preserves the pixelated appearance even when scaled to non-integer sizes.
	FilterPixelated Filter = Filter(builtinshader.FilterPixelated)
)

// DebugInfo is a struct to store debug info about the graphics.
//
// The graphics library in use is a window system's to know, through ggfx/host's CurrentDriver.
type DebugInfo struct {
	// TotalGPUImageMemoryUsageInBytes is the total image memory usage for GPU in bytes.
	// TotalGPUImageMemoryUsageInBytes is approximately the total memory usage for GPU.
	TotalGPUImageMemoryUsageInBytes int64
}

// ReadDebugInfo writes debug info (e.g. the GPU memory used by images) into a provided struct.
//
// ReadDebugInfo is concurrent-safe.
func ReadDebugInfo(d *DebugInfo) {
	d.TotalGPUImageMemoryUsageInBytes = atlas.TotalGPUImageMemoryUsageInBytes()
}
