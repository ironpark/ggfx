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
	"runtime"
	"sync"

	"github.com/ironpark/ggfx/internal/recent"
)

// ManagedBytes is a managed byte slice.
// The internal byte alice are managed in a pool.
// ManagedBytes is useful when its lifetime is explicit, as the underlying byte slice can be reused for another ManagedBytes later.
// This can reduce allocations and GCs.
type ManagedBytes struct {
	bytes   []byte
	pool    *bytesPool
	cleanup runtime.Cleanup
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

// GetAndRelease returns the raw byte slice and a finalizer.
// A finalizer should be called when you can ensure that the slice is no longer used,
// e.g. when a graphics command using this slice is sent and executed.
//
// After GetAndRelease is called, the underlying byte slice is no longer available.
func (m *ManagedBytes) GetAndRelease() ([]byte, func()) {
	bs := m.bytes
	m.bytes = nil
	return bs, func() {
		m.pool.put(bs)
		m.cleanup.Stop()
	}
}

// Release releases the underlying byte slice.
//
// After Release is called, the underlying byte slice is no longer available.
func (m *ManagedBytes) Release() {
	m.pool.put(m.bytes)
	m.bytes = nil
	m.cleanup.Stop()
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
// 2*recent.Window took from it, so that a rare large upload's arrays are given back.
const (
	// maxPooledBytes is the most the pool keeps. An array larger than this is never kept.
	maxPooledBytes = 8 * 1024 * 1024

	// maxPooledArrays is the most arrays the pool keeps.
	maxPooledArrays = 64

	// maxWasteFactor is how many times larger than the request an array taken from the pool may be.
	maxWasteFactor = 4
)

type bytesPool struct {
	// pool holds the arrays, the oldest given back first.
	pool [][]byte
	// total is the sum of the capacities of the arrays in pool.
	total int

	// taken is the bytes of the arrays taken, from the pool or newly made, in this frame, and use
	// tracks it over recent frames.
	taken int
	use   recent.Peak

	m sync.Mutex
}

var theBytesPool bytesPool

// AdvanceBytesPoolFrame tells the pool of upload bytes that a frame ended, and gives back the arrays
// recent frames did not need. It can be called from any goroutine.
func AdvanceBytesPoolFrame() {
	theBytesPool.advanceFrame()
}

func (b *bytesPool) get(size int) *ManagedBytes {
	bs := b.getFromCache(size)
	if bs == nil {
		bs = make([]byte, size)
	}
	m := &ManagedBytes{
		bytes: bs,
		pool:  b,
	}
	m.cleanup = runtime.AddCleanup(m, func(bytes []byte) {
		b.put(bytes)
	}, m.bytes)
	return m
}

// getFromCache returns the smallest array in the pool that holds size bytes and is at most
// maxWasteFactor times as large, sliced to size, or nil if there is none.
func (b *bytesPool) getFromCache(size int) []byte {
	b.m.Lock()
	defer b.m.Unlock()

	best := -1
	for i, bs := range b.pool {
		c := cap(bs)
		if c < size || c > maxWasteFactor*max(size, 64) {
			continue
		}
		if best < 0 || c < cap(b.pool[best]) {
			best = i
			if c == size {
				break
			}
		}
	}
	if best < 0 {
		b.taken += size
		return nil
	}

	bs := b.pool[best]
	b.removeAt(best)
	b.taken += cap(bs)
	return bs[:size]
}

func (b *bytesPool) removeAt(i int) {
	b.total -= cap(b.pool[i])
	copy(b.pool[i:], b.pool[i+1:])
	b.pool[len(b.pool)-1] = nil
	b.pool = b.pool[:len(b.pool)-1]
}

func (b *bytesPool) put(bs []byte) {
	if cap(bs) == 0 || cap(bs) > maxPooledBytes {
		return
	}

	b.m.Lock()
	defer b.m.Unlock()

	b.pool = append(b.pool, bs[:0])
	b.total += cap(bs)
	b.trim(maxPooledBytes)
}

// trim drops the oldest arrays until the pool holds at most limit bytes and maxPooledArrays arrays.
func (b *bytesPool) trim(limit int) {
	n := 0
	total := b.total
	for n < len(b.pool) && (len(b.pool)-n > maxPooledArrays || total > limit) {
		total -= cap(b.pool[n])
		n++
	}
	if n == 0 {
		return
	}
	m := copy(b.pool, b.pool[n:])
	clear(b.pool[m:])
	b.pool = b.pool[:m]
	b.total = total
}

func (b *bytesPool) advanceFrame() {
	b.m.Lock()
	defer b.m.Unlock()

	b.use.Add(b.taken)
	b.taken = 0
	b.trim(min(maxPooledBytes, b.use.Max()))
}
