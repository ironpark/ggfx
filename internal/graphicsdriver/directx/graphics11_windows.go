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
	"github.com/ironpark/ggfx/internal/shader"
)

var inputElementDescsForDX11 []_D3D11_INPUT_ELEMENT_DESC

func init() {
	for i, f := range vertexAttributeFormats() {
		inputElementDescsForDX11 = append(inputElementDescsForDX11, _D3D11_INPUT_ELEMENT_DESC{
			SemanticName:         vertexAttributeSemanticName,
			SemanticIndex:        uint32(i),
			Format:               f,
			InputSlot:            0,
			AlignedByteOffset:    _D3D11_APPEND_ALIGNED_ELEMENT,
			InputSlotClass:       _D3D11_INPUT_PER_VERTEX_DATA,
			InstanceDataStepRate: 0,
		})
	}
}

type blendStateKey struct {
	blend     graphicsdriver.Blend
	writeMask uint8
}

type graphics11 struct {
	graphicsInfra *graphicsInfra

	device        *_ID3D11Device
	deviceContext *_ID3D11DeviceContext

	// surfaces are the surfaces that have a swap chain, in creation order.
	surfaces []*surface11

	// tmpSwapChains is a temporary slice of the swap chains to present.
	tmpSwapChains []*swapChain

	// vertexStages are the vertex shaders and input layouts the shaders share, by texture count.
	vertexStages map[int]*vertexStage11

	// internalConstantBuffer is the constant buffer for the internal uniform block, which every
	// shader declares the same. internalUniforms is what it holds, or empty when unknown.
	internalConstantBuffer *_ID3D11Buffer
	internalUniforms       []uint32

	// state is the pipeline state the last draw left bound.
	state drawState11

	vertexBuffer            *_ID3D11Buffer
	vertexBufferSizeInBytes uint32

	indexBuffer            *_ID3D11Buffer
	indexBufferSizeInBytes uint32

	// vertexBufferUse and indexBufferUse track the bytes the batches of recent frames sent, so that
	// the buffers give back the room a large batch took.
	vertexBufferUse recent.Peak
	indexBufferUse  recent.Peak

	rasterizerState *_ID3D11RasterizerState
	blendStates     map[blendStateKey]*_ID3D11BlendState
}

// drawState11 is the pipeline state the last draw bound, so that a draw sets only what changed, as
// the Metal driver's rceState does. The device context holds a reference to each bound object, so a
// pointer here cannot be reused by a new object while it is bound. The zero value means nothing is
// known; a path that binds or unbinds state behind DrawTriangles' back must reset it.
type drawState11 struct {
	bound bool

	viewportWidth  int
	viewportHeight int
	rtv            *_ID3D11RenderTargetView
	srvs           [graphics.ShaderSrcImageCount]*_ID3D11ShaderResourceView

	vertexShader    *_ID3D11VertexShader
	inputLayout     *_ID3D11InputLayout
	pixelShader     *_ID3D11PixelShader
	constantBuffers [2]*_ID3D11Buffer
	blendState      *_ID3D11BlendState
	scissor         _D3D11_RECT
	scissorValid    bool
}

// surface11 is the swap chain on one window.
type surface11 struct {
	graphics *graphics11
	window   windows.HWND

	// swapChain is nil until the first screen image is created.
	swapChain   *swapChain
	screenImage *image11

	// drawn reports whether the screen image was drawn to since the previous present.
	drawn bool
}

