const std = @import("std");
const builtin = @import("builtin");

const is_windows = builtin.os.tag == .windows;

const win = if (is_windows) struct {
    extern "kernel32" fn QueryPerformanceCounter(lpPerformanceCount: *i64) callconv(.winapi) c_int;
    extern "kernel32" fn QueryPerformanceFrequency(lpFrequency: *i64) callconv(.winapi) c_int;
    extern "kernel32" fn Sleep(dwMilliseconds: u32) callconv(.winapi) void;

    var cached_freq = std.atomic.Value(u64).init(0);

    fn frequency() u64 {
        const f = cached_freq.load(.acquire);
        if (f != 0) return f;
        var raw: i64 = 0;
        _ = QueryPerformanceFrequency(&raw);
        const val: u64 = if (raw > 0) @intCast(raw) else 1;
        cached_freq.store(val, .release);
        return val;
    }
} else struct {};

pub fn monotonicNs() u64 {
    if (is_windows) {
        var counter: i64 = 0;
        _ = win.QueryPerformanceCounter(&counter);
        const freq = win.frequency();
        const c: u128 = if (counter > 0) @intCast(counter) else 0;
        return @intCast((c * std.time.ns_per_s) / freq);
    } else {
        var ts: std.c.timespec = undefined;
        _ = std.c.clock_gettime(std.c.CLOCK.MONOTONIC, &ts);
        return @as(u64, @intCast(ts.sec)) * std.time.ns_per_s + @as(u64, @intCast(ts.nsec));
    }
}

pub fn sleepNs(ns: u64) void {
    if (ns == 0) return;
    if (is_windows) {
        const ms_u64 = (ns + std.time.ns_per_ms - 1) / std.time.ns_per_ms;
        const ms: u32 = @intCast(@min(ms_u64, std.math.maxInt(u32) - 1));
        win.Sleep(ms);
    } else {
        var req = std.c.timespec{
            .sec = @intCast(ns / std.time.ns_per_s),
            .nsec = @intCast(ns % std.time.ns_per_s),
        };
        var rem: std.c.timespec = undefined;
        while (std.c.nanosleep(&req, &rem) != 0) {
            if (std.c._errno().* == @intFromEnum(std.c.E.INTR)) {
                req = rem;
                continue;
            }
            break;
        }
    }
}

test "monotonicNs advances across sleepNs" {
    const t0 = monotonicNs();
    sleepNs(2 * std.time.ns_per_ms);
    const t1 = monotonicNs();
    try std.testing.expect(t1 >= t0);
}
