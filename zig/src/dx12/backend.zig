const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const handles = @import("../handles.zig");
const state_cache = @import("../state_cache.zig");
const sync = @import("../sync.zig");
const replayer = @import("../replayer.zig");
const upload_arena = @import("../upload_arena.zig");

const win32 = @import("win32.zig");
const com = @import("com.zig");
const dxgi = @import("dxgi.zig");
const d3d12 = @import("d3d12.zig");
const dcomp = @import("dcomp.zig");
const d3dcompiler = @import("d3dcompiler.zig");
const descriptors = @import("descriptors.zig");
const upload = @import("upload.zig");
const swapchain_mod = @import("swapchain.zig");
const pipeline = @import("pipeline.zig");

const Allocator = std.mem.Allocator;
const Error = errors.Error;
const Diag = errors.Diag;
const max_frames = descriptors.max_frames_in_flight;

const Image = struct {
    width: u32,
    height: u32,
    internal_width: u32,
    internal_height: u32,
    format: abi.PixelFormat,
    is_screen: bool,
    surface_id: u32,
    resource: ?*d3d12.ID3D12Resource,
    state: d3d12.D3D12_RESOURCE_STATES,
    cpu_srv: ?descriptors.CpuDescriptorSlot,
    cpu_rtv: ?descriptors.CpuDescriptorSlot,

    fn dxgiFormat(self: *const Image) dxgi.DXGI_FORMAT {
        if (self.is_screen) return dxgi.DXGI_FORMAT_B8G8R8A8_UNORM;
        return switch (self.format) {
            .rgba8 => dxgi.DXGI_FORMAT_R8G8B8A8_UNORM,
            .a8 => dxgi.DXGI_FORMAT_R8_UNORM,
        };
    }
};

const DeferredImage = struct {
    resource: ?*d3d12.ID3D12Resource,
    cpu_srv: ?descriptors.CpuDescriptorSlot,
    cpu_rtv: ?descriptors.CpuDescriptorSlot,
};

const Surface = struct {
    hwnd: win32.HWND,
    swapchain: ?swapchain_mod.SwapChain = null,
    screen_image_id: u32 = 0,
    drawn: bool = false,

    fn deinit(self: *Surface) void {
        if (self.swapchain) |*sc| {
            sc.deinit();
            self.swapchain = null;
        }
    }
};

fn dredBreadcrumbOpName(op: i32) []const u8 {
    return switch (op) {
        3 => "DrawInstanced",
        4 => "DrawIndexedInstanced",
        6 => "Dispatch",
        7 => "CopyBufferRegion",
        8 => "CopyTextureRegion",
        9 => "CopyResource",
        12 => "ClearRenderTargetView",
        14 => "ClearDepthStencilView",
        15 => "ResourceBarrier",
        17 => "Present",
        else => "OtherOp",
    };
}

