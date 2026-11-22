const std = @import("std");
const errors = @import("errors.zig");
const Allocator = std.mem.Allocator;
const Error = errors.Error;

/// Dense handle table indexed directly by u32 handle ID (id 0 is reserved as none).
pub fn HandleTable(comptime T: type) type {
    return struct {
        const Self = @This();

        items: std.ArrayList(?T) = .empty,
        live: u32 = 0,

        pub fn put(self: *Self, gpa: Allocator, id: u32, v: T) Error!void {
            if (id == 0) return error.InvalidArgument;
            const idx: usize = id;
            if (idx < self.items.items.len) {
                if (self.items.items[idx] != null) return error.DuplicateHandle;
                self.items.items[idx] = v;
                self.live += 1;
                return;
            }
            const old_len = self.items.items.len;
            const new_len = std.math.add(usize, idx, 1) catch return error.OutOfMemory;
            self.items.resize(gpa, new_len) catch return error.OutOfMemory;
            @memset(self.items.items[old_len..new_len], null);
            self.items.items[idx] = v;
            self.live += 1;
        }

        pub fn get(self: *Self, id: u32) Error!*T {
            if (id == 0) return error.UnknownHandle;
            const idx: usize = id;
            if (idx >= self.items.items.len) return error.UnknownHandle;
            if (self.items.items[idx]) |*slot| {
                return slot;
            }
            return error.UnknownHandle;
        }

        pub fn getOpt(self: *Self, id: u32) ?*T {
            if (id == 0) return null;
            const idx: usize = id;
            if (idx >= self.items.items.len) return null;
            if (self.items.items[idx]) |*slot| {
                return slot;
            }
            return null;
        }

        pub fn remove(self: *Self, id: u32) Error!T {
            if (id == 0) return error.UnknownHandle;
            const idx: usize = id;
            if (idx >= self.items.items.len) return error.UnknownHandle;
            if (self.items.items[idx]) |val| {
                self.items.items[idx] = null;
                self.live -= 1;
                return val;
            }
            return error.UnknownHandle;
        }

        pub fn deinit(self: *Self, gpa: Allocator) void {
            self.items.deinit(gpa);
            self.live = 0;
        }
    };
}
