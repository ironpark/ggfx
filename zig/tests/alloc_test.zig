const std = @import("std");
const ggfx = @import("ggfx");
const alloc = ggfx.alloc;
const abi = ggfx.abi;

test "AllocatorState clean lifecycle reports ok" {
    var state: alloc.AllocatorState = .init;
    const gpa = state.allocator();

    const buf = try gpa.alloc(u8, 256);
    @memset(buf, 0xAB);
    gpa.free(buf);

    try std.testing.expectEqual(alloc.CheckResult.ok, state.deinit());
}

test "AllocatorState tracks active allocations in Debug/ReleaseSafe" {
    if (!alloc.uses_debug_allocator) return;

    var state: alloc.AllocatorState = .init;
    const gpa = state.allocator();

    // Large allocation is tracked in DebugAllocator.large_allocations until freed.
    const big = try gpa.alloc(u8, 64 * 1024);
    try std.testing.expectEqual(@as(usize, 1), state.inner.large_allocations.count());
    gpa.free(big);
    try std.testing.expectEqual(@as(usize, 0), state.inner.large_allocations.count());

    try std.testing.expectEqual(alloc.CheckResult.ok, state.deinit());
}

test "C ABI device create/destroy lifecycle has zero leaks" {
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
    var dev: ?*ggfx.Device = null;
    var caps: abi.DriverCaps = undefined;
    var err_buf: [256]u8 = undefined;

    try std.testing.expectEqual(@as(u32, 2), ggfx.ggfx_abi_version());
    try std.testing.expectEqual(
        abi.Status.ok,
        ggfx.ggfx_device_create(&opts, &dev, &caps, &err_buf, err_buf.len),
    );
    try std.testing.expect(dev != null);
    try std.testing.expectEqual(@as(u32, @sizeOf(abi.DriverCaps)), caps.struct_size);

    var stats: abi.DebugStats = undefined;
    try std.testing.expectEqual(abi.Status.ok, ggfx.ggfx_debug_stats(dev, &stats));
    try std.testing.expectEqual(@as(u64, 0), stats.live_images);

    try std.testing.expectEqual(
        abi.Status.ok,
        ggfx.ggfx_device_destroy(dev, &err_buf, err_buf.len),
    );
}

test "C ABI device create rejects ABI mismatch" {
    const opts = abi.DeviceOptions{
        .struct_size = @sizeOf(abi.DeviceOptions),
        .abi_version = 999,
        .backend = @intFromEnum(abi.Backend.null),
        .color_space = 0,
        .flags = 0,
        .frames_in_flight = 2,
        .dx_feature_level = 0,
        .main_thread_mode = 0,
        .reserved = .{ 0, 0 },
    };
    var dev: ?*ggfx.Device = null;
    var caps: abi.DriverCaps = undefined;
    var err_buf: [256]u8 = undefined;

    try std.testing.expectEqual(
        abi.Status.err_abi_mismatch,
        ggfx.ggfx_device_create(&opts, &dev, &caps, &err_buf, err_buf.len),
    );
    try std.testing.expect(dev == null);
}
