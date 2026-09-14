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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// countingSweeper records every pass and announces it on a channel, so
// a test waits for a sweep rather than sleeping past one.
type countingSweeper struct {
	passes   atomic.Int64
	swept    chan struct{}
	nudged   int
	err      error
	failures []error
}

func newCountingSweeper() *countingSweeper {
	return &countingSweeper{swept: make(chan struct{}, 16)}
}

// Sweep implements sdlcBackstopSweeper.
func (s *countingSweeper) Sweep(ctx context.Context, _ orchestration.TenantLister) (int, error) {
	s.passes.Add(1)
	select {
	case s.swept <- struct{}{}:
	default:
	}
	if s.err != nil {
		return 0, s.err
	}
	return s.nudged, ctx.Err()
}

// SweepFailures implements sdlcBackstopSweeper: drained, so a failure is reported
// once and not on every later pass.
func (s *countingSweeper) SweepFailures() []error {
	failures := s.failures
	s.failures = nil
	return failures
}

// staticTenantLister is a tenant source for the loop.
type staticTenantLister struct{ ids []string }

func (l *staticTenantLister) ListTenantIDs(context.Context) ([]string, error) {
	return l.ids, nil
}

func awaitBackstopSweep(t *testing.T, what string, s *countingSweeper) {
	t.Helper()
	select {
	case <-s.swept:
	case <-time.After(5 * time.Second):
		t.Fatalf("the %s did not sweep", what)
	}
}

// triageBackstop and reviewFloor build the two backstops the loop drives, so
// a test names only what it is varying.
func triageBackstop(sweeper sdlcBackstopSweeper, interval time.Duration) sdlcBackstop {
	return sdlcBackstop{what: "triage backstop", counted: "nudged", interval: interval, sweeper: sweeper}
}

func reviewFloorBackstop(sweeper sdlcBackstopSweeper, interval time.Duration) sdlcBackstop {
	return sdlcBackstop{what: "review floor", counted: "started a review for", interval: interval, sweeper: sweeper}
}

// TestRunSDLCBackstops_SweepBothAtBootAndOnTheirOwnTicks proves the boot pass
// happens for EACH backstop — the deliveries a node missed are exactly the
// ones that arrived while it was down, and that is as true of a pull request
// that went green as of an issue somebody filed — and that one goroutine
// keeps both going on their own cadences.
func TestRunSDLCBackstops_SweepBothAtBootAndOnTheirOwnTicks(t *testing.T) {
	s := testReconcileServer(t)
	triage, review := newCountingSweeper(), newCountingSweeper()

	s.runSDLCBackstops(
		triageBackstop(triage, 10*time.Millisecond),
		reviewFloorBackstop(review, 10*time.Millisecond),
		&staticTenantLister{ids: []string{"t1"}})
	t.Cleanup(s.stopSDLCBackstops)

	awaitBackstopSweep(t, "triage backstop", triage)
	awaitBackstopSweep(t, "triage backstop", triage)
	awaitBackstopSweep(t, "review floor", review)
	awaitBackstopSweep(t, "review floor", review)
	if triage.passes.Load() < 2 || review.passes.Load() < 2 {
		t.Fatalf("the loop must keep sweeping both, got triage=%d review=%d",
			triage.passes.Load(), review.passes.Load())
	}
}

// TestRunSDLCBackstops_ABackstopWithNoSweeperIsLeftOut proves a node that
// mounts one half of the module runs that half and does not spin on a ticker
// nothing answers.
func TestRunSDLCBackstops_ABackstopWithNoSweeperIsLeftOut(t *testing.T) {
	s := testReconcileServer(t)
	review := newCountingSweeper()

	s.runSDLCBackstops(
		triageBackstop(nil, 10*time.Millisecond),
		reviewFloorBackstop(review, 10*time.Millisecond),
		&staticTenantLister{ids: []string{"t1"}})
	t.Cleanup(s.stopSDLCBackstops)

	awaitBackstopSweep(t, "review floor", review)
	awaitBackstopSweep(t, "review floor", review)
}

