/* abi_check.c — Compile-time verification of every struct size and field offset in ggfx_driver.h. */
#include <stddef.h>
#include "ggfx_driver.h"

#define CHECK_SIZE(T, N) _Static_assert(sizeof(T) == (N), "sizeof(" #T ") != " #N)
#define CHECK_OFF(T, F, K) _Static_assert(offsetof(T, F) == (K), "offsetof(" #T ", " #F ") != " #K)

_Static_assert(sizeof(void*) == 8, "64-bit platform required");
_Static_assert(GGFX_DRIVER_ABI_VERSION == 2u, "GGFX_DRIVER_ABI_VERSION must be 2");

/* GgfxRect: 16 B */
CHECK_SIZE(GgfxRect, 16);
CHECK_OFF(GgfxRect, x0, 0);
CHECK_OFF(GgfxRect, y0, 4);
CHECK_OFF(GgfxRect, x1, 8);
CHECK_OFF(GgfxRect, y1, 12);

/* GgfxBlend: 6 B */
CHECK_SIZE(GgfxBlend, 6);
CHECK_OFF(GgfxBlend, src_rgb, 0);
CHECK_OFF(GgfxBlend, src_a, 1);
CHECK_OFF(GgfxBlend, dst_rgb, 2);
CHECK_OFF(GgfxBlend, dst_a, 3);
CHECK_OFF(GgfxBlend, op_rgb, 4);
CHECK_OFF(GgfxBlend, op_a, 5);

/* GgfxDeviceOptions: 48 B */
CHECK_SIZE(GgfxDeviceOptions, 48);
CHECK_OFF(GgfxDeviceOptions, struct_size, 0);
CHECK_OFF(GgfxDeviceOptions, abi_version, 4);
CHECK_OFF(GgfxDeviceOptions, backend, 8);
CHECK_OFF(GgfxDeviceOptions, color_space, 12);
CHECK_OFF(GgfxDeviceOptions, flags, 16);
CHECK_OFF(GgfxDeviceOptions, frames_in_flight, 20);
CHECK_OFF(GgfxDeviceOptions, dx_feature_level, 24);
CHECK_OFF(GgfxDeviceOptions, main_thread_mode, 28);
CHECK_OFF(GgfxDeviceOptions, reserved, 32);

/* GgfxDriverCaps: 96 B */
CHECK_SIZE(GgfxDriverCaps, 96);
CHECK_OFF(GgfxDriverCaps, struct_size, 0);
CHECK_OFF(GgfxDriverCaps, backend, 4);
CHECK_OFF(GgfxDriverCaps, max_image_size, 8);
CHECK_OFF(GgfxDriverCaps, flags, 12);
CHECK_OFF(GgfxDriverCaps, shader_format, 16);
CHECK_OFF(GgfxDriverCaps, shader_version, 20);
CHECK_OFF(GgfxDriverCaps, color_space, 24);
CHECK_OFF(GgfxDriverCaps, frames_in_flight, 28);
CHECK_OFF(GgfxDriverCaps, device_name, 32);

/* GgfxCmdHeader: 8 B */
CHECK_SIZE(GgfxCmdHeader, 8);
CHECK_OFF(GgfxCmdHeader, op, 0);
CHECK_OFF(GgfxCmdHeader, flags, 1);
CHECK_OFF(GgfxCmdHeader, aux, 2);
CHECK_OFF(GgfxCmdHeader, size, 4);

/* GgfxBatch: 48 B */
CHECK_SIZE(GgfxBatch, 48);
CHECK_OFF(GgfxBatch, struct_size, 0);
CHECK_OFF(GgfxBatch, cmd_count, 4);
CHECK_OFF(GgfxBatch, cmds, 8);
CHECK_OFF(GgfxBatch, cmds_len, 16);
CHECK_OFF(GgfxBatch, payload, 24);
CHECK_OFF(GgfxBatch, payload_len, 32);
CHECK_OFF(GgfxBatch, sequence, 40);

/* GgfxSubmitResult: 24 B */
CHECK_SIZE(GgfxSubmitResult, 24);
CHECK_OFF(GgfxSubmitResult, flags, 0);
CHECK_OFF(GgfxSubmitResult, presented_surfaces, 4);
CHECK_OFF(GgfxSubmitResult, completed_frames, 8);
CHECK_OFF(GgfxSubmitResult, reserved, 16);

