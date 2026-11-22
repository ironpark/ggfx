const std = @import("std");
const dynlib = @import("../dynlib.zig");
const sync = @import("../sync.zig");
const time = @import("../time.zig");

pub const CVDisplayLinkRef = ?*anyopaque;
pub const CVReturn = i32;
pub const kCVReturnSuccess: CVReturn = 0;

/// How long the display link runs with no frame waiting for it before housekeeping stops it.
pub const link_idle_ns: u64 = 500 * std.time.ns_per_ms;

const CVDisplayLinkOutputCallbackFn = *const fn (
    display_link: CVDisplayLinkRef,
    in_now: ?*const anyopaque,
    in_output_time: ?*const anyopaque,
    flags_in: u64,
    flags_out: ?*u64,
    display_link_context: ?*anyopaque,
) callconv(.c) CVReturn;

const CreateWithActiveCGDisplaysFn = *const fn (*CVDisplayLinkRef) callconv(.c) CVReturn;
const SetOutputCallbackFn = *const fn (CVDisplayLinkRef, CVDisplayLinkOutputCallbackFn, ?*anyopaque) callconv(.c) CVReturn;
const StartFn = *const fn (CVDisplayLinkRef) callconv(.c) CVReturn;
const StopFn = *const fn (CVDisplayLinkRef) callconv(.c) CVReturn;
const ReleaseFn = *const fn (CVDisplayLinkRef) callconv(.c) void;

const Functions = struct {
    create: CreateWithActiveCGDisplaysFn,
    set_callback: SetOutputCallbackFn,
    start: StartFn,
    stop: StopFn,
    release: ReleaseFn,
    lib_cv: dynlib.DynLib,
};

var load_mu: sync.Mutex = .{};
var load_state = std.atomic.Value(u8).init(0); // 0=uninit, 1=ok, 2=failed
var funcs: Functions = undefined;

pub fn ensureLoaded() ?*const Functions {
    const st = load_state.load(.acquire);
    if (st == 1) return &funcs;
    if (st == 2) return null;

    load_mu.lock();
    defer load_mu.unlock();

    const st2 = load_state.load(.acquire);
    if (st2 == 1) return &funcs;
    if (st2 == 2) return null;

    if (loadInternal()) |f| {
        funcs = f;
        load_state.store(1, .release);
        return &funcs;
    } else {
        load_state.store(2, .release);
        return null;
    }
}

fn loadInternal() ?Functions {
    var lib = dynlib.DynLib.openZ("/System/Library/Frameworks/CoreVideo.framework/CoreVideo") catch return null;
    errdefer lib.close();

    const create = lib.lookup(CreateWithActiveCGDisplaysFn, "CVDisplayLinkCreateWithActiveCGDisplays") orelse return null;
    const set_cb = lib.lookup(SetOutputCallbackFn, "CVDisplayLinkSetOutputCallback") orelse return null;
    const start = lib.lookup(StartFn, "CVDisplayLinkStart") orelse return null;
    const stop = lib.lookup(StopFn, "CVDisplayLinkStop") orelse return null;
    const release = lib.lookup(ReleaseFn, "CVDisplayLinkRelease") orelse return null;

    return .{
        .create = create,
        .set_callback = set_cb,
        .start = start,
        .stop = stop,
        .release = release,
        .lib_cv = lib,
    };
}

pub const Fence = struct {
    mu: sync.Mutex = .{},
    cv: sync.Condition = .{},
    value: u64 = 0,
    last_value: u64 = 0,
    closed: bool = false,

    pub fn deinit(self: *Fence) void {
        self.cv.deinit();
        self.mu.deinit();
    }

    pub fn wait(self: *Fence) void {
        self.mu.lock();
        defer self.mu.unlock();
        while (!self.closed and self.last_value >= self.value) {
            // Bounded wait so a display reconfiguration or headless VM cannot hang forever.
            const res = self.cv.timedWaitNs(&self.mu, 50 * std.time.ns_per_ms);
            if (res == .timed_out) break;
        }
        self.last_value = self.value;
    }

    pub fn advance(self: *Fence) void {
        self.mu.lock();
        defer self.mu.unlock();
        self.value +%= 1;
        self.cv.broadcast();
    }

    pub fn close(self: *Fence) void {
        self.mu.lock();
        defer self.mu.unlock();
        self.closed = true;
        self.cv.broadcast();
    }
};

fn outputCallback(
    display_link: CVDisplayLinkRef,
    in_now: ?*const anyopaque,
    in_output_time: ?*const anyopaque,
    flags_in: u64,
    flags_out: ?*u64,
    display_link_context: ?*anyopaque,
) callconv(.c) CVReturn {
    _ = display_link;
    _ = in_now;
    _ = in_output_time;
    _ = flags_in;
    _ = flags_out;
    if (display_link_context) |ctx| {
        const fence: *Fence = @ptrCast(@alignCast(ctx));
        fence.advance();
    }
    return kCVReturnSuccess;
}

/// Manages a CVDisplayLink pinned in memory (must not move once initialized).
pub const DisplayLink = struct {
    mu: sync.Mutex = .{},
    ref: CVDisplayLinkRef = null,
    stopped: bool = true,
    last_wait_ns: u64 = 0,
    fence: Fence = .{},

    pub fn init(self: *DisplayLink) void {
        self.* = .{};
        const f = ensureLoaded() orelse return;
        var dl: CVDisplayLinkRef = null;
        if (f.create(&dl) != kCVReturnSuccess or dl == null) {
            return;
        }
        _ = f.set_callback(dl, outputCallback, &self.fence);
        self.mu.lock();
        self.ref = dl;
        self.stopped = true;
        self.mu.unlock();
    }

    pub fn waitForRefresh(self: *DisplayLink) void {
        const f = ensureLoaded() orelse return;
        self.mu.lock();
        if (self.ref == null) {
            self.mu.unlock();
            return;
        }
        if (self.stopped) {
            _ = f.start(self.ref);
            self.stopped = false;
        }
        self.last_wait_ns = time.monotonicNs();
        self.mu.unlock();

        self.fence.wait();
    }

    /// Called periodically by the backend's housekeeping thread.
    pub fn tickIdle(self: *DisplayLink, now_ns: u64) void {
        const f = ensureLoaded() orelse return;
        self.mu.lock();
        defer self.mu.unlock();
        if (self.ref != null and !self.stopped) {
            if (now_ns >= self.last_wait_ns and (now_ns - self.last_wait_ns) >= link_idle_ns) {
                _ = f.stop(self.ref);
                self.stopped = true;
            }
        }
    }

    pub fn deinit(self: *DisplayLink) void {
        const f = ensureLoaded();
        self.mu.lock();
        const dl = self.ref;
        self.ref = null;
        self.stopped = true;
        self.mu.unlock();

        self.fence.close();

        if (dl != null) {
            if (f) |fns| {
                // CVDisplayLinkStop blocks until any in-progress output callback finishes,
                // guaranteeing `self.fence` is no longer accessed after this call.
                _ = fns.stop(dl);
                fns.release(dl);
            }
        }
        self.fence.deinit();
        self.mu.deinit();
    }
};
