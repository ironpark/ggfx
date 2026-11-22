const std = @import("std");
const builtin = @import("builtin");
const dynlib = @import("../dynlib.zig");
const errors = @import("../errors.zig");
const win32 = @import("win32.zig");
const com = @import("com.zig");
const dxgi = @import("dxgi.zig");
const d3dcompiler = @import("d3dcompiler.zig");

const BOOL = win32.BOOL;
const HRESULT = win32.HRESULT;
const UINT = win32.UINT;
const UINT64 = win32.UINT64;
const HANDLE = win32.HANDLE;
const LUID = win32.LUID;
const RECT = win32.RECT;
const GUID = com.GUID;
const DXGI_FORMAT = dxgi.DXGI_FORMAT;
const DXGI_SAMPLE_DESC = dxgi.DXGI_SAMPLE_DESC;
const ID3DBlob = d3dcompiler.ID3DBlob;
const Error = errors.Error;
const Diag = errors.Diag;
const PlaceholderFn = *const fn () callconv(.winapi) void;

pub const D3D12_APPEND_ALIGNED_ELEMENT: UINT = 0xFFFF_FFFF;
pub const D3D12_DEFAULT_DEPTH_BIAS: i32 = 0;
pub const D3D12_DEFAULT_DEPTH_BIAS_CLAMP: f32 = 0.0;
pub const D3D12_DEFAULT_SLOPE_SCALED_DEPTH_BIAS: f32 = 0.0;
pub const D3D12_MAX_DEPTH: f32 = 1.0;
pub const D3D12_MIN_DEPTH: f32 = 0.0;
pub const D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION: UINT = 16384;
pub const D3D12_RESOURCE_BARRIER_ALL_SUBRESOURCES: UINT = 0xFFFF_FFFF;
pub const D3D12_CONSTANT_BUFFER_DATA_PLACEMENT_ALIGNMENT: usize = 256;
pub const D3D12_TEXTURE_DATA_PITCH_ALIGNMENT: usize = 256;
pub const D3D12_TEXTURE_DATA_PLACEMENT_ALIGNMENT: usize = 512;

// Shader 4-component mapping constants (d3d12.h):
// RGBA8 default (0, 1, 2, 3) | always-set-bit (1 << 12) = 0x1688
// A8 swizzle   (0, 0, 0, 0) | always-set-bit (1 << 12) = 0x1000
pub const D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING: UINT = 0x1688;
pub const D3D12_SHADER_4_COMPONENT_MAPPING_R_R_R_R: UINT = 0x1000;

pub const D3D_FEATURE_LEVEL = i32;
pub const D3D_FEATURE_LEVEL_11_0: D3D_FEATURE_LEVEL = 0xb000;
pub const D3D_FEATURE_LEVEL_11_1: D3D_FEATURE_LEVEL = 0xb100;
pub const D3D_FEATURE_LEVEL_12_0: D3D_FEATURE_LEVEL = 0xc000;
pub const D3D_FEATURE_LEVEL_12_1: D3D_FEATURE_LEVEL = 0xc100;
pub const D3D_FEATURE_LEVEL_12_2: D3D_FEATURE_LEVEL = 0xc200;

pub const D3D_PRIMITIVE_TOPOLOGY = i32;
pub const D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST: D3D_PRIMITIVE_TOPOLOGY = 4;

pub const D3D_ROOT_SIGNATURE_VERSION = i32;
pub const D3D_ROOT_SIGNATURE_VERSION_1_0: D3D_ROOT_SIGNATURE_VERSION = 0x1;

pub const D3D12_BLEND = i32;
pub const D3D12_BLEND_ZERO: D3D12_BLEND = 1;
pub const D3D12_BLEND_ONE: D3D12_BLEND = 2;
pub const D3D12_BLEND_SRC_COLOR: D3D12_BLEND = 3;
pub const D3D12_BLEND_INV_SRC_COLOR: D3D12_BLEND = 4;
pub const D3D12_BLEND_SRC_ALPHA: D3D12_BLEND = 5;
pub const D3D12_BLEND_INV_SRC_ALPHA: D3D12_BLEND = 6;
pub const D3D12_BLEND_DEST_ALPHA: D3D12_BLEND = 7;
pub const D3D12_BLEND_INV_DEST_ALPHA: D3D12_BLEND = 8;
pub const D3D12_BLEND_DEST_COLOR: D3D12_BLEND = 9;
pub const D3D12_BLEND_INV_DEST_COLOR: D3D12_BLEND = 10;
pub const D3D12_BLEND_SRC_ALPHA_SAT: D3D12_BLEND = 11;

pub const D3D12_BLEND_OP = i32;
pub const D3D12_BLEND_OP_ADD: D3D12_BLEND_OP = 1;
pub const D3D12_BLEND_OP_SUBTRACT: D3D12_BLEND_OP = 2;
pub const D3D12_BLEND_OP_REV_SUBTRACT: D3D12_BLEND_OP = 3;
pub const D3D12_BLEND_OP_MIN: D3D12_BLEND_OP = 4;
pub const D3D12_BLEND_OP_MAX: D3D12_BLEND_OP = 5;

pub const D3D12_COLOR_WRITE_ENABLE_ALL: u8 = 0x0F;

pub const D3D12_COMMAND_LIST_TYPE = i32;
pub const D3D12_COMMAND_LIST_TYPE_DIRECT: D3D12_COMMAND_LIST_TYPE = 0;

pub const D3D12_COMMAND_QUEUE_FLAGS = i32;
pub const D3D12_COMMAND_QUEUE_FLAG_NONE: D3D12_COMMAND_QUEUE_FLAGS = 0;

pub const D3D12_COMPARISON_FUNC = i32;
pub const D3D12_COMPARISON_FUNC_NEVER: D3D12_COMPARISON_FUNC = 1;
pub const D3D12_COMPARISON_FUNC_ALWAYS: D3D12_COMPARISON_FUNC = 8;

pub const D3D12_CONSERVATIVE_RASTERIZATION_MODE = i32;
pub const D3D12_CONSERVATIVE_RASTERIZATION_MODE_OFF: D3D12_CONSERVATIVE_RASTERIZATION_MODE = 0;

pub const D3D12_CPU_PAGE_PROPERTY = i32;
pub const D3D12_CPU_PAGE_PROPERTY_UNKNOWN: D3D12_CPU_PAGE_PROPERTY = 0;

pub const D3D12_CULL_MODE = i32;
pub const D3D12_CULL_MODE_NONE: D3D12_CULL_MODE = 1;

pub const D3D12_DEPTH_WRITE_MASK = i32;
pub const D3D12_DEPTH_WRITE_MASK_ZERO: D3D12_DEPTH_WRITE_MASK = 0;

pub const D3D12_DESCRIPTOR_HEAP_TYPE = i32;
pub const D3D12_DESCRIPTOR_HEAP_TYPE_CBV_SRV_UAV: D3D12_DESCRIPTOR_HEAP_TYPE = 0;
pub const D3D12_DESCRIPTOR_HEAP_TYPE_SAMPLER: D3D12_DESCRIPTOR_HEAP_TYPE = 1;
pub const D3D12_DESCRIPTOR_HEAP_TYPE_RTV: D3D12_DESCRIPTOR_HEAP_TYPE = 2;
pub const D3D12_DESCRIPTOR_HEAP_TYPE_DSV: D3D12_DESCRIPTOR_HEAP_TYPE = 3;

pub const D3D12_DESCRIPTOR_HEAP_FLAGS = i32;
pub const D3D12_DESCRIPTOR_HEAP_FLAG_NONE: D3D12_DESCRIPTOR_HEAP_FLAGS = 0;
pub const D3D12_DESCRIPTOR_HEAP_FLAG_SHADER_VISIBLE: D3D12_DESCRIPTOR_HEAP_FLAGS = 0x1;

pub const D3D12_DESCRIPTOR_RANGE_TYPE = i32;
pub const D3D12_DESCRIPTOR_RANGE_TYPE_SRV: D3D12_DESCRIPTOR_RANGE_TYPE = 0;
pub const D3D12_DESCRIPTOR_RANGE_TYPE_UAV: D3D12_DESCRIPTOR_RANGE_TYPE = 1;
pub const D3D12_DESCRIPTOR_RANGE_TYPE_CBV: D3D12_DESCRIPTOR_RANGE_TYPE = 2;
pub const D3D12_DESCRIPTOR_RANGE_TYPE_SAMPLER: D3D12_DESCRIPTOR_RANGE_TYPE = 3;

pub const D3D12_FENCE_FLAGS = i32;
pub const D3D12_FENCE_FLAG_NONE: D3D12_FENCE_FLAGS = 0;

pub const D3D12_FILL_MODE = i32;
pub const D3D12_FILL_MODE_SOLID: D3D12_FILL_MODE = 3;

pub const D3D12_HEAP_FLAGS = i32;
pub const D3D12_HEAP_FLAG_NONE: D3D12_HEAP_FLAGS = 0;

pub const D3D12_HEAP_TYPE = i32;
pub const D3D12_HEAP_TYPE_DEFAULT: D3D12_HEAP_TYPE = 1;
pub const D3D12_HEAP_TYPE_UPLOAD: D3D12_HEAP_TYPE = 2;
pub const D3D12_HEAP_TYPE_READBACK: D3D12_HEAP_TYPE = 3;

pub const D3D12_INDEX_BUFFER_STRIP_CUT_VALUE = i32;
pub const D3D12_INDEX_BUFFER_STRIP_CUT_VALUE_DISABLED: D3D12_INDEX_BUFFER_STRIP_CUT_VALUE = 0;

pub const D3D12_INPUT_CLASSIFICATION = i32;
pub const D3D12_INPUT_CLASSIFICATION_PER_VERTEX_DATA: D3D12_INPUT_CLASSIFICATION = 0;

pub const D3D12_LOGIC_OP = i32;
pub const D3D12_LOGIC_OP_NOOP: D3D12_LOGIC_OP = 4;

pub const D3D12_MEMORY_POOL = i32;
pub const D3D12_MEMORY_POOL_UNKNOWN: D3D12_MEMORY_POOL = 0;

pub const D3D12_PIPELINE_STATE_FLAGS = i32;
pub const D3D12_PIPELINE_STATE_FLAG_NONE: D3D12_PIPELINE_STATE_FLAGS = 0;

pub const D3D12_PRIMITIVE_TOPOLOGY_TYPE = i32;
pub const D3D12_PRIMITIVE_TOPOLOGY_TYPE_TRIANGLE: D3D12_PRIMITIVE_TOPOLOGY_TYPE = 3;

pub const D3D12_RESOURCE_BARRIER_FLAGS = i32;
pub const D3D12_RESOURCE_BARRIER_FLAG_NONE: D3D12_RESOURCE_BARRIER_FLAGS = 0;

pub const D3D12_RESOURCE_BARRIER_TYPE = i32;
pub const D3D12_RESOURCE_BARRIER_TYPE_TRANSITION: D3D12_RESOURCE_BARRIER_TYPE = 0;

