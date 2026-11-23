const std = @import("std");
const errors = @import("../errors.zig");
const win32 = @import("win32.zig");
const d3d12 = @import("d3d12.zig");
const dxgi = @import("dxgi.zig");

const Allocator = std.mem.Allocator;
const Error = errors.Error;
const Diag = errors.Diag;

pub const min_ring_size: usize = 256 * 1024;
pub const default_shared_arena_size: u64 = 4 << 20; // 4 MiB
pub const max_shared_arena_size: u64 = 256 << 20; // 256 MiB
pub const vb_ib_alignment: usize = 4;
pub const cbv_alignment: usize = d3d12.D3D12_CONSTANT_BUFFER_DATA_PLACEMENT_ALIGNMENT; // 256
pub const texture_placement_alignment: usize = d3d12.D3D12_TEXTURE_DATA_PLACEMENT_ALIGNMENT; // 512
pub const texture_pitch_alignment: usize = d3d12.D3D12_TEXTURE_DATA_PITCH_ALIGNMENT; // 256

pub inline fn alignUp(val: usize, alignment: usize) usize {
    std.debug.assert(std.math.isPowerOfTwo(alignment));
    return (val + alignment - 1) & ~(alignment - 1);
}

pub inline fn alignUpU32(val: u32, alignment: u32) u32 {
    std.debug.assert(std.math.isPowerOfTwo(alignment));
    return (val + alignment - 1) & ~(alignment - 1);
}

pub inline fn alignUpU64(val: u64, alignment: u64) u64 {
    std.debug.assert(std.math.isPowerOfTwo(alignment));
    return (val + alignment - 1) & ~(alignment - 1);
}

pub fn createBuffer(
    device: *d3d12.ID3D12Device,
    size_bytes: u64,
    heap_type: d3d12.D3D12_HEAP_TYPE,
    diag: *Diag,
) Error!*d3d12.ID3D12Resource {
    const initial_state: d3d12.D3D12_RESOURCE_STATES = if (heap_type == d3d12.D3D12_HEAP_TYPE_READBACK)
        d3d12.D3D12_RESOURCE_STATE_COPY_DEST
    else
        d3d12.D3D12_RESOURCE_STATE_GENERIC_READ;

    const heap_props = d3d12.D3D12_HEAP_PROPERTIES{
        .Type = heap_type,
        .CPUPageProperty = d3d12.D3D12_CPU_PAGE_PROPERTY_UNKNOWN,
        .MemoryPoolPreference = d3d12.D3D12_MEMORY_POOL_UNKNOWN,
        .CreationNodeMask = 1,
        .VisibleNodeMask = 1,
    };
    const desc = d3d12.D3D12_RESOURCE_DESC{
        .Dimension = d3d12.D3D12_RESOURCE_DIMENSION_BUFFER,
        .Alignment = 0,
        .Width = @max(size_bytes, 256),
        .Height = 1,
        .DepthOrArraySize = 1,
        .MipLevels = 1,
        .Format = dxgi.DXGI_FORMAT_UNKNOWN,
        .SampleDesc = .{ .Count = 1, .Quality = 0 },
        .Layout = d3d12.D3D12_TEXTURE_LAYOUT_ROW_MAJOR,
        .Flags = d3d12.D3D12_RESOURCE_FLAG_NONE,
    };

    var res: ?*d3d12.ID3D12Resource = null;
    const hr = device.createCommittedResource(&heap_props, d3d12.D3D12_HEAP_FLAG_NONE, &desc, initial_state, null, &res);
    if (win32.failed(hr) or res == null) {
        diag.print("directx: CreateCommittedResource(buffer {d} B) failed (0x{x:0>8})", .{
            size_bytes,
            win32.hrHex(hr),
        });
        return win32.hrToError(hr);
    }
    return res.?;
}

pub const MappedUploadBuffer = struct {
    resource: *d3d12.ID3D12Resource,
    cpu_ptr: [*]u8,
    size: u64,
};

