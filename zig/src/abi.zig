const std = @import("std");
const builtin = @import("builtin");

comptime {
    if (@sizeOf(usize) != 8 or builtin.cpu.arch.endian() != .little) {
        @compileError("ggfx_driver ABI requires a 64-bit little-endian target");
    }
}

pub const driver_abi_version: u32 = 1;

pub const Status = enum(i32) {
    ok = 0,
    err_invalid_argument = 1,
    err_abi_mismatch = 2,
    err_backend_unavailable = 3,
    err_out_of_memory = 4,
    err_device_lost = 5,
    err_unknown_handle = 6,
    err_duplicate_handle = 7,
    err_shader_compile = 8,
    err_unsupported = 9,
    err_malformed_batch = 10,
    err_surface_lost = 11,
    err_internal = 12,
    err_leak_detected = 13,
};

pub const Backend = enum(u32) {
    auto = 0,
    metal = 1,
    dx12 = 2,
    vulkan = 3,
    null = 255,
    _,
};

pub const FlushMode = enum(u32) {
    intermediate = 0,
    end_frame = 1,
    present = 2,
};

pub const PixelFormat = enum(u32) {
    rgba8 = 0,
    a8 = 1,

    pub fn bytesPerPixel(self: PixelFormat) u32 {
        return switch (self) {
            .rgba8 => 4,
            .a8 => 1,
        };
    }
};

pub const ShaderFormat = enum(u32) {
    msl = 1,
    hlsl = 2,
    dxbc = 3,
    dxil = 4,
    spirv = 5,
    _,
};

pub const SurfaceKind = enum(u32) {
    nsview = 1,
    hwnd = 2,
    x11 = 3,
    wayland = 4,
    _,
};

pub const MainThreadMode = enum(u32) {
    @"inline" = 0,
    deferred = 1,
    dispatch = 2,
    _,
};

pub const NativeFormat = enum(u32) {
    bgra8 = 1,
    rgba8 = 2,
};

pub const Op = enum(u8) {
    set_vertices = 1,
    new_image = 2,
    new_screen_image = 3,
    dispose_image = 4,
    dispose_shader = 5,
    dispose_surface = 6,
    write_pixels = 7,
    draw_triangles = 8,
    set_damage = 9,
    _,

    pub fn name(self: Op) []const u8 {
        return switch (self) {
            .set_vertices => "SET_VERTICES",
            .new_image => "NEW_IMAGE",
            .new_screen_image => "NEW_SCREEN_IMAGE",
            .dispose_image => "DISPOSE_IMAGE",
            .dispose_shader => "DISPOSE_SHADER",
            .dispose_surface => "DISPOSE_SURFACE",
            .write_pixels => "WRITE_PIXELS",
            .draw_triangles => "DRAW_TRIANGLES",
            .set_damage => "SET_DAMAGE",
            _ => "UNKNOWN",
        };
    }
};

// DeviceOptions.flags
pub const device_debug: u32 = 1 << 0;
pub const device_warp: u32 = 1 << 1;
pub const device_dred: u32 = 1 << 2;
pub const device_metal_no_iosurface: u32 = 1 << 3;
pub const device_low_power: u32 = 1 << 4;
pub const device_allow_dxil: u32 = 1 << 5;

// DriverCaps.flags
pub const cap_flips_y: u32 = 1 << 0;
pub const cap_supports_a8: u32 = 1 << 1;
pub const cap_needs_clearing_screen: u32 = 1 << 2;
pub const cap_direct_composition: u32 = 1 << 3;
pub const cap_unified_memory: u32 = 1 << 4;
pub const cap_kept_surfaces: u32 = 1 << 5;
pub const cap_damage_surfaces: u32 = 1 << 6;
pub const cap_shared_upload_arena: u32 = 1 << 7;

// UploadArena.flags
pub const arena_flag_gpu_mapped: u32 = 1 << 0;
pub const arena_flag_coherent: u32 = 1 << 1;

// SubmitResult.flags
pub const result_occluded: u32 = 1 << 0;
pub const result_needs_main_thread_present: u32 = 1 << 1;

// ShaderDesc.flags
pub const shader_fragment_reads_internal: u32 = 1 << 0;

// SurfaceDesc.flags
pub const surface_transparent: u32 = 1 << 0;