pub const D3D12_RESOURCE_DIMENSION = i32;
pub const D3D12_RESOURCE_DIMENSION_BUFFER: D3D12_RESOURCE_DIMENSION = 1;
pub const D3D12_RESOURCE_DIMENSION_TEXTURE2D: D3D12_RESOURCE_DIMENSION = 3;

pub const D3D12_RESOURCE_FLAGS = i32;
pub const D3D12_RESOURCE_FLAG_NONE: D3D12_RESOURCE_FLAGS = 0;
pub const D3D12_RESOURCE_FLAG_ALLOW_RENDER_TARGET: D3D12_RESOURCE_FLAGS = 0x1;

pub const D3D12_RESOURCE_STATES = i32;
pub const D3D12_RESOURCE_STATE_COMMON: D3D12_RESOURCE_STATES = 0;
pub const D3D12_RESOURCE_STATE_PRESENT: D3D12_RESOURCE_STATES = 0;
pub const D3D12_RESOURCE_STATE_VERTEX_AND_CONSTANT_BUFFER: D3D12_RESOURCE_STATES = 0x1;
pub const D3D12_RESOURCE_STATE_INDEX_BUFFER: D3D12_RESOURCE_STATES = 0x2;
pub const D3D12_RESOURCE_STATE_RENDER_TARGET: D3D12_RESOURCE_STATES = 0x4;
pub const D3D12_RESOURCE_STATE_NON_PIXEL_SHADER_RESOURCE: D3D12_RESOURCE_STATES = 0x40;
pub const D3D12_RESOURCE_STATE_PIXEL_SHADER_RESOURCE: D3D12_RESOURCE_STATES = 0x80;
pub const D3D12_RESOURCE_STATE_INDIRECT_ARGUMENT: D3D12_RESOURCE_STATES = 0x200;
pub const D3D12_RESOURCE_STATE_COPY_DEST: D3D12_RESOURCE_STATES = 0x400;
pub const D3D12_RESOURCE_STATE_COPY_SOURCE: D3D12_RESOURCE_STATES = 0x800;
pub const D3D12_RESOURCE_STATE_GENERIC_READ: D3D12_RESOURCE_STATES =
    D3D12_RESOURCE_STATE_VERTEX_AND_CONSTANT_BUFFER |
    D3D12_RESOURCE_STATE_INDEX_BUFFER |
    D3D12_RESOURCE_STATE_NON_PIXEL_SHADER_RESOURCE |
    D3D12_RESOURCE_STATE_PIXEL_SHADER_RESOURCE |
    D3D12_RESOURCE_STATE_INDIRECT_ARGUMENT |
    D3D12_RESOURCE_STATE_COPY_SOURCE;

pub const D3D12_ROOT_PARAMETER_TYPE = i32;
pub const D3D12_ROOT_PARAMETER_TYPE_DESCRIPTOR_TABLE: D3D12_ROOT_PARAMETER_TYPE = 0;
pub const D3D12_ROOT_PARAMETER_TYPE_32BIT_CONSTANTS: D3D12_ROOT_PARAMETER_TYPE = 1;
pub const D3D12_ROOT_PARAMETER_TYPE_CBV: D3D12_ROOT_PARAMETER_TYPE = 2;
pub const D3D12_ROOT_PARAMETER_TYPE_SRV: D3D12_ROOT_PARAMETER_TYPE = 3;
pub const D3D12_ROOT_PARAMETER_TYPE_UAV: D3D12_ROOT_PARAMETER_TYPE = 4;

pub const D3D12_ROOT_SIGNATURE_FLAGS = i32;
pub const D3D12_ROOT_SIGNATURE_FLAG_ALLOW_INPUT_ASSEMBLER_INPUT_LAYOUT: D3D12_ROOT_SIGNATURE_FLAGS = 0x1;

pub const D3D12_SHADER_VISIBILITY = i32;
pub const D3D12_SHADER_VISIBILITY_ALL: D3D12_SHADER_VISIBILITY = 0;
pub const D3D12_SHADER_VISIBILITY_VERTEX: D3D12_SHADER_VISIBILITY = 1;
pub const D3D12_SHADER_VISIBILITY_PIXEL: D3D12_SHADER_VISIBILITY = 5;

pub const D3D12_SRV_DIMENSION = i32;
pub const D3D12_SRV_DIMENSION_TEXTURE2D: D3D12_SRV_DIMENSION = 4;

pub const D3D12_STENCIL_OP = i32;
pub const D3D12_STENCIL_OP_KEEP: D3D12_STENCIL_OP = 1;

pub const D3D12_TEXTURE_COPY_TYPE = i32;
pub const D3D12_TEXTURE_COPY_TYPE_SUBRESOURCE_INDEX: D3D12_TEXTURE_COPY_TYPE = 0;
pub const D3D12_TEXTURE_COPY_TYPE_PLACED_FOOTPRINT: D3D12_TEXTURE_COPY_TYPE = 1;

pub const D3D12_TEXTURE_LAYOUT = i32;
pub const D3D12_TEXTURE_LAYOUT_UNKNOWN: D3D12_TEXTURE_LAYOUT = 0;
pub const D3D12_TEXTURE_LAYOUT_ROW_MAJOR: D3D12_TEXTURE_LAYOUT = 1;

pub const D3D12_DRED_ENABLEMENT = i32;
pub const D3D12_DRED_ENABLEMENT_FORCED_ON: D3D12_DRED_ENABLEMENT = 2;

pub const D3D12_GPU_VIRTUAL_ADDRESS = u64;

// D3D12 IIDs (verified against d3d12.h and internal/graphicsdriver/directx/d3d12_windows.go)
pub const IID_ID3D12CommandAllocator = GUID{
    .Data1 = 0x6102dee4,
    .Data2 = 0xaf59,
    .Data3 = 0x4b09,
    .Data4 = .{ 0xb9, 0x99, 0xb4, 0x4d, 0x73, 0xf0, 0x9b, 0x24 },
};
pub const IID_ID3D12CommandQueue = GUID{
    .Data1 = 0x0ec870a6,
    .Data2 = 0x5d7e,
    .Data3 = 0x4c22,
    .Data4 = .{ 0x8c, 0xfc, 0x5b, 0xaa, 0xe0, 0x76, 0x16, 0xed },
};
pub const IID_ID3D12Debug = GUID{
    .Data1 = 0x344488b7,
    .Data2 = 0x6846,
    .Data3 = 0x474b,
    .Data4 = .{ 0xb9, 0x89, 0xf0, 0x27, 0x44, 0x82, 0x45, 0xe0 },
};
pub const IID_ID3D12DescriptorHeap = GUID{
    .Data1 = 0x8efb471d,
    .Data2 = 0x616c,
    .Data3 = 0x4f49,
    .Data4 = .{ 0x90, 0xf7, 0x12, 0x7b, 0xb7, 0x63, 0xfa, 0x51 },
};
pub const IID_ID3D12Device = GUID{
    .Data1 = 0x189819f1,
    .Data2 = 0x1db6,
    .Data3 = 0x4b57,
    .Data4 = .{ 0xbe, 0x54, 0x18, 0x21, 0x33, 0x9b, 0x85, 0xf7 },
};
pub const IID_ID3D12Fence = GUID{
    .Data1 = 0x0a753dcf,
    .Data2 = 0xc4d8,
    .Data3 = 0x4b91,
    .Data4 = .{ 0xad, 0xf6, 0xbe, 0x5a, 0x60, 0xd9, 0x5a, 0x76 },
};
pub const IID_ID3D12GraphicsCommandList = GUID{
    .Data1 = 0x5b160d0f,
    .Data2 = 0xac1b,
    .Data3 = 0x4185,
    .Data4 = .{ 0x8b, 0xa8, 0xb3, 0xae, 0x42, 0xa5, 0xa4, 0x55 },
};
pub const IID_ID3D12PipelineState = GUID{
    .Data1 = 0x765a30f3,
    .Data2 = 0xf624,
    .Data3 = 0x4c6f,
    .Data4 = .{ 0xa8, 0x28, 0xac, 0xe9, 0x48, 0x62, 0x24, 0x45 },
};
pub const IID_ID3D12Resource = GUID{
    .Data1 = 0x696442be,
    .Data2 = 0xa72e,
    .Data3 = 0x4059,
    .Data4 = .{ 0xbc, 0x79, 0x5b, 0x5c, 0x98, 0x04, 0x0f, 0xad },
};
pub const IID_ID3D12RootSignature = GUID{
    .Data1 = 0xc54a6b66,
    .Data2 = 0x72df,
    .Data3 = 0x4ee8,
    .Data4 = .{ 0x8b, 0xe5, 0xa9, 0x46, 0xa1, 0x42, 0x92, 0x14 },
};
pub const IID_ID3D12DeviceRemovedExtendedDataSettings = GUID{
    .Data1 = 0x82bc481c,
    .Data2 = 0x6b9b,
    .Data3 = 0x4030,
    .Data4 = .{ 0xae, 0xdb, 0x7e, 0xe3, 0xd1, 0xdf, 0x1e, 0x63 },
};
pub const IID_ID3D12DeviceRemovedExtendedData = GUID{
    .Data1 = 0x98931d33,
    .Data2 = 0x5ae8,
    .Data3 = 0x4791,
    .Data4 = .{ 0xaa, 0x3c, 0x1a, 0x73, 0xa2, 0x93, 0x4e, 0x71 },
};

// --- Structures ---

pub const D3D12_CPU_DESCRIPTOR_HANDLE = extern struct {
    ptr: usize = 0,

    pub inline fn offset(self: D3D12_CPU_DESCRIPTOR_HANDLE, count: usize, stride: u32) D3D12_CPU_DESCRIPTOR_HANDLE {
        return .{ .ptr = self.ptr + count * @as(usize, stride) };
    }
};

pub const D3D12_GPU_DESCRIPTOR_HANDLE = extern struct {
    ptr: u64 = 0,

    pub inline fn offset(self: D3D12_GPU_DESCRIPTOR_HANDLE, count: usize, stride: u32) D3D12_GPU_DESCRIPTOR_HANDLE {
        return .{ .ptr = self.ptr + @as(u64, @intCast(count)) * @as(u64, stride) };
    }
};

pub const D3D12_BOX = extern struct {
    left: UINT = 0,
    top: UINT = 0,
    front: UINT = 0,
    right: UINT = 0,
    bottom: UINT = 0,
    back: UINT = 1,
};

pub const D3D12_RANGE = extern struct {
    Begin: usize = 0,
    End: usize = 0,
};

pub const D3D12_VIEWPORT = extern struct {
    TopLeftX: f32 = 0.0,
    TopLeftY: f32 = 0.0,
    Width: f32 = 0.0,
    Height: f32 = 0.0,
    MinDepth: f32 = D3D12_MIN_DEPTH,
    MaxDepth: f32 = D3D12_MAX_DEPTH,
};

pub const D3D12_COMMAND_QUEUE_DESC = extern struct {
    Type: D3D12_COMMAND_LIST_TYPE = D3D12_COMMAND_LIST_TYPE_DIRECT,
    Priority: i32 = 0,
    Flags: D3D12_COMMAND_QUEUE_FLAGS = D3D12_COMMAND_QUEUE_FLAG_NONE,
    NodeMask: UINT = 0,
};

