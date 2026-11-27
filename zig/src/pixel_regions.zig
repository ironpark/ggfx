const std = @import("std");
const abi = @import("abi.zig");
const errors = @import("errors.zig");
const Diag = errors.Diag;
const Error = errors.Error;

pub const WriteSummary = struct {
    has_non_empty: bool = false,
    covers_full_internal: bool = false,
    total_raw_bytes: u64 = 0,
};

pub const ReadSummary = struct {
    has_non_empty: bool = false,
    union_x0: u32 = std.math.maxInt(u32),
    union_y0: u32 = std.math.maxInt(u32),
    union_x1: u32 = 0,
    union_y1: u32 = 0,
};

/// Returns the byte stride between consecutive rows of a `dx`-pixel-wide region
/// with `bpp` bytes per pixel and optional power-of-two `row_pitch_align` (0 or 1 = tightly packed).
pub inline fn rowPitch(dx: u32, bpp: u32, row_pitch_align: u32) u64 {
    const row_bytes: u64 = @as(u64, dx) * @as(u64, bpp);
    if (row_pitch_align <= 1) return row_bytes;
    return std.mem.alignForward(u64, row_bytes, row_pitch_align);
}

/// Returns the expected payload byte length for a `dx` x `dy` region (unpadded last row).
pub inline fn expectedPayloadLen(dx: u32, dy: u32, bpp: u32, row_pitch_align: u32) u64 {
    if (dx == 0 or dy == 0) return 0;
    const row_bytes: u64 = @as(u64, dx) * @as(u64, bpp);
    const pitch = rowPitch(dx, bpp, row_pitch_align);
    return pitch * @as(u64, dy - 1) + row_bytes;
}

/// Validates every region of a writePixels call up front before recording GPU work.
pub fn validateWriteRegions(
    regions: []align(4) const abi.PixelsRegion,
    internal_w: u32,
    internal_h: u32,
    bpp: u32,
    row_pitch_align: u32,
    backend_name: []const u8,
    diag: *Diag,
) Error!WriteSummary {
    var summary = WriteSummary{};
    for (regions) |r| {
        const bounds = r.rect.validate(internal_w, internal_h) orelse {
            diag.print("{s}: writePixels rect ({d},{d})-({d},{d}) out of bounds {d}x{d}", .{
                backend_name, r.rect.x0, r.rect.y0, r.rect.x1, r.rect.y1, internal_w, internal_h,
            });
            return error.InvalidArgument;
        };
        const expected_len = expectedPayloadLen(bounds.dx, bounds.dy, bpp, row_pitch_align);
        if (r.payload_len != expected_len) {
            diag.print("{s}: writePixels payload_len {d} != expected {d}", .{
                backend_name, r.payload_len, expected_len,
            });
            return error.InvalidArgument;
        }
        if (bounds.dx > 0 and bounds.dy > 0) {
            summary.has_non_empty = true;
            summary.total_raw_bytes += r.payload_len;
            if (bounds.x0 == 0 and bounds.y0 == 0 and bounds.dx == internal_w and bounds.dy == internal_h) {
                summary.covers_full_internal = true;
            }
        }
    }
    return summary;
}

