//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"bytes"
	"encoding/binary"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

var updateGolden = flag.Bool("update", false, "regenerate zig/tests/golden/batch_v1.bin")

func TestABILayout(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Fatalf("expected 64-bit pointer size, got %d", unsafe.Sizeof(uintptr(0)))
	}

	type fieldCheck struct {
		name string
		got  uintptr
		want uintptr
	}

	checks := []fieldCheck{
		// Rect (16 B)
		{"sizeof(Rect)", unsafe.Sizeof(Rect{}), 16},
		{"Rect.X0", unsafe.Offsetof(Rect{}.X0), 0},
		{"Rect.Y0", unsafe.Offsetof(Rect{}.Y0), 4},
		{"Rect.X1", unsafe.Offsetof(Rect{}.X1), 8},
		{"Rect.Y1", unsafe.Offsetof(Rect{}.Y1), 12},

		// Blend (6 B)
		{"sizeof(Blend)", unsafe.Sizeof(Blend{}), 6},
		{"Blend.SrcRGB", unsafe.Offsetof(Blend{}.SrcRGB), 0},
		{"Blend.SrcA", unsafe.Offsetof(Blend{}.SrcA), 1},
		{"Blend.DstRGB", unsafe.Offsetof(Blend{}.DstRGB), 2},
		{"Blend.DstA", unsafe.Offsetof(Blend{}.DstA), 3},
		{"Blend.OpRGB", unsafe.Offsetof(Blend{}.OpRGB), 4},
		{"Blend.OpA", unsafe.Offsetof(Blend{}.OpA), 5},

		// DeviceOptions (48 B)
		{"sizeof(DeviceOptions)", unsafe.Sizeof(DeviceOptions{}), 48},
		{"DeviceOptions.StructSize", unsafe.Offsetof(DeviceOptions{}.StructSize), 0},
		{"DeviceOptions.ABIVersion", unsafe.Offsetof(DeviceOptions{}.ABIVersion), 4},
		{"DeviceOptions.Backend", unsafe.Offsetof(DeviceOptions{}.Backend), 8},
		{"DeviceOptions.ColorSpace", unsafe.Offsetof(DeviceOptions{}.ColorSpace), 12},
		{"DeviceOptions.Flags", unsafe.Offsetof(DeviceOptions{}.Flags), 16},
		{"DeviceOptions.FramesInFlight", unsafe.Offsetof(DeviceOptions{}.FramesInFlight), 20},
		{"DeviceOptions.DXFeatureLevel", unsafe.Offsetof(DeviceOptions{}.DXFeatureLevel), 24},
		{"DeviceOptions.MainThreadMode", unsafe.Offsetof(DeviceOptions{}.MainThreadMode), 28},
		{"DeviceOptions.Reserved", unsafe.Offsetof(DeviceOptions{}.Reserved), 32},

		// DriverCaps (96 B)
		{"sizeof(DriverCaps)", unsafe.Sizeof(DriverCaps{}), 96},
		{"DriverCaps.StructSize", unsafe.Offsetof(DriverCaps{}.StructSize), 0},
		{"DriverCaps.Backend", unsafe.Offsetof(DriverCaps{}.Backend), 4},
		{"DriverCaps.MaxImageSize", unsafe.Offsetof(DriverCaps{}.MaxImageSize), 8},
		{"DriverCaps.Flags", unsafe.Offsetof(DriverCaps{}.Flags), 12},
		{"DriverCaps.ShaderFormat", unsafe.Offsetof(DriverCaps{}.ShaderFormat), 16},
		{"DriverCaps.ShaderVersion", unsafe.Offsetof(DriverCaps{}.ShaderVersion), 20},
		{"DriverCaps.ColorSpace", unsafe.Offsetof(DriverCaps{}.ColorSpace), 24},
		{"DriverCaps.FramesInFlight", unsafe.Offsetof(DriverCaps{}.FramesInFlight), 28},
		{"DriverCaps.DeviceName", unsafe.Offsetof(DriverCaps{}.DeviceName), 32},

		// CmdHeader (8 B)
		{"sizeof(CmdHeader)", unsafe.Sizeof(CmdHeader{}), 8},
		{"CmdHeader.Op", unsafe.Offsetof(CmdHeader{}.Op), 0},
		{"CmdHeader.Flags", unsafe.Offsetof(CmdHeader{}.Flags), 1},
		{"CmdHeader.Aux", unsafe.Offsetof(CmdHeader{}.Aux), 2},
		{"CmdHeader.Size", unsafe.Offsetof(CmdHeader{}.Size), 4},

		// Batch (48 B)
		{"sizeof(Batch)", unsafe.Sizeof(Batch{}), 48},
		{"Batch.StructSize", unsafe.Offsetof(Batch{}.StructSize), 0},
		{"Batch.CmdCount", unsafe.Offsetof(Batch{}.CmdCount), 4},
		{"Batch.Cmds", unsafe.Offsetof(Batch{}.Cmds), 8},
		{"Batch.CmdsLen", unsafe.Offsetof(Batch{}.CmdsLen), 16},
		{"Batch.Payload", unsafe.Offsetof(Batch{}.Payload), 24},
		{"Batch.PayloadLen", unsafe.Offsetof(Batch{}.PayloadLen), 32},
		{"Batch.Sequence", unsafe.Offsetof(Batch{}.Sequence), 40},

		// SubmitResult (24 B)
		{"sizeof(SubmitResult)", unsafe.Sizeof(SubmitResult{}), 24},
		{"SubmitResult.Flags", unsafe.Offsetof(SubmitResult{}.Flags), 0},
		{"SubmitResult.PresentedSurfaces", unsafe.Offsetof(SubmitResult{}.PresentedSurfaces), 4},
		{"SubmitResult.CompletedFrames", unsafe.Offsetof(SubmitResult{}.CompletedFrames), 8},
		{"SubmitResult.Reserved", unsafe.Offsetof(SubmitResult{}.Reserved), 16},

		// ShaderStage (32 B)
		{"sizeof(ShaderStage)", unsafe.Sizeof(ShaderStage{}), 32},
		{"ShaderStage.Code", unsafe.Offsetof(ShaderStage{}.Code), 0},
		{"ShaderStage.CodeLen", unsafe.Offsetof(ShaderStage{}.CodeLen), 8},
		{"ShaderStage.Entry", unsafe.Offsetof(ShaderStage{}.Entry), 16},
		{"ShaderStage.EntryLen", unsafe.Offsetof(ShaderStage{}.EntryLen), 24},
		{"ShaderStage.Reserved", unsafe.Offsetof(ShaderStage{}.Reserved), 28},

		// ShaderDesc (88 B)
		{"sizeof(ShaderDesc)", unsafe.Sizeof(ShaderDesc{}), 88},
		{"ShaderDesc.StructSize", unsafe.Offsetof(ShaderDesc{}.StructSize), 0},
		{"ShaderDesc.Format", unsafe.Offsetof(ShaderDesc{}.Format), 4},
		{"ShaderDesc.TextureCount", unsafe.Offsetof(ShaderDesc{}.TextureCount), 8},
		{"ShaderDesc.UserUniformDwords", unsafe.Offsetof(ShaderDesc{}.UserUniformDwords), 12},
		{"ShaderDesc.Flags", unsafe.Offsetof(ShaderDesc{}.Flags), 16},
		{"ShaderDesc.Reserved", unsafe.Offsetof(ShaderDesc{}.Reserved), 20},
		{"ShaderDesc.Vertex", unsafe.Offsetof(ShaderDesc{}.Vertex), 24},
		{"ShaderDesc.Fragment", unsafe.Offsetof(ShaderDesc{}.Fragment), 56},

		// SurfaceDesc (40 B)
		{"sizeof(SurfaceDesc)", unsafe.Sizeof(SurfaceDesc{}), 40},
		{"SurfaceDesc.StructSize", unsafe.Offsetof(SurfaceDesc{}.StructSize), 0},
		{"SurfaceDesc.Kind", unsafe.Offsetof(SurfaceDesc{}.Kind), 4},
		{"SurfaceDesc.Flags", unsafe.Offsetof(SurfaceDesc{}.Flags), 8},
		{"SurfaceDesc.Reserved", unsafe.Offsetof(SurfaceDesc{}.Reserved), 12},
		{"SurfaceDesc.Handle1", unsafe.Offsetof(SurfaceDesc{}.Handle1), 16},
		{"SurfaceDesc.Handle2", unsafe.Offsetof(SurfaceDesc{}.Handle2), 24},
		{"SurfaceDesc.WidthHint", unsafe.Offsetof(SurfaceDesc{}.WidthHint), 32},
		{"SurfaceDesc.HeightHint", unsafe.Offsetof(SurfaceDesc{}.HeightHint), 36},

		// SurfaceCaps (16 B)
		{"sizeof(SurfaceCaps)", unsafe.Sizeof(SurfaceCaps{}), 16},
		{"SurfaceCaps.StructSize", unsafe.Offsetof(SurfaceCaps{}.StructSize), 0},
		{"SurfaceCaps.Flags", unsafe.Offsetof(SurfaceCaps{}.Flags), 4},
		{"SurfaceCaps.NativeFormat", unsafe.Offsetof(SurfaceCaps{}.NativeFormat), 8},
		{"SurfaceCaps.Reserved", unsafe.Offsetof(SurfaceCaps{}.Reserved), 12},

		// ReadRegion (32 B)
		{"sizeof(ReadRegion)", unsafe.Sizeof(ReadRegion{}), 32},
		{"ReadRegion.Rect", unsafe.Offsetof(ReadRegion{}.Rect), 0},
		{"ReadRegion.Dst", unsafe.Offsetof(ReadRegion{}.Dst), 16},
		{"ReadRegion.DstLen", unsafe.Offsetof(ReadRegion{}.DstLen), 24},

		// DebugStats (64 B)
		{"sizeof(DebugStats)", unsafe.Sizeof(DebugStats{}), 64},
		{"DebugStats.LiveImages", unsafe.Offsetof(DebugStats{}.LiveImages), 0},
		{"DebugStats.LiveShaders", unsafe.Offsetof(DebugStats{}.LiveShaders), 8},
		{"DebugStats.LiveSurfaces", unsafe.Offsetof(DebugStats{}.LiveSurfaces), 16},
		{"DebugStats.LivePipelines", unsafe.Offsetof(DebugStats{}.LivePipelines), 24},
		{"DebugStats.Submits", unsafe.Offsetof(DebugStats{}.Submits), 32},
		{"DebugStats.Draws", unsafe.Offsetof(DebugStats{}.Draws), 40},
		{"DebugStats.BindsSkipped", unsafe.Offsetof(DebugStats{}.BindsSkipped), 48},
		{"DebugStats.UploadedBytes", unsafe.Offsetof(DebugStats{}.UploadedBytes), 56},

		// UploadArena (40 B)
		{"sizeof(UploadArena)", unsafe.Sizeof(UploadArena{}), 40},
		{"UploadArena.StructSize", unsafe.Offsetof(UploadArena{}.StructSize), 0},
		{"UploadArena.ArenaID", unsafe.Offsetof(UploadArena{}.ArenaID), 4},
		{"UploadArena.Ptr", unsafe.Offsetof(UploadArena{}.Ptr), 8},
		{"UploadArena.Capacity", unsafe.Offsetof(UploadArena{}.Capacity), 16},
		{"UploadArena.Alignment", unsafe.Offsetof(UploadArena{}.Alignment), 24},
		{"UploadArena.Flags", unsafe.Offsetof(UploadArena{}.Flags), 28},
		{"UploadArena.RowPitchAlignment", unsafe.Offsetof(UploadArena{}.RowPitchAlignment), 32},
		{"UploadArena.Reserved", unsafe.Offsetof(UploadArena{}.Reserved), 36},

		// Command records
		{"sizeof(CmdSetVertices)", unsafe.Sizeof(CmdSetVertices{}), 32},
		{"CmdSetVertices.Hdr", unsafe.Offsetof(CmdSetVertices{}.Hdr), 0},
		{"CmdSetVertices.Vertices", unsafe.Offsetof(CmdSetVertices{}.Vertices), 8},
		{"CmdSetVertices.Indices", unsafe.Offsetof(CmdSetVertices{}.Indices), 16},
		{"CmdSetVertices.VertexFloatCount", unsafe.Offsetof(CmdSetVertices{}.VertexFloatCount), 24},
		{"CmdSetVertices.IndexCount", unsafe.Offsetof(CmdSetVertices{}.IndexCount), 28},

		{"sizeof(CmdNewImage)", unsafe.Sizeof(CmdNewImage{}), 32},
		{"CmdNewImage.Hdr", unsafe.Offsetof(CmdNewImage{}.Hdr), 0},
		{"CmdNewImage.ID", unsafe.Offsetof(CmdNewImage{}.ID), 8},
		{"CmdNewImage.Width", unsafe.Offsetof(CmdNewImage{}.Width), 12},
		{"CmdNewImage.Height", unsafe.Offsetof(CmdNewImage{}.Height), 16},
		{"CmdNewImage.InternalWidth", unsafe.Offsetof(CmdNewImage{}.InternalWidth), 20},
		{"CmdNewImage.InternalHeight", unsafe.Offsetof(CmdNewImage{}.InternalHeight), 24},
		{"CmdNewImage.Reserved", unsafe.Offsetof(CmdNewImage{}.Reserved), 28},

		{"sizeof(CmdNewScreenImage)", unsafe.Sizeof(CmdNewScreenImage{}), 24},
		{"CmdNewScreenImage.Hdr", unsafe.Offsetof(CmdNewScreenImage{}.Hdr), 0},
		{"CmdNewScreenImage.ID", unsafe.Offsetof(CmdNewScreenImage{}.ID), 8},
		{"CmdNewScreenImage.SurfaceID", unsafe.Offsetof(CmdNewScreenImage{}.SurfaceID), 12},
		{"CmdNewScreenImage.Width", unsafe.Offsetof(CmdNewScreenImage{}.Width), 16},
		{"CmdNewScreenImage.Height", unsafe.Offsetof(CmdNewScreenImage{}.Height), 20},

		{"sizeof(CmdDisposeImage)", unsafe.Sizeof(CmdDisposeImage{}), 16},
		{"CmdDisposeImage.Hdr", unsafe.Offsetof(CmdDisposeImage{}.Hdr), 0},
		{"CmdDisposeImage.ID", unsafe.Offsetof(CmdDisposeImage{}.ID), 8},
		{"CmdDisposeImage.Reserved", unsafe.Offsetof(CmdDisposeImage{}.Reserved), 12},

		{"sizeof(CmdDisposeShader)", unsafe.Sizeof(CmdDisposeShader{}), 16},
		{"CmdDisposeShader.Hdr", unsafe.Offsetof(CmdDisposeShader{}.Hdr), 0},
		{"CmdDisposeShader.ID", unsafe.Offsetof(CmdDisposeShader{}.ID), 8},
		{"CmdDisposeShader.Reserved", unsafe.Offsetof(CmdDisposeShader{}.Reserved), 12},

		{"sizeof(CmdDisposeSurface)", unsafe.Sizeof(CmdDisposeSurface{}), 16},
		{"CmdDisposeSurface.Hdr", unsafe.Offsetof(CmdDisposeSurface{}.Hdr), 0},
		{"CmdDisposeSurface.SurfaceID", unsafe.Offsetof(CmdDisposeSurface{}.SurfaceID), 8},
		{"CmdDisposeSurface.Reserved", unsafe.Offsetof(CmdDisposeSurface{}.Reserved), 12},

		{"sizeof(PixelsRegion)", unsafe.Sizeof(PixelsRegion{}), 24},
		{"PixelsRegion.Rect", unsafe.Offsetof(PixelsRegion{}.Rect), 0},
		{"PixelsRegion.PayloadOffset", unsafe.Offsetof(PixelsRegion{}.PayloadOffset), 16},
		{"PixelsRegion.PayloadLen", unsafe.Offsetof(PixelsRegion{}.PayloadLen), 20},

		{"sizeof(CmdWritePixels)", unsafe.Sizeof(CmdWritePixels{}), 16},
		{"CmdWritePixels.Hdr", unsafe.Offsetof(CmdWritePixels{}.Hdr), 0},
		{"CmdWritePixels.ImageID", unsafe.Offsetof(CmdWritePixels{}.ImageID), 8},
		{"CmdWritePixels.Count", unsafe.Offsetof(CmdWritePixels{}.Count), 12},

		{"sizeof(DrawRegion)", unsafe.Sizeof(DrawRegion{}), 24},
		{"DrawRegion.Rect", unsafe.Offsetof(DrawRegion{}.Rect), 0},
		{"DrawRegion.IndexCount", unsafe.Offsetof(DrawRegion{}.IndexCount), 16},
		{"DrawRegion.Reserved", unsafe.Offsetof(DrawRegion{}.Reserved), 20},

		{"sizeof(CmdDrawTriangles)", unsafe.Sizeof(CmdDrawTriangles{}), 80},
		{"CmdDrawTriangles.Hdr", unsafe.Offsetof(CmdDrawTriangles{}.Hdr), 0},
		{"CmdDrawTriangles.DstID", unsafe.Offsetof(CmdDrawTriangles{}.DstID), 8},
		{"CmdDrawTriangles.ShaderID", unsafe.Offsetof(CmdDrawTriangles{}.ShaderID), 12},
		{"CmdDrawTriangles.SrcIDs", unsafe.Offsetof(CmdDrawTriangles{}.SrcIDs), 16},
		{"CmdDrawTriangles.IndexOffset", unsafe.Offsetof(CmdDrawTriangles{}.IndexOffset), 32},
		{"CmdDrawTriangles.RegionCount", unsafe.Offsetof(CmdDrawTriangles{}.RegionCount), 36},
		{"CmdDrawTriangles.Blend", unsafe.Offsetof(CmdDrawTriangles{}.Blend), 40},
		{"CmdDrawTriangles.Reserved0", unsafe.Offsetof(CmdDrawTriangles{}.Reserved0), 46},
		{"CmdDrawTriangles.InternalVersion", unsafe.Offsetof(CmdDrawTriangles{}.InternalVersion), 48},
		{"CmdDrawTriangles.UserVersion", unsafe.Offsetof(CmdDrawTriangles{}.UserVersion), 56},
		{"CmdDrawTriangles.InternalOffset", unsafe.Offsetof(CmdDrawTriangles{}.InternalOffset), 64},
		{"CmdDrawTriangles.UserOffset", unsafe.Offsetof(CmdDrawTriangles{}.UserOffset), 68},
		{"CmdDrawTriangles.UserDwords", unsafe.Offsetof(CmdDrawTriangles{}.UserDwords), 72},
		{"CmdDrawTriangles.Reserved1", unsafe.Offsetof(CmdDrawTriangles{}.Reserved1), 76},

		{"sizeof(CmdSetDamage)", unsafe.Sizeof(CmdSetDamage{}), 16},
		{"CmdSetDamage.Hdr", unsafe.Offsetof(CmdSetDamage{}.Hdr), 0},
		{"CmdSetDamage.SurfaceID", unsafe.Offsetof(CmdSetDamage{}.SurfaceID), 8},
		{"CmdSetDamage.Count", unsafe.Offsetof(CmdSetDamage{}.Count), 12},
	}

	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// goldenBatchFileHeader is the 48-byte (16-byte aligned) header of zig/tests/golden/batch_v1.bin.