pub const D3D12_DESCRIPTOR_HEAP_DESC = extern struct {
    Type: D3D12_DESCRIPTOR_HEAP_TYPE,
    NumDescriptors: UINT,
    Flags: D3D12_DESCRIPTOR_HEAP_FLAGS = D3D12_DESCRIPTOR_HEAP_FLAG_NONE,
    NodeMask: UINT = 0,
};

pub const D3D12_HEAP_PROPERTIES = extern struct {
    Type: D3D12_HEAP_TYPE,
    CPUPageProperty: D3D12_CPU_PAGE_PROPERTY = D3D12_CPU_PAGE_PROPERTY_UNKNOWN,
    MemoryPoolPreference: D3D12_MEMORY_POOL = D3D12_MEMORY_POOL_UNKNOWN,
    CreationNodeMask: UINT = 1,
    VisibleNodeMask: UINT = 1,
};

pub const D3D12_RESOURCE_DESC = extern struct {
    Dimension: D3D12_RESOURCE_DIMENSION,
    Alignment: UINT64 = 0,
    Width: UINT64,
    Height: UINT = 1,
    DepthOrArraySize: u16 = 1,
    MipLevels: u16 = 1,
    Format: DXGI_FORMAT = dxgi.DXGI_FORMAT_UNKNOWN,
    SampleDesc: DXGI_SAMPLE_DESC = .{ .Count = 1, .Quality = 0 },
    Layout: D3D12_TEXTURE_LAYOUT = D3D12_TEXTURE_LAYOUT_UNKNOWN,
    Flags: D3D12_RESOURCE_FLAGS = D3D12_RESOURCE_FLAG_NONE,
};

pub const D3D12_RESOURCE_ALLOCATION_INFO = extern struct {
    SizeInBytes: UINT64 = 0,
    Alignment: UINT64 = 0,
};

pub const D3D12_CLEAR_VALUE = extern struct {
    Format: DXGI_FORMAT = dxgi.DXGI_FORMAT_UNKNOWN,
    Color: [4]f32 = .{ 0, 0, 0, 0 },
};

pub const D3D12_CONSTANT_BUFFER_VIEW_DESC = extern struct {
    BufferLocation: D3D12_GPU_VIRTUAL_ADDRESS,
    SizeInBytes: UINT,
};

pub const D3D12_BUFFER_SRV = extern struct {
    FirstElement: UINT64 = 0,
    NumElements: UINT = 0,
    StructureByteStride: UINT = 0,
    Flags: i32 = 0,
};

pub const D3D12_TEX2D_SRV = extern struct {
    MostDetailedMip: UINT = 0,
    MipLevels: UINT = 1,
    PlaneSlice: UINT = 0,
    ResourceMinLODClamp: f32 = 0.0,
};

pub const D3D12_SHADER_RESOURCE_VIEW_DESC = extern struct {
    Format: DXGI_FORMAT,
    ViewDimension: D3D12_SRV_DIMENSION,
    Shader4ComponentMapping: UINT,
    u: extern union {
        Buffer: D3D12_BUFFER_SRV,
        Texture2D: D3D12_TEX2D_SRV,
        _pad: [24]u8,
    },
};

pub const D3D12_RENDER_TARGET_VIEW_DESC = extern struct {
    Format: DXGI_FORMAT,
    ViewDimension: i32,
    _union_pad: [3]u32 = .{ 0, 0, 0 },
};

pub const D3D12_DESCRIPTOR_RANGE = extern struct {
    RangeType: D3D12_DESCRIPTOR_RANGE_TYPE,
    NumDescriptors: UINT,
    BaseShaderRegister: UINT,
    RegisterSpace: UINT,
    OffsetInDescriptorsFromTableStart: UINT,
};

pub const D3D12_ROOT_DESCRIPTOR_TABLE = extern struct {
    NumDescriptorRanges: UINT,
    pDescriptorRanges: ?[*]const D3D12_DESCRIPTOR_RANGE,
};

pub const D3D12_ROOT_CONSTANTS = extern struct {
    ShaderRegister: UINT,
    RegisterSpace: UINT,
    Num32BitValues: UINT,
};

pub const D3D12_ROOT_DESCRIPTOR = extern struct {
    ShaderRegister: UINT,
    RegisterSpace: UINT,
};

pub const D3D12_ROOT_PARAMETER = extern struct {
    ParameterType: D3D12_ROOT_PARAMETER_TYPE,
    u: extern union {
        DescriptorTable: D3D12_ROOT_DESCRIPTOR_TABLE,
        Constants: D3D12_ROOT_CONSTANTS,
        Descriptor: D3D12_ROOT_DESCRIPTOR,
    },
    ShaderVisibility: D3D12_SHADER_VISIBILITY,
};

pub const D3D12_STATIC_SAMPLER_DESC = extern struct {
    Filter: i32 = 0,
    AddressU: i32 = 0,
    AddressV: i32 = 0,
    AddressW: i32 = 0,
    MipLODBias: f32 = 0,
    MaxAnisotropy: UINT = 0,
    ComparisonFunc: D3D12_COMPARISON_FUNC = D3D12_COMPARISON_FUNC_NEVER,
    BorderColor: i32 = 0,
    MinLOD: f32 = 0,
    MaxLOD: f32 = 0,
    ShaderRegister: UINT = 0,
    RegisterSpace: UINT = 0,
    ShaderVisibility: D3D12_SHADER_VISIBILITY = D3D12_SHADER_VISIBILITY_ALL,
};

pub const D3D12_ROOT_SIGNATURE_DESC = extern struct {
    NumParameters: UINT,
    pParameters: ?[*]const D3D12_ROOT_PARAMETER,
    NumStaticSamplers: UINT = 0,
    pStaticSamplers: ?[*]const D3D12_STATIC_SAMPLER_DESC = null,
    Flags: D3D12_ROOT_SIGNATURE_FLAGS = 0,
};

pub const D3D12_SHADER_BYTECODE = extern struct {
    pShaderBytecode: ?*const anyopaque = null,
    BytecodeLength: usize = 0,
};

pub const D3D12_SO_DECLARATION_ENTRY = extern struct {
    Stream: UINT = 0,
    SemanticName: ?[*:0]const u8 = null,
    SemanticIndex: UINT = 0,
    StartComponent: u8 = 0,
    ComponentCount: u8 = 0,
    OutputSlot: u8 = 0,
};

pub const D3D12_STREAM_OUTPUT_DESC = extern struct {
    pSODeclaration: ?[*]const D3D12_SO_DECLARATION_ENTRY = null,
    NumEntries: UINT = 0,
    pBufferStrides: ?[*]const UINT = null,
    NumStrides: UINT = 0,
    RasterizedStream: UINT = 0,
};

pub const D3D12_RENDER_TARGET_BLEND_DESC = extern struct {
    BlendEnable: BOOL = 0,
    LogicOpEnable: BOOL = 0,
    SrcBlend: D3D12_BLEND = D3D12_BLEND_ONE,
    DestBlend: D3D12_BLEND = D3D12_BLEND_ZERO,
    BlendOp: D3D12_BLEND_OP = D3D12_BLEND_OP_ADD,
    SrcBlendAlpha: D3D12_BLEND = D3D12_BLEND_ONE,
    DestBlendAlpha: D3D12_BLEND = D3D12_BLEND_ZERO,
    BlendOpAlpha: D3D12_BLEND_OP = D3D12_BLEND_OP_ADD,
    LogicOp: D3D12_LOGIC_OP = D3D12_LOGIC_OP_NOOP,
    RenderTargetWriteMask: u8 = D3D12_COLOR_WRITE_ENABLE_ALL,
};

pub const D3D12_BLEND_DESC = extern struct {
    AlphaToCoverageEnable: BOOL = 0,
    IndependentBlendEnable: BOOL = 0,
    RenderTarget: [8]D3D12_RENDER_TARGET_BLEND_DESC = @splat(.{}),
};

pub const D3D12_RASTERIZER_DESC = extern struct {
    FillMode: D3D12_FILL_MODE = D3D12_FILL_MODE_SOLID,
    CullMode: D3D12_CULL_MODE = D3D12_CULL_MODE_NONE,
    FrontCounterClockwise: BOOL = 0,
    DepthBias: i32 = D3D12_DEFAULT_DEPTH_BIAS,
    DepthBiasClamp: f32 = D3D12_DEFAULT_DEPTH_BIAS_CLAMP,
    SlopeScaledDepthBias: f32 = D3D12_DEFAULT_SLOPE_SCALED_DEPTH_BIAS,
    DepthClipEnable: BOOL = 0,
    MultisampleEnable: BOOL = 0,
    AntialiasedLineEnable: BOOL = 0,
    ForcedSampleCount: UINT = 0,
    ConservativeRaster: D3D12_CONSERVATIVE_RASTERIZATION_MODE = D3D12_CONSERVATIVE_RASTERIZATION_MODE_OFF,
};

pub const D3D12_DEPTH_STENCILOP_DESC = extern struct {
    StencilFailOp: D3D12_STENCIL_OP = D3D12_STENCIL_OP_KEEP,
    StencilDepthFailOp: D3D12_STENCIL_OP = D3D12_STENCIL_OP_KEEP,
    StencilPassOp: D3D12_STENCIL_OP = D3D12_STENCIL_OP_KEEP,
    StencilFunc: D3D12_COMPARISON_FUNC = D3D12_COMPARISON_FUNC_ALWAYS,
};

pub const D3D12_DEPTH_STENCIL_DESC = extern struct {
    DepthEnable: BOOL = 0,
    DepthWriteMask: D3D12_DEPTH_WRITE_MASK = D3D12_DEPTH_WRITE_MASK_ZERO,
    DepthFunc: D3D12_COMPARISON_FUNC = D3D12_COMPARISON_FUNC_ALWAYS,
    StencilEnable: BOOL = 0,
    StencilReadMask: u8 = 0xFF,
    StencilWriteMask: u8 = 0xFF,
    FrontFace: D3D12_DEPTH_STENCILOP_DESC = .{},
    BackFace: D3D12_DEPTH_STENCILOP_DESC = .{},
};

pub const D3D12_INPUT_ELEMENT_DESC = extern struct {
    SemanticName: [*:0]const u8,
    SemanticIndex: UINT,
    Format: DXGI_FORMAT,
    InputSlot: UINT = 0,
    AlignedByteOffset: UINT = D3D12_APPEND_ALIGNED_ELEMENT,
    InputSlotClass: D3D12_INPUT_CLASSIFICATION = D3D12_INPUT_CLASSIFICATION_PER_VERTEX_DATA,
    InstanceDataStepRate: UINT = 0,
};

pub const D3D12_INPUT_LAYOUT_DESC = extern struct {
    pInputElementDescs: ?[*]const D3D12_INPUT_ELEMENT_DESC = null,
    NumElements: UINT = 0,
};

