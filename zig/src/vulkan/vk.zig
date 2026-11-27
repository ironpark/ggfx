const std = @import("std");
const builtin = @import("builtin");

comptime {
    if (@sizeOf(usize) != 8 or builtin.cpu.arch.endian() != .little) {
        @compileError("Vulkan backend requires a 64-bit little-endian target");
    }
}

pub const Bool32 = u32;
pub const FALSE: Bool32 = 0;
pub const TRUE: Bool32 = 1;

pub const DeviceSize = u64;
pub const Flags = u32;
pub const SampleMask = u32;

pub const WHOLE_SIZE: DeviceSize = ~@as(DeviceSize, 0);
pub const QUEUE_FAMILY_IGNORED: u32 = ~@as(u32, 0);
pub const REMAINING_MIP_LEVELS: u32 = ~@as(u32, 0);
pub const REMAINING_ARRAY_LAYERS: u32 = ~@as(u32, 0);
pub const SUBPASS_EXTERNAL: u32 = ~@as(u32, 0);

pub const MAX_PHYSICAL_DEVICE_NAME_SIZE: usize = 256;
pub const UUID_SIZE: usize = 16;
pub const MAX_EXTENSION_NAME_SIZE: usize = 256;
pub const MAX_DESCRIPTION_SIZE: usize = 256;
pub const MAX_MEMORY_TYPES: usize = 32;
pub const MAX_MEMORY_HEAPS: usize = 16;

pub fn makeApiVersion(variant: u32, major: u32, minor: u32, patch: u32) u32 {
    return (variant << 29) | (major << 22) | (minor << 12) | patch;
}

pub const API_VERSION_1_0: u32 = makeApiVersion(0, 1, 0, 0);
pub const API_VERSION_1_1: u32 = makeApiVersion(0, 1, 1, 0);

// Dispatchable handles (pointers on all platforms).
const InstanceImpl = opaque {};
const PhysicalDeviceImpl = opaque {};
const DeviceImpl = opaque {};
const QueueImpl = opaque {};
const CommandBufferImpl = opaque {};

pub const Instance = ?*InstanceImpl;
pub const PhysicalDevice = ?*PhysicalDeviceImpl;
pub const Device = ?*DeviceImpl;
pub const Queue = ?*QueueImpl;
pub const CommandBuffer = ?*CommandBufferImpl;

// Non-dispatchable handles (64-bit integers / opaque 64-bit handles).
pub const DeviceMemory = enum(u64) { null_handle = 0, _ };
pub const Buffer = enum(u64) { null_handle = 0, _ };
pub const Image = enum(u64) { null_handle = 0, _ };
pub const ImageView = enum(u64) { null_handle = 0, _ };
pub const ShaderModule = enum(u64) { null_handle = 0, _ };
pub const PipelineLayout = enum(u64) { null_handle = 0, _ };
pub const Pipeline = enum(u64) { null_handle = 0, _ };
pub const PipelineCache = enum(u64) { null_handle = 0, _ };
pub const RenderPass = enum(u64) { null_handle = 0, _ };
pub const Framebuffer = enum(u64) { null_handle = 0, _ };
pub const DescriptorSetLayout = enum(u64) { null_handle = 0, _ };
pub const DescriptorPool = enum(u64) { null_handle = 0, _ };
pub const DescriptorSet = enum(u64) { null_handle = 0, _ };
pub const CommandPool = enum(u64) { null_handle = 0, _ };
pub const Fence = enum(u64) { null_handle = 0, _ };
pub const Semaphore = enum(u64) { null_handle = 0, _ };
pub const Sampler = enum(u64) { null_handle = 0, _ };
pub const SurfaceKHR = enum(u64) { null_handle = 0, _ };
pub const SwapchainKHR = enum(u64) { null_handle = 0, _ };
pub const DebugUtilsMessengerEXT = enum(u64) { null_handle = 0, _ };

pub const Result = enum(i32) {
    success = 0,
    not_ready = 1,
    timeout = 2,
    event_set = 3,
    event_reset = 4,
    incomplete = 5,
    suboptimal_khr = 1000001003,
    error_out_of_host_memory = -1,
    error_out_of_device_memory = -2,
    error_initialization_failed = -3,
    error_device_lost = -4,
    error_memory_map_failed = -5,
    error_layer_not_present = -6,
    error_extension_not_present = -7,
    error_feature_not_present = -8,
    error_incompatible_driver = -9,
    error_too_many_objects = -10,
    error_format_not_supported = -11,
    error_fragmented_pool = -12,
    error_unknown = -13,
    error_out_of_pool_memory = -1000069000,
    error_surface_lost_khr = -1000000000,
    error_native_window_in_use_khr = -1000000001,
    error_out_of_date_khr = -1000001004,
    _,
};

pub const StructureType = enum(u32) {
    application_info = 0,
    instance_create_info = 1,
    device_queue_create_info = 2,
    device_create_info = 3,
    submit_info = 4,
    memory_allocate_info = 5,
    mapped_memory_range = 6,
    fence_create_info = 8,
    semaphore_create_info = 9,
    buffer_create_info = 12,
    image_create_info = 14,
    image_view_create_info = 15,
    shader_module_create_info = 16,
    pipeline_cache_create_info = 17,
    pipeline_shader_stage_create_info = 18,
    pipeline_vertex_input_state_create_info = 19,
    pipeline_input_assembly_state_create_info = 20,
    pipeline_viewport_state_create_info = 22,
    pipeline_rasterization_state_create_info = 23,
    pipeline_multisample_state_create_info = 24,
    pipeline_depth_stencil_state_create_info = 25,
    pipeline_color_blend_state_create_info = 26,
    pipeline_dynamic_state_create_info = 27,
    graphics_pipeline_create_info = 28,
    pipeline_layout_create_info = 30,
    descriptor_set_layout_create_info = 32,
    descriptor_pool_create_info = 33,
    descriptor_set_allocate_info = 34,
    write_descriptor_set = 35,
    framebuffer_create_info = 37,
    render_pass_create_info = 38,
    command_pool_create_info = 39,
    command_buffer_allocate_info = 40,
    command_buffer_begin_info = 42,
    render_pass_begin_info = 43,
    buffer_memory_barrier = 44,
    image_memory_barrier = 45,
    memory_barrier = 46,
    swapchain_create_info_khr = 1000001000,
    present_info_khr = 1000001001,
    xlib_surface_create_info_khr = 1000004000,
    wayland_surface_create_info_khr = 1000006000,
    debug_utils_messenger_callback_data_ext = 1000128003,
    debug_utils_messenger_create_info_ext = 1000128004,
    _,
};

pub const PhysicalDeviceType = enum(u32) {
    other = 0,
    integrated_gpu = 1,
    discrete_gpu = 2,
    virtual_gpu = 3,
    cpu = 4,
    _,
};

pub const Format = enum(i32) {
    undefined = 0,
    r8_unorm = 9,
    r8g8b8a8_unorm = 37,
    b8g8r8a8_unorm = 44,
    r32g32_sfloat = 103,
    r32g32b32a32_sfloat = 109,
    _,
};

pub const ImageType = enum(u32) {
    @"1d" = 0,
    @"2d" = 1,
    @"3d" = 2,
};

pub const ImageTiling = enum(u32) {
    optimal = 0,
    linear = 1,
};

pub const ImageLayout = enum(u32) {
    undefined = 0,
    general = 1,
    color_attachment_optimal = 2,
    depth_stencil_attachment_optimal = 3,
    depth_stencil_read_only_optimal = 4,
    shader_read_only_optimal = 5,
    transfer_src_optimal = 6,
    transfer_dst_optimal = 7,
    preinitialized = 8,
    present_src_khr = 1000001002,
    _,
};

pub const ImageViewType = enum(u32) {
    @"1d" = 0,
    @"2d" = 1,
    @"3d" = 2,
    cube = 3,
    @"1d_array" = 4,
    @"2d_array" = 5,
    cube_array = 6,
};

pub const ComponentSwizzle = enum(u32) {
    identity = 0,
    zero = 1,
    one = 2,
    r = 3,
    g = 4,
    b = 5,
    a = 6,
};

