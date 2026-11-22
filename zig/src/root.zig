const std = @import("std");
pub const abi = @import("abi.zig");
pub const errors = @import("errors.zig");
pub const alloc = @import("alloc.zig");
pub const sync = @import("sync.zig");
pub const time = @import("time.zig");
pub const dynlib = @import("dynlib.zig");
pub const handles = @import("handles.zig");
pub const state_cache = @import("state_cache.zig");
pub const replayer = @import("replayer.zig");
pub const backend = @import("backend.zig");
pub const null_backend = @import("null/backend.zig");

const Diag = errors.Diag;
const Error = errors.Error;

pub const Device = struct {
    alloc_state: alloc.AllocatorState,
    impl: *backend.Impl,
};

inline fn enterAutoreleasePool() ?*anyopaque {
    if (@hasDecl(backend.Impl, "enterAutoreleasePool")) {
        return backend.Impl.enterAutoreleasePool();
    }
    return null;
}

inline fn leaveAutoreleasePool(pool: ?*anyopaque) void {
    if (@hasDecl(backend.Impl, "leaveAutoreleasePool")) {
        backend.Impl.leaveAutoreleasePool(pool);
    }
}

inline fn abortBatch(impl: *backend.Impl) void {
    if (@hasDecl(backend.Impl, "abortBatch")) {
        impl.abortBatch();
    }
}

fn parseFlushMode(mode: u32) Error!abi.FlushMode {
    return switch (mode) {
        @intFromEnum(abi.FlushMode.intermediate) => .intermediate,
        @intFromEnum(abi.FlushMode.end_frame) => .end_frame,
        @intFromEnum(abi.FlushMode.present) => .present,
        else => error.InvalidArgument,
    };
}

pub export fn ggfx_abi_version() callconv(.c) u32 {
    return abi.driver_abi_version;
}

pub export fn ggfx_device_create(
    opts_opt: ?*const abi.DeviceOptions,
    out_dev_opt: ?*?*Device,
    out_caps_opt: ?*abi.DriverCaps,
    err_buf: ?[*]u8,
    err_cap: usize,
) callconv(.c) abi.Status {
    const pool = enterAutoreleasePool();
    defer leaveAutoreleasePool(pool);

    var diag = Diag.init(err_buf, err_cap);
    const out_dev = out_dev_opt orelse {
        diag.set("out_dev is null");
        return .err_invalid_argument;
    };
    out_dev.* = null;

    const opts = opts_opt orelse {
        diag.set("opts is null");
        return .err_invalid_argument;
    };
    const out_caps = out_caps_opt orelse {
        diag.set("out_caps is null");
        return .err_invalid_argument;
    };

    if (opts.struct_size != @sizeOf(abi.DeviceOptions)) {
        diag.print("DeviceOptions struct_size mismatch: got {d}, want {d}", .{
            opts.struct_size,
            @sizeOf(abi.DeviceOptions),
        });
        return .err_invalid_argument;
    }
    if (opts.abi_version != abi.driver_abi_version) {
        diag.print("ABI version mismatch: got {d}, want {d}", .{
            opts.abi_version,
            abi.driver_abi_version,
        });
        return .err_abi_mismatch;
    }

    const dev = std.heap.page_allocator.create(Device) catch {
        diag.set("out of memory allocating Device");
        return .err_out_of_memory;
    };
    dev.alloc_state = .init;

    out_caps.* = std.mem.zeroes(abi.DriverCaps);
    const impl = backend.Impl.init(dev.alloc_state.allocator(), opts, out_caps, &diag) catch |err| {
        _ = dev.alloc_state.deinit();
        std.heap.page_allocator.destroy(dev);
        return errors.toStatus(err);
    };
    dev.impl = impl;
    out_dev.* = dev;
    return .ok;
}

