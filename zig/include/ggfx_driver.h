/* ggfx_driver.h — C ABI contract for the ggfx Zig graphics driver (v1).
 *
 * 64-bit little-endian only (amd64 / arm64).
 * All exported functions use pointer and scalar arguments only (purego compatible).
 * The driver never retains Go/caller pointers after an API call returns.
 */
#ifndef GGFX_DRIVER_H
#define GGFX_DRIVER_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define GGFX_DRIVER_ABI_VERSION 1u

/* Status codes */
typedef int32_t GgfxStatus;
enum {
    GGFX_OK                      = 0,
    GGFX_ERR_INVALID_ARGUMENT    = 1,
    GGFX_ERR_ABI_MISMATCH        = 2,
    GGFX_ERR_BACKEND_UNAVAILABLE = 3,
    GGFX_ERR_OUT_OF_MEMORY       = 4,
    GGFX_ERR_DEVICE_LOST         = 5,
    GGFX_ERR_UNKNOWN_HANDLE      = 6,
    GGFX_ERR_DUPLICATE_HANDLE    = 7,
    GGFX_ERR_SHADER_COMPILE      = 8,
    GGFX_ERR_UNSUPPORTED         = 9,
    GGFX_ERR_MALFORMED_BATCH     = 10,
    GGFX_ERR_SURFACE_LOST        = 11,
    GGFX_ERR_INTERNAL            = 12,
    GGFX_ERR_LEAK_DETECTED       = 13
};

/* Backend identifiers */
enum {
    GGFX_BACKEND_AUTO   = 0,
    GGFX_BACKEND_METAL  = 1,
    GGFX_BACKEND_DX12   = 2,
    GGFX_BACKEND_VULKAN = 3,
    GGFX_BACKEND_NULL   = 255
};

/* Flush modes (matches graphicsdriver.FlushMode) */
enum {
    GGFX_FLUSH_INTERMEDIATE = 0,
    GGFX_FLUSH_END_FRAME    = 1,
    GGFX_FLUSH_PRESENT      = 2
};

/* Pixel formats (matches graphicsdriver.PixelFormat) */
enum {
    GGFX_PIXEL_FORMAT_RGBA8 = 0,
    GGFX_PIXEL_FORMAT_A8    = 1
};

/* Shader formats */
enum {
    GGFX_SHADER_MSL   = 1,
    GGFX_SHADER_HLSL  = 2,
    GGFX_SHADER_DXBC  = 3,
    GGFX_SHADER_DXIL  = 4,
    GGFX_SHADER_SPIRV = 5
};

/* Surface kinds */
enum {
    GGFX_SURFACE_NSVIEW  = 1,
    GGFX_SURFACE_HWND    = 2,
    GGFX_SURFACE_X11     = 3,
    GGFX_SURFACE_WAYLAND = 4
};

/* Main-thread presentation modes */
enum {
    GGFX_MAIN_THREAD_INLINE   = 0,
    GGFX_MAIN_THREAD_DEFERRED = 1,
    GGFX_MAIN_THREAD_DISPATCH = 2
};

/* Native surface pixel formats */
enum {
    GGFX_NATIVE_FORMAT_BGRA8 = 1,
    GGFX_NATIVE_FORMAT_RGBA8 = 2
};

/* Command opcodes */
enum {
    GGFX_OP_SET_VERTICES     = 1,
    GGFX_OP_NEW_IMAGE        = 2,
    GGFX_OP_NEW_SCREEN_IMAGE = 3,
    GGFX_OP_DISPOSE_IMAGE    = 4,
    GGFX_OP_DISPOSE_SHADER   = 5,
    GGFX_OP_DISPOSE_SURFACE  = 6,
    GGFX_OP_WRITE_PIXELS     = 7,
    GGFX_OP_DRAW_TRIANGLES   = 8,
    GGFX_OP_SET_DAMAGE       = 9
};

/* GgfxDeviceOptions.flags */
#define GGFX_DEVICE_DEBUG              (1u << 0)
#define GGFX_DEVICE_WARP               (1u << 1)
#define GGFX_DEVICE_DRED               (1u << 2)
#define GGFX_DEVICE_METAL_NO_IOSURFACE (1u << 3)
#define GGFX_DEVICE_LOW_POWER          (1u << 4)
#define GGFX_DEVICE_ALLOW_DXIL         (1u << 5)

/* GgfxDriverCaps.flags */
#define GGFX_CAP_FLIPS_Y               (1u << 0)
#define GGFX_CAP_SUPPORTS_A8           (1u << 1)
#define GGFX_CAP_NEEDS_CLEARING_SCREEN (1u << 2)
#define GGFX_CAP_DIRECT_COMPOSITION    (1u << 3)
#define GGFX_CAP_UNIFIED_MEMORY        (1u << 4)
#define GGFX_CAP_KEPT_SURFACES         (1u << 5)
#define GGFX_CAP_DAMAGE_SURFACES       (1u << 6)