pub const SharingMode = enum(u32) {
    exclusive = 0,
    concurrent = 1,
};

pub const DescriptorType = enum(u32) {
    sampler = 0,
    combined_image_sampler = 1,
    sampled_image = 2,
    storage_image = 3,
    uniform_texel_buffer = 4,
    storage_texel_buffer = 5,
    uniform_buffer = 6,
    storage_buffer = 7,
    uniform_buffer_dynamic = 8,
    storage_buffer_dynamic = 9,
    input_attachment = 10,
    _,
};

pub const AttachmentLoadOp = enum(u32) {
    load = 0,
    clear = 1,
    dont_care = 2,
};

pub const AttachmentStoreOp = enum(u32) {
    store = 0,
    dont_care = 1,
};

pub const PipelineBindPoint = enum(u32) {
    graphics = 0,
    compute = 1,
};

pub const CommandBufferLevel = enum(u32) {
    primary = 0,
    secondary = 1,
};

pub const SubpassContents = enum(u32) {
    @"inline" = 0,
    secondary_command_buffers = 1,
};

pub const IndexType = enum(u32) {
    uint16 = 0,
    uint32 = 1,
};

pub const VertexInputRate = enum(u32) {
    vertex = 0,
    instance = 1,
};

pub const PrimitiveTopology = enum(u32) {
    point_list = 0,
    line_list = 1,
    line_strip = 2,
    triangle_list = 3,
    triangle_strip = 4,
    triangle_fan = 5,
};

pub const PolygonMode = enum(u32) {
    fill = 0,
    line = 1,
    point = 2,
};

pub const FrontFace = enum(u32) {
    counter_clockwise = 0,
    clockwise = 1,
};

pub const CompareOp = enum(u32) {
    never = 0,
    less = 1,
    equal = 2,
    less_or_equal = 3,
    greater = 4,
    not_equal = 5,
    greater_or_equal = 6,
    always = 7,
};

pub const LogicOp = enum(u32) {
    clear = 0,
    @"and" = 1,
    and_reverse = 2,
    copy = 3,
    and_inverted = 4,
    no_op = 5,
    xor = 6,
    @"or" = 7,
    nor = 8,
    equivalent = 9,
    invert = 10,
    or_reverse = 11,
    copy_inverted = 12,
    or_inverted = 13,
    nand = 14,
    set = 15,
};

pub const BlendFactor = enum(u32) {
    zero = 0,
    one = 1,
    src_color = 2,
    one_minus_src_color = 3,
    dst_color = 4,
    one_minus_dst_color = 5,
    src_alpha = 6,
    one_minus_src_alpha = 7,
    dst_alpha = 8,
    one_minus_dst_alpha = 9,
    constant_color = 10,
    one_minus_constant_color = 11,
    constant_alpha = 12,
    one_minus_constant_alpha = 13,
    src_alpha_saturate = 14,
};

pub const BlendOp = enum(u32) {
    add = 0,
    subtract = 1,
    reverse_subtract = 2,
    min = 3,
    max = 4,
};

pub const DynamicState = enum(u32) {
    viewport = 0,
    scissor = 1,
    line_width = 2,
    depth_bias = 3,
    blend_constants = 4,
    depth_bounds = 5,
    stencil_compare_mask = 6,
    stencil_write_mask = 7,
    stencil_reference = 8,
};

pub const ColorSpaceKHR = enum(u32) {
    srgb_nonlinear_khr = 0,
    _,
};

pub const PresentModeKHR = enum(u32) {
    immediate_khr = 0,
    mailbox_khr = 1,
    fifo_khr = 2,
    fifo_relaxed_khr = 3,
    _,
};

// Bitmask flag constants.
pub const QueueFlags = struct {
    pub const graphics_bit: Flags = 0x00000001;
    pub const compute_bit: Flags = 0x00000002;
    pub const transfer_bit: Flags = 0x00000004;
};

pub const FormatFeature = struct {
    pub const sampled_image_bit: Flags = 0x00000001;
    pub const color_attachment_bit: Flags = 0x00000080;
    pub const color_attachment_blend_bit: Flags = 0x00000100;
    pub const transfer_src_bit: Flags = 0x00004000;
    pub const transfer_dst_bit: Flags = 0x00008000;
};

pub const MemoryProperty = struct {
    pub const device_local_bit: Flags = 0x00000001;
    pub const host_visible_bit: Flags = 0x00000002;
    pub const host_coherent_bit: Flags = 0x00000004;
    pub const host_cached_bit: Flags = 0x00000008;
};

pub const BufferUsage = struct {
    pub const transfer_src_bit: Flags = 0x00000001;
    pub const transfer_dst_bit: Flags = 0x00000002;
    pub const uniform_buffer_bit: Flags = 0x00000010;
    pub const index_buffer_bit: Flags = 0x00000040;
    pub const vertex_buffer_bit: Flags = 0x00000080;
};

pub const ImageUsage = struct {
    pub const transfer_src_bit: Flags = 0x00000001;
    pub const transfer_dst_bit: Flags = 0x00000002;
    pub const sampled_bit: Flags = 0x00000004;
    pub const color_attachment_bit: Flags = 0x00000010;
};

pub const ImageAspect = struct {
    pub const color_bit: Flags = 0x00000001;
};

pub const SampleCount = struct {
    pub const @"1_bit": Flags = 0x00000001;
};

pub const ShaderStage = struct {
    pub const vertex_bit: Flags = 0x00000001;
    pub const fragment_bit: Flags = 0x00000010;
    pub const all_graphics: Flags = 0x0000001F;
};

pub const CullMode = struct {
    pub const none: Flags = 0;
    pub const front_bit: Flags = 0x00000001;
    pub const back_bit: Flags = 0x00000002;
};

pub const ColorComponent = struct {
    pub const r_bit: Flags = 0x00000001;
    pub const g_bit: Flags = 0x00000002;
    pub const b_bit: Flags = 0x00000004;
    pub const a_bit: Flags = 0x00000008;
    pub const rgba_bits: Flags = r_bit | g_bit | b_bit | a_bit;
};

pub const PipelineStage = struct {
    pub const top_of_pipe_bit: Flags = 0x00000001;
    pub const draw_indirect_bit: Flags = 0x00000002;
    pub const vertex_input_bit: Flags = 0x00000004;
    pub const vertex_shader_bit: Flags = 0x00000008;
    pub const fragment_shader_bit: Flags = 0x00000080;
    pub const color_attachment_output_bit: Flags = 0x00000400;
    pub const transfer_bit: Flags = 0x00001000;
    pub const bottom_of_pipe_bit: Flags = 0x00002000;
    pub const host_bit: Flags = 0x00004000;
    pub const all_graphics_bit: Flags = 0x00008000;
    pub const all_commands_bit: Flags = 0x00010000;
};

pub const Access = struct {
    pub const shader_read_bit: Flags = 0x00000020;
    pub const color_attachment_read_bit: Flags = 0x00000080;
    pub const color_attachment_write_bit: Flags = 0x00000100;
    pub const transfer_read_bit: Flags = 0x00000800;
    pub const transfer_write_bit: Flags = 0x00001000;
    pub const host_read_bit: Flags = 0x00002000;
    pub const host_write_bit: Flags = 0x00004000;
    pub const memory_read_bit: Flags = 0x00008000;
    pub const memory_write_bit: Flags = 0x00010000;
};

pub const CommandPoolCreate = struct {
    pub const transient_bit: Flags = 0x00000001;
    pub const reset_command_buffer_bit: Flags = 0x00000002;
};

pub const CommandBufferUsage = struct {
    pub const one_time_submit_bit: Flags = 0x00000001;
};

pub const FenceCreate = struct {
    pub const signaled_bit: Flags = 0x00000001;
};

pub const SurfaceTransformKHR = struct {
    pub const identity_bit_khr: Flags = 0x00000001;
};

pub const CompositeAlphaKHR = struct {
    pub const opaque_bit_khr: Flags = 0x00000001;
    pub const pre_multiplied_bit_khr: Flags = 0x00000002;
    pub const post_multiplied_bit_khr: Flags = 0x00000004;
    pub const inherit_bit_khr: Flags = 0x00000008;
};

