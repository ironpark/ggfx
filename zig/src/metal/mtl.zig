const std = @import("std");
const objc = @import("objc.zig");
const iosurface = @import("iosurface.zig");
const errors = @import("../errors.zig");

const Id = objc.Id;
const Class = objc.Class;
const Sel = objc.Sel;
const Diag = errors.Diag;
const Error = errors.Error;

pub const GPUFamily = enum(isize) {
    apple1 = 1001,
    apple2 = 1002,
    apple3 = 1003,
    apple4 = 1004,
    apple5 = 1005,
    apple6 = 1006,
    apple7 = 1007,
    apple8 = 1008,
    mac2 = 2002,
};

pub const FeatureSet = enum(usize) {
    mac_os_gpu_family1_v1 = 10000,
};

pub const TextureType = enum(usize) {
    type_2d = 2,
};

pub const PixelFormat = enum(u16) {
    r8_unorm = 10,
    rgba8_unorm = 70,
    rgba8_unorm_srgb = 71,
    bgra8_unorm = 80,
    bgra8_unorm_srgb = 81,
    stencil8 = 253,
};

pub const PrimitiveType = enum(usize) {
    point = 0,
    line = 1,
    line_strip = 2,
    triangle = 3,
    triangle_strip = 4,
};

pub const LoadAction = enum(usize) {
    dont_care = 0,
    load = 1,
    clear = 2,
};

pub const StoreAction = enum(usize) {
    dont_care = 0,
    store = 1,
};

pub const StorageMode = enum(usize) {
    shared = 0,
    managed = 1,
    private = 2,
    memoryless = 3,
};

pub const ResourceOptions = enum(usize) {
    storage_mode_shared = 0 << 4,
    storage_mode_managed = 1 << 4,
};

pub const IndexType = enum(usize) {
    uint16 = 0,
    uint32 = 1,
};

pub const texture_usage_shader_read: usize = 0x0001;
pub const texture_usage_shader_write: usize = 0x0002;
pub const texture_usage_render_target: usize = 0x0004;
pub const texture_usage_pixel_format_view: usize = 0x0008;

pub const BlendFactor = enum(u8) {
    zero = 0,
    one = 1,
    source_color = 2,
    one_minus_source_color = 3,
    source_alpha = 4,
    one_minus_source_alpha = 5,
    destination_color = 6,
    one_minus_destination_color = 7,
    destination_alpha = 8,
    one_minus_destination_alpha = 9,
    source_alpha_saturated = 10,
};

pub const BlendOperation = enum(u8) {
    add = 0,
    subtract = 1,
    reverse_subtract = 2,
    min = 3,
    max = 4,
};

pub const color_write_mask_all: usize = 0xf;

pub const VertexFormat = enum(usize) {
    float = 28,
    float2 = 29,
    float3 = 30,
    float4 = 31,
};

pub const CommandBufferStatus = enum(usize) {
    not_enqueued = 0,
    enqueued = 1,
    committed = 2,
    scheduled = 3,
    completed = 4,
    @"error" = 5,
    _,
};

pub const TextureSwizzle = enum(u8) {
    zero = 0,
    one = 1,
    red = 2,
    green = 3,
    blue = 4,
    alpha = 5,
};

pub const TextureSwizzleChannels = extern struct {
    red: u8,
    green: u8,
    blue: u8,
    alpha: u8,

    pub const rrrr = TextureSwizzleChannels{
        .red = @intFromEnum(TextureSwizzle.red),
        .green = @intFromEnum(TextureSwizzle.red),
        .blue = @intFromEnum(TextureSwizzle.red),
        .alpha = @intFromEnum(TextureSwizzle.red),
    };
};

pub const Origin = extern struct {
    x: usize,
    y: usize,
    z: usize,
};

pub const Size = extern struct {
    width: usize,
    height: usize,
    depth: usize,
};

pub const Region = extern struct {
    origin: Origin,
    size: Size,
};

pub const Viewport = extern struct {
    origin_x: f64,
    origin_y: f64,
    width: f64,
    height: f64,
    znear: f64,
    zfar: f64,
};

