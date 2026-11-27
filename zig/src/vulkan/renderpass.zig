const std = @import("std");
const errors = @import("../errors.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const device_mod = @import("device.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

pub const RenderPassKey = struct {
    format: vk.Format,
    load_op: vk.AttachmentLoadOp,
};

pub const RenderPassCache = struct {
    map: std.AutoHashMapUnmanaged(RenderPassKey, vk.RenderPass) = .empty,

    pub fn getOrCreate(
        self: *RenderPassCache,
        gpa: Allocator,
        dev: *const device_mod.Device,
        key: RenderPassKey,
        diag: *Diag,
    ) Error!vk.RenderPass {
        if (self.map.get(key)) |rp| return rp;

        const attachment = vk.AttachmentDescription{
            .format = key.format,
            .samples = vk.SampleCount.@"1_bit",
            .loadOp = key.load_op,
            .storeOp = .store,
            .stencilLoadOp = .dont_care,
            .stencilStoreOp = .dont_care,
            .initialLayout = .color_attachment_optimal,
            .finalLayout = .color_attachment_optimal,
        };
        const color_ref = vk.AttachmentReference{
            .attachment = 0,
            .layout = .color_attachment_optimal,
        };
        const subpass = vk.SubpassDescription{
            .pipelineBindPoint = .graphics,
            .colorAttachmentCount = 1,
            .pColorAttachments = @ptrCast(&color_ref),
        };
        // Synchronize consecutive render passes targeting an unchanged COLOR_ATTACHMENT_OPTIMAL
        // attachment (loadOp=LOAD, blending, or subsequent overwrite).
        const dependency = vk.SubpassDependency{
            .srcSubpass = vk.SUBPASS_EXTERNAL,
            .dstSubpass = 0,
            .srcStageMask = vk.PipelineStage.color_attachment_output_bit,
            .dstStageMask = vk.PipelineStage.color_attachment_output_bit,
            .srcAccessMask = vk.Access.color_attachment_write_bit,
            .dstAccessMask = vk.Access.color_attachment_read_bit | vk.Access.color_attachment_write_bit,
            .dependencyFlags = 0,
        };
        const rp_ci = vk.RenderPassCreateInfo{
            .attachmentCount = 1,
            .pAttachments = @ptrCast(&attachment),
            .subpassCount = 1,
            .pSubpasses = @ptrCast(&subpass),
            .dependencyCount = 1,
            .pDependencies = @ptrCast(&dependency),
        };

        var rp: vk.RenderPass = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateRenderPass(dev.handle, &rp_ci, null, &rp),
            diag,
            "vkCreateRenderPass",
        );
        errdefer dev.disp.vkDestroyRenderPass(dev.handle, rp, null);

        self.map.put(gpa, key, rp) catch return error.OutOfMemory;
        return rp;
    }

    pub fn deinit(self: *RenderPassCache, gpa: Allocator, dev: *const device_mod.Device) void {
        var it = self.map.valueIterator();
        while (it.next()) |rp| {
            if (rp.* != .null_handle) {
                dev.disp.vkDestroyRenderPass(dev.handle, rp.*, null);
            }
        }
        self.map.deinit(gpa);
    }
};

pub fn createFramebuffer(
    dev: *const device_mod.Device,
    render_pass: vk.RenderPass,
    view: vk.ImageView,
    width: u32,
    height: u32,
    diag: *Diag,
) Error!vk.Framebuffer {
    const attachments = [_]vk.ImageView{view};
    const fb_ci = vk.FramebufferCreateInfo{
        .renderPass = render_pass,
        .attachmentCount = 1,
        .pAttachments = &attachments,
        .width = width,
        .height = height,
        .layers = 1,
    };
    var fb: vk.Framebuffer = .null_handle;
    try loader_mod.check(
        dev.disp.vkCreateFramebuffer(dev.handle, &fb_ci, null, &fb),
        diag,
        "vkCreateFramebuffer",
    );
    return fb;
}