// SurfaceCaps.flags
pub const surface_cap_flips_y: u32 = 1 << 0;
pub const surface_cap_readable: u32 = 1 << 1;
pub const surface_cap_kept: u32 = 1 << 2;
pub const surface_cap_damage: u32 = 1 << 3;
pub const surface_cap_window_state: u32 = 1 << 4;

// Window state flags
pub const window_live_resizing: u32 = 1 << 0;
pub const window_fullscreen: u32 = 1 << 1;
pub const window_hidden: u32 = 1 << 2;

// Miscellaneous constants
pub const damage_full: u32 = 0xFFFF_FFFF;
pub const internal_uniform_dwords: u32 = 72;
pub const internal_uniform_bytes: u32 = internal_uniform_dwords * 4; // 288
pub const max_user_uniform_dwords: u32 = 16384;
pub const max_src_images: usize = 4;

pub const Rect = extern struct {
    x0: i32,
    y0: i32,
    x1: i32,
    y1: i32,

    pub fn eql(a: Rect, b: Rect) bool {
        return a.x0 == b.x0 and a.y0 == b.y0 and a.x1 == b.x1 and a.y1 == b.y1;
    }

    /// A rect known to lie inside a `w` x `h` image, as unsigned origin and extent.
    pub const Valid = struct {
        x0: u32,
        y0: u32,
        dx: u32,
        dy: u32,
    };

    /// Returns null when the rect is inverted or reaches outside `w` x `h`.
    /// Empty rects are accepted.
    pub fn validate(r: Rect, w: u32, h: u32) ?Valid {
        if (r.x0 < 0 or r.y0 < 0 or r.x1 < r.x0 or r.y1 < r.y0) return null;
        const x0: u32 = @intCast(r.x0);
        const y0: u32 = @intCast(r.y0);
        const x1: u32 = @intCast(r.x1);
        const y1: u32 = @intCast(r.y1);
        if (x1 > w or y1 > h) return null;
        return .{
            .x0 = x0,
            .y0 = y0,
            .dx = x1 - x0,
            .dy = y1 - y0,
        };
    }
};

pub const Blend = extern struct {
    src_rgb: u8,
    src_a: u8,
    dst_rgb: u8,
    dst_a: u8,
    op_rgb: u8,
    op_a: u8,

    pub fn eql(a: Blend, b: Blend) bool {
        return a.src_rgb == b.src_rgb and
            a.src_a == b.src_a and
            a.dst_rgb == b.dst_rgb and
            a.dst_a == b.dst_a and
            a.op_rgb == b.op_rgb and
            a.op_a == b.op_a;
    }
};

pub const DeviceOptions = extern struct {
    struct_size: u32,
    abi_version: u32,
    backend: u32,
    color_space: u32,
    flags: u32,
    frames_in_flight: u32,
    dx_feature_level: u32,
    main_thread_mode: u32,
    reserved: [2]u64,
};

pub const DriverCaps = extern struct {
    struct_size: u32,
    backend: u32,
    max_image_size: u32,
    flags: u32,
    shader_format: u32,
    shader_version: u32,
    color_space: u32,
    frames_in_flight: u32,
    device_name: [64]u8,
};

pub const CmdHeader = extern struct {
    op: u8,
    flags: u8,
    aux: u16,
    size: u32,
};

pub const Batch = extern struct {
    struct_size: u32,
    cmd_count: u32,
    cmds: ?[*]const u8,
    cmds_len: u64,
    payload: ?[*]const u8,
    payload_len: u64,
    sequence: u64,
};

pub const SubmitResult = extern struct {
    flags: u32,
    presented_surfaces: u32,
    completed_frames: u64,
    reserved: u64,
};

pub const ShaderStage = extern struct {
    code: ?[*]const u8,
    code_len: u64,
    entry: ?[*:0]const u8,
    entry_len: u32,
    reserved: u32,
};

pub const ShaderDesc = extern struct {
    struct_size: u32,
    format: u32,
    texture_count: u32,
    user_uniform_dwords: u32,
    flags: u32,
    reserved: u32,
    vertex: ShaderStage,
    fragment: ShaderStage,
};

pub const SurfaceDesc = extern struct {
    struct_size: u32,
    kind: u32,
    flags: u32,
    reserved: u32,
    handle1: u64,
    handle2: u64,
    width_hint: u32,
    height_hint: u32,
};

pub const SurfaceCaps = extern struct {
    struct_size: u32,
    flags: u32,
    native_format: u32,
    reserved: u32,
};

pub const ReadRegion = extern struct {
    rect: Rect,
    dst: ?[*]u8,
    dst_len: u64,
};