// Layout:
//
//	[0..8]   magic "GGFXB1\x00\x00"
//	[8..12]  cmd_count (uint32 LE)
//	[12..16] reserved (uint32 LE = 0)
//	[16..24] cmds_len (uint64 LE)
//	[24..32] payload_len (uint64 LE)
//	[32..40] sequence (uint64 LE)
//	[40..48] reserved2 (uint64 LE = 0)
//	[48 .. 48+align16(cmds_len)] cmds bytes (zero-padded to 16B)
//	[48+align16(cmds_len) ..]    payload bytes
var goldenBatchMagic = [8]byte{'G', 'G', 'F', 'X', 'B', '1', 0, 0}

func appendStruct[T any](buf *bytes.Buffer, v T) {
	p := unsafe.Slice((*byte)(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	buf.Write(p)
}

func buildGoldenBatchV1() []byte {
	var cmds bytes.Buffer
	var payload bytes.Buffer

	appendPayload := func(data []byte) uint32 {
		pad := align16Up(payload.Len()) - payload.Len()
		for range pad {
			payload.WriteByte(0)
		}
		off := uint32(payload.Len())
		payload.Write(data)
		return off
	}

	var cmdCount uint32

	// 1. OP_SET_VERTICES (empty/zero pointers for deterministic cross-process golden file)
	appendStruct(&cmds, CmdSetVertices{
		Hdr: CmdHeader{
			Op:   OpSetVertices,
			Size: uint32(unsafe.Sizeof(CmdSetVertices{})),
		},
	})
	cmdCount++

	// 2. OP_NEW_IMAGE (RGBA8)
	appendStruct(&cmds, CmdNewImage{
		Hdr: CmdHeader{
			Op:   OpNewImage,
			Aux:  uint16(PixelFormatRGBA8),
			Size: uint32(unsafe.Sizeof(CmdNewImage{})),
		},
		ID:             1,
		Width:          8,
		Height:         4,
		InternalWidth:  16,
		InternalHeight: 16,
	})
	cmdCount++

	// 3. OP_NEW_IMAGE (A8)
	appendStruct(&cmds, CmdNewImage{
		Hdr: CmdHeader{
			Op:   OpNewImage,
			Aux:  uint16(PixelFormatA8),
			Size: uint32(unsafe.Sizeof(CmdNewImage{})),
		},
		ID:             2,
		Width:          4,
		Height:         4,
		InternalWidth:  16,
		InternalHeight: 16,
	})
	cmdCount++

	// 4. OP_NEW_SCREEN_IMAGE
	appendStruct(&cmds, CmdNewScreenImage{
		Hdr: CmdHeader{
			Op:   OpNewScreenImage,
			Size: uint32(unsafe.Sizeof(CmdNewScreenImage{})),
		},
		ID:        3,
		SurfaceID: 10,
		Width:     640,
		Height:    480,
	})
	cmdCount++

	// 5. OP_WRITE_PIXELS (2 regions to RGBA8 image 1)
	pix0 := make([]byte, 16) // 2x2 RGBA8
	for i := range pix0 {
		pix0[i] = byte(0x10 + i)
	}
	pix1 := make([]byte, 8) // 2x1 RGBA8
	for i := range pix1 {
		pix1[i] = byte(0x20 + i)
	}
	pix0Off := appendPayload(pix0)
	pix1Off := appendPayload(pix1)

	wpRegions := []PixelsRegion{
		{
			Rect:          Rect{X0: 0, Y0: 0, X1: 2, Y1: 2},
			PayloadOffset: pix0Off,
			PayloadLen:    uint32(len(pix0)),
		},
		{
			Rect:          Rect{X0: 2, Y0: 1, X1: 4, Y1: 2},
			PayloadOffset: pix1Off,
			PayloadLen:    uint32(len(pix1)),
		},
	}
	appendStruct(&cmds, CmdWritePixels{
		Hdr: CmdHeader{
			Op:   OpWritePixels,
			Size: uint32(unsafe.Sizeof(CmdWritePixels{}) + uintptr(len(wpRegions))*unsafe.Sizeof(PixelsRegion{})),
		},
		ImageID: 1,
		Count:   uint32(len(wpRegions)),
	})
	for _, r := range wpRegions {
		appendStruct(&cmds, r)
	}
	cmdCount++

	// 6. OP_SET_DAMAGE (2 explicit rects)
	dmgRects := []Rect{
		{X0: 10, Y0: 20, X1: 110, Y1: 120},
		{X0: 200, Y0: 50, X1: 300, Y1: 150},
	}
	appendStruct(&cmds, CmdSetDamage{
		Hdr: CmdHeader{
			Op:   OpSetDamage,
			Size: uint32(unsafe.Sizeof(CmdSetDamage{}) + uintptr(len(dmgRects))*unsafe.Sizeof(Rect{})),
		},
		SurfaceID: 10,
		Count:     uint32(len(dmgRects)),
	})
	for _, r := range dmgRects {
		appendStruct(&cmds, r)
	}
	cmdCount++

	// 7. OP_SET_DAMAGE (full damage)
	appendStruct(&cmds, CmdSetDamage{
		Hdr: CmdHeader{
			Op:   OpSetDamage,
			Size: uint32(unsafe.Sizeof(CmdSetDamage{})),
		},
		SurfaceID: 10,
		Count:     DamageFull,
	})
	cmdCount++

	// 8. OP_DRAW_TRIANGLES (internal + 4 user uniform dwords, 2 draw regions)
	internalBytes := make([]byte, InternalUniformBytes)
	for i := uint32(0); i < InternalUniformDwords; i++ {
		binary.LittleEndian.PutUint32(internalBytes[i*4:], 0x10000000+i)
	}
	userBytes := make([]byte, 4*4)
	for i := uint32(0); i < 4; i++ {
		binary.LittleEndian.PutUint32(userBytes[i*4:], 0xDEAD0001+i)
	}
	internalOff := appendPayload(internalBytes)
	userOff := appendPayload(userBytes)

	drawRegions := []DrawRegion{
		{Rect: Rect{X0: 0, Y0: 0, X1: 320, Y1: 240}, IndexCount: 6},
		{Rect: Rect{X0: 320, Y0: 240, X1: 640, Y1: 480}, IndexCount: 12},
	}
	appendStruct(&cmds, CmdDrawTriangles{
		Hdr: CmdHeader{
			Op:   OpDrawTriangles,
			Size: uint32(unsafe.Sizeof(CmdDrawTriangles{}) + uintptr(len(drawRegions))*unsafe.Sizeof(DrawRegion{})),
		},
		DstID:           3,
		ShaderID:        7,
		SrcIDs:          [MaxSrcImages]uint32{1, 2, 0, 0},
		IndexOffset:     6,
		RegionCount:     uint32(len(drawRegions)),
		Blend:           Blend{SrcRGB: 1, SrcA: 1, DstRGB: 5, DstA: 5, OpRGB: 0, OpA: 0},
		InternalVersion: 101,
		UserVersion:     202,
		InternalOffset:  internalOff,
		UserOffset:      userOff,
		UserDwords:      4,
	})
	for _, r := range drawRegions {
		appendStruct(&cmds, r)
	}
	cmdCount++

	// 9. OP_DISPOSE_IMAGE
	appendStruct(&cmds, CmdDisposeImage{
		Hdr: CmdHeader{
			Op:   OpDisposeImage,
			Size: uint32(unsafe.Sizeof(CmdDisposeImage{})),
		},
		ID: 2,
	})
	cmdCount++

	// 10. OP_DISPOSE_SHADER
	appendStruct(&cmds, CmdDisposeShader{
		Hdr: CmdHeader{
			Op:   OpDisposeShader,
			Size: uint32(unsafe.Sizeof(CmdDisposeShader{})),
		},
		ID: 7,
	})
	cmdCount++

	// 11. OP_DISPOSE_SURFACE
	appendStruct(&cmds, CmdDisposeSurface{
		Hdr: CmdHeader{
			Op:   OpDisposeSurface,
			Size: uint32(unsafe.Sizeof(CmdDisposeSurface{})),
		},
		SurfaceID: 10,
	})
	cmdCount++

	// Pad payload to 16-byte multiple for tidy file size.
	for payload.Len()%16 != 0 {
		payload.WriteByte(0)
	}

	var out bytes.Buffer
	out.Write(goldenBatchMagic[:])
	_ = binary.Write(&out, binary.LittleEndian, cmdCount)
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))
	_ = binary.Write(&out, binary.LittleEndian, uint64(cmds.Len()))
	_ = binary.Write(&out, binary.LittleEndian, uint64(payload.Len()))
	_ = binary.Write(&out, binary.LittleEndian, uint64(42)) // deterministic sequence
	_ = binary.Write(&out, binary.LittleEndian, uint64(0))
	out.Write(cmds.Bytes())
	for out.Len()%16 != 0 {
		out.WriteByte(0)
	}
	out.Write(payload.Bytes())
	return out.Bytes()
}

func TestGoldenBatchV1(t *testing.T) {
	want := buildGoldenBatchV1()
	goldenPath := filepath.Join("..", "..", "..", "zig", "tests", "golden", "batch_v1.bin")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, want, 0o644); err != nil {
			t.Fatalf("write golden batch: %v", err)
		}
		t.Logf("updated %s (%d bytes)", goldenPath, len(want))
		return
	}

	got, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read %s (run with -update to generate): %v", goldenPath, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden batch mismatch (%d vs %d bytes); run go test -update to regenerate", len(got), len(want))
	}
}
