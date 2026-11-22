const std = @import("std");
const errors = @import("../errors.zig");
const vk = @import("vk.zig");
const loader_mod = @import("loader.zig");
const device_mod = @import("device.zig");
const Allocator = std.mem.Allocator;
const Diag = errors.Diag;
const Error = errors.Error;

pub const default_block_size: u64 = 64 * 1024 * 1024; // 64 MiB
pub const dedicated_threshold: u64 = 32 * 1024 * 1024; // 32 MiB
const fallback_block_min: u64 = 4 * 1024 * 1024;

pub const FreeRegion = struct {
    offset: u64,
    size: u64,
};

const Block = struct {
    memory: vk.DeviceMemory,
    memory_type_index: u32,
    size: u64,
    mapped: ?[*]u8,
    linear: bool,
    dedicated: bool,
    coherent: bool,
    free_regions: std.ArrayList(FreeRegion) = .empty,
};

pub const Allocation = struct {
    block_index: usize = 0,
    memory: vk.DeviceMemory = .null_handle,
    offset: u64 = 0,
    size: u64 = 0,
    mapped: ?[*]u8 = null,
    coherent: bool = true,
};

pub fn alignUp(value: u64, alignment: u64) u64 {
    if (alignment <= 1) return value;
    const rem = value % alignment;
    if (rem == 0) return value;
    return value + (alignment - rem);
}

pub fn findMemoryType(
    mem_props: *const vk.PhysicalDeviceMemoryProperties,
    type_bits: u32,
    required_flags: vk.Flags,
    preferred_flags: vk.Flags,
) ?u32 {
    const count = @min(mem_props.memoryTypeCount, vk.MAX_MEMORY_TYPES);
    const combined = required_flags | preferred_flags;
    if (preferred_flags != 0) {
        var i: u32 = 0;
        while (i < count) : (i += 1) {
            const bit = @as(u32, 1) << @intCast(i);
            if ((type_bits & bit) != 0 and (mem_props.memoryTypes[i].propertyFlags & combined) == combined) {
                return i;
            }
        }
    }
    var i: u32 = 0;
    while (i < count) : (i += 1) {
        const bit = @as(u32, 1) << @intCast(i);
        if ((type_bits & bit) != 0 and (mem_props.memoryTypes[i].propertyFlags & required_flags) == required_flags) {
            return i;
        }
    }
    return null;
}

