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
	"github.com/ironpark/ggfx/internal/graphicscommand"
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

	// Flushes, Commands and Vertices count the command queue's flushes to
	// the GPU, the commands it ran and the vertices it sent, since the
	// program started; read twice, they give a rate.
	Flushes, Commands, Vertices int64
	// QueueBufferBytes is what the queue keeps between flushes for
	// vertices and indices, as large as the largest batch so far.
	QueueBufferBytes int64

	// AtlasPages are the GPU images that images share, an atlas page each, and Isolated sums
	// the rest, one image's each, the screens among them.
	AtlasPages []AtlasPage
	Isolated   AtlasPage
	// ScreenBytes is what TotalGPUImageMemoryUsageInBytes counts for the screens, which on Metal
	// are the layer's drawables, counted by the window system's memory rather than as images.
	ScreenBytes int64
}

// AtlasPage is a GPU image some images share: its size in pixels, the bytes the GPU keeps for it,
// and how many pixels of it, and how many images, are in use. A page grows as images come and
// lets go only once all of them have gone.
type AtlasPage struct {
	Width, Height int
	Bytes         int64
	UsedPixels    int64
	Images        int
	Source        bool
	// Format is the page's format on the GPU: PixelFormatAlpha only where the driver has
	// alpha-only textures.
	Format PixelFormat
}

// ReadDebugInfo writes debug info (e.g. the GPU memory used by images) into a provided struct.
//
// ReadDebugInfo is concurrent-safe.
func ReadDebugInfo(d *DebugInfo) {
	d.TotalGPUImageMemoryUsageInBytes = atlas.TotalGPUImageMemoryUsageInBytes()
	d.Flushes, d.Commands, d.Vertices, d.QueueBufferBytes = graphicscommand.Counts()
	d.AtlasPages, d.Isolated, d.ScreenBytes = d.AtlasPages[:0], AtlasPage{}, 0
	for _, b := range atlas.ReadBackendStats(nil) {
		p := AtlasPage{Width: b.Width, Height: b.Height, Bytes: int64(b.Format.BytesPerPixel()) * int64(b.InternalWidth) * int64(b.InternalHeight),
			UsedPixels: b.UsedArea, Images: b.Images, Source: b.Source, Format: PixelFormat(b.Format)}
		if b.Screen {
			d.ScreenBytes += p.Bytes
		}
		if !b.Atlas {
			d.Isolated.Bytes += p.Bytes
			d.Isolated.UsedPixels += p.UsedPixels
			d.Isolated.Images++
			continue
		}
		d.AtlasPages = append(d.AtlasPages, p)
	}
}