/// Windows Direct3D 12 backend (single unified DIRECT command list, persistently mapped upload rings,
/// root CBVs b0/b1 + 4-SRV descriptor table ring, and HWND/DirectComposition swapchains).
pub const Backend = struct {
    gpa: Allocator,

    // Runtime-loaded system libraries
    user32_lib: win32.User32Lib,
    dcomp_lib: dcomp.DCompLib,
    compiler_lib: d3dcompiler.CompilerLib,
    dxgi_lib: dxgi.DxgiLib,
    d3d12_lib: d3d12.D3D12Lib,

    // Core DXGI / D3D12 COM objects
    debug: ?*d3d12.ID3D12Debug = null,
    dred_enabled: bool = false,
    allow_tearing: bool = false,
    factory4: *dxgi.IDXGIFactory4,
    device: *d3d12.ID3D12Device,
    cmd_queue: *d3d12.ID3D12CommandQueue,

    frames_in_flight: u32,
    frame_index: usize = 0,
    cmd_allocators: [max_frames]?*d3d12.ID3D12CommandAllocator = @splat(null),
    allocator_needs_reset: [max_frames]bool = @splat(false),
    cmd_list: *d3d12.ID3D12GraphicsCommandList,
    cmd_list_open: bool = false,
    cmd_list_has_work: bool = false,

    fences: [max_frames]?*d3d12.ID3D12Fence = @splat(null),
    fence_values: [max_frames]u64 = @splat(0),
    fence_event: win32.HANDLE,

    // Subsystems
    descriptors: descriptors.DescriptorManager,
    pipelines: pipeline.PipelineCache,
    upload_rings: [max_frames]upload.UploadRing = @splat(.{}),
    dummy_cbv_buf: *d3d12.ID3D12Resource,
    dummy_cbv_gpu: d3d12.D3D12_GPU_VIRTUAL_ADDRESS,

    // Active GPU-mapped shared upload arena (zero-copy writePixels)
    shared_upload: ?upload.MappedUploadBuffer = null,
    shared_pool: upload.SharedArenaPool = .{},
    shared_arena_id: u32 = 0,

    // Resource handle tables
    images: handles.HandleTable(Image) = .{},
    shaders: handles.HandleTable(pipeline.Shader) = .{},
    surfaces: handles.HandleTable(Surface) = .{},
    surfaces_lock: sync.Mutex = .{},

    // Deferred disposal queues (reclaimed once corresponding frame fence completes)
    deferred_images: [max_frames]std.ArrayList(DeferredImage) = @splat(.empty),
    deferred_shaders: [max_frames]std.ArrayList(pipeline.Shader) = @splat(.empty),

    // Active vertex/index buffer views for current batch
    active_vbv: d3d12.D3D12_VERTEX_BUFFER_VIEW = .{},
    active_ibv: d3d12.D3D12_INDEX_BUFFER_VIEW = .{},
    active_buffers_valid: bool = false,

    // Per-command-list binding state cache (reset on every cmd_list.Reset)
    cache: state_cache.BindCache = .{},
    invariants_set: bool = false,
    bound_rtv: usize = 0,
    bound_vbv: d3d12.D3D12_VERTEX_BUFFER_VIEW = .{},
    bound_ibv: d3d12.D3D12_INDEX_BUFFER_VIEW = .{},
    bound_internal_gpu: d3d12.D3D12_GPU_VIRTUAL_ADDRESS = 0,
    bound_user_gpu: d3d12.D3D12_GPU_VIRTUAL_ADDRESS = 0,
    bound_srv_table_gpu: u64 = 0,

    // Cross-thread atomics
    occluded: std.atomic.Value(bool) = std.atomic.Value(bool).init(false),

    // Debug stats
    live_pipelines: u64 = 0,
    submits: u64 = 0,
    draws: u64 = 0,
    binds_skipped: u64 = 0,
    uploaded_bytes: u64 = 0,
    completed_frames: u64 = 0,

    pub fn init(gpa: Allocator, opts: *const abi.DeviceOptions, caps: *abi.DriverCaps, diag: *Diag) Error!*Backend {
        const frames_in_flight: u32 = if (opts.frames_in_flight == 3) 3 else 2;
        const use_debug = (opts.flags & abi.device_debug) != 0;
        const use_warp = (opts.flags & abi.device_warp) != 0;
        const use_dred = (opts.flags & abi.device_dred) != 0;
        const low_power = (opts.flags & abi.device_low_power) != 0;
        const allow_dxil = (opts.flags & abi.device_allow_dxil) != 0;
        const feature_level: d3d12.D3D_FEATURE_LEVEL = if (opts.dx_feature_level != 0)
            @intCast(opts.dx_feature_level)
        else
            d3d12.D3D_FEATURE_LEVEL_11_0;

        // 1. Load required & optional DLLs at runtime.
        var compiler_lib = try d3dcompiler.CompilerLib.load(diag);
        errdefer compiler_lib.deinit();

        var d3d12_lib = try d3d12.D3D12Lib.load(diag);
        errdefer d3d12_lib.deinit();

        var dxgi_lib = try dxgi.DxgiLib.load(diag);
        errdefer dxgi_lib.deinit();

        var dcomp_lib = dcomp.DCompLib.loadOptional();
        errdefer dcomp_lib.deinit();

        var user32_lib = win32.User32Lib.load();
        errdefer user32_lib.deinit();

        // 2. Optional D3D12 Debug Layer and DRED (must be enabled prior to device creation).
        var debug_iface: ?*d3d12.ID3D12Debug = null;
        errdefer com.safeRelease(&debug_iface);

        if (use_debug) {
            if (d3d12_lib.get_debug_interface) |get_dbg| {
                var raw: ?*anyopaque = null;
                if (win32.succeeded(get_dbg(&d3d12.IID_ID3D12Debug, &raw)) and raw != null) {
                    const dbg: *d3d12.ID3D12Debug = @ptrCast(@alignCast(raw.?));
                    dbg.enableDebugLayer();
                    debug_iface = dbg;
                }
            }
        }

        var dred_enabled = false;
        if (use_dred) {
            if (d3d12_lib.get_debug_interface) |get_dbg| {
                var raw: ?*anyopaque = null;
                if (win32.succeeded(get_dbg(&d3d12.IID_ID3D12DeviceRemovedExtendedDataSettings, &raw)) and raw != null) {
                    const settings: *d3d12.ID3D12DeviceRemovedExtendedDataSettings = @ptrCast(@alignCast(raw.?));
                    settings.setAutoBreadcrumbsEnablement(d3d12.D3D12_DRED_ENABLEMENT_FORCED_ON);
                    settings.setPageFaultEnablement(d3d12.D3D12_DRED_ENABLEMENT_FORCED_ON);
                    _ = settings.release();
                    dred_enabled = true;
                }
            }
        }

        // 3. Create DXGI Factory4 (try debug flag first when requested, fall back cleanly).
        var factory_raw: ?*anyopaque = null;
        var factory_hr = dxgi_lib.create_factory2(
            if (use_debug) dxgi.DXGI_CREATE_FACTORY_DEBUG else 0,
            &dxgi.IID_IDXGIFactory4,
            &factory_raw,
        );
        if (use_debug and (win32.failed(factory_hr) or factory_raw == null)) {
            factory_raw = null;
            factory_hr = dxgi_lib.create_factory2(0, &dxgi.IID_IDXGIFactory4, &factory_raw);
        }
        if (win32.failed(factory_hr) or factory_raw == null) {
            diag.print("directx: CreateDXGIFactory2 failed (0x{x:0>8})", .{win32.hrHex(factory_hr)});
            return error.BackendUnavailable;
        }
        const factory4: *dxgi.IDXGIFactory4 = @ptrCast(@alignCast(factory_raw.?));
        errdefer _ = factory4.release();

        // Query ALLOW_TEARING via IDXGIFactory5.
        var allow_tearing = false;
        if (com.queryInterface(dxgi.IDXGIFactory5, factory4, &dxgi.IID_IDXGIFactory5)) |f5| {
            defer _ = f5.release();
            var tearing_val: win32.BOOL = 0;
            if (win32.succeeded(f5.checkFeatureSupport(
                dxgi.DXGI_FEATURE_PRESENT_ALLOW_TEARING,
                @ptrCast(&tearing_val),
                @sizeOf(win32.BOOL),
            )) and tearing_val != 0) {
                allow_tearing = true;
            }
        }

        // 4. Select adapter (WARP -> EnumWarpAdapter, else IDXGIFactory6.EnumAdapterByGpuPreference -> fallback EnumAdapters1).
        const adapter = try selectAdapter(factory4, &d3d12_lib, use_warp, low_power, feature_level, diag);
        defer _ = adapter.release();

        var adapter_desc: dxgi.DXGI_ADAPTER_DESC1 = std.mem.zeroes(dxgi.DXGI_ADAPTER_DESC1);
        const has_desc = win32.succeeded(adapter.getDesc1(&adapter_desc));

        // 5. Create ID3D12Device.
        var dev_raw: ?*anyopaque = null;
        const dev_hr = d3d12_lib.create_device(@ptrCast(adapter), feature_level, &d3d12.IID_ID3D12Device, &dev_raw);
        if (win32.failed(dev_hr) or dev_raw == null) {
            diag.print("directx: D3D12CreateDevice failed (0x{x:0>8})", .{win32.hrHex(dev_hr)});
            return error.BackendUnavailable;
        }
        const device: *d3d12.ID3D12Device = @ptrCast(@alignCast(dev_raw.?));
        errdefer _ = device.release();

        // 6. Create single DIRECT command queue, per-frame command allocators, single DIRECT command list, fences & event.
        const queue_desc = d3d12.D3D12_COMMAND_QUEUE_DESC{
            .Type = d3d12.D3D12_COMMAND_LIST_TYPE_DIRECT,
            .Priority = 0,
            .Flags = d3d12.D3D12_COMMAND_QUEUE_FLAG_NONE,
            .NodeMask = 0,
        };
        var cmd_queue_opt: ?*d3d12.ID3D12CommandQueue = null;
        const q_hr = device.createCommandQueue(&queue_desc, &cmd_queue_opt);
        if (win32.failed(q_hr) or cmd_queue_opt == null) {
            diag.print("directx: CreateCommandQueue failed (0x{x:0>8})", .{win32.hrHex(q_hr)});
            return win32.hrToError(q_hr);
        }
        const cmd_queue = cmd_queue_opt.?;
        errdefer _ = cmd_queue.release();

        var cmd_allocators: [max_frames]?*d3d12.ID3D12CommandAllocator = @splat(null);
        errdefer for (&cmd_allocators) |*a| com.safeRelease(a);

        var fences: [max_frames]?*d3d12.ID3D12Fence = @splat(null);
        errdefer for (&fences) |*f| com.safeRelease(f);

        var fi: u32 = 0;
        while (fi < frames_in_flight) : (fi += 1) {
            const ca_hr = device.createCommandAllocator(d3d12.D3D12_COMMAND_LIST_TYPE_DIRECT, &cmd_allocators[fi]);
            if (win32.failed(ca_hr) or cmd_allocators[fi] == null) {
                diag.print("directx: CreateCommandAllocator failed (0x{x:0>8})", .{win32.hrHex(ca_hr)});
                return win32.hrToError(ca_hr);
            }
            const fn_hr = device.createFence(0, d3d12.D3D12_FENCE_FLAG_NONE, &fences[fi]);
            if (win32.failed(fn_hr) or fences[fi] == null) {
                diag.print("directx: CreateFence failed (0x{x:0>8})", .{win32.hrHex(fn_hr)});
                return win32.hrToError(fn_hr);
            }
        }

        const fence_event = win32.createEvent() orelse {
            diag.set("directx: CreateEventExW failed");
            return error.Internal;
        };
        errdefer win32.closeHandle(fence_event);

        var cmd_list_opt: ?*d3d12.ID3D12GraphicsCommandList = null;
        const cl_hr = device.createCommandList(
            0,
            d3d12.D3D12_COMMAND_LIST_TYPE_DIRECT,
            cmd_allocators[0].?,
            null,
            &cmd_list_opt,
        );
        if (win32.failed(cl_hr) or cmd_list_opt == null) {
            diag.print("directx: CreateCommandList failed (0x{x:0>8})", .{win32.hrHex(cl_hr)});
            return win32.hrToError(cl_hr);
        }
        const cmd_list = cmd_list_opt.?;
        errdefer _ = cmd_list.release();

        // Close immediately; opened lazily on first GPU command.
        const close_hr = cmd_list.close();
        if (win32.failed(close_hr)) {
            diag.print("directx: initial CommandList::Close failed (0x{x:0>8})", .{win32.hrHex(close_hr)});
            return win32.hrToError(close_hr);
        }

        // 7. Descriptor manager, root signature / pipeline cache, and 256B zeroed dummy CBV.
        var desc_mgr = try descriptors.DescriptorManager.init(gpa, device, frames_in_flight, diag);
        errdefer desc_mgr.deinit(gpa);

        var pipe_cache = try pipeline.PipelineCache.init(device, &d3d12_lib, allow_dxil, diag);
        errdefer pipe_cache.deinit(gpa);

        const dummy_cbv_buf = try upload.createBuffer(device, 256, d3d12.D3D12_HEAP_TYPE_UPLOAD, diag);
        errdefer _ = dummy_cbv_buf.release();
        {
            var mapped: ?*anyopaque = null;
            const r = d3d12.D3D12_RANGE{ .Begin = 0, .End = 0 };
            if (win32.succeeded(dummy_cbv_buf.map(0, &r, &mapped)) and mapped != null) {
                const bytes: [*]u8 = @ptrCast(mapped.?);
                @memset(bytes[0..256], 0);
                dummy_cbv_buf.unmap(0, null);
            }
        }
        const dummy_cbv_gpu = dummy_cbv_buf.getGpuVirtualAddress();

        // 8. Probe DirectComposition support.
        const supports_dcomp = swapchain_mod.probeDirectComposition(
            factory4,
            cmd_queue,
            &dcomp_lib,
            allow_tearing,
            frames_in_flight,
        );

        // 9. Populate Backend instance and caps.
        const self = gpa.create(Backend) catch return error.OutOfMemory;
        self.* = .{
            .gpa = gpa,
            .user32_lib = user32_lib,
            .dcomp_lib = dcomp_lib,
            .compiler_lib = compiler_lib,
            .dxgi_lib = dxgi_lib,
            .d3d12_lib = d3d12_lib,
            .debug = debug_iface,
            .dred_enabled = dred_enabled,
            .allow_tearing = allow_tearing,
            .factory4 = factory4,
            .device = device,
            .cmd_queue = cmd_queue,
            .frames_in_flight = frames_in_flight,
            .cmd_allocators = cmd_allocators,
            .cmd_list = cmd_list,
            .fences = fences,
            .fence_event = fence_event,
            .descriptors = desc_mgr,
            .pipelines = pipe_cache,
            .dummy_cbv_buf = dummy_cbv_buf,
            .dummy_cbv_gpu = dummy_cbv_gpu,
        };

        caps.* = std.mem.zeroes(abi.DriverCaps);
        caps.struct_size = @sizeOf(abi.DriverCaps);
        caps.backend = @intFromEnum(abi.Backend.dx12);
        caps.max_image_size = d3d12.D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION;
        caps.flags = abi.cap_flips_y |
            abi.cap_needs_clearing_screen |
            abi.cap_shared_upload_arena |
            abi.cap_supports_a8;
        if (supports_dcomp) {
            caps.flags |= abi.cap_direct_composition;
        }
        caps.shader_format = @intFromEnum(abi.ShaderFormat.hlsl);
        caps.shader_version = 50;
        caps.color_space = if (opts.color_space != 0) opts.color_space else 1;
        caps.frames_in_flight = frames_in_flight;

        if (has_desc) {
            win32.utf16LeToUtf8Z(&caps.device_name, &adapter_desc.Description);
        }
        if (caps.device_name[0] == 0) {
            const fallback = "Direct3D 12";
            @memcpy(caps.device_name[0..fallback.len], fallback);
            caps.device_name[fallback.len] = 0;
        }

        return self;
    }

    pub fn deinit(self: *Backend) void {
        const gpa = self.gpa;
        var dummy_diag = Diag.none();

        // Wait for GPU to finish all pending work across all frames before releasing anything.
        if (self.cmd_list_open) {
            _ = self.cmd_list.close();
            self.cmd_list_open = false;
            if (self.cmd_list_has_work) {
                self.cmd_queue.executeCommandLists(&.{self.cmd_list});
                self.cmd_list_has_work = false;
            }
        }
        self.waitForQueueIdle(&dummy_diag) catch {};

        // Drain all deferred disposal queues across every frame slot.
        for (0..max_frames) |f| {
            self.releaseDeferredForFrame(f);
            self.deferred_images[f].deinit(gpa);
            self.deferred_shaders[f].deinit(gpa);
        }

        // Release remaining live images.
        for (self.images.items.items) |*slot| {
            if (slot.*) |img| {
                if (!img.is_screen) {
                    if (img.cpu_srv) |s| self.descriptors.freeImageSrv(gpa, s);
                    if (img.cpu_rtv) |r| self.descriptors.freeImageRtv(gpa, r);
                    if (img.resource) |res| _ = res.release();
                }
                slot.* = null;
            }
        }
        self.images.deinit(gpa);

        // Release remaining live shaders & PSOs.
        for (self.shaders.items.items) |*slot| {
            if (slot.*) |*sh| {
                sh.deinit(gpa, &self.live_pipelines);
                slot.* = null;
            }
        }
        self.shaders.deinit(gpa);

        // Release remaining live surfaces & swapchains.
        self.surfaces_lock.lock();
        for (self.surfaces.items.items) |*slot| {
            if (slot.*) |*surf| {
                surf.deinit();
                slot.* = null;
            }
        }
        self.surfaces.deinit(gpa);
        self.surfaces_lock.unlock();
        self.surfaces_lock.deinit();

        // Release shared upload arena, upload rings, dummy CBV, pipelines, descriptors.
        if (self.shared_upload) |arena| {
            upload.destroyMappedUploadBuffer(arena);
            self.shared_upload = null;
        }
        for (&self.upload_rings) |*ring| {
            ring.deinit(gpa, &self.shared_pool);
        }
        self.shared_pool.deinit();
        _ = self.dummy_cbv_buf.release();
        self.pipelines.deinit(gpa);
        self.descriptors.deinit(gpa);

        // Release core D3D12/DXGI COM objects in reverse creation order.
        _ = self.cmd_list.release();
        for (&self.fences) |*f| com.safeRelease(f);
        for (&self.cmd_allocators) |*a| com.safeRelease(a);
        win32.closeHandle(self.fence_event);
        _ = self.cmd_queue.release();
        _ = self.device.release();
        _ = self.factory4.release();
        com.safeRelease(&self.debug);

        // Close loaded DLLs.
        self.user32_lib.deinit();
        self.dcomp_lib.deinit();
        self.dxgi_lib.deinit();
        self.d3d12_lib.deinit();
        self.compiler_lib.deinit();

        gpa.destroy(self);
    }

    pub fn beginBatch(self: *Backend, diag: *Diag) Error!void {
        _ = self;
        _ = diag;
        // Command list is opened lazily on first GPU command so empty batches emit 0 D3D12 calls.
    }

    pub fn abortBatch(self: *Backend) void {
        if (self.cmd_list_open) {
            _ = self.cmd_list.close();
            self.cmd_list_open = false;
            self.cmd_list_has_work = false;
            var dummy = Diag.none();
            if (self.waitForQueueIdle(&dummy)) |_| {
                if (self.cmd_allocators[self.frame_index]) |alloc| {
                    _ = alloc.reset();
                    self.allocator_needs_reset[self.frame_index] = false;
                }
            } else |_| {}
        }
        self.resetCmdListState();
    }

    pub fn endBatch(self: *Backend, mode: abi.FlushMode, res: *abi.SubmitResult, diag: *Diag) Error!void {
        self.submits += 1;
        res.* = std.mem.zeroes(abi.SubmitResult);

        switch (mode) {
            .intermediate => {
                if (self.cmd_list_open) {
                    try self.closeAndExecuteCmdList(diag);
                }
                // Guard against unbounded retired upload buffer growth during long intermediate-only sequences.
                if (self.upload_rings[self.frame_index].retiredCount() >= 16) {
                    try self.waitForQueueIdle(diag);
                    self.releaseDeferredForFrame(self.frame_index);
                    // Only safe to clear retired buffers (the active buffer may still back active_vbv/ibv).
                    self.upload_rings[self.frame_index].releaseRetired(&self.shared_pool);
                }
            },
            .end_frame => {
                if (self.cmd_list_open) {
                    try self.closeAndExecuteCmdList(diag);
                }
                try self.waitForQueueIdle(diag);
                self.reclaimFrame(self.frame_index);
                self.completed_frames += 1;
            },
            .present => {
                // 1. Transition all drawn swapchain back buffers to PRESENT before executing.
                var barriers: [16]d3d12.D3D12_RESOURCE_BARRIER = undefined;
                var barrier_count: usize = 0;

                self.surfaces_lock.lock();
                for (self.surfaces.items.items) |*slot| {
                    if (slot.*) |*surf| {
                        if (!surf.drawn) continue;
                        if (surf.swapchain) |*sc| {
                            const cur_state = sc.currentState();
                            if (cur_state != d3d12.D3D12_RESOURCE_STATE_PRESENT) {
                                if (barrier_count == barriers.len) {
                                    self.surfaces_lock.unlock();
                                    try self.ensureCmdListOpen(diag);
                                    self.cmd_list.resourceBarrier(barriers[0..barrier_count]);
                                    self.cmd_list_has_work = true;
                                    barrier_count = 0;
                                    self.surfaces_lock.lock();
                                }
                                barriers[barrier_count] = d3d12.D3D12_RESOURCE_BARRIER.transition(
                                    sc.currentResource(),
                                    cur_state,
                                    d3d12.D3D12_RESOURCE_STATE_PRESENT,
                                );
                                sc.setCurrentState(d3d12.D3D12_RESOURCE_STATE_PRESENT);
                                barrier_count += 1;
                            }
                        }
                    }
                }
                self.surfaces_lock.unlock();

                if (barrier_count > 0) {
                    try self.ensureCmdListOpen(diag);
                    self.cmd_list.resourceBarrier(barriers[0..barrier_count]);
                    self.cmd_list_has_work = true;
                }

                // 2. Close and execute command list.
                if (self.cmd_list_open) {
                    try self.closeAndExecuteCmdList(diag);
                }

                // 3. Present drawn swapchains (first visible waits for vsync; occluded ones run DXGI_PRESENT_TEST).
                var presented: u32 = 0;
                var all_occluded = true;
                var waited = false;

                self.surfaces_lock.lock();
                var present_err: ?Error = null;
                for (self.surfaces.items.items) |*slot| {
                    if (slot.*) |*surf| {
                        if (!surf.drawn) continue;
                        surf.drawn = false;
                        if (surf.swapchain) |*sc| {
                            const was_occluded = sc.occluded;
                            const visible = sc.present(!waited, diag) catch |err| {
                                present_err = err;
                                break;
                            };
                            if (visible) {
                                all_occluded = false;
                            }
                            if (!was_occluded) {
                                waited = true;
                            }
                            presented += 1;
                        }
                    }
                }
                self.surfaces_lock.unlock();

                if (present_err) |err| {
                    return self.checkDeviceRemovedOr(err, "Present", diag);
                }

                if (presented > 0) {
                    self.occluded.store(all_occluded, .release);
                } else {
                    self.occluded.store(false, .release);
                }
                res.presented_surfaces = presented;

                // 4. Signal current frame fence, advance frame_index, wait for next frame's fence.
                const cur = self.frame_index;
                self.fence_values[cur] += 1;
                const sig_val = self.fence_values[cur];
                const sig_hr = self.cmd_queue.signal(self.fences[cur].?, sig_val);
                if (win32.failed(sig_hr)) {
                    return self.diagnoseError(sig_hr, "CommandQueue::Signal", diag);
                }

                const next = (cur + 1) % @as(usize, self.frames_in_flight);
                self.frame_index = next;
                const next_fence = self.fences[next].?;
                const target_val = self.fence_values[next];
                try self.waitForFence(next_fence, target_val, "frame fence", diag);

                self.reclaimFrame(next);
                self.completed_frames += 1;
            },
        }

        res.completed_frames = self.completed_frames;
        if (self.occluded.load(.acquire)) {
            res.flags |= abi.result_occluded;
        }
    }

    pub fn setVertices(self: *Backend, vertices: []const f32, indices: []const u32, diag: *Diag) Error!void {
        if (vertices.len == 0 or indices.len == 0) {
            return;
        }
        const v_bytes = std.mem.sliceAsBytes(vertices);
        const i_bytes = std.mem.sliceAsBytes(indices);

        const ring = &self.upload_rings[self.frame_index];
        const v_alloc = try ring.alloc(self.gpa, self.device, v_bytes.len, upload.vb_ib_alignment, diag);
        @memcpy(v_alloc.cpu_ptr[0..v_bytes.len], v_bytes);

        const i_alloc = try ring.alloc(self.gpa, self.device, i_bytes.len, upload.vb_ib_alignment, diag);
        @memcpy(i_alloc.cpu_ptr[0..i_bytes.len], i_bytes);

        self.active_vbv = .{
            .BufferLocation = v_alloc.gpu_addr,
            .SizeInBytes = @intCast(v_bytes.len),
            .StrideInBytes = pipeline.vertex_stride_bytes,
        };
        self.active_ibv = .{
            .BufferLocation = i_alloc.gpu_addr,
            .SizeInBytes = @intCast(i_bytes.len),
            .Format = dxgi.DXGI_FORMAT_R32_UINT,
        };
        self.active_buffers_valid = true;
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
        if (w == 0 or h == 0 or w > d3d12.D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION or h > d3d12.D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION) {
            diag.print("directx: invalid image dimensions {d}x{d}", .{ w, h });
            return error.InvalidArgument;
        }
        const int_w = if (iw > 0) iw else w;
        const int_h = if (ih > 0) ih else h;
        if (int_w > d3d12.D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION or int_h > d3d12.D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION) {
            diag.print("directx: invalid internal image dimensions {d}x{d}", .{ int_w, int_h });
            return error.InvalidArgument;
        }
        const dxgi_fmt: dxgi.DXGI_FORMAT = switch (fmt) {
            .rgba8 => dxgi.DXGI_FORMAT_R8G8B8A8_UNORM,
            .a8 => dxgi.DXGI_FORMAT_R8_UNORM,
        };

        const heap_props = d3d12.D3D12_HEAP_PROPERTIES{
            .Type = d3d12.D3D12_HEAP_TYPE_DEFAULT,
            .CPUPageProperty = d3d12.D3D12_CPU_PAGE_PROPERTY_UNKNOWN,
            .MemoryPoolPreference = d3d12.D3D12_MEMORY_POOL_UNKNOWN,
            .CreationNodeMask = 1,
            .VisibleNodeMask = 1,
        };
        const res_desc = d3d12.D3D12_RESOURCE_DESC{
            .Dimension = d3d12.D3D12_RESOURCE_DIMENSION_TEXTURE2D,
            .Alignment = 0,
            .Width = int_w,
            .Height = int_h,
            .DepthOrArraySize = 1,
            .MipLevels = 1,
            .Format = dxgi_fmt,
            .SampleDesc = .{ .Count = 1, .Quality = 0 },
            .Layout = d3d12.D3D12_TEXTURE_LAYOUT_UNKNOWN,
            .Flags = d3d12.D3D12_RESOURCE_FLAG_ALLOW_RENDER_TARGET,
        };

        const initial_state = d3d12.D3D12_RESOURCE_STATE_PIXEL_SHADER_RESOURCE;
        var tex_opt: ?*d3d12.ID3D12Resource = null;
        const hr = self.device.createCommittedResource(
            &heap_props,
            d3d12.D3D12_HEAP_FLAG_NONE,
            &res_desc,
            initial_state,
            null,
            &tex_opt,
        );
        if (win32.failed(hr) or tex_opt == null) {
            return self.diagnoseError(hr, "CreateCommittedResource(texture2D)", diag);
        }
        const tex = tex_opt.?;
        errdefer _ = tex.release();

        const cpu_srv = try self.descriptors.createImageSrv(self.gpa, self.device, tex, fmt, diag);
        errdefer self.descriptors.freeImageSrv(self.gpa, cpu_srv);

        const cpu_rtv = try self.descriptors.createImageRtv(self.gpa, self.device, tex, diag);
        errdefer self.descriptors.freeImageRtv(self.gpa, cpu_rtv);

        try self.images.put(self.gpa, id, .{
            .width = w,
            .height = h,
            .internal_width = int_w,
            .internal_height = int_h,
            .format = fmt,
            .is_screen = false,
            .surface_id = 0,
            .resource = tex,
            .state = initial_state,
            .cpu_srv = cpu_srv,
            .cpu_rtv = cpu_rtv,
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
        if (w == 0 or h == 0 or w > d3d12.D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION or h > d3d12.D3D12_REQ_TEXTURE2D_U_OR_V_DIMENSION) {
            diag.print("directx: invalid screen image dimensions {d}x{d}", .{ w, h });
            return error.InvalidArgument;
        }

        // Inspect surface state under surfaces_lock.
        self.surfaces_lock.lock();
        const surf_check = self.surfaces.getOpt(surface_id);
        if (surf_check == null) {
            self.surfaces_lock.unlock();
            diag.print("directx: unknown surface_id {d}", .{surface_id});
            return error.UnknownHandle;
        }
        const hwnd = surf_check.?.hwnd;
        const has_sc = surf_check.?.swapchain != null;
        const can_reuse = if (surf_check.?.swapchain) |*sc| sc.canReuseBuffers(w, h) else false;
        self.surfaces_lock.unlock();

        if (!has_sc) {
            const sc = try swapchain_mod.SwapChain.create(
                self.device,
                self.factory4,
                self.cmd_queue,
                &self.dcomp_lib,
                &self.user32_lib,
                hwnd,
                w,
                h,
                self.frames_in_flight,
                self.allow_tearing,
                self.descriptors.rtv_size,
                diag,
            );
            self.surfaces_lock.lock();
            if (self.surfaces.getOpt(surface_id)) |surf| {
                surf.swapchain = sc;
                surf.screen_image_id = id;
                surf.drawn = false;
            } else {
                self.surfaces_lock.unlock();
                var tmp = sc;
                tmp.deinit();
                return error.UnknownHandle;
            }
            self.surfaces_lock.unlock();
        } else if (!can_reuse) {
            // Resize requires GPU idle and all references to back buffers released.
            if (self.cmd_list_open) {
                try self.closeAndExecuteCmdList(diag);
            }
            try self.waitForQueueIdle(diag);
            self.releaseDeferredForFrame(self.frame_index);
            self.descriptors.resetFrame(self.frame_index);
            self.allocator_needs_reset[self.frame_index] = true;
            self.bound_rtv = 0;

            self.surfaces_lock.lock();
            const resize_res: Error!void = if (self.surfaces.getOpt(surface_id)) |surf| blk: {
                if (surf.swapchain) |*sc| {
                    break :blk sc.resize(self.device, w, h, diag);
                }
                break :blk error.Internal;
            } else error.UnknownHandle;
            if (resize_res) |_| {
                if (self.surfaces.getOpt(surface_id)) |surf| {
                    surf.screen_image_id = id;
                    surf.drawn = false;
                }
            } else |err| {
                self.surfaces_lock.unlock();
                return err;
            }
            self.surfaces_lock.unlock();
        } else {
            self.surfaces_lock.lock();
            if (self.surfaces.getOpt(surface_id)) |surf| {
                surf.screen_image_id = id;
            }
            self.surfaces_lock.unlock();
        }

        try self.images.put(self.gpa, id, .{
            .width = w,
            .height = h,
            .internal_width = w,
            .internal_height = h,
            .format = .rgba8,
            .is_screen = true,
            .surface_id = surface_id,
            .resource = null,
            .state = d3d12.D3D12_RESOURCE_STATE_PRESENT,
            .cpu_srv = null,
            .cpu_rtv = null,
        });
    }

    pub fn disposeImage(self: *Backend, id: u32) Error!void {
        const img = try self.images.remove(id);
        if (img.is_screen) {
            self.surfaces_lock.lock();
            if (self.surfaces.getOpt(img.surface_id)) |surf| {
                if (surf.screen_image_id == id) {
                    surf.screen_image_id = 0;
                    surf.drawn = false;
                }
            }
            self.surfaces_lock.unlock();
            self.bound_rtv = 0;
            return;
        }

        if (img.cpu_rtv) |rtv| {
            if (self.bound_rtv == rtv.handle.ptr) {
                self.bound_rtv = 0;
            }
        }
        if (img.cpu_srv) |srv| {
            self.descriptors.invalidateSrcHandle(srv.handle.ptr);
            self.bound_srv_table_gpu = 0;
        }

        self.deferred_images[self.frame_index].append(self.gpa, .{
            .resource = img.resource,
            .cpu_srv = img.cpu_srv,
            .cpu_rtv = img.cpu_rtv,
        }) catch {
            // Fallback: wait for GPU idle and release immediately if OOM on deferred list.
            self.drainBestEffort();
            if (img.cpu_srv) |s| self.descriptors.freeImageSrv(self.gpa, s);
            if (img.cpu_rtv) |r| self.descriptors.freeImageRtv(self.gpa, r);
            if (img.resource) |res| _ = res.release();
        };
    }

    pub fn disposeShader(self: *Backend, id: u32) Error!void {
        var sh = try self.shaders.remove(id);
        self.cache.pipeline = 0;
        self.deferred_shaders[self.frame_index].append(self.gpa, sh) catch {
            self.drainBestEffort();
            sh.deinit(self.gpa, &self.live_pipelines);
        };
    }

    pub fn disposeSurface(self: *Backend, id: u32) Error!void {
        self.drainBestEffort();

        self.surfaces_lock.lock();
        var surf = self.surfaces.remove(id) catch |err| {
            self.surfaces_lock.unlock();
            return err;
        };
        self.surfaces_lock.unlock();

        surf.deinit();
        self.bound_rtv = 0;
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
            diag.set("directx: writePixels cannot be called on a screen image");
            return error.InvalidArgument;
        }
        const tex = img.resource orelse return error.UnknownHandle;
        const bpp = img.format.bytesPerPixel();

        // Validate every region upfront before recording any GPU commands.
        var has_non_empty = false;
        for (regions) |r| {
            const bounds = r.rect.validate(img.internal_width, img.internal_height) orelse {
                diag.print("directx: writePixels rect ({d},{d})-({d},{d}) out of bounds {d}x{d}", .{
                    r.rect.x0, r.rect.y0, r.rect.x1, r.rect.y1, img.internal_width, img.internal_height,
                });
                return error.InvalidArgument;
            };
            const expected_len = @as(u64, bounds.dx) * @as(u64, bounds.dy) * bpp;
            if (r.payload_len != expected_len) {
                diag.print("directx: writePixels payload_len {d} != expected {d}", .{ r.payload_len, expected_len });
                return error.InvalidArgument;
            }
            if (bounds.dx > 0 and bounds.dy > 0) {
                has_non_empty = true;
            }
        }
        if (!has_non_empty) return;

        try self.ensureCmdListOpen(diag);

        // Transition destination texture to COPY_DEST.
        if (img.state != d3d12.D3D12_RESOURCE_STATE_COPY_DEST) {
            const rb = [1]d3d12.D3D12_RESOURCE_BARRIER{
                d3d12.D3D12_RESOURCE_BARRIER.transition(tex, img.state, d3d12.D3D12_RESOURCE_STATE_COPY_DEST),
            };
            self.cmd_list.resourceBarrier(&rb);
            img.state = d3d12.D3D12_RESOURCE_STATE_COPY_DEST;
        }

        const dxgi_fmt = img.dxgiFormat();
        const ring = &self.upload_rings[self.frame_index];

        // Check if payload resides inside the active GPU-mapped shared upload arena.
        var shared_res: ?*d3d12.ID3D12Resource = null;
        var shared_base_off: u64 = 0;
        var shared_size: u64 = 0;
        if (self.shared_upload) |arena| {
            if (payload.len > 0) {
                if (upload_arena.offsetWithin(arena.cpu_ptr, @intCast(arena.size), payload)) |off| {
                    shared_res = arena.resource;
                    shared_base_off = off;
                    shared_size = arena.size;
                }
            }
        }

        for (regions) |r| {
            const bounds = r.rect.validate(img.internal_width, img.internal_height).?;
            if (bounds.dx == 0 or bounds.dy == 0) continue;

            const dst_loc = d3d12.D3D12_TEXTURE_COPY_LOCATION.subresource(tex, 0);
            const src_box = d3d12.D3D12_BOX{
                .left = 0,
                .top = 0,
                .front = 0,
                .right = bounds.dx,
                .bottom = bounds.dy,
                .back = 1,
            };

            const bpr: u64 = @as(u64, bounds.dx) * @as(u64, bpp);
            if (shared_res) |sbuf| {
                const placed_off = shared_base_off + @as(u64, r.payload_offset);
                if ((bpr % upload.texture_pitch_alignment) == 0 and
                    (placed_off % upload.texture_placement_alignment) == 0 and
                    placed_off + r.payload_len <= shared_size)
                {
                    // Zero-copy directly from the persistently-mapped shared upload arena.
                    const src_loc = d3d12.D3D12_TEXTURE_COPY_LOCATION.placedFootprint(sbuf, .{
                        .Offset = placed_off,
                        .Footprint = .{
                            .Format = dxgi_fmt,
                            .Width = bounds.dx,
                            .Height = bounds.dy,
                            .Depth = 1,
                            .RowPitch = @intCast(bpr),
                        },
                    });
                    self.cmd_list.copyTextureRegion(&dst_loc, bounds.x0, bounds.y0, 0, &src_loc, &src_box);
                    self.uploaded_bytes += r.payload_len;
                    continue;
                }
            }

            const fp = upload.computeTextureFootprint(bounds.dx, bounds.dy, bpp);
            const staging = try ring.alloc(
                self.gpa,
                self.device,
                fp.total_bytes,
                upload.texture_placement_alignment,
                diag,
            );

            const src_pixels = payload[r.payload_offset .. r.payload_offset + r.payload_len];
            upload.packRowsToFootprint(staging.cpu_ptr, fp.row_pitch, src_pixels, bounds.dx, bounds.dy, bpp);

            const src_loc = d3d12.D3D12_TEXTURE_COPY_LOCATION.placedFootprint(staging.resource, .{
                .Offset = staging.offset,
                .Footprint = .{
                    .Format = dxgi_fmt,
                    .Width = bounds.dx,
                    .Height = bounds.dy,
                    .Depth = 1,
                    .RowPitch = fp.row_pitch,
                },
            });
            self.cmd_list.copyTextureRegion(&dst_loc, bounds.x0, bounds.y0, 0, &src_loc, &src_box);
            self.uploaded_bytes += r.payload_len;
        }

        self.cmd_list_has_work = true;
    }

    pub fn drawTriangles(self: *Backend, dc: *const replayer.DrawCall, diag: *Diag) Error!void {
        const dst = self.images.get(dc.dst) catch {
            diag.print("directx: drawTriangles unknown dst image {d}", .{dc.dst});
            return error.UnknownHandle;
        };
        const shader = self.shaders.get(dc.shader) catch {
            diag.print("directx: drawTriangles unknown shader {d}", .{dc.shader});
            return error.UnknownHandle;
        };

        var src_imgs: [abi.max_src_images]?*Image = @splat(null);
        for (dc.srcs, 0..) |src_id, i| {
            if (src_id == 0) continue;
            if (src_id == dc.dst) {
                diag.set("directx: destination and source images must be different");
                return error.InvalidArgument;
            }
            const src_img = self.images.get(src_id) catch {
                diag.print("directx: drawTriangles unknown src image {d}", .{src_id});
                return error.UnknownHandle;
            };
            if (src_img.is_screen) {
                diag.set("directx: screen image cannot be sampled as a source texture");
                return error.InvalidArgument;
            }
            src_imgs[i] = src_img;
        }

        if (dc.regions.len == 0) return;

        if (!self.active_buffers_valid) {
            diag.set("directx: drawTriangles called before setVertices");
            return error.InvalidArgument;
        }

        const user_bytes: u32 = @intCast(dc.user.len * 4);
        const user_cb_aligned: u32 = @max(std.mem.alignForward(u32, shader.user_cb_size, @intCast(upload.cbv_alignment)), @as(u32, @intCast(upload.cbv_alignment)));
        if (user_bytes > 0 and user_bytes > user_cb_aligned) {
            diag.print("directx: user uniform block ({d} B) exceeds shader size ({d} B)", .{ user_bytes, user_cb_aligned });
            return error.InvalidArgument;
        }

        // If the shader-visible SRV descriptor ring for this frame is exhausted, flush + wait GPU
        // and reset the SRV ring (preserving upload_rings so active_vbv/active_ibv stay valid!).
        if (!self.descriptors.hasRoom(self.frame_index)) {
            if (self.cmd_list_open) {
                try self.closeAndExecuteCmdList(diag);
            }
            try self.waitForQueueIdle(diag);
            self.descriptors.resetFrame(self.frame_index);
            self.allocator_needs_reset[self.frame_index] = true;
        }

        try self.ensureCmdListOpen(diag);

        // Resolve destination resource, state, and RTV handle.
        var dst_res: *d3d12.ID3D12Resource = undefined;
        var dst_rtv: d3d12.D3D12_CPU_DESCRIPTOR_HANDLE = undefined;
        var barriers: [1 + abi.max_src_images]d3d12.D3D12_RESOURCE_BARRIER = undefined;
        var barrier_count: usize = 0;

        if (dst.is_screen) {
            self.surfaces_lock.lock();
            const surf_opt = self.surfaces.getOpt(dst.surface_id);
            if (surf_opt == null or surf_opt.?.swapchain == null) {
                self.surfaces_lock.unlock();
                diag.print("directx: screen image surface {d} has no swapchain", .{dst.surface_id});
                return error.SurfaceLost;
            }
            const surf = surf_opt.?;
            const sc = &surf.swapchain.?;
            surf.drawn = true;
            dst_res = sc.currentResource();
            dst_rtv = sc.currentRtvHandle();
            const cur_state = sc.currentState();
            if (cur_state != d3d12.D3D12_RESOURCE_STATE_RENDER_TARGET) {
                barriers[barrier_count] = d3d12.D3D12_RESOURCE_BARRIER.transition(
                    dst_res,
                    cur_state,
                    d3d12.D3D12_RESOURCE_STATE_RENDER_TARGET,
                );
                sc.setCurrentState(d3d12.D3D12_RESOURCE_STATE_RENDER_TARGET);
                barrier_count += 1;
            }
            self.surfaces_lock.unlock();
        } else {
            dst_res = dst.resource orelse return error.UnknownHandle;
            dst_rtv = (dst.cpu_rtv orelse return error.Internal).handle;
            if (dst.state != d3d12.D3D12_RESOURCE_STATE_RENDER_TARGET) {
                barriers[barrier_count] = d3d12.D3D12_RESOURCE_BARRIER.transition(
                    dst_res,
                    dst.state,
                    d3d12.D3D12_RESOURCE_STATE_RENDER_TARGET,
                );
                dst.state = d3d12.D3D12_RESOURCE_STATE_RENDER_TARGET;
                barrier_count += 1;
            }
        }

        // Transition source images to PIXEL_SHADER_RESOURCE and collect CPU SRV handles.
        var src_cpu_handles: [abi.max_src_images]d3d12.D3D12_CPU_DESCRIPTOR_HANDLE = undefined;
        for (src_imgs, 0..) |s_opt, i| {
            if (s_opt) |src| {
                const s_res = src.resource orelse return error.UnknownHandle;
                if (src.state != d3d12.D3D12_RESOURCE_STATE_PIXEL_SHADER_RESOURCE) {
                    barriers[barrier_count] = d3d12.D3D12_RESOURCE_BARRIER.transition(
                        s_res,
                        src.state,
                        d3d12.D3D12_RESOURCE_STATE_PIXEL_SHADER_RESOURCE,
                    );
                    src.state = d3d12.D3D12_RESOURCE_STATE_PIXEL_SHADER_RESOURCE;
                    barrier_count += 1;
                }
                src_cpu_handles[i] = (src.cpu_srv orelse return error.Internal).handle;
            } else {
                src_cpu_handles[i] = self.descriptors.null_srv_slot.handle;
            }
        }

        if (barrier_count > 0) {
            self.cmd_list.resourceBarrier(barriers[0..barrier_count]);
        }

        // Bind invariant graphics pipeline state once per command list reset.
        if (!self.invariants_set) {
            self.cmd_list.setGraphicsRootSignature(self.pipelines.root_signature);
            const heaps = [1]*d3d12.ID3D12DescriptorHeap{self.descriptors.shader_visible_heap};
            self.cmd_list.setDescriptorHeaps(&heaps);
            self.cmd_list.iaSetPrimitiveTopology(d3d12.D3D_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
            self.invariants_set = true;
        }

        // Bind RTV.
        if (self.bound_rtv != dst_rtv.ptr) {
            const rtvs = [1]d3d12.D3D12_CPU_DESCRIPTOR_HANDLE{dst_rtv};
            self.cmd_list.omSetRenderTargets(&rtvs);
            self.bound_rtv = dst_rtv.ptr;
        } else {
            self.binds_skipped += 1;
        }

        // Viewport.
        const vw = dst.internal_width;
        const vh = dst.internal_height;
        if (self.cache.viewport[0] != vw or self.cache.viewport[1] != vh) {
            const vp = [1]d3d12.D3D12_VIEWPORT{.{
                .TopLeftX = 0.0,
                .TopLeftY = 0.0,
                .Width = @floatFromInt(vw),
                .Height = @floatFromInt(vh),
                .MinDepth = d3d12.D3D12_MIN_DEPTH,
                .MaxDepth = d3d12.D3D12_MAX_DEPTH,
            }};
            self.cmd_list.rsSetViewports(&vp);
            self.cache.viewport = .{ vw, vh };
        } else {
            self.binds_skipped += 1;
        }

        // Vertex & Index buffers.
        if (!self.bound_vbv.eql(self.active_vbv)) {
            const vbvs = [1]d3d12.D3D12_VERTEX_BUFFER_VIEW{self.active_vbv};
            self.cmd_list.iaSetVertexBuffers(0, &vbvs);
            self.bound_vbv = self.active_vbv;
        } else {
            self.binds_skipped += 1;
        }
        if (!self.bound_ibv.eql(self.active_ibv)) {
            self.cmd_list.iaSetIndexBuffer(&self.active_ibv);
            self.bound_ibv = self.active_ibv;
        } else {
            self.binds_skipped += 1;
        }

        // PSO (keyed by shader, blend, rtv_format).
        const pso = try self.pipelines.getOrCreatePso(
            self.gpa,
            self.device,
            shader,
            dc.blend,
            dst.dxgiFormat(),
            &self.live_pipelines,
            diag,
        );
        const pso_addr = @intFromPtr(pso);
        if (self.cache.pipeline != pso_addr) {
            self.cmd_list.setPipelineState(pso);
            self.cache.pipeline = pso_addr;
        } else {
            self.binds_skipped += 1;
        }

        // Root Parameter [0]: Root CBV b0 (internal uniforms, 512 B = align256(288 B)).
        const ring = &self.upload_rings[self.frame_index];
        const internal_cb_size: usize = comptime std.mem.alignForward(usize, abi.internal_uniform_bytes, upload.cbv_alignment);
        const internal_gpu: d3d12.D3D12_GPU_VIRTUAL_ADDRESS = if (dc.internal_version != 0 and ring.last_internal_version == dc.internal_version)
            ring.last_internal_gpu
        else blk: {
            const alloc = try ring.alloc(self.gpa, self.device, internal_cb_size, upload.cbv_alignment, diag);
            const src_bytes = std.mem.asBytes(dc.internal);
            @memcpy(alloc.cpu_ptr[0..src_bytes.len], src_bytes);
            ring.last_internal_version = dc.internal_version;
            ring.last_internal_gpu = alloc.gpu_addr;
            self.uploaded_bytes += src_bytes.len;
            break :blk alloc.gpu_addr;
        };

        if (self.bound_internal_gpu != internal_gpu) {
            self.cmd_list.setGraphicsRootConstantBufferView(0, internal_gpu);
            self.bound_internal_gpu = internal_gpu;
        } else {
            self.binds_skipped += 1;
        }

        // Root Parameter [1]: Root CBV b1 (user uniforms, max(align256(user_bytes), 256)).
        const user_gpu: d3d12.D3D12_GPU_VIRTUAL_ADDRESS = if (dc.user.len == 0)
            self.dummy_cbv_gpu
        else if (dc.user_version != 0 and ring.last_user_version == dc.user_version and ring.last_user_size == user_cb_aligned)
            ring.last_user_gpu
        else blk: {
            const alloc = try ring.alloc(self.gpa, self.device, user_cb_aligned, upload.cbv_alignment, diag);
            const src_bytes = std.mem.sliceAsBytes(dc.user);
            @memcpy(alloc.cpu_ptr[0..src_bytes.len], src_bytes);
            ring.last_user_version = dc.user_version;
            ring.last_user_gpu = alloc.gpu_addr;
            ring.last_user_size = user_cb_aligned;
            self.uploaded_bytes += src_bytes.len;
            break :blk alloc.gpu_addr;
        };

        if (self.bound_user_gpu != user_gpu) {
            self.cmd_list.setGraphicsRootConstantBufferView(1, user_gpu);
            self.bound_user_gpu = user_gpu;
        } else {
            self.binds_skipped += 1;
        }

        // Root Parameter [2]: Descriptor Table SRV t0..t3 (PIXEL).
        const srv_res = self.descriptors.getOrAllocSrvTable(self.device, self.frame_index, src_cpu_handles);
        if (self.bound_srv_table_gpu != srv_res.gpu.ptr) {
            self.cmd_list.setGraphicsRootDescriptorTable(2, srv_res.gpu);
            self.bound_srv_table_gpu = srv_res.gpu.ptr;
        } else {
            self.binds_skipped += 1;
        }

        // Issue indexed draw calls per scissor region.
        var idx_off = dc.index_offset;
        for (dc.regions) |r| {
            if (!self.cache.scissor_valid or !self.cache.scissor.eql(r.rect)) {
                const sc = [1]win32.RECT{.{
                    .left = r.rect.x0,
                    .top = r.rect.y0,
                    .right = r.rect.x1,
                    .bottom = r.rect.y1,
                }};
                self.cmd_list.rsSetScissorRects(&sc);
                self.cache.scissor = r.rect;
                self.cache.scissor_valid = true;
            } else {
                self.binds_skipped += 1;
            }
            self.cmd_list.drawIndexedInstanced(r.index_count, 1, idx_off, 0, 0);
            idx_off += r.index_count;
            self.draws += 1;
        }

        self.cmd_list_has_work = true;
    }

    pub fn setDamage(self: *Backend, surface_id: u32, rects: ?[]align(4) const abi.Rect) Error!void {
        _ = rects;
        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        _ = try self.surfaces.get(surface_id);
    }

    pub fn createShader(self: *Backend, id: u32, desc: *const abi.ShaderDesc, diag: *Diag) Error!void {
        var shader = try self.pipelines.compileShader(self.gpa, &self.compiler_lib, desc, diag);
        errdefer shader.deinit(self.gpa, &self.live_pipelines);
        try self.shaders.put(self.gpa, id, shader);
    }

    pub fn readPixels(self: *Backend, id: u32, regions: []const abi.ReadRegion, diag: *Diag) Error!void {
        const img = try self.images.get(id);
        if (img.is_screen) {
            diag.set("directx: readPixels cannot be called on a screen image");
            return error.InvalidArgument;
        }
        const tex = img.resource orelse return error.UnknownHandle;
        const bpp = img.format.bytesPerPixel();

        var union_x0: u32 = std.math.maxInt(u32);
        var union_y0: u32 = std.math.maxInt(u32);
        var union_x1: u32 = 0;
        var union_y1: u32 = 0;

        for (regions) |r| {
            const bounds = r.rect.validate(img.internal_width, img.internal_height) orelse {
                diag.print("directx: readPixels rect ({d},{d})-({d},{d}) out of bounds {d}x{d}", .{
                    r.rect.x0, r.rect.y0, r.rect.x1, r.rect.y1, img.internal_width, img.internal_height,
                });
                return error.InvalidArgument;
            };
            const expected_len = @as(u64, bounds.dx) * @as(u64, bounds.dy) * bpp;
            if (r.dst_len != expected_len) {
                diag.print("directx: readPixels dst_len {d} != expected {d}", .{ r.dst_len, expected_len });
                return error.InvalidArgument;
            }
            if (expected_len == 0) continue;
            if (r.dst == null) {
                diag.set("directx: readPixels dst pointer is null");
                return error.InvalidArgument;
            }
            union_x0 = @min(union_x0, bounds.x0);
            union_y0 = @min(union_y0, bounds.y0);
            union_x1 = @max(union_x1, bounds.x0 + bounds.dx);
            union_y1 = @max(union_y1, bounds.y0 + bounds.dy);
        }

        if (union_x0 >= union_x1 or union_y0 >= union_y1) return;

        const union_w = union_x1 - union_x0;
        const union_h = union_y1 - union_y0;
        const fp_desc = d3d12.D3D12_RESOURCE_DESC{
            .Dimension = d3d12.D3D12_RESOURCE_DIMENSION_TEXTURE2D,
            .Alignment = 0,
            .Width = union_w,
            .Height = union_h,
            .DepthOrArraySize = 1,
            .MipLevels = 1,
            .Format = img.dxgiFormat(),
            .SampleDesc = .{ .Count = 1, .Quality = 0 },
            .Layout = d3d12.D3D12_TEXTURE_LAYOUT_UNKNOWN,
            .Flags = d3d12.D3D12_RESOURCE_FLAG_ALLOW_RENDER_TARGET,
        };

        var layout: d3d12.D3D12_PLACED_SUBRESOURCE_FOOTPRINT = .{};
        var num_rows: u32 = 0;
        var row_size: u64 = 0;
        var total_bytes: u64 = 0;
        self.device.getCopyableFootprints(&fp_desc, 0, 1, 0, &layout, &num_rows, &row_size, &total_bytes);
        if (total_bytes == 0 or layout.Footprint.RowPitch == 0) {
            diag.set("directx: GetCopyableFootprints returned invalid layout");
            return error.Internal;
        }

        const readback = try upload.createBuffer(self.device, total_bytes, d3d12.D3D12_HEAP_TYPE_READBACK, diag);
        defer _ = readback.release();

        try self.ensureCmdListOpen(diag);

        if (img.state != d3d12.D3D12_RESOURCE_STATE_COPY_SOURCE) {
            const rb = [1]d3d12.D3D12_RESOURCE_BARRIER{
                d3d12.D3D12_RESOURCE_BARRIER.transition(tex, img.state, d3d12.D3D12_RESOURCE_STATE_COPY_SOURCE),
            };
            self.cmd_list.resourceBarrier(&rb);
            img.state = d3d12.D3D12_RESOURCE_STATE_COPY_SOURCE;
        }

        const dst_loc = d3d12.D3D12_TEXTURE_COPY_LOCATION.placedFootprint(readback, layout);
        const src_loc = d3d12.D3D12_TEXTURE_COPY_LOCATION.subresource(tex, 0);
        const src_box = d3d12.D3D12_BOX{
            .left = union_x0,
            .top = union_y0,
            .front = 0,
            .right = union_x1,
            .bottom = union_y1,
            .back = 1,
        };
        self.cmd_list.copyTextureRegion(&dst_loc, 0, 0, 0, &src_loc, &src_box);
        self.cmd_list_has_work = true;

        // Submit copy and wait for GPU completion before mapping readback buffer.
        try self.closeAndExecuteCmdList(diag);
        try self.waitForQueueIdle(diag);

        var mapped_raw: ?*anyopaque = null;
        const read_range = d3d12.D3D12_RANGE{ .Begin = 0, .End = @intCast(total_bytes) };
        const map_hr = readback.map(0, &read_range, &mapped_raw);
        if (win32.failed(map_hr) or mapped_raw == null) {
            return self.diagnoseError(map_hr, "ID3D12Resource::Map(readback)", diag);
        }
        const write_none = d3d12.D3D12_RANGE{ .Begin = 0, .End = 0 };
        defer readback.unmap(0, &write_none);

        const src_ptr: [*]const u8 = @ptrCast(mapped_raw.?);
        const row_pitch: usize = layout.Footprint.RowPitch;
        for (regions) |r| {
            const bounds = r.rect.validate(img.internal_width, img.internal_height).?;
            if (bounds.dx == 0 or bounds.dy == 0) continue;
            upload.unpackRowsFromFootprint(
                r.dst.?,
                src_ptr,
                union_x0,
                union_y0,
                bounds.x0,
                bounds.y0,
                bounds.dx,
                bounds.dy,
                row_pitch,
                bpp,
            );
        }
    }

    pub fn finishForcedFrame(self: *Backend, diag: *Diag) Error!void {
        if (self.cmd_list_open) {
            try self.closeAndExecuteCmdList(diag);
        }
        try self.waitForQueueIdle(diag);
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
        if ((desc.flags & abi.surface_transparent) != 0) {
            diag.set("directx: a transparent surface is not supported");
            return error.Unsupported;
        }
        const kind: abi.SurfaceKind = @enumFromInt(desc.kind);
        if (kind != .hwnd) {
            diag.print("directx: NewSurface needs SurfaceKindHWND, got {d}", .{desc.kind});
            return error.InvalidArgument;
        }
        if (desc.handle1 == 0) {
            diag.set("directx: HWND handle is null");
            return error.InvalidArgument;
        }
        const hwnd: win32.HWND = @ptrFromInt(@as(usize, @intCast(desc.handle1)));

        self.surfaces_lock.lock();
        defer self.surfaces_lock.unlock();
        try self.surfaces.put(self.gpa, id, .{
            .hwnd = hwnd,
        });

        caps.* = .{
            .struct_size = @sizeOf(abi.SurfaceCaps),
            .flags = abi.surface_cap_flips_y,
            .native_format = @intFromEnum(abi.NativeFormat.bgra8),
            .reserved = 0,
        };
    }

    pub fn setWindowState(self: *Backend, id: u32, flags: u32) void {
        _ = self;
        _ = id;
        _ = flags;
    }

    pub fn surfaceKept(self: *Backend, id: u32) bool {
        _ = self;
        _ = id;
        return false;
    }

    pub fn setMainThreadMode(self: *Backend, mode: u32) void {
        _ = self;
        _ = mode;
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
            diag.print("directx: requested shared upload arena size ({d} B) exceeds maximum ({d} B)", .{
                min_bytes,
                upload_arena.max_size,
            });
            return error.OutOfMemory;
        }

        const alignment: u32 = @intCast(upload.texture_placement_alignment); // 512
        const target_size: u64 = std.mem.alignForward(u64, @max(min_bytes, upload_arena.default_size), alignment);

        const mapped = self.shared_pool.take(target_size) orelse
            try upload.createMappedUploadBuffer(self.device, target_size, diag);
        errdefer upload.destroyMappedUploadBuffer(mapped);

        // The old arena may still be read by commands of this frame: it returns
        // to the pool only when the frame is reclaimed.
        if (self.shared_upload) |old| {
            self.upload_rings[self.frame_index].retired_shared.append(self.gpa, old) catch return error.OutOfMemory;
        }

        self.shared_upload = mapped;

        out_arena.* = .{
            .struct_size = @sizeOf(abi.UploadArena),
            .arena_id = upload_arena.nextId(&self.shared_arena_id),
            .ptr = mapped.cpu_ptr,
            .capacity = mapped.size,
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
            .live_pipelines = self.live_pipelines,
            .submits = self.submits,
            .draws = self.draws,
            .binds_skipped = self.binds_skipped,
            .uploaded_bytes = self.uploaded_bytes,
        };
    }

    // --- Internal helpers ---

    fn resetCmdListState(self: *Backend) void {
        self.cache.reset();
        self.invariants_set = false;
        self.bound_rtv = 0;
        self.bound_vbv = .{};
        self.bound_ibv = .{};
        self.bound_internal_gpu = 0;
        self.bound_user_gpu = 0;
        self.bound_srv_table_gpu = 0;
    }

    fn ensureCmdListOpen(self: *Backend, diag: *Diag) Error!void {
        if (self.cmd_list_open) return;

        const alloc = self.cmd_allocators[self.frame_index].?;
        if (self.allocator_needs_reset[self.frame_index]) {
            const a_hr = alloc.reset();
            if (win32.failed(a_hr)) {
                return self.diagnoseError(a_hr, "CommandAllocator::Reset", diag);
            }
            self.allocator_needs_reset[self.frame_index] = false;
        }

        const cl_hr = self.cmd_list.reset(alloc, null);
        if (win32.failed(cl_hr)) {
            return self.diagnoseError(cl_hr, "CommandList::Reset", diag);
        }
        self.cmd_list_open = true;
        self.cmd_list_has_work = false;
        self.resetCmdListState();
    }

    fn closeAndExecuteCmdList(self: *Backend, diag: *Diag) Error!void {
        if (!self.cmd_list_open) return;
        self.cmd_list_open = false;
        const hr = self.cmd_list.close();
        if (win32.failed(hr)) {
            self.cmd_list_has_work = false;
            return self.diagnoseError(hr, "CommandList::Close", diag);
        }
        if (self.cmd_list_has_work) {
            self.cmd_queue.executeCommandLists(&.{self.cmd_list});
            self.cmd_list_has_work = false;
        }
    }

    fn waitForQueueIdle(self: *Backend, diag: *Diag) Error!void {
        const f = self.frame_index;
        self.fence_values[f] += 1;
        const target = self.fence_values[f];
        const fence = self.fences[f].?;

        const sig_hr = self.cmd_queue.signal(fence, target);
        if (win32.failed(sig_hr)) {
            return self.diagnoseError(sig_hr, "CommandQueue::Signal", diag);
        }
        try self.waitForFence(fence, target, "queue idle", diag);
    }

    /// Blocks until `fence` has reached `target`. `what` names the wait in diagnostics.
    fn waitForFence(
        self: *Backend,
        fence: *d3d12.ID3D12Fence,
        target: u64,
        comptime what: []const u8,
        diag: *Diag,
    ) Error!void {
        const completed = fence.getCompletedValue();
        if (completed == std.math.maxInt(u64)) {
            return self.checkDeviceRemovedOr(error.DeviceLost, "Fence::GetCompletedValue", diag);
        }
        if (completed >= target) return;

        const ev_hr = fence.setEventOnCompletion(target, self.fence_event);
        if (win32.failed(ev_hr)) {
            return self.diagnoseError(ev_hr, "Fence::SetEventOnCompletion", diag);
        }
        if (!win32.waitForSingleObject(self.fence_event, win32.INFINITE)) {
            diag.set("directx: WaitForSingleObject(" ++ what ++ ") failed");
            return error.Internal;
        }
    }

    /// Submits any open command list and waits for the GPU to go idle, ignoring
    /// failures. For paths that must release resources immediately and cannot
    /// report an error.
    fn drainBestEffort(self: *Backend) void {
        var dummy = Diag.none();
        if (self.cmd_list_open) {
            self.closeAndExecuteCmdList(&dummy) catch {};
        }
        self.waitForQueueIdle(&dummy) catch {};
    }

    fn reclaimFrame(self: *Backend, frame_idx: usize) void {
        self.releaseDeferredForFrame(frame_idx);
        self.upload_rings[frame_idx].reset(&self.shared_pool);
        self.descriptors.resetFrame(frame_idx);
        self.allocator_needs_reset[frame_idx] = true;
        self.active_buffers_valid = false;
    }

    fn releaseDeferredForFrame(self: *Backend, frame_idx: usize) void {
        for (self.deferred_images[frame_idx].items) |item| {
            if (item.cpu_srv) |s| self.descriptors.freeImageSrv(self.gpa, s);
            if (item.cpu_rtv) |r| self.descriptors.freeImageRtv(self.gpa, r);
            if (item.resource) |res| _ = res.release();
        }
        self.deferred_images[frame_idx].clearRetainingCapacity();

        for (self.deferred_shaders[frame_idx].items) |*sh| {
            sh.deinit(self.gpa, &self.live_pipelines);
        }
        self.deferred_shaders[frame_idx].clearRetainingCapacity();
    }

    fn checkDeviceRemovedOr(self: *Backend, err: Error, op_name: []const u8, diag: *Diag) Error {
        const reason = self.device.getDeviceRemovedReason();
        if (win32.failed(reason)) {
            return self.formatDeviceRemoved(reason, op_name, diag);
        }
        return err;
    }

    fn diagnoseError(self: *Backend, hr: win32.HRESULT, op_name: []const u8, diag: *Diag) Error {
        const reason = self.device.getDeviceRemovedReason();
        if (win32.failed(reason)) {
            return self.formatDeviceRemoved(reason, op_name, diag);
        }
        diag.print("directx: {s} failed (0x{x:0>8})", .{ op_name, win32.hrHex(hr) });
        return win32.hrToError(hr);
    }

    fn formatDeviceRemoved(self: *Backend, reason: win32.HRESULT, op_name: []const u8, diag: *Diag) Error {
        if (self.dred_enabled) {
            if (com.queryInterface(d3d12.ID3D12DeviceRemovedExtendedData, self.device, &d3d12.IID_ID3D12DeviceRemovedExtendedData)) |dred| {
                defer _ = dred.release();
                var pf = d3d12.D3D12_DRED_PAGE_FAULT_OUTPUT{};
                const has_pf = win32.succeeded(dred.getPageFaultAllocationOutput(&pf));
                var bc = d3d12.D3D12_DRED_AUTO_BREADCRUMBS_OUTPUT{};
                var last_op: []const u8 = "none";
                if (win32.succeeded(dred.getAutoBreadcrumbsOutput(&bc))) {
                    if (bc.pHeadAutoBreadcrumbNode) |node| {
                        const completed = if (node.pLastBreadcrumbValue) |p| p.* else 0;
                        if (node.pCommandHistory) |hist| {
                            if (completed < node.BreadcrumbCount) {
                                last_op = dredBreadcrumbOpName(hist[completed]);
                            }
                        }
                    }
                }
                diag.print("directx: device lost during {s} (reason=0x{x:0>8}, dred_va=0x{x}, faulting_op={s})", .{
                    op_name,
                    win32.hrHex(reason),
                    if (has_pf) pf.PageFaultVA else @as(u64, 0),
                    last_op,
                });
                return error.DeviceLost;
            }
        }
        diag.print("directx: device lost during {s} (reason=0x{x:0>8})", .{ op_name, win32.hrHex(reason) });
        return error.DeviceLost;
    }
};

fn selectAdapter(
    factory4: *dxgi.IDXGIFactory4,
    d3d12_lib: *const d3d12.D3D12Lib,
    use_warp: bool,
    low_power: bool,
    feature_level: d3d12.D3D_FEATURE_LEVEL,
    diag: *Diag,
) Error!*dxgi.IDXGIAdapter1 {
    if (use_warp) {
        var warp_raw: ?*anyopaque = null;
        const hr = factory4.enumWarpAdapter(&dxgi.IID_IDXGIAdapter1, &warp_raw);
        if (win32.failed(hr) or warp_raw == null) {
            diag.print("directx: EnumWarpAdapter failed (0x{x:0>8})", .{win32.hrHex(hr)});
            return error.BackendUnavailable;
        }
        return @ptrCast(@alignCast(warp_raw.?));
    }

    // Prefer IDXGIFactory6::EnumAdapterByGpuPreference when available.
    if (com.queryInterface(dxgi.IDXGIFactory6, factory4, &dxgi.IID_IDXGIFactory6)) |f6| {
        defer _ = f6.release();
        const pref: dxgi.DXGI_GPU_PREFERENCE = if (low_power)
            dxgi.DXGI_GPU_PREFERENCE_MINIMUM_POWER
        else
            dxgi.DXGI_GPU_PREFERENCE_HIGH_PERFORMANCE;
        var idx: u32 = 0;
        while (true) : (idx += 1) {
            var raw: ?*anyopaque = null;
            const hr = f6.enumAdapterByGpuPreference(idx, pref, &dxgi.IID_IDXGIAdapter1, &raw);
            if (hr == win32.DXGI_ERROR_NOT_FOUND) break;
            if (win32.failed(hr) or raw == null) break;
            const adapter: *dxgi.IDXGIAdapter1 = @ptrCast(@alignCast(raw.?));
            if (isSuitableHardwareAdapter(adapter, d3d12_lib, feature_level)) {
                return adapter;
            }
            _ = adapter.release();
        }
    }

    // Fallback to IDXGIFactory4::EnumAdapters1.
    var idx: u32 = 0;
    while (true) : (idx += 1) {
        var adapter_opt: ?*dxgi.IDXGIAdapter1 = null;
        const hr = factory4.enumAdapters1(idx, &adapter_opt);
        if (hr == win32.DXGI_ERROR_NOT_FOUND) break;
        if (win32.failed(hr) or adapter_opt == null) break;
        const adapter = adapter_opt.?;
        if (isSuitableHardwareAdapter(adapter, d3d12_lib, feature_level)) {
            return adapter;
        }
        _ = adapter.release();
    }

    diag.set("directx: no Direct3D 12 compatible hardware adapter found");
    return error.BackendUnavailable;
}

fn isSuitableHardwareAdapter(
    adapter: *dxgi.IDXGIAdapter1,
    d3d12_lib: *const d3d12.D3D12Lib,
    feature_level: d3d12.D3D_FEATURE_LEVEL,
) bool {
    var desc: dxgi.DXGI_ADAPTER_DESC1 = std.mem.zeroes(dxgi.DXGI_ADAPTER_DESC1);
    if (win32.failed(adapter.getDesc1(&desc))) return false;
    if ((desc.Flags & dxgi.DXGI_ADAPTER_FLAG_SOFTWARE) != 0) return false;
    // Probe D3D12 support without instantiating a device (ppDevice = null -> S_FALSE on success).
    const hr = d3d12_lib.create_device(@ptrCast(adapter), feature_level, &d3d12.IID_ID3D12Device, null);
    return win32.succeeded(hr);
}
