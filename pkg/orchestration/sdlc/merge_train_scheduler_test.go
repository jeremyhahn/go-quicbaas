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

package sdlc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// What wakes a train
// =============================================================================

// newTrainScheduler builds a scheduler over one project, trained or not.
func newTrainScheduler(t *testing.T, trained bool) (*TrainScheduler, *fakeJobStarter, *Store) {
	t.Helper()
	scheduler, starter, store, _ := newClockedTrainScheduler(t, trained)
	return scheduler, starter, store
}

// newClockedTrainScheduler is newTrainScheduler with the clock its formation
// interval is measured on.
func newClockedTrainScheduler(t *testing.T, trained bool) (*TrainScheduler, *fakeJobStarter, *Store, *testClock) {
	t.Helper()
	store := newTestStore(t)
	binding := testBinding()
	if trained {
		binding.Reviewer = trainTestReviewer()
	}
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	starter := &fakeJobStarter{}
	clock := newTestClock()
	scheduler, err := NewTrainScheduler(TrainSchedulerConfig{Bindings: store, Trains: store, Jobs: starter, Now: clock.now})
	if err != nil {
		t.Fatalf("NewTrainScheduler: %v", err)
	}
	return scheduler, starter, store, clock
}

func TestTrainScheduler_ANudgeStartsOneTrainJobForATrainedProject(t *testing.T) {
	scheduler, starter, _ := newTrainScheduler(t, true)
	if err := scheduler.NudgeTrain(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("NudgeTrain: %v", err)
	}
	started := starter.requests()
	if len(started) != 1 || started[0].Runner != TrainRunnerName || started[0].ProjectID != testProject || started[0].TaskID != "" {
		t.Fatalf("started = %+v, want one project train job", started)
	}
}

func TestTrainScheduler_AnUntrainedOrUnboundProjectIsQuiet(t *testing.T) {
	scheduler, starter, _ := newTrainScheduler(t, false)
	if err := scheduler.NudgeTrain(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("untrained: %v", err)
	}
	if err := scheduler.NudgeTrain(context.Background(), testTenant, "no-such-project"); err != nil {
		t.Fatalf("unbound: %v", err)
	}
	if len(starter.requests()) != 0 {
		t.Fatalf("started %d job(s) for projects with no train", len(starter.requests()))
	}
}

// TestTrainScheduler_ATrainAlreadyRunningIsNotAFailure proves the lease is the
// bound and its refusal is the system working.
func TestTrainScheduler_ATrainAlreadyRunningIsNotAFailure(t *testing.T) {
	scheduler, starter, _, clock := newClockedTrainScheduler(t, true)
	starter.err = &orchestration.JobLeaseError{Sentinel: orchestration.ErrJobLeaseTaken, Message: "held"}
	if err := scheduler.NudgeTrain(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("a held lease surfaced as %v", err)
	}
	clock.advance(DefaultTrainFormationInterval)
	starter.err = errors.New("engine down")
	if err := scheduler.NudgeTrain(context.Background(), testTenant, testProject); !errors.Is(err, ErrBackend) {
		t.Fatalf("an engine failure = %v, want a backend error", err)
	}
}

func TestTrainScheduler_RefusesAnUnscopedNudge(t *testing.T) {
	scheduler, _, _ := newTrainScheduler(t, true)
	if err := scheduler.NudgeTrain(context.Background(), "", testProject); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no tenant: %v", err)
	}
	if err := scheduler.NudgeTrain(context.Background(), testTenant, " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no project: %v", err)
	}
}

// TestTrainScheduler_TheSweepStartsATrainOnlyWhereSomethingWaits proves the
// backstop is cheap: a quiet project costs a queue read and nothing else.
func TestTrainScheduler_TheSweepStartsATrainOnlyWhereSomethingWaits(t *testing.T) {
	scheduler, starter, store := newTrainScheduler(t, true)
	tenants := &fakeTenants{ids: []string{testTenant}}
	nudged, err := scheduler.Sweep(context.Background(), tenants)
	if err != nil || nudged != 0 || len(starter.requests()) != 0 {
		t.Fatalf("a quiet project: nudged=%d err=%v started=%d", nudged, err, len(starter.requests()))
	}
	if err := store.SaveTrainEntry(context.Background(), testTenant, &TrainEntry{
		ProjectID: testProject, TaskID: "task-1", PullRequest: 5, HeadSHA: "abc", Target: "main",
		Branch: "issue/5", State: TrainEntryQueued,
	}); err != nil {
		t.Fatal(err)
	}
	nudged, err = scheduler.Sweep(context.Background(), tenants)
	if err != nil || nudged != 1 {
		t.Fatalf("a waiting change: nudged=%d err=%v", nudged, err)
	}
	if tenantsSeen := starter.contextTenants(); len(tenantsSeen) != 1 || tenantsSeen[0] != testTenant {
		t.Fatalf("the sweep's StartJob carried tenant %v on its context", tenantsSeen)
	}
	if failures := scheduler.SweepFailures(); len(failures) != 0 {
		t.Fatalf("failures = %v", failures)
	}
}

