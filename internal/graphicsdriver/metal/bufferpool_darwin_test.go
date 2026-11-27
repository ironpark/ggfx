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
	if p.len() != 1 || p.bytes != small.length {
		t.Errorf("pool keeps %d buffers and %d bytes, want 1 and %d", p.len(), p.bytes, small.length)
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

// Even if a single generation puts thousands of buffers into the pool, evict trims to maxPooledBuffers.
func TestBufferPoolTrimsSingleGenerationBurst(t *testing.T) {
	var p bufferPool[*fakeBuffer]
	const total = 4000
	bufs := make([]*fakeBuffer, total)
	for i := range bufs {
		b := &fakeBuffer{length: 4 << 10}
		bufs[i] = b
		p.put(b, b.length, 10)
	}
	p.evict(11, releaseFake)
	if p.len() != maxPooledBuffers {
		t.Fatalf("pool kept %d buffers after burst, want %d", p.len(), maxPooledBuffers)
	}
	var released int
	for _, b := range bufs {
		released += b.released
	}
	if want := total - maxPooledBuffers; released != want {
		t.Errorf("released %d buffers, want %d", released, want)
	}
}

func TestBufferPoolNonPowerOfTwo(t *testing.T) {
	var p bufferPool[*fakeBuffer]
	small := &fakeBuffer{length: 900}
	mid := &fakeBuffer{length: 1000}
	exact := &fakeBuffer{length: 1024}
	p.put(small, small.length, 0)
	p.put(exact, exact.length, 0)
	p.put(mid, mid.length, 0)

	b, ok := p.get(950)
	if !ok || b != mid {
		t.Fatalf("get(950) = %v, %v; want 1000-byte buffer", b, ok)
	}
	b, ok = p.get(950)
	if !ok || b != exact {
		t.Fatalf("second get(950) = %v, %v; want 1024-byte buffer", b, ok)
	}
	if b, ok := p.get(950); ok {
		t.Fatalf("third get(950) = %v, true; want false", b)
	}
	if b, ok := p.get(800); !ok || b != small {
		t.Fatalf("get(800) = %v, %v; want 900-byte buffer", b, ok)
	}
}

// Evicting when every buffer belongs to the newest generation, exceeds maxPooledBufferBytes,
// and count <= maxPooledBuffers must not allocate or release anything.
func TestBufferPoolEvictNoVictimZeroAllocs(t *testing.T) {
	var p bufferPool[*fakeBuffer]
	for range 4 {
		b := &fakeBuffer{length: 32 << 20}
		p.put(b, b.length, 5)
	}
	allocs := testing.AllocsPerRun(100, func() {
		p.evict(6, releaseFake)
	})
	if allocs != 0 {
		t.Errorf("evict in no-victim newest-gen case allocated %v times/run, want 0", allocs)
	}
	if p.len() != 4 {
		t.Errorf("pool kept %d buffers, want 4", p.len())
	}
}

// When a single generation exceeds maxPooledBuffers, smallest buffers are trimmed first so large
// allocations stay reusable.
func TestBufferPoolSingleGenerationTrimsSmallestFirst(t *testing.T) {
	var p bufferPool[*fakeBuffer]
	large := &fakeBuffer{length: 1 << 20}
	p.put(large, large.length, 10)
	for range maxPooledBuffers + 20 {
		b := &fakeBuffer{length: 4 << 10}
		p.put(b, b.length, 10)
	}
	p.evict(11, releaseFake)
	if p.len() != maxPooledBuffers {
		t.Fatalf("pool kept %d buffers, want %d", p.len(), maxPooledBuffers)
	}
	if large.released != 0 {
		t.Errorf("large buffer was released %d times, want 0 (smallest should be trimmed first)", large.released)
	}
}

func BenchmarkBufferPoolGet(b *testing.B) {
	var p bufferPool[*fakeBuffer]
	for i := range maxPooledBuffers {
		size := uintptr(4<<10) << (i % 8)
		p.put(&fakeBuffer{length: size}, size, 1)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		buf, ok := p.get(16 << 10)
		if !ok {
			b.Fatal("expected pooled buffer")
		}
		p.put(buf, buf.length, 1)
	}
}
