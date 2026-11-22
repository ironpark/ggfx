const std = @import("std");
const abi = @import("abi.zig");
const errors = @import("errors.zig");
const Diag = errors.Diag;
const Error = errors.Error;

pub const DrawCall = struct {
    dst: u32,
    srcs: [abi.max_src_images]u32,
    shader: u32,
    index_offset: u32,
    blend: abi.Blend,
    internal: *align(4) const [abi.internal_uniform_dwords]u32,
    internal_version: u64,
    user: []align(4) const u32,
    user_version: u64,
    regions: []align(4) const abi.DrawRegion,
};

fn fail(diag: *Diag, cmd_idx: u32, op_name: []const u8, byte_off: usize, msg: []const u8) Error {
    diag.print("op #{d} ({s}) at byte {d}: {s}", .{ cmd_idx, op_name, byte_off, msg });
    return error.MalformedBatch;
}

/// Validates and replays every command in `batch` into `be`.
/// Validation runs in all build modes (including ReleaseFast) in O(n) time.
pub fn replay(be: anytype, batch: *const abi.Batch, diag: *Diag) Error!void {
    if (batch.struct_size != @sizeOf(abi.Batch)) {
        diag.print("batch struct_size mismatch: got {d}, want {d}", .{
            batch.struct_size,
            @sizeOf(abi.Batch),
        });
        return error.MalformedBatch;
    }

    const cmds_len: usize = std.math.cast(usize, batch.cmds_len) orelse {
        diag.set("batch cmds_len overflows usize");
        return error.MalformedBatch;
    };
    const payload_len: usize = std.math.cast(usize, batch.payload_len) orelse {
        diag.set("batch payload_len overflows usize");
        return error.MalformedBatch;
    };

    const payload: []const u8 = if (payload_len == 0)
        &.{}
    else blk: {
        const ptr = batch.payload orelse {
            diag.set("batch payload is null with non-zero payload_len");
            return error.MalformedBatch;
        };
        if (@intFromPtr(ptr) % 16 != 0) {
            diag.set("batch payload pointer is not 16-byte aligned");
            return error.MalformedBatch;
        }
        break :blk ptr[0..payload_len];
    };

    if (cmds_len == 0) {
        if (batch.cmd_count != 0) {
            diag.print("batch cmd_count is {d} but cmds_len is 0", .{batch.cmd_count});
            return error.MalformedBatch;
        }
        return;
    }

    const cmds_ptr = batch.cmds orelse {
        diag.set("batch cmds is null with non-zero cmds_len");
        return error.MalformedBatch;
    };
    if (@intFromPtr(cmds_ptr) % 8 != 0) {
        diag.set("batch cmds pointer is not 8-byte aligned");
        return error.MalformedBatch;
    }
    if (cmds_len % 8 != 0) {
        diag.print("batch cmds_len ({d}) is not a multiple of 8", .{cmds_len});
        return error.MalformedBatch;
    }
    const cmds: []align(8) const u8 = @alignCast(cmds_ptr[0..cmds_len]);

    var offset: usize = 0;
    var cmd_idx: u32 = 0;

    while (offset < cmds.len) {
        const remaining = cmds.len - offset;
        if (remaining < @sizeOf(abi.CmdHeader)) {
            return fail(diag, cmd_idx, "HEADER", offset, "truncated command header");
        }

        const rec_ptr: [*]align(8) const u8 = @alignCast(cmds.ptr + offset);
        const hdr: *const abi.CmdHeader = @ptrCast(rec_ptr);
        const op: abi.Op = @enumFromInt(hdr.op);
        const op_name = op.name();

        if (hdr.size < @sizeOf(abi.CmdHeader)) {
            return fail(diag, cmd_idx, op_name, offset, "command size smaller than header");
        }
        if (hdr.size % 8 != 0) {
            return fail(diag, cmd_idx, op_name, offset, "command size is not 8-byte aligned");
        }
        if (hdr.size > remaining) {
            return fail(diag, cmd_idx, op_name, offset, "command size exceeds remaining batch bytes");
        }

        switch (op) {
            .set_vertices => {
                if (hdr.size != @sizeOf(abi.CmdSetVertices)) {
                    return fail(diag, cmd_idx, op_name, offset, "invalid SET_VERTICES record size");
                }
                const cmd: *const abi.CmdSetVertices = @ptrCast(rec_ptr);
                const verts: []const f32 = if (cmd.vertex_float_count == 0)
                    &.{}
                else blk: {
                    const p = cmd.vertices orelse return fail(diag, cmd_idx, op_name, offset, "null vertices pointer");
                    if (@intFromPtr(p) % @alignOf(f32) != 0) {
                        return fail(diag, cmd_idx, op_name, offset, "unaligned vertices pointer");
                    }
                    break :blk p[0..cmd.vertex_float_count];
                };
                const idxs: []const u32 = if (cmd.index_count == 0)
                    &.{}
                else blk: {
                    const p = cmd.indices orelse return fail(diag, cmd_idx, op_name, offset, "null indices pointer");
                    if (@intFromPtr(p) % @alignOf(u32) != 0) {
                        return fail(diag, cmd_idx, op_name, offset, "unaligned indices pointer");
                    }
                    break :blk p[0..cmd.index_count];
                };
                try be.setVertices(verts, idxs, diag);
            },
            .new_image => {
                if (hdr.size != @sizeOf(abi.CmdNewImage)) {
                    return fail(diag, cmd_idx, op_name, offset, "invalid NEW_IMAGE record size");
                }
                const cmd: *const abi.CmdNewImage = @ptrCast(rec_ptr);
                const fmt: abi.PixelFormat = switch (hdr.aux) {
                    @intFromEnum(abi.PixelFormat.rgba8) => .rgba8,
                    @intFromEnum(abi.PixelFormat.a8) => .a8,
                    else => return fail(diag, cmd_idx, op_name, offset, "unknown pixel format"),
                };
                try be.newImage(cmd.id, cmd.width, cmd.height, cmd.internal_width, cmd.internal_height, fmt, diag);
            },
            .new_screen_image => {
                if (hdr.size != @sizeOf(abi.CmdNewScreenImage)) {
                    return fail(diag, cmd_idx, op_name, offset, "invalid NEW_SCREEN_IMAGE record size");
                }
                const cmd: *const abi.CmdNewScreenImage = @ptrCast(rec_ptr);
                try be.newScreenImage(cmd.id, cmd.surface_id, cmd.width, cmd.height, diag);
            },
            .dispose_image => {
                if (hdr.size != @sizeOf(abi.CmdDisposeImage)) {
                    return fail(diag, cmd_idx, op_name, offset, "invalid DISPOSE_IMAGE record size");
                }
                const cmd: *const abi.CmdDisposeImage = @ptrCast(rec_ptr);
                try be.disposeImage(cmd.id);
            },
            .dispose_shader => {
                if (hdr.size != @sizeOf(abi.CmdDisposeShader)) {
                    return fail(diag, cmd_idx, op_name, offset, "invalid DISPOSE_SHADER record size");
                }
                const cmd: *const abi.CmdDisposeShader = @ptrCast(rec_ptr);
                try be.disposeShader(cmd.id);
            },
            .dispose_surface => {
                if (hdr.size != @sizeOf(abi.CmdDisposeSurface)) {
                    return fail(diag, cmd_idx, op_name, offset, "invalid DISPOSE_SURFACE record size");
                }
                const cmd: *const abi.CmdDisposeSurface = @ptrCast(rec_ptr);
                try be.disposeSurface(cmd.surface_id);
            },
            .write_pixels => {
                if (hdr.size < @sizeOf(abi.CmdWritePixels)) {
                    return fail(diag, cmd_idx, op_name, offset, "truncated WRITE_PIXELS record");
                }
                const cmd: *const abi.CmdWritePixels = @ptrCast(rec_ptr);
                const expected: u64 = @sizeOf(abi.CmdWritePixels) +
                    @as(u64, cmd.count) * @sizeOf(abi.PixelsRegion);
                if (expected != hdr.size) {
                    return fail(diag, cmd_idx, op_name, offset, "WRITE_PIXELS size does not match region count");
                }
                const regions_ptr: [*]align(4) const abi.PixelsRegion = @ptrCast(
                    @as([*]align(4) const u8, @alignCast(rec_ptr + @sizeOf(abi.CmdWritePixels))),
                );
                const regions = regions_ptr[0..cmd.count];
                for (regions) |r| {
                    if (r.payload_offset % 16 != 0) {
                        return fail(diag, cmd_idx, op_name, offset, "WRITE_PIXELS payload_offset not 16-byte aligned");
                    }
                    const end = @as(u64, r.payload_offset) + @as(u64, r.payload_len);
                    if (end > payload.len) {
                        return fail(diag, cmd_idx, op_name, offset, "WRITE_PIXELS payload slice out of bounds");
                    }
                }
                try be.writePixels(cmd.image_id, regions, payload, diag);
            },
            .draw_triangles => {
                if (hdr.size < @sizeOf(abi.CmdDrawTriangles)) {
                    return fail(diag, cmd_idx, op_name, offset, "truncated DRAW_TRIANGLES record");
                }
                const cmd: *const abi.CmdDrawTriangles = @ptrCast(rec_ptr);
                const expected: u64 = @sizeOf(abi.CmdDrawTriangles) +
                    @as(u64, cmd.region_count) * @sizeOf(abi.DrawRegion);
                if (expected != hdr.size) {
                    return fail(diag, cmd_idx, op_name, offset, "DRAW_TRIANGLES size does not match region_count");
                }
                if (cmd.internal_version == 0) {
                    return fail(diag, cmd_idx, op_name, offset, "internal_version must not be 0");
                }
                if (cmd.internal_offset % 16 != 0) {
                    return fail(diag, cmd_idx, op_name, offset, "internal_offset not 16-byte aligned");
                }
                const internal_end = @as(u64, cmd.internal_offset) + abi.internal_uniform_bytes;
                if (internal_end > payload.len) {
                    return fail(diag, cmd_idx, op_name, offset, "internal uniform block out of payload bounds");
                }
                if (cmd.user_dwords > abi.max_user_uniform_dwords) {
                    return fail(diag, cmd_idx, op_name, offset, "user_dwords exceeds limit");
                }

                const user_slice: []align(4) const u32 = if (cmd.user_dwords == 0)
                    &.{}
                else blk: {
                    if (cmd.user_version == 0) {
                        return fail(diag, cmd_idx, op_name, offset, "user_version must not be 0 when user_dwords > 0");
                    }
                    if (cmd.user_offset % 16 != 0) {
                        return fail(diag, cmd_idx, op_name, offset, "user_offset not 16-byte aligned");
                    }
                    const user_bytes = @as(u64, cmd.user_dwords) * 4;
                    if (@as(u64, cmd.user_offset) + user_bytes > payload.len) {
                        return fail(diag, cmd_idx, op_name, offset, "user uniform block out of payload bounds");
                    }
                    const up: [*]align(4) const u32 = @ptrCast(@alignCast(payload.ptr + cmd.user_offset));
                    break :blk up[0..cmd.user_dwords];
                };

                const internal_ptr: *align(4) const [abi.internal_uniform_dwords]u32 = @ptrCast(
                    @alignCast(payload.ptr + cmd.internal_offset),
                );
                const regions_ptr: [*]align(4) const abi.DrawRegion = @ptrCast(
                    @as([*]align(4) const u8, @alignCast(rec_ptr + @sizeOf(abi.CmdDrawTriangles))),
                );

                const dc = DrawCall{
                    .dst = cmd.dst_id,
                    .srcs = cmd.src_ids,
                    .shader = cmd.shader_id,
                    .index_offset = cmd.index_offset,
                    .blend = cmd.blend,
                    .internal = internal_ptr,
                    .internal_version = cmd.internal_version,
                    .user = user_slice,
                    .user_version = cmd.user_version,
                    .regions = regions_ptr[0..cmd.region_count],
                };
                try be.drawTriangles(&dc, diag);
            },
            .set_damage => {
                if (hdr.size < @sizeOf(abi.CmdSetDamage)) {
                    return fail(diag, cmd_idx, op_name, offset, "truncated SET_DAMAGE record");
                }
                const cmd: *const abi.CmdSetDamage = @ptrCast(rec_ptr);
                if (cmd.count == abi.damage_full) {
                    if (hdr.size != @sizeOf(abi.CmdSetDamage)) {
                        return fail(diag, cmd_idx, op_name, offset, "SET_DAMAGE with DAMAGE_FULL must have size 16");
                    }
                    try be.setDamage(cmd.surface_id, null);
                } else {
                    const expected: u64 = @sizeOf(abi.CmdSetDamage) +
                        @as(u64, cmd.count) * @sizeOf(abi.Rect);
                    if (expected != hdr.size) {
                        return fail(diag, cmd_idx, op_name, offset, "SET_DAMAGE size does not match rect count");
                    }
                    const rects_ptr: [*]align(4) const abi.Rect = @ptrCast(
                        @as([*]align(4) const u8, @alignCast(rec_ptr + @sizeOf(abi.CmdSetDamage))),
                    );
                    try be.setDamage(cmd.surface_id, rects_ptr[0..cmd.count]);
                }
            },
            _ => return fail(diag, cmd_idx, op_name, offset, "unknown command opcode"),
        }

        offset += hdr.size;
        cmd_idx = std.math.add(u32, cmd_idx, 1) catch {
            diag.set("batch command count overflow");
            return error.MalformedBatch;
        };
    }

    if (cmd_idx != batch.cmd_count) {
        diag.print("batch cmd_count mismatch: header says {d}, decoded {d}", .{
            batch.cmd_count,
            cmd_idx,
        });
        return error.MalformedBatch;
    }
}
