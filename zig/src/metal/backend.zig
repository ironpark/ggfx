const std = @import("std");
const Allocator = std.mem.Allocator;
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const handles = @import("../handles.zig");
const state_cache = @import("../state_cache.zig");
const sync = @import("../sync.zig");
const time = @import("../time.zig");
const replayer = @import("../replayer.zig");
const upload_arena = @import("../upload_arena.zig");
const pixel_regions = @import("../pixel_regions.zig");

const objc = @import("objc.zig");
const mtl = @import("mtl.zig");
const ca = @import("ca.zig");
const buffers = @import("buffers.zig");
const pipeline = @import("pipeline.zig");
const swapchain = @import("swapchain.zig");
const surface_mod = @import("surface.zig");

const Diag = errors.Diag;
const Error = errors.Error;
const Surface = surface_mod.Surface;
const MainPresentItem = surface_mod.MainPresentItem;

const max_encoders_per_command_buffer: u32 = 512;
const metal_vertex_buffer_index: usize = 0;
const metal_internal_uniform_buffer_index: usize = 1;
const metal_user_uniform_buffer_index: usize = 2;
const metal_max_inline_uniform_bytes: usize = 4096;
const housekeeping_tick_ns: u64 = 50 * std.time.ns_per_ms;

/// Copy of the live surface list, taken so Metal calls run without `surfaces_lock` held.
/// Apps have a handful of surfaces; past the inline capacity the list spills to the heap.
const SurfaceSnapshot = struct {
    stack: [16]*Surface = undefined,
    spill: std.ArrayList(*Surface) = .empty,
    len: usize = 0,

    fn take(self: *SurfaceSnapshot, be: *Backend) Error!void {
        be.surfaces_lock.lock();
        defer be.surfaces_lock.unlock();
        for (be.surfaces.items.items) |slot| {
            const s = slot orelse continue;
            if (self.len < self.stack.len) {
                self.stack[self.len] = s;
            } else {
                if (self.spill.items.len == 0) {
                    self.spill.appendSlice(be.gpa, &self.stack) catch return error.OutOfMemory;
                }
                self.spill.append(be.gpa, s) catch return error.OutOfMemory;
            }
            self.len += 1;
        }
    }

    fn items(self: *SurfaceSnapshot) []*Surface {
        return if (self.spill.items.len > 0) self.spill.items else self.stack[0..self.len];
    }

    fn deinit(self: *SurfaceSnapshot, gpa: Allocator) void {
        self.spill.deinit(gpa);
    }
};

const Image = struct {
    width: u32,
    height: u32,
    internal_width: u32,
    internal_height: u32,
    format: abi.PixelFormat,
    is_screen: bool,
    surface_id: u32,
    texture: mtl.Texture = .{},
    /// RRRR swizzled view used when sampling an A8 (R8Unorm) texture.
    sampled: mtl.Texture = .{},

    fn colorPixelFormat(self: *const Image) mtl.PixelFormat {
        if (self.is_screen) return .bgra8_unorm;
        return switch (self.format) {
            .rgba8 => .rgba8_unorm,
            .a8 => .r8_unorm,
        };
    }

    fn viewportSize(self: *const Image) [2]u32 {
        if (self.is_screen) return .{ self.width, self.height };
        return .{ self.internal_width, self.internal_height };
    }
};

