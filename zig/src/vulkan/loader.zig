const std = @import("std");
const dynlib = @import("../dynlib.zig");
const errors = @import("../errors.zig");
const vk = @import("vk.zig");
const Diag = errors.Diag;
const Error = errors.Error;

pub const GlobalDispatch = struct {
    vkGetInstanceProcAddr: vk.PFN_vkGetInstanceProcAddr,
    vkEnumerateInstanceVersion: ?vk.PFN_vkEnumerateInstanceVersion,
    vkEnumerateInstanceExtensionProperties: vk.PFN_vkEnumerateInstanceExtensionProperties,
    vkEnumerateInstanceLayerProperties: vk.PFN_vkEnumerateInstanceLayerProperties,
    vkCreateInstance: vk.PFN_vkCreateInstance,
};

pub const InstanceDispatch = struct {
    vkDestroyInstance: vk.PFN_vkDestroyInstance,
    vkEnumeratePhysicalDevices: vk.PFN_vkEnumeratePhysicalDevices,
    vkGetPhysicalDeviceProperties: vk.PFN_vkGetPhysicalDeviceProperties,
    vkGetPhysicalDeviceMemoryProperties: vk.PFN_vkGetPhysicalDeviceMemoryProperties,
    vkGetPhysicalDeviceFormatProperties: vk.PFN_vkGetPhysicalDeviceFormatProperties,
    vkGetPhysicalDeviceQueueFamilyProperties: vk.PFN_vkGetPhysicalDeviceQueueFamilyProperties,
    vkEnumerateDeviceExtensionProperties: vk.PFN_vkEnumerateDeviceExtensionProperties,
    vkCreateDevice: vk.PFN_vkCreateDevice,
    vkGetDeviceProcAddr: vk.PFN_vkGetDeviceProcAddr,

    // Optional KHR surface functions.
    vkDestroySurfaceKHR: ?vk.PFN_vkDestroySurfaceKHR = null,
    vkGetPhysicalDeviceSurfaceSupportKHR: ?vk.PFN_vkGetPhysicalDeviceSurfaceSupportKHR = null,
    vkGetPhysicalDeviceSurfaceCapabilitiesKHR: ?vk.PFN_vkGetPhysicalDeviceSurfaceCapabilitiesKHR = null,
    vkGetPhysicalDeviceSurfaceFormatsKHR: ?vk.PFN_vkGetPhysicalDeviceSurfaceFormatsKHR = null,
    vkGetPhysicalDeviceSurfacePresentModesKHR: ?vk.PFN_vkGetPhysicalDeviceSurfacePresentModesKHR = null,
    vkCreateXlibSurfaceKHR: ?vk.PFN_vkCreateXlibSurfaceKHR = null,
    vkCreateWaylandSurfaceKHR: ?vk.PFN_vkCreateWaylandSurfaceKHR = null,

    // Optional EXT debug utils functions.
    vkCreateDebugUtilsMessengerEXT: ?vk.PFN_vkCreateDebugUtilsMessengerEXT = null,
    vkDestroyDebugUtilsMessengerEXT: ?vk.PFN_vkDestroyDebugUtilsMessengerEXT = null,
};