pub const ScissorRect = extern struct {
    x: usize,
    y: usize,
    width: usize,
    height: usize,
};

pub const ClearColor = extern struct {
    red: f64 = 0,
    green: f64 = 0,
    blue: f64 = 0,
    alpha: f64 = 0,
};

pub const TextureDescriptor = struct {
    texture_type: TextureType = .type_2d,
    pixel_format: PixelFormat,
    width: usize,
    height: usize,
    storage_mode: StorageMode,
    usage: usize,
};

pub const RenderPassConfig = struct {
    texture: Texture,
    load_action: LoadAction,
    store_action: StoreAction = .store,
    clear_color: ClearColor = .{},
};

const CreateSystemDefaultDeviceFn = *const fn () callconv(.c) Id;

pub fn createSystemDefaultDevice() ?Device {
    const rt = objc.ensureLoaded() orelse return null;
    var lib_metal = rt.lib_metal orelse return null;
    const create_fn = lib_metal.lookup(CreateSystemDefaultDeviceFn, "MTLCreateSystemDefaultDevice") orelse return null;
    const d = create_fn() orelse return null;

    var has_unified = false;
    if (objc.respondsToSelector(d, rt.sels.hasUnifiedMemory)) {
        has_unified = objc.send(*const fn (Id, Sel) callconv(.c) u8)(d, rt.sels.hasUnifiedMemory) != 0;
    }

    return .{
        .id = d,
        .has_unified_memory = has_unified,
    };
}

pub fn textureSwizzleAvailable() bool {
    const rt = objc.get();
    if (rt.classes.MTLTextureDescriptor == null) return false;
    const td = objc.send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.MTLTextureDescriptor, rt.sels.new);
    if (td == null) return false;
    defer objc.release(td);
    return objc.respondsToSelector(td, rt.sels.setSwizzle);
}

