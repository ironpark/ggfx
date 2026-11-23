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
	"image"
	"math"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
)

const (
	minCmdBytes     = 16 << 10             // 16 KiB
	minPayloadBytes = 256 << 10            // 256 KiB
	maxPayloadBytes = math.MaxUint32 &^ 15 // 16B-aligned uint32 payload upper bound
)

func alignUp(n, align int) int {
	return (n + align - 1) &^ (align - 1)
}

func align16Up(n int) int {
	return alignUp(n, 16)
}

func nativeBytes(ptr uintptr, n int) []byte {
	base := *(*unsafe.Pointer)(unsafe.Pointer(&ptr))
	return unsafe.Slice((*byte)(base), n)
}

func allocAligned16(size int) (raw, aligned []byte) {
	if size <= 0 {
		return nil, nil
	}
	raw = make([]byte, size+15)
	addr := uintptr(unsafe.Pointer(&raw[0]))
	shift := int((16 - addr%16) % 16)
	aligned = raw[shift : shift+size]
	return raw, aligned
}

// sharedUploadArena tracks a driver-owned GPU-mapped upload buffer shared across
// batches on the render thread. Each submitted batch occupies a monotonic slice
// [batchStart:cursor] so subsequent batches never overwrite bytes already handed
// to the GPU until the backend rotates in an idle buffer via acquireUploadArena.
type sharedUploadArena struct {
	enabled    bool
	arenaID    uint32
	buf        []byte
	align      int // >= 16, power of 2
	batchStart int
	cursor     int
	flags      uint32
}

func (a *sharedUploadArena) set(ua UploadArena) bool {
	if ua.Ptr == 0 || ua.Capacity == 0 || ua.Ptr%16 != 0 {
		return false
	}
	align := int(ua.Alignment)
	if align < 16 {
		align = 16
	}
	if align&(align-1) != 0 {
		align = recent.NextPowerOf2(align)
	}
	capBytes := ua.Capacity
	if capBytes > uint64(maxPayloadBytes) {
		capBytes = uint64(maxPayloadBytes)
	}
	a.buf = nativeBytes(ua.Ptr, int(capBytes))
	a.arenaID = ua.ArenaID
	a.align = align
	a.batchStart = 0
	a.cursor = 0
	a.flags = ua.Flags
	a.enabled = true
	return true
}

func (a *sharedUploadArena) clear() {
	a.enabled = false
	a.arenaID = 0
	a.buf = nil
	a.align = 0
	a.batchStart = 0
	a.cursor = 0
	a.flags = 0
}

func (a *sharedUploadArena) alignment() int {
	if a.align >= 16 {
		return a.align
	}
	return 16
}

func (a *sharedUploadArena) advanceBatch() {
	if !a.enabled {
		return
	}
	next := alignUp(a.cursor, a.alignment())
	if next < a.cursor || next > len(a.buf) {
		next = len(a.buf)
	}
	a.batchStart = next
	a.cursor = next
}

// payloadCursor simulates payload reservations without writing anything, so
// an encoder can check that everything it needs fits before it emits a
// command. It applies the alignment and bounds rules appendPayload writes by.
type payloadCursor struct {
	pos   int // next free byte in the arena
	base  int // where this batch's payload starts; payload offsets are relative to it
	align int
	cap   int // bytes the arena can hold
	limit int // most payload bytes one batch may span
}

// reserve claims n bytes at the next aligned position and reports whether
// they fit. The cursor is left unchanged when they do not.
func (c *payloadCursor) reserve(n int) bool {
	off := alignUp(c.pos, c.align)
	if n < 0 || off < c.pos || off > c.cap || n > c.cap-off {
		return false
	}
	rel := off - c.base
	if rel < 0 || rel > c.limit || n > c.limit-rel {
		return false
	}
	c.pos = off + n
	return true
}

// arenaCursor simulates reservations against the bytes left in the arena
// alone, with no per-batch limit.
func (a *sharedUploadArena) arenaCursor() payloadCursor {
	return payloadCursor{pos: a.cursor, align: a.alignment(), cap: len(a.buf), limit: math.MaxInt}
}

func (a *sharedUploadArena) canFit(n uint64) bool {
	if !a.enabled || n > math.MaxInt {
		return false
	}
	c := a.arenaCursor()
	return c.reserve(int(n))
}

func (a *sharedUploadArena) canFitWritePixels(args []graphicsdriver.PixelsArgs) bool {
	if !a.enabled {
		return false
	}
	c := a.arenaCursor()
	for _, arg := range args {
		if !c.reserve(len(arg.Pixels)) {
			return false
		}
	}
	return true
}

