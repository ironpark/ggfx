const std = @import("std");
const builtin = @import("builtin");
const dynlib = @import("../dynlib.zig");
const errors = @import("../errors.zig");
const win32 = @import("win32.zig");
const com = @import("com.zig");

const BOOL = win32.BOOL;
const HRESULT = win32.HRESULT;
const UINT = win32.UINT;
const HWND = win32.HWND;
const LUID = win32.LUID;
const GUID = com.GUID;
const Error = errors.Error;
const Diag = errors.Diag;

pub const DXGI_FORMAT = i32;
pub const DXGI_FORMAT_UNKNOWN: DXGI_FORMAT = 0;
pub const DXGI_FORMAT_R32G32B32A32_FLOAT: DXGI_FORMAT = 2;
pub const DXGI_FORMAT_R32G32_FLOAT: DXGI_FORMAT = 16;
pub const DXGI_FORMAT_R8G8B8A8_UNORM: DXGI_FORMAT = 28;
pub const DXGI_FORMAT_R32_UINT: DXGI_FORMAT = 42;
pub const DXGI_FORMAT_D24_UNORM_S8_UINT: DXGI_FORMAT = 45;
pub const DXGI_FORMAT_R8_UNORM: DXGI_FORMAT = 61;
pub const DXGI_FORMAT_B8G8R8A8_UNORM: DXGI_FORMAT = 87;

pub const DXGI_ALPHA_MODE = u32;
pub const DXGI_ALPHA_MODE_UNSPECIFIED: DXGI_ALPHA_MODE = 0;
pub const DXGI_ALPHA_MODE_PREMULTIPLIED: DXGI_ALPHA_MODE = 1;
pub const DXGI_ALPHA_MODE_STRAIGHT: DXGI_ALPHA_MODE = 2;
pub const DXGI_ALPHA_MODE_IGNORE: DXGI_ALPHA_MODE = 3;

pub const DXGI_FEATURE = i32;
pub const DXGI_FEATURE_PRESENT_ALLOW_TEARING: DXGI_FEATURE = 0;

pub const DXGI_GPU_PREFERENCE = i32;
pub const DXGI_GPU_PREFERENCE_UNSPECIFIED: DXGI_GPU_PREFERENCE = 0;
pub const DXGI_GPU_PREFERENCE_MINIMUM_POWER: DXGI_GPU_PREFERENCE = 1;
pub const DXGI_GPU_PREFERENCE_HIGH_PERFORMANCE: DXGI_GPU_PREFERENCE = 2;

pub const DXGI_SCALING = i32;
pub const DXGI_SCALING_STRETCH: DXGI_SCALING = 0;
pub const DXGI_SCALING_NONE: DXGI_SCALING = 1;

pub const DXGI_SWAP_EFFECT = i32;
pub const DXGI_SWAP_EFFECT_DISCARD: DXGI_SWAP_EFFECT = 0;
pub const DXGI_SWAP_EFFECT_SEQUENTIAL: DXGI_SWAP_EFFECT = 1;
pub const DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL: DXGI_SWAP_EFFECT = 3;
pub const DXGI_SWAP_EFFECT_FLIP_DISCARD: DXGI_SWAP_EFFECT = 4;

pub const DXGI_USAGE = u32;
pub const DXGI_USAGE_RENDER_TARGET_OUTPUT: DXGI_USAGE = 1 << (1 + 4);

pub const DXGI_PRESENT_TEST: UINT = 0x0000_0001;
pub const DXGI_PRESENT_ALLOW_TEARING: UINT = 0x0000_0200;

pub const DXGI_SWAP_CHAIN_FLAG_ALLOW_TEARING: UINT = 2048;

pub const DXGI_ADAPTER_FLAG_SOFTWARE: UINT = 2;
pub const DXGI_CREATE_FACTORY_DEBUG: UINT = 0x01;

pub const DXGI_MWA_NO_WINDOW_CHANGES: UINT = 0x1;
pub const DXGI_MWA_NO_ALT_ENTER: UINT = 0x2;

// Standard DXGI IIDs (dxgi.h / dxgi1_4.h / dxgi1_5.h / dxgi1_6.h)
pub const IID_IDXGIAdapter1 = GUID{
    .Data1 = 0x29038f61,
    .Data2 = 0x3839,
    .Data3 = 0x4626,
    .Data4 = .{ 0x91, 0xfd, 0x08, 0x68, 0x79, 0x01, 0x1a, 0x05 },
};

