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

package vector

import (
	"image"
	"math"
	"slices"
	"sync/atomic"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/internal/recent"
	"github.com/ironpark/ggfx/internal/render"
)

// The stencil cache keeps the stencil buffers of the paths filled across flushes and frames, so
// that a path filled again needs only its cover quad: no stencil draw, and, when every path of a
// flush is cached, no switch of the render target to a stencil image.
//
// A stencil buffer's texels depend on the path's points relative to its rendering position, the
// size of its rendering bounds and antialiasing only; the fill rule, the color and the blend are
// the cover's. The key is these, with the points as the exact bits of their float32 offsets from
// the rendering position, which is on whole pixels, so a path moved by whole pixels hits. The
// stencil triangles are computed from the same offsets (see appendStencilTriangles), so a hit's
// texels are the ones drawing the path again at the same place would make. The whole key is kept
// and compared, so a hash collision cannot give a wrong buffer.
//
// A path is cached the second time it is filled, in the frame it is first filled or the next: a
// path filled once, as one moving by a fraction of a pixel every frame is, stays on theAtlas, so
// that a scene whose paths all change costs no more than without the cache but the keys.
//
// The buffers are on pages, unmanaged images like theAtlas's. A page's regions are allocated in
// shelves and never freed one by one: a page is evicted whole, its entries dropped and its image
// cleared once, so no region needs a clear of its own, which would be a draw each. The page evicted
// is one no fill of this frame or the last used, or, when the pages are at their size bound, the
// least recently used one but the pages of the current flush; with none, a path stays on theAtlas. The pages
// survive a loss of the GPU's resources like any image: internal/atlas saves and restores the
// pixels of all of them.
//
// The cache is protected by theFillPathM.
var theStencilCache stencilCache

const (
	// A new page is twice as large as the largest page kept, from stencilPageMinSize up to
	// stencilPageMaxSize, so a few paths take little memory and many take few pages; a cover draw
	// reads up to four. A larger region gets a page of its own, of its size.
	stencilPageMinSize = 256
	stencilPageMaxSize = 1024

	// stencilCacheMaxBytes bounds the size of the pages kept. A larger region than half of it is
	// not cached, nor is a region larger than an image theAtlas would make.
	stencilCacheMaxBytes  = 32 << 20
	stencilMaxRegionSize  = 4093
	stencilMaxRegionBytes = stencilCacheMaxBytes / 2
)

// disableStencilCacheForTesting makes every path go to theAtlas, as before the cache.
var disableStencilCacheForTesting bool

// redrawStencilCacheForTesting makes a cached path's buffers cleared and drawn again where they
// are, which must change no texel.
var redrawStencilCacheForTesting bool

type stencilEntry struct {
	key    []uint32
	hash   uint64
	page   *stencilPage
	region image.Rectangle
	// next is the next entry with the same hash.
	next *stencilEntry
}

type stencilShelf struct {
	y, h int
	// x is where the free space of the shelf begins.
	x int
}

type stencilPage struct {
	img     *ggfx.Image
	size    image.Point
	shelves []stencilShelf
	// bottom is where the space below the shelves begins.
	bottom  int
	entries []*stencilEntry

	// flush is the serial of the last flush that used the page, and frame the frame it was in.
	flush uint64
	frame int64
}

const stencilSeenBits = 12

type stencilSeen struct {
	hash  uint64
	frame int64
}

type stencilCache struct {
	entries map[uint64]*stencilEntry
	pages   []*stencilPage
	// free are entries of evicted pages, kept for their keys' capacity.
	free []*stencilEntry
	// seen holds the hashes of keys missed, by their top bits, with the frames they were missed
	// in. A hash pushed out by others is only admitted later.
	seen [1 << stencilSeenBits]stencilSeen
	// flush counts the flushes placing paths in the cache.
	flush uint64
	bytes atomic.Int64
	// key is the key of the path being placed, reused so that a lookup allocates nothing.
	key []uint32
}

// stencilSlot is where the stencil buffers of a path are for a flush.
type stencilSlot struct {
	// img is nil for a path with no region, which is drawn nowhere.
	img *ggfx.Image
	// region holds the stencil buffers: it is the buffer, or for antialiasing the two side by side.
	region image.Rectangle
	// draw reports whether the buffers are yet to be drawn.
	draw bool
	// transient reports whether the path is on theAtlas, for this flush only.
	transient bool
}

