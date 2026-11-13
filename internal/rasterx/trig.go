// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The functions here are the math package's sin, cos, tan and atan2, in Go
// (sin.go, tan.go, atan.go and atan2.go), with each product converted to
// float64 so that the compiler does not fuse it with an addition. On arm64
// it fuses the math package's, which then rounds otherwise than on amd64
// and wasm, where these return what it does, bit for bit.

package rasterx

import "math"

// reduceThreshold is the math package's: past it, its reduction of an
// argument by Pi/4 loses precision, and it reduces otherwise.
const reduceThreshold = 1 << 29

var _sin = [...]float64{
	1.58962301576546568060e-10, // 0x3de5d8fd1fd19ccd
	-2.50507477628578072866e-8, // 0xbe5ae5e5a9291f5d
	2.75573136213857245213e-6,  // 0x3ec71de3567d48a1
	-1.98412698295895385996e-4, // 0xbf2a01a019bfdf03
	8.33333333332211858878e-3,  // 0x3f8111111110f7d0
	-1.66666666666666307295e-1, // 0xbfc5555555555548
}

var _cos = [...]float64{
	-1.13585365213876817300e-11, // 0xbda8fa49a0861a9b
	2.08757008419747316778e-9,   // 0x3e21ee9d7b4e3f05
	-2.75573141792967388112e-7,  // 0xbe927e4f7eac4bc6
	2.48015872888517045348e-5,   // 0x3efa01a019c844f5
	-1.38888888888730564116e-3,  // 0xbf56c16c16c14f91
	4.16666666666665929218e-2,   // 0x3fa555555555554b
}

// reduce is |x|'s octant j and its place z in it, for |x| under
// reduceThreshold, and whether x was negative.
func reduce(x float64) (j uint64, z float64, neg bool) {
	const (
		PI4A = 7.85398125648498535156e-1  // 0x3fe921fb40000000, Pi/4 split into three parts
		PI4B = 3.77489470793079817668e-8  // 0x3e64442d00000000,
		PI4C = 2.69515142907905952645e-15 // 0x3ce8469898cc5170,
	)
	if x < 0 {
		x, neg = -x, true
	}
	j = uint64(x * (4 / math.Pi)) // integer part of x/(Pi/4), as integer for tests on the phase angle
	y := float64(j)               // integer part of x/(Pi/4), as float

	// map zeros to origin
	if j&1 == 1 {
		j++
		y++
	}
	z = ((x - float64(y*PI4A)) - float64(y*PI4B)) - float64(y*PI4C) // Extended precision modular arithmetic
	return j, z, neg
}

// sinPoly and cosPoly are sin and cos of z in [-Pi/4, Pi/4].
func sinPoly(z float64) float64 {
	zz := z * z
	p := float64(_sin[0]*zz) + _sin[1]
	p = float64(p*zz) + _sin[2]
	p = float64(p*zz) + _sin[3]
	p = float64(p*zz) + _sin[4]
	p = float64(p*zz) + _sin[5]
	return z + float64(float64(z*zz)*p)
}

func cosPoly(z float64) float64 {
	zz := z * z
	p := float64(_cos[0]*zz) + _cos[1]
	p = float64(p*zz) + _cos[2]
	p = float64(p*zz) + _cos[3]
	p = float64(p*zz) + _cos[4]
	p = float64(p*zz) + _cos[5]
	return 1.0 - float64(0.5*zz) + float64(float64(zz*zz)*p)
}

// Sin is math.Sin.
func Sin(x float64) float64 {
	if x == 0 || math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= reduceThreshold {
		return math.Sin(x)
	}
	j, z, sign := reduce(x)
	j &= 7 // octant modulo 2Pi radians (360 degrees)
	// reflect in x axis
	if j > 3 {
		sign = !sign
		j -= 4
	}
	var y float64
	if j == 1 || j == 2 {
		y = cosPoly(z)
	} else {
		y = sinPoly(z)
	}
	if sign {
		y = -y
	}
	return y
}

// Cos is math.Cos.
func Cos(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= reduceThreshold {
		return math.Cos(x)
	}
	j, z, _ := reduce(x)
	j &= 7 // octant modulo 2Pi radians (360 degrees)
	sign := false
	if j > 3 {
		j -= 4
		sign = !sign
	}
	if j > 1 {
		sign = !sign
	}
	var y float64
	if j == 1 || j == 2 {
		y = sinPoly(z)
	} else {
		y = cosPoly(z)
	}
	if sign {
		y = -y
	}
	return y
}

// Tan is math.Tan.
func Tan(x float64) float64 {
	if x == 0 || math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= reduceThreshold {
		return math.Tan(x)
	}
	j, z, sign := reduce(x)
	zz := float64(z * z)
	y := z
	if zz > 1e-14 {
		num := float64(-1.30936939181383777646e4*zz) + 1.15351664838587416140e6
		num = float64(num*zz) + -1.79565251976484877988e7
		den := zz + 1.36812963470692954678e4
		den = float64(den*zz) + -1.32089234440210967447e6
		den = float64(den*zz) + 2.50083801823357915839e7
		den = float64(den*zz) + -5.38695755929454629881e7
		y = z + float64(z*(float64(zz*num)/den))
	}
	if j&2 == 2 {
		y = -1 / y
	}
	if sign {
		y = -y
	}
	return y
}

// xatan is the math package's: atan of x in [0, 0.66].
func xatan(x float64) float64 {
	const (
		P0 = -8.750608600031904122785e-01
		P1 = -1.615753718733365076637e+01
		P2 = -7.500855792314704667340e+01
		P3 = -1.228866684490136173410e+02
		P4 = -6.485021904942025371773e+01
		Q0 = +2.485846490142306297962e+01
		Q1 = +1.650270098316988542046e+02
		Q2 = +4.328810604912902668951e+02
		Q3 = +4.853903996359136964868e+02
		Q4 = +1.945506571482613964425e+02
	)
	z := float64(x * x)
	num := float64(P0*z) + P1
	num = float64(num*z) + P2
	num = float64(num*z) + P3
	num = float64(num*z) + P4
	den := z + Q0
	den = float64(den*z) + Q1
	den = float64(den*z) + Q2
	den = float64(den*z) + Q3
	den = float64(den*z) + Q4
	z = float64(z*num) / den
	return float64(x*z) + x
}

// satan is the math package's: atan of a positive x.
func satan(x float64) float64 {
	const (
		Morebits = 6.123233995736765886130e-17 // pi/2 = PIO2 + Morebits
		Tan3pio8 = 2.41421356237309504880      // tan(3*pi/8)
	)
	if x <= 0.66 {
		return xatan(x)
	}
	if x > Tan3pio8 {
		return math.Pi/2 - xatan(1/x) + Morebits
	}
	return math.Pi/4 + xatan((x-1)/(x+1)) + 0.5*Morebits
}

// Atan2 is math.Atan2.
func Atan2(y, x float64) float64 {
	if math.IsNaN(y) || math.IsNaN(x) || y == 0 || x == 0 || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return math.Atan2(y, x) // exact
	}
	// Call atan and determine the quadrant.
	q := y / x
	switch {
	case q > 0:
		q = satan(q)
	case q < 0:
		q = -satan(-q)
	}
	if x < 0 {
		if q <= 0 {
			return q + math.Pi
		}
		return q - math.Pi
	}
	return q
}