pub const Device = struct {
    id: Id = null,
    has_unified_memory: bool = false,

    pub inline fn release(self: Device) void {
        objc.release(self.id);
    }

    pub fn name(self: Device) []const u8 {
        const rt = objc.get();
        const ns_name = objc.send(*const fn (Id, Sel) callconv(.c) Id)(self.id, rt.sels.name);
        return objc.nsStringSlice(ns_name);
    }

    pub fn maxImageSize(self: Device) u32 {
        const rt = objc.get();
        if (objc.respondsToSelector(self.id, rt.sels.supportsFamily)) {
            if (self.supportsFamily(.apple3) or self.supportsFamily(.mac2)) {
                return 16384;
            }
            return 8192;
        }
        if (self.supportsFeatureSet(.mac_os_gpu_family1_v1)) {
            return 16384;
        }
        return 8192;
    }

    pub fn supportsFamily(self: Device, family: GPUFamily) bool {
        const rt = objc.get();
        return objc.send(*const fn (Id, Sel, isize) callconv(.c) u8)(self.id, rt.sels.supportsFamily, @intFromEnum(family)) != 0;
    }

    pub fn supportsFeatureSet(self: Device, fs: FeatureSet) bool {
        const rt = objc.get();
        return objc.send(*const fn (Id, Sel, usize) callconv(.c) u8)(self.id, rt.sels.supportsFeatureSet, @intFromEnum(fs)) != 0;
    }

    pub inline fn textureStorageMode(self: Device) StorageMode {
        return if (self.has_unified_memory) .shared else .managed;
    }

    pub inline fn bufferResourceOptions(self: Device) ResourceOptions {
        return if (self.has_unified_memory) .storage_mode_shared else .storage_mode_managed;
    }

    pub fn newCommandQueue(self: Device) CommandQueue {
        const rt = objc.get();
        return .{ .id = objc.send(*const fn (Id, Sel) callconv(.c) Id)(self.id, rt.sels.newCommandQueue) };
    }

    pub fn newBufferWithLength(self: Device, length: usize, opt: ResourceOptions) Buffer {
        const rt = objc.get();
        const id = objc.send(*const fn (Id, Sel, usize, usize) callconv(.c) Id)(
            self.id,
            rt.sels.newBufferWithLength_options,
            length,
            @intFromEnum(opt),
        );
        if (id == null) return .{};
        return .{
            .id = id,
            .length = length,
            .managed = (opt == .storage_mode_managed),
        };
    }

    pub fn newTextureWithDescriptor(self: Device, desc: TextureDescriptor) Texture {
        const rt = objc.get();
        const td = objc.send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.MTLTextureDescriptor, rt.sels.new);
        if (td == null) return .{};
        defer objc.release(td);

        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setTextureType, @intFromEnum(desc.texture_type));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setPixelFormat, @intFromEnum(desc.pixel_format));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setWidth, desc.width);
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setHeight, desc.height);
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setStorageMode, @intFromEnum(desc.storage_mode));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setUsage, desc.usage);

        return .{
            .id = objc.send(*const fn (Id, Sel, Id) callconv(.c) Id)(self.id, rt.sels.newTextureWithDescriptor, td),
        };
    }

    pub fn newTextureWithIOSurface(self: Device, desc: TextureDescriptor, surface: iosurface.IOSurfaceRef, plane: usize) Texture {
        const rt = objc.get();
        const td = objc.send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.MTLTextureDescriptor, rt.sels.new);
        if (td == null) return .{};
        defer objc.release(td);

        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setTextureType, @intFromEnum(desc.texture_type));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setPixelFormat, @intFromEnum(desc.pixel_format));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setWidth, desc.width);
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setHeight, desc.height);
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setStorageMode, @intFromEnum(desc.storage_mode));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(td, rt.sels.setUsage, desc.usage);

        return .{
            .id = objc.send(*const fn (Id, Sel, Id, iosurface.IOSurfaceRef, usize) callconv(.c) Id)(
                self.id,
                rt.sels.newTextureWithDescriptor_iosurface_plane,
                td,
                surface,
                plane,
            ),
        };
    }

    pub fn newLibraryWithSource(self: Device, source: []const u8, diag: *Diag) Error!Library {
        const rt = objc.get();
        const ns_src = objc.nsStringFromSlice(source);
        if (ns_src == null) {
            diag.set("metal: failed to create NSString for MSL source");
            return error.OutOfMemory;
        }
        defer objc.release(ns_src);

        var ns_err: Id = null;
        const lib = objc.send(*const fn (Id, Sel, Id, Id, *Id) callconv(.c) Id)(
            self.id,
            rt.sels.newLibraryWithSource_options_error,
            ns_src,
            null,
            &ns_err,
        );
        if (lib == null) {
            const desc = objc.nsErrorDescription(ns_err);
            const snippet_len = @min(source.len, 256);
            diag.print("metal: newLibraryWithSource failed: {s} (source prefix: {s})", .{ desc, source[0..snippet_len] });
            return error.ShaderCompile;
        }
        return .{ .id = lib };
    }

    pub fn newRenderPipelineState(
        self: Device,
        vs: Function,
        fs: Function,
        pix: PixelFormat,
        src_rgb: BlendFactor,
        src_a: BlendFactor,
        dst_rgb: BlendFactor,
        dst_a: BlendFactor,
        op_rgb: BlendOperation,
        op_a: BlendOperation,
        diag: *Diag,
    ) Error!RenderPipelineState {
        const rt = objc.get();
        const rpd = objc.send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.MTLRenderPipelineDescriptor, rt.sels.new);
        if (rpd == null) {
            diag.set("metal: failed to allocate MTLRenderPipelineDescriptor");
            return error.OutOfMemory;
        }
        defer objc.release(rpd);

        objc.send(*const fn (Id, Sel, Id) callconv(.c) void)(rpd, rt.sels.setVertexFunction, vs.id);
        objc.send(*const fn (Id, Sel, Id) callconv(.c) void)(rpd, rt.sels.setFragmentFunction, fs.id);

        const attachments = objc.send(*const fn (Id, Sel) callconv(.c) Id)(rpd, rt.sels.colorAttachments);
        const att0 = objc.send(*const fn (Id, Sel, usize) callconv(.c) Id)(attachments, rt.sels.objectAtIndexedSubscript, 0);

        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setPixelFormat, @intFromEnum(pix));
        objc.send(*const fn (Id, Sel, u8) callconv(.c) void)(att0, rt.sels.setBlendingEnabled, 1);
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setSourceRGBBlendFactor, @intFromEnum(src_rgb));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setSourceAlphaBlendFactor, @intFromEnum(src_a));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setDestinationRGBBlendFactor, @intFromEnum(dst_rgb));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setDestinationAlphaBlendFactor, @intFromEnum(dst_a));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setRgbBlendOperation, @intFromEnum(op_rgb));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setAlphaBlendOperation, @intFromEnum(op_a));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setWriteMask, color_write_mask_all);

        // Fixed ggfx vertex layout: buffer 0, stride 48 bytes:
        // attr 0: float2 @ 0  (dst pos)
        // attr 1: float2 @ 8  (src pos)
        // attr 2: float4 @ 16 (color)
        // attr 3: float4 @ 32 (custom)
        const vd = objc.send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.MTLVertexDescriptor, rt.sels.vertexDescriptor);
        const attrs = objc.send(*const fn (Id, Sel) callconv(.c) Id)(vd, rt.sels.attributes);
        const attr_specs = [_]struct { fmt: VertexFormat, offset: usize }{
            .{ .fmt = .float2, .offset = 0 },
            .{ .fmt = .float2, .offset = 8 },
            .{ .fmt = .float4, .offset = 16 },
            .{ .fmt = .float4, .offset = 32 },
        };
        for (attr_specs, 0..) |spec, i| {
            const attr = objc.send(*const fn (Id, Sel, usize) callconv(.c) Id)(attrs, rt.sels.objectAtIndexedSubscript, i);
            objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(attr, rt.sels.setFormat, @intFromEnum(spec.fmt));
            objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(attr, rt.sels.setOffset, spec.offset);
            objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(attr, rt.sels.setBufferIndex, 0);
        }
        const layouts = objc.send(*const fn (Id, Sel) callconv(.c) Id)(vd, rt.sels.layouts);
        const layout0 = objc.send(*const fn (Id, Sel, usize) callconv(.c) Id)(layouts, rt.sels.objectAtIndexedSubscript, 0);
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(layout0, rt.sels.setStride, 48);
        objc.send(*const fn (Id, Sel, Id) callconv(.c) void)(rpd, rt.sels.setVertexDescriptor, vd);

        var ns_err: Id = null;
        const rps = objc.send(*const fn (Id, Sel, Id, *Id) callconv(.c) Id)(
            self.id,
            rt.sels.newRenderPipelineStateWithDescriptor_error,
            rpd,
            &ns_err,
        );
        if (rps == null) {
            diag.print("metal: newRenderPipelineStateWithDescriptor failed: {s}", .{objc.nsErrorDescription(ns_err)});
            return error.ShaderCompile;
        }
        return .{ .id = rps };
    }
};

