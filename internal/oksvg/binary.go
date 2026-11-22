// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"math"
	reflectlite "reflect" // the package has a function named reflect
	"slices"

	"golang.org/x/image/math/fixed"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// The binary form of an icon is what reading its SVG arrives at, for an
// icon known ahead of time to be kept without its source and drawn without
// reading it: the view box and, for each path, its paints, its stroke, its
// transform and its segments. It holds the icons made of shapes painted in
// plain colors, which is what an icon library's are; one that needs more,
// a gradient, a clip path, a mask or a marker, has no binary form.
//
// The first byte is the version. A newer reader reads every older version;
// an older reader refuses a newer one.
const (
	binaryVersionV1 = 1
	binaryVersionV2 = 2
)

// ErrNoBinaryForm is why an icon has no binary form.
var ErrNoBinaryForm = errors.New("oksvg: an icon with gradients, clips, masks, markers, or animations has no binary form")

// The stroke functions by their numbers in the binary form, zero for none.
var (
	binaryCaps = []rasterx.CapFunc{nil, rasterx.ButtCap, rasterx.SquareCap, rasterx.RoundCap, rasterx.CubicCap, rasterx.QuadraticCap}
	binaryGaps = []rasterx.GapFunc{nil, rasterx.FlatGap, rasterx.RoundGap, rasterx.CubicGap, rasterx.QuadraticGap}
)

func funcIndex[F any](list []F, f F) (byte, bool) {
	p := reflectlite.ValueOf(f).Pointer()
	for i, c := range list {
		if reflectlite.ValueOf(c).Pointer() == p {
			return byte(i), true
		}
	}
	return 0, false
}

// Flags of a path in the binary form.
const (
	binaryFill = 1 << iota
	binaryLine
	binaryNonZero
	binaryMatrix
	// binarySame: painted, stroked and transformed as the path before it,
	// which most of an icon's paths are, so none of that follows.
	binarySame
	// v2 flags: fill or stroke uses SVG currentColor (no 4-byte RGBA follows).
	binaryFillCurrent
	binaryLineCurrent
	binaryKnownFlagsV1 = binaryFill | binaryLine | binaryNonZero | binaryMatrix | binarySame
	binaryKnownFlagsV2 = binaryKnownFlagsV1 | binaryFillCurrent | binaryLineCurrent
)

// AppendBinary appends the icon's binary form to b.
func (s *SvgIcon) AppendBinary(b []byte) ([]byte, error) {
	if s.Animated() || s.needsSceneForStatic() {
		return nil, ErrNoBinaryForm
	}
	version := byte(binaryVersionV1)
	for i := range s.SVGPaths {
		p := &s.SVGPaths[i]
		if p.clip != nil || p.markers != [3]string{} || p.fillerColor.grad != nil || p.linerColor.grad != nil {
			return nil, ErrNoBinaryForm
		}
		if p.fillerColor.current || p.linerColor.current {
			version = binaryVersionV2
		}
	}
	b = append(b, version)
	for _, v := range []float64{s.ViewBox.X, s.ViewBox.Y, s.ViewBox.W, s.ViewBox.H} {
		b = appendFloat(b, v)
	}
	b = binary.AppendUvarint(b, uint64(len(s.SVGPaths)))
	for i := range s.SVGPaths {
		p := &s.SVGPaths[i]
		var flags byte
		fill, okFill := p.fillerColor.color.(color.NRGBA)
		line, okLine := p.linerColor.color.(color.NRGBA)
		if p.fillerColor.current {
			flags |= binaryFillCurrent
		} else if p.fillerColor.color != nil {
			if !okFill {
				return nil, ErrNoBinaryForm
			}
			flags |= binaryFill
		}
		if p.linerColor.current {
			flags |= binaryLineCurrent
		} else if p.linerColor.color != nil {
			if !okLine {
				return nil, ErrNoBinaryForm
			}
			flags |= binaryLine
		}
		if p.UseNonZeroWinding {
			flags |= binaryNonZero
		}
		if p.mAdder.M != rasterx.Identity {
			flags |= binaryMatrix
		}
		if i > 0 && sameStyle(p, &s.SVGPaths[i-1]) {
			b = append(b, binarySame)
			b = appendPath(b, p.Path)
			continue
		}
		b = append(b, flags)
		if flags&binaryFill != 0 {
			b = append(b, fill.R, fill.G, fill.B, fill.A)
			b = appendFloat(b, p.FillOpacity)
		} else if flags&binaryFillCurrent != 0 {
			b = appendFloat(b, p.FillOpacity)
		}
		if flags&(binaryLine|binaryLineCurrent) != 0 {
			lineCap, ok1 := funcIndex(binaryCaps, p.LineCap)
			lead, ok2 := funcIndex(binaryCaps, p.LeadLineCap)
			gap, ok3 := funcIndex(binaryGaps, p.LineGap)
			if !ok1 || !ok2 || !ok3 {
				return nil, ErrNoBinaryForm
			}
			if flags&binaryLine != 0 {
				b = append(b, line.R, line.G, line.B, line.A)
			}
			b = append(b, lineCap, lead, gap, byte(p.LineJoin))
			for _, v := range []float64{p.LineOpacity, p.LineWidth, p.MiterLimit, p.DashOffset} {
				b = appendFloat(b, v)
			}
			b = binary.AppendUvarint(b, uint64(len(p.Dash)))
			for _, v := range p.Dash {
				b = appendFloat(b, v)
			}
		}
		if flags&binaryMatrix != 0 {
			m := p.mAdder.M
			for _, v := range []float64{m.A, m.B, m.C, m.D, m.E, m.F} {
				b = appendFloat(b, v)
			}
		}
		b = appendPath(b, p.Path)
	}
	return b, nil
}