/// Creates and persistently maps a D3D12_HEAP_TYPE_UPLOAD buffer for the shared upload arena.
pub fn createMappedUploadBuffer(
    device: *d3d12.ID3D12Device,
    size_bytes: u64,
    diag: *Diag,
) Error!MappedUploadBuffer {
    const buf = try createBuffer(device, size_bytes, d3d12.D3D12_HEAP_TYPE_UPLOAD, diag);
    var mapped_raw: ?*anyopaque = null;
    const read_range = d3d12.D3D12_RANGE{ .Begin = 0, .End = 0 };
    const hr = buf.map(0, &read_range, &mapped_raw);
    if (win32.failed(hr) or mapped_raw == null) {
        _ = buf.release();
        diag.print("directx: ID3D12Resource::Map(shared upload arena) failed (0x{x:0>8})", .{win32.hrHex(hr)});
        return win32.hrToError(hr);
    }
    return .{
        .resource = buf,
        .cpu_ptr = @ptrCast(mapped_raw.?),
        .size = size_bytes,
    };
}

pub const Allocation = struct {
    resource: *d3d12.ID3D12Resource,
    offset: u64,
    gpu_addr: d3d12.D3D12_GPU_VIRTUAL_ADDRESS,
    cpu_ptr: [*]u8,
    size: usize,
};

/// Per-frame persistently mapped UPLOAD heap ring buffer.
/// On overflow, the previous buffer is moved to `retired` so in-flight GPU commands
/// referencing it remain valid until the frame fence completes and `reset()` is called.
pub const UploadRing = struct {
    buffer: ?*d3d12.ID3D12Resource = null,
    gpu_base: d3d12.D3D12_GPU_VIRTUAL_ADDRESS = 0,
    cpu_base: ?[*]u8 = null,
    size: usize = 0,
    offset: usize = 0,
    retired: std.ArrayList(*d3d12.ID3D12Resource) = .empty,

    // Frame-local uniform version dedupe cache (safe across growth because retired buffers stay alive).
    last_internal_version: u64 = 0,
    last_internal_gpu: d3d12.D3D12_GPU_VIRTUAL_ADDRESS = 0,
    last_user_version: u64 = 0,
    last_user_gpu: d3d12.D3D12_GPU_VIRTUAL_ADDRESS = 0,
    last_user_size: u32 = 0,

    pub fn alloc(
        self: *UploadRing,
        gpa: Allocator,
        device: *d3d12.ID3D12Device,
        byte_size: usize,
        alignment: usize,
        diag: *Diag,
    ) Error!Allocation {
        const req_size = @max(byte_size, 1);
        var aligned_off = alignUp(self.offset, alignment);

        if (self.buffer == null or aligned_off + req_size > self.size) {
            if (self.buffer) |old_buf| {
                self.retired.append(gpa, old_buf) catch return error.OutOfMemory;
                self.buffer = null;
                self.cpu_base = null;
                self.gpu_base = 0;
                self.offset = 0;
            }
            const doubled = std.math.mul(usize, self.size, 2) catch self.size;
            const target_size = @max(@max(doubled, alignUp(req_size, 65536)), min_ring_size);

            const new_buf = try createBuffer(device, @intCast(target_size), d3d12.D3D12_HEAP_TYPE_UPLOAD, diag);
            var mapped_raw: ?*anyopaque = null;
            const read_range = d3d12.D3D12_RANGE{ .Begin = 0, .End = 0 };
            const hr = new_buf.map(0, &read_range, &mapped_raw);
            if (win32.failed(hr) or mapped_raw == null) {
                _ = new_buf.release();
                diag.print("directx: ID3D12Resource::Map(upload ring) failed (0x{x:0>8})", .{win32.hrHex(hr)});
                return win32.hrToError(hr);
            }

            self.buffer = new_buf;
            self.gpu_base = new_buf.getGpuVirtualAddress();
            self.cpu_base = @ptrCast(mapped_raw.?);
            self.size = target_size;
            self.offset = 0;
            aligned_off = 0;
        }

        const buf = self.buffer.?;
        const cpu = self.cpu_base.?;
        self.offset = aligned_off + req_size;

        return .{
            .resource = buf,
            .offset = @intCast(aligned_off),
            .gpu_addr = self.gpu_base + @as(u64, @intCast(aligned_off)),
            .cpu_ptr = cpu + aligned_off,
            .size = req_size,
        };
    }

    /// Reclaims retired buffers and resets write offset after GPU completes this frame.
    pub fn reset(self: *UploadRing) void {
        for (self.retired.items) |buf| {
            buf.unmap(0, null);
            _ = buf.release();
        }
        self.retired.clearRetainingCapacity();
        self.offset = 0;
        self.last_internal_version = 0;
        self.last_internal_gpu = 0;
        self.last_user_version = 0;
        self.last_user_gpu = 0;
        self.last_user_size = 0;
    }

    pub fn deinit(self: *UploadRing, gpa: Allocator) void {
        self.reset();
        self.retired.deinit(gpa);
        if (self.buffer) |buf| {
            buf.unmap(0, null);
            _ = buf.release();
            self.buffer = null;
        }
        self.cpu_base = null;
        self.gpu_base = 0;
        self.size = 0;
    }
};