func (a *sharedUploadArena) writePixelsNeed(args []graphicsdriver.PixelsArgs) (uint64, bool) {
	align := uint64(a.alignment())
	var total uint64
	for _, arg := range args {
		off := (total + align - 1) &^ (align - 1)
		if off < total {
			return 0, false
		}
		n := uint64(len(arg.Pixels))
		if off > math.MaxUint64-n {
			return 0, false
		}
		total = off + n
	}
	return total, true
}

func (a *sharedUploadArena) drawUniformsNeed(userDwords uint32) uint64 {
	total := uint64(InternalUniformBytes)
	if userDwords > 0 {
		align := uint64(a.alignment())
		off := (total + align - 1) &^ (align - 1)
		total = off + uint64(userDwords)*4
	}
	return total
}

// slot is one linear command + payload arena in the submission ring.
type slot struct {
	cmds     []uint64 // 8B-aligned backing store; byte capacity = len(cmds) * 8
	cmdLen   int      // bytes written into cmds (always a multiple of 8)
	cmdCount uint32

	shared      *sharedUploadArena
	usingShared bool // true while current batch's payload resides in shared.buf
	forceHeap   bool // true while current batch falls back to Go heap payload

	payRaw  []byte // cap + 15 raw backing allocation (lazily allocated)
	payload []byte // 16B-aligned view into payRaw
	payLen  int

	pinsF [][]float32 // retains borrowed vertex slices for GC during batch lifetime
	pinsU [][]uint32  // retains borrowed index slices for GC during batch lifetime

	lastInternalVer uint64
	lastUserVer     uint64
	lastInternalOff uint32
	lastUserOff     uint32

	frameMaxCmd int
	frameMaxPay int
	cmdPeak     recent.Peak
	payPeak     recent.Peak
	seq         uint64

	// maxPayload overrides maxPayloadBytes in tests to exercise overflow flushing.
	maxPayload int
}

func (s *slot) init() {
	s.cmds = make([]uint64, minCmdBytes/8)
	s.payRaw = nil
	s.payload = nil
	s.maxPayload = maxPayloadBytes
}

func (s *slot) useShared() bool {
	if s.shared == nil || !s.shared.enabled {
		return false
	}
	if s.usingShared {
		return true
	}
	return s.payLen == 0 && !s.forceHeap
}

func (s *slot) payloadLimit() int {
	if s.maxPayload > 0 {
		return s.maxPayload
	}
	return maxPayloadBytes
}

func (s *slot) cmdCapBytes() int {
	return len(s.cmds) * 8
}

func (s *slot) cmdBytes() []byte {
	if len(s.cmds) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s.cmds[0])), len(s.cmds)*8)
}

func (s *slot) appendCmd(size int) []byte {
	if size < int(unsafe.Sizeof(CmdHeader{})) || size%8 != 0 {
		panic("zigdriver: command record size must be a positive multiple of 8")
	}
	need := s.cmdLen + size
	if need > s.cmdCapBytes() {
		newCap := recent.NextPowerOf2(max(need, minCmdBytes))
		newCap = (newCap + 7) &^ 7
		newCmds := make([]uint64, newCap/8)
		copy(newCmds, s.cmds[:s.cmdLen/8])
		s.cmds = newCmds
	}
	dst := s.cmdBytes()[s.cmdLen : s.cmdLen+size]
	s.cmdLen += size
	s.cmdCount++
	return dst
}

// payloadCursor returns a cursor at the slot's current payload position, in
// the shared upload arena when the slot is using it and the heap arena otherwise.
func (s *slot) payloadCursor() payloadCursor {
	limit := s.payloadLimit()
	if s.useShared() {
		sh := s.shared
		return payloadCursor{pos: sh.cursor, base: sh.batchStart, align: sh.alignment(), cap: len(sh.buf), limit: limit}
	}
	return payloadCursor{pos: s.payLen, align: 16, cap: limit, limit: limit}
}

func (s *slot) canAppendPayload(n int) bool {
	c := s.payloadCursor()
	return c.reserve(n)
}

