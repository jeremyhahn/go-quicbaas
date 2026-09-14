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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// sweepHead is the head of the rig's pull request #41, distinct from
// dispatchHead so a test that mixes the two paths can tell them apart.
const sweepHead = "2222222222222222222222222222222222222222"

// sweepRig is a review floor over one bound project, a forge the test
// controls and a trigger that records what it was asked for.
type sweepRig struct {
	sweeper *ReviewSweeper
	store   *Store
	forge   *fakeForge
	trigger *fakeReviewTrigger
}

// newSweepRig builds a floor over one open, green, unjudged pull request
// (#41) whose project requires green checks and binds a reviewer.
func newSweepRig(t *testing.T) *sweepRig {
	t.Helper()
	store := newTestStore(t)
	binding := testBinding()
	binding.Reviewer = ReviewerConfig{Backend: "codex", Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge()
	forge.prs[41] = &ForgePullRequest{
		Number: 41, State: "open", HeadRef: "issue/41-thing", BaseRef: "main", HeadSHA: sweepHead,
	}
	forge.checks[sweepHead] = []ForgeCheckRun{greenRun("ci", sweepHead)}
	trigger := &fakeReviewTrigger{started: true}
	sweeper, err := NewReviewSweeper(ReviewSweeperConfig{
		Bindings: store, Forges: &fakeForges{forge: forge}, Reviews: trigger,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &sweepRig{sweeper: sweeper, store: store, forge: forge, trigger: trigger}
}

// rebind applies mutate to the rig's binding and stores it back.
func (r *sweepRig) rebind(t *testing.T, mutate func(*ProjectBinding)) {
	t.Helper()
	binding, err := r.store.GetBinding(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	mutate(binding)
	if err := r.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
}

// setChecks replaces the runs the rig's forge reports against sweepHead.
func (r *sweepRig) setChecks(runs ...ForgeCheckRun) {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	r.forge.checks[sweepHead] = append([]ForgeCheckRun(nil), runs...)
}

// reviewStatus is the status the review runner publishes under the
// reviewer's own context, in the state given.
func reviewStatus(state CheckStatus) ForgeCheckRun {
	return ForgeCheckRun{
		ID: "9001", Name: DefaultReviewStatusContext, HeadSHA: sweepHead, Status: "completed",
		Conclusion: string(state), Completed: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
	}
}

// TestReviewSweeper_SweepsWhatTheDeliveryPathNeverSaw is the defect this file
// exists for: a green pull request with no verdict on its head is reviewed
// from the pass alone, exactly once, with no delivery anywhere in the story.
func TestReviewSweeper_SweepsWhatTheDeliveryPathNeverSaw(t *testing.T) {
	rig := newSweepRig(t)

	started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if started != 1 {
		t.Fatalf("started %d reviews, want 1", started)
	}
	asked := rig.trigger.requests()
	if len(asked) != 1 {
		t.Fatalf("the trigger was asked %d times: %+v", len(asked), asked)
	}
	want := ReviewRequest{TenantID: testTenant, ProjectID: testProject, PullRequest: 41}
	if asked[0] != want {
		t.Fatalf("request = %+v, want %+v", asked[0], want)
	}
}

// TestReviewSweeper_TheHeadDecides walks the states a head can be in. The two
// filters are one forge read, and each of them has to hold on its own: a
// verdict already on the head ends it whatever the pipeline says, and a
// pipeline that has not finished green is not ready for a reviewer.
func TestReviewSweeper_TheHeadDecides(t *testing.T) {
	for name, tc := range map[string]struct {
		runs []ForgeCheckRun
		want int
	}{
		"green and unjudged is the candidate": {
			runs: []ForgeCheckRun{greenRun("ci", sweepHead)}, want: 1,
		},
		"a verdict already stands": {
			runs: []ForgeCheckRun{greenRun("ci", sweepHead), reviewStatus(CheckSuccess)}, want: 0,
		},
		"a rejection already stands": {
			runs: []ForgeCheckRun{greenRun("ci", sweepHead), reviewStatus(CheckFailure)}, want: 0,
		},
		// A pending status under the reviewer's context is a review that has
		// been started — a draft declined, a head left for a later round, or
		// one in flight. Reading its STATE rather than its presence would
		// start a second reviewer on the same pull request.
		"a review is already in flight": {
			runs: []ForgeCheckRun{greenRun("ci", sweepHead), reviewStatus(CheckPending)}, want: 0,
		},
		"the pipeline is still running": {
			runs: []ForgeCheckRun{pendingRun("ci", sweepHead)}, want: 0,
		},
		"the pipeline is red": {
			runs: []ForgeCheckRun{redRun("ci", sweepHead)}, want: 0,
		},
		"one of two checks is still running": {
			runs: []ForgeCheckRun{greenRun("ci", sweepHead), pendingRun("lint", sweepHead)}, want: 0,
		},
		// A gated project with no pipeline at all is an operator's
		// misconfiguration, left visible rather than reviewed on checks
		// nobody ran — the same answer the delivery path gives.
		"a gated project with no checks": {runs: nil, want: 0},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newSweepRig(t)
			rig.setChecks(tc.runs...)

			started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
			if err != nil {
				t.Fatalf("SweepProject: %v", err)
			}
			if started != tc.want {
				t.Fatalf("started %d reviews, want %d", started, tc.want)
			}
			if got := len(rig.trigger.requests()); got != tc.want {
				t.Fatalf("the trigger was asked %d times, want %d", got, tc.want)
			}
		})
	}
}

// TestReviewSweeper_AnUngatedProjectReadsNoPipeline proves the floor agrees
// with the dispatcher about a project whose checks do not decide whether a
// change may land: the change is a candidate, and the same single status read
// still answers whether it has been judged.
func TestReviewSweeper_AnUngatedProjectIsSweptOnTheVerdictAlone(t *testing.T) {
	rig := newSweepRig(t)
	rig.rebind(t, func(b *ProjectBinding) { b.Review.RequireGreenChecks = false })
	rig.setChecks(redRun("ci", sweepHead))

	started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil || started != 1 {
		t.Fatalf("SweepProject: started=%d err=%v", started, err)
	}

	rig.setChecks(redRun("ci", sweepHead), reviewStatus(CheckFailure))
	started, err = rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil || started != 0 {
		t.Fatalf("a judged head was swept again: started=%d err=%v", started, err)
	}
}

// TestReviewSweeper_CostsOneListingAndOneStatusReadPerPullRequest pins the
// forge budget. The pass runs on a timer against every bound project, so a
// second read per pull request is a cost multiplied by the whole fleet — and
// the reason both filters are answered from one listing.
func TestReviewSweeper_CostsOneListingAndOneStatusReadPerPullRequest(t *testing.T) {
	rig := newSweepRig(t)
	rig.forge.prs[42] = &ForgePullRequest{Number: 42, State: "open", HeadSHA: "3333", BaseRef: "main"}
	rig.forge.checks["3333"] = []ForgeCheckRun{greenRun("ci", "3333"), reviewStatus(CheckSuccess)}
	rig.forge.prs[43] = &ForgePullRequest{Number: 43, State: "closed", HeadSHA: "4444", BaseRef: "main"}

	if _, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if got := rig.forge.listOpenPRCalls(); got != 1 {
		t.Fatalf("listed the open pull requests %d times, want 1", got)
	}
	// Two OPEN pull requests, one status read each, and nothing for the
	// closed one — which the listing never returned.
	if got := rig.forge.checkCalls(); got != 2 {
		t.Fatalf("read the head statuses %d times, want 2", got)
	}
}

// TestReviewSweeper_DiscoveryModeDecidesWhetherThePassLooks proves `webhook`
// sweeps nothing and makes no forge call at all, while `poll` and `both` do.
func TestReviewSweeper_DiscoveryModeDecidesWhetherThePassLooks(t *testing.T) {
	for name, tc := range map[string]struct {
		mode      DiscoveryMode
		want      int
		wantCalls int
	}{
		"the default sweeps": {mode: "", want: 1, wantCalls: 1},
		"both sweeps":        {mode: DiscoveryBoth, want: 1, wantCalls: 1},
		"poll sweeps":        {mode: DiscoveryPoll, want: 1, wantCalls: 1},
		// Not merely "starts nothing": a webhook-only project must cost no
		// forge call either, or every such project on the node pays for a
		// pass it asked not to have.
		"webhook does not sweep": {mode: DiscoveryWebhook, want: 0, wantCalls: 0},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newSweepRig(t)
			rig.rebind(t, func(b *ProjectBinding) { b.Discovery = tc.mode })

			started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
			if err != nil {
				t.Fatalf("SweepProject: %v", err)
			}
			if started != tc.want {
				t.Fatalf("started %d reviews, want %d", started, tc.want)
			}
			if got := rig.forge.listOpenPRCalls(); got != tc.wantCalls {
				t.Fatalf("made %d forge listings, want %d", got, tc.wantCalls)
			}
		})
	}
}

