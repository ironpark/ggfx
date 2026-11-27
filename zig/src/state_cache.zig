const std = @import("std");
const abi = @import("abi.zig");

/// Shared per-encoder / per-command-list binding and uniform version cache.
/// Backends reset this whenever starting a new render pass/encoder or command list.
pub const BindCache = struct {
    pipeline: usize = 0,
    textures: [abi.max_src_images]usize = @splat(0),
    scissor: abi.Rect = .{ .x0 = 0, .y0 = 0, .x1 = 0, .y1 = 0 },
    scissor_valid: bool = false,
    viewport: [2]u32 = .{ 0, 0 },
    vertex_internal_set: bool = false,
    projection: [16]u32 = @splat(0),
    fragment_internal_version: u64 = 0,
    user_version: u64 = 0,
    vertex_buffer: usize = 0,

    pub fn reset(self: *BindCache) void {
        self.* = .{};
    }

    pub fn invalidateTexture(self: *BindCache, handle: usize) void {
        if (handle == 0) return;
        for (&self.textures) |*slot| {
            if (slot.* == handle) slot.* = 0;
        }
    }
};

/// Lifecycle phase of a Vulkan primary command buffer so abort paths never call
/// vkEndCommandBuffer on an already-ended/invalid CB or reset a pending CB.
pub const CmdBufferPhase = enum {
    idle,
    recording,
    ended,
    invalid,
    submitted,

    pub const AbortAction = enum {
        none,
        /// Reset directly with vkResetCommandBuffer without calling vkEndCommandBuffer.
        reset_only,
        /// Command buffer may be pending on the GPU; do not reset and mark device lost.
        leave_pending_and_lose_device,
    };

    pub fn onBegin(self: *CmdBufferPhase) void {
        self.* = .recording;
    }

    pub fn onEndResult(self: *CmdBufferPhase, ok: bool) void {
        self.* = if (ok) .ended else .invalid;
    }

    pub fn onSubmitAttempt(self: *CmdBufferPhase) void {
        self.* = .submitted;
    }

    pub fn onSubmitted(self: *CmdBufferPhase) void {
        self.* = .idle;
    }

    pub fn onAbort(self: *CmdBufferPhase) AbortAction {
        const action: AbortAction = switch (self.*) {
            .idle => .none,
            .recording, .ended, .invalid => .reset_only,
            .submitted => .leave_pending_and_lose_device,
        };
        self.* = .idle;
        return action;
    }
};

test "CmdBufferPhase abort transitions never end twice or reset pending CB" {
    var phase: CmdBufferPhase = .idle;
    try std.testing.expectEqual(CmdBufferPhase.AbortAction.none, phase.onAbort());
    try std.testing.expectEqual(CmdBufferPhase.idle, phase);

    // Abort while recording -> reset_only.
    phase.onBegin();
    try std.testing.expectEqual(CmdBufferPhase.recording, phase);
    try std.testing.expectEqual(CmdBufferPhase.AbortAction.reset_only, phase.onAbort());
    try std.testing.expectEqual(CmdBufferPhase.idle, phase);

    // Abort after vkEndCommandBuffer succeeded (e.g. vkResetFences failed) -> reset_only.
    phase.onBegin();
    phase.onEndResult(true);
    try std.testing.expectEqual(CmdBufferPhase.ended, phase);
    try std.testing.expectEqual(CmdBufferPhase.AbortAction.reset_only, phase.onAbort());
    try std.testing.expectEqual(CmdBufferPhase.idle, phase);

    // Abort after vkEndCommandBuffer failed -> invalid -> reset_only.
    phase.onBegin();
    phase.onEndResult(false);
    try std.testing.expectEqual(CmdBufferPhase.invalid, phase);
    try std.testing.expectEqual(CmdBufferPhase.AbortAction.reset_only, phase.onAbort());
    try std.testing.expectEqual(CmdBufferPhase.idle, phase);

    // Abort after vkQueueSubmit was attempted and failed -> leave_pending_and_lose_device.
    phase.onBegin();
    phase.onEndResult(true);
    phase.onSubmitAttempt();
    try std.testing.expectEqual(CmdBufferPhase.submitted, phase);
    try std.testing.expectEqual(CmdBufferPhase.AbortAction.leave_pending_and_lose_device, phase.onAbort());
    try std.testing.expectEqual(CmdBufferPhase.idle, phase);

    // Normal submit completes cleanly back to idle.
    phase.onBegin();
    phase.onEndResult(true);
    phase.onSubmitAttempt();
    phase.onSubmitted();
    try std.testing.expectEqual(CmdBufferPhase.idle, phase);
}