func (s *slot) appendPayload(b []byte) (uint32, bool) {
	if !s.canAppendPayload(len(b)) {
		return 0, false
	}
	if s.useShared() {
		sh := s.shared
		absOff := alignUp(sh.cursor, sh.alignment())
		end := absOff + len(b)
		if absOff > sh.cursor {
			clear(sh.buf[sh.cursor:absOff])
		}
		copy(sh.buf[absOff:end], b)
		sh.cursor = end
		s.usingShared = true
		s.payLen = end - sh.batchStart
		return uint32(absOff - sh.batchStart), true
	}
	off := align16Up(s.payLen)
	end := off + len(b)
	if end > len(s.payload) {
		limit := s.payloadLimit()
		newCap := recent.NextPowerOf2(max(end, minPayloadBytes))
		if newCap > limit {
			newCap = limit
		}
		raw, aligned := allocAligned16(newCap)
		copy(aligned, s.payload[:s.payLen])
		s.payRaw, s.payload = raw, aligned
	}
	if off > s.payLen {
		clear(s.payload[s.payLen:off])
	}
	copy(s.payload[off:end], b)
	s.payLen = end
	return uint32(off), true
}

func (s *slot) appendPayloadUint32s(u []uint32) (uint32, bool) {
	if len(u) == 0 {
		return s.appendPayload(nil)
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(u))), len(u)*4)
	return s.appendPayload(b)
}

func (s *slot) reset() {
	s.cmdLen = 0
	s.cmdCount = 0
	s.payLen = 0
	s.usingShared = false
	s.forceHeap = false
	clear(s.pinsF)
	s.pinsF = s.pinsF[:0]
	clear(s.pinsU)
	s.pinsU = s.pinsU[:0]
	s.lastInternalVer = 0
	s.lastUserVer = 0
	s.lastInternalOff = 0
	s.lastUserOff = 0
	s.seq++
}

func (s *slot) shrink(usedCmds, usedPay int) {
	curCmdCap := s.cmdCapBytes()
	wantCmdCap := s.cmdPeak.Capacity(curCmdCap, usedCmds, minCmdBytes)
	wantCmdCap = (wantCmdCap + 7) &^ 7
	if wantCmdCap < curCmdCap {
		s.cmds = make([]uint64, wantCmdCap/8)
	}

	curPayCap := len(s.payload)
	if curPayCap > 0 {
		wantPayCap := s.payPeak.Capacity(curPayCap, usedPay, minPayloadBytes)
		if limit := s.payloadLimit(); wantPayCap > limit {
			wantPayCap = limit
		}
		if wantPayCap < curPayCap {
			s.payRaw, s.payload = allocAligned16(wantPayCap)
		}
	}
}

func (s *slot) fillBatch(dst *Batch) {
	var cmdsPtr uintptr
	if s.cmdLen > 0 {
		cmdsPtr = uintptr(unsafe.Pointer(&s.cmds[0]))
	}
	var payPtr uintptr
	var payLen uint64
	if s.usingShared {
		sh := s.shared
		n := sh.cursor - sh.batchStart
		if n > 0 {
			payPtr = uintptr(unsafe.Pointer(&sh.buf[sh.batchStart]))
			payLen = uint64(n)
		}
	} else if s.payLen > 0 {
		payPtr = uintptr(unsafe.Pointer(&s.payload[0]))
		payLen = uint64(s.payLen)
	}
	*dst = Batch{
		StructSize: uint32(unsafe.Sizeof(Batch{})),
		CmdCount:   s.cmdCount,
		Cmds:       cmdsPtr,
		CmdsLen:    uint64(s.cmdLen),
		Payload:    payPtr,
		PayloadLen: payLen,
		Sequence:   s.seq,
	}
}

func (s *slot) encodeSetVertices(vertices []float32, indices []uint32) {
	var vPtr, iPtr uintptr
	if len(vertices) > 0 {
		vPtr = uintptr(unsafe.Pointer(&vertices[0]))
		s.pinsF = append(s.pinsF, vertices)
	}
	if len(indices) > 0 {
		iPtr = uintptr(unsafe.Pointer(&indices[0]))
		s.pinsU = append(s.pinsU, indices)
	}
	const sz = int(unsafe.Sizeof(CmdSetVertices{}))
	buf := s.appendCmd(sz)
	*(*CmdSetVertices)(unsafe.Pointer(&buf[0])) = CmdSetVertices{
		Hdr: CmdHeader{
			Op:   OpSetVertices,
			Size: uint32(sz),
		},
		Vertices:         vPtr,
		Indices:          iPtr,
		VertexFloatCount: uint32(len(vertices)),
		IndexCount:       uint32(len(indices)),
	}
}

func (s *slot) encodeNewImage(id, width, height, internalW, internalH uint32, format uint16) {
	const sz = int(unsafe.Sizeof(CmdNewImage{}))
	buf := s.appendCmd(sz)
	*(*CmdNewImage)(unsafe.Pointer(&buf[0])) = CmdNewImage{
		Hdr: CmdHeader{
			Op:   OpNewImage,
			Aux:  format,
			Size: uint32(sz),
		},
		ID:             id,
		Width:          width,
		Height:         height,
		InternalWidth:  internalW,
		InternalHeight: internalH,
	}
}