pub const DebugUtilsMessageSeverityEXT = struct {
    pub const verbose_bit_ext: Flags = 0x00000001;
    pub const info_bit_ext: Flags = 0x00000010;
    pub const warning_bit_ext: Flags = 0x00000100;
    pub const error_bit_ext: Flags = 0x00001000;
};

pub const DebugUtilsMessageTypeEXT = struct {
    pub const general_bit_ext: Flags = 0x00000001;
    pub const validation_bit_ext: Flags = 0x00000002;
    pub const performance_bit_ext: Flags = 0x00000004;
};

// Structs.
pub const AllocationCallbacks = opaque {};

pub const Offset2D = extern struct {
    x: i32 = 0,
    y: i32 = 0,
};

pub const Offset3D = extern struct {
    x: i32 = 0,
    y: i32 = 0,
    z: i32 = 0,
};

pub const Extent2D = extern struct {
    width: u32 = 0,
    height: u32 = 0,
};

pub const Extent3D = extern struct {
    width: u32 = 0,
    height: u32 = 0,
    depth: u32 = 0,
};

pub const Rect2D = extern struct {
    offset: Offset2D = .{},
    extent: Extent2D = .{},
};

pub const Viewport = extern struct {
    x: f32 = 0,
    y: f32 = 0,
    width: f32 = 0,
    height: f32 = 0,
    minDepth: f32 = 0,
    maxDepth: f32 = 1,
};

pub const ComponentMapping = extern struct {
    r: ComponentSwizzle = .identity,
    g: ComponentSwizzle = .identity,
    b: ComponentSwizzle = .identity,
    a: ComponentSwizzle = .identity,
};

pub const ImageSubresourceRange = extern struct {
    aspectMask: Flags = ImageAspect.color_bit,
    baseMipLevel: u32 = 0,
    levelCount: u32 = 1,
    baseArrayLayer: u32 = 0,
    layerCount: u32 = 1,
};

pub const ImageSubresourceLayers = extern struct {
    aspectMask: Flags = ImageAspect.color_bit,
    mipLevel: u32 = 0,
    baseArrayLayer: u32 = 0,
    layerCount: u32 = 1,
};

pub const ExtensionProperties = extern struct {
    extensionName: [MAX_EXTENSION_NAME_SIZE]u8 = @splat(0),
    specVersion: u32 = 0,
};

pub const LayerProperties = extern struct {
    layerName: [MAX_EXTENSION_NAME_SIZE]u8 = @splat(0),
    specVersion: u32 = 0,
    implementationVersion: u32 = 0,
    description: [MAX_DESCRIPTION_SIZE]u8 = @splat(0),
};

pub const ApplicationInfo = extern struct {
    sType: StructureType = .application_info,
    pNext: ?*const anyopaque = null,
    pApplicationName: ?[*:0]const u8 = null,
    applicationVersion: u32 = 0,
    pEngineName: ?[*:0]const u8 = null,
    engineVersion: u32 = 0,
    apiVersion: u32 = API_VERSION_1_0,
};

pub const InstanceCreateInfo = extern struct {
    sType: StructureType = .instance_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    pApplicationInfo: ?*const ApplicationInfo = null,
    enabledLayerCount: u32 = 0,
    ppEnabledLayerNames: ?[*]const [*:0]const u8 = null,
    enabledExtensionCount: u32 = 0,
    ppEnabledExtensionNames: ?[*]const [*:0]const u8 = null,
};

pub const PhysicalDeviceLimits = extern struct {
    maxImageDimension1D: u32,
    maxImageDimension2D: u32,
    maxImageDimension3D: u32,
    maxImageDimensionCube: u32,
    maxImageArrayLayers: u32,
    maxTexelBufferElements: u32,
    maxUniformBufferRange: u32,
    maxStorageBufferRange: u32,
    maxPushConstantsSize: u32,
    maxMemoryAllocationCount: u32,
    maxSamplerAllocationCount: u32,
    bufferImageGranularity: DeviceSize,
    sparseAddressSpaceSize: DeviceSize,
    maxBoundDescriptorSets: u32,
    maxPerStageDescriptorSamplers: u32,
    maxPerStageDescriptorUniformBuffers: u32,
    maxPerStageDescriptorStorageBuffers: u32,
    maxPerStageDescriptorSampledImages: u32,
    maxPerStageDescriptorStorageImages: u32,
    maxPerStageDescriptorInputAttachments: u32,
    maxPerStageResources: u32,
    maxDescriptorSetSamplers: u32,
    maxDescriptorSetUniformBuffers: u32,
    maxDescriptorSetUniformBuffersDynamic: u32,
    maxDescriptorSetStorageBuffers: u32,
    maxDescriptorSetStorageBuffersDynamic: u32,
    maxDescriptorSetSampledImages: u32,
    maxDescriptorSetStorageImages: u32,
    maxDescriptorSetInputAttachments: u32,
    maxVertexInputAttributes: u32,
    maxVertexInputBindings: u32,
    maxVertexInputAttributeOffset: u32,
    maxVertexInputBindingStride: u32,
    maxVertexOutputComponents: u32,
    maxTessellationGenerationLevel: u32,
    maxTessellationPatchSize: u32,
    maxTessellationControlPerVertexInputComponents: u32,
    maxTessellationControlPerVertexOutputComponents: u32,
    maxTessellationControlPerPatchOutputComponents: u32,
    maxTessellationControlTotalOutputComponents: u32,
    maxTessellationEvaluationInputComponents: u32,
    maxTessellationEvaluationOutputComponents: u32,
    maxGeometryShaderInvocations: u32,
    maxGeometryInputComponents: u32,
    maxGeometryOutputComponents: u32,
    maxGeometryOutputVertices: u32,
    maxGeometryTotalOutputComponents: u32,
    maxFragmentInputComponents: u32,
    maxFragmentOutputAttachments: u32,
    maxFragmentDualSrcAttachments: u32,
    maxFragmentCombinedOutputResources: u32,
    maxComputeSharedMemorySize: u32,
    maxComputeWorkGroupCount: [3]u32,
    maxComputeWorkGroupInvocations: u32,
    maxComputeWorkGroupSize: [3]u32,
    subPixelPrecisionBits: u32,
    subTexelPrecisionBits: u32,
    mipmapPrecisionBits: u32,
    maxDrawIndexedIndexValue: u32,
    maxDrawIndirectCount: u32,
    maxSamplerLodBias: f32,
    maxSamplerAnisotropy: f32,
    maxViewports: u32,
    maxViewportDimensions: [2]u32,
    viewportBoundsRange: [2]f32,
    viewportSubPixelBits: u32,
    minMemoryMapAlignment: usize,
    minTexelBufferOffsetAlignment: DeviceSize,
    minUniformBufferOffsetAlignment: DeviceSize,
    minStorageBufferOffsetAlignment: DeviceSize,
    minTexelOffset: i32,
    maxTexelOffset: u32,
    minTexelGatherOffset: i32,
    maxTexelGatherOffset: u32,
    minInterpolationOffset: f32,
    maxInterpolationOffset: f32,
    subPixelInterpolationOffsetBits: u32,
    maxFramebufferWidth: u32,
    maxFramebufferHeight: u32,
    maxFramebufferLayers: u32,
    framebufferColorSampleCounts: Flags,
    framebufferDepthSampleCounts: Flags,
    framebufferStencilSampleCounts: Flags,
    framebufferNoAttachmentsSampleCounts: Flags,
    maxColorAttachments: u32,
    sampledImageColorSampleCounts: Flags,
    sampledImageIntegerSampleCounts: Flags,
    sampledImageDepthSampleCounts: Flags,
    sampledImageStencilSampleCounts: Flags,
    storageImageSampleCounts: Flags,
    maxSampleMaskWords: u32,
    timestampComputeAndGraphics: Bool32,
    timestampPeriod: f32,
    maxClipDistances: u32,
    maxCullDistances: u32,
    maxCombinedClipAndCullDistances: u32,
    discreteQueuePriorities: u32,
    pointSizeRange: [2]f32,
    lineWidthRange: [2]f32,
    pointSizeGranularity: f32,
    lineWidthGranularity: f32,
    strictLines: Bool32,
    standardSampleLocations: Bool32,
    optimalBufferCopyOffsetAlignment: DeviceSize,
    optimalBufferCopyRowPitchAlignment: DeviceSize,
    nonCoherentAtomSize: DeviceSize,
};

