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
	"cmp"
	"math/bits"
	"slices"

	"github.com/ironpark/ggfx/internal/recent"
)

const (
	// maxIdleBufferFrames is how many generations a pooled buffer may go unused before it is
	// released.
	maxIdleBufferFrames = 120

	// maxPooledBufferBytes is the most bytes the pool keeps in buffers no batch is using. One large
	// batch would otherwise pin its buffers' memory for as long as the app ran.
	maxPooledBufferBytes = 64 << 20

	// maxPooledBuffers is the most buffers the pool keeps, so that pool memory and eviction stay
	// bounded. A steady frame typically uses a handful of buffers (vertex, index, staging); 256
	// covers dozens of intermediate flushes while bounding worst-case eviction work. When a single
	// generation bursts past 256 buffers, the smallest buffers are trimmed first so expensive large
	// allocations stay reusable.
	maxPooledBuffers = 256

	// maxBufferWaste bounds how much larger than a request a reused buffer may be: a buffer at
	// most this many times the request's power of two, or at most minReusedBufferBytes, is handed
	// out. Were any larger buffer handed out, small batches would keep a large batch's buffers in
	// use, and they would never be released.
	maxBufferWaste       = 4
	minReusedBufferBytes = 64 << 10
)

// pooledBuffer is a buffer no batch is using, with its length and the generation that last used it.
type pooledBuffer[B any] struct {
	buf    B
	length uintptr
	gen    int64
}

type bufferBucket[B any] struct {
	bufs    []pooledBuffer[B]
	nonPow2 int
}

// bufferPool keeps the buffers of completed generations for later batches to reuse, bucketed by
// power-of-two size class so get is O(1) for power-of-two buffers. It releases a buffer unused for
// maxIdleBufferFrames generations, and the oldest buffers past maxPooledBufferBytes or
// maxPooledBuffers.
type bufferPool[B any] struct {
	buckets [64]bufferBucket[B]
	active  uint64 // bit k is set iff len(buckets[k].bufs) > 0
	count   int
	bytes   uintptr
}

func bucketIndex(length uintptr) int {
	if length <= 1 {
		return 0
	}
	k := bits.Len64(uint64(length - 1))
	if k >= 64 {
		return 63
	}
	return k
}

func (p *bufferPool[B]) len() int {
	return p.count
}

// put keeps buf, length bytes long and last used in gen, for reuse.
func (p *bufferPool[B]) put(buf B, length uintptr, gen int64) {
	k := bucketIndex(length)
	b := &p.buckets[k]
	b.bufs = append(b.bufs, pooledBuffer[B]{buf: buf, length: length, gen: gen})
	if length == 0 || length&(length-1) != 0 {
		b.nonPow2++
	}
	p.active |= 1 << k
	p.count++
	p.bytes += length
}

// get takes out the smallest buffer at least length bytes long and not too much longer, and
// reports whether there was one.
func (p *bufferPool[B]) get(length uintptr) (B, bool) {
	limit := max(recent.NextPowerOf2(length)*maxBufferWaste, minReusedBufferBytes)
	minK := bucketIndex(length)
	maxK := bucketIndex(limit)
	mask := p.active >> minK
	for mask != 0 {
		tz := bits.TrailingZeros64(mask)
		k := minK + tz
		if k > maxK {
			break
		}
		b := &p.buckets[k]
		if b.nonPow2 == 0 {
			// Every entry in bucket k has exact length 1<<k, which is in [length, limit].
			i := len(b.bufs) - 1
			res := b.bufs[i].buf
			p.remove(k, i)
			return res, true
		}
		best := -1
		for i, e := range b.bufs {
			if e.length < length || e.length > limit {
				continue
			}
			if best < 0 || e.length < b.bufs[best].length {
				best = i
			}
		}
		if best >= 0 {
			res := b.bufs[best].buf
			p.remove(k, best)
			return res, true
		}
		mask &^= 1 << tz
	}
	var zero B
	return zero, false
}