/* GgfxSubmitResult.flags */
#define GGFX_RESULT_OCCLUDED                  (1u << 0)
#define GGFX_RESULT_NEEDS_MAIN_THREAD_PRESENT (1u << 1)

/* GgfxShaderDesc.flags */
#define GGFX_SHADER_FRAGMENT_READS_INTERNAL (1u << 0)

/* GgfxSurfaceDesc.flags */
#define GGFX_SURFACE_TRANSPARENT (1u << 0)

/* GgfxSurfaceCaps.flags */
#define GGFX_SURFACE_CAP_FLIPS_Y      (1u << 0)
#define GGFX_SURFACE_CAP_READABLE     (1u << 1)
#define GGFX_SURFACE_CAP_KEPT         (1u << 2)
#define GGFX_SURFACE_CAP_DAMAGE       (1u << 3)
#define GGFX_SURFACE_CAP_WINDOW_STATE (1u << 4)

/* Window state flags */
#define GGFX_WINDOW_LIVE_RESIZING (1u << 0)
#define GGFX_WINDOW_FULLSCREEN    (1u << 1)
#define GGFX_WINDOW_HIDDEN        (1u << 2)

/* Miscellaneous constants */
#define GGFX_DAMAGE_FULL             0xFFFFFFFFu
#define GGFX_INTERNAL_UNIFORM_DWORDS 72u
#define GGFX_MAX_USER_UNIFORM_DWORDS 16384u
#define GGFX_MAX_SRC_IMAGES          4u

/* Opaque device handle */
typedef struct GgfxDevice GgfxDevice;

/* Primitives */
typedef struct GgfxRect {
    int32_t x0;
    int32_t y0;
    int32_t x1;
    int32_t y1;
} GgfxRect;

typedef struct GgfxBlend {
    uint8_t src_rgb;
    uint8_t src_a;
    uint8_t dst_rgb;
    uint8_t dst_a;
    uint8_t op_rgb;
    uint8_t op_a;
} GgfxBlend;

/* API structs */
typedef struct GgfxDeviceOptions {
    uint32_t struct_size;
    uint32_t abi_version;
    uint32_t backend;
    uint32_t color_space;
    uint32_t flags;
    uint32_t frames_in_flight;
    uint32_t dx_feature_level;
    uint32_t main_thread_mode;
    uint64_t reserved[2];
} GgfxDeviceOptions;

typedef struct GgfxDriverCaps {
    uint32_t struct_size;
    uint32_t backend;
    uint32_t max_image_size;
    uint32_t flags;
    uint32_t shader_format;
    uint32_t shader_version;
    uint32_t color_space;
    uint32_t frames_in_flight;
    uint8_t  device_name[64];
} GgfxDriverCaps;

typedef struct GgfxCmdHeader {
    uint8_t  op;
    uint8_t  flags;
    uint16_t aux;
    uint32_t size;
} GgfxCmdHeader;

typedef struct GgfxBatch {
    uint32_t       struct_size;
    uint32_t       cmd_count;
    const uint8_t* cmds;
    uint64_t       cmds_len;
    const uint8_t* payload;
    uint64_t       payload_len;
    uint64_t       sequence;
} GgfxBatch;

typedef struct GgfxSubmitResult {
    uint32_t flags;
    uint32_t presented_surfaces;
    uint64_t completed_frames;
    uint64_t reserved;
} GgfxSubmitResult;

typedef struct GgfxShaderStage {
    const uint8_t* code;
    uint64_t       code_len;
    const char*    entry;
    uint32_t       entry_len;
    uint32_t       reserved;
} GgfxShaderStage;

typedef struct GgfxShaderDesc {
    uint32_t        struct_size;
    uint32_t        format;
    uint32_t        texture_count;
    uint32_t        user_uniform_dwords;
    uint32_t        flags;
    uint32_t        reserved;
    GgfxShaderStage vertex;
    GgfxShaderStage fragment;
} GgfxShaderDesc;

typedef struct GgfxSurfaceDesc {
    uint32_t struct_size;
    uint32_t kind;
    uint32_t flags;
    uint32_t reserved;
    uint64_t handle1;
    uint64_t handle2;
    uint32_t width_hint;
    uint32_t height_hint;
} GgfxSurfaceDesc;

typedef struct GgfxSurfaceCaps {
    uint32_t struct_size;
    uint32_t flags;
    uint32_t native_format;
    uint32_t reserved;
} GgfxSurfaceCaps;

typedef struct GgfxReadRegion {
    GgfxRect rect;
    uint8_t* dst;
    uint64_t dst_len;
} GgfxReadRegion;

typedef struct GgfxDebugStats {
    uint64_t live_images;
    uint64_t live_shaders;
    uint64_t live_surfaces;
    uint64_t live_pipelines;
    uint64_t submits;
    uint64_t draws;
    uint64_t binds_skipped;
    uint64_t uploaded_bytes;
} GgfxDebugStats;

