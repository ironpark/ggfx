const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const handles = @import("../handles.zig");
const state_cache = @import("../state_cache.zig");
const sync = @import("../sync.zig");
const replayer = @import("../replayer.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

const max_image_size: u32 = 16384;

const Image = struct {
    width: u32,
    height: u32,
    internal_width: u32,
    internal_height: u32,
    format: abi.PixelFormat,
    is_screen: bool,
    surface_id: u32,
    pixels: []u8,
};

const Shader = struct {
    format: u32,
    texture_count: u32,
    user_uniform_dwords: u32,
    flags: u32,
};

const Surface = struct {
    kind: u32,
    flags: u32,
    handle1: u64,
    handle2: u64,
    window_state: u32 = 0,
    drawn: bool = false,
};

/// Reference GPU-less backend for cross-OS testing, ABI validation, and CI.
pub const Backend = struct {
    gpa: Allocator,
    images: handles.HandleTable(Image) = .{},
    shaders: handles.HandleTable(Shader) = .{},
    surfaces: handles.HandleTable(Surface) = .{},
    surfaces_lock: sync.Mutex = .{},

    cache: state_cache.BindCache = .{},
    has_drawn: bool = false,

    main_thread_mode: std.atomic.Value(u32) = std.atomic.Value(u32).init(0),
    occluded: std.atomic.Value(bool) = std.atomic.Value(bool).init(false),

    submits: u64 = 0,
    draws: u64 = 0,
    binds_skipped: u64 = 0,
    uploaded_bytes: u64 = 0,
    completed_frames: u64 = 0,

    shared_arena: []align(16) u8 = &.{},
    shared_arena_id: u32 = 0,

    pub fn init(gpa: Allocator, opts: *const abi.DeviceOptions, caps: *abi.DriverCaps, diag: *Diag) Error!*Backend {
        _ = diag;
        const self = gpa.create(Backend) catch return error.OutOfMemory;
        self.* = .{
            .gpa = gpa,
            .main_thread_mode = std.atomic.Value(u32).init(opts.main_thread_mode),
        };

        caps.* = std.mem.zeroes(abi.DriverCaps);
        caps.struct_size = @sizeOf(abi.DriverCaps);
        caps.backend = @intFromEnum(abi.Backend.null);
        caps.max_image_size = max_image_size;
        caps.flags = abi.cap_supports_a8 | abi.cap_shared_upload_arena;
        caps.shader_format = @intFromEnum(abi.ShaderFormat.spirv);
        caps.shader_version = 10;
        caps.color_space = opts.color_space;
        caps.frames_in_flight = if (opts.frames_in_flight == 3) 3 else 2;
        const name = "ggfx-null";
        @memcpy(caps.device_name[0..name.len], name);
        caps.device_name[name.len] = 0;

        return self;
    }

    pub fn deinit(self: *Backend) void {
        const gpa = self.gpa;
        for (self.images.items.items) |*slot| {
            if (slot.*) |img| {
                if (img.pixels.len > 0) gpa.free(img.pixels);
                slot.* = null;
            }
        }
        self.images.deinit(gpa);
        self.shaders.deinit(gpa);

        self.surfaces_lock.lock();
        self.surfaces.deinit(gpa);
        self.surfaces_lock.unlock();
        self.surfaces_lock.deinit();

        if (self.shared_arena.len > 0) {
            gpa.free(self.shared_arena);
            self.shared_arena = &.{};
        }

        gpa.destroy(self);
    }

    pub fn beginBatch(self: *Backend, diag: *Diag) Error!void {
        _ = diag;
        self.cache.reset();
        self.has_drawn = false;
    }

    pub fn abortBatch(self: *Backend) void {
        self.cache.reset();
        self.has_drawn = false;
    }

    pub fn endBatch(self: *Backend, mode: abi.FlushMode, res: *abi.SubmitResult, diag: *Diag) Error!void {
        _ = diag;
        self.submits += 1;
        res.* = std.mem.zeroes(abi.SubmitResult);

        switch (mode) {
            .intermediate => {},
            .end_frame => {
                self.completed_frames += 1;
            },
            .present => {
                var presented: u32 = 0;
                self.surfaces_lock.lock();
                for (self.surfaces.items.items) |*slot| {
                    if (slot.*) |*surf| {
                        if (surf.drawn) {
                            surf.drawn = false;
                            presented += 1;
                        }
                    }
                }
                self.surfaces_lock.unlock();
                self.completed_frames += 1;
                res.presented_surfaces = presented;
            },
        }
        res.completed_frames = self.completed_frames;
        if (self.occluded.load(.acquire)) {
            res.flags |= abi.result_occluded;
        }
    }

    pub fn setVertices(self: *Backend, vertices: []const f32, indices: []const u32, diag: *Diag) Error!void {
        _ = diag;
        const vbytes: u64 = @as(u64, vertices.len) * @sizeOf(f32);
        const ibytes: u64 = @as(u64, indices.len) * @sizeOf(u32);
        self.uploaded_bytes += vbytes + ibytes;
        const vptr: usize = if (vertices.len > 0) @intFromPtr(vertices.ptr) else 0;
        self.cache.vertex_buffer = vptr;
    }

    pub fn newImage(
        self: *Backend,
        id: u32,
        w: u32,
        h: u32,
        iw: u32,
        ih: u32,
        fmt: abi.PixelFormat,
        diag: *Diag,
    ) Error!void {
        if (w == 0 or h == 0 or w > max_image_size or h > max_image_size) {
            diag.print("invalid image dimensions {d}x{d}", .{ w, h });
            return error.InvalidArgument;
        }
        const internal_w = if (iw > 0) iw else w;
        const internal_h = if (ih > 0) ih else h;
        if (internal_w > max_image_size or internal_h > max_image_size) {
            diag.print("invalid internal image dimensions {d}x{d}", .{ internal_w, internal_h });
            return error.InvalidArgument;
        }
        const bpp: u64 = fmt.bytesPerPixel();
        const byte_len_u64 = @as(u64, internal_w) * @as(u64, internal_h) * bpp;
        const byte_len = std.math.cast(usize, byte_len_u64) orelse return error.OutOfMemory;

        const pixels = self.gpa.alloc(u8, byte_len) catch return error.OutOfMemory;
        errdefer self.gpa.free(pixels);
        @memset(pixels, 0);

        try self.images.put(self.gpa, id, .{
            .width = w,
            .height = h,
            .internal_width = internal_w,
            .internal_height = internal_h,
            .format = fmt,
            .is_screen = false,
            .surface_id = 0,
            .pixels = pixels,
        });
    }

    pub fn newScreenImage(
        self: *Backend,
        id: u32,
        surface_id: u32,
        w: u32,
        h: u32,
        diag: *Diag,
    ) Error!void {
        if (w == 0 or h == 0 or w > max_image_size or h > max_image_size) {
            diag.print("invalid screen image dimensions {d}x{d}", .{ w, h });
            return error.InvalidArgument;
        }
        self.surfaces_lock.lock();
        const has_surf = self.surfaces.getOpt(surface_id) != null;
        self.surfaces_lock.unlock();
        if (!has_surf) {
            diag.print("unknown surface_id {d}", .{surface_id});
            return error.UnknownHandle;
        }

        try self.images.put(self.gpa, id, .{
            .width = w,
            .height = h,
            .internal_width = w,
            .internal_height = h,
            .format = .rgba8,
            .is_screen = true,
            .surface_id = surface_id,
            .pixels = &.{},
        });
    }

    pub fn disposeImage(self: *Backend, id: u32) Error!void {
        const img = try self.images.remove(id);
        if (img.pixels.len > 0) self.gpa.free(img.pixels);
        self.cache.reset();
        self.has_drawn = false;
    }

    pub fn disposeShader(self: *Backend, id: u32) Error!void {
        _ = try self.shaders.remove(id);
        self.cache.pipeline = 0;
    }

    pub fn disposeSurface(self: *Backend, id: u32) Error!void {
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        _ = try self.surfaces.remove(id);
    }

    pub fn writePixels(
        self: *Backend,
        id: u32,
        regions: []align(4) const abi.PixelsRegion,
        payload: []const u8,
        diag: *Diag,
    ) Error!void {
        const img = try self.images.get(id);
        if (img.is_screen) {
            diag.set("cannot write pixels to a screen image");
            return error.InvalidArgument;
        }
        const bpp: usize = img.format.bytesPerPixel();
        const img_stride: usize = @as(usize, img.internal_width) * bpp;

        for (regions) |r| {
            const bounds = validateRect(r.rect, img.internal_width, img.internal_height) orelse {
                diag.print("writePixels rect ({d},{d})-({d},{d}) out of image bounds {d}x{d}", .{
                    r.rect.x0, r.rect.y0, r.rect.x1, r.rect.y1, img.internal_width, img.internal_height,
                });
                return error.InvalidArgument;
            };
            const expected_len = @as(u64, bounds.dx) * @as(u64, bounds.dy) * bpp;
            if (r.payload_len != expected_len) {
                diag.print("writePixels payload_len {d} != expected {d}", .{ r.payload_len, expected_len });
                return error.InvalidArgument;
            }
            const src = payload[r.payload_offset .. r.payload_offset + r.payload_len];
            const row_bytes: usize = @as(usize, bounds.dx) * bpp;
            var row: usize = 0;
            while (row < bounds.dy) : (row += 1) {
                const dst_off = (@as(usize, bounds.y0) + row) * img_stride + @as(usize, bounds.x0) * bpp;
                const src_off = row * row_bytes;
                @memcpy(img.pixels[dst_off .. dst_off + row_bytes], src[src_off .. src_off + row_bytes]);
            }
            self.uploaded_bytes += r.payload_len;
        }
    }

    pub fn drawTriangles(self: *Backend, dc: *const replayer.DrawCall, diag: *Diag) Error!void {
        const dst = self.images.get(dc.dst) catch {
            diag.print("drawTriangles: unknown dst image {d}", .{dc.dst});
            return error.UnknownHandle;
        };
        const shader = self.shaders.get(dc.shader) catch {
            diag.print("drawTriangles: unknown shader {d}", .{dc.shader});
            return error.UnknownHandle;
        };
        for (dc.srcs) |src_id| {
            if (src_id != 0) {
                _ = self.images.get(src_id) catch {
                    diag.print("drawTriangles: unknown src image {d}", .{src_id});
                    return error.UnknownHandle;
                };
            }
        }

        if (dst.is_screen) {
            self.surfaces_lock.lock();
            if (self.surfaces.getOpt(dst.surface_id)) |surf| {
                surf.drawn = true;
            }
            self.surfaces_lock.unlock();
        }

        // Track bind cache hits to exercise and verify state caching behavior.
        const pipe_key = packPipelineKey(dc.shader, dc.blend);
        if (self.cache.pipeline == pipe_key) {
            self.binds_skipped += 1;
        } else {
            self.cache.pipeline = pipe_key;
        }

        const proj = dc.internal[56..72];
        if (self.cache.vertex_internal_set and std.mem.eql(u32, &self.cache.projection, proj)) {
            self.binds_skipped += 1;
        } else {
            @memcpy(&self.cache.projection, proj);
            self.cache.vertex_internal_set = true;
        }

        if ((shader.flags & abi.shader_fragment_reads_internal) != 0) {
            if (self.cache.fragment_internal_version == dc.internal_version) {
                self.binds_skipped += 1;
            } else {
                self.cache.fragment_internal_version = dc.internal_version;
            }
        }

        if (dc.user.len > 0) {
            if (self.cache.user_version == dc.user_version) {
                self.binds_skipped += 1;
            } else {
                self.cache.user_version = dc.user_version;
            }
        }

        for (dc.srcs, 0..) |src_id, i| {
            const h: usize = src_id;
            if (self.has_drawn and self.cache.textures[i] == h) {
                self.binds_skipped += 1;
            } else {
                self.cache.textures[i] = h;
            }
        }

        for (dc.regions) |r| {
            if (self.cache.scissor_valid and self.cache.scissor.eql(r.rect)) {
                self.binds_skipped += 1;
            } else {
                self.cache.scissor = r.rect;
                self.cache.scissor_valid = true;
            }
            self.draws += 1;
        }
        self.has_drawn = true;
    }

    pub fn setDamage(self: *Backend, surface_id: u32, rects: ?[]align(4) const abi.Rect) Error!void {
        _ = rects;
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        _ = try self.surfaces.get(surface_id);
    }

    pub fn createShader(self: *Backend, id: u32, desc: *const abi.ShaderDesc, diag: *Diag) Error!void {
        if (desc.texture_count > abi.max_src_images) {
            diag.print("shader texture_count {d} > {d}", .{ desc.texture_count, abi.max_src_images });
            return error.InvalidArgument;
        }
        if (desc.vertex.code_len == 0 or desc.vertex.code == null) {
            diag.set("shader vertex stage code is empty");
            return error.InvalidArgument;
        }
        if (desc.fragment.code_len == 0 or desc.fragment.code == null) {
            diag.set("shader fragment stage code is empty");
            return error.InvalidArgument;
        }
        if (desc.vertex.entry == null or desc.fragment.entry == null) {
            diag.set("shader stage entry point is null");
            return error.InvalidArgument;
        }

        try self.shaders.put(self.gpa, id, .{
            .format = desc.format,
            .texture_count = desc.texture_count,
            .user_uniform_dwords = desc.user_uniform_dwords,
            .flags = desc.flags,
        });
    }

    pub fn readPixels(
        self: *Backend,
        id: u32,
        regions: []const abi.ReadRegion,
        diag: *Diag,
    ) Error!void {
        const img = try self.images.get(id);
        if (img.is_screen) {
            diag.set("cannot read pixels from a screen image");
            return error.InvalidArgument;
        }
        const bpp: usize = img.format.bytesPerPixel();
        const img_stride: usize = @as(usize, img.internal_width) * bpp;

        for (regions) |r| {
            const bounds = validateRect(r.rect, img.internal_width, img.internal_height) orelse {
                diag.print("readPixels rect ({d},{d})-({d},{d}) out of image bounds {d}x{d}", .{
                    r.rect.x0, r.rect.y0, r.rect.x1, r.rect.y1, img.internal_width, img.internal_height,
                });
                return error.InvalidArgument;
            };
            const expected_len = @as(u64, bounds.dx) * @as(u64, bounds.dy) * bpp;
            if (r.dst_len != expected_len) {
                diag.print("readPixels dst_len {d} != expected {d}", .{ r.dst_len, expected_len });
                return error.InvalidArgument;
            }
            if (expected_len == 0) continue;
            const dst = r.dst orelse {
                diag.set("readPixels dst pointer is null");
                return error.InvalidArgument;
            };
            const row_bytes: usize = @as(usize, bounds.dx) * bpp;
            var row: usize = 0;
            while (row < bounds.dy) : (row += 1) {
                const src_off = (@as(usize, bounds.y0) + row) * img_stride + @as(usize, bounds.x0) * bpp;
                const dst_off = row * row_bytes;
                @memcpy(dst[dst_off .. dst_off + row_bytes], img.pixels[src_off .. src_off + row_bytes]);
            }
        }
    }

    pub fn finishForcedFrame(self: *Backend, diag: *Diag) Error!void {
        _ = self;
        _ = diag;
    }

    pub fn presentOnMainThread(self: *Backend, diag: *Diag) Error!void {
        _ = self;
        _ = diag;
    }

    pub fn createSurface(
        self: *Backend,
        id: u32,
        desc: *const abi.SurfaceDesc,
        caps: *abi.SurfaceCaps,
        diag: *Diag,
    ) Error!void {
        switch (@as(abi.SurfaceKind, @enumFromInt(desc.kind))) {
            .nsview, .hwnd, .x11, .wayland => {},
            _ => {
                diag.print("unknown surface kind {d}", .{desc.kind});
                return error.InvalidArgument;
            },
        }
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        try self.surfaces.put(self.gpa, id, .{
            .kind = desc.kind,
            .flags = desc.flags,
            .handle1 = desc.handle1,
            .handle2 = desc.handle2,
        });

        caps.* = .{
            .struct_size = @sizeOf(abi.SurfaceCaps),
            .flags = 0,
            .native_format = @intFromEnum(abi.NativeFormat.rgba8),
            .reserved = 0,
        };
    }

    pub fn setWindowState(self: *Backend, id: u32, flags: u32) void {
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        if (self.surfaces.getOpt(id)) |surf| {
            surf.window_state = flags;
        }
    }

    pub fn surfaceKept(self: *Backend, id: u32) bool {
        _ = self;
        _ = id;
        return false;
    }

    pub fn setMainThreadMode(self: *Backend, mode: u32) void {
        self.main_thread_mode.store(mode, .release);
    }

    pub fn isOccluded(self: *Backend) bool {
        return self.occluded.load(.acquire);
    }

    pub fn stats(self: *Backend, out: *abi.DebugStats) void {
        self.surfaces_lock.lock();
        const live_surfaces: u64 = self.surfaces.live;
        self.surfaces_lock.unlock();

        out.* = .{
            .live_images = self.images.live,
            .live_shaders = self.shaders.live,
            .live_surfaces = live_surfaces,
            .live_pipelines = 0,
            .submits = self.submits,
            .draws = self.draws,
            .binds_skipped = self.binds_skipped,
            .uploaded_bytes = self.uploaded_bytes,
        };
    }

    pub fn acquireUploadArena(
        self: *Backend,
        min_bytes: u64,
        out_arena: *abi.UploadArena,
        diag: *Diag,
    ) Error!void {
        const max_cap: u64 = 256 << 20;
        const default_cap: u64 = 256 << 10;
        if (min_bytes > max_cap) {
            diag.print("null: acquireUploadArena min_bytes {d} exceeds max {d}", .{ min_bytes, max_cap });
            return error.OutOfMemory;
        }
        const raw_want: usize = @intCast(@max(min_bytes, default_cap));
        const want = (raw_want + 15) & ~@as(usize, 15);
        if (self.shared_arena.len < want) {
            const next = self.gpa.alignedAlloc(u8, .@"16", want) catch return error.OutOfMemory;
            if (self.shared_arena.len > 0) self.gpa.free(self.shared_arena);
            self.shared_arena = next;
        }
        self.shared_arena_id +%= 1;
        if (self.shared_arena_id == 0) self.shared_arena_id = 1;
        out_arena.* = .{
            .struct_size = @sizeOf(abi.UploadArena),
            .arena_id = self.shared_arena_id,
            .ptr = self.shared_arena.ptr,
            .capacity = self.shared_arena.len,
            .alignment = 16,
            .flags = abi.arena_flag_coherent,
        };
    }
};

const ValidRect = struct {
    x0: u32,
    y0: u32,
    dx: u32,
    dy: u32,
};

fn validateRect(r: abi.Rect, w: u32, h: u32) ?ValidRect {
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

fn packPipelineKey(shader_id: u32, blend: abi.Blend) usize {
    var h: u64 = shader_id;
    h = (h << 8) | blend.src_rgb;
    h = (h << 8) | blend.src_a;
    h = (h << 8) | blend.dst_rgb;
    h = (h << 8) | blend.dst_a;
    h ^= (@as(u64, blend.op_rgb) << 40) | (@as(u64, blend.op_a) << 48);
    // Reserve 0 for "unbound".
    return if (h == 0) 1 else @intCast(h);
}

/// Temporary delegating stub for OS backends prior to Wave 3B implementation.
/// Delegates all operations to the null backend and overrides `caps.backend`.
pub fn Stub(comptime backend_tag: abi.Backend) type {
    return struct {
        const Self = @This();

        inner: *Backend,
        gpa: Allocator,

        pub fn init(gpa: Allocator, opts: *const abi.DeviceOptions, caps: *abi.DriverCaps, diag: *Diag) Error!*Self {
            const inner = try Backend.init(gpa, opts, caps, diag);
            errdefer inner.deinit();
            const self = gpa.create(Self) catch return error.OutOfMemory;
            self.* = .{ .inner = inner, .gpa = gpa };
            caps.backend = @intFromEnum(backend_tag);
            return self;
        }

        pub fn deinit(self: *Self) void {
            const gpa = self.gpa;
            self.inner.deinit();
            gpa.destroy(self);
        }

        pub fn beginBatch(self: *Self, diag: *Diag) Error!void {
            return self.inner.beginBatch(diag);
        }
        pub fn endBatch(self: *Self, mode: abi.FlushMode, res: *abi.SubmitResult, diag: *Diag) Error!void {
            return self.inner.endBatch(mode, res, diag);
        }
        pub fn setVertices(self: *Self, vertices: []const f32, indices: []const u32, diag: *Diag) Error!void {
            return self.inner.setVertices(vertices, indices, diag);
        }
        pub fn newImage(self: *Self, id: u32, w: u32, h: u32, iw: u32, ih: u32, fmt: abi.PixelFormat, diag: *Diag) Error!void {
            return self.inner.newImage(id, w, h, iw, ih, fmt, diag);
        }
        pub fn newScreenImage(self: *Self, id: u32, surface_id: u32, w: u32, h: u32, diag: *Diag) Error!void {
            return self.inner.newScreenImage(id, surface_id, w, h, diag);
        }
        pub fn disposeImage(self: *Self, id: u32) Error!void {
            return self.inner.disposeImage(id);
        }
        pub fn disposeShader(self: *Self, id: u32) Error!void {
            return self.inner.disposeShader(id);
        }
        pub fn disposeSurface(self: *Self, id: u32) Error!void {
            return self.inner.disposeSurface(id);
        }
        pub fn writePixels(self: *Self, id: u32, regions: []align(4) const abi.PixelsRegion, payload: []const u8, diag: *Diag) Error!void {
            return self.inner.writePixels(id, regions, payload, diag);
        }
        pub fn drawTriangles(self: *Self, dc: *const replayer.DrawCall, diag: *Diag) Error!void {
            return self.inner.drawTriangles(dc, diag);
        }
        pub fn setDamage(self: *Self, surface_id: u32, rects: ?[]align(4) const abi.Rect) Error!void {
            return self.inner.setDamage(surface_id, rects);
        }
        pub fn createShader(self: *Self, id: u32, desc: *const abi.ShaderDesc, diag: *Diag) Error!void {
            return self.inner.createShader(id, desc, diag);
        }
        pub fn readPixels(self: *Self, id: u32, regions: []const abi.ReadRegion, diag: *Diag) Error!void {
            return self.inner.readPixels(id, regions, diag);
        }
        pub fn finishForcedFrame(self: *Self, diag: *Diag) Error!void {
            return self.inner.finishForcedFrame(diag);
        }
        pub fn presentOnMainThread(self: *Self, diag: *Diag) Error!void {
            return self.inner.presentOnMainThread(diag);
        }
        pub fn createSurface(self: *Self, id: u32, desc: *const abi.SurfaceDesc, caps: *abi.SurfaceCaps, diag: *Diag) Error!void {
            return self.inner.createSurface(id, desc, caps, diag);
        }
        pub fn setWindowState(self: *Self, id: u32, flags: u32) void {
            self.inner.setWindowState(id, flags);
        }
        pub fn surfaceKept(self: *Self, id: u32) bool {
            return self.inner.surfaceKept(id);
        }
        pub fn setMainThreadMode(self: *Self, mode: u32) void {
            self.inner.setMainThreadMode(mode);
        }
        pub fn isOccluded(self: *Self) bool {
            return self.inner.isOccluded();
        }
        pub fn stats(self: *Self, out: *abi.DebugStats) void {
            self.inner.stats(out);
        }
    };
}

test "null backend writePixels and readPixels roundtrip (RGBA8 and A8) plus bind cache" {
    const gpa = std.testing.allocator;
    var diag = Diag.none();
    const opts = abi.DeviceOptions{
        .struct_size = @sizeOf(abi.DeviceOptions),
        .abi_version = abi.driver_abi_version,
        .backend = @intFromEnum(abi.Backend.null),
        .color_space = 1,
        .flags = 0,
        .frames_in_flight = 2,
        .dx_feature_level = 0,
        .main_thread_mode = 0,
        .reserved = .{ 0, 0 },
    };
    var caps: abi.DriverCaps = undefined;
    const be = try Backend.init(gpa, &opts, &caps, &diag);
    defer be.deinit();

    try std.testing.expectEqual(@as(u32, @intFromEnum(abi.Backend.null)), caps.backend);
    try std.testing.expect((caps.flags & abi.cap_supports_a8) != 0);

    // Create RGBA8 4x4 image and A8 4x4 image.
    try be.newImage(1, 4, 4, 16, 16, .rgba8, &diag);
    try be.newImage(2, 4, 4, 16, 16, .a8, &diag);

    // Write 2x2 subrect at (1, 1) in RGBA8.
    var payload: [32]u8 align(16) = undefined;
    for (&payload, 0..) |*b, i| b.* = @intCast(i + 1);
    const wr = [_]abi.PixelsRegion{.{
        .rect = .{ .x0 = 1, .y0 = 1, .x1 = 3, .y1 = 3 },
        .payload_offset = 0,
        .payload_len = 16,
    }};
    try be.writePixels(1, &wr, &payload, &diag);

    var read_buf: [16]u8 = @splat(0);
    const rr = [_]abi.ReadRegion{.{
        .rect = .{ .x0 = 1, .y0 = 1, .x1 = 3, .y1 = 3 },
        .dst = &read_buf,
        .dst_len = read_buf.len,
    }};
    try be.readPixels(1, &rr, &diag);
    try std.testing.expectEqualSlices(u8, payload[0..16], &read_buf);

    // Also verify write/read within internal_width x internal_height padding (beyond logical 4x4).
    const pad_wr = [_]abi.PixelsRegion{.{
        .rect = .{ .x0 = 4, .y0 = 4, .x1 = 6, .y1 = 6 },
        .payload_offset = 0,
        .payload_len = 16,
    }};
    try be.writePixels(1, &pad_wr, &payload, &diag);
    var pad_read: [16]u8 = @splat(0);
    const pad_rr = [_]abi.ReadRegion{.{
        .rect = .{ .x0 = 4, .y0 = 4, .x1 = 6, .y1 = 6 },
        .dst = &pad_read,
        .dst_len = pad_read.len,
    }};
    try be.readPixels(1, &pad_rr, &diag);
    try std.testing.expectEqualSlices(u8, payload[0..16], &pad_read);

    // Write & read A8 2x2 subrect.
    const a8_wr = [_]abi.PixelsRegion{.{
        .rect = .{ .x0 = 0, .y0 = 2, .x1 = 2, .y1 = 4 },
        .payload_offset = 16,
        .payload_len = 4,
    }};
    try be.writePixels(2, &a8_wr, &payload, &diag);

    var a8_read: [4]u8 = @splat(0);
    const a8_rr = [_]abi.ReadRegion{.{
        .rect = .{ .x0 = 0, .y0 = 2, .x1 = 2, .y1 = 4 },
        .dst = &a8_read,
        .dst_len = a8_read.len,
    }};
    try be.readPixels(2, &a8_rr, &diag);
    try std.testing.expectEqualSlices(u8, payload[16..20], &a8_read);

    // Create shader and perform two identical draws to verify binds_skipped increments.
    const vs_code = "vs";
    const fs_code = "fs";
    const shader_desc = abi.ShaderDesc{
        .struct_size = @sizeOf(abi.ShaderDesc),
        .format = @intFromEnum(abi.ShaderFormat.spirv),
        .texture_count = 1,
        .user_uniform_dwords = 4,
        .flags = abi.shader_fragment_reads_internal,
        .reserved = 0,
        .vertex = .{ .code = vs_code.ptr, .code_len = vs_code.len, .entry = "main", .entry_len = 4, .reserved = 0 },
        .fragment = .{ .code = fs_code.ptr, .code_len = fs_code.len, .entry = "main", .entry_len = 4, .reserved = 0 },
    };
    try be.createShader(10, &shader_desc, &diag);

    var internal_u: [abi.internal_uniform_dwords]u32 = @splat(0);
    internal_u[56] = 0x3f800000;
    const user_u = [_]u32{ 1, 2, 3, 4 };
    const regions = [_]abi.DrawRegion{.{
        .rect = .{ .x0 = 0, .y0 = 0, .x1 = 4, .y1 = 4 },
        .index_count = 6,
        .reserved = 0,
    }};
    const dc = replayer.DrawCall{
        .dst = 1,
        .srcs = .{ 2, 0, 0, 0 },
        .shader = 10,
        .index_offset = 0,
        .blend = .{ .src_rgb = 1, .src_a = 1, .dst_rgb = 5, .dst_a = 5, .op_rgb = 0, .op_a = 0 },
        .internal = &internal_u,
        .internal_version = 1,
        .user = &user_u,
        .user_version = 1,
        .regions = &regions,
    };

    try be.beginBatch(&diag);
    try be.drawTriangles(&dc, &diag);
    var st1: abi.DebugStats = undefined;
    be.stats(&st1);
    try std.testing.expectEqual(@as(u64, 0), st1.binds_skipped);

    try be.drawTriangles(&dc, &diag);
    var st2: abi.DebugStats = undefined;
    be.stats(&st2);
    try std.testing.expect(st2.binds_skipped > 0);
    try std.testing.expectEqual(@as(u64, 2), st2.draws);

    try be.disposeImage(1);
    try be.disposeImage(2);
    try be.disposeShader(10);
    be.stats(&st2);
    try std.testing.expectEqual(@as(u64, 0), st2.live_images);
    try std.testing.expectEqual(@as(u64, 0), st2.live_shaders);
}
