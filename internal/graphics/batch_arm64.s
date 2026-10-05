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

#include "textflag.h"

// A vertex is 12 floats, which are three vectors: a position (dst x, dst y, src x, src y), a color,
// and custom values.

// func convertVerticesSIMD(dst, src *float32, n int, offset *[4]float32, flags ConvertVerticesFlags)
TEXT ·convertVerticesSIMD(SB), NOSPLIT, $0-40
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2
	MOVD	offset+24(FP), R3
	MOVD	flags+32(FP), R4
	VLD1	(R3), [V30.S4]
	// V31.S[0] is 1, which the alpha of a color is multiplied by on premultiplying.
	FMOVS	$(1.0), F31

loop:
	VLD1.P	48(R1), [V0.S4, V1.S4, V2.S4]
	VFADD	V30.S4, V0.S4, V0.S4
	TBZ	$0, R4, stored_color
	// Multiply (r, g, b, a) by (a, a, a, 1).
	VDUP	V1.S[3], V3.S4
	VMOV	V31.S[0], V3.S[3]
	VFMUL	V3.S4, V1.S4, V1.S4

stored_color:
	TBZ	$1, R4, no_custom
	VST1.P	[V0.S4, V1.S4, V2.S4], 48(R0)
	SUBS	$1, R2
	BNE	loop
	RET

no_custom:
	// Leave the custom values in dst as they are.
	VST1.P	[V0.S4, V1.S4], 32(R0)
	ADD	$16, R0
	SUBS	$1, R2
	BNE	loop
	RET

// func translateVerticesSIMD(vertices *float32, n int, offset *[4]float32)
TEXT ·translateVerticesSIMD(SB), NOSPLIT, $0-24
	MOVD	vertices+0(FP), R0
	MOVD	n+8(FP), R1
	MOVD	offset+16(FP), R2
	VLD1	(R2), [V30.S4]

loop:
	VLD1	(R0), [V0.S4]
	VFADD	V30.S4, V0.S4, V0.S4
	VST1	[V0.S4], (R0)
	ADD	$48, R0
	SUBS	$1, R1
	BNE	loop
	RET

// func widenIndicesSIMD(dst *uint32, src *uint16, n int)
TEXT ·widenIndicesSIMD(SB), NOSPLIT, $0-24
	MOVD	dst+0(FP), R0
	MOVD	src+8(FP), R1
	MOVD	n+16(FP), R2
	// R3 is the number of the elements processed eight at a time.
	AND	$~7, R2, R3
	CBZ	R3, tail

loop8:
	VLD1.P	16(R1), [V0.H8]
	VUXTL	V0.H4, V1.S4
	VUXTL2	V0.H8, V2.S4
	VST1.P	[V1.S4, V2.S4], 32(R0)
	SUBS	$8, R3
	BNE	loop8

tail:
	ANDS	$7, R2
	BEQ	done

loop1:
	MOVHU.P	2(R1), R4
	MOVWU.P	R4, 4(R0)
	SUBS	$1, R2
	BNE	loop1

done:
	RET

// func offsetIndicesSIMD(indices *uint32, n int, offset uint32)
TEXT ·offsetIndicesSIMD(SB), NOSPLIT, $0-20
	MOVD	indices+0(FP), R0
	MOVD	n+8(FP), R1
	MOVWU	offset+16(FP), R2
	VDUP	R2, V30.S4
	AND	$~7, R1, R3
	CBZ	R3, tail

loop8:
	VLD1	(R0), [V0.S4, V1.S4]
	VADD	V30.S4, V0.S4, V0.S4
	VADD	V30.S4, V1.S4, V1.S4
	VST1.P	[V0.S4, V1.S4], 32(R0)
	SUBS	$8, R3
	BNE	loop8

tail:
	ANDS	$7, R1
	BEQ	done

loop1:
	MOVWU	(R0), R4
	ADDW	R2, R4
	MOVWU.P	R4, 4(R0)
	SUBS	$1, R1
	BNE	loop1

done:
	RET

// func maxIndexSIMD(indices *uint32, n int) uint32
TEXT ·maxIndexSIMD(SB), NOSPLIT, $0-20
	MOVD	indices+0(FP), R0
	MOVD	n+8(FP), R1
	VEOR	V30.B16, V30.B16, V30.B16
	VEOR	V31.B16, V31.B16, V31.B16
	AND	$~7, R1, R3
	CBZ	R3, reduce

loop8:
	VLD1.P	32(R0), [V0.S4, V1.S4]
	VUMAX	V0.S4, V30.S4, V30.S4
	VUMAX	V1.S4, V31.S4, V31.S4
	SUBS	$8, R3
	BNE	loop8

reduce:
	VUMAX	V31.S4, V30.S4, V30.S4
	VUMAXV	V30.S4, V30
	VMOV	V30.S[0], R2
	ANDS	$7, R1
	BEQ	done

loop1:
	MOVWU.P	4(R0), R4
	CMPW	R2, R4
	CSELW	HI, R4, R2, R2
	SUBS	$1, R1
	BNE	loop1

done:
	MOVW	R2, ret+16(FP)
	RET