pub const D3D12_CACHED_PIPELINE_STATE = extern struct {
    pCachedBlob: ?*const anyopaque = null,
    CachedBlobSizeInBytes: usize = 0,
};

pub const D3D12_GRAPHICS_PIPELINE_STATE_DESC = extern struct {
    pRootSignature: ?*ID3D12RootSignature = null,
    VS: D3D12_SHADER_BYTECODE = .{},
    PS: D3D12_SHADER_BYTECODE = .{},
    DS: D3D12_SHADER_BYTECODE = .{},
    HS: D3D12_SHADER_BYTECODE = .{},
    GS: D3D12_SHADER_BYTECODE = .{},
    StreamOutput: D3D12_STREAM_OUTPUT_DESC = .{},
    BlendState: D3D12_BLEND_DESC = .{},
    SampleMask: UINT = 0xFFFF_FFFF,
    RasterizerState: D3D12_RASTERIZER_DESC = .{},
    DepthStencilState: D3D12_DEPTH_STENCIL_DESC = .{},
    InputLayout: D3D12_INPUT_LAYOUT_DESC = .{},
    IBStripCutValue: D3D12_INDEX_BUFFER_STRIP_CUT_VALUE = D3D12_INDEX_BUFFER_STRIP_CUT_VALUE_DISABLED,
    PrimitiveTopologyType: D3D12_PRIMITIVE_TOPOLOGY_TYPE = D3D12_PRIMITIVE_TOPOLOGY_TYPE_TRIANGLE,
    NumRenderTargets: UINT = 1,
    RTVFormats: [8]DXGI_FORMAT = @splat(dxgi.DXGI_FORMAT_UNKNOWN),
    DSVFormat: DXGI_FORMAT = dxgi.DXGI_FORMAT_UNKNOWN,
    SampleDesc: DXGI_SAMPLE_DESC = .{ .Count = 1, .Quality = 0 },
    NodeMask: UINT = 0,
    CachedPSO: D3D12_CACHED_PIPELINE_STATE = .{},
    Flags: D3D12_PIPELINE_STATE_FLAGS = D3D12_PIPELINE_STATE_FLAG_NONE,
};

pub const D3D12_VERTEX_BUFFER_VIEW = extern struct {
    BufferLocation: D3D12_GPU_VIRTUAL_ADDRESS = 0,
    SizeInBytes: UINT = 0,
    StrideInBytes: UINT = 0,

    pub fn eql(a: D3D12_VERTEX_BUFFER_VIEW, b: D3D12_VERTEX_BUFFER_VIEW) bool {
        return a.BufferLocation == b.BufferLocation and a.SizeInBytes == b.SizeInBytes and a.StrideInBytes == b.StrideInBytes;
    }
};

pub const D3D12_INDEX_BUFFER_VIEW = extern struct {
    BufferLocation: D3D12_GPU_VIRTUAL_ADDRESS = 0,
    SizeInBytes: UINT = 0,
    Format: DXGI_FORMAT = dxgi.DXGI_FORMAT_UNKNOWN,

    pub fn eql(a: D3D12_INDEX_BUFFER_VIEW, b: D3D12_INDEX_BUFFER_VIEW) bool {
        return a.BufferLocation == b.BufferLocation and a.SizeInBytes == b.SizeInBytes and a.Format == b.Format;
    }
};

pub const D3D12_RESOURCE_TRANSITION_BARRIER = extern struct {
    pResource: *ID3D12Resource,
    Subresource: UINT = D3D12_RESOURCE_BARRIER_ALL_SUBRESOURCES,
    StateBefore: D3D12_RESOURCE_STATES,
    StateAfter: D3D12_RESOURCE_STATES,
};

pub const D3D12_RESOURCE_ALIASING_BARRIER = extern struct {
    pResourceBefore: ?*ID3D12Resource,
    pResourceAfter: ?*ID3D12Resource,
};

pub const D3D12_RESOURCE_UAV_BARRIER = extern struct {
    pResource: ?*ID3D12Resource,
};

pub const D3D12_RESOURCE_BARRIER = extern struct {
    Type: D3D12_RESOURCE_BARRIER_TYPE = D3D12_RESOURCE_BARRIER_TYPE_TRANSITION,
    Flags: D3D12_RESOURCE_BARRIER_FLAGS = D3D12_RESOURCE_BARRIER_FLAG_NONE,
    u: extern union {
        Transition: D3D12_RESOURCE_TRANSITION_BARRIER,
        Aliasing: D3D12_RESOURCE_ALIASING_BARRIER,
        UAV: D3D12_RESOURCE_UAV_BARRIER,
    },

    pub fn transition(
        resource: *ID3D12Resource,
        before: D3D12_RESOURCE_STATES,
        after: D3D12_RESOURCE_STATES,
    ) D3D12_RESOURCE_BARRIER {
        return .{
            .Type = D3D12_RESOURCE_BARRIER_TYPE_TRANSITION,
            .Flags = D3D12_RESOURCE_BARRIER_FLAG_NONE,
            .u = .{
                .Transition = .{
                    .pResource = resource,
                    .Subresource = D3D12_RESOURCE_BARRIER_ALL_SUBRESOURCES,
                    .StateBefore = before,
                    .StateAfter = after,
                },
            },
        };
    }
};

pub const D3D12_SUBRESOURCE_FOOTPRINT = extern struct {
    Format: DXGI_FORMAT = dxgi.DXGI_FORMAT_UNKNOWN,
    Width: UINT = 0,
    Height: UINT = 0,
    Depth: UINT = 1,
    RowPitch: UINT = 0,
};

pub const D3D12_PLACED_SUBRESOURCE_FOOTPRINT = extern struct {
    Offset: UINT64 = 0,
    Footprint: D3D12_SUBRESOURCE_FOOTPRINT = .{},
};

pub const D3D12_TEXTURE_COPY_LOCATION = extern struct {
    pResource: *ID3D12Resource,
    Type: D3D12_TEXTURE_COPY_TYPE,
    u: extern union {
        PlacedFootprint: D3D12_PLACED_SUBRESOURCE_FOOTPRINT,
        SubresourceIndex: UINT,
    },

    pub fn subresource(res: *ID3D12Resource, index: UINT) D3D12_TEXTURE_COPY_LOCATION {
        return .{
            .pResource = res,
            .Type = D3D12_TEXTURE_COPY_TYPE_SUBRESOURCE_INDEX,
            .u = .{ .SubresourceIndex = index },
        };
    }

    pub fn placedFootprint(res: *ID3D12Resource, fp: D3D12_PLACED_SUBRESOURCE_FOOTPRINT) D3D12_TEXTURE_COPY_LOCATION {
        return .{
            .pResource = res,
            .Type = D3D12_TEXTURE_COPY_TYPE_PLACED_FOOTPRINT,
            .u = .{ .PlacedFootprint = fp },
        };
    }
};

// DRED structures
pub const D3D12_DRED_ALLOCATION_NODE = extern struct {
    ObjectNameA: ?[*:0]const u8,
    ObjectNameW: ?[*:0]const u16,
    AllocationType: i32,
    pNext: ?*const D3D12_DRED_ALLOCATION_NODE,
};

pub const D3D12_DRED_PAGE_FAULT_OUTPUT = extern struct {
    PageFaultVA: UINT64 = 0,
    pHeadExistingAllocationNode: ?*const D3D12_DRED_ALLOCATION_NODE = null,
    pHeadRecentFreedAllocationNode: ?*const D3D12_DRED_ALLOCATION_NODE = null,
};

pub const D3D12_AUTO_BREADCRUMB_NODE = extern struct {
    pCommandListDebugNameA: ?[*:0]const u8,
    pCommandListDebugNameW: ?[*:0]const u16,
    pCommandQueueDebugNameA: ?[*:0]const u8,
    pCommandQueueDebugNameW: ?[*:0]const u16,
    pCommandList: ?*anyopaque,
    pCommandQueue: ?*anyopaque,
    BreadcrumbCount: UINT,
    pLastBreadcrumbValue: ?*const UINT,
    pCommandHistory: ?[*]const i32,
    pNext: ?*const D3D12_AUTO_BREADCRUMB_NODE,
};

pub const D3D12_DRED_AUTO_BREADCRUMBS_OUTPUT = extern struct {
    pHeadAutoBreadcrumbNode: ?*const D3D12_AUTO_BREADCRUMB_NODE = null,
};

// --- COM Interfaces (exact d3d12.h vtable slot order) ---

