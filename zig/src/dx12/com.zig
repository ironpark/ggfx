const std = @import("std");
const win32 = @import("win32.zig");
const HRESULT = win32.HRESULT;

pub const GUID = extern struct {
    Data1: u32,
    Data2: u16,
    Data3: u16,
    Data4: [8]u8,
};

/// Base COM IUnknown interface.
pub const IUnknown = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0] IUnknown::QueryInterface
        QueryInterface: *const fn (self: *IUnknown, riid: *const GUID, ppvObject: *?*anyopaque) callconv(.winapi) HRESULT,
        // [1] IUnknown::AddRef
        AddRef: *const fn (self: *IUnknown) callconv(.winapi) u32,
        // [2] IUnknown::Release
        Release: *const fn (self: *IUnknown) callconv(.winapi) u32,
    };

    pub inline fn queryInterface(self: *IUnknown, riid: *const GUID, out: *?*anyopaque) HRESULT {
        return self.vtbl.QueryInterface(self, riid, out);
    }

    pub inline fn addRef(self: *IUnknown) u32 {
        return self.vtbl.AddRef(self);
    }

    pub inline fn release(self: *IUnknown) u32 {
        return self.vtbl.Release(self);
    }
};

/// QueryInterface wrapper returning a typed pointer to `Target` on S_OK.
pub inline fn queryInterface(comptime Target: type, obj: anytype, riid: *const GUID) ?*Target {
    const unk: *IUnknown = @ptrCast(obj);
    var out: ?*anyopaque = null;
    const hr = unk.vtbl.QueryInterface(unk, riid, &out);
    if (!win32.succeeded(hr)) return null;
    const ptr = out orelse return null;
    return @ptrCast(@alignCast(ptr));
}

pub inline fn addRef(obj: anytype) u32 {
    const unk: *IUnknown = @ptrCast(obj);
    return unk.vtbl.AddRef(unk);
}

pub inline fn release(obj: anytype) u32 {
    const unk: *IUnknown = @ptrCast(obj);
    return unk.vtbl.Release(unk);
}

/// Releases an optional COM pointer slot and clears it to null.
pub inline fn safeRelease(slot: anytype) void {
    if (slot.*) |ptr| {
        _ = release(ptr);
        slot.* = null;
    }
}

/// Comptime verifier that a COM vtable field resides at the exact expected slot index.
pub fn expectVtblSlot(comptime Vtbl: type, comptime field: []const u8, comptime slot: usize) void {
    const actual = @offsetOf(Vtbl, field);
    const expected = slot * @sizeOf(usize);
    if (actual != expected) {
        @compileError(std.fmt.comptimePrint(
            "COM vtable slot mismatch for {s}.{s}: byte offset {d} (slot {d}), want byte offset {d} (slot {d})",
            .{ @typeName(Vtbl), field, actual, actual / @sizeOf(usize), expected, slot },
        ));
    }
}

/// Comptime verifier for MSVC COM hidden-out-pointer ABI on struct-returning methods:
///   fn (self: *Self, out: *Ret) callconv(.winapi) *Ret
pub fn expectHiddenReturnAbi(comptime FnPtr: type, comptime Self: type, comptime Ret: type) void {
    const ptr_info = @typeInfo(FnPtr);
    if (ptr_info != .pointer) {
        @compileError("expected function pointer for hidden return ABI check");
    }
    const fn_info = @typeInfo(ptr_info.pointer.child).@"fn";
    if (!std.meta.eql(fn_info.attrs.@"callconv", std.builtin.CallingConvention.winapi)) {
        @compileError("COM method must use callconv(.winapi)");
    }
    if (fn_info.param_types.len < 2) {
        @compileError("MSVC COM struct-returning method must take (self, out: *Ret, ...)");
    }
    if (fn_info.param_types[0] != *Self) {
        @compileError("COM method param 0 must be *Self");
    }
    if (fn_info.param_types[1] != *Ret) {
        @compileError("MSVC COM struct-returning method param 1 must be hidden out pointer *Ret");
    }
    if (fn_info.return_type != *Ret) {
        @compileError("MSVC COM struct-returning method must return *Ret");
    }
}

comptime {
    std.debug.assert(@sizeOf(GUID) == 16);
    std.debug.assert(@offsetOf(GUID, "Data1") == 0);
    std.debug.assert(@offsetOf(GUID, "Data2") == 4);
    std.debug.assert(@offsetOf(GUID, "Data3") == 6);
    std.debug.assert(@offsetOf(GUID, "Data4") == 8);

    std.debug.assert(@sizeOf(IUnknown) == @sizeOf(usize));
    expectVtblSlot(IUnknown.Vtbl, "QueryInterface", 0);
    expectVtblSlot(IUnknown.Vtbl, "AddRef", 1);
    expectVtblSlot(IUnknown.Vtbl, "Release", 2);
}