// TestRunSDLCBackstops_ANonPositiveIntervalTakesTheDefault proves a
// misconfigured interval does not become a hot loop or a dead one.
func TestRunSDLCBackstops_ANonPositiveIntervalTakesTheDefault(t *testing.T) {
	s := testReconcileServer(t)
	triage, review := newCountingSweeper(), newCountingSweeper()

	s.runSDLCBackstops(
		triageBackstop(triage, 0),
		reviewFloorBackstop(review, -time.Second),
		&staticTenantLister{ids: []string{"t1"}})
	t.Cleanup(s.stopSDLCBackstops)

	awaitBackstopSweep(t, "triage backstop", triage)
	awaitBackstopSweep(t, "review floor", review)
	if s.sdlcBackstops == nil {
		t.Fatal("a non-positive interval started no loop")
	}
}

// TestBackstopInterval_TakesTheDefaultOnlyForNonsense proves the interval
// helper honours what an operator set and substitutes only what it cannot use.
func TestBackstopInterval_TakesTheDefaultOnlyForNonsense(t *testing.T) {
	for name, tc := range map[string]struct {
		configured time.Duration
		want       time.Duration
	}{
		"an operator's interval is kept": {2 * time.Minute, 2 * time.Minute},
		"zero takes the default":         {0, sdlc.DefaultReviewSweepInterval},
		"negative takes the default":     {-time.Hour, sdlc.DefaultReviewSweepInterval},
	} {
		t.Run(name, func(t *testing.T) {
			if got := backstopInterval(tc.configured, sdlc.DefaultReviewSweepInterval); got != tc.want {
				t.Fatalf("backstopInterval(%s) = %s, want %s", tc.configured, got, tc.want)
			}
		})
	}
}

// TestStopSDLCBackstops_EndsTheLoopAndIsIdempotent proves shutdown waits for
// the goroutine and that stopping twice is safe.
func TestStopSDLCBackstops_EndsTheLoopAndIsIdempotent(t *testing.T) {
	s := testReconcileServer(t)
	triage, review := newCountingSweeper(), newCountingSweeper()

	s.runSDLCBackstops(
		triageBackstop(triage, time.Hour),
		reviewFloorBackstop(review, time.Hour),
		&staticTenantLister{ids: []string{"t1"}})
	awaitBackstopSweep(t, "triage backstop", triage)
	awaitBackstopSweep(t, "review floor", review)

	s.stopSDLCBackstops()
	if s.sdlcBackstops != nil {
		t.Fatal("the loop handle must be cleared")
	}
	s.stopSDLCBackstops()
}

// TestSweepSDLCBackstop_ReportsWhatItCouldNotDo proves a failing pass and a
// per-project failure are both survivable, and that the drained failures do
// not accumulate.
func TestSweepSDLCBackstop_ReportsWhatItCouldNotDo(t *testing.T) {
	tests := []struct {
		name    string
		sweeper *countingSweeper
	}{
		{
			name:    "a pass that could not enumerate tenants",
			sweeper: &countingSweeper{swept: make(chan struct{}, 4), err: errors.New("tenant store down")},
		},
		{
			name: "a pass that skipped one project",
			sweeper: &countingSweeper{
				swept: make(chan struct{}, 4), nudged: 2,
				failures: []error{errors.New("project proj-3 refused")},
			},
		},
		{
			name:    "a clean pass",
			sweeper: &countingSweeper{swept: make(chan struct{}, 4), nudged: 3},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testReconcileServer(t)
			backstop := reviewFloorBackstop(tc.sweeper, time.Hour)
			s.sweepSDLCBackstop(context.Background(), backstop, &staticTenantLister{ids: []string{"t1"}})
			if tc.sweeper.passes.Load() != 1 {
				t.Fatalf("one pass, got %d", tc.sweeper.passes.Load())
			}
			// A second pass must not re-report the first's failures.
			s.sweepSDLCBackstop(context.Background(), backstop, &staticTenantLister{ids: []string{"t1"}})
			if len(tc.sweeper.failures) != 0 {
				t.Fatalf("failures must be drained, got %d", len(tc.sweeper.failures))
			}
		})
	}

	// A backstop with no sweeper is a no-op rather than a nil dereference.
	s := testReconcileServer(t)
	s.sweepSDLCBackstop(context.Background(), reviewFloorBackstop(nil, time.Hour), &staticTenantLister{ids: []string{"t1"}})
}

