const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const handles = @import("../handles.zig");
const state_cache = @import("../state_cache.zig");
const sync = @import("../sync.zig");
const replayer = @import("../replayer.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const instance_mod = @import("instance.zig");
const device_mod = @import("device.zig");
const memory_mod = @import("memory.zig");
const descriptors_mod = @import("descriptors.zig");
const renderpass_mod = @import("renderpass.zig");
const pipeline_mod = @import("pipeline.zig");
const swapchain_mod = @import("swapchain.zig");
const upload_mod = @import("upload.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

const max_frames_in_flight: usize = 3;

const Image = struct {
    width: u32,
    height: u32,
    internal_width: u32,
    internal_height: u32,
    format: abi.PixelFormat,
    vk_format: vk.Format,
    is_screen: bool,
    surface_id: u32,
    vk_image: vk.Image = .null_handle,
    alloc: memory_mod.Allocation = .{},
    sample_view: vk.ImageView = .null_handle,
    rt_view: vk.ImageView = .null_handle, // always identity swizzle; equals sample_view for RGBA8
    framebuffer: vk.Framebuffer = .null_handle,
    layout: vk.ImageLayout = .undefined,
};

const DestroyedImage = struct {
    vk_image: vk.Image,
    alloc: memory_mod.Allocation,
    sample_view: vk.ImageView,
    rt_view: vk.ImageView,
    framebuffer: vk.Framebuffer,
};

const CmdEntry = struct {
    cb: vk.CommandBuffer,
    fence: vk.Fence,
    submitted: bool = false,
};

const FrameSlot = struct {
    cmd_pool: vk.CommandPool = .null_handle,
    cmd_entries: std.ArrayList(CmdEntry) = .empty,
    cmd_used: usize = 0,
    upload: upload_mod.UploadRing = .{},
    desc_pool: descriptors_mod.DescriptorPoolRing = .{},
    deferred_images: std.ArrayList(DestroyedImage) = .empty,
    deferred_shaders: std.ArrayList(vk.ShaderModule) = .empty,
    deferred_pipelines: std.ArrayList(vk.Pipeline) = .empty,

    fn init(gpa: Allocator, dev: *const device_mod.Device, diag: *Diag) Error!FrameSlot {
        const pool_ci = vk.CommandPoolCreateInfo{
            .flags = vk.CommandPoolCreate.transient_bit | vk.CommandPoolCreate.reset_command_buffer_bit,
            .queueFamilyIndex = dev.graphics_queue_family,
        };
        var pool: vk.CommandPool = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateCommandPool(dev.handle, &pool_ci, null, &pool),
            diag,
            "vkCreateCommandPool",
        );
        errdefer dev.disp.vkDestroyCommandPool(dev.handle, pool, null);

        var slot = FrameSlot{ .cmd_pool = pool };
        errdefer slot.deinit(gpa, dev, null);
        _ = try slot.acquireCmdEntry(gpa, dev, diag);
        slot.cmd_used = 0;
        return slot;
    }

    fn acquireCmdEntry(self: *FrameSlot, gpa: Allocator, dev: *const device_mod.Device, diag: *Diag) Error!*CmdEntry {
        if (self.cmd_used < self.cmd_entries.items.len) {
            return &self.cmd_entries.items[self.cmd_used];
        }
        const cb_ai = vk.CommandBufferAllocateInfo{
            .commandPool = self.cmd_pool,
            .level = .primary,
            .commandBufferCount = 1,
        };
        var cb: vk.CommandBuffer = null;
        try loader_mod.check(
            dev.disp.vkAllocateCommandBuffers(dev.handle, &cb_ai, @ptrCast(&cb)),
            diag,
            "vkAllocateCommandBuffers",
        );

        const fence_ci = vk.FenceCreateInfo{};
        var fence: vk.Fence = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateFence(dev.handle, &fence_ci, null, &fence),
            diag,
            "vkCreateFence",
        );
        errdefer dev.disp.vkDestroyFence(dev.handle, fence, null);

        self.cmd_entries.append(gpa, .{
            .cb = cb,
            .fence = fence,
            .submitted = false,
        }) catch return error.OutOfMemory;
        return &self.cmd_entries.items[self.cmd_entries.items.len - 1];
    }

    fn waitAndReset(
        self: *FrameSlot,
        dev: *const device_mod.Device,
        mem: *memory_mod.MemoryAllocator,
        diag: *Diag,
    ) Error!void {
        // Wait for every submitted command buffer in this frame slot.
        for (self.cmd_entries.items) |*entry| {
            if (entry.submitted) {
                try loader_mod.check(
                    dev.disp.vkWaitForFences(dev.handle, 1, @ptrCast(&entry.fence), vk.TRUE, std.math.maxInt(u64)),
                    diag,
                    "vkWaitForFences",
                );
                entry.submitted = false;
            }
        }
        if (self.cmd_used > 0) {
            try loader_mod.check(
                dev.disp.vkResetCommandPool(dev.handle, self.cmd_pool, 0),
                diag,
                "vkResetCommandPool",
            );
            self.cmd_used = 0;
        }
        self.upload.reset(dev, mem);
        self.desc_pool.reset(dev);
        self.drainDeferred(dev, mem);
    }

    fn drainDeferred(self: *FrameSlot, dev: *const device_mod.Device, mem: *memory_mod.MemoryAllocator) void {
        for (self.deferred_images.items) |img| {
            destroyImageResources(dev, mem, img);
        }
        self.deferred_images.clearRetainingCapacity();

        for (self.deferred_shaders.items) |mod| {
            if (mod != .null_handle) dev.disp.vkDestroyShaderModule(dev.handle, mod, null);
        }
        self.deferred_shaders.clearRetainingCapacity();

        for (self.deferred_pipelines.items) |pipe| {
            if (pipe != .null_handle) dev.disp.vkDestroyPipeline(dev.handle, pipe, null);
        }
        self.deferred_pipelines.clearRetainingCapacity();
    }

    fn deinit(self: *FrameSlot, gpa: Allocator, dev: *const device_mod.Device, mem_opt: ?*memory_mod.MemoryAllocator) void {
        if (mem_opt) |mem| {
            self.drainDeferred(dev, mem);
            self.upload.deinit(gpa, dev, mem);
        }
        self.desc_pool.deinit(gpa, dev);
        for (self.cmd_entries.items) |entry| {
            if (entry.fence != .null_handle) dev.disp.vkDestroyFence(dev.handle, entry.fence, null);
        }
        self.cmd_entries.deinit(gpa);
        if (self.cmd_pool != .null_handle) {
            dev.disp.vkDestroyCommandPool(dev.handle, self.cmd_pool, null);
            self.cmd_pool = .null_handle;
        }
        self.deferred_images.deinit(gpa);
        self.deferred_shaders.deinit(gpa);
        self.deferred_pipelines.deinit(gpa);
    }
};

fn destroyImageResources(dev: *const device_mod.Device, mem: *memory_mod.MemoryAllocator, img: DestroyedImage) void {
    if (img.framebuffer != .null_handle) dev.disp.vkDestroyFramebuffer(dev.handle, img.framebuffer, null);
    if (img.rt_view != .null_handle and img.rt_view != img.sample_view) {
        dev.disp.vkDestroyImageView(dev.handle, img.rt_view, null);
    }
    if (img.sample_view != .null_handle) dev.disp.vkDestroyImageView(dev.handle, img.sample_view, null);
    if (img.vk_image != .null_handle) dev.disp.vkDestroyImage(dev.handle, img.vk_image, null);
    mem.free(img.alloc);
}