pub const CommandQueue = struct {
    id: Id = null,

    pub inline fn isNull(self: CommandQueue) bool {
        return self.id == null;
    }

    pub inline fn release(self: CommandQueue) void {
        objc.release(self.id);
    }

    /// Returns an autoreleased MTLCommandBuffer. Retain if kept beyond current pool!
    pub inline fn commandBuffer(self: CommandQueue) CommandBuffer {
        const rt = objc.get();
        return .{ .id = objc.send(*const fn (Id, Sel) callconv(.c) Id)(self.id, rt.sels.commandBuffer) };
    }
};

pub const CommandBuffer = struct {
    id: Id = null,

    pub inline fn isNull(self: CommandBuffer) bool {
        return self.id == null;
    }

    pub inline fn retain(self: CommandBuffer) void {
        _ = objc.retain(self.id);
    }

    pub inline fn release(self: CommandBuffer) void {
        objc.release(self.id);
    }

    pub inline fn status(self: CommandBuffer) CommandBufferStatus {
        const rt = objc.get();
        const raw = objc.send(*const fn (Id, Sel) callconv(.c) usize)(self.id, rt.sels.status);
        return @enumFromInt(raw);
    }

    pub inline fn presentDrawable(self: CommandBuffer, drawable_id: Id) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id) callconv(.c) void)(self.id, rt.sels.presentDrawable, drawable_id);
    }

    pub inline fn commit(self: CommandBuffer) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel) callconv(.c) void)(self.id, rt.sels.commit);
    }

    pub inline fn waitUntilCompleted(self: CommandBuffer) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel) callconv(.c) void)(self.id, rt.sels.waitUntilCompleted);
    }

    pub inline fn waitUntilScheduled(self: CommandBuffer) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel) callconv(.c) void)(self.id, rt.sels.waitUntilScheduled);
    }

    /// Creates a render command encoder (autoreleased; caller retains if stored across calls).
    pub fn renderCommandEncoder(self: CommandBuffer, cfg: RenderPassConfig) RenderCommandEncoder {
        const rt = objc.get();
        const rpd = objc.send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.MTLRenderPassDescriptor, rt.sels.new);
        if (rpd == null) return .{};
        defer objc.release(rpd);

        const attachments = objc.send(*const fn (Id, Sel) callconv(.c) Id)(rpd, rt.sels.colorAttachments);
        const att0 = objc.send(*const fn (Id, Sel, usize) callconv(.c) Id)(attachments, rt.sels.objectAtIndexedSubscript, 0);
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setLoadAction, @intFromEnum(cfg.load_action));
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(att0, rt.sels.setStoreAction, @intFromEnum(cfg.store_action));
        objc.send(*const fn (Id, Sel, Id) callconv(.c) void)(att0, rt.sels.setTexture, cfg.texture.id);
        if (cfg.load_action == .clear) {
            objc.send(*const fn (Id, Sel, ClearColor) callconv(.c) void)(att0, rt.sels.setClearColor, cfg.clear_color);
        }

        const rce = objc.send(*const fn (Id, Sel, Id) callconv(.c) Id)(
            self.id,
            rt.sels.renderCommandEncoderWithDescriptor,
            rpd,
        );
        return .{ .id = rce };
    }

    /// Creates an autoreleased blit command encoder.
    pub inline fn blitCommandEncoder(self: CommandBuffer) BlitCommandEncoder {
        const rt = objc.get();
        return .{ .id = objc.send(*const fn (Id, Sel) callconv(.c) Id)(self.id, rt.sels.blitCommandEncoder) };
    }
};

