// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package trig

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/rand"
	"runtime"
	"testing"
)

// The trigonometric functions return the same bits on every
// architecture: those the math package's return where the compiler fuses
// none of their products, as on wasm (and amd64 below GOAMD64=v3).
func TestTrigIsTheSameEverywhere(t *testing.T) {
	for _, tc := range []struct {
		name    string
		f, math func(float64) float64
		want    string
	}{
		{"Sin", Sin, math.Sin, "0dba088738d3"},
		{"Cos", Cos, math.Cos, "27a21c177f25"},
		{"Tan", Tan, math.Tan, "c5b476c7cb0c"},
		{"Atan2(x, 0.7)", func(x float64) float64 { return Atan2(x, 0.7) }, func(x float64) float64 { return math.Atan2(x, 0.7) }, "fa98d821ee64"},
		{"Atan2(0.3, x)", func(x float64) float64 { return Atan2(0.3, x) }, func(x float64) float64 { return math.Atan2(0.3, x) }, "faee7ea28f22"},
	} {
		r := rand.New(rand.NewSource(1))
		h := sha256.New()
		var b [8]byte
		for range 1000000 {
			x := (r.Float64() - 0.5) * math.Pow(10, float64(r.Intn(8)-3))
			y := tc.f(x)
			if m := tc.math(x); y != m && runtime.GOARCH == "wasm" {
				t.Fatalf("%s at %v: %v; math's %v", tc.name, x, y, m)
			}
			binary.LittleEndian.PutUint64(b[:], math.Float64bits(y))
			h.Write(b[:])
		}
		if got := hex.EncodeToString(h.Sum(nil)[:6]); got != tc.want {
			t.Errorf("%s: hashed %s; want %s", tc.name, got, tc.want)
		}
	}
}

// Sincos is Sin and Cos, bit for bit.
func TestSincosIsSinAndCos(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for range 1000000 {
		x := (r.Float64() - 0.5) * math.Pow(10, float64(r.Intn(8)-3))
		s, c := Sincos(x)
		if s != Sin(x) || c != Cos(x) {
			t.Fatalf("Sincos(%v) = %v, %v; Sin, Cos %v, %v", x, s, c, Sin(x), Cos(x))
		}
		if ms, mc := math.Sincos(x); (s != ms || c != mc) && runtime.GOARCH == "wasm" {
			t.Fatalf("Sincos(%v) = %v, %v; math's %v, %v", x, s, c, ms, mc)
		}
	}
	for _, x := range []float64{0, math.Copysign(0, -1), math.Inf(1), math.Inf(-1), 1 << 30, -(1 << 31)} {
		s, c := Sincos(x)
		if ms, mc := math.Sincos(x); math.Float64bits(s) != math.Float64bits(ms) || math.Float64bits(c) != math.Float64bits(mc) {
			t.Errorf("Sincos(%v) = %v, %v; math's %v, %v", x, s, c, ms, mc)
		}
	}
}
