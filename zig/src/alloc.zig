const std = @import("std");
const builtin = @import("builtin");
const build_options = @import("build_options");

pub const uses_debug_allocator = builtin.mode == .debug or builtin.mode == .safe;

pub const CheckResult = enum {
    ok,
    leak,
};

/// Recording allocation stack traces unwinds through every loaded module. The
/// build can turn that off (`-Dalloc-trace-frames=0`) for hosts where Zig's
/// unwinder cannot do that: in a sandboxed container whose vDSO has a wrapping
/// load address, Zig 0.17.1 panics with "integer overflow" in
/// std/debug/SelfInfo/Elf.zig on the first allocation and then deadlocks
/// reporting the panic. Leak detection works either way.
pub const DebugAllocatorType = std.heap.DebugAllocator(.{
    .stack_trace_frames = build_options.alloc_trace_frames,
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
