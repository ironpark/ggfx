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

package directx

import (
	"errors"
	"fmt"
	"slices"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/ironpark/ggfx/internal/color"
	"github.com/ironpark/ggfx/internal/graphics"
	"github.com/ironpark/ggfx/internal/graphicsdriver"
	"github.com/ironpark/ggfx/internal/recent"
)

type resourceWithSize struct {
	value       *_ID3D12Resource
	gpuAddress  _D3D12_GPU_VIRTUAL_ADDRESS
	sizeInBytes uint32
}

func (r *resourceWithSize) release() {
	r.value.Release()
	r.value = nil
	r.gpuAddress = 0
	r.sizeInBytes = 0
}

const (
	// minUploadArenaSize is the initial upload arena size per frame slot: enough for typical
	// glyph and sprite-region uploads without growing.
	minUploadArenaSize = 256 * 1024

	// maxUploadArenaSize bounds each frame slot's persistently mapped upload arena (and any
	// retained backing buffer after reset). 16 MiB fits a full 2048x2048 RGBA8 atlas page in
	// one frame slot (32 MiB across frameCount=2 slots) while staying well within 32-bit
	// (GOARCH=386) virtual address space limits. Transfers larger than this budget use an
	// exact-size one-off upload buffer released on the next fence completion.
	maxUploadArenaSize = 16 * 1024 * 1024

	// minReadbackBufferSize and maxReadbackBufferSize bound the retained readback buffer reused
	// across ReadPixels calls. ReadPixels calls larger than maxReadbackBufferSize use a one-off
	// readback buffer released immediately before ReadPixels returns.
	minReadbackBufferSize = 64 * 1024
	maxReadbackBufferSize = 16 * 1024 * 1024
)

func createMappedUploadBuffer(device *_ID3D12Device, size uint64) (*_ID3D12Resource, uintptr, error) {
	b, err := createBuffer(device, size, _D3D12_HEAP_TYPE_UPLOAD)
	if err != nil {
		return nil, 0, err
	}
	// An upload heap resource can stay mapped while the GPU reads it; the CPU only writes it.
	m, err := b.Map(0, &_D3D12_RANGE{Begin: 0, End: 0})
	if err != nil {
		b.Release()
		return nil, 0, err
	}
	if m == 0 {
		b.Release()
		return nil, 0, fmt.Errorf("directx: ID3D12Resource::Map failed")
	}
	return b, m, nil
}

// uploadArena12 is one frame slot's bounded upload buffer pool for WritePixels copies.
//
// Fence and reuse invariant:
//   - Bytes in buffer[0:offset] and every buffer in retired may be referenced by
//     CopyTextureRegion commands recorded on drawCommandList or already submitted via an
//     earlier End(FlushModeIntermediate) during this frameIndex.
//   - No byte in buffer[0:offset] is overwritten, and no buffer in retired is unmapped or
//     released, until reset() runs.
//   - reset() is called only after the GPU has completed all commands previously submitted for
//     this frameIndex: either moveToNextFrame() waited for
//     fences[frameIndex] >= fenceValues[frameIndex], or waitForCommandQueue() signaled a new
//     fence value on fences[frameIndex] and waited for it on the FIFO DIRECT command queue
//     (covering both the just-submitted drawCommandList and any prior intermediate submissions).
//   - Therefore, once reset() returns, zero GPU commands reference any prior byte of this slot.
//
// Memory bound invariant:
//   - Ordinary arena storage (buffer plus any outgrown arena buffers in retired) never exceeds
//     maxUploadArenaSize (16 MiB) per slot, plus at most one oversized one-off buffer.
//   - When an ordinary allocation (size <= maxUploadArenaSize) does not fit in buffer and growing
//     would exceed maxUploadArenaSize (including any pending retired bytes), graphics12 drains the
//     command queue (even if drawCommandList is empty due to an intermediate submission) and
//     recycles the slot from offset 0.
//   - Oversized transfers (size > maxUploadArenaSize) use an exact-size one-off buffer stored in
//     retired and never folded into buffer; if retired already holds bytes, the slot is drained
//     first so at most one oversized one-off buffer is live per slot (a subsequent small upload
//     may still be served from existing free room in buffer while that single one-off is pending).
//   - On frame-end reset(use), buffer is released to shrink once recent.Peak ages out (60–120
//     frame-clock ticks with recent.ShrinkFactor hysteresis), returning temporary growth.
type uploadArena12 struct {
	buffer  *_ID3D12Resource
	data    uintptr
	size    uint64
	offset  uint64
	peakUse uint64

	// retired holds previous arena buffers outgrown within the budget and exact-size one-off
	// buffers for transfers larger than maxUploadArenaSize. All are unmapped and released on reset.
	retired      []*_ID3D12Resource
	retiredBytes uint64
}

func (a *uploadArena12) reset(use *recent.Peak) {
	for i, b := range a.retired {
		b.Unmap(0, nil)
		b.Release()
		a.retired[i] = nil
	}
	a.retired = a.retired[:0]
	a.retiredBytes = 0
	a.offset = 0

	if use != nil {
		peak := a.peakUse
		a.peakUse = 0
		if a.buffer != nil {
			want := min(uint64(use.Capacity(int(a.size), int(peak), minUploadArenaSize)), maxUploadArenaSize)
			if want < a.size {
				a.buffer.Unmap(0, nil)
				a.buffer.Release()
				a.buffer, a.data, a.size = nil, 0, 0
			}
		}
	}
}

func (a *uploadArena12) release() {
	a.reset(nil)
	if a.buffer != nil {
		a.buffer.Unmap(0, nil)
		a.buffer.Release()
	}
	*a = uploadArena12{}
}

type graphics12 struct {
	debug             *_ID3D12Debug
	dredEnabled       bool
	device            *_ID3D12Device
	commandQueue      *_ID3D12CommandQueue
	rtvDescriptorSize uint32

	fences         [frameCount]*_ID3D12Fence
	fenceValues    [frameCount]uint64
	fenceWaitEvent windows.Handle

	// drawCommandAllocators are command allocators for the DIRECT command list, which records both
	// draws and texture copies with resource barriers between them.
	drawCommandAllocators [frameCount]*_ID3D12CommandAllocator

	// drawCommandList is the DIRECT command list for draws and copies.
	drawCommandList          *_ID3D12GraphicsCommandList
	drawCommandListOpen      bool
	needFlushDrawCommandList bool

	// drawListState is the state set on drawCommandList since its last Reset.
	drawListState drawListState12

	// uploadArenas hold per-frame bounded upload buffers for WritePixels, reset when the frame
	// completes and shrunk via uploadArenaUse.
	uploadArenas   [frameCount]uploadArena12
	uploadArenaUse recent.Peak

	// readbackBuffer is a bounded readback buffer reused across ReadPixels calls and shrunk via
	// readbackBufferUse. Transfers larger than maxReadbackBufferSize use a one-off buffer.
	// pendingReadbacks holds any readback buffer whose copy submission returned an error before
	// GPU completion was confirmed, so it is not released while a copy may still be in flight.
	readbackBuffer     *_ID3D12Resource
	readbackBufferSize uint64
	readbackBufferUse  recent.Peak
	pendingReadbacks   [frameCount][]*_ID3D12Resource

	vertices [frameCount][]*resourceWithSize
	indices  [frameCount][]*resourceWithSize

	// verticesUse and indicesUse track the bytes the batches of recent frames sent, and batchesUse
	// how many batches recent frames sent, so that the buffers give back the room a large frame
	// took.
	verticesUse recent.Peak
	indicesUse  recent.Peak
	batchesUse  recent.Peak

	graphicsInfra *graphicsInfra

	// surfaces are the surfaces that have render targets, in creation order.
	surfaces []*surface12

	// tmpSwapChains is a temporary slice of the swap chains to present.
	tmpSwapChains []*swapChain

	// frameIndex is the index of the frame resources: the vertex buffers, the command allocators, and
	// the fences. frameIndex cycles through frameCount frames independently of the swap chains' back
	// buffer indices, as each surface's swap chain has its own (see surface12.backBufferIndex).
	// frameIndex must stay stable within a frame.
	frameIndex          int
	prevBeginFrameIndex int

	disposedImages  [frameCount][]*image12
	disposedShaders [frameCount][]*shader12

	pipelineStates
}

