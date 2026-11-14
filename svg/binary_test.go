// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svg_test

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/svg"
)

// What a binary form holds: fills by either rule, strokes with their caps,
// joins and dashes, opacity, shapes, and transforms on a shape and a group.
var binaryDocs = []string{
	svgDoc("0 0 24 24", `<path d="M12 15V3M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4m4-9 5 5 5-5" fill="none" stroke="#fff" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`),
	svgDoc("0 0 24 24", `<path fill-rule="evenodd" d="M2 2h20v20H2zM8 8h8v8H8z" fill="#fff" fill-opacity=".5"/>`),
	svgDoc("2 3 20 18", `<g transform="translate(2 3) rotate(10)"><circle cx="9" cy="8" r="6" fill="#f00" stroke="#00f" stroke-width="1.3" stroke-dasharray="3 1.5" stroke-dashoffset="1"/><rect x="1" y="1" width="12" height="7" rx="2" transform="scale(1.1 .9)" fill="none" stroke="#0f0" stroke-linejoin="miter" stroke-miterlimit="3" stroke-opacity=".7" stroke-linecap="square"/></g>`),
	svgDoc("0 0 10 10", `<line x1="1" y1="1" x2="9" y2="9" stroke="#fff"/><polyline points="1,9 5,2 9,9" fill="none" stroke="#fff" stroke-linejoin="bevel"/><ellipse cx="5" cy="5" rx="4" ry="2"/>`),
	svgDoc("0 0 10 10", ``),
}

func TestABinaryFormDrawsAsTheDocumentItWasWrittenFrom(t *testing.T) {
	for i, src := range binaryDocs {
		d := parse(t, src)
		data, err := d.MarshalBinary()
		if err != nil {
			t.Fatalf("document %d: %v", i, err)
		}
		var back svg.Document
		if err := back.UnmarshalBinary(data); err != nil {
			t.Fatalf("document %d: %v", i, err)
		}
		x, y, w, h := d.ViewBox()
		if bx, by, bw, bh := back.ViewBox(); bx != x || by != y || bw != w || bh != h {
			t.Fatalf("document %d: view box %v %v %v %v read back as %v %v %v %v", i, x, y, w, h, bx, by, bw, bh)
		}
		var g ggfx.GeoM
		g.Translate(-x, -y)
		g.Scale(3.7, 3.7)
		bounds := image.Rect(0, 0, 96, 96)
		want, got := drawInto(d, bounds, g), drawInto(&back, bounds, g)
		if !bytes.Equal(want.Pix, got.Pix) {
			t.Errorf("document %d draws differently from its binary form", i)
		}
		again, err := back.MarshalBinary()
		if err != nil || !bytes.Equal(again, data) {
			t.Errorf("document %d: the form read back writes a different one (%v)", i, err)
		}
	}
}

func TestADocumentWithMoreThanPlainShapesHasNoBinaryForm(t *testing.T) {
	for name, body := range map[string]string{
		"gradient": `<defs><linearGradient id="g"><stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/></linearGradient></defs><rect width="10" height="10" fill="url(#g)"/>`,
		"clip":     `<defs><clipPath id="c"><rect width="5" height="5"/></clipPath></defs><rect width="10" height="10" clip-path="url(#c)"/>`,
		"mask":     `<defs><mask id="m"><rect width="5" height="5" fill="#fff"/></mask></defs><rect width="10" height="10" mask="url(#m)"/>`,
		"marker":   `<defs><marker id="k" markerWidth="2" markerHeight="2"><circle cx="1" cy="1" r="1"/></marker></defs><path d="M1 1L9 9" stroke="#000" marker-end="url(#k)"/>`,
	} {
		if _, err := parse(t, svgDoc("0 0 10 10", body)).MarshalBinary(); !errors.Is(err, svg.ErrNoBinaryForm) {
			t.Errorf("a document with a %s: %v, want ErrNoBinaryForm", name, err)
		}
	}
}

func TestUnmarshalBinaryRefusesWhatIsNotAForm(t *testing.T) {
	data, err := parse(t, binaryDocs[0]).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var d svg.Document
	for n := 0; n < len(data); n++ {
		if err := d.UnmarshalBinary(data[:n]); err == nil {
			t.Fatalf("the first %d of %d bytes read as a document", n, len(data))
		}
	}
	if err := d.UnmarshalBinary(append(bytes.Clone(data), 0)); err == nil {
		t.Error("a form with a byte after it read as a document")
	}
	newer := bytes.Clone(data)
	newer[0] = 0xff
	if err := d.UnmarshalBinary(newer); err == nil {
		t.Error("a form of a version not written yet read as a document")
	}
}

// Whatever bytes it is given, UnmarshalBinary returns a document that can
// be drawn or an error.
func FuzzUnmarshalBinary(f *testing.F) {
	for _, src := range binaryDocs {
		d, err := svg.Parse(bytes.NewReader([]byte(src)))
		if err != nil {
			f.Fatal(err)
		}
		data, err := d.MarshalBinary()
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<12 {
			return
		}
		var d svg.Document
		if d.UnmarshalBinary(data) != nil {
			return
		}
		var g ggfx.GeoM
		g.Scale(2, 2)
		drawInto(&d, image.Rect(0, 0, 32, 32), g)
		if _, err := d.MarshalBinary(); err != nil {
			t.Fatalf("a form that was read cannot be written: %v", err)
		}
	})
}