pub const DebugStats = extern struct {
    live_images: u64,
    live_shaders: u64,
    live_surfaces: u64,
    live_pipelines: u64,
    submits: u64,
    draws: u64,
    binds_skipped: u64,
    uploaded_bytes: u64,
};

pub const UploadArena = extern struct {
    struct_size: u32,
    arena_id: u32,
    ptr: ?[*]u8,
    capacity: u64,
    alignment: u32,
    flags: u32,
};

pub const CmdSetVertices = extern struct {
    hdr: CmdHeader,
    vertices: ?[*]const f32,
    indices: ?[*]const u32,
    vertex_float_count: u32,
    index_count: u32,
};

pub const CmdNewImage = extern struct {
    hdr: CmdHeader,
    id: u32,
    width: u32,
    height: u32,
    internal_width: u32,
    internal_height: u32,
    reserved: u32,
};

pub const CmdNewScreenImage = extern struct {
    hdr: CmdHeader,
    id: u32,
    surface_id: u32,
    width: u32,
    height: u32,
};

pub const CmdDisposeImage = extern struct {
    hdr: CmdHeader,
    id: u32,
    reserved: u32,
};

pub const CmdDisposeShader = extern struct {
    hdr: CmdHeader,
    id: u32,
    reserved: u32,
};

pub const CmdDisposeSurface = extern struct {
    hdr: CmdHeader,
    surface_id: u32,
    reserved: u32,
};

pub const PixelsRegion = extern struct {
    rect: Rect,
    payload_offset: u32,
    payload_len: u32,
};

pub const CmdWritePixels = extern struct {
    hdr: CmdHeader,
    image_id: u32,
    count: u32,
};

pub const DrawRegion = extern struct {
    rect: Rect,
    index_count: u32,
    reserved: u32,
};

pub const CmdDrawTriangles = extern struct {
    hdr: CmdHeader,
    dst_id: u32,
    shader_id: u32,
    src_ids: [4]u32,
    index_offset: u32,
    region_count: u32,
    blend: Blend,
    reserved0: u16,
    internal_version: u64,
    user_version: u64,
    internal_offset: u32,
    user_offset: u32,
    user_dwords: u32,
    reserved1: u32,
};

pub const CmdSetDamage = extern struct {
    hdr: CmdHeader,
    surface_id: u32,
    count: u32,
};

// Exhaustive comptime size and offset assertions (mirrors zig/tests/abi_check.c and zigdriver/abi_test.go).
fn checkSize(comptime T: type, comptime expected: usize) void {
    if (@sizeOf(T) != expected) {
        @compileError(std.fmt.comptimePrint(
            "ABI size mismatch for {s}: got {d}, want {d}",
            .{ @typeName(T), @sizeOf(T), expected },
        ));
    }
}

fn checkOffset(comptime T: type, comptime field: []const u8, comptime expected: usize) void {
    const actual = @offsetOf(T, field);
    if (actual != expected) {
        @compileError(std.fmt.comptimePrint(
            "ABI offset mismatch for {s}.{s}: got {d}, want {d}",
            .{ @typeName(T), field, actual, expected },
        ));
    }
}