func newGraphics12(useWARP bool, useDebugLayer bool, useDRED bool, featureLevel _D3D_FEATURE_LEVEL) (*graphics12, error) {
	g := &graphics12{}

	// Initialize not only a device but also other members like a fence.
	// Even if initializing a device succeeds, initializing a fence might fail (#2142).
	if err := g.initializeDesktop(useWARP, useDebugLayer, useDRED, featureLevel); err != nil {
		return nil, err
	}

	return g, nil
}

func (g *graphics12) initializeDesktop(useWARP bool, useDebugLayer bool, useDRED bool, featureLevel _D3D_FEATURE_LEVEL) (ferr error) {
	if err := d3d12.Load(); err != nil {
		return err
	}

	// As g's lifetime is the same as the process's lifetime, debug and other objects are never released
	// if this initialization succeeds.

	// The debug interface is optional and might not exist.
	if useDebugLayer {
		d, err := _D3D12GetDebugInterface()
		if err != nil {
			return err
		}
		g.debug = d
		defer func() {
			if ferr != nil {
				g.debug.Release()
				g.debug = nil
			}
		}()
		g.debug.EnableDebugLayer()
	}

	// GGFX_DIRECTX's dred token enables Device Removed Extended Data so a device removal can be
	// diagnosed (the page-fault address and the GPU's last command). It must be enabled before the
	// device is created.
	if useDRED {
		settings, err := _D3D12GetDREDSettings()
		if err != nil {
			return err
		}
		settings.SetAutoBreadcrumbsEnablement(_D3D12_DRED_ENABLEMENT_FORCED_ON)
		settings.SetPageFaultEnablement(_D3D12_DRED_ENABLEMENT_FORCED_ON)
		settings.Release()
		g.dredEnabled = true
	}

	f, err := _CreateDXGIFactory()
	if err != nil {
		return err
	}
	gi, err := newGraphicsInfra(f)
	if err != nil {
		return err
	}
	g.graphicsInfra = gi
	defer func() {
		if ferr != nil {
			g.graphicsInfra.release()
			g.graphicsInfra = nil
		}
	}()

	adapters, err := g.graphicsInfra.appendAdapters(nil, useWARP)
	if err != nil {
		return err
	}
	defer func() {
		for _, a := range adapters {
			a.Release()
		}
	}()

	var adapter *_IDXGIAdapter1
	if useWARP {
		if len(adapters) > 0 {
			adapter = adapters[0]
		}
	} else {
		for _, a := range adapters {
			desc, err := a.GetDesc1()
			if err != nil {
				continue
			}
			if desc.Flags&_DXGI_ADAPTER_FLAG_SOFTWARE != 0 {
				continue
			}

			// Test D3D12CreateDevice without creating an actual device.
			if _, err := _D3D12CreateDevice(unsafe.Pointer(a), featureLevel, &_IID_ID3D12Device, false); err != nil {
				continue
			}
			adapter = a
			break
		}
	}

	if adapter == nil {
		return errors.New("directx: DirectX 12 is not supported")
	}

	d, err := _D3D12CreateDevice(unsafe.Pointer(adapter), featureLevel, &_IID_ID3D12Device, true)
	if err != nil {
		return err
	}
	g.device = (*_ID3D12Device)(d)
	defer func() {
		if ferr != nil {
			g.device.Release()
			g.device = nil
		}
	}()

	if err := g.initializeMembers(); err != nil {
		return err
	}
	defer func() {
		if ferr != nil {
			g.releaseMembers()
		}
	}()

	// GetCopyableFootprints might return an invalid value with Wine (#2114).
	// To check this early, call NewImage here.
	i, err := g.NewImage(1, 1, graphicsdriver.PixelFormatRGBA8)
	if err != nil {
		return err
	}
	i.Dispose()

	return nil
}

func (g *graphics12) initializeMembers() (ferr error) {
	defer func() {
		if ferr != nil {
			g.releaseMembers()
		}
	}()

	// Create an event for a fence.
	e, err := windows.CreateEventEx(nil, nil, 0, windows.EVENT_MODIFY_STATE|windows.SYNCHRONIZE)
	if err != nil {
		return fmt.Errorf("directx: CreateEvent failed: %w", err)
	}
	g.fenceWaitEvent = e

	// Create a command queue.
	desc := _D3D12_COMMAND_QUEUE_DESC{
		Type:  _D3D12_COMMAND_LIST_TYPE_DIRECT,
		Flags: _D3D12_COMMAND_QUEUE_FLAG_NONE,
	}
	c, err := g.device.CreateCommandQueue(&desc)
	if err != nil {
		return err
	}
	g.commandQueue = c

	// Create command allocators.
	for i := range frameCount {
		dca, err := g.device.CreateCommandAllocator(_D3D12_COMMAND_LIST_TYPE_DIRECT)
		if err != nil {
			return err
		}
		g.drawCommandAllocators[i] = dca
	}

	// Create a frame fence.
	for i := range frameCount {
		f, err := g.device.CreateFence(0, _D3D12_FENCE_FLAG_NONE)
		if err != nil {
			return err
		}
		g.fences[i] = f
	}

	// Create command list.
	dcl, err := g.device.CreateCommandList(0, _D3D12_COMMAND_LIST_TYPE_DIRECT, g.drawCommandAllocators[0], nil)
	if err != nil {
		return err
	}
	g.drawCommandList = dcl

	// Close the command list once as this is immediately Reset at Begin.
	if err := g.drawCommandList.Close(); err != nil {
		return err
	}

	// Each surface has its own descriptor heap for RTV (see surface12).
	g.rtvDescriptorSize = g.device.GetDescriptorHandleIncrementSize(_D3D12_DESCRIPTOR_HEAP_TYPE_RTV)

	if err := g.pipelineStates.initialize(g.device); err != nil {
		return err
	}

	return nil
}

