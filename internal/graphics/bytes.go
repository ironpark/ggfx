// Copyright 2023 The Ebitengine Authors
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

package graphics

import (
	"math/bits"
	"sync"

	"github.com/ironpark/ggfx/internal/recent"
)

// ManagedBytes is a managed byte slice.
// The internal byte alice are managed in a pool.
// ManagedBytes is useful when its lifetime is explicit, as the underlying byte slice can be reused for another ManagedBytes later.
// This can reduce allocations and GCs.
//
// A ManagedBytes dropped without Release or Take is collected with its slice, which is then just
// not reused.
type ManagedBytes struct {
	bytes []byte
	pool  *bytesPool
}

// Len returns the length of the slice.
func (m *ManagedBytes) Len() int {
	return len(m.bytes)
}

// Read reads the byte slice's content to dst.
func (m *ManagedBytes) Read(dst []byte, from, to int) {
	copy(dst, m.bytes[from:to])
}

// Clone creates a new ManagedBytes with the same content.
func (m *ManagedBytes) Clone() *ManagedBytes {
	return NewManagedBytes(len(m.bytes), func(bs []byte) {
		copy(bs, m.bytes)
	})
}

// Take returns the raw byte slice. Give it back with PutBytes when you can ensure that the slice
// is no longer used, e.g. when a graphics command using this slice is sent and executed.
//
// After Take is called, the underlying byte slice is no longer available.
func (m *ManagedBytes) Take() []byte {
	bs := m.bytes
	m.bytes = nil
	return bs
}

// PutBytes gives back a slice Take returned, which must no longer be used, for later
// ManagedBytes to reuse.
func PutBytes(bs []byte) {
	theBytesPool.put(bs)
}

// Release releases the underlying byte slice.
//
// After Release is called, the underlying byte slice is no longer available.
func (m *ManagedBytes) Release() {
	m.pool.put(m.bytes)
	m.bytes = nil
}

// NewManagedBytes returns a managed byte slice initialized by the given constructor f.
//
// The byte slice is not zero-cleared at the constructor.
func NewManagedBytes(size int, f func([]byte)) *ManagedBytes {
	bs := theBytesPool.get(size)
	f(bs.bytes)
	return bs
}

// The pool keeps byte arrays given back for later uploads. It keeps at most maxPooledBytes of
// arrays, counted by capacity, and no more than the frames of the last recent.Window to
// 2*recent.Window took from it, so that a rare large upload's arrays are given back. The arrays
// are kept in buckets by the power of two of their capacity, so that a frame of many small uploads,
// a glyph each, finds its arrays without a long search.
const (
	// maxPooledBytes is the most the pool keeps. An array larger than this is never kept.
	maxPooledBytes = 8 * 1024 * 1024

	// maxPooledArrays is the most arrays the pool keeps.
	maxPooledArrays = 4096

	// maxWasteFactor is how many times larger than the request an array taken from the pool may be.
	maxWasteFactor = 4

	// maxRoundedBytes is the largest request whose new array is rounded up to a power of two.
	maxRoundedBytes = 64 * 1024
)

// numBytesClasses is the number of buckets: class c holds arrays of capacity in [2^c, 2^(c+1)).
const numBytesClasses = 64

// pooledBytes is an array in the pool, and when it was given back, in the order of puts.
type pooledBytes struct {
	bytes []byte
	seq   uint64
}

type bytesPool struct {
	// classes hold the arrays by the power of two of their capacity, the last given back last.
	classes [numBytesClasses][]pooledBytes
	// seq counts the puts.
	seq uint64
	// total is the sum of the capacities of the arrays in the pool, and count how many there are.
	total int
	count int

	// taken is the bytes of the arrays taken, from the pool or newly made, in this frame, and use
	// tracks it over recent frames.
	taken int
	use   recent.Peak

	m sync.Mutex
}

var theBytesPool bytesPool

