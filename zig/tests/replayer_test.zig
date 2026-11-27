const std = @import("std");
const ggfx = @import("ggfx");
const abi = ggfx.abi;
const errors = ggfx.errors;
const replayer = ggfx.replayer;
const Diag = errors.Diag;
const Error = errors.Error;

const golden_batch_bytes = @embedFile("golden/batch_v1.bin");

const Recorder = struct {
    set_vertices_calls: usize = 0,
    last_verts_len: usize = 0,
    last_indices_len: usize = 0,

    new_image_calls: usize = 0,
    new_images: [4]struct { id: u32, w: u32, h: u32, iw: u32, ih: u32, fmt: abi.PixelFormat } = undefined,

    new_screen_image_calls: usize = 0,
    last_screen_img: struct { id: u32, surface_id: u32, w: u32, h: u32 } = undefined,

    dispose_image_id: u32 = 0,
    dispose_shader_id: u32 = 0,
    dispose_surface_id: u32 = 0,

    write_pixels_calls: usize = 0,
    wp_image_id: u32 = 0,
    wp_row_pitch_align: u32 = 0,
    wp_region_count: usize = 0,
    wp_r0_rect: abi.Rect = undefined,
    wp_r0_first: u8 = 0,
    wp_r0_last: u8 = 0,
    wp_r1_rect: abi.Rect = undefined,
    wp_r1_first: u8 = 0,
    wp_r1_last: u8 = 0,

    set_damage_calls: usize = 0,
    dmg0_surface: u32 = 0,
    dmg0_count: usize = 0,
    dmg0_r0: abi.Rect = undefined,
    dmg0_r1: abi.Rect = undefined,
    dmg1_surface: u32 = 0,
    dmg1_is_null: bool = false,

    draw_calls: usize = 0,
    dc_dst: u32 = 0,
    dc_shader: u32 = 0,
    dc_srcs: [4]u32 = .{ 0, 0, 0, 0 },
    dc_index_offset: u32 = 0,
    dc_blend: abi.Blend = undefined,
    dc_internal_version: u64 = 0,
    dc_user_version: u64 = 0,
    dc_internal_0: u32 = 0,
    dc_internal_71: u32 = 0,
    dc_user_len: usize = 0,
    dc_user_0: u32 = 0,
    dc_user_3: u32 = 0,
    dc_region_count: usize = 0,
    dc_r0: abi.DrawRegion = undefined,
    dc_r1: abi.DrawRegion = undefined,

    pub fn setVertices(self: *Recorder, vertices: []const f32, indices: []const u32, diag: *Diag) Error!void {
        _ = diag;
        self.set_vertices_calls += 1;
        self.last_verts_len = vertices.len;
        self.last_indices_len = indices.len;
    }

    pub fn newImage(
        self: *Recorder,
        id: u32,
        w: u32,
        h: u32,
        iw: u32,
        ih: u32,
        fmt: abi.PixelFormat,
        diag: *Diag,
    ) Error!void {
        _ = diag;
        self.new_images[self.new_image_calls] = .{ .id = id, .w = w, .h = h, .iw = iw, .ih = ih, .fmt = fmt };
        self.new_image_calls += 1;
    }

    pub fn newScreenImage(self: *Recorder, id: u32, surface_id: u32, w: u32, h: u32, diag: *Diag) Error!void {
        _ = diag;
        self.new_screen_image_calls += 1;
        self.last_screen_img = .{ .id = id, .surface_id = surface_id, .w = w, .h = h };
    }

    pub fn disposeImage(self: *Recorder, id: u32) Error!void {
        self.dispose_image_id = id;
    }

    pub fn disposeShader(self: *Recorder, id: u32) Error!void {
        self.dispose_shader_id = id;
    }

    pub fn disposeSurface(self: *Recorder, id: u32) Error!void {
        self.dispose_surface_id = id;
    }

    pub fn writePixels(
        self: *Recorder,
        id: u32,
        regions: []align(4) const abi.PixelsRegion,
        payload: []const u8,
        row_pitch_align: u32,
        diag: *Diag,
    ) Error!void {
        _ = diag;
        self.write_pixels_calls += 1;
        self.wp_image_id = id;
        self.wp_row_pitch_align = row_pitch_align;
        self.wp_region_count = regions.len;
        if (regions.len >= 2) {
            self.wp_r0_rect = regions[0].rect;
            const s0 = payload[regions[0].payload_offset .. regions[0].payload_offset + regions[0].payload_len];
            self.wp_r0_first = s0[0];
            self.wp_r0_last = s0[s0.len - 1];

            self.wp_r1_rect = regions[1].rect;
            const s1 = payload[regions[1].payload_offset .. regions[1].payload_offset + regions[1].payload_len];
            self.wp_r1_first = s1[0];
            self.wp_r1_last = s1[s1.len - 1];
        }
    }

    pub fn drawTriangles(self: *Recorder, dc: *const replayer.DrawCall, diag: *Diag) Error!void {
        _ = diag;
        self.draw_calls += 1;
        self.dc_dst = dc.dst;
        self.dc_shader = dc.shader;
        self.dc_srcs = dc.srcs;
        self.dc_index_offset = dc.index_offset;
        self.dc_blend = dc.blend;
        self.dc_internal_version = dc.internal_version;
        self.dc_user_version = dc.user_version;
        self.dc_internal_0 = dc.internal[0];
        self.dc_internal_71 = dc.internal[71];
        self.dc_user_len = dc.user.len;
        if (dc.user.len >= 4) {
            self.dc_user_0 = dc.user[0];
            self.dc_user_3 = dc.user[3];
        }
        self.dc_region_count = dc.regions.len;
        if (dc.regions.len >= 2) {
            self.dc_r0 = dc.regions[0];
            self.dc_r1 = dc.regions[1];
        }
    }

    pub fn setDamage(self: *Recorder, surface_id: u32, rects: ?[]align(4) const abi.Rect) Error!void {
        if (self.set_damage_calls == 0) {
            self.dmg0_surface = surface_id;
            if (rects) |rs| {
                self.dmg0_count = rs.len;
                self.dmg0_r0 = rs[0];
                self.dmg0_r1 = rs[1];
            }
        } else {
            self.dmg1_surface = surface_id;
            self.dmg1_is_null = (rects == null);
        }
        self.set_damage_calls += 1;
    }
};