pub const ID3D12Debug = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12Debug, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12Debug) callconv(.winapi) u32,
        Release: *const fn (*ID3D12Debug) callconv(.winapi) u32,
        // [3] ID3D12Debug
        EnableDebugLayer: *const fn (*ID3D12Debug) callconv(.winapi) void,
    };

    pub inline fn enableDebugLayer(self: *ID3D12Debug) void {
        self.vtbl.EnableDebugLayer(self);
    }

    pub inline fn release(self: *ID3D12Debug) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12DeviceRemovedExtendedDataSettings = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12DeviceRemovedExtendedDataSettings, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12DeviceRemovedExtendedDataSettings) callconv(.winapi) u32,
        Release: *const fn (*ID3D12DeviceRemovedExtendedDataSettings) callconv(.winapi) u32,
        // [3..5] ID3D12DeviceRemovedExtendedDataSettings
        SetAutoBreadcrumbsEnablement: *const fn (*ID3D12DeviceRemovedExtendedDataSettings, D3D12_DRED_ENABLEMENT) callconv(.winapi) void,
        SetPageFaultEnablement: *const fn (*ID3D12DeviceRemovedExtendedDataSettings, D3D12_DRED_ENABLEMENT) callconv(.winapi) void,
        SetWatsonDumpEnablement: *const fn (*ID3D12DeviceRemovedExtendedDataSettings, D3D12_DRED_ENABLEMENT) callconv(.winapi) void,
    };

    pub inline fn setAutoBreadcrumbsEnablement(self: *ID3D12DeviceRemovedExtendedDataSettings, e: D3D12_DRED_ENABLEMENT) void {
        self.vtbl.SetAutoBreadcrumbsEnablement(self, e);
    }

    pub inline fn setPageFaultEnablement(self: *ID3D12DeviceRemovedExtendedDataSettings, e: D3D12_DRED_ENABLEMENT) void {
        self.vtbl.SetPageFaultEnablement(self, e);
    }

    pub inline fn release(self: *ID3D12DeviceRemovedExtendedDataSettings) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12DeviceRemovedExtendedData = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12DeviceRemovedExtendedData, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12DeviceRemovedExtendedData) callconv(.winapi) u32,
        Release: *const fn (*ID3D12DeviceRemovedExtendedData) callconv(.winapi) u32,
        // [3..4] ID3D12DeviceRemovedExtendedData
        GetAutoBreadcrumbsOutput: *const fn (*ID3D12DeviceRemovedExtendedData, *D3D12_DRED_AUTO_BREADCRUMBS_OUTPUT) callconv(.winapi) HRESULT,
        GetPageFaultAllocationOutput: *const fn (*ID3D12DeviceRemovedExtendedData, *D3D12_DRED_PAGE_FAULT_OUTPUT) callconv(.winapi) HRESULT,
    };

    pub inline fn getAutoBreadcrumbsOutput(self: *ID3D12DeviceRemovedExtendedData, out: *D3D12_DRED_AUTO_BREADCRUMBS_OUTPUT) HRESULT {
        return self.vtbl.GetAutoBreadcrumbsOutput(self, out);
    }

    pub inline fn getPageFaultAllocationOutput(self: *ID3D12DeviceRemovedExtendedData, out: *D3D12_DRED_PAGE_FAULT_OUTPUT) HRESULT {
        return self.vtbl.GetPageFaultAllocationOutput(self, out);
    }

    pub inline fn release(self: *ID3D12DeviceRemovedExtendedData) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12CommandAllocator = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12CommandAllocator, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12CommandAllocator) callconv(.winapi) u32,
        Release: *const fn (*ID3D12CommandAllocator) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7] ID3D12DeviceChild
        GetDevice: PlaceholderFn,
        // [8] ID3D12CommandAllocator
        Reset: *const fn (*ID3D12CommandAllocator) callconv(.winapi) HRESULT,
    };

    pub inline fn reset(self: *ID3D12CommandAllocator) HRESULT {
        return self.vtbl.Reset(self);
    }

    pub inline fn release(self: *ID3D12CommandAllocator) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12Fence = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12Fence, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12Fence) callconv(.winapi) u32,
        Release: *const fn (*ID3D12Fence) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7] ID3D12DeviceChild
        GetDevice: PlaceholderFn,
        // [8..10] ID3D12Fence
        GetCompletedValue: *const fn (*ID3D12Fence) callconv(.winapi) UINT64,
        SetEventOnCompletion: *const fn (*ID3D12Fence, UINT64, HANDLE) callconv(.winapi) HRESULT,
        Signal: *const fn (*ID3D12Fence, UINT64) callconv(.winapi) HRESULT,
    };

    pub inline fn getCompletedValue(self: *ID3D12Fence) UINT64 {
        return self.vtbl.GetCompletedValue(self);
    }

    pub inline fn setEventOnCompletion(self: *ID3D12Fence, value: UINT64, event: HANDLE) HRESULT {
        return self.vtbl.SetEventOnCompletion(self, value, event);
    }

    pub inline fn release(self: *ID3D12Fence) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12DescriptorHeap = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12DescriptorHeap, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12DescriptorHeap) callconv(.winapi) u32,
        Release: *const fn (*ID3D12DescriptorHeap) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7] ID3D12DeviceChild
        GetDevice: PlaceholderFn,
        // [8..10] ID3D12DescriptorHeap — MSVC COM hidden out-pointer ABI!
        GetDesc: *const fn (*ID3D12DescriptorHeap, *D3D12_DESCRIPTOR_HEAP_DESC) callconv(.winapi) *D3D12_DESCRIPTOR_HEAP_DESC,
        GetCPUDescriptorHandleForHeapStart: *const fn (*ID3D12DescriptorHeap, *D3D12_CPU_DESCRIPTOR_HANDLE) callconv(.winapi) *D3D12_CPU_DESCRIPTOR_HANDLE,
        GetGPUDescriptorHandleForHeapStart: *const fn (*ID3D12DescriptorHeap, *D3D12_GPU_DESCRIPTOR_HANDLE) callconv(.winapi) *D3D12_GPU_DESCRIPTOR_HANDLE,
    };

    pub inline fn getDesc(self: *ID3D12DescriptorHeap) D3D12_DESCRIPTOR_HEAP_DESC {
        var out: D3D12_DESCRIPTOR_HEAP_DESC = undefined;
        _ = self.vtbl.GetDesc(self, &out);
        return out;
    }

    pub inline fn getCpuHandleStart(self: *ID3D12DescriptorHeap) D3D12_CPU_DESCRIPTOR_HANDLE {
        var out: D3D12_CPU_DESCRIPTOR_HANDLE = .{ .ptr = 0 };
        _ = self.vtbl.GetCPUDescriptorHandleForHeapStart(self, &out);
        return out;
    }

    pub inline fn getGpuHandleStart(self: *ID3D12DescriptorHeap) D3D12_GPU_DESCRIPTOR_HANDLE {
        var out: D3D12_GPU_DESCRIPTOR_HANDLE = .{ .ptr = 0 };
        _ = self.vtbl.GetGPUDescriptorHandleForHeapStart(self, &out);
        return out;
    }

    pub inline fn release(self: *ID3D12DescriptorHeap) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12RootSignature = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12RootSignature, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12RootSignature) callconv(.winapi) u32,
        Release: *const fn (*ID3D12RootSignature) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7] ID3D12DeviceChild
        GetDevice: PlaceholderFn,
    };

    pub inline fn release(self: *ID3D12RootSignature) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12PipelineState = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12PipelineState, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12PipelineState) callconv(.winapi) u32,
        Release: *const fn (*ID3D12PipelineState) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7] ID3D12DeviceChild
        GetDevice: PlaceholderFn,
        // [8] ID3D12PipelineState
        GetCachedBlob: PlaceholderFn,
    };

    pub inline fn release(self: *ID3D12PipelineState) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12Resource = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12Resource, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12Resource) callconv(.winapi) u32,
        Release: *const fn (*ID3D12Resource) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7] ID3D12DeviceChild
        GetDevice: PlaceholderFn,
        // [8..14] ID3D12Resource
        Map: *const fn (*ID3D12Resource, UINT, ?*const D3D12_RANGE, ?*?*anyopaque) callconv(.winapi) HRESULT,
        Unmap: *const fn (*ID3D12Resource, UINT, ?*const D3D12_RANGE) callconv(.winapi) void,
        // [10] MSVC COM hidden out-pointer ABI!
        GetDesc: *const fn (*ID3D12Resource, *D3D12_RESOURCE_DESC) callconv(.winapi) *D3D12_RESOURCE_DESC,
        // [11]
        GetGPUVirtualAddress: *const fn (*ID3D12Resource) callconv(.winapi) D3D12_GPU_VIRTUAL_ADDRESS,
        WriteToSubresource: PlaceholderFn,
        ReadFromSubresource: PlaceholderFn,
        GetHeapProperties: PlaceholderFn,
    };

    pub inline fn map(self: *ID3D12Resource, subresource: UINT, read_range: ?*const D3D12_RANGE, out_data: *?*anyopaque) HRESULT {
        return self.vtbl.Map(self, subresource, read_range, out_data);
    }

    pub inline fn unmap(self: *ID3D12Resource, subresource: UINT, written_range: ?*const D3D12_RANGE) void {
        self.vtbl.Unmap(self, subresource, written_range);
    }

    pub inline fn getDesc(self: *ID3D12Resource) D3D12_RESOURCE_DESC {
        var out: D3D12_RESOURCE_DESC = undefined;
        _ = self.vtbl.GetDesc(self, &out);
        return out;
    }

    pub inline fn getGpuVirtualAddress(self: *ID3D12Resource) D3D12_GPU_VIRTUAL_ADDRESS {
        return self.vtbl.GetGPUVirtualAddress(self);
    }

    pub inline fn release(self: *ID3D12Resource) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12CommandQueue = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12CommandQueue, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12CommandQueue) callconv(.winapi) u32,
        Release: *const fn (*ID3D12CommandQueue) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7] ID3D12DeviceChild
        GetDevice: PlaceholderFn,
        // [8..18] ID3D12CommandQueue
        UpdateTileMappings: PlaceholderFn,
        CopyTileMappings: PlaceholderFn,
        ExecuteCommandLists: *const fn (*ID3D12CommandQueue, UINT, [*]const *ID3D12GraphicsCommandList) callconv(.winapi) void,
        SetMarker: PlaceholderFn,
        BeginEvent: PlaceholderFn,
        EndEvent: PlaceholderFn,
        Signal: *const fn (*ID3D12CommandQueue, *ID3D12Fence, UINT64) callconv(.winapi) HRESULT,
        Wait: *const fn (*ID3D12CommandQueue, *ID3D12Fence, UINT64) callconv(.winapi) HRESULT,
        GetTimestampFrequency: PlaceholderFn,
        GetClockCalibration: PlaceholderFn,
        // [18] MSVC COM hidden out-pointer ABI!
        GetDesc: *const fn (*ID3D12CommandQueue, *D3D12_COMMAND_QUEUE_DESC) callconv(.winapi) *D3D12_COMMAND_QUEUE_DESC,
    };

    pub inline fn executeCommandLists(self: *ID3D12CommandQueue, lists: []const *ID3D12GraphicsCommandList) void {
        if (lists.len == 0) return;
        self.vtbl.ExecuteCommandLists(self, @intCast(lists.len), lists.ptr);
    }

    pub inline fn signal(self: *ID3D12CommandQueue, fence: *ID3D12Fence, value: UINT64) HRESULT {
        return self.vtbl.Signal(self, fence, value);
    }

    pub inline fn getDesc(self: *ID3D12CommandQueue) D3D12_COMMAND_QUEUE_DESC {
        var out: D3D12_COMMAND_QUEUE_DESC = undefined;
        _ = self.vtbl.GetDesc(self, &out);
        return out;
    }

    pub inline fn release(self: *ID3D12CommandQueue) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12GraphicsCommandList = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12GraphicsCommandList, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12GraphicsCommandList) callconv(.winapi) u32,
        Release: *const fn (*ID3D12GraphicsCommandList) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7] ID3D12DeviceChild
        GetDevice: PlaceholderFn,
        // [8] ID3D12CommandList
        GetType: *const fn (*ID3D12GraphicsCommandList) callconv(.winapi) D3D12_COMMAND_LIST_TYPE,
        // [9..59] ID3D12GraphicsCommandList
        Close: *const fn (*ID3D12GraphicsCommandList) callconv(.winapi) HRESULT,
        Reset: *const fn (*ID3D12GraphicsCommandList, *ID3D12CommandAllocator, ?*ID3D12PipelineState) callconv(.winapi) HRESULT,
        ClearState: PlaceholderFn,
        DrawInstanced: PlaceholderFn,
        DrawIndexedInstanced: *const fn (*ID3D12GraphicsCommandList, UINT, UINT, UINT, i32, UINT) callconv(.winapi) void,
        Dispatch: PlaceholderFn,
        CopyBufferRegion: *const fn (*ID3D12GraphicsCommandList, *ID3D12Resource, UINT64, *ID3D12Resource, UINT64, UINT64) callconv(.winapi) void,
        CopyTextureRegion: *const fn (*ID3D12GraphicsCommandList, *const D3D12_TEXTURE_COPY_LOCATION, UINT, UINT, UINT, *const D3D12_TEXTURE_COPY_LOCATION, ?*const D3D12_BOX) callconv(.winapi) void,
        CopyResource: PlaceholderFn,
        CopyTiles: PlaceholderFn,
        ResolveSubresource: PlaceholderFn,
        IASetPrimitiveTopology: *const fn (*ID3D12GraphicsCommandList, D3D_PRIMITIVE_TOPOLOGY) callconv(.winapi) void,
        RSSetViewports: *const fn (*ID3D12GraphicsCommandList, UINT, [*]const D3D12_VIEWPORT) callconv(.winapi) void,
        RSSetScissorRects: *const fn (*ID3D12GraphicsCommandList, UINT, [*]const RECT) callconv(.winapi) void,
        OMSetBlendFactor: PlaceholderFn,
        OMSetStencilRef: PlaceholderFn,
        SetPipelineState: *const fn (*ID3D12GraphicsCommandList, *ID3D12PipelineState) callconv(.winapi) void,
        ResourceBarrier: *const fn (*ID3D12GraphicsCommandList, UINT, [*]const D3D12_RESOURCE_BARRIER) callconv(.winapi) void,
        ExecuteBundle: PlaceholderFn,
        SetDescriptorHeaps: *const fn (*ID3D12GraphicsCommandList, UINT, [*]const *ID3D12DescriptorHeap) callconv(.winapi) void,
        SetComputeRootSignature: PlaceholderFn,
        SetGraphicsRootSignature: *const fn (*ID3D12GraphicsCommandList, ?*ID3D12RootSignature) callconv(.winapi) void,
        SetComputeRootDescriptorTable: PlaceholderFn,
        SetGraphicsRootDescriptorTable: *const fn (*ID3D12GraphicsCommandList, UINT, D3D12_GPU_DESCRIPTOR_HANDLE) callconv(.winapi) void,
        SetComputeRoot32BitConstant: PlaceholderFn,
        SetGraphicsRoot32BitConstant: PlaceholderFn,
        SetComputeRoot32BitConstants: PlaceholderFn,
        SetGraphicsRoot32BitConstants: PlaceholderFn,
        SetComputeRootConstantBufferView: PlaceholderFn,
        SetGraphicsRootConstantBufferView: *const fn (*ID3D12GraphicsCommandList, UINT, D3D12_GPU_VIRTUAL_ADDRESS) callconv(.winapi) void,
        SetComputeRootShaderResourceView: PlaceholderFn,
        SetGraphicsRootShaderResourceView: PlaceholderFn,
        SetComputeRootUnorderedAccessView: PlaceholderFn,
        SetGraphicsRootUnorderedAccessView: PlaceholderFn,
        IASetIndexBuffer: *const fn (*ID3D12GraphicsCommandList, ?*const D3D12_INDEX_BUFFER_VIEW) callconv(.winapi) void,
        IASetVertexBuffers: *const fn (*ID3D12GraphicsCommandList, UINT, UINT, ?[*]const D3D12_VERTEX_BUFFER_VIEW) callconv(.winapi) void,
        SOSetTargets: PlaceholderFn,
        OMSetRenderTargets: *const fn (*ID3D12GraphicsCommandList, UINT, ?[*]const D3D12_CPU_DESCRIPTOR_HANDLE, BOOL, ?*const D3D12_CPU_DESCRIPTOR_HANDLE) callconv(.winapi) void,
        ClearDepthStencilView: PlaceholderFn,
        ClearRenderTargetView: *const fn (*ID3D12GraphicsCommandList, D3D12_CPU_DESCRIPTOR_HANDLE, *const [4]f32, UINT, ?[*]const RECT) callconv(.winapi) void,
        ClearUnorderedAccessViewUint: PlaceholderFn,
        ClearUnorderedAccessViewFloat: PlaceholderFn,
        DiscardResource: PlaceholderFn,
        BeginQuery: PlaceholderFn,
        EndQuery: PlaceholderFn,
        ResolveQueryData: PlaceholderFn,
        SetPredication: PlaceholderFn,
        SetMarker: PlaceholderFn,
        BeginEvent: PlaceholderFn,
        EndEvent: PlaceholderFn,
        ExecuteIndirect: PlaceholderFn,
    };

    pub inline fn close(self: *ID3D12GraphicsCommandList) HRESULT {
        return self.vtbl.Close(self);
    }

    pub inline fn reset(self: *ID3D12GraphicsCommandList, allocator: *ID3D12CommandAllocator, pso: ?*ID3D12PipelineState) HRESULT {
        return self.vtbl.Reset(self, allocator, pso);
    }

    pub inline fn drawIndexedInstanced(
        self: *ID3D12GraphicsCommandList,
        index_count: UINT,
        instance_count: UINT,
        start_index: UINT,
        base_vertex: i32,
        start_instance: UINT,
    ) void {
        self.vtbl.DrawIndexedInstanced(self, index_count, instance_count, start_index, base_vertex, start_instance);
    }

    pub inline fn copyTextureRegion(
        self: *ID3D12GraphicsCommandList,
        dst: *const D3D12_TEXTURE_COPY_LOCATION,
        dst_x: UINT,
        dst_y: UINT,
        dst_z: UINT,
        src: *const D3D12_TEXTURE_COPY_LOCATION,
        src_box: ?*const D3D12_BOX,
    ) void {
        self.vtbl.CopyTextureRegion(self, dst, dst_x, dst_y, dst_z, src, src_box);
    }

    pub inline fn iaSetPrimitiveTopology(self: *ID3D12GraphicsCommandList, topo: D3D_PRIMITIVE_TOPOLOGY) void {
        self.vtbl.IASetPrimitiveTopology(self, topo);
    }

    pub inline fn rsSetViewports(self: *ID3D12GraphicsCommandList, vps: []const D3D12_VIEWPORT) void {
        self.vtbl.RSSetViewports(self, @intCast(vps.len), vps.ptr);
    }

    pub inline fn rsSetScissorRects(self: *ID3D12GraphicsCommandList, rects: []const RECT) void {
        self.vtbl.RSSetScissorRects(self, @intCast(rects.len), rects.ptr);
    }

    pub inline fn setPipelineState(self: *ID3D12GraphicsCommandList, pso: *ID3D12PipelineState) void {
        self.vtbl.SetPipelineState(self, pso);
    }

    pub inline fn resourceBarrier(self: *ID3D12GraphicsCommandList, barriers: []const D3D12_RESOURCE_BARRIER) void {
        if (barriers.len == 0) return;
        self.vtbl.ResourceBarrier(self, @intCast(barriers.len), barriers.ptr);
    }

    pub inline fn setDescriptorHeaps(self: *ID3D12GraphicsCommandList, heaps: []const *ID3D12DescriptorHeap) void {
        self.vtbl.SetDescriptorHeaps(self, @intCast(heaps.len), heaps.ptr);
    }

    pub inline fn setGraphicsRootSignature(self: *ID3D12GraphicsCommandList, rs: *ID3D12RootSignature) void {
        self.vtbl.SetGraphicsRootSignature(self, rs);
    }

    pub inline fn setGraphicsRootDescriptorTable(
        self: *ID3D12GraphicsCommandList,
        param_idx: UINT,
        base_gpu_handle: D3D12_GPU_DESCRIPTOR_HANDLE,
    ) void {
        self.vtbl.SetGraphicsRootDescriptorTable(self, param_idx, base_gpu_handle);
    }

    pub inline fn setGraphicsRootConstantBufferView(
        self: *ID3D12GraphicsCommandList,
        param_idx: UINT,
        gpu_addr: D3D12_GPU_VIRTUAL_ADDRESS,
    ) void {
        self.vtbl.SetGraphicsRootConstantBufferView(self, param_idx, gpu_addr);
    }

    pub inline fn iaSetIndexBuffer(self: *ID3D12GraphicsCommandList, view: *const D3D12_INDEX_BUFFER_VIEW) void {
        self.vtbl.IASetIndexBuffer(self, view);
    }

    pub inline fn iaSetVertexBuffers(self: *ID3D12GraphicsCommandList, start_slot: UINT, views: []const D3D12_VERTEX_BUFFER_VIEW) void {
        self.vtbl.IASetVertexBuffers(self, start_slot, @intCast(views.len), views.ptr);
    }

    pub inline fn omSetRenderTargets(self: *ID3D12GraphicsCommandList, rtvs: []const D3D12_CPU_DESCRIPTOR_HANDLE) void {
        self.vtbl.OMSetRenderTargets(self, @intCast(rtvs.len), rtvs.ptr, 0, null);
    }

    pub inline fn release(self: *ID3D12GraphicsCommandList) u32 {
        return self.vtbl.Release(self);
    }
};