pub const IID_IDXGIFactory = GUID{
    .Data1 = 0x7b7166ec,
    .Data2 = 0x21c7,
    .Data3 = 0x44ae,
    .Data4 = .{ 0xb2, 0x1a, 0xc9, 0xae, 0x32, 0x1a, 0xe3, 0x69 },
};

pub const IID_IDXGIFactory4 = GUID{
    .Data1 = 0x1bc6ea02,
    .Data2 = 0xef36,
    .Data3 = 0x464f,
    .Data4 = .{ 0xbf, 0x0c, 0x21, 0xca, 0x39, 0xe5, 0x16, 0x8a },
};

pub const IID_IDXGIFactory5 = GUID{
    .Data1 = 0x7632e1f5,
    .Data2 = 0xee65,
    .Data3 = 0x4dca,
    .Data4 = .{ 0x87, 0xfd, 0x84, 0xcd, 0x75, 0xf8, 0x83, 0x8d },
};

pub const IID_IDXGIFactory6 = GUID{
    .Data1 = 0xc1b6694f,
    .Data2 = 0xff09,
    .Data3 = 0x44a9,
    .Data4 = .{ 0xb0, 0x3c, 0x77, 0x90, 0x0a, 0x0a, 0x1d, 0x17 },
};

pub const IID_IDXGISwapChain1 = GUID{
    .Data1 = 0x790a45f7,
    .Data2 = 0x0d42,
    .Data3 = 0x4876,
    .Data4 = .{ 0x98, 0x3a, 0x0a, 0x55, 0xcf, 0xe6, 0xf4, 0xaa },
};

pub const IID_IDXGISwapChain3 = GUID{
    .Data1 = 0x94d99bdb,
    .Data2 = 0xf1f8,
    .Data3 = 0x4ab0,
    .Data4 = .{ 0xb2, 0x36, 0x7d, 0xa0, 0x17, 0x0e, 0xda, 0xb1 },
};

pub const IID_IDXGISwapChain4 = GUID{
    .Data1 = 0x3d585d5a,
    .Data2 = 0xbd4a,
    .Data3 = 0x489e,
    .Data4 = .{ 0xb1, 0xf4, 0x3d, 0xbc, 0xb6, 0x45, 0x2f, 0xfb },
};

pub const DXGI_RATIONAL = extern struct {
    Numerator: UINT = 0,
    Denominator: UINT = 0,
};

pub const DXGI_SAMPLE_DESC = extern struct {
    Count: UINT = 1,
    Quality: UINT = 0,
};

pub const DXGI_MODE_DESC = extern struct {
    Width: UINT = 0,
    Height: UINT = 0,
    RefreshRate: DXGI_RATIONAL = .{},
    Format: DXGI_FORMAT = DXGI_FORMAT_UNKNOWN,
    ScanlineOrdering: i32 = 0,
    Scaling: i32 = 0,
};

pub const DXGI_SWAP_CHAIN_FULLSCREEN_DESC = extern struct {
    RefreshRate: DXGI_RATIONAL = .{},
    ScanlineOrdering: i32 = 0,
    Scaling: i32 = 0,
    Windowed: BOOL = 1,
};

pub const DXGI_SWAP_CHAIN_DESC = extern struct {
    BufferDesc: DXGI_MODE_DESC = .{},
    SampleDesc: DXGI_SAMPLE_DESC = .{},
    BufferUsage: DXGI_USAGE = 0,
    BufferCount: UINT = 0,
    OutputWindow: ?HWND = null,
    Windowed: BOOL = 1,
    SwapEffect: DXGI_SWAP_EFFECT = DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
    Flags: UINT = 0,
};

pub const DXGI_SWAP_CHAIN_DESC1 = extern struct {
    Width: UINT = 0,
    Height: UINT = 0,
    Format: DXGI_FORMAT = DXGI_FORMAT_UNKNOWN,
    Stereo: BOOL = 0,
    SampleDesc: DXGI_SAMPLE_DESC = .{},
    BufferUsage: DXGI_USAGE = 0,
    BufferCount: UINT = 0,
    Scaling: DXGI_SCALING = DXGI_SCALING_STRETCH,
    SwapEffect: DXGI_SWAP_EFFECT = DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
    AlphaMode: DXGI_ALPHA_MODE = DXGI_ALPHA_MODE_UNSPECIFIED,
    Flags: UINT = 0,
};

