const std = @import("std");
const builtin = @import("builtin");
const dynlib = @import("../dynlib.zig");
const errors = @import("../errors.zig");

const is_windows = builtin.os.tag == .windows;

pub const BOOL = i32;
pub const HRESULT = i32;
pub const UINT = u32;
pub const INT = i32;
pub const DWORD = u32;
pub const ULONG = u32;
pub const LONG = i32;
pub const UINT64 = u64;
pub const SIZE_T = usize;
pub const HANDLE = *opaque {};
pub const HWND = *opaque {};

pub const S_OK: HRESULT = 0;
pub const S_FALSE: HRESULT = 1;

pub const E_NOINTERFACE: HRESULT = @bitCast(@as(u32, 0x8000_4002));
pub const E_POINTER: HRESULT = @bitCast(@as(u32, 0x8000_4003));
pub const E_FAIL: HRESULT = @bitCast(@as(u32, 0x8000_4005));
pub const E_OUTOFMEMORY: HRESULT = @bitCast(@as(u32, 0x8007_000E));
pub const E_INVALIDARG: HRESULT = @bitCast(@as(u32, 0x8007_0057));

pub const DXGI_STATUS_OCCLUDED: HRESULT = @bitCast(@as(u32, 0x087A_0001));
pub const DXGI_ERROR_INVALID_CALL: HRESULT = @bitCast(@as(u32, 0x887A_0001));
pub const DXGI_ERROR_NOT_FOUND: HRESULT = @bitCast(@as(u32, 0x887A_0002));
pub const DXGI_ERROR_UNSUPPORTED: HRESULT = @bitCast(@as(u32, 0x887A_0004));
pub const DXGI_ERROR_DEVICE_REMOVED: HRESULT = @bitCast(@as(u32, 0x887A_0005));
pub const DXGI_ERROR_DEVICE_HUNG: HRESULT = @bitCast(@as(u32, 0x887A_0006));
pub const DXGI_ERROR_DEVICE_RESET: HRESULT = @bitCast(@as(u32, 0x887A_0007));
pub const DXGI_ERROR_DRIVER_INTERNAL_ERROR: HRESULT = @bitCast(@as(u32, 0x887A_0020));
pub const DXGI_ERROR_SDK_COMPONENT_MISSING: HRESULT = @bitCast(@as(u32, 0x887A_002D));

pub const INFINITE: DWORD = 0xFFFF_FFFF;
pub const WAIT_OBJECT_0: DWORD = 0x0000_0000;
pub const WAIT_FAILED: DWORD = 0xFFFF_FFFF;

pub const EVENT_MODIFY_STATE: DWORD = 0x0002;
pub const SYNCHRONIZE: DWORD = 0x0010_0000;

pub const GWL_EXSTYLE: INT = -20;
pub const WS_EX_NOREDIRECTIONBITMAP: LONG = 0x0020_0000;

pub const LUID = extern struct {
    LowPart: DWORD,
    HighPart: LONG,
};

pub const RECT = extern struct {
    left: LONG,
    top: LONG,
    right: LONG,
    bottom: LONG,

    pub fn eql(a: RECT, b: RECT) bool {
        return a.left == b.left and a.top == b.top and a.right == b.right and a.bottom == b.bottom;
    }
};

pub inline fn succeeded(hr: HRESULT) bool {
    return hr >= 0;
}

pub inline fn failed(hr: HRESULT) bool {
    return hr < 0;
}

pub inline fn hrHex(hr: HRESULT) u32 {
    return @bitCast(hr);
}

pub fn isDeviceLost(hr: HRESULT) bool {
    return hr == DXGI_ERROR_DEVICE_REMOVED or
        hr == DXGI_ERROR_DEVICE_HUNG or
        hr == DXGI_ERROR_DEVICE_RESET or
        hr == DXGI_ERROR_DRIVER_INTERNAL_ERROR;
}

pub fn hrToError(hr: HRESULT) errors.Error {
    if (isDeviceLost(hr)) return error.DeviceLost;
    if (hr == E_OUTOFMEMORY) return error.OutOfMemory;
    if (hr == E_INVALIDARG or hr == DXGI_ERROR_INVALID_CALL) return error.InvalidArgument;
    if (hr == DXGI_ERROR_UNSUPPORTED) return error.Unsupported;
    return error.Internal;
}

