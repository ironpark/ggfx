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

package graphicsdriver

import "fmt"

// ShaderLanguage is the language of a shader stage's code.
type ShaderLanguage uint8

const (
	ShaderLanguageUnknown ShaderLanguage = iota
	ShaderLanguageMSL                    // Metal Shading Language source (UTF-8)
	ShaderLanguageHLSL                   // HLSL source (UTF-8), Version = shader model ×10 (50)
	ShaderLanguageGLSL                   // desktop GLSL source, Version = 330
	ShaderLanguageGLSLES                 // GLSL ES source, Version = 300
	ShaderLanguageSPIRV                  // SPIR-V binary (little-endian words), Version = 10 (1.0)
)

// String returns the language's name.
func (l ShaderLanguage) String() string {
	switch l {
	case ShaderLanguageUnknown:
		return "Unknown"
	case ShaderLanguageMSL:
		return "MSL"
	case ShaderLanguageHLSL:
		return "HLSL"
	case ShaderLanguageGLSL:
		return "GLSL"
	case ShaderLanguageGLSLES:
		return "GLSL ES"
	case ShaderLanguageSPIRV:
		return "SPIR-V"
	}
	return fmt.Sprintf("ShaderLanguage(%d)", uint8(l))
}

// ShaderTarget is what a driver asks a ShaderSource to translate to.
type ShaderTarget struct {
	Language ShaderLanguage
	// Version: GLSL 330/300, HLSL shader model 50, SPIR-V 10 (=1.0), MSL 0 (naga default).
	Version uint32
}

// ShaderUniformBlockKind identifies a uniform block of the ggfx layout.
type ShaderUniformBlockKind uint8

const (
	ShaderUniformBlockInternal ShaderUniformBlockKind = iota + 1 // group 0 binding 0
	ShaderUniformBlockUser                                       // group 1 binding 0
)

// ShaderUniformBlock is a uniform block a GLSL stage declares (GLSL only).
type ShaderUniformBlock struct {
	Name string
	Kind ShaderUniformBlockKind
}

// ShaderStageCode is one translated stage.
type ShaderStageCode struct {
	Code       []byte // source text or SPIR-V words; immutable, may be shared
	EntryPoint string // the entry point's name in Code (naga may rename it)
	// GLSL only: the uniform blocks the stage emits and, for the fragment stage,
	// the sampler uniform name of each source texture ("" for one it does not read).
	UniformBlocks []ShaderUniformBlock
	TextureNames  []string
}

// ShaderSource translates a compiled program for a driver. internal/shader.Program implements it.
// Both methods are safe for concurrent use; VertexStage results are cached per
// (TextureCount, ShaderTarget) and shared by every program of that TextureCount.
type ShaderSource interface {
	VertexStage(target ShaderTarget) (ShaderStageCode, error)
	FragmentStage(target ShaderTarget) (ShaderStageCode, error)
}

// ShaderDescriptor is everything a driver needs to make a Shader.
type ShaderDescriptor struct {
	TextureCount                  int
	UniformDwordCount             int // user block, multiple of 4, 0 without one
	FragmentReadsInternalUniforms bool
	WGSL                          []byte // the user's WGSL without the prelude (remote driver)
	Source                        ShaderSource
}
