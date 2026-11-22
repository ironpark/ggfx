const std = @import("std");
const Allocator = std.mem.Allocator;
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const mtl = @import("mtl.zig");

const Diag = errors.Diag;
const Error = errors.Error;

pub const VertexCacheKey = struct {
    code_hash: u64,
    code_len: usize,
    entry_hash: u64,
    entry_len: usize,
};

pub const VertexStage = struct {
    lib: mtl.Library,
    func: mtl.Function,
};

/// Caches compiled MSL vertex libraries/functions keyed by Wyhash(code)+len (+entry).
pub const VertexStageCache = struct {
    map: std.AutoHashMapUnmanaged(VertexCacheKey, VertexStage) = .{},

    pub fn deinit(self: *VertexStageCache, gpa: Allocator) void {
        var it = self.map.valueIterator();
        while (it.next()) |vs| {
            vs.func.release();
            vs.lib.release();
        }
        self.map.deinit(gpa);
    }

    pub fn getOrCompile(
        self: *VertexStageCache,
        gpa: Allocator,
        device: mtl.Device,
        code: []const u8,
        entry: []const u8,
        diag: *Diag,
    ) Error!mtl.Function {
        const key = VertexCacheKey{
            .code_hash = std.hash.Wyhash.hash(0, code),
            .code_len = code.len,
            .entry_hash = std.hash.Wyhash.hash(0, entry),
            .entry_len = entry.len,
        };
        if (self.map.get(key)) |cached| {
            return cached.func;
        }

        const lib = try device.newLibraryWithSource(code, diag);
        errdefer lib.release();
        const func = try lib.newFunctionWithName(entry, diag);
        errdefer func.release();

        self.map.put(gpa, key, .{ .lib = lib, .func = func }) catch return error.OutOfMemory;
        return func;
    }
};

pub const RpsKey = struct {
    pix: mtl.PixelFormat,
    src_rgb: u8,
    src_a: u8,
    dst_rgb: u8,
    dst_a: u8,
    op_rgb: u8,
    op_a: u8,

    pub fn init(pix: mtl.PixelFormat, b: abi.Blend) RpsKey {
        return .{
            .pix = pix,
            .src_rgb = b.src_rgb,
            .src_a = b.src_a,
            .dst_rgb = b.dst_rgb,
            .dst_a = b.dst_a,
            .op_rgb = b.op_rgb,
            .op_a = b.op_a,
        };
    }
};

pub const Shader = struct {
    lib: mtl.Library,
    fs: mtl.Function,
    /// Borrowed from Backend's VertexStageCache; must NOT be released by Shader.deinit.
    vs: mtl.Function,
    rpss: std.AutoHashMapUnmanaged(RpsKey, mtl.RenderPipelineState) = .{},
    texture_count: u32,
    user_uniform_dwords: u32,
    fragment_reads_internal: bool,

    pub fn deinit(self: *Shader, gpa: Allocator) u64 {
        const released_pipelines: u64 = self.rpss.count();
        var it = self.rpss.valueIterator();
        while (it.next()) |rps| {
            rps.release();
        }
        self.rpss.deinit(gpa);
        self.fs.release();
        self.lib.release();
        return released_pipelines;
    }

    pub const PipelineLookup = struct {
        rps: mtl.RenderPipelineState,
        created_new: bool,
    };

    pub fn getOrCreatePipeline(
        self: *Shader,
        gpa: Allocator,
        device: mtl.Device,
        pix: mtl.PixelFormat,
        blend: abi.Blend,
        diag: *Diag,
    ) Error!PipelineLookup {
        const key = RpsKey.init(pix, blend);
        if (self.rpss.get(key)) |rps| {
            return .{ .rps = rps, .created_new = false };
        }

        const src_rgb = blendFactorToMetal(blend.src_rgb) catch {
            diag.print("metal: invalid blend src_rgb factor {d}", .{blend.src_rgb});
            return error.InvalidArgument;
        };
        const src_a = blendFactorToMetal(blend.src_a) catch {
            diag.print("metal: invalid blend src_a factor {d}", .{blend.src_a});
            return error.InvalidArgument;
        };
        const dst_rgb = blendFactorToMetal(blend.dst_rgb) catch {
            diag.print("metal: invalid blend dst_rgb factor {d}", .{blend.dst_rgb});
            return error.InvalidArgument;
        };
        const dst_a = blendFactorToMetal(blend.dst_a) catch {
            diag.print("metal: invalid blend dst_a factor {d}", .{blend.dst_a});
            return error.InvalidArgument;
        };
        const op_rgb = blendOperationToMetal(blend.op_rgb) catch {
            diag.print("metal: invalid blend op_rgb {d}", .{blend.op_rgb});
            return error.InvalidArgument;
        };
        const op_a = blendOperationToMetal(blend.op_a) catch {
            diag.print("metal: invalid blend op_a {d}", .{blend.op_a});
            return error.InvalidArgument;
        };

        const rps = try device.newRenderPipelineState(
            self.vs,
            self.fs,
            pix,
            src_rgb,
            src_a,
            dst_rgb,
            dst_a,
            op_rgb,
            op_a,
            diag,
        );
        errdefer rps.release();

        self.rpss.put(gpa, key, rps) catch return error.OutOfMemory;
        return .{ .rps = rps, .created_new = true };
    }
};

pub fn blendFactorToMetal(c: u8) Error!mtl.BlendFactor {
    return switch (c) {
        0 => .zero,
        1 => .one,
        2 => .source_color,
        3 => .one_minus_source_color,
        4 => .source_alpha,
        5 => .one_minus_source_alpha,
        6 => .destination_color,
        7 => .one_minus_destination_color,
        8 => .destination_alpha,
        9 => .one_minus_destination_alpha,
        10 => .source_alpha_saturated,
        else => error.InvalidArgument,
    };
}

pub fn blendOperationToMetal(o: u8) Error!mtl.BlendOperation {
    return switch (o) {
        0 => .add,
        1 => .subtract,
        2 => .reverse_subtract,
        3 => .min,
        4 => .max,
        else => error.InvalidArgument,
    };
}
