const objc = @import("objc.zig");
const cf = @import("cf.zig");
const mtl = @import("mtl.zig");

const Id = objc.Id;
const Class = objc.Class;
const Sel = objc.Sel;

pub const ns_view_layer_contents_redraw_never: isize = 0;

/// Attaches a CALayer (or CAMetalLayer) to an NSView on the main thread.
pub fn attachLayerToNSView(ns_view: Id, layer: Id) void {
    if (ns_view == null or layer == null) return;
    const rt = objc.get();
    objc.send(*const fn (Id, Sel, Id) callconv(.c) void)(ns_view, rt.sels.setLayer, layer);
    objc.send(*const fn (Id, Sel, u8) callconv(.c) void)(ns_view, rt.sels.setWantsLayer, 1);
    objc.send(*const fn (Id, Sel, isize) callconv(.c) void)(
        ns_view,
        rt.sels.setLayerContentsRedrawPolicy,
        ns_view_layer_contents_redraw_never,
    );
}

/// Core Animation explicit/implicit transaction helpers.
pub const Transaction = struct {
    pub inline fn begin() void {
        const rt = objc.get();
        objc.send(*const fn (Class, Sel) callconv(.c) void)(rt.classes.CATransaction, rt.sels.begin);
    }

    pub inline fn setDisableActions(disable: bool) void {
        const rt = objc.get();
        objc.send(*const fn (Class, Sel, u8) callconv(.c) void)(
            rt.classes.CATransaction,
            rt.sels.setDisableActions,
            if (disable) 1 else 0,
        );
    }

    pub inline fn commit() void {
        const rt = objc.get();
        objc.send(*const fn (Class, Sel) callconv(.c) void)(rt.classes.CATransaction, rt.sels.commit);
    }

    pub inline fn flush() void {
        const rt = objc.get();
        objc.send(*const fn (Class, Sel) callconv(.c) void)(rt.classes.CATransaction, rt.sels.flush);
    }
};

pub const Layer = struct {
    id: Id = null,

    /// Creates a +1 retained CALayer (`[CALayer layer]` + retain).
    pub fn create() ?Layer {
        const rt = objc.get();
        if (rt.classes.CALayer == null) return null;
        const l = objc.send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.CALayer, rt.sels.layer);
        if (l == null) return null;
        _ = objc.retain(l);
        return .{ .id = l };
    }

    pub inline fn retain(self: Layer) void {
        _ = objc.retain(self.id);
    }

    pub inline fn release(self: Layer) void {
        objc.release(self.id);
    }

    pub inline fn setOpaque(self: Layer, opaque_flag: bool) void {
        if (self.id == null) return;
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, u8) callconv(.c) void)(
            self.id,
            rt.sels.setOpaque,
            if (opaque_flag) 1 else 0,
        );
    }

    pub inline fn setContents(self: Layer, contents: ?*anyopaque) void {
        if (self.id == null) return;
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, ?*anyopaque) callconv(.c) void)(
            self.id,
            rt.sels.setContents,
            contents,
        );
    }
};

pub const MetalDrawable = struct {
    id: Id = null,

    pub inline fn isNull(self: MetalDrawable) bool {
        return self.id == null;
    }

    pub inline fn retain(self: MetalDrawable) void {
        _ = objc.retain(self.id);
    }

    pub inline fn release(self: MetalDrawable) void {
        objc.release(self.id);
    }

    pub inline fn texture(self: MetalDrawable) mtl.Texture {
        if (self.id == null) return .{};
        const rt = objc.get();
        return .{ .id = objc.send(*const fn (Id, Sel) callconv(.c) Id)(self.id, rt.sels.texture) };
    }

    pub inline fn present(self: MetalDrawable) void {
        if (self.id == null) return;
        const rt = objc.get();
        objc.send(*const fn (Id, Sel) callconv(.c) void)(self.id, rt.sels.present);
    }
};

pub const MetalLayer = struct {
    id: Id = null,

    /// Creates a +1 retained CAMetalLayer configured with color_space (2 = DisplayP3, else sRGB).
    pub fn create(color_space: u32) ?MetalLayer {
        const rt = objc.get();
        if (rt.classes.CAMetalLayer == null) return null;
        const layer = objc.send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.CAMetalLayer, rt.sels.new);
        if (layer == null) return null;

        if (cf.createCGColorSpace(color_space)) |cs| {
            objc.send(*const fn (Id, Sel, cf.CGColorSpaceRef) callconv(.c) void)(layer, rt.sels.setColorspace, cs);
            cf.releaseCGColorSpace(cs);
        }
        return .{ .id = layer };
    }

    pub inline fn release(self: MetalLayer) void {
        objc.release(self.id);
    }

    pub inline fn setDevice(self: MetalLayer, device: mtl.Device) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, Id) callconv(.c) void)(self.id, rt.sels.setDevice, device.id);
    }

    pub inline fn setOpaque(self: MetalLayer, opaque_flag: bool) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, u8) callconv(.c) void)(
            self.id,
            rt.sels.setOpaque,
            if (opaque_flag) 1 else 0,
        );
    }

    pub inline fn setPixelFormat(self: MetalLayer, pf: mtl.PixelFormat) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(self.id, rt.sels.setPixelFormat, @intFromEnum(pf));
    }

    pub inline fn setMaximumDrawableCount(self: MetalLayer, count: usize) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, usize) callconv(.c) void)(self.id, rt.sels.setMaximumDrawableCount, count);
    }

    pub inline fn setDisplaySyncEnabled(self: MetalLayer, enabled: bool) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, u8) callconv(.c) void)(
            self.id,
            rt.sels.setDisplaySyncEnabled,
            if (enabled) 1 else 0,
        );
    }

    pub inline fn setFramebufferOnly(self: MetalLayer, fb_only: bool) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, u8) callconv(.c) void)(
            self.id,
            rt.sels.setFramebufferOnly,
            if (fb_only) 1 else 0,
        );
    }

    pub inline fn setPresentsWithTransaction(self: MetalLayer, enabled: bool) void {
        const rt = objc.get();
        objc.send(*const fn (Id, Sel, u8) callconv(.c) void)(
            self.id,
            rt.sels.setPresentsWithTransaction,
            if (enabled) 1 else 0,
        );
    }

    pub inline fn setDrawableSize(self: MetalLayer, width: u32, height: u32) void {
        const rt = objc.get();
        const sz = objc.CGSize{
            .width = @floatFromInt(width),
            .height = @floatFromInt(height),
        };
        objc.send(*const fn (Id, Sel, objc.CGSize) callconv(.c) void)(self.id, rt.sels.setDrawableSize, sz);
    }

    /// Returns an autoreleased CAMetalDrawable (or null if unavailable).
    pub inline fn nextDrawable(self: MetalLayer) MetalDrawable {
        const rt = objc.get();
        return .{ .id = objc.send(*const fn (Id, Sel) callconv(.c) Id)(self.id, rt.sels.nextDrawable) };
    }
};
