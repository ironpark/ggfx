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

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"bytes"
	"image"
	"testing"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
)

func TestSlotAlignmentAndGrowth(t *testing.T) {
	var s slot
	s.init()

	if uintptr(unsafe.Pointer(&s.cmds[0]))%8 != 0 {
		t.Fatalf("initial cmds not 8B aligned")
	}
	if s.cmdCapBytes() != minCmdBytes {
		t.Fatalf("initial cmd cap = %d, want %d", s.cmdCapBytes(), minCmdBytes)
	}
	if len(s.payRaw) != 0 || len(s.payload) != 0 {
		t.Fatalf("expected lazy payload allocation on init, got payRaw=%d payload=%d", len(s.payRaw), len(s.payload))
	}

	// First heap payload write lazily allocates minPayloadBytes with 16B alignment.
	if off, ok := s.appendPayload([]byte{1, 2, 3, 4}); !ok || off != 0 {
		t.Fatalf("initial lazy appendPayload = (%d, %v), want (0, true)", off, ok)
	}
	if uintptr(unsafe.Pointer(&s.payload[0]))%16 != 0 {
		t.Fatalf("initial payload not 16B aligned")
	}
	if len(s.payload) != minPayloadBytes {
		t.Fatalf("initial payload cap = %d, want %d", len(s.payload), minPayloadBytes)
	}
	s.reset()

	// Grow cmds past minCmdBytes.
	for i := range 2000 {
		s.encodeNewImage(uint32(i+1), 16, 16, 16, 16, uint16(PixelFormatRGBA8))
	}
	if s.cmdLen <= minCmdBytes {
		t.Fatalf("expected cmdLen > %d, got %d", minCmdBytes, s.cmdLen)
	}
	if uintptr(unsafe.Pointer(&s.cmds[0]))%8 != 0 {
		t.Fatalf("grown cmds not 8B aligned")
	}

	// Grow payload past minPayloadBytes with odd sizes and check 16B alignment of every offset.
	chunk := bytes.Repeat([]byte{0xAB}, 70*1024+3)
	for range 5 {
		off, ok := s.appendPayload(chunk)
		if !ok {
			t.Fatalf("appendPayload failed unexpectedly")
		}
		if off%16 != 0 {
			t.Fatalf("payload offset %d is not 16B aligned", off)
		}
		if !bytes.Equal(s.payload[off:int(off)+len(chunk)], chunk) {
			t.Fatalf("payload content mismatch at offset %d", off)
		}
	}
	if uintptr(unsafe.Pointer(&s.payload[0]))%16 != 0 {
		t.Fatalf("grown payload base not 16B aligned")
	}
}

