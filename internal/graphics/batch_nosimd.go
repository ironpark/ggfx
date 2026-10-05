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
//go:build !amd64 && !arm64

package graphics

const useSIMD = false

func convertVerticesSIMD(dst, src *float32, n int, offset *[4]float32, flags ConvertVerticesFlags) {
	panic("graphics: convertVerticesSIMD is not implemented")
}

func translateVerticesSIMD(vertices *float32, n int, offset *[4]float32) {
	panic("graphics: translateVerticesSIMD is not implemented")
}

func widenIndicesSIMD(dst *uint32, src *uint16, n int) {
	panic("graphics: widenIndicesSIMD is not implemented")
}

func offsetIndicesSIMD(indices *uint32, n int, offset uint32) {
	panic("graphics: offsetIndicesSIMD is not implemented")
}

func maxIndexSIMD(indices *uint32, n int) uint32 {
	panic("graphics: maxIndexSIMD is not implemented")
}
