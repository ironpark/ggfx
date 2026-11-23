const std = @import("std");
const dynlib = @import("../dynlib.zig");
const sync = @import("../sync.zig");

pub const Id = ?*anyopaque;
pub const Class = ?*anyopaque;
pub const Sel = ?*anyopaque;

pub const NSRange = extern struct {
    location: usize,
    length: usize,
};

pub const CGSize = extern struct {
    width: f64,
    height: f64,
};

pub const ns_utf8_string_encoding: usize = 4;

pub const MsgSendFn = *const anyopaque;
pub const GetClassFn = *const fn ([*:0]const u8) callconv(.c) Class;
pub const SelRegisterNameFn = *const fn ([*:0]const u8) callconv(.c) Sel;
pub const AutoreleasePoolPushFn = *const fn () callconv(.c) ?*anyopaque;
pub const AutoreleasePoolPopFn = *const fn (?*anyopaque) callconv(.c) void;
pub const RetainFn = *const fn (Id) callconv(.c) Id;
pub const ReleaseFn = *const fn (Id) callconv(.c) void;
pub const DispatchWorkFn = *const fn (?*anyopaque) callconv(.c) void;
pub const DispatchSyncFFn = *const fn (?*anyopaque, ?*anyopaque, DispatchWorkFn) callconv(.c) void;

pub const Classes = struct {
    NSString: Class = null,
    CALayer: Class = null,
    CAMetalLayer: Class = null,
    CATransaction: Class = null,
    MTLRenderPipelineDescriptor: Class = null,
    MTLVertexDescriptor: Class = null,
    MTLTextureDescriptor: Class = null,
    MTLRenderPassDescriptor: Class = null,
};

pub const Sels = struct {
    // NSObject / NSString / NSError
    alloc: Sel = null,
    new: Sel = null,
    respondsToSelector: Sel = null,
    initWithBytes_length_encoding: Sel = null,
    UTF8String: Sel = null,
    localizedDescription: Sel = null,

    // NSView
    setLayer: Sel = null,
    setWantsLayer: Sel = null,
    setLayerContentsRedrawPolicy: Sel = null,

    // CALayer / CAMetalLayer / CATransaction / CAMetalDrawable
    layer: Sel = null,
    setContents: Sel = null,
    setOpaque: Sel = null,
    begin: Sel = null,
    commit: Sel = null,
    flush: Sel = null,
    setDisableActions: Sel = null,
    setDevice: Sel = null,
    setPixelFormat: Sel = null,
    setColorspace: Sel = null,
    setMaximumDrawableCount: Sel = null,
    setDisplaySyncEnabled: Sel = null,
    setDrawableSize: Sel = null,
    nextDrawable: Sel = null,
    setPresentsWithTransaction: Sel = null,
    setFramebufferOnly: Sel = null,
    texture: Sel = null,
    present: Sel = null,

    // MTLDevice
    hasUnifiedMemory: Sel = null,
    name: Sel = null,
    supportsFamily: Sel = null,
    supportsFeatureSet: Sel = null,
    newCommandQueue: Sel = null,
    newLibraryWithSource_options_error: Sel = null,
    newRenderPipelineStateWithDescriptor_error: Sel = null,
    newBufferWithLength_options: Sel = null,
    newTextureWithDescriptor: Sel = null,
    newTextureWithDescriptor_iosurface_plane: Sel = null,

    // MTLLibrary
    newFunctionWithName: Sel = null,

    // MTLRenderPipelineDescriptor / MTLVertexDescriptor
    setVertexFunction: Sel = null,
    setFragmentFunction: Sel = null,
    setVertexDescriptor: Sel = null,
    vertexDescriptor: Sel = null,
    attributes: Sel = null,
    layouts: Sel = null,
    setFormat: Sel = null,
    setOffset: Sel = null,
    setBufferIndex: Sel = null,
    setStride: Sel = null,
    colorAttachments: Sel = null,
    objectAtIndexedSubscript: Sel = null,
    setBlendingEnabled: Sel = null,
    setDestinationAlphaBlendFactor: Sel = null,
    setDestinationRGBBlendFactor: Sel = null,
    setSourceAlphaBlendFactor: Sel = null,
    setSourceRGBBlendFactor: Sel = null,
    setAlphaBlendOperation: Sel = null,
    setRgbBlendOperation: Sel = null,
    setWriteMask: Sel = null,

    // MTLTextureDescriptor / MTLTexture
    setTextureType: Sel = null,
    setWidth: Sel = null,
    setHeight: Sel = null,
    setStorageMode: Sel = null,
    setUsage: Sel = null,
    setSwizzle: Sel = null,
    width: Sel = null,
    height: Sel = null,
    newTextureViewWithPixelFormat_textureType_levels_slices_swizzle: Sel = null,
    getBytes_bytesPerRow_fromRegion_mipmapLevel: Sel = null,

    // MTLBuffer
    contents: Sel = null,
    didModifyRange: Sel = null,

    // MTLCommandQueue / MTLCommandBuffer
    commandBuffer: Sel = null,
    status: Sel = null,
    presentDrawable: Sel = null,
    waitUntilCompleted: Sel = null,
    waitUntilScheduled: Sel = null,
    renderCommandEncoderWithDescriptor: Sel = null,
    blitCommandEncoder: Sel = null,

    // MTLRenderPassDescriptor
    setLoadAction: Sel = null,
    setStoreAction: Sel = null,
    setTexture: Sel = null,
    setClearColor: Sel = null,

    // MTLCommandEncoder / MTLRenderCommandEncoder / MTLBlitCommandEncoder
    endEncoding: Sel = null,
    setRenderPipelineState: Sel = null,
    setViewport: Sel = null,
    setScissorRect: Sel = null,
    setVertexBuffer_offset_atIndex: Sel = null,
    setVertexBytes_length_atIndex: Sel = null,
    setFragmentBytes_length_atIndex: Sel = null,
    setFragmentBuffer_offset_atIndex: Sel = null,
    setFragmentTexture_atIndex: Sel = null,
    drawIndexedPrimitives_indexCount_indexType_indexBuffer_indexBufferOffset: Sel = null,
    synchronizeTexture_slice_level: Sel = null,
    copyFromTexture_sourceSlice_sourceLevel_sourceOrigin_sourceSize_toTexture_destinationSlice_destinationLevel_destinationOrigin: Sel = null,
    copyFromBuffer_sourceOffset_sourceBytesPerRow_sourceBytesPerImage_sourceSize_toTexture_destinationSlice_destinationLevel_destinationOrigin: Sel = null,
};