test "golden batch_v1.bin decodes all op 1..9 fields accurately" {
    const gpa = std.testing.allocator;
    try std.testing.expect(golden_batch_bytes.len >= 48);
    try std.testing.expectEqualSlices(u8, "GGFXB1\x00\x00", golden_batch_bytes[0..8]);

    const cmd_count = std.mem.readInt(u32, golden_batch_bytes[8..12], .little);
    const cmds_len = std.mem.readInt(u64, golden_batch_bytes[16..24], .little);
    const payload_len = std.mem.readInt(u64, golden_batch_bytes[24..32], .little);
    const sequence = std.mem.readInt(u64, golden_batch_bytes[32..40], .little);

    try std.testing.expectEqual(@as(u32, 11), cmd_count);
    try std.testing.expectEqual(@as(u64, 42), sequence);

    const cmds_aligned = (cmds_len + 15) & ~@as(u64, 15);
    try std.testing.expectEqual(@as(usize, 48 + cmds_aligned + payload_len), golden_batch_bytes.len);

    // Copy into explicitly 16-byte aligned buffers so alignment checks are exercised strictly.
    const cmds_buf = try gpa.alignedAlloc(u8, .@"16", @intCast(cmds_len));
    defer gpa.free(cmds_buf);
    @memcpy(cmds_buf, golden_batch_bytes[48 .. 48 + @as(usize, @intCast(cmds_len))]);

    const payload_buf = try gpa.alignedAlloc(u8, .@"16", @intCast(payload_len));
    defer gpa.free(payload_buf);
    const pay_start: usize = @intCast(48 + cmds_aligned);
    @memcpy(payload_buf, golden_batch_bytes[pay_start .. pay_start + @as(usize, @intCast(payload_len))]);

    const batch = abi.Batch{
        .struct_size = @sizeOf(abi.Batch),
        .cmd_count = cmd_count,
        .cmds = cmds_buf.ptr,
        .cmds_len = cmds_len,
        .payload = payload_buf.ptr,
        .payload_len = payload_len,
        .sequence = sequence,
    };

    var rec = Recorder{};
    var err_buf: [256]u8 = undefined;
    var diag = Diag.init(&err_buf, err_buf.len);
    try replayer.replay(&rec, &batch, &diag);

    // Op 1: SET_VERTICES
    try std.testing.expectEqual(@as(usize, 1), rec.set_vertices_calls);
    try std.testing.expectEqual(@as(usize, 0), rec.last_verts_len);
    try std.testing.expectEqual(@as(usize, 0), rec.last_indices_len);

    // Op 2: NEW_IMAGE (RGBA8 & A8)
    try std.testing.expectEqual(@as(usize, 2), rec.new_image_calls);
    try std.testing.expectEqual(@as(u32, 1), rec.new_images[0].id);
    try std.testing.expectEqual(@as(u32, 8), rec.new_images[0].w);
    try std.testing.expectEqual(@as(u32, 4), rec.new_images[0].h);
    try std.testing.expectEqual(@as(u32, 16), rec.new_images[0].iw);
    try std.testing.expectEqual(@as(u32, 16), rec.new_images[0].ih);
    try std.testing.expectEqual(abi.PixelFormat.rgba8, rec.new_images[0].fmt);

    try std.testing.expectEqual(@as(u32, 2), rec.new_images[1].id);
    try std.testing.expectEqual(@as(u32, 4), rec.new_images[1].w);
    try std.testing.expectEqual(@as(u32, 4), rec.new_images[1].h);
    try std.testing.expectEqual(abi.PixelFormat.a8, rec.new_images[1].fmt);

    // Op 3: NEW_SCREEN_IMAGE
    try std.testing.expectEqual(@as(usize, 1), rec.new_screen_image_calls);
    try std.testing.expectEqual(@as(u32, 3), rec.last_screen_img.id);
    try std.testing.expectEqual(@as(u32, 10), rec.last_screen_img.surface_id);
    try std.testing.expectEqual(@as(u32, 640), rec.last_screen_img.w);
    try std.testing.expectEqual(@as(u32, 480), rec.last_screen_img.h);

    // Op 7: WRITE_PIXELS
    try std.testing.expectEqual(@as(usize, 1), rec.write_pixels_calls);
    try std.testing.expectEqual(@as(u32, 1), rec.wp_image_id);
    try std.testing.expectEqual(@as(usize, 2), rec.wp_region_count);
    try std.testing.expect(rec.wp_r0_rect.eql(.{ .x0 = 0, .y0 = 0, .x1 = 2, .y1 = 2 }));
    try std.testing.expectEqual(@as(u8, 0x10), rec.wp_r0_first);
    try std.testing.expectEqual(@as(u8, 0x1f), rec.wp_r0_last);
    try std.testing.expect(rec.wp_r1_rect.eql(.{ .x0 = 2, .y0 = 1, .x1 = 4, .y1 = 2 }));
    try std.testing.expectEqual(@as(u8, 0x20), rec.wp_r1_first);
    try std.testing.expectEqual(@as(u8, 0x27), rec.wp_r1_last);

    // Op 9: SET_DAMAGE (rects and full)
    try std.testing.expectEqual(@as(usize, 2), rec.set_damage_calls);
    try std.testing.expectEqual(@as(u32, 10), rec.dmg0_surface);
    try std.testing.expectEqual(@as(usize, 2), rec.dmg0_count);
    try std.testing.expect(rec.dmg0_r0.eql(.{ .x0 = 10, .y0 = 20, .x1 = 110, .y1 = 120 }));
    try std.testing.expect(rec.dmg0_r1.eql(.{ .x0 = 200, .y0 = 50, .x1 = 300, .y1 = 150 }));
    try std.testing.expectEqual(@as(u32, 10), rec.dmg1_surface);
    try std.testing.expect(rec.dmg1_is_null);

    // Op 8: DRAW_TRIANGLES
    try std.testing.expectEqual(@as(usize, 1), rec.draw_calls);
    try std.testing.expectEqual(@as(u32, 3), rec.dc_dst);
    try std.testing.expectEqual(@as(u32, 7), rec.dc_shader);
    try std.testing.expectEqual([4]u32{ 1, 2, 0, 0 }, rec.dc_srcs);
    try std.testing.expectEqual(@as(u32, 6), rec.dc_index_offset);
    try std.testing.expect(rec.dc_blend.eql(.{ .src_rgb = 1, .src_a = 1, .dst_rgb = 5, .dst_a = 5, .op_rgb = 0, .op_a = 0 }));
    try std.testing.expectEqual(@as(u64, 101), rec.dc_internal_version);
    try std.testing.expectEqual(@as(u64, 202), rec.dc_user_version);
    try std.testing.expectEqual(@as(u32, 0x1000_0000), rec.dc_internal_0);
    try std.testing.expectEqual(@as(u32, 0x1000_0000 + 71), rec.dc_internal_71);
    try std.testing.expectEqual(@as(usize, 4), rec.dc_user_len);
    try std.testing.expectEqual(@as(u32, 0xDEAD_0001), rec.dc_user_0);
    try std.testing.expectEqual(@as(u32, 0xDEAD_0004), rec.dc_user_3);
    try std.testing.expectEqual(@as(usize, 2), rec.dc_region_count);
    try std.testing.expect(rec.dc_r0.rect.eql(.{ .x0 = 0, .y0 = 0, .x1 = 320, .y1 = 240 }));
    try std.testing.expectEqual(@as(u32, 6), rec.dc_r0.index_count);
    try std.testing.expect(rec.dc_r1.rect.eql(.{ .x0 = 320, .y0 = 240, .x1 = 640, .y1 = 480 }));
    try std.testing.expectEqual(@as(u32, 12), rec.dc_r1.index_count);

    // Ops 4, 5, 6: DISPOSE_*
    try std.testing.expectEqual(@as(u32, 2), rec.dispose_image_id);
    try std.testing.expectEqual(@as(u32, 7), rec.dispose_shader_id);
    try std.testing.expectEqual(@as(u32, 10), rec.dispose_surface_id);
}

