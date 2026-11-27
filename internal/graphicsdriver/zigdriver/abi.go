//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"structs"
	"unsafe"
)

const DriverABIVersion uint32 = 2

// Status codes returned by the Zig driver C ABI (GgfxStatus).
const (
	StatusOK                    int32 = 0
	StatusErrInvalidArgument    int32 = 1
	StatusErrABIMismatch        int32 = 2
	StatusErrBackendUnavailable int32 = 3
	StatusErrOutOfMemory        int32 = 4
	StatusErrDeviceLost         int32 = 5
	StatusErrUnknownHandle      int32 = 6
	StatusErrDuplicateHandle    int32 = 7
	StatusErrShaderCompile      int32 = 8
	StatusErrUnsupported        int32 = 9
	StatusErrMalformedBatch     int32 = 10
	StatusErrSurfaceLost        int32 = 11
	StatusErrInternal           int32 = 12
	StatusErrLeakDetected       int32 = 13
)

// Backend identifiers (GgfxBackend).
const (
	BackendAuto   uint32 = 0
	BackendMetal  uint32 = 1
	BackendDX12   uint32 = 2
	BackendVulkan uint32 = 3
	BackendNull   uint32 = 255
)

// Flush modes (matches graphicsdriver.FlushMode).
const (
	FlushIntermediate uint32 = 0
	FlushEndFrame     uint32 = 1
	FlushPresent      uint32 = 2
)

// Pixel formats (matches graphicsdriver.PixelFormat).
const (
	PixelFormatRGBA8 uint32 = 0
	PixelFormatA8    uint32 = 1
)

// Shader formats.
const (
	ShaderFormatMSL   uint32 = 1
	ShaderFormatHLSL  uint32 = 2
	ShaderFormatDXBC  uint32 = 3
	ShaderFormatDXIL  uint32 = 4
	ShaderFormatSPIRV uint32 = 5
)

// Surface kinds.
const (
	SurfaceKindNSView  uint32 = 1
	SurfaceKindHWND    uint32 = 2
	SurfaceKindX11     uint32 = 3
	SurfaceKindWayland uint32 = 4
)

// Main-thread presentation modes.
const (
	MainThreadInline   uint32 = 0
	MainThreadDeferred uint32 = 1
	MainThreadDispatch uint32 = 2
)

// Native surface pixel formats.
const (
	NativeFormatBGRA8 uint32 = 1
	NativeFormatRGBA8 uint32 = 2
)

// Command opcodes.
const (
	OpSetVertices    uint8 = 1
	OpNewImage       uint8 = 2
	OpNewScreenImage uint8 = 3
	OpDisposeImage   uint8 = 4
	OpDisposeShader  uint8 = 5
	OpDisposeSurface uint8 = 6
	OpWritePixels    uint8 = 7
	OpDrawTriangles  uint8 = 8
	OpSetDamage      uint8 = 9
)

// DeviceOptions.Flags bits.
const (
	DeviceFlagDebug            uint32 = 1 << 0
	DeviceFlagWARP             uint32 = 1 << 1
	DeviceFlagDRED             uint32 = 1 << 2
	DeviceFlagMetalNoIOSurface uint32 = 1 << 3
	DeviceFlagLowPower         uint32 = 1 << 4
	DeviceFlagAllowDXIL        uint32 = 1 << 5
)

// DriverCaps.Flags bits.
const (
	CapFlagFlipsY              uint32 = 1 << 0
	CapFlagSupportsA8          uint32 = 1 << 1
	CapFlagNeedsClearingScreen uint32 = 1 << 2
	CapFlagDirectComposition   uint32 = 1 << 3
	CapFlagUnifiedMemory       uint32 = 1 << 4
	CapFlagKeptSurfaces        uint32 = 1 << 5
	CapFlagDamageSurfaces      uint32 = 1 << 6
	CapFlagSharedUploadArena   uint32 = 1 << 7
)

// UploadArena.Flags bits.
const (
	ArenaFlagGPUMapped uint32 = 1 << 0
	ArenaFlagCoherent  uint32 = 1 << 1
)

// SubmitResult.Flags bits.
const (
	ResultFlagOccluded               uint32 = 1 << 0
	ResultFlagNeedsMainThreadPresent uint32 = 1 << 1
)