pub const Runtime = struct {
    objc_msgSend: MsgSendFn,
    objc_getClass: GetClassFn,
    sel_registerName: SelRegisterNameFn,
    objc_autoreleasePoolPush: AutoreleasePoolPushFn,
    objc_autoreleasePoolPop: AutoreleasePoolPopFn,
    objc_retain: RetainFn,
    objc_release: ReleaseFn,

    // Optional GCD main queue dispatch (libSystem.B.dylib).
    dispatch_main_q: ?*anyopaque,
    dispatch_sync_f: ?DispatchSyncFFn,

    // Framework handles kept loaded for process lifetime.
    lib_objc: dynlib.DynLib,
    lib_foundation: ?dynlib.DynLib,
    lib_metal: ?dynlib.DynLib,
    lib_quartzcore: ?dynlib.DynLib,
    lib_system: ?dynlib.DynLib,

    classes: Classes,
    sels: Sels,
};

var loader: sync.LazyOnce(Runtime, loadInternal) = .{};

pub fn ensureLoaded() ?*const Runtime {
    return loader.get();
}

pub fn get() *const Runtime {
    return ensureLoaded().?;
}

fn loadInternal() ?Runtime {
    var lib_objc = dynlib.DynLib.openZ("/usr/lib/libobjc.A.dylib") catch return null;
    errdefer lib_objc.close();

    const msg_send = lib_objc.lookup(MsgSendFn, "objc_msgSend") orelse return null;
    const get_class = lib_objc.lookup(GetClassFn, "objc_getClass") orelse return null;
    const sel_reg = lib_objc.lookup(SelRegisterNameFn, "sel_registerName") orelse return null;
    const pool_push = lib_objc.lookup(AutoreleasePoolPushFn, "objc_autoreleasePoolPush") orelse return null;
    const pool_pop = lib_objc.lookup(AutoreleasePoolPopFn, "objc_autoreleasePoolPop") orelse return null;
    const retain_fn = lib_objc.lookup(RetainFn, "objc_retain") orelse return null;
    const release_fn = lib_objc.lookup(ReleaseFn, "objc_release") orelse return null;

    // Load Foundation, Metal, and QuartzCore so ObjC classes are registered.
    const lib_foundation = dynlib.DynLib.openZ("/System/Library/Frameworks/Foundation.framework/Foundation") catch null;
    const lib_metal = dynlib.DynLib.openZ("/System/Library/Frameworks/Metal.framework/Metal") catch null;
    const lib_quartzcore = dynlib.DynLib.openZ("/System/Library/Frameworks/QuartzCore.framework/QuartzCore") catch null;

    var lib_system = dynlib.DynLib.openZ("/usr/lib/libSystem.B.dylib") catch null;
    var dispatch_main_q: ?*anyopaque = null;
    var dispatch_sync_f: ?DispatchSyncFFn = null;
    if (lib_system) |*ls| {
        dispatch_main_q = ls.lookup(?*anyopaque, "_dispatch_main_q") orelse null;
        dispatch_sync_f = ls.lookup(DispatchSyncFFn, "dispatch_sync_f");
    }

    var rt = Runtime{
        .objc_msgSend = msg_send,
        .objc_getClass = get_class,
        .sel_registerName = sel_reg,
        .objc_autoreleasePoolPush = pool_push,
        .objc_autoreleasePoolPop = pool_pop,
        .objc_retain = retain_fn,
        .objc_release = release_fn,
        .dispatch_main_q = dispatch_main_q,
        .dispatch_sync_f = dispatch_sync_f,
        .lib_objc = lib_objc,
        .lib_foundation = lib_foundation,
        .lib_metal = lib_metal,
        .lib_quartzcore = lib_quartzcore,
        .lib_system = lib_system,
        .classes = .{},
        .sels = .{},
    };

    rt.classes = .{
        .NSString = get_class("NSString"),
        .CALayer = get_class("CALayer"),
        .CAMetalLayer = get_class("CAMetalLayer"),
        .CATransaction = get_class("CATransaction"),
        .MTLRenderPipelineDescriptor = get_class("MTLRenderPipelineDescriptor"),
        .MTLVertexDescriptor = get_class("MTLVertexDescriptor"),
        .MTLTextureDescriptor = get_class("MTLTextureDescriptor"),
        .MTLRenderPassDescriptor = get_class("MTLRenderPassDescriptor"),
    };

    rt.sels = .{
        .alloc = sel_reg("alloc"),
        .new = sel_reg("new"),
        .respondsToSelector = sel_reg("respondsToSelector:"),
        .initWithBytes_length_encoding = sel_reg("initWithBytes:length:encoding:"),
        .UTF8String = sel_reg("UTF8String"),
        .localizedDescription = sel_reg("localizedDescription"),

        .setLayer = sel_reg("setLayer:"),
        .setWantsLayer = sel_reg("setWantsLayer:"),
        .setLayerContentsRedrawPolicy = sel_reg("setLayerContentsRedrawPolicy:"),

        .layer = sel_reg("layer"),
        .setContents = sel_reg("setContents:"),
        .setOpaque = sel_reg("setOpaque:"),
        .begin = sel_reg("begin"),
        .commit = sel_reg("commit"),
        .flush = sel_reg("flush"),
        .setDisableActions = sel_reg("setDisableActions:"),
        .setDevice = sel_reg("setDevice:"),
        .setPixelFormat = sel_reg("setPixelFormat:"),
        .setColorspace = sel_reg("setColorspace:"),
        .setMaximumDrawableCount = sel_reg("setMaximumDrawableCount:"),
        .setDisplaySyncEnabled = sel_reg("setDisplaySyncEnabled:"),
        .setDrawableSize = sel_reg("setDrawableSize:"),
        .nextDrawable = sel_reg("nextDrawable"),
        .setPresentsWithTransaction = sel_reg("setPresentsWithTransaction:"),
        .setFramebufferOnly = sel_reg("setFramebufferOnly:"),
        .texture = sel_reg("texture"),
        .present = sel_reg("present"),

        .hasUnifiedMemory = sel_reg("hasUnifiedMemory"),
        .name = sel_reg("name"),
        .supportsFamily = sel_reg("supportsFamily:"),
        .supportsFeatureSet = sel_reg("supportsFeatureSet:"),
        .newCommandQueue = sel_reg("newCommandQueue"),
        .newLibraryWithSource_options_error = sel_reg("newLibraryWithSource:options:error:"),
        .newRenderPipelineStateWithDescriptor_error = sel_reg("newRenderPipelineStateWithDescriptor:error:"),
        .newBufferWithLength_options = sel_reg("newBufferWithLength:options:"),
        .newTextureWithDescriptor = sel_reg("newTextureWithDescriptor:"),
        .newTextureWithDescriptor_iosurface_plane = sel_reg("newTextureWithDescriptor:iosurface:plane:"),

        .newFunctionWithName = sel_reg("newFunctionWithName:"),

        .setVertexFunction = sel_reg("setVertexFunction:"),
        .setFragmentFunction = sel_reg("setFragmentFunction:"),
        .setVertexDescriptor = sel_reg("setVertexDescriptor:"),
        .vertexDescriptor = sel_reg("vertexDescriptor"),
        .attributes = sel_reg("attributes"),
        .layouts = sel_reg("layouts"),
        .setFormat = sel_reg("setFormat:"),
        .setOffset = sel_reg("setOffset:"),
        .setBufferIndex = sel_reg("setBufferIndex:"),
        .setStride = sel_reg("setStride:"),
        .colorAttachments = sel_reg("colorAttachments"),
        .objectAtIndexedSubscript = sel_reg("objectAtIndexedSubscript:"),
        .setBlendingEnabled = sel_reg("setBlendingEnabled:"),
        .setDestinationAlphaBlendFactor = sel_reg("setDestinationAlphaBlendFactor:"),
        .setDestinationRGBBlendFactor = sel_reg("setDestinationRGBBlendFactor:"),
        .setSourceAlphaBlendFactor = sel_reg("setSourceAlphaBlendFactor:"),
        .setSourceRGBBlendFactor = sel_reg("setSourceRGBBlendFactor:"),
        .setAlphaBlendOperation = sel_reg("setAlphaBlendOperation:"),
        .setRgbBlendOperation = sel_reg("setRgbBlendOperation:"),
        .setWriteMask = sel_reg("setWriteMask:"),

        .setTextureType = sel_reg("setTextureType:"),
        .setWidth = sel_reg("setWidth:"),
        .setHeight = sel_reg("setHeight:"),
        .setStorageMode = sel_reg("setStorageMode:"),
        .setUsage = sel_reg("setUsage:"),
        .setSwizzle = sel_reg("setSwizzle:"),
        .width = sel_reg("width"),
        .height = sel_reg("height"),
        .newTextureViewWithPixelFormat_textureType_levels_slices_swizzle = sel_reg("newTextureViewWithPixelFormat:textureType:levels:slices:swizzle:"),
        .getBytes_bytesPerRow_fromRegion_mipmapLevel = sel_reg("getBytes:bytesPerRow:fromRegion:mipmapLevel:"),

        .contents = sel_reg("contents"),
        .didModifyRange = sel_reg("didModifyRange:"),

        .commandBuffer = sel_reg("commandBuffer"),
        .status = sel_reg("status"),
        .presentDrawable = sel_reg("presentDrawable:"),
        .waitUntilCompleted = sel_reg("waitUntilCompleted"),
        .waitUntilScheduled = sel_reg("waitUntilScheduled"),
        .renderCommandEncoderWithDescriptor = sel_reg("renderCommandEncoderWithDescriptor:"),
        .blitCommandEncoder = sel_reg("blitCommandEncoder"),

        .setLoadAction = sel_reg("setLoadAction:"),
        .setStoreAction = sel_reg("setStoreAction:"),
        .setTexture = sel_reg("setTexture:"),
        .setClearColor = sel_reg("setClearColor:"),

        .endEncoding = sel_reg("endEncoding"),
        .setRenderPipelineState = sel_reg("setRenderPipelineState:"),
        .setViewport = sel_reg("setViewport:"),
        .setScissorRect = sel_reg("setScissorRect:"),
        .setVertexBuffer_offset_atIndex = sel_reg("setVertexBuffer:offset:atIndex:"),
        .setVertexBytes_length_atIndex = sel_reg("setVertexBytes:length:atIndex:"),
        .setFragmentBytes_length_atIndex = sel_reg("setFragmentBytes:length:atIndex:"),
        .setFragmentBuffer_offset_atIndex = sel_reg("setFragmentBuffer:offset:atIndex:"),
        .setFragmentTexture_atIndex = sel_reg("setFragmentTexture:atIndex:"),
        .drawIndexedPrimitives_indexCount_indexType_indexBuffer_indexBufferOffset = sel_reg("drawIndexedPrimitives:indexCount:indexType:indexBuffer:indexBufferOffset:"),
        .synchronizeTexture_slice_level = sel_reg("synchronizeTexture:slice:level:"),
        .copyFromTexture_sourceSlice_sourceLevel_sourceOrigin_sourceSize_toTexture_destinationSlice_destinationLevel_destinationOrigin = sel_reg("copyFromTexture:sourceSlice:sourceLevel:sourceOrigin:sourceSize:toTexture:destinationSlice:destinationLevel:destinationOrigin:"),
        .copyFromBuffer_sourceOffset_sourceBytesPerRow_sourceBytesPerImage_sourceSize_toTexture_destinationSlice_destinationLevel_destinationOrigin = sel_reg("copyFromBuffer:sourceOffset:sourceBytesPerRow:sourceBytesPerImage:sourceSize:toTexture:destinationSlice:destinationLevel:destinationOrigin:"),
    };

    return rt;
}