pub const DeviceDispatch = struct {
    vkDestroyDevice: vk.PFN_vkDestroyDevice,
    vkGetDeviceQueue: vk.PFN_vkGetDeviceQueue,
    vkDeviceWaitIdle: vk.PFN_vkDeviceWaitIdle,
    vkQueueSubmit: vk.PFN_vkQueueSubmit,
    vkQueueWaitIdle: vk.PFN_vkQueueWaitIdle,

    vkAllocateMemory: vk.PFN_vkAllocateMemory,
    vkFreeMemory: vk.PFN_vkFreeMemory,
    vkMapMemory: vk.PFN_vkMapMemory,
    vkUnmapMemory: vk.PFN_vkUnmapMemory,
    vkInvalidateMappedMemoryRanges: vk.PFN_vkInvalidateMappedMemoryRanges,
    vkFlushMappedMemoryRanges: vk.PFN_vkFlushMappedMemoryRanges,

    vkCreateBuffer: vk.PFN_vkCreateBuffer,
    vkDestroyBuffer: vk.PFN_vkDestroyBuffer,
    vkGetBufferMemoryRequirements: vk.PFN_vkGetBufferMemoryRequirements,
    vkBindBufferMemory: vk.PFN_vkBindBufferMemory,

    vkCreateImage: vk.PFN_vkCreateImage,
    vkDestroyImage: vk.PFN_vkDestroyImage,
    vkGetImageMemoryRequirements: vk.PFN_vkGetImageMemoryRequirements,
    vkBindImageMemory: vk.PFN_vkBindImageMemory,

    vkCreateImageView: vk.PFN_vkCreateImageView,
    vkDestroyImageView: vk.PFN_vkDestroyImageView,

    vkCreateShaderModule: vk.PFN_vkCreateShaderModule,
    vkDestroyShaderModule: vk.PFN_vkDestroyShaderModule,

    vkCreatePipelineLayout: vk.PFN_vkCreatePipelineLayout,
    vkDestroyPipelineLayout: vk.PFN_vkDestroyPipelineLayout,
    vkCreatePipelineCache: vk.PFN_vkCreatePipelineCache,
    vkDestroyPipelineCache: vk.PFN_vkDestroyPipelineCache,
    vkCreateGraphicsPipelines: vk.PFN_vkCreateGraphicsPipelines,
    vkDestroyPipeline: vk.PFN_vkDestroyPipeline,

    vkCreateRenderPass: vk.PFN_vkCreateRenderPass,
    vkDestroyRenderPass: vk.PFN_vkDestroyRenderPass,
    vkCreateFramebuffer: vk.PFN_vkCreateFramebuffer,
    vkDestroyFramebuffer: vk.PFN_vkDestroyFramebuffer,

    vkCreateDescriptorSetLayout: vk.PFN_vkCreateDescriptorSetLayout,
    vkDestroyDescriptorSetLayout: vk.PFN_vkDestroyDescriptorSetLayout,
    vkCreateDescriptorPool: vk.PFN_vkCreateDescriptorPool,
    vkDestroyDescriptorPool: vk.PFN_vkDestroyDescriptorPool,
    vkResetDescriptorPool: vk.PFN_vkResetDescriptorPool,
    vkAllocateDescriptorSets: vk.PFN_vkAllocateDescriptorSets,
    vkUpdateDescriptorSets: vk.PFN_vkUpdateDescriptorSets,

    vkCreateCommandPool: vk.PFN_vkCreateCommandPool,
    vkDestroyCommandPool: vk.PFN_vkDestroyCommandPool,
    vkResetCommandPool: vk.PFN_vkResetCommandPool,
    vkAllocateCommandBuffers: vk.PFN_vkAllocateCommandBuffers,
    vkFreeCommandBuffers: vk.PFN_vkFreeCommandBuffers,
    vkBeginCommandBuffer: vk.PFN_vkBeginCommandBuffer,
    vkEndCommandBuffer: vk.PFN_vkEndCommandBuffer,
    vkResetCommandBuffer: vk.PFN_vkResetCommandBuffer,

    vkCreateFence: vk.PFN_vkCreateFence,
    vkDestroyFence: vk.PFN_vkDestroyFence,
    vkResetFences: vk.PFN_vkResetFences,
    vkWaitForFences: vk.PFN_vkWaitForFences,

    vkCreateSemaphore: vk.PFN_vkCreateSemaphore,
    vkDestroySemaphore: vk.PFN_vkDestroySemaphore,

    vkCmdBeginRenderPass: vk.PFN_vkCmdBeginRenderPass,
    vkCmdEndRenderPass: vk.PFN_vkCmdEndRenderPass,
    vkCmdBindPipeline: vk.PFN_vkCmdBindPipeline,
    vkCmdSetViewport: vk.PFN_vkCmdSetViewport,
    vkCmdSetScissor: vk.PFN_vkCmdSetScissor,
    vkCmdBindDescriptorSets: vk.PFN_vkCmdBindDescriptorSets,
    vkCmdBindVertexBuffers: vk.PFN_vkCmdBindVertexBuffers,
    vkCmdBindIndexBuffer: vk.PFN_vkCmdBindIndexBuffer,
    vkCmdDrawIndexed: vk.PFN_vkCmdDrawIndexed,
    vkCmdPipelineBarrier: vk.PFN_vkCmdPipelineBarrier,
    vkCmdCopyBufferToImage: vk.PFN_vkCmdCopyBufferToImage,
    vkCmdCopyImageToBuffer: vk.PFN_vkCmdCopyImageToBuffer,
    vkCmdClearColorImage: vk.PFN_vkCmdClearColorImage,

    // Optional KHR swapchain functions.
    vkCreateSwapchainKHR: ?vk.PFN_vkCreateSwapchainKHR = null,
    vkDestroySwapchainKHR: ?vk.PFN_vkDestroySwapchainKHR = null,
    vkGetSwapchainImagesKHR: ?vk.PFN_vkGetSwapchainImagesKHR = null,
    vkAcquireNextImageKHR: ?vk.PFN_vkAcquireNextImageKHR = null,
    vkQueuePresentKHR: ?vk.PFN_vkQueuePresentKHR = null,
};

