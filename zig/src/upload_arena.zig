//! Bookkeeping every backend's shared upload arena has in common. Allocating,
//! mapping, and retiring the arena's buffer stays in the backend.

/// Capacity handed out when the caller asks for less.
pub const default_size: u64 = 4 << 20; // 4 MiB
/// Largest arena a backend will allocate.
pub const max_size: u64 = 256 << 20; // 256 MiB

/// Offset of `slice` inside the mapping `[base, base + cap)`, or null when any
/// part of it lies outside.
pub fn offsetWithin(base: ?[*]const u8, cap: usize, slice: []const u8) ?usize {
    const base_addr = @intFromPtr(base orelse return null);
    const start = @intFromPtr(slice.ptr);
    if (start < base_addr) return null;
    const off = start - base_addr;
    if (off > cap or slice.len > cap - off) return null;
    return off;
}

/// Advances `id` to the next arena id. 0 is skipped: it means "no arena".
pub fn nextId(id: *u32) u32 {
    id.* +%= 1;
    if (id.* == 0) id.* = 1;
    return id.*;
}

test "offsetWithin accepts slices inside the mapping, including its end" {
    const std = @import("std");
    var buf: [64]u8 = undefined;
    const base: [*]const u8 = &buf;

    try std.testing.expectEqual(@as(?usize, 0), offsetWithin(base, buf.len, buf[0..64]));
    try std.testing.expectEqual(@as(?usize, 16), offsetWithin(base, buf.len, buf[16..32]));
    try std.testing.expectEqual(@as(?usize, 64), offsetWithin(base, buf.len, buf[64..64]));
    try std.testing.expectEqual(@as(?usize, null), offsetWithin(base, 32, buf[16..48]));
    try std.testing.expectEqual(@as(?usize, null), offsetWithin(base + 8, 56, buf[0..8]));
    try std.testing.expectEqual(@as(?usize, null), offsetWithin(null, buf.len, buf[0..8]));
}

test "nextId skips zero on wrap" {
    const std = @import("std");
    var id: u32 = 0;
    try std.testing.expectEqual(@as(u32, 1), nextId(&id));
    id = 0xFFFF_FFFF;
    try std.testing.expectEqual(@as(u32, 1), nextId(&id));
}