pub const RenderCommandEncoder = struct {
    id: Id = null,

    pub inline fn isNull(self: RenderCommandEncoder) bool {
        return self.id == null;
    }

    pub inline fn retain(self: RenderCommandEncoder) void {
        _ = objc.retain(self.id);
    }

    pub inline fn release(self: RenderCommandEncoder) void {
        objc.release(self.id);
    }

    pub inline fn endEncoding(self: RenderCommandEncoder) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel) callconv(.c) void)(self.id, rt.sels.endEncoding);
    }

    pub inline fn setRenderPipelineState(self: RenderCommandEncoder, rps: RenderPipelineState) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id) callconv(.c) void)(self.id, rt.sels.setRenderPipelineState, rps.id);
    }

    pub inline fn setViewport(self: RenderCommandEncoder, vp: Viewport) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Viewport) callconv(.c) void)(self.id, rt.sels.setViewport, vp);
    }

    pub inline fn setScissorRect(self: RenderCommandEncoder, rect: ScissorRect) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, ScissorRect) callconv(.c) void)(self.id, rt.sels.setScissorRect, rect);
    }

    pub inline fn setVertexBuffer(self: RenderCommandEncoder, buf: Buffer, offset: usize, index: usize) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id, usize, usize) callconv(.c) void)(
            self.id,
            rt.sels.setVertexBuffer_offset_atIndex,
            buf.id,
            offset,
            index,
        );
    }

    pub inline fn setVertexBytes(self: RenderCommandEncoder, bytes: *const anyopaque, length: usize, index: usize) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, *const anyopaque, usize, usize) callconv(.c) void)(
            self.id,
            rt.sels.setVertexBytes_length_atIndex,
            bytes,
            length,
            index,
        );
    }

    pub inline fn setFragmentBytes(self: RenderCommandEncoder, bytes: *const anyopaque, length: usize, index: usize) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, *const anyopaque, usize, usize) callconv(.c) void)(
            self.id,
            rt.sels.setFragmentBytes_length_atIndex,
            bytes,
            length,
            index,
        );
    }

    pub inline fn setFragmentBuffer(self: RenderCommandEncoder, buf: Buffer, offset: usize, index: usize) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id, usize, usize) callconv(.c) void)(
            self.id,
            rt.sels.setFragmentBuffer_offset_atIndex,
            buf.id,
            offset,
            index,
        );
    }

    pub inline fn setFragmentTexture(self: RenderCommandEncoder, tex: Texture, index: usize) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id, usize) callconv(.c) void)(
            self.id,
            rt.sels.setFragmentTexture_atIndex,
            tex.id,
            index,
        );
    }

    pub inline fn drawIndexedPrimitives(
        self: RenderCommandEncoder,
        prim_type: PrimitiveType,
        index_count: usize,
        index_type: IndexType,
        index_buffer: Buffer,
        index_buffer_offset: usize,
    ) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, usize, usize, usize, Id, usize) callconv(.c) void)(
            self.id,
            rt.sels.drawIndexedPrimitives_indexCount_indexType_indexBuffer_indexBufferOffset,
            @intFromEnum(prim_type),
            index_count,
            @intFromEnum(index_type),
            index_buffer.id,
            index_buffer_offset,
        );
    }
};