pub const PhysicalDeviceSparseProperties = extern struct {
    residencyStandard2DBlockShape: Bool32,
    residencyStandard2DMultisampleBlockShape: Bool32,
    residencyStandard3DBlockShape: Bool32,
    residencyAlignedMipSize: Bool32,
    residencyNonResidentStrict: Bool32,
};

pub const PhysicalDeviceProperties = extern struct {
    apiVersion: u32,
    driverVersion: u32,
    vendorID: u32,
    deviceID: u32,
    deviceType: PhysicalDeviceType,
    deviceName: [MAX_PHYSICAL_DEVICE_NAME_SIZE]u8,
    pipelineCacheUUID: [UUID_SIZE]u8,
    limits: PhysicalDeviceLimits,
    sparseProperties: PhysicalDeviceSparseProperties,
};

pub const MemoryType = extern struct {
    propertyFlags: Flags,
    heapIndex: u32,
};

pub const MemoryHeap = extern struct {
    size: DeviceSize,
    flags: Flags,
};

pub const PhysicalDeviceMemoryProperties = extern struct {
    memoryTypeCount: u32,
    memoryTypes: [MAX_MEMORY_TYPES]MemoryType,
    memoryHeapCount: u32,
    memoryHeaps: [MAX_MEMORY_HEAPS]MemoryHeap,
};

pub const FormatProperties = extern struct {
    linearTilingFeatures: Flags,
    optimalTilingFeatures: Flags,
    bufferFeatures: Flags,
};

pub const QueueFamilyProperties = extern struct {
    queueFlags: Flags,
    queueCount: u32,
    timestampValidBits: u32,
    minImageTransferGranularity: Extent3D,
};

pub const DeviceQueueCreateInfo = extern struct {
    sType: StructureType = .device_queue_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    queueFamilyIndex: u32 = 0,
    queueCount: u32 = 0,
    pQueuePriorities: ?[*]const f32 = null,
};

pub const DeviceCreateInfo = extern struct {
    sType: StructureType = .device_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    queueCreateInfoCount: u32 = 0,
    pQueueCreateInfos: ?[*]const DeviceQueueCreateInfo = null,
    enabledLayerCount: u32 = 0,
    ppEnabledLayerNames: ?[*]const [*:0]const u8 = null,
    enabledExtensionCount: u32 = 0,
    ppEnabledExtensionNames: ?[*]const [*:0]const u8 = null,
    pEnabledFeatures: ?*const anyopaque = null,
};

pub const MemoryRequirements = extern struct {
    size: DeviceSize,
    alignment: DeviceSize,
    memoryTypeBits: u32,
};

pub const MemoryAllocateInfo = extern struct {
    sType: StructureType = .memory_allocate_info,
    pNext: ?*const anyopaque = null,
    allocationSize: DeviceSize = 0,
    memoryTypeIndex: u32 = 0,
};

pub const MappedMemoryRange = extern struct {
    sType: StructureType = .mapped_memory_range,
    pNext: ?*const anyopaque = null,
    memory: DeviceMemory = .null_handle,
    offset: DeviceSize = 0,
    size: DeviceSize = 0,
};

pub const BufferCreateInfo = extern struct {
    sType: StructureType = .buffer_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    size: DeviceSize = 0,
    usage: Flags = 0,
    sharingMode: SharingMode = .exclusive,
    queueFamilyIndexCount: u32 = 0,
    pQueueFamilyIndices: ?[*]const u32 = null,
};

pub const ImageCreateInfo = extern struct {
    sType: StructureType = .image_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    imageType: ImageType = .@"2d",
    format: Format = .undefined,
    extent: Extent3D = .{},
    mipLevels: u32 = 1,
    arrayLayers: u32 = 1,
    samples: Flags = SampleCount.@"1_bit",
    tiling: ImageTiling = .optimal,
    usage: Flags = 0,
    sharingMode: SharingMode = .exclusive,
    queueFamilyIndexCount: u32 = 0,
    pQueueFamilyIndices: ?[*]const u32 = null,
    initialLayout: ImageLayout = .undefined,
};

pub const ImageViewCreateInfo = extern struct {
    sType: StructureType = .image_view_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    image: Image = .null_handle,
    viewType: ImageViewType = .@"2d",
    format: Format = .undefined,
    components: ComponentMapping = .{},
    subresourceRange: ImageSubresourceRange = .{},
};

pub const ShaderModuleCreateInfo = extern struct {
    sType: StructureType = .shader_module_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    codeSize: usize = 0,
    pCode: ?[*]const u32 = null,
};

pub const DescriptorSetLayoutBinding = extern struct {
    binding: u32 = 0,
    descriptorType: DescriptorType = .uniform_buffer_dynamic,
    descriptorCount: u32 = 1,
    stageFlags: Flags = 0,
    pImmutableSamplers: ?[*]const Sampler = null,
};

pub const DescriptorSetLayoutCreateInfo = extern struct {
    sType: StructureType = .descriptor_set_layout_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    bindingCount: u32 = 0,
    pBindings: ?[*]const DescriptorSetLayoutBinding = null,
};

pub const DescriptorPoolSize = extern struct {
    type: DescriptorType = .uniform_buffer_dynamic,
    descriptorCount: u32 = 0,
};

pub const DescriptorPoolCreateInfo = extern struct {
    sType: StructureType = .descriptor_pool_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    maxSets: u32 = 0,
    poolSizeCount: u32 = 0,
    pPoolSizes: ?[*]const DescriptorPoolSize = null,
};

pub const DescriptorSetAllocateInfo = extern struct {
    sType: StructureType = .descriptor_set_allocate_info,
    pNext: ?*const anyopaque = null,
    descriptorPool: DescriptorPool = .null_handle,
    descriptorSetCount: u32 = 0,
    pSetLayouts: ?[*]const DescriptorSetLayout = null,
};

pub const DescriptorImageInfo = extern struct {
    sampler: Sampler = .null_handle,
    imageView: ImageView = .null_handle,
    imageLayout: ImageLayout = .shader_read_only_optimal,
};

pub const DescriptorBufferInfo = extern struct {
    buffer: Buffer = .null_handle,
    offset: DeviceSize = 0,
    range: DeviceSize = 0,
};

pub const WriteDescriptorSet = extern struct {
    sType: StructureType = .write_descriptor_set,
    pNext: ?*const anyopaque = null,
    dstSet: DescriptorSet = .null_handle,
    dstBinding: u32 = 0,
    dstArrayElement: u32 = 0,
    descriptorCount: u32 = 0,
    descriptorType: DescriptorType = .uniform_buffer_dynamic,
    pImageInfo: ?[*]const DescriptorImageInfo = null,
    pBufferInfo: ?[*]const DescriptorBufferInfo = null,
    pTexelBufferView: ?[*]const u64 = null,
};

pub const PipelineLayoutCreateInfo = extern struct {
    sType: StructureType = .pipeline_layout_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    setLayoutCount: u32 = 0,
    pSetLayouts: ?[*]const DescriptorSetLayout = null,
    pushConstantRangeCount: u32 = 0,
    pPushConstantRanges: ?*const anyopaque = null,
};

pub const PipelineCacheCreateInfo = extern struct {
    sType: StructureType = .pipeline_cache_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    initialDataSize: usize = 0,
    pInitialData: ?*const anyopaque = null,
};

pub const PipelineShaderStageCreateInfo = extern struct {
    sType: StructureType = .pipeline_shader_stage_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    stage: Flags = 0,
    module: ShaderModule = .null_handle,
    pName: [*:0]const u8 = "main",
    pSpecializationInfo: ?*const anyopaque = null,
};

pub const VertexInputBindingDescription = extern struct {
    binding: u32 = 0,
    stride: u32 = 0,
    inputRate: VertexInputRate = .vertex,
};

pub const VertexInputAttributeDescription = extern struct {
    location: u32 = 0,
    binding: u32 = 0,
    format: Format = .undefined,
    offset: u32 = 0,
};