// buffer returns the k-th stencil buffer of the slot.
func (s *stencilSlot) buffer(antialias bool, k int) image.Rectangle {
	return stencilBuffer(s.region, antialias, k)
}

// stencilBuffer returns the k-th stencil buffer in region, which is the buffer, or for antialiasing
// the two side by side.
func stencilBuffer(region image.Rectangle, antialias bool, k int) image.Rectangle {
	if antialias {
		switch k {
		case 0:
			region.Max.X = region.Min.X + region.Dx()/2
		case 1:
			region.Min.X += region.Dx() / 2
		default:
			panic("not reached")
		}
	}
	return region
}

// place places the paths of a flush at time now, in frames, whose rendering bounds are rbs. A path
// already cached gets its buffers, a new one a region to draw them in, and a path the cache cannot
// hold is marked transient.
func (c *stencilCache) place(now int64, paths []*Path, rbs []image.Rectangle, antialias bool, slots []stencilSlot) {
	c.flush++
	c.expire(now)
	for i, p := range paths {
		rb := rbs[i]
		if rb.Empty() {
			continue
		}
		s := rb.Size()
		if antialias {
			s.X *= 2
		}
		slot := &slots[i]
		if s.X > stencilMaxRegionSize || s.Y > stencilMaxRegionSize || 4*int64(s.X)*int64(s.Y) > stencilMaxRegionBytes {
			slot.transient = true
			continue
		}
		var h uint64
		c.key, h = appendStencilKey(c.key[:0], p, rb, antialias)
		e := c.lookup(h, c.key)
		if e == nil {
			if !c.admit(h, now) {
				slot.transient = true
				continue
			}
			if e = c.alloc(now, s); e == nil {
				slot.transient = true
				continue
			}
			e.key = append(e.key[:0], c.key...)
			e.hash = h
			if c.entries == nil {
				c.entries = map[uint64]*stencilEntry{}
			}
			e.next = c.entries[h]
			c.entries[h] = e
			slot.draw = true
		} else if redrawStencilCacheForTesting && !slices.ContainsFunc(slots[:i], func(s stencilSlot) bool {
			// A path placed before in this flush has the buffers drawn or kept already.
			return s.img == e.page.img && s.region == e.region
		}) {
			sub := e.page.img.RecyclableSubImage(e.region)
			clearStencilImage(sub)
			sub.Recycle()
			slot.draw = true
		}
		// A page used by this flush is never evicted by it.
		e.page.flush, e.page.frame = c.flush, now
		slot.img, slot.region = e.page.img, e.region
	}
}

// admit reports whether a key missed with hash h at time now was missed in this frame or the last.
// A hash colliding with another key's admits a path early, which is harmless. A hash has two
// places, so that two keys missed in turn do not push each other out.
func (c *stencilCache) admit(h uint64, now int64) bool {
	i := h >> (64 - stencilSeenBits) &^ 1
	s0, s1 := &c.seen[i], &c.seen[i+1]
	for _, s := range []*stencilSeen{s0, s1} {
		if s.hash == h && s.frame >= now-1 {
			return true
		}
	}
	// The older place goes, or an empty one, as every place is as old before the first frame.
	if s1.frame < s0.frame || s1.hash == 0 {
		s0 = s1
	}
	s0.hash, s0.frame = h, now
	return false
}

func (c *stencilCache) lookup(h uint64, key []uint32) *stencilEntry {
	for e := c.entries[h]; e != nil; e = e.next {
		if slices.Equal(e.key, key) {
			return e
		}
	}
	return nil
}

// alloc allocates a region of size s at time now, evicting pages if needed. It returns nil if
// only the pages this flush uses could make room.
func (c *stencilCache) alloc(now int64, s image.Point) *stencilEntry {
	for _, pg := range c.pages {
		if r, ok := pg.alloc(s); ok {
			return c.newEntry(pg, r)
		}
	}
	for {
		// A page no flush of this or the last frame used goes first: its paths are likely gone,
		// and reusing it keeps the cache small while the paths keep changing.
		victim := c.leastRecentlyUsed(func(pg *stencilPage) bool { return pg.frame < now-1 })
		if victim == nil {
			if pg := c.newPage(s); pg != nil {
				r, _ := pg.alloc(s)
				return c.newEntry(pg, r)
			}
			// A page the previous flushes of this frame used may go: their draws reading it are
			// queued before the clear.
			victim = c.leastRecentlyUsed(func(pg *stencilPage) bool { return pg.flush != c.flush })
			if victim == nil {
				return nil
			}
		}
		if s.X <= victim.size.X && s.Y <= victim.size.Y {
			c.evict(victim)
			clearStencilImage(victim.img)
			r, _ := victim.alloc(s)
			return c.newEntry(victim, r)
		}
		c.remove(victim)
	}
}

