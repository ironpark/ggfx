const std = @import("std");
const abi = @import("../abi.zig");
const errors = @import("../errors.zig");
const vertex_cache = @import("../vertex_cache.zig");
const win32 = @import("win32.zig");
const d3d12 = @import("d3d12.zig");
const dxgi = @import("dxgi.zig");
const d3dcompiler = @import("d3dcompiler.zig");

const Allocator = std.mem.Allocator;
const Error = errors.Error;
const Diag = errors.Diag;

/// Vertex input layout matching ggfx vertex format (12 floats = 48 bytes per vertex):
/// LOC0: float2 (pos), LOC1: float2 (tex), LOC2: float4 (color), LOC3: float4 (custom)
pub const vertex_stride_bytes: u32 = 12 * @sizeOf(f32);

pub const input_elements = [_]d3d12.D3D12_INPUT_ELEMENT_DESC{
    .{
        .SemanticName = "LOC",
        .SemanticIndex = 0,
        .Format = dxgi.DXGI_FORMAT_R32G32_FLOAT,
        .InputSlot = 0,
        .AlignedByteOffset = d3d12.D3D12_APPEND_ALIGNED_ELEMENT,
        .InputSlotClass = d3d12.D3D12_INPUT_CLASSIFICATION_PER_VERTEX_DATA,
        .InstanceDataStepRate = 0,
    },
    .{
        .SemanticName = "LOC",
        .SemanticIndex = 1,
        .Format = dxgi.DXGI_FORMAT_R32G32_FLOAT,
        .InputSlot = 0,
        .AlignedByteOffset = d3d12.D3D12_APPEND_ALIGNED_ELEMENT,
        .InputSlotClass = d3d12.D3D12_INPUT_CLASSIFICATION_PER_VERTEX_DATA,
        .InstanceDataStepRate = 0,
    },
    .{
        .SemanticName = "LOC",
        .SemanticIndex = 2,
        .Format = dxgi.DXGI_FORMAT_R32G32B32A32_FLOAT,
        .InputSlot = 0,
        .AlignedByteOffset = d3d12.D3D12_APPEND_ALIGNED_ELEMENT,
        .InputSlotClass = d3d12.D3D12_INPUT_CLASSIFICATION_PER_VERTEX_DATA,
        .InstanceDataStepRate = 0,
    },
    .{
        .SemanticName = "LOC",
        .SemanticIndex = 3,
        .Format = dxgi.DXGI_FORMAT_R32G32B32A32_FLOAT,
        .InputSlot = 0,
        .AlignedByteOffset = d3d12.D3D12_APPEND_ALIGNED_ELEMENT,
        .InputSlotClass = d3d12.D3D12_INPUT_CLASSIFICATION_PER_VERTEX_DATA,
        .InstanceDataStepRate = 0,
    },
};

pub const OwnedBytecode = union(enum) {
    blob: *d3dcompiler.ID3DBlob,
    owned: []u8,

    pub fn slice(self: OwnedBytecode) []const u8 {
        return switch (self) {
            .blob => |b| b.slice(),
            .owned => |buf| buf,
        };
    }

    pub fn deinit(self: *OwnedBytecode, gpa: Allocator) void {
        switch (self.*) {
            .blob => |b| _ = b.release(),
            .owned => |buf| gpa.free(buf),
        }
    }
};

pub const PsoKey = struct {
    rtv_format: dxgi.DXGI_FORMAT,
    src_rgb: u8,
    src_a: u8,
    dst_rgb: u8,
    dst_a: u8,
    op_rgb: u8,
    op_a: u8,

    pub fn init(blend: abi.Blend, rtv_format: dxgi.DXGI_FORMAT) PsoKey {
        return .{
            .rtv_format = rtv_format,
            .src_rgb = blend.src_rgb,
            .src_a = blend.src_a,
            .dst_rgb = blend.dst_rgb,
            .dst_a = blend.dst_a,
            .op_rgb = blend.op_rgb,
            .op_a = blend.op_a,
        };
    }
};

