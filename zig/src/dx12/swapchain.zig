const std = @import("std");
const errors = @import("../errors.zig");
const win32 = @import("win32.zig");
const com = @import("com.zig");
const dxgi = @import("dxgi.zig");
const d3d12 = @import("d3d12.zig");
const dcomp = @import("dcomp.zig");
const descriptors = @import("descriptors.zig");

const Error = errors.Error;
const Diag = errors.Diag;
const max_buffers = descriptors.max_frames_in_flight;

pub const dcomp_buffer_align: u32 = 1024;

pub inline fn alignSwapChainBufferSize(size: u32) u32 {
    return ((size + dcomp_buffer_align - 1) / dcomp_buffer_align) * dcomp_buffer_align;
}

/// Probes whether DirectComposition + CreateSwapChainForComposition work on this system.
pub fn probeDirectComposition(
    factory4: *dxgi.IDXGIFactory4,
    cmd_queue: *d3d12.ID3D12CommandQueue,
    dcomp_lib: *const dcomp.DCompLib,
    allow_tearing: bool,
    buffer_count: u32,
) bool {
    if (!dcomp_lib.isAvailable()) return false;

    var desc = dxgi.DXGI_SWAP_CHAIN_DESC1{
        .Width = 1,
        .Height = 1,
        .Format = dxgi.DXGI_FORMAT_B8G8R8A8_UNORM,
        .Stereo = 0,
        .SampleDesc = .{ .Count = 1, .Quality = 0 },
        .BufferUsage = dxgi.DXGI_USAGE_RENDER_TARGET_OUTPUT,
        .BufferCount = buffer_count,
        .Scaling = dxgi.DXGI_SCALING_STRETCH,
        .SwapEffect = dxgi.DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
        .AlphaMode = dxgi.DXGI_ALPHA_MODE_IGNORE,
        .Flags = if (allow_tearing) dxgi.DXGI_SWAP_CHAIN_FLAG_ALLOW_TEARING else 0,
    };
    var sc1: ?*dxgi.IDXGISwapChain1 = null;
    const sc_hr = factory4.createSwapChainForComposition(@ptrCast(cmd_queue), &desc, null, &sc1);
    if (win32.failed(sc_hr) or sc1 == null) return false;
    _ = sc1.?.release();

    var dcomp_dev: ?*dcomp.IDCompositionDevice = null;
    const dc_hr = dcomp_lib.createDevice(null, &dcomp_dev);
    if (win32.failed(dc_hr) or dcomp_dev == null) return false;
    _ = dcomp_dev.?.release();

    return true;
}