// releaseMembers releases the objects created by initializeMembers.
func (g *graphics12) releaseMembers() {
	g.pipelineStates.release()

	for i := range g.uploadArenas {
		g.uploadArenas[i].release()
	}
	if g.readbackBuffer != nil {
		g.readbackBuffer.Release()
		g.readbackBuffer = nil
		g.readbackBufferSize = 0
	}
	for i := range g.pendingReadbacks {
		g.releasePendingReadbacks(i)
	}

	g.rtvDescriptorSize = 0

	if g.drawCommandList != nil {
		g.drawCommandList.Release()
		g.drawCommandList = nil
	}

	for i := range frameCount {
		if g.fences[i] != nil {
			g.fences[i].Release()
			g.fences[i] = nil
		}
		if g.drawCommandAllocators[i] != nil {
			g.drawCommandAllocators[i].Release()
			g.drawCommandAllocators[i] = nil
		}
	}

	if g.commandQueue != nil {
		g.commandQueue.Release()
		g.commandQueue = nil
	}

	if g.fenceWaitEvent != 0 {
		_ = windows.CloseHandle(g.fenceWaitEvent)
		g.fenceWaitEvent = 0
	}
}

func (g *graphics12) Initialize() (err error) {
	// Initialization should already be done.
	return nil
}

func createBuffer(device *_ID3D12Device, bufferSize uint64, heapType _D3D12_HEAP_TYPE) (*_ID3D12Resource, error) {
	state := _D3D12_RESOURCE_STATE_GENERIC_READ
	if heapType == _D3D12_HEAP_TYPE_READBACK {
		state = _D3D12_RESOURCE_STATE_COPY_DEST
	}

	r, err := device.CreateCommittedResource(&_D3D12_HEAP_PROPERTIES{
		Type:                 heapType,
		CPUPageProperty:      _D3D12_CPU_PAGE_PROPERTY_UNKNOWN,
		MemoryPoolPreference: _D3D12_MEMORY_POOL_UNKNOWN,
		CreationNodeMask:     1,
		VisibleNodeMask:      1,
	}, _D3D12_HEAP_FLAG_NONE, &_D3D12_RESOURCE_DESC{
		Dimension:        _D3D12_RESOURCE_DIMENSION_BUFFER,
		Alignment:        0,
		Width:            bufferSize,
		Height:           1,
		DepthOrArraySize: 1,
		MipLevels:        1,
		Format:           _DXGI_FORMAT_UNKNOWN,
		SampleDesc: _DXGI_SAMPLE_DESC{
			Count:   1,
			Quality: 0,
		},
		Layout: _D3D12_TEXTURE_LAYOUT_ROW_MAJOR,
		Flags:  _D3D12_RESOURCE_FLAG_NONE,
	}, state, nil)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// allocUpload returns a persistently mapped upload buffer, a 512-byte-aligned offset into it,
// and the corresponding writable byte slice of length size.
func (g *graphics12) allocUpload(size uint64) (*_ID3D12Resource, uint64, []byte, error) {
	a := &g.uploadArenas[g.frameIndex]

	// Oversized transfers get an exact-size one-off upload buffer rather than growing the arena.
	// Drain first if the slot already holds retired/one-off buffers so repeated oversized
	// WritePixels (even across FlushModeIntermediate calls) never pile up more than one buffer.
	if size > maxUploadArenaSize {
		if a.retiredBytes > 0 {
			if err := g.drainCurrentFrame(); err != nil {
				return nil, 0, nil, err
			}
		}
		b, m, err := createMappedUploadBuffer(g.device, size)
		if err != nil {
			return nil, 0, nil, err
		}
		a.retired = append(a.retired, b)
		a.retiredBytes += size
		return b, 0, unsafe.Slice((*byte)(unsafe.Pointer(m)), size), nil
	}

	// Fast path: serve from existing free room in a.buffer (even if a single oversized one-off
	// buffer is pending in a.retired; growth/overflow paths below drain if retiredBytes+size
	// exceeds the budget).
	alignedOff := (a.offset + textureDataPlacementAlignment - 1) &^ (textureDataPlacementAlignment - 1)
	if a.buffer != nil && alignedOff >= a.offset && size <= a.size && alignedOff <= a.size-size {
		a.offset = alignedOff + size
		a.peakUse = max(a.peakUse, a.offset)
		return a.buffer, alignedOff, unsafe.Slice((*byte)(unsafe.Pointer(a.data+uintptr(alignedOff))), size), nil
	}

	newSize := max(a.size*2, size, minUploadArenaSize)
	if newSize > maxUploadArenaSize || newSize < a.size {
		newSize = maxUploadArenaSize
	}

	if a.buffer != nil && (a.size >= maxUploadArenaSize || a.retiredBytes+a.size+newSize > maxUploadArenaSize) {
		if err := g.drainCurrentFrame(); err != nil {
			return nil, 0, nil, err
		}
		if size <= a.size {
			a.offset = size
			a.peakUse = max(a.peakUse, a.offset)
			return a.buffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(a.data)), size), nil
		}
		// The GPU is idle for this slot, so the smaller arena buffer can be released immediately.
		a.buffer.Unmap(0, nil)
		a.buffer.Release()
		a.buffer, a.data, a.size = nil, 0, 0
	} else if a.buffer != nil {
		a.retired = append(a.retired, a.buffer)
		a.retiredBytes += a.size
		a.buffer, a.data, a.size, a.offset = nil, 0, 0, 0
	} else if a.retiredBytes+newSize > maxUploadArenaSize {
		if err := g.drainCurrentFrame(); err != nil {
			return nil, 0, nil, err
		}
	}

	b, m, err := createMappedUploadBuffer(g.device, newSize)
	if err != nil {
		return nil, 0, nil, err
	}
	a.buffer = b
	a.data = m
	a.size = newSize
	a.offset = size
	a.peakUse = max(a.peakUse, a.offset)
	return a.buffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(a.data)), size), nil
}

// acquireReadbackBuffer returns a readback buffer of at least size bytes. If oneOff is true, the
// transfer exceeded maxReadbackBufferSize and the caller must release the returned buffer after
// ReadPixels finishes.
func (g *graphics12) acquireReadbackBuffer(size uint64) (*_ID3D12Resource, bool, error) {
	if size > maxReadbackBufferSize {
		b, err := createBuffer(g.device, size, _D3D12_HEAP_TYPE_READBACK)
		if err != nil {
			return nil, false, err
		}
		return b, true, nil
	}
	want := min(uint64(g.readbackBufferUse.Capacity(int(g.readbackBufferSize), int(size), minReadbackBufferSize)), maxReadbackBufferSize)
	if g.readbackBuffer != nil && g.readbackBufferSize == want {
		return g.readbackBuffer, false, nil
	}
	if g.readbackBuffer != nil {
		g.readbackBuffer.Release()
		g.readbackBuffer = nil
		g.readbackBufferSize = 0
	}
	b, err := createBuffer(g.device, want, _D3D12_HEAP_TYPE_READBACK)
	if err != nil {
		return nil, false, err
	}
	g.readbackBuffer = b
	g.readbackBufferSize = want
	return b, false, nil
}