/// Validates every region of a readPixels call up front before waiting on the GPU.
pub fn validateReadRegions(
    regions: []const abi.ReadRegion,
    internal_w: u32,
    internal_h: u32,
    bpp: u32,
    backend_name: []const u8,
    diag: *Diag,
) Error!ReadSummary {
    var summary = ReadSummary{};
    for (regions) |r| {
        const bounds = r.rect.validate(internal_w, internal_h) orelse {
            diag.print("{s}: readPixels rect ({d},{d})-({d},{d}) out of bounds {d}x{d}", .{
                backend_name, r.rect.x0, r.rect.y0, r.rect.x1, r.rect.y1, internal_w, internal_h,
            });
            return error.InvalidArgument;
        };
        const expected_len = @as(u64, bounds.dx) * @as(u64, bounds.dy) * bpp;
        if (r.dst_len != expected_len) {
            diag.print("{s}: readPixels dst_len {d} != expected {d}", .{
                backend_name, r.dst_len, expected_len,
            });
            return error.InvalidArgument;
        }
        if (expected_len == 0) continue;
        if (r.dst == null) {
            diag.print("{s}: readPixels dst pointer is null", .{backend_name});
            return error.InvalidArgument;
        }
        summary.has_non_empty = true;
        summary.union_x0 = @min(summary.union_x0, bounds.x0);
        summary.union_y0 = @min(summary.union_y0, bounds.y0);
        summary.union_x1 = @max(summary.union_x1, bounds.x0 + bounds.dx);
        summary.union_y1 = @max(summary.union_y1, bounds.y0 + bounds.dy);
    }
    return summary;
}

test "validateWriteRegions handles tightly packed and pitched layouts" {
    var diag = Diag.none();

    const tight = [_]abi.PixelsRegion{.{
        .rect = .{ .x0 = 0, .y0 = 0, .x1 = 4, .y1 = 4 },
        .payload_offset = 0,
        .payload_len = 4 * 4 * 4,
    }};
    const s1 = try validateWriteRegions(&tight, 4, 4, 4, 0, "test", &diag);
    try std.testing.expect(s1.has_non_empty);
    try std.testing.expect(s1.covers_full_internal);
    try std.testing.expectEqual(@as(u64, 64), s1.total_raw_bytes);

    // 3x2 RGBA8 with 256-byte row pitch: row0 (256 B) + row1 (12 B) = 268 B.
    const pitched = [_]abi.PixelsRegion{.{
        .rect = .{ .x0 = 1, .y0 = 1, .x1 = 4, .y1 = 3 },
        .payload_offset = 0,
        .payload_len = 256 + 12,
    }};
    const s2 = try validateWriteRegions(&pitched, 8, 8, 4, 256, "test", &diag);
    try std.testing.expect(s2.has_non_empty);
    try std.testing.expect(!s2.covers_full_internal);
    try std.testing.expectEqual(@as(u64, 268), s2.total_raw_bytes);

    const bad_len = [_]abi.PixelsRegion{.{
        .rect = .{ .x0 = 0, .y0 = 0, .x1 = 2, .y1 = 2 },
        .payload_offset = 0,
        .payload_len = 12,
    }};
    try std.testing.expectError(error.InvalidArgument, validateWriteRegions(&bad_len, 4, 4, 4, 0, "test", &diag));
}

test "validateReadRegions computes union bounds and rejects null dst" {
    var diag = Diag.none();
    var buf0: [16]u8 = undefined;
    var buf1: [16]u8 = undefined;

    const regs = [_]abi.ReadRegion{
        .{ .rect = .{ .x0 = 1, .y0 = 2, .x1 = 3, .y1 = 4 }, .dst = &buf0, .dst_len = 16 },
        .{ .rect = .{ .x0 = 4, .y0 = 1, .x1 = 6, .y1 = 3 }, .dst = &buf1, .dst_len = 16 },
    };
    const s = try validateReadRegions(&regs, 8, 8, 4, "test", &diag);
    try std.testing.expect(s.has_non_empty);
    try std.testing.expectEqual(@as(u32, 1), s.union_x0);
    try std.testing.expectEqual(@as(u32, 1), s.union_y0);
    try std.testing.expectEqual(@as(u32, 6), s.union_x1);
    try std.testing.expectEqual(@as(u32, 4), s.union_y1);

    const null_dst = [_]abi.ReadRegion{
        .{ .rect = .{ .x0 = 0, .y0 = 0, .x1 = 1, .y1 = 1 }, .dst = null, .dst_len = 4 },
    };
    try std.testing.expectError(error.InvalidArgument, validateReadRegions(&null_dst, 4, 4, 4, "test", &diag));
}