func newGraphics11(useWARP bool, useDebugLayer bool) (gr11 *graphics11, ferr error) {
	g := &graphics11{}

	driverType := _D3D_DRIVER_TYPE_HARDWARE
	if useWARP {
		driverType = _D3D_DRIVER_TYPE_WARP
	}

	var flags _D3D11_CREATE_DEVICE_FLAG
	if useDebugLayer {
		flags |= _D3D11_CREATE_DEVICE_DEBUG
	}

	// Avoid _D3D_FEATURE_LEVEL_11_1 as DirectX 11.0 doesn't recognize this.
	// Avoid _D3D_FEATURE_LEVEL_9_* for some shaders features (#1431).
	// Shader model 5.0 needs feature level 11.0.
	featureLevels := []_D3D_FEATURE_LEVEL{
		_D3D_FEATURE_LEVEL_11_0,
	}

	// Apparently, adapter must be nil if the driver type is not unknown. This is not documented explicitly.
	// https://learn.microsoft.com/en-us/windows/win32/api/d3d11/nf-d3d11-d3d11createdevice
	d, _, ctx, err := _D3D11CreateDevice(nil, driverType, 0, uint32(flags), featureLevels, true, true)
	if err != nil {
		return nil, err
	}
	g.device = (*_ID3D11Device)(d)
	defer func() {
		if ferr != nil {
			g.device.Release()
			g.device = nil
		}
	}()
	g.deviceContext = (*_ID3D11DeviceContext)(ctx)
	defer func() {
		if ferr != nil {
			g.deviceContext.Release()
			g.deviceContext = nil
		}
	}()

	// Get IDXGIFactory from the current device and use it, instead of CreateDXGIFactory.
	// Or, MakeWindowAssociation doesn't work well (#2661).
	dd, err := g.device.QueryInterface(&_IID_IDXGIDevice)
	if err != nil {
		return nil, err
	}
	dxgiDevice := (*_IDXGIDevice)(dd)
	defer dxgiDevice.Release()

	dxgiAdapter, err := dxgiDevice.GetAdapter()
	if err != nil {
		return nil, err
	}
	defer dxgiAdapter.Release()

	df, err := dxgiAdapter.GetParent(&_IID_IDXGIFactory)
	if err != nil {
		return nil, err
	}
	dxgiFactory := (*_IDXGIFactory)(df)

	gi, err := newGraphicsInfra(dxgiFactory)
	if err != nil {
		return nil, err
	}
	g.graphicsInfra = gi
	defer func() {
		if ferr != nil {
			g.graphicsInfra.release()
			g.graphicsInfra = nil
		}
	}()

	g.deviceContext.IASetPrimitiveTopology(_D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST)

	// Set the rasterizer state.
	if g.rasterizerState == nil {
		rs, err := g.device.CreateRasterizerState(&_D3D11_RASTERIZER_DESC{
			FillMode:              _D3D11_FILL_SOLID,
			CullMode:              _D3D11_CULL_NONE,
			FrontCounterClockwise: 0,
			DepthBias:             0,
			DepthBiasClamp:        0,
			SlopeScaledDepthBias:  0,
			DepthClipEnable:       0,
			ScissorEnable:         1,
			MultisampleEnable:     0,
			AntialiasedLineEnable: 0,
		})
		if err != nil {
			return nil, err
		}
		g.rasterizerState = rs
		defer func() {
			if ferr != nil {
				g.rasterizerState.Release()
				g.rasterizerState = nil
			}
		}()
	}
	g.deviceContext.RSSetState(g.rasterizerState)

	return g, nil
}

func (g *graphics11) Initialize() error {
	return nil
}

func (g *graphics11) ColorSpace() color.ColorSpace {
	return color.ColorSpaceSRGB
}

func (g *graphics11) Begin() error {
	return nil
}

// IsOccluded reports whether the screen is invisible: whether none of the windows presented at the
// last present was visible.
func (g *graphics11) IsOccluded() bool {
	if g.graphicsInfra == nil {
		return false
	}
	return g.graphicsInfra.occluded.Load()
}

func (g *graphics11) End(mode graphicsdriver.FlushMode) error {
	if mode != graphicsdriver.FlushModePresent {
		return nil
	}

	// Present every surface drawn since the previous present. A surface that was not drawn keeps
	// showing its previous frame.
	swapChains := g.tmpSwapChains[:0]
	for _, s := range g.surfaces {
		if !s.drawn {
			continue
		}
		swapChains = append(swapChains, s.swapChain)
		s.drawn = false
	}
	err := g.graphicsInfra.present(swapChains)
	// A flip-model present unbinds the back buffer from the pipeline.
	g.state = drawState11{}
	clear(swapChains)
	g.tmpSwapChains = swapChains[:0]
	if err != nil {
		return err
	}

	return nil
}

