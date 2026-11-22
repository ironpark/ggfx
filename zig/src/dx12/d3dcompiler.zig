const std = @import("std");
const builtin = @import("builtin");
const dynlib = @import("../dynlib.zig");
const errors = @import("../errors.zig");
const win32 = @import("win32.zig");
const com = @import("com.zig");

const HRESULT = win32.HRESULT;
const UINT = win32.UINT;
const GUID = com.GUID;
const Error = errors.Error;
const Diag = errors.Diag;

pub const D3DCOMPILE_OPTIMIZATION_LEVEL3: UINT = 1 << 15;

pub const D3D_SHADER_MACRO = extern struct {
    Name: ?[*:0]const u8,
    Definition: ?[*:0]const u8,
};

pub const ID3DBlob = extern struct {
    vtbl: *const Vtbl,

    pub const Vtbl = extern struct {
        // [0..2] IUnknown
        QueryInterface: *const fn (*ID3DBlob, *const GUID, *?*anyopaque) callconv(.winapi) HRESULT,
        AddRef: *const fn (*ID3DBlob) callconv(.winapi) u32,
        Release: *const fn (*ID3DBlob) callconv(.winapi) u32,
        // [3..4] ID3D10Blob / ID3DBlob
        GetBufferPointer: *const fn (*ID3DBlob) callconv(.winapi) ?*anyopaque,
        GetBufferSize: *const fn (*ID3DBlob) callconv(.winapi) usize,
    };

    pub inline fn addRef(self: *ID3DBlob) u32 {
        return self.vtbl.AddRef(self);
    }

    pub inline fn release(self: *ID3DBlob) u32 {
        return self.vtbl.Release(self);
    }

    pub inline fn getBufferPointer(self: *ID3DBlob) ?*anyopaque {
        return self.vtbl.GetBufferPointer(self);
    }

    pub inline fn getBufferSize(self: *ID3DBlob) usize {
        return self.vtbl.GetBufferSize(self);
    }

    pub fn slice(self: *ID3DBlob) []const u8 {
        const len = self.getBufferSize();
        if (len == 0) return &.{};
        const ptr = self.getBufferPointer() orelse return &.{};
        return @as([*]const u8, @ptrCast(ptr))[0..len];
    }
};

pub const D3DCompileFn = *const fn (
    pSrcData: [*]const u8,
    SrcDataSize: usize,
    pSourceName: ?[*:0]const u8,
    pDefines: ?[*]const D3D_SHADER_MACRO,
    pInclude: ?*anyopaque,
    pEntrypoint: [*:0]const u8,
    pTarget: [*:0]const u8,
    Flags1: UINT,
    Flags2: UINT,
    ppCode: *?*ID3DBlob,
    ppErrorMsgs: *?*ID3DBlob,
) callconv(.winapi) HRESULT;

pub const CompilerLib = struct {
    dll: dynlib.DynLib,
    compile_fn: D3DCompileFn,

    /// Loads d3dcompiler_47.dll -> d3dcompiler_46.dll -> d3dcompiler_43.dll.
    pub fn load(diag: *Diag) Error!CompilerLib {
        if (builtin.os.tag != .windows) {
            diag.set("directx: d3dcompiler_*.dll is only available on Windows");
            return error.BackendUnavailable;
        }
        const candidates = [_][]const u8{
            "d3dcompiler_47.dll",
            "d3dcompiler_46.dll",
            "d3dcompiler_43.dll",
        };
        for (candidates) |name| {
            var dll = dynlib.DynLib.open(name) catch continue;
            if (dll.lookup(D3DCompileFn, "D3DCompile")) |proc| {
                return .{
                    .dll = dll,
                    .compile_fn = proc,
                };
            }
            dll.close();
        }
        diag.set("directx: d3dcompiler_*.dll is missing in this environment");
        return error.BackendUnavailable;
    }

    pub fn deinit(self: *CompilerLib) void {
        self.dll.close();
    }

    pub fn compile(
        self: *const CompilerLib,
        source: []const u8,
        entry: [*:0]const u8,
        target: [*:0]const u8,
        diag: *Diag,
    ) Error!*ID3DBlob {
        if (source.len == 0) {
            diag.set("directx: HLSL shader source is empty");
            return error.ShaderCompile;
        }
        var code_blob: ?*ID3DBlob = null;
        var err_blob: ?*ID3DBlob = null;
        defer if (err_blob) |eb| {
            _ = eb.release();
        };

        const hr = self.compile_fn(
            source.ptr,
            source.len,
            "shader",
            null,
            null,
            entry,
            target,
            D3DCOMPILE_OPTIMIZATION_LEVEL3,
            0,
            &code_blob,
            &err_blob,
        );
        if (win32.failed(hr) or code_blob == null) {
            if (code_blob) |cb| {
                _ = cb.release();
            }
            if (err_blob) |eb| {
                const msg = std.mem.trimEnd(u8, eb.slice(), "\r\n\x00 ");
                diag.print("directx: D3DCompile ({s}) failed (0x{x:0>8}): {s}", .{
                    std.mem.span(target),
                    win32.hrHex(hr),
                    msg,
                });
            } else {
                diag.print("directx: D3DCompile ({s}) failed (0x{x:0>8})", .{
                    std.mem.span(target),
                    win32.hrHex(hr),
                });
            }
            return error.ShaderCompile;
        }
        return code_blob.?;
    }
};

comptime {
    com.expectVtblSlot(ID3DBlob.Vtbl, "QueryInterface", 0);
    com.expectVtblSlot(ID3DBlob.Vtbl, "AddRef", 1);
    com.expectVtblSlot(ID3DBlob.Vtbl, "Release", 2);
    com.expectVtblSlot(ID3DBlob.Vtbl, "GetBufferPointer", 3);
    com.expectVtblSlot(ID3DBlob.Vtbl, "GetBufferSize", 4);
}
