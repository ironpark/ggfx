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

// multiwindow opens two windows and draws a different color in each. Pass -frames N to quit
// after each window drew N frames, which is what the smoke test does.
package main

import (
	"flag"
	"fmt"
	"image/color"
	"log"
	"os"

	"github.com/ironpark/ggfx"
	"github.com/ironpark/ggfx/vector"
)

var (
	frames    = flag.Int("frames", 0, "quit after each window drew this many frames (0 runs until closed)")
	closeTest = flag.Bool("close", false, "close each window after -frames frames instead of quitting; Run then ends when the last window closes")
)

type app struct {
	windows map[*ggfx.Window]*state
}

type state struct {
	name   string
	color  color.RGBA
	frames int
	events int
	w, h   float64
}

func (a *app) HandleEvent(ev ggfx.Event) error {
	switch ev := ev.(type) {
	case ggfx.StartEvent:
		for i, s := range []*state{
			{name: "red", color: color.RGBA{0xc0, 0x30, 0x30, 0xff}},
			{name: "blue", color: color.RGBA{0x30, 0x30, 0xc0, 0xff}},
		} {
			w, err := ggfx.NewWindow(&ggfx.WindowOptions{
				Title:     s.name,
				Width:     320,
				Height:    240,
				Resizable: true,
			})
			if err != nil {
				return err
			}
			if i == 1 {
				x, y := w.Position()
				w.SetPosition(x+360, y)
			}
			a.windows[w] = s
		}
	case ggfx.FrameEvent:
		s := a.windows[ev.Window]
		s.frames++
		ev.Screen.Fill(s.color)
		b := ev.Screen.Bounds()
		// A moving square shows that frames are distinct.
		x := float32(s.frames%int(b.Dx()/4)) * 4
		vector.FillRect(ev.Screen, x, float32(b.Dy())/2-10*float32(ev.Scale), 20*float32(ev.Scale), 20*float32(ev.Scale), color.White, false)
		if *frames > 0 && *closeTest {
			if s.frames == *frames {
				fmt.Printf("%s: closing after %d frames\n", s.name, s.frames)
				ev.Window.Close()
			}
		} else if *frames > 0 {
			done := true
			for _, s := range a.windows {
				if s.frames < *frames {
					done = false
				}
			}
			if done {
				for w, s := range a.windows {
					ww, wh := w.Size()
					fmt.Printf("%s: %d frames, %d events, %gx%g DIP, window %dx%d\n", s.name, s.frames, s.events, s.w, s.h, ww, wh)
				}
				return ggfx.Termination
			}
		}
		ev.Window.RequestFrame()
	case ggfx.ResizeEvent:
		s := a.windows[ev.Window]
		s.w, s.h = ev.Width, ev.Height
		s.events++
	case ggfx.CloseEvent:
		fmt.Printf("%s: close event\n", a.windows[ev.Window].name)
		delete(a.windows, ev.Window)
	case ggfx.KeyEvent:
		if ev.Key == ggfx.KeyEscape && ev.Pressed {
			return ggfx.Termination
		}
		a.windows[ev.Window].events++
	}
	return nil
}

func main() {
	flag.Parse()
	a := &app{windows: map[*ggfx.Window]*state{}}
	if err := ggfx.Run(a, nil); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