pub const PipelineVertexInputStateCreateInfo = extern struct {
    sType: StructureType = .pipeline_vertex_input_state_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    vertexBindingDescriptionCount: u32 = 0,
    pVertexBindingDescriptions: ?[*]const VertexInputBindingDescription = null,
    vertexAttributeDescriptionCount: u32 = 0,
    pVertexAttributeDescriptions: ?[*]const VertexInputAttributeDescription = null,
};

pub const PipelineInputAssemblyStateCreateInfo = extern struct {
    sType: StructureType = .pipeline_input_assembly_state_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    topology: PrimitiveTopology = .triangle_list,
    primitiveRestartEnable: Bool32 = FALSE,
};

pub const PipelineViewportStateCreateInfo = extern struct {
    sType: StructureType = .pipeline_viewport_state_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    viewportCount: u32 = 1,
    pViewports: ?[*]const Viewport = null,
    scissorCount: u32 = 1,
    pScissors: ?[*]const Rect2D = null,
};

pub const PipelineRasterizationStateCreateInfo = extern struct {
    sType: StructureType = .pipeline_rasterization_state_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    depthClampEnable: Bool32 = FALSE,
    rasterizerDiscardEnable: Bool32 = FALSE,
    polygonMode: PolygonMode = .fill,
    cullMode: Flags = CullMode.none,
    frontFace: FrontFace = .counter_clockwise,
    depthBiasEnable: Bool32 = FALSE,
    depthBiasConstantFactor: f32 = 0,
    depthBiasClamp: f32 = 0,
    depthBiasSlopeFactor: f32 = 0,
    lineWidth: f32 = 1.0,
};

pub const PipelineMultisampleStateCreateInfo = extern struct {
    sType: StructureType = .pipeline_multisample_state_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    rasterizationSamples: Flags = SampleCount.@"1_bit",
    sampleShadingEnable: Bool32 = FALSE,
    minSampleShading: f32 = 1.0,
    pSampleMask: ?*const SampleMask = null,
    alphaToCoverageEnable: Bool32 = FALSE,
    alphaToOneEnable: Bool32 = FALSE,
};

pub const PipelineColorBlendAttachmentState = extern struct {
    blendEnable: Bool32 = FALSE,
    srcColorBlendFactor: BlendFactor = .one,
    dstColorBlendFactor: BlendFactor = .zero,
    colorBlendOp: BlendOp = .add,
    srcAlphaBlendFactor: BlendFactor = .one,
    dstAlphaBlendFactor: BlendFactor = .zero,
    alphaBlendOp: BlendOp = .add,
    colorWriteMask: Flags = ColorComponent.rgba_bits,
};

pub const PipelineColorBlendStateCreateInfo = extern struct {
    sType: StructureType = .pipeline_color_blend_state_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    logicOpEnable: Bool32 = FALSE,
    logicOp: LogicOp = .copy,
    attachmentCount: u32 = 0,
    pAttachments: ?[*]const PipelineColorBlendAttachmentState = null,
    blendConstants: [4]f32 = .{ 0, 0, 0, 0 },
};

pub const PipelineDynamicStateCreateInfo = extern struct {
    sType: StructureType = .pipeline_dynamic_state_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    dynamicStateCount: u32 = 0,
    pDynamicStates: ?[*]const DynamicState = null,
};

pub const GraphicsPipelineCreateInfo = extern struct {
    sType: StructureType = .graphics_pipeline_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    stageCount: u32 = 0,
    pStages: ?[*]const PipelineShaderStageCreateInfo = null,
    pVertexInputState: ?*const PipelineVertexInputStateCreateInfo = null,
    pInputAssemblyState: ?*const PipelineInputAssemblyStateCreateInfo = null,
    pTessellationState: ?*const anyopaque = null,
    pViewportState: ?*const PipelineViewportStateCreateInfo = null,
    pRasterizationState: ?*const PipelineRasterizationStateCreateInfo = null,
    pMultisampleState: ?*const PipelineMultisampleStateCreateInfo = null,
    pDepthStencilState: ?*const anyopaque = null,
    pColorBlendState: ?*const PipelineColorBlendStateCreateInfo = null,
    pDynamicState: ?*const PipelineDynamicStateCreateInfo = null,
    layout: PipelineLayout = .null_handle,
    renderPass: RenderPass = .null_handle,
    subpass: u32 = 0,
    basePipelineHandle: Pipeline = .null_handle,
    basePipelineIndex: i32 = -1,
};

pub const AttachmentDescription = extern struct {
    flags: Flags = 0,
    format: Format = .undefined,
    samples: Flags = SampleCount.@"1_bit",
    loadOp: AttachmentLoadOp = .load,
    storeOp: AttachmentStoreOp = .store,
    stencilLoadOp: AttachmentLoadOp = .dont_care,
    stencilStoreOp: AttachmentStoreOp = .dont_care,
    initialLayout: ImageLayout = .color_attachment_optimal,
    finalLayout: ImageLayout = .color_attachment_optimal,
};

pub const AttachmentReference = extern struct {
    attachment: u32 = 0,
    layout: ImageLayout = .color_attachment_optimal,
};

pub const SubpassDescription = extern struct {
    flags: Flags = 0,
    pipelineBindPoint: PipelineBindPoint = .graphics,
    inputAttachmentCount: u32 = 0,
    pInputAttachments: ?[*]const AttachmentReference = null,
    colorAttachmentCount: u32 = 0,
    pColorAttachments: ?[*]const AttachmentReference = null,
    pResolveAttachments: ?[*]const AttachmentReference = null,
    pDepthStencilAttachment: ?*const AttachmentReference = null,
    preserveAttachmentCount: u32 = 0,
    pPreserveAttachments: ?[*]const u32 = null,
};

pub const SubpassDependency = extern struct {
    srcSubpass: u32 = 0,
    dstSubpass: u32 = 0,
    srcStageMask: Flags = 0,
    dstStageMask: Flags = 0,
    srcAccessMask: Flags = 0,
    dstAccessMask: Flags = 0,
    dependencyFlags: Flags = 0,
};

pub const RenderPassCreateInfo = extern struct {
    sType: StructureType = .render_pass_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    attachmentCount: u32 = 0,
    pAttachments: ?[*]const AttachmentDescription = null,
    subpassCount: u32 = 0,
    pSubpasses: ?[*]const SubpassDescription = null,
    dependencyCount: u32 = 0,
    pDependencies: ?[*]const SubpassDependency = null,
};

pub const FramebufferCreateInfo = extern struct {
    sType: StructureType = .framebuffer_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    renderPass: RenderPass = .null_handle,
    attachmentCount: u32 = 0,
    pAttachments: ?[*]const ImageView = null,
    width: u32 = 0,
    height: u32 = 0,
    layers: u32 = 1,
};

pub const ClearColorValue = extern union {
    float32: [4]f32,
    int32: [4]i32,
    uint32: [4]u32,
};

pub const ClearDepthStencilValue = extern struct {
    depth: f32,
    stencil: u32,
};

pub const ClearValue = extern union {
    color: ClearColorValue,
    depthStencil: ClearDepthStencilValue,
};

pub const RenderPassBeginInfo = extern struct {
    sType: StructureType = .render_pass_begin_info,
    pNext: ?*const anyopaque = null,
    renderPass: RenderPass = .null_handle,
    framebuffer: Framebuffer = .null_handle,
    renderArea: Rect2D = .{},
    clearValueCount: u32 = 0,
    pClearValues: ?[*]const ClearValue = null,
};

pub const CommandPoolCreateInfo = extern struct {
    sType: StructureType = .command_pool_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    queueFamilyIndex: u32 = 0,
};

pub const CommandBufferAllocateInfo = extern struct {
    sType: StructureType = .command_buffer_allocate_info,
    pNext: ?*const anyopaque = null,
    commandPool: CommandPool = .null_handle,
    level: CommandBufferLevel = .primary,
    commandBufferCount: u32 = 1,
};

pub const CommandBufferBeginInfo = extern struct {
    sType: StructureType = .command_buffer_begin_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    pInheritanceInfo: ?*const anyopaque = null,
};

pub const FenceCreateInfo = extern struct {
    sType: StructureType = .fence_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
};