pub const DXGI_ADAPTER_DESC1 = extern struct {
    Description: [128]u16,
    VendorId: UINT,
    DeviceId: UINT,
    SubSysId: UINT,
    Revision: UINT,
    DedicatedVideoMemory: usize,
    DedicatedSystemMemory: usize,
    SharedSystemMemory: usize,
    AdapterLuid: LUID,
    Flags: UINT,
};

const PlaceholderFn = *const fn () callconv(.winapi) void;

pub const IDXGIOutput = extern struct {
    vtbl: *const Vtbl,
    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDXGIOutput, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDXGIOutput) callconv(.winapi) u32,
        Release: *const fn (*IDXGIOutput) callconv(.winapi) u32,
    };
};

pub const IDXGIAdapter1 = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDXGIAdapter1, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDXGIAdapter1) callconv(.winapi) u32,
        Release: *const fn (*IDXGIAdapter1) callconv(.winapi) u32,
        // [3..6] IDXGIObject
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        GetPrivateData: PlaceholderFn,
        GetParent: *const fn (*IDXGIAdapter1, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        // [7..9] IDXGIAdapter
        EnumOutputs: *const fn (*IDXGIAdapter1, UINT, *?*IDXGIOutput) callconv(.winapi) HRESULT,
        GetDesc: PlaceholderFn,
        CheckInterfaceSupport: PlaceholderFn,
        // [10] IDXGIAdapter1
        GetDesc1: *const fn (*IDXGIAdapter1, *DXGI_ADAPTER_DESC1) callconv(.winapi) HRESULT,
    };

    pub inline fn release(self: *IDXGIAdapter1) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn getDesc1(self: *IDXGIAdapter1, out: *DXGI_ADAPTER_DESC1) HRESULT {
        return self.vtbl.GetDesc1(self, out);
    }
};

pub const IDXGISwapChain1 = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDXGISwapChain1, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDXGISwapChain1) callconv(.winapi) u32,
        Release: *const fn (*IDXGISwapChain1) callconv(.winapi) u32,
        // [3..6] IDXGIObject
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        GetPrivateData: PlaceholderFn,
        GetParent: PlaceholderFn,
        // [7] IDXGIDeviceSubObject
        GetDevice: PlaceholderFn,
        // [8..17] IDXGISwapChain
        Present: *const fn (*IDXGISwapChain1, UINT, UINT) callconv(.winapi) HRESULT,
        GetBuffer: *const fn (*IDXGISwapChain1, UINT, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        SetFullscreenState: PlaceholderFn,
        GetFullscreenState: PlaceholderFn,
        GetDesc: PlaceholderFn,
        ResizeBuffers: *const fn (*IDXGISwapChain1, UINT, UINT, UINT, DXGI_FORMAT, UINT) callconv(.winapi) HRESULT,
        ResizeTarget: PlaceholderFn,
        GetContainingOutput: PlaceholderFn,
        GetFrameStatistics: PlaceholderFn,
        GetLastPresentCount: PlaceholderFn,
        // [18..28] IDXGISwapChain1
        GetDesc1: *const fn (*IDXGISwapChain1, *DXGI_SWAP_CHAIN_DESC1) callconv(.winapi) HRESULT,
        GetFullscreenDesc: PlaceholderFn,
        GetHwnd: PlaceholderFn,
        GetCoreWindow: PlaceholderFn,
        Present1: PlaceholderFn,
        IsTemporaryMonoSupported: PlaceholderFn,
        GetRestrictToOutput: PlaceholderFn,
        SetBackgroundColor: PlaceholderFn,
        GetBackgroundColor: PlaceholderFn,
        SetRotation: PlaceholderFn,
        GetRotation: PlaceholderFn,
    };

    pub inline fn release(self: *IDXGISwapChain1) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn present(self: *IDXGISwapChain1, sync_interval: UINT, flags: UINT) HRESULT {
        return self.vtbl.Present(self, sync_interval, flags);
    }

    pub inline fn getBuffer(self: *IDXGISwapChain1, index: UINT, riid: *const GUID, out: *?*anyopaque) HRESULT {
        return self.vtbl.GetBuffer(self, index, riid, out);
    }

    pub inline fn resizeBuffers(
        self: *IDXGISwapChain1,
        buffer_count: UINT,
        width: UINT,
        height: UINT,
        format: DXGI_FORMAT,
        flags: UINT,
    ) HRESULT {
        return self.vtbl.ResizeBuffers(self, buffer_count, width, height, format, flags);
    }
};

