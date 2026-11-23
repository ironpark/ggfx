const std = @import("std");
const dynlib = @import("../dynlib.zig");
const sync = @import("../sync.zig");

pub const CFTypeRef = ?*anyopaque;
pub const CFStringRef = ?*const anyopaque;
pub const CFAllocatorRef = ?*anyopaque;
pub const CFMutableDictionaryRef = ?*anyopaque;
pub const CFNumberRef = ?*anyopaque;
pub const CGColorSpaceRef = ?*anyopaque;

pub const cf_number_sint32_type: isize = 3;

const CFRetainFn = *const fn (CFTypeRef) callconv(.c) CFTypeRef;
const CFReleaseFn = *const fn (CFTypeRef) callconv(.c) void;
const CFDictionaryCreateMutableFn = *const fn (CFAllocatorRef, isize, ?*const anyopaque, ?*const anyopaque) callconv(.c) CFMutableDictionaryRef;
const CFDictionarySetValueFn = *const fn (CFMutableDictionaryRef, ?*const anyopaque, ?*const anyopaque) callconv(.c) void;
const CFNumberCreateFn = *const fn (CFAllocatorRef, isize, ?*const anyopaque) callconv(.c) CFNumberRef;
const CGColorSpaceCreateWithNameFn = *const fn (CFStringRef) callconv(.c) CGColorSpaceRef;
const CGColorSpaceReleaseFn = *const fn (CGColorSpaceRef) callconv(.c) void;

pub const Functions = struct {
    cf_retain: CFRetainFn,
    cf_release: CFReleaseFn,
    dict_create_mutable: CFDictionaryCreateMutableFn,
    dict_set_value: CFDictionarySetValueFn,
    number_create: CFNumberCreateFn,
    key_callbacks: *const anyopaque,
    value_callbacks: *const anyopaque,

    cg_color_space_create_with_name: CGColorSpaceCreateWithNameFn,
    cg_color_space_release: CGColorSpaceReleaseFn,
    k_cg_color_space_srgb: CFStringRef,
    k_cg_color_space_display_p3: CFStringRef,

    lib_cf: dynlib.DynLib,
    lib_cg: dynlib.DynLib,
};

var loader: sync.LazyOnce(Functions, loadInternal) = .{};

pub fn ensureLoaded() ?*const Functions {
    return loader.get();
}

fn loadInternal() ?Functions {
    var lib_cf = dynlib.DynLib.openZ("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation") catch return null;
    errdefer lib_cf.close();

    var lib_cg = dynlib.DynLib.openZ("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics") catch return null;
    errdefer lib_cg.close();

    const cf_retain = lib_cf.lookup(CFRetainFn, "CFRetain") orelse return null;
    const cf_release = lib_cf.lookup(CFReleaseFn, "CFRelease") orelse return null;
    const dict_create = lib_cf.lookup(CFDictionaryCreateMutableFn, "CFDictionaryCreateMutable") orelse return null;
    const dict_set = lib_cf.lookup(CFDictionarySetValueFn, "CFDictionarySetValue") orelse return null;
    const num_create = lib_cf.lookup(CFNumberCreateFn, "CFNumberCreate") orelse return null;
    const key_cb = lib_cf.lookup(*const anyopaque, "kCFTypeDictionaryKeyCallBacks") orelse return null;
    const val_cb = lib_cf.lookup(*const anyopaque, "kCFTypeDictionaryValueCallBacks") orelse return null;

    const cg_create = lib_cg.lookup(CGColorSpaceCreateWithNameFn, "CGColorSpaceCreateWithName") orelse return null;
    const cg_release = lib_cg.lookup(CGColorSpaceReleaseFn, "CGColorSpaceRelease") orelse return null;
    const srgb_sym = lib_cg.lookup(*const CFStringRef, "kCGColorSpaceSRGB") orelse return null;
    const p3_sym = lib_cg.lookup(*const CFStringRef, "kCGColorSpaceDisplayP3") orelse return null;

    return .{
        .cf_retain = cf_retain,
        .cf_release = cf_release,
        .dict_create_mutable = dict_create,
        .dict_set_value = dict_set,
        .number_create = num_create,
        .key_callbacks = key_cb,
        .value_callbacks = val_cb,
        .cg_color_space_create_with_name = cg_create,
        .cg_color_space_release = cg_release,
        .k_cg_color_space_srgb = srgb_sym.*,
        .k_cg_color_space_display_p3 = p3_sym.*,
        .lib_cf = lib_cf,
        .lib_cg = lib_cg,
    };
}

pub inline fn retain(ref: CFTypeRef) CFTypeRef {
    if (ref == null) return null;
    const f = ensureLoaded() orelse return null;
    return f.cf_retain(ref);
}

pub inline fn release(ref: CFTypeRef) void {
    if (ref == null) return;
    const f = ensureLoaded() orelse return;
    f.cf_release(ref);
}

pub inline fn createMutableDictionary() CFMutableDictionaryRef {
    const f = ensureLoaded() orelse return null;
    return f.dict_create_mutable(null, 0, f.key_callbacks, f.value_callbacks);
}

pub inline fn dictionarySetValue(dict: CFMutableDictionaryRef, key: ?*const anyopaque, value: ?*const anyopaque) void {
    const f = ensureLoaded() orelse return;
    f.dict_set_value(dict, key, value);
}

pub inline fn createNumberI32(value: i32) CFNumberRef {
    const f = ensureLoaded() orelse return null;
    var v = value;
    return f.number_create(null, cf_number_sint32_type, &v);
}

/// Returns the CFStringRef name for color_space (2 = DisplayP3, else sRGB).
pub fn colorSpaceName(color_space: u32) CFStringRef {
    const f = ensureLoaded() orelse return null;
    return if (color_space == 2) f.k_cg_color_space_display_p3 else f.k_cg_color_space_srgb;
}

/// Creates a CGColorSpaceRef for color_space (2 = DisplayP3, else sRGB).
/// Caller must release with `releaseCGColorSpace`.
pub fn createCGColorSpace(color_space: u32) CGColorSpaceRef {
    const f = ensureLoaded() orelse return null;
    const name = if (color_space == 2) f.k_cg_color_space_display_p3 else f.k_cg_color_space_srgb;
    return f.cg_color_space_create_with_name(name);
}

pub inline fn releaseCGColorSpace(cs: CGColorSpaceRef) void {
    if (cs == null) return;
    const f = ensureLoaded() orelse return;
    f.cg_color_space_release(cs);
}
