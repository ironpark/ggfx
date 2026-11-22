const std = @import("std");

const BackendOpt = enum {
    auto,
    null,
};

const cross_targets = [_]std.Target.Query{
    .{ .cpu_arch = .aarch64, .os_tag = .macos },
    .{ .cpu_arch = .x86_64, .os_tag = .macos },
    .{ .cpu_arch = .x86_64, .os_tag = .windows, .abi = .gnu },
    .{ .cpu_arch = .aarch64, .os_tag = .windows, .abi = .gnu },
    .{ .cpu_arch = .x86_64, .os_tag = .linux, .abi = .gnu },
    .{ .cpu_arch = .aarch64, .os_tag = .linux, .abi = .gnu },
};

fn addDriverLibrary(
    b: *std.Build,
    target: std.Build.ResolvedTarget,
    optimize: std.builtin.OptimizeMode,
    null_backend: bool,
) *std.Build.Step.Compile {
    const os = target.result.os.tag;
    const opts = b.addOptions();
    opts.addOption(bool, "null_backend", null_backend);

    const mod = b.createModule(.{
        .root_source_file = b.path("src/root.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = (os == .macos or os == .linux),
        .pic = true,
    });
    mod.addOptions("build_options", opts);

    return b.addLibrary(.{
        .linkage = .dynamic,
        .name = "ggfx_driver",
        .root_module = mod,
        .use_llvm = true,
    });
}

pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const backend_opt = b.option(
        BackendOpt,
        "backend",
        "auto = OS native, null = test backend",
    ) orelse .auto;

    const lib = addDriverLibrary(b, target, optimize, backend_opt == .null);
    b.installArtifact(lib);

    // Test step: always uses null backend + runs C ABI static assertions.
    const test_step = b.step("test", "Run Zig unit tests (null backend) and C ABI header check");

    const test_opts = b.addOptions();
    test_opts.addOption(bool, "null_backend", true);

    const host_os = target.result.os.tag;
    const link_libc_host = (host_os == .macos or host_os == .linux);

    const src_mod = b.createModule(.{
        .root_source_file = b.path("src/root.zig"),
        .target = target,
        .optimize = optimize,
        .link_libc = link_libc_host,
    });
    src_mod.addOptions("build_options", test_opts);

    const root_tests = b.addTest(.{
        .root_module = src_mod,
        .use_llvm = true,
    });
    test_step.dependOn(&b.addRunArtifact(root_tests).step);

    const test_files = [_][]const u8{
        "tests/replayer_test.zig",
        "tests/handles_test.zig",
        "tests/alloc_test.zig",
    };
    for (test_files) |tf| {
        const tm = b.createModule(.{
            .root_source_file = b.path(tf),
            .target = target,
            .optimize = optimize,
            .link_libc = link_libc_host,
        });
        tm.addOptions("build_options", test_opts);
        tm.addImport("ggfx", src_mod);

        const unit_test = b.addTest(.{
            .root_module = tm,
            .use_llvm = true,
        });
        test_step.dependOn(&b.addRunArtifact(unit_test).step);
    }

    // C header _Static_assert verification via `zig cc`.
    const c_abi_check = b.addSystemCommand(&.{
        b.graph.zig_exe,
        "cc",
        "-std=c11",
        "-Wall",
        "-Wextra",
        "-Werror",
        "-c",
        "-Iinclude",
        "tests/abi_check.c",
        "-o",
    });
    _ = c_abi_check.addOutputFileArg("abi_check.o");
    test_step.dependOn(&c_abi_check.step);

    // Cross-compilation verification step across all 6 desktop targets.
    const cross_step = b.step("cross", "Build dynamic library for all 6 desktop targets");
    for (cross_targets) |query| {
        const resolved = b.resolveTargetQuery(query);
        const cross_lib = addDriverLibrary(b, resolved, optimize, backend_opt == .null);
        const triple = query.zigTriple(b.allocator) catch @panic("OOM");
        const install = b.addInstallArtifact(cross_lib, .{
            .dest_dir = .{
                .override = .{
                    .custom = b.fmt("cross/{s}", .{triple}),
                },
            },
        });
        cross_step.dependOn(&install.step);
    }
}
