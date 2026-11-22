const std = @import("std");
const Allocator = std.mem.Allocator;
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const sync = @import("../sync.zig");
const objc = @import("objc.zig");
const mtl = @import("mtl.zig");
const ca = @import("ca.zig");
const cvdisplaylink = @import("cvdisplaylink.zig");
const buffers = @import("buffers.zig");
const swapchain = @import("swapchain.zig");
const layer_surface = @import("layer_surface.zig");

const Error = errors.Error;
const Diag = errors.Diag;

/// Deferred present work item queued when `live_resizing && main_thread_mode == DEFERRED`.
pub const MainPresentItem = struct {
    surface_id: u32,
    cb: mtl.CommandBuffer,
    kind: union(enum) {
        swap_chain: struct {
            sc: *swapchain.SwapChain,
            tok: swapchain.PresentToken,
        },
        metal_drawable: ca.MetalDrawable,
    },

    pub fn execute(self: *MainPresentItem) void {
        self.cb.waitUntilCompleted();
        self.cb.release();
        switch (self.kind) {
            .swap_chain => |s| s.sc.finishShow(s.tok),
            .metal_drawable => |d| {
                d.present();
                ca.Transaction.flush();
                d.release();
            },
        }
    }

    pub fn cancel(self: *MainPresentItem) void {
        self.cb.release();
        switch (self.kind) {
            .swap_chain => |s| swapchain.SwapChain.cancelPresentToken(s.tok),
            .metal_drawable => |d| d.release(),
        }
    }
};

