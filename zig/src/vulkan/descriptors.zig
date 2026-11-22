const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const device_mod = @import("device.zig");
const memory_mod = @import("memory.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

// Matches internal/graphicsdriver/shaderlayout.go:
//   set 0, binding 0:    UNIFORM_BUFFER_DYNAMIC (VERTEX | FRAGMENT) - 288B internal uniform
//   set 0, binding 1..4: SAMPLED_IMAGE          (FRAGMENT)          - 4 source textures
//   set 0, binding 5:    UNIFORM_BUFFER_DYNAMIC (FRAGMENT)          - user uniform
pub const descriptor_set_index: u32 = 0;
pub const internal_uniform_binding: u32 = 0;
pub const texture_binding_base: u32 = 1;
pub const user_uniform_binding: u32 = 5;
pub const dummy_user_ubo_size: u64 = 256;

pub const DescriptorKey = struct {
    internal_buf: vk.Buffer,
    user_buf: vk.Buffer,
    user_range: u64,
    views: [abi.max_src_images]vk.ImageView,
};

/// Per-frame growing descriptor pool with within-frame DescriptorKey -> DescriptorSet cache.
pub const DescriptorPoolRing = struct {
    pools: std.ArrayList(vk.DescriptorPool) = .empty,
    active_idx: usize = 0,
    next_max_sets: u32 = 256,
    cache: std.AutoHashMapUnmanaged(DescriptorKey, vk.DescriptorSet) = .empty,

    pub fn getOrCreateSet(
        self: *DescriptorPoolRing,
        gpa: Allocator,
        dev: *const device_mod.Device,
        set_layout: vk.DescriptorSetLayout,
        key: DescriptorKey,
        diag: *Diag,
    ) Error!vk.DescriptorSet {
        if (self.cache.get(key)) |existing| {
            return existing;
        }

        const set = try self.allocateSet(gpa, dev, set_layout, diag);

        const internal_info = vk.DescriptorBufferInfo{
            .buffer = key.internal_buf,
            .offset = 0,
            .range = abi.internal_uniform_bytes,
        };
        const user_info = vk.DescriptorBufferInfo{
            .buffer = key.user_buf,
            .offset = 0,
            .range = key.user_range,
        };
        var img_infos: [abi.max_src_images]vk.DescriptorImageInfo = undefined;
        for (0..abi.max_src_images) |i| {
            img_infos[i] = .{
                .sampler = .null_handle,
                .imageView = key.views[i],
                .imageLayout = .shader_read_only_optimal,
            };
        }

        const writes = [_]vk.WriteDescriptorSet{
            .{
                .dstSet = set,
                .dstBinding = internal_uniform_binding,
                .dstArrayElement = 0,
                .descriptorCount = 1,
                .descriptorType = .uniform_buffer_dynamic,
                .pBufferInfo = @ptrCast(&internal_info),
            },
            .{
                .dstSet = set,
                .dstBinding = texture_binding_base + 0,
                .dstArrayElement = 0,
                .descriptorCount = 1,
                .descriptorType = .sampled_image,
                .pImageInfo = @ptrCast(&img_infos[0]),
            },
            .{
                .dstSet = set,
                .dstBinding = texture_binding_base + 1,
                .dstArrayElement = 0,
                .descriptorCount = 1,
                .descriptorType = .sampled_image,
                .pImageInfo = @ptrCast(&img_infos[1]),
            },
            .{
                .dstSet = set,
                .dstBinding = texture_binding_base + 2,
                .dstArrayElement = 0,
                .descriptorCount = 1,
                .descriptorType = .sampled_image,
                .pImageInfo = @ptrCast(&img_infos[2]),
            },
            .{
                .dstSet = set,
                .dstBinding = texture_binding_base + 3,
                .dstArrayElement = 0,
                .descriptorCount = 1,
                .descriptorType = .sampled_image,
                .pImageInfo = @ptrCast(&img_infos[3]),
            },
            .{
                .dstSet = set,
                .dstBinding = user_uniform_binding,
                .dstArrayElement = 0,
                .descriptorCount = 1,
                .descriptorType = .uniform_buffer_dynamic,
                .pBufferInfo = @ptrCast(&user_info),
            },
        };

        dev.disp.vkUpdateDescriptorSets(dev.handle, writes.len, &writes, 0, null);
        self.cache.put(gpa, key, set) catch return error.OutOfMemory;
        return set;
    }

    fn allocateSet(
        self: *DescriptorPoolRing,
        gpa: Allocator,
        dev: *const device_mod.Device,
        set_layout: vk.DescriptorSetLayout,
        diag: *Diag,
    ) Error!vk.DescriptorSet {
        if (self.pools.items.len == 0) {
            try self.addPool(gpa, dev, diag);
        }

        while (true) {
            const pool = self.pools.items[self.active_idx];
            const ai = vk.DescriptorSetAllocateInfo{
                .descriptorPool = pool,
                .descriptorSetCount = 1,
                .pSetLayouts = @ptrCast(&set_layout),
            };
            var set: vk.DescriptorSet = .null_handle;
            const res = dev.disp.vkAllocateDescriptorSets(dev.handle, &ai, @ptrCast(&set));
            if (res == .success) return set;

            if (res == .error_out_of_pool_memory or res == .error_fragmented_pool) {
                if (self.active_idx + 1 < self.pools.items.len) {
                    self.active_idx += 1;
                } else {
                    try self.addPool(gpa, dev, diag);
                    self.active_idx = self.pools.items.len - 1;
                }
                continue;
            }
            try loader_mod.check(res, diag, "vkAllocateDescriptorSets");
            unreachable;
        }
    }

    fn addPool(self: *DescriptorPoolRing, gpa: Allocator, dev: *const device_mod.Device, diag: *Diag) Error!void {
        const max_sets = self.next_max_sets;
        const pool_sizes = [_]vk.DescriptorPoolSize{
            .{ .type = .uniform_buffer_dynamic, .descriptorCount = max_sets * 2 },
            .{ .type = .sampled_image, .descriptorCount = max_sets * 4 },
        };
        const ci = vk.DescriptorPoolCreateInfo{
            .maxSets = max_sets,
            .poolSizeCount = pool_sizes.len,
            .pPoolSizes = &pool_sizes,
        };
        var pool: vk.DescriptorPool = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateDescriptorPool(dev.handle, &ci, null, &pool),
            diag,
            "vkCreateDescriptorPool",
        );
        errdefer dev.disp.vkDestroyDescriptorPool(dev.handle, pool, null);

        self.pools.append(gpa, pool) catch return error.OutOfMemory;
        self.next_max_sets = @min(max_sets * 2, 8192);
    }

    pub fn reset(self: *DescriptorPoolRing, dev: *const device_mod.Device) void {
        for (self.pools.items) |pool| {
            _ = dev.disp.vkResetDescriptorPool(dev.handle, pool, 0);
        }
        self.active_idx = 0;
        self.cache.clearRetainingCapacity();
    }

    pub fn deinit(self: *DescriptorPoolRing, gpa: Allocator, dev: *const device_mod.Device) void {
        for (self.pools.items) |pool| {
            if (pool != .null_handle) {
                dev.disp.vkDestroyDescriptorPool(dev.handle, pool, null);
            }
        }
        self.pools.deinit(gpa);
        self.cache.deinit(gpa);
    }
};