comptime {
    checkSize(Rect, 16);
    checkOffset(Rect, "x0", 0);
    checkOffset(Rect, "y0", 4);
    checkOffset(Rect, "x1", 8);
    checkOffset(Rect, "y1", 12);

    checkSize(Blend, 6);
    checkOffset(Blend, "src_rgb", 0);
    checkOffset(Blend, "src_a", 1);
    checkOffset(Blend, "dst_rgb", 2);
    checkOffset(Blend, "dst_a", 3);
    checkOffset(Blend, "op_rgb", 4);
    checkOffset(Blend, "op_a", 5);

    checkSize(DeviceOptions, 48);
    checkOffset(DeviceOptions, "struct_size", 0);
    checkOffset(DeviceOptions, "abi_version", 4);
    checkOffset(DeviceOptions, "backend", 8);
    checkOffset(DeviceOptions, "color_space", 12);
    checkOffset(DeviceOptions, "flags", 16);
    checkOffset(DeviceOptions, "frames_in_flight", 20);
    checkOffset(DeviceOptions, "dx_feature_level", 24);
    checkOffset(DeviceOptions, "main_thread_mode", 28);
    checkOffset(DeviceOptions, "reserved", 32);

    checkSize(DriverCaps, 96);
    checkOffset(DriverCaps, "struct_size", 0);
    checkOffset(DriverCaps, "backend", 4);
    checkOffset(DriverCaps, "max_image_size", 8);
    checkOffset(DriverCaps, "flags", 12);
    checkOffset(DriverCaps, "shader_format", 16);
    checkOffset(DriverCaps, "shader_version", 20);
    checkOffset(DriverCaps, "color_space", 24);
    checkOffset(DriverCaps, "frames_in_flight", 28);
    checkOffset(DriverCaps, "device_name", 32);

    checkSize(CmdHeader, 8);
    checkOffset(CmdHeader, "op", 0);
    checkOffset(CmdHeader, "flags", 1);
    checkOffset(CmdHeader, "aux", 2);
    checkOffset(CmdHeader, "size", 4);

    checkSize(Batch, 48);
    checkOffset(Batch, "struct_size", 0);
    checkOffset(Batch, "cmd_count", 4);
    checkOffset(Batch, "cmds", 8);
    checkOffset(Batch, "cmds_len", 16);
    checkOffset(Batch, "payload", 24);
    checkOffset(Batch, "payload_len", 32);
    checkOffset(Batch, "sequence", 40);

    checkSize(SubmitResult, 24);
    checkOffset(SubmitResult, "flags", 0);
    checkOffset(SubmitResult, "presented_surfaces", 4);
    checkOffset(SubmitResult, "completed_frames", 8);
    checkOffset(SubmitResult, "reserved", 16);

    checkSize(ShaderStage, 32);
    checkOffset(ShaderStage, "code", 0);
    checkOffset(ShaderStage, "code_len", 8);
    checkOffset(ShaderStage, "entry", 16);
    checkOffset(ShaderStage, "entry_len", 24);
    checkOffset(ShaderStage, "reserved", 28);

    checkSize(ShaderDesc, 88);
    checkOffset(ShaderDesc, "struct_size", 0);
    checkOffset(ShaderDesc, "format", 4);
    checkOffset(ShaderDesc, "texture_count", 8);
    checkOffset(ShaderDesc, "user_uniform_dwords", 12);
    checkOffset(ShaderDesc, "flags", 16);
    checkOffset(ShaderDesc, "reserved", 20);
    checkOffset(ShaderDesc, "vertex", 24);
    checkOffset(ShaderDesc, "fragment", 56);

    checkSize(SurfaceDesc, 40);
    checkOffset(SurfaceDesc, "struct_size", 0);
    checkOffset(SurfaceDesc, "kind", 4);
    checkOffset(SurfaceDesc, "flags", 8);
    checkOffset(SurfaceDesc, "reserved", 12);
    checkOffset(SurfaceDesc, "handle1", 16);
    checkOffset(SurfaceDesc, "handle2", 24);
    checkOffset(SurfaceDesc, "width_hint", 32);
    checkOffset(SurfaceDesc, "height_hint", 36);

    checkSize(SurfaceCaps, 16);
    checkOffset(SurfaceCaps, "struct_size", 0);
    checkOffset(SurfaceCaps, "flags", 4);
    checkOffset(SurfaceCaps, "native_format", 8);
    checkOffset(SurfaceCaps, "reserved", 12);

    checkSize(ReadRegion, 32);
    checkOffset(ReadRegion, "rect", 0);
    checkOffset(ReadRegion, "dst", 16);
    checkOffset(ReadRegion, "dst_len", 24);

    checkSize(DebugStats, 64);
    checkOffset(DebugStats, "live_images", 0);
    checkOffset(DebugStats, "live_shaders", 8);
    checkOffset(DebugStats, "live_surfaces", 16);
    checkOffset(DebugStats, "live_pipelines", 24);
    checkOffset(DebugStats, "submits", 32);
    checkOffset(DebugStats, "draws", 40);
    checkOffset(DebugStats, "binds_skipped", 48);
    checkOffset(DebugStats, "uploaded_bytes", 56);

    checkSize(UploadArena, 32);
    checkOffset(UploadArena, "struct_size", 0);
    checkOffset(UploadArena, "arena_id", 4);
    checkOffset(UploadArena, "ptr", 8);
    checkOffset(UploadArena, "capacity", 16);
    checkOffset(UploadArena, "alignment", 24);
    checkOffset(UploadArena, "flags", 28);

    checkSize(CmdSetVertices, 32);
    checkOffset(CmdSetVertices, "hdr", 0);
    checkOffset(CmdSetVertices, "vertices", 8);
    checkOffset(CmdSetVertices, "indices", 16);
    checkOffset(CmdSetVertices, "vertex_float_count", 24);
    checkOffset(CmdSetVertices, "index_count", 28);

    checkSize(CmdNewImage, 32);
    checkOffset(CmdNewImage, "hdr", 0);
    checkOffset(CmdNewImage, "id", 8);
    checkOffset(CmdNewImage, "width", 12);
    checkOffset(CmdNewImage, "height", 16);
    checkOffset(CmdNewImage, "internal_width", 20);
    checkOffset(CmdNewImage, "internal_height", 24);
    checkOffset(CmdNewImage, "reserved", 28);

    checkSize(CmdNewScreenImage, 24);
    checkOffset(CmdNewScreenImage, "hdr", 0);
    checkOffset(CmdNewScreenImage, "id", 8);
    checkOffset(CmdNewScreenImage, "surface_id", 12);
    checkOffset(CmdNewScreenImage, "width", 16);
    checkOffset(CmdNewScreenImage, "height", 20);

    checkSize(CmdDisposeImage, 16);
    checkOffset(CmdDisposeImage, "hdr", 0);
    checkOffset(CmdDisposeImage, "id", 8);
    checkOffset(CmdDisposeImage, "reserved", 12);

    checkSize(CmdDisposeShader, 16);
    checkOffset(CmdDisposeShader, "hdr", 0);
    checkOffset(CmdDisposeShader, "id", 8);
    checkOffset(CmdDisposeShader, "reserved", 12);

    checkSize(CmdDisposeSurface, 16);
    checkOffset(CmdDisposeSurface, "hdr", 0);
    checkOffset(CmdDisposeSurface, "surface_id", 8);
    checkOffset(CmdDisposeSurface, "reserved", 12);

    checkSize(PixelsRegion, 24);
    checkOffset(PixelsRegion, "rect", 0);
    checkOffset(PixelsRegion, "payload_offset", 16);
    checkOffset(PixelsRegion, "payload_len", 20);

    checkSize(CmdWritePixels, 16);
    checkOffset(CmdWritePixels, "hdr", 0);
    checkOffset(CmdWritePixels, "image_id", 8);
    checkOffset(CmdWritePixels, "count", 12);

    checkSize(DrawRegion, 24);
    checkOffset(DrawRegion, "rect", 0);
    checkOffset(DrawRegion, "index_count", 16);
    checkOffset(DrawRegion, "reserved", 20);

    checkSize(CmdDrawTriangles, 80);
    checkOffset(CmdDrawTriangles, "hdr", 0);
    checkOffset(CmdDrawTriangles, "dst_id", 8);
    checkOffset(CmdDrawTriangles, "shader_id", 12);
    checkOffset(CmdDrawTriangles, "src_ids", 16);
    checkOffset(CmdDrawTriangles, "index_offset", 32);
    checkOffset(CmdDrawTriangles, "region_count", 36);
    checkOffset(CmdDrawTriangles, "blend", 40);
    checkOffset(CmdDrawTriangles, "reserved0", 46);
    checkOffset(CmdDrawTriangles, "internal_version", 48);
    checkOffset(CmdDrawTriangles, "user_version", 56);
    checkOffset(CmdDrawTriangles, "internal_offset", 64);
    checkOffset(CmdDrawTriangles, "user_offset", 68);
    checkOffset(CmdDrawTriangles, "user_dwords", 72);
    checkOffset(CmdDrawTriangles, "reserved1", 76);

    checkSize(CmdSetDamage, 16);
    checkOffset(CmdSetDamage, "hdr", 0);
    checkOffset(CmdSetDamage, "surface_id", 8);
    checkOffset(CmdSetDamage, "count", 12);
}

test "abi version and status constants" {
    try std.testing.expectEqual(@as(u32, 1), driver_abi_version);
    try std.testing.expectEqual(@as(i32, 0), @intFromEnum(Status.ok));
    try std.testing.expectEqual(@as(i32, 13), @intFromEnum(Status.err_leak_detected));
    try std.testing.expectEqual(@as(u32, 255), @intFromEnum(Backend.null));
    try std.testing.expectEqual(@as(u32, 4), PixelFormat.rgba8.bytesPerPixel());
    try std.testing.expectEqual(@as(u32, 1), PixelFormat.a8.bytesPerPixel());
}
