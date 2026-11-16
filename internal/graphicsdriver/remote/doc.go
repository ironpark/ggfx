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

// Package remote is a graphics driver that draws nothing itself. Graphics, used in an app process,
// encodes every driver call into a stream; Replayer, used in a viewer process that owns the
// window, decodes the stream and makes the same calls on its real driver. The app then needs no
// GPU, nothing is read back, and what crosses between the processes grows with what changed, not
// with the size of the window. ggfx/remote is the public face of both sides.
//
// # Wire format
//
// This is the whole format; Version changes whenever any of it does. Numbers are little-endian.
// An array is its length in bytes as a u32, then zero padding to a multiple of 4 bytes from the
// start of the batch, then its bytes: an array of float32 or uint32 is then aligned for the
// replayer to use in place. A rectangle is four i32: min x, min y, max x, max y. An id is a u32
// the app gives an image or a shader; 0 is no image.
//
// The viewer first tells the app what its driver answers, as an Info of 12 bytes:
//
//	"GGFI" | version u16 | color space u8 | flags u8 (1: FlipsY, 2: A8 images) | max image size u32
//
// The app then sends batches, one for each frame it ends and one before each read of pixels. A
// batch is a header of 20 bytes and operations up to its end:
//
//	"GGFX" | version u16 | reserved u16 (0) | screen id u32 | screen width u32 | screen height u32
//
// The screen fields are the app's screen as of the end of the batch, all 0 when it has none, so
// that the viewer can give the replay a target of that size before it runs. Each operation is
// an opcode byte and its fields:
//
//	1 SetVertices      vertices (float32 array), indices (uint32 array)
//	2 NewImage         id, width u32, height u32, format u8
//	3 NewScreenImage   id, width u32, height u32
//	4 DisposeImage     id
//	5 NewShader        id, texture count u32, source (byte array: the shader's own source,
//	                   without the prelude; the viewer compiles it again, as the IR has no wire form)
//	6 DisposeShader    id
//	7 WritePixels      image id, count u32, count × (rectangle, pixels byte array)
//	8 DrawTriangles    dst id, 4 × source id, shader id, count u32, count × (rectangle, index
//	                   count u32), index offset u32, 6 × blend u8 (source RGB, source alpha,
//	                   destination RGB, destination alpha factors, RGB and alpha operations),
//	                   internal uniform version u64, user uniform version u64, uniforms
//	                   (uint32 array)
//	9 Present          screen id, count u32, count × rectangle: what changed; a count of
//	                   0xffffffff is all of the screen
//
// Begin and End are not sent: the replay runs inside a command batch of the viewer's own, and the
// end of a frame is the end of a batch.
//
// # The viewer's side
//
// A batch is untrusted input. Replay checks every operation before it calls the driver: lengths
// against the batch, ids against what the app made, sizes against the driver's limits, rectangles
// against the images, indices against the vertices, uniforms against the shader. A batch that
// fails a check returns an error and the replay stops there; what ran before it stays, so the
// viewer should drop the app.
//
// The app's screen is not an image of the replayer's: it is the target the viewer gives each
// replay, an image of its own that it keeps from frame to frame and across apps. An offscreen
// image draws at its internal size, a multiple of 16, while the app wrote its screen draws'
// projection and destination size for the screen's exact size; the replay rewrites those two
// uniforms for the target's internal size, which keeps every driver's offscreen images as they
// are.
package remote
