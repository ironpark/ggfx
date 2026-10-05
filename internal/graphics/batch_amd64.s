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

// These functions use only SSE2, which every amd64 CPU has (GOAMD64=v1).

// A vertex is 12 floats, which are three vectors: a position (dst x, dst y, src x, src y), a color,
// and custom values.

// func convertVerticesSIMD(dst, src *float32, n int, offset *[4]float32, flags ConvertVerticesFlags)
TEXT ·convertVerticesSIMD(SB), NOSPLIT, $0-40
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX
	MOVQ	offset+24(FP), AX
	MOVQ	flags+32(FP), BX
	MOVUPS	(AX), X7
	// X6 is (1, 1, 1, 1).
	MOVL	$0x3f800000, DX
	MOVD	DX, X6
	SHUFPS	$0x00, X6, X6

loop:
	MOVUPS	0(SI), X0
	MOVUPS	16(SI), X1
	ADDPS	X7, X0
	TESTQ	$1, BX
	JZ	stored_color
	// Multiply (r, g, b, a) by (a, a, a, 1).
	MOVAPS	X1, X2
	SHUFPS	$0x0f, X6, X2 // (a, a, 1, 1)
	SHUFPS	$0x80, X2, X2 // (a, a, a, 1)
	MULPS	X2, X1

stored_color:
	MOVUPS	X0, 0(DI)
	MOVUPS	X1, 16(DI)
	TESTQ	$2, BX
	// Without the flag, leave the custom values in dst as they are.
	JZ	next
	MOVUPS	32(SI), X3
	MOVUPS	X3, 32(DI)

next:
	ADDQ	$48, SI
	ADDQ	$48, DI
	DECQ	CX
	JNZ	loop
	RET

// func translateVerticesSIMD(vertices *float32, n int, offset *[4]float32)
TEXT ·translateVerticesSIMD(SB), NOSPLIT, $0-24
	MOVQ	vertices+0(FP), DI
	MOVQ	n+8(FP), CX
	MOVQ	offset+16(FP), AX
	MOVUPS	(AX), X7

loop:
	MOVUPS	(DI), X0
	ADDPS	X7, X0
	MOVUPS	X0, (DI)
	ADDQ	$48, DI
	DECQ	CX
	JNZ	loop
	RET

// func widenIndicesSIMD(dst *uint32, src *uint16, n int)
TEXT ·widenIndicesSIMD(SB), NOSPLIT, $0-24
	MOVQ	dst+0(FP), DI
	MOVQ	src+8(FP), SI
	MOVQ	n+16(FP), CX
	PXOR	X7, X7
	// DX is the number of the elements processed eight at a time.
	MOVQ	CX, DX
	ANDQ	$~7, DX
	JZ	tail

loop8:
	MOVOU	(SI), X0
	MOVOA	X0, X1
	PUNPCKLWL	X7, X0
	PUNPCKHWL	X7, X1
	MOVOU	X0, (DI)
	MOVOU	X1, 16(DI)
	ADDQ	$16, SI
	ADDQ	$32, DI
	SUBQ	$8, DX
	JNZ	loop8

tail:
	ANDQ	$7, CX
	JZ	done

loop1:
	MOVWLZX	(SI), AX
	MOVL	AX, (DI)
	ADDQ	$2, SI
	ADDQ	$4, DI
	DECQ	CX
	JNZ	loop1

done:
	RET

// func offsetIndicesSIMD(indices *uint32, n int, offset uint32)
TEXT ·offsetIndicesSIMD(SB), NOSPLIT, $0-20
	MOVQ	indices+0(FP), DI
	MOVQ	n+8(FP), CX
	MOVL	offset+16(FP), AX
	MOVD	AX, X7
	PSHUFD	$0x00, X7, X7
	MOVQ	CX, DX
	ANDQ	$~7, DX
	JZ	tail

loop8:
	MOVOU	(DI), X0
	MOVOU	16(DI), X1
	PADDL	X7, X0
	PADDL	X7, X1
	MOVOU	X0, (DI)
	MOVOU	X1, 16(DI)
	ADDQ	$32, DI
	SUBQ	$8, DX
	JNZ	loop8

tail:
	ANDQ	$7, CX
	JZ	done

loop1:
	ADDL	AX, (DI)
	ADDQ	$4, DI
	DECQ	CX
	JNZ	loop1

done:
	RET

// UMAXL sets acc to the unsigned maximum of acc and src, both of which are biased by 1<<31 so that
// a signed comparison works as an unsigned one: SSE2 has no unsigned comparison of 32-bit integers.
#define UMAXL(src, acc, tmp) \
	MOVOA	acc, tmp \
	PCMPGTL	src, tmp \
	PAND	tmp, acc \
	PANDN	src, tmp \
	POR	tmp, acc

// func maxIndexSIMD(indices *uint32, n int) uint32
TEXT ·maxIndexSIMD(SB), NOSPLIT, $0-20
	MOVQ	indices+0(FP), SI
	MOVQ	n+8(FP), CX
	// X6 is the bias. The accumulators X0 and X1 start at the biased 0.
	MOVL	$0x80000000, AX
	MOVD	AX, X6
	PSHUFD	$0x00, X6, X6
	MOVOA	X6, X0
	MOVOA	X6, X1
	MOVQ	CX, DX
	ANDQ	$~7, DX
	JZ	reduce

loop8:
	MOVOU	(SI), X2
	MOVOU	16(SI), X3
	PXOR	X6, X2
	PXOR	X6, X3
	UMAXL(X2, X0, X4)
	UMAXL(X3, X1, X5)
	ADDQ	$32, SI
	SUBQ	$8, DX
	JNZ	loop8

reduce:
	UMAXL(X1, X0, X4)
	PSHUFD	$0x4e, X0, X1
	UMAXL(X1, X0, X4)
	PSHUFD	$0xb1, X0, X1
	UMAXL(X1, X0, X4)
	MOVD	X0, AX
	XORL	$0x80000000, AX
	ANDQ	$7, CX
	JZ	done

loop1:
	MOVL	(SI), DX
	CMPL	DX, AX
	CMOVLHI	DX, AX
	ADDQ	$4, SI
	DECQ	CX
	JNZ	loop1

done:
	MOVL	AX, ret+16(FP)
	RET
