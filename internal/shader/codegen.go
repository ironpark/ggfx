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

package shader

import (
	"fmt"
	"sync"

	"github.com/gogpu/naga/glsl"
	"github.com/gogpu/naga/hlsl"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/gogpu/naga/spirv"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

var _ graphicsdriver.ShaderSource = (*Program)(nil)

// Descriptor returns the ShaderDescriptor for p, built once and reused across calls.
func (p *Program) Descriptor() *graphicsdriver.ShaderDescriptor {
	p.descOnce.Do(func() {
		p.desc = graphicsdriver.ShaderDescriptor{
			TextureCount:                  p.TextureCount,
			UniformDwordCount:             p.UniformDwordCount,
			FragmentReadsInternalUniforms: p.FragmentReadsInternalUniforms,
			WGSL:                          p.Source,
			Source:                        p,
		}
	})
	return &p.desc
}

type vertexStageKey struct {
	textureCount int
	target       graphicsdriver.ShaderTarget
}

var (
	vertexStagesM sync.Mutex
	vertexStages  = map[vertexStageKey]func() (graphicsdriver.ShaderStageCode, error){}
)

// VertexStage translates the shared vertex stage for p.TextureCount to target. Results are cached
// per (TextureCount, target) and shared by every program with the same TextureCount.
func (p *Program) VertexStage(target graphicsdriver.ShaderTarget) (graphicsdriver.ShaderStageCode, error) {
	return vertexStage(p.TextureCount, target)
}

func vertexStage(textureCount int, target graphicsdriver.ShaderTarget) (graphicsdriver.ShaderStageCode, error) {
	key := vertexStageKey{textureCount: textureCount, target: target}
	vertexStagesM.Lock()
	f, ok := vertexStages[key]
	if !ok {
		f = sync.OnceValues(func() (graphicsdriver.ShaderStageCode, error) {
			vp, err := VertexProgram(textureCount)
			if err != nil {
				return graphicsdriver.ShaderStageCode{}, err
			}
			return translate(vp.Module, textureCount, ir.StageVertex, VertexEntryPoint, target)
		})
		vertexStages[key] = f
	}
	vertexStagesM.Unlock()
	return f()
}

// FragmentStage translates p's fragment stage to target.
func (p *Program) FragmentStage(target graphicsdriver.ShaderTarget) (graphicsdriver.ShaderStageCode, error) {
	return translate(p.Module, p.TextureCount, ir.StageFragment, FragmentEntryPoint, target)
}

func translate(module *ir.Module, textureCount int, stage ir.ShaderStage, entryPoint string, target graphicsdriver.ShaderTarget) (graphicsdriver.ShaderStageCode, error) {
	switch target.Language {
	case graphicsdriver.ShaderLanguageMSL:
		src, info, err := msl.Compile(module, mslOptions(textureCount, stage, entryPoint))
		if err != nil {
			return graphicsdriver.ShaderStageCode{}, fmt.Errorf("shader: translating %s to MSL failed: %w", entryPoint, err)
		}
		return graphicsdriver.ShaderStageCode{
			Code:       []byte(src),
			EntryPoint: info.EntryPointNames[entryPoint],
		}, nil

	case graphicsdriver.ShaderLanguageHLSL:
		o, err := hlslOptions(textureCount, stage, entryPoint, target.Version)
		if err != nil {
			return graphicsdriver.ShaderStageCode{}, err
		}
		src, info, err := hlsl.Compile(module, o)
		if err != nil {
			return graphicsdriver.ShaderStageCode{}, fmt.Errorf("shader: translating %s to HLSL failed: %w", entryPoint, err)
		}
		return graphicsdriver.ShaderStageCode{
			Code:       []byte(src),
			EntryPoint: info.EntryPointNames[entryPoint],
		}, nil

	case graphicsdriver.ShaderLanguageGLSL, graphicsdriver.ShaderLanguageGLSLES:
		o, err := glslOptions(stage, entryPoint, target)
		if err != nil {
			return graphicsdriver.ShaderStageCode{}, err
		}
		src, info, err := glsl.Compile(module, o)
		if err != nil {
			return graphicsdriver.ShaderStageCode{}, fmt.Errorf("shader: translating %s to GLSL failed: %w", entryPoint, err)
		}
		var blocks []graphicsdriver.ShaderUniformBlock
		for _, u := range info.Uniforms {
			switch u.Binding {
			case InternalUniformBinding:
				blocks = append(blocks, graphicsdriver.ShaderUniformBlock{
					Name: u.BlockName,
					Kind: graphicsdriver.ShaderUniformBlockInternal,
				})
			case UserUniformBinding:
				blocks = append(blocks, graphicsdriver.ShaderUniformBlock{
					Name: u.BlockName,
					Kind: graphicsdriver.ShaderUniformBlockUser,
				})
			}
		}
		var texNames []string
		if stage == ir.StageFragment {
			texNames = make([]string, textureCount)
			for i := range textureCount {
				texNames[i] = glslTextureName(i)
			}
		}
		return graphicsdriver.ShaderStageCode{
			Code:          []byte(src),
			EntryPoint:    info.EntryPointNames[entryPoint],
			UniformBlocks: blocks,
			TextureNames:  texNames,
		}, nil

	case graphicsdriver.ShaderLanguageSPIRV:
		o, err := spirvOptions(textureCount, stage, entryPoint, target.Version)
		if err != nil {
			return graphicsdriver.ShaderStageCode{}, err
		}
		bin, info, err := spirv.Compile(module, o)
		if err != nil {
			return graphicsdriver.ShaderStageCode{}, fmt.Errorf("shader: translating %s to SPIR-V failed: %w", entryPoint, err)
		}
		return graphicsdriver.ShaderStageCode{
			Code:       bin,
			EntryPoint: info.EntryPointNames[entryPoint],
		}, nil

	default:
		return graphicsdriver.ShaderStageCode{}, fmt.Errorf("shader: unsupported shader language %s", target.Language)
	}
}