pub const SwapChain = struct {
    swapchain: *dxgi.IDXGISwapChain3,
    dcomp_device: ?*dcomp.IDCompositionDevice = null,
    dcomp_target: ?*dcomp.IDCompositionTarget = null,
    dcomp_visual: ?*dcomp.IDCompositionVisual = null,

    rtv_heap: *d3d12.ID3D12DescriptorHeap,
    rtv_start: d3d12.D3D12_CPU_DESCRIPTOR_HANDLE,
    rtv_stride: u32,

    back_buffers: [max_buffers]?*d3d12.ID3D12Resource = @splat(null),
    states: [max_buffers]d3d12.D3D12_RESOURCE_STATES = @splat(d3d12.D3D12_RESOURCE_STATE_PRESENT),
    buffer_count: u32,
    back_buffer_index: u32 = 0,

    buffer_width: u32,
    buffer_height: u32,
    allow_tearing: bool,
    occluded: bool = false,

    pub fn create(
        device: *d3d12.ID3D12Device,
        factory4: *dxgi.IDXGIFactory4,
        cmd_queue: *d3d12.ID3D12CommandQueue,
        dcomp_lib: *const dcomp.DCompLib,
        user32_lib: *const win32.User32Lib,
        hwnd: win32.HWND,
        width: u32,
        height: u32,
        buffer_count: u32,
        allow_tearing: bool,
        rtv_stride: u32,
        diag: *Diag,
    ) Error!SwapChain {
        const use_dcomp = user32_lib.windowHasNoRedirectionBitmap(hwnd);
        const buf_w = if (use_dcomp) alignSwapChainBufferSize(width) else width;
        const buf_h = if (use_dcomp) alignSwapChainBufferSize(height) else height;
        const sc_flags: u32 = if (allow_tearing) dxgi.DXGI_SWAP_CHAIN_FLAG_ALLOW_TEARING else 0;

        var sc1: ?*dxgi.IDXGISwapChain1 = null;
        var dcomp_dev: ?*dcomp.IDCompositionDevice = null;
        var dcomp_target: ?*dcomp.IDCompositionTarget = null;
        var dcomp_visual: ?*dcomp.IDCompositionVisual = null;
        errdefer {
            com.safeRelease(&dcomp_visual);
            com.safeRelease(&dcomp_target);
            com.safeRelease(&dcomp_dev);
            com.safeRelease(&sc1);
        }

        if (use_dcomp) {
            const desc1 = dxgi.DXGI_SWAP_CHAIN_DESC1{
                .Width = buf_w,
                .Height = buf_h,
                .Format = dxgi.DXGI_FORMAT_B8G8R8A8_UNORM,
                .Stereo = 0,
                .SampleDesc = .{ .Count = 1, .Quality = 0 },
                .BufferUsage = dxgi.DXGI_USAGE_RENDER_TARGET_OUTPUT,
                .BufferCount = buffer_count,
                .Scaling = dxgi.DXGI_SCALING_STRETCH,
                .SwapEffect = dxgi.DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
                .AlphaMode = dxgi.DXGI_ALPHA_MODE_IGNORE,
                .Flags = sc_flags,
            };
            const hr = factory4.createSwapChainForComposition(@ptrCast(cmd_queue), &desc1, null, &sc1);
            if (win32.failed(hr) or sc1 == null) {
                diag.print("directx: CreateSwapChainForComposition failed (0x{x:0>8})", .{win32.hrHex(hr)});
                return win32.hrToError(hr);
            }

            const dc_hr = dcomp_lib.createDevice(null, &dcomp_dev);
            if (win32.failed(dc_hr) or dcomp_dev == null) {
                diag.print("directx: DCompositionCreateDevice failed (0x{x:0>8})", .{win32.hrHex(dc_hr)});
                return win32.hrToError(dc_hr);
            }
            const tgt_hr = dcomp_dev.?.createTargetForHwnd(hwnd, true, &dcomp_target);
            if (win32.failed(tgt_hr) or dcomp_target == null) {
                diag.print("directx: IDCompositionDevice::CreateTargetForHwnd failed (0x{x:0>8})", .{win32.hrHex(tgt_hr)});
                return win32.hrToError(tgt_hr);
            }
            const vis_hr = dcomp_dev.?.createVisual(&dcomp_visual);
            if (win32.failed(vis_hr) or dcomp_visual == null) {
                diag.print("directx: IDCompositionDevice::CreateVisual failed (0x{x:0>8})", .{win32.hrHex(vis_hr)});
                return win32.hrToError(vis_hr);
            }
            const cnt_hr = dcomp_visual.?.setContent(@ptrCast(sc1.?));
            if (win32.failed(cnt_hr)) {
                diag.print("directx: IDCompositionVisual::SetContent failed (0x{x:0>8})", .{win32.hrHex(cnt_hr)});
                return win32.hrToError(cnt_hr);
            }
            const root_hr = dcomp_target.?.setRoot(dcomp_visual.?);
            if (win32.failed(root_hr)) {
                diag.print("directx: IDCompositionTarget::SetRoot failed (0x{x:0>8})", .{win32.hrHex(root_hr)});
                return win32.hrToError(root_hr);
            }
            const com_hr = dcomp_dev.?.commit();
            if (win32.failed(com_hr)) {
                diag.print("directx: IDCompositionDevice::Commit failed (0x{x:0>8})", .{win32.hrHex(com_hr)});
                return win32.hrToError(com_hr);
            }
        } else {
            const desc1 = dxgi.DXGI_SWAP_CHAIN_DESC1{
                .Width = buf_w,
                .Height = buf_h,
                .Format = dxgi.DXGI_FORMAT_B8G8R8A8_UNORM,
                .Stereo = 0,
                .SampleDesc = .{ .Count = 1, .Quality = 0 },
                .BufferUsage = dxgi.DXGI_USAGE_RENDER_TARGET_OUTPUT,
                .BufferCount = buffer_count,
                .Scaling = dxgi.DXGI_SCALING_STRETCH,
                .SwapEffect = dxgi.DXGI_SWAP_EFFECT_FLIP_SEQUENTIAL,
                .AlphaMode = dxgi.DXGI_ALPHA_MODE_UNSPECIFIED,
                .Flags = sc_flags,
            };
            const hr = factory4.createSwapChainForHwnd(@ptrCast(cmd_queue), hwnd, &desc1, null, null, &sc1);
            if (win32.failed(hr) or sc1 == null) {
                diag.print("directx: CreateSwapChainForHwnd failed (0x{x:0>8})", .{win32.hrHex(hr)});
                return win32.hrToError(hr);
            }
            const mwa_hr = factory4.makeWindowAssociation(
                hwnd,
                dxgi.DXGI_MWA_NO_WINDOW_CHANGES | dxgi.DXGI_MWA_NO_ALT_ENTER,
            );
            if (win32.failed(mwa_hr)) {
                diag.print("directx: MakeWindowAssociation failed (0x{x:0>8})", .{win32.hrHex(mwa_hr)});
                return win32.hrToError(mwa_hr);
            }
        }

        const sc3 = com.queryInterface(dxgi.IDXGISwapChain3, sc1.?, &dxgi.IID_IDXGISwapChain3) orelse {
            diag.set("directx: IDXGISwapChain3 is not available");
            return error.Unsupported;
        };
        // IDXGISwapChain3 owns a reference now; drop sc1.
        _ = sc1.?.release();
        sc1 = null;
        errdefer _ = sc3.release();

        // Dedicated per-swapchain RTV descriptor heap.
        const rtv_heap_desc = d3d12.D3D12_DESCRIPTOR_HEAP_DESC{
            .Type = d3d12.D3D12_DESCRIPTOR_HEAP_TYPE_RTV,
            .NumDescriptors = buffer_count,
            .Flags = d3d12.D3D12_DESCRIPTOR_HEAP_FLAG_NONE,
            .NodeMask = 0,
        };
        var rtv_heap_opt: ?*d3d12.ID3D12DescriptorHeap = null;
        const heap_hr = device.createDescriptorHeap(&rtv_heap_desc, &rtv_heap_opt);
        if (win32.failed(heap_hr) or rtv_heap_opt == null) {
            diag.print("directx: CreateDescriptorHeap(swapchain RTV) failed (0x{x:0>8})", .{win32.hrHex(heap_hr)});
            return win32.hrToError(heap_hr);
        }
        const rtv_heap = rtv_heap_opt.?;
        errdefer _ = rtv_heap.release();

        const rtv_start = rtv_heap.getCpuHandleStart();
        if (rtv_start.ptr == 0) {
            diag.set("directx: swapchain RTV heap start handle is 0");
            return error.Internal;
        }

        var self = SwapChain{
            .swapchain = sc3,
            .dcomp_device = dcomp_dev,
            .dcomp_target = dcomp_target,
            .dcomp_visual = dcomp_visual,
            .rtv_heap = rtv_heap,
            .rtv_start = rtv_start,
            .rtv_stride = rtv_stride,
            .buffer_count = buffer_count,
            .buffer_width = buf_w,
            .buffer_height = buf_h,
            .allow_tearing = allow_tearing,
        };
        errdefer self.releaseBackBuffers();
        try self.createBackBufferRtvs(device, diag);
        self.back_buffer_index = sc3.getCurrentBackBufferIndex();
        return self;
    }

    pub fn deinit(self: *SwapChain) void {
        self.releaseBackBuffers();
        _ = self.rtv_heap.release();
        _ = self.swapchain.release();
        com.safeRelease(&self.dcomp_visual);
        com.safeRelease(&self.dcomp_target);
        com.safeRelease(&self.dcomp_device);
    }

    pub fn canReuseBuffers(self: *const SwapChain, width: u32, height: u32) bool {
        return self.dcomp_device != null and width <= self.buffer_width and height <= self.buffer_height;
    }

    /// Resizes swapchain back buffers. Caller MUST ensure GPU is completely idle beforehand.
    pub fn resize(self: *SwapChain, device: *d3d12.ID3D12Device, width: u32, height: u32, diag: *Diag) Error!void {
        self.releaseBackBuffers();

        const new_w = if (self.dcomp_device != null)
            alignSwapChainBufferSize(@max(width, self.buffer_width))
        else
            width;
        const new_h = if (self.dcomp_device != null)
            alignSwapChainBufferSize(@max(height, self.buffer_height))
        else
            height;
        const flags: u32 = if (self.allow_tearing) dxgi.DXGI_SWAP_CHAIN_FLAG_ALLOW_TEARING else 0;

        const hr = self.swapchain.resizeBuffers(
            self.buffer_count,
            new_w,
            new_h,
            dxgi.DXGI_FORMAT_B8G8R8A8_UNORM,
            flags,
        );
        if (win32.failed(hr)) {
            diag.print("directx: IDXGISwapChain::ResizeBuffers({d}x{d}) failed (0x{x:0>8})", .{
                new_w,
                new_h,
                win32.hrHex(hr),
            });
            return win32.hrToError(hr);
        }
        self.buffer_width = new_w;
        self.buffer_height = new_h;

        if (self.dcomp_device) |dc| {
            const com_hr = dc.commit();
            if (win32.failed(com_hr)) {
                diag.print("directx: IDCompositionDevice::Commit(resize) failed (0x{x:0>8})", .{win32.hrHex(com_hr)});
                return win32.hrToError(com_hr);
            }
        }

        try self.createBackBufferRtvs(device, diag);
        self.back_buffer_index = self.swapchain.getCurrentBackBufferIndex();
    }

    fn createBackBufferRtvs(self: *SwapChain, device: *d3d12.ID3D12Device, diag: *Diag) Error!void {
        var i: u32 = 0;
        while (i < self.buffer_count) : (i += 1) {
            var raw: ?*anyopaque = null;
            const hr = self.swapchain.getBuffer(i, &d3d12.IID_ID3D12Resource, &raw);
            if (win32.failed(hr) or raw == null) {
                diag.print("directx: IDXGISwapChain::GetBuffer({d}) failed (0x{x:0>8})", .{ i, win32.hrHex(hr) });
                return win32.hrToError(hr);
            }
            const res: *d3d12.ID3D12Resource = @ptrCast(@alignCast(raw.?));
            self.back_buffers[i] = res;
            self.states[i] = d3d12.D3D12_RESOURCE_STATE_PRESENT;
            const rtv_handle = self.rtv_start.offset(i, self.rtv_stride);
            device.createRenderTargetView(res, null, rtv_handle);
        }
    }

    pub fn releaseBackBuffers(self: *SwapChain) void {
        for (&self.back_buffers, &self.states) |*slot, *st| {
            com.safeRelease(slot);
            st.* = d3d12.D3D12_RESOURCE_STATE_PRESENT;
        }
    }

    pub inline fn currentResource(self: *const SwapChain) *d3d12.ID3D12Resource {
        return self.back_buffers[self.back_buffer_index].?;
    }

    pub inline fn currentRtvHandle(self: *const SwapChain) d3d12.D3D12_CPU_DESCRIPTOR_HANDLE {
        return self.rtv_start.offset(self.back_buffer_index, self.rtv_stride);
    }

    pub inline fn currentState(self: *const SwapChain) d3d12.D3D12_RESOURCE_STATES {
        return self.states[self.back_buffer_index];
    }

    pub inline fn setCurrentState(self: *SwapChain, state: d3d12.D3D12_RESOURCE_STATES) void {
        self.states[self.back_buffer_index] = state;
    }

    /// Presents the swapchain; returns true if visible (not occluded).
    pub fn present(self: *SwapChain, wait_for_vsync: bool, diag: *Diag) Error!bool {
        var sync_interval: u32 = 0;
        var flags: u32 = 0;
        if (self.occluded) {
            flags |= dxgi.DXGI_PRESENT_TEST;
        } else if (wait_for_vsync) {
            sync_interval = 1;
        }

        const hr = self.swapchain.present(sync_interval, flags);
        if (hr == win32.DXGI_STATUS_OCCLUDED) {
            self.occluded = true;
            return false;
        }
        if (win32.failed(hr)) {
            diag.print("directx: IDXGISwapChain::Present failed (0x{x:0>8})", .{win32.hrHex(hr)});
            return win32.hrToError(hr);
        }
        self.occluded = false;
        self.back_buffer_index = self.swapchain.getCurrentBackBufferIndex();
        return true;
    }
};