pub const IDXGISwapChain3 = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDXGISwapChain3, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDXGISwapChain3) callconv(.winapi) u32,
        Release: *const fn (*IDXGISwapChain3) callconv(.winapi) u32,
        // [3..6] IDXGIObject
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        GetPrivateData: PlaceholderFn,
        GetParent: PlaceholderFn,
        // [7] IDXGIDeviceSubObject
        GetDevice: PlaceholderFn,
        // [8..17] IDXGISwapChain
        Present: *const fn (*IDXGISwapChain3, UINT, UINT) callconv(.winapi) HRESULT,
        GetBuffer: *const fn (*IDXGISwapChain3, UINT, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        SetFullscreenState: PlaceholderFn,
        GetFullscreenState: PlaceholderFn,
        GetDesc: PlaceholderFn,
        ResizeBuffers: *const fn (*IDXGISwapChain3, UINT, UINT, UINT, DXGI_FORMAT, UINT) callconv(.winapi) HRESULT,
        ResizeTarget: PlaceholderFn,
        GetContainingOutput: PlaceholderFn,
        GetFrameStatistics: PlaceholderFn,
        GetLastPresentCount: PlaceholderFn,
        // [18..28] IDXGISwapChain1
        GetDesc1: *const fn (*IDXGISwapChain3, *DXGI_SWAP_CHAIN_DESC1) callconv(.winapi) HRESULT,
        GetFullscreenDesc: PlaceholderFn,
        GetHwnd: PlaceholderFn,
        GetCoreWindow: PlaceholderFn,
        Present1: PlaceholderFn,
        IsTemporaryMonoSupported: PlaceholderFn,
        GetRestrictToOutput: PlaceholderFn,
        SetBackgroundColor: PlaceholderFn,
        GetBackgroundColor: PlaceholderFn,
        SetRotation: PlaceholderFn,
        GetRotation: PlaceholderFn,
        // [29..35] IDXGISwapChain2
        SetSourceSize: PlaceholderFn,
        GetSourceSize: PlaceholderFn,
        SetMaximumFrameLatency: PlaceholderFn,
        GetMaximumFrameLatency: PlaceholderFn,
        GetFrameLatencyWaitableObject: PlaceholderFn,
        SetMatrixTransform: PlaceholderFn,
        GetMatrixTransform: PlaceholderFn,
        // [36..39] IDXGISwapChain3
        GetCurrentBackBufferIndex: *const fn (*IDXGISwapChain3) callconv(.winapi) UINT,
        CheckColorSpaceSupport: PlaceholderFn,
        SetColorSpace1: PlaceholderFn,
        ResizeBuffers1: PlaceholderFn,
    };

    pub inline fn release(self: *IDXGISwapChain3) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn present(self: *IDXGISwapChain3, sync_interval: UINT, flags: UINT) HRESULT {
        return self.vtbl.Present(self, sync_interval, flags);
    }

    pub inline fn getBuffer(self: *IDXGISwapChain3, index: UINT, riid: *const GUID, out: *?*anyopaque) HRESULT {
        return self.vtbl.GetBuffer(self, index, riid, out);
    }

    pub inline fn resizeBuffers(
        self: *IDXGISwapChain3,
        buffer_count: UINT,
        width: UINT,
        height: UINT,
        format: DXGI_FORMAT,
        flags: UINT,
    ) HRESULT {
        return self.vtbl.ResizeBuffers(self, buffer_count, width, height, format, flags);
    }

    pub inline fn getCurrentBackBufferIndex(self: *IDXGISwapChain3) UINT {
        return self.vtbl.GetCurrentBackBufferIndex(self);
    }
};