pub const Backend = struct {
    gpa: Allocator,
    loader: loader_mod.Loader,
    inst: instance_mod.Instance,
    dev: device_mod.Device,
    mem: memory_mod.MemoryAllocator,
    descs: descriptors_mod.DescriptorManager,
    rp_cache: renderpass_mod.RenderPassCache = .{},
    pipelines: pipeline_mod.PipelineCache,

    frames_in_flight: u32,
    frame_index: u32 = 0,
    frames: [max_frames_in_flight]FrameSlot = @splat(.{}),

    // Active command recording & render pass state.
    active_cb: vk.CommandBuffer = null,
    active_entry_idx: usize = 0,
    in_render_pass: bool = false,
    current_dst_id: u32 = 0,
    current_dst_is_screen: bool = false,
    current_dst_surface_id: u32 = 0,
    current_dst_extent: vk.Extent2D = .{ .width = 0, .height = 0 },

    // Vertex / index upload state.
    current_vb: vk.Buffer = .null_handle,
    current_vb_offset: u64 = 0,
    current_ib: vk.Buffer = .null_handle,
    current_ib_offset: u64 = 0,
    vb_bound: bool = false,

    // Binding & uniform version caches.
    cache: state_cache.BindCache = .{},
    bound_desc_set: vk.DescriptorSet = .null_handle,
    bound_internal_buf: vk.Buffer = .null_handle,
    bound_user_buf: vk.Buffer = .null_handle,
    bound_user_range: u64 = 0,
    bound_internal_offset: u32 = 0,
    bound_user_offset: u32 = 0,
    bound_src_views: [abi.max_src_images]vk.ImageView = @splat(.null_handle),
    bound_internal_version: u64 = 0,
    bound_user_version: u64 = 0,
    has_bound_desc: bool = false,

    // Cached last uploaded uniform slices (within current frame slot).
    last_uploaded_internal_ver: u64 = 0,
    last_uploaded_internal_buf: vk.Buffer = .null_handle,
    last_uploaded_internal_off: u32 = 0,
    last_uploaded_user_ver: u64 = 0,
    last_uploaded_user_buf: vk.Buffer = .null_handle,
    last_uploaded_user_off: u32 = 0,
    last_uploaded_user_range: u64 = 0,

    // Resource handle tables.
    images: handles.HandleTable(Image) = .{},
    shaders: handles.HandleTable(pipeline_mod.Shader) = .{},
    surfaces: handles.HandleTable(*swapchain_mod.Surface) = .{},
    surfaces_lock: sync.Mutex = .{},

    readback: upload_mod.ReadbackBuffer = .{},
    drawn_surfaces: std.ArrayList(u32) = .empty,

    // Active GPU-mapped shared upload arena (zero-copy writePixels).
    shared_upload_chunk: ?upload_mod.Chunk = null,
    shared_arena_id: u32 = 0,

    main_thread_mode: std.atomic.Value(u32) = std.atomic.Value(u32).init(0),
    next_shader_gen: u64 = 1,

    submits: u64 = 0,
    draws: u64 = 0,
    binds_skipped: u64 = 0,
    uploaded_bytes: u64 = 0,
    completed_frames: u64 = 0,

    pub fn init(gpa: Allocator, opts: *const abi.DeviceOptions, caps: *abi.DriverCaps, diag: *Diag) Error!*Backend {
        const req_backend: abi.Backend = @enumFromInt(opts.backend);
        if (req_backend != .auto and req_backend != .vulkan) {
            diag.print("vulkan: requested backend {d} is not supported on Linux Vulkan driver", .{opts.backend});
            return error.Unsupported;
        }

        const self = gpa.create(Backend) catch return error.OutOfMemory;
        errdefer gpa.destroy(self);

        var loader = try loader_mod.Loader.open(diag);
        errdefer loader.close();

        const debug_enabled = (opts.flags & abi.device_debug) != 0;
        var inst = try instance_mod.Instance.init(gpa, &loader, debug_enabled, diag);
        errdefer inst.deinit(gpa);

        var dev = try device_mod.Device.init(gpa, &loader, &inst, opts, caps, diag);
        errdefer dev.deinit();

        const frames_in_flight = caps.frames_in_flight;

        self.* = .{
            .gpa = gpa,
            .loader = loader,
            .inst = inst,
            .dev = dev,
            .mem = undefined,
            .descs = undefined,
            .pipelines = undefined,
            .frames_in_flight = frames_in_flight,
            .main_thread_mode = std.atomic.Value(u32).init(opts.main_thread_mode),
        };
        // Point MemoryAllocator at the pinned self.dev address.
        self.mem = memory_mod.MemoryAllocator.init(gpa, &self.dev);
        errdefer self.mem.deinit();

        self.descs = try descriptors_mod.DescriptorManager.init(&self.dev, &self.mem, diag);
        errdefer self.descs.deinit(&self.dev, &self.mem);

        self.pipelines = try pipeline_mod.PipelineCache.init(&self.dev, diag);
        errdefer self.pipelines.deinit(gpa, &self.dev);

        var inited_frames: usize = 0;
        errdefer {
            for (self.frames[0..inited_frames]) |*f| {
                f.deinit(gpa, &self.dev, &self.mem);
            }
        }
        for (0..frames_in_flight) |i| {
            self.frames[i] = try FrameSlot.init(gpa, &self.dev, diag);
            inited_frames += 1;
        }

        // Transition 1x1 dummy texture to SHADER_READ_ONLY_OPTIMAL once at startup.
        try self.descs.initDummyImageLayout(&self.dev, self.frames[0].cmd_pool, diag);
        try loader_mod.check(
            self.dev.disp.vkResetCommandPool(self.dev.handle, self.frames[0].cmd_pool, 0),
            diag,
            "vkResetCommandPool(init)",
        );

        caps.flags |= abi.cap_shared_upload_arena;
        return self;
    }

    pub fn deinit(self: *Backend) void {
        const gpa = self.gpa;
        self.dev.waitIdle();

        // Destroy all remaining live offscreen images.
        for (self.images.items.items) |*slot| {
            if (slot.*) |img| {
                if (!img.is_screen) {
                    destroyImageResources(&self.dev, &self.mem, .{
                        .vk_image = img.vk_image,
                        .alloc = img.alloc,
                        .sample_view = img.sample_view,
                        .rt_view = img.rt_view,
                        .framebuffer = img.framebuffer,
                    });
                }
                slot.* = null;
            }
        }
        self.images.deinit(gpa);

        // Destroy all remaining live shaders.
        for (self.shaders.items.items) |*slot| {
            if (slot.*) |sh| {
                if (sh.frag_module != .null_handle) {
                    self.dev.disp.vkDestroyShaderModule(self.dev.handle, sh.frag_module, null);
                }
                gpa.free(sh.vert_entry);
                gpa.free(sh.frag_entry);
                slot.* = null;
            }
        }
        self.shaders.deinit(gpa);

        // Destroy all remaining live surfaces.
        self.surfaces_lock.lock();
        for (self.surfaces.items.items) |*slot| {
            if (slot.*) |surf| {
                surf.deinit(gpa, &self.inst, &self.dev);
                slot.* = null;
            }
        }
        self.surfaces.deinit(gpa);
        self.surfaces_lock.unlock();
        self.surfaces_lock.deinit();

        self.drawn_surfaces.deinit(gpa);
        self.readback.deinit(&self.dev, &self.mem);
        if (self.shared_upload_chunk) |chunk| {
            upload_mod.destroyChunk(&self.dev, &self.mem, chunk);
            self.shared_upload_chunk = null;
        }

        for (0..self.frames_in_flight) |i| {
            self.frames[i].deinit(gpa, &self.dev, &self.mem);
        }

        self.pipelines.deinit(gpa, &self.dev);
        self.rp_cache.deinit(gpa, &self.dev);
        self.descs.deinit(&self.dev, &self.mem);
        self.mem.deinit();
        self.dev.deinit();
        self.inst.deinit(gpa);
        self.loader.close();
        gpa.destroy(self);
    }

    inline fn currentFrame(self: *Backend) *FrameSlot {
        return &self.frames[self.frame_index];
    }

    fn resetBindState(self: *Backend) void {
        self.cache.reset();
        self.vb_bound = false;
        self.bound_desc_set = .null_handle;
        self.bound_internal_buf = .null_handle;
        self.bound_user_buf = .null_handle;
        self.bound_user_range = 0;
        self.bound_internal_offset = 0;
        self.bound_user_offset = 0;
        self.bound_src_views = @splat(.null_handle);
        self.bound_internal_version = 0;
        self.bound_user_version = 0;
        self.has_bound_desc = false;
    }

    fn resetFrameUploadCache(self: *Backend) void {
        self.last_uploaded_internal_ver = 0;
        self.last_uploaded_internal_buf = .null_handle;
        self.last_uploaded_internal_off = 0;
        self.last_uploaded_user_ver = 0;
        self.last_uploaded_user_buf = .null_handle;
        self.last_uploaded_user_off = 0;
        self.last_uploaded_user_range = 0;
    }

    fn ensureRecording(self: *Backend, diag: *Diag) Error!vk.CommandBuffer {
        if (self.active_cb != null) return self.active_cb;
        const frame = self.currentFrame();
        const entry = try frame.acquireCmdEntry(self.gpa, &self.dev, diag);
        const entry_idx = frame.cmd_used;

        const begin_ci = vk.CommandBufferBeginInfo{
            .flags = vk.CommandBufferUsage.one_time_submit_bit,
        };
        try loader_mod.check(
            self.dev.disp.vkBeginCommandBuffer(entry.cb, &begin_ci),
            diag,
            "vkBeginCommandBuffer",
        );

        self.active_cb = entry.cb;
        self.active_entry_idx = entry_idx;
        self.in_render_pass = false;
        self.current_dst_id = 0;
        self.current_dst_is_screen = false;
        self.current_dst_surface_id = 0;
        self.resetBindState();
        return entry.cb;
    }

    fn endRenderPass(self: *Backend) void {
        if (!self.in_render_pass) return;
        if (self.active_cb) |cb| {
            self.dev.disp.vkCmdEndRenderPass(cb);
        }
        self.in_render_pass = false;
        self.current_dst_id = 0;
        self.current_dst_is_screen = false;
        self.current_dst_surface_id = 0;
        self.cache.reset();
        self.has_bound_desc = false;
        self.bound_desc_set = .null_handle;
    }

    fn submitActiveCommandBuffer(
        self: *Backend,
        signal_sems: []const vk.Semaphore,
        diag: *Diag,
    ) Error!void {
        self.endRenderPass();
        const cb = self.active_cb orelse return;

        try loader_mod.check(
            self.dev.disp.vkEndCommandBuffer(cb),
            diag,
            "vkEndCommandBuffer",
        );

        // Wait on any pending swapchain acquire semaphores before the GPU executes draws to them.
        var wait_sems: std.ArrayList(vk.Semaphore) = .empty;
        defer wait_sems.deinit(self.gpa);
        var wait_stages: std.ArrayList(vk.Flags) = .empty;
        defer wait_stages.deinit(self.gpa);
        var pending_surfs: std.ArrayList(*swapchain_mod.Surface) = .empty;
        defer pending_surfs.deinit(self.gpa);

        for (self.drawn_surfaces.items) |sid| {
            const surf = self.lookupSurface(sid) orelse continue;
            if (surf.acquire_wait_pending) {
                wait_sems.append(self.gpa, surf.currentAcquireSemaphore()) catch return error.OutOfMemory;
                wait_stages.append(self.gpa, vk.PipelineStage.color_attachment_output_bit) catch return error.OutOfMemory;
                pending_surfs.append(self.gpa, surf) catch return error.OutOfMemory;
            }
        }

        const frame = self.currentFrame();
        const entry = &frame.cmd_entries.items[self.active_entry_idx];
        try loader_mod.check(
            self.dev.disp.vkResetFences(self.dev.handle, 1, @ptrCast(&entry.fence)),
            diag,
            "vkResetFences",
        );

        const submit_info = vk.SubmitInfo{
            .waitSemaphoreCount = @intCast(wait_sems.items.len),
            .pWaitSemaphores = if (wait_sems.items.len > 0) wait_sems.items.ptr else null,
            .pWaitDstStageMask = if (wait_stages.items.len > 0) wait_stages.items.ptr else null,
            .commandBufferCount = 1,
            .pCommandBuffers = @ptrCast(&cb),
            .signalSemaphoreCount = @intCast(signal_sems.len),
            .pSignalSemaphores = if (signal_sems.len > 0) signal_sems.ptr else null,
        };
        try loader_mod.check(
            self.dev.disp.vkQueueSubmit(self.dev.graphics_queue, 1, @ptrCast(&submit_info), entry.fence),
            diag,
            "vkQueueSubmit",
        );

        for (pending_surfs.items) |surf| {
            surf.acquire_wait_pending = false;
        }

        entry.submitted = true;
        frame.cmd_used = @max(frame.cmd_used, self.active_entry_idx + 1);
        self.active_cb = null;
        self.resetBindState();
    }

    fn cmdTransitionImage(
        self: *Backend,
        cb: vk.CommandBuffer,
        vk_img: vk.Image,
        old_layout: vk.ImageLayout,
        new_layout: vk.ImageLayout,
        is_swapchain_acquire: bool,
    ) void {
        if (old_layout == new_layout) return;

        const src: struct { stage: vk.Flags, access: vk.Flags } = switch (old_layout) {
            .undefined => .{
                .stage = if (is_swapchain_acquire)
                    vk.PipelineStage.color_attachment_output_bit
                else
                    vk.PipelineStage.top_of_pipe_bit,
                .access = 0,
            },
            .transfer_dst_optimal => .{
                .stage = vk.PipelineStage.transfer_bit,
                .access = vk.Access.transfer_write_bit,
            },
            .transfer_src_optimal => .{
                .stage = vk.PipelineStage.transfer_bit,
                .access = vk.Access.transfer_read_bit,
            },
            .shader_read_only_optimal => .{
                .stage = vk.PipelineStage.fragment_shader_bit,
                .access = vk.Access.shader_read_bit,
            },
            .color_attachment_optimal => .{
                .stage = vk.PipelineStage.color_attachment_output_bit,
                .access = vk.Access.color_attachment_write_bit,
            },
            .present_src_khr => .{
                .stage = vk.PipelineStage.bottom_of_pipe_bit,
                .access = 0,
            },
            else => .{
                .stage = vk.PipelineStage.all_commands_bit,
                .access = vk.Access.memory_write_bit,
            },
        };

        const dst: struct { stage: vk.Flags, access: vk.Flags } = switch (new_layout) {
            .transfer_dst_optimal => .{
                .stage = vk.PipelineStage.transfer_bit,
                .access = vk.Access.transfer_write_bit,
            },
            .transfer_src_optimal => .{
                .stage = vk.PipelineStage.transfer_bit,
                .access = vk.Access.transfer_read_bit,
            },
            .shader_read_only_optimal => .{
                .stage = vk.PipelineStage.fragment_shader_bit,
                .access = vk.Access.shader_read_bit,
            },
            .color_attachment_optimal => .{
                .stage = vk.PipelineStage.color_attachment_output_bit,
                .access = vk.Access.color_attachment_read_bit | vk.Access.color_attachment_write_bit,
            },
            .present_src_khr => .{
                .stage = vk.PipelineStage.bottom_of_pipe_bit,
                .access = 0,
            },
            else => .{
                .stage = vk.PipelineStage.all_commands_bit,
                .access = vk.Access.memory_read_bit | vk.Access.memory_write_bit,
            },
        };

        const barrier = vk.ImageMemoryBarrier{
            .srcAccessMask = src.access,
            .dstAccessMask = dst.access,
            .oldLayout = old_layout,
            .newLayout = new_layout,
            .image = vk_img,
            .subresourceRange = .{
                .aspectMask = vk.ImageAspect.color_bit,
                .baseMipLevel = 0,
                .levelCount = 1,
                .baseArrayLayer = 0,
                .layerCount = 1,
            },
        };
        self.dev.disp.vkCmdPipelineBarrier(
            cb,
            src.stage,
            dst.stage,
            0,
            0,
            null,
            0,
            null,
            1,
            @ptrCast(&barrier),
        );
    }

    fn cmdClearUndefinedImage(self: *Backend, cb: vk.CommandBuffer, img: *Image) void {
        self.cmdTransitionImage(cb, img.vk_image, .undefined, .transfer_dst_optimal, false);
        const zero = vk.ClearColorValue{ .float32 = .{ 0, 0, 0, 0 } };
        const range = vk.ImageSubresourceRange{
            .aspectMask = vk.ImageAspect.color_bit,
            .baseMipLevel = 0,
            .levelCount = 1,
            .baseArrayLayer = 0,
            .layerCount = 1,
        };
        self.dev.disp.vkCmdClearColorImage(cb, img.vk_image, .transfer_dst_optimal, &zero, 1, @ptrCast(&range));
        img.layout = .transfer_dst_optimal;
    }

    fn addDrawnSurface(self: *Backend, surface_id: u32) Error!void {
        for (self.drawn_surfaces.items) |sid| {
            if (sid == surface_id) return;
        }
        self.drawn_surfaces.append(self.gpa, surface_id) catch return error.OutOfMemory;
    }

    fn lookupSurface(self: *Backend, surface_id: u32) ?*swapchain_mod.Surface {
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        const ptr = self.surfaces.getOpt(surface_id) orelse return null;
        return ptr.*;
    }

    // -------------------------------------------------------------------------
    // Backend contract methods (§3.3 — 23 required signatures)
    // -------------------------------------------------------------------------

    pub fn beginBatch(self: *Backend, diag: *Diag) Error!void {
        _ = diag;
        self.resetBindState();
    }

    pub fn abortBatch(self: *Backend) void {
        self.endRenderPass();
        if (self.active_cb) |cb| {
            _ = self.dev.disp.vkEndCommandBuffer(cb);
            _ = self.dev.disp.vkResetCommandBuffer(cb, 0);
            self.active_cb = null;
        }
        self.resetBindState();
    }

    pub fn endBatch(self: *Backend, mode: abi.FlushMode, res: *abi.SubmitResult, diag: *Diag) Error!void {
        self.submits += 1;
        res.* = std.mem.zeroes(abi.SubmitResult);

        switch (mode) {
            .intermediate => {
                // End active pass and submit current CB; keep frame slot active for subsequent CBs.
                if (self.active_cb != null) {
                    try self.submitActiveCommandBuffer(&.{}, diag);
                }
            },
            .end_frame => {
                // Submit any recorded CB and wait for all fences in this frame slot.
                if (self.active_cb != null) {
                    try self.submitActiveCommandBuffer(&.{}, diag);
                }
                const frame = self.currentFrame();
                // If retired upload chunks (e.g. rotated shared arena) are queued in this slot,
                // ensure any earlier in-flight frame slots have also completed before freeing them.
                if (frame.upload.retired.items.len > 0) {
                    for (0..self.frames_in_flight) |i| {
                        if (i == self.frame_index) continue;
                        for (self.frames[i].cmd_entries.items) |*entry| {
                            if (entry.submitted) {
                                try loader_mod.check(
                                    self.dev.disp.vkWaitForFences(self.dev.handle, 1, @ptrCast(&entry.fence), vk.TRUE, std.math.maxInt(u64)),
                                    diag,
                                    "vkWaitForFences(retired)",
                                );
                                entry.submitted = false;
                            }
                        }
                    }
                }
                try frame.waitAndReset(&self.dev, &self.mem, diag);
                // Any swapchain image acquired during this frame was not presented: discard safely
                // so acquire semaphores are never left signaled and swapchain slots never exhaust.
                for (self.drawn_surfaces.items) |sid| {
                    const surf = self.lookupSurface(sid) orelse continue;
                    if (surf.acquired_index != null) {
                        try surf.discardAcquired(&self.dev, diag);
                    }
                }
                self.resetFrameUploadCache();
                self.current_vb = .null_handle;
                self.current_ib = .null_handle;
                self.drawn_surfaces.clearRetainingCapacity();
                self.completed_frames += 1;
            },
            .present => {
                self.endRenderPass();

                var signal_sems: std.ArrayList(vk.Semaphore) = .empty;
                defer signal_sems.deinit(self.gpa);
                var present_surfs: std.ArrayList(*swapchain_mod.Surface) = .empty;
                defer present_surfs.deinit(self.gpa);
                var discard_surfs: std.ArrayList(*swapchain_mod.Surface) = .empty;
                defer discard_surfs.deinit(self.gpa);
                var swapchains: std.ArrayList(vk.SwapchainKHR) = .empty;
                defer swapchains.deinit(self.gpa);
                var img_indices: std.ArrayList(u32) = .empty;
                defer img_indices.deinit(self.gpa);

                for (self.drawn_surfaces.items) |sid| {
                    const surf = self.lookupSurface(sid) orelse continue;
                    const idx = surf.acquired_index orelse continue;
                    if (surf.isHidden() or surf.swapchain == .null_handle) {
                        discard_surfs.append(self.gpa, surf) catch return error.OutOfMemory;
                        continue;
                    }
                    const sc_img = surf.currentSwapchainImage() orelse continue;
                    const cb = try self.ensureRecording(diag);
                    self.cmdTransitionImage(cb, sc_img.image, sc_img.layout, .present_src_khr, false);
                    sc_img.layout = .present_src_khr;

                    signal_sems.append(self.gpa, surf.currentRenderSemaphore()) catch return error.OutOfMemory;
                    present_surfs.append(self.gpa, surf) catch return error.OutOfMemory;
                    swapchains.append(self.gpa, surf.swapchain) catch return error.OutOfMemory;
                    img_indices.append(self.gpa, idx) catch return error.OutOfMemory;
                }

                if (self.active_cb != null) {
                    try self.submitActiveCommandBuffer(signal_sems.items, diag);
                }

                for (discard_surfs.items) |surf| {
                    try surf.discardAcquired(&self.dev, diag);
                }

                var presented: u32 = 0;
                if (swapchains.items.len > 0) {
                    const present_fn = self.dev.disp.vkQueuePresentKHR orelse return error.Unsupported;
                    const results = self.gpa.alloc(vk.Result, swapchains.items.len) catch return error.OutOfMemory;
                    defer self.gpa.free(results);

                    const pi = vk.PresentInfoKHR{
                        .waitSemaphoreCount = @intCast(signal_sems.items.len),
                        .pWaitSemaphores = signal_sems.items.ptr,
                        .swapchainCount = @intCast(swapchains.items.len),
                        .pSwapchains = swapchains.items.ptr,
                        .pImageIndices = img_indices.items.ptr,
                        .pResults = results.ptr,
                    };
                    const overall = present_fn(self.dev.graphics_queue, &pi);

                    for (present_surfs.items, results) |surf, r| {
                        surf.acquired_index = null;
                        surf.drawn = false;
                        surf.cleared = false;
                        switch (r) {
                            .success => presented += 1,
                            .suboptimal_khr => {
                                surf.suboptimal = true;
                                presented += 1;
                            },
                            .error_out_of_date_khr => {
                                surf.needs_recreate = true;
                            },
                            else => {},
                        }
                    }

                    if (overall != .success and overall != .suboptimal_khr and overall != .error_out_of_date_khr) {
                        try loader_mod.check(overall, diag, "vkQueuePresentKHR");
                    }
                }
                self.drawn_surfaces.clearRetainingCapacity();

                // Advance to next frame slot and wait on its fence before reuse.
                self.frame_index = (self.frame_index + 1) % self.frames_in_flight;
                try self.currentFrame().waitAndReset(&self.dev, &self.mem, diag);
                self.resetFrameUploadCache();
                self.current_vb = .null_handle;
                self.current_ib = .null_handle;

                self.completed_frames += 1;
                res.presented_surfaces = presented;
            },
        }

        res.completed_frames = self.completed_frames;
    }

    pub fn setVertices(self: *Backend, vertices: []const f32, indices: []const u32, diag: *Diag) Error!void {
        if (vertices.len == 0 and indices.len == 0) return;

        const frame = self.currentFrame();

        if (vertices.len > 0) {
            const vbytes: u64 = @as(u64, vertices.len) * @sizeOf(f32);
            const vslice = try frame.upload.alloc(self.gpa, &self.dev, &self.mem, vbytes, 16, diag);
            @memcpy(vslice.ptr[0..@intCast(vbytes)], std.mem.sliceAsBytes(vertices));
            self.current_vb = vslice.buffer;
            self.current_vb_offset = vslice.offset;
            self.uploaded_bytes += vbytes;
        }

        if (indices.len > 0) {
            const ibytes: u64 = @as(u64, indices.len) * @sizeOf(u32);
            const islice = try frame.upload.alloc(self.gpa, &self.dev, &self.mem, ibytes, 4, diag);
            @memcpy(islice.ptr[0..@intCast(ibytes)], std.mem.sliceAsBytes(indices));
            self.current_ib = islice.buffer;
            self.current_ib_offset = islice.offset;
            self.uploaded_bytes += ibytes;
        }

        self.vb_bound = false;
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
        if (w == 0 or h == 0 or w > self.dev.max_image_size or h > self.dev.max_image_size) {
            diag.print("vulkan: invalid image dimensions {d}x{d}", .{ w, h });
            return error.InvalidArgument;
        }
        const actual_iw = if (iw > 0) iw else w;
        const actual_ih = if (ih > 0) ih else h;
        if (actual_iw > self.dev.max_image_size or actual_ih > self.dev.max_image_size) {
            diag.print("vulkan: internal image dimensions {d}x{d} exceed max {d}", .{
                actual_iw,
                actual_ih,
                self.dev.max_image_size,
            });
            return error.InvalidArgument;
        }
        if (fmt == .a8 and !self.dev.supports_a8) {
            diag.set("vulkan: A8 (R8_UNORM) pixel format is not supported by this device");
            return error.Unsupported;
        }

        const vk_fmt: vk.Format = switch (fmt) {
            .rgba8 => .r8g8b8a8_unorm,
            .a8 => .r8_unorm,
        };

        const img_ci = vk.ImageCreateInfo{
            .imageType = .@"2d",
            .format = vk_fmt,
            .extent = .{ .width = actual_iw, .height = actual_ih, .depth = 1 },
            .mipLevels = 1,
            .arrayLayers = 1,
            .samples = vk.SampleCount.@"1_bit",
            .tiling = .optimal,
            .usage = vk.ImageUsage.sampled_bit |
                vk.ImageUsage.color_attachment_bit |
                vk.ImageUsage.transfer_src_bit |
                vk.ImageUsage.transfer_dst_bit,
            .sharingMode = .exclusive,
            .initialLayout = .undefined,
        };

        var vk_img: vk.Image = .null_handle;
        try loader_mod.check(
            self.dev.disp.vkCreateImage(self.dev.handle, &img_ci, null, &vk_img),
            diag,
            "vkCreateImage",
        );
        errdefer self.dev.disp.vkDestroyImage(self.dev.handle, vk_img, null);

        var req: vk.MemoryRequirements = undefined;
        self.dev.disp.vkGetImageMemoryRequirements(self.dev.handle, vk_img, &req);
        const allocation = try self.mem.alloc(
            req,
            vk.MemoryProperty.device_local_bit,
            0,
            false,
            false,
            false,
            diag,
        );
        errdefer self.mem.free(allocation);

        try loader_mod.check(
            self.dev.disp.vkBindImageMemory(self.dev.handle, vk_img, allocation.memory, allocation.offset),
            diag,
            "vkBindImageMemory",
        );

        const subrange = vk.ImageSubresourceRange{
            .aspectMask = vk.ImageAspect.color_bit,
            .baseMipLevel = 0,
            .levelCount = 1,
            .baseArrayLayer = 0,
            .layerCount = 1,
        };

        // Sample view: A8 uses (R, R, R, R) swizzle; RGBA8 uses identity.
        const sample_view_ci = vk.ImageViewCreateInfo{
            .image = vk_img,
            .viewType = .@"2d",
            .format = vk_fmt,
            .components = if (fmt == .a8)
                .{ .r = .r, .g = .r, .b = .r, .a = .r }
            else
                .{},
            .subresourceRange = subrange,
        };
        var sample_view: vk.ImageView = .null_handle;
        try loader_mod.check(
            self.dev.disp.vkCreateImageView(self.dev.handle, &sample_view_ci, null, &sample_view),
            diag,
            "vkCreateImageView(sample)",
        );
        errdefer self.dev.disp.vkDestroyImageView(self.dev.handle, sample_view, null);

        // Render-target view MUST always have identity component mapping (framebuffer swizzle is forbidden!).
        var rt_view: vk.ImageView = sample_view;
        if (fmt == .a8) {
            const rt_view_ci = vk.ImageViewCreateInfo{
                .image = vk_img,
                .viewType = .@"2d",
                .format = vk_fmt,
                .components = .{},
                .subresourceRange = subrange,
            };
            try loader_mod.check(
                self.dev.disp.vkCreateImageView(self.dev.handle, &rt_view_ci, null, &rt_view),
                diag,
                "vkCreateImageView(rt)",
            );
        }
        errdefer if (rt_view != sample_view) self.dev.disp.vkDestroyImageView(self.dev.handle, rt_view, null);

        const compat_rp = try self.rp_cache.getOrCreate(self.gpa, &self.dev, .{
            .format = vk_fmt,
            .load_op = .load,
        }, diag);
        const fb = try renderpass_mod.createFramebuffer(&self.dev, compat_rp, rt_view, actual_iw, actual_ih, diag);
        errdefer self.dev.disp.vkDestroyFramebuffer(self.dev.handle, fb, null);

        try self.images.put(self.gpa, id, .{
            .width = w,
            .height = h,
            .internal_width = actual_iw,
            .internal_height = actual_ih,
            .format = fmt,
            .vk_format = vk_fmt,
            .is_screen = false,
            .surface_id = 0,
            .vk_image = vk_img,
            .alloc = allocation,
            .sample_view = sample_view,
            .rt_view = rt_view,
            .framebuffer = fb,
            .layout = .undefined,
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
        if (w == 0 or h == 0 or w > self.dev.max_image_size or h > self.dev.max_image_size) {
            diag.print("vulkan: invalid screen image dimensions {d}x{d}", .{ w, h });
            return error.InvalidArgument;
        }
        const surf = self.lookupSurface(surface_id) orelse {
            diag.print("vulkan: unknown surface_id {d}", .{surface_id});
            return error.UnknownHandle;
        };

        // End any open render pass and flush active CB if resizing an existing swapchain.
        if (self.in_render_pass and self.current_dst_is_screen and self.current_dst_surface_id == surface_id) {
            self.endRenderPass();
        }
        if (surf.swapchain != .null_handle and (surf.extent.width != w or surf.extent.height != h or surf.needs_recreate or surf.suboptimal)) {
            if (self.active_cb != null) {
                try self.submitActiveCommandBuffer(&.{}, diag);
            }
        }

        // Create (or resize) swapchain on newScreenImage unless window is currently hidden.
        if (!surf.isHidden()) {
            _ = try surf.ensureSwapchain(self.gpa, &self.inst, &self.dev, &self.rp_cache, w, h, diag);
        } else {
            surf.requested_width = w;
            surf.requested_height = h;
        }

        try self.images.put(self.gpa, id, .{
            .width = w,
            .height = h,
            .internal_width = w,
            .internal_height = h,
            .format = .rgba8,
            .vk_format = surf.format,
            .is_screen = true,
            .surface_id = surface_id,
        });
    }

    pub fn disposeImage(self: *Backend, id: u32) Error!void {
        if (self.in_render_pass and self.current_dst_id == id) {
            self.endRenderPass();
        }
        const img = try self.images.remove(id);
        if (!img.is_screen) {
            self.cache.invalidateTexture(@intFromEnum(img.sample_view));
            for (self.bound_src_views) |v| {
                if (v == img.sample_view) {
                    self.has_bound_desc = false;
                    self.bound_desc_set = .null_handle;
                    break;
                }
            }
            const destroyed = DestroyedImage{
                .vk_image = img.vk_image,
                .alloc = img.alloc,
                .sample_view = img.sample_view,
                .rt_view = img.rt_view,
                .framebuffer = img.framebuffer,
            };
            self.currentFrame().deferred_images.append(self.gpa, destroyed) catch {
                // OOM deferring: wait idle and destroy immediately to prevent leak.
                self.dev.waitIdle();
                destroyImageResources(&self.dev, &self.mem, destroyed);
            };
        }
    }

    pub fn disposeShader(self: *Backend, id: u32) Error!void {
        const sh = try self.shaders.remove(id);
        self.cache.pipeline = 0;

        const frame = self.currentFrame();
        self.pipelines.purgeShaderPipelines(self.gpa, id, sh.gen, &frame.deferred_pipelines);
        if (sh.frag_module != .null_handle) {
            frame.deferred_shaders.append(self.gpa, sh.frag_module) catch {
                self.dev.waitIdle();
                self.dev.disp.vkDestroyShaderModule(self.dev.handle, sh.frag_module, null);
            };
        }
        self.gpa.free(sh.vert_entry);
        self.gpa.free(sh.frag_entry);
    }

    pub fn disposeSurface(self: *Backend, id: u32) Error!void {
        if (self.in_render_pass and self.current_dst_is_screen and self.current_dst_surface_id == id) {
            self.endRenderPass();
        }
        self.surfaces_lock.lock();
        const surf = self.surfaces.remove(id) catch |err| {
            self.surfaces_lock.unlock();
            return err;
        };
        self.surfaces_lock.unlock();

        // Remove from drawn_surfaces if present.
        var i: usize = 0;
        while (i < self.drawn_surfaces.items.len) {
            if (self.drawn_surfaces.items[i] == id) {
                _ = self.drawn_surfaces.swapRemove(i);
            } else {
                i += 1;
            }
        }

        // Flush any recorded command buffer and wait for device idle before destroying swapchain & VkSurfaceKHR.
        if (self.active_cb != null) {
            var none_diag = Diag.none();
            self.submitActiveCommandBuffer(&.{}, &none_diag) catch {};
        }
        self.dev.waitIdle();
        surf.deinit(self.gpa, &self.inst, &self.dev);
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
            diag.set("vulkan: cannot writePixels to a screen image");
            return error.InvalidArgument;
        }
        const bpp: u32 = img.format.bytesPerPixel();

        // Validate all regions up front.
        var covers_full_internal = false;
        for (regions) |r| {
            const bounds = validateRect(r.rect, img.internal_width, img.internal_height) orelse {
                diag.print("vulkan: writePixels rect ({d},{d})-({d},{d}) out of bounds {d}x{d}", .{
                    r.rect.x0, r.rect.y0, r.rect.x1, r.rect.y1, img.internal_width, img.internal_height,
                });
                return error.InvalidArgument;
            };
            const expected_len = @as(u64, bounds.dx) * @as(u64, bounds.dy) * bpp;
            if (r.payload_len != expected_len) {
                diag.print("vulkan: writePixels payload_len {d} != expected {d}", .{ r.payload_len, expected_len });
                return error.InvalidArgument;
            }
            if (bounds.x0 == 0 and bounds.y0 == 0 and bounds.dx == img.internal_width and bounds.dy == img.internal_height) {
                covers_full_internal = true;
            }
        }
        if (regions.len == 0) return;

        self.endRenderPass();
        const cb = try self.ensureRecording(diag);

        if (img.layout == .undefined) {
            if (covers_full_internal) {
                self.cmdTransitionImage(cb, img.vk_image, .undefined, .transfer_dst_optimal, false);
                img.layout = .transfer_dst_optimal;
            } else {
                self.cmdClearUndefinedImage(cb, img);
                // Ensure clear completes before copy writes to TRANSFER_DST_OPTIMAL.
                const barrier = vk.ImageMemoryBarrier{
                    .srcAccessMask = vk.Access.transfer_write_bit,
                    .dstAccessMask = vk.Access.transfer_write_bit,
                    .oldLayout = .transfer_dst_optimal,
                    .newLayout = .transfer_dst_optimal,
                    .image = img.vk_image,
                    .subresourceRange = .{
                        .aspectMask = vk.ImageAspect.color_bit,
                        .baseMipLevel = 0,
                        .levelCount = 1,
                        .baseArrayLayer = 0,
                        .layerCount = 1,
                    },
                };
                self.dev.disp.vkCmdPipelineBarrier(
                    cb,
                    vk.PipelineStage.transfer_bit,
                    vk.PipelineStage.transfer_bit,
                    0,
                    0,
                    null,
                    0,
                    null,
                    1,
                    @ptrCast(&barrier),
                );
            }
        } else if (img.layout != .transfer_dst_optimal) {
            self.cmdTransitionImage(cb, img.vk_image, img.layout, .transfer_dst_optimal, false);
            img.layout = .transfer_dst_optimal;
        }

        const copy_align = @max(@max(self.dev.props.limits.optimalBufferCopyOffsetAlignment, bpp), 4);
        const frame = self.currentFrame();

        // Check if payload resides inside the active GPU-mapped shared upload arena for zero-copy transfer.
        var shared_buf: vk.Buffer = .null_handle;
        var shared_base_off: u64 = 0;
        var shared_cap: u64 = 0;
        if (self.shared_upload_chunk) |chunk| {
            if (chunk.alloc.mapped) |chunk_base_ptr| {
                if (payload.len > 0) {
                    const p_addr: usize = @intFromPtr(payload.ptr);
                    const c_addr: usize = @intFromPtr(chunk_base_ptr);
                    const c_cap: usize = @intCast(chunk.capacity);
                    if (p_addr >= c_addr and p_addr - c_addr <= c_cap and payload.len <= c_cap - (p_addr - c_addr)) {
                        shared_buf = chunk.buffer;
                        shared_base_off = @intCast(p_addr - c_addr);
                        shared_cap = chunk.capacity;
                    }
                }
            }
        }

        for (regions) |r| {
            const bounds = validateRect(r.rect, img.internal_width, img.internal_height).?;
            if (bounds.dx == 0 or bounds.dy == 0 or r.payload_len == 0) continue;

            if (shared_buf != .null_handle) {
                const buf_off = shared_base_off + @as(u64, r.payload_offset);
                if ((buf_off % bpp) == 0 and buf_off + r.payload_len <= shared_cap) {
                    const copy_region = vk.BufferImageCopy{
                        .bufferOffset = buf_off,
                        .bufferRowLength = 0, // tightly packed
                        .bufferImageHeight = 0,
                        .imageSubresource = .{
                            .aspectMask = vk.ImageAspect.color_bit,
                            .mipLevel = 0,
                            .baseArrayLayer = 0,
                            .layerCount = 1,
                        },
                        .imageOffset = .{ .x = @intCast(bounds.x0), .y = @intCast(bounds.y0), .z = 0 },
                        .imageExtent = .{ .width = bounds.dx, .height = bounds.dy, .depth = 1 },
                    };
                    self.dev.disp.vkCmdCopyBufferToImage(
                        cb,
                        shared_buf,
                        img.vk_image,
                        .transfer_dst_optimal,
                        1,
                        @ptrCast(&copy_region),
                    );
                    self.uploaded_bytes += r.payload_len;
                    continue;
                }
            }

            const slice = try frame.upload.alloc(self.gpa, &self.dev, &self.mem, r.payload_len, copy_align, diag);
            @memcpy(
                slice.ptr[0..r.payload_len],
                payload[r.payload_offset .. r.payload_offset + r.payload_len],
            );

            const copy_region = vk.BufferImageCopy{
                .bufferOffset = slice.offset,
                .bufferRowLength = 0, // tightly packed
                .bufferImageHeight = 0,
                .imageSubresource = .{
                    .aspectMask = vk.ImageAspect.color_bit,
                    .mipLevel = 0,
                    .baseArrayLayer = 0,
                    .layerCount = 1,
                },
                .imageOffset = .{ .x = @intCast(bounds.x0), .y = @intCast(bounds.y0), .z = 0 },
                .imageExtent = .{ .width = bounds.dx, .height = bounds.dy, .depth = 1 },
            };
            self.dev.disp.vkCmdCopyBufferToImage(
                cb,
                slice.buffer,
                img.vk_image,
                .transfer_dst_optimal,
                1,
                @ptrCast(&copy_region),
            );
            self.uploaded_bytes += r.payload_len;
        }
    }

    pub fn drawTriangles(self: *Backend, dc: *const replayer.DrawCall, diag: *Diag) Error!void {
        const dst = self.images.get(dc.dst) catch {
            diag.print("vulkan: drawTriangles unknown dst image {d}", .{dc.dst});
            return error.UnknownHandle;
        };
        const shader = self.shaders.get(dc.shader) catch {
            diag.print("vulkan: drawTriangles unknown shader {d}", .{dc.shader});
            return error.UnknownHandle;
        };

        var src_imgs: [abi.max_src_images]?*Image = @splat(null);
        var src_views: [abi.max_src_images]vk.ImageView = @splat(self.descs.dummy_view);
        var need_src_barrier = false;

        for (dc.srcs, 0..) |src_id, i| {
            if (src_id == 0) continue;
            const s = self.images.get(src_id) catch {
                diag.print("vulkan: drawTriangles unknown src image {d}", .{src_id});
                return error.UnknownHandle;
            };
            if (s.is_screen) {
                diag.set("vulkan: sampling from a screen image is not supported");
                return error.Unsupported;
            }
            src_imgs[i] = s;
            src_views[i] = s.sample_view;
            if (s.layout != .shader_read_only_optimal or (self.in_render_pass and self.current_dst_id == src_id)) {
                need_src_barrier = true;
            }
        }

        if (dc.regions.len == 0) return;

        // Validate uniform buffer ranges against device limits before recording GPU commands.
        const max_ubo_range: u64 = self.dev.props.limits.maxUniformBufferRange;
        if (abi.internal_uniform_bytes > max_ubo_range) {
            diag.print("vulkan: internal uniform block ({d} B) exceeds maxUniformBufferRange ({d} B)", .{
                abi.internal_uniform_bytes,
                max_ubo_range,
            });
            return error.InvalidArgument;
        }
        const user_bytes_total: u64 = @as(u64, dc.user.len) * @sizeOf(u32);
        if (user_bytes_total > max_ubo_range) {
            diag.print("vulkan: user uniform block ({d} B) exceeds maxUniformBufferRange ({d} B)", .{
                user_bytes_total,
                max_ubo_range,
            });
            return error.InvalidArgument;
        }

        // Resolve destination target (if screen, lazily acquire swapchain image).
        var target_surf: ?*swapchain_mod.Surface = null;
        if (dst.is_screen) {
            const surf = self.lookupSurface(dst.surface_id) orelse {
                diag.print("vulkan: drawTriangles unknown surface {d}", .{dst.surface_id});
                return error.UnknownHandle;
            };
            const ok = try surf.acquireNextImage(
                self.gpa,
                &self.inst,
                &self.dev,
                &self.rp_cache,
                self.frame_index,
                diag,
            );
            if (!ok) {
                // Hidden or minimized window: skip draw cleanly.
                return;
            }
            target_surf = surf;
            dst.vk_format = surf.format;
        }

        // End current render pass if destination changed or any source needs a layout barrier.
        if (self.in_render_pass and (self.current_dst_id != dc.dst or need_src_barrier)) {
            self.endRenderPass();
        }

        const cb = try self.ensureRecording(diag);

        // Transition any source images to SHADER_READ_ONLY_OPTIMAL (includes COLOR_ATTACHMENT_WRITE -> SHADER_READ).
        if (need_src_barrier) {
            for (src_imgs) |src_opt| {
                const s = src_opt orelse continue;
                if (s.layout == .undefined) {
                    self.cmdClearUndefinedImage(cb, s);
                }
                if (s.layout != .shader_read_only_optimal) {
                    self.cmdTransitionImage(cb, s.vk_image, s.layout, .shader_read_only_optimal, false);
                    s.layout = .shader_read_only_optimal;
                }
            }
        }

        // Begin render pass if not already active on `dst`.
        if (!self.in_render_pass) {
            var rp_format: vk.Format = dst.vk_format;
            var rp_fb: vk.Framebuffer = dst.framebuffer;
            var rp_extent = vk.Extent2D{ .width = dst.internal_width, .height = dst.internal_height };
            var load_op: vk.AttachmentLoadOp = .load;

            if (target_surf) |surf| {
                const sc_img = surf.currentSwapchainImage() orelse return;
                rp_format = surf.format;
                rp_fb = sc_img.framebuffer;
                rp_extent = surf.extent;
                if (sc_img.layout != .color_attachment_optimal) {
                    const is_acquire = (sc_img.layout == .undefined);
                    self.cmdTransitionImage(cb, sc_img.image, sc_img.layout, .color_attachment_optimal, is_acquire);
                    sc_img.layout = .color_attachment_optimal;
                }
                if (!surf.cleared) {
                    load_op = .clear;
                    surf.cleared = true;
                }
                surf.drawn = true;
                try self.addDrawnSurface(dst.surface_id);
            } else {
                if (dst.layout == .undefined) {
                    self.cmdTransitionImage(cb, dst.vk_image, .undefined, .color_attachment_optimal, false);
                    dst.layout = .color_attachment_optimal;
                    load_op = .clear;
                } else if (dst.layout != .color_attachment_optimal) {
                    self.cmdTransitionImage(cb, dst.vk_image, dst.layout, .color_attachment_optimal, false);
                    dst.layout = .color_attachment_optimal;
                }
            }

            const rp = try self.rp_cache.getOrCreate(self.gpa, &self.dev, .{
                .format = rp_format,
                .load_op = load_op,
            }, diag);

            const clear_val = vk.ClearValue{ .color = .{ .float32 = .{ 0, 0, 0, 0 } } };
            const rp_begin = vk.RenderPassBeginInfo{
                .renderPass = rp,
                .framebuffer = rp_fb,
                .renderArea = .{
                    .offset = .{ .x = 0, .y = 0 },
                    .extent = rp_extent,
                },
                .clearValueCount = if (load_op == .clear) 1 else 0,
                .pClearValues = if (load_op == .clear) @ptrCast(&clear_val) else null,
            };
            self.dev.disp.vkCmdBeginRenderPass(cb, &rp_begin, .@"inline");

            // Dynamic viewport with positive height (Vulkan FlipsY=false).
            const vp = vk.Viewport{
                .x = 0,
                .y = 0,
                .width = @floatFromInt(rp_extent.width),
                .height = @floatFromInt(rp_extent.height),
                .minDepth = 0,
                .maxDepth = 1,
            };
            self.dev.disp.vkCmdSetViewport(cb, 0, 1, @ptrCast(&vp));

            self.in_render_pass = true;
            self.current_dst_id = dc.dst;
            self.current_dst_is_screen = dst.is_screen;
            self.current_dst_surface_id = dst.surface_id;
            self.current_dst_extent = rp_extent;
            self.cache.viewport = .{ rp_extent.width, rp_extent.height };
            self.cache.scissor_valid = false;
        }

        // Bind graphics pipeline (skip if unchanged).
        const pipe = try self.pipelines.getOrCreatePipeline(
            self.gpa,
            &self.dev,
            self.descs.pipeline_layout,
            &self.rp_cache,
            dc.shader,
            shader,
            dc.blend,
            dst.vk_format,
            diag,
        );
        const pipe_handle: usize = @intCast(@intFromEnum(pipe));
        if (self.cache.pipeline == pipe_handle) {
            self.binds_skipped += 1;
        } else {
            self.dev.disp.vkCmdBindPipeline(cb, .graphics, pipe);
            self.cache.pipeline = pipe_handle;
        }

        // Bind vertex & index buffers if not already bound.
        if (self.current_vb == .null_handle or self.current_ib == .null_handle) {
            diag.set("vulkan: drawTriangles called before setVertices");
            return error.InvalidArgument;
        }
        if (self.vb_bound) {
            self.binds_skipped += 1;
        } else {
            self.dev.disp.vkCmdBindVertexBuffers(cb, 0, 1, @ptrCast(&self.current_vb), @ptrCast(&self.current_vb_offset));
            self.dev.disp.vkCmdBindIndexBuffer(cb, self.current_ib, self.current_ib_offset, .uint32);
            self.vb_bound = true;
        }

        // Upload internal & user uniforms only when their versions change.
        const ubo_align = @max(self.dev.props.limits.minUniformBufferOffsetAlignment, 16);
        const frame = self.currentFrame();

        var internal_buf: vk.Buffer = undefined;
        var internal_off: u32 = undefined;
        if (self.last_uploaded_internal_ver == dc.internal_version and self.last_uploaded_internal_buf != .null_handle) {
            internal_buf = self.last_uploaded_internal_buf;
            internal_off = self.last_uploaded_internal_off;
        } else {
            const slice = try frame.upload.alloc(
                self.gpa,
                &self.dev,
                &self.mem,
                abi.internal_uniform_bytes,
                ubo_align,
                diag,
            );
            @memcpy(slice.ptr[0..abi.internal_uniform_bytes], std.mem.sliceAsBytes(dc.internal));
            internal_buf = slice.buffer;
            internal_off = std.math.cast(u32, slice.offset) orelse return error.OutOfMemory;
            self.last_uploaded_internal_ver = dc.internal_version;
            self.last_uploaded_internal_buf = internal_buf;
            self.last_uploaded_internal_off = internal_off;
            self.uploaded_bytes += abi.internal_uniform_bytes;
        }

        var user_buf: vk.Buffer = self.descs.dummy_ubo_buf;
        var user_off: u32 = 0;
        var user_range: u64 = descriptors_mod.dummy_user_ubo_size;
        if (dc.user.len > 0) {
            const user_bytes: u64 = @as(u64, dc.user.len) * @sizeOf(u32);
            if (self.last_uploaded_user_ver == dc.user_version and
                self.last_uploaded_user_buf != .null_handle and
                self.last_uploaded_user_range == user_bytes)
            {
                user_buf = self.last_uploaded_user_buf;
                user_off = self.last_uploaded_user_off;
                user_range = user_bytes;
            } else {
                const slice = try frame.upload.alloc(
                    self.gpa,
                    &self.dev,
                    &self.mem,
                    user_bytes,
                    ubo_align,
                    diag,
                );
                @memcpy(slice.ptr[0..@intCast(user_bytes)], std.mem.sliceAsBytes(dc.user));
                user_buf = slice.buffer;
                user_off = std.math.cast(u32, slice.offset) orelse return error.OutOfMemory;
                user_range = user_bytes;
                self.last_uploaded_user_ver = dc.user_version;
                self.last_uploaded_user_buf = user_buf;
                self.last_uploaded_user_off = user_off;
                self.last_uploaded_user_range = user_range;
                self.uploaded_bytes += user_bytes;
            }
        }

        // Bind descriptor set (set 0) with dynamic offsets; skip completely if tuple & versions match.
        const same_views = std.mem.eql(vk.ImageView, &self.bound_src_views, &src_views);
        const same_bufs = (self.bound_internal_buf == internal_buf and
            self.bound_user_buf == user_buf and
            self.bound_user_range == user_range);
        const same_offsets = (self.bound_internal_offset == internal_off and
            self.bound_user_offset == user_off);

        if (self.has_bound_desc and same_views and same_bufs and same_offsets) {
            self.binds_skipped += 1;
        } else {
            const desc_set: vk.DescriptorSet = if (self.has_bound_desc and same_views and same_bufs)
                self.bound_desc_set
            else
                try frame.desc_pool.getOrCreateSet(self.gpa, &self.dev, self.descs.set_layout, .{
                    .internal_buf = internal_buf,
                    .user_buf = user_buf,
                    .user_range = user_range,
                    .views = src_views,
                }, diag);

            const dyn_offsets = [_]u32{ internal_off, user_off };
            self.dev.disp.vkCmdBindDescriptorSets(
                cb,
                .graphics,
                self.descs.pipeline_layout,
                descriptors_mod.descriptor_set_index,
                1,
                @ptrCast(&desc_set),
                dyn_offsets.len,
                &dyn_offsets,
            );

            self.bound_desc_set = desc_set;
            self.bound_internal_buf = internal_buf;
            self.bound_user_buf = user_buf;
            self.bound_user_range = user_range;
            self.bound_internal_offset = internal_off;
            self.bound_user_offset = user_off;
            self.bound_src_views = src_views;
            self.bound_internal_version = dc.internal_version;
            self.bound_user_version = dc.user_version;
            self.has_bound_desc = true;
        }

        // Emit indexed draws per scissor region.
        const max_w: i32 = @intCast(self.current_dst_extent.width);
        const max_h: i32 = @intCast(self.current_dst_extent.height);
        var idx_off: u32 = dc.index_offset;

        for (dc.regions) |r| {
            defer idx_off +%= r.index_count;
            if (r.index_count == 0) continue;

            const x0 = @max(r.rect.x0, 0);
            const y0 = @max(r.rect.y0, 0);
            const x1 = @min(r.rect.x1, max_w);
            const y1 = @min(r.rect.y1, max_h);
            if (x1 <= x0 or y1 <= y0) continue;

            const clamped = abi.Rect{ .x0 = x0, .y0 = y0, .x1 = x1, .y1 = y1 };
            if (self.cache.scissor_valid and self.cache.scissor.eql(clamped)) {
                self.binds_skipped += 1;
            } else {
                const scissor = vk.Rect2D{
                    .offset = .{ .x = x0, .y = y0 },
                    .extent = .{ .width = @intCast(x1 - x0), .height = @intCast(y1 - y0) },
                };
                self.dev.disp.vkCmdSetScissor(cb, 0, 1, @ptrCast(&scissor));
                self.cache.scissor = clamped;
                self.cache.scissor_valid = true;
            }

            self.dev.disp.vkCmdDrawIndexed(cb, r.index_count, 1, idx_off, 0, 0);
            self.draws += 1;
        }
    }

    pub fn setDamage(self: *Backend, surface_id: u32, rects: ?[]align(4) const abi.Rect) Error!void {
        _ = rects;
        if (self.lookupSurface(surface_id) == null) return error.UnknownHandle;
    }

    pub fn createShader(self: *Backend, id: u32, desc: *const abi.ShaderDesc, diag: *Diag) Error!void {
        if (id == 0) {
            diag.set("vulkan: shader id must not be 0");
            return error.InvalidArgument;
        }
        if (self.shaders.getOpt(id) != null) {
            diag.print("vulkan: duplicate shader id {d}", .{id});
            return error.DuplicateHandle;
        }
        const gen = self.next_shader_gen;
        self.next_shader_gen += 1;

        const sh = try self.pipelines.createShader(self.gpa, &self.dev, gen, desc, diag);
        errdefer {
            if (sh.frag_module != .null_handle) {
                self.dev.disp.vkDestroyShaderModule(self.dev.handle, sh.frag_module, null);
            }
            self.gpa.free(sh.vert_entry);
            self.gpa.free(sh.frag_entry);
        }

        try self.shaders.put(self.gpa, id, sh);
    }

    pub fn readPixels(
        self: *Backend,
        id: u32,
        regions: []const abi.ReadRegion,
        diag: *Diag,
    ) Error!void {
        const img = try self.images.get(id);
        if (img.is_screen) {
            diag.set("vulkan: cannot readPixels from a screen image");
            return error.InvalidArgument;
        }
        const bpp: u32 = img.format.bytesPerPixel();
        const copy_align = @max(@max(self.dev.props.limits.optimalBufferCopyOffsetAlignment, bpp), 4);

        var total_bytes: u64 = 0;
        var has_non_empty = false;
        for (regions) |r| {
            const bounds = validateRect(r.rect, img.internal_width, img.internal_height) orelse {
                diag.print("vulkan: readPixels rect ({d},{d})-({d},{d}) out of bounds {d}x{d}", .{
                    r.rect.x0, r.rect.y0, r.rect.x1, r.rect.y1, img.internal_width, img.internal_height,
                });
                return error.InvalidArgument;
            };
            const expected_len = @as(u64, bounds.dx) * @as(u64, bounds.dy) * bpp;
            if (r.dst_len != expected_len) {
                diag.print("vulkan: readPixels dst_len {d} != expected {d}", .{ r.dst_len, expected_len });
                return error.InvalidArgument;
            }
            if (expected_len > 0) {
                if (r.dst == null) {
                    diag.set("vulkan: readPixels dst pointer is null");
                    return error.InvalidArgument;
                }
                total_bytes = memory_mod.alignUp(total_bytes, copy_align) + expected_len;
                has_non_empty = true;
            }
        }
        if (!has_non_empty) return;

        self.endRenderPass();
        try self.readback.ensure(&self.dev, &self.mem, total_bytes, diag);

        const cb = try self.ensureRecording(diag);
        if (img.layout == .undefined) {
            self.cmdClearUndefinedImage(cb, img);
        }
        if (img.layout != .transfer_src_optimal) {
            self.cmdTransitionImage(cb, img.vk_image, img.layout, .transfer_src_optimal, false);
            img.layout = .transfer_src_optimal;
        }

        const offsets = self.gpa.alloc(u64, regions.len) catch return error.OutOfMemory;
        defer self.gpa.free(offsets);

        var cur_off: u64 = 0;
        for (regions, 0..) |r, i| {
            const bounds = validateRect(r.rect, img.internal_width, img.internal_height).?;
            if (bounds.dx == 0 or bounds.dy == 0 or r.dst_len == 0) {
                offsets[i] = 0;
                continue;
            }
            cur_off = memory_mod.alignUp(cur_off, copy_align);
            offsets[i] = cur_off;

            const copy_region = vk.BufferImageCopy{
                .bufferOffset = cur_off,
                .bufferRowLength = 0,
                .bufferImageHeight = 0,
                .imageSubresource = .{
                    .aspectMask = vk.ImageAspect.color_bit,
                    .mipLevel = 0,
                    .baseArrayLayer = 0,
                    .layerCount = 1,
                },
                .imageOffset = .{ .x = @intCast(bounds.x0), .y = @intCast(bounds.y0), .z = 0 },
                .imageExtent = .{ .width = bounds.dx, .height = bounds.dy, .depth = 1 },
            };
            self.dev.disp.vkCmdCopyImageToBuffer(
                cb,
                img.vk_image,
                .transfer_src_optimal,
                self.readback.buffer,
                1,
                @ptrCast(&copy_region),
            );
            cur_off += r.dst_len;
        }

        try self.submitActiveCommandBuffer(&.{}, diag);

        // Wait for all submitted work in the current frame slot to finish before reading back.
        const frame = self.currentFrame();
        for (frame.cmd_entries.items) |*entry| {
            if (entry.submitted) {
                try loader_mod.check(
                    self.dev.disp.vkWaitForFences(self.dev.handle, 1, @ptrCast(&entry.fence), vk.TRUE, std.math.maxInt(u64)),
                    diag,
                    "vkWaitForFences(readPixels)",
                );
                entry.submitted = false;
            }
        }

        self.readback.invalidate(&self.dev, total_bytes);
        const mapped = self.readback.alloc.mapped orelse return error.Internal;

        for (regions, 0..) |r, i| {
            if (r.dst_len == 0) continue;
            const dst_ptr = r.dst.?;
            const off: usize = @intCast(offsets[i]);
            const len: usize = @intCast(r.dst_len);
            @memcpy(dst_ptr[0..len], mapped[off .. off + len]);
        }
    }

    pub fn finishForcedFrame(self: *Backend, diag: *Diag) Error!void {
        if (self.active_cb != null) {
            try self.submitActiveCommandBuffer(&.{}, diag);
        }
        try loader_mod.check(
            self.dev.disp.vkQueueWaitIdle(self.dev.graphics_queue),
            diag,
            "vkQueueWaitIdle(finishForcedFrame)",
        );
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
        if (id == 0) {
            diag.set("vulkan: surface id must not be 0");
            return error.InvalidArgument;
        }

        // Check for duplicate before allocating Vulkan surface.
        self.surfaces_lock.lock();
        const dup = self.surfaces.getOpt(id) != null;
        self.surfaces_lock.unlock();
        if (dup) {
            diag.print("vulkan: duplicate surface id {d}", .{id});
            return error.DuplicateHandle;
        }

        const surf = try swapchain_mod.Surface.init(self.gpa, &self.inst, &self.dev, desc, caps, diag);
        errdefer surf.deinit(self.gpa, &self.inst, &self.dev);

        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        try self.surfaces.put(self.gpa, id, surf);
    }

    pub fn setWindowState(self: *Backend, id: u32, flags: u32) void {
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        if (self.surfaces.getOpt(id)) |slot| {
            slot.*.window_state.store(flags, .release);
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
        _ = self;
        return false;
    }

    pub fn acquireUploadArena(
        self: *Backend,
        min_bytes: u64,
        out_arena: *abi.UploadArena,
        diag: *Diag,
    ) Error!void {
        if (min_bytes > upload_mod.max_shared_arena_size) {
            diag.print("vulkan: requested shared upload arena size ({d} B) exceeds maximum ({d} B)", .{
                min_bytes,
                upload_mod.max_shared_arena_size,
            });
            return error.OutOfMemory;
        }

        const raw_align: u32 = @intCast(@min(self.dev.props.limits.optimalBufferCopyOffsetAlignment, @as(u64, 4096)));
        const alignment: u32 = std.math.ceilPowerOfTwoAssert(u32, @max(@as(u32, 16), raw_align));
        const capacity: u64 = memory_mod.alignUp(@max(min_bytes, upload_mod.default_shared_arena_size), 16);

        const new_chunk = try upload_mod.createSharedChunk(&self.dev, &self.mem, capacity, diag);
        errdefer upload_mod.destroyChunk(&self.dev, &self.mem, new_chunk);

        const mapped_ptr = new_chunk.alloc.mapped orelse {
            diag.set("vulkan: shared upload arena memory is not mapped");
            return error.Internal;
        };

        if (self.shared_upload_chunk) |old_chunk| {
            try self.currentFrame().upload.retireChunk(self.gpa, old_chunk);
        }

        self.shared_upload_chunk = new_chunk;
        self.shared_arena_id +%= 1;
        if (self.shared_arena_id == 0) self.shared_arena_id = 1;

        out_arena.* = .{
            .struct_size = @sizeOf(abi.UploadArena),
            .arena_id = self.shared_arena_id,
            .ptr = mapped_ptr,
            .capacity = new_chunk.capacity,
            .alignment = alignment,
            .flags = abi.arena_flag_gpu_mapped | abi.arena_flag_coherent,
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
            .live_pipelines = self.pipelines.liveCount(),
            .submits = self.submits,
            .draws = self.draws,
            .binds_skipped = self.binds_skipped,
            .uploaded_bytes = self.uploaded_bytes,
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