pub const ID3D12Device = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3D12Device, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3D12Device) callconv(.winapi) u32,
        Release: *const fn (*ID3D12Device) callconv(.winapi) u32,
        // [3..6] ID3D12Object
        GetPrivateData: PlaceholderFn,
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        SetName: PlaceholderFn,
        // [7..43] ID3D12Device
        GetNodeCount: *const fn (*ID3D12Device) callconv(.winapi) UINT,
        CreateCommandQueue: *const fn (*ID3D12Device, *const D3D12_COMMAND_QUEUE_DESC, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        CreateCommandAllocator: *const fn (*ID3D12Device, D3D12_COMMAND_LIST_TYPE, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        CreateGraphicsPipelineState: *const fn (*ID3D12Device, *const D3D12_GRAPHICS_PIPELINE_STATE_DESC, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        CreateComputePipelineState: PlaceholderFn,
        CreateCommandList: *const fn (*ID3D12Device, UINT, D3D12_COMMAND_LIST_TYPE, *ID3D12CommandAllocator, ?*ID3D12PipelineState, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        CheckFeatureSupport: PlaceholderFn,
        CreateDescriptorHeap: *const fn (*ID3D12Device, *const D3D12_DESCRIPTOR_HEAP_DESC, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        GetDescriptorHandleIncrementSize: *const fn (*ID3D12Device, D3D12_DESCRIPTOR_HEAP_TYPE) callconv(.winapi) UINT,
        CreateRootSignature: *const fn (*ID3D12Device, UINT, *const anyopaque, usize, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        CreateConstantBufferView: *const fn (*ID3D12Device, ?*const D3D12_CONSTANT_BUFFER_VIEW_DESC, D3D12_CPU_DESCRIPTOR_HANDLE) callconv(.winapi) void,
        CreateShaderResourceView: *const fn (*ID3D12Device, ?*ID3D12Resource, ?*const D3D12_SHADER_RESOURCE_VIEW_DESC, D3D12_CPU_DESCRIPTOR_HANDLE) callconv(.winapi) void,
        CreateUnorderedAccessView: PlaceholderFn,
        CreateRenderTargetView: *const fn (*ID3D12Device, ?*ID3D12Resource, ?*const D3D12_RENDER_TARGET_VIEW_DESC, D3D12_CPU_DESCRIPTOR_HANDLE) callconv(.winapi) void,
        CreateDepthStencilView: PlaceholderFn,
        CreateSampler: PlaceholderFn,
        CopyDescriptors: *const fn (
            *ID3D12Device,
            UINT,
            [*]const D3D12_CPU_DESCRIPTOR_HANDLE,
            ?[*]const UINT,
            UINT,
            [*]const D3D12_CPU_DESCRIPTOR_HANDLE,
            ?[*]const UINT,
            D3D12_DESCRIPTOR_HEAP_TYPE,
        ) callconv(.winapi) void,
        CopyDescriptorsSimple: *const fn (
            *ID3D12Device,
            UINT,
            D3D12_CPU_DESCRIPTOR_HANDLE,
            D3D12_CPU_DESCRIPTOR_HANDLE,
            D3D12_DESCRIPTOR_HEAP_TYPE,
        ) callconv(.winapi) void,
        // [25..26] MSVC COM hidden out-pointer ABI!
        GetResourceAllocationInfo: *const fn (*ID3D12Device, *D3D12_RESOURCE_ALLOCATION_INFO, UINT, UINT, [*]const D3D12_RESOURCE_DESC) callconv(.winapi) *D3D12_RESOURCE_ALLOCATION_INFO,
        GetCustomHeapProperties: *const fn (*ID3D12Device, *D3D12_HEAP_PROPERTIES, UINT, D3D12_HEAP_TYPE) callconv(.winapi) *D3D12_HEAP_PROPERTIES,
        // [27..42]
        CreateCommittedResource: *const fn (
            *ID3D12Device,
            *const D3D12_HEAP_PROPERTIES,
            D3D12_HEAP_FLAGS,
            *const D3D12_RESOURCE_DESC,
            D3D12_RESOURCE_STATES,
            ?*const D3D12_CLEAR_VALUE,
            *const GUID,
            *?*anyopaque,
        ) callconv(.winapi) HRESULT,
        CreateHeap: PlaceholderFn,
        CreatePlacedResource: PlaceholderFn,
        CreateReservedResource: PlaceholderFn,
        CreateSharedHandle: PlaceholderFn,
        OpenSharedHandle: PlaceholderFn,
        OpenSharedHandleByName: PlaceholderFn,
        MakeResident: PlaceholderFn,
        Evict: PlaceholderFn,
        CreateFence: *const fn (*ID3D12Device, UINT64, D3D12_FENCE_FLAGS, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        GetDeviceRemovedReason: *const fn (*ID3D12Device) callconv(.winapi) HRESULT,
        GetCopyableFootprints: *const fn (
            *ID3D12Device,
            *const D3D12_RESOURCE_DESC,
            UINT,
            UINT,
            UINT64,
            ?[*]D3D12_PLACED_SUBRESOURCE_FOOTPRINT,
            ?[*]UINT,
            ?[*]UINT64,
            ?*UINT64,
        ) callconv(.winapi) void,
        CreateQueryHeap: PlaceholderFn,
        SetStablePowerState: PlaceholderFn,
        CreateCommandSignature: PlaceholderFn,
        GetResourceTiling: PlaceholderFn,
        // [43] MSVC COM hidden out-pointer ABI!
        GetAdapterLuid: *const fn (*ID3D12Device, *LUID) callconv(.winapi) *LUID,
    };

    pub inline fn release(self: *ID3D12Device) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn getDescriptorHandleIncrementSize(self: *ID3D12Device, heap_type: D3D12_DESCRIPTOR_HEAP_TYPE) UINT {
        return self.vtbl.GetDescriptorHandleIncrementSize(self, heap_type);
    }

    pub inline fn getDeviceRemovedReason(self: *ID3D12Device) HRESULT {
        return self.vtbl.GetDeviceRemovedReason(self);
    }

    pub inline fn createCommandQueue(self: *ID3D12Device, desc: *const D3D12_COMMAND_QUEUE_DESC, out: *?*ID3D12CommandQueue) HRESULT {
        var raw: ?*anyopaque = null;
        const hr = self.vtbl.CreateCommandQueue(self, desc, &IID_ID3D12CommandQueue, &raw);
        out.* = if (win32.succeeded(hr) and raw != null) @ptrCast(@alignCast(raw.?)) else null;
        return hr;
    }

    pub inline fn createCommandAllocator(self: *ID3D12Device, list_type: D3D12_COMMAND_LIST_TYPE, out: *?*ID3D12CommandAllocator) HRESULT {
        var raw: ?*anyopaque = null;
        const hr = self.vtbl.CreateCommandAllocator(self, list_type, &IID_ID3D12CommandAllocator, &raw);
        out.* = if (win32.succeeded(hr) and raw != null) @ptrCast(@alignCast(raw.?)) else null;
        return hr;
    }

    pub inline fn createCommandList(
        self: *ID3D12Device,
        node_mask: UINT,
        list_type: D3D12_COMMAND_LIST_TYPE,
        allocator: *ID3D12CommandAllocator,
        initial_pso: ?*ID3D12PipelineState,
        out: *?*ID3D12GraphicsCommandList,
    ) HRESULT {
        var raw: ?*anyopaque = null;
        const hr = self.vtbl.CreateCommandList(self, node_mask, list_type, allocator, initial_pso, &IID_ID3D12GraphicsCommandList, &raw);
        out.* = if (win32.succeeded(hr) and raw != null) @ptrCast(@alignCast(raw.?)) else null;
        return hr;
    }

    pub inline fn createDescriptorHeap(self: *ID3D12Device, desc: *const D3D12_DESCRIPTOR_HEAP_DESC, out: *?*ID3D12DescriptorHeap) HRESULT {
        var raw: ?*anyopaque = null;
        const hr = self.vtbl.CreateDescriptorHeap(self, desc, &IID_ID3D12DescriptorHeap, &raw);
        out.* = if (win32.succeeded(hr) and raw != null) @ptrCast(@alignCast(raw.?)) else null;
        return hr;
    }

    pub inline fn createRootSignature(
        self: *ID3D12Device,
        node_mask: UINT,
        blob_ptr: *const anyopaque,
        blob_len: usize,
        out: *?*ID3D12RootSignature,
    ) HRESULT {
        var raw: ?*anyopaque = null;
        const hr = self.vtbl.CreateRootSignature(self, node_mask, blob_ptr, blob_len, &IID_ID3D12RootSignature, &raw);
        out.* = if (win32.succeeded(hr) and raw != null) @ptrCast(@alignCast(raw.?)) else null;
        return hr;
    }

    pub inline fn createGraphicsPipelineState(
        self: *ID3D12Device,
        desc: *const D3D12_GRAPHICS_PIPELINE_STATE_DESC,
        out: *?*ID3D12PipelineState,
    ) HRESULT {
        var raw: ?*anyopaque = null;
        const hr = self.vtbl.CreateGraphicsPipelineState(self, desc, &IID_ID3D12PipelineState, &raw);
        out.* = if (win32.succeeded(hr) and raw != null) @ptrCast(@alignCast(raw.?)) else null;
        return hr;
    }

    pub inline fn createCommittedResource(
        self: *ID3D12Device,
        heap_props: *const D3D12_HEAP_PROPERTIES,
        heap_flags: D3D12_HEAP_FLAGS,
        desc: *const D3D12_RESOURCE_DESC,
        initial_state: D3D12_RESOURCE_STATES,
        clear_val: ?*const D3D12_CLEAR_VALUE,
        out: *?*ID3D12Resource,
    ) HRESULT {
        var raw: ?*anyopaque = null;
        const hr = self.vtbl.CreateCommittedResource(self, heap_props, heap_flags, desc, initial_state, clear_val, &IID_ID3D12Resource, &raw);
        out.* = if (win32.succeeded(hr) and raw != null) @ptrCast(@alignCast(raw.?)) else null;
        return hr;
    }

    pub inline fn createFence(self: *ID3D12Device, initial_value: UINT64, flags: D3D12_FENCE_FLAGS, out: *?*ID3D12Fence) HRESULT {
        var raw: ?*anyopaque = null;
        const hr = self.vtbl.CreateFence(self, initial_value, flags, &IID_ID3D12Fence, &raw);
        out.* = if (win32.succeeded(hr) and raw != null) @ptrCast(@alignCast(raw.?)) else null;
        return hr;
    }

    pub inline fn createShaderResourceView(
        self: *ID3D12Device,
        resource: ?*ID3D12Resource,
        desc: ?*const D3D12_SHADER_RESOURCE_VIEW_DESC,
        dst: D3D12_CPU_DESCRIPTOR_HANDLE,
    ) void {
        self.vtbl.CreateShaderResourceView(self, resource, desc, dst);
    }

    pub inline fn createRenderTargetView(
        self: *ID3D12Device,
        resource: ?*ID3D12Resource,
        desc: ?*const D3D12_RENDER_TARGET_VIEW_DESC,
        dst: D3D12_CPU_DESCRIPTOR_HANDLE,
    ) void {
        self.vtbl.CreateRenderTargetView(self, resource, desc, dst);
    }

    pub inline fn copyDescriptors(
        self: *ID3D12Device,
        dst_starts: []const D3D12_CPU_DESCRIPTOR_HANDLE,
        dst_sizes: ?[]const UINT,
        src_starts: []const D3D12_CPU_DESCRIPTOR_HANDLE,
        src_sizes: ?[]const UINT,
        heap_type: D3D12_DESCRIPTOR_HEAP_TYPE,
    ) void {
        self.vtbl.CopyDescriptors(
            self,
            @intCast(dst_starts.len),
            dst_starts.ptr,
            if (dst_sizes) |s| s.ptr else null,
            @intCast(src_starts.len),
            src_starts.ptr,
            if (src_sizes) |s| s.ptr else null,
            heap_type,
        );
    }

    pub inline fn getCopyableFootprints(
        self: *ID3D12Device,
        desc: *const D3D12_RESOURCE_DESC,
        first_subresource: UINT,
        num_subresources: UINT,
        base_offset: UINT64,
        out_layout: *D3D12_PLACED_SUBRESOURCE_FOOTPRINT,
        out_num_rows: *UINT,
        out_row_size: *UINT64,
        out_total_bytes: *UINT64,
    ) void {
        self.vtbl.GetCopyableFootprints(
            self,
            desc,
            first_subresource,
            num_subresources,
            base_offset,
            @ptrCast(out_layout),
            @ptrCast(out_num_rows),
            @ptrCast(out_row_size),
            out_total_bytes,
        );
    }

    pub inline fn getResourceAllocationInfo(
        self: *ID3D12Device,
        visible_mask: UINT,
        descs: []const D3D12_RESOURCE_DESC,
    ) D3D12_RESOURCE_ALLOCATION_INFO {
        var out: D3D12_RESOURCE_ALLOCATION_INFO = .{};
        _ = self.vtbl.GetResourceAllocationInfo(self, &out, visible_mask, @intCast(descs.len), descs.ptr);
        return out;
    }
};

// --- Runtime DLL loader for d3d12.dll ---

pub const D3D12CreateDeviceFn = *const fn (
    pAdapter: ?*anyopaque,
    MinimumFeatureLevel: D3D_FEATURE_LEVEL,
    riid: *const GUID,
    ppDevice: ?*?*anyopaque,
) callconv(.winapi) HRESULT;

pub const D3D12GetDebugInterfaceFn = *const fn (
    riid: *const GUID,
    ppvDebug: *?*anyopaque,
) callconv(.winapi) HRESULT;

pub const D3D12SerializeRootSignatureFn = *const fn (
    pRootSignature: *const D3D12_ROOT_SIGNATURE_DESC,
    Version: D3D_ROOT_SIGNATURE_VERSION,
    ppBlob: *?*ID3DBlob,
    ppErrorBlob: ?*?*ID3DBlob,
) callconv(.winapi) HRESULT;

pub const D3D12Lib = struct {
    dll: dynlib.DynLib,
    create_device: D3D12CreateDeviceFn,
    get_debug_interface: ?D3D12GetDebugInterfaceFn,
    serialize_root_signature: D3D12SerializeRootSignatureFn,

    pub fn load(diag: *Diag) Error!D3D12Lib {
        if (builtin.os.tag != .windows) {
            diag.set("d3d12.dll is only available on Windows");
            return error.BackendUnavailable;
        }
        var dll = dynlib.DynLib.open("d3d12.dll") catch {
            diag.set("directx: failed to load d3d12.dll");
            return error.BackendUnavailable;
        };
        const create_dev = dll.lookup(D3D12CreateDeviceFn, "D3D12CreateDevice") orelse {
            dll.close();
            diag.set("directx: D3D12CreateDevice not found in d3d12.dll");
            return error.BackendUnavailable;
        };
        const serialize_rs = dll.lookup(D3D12SerializeRootSignatureFn, "D3D12SerializeRootSignature") orelse {
            dll.close();
            diag.set("directx: D3D12SerializeRootSignature not found in d3d12.dll");
            return error.BackendUnavailable;
        };
        const get_dbg = dll.lookup(D3D12GetDebugInterfaceFn, "D3D12GetDebugInterface");
        return .{
            .dll = dll,
            .create_device = create_dev,
            .get_debug_interface = get_dbg,
            .serialize_root_signature = serialize_rs,
        };
    }

    pub fn deinit(self: *D3D12Lib) void {
        self.dll.close();
    }
};

// --- Exhaustive comptime ABI, struct layout, vtable slot, and hidden-pointer assertions ---
comptime {
    // Descriptor handles
    std.debug.assert(@sizeOf(D3D12_CPU_DESCRIPTOR_HANDLE) == 8);
    std.debug.assert(@sizeOf(D3D12_GPU_DESCRIPTOR_HANDLE) == 8);
    std.debug.assert(D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING == 0x1688);
    std.debug.assert(D3D12_SHADER_4_COMPONENT_MAPPING_R_R_R_R == 0x1000);

    // Core struct sizes & offsets on 64-bit Windows
    std.debug.assert(@sizeOf(D3D12_HEAP_PROPERTIES) == 20);
    std.debug.assert(@sizeOf(D3D12_RESOURCE_DESC) == 56);
    std.debug.assert(@offsetOf(D3D12_RESOURCE_DESC, "Alignment") == 8);
    std.debug.assert(@offsetOf(D3D12_RESOURCE_DESC, "Width") == 16);
    std.debug.assert(@offsetOf(D3D12_RESOURCE_DESC, "Format") == 32);
    std.debug.assert(@offsetOf(D3D12_RESOURCE_DESC, "Flags") == 48);

    std.debug.assert(@sizeOf(D3D12_SHADER_RESOURCE_VIEW_DESC) == 40);
    std.debug.assert(@offsetOf(D3D12_SHADER_RESOURCE_VIEW_DESC, "Shader4ComponentMapping") == 8);
    std.debug.assert(@offsetOf(D3D12_SHADER_RESOURCE_VIEW_DESC, "u") == 16);

    std.debug.assert(@sizeOf(D3D12_ROOT_PARAMETER) == 32);
    std.debug.assert(@offsetOf(D3D12_ROOT_PARAMETER, "u") == 8);
    std.debug.assert(@offsetOf(D3D12_ROOT_PARAMETER, "ShaderVisibility") == 24);

    std.debug.assert(@sizeOf(D3D12_ROOT_SIGNATURE_DESC) == 40);
    std.debug.assert(@sizeOf(D3D12_RESOURCE_BARRIER) == 32);
    std.debug.assert(@offsetOf(D3D12_RESOURCE_BARRIER, "u") == 8);

    std.debug.assert(@sizeOf(D3D12_PLACED_SUBRESOURCE_FOOTPRINT) == 32);
    std.debug.assert(@sizeOf(D3D12_TEXTURE_COPY_LOCATION) == 48);
    std.debug.assert(@offsetOf(D3D12_TEXTURE_COPY_LOCATION, "u") == 16);

    std.debug.assert(@sizeOf(D3D12_RENDER_TARGET_BLEND_DESC) == 40);
    std.debug.assert(@sizeOf(D3D12_BLEND_DESC) == 328);
    std.debug.assert(@sizeOf(D3D12_RASTERIZER_DESC) == 44);
    std.debug.assert(@sizeOf(D3D12_DEPTH_STENCIL_DESC) == 52);
    std.debug.assert(@sizeOf(D3D12_GRAPHICS_PIPELINE_STATE_DESC) == 656);
    std.debug.assert(@offsetOf(D3D12_GRAPHICS_PIPELINE_STATE_DESC, "BlendState") == 120);
    std.debug.assert(@offsetOf(D3D12_GRAPHICS_PIPELINE_STATE_DESC, "RasterizerState") == 452);
    std.debug.assert(@offsetOf(D3D12_GRAPHICS_PIPELINE_STATE_DESC, "InputLayout") == 552);
    std.debug.assert(@offsetOf(D3D12_GRAPHICS_PIPELINE_STATE_DESC, "RTVFormats") == 580);
    std.debug.assert(@offsetOf(D3D12_GRAPHICS_PIPELINE_STATE_DESC, "Flags") == 648);

    // Vtable slot verification
    com.expectVtblSlot(ID3D12Debug.Vtbl, "EnableDebugLayer", 3);
    com.expectVtblSlot(ID3D12DeviceRemovedExtendedDataSettings.Vtbl, "SetAutoBreadcrumbsEnablement", 3);
    com.expectVtblSlot(ID3D12DeviceRemovedExtendedDataSettings.Vtbl, "SetPageFaultEnablement", 4);
    com.expectVtblSlot(ID3D12DeviceRemovedExtendedData.Vtbl, "GetAutoBreadcrumbsOutput", 3);
    com.expectVtblSlot(ID3D12DeviceRemovedExtendedData.Vtbl, "GetPageFaultAllocationOutput", 4);

    com.expectVtblSlot(ID3D12CommandAllocator.Vtbl, "Reset", 8);
    com.expectVtblSlot(ID3D12Fence.Vtbl, "GetCompletedValue", 8);
    com.expectVtblSlot(ID3D12Fence.Vtbl, "SetEventOnCompletion", 9);
    com.expectVtblSlot(ID3D12Fence.Vtbl, "Signal", 10);

    com.expectVtblSlot(ID3D12DescriptorHeap.Vtbl, "GetDesc", 8);
    com.expectVtblSlot(ID3D12DescriptorHeap.Vtbl, "GetCPUDescriptorHandleForHeapStart", 9);
    com.expectVtblSlot(ID3D12DescriptorHeap.Vtbl, "GetGPUDescriptorHandleForHeapStart", 10);

    com.expectVtblSlot(ID3D12Resource.Vtbl, "Map", 8);
    com.expectVtblSlot(ID3D12Resource.Vtbl, "Unmap", 9);
    com.expectVtblSlot(ID3D12Resource.Vtbl, "GetDesc", 10);
    com.expectVtblSlot(ID3D12Resource.Vtbl, "GetGPUVirtualAddress", 11);

    com.expectVtblSlot(ID3D12CommandQueue.Vtbl, "ExecuteCommandLists", 10);
    com.expectVtblSlot(ID3D12CommandQueue.Vtbl, "Signal", 14);
    com.expectVtblSlot(ID3D12CommandQueue.Vtbl, "GetDesc", 18);

    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "Close", 9);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "Reset", 10);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "DrawIndexedInstanced", 13);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "CopyTextureRegion", 16);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "IASetPrimitiveTopology", 20);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "RSSetViewports", 21);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "RSSetScissorRects", 22);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "SetPipelineState", 25);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "ResourceBarrier", 26);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "SetDescriptorHeaps", 28);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "SetGraphicsRootSignature", 30);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "SetGraphicsRootDescriptorTable", 32);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "SetGraphicsRootConstantBufferView", 38);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "IASetIndexBuffer", 43);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "IASetVertexBuffers", 44);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "OMSetRenderTargets", 46);
    com.expectVtblSlot(ID3D12GraphicsCommandList.Vtbl, "ExecuteIndirect", 59);

    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateCommandQueue", 8);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateCommandAllocator", 9);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateGraphicsPipelineState", 10);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateCommandList", 12);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateDescriptorHeap", 14);
    com.expectVtblSlot(ID3D12Device.Vtbl, "GetDescriptorHandleIncrementSize", 15);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateRootSignature", 16);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateShaderResourceView", 18);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateRenderTargetView", 20);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CopyDescriptors", 23);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CopyDescriptorsSimple", 24);
    com.expectVtblSlot(ID3D12Device.Vtbl, "GetResourceAllocationInfo", 25);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateCommittedResource", 27);
    com.expectVtblSlot(ID3D12Device.Vtbl, "CreateFence", 36);
    com.expectVtblSlot(ID3D12Device.Vtbl, "GetDeviceRemovedReason", 37);
    com.expectVtblSlot(ID3D12Device.Vtbl, "GetCopyableFootprints", 38);
    com.expectVtblSlot(ID3D12Device.Vtbl, "GetAdapterLuid", 43);

    // MSVC COM hidden out-pointer ABI checks
    com.expectHiddenReturnAbi(
        @FieldType(ID3D12DescriptorHeap.Vtbl, "GetCPUDescriptorHandleForHeapStart"),
        ID3D12DescriptorHeap,
        D3D12_CPU_DESCRIPTOR_HANDLE,
    );
    com.expectHiddenReturnAbi(
        @FieldType(ID3D12DescriptorHeap.Vtbl, "GetGPUDescriptorHandleForHeapStart"),
        ID3D12DescriptorHeap,
        D3D12_GPU_DESCRIPTOR_HANDLE,
    );
    com.expectHiddenReturnAbi(
        @FieldType(ID3D12DescriptorHeap.Vtbl, "GetDesc"),
        ID3D12DescriptorHeap,
        D3D12_DESCRIPTOR_HEAP_DESC,
    );
    com.expectHiddenReturnAbi(
        @FieldType(ID3D12Resource.Vtbl, "GetDesc"),
        ID3D12Resource,
        D3D12_RESOURCE_DESC,
    );
    com.expectHiddenReturnAbi(
        @FieldType(ID3D12CommandQueue.Vtbl, "GetDesc"),
        ID3D12CommandQueue,
        D3D12_COMMAND_QUEUE_DESC,
    );
    com.expectHiddenReturnAbi(
        @FieldType(ID3D12Device.Vtbl, "GetResourceAllocationInfo"),
        ID3D12Device,
        D3D12_RESOURCE_ALLOCATION_INFO,
    );
    com.expectHiddenReturnAbi(
        @FieldType(ID3D12Device.Vtbl, "GetAdapterLuid"),
        ID3D12Device,
        LUID,
    );
}