func TestSlotUniformDeduplication(t *testing.T) {
	var s slot
	s.init()

	uniforms := make([]uint32, InternalUniformDwords+4)
	for i := range uniforms {
		uniforms[i] = uint32(0x100 + i)
	}
	regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 16, 16), IndexCount: 6}}
	v1 := graphicsdriver.UniformVersions{Internal: 10, User: 20}

	if !s.encodeDrawTriangles(1, 2, [MaxSrcImages]uint32{3}, 0, Blend{}, regions, uniforms, v1, 4) {
		t.Fatal("first encodeDrawTriangles failed")
	}
	payAfterFirst := s.payLen
	if payAfterFirst != int(InternalUniformBytes)+16 {
		t.Fatalf("payLen after first draw = %d, want %d", payAfterFirst, InternalUniformBytes+16)
	}

	// Same internal and user versions: payload must not grow.
	if !s.encodeDrawTriangles(1, 2, [MaxSrcImages]uint32{3}, 6, Blend{}, regions, uniforms, v1, 4) {
		t.Fatal("second encodeDrawTriangles failed")
	}
	if s.payLen != payAfterFirst {
		t.Fatalf("payLen grew on duplicate versions: got %d, want %d", s.payLen, payAfterFirst)
	}

	// Verify both recorded CmdDrawTriangles reference identical offsets.
	cmdBytes := s.cmdBytes()
	const recSz = int(unsafe.Sizeof(CmdDrawTriangles{}) + unsafe.Sizeof(DrawRegion{}))
	c0 := (*CmdDrawTriangles)(unsafe.Pointer(&cmdBytes[0]))
	c1 := (*CmdDrawTriangles)(unsafe.Pointer(&cmdBytes[recSz]))
	if c0.InternalOffset != c1.InternalOffset || c0.UserOffset != c1.UserOffset {
		t.Fatalf("deduplicated offsets differ: c0=(%d,%d) c1=(%d,%d)",
			c0.InternalOffset, c0.UserOffset, c1.InternalOffset, c1.UserOffset)
	}

	// Change only user version: internal stays deduplicated, user appends 16B.
	v2 := graphicsdriver.UniformVersions{Internal: 10, User: 21}
	if !s.encodeDrawTriangles(1, 2, [MaxSrcImages]uint32{3}, 12, Blend{}, regions, uniforms, v2, 4) {
		t.Fatal("third encodeDrawTriangles failed")
	}
	c2 := (*CmdDrawTriangles)(unsafe.Pointer(&cmdBytes[2*recSz]))
	if c2.InternalOffset != c0.InternalOffset {
		t.Fatalf("internal offset changed when version was unchanged: %d != %d", c2.InternalOffset, c0.InternalOffset)
	}
	if c2.UserOffset == c0.UserOffset {
		t.Fatalf("user offset did not change when user version changed")
	}
	if c2.UserOffset%16 != 0 {
		t.Fatalf("user offset %d not 16B aligned", c2.UserOffset)
	}

	// After reset(), dedup state is cleared so same version writes again at offset 0.
	s.reset()
	if !s.encodeDrawTriangles(1, 2, [MaxSrcImages]uint32{3}, 0, Blend{}, regions, uniforms, v1, 4) {
		t.Fatal("encodeDrawTriangles after reset failed")
	}
	c3 := (*CmdDrawTriangles)(unsafe.Pointer(&s.cmdBytes()[0]))
	if c3.InternalOffset != 0 {
		t.Fatalf("internal offset after reset = %d, want 0", c3.InternalOffset)
	}
}

func TestSlotPinsAndReset(t *testing.T) {
	var s slot
	s.init()
	defer s.reset()

	vs := []float32{1, 2, 3, 4}
	is := []uint32{0, 1, 2}
	s.encodeSetVertices(vs, is)

	if s.pinCount != 2 {
		t.Fatalf("expected 2 pinned vertex/index slices, got %d", s.pinCount)
	}
	cmd := (*CmdSetVertices)(unsafe.Pointer(&s.cmdBytes()[0]))
	if cmd.Vertices != uintptr(unsafe.Pointer(&vs[0])) || cmd.Indices != uintptr(unsafe.Pointer(&is[0])) {
		t.Fatalf("SetVertices raw pointers mismatch")
	}

	prevSeq := s.seq
	s.reset()
	if s.cmdLen != 0 || s.cmdCount != 0 || s.payLen != 0 {
		t.Fatalf("slot not cleared after reset")
	}
	if s.pinCount != 0 {
		t.Fatalf("pins not cleared after reset: %d", s.pinCount)
	}
	if s.seq != prevSeq+1 {
		t.Fatalf("seq did not increment on reset: got %d, want %d", s.seq, prevSeq+1)
	}
}