// ShaderDesc.Flags bits.
const (
	ShaderFlagFragmentReadsInternal uint32 = 1 << 0
)

// SurfaceDesc.Flags bits.
const (
	SurfaceFlagTransparent uint32 = 1 << 0
)

// SurfaceCaps.Flags bits.
const (
	SurfaceCapFlagFlipsY      uint32 = 1 << 0
	SurfaceCapFlagReadable    uint32 = 1 << 1
	SurfaceCapFlagKept        uint32 = 1 << 2
	SurfaceCapFlagDamage      uint32 = 1 << 3
	SurfaceCapFlagWindowState uint32 = 1 << 4
)

// Window state flags.
const (
	WindowStateLiveResizing uint32 = 1 << 0
	WindowStateFullscreen   uint32 = 1 << 1
	WindowStateHidden       uint32 = 1 << 2
)

// Miscellaneous ABI constants.
const (
	DamageFull            uint32 = 0xFFFFFFFF
	InternalUniformDwords uint32 = 72
	InternalUniformBytes  uint32 = InternalUniformDwords * 4
	MaxUserUniformDwords  uint32 = 16384
	MaxSrcImages                 = 4
)

// Rect mirrors GgfxRect (16 B).
type Rect struct {
	_  structs.HostLayout
	X0 int32
	Y0 int32
	X1 int32
	Y1 int32
}

// Blend mirrors GgfxBlend (6 B).
type Blend struct {
	_      structs.HostLayout
	SrcRGB uint8
	SrcA   uint8
	DstRGB uint8
	DstA   uint8
	OpRGB  uint8
	OpA    uint8
}

// DeviceOptions mirrors GgfxDeviceOptions (48 B).
type DeviceOptions struct {
	_              structs.HostLayout
	StructSize     uint32
	ABIVersion     uint32
	Backend        uint32
	ColorSpace     uint32
	Flags          uint32
	FramesInFlight uint32
	DXFeatureLevel uint32
	MainThreadMode uint32
	Reserved       [2]uint64
}

// DriverCaps mirrors GgfxDriverCaps (96 B).
type DriverCaps struct {
	_              structs.HostLayout
	StructSize     uint32
	Backend        uint32
	MaxImageSize   uint32
	Flags          uint32
	ShaderFormat   uint32
	ShaderVersion  uint32
	ColorSpace     uint32
	FramesInFlight uint32
	DeviceName     [64]byte
}

// CmdHeader mirrors GgfxCmdHeader (8 B).
type CmdHeader struct {
	_     structs.HostLayout
	Op    uint8
	Flags uint8
	Aux   uint16
	Size  uint32
}

// Batch mirrors GgfxBatch (48 B).
type Batch struct {
	_          structs.HostLayout
	StructSize uint32
	CmdCount   uint32
	Cmds       uintptr
	CmdsLen    uint64
	Payload    uintptr
	PayloadLen uint64
	Sequence   uint64
}

// SubmitResult mirrors GgfxSubmitResult (24 B).
type SubmitResult struct {
	_                 structs.HostLayout
	Flags             uint32
	PresentedSurfaces uint32
	CompletedFrames   uint64
	Reserved          uint64
}

// ShaderStage mirrors GgfxShaderStage (32 B).
type ShaderStage struct {
	_        structs.HostLayout
	Code     uintptr
	CodeLen  uint64
	Entry    uintptr
	EntryLen uint32
	Reserved uint32
}

// ShaderDesc mirrors GgfxShaderDesc (88 B).
type ShaderDesc struct {
	_                 structs.HostLayout
	StructSize        uint32
	Format            uint32
	TextureCount      uint32
	UserUniformDwords uint32
	Flags             uint32
	Reserved          uint32
	Vertex            ShaderStage
	Fragment          ShaderStage
}

// SurfaceDesc mirrors GgfxSurfaceDesc (40 B).
type SurfaceDesc struct {
	_          structs.HostLayout
	StructSize uint32
	Kind       uint32
	Flags      uint32
	Reserved   uint32
	Handle1    uint64
	Handle2    uint64
	WidthHint  uint32
	HeightHint uint32
}