pub const Shader = struct {
    texture_count: u32,
    user_cb_size: u32,
    flags: u32,
    /// Borrowed from PipelineCache.vs_cache (lives for the lifetime of PipelineCache).
    vs_bytecode: []const u8,
    ps_bytecode: OwnedBytecode,
    psos: std.AutoHashMapUnmanaged(PsoKey, *d3d12.ID3D12PipelineState) = .empty,

    pub fn deinit(self: *Shader, gpa: Allocator, live_pipelines: *u64) void {
        var it = self.psos.valueIterator();
        while (it.next()) |pso_ptr| {
            _ = pso_ptr.*.release();
            if (live_pipelines.* > 0) live_pipelines.* -= 1;
        }
        self.psos.deinit(gpa);
        self.ps_bytecode.deinit(gpa);
    }
};

fn destroyOwnedBytecode(gpa: Allocator, entry: *OwnedBytecode) void {
    entry.deinit(gpa);
}

pub const PipelineCache = struct {
    root_signature: *d3d12.ID3D12RootSignature,
    allow_dxil: bool,
    vs_cache: vertex_cache.VertexStageCache(OwnedBytecode) = .{},

    pub fn init(
        device: *d3d12.ID3D12Device,
        d3d12_lib: *const d3d12.D3D12Lib,
        allow_dxil: bool,
        diag: *Diag,
    ) Error!PipelineCache {
        // Root Signature v1.0 (§4.2):
        //   [0] Root CBV b0 (VISIBILITY_ALL) — internal uniforms
        //   [1] Root CBV b1 (VISIBILITY_PIXEL) — user uniforms
        //   [2] Descriptor Table SRV t0..t3 (VISIBILITY_PIXEL) — source textures
        const srv_range = [1]d3d12.D3D12_DESCRIPTOR_RANGE{.{
            .RangeType = d3d12.D3D12_DESCRIPTOR_RANGE_TYPE_SRV,
            .NumDescriptors = abi.max_src_images,
            .BaseShaderRegister = 0,
            .RegisterSpace = 0,
            .OffsetInDescriptorsFromTableStart = 0,
        }};

        const root_params = [3]d3d12.D3D12_ROOT_PARAMETER{
            .{
                .ParameterType = d3d12.D3D12_ROOT_PARAMETER_TYPE_CBV,
                .u = .{ .Descriptor = .{ .ShaderRegister = 0, .RegisterSpace = 0 } },
                .ShaderVisibility = d3d12.D3D12_SHADER_VISIBILITY_ALL,
            },
            .{
                .ParameterType = d3d12.D3D12_ROOT_PARAMETER_TYPE_CBV,
                .u = .{ .Descriptor = .{ .ShaderRegister = 1, .RegisterSpace = 0 } },
                .ShaderVisibility = d3d12.D3D12_SHADER_VISIBILITY_PIXEL,
            },
            .{
                .ParameterType = d3d12.D3D12_ROOT_PARAMETER_TYPE_DESCRIPTOR_TABLE,
                .u = .{ .DescriptorTable = .{ .NumDescriptorRanges = 1, .pDescriptorRanges = &srv_range } },
                .ShaderVisibility = d3d12.D3D12_SHADER_VISIBILITY_PIXEL,
            },
        };

        const rs_desc = d3d12.D3D12_ROOT_SIGNATURE_DESC{
            .NumParameters = root_params.len,
            .pParameters = &root_params,
            .NumStaticSamplers = 0,
            .pStaticSamplers = null,
            .Flags = d3d12.D3D12_ROOT_SIGNATURE_FLAG_ALLOW_INPUT_ASSEMBLER_INPUT_LAYOUT,
        };

        var sig_blob: ?*d3dcompiler.ID3DBlob = null;
        var err_blob: ?*d3dcompiler.ID3DBlob = null;
        defer if (err_blob) |eb| {
            _ = eb.release();
        };

        const ser_hr = d3d12_lib.serialize_root_signature(
            &rs_desc,
            d3d12.D3D_ROOT_SIGNATURE_VERSION_1_0,
            &sig_blob,
            &err_blob,
        );
        if (win32.failed(ser_hr) or sig_blob == null) {
            if (sig_blob) |sb| _ = sb.release();
            if (err_blob) |eb| {
                const msg = std.mem.trimEnd(u8, eb.slice(), "\r\n\x00 ");
                diag.print("directx: D3D12SerializeRootSignature failed (0x{x:0>8}): {s}", .{
                    win32.hrHex(ser_hr),
                    msg,
                });
            } else {
                diag.print("directx: D3D12SerializeRootSignature failed (0x{x:0>8})", .{win32.hrHex(ser_hr)});
            }
            return win32.hrToError(ser_hr);
        }
        const blob = sig_blob.?;
        defer _ = blob.release();

        const blob_ptr = blob.getBufferPointer() orelse {
            diag.set("directx: serialized root signature blob has null pointer");
            return error.Internal;
        };
        var rs: ?*d3d12.ID3D12RootSignature = null;
        const create_hr = device.createRootSignature(0, blob_ptr, blob.getBufferSize(), &rs);
        if (win32.failed(create_hr) or rs == null) {
            diag.print("directx: CreateRootSignature failed (0x{x:0>8})", .{win32.hrHex(create_hr)});
            return win32.hrToError(create_hr);
        }

        return .{
            .root_signature = rs.?,
            .allow_dxil = allow_dxil,
        };
    }

    pub fn deinit(self: *PipelineCache, gpa: Allocator) void {
        self.vs_cache.deinit(gpa, gpa, destroyOwnedBytecode);
        _ = self.root_signature.release();
    }

    pub fn compileShader(
        self: *PipelineCache,
        gpa: Allocator,
        compiler: *const d3dcompiler.CompilerLib,
        desc: *const abi.ShaderDesc,
        diag: *Diag,
    ) Error!Shader {
        if (desc.texture_count > abi.max_src_images) {
            diag.print("directx: shader texture_count {d} > {d}", .{ desc.texture_count, abi.max_src_images });
            return error.InvalidArgument;
        }
        if (desc.vertex.code == null or desc.vertex.code_len == 0) {
            diag.set("directx: vertex shader code is empty");
            return error.InvalidArgument;
        }
        if (desc.fragment.code == null or desc.fragment.code_len == 0) {
            diag.set("directx: fragment shader code is empty");
            return error.InvalidArgument;
        }

        const format: abi.ShaderFormat = @enumFromInt(desc.format);
        switch (format) {
            .hlsl, .dxbc => {},
            .dxil => {
                if (!self.allow_dxil) {
                    diag.set("directx: DXIL shaders require GGFX_DEVICE_ALLOW_DXIL");
                    return error.Unsupported;
                }
            },
            else => {
                diag.print("directx: unsupported shader format {d}", .{desc.format});
                return error.Unsupported;
            },
        }

        const vs_len = std.math.cast(usize, desc.vertex.code_len) orelse return error.InvalidArgument;
        const ps_len = std.math.cast(usize, desc.fragment.code_len) orelse return error.InvalidArgument;
        const vs_code = desc.vertex.code.?[0..vs_len];
        const ps_code = desc.fragment.code.?[0..ps_len];

        const vs_entry: [*:0]const u8 = desc.vertex.entry orelse "main";
        const ps_entry: [*:0]const u8 = desc.fragment.entry orelse "main";
        const vs_entry_slice = std.mem.span(vs_entry);

        // Vertex shader bytecode (cached by exact source/entry/format bytes across shaders in this device).
        const vs_bytecode: []const u8 = if (self.vs_cache.getPtr(vs_code, vs_entry_slice, desc.format)) |cached|
            cached.slice()
        else blk: {
            var compiled_vs: OwnedBytecode = switch (format) {
                .hlsl => .{ .blob = try compiler.compile(vs_code, vs_entry, "vs_5_0", diag) },
                .dxbc, .dxil => .{ .owned = gpa.dupe(u8, vs_code) catch return error.OutOfMemory },
                else => unreachable,
            };
            errdefer compiled_vs.deinit(gpa);
            const s = compiled_vs.slice();
            try self.vs_cache.put(gpa, vs_code, vs_entry_slice, desc.format, compiled_vs);
            break :blk s;
        };

        // Pixel shader bytecode (owned by Shader).
        var ps_bytecode: OwnedBytecode = switch (format) {
            .hlsl => .{ .blob = try compiler.compile(ps_code, ps_entry, "ps_5_0", diag) },
            .dxbc, .dxil => .{ .owned = gpa.dupe(u8, ps_code) catch return error.OutOfMemory },
            else => unreachable,
        };
        errdefer ps_bytecode.deinit(gpa);

        // Align user CB size up to 16 bytes (consistent with Go shader_windows.go).
        const raw_user_bytes = std.math.mul(u32, desc.user_uniform_dwords, 4) catch return error.InvalidArgument;
        const user_cb_size = if (raw_user_bytes == 0) 0 else ((raw_user_bytes + 15) & ~@as(u32, 15));

        return .{
            .texture_count = desc.texture_count,
            .user_cb_size = user_cb_size,
            .flags = desc.flags,
            .vs_bytecode = vs_bytecode,
            .ps_bytecode = ps_bytecode,
        };
    }

    pub fn getOrCreatePso(
        self: *PipelineCache,
        gpa: Allocator,
        device: *d3d12.ID3D12Device,
        shader: *Shader,
        blend: abi.Blend,
        rtv_format: dxgi.DXGI_FORMAT,
        live_pipelines: *u64,
        diag: *Diag,
    ) Error!*d3d12.ID3D12PipelineState {
        const key = PsoKey.init(blend, rtv_format);
        if (shader.psos.get(key)) |existing| {
            return existing;
        }

        const src_rgb = try blendFactorToD3D12(blend.src_rgb, false, diag);
        const dst_rgb = try blendFactorToD3D12(blend.dst_rgb, false, diag);
        const op_rgb = try blendOpToD3D12(blend.op_rgb, diag);
        const src_a = try blendFactorToD3D12(blend.src_a, true, diag);
        const dst_a = try blendFactorToD3D12(blend.dst_a, true, diag);
        const op_a = try blendOpToD3D12(blend.op_a, diag);

        const ps_bytes = shader.ps_bytecode.slice();
        var pso_desc = d3d12.D3D12_GRAPHICS_PIPELINE_STATE_DESC{
            .pRootSignature = self.root_signature,
            .VS = .{
                .pShaderBytecode = shader.vs_bytecode.ptr,
                .BytecodeLength = shader.vs_bytecode.len,
            },
            .PS = .{
                .pShaderBytecode = ps_bytes.ptr,
                .BytecodeLength = ps_bytes.len,
            },
            .BlendState = .{
                .AlphaToCoverageEnable = 0,
                .IndependentBlendEnable = 0,
                .RenderTarget = @splat(.{}),
            },
            .SampleMask = 0xFFFF_FFFF,
            .RasterizerState = .{
                .FillMode = d3d12.D3D12_FILL_MODE_SOLID,
                .CullMode = d3d12.D3D12_CULL_MODE_NONE,
                .FrontCounterClockwise = 0,
                .DepthBias = d3d12.D3D12_DEFAULT_DEPTH_BIAS,
                .DepthBiasClamp = d3d12.D3D12_DEFAULT_DEPTH_BIAS_CLAMP,
                .SlopeScaledDepthBias = d3d12.D3D12_DEFAULT_SLOPE_SCALED_DEPTH_BIAS,
                .DepthClipEnable = 0,
                .MultisampleEnable = 0,
                .AntialiasedLineEnable = 0,
                .ForcedSampleCount = 0,
                .ConservativeRaster = d3d12.D3D12_CONSERVATIVE_RASTERIZATION_MODE_OFF,
            },
            .DepthStencilState = .{
                .DepthEnable = 0,
                .DepthWriteMask = d3d12.D3D12_DEPTH_WRITE_MASK_ZERO,
                .DepthFunc = d3d12.D3D12_COMPARISON_FUNC_ALWAYS,
                .StencilEnable = 0,
                .StencilReadMask = 0xFF,
                .StencilWriteMask = 0xFF,
                .FrontFace = .{},
                .BackFace = .{},
            },
            .InputLayout = .{
                .pInputElementDescs = &input_elements,
                .NumElements = input_elements.len,
            },
            .IBStripCutValue = d3d12.D3D12_INDEX_BUFFER_STRIP_CUT_VALUE_DISABLED,
            .PrimitiveTopologyType = d3d12.D3D12_PRIMITIVE_TOPOLOGY_TYPE_TRIANGLE,
            .NumRenderTargets = 1,
            .RTVFormats = @splat(dxgi.DXGI_FORMAT_UNKNOWN),
            .DSVFormat = dxgi.DXGI_FORMAT_UNKNOWN,
            .SampleDesc = .{ .Count = 1, .Quality = 0 },
            .NodeMask = 0,
            .CachedPSO = .{},
            .Flags = d3d12.D3D12_PIPELINE_STATE_FLAG_NONE,
        };
        pso_desc.BlendState.RenderTarget[0] = .{
            .BlendEnable = 1,
            .LogicOpEnable = 0,
            .SrcBlend = src_rgb,
            .DestBlend = dst_rgb,
            .BlendOp = op_rgb,
            .SrcBlendAlpha = src_a,
            .DestBlendAlpha = dst_a,
            .BlendOpAlpha = op_a,
            .LogicOp = d3d12.D3D12_LOGIC_OP_NOOP,
            .RenderTargetWriteMask = d3d12.D3D12_COLOR_WRITE_ENABLE_ALL,
        };
        pso_desc.RTVFormats[0] = rtv_format;

        var pso: ?*d3d12.ID3D12PipelineState = null;
        const hr = device.createGraphicsPipelineState(&pso_desc, &pso);
        if (win32.failed(hr) or pso == null) {
            diag.print("directx: CreateGraphicsPipelineState failed (0x{x:0>8})", .{win32.hrHex(hr)});
            return win32.hrToError(hr);
        }
        const created = pso.?;
        shader.psos.put(gpa, key, created) catch {
            _ = created.release();
            return error.OutOfMemory;
        };
        live_pipelines.* += 1;
        return created;
    }
};

