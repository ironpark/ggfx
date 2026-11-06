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

package graphics

import (
	"testing"

	"github.com/ironpark/ggfx/internal/recent"
)

func TestBytesPoolBestFit(t *testing.T) {
	var p bytesPool
	p.put(make([]byte, 1<<20))
	p.put(make([]byte, 4096))
	p.put(make([]byte, 1024))

	// A small request takes neither the 1 MiB nor the 4 KiB array, too large for it.
	if bs := cached(&p, 140); bs != nil {
		t.Errorf("getFromCache(140): got an array of cap %d, want none", cap(bs))
	}
	if bs := cached(&p, 300); cap(bs) != 1024 {
		t.Errorf("getFromCache(300): got cap %d, want 1024", cap(bs))
	}
	if bs := cached(&p, 3000); cap(bs) != 4096 {
		t.Errorf("getFromCache(3000): got cap %d, want 4096", cap(bs))
	}
	if got, want := p.total, 1<<20; got != want {
		t.Errorf("total: got %d, want %d", got, want)
	}
}

func TestBytesPoolCountsCapacity(t *testing.T) {
	var p bytesPool
	p.put(make([]byte, 10, 1<<20))
	if got, want := p.total, 1<<20; got != want {
		t.Errorf("total: got %d, want %d", got, want)
	}
}

func TestBytesPoolLimit(t *testing.T) {
	var p bytesPool
	// An array larger than the limit is never kept.
	p.put(make([]byte, 16<<20))
	if p.count != 0 {
		t.Errorf("a 16 MiB array was kept")
	}
	for range 4 {
		p.put(make([]byte, 3<<20))
	}
	if p.total > maxPooledBytes {
		t.Errorf("total: got %d, want at most %d", p.total, maxPooledBytes)
	}
}

func TestBytesPoolAgesOut(t *testing.T) {
	var p bytesPool
	bs := p.get(1 << 20)
	bs.Release()
	if p.total == 0 {
		t.Fatalf("the array was not kept")
	}
	advanceFrame(&p)
	if p.total == 0 {
		t.Fatalf("the array was given back in the frame that used it")
	}
	for range 2 * recent.Window {
		advanceFrame(&p)
	}
	if p.total != 0 {
		t.Errorf("total after %d idle frames: got %d, want 0", 2*recent.Window, p.total)
	}
}

// A frame of many small uploads finds all its arrays in the pool the next frame.
func TestBytesPoolManySmall(t *testing.T) {
	var p bytesPool
	const n = 500
	sizes := func(k int) int { return 100 + k%40 }
	var bs [][]byte
	for k := range n {
		bs = append(bs, p.get(sizes(k)).Take())
	}
	for _, b := range bs {
		p.put(b)
	}
	advanceFrame(&p)
	for k := range n {
		if got := cached(&p, sizes(k)); got == nil {
			t.Fatalf("upload %d of %d bytes: no array in the pool", k, sizes(k))
		}
	}
}

// The pool drops the arrays given back longest ago, not the ones in use: an array given back every
// frame stays while older ones go.
func TestBytesPoolTrimsOldest(t *testing.T) {
	var p bytesPool
	for range 60 {
		p.put(make([]byte, 0, 256))
	}
	for range 3 * recent.Window {
		bs := p.get(5000).Take()
		p.put(bs)
		advanceFrame(&p)
	}
	if got := cached(&p, 5000); got == nil {
		t.Errorf("the array given back every frame was dropped")
	}
	if p.count != 0 {
		t.Errorf("arrays left: got %d, want 0", p.count)
	}
}

// advanceFrame ends a frame for p, as the frame loop and the command queue do.
func advanceFrame(p *bytesPool) {
	recent.Advance()
	p.advanceFrame()
}

// cached returns what p.getFromCache returns from its pool.
func cached(p *bytesPool, size int) []byte {
	bs, _ := p.getFromCache(size)
	return bs
}