// SurfaceCaps mirrors GgfxSurfaceCaps (16 B).
type SurfaceCaps struct {
	_            structs.HostLayout
	StructSize   uint32
	Flags        uint32
	NativeFormat uint32
	Reserved     uint32
}

// ReadRegion mirrors GgfxReadRegion (32 B).
type ReadRegion struct {
	_      structs.HostLayout
	Rect   Rect
	Dst    uintptr
	DstLen uint64
}

// DebugStats mirrors GgfxDebugStats (64 B).
type DebugStats struct {
	_             structs.HostLayout
	LiveImages    uint64
	LiveShaders   uint64
	LiveSurfaces  uint64
	LivePipelines uint64
	Submits       uint64
	Draws         uint64
	BindsSkipped  uint64
	UploadedBytes uint64
}

// UploadArena mirrors GgfxUploadArena (40 B).
type UploadArena struct {
	_                 structs.HostLayout
	StructSize        uint32
	ArenaID           uint32
	Ptr               uintptr
	Capacity          uint64
	Alignment         uint32
	Flags             uint32
	RowPitchAlignment uint32
	Reserved          uint32
}

// CmdSetVertices mirrors GgfxCmdSetVertices (32 B).
type CmdSetVertices struct {
	_                structs.HostLayout
	Hdr              CmdHeader
	Vertices         uintptr
	Indices          uintptr
	VertexFloatCount uint32
	IndexCount       uint32
}

// CmdNewImage mirrors GgfxCmdNewImage (32 B).
type CmdNewImage struct {
	_              structs.HostLayout
	Hdr            CmdHeader
	ID             uint32
	Width          uint32
	Height         uint32
	InternalWidth  uint32
	InternalHeight uint32
	Reserved       uint32
}

// CmdNewScreenImage mirrors GgfxCmdNewScreenImage (24 B).
type CmdNewScreenImage struct {
	_         structs.HostLayout
	Hdr       CmdHeader
	ID        uint32
	SurfaceID uint32
	Width     uint32
	Height    uint32
}

// CmdDisposeImage mirrors GgfxCmdDisposeImage (16 B).
type CmdDisposeImage struct {
	_        structs.HostLayout
	Hdr      CmdHeader
	ID       uint32
	Reserved uint32
}

// CmdDisposeShader mirrors GgfxCmdDisposeShader (16 B).
type CmdDisposeShader struct {
	_        structs.HostLayout
	Hdr      CmdHeader
	ID       uint32
	Reserved uint32
}

// CmdDisposeSurface mirrors GgfxCmdDisposeSurface (16 B).
type CmdDisposeSurface struct {
	_         structs.HostLayout
	Hdr       CmdHeader
	SurfaceID uint32
	Reserved  uint32
}

// PixelsRegion mirrors GgfxPixelsRegion (24 B).
type PixelsRegion struct {
	_             structs.HostLayout
	Rect          Rect
	PayloadOffset uint32
	PayloadLen    uint32
}

// CmdWritePixels mirrors GgfxCmdWritePixels (16 B, followed by PixelsRegion[Count]).
type CmdWritePixels struct {
	_       structs.HostLayout
	Hdr     CmdHeader
	ImageID uint32
	Count   uint32
}

// DrawRegion mirrors GgfxDrawRegion (24 B).
type DrawRegion struct {
	_          structs.HostLayout
	Rect       Rect
	IndexCount uint32
	Reserved   uint32
}

// CmdDrawTriangles mirrors GgfxCmdDrawTriangles (80 B, followed by DrawRegion[RegionCount]).
type CmdDrawTriangles struct {
	_               structs.HostLayout
	Hdr             CmdHeader
	DstID           uint32
	ShaderID        uint32
	SrcIDs          [MaxSrcImages]uint32
	IndexOffset     uint32
	RegionCount     uint32
	Blend           Blend
	Reserved0       uint16
	InternalVersion uint64
	UserVersion     uint64
	InternalOffset  uint32
	UserOffset      uint32
	UserDwords      uint32
	Reserved1       uint32
}

// CmdSetDamage mirrors GgfxCmdSetDamage (16 B, followed by Rect[Count] unless Count == DamageFull).
type CmdSetDamage struct {
	_         structs.HostLayout
	Hdr       CmdHeader
	SurfaceID uint32
	Count     uint32
}

