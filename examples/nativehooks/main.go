// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

// nativehooks exercises native text input and file drags. Use -windows 2 with
// Metal to check isolation, or -smoke for a deterministic macOS native-hook test.
package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"strings"
	"time"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/vector"
)

var smoke = flag.Bool("smoke", false, "inject and validate native callbacks on macOS")
var windows = flag.Int("windows", 2, "number of windows (use 1 for OpenGL or DirectX)")

type state struct {
	text, composition string
	drag              bool
	injected          bool
	events            []string
}
type app struct {
	states   map[*ggfx.Window]*state
	deadline time.Time
	done     bool
}

func (a *app) HandleEvent(event ggfx.Event) error {
	switch e := event.(type) {
	case ggfx.StartEvent:
		for i := 0; i < *windows; i++ {
			w, err := ggfx.NewWindow(&ggfx.WindowOptions{Title: "Native hooks: type text or drag a file", Width: 400, Height: 200, Resizable: true})
			if err != nil {
				return err
			}
			a.states[w] = &state{}
			w.SetTextInputEnabled(true)
			w.SetTextInputRect(20, 40, 2, 20)
			if i != 0 {
				x, y := w.Position()
				w.SetPosition(x+430*i, y)
			}
		}
		a.deadline = time.Now().Add(10 * time.Second)
	case ggfx.CompositionEvent:
		s := a.states[e.Window]
		s.composition = e.Text
		s.events = append(s.events, fmt.Sprintf("composition:%s:%d:%d:%t", e.Text, e.Start, e.End, e.Done))
		e.Window.RequestFrame()
	case ggfx.TextEvent:
		s := a.states[e.Window]
		s.text += e.Text
		s.events = append(s.events, fmt.Sprintf("text:%s:%d:%d:%t", e.Text, e.ReplacementStart, e.ReplacementEnd, e.HasReplacement))
		e.Window.RequestFrame()
	case ggfx.DragEvent:
		s := a.states[e.Window]
		s.drag = e.Phase == ggfx.DragEntered || e.Phase == ggfx.DragMoved
		s.events = append(s.events, fmt.Sprintf("drag:%d", e.Phase))
		if *smoke && (e.X != 30 || e.Y != 40) {
			return fmt.Errorf("drag coordinate mismatch: %g,%g", e.X, e.Y)
		}
	case ggfx.DropEvent:
		a.states[e.Window].events = append(a.states[e.Window].events, "drop")
	case ggfx.FrameEvent:
		if a.done {
			break
		}
		s := a.states[e.Window]
		bg := color.RGBA{30, 35, 45, 255}
		if s.drag {
			bg = color.RGBA{30, 100, 70, 255}
		}
		e.Screen.Fill(bg)
		vector.FillRect(e.Screen, 20*float32(e.Scale), 40*float32(e.Scale), 2*float32(e.Scale), 20*float32(e.Scale), color.White, false)
		e.Window.SetTitle(fmt.Sprintf("Text: %s | Composition: %s | Drag: %t", s.text, s.composition, s.drag))
		if !*smoke {
			break
		}
		if !s.injected {
			s.injected = true
			if err := injectNative(e.Window); err != nil {
				return err
			}
		}
		complete := true
		for _, st := range a.states {
			if !st.injected || len(st.events) < len(smokeEvents) {
				complete = false
				continue
			}
			if strings.Join(st.events, "\n") != strings.Join(smokeEvents, "\n") {
				return fmt.Errorf("unexpected native sequence:\n%s", strings.Join(st.events, "\n"))
			}
		}
		if complete {
			a.done = true
			fmt.Printf("native hooks: %d windows, composition/commit/replacement/drag/accessibility passed\n", len(a.states))
			for w := range a.states {
				w.Close()
			}
		} else {
			if time.Now().After(a.deadline) {
				return fmt.Errorf("native smoke timed out: %+v", s.events)
			}
			e.Window.RequestFrame()
		}
	}
	return nil
}

var smokeEvents = []string{
	"composition:한😀:3:7:false",
	"composition::0:0:true",
	"text:한:0:0:false",
	"composition:ㄱ:3:3:false",
	"composition::0:0:true",
	"text:é:-1:0:true",
	"drag:0", "drag:1", "drag:2", "drag:0", "drop", "drag:3",
}

func main() {
	flag.Parse()
	if *windows < 1 {
		log.Fatal("-windows must be positive")
	}
	if err := ggfx.Run(&app{states: map[*ggfx.Window]*state{}}, nil); err != nil {
		log.Fatal(err)
	}
}