func TestSlotPayloadOverflowLeavesSlotUnmodified(t *testing.T) {
	var s slot
	s.init()
	s.maxPayload = 128

	pix := make([]byte, 80)
	args := []graphicsdriver.PixelsArgs{{Pixels: pix, Region: image.Rect(0, 0, 5, 4)}}
	if !s.encodeWritePixels(1, 4, args) {
		t.Fatal("first WritePixels should fit")
	}
	savedCmdLen := s.cmdLen
	savedCmdCount := s.cmdCount
	savedPayLen := s.payLen

	// Second WritePixels would need align16(80)+80 = 160 > 128 -> must fail cleanly.
	if s.encodeWritePixels(1, 4, args) {
		t.Fatal("second WritePixels should overflow")
	}
	if s.cmdLen != savedCmdLen || s.cmdCount != savedCmdCount || s.payLen != savedPayLen {
		t.Fatalf("slot mutated on overflow: got (%d,%d,%d), want (%d,%d,%d)",
			s.cmdLen, s.cmdCount, s.payLen, savedCmdLen, savedCmdCount, savedPayLen)
	}
}

func TestSlotSetDamageEncoding(t *testing.T) {
	var s slot
	s.init()

	// Full damage (nil).
	s.encodeSetDamage(7, nil)
	cFull := (*CmdSetDamage)(unsafe.Pointer(&s.cmdBytes()[0]))
	if cFull.Hdr.Op != OpSetDamage || cFull.Hdr.Size != 16 || cFull.SurfaceID != 7 || cFull.Count != DamageFull {
		t.Fatalf("unexpected full damage record: %+v", *cFull)
	}

	// Explicit rects.
	off := s.cmdLen
	s.encodeSetDamage(7, []image.Rectangle{image.Rect(1, 2, 3, 4), image.Rect(5, 6, 7, 8)})
	cRects := (*CmdSetDamage)(unsafe.Pointer(&s.cmdBytes()[off]))
	if cRects.Hdr.Size != 16+2*16 || cRects.Count != 2 {
		t.Fatalf("unexpected explicit damage record: %+v", *cRects)
	}

	// Empty (non-nil) damage.
	off2 := s.cmdLen
	s.encodeSetDamage(7, []image.Rectangle{})
	cEmpty := (*CmdSetDamage)(unsafe.Pointer(&s.cmdBytes()[off2]))
	if cEmpty.Hdr.Size != 16 || cEmpty.Count != 0 {
		t.Fatalf("unexpected empty damage record: %+v", *cEmpty)
	}
}

func TestRingSlotsAndShrink(t *testing.T) {
	var r2 ring
	r2.init(2)
	if r2.n != 2 {
		t.Fatalf("ring(2).n = %d, want 2", r2.n)
	}
	s0 := r2.cur()
	r2.advance()
	s1 := r2.cur()
	r2.advance()
	if s0 == s1 || r2.cur() != s0 {
		t.Fatalf("2-slot ring did not wrap properly")
	}

	var r3 ring
	r3.init(3)
	if r3.n != 3 {
		t.Fatalf("ring(3).n = %d, want 3", r3.n)
	}
	seen := map[*slot]bool{}
	for range 3 {
		seen[r3.cur()] = true
		r3.advance()
	}
	if len(seen) != 3 {
		t.Fatalf("3-slot ring visited %d distinct slots, want 3", len(seen))
	}

	// Grow slot capacities well above minimum, then advance frame clock past 2*Window and shrink.
	s := r3.cur()
	bigChunk := make([]byte, 1<<20) // 1 MiB
	if _, ok := s.appendPayload(bigChunk); !ok {
		t.Fatal("appendPayload 1 MiB failed")
	}
	for i := range 4000 {
		s.encodeNewImage(uint32(i+1), 8, 8, 16, 16, uint16(PixelFormatRGBA8))
	}
	grownCmdCap := s.cmdCapBytes()
	grownPayCap := len(s.payload)
	if grownCmdCap <= minCmdBytes*2 || grownPayCap <= minPayloadBytes*2 {
		t.Fatalf("expected grown capacities > 2x min, got cmd=%d pay=%d", grownCmdCap, grownPayCap)
	}

	s.reset()
	s.shrink(grownCmdCap, grownPayCap)
	if s.cmdCapBytes() != grownCmdCap || len(s.payload) != grownPayCap {
		t.Fatalf("slot shrunk prematurely while peak was still high")
	}

	for range 2*recent.Window + 1 {
		recent.Advance()
	}
	s.shrink(64, 128)
	if s.cmdCapBytes() != minCmdBytes {
		t.Fatalf("cmdCap after idle window = %d, want %d", s.cmdCapBytes(), minCmdBytes)
	}
	if len(s.payload) != minPayloadBytes {
		t.Fatalf("payload cap after idle window = %d, want %d", len(s.payload), minPayloadBytes)
	}
	if uintptr(unsafe.Pointer(&s.payload[0]))%16 != 0 {
		t.Fatalf("shrunk payload not 16B aligned")
	}
}

