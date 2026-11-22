const std = @import("std");
const builtin = @import("builtin");

const is_windows = builtin.os.tag == .windows;

const win = if (is_windows) struct {
    const HMODULE = *opaque {};
    const FARPROC = *const anyopaque;

    extern "kernel32" fn LoadLibraryW(lpLibFileName: [*:0]const u16) callconv(.winapi) ?HMODULE;
    extern "kernel32" fn GetProcAddress(hModule: HMODULE, lpProcName: [*:0]const u8) callconv(.winapi) ?FARPROC;
    extern "kernel32" fn FreeLibrary(hLibModule: HMODULE) callconv(.winapi) c_int;
} else struct {};

pub const OpenError = error{
    FileNotFound,
    InvalidUtf8,
    PathTooLong,
};

fn utf8PathToWideZ(wide_buf: *[512:0]u16, path: []const u8) OpenError![:0]const u16 {
    const utf16_len = std.unicode.calcUtf16LeLen(path) catch return error.InvalidUtf8;
    if (utf16_len > wide_buf.len) return error.PathTooLong;
    const written = std.unicode.utf8ToUtf16Le(wide_buf[0..utf16_len], path) catch return error.InvalidUtf8;
    wide_buf[written] = 0;
    return wide_buf[0..written :0];
}

pub const DynLib = struct {
    handle: if (is_windows) win.HMODULE else std.DynLib,

    pub fn open(path: []const u8) OpenError!DynLib {
        if (is_windows) {
            var wide_buf: [512:0]u16 = undefined;
            const path_w = try utf8PathToWideZ(&wide_buf, path);
            return openW(path_w);
        } else {
            const lib = std.DynLib.open(path) catch return error.FileNotFound;
            return .{ .handle = lib };
        }
    }

    pub fn openZ(path_c: [:0]const u8) OpenError!DynLib {
        if (is_windows) {
            return open(path_c);
        } else {
            const lib = std.DynLib.openZ(path_c.ptr) catch return error.FileNotFound;
            return .{ .handle = lib };
        }
    }

    pub fn openW(path_w: [:0]const u16) OpenError!DynLib {
        if (!is_windows) @compileError("openW is only available on Windows");
        const h = win.LoadLibraryW(path_w.ptr) orelse return error.FileNotFound;
        return .{ .handle = h };
    }

    pub fn lookup(self: *DynLib, comptime T: type, name: [:0]const u8) ?T {
        if (is_windows) {
            const proc = win.GetProcAddress(self.handle, name.ptr) orelse return null;
            return @ptrCast(@alignCast(proc));
        } else {
            return self.handle.lookup(T, name);
        }
    }

    pub fn close(self: *DynLib) void {
        if (is_windows) {
            _ = win.FreeLibrary(self.handle);
        } else {
            self.handle.close();
        }
    }
};

test "utf8PathToWideZ rejects oversized paths without panic" {
    var wide_buf: [512:0]u16 = undefined;

    // >600-byte path must return PathTooLong without overflowing the stack buffer.
    const long_path: [601]u8 = @splat('a');
    try std.testing.expectError(error.PathTooLong, utf8PathToWideZ(&wide_buf, &long_path));

    // Exact 512-unit boundary succeeds and NUL-terminates at index 512.
    const exact_path: [512]u8 = @splat('b');
    const exact_w = try utf8PathToWideZ(&wide_buf, &exact_path);
    try std.testing.expectEqual(@as(usize, 512), exact_w.len);
    try std.testing.expectEqual(@as(u16, 0), exact_w[512]);
    try std.testing.expectEqual(@as(u16, 'b'), exact_w[0]);
    try std.testing.expectEqual(@as(u16, 'b'), exact_w[511]);

    // 513-unit path exceeds capacity.
    const over_by_one: [513]u8 = @splat('c');
    try std.testing.expectError(error.PathTooLong, utf8PathToWideZ(&wide_buf, &over_by_one));

    // Invalid UTF-8 returns InvalidUtf8.
    const bad_utf8 = [_]u8{ 0xFF, 0xFE };
    try std.testing.expectError(error.InvalidUtf8, utf8PathToWideZ(&wide_buf, &bad_utf8));
}
