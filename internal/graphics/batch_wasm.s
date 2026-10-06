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

//go:build !ggfxnowasmsimd

#include "textflag.h"

// These functions use the fixed-width SIMD (SIMD128) proposal. A WebAssembly module is validated as
// a whole, so an engine without SIMD128 cannot load an application that includes them at all; the
// build tag ggfxnowasmsimd excludes them.
//
// R registers are i64 locals and V registers are v128 locals. V0 is not used, as the assembler
// uses it for its own rewriting.
//
// The assembler takes the immediate of V128Store and the lane ops (e.g. I32x4ExtractLane) as their
// source operands while it encodes their destination operands, so the immediates would silently be
// 0. Thus, V128Store always stores to the address on the stack without an offset, and no lane op is
// used.

// ADDR pushes the address held in the register r as an i32.
#define ADDR(r) Get r; I32WrapI64

// ADVANCE adds n to the register r.
#define ADVANCE(r, n) Get r; I64Const $n; I64Add; Set r

// A vertex is 12 floats, which are three vectors: a position (dst x, dst y, src x, src y), a color,
// and custom values.

// func convertVerticesSIMD(dst, src *float32, n int, offset *[4]float32, flags ConvertVerticesFlags)
TEXT ·convertVerticesSIMD(SB), NOSPLIT, $0-40
	I64Load dst+0(FP)
	Set R0
	I64Load src+8(FP)
	Set R1
	I64Load n+16(FP)
	Set R2
	I64Load offset+24(FP)
	I32WrapI64
	V128Load $0
	Set V1
	I64Load flags+32(FP)
	Set R3

	Loop
		ADDR(R0)
		ADDR(R1)
		V128Load $0
		Get V1
		F32x4Add
		V128Store $0

		ADDR(R1)
		V128Load $16
		Set V2
		Get R3
		I64Const $1
		I64And
		Tee R4
		I32WrapI64
		If
			// Multiply (r, g, b, a) by a. The alpha is restored below.
			Get V2
			ADDR(R1)
			F32Load $28
			F32x4Splat
			F32x4Mul
			Set V2
		End
		ADDR(R0)
		I32Const $16
		I32Add
		Get V2
		V128Store $0
		Get R4
		I32WrapI64
		If
			ADDR(R0)
			ADDR(R1)
			F32Load $28
			F32Store $28
		End

		Get R3
		I64Const $2
		I64And
		I32WrapI64
		// Without the flag, leave the custom values in dst as they are.
		If
			ADDR(R0)
			I32Const $32
			I32Add
			ADDR(R1)
			V128Load $32
			V128Store $0
		End

		ADVANCE(R0, 48)
		ADVANCE(R1, 48)
		Get R2
		I64Const $-1
		I64Add
		Tee R2
		I64Eqz
		I32Eqz
		BrIf $0
	End
	RET

// func translateVerticesSIMD(vertices *float32, n int, offset *[4]float32)
TEXT ·translateVerticesSIMD(SB), NOSPLIT, $0-24
	I64Load vertices+0(FP)
	Set R0
	I64Load n+8(FP)
	Set R1
	I64Load offset+16(FP)
	I32WrapI64
	V128Load $0
	Set V1

	Loop
		ADDR(R0)
		ADDR(R0)
		V128Load $0
		Get V1
		F32x4Add
		V128Store $0

		ADVANCE(R0, 48)
		Get R1
		I64Const $-1
		I64Add
		Tee R1
		I64Eqz
		I32Eqz
		BrIf $0
	End
	RET