func TestSlotSharedUploadArena(t *testing.T) {
	_, backing := allocAligned16(2048)

	var sh sharedUploadArena
	ok := sh.set(UploadArena{
		StructSize: uint32(unsafe.Sizeof(UploadArena{})),
		ArenaID:    1,
		Ptr:        uintptr(unsafe.Pointer(&backing[0])),
		Capacity:   uint64(len(backing)),
		Alignment:  64,
		Flags:      ArenaFlagGPUMapped | ArenaFlagCoherent,
	})
	if !ok {
		t.Fatal("sharedUploadArena.set failed")
	}

	var s slot
	s.init()
	s.shared = &sh

	// Batch 1: multi-region WritePixels uses shared arena directly with 64B alignment and zero heap allocation.
	p0 := bytes.Repeat([]byte{0x11}, 20)
	p1 := bytes.Repeat([]byte{0x22}, 36)
	args := []graphicsdriver.PixelsArgs{
		{Pixels: p0, Region: image.Rect(0, 0, 5, 1)},
		{Pixels: p1, Region: image.Rect(0, 1, 9, 2)},
	}
	if !s.encodeWritePixels(1, 4, args) {
		t.Fatal("encodeWritePixels in shared arena failed")
	}
	if !s.usingShared {
		t.Fatal("expected s.usingShared == true")
	}
	if len(s.payRaw) != 0 || len(s.payload) != 0 {
		t.Fatalf("shared arena path unexpectedly allocated Go heap payload: raw=%d pay=%d", len(s.payRaw), len(s.payload))
	}

	var b Batch
	s.fillBatch(&b)
	if b.Payload != uintptr(unsafe.Pointer(&sh.buf[0])) {
		t.Fatalf("batch 1 Payload = %#x, want %#x", b.Payload, uintptr(unsafe.Pointer(&sh.buf[0])))
	}
	// p0 at [0:20], aligned to 64 -> p1 at [64:100] -> PayloadLen == 100.
	if b.PayloadLen != 100 {
		t.Fatalf("batch 1 PayloadLen = %d, want 100", b.PayloadLen)
	}
	if !bytes.Equal(sh.buf[0:20], p0) || !bytes.Equal(sh.buf[64:100], p1) {
		t.Fatal("shared arena pixel payload mismatch")
	}

	sh.advanceBatch()
	s.reset()
	if sh.batchStart != 128 || sh.cursor != 128 {
		t.Fatalf("after advanceBatch: batchStart=%d cursor=%d, want 128,128", sh.batchStart, sh.cursor)
	}

	// Batch 2: DrawTriangles with uniform dedup starts at batchStart=128 with relative offset 0.
	uniforms := make([]uint32, InternalUniformDwords+4)
	for i := range uniforms {
		uniforms[i] = uint32(0x200 + i)
	}
	regions := []graphicsdriver.DstRegion{{Region: image.Rect(0, 0, 16, 16), IndexCount: 6}}
	v1 := graphicsdriver.UniformVersions{Internal: 1, User: 2}
	if !s.encodeDrawTriangles(1, 2, [MaxSrcImages]uint32{3}, 0, Blend{}, regions, uniforms, v1, 4) {
		t.Fatal("first shared DrawTriangles failed")
	}
	cursorAfterFirst := sh.cursor
	if !s.encodeDrawTriangles(1, 2, [MaxSrcImages]uint32{3}, 6, Blend{}, regions, uniforms, v1, 4) {
		t.Fatal("second deduplicated shared DrawTriangles failed")
	}
	if sh.cursor != cursorAfterFirst {
		t.Fatalf("shared cursor grew on deduplicated draw: %d != %d", sh.cursor, cursorAfterFirst)
	}
	s.fillBatch(&b)
	if b.Payload != uintptr(unsafe.Pointer(&sh.buf[128])) {
		t.Fatalf("batch 2 Payload = %#x, want %#x", b.Payload, uintptr(unsafe.Pointer(&sh.buf[128])))
	}
	// InternalUniformBytes (288) aligned to 64 -> 320, plus 16 user bytes -> 336.
	if b.PayloadLen != 336 {
		t.Fatalf("batch 2 PayloadLen = %d, want 336", b.PayloadLen)
	}
	cmd0 := (*CmdDrawTriangles)(unsafe.Pointer(&s.cmdBytes()[0]))
	if cmd0.InternalOffset != 0 || cmd0.UserOffset != 320 {
		t.Fatalf("shared draw relative offsets = (%d, %d), want (0, 320)", cmd0.InternalOffset, cmd0.UserOffset)
	}

	// Multi-region pre-flight check: if total exceeds remaining shared space, nothing is mutated.
	savedCmdLen, savedCmdCount, savedCursor := s.cmdLen, s.cmdCount, sh.cursor
	big := make([]byte, 900)
	overflowArgs := []graphicsdriver.PixelsArgs{
		{Pixels: big, Region: image.Rect(0, 0, 225, 1)},
		{Pixels: big, Region: image.Rect(0, 1, 225, 2)},
	}
	if s.encodeWritePixels(1, 4, overflowArgs) {
		t.Fatal("expected multi-region WritePixels to overflow shared arena")
	}
	if s.cmdLen != savedCmdLen || s.cmdCount != savedCmdCount || sh.cursor != savedCursor {
		t.Fatalf("slot/shared mutated on overflow: cmdLen=%d/%d cmdCount=%d/%d cursor=%d/%d",
			s.cmdLen, savedCmdLen, s.cmdCount, savedCmdCount, sh.cursor, savedCursor)
	}

	sh.advanceBatch()
	s.reset()

	// Single-batch Go heap fallback when rotation fails: shared cursor stays untouched,
	// and reset() restores shared arena mode on the following batch.
	cursorBeforeFallback := sh.cursor
	s.forceHeap = true
	if !s.encodeWritePixels(1, 4, []graphicsdriver.PixelsArgs{{Pixels: p0, Region: image.Rect(0, 0, 5, 1)}}) {
		t.Fatal("heap fallback encodeWritePixels failed")
	}
	if s.usingShared {
		t.Fatal("expected s.usingShared == false during forceHeap fallback")
	}
	if sh.cursor != cursorBeforeFallback {
		t.Fatalf("shared cursor advanced during heap fallback: %d != %d", sh.cursor, cursorBeforeFallback)
	}
	s.fillBatch(&b)
	if b.Payload != uintptr(unsafe.Pointer(&s.payload[0])) || b.PayloadLen != 20 {
		t.Fatalf("heap fallback batch mismatch: ptr=%#x len=%d", b.Payload, b.PayloadLen)
	}

	s.reset()
	if !s.useShared() {
		t.Fatal("expected slot to resume shared arena mode after reset()")
	}
}