// AdvanceBytesPoolFrame tells the pool of upload bytes that a frame's uploads are done, and gives
// back the arrays recent frames did not need. The pool ages by the frame clock, recent.Now, so
// calling it more than once a frame does not age it faster. It can be called from any goroutine.
func AdvanceBytesPoolFrame() {
	theBytesPool.advanceFrame()
}

func (b *bytesPool) get(size int) *ManagedBytes {
	bs, newCap := b.getFromCache(size)
	if bs == nil {
		bs = make([]byte, size, newCap)
	}
	return &ManagedBytes{
		bytes: bs,
		pool:  b,
	}
}

// bytesClass returns the class of an array of capacity c > 0.
func bytesClass(c int) int {
	return bits.Len(uint(c)) - 1
}

// getFromCache returns an array in the pool that holds size bytes and is at most maxWasteFactor
// times as large, sliced to size: the last given back of the smallest class that has one. If there
// is none, it returns nil and the capacity to make a new array with, which it counts as taken.
func (b *bytesPool) getFromCache(size int) (bs []byte, newCap int) {
	b.m.Lock()
	defer b.m.Unlock()

	limit := maxWasteFactor * max(size, 64)
	if size > 0 {
		for c := bytesClass(size); c < numBytesClasses && 1<<c <= limit; c++ {
			bucket := b.classes[c]
			for i := len(bucket) - 1; i >= 0; i-- {
				bs := bucket[i].bytes
				if n := cap(bs); n < size || n > limit {
					continue
				}
				copy(bucket[i:], bucket[i+1:])
				bucket[len(bucket)-1] = pooledBytes{}
				b.classes[c] = bucket[:len(bucket)-1]
				b.total -= cap(bs)
				b.count--
				b.taken += cap(bs)
				return bs[:size], 0
			}
		}
	}

	// A small array has the capacity of the power of two that holds it, so that any array of its
	// class serves a later request of the same class.
	newCap = size
	if size <= maxRoundedBytes {
		newCap = recent.NextPowerOf2(size)
	}
	b.taken += newCap
	return nil, newCap
}

func (b *bytesPool) put(bs []byte) {
	if cap(bs) == 0 || cap(bs) > maxPooledBytes {
		return
	}

	b.m.Lock()
	defer b.m.Unlock()

	c := bytesClass(cap(bs))
	b.seq++
	b.classes[c] = append(b.classes[c], pooledBytes{bytes: bs[:0], seq: b.seq})
	b.total += cap(bs)
	b.count++
	b.trim(maxPooledBytes)
}

// trim drops arrays, the oldest given back first, until the pool holds at most limit bytes and
// maxPooledArrays arrays.
func (b *bytesPool) trim(limit int) {
	if b.total <= limit && b.count <= maxPooledArrays {
		return
	}
	// dropped[c] is how many of the oldest arrays, the first, of class c are dropped.
	var dropped [numBytesClasses]int
	for b.count > 0 && (b.total > limit || b.count > maxPooledArrays) {
		oldest := -1
		for c := range b.classes {
			if dropped[c] == len(b.classes[c]) {
				continue
			}
			if oldest < 0 || b.classes[c][dropped[c]].seq < b.classes[oldest][dropped[oldest]].seq {
				oldest = c
			}
		}
		b.total -= cap(b.classes[oldest][dropped[oldest]].bytes)
		b.count--
		dropped[oldest]++
	}
	for c, n := range dropped {
		if n == 0 {
			continue
		}
		bucket := b.classes[c]
		m := copy(bucket, bucket[n:])
		clear(bucket[m:])
		b.classes[c] = bucket[:m]
	}
}

func (b *bytesPool) advanceFrame() {
	b.m.Lock()
	defer b.m.Unlock()

	b.use.ObserveNow(b.taken)
	b.taken = 0
	b.trim(min(maxPooledBytes, b.use.Max()))
}