func (c *stencilCache) leastRecentlyUsed(ok func(pg *stencilPage) bool) *stencilPage {
	var victim *stencilPage
	for _, pg := range c.pages {
		if ok(pg) && (victim == nil || pg.flush < victim.flush) {
			victim = pg
		}
	}
	return victim
}

// newPage allocates a page that can hold a region of size s, or returns nil if the cache has no
// room for it.
func (c *stencilCache) newPage(s image.Point) *stencilPage {
	size := s
	if s.X <= stencilPageMaxSize && s.Y <= stencilPageMaxSize {
		fit := max(stencilPageMinSize, recent.NextPowerOf2(max(s.X, s.Y)))
		n := fit
		for _, pg := range c.pages {
			if pg.size.X <= stencilPageMaxSize && pg.size.X == pg.size.Y {
				n = max(n, min(stencilPageMaxSize, 2*pg.size.X))
			}
		}
		// A smaller page than the growth wants may still fit in.
		if c.bytes.Load()+4*int64(n)*int64(n) > stencilCacheMaxBytes {
			n = fit
		}
		size = image.Pt(n, n)
	}
	bytes := 4 * int64(size.X) * int64(size.Y)
	if c.bytes.Load()+bytes > stencilCacheMaxBytes {
		return nil
	}
	pg := &stencilPage{
		img:  ggfx.NewImageWithOptions(image.Rectangle{Max: size}, &ggfx.NewImageOptions{Unmanaged: true}),
		size: size,
	}
	c.pages = append(c.pages, pg)
	c.bytes.Add(bytes)
	return pg
}

func (c *stencilCache) newEntry(pg *stencilPage, r image.Rectangle) *stencilEntry {
	var e *stencilEntry
	if n := len(c.free); n > 0 {
		e = c.free[n-1]
		c.free[n-1] = nil
		c.free = c.free[:n-1]
	} else {
		e = &stencilEntry{}
	}
	e.page = pg
	e.region = r
	pg.entries = append(pg.entries, e)
	return e
}

// evict drops the entries of pg and empties it. The caller clears or deallocates its image.
func (c *stencilCache) evict(pg *stencilPage) {
	for _, e := range pg.entries {
		c.unlink(e)
		e.page = nil
		e.next = nil
		c.free = append(c.free, e)
	}
	clear(pg.entries)
	pg.entries = pg.entries[:0]
	pg.shelves = pg.shelves[:0]
	pg.bottom = 0
}

func (c *stencilCache) unlink(e *stencilEntry) {
	head := c.entries[e.hash]
	if head == e {
		if e.next == nil {
			delete(c.entries, e.hash)
		} else {
			c.entries[e.hash] = e.next
		}
		return
	}
	for q := head; q != nil; q = q.next {
		if q.next == e {
			q.next = e.next
			return
		}
	}
}

// remove evicts pg and deallocates it.
func (c *stencilCache) remove(pg *stencilPage) {
	c.evict(pg)
	pg.img.Deallocate()
	c.bytes.Add(-4 * int64(pg.size.X) * int64(pg.size.Y))
	i := slices.Index(c.pages, pg)
	c.pages = slices.Delete(c.pages, i, i+1)
}

// expire removes the pages no fill used in the last 2*recent.Window frames, so the cache gives its
// memory back when the paths are gone, as theAtlas does.
func (c *stencilCache) expire(now int64) {
	for i := len(c.pages) - 1; i >= 0; i-- {
		if pg := c.pages[i]; now-pg.frame > 2*recent.Window {
			c.remove(pg)
		}
	}
}

// stencilSweepInterval is how many frames apart sweepStencils looks at the stencil images.
const stencilSweepInterval = recent.Window / 4

func init() {
	render.AddFrameSweeper(sweepStencils)
}