// Compile-time exact-size assertions matching zig/include/ggfx_driver.h and zig/src/abi.zig.
const (
	_ = uint(unsafe.Sizeof(Rect{}) - 16)
	_ = uint(16 - unsafe.Sizeof(Rect{}))

	_ = uint(unsafe.Sizeof(Blend{}) - 6)
	_ = uint(6 - unsafe.Sizeof(Blend{}))

	_ = uint(unsafe.Sizeof(DeviceOptions{}) - 48)
	_ = uint(48 - unsafe.Sizeof(DeviceOptions{}))

	_ = uint(unsafe.Sizeof(DriverCaps{}) - 96)
	_ = uint(96 - unsafe.Sizeof(DriverCaps{}))

	_ = uint(unsafe.Sizeof(CmdHeader{}) - 8)
	_ = uint(8 - unsafe.Sizeof(CmdHeader{}))

	_ = uint(unsafe.Sizeof(Batch{}) - 48)
	_ = uint(48 - unsafe.Sizeof(Batch{}))

	_ = uint(unsafe.Sizeof(SubmitResult{}) - 24)
	_ = uint(24 - unsafe.Sizeof(SubmitResult{}))

	_ = uint(unsafe.Sizeof(ShaderStage{}) - 32)
	_ = uint(32 - unsafe.Sizeof(ShaderStage{}))

	_ = uint(unsafe.Sizeof(ShaderDesc{}) - 88)
	_ = uint(88 - unsafe.Sizeof(ShaderDesc{}))

	_ = uint(unsafe.Sizeof(SurfaceDesc{}) - 40)
	_ = uint(40 - unsafe.Sizeof(SurfaceDesc{}))

	_ = uint(unsafe.Sizeof(SurfaceCaps{}) - 16)
	_ = uint(16 - unsafe.Sizeof(SurfaceCaps{}))

	_ = uint(unsafe.Sizeof(ReadRegion{}) - 32)
	_ = uint(32 - unsafe.Sizeof(ReadRegion{}))

	_ = uint(unsafe.Sizeof(DebugStats{}) - 64)
	_ = uint(64 - unsafe.Sizeof(DebugStats{}))

	_ = uint(unsafe.Sizeof(UploadArena{}) - 40)
	_ = uint(40 - unsafe.Sizeof(UploadArena{}))

	_ = uint(unsafe.Sizeof(CmdSetVertices{}) - 32)
	_ = uint(32 - unsafe.Sizeof(CmdSetVertices{}))

	_ = uint(unsafe.Sizeof(CmdNewImage{}) - 32)
	_ = uint(32 - unsafe.Sizeof(CmdNewImage{}))

	_ = uint(unsafe.Sizeof(CmdNewScreenImage{}) - 24)
	_ = uint(24 - unsafe.Sizeof(CmdNewScreenImage{}))

	_ = uint(unsafe.Sizeof(CmdDisposeImage{}) - 16)
	_ = uint(16 - unsafe.Sizeof(CmdDisposeImage{}))

	_ = uint(unsafe.Sizeof(CmdDisposeShader{}) - 16)
	_ = uint(16 - unsafe.Sizeof(CmdDisposeShader{}))

	_ = uint(unsafe.Sizeof(CmdDisposeSurface{}) - 16)
	_ = uint(16 - unsafe.Sizeof(CmdDisposeSurface{}))

	_ = uint(unsafe.Sizeof(PixelsRegion{}) - 24)
	_ = uint(24 - unsafe.Sizeof(PixelsRegion{}))

	_ = uint(unsafe.Sizeof(CmdWritePixels{}) - 16)
	_ = uint(16 - unsafe.Sizeof(CmdWritePixels{}))

	_ = uint(unsafe.Sizeof(DrawRegion{}) - 24)
	_ = uint(24 - unsafe.Sizeof(DrawRegion{}))

	_ = uint(unsafe.Sizeof(CmdDrawTriangles{}) - 80)
	_ = uint(80 - unsafe.Sizeof(CmdDrawTriangles{}))

	_ = uint(unsafe.Sizeof(CmdSetDamage{}) - 16)
	_ = uint(16 - unsafe.Sizeof(CmdSetDamage{}))
)