func appendPath(b []byte, p rasterx.Path) []byte {
	b = binary.AppendUvarint(b, uint64(len(p)))
	for _, v := range p {
		b = binary.AppendVarint(b, int64(v))
	}
	return b
}

// sameStyle reports whether a and b, both with a binary form, are painted,
// stroked and transformed alike.
func sameStyle(a, b *SvgPath) bool {
	capOf := func(f rasterx.CapFunc) byte { i, _ := funcIndex(binaryCaps, f); return i }
	gapOf := func(f rasterx.GapFunc) byte { i, _ := funcIndex(binaryGaps, f); return i }
	return a.fillerColor == b.fillerColor && a.linerColor == b.linerColor &&
		a.FillOpacity == b.FillOpacity && a.LineOpacity == b.LineOpacity &&
		a.LineWidth == b.LineWidth && a.DashOffset == b.DashOffset && a.MiterLimit == b.MiterLimit &&
		slices.Equal(a.Dash, b.Dash) && a.UseNonZeroWinding == b.UseNonZeroWinding &&
		capOf(a.LineCap) == capOf(b.LineCap) && capOf(a.LeadLineCap) == capOf(b.LeadLineCap) &&
		gapOf(a.LineGap) == gapOf(b.LineGap) && a.LineJoin == b.LineJoin && a.mAdder.M == b.mAdder.M
}

// appendFloat appends v in four bytes where a float32 holds it exactly,
// which the numbers an icon is written with mostly are, and in eight
// otherwise, after a byte saying which.
func appendFloat(b []byte, v float64) []byte {
	if f := float32(v); float64(f) == v {
		return binary.LittleEndian.AppendUint32(append(b, 4), math.Float32bits(f))
	}
	return binary.LittleEndian.AppendUint64(append(b, 8), math.Float64bits(v))
}

var errBinary = errors.New("oksvg: not an icon's binary form")

// binaryReader reads a binary form, remembering the first thing wrong.
type binaryReader struct {
	b   []byte
	err error
}

func (r *binaryReader) fail() { r.err, r.b = errBinary, nil }