// NewSurface records the window. Its swap chain is created with the first screen image, on the
// rendering thread.
func (g *graphics11) NewSurface(target any, transparent bool) (graphicsdriver.Surface, error) {
	if transparent {
		return nil, errors.New("directx: a transparent surface is not supported")
	}
	w, err := surfaceTarget(target)
	if err != nil {
		return nil, err
	}
	return &surface11{
		graphics: g,
		window:   w,
	}, nil
}

func (s *surface11) NewScreenImage(width, height int) (graphicsdriver.Image, error) {
	return s.graphics.newScreenImage(s, width, height)
}

// FlipsY reports true: a back buffer's rows go down from clip-space Y = 1.
func (s *surface11) FlipsY() bool { return true }

// Dispose releases the swap chain. Dispose is called on the rendering thread.
func (s *surface11) Dispose() {
	g := s.graphics
	g.surfaces = slices.DeleteFunc(g.surfaces, func(t *surface11) bool { return t == s })

	if s.screenImage != nil {
		// Release the references to the swap chain's buffer. The image itself stays until its owner
		// disposes it.
		s.screenImage.disposeBuffers()
		s.screenImage.surface = nil
		s.screenImage = nil
	}
	if s.swapChain != nil {
		// Unbind the render target that might still be the swap chain's buffer. Direct3D 11 destroys
		// a swap chain lazily, so flush the device context to destroy it now.
		g.deviceContext.OMSetRenderTargets([]*_ID3D11RenderTargetView{nil}, nil)
		g.state = drawState11{}
		s.swapChain.release()
		s.swapChain = nil
		g.deviceContext.Flush()
	}
	s.drawn = false
}

// SupportsDirectComposition reports whether this driver can present through DirectComposition.
// It must be called before the window is created, as it decides whether the window can be created
// without a redirection surface.
func (g *graphics11) SupportsDirectComposition() bool {
	return g.graphicsInfra.supportsComposition(unsafe.Pointer(g.device))
}

func (g *graphics11) SetVertices(vertices []float32, indices []uint32) error {
	// The buffers are mapped with WRITE_DISCARD, so replacing one does not wait for the GPU.
	if size := uint32(g.vertexBufferUse.Capacity(int(g.vertexBufferSizeInBytes), len(vertices)*int(unsafe.Sizeof(vertices[0])), graphicsdriver.MinVertexBufferBytes)); g.vertexBuffer == nil || g.vertexBufferSizeInBytes != size {
		if g.vertexBuffer != nil {
			g.vertexBuffer.Release()
			g.vertexBuffer = nil
		}
		b, err := g.device.CreateBuffer(&_D3D11_BUFFER_DESC{
			ByteWidth:      size,
			Usage:          _D3D11_USAGE_DYNAMIC,
			BindFlags:      uint32(_D3D11_BIND_VERTEX_BUFFER),
			CPUAccessFlags: uint32(_D3D11_CPU_ACCESS_WRITE),
		}, nil)
		if err != nil {
			return err
		}
		g.vertexBuffer = b
		g.vertexBufferSizeInBytes = size
		g.deviceContext.IASetVertexBuffers(0, []*_ID3D11Buffer{g.vertexBuffer},
			[]uint32{graphics.VertexFloatCount * uint32(unsafe.Sizeof(vertices[0]))}, []uint32{0})
	}
	if size := uint32(g.indexBufferUse.Capacity(int(g.indexBufferSizeInBytes), len(indices)*int(unsafe.Sizeof(indices[0])), graphicsdriver.MinVertexBufferBytes)); g.indexBuffer == nil || g.indexBufferSizeInBytes != size {
		if g.indexBuffer != nil {
			g.indexBuffer.Release()
			g.indexBuffer = nil
		}
		b, err := g.device.CreateBuffer(&_D3D11_BUFFER_DESC{
			ByteWidth:      size,
			Usage:          _D3D11_USAGE_DYNAMIC,
			BindFlags:      uint32(_D3D11_BIND_INDEX_BUFFER),
			CPUAccessFlags: uint32(_D3D11_CPU_ACCESS_WRITE),
		}, nil)
		if err != nil {
			return err
		}
		g.indexBuffer = b
		g.indexBufferSizeInBytes = size
		g.deviceContext.IASetIndexBuffer(g.indexBuffer, _DXGI_FORMAT_R32_UINT, 0)
	}

	// Copy the vertices data.
	{
		var mapped _D3D11_MAPPED_SUBRESOURCE
		if err := g.deviceContext.Map(unsafe.Pointer(g.vertexBuffer), 0, _D3D11_MAP_WRITE_DISCARD, 0, &mapped); err != nil {
			return err
		}
		copy(unsafe.Slice((*float32)(mapped.pData), len(vertices)), vertices)
		g.deviceContext.Unmap(unsafe.Pointer(g.vertexBuffer), 0)
	}

	// Copy the indices data.
	{
		var mapped _D3D11_MAPPED_SUBRESOURCE
		if err := g.deviceContext.Map(unsafe.Pointer(g.indexBuffer), 0, _D3D11_MAP_WRITE_DISCARD, 0, &mapped); err != nil {
			return err
		}
		copy(unsafe.Slice((*uint32)(mapped.pData), len(indices)), indices)
		g.deviceContext.Unmap(unsafe.Pointer(g.indexBuffer), 0)
	}

	return nil
}

