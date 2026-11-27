const std = @import("std");
const Allocator = std.mem.Allocator;
const errors = @import("errors.zig");
const Error = errors.Error;

/// Shared vertex-stage cache keyed by exact (code, entry, format) bytes.
/// Uses Wyhash for fast lookup and compares full byte slices on a hit so a
/// hash collision can never return a mismatched vertex stage.
pub fn VertexStageCache(comptime Value: type) type {
    return struct {
        const Self = @This();

        pub const Key = struct {
            code: []const u8,
            entry: []const u8,
            format: u32 = 0,
        };

        const KeyContext = struct {
            pub fn hash(_: KeyContext, k: Key) u64 {
                var h = std.hash.Wyhash.init(0);
                h.update(std.mem.asBytes(&k.format));
                const code_len: u64 = k.code.len;
                h.update(std.mem.asBytes(&code_len));
                h.update(k.code);
                h.update(k.entry);
                return h.final();
            }

            pub fn eql(_: KeyContext, a: Key, b: Key) bool {
                return a.format == b.format and
                    std.mem.eql(u8, a.code, b.code) and
                    std.mem.eql(u8, a.entry, b.entry);
            }
        };

        map: std.HashMapUnmanaged(Key, Value, KeyContext, std.hash_map.default_max_load_percentage) = .empty,

        pub fn get(self: *const Self, code: []const u8, entry: []const u8, format: u32) ?Value {
            return self.map.get(.{ .code = code, .entry = entry, .format = format });
        }

        pub fn getPtr(self: *Self, code: []const u8, entry: []const u8, format: u32) ?*Value {
            return self.map.getPtr(.{ .code = code, .entry = entry, .format = format });
        }

        /// Inserts `value` under an owned copy of `code` and `entry`.
        /// Caller must only call `put` when `get` returned null.
        pub fn put(self: *Self, gpa: Allocator, code: []const u8, entry: []const u8, format: u32, value: Value) Error!void {
            const owned_code = gpa.dupe(u8, code) catch return error.OutOfMemory;
            errdefer gpa.free(owned_code);

            const owned_entry = if (entry.len > 0)
                gpa.dupe(u8, entry) catch return error.OutOfMemory
            else
                &.{};
            errdefer if (owned_entry.len > 0) gpa.free(owned_entry);

            const key = Key{
                .code = owned_code,
                .entry = owned_entry,
                .format = format,
            };
            self.map.putNoClobber(gpa, key, value) catch return error.OutOfMemory;
        }

        pub fn deinit(self: *Self, gpa: Allocator, ctx: anytype, comptime destroyFn: fn (@TypeOf(ctx), *Value) void) void {
            var it = self.map.iterator();
            while (it.next()) |kv| {
                destroyFn(ctx, kv.value_ptr);
                if (kv.key_ptr.code.len > 0) gpa.free(kv.key_ptr.code);
                if (kv.key_ptr.entry.len > 0) gpa.free(kv.key_ptr.entry);
            }
            self.map.deinit(gpa);
        }
    };
}

test "VertexStageCache compares full bytes and frees keys cleanly" {
    const gpa = std.testing.allocator;
    const DestroyCtx = struct { count: *u32 };
    const Helpers = struct {
        fn destroy(ctx: DestroyCtx, _: *u32) void {
            ctx.count.* += 1;
        }
    };

    var destroyed: u32 = 0;
    var cache: VertexStageCache(u32) = .{};
    defer cache.deinit(gpa, DestroyCtx{ .count = &destroyed }, Helpers.destroy);

    try std.testing.expectEqual(@as(?u32, null), cache.get("vertex_a", "main", 1));
    try cache.put(gpa, "vertex_a", "main", 1, 42);
    try cache.put(gpa, "vertex_b", "main", 1, 99);
    try cache.put(gpa, "vertex_a", "alt", 1, 100);
    try cache.put(gpa, "vertex_a", "main", 2, 101);

    try std.testing.expectEqual(@as(?u32, 42), cache.get("vertex_a", "main", 1));
    try std.testing.expectEqual(@as(?u32, 99), cache.get("vertex_b", "main", 1));
    try std.testing.expectEqual(@as(?u32, 100), cache.get("vertex_a", "alt", 1));
    try std.testing.expectEqual(@as(?u32, 101), cache.get("vertex_a", "main", 2));
    try std.testing.expectEqual(@as(?u32, null), cache.get("vertex_c", "main", 1));

    // Verify full key + value cleanup via explicit deinit before testing.allocator check.
    cache.deinit(gpa, DestroyCtx{ .count = &destroyed }, Helpers.destroy);
    cache = .{};
    try std.testing.expectEqual(@as(u32, 4), destroyed);
}
