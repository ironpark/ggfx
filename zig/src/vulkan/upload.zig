const std = @import("std");
const errors = @import("../errors.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const device_mod = @import("device.zig");
const memory_mod = @import("memory.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

const initial_chunk_size: u64 = 2 * 1024 * 1024; // 2 MiB
const max_chunk_size: u64 = 256 * 1024 * 1024; // 256 MiB (keeps dynamic UBO offsets well inside u32)
const min_readback_size: u64 = 64 * 1024; // 64 KiB

pub const UploadSlice = struct {
    buffer: vk.Buffer,
    offset: u64,
    ptr: [*]u8,
};

pub const Chunk = struct {
    buffer: vk.Buffer,
    alloc: memory_mod.Allocation,
    capacity: u64,
    offset: u64,
};

/// Per-frame growing HOST_VISIBLE | HOST_COHERENT upload ring for VB/IB, UBO, and staging data.
pub const UploadRing = struct {
    active: ?Chunk = null,
    retired: std.ArrayList(Chunk) = .empty,
    /// Rotated-out shared upload arenas. Unlike `retired`, these go back to a
    /// `SharedChunkPool` for reuse once the frame's fence completes.
    retired_shared: std.ArrayList(Chunk) = .empty,

    pub fn alloc(
        self: *UploadRing,
        gpa: Allocator,
        dev: *const device_mod.Device,
        mem: *memory_mod.MemoryAllocator,
        size: u64,
        alignment: u64,
        diag: *Diag,
    ) Error!UploadSlice {
        const alloc_size = @max(size, 1);
        const align_val = @max(alignment, 1);

        // Oversized single request (> 256 MiB): allocate a dedicated chunk at offset 0 and track in
        // `retired` so `active` chunks never exceed `max_chunk_size`.
        if (alloc_size > max_chunk_size) {
            const dedicated = try createUploadChunk(dev, mem, alloc_size, diag);
            errdefer destroyChunk(dev, mem, dedicated);
            const base = dedicated.alloc.mapped orelse return error.Internal;
            self.retired.append(gpa, dedicated) catch return error.OutOfMemory;
            return .{
                .buffer = dedicated.buffer,
                .offset = 0,
                .ptr = base,
            };
        }

        if (self.active) |*chunk| {
            const aligned = memory_mod.alignUp(chunk.offset, align_val);
            if (aligned <= chunk.capacity and alloc_size <= chunk.capacity - aligned) {
                chunk.offset = aligned + alloc_size;
                const base = chunk.alloc.mapped orelse return error.Internal;
                return .{
                    .buffer = chunk.buffer,
                    .offset = aligned,
                    .ptr = base + @as(usize, @intCast(aligned)),
                };
            }
            // Active chunk is full: retire it until frame fence completes.
            const prev_cap = chunk.capacity;
            self.retired.append(gpa, chunk.*) catch return error.OutOfMemory;
            self.active = null;
            const grown = @min(prev_cap *| 2, max_chunk_size);
            const new_cap = @max(@max(grown, alloc_size), initial_chunk_size);
            self.active = try createUploadChunk(dev, mem, new_cap, diag);
        } else {
            const new_cap = @max(alloc_size, initial_chunk_size);
            self.active = try createUploadChunk(dev, mem, new_cap, diag);
        }

        const chunk = &(self.active.?);
        chunk.offset = alloc_size;
        const base = chunk.alloc.mapped orelse return error.Internal;
        return .{
            .buffer = chunk.buffer,
            .offset = 0,
            .ptr = base,
        };
    }

    /// Holds a rotated-out shared upload arena until this frame's GPU fence
    /// completes, when `reset` hands it to the pool.
    pub fn retireSharedChunk(self: *UploadRing, gpa: Allocator, chunk: Chunk) Error!void {
        self.retired_shared.append(gpa, chunk) catch return error.OutOfMemory;
    }

    /// Whether `reset` has chunks to release. Their contents may still be read
    /// by command buffers of other frame slots.
    pub fn hasRetired(self: *const UploadRing) bool {
        return self.retired.items.len > 0 or self.retired_shared.items.len > 0;
    }

    /// Resets active offset, frees retired chunks, and returns retired shared arenas to `pool`
    /// (must only be called after frame fence signals).
    pub fn reset(
        self: *UploadRing,
        dev: *const device_mod.Device,
        mem: *memory_mod.MemoryAllocator,
        pool: *SharedChunkPool,
    ) void {
        for (self.retired.items) |chunk| {
            destroyChunk(dev, mem, chunk);
        }
        self.retired.clearRetainingCapacity();
        for (self.retired_shared.items) |chunk| {
            pool.put(dev, mem, chunk);
        }
        self.retired_shared.clearRetainingCapacity();
        if (self.active) |*chunk| {
            chunk.offset = 0;
        }
    }

    pub fn deinit(
        self: *UploadRing,
        gpa: Allocator,
        dev: *const device_mod.Device,
        mem: *memory_mod.MemoryAllocator,
        pool: *SharedChunkPool,
    ) void {
        self.reset(dev, mem, pool);
        if (self.active) |chunk| {
            destroyChunk(dev, mem, chunk);
            self.active = null;
        }
        self.retired.deinit(gpa);
        self.retired_shared.deinit(gpa);
    }
};

/// Idle shared upload arenas kept for reuse, so that rotating the arena does
/// not create, bind, and map a new buffer every time. Bounded by count and by
/// total bytes; a chunk that does not fit is destroyed instead.
pub const SharedChunkPool = struct {
    pub const max_idle = 4;
    pub const max_idle_bytes: u64 = 64 << 20;

    idle: [max_idle]Chunk = undefined,
    len: usize = 0,
    bytes: u64 = 0,

    /// Removes and returns the smallest idle chunk holding at least `capacity` bytes.
    pub fn take(self: *SharedChunkPool, capacity: u64) ?Chunk {
        var best: ?usize = null;
        for (self.idle[0..self.len], 0..) |chunk, i| {
            if (chunk.capacity < capacity) continue;
            if (best == null or chunk.capacity < self.idle[best.?].capacity) best = i;
        }
        const i = best orelse return null;
        const chunk = self.idle[i];
        self.len -= 1;
        self.idle[i] = self.idle[self.len];
        self.bytes -= chunk.capacity;
        return chunk;
    }

    /// Keeps `chunk` for reuse. The GPU must no longer be reading it.
    pub fn put(
        self: *SharedChunkPool,
        dev: *const device_mod.Device,
        mem: *memory_mod.MemoryAllocator,
        chunk: Chunk,
    ) void {
        if (self.len == max_idle or chunk.capacity > max_idle_bytes - self.bytes) {
            destroyChunk(dev, mem, chunk);
            return;
        }
        self.idle[self.len] = chunk;
        self.len += 1;
        self.bytes += chunk.capacity;
    }

    pub fn deinit(self: *SharedChunkPool, dev: *const device_mod.Device, mem: *memory_mod.MemoryAllocator) void {
        for (self.idle[0..self.len]) |chunk| {
            destroyChunk(dev, mem, chunk);
        }
        self.len = 0;
        self.bytes = 0;
    }
};

fn createChunkWithUsage(
    dev: *const device_mod.Device,
    mem: *memory_mod.MemoryAllocator,
    capacity: u64,
    usage: vk.Flags,
    label: []const u8,
    diag: *Diag,
) Error!Chunk {
    const bci = vk.BufferCreateInfo{
        .size = capacity,
        .usage = usage,
        .sharingMode = .exclusive,
    };
    var buf: vk.Buffer = .null_handle;
    try loader_mod.check(
        dev.disp.vkCreateBuffer(dev.handle, &bci, null, &buf),
        diag,
        label,
    );
    errdefer dev.disp.vkDestroyBuffer(dev.handle, buf, null);

    var req: vk.MemoryRequirements = undefined;
    dev.disp.vkGetBufferMemoryRequirements(dev.handle, buf, &req);

    const allocation = try mem.alloc(
        req,
        vk.MemoryProperty.host_visible_bit | vk.MemoryProperty.host_coherent_bit,
        0,
        true,
        true,
        true,
        diag,
    );
    errdefer mem.free(allocation);

    try loader_mod.check(
        dev.disp.vkBindBufferMemory(dev.handle, buf, allocation.memory, allocation.offset),
        diag,
        "vkBindBufferMemory(upload)",
    );

    return .{
        .buffer = buf,
        .alloc = allocation,
        .capacity = capacity,
        .offset = 0,
    };
}

fn createUploadChunk(
    dev: *const device_mod.Device,
    mem: *memory_mod.MemoryAllocator,
    capacity: u64,
    diag: *Diag,
) Error!Chunk {
    return createChunkWithUsage(
        dev,
        mem,
        capacity,
        vk.BufferUsage.vertex_buffer_bit |
            vk.BufferUsage.index_buffer_bit |
            vk.BufferUsage.uniform_buffer_bit |
            vk.BufferUsage.transfer_src_bit,
        "vkCreateBuffer(upload)",
        diag,
    );
}

/// Creates a HOST_VISIBLE | HOST_COHERENT persistently-mapped VkBuffer for the shared upload arena.
pub fn createSharedChunk(
    dev: *const device_mod.Device,
    mem: *memory_mod.MemoryAllocator,
    capacity: u64,
    diag: *Diag,
) Error!Chunk {
    return createChunkWithUsage(
        dev,
        mem,
        capacity,
        vk.BufferUsage.transfer_src_bit | vk.BufferUsage.uniform_buffer_bit,
        "vkCreateBuffer(shared_upload)",
        diag,
    );
}

pub fn destroyChunk(dev: *const device_mod.Device, mem: *memory_mod.MemoryAllocator, chunk: Chunk) void {
    if (chunk.buffer != .null_handle) {
        dev.disp.vkDestroyBuffer(dev.handle, chunk.buffer, null);
    }
    mem.free(chunk.alloc);
}

/// Reusable host-visible (prefers cached) readback staging buffer for readPixels.
pub const ReadbackBuffer = struct {
    buffer: vk.Buffer = .null_handle,
    alloc: memory_mod.Allocation = .{},
    capacity: u64 = 0,

    pub fn ensure(
        self: *ReadbackBuffer,
        dev: *const device_mod.Device,
        mem: *memory_mod.MemoryAllocator,
        needed: u64,
        diag: *Diag,
    ) Error!void {
        if (self.buffer != .null_handle and self.capacity >= needed) return;

        self.deinit(dev, mem);

        const cap = @max(needed, min_readback_size);
        const bci = vk.BufferCreateInfo{
            .size = cap,
            .usage = vk.BufferUsage.transfer_dst_bit,
            .sharingMode = .exclusive,
        };
        var buf: vk.Buffer = .null_handle;
        try loader_mod.check(
            dev.disp.vkCreateBuffer(dev.handle, &bci, null, &buf),
            diag,
            "vkCreateBuffer(readback)",
        );
        errdefer dev.disp.vkDestroyBuffer(dev.handle, buf, null);

        var req: vk.MemoryRequirements = undefined;
        dev.disp.vkGetBufferMemoryRequirements(dev.handle, buf, &req);

        const allocation = try mem.alloc(
            req,
            vk.MemoryProperty.host_visible_bit,
            vk.MemoryProperty.host_cached_bit,
            true,
            true,
            true,
            diag,
        );
        errdefer mem.free(allocation);

        try loader_mod.check(
            dev.disp.vkBindBufferMemory(dev.handle, buf, allocation.memory, allocation.offset),
            diag,
            "vkBindBufferMemory(readback)",
        );

        self.buffer = buf;
        self.alloc = allocation;
        self.capacity = cap;
    }

    pub fn invalidate(self: *const ReadbackBuffer, dev: *const device_mod.Device, size: u64) void {
        if (self.alloc.coherent or self.alloc.memory == .null_handle) return;
        const atom = @max(dev.props.limits.nonCoherentAtomSize, 1);
        const aligned_size = @min(memory_mod.alignUp(size, atom), self.alloc.size);
        const range = vk.MappedMemoryRange{
            .memory = self.alloc.memory,
            .offset = self.alloc.offset,
            .size = if (aligned_size == self.alloc.size) vk.WHOLE_SIZE else aligned_size,
        };
        _ = dev.disp.vkInvalidateMappedMemoryRanges(dev.handle, 1, @ptrCast(&range));
    }

    pub fn deinit(self: *ReadbackBuffer, dev: *const device_mod.Device, mem: *memory_mod.MemoryAllocator) void {
        if (self.buffer != .null_handle) {
            dev.disp.vkDestroyBuffer(dev.handle, self.buffer, null);
            self.buffer = .null_handle;
        }
        mem.free(self.alloc);
        self.alloc = .{};
        self.capacity = 0;
    }
};