pub fn blendFactorToD3D12(factor: u8, alpha: bool, diag: *Diag) Error!d3d12.D3D12_BLEND {
    return switch (factor) {
        0 => d3d12.D3D12_BLEND_ZERO,
        1 => d3d12.D3D12_BLEND_ONE,
        2 => if (alpha) d3d12.D3D12_BLEND_SRC_ALPHA else d3d12.D3D12_BLEND_SRC_COLOR,
        3 => if (alpha) d3d12.D3D12_BLEND_INV_SRC_ALPHA else d3d12.D3D12_BLEND_INV_SRC_COLOR,
        4 => d3d12.D3D12_BLEND_SRC_ALPHA,
        5 => d3d12.D3D12_BLEND_INV_SRC_ALPHA,
        6 => if (alpha) d3d12.D3D12_BLEND_DEST_ALPHA else d3d12.D3D12_BLEND_DEST_COLOR,
        7 => if (alpha) d3d12.D3D12_BLEND_INV_DEST_ALPHA else d3d12.D3D12_BLEND_INV_DEST_COLOR,
        8 => d3d12.D3D12_BLEND_DEST_ALPHA,
        9 => d3d12.D3D12_BLEND_INV_DEST_ALPHA,
        10 => d3d12.D3D12_BLEND_SRC_ALPHA_SAT,
        else => {
            diag.print("directx: invalid blend factor {d}", .{factor});
            return error.InvalidArgument;
        },
    };
}

pub fn blendOpToD3D12(op: u8, diag: *Diag) Error!d3d12.D3D12_BLEND_OP {
    return switch (op) {
        0 => d3d12.D3D12_BLEND_OP_ADD,
        1 => d3d12.D3D12_BLEND_OP_SUBTRACT,
        2 => d3d12.D3D12_BLEND_OP_REV_SUBTRACT,
        3 => d3d12.D3D12_BLEND_OP_MIN,
        4 => d3d12.D3D12_BLEND_OP_MAX,
        else => {
            diag.print("directx: invalid blend operation {d}", .{op});
            return error.InvalidArgument;
        },
    };
}
