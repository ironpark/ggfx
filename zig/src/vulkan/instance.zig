const std = @import("std");
const errors = @import("../errors.zig");
const sync = @import("../sync.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

const ext_surface = "VK_KHR_surface";
const ext_xlib_surface = "VK_KHR_xlib_surface";
const ext_wayland_surface = "VK_KHR_wayland_surface";
const ext_debug_utils = "VK_EXT_debug_utils";
const layer_validation = "VK_LAYER_KHRONOS_validation";

/// Ring buffer collecting validation ERROR messages from VK_EXT_debug_utils.
pub const DebugRing = struct {
    pub const capacity = 16;
    pub const entry_bytes = 256;

    mu: sync.Mutex = .{},
    entries: [capacity][entry_bytes]u8 = @splat(@splat(0)),
    lens: [capacity]u16 = @splat(0),
    head: usize = 0,
    error_count: std.atomic.Value(u64) = std.atomic.Value(u64).init(0),

    pub fn push(self: *DebugRing, msg: []const u8) void {
        self.mu.lock();
        defer self.mu.unlock();
        const slot = self.head % capacity;
        const n = @min(msg.len, entry_bytes - 1);
        if (n > 0) @memcpy(self.entries[slot][0..n], msg[0..n]);
        self.entries[slot][n] = 0;
        self.lens[slot] = @intCast(n);
        self.head +%= 1;
        _ = self.error_count.fetchAdd(1, .release);
    }

    pub fn deinit(self: *DebugRing) void {
        self.mu.deinit();
    }
};

fn debugUtilsCallback(
    message_severity: vk.Flags,
    _: vk.Flags,
    callback_data: *const vk.DebugUtilsMessengerCallbackDataEXT,
    user_data: ?*anyopaque,
) callconv(.c) vk.Bool32 {
    if ((message_severity & vk.DebugUtilsMessageSeverityEXT.error_bit_ext) != 0) {
        if (user_data) |ptr| {
            const ring: *DebugRing = @ptrCast(@alignCast(ptr));
            const msg: []const u8 = if (callback_data.pMessage) |m| std.mem.span(m) else "vulkan validation error";
            ring.push(msg);
        }
    }
    return vk.FALSE;
}

fn hasExtension(exts: []const vk.ExtensionProperties, name: []const u8) bool {
    for (exts) |*e| {
        if (std.mem.eql(u8, std.mem.sliceTo(&e.extensionName, 0), name)) return true;
    }
    return false;
}

fn hasLayer(layers: []const vk.LayerProperties, name: []const u8) bool {
    for (layers) |*l| {
        if (std.mem.eql(u8, std.mem.sliceTo(&l.layerName, 0), name)) return true;
    }
    return false;
}

pub const Instance = struct {
    handle: vk.Instance,
    disp: loader_mod.InstanceDispatch,
    api_version: u32,
    has_surface: bool,
    has_xlib_surface: bool,
    has_wayland_surface: bool,
    has_debug_utils: bool,
    debug_ring: ?*DebugRing = null,
    debug_messenger: vk.DebugUtilsMessengerEXT = .null_handle,

    pub fn init(gpa: Allocator, loader: *const loader_mod.Loader, debug_enabled: bool, diag: *Diag) Error!Instance {
        // Choose Vulkan 1.1 when supported by loader, otherwise 1.0.
        var api_version: u32 = vk.API_VERSION_1_0;
        if (loader.global.vkEnumerateInstanceVersion) |enum_ver| {
            var supported: u32 = vk.API_VERSION_1_0;
            if (enum_ver(&supported) == .success and supported >= vk.API_VERSION_1_1) {
                api_version = vk.API_VERSION_1_1;
            }
        }

        // Enumerate available instance extensions.
        var ext_count: u32 = 0;
        try loader_mod.check(
            loader.global.vkEnumerateInstanceExtensionProperties(null, &ext_count, null),
            diag,
            "vkEnumerateInstanceExtensionProperties(count)",
        );
        const avail_exts = gpa.alloc(vk.ExtensionProperties, ext_count) catch return error.OutOfMemory;
        defer gpa.free(avail_exts);
        if (ext_count > 0) {
            try loader_mod.check(
                loader.global.vkEnumerateInstanceExtensionProperties(null, &ext_count, avail_exts.ptr),
                diag,
                "vkEnumerateInstanceExtensionProperties",
            );
        }

        var enabled_exts_buf: [4][*:0]const u8 = undefined;
        var enabled_ext_count: u32 = 0;

        // Enable available surface extensions only — headless works with 0 surface extensions.
        const has_surface = hasExtension(avail_exts[0..ext_count], ext_surface);
        var has_xlib = false;
        var has_wayland = false;
        var has_debug_utils = false;

        if (has_surface) {
            enabled_exts_buf[enabled_ext_count] = ext_surface;
            enabled_ext_count += 1;

            if (hasExtension(avail_exts[0..ext_count], ext_xlib_surface)) {
                has_xlib = true;
                enabled_exts_buf[enabled_ext_count] = ext_xlib_surface;
                enabled_ext_count += 1;
            }
            if (hasExtension(avail_exts[0..ext_count], ext_wayland_surface)) {
                has_wayland = true;
                enabled_exts_buf[enabled_ext_count] = ext_wayland_surface;
                enabled_ext_count += 1;
            }
        }

        var enabled_layers_buf: [1][*:0]const u8 = undefined;
        var enabled_layer_count: u32 = 0;

        if (debug_enabled) {
            if (hasExtension(avail_exts[0..ext_count], ext_debug_utils)) {
                has_debug_utils = true;
                enabled_exts_buf[enabled_ext_count] = ext_debug_utils;
                enabled_ext_count += 1;
            }

            var layer_count: u32 = 0;
            if (loader.global.vkEnumerateInstanceLayerProperties(&layer_count, null) == .success and layer_count > 0) {
                const avail_layers = gpa.alloc(vk.LayerProperties, layer_count) catch return error.OutOfMemory;
                defer gpa.free(avail_layers);
                if (loader.global.vkEnumerateInstanceLayerProperties(&layer_count, avail_layers.ptr) == .success) {
                    if (hasLayer(avail_layers[0..layer_count], layer_validation)) {
                        enabled_layers_buf[0] = layer_validation;
                        enabled_layer_count = 1;
                    }
                }
            }
        }

        var debug_ring: ?*DebugRing = null;
        errdefer if (debug_ring) |r| {
            r.deinit();
            gpa.destroy(r);
        };

        var debug_ci: vk.DebugUtilsMessengerCreateInfoEXT = undefined;
        var p_next: ?*const anyopaque = null;
        if (has_debug_utils) {
            const ring = gpa.create(DebugRing) catch return error.OutOfMemory;
            ring.* = .{};
            debug_ring = ring;
            debug_ci = .{
                .messageSeverity = vk.DebugUtilsMessageSeverityEXT.error_bit_ext |
                    vk.DebugUtilsMessageSeverityEXT.warning_bit_ext,
                .messageType = vk.DebugUtilsMessageTypeEXT.general_bit_ext |
                    vk.DebugUtilsMessageTypeEXT.validation_bit_ext |
                    vk.DebugUtilsMessageTypeEXT.performance_bit_ext,
                .pfnUserCallback = debugUtilsCallback,
                .pUserData = ring,
            };
            p_next = &debug_ci;
        }

        const app_info = vk.ApplicationInfo{
            .pApplicationName = "ggfx",
            .applicationVersion = 1,
            .pEngineName = "ggfx",
            .engineVersion = 1,
            .apiVersion = api_version,
        };

        const create_info = vk.InstanceCreateInfo{
            .pNext = p_next,
            .pApplicationInfo = &app_info,
            .enabledLayerCount = enabled_layer_count,
            .ppEnabledLayerNames = if (enabled_layer_count > 0) &enabled_layers_buf else null,
            .enabledExtensionCount = enabled_ext_count,
            .ppEnabledExtensionNames = if (enabled_ext_count > 0) &enabled_exts_buf else null,
        };

        var handle: vk.Instance = null;
        try loader_mod.check(
            loader.global.vkCreateInstance(&create_info, null, &handle),
            diag,
            "vkCreateInstance",
        );

        const disp = loader.loadInstanceDispatch(handle, diag) catch |err| {
            // If vkDestroyInstance itself wasn't loaded we can't destroy, otherwise clean up.
            if (loader.global.vkGetInstanceProcAddr(handle, "vkDestroyInstance")) |raw| {
                const destroy_fn: vk.PFN_vkDestroyInstance = @ptrCast(raw);
                destroy_fn(handle, null);
            }
            return err;
        };
        errdefer disp.vkDestroyInstance(handle, null);

        var messenger: vk.DebugUtilsMessengerEXT = .null_handle;
        if (has_debug_utils) {
            if (disp.vkCreateDebugUtilsMessengerEXT) |create_dbg| {
                _ = create_dbg(handle, &debug_ci, null, &messenger);
            }
        }

        return .{
            .handle = handle,
            .disp = disp,
            .api_version = api_version,
            .has_surface = has_surface,
            .has_xlib_surface = has_xlib,
            .has_wayland_surface = has_wayland,
            .has_debug_utils = has_debug_utils,
            .debug_ring = debug_ring,
            .debug_messenger = messenger,
        };
    }

    pub fn deinit(self: *Instance, gpa: Allocator) void {
        if (self.debug_messenger != .null_handle) {
            if (self.disp.vkDestroyDebugUtilsMessengerEXT) |destroy_dbg| {
                destroy_dbg(self.handle, self.debug_messenger, null);
            }
            self.debug_messenger = .null_handle;
        }
        if (self.handle != null) {
            self.disp.vkDestroyInstance(self.handle, null);
            self.handle = null;
        }
        if (self.debug_ring) |ring| {
            ring.deinit();
            gpa.destroy(ring);
            self.debug_ring = null;
        }
    }
};
