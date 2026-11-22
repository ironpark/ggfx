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
