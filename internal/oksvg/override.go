// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package oksvg

import (
	"image/color"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// PaintOverrideKind selects how a NodeOverride replaces fill or stroke.
type PaintOverrideKind uint8

const (
	PaintOvUnset PaintOverrideKind = iota
	PaintOvNone
	PaintOvColor
	PaintOvCurrentColor
	PaintOvInherit
)

// PaintOverride is the internal representation of svg.Paint.
type PaintOverride struct {
	Kind  PaintOverrideKind
	Color color.Color
}

// PivotOverride is the internal representation of svg.Pivot.
type PivotOverride struct {
	Box  bool
	X, Y float64
}

// NodeOverride is the internal representation of svg.NodeOverride.
type NodeOverride struct {
	HasTranslate bool
	HasRotate    bool
	HasScale     bool
	HasSkew      bool
	HasPivot     bool
	HasMatrix    bool

	Tx, Ty       float64
	Rotate       float64
	Sx, Sy       float64
	SkewX, SkewY float64
	Pivot        PivotOverride
	Matrix       rasterx.Matrix2D

	HasOpacity bool
	Opacity    float64
	HasHidden  bool
	Hidden     bool
	HasVisible bool
	Visible    bool

	HasFill   bool
	Fill      PaintOverride
	HasStroke bool
	Stroke    PaintOverride
	HasColor  bool
	Color     color.Color

	HasFillOpacity   bool
	FillOpacity      float64
	HasStrokeOpacity bool
	StrokeOpacity    float64
	HasStrokeWidth   bool
	StrokeWidth      float64

	HasDash       bool
	Dash          []float64
	HasDashOffset bool
	DashOffset    float64

	HasTrim                        bool
	TrimStart, TrimEnd, TrimOffset float64
}

// EvalOptions controls scene evaluation for DrawWith, Renderer.Draw, and Bounds.
type EvalOptions struct {
	Time         float64 // document time in seconds
	CurrentColor color.Color
	Overrides    map[string]NodeOverride
	Animate      bool // true for DrawWith/Renderer.Draw, false for static Draw/Bounds(nil)
}
