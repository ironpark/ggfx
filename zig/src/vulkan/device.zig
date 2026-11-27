const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const instance_mod = @import("instance.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

const ext_swapchain = "VK_KHR_swapchain";

const required_a8_features: vk.Flags =
    vk.FormatFeature.sampled_image_bit |
    vk.FormatFeature.color_attachment_bit |
    vk.FormatFeature.color_attachment_blend_bit |
    vk.FormatFeature.transfer_src_bit |
    vk.FormatFeature.transfer_dst_bit;

fn scoreDeviceType(dev_type: vk.PhysicalDeviceType, low_power: bool) u32 {
    if (low_power) {
        return switch (dev_type) {
            .integrated_gpu => 400,
            .discrete_gpu => 300,
            .virtual_gpu => 200,
            .cpu => 100,
            else => 50,
        };
    }
    return switch (dev_type) {
        .discrete_gpu => 400,
        .integrated_gpu => 300,
        .virtual_gpu => 200,
        .cpu => 100,
        else => 50,
    };
}

fn findGraphicsQueueFamily(
    gpa: Allocator,
    inst: *const instance_mod.Instance,
    phys: vk.PhysicalDevice,
) Error!?u32 {
    var count: u32 = 0;
    inst.disp.vkGetPhysicalDeviceQueueFamilyProperties(phys, &count, null);
    if (count == 0) return null;

    const props = gpa.alloc(vk.QueueFamilyProperties, count) catch return error.OutOfMemory;
    defer gpa.free(props);
    inst.disp.vkGetPhysicalDeviceQueueFamilyProperties(phys, &count, props.ptr);

    for (props[0..count], 0..) |*qf, idx| {
        if (qf.queueCount > 0 and (qf.queueFlags & vk.QueueFlags.graphics_bit) != 0) {
            return @intCast(idx);
        }
    }
    return null;
}

fn hasDeviceExtension(
    gpa: Allocator,
    inst: *const instance_mod.Instance,
    phys: vk.PhysicalDevice,
    ext_name: []const u8,
) Error!bool {
    var count: u32 = 0;
    if (inst.disp.vkEnumerateDeviceExtensionProperties(phys, null, &count, null) != .success or count == 0) {
        return false;
    }
    const exts = gpa.alloc(vk.ExtensionProperties, count) catch return error.OutOfMemory;
    defer gpa.free(exts);
    if (inst.disp.vkEnumerateDeviceExtensionProperties(phys, null, &count, exts.ptr) != .success) {
        return false;
    }
    for (exts[0..count]) |*e| {
        if (std.mem.eql(u8, std.mem.sliceTo(&e.extensionName, 0), ext_name)) return true;
    }
    return false;
}

pub const Device = struct {
    phys: vk.PhysicalDevice,
    handle: vk.Device,
    disp: loader_mod.DeviceDispatch,
    graphics_queue_family: u32,
    graphics_queue: vk.Queue,
    props: vk.PhysicalDeviceProperties,
    mem_props: vk.PhysicalDeviceMemoryProperties,
    has_swapchain: bool,
    supports_a8: bool,
    unified_memory: bool,
    max_image_size: u32,

    pub fn init(
        gpa: Allocator,
        loader: *const loader_mod.Loader,
        inst: *const instance_mod.Instance,
        opts: *const abi.DeviceOptions,
        caps: *abi.DriverCaps,
        diag: *Diag,
    ) Error!Device {
        var dev_count: u32 = 0;
        try loader_mod.check(
            inst.disp.vkEnumeratePhysicalDevices(inst.handle, &dev_count, null),
            diag,
            "vkEnumeratePhysicalDevices(count)",
        );
        if (dev_count == 0) {
            diag.set("vulkan: no physical devices found");
            return error.BackendUnavailable;
        }

        const phys_devs = gpa.alloc(vk.PhysicalDevice, dev_count) catch return error.OutOfMemory;
        defer gpa.free(phys_devs);
        try loader_mod.check(
            inst.disp.vkEnumeratePhysicalDevices(inst.handle, &dev_count, phys_devs.ptr),
            diag,
            "vkEnumeratePhysicalDevices",
        );

        const low_power = (opts.flags & abi.device_low_power) != 0;

        var best_phys: vk.PhysicalDevice = null;
        var best_qf: u32 = 0;
        var best_score: u32 = 0;
        var best_props: vk.PhysicalDeviceProperties = undefined;

        for (phys_devs[0..dev_count]) |phys| {
            const qf = (try findGraphicsQueueFamily(gpa, inst, phys)) orelse continue;
            var props: vk.PhysicalDeviceProperties = undefined;
            inst.disp.vkGetPhysicalDeviceProperties(phys, &props);
            const score = scoreDeviceType(props.deviceType, low_power);
            if (best_phys == null or score > best_score) {
                best_phys = phys;
                best_qf = qf;
                best_score = score;
                best_props = props;
            }
        }

        if (best_phys == null) {
            diag.set("vulkan: no physical device with a graphics queue family");
            return error.BackendUnavailable;
        }

        const has_swapchain = try hasDeviceExtension(gpa, inst, best_phys, ext_swapchain);

        var enabled_exts: [1][*:0]const u8 = undefined;
        var enabled_ext_count: u32 = 0;
        if (has_swapchain) {
            enabled_exts[0] = ext_swapchain;
            enabled_ext_count = 1;
        }

        const queue_priority: f32 = 1.0;
        const queue_ci = vk.DeviceQueueCreateInfo{
            .queueFamilyIndex = best_qf,
            .queueCount = 1,
            .pQueuePriorities = @ptrCast(&queue_priority),
        };

        const dev_ci = vk.DeviceCreateInfo{
            .queueCreateInfoCount = 1,
            .pQueueCreateInfos = @ptrCast(&queue_ci),
            .enabledExtensionCount = enabled_ext_count,
            .ppEnabledExtensionNames = if (enabled_ext_count > 0) &enabled_exts else null,
            .pEnabledFeatures = null,
        };

        var dev_handle: vk.Device = null;
        try loader_mod.check(
            inst.disp.vkCreateDevice(best_phys, &dev_ci, null, &dev_handle),
            diag,
            "vkCreateDevice",
        );

        const dev_disp = loader.loadDeviceDispatch(inst.handle, &inst.disp, dev_handle, diag) catch |err| {
            if (inst.disp.vkGetDeviceProcAddr(dev_handle, "vkDestroyDevice")) |raw| {
                const destroy_fn: vk.PFN_vkDestroyDevice = @ptrCast(raw);
                destroy_fn(dev_handle, null);
            }
            return err;
        };

        var graphics_queue: vk.Queue = null;
        dev_disp.vkGetDeviceQueue(dev_handle, best_qf, 0, &graphics_queue);

        var mem_props: vk.PhysicalDeviceMemoryProperties = undefined;
        inst.disp.vkGetPhysicalDeviceMemoryProperties(best_phys, &mem_props);

        // Query A8 (R8_UNORM) capability.
        var r8_fmt_props: vk.FormatProperties = undefined;
        inst.disp.vkGetPhysicalDeviceFormatProperties(best_phys, .r8_unorm, &r8_fmt_props);
        const supports_a8 = (r8_fmt_props.optimalTilingFeatures & required_a8_features) == required_a8_features;

        // Detect unified memory (device-local + host-visible memory type).
        var unified_memory = false;
        const mt_count = @min(mem_props.memoryTypeCount, vk.MAX_MEMORY_TYPES);
        for (mem_props.memoryTypes[0..mt_count]) |mt| {
            const both = vk.MemoryProperty.device_local_bit | vk.MemoryProperty.host_visible_bit;
            if ((mt.propertyFlags & both) == both) {
                unified_memory = true;
                break;
            }
        }

        const raw_max = best_props.limits.maxImageDimension2D;
        const max_image_size: u32 = if (raw_max == 0) 4096 else @min(raw_max, 16384);
        const frames_in_flight: u32 = if (opts.frames_in_flight == 3) 3 else 2;

        // Populate DriverCaps (note: NO FLIPS_Y for Vulkan!).
        caps.* = std.mem.zeroes(abi.DriverCaps);
        caps.struct_size = @sizeOf(abi.DriverCaps);
        caps.backend = @intFromEnum(abi.Backend.vulkan);
        caps.max_image_size = max_image_size;
        caps.flags = abi.cap_needs_clearing_screen;
        if (supports_a8) caps.flags |= abi.cap_supports_a8;
        if (unified_memory) caps.flags |= abi.cap_unified_memory;
        caps.shader_format = @intFromEnum(abi.ShaderFormat.spirv);
        caps.shader_version = 10;
        caps.color_space = opts.color_space;
        caps.frames_in_flight = frames_in_flight;

        const name_slice = std.mem.sliceTo(&best_props.deviceName, 0);
        const copy_len = @min(name_slice.len, caps.device_name.len - 1);
        if (copy_len > 0) @memcpy(caps.device_name[0..copy_len], name_slice[0..copy_len]);
        caps.device_name[copy_len] = 0;

        return .{
            .phys = best_phys,
            .handle = dev_handle,
            .disp = dev_disp,
            .graphics_queue_family = best_qf,
            .graphics_queue = graphics_queue,
            .props = best_props,
            .mem_props = mem_props,
            .has_swapchain = has_swapchain and dev_disp.vkCreateSwapchainKHR != null,
            .supports_a8 = supports_a8,
            .unified_memory = unified_memory,
            .max_image_size = max_image_size,
        };
    }

    pub fn waitIdle(self: *const Device) void {
        if (self.handle != null) {
            _ = self.disp.vkDeviceWaitIdle(self.handle);
        }
    }

    pub fn waitIdleChecked(self: *const Device, diag: *Diag) Error!void {
        if (self.handle == null) return;
        try loader_mod.check(
            self.disp.vkDeviceWaitIdle(self.handle),
            diag,
            "vkDeviceWaitIdle",
        );
    }

    pub fn deinit(self: *Device) void {
        if (self.handle != null) {
            self.disp.vkDestroyDevice(self.handle, null);
            self.handle = null;
        }
    }
};