// trimReadbackBuffer releases the retained readback buffer once recent ReadPixels calls no longer
// need its size.
func (g *graphics12) trimReadbackBuffer() {
	if g.readbackBuffer == nil {
		return
	}
	want := min(uint64(g.readbackBufferUse.Capacity(int(g.readbackBufferSize), 0, minReadbackBufferSize)), maxReadbackBufferSize)
	if want < g.readbackBufferSize {
		g.readbackBuffer.Release()
		g.readbackBuffer = nil
		g.readbackBufferSize = 0
	}
}

// retireInFlightReadback keeps buf alive in the current frame slot until a subsequent fence
// confirms GPU completion. If buf is the shared readbackBuffer, it is detached so future calls
// cannot reuse or release it while the copy may still be in flight.
func (g *graphics12) retireInFlightReadback(buf *_ID3D12Resource) {
	if buf == g.readbackBuffer {
		g.readbackBuffer = nil
		g.readbackBufferSize = 0
	}
	g.pendingReadbacks[g.frameIndex] = append(g.pendingReadbacks[g.frameIndex], buf)
}

// releasePendingReadbacks releases readback buffers retired during frameIndex once GPU completion
// of that frame slot has been confirmed.
func (g *graphics12) releasePendingReadbacks(frameIndex int) {
	for i, b := range g.pendingReadbacks[frameIndex] {
		b.Release()
		g.pendingReadbacks[frameIndex][i] = nil
	}
	g.pendingReadbacks[frameIndex] = g.pendingReadbacks[frameIndex][:0]
}

// surface12 is the swap chain on one window.
type surface12 struct {
	graphics *graphics12
	window   windows.HWND

	// swapChain is nil until the first screen image is created.
	swapChain *swapChain

	rtvDescriptorHeap *_ID3D12DescriptorHeap
	renderTargets     [frameCount]*_ID3D12Resource
	states            [frameCount]_D3D12_RESOURCE_STATES

	// backBufferIndex is the index of the swap chain's current back buffer, which the screen image
	// renders into and which present flips. It is updated when the swap chain is created, resized,
	// or presented.
	backBufferIndex int

	screenImage *image12

	// drawn reports whether the screen image was drawn to since the previous present.
	drawn bool
}

// NewSurface records the window. Its swap chain is created with the first screen image, on the
// rendering thread.
func (g *graphics12) NewSurface(desc graphicsdriver.SurfaceDescriptor) (graphicsdriver.Surface, error) {
	w, err := hwndFromDescriptor(desc)
	if err != nil {
		return nil, err
	}
	return &surface12{
		graphics: g,
		window:   w,
	}, nil
}

func (s *surface12) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	return s.graphics.newScreenImage(s, width, height)
}

// FlipsY reports true: a back buffer's rows go down from clip-space Y = 1.
func (s *surface12) FlipsY() bool { return true }

// Dispose releases the swap chain and the render targets. Dispose is called on the rendering thread
// between Begin and End.
func (s *surface12) Dispose() {
	g := s.graphics
	g.surfaces = slices.DeleteFunc(g.surfaces, func(t *surface12) bool { return t == s })
	s.drawn = false

	if s.swapChain == nil && s.rtvDescriptorHeap == nil && s.renderTargets == [frameCount]*_ID3D12Resource{} {
		return
	}

	// The commands recorded so far and the frames in flight might still use the render targets.
	// Execute the recorded commands and wait for the GPU to be idle before releasing them.
	if err := g.waitForGPUIdle(); err != nil {
		// The device is likely removed. Leak the resources rather than releasing what the GPU
		// might still use.
		return
	}

	if s.screenImage != nil {
		s.screenImage.surface = nil
		s.screenImage = nil
	}
	s.releaseRenderTargets()
	if s.rtvDescriptorHeap != nil {
		s.rtvDescriptorHeap.Release()
		s.rtvDescriptorHeap = nil
	}
	if s.swapChain != nil {
		s.swapChain.release()
		s.swapChain = nil
	}
}

// waitForGPUIdle executes the commands recorded so far and waits for all the commands submitted to
// the command queue to complete. This must be called between Begin and End, while the command list
// is open, and keeps it open.
func (g *graphics12) waitForGPUIdle() error {
	if err := g.flushDrawCommandList(); err != nil {
		return err
	}
	// Signaling the command queue and waiting for it makes the GPU idle, since the queue executes
	// serially. This covers the frames in flight too.
	return g.waitForCommandQueue()
}

func (g *graphics12) updateSwapChain(s *surface12, width, height int) error {
	if s.window == 0 {
		return errors.New("directx: the window handle is not initialized yet")
	}

	if s.rtvDescriptorHeap == nil {
		h, err := g.device.CreateDescriptorHeap(&_D3D12_DESCRIPTOR_HEAP_DESC{
			Type:           _D3D12_DESCRIPTOR_HEAP_TYPE_RTV,
			NumDescriptors: frameCount,
			Flags:          _D3D12_DESCRIPTOR_HEAP_FLAG_NONE,
			NodeMask:       0,
		})
		if err != nil {
			return err
		}
		s.rtvDescriptorHeap = h
	}

	if err := g.updateSwapChainImpl(s, width, height); err != nil {
		return err
	}

	if !slices.Contains(g.surfaces, s) {
		g.surfaces = append(g.surfaces, s)
	}
	return nil
}

func (g *graphics12) updateSwapChainImpl(s *surface12, width, height int) error {
	if s.swapChain == nil {
		if err := g.initSwapChainDesktop(s, width, height); err != nil {
			return err
		}
		return nil
	}

	// If the buffers already cover the window, leave the swap chain unchanged; the screen renders
	// into the top-left window-sized region.
	if s.swapChain.canReuseBuffers(width, height) {
		return nil
	}

	// Otherwise reallocate the swap chain now, before this frame renders the screen, so that the
	// frame renders and presents at the new size. Presenting a stale-size buffer while the window is
	// already at the new size makes the compositor scale it for a moment (#3477).
	if err := g.resizeSwapChainDesktop(s, width, height); err != nil {
		return err
	}

	return nil
}

func (g *graphics12) initSwapChainDesktop(s *surface12, width, height int) (ferr error) {
	sc, err := g.graphicsInfra.newSwapChain(width, height, unsafe.Pointer(g.commandQueue), s.window)
	if err != nil {
		return err
	}
	s.swapChain = sc
	defer func() {
		if ferr != nil {
			s.releaseRenderTargets()
			s.swapChain.release()
			s.swapChain = nil
		}
	}()

	if err := g.createRenderTargetViewsDesktop(s); err != nil {
		return err
	}

	idx, err := s.swapChain.currentBackBufferIndex()
	if err != nil {
		return err
	}
	s.backBufferIndex = idx
	s.states = [frameCount]_D3D12_RESOURCE_STATES{}

	return nil
}