func (s *slot) encodeNewScreenImage(id, surfaceID, width, height uint32) {
	const sz = int(unsafe.Sizeof(CmdNewScreenImage{}))
	buf := s.appendCmd(sz)
	*(*CmdNewScreenImage)(unsafe.Pointer(&buf[0])) = CmdNewScreenImage{
		Hdr: CmdHeader{
			Op:   OpNewScreenImage,
			Size: uint32(sz),
		},
		ID:        id,
		SurfaceID: surfaceID,
		Width:     width,
		Height:    height,
	}
}

func (s *slot) encodeDisposeImage(id uint32) {
	const sz = int(unsafe.Sizeof(CmdDisposeImage{}))
	buf := s.appendCmd(sz)
	*(*CmdDisposeImage)(unsafe.Pointer(&buf[0])) = CmdDisposeImage{
		Hdr: CmdHeader{
			Op:   OpDisposeImage,
			Size: uint32(sz),
		},
		ID: id,
	}
}

func (s *slot) encodeDisposeShader(id uint32) {
	const sz = int(unsafe.Sizeof(CmdDisposeShader{}))
	buf := s.appendCmd(sz)
	*(*CmdDisposeShader)(unsafe.Pointer(&buf[0])) = CmdDisposeShader{
		Hdr: CmdHeader{
			Op:   OpDisposeShader,
			Size: uint32(sz),
		},
		ID: id,
	}
}

func (s *slot) encodeDisposeSurface(surfaceID uint32) {
	const sz = int(unsafe.Sizeof(CmdDisposeSurface{}))
	buf := s.appendCmd(sz)
	*(*CmdDisposeSurface)(unsafe.Pointer(&buf[0])) = CmdDisposeSurface{
		Hdr: CmdHeader{
			Op:   OpDisposeSurface,
			Size: uint32(sz),
		},
		SurfaceID: surfaceID,
	}
}

// encodeWritePixels writes OP_WRITE_PIXELS and copies all region pixel buffers into payload.
// Returns false without mutating the slot if payload capacity would exceed the slot limit.
func (s *slot) encodeWritePixels(imageID uint32, args []graphicsdriver.PixelsArgs) bool {
	if len(args) == 0 {
		return true
	}
	// Pre-flight payload fit check so partial state is never written on overflow.
	fit := s.payloadCursor()
	for _, a := range args {
		if !fit.reserve(len(a.Pixels)) {
			return false
		}
	}

	const hdrSz = int(unsafe.Sizeof(CmdWritePixels{}))
	const regSz = int(unsafe.Sizeof(PixelsRegion{}))
	totalSz := hdrSz + len(args)*regSz
	buf := s.appendCmd(totalSz)
	*(*CmdWritePixels)(unsafe.Pointer(&buf[0])) = CmdWritePixels{
		Hdr: CmdHeader{
			Op:   OpWritePixels,
			Size: uint32(totalSz),
		},
		ImageID: imageID,
		Count:   uint32(len(args)),
	}
	regs := unsafe.Slice((*PixelsRegion)(unsafe.Pointer(&buf[hdrSz])), len(args))
	for i, a := range args {
		off, ok := s.appendPayload(a.Pixels)
		if !ok {
			panic("zigdriver: payload pre-flight check failed unexpectedly")
		}
		regs[i] = PixelsRegion{
			Rect: Rect{
				X0: int32(a.Region.Min.X),
				Y0: int32(a.Region.Min.Y),
				X1: int32(a.Region.Max.X),
				Y1: int32(a.Region.Max.Y),
			},
			PayloadOffset: off,
			PayloadLen:    uint32(len(a.Pixels)),
		}
	}
	return true
}