pub const Backend = struct {
    gpa: Allocator,
    device: mtl.Device,
    cq: mtl.CommandQueue,
    color_space: u32,
    no_iosurface: bool,
    max_image_size: u32,
    supports_a8: bool,

    // Active command buffer & encoders (render thread only; at most one encoder active at a time).
    cb: mtl.CommandBuffer = .{},
    rce: mtl.RenderCommandEncoder = .{},
    bce: mtl.BlitCommandEncoder = .{},
    reusable_rpd: mtl.ReusableRenderPass = .{},
    encoders: u32 = 0,
    encoder_has_drawn: bool = false,
    cache: state_cache.BindCache = .{},
    last_dst_id: u32 = 0,
    last_dst_surface_id: u32 = 0,

    // Current vertex & index buffers (owned by ring's current FrameResources).
    vb: mtl.Buffer = .{},
    ib: mtl.Buffer = .{},

    // Standalone GPU-mapped shared upload arena (exclusively owned by Backend until retired).
    shared_upload_buf: mtl.Buffer = .{},
    shared_upload_ptr: ?[*]u8 = null,
    shared_arena_id: u32 = 0,

    ring: buffers.ResourceRing,
    vertex_cache: pipeline.VertexStageCache = .{},

    images: handles.HandleTable(Image) = .{},
    shaders: handles.HandleTable(pipeline.Shader) = .{},
    surfaces: handles.HandleTable(*Surface) = .{},
    surfaces_lock: sync.Mutex = .{},

    // Deferred main-thread present queue.
    // Lock order: present_exec_lock -> main_present_lock.
    present_exec_lock: sync.Mutex = .{},
    main_present_lock: sync.Mutex = .{},
    main_presents: std.ArrayList(MainPresentItem) = .empty,

    // Housekeeping thread (250ms swapchain purgeable + 500ms displaylink idle stop).
    // Sleeps without a timeout until armed by a present or waitForRefresh.
    hk_mu: sync.Mutex = .{},
    hk_cv: sync.Condition = .{},
    hk_stop: bool = false,
    hk_armed: bool = false,
    hk_thread: ?std.Thread = null,

    main_thread_mode: std.atomic.Value(u32) = std.atomic.Value(u32).init(0),
    occluded: std.atomic.Value(bool) = std.atomic.Value(bool).init(false),

    // DebugStats counters.
    live_pipelines: u64 = 0,
    submits: u64 = 0,
    draws: u64 = 0,
    binds_skipped: u64 = 0,
    uploaded_bytes: u64 = 0,
    completed_frames: u64 = 0,

    /// Optional autorelease pool hooks called by `root.zig` around every exported C entry point.
    pub fn enterAutoreleasePool() ?*anyopaque {
        return objc.autoreleasePoolPush();
    }

    pub fn leaveAutoreleasePool(pool: ?*anyopaque) void {
        objc.autoreleasePoolPop(pool);
    }

    pub fn init(gpa: Allocator, opts: *const abi.DeviceOptions, caps: *abi.DriverCaps, diag: *Diag) Error!*Backend {
        if (objc.ensureLoaded() == null) {
            diag.set("metal: failed to load Objective-C runtime");
            return error.BackendUnavailable;
        }
        const dev = mtl.createSystemDefaultDevice() orelse {
            diag.set("metal: MTLCreateSystemDefaultDevice returned nil");
            return error.BackendUnavailable;
        };
        errdefer dev.release();

        const cq = dev.newCommandQueue();
        if (cq.isNull()) {
            diag.set("metal: newCommandQueue returned nil");
            return error.BackendUnavailable;
        }
        errdefer cq.release();

        const max_img_size = dev.maxImageSize();
        const supports_a8 = mtl.textureSwizzleAvailable();
        const color_space: u32 = if (opts.color_space == 2) 2 else 1;
        const frames_in_flight: u32 = if (opts.frames_in_flight == 3) 3 else 2;

        const self = gpa.create(Backend) catch return error.OutOfMemory;
        errdefer gpa.destroy(self);

        self.* = .{
            .gpa = gpa,
            .device = dev,
            .cq = cq,
            .color_space = color_space,
            .no_iosurface = (opts.flags & abi.device_metal_no_iosurface) != 0,
            .max_image_size = max_img_size,
            .supports_a8 = supports_a8,
            .reusable_rpd = mtl.ReusableRenderPass.init(),
            .ring = buffers.ResourceRing.init(dev),
            .main_thread_mode = std.atomic.Value(u32).init(opts.main_thread_mode),
        };

        // Start per-device housekeeping thread (50ms tick for idle purge & display link stop).
        self.hk_thread = std.Thread.spawn(.{}, housekeepingMain, .{self}) catch null;

        caps.* = std.mem.zeroes(abi.DriverCaps);
        caps.struct_size = @sizeOf(abi.DriverCaps);
        caps.backend = @intFromEnum(abi.Backend.metal);
        caps.max_image_size = max_img_size;
        caps.flags = abi.cap_flips_y |
            (if (supports_a8) abi.cap_supports_a8 else 0) |
            abi.cap_kept_surfaces |
            abi.cap_damage_surfaces |
            abi.cap_shared_upload_arena |
            (if (dev.has_unified_memory) abi.cap_unified_memory else 0);
        caps.shader_format = @intFromEnum(abi.ShaderFormat.msl);
        caps.shader_version = 0;
        caps.color_space = color_space;
        caps.frames_in_flight = frames_in_flight;

        const dev_name = dev.name();
        const copy_len = @min(dev_name.len, caps.device_name.len - 1);
        if (copy_len > 0) @memcpy(caps.device_name[0..copy_len], dev_name[0..copy_len]);
        caps.device_name[copy_len] = 0;

        return self;
    }

    pub fn deinit(self: *Backend) void {
        const gpa = self.gpa;

        // 1. Stop and join housekeeping thread first so no background thread touches surfaces.
        if (self.hk_thread) |t| {
            self.hk_mu.lock();
            self.hk_stop = true;
            self.hk_cv.broadcast();
            self.hk_mu.unlock();
            t.join();
            self.hk_thread = null;
        }
        self.hk_cv.deinit();
        self.hk_mu.deinit();

        // 2. End any active encoder and commit any active command buffer so waits never stall.
        self.flushRenderCommandEncoderIfNeeded();
        self.flushBlitCommandEncoderIfNeeded();
        self.reusable_rpd.deinit();
        if (!self.cb.isNull()) {
            self.cb.commit();
            self.cb = .{};
        }

        // 3. Cancel/drain any pending main-thread presents before waiting/destroying surfaces.
        self.present_exec_lock.lock();
        self.main_present_lock.lock();
        for (self.main_presents.items) |*item| {
            item.cancel();
        }
        self.main_presents.deinit(gpa);
        self.main_present_lock.unlock();
        self.present_exec_lock.unlock();
        self.main_present_lock.deinit();
        self.present_exec_lock.deinit();

        // 4. Wait for all in-flight GPU work and release pooled/transient buffers & CBs.
        self.ring.deinit(gpa);
        if (!self.shared_upload_buf.isNull()) {
            self.shared_upload_buf.release();
            self.shared_upload_buf = .{};
            self.shared_upload_ptr = null;
        }

        // 5. Release all live images.
        for (self.images.items.items) |*slot| {
            if (slot.*) |*img| {
                if (!img.sampled.isNull()) img.sampled.release();
                if (!img.texture.isNull()) img.texture.release();
                slot.* = null;
            }
        }
        self.images.deinit(gpa);

        // 6. Release all live shaders and cached vertex stages.
        for (self.shaders.items.items) |*slot| {
            if (slot.*) |*sh| {
                _ = sh.deinit(gpa);
                slot.* = null;
            }
        }
        self.shaders.deinit(gpa);
        self.vertex_cache.deinit(gpa);

        // 7. Release all live surfaces.
        self.surfaces_lock.lock();
        for (self.surfaces.items.items) |*slot| {
            if (slot.*) |surf| {
                surf.deinit(gpa);
                gpa.destroy(surf);
                slot.* = null;
            }
        }
        self.surfaces.deinit(gpa);
        self.surfaces_lock.unlock();
        self.surfaces_lock.deinit();

        // 8. Release command queue and device.
        self.cq.release();
        self.device.release();

        gpa.destroy(self);
    }

    pub fn armHousekeeping(self: *Backend) void {
        self.hk_mu.lock();
        if (!self.hk_armed) {
            self.hk_armed = true;
            self.hk_cv.signal();
        }
        self.hk_mu.unlock();
    }

    fn housekeepingMain(self: *Backend) void {
        while (true) {
            self.hk_mu.lock();
            while (!self.hk_stop and !self.hk_armed) {
                self.hk_cv.wait(&self.hk_mu);
            }
            if (self.hk_stop) {
                self.hk_mu.unlock();
                return;
            }
            _ = self.hk_cv.timedWaitNs(&self.hk_mu, housekeeping_tick_ns);
            const stop = self.hk_stop;
            // Clear hk_armed before scanning surfaces so a concurrent present or
            // waitForRefresh re-arms the thread and cannot lose its wakeup.
            self.hk_armed = false;
            self.hk_mu.unlock();
            if (stop) return;

            const pool = objc.autoreleasePoolPush();
            defer objc.autoreleasePoolPop(pool);

            const now = time.monotonicNs();
            var pending = false;
            self.surfaces_lock.lock();
            for (self.surfaces.items.items) |slot| {
                if (slot) |surf| {
                    if (surf.tickHousekeeping(now)) {
                        pending = true;
                    }
                }
            }
            self.surfaces_lock.unlock();

            if (pending) {
                self.hk_mu.lock();
                self.hk_armed = true;
                self.hk_mu.unlock();
            }
        }
    }

    pub fn beginBatch(self: *Backend, diag: *Diag) Error!void {
        _ = diag;

        // Safety net: if caller did not call presentOnMainThread for previously queued presents,
        // drain them inline so no frames/buffers stay pinned.
        self.drainMainPresents();

        const mode = self.main_thread_mode.load(.acquire);
        var snapshot: SurfaceSnapshot = .{};
        defer snapshot.deinit(self.gpa);
        try snapshot.take(self);
        const surfs = snapshot.items();

        for (surfs) |surf| {
            surf.updatePresentationState(mode, &self.ring);
        }
    }

    pub fn abortBatch(self: *Backend) bool {
        self.flushRenderCommandEncoderIfNeeded();
        self.flushBlitCommandEncoderIfNeeded();
        if (!self.cb.isNull()) {
            // Discard uncommitted command buffer cleanly so partial commands are never sent to GPU.
            self.ring.untrackCommandBuffer(self.cb);
            self.cb = .{};
            self.encoders = 0;
        }
        self.cache.reset();
        self.vb = .{};
        self.ib = .{};
        self.last_dst_id = 0;
        self.last_dst_surface_id = 0;
        return false;
    }

    pub fn isDeviceLost(self: *const Backend) bool {
        _ = self;
        return false;
    }

    pub fn endBatch(self: *Backend, mode: abi.FlushMode, res: *abi.SubmitResult, diag: *Diag) Error!void {
        _ = diag;
        self.submits += 1;
        res.* = std.mem.zeroes(abi.SubmitResult);

        try self.flushCommandBufferIfNeeded(mode == .present, res);

        if (mode != .intermediate) {
            self.ring.frame += 1;
            self.completed_frames += 1;
        }
        self.gcBuffers();

        res.completed_frames = self.completed_frames;
        if (self.occluded.load(.acquire)) {
            res.flags |= abi.result_occluded;
        }
    }

    pub fn setVertices(self: *Backend, vertices: []const f32, indices: []const u32, diag: *Diag) Error!void {
        if (vertices.len == 0 or indices.len == 0) return;

        try self.ensureCommandBuffer();

        const v_bytes = std.mem.sliceAsBytes(vertices);
        const i_bytes = std.mem.sliceAsBytes(indices);

        const vb = self.ring.acquireBuffer(self.gpa, v_bytes.len) catch {
            diag.print("metal: allocating vertex buffer ({d} bytes) failed", .{v_bytes.len});
            return error.OutOfMemory;
        };
        vb.copyFromSlice(v_bytes);
        self.vb = vb;

        const ib = self.ring.acquireBuffer(self.gpa, i_bytes.len) catch {
            diag.print("metal: allocating index buffer ({d} bytes) failed", .{i_bytes.len});
            return error.OutOfMemory;
        };
        ib.copyFromSlice(i_bytes);
        self.ib = ib;

        self.uploaded_bytes += v_bytes.len + i_bytes.len;
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
        if (w == 0 or h == 0 or w > self.max_image_size or h > self.max_image_size) {
            diag.print("metal: invalid image size {d}x{d} (max {d})", .{ w, h, self.max_image_size });
            return error.InvalidArgument;
        }
        const tex_w = if (iw > 0) iw else w;
        const tex_h = if (ih > 0) ih else h;
        if (tex_w > self.max_image_size or tex_h > self.max_image_size) {
            diag.print("metal: invalid internal image size {d}x{d} (max {d})", .{ tex_w, tex_h, self.max_image_size });
            return error.InvalidArgument;
        }
        if (fmt == .a8 and !self.supports_a8) {
            diag.set("metal: A8 pixel format requires texture swizzle support");
            return error.Unsupported;
        }

        const pix_fmt: mtl.PixelFormat = switch (fmt) {
            .rgba8 => .rgba8_unorm,
            .a8 => .r8_unorm,
        };
        var usage: usize = mtl.texture_usage_shader_read | mtl.texture_usage_render_target;
        if (fmt == .a8) usage |= mtl.texture_usage_pixel_format_view;

        const tex = self.device.newTextureWithDescriptor(.{
            .texture_type = .type_2d,
            .pixel_format = pix_fmt,
            .width = tex_w,
            .height = tex_h,
            .storage_mode = self.device.textureStorageMode(),
            .usage = usage,
        });
        if (tex.isNull()) {
            diag.print("metal: newTextureWithDescriptor({d}x{d}) failed", .{ tex_w, tex_h });
            return error.OutOfMemory;
        }
        errdefer tex.release();

        var sampled: mtl.Texture = .{};
        if (fmt == .a8) {
            sampled = tex.newTextureViewWithSwizzle(.r8_unorm, .type_2d, mtl.TextureSwizzleChannels.rrrr);
            if (sampled.isNull()) {
                diag.print("metal: newTextureViewWithSwizzle({d}x{d}) failed", .{ tex_w, tex_h });
                return error.Internal;
            }
        }
        errdefer if (!sampled.isNull()) sampled.release();

        try self.images.put(self.gpa, id, .{
            .width = w,
            .height = h,
            .internal_width = tex_w,
            .internal_height = tex_h,
            .format = fmt,
            .is_screen = false,
            .surface_id = 0,
            .texture = tex,
            .sampled = sampled,
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
        if (w == 0 or h == 0 or w > self.max_image_size or h > self.max_image_size) {
            diag.print("metal: invalid screen image size {d}x{d}", .{ w, h });
            return error.InvalidArgument;
        }

        self.surfaces_lock.lock();
        const surf_opt = self.surfaces.getOpt(surface_id);
        if (surf_opt) |surf_ptr| {
            surf_ptr.*.setDrawableSize(w, h);
        }
        self.surfaces_lock.unlock();

        if (surf_opt == null) {
            diag.print("metal: unknown surface_id {d}", .{surface_id});
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
        });
    }

    pub fn disposeImage(self: *Backend, id: u32) Error!void {
        const img = try self.images.remove(id);
        if (self.last_dst_id == id) {
            self.flushRenderCommandEncoderIfNeeded();
        }
        if (!img.texture.isNull()) {
            self.reusable_rpd.clearTextureIfMatches(img.texture);
            self.cache.invalidateTexture(@intFromPtr(img.texture.id));
        }
        if (!img.sampled.isNull()) {
            self.cache.invalidateTexture(@intFromPtr(img.sampled.id));
        }
        if (!img.sampled.isNull()) img.sampled.release();
        if (!img.texture.isNull()) img.texture.release();
    }

    pub fn disposeShader(self: *Backend, id: u32) Error!void {
        var sh = try self.shaders.remove(id);
        const released = sh.deinit(self.gpa);
        if (self.live_pipelines >= released) {
            self.live_pipelines -= released;
        } else {
            self.live_pipelines = 0;
        }
        self.cache.pipeline = 0;
    }

    pub fn disposeSurface(self: *Backend, id: u32) Error!void {
        if (self.last_dst_surface_id == id) {
            self.flushCommandBufferIfNeeded(false, null) catch {};
        }

        // Serialize with any in-flight drainMainPresents item.execute() so `surf` cannot be freed
        // while a popped MainPresentItem is still accessing its swapchain.
        self.present_exec_lock.lock();
        defer self.present_exec_lock.unlock();

        // Cancel any deferred present items targeting this surface before destroying it.
        self.main_present_lock.lock();
        var i: usize = 0;
        while (i < self.main_presents.items.len) {
            if (self.main_presents.items[i].surface_id == id) {
                var item = self.main_presents.orderedRemove(i);
                item.cancel();
            } else {
                i += 1;
            }
        }
        self.main_present_lock.unlock();

        self.surfaces_lock.lock();
        const surf = self.surfaces.remove(id) catch |err| {
            self.surfaces_lock.unlock();
            return err;
        };
        self.surfaces_lock.unlock();

        surf.deinit(self.gpa);
        self.gpa.destroy(surf);
    }

    pub fn writePixels(
        self: *Backend,
        id: u32,
        regions: []align(4) const abi.PixelsRegion,
        payload: []const u8,
        row_pitch_align: u32,
        diag: *Diag,
    ) Error!void {
        const img = self.images.get(id) catch {
            diag.print("metal: writePixels unknown image {d}", .{id});
            return error.UnknownHandle;
        };
        if (img.is_screen) {
            diag.set("metal: writePixels cannot be called on a screen image");
            return error.InvalidArgument;
        }
        if (img.texture.isNull()) {
            diag.set("metal: writePixels on disposed texture");
            return error.UnknownHandle;
        }

        const bpp = img.format.bytesPerPixel();
        const summary = try pixel_regions.validateWriteRegions(
            regions,
            img.internal_width,
            img.internal_height,
            bpp,
            row_pitch_align,
            "metal",
            diag,
        );
        if (!summary.has_non_empty) return;

        var total_aligned: usize = 0;
        for (regions) |r| {
            if (r.payload_len > 0) {
                total_aligned += buffers.alignUploadOffset(r.payload_len);
            }
        }
        const total_raw = summary.total_raw_bytes;

        // Zero-copy fast path: payload already resides in the GPU-mapped shared upload arena.
        if (self.sharedPayloadOffset(payload)) |base_off| {
            const bce = try self.ensureBlitCommandEncoder(diag);
            if (!self.device.has_unified_memory) {
                var min_off: usize = std.math.maxInt(usize);
                var max_end: usize = 0;
                for (regions) |r| {
                    if (r.payload_len == 0) continue;
                    const r_off: usize = base_off + r.payload_offset;
                    const r_end: usize = r_off + r.payload_len;
                    if (r_off < min_off) min_off = r_off;
                    if (r_end > max_end) max_end = r_end;
                }
                if (max_end > min_off) {
                    self.shared_upload_buf.didModifyRange(min_off, max_end - min_off);
                }
            }
            for (regions) |r| {
                if (r.payload_len == 0) continue;
                copyRegionToTexture(bce, self.shared_upload_buf, base_off + r.payload_offset, bpp, row_pitch_align, r.rect, img.texture);
            }
            self.uploaded_bytes += total_raw;
            return;
        }

        // Fallback staging path: copy into per-frame upload ring buffer, sharing the active blit encoder.
        const bce = try self.ensureBlitCommandEncoder(diag);
        const alloc = self.ring.uploadSpace(self.gpa, total_aligned) catch {
            diag.print("metal: writePixels uploadSpace({d}) failed", .{total_aligned});
            return error.OutOfMemory;
        };

        const dst_base = alloc.buf.contents() orelse {
            diag.set("metal: upload buffer contents() returned null");
            return error.Internal;
        };

        var o: usize = alloc.offset;
        for (regions) |r| {
            if (r.payload_len == 0) continue;
            const src = payload[r.payload_offset .. r.payload_offset + r.payload_len];
            @memcpy(dst_base[o .. o + r.payload_len], src);
            o += buffers.alignUploadOffset(r.payload_len);
        }
        alloc.buf.didModifyRange(alloc.offset, total_aligned);

        o = alloc.offset;
        for (regions) |r| {
            if (r.payload_len == 0) continue;
            copyRegionToTexture(bce, alloc.buf, o, bpp, row_pitch_align, r.rect, img.texture);
            o += buffers.alignUploadOffset(r.payload_len);
        }

        self.uploaded_bytes += total_raw;
    }

    /// Blits pixels for `rect` from `buf` at `offset` into `texture`.
    fn copyRegionToTexture(
        bce: mtl.BlitCommandEncoder,
        buf: mtl.Buffer,
        offset: usize,
        bpp: u32,
        row_pitch_align: u32,
        rect: abi.Rect,
        texture: mtl.Texture,
    ) void {
        const dx: u32 = @intCast(rect.x1 - rect.x0);
        const dy: u32 = @intCast(rect.y1 - rect.y0);
        const bpr: usize = @intCast(pixel_regions.rowPitch(dx, bpp, row_pitch_align));
        bce.copyFromBuffer(
            buf,
            offset,
            bpr,
            bpr * @as(usize, dy),
            .{ .width = dx, .height = dy, .depth = 1 },
            texture,
            0,
            0,
            .{ .x = @intCast(rect.x0), .y = @intCast(rect.y0), .z = 0 },
        );
    }

    pub fn drawTriangles(self: *Backend, dc: *const replayer.DrawCall, diag: *Diag) Error!void {
        self.flushBlitCommandEncoderIfNeeded();

        const dst = self.images.get(dc.dst) catch {
            diag.print("metal: drawTriangles unknown dst image {d}", .{dc.dst});
            return error.UnknownHandle;
        };
        const sh = self.shaders.get(dc.shader) catch {
            diag.print("metal: drawTriangles unknown shader {d}", .{dc.shader});
            return error.UnknownHandle;
        };

        // Resolve source textures BEFORE opening dst render pass (a screen src may acquire/copy/clear).
        var src_textures: [abi.max_src_images]mtl.Texture = @splat(.{});
        for (dc.srcs, 0..) |src_id, i| {
            if (src_id == 0) continue;
            if (src_id == dc.dst) {
                diag.set("metal: destination and source images must be different");
                return error.InvalidArgument;
            }
            const src_img = self.images.get(src_id) catch {
                diag.print("metal: drawTriangles unknown src image {d}", .{src_id});
                return error.UnknownHandle;
            };
            const stex = try self.sampledTexture(src_img);
            src_textures[i] = stex;
            if (src_img.is_screen) {
                if (stex.isNull()) {
                    // No drawable available for this screen source: skip draw (matches Go).
                    return;
                }
                const surf = self.getSurfacePtr(src_img.surface_id) orelse return;
                if (!surf.cleared) {
                    self.flushRenderCommandEncoderIfNeeded();
                    const cb = try self.encoderCommandBuffer();
                    const clear_rce = self.reusable_rpd.beginEncoder(cb, stex, .clear);
                    clear_rce.endEncoding();
                    surf.cleared = true;
                }
            }
        }

        // Separate command buffers when switching between screen surfaces or offscreen <-> screen.
        const dst_surface_id: u32 = if (dst.is_screen) dst.surface_id else 0;
        if (self.last_dst_id != 0 and self.last_dst_surface_id != dst_surface_id) {
            try self.flushCommandBufferIfNeeded(false, null);
        }

        // A render pass has one destination.
        if (self.last_dst_id != dc.dst) {
            self.flushRenderCommandEncoderIfNeeded();
        }
        self.last_dst_id = dc.dst;
        self.last_dst_surface_id = dst_surface_id;

        if (self.rce.isNull()) {
            const dst_tex = try self.imageTexture(dst);
            if (dst_tex.isNull()) {
                // No drawable available for screen destination: skip draw cleanly.
                self.last_dst_id = 0;
                self.last_dst_surface_id = 0;
                return;
            }

            var load_action: mtl.LoadAction = .load;
            if (dst.is_screen) {
                if (self.getSurfacePtr(dst.surface_id)) |surf| {
                    if (!surf.cleared) {
                        load_action = .clear;
                        surf.cleared = true;
                    }
                }
            }

            const cb = try self.encoderCommandBuffer();
            const rce = self.reusable_rpd.beginEncoder(cb, dst_tex, load_action);
            if (rce.isNull()) {
                diag.set("metal: renderCommandEncoderWithDescriptor returned nil");
                return error.Internal;
            }
            // rce is autoreleased in the current ggfx_submit autorelease pool and is
            // always ended before the pool pops (via endBatch or abortBatch).
            self.rce = rce;
            self.encoder_has_drawn = false;
            self.cache.reset();
        }

        // 1. Viewport (internal size for offscreen, actual size for screen).
        const vp = dst.viewportSize();
        if (self.cache.viewport[0] == vp[0] and self.cache.viewport[1] == vp[1]) {
            self.binds_skipped += 1;
        } else {
            self.rce.setViewport(.{
                .origin_x = 0,
                .origin_y = 0,
                .width = @floatFromInt(vp[0]),
                .height = @floatFromInt(vp[1]),
                .znear = -1.0,
                .zfar = 1.0,
            });
            self.cache.viewport = vp;
        }

        // 2. Vertex buffer 0.
        if (self.vb.isNull() or self.ib.isNull()) {
            diag.set("metal: drawTriangles called without vertices/indices set");
            return error.InvalidArgument;
        }
        const vb_handle = @intFromPtr(self.vb.id);
        if (self.cache.vertex_buffer == vb_handle) {
            self.binds_skipped += 1;
        } else {
            self.rce.setVertexBuffer(self.vb, 0, metal_vertex_buffer_index);
            self.cache.vertex_buffer = vb_handle;
        }

        // 3. Internal uniform block (288 bytes): vertex only reads projection [56..72].
        const proj = dc.internal[56..72];
        if (self.cache.vertex_internal_set and std.mem.eql(u32, &self.cache.projection, proj)) {
            self.binds_skipped += 1;
        } else {
            self.rce.setVertexBytes(dc.internal, abi.internal_uniform_bytes, metal_internal_uniform_buffer_index);
            @memcpy(&self.cache.projection, proj);
            self.cache.vertex_internal_set = true;
        }

        // 4. Internal uniform block for fragment (only when shader reads it).
        if (sh.fragment_reads_internal) {
            if (self.cache.fragment_internal_version == dc.internal_version) {
                self.binds_skipped += 1;
            } else {
                self.rce.setFragmentBytes(dc.internal, abi.internal_uniform_bytes, metal_internal_uniform_buffer_index);
                self.cache.fragment_internal_version = dc.internal_version;
            }
        }

        // 5. User uniform block (<=4096B inline via setFragmentBytes, >4096B via shared arena or upload buffer).
        if (dc.user.len > 0) {
            if (self.cache.user_version == dc.user_version) {
                self.binds_skipped += 1;
            } else {
                const user_bytes = std.mem.sliceAsBytes(dc.user);
                if (user_bytes.len <= metal_max_inline_uniform_bytes) {
                    self.rce.setFragmentBytes(user_bytes.ptr, user_bytes.len, metal_user_uniform_buffer_index);
                } else if (self.sharedPayloadOffset(user_bytes)) |off| {
                    if (!self.device.has_unified_memory) {
                        self.shared_upload_buf.didModifyRange(off, user_bytes.len);
                    }
                    self.rce.setFragmentBuffer(self.shared_upload_buf, off, metal_user_uniform_buffer_index);
                    self.uploaded_bytes += user_bytes.len;
                } else {
                    const alloc = self.ring.uploadSpace(self.gpa, user_bytes.len) catch {
                        diag.print("metal: allocating user uniform upload buffer ({d} bytes) failed", .{user_bytes.len});
                        return error.OutOfMemory;
                    };
                    const ptr = alloc.buf.contents() orelse {
                        diag.set("metal: upload buffer contents() returned null");
                        return error.Internal;
                    };
                    @memcpy(ptr[alloc.offset .. alloc.offset + user_bytes.len], user_bytes);
                    alloc.buf.didModifyRange(alloc.offset, user_bytes.len);
                    self.rce.setFragmentBuffer(alloc.buf, alloc.offset, metal_user_uniform_buffer_index);
                    self.uploaded_bytes += user_bytes.len;
                }
                self.cache.user_version = dc.user_version;
            }
        }

        // 6. Fragment textures 0..3.
        for (src_textures, 0..) |tex, i| {
            const h = @intFromPtr(tex.id);
            if (self.cache.textures[i] == h) {
                if (self.encoder_has_drawn) self.binds_skipped += 1;
            } else {
                self.rce.setFragmentTexture(tex, i);
                self.cache.textures[i] = h;
            }
        }

        // 7. RenderPipelineState.
        const rps_lookup = try sh.getOrCreatePipeline(self.gpa, self.device, dst.colorPixelFormat(), dc.blend, diag);
        if (rps_lookup.created_new) {
            self.live_pipelines += 1;
        }
        const rps_h = @intFromPtr(rps_lookup.rps.id);
        if (self.cache.pipeline == rps_h) {
            self.binds_skipped += 1;
        } else {
            self.rce.setRenderPipelineState(rps_lookup.rps);
            self.cache.pipeline = rps_h;
        }

        // 8. Scissor + DrawIndexedPrimitives per region.
        const vp_w: i32 = @intCast(vp[0]);
        const vp_h: i32 = @intCast(vp[1]);
        var idx_off: usize = dc.index_offset;
        for (dc.regions) |r| {
            const x0 = @max(r.rect.x0, 0);
            const y0 = @max(r.rect.y0, 0);
            const x1 = @min(r.rect.x1, vp_w);
            const y1 = @min(r.rect.y1, vp_h);
            const clamped = abi.Rect{ .x0 = x0, .y0 = y0, .x1 = x1, .y1 = y1 };

            if (x1 > x0 and y1 > y0 and r.index_count > 0) {
                if (self.cache.scissor_valid and self.cache.scissor.eql(clamped)) {
                    self.binds_skipped += 1;
                } else {
                    self.rce.setScissorRect(.{
                        .x = @intCast(x0),
                        .y = @intCast(y0),
                        .width = @intCast(x1 - x0),
                        .height = @intCast(y1 - y0),
                    });
                    self.cache.scissor = clamped;
                    self.cache.scissor_valid = true;
                }
                self.rce.drawIndexedPrimitives(
                    .triangle,
                    r.index_count,
                    .uint32,
                    self.ib,
                    idx_off * @sizeOf(u32),
                );
                self.draws += 1;
                self.encoder_has_drawn = true;
            }
            idx_off += r.index_count;
        }
    }

    pub fn setDamage(self: *Backend, surface_id: u32, rects: ?[]align(4) const abi.Rect) Error!void {
        const surf = self.getSurfacePtr(surface_id) orelse return error.UnknownHandle;
        try surf.setDamage(self.gpa, rects);
    }

    pub fn createShader(self: *Backend, id: u32, desc: *const abi.ShaderDesc, diag: *Diag) Error!void {
        if (desc.format != @intFromEnum(abi.ShaderFormat.msl)) {
            diag.print("metal: unsupported shader format {d} (expected MSL=1)", .{desc.format});
            return error.Unsupported;
        }
        if (desc.texture_count > abi.max_src_images) {
            diag.print("metal: shader texture_count {d} > {d}", .{ desc.texture_count, abi.max_src_images });
            return error.InvalidArgument;
        }
        if (desc.vertex.code == null or desc.vertex.code_len == 0 or desc.vertex.entry == null) {
            diag.set("metal: shader vertex stage code/entry is empty");
            return error.InvalidArgument;
        }
        if (desc.fragment.code == null or desc.fragment.code_len == 0 or desc.fragment.entry == null) {
            diag.set("metal: shader fragment stage code/entry is empty");
            return error.InvalidArgument;
        }

        const vs_code = desc.vertex.code.?[0..@intCast(desc.vertex.code_len)];
        const vs_entry = if (desc.vertex.entry_len > 0)
            desc.vertex.entry.?[0..desc.vertex.entry_len]
        else
            std.mem.span(desc.vertex.entry.?);

        const fs_code = desc.fragment.code.?[0..@intCast(desc.fragment.code_len)];
        const fs_entry = if (desc.fragment.entry_len > 0)
            desc.fragment.entry.?[0..desc.fragment.entry_len]
        else
            std.mem.span(desc.fragment.entry.?);

        const vs_fn = try self.vertex_cache.getOrCompile(self.gpa, self.device, vs_code, vs_entry, diag);

        const fs_lib = try self.device.newLibraryWithSource(fs_code, diag);
        errdefer fs_lib.release();

        const fs_fn = try fs_lib.newFunctionWithName(fs_entry, diag);
        errdefer fs_fn.release();

        try self.shaders.put(self.gpa, id, .{
            .lib = fs_lib,
            .fs = fs_fn,
            .vs = vs_fn,
            .texture_count = desc.texture_count,
            .user_uniform_dwords = desc.user_uniform_dwords,
            .fragment_reads_internal = (desc.flags & abi.shader_fragment_reads_internal) != 0,
        });
    }

    pub fn readPixels(self: *Backend, id: u32, regions: []const abi.ReadRegion, diag: *Diag) Error!void {
        const img = self.images.get(id) catch {
            diag.print("metal: readPixels unknown image {d}", .{id});
            return error.UnknownHandle;
        };
        if (img.is_screen) {
            diag.set("metal: readPixels cannot be called on a screen image");
            return error.InvalidArgument;
        }
        if (img.texture.isNull()) {
            diag.set("metal: readPixels on disposed image");
            return error.UnknownHandle;
        }

        // Validate all regions up front before waiting on GPU.
        const bpp = img.format.bytesPerPixel();
        const summary = try pixel_regions.validateReadRegions(
            regions,
            img.internal_width,
            img.internal_height,
            bpp,
            "metal",
            diag,
        );
        if (!summary.has_non_empty) return;

        // Flush any open encoder and commit pending CB without presenting.
        self.flushBlitCommandEncoderIfNeeded();
        try self.flushCommandBufferIfNeeded(false, null);

        if (self.device.has_unified_memory) {
            // Shared storage on Apple Silicon / unified memory: just wait for in-flight CBs.
            self.ring.waitAllInFlight();
        } else {
            // Discrete GPU with Managed storage: synchronize texture GPU -> CPU via blit encoder.
            const cb = self.cq.commandBuffer();
            if (cb.isNull()) {
                diag.set("metal: commandBuffer returned nil in readPixels");
                return error.Internal;
            }
            const bce = cb.blitCommandEncoder();
            bce.synchronizeTexture(img.texture, 0, 0);
            bce.endEncoding();
            cb.commit();
            cb.waitUntilCompleted();
        }

        for (regions) |r| {
            const dx: usize = @intCast(r.rect.x1 - r.rect.x0);
            const dy: usize = @intCast(r.rect.y1 - r.rect.y0);
            if (dx == 0 or dy == 0) continue;
            img.texture.getBytes(
                r.dst.?,
                bpp * dx,
                .{
                    .origin = .{ .x = @intCast(r.rect.x0), .y = @intCast(r.rect.y0), .z = 0 },
                    .size = .{ .width = dx, .height = dy, .depth = 1 },
                },
                0,
            );
        }
    }

    pub fn finishForcedFrame(self: *Backend, diag: *Diag) Error!void {
        _ = diag;
        try self.flushCommandBufferIfNeeded(false, null);
        self.ring.waitAllInFlight();
        self.gcBuffers();
    }

    pub fn presentOnMainThread(self: *Backend, diag: *Diag) Error!void {
        _ = diag;
        self.drainMainPresents();
    }

    pub fn createSurface(
        self: *Backend,
        id: u32,
        desc: *const abi.SurfaceDesc,
        caps: *abi.SurfaceCaps,
        diag: *Diag,
    ) Error!void {
        if (desc.kind != @intFromEnum(abi.SurfaceKind.nsview)) {
            diag.print("metal: createSurface requires NSView kind (1), got {d}", .{desc.kind});
            return error.InvalidArgument;
        }
        if (desc.handle1 == 0) {
            diag.set("metal: createSurface handle1 (NSView*) is null");
            return error.InvalidArgument;
        }
        const ns_view: objc.Id = @ptrFromInt(@as(usize, @intCast(desc.handle1)));
        const transparent = (desc.flags & abi.surface_transparent) != 0;

        const surf = self.gpa.create(Surface) catch return error.OutOfMemory;
        errdefer self.gpa.destroy(surf);

        try surf.init(id, self.device, self.color_space, ns_view, transparent, self.no_iosurface, diag);
        errdefer surf.deinit(self.gpa);

        self.surfaces_lock.lock();
        self.surfaces.put(self.gpa, id, surf) catch |err| {
            self.surfaces_lock.unlock();
            return err;
        };
        self.surfaces_lock.unlock();

        caps.* = .{
            .struct_size = @sizeOf(abi.SurfaceCaps),
            .flags = abi.surface_cap_flips_y |
                abi.surface_cap_readable |
                abi.surface_cap_kept |
                abi.surface_cap_damage |
                abi.surface_cap_window_state,
            .native_format = @intFromEnum(abi.NativeFormat.bgra8),
            .reserved = 0,
        };
    }

    pub fn setWindowState(self: *Backend, id: u32, flags: u32) void {
        self.surfaces_lock.lock();
        const surf_opt = self.surfaces.getOpt(id);
        const surf: ?*Surface = if (surf_opt) |p| p.* else null;
        // Note: setWindowState only touches atomic fields and per-surface/swapchain mutexes.
        if (surf) |s| {
            s.setWindowState(flags);
        }
        self.surfaces_lock.unlock();
    }

    pub fn surfaceKept(self: *Backend, id: u32) bool {
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        if (self.surfaces.getOpt(id)) |p| {
            return p.*.kept();
        }
        return false;
    }

    pub fn setMainThreadMode(self: *Backend, mode: u32) void {
        self.main_thread_mode.store(mode, .release);
    }

    pub fn isOccluded(self: *Backend) bool {
        return self.occluded.load(.acquire);
    }

    pub fn acquireUploadArena(
        self: *Backend,
        min_bytes: u64,
        out_arena: *abi.UploadArena,
        diag: *Diag,
    ) Error!void {
        if (min_bytes > upload_arena.max_size) {
            diag.print("metal: requested upload arena ({d} bytes) exceeds max ({d} bytes)", .{
                min_bytes,
                upload_arena.max_size,
            });
            return error.OutOfMemory;
        }

        // Flush any open command buffer so commands referencing the previous shared arena
        // are committed and tracked before we retire the buffer.
        if (!self.cb.isNull()) {
            try self.flushCommandBufferIfNeeded(false, null);
        }

        // Allocate and map the replacement buffer before retiring the previous one so a
        // failed rotation never leaves Go holding a mapping to an already-retired buffer.
        const req: usize = @intCast(@max(min_bytes, upload_arena.default_size));
        const buf = self.ring.acquireStandaloneBuffer(self.gpa, req) catch {
            diag.print("metal: allocating shared upload arena ({d} bytes) failed", .{req});
            return error.OutOfMemory;
        };
        const ptr = buf.contents() orelse {
            buf.release();
            diag.set("metal: shared upload buffer contents() returned null");
            return error.Internal;
        };

        if (!self.shared_upload_buf.isNull()) {
            self.ring.retireSharedBuffer(self.gpa, self.shared_upload_buf);
        }
        self.shared_upload_buf = buf;
        self.shared_upload_ptr = ptr;

        out_arena.* = .{
            .struct_size = @sizeOf(abi.UploadArena),
            .arena_id = upload_arena.nextId(&self.shared_arena_id),
            .ptr = ptr,
            .capacity = buf.length,
            .alignment = buffers.upload_alignment,
            .flags = abi.arena_flag_gpu_mapped |
                (if (self.device.has_unified_memory) abi.arena_flag_coherent else 0),
            .row_pitch_alignment = 0,
            .reserved = 0,
        };
    }

    pub fn stats(self: *Backend, out: *abi.DebugStats) void {
        self.surfaces_lock.lock();
        const live_surfaces: u64 = self.surfaces.live;
        self.surfaces_lock.unlock();

        out.* = .{
            .live_images = self.images.live,
            .live_shaders = self.shaders.live,
            .live_surfaces = live_surfaces,
            .live_pipelines = self.live_pipelines,
            .submits = self.submits,
            .draws = self.draws,
            .binds_skipped = self.binds_skipped,
            .uploaded_bytes = self.uploaded_bytes,
        };
    }

    // ---- Internal helpers ----

    fn getSurfacePtr(self: *Backend, surface_id: u32) ?*Surface {
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        const p = self.surfaces.getOpt(surface_id) orelse return null;
        return p.*;
    }

    fn sampledTexture(self: *Backend, img: *const Image) Error!mtl.Texture {
        if (!img.sampled.isNull()) return img.sampled;
        return self.imageTexture(img);
    }

    fn imageTexture(self: *Backend, img: *const Image) Error!mtl.Texture {
        if (img.is_screen) {
            const surf = self.getSurfacePtr(img.surface_id) orelse return .{};
            return surf.acquireTexture(self.main_thread_mode.load(.acquire), self);
        }
        return img.texture;
    }

    pub fn gcBuffers(self: *Backend) void {
        self.ring.gc(self.gpa);
    }

    fn ensureCommandBuffer(self: *Backend) Error!void {
        if (!self.cb.isNull()) return;
        const cb = self.cq.commandBuffer();
        if (cb.isNull()) return error.Internal;
        self.cb = cb;
        self.encoders = 0;
        try self.ring.trackCommandBuffer(self.gpa, cb);
    }

    fn sharedPayloadOffset(self: *const Backend, slice: []const u8) ?usize {
        if (self.shared_upload_buf.isNull() or slice.len == 0) return null;
        return upload_arena.offsetWithin(self.shared_upload_ptr, self.shared_upload_buf.length, slice);
    }

    pub fn encoderCommandBuffer(self: *Backend) Error!mtl.CommandBuffer {
        std.debug.assert(self.bce.isNull());
        if (!self.cb.isNull() and self.encoders >= max_encoders_per_command_buffer) {
            try self.flushCommandBufferIfNeeded(false, null);
        }
        try self.ensureCommandBuffer();
        self.encoders += 1;
        return self.cb;
    }

    pub fn flushRenderCommandEncoderIfNeeded(self: *Backend) void {
        if (self.rce.isNull()) return;
        self.rce.endEncoding();
        self.rce = .{};
        self.encoder_has_drawn = false;
        self.cache.reset();
        self.last_dst_id = 0;
        self.last_dst_surface_id = 0;
    }

    pub fn flushBlitCommandEncoderIfNeeded(self: *Backend) void {
        if (self.bce.isNull()) return;
        self.bce.endEncoding();
        self.bce.release();
        self.bce = .{};
    }

    fn ensureBlitCommandEncoder(self: *Backend, diag: *Diag) Error!mtl.BlitCommandEncoder {
        self.flushRenderCommandEncoderIfNeeded();
        if (!self.bce.isNull()) return self.bce;
        const cb = try self.encoderCommandBuffer();
        const bce = cb.blitCommandEncoder();
        if (bce.isNull()) {
            diag.set("metal: blitCommandEncoder returned nil");
            return error.Internal;
        }
        bce.retain();
        self.bce = bce;
        return bce;
    }

    const DispatchSwapCtx = struct {
        sc: *swapchain.SwapChain,
        cb: mtl.CommandBuffer,
        tok: swapchain.PresentToken,
    };

    fn dispatchSwapShow(raw: ?*anyopaque) callconv(.c) void {
        const ctx: *DispatchSwapCtx = @ptrCast(@alignCast(raw.?));
        ctx.cb.waitUntilCompleted();
        ctx.sc.finishShow(ctx.tok);
    }

    const DispatchDrawableCtx = struct {
        cb: mtl.CommandBuffer,
        drawable: ca.MetalDrawable,
    };

    fn dispatchDrawableShow(raw: ?*anyopaque) callconv(.c) void {
        const ctx: *DispatchDrawableCtx = @ptrCast(@alignCast(raw.?));
        ctx.cb.waitUntilCompleted();
        ctx.drawable.present();
        ca.Transaction.flush();
    }

    fn drainMainPresents(self: *Backend) void {
        while (true) {
            self.present_exec_lock.lock();
            self.main_present_lock.lock();
            if (self.main_presents.items.len == 0) {
                self.main_present_lock.unlock();
                self.present_exec_lock.unlock();
                return;
            }
            var item = self.main_presents.orderedRemove(0);
            self.main_present_lock.unlock();
            item.execute();
            self.present_exec_lock.unlock();
        }
    }

    fn flushCommandBufferIfNeeded(self: *Backend, present: bool, out_res: ?*abi.SubmitResult) Error!void {
        self.flushBlitCommandEncoderIfNeeded();
        self.flushRenderCommandEncoderIfNeeded();
        self.reusable_rpd.clearTexture();

        if (!present) {
            if (self.cb.isNull()) return;
            const cb = self.cb;
            cb.commit();
            self.cb = .{};
            self.encoders = 0;
            return;
        }

        var snapshot: SurfaceSnapshot = .{};
        defer snapshot.deinit(self.gpa);
        try snapshot.take(self);
        const surfs = snapshot.items();

        if (self.cb.isNull()) {
            var has_pending = false;
            for (surfs) |s| {
                if (s.chained or !s.drawable.isNull()) {
                    has_pending = true;
                    break;
                }
            }
            if (!has_pending) return;
            try self.ensureCommandBuffer();
        }

        const cb = self.cb;
        const mode = self.main_thread_mode.load(.acquire);

        // Pre-commit: enqueue non-transaction CAMetalLayer drawables via presentDrawable:.
        if (present) {
            for (surfs) |s| {
                if (s.chained or s.drawable.isNull()) continue;
                if (s.layer_surf) |*ls| {
                    if (!ls.shouldPresentWithTransaction()) {
                        cb.presentDrawable(s.drawable.id);
                    }
                }
            }
        }

        cb.commit();
        self.cb = .{};
        self.encoders = 0;

        if (!present) return;

        var presented_count: u32 = 0;
        for (surfs) |s| {
            if (s.chained) {
                const sc = &(s.swap_chain.?);
                const tex = s.drawable_tex;
                s.chained = false;
                s.drawable_tex = .{};

                if (sc.beginPresent()) |tok| {
                    s.presented(self.gpa, tex);
                    presented_count += 1;

                    const live_resizing = s.live_resizing.load(.acquire);
                    if (live_resizing and mode == @intFromEnum(abi.MainThreadMode.deferred)) {
                        cb.retain();
                        self.main_present_lock.lock();
                        const push_err = self.main_presents.append(self.gpa, .{
                            .surface_id = s.id,
                            .cb = cb,
                            .kind = .{ .swap_chain = .{ .sc = sc, .tok = tok } },
                        });
                        self.main_present_lock.unlock();
                        if (push_err) |_| {
                            if (out_res) |r| r.flags |= abi.result_needs_main_thread_present;
                        } else |_| {
                            // OOM fallback: show inline so we never leak cb/tok.
                            cb.waitUntilCompleted();
                            cb.release();
                            sc.finishShow(tok);
                        }
                    } else if (live_resizing and mode == @intFromEnum(abi.MainThreadMode.dispatch)) {
                        var ctx = DispatchSwapCtx{ .sc = sc, .cb = cb, .tok = tok };
                        objc.dispatchSyncMain(&ctx, dispatchSwapShow);
                    } else {
                        cb.waitUntilScheduled();
                        sc.finishShow(tok);
                    }
                }
                continue;
            }

            if (!s.drawable.isNull()) {
                const d = s.drawable;
                const tex = s.drawable_tex;
                s.drawable = .{};
                s.drawable_tex = .{};
                s.presented(self.gpa, tex);
                presented_count += 1;

                const with_tx = if (s.layer_surf) |*ls| ls.shouldPresentWithTransaction() else false;
                if (!with_tx) {
                    // Already scheduled via cb.presentDrawable before commit.
                    d.release();
                } else {
                    const live_resizing = s.live_resizing.load(.acquire);
                    if (live_resizing and mode == @intFromEnum(abi.MainThreadMode.deferred)) {
                        cb.retain();
                        self.main_present_lock.lock();
                        const push_err = self.main_presents.append(self.gpa, .{
                            .surface_id = s.id,
                            .cb = cb,
                            .kind = .{ .metal_drawable = d },
                        });
                        self.main_present_lock.unlock();
                        if (push_err) |_| {
                            if (out_res) |r| r.flags |= abi.result_needs_main_thread_present;
                        } else |_| {
                            cb.waitUntilCompleted();
                            cb.release();
                            d.present();
                            ca.Transaction.flush();
                            d.release();
                        }
                    } else if (live_resizing and mode == @intFromEnum(abi.MainThreadMode.dispatch)) {
                        var ctx = DispatchDrawableCtx{ .cb = cb, .drawable = d };
                        objc.dispatchSyncMain(&ctx, dispatchDrawableShow);
                        d.release();
                    } else {
                        cb.waitUntilScheduled();
                        d.present();
                        ca.Transaction.flush();
                        d.release();
                    }
                }
            }
        }

        if (presented_count > 0) {
            self.armHousekeeping();
        }
        if (out_res) |r| {
            r.presented_surfaces = presented_count;
        }
    }
};