// sweepStencils expires the stencil cache's pages and theAtlas's images at the end of a frame at
// time now, by the rules a fill expires them by, so that they go once the frames stop filling
// rather than at the next fill, which may never come. Every stencilSweepInterval frames is soon
// enough for windows of recent.Window frames. A frame sweeper is never called under theFillPathM:
// a fill holds it, and nothing a fill calls ends a frame.
func sweepStencils(now int64) {
	if now%stencilSweepInterval != 0 || StencilBytes() == 0 {
		return
	}
	theFillPathM.Lock()
	defer theFillPathM.Unlock()
	theStencilCache.expire(now)
	theAtlas.expire(now)
}

// dispose removes every page, and forgets the keys seen.
func (c *stencilCache) dispose() {
	c.seen = [len(c.seen)]stencilSeen{}
	for len(c.pages) > 0 {
		c.remove(c.pages[len(c.pages)-1])
	}
}

// alloc allocates a region of size s on the page, on the lowest shelf not much taller than s, or
// on a new shelf, or on any shelf as tall as s.
func (p *stencilPage) alloc(s image.Point) (image.Rectangle, bool) {
	if s.X > p.size.X || s.Y > p.size.Y {
		return image.Rectangle{}, false
	}
	best := -1
	for i, sh := range p.shelves {
		if sh.h < s.Y || sh.h > s.Y+s.Y/2 || sh.x+s.X > p.size.X {
			continue
		}
		if best < 0 || sh.h < p.shelves[best].h {
			best = i
		}
	}
	if best < 0 && p.bottom+s.Y <= p.size.Y {
		p.shelves = append(p.shelves, stencilShelf{y: p.bottom, h: s.Y})
		p.bottom += s.Y
		best = len(p.shelves) - 1
	}
	if best < 0 {
		for i, sh := range p.shelves {
			if sh.h >= s.Y && sh.x+s.X <= p.size.X && (best < 0 || sh.h < p.shelves[best].h) {
				best = i
			}
		}
	}
	if best < 0 {
		return image.Rectangle{}, false
	}
	sh := &p.shelves[best]
	r := image.Rect(sh.x, sh.y, sh.x+s.X, sh.y+s.Y)
	sh.x += s.X
	return r, true
}

// appendStencilKey appends the key of the stencil buffers of p, whose rendering bounds are rb: what
// appendStencilTriangles reads of p, relative to rb.Min, the size of rb and antialias. It returns
// the key with its hash, computed in the same walk, where a point's two words are hashed as one,
// which halves the steps.
func appendStencilKey(key []uint32, p *Path, rb image.Rectangle, antialias bool) ([]uint32, uint64) {
	const prime = 0x9e3779b97f4a7c15
	var aa uint32
	if antialias {
		aa = 1
	}
	key = append(key, uint32(rb.Dx()), uint32(rb.Dy()), aa)
	h := (uint64(rb.Dx())<<32 | uint64(rb.Dy())) * prime
	h = (h ^ uint64(aa)) * prime
	ox, oy := float32(rb.Min.X), float32(rb.Min.Y)
	for i := range p.subPaths {
		subPath := &p.subPaths[i]
		if !subPath.isValid() {
			continue
		}
		x, y := math.Float32bits(subPath.start.x-ox), math.Float32bits(subPath.start.y-oy)
		key = append(key, uint32(len(subPath.ops)), x, y)
		h = (h ^ uint64(len(subPath.ops))) * prime
		h = (h ^ (uint64(x)<<32 | uint64(y))) * prime
		for _, op := range subPath.ops {
			x, y := math.Float32bits(op.p1.x-ox), math.Float32bits(op.p1.y-oy)
			key = append(key, uint32(op.typ), x, y)
			h += uint64(op.typ)
			h = (h ^ (uint64(x)<<32 | uint64(y))) * prime
			if op.typ == opTypeQuadTo {
				x, y := math.Float32bits(op.p2.x-ox), math.Float32bits(op.p2.y-oy)
				key = append(key, x, y)
				h = (h ^ (uint64(x)<<32 | uint64(y))) * prime
			}
		}
	}
	// The finalizer of MurmurHash3, as the steps above leave the low bits of a point's word in few
	// bits of h, and admit indexes by the top bits.
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	h *= 0xc4ceb9fe1a85ec53
	h ^= h >> 33
	return key, h
}
