// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package workerimage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestRefresh_IsRefusedWithNoLoopToPickItUp(t *testing.T) {
	h := newHarness(t, testConfig())
	if err := h.r.RequestRefresh(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("err = %v, want ErrNotRunning", err)
	}
}

// requestWhenServing asks for a pass as soon as the loop reports itself
// serving, without sleeping: the loop's start is a goroutine this test does
// not otherwise synchronise with.
func requestWhenServing(t *testing.T, r *Refresher) {
	t.Helper()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		err := r.RequestRefresh()
		if err == nil {
			return
		}
		if !errors.Is(err, ErrNotRunning) {
			t.Fatalf("RequestRefresh: %v", err)
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("the loop never started serving")
		}
	}
}

func TestRun_RunsNothingAtStartARequestedPassOnDemandAndStopsWithItsContext(t *testing.T) {
	h := newHarness(t, testConfig())
	h.observer.observed = make(chan Outcome, 4)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.r.Run(ctx, Schedule{RefreshInterval: time.Hour, PassTimeout: time.Minute})
	}()

	// No pass runs at start: the first thing the loop does is the request.
	requestWhenServing(t, h.r)
	if got := <-h.observer.observed; got != OutcomePromoted {
		t.Fatalf("requested pass = %s, want promoted", got)
	}
	if h.versions.calls != 3 {
		t.Fatalf("the registry was read %d times; only the requested pass may read it", h.versions.calls)
	}
	if last := h.r.Status().Last; last == nil || last.Trigger != TriggerManual {
		t.Fatalf("last = %+v", last)
	}

	cancel()
	<-done
	if h.r.Status().Serving {
		t.Fatal("a stopped loop must not report itself serving")
	}
}

func TestRequestRefresh_RefusesASecondRequestWhileOneIsQueued(t *testing.T) {
	h := newHarness(t, testConfig())
	h.r.serving.Store(true)
	if err := h.r.RequestRefresh(); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if !h.r.Status().Queued {
		t.Fatal("a queued request must show on the status")
	}
	var busy *RefreshInProgressError
	if err := h.r.RequestRefresh(); !errors.As(err, &busy) || !busy.Since.IsZero() {
		t.Fatalf("err = %v, want a queued RefreshInProgressError", err)
	}
	h.r.running.Store(true)
	h.r.runningSince.Store(time.Now().UnixNano())
	if err := h.r.RequestRefresh(); !errors.As(err, &busy) || busy.Since.IsZero() {
		t.Fatalf("err = %v, want a running RefreshInProgressError", err)
	}
	if !h.r.Status().Running || h.r.Status().RunningSince.IsZero() {
		t.Fatal("a running pass must show on the status")
	}
}

func TestSchedule_ZeroFieldsTakeTheirDefaults(t *testing.T) {
	got := Schedule{}.withDefaults()
	if got.RefreshInterval != DefaultRefreshInterval || got.PassTimeout != DefaultPassTimeout {
		t.Fatalf("defaults = %+v", got)
	}
	kept := Schedule{RefreshInterval: time.Minute, PassTimeout: time.Second}.withDefaults()
	if kept.RefreshInterval != time.Minute || kept.PassTimeout != time.Second {
		t.Fatalf("configured values must be kept: %+v", kept)
	}
}