/* GgfxShaderStage: 32 B */
CHECK_SIZE(GgfxShaderStage, 32);
CHECK_OFF(GgfxShaderStage, code, 0);
CHECK_OFF(GgfxShaderStage, code_len, 8);
CHECK_OFF(GgfxShaderStage, entry, 16);
CHECK_OFF(GgfxShaderStage, entry_len, 24);
CHECK_OFF(GgfxShaderStage, reserved, 28);

/* GgfxShaderDesc: 88 B */
CHECK_SIZE(GgfxShaderDesc, 88);
CHECK_OFF(GgfxShaderDesc, struct_size, 0);
CHECK_OFF(GgfxShaderDesc, format, 4);
CHECK_OFF(GgfxShaderDesc, texture_count, 8);
CHECK_OFF(GgfxShaderDesc, user_uniform_dwords, 12);
CHECK_OFF(GgfxShaderDesc, flags, 16);
CHECK_OFF(GgfxShaderDesc, reserved, 20);
CHECK_OFF(GgfxShaderDesc, vertex, 24);
CHECK_OFF(GgfxShaderDesc, fragment, 56);

/* GgfxSurfaceDesc: 40 B */
CHECK_SIZE(GgfxSurfaceDesc, 40);
CHECK_OFF(GgfxSurfaceDesc, struct_size, 0);
CHECK_OFF(GgfxSurfaceDesc, kind, 4);
CHECK_OFF(GgfxSurfaceDesc, flags, 8);
CHECK_OFF(GgfxSurfaceDesc, reserved, 12);
CHECK_OFF(GgfxSurfaceDesc, handle1, 16);
CHECK_OFF(GgfxSurfaceDesc, handle2, 24);
CHECK_OFF(GgfxSurfaceDesc, width_hint, 32);
CHECK_OFF(GgfxSurfaceDesc, height_hint, 36);

/* GgfxSurfaceCaps: 16 B */
CHECK_SIZE(GgfxSurfaceCaps, 16);
CHECK_OFF(GgfxSurfaceCaps, struct_size, 0);
CHECK_OFF(GgfxSurfaceCaps, flags, 4);
CHECK_OFF(GgfxSurfaceCaps, native_format, 8);
CHECK_OFF(GgfxSurfaceCaps, reserved, 12);

/* GgfxReadRegion: 32 B */
CHECK_SIZE(GgfxReadRegion, 32);
CHECK_OFF(GgfxReadRegion, rect, 0);
CHECK_OFF(GgfxReadRegion, dst, 16);
CHECK_OFF(GgfxReadRegion, dst_len, 24);

/* GgfxDebugStats: 64 B */
CHECK_SIZE(GgfxDebugStats, 64);
CHECK_OFF(GgfxDebugStats, live_images, 0);
CHECK_OFF(GgfxDebugStats, live_shaders, 8);
CHECK_OFF(GgfxDebugStats, live_surfaces, 16);
CHECK_OFF(GgfxDebugStats, live_pipelines, 24);
CHECK_OFF(GgfxDebugStats, submits, 32);
CHECK_OFF(GgfxDebugStats, draws, 40);
CHECK_OFF(GgfxDebugStats, binds_skipped, 48);
CHECK_OFF(GgfxDebugStats, uploaded_bytes, 56);

/* GgfxUploadArena: 40 B */
CHECK_SIZE(GgfxUploadArena, 40);
CHECK_OFF(GgfxUploadArena, struct_size, 0);
CHECK_OFF(GgfxUploadArena, arena_id, 4);
CHECK_OFF(GgfxUploadArena, ptr, 8);
CHECK_OFF(GgfxUploadArena, capacity, 16);
CHECK_OFF(GgfxUploadArena, alignment, 24);
CHECK_OFF(GgfxUploadArena, flags, 28);
CHECK_OFF(GgfxUploadArena, row_pitch_alignment, 32);
CHECK_OFF(GgfxUploadArena, reserved, 36);

/* Command records */
CHECK_SIZE(GgfxCmdSetVertices, 32);
CHECK_OFF(GgfxCmdSetVertices, hdr, 0);
CHECK_OFF(GgfxCmdSetVertices, vertices, 8);
CHECK_OFF(GgfxCmdSetVertices, indices, 16);
CHECK_OFF(GgfxCmdSetVertices, vertex_float_count, 24);
CHECK_OFF(GgfxCmdSetVertices, index_count, 28);