/// Presentation target bound to an NSView.
/// Always heap-allocated and pinned in memory (CVDisplayLink holds `&self.display_link.fence`).
pub const Surface = struct {
    id: u32,
    swap_chain: ?swapchain.SwapChain = null,
    layer_surf: ?layer_surface.LayerSurface = null,
    display_link: cvdisplaylink.DisplayLink = .{},

    drawable_width: u32 = 0,
    drawable_height: u32 = 0,

    // Atomic window state written from arbitrary goroutine/thread, snapshotted at beginBatch.
    window_live_resizing: std.atomic.Value(bool) = std.atomic.Value(bool).init(false),
    window_fullscreen: std.atomic.Value(bool) = std.atomic.Value(bool).init(false),
    window_hidden: std.atomic.Value(bool) = std.atomic.Value(bool).init(false),
    live_resizing: std.atomic.Value(bool) = std.atomic.Value(bool).init(false),

    // Current-frame render-thread state.
    drawable: ca.MetalDrawable = .{},
    drawable_tex: mtl.Texture = .{},
    chained: bool = false,
    cleared: bool = false,

    // Kept / damage state protected by `mu` (accessed from frame goroutine and render thread).
    mu: sync.Mutex = .{},
    last: mtl.Texture = .{},
    keep: bool = false,
    damage: std.ArrayList(abi.Rect) = .empty,
    damaged: bool = false,
    last_damage: std.ArrayList(abi.Rect) = .empty,
    has_last_damage: bool = false,

    pub fn init(
        self: *Surface,
        id: u32,
        device: mtl.Device,
        color_space: u32,
        ns_view: objc.Id,
        transparent: bool,
        no_iosurface: bool,
        diag: *Diag,
    ) Error!void {
        self.* = .{ .id = id };

        if (!no_iosurface) {
            if (swapchain.SwapChain.create(device, color_space, transparent)) |sc| {
                self.swap_chain = sc;
            }
        }
        if (self.swap_chain == null) {
            if (layer_surface.LayerSurface.create(device, color_space, transparent)) |ls| {
                self.layer_surf = ls;
            } else {
                diag.set("metal: failed to create CALayer/CAMetalLayer for surface");
                return error.Internal;
            }
        }

        self.display_link.init();

        const layer_id: objc.Id = if (self.swap_chain) |*sc|
            sc.layer.id
        else
            self.layer_surf.?.ml.id;
        ca.attachLayerToNSView(ns_view, layer_id);
    }

    pub fn deinit(self: *Surface, gpa: Allocator) void {
        self.mu.lock();
        self.forgetLastLocked();
        self.damage.deinit(gpa);
        self.last_damage.deinit(gpa);
        self.mu.unlock();
        self.mu.deinit();

        if (!self.drawable.isNull()) {
            self.drawable.release();
            self.drawable = .{};
            self.drawable_tex = .{};
        }
        self.chained = false;

        if (self.swap_chain) |*sc| {
            sc.release();
            self.swap_chain = null;
        }
        self.display_link.deinit();
        if (self.layer_surf) |*ls| {
            ls.release();
            self.layer_surf = null;
        }
    }

    pub fn setDrawableSize(self: *Surface, w: u32, h: u32) void {
        self.drawable_width = w;
        self.drawable_height = h;
        if (self.layer_surf) |*ls| {
            ls.setDrawableSize(w, h);
        }
    }

    pub fn updatePresentationState(
        self: *Surface,
        main_thread_mode: u32,
        ring: *buffers.ResourceRing,
    ) void {
        self.live_resizing.store(self.window_live_resizing.load(.acquire), .release);
        if (self.swap_chain != null) return;
        if (self.layer_surf) |*ls| {
            ls.updatePresentationState(self.window_fullscreen.load(.acquire), main_thread_mode, ring);
        }
    }

    pub fn setWindowState(self: *Surface, flags: u32) void {
        const live_resizing = (flags & abi.window_live_resizing) != 0;
        const fullscreen = (flags & abi.window_fullscreen) != 0;
        const hidden = (flags & abi.window_hidden) != 0;

        if (hidden) {
            self.mu.lock();
            self.forgetLastLocked();
            self.mu.unlock();
        }

        self.window_live_resizing.store(live_resizing, .release);
        self.window_fullscreen.store(fullscreen, .release);
        const was_hidden = self.window_hidden.swap(hidden, .acq_rel);
        if (!was_hidden and hidden) {
            if (self.swap_chain) |*sc| {
                sc.drop();
            } else if (self.layer_surf) |*ls| {
                ls.dropDrawables();
            }
        }
    }

    pub fn kept(self: *Surface) bool {
        self.mu.lock();
        defer self.mu.unlock();
        self.keep = !self.last.isNull();
        return self.keep;
    }

    pub fn setDamage(self: *Surface, gpa: Allocator, rects: ?[]align(4) const abi.Rect) Error!void {
        self.mu.lock();
        defer self.mu.unlock();
        if (rects) |rs| {
            self.damage.clearRetainingCapacity();
            self.damage.ensureTotalCapacity(gpa, rs.len) catch return error.OutOfMemory;
            for (rs) |r| self.damage.appendAssumeCapacity(r);
            self.damaged = true;
        } else {
            self.damage.clearRetainingCapacity();
            self.damaged = false;
        }
    }

    pub fn coversLocked(self: *const Surface, w: u32, h: u32) bool {
        if (!self.damaged) return false;
        const wi: i32 = @intCast(w);
        const hi: i32 = @intCast(h);
        for (self.damage.items) |r| {
            if (r.x0 <= 0 and r.y0 <= 0 and r.x1 >= wi and r.y1 >= hi) {
                return true;
            }
        }
        return false;
    }

    /// Copies the last presented texture (or only `last_damage` when `previous` is true) into `tex`.
    /// Returns true if `tex` now holds valid kept contents and does NOT need clearing.
    pub fn startFrom(self: *Surface, tex: mtl.Texture, previous: bool, backend_ctx: anytype) Error!bool {
        self.mu.lock();
        defer self.mu.unlock();

        if (!self.keep or self.last.isNull()) return false;

        const w = tex.width();
        const h = tex.height();
        if (self.last.width() != w or self.last.height() != h) {
            self.forgetLastLocked();
            return false;
        }
        if (self.coversLocked(w, h)) {
            return true;
        }

        const full_rect = [1]abi.Rect{.{
            .x0 = 0,
            .y0 = 0,
            .x1 = @intCast(w),
            .y1 = @intCast(h),
        }};
        const copies: []const abi.Rect = if (previous and self.has_last_damage)
            self.last_damage.items
        else
            &full_rect;

        if (copies.len > 0) {
            backend_ctx.flushRenderCommandEncoderIfNeeded();
            const cb = try backend_ctx.encoderCommandBuffer();
            const bce = cb.blitCommandEncoder();
            const wi: i32 = @intCast(w);
            const hi: i32 = @intCast(h);
            for (copies) |raw| {
                const x0 = @max(raw.x0, 0);
                const y0 = @max(raw.y0, 0);
                const x1 = @min(raw.x1, wi);
                const y1 = @min(raw.y1, hi);
                if (x1 <= x0 or y1 <= y0) continue;
                const origin = mtl.Origin{
                    .x = @intCast(x0),
                    .y = @intCast(y0),
                    .z = 0,
                };
                const size = mtl.Size{
                    .width = @intCast(x1 - x0),
                    .height = @intCast(y1 - y0),
                    .depth = 1,
                };
                bce.copyFromTexture(self.last, 0, 0, origin, size, tex, 0, 0, origin);
            }
            bce.endEncoding();
        }
        return true;
    }

    pub fn presented(self: *Surface, gpa: Allocator, tex: mtl.Texture) void {
        self.mu.lock();
        defer self.mu.unlock();
        tex.retain();
        if (!self.last.isNull()) {
            self.last.release();
        }
        self.last = tex;

        const w = tex.width();
        const h = tex.height();
        if (self.damaged and self.damage.items.len > 0 and !self.coversLocked(w, h)) {
            self.last_damage.clearRetainingCapacity();
            self.last_damage.appendSlice(gpa, self.damage.items) catch {
                self.has_last_damage = false;
                self.damage.clearRetainingCapacity();
                self.damaged = false;
                return;
            };
            self.has_last_damage = true;
        } else {
            self.last_damage.clearRetainingCapacity();
            self.has_last_damage = false;
        }
        self.damage.clearRetainingCapacity();
        self.damaged = false;
    }

    pub fn forgetLastLocked(self: *Surface) void {
        self.last_damage.clearRetainingCapacity();
        self.has_last_damage = false;
        if (!self.last.isNull()) {
            self.last.release();
            self.last = .{};
        }
    }

    pub fn acquireTexture(self: *Surface, main_thread_mode: u32, backend_ctx: anytype) Error!mtl.Texture {
        if (self.swap_chain) |*sc| {
            if (!self.chained) {
                self.display_link.waitForRefresh();
                const acq = sc.acquire(self.drawable_width, self.drawable_height);
                if (acq.tex.isNull()) return .{};
                self.chained = true;
                self.drawable_tex = acq.tex;
                self.cleared = try self.startFrom(acq.tex, acq.previous, backend_ctx);
                backend_ctx.gcBuffers();
            }
            return self.drawable_tex;
        }
        if (self.layer_surf) |*ls| {
            if (self.drawable.isNull()) {
                ls.applyDrawableSizeIfNeeded(self.live_resizing.load(.acquire), main_thread_mode);
                self.display_link.waitForRefresh();
                const d = ls.nextDrawable();
                if (d.isNull()) return .{};
                // Retain across intermediate flushes that drain the autorelease pool (#3704).
                d.retain();
                self.drawable = d;
                self.drawable_tex = d.texture();
                self.cleared = try self.startFrom(self.drawable_tex, false, backend_ctx);
                backend_ctx.gcBuffers();
            }
            return self.drawable_tex;
        }
        return .{};
    }

    pub fn tickHousekeeping(self: *Surface, now_ns: u64) void {
        if (self.swap_chain) |*sc| {
            sc.tickIdle(now_ns);
        }
        self.display_link.tickIdle(now_ns);
    }
};
