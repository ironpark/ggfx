const std = @import("std");
const dynlib = @import("../dynlib.zig");
const sync = @import("../sync.zig");
const cf = @import("cf.zig");

pub const IOSurfaceRef = ?*anyopaque;

pub const purgeable_non_volatile: u32 = 0;
pub const purgeable_volatile: u32 = 1;
pub const purgeable_empty: u32 = 2;

pub const pixel_format_bgra: i32 = 0x42475241; // 'BGRA'

const IOSurfaceCreateFn = *const fn (cf.CFMutableDictionaryRef) callconv(.c) IOSurfaceRef;
const IOSurfaceIsInUseFn = *const fn (IOSurfaceRef) callconv(.c) u8;
const IOSurfaceSetPurgeableFn = *const fn (IOSurfaceRef, u32, ?*u32) callconv(.c) i32;
const IOSurfaceSetValueFn = *const fn (IOSurfaceRef, cf.CFStringRef, cf.CFStringRef) callconv(.c) void;

pub const Functions = struct {
    create: IOSurfaceCreateFn,
    is_in_use: IOSurfaceIsInUseFn,
    set_purgeable: IOSurfaceSetPurgeableFn,
    set_value: IOSurfaceSetValueFn,

    k_width: cf.CFStringRef,
    k_height: cf.CFStringRef,
    k_bytes_per_element: cf.CFStringRef,
    k_pixel_format: cf.CFStringRef,
    k_color_space: cf.CFStringRef,

    lib_iosurface: dynlib.DynLib,
};

var loader: sync.LazyOnce(Functions, loadInternal) = .{};

pub fn ensureLoaded() ?*const Functions {
    return loader.get();
}

fn loadInternal() ?Functions {
    if (cf.ensureLoaded() == null) return null;

    var lib = dynlib.DynLib.openZ("/System/Library/Frameworks/IOSurface.framework/IOSurface") catch return null;
    errdefer lib.close();

    const create_fn = lib.lookup(IOSurfaceCreateFn, "IOSurfaceCreate") orelse return null;
    const is_in_use_fn = lib.lookup(IOSurfaceIsInUseFn, "IOSurfaceIsInUse") orelse return null;
    const set_purgeable_fn = lib.lookup(IOSurfaceSetPurgeableFn, "IOSurfaceSetPurgeable") orelse return null;
    const set_value_fn = lib.lookup(IOSurfaceSetValueFn, "IOSurfaceSetValue") orelse return null;

    const k_width = (lib.lookup(*const cf.CFStringRef, "kIOSurfaceWidth") orelse return null).*;
    const k_height = (lib.lookup(*const cf.CFStringRef, "kIOSurfaceHeight") orelse return null).*;
    const k_bpe = (lib.lookup(*const cf.CFStringRef, "kIOSurfaceBytesPerElement") orelse return null).*;
    const k_pf = (lib.lookup(*const cf.CFStringRef, "kIOSurfacePixelFormat") orelse return null).*;
    const k_cs = (lib.lookup(*const cf.CFStringRef, "kIOSurfaceColorSpace") orelse return null).*;

    return .{
        .create = create_fn,
        .is_in_use = is_in_use_fn,
        .set_purgeable = set_purgeable_fn,
        .set_value = set_value_fn,
        .k_width = k_width,
        .k_height = k_height,
        .k_bytes_per_element = k_bpe,
        .k_pixel_format = k_pf,
        .k_color_space = k_cs,
        .lib_iosurface = lib,
    };
}

/// Allocates a BGRA8 IOSurface of width x height with the given color_space (2 = DisplayP3, else sRGB).
/// Caller owns the returned IOSurfaceRef (+1) and must release with `cf.release`.
pub fn createBGRA(width: u32, height: u32, color_space: u32) IOSurfaceRef {
    const f = ensureLoaded() orelse return null;
    const dict = cf.createMutableDictionary() orelse return null;
    defer cf.release(dict);

    const kvs = [_]struct { key: cf.CFStringRef, val: i32 }{
        .{ .key = f.k_width, .val = @intCast(width) },
        .{ .key = f.k_height, .val = @intCast(height) },
        .{ .key = f.k_bytes_per_element, .val = 4 },
        .{ .key = f.k_pixel_format, .val = pixel_format_bgra },
    };
    for (kvs) |kv| {
        const num = cf.createNumberI32(kv.val) orelse return null;
        cf.dictionarySetValue(dict, kv.key, num);
        cf.release(num);
    }

    const surf = f.create(dict) orelse return null;
    const cs_name = cf.colorSpaceName(color_space);
    if (cs_name != null) {
        f.set_value(surf, f.k_color_space, cs_name);
    }
    return surf;
}

pub inline fn isInUse(surf: IOSurfaceRef) bool {
    if (surf == null) return false;
    const f = ensureLoaded() orelse return false;
    return f.is_in_use(surf) != 0;
}

pub inline fn setPurgeable(surf: IOSurfaceRef, new_state: u32, old_state: ?*u32) i32 {
    if (surf == null) return 0;
    const f = ensureLoaded() orelse return 0;
    return f.set_purgeable(surf, new_state, old_state);
}