// encodeDrawTriangles writes OP_DRAW_TRIANGLES with per-batch uniform payload deduplication.
// Returns false without mutating the slot if payload capacity would exceed the slot limit.
func (s *slot) encodeDrawTriangles(
	dstID, shaderID uint32,
	srcIDs [MaxSrcImages]uint32,
	indexOffset uint32,
	blend Blend,
	regions []graphicsdriver.DstRegion,
	uniforms []uint32,
	versions graphicsdriver.UniformVersions,
	userDwords uint32,
) bool {
	needInternal := s.lastInternalVer != versions.Internal
	needUser := userDwords > 0 && s.lastUserVer != versions.User

	// Pre-flight payload fit check.
	fit := s.payloadCursor()
	if needInternal && !fit.reserve(int(InternalUniformBytes)) {
		return false
	}
	if needUser && !fit.reserve(int(userDwords)*4) {
		return false
	}

	internalOff := s.lastInternalOff
	if needInternal {
		off, ok := s.appendPayloadUint32s(uniforms[:InternalUniformDwords])
		if !ok {
			panic("zigdriver: internal uniform payload pre-flight check failed")
		}
		internalOff = off
		s.lastInternalVer = versions.Internal
		s.lastInternalOff = off
	}

	var userOff uint32
	var userVer uint64
	if userDwords > 0 {
		userVer = versions.User
		if needUser {
			off, ok := s.appendPayloadUint32s(uniforms[InternalUniformDwords : InternalUniformDwords+userDwords])
			if !ok {
				panic("zigdriver: user uniform payload pre-flight check failed")
			}
			userOff = off
			s.lastUserVer = versions.User
			s.lastUserOff = off
		} else {
			userOff = s.lastUserOff
		}
	}

	const hdrSz = int(unsafe.Sizeof(CmdDrawTriangles{}))
	const regSz = int(unsafe.Sizeof(DrawRegion{}))
	totalSz := hdrSz + len(regions)*regSz
	buf := s.appendCmd(totalSz)
	*(*CmdDrawTriangles)(unsafe.Pointer(&buf[0])) = CmdDrawTriangles{
		Hdr: CmdHeader{
			Op:   OpDrawTriangles,
			Size: uint32(totalSz),
		},
		DstID:           dstID,
		ShaderID:        shaderID,
		SrcIDs:          srcIDs,
		IndexOffset:     indexOffset,
		RegionCount:     uint32(len(regions)),
		Blend:           blend,
		InternalVersion: versions.Internal,
		UserVersion:     userVer,
		InternalOffset:  internalOff,
		UserOffset:      userOff,
		UserDwords:      userDwords,
	}
	if len(regions) > 0 {
		regs := unsafe.Slice((*DrawRegion)(unsafe.Pointer(&buf[hdrSz])), len(regions))
		for i, r := range regions {
			regs[i] = DrawRegion{
				Rect: Rect{
					X0: int32(r.Region.Min.X),
					Y0: int32(r.Region.Min.Y),
					X1: int32(r.Region.Max.X),
					Y1: int32(r.Region.Max.Y),
				},
				IndexCount: uint32(r.IndexCount),
			}
		}
	}
	return true
}

func (s *slot) encodeSetDamage(surfaceID uint32, rects []image.Rectangle) {
	const hdrSz = int(unsafe.Sizeof(CmdSetDamage{}))
	if rects == nil {
		buf := s.appendCmd(hdrSz)
		*(*CmdSetDamage)(unsafe.Pointer(&buf[0])) = CmdSetDamage{
			Hdr: CmdHeader{
				Op:   OpSetDamage,
				Size: uint32(hdrSz),
			},
			SurfaceID: surfaceID,
			Count:     DamageFull,
		}
		return
	}

	const rectSz = int(unsafe.Sizeof(Rect{}))
	totalSz := hdrSz + len(rects)*rectSz
	buf := s.appendCmd(totalSz)
	*(*CmdSetDamage)(unsafe.Pointer(&buf[0])) = CmdSetDamage{
		Hdr: CmdHeader{
			Op:   OpSetDamage,
			Size: uint32(totalSz),
		},
		SurfaceID: surfaceID,
		Count:     uint32(len(rects)),
	}
	if len(rects) > 0 {
		dst := unsafe.Slice((*Rect)(unsafe.Pointer(&buf[hdrSz])), len(rects))
		for i, r := range rects {
			dst[i] = Rect{
				X0: int32(r.Min.X),
				Y0: int32(r.Min.Y),
				X1: int32(r.Max.X),
				Y1: int32(r.Max.Y),
			}
		}
	}
}

// ring manages 2 or 3 command arena slots.
type ring struct {
	slots   [3]slot
	n       int
	k       int
	nextSeq uint64
}

func (r *ring) init(n int) {
	if n != 3 {
		n = 2
	}
	r.n = n
	r.k = 0
	for i := range n {
		r.slots[i].init()
	}
}

func (r *ring) setShared(shared *sharedUploadArena) {
	for i := range r.n {
		r.slots[i].shared = shared
	}
}

func (r *ring) cur() *slot {
	return &r.slots[r.k]
}

func (r *ring) advance() {
	r.k = (r.k + 1) % r.n
}