/// Casts objc_msgSend to an exact non-variadic `callconv(.c)` function pointer.
/// Enforces at comptime that the signature is non-variadic, C calling convention,
/// and does not return a struct > 16 bytes (which would require objc_msgSend_stret on x86_64).
pub inline fn send(comptime FnPtr: type) FnPtr {
    comptime {
        const ptr_info = @typeInfo(FnPtr).pointer;
        const fn_info = @typeInfo(ptr_info.child).@"fn";
        if (fn_info.attrs.varargs) {
            @compileError("objc.send must never use variadic prototypes (arm64 ABI requirement)");
        }
        if (!std.meta.eql(fn_info.attrs.@"callconv", std.builtin.CallingConvention.c)) {
            @compileError("objc.send requires callconv(.c)");
        }
        if (fn_info.return_type) |Ret| {
            if (Ret != void and @sizeOf(Ret) > 16) {
                @compileError("objc.send forbids return types > 16 bytes (x86_64 objc_msgSend_stret)");
            }
        }
    }
    return @ptrCast(@alignCast(get().objc_msgSend));
}

pub inline fn autoreleasePoolPush() ?*anyopaque {
    const rt = ensureLoaded() orelse return null;
    return rt.objc_autoreleasePoolPush();
}

pub inline fn autoreleasePoolPop(pool: ?*anyopaque) void {
    const p = pool orelse return;
    const rt = ensureLoaded() orelse return;
    rt.objc_autoreleasePoolPop(p);
}