pub const IDXGIFactory4 = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDXGIFactory4, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDXGIFactory4) callconv(.winapi) u32,
        Release: *const fn (*IDXGIFactory4) callconv(.winapi) u32,
        // [3..6] IDXGIObject
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        GetPrivateData: PlaceholderFn,
        GetParent: PlaceholderFn,
        // [7..11] IDXGIFactory
        EnumAdapters: PlaceholderFn,
        MakeWindowAssociation: *const fn (*IDXGIFactory4, HWND, UINT) callconv(.winapi) HRESULT,
        GetWindowAssociation: PlaceholderFn,
        CreateSwapChain: PlaceholderFn,
        CreateSoftwareAdapter: PlaceholderFn,
        // [12..13] IDXGIFactory1
        EnumAdapters1: *const fn (*IDXGIFactory4, UINT, *?*IDXGIAdapter1) callconv(.winapi) HRESULT,
        IsCurrent: PlaceholderFn,
        // [14..24] IDXGIFactory2
        IsWindowedStereoEnabled: PlaceholderFn,
        CreateSwapChainForHwnd: *const fn (
            *IDXGIFactory4,
            *anyopaque,
            HWND,
            *const DXGI_SWAP_CHAIN_DESC1,
            ?*const DXGI_SWAP_CHAIN_FULLSCREEN_DESC,
            ?*IDXGIOutput,
            *?*IDXGISwapChain1,
        ) callconv(.winapi) HRESULT,
        CreateSwapChainForCoreWindow: PlaceholderFn,
        GetSharedResourceAdapterLuid: PlaceholderFn,
        RegisterStereoStatusWindow: PlaceholderFn,
        RegisterStereoStatusEvent: PlaceholderFn,
        UnregisterStereoStatus: PlaceholderFn,
        RegisterOcclusionStatusWindow: PlaceholderFn,
        RegisterOcclusionStatusEvent: PlaceholderFn,
        UnregisterOcclusionStatus: PlaceholderFn,
        CreateSwapChainForComposition: *const fn (
            *IDXGIFactory4,
            *anyopaque,
            *const DXGI_SWAP_CHAIN_DESC1,
            ?*IDXGIOutput,
            *?*IDXGISwapChain1,
        ) callconv(.winapi) HRESULT,
        // [25] IDXGIFactory3
        GetCreationFlags: *const fn (*IDXGIFactory4) callconv(.winapi) UINT,
        // [26..27] IDXGIFactory4
        EnumAdapterByLuid: *const fn (*IDXGIFactory4, LUID, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        EnumWarpAdapter: *const fn (*IDXGIFactory4, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
    };

    pub inline fn release(self: *IDXGIFactory4) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn makeWindowAssociation(self: *IDXGIFactory4, hwnd: HWND, flags: UINT) HRESULT {
        return self.vtbl.MakeWindowAssociation(self, hwnd, flags);
    }

    pub inline fn enumAdapters1(self: *IDXGIFactory4, index: UINT, out: *?*IDXGIAdapter1) HRESULT {
        return self.vtbl.EnumAdapters1(self, index, out);
    }

    pub inline fn enumWarpAdapter(self: *IDXGIFactory4, riid: *const GUID, out: *?*anyopaque) HRESULT {
        return self.vtbl.EnumWarpAdapter(self, riid, out);
    }

    pub inline fn createSwapChainForHwnd(
        self: *IDXGIFactory4,
        device: *anyopaque,
        hwnd: HWND,
        desc: *const DXGI_SWAP_CHAIN_DESC1,
        fullscreen: ?*const DXGI_SWAP_CHAIN_FULLSCREEN_DESC,
        restrict_output: ?*IDXGIOutput,
        out: *?*IDXGISwapChain1,
    ) HRESULT {
        return self.vtbl.CreateSwapChainForHwnd(self, device, hwnd, desc, fullscreen, restrict_output, out);
    }

    pub inline fn createSwapChainForComposition(
        self: *IDXGIFactory4,
        device: *anyopaque,
        desc: *const DXGI_SWAP_CHAIN_DESC1,
        restrict_output: ?*IDXGIOutput,
        out: *?*IDXGISwapChain1,
    ) HRESULT {
        return self.vtbl.CreateSwapChainForComposition(self, device, desc, restrict_output, out);
    }
};