pub export fn ggfx_device_destroy(
    dev_opt: ?*Device,
    err_buf: ?[*]u8,
    err_cap: usize,
) callconv(.c) abi.Status {
    const pool = enterAutoreleasePool();
    defer leaveAutoreleasePool(pool);

    var diag = Diag.init(err_buf, err_cap);
    const dev = dev_opt orelse {
        diag.set("dev is null");
        return .err_invalid_argument;
    };

    dev.impl.deinit();
    const check = dev.alloc_state.deinit();
    std.heap.page_allocator.destroy(dev);

    if (check == .leak) {
        diag.set("memory leak detected on device destroy");
        return .err_leak_detected;
    }
    return .ok;
}

pub export fn ggfx_device_set_main_thread_mode(
    dev_opt: ?*Device,
    mode: u32,
) callconv(.c) void {
    const dev = dev_opt orelse return;
    dev.impl.setMainThreadMode(mode);
}

pub export fn ggfx_submit(
    dev_opt: ?*Device,
    batch_opt: ?*const abi.Batch,
    flush_mode_raw: u32,
    out_res_opt: ?*abi.SubmitResult,
    err_buf: ?[*]u8,
    err_cap: usize,
) callconv(.c) abi.Status {
    const pool = enterAutoreleasePool();
    defer leaveAutoreleasePool(pool);

    var diag = Diag.init(err_buf, err_cap);
    const dev = dev_opt orelse {
        diag.set("dev is null");
        return .err_invalid_argument;
    };
    const batch = batch_opt orelse {
        diag.set("batch is null");
        return .err_invalid_argument;
    };
    const out_res = out_res_opt orelse {
        diag.set("out_res is null");
        return .err_invalid_argument;
    };
    out_res.* = std.mem.zeroes(abi.SubmitResult);

    const flush_mode = parseFlushMode(flush_mode_raw) catch {
        diag.print("invalid flush_mode: {d}", .{flush_mode_raw});
        return .err_invalid_argument;
    };

    dev.impl.beginBatch(&diag) catch |err| return errors.toStatus(err);
    replayer.replay(dev.impl, batch, &diag) catch |err| {
        abortBatch(dev.impl);
        return errors.toStatus(err);
    };
    dev.impl.endBatch(flush_mode, out_res, &diag) catch |err| return errors.toStatus(err);
    return .ok;
}

pub export fn ggfx_create_shader(
    dev_opt: ?*Device,
    shader_id: u32,
    desc_opt: ?*const abi.ShaderDesc,
    err_buf: ?[*]u8,
    err_cap: usize,
) callconv(.c) abi.Status {
    const pool = enterAutoreleasePool();
    defer leaveAutoreleasePool(pool);

    var diag = Diag.init(err_buf, err_cap);
    const dev = dev_opt orelse {
        diag.set("dev is null");
        return .err_invalid_argument;
    };
    const desc = desc_opt orelse {
        diag.set("desc is null");
        return .err_invalid_argument;
    };
    if (desc.struct_size != @sizeOf(abi.ShaderDesc)) {
        diag.print("ShaderDesc struct_size mismatch: got {d}, want {d}", .{
            desc.struct_size,
            @sizeOf(abi.ShaderDesc),
        });
        return .err_invalid_argument;
    }

    dev.impl.createShader(shader_id, desc, &diag) catch |err| return errors.toStatus(err);
    return .ok;
}

pub export fn ggfx_read_pixels(
    dev_opt: ?*Device,
    pending_opt: ?*const abi.Batch,
    image_id: u32,
    regions_opt: ?[*]const abi.ReadRegion,
    region_count: u32,
    err_buf: ?[*]u8,
    err_cap: usize,
) callconv(.c) abi.Status {
    const pool = enterAutoreleasePool();
    defer leaveAutoreleasePool(pool);

    var diag = Diag.init(err_buf, err_cap);
    const dev = dev_opt orelse {
        diag.set("dev is null");
        return .err_invalid_argument;
    };
    const regions: []const abi.ReadRegion = if (region_count == 0)
        &.{}
    else blk: {
        const ptr = regions_opt orelse {
            diag.set("regions is null with non-zero region_count");
            return .err_invalid_argument;
        };
        break :blk ptr[0..region_count];
    };

    if (pending_opt) |pending| {
        var dummy_res = std.mem.zeroes(abi.SubmitResult);
        dev.impl.beginBatch(&diag) catch |err| return errors.toStatus(err);
        replayer.replay(dev.impl, pending, &diag) catch |err| {
            abortBatch(dev.impl);
            return errors.toStatus(err);
        };
        dev.impl.endBatch(.intermediate, &dummy_res, &diag) catch |err| return errors.toStatus(err);
    }

    dev.impl.readPixels(image_id, regions, &diag) catch |err| return errors.toStatus(err);
    return .ok;
}