// SupportsPixelFormat reports only PixelFormatRGBA8: Direct3D 11 cannot swizzle a texture's
// components, which PixelFormatA8 needs.
func (g *graphics11) SupportsPixelFormat(format graphicsdriver.PixelFormat) bool {
	return format == graphicsdriver.PixelFormatRGBA8
}

func (g *graphics11) NewImage(width, height int, format graphicsdriver.PixelFormat) (graphicsdriver.Image, error) {
	if format != graphicsdriver.PixelFormatRGBA8 {
		return nil, fmt.Errorf("directx: unsupported pixel format %s", format)
	}
	t, err := g.device.CreateTexture2D(&_D3D11_TEXTURE2D_DESC{
		Width:     uint32(graphics.InternalImageSize(width)),
		Height:    uint32(graphics.InternalImageSize(height)),
		MipLevels: 1, // 0 doesn't work when shrinking the image.
		ArraySize: 1,
		Format:    _DXGI_FORMAT_R8G8B8A8_UNORM,
		SampleDesc: _DXGI_SAMPLE_DESC{
			Count:   1,
			Quality: 0,
		},
		Usage:          _D3D11_USAGE_DEFAULT,
		BindFlags:      uint32(_D3D11_BIND_SHADER_RESOURCE | _D3D11_BIND_RENDER_TARGET),
		CPUAccessFlags: 0,
		MiscFlags:      0,
	}, nil)
	if err != nil {
		return nil, err
	}

	i := &image11{
		graphics: g,
		width:    width,
		height:   height,
		texture:  t,
	}
	return i, nil
}

func (g *graphics11) newScreenImage(s *surface11, width, height int) (graphicsdriver.Image, error) {
	if s.screenImage != nil {
		// Dispose the screen image, so that no reference to the swap chain's buffer remains before
		// ResizeBuffers.
		s.screenImage.Dispose()
		s.screenImage = nil
	}

	if s.swapChain == nil {
		sc, err := g.graphicsInfra.newSwapChain(width, height, unsafe.Pointer(g.device), s.window)
		if err != nil {
			return nil, err
		}
		s.swapChain = sc
		g.surfaces = append(g.surfaces, s)
	} else if !s.swapChain.canReuseBuffers(width, height) {
		// Resize the swap chain now, before this frame renders the screen, so that the frame renders
		// and presents at the new size. Presenting a stale-size buffer while the window is already at
		// the new size makes the compositor scale it for a moment (#3477).
		//
		// The render target might still be the old screen image's buffer.
		g.deviceContext.OMSetRenderTargets([]*_ID3D11RenderTargetView{nil}, nil)
		g.state = drawState11{}
		if err := s.swapChain.resize(width, height); err != nil {
			return nil, err
		}
	}

	t, err := s.swapChain.getBuffer(0, &_IID_ID3D11Texture2D)
	if err != nil {
		return nil, err
	}

	i := &image11{
		graphics: g,
		width:    width,
		height:   height,
		screen:   true,
		surface:  s,
		texture:  (*_ID3D11Texture2D)(t),
	}
	s.screenImage = i
	return i, nil
}