// resizeSwapChainDesktop resizes the surface's swap chain in the middle of a frame, before the frame
// renders the screen. This must be called between Begin and End, while the command list is open.
func (g *graphics12) resizeSwapChainDesktop(s *surface12, width, height int) error {
	// Execute the commands recorded so far this frame, then close the command list. ResizeBuffers
	// requires all references to the swap chain's buffers released, all the GPU work referencing them
	// finished, and the command list not recording.
	if err := g.flushDrawCommandList(); err != nil {
		return err
	}
	if g.drawCommandListOpen {
		g.drawCommandListOpen = false
		g.needFlushDrawCommandList = false
		if err := g.drawCommandList.Close(); err != nil {
			return g.withDeviceRemovedReason(err)
		}
	}

	// Signaling the command queue and waiting for it makes the GPU idle, since the queue executes
	// serially. This also covers the frames in flight that presented this swap chain.
	if err := g.waitForCommandQueue(); err != nil {
		return err
	}
	g.releaseResources(g.frameIndex)
	g.pipelineStates.resetFrame(g.frameIndex)
	g.uploadArenas[g.frameIndex].reset(nil)
	g.releasePendingReadbacks(g.frameIndex)

	// Only this surface's render targets are released. The other surfaces' swap chains are not
	// affected by ResizeBuffers.
	s.releaseRenderTargets()

	if err := s.swapChain.resize(width, height); err != nil {
		return err
	}

	if err := g.createRenderTargetViewsDesktop(s); err != nil {
		return err
	}

	// ResizeBuffers resets the current back buffer index. Update backBufferIndex so that the screen
	// renders into the current back buffer. frameIndex is independent of it and stays unchanged: the
	// vertex buffers for this frame were already uploaded under frameIndex, and the command allocators
	// and fences are indexed by it.
	idx, err := s.swapChain.currentBackBufferIndex()
	if err != nil {
		return err
	}
	s.backBufferIndex = idx
	s.states = [frameCount]_D3D12_RESOURCE_STATES{}

	// Re-open the command list so the rest of the frame can keep recording. The GPU is already idle,
	// so resetting the allocator is safe.
	if err := g.drawCommandAllocators[g.frameIndex].Reset(); err != nil {
		return err
	}
	if err := g.drawCommandList.Reset(g.drawCommandAllocators[g.frameIndex], nil); err != nil {
		return err
	}
	g.drawCommandListOpen = true
	g.drawListState = drawListState12{}

	return nil
}

// releaseRenderTargets releases the current render targets. The caller must make sure that the GPU
// no longer uses them.
func (s *surface12) releaseRenderTargets() {
	for i, r := range s.renderTargets {
		if r == nil {
			continue
		}
		r.Release()
		s.renderTargets[i] = nil
	}
}

func (g *graphics12) createRenderTargetViewsDesktop(s *surface12) (ferr error) {
	// Create frame resources.
	h, err := s.rtvDescriptorHeap.GetCPUDescriptorHandleForHeapStart()
	if err != nil {
		return err
	}
	for i := range frameCount {
		r, err := s.swapChain.getBuffer(uint32(i), &_IID_ID3D12Resource)
		if err != nil {
			return err
		}
		s.renderTargets[i] = (*_ID3D12Resource)(r)
		defer func(i int) {
			if ferr != nil {
				s.renderTargets[i].Release()
				s.renderTargets[i] = nil
			}
		}(i)

		g.device.CreateRenderTargetView((*_ID3D12Resource)(r), nil, h)
		h.Offset(1, g.rtvDescriptorSize)
	}

	return nil
}

func (g *graphics12) ColorSpace() color.ColorSpace {
	return color.ColorSpaceSRGB
}

func (g *graphics12) Begin() error {
	if g.prevBeginFrameIndex != g.frameIndex {
		if g.drawCommandListOpen {
			g.drawCommandListOpen = false
			g.needFlushDrawCommandList = false
			if err := g.drawCommandList.Close(); err != nil {
				return g.withDeviceRemovedReason(err)
			}
		}
		if err := g.drawCommandAllocators[g.frameIndex].Reset(); err != nil {
			return err
		}
	}
	g.prevBeginFrameIndex = g.frameIndex

	if !g.drawCommandListOpen {
		if err := g.drawCommandList.Reset(g.drawCommandAllocators[g.frameIndex], nil); err != nil {
			return err
		}
		g.drawCommandListOpen = true
		g.drawListState = drawListState12{}
	}
	return nil
}

// IsOccluded reports whether the screen is invisible: whether none of the windows presented at the
// last present was visible.
func (g *graphics12) IsOccluded() bool {
	return g.graphicsInfra.occluded.Load()
}

func (g *graphics12) End(mode graphicsdriver.FlushMode) error {
	// The swap chain might still be nil when Begin-End is invoked not by a frame (e.g., Image.At).

	if g.drawCommandListOpen {
		// Transition the back buffers to present. There might be no surfaces in tests.
		if mode == graphicsdriver.FlushModePresent {
			for _, s := range g.surfaces {
				if !g.shouldPresent(s) {
					continue
				}
				if rb, ok := s.screenImage.transiteState(_D3D12_RESOURCE_STATE_PRESENT); ok {
					g.drawCommandList.ResourceBarrier([]_D3D12_RESOURCE_BARRIER_Transition{rb})
				}
			}
		}

		g.drawCommandListOpen = false
		g.needFlushDrawCommandList = false
		if err := g.drawCommandList.Close(); err != nil {
			return g.withDeviceRemovedReason(err)
		}
		g.commandQueue.ExecuteCommandLists([]*_ID3D12GraphicsCommandList{g.drawCommandList})
	}

	// Release vertices and indices buffers when too many ones were created.
	// The threshold is an arbitrary number.
	// Tests can submit command batches without completing a frame.
	if len(g.vertices[g.frameIndex]) >= 16 {
		if err := g.waitForCommandQueue(); err != nil {
			return err
		}
		g.releaseResources(g.frameIndex)
		g.resetVerticesAndIndices(g.frameIndex, true)
		g.pipelineStates.resetFrame(g.frameIndex)
		g.uploadArenas[g.frameIndex].reset(nil)
		g.releasePendingReadbacks(g.frameIndex)
	}

	// An intermediate flush does not wait for the GPU, so the frame's uniform ring, upload arena,
	// and descriptors stay taken until the frame completes below or a flush waits for the GPU.

	if mode == graphicsdriver.FlushModePresent {
		if err := g.presentDesktop(); err != nil {
			return err
		}
	}

	if mode != graphicsdriver.FlushModeIntermediate {
		if mode == graphicsdriver.FlushModePresent {
			if err := g.moveToNextFrame(); err != nil {
				return err
			}
		} else {
			// The back buffer stays current, so wait before reusing its frame resources.
			if err := g.waitForCommandQueue(); err != nil {
				return err
			}
		}
		// Reset the command allocator when the next frame begins, even if the back buffer did not change.
		g.prevBeginFrameIndex = -1

		g.releaseResources(g.frameIndex)
		g.resetVerticesAndIndices(g.frameIndex, false)
		g.pipelineStates.resetFrame(g.frameIndex)
		g.uploadArenas[g.frameIndex].reset(&g.uploadArenaUse)
		g.releasePendingReadbacks(g.frameIndex)
		g.trimReadbackBuffer()
	}

	return nil
}

