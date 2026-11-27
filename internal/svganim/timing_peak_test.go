// Copyright 2026 The ggfx Authors
// SPDX-License-Identifier: Apache-2.0

package svganim

import (
	"math"
	"testing"
	"time"
)

func buildSyncFloodTimings() []*trackTiming {
	// Track 0 has 2,000 static begins and shortens itself via syncbase end.
	begins := make([]beginSpec, 2000)
	for i := range begins {
		begins[i] = beginSpec{kind: beginOffset, offset: float64(i) * 0.001}
	}
	begins = append(begins, beginSpec{kind: beginSyncEnd, syncID: "src", offset: 0.0005})
	timings := []*trackTiming{
		{
			id:          "src",
			simpleDur:   10,
			begins:      begins,
			ends:        []beginSpec{{kind: beginSyncBegin, syncID: "src", offset: 0.0005}},
			restart:     restartAlways,
			repeatCount: 1,
			repeatDur:   math.NaN(),
			minDur:      math.NaN(),
			maxDur:      math.NaN(),
		},
	}
	// 128 dependent tracks triggered on every src.begin and src.end -> high fanout.
	for range 128 {
		timings = append(timings, &trackTiming{
			simpleDur: 0.001,
			begins: []beginSpec{
				{kind: beginSyncBegin, syncID: "src", offset: 0},
				{kind: beginSyncEnd, syncID: "src", offset: 0},
			},
			restart:     restartAlways,
			repeatCount: 1,
			repeatDur:   math.NaN(),
			minDur:      math.NaN(),
			maxDur:      math.NaN(),
		})
	}
	return timings
}

// Item M3 & Y4: verify the SMIL syncbase queue cap bounds peak queue length and
// actively refuses enqueues once capacity is reached.
func TestSMILSyncQueuePeakBounded(t *testing.T) {
	t.Run("refuses-at-injectable-cap", func(t *testing.T) {
		const smallCap = 256
		timings := buildSyncFloodTimings()
		type res struct{ peak, refused int }
		done := make(chan res, 1)
		go func() {
			p, r := resolveSMILTimingsWithLimit(timings, smallCap)
			done <- res{p, r}
		}()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case got := <-done:
			if got.peak != smallCap {
				t.Errorf("peak queue = %d; want exact cap %d", got.peak, smallCap)
			}
			if got.refused == 0 {
				t.Errorf("refused enqueues = 0; want > 0 at cap %d", smallCap)
			}
		case <-timer.C:
			t.Fatal("resolveSMILTimingsWithLimit(smallCap) stalled past 2s")
		}
	})

	t.Run("bounded-under-default-cap", func(t *testing.T) {
		timings := buildSyncFloodTimings()
		done := make(chan int, 1)
		go func() {
			p, _ := resolveSMILTimingsWithLimit(timings, maxSyncQueue)
			done <- p
		}()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case peak := <-done:
			if peak <= 0 || peak > maxSyncQueue {
				t.Fatalf("peak queue = %d; want in (0, %d]", peak, maxSyncQueue)
			}
		case <-timer.C:
			t.Fatal("resolveSMILTimingsWithLimit(maxSyncQueue) stalled past 2s")
		}
	})
}
