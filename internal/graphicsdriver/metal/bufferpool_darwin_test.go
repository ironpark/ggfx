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

package metal

import (
	"testing"
)

// fakeBuffer stands for a Metal buffer; release counts its releases.
type fakeBuffer struct {
	length   uintptr
	released int
}

func releaseFake(b *fakeBuffer) {
	b.released++
}

// A steady frame reuses the buffers of the frame before it, and releases none.
func TestBufferPoolReusesSteadyFrames(t *testing.T) {
	var p bufferPool[*fakeBuffer]
	var allocated int
	inUse := []*fakeBuffer{}
	for frame := range int64(1000) {
		for _, b := range inUse {
			p.put(b, b.length, frame-1)
		}
		inUse = inUse[:0]
		p.evict(frame, releaseFake)
		for _, length := range []uintptr{1 << 10, 1 << 20, 16 << 20} {
			b, ok := p.get(length)
			if !ok {
				b = &fakeBuffer{length: length}
				allocated++
			}
			inUse = append(inUse, b)
		}
	}
	if allocated != 3 {
		t.Errorf("allocated %d buffers over steady frames, want 3", allocated)
	}
	for _, b := range inUse {
		if b.released != 0 {
			t.Errorf("a buffer in use was released %d times", b.released)
		}
	}
}

// The buffers of one large frame are not kept for long once frames are small again.
func TestBufferPoolReleasesAPeakFrame(t *testing.T) {
	var p bufferPool[*fakeBuffer]
	var peak []*fakeBuffer
	for range 8 {
		peak = append(peak, &fakeBuffer{length: 32 << 20})
	}
	for _, b := range peak {
		p.put(b, b.length, 0)
	}
	small := &fakeBuffer{length: 4 << 10}
	p.put(small, small.length, 1)

	// Past the byte limit, the oldest buffers go at once; the newest frame's stay.
	p.evict(2, releaseFake)
	if p.bytes > maxPooledBufferBytes {
		t.Errorf("after the peak frame: pool keeps %d bytes, want at most %d", p.bytes, maxPooledBufferBytes)
	}

	// Small requests are not served by the large buffers, so those stay unused.
	for frame := int64(2); frame < 2+maxIdleBufferFrames+2; frame++ {
		b, ok := p.get(1 << 10)
		if !ok {
			t.Fatalf("frame %d: no buffer for a small request", frame)
		}
		if b != small {
			t.Fatalf("frame %d: a small request got a %d-byte buffer", frame, b.length)
		}
		p.put(b, b.length, frame)
		p.evict(frame+1, releaseFake)
	}
	for i, b := range peak {
		if b.released != 1 {
			t.Errorf("peak buffer %d released %d times, want 1", i, b.released)
		}
	}
	if small.released != 0 {
		t.Errorf("the small buffer in steady use was released %d times", small.released)
	}
	if len(p.bufs) != 1 || p.bytes != small.length {
		t.Errorf("pool keeps %d buffers and %d bytes, want 1 and %d", len(p.bufs), p.bytes, small.length)
	}
}

// A frame larger than the byte limit keeps its buffers for the next frame.
func TestBufferPoolKeepsTheNewestFrame(t *testing.T) {
	var p bufferPool[*fakeBuffer]
	var bufs []*fakeBuffer
	for range 4 {
		b := &fakeBuffer{length: 32 << 20}
		bufs = append(bufs, b)
		p.put(b, b.length, 5)
	}
	p.evict(6, releaseFake)
	for i, b := range bufs {
		if b.released != 0 {
			t.Errorf("buffer %d of the newest frame released %d times, want 0", i, b.released)
		}
	}
	if b, ok := p.get(20 << 20); !ok || b.length != 32<<20 {
		t.Errorf("get(20MB) = %v, %v; want a 32MB buffer", b, ok)
	}
}
