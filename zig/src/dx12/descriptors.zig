const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const win32 = @import("win32.zig");
const d3d12 = @import("d3d12.zig");
const dxgi = @import("dxgi.zig");

const Allocator = std.mem.Allocator;
const Error = errors.Error;
const Diag = errors.Diag;

pub const srv_table_size: u32 = abi.max_src_images; // 4 (t0..t3)
pub const tables_per_frame: u32 = 4096;
pub const max_frames_in_flight: usize = 3;

const cpu_srv_chunk_size: u32 = 1024;
const cpu_rtv_chunk_size: u32 = 256;

pub const CpuDescriptorSlot = struct {
    chunk_idx: u32,
    slot_idx: u32,
    handle: d3d12.D3D12_CPU_DESCRIPTOR_HANDLE,
};

const CpuDescriptorChunk = struct {
    heap: *d3d12.ID3D12DescriptorHeap,
    cpu_start: d3d12.D3D12_CPU_DESCRIPTOR_HANDLE,
    used: u32,
    capacity: u32,
};

/// Growable chunked non-shader-visible CPU descriptor pool with free-list recycling.
pub const CpuDescriptorPool = struct {
    heap_type: d3d12.D3D12_DESCRIPTOR_HEAP_TYPE,
    chunk_capacity: u32,
    increment_size: u32,
    chunks: std.ArrayList(CpuDescriptorChunk) = .empty,
    free_list: std.ArrayList(CpuDescriptorSlot) = .empty,

    pub fn init(heap_type: d3d12.D3D12_DESCRIPTOR_HEAP_TYPE, chunk_capacity: u32, increment_size: u32) CpuDescriptorPool {
        return .{
            .heap_type = heap_type,
            .chunk_capacity = chunk_capacity,
            .increment_size = increment_size,
        };
    }

    pub fn deinit(self: *CpuDescriptorPool, gpa: Allocator) void {
        for (self.chunks.items) |chunk| {
            _ = chunk.heap.release();
        }
        self.chunks.deinit(gpa);
        self.free_list.deinit(gpa);
    }

    pub fn alloc(self: *CpuDescriptorPool, gpa: Allocator, device: *d3d12.ID3D12Device, diag: *Diag) Error!CpuDescriptorSlot {
        if (self.free_list.items.len > 0) {
            return self.free_list.pop().?;
        }
        if (self.chunks.items.len == 0 or self.chunks.items[self.chunks.items.len - 1].used >= self.chunks.items[self.chunks.items.len - 1].capacity) {
            const desc = d3d12.D3D12_DESCRIPTOR_HEAP_DESC{
                .Type = self.heap_type,
                .NumDescriptors = self.chunk_capacity,
                .Flags = d3d12.D3D12_DESCRIPTOR_HEAP_FLAG_NONE,
                .NodeMask = 0,
            };
            var heap: ?*d3d12.ID3D12DescriptorHeap = null;
            const hr = device.createDescriptorHeap(&desc, &heap);
            if (win32.failed(hr) or heap == null) {
                diag.print("directx: CreateDescriptorHeap(CPU pool) failed (0x{x:0>8})", .{win32.hrHex(hr)});
                return win32.hrToError(hr);
            }
            const h = heap.?;
            const start = h.getCpuHandleStart();
            if (start.ptr == 0) {
                _ = h.release();
                diag.set("directx: GetCPUDescriptorHandleForHeapStart returned 0");
                return error.Internal;
            }
            self.chunks.append(gpa, .{
                .heap = h,
                .cpu_start = start,
                .used = 0,
                .capacity = self.chunk_capacity,
            }) catch {
                _ = h.release();
                return error.OutOfMemory;
            };
        }
        const chunk_idx: u32 = @intCast(self.chunks.items.len - 1);
        const chunk = &self.chunks.items[chunk_idx];
        const slot_idx = chunk.used;
        chunk.used += 1;
        return .{
            .chunk_idx = chunk_idx,
            .slot_idx = slot_idx,
            .handle = chunk.cpu_start.offset(slot_idx, self.increment_size),
        };
    }

    pub fn free(self: *CpuDescriptorPool, gpa: Allocator, slot: CpuDescriptorSlot) void {
        self.free_list.append(gpa, slot) catch {};
    }
};

const FrameSrvRingState = struct {
    used_tables: u32 = 0,
    last_srcs: [srv_table_size]usize = @splat(0),
    last_gpu: d3d12.D3D12_GPU_DESCRIPTOR_HANDLE = .{ .ptr = 0 },
    last_valid: bool = false,
};

