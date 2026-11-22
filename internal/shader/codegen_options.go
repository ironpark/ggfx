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

	"github.com/gogpu/naga/backend"
	"github.com/gogpu/naga/glsl"
	"github.com/gogpu/naga/hlsl"
	"github.com/gogpu/naga/ir"
	"github.com/gogpu/naga/msl"
	"github.com/gogpu/naga/spirv"

	"github.com/ironpark/ggfx/internal/graphicsdriver"
)

// mslOptions maps the shader's resource bindings to Metal buffer and texture indices, and selects
// the one entry point to write.
func mslOptions(textureCount int, stage ir.ShaderStage, entryPoint string) msl.Options {
	index := func(i int) *uint8 {
		n := uint8(i)
		return &n
	}
	resources := map[ir.ResourceBinding]msl.BindTarget{
		InternalUniformBinding: {Buffer: index(graphicsdriver.MetalInternalUniformBufferIndex)},
		UserUniformBinding:     {Buffer: index(graphicsdriver.MetalUserUniformBufferIndex)},
	}
	for i := range textureCount {
		resources[TextureBinding(i)] = msl.BindTarget{Texture: index(i)}
	}

	o := msl.DefaultOptions()
	o.BoundsCheckPolicies = backend.BoundsCheckPolicies{
		Index:        backend.BoundsCheckReadZeroSkipWrite,
		Buffer:       backend.BoundsCheckReadZeroSkipWrite,
		ImageLoad:    backend.BoundsCheckUnchecked,
		BindingArray: backend.BoundsCheckReadZeroSkipWrite,
	}
	o.PerEntryPointMap = map[string]msl.EntryPointResources{
		entryPoint: {Resources: resources},
	}
	o.EntryPoint = &backend.EntryPointSelector{Stage: stage, Name: entryPoint}
	return o
}

// hlslOptions maps the shader's resource bindings to HLSL registers and selects the entry point.
func hlslOptions(textureCount int, stage ir.ShaderStage, entryPoint string, version uint32) (hlsl.Options, error) {
	if version == 0 {
		version = 50
	}
	sm := backend.ShaderModel{Major: uint8(version / 10), Minor: uint8(version % 10)}
	if sm != (backend.ShaderModel{Major: 5, Minor: 0}) {
		return hlsl.Options{}, fmt.Errorf("shader: unsupported HLSL shader model %d.%d", sm.Major, sm.Minor)
	}
	o := hlsl.DefaultOptions()
	o.ShaderModel = sm
	o.BindingMap = map[ir.ResourceBinding]hlsl.BindTarget{
		InternalUniformBinding: {Register: graphicsdriver.HLSLInternalConstantBufferRegister},
		UserUniformBinding:     {Register: graphicsdriver.HLSLUserConstantBufferRegister},
	}
	for i := range textureCount {
		o.BindingMap[TextureBinding(i)] = hlsl.BindTarget{Register: uint32(i)}
	}
	// A resource the map leaves out goes where its @group and @binding say, as before naga 0.21.
	o.FakeMissingBindings = true
	o.EntryPoint = &backend.EntryPointSelector{Stage: stage, Name: entryPoint}
	return o, nil
}

// glslOptions returns the options to translate entryPoint of the shader to GLSL.
func glslOptions(stage ir.ShaderStage, entryPoint string, target graphicsdriver.ShaderTarget) (glsl.Options, error) {
	var version glsl.Version
	switch target.Language {
	case graphicsdriver.ShaderLanguageGLSL:
		v := target.Version
		if v == 0 {
			v = 330
		}
		version = glsl.Version{Major: uint8(v / 100), Minor: uint8(v % 100), ES: false}
	case graphicsdriver.ShaderLanguageGLSLES:
		v := target.Version
		if v == 0 {
			v = 300
		}
		version = glsl.Version{Major: uint8(v / 100), Minor: uint8(v % 100), ES: true}
	default:
		return glsl.Options{}, fmt.Errorf("shader: unexpected GLSL language %s", target.Language)
	}

	o := glsl.DefaultOptions()
	o.LangVersion = version
	o.EntryPoint = &backend.EntryPointSelector{Stage: stage, Name: entryPoint}
	// The projection matrix the driver sets already maps to OpenGL's clip space.
	o.SkipAdjustCoordinateSpace = true
	o.BoundsCheckPolicies.ImageLoad = backend.BoundsCheckUnchecked
	return o, nil
}

// glslTextureName returns the name naga gives the sampler uniform of source texture i in the
// fragment stage.
func glslTextureName(i int) string {
	b := TextureBinding(i)
	return fmt.Sprintf("_group_%d_binding_%d_fs", b.Group, b.Binding)
}

// spirvOptions maps the shader's resource bindings into Vulkan descriptor set 0 and selects
// the entry point for SPIR-V 1.0 output.
func spirvOptions(textureCount int, stage ir.ShaderStage, entryPoint string, version uint32) (spirv.Options, error) {
	if version == 0 {
		version = 10
	}
	v := spirv.Version{Major: uint8(version / 10), Minor: uint8(version % 10)}
	if v != (spirv.Version{Major: 1, Minor: 0}) {
		return spirv.Options{}, fmt.Errorf("shader: unsupported SPIR-V version %d.%d", v.Major, v.Minor)
	}

	bindingMap := map[ir.ResourceBinding]spirv.BindingInfo{
		InternalUniformBinding: {
			DescriptorSet: graphicsdriver.VulkanDescriptorSet,
			Binding:       graphicsdriver.VulkanInternalUniformBinding,
		},
		UserUniformBinding: {
			DescriptorSet: graphicsdriver.VulkanDescriptorSet,
			Binding:       graphicsdriver.VulkanUserUniformBinding,
		},
	}
	for i := range textureCount {
		bindingMap[TextureBinding(i)] = spirv.BindingInfo{
			DescriptorSet: graphicsdriver.VulkanDescriptorSet,
			Binding:       graphicsdriver.VulkanTextureBindingBase + uint32(i),
		}
	}

	o := spirv.DefaultOptions()
	o.Version = v
	o.AdjustCoordinateSpace = false
	o.BoundsCheckPolicies = backend.BoundsCheckPolicies{
		Index:        backend.BoundsCheckReadZeroSkipWrite,
		Buffer:       backend.BoundsCheckReadZeroSkipWrite,
		ImageLoad:    backend.BoundsCheckUnchecked,
		BindingArray: backend.BoundsCheckReadZeroSkipWrite,
	}
	o.EntryPoint = &backend.EntryPointSelector{Stage: stage, Name: entryPoint}
	o.BindingMap = bindingMap
	o.FakeMissingBindings = false
	return o, nil
}