pub inline fn retain(obj: Id) Id {
    if (obj == null) return null;
    return get().objc_retain(obj);
}

pub inline fn release(obj: Id) void {
    if (obj == null) return;
    get().objc_release(obj);
}

pub inline fn respondsToSelector(obj: Id, sel: Sel) bool {
    if (obj == null) return false;
    const rt = get();
    return send(*const fn (Id, Sel, Sel) callconv(.c) u8)(obj, rt.sels.respondsToSelector, sel) != 0;
}

/// Creates a +1 retained NSString from UTF-8 bytes (does not require NUL termination).
/// Caller MUST call `objc.release` on the returned Id when done.
pub fn nsStringFromSlice(bytes: []const u8) Id {
    const rt = get();
    const alloc_obj = send(*const fn (Class, Sel) callconv(.c) Id)(rt.classes.NSString, rt.sels.alloc);
    if (alloc_obj == null) return null;
    const ptr: ?*const anyopaque = if (bytes.len == 0) "" else bytes.ptr;
    return send(*const fn (Id, Sel, ?*const anyopaque, usize, usize) callconv(.c) Id)(
        alloc_obj,
        rt.sels.initWithBytes_length_encoding,
        ptr,
        bytes.len,
        ns_utf8_string_encoding,
    );
}

/// Returns a borrowed UTF-8 slice from an NSString (valid while NSString/autorelease pool lives).
pub fn nsStringSlice(ns_str: Id) []const u8 {
    if (ns_str == null) return "";
    const rt = get();
    const cstr = send(*const fn (Id, Sel) callconv(.c) ?[*:0]const u8)(ns_str, rt.sels.UTF8String) orelse return "";
    return std.mem.span(cstr);
}

/// Extracts `localizedDescription` from an NSError as a borrowed UTF-8 slice.
pub fn nsErrorDescription(ns_err: Id) []const u8 {
    if (ns_err == null) return "unknown Metal error";
    const rt = get();
    const desc = send(*const fn (Id, Sel) callconv(.c) Id)(ns_err, rt.sels.localizedDescription);
    if (desc == null) return "unknown Metal error";
    return nsStringSlice(desc);
}

/// Synchronously runs `work(ctx)` on the GCD main queue if libSystem dispatch is available,
/// otherwise executes inline.
pub fn dispatchSyncMain(ctx: ?*anyopaque, work: DispatchWorkFn) void {
    const rt = get();
    if (rt.dispatch_main_q) |q| {
        if (rt.dispatch_sync_f) |sync_f| {
            sync_f(q, ctx, work);
            return;
        }
    }
    work(ctx);
}