// shouldPresent reports whether the surface is presented at the present in this frame.
func (g *graphics12) shouldPresent(s *surface12) bool {
	if s.screenImage == nil {
		return false
	}
	// A surface that was not drawn keeps showing its previous frame.
	return s.drawn
}

func (g *graphics12) presentDesktop() error {
	swapChains := g.tmpSwapChains[:0]
	for _, s := range g.surfaces {
		if !g.shouldPresent(s) {
			continue
		}
		swapChains = append(swapChains, s.swapChain)
	}
	err := g.graphicsInfra.present(swapChains)
	clear(swapChains)
	g.tmpSwapChains = swapChains[:0]
	if err != nil {
		return err
	}

	for _, s := range g.surfaces {
		if !g.shouldPresent(s) {
			continue
		}
		s.drawn = false
		idx, err := s.swapChain.currentBackBufferIndex()
		if err != nil {
			return err
		}
		s.backBufferIndex = idx
	}
	return nil
}

// FinishForcedFrame waits for a frame forced while the game loop is blocked (e.g. during a window
// resize) to finish on the GPU, so it reaches the screen immediately rather than a frame late, which
// DirectX 12's frame pipelining would otherwise cause (#3477).
func (g *graphics12) FinishForcedFrame() error {
	return g.waitForCommandQueue()
}

func (g *graphics12) moveToNextFrame() error {
	g.fenceValues[g.frameIndex]++
	fv := g.fenceValues[g.frameIndex]
	if err := g.commandQueue.Signal(g.fences[g.frameIndex], fv); err != nil {
		return err
	}

	// Update the frame index. The frame resources cycle independently of the swap chains, whose back
	// buffer indices are updated at present. Waiting for the fence of the next frame index below also
	// makes sure that the GPU finished the frame that last rendered into each surface's next back
	// buffer, as a swap chain's back buffer is not reused until frameCount presents later.
	g.frameIndex = (g.frameIndex + 1) % frameCount

	if g.fences[g.frameIndex].GetCompletedValue() < g.fenceValues[g.frameIndex] {
		if err := g.fences[g.frameIndex].SetEventOnCompletion(g.fenceValues[g.frameIndex], g.fenceWaitEvent); err != nil {
			return err
		}
		if _, err := windows.WaitForSingleObject(g.fenceWaitEvent, windows.INFINITE); err != nil {
			return err
		}
	}
	return nil
}

func (g *graphics12) releaseResources(frameIndex int) {
	for i, img := range g.disposedImages[frameIndex] {
		img.disposeImpl()
		g.disposedImages[frameIndex][i] = nil
	}
	g.disposedImages[frameIndex] = g.disposedImages[frameIndex][:0]

	for i, s := range g.disposedShaders[frameIndex] {
		s.disposeImpl()
		g.disposedShaders[frameIndex][i] = nil
	}
	g.disposedShaders[frameIndex] = g.disposedShaders[frameIndex][:0]
}

func (g *graphics12) resetVerticesAndIndices(frameIndex int, release bool) {
	if !release {
		// Release the buffers of the slots recent frames did not use: a frame of many batches
		// would otherwise keep its buffers for good. The GPU has completed the frame.
		g.batchesUse.ObserveNow(len(g.vertices[frameIndex]))
		keep := max(g.batchesUse.Max(), 1)
		for _, bs := range [][]*resourceWithSize{g.vertices[frameIndex], g.indices[frameIndex]} {
			bs = bs[:cap(bs)]
			for i := keep; i < len(bs); i++ {
				if bs[i] != nil {
					bs[i].release()
					bs[i] = nil
				}
			}
		}
	}
	if release {
		for _, bs := range [][]*resourceWithSize{g.vertices[frameIndex], g.indices[frameIndex]} {
			bs = bs[:cap(bs)]
			for i := range bs {
				if bs[i] != nil {
					bs[i].release()
					bs[i] = nil
				}
			}
		}
	}
	g.vertices[frameIndex] = g.vertices[frameIndex][:0]
	g.indices[frameIndex] = g.indices[frameIndex][:0]
}

// flushDrawCommandList executes commands recorded on drawCommandList and waits for completion.
func (g *graphics12) flushDrawCommandList() error {
	if !g.needFlushDrawCommandList || !g.drawCommandListOpen {
		return nil
	}
	g.needFlushDrawCommandList = false
	g.drawCommandListOpen = false

	if err := g.drawCommandList.Close(); err != nil {
		return g.withDeviceRemovedReason(err)
	}

	g.commandQueue.ExecuteCommandLists([]*_ID3D12GraphicsCommandList{g.drawCommandList})

	if err := g.waitForCommandQueue(); err != nil {
		return err
	}

	if err := g.drawCommandAllocators[g.frameIndex].Reset(); err != nil {
		return err
	}
	if err := g.drawCommandList.Reset(g.drawCommandAllocators[g.frameIndex], nil); err != nil {
		return err
	}
	g.drawCommandListOpen = true
	g.drawListState = drawListState12{}
	// The GPU is idle, so the frame's uniform ring, upload arena, and descriptors can be reused.
	g.pipelineStates.resetFrame(g.frameIndex)
	g.uploadArenas[g.frameIndex].reset(nil)
	g.releasePendingReadbacks(g.frameIndex)

	return nil
}

// drainCurrentFrame executes any recorded commands on drawCommandList, waits for the command queue
// to complete (including any earlier FlushModeIntermediate submissions), and resets the current
// frame slot's transient arenas and descriptors.
func (g *graphics12) drainCurrentFrame() error {
	if g.needFlushDrawCommandList && g.drawCommandListOpen {
		return g.flushDrawCommandList()
	}
	if err := g.waitForCommandQueue(); err != nil {
		return err
	}
	if g.drawCommandListOpen {
		g.drawCommandListOpen = false
		if err := g.drawCommandList.Close(); err != nil {
			return g.withDeviceRemovedReason(err)
		}
		if err := g.drawCommandAllocators[g.frameIndex].Reset(); err != nil {
			return err
		}
		if err := g.drawCommandList.Reset(g.drawCommandAllocators[g.frameIndex], nil); err != nil {
			return err
		}
		g.drawCommandListOpen = true
		g.drawListState = drawListState12{}
	}
	g.pipelineStates.resetFrame(g.frameIndex)
	g.drawListState.resetDescriptorBindings()
	g.uploadArenas[g.frameIndex].reset(nil)
	g.releasePendingReadbacks(g.frameIndex)
	return nil
}

func (g *graphics12) waitForCommandQueue() error {
	g.fenceValues[g.frameIndex]++
	fv := g.fenceValues[g.frameIndex]
	if err := g.commandQueue.Signal(g.fences[g.frameIndex], fv); err != nil {
		return err
	}
	if err := g.fences[g.frameIndex].SetEventOnCompletion(fv, g.fenceWaitEvent); err != nil {
		return err
	}
	if _, err := windows.WaitForSingleObject(g.fenceWaitEvent, windows.INFINITE); err != nil {
		return err
	}
	return nil
}