// TestDispatchReview_DiscoveryModeDecidesWhetherADeliveryActs is the other
// half of the same matrix: a poll-only project ignores a delivery, and the
// two modes that admit one act on it.
func TestDispatchReview_DiscoveryModeDecidesWhetherADeliveryActs(t *testing.T) {
	for name, tc := range map[string]struct {
		mode DiscoveryMode
		want bool
	}{
		"the default dispatches": {mode: "", want: true},
		"both dispatches":        {mode: DiscoveryBoth, want: true},
		"webhook dispatches":     {mode: DiscoveryWebhook, want: true},
		"poll ignores it":        {mode: DiscoveryPoll, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			trigger := &fakeReviewTrigger{started: true}
			event := &ForgeEvent{Kind: EventCheckRunUpdated, HeadSHA: dispatchHead}
			started, err := DispatchReview(context.Background(), trigger, testTenant, deliveredTo(tc.mode), event)
			if err != nil {
				t.Fatalf("DispatchReview: %v", err)
			}
			if started != tc.want {
				t.Fatalf("started = %v, want %v", started, tc.want)
			}
			if got := len(trigger.requests()); (got > 0) != tc.want {
				t.Fatalf("the trigger was asked %d times", got)
			}
		})
	}
}

// TestReviewSweeper_ADeliveryAndAPassNameTheSameWorkInBothMode is the
// double-start property. Both triggers resolve to one pull request, so both
// jobs take the SAME lease subject and the engine admits one of them — which
// is where the answer belongs, because a sweeper-side flag would disagree
// with the lease across a restart or a second node.
func TestReviewSweeper_ADeliveryAndAPassNameTheSameWorkInBothMode(t *testing.T) {
	rig := newSweepRig(t)
	binding, err := rig.store.GetBinding(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject); err != nil {
			t.Errorf("SweepProject: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		event := &ForgeEvent{Kind: EventCheckRunUpdated, PullRequestNumber: 41, HeadSHA: sweepHead}
		if _, err := DispatchReview(context.Background(), rig.trigger, testTenant, binding, event); err != nil {
			t.Errorf("DispatchReview: %v", err)
		}
	}()
	wg.Wait()

	asked := rig.trigger.requests()
	if len(asked) != 2 {
		t.Fatalf("both triggers must reach the port, got %d: %+v", len(asked), asked)
	}
	want := ReviewLeaseSubject(testProject, 41)
	for _, req := range asked {
		if req.PullRequest != 41 {
			// The delivery names the pull request; the pass names the pull
			// request it listed. A path that named something else would take
			// a different lease and a second reviewer would run.
			t.Fatalf("a trigger named pull request %d: %+v", req.PullRequest, req)
		}
		if got := ReviewLeaseSubject(req.ProjectID, req.PullRequest); got != want {
			t.Fatalf("lease subject = %q, want %q", got, want)
		}
	}
}

// TestReviewSweeper_OneProjectsForgeFailureDoesNotStopTheRest proves a pass is
// per project: the failure is typed, names the project, and the other tenant's
// project is still swept.
func TestReviewSweeper_OneProjectsForgeFailureDoesNotStopTheRest(t *testing.T) {
	rig := newSweepRig(t)
	other := testBinding()
	other.ProjectID = "proj-2"
	other.Reviewer = ReviewerConfig{Backend: "codex", Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	if err := rig.store.PutBinding(context.Background(), testTenant, other); err != nil {
		t.Fatal(err)
	}
	// One forge serves both bindings, and it refuses the listing for the
	// first project visited; the failure must be carried, not thrown.
	outage := errors.New("forge unreachable")
	rig.forge.listOpenPRsErr = outage

	started, err := rig.sweeper.Sweep(context.Background(), &fakeTenants{ids: []string{testTenant}})
	if err != nil {
		t.Fatalf("a per-project failure ended the pass: %v", err)
	}
	if started != 0 {
		t.Fatalf("started %d reviews", started)
	}
	failures := rig.sweeper.SweepFailures()
	if len(failures) != 2 {
		t.Fatalf("want a failure per project, got %d: %v", len(failures), failures)
	}
	var sweepErr *ReviewSweepError
	if !errors.As(failures[0], &sweepErr) {
		t.Fatalf("failure is not a ReviewSweepError: %v", failures[0])
	}
	if sweepErr.ProjectID == "" || sweepErr.TenantID != testTenant || sweepErr.Op != "ListOpenPullRequests" {
		t.Fatalf("failure does not name the project and the call: %+v", sweepErr)
	}
	if !errors.Is(failures[0], outage) {
		t.Fatalf("the forge's own failure is not reachable: %v", failures[0])
	}
	if drained := rig.sweeper.SweepFailures(); len(drained) != 0 {
		t.Fatalf("failures must be drained, got %d", len(drained))
	}
}

// TestReviewSweeper_SweepStampsTheTenantOnTheContext is the bug that made the
// triage backstop report clean passes while starting nothing: the engine
// guards StartJob on the tenant bound to the CONTEXT, and this pass runs on a
// background context of its own.
func TestReviewSweeper_SweepStampsTheTenantOnTheContext(t *testing.T) {
	rig := newSweepRig(t)
	seen := &tenantRecordingTrigger{}
	sweeper, err := NewReviewSweeper(ReviewSweeperConfig{
		Bindings: rig.store, Forges: &fakeForges{forge: rig.forge}, Reviews: seen,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := sweeper.Sweep(context.Background(), &fakeTenants{ids: []string{testTenant}}); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := seen.tenants(); len(got) != 1 || got[0] != testTenant {
		t.Fatalf("the trigger saw context tenants %v, want [%s]", got, testTenant)
	}
}

// TestReviewSweeper_SweepRefusesWhatItCannotVisit covers the pass's bad
// inputs: a nil tenant lister would report a clean pass over nothing, and a
// tenant store that will not answer is a fault rather than an empty fleet.
func TestReviewSweeper_SweepRefusesWhatItCannotVisit(t *testing.T) {
	rig := newSweepRig(t)

	if _, err := rig.sweeper.Sweep(context.Background(), nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a nil tenant lister was accepted: %v", err)
	}
	down := errors.New("tenant store down")
	if _, err := rig.sweeper.Sweep(context.Background(), &fakeTenants{err: down}); !errors.Is(err, down) {
		t.Fatalf("want the tenant store's failure, got %v", err)
	}
	// A tenant whose bindings cannot be listed costs that tenant a pass.
	failing := &failingBindings{BindingReader: rig.store, err: errors.New("binding store down")}
	sweeper, err := NewReviewSweeper(ReviewSweeperConfig{
		Bindings: failing, Forges: &fakeForges{forge: rig.forge}, Reviews: rig.trigger,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sweeper.Sweep(context.Background(), &fakeTenants{ids: []string{testTenant}}); err != nil {
		t.Fatalf("a tenant's listing failure ended the pass: %v", err)
	}
	if len(sweeper.SweepFailures()) != 1 {
		t.Fatal("the tenant's listing failure was not reported")
	}
}

// TestReviewSweeper_SweepProjectRefusesWhatItCannotScope covers the
// per-project entry point's bad inputs, and the two quiet no-ops that are
// real configurations rather than faults.
func TestReviewSweeper_SweepProjectRefusesWhatItCannotScope(t *testing.T) {
	rig := newSweepRig(t)

	if _, err := rig.sweeper.SweepProject(context.Background(), "", testProject); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unscoped tenant was accepted: %v", err)
	}
	if _, err := rig.sweeper.SweepProject(context.Background(), testTenant, "  "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unscoped project was accepted: %v", err)
	}
	// An unbound project is most projects on a node: quiet, and no forge call.
	started, err := rig.sweeper.SweepProject(context.Background(), testTenant, "no-such-project")
	if err != nil || started != 0 {
		t.Fatalf("an unbound project: started=%d err=%v", started, err)
	}
	if got := rig.forge.listOpenPRCalls(); got != 0 {
		t.Fatalf("an unbound project cost %d forge listings", got)
	}
	// A project that binds no reviewer can never produce a verdict, so the
	// pass has nothing to start and reads nothing to find that out.
	rig.rebind(t, func(b *ProjectBinding) { b.Reviewer = ReviewerConfig{} })
	if started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject); err != nil || started != 0 {
		t.Fatalf("a project with no reviewer: started=%d err=%v", started, err)
	}
	if got := rig.forge.listOpenPRCalls(); got != 0 {
		t.Fatalf("a project with no reviewer cost %d forge listings", got)
	}
}

// TestReviewSweeper_ForgeFailuresAreTypedAndNameTheCall walks the three calls
// a project's pass makes and proves each failure arrives named rather than
// bare.
func TestReviewSweeper_ForgeFailuresAreTypedAndNameTheCall(t *testing.T) {
	boom := errors.New("boom")
	for name, tc := range map[string]struct {
		arrange func(*sweepRig)
		wantOp  string
	}{
		"the adapter cannot be built": {
			arrange: func(r *sweepRig) { r.swapForges(&fakeForges{err: boom}) },
			wantOp:  "ForgeFor",
		},
		"no adapter is registered": {
			arrange: func(r *sweepRig) { r.swapForges(&fakeForges{}) },
			wantOp:  "ForgeFor",
		},
		"the open listing fails": {
			arrange: func(r *sweepRig) { r.forge.listOpenPRsErr = boom },
			wantOp:  "ListOpenPullRequests",
		},
		"a head's statuses cannot be read": {
			arrange: func(r *sweepRig) { r.forge.listChecksErr = boom },
			wantOp:  "ListCheckRuns",
		},
		"the trigger refuses": {
			arrange: func(r *sweepRig) { r.trigger.err = boom },
			wantOp:  "TriggerReview",
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newSweepRig(t)
			tc.arrange(rig)

			_, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
			var sweepErr *ReviewSweepError
			if !errors.As(err, &sweepErr) {
				t.Fatalf("want a ReviewSweepError, got %v", err)
			}
			if sweepErr.Op != tc.wantOp {
				t.Fatalf("Op = %q, want %q", sweepErr.Op, tc.wantOp)
			}
			if sweepErr.ProjectID != testProject || sweepErr.TenantID != testTenant {
				t.Fatalf("the failure does not name the project: %+v", sweepErr)
			}
			if sweepErr.Error() == "" || sweepErr.Unwrap() == nil {
				t.Fatalf("the failure carries no cause: %+v", sweepErr)
			}
		})
	}
}

// TestReviewSweeper_ACancelledPassStops proves shutdown reaches the pass: the
// daemon cancels the backstop's context, and a pass that ignored it would keep
// reading the forge and starting jobs while the module detaches.
func TestReviewSweeper_ACancelledPassStops(t *testing.T) {
	rig := newSweepRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := rig.sweeper.Sweep(ctx, &fakeTenants{ids: []string{testTenant}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sweep on a cancelled context: %v", err)
	}
	if _, err := rig.sweeper.SweepProject(ctx, testTenant, testProject); err == nil {
		t.Fatal("SweepProject on a cancelled context reported a clean pass")
	}
	if got := len(rig.trigger.requests()); got != 0 {
		t.Fatalf("a cancelled pass started %d reviews", got)
	}
}

// TestReviewSweeper_ABindingThatCannotBeReadIsAFault keeps the per-project
// entry point fail-closed: an ABSENT binding is a quiet no-op, but a store
// that will not answer must not read as "this project is not bound".
func TestReviewSweeper_ABindingThatCannotBeReadIsAFault(t *testing.T) {
	rig := newSweepRig(t)
	down := errors.New("binding store down")
	sweeper, err := NewReviewSweeper(ReviewSweeperConfig{
		Bindings: &failingBindings{BindingReader: rig.store, getErr: down},
		Forges:   &fakeForges{forge: rig.forge},
		Reviews:  rig.trigger,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sweeper.SweepProject(context.Background(), testTenant, testProject); !errors.Is(err, down) {
		t.Fatalf("want the store's failure, got %v", err)
	}
}

// TestReviewSweeper_APullRequestWithNoHeadIsPassedOver proves the pass does
// not read statuses for a listing entry that names no commit: a verdict would
// bind to nothing, and asking the forge for the statuses of "" is a refusal
// that would cost the whole project its pass.
func TestReviewSweeper_APullRequestWithNoHeadIsPassedOver(t *testing.T) {
	rig := newSweepRig(t)
	rig.forge.prs[41] = &ForgePullRequest{Number: 41, State: "open", BaseRef: "main"}

	started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil || started != 0 {
		t.Fatalf("SweepProject: started=%d err=%v", started, err)
	}
	if got := rig.forge.checkCalls(); got != 0 {
		t.Fatalf("a headless pull request cost %d status reads", got)
	}
}

// TestNewReviewSweeper_RefusesAnIncompleteWiring proves a floor that could
// not work is refused at construction rather than at three in the morning.
func TestNewReviewSweeper_RefusesAnIncompleteWiring(t *testing.T) {
	store := newTestStore(t)
	forges := &fakeForges{forge: newFakeForge()}
	trigger := &fakeReviewTrigger{}

	for name, cfg := range map[string]ReviewSweeperConfig{
		"no bindings": {Forges: forges, Reviews: trigger},
		"no forges":   {Bindings: store, Reviews: trigger},
		"no trigger":  {Bindings: store, Forges: forges},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewReviewSweeper(cfg); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("an incomplete wiring was accepted: %v", err)
			}
		})
	}
	sweeper, err := NewReviewSweeper(ReviewSweeperConfig{Bindings: store, Forges: forges, Reviews: trigger})
	if err != nil {
		t.Fatalf("a complete wiring was refused: %v", err)
	}
	if sweeper.now == nil {
		t.Fatal("a sweeper with no clock cannot evaluate a settle-free green")
	}
}

// TestCheckRunPresent walks the helper that decides "has this head been
// through review", including the inputs that must NOT read as a verdict.
func TestCheckRunPresent(t *testing.T) {
	runs := []ForgeCheckRun{
		{Name: "CI / CI Gate (pull_request)"},
		{Name: "  " + DefaultReviewStatusContext + " "},
	}
	for name, tc := range map[string]struct {
		runs    []ForgeCheckRun
		context string
		want    bool
	}{
		"the reviewer's context, whitespace and all": {runs, DefaultReviewStatusContext, true},
		"case does not decide it":                    {runs, "QuicBaaS/Review", true},
		"another project's context":                  {runs, "other/review", false},
		"no runs at all":                             {nil, DefaultReviewStatusContext, false},
		// An empty context would match the unnamed runs some forges return,
		// and every head would read as already reviewed.
		"an empty context matches nothing": {[]ForgeCheckRun{{Name: ""}}, "", false},
		"a run with no name":               {[]ForgeCheckRun{{Name: "  "}}, DefaultReviewStatusContext, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkRunPresent(tc.runs, tc.context); got != tc.want {
				t.Fatalf("checkRunPresent = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestReviewSweepError_ReadsAsOneLine proves the deferred failure an operator
// sees names the project, the call and the cause.
func TestReviewSweepError_ReadsAsOneLine(t *testing.T) {
	err := &ReviewSweepError{TenantID: testTenant, ProjectID: testProject, Op: "ListOpenPullRequests", Cause: errors.New("502")}
	message := err.Error()
	for _, want := range []string{testTenant, testProject, "ListOpenPullRequests", "502"} {
		if !strings.Contains(message, want) {
			t.Fatalf("%q does not name %q", message, want)
		}
	}
}

// swapForges points the rig's sweeper at another resolver.
func (r *sweepRig) swapForges(forges ForgeResolver) {
	r.sweeper.forges = forges
}

// tenantRecordingTrigger records the tenant bound to each call's CONTEXT,
// which is what the engine guards StartJob on.
type tenantRecordingTrigger struct {
	mu   sync.Mutex
	seen []string
}

func (t *tenantRecordingTrigger) TriggerReview(ctx context.Context, _ ReviewRequest) (bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seen = append(t.seen, orchestration.TenantFromContext(ctx))
	return true, nil
}

func (t *tenantRecordingTrigger) tenants() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.seen...)
}

// failingBindings is a BindingReader whose listing, or whose read of one
// binding, fails.
type failingBindings struct {
	BindingReader
	err    error
	getErr error
}

func (f *failingBindings) ListBindings(ctx context.Context, tenantID string) ([]*ProjectBinding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.BindingReader.ListBindings(ctx, tenantID)
}

func (f *failingBindings) GetBinding(ctx context.Context, tenantID, projectID string) (*ProjectBinding, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.BindingReader.GetBinding(ctx, tenantID, projectID)
}