// func widenIndicesSIMD(dst *uint32, src *uint16, n int)
TEXT ·widenIndicesSIMD(SB), NOSPLIT, $0-24
	I64Load dst+0(FP)
	Set R0
	I64Load src+8(FP)
	Set R1
	I64Load n+16(FP)
	Set R2
	// R3 is the number of the elements processed eight at a time.
	Get R2
	I64Const $-8
	I64And
	Tee R3
	I64Eqz
	I32Eqz
	If
		Loop
			ADDR(R1)
			V128Load $0
			Set V1
			ADDR(R0)
			Get V1
			I32x4ExtendLowI16x8U
			V128Store $0
			ADDR(R0)
			I32Const $16
			I32Add
			Get V1
			I32x4ExtendHighI16x8U
			V128Store $0

			ADVANCE(R0, 32)
			ADVANCE(R1, 16)
			Get R3
			I64Const $-8
			I64Add
			Tee R3
			I64Eqz
			I32Eqz
			BrIf $0
		End
	End

	Get R2
	I64Const $7
	I64And
	Tee R2
	I64Eqz
	I32Eqz
	If
		Loop
			ADDR(R0)
			ADDR(R1)
			I32Load16U $0
			I32Store $0

			ADVANCE(R0, 4)
			ADVANCE(R1, 2)
			Get R2
			I64Const $-1
			I64Add
			Tee R2
			I64Eqz
			I32Eqz
			BrIf $0
		End
	End
	RET

// func offsetIndicesSIMD(indices *uint32, n int, offset uint32)
TEXT ·offsetIndicesSIMD(SB), NOSPLIT, $0-20
	I64Load indices+0(FP)
	Set R0
	I64Load n+8(FP)
	Set R1
	I32Load offset+16(FP)
	I32x4Splat
	Set V1
	// R3 is the number of the elements processed four at a time.
	Get R1
	I64Const $-4
	I64And
	Tee R3
	I64Eqz
	I32Eqz
	If
		Loop
			ADDR(R0)
			ADDR(R0)
			V128Load $0
			Get V1
			I32x4Add
			V128Store $0

			ADVANCE(R0, 16)
			Get R3
			I64Const $-4
			I64Add
			Tee R3
			I64Eqz
			I32Eqz
			BrIf $0
		End
	End

	Get R1
	I64Const $3
	I64And
	Tee R1
	I64Eqz
	I32Eqz
	If
		Loop
			ADDR(R0)
			ADDR(R0)
			I32Load $0
			I32Load offset+16(FP)
			I32Add
			I32Store $0

			ADVANCE(R0, 4)
			Get R1
			I64Const $-1
			I64Add
			Tee R1
			I64Eqz
			I32Eqz
			BrIf $0
		End
	End
	RET

// MAXR sets the register acc to the maximum of acc and the i64 on the stack.
#define MAXR(acc) Set R5; Get R5; Get acc; Get R5; Get acc; I64GtU; Select; Set acc

// func maxIndexSIMD(indices *uint32, n int) uint32
// The frame holds the four maximums of the lanes, which cannot be extracted with lane ops.
TEXT ·maxIndexSIMD(SB), NOSPLIT, $16-20
	I64Load indices+0(FP)
	Set R0
	I64Load n+8(FP)
	Set R1
	I32Const $0
	I32x4Splat
	Set V1
	// R3 is the number of the elements processed four at a time.
	Get R1
	I64Const $-4
	I64And
	Tee R3
	I64Eqz
	I32Eqz
	If
		Loop
			Get V1
			ADDR(R0)
			V128Load $0
			I32x4MaxU
			Set V1

			ADVANCE(R0, 16)
			Get R3
			I64Const $-4
			I64Add
			Tee R3
			I64Eqz
			I32Eqz
			BrIf $0
		End
	End

	// R4 is the maximum.
	Get SP
	Get V1
	V128Store $0
	Get SP
	I32Load $0
	I64ExtendI32U
	Set R4
	Get SP
	I32Load $4
	I64ExtendI32U
	MAXR(R4)
	Get SP
	I32Load $8
	I64ExtendI32U
	MAXR(R4)
	Get SP
	I32Load $12
	I64ExtendI32U
	MAXR(R4)

	Get R1
	I64Const $3
	I64And
	Tee R1
	I64Eqz
	I32Eqz
	If
		Loop
			ADDR(R0)
			I32Load $0
			I64ExtendI32U
			MAXR(R4)

			ADVANCE(R0, 4)
			Get R1
			I64Const $-1
			I64Add
			Tee R1
			I64Eqz
			I32Eqz
			BrIf $0
		End
	End

	Get SP
	Get R4
	I32WrapI64
	I32Store ret+16(FP)
	RET