// withDeviceRemovedReason augments err with the device-removed reason when the device has been removed.
// A GPU fault surfaces from a later API call (e.g. command list Close) as a generic
// DXGI_ERROR_DEVICE_REMOVED, so appending the reason — and DRED's breadcrumbs/page-fault when enabled —
// pinpoints the underlying cause.
func (g *graphics12) withDeviceRemovedReason(err error) error {
	if err == nil {
		return nil
	}
	if reason := g.device.GetDeviceRemovedReason(); reason != nil {
		if g.dredEnabled {
			g.dumpDRED()
		}
		return fmt.Errorf("%w (device removed reason: %w)", err, reason)
	}
	return err
}

// SupportsDirectComposition reports whether this driver can present through DirectComposition.
// It must be called before the window is created, as it decides whether the window can be created
// without a redirection surface.
func (g *graphics12) SupportsDirectComposition() bool {
	return g.graphicsInfra.supportsComposition(unsafe.Pointer(g.commandQueue))
}

func (g *graphics12) SetVertices(vertices []float32, indices []uint32) (ferr error) {
	if len(vertices) == 0 || len(indices) == 0 {
		return nil
	}

	// Create buffers if necessary.
	vidx := len(g.vertices[g.frameIndex])
	if cap(g.vertices[g.frameIndex]) > vidx {
		g.vertices[g.frameIndex] = g.vertices[g.frameIndex][:vidx+1]
	} else {
		g.vertices[g.frameIndex] = append(g.vertices[g.frameIndex], nil)
	}
	defer func() {
		if ferr != nil {
			g.vertices[g.frameIndex] = g.vertices[g.frameIndex][:vidx]
		}
	}()
	// The slot's buffer was last used by a frame the GPU has completed, so it can be replaced.
	var curvsize int
	if b := g.vertices[g.frameIndex][vidx]; b != nil {
		curvsize = int(b.sizeInBytes)
	}
	vsize := uint32(g.verticesUse.Capacity(curvsize, len(vertices)*int(unsafe.Sizeof(float32(0))), graphicsdriver.MinVertexBufferBytes))
	if g.vertices[g.frameIndex][vidx] != nil && g.vertices[g.frameIndex][vidx].sizeInBytes != vsize {
		g.vertices[g.frameIndex][vidx].release()
		g.vertices[g.frameIndex][vidx] = nil
	}
	if g.vertices[g.frameIndex][vidx] == nil {
		// TODO: Use the default heap for efficiently. See the official example HelloTriangle.
		vs, err := createBuffer(g.device, uint64(vsize), _D3D12_HEAP_TYPE_UPLOAD)
		if err != nil {
			return err
		}
		g.vertices[g.frameIndex][vidx] = &resourceWithSize{
			value:       vs,
			gpuAddress:  vs.GetGPUVirtualAddress(),
			sizeInBytes: vsize,
		}
		defer func() {
			if ferr != nil {
				g.vertices[g.frameIndex][vidx].release()
				g.vertices[g.frameIndex][vidx] = nil
			}
		}()
	}

	iidx := len(g.indices[g.frameIndex])
	if cap(g.indices[g.frameIndex]) > iidx {
		g.indices[g.frameIndex] = g.indices[g.frameIndex][:iidx+1]
	} else {
		g.indices[g.frameIndex] = append(g.indices[g.frameIndex], nil)
	}
	defer func() {
		if ferr != nil {
			g.indices[g.frameIndex] = g.indices[g.frameIndex][:iidx]
		}
	}()
	// The slot's buffer was last used by a frame the GPU has completed, so it can be replaced.
	var curisize int
	if b := g.indices[g.frameIndex][iidx]; b != nil {
		curisize = int(b.sizeInBytes)
	}
	isize := uint32(g.indicesUse.Capacity(curisize, len(indices)*int(unsafe.Sizeof(uint32(0))), graphicsdriver.MinVertexBufferBytes))
	if g.indices[g.frameIndex][iidx] != nil && g.indices[g.frameIndex][iidx].sizeInBytes != isize {
		g.indices[g.frameIndex][iidx].release()
		g.indices[g.frameIndex][iidx] = nil
	}
	if g.indices[g.frameIndex][iidx] == nil {
		is, err := createBuffer(g.device, uint64(isize), _D3D12_HEAP_TYPE_UPLOAD)
		if err != nil {
			return err
		}
		g.indices[g.frameIndex][iidx] = &resourceWithSize{
			value:       is,
			gpuAddress:  is.GetGPUVirtualAddress(),
			sizeInBytes: isize,
		}
		defer func() {
			if ferr != nil {
				g.indices[g.frameIndex][iidx].release()
				g.indices[g.frameIndex][iidx] = nil
			}
		}()
	}

	m, err := g.vertices[g.frameIndex][vidx].value.Map(0, &_D3D12_RANGE{Begin: 0, End: 0})
	if err != nil {
		return err
	}
	copy(unsafe.Slice((*float32)(unsafe.Pointer(m)), len(vertices)), vertices)
	g.vertices[g.frameIndex][vidx].value.Unmap(0, nil)

	m, err = g.indices[g.frameIndex][iidx].value.Map(0, &_D3D12_RANGE{Begin: 0, End: 0})
	if err != nil {
		return err
	}
	copy(unsafe.Slice((*uint32)(unsafe.Pointer(m)), len(indices)), indices)
	g.indices[g.frameIndex][iidx].value.Unmap(0, nil)

	return nil
}

// SupportsPixelFormat reports PixelFormatRGBA8 and PixelFormatA8.
//
// An A8 image is a DXGI_FORMAT_R8_UNORM texture, drawn into through a plain R8 render target
// view and sampled through a shader resource view that maps all four components to the red one.
// Sampling, rendering to and blending R8_UNORM are required at feature level 11_0, the lowest
// Direct3D 12 runs at, so no device needs asking.
func (g *graphics12) SupportsPixelFormat(format graphicsdriver.PixelFormat) bool {
	switch format {
	case graphicsdriver.PixelFormatRGBA8, graphicsdriver.PixelFormatA8:
		return true
	}
	return false
}

func (g *graphics12) NewImage(width, height int, format graphicsdriver.PixelFormat) (graphicsdriver.Image, error) {
	if !g.SupportsPixelFormat(format) {
		return nil, fmt.Errorf("directx: unsupported pixel format %s", format)
	}
	graphicsdriver.CheckSize("directx", width, height, g.MaxImageSize())
	desc := _D3D12_RESOURCE_DESC{
		Dimension:        _D3D12_RESOURCE_DIMENSION_TEXTURE2D,
		Alignment:        0,
		Width:            uint64(graphics.InternalImageSize(width)),
		Height:           uint32(graphics.InternalImageSize(height)),
		DepthOrArraySize: 1,
		MipLevels:        1,
		Format:           dxgiFormat(format),
		SampleDesc: _DXGI_SAMPLE_DESC{
			Count:   1,
			Quality: 0,
		},
		Layout: _D3D12_TEXTURE_LAYOUT_UNKNOWN,
		Flags:  _D3D12_RESOURCE_FLAG_ALLOW_RENDER_TARGET,
	}

	state := _D3D12_RESOURCE_STATE_PIXEL_SHADER_RESOURCE
	t, err := g.device.CreateCommittedResource(&_D3D12_HEAP_PROPERTIES{
		Type:                 _D3D12_HEAP_TYPE_DEFAULT, // Upload?
		CPUPageProperty:      _D3D12_CPU_PAGE_PROPERTY_UNKNOWN,
		MemoryPoolPreference: _D3D12_MEMORY_POOL_UNKNOWN,
		CreationNodeMask:     1,
		VisibleNodeMask:      1,
	}, _D3D12_HEAP_FLAG_NONE, &desc, state, nil)
	if err != nil {
		return nil, err
	}

	i := &image12{
		graphics: g,
		width:    width,
		height:   height,
		format:   format,
		texture:  t,
		resState: state,
	}
	return i, nil
}