pub export fn ggfx_finish_forced_frame(
    dev_opt: ?*Device,
    err_buf: ?[*]u8,
    err_cap: usize,
) callconv(.c) abi.Status {
    const pool = enterAutoreleasePool();
    defer leaveAutoreleasePool(pool);

    var diag = Diag.init(err_buf, err_cap);
    const dev = dev_opt orelse {
        diag.set("dev is null");
        return .err_invalid_argument;
    };
    dev.impl.finishForcedFrame(&diag) catch |err| return errors.toStatus(err);
    return .ok;
}

pub export fn ggfx_present_on_main_thread(
    dev_opt: ?*Device,
    err_buf: ?[*]u8,
    err_cap: usize,
) callconv(.c) abi.Status {
    const pool = enterAutoreleasePool();
    defer leaveAutoreleasePool(pool);

    var diag = Diag.init(err_buf, err_cap);
    const dev = dev_opt orelse {
        diag.set("dev is null");
        return .err_invalid_argument;
    };
    dev.impl.presentOnMainThread(&diag) catch |err| return errors.toStatus(err);
    return .ok;
}

pub export fn ggfx_device_is_occluded(dev_opt: ?*Device) callconv(.c) u32 {
    const dev = dev_opt orelse return 0;
    return if (dev.impl.isOccluded()) 1 else 0;
}

pub export fn ggfx_surface_create(
    dev_opt: ?*Device,
    surface_id: u32,
    desc_opt: ?*const abi.SurfaceDesc,
    out_caps_opt: ?*abi.SurfaceCaps,
    err_buf: ?[*]u8,
    err_cap: usize,
) callconv(.c) abi.Status {
    const pool = enterAutoreleasePool();
    defer leaveAutoreleasePool(pool);

    var diag = Diag.init(err_buf, err_cap);
    const dev = dev_opt orelse {
        diag.set("dev is null");
        return .err_invalid_argument;
    };
    const desc = desc_opt orelse {
        diag.set("desc is null");
        return .err_invalid_argument;
    };
    const out_caps = out_caps_opt orelse {
        diag.set("out_caps is null");
        return .err_invalid_argument;
    };
    if (desc.struct_size != @sizeOf(abi.SurfaceDesc)) {
        diag.print("SurfaceDesc struct_size mismatch: got {d}, want {d}", .{
            desc.struct_size,
            @sizeOf(abi.SurfaceDesc),
        });
        return .err_invalid_argument;
    }

    out_caps.* = std.mem.zeroes(abi.SurfaceCaps);
    dev.impl.createSurface(surface_id, desc, out_caps, &diag) catch |err| return errors.toStatus(err);
    return .ok;
}

pub export fn ggfx_surface_set_window_state(
    dev_opt: ?*Device,
    surface_id: u32,
    state_flags: u32,
) callconv(.c) void {
    const dev = dev_opt orelse return;
    dev.impl.setWindowState(surface_id, state_flags);
}

pub export fn ggfx_surface_kept(
    dev_opt: ?*Device,
    surface_id: u32,
) callconv(.c) u32 {
    const dev = dev_opt orelse return 0;
    return if (dev.impl.surfaceKept(surface_id)) 1 else 0;
}

pub export fn ggfx_debug_stats(
    dev_opt: ?*Device,
    out_opt: ?*abi.DebugStats,
) callconv(.c) abi.Status {
    const dev = dev_opt orelse return .err_invalid_argument;
    const out = out_opt orelse return .err_invalid_argument;
    out.* = std.mem.zeroes(abi.DebugStats);
    dev.impl.stats(out);
    return .ok;
}

test {
    _ = abi;
    _ = errors;
    _ = sync;
    _ = time;
    _ = dynlib;
    _ = null_backend;
}