type poolEntryRef struct {
	bucket int
	idx    int
	gen    int64
	length uintptr
}

// evict releases the buffers unused since maxIdleBufferFrames before gen, then the oldest buffers
// while the pool is over its limits. The buffers of the newest generation in the pool are kept for
// the next batch past the byte limit so a large batch does not reallocate every time, while
// maxPooledBuffers is always enforced.
func (p *bufferPool[B]) evict(gen int64, release func(B)) {
	var oldest, newest int64
	hasEntry := false
	for mask := p.active; mask != 0; {
		k := bits.TrailingZeros64(mask)
		mask &^= 1 << k
		b := &p.buckets[k]
		for i := 0; i < len(b.bufs); {
			if e := b.bufs[i]; gen-e.gen > maxIdleBufferFrames {
				release(e.buf)
				p.remove(k, i)
				continue
			}
			if !hasEntry {
				oldest, newest = b.bufs[i].gen, b.bufs[i].gen
				hasEntry = true
			} else {
				oldest = min(oldest, b.bufs[i].gen)
				newest = max(newest, b.bufs[i].gen)
			}
			i++
		}
	}

	if p.bytes <= maxPooledBufferBytes && p.count <= maxPooledBuffers {
		return
	}
	// When every remaining buffer belongs to the newest generation and count is within
	// maxPooledBuffers, nothing is eligible for eviction; avoid allocating refs.
	if oldest == newest && p.count <= maxPooledBuffers {
		return
	}

	refs := make([]poolEntryRef, 0, p.count)
	for mask := p.active; mask != 0; {
		k := bits.TrailingZeros64(mask)
		mask &^= 1 << k
		for i, e := range p.buckets[k].bufs {
			refs = append(refs, poolEntryRef{bucket: k, idx: i, gen: e.gen, length: e.length})
		}
	}
	// Evict older generations first, largest buffers first within an older generation to reclaim
	// peak memory quickly; newest generation comes last and trims smallest-first if count alone
	// exceeds maxPooledBuffers, preserving expensive large allocations.
	slices.SortFunc(refs, func(a, b poolEntryRef) int {
		if c := cmp.Compare(a.gen, b.gen); c != 0 {
			return c
		}
		if a.gen == newest {
			return cmp.Compare(a.length, b.length)
		}
		return cmp.Compare(b.length, a.length)
	})

	bytes := p.bytes
	count := p.count
	drop := 0
	for drop < len(refs) && (bytes > maxPooledBufferBytes || count > maxPooledBuffers) {
		r := refs[drop]
		if r.gen == newest && count <= maxPooledBuffers {
			break
		}
		bytes -= r.length
		count--
		drop++
	}
	if drop == 0 {
		return
	}
	victims := refs[:drop]
	// Remove from highest index to lowest within each bucket so swap-remove does not shift
	// indices of earlier victims in the same bucket.
	slices.SortFunc(victims, func(a, b poolEntryRef) int {
		if c := cmp.Compare(a.bucket, b.bucket); c != 0 {
			return c
		}
		return cmp.Compare(b.idx, a.idx)
	})
	for _, v := range victims {
		release(p.buckets[v.bucket].bufs[v.idx].buf)
		p.remove(v.bucket, v.idx)
	}
}

// remove drops the i-th buffer of bucket k, not releasing it.
func (p *bufferPool[B]) remove(k, i int) {
	b := &p.buckets[k]
	e := b.bufs[i]
	p.bytes -= e.length
	p.count--
	if e.length == 0 || e.length&(e.length-1) != 0 {
		b.nonPow2--
	}
	last := len(b.bufs) - 1
	b.bufs[i] = b.bufs[last]
	b.bufs[last] = pooledBuffer[B]{}
	b.bufs = b.bufs[:last]
	if len(b.bufs) == 0 {
		p.active &^= 1 << k
	}
}