/// Owns the single descriptor set layout, pipeline layout, 1x1 dummy RGBA8 texture, and 256B dummy UBO.
pub const DescriptorManager = struct {
    set_layout: vk.DescriptorSetLayout,
    pipeline_layout: vk.PipelineLayout,

    dummy_image: vk.Image,
    dummy_alloc: memory_mod.Allocation,
    dummy_view: vk.ImageView,

    dummy_ubo_buf: vk.Buffer,
    dummy_ubo_alloc: memory_mod.Allocation,

    pub fn init(dev: *const device_mod.Device, mem: *memory_mod.MemoryAllocator, diag: *Diag) Error!DescriptorManager {
        const bindings = [_]vk.DescriptorSetLayoutBinding{
            .{
                .binding = internal_uniform_binding,
                .descriptorType = .uniform_buffer_dynamic,
                .descriptorCount = 1,
                .stageFlags = vk.ShaderStage.vertex_bit | vk.ShaderStage.fragment_bit,
            },
            .{
                .binding = texture_binding_base + 0,
                .descriptorType = .sampled_image,
                .descriptorCount = 1,
                .stageFlags = vk.ShaderStage.fragment_bit,
            },
            .{
                .binding = texture_binding_base + 1,
                .descriptorType = .sampled_image,
                .descriptorCount = 1,
                .stageFlags = vk.ShaderStage.fragment_bit,
            },
            .{
                .binding = texture_binding_base + 2,
                .descriptorType = .sampled_image,
                .descriptorCount = 1,
                .stageFlags = vk.ShaderStage.fragment_bit,
            },
            .{
                .binding = texture_binding_base + 3,
                .descriptorType = .sampled_image,
                .descriptorCount = 1,
                .stageFlags = vk.ShaderStage.fragment_bit,
            },
            .{
                .binding = user_uniform_binding,
                .descriptorType = .uniform_buffer_dynamic,
                .descriptorCount = 1,
                .stageFlags = vk.ShaderStage.fragment_bit,
            },
        };

        const dsl_ci = vk.DescriptorSetLayoutCreateInfo{
            .bindingCount = bindings.len,
            .pBindings = &bindings,
        };
        var set_layout: vk.DescriptorSetLayout = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateDescriptorSetLayout(dev.handle, &dsl_ci, null, &set_layout),
            diag,
            "vkCreateDescriptorSetLayout",
        );
        errdefer dev.disp.vkDestroyDescriptorSetLayout(dev.handle, set_layout, null);

        const pl_ci = vk.PipelineLayoutCreateInfo{
            .setLayoutCount = 1,
            .pSetLayouts = @ptrCast(&set_layout),
        };
        var pipeline_layout: vk.PipelineLayout = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreatePipelineLayout(dev.handle, &pl_ci, null, &pipeline_layout),
            diag,
            "vkCreatePipelineLayout",
        );
        errdefer dev.disp.vkDestroyPipelineLayout(dev.handle, pipeline_layout, null);

        // 1x1 dummy RGBA8 image for unused texture slots.
        const img_ci = vk.ImageCreateInfo{
            .imageType = .@"2d",
            .format = .r8g8b8a8_unorm,
            .extent = .{ .width = 1, .height = 1, .depth = 1 },
            .mipLevels = 1,
            .arrayLayers = 1,
            .samples = vk.SampleCount.@"1_bit",
            .tiling = .optimal,
            .usage = vk.ImageUsage.sampled_bit | vk.ImageUsage.transfer_dst_bit,
            .sharingMode = .exclusive,
            .initialLayout = .undefined,
        };
        var dummy_image: vk.Image = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateImage(dev.handle, &img_ci, null, &dummy_image),
            diag,
            "vkCreateImage(dummy)",
        );
        errdefer dev.disp.vkDestroyImage(dev.handle, dummy_image, null);

        var img_req: vk.MemoryRequirements = undefined;
        dev.disp.vkGetImageMemoryRequirements(dev.handle, dummy_image, &img_req);
        const dummy_alloc = try mem.alloc(
            img_req,
            vk.MemoryProperty.device_local_bit,
            0,
            false,
            false,
            false,
            diag,
        );
        errdefer mem.free(dummy_alloc);

        try loader_mod.check(
            dev.disp.vkBindImageMemory(dev.handle, dummy_image, dummy_alloc.memory, dummy_alloc.offset),
            diag,
            "vkBindImageMemory(dummy)",
        );

        const view_ci = vk.ImageViewCreateInfo{
            .image = dummy_image,
            .viewType = .@"2d",
            .format = .r8g8b8a8_unorm,
            .components = .{},
            .subresourceRange = .{
                .aspectMask = vk.ImageAspect.color_bit,
                .baseMipLevel = 0,
                .levelCount = 1,
                .baseArrayLayer = 0,
                .layerCount = 1,
            },
        };
        var dummy_view: vk.ImageView = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateImageView(dev.handle, &view_ci, null, &dummy_view),
            diag,
            "vkCreateImageView(dummy)",
        );
        errdefer dev.disp.vkDestroyImageView(dev.handle, dummy_view, null);

        // 256-byte zeroed dummy UBO for shaders without user uniforms.
        const buf_ci = vk.BufferCreateInfo{
            .size = dummy_user_ubo_size,
            .usage = vk.BufferUsage.uniform_buffer_bit,
            .sharingMode = .exclusive,
        };
        var dummy_ubo_buf: vk.Buffer = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateBuffer(dev.handle, &buf_ci, null, &dummy_ubo_buf),
            diag,
            "vkCreateBuffer(dummy_ubo)",
        );
        errdefer dev.disp.vkDestroyBuffer(dev.handle, dummy_ubo_buf, null);

        var buf_req: vk.MemoryRequirements = undefined;
        dev.disp.vkGetBufferMemoryRequirements(dev.handle, dummy_ubo_buf, &buf_req);
        const dummy_ubo_alloc = try mem.alloc(
            buf_req,
            vk.MemoryProperty.host_visible_bit | vk.MemoryProperty.host_coherent_bit,
            0,
            true,
            true,
            false,
            diag,
        );
        errdefer mem.free(dummy_ubo_alloc);

        try loader_mod.check(
            dev.disp.vkBindBufferMemory(dev.handle, dummy_ubo_buf, dummy_ubo_alloc.memory, dummy_ubo_alloc.offset),
            diag,
            "vkBindBufferMemory(dummy_ubo)",
        );
        if (dummy_ubo_alloc.mapped) |p| {
            @memset(p[0..dummy_user_ubo_size], 0);
        }

        return .{
            .set_layout = set_layout,
            .pipeline_layout = pipeline_layout,
            .dummy_image = dummy_image,
            .dummy_alloc = dummy_alloc,
            .dummy_view = dummy_view,
            .dummy_ubo_buf = dummy_ubo_buf,
            .dummy_ubo_alloc = dummy_ubo_alloc,
        };
    }

    /// Transitions the 1x1 dummy image to SHADER_READ_ONLY_OPTIMAL with transparent black contents.
    pub fn initDummyImageLayout(
        self: *const DescriptorManager,
        dev: *const device_mod.Device,
        cmd_pool: vk.CommandPool,
        diag: *Diag,
    ) Error!void {
        const alloc_ci = vk.CommandBufferAllocateInfo{
            .commandPool = cmd_pool,
            .level = .primary,
            .commandBufferCount = 1,
        };
        var cb: vk.CommandBuffer = null;
        try loader_mod.check(
            dev.disp.vkAllocateCommandBuffers(dev.handle, &alloc_ci, @ptrCast(&cb)),
            diag,
            "vkAllocateCommandBuffers(dummy_init)",
        );
        defer dev.disp.vkFreeCommandBuffers(dev.handle, cmd_pool, 1, @ptrCast(&cb));

        const begin_ci = vk.CommandBufferBeginInfo{
            .flags = vk.CommandBufferUsage.one_time_submit_bit,
        };
        try loader_mod.check(
            dev.disp.vkBeginCommandBuffer(cb, &begin_ci),
            diag,
            "vkBeginCommandBuffer(dummy_init)",
        );

        const range = vk.ImageSubresourceRange{
            .aspectMask = vk.ImageAspect.color_bit,
            .baseMipLevel = 0,
            .levelCount = 1,
            .baseArrayLayer = 0,
            .layerCount = 1,
        };

        const to_dst = vk.ImageMemoryBarrier{
            .srcAccessMask = 0,
            .dstAccessMask = vk.Access.transfer_write_bit,
            .oldLayout = .undefined,
            .newLayout = .transfer_dst_optimal,
            .image = self.dummy_image,
            .subresourceRange = range,
        };
        dev.disp.vkCmdPipelineBarrier(
            cb,
            vk.PipelineStage.top_of_pipe_bit,
            vk.PipelineStage.transfer_bit,
            0,
            0,
            null,
            0,
            null,
            1,
            @ptrCast(&to_dst),
        );

        const clear_color = vk.ClearColorValue{ .float32 = .{ 0, 0, 0, 0 } };
        dev.disp.vkCmdClearColorImage(
            cb,
            self.dummy_image,
            .transfer_dst_optimal,
            &clear_color,
            1,
            @ptrCast(&range),
        );

        const to_read = vk.ImageMemoryBarrier{
            .srcAccessMask = vk.Access.transfer_write_bit,
            .dstAccessMask = vk.Access.shader_read_bit,
            .oldLayout = .transfer_dst_optimal,
            .newLayout = .shader_read_only_optimal,
            .image = self.dummy_image,
            .subresourceRange = range,
        };
        dev.disp.vkCmdPipelineBarrier(
            cb,
            vk.PipelineStage.transfer_bit,
            vk.PipelineStage.fragment_shader_bit,
            0,
            0,
            null,
            0,
            null,
            1,
            @ptrCast(&to_read),
        );

        try loader_mod.check(
            dev.disp.vkEndCommandBuffer(cb),
            diag,
            "vkEndCommandBuffer(dummy_init)",
        );

        const submit_info = vk.SubmitInfo{
            .commandBufferCount = 1,
            .pCommandBuffers = @ptrCast(&cb),
        };
        try loader_mod.check(
            dev.disp.vkQueueSubmit(dev.graphics_queue, 1, @ptrCast(&submit_info), .null_handle),
            diag,
            "vkQueueSubmit(dummy_init)",
        );
        try loader_mod.check(
            dev.disp.vkQueueWaitIdle(dev.graphics_queue),
            diag,
            "vkQueueWaitIdle(dummy_init)",
        );
    }

    pub fn deinit(self: *DescriptorManager, dev: *const device_mod.Device, mem: *memory_mod.MemoryAllocator) void {
        if (self.dummy_ubo_buf != .null_handle) {
            dev.disp.vkDestroyBuffer(dev.handle, self.dummy_ubo_buf, null);
            self.dummy_ubo_buf = .null_handle;
        }
        mem.free(self.dummy_ubo_alloc);
        self.dummy_ubo_alloc = .{};

        if (self.dummy_view != .null_handle) {
            dev.disp.vkDestroyImageView(dev.handle, self.dummy_view, null);
            self.dummy_view = .null_handle;
        }
        if (self.dummy_image != .null_handle) {
            dev.disp.vkDestroyImage(dev.handle, self.dummy_image, null);
            self.dummy_image = .null_handle;
        }
        mem.free(self.dummy_alloc);
        self.dummy_alloc = .{};

        if (self.pipeline_layout != .null_handle) {
            dev.disp.vkDestroyPipelineLayout(dev.handle, self.pipeline_layout, null);
            self.pipeline_layout = .null_handle;
        }
        if (self.set_layout != .null_handle) {
            dev.disp.vkDestroyDescriptorSetLayout(dev.handle, self.set_layout, null);
            self.set_layout = .null_handle;
        }
    }
};
