const std = @import("std");
const Allocator = std.mem.Allocator;
const mtl = @import("mtl.zig");
const errors = @import("../errors.zig");
const Error = errors.Error;

/// Alignment of sub-allocations in the upload staging buffer (16 bytes).
pub const upload_alignment: usize = 16;

/// Minimum staging buffer size for frame uploads (256 KiB).
pub const min_upload_buffer_bytes: usize = 256 << 10;

/// How many frames a pooled buffer may go unused before eviction.
pub const max_idle_buffer_frames: u64 = 120;

/// Maximum total bytes retained across unused pooled buffers (64 MiB).
pub const max_pooled_buffer_bytes: usize = 64 << 20;

/// Maximum number of buffers kept in the pool.
pub const max_pooled_buffers: usize = 256;

/// Maximum factor above requested power-of-two size a reused buffer may have.
pub const max_buffer_waste: usize = 4;

/// Minimum upper bound for reuse matching (64 KiB).
pub const min_reused_buffer_bytes: usize = 64 << 10;

pub inline fn alignUploadOffset(n: usize) usize {
    return (n + upload_alignment - 1) & ~@as(usize, upload_alignment - 1);
}

pub inline fn nextPowerOf2(n: usize) usize {
    if (n <= 1) return 1;
    return std.math.ceilPowerOfTwo(usize, n) catch n;
}

pub const PooledBuffer = struct {
    buf: mtl.Buffer,
    frame: u64,
};

/// Power-of-two Metal buffer pool matching Go `bufferpool_darwin.go`.
pub const BufferPool = struct {
    bufs: std.ArrayList(PooledBuffer) = .empty,
    bytes: usize = 0,

    pub fn deinit(self: *BufferPool, gpa: Allocator) void {
        for (self.bufs.items) |entry| {
            entry.buf.release();
        }
        self.bufs.deinit(gpa);
        self.bytes = 0;
    }

    pub fn put(self: *BufferPool, gpa: Allocator, buf: mtl.Buffer, frame: u64) void {
        if (buf.isNull()) return;
        self.bufs.append(gpa, .{ .buf = buf, .frame = frame }) catch {
            // OOM while pooling: release immediately rather than leaking the Metal buffer.
            buf.release();
            return;
        };
        self.bytes += buf.length;
    }

    pub fn get(self: *BufferPool, length: usize) ?mtl.Buffer {
        const pow2 = nextPowerOf2(length);
        const scaled = std.math.mul(usize, pow2, max_buffer_waste) catch std.math.maxInt(usize);
        const limit = @max(scaled, min_reused_buffer_bytes);

        var best: ?usize = null;
        for (self.bufs.items, 0..) |b, i| {
            if (b.buf.length < length or b.buf.length > limit) continue;
            if (best == null or b.buf.length < self.bufs.items[best.?].buf.length) {
                best = i;
            }
        }
        const idx = best orelse return null;
        const chosen = self.bufs.items[idx].buf;
        self.removeSwap(idx);
        return chosen;
    }

    pub fn evict(self: *BufferPool, frame: u64) void {
        var newest: u64 = 0;
        var i: usize = 0;
        while (i < self.bufs.items.len) {
            const b = self.bufs.items[i];
            if (frame > b.frame and (frame - b.frame) > max_idle_buffer_frames) {
                b.buf.release();
                self.removeSwap(i);
                continue;
            }
            if (b.frame > newest) newest = b.frame;
            i += 1;
        }

        while (self.bytes > max_pooled_buffer_bytes or self.bufs.items.len > max_pooled_buffers) {
            var victim: ?usize = null;
            for (self.bufs.items, 0..) |b, idx| {
                if (b.frame == newest) continue;
                if (victim == null) {
                    victim = idx;
                    continue;
                }
                const v = self.bufs.items[victim.?];
                if (b.frame < v.frame or (b.frame == v.frame and b.buf.length > v.buf.length)) {
                    victim = idx;
                }
            }
            const vi = victim orelse break;
            self.bufs.items[vi].buf.release();
            self.removeSwap(vi);
        }
    }

    fn removeSwap(self: *BufferPool, idx: usize) void {
        self.bytes -= self.bufs.items[idx].buf.length;
        _ = self.bufs.swapRemove(idx);
    }
};

/// Command buffers and transient GPU buffers owned by a single in-flight frame.
pub const FrameResources = struct {
    frame: u64 = 0,
    cbs: std.ArrayList(mtl.CommandBuffer) = .empty,
    buffers: std.ArrayList(mtl.Buffer) = .empty,

    pub fn deinit(self: *FrameResources, gpa: Allocator) void {
        for (self.cbs.items) |cb| cb.release();
        for (self.buffers.items) |b| b.release();
        self.cbs.deinit(gpa);
        self.buffers.deinit(gpa);
    }

    pub fn clearKeepingCapacity(self: *FrameResources) void {
        self.cbs.clearRetainingCapacity();
        self.buffers.clearRetainingCapacity();
    }
};