test "replayer validates malformed batches" {
    var rec = Recorder{};
    var err_buf: [256]u8 = undefined;
    var diag = Diag.init(&err_buf, err_buf.len);

    // Bad struct_size
    var bad_batch = abi.Batch{
        .struct_size = 12,
        .cmd_count = 0,
        .cmds = null,
        .cmds_len = 0,
        .payload = null,
        .payload_len = 0,
        .sequence = 0,
    };
    try std.testing.expectError(error.MalformedBatch, replayer.replay(&rec, &bad_batch, &diag));

    // Unaligned cmds pointer
    var raw_cmds: [32]u8 align(16) = @splat(0);
    bad_batch = .{
        .struct_size = @sizeOf(abi.Batch),
        .cmd_count = 1,
        .cmds = raw_cmds[1..].ptr,
        .cmds_len = 16,
        .payload = null,
        .payload_len = 0,
        .sequence = 1,
    };
    try std.testing.expectError(error.MalformedBatch, replayer.replay(&rec, &bad_batch, &diag));

    // Unknown opcode
    var unk_cmd = abi.CmdDisposeImage{
        .hdr = .{ .op = 99, .flags = 0, .aux = 0, .size = @sizeOf(abi.CmdDisposeImage) },
        .id = 1,
        .reserved = 0,
    };
    const unk_bytes: [*]const u8 = @ptrCast(&unk_cmd);
    bad_batch = .{
        .struct_size = @sizeOf(abi.Batch),
        .cmd_count = 1,
        .cmds = unk_bytes,
        .cmds_len = @sizeOf(abi.CmdDisposeImage),
        .payload = null,
        .payload_len = 0,
        .sequence = 2,
    };
    try std.testing.expectError(error.MalformedBatch, replayer.replay(&rec, &bad_batch, &diag));
    try std.testing.expect(std.mem.indexOf(u8, diag.slice(), "op #0 (UNKNOWN) at byte 0") != null);

    // cmd_count mismatch
    var ok_cmd = abi.CmdDisposeImage{
        .hdr = .{ .op = @intFromEnum(abi.Op.dispose_image), .flags = 0, .aux = 0, .size = @sizeOf(abi.CmdDisposeImage) },
        .id = 5,
        .reserved = 0,
    };
    const ok_bytes: [*]const u8 = @ptrCast(&ok_cmd);
    bad_batch = .{
        .struct_size = @sizeOf(abi.Batch),
        .cmd_count = 2,
        .cmds = ok_bytes,
        .cmds_len = @sizeOf(abi.CmdDisposeImage),
        .payload = null,
        .payload_len = 0,
        .sequence = 3,
    };
    try std.testing.expectError(error.MalformedBatch, replayer.replay(&rec, &bad_batch, &diag));
}