func TestSlotSharedUploadArenaPitchedWritePixels(t *testing.T) {
	_, backing := allocAligned16(4096)

	var sh sharedUploadArena
	ok := sh.set(UploadArena{
		StructSize:        uint32(unsafe.Sizeof(UploadArena{})),
		ArenaID:           1,
		Ptr:               uintptr(unsafe.Pointer(&backing[0])),
		Capacity:          uint64(len(backing)),
		Alignment:         512,
		Flags:             ArenaFlagGPUMapped | ArenaFlagCoherent,
		RowPitchAlignment: 256,
	})
	if !ok {
		t.Fatal("sharedUploadArena.set failed")
	}

	var s slot
	s.init()
	s.shared = &sh

	// 5x3 RGBA8: rowBytes=20, pitched row stride=256 -> payloadLen = 2*256 + 20 = 532.
	const w, h, bpp = 5, 3, 4
	pix := make([]byte, w*h*bpp)
	for i := range pix {
		pix[i] = byte(i + 1)
	}
	args := []graphicsdriver.PixelsArgs{
		{Pixels: pix, Region: image.Rect(0, 0, w, h)},
	}
	if !s.encodeWritePixels(1, bpp, args) {
		t.Fatal("pitched encodeWritePixels failed")
	}
	if !s.usingShared {
		t.Fatal("expected s.usingShared == true")
	}

	cmd := (*CmdWritePixels)(unsafe.Pointer(&s.cmdBytes()[0]))
	if cmd.Hdr.Op != OpWritePixels || cmd.Hdr.Aux != 256 {
		t.Fatalf("CmdWritePixels header = %+v, want Op=%d Aux=256", cmd.Hdr, OpWritePixels)
	}
	reg := (*PixelsRegion)(unsafe.Pointer(&s.cmdBytes()[unsafe.Sizeof(CmdWritePixels{})]))
	const wantLen = 2*256 + w*bpp
	if reg.PayloadOffset != 0 || reg.PayloadLen != wantLen {
		t.Fatalf("PixelsRegion = (%d, %d), want (0, %d)", reg.PayloadOffset, reg.PayloadLen, wantLen)
	}
	for y := range h {
		wantRow := pix[y*w*bpp : (y+1)*w*bpp]
		gotRow := sh.buf[y*256 : y*256+w*bpp]
		if !bytes.Equal(gotRow, wantRow) {
			t.Fatalf("row %d mismatch: got %v, want %v", y, gotRow, wantRow)
		}
	}

	// Verify writePixelsNeed matches actual cursor advancement for multi-region pitched writes.
	sh.advanceBatch()
	s.reset()
	need, needOK := sh.writePixelsNeed([]graphicsdriver.PixelsArgs{
		{Pixels: pix, Region: image.Rect(0, 0, w, h)},
		{Pixels: pix, Region: image.Rect(0, h, w, 2*h)},
	}, bpp)
	// Region 0: 532B, aligned to 512 -> 1024; Region 1: 532B -> total 1556B.
	if !needOK || need != 1024+wantLen {
		t.Fatalf("writePixelsNeed = (%d, %v), want (%d, true)", need, needOK, 1024+wantLen)
	}

	// Overflow pre-flight leaves slot and cursor untouched.
	sh.batchStart = len(sh.buf) - 512
	sh.cursor = sh.batchStart
	savedCmdLen, savedCmdCount, savedCursor := s.cmdLen, s.cmdCount, sh.cursor
	if s.encodeWritePixels(1, bpp, args) {
		t.Fatal("expected pitched WritePixels to overflow remaining 512B")
	}
	if s.cmdLen != savedCmdLen || s.cmdCount != savedCmdCount || sh.cursor != savedCursor {
		t.Fatalf("slot mutated on pitched overflow")
	}

	// Heap fallback must encode tightly packed rows with Aux == 0.
	s.forceHeap = true
	if !s.encodeWritePixels(1, bpp, args) {
		t.Fatal("heap fallback encodeWritePixels failed")
	}
	heapCmd := (*CmdWritePixels)(unsafe.Pointer(&s.cmdBytes()[0]))
	if heapCmd.Hdr.Aux != 0 {
		t.Fatalf("heap fallback Aux = %d, want 0", heapCmd.Hdr.Aux)
	}
	heapReg := (*PixelsRegion)(unsafe.Pointer(&s.cmdBytes()[unsafe.Sizeof(CmdWritePixels{})]))
	if heapReg.PayloadLen != uint32(len(pix)) {
		t.Fatalf("heap fallback PayloadLen = %d, want %d", heapReg.PayloadLen, len(pix))
	}
}