func (g *graphics11) NeedsClearingScreen() bool {
	// TODO: Confirm this is really true.
	return true
}

// MaxImageSize returns D3D11_REQ_TEXTURE2D_U_OR_V_DIMENSION: newGraphics11 requests feature level
// 11_0 only, so the device has it.
func (g *graphics11) MaxImageSize() int {
	return 16384
}

func (g *graphics11) NewShader(program *shader.Program) (_ graphicsdriver.Shader, ferr error) {
	psh, err := compileShader(program)
	if err != nil {
		return nil, err
	}
	defer func() {
		if ferr != nil {
			psh.Release()
		}
	}()
	vs, err := g.vertexStage(program.TextureCount)
	if err != nil {
		return nil, err
	}

	s := &shader11{
		graphics:               g,
		userConstantBufferSize: userConstantBufferSize(program),
		pixelShaderBlob:        psh,
		vertexStage:            vs,
	}
	return s, nil
}

// FlipsY reports true: a texture's rows go down from clip-space Y = 1.
func (g *graphics11) FlipsY() bool {
	return true
}

func (g *graphics11) DrawTriangles(dstImage graphicsdriver.Image, srcImages [graphics.ShaderSrcImageCount]graphicsdriver.Image, shaderImpl graphicsdriver.Shader, dstRegions []graphicsdriver.DstRegion, indexOffset int, blend graphicsdriver.Blend, uniforms []uint32) error {
	dst := dstImage.(*image11)
	if dst.surface != nil {
		dst.surface.drawn = true
	}
	var srcs [graphics.ShaderSrcImageCount]*image11
	var srvs [graphics.ShaderSrcImageCount]*_ID3D11ShaderResourceView
	for i, src := range srcImages {
		if src == nil {
			continue
		}
		img := src.(*image11)
		srcs[i] = img
		srv, err := img.getShaderResourceView()
		if err != nil {
			return err
		}
		srvs[i] = srv
	}
	rtv, err := dst.ensureRenderTargetView()
	if err != nil {
		return err
	}

	shader := shaderImpl.(*shader11)
	ps, err := shader.ensurePixelShader()
	if err != nil {
		return err
	}
	userCB, err := shader.ensureUserConstantBuffer()
	if err != nil {
		return err
	}
	if g.internalConstantBuffer == nil {
		cb, err := g.newConstantBuffer(internalConstantBufferSize)
		if err != nil {
			return err
		}
		g.internalConstantBuffer = cb
	}
	bs, err := g.blendState(blend)
	if err != nil {
		return err
	}

	// Bind only what differs from the last draw. Nothing below fails, so st matches the device
	// context once it is done.
	st := &g.state
	known := st.bound
	ctx := g.deviceContext

	if !known || st.rtv != rtv {
		// Direct3D 11 unbinds a shader input whose resource becomes the render target. Unbind the
		// inputs first when dst is one of them, so that the runtime changes nothing behind st and the
		// debug layer does not warn. With nothing known, unbind them as well.
		if !known || (dst.shaderResourceView != nil && slices.Contains(st.srvs[:], dst.shaderResourceView)) {
			st.srvs = [graphics.ShaderSrcImageCount]*_ID3D11ShaderResourceView{}
			ctx.PSSetShaderResources(0, st.srvs[:])
		}
		ctx.OMSetRenderTargets([]*_ID3D11RenderTargetView{rtv}, nil)
		st.rtv = rtv
	}
	if !known || st.srvs != srvs {
		ctx.PSSetShaderResources(0, srvs[:])
		st.srvs = srvs
	}

	if w, h := dst.internalSize(); !known || st.viewportWidth != w || st.viewportHeight != h {
		ctx.RSSetViewports([]_D3D11_VIEWPORT{
			{
				TopLeftX: 0,
				TopLeftY: 0,
				Width:    float32(w),
				Height:   float32(h),
				MinDepth: 0,
				MaxDepth: 1,
			},
		})
		st.viewportWidth, st.viewportHeight = w, h
	}

	if vs := shader.vertexStage; !known || st.vertexShader != vs.vertexShader || st.inputLayout != vs.inputLayout {
		ctx.VSSetShader(vs.vertexShader, nil)
		ctx.IASetInputLayout(vs.inputLayout)
		st.vertexShader = vs.vertexShader
		st.inputLayout = vs.inputLayout
	}
	if !known || st.pixelShader != ps {
		ctx.PSSetShader(ps, nil)
		st.pixelShader = ps
	}

	// A shader without a user uniform block leaves slot 1 as it was; it does not read it.
	if cbs := [2]*_ID3D11Buffer{g.internalConstantBuffer, userCB}; !known || st.constantBuffers != cbs {
		n := 1
		if userCB != nil {
			n = 2
		}
		ctx.VSSetConstantBuffers(0, cbs[:n])
		ctx.PSSetConstantBuffers(0, cbs[:n])
		st.constantBuffers = cbs
	}

	if !known || st.blendState != bs {
		ctx.OMSetBlendState(bs, nil, 0xffffffff)
		st.blendState = bs
	}

	// When a source is the destination itself, the runtime leaves that input unbound, which st does
	// not record. Forget st then.
	st.bound = !slices.Contains(srcs[:], dst)

	// The internal uniform block comes first, then the user's block. Both are already laid out as
	// the shader expects, the projection flipped for Direct3D's render targets (see FlipsY).
	if err := g.uploadConstantBuffer(g.internalConstantBuffer, uniforms[:graphics.PreservedUniformDwordCount], &g.internalUniforms); err != nil {
		return err
	}
	if userCB != nil {
		if err := g.uploadConstantBuffer(userCB, uniforms[graphics.PreservedUniformDwordCount:], &shader.userUniforms); err != nil {
			return err
		}
	}

	for _, dstRegion := range dstRegions {
		r := _D3D11_RECT{
			left:   int32(dstRegion.Region.Min.X),
			top:    int32(dstRegion.Region.Min.Y),
			right:  int32(dstRegion.Region.Max.X),
			bottom: int32(dstRegion.Region.Max.Y),
		}
		if !st.scissorValid || st.scissor != r {
			ctx.RSSetScissorRects([]_D3D11_RECT{r})
			st.scissor = r
			st.scissorValid = true
		}
		g.deviceContext.DrawIndexed(uint32(dstRegion.IndexCount), uint32(indexOffset), 0)
		indexOffset += dstRegion.IndexCount
	}

	return nil
}