pub const BlitCommandEncoder = struct {
    id: Id = null,

    pub inline fn isNull(self: BlitCommandEncoder) bool {
        return self.id == null;
    }

    pub inline fn retain(self: BlitCommandEncoder) void {
        _ = objc.retain(self.id);
    }

    pub inline fn release(self: BlitCommandEncoder) void {
        objc.release(self.id);
    }

    pub inline fn endEncoding(self: BlitCommandEncoder) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel) callconv(.c) void)(self.id, rt.sels.endEncoding);
    }

    pub inline fn synchronizeTexture(self: BlitCommandEncoder, tex: Texture, slice: usize, level: usize) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id, usize, usize) callconv(.c) void)(
            self.id,
            rt.sels.synchronizeTexture_slice_level,
            tex.id,
            slice,
            level,
        );
    }

    pub inline fn copyFromTexture(
        self: BlitCommandEncoder,
        src_tex: Texture,
        src_slice: usize,
        src_level: usize,
        src_origin: Origin,
        src_size: Size,
        dst_tex: Texture,
        dst_slice: usize,
        dst_level: usize,
        dst_origin: Origin,
    ) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id, usize, usize, Origin, Size, Id, usize, usize, Origin) callconv(.c) void)(
            self.id,
            rt.sels.copyFromTexture_sourceSlice_sourceLevel_sourceOrigin_sourceSize_toTexture_destinationSlice_destinationLevel_destinationOrigin,
            src_tex.id,
            src_slice,
            src_level,
            src_origin,
            src_size,
            dst_tex.id,
            dst_slice,
            dst_level,
            dst_origin,
        );
    }

    pub inline fn copyFromBuffer(
        self: BlitCommandEncoder,
        src_buf: Buffer,
        src_offset: usize,
        src_bytes_per_row: usize,
        src_bytes_per_image: usize,
        src_size: Size,
        dst_tex: Texture,
        dst_slice: usize,
        dst_level: usize,
        dst_origin: Origin,
    ) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id, usize, usize, usize, Size, Id, usize, usize, Origin) callconv(.c) void)(
            self.id,
            rt.sels.copyFromBuffer_sourceOffset_sourceBytesPerRow_sourceBytesPerImage_sourceSize_toTexture_destinationSlice_destinationLevel_destinationOrigin,
            src_buf.id,
            src_offset,
            src_bytes_per_row,
            src_bytes_per_image,
            src_size,
            dst_tex.id,
            dst_slice,
            dst_level,
            dst_origin,
        );
    }
};