func TestTrainScheduler_TheSweepWakesATrainLeftInFlight(t *testing.T) {
	scheduler, starter, store := newTrainScheduler(t, true)
	ledger, err := store.GetTrainLedger(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	ledger.Active = planTrain(1)
	if err := store.SaveTrainLedger(context.Background(), testTenant, ledger); err != nil {
		t.Fatal(err)
	}
	if nudged, err := scheduler.Sweep(context.Background(), &fakeTenants{ids: []string{testTenant}}); err != nil || nudged != 1 {
		t.Fatalf("nudged=%d err=%v, want the orphaned train woken", nudged, err)
	}
	if len(starter.requests()) != 1 {
		t.Fatal("no job was started for the train in flight")
	}
}

func TestTrainScheduler_ASweepThatCannotEnumerateTenantsFails(t *testing.T) {
	scheduler, _, _ := newTrainScheduler(t, true)
	if _, err := scheduler.Sweep(context.Background(), nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil tenants: %v", err)
	}
	if _, err := scheduler.Sweep(context.Background(), &fakeTenants{err: errors.New("iam down")}); !errors.Is(err, ErrBackend) {
		t.Fatalf("unlistable tenants: %v", err)
	}
}

func TestNewTrainScheduler_RefusesAnIncompleteWiring(t *testing.T) {
	store := newTestStore(t)
	cases := map[string]TrainSchedulerConfig{
		"no bindings": {Trains: store, Jobs: &fakeJobStarter{}},
		"no trains":   {Bindings: store, Jobs: &fakeJobStarter{}},
		"no jobs":     {Bindings: store, Trains: store},
		"a negative formation interval": {
			Bindings: store, Trains: store, Jobs: &fakeJobStarter{}, FormationInterval: -time.Second,
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewTrainScheduler(cfg); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("NewTrainScheduler = %v", err)
			}
		})
	}
}

// TestTrainScheduler_FormationIsRateLimitedPerProject is X15's bound: a change
// re-queued as fast as a train ejected it started a train every thirty seconds
// (go-luks2, trains #79 to #152). Inside the interval a nudge starts nothing;
// once it has passed the next one does, and another project is never held by
// this one's interval.
func TestTrainScheduler_FormationIsRateLimitedPerProject(t *testing.T) {
	scheduler, starter, store, clock := newClockedTrainScheduler(t, true)
	other := testBinding()
	other.ProjectID = "project-2"
	other.Reviewer = trainTestReviewer()
	if err := store.PutBinding(context.Background(), testTenant, other); err != nil {
		t.Fatal(err)
	}
	nudge := func(projectID string) {
		t.Helper()
		if err := scheduler.NudgeTrain(context.Background(), testTenant, projectID); err != nil {
			t.Fatalf("NudgeTrain(%s): %v", projectID, err)
		}
	}
	nudge(testProject)
	clock.advance(30 * time.Second)
	nudge(testProject)
	if started := len(starter.requests()); started != 1 {
		t.Fatalf("started %d train jobs inside the interval, want 1", started)
	}
	nudge("project-2")
	if started := len(starter.requests()); started != 2 {
		t.Fatalf("another project was held by this one's interval: started %d, want 2", started)
	}
	clock.advance(DefaultTrainFormationInterval)
	nudge(testProject)
	if started := len(starter.requests()); started != 3 {
		t.Fatalf("started %d train jobs once the interval passed, want 3", started)
	}
}

// TestTrainScheduler_AFailedStartDoesNotHoldTheInterval: an engine that
// refused the start started nothing, so the very next nudge tries again.
func TestTrainScheduler_AFailedStartDoesNotHoldTheInterval(t *testing.T) {
	scheduler, starter, _ := newTrainScheduler(t, true)
	starter.err = errors.New("engine down")
	if err := scheduler.NudgeTrain(context.Background(), testTenant, testProject); !errors.Is(err, ErrBackend) {
		t.Fatalf("an engine failure = %v, want a backend error", err)
	}
	starter.err = nil
	if err := scheduler.NudgeTrain(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("NudgeTrain: %v", err)
	}
	if started := len(starter.requests()); started != 1 {
		t.Fatalf("started %d train jobs after a failed start, want 1", started)
	}
}