pub const Loader = struct {
    lib: dynlib.DynLib,
    global: GlobalDispatch,

    pub fn open(diag: *Diag) Error!Loader {
        var lib = dynlib.DynLib.openZ("libvulkan.so.1") catch
            dynlib.DynLib.openZ("libvulkan.so") catch {
            diag.set("vulkan: failed to load libvulkan.so.1 or libvulkan.so");
            return error.BackendUnavailable;
        };
        errdefer lib.close();

        const get_inst_proc = lib.lookup(vk.PFN_vkGetInstanceProcAddr, "vkGetInstanceProcAddr") orelse {
            diag.set("vulkan: symbol vkGetInstanceProcAddr not found");
            return error.BackendUnavailable;
        };

        const global = GlobalDispatch{
            .vkGetInstanceProcAddr = get_inst_proc,
            .vkEnumerateInstanceVersion = loadInstOpt(get_inst_proc, null, vk.PFN_vkEnumerateInstanceVersion, "vkEnumerateInstanceVersion"),
            .vkEnumerateInstanceExtensionProperties = try loadInstReq(get_inst_proc, null, vk.PFN_vkEnumerateInstanceExtensionProperties, "vkEnumerateInstanceExtensionProperties", diag),
            .vkEnumerateInstanceLayerProperties = try loadInstReq(get_inst_proc, null, vk.PFN_vkEnumerateInstanceLayerProperties, "vkEnumerateInstanceLayerProperties", diag),
            .vkCreateInstance = try loadInstReq(get_inst_proc, null, vk.PFN_vkCreateInstance, "vkCreateInstance", diag),
        };

        return .{
            .lib = lib,
            .global = global,
        };
    }

    pub fn close(self: *Loader) void {
        self.lib.close();
    }

    pub fn loadInstanceDispatch(self: *const Loader, inst: vk.Instance, diag: *Diag) Error!InstanceDispatch {
        const gipa = self.global.vkGetInstanceProcAddr;
        return .{
            .vkDestroyInstance = try loadInstReq(gipa, inst, vk.PFN_vkDestroyInstance, "vkDestroyInstance", diag),
            .vkEnumeratePhysicalDevices = try loadInstReq(gipa, inst, vk.PFN_vkEnumeratePhysicalDevices, "vkEnumeratePhysicalDevices", diag),
            .vkGetPhysicalDeviceProperties = try loadInstReq(gipa, inst, vk.PFN_vkGetPhysicalDeviceProperties, "vkGetPhysicalDeviceProperties", diag),
            .vkGetPhysicalDeviceMemoryProperties = try loadInstReq(gipa, inst, vk.PFN_vkGetPhysicalDeviceMemoryProperties, "vkGetPhysicalDeviceMemoryProperties", diag),
            .vkGetPhysicalDeviceFormatProperties = try loadInstReq(gipa, inst, vk.PFN_vkGetPhysicalDeviceFormatProperties, "vkGetPhysicalDeviceFormatProperties", diag),
            .vkGetPhysicalDeviceQueueFamilyProperties = try loadInstReq(gipa, inst, vk.PFN_vkGetPhysicalDeviceQueueFamilyProperties, "vkGetPhysicalDeviceQueueFamilyProperties", diag),
            .vkEnumerateDeviceExtensionProperties = try loadInstReq(gipa, inst, vk.PFN_vkEnumerateDeviceExtensionProperties, "vkEnumerateDeviceExtensionProperties", diag),
            .vkCreateDevice = try loadInstReq(gipa, inst, vk.PFN_vkCreateDevice, "vkCreateDevice", diag),
            .vkGetDeviceProcAddr = try loadInstReq(gipa, inst, vk.PFN_vkGetDeviceProcAddr, "vkGetDeviceProcAddr", diag),

            .vkDestroySurfaceKHR = loadInstOpt(gipa, inst, vk.PFN_vkDestroySurfaceKHR, "vkDestroySurfaceKHR"),
            .vkGetPhysicalDeviceSurfaceSupportKHR = loadInstOpt(gipa, inst, vk.PFN_vkGetPhysicalDeviceSurfaceSupportKHR, "vkGetPhysicalDeviceSurfaceSupportKHR"),
            .vkGetPhysicalDeviceSurfaceCapabilitiesKHR = loadInstOpt(gipa, inst, vk.PFN_vkGetPhysicalDeviceSurfaceCapabilitiesKHR, "vkGetPhysicalDeviceSurfaceCapabilitiesKHR"),
            .vkGetPhysicalDeviceSurfaceFormatsKHR = loadInstOpt(gipa, inst, vk.PFN_vkGetPhysicalDeviceSurfaceFormatsKHR, "vkGetPhysicalDeviceSurfaceFormatsKHR"),
            .vkGetPhysicalDeviceSurfacePresentModesKHR = loadInstOpt(gipa, inst, vk.PFN_vkGetPhysicalDeviceSurfacePresentModesKHR, "vkGetPhysicalDeviceSurfacePresentModesKHR"),
            .vkCreateXlibSurfaceKHR = loadInstOpt(gipa, inst, vk.PFN_vkCreateXlibSurfaceKHR, "vkCreateXlibSurfaceKHR"),
            .vkCreateWaylandSurfaceKHR = loadInstOpt(gipa, inst, vk.PFN_vkCreateWaylandSurfaceKHR, "vkCreateWaylandSurfaceKHR"),

            .vkCreateDebugUtilsMessengerEXT = loadInstOpt(gipa, inst, vk.PFN_vkCreateDebugUtilsMessengerEXT, "vkCreateDebugUtilsMessengerEXT"),
            .vkDestroyDebugUtilsMessengerEXT = loadInstOpt(gipa, inst, vk.PFN_vkDestroyDebugUtilsMessengerEXT, "vkDestroyDebugUtilsMessengerEXT"),
        };
    }

    pub fn loadDeviceDispatch(
        self: *const Loader,
        inst: vk.Instance,
        inst_disp: *const InstanceDispatch,
        dev: vk.Device,
        diag: *Diag,
    ) Error!DeviceDispatch {
        const gdpa = inst_disp.vkGetDeviceProcAddr;
        const gipa = self.global.vkGetInstanceProcAddr;
        return .{
            .vkDestroyDevice = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyDevice, "vkDestroyDevice", diag),
            .vkGetDeviceQueue = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkGetDeviceQueue, "vkGetDeviceQueue", diag),
            .vkDeviceWaitIdle = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDeviceWaitIdle, "vkDeviceWaitIdle", diag),
            .vkQueueSubmit = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkQueueSubmit, "vkQueueSubmit", diag),
            .vkQueueWaitIdle = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkQueueWaitIdle, "vkQueueWaitIdle", diag),

            .vkAllocateMemory = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkAllocateMemory, "vkAllocateMemory", diag),
            .vkFreeMemory = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkFreeMemory, "vkFreeMemory", diag),
            .vkMapMemory = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkMapMemory, "vkMapMemory", diag),
            .vkUnmapMemory = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkUnmapMemory, "vkUnmapMemory", diag),
            .vkInvalidateMappedMemoryRanges = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkInvalidateMappedMemoryRanges, "vkInvalidateMappedMemoryRanges", diag),
            .vkFlushMappedMemoryRanges = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkFlushMappedMemoryRanges, "vkFlushMappedMemoryRanges", diag),

            .vkCreateBuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateBuffer, "vkCreateBuffer", diag),
            .vkDestroyBuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyBuffer, "vkDestroyBuffer", diag),
            .vkGetBufferMemoryRequirements = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkGetBufferMemoryRequirements, "vkGetBufferMemoryRequirements", diag),
            .vkBindBufferMemory = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkBindBufferMemory, "vkBindBufferMemory", diag),

            .vkCreateImage = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateImage, "vkCreateImage", diag),
            .vkDestroyImage = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyImage, "vkDestroyImage", diag),
            .vkGetImageMemoryRequirements = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkGetImageMemoryRequirements, "vkGetImageMemoryRequirements", diag),
            .vkBindImageMemory = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkBindImageMemory, "vkBindImageMemory", diag),

            .vkCreateImageView = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateImageView, "vkCreateImageView", diag),
            .vkDestroyImageView = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyImageView, "vkDestroyImageView", diag),

            .vkCreateShaderModule = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateShaderModule, "vkCreateShaderModule", diag),
            .vkDestroyShaderModule = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyShaderModule, "vkDestroyShaderModule", diag),

            .vkCreatePipelineLayout = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreatePipelineLayout, "vkCreatePipelineLayout", diag),
            .vkDestroyPipelineLayout = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyPipelineLayout, "vkDestroyPipelineLayout", diag),
            .vkCreatePipelineCache = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreatePipelineCache, "vkCreatePipelineCache", diag),
            .vkDestroyPipelineCache = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyPipelineCache, "vkDestroyPipelineCache", diag),
            .vkCreateGraphicsPipelines = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateGraphicsPipelines, "vkCreateGraphicsPipelines", diag),
            .vkDestroyPipeline = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyPipeline, "vkDestroyPipeline", diag),

            .vkCreateRenderPass = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateRenderPass, "vkCreateRenderPass", diag),
            .vkDestroyRenderPass = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyRenderPass, "vkDestroyRenderPass", diag),
            .vkCreateFramebuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateFramebuffer, "vkCreateFramebuffer", diag),
            .vkDestroyFramebuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyFramebuffer, "vkDestroyFramebuffer", diag),

            .vkCreateDescriptorSetLayout = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateDescriptorSetLayout, "vkCreateDescriptorSetLayout", diag),
            .vkDestroyDescriptorSetLayout = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyDescriptorSetLayout, "vkDestroyDescriptorSetLayout", diag),
            .vkCreateDescriptorPool = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateDescriptorPool, "vkCreateDescriptorPool", diag),
            .vkDestroyDescriptorPool = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyDescriptorPool, "vkDestroyDescriptorPool", diag),
            .vkResetDescriptorPool = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkResetDescriptorPool, "vkResetDescriptorPool", diag),
            .vkAllocateDescriptorSets = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkAllocateDescriptorSets, "vkAllocateDescriptorSets", diag),
            .vkUpdateDescriptorSets = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkUpdateDescriptorSets, "vkUpdateDescriptorSets", diag),

            .vkCreateCommandPool = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateCommandPool, "vkCreateCommandPool", diag),
            .vkDestroyCommandPool = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyCommandPool, "vkDestroyCommandPool", diag),
            .vkResetCommandPool = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkResetCommandPool, "vkResetCommandPool", diag),
            .vkAllocateCommandBuffers = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkAllocateCommandBuffers, "vkAllocateCommandBuffers", diag),
            .vkFreeCommandBuffers = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkFreeCommandBuffers, "vkFreeCommandBuffers", diag),
            .vkBeginCommandBuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkBeginCommandBuffer, "vkBeginCommandBuffer", diag),
            .vkEndCommandBuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkEndCommandBuffer, "vkEndCommandBuffer", diag),
            .vkResetCommandBuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkResetCommandBuffer, "vkResetCommandBuffer", diag),

            .vkCreateFence = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateFence, "vkCreateFence", diag),
            .vkDestroyFence = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroyFence, "vkDestroyFence", diag),
            .vkResetFences = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkResetFences, "vkResetFences", diag),
            .vkWaitForFences = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkWaitForFences, "vkWaitForFences", diag),

            .vkCreateSemaphore = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCreateSemaphore, "vkCreateSemaphore", diag),
            .vkDestroySemaphore = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkDestroySemaphore, "vkDestroySemaphore", diag),

            .vkCmdBeginRenderPass = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdBeginRenderPass, "vkCmdBeginRenderPass", diag),
            .vkCmdEndRenderPass = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdEndRenderPass, "vkCmdEndRenderPass", diag),
            .vkCmdBindPipeline = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdBindPipeline, "vkCmdBindPipeline", diag),
            .vkCmdSetViewport = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdSetViewport, "vkCmdSetViewport", diag),
            .vkCmdSetScissor = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdSetScissor, "vkCmdSetScissor", diag),
            .vkCmdBindDescriptorSets = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdBindDescriptorSets, "vkCmdBindDescriptorSets", diag),
            .vkCmdBindVertexBuffers = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdBindVertexBuffers, "vkCmdBindVertexBuffers", diag),
            .vkCmdBindIndexBuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdBindIndexBuffer, "vkCmdBindIndexBuffer", diag),
            .vkCmdDrawIndexed = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdDrawIndexed, "vkCmdDrawIndexed", diag),
            .vkCmdPipelineBarrier = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdPipelineBarrier, "vkCmdPipelineBarrier", diag),
            .vkCmdCopyBufferToImage = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdCopyBufferToImage, "vkCmdCopyBufferToImage", diag),
            .vkCmdCopyImageToBuffer = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdCopyImageToBuffer, "vkCmdCopyImageToBuffer", diag),
            .vkCmdClearColorImage = try loadDevReq(gdpa, gipa, inst, dev, vk.PFN_vkCmdClearColorImage, "vkCmdClearColorImage", diag),

            .vkCreateSwapchainKHR = loadDevOpt(gdpa, gipa, inst, dev, vk.PFN_vkCreateSwapchainKHR, "vkCreateSwapchainKHR"),
            .vkDestroySwapchainKHR = loadDevOpt(gdpa, gipa, inst, dev, vk.PFN_vkDestroySwapchainKHR, "vkDestroySwapchainKHR"),
            .vkGetSwapchainImagesKHR = loadDevOpt(gdpa, gipa, inst, dev, vk.PFN_vkGetSwapchainImagesKHR, "vkGetSwapchainImagesKHR"),
            .vkAcquireNextImageKHR = loadDevOpt(gdpa, gipa, inst, dev, vk.PFN_vkAcquireNextImageKHR, "vkAcquireNextImageKHR"),
            .vkQueuePresentKHR = loadDevOpt(gdpa, gipa, inst, dev, vk.PFN_vkQueuePresentKHR, "vkQueuePresentKHR"),
        };
    }
};

