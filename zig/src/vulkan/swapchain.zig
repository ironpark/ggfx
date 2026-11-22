const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const instance_mod = @import("instance.zig");
const device_mod = @import("device.zig");
const renderpass_mod = @import("renderpass.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

pub const SwapchainImage = struct {
    image: vk.Image,
    view: vk.ImageView,
    framebuffer: vk.Framebuffer,
    layout: vk.ImageLayout = .undefined,
};

pub const Surface = struct {
    vk_surface: vk.SurfaceKHR,
    transparent: bool,
    window_state: std.atomic.Value(u32) = std.atomic.Value(u32).init(0),

    swapchain: vk.SwapchainKHR = .null_handle,
    format: vk.Format = .undefined,
    extent: vk.Extent2D = .{ .width = 0, .height = 0 },
    images: std.ArrayList(SwapchainImage) = .empty,

    // Acquire semaphores indexed by frame slot (up to 3); render semaphores indexed by swapchain image index.
    acquire_sems: [3]vk.Semaphore = @splat(.null_handle),
    render_sems: std.ArrayList(vk.Semaphore) = .empty,

    acquired_index: ?u32 = null,
    acquired_frame_slot: u32 = 0,
    acquire_wait_pending: bool = false,
    requested_width: u32 = 0,
    requested_height: u32 = 0,
    suboptimal: bool = false,
    needs_recreate: bool = false,
    cleared: bool = false,
    drawn: bool = false,

    pub fn init(
        gpa: Allocator,
        inst: *const instance_mod.Instance,
        dev: *const device_mod.Device,
        desc: *const abi.SurfaceDesc,
        out_caps: *abi.SurfaceCaps,
        diag: *Diag,
    ) Error!*Surface {
        if (!dev.has_swapchain or !inst.has_surface) {
            diag.set("vulkan: surface/swapchain extensions are not available");
            return error.Unsupported;
        }
        const destroy_surface = inst.disp.vkDestroySurfaceKHR orelse {
            diag.set("vulkan: vkDestroySurfaceKHR unavailable");
            return error.Unsupported;
        };
        const get_support = inst.disp.vkGetPhysicalDeviceSurfaceSupportKHR orelse {
            diag.set("vulkan: vkGetPhysicalDeviceSurfaceSupportKHR unavailable");
            return error.Unsupported;
        };

        var vk_surface: vk.SurfaceKHR = .null_handle;
        switch (@as(abi.SurfaceKind, @enumFromInt(desc.kind))) {
            .x11 => {
                const create_xlib = inst.disp.vkCreateXlibSurfaceKHR orelse {
                    diag.set("vulkan: VK_KHR_xlib_surface is not available");
                    return error.Unsupported;
                };
                if (!inst.has_xlib_surface) {
                    diag.set("vulkan: VK_KHR_xlib_surface is not enabled");
                    return error.Unsupported;
                }
                if (desc.handle1 == 0 or desc.handle2 == 0) {
                    diag.set("vulkan: X11 surface requires non-zero Display* and Window");
                    return error.InvalidArgument;
                }
                const ci = vk.XlibSurfaceCreateInfoKHR{
                    .dpy = @ptrFromInt(desc.handle1),
                    .window = @intCast(desc.handle2),
                };
                try loader_mod.check(
                    create_xlib(inst.handle, &ci, null, &vk_surface),
                    diag,
                    "vkCreateXlibSurfaceKHR",
                );
            },
            .wayland => {
                const create_wl = inst.disp.vkCreateWaylandSurfaceKHR orelse {
                    diag.set("vulkan: VK_KHR_wayland_surface is not available");
                    return error.Unsupported;
                };
                if (!inst.has_wayland_surface) {
                    diag.set("vulkan: VK_KHR_wayland_surface is not enabled");
                    return error.Unsupported;
                }
                if (desc.handle1 == 0 or desc.handle2 == 0) {
                    diag.set("vulkan: Wayland surface requires non-zero wl_display* and wl_surface*");
                    return error.InvalidArgument;
                }
                const ci = vk.WaylandSurfaceCreateInfoKHR{
                    .display = @ptrFromInt(desc.handle1),
                    .surface = @ptrFromInt(desc.handle2),
                };
                try loader_mod.check(
                    create_wl(inst.handle, &ci, null, &vk_surface),
                    diag,
                    "vkCreateWaylandSurfaceKHR",
                );
            },
            else => {
                diag.print("vulkan: unsupported surface kind {d}", .{desc.kind});
                return error.Unsupported;
            },
        }
        errdefer destroy_surface(inst.handle, vk_surface, null);

        var supported: vk.Bool32 = vk.FALSE;
        try loader_mod.check(
            get_support(dev.phys, dev.graphics_queue_family, vk_surface, &supported),
            diag,
            "vkGetPhysicalDeviceSurfaceSupportKHR",
        );
        if (supported == vk.FALSE) {
            diag.set("vulkan: graphics queue family does not support presentation to this surface");
            return error.Unsupported;
        }

        const transparent = (desc.flags & abi.surface_transparent) != 0;
        const chosen_fmt = try pickSurfaceFormat(gpa, inst, dev, vk_surface, diag);

        // Verify transparent composite alpha support upfront when requested.
        if (transparent) {
            if (inst.disp.vkGetPhysicalDeviceSurfaceCapabilitiesKHR) |get_caps| {
                var scaps: vk.SurfaceCapabilitiesKHR = undefined;
                try loader_mod.check(
                    get_caps(dev.phys, vk_surface, &scaps),
                    diag,
                    "vkGetPhysicalDeviceSurfaceCapabilitiesKHR",
                );
                if ((scaps.supportedCompositeAlpha & vk.CompositeAlphaKHR.pre_multiplied_bit_khr) == 0) {
                    diag.set("vulkan: surface does not support pre-multiplied transparent composite alpha");
                    return error.Unsupported;
                }
            }
        }

        const surf = gpa.create(Surface) catch return error.OutOfMemory;
        errdefer gpa.destroy(surf);
        surf.* = .{
            .vk_surface = vk_surface,
            .transparent = transparent,
            .format = chosen_fmt,
        };

        // Pre-create per-frame acquire semaphores.
        var created_sems: usize = 0;
        errdefer {
            for (surf.acquire_sems[0..created_sems]) |sem| {
                if (sem != .null_handle) dev.disp.vkDestroySemaphore(dev.handle, sem, null);
            }
        }
        const sem_ci = vk.SemaphoreCreateInfo{};
        for (&surf.acquire_sems) |*sem| {
            try loader_mod.check(
                dev.disp.vkCreateSemaphore(dev.handle, &sem_ci, null, sem),
                diag,
                "vkCreateSemaphore(acquire)",
            );
            created_sems += 1;
        }

        out_caps.* = .{
            .struct_size = @sizeOf(abi.SurfaceCaps),
            .flags = 0, // No FLIPS_Y, no Readable, no Kept, no Damage.
            .native_format = if (chosen_fmt == .b8g8r8a8_unorm)
                @intFromEnum(abi.NativeFormat.bgra8)
            else
                @intFromEnum(abi.NativeFormat.rgba8),
            .reserved = 0,
        };

        return surf;
    }

    pub fn isHidden(self: *const Surface) bool {
        return (self.window_state.load(.acquire) & abi.window_hidden) != 0;
    }

    pub fn ensureSwapchain(
        self: *Surface,
        gpa: Allocator,
        inst: *const instance_mod.Instance,
        dev: *const device_mod.Device,
        rp_cache: *renderpass_mod.RenderPassCache,
        width: u32,
        height: u32,
        diag: *Diag,
    ) Error!bool {
        self.requested_width = width;
        self.requested_height = height;

        if (width == 0 or height == 0) return false;
        if (self.swapchain != .null_handle and
            !self.needs_recreate and
            !self.suboptimal and
            self.extent.width == width and
            self.extent.height == height)
        {
            return true;
        }

        const get_caps = inst.disp.vkGetPhysicalDeviceSurfaceCapabilitiesKHR orelse return error.Unsupported;
        const create_sc = dev.disp.vkCreateSwapchainKHR orelse return error.Unsupported;
        const destroy_sc = dev.disp.vkDestroySwapchainKHR orelse return error.Unsupported;
        const get_sc_images = dev.disp.vkGetSwapchainImagesKHR orelse return error.Unsupported;

        if (self.swapchain != .null_handle) {
            dev.waitIdle();
        }

        var scaps: vk.SurfaceCapabilitiesKHR = undefined;
        try loader_mod.check(
            get_caps(dev.phys, self.vk_surface, &scaps),
            diag,
            "vkGetPhysicalDeviceSurfaceCapabilitiesKHR",
        );

        // Wayland reports currentExtent = 0xFFFFFFFF; clamp requested dimensions to min..max.
        const extent: vk.Extent2D = if (scaps.currentExtent.width != 0xFFFF_FFFF and scaps.currentExtent.height != 0xFFFF_FFFF)
            scaps.currentExtent
        else .{
            .width = std.math.clamp(width, scaps.minImageExtent.width, scaps.maxImageExtent.width),
            .height = std.math.clamp(height, scaps.minImageExtent.height, scaps.maxImageExtent.height),
        };
        if (extent.width == 0 or extent.height == 0) {
            return false;
        }

        const fmt = try pickSurfaceFormat(gpa, inst, dev, self.vk_surface, diag);

        var min_img_count = @max(scaps.minImageCount, 2);
        if (scaps.maxImageCount > 0) {
            min_img_count = @min(min_img_count, scaps.maxImageCount);
        }

        const composite_alpha: vk.Flags = if (self.transparent) blk: {
            if ((scaps.supportedCompositeAlpha & vk.CompositeAlphaKHR.pre_multiplied_bit_khr) == 0) {
                diag.set("vulkan: PRE_MULTIPLIED composite alpha unsupported for transparent surface");
                return error.Unsupported;
            }
            break :blk vk.CompositeAlphaKHR.pre_multiplied_bit_khr;
        } else if ((scaps.supportedCompositeAlpha & vk.CompositeAlphaKHR.opaque_bit_khr) != 0)
            vk.CompositeAlphaKHR.opaque_bit_khr
        else if ((scaps.supportedCompositeAlpha & vk.CompositeAlphaKHR.inherit_bit_khr) != 0)
            vk.CompositeAlphaKHR.inherit_bit_khr
        else
            vk.CompositeAlphaKHR.opaque_bit_khr;

        const pre_transform: vk.Flags = if ((scaps.supportedTransforms & vk.SurfaceTransformKHR.identity_bit_khr) != 0)
            vk.SurfaceTransformKHR.identity_bit_khr
        else
            scaps.currentTransform;

        const old_sc = self.swapchain;
        const sc_ci = vk.SwapchainCreateInfoKHR{
            .surface = self.vk_surface,
            .minImageCount = min_img_count,
            .imageFormat = fmt,
            .imageColorSpace = .srgb_nonlinear_khr,
            .imageExtent = extent,
            .imageArrayLayers = 1,
            .imageUsage = vk.ImageUsage.color_attachment_bit,
            .imageSharingMode = .exclusive,
            .preTransform = pre_transform,
            .compositeAlpha = composite_alpha,
            .presentMode = .fifo_khr,
            .clipped = vk.TRUE,
            .oldSwapchain = old_sc,
        };

        var new_sc: vk.SwapchainKHR = .null_handle;
        try loader_mod.check(
            create_sc(dev.handle, &sc_ci, null, &new_sc),
            diag,
            "vkCreateSwapchainKHR",
        );

        // Tear down old image views, framebuffers, render semaphores, and old swapchain.
        self.destroySwapchainViewsAndSems(dev);
        if (old_sc != .null_handle) {
            destroy_sc(dev.handle, old_sc, null);
        }
        self.swapchain = new_sc;
        self.format = fmt;
        self.extent = extent;
        self.acquired_index = null;
        self.acquire_wait_pending = false;
        self.suboptimal = false;
        self.needs_recreate = false;
        self.cleared = false;

        var img_count: u32 = 0;
        try loader_mod.check(
            get_sc_images(dev.handle, new_sc, &img_count, null),
            diag,
            "vkGetSwapchainImagesKHR(count)",
        );
        const raw_imgs = gpa.alloc(vk.Image, img_count) catch return error.OutOfMemory;
        defer gpa.free(raw_imgs);
        if (img_count > 0) {
            try loader_mod.check(
                get_sc_images(dev.handle, new_sc, &img_count, raw_imgs.ptr),
                diag,
                "vkGetSwapchainImagesKHR",
            );
        }

        const compat_rp = try rp_cache.getOrCreate(gpa, dev, .{
            .format = fmt,
            .load_op = .load,
        }, diag);

        const sem_ci = vk.SemaphoreCreateInfo{};
        for (raw_imgs[0..img_count]) |vk_img| {
            const view_ci = vk.ImageViewCreateInfo{
                .image = vk_img,
                .viewType = .@"2d",
                .format = fmt,
                .components = .{}, // Identity swizzle required for framebuffer attachments.
                .subresourceRange = .{
                    .aspectMask = vk.ImageAspect.color_bit,
                    .baseMipLevel = 0,
                    .levelCount = 1,
                    .baseArrayLayer = 0,
                    .layerCount = 1,
                },
            };
            var view: vk.ImageView = .null_handle;
            try loader_mod.check(
                dev.disp.vkCreateImageView(dev.handle, &view_ci, null, &view),
                diag,
                "vkCreateImageView(swapchain)",
            );
            errdefer dev.disp.vkDestroyImageView(dev.handle, view, null);

            const fb = try renderpass_mod.createFramebuffer(dev, compat_rp, view, extent.width, extent.height, diag);
            errdefer dev.disp.vkDestroyFramebuffer(dev.handle, fb, null);

            var rsem: vk.Semaphore = .null_handle;
            try loader_mod.check(
                dev.disp.vkCreateSemaphore(dev.handle, &sem_ci, null, &rsem),
                diag,
                "vkCreateSemaphore(render_finished)",
            );
            errdefer dev.disp.vkDestroySemaphore(dev.handle, rsem, null);

            self.images.append(gpa, .{
                .image = vk_img,
                .view = view,
                .framebuffer = fb,
                .layout = .undefined,
            }) catch return error.OutOfMemory;
            self.render_sems.append(gpa, rsem) catch return error.OutOfMemory;
        }

        return true;
    }

    /// Lazily acquires the next swapchain image for drawing. Returns false when hidden or zero-extent.
    pub fn acquireNextImage(
        self: *Surface,
        gpa: Allocator,
        inst: *const instance_mod.Instance,
        dev: *const device_mod.Device,
        rp_cache: *renderpass_mod.RenderPassCache,
        frame_slot: u32,
        diag: *Diag,
    ) Error!bool {
        if (self.isHidden()) return false;
        if (self.acquired_index != null) return true;

        const w = if (self.requested_width > 0) self.requested_width else self.extent.width;
        const h = if (self.requested_height > 0) self.requested_height else self.extent.height;
        if (self.swapchain == .null_handle or self.needs_recreate or self.suboptimal) {
            const ok = try self.ensureSwapchain(gpa, inst, dev, rp_cache, w, h, diag);
            if (!ok) return false;
        }

        const acquire_fn = dev.disp.vkAcquireNextImageKHR orelse return error.Unsupported;
        const sem = self.acquire_sems[frame_slot];

        var attempt: u32 = 0;
        while (attempt < 2) : (attempt += 1) {
            var img_idx: u32 = 0;
            const res = acquire_fn(dev.handle, self.swapchain, std.math.maxInt(u64), sem, .null_handle, &img_idx);
            switch (res) {
                .success => {
                    self.acquired_index = img_idx;
                    self.acquired_frame_slot = frame_slot;
                    self.acquire_wait_pending = true;
                    return true;
                },
                .suboptimal_khr => {
                    self.acquired_index = img_idx;
                    self.acquired_frame_slot = frame_slot;
                    self.acquire_wait_pending = true;
                    self.suboptimal = true;
                    return true;
                },
                .error_out_of_date_khr => {
                    if (attempt == 0) {
                        self.needs_recreate = true;
                        const ok = try self.ensureSwapchain(gpa, inst, dev, rp_cache, w, h, diag);
                        if (!ok) return false;
                        continue;
                    }
                    diag.set("vulkan: swapchain out of date after recreate retry");
                    return error.SurfaceLost;
                },
                else => {
                    try loader_mod.check(res, diag, "vkAcquireNextImageKHR");
                    unreachable;
                },
            }
        }
        return error.SurfaceLost;
    }

    pub fn currentSwapchainImage(self: *Surface) ?*SwapchainImage {
        const idx = self.acquired_index orelse return null;
        if (idx >= self.images.items.len) return null;
        return &self.images.items[idx];
    }

    pub fn currentAcquireSemaphore(self: *const Surface) vk.Semaphore {
        return self.acquire_sems[self.acquired_frame_slot];
    }

    pub fn currentRenderSemaphore(self: *const Surface) vk.Semaphore {
        const idx = self.acquired_index orelse return .null_handle;
        if (idx >= self.render_sems.items.len) return .null_handle;
        return self.render_sems.items[idx];
    }

    /// Discards an acquired-but-unpresented swapchain image cleanly:
    /// if the acquire semaphore was never consumed by a queue submit, waits for device idle
    /// and recreates that semaphore in an unsignaled state; always marks the swapchain for
    /// recreation so acquired image slots are never exhausted.
    pub fn discardAcquired(self: *Surface, dev: *const device_mod.Device, diag: *Diag) Error!void {
        if (self.acquire_wait_pending) {
            dev.waitIdle();
            const slot = self.acquired_frame_slot;
            if (self.acquire_sems[slot] != .null_handle) {
                dev.disp.vkDestroySemaphore(dev.handle, self.acquire_sems[slot], null);
                self.acquire_sems[slot] = .null_handle;
            }
            const sem_ci = vk.SemaphoreCreateInfo{};
            try loader_mod.check(
                dev.disp.vkCreateSemaphore(dev.handle, &sem_ci, null, &self.acquire_sems[slot]),
                diag,
                "vkCreateSemaphore(acquire_recreate)",
            );
            self.acquire_wait_pending = false;
        }
        self.acquired_index = null;
        self.drawn = false;
        self.cleared = false;
        self.needs_recreate = true;
    }

    fn destroySwapchainViewsAndSems(self: *Surface, dev: *const device_mod.Device) void {
        for (self.images.items) |img| {
            if (img.framebuffer != .null_handle) dev.disp.vkDestroyFramebuffer(dev.handle, img.framebuffer, null);
            if (img.view != .null_handle) dev.disp.vkDestroyImageView(dev.handle, img.view, null);
        }
        self.images.clearRetainingCapacity();

        for (self.render_sems.items) |sem| {
            if (sem != .null_handle) dev.disp.vkDestroySemaphore(dev.handle, sem, null);
        }
        self.render_sems.clearRetainingCapacity();
    }

    pub fn deinit(self: *Surface, gpa: Allocator, inst: *const instance_mod.Instance, dev: *const device_mod.Device) void {
        self.destroySwapchainViewsAndSems(dev);
        self.images.deinit(gpa);
        self.render_sems.deinit(gpa);

        if (self.swapchain != .null_handle) {
            if (dev.disp.vkDestroySwapchainKHR) |destroy_sc| {
                destroy_sc(dev.handle, self.swapchain, null);
            }
            self.swapchain = .null_handle;
        }
        for (&self.acquire_sems) |*sem| {
            if (sem.* != .null_handle) {
                dev.disp.vkDestroySemaphore(dev.handle, sem.*, null);
                sem.* = .null_handle;
            }
        }
        if (self.vk_surface != .null_handle) {
            if (inst.disp.vkDestroySurfaceKHR) |destroy_surf| {
                destroy_surf(inst.handle, self.vk_surface, null);
            }
            self.vk_surface = .null_handle;
        }
        gpa.destroy(self);
    }
};

fn pickSurfaceFormat(
    gpa: Allocator,
    inst: *const instance_mod.Instance,
    dev: *const device_mod.Device,
    vk_surface: vk.SurfaceKHR,
    diag: *Diag,
) Error!vk.Format {
    const get_fmts = inst.disp.vkGetPhysicalDeviceSurfaceFormatsKHR orelse return error.Unsupported;
    var count: u32 = 0;
    try loader_mod.check(
        get_fmts(dev.phys, vk_surface, &count, null),
        diag,
        "vkGetPhysicalDeviceSurfaceFormatsKHR(count)",
    );
    if (count == 0) {
        diag.set("vulkan: surface reports 0 supported formats");
        return error.Unsupported;
    }
    const fmts = gpa.alloc(vk.SurfaceFormatKHR, count) catch return error.OutOfMemory;
    defer gpa.free(fmts);
    try loader_mod.check(
        get_fmts(dev.phys, vk_surface, &count, fmts.ptr),
        diag,
        "vkGetPhysicalDeviceSurfaceFormatsKHR",
    );

    // Prefer B8G8R8A8_UNORM + SRGB_NONLINEAR, fallback R8G8B8A8_UNORM + SRGB_NONLINEAR.
    var has_rgba = false;
    for (fmts[0..count]) |sf| {
        if (sf.format == .undefined) {
            return .b8g8r8a8_unorm;
        }
        if (sf.colorSpace == .srgb_nonlinear_khr) {
            if (sf.format == .b8g8r8a8_unorm) return .b8g8r8a8_unorm;
            if (sf.format == .r8g8b8a8_unorm) has_rgba = true;
        }
    }
    if (has_rgba) return .r8g8b8a8_unorm;

    diag.set("vulkan: surface supports neither B8G8R8A8_UNORM nor R8G8B8A8_UNORM with SRGB_NONLINEAR");
    return error.Unsupported;
}
