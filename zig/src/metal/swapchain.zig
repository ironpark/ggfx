const std = @import("std");
const sync = @import("../sync.zig");
const time = @import("../time.zig");
const mtl = @import("mtl.zig");
const ca = @import("ca.zig");
const cf = @import("cf.zig");
const iosurface = @import("iosurface.zig");

/// How long after the last present before the non-front IOSurface is marked purgeable.
pub const swap_idle_ns: u64 = 250 * std.time.ns_per_ms;

/// Maximum time acquire waits for the window server to finish reading the back IOSurface.
pub const most_in_use_wait_ns: u64 = 20 * std.time.ns_per_ms;
pub const in_use_poll_ns: u64 = 250 * std.time.ns_per_us;

pub const SwapBuffer = struct {
    surface: iosurface.IOSurfaceRef = null,
    tex: mtl.Texture = .{},
    volatile_flag: bool = false,
    shown: bool = false,
};

pub const AcquireResult = struct {
    tex: mtl.Texture = .{},
    previous: bool = false,
};

pub const PresentToken = struct {
    surface: iosurface.IOSurfaceRef,
    layer: ca.Layer,
    buf_idx: usize,
};

/// Two-buffer IOSurface swapchain matching Go `swapchain_macos.go`.
pub const SwapChain = struct {
    layer: ca.Layer,
    device: mtl.Device,
    color_space: u32,

    mu: sync.Mutex = .{},
    width: u32 = 0,
    height: u32 = 0,
    bufs: [2]SwapBuffer = .{ .{}, .{} },
    front: i32 = -1,
    drawing: i32 = -1,
    dropped: bool = false,
    released: bool = false,
    has_presented: bool = false,
    last_present_ns: u64 = 0,

    pub fn create(device: mtl.Device, color_space: u32, transparent: bool) ?SwapChain {
        if (iosurface.ensureLoaded() == null) return null;
        const layer = ca.Layer.create() orelse return null;
        layer.setOpaque(!transparent);
        return .{
            .layer = layer,
            .device = device,
            .color_space = color_space,
        };
    }

    pub fn setOpaque(self: *SwapChain, opaque_flag: bool) void {
        self.layer.setOpaque(opaque_flag);
    }

    fn newBuffer(self: *SwapChain, w: u32, h: u32) ?SwapBuffer {
        const surf = iosurface.createBGRA(w, h, self.color_space) orelse return null;
        const tex = self.device.newTextureWithIOSurface(.{
            .texture_type = .type_2d,
            .pixel_format = .bgra8_unorm,
            .width = w,
            .height = h,
            .storage_mode = self.device.textureStorageMode(),
            .usage = mtl.texture_usage_render_target | mtl.texture_usage_shader_read,
        }, surf, 0);
        if (tex.isNull()) {
            cf.release(surf);
            return null;
        }
        return .{
            .surface = surf,
            .tex = tex,
            .volatile_flag = false,
            .shown = false,
        };
    }

    fn freeLocked(self: *SwapChain, idx: usize) void {
        const b = &self.bufs[idx];
        if (b.surface == null) return;
        b.tex.release();
        cf.release(b.surface);
        b.* = .{};
    }

    pub fn acquire(self: *SwapChain, w: u32, h: u32) AcquireResult {
        self.mu.lock();
        if (self.released or w == 0 or h == 0) {
            self.mu.unlock();
            return .{};
        }
        self.dropped = false;
        if (self.width != w or self.height != h) {
            self.freeLocked(0);
            self.freeLocked(1);
            self.width = w;
            self.height = h;
            self.front = -1;
        }
        const i: usize = if (self.front == 0) 1 else 0;
        const b = &self.bufs[i];
        if (b.surface == null) {
            const nb = self.newBuffer(w, h) orelse {
                self.mu.unlock();
                return .{};
            };
            b.* = nb;
        }
        if (b.volatile_flag) {
            var old_state: u32 = 0;
            _ = iosurface.setPurgeable(b.surface, iosurface.purgeable_non_volatile, &old_state);
            b.volatile_flag = false;
            if (old_state == iosurface.purgeable_empty) {
                b.shown = false;
            }
        }
        const previous = b.shown and self.front >= 0;
        b.shown = false;
        const surf = b.surface;
        const tex = b.tex;
        self.drawing = @intCast(i);
        _ = cf.retain(surf);
        self.mu.unlock();

        // Wait for the window server without holding self.mu so drop() on main thread does not stall.
        const start = time.monotonicNs();
        while (time.monotonicNs() - start < most_in_use_wait_ns) {
            if (!iosurface.isInUse(surf)) break;
            time.sleepNs(in_use_poll_ns);
        }
        cf.release(surf);

        self.mu.lock();
        defer self.mu.unlock();
        if (self.released or self.drawing != @as(i32, @intCast(i)) or self.bufs[i].surface != surf) {
            return .{};
        }
        return .{ .tex = tex, .previous = previous };
    }

    /// Commits buffer ownership transition from `drawing` to `front` and returns a retained token
    /// for showing via CATransaction (either immediately, via dispatch, or deferred to main thread).
    pub fn beginPresent(self: *SwapChain) ?PresentToken {
        self.mu.lock();
        defer self.mu.unlock();
        const i_signed = self.drawing;
        self.drawing = -1;
        if (self.released or i_signed < 0) return null;
        const i: usize = @intCast(i_signed);
        const surf = self.bufs[i].surface;
        if (surf == null) return null;

        _ = cf.retain(surf);
        self.layer.retain();
        self.bufs[i].shown = true;
        self.front = i_signed;
        self.has_presented = true;
        self.last_present_ns = time.monotonicNs();

        return .{
            .surface = surf,
            .layer = self.layer,
            .buf_idx = i,
        };
    }

    /// Presents `tok` on the CALayer inside a disabled-actions CATransaction, then releases `tok`.
    pub fn finishShow(self: *SwapChain, tok: PresentToken) void {
        defer {
            cf.release(tok.surface);
            tok.layer.release();
        }
        self.mu.lock();
        const valid = !self.released and
            !self.dropped and
            self.layer.id == tok.layer.id and
            self.front == @as(i32, @intCast(tok.buf_idx)) and
            self.bufs[tok.buf_idx].surface == tok.surface;
        if (valid) {
            ca.Transaction.begin();
            ca.Transaction.setDisableActions(true);
            tok.layer.setContents(tok.surface);
            ca.Transaction.commit();
        }
        self.mu.unlock();
        if (valid) {
            ca.Transaction.flush();
        }
    }

    /// Releases a PresentToken without presenting (e.g., when surface is disposed before drain).
    pub fn cancelPresentToken(tok: PresentToken) void {
        cf.release(tok.surface);
        tok.layer.release();
    }

    /// Marks the non-front IOSurface purgeable once idle for >= swap_idle_ns.
    /// Returns true if a non-front buffer is still waiting for the idle timeout.
    pub fn tickIdle(self: *SwapChain, now_ns: u64) bool {
        self.mu.lock();
        defer self.mu.unlock();
        if (self.released or !self.has_presented) return false;

        var has_non_volatile = false;
        for (&self.bufs, 0..) |*b, idx| {
            const i: i32 = @intCast(idx);
            if (i == self.front or i == self.drawing or b.surface == null or b.volatile_flag) {
                continue;
            }
            has_non_volatile = true;
            break;
        }
        if (!has_non_volatile) return false;
        if (now_ns < self.last_present_ns or (now_ns - self.last_present_ns) < swap_idle_ns) {
            return true;
        }

        for (&self.bufs, 0..) |*b, idx| {
            const i: i32 = @intCast(idx);
            if (i == self.front or i == self.drawing or b.surface == null or b.volatile_flag) {
                continue;
            }
            var old_state: u32 = 0;
            _ = iosurface.setPurgeable(b.surface, iosurface.purgeable_volatile, &old_state);
            b.volatile_flag = true;
        }
        return false;
    }

    /// Drops both buffers and clears layer contents when the window is hidden.
    pub fn drop(self: *SwapChain) void {
        self.mu.lock();
        defer self.mu.unlock();
        if (self.released or self.drawing >= 0) return;
        self.dropped = true;
        if (self.layer.id != null) {
            self.layer.setContents(null);
        }
        self.freeLocked(0);
        self.freeLocked(1);
        self.front = -1;
    }

    pub fn release(self: *SwapChain) void {
        self.mu.lock();
        self.released = true;
        self.freeLocked(0);
        self.freeLocked(1);
        self.front = -1;
        self.drawing = -1;
        const l = self.layer;
        self.layer = .{};
        self.mu.unlock();

        if (l.id != null) {
            l.release();
        }
        self.mu.deinit();
    }
};
