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

/// Returns whether any non-zero address in `addrs` falls within `[base, base + size)`.
pub fn rangeContainsAny(base: u64, size: u64, addrs: []const u64) bool {
    const end = base +| size;
    for (addrs) |addr| {
        if (addr != 0 and addr >= base and addr < end) {
            return true;
        }
    }
    return false;
}

/// Advances `id` to the next arena id. 0 is skipped: it means "no arena".
pub fn nextId(id: *u32) u32 {
    id.* +%= 1;
    if (id.* == 0) id.* = 1;
    return id.*;
}

/// Bounded pool of idle GPU-mapped shared upload buffers kept for reuse so
/// arena rotation does not reallocate and map a new buffer every time.
/// Entries that do not fit within the count or byte caps are destroyed.
pub fn Pool(
    comptime Entry: type,
    comptime Ctx: type,
    comptime sizeFn: fn (Entry) u64,
    comptime destroyFn: fn (Ctx, Entry) void,
) type {
    return struct {
        const Self = @This();

        pub const max_idle = 4;
        pub const max_idle_bytes: u64 = 64 << 20;

        idle: [max_idle]Entry = undefined,
        len: usize = 0,
        bytes: u64 = 0,

        /// Removes and returns the smallest idle entry holding at least `min_size` bytes.
        pub fn take(self: *Self, min_size: u64) ?Entry {
            var best: ?usize = null;
            for (self.idle[0..self.len], 0..) |entry, i| {
                const sz = sizeFn(entry);
                if (sz < min_size) continue;
                if (best == null or sz < sizeFn(self.idle[best.?])) best = i;
            }
            const i = best orelse return null;
            const entry = self.idle[i];
            self.len -= 1;
            self.idle[i] = self.idle[self.len];
            self.bytes -= sizeFn(entry);
            return entry;
        }

        /// Keeps `entry` for reuse, or destroys it if the pool is full.
        /// The GPU must no longer be reading `entry`.
        pub fn put(self: *Self, ctx: Ctx, entry: Entry) void {
            const sz = sizeFn(entry);
            if (self.len == max_idle or sz > max_idle_bytes - self.bytes) {
                destroyFn(ctx, entry);
                return;
            }
            self.idle[self.len] = entry;
            self.len += 1;
            self.bytes += sz;
        }

        pub fn deinit(self: *Self, ctx: Ctx) void {
            for (self.idle[0..self.len]) |entry| {
                destroyFn(ctx, entry);
            }
            self.len = 0;
            self.bytes = 0;
        }
    };
}

test "Pool best-fit take, caps, and deinit" {
    const std = @import("std");
    const FakeEntry = struct { id: u32, size: u64 };
    const Ctx = struct { destroyed: *u32 };
    const Helpers = struct {
        fn sizeOf(e: FakeEntry) u64 {
            return e.size;
        }
        fn destroy(ctx: Ctx, _: FakeEntry) void {
            ctx.destroyed.* += 1;
        }
    };

    var destroyed: u32 = 0;
    const ctx = Ctx{ .destroyed = &destroyed };
    var pool: Pool(FakeEntry, Ctx, Helpers.sizeOf, Helpers.destroy) = .{};

    pool.put(ctx, .{ .id = 1, .size = 8 << 20 });
    pool.put(ctx, .{ .id = 2, .size = 4 << 20 });
    pool.put(ctx, .{ .id = 3, .size = 16 << 20 });
    try std.testing.expectEqual(@as(usize, 3), pool.len);

    // Best-fit picks the 8 MiB entry for a 6 MiB request.
    const got = pool.take(6 << 20).?;
    try std.testing.expectEqual(@as(u32, 1), got.id);
    try std.testing.expectEqual(@as(usize, 2), pool.len);

    // Oversized entry exceeding max_idle_bytes is destroyed immediately.
    pool.put(ctx, .{ .id = 4, .size = 64 << 20 });
    try std.testing.expectEqual(@as(u32, 1), destroyed);

    // Fill to max_idle (4), then 5th entry is destroyed.
    pool.put(ctx, .{ .id = 5, .size = 1 << 20 });
    pool.put(ctx, .{ .id = 6, .size = 1 << 20 });
    pool.put(ctx, .{ .id = 7, .size = 1 << 20 });
    try std.testing.expectEqual(@as(usize, 4), pool.len);
    try std.testing.expectEqual(@as(u32, 2), destroyed);

    pool.deinit(ctx);
    try std.testing.expectEqual(@as(usize, 0), pool.len);
    try std.testing.expectEqual(@as(u64, 0), pool.bytes);
    try std.testing.expectEqual(@as(u32, 6), destroyed);
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

test "rangeContainsAny detects live GPU addresses inside retired upload buffers" {
    const std = @import("std");
    try std.testing.expect(!rangeContainsAny(0x1000, 0x400, &.{ 0, 0x0FFF, 0x1400, 0x2000 }));
    try std.testing.expect(rangeContainsAny(0x1000, 0x400, &.{ 0, 0x1000 }));
    try std.testing.expect(rangeContainsAny(0x1000, 0x400, &.{ 0x2000, 0x13FF }));
    try std.testing.expect(!rangeContainsAny(0x1000, 0, &.{0x1000}));
}