pub const DescriptorManager = struct {
    cbv_srv_uav_size: u32,
    rtv_size: u32,
    frames_in_flight: u32,

    cpu_srv_pool: CpuDescriptorPool,
    cpu_rtv_pool: CpuDescriptorPool,
    null_srv_slot: CpuDescriptorSlot,

    shader_visible_heap: *d3d12.ID3D12DescriptorHeap,
    sv_cpu_start: d3d12.D3D12_CPU_DESCRIPTOR_HANDLE,
    sv_gpu_start: d3d12.D3D12_GPU_DESCRIPTOR_HANDLE,
    frames: [max_frames_in_flight]FrameSrvRingState = @splat(.{}),

    pub fn init(gpa: Allocator, device: *d3d12.ID3D12Device, frames_in_flight: u32, diag: *Diag) Error!DescriptorManager {
        const cbv_srv_uav_size = device.getDescriptorHandleIncrementSize(d3d12.D3D12_DESCRIPTOR_HEAP_TYPE_CBV_SRV_UAV);
        const rtv_size = device.getDescriptorHandleIncrementSize(d3d12.D3D12_DESCRIPTOR_HEAP_TYPE_RTV);

        var cpu_srv_pool = CpuDescriptorPool.init(
            d3d12.D3D12_DESCRIPTOR_HEAP_TYPE_CBV_SRV_UAV,
            cpu_srv_chunk_size,
            cbv_srv_uav_size,
        );
        errdefer cpu_srv_pool.deinit(gpa);

        var cpu_rtv_pool = CpuDescriptorPool.init(
            d3d12.D3D12_DESCRIPTOR_HEAP_TYPE_RTV,
            cpu_rtv_chunk_size,
            rtv_size,
        );
        errdefer cpu_rtv_pool.deinit(gpa);

        // Allocate and populate the shared null SRV descriptor in the CPU-only heap.
        const null_srv_slot = try cpu_srv_pool.alloc(gpa, device, diag);
        const null_desc = d3d12.D3D12_SHADER_RESOURCE_VIEW_DESC{
            .Format = dxgi.DXGI_FORMAT_R8G8B8A8_UNORM,
            .ViewDimension = d3d12.D3D12_SRV_DIMENSION_TEXTURE2D,
            .Shader4ComponentMapping = d3d12.D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING,
            .u = .{ .Texture2D = .{ .MostDetailedMip = 0, .MipLevels = 1, .PlaneSlice = 0, .ResourceMinLODClamp = 0.0 } },
        };
        device.createShaderResourceView(null, &null_desc, null_srv_slot.handle);

        // Allocate shader-visible CBV/SRV/UAV ring heap: frames_in_flight * tables_per_frame * 4 SRVs.
        const total_sv_descriptors = frames_in_flight * tables_per_frame * srv_table_size;
        const sv_desc = d3d12.D3D12_DESCRIPTOR_HEAP_DESC{
            .Type = d3d12.D3D12_DESCRIPTOR_HEAP_TYPE_CBV_SRV_UAV,
            .NumDescriptors = total_sv_descriptors,
            .Flags = d3d12.D3D12_DESCRIPTOR_HEAP_FLAG_SHADER_VISIBLE,
            .NodeMask = 0,
        };
        var sv_heap: ?*d3d12.ID3D12DescriptorHeap = null;
        const hr = device.createDescriptorHeap(&sv_desc, &sv_heap);
        if (win32.failed(hr) or sv_heap == null) {
            diag.print("directx: CreateDescriptorHeap(shader-visible SRV ring) failed (0x{x:0>8})", .{win32.hrHex(hr)});
            return win32.hrToError(hr);
        }
        const heap = sv_heap.?;
        errdefer _ = heap.release();

        const sv_cpu_start = heap.getCpuHandleStart();
        const sv_gpu_start = heap.getGpuHandleStart();
        if (sv_cpu_start.ptr == 0 or sv_gpu_start.ptr == 0) {
            diag.set("directx: shader-visible descriptor heap returned null start handle");
            return error.Internal;
        }

        return .{
            .cbv_srv_uav_size = cbv_srv_uav_size,
            .rtv_size = rtv_size,
            .frames_in_flight = frames_in_flight,
            .cpu_srv_pool = cpu_srv_pool,
            .cpu_rtv_pool = cpu_rtv_pool,
            .null_srv_slot = null_srv_slot,
            .shader_visible_heap = heap,
            .sv_cpu_start = sv_cpu_start,
            .sv_gpu_start = sv_gpu_start,
        };
    }

    pub fn deinit(self: *DescriptorManager, gpa: Allocator) void {
        _ = self.shader_visible_heap.release();
        self.cpu_srv_pool.deinit(gpa);
        self.cpu_rtv_pool.deinit(gpa);
    }

    pub fn createImageSrv(
        self: *DescriptorManager,
        gpa: Allocator,
        device: *d3d12.ID3D12Device,
        resource: *d3d12.ID3D12Resource,
        format: abi.PixelFormat,
        diag: *Diag,
    ) Error!CpuDescriptorSlot {
        const slot = try self.cpu_srv_pool.alloc(gpa, device, diag);
        const dxgi_fmt: dxgi.DXGI_FORMAT = switch (format) {
            .rgba8 => dxgi.DXGI_FORMAT_R8G8B8A8_UNORM,
            .a8 => dxgi.DXGI_FORMAT_R8_UNORM,
        };
        // RGBA8: default identity swizzle 0x1688
        // A8:    R8_UNORM with RRRR swizzle 0x1000 so shader reads (a, a, a, a)
        const mapping: u32 = switch (format) {
            .rgba8 => d3d12.D3D12_DEFAULT_SHADER_4_COMPONENT_MAPPING,
            .a8 => d3d12.D3D12_SHADER_4_COMPONENT_MAPPING_R_R_R_R,
        };
        const srv_desc = d3d12.D3D12_SHADER_RESOURCE_VIEW_DESC{
            .Format = dxgi_fmt,
            .ViewDimension = d3d12.D3D12_SRV_DIMENSION_TEXTURE2D,
            .Shader4ComponentMapping = mapping,
            .u = .{ .Texture2D = .{ .MostDetailedMip = 0, .MipLevels = 1, .PlaneSlice = 0, .ResourceMinLODClamp = 0.0 } },
        };
        device.createShaderResourceView(resource, &srv_desc, slot.handle);
        return slot;
    }

    pub fn createImageRtv(
        self: *DescriptorManager,
        gpa: Allocator,
        device: *d3d12.ID3D12Device,
        resource: *d3d12.ID3D12Resource,
        diag: *Diag,
    ) Error!CpuDescriptorSlot {
        const slot = try self.cpu_rtv_pool.alloc(gpa, device, diag);
        device.createRenderTargetView(resource, null, slot.handle);
        return slot;
    }

    pub fn freeImageSrv(self: *DescriptorManager, gpa: Allocator, slot: CpuDescriptorSlot) void {
        self.invalidateSrcHandle(slot.handle.ptr);
        self.cpu_srv_pool.free(gpa, slot);
    }

    pub fn freeImageRtv(self: *DescriptorManager, gpa: Allocator, slot: CpuDescriptorSlot) void {
        self.cpu_rtv_pool.free(gpa, slot);
    }

    pub fn invalidateSrcHandle(self: *DescriptorManager, cpu_ptr: usize) void {
        for (&self.frames) |*f| {
            if (!f.last_valid) continue;
            for (f.last_srcs) |s| {
                if (s == cpu_ptr) {
                    f.last_valid = false;
                    break;
                }
            }
        }
    }

    pub fn hasRoom(self: *const DescriptorManager, frame_index: usize) bool {
        return self.frames[frame_index].used_tables < tables_per_frame;
    }

    pub fn resetFrame(self: *DescriptorManager, frame_index: usize) void {
        self.frames[frame_index] = .{};
    }

    /// Returns a GPU descriptor handle for the 4-SRV table (t0..t3), reusing the
    /// previously allocated table if the source handle tuple matches.
    /// Caller must ensure `hasRoom(frame_index)` (or that the tuple matches) beforehand.
    pub fn getOrAllocSrvTable(
        self: *DescriptorManager,
        device: *d3d12.ID3D12Device,
        frame_index: usize,
        src_handles: [srv_table_size]d3d12.D3D12_CPU_DESCRIPTOR_HANDLE,
    ) struct { gpu: d3d12.D3D12_GPU_DESCRIPTOR_HANDLE, reused: bool } {
        const f = &self.frames[frame_index];
        var keys: [srv_table_size]usize = undefined;
        for (src_handles, 0..) |h, i| {
            keys[i] = h.ptr;
        }
        if (f.last_valid and std.mem.eql(usize, &f.last_srcs, &keys)) {
            return .{ .gpu = f.last_gpu, .reused = true };
        }

        const table_idx = f.used_tables;
        std.debug.assert(table_idx < tables_per_frame);
        f.used_tables += 1;

        const base_desc_idx: usize = (frame_index * tables_per_frame + table_idx) * srv_table_size;
        const dst_cpu = [1]d3d12.D3D12_CPU_DESCRIPTOR_HANDLE{
            self.sv_cpu_start.offset(base_desc_idx, self.cbv_srv_uav_size),
        };
        const dst_sizes = [1]u32{srv_table_size};
        device.copyDescriptors(
            &dst_cpu,
            &dst_sizes,
            &src_handles,
            null,
            d3d12.D3D12_DESCRIPTOR_HEAP_TYPE_CBV_SRV_UAV,
        );

        const gpu = self.sv_gpu_start.offset(base_desc_idx, self.cbv_srv_uav_size);
        f.last_srcs = keys;
        f.last_gpu = gpu;
        f.last_valid = true;
        return .{ .gpu = gpu, .reused = false };
    }
};