pub const SemaphoreCreateInfo = extern struct {
    sType: StructureType = .semaphore_create_info,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
};

pub const SubmitInfo = extern struct {
    sType: StructureType = .submit_info,
    pNext: ?*const anyopaque = null,
    waitSemaphoreCount: u32 = 0,
    pWaitSemaphores: ?[*]const Semaphore = null,
    pWaitDstStageMask: ?[*]const Flags = null,
    commandBufferCount: u32 = 0,
    pCommandBuffers: ?[*]const CommandBuffer = null,
    signalSemaphoreCount: u32 = 0,
    pSignalSemaphores: ?[*]const Semaphore = null,
};

pub const BufferMemoryBarrier = extern struct {
    sType: StructureType = .buffer_memory_barrier,
    pNext: ?*const anyopaque = null,
    srcAccessMask: Flags = 0,
    dstAccessMask: Flags = 0,
    srcQueueFamilyIndex: u32 = QUEUE_FAMILY_IGNORED,
    dstQueueFamilyIndex: u32 = QUEUE_FAMILY_IGNORED,
    buffer: Buffer = .null_handle,
    offset: DeviceSize = 0,
    size: DeviceSize = WHOLE_SIZE,
};

pub const ImageMemoryBarrier = extern struct {
    sType: StructureType = .image_memory_barrier,
    pNext: ?*const anyopaque = null,
    srcAccessMask: Flags = 0,
    dstAccessMask: Flags = 0,
    oldLayout: ImageLayout = .undefined,
    newLayout: ImageLayout = .undefined,
    srcQueueFamilyIndex: u32 = QUEUE_FAMILY_IGNORED,
    dstQueueFamilyIndex: u32 = QUEUE_FAMILY_IGNORED,
    image: Image = .null_handle,
    subresourceRange: ImageSubresourceRange = .{},
};

pub const BufferImageCopy = extern struct {
    bufferOffset: DeviceSize = 0,
    bufferRowLength: u32 = 0,
    bufferImageHeight: u32 = 0,
    imageSubresource: ImageSubresourceLayers = .{},
    imageOffset: Offset3D = .{},
    imageExtent: Extent3D = .{},
};

// WSI / KHR Surface & Swapchain structs.
pub const SurfaceCapabilitiesKHR = extern struct {
    minImageCount: u32,
    maxImageCount: u32,
    currentExtent: Extent2D,
    minImageExtent: Extent2D,
    maxImageExtent: Extent2D,
    maxImageArrayLayers: u32,
    supportedTransforms: Flags,
    currentTransform: Flags,
    supportedCompositeAlpha: Flags,
    supportedUsageFlags: Flags,
};

pub const SurfaceFormatKHR = extern struct {
    format: Format,
    colorSpace: ColorSpaceKHR,
};

pub const SwapchainCreateInfoKHR = extern struct {
    sType: StructureType = .swapchain_create_info_khr,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    surface: SurfaceKHR = .null_handle,
    minImageCount: u32 = 2,
    imageFormat: Format = .undefined,
    imageColorSpace: ColorSpaceKHR = .srgb_nonlinear_khr,
    imageExtent: Extent2D = .{},
    imageArrayLayers: u32 = 1,
    imageUsage: Flags = ImageUsage.color_attachment_bit,
    imageSharingMode: SharingMode = .exclusive,
    queueFamilyIndexCount: u32 = 0,
    pQueueFamilyIndices: ?[*]const u32 = null,
    preTransform: Flags = SurfaceTransformKHR.identity_bit_khr,
    compositeAlpha: Flags = CompositeAlphaKHR.opaque_bit_khr,
    presentMode: PresentModeKHR = .fifo_khr,
    clipped: Bool32 = TRUE,
    oldSwapchain: SwapchainKHR = .null_handle,
};

pub const PresentInfoKHR = extern struct {
    sType: StructureType = .present_info_khr,
    pNext: ?*const anyopaque = null,
    waitSemaphoreCount: u32 = 0,
    pWaitSemaphores: ?[*]const Semaphore = null,
    swapchainCount: u32 = 0,
    pSwapchains: ?[*]const SwapchainKHR = null,
    pImageIndices: ?[*]const u32 = null,
    pResults: ?[*]Result = null,
};

pub const XlibSurfaceCreateInfoKHR = extern struct {
    sType: StructureType = .xlib_surface_create_info_khr,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    dpy: ?*anyopaque = null,
    window: usize = 0,
};

pub const WaylandSurfaceCreateInfoKHR = extern struct {
    sType: StructureType = .wayland_surface_create_info_khr,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    display: ?*anyopaque = null,
    surface: ?*anyopaque = null,
};

// VK_EXT_debug_utils structs.
pub const DebugUtilsMessengerCallbackDataEXT = extern struct {
    sType: StructureType,
    pNext: ?*const anyopaque,
    flags: Flags,
    pMessageIdName: ?[*:0]const u8,
    messageIdNumber: i32,
    pMessage: ?[*:0]const u8,
    queueLabelCount: u32,
    pQueueLabels: ?*const anyopaque,
    cmdBufLabelCount: u32,
    pCmdBufLabels: ?*const anyopaque,
    objectCount: u32,
    pObjects: ?*const anyopaque,
};

pub const PFN_vkDebugUtilsMessengerCallbackEXT = *const fn (
    messageSeverity: Flags,
    messageTypes: Flags,
    pCallbackData: *const DebugUtilsMessengerCallbackDataEXT,
    pUserData: ?*anyopaque,
) callconv(.c) Bool32;

pub const DebugUtilsMessengerCreateInfoEXT = extern struct {
    sType: StructureType = .debug_utils_messenger_create_info_ext,
    pNext: ?*const anyopaque = null,
    flags: Flags = 0,
    messageSeverity: Flags = 0,
    messageType: Flags = 0,
    pfnUserCallback: PFN_vkDebugUtilsMessengerCallbackEXT,
    pUserData: ?*anyopaque = null,
};

// Function pointer typedefs.
pub const PFN_vkVoidFunction = *const fn () callconv(.c) void;
pub const PFN_vkGetInstanceProcAddr = *const fn (Instance, [*:0]const u8) callconv(.c) ?PFN_vkVoidFunction;
pub const PFN_vkGetDeviceProcAddr = *const fn (Device, [*:0]const u8) callconv(.c) ?PFN_vkVoidFunction;

// Global commands.
pub const PFN_vkEnumerateInstanceVersion = *const fn (*u32) callconv(.c) Result;
pub const PFN_vkEnumerateInstanceExtensionProperties = *const fn (?[*:0]const u8, *u32, ?[*]ExtensionProperties) callconv(.c) Result;
pub const PFN_vkEnumerateInstanceLayerProperties = *const fn (*u32, ?[*]LayerProperties) callconv(.c) Result;
pub const PFN_vkCreateInstance = *const fn (*const InstanceCreateInfo, ?*const AllocationCallbacks, *Instance) callconv(.c) Result;