pub const IDXGIFactory5 = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDXGIFactory5, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDXGIFactory5) callconv(.winapi) u32,
        Release: *const fn (*IDXGIFactory5) callconv(.winapi) u32,
        // [3..6] IDXGIObject
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        GetPrivateData: PlaceholderFn,
        GetParent: PlaceholderFn,
        // [7..11] IDXGIFactory
        EnumAdapters: PlaceholderFn,
        MakeWindowAssociation: PlaceholderFn,
        GetWindowAssociation: PlaceholderFn,
        CreateSwapChain: PlaceholderFn,
        CreateSoftwareAdapter: PlaceholderFn,
        // [12..13] IDXGIFactory1
        EnumAdapters1: PlaceholderFn,
        IsCurrent: PlaceholderFn,
        // [14..24] IDXGIFactory2
        IsWindowedStereoEnabled: PlaceholderFn,
        CreateSwapChainForHwnd: PlaceholderFn,
        CreateSwapChainForCoreWindow: PlaceholderFn,
        GetSharedResourceAdapterLuid: PlaceholderFn,
        RegisterStereoStatusWindow: PlaceholderFn,
        RegisterStereoStatusEvent: PlaceholderFn,
        UnregisterStereoStatus: PlaceholderFn,
        RegisterOcclusionStatusWindow: PlaceholderFn,
        RegisterOcclusionStatusEvent: PlaceholderFn,
        UnregisterOcclusionStatus: PlaceholderFn,
        CreateSwapChainForComposition: PlaceholderFn,
        // [25] IDXGIFactory3
        GetCreationFlags: PlaceholderFn,
        // [26..27] IDXGIFactory4
        EnumAdapterByLuid: PlaceholderFn,
        EnumWarpAdapter: PlaceholderFn,
        // [28] IDXGIFactory5
        CheckFeatureSupport: *const fn (*IDXGIFactory5, DXGI_FEATURE, *anyopaque, UINT) callconv(.winapi) HRESULT,
    };

    pub inline fn release(self: *IDXGIFactory5) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn checkFeatureSupport(self: *IDXGIFactory5, feature: DXGI_FEATURE, data: *anyopaque, size: UINT) HRESULT {
        return self.vtbl.CheckFeatureSupport(self, feature, data, size);
    }
};

pub const IDXGIFactory6 = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDXGIFactory6, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDXGIFactory6) callconv(.winapi) u32,
        Release: *const fn (*IDXGIFactory6) callconv(.winapi) u32,
        // [3..6] IDXGIObject
        SetPrivateData: PlaceholderFn,
        SetPrivateDataInterface: PlaceholderFn,
        GetPrivateData: PlaceholderFn,
        GetParent: PlaceholderFn,
        // [7..11] IDXGIFactory
        EnumAdapters: PlaceholderFn,
        MakeWindowAssociation: PlaceholderFn,
        GetWindowAssociation: PlaceholderFn,
        CreateSwapChain: PlaceholderFn,
        CreateSoftwareAdapter: PlaceholderFn,
        // [12..13] IDXGIFactory1
        EnumAdapters1: PlaceholderFn,
        IsCurrent: PlaceholderFn,
        // [14..24] IDXGIFactory2
        IsWindowedStereoEnabled: PlaceholderFn,
        CreateSwapChainForHwnd: PlaceholderFn,
        CreateSwapChainForCoreWindow: PlaceholderFn,
        GetSharedResourceAdapterLuid: PlaceholderFn,
        RegisterStereoStatusWindow: PlaceholderFn,
        RegisterStereoStatusEvent: PlaceholderFn,
        UnregisterStereoStatus: PlaceholderFn,
        RegisterOcclusionStatusWindow: PlaceholderFn,
        RegisterOcclusionStatusEvent: PlaceholderFn,
        UnregisterOcclusionStatus: PlaceholderFn,
        CreateSwapChainForComposition: PlaceholderFn,
        // [25] IDXGIFactory3
        GetCreationFlags: PlaceholderFn,
        // [26..27] IDXGIFactory4
        EnumAdapterByLuid: PlaceholderFn,
        EnumWarpAdapter: PlaceholderFn,
        // [28] IDXGIFactory5
        CheckFeatureSupport: PlaceholderFn,
        // [29] IDXGIFactory6
        EnumAdapterByGpuPreference: *const fn (*IDXGIFactory6, UINT, DXGI_GPU_PREFERENCE, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
    };

    pub inline fn release(self: *IDXGIFactory6) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn enumAdapterByGpuPreference(
        self: *IDXGIFactory6,
        index: UINT,
        pref: DXGI_GPU_PREFERENCE,
        riid: *const GUID,
        out: *?*anyopaque,
    ) HRESULT {
        return self.vtbl.EnumAdapterByGpuPreference(self, index, pref, riid, out);
    }
};