func (g *graphics12) newScreenImage(s *surface12, width, height int) (graphicsdriver.Image, error) {
	if s.screenImage != nil {
		s.screenImage.Dispose()
		s.screenImage = nil
	}

	// updateSwapChain resizes the swap chain to the new size immediately, so the screen image can be
	// created with the new size and the frame renders and presents at that size (#3477).
	if err := g.updateSwapChain(s, width, height); err != nil {
		return nil, err
	}

	i := &image12{
		graphics: g,
		width:    width,
		height:   height,
		screen:   true,
		surface:  s,
	}
	s.screenImage = i
	return i, nil
}

func (g *graphics12) removeImage(img *image12) {
	g.disposedImages[g.frameIndex] = append(g.disposedImages[g.frameIndex], img)
}

func (g *graphics12) removeShader(s *shader12) {
	g.disposedShaders[g.frameIndex] = append(g.disposedShaders[g.frameIndex], s)
}

func (g *graphics12) NeedsClearingScreen() bool {
	// TODO: Confirm this is really true.
	return true
}

func (g *graphics12) MaxImageSize() int {
	return _D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION
}

func (g *graphics12) NewShader(desc *graphicsdriver.ShaderDescriptor) (graphicsdriver.Shader, error) {
	vsh, err := vertexShaderBlob(desc)
	if err != nil {
		return nil, err
	}
	psh, err := compileShader(desc)
	if err != nil {
		return nil, err
	}

	s := &shader12{
		graphics:               g,
		userConstantBufferSize: userConstantBufferSize(desc),
		vertexShader:           vsh,
		pixelShader:            psh,
	}
	return s, nil
}

// FlipsY reports true: a texture's rows go down from clip-space Y = 1.
func (g *graphics12) FlipsY() bool {
	return true
}

func (g *graphics12) DrawTriangles(dstImage graphicsdriver.Image, srcs [graphics.ShaderSrcImageCount]graphicsdriver.Image, shaderImpl graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32, versions graphicsdriver.UniformVersions) error {
	if shaderImpl == nil {
		return fmt.Errorf("directx: shader is nil")
	}

	// When the frame's descriptors run out, wait for the GPU to finish the draws so far, which frees
	// them.
	if !g.pipelineStates.hasRoom(g.frameIndex) {
		if err := g.drainCurrentFrame(); err != nil {
			return err
		}
	}

	dst := dstImage.(*image12)
	var srcImages [graphics.ShaderSrcImageCount]*image12
	for i, s := range srcs {
		if s == nil {
			continue
		}
		src := s.(*image12)
		if src == dst {
			return errors.New("directx: destination and source images must be different")
		}
		srcImages[i] = src
	}

	if dst.surface != nil {
		dst.surface.drawn = true
	}
	var resourceBarriers []_D3D12_RESOURCE_BARRIER_Transition
	if rb, ok := dst.transiteState(_D3D12_RESOURCE_STATE_RENDER_TARGET); ok {
		resourceBarriers = append(resourceBarriers, rb)
	}

	for _, src := range srcImages {
		if src == nil {
			continue
		}
		if rb, ok := src.transiteState(_D3D12_RESOURCE_STATE_PIXEL_SHADER_RESOURCE); ok {
			resourceBarriers = append(resourceBarriers, rb)
		}
	}

	if len(resourceBarriers) > 0 {
		g.drawCommandList.ResourceBarrier(resourceBarriers)
	}

	backIdx := -1
	if dst.screen {
		backIdx = dst.surface.backBufferIndex
	}
	if g.drawListState.rtvImage != dst || g.drawListState.rtvBackIndex != backIdx {
		if err := dst.setAsRenderTarget(g.drawCommandList, g.device); err != nil {
			return err
		}
		g.drawListState.rtvImage = dst
		g.drawListState.rtvBackIndex = backIdx
	}

	shader := shaderImpl.(*shader12)

	w, h := dst.internalSize()
	g.needFlushDrawCommandList = true
	if g.drawListState.viewportW != w || g.drawListState.viewportH != h {
		g.drawCommandList.RSSetViewports([]_D3D12_VIEWPORT{
			{
				TopLeftX: 0,
				TopLeftY: 0,
				Width:    float32(w),
				Height:   float32(h),
				MinDepth: _D3D12_MIN_DEPTH,
				MaxDepth: _D3D12_MAX_DEPTH,
			},
		})
		g.drawListState.viewportW = w
		g.drawListState.viewportH = h
	}
	// The buffers of a batch stay the same over its draws. Buffers are released only after the GPU
	// completed the command lists that used them, and drawListState is cleared at every Reset, so a
	// view here cannot stand for a released buffer.
	vb := g.vertices[g.frameIndex][len(g.vertices[g.frameIndex])-1]
	vbv := _D3D12_VERTEX_BUFFER_VIEW{
		BufferLocation: vb.gpuAddress,
		SizeInBytes:    vb.sizeInBytes,
		StrideInBytes:  graphics.VertexFloatCount * uint32(unsafe.Sizeof(float32(0))),
	}
	if g.drawListState.vertexBuffer != vbv {
		g.drawCommandList.IASetVertexBuffers(0, []_D3D12_VERTEX_BUFFER_VIEW{vbv})
		g.drawListState.vertexBuffer = vbv
	}
	ib := g.indices[g.frameIndex][len(g.indices[g.frameIndex])-1]
	ibv := _D3D12_INDEX_BUFFER_VIEW{
		BufferLocation: ib.gpuAddress,
		SizeInBytes:    ib.sizeInBytes,
		Format:         _DXGI_FORMAT_R32_UINT,
	}
	if g.drawListState.indexBuffer != ibv {
		g.drawCommandList.IASetIndexBuffer(&ibv)
		g.drawListState.indexBuffer = ibv
	}

	// The projection is already flipped for Direct3D's render targets (see FlipsY).
	if err := g.pipelineStates.drawTriangles(g.device, g.drawCommandList, &g.drawListState, g.frameIndex, dst.renderTargetFormat(), srcImages, shader, dstRegions, uniforms[:graphics.PreservedUniformDwordCount], uniforms[graphics.PreservedUniformDwordCount:], versions, blend, indexOffset); err != nil {
		return err
	}

	return nil
}
