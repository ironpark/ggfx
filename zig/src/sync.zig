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

/// Loads a `T` on first use and keeps the outcome: a failed load is remembered
/// and not retried. `get` may be called from any thread.
pub fn LazyOnce(comptime T: type, comptime load: fn () ?T) type {
    return struct {
        const Self = @This();
        const State = enum(u8) { unloaded, loaded, failed };

        mu: Mutex = .{},
        state: std.atomic.Value(State) = .init(.unloaded),
        value: T = undefined,

        pub fn get(self: *Self) ?*const T {
            if (self.peek()) |known| return known;

            self.mu.lock();
            defer self.mu.unlock();
            if (self.peek()) |known| return known;

            if (load()) |v| {
                self.value = v;
                self.state.store(.loaded, .release);
                return &self.value;
            }
            self.state.store(.failed, .release);
            return null;
        }

        /// The settled outcome, or null (outer optional) while still unloaded.
        fn peek(self: *Self) ??*const T {
            return switch (self.state.load(.acquire)) {
                .unloaded => null,
                .loaded => @as(?*const T, &self.value),
                .failed => @as(?*const T, null),
            };
        }
    };
}

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

test "LazyOnce loads once and remembers failure" {
    const Counted = struct {
        var ok_calls: u32 = 0;
        var fail_calls: u32 = 0;
        fn ok() ?u32 {
            ok_calls += 1;
            return 7;
        }
        fn fail() ?u32 {
            fail_calls += 1;
            return null;
        }
    };

    var good: LazyOnce(u32, Counted.ok) = .{};
    try std.testing.expectEqual(@as(u32, 7), good.get().?.*);
    try std.testing.expectEqual(@as(u32, 7), good.get().?.*);
    try std.testing.expectEqual(@as(u32, 1), Counted.ok_calls);

    var bad: LazyOnce(u32, Counted.fail) = .{};
    try std.testing.expect(bad.get() == null);
    try std.testing.expect(bad.get() == null);
    try std.testing.expectEqual(@as(u32, 1), Counted.fail_calls);
}
