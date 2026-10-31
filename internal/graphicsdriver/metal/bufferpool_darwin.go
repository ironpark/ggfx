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

const (
	// maxIdleBufferFrames is how many frames a pooled buffer may go unused before it is released.
	maxIdleBufferFrames = 120

	// maxPooledBufferBytes is the most bytes the pool keeps in buffers no frame is using. One large
	// frame would otherwise pin its buffers' memory for as long as the app ran.
	maxPooledBufferBytes = 64 << 20

	// maxPooledBuffers is the most buffers the pool keeps, so that finding one stays cheap.
	maxPooledBuffers = 256

	// maxBufferWaste bounds how much larger than a request a reused buffer may be: a buffer at
	// most this many times the request's power of two, or at most minReusedBufferBytes, is handed
	// out. Were any larger buffer handed out, small frames would keep a large frame's buffers in
	// use, and they would never be released.
	maxBufferWaste       = 4
	minReusedBufferBytes = 64 << 10
)

// pooledBuffer is a buffer no frame is using, with its length and the last frame that used it.
type pooledBuffer[B any] struct {
	buf    B
	length uintptr
	frame  int64
}

// bufferPool keeps the buffers of completed frames for later frames to reuse. It releases a buffer
// unused for maxIdleBufferFrames frames, and the oldest buffers past maxPooledBufferBytes or
// maxPooledBuffers.
type bufferPool[B any] struct {
	bufs  []pooledBuffer[B]
	bytes uintptr
}

// put keeps buf, length bytes long and last used in frame, for reuse.
func (p *bufferPool[B]) put(buf B, length uintptr, frame int64) {
	p.bufs = append(p.bufs, pooledBuffer[B]{buf: buf, length: length, frame: frame})
	p.bytes += length
}

// get takes out the smallest buffer at least length bytes long and not too much longer, and
// reports whether there was one.
func (p *bufferPool[B]) get(length uintptr) (B, bool) {
	limit := max(pow2(length)*maxBufferWaste, minReusedBufferBytes)
	best := -1
	for i, b := range p.bufs {
		if b.length < length || b.length > limit {
			continue
		}
		if best < 0 || b.length < p.bufs[best].length {
			best = i
		}
	}
	if best < 0 {
		var zero B
		return zero, false
	}
	b := p.bufs[best]
	p.remove(best)
	return b.buf, true
}

// evict releases the buffers unused since maxIdleBufferFrames before frame, then the oldest buffers
// while the pool is over its limits. The buffers of the newest frame in the pool are kept for the
// next frame whatever their size, so a frame that needs more than the limit does not allocate its
// buffers anew every time.
func (p *bufferPool[B]) evict(frame int64, release func(B)) {
	var newest int64
	for i := 0; i < len(p.bufs); {
		if b := p.bufs[i]; frame-b.frame > maxIdleBufferFrames {
			release(b.buf)
			p.remove(i)
			continue
		}
		newest = max(newest, p.bufs[i].frame)
		i++
	}

	for p.bytes > maxPooledBufferBytes || len(p.bufs) > maxPooledBuffers {
		// Release the oldest buffer, and the largest of the oldest.
		victim := -1
		for i, b := range p.bufs {
			if b.frame == newest {
				continue
			}
			if victim < 0 {
				victim = i
				continue
			}
			if v := p.bufs[victim]; b.frame < v.frame || (b.frame == v.frame && b.length > v.length) {
				victim = i
			}
		}
		if victim < 0 {
			break
		}
		release(p.bufs[victim].buf)
		p.remove(victim)
	}
}

// remove drops the i-th buffer, not releasing it.
func (p *bufferPool[B]) remove(i int) {
	p.bytes -= p.bufs[i].length
	last := len(p.bufs) - 1
	p.bufs[i] = p.bufs[last]
	p.bufs[last] = pooledBuffer[B]{}
	p.bufs = p.bufs[:last]
}