pub const CreateDXGIFactory2Fn = *const fn (flags: UINT, riid: *const GUID, ppFactory: *?*anyopaque) callconv(.winapi) HRESULT;

pub const DxgiLib = struct {
    dll: dynlib.DynLib,
    create_factory2: CreateDXGIFactory2Fn,

    pub fn load(diag: *Diag) Error!DxgiLib {
        if (builtin.os.tag != .windows) {
            diag.set("dxgi.dll is only available on Windows");
            return error.BackendUnavailable;
        }
        var dll = dynlib.DynLib.open("dxgi.dll") catch {
            diag.set("directx: failed to load dxgi.dll");
            return error.BackendUnavailable;
        };
        const proc = dll.lookup(CreateDXGIFactory2Fn, "CreateDXGIFactory2") orelse {
            dll.close();
            diag.set("directx: CreateDXGIFactory2 not found in dxgi.dll");
            return error.BackendUnavailable;
        };
        return .{
            .dll = dll,
            .create_factory2 = proc,
        };
    }

    pub fn deinit(self: *DxgiLib) void {
        self.dll.close();
    }
};

comptime {
    std.debug.assert(@sizeOf(DXGI_SAMPLE_DESC) == 8);
    std.debug.assert(@sizeOf(DXGI_SWAP_CHAIN_DESC1) == 48);
    std.debug.assert(@offsetOf(DXGI_SWAP_CHAIN_DESC1, "AlphaMode") == 40);
    std.debug.assert(@offsetOf(DXGI_SWAP_CHAIN_DESC1, "Flags") == 44);

    std.debug.assert(@sizeOf(DXGI_ADAPTER_DESC1) == 312);
    std.debug.assert(@offsetOf(DXGI_ADAPTER_DESC1, "VendorId") == 256);
    std.debug.assert(@offsetOf(DXGI_ADAPTER_DESC1, "DedicatedVideoMemory") == 272);
    std.debug.assert(@offsetOf(DXGI_ADAPTER_DESC1, "AdapterLuid") == 296);
    std.debug.assert(@offsetOf(DXGI_ADAPTER_DESC1, "Flags") == 304);

    com.expectVtblSlot(IDXGIAdapter1.Vtbl, "EnumOutputs", 7);
    com.expectVtblSlot(IDXGIAdapter1.Vtbl, "GetDesc1", 10);

    com.expectVtblSlot(IDXGISwapChain1.Vtbl, "Present", 8);
    com.expectVtblSlot(IDXGISwapChain1.Vtbl, "GetBuffer", 9);
    com.expectVtblSlot(IDXGISwapChain1.Vtbl, "ResizeBuffers", 13);
    com.expectVtblSlot(IDXGISwapChain1.Vtbl, "GetDesc1", 18);

    com.expectVtblSlot(IDXGISwapChain3.Vtbl, "Present", 8);
    com.expectVtblSlot(IDXGISwapChain3.Vtbl, "GetBuffer", 9);
    com.expectVtblSlot(IDXGISwapChain3.Vtbl, "ResizeBuffers", 13);
    com.expectVtblSlot(IDXGISwapChain3.Vtbl, "GetCurrentBackBufferIndex", 36);
    com.expectVtblSlot(IDXGISwapChain3.Vtbl, "ResizeBuffers1", 39);

    com.expectVtblSlot(IDXGIFactory4.Vtbl, "MakeWindowAssociation", 8);
    com.expectVtblSlot(IDXGIFactory4.Vtbl, "EnumAdapters1", 12);
    com.expectVtblSlot(IDXGIFactory4.Vtbl, "CreateSwapChainForHwnd", 15);
    com.expectVtblSlot(IDXGIFactory4.Vtbl, "CreateSwapChainForComposition", 24);
    com.expectVtblSlot(IDXGIFactory4.Vtbl, "GetCreationFlags", 25);
    com.expectVtblSlot(IDXGIFactory4.Vtbl, "EnumAdapterByLuid", 26);
    com.expectVtblSlot(IDXGIFactory4.Vtbl, "EnumWarpAdapter", 27);

    com.expectVtblSlot(IDXGIFactory5.Vtbl, "CheckFeatureSupport", 28);
    com.expectVtblSlot(IDXGIFactory6.Vtbl, "EnumAdapterByGpuPreference", 29);
}