test "replayer SET_VERTICES non-empty slices and full C API roundtrip" {
    const verts = [_]f32{ 0, 0, 1, 0, 0, 1 };
    const indices = [_]u32{ 0, 1, 2 };
    var cmd = abi.CmdSetVertices{
        .hdr = .{ .op = @intFromEnum(abi.Op.set_vertices), .flags = 0, .aux = 0, .size = @sizeOf(abi.CmdSetVertices) },
        .vertices = &verts,
        .indices = &indices,
        .vertex_float_count = verts.len,
        .index_count = indices.len,
    };
    const batch = abi.Batch{
        .struct_size = @sizeOf(abi.Batch),
        .cmd_count = 1,
        .cmds = @ptrCast(&cmd),
        .cmds_len = @sizeOf(abi.CmdSetVertices),
        .payload = null,
        .payload_len = 0,
        .sequence = 1,
    };

    var rec = Recorder{};
    var diag = Diag.none();
    try replayer.replay(&rec, &batch, &diag);
    try std.testing.expectEqual(@as(usize, 1), rec.set_vertices_calls);
    try std.testing.expectEqual(@as(usize, 6), rec.last_verts_len);
    try std.testing.expectEqual(@as(usize, 3), rec.last_indices_len);
}

test "replayer validates writePixels row_pitch_align and draw src != dst" {
    var rec = Recorder{};
    var err_buf: [256]u8 = undefined;
    var diag = Diag.init(&err_buf, err_buf.len);

    // Non-power-of-two hdr.aux in OP_WRITE_PIXELS must fail with MalformedBatch.
    const WriteRec = extern struct {
        cmd: abi.CmdWritePixels,
        reg: abi.PixelsRegion,
    };
    var payload: [64]u8 align(16) = @splat(0x55);
    var bad_wp = WriteRec{
        .cmd = .{
            .hdr = .{ .op = @intFromEnum(abi.Op.write_pixels), .flags = 0, .aux = 300, .size = @sizeOf(WriteRec) },
            .image_id = 1,
            .count = 1,
        },
        .reg = .{
            .rect = .{ .x0 = 0, .y0 = 0, .x1 = 2, .y1 = 2 },
            .payload_offset = 0,
            .payload_len = 16,
        },
    };
    var batch = abi.Batch{
        .struct_size = @sizeOf(abi.Batch),
        .cmd_count = 1,
        .cmds = @ptrCast(&bad_wp),
        .cmds_len = @sizeOf(WriteRec),
        .payload = &payload,
        .payload_len = payload.len,
        .sequence = 10,
    };
    try std.testing.expectError(error.MalformedBatch, replayer.replay(&rec, &batch, &diag));

    // Power-of-two hdr.aux=256 is accepted and forwarded to backend.writePixels.
    bad_wp.cmd.hdr.aux = 256;
    try replayer.replay(&rec, &batch, &diag);
    try std.testing.expectEqual(@as(u32, 256), rec.wp_row_pitch_align);

    // OP_DRAW_TRIANGLES with src == dst must be rejected with InvalidArgument.
    const DrawRec = extern struct {
        cmd: abi.CmdDrawTriangles,
        reg: abi.DrawRegion,
    };
    var uniforms: [abi.internal_uniform_dwords]u32 align(16) = @splat(0);
    var bad_draw = DrawRec{
        .cmd = .{
            .hdr = .{ .op = @intFromEnum(abi.Op.draw_triangles), .flags = 0, .aux = 0, .size = @sizeOf(DrawRec) },
            .dst_id = 5,
            .shader_id = 7,
            .src_ids = .{ 1, 5, 0, 0 },
            .index_offset = 0,
            .region_count = 1,
            .blend = .{ .src_rgb = 1, .src_a = 1, .dst_rgb = 0, .dst_a = 0, .op_rgb = 0, .op_a = 0 },
            .reserved0 = 0,
            .internal_version = 1,
            .user_version = 0,
            .internal_offset = 0,
            .user_offset = 0,
            .user_dwords = 0,
            .reserved1 = 0,
        },
        .reg = .{
            .rect = .{ .x0 = 0, .y0 = 0, .x1 = 4, .y1 = 4 },
            .index_count = 6,
            .reserved = 0,
        },
    };
    const draw_batch = abi.Batch{
        .struct_size = @sizeOf(abi.Batch),
        .cmd_count = 1,
        .cmds = @ptrCast(&bad_draw),
        .cmds_len = @sizeOf(DrawRec),
        .payload = std.mem.sliceAsBytes(&uniforms).ptr,
        .payload_len = @sizeOf(@TypeOf(uniforms)),
        .sequence = 11,
    };
    try std.testing.expectError(error.InvalidArgument, replayer.replay(&rec, &draw_batch, &diag));
}