const kernel32 = if (is_windows) struct {
    extern "kernel32" fn CreateEventExW(
        lpEventAttributes: ?*anyopaque,
        lpName: ?[*:0]const u16,
        dwFlags: DWORD,
        dwDesiredAccess: DWORD,
    ) callconv(.winapi) ?HANDLE;

    extern "kernel32" fn WaitForSingleObject(
        hHandle: HANDLE,
        dwMilliseconds: DWORD,
    ) callconv(.winapi) DWORD;

    extern "kernel32" fn CloseHandle(hObject: HANDLE) callconv(.winapi) BOOL;
} else struct {};

pub fn createEvent() ?HANDLE {
    if (!is_windows) return null;
    return kernel32.CreateEventExW(null, null, 0, EVENT_MODIFY_STATE | SYNCHRONIZE);
}

pub fn waitForSingleObject(handle: HANDLE, timeout_ms: DWORD) bool {
    if (!is_windows) return false;
    return kernel32.WaitForSingleObject(handle, timeout_ms) == WAIT_OBJECT_0;
}

pub fn closeHandle(handle: HANDLE) void {
    if (!is_windows) return;
    _ = kernel32.CloseHandle(handle);
}

pub const GetWindowLongWFn = *const fn (hWnd: HWND, nIndex: INT) callconv(.winapi) LONG;

pub const User32Lib = struct {
    dll: ?dynlib.DynLib = null,
    get_window_long_w: ?GetWindowLongWFn = null,

    pub fn load() User32Lib {
        if (!is_windows) return .{};
        var dll = dynlib.DynLib.open("user32.dll") catch return .{};
        const proc = dll.lookup(GetWindowLongWFn, "GetWindowLongW") orelse {
            dll.close();
            return .{};
        };
        return .{
            .dll = dll,
            .get_window_long_w = proc,
        };
    }

    pub fn deinit(self: *User32Lib) void {
        if (self.dll) |*d| {
            d.close();
            self.dll = null;
        }
        self.get_window_long_w = null;
    }

    pub fn windowHasNoRedirectionBitmap(self: *const User32Lib, hwnd: HWND) bool {
        const proc = self.get_window_long_w orelse return false;
        const ex_style = proc(hwnd, GWL_EXSTYLE);
        return (ex_style & WS_EX_NOREDIRECTIONBITMAP) != 0;
    }
};

/// Convert a NUL-terminated UTF-16LE buffer (such as DXGI_ADAPTER_DESC1.Description)
/// into a NUL-terminated UTF-8 fixed-size destination buffer.
pub fn utf16LeToUtf8Z(dst: []u8, src: []const u16) void {
    if (dst.len == 0) return;
    var src_len: usize = 0;
    while (src_len < src.len and src[src_len] != 0) : (src_len += 1) {}
    const slice = src[0..src_len];

    const max_out = dst.len - 1;
    var out_idx: usize = 0;
    var it = std.unicode.Utf16LeIterator.init(slice);
    while (it.nextCodepoint() catch null) |cp| {
        var encoded: [4]u8 = undefined;
        const n = std.unicode.utf8Encode(cp, &encoded) catch continue;
        if (out_idx + n > max_out) break;
        @memcpy(dst[out_idx .. out_idx + n], encoded[0..n]);
        out_idx += n;
    }
    dst[out_idx] = 0;
}

comptime {
    std.debug.assert(@sizeOf(LUID) == 8);
    std.debug.assert(@offsetOf(LUID, "LowPart") == 0);
    std.debug.assert(@offsetOf(LUID, "HighPart") == 4);

    std.debug.assert(@sizeOf(RECT) == 16);
    std.debug.assert(@offsetOf(RECT, "left") == 0);
    std.debug.assert(@offsetOf(RECT, "top") == 4);
    std.debug.assert(@offsetOf(RECT, "right") == 8);
    std.debug.assert(@offsetOf(RECT, "bottom") == 12);
}