// TestStartSDLCBackstops_RefusesQuietlyWithoutItsCollaborators proves a node
// with no tenant manager, or neither sweeper, logs and starts nothing rather
// than refusing the module's boot. The webhook path carries its own tenant and
// is unaffected, so both halves degrade to delivery-driven.
func TestStartSDLCBackstops_RefusesQuietlyWithoutItsCollaborators(t *testing.T) {
	tests := []struct {
		name           string
		triage, review sdlcBackstopSweeper
	}{
		{name: "no sweeper at all"},
		{name: "no tenant manager", triage: newCountingSweeper(), review: newCountingSweeper()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := testReconcileServer(t)
			s.config.SDLC.TriageSweepInterval = time.Hour
			s.config.SDLC.ReviewSweepInterval = time.Hour

			s.startSDLCBackstops(tc.triage, tc.review)
			if s.sdlcBackstops != nil {
				s.stopSDLCBackstops()
				t.Fatal("a node missing a collaborator started a backstop")
			}
		})
	}
	s := testReconcileServer(t)
	if _, ok := s.sdlcBackstopTenants(); ok {
		t.Fatal("a node with no tenant manager offered a tenant lister")
	}
}

// TestSDLCTriageForwarder_DeclaresItsShapeWithoutARuntime proves the parts
// the engine reads at REGISTRATION time answer before any module is mounted.
//
// The lease policy especially: it is read when a job takes its lease, which
// can happen before this node's module finished starting, and a triage job
// that took an on-budget lease because the policy was not yet readable would
// spend a worker slot for its whole run.
func TestSDLCTriageForwarder_DeclaresItsShapeWithoutARuntime(t *testing.T) {
	var runner orchestration.JobRunner = sdlcTriageForwarder{}

	if runner.Name() != sdlc.TriageRunnerName {
		t.Fatalf("name: want %q, got %q", sdlc.TriageRunnerName, runner.Name())
	}
	if len(runner.States()) == 0 {
		t.Fatal("the states must be readable before the module is mounted")
	}
	policy, ok := runner.(orchestration.JobLeasePolicy)
	if !ok {
		t.Fatal("the forwarder must declare the lease policy, not only the live runner")
	}
	if !policy.OffProjectBudget() {
		t.Fatal("triage must be off the project's worker budget")
	}
	want := sdlc.TriageLeaseSubject("proj-1")
	if got := policy.LeaseSubject(&orchestration.JobConfig{ProjectID: "proj-1"}); got != want {
		t.Fatalf("lease subject: want %q, got %q", want, got)
	}
	if got := policy.LeaseSubject(nil); got != "" {
		t.Fatalf("a nil config names no subject, got %q", got)
	}
	if got := policy.LeaseSubject(&orchestration.JobConfig{}); got != "" {
		t.Fatalf("a config with no project names no subject, got %q", got)
	}
}

// TestSDLCTriageForwarder_RefusesWithoutALiveModule proves the half that
// DOES need the runtime says so, rather than half-driving a job.
func TestSDLCTriageForwarder_RefusesWithoutALiveModule(t *testing.T) {
	live := sdlcModule.live.Load()
	sdlcModule.live.Store(nil)
	t.Cleanup(func() { sdlcModule.live.Store(live) })

	forwarder := sdlcTriageForwarder{}
	if err := forwarder.Validate(context.Background(), &orchestration.JobConfig{ProjectID: "p"}); !errors.Is(err, ErrSDLCUnavailable) {
		t.Fatalf("want ErrSDLCUnavailable, got %v", err)
	}
	if err := forwarder.Run(context.Background(), nil); !errors.Is(err, ErrSDLCUnavailable) {
		t.Fatalf("want ErrSDLCUnavailable, got %v", err)
	}
}
