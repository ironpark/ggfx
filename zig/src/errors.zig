const std = @import("std");
const abi = @import("abi.zig");

pub const Error = error{
    InvalidArgument,
    AbiMismatch,
    BackendUnavailable,
    OutOfMemory,
    DeviceLost,
    UnknownHandle,
    DuplicateHandle,
    ShaderCompile,
    Unsupported,
    MalformedBatch,
    SurfaceLost,
    Internal,
    LeakDetected,
};

pub fn toStatus(err: Error) abi.Status {
    return switch (err) {
        error.InvalidArgument => .err_invalid_argument,
        error.AbiMismatch => .err_abi_mismatch,
        error.BackendUnavailable => .err_backend_unavailable,
        error.OutOfMemory => .err_out_of_memory,
        error.DeviceLost => .err_device_lost,
        error.UnknownHandle => .err_unknown_handle,
        error.DuplicateHandle => .err_duplicate_handle,
        error.ShaderCompile => .err_shader_compile,
        error.Unsupported => .err_unsupported,
        error.MalformedBatch => .err_malformed_batch,
        error.SurfaceLost => .err_surface_lost,
        error.Internal => .err_internal,
        error.LeakDetected => .err_leak_detected,
    };
}

pub fn fromStatus(status: abi.Status) Error!void {
    return switch (status) {
        .ok => {},
        .err_invalid_argument => error.InvalidArgument,
        .err_abi_mismatch => error.AbiMismatch,
        .err_backend_unavailable => error.BackendUnavailable,
        .err_out_of_memory => error.OutOfMemory,
        .err_device_lost => error.DeviceLost,
        .err_unknown_handle => error.UnknownHandle,
        .err_duplicate_handle => error.DuplicateHandle,
        .err_shader_compile => error.ShaderCompile,
        .err_unsupported => error.Unsupported,
        .err_malformed_batch => error.MalformedBatch,
        .err_surface_lost => error.SurfaceLost,
        .err_internal => error.Internal,
        .err_leak_detected => error.LeakDetected,
    };
}

/// Diagnostic error message formatter writing NUL-terminated UTF-8 into a caller-provided buffer.
pub const Diag = struct {
    buf: ?[*]u8,
    cap: usize,
    len: usize = 0,

    pub fn init(buf: ?[*]u8, cap: usize) Diag {
        if (buf) |b| {
            if (cap > 0) b[0] = 0;
        }
        return .{ .buf = buf, .cap = cap, .len = 0 };
    }

    pub fn none() Diag {
        return .{ .buf = null, .cap = 0, .len = 0 };
    }

    pub fn set(self: *Diag, msg: []const u8) void {
        const b = self.buf orelse return;
        if (self.cap == 0) return;
        const max_copy = self.cap - 1;
        const n = @min(msg.len, max_copy);
        if (n > 0) @memcpy(b[0..n], msg[0..n]);
        b[n] = 0;
        self.len = n;
    }

    pub fn print(self: *Diag, comptime fmt: []const u8, args: anytype) void {
        const b = self.buf orelse return;
        if (self.cap == 0) return;
        const max_copy = self.cap - 1;
        if (max_copy == 0) {
            b[0] = 0;
            self.len = 0;
            return;
        }
        if (std.fmt.bufPrint(b[0..max_copy], fmt, args)) |written| {
            b[written.len] = 0;
            self.len = written.len;
        } else |_| {
            // Truncated output: fall back to a stack scratch buffer to capture prefix cleanly.
            var scratch: [1024]u8 = undefined;
            const prefix = std.fmt.bufPrint(&scratch, fmt, args) catch scratch[0..];
            const n = @min(prefix.len, max_copy);
            @memcpy(b[0..n], prefix[0..n]);
            b[n] = 0;
            self.len = n;
        }
    }

    pub fn slice(self: *const Diag) []const u8 {
        const b = self.buf orelse return &.{};
        return b[0..self.len];
    }
};

test "errors roundtrip and diag truncation" {
    inline for (@typeInfo(abi.Status).@"enum".field_values) |v| {
        const st: abi.Status = @enumFromInt(v);
        if (st == .ok) {
            try fromStatus(st);
        } else {
            const err = blk: {
                fromStatus(st) catch |e| break :blk e;
                unreachable;
            };
            try std.testing.expectEqual(st, toStatus(err));
        }
    }

    var buf: [12]u8 = undefined;
    var diag = Diag.init(&buf, buf.len);
    diag.print("error code {d}: {s}", .{ 42, "something long" });
    try std.testing.expectEqual(@as(u8, 0), buf[11]);
    try std.testing.expectEqualStrings("error code ", diag.slice());
}