/* Command records (8-byte aligned inside GgfxBatch.cmds) */
typedef struct GgfxCmdSetVertices {
    GgfxCmdHeader   hdr;
    const float*    vertices;
    const uint32_t* indices;
    uint32_t        vertex_float_count;
    uint32_t        index_count;
} GgfxCmdSetVertices;

typedef struct GgfxCmdNewImage {
    GgfxCmdHeader hdr;
    uint32_t      id;
    uint32_t      width;
    uint32_t      height;
    uint32_t      internal_width;
    uint32_t      internal_height;
    uint32_t      reserved;
} GgfxCmdNewImage;

typedef struct GgfxCmdNewScreenImage {
    GgfxCmdHeader hdr;
    uint32_t      id;
    uint32_t      surface_id;
    uint32_t      width;
    uint32_t      height;
} GgfxCmdNewScreenImage;

typedef struct GgfxCmdDisposeImage {
    GgfxCmdHeader hdr;
    uint32_t      id;
    uint32_t      reserved;
} GgfxCmdDisposeImage;

typedef struct GgfxCmdDisposeShader {
    GgfxCmdHeader hdr;
    uint32_t      id;
    uint32_t      reserved;
} GgfxCmdDisposeShader;

typedef struct GgfxCmdDisposeSurface {
    GgfxCmdHeader hdr;
    uint32_t      surface_id;
    uint32_t      reserved;
} GgfxCmdDisposeSurface;

typedef struct GgfxPixelsRegion {
    GgfxRect rect;
    uint32_t payload_offset;
    uint32_t payload_len;
} GgfxPixelsRegion;

typedef struct GgfxCmdWritePixels {
    GgfxCmdHeader hdr;
    uint32_t      image_id;
    uint32_t      count;
    /* Followed by GgfxPixelsRegion[count] */
} GgfxCmdWritePixels;

typedef struct GgfxDrawRegion {
    GgfxRect rect;
    uint32_t index_count;
    uint32_t reserved;
} GgfxDrawRegion;

typedef struct GgfxCmdDrawTriangles {
    GgfxCmdHeader hdr;
    uint32_t      dst_id;
    uint32_t      shader_id;
    uint32_t      src_ids[4];
    uint32_t      index_offset;
    uint32_t      region_count;
    GgfxBlend     blend;
    uint16_t      reserved0;
    uint64_t      internal_version;
    uint64_t      user_version;
    uint32_t      internal_offset;
    uint32_t      user_offset;
    uint32_t      user_dwords;
    uint32_t      reserved1;
    /* Followed by GgfxDrawRegion[region_count] */
} GgfxCmdDrawTriangles;

typedef struct GgfxCmdSetDamage {
    GgfxCmdHeader hdr;
    uint32_t      surface_id;
    uint32_t      count;
    /* Followed by GgfxRect[count] (0 if count == GGFX_DAMAGE_FULL) */
} GgfxCmdSetDamage;

/* Exported functions (14) */
uint32_t   ggfx_abi_version(void);
GgfxStatus ggfx_device_create(const GgfxDeviceOptions* opts, GgfxDevice** out_dev, GgfxDriverCaps* out_caps, char* err, size_t cap);
GgfxStatus ggfx_device_destroy(GgfxDevice* dev, char* err, size_t cap);
void       ggfx_device_set_main_thread_mode(GgfxDevice* dev, uint32_t mode);
GgfxStatus ggfx_submit(GgfxDevice* dev, const GgfxBatch* batch, uint32_t flush_mode, GgfxSubmitResult* out_res, char* err, size_t cap);
GgfxStatus ggfx_create_shader(GgfxDevice* dev, uint32_t shader_id, const GgfxShaderDesc* desc, char* err, size_t cap);
GgfxStatus ggfx_read_pixels(GgfxDevice* dev, const GgfxBatch* pending, uint32_t image_id,
                            const GgfxReadRegion* regions, uint32_t region_count, char* err, size_t cap);
GgfxStatus ggfx_finish_forced_frame(GgfxDevice* dev, char* err, size_t cap);
GgfxStatus ggfx_present_on_main_thread(GgfxDevice* dev, char* err, size_t cap);
uint32_t   ggfx_device_is_occluded(GgfxDevice* dev);
GgfxStatus ggfx_surface_create(GgfxDevice* dev, uint32_t surface_id, const GgfxSurfaceDesc* desc, GgfxSurfaceCaps* out_caps, char* err, size_t cap);
void       ggfx_surface_set_window_state(GgfxDevice* dev, uint32_t surface_id, uint32_t state_flags);
uint32_t   ggfx_surface_kept(GgfxDevice* dev, uint32_t surface_id);
GgfxStatus ggfx_debug_stats(GgfxDevice* dev, GgfxDebugStats* out);

#ifdef __cplusplus
}
#endif

#endif /* GGFX_DRIVER_H */