func (g *graphics11) blendState(blend graphicsdriver.Blend) (*_ID3D11BlendState, error) {
	writeMask := uint8(_D3D11_COLOR_WRITE_ENABLE_ALL)
	key := blendStateKey{
		blend:     blend,
		writeMask: writeMask,
	}
	if bs, ok := g.blendStates[key]; ok {
		return bs, nil
	}

	bs, err := g.device.CreateBlendState(&_D3D11_BLEND_DESC{
		AlphaToCoverageEnable:  0,
		IndependentBlendEnable: 0,
		RenderTarget: [8]_D3D11_RENDER_TARGET_BLEND_DESC{
			{
				BlendEnable:           1,
				SrcBlend:              blendFactorToD3D[_D3D11_BLEND](blend.BlendFactorSourceRGB, false),
				DestBlend:             blendFactorToD3D[_D3D11_BLEND](blend.BlendFactorDestinationRGB, false),
				BlendOp:               blendOperationToD3D[_D3D11_BLEND_OP](blend.BlendOperationRGB),
				SrcBlendAlpha:         blendFactorToD3D[_D3D11_BLEND](blend.BlendFactorSourceAlpha, true),
				DestBlendAlpha:        blendFactorToD3D[_D3D11_BLEND](blend.BlendFactorDestinationAlpha, true),
				BlendOpAlpha:          blendOperationToD3D[_D3D11_BLEND_OP](blend.BlendOperationAlpha),
				RenderTargetWriteMask: writeMask,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	if g.blendStates == nil {
		g.blendStates = map[blendStateKey]*_ID3D11BlendState{}
	}
	g.blendStates[key] = bs
	return bs, nil
}
