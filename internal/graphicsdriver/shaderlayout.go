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

// Shader resource binding indices shared by code generation (internal/shader) and the backends.
const (
	// Metal
	MetalVertexBufferIndex          = 0
	MetalInternalUniformBufferIndex = 1 // 288 B, setVertexBytes / setFragmentBytes
	MetalUserUniformBufferIndex     = 2 // ≤4096 B setFragmentBytes, else 16B-aligned upload buffer
	MetalMaxInlineUniformBytes      = 4096
	// textures: [[texture(i)]] i = 0..3, no samplers (textureLoad only)

	// HLSL / D3D
	HLSLInternalConstantBufferRegister = 0 // b0
	HLSLUserConstantBufferRegister     = 1 // b1
	// textures t0..t3, vertex semantics LOC0..LOC3
	D3DConstantBufferAlignment = 256

	// GLSL
	GLInternalUniformBlockBinding = 0
	GLUserUniformBlockBinding     = 1

	// SPIR-V / Vulkan (descriptor set 0 only)
	VulkanDescriptorSet          = 0
	VulkanInternalUniformBinding = 0 // UNIFORM_BUFFER_DYNAMIC, VERTEX|FRAGMENT
	VulkanTextureBindingBase     = 1 // SAMPLED_IMAGE 1..4, FRAGMENT
	VulkanUserUniformBinding     = 5 // UNIFORM_BUFFER_DYNAMIC, FRAGMENT
)
