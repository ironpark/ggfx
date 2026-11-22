const std = @import("std");
const abi = @import("../abi.zig");
const objc = @import("objc.zig");
const mtl = @import("mtl.zig");
const ca = @import("ca.zig");
const buffers = @import("buffers.zig");

pub const maximum_drawable_count: usize = 2;

/// CAMetalLayer fallback surface used when GGFX_DEVICE_METAL_NO_IOSURFACE is set
/// or when IOSurface swapchain creation fails.
pub const LayerSurface = struct {
    ml: ca.MetalLayer,
    drawable_width: u32 = 0,
    drawable_height: u32 = 0,
    drawable_size_dirty: bool = false,
    drawables_dropped: std.atomic.Value(bool) = std.atomic.Value(bool).init(false),
    presents_with_transaction: bool = false,

    pub fn create(device: mtl.Device, color_space: u32, transparent: bool) ?LayerSurface {
        const ml = ca.MetalLayer.create(color_space) orelse return null;
        ml.setDevice(device);
        ml.setPixelFormat(.bgra8_unorm);
        ml.setDisplaySyncEnabled(true);
        ml.setFramebufferOnly(false);
        ml.setPresentsWithTransaction(false);
        ml.setMaximumDrawableCount(maximum_drawable_count);
        ml.setOpaque(!transparent);
        return .{ .ml = ml };
    }

    pub fn release(self: *LayerSurface) void {
        self.ml.release();
        self.ml = .{};
    }

    pub fn setDrawableSize(self: *LayerSurface, w: u32, h: u32) void {
        if (self.drawable_width == w and self.drawable_height == h) return;
        self.drawable_width = w;
        self.drawable_height = h;
        self.drawable_size_dirty = true;
    }

    pub fn dropDrawables(self: *LayerSurface) void {
        self.ml.setDrawableSize(1, 1);
        self.drawables_dropped.store(true, .release);
    }

    const SetTxCtx = struct {
        ml: ca.MetalLayer,
        enabled: bool,
    };

    fn setTxOnMain(raw: ?*anyopaque) callconv(.c) void {
        const ctx: *SetTxCtx = @ptrCast(@alignCast(raw.?));
        ctx.ml.setPresentsWithTransaction(ctx.enabled);
    }

    pub fn updatePresentationState(
        self: *LayerSurface,
        fullscreen: bool,
        main_thread_mode: u32,
        ring: *buffers.ResourceRing,
    ) void {
        const want = !fullscreen;
        if (self.presents_with_transaction == want) return;

        if (want) {
            // Wait for any in-flight command buffers before switching to presentsWithTransaction=true.
            ring.waitAllInFlight();
        }

        if (main_thread_mode == @intFromEnum(abi.MainThreadMode.dispatch)) {
            var ctx = SetTxCtx{ .ml = self.ml, .enabled = want };
            objc.dispatchSyncMain(&ctx, setTxOnMain);
        } else {
            // In DEFERRED or INLINE mode, never call GCD dispatch_sync_f to avoid deadlock.
            self.ml.setPresentsWithTransaction(want);
        }
        self.presents_with_transaction = want;
    }

    const SetSizeCtx = struct {
        ml: ca.MetalLayer,
        w: u32,
        h: u32,
    };

    fn setSizeOnMain(raw: ?*anyopaque) callconv(.c) void {
        const ctx: *SetSizeCtx = @ptrCast(@alignCast(raw.?));
        ctx.ml.setDrawableSize(ctx.w, ctx.h);
    }

    pub fn applyDrawableSizeIfNeeded(self: *LayerSurface, live_resizing: bool, main_thread_mode: u32) void {
        const dropped = self.drawables_dropped.swap(false, .acq_rel);
        if (!dropped and !self.drawable_size_dirty) return;
        self.drawable_size_dirty = false;

        if (self.drawable_width == 0 or self.drawable_height == 0) return;

        if (live_resizing and main_thread_mode == @intFromEnum(abi.MainThreadMode.dispatch)) {
            var ctx = SetSizeCtx{
                .ml = self.ml,
                .w = self.drawable_width,
                .h = self.drawable_height,
            };
            objc.dispatchSyncMain(&ctx, setSizeOnMain);
        } else {
            // Do not call ca.Transaction.flush() before drawing in DEFERRED mode (prevents #3478 blanking).
            self.ml.setDrawableSize(self.drawable_width, self.drawable_height);
        }
    }

    pub fn shouldPresentWithTransaction(self: *const LayerSurface) bool {
        return self.presents_with_transaction;
    }

    pub fn nextDrawable(self: *LayerSurface) ca.MetalDrawable {
        return self.ml.nextDrawable();
    }
};
