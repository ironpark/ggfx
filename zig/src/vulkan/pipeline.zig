const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const vertex_cache = @import("../vertex_cache.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const device_mod = @import("device.zig");
const renderpass_mod = @import("renderpass.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

const spirv_magic: u32 = 0x07230203;
const vertex_stride_bytes: u32 = 48; // 12 * sizeof(f32)

pub const Shader = struct {
    gen: u64,
    texture_count: u32,
    user_uniform_dwords: u32,
    flags: u32,
    vert_module: vk.ShaderModule, // owned by PipelineCache.vertex_cache
    frag_module: vk.ShaderModule, // owned by Shader
    vert_entry: [:0]u8,
    frag_entry: [:0]u8,
};

pub const PipelineKey = struct {
    shader_id: u32,
    shader_gen: u64,
    blend: abi.Blend,
    format: vk.Format,
};

pub fn mapBlendFactor(f: u8) Error!vk.BlendFactor {
    // Matches internal/graphicsdriver/blend.go BlendFactor iota order.
    return switch (f) {
        0 => .zero,
        1 => .one,
        2 => .src_color,
        3 => .one_minus_src_color,
        4 => .src_alpha,
        5 => .one_minus_src_alpha,
        6 => .dst_color,
        7 => .one_minus_dst_color,
        8 => .dst_alpha,
        9 => .one_minus_dst_alpha,
        10 => .src_alpha_saturate,
        else => error.InvalidArgument,
    };
}

pub fn mapBlendOp(op: u8) Error!vk.BlendOp {
    // Matches internal/graphicsdriver/blend.go BlendOperation iota order.
    return switch (op) {
        0 => .add,
        1 => .subtract,
        2 => .reverse_subtract,
        3 => .min,
        4 => .max,
        else => error.InvalidArgument,
    };
}

fn validateSpirv(stage_name: []const u8, stage: abi.ShaderStage, diag: *Diag) Error!struct {
    code: []const u8,
    entry: []const u8,
} {
    const code_ptr = stage.code orelse {
        diag.print("vulkan: {s} shader code is null", .{stage_name});
        return error.ShaderCompile;
    };
    const code_len = std.math.cast(usize, stage.code_len) orelse {
        diag.print("vulkan: {s} shader code_len overflow", .{stage_name});
        return error.ShaderCompile;
    };
    if (code_len < 20 or (code_len % 4) != 0) {
        diag.print("vulkan: {s} SPIR-V code_len ({d}) must be >= 20 and multiple of 4", .{ stage_name, code_len });
        return error.ShaderCompile;
    }
    const code = code_ptr[0..code_len];
    const magic = std.mem.readInt(u32, code[0..4], .little);
    if (magic != spirv_magic) {
        diag.print("vulkan: {s} SPIR-V invalid magic 0x{x:0>8} (expected 0x07230203)", .{ stage_name, magic });
        return error.ShaderCompile;
    }
    const entry_ptr = stage.entry orelse {
        diag.print("vulkan: {s} shader entry point is null", .{stage_name});
        return error.InvalidArgument;
    };
    const entry_slice: []const u8 = if (stage.entry_len > 0)
        entry_ptr[0..stage.entry_len]
    else
        std.mem.span(entry_ptr);
    if (entry_slice.len == 0) {
        diag.print("vulkan: {s} shader entry point is empty", .{stage_name});
        return error.InvalidArgument;
    }
    return .{ .code = code, .entry = entry_slice };
}

fn dupeSentinelZ(gpa: Allocator, s: []const u8) Error![:0]u8 {
    const out = gpa.allocSentinel(u8, s.len, 0) catch return error.OutOfMemory;
    @memcpy(out, s);
    return out;
}

fn createShaderModule(
    gpa: Allocator,
    dev: *const device_mod.Device,
    code: []const u8,
    diag: *Diag,
) Error!vk.ShaderModule {
    var aligned_buf: ?[]u32 = null;
    defer if (aligned_buf) |b| gpa.free(b);

    const words_ptr: [*]const u32 = if (@intFromPtr(code.ptr) % @alignOf(u32) == 0)
        @ptrCast(@alignCast(code.ptr))
    else blk: {
        const tmp = gpa.alloc(u32, code.len / 4) catch return error.OutOfMemory;
        aligned_buf = tmp;
        @memcpy(std.mem.sliceAsBytes(tmp), code);
        break :blk tmp.ptr;
    };

    const ci = vk.ShaderModuleCreateInfo{
        .codeSize = code.len,
        .pCode = words_ptr,
    };
    var mod: vk.ShaderModule = .null_handle;
    const res = dev.disp.vkCreateShaderModule(dev.handle, &ci, null, &mod);
    if (res != .success) {
        diag.print("vulkan: vkCreateShaderModule failed (VkResult={d})", .{@intFromEnum(res)});
        return if (res == .error_out_of_host_memory or res == .error_out_of_device_memory)
            error.OutOfMemory
        else
            error.ShaderCompile;
    }
    return mod;
}

fn destroyShaderModule(dev: *const device_mod.Device, mod: *vk.ShaderModule) void {
    if (mod.* != .null_handle) {
        dev.disp.vkDestroyShaderModule(dev.handle, mod.*, null);
    }
}

pub const PipelineCache = struct {
    vk_cache: vk.PipelineCache = .null_handle,
    vertex_cache: vertex_cache.VertexStageCache(vk.ShaderModule) = .{},
    pipelines: std.AutoHashMapUnmanaged(PipelineKey, vk.Pipeline) = .empty,

    pub fn init(dev: *const device_mod.Device, diag: *Diag) Error!PipelineCache {
        const ci = vk.PipelineCacheCreateInfo{};
        var vk_cache: vk.PipelineCache = .null_handle;
        // PipelineCache creation is optional optimization; tolerate failure gracefully unless OOM.
        const res = dev.disp.vkCreatePipelineCache(dev.handle, &ci, null, &vk_cache);
        if (res != .success and res != .error_initialization_failed) {
            try loader_mod.check(res, diag, "vkCreatePipelineCache");
        }
        return .{ .vk_cache = vk_cache };
    }

    pub fn createShader(
        self: *PipelineCache,
        gpa: Allocator,
        dev: *const device_mod.Device,
        gen: u64,
        desc: *const abi.ShaderDesc,
        diag: *Diag,
    ) Error!Shader {
        if (desc.format != @intFromEnum(abi.ShaderFormat.spirv)) {
            diag.print("vulkan: unsupported shader format {d} (expected SPIR-V)", .{desc.format});
            return error.Unsupported;
        }
        if (desc.texture_count > abi.max_src_images) {
            diag.print("vulkan: shader texture_count {d} exceeds {d}", .{ desc.texture_count, abi.max_src_images });
            return error.InvalidArgument;
        }

        const vs = try validateSpirv("vertex", desc.vertex, diag);
        const fs = try validateSpirv("fragment", desc.fragment, diag);

        // Cache vertex shader modules by exact SPIR-V bytes.
        const vert_mod: vk.ShaderModule = if (self.vertex_cache.get(vs.code, "", 0)) |cached|
            cached
        else blk: {
            const created = try createShaderModule(gpa, dev, vs.code, diag);
            errdefer dev.disp.vkDestroyShaderModule(dev.handle, created, null);
            try self.vertex_cache.put(gpa, vs.code, "", 0, created);
            break :blk created;
        };

        const frag_mod = try createShaderModule(gpa, dev, fs.code, diag);
        errdefer dev.disp.vkDestroyShaderModule(dev.handle, frag_mod, null);

        const vert_entry = try dupeSentinelZ(gpa, vs.entry);
        errdefer gpa.free(vert_entry);
        const frag_entry = try dupeSentinelZ(gpa, fs.entry);
        errdefer gpa.free(frag_entry);

        return .{
            .gen = gen,
            .texture_count = desc.texture_count,
            .user_uniform_dwords = desc.user_uniform_dwords,
            .flags = desc.flags,
            .vert_module = vert_mod,
            .frag_module = frag_mod,
            .vert_entry = vert_entry,
            .frag_entry = frag_entry,
        };
    }

    pub fn getOrCreatePipeline(
        self: *PipelineCache,
        gpa: Allocator,
        dev: *const device_mod.Device,
        pipeline_layout: vk.PipelineLayout,
        rp_cache: *renderpass_mod.RenderPassCache,
        shader_id: u32,
        shader: *const Shader,
        blend: abi.Blend,
        color_format: vk.Format,
        diag: *Diag,
    ) Error!vk.Pipeline {
        const key = PipelineKey{
            .shader_id = shader_id,
            .shader_gen = shader.gen,
            .blend = blend,
            .format = color_format,
        };
        if (self.pipelines.get(key)) |pipe| return pipe;

        const src_rgb = mapBlendFactor(blend.src_rgb) catch {
            diag.print("vulkan: invalid blend src_rgb {d}", .{blend.src_rgb});
            return error.InvalidArgument;
        };
        const dst_rgb = mapBlendFactor(blend.dst_rgb) catch {
            diag.print("vulkan: invalid blend dst_rgb {d}", .{blend.dst_rgb});
            return error.InvalidArgument;
        };
        const op_rgb = mapBlendOp(blend.op_rgb) catch {
            diag.print("vulkan: invalid blend op_rgb {d}", .{blend.op_rgb});
            return error.InvalidArgument;
        };
        const src_a = mapBlendFactor(blend.src_a) catch {
            diag.print("vulkan: invalid blend src_a {d}", .{blend.src_a});
            return error.InvalidArgument;
        };
        const dst_a = mapBlendFactor(blend.dst_a) catch {
            diag.print("vulkan: invalid blend dst_a {d}", .{blend.dst_a});
            return error.InvalidArgument;
        };
        const op_a = mapBlendOp(blend.op_a) catch {
            diag.print("vulkan: invalid blend op_a {d}", .{blend.op_a});
            return error.InvalidArgument;
        };

        // Compatible render pass (loadOp does not affect render pass compatibility).
        const compat_rp = try rp_cache.getOrCreate(gpa, dev, .{
            .format = color_format,
            .load_op = .load,
        }, diag);

        const stages = [_]vk.PipelineShaderStageCreateInfo{
            .{
                .stage = vk.ShaderStage.vertex_bit,
                .module = shader.vert_module,
                .pName = shader.vert_entry.ptr,
            },
            .{
                .stage = vk.ShaderStage.fragment_bit,
                .module = shader.frag_module,
                .pName = shader.frag_entry.ptr,
            },
        };

        const binding_desc = vk.VertexInputBindingDescription{
            .binding = 0,
            .stride = vertex_stride_bytes,
            .inputRate = .vertex,
        };
        const attr_descs = [_]vk.VertexInputAttributeDescription{
            .{ .location = 0, .binding = 0, .format = .r32g32_sfloat, .offset = 0 },
            .{ .location = 1, .binding = 0, .format = .r32g32_sfloat, .offset = 8 },
            .{ .location = 2, .binding = 0, .format = .r32g32b32a32_sfloat, .offset = 16 },
            .{ .location = 3, .binding = 0, .format = .r32g32b32a32_sfloat, .offset = 32 },
        };
        const vertex_input = vk.PipelineVertexInputStateCreateInfo{
            .vertexBindingDescriptionCount = 1,
            .pVertexBindingDescriptions = @ptrCast(&binding_desc),
            .vertexAttributeDescriptionCount = attr_descs.len,
            .pVertexAttributeDescriptions = &attr_descs,
        };

        const input_assembly = vk.PipelineInputAssemblyStateCreateInfo{
            .topology = .triangle_list,
            .primitiveRestartEnable = vk.FALSE,
        };

        const viewport_state = vk.PipelineViewportStateCreateInfo{
            .viewportCount = 1,
            .pViewports = null,
            .scissorCount = 1,
            .pScissors = null,
        };

        const rasterization = vk.PipelineRasterizationStateCreateInfo{
            .polygonMode = .fill,
            .cullMode = vk.CullMode.none,
            .frontFace = .counter_clockwise,
            .lineWidth = 1.0,
        };

        const multisample = vk.PipelineMultisampleStateCreateInfo{
            .rasterizationSamples = vk.SampleCount.@"1_bit",
            .sampleShadingEnable = vk.FALSE,
        };

        const color_blend_att = vk.PipelineColorBlendAttachmentState{
            .blendEnable = vk.TRUE,
            .srcColorBlendFactor = src_rgb,
            .dstColorBlendFactor = dst_rgb,
            .colorBlendOp = op_rgb,
            .srcAlphaBlendFactor = src_a,
            .dstAlphaBlendFactor = dst_a,
            .alphaBlendOp = op_a,
            .colorWriteMask = vk.ColorComponent.rgba_bits,
        };
        const color_blend = vk.PipelineColorBlendStateCreateInfo{
            .logicOpEnable = vk.FALSE,
            .attachmentCount = 1,
            .pAttachments = @ptrCast(&color_blend_att),
        };

        const dyn_states = [_]vk.DynamicState{ .viewport, .scissor };
        const dynamic_state = vk.PipelineDynamicStateCreateInfo{
            .dynamicStateCount = dyn_states.len,
            .pDynamicStates = &dyn_states,
        };

        const gp_ci = vk.GraphicsPipelineCreateInfo{
            .stageCount = stages.len,
            .pStages = &stages,
            .pVertexInputState = &vertex_input,
            .pInputAssemblyState = &input_assembly,
            .pViewportState = &viewport_state,
            .pRasterizationState = &rasterization,
            .pMultisampleState = &multisample,
            .pDepthStencilState = null,
            .pColorBlendState = &color_blend,
            .pDynamicState = &dynamic_state,
            .layout = pipeline_layout,
            .renderPass = compat_rp,
            .subpass = 0,
        };

        var pipe: vk.Pipeline = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateGraphicsPipelines(dev.handle, self.vk_cache, 1, @ptrCast(&gp_ci), null, @ptrCast(&pipe)),
            diag,
            "vkCreateGraphicsPipelines",
        );
        errdefer dev.disp.vkDestroyPipeline(dev.handle, pipe, null);

        self.pipelines.put(gpa, key, pipe) catch return error.OutOfMemory;
        return pipe;
    }

    /// Removes all pipelines belonging to `shader_id` and appends them to `deferred` for safe destruction.
    pub fn purgeShaderPipelines(
        self: *PipelineCache,
        gpa: Allocator,
        shader_id: u32,
        shader_gen: u64,
        deferred: *std.ArrayList(vk.Pipeline),
    ) void {
        var to_remove: std.ArrayList(PipelineKey) = .empty;
        defer to_remove.deinit(gpa);

        var it = self.pipelines.iterator();
        while (it.next()) |entry| {
            if (entry.key_ptr.shader_id == shader_id and entry.key_ptr.shader_gen == shader_gen) {
                to_remove.append(gpa, entry.key_ptr.*) catch break;
            }
        }
        for (to_remove.items) |k| {
            if (self.pipelines.fetchRemove(k)) |removed| {
                deferred.append(gpa, removed.value) catch {};
            }
        }
    }

    pub fn liveCount(self: *const PipelineCache) u64 {
        return self.pipelines.count();
    }

    pub fn deinit(self: *PipelineCache, gpa: Allocator, dev: *const device_mod.Device) void {
        var pit = self.pipelines.valueIterator();
        while (pit.next()) |pipe| {
            if (pipe.* != .null_handle) {
                dev.disp.vkDestroyPipeline(dev.handle, pipe.*, null);
            }
        }
        self.pipelines.deinit(gpa);

        self.vertex_cache.deinit(gpa, dev, destroyShaderModule);

        if (self.vk_cache != .null_handle) {
            dev.disp.vkDestroyPipelineCache(dev.handle, self.vk_cache, null);
            self.vk_cache = .null_handle;
        }
    }
};
