const std = @import("std");
pub const abi = @import("abi.zig");
pub const errors = @import("errors.zig");
pub const alloc = @import("alloc.zig");
pub const sync = @import("sync.zig");
pub const time = @import("time.zig");
pub const dynlib = @import("dynlib.zig");
pub const handles = @import("handles.zig");
pub const state_cache = @import("state_cache.zig");
pub const upload_arena = @import("upload_arena.zig");
pub const pixel_regions = @import("pixel_regions.zig");
pub const vertex_cache = @import("vertex_cache.zig");
pub const replayer = @import("replayer.zig");
pub const backend = @import("backend.zig");
pub const null_backend = @import("null/backend.zig");

const Diag = errors.Diag;
const Error = errors.Error;

pub const Device = struct {
    alloc_state: alloc.AllocatorState,
    impl: *backend.Impl,
    lost: std.atomic.Value(bool) = .init(false),
    vertices_valid: bool = false,

    pub fn isLost(self: *const Device) bool {
        return self.lost.load(.acquire);
    }

    pub fn markLost(self: *Device) void {
        self.lost.store(true, .release);
    }
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

/// Replays one batch between beginBatch and endBatch, aborting on replay/end failure and
/// latching the terminal device-lost state only when the API reports DeviceLost or the
/// backend signals an unrecoverable execution/WSI state.
fn runBatch(
    dev: *Device,
    batch: *const abi.Batch,
    mode: abi.FlushMode,
    out_res: *abi.SubmitResult,
    diag: *Diag,
) Error!void {
    dev.impl.beginBatch(diag) catch |err| {
        if (err == error.DeviceLost or dev.impl.isDeviceLost()) dev.markLost();
        return err;
    };
    replayer.replayWithState(dev.impl, batch, &dev.vertices_valid, diag) catch |err| {
        dev.vertices_valid = false;
        if (dev.impl.abortBatch() or err == error.DeviceLost or dev.impl.isDeviceLost()) {
            dev.markLost();
        }
        return err;
    };
    dev.impl.endBatch(mode, out_res, diag) catch |err| {
        dev.vertices_valid = false;
        if (dev.impl.abortBatch() or err == error.DeviceLost or dev.impl.isDeviceLost()) {
            dev.markLost();
        }
        return err;
    };
    if (mode != .intermediate) {
        dev.vertices_valid = false;
    }
}

fn checkNotLost(dev: *const Device, diag: *Diag) bool {
    if (dev.isLost()) {
        diag.set("device is lost");
        return false;
    }
    return true;
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
    const opts = opts_opt orelse {
        diag.set("opts is null");
        return .err_invalid_argument;
    };
    const out_dev = out_dev_opt orelse {
        diag.set("out_dev is null");
        return .err_invalid_argument;
    };
    const out_caps = out_caps_opt orelse {
        diag.set("out_caps is null");
        return .err_invalid_argument;
    };

    // Validate the caller's ABI version before inspecting full struct size or writing any output
    // struct so a mismatched caller's memory is never touched.
    if (opts.struct_size < @offsetOf(abi.DeviceOptions, "abi_version") + @sizeOf(u32)) {
        diag.print("DeviceOptions struct_size too small: got {d}, want {d}", .{
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
    if (opts.struct_size != @sizeOf(abi.DeviceOptions)) {
        diag.print("DeviceOptions struct_size mismatch: got {d}, want {d}", .{
            opts.struct_size,
            @sizeOf(abi.DeviceOptions),
        });
        return .err_invalid_argument;
    }

    out_dev.* = null;

    const dev = std.heap.page_allocator.create(Device) catch {
        diag.set("out of memory allocating Device");
        return .err_out_of_memory;
    };
    dev.alloc_state = .init;
    dev.lost = .init(false);
    dev.vertices_valid = false;

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
    if (dev.isLost()) return;
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
    if (!checkNotLost(dev, &diag)) return .err_device_lost;
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

    runBatch(dev, batch, flush_mode, out_res, &diag) catch |err| return errors.toStatus(err);
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
    if (!checkNotLost(dev, &diag)) return .err_device_lost;
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

    dev.impl.createShader(shader_id, desc, &diag) catch |err| {
        if (err == error.DeviceLost or dev.impl.isDeviceLost()) dev.markLost();
        return errors.toStatus(err);
    };
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
    if (!checkNotLost(dev, &diag)) return .err_device_lost;
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
        runBatch(dev, pending, .intermediate, &dummy_res, &diag) catch |err| return errors.toStatus(err);
    }

    dev.impl.readPixels(image_id, regions, &diag) catch |err| {
        if (dev.impl.abortBatch() or err == error.DeviceLost or dev.impl.isDeviceLost()) {
            dev.markLost();
        }
        return errors.toStatus(err);
    };
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
    if (!checkNotLost(dev, &diag)) return .err_device_lost;
    dev.impl.finishForcedFrame(&diag) catch |err| {
        if (err == error.DeviceLost or dev.impl.isDeviceLost()) dev.markLost();
        return errors.toStatus(err);
    };
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
    if (!checkNotLost(dev, &diag)) return .err_device_lost;
    dev.impl.presentOnMainThread(&diag) catch |err| {
        if (err == error.DeviceLost or dev.impl.isDeviceLost()) dev.markLost();
        return errors.toStatus(err);
    };
    return .ok;
}

pub export fn ggfx_device_is_occluded(dev_opt: ?*Device) callconv(.c) u32 {
    const dev = dev_opt orelse return 0;
    if (dev.isLost()) return 0;
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
    if (!checkNotLost(dev, &diag)) return .err_device_lost;
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
    dev.impl.createSurface(surface_id, desc, out_caps, &diag) catch |err| {
        if (err == error.DeviceLost or dev.impl.isDeviceLost()) dev.markLost();
        return errors.toStatus(err);
    };
    return .ok;
}

pub export fn ggfx_surface_set_window_state(
    dev_opt: ?*Device,
    surface_id: u32,
    state_flags: u32,
) callconv(.c) void {
    const dev = dev_opt orelse return;
    if (dev.isLost()) return;
    dev.impl.setWindowState(surface_id, state_flags);
}

pub export fn ggfx_surface_kept(
    dev_opt: ?*Device,
    surface_id: u32,
) callconv(.c) u32 {
    const dev = dev_opt orelse return 0;
    if (dev.isLost()) return 0;
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

pub export fn ggfx_acquire_upload_arena(
    dev_opt: ?*Device,
    min_bytes: u64,
    out_arena_opt: ?*abi.UploadArena,
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
    if (!checkNotLost(dev, &diag)) return .err_device_lost;
    const out_arena = out_arena_opt orelse {
        diag.set("out_arena is null");
        return .err_invalid_argument;
    };
    out_arena.* = std.mem.zeroes(abi.UploadArena);

    dev.impl.acquireUploadArena(min_bytes, out_arena, &diag) catch |err| {
        if (err == error.DeviceLost or dev.impl.isDeviceLost()) dev.markLost();
        return errors.toStatus(err);
    };
    return .ok;
}

test "device_create rejects ABI version mismatch without writing output structs" {
    const opts = abi.DeviceOptions{
        .struct_size = @sizeOf(abi.DeviceOptions),
        .abi_version = abi.driver_abi_version - 1,
        .backend = @intFromEnum(abi.Backend.null),
        .color_space = 1,
        .flags = 0,
        .frames_in_flight = 2,
        .dx_feature_level = 0,
        .main_thread_mode = 0,
        .reserved = .{ 0, 0 },
    };
    const sentinel_ptr: *Device = @ptrFromInt(0x12340);
    var out_dev: ?*Device = sentinel_ptr;
    var out_caps: abi.DriverCaps = undefined;
    @memset(std.mem.asBytes(&out_caps), 0xA5);
    var err_buf: [256]u8 = @splat(0);

    const st = ggfx_device_create(&opts, &out_dev, &out_caps, &err_buf, err_buf.len);
    try std.testing.expectEqual(abi.Status.err_abi_mismatch, st);
    try std.testing.expectEqual(@as(?*Device, sentinel_ptr), out_dev);
    for (std.mem.asBytes(&out_caps)) |b| {
        try std.testing.expectEqual(@as(u8, 0xA5), b);
    }
}

test "terminal device-lost state rejects all subsequent operations until destroy" {
    if (backend.Impl != null_backend.Backend) return;

    const opts = abi.DeviceOptions{
        .struct_size = @sizeOf(abi.DeviceOptions),
        .abi_version = abi.driver_abi_version,
        .backend = @intFromEnum(abi.Backend.null),
        .color_space = 1,
        .flags = 0,
        .frames_in_flight = 2,
        .dx_feature_level = 0,
        .main_thread_mode = 0,
        .reserved = .{ 0, 0 },
    };
    var dev: ?*Device = null;
    var caps: abi.DriverCaps = undefined;
    var err_buf: [256]u8 = @splat(0);
    try std.testing.expectEqual(abi.Status.ok, ggfx_device_create(&opts, &dev, &caps, &err_buf, err_buf.len));
    errdefer _ = ggfx_device_destroy(dev, &err_buf, err_buf.len);

    // A non-terminal surface condition during endBatch does not latch device-lost.
    dev.?.impl.fail_next_end_batch = .{ .err = error.SurfaceLost, .mark_lost = false };
    const empty_batch = abi.Batch{
        .struct_size = @sizeOf(abi.Batch),
        .cmd_count = 0,
        .cmds = null,
        .cmds_len = 0,
        .payload = null,
        .payload_len = 0,
        .sequence = 1,
    };
    var res: abi.SubmitResult = undefined;
    try std.testing.expectEqual(
        abi.Status.err_surface_lost,
        ggfx_submit(dev, &empty_batch, @intFromEnum(abi.FlushMode.present), &res, &err_buf, err_buf.len),
    );
    try std.testing.expect(!dev.?.isLost());
    try std.testing.expectEqual(
        abi.Status.ok,
        ggfx_submit(dev, &empty_batch, @intFromEnum(abi.FlushMode.end_frame), &res, &err_buf, err_buf.len),
    );

    // An uncertain submission failure latches terminal device-lost state so every subsequent
    // status-returning call reports err_device_lost while stats and destroy still succeed.
    dev.?.impl.fail_next_end_batch = .{ .err = error.OutOfMemory, .mark_lost = true };
    try std.testing.expectEqual(
        abi.Status.err_out_of_memory,
        ggfx_submit(dev, &empty_batch, @intFromEnum(abi.FlushMode.end_frame), &res, &err_buf, err_buf.len),
    );
    try std.testing.expect(dev.?.isLost());

    // Every subsequent API entry point must report err_device_lost without mutating outputs.
    try std.testing.expectEqual(
        abi.Status.err_device_lost,
        ggfx_submit(dev, &empty_batch, @intFromEnum(abi.FlushMode.end_frame), &res, &err_buf, err_buf.len),
    );
    const shader_desc = std.mem.zeroes(abi.ShaderDesc);
    try std.testing.expectEqual(
        abi.Status.err_device_lost,
        ggfx_create_shader(dev, 1, &shader_desc, &err_buf, err_buf.len),
    );
    try std.testing.expectEqual(
        abi.Status.err_device_lost,
        ggfx_read_pixels(dev, null, 1, null, 0, &err_buf, err_buf.len),
    );
    try std.testing.expectEqual(
        abi.Status.err_device_lost,
        ggfx_finish_forced_frame(dev, &err_buf, err_buf.len),
    );
    try std.testing.expectEqual(
        abi.Status.err_device_lost,
        ggfx_present_on_main_thread(dev, &err_buf, err_buf.len),
    );
    const surf_desc = std.mem.zeroes(abi.SurfaceDesc);
    var surf_caps: abi.SurfaceCaps = undefined;
    try std.testing.expectEqual(
        abi.Status.err_device_lost,
        ggfx_surface_create(dev, 1, &surf_desc, &surf_caps, &err_buf, err_buf.len),
    );
    var arena: abi.UploadArena = undefined;
    @memset(std.mem.asBytes(&arena), 0x5A);
    try std.testing.expectEqual(
        abi.Status.err_device_lost,
        ggfx_acquire_upload_arena(dev, 4096, &arena, &err_buf, err_buf.len),
    );
    for (std.mem.asBytes(&arena)) |b| {
        try std.testing.expectEqual(@as(u8, 0x5A), b);
    }
    try std.testing.expectEqual(abi.Status.ok, ggfx_device_destroy(dev, &err_buf, err_buf.len));
}

test {
    _ = abi;
    _ = errors;
    _ = sync;
    _ = time;
    _ = dynlib;
    _ = state_cache;
    _ = upload_arena;
    _ = pixel_regions;
    _ = vertex_cache;
    _ = null_backend;
}
