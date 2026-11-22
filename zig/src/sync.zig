const std = @import("std");
const builtin = @import("builtin");

pub const TimedWaitResult = enum {
    signaled,
    timed_out,
};

const is_windows = builtin.os.tag == .windows;

const win = if (is_windows) struct {
    const SRWLOCK = extern struct {
        ptr: ?*anyopaque = null,
    };
    const CONDITION_VARIABLE = extern struct {
        ptr: ?*anyopaque = null,
    };

    const ERROR_TIMEOUT: u32 = 1460;
    const INFINITE: u32 = 0xFFFF_FFFF;

    extern "kernel32" fn AcquireSRWLockExclusive(SRWLock: *SRWLOCK) callconv(.winapi) void;
    extern "kernel32" fn ReleaseSRWLockExclusive(SRWLock: *SRWLOCK) callconv(.winapi) void;
    extern "kernel32" fn TryAcquireSRWLockExclusive(SRWLock: *SRWLOCK) callconv(.winapi) u8;
    extern "kernel32" fn SleepConditionVariableSRW(
        ConditionVariable: *CONDITION_VARIABLE,
        SRWLock: *SRWLOCK,
        dwMilliseconds: u32,
        Flags: u32,
    ) callconv(.winapi) c_int;
    extern "kernel32" fn WakeConditionVariable(ConditionVariable: *CONDITION_VARIABLE) callconv(.winapi) void;
    extern "kernel32" fn WakeAllConditionVariable(ConditionVariable: *CONDITION_VARIABLE) callconv(.winapi) void;
    extern "kernel32" fn GetLastError() callconv(.winapi) u32;
} else struct {};

pub const Mutex = struct {
    raw: if (is_windows) win.SRWLOCK else std.c.pthread_mutex_t =
        if (is_windows) .{} else std.c.PTHREAD_MUTEX_INITIALIZER,

    pub fn lock(self: *Mutex) void {
        if (is_windows) {
            win.AcquireSRWLockExclusive(&self.raw);
        } else {
            const rc = std.c.pthread_mutex_lock(&self.raw);
            std.debug.assert(rc == .SUCCESS);
        }
    }

    pub fn tryLock(self: *Mutex) bool {
        if (is_windows) {
            return win.TryAcquireSRWLockExclusive(&self.raw) != 0;
        } else {
            return std.c.pthread_mutex_trylock(&self.raw) == .SUCCESS;
        }
    }

    pub fn unlock(self: *Mutex) void {
        if (is_windows) {
            win.ReleaseSRWLockExclusive(&self.raw);
        } else {
            const rc = std.c.pthread_mutex_unlock(&self.raw);
            std.debug.assert(rc == .SUCCESS);
        }
    }

    pub fn deinit(self: *Mutex) void {
        if (!is_windows) {
            _ = std.c.pthread_mutex_destroy(&self.raw);
        }
    }
};

pub const Condition = struct {
    raw: if (is_windows) win.CONDITION_VARIABLE else std.c.pthread_cond_t =
        if (is_windows) .{} else std.c.PTHREAD_COND_INITIALIZER,

    pub fn wait(self: *Condition, mutex: *Mutex) void {
        if (is_windows) {
            _ = win.SleepConditionVariableSRW(&self.raw, &mutex.raw, win.INFINITE, 0);
        } else {
            const rc = std.c.pthread_cond_wait(&self.raw, &mutex.raw);
            std.debug.assert(rc == .SUCCESS);
        }
    }

    pub fn timedWaitNs(self: *Condition, mutex: *Mutex, timeout_ns: u64) TimedWaitResult {
        if (is_windows) {
            const ms_u64 = if (timeout_ns == 0)
                0
            else
                (timeout_ns + std.time.ns_per_ms - 1) / std.time.ns_per_ms;
            const ms: u32 = @intCast(@min(ms_u64, win.INFINITE - 1));
            const ok = win.SleepConditionVariableSRW(&self.raw, &mutex.raw, ms, 0);
            if (ok != 0) return .signaled;
            if (win.GetLastError() == win.ERROR_TIMEOUT) return .timed_out;
            return .signaled;
        } else {
            var now: std.c.timespec = undefined;
            _ = std.c.clock_gettime(std.c.CLOCK.REALTIME, &now);

            const add_sec = timeout_ns / std.time.ns_per_s;
            const add_nsec = timeout_ns % std.time.ns_per_s;

            var total_nsec: u64 = @as(u64, @intCast(now.nsec)) + add_nsec;
            const carry = total_nsec / std.time.ns_per_s;
            total_nsec %= std.time.ns_per_s;

            const max_sec: i64 = std.math.maxInt(i64);
            const cur_sec: i64 = @intCast(now.sec);
            const delta_sec: i64 = @intCast(@min(add_sec + carry, @as(u64, @intCast(max_sec - cur_sec))));

            const abs = std.c.timespec{
                .sec = @intCast(cur_sec + delta_sec),
                .nsec = @intCast(total_nsec),
            };
            const rc = std.c.pthread_cond_timedwait(&self.raw, &mutex.raw, &abs);
            if (rc == .TIMEDOUT) return .timed_out;
            return .signaled;
        }
    }

    pub fn signal(self: *Condition) void {
        if (is_windows) {
            win.WakeConditionVariable(&self.raw);
        } else {
            const rc = std.c.pthread_cond_signal(&self.raw);
            std.debug.assert(rc == .SUCCESS);
        }
    }

    pub fn broadcast(self: *Condition) void {
        if (is_windows) {
            win.WakeAllConditionVariable(&self.raw);
        } else {
            const rc = std.c.pthread_cond_broadcast(&self.raw);
            std.debug.assert(rc == .SUCCESS);
        }
    }

    pub fn deinit(self: *Condition) void {
        if (!is_windows) {
            _ = std.c.pthread_cond_destroy(&self.raw);
        }
    }
};

test "mutex and condition timedWaitNs" {
    var mu: Mutex = .{};
    defer mu.deinit();
    var cv: Condition = .{};
    defer cv.deinit();

    try std.testing.expect(mu.tryLock());
    mu.unlock();

    mu.lock();
    const res = cv.timedWaitNs(&mu, 2 * std.time.ns_per_ms);
    mu.unlock();
    try std.testing.expectEqual(TimedWaitResult.timed_out, res);
}