fn loadInstOpt(
    gipa: vk.PFN_vkGetInstanceProcAddr,
    inst: vk.Instance,
    comptime Fn: type,
    name: [*:0]const u8,
) ?Fn {
    const raw = gipa(inst, name) orelse return null;
    return @ptrCast(raw);
}

fn loadInstReq(
    gipa: vk.PFN_vkGetInstanceProcAddr,
    inst: vk.Instance,
    comptime Fn: type,
    name: [*:0]const u8,
    diag: *Diag,
) Error!Fn {
    return loadInstOpt(gipa, inst, Fn, name) orelse {
        diag.print("vulkan: missing required function {s}", .{std.mem.span(name)});
        return error.BackendUnavailable;
    };
}

fn loadDevOpt(
    gdpa: vk.PFN_vkGetDeviceProcAddr,
    gipa: vk.PFN_vkGetInstanceProcAddr,
    inst: vk.Instance,
    dev: vk.Device,
    comptime Fn: type,
    name: [*:0]const u8,
) ?Fn {
    if (gdpa(dev, name)) |raw| return @ptrCast(raw);
    if (gipa(inst, name)) |raw| return @ptrCast(raw);
    return null;
}

fn loadDevReq(
    gdpa: vk.PFN_vkGetDeviceProcAddr,
    gipa: vk.PFN_vkGetInstanceProcAddr,
    inst: vk.Instance,
    dev: vk.Device,
    comptime Fn: type,
    name: [*:0]const u8,
    diag: *Diag,
) Error!Fn {
    return loadDevOpt(gdpa, gipa, inst, dev, Fn, name) orelse {
        diag.print("vulkan: missing required device function {s}", .{std.mem.span(name)});
        return error.BackendUnavailable;
    };
}

pub fn check(res: vk.Result, diag: *Diag, ctx: []const u8) Error!void {
    if (res == .success) return;
    diag.print("vulkan: {s} failed (VkResult={d})", .{ ctx, @intFromEnum(res) });
    return switch (res) {
        .error_out_of_host_memory, .error_out_of_device_memory => error.OutOfMemory,
        .error_device_lost => error.DeviceLost,
        .error_surface_lost_khr, .error_out_of_date_khr => error.SurfaceLost,
        .error_initialization_failed, .error_incompatible_driver, .error_extension_not_present, .error_layer_not_present => error.BackendUnavailable,
        else => error.Internal,
    };
}
