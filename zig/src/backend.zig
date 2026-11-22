const std = @import("std");
const builtin = @import("builtin");
const build_options = @import("build_options");
const abi = @import("abi.zig");
const errors = @import("errors.zig");
const replayer = @import("replayer.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const E = errors.Error;

pub const Impl = if (build_options.null_backend)
    @import("null/backend.zig").Backend
else switch (builtin.os.tag) {
    .macos => @import("metal/backend.zig").Backend,
    .windows => @import("dx12/backend.zig").Backend,
    .linux => @import("vulkan/backend.zig").Backend,
    else => @import("null/backend.zig").Backend,
};

fn expectDecl(comptime Self: type, comptime name: []const u8, comptime Expected: type) void {
    if (!@hasDecl(Self, name)) {
        @compileError("backend " ++ @typeName(Self) ++ " is missing required declaration '" ++ name ++ "'");
    }
    const Actual = @TypeOf(@field(Self, name));
    if (Actual != Expected) {
        @compileError(std.fmt.comptimePrint(
            "backend {s}.{s} has wrong signature:\n  got:  {s}\n  want: {s}",
            .{ @typeName(Self), name, @typeName(Actual), @typeName(Expected) },
        ));
    }
}

/// Comptime contract check verifying every required backend method signature (§3.3).
pub fn check(comptime Self: type) void {
    expectDecl(Self, "init", fn (Allocator, *const abi.DeviceOptions, *abi.DriverCaps, *Diag) E!*Self);
    expectDecl(Self, "deinit", fn (*Self) void);
    expectDecl(Self, "beginBatch", fn (*Self, *Diag) E!void);
    expectDecl(Self, "endBatch", fn (*Self, abi.FlushMode, *abi.SubmitResult, *Diag) E!void);
    expectDecl(Self, "setVertices", fn (*Self, []const f32, []const u32, *Diag) E!void);
    expectDecl(Self, "newImage", fn (*Self, u32, u32, u32, u32, u32, abi.PixelFormat, *Diag) E!void);
    expectDecl(Self, "newScreenImage", fn (*Self, u32, u32, u32, u32, *Diag) E!void);
    expectDecl(Self, "disposeImage", fn (*Self, u32) E!void);
    expectDecl(Self, "disposeShader", fn (*Self, u32) E!void);
    expectDecl(Self, "disposeSurface", fn (*Self, u32) E!void);
    expectDecl(Self, "writePixels", fn (*Self, u32, []align(4) const abi.PixelsRegion, []const u8, *Diag) E!void);
    expectDecl(Self, "drawTriangles", fn (*Self, *const replayer.DrawCall, *Diag) E!void);
    expectDecl(Self, "setDamage", fn (*Self, u32, ?[]align(4) const abi.Rect) E!void);
    expectDecl(Self, "createShader", fn (*Self, u32, *const abi.ShaderDesc, *Diag) E!void);
    expectDecl(Self, "readPixels", fn (*Self, u32, []const abi.ReadRegion, *Diag) E!void);
    expectDecl(Self, "finishForcedFrame", fn (*Self, *Diag) E!void);
    expectDecl(Self, "presentOnMainThread", fn (*Self, *Diag) E!void);
    expectDecl(Self, "createSurface", fn (*Self, u32, *const abi.SurfaceDesc, *abi.SurfaceCaps, *Diag) E!void);
    expectDecl(Self, "setWindowState", fn (*Self, u32, u32) void);
    expectDecl(Self, "surfaceKept", fn (*Self, u32) bool);
    expectDecl(Self, "setMainThreadMode", fn (*Self, u32) void);
    expectDecl(Self, "isOccluded", fn (*Self) bool);
    expectDecl(Self, "stats", fn (*Self, *abi.DebugStats) void);
}

comptime {
    check(Impl);
}