pub const Library = struct {
    id: Id = null,

    pub inline fn release(self: Library) void {
        objc.release(self.id);
    }

    pub fn newFunctionWithName(self: Library, fn_name: []const u8, diag: *Diag) Error!Function {
        const rt = objc.get();
        const ns_name = objc.nsStringFromSlice(fn_name);
        if (ns_name == null) {
            diag.set("metal: failed to allocate NSString for function name");
            return error.OutOfMemory;
        }
        defer objc.release(ns_name);

        const f = objc.send(*const fn (Id, Sel, Id) callconv(.c) Id)(self.id, rt.sels.newFunctionWithName, ns_name);
        if (f == null) {
            diag.print("metal: function '{s}' not found in compiled library", .{fn_name});
            return error.ShaderCompile;
        }
        return .{ .id = f };
    }
};

pub const Function = struct {
    id: Id = null,

    pub inline fn release(self: Function) void {
        objc.release(self.id);
    }
};

pub const RenderPipelineState = struct {
    id: Id = null,

    pub inline fn release(self: RenderPipelineState) void {
        objc.release(self.id);
    }
};

pub const Texture = struct {
    id: Id = null,

    pub inline fn isNull(self: Texture) bool {
        return self.id == null;
    }

    pub inline fn eql(a: Texture, b: Texture) bool {
        return a.id == b.id;
    }

    pub inline fn retain(self: Texture) void {
        _ = objc.retain(self.id);
    }

    pub inline fn release(self: Texture) void {
        objc.release(self.id);
    }

    pub inline fn width(self: Texture) u32 {
        if (self.id == null) return 0;
        const rt = objc.get();
        return @intCast(objc.send(*const fn (Id, Sel) callconv(.c) usize)(self.id, rt.sels.width));
    }

    pub inline fn height(self: Texture) u32 {
        if (self.id == null) return 0;
        const rt = objc.get();
        return @intCast(objc.send(*const fn (Id, Sel) callconv(.c) usize)(self.id, rt.sels.height));
    }

    pub fn newTextureViewWithSwizzle(self: Texture, pf: PixelFormat, tt: TextureType, swizzle: TextureSwizzleChannels) Texture {
        const rt = objc.get();
        const range = objc.NSRange{ .location = 0, .length = 1 };
        const view = objc.send(*const fn (Id, Sel, usize, usize, objc.NSRange, objc.NSRange, TextureSwizzleChannels) callconv(.c) Id)(
            self.id,
            rt.sels.newTextureViewWithPixelFormat_textureType_levels_slices_swizzle,
            @intFromEnum(pf),
            @intFromEnum(tt),
            range,
            range,
            swizzle,
        );
        return .{ .id = view };
    }

    pub inline fn getBytes(self: Texture, dst: [*]u8, bytes_per_row: usize, region: Region, mipmap_level: usize) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, [*]u8, usize, Region, usize) callconv(.c) void)(
            self.id,
            rt.sels.getBytes_bytesPerRow_fromRegion_mipmapLevel,
            dst,
            bytes_per_row,
            region,
            mipmap_level,
        );
    }
};

pub const Buffer = struct {
    id: Id = null,
    length: usize = 0,
    managed: bool = false,

    pub inline fn isNull(self: Buffer) bool {
        return self.id == null;
    }

    pub inline fn release(self: Buffer) void {
        objc.release(self.id);
    }

    pub inline fn contents(self: Buffer) ?[*]u8 {
        if (self.id == null) return null;
        const rt = objc.get();
        return objc.send(*const fn (Id, Sel) callconv(.c) ?[*]u8)(self.id, rt.sels.contents);
    }

    pub inline fn didModifyRange(self: Buffer, offset: usize, len: usize) void {
        if (!self.managed or self.id == null or len == 0) return;
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, objc.NSRange) callconv(.c) void)(
            self.id,
            rt.sels.didModifyRange,
            .{ .location = offset, .length = len },
        );
    }

    pub fn copyFromSlice(self: Buffer, data: []const u8) void {
        if (data.len == 0) return;
        const ptr = self.contents() orelse return;
        @memcpy(ptr[0..data.len], data);
        self.didModifyRange(0, data.len);
    }
};
