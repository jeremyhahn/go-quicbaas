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

package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// countingReconciler records every pass and announces each one on a channel,
// so a test waits for a sweep rather than sleeping past one.
type countingReconciler struct {
	passes atomic.Int64
	swept  chan struct{}
	report sdlc.LabelReconcileReport
	err    error
}

func newCountingReconciler() *countingReconciler {
	return &countingReconciler{swept: make(chan struct{}, 16)}
}

// Reconcile implements labelReconciler.
func (r *countingReconciler) Reconcile(ctx context.Context) (sdlc.LabelReconcileReport, error) {
	r.passes.Add(1)
	select {
	case r.swept <- struct{}{}:
	default:
	}
	if r.err != nil {
		return sdlc.LabelReconcileReport{}, r.err
	}
	return r.report, ctx.Err()
}

// testReconcileServer is a Server with just enough of itself to run the sweep
// loop: the loop reads the logger and nothing else.
func testReconcileServer(t *testing.T) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout", Component: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return &Server{config: &config.Config{}, logger: log}
}

// awaitSweep waits for one pass rather than sleeping past one.
func awaitSweep(t *testing.T, r *countingReconciler) {
	t.Helper()
	select {
	case <-r.swept:
	case <-time.After(5 * time.Second):
		t.Fatal("the reconciliation loop did not sweep")
	}
}

// TestRunSDLCLabelReconcile_SweepsAtBootAndOnTheTicker is the property the
// wiring exists for: the repair runs with nobody asking. The boot pass is
// load-bearing on its own — a node that died between concluding a task and
// posting its outcome comes back holding exactly the inconsistency the sweep
// fixes.
func TestRunSDLCLabelReconcile_SweepsAtBootAndOnTheTicker(t *testing.T) {
	s := testReconcileServer(t)
	reconciler := newCountingReconciler()

	s.runSDLCLabelReconcile(reconciler, 5*time.Millisecond)
	t.Cleanup(s.stopSDLCLabelReconcile)

	awaitSweep(t, reconciler) // boot
	awaitSweep(t, reconciler) // ticker
	if got := reconciler.passes.Load(); got < 2 {
		t.Fatalf("passes = %d, want the boot pass and at least one tick", got)
	}
}

// TestRunSDLCLabelReconcile_StopWaitsForTheLoop proves shutdown is ordered: the
// goroutine has returned by the time stop returns, so the module's stores are
// not closed under a pass still writing to them.
func TestRunSDLCLabelReconcile_StopWaitsForTheLoop(t *testing.T) {
	s := testReconcileServer(t)
	reconciler := newCountingReconciler()

	s.runSDLCLabelReconcile(reconciler, time.Hour)
	awaitSweep(t, reconciler)

	done := s.sdlcLabelSweeps.done
	s.stopSDLCLabelReconcile()
	select {
	case <-done:
	default:
		t.Fatal("stop returned while the loop was still running")
	}
	if s.sdlcLabelSweeps != nil {
		t.Fatal("stop left the sweep handle behind")
	}
	// A second stop is a no-op rather than a nil dereference: shutdown paths
	// run more than once when a start failed halfway.
	s.stopSDLCLabelReconcile()
}

// TestRunSDLCLabelReconcile_AFailingPassKeepsSweeping is the negative half: a
// pass that cannot run leaves the state wrong for one more interval, which is
// strictly better than the previous behaviour of leaving it wrong for ever. It
// must not end the loop and must not spin.
func TestRunSDLCLabelReconcile_AFailingPassKeepsSweeping(t *testing.T) {
	s := testReconcileServer(t)
	reconciler := newCountingReconciler()
	reconciler.err = errors.New("broker down")

	s.runSDLCLabelReconcile(reconciler, 5*time.Millisecond)
	t.Cleanup(s.stopSDLCLabelReconcile)

	awaitSweep(t, reconciler)
	awaitSweep(t, reconciler)
	if got := reconciler.passes.Load(); got < 2 {
		t.Fatalf("passes = %d, want a failing pass to be followed by another", got)
	}
}

// TestRunSDLCLabelReconcile_ANonPositiveIntervalTakesTheDefault proves a
// misconfigured cadence cannot build a ticker the runtime panics on.
func TestRunSDLCLabelReconcile_ANonPositiveIntervalTakesTheDefault(t *testing.T) {
	s := testReconcileServer(t)
	reconciler := newCountingReconciler()

	s.runSDLCLabelReconcile(reconciler, 0)
	t.Cleanup(s.stopSDLCLabelReconcile)

	awaitSweep(t, reconciler)
	if s.sdlcLabelSweeps == nil {
		t.Fatal("a zero interval started no loop")
	}
}

// TestStartSDLCLabelReconcile_RefusesQuietlyWithoutItsCollaborators is the
// negative half of the mount: a node with no tenant manager or no task store
// logs and starts nothing, rather than refusing the whole module's boot over a
// sweep that only repairs display state.
func TestStartSDLCLabelReconcile_RefusesQuietlyWithoutItsCollaborators(t *testing.T) {
	s := testReconcileServer(t)
	s.config.SDLC.LabelReconcile = config.SDLCLabelReconcile{Interval: time.Hour}

	s.startSDLCLabelReconcile(nil, nil)
	if s.sdlcLabelSweeps != nil {
		s.stopSDLCLabelReconcile()
		t.Fatal("a node with no tenant manager started a reconciliation sweep")
	}
	if _, ok := s.sdlcReconcileTenants(); ok {
		t.Fatal("a node with no tenant manager offered a tenant lister")
	}
}

// TestLogSDLCLabelReconcile_ReportsOnlyWhatDrifted proves the logging
// discriminates: a converged fleet is silent, and everything else names its
// subject. It is asserted through the report rather than the log sink because
// what must not drift is which findings are considered worth a line.
func TestLogSDLCLabelReconcile_ReportsOnlyWhatDrifted(t *testing.T) {
	s := testReconcileServer(t)
	report := sdlc.LabelReconcileReport{Findings: []sdlc.LabelReconcileFinding{
		{TenantID: "t", ProjectID: "p", Issue: 1, Verdict: sdlc.LabelReconcileConsistent},
		{TenantID: "t", ProjectID: "p", Issue: 2, TaskID: "task-2", Outcome: sdlc.OutcomeBlocked,
			Verdict: sdlc.LabelReconcileSurfaced, Evidence: "posted"},
		{TenantID: "t", ProjectID: "p", Issue: 3, Verdict: sdlc.LabelReconcileDeferred, Evidence: "forge down"},
	}}
	if got := len(report.Drifted()); got != 2 {
		t.Fatalf("Drifted() = %d findings, want the two that are not consistent", got)
	}
	// The call itself must survive every shape, including the empty report a
	// converged fleet produces.
	s.logSDLCLabelReconcile(report)
	s.logSDLCLabelReconcile(sdlc.LabelReconcileReport{})
}
