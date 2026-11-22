// Copyright 2026 The ggfx Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build ((darwin && !ios) || windows || (linux && !android)) && (amd64 || arm64)

package zigdriver

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// Shader implements graphicsdriver.Shader backed by a native shader handle.
type Shader struct {
	g                 *Graphics
	id                uint32
	userUniformDwords uint32
	disposed          bool
}

var _ graphicsdriver.Shader = (*Shader)(nil)

func shaderTargetForFormat(format uint32) (graphicsdriver.ShaderTarget, error) {
	switch format {
	case ShaderFormatMSL:
		return graphicsdriver.ShaderTarget{Language: graphicsdriver.ShaderLanguageMSL, Version: 0}, nil
	case ShaderFormatHLSL:
		return graphicsdriver.ShaderTarget{Language: graphicsdriver.ShaderLanguageHLSL, Version: 50}, nil
	case ShaderFormatSPIRV:
		return graphicsdriver.ShaderTarget{Language: graphicsdriver.ShaderLanguageSPIRV, Version: 10}, nil
	default:
		return graphicsdriver.ShaderTarget{}, fmt.Errorf("%w: unsupported shader format %d", ErrUnsupported, format)
	}
}

func makeShaderStage(pinner *runtime.Pinner, code []byte, entryBytes []byte, entryLen int) ShaderStage {
	var codePtr, entryPtr uintptr
	if len(code) > 0 {
		pinner.Pin(&code[0])
		codePtr = uintptr(unsafe.Pointer(&code[0]))
	}
	if len(entryBytes) > 0 {
		pinner.Pin(&entryBytes[0])
		entryPtr = uintptr(unsafe.Pointer(&entryBytes[0]))
	}
	return ShaderStage{
		Code:     codePtr,
		CodeLen:  uint64(len(code)),
		Entry:    entryPtr,
		EntryLen: uint32(entryLen),
	}
}

// NewShader implements graphicsdriver.Graphics.
func (g *Graphics) NewShader(desc *graphicsdriver.ShaderDescriptor) (graphicsdriver.Shader, error) {
	if g.closed.Load() {
		return nil, ErrClosed
	}
	if desc == nil || desc.Source == nil {
		return nil, fmt.Errorf("%w: nil shader descriptor or source", ErrInvalidArgument)
	}
	if desc.TextureCount < 0 || desc.TextureCount > MaxSrcImages {
		return nil, fmt.Errorf("%w: texture count %d out of range [0,%d]", ErrInvalidArgument, desc.TextureCount, MaxSrcImages)
	}
	if desc.UniformDwordCount < 0 || uint32(desc.UniformDwordCount) > MaxUserUniformDwords {
		return nil, fmt.Errorf("%w: uniform dword count %d out of range [0,%d]", ErrInvalidArgument, desc.UniformDwordCount, MaxUserUniformDwords)
	}

	target, err := shaderTargetForFormat(g.caps.ShaderFormat)
	if err != nil {
		return nil, err
	}

	vs, err := desc.Source.VertexStage(target)
	if err != nil {
		return nil, fmt.Errorf("zigdriver: vertex stage translation failed: %w", err)
	}
	fs, err := desc.Source.FragmentStage(target)
	if err != nil {
		return nil, fmt.Errorf("zigdriver: fragment stage translation failed: %w", err)
	}

	vsEntry := make([]byte, len(vs.EntryPoint)+1)
	copy(vsEntry, vs.EntryPoint)
	fsEntry := make([]byte, len(fs.EntryPoint)+1)
	copy(fsEntry, fs.EntryPoint)

	var flags uint32
	if desc.FragmentReadsInternalUniforms {
		flags |= ShaderFlagFragmentReadsInternal
	}

	var pinner runtime.Pinner
	defer pinner.Unpin()

	id := g.allocShaderID()
	sd := ShaderDesc{
		StructSize:        uint32(unsafe.Sizeof(ShaderDesc{})),
		Format:            g.caps.ShaderFormat,
		TextureCount:      uint32(desc.TextureCount),
		UserUniformDwords: uint32(desc.UniformDwordCount),
		Flags:             flags,
		Vertex:            makeShaderStage(&pinner, vs.Code, vsEntry, len(vs.EntryPoint)),
		Fragment:          makeShaderStage(&pinner, fs.Code, fsEntry, len(fs.EntryPoint)),
	}

	g.errBuf[0] = 0
	st := g.lib.createShader(g.dev, id, &sd, &g.errBuf[0], uintptr(len(g.errBuf)))
	runtime.KeepAlive(vs.Code)
	runtime.KeepAlive(fs.Code)
	runtime.KeepAlive(vsEntry)
	runtime.KeepAlive(fsEntry)
	runtime.KeepAlive(&sd)

	if st != StatusOK {
		g.freeShaderID(id)
		return nil, statusToError("create_shader", st, g.errBuf[:])
	}

	return &Shader{
		g:                 g,
		id:                id,
		userUniformDwords: uint32(desc.UniformDwordCount),
	}, nil
}

// Dispose implements graphicsdriver.Shader.
func (s *Shader) Dispose() {
	if s.disposed || s.g.closed.Load() {
		return
	}
	s.disposed = true
	s.g.ring.cur().encodeDisposeShader(s.id)
	s.g.deferFreeShaderID(s.id)
}