// Instance commands.
pub const PFN_vkDestroyInstance = *const fn (Instance, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkEnumeratePhysicalDevices = *const fn (Instance, *u32, ?[*]PhysicalDevice) callconv(.c) Result;
pub const PFN_vkGetPhysicalDeviceProperties = *const fn (PhysicalDevice, *PhysicalDeviceProperties) callconv(.c) void;
pub const PFN_vkGetPhysicalDeviceMemoryProperties = *const fn (PhysicalDevice, *PhysicalDeviceMemoryProperties) callconv(.c) void;
pub const PFN_vkGetPhysicalDeviceFormatProperties = *const fn (PhysicalDevice, Format, *FormatProperties) callconv(.c) void;
pub const PFN_vkGetPhysicalDeviceQueueFamilyProperties = *const fn (PhysicalDevice, *u32, ?[*]QueueFamilyProperties) callconv(.c) void;
pub const PFN_vkEnumerateDeviceExtensionProperties = *const fn (PhysicalDevice, ?[*:0]const u8, *u32, ?[*]ExtensionProperties) callconv(.c) Result;
pub const PFN_vkCreateDevice = *const fn (PhysicalDevice, *const DeviceCreateInfo, ?*const AllocationCallbacks, *Device) callconv(.c) Result;

// Instance WSI & Debug commands.
pub const PFN_vkDestroySurfaceKHR = *const fn (Instance, SurfaceKHR, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkGetPhysicalDeviceSurfaceSupportKHR = *const fn (PhysicalDevice, u32, SurfaceKHR, *Bool32) callconv(.c) Result;
pub const PFN_vkGetPhysicalDeviceSurfaceCapabilitiesKHR = *const fn (PhysicalDevice, SurfaceKHR, *SurfaceCapabilitiesKHR) callconv(.c) Result;
pub const PFN_vkGetPhysicalDeviceSurfaceFormatsKHR = *const fn (PhysicalDevice, SurfaceKHR, *u32, ?[*]SurfaceFormatKHR) callconv(.c) Result;
pub const PFN_vkGetPhysicalDeviceSurfacePresentModesKHR = *const fn (PhysicalDevice, SurfaceKHR, *u32, ?[*]PresentModeKHR) callconv(.c) Result;
pub const PFN_vkCreateXlibSurfaceKHR = *const fn (Instance, *const XlibSurfaceCreateInfoKHR, ?*const AllocationCallbacks, *SurfaceKHR) callconv(.c) Result;
pub const PFN_vkCreateWaylandSurfaceKHR = *const fn (Instance, *const WaylandSurfaceCreateInfoKHR, ?*const AllocationCallbacks, *SurfaceKHR) callconv(.c) Result;
pub const PFN_vkCreateDebugUtilsMessengerEXT = *const fn (Instance, *const DebugUtilsMessengerCreateInfoEXT, ?*const AllocationCallbacks, *DebugUtilsMessengerEXT) callconv(.c) Result;
pub const PFN_vkDestroyDebugUtilsMessengerEXT = *const fn (Instance, DebugUtilsMessengerEXT, ?*const AllocationCallbacks) callconv(.c) void;

// Device commands.
pub const PFN_vkDestroyDevice = *const fn (Device, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkGetDeviceQueue = *const fn (Device, u32, u32, *Queue) callconv(.c) void;
pub const PFN_vkDeviceWaitIdle = *const fn (Device) callconv(.c) Result;
pub const PFN_vkQueueSubmit = *const fn (Queue, u32, ?[*]const SubmitInfo, Fence) callconv(.c) Result;
pub const PFN_vkQueueWaitIdle = *const fn (Queue) callconv(.c) Result;

pub const PFN_vkAllocateMemory = *const fn (Device, *const MemoryAllocateInfo, ?*const AllocationCallbacks, *DeviceMemory) callconv(.c) Result;
pub const PFN_vkFreeMemory = *const fn (Device, DeviceMemory, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkMapMemory = *const fn (Device, DeviceMemory, DeviceSize, DeviceSize, Flags, *?*anyopaque) callconv(.c) Result;
pub const PFN_vkUnmapMemory = *const fn (Device, DeviceMemory) callconv(.c) void;
pub const PFN_vkInvalidateMappedMemoryRanges = *const fn (Device, u32, [*]const MappedMemoryRange) callconv(.c) Result;
pub const PFN_vkFlushMappedMemoryRanges = *const fn (Device, u32, [*]const MappedMemoryRange) callconv(.c) Result;

pub const PFN_vkCreateBuffer = *const fn (Device, *const BufferCreateInfo, ?*const AllocationCallbacks, *Buffer) callconv(.c) Result;
pub const PFN_vkDestroyBuffer = *const fn (Device, Buffer, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkGetBufferMemoryRequirements = *const fn (Device, Buffer, *MemoryRequirements) callconv(.c) void;
pub const PFN_vkBindBufferMemory = *const fn (Device, Buffer, DeviceMemory, DeviceSize) callconv(.c) Result;

pub const PFN_vkCreateImage = *const fn (Device, *const ImageCreateInfo, ?*const AllocationCallbacks, *Image) callconv(.c) Result;
pub const PFN_vkDestroyImage = *const fn (Device, Image, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkGetImageMemoryRequirements = *const fn (Device, Image, *MemoryRequirements) callconv(.c) void;
pub const PFN_vkBindImageMemory = *const fn (Device, Image, DeviceMemory, DeviceSize) callconv(.c) Result;

pub const PFN_vkCreateImageView = *const fn (Device, *const ImageViewCreateInfo, ?*const AllocationCallbacks, *ImageView) callconv(.c) Result;
pub const PFN_vkDestroyImageView = *const fn (Device, ImageView, ?*const AllocationCallbacks) callconv(.c) void;

pub const PFN_vkCreateShaderModule = *const fn (Device, *const ShaderModuleCreateInfo, ?*const AllocationCallbacks, *ShaderModule) callconv(.c) Result;
pub const PFN_vkDestroyShaderModule = *const fn (Device, ShaderModule, ?*const AllocationCallbacks) callconv(.c) void;

pub const PFN_vkCreatePipelineLayout = *const fn (Device, *const PipelineLayoutCreateInfo, ?*const AllocationCallbacks, *PipelineLayout) callconv(.c) Result;
pub const PFN_vkDestroyPipelineLayout = *const fn (Device, PipelineLayout, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkCreatePipelineCache = *const fn (Device, *const PipelineCacheCreateInfo, ?*const AllocationCallbacks, *PipelineCache) callconv(.c) Result;
pub const PFN_vkDestroyPipelineCache = *const fn (Device, PipelineCache, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkCreateGraphicsPipelines = *const fn (Device, PipelineCache, u32, [*]const GraphicsPipelineCreateInfo, ?*const AllocationCallbacks, [*]Pipeline) callconv(.c) Result;
pub const PFN_vkDestroyPipeline = *const fn (Device, Pipeline, ?*const AllocationCallbacks) callconv(.c) void;

pub const PFN_vkCreateRenderPass = *const fn (Device, *const RenderPassCreateInfo, ?*const AllocationCallbacks, *RenderPass) callconv(.c) Result;
pub const PFN_vkDestroyRenderPass = *const fn (Device, RenderPass, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkCreateFramebuffer = *const fn (Device, *const FramebufferCreateInfo, ?*const AllocationCallbacks, *Framebuffer) callconv(.c) Result;
pub const PFN_vkDestroyFramebuffer = *const fn (Device, Framebuffer, ?*const AllocationCallbacks) callconv(.c) void;

pub const PFN_vkCreateDescriptorSetLayout = *const fn (Device, *const DescriptorSetLayoutCreateInfo, ?*const AllocationCallbacks, *DescriptorSetLayout) callconv(.c) Result;
pub const PFN_vkDestroyDescriptorSetLayout = *const fn (Device, DescriptorSetLayout, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkCreateDescriptorPool = *const fn (Device, *const DescriptorPoolCreateInfo, ?*const AllocationCallbacks, *DescriptorPool) callconv(.c) Result;
pub const PFN_vkDestroyDescriptorPool = *const fn (Device, DescriptorPool, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkResetDescriptorPool = *const fn (Device, DescriptorPool, Flags) callconv(.c) Result;
pub const PFN_vkAllocateDescriptorSets = *const fn (Device, *const DescriptorSetAllocateInfo, [*]DescriptorSet) callconv(.c) Result;
pub const PFN_vkUpdateDescriptorSets = *const fn (Device, u32, ?[*]const WriteDescriptorSet, u32, ?*const anyopaque) callconv(.c) void;

pub const PFN_vkCreateCommandPool = *const fn (Device, *const CommandPoolCreateInfo, ?*const AllocationCallbacks, *CommandPool) callconv(.c) Result;
pub const PFN_vkDestroyCommandPool = *const fn (Device, CommandPool, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkResetCommandPool = *const fn (Device, CommandPool, Flags) callconv(.c) Result;
pub const PFN_vkAllocateCommandBuffers = *const fn (Device, *const CommandBufferAllocateInfo, [*]CommandBuffer) callconv(.c) Result;
pub const PFN_vkFreeCommandBuffers = *const fn (Device, CommandPool, u32, [*]const CommandBuffer) callconv(.c) void;
pub const PFN_vkBeginCommandBuffer = *const fn (CommandBuffer, *const CommandBufferBeginInfo) callconv(.c) Result;
pub const PFN_vkEndCommandBuffer = *const fn (CommandBuffer) callconv(.c) Result;
pub const PFN_vkResetCommandBuffer = *const fn (CommandBuffer, Flags) callconv(.c) Result;

pub const PFN_vkCreateFence = *const fn (Device, *const FenceCreateInfo, ?*const AllocationCallbacks, *Fence) callconv(.c) Result;
pub const PFN_vkDestroyFence = *const fn (Device, Fence, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkResetFences = *const fn (Device, u32, [*]const Fence) callconv(.c) Result;
pub const PFN_vkWaitForFences = *const fn (Device, u32, [*]const Fence, Bool32, u64) callconv(.c) Result;

pub const PFN_vkCreateSemaphore = *const fn (Device, *const SemaphoreCreateInfo, ?*const AllocationCallbacks, *Semaphore) callconv(.c) Result;
pub const PFN_vkDestroySemaphore = *const fn (Device, Semaphore, ?*const AllocationCallbacks) callconv(.c) void;

pub const PFN_vkCmdBeginRenderPass = *const fn (CommandBuffer, *const RenderPassBeginInfo, SubpassContents) callconv(.c) void;
pub const PFN_vkCmdEndRenderPass = *const fn (CommandBuffer) callconv(.c) void;
pub const PFN_vkCmdBindPipeline = *const fn (CommandBuffer, PipelineBindPoint, Pipeline) callconv(.c) void;
pub const PFN_vkCmdSetViewport = *const fn (CommandBuffer, u32, u32, [*]const Viewport) callconv(.c) void;
pub const PFN_vkCmdSetScissor = *const fn (CommandBuffer, u32, u32, [*]const Rect2D) callconv(.c) void;
pub const PFN_vkCmdBindDescriptorSets = *const fn (CommandBuffer, PipelineBindPoint, PipelineLayout, u32, u32, [*]const DescriptorSet, u32, ?[*]const u32) callconv(.c) void;
pub const PFN_vkCmdBindVertexBuffers = *const fn (CommandBuffer, u32, u32, [*]const Buffer, [*]const DeviceSize) callconv(.c) void;
pub const PFN_vkCmdBindIndexBuffer = *const fn (CommandBuffer, Buffer, DeviceSize, IndexType) callconv(.c) void;
pub const PFN_vkCmdDrawIndexed = *const fn (CommandBuffer, u32, u32, u32, i32, u32) callconv(.c) void;
pub const PFN_vkCmdPipelineBarrier = *const fn (CommandBuffer, Flags, Flags, Flags, u32, ?*const anyopaque, u32, ?*const anyopaque, u32, ?[*]const ImageMemoryBarrier) callconv(.c) void;
pub const PFN_vkCmdCopyBufferToImage = *const fn (CommandBuffer, Buffer, Image, ImageLayout, u32, [*]const BufferImageCopy) callconv(.c) void;
pub const PFN_vkCmdCopyImageToBuffer = *const fn (CommandBuffer, Image, ImageLayout, Buffer, u32, [*]const BufferImageCopy) callconv(.c) void;
pub const PFN_vkCmdClearColorImage = *const fn (CommandBuffer, Image, ImageLayout, *const ClearColorValue, u32, [*]const ImageSubresourceRange) callconv(.c) void;

// Swapchain device commands.
pub const PFN_vkCreateSwapchainKHR = *const fn (Device, *const SwapchainCreateInfoKHR, ?*const AllocationCallbacks, *SwapchainKHR) callconv(.c) Result;
pub const PFN_vkDestroySwapchainKHR = *const fn (Device, SwapchainKHR, ?*const AllocationCallbacks) callconv(.c) void;
pub const PFN_vkGetSwapchainImagesKHR = *const fn (Device, SwapchainKHR, *u32, ?[*]Image) callconv(.c) Result;
pub const PFN_vkAcquireNextImageKHR = *const fn (Device, SwapchainKHR, u64, Semaphore, Fence, *u32) callconv(.c) Result;
pub const PFN_vkQueuePresentKHR = *const fn (Queue, *const PresentInfoKHR) callconv(.c) Result;

// Static ABI verification of critical Vulkan C struct sizes & offsets on 64-bit Linux.
fn assertSize(comptime T: type, comptime expected: usize) void {
    if (@sizeOf(T) != expected) {
        @compileError(std.fmt.comptimePrint(
            "Vulkan ABI size mismatch for {s}: got {d}, want {d}",
            .{ @typeName(T), @sizeOf(T), expected },
        ));
    }
}

fn assertOffset(comptime T: type, comptime field: []const u8, comptime expected: usize) void {
    const actual = @offsetOf(T, field);
    if (actual != expected) {
        @compileError(std.fmt.comptimePrint(
            "Vulkan ABI offset mismatch for {s}.{s}: got {d}, want {d}",
            .{ @typeName(T), field, actual, expected },
        ));
    }
}

comptime {
    // Handle sizes.
    assertSize(Instance, 8);
    assertSize(PhysicalDevice, 8);
    assertSize(Device, 8);
    assertSize(Queue, 8);
    assertSize(CommandBuffer, 8);
    assertSize(Buffer, 8);
    assertSize(Image, 8);
    assertSize(ImageView, 8);
    assertSize(DeviceMemory, 8);
    assertSize(SurfaceKHR, 8);
    assertSize(SwapchainKHR, 8);
    assertSize(DebugUtilsMessengerEXT, 8);

    // Core struct layouts.
    assertSize(PhysicalDeviceLimits, 504);
    assertOffset(PhysicalDeviceLimits, "maxImageDimension2D", 4);
    assertOffset(PhysicalDeviceLimits, "bufferImageGranularity", 48);
    assertOffset(PhysicalDeviceLimits, "minMemoryMapAlignment", 304);
    assertOffset(PhysicalDeviceLimits, "minUniformBufferOffsetAlignment", 320);
    assertOffset(PhysicalDeviceLimits, "optimalBufferCopyOffsetAlignment", 480);
    assertOffset(PhysicalDeviceLimits, "nonCoherentAtomSize", 496);

    assertSize(PhysicalDeviceSparseProperties, 20);
    assertSize(PhysicalDeviceProperties, 824);
    assertOffset(PhysicalDeviceProperties, "deviceType", 16);
    assertOffset(PhysicalDeviceProperties, "deviceName", 20);
    assertOffset(PhysicalDeviceProperties, "limits", 296);
    assertOffset(PhysicalDeviceProperties, "sparseProperties", 800);

    assertSize(PhysicalDeviceMemoryProperties, 520);
    assertOffset(PhysicalDeviceMemoryProperties, "memoryHeapCount", 260);
    assertOffset(PhysicalDeviceMemoryProperties, "memoryHeaps", 264);

    assertSize(MemoryRequirements, 24);
    assertSize(BufferCreateInfo, 56);
    assertSize(ImageCreateInfo, 88);
    assertSize(ImageViewCreateInfo, 80);
    assertSize(WriteDescriptorSet, 64);
    assertSize(DescriptorImageInfo, 24);
    assertSize(DescriptorBufferInfo, 24);
    assertSize(GraphicsPipelineCreateInfo, 144);
    assertSize(AttachmentDescription, 36);
    assertSize(SubpassDescription, 72);
    assertSize(SubpassDependency, 28);
    assertSize(FramebufferCreateInfo, 64);
    assertSize(RenderPassBeginInfo, 64);
    assertSize(ClearValue, 16);
    assertSize(SubmitInfo, 72);
    assertSize(ImageMemoryBarrier, 72);
    assertSize(BufferImageCopy, 56);

    // WSI & Debug Utils layouts.
    assertSize(SurfaceCapabilitiesKHR, 52);
    assertSize(SwapchainCreateInfoKHR, 104);
    assertOffset(SwapchainCreateInfoKHR, "surface", 24);
    assertOffset(SwapchainCreateInfoKHR, "oldSwapchain", 96);
    assertSize(PresentInfoKHR, 64);
    assertSize(XlibSurfaceCreateInfoKHR, 40);
    assertSize(WaylandSurfaceCreateInfoKHR, 40);
    assertSize(DebugUtilsMessengerCallbackDataEXT, 96);
    assertSize(DebugUtilsMessengerCreateInfoEXT, 48);
}
