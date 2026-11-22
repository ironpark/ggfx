const std = @import("std");
const ggfx = @import("ggfx");
const HandleTable = ggfx.handles.HandleTable;

test "HandleTable rejects id 0, duplicates, and unknown handles" {
    const gpa = std.testing.allocator;
    var tbl: HandleTable(u64) = .{};
    defer tbl.deinit(gpa);

    try std.testing.expectError(error.InvalidArgument, tbl.put(gpa, 0, 123));
    try std.testing.expectError(error.UnknownHandle, tbl.get(0));
    try std.testing.expectError(error.UnknownHandle, tbl.get(1));
    try std.testing.expectEqual(@as(?*u64, null), tbl.getOpt(0));
    try std.testing.expectEqual(@as(?*u64, null), tbl.getOpt(1));
    try std.testing.expectError(error.UnknownHandle, tbl.remove(0));
    try std.testing.expectError(error.UnknownHandle, tbl.remove(1));

    try tbl.put(gpa, 1, 100);
    try tbl.put(gpa, 5, 500);
    try std.testing.expectEqual(@as(u32, 2), tbl.live);
    try std.testing.expectError(error.DuplicateHandle, tbl.put(gpa, 1, 999));
    try std.testing.expectError(error.DuplicateHandle, tbl.put(gpa, 5, 999));

    const p1 = try tbl.get(1);
    try std.testing.expectEqual(@as(u64, 100), p1.*);
    p1.* = 101;
    try std.testing.expectEqual(@as(u64, 101), (try tbl.get(1)).*);
    try std.testing.expectEqual(@as(?*u64, null), tbl.getOpt(3));
}

test "HandleTable remove and reuse id" {
    const gpa = std.testing.allocator;
    var tbl: HandleTable(u32) = .{};
    defer tbl.deinit(gpa);

    try tbl.put(gpa, 2, 20);
    try tbl.put(gpa, 4, 40);
    try std.testing.expectEqual(@as(u32, 2), tbl.live);

    const removed = try tbl.remove(2);
    try std.testing.expectEqual(@as(u32, 20), removed);
    try std.testing.expectEqual(@as(u32, 1), tbl.live);
    try std.testing.expectError(error.UnknownHandle, tbl.get(2));
    try std.testing.expectError(error.UnknownHandle, tbl.remove(2));

    // Reusing freed id succeeds.
    try tbl.put(gpa, 2, 22);
    try std.testing.expectEqual(@as(u32, 2), tbl.live);
    try std.testing.expectEqual(@as(u32, 22), (try tbl.get(2)).*);
}
