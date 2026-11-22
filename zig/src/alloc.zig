const std = @import("std");
const builtin = @import("builtin");

pub const uses_debug_allocator = builtin.mode == .debug or builtin.mode == .safe;

pub const CheckResult = enum {
    ok,
    leak,
};

pub const DebugAllocatorType = std.heap.DebugAllocator(.{
    .stack_trace_frames = 8,
});

/// Per-device allocator state. Must stay pinned in memory while in use.
pub const AllocatorState = struct {
    inner: if (uses_debug_allocator) DebugAllocatorType else void =
        if (uses_debug_allocator) .init else {},

    pub const init: AllocatorState = .{};

    pub fn allocator(self: *AllocatorState) std.mem.Allocator {
        if (uses_debug_allocator) {
            return self.inner.allocator();
        }
        return std.heap.smp_allocator;
    }

    pub fn deinit(self: *AllocatorState) CheckResult {
        if (uses_debug_allocator) {
            return switch (self.inner.deinit()) {
                .ok => .ok,
                .leak => .leak,
            };
        }
        return .ok;
    }
};