/// Manages per-frame command buffers, transient buffer pooling, and upload staging.
pub const ResourceRing = struct {
    device: mtl.Device,
    frame: u64 = 0,
    in_flight: std.ArrayList(*FrameResources) = .empty,
    spare_frames: std.ArrayList(*FrameResources) = .empty,
    unused_buffers: BufferPool = .{},

    upload_buffer: mtl.Buffer = .{},
    upload_frame: u64 = 0,
    upload_offset: usize = 0,

    pub fn init(device: mtl.Device) ResourceRing {
        return .{ .device = device };
    }

    pub fn deinit(self: *ResourceRing, gpa: Allocator) void {
        // Wait for all committed in-flight command buffers before releasing resources.
        self.waitAllInFlight();
        for (self.in_flight.items) |r| {
            r.deinit(gpa);
            gpa.destroy(r);
        }
        self.in_flight.deinit(gpa);

        for (self.spare_frames.items) |r| {
            r.deinit(gpa);
            gpa.destroy(r);
        }
        self.spare_frames.deinit(gpa);

        self.unused_buffers.deinit(gpa);
        self.upload_buffer = .{};
        self.upload_offset = 0;
    }

    pub fn currentFrame(self: *ResourceRing, gpa: Allocator) Error!*FrameResources {
        const len = self.in_flight.items.len;
        if (len > 0 and self.in_flight.items[len - 1].frame == self.frame) {
            return self.in_flight.items[len - 1];
        }
        const rec: *FrameResources = if (self.spare_frames.items.len > 0)
            self.spare_frames.pop().?
        else blk: {
            const r = gpa.create(FrameResources) catch return error.OutOfMemory;
            r.* = .{};
            break :blk r;
        };
        rec.frame = self.frame;
        self.in_flight.append(gpa, rec) catch {
            self.spare_frames.append(gpa, rec) catch {
                rec.deinit(gpa);
                gpa.destroy(rec);
            };
            return error.OutOfMemory;
        };
        return rec;
    }

    pub fn trackCommandBuffer(self: *ResourceRing, gpa: Allocator, cb: mtl.CommandBuffer) Error!void {
        const rec = try self.currentFrame(gpa);
        rec.cbs.append(gpa, cb) catch return error.OutOfMemory;
        cb.retain();
    }

    pub fn untrackCommandBuffer(self: *ResourceRing, cb: mtl.CommandBuffer) void {
        if (cb.isNull()) return;
        for (self.in_flight.items) |r| {
            var i: usize = r.cbs.items.len;
            while (i > 0) {
                i -= 1;
                if (r.cbs.items[i].id == cb.id) {
                    const removed = r.cbs.orderedRemove(i);
                    removed.release();
                    return;
                }
            }
        }
    }

    pub fn acquireBuffer(self: *ResourceRing, gpa: Allocator, length: usize) Error!mtl.Buffer {
        const req = if (length == 0) 16 else length;
        const buf: mtl.Buffer = self.unused_buffers.get(req) orelse blk: {
            const alloc_len = nextPowerOf2(req);
            const b = self.device.newBufferWithLength(alloc_len, self.device.bufferResourceOptions());
            if (b.isNull()) return error.OutOfMemory;
            break :blk b;
        };
        const rec = try self.currentFrame(gpa);
        rec.buffers.append(gpa, buf) catch {
            buf.release();
            return error.OutOfMemory;
        };
        return buf;
    }

    pub const UploadAlloc = struct {
        buf: mtl.Buffer,
        offset: usize,
    };

    /// Sub-allocates `length` bytes (already 16B aligned) from the current frame's upload buffer.
    pub fn uploadSpace(self: *ResourceRing, gpa: Allocator, length: usize) Error!UploadAlloc {
        const aligned_len = alignUploadOffset(length);
        if (!self.upload_buffer.isNull() and
            self.upload_frame == self.frame and
            self.upload_offset + aligned_len <= self.upload_buffer.length)
        {
            const off = self.upload_offset;
            self.upload_offset += aligned_len;
            return .{ .buf = self.upload_buffer, .offset = off };
        }
        const alloc_len = @max(aligned_len, min_upload_buffer_bytes);
        const buf = try self.acquireBuffer(gpa, alloc_len);
        self.upload_buffer = buf;
        self.upload_frame = self.frame;
        self.upload_offset = aligned_len;
        return .{ .buf = buf, .offset = 0 };
    }

    pub fn waitAllInFlight(self: *ResourceRing) void {
        for (self.in_flight.items) |r| {
            for (r.cbs.items) |cb| {
                const st = cb.status();
                const raw = @intFromEnum(st);
                // Only wait if committed/scheduled (not_enqueued would hang forever).
                if (raw >= @intFromEnum(mtl.CommandBufferStatus.committed) and
                    raw < @intFromEnum(mtl.CommandBufferStatus.completed))
                {
                    cb.waitUntilCompleted();
                }
            }
        }
    }

    /// Reclaims buffers from completed past frames and evicts old pooled buffers.
    pub fn gc(self: *ResourceRing, gpa: Allocator) void {
        var keep_idx: usize = 0;
        for (self.in_flight.items) |r| {
            if (r.frame == self.frame) {
                self.in_flight.items[keep_idx] = r;
                keep_idx += 1;
                continue;
            }
            var all_done = true;
            for (r.cbs.items) |cb| {
                const raw = @intFromEnum(cb.status());
                if (raw >= @intFromEnum(mtl.CommandBufferStatus.committed) and
                    raw < @intFromEnum(mtl.CommandBufferStatus.completed))
                {
                    all_done = false;
                    break;
                }
            }
            if (!all_done) {
                self.in_flight.items[keep_idx] = r;
                keep_idx += 1;
                continue;
            }
            for (r.cbs.items) |cb| cb.release();
            for (r.buffers.items) |b| {
                self.unused_buffers.put(gpa, b, r.frame);
            }
            r.clearKeepingCapacity();
            self.spare_frames.append(gpa, r) catch {
                r.deinit(gpa);
                gpa.destroy(r);
            };
        }
        self.in_flight.items.len = keep_idx;
        self.unused_buffers.evict(self.frame);
    }
};
