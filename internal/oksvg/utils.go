// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2017 The oksvg Authors
// SPDX-FileCopyrightText: 2026 The Ebitengine Authors

package oksvg

import (
	"image/color"
	"strconv"
	"strings"

	"golang.org/x/image/colornames"

	"github.com/ironpark/ggfx/internal/rasterx"
)

// unitSuffixes are suffixes sometimes applied to the width and height attributes
// of the svg element.
var unitSuffixes = []string{"cm", "mm", "px", "pt"}

func parseColorValue(v string) (uint8, error) {
	v = strings.TrimSpace(v)
	if before, ok := strings.CutSuffix(v, "%"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(before))
		if err != nil {
			return 0, err
		}
		n = max(0, min(n, 100))
		return uint8(n * 0xFF / 100), nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	n = max(0, min(n, 255))
	return uint8(n), nil
}

// trimSuffixes removes unitSuffixes from any number that is not just numeric
func trimSuffixes(a string) (b string) {
	if a == "" || (a[len(a)-1] >= '0' && a[len(a)-1] <= '9') {
		return a
	}
	b = a
	for _, v := range unitSuffixes {
		b = strings.TrimSuffix(b, v)
	}
	return
}

// parseFloat is a helper function that strips suffixes before passing to strconv.ParseFloat
func parseFloat(s string, bitSize int) (float64, error) {
	val := trimSuffixes(strings.TrimSpace(s))
	return strconv.ParseFloat(val, bitSize)
}

// splitOnCommaOrSpace returns a list of strings after splitting the input on comma and space delimiters
func splitOnCommaOrSpace(s string) []string {
	return strings.FieldsFunc(s,
		func(r rune) bool {
			return r == ',' || r == ' '
		})
}

func readFraction(v string) (f float64, err error) {
	v = strings.TrimSpace(v)
	d := 1.0
	if strings.HasSuffix(v, "%") {
		d = 100
		v = strings.TrimSuffix(v, "%")
	}
	f, err = parseFloat(v, 64)
	f /= d
	// Is this is an unnecessary restriction? For now fractions can be all values not just in the range [0,1]
	// if f > 1 {
	// 	f = 1
	// } else if f < 0 {
	// 	f = 0
	// }
	return
}

// getColor is a helper function to get the background color
// if ReadGradUrl needs it.
func getColor(clr paint) color.Color {
	if clr.grad != nil { // This is a bit lazy but oh well
		for _, s := range clr.grad.Stops {
			if s.StopColor != nil {
				return s.StopColor
			}
		}
	} else if c, ok := clr.color.(color.NRGBA); ok {
		return c
	}
	return colornames.Black
}

func localizeGradIfStopClrNil(g *rasterx.Gradient, defaultColor paint) *rasterx.Gradient {
	grad := *g
	for _, s := range grad.Stops {
		if s.StopColor == nil { // This means we need copy the gradient's Stop slice
			// and fill in the default color

			// Copy the stops
			stops := make([]rasterx.GradStop, len(grad.Stops))
			copy(stops, grad.Stops)
			grad.Stops = stops
			// Use the background color when a stop color is nil
			clr := getColor(defaultColor)
			for i, s := range stops {
				if s.StopColor == nil {
					grad.Stops[i].StopColor = clr
				}
			}
			break // Only need to do this once
		}
	}
	return &grad
}