CHECK_SIZE(GgfxCmdNewImage, 32);
CHECK_OFF(GgfxCmdNewImage, hdr, 0);
CHECK_OFF(GgfxCmdNewImage, id, 8);
CHECK_OFF(GgfxCmdNewImage, width, 12);
CHECK_OFF(GgfxCmdNewImage, height, 16);
CHECK_OFF(GgfxCmdNewImage, internal_width, 20);
CHECK_OFF(GgfxCmdNewImage, internal_height, 24);
CHECK_OFF(GgfxCmdNewImage, reserved, 28);

CHECK_SIZE(GgfxCmdNewScreenImage, 24);
CHECK_OFF(GgfxCmdNewScreenImage, hdr, 0);
CHECK_OFF(GgfxCmdNewScreenImage, id, 8);
CHECK_OFF(GgfxCmdNewScreenImage, surface_id, 12);
CHECK_OFF(GgfxCmdNewScreenImage, width, 16);
CHECK_OFF(GgfxCmdNewScreenImage, height, 20);

CHECK_SIZE(GgfxCmdDisposeImage, 16);
CHECK_OFF(GgfxCmdDisposeImage, hdr, 0);
CHECK_OFF(GgfxCmdDisposeImage, id, 8);
CHECK_OFF(GgfxCmdDisposeImage, reserved, 12);

CHECK_SIZE(GgfxCmdDisposeShader, 16);
CHECK_OFF(GgfxCmdDisposeShader, hdr, 0);
CHECK_OFF(GgfxCmdDisposeShader, id, 8);
CHECK_OFF(GgfxCmdDisposeShader, reserved, 12);

CHECK_SIZE(GgfxCmdDisposeSurface, 16);
CHECK_OFF(GgfxCmdDisposeSurface, hdr, 0);
CHECK_OFF(GgfxCmdDisposeSurface, surface_id, 8);
CHECK_OFF(GgfxCmdDisposeSurface, reserved, 12);

CHECK_SIZE(GgfxPixelsRegion, 24);
CHECK_OFF(GgfxPixelsRegion, rect, 0);
CHECK_OFF(GgfxPixelsRegion, payload_offset, 16);
CHECK_OFF(GgfxPixelsRegion, payload_len, 20);

CHECK_SIZE(GgfxCmdWritePixels, 16);
CHECK_OFF(GgfxCmdWritePixels, hdr, 0);
CHECK_OFF(GgfxCmdWritePixels, image_id, 8);
CHECK_OFF(GgfxCmdWritePixels, count, 12);

CHECK_SIZE(GgfxDrawRegion, 24);
CHECK_OFF(GgfxDrawRegion, rect, 0);
CHECK_OFF(GgfxDrawRegion, index_count, 16);
CHECK_OFF(GgfxDrawRegion, reserved, 20);

CHECK_SIZE(GgfxCmdDrawTriangles, 80);
CHECK_OFF(GgfxCmdDrawTriangles, hdr, 0);
CHECK_OFF(GgfxCmdDrawTriangles, dst_id, 8);
CHECK_OFF(GgfxCmdDrawTriangles, shader_id, 12);
CHECK_OFF(GgfxCmdDrawTriangles, src_ids, 16);
CHECK_OFF(GgfxCmdDrawTriangles, index_offset, 32);
CHECK_OFF(GgfxCmdDrawTriangles, region_count, 36);
CHECK_OFF(GgfxCmdDrawTriangles, blend, 40);
CHECK_OFF(GgfxCmdDrawTriangles, reserved0, 46);
CHECK_OFF(GgfxCmdDrawTriangles, internal_version, 48);
CHECK_OFF(GgfxCmdDrawTriangles, user_version, 56);
CHECK_OFF(GgfxCmdDrawTriangles, internal_offset, 64);
CHECK_OFF(GgfxCmdDrawTriangles, user_offset, 68);
CHECK_OFF(GgfxCmdDrawTriangles, user_dwords, 72);
CHECK_OFF(GgfxCmdDrawTriangles, reserved1, 76);

CHECK_SIZE(GgfxCmdSetDamage, 16);
CHECK_OFF(GgfxCmdSetDamage, hdr, 0);
CHECK_OFF(GgfxCmdSetDamage, surface_id, 8);
CHECK_OFF(GgfxCmdSetDamage, count, 12);