/// 64 MiB block-based best-fit suballocator with dedicated fallback for large resources.
pub const MemoryAllocator = struct {
    gpa: Allocator,
    dev: *const device_mod.Device,
    blocks: std.ArrayList(?Block) = .empty,

    pub fn init(gpa: Allocator, dev: *const device_mod.Device) MemoryAllocator {
        return .{
            .gpa = gpa,
            .dev = dev,
        };
    }

    pub fn deinit(self: *MemoryAllocator) void {
        for (self.blocks.items) |*slot| {
            if (slot.*) |*blk| {
                if (blk.mapped != null) {
                    self.dev.disp.vkUnmapMemory(self.dev.handle, blk.memory);
                }
                self.dev.disp.vkFreeMemory(self.dev.handle, blk.memory, null);
                blk.free_regions.deinit(self.gpa);
                slot.* = null;
            }
        }
        self.blocks.deinit(self.gpa);
    }

    pub fn alloc(
        self: *MemoryAllocator,
        req: vk.MemoryRequirements,
        required_flags: vk.Flags,
        preferred_flags: vk.Flags,
        linear: bool,
        map_memory: bool,
        dedicated_hint: bool,
        diag: *Diag,
    ) Error!Allocation {
        const mem_type = findMemoryType(&self.dev.mem_props, req.memoryTypeBits, required_flags, preferred_flags) orelse {
            diag.print("vulkan: no suitable memory type (bits=0x{x}, req=0x{x})", .{
                req.memoryTypeBits,
                required_flags,
            });
            return error.OutOfMemory;
        };

        const type_flags = self.dev.mem_props.memoryTypes[mem_type].propertyFlags;
        const coherent = (type_flags & vk.MemoryProperty.host_coherent_bit) != 0;
        const alignment: u64 = @max(req.alignment, 1);
        const size: u64 = @max(req.size, 1);

        const use_dedicated = dedicated_hint or size >= dedicated_threshold;
        if (use_dedicated) {
            return self.allocDedicated(size, mem_type, linear, map_memory, coherent, diag);
        }

        // Best-fit search across existing matching non-dedicated blocks.
        var best_block: ?usize = null;
        var best_region: usize = 0;
        var best_aligned: u64 = 0;
        var best_waste: u64 = std.math.maxInt(u64);

        for (self.blocks.items, 0..) |*slot, bi| {
            const blk = &(slot.* orelse continue);
            if (blk.dedicated or blk.memory_type_index != mem_type or blk.linear != linear) continue;
            if ((blk.mapped != null) != map_memory) continue;

            for (blk.free_regions.items, 0..) |reg, ri| {
                const aligned = alignUp(reg.offset, alignment);
                const reg_end = reg.offset + reg.size;
                if (aligned + size <= reg_end) {
                    const waste = reg_end - (aligned + size) + (aligned - reg.offset);
                    if (waste < best_waste) {
                        best_waste = waste;
                        best_block = bi;
                        best_region = ri;
                        best_aligned = aligned;
                        if (waste == 0) break;
                    }
                }
            }
            if (best_waste == 0) break;
        }

        if (best_block) |bi| {
            const blk = &(self.blocks.items[bi].?);
            try self.consumeFreeRegion(blk, best_region, best_aligned, size);
            return .{
                .block_index = bi,
                .memory = blk.memory,
                .offset = best_aligned,
                .size = size,
                .mapped = if (blk.mapped) |p| p + @as(usize, @intCast(best_aligned)) else null,
                .coherent = blk.coherent,
            };
        }

        // Allocate a new 64 MiB suballocation block (with smaller fallback if OOM).
        const block_idx = try self.allocSuballocBlock(size, mem_type, linear, map_memory, coherent, diag);
        const blk = &(self.blocks.items[block_idx].?);
        try self.consumeFreeRegion(blk, 0, 0, size);
        return .{
            .block_index = block_idx,
            .memory = blk.memory,
            .offset = 0,
            .size = size,
            .mapped = blk.mapped,
            .coherent = blk.coherent,
        };
    }

    fn insertBlock(self: *MemoryAllocator, blk: Block) Error!usize {
        for (self.blocks.items, 0..) |*slot, idx| {
            if (slot.* == null) {
                slot.* = blk;
                return idx;
            }
        }
        const idx = self.blocks.items.len;
        self.blocks.append(self.gpa, blk) catch return error.OutOfMemory;
        return idx;
    }

    fn allocDedicated(
        self: *MemoryAllocator,
        size: u64,
        mem_type: u32,
        linear: bool,
        map_memory: bool,
        coherent: bool,
        diag: *Diag,
    ) Error!Allocation {
        const ai = vk.MemoryAllocateInfo{
            .allocationSize = size,
            .memoryTypeIndex = mem_type,
        };
        var mem: vk.DeviceMemory = .null_handle;
        try loader_mod.check(
            self.dev.disp.vkAllocateMemory(self.dev.handle, &ai, null, &mem),
            diag,
            "vkAllocateMemory(dedicated)",
        );
        errdefer self.dev.disp.vkFreeMemory(self.dev.handle, mem, null);

        var mapped: ?[*]u8 = null;
        if (map_memory) {
            var raw: ?*anyopaque = null;
            try loader_mod.check(
                self.dev.disp.vkMapMemory(self.dev.handle, mem, 0, size, 0, &raw),
                diag,
                "vkMapMemory(dedicated)",
            );
            mapped = @ptrCast(raw orelse return error.OutOfMemory);
        }
        errdefer if (mapped != null) self.dev.disp.vkUnmapMemory(self.dev.handle, mem);

        const idx = try self.insertBlock(.{
            .memory = mem,
            .memory_type_index = mem_type,
            .size = size,
            .mapped = mapped,
            .linear = linear,
            .dedicated = true,
            .coherent = coherent,
        });

        return .{
            .block_index = idx,
            .memory = mem,
            .offset = 0,
            .size = size,
            .mapped = mapped,
            .coherent = coherent,
        };
    }

    fn allocSuballocBlock(
        self: *MemoryAllocator,
        min_size: u64,
        mem_type: u32,
        linear: bool,
        map_memory: bool,
        coherent: bool,
        diag: *Diag,
    ) Error!usize {
        var block_size = @max(default_block_size, min_size);
        var mem: vk.DeviceMemory = .null_handle;
        var res = self.dev.disp.vkAllocateMemory(self.dev.handle, &.{
            .allocationSize = block_size,
            .memoryTypeIndex = mem_type,
        }, null, &mem);
        if (res == .error_out_of_device_memory or res == .error_out_of_host_memory) {
            block_size = @max(min_size, fallback_block_min);
            res = self.dev.disp.vkAllocateMemory(self.dev.handle, &.{
                .allocationSize = block_size,
                .memoryTypeIndex = mem_type,
            }, null, &mem);
        }
        try loader_mod.check(res, diag, "vkAllocateMemory(block)");
        errdefer self.dev.disp.vkFreeMemory(self.dev.handle, mem, null);

        var mapped: ?[*]u8 = null;
        if (map_memory) {
            var raw: ?*anyopaque = null;
            try loader_mod.check(
                self.dev.disp.vkMapMemory(self.dev.handle, mem, 0, block_size, 0, &raw),
                diag,
                "vkMapMemory(block)",
            );
            mapped = @ptrCast(raw orelse return error.OutOfMemory);
        }
        errdefer if (mapped != null) self.dev.disp.vkUnmapMemory(self.dev.handle, mem);

        var free_regions: std.ArrayList(FreeRegion) = .empty;
        errdefer free_regions.deinit(self.gpa);
        free_regions.append(self.gpa, .{ .offset = 0, .size = block_size }) catch return error.OutOfMemory;

        return self.insertBlock(.{
            .memory = mem,
            .memory_type_index = mem_type,
            .size = block_size,
            .mapped = mapped,
            .linear = linear,
            .dedicated = false,
            .coherent = coherent,
            .free_regions = free_regions,
        });
    }

    fn consumeFreeRegion(self: *MemoryAllocator, blk: *Block, region_idx: usize, aligned: u64, size: u64) Error!void {
        const reg = blk.free_regions.items[region_idx];
        const prefix_size = aligned - reg.offset;
        const suffix_start = aligned + size;
        const reg_end = reg.offset + reg.size;
        const suffix_size = reg_end - suffix_start;

        if (prefix_size > 0 and suffix_size > 0) {
            blk.free_regions.items[region_idx] = .{ .offset = reg.offset, .size = prefix_size };
            blk.free_regions.insert(self.gpa, region_idx + 1, .{ .offset = suffix_start, .size = suffix_size }) catch return error.OutOfMemory;
        } else if (prefix_size > 0) {
            blk.free_regions.items[region_idx] = .{ .offset = reg.offset, .size = prefix_size };
        } else if (suffix_size > 0) {
            blk.free_regions.items[region_idx] = .{ .offset = suffix_start, .size = suffix_size };
        } else {
            _ = blk.free_regions.orderedRemove(region_idx);
        }
    }

    pub fn free(self: *MemoryAllocator, allocation: Allocation) void {
        if (allocation.memory == .null_handle) return;
        if (allocation.block_index >= self.blocks.items.len) return;
        const slot = &self.blocks.items[allocation.block_index];
        var blk = &(slot.* orelse return);

        if (blk.dedicated) {
            if (blk.mapped != null) {
                self.dev.disp.vkUnmapMemory(self.dev.handle, blk.memory);
            }
            self.dev.disp.vkFreeMemory(self.dev.handle, blk.memory, null);
            blk.free_regions.deinit(self.gpa);
            slot.* = null;
            return;
        }

        // Insert back in sorted offset order and coalesce adjacent regions.
        const items = blk.free_regions.items;
        var insert_at: usize = items.len;
        for (items, 0..) |reg, i| {
            if (allocation.offset < reg.offset) {
                insert_at = i;
                break;
            }
        }
        blk.free_regions.insert(self.gpa, insert_at, .{
            .offset = allocation.offset,
            .size = allocation.size,
        }) catch {
            // OOM inserting free region: memory will still be freed when the block is destroyed in deinit().
            return;
        };

        // Coalesce with next.
        if (insert_at + 1 < blk.free_regions.items.len) {
            const cur = &blk.free_regions.items[insert_at];
            const next = blk.free_regions.items[insert_at + 1];
            if (cur.offset + cur.size == next.offset) {
                cur.size += next.size;
                _ = blk.free_regions.orderedRemove(insert_at + 1);
            }
        }
        // Coalesce with prev.
        if (insert_at > 0) {
            const prev = &blk.free_regions.items[insert_at - 1];
            const cur = blk.free_regions.items[insert_at];
            if (prev.offset + prev.size == cur.offset) {
                prev.size += cur.size;
                _ = blk.free_regions.orderedRemove(insert_at);
            }
        }
    }
};
