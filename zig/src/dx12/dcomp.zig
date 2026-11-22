const std = @import("std");
const builtin = @import("builtin");
const dynlib = @import("../dynlib.zig");
const win32 = @import("win32.zig");
const com = @import("com.zig");

const BOOL = win32.BOOL;
const HRESULT = win32.HRESULT;
const HWND = win32.HWND;
const GUID = com.GUID;
const PlaceholderFn = *const fn () callconv(.winapi) void;

pub const IID_IDCompositionDevice = GUID{
    .Data1 = 0xc37ea93a,
    .Data2 = 0xe7aa,
    .Data3 = 0x450d,
    .Data4 = .{ 0xb1, 0x6f, 0x97, 0x46, 0xcb, 0x04, 0x07, 0xf3 },
};

pub const IDCompositionVisual = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDCompositionVisual, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDCompositionVisual) callconv(.winapi) u32,
        Release: *const fn (*IDCompositionVisual) callconv(.winapi) u32,
        // [3..15] IDCompositionVisual (dcomp.h order)
        SetOffsetX_Animation: PlaceholderFn,
        SetOffsetX: PlaceholderFn,
        SetOffsetY_Animation: PlaceholderFn,
        SetOffsetY: PlaceholderFn,
        SetTransform_Object: PlaceholderFn,
        SetTransform: PlaceholderFn,
        SetTransformParent: PlaceholderFn,
        SetEffect: PlaceholderFn,
        SetBitmapInterpolationMode: PlaceholderFn,
        SetBorderMode: PlaceholderFn,
        SetClip_Object: PlaceholderFn,
        SetClip: PlaceholderFn,
        SetContent: *const fn (*IDCompositionVisual, ?*anyopaque) callconv(.winapi) HRESULT,
    };

    pub inline fn release(self: *IDCompositionVisual) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn setContent(self: *IDCompositionVisual, content: ?*anyopaque) HRESULT {
        return self.vtbl.SetContent(self, content);
    }
};

pub const IDCompositionTarget = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDCompositionTarget, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDCompositionTarget) callconv(.winapi) u32,
        Release: *const fn (*IDCompositionTarget) callconv(.winapi) u32,
        // [3] IDCompositionTarget
        SetRoot: *const fn (*IDCompositionTarget, ?*IDCompositionVisual) callconv(.winapi) HRESULT,
    };

    pub inline fn release(self: *IDCompositionTarget) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn setRoot(self: *IDCompositionTarget, visual: ?*IDCompositionVisual) HRESULT {
        return self.vtbl.SetRoot(self, visual);
    }
};

pub const IDCompositionDevice = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*IDCompositionDevice, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*IDCompositionDevice) callconv(.winapi) u32,
        Release: *const fn (*IDCompositionDevice) callconv(.winapi) u32,
        // [3..7] IDCompositionDevice (dcomp.h order)
        Commit: *const fn (*IDCompositionDevice) callconv(.winapi) HRESULT,
        WaitForCommitCompletion: *const fn (*IDCompositionDevice) callconv(.winapi) HRESULT,
        GetFrameStatistics: PlaceholderFn,
        CreateTargetForHwnd: *const fn (*IDCompositionDevice, HWND, BOOL, *?*IDCompositionTarget) callconv(.winapi) HRESULT,
        CreateVisual: *const fn (*IDCompositionDevice, *?*IDCompositionVisual) callconv(.winapi) HRESULT,
    };

    pub inline fn release(self: *IDCompositionDevice) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn commit(self: *IDCompositionDevice) HRESULT {
        return self.vtbl.Commit(self);
    }

    pub inline fn createTargetForHwnd(
        self: *IDCompositionDevice,
        hwnd: HWND,
        topmost: bool,
        out: *?*IDCompositionTarget,
    ) HRESULT {
        return self.vtbl.CreateTargetForHwnd(self, hwnd, if (topmost) 1 else 0, out);
    }

    pub inline fn createVisual(self: *IDCompositionDevice, out: *?*IDCompositionVisual) HRESULT {
        return self.vtbl.CreateVisual(self, out);
    }
};

pub const DCompositionCreateDeviceFn = *const fn (
    dxgiDevice: ?*anyopaque,
    iid: *const GUID,
    dcompositionDevice: *?*anyopaque,
) callconv(.winapi) HRESULT;

pub const DCompLib = struct {
    dll: ?dynlib.DynLib = null,
    create_device: ?DCompositionCreateDeviceFn = null,

    /// DirectComposition is optional; returns an empty handle if dcomp.dll is absent.
    pub fn loadOptional() DCompLib {
        if (builtin.os.tag != .windows) return .{};
        var dll = dynlib.DynLib.open("dcomp.dll") catch return .{};
        const proc = dll.lookup(DCompositionCreateDeviceFn, "DCompositionCreateDevice") orelse {
            dll.close();
            return .{};
        };
        return .{
            .dll = dll,
            .create_device = proc,
        };
    }

    pub fn deinit(self: *DCompLib) void {
        if (self.dll) |*d| {
            d.close();
            self.dll = null;
        }
        self.create_device = null;
    }

    pub fn isAvailable(self: *const DCompLib) bool {
        return self.create_device != null;
    }

    pub fn createDevice(self: *const DCompLib, dxgi_device: ?*anyopaque, out: *?*IDCompositionDevice) HRESULT {
        const proc = self.create_device orelse return win32.E_NOINTERFACE;
        var raw: ?*anyopaque = null;
        const hr = proc(dxgi_device, &IID_IDCompositionDevice, &raw);
        if (win32.succeeded(hr)) {
            out.* = if (raw) |p| @ptrCast(@alignCast(p)) else null;
        } else {
            out.* = null;
        }
        return hr;
    }
};

comptime {
    com.expectVtblSlot(IDCompositionDevice.Vtbl, "Commit", 3);
    com.expectVtblSlot(IDCompositionDevice.Vtbl, "WaitForCommitCompletion", 4);
    com.expectVtblSlot(IDCompositionDevice.Vtbl, "GetFrameStatistics", 5);
    com.expectVtblSlot(IDCompositionDevice.Vtbl, "CreateTargetForHwnd", 6);
    com.expectVtblSlot(IDCompositionDevice.Vtbl, "CreateVisual", 7);

    com.expectVtblSlot(IDCompositionTarget.Vtbl, "SetRoot", 3);

    com.expectVtblSlot(IDCompositionVisual.Vtbl, "SetOffsetX_Animation", 3);
    com.expectVtblSlot(IDCompositionVisual.Vtbl, "SetContent", 15);
}