pub const TextureFootprint = struct {
    row_pitch: u32,
    total_bytes: usize,
};

/// Computes 256-byte aligned row pitch and total staging footprint size (unpadded last row).
pub fn computeTextureFootprint(width: u32, height: u32, bpp: u32) TextureFootprint {
    if (width == 0 or height == 0) {
        return .{ .row_pitch = 0, .total_bytes = 0 };
    }
    const row_bytes: usize = @as(usize, width) * @as(usize, bpp);
    const row_pitch: usize = alignUp(row_bytes, texture_pitch_alignment);
    const total_bytes: usize = row_pitch * @as(usize, height - 1) + row_bytes;
    return .{
        .row_pitch = @intCast(row_pitch),
        .total_bytes = total_bytes,
    };
}

/// Repacks tightly-packed pixel rows into a 256-byte RowPitch-aligned upload staging buffer.
pub fn packRowsToFootprint(
    dst: [*]u8,
    row_pitch: usize,
    src: []const u8,
    width: u32,
    height: u32,
    bpp: u32,
) void {
    const row_bytes: usize = @as(usize, width) * @as(usize, bpp);
    if (row_bytes == 0 or height == 0) return;
    if (row_pitch == row_bytes) {
        const total = row_bytes * @as(usize, height);
        @memcpy(dst[0..total], src[0..total]);
        return;
    }
    var row: usize = 0;
    while (row < height) : (row += 1) {
        const dst_off = row * row_pitch;
        const src_off = row * row_bytes;
        @memcpy(dst[dst_off .. dst_off + row_bytes], src[src_off .. src_off + row_bytes]);
    }
}

/// Unpacks a sub-rectangle from a 256-byte RowPitch-aligned readback footprint into tightly-packed dst.
pub fn unpackRowsFromFootprint(
    dst: [*]u8,
    src: [*]const u8,
    origin_x: u32,
    origin_y: u32,
    rect_x0: u32,
    rect_y0: u32,
    rect_dx: u32,
    rect_dy: u32,
    row_pitch: usize,
    bpp: u32,
) void {
    const row_bytes: usize = @as(usize, rect_dx) * @as(usize, bpp);
    if (row_bytes == 0 or rect_dy == 0) return;
    const base_off: usize = @as(usize, rect_x0 - origin_x) * @as(usize, bpp) +
        @as(usize, rect_y0 - origin_y) * row_pitch;
    if (base_off == 0 and row_pitch == row_bytes) {
        const total = row_bytes * @as(usize, rect_dy);
        @memcpy(dst[0..total], src[0..total]);
        return;
    }
    var row: usize = 0;
    while (row < rect_dy) : (row += 1) {
        const src_off = base_off + row * row_pitch;
        const dst_off = row * row_bytes;
        @memcpy(dst[dst_off .. dst_off + row_bytes], src[src_off .. src_off + row_bytes]);
    }
}