func (r *binaryReader) byte() byte {
	if len(r.b) < 1 {
		r.fail()
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *binaryReader) float() float64 {
	switch n := r.byte(); {
	case n == 4 && len(r.b) >= 4:
		v := math.Float32frombits(binary.LittleEndian.Uint32(r.b))
		r.b = r.b[4:]
		return float64(v)
	case n == 8 && len(r.b) >= 8:
		v := math.Float64frombits(binary.LittleEndian.Uint64(r.b))
		r.b = r.b[8:]
		return v
	}
	r.fail()
	return 0
}

// countFor reads a length of records each at least minItemBytes bytes long.
func (r *binaryReader) countFor(minItemBytes int) int {
	v, n := binary.Uvarint(r.b)
	if n <= 0 || v > uint64(len(r.b)-n)/uint64(minItemBytes) {
		r.fail()
		return 0
	}
	r.b = r.b[n:]
	return int(v)
}

func (r *binaryReader) color() color.NRGBA {
	return color.NRGBA{R: r.byte(), G: r.byte(), B: r.byte(), A: r.byte()}
}

func (r *binaryReader) path() rasterx.Path {
	p := make(rasterx.Path, r.countFor(1))
	for j := range p {
		v, n := binary.Varint(r.b)
		if n <= 0 || v < math.MinInt32 || v > math.MaxInt32 {
			r.fail()
			return nil
		}
		r.b = r.b[n:]
		p[j] = fixed.Int26_6(v)
	}
	return p
}

// ReadBinary is the icon that AppendBinary wrote as data.
func ReadBinary(data []byte) (*SvgIcon, error) {
	r := &binaryReader{b: data}
	v := r.byte()
	if r.err == nil && v > binaryVersionV2 {
		return nil, fmt.Errorf("oksvg: an icon's binary form of version %d, newer than this reads", v)
	} else if v == 0 {
		r.fail()
	}
	knownFlags := byte(binaryKnownFlagsV1)
	if v >= binaryVersionV2 {
		knownFlags = binaryKnownFlagsV2
	}
	s := &SvgIcon{Transform: rasterx.Identity}
	s.ViewBox.X, s.ViewBox.Y, s.ViewBox.W, s.ViewBox.H = r.float(), r.float(), r.float(), r.float()
	s.SVGPaths = make([]SvgPath, r.countFor(2))
	for i := range s.SVGPaths {
		p := &s.SVGPaths[i]
		p.PathStyle = defaultStyle
		p.fillerColor = paint{}
		flags := r.byte()
		if flags&^knownFlags != 0 {
			return nil, errBinary
		}
		if flags&binarySame != 0 {
			if i == 0 || flags != binarySame {
				return nil, errBinary
			}
			p.PathStyle = s.SVGPaths[i-1].PathStyle
			if p.Path = r.path(); r.err != nil || !validPath(p.Path) {
				return nil, errBinary
			}
			continue
		}
		if (flags&binaryFill != 0 && flags&binaryFillCurrent != 0) ||
			(flags&binaryLine != 0 && flags&binaryLineCurrent != 0) {
			return nil, errBinary
		}
		p.UseNonZeroWinding = flags&binaryNonZero != 0
		if flags&binaryFill != 0 {
			p.fillerColor = paint{color: r.color()}
			p.FillOpacity = r.float()
		} else if flags&binaryFillCurrent != 0 {
			p.fillerColor = paint{current: true}
			p.FillOpacity = r.float()
			s.usesCurrentColor = true
		}
		if flags&(binaryLine|binaryLineCurrent) != 0 {
			if flags&binaryLine != 0 {
				p.linerColor = paint{color: r.color()}
			} else {
				p.linerColor = paint{current: true}
				s.usesCurrentColor = true
			}
			lineCap, lead, gap, join := r.byte(), r.byte(), r.byte(), r.byte()
			if int(lineCap) >= len(binaryCaps) || int(lead) >= len(binaryCaps) || int(gap) >= len(binaryGaps) || rasterx.JoinMode(join) > rasterx.Round {
				r.fail()
				break
			}
			p.LineCap, p.LeadLineCap, p.LineGap, p.LineJoin = binaryCaps[lineCap], binaryCaps[lead], binaryGaps[gap], rasterx.JoinMode(join)
			p.LineOpacity, p.LineWidth, p.MiterLimit, p.DashOffset = r.float(), r.float(), r.float(), r.float()
			if n := r.countFor(5); n > 0 {
				p.Dash = make([]float64, n)
				for j := range p.Dash {
					p.Dash[j] = r.float()
				}
			}
		}
		if flags&binaryMatrix != 0 {
			p.mAdder.M = rasterx.Matrix2D{A: r.float(), B: r.float(), C: r.float(), D: r.float(), E: r.float(), F: r.float()}
		}
		p.Path = r.path()
		if r.err != nil || !validPath(p.Path) {
			return nil, errBinary
		}
	}
	if r.err != nil || len(r.b) != 0 {
		return nil, errBinary
	}
	return s, nil
}

// validPath reports whether p is commands each with the points it takes,
// which drawing a path relies on.
func validPath(p rasterx.Path) bool {
	for i := 0; i < len(p); {
		switch rasterx.PathCommand(p[i]) {
		case rasterx.PathMoveTo, rasterx.PathLineTo:
			i += 3
		case rasterx.PathQuadTo:
			i += 5
		case rasterx.PathCubicTo:
			i += 7
		case rasterx.PathClose:
			i++
		default:
			return false
		}
		if i > len(p) {
			return false
		}
	}
	return true
}
