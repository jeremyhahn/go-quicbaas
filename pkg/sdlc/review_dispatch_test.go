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
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeJobStarter records the jobs a caller asked the engine to start. It is
// the package's one job-starting double — the review dispatcher and the
// triage scheduler both drive orchestration.JobStarter, so a second copy
// would only be a second thing to keep in step.
type fakeJobStarter struct {
	mu      sync.Mutex
	started []*orchestration.StartJobRequest
	err     error
	noID    bool
	seq     int

	// ctxTenants is the tenant bound to the CONTEXT of each call, which the
	// engine guards StartJob on and which a request field cannot stand in for.
	// Recorded because a double that discarded the context is what let
	// go-quicbaas#918's sibling defect — a triage backstop refused `tenant ID
	// required` on every pass — pass a green suite.
	ctxTenants []string
}

func (f *fakeJobStarter) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctxTenants = append(f.ctxTenants, orchestration.TenantFromContext(ctx))
	if f.err != nil {
		return nil, f.err
	}
	f.seq++
	f.started = append(f.started, req)
	if f.noID {
		return &orchestration.StartJobResponse{}, nil
	}
	return &orchestration.StartJobResponse{JobID: "job-" + itoa(f.seq)}, nil
}

func (f *fakeJobStarter) requests() []*orchestration.StartJobRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*orchestration.StartJobRequest(nil), f.started...)
}

// contextTenants returns the tenant each call carried on its context.
func (f *fakeJobStarter) contextTenants() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ctxTenants...)
}

// dispatchRig is a dispatcher wired to a store, a job starter and a forge the
// test controls.
type dispatchRig struct {
	dispatcher *ReviewDispatcher
	store      *Store
	jobs       *fakeJobStarter
	forge      *fakeForge
}

// dispatchHead is the head every rig's pull request 41 sits on.
const dispatchHead = "1111111111111111111111111111111111111111"

// newDispatchRig builds a dispatcher over one open pull request (#41) whose
// project requires green checks — the configuration the gate exists for.
func newDispatchRig(t *testing.T) *dispatchRig {
	t.Helper()
	store := newTestStore(t)
	binding := testBinding()
	binding.Reviewer = ReviewerConfig{Backend: "codex", Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge()
	forge.prs[41] = &ForgePullRequest{
		Number: 41, State: "open", HeadRef: "issue/41-thing", BaseRef: "main", HeadSHA: dispatchHead,
	}
	jobs := &fakeJobStarter{}
	dispatcher, err := NewReviewDispatcher(store, jobs, &fakeForges{forge: forge})
	if err != nil {
		t.Fatal(err)
	}
	return &dispatchRig{dispatcher: dispatcher, store: store, jobs: jobs, forge: forge}
}

// setChecks replaces the runs the forge reports against sha.
func (r *dispatchRig) setChecks(sha string, runs ...ForgeCheckRun) {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	r.forge.checks[sha] = append([]ForgeCheckRun(nil), runs...)
}

// greenRun is one finished, successful check named name.
func greenRun(name, sha string) ForgeCheckRun {
	return ForgeCheckRun{ID: name, Name: name, HeadSHA: sha, Status: "completed", Conclusion: "success",
		Completed: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
}

// pendingRun is one check still running.
func pendingRun(name, sha string) ForgeCheckRun {
	return ForgeCheckRun{ID: name, Name: name, HeadSHA: sha, Status: "in_progress"}
}

// redRun is one finished, failed check.
func redRun(name, sha string) ForgeCheckRun {
	return ForgeCheckRun{ID: name, Name: name, HeadSHA: sha, Status: "completed", Conclusion: "failure",
		Completed: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)}
}

// deliveredTo is the binding a delivery is dispatched against. Only the
// project id and the discovery mode are read on that path, and the zero mode
// is DiscoveryBoth — the delivery path every binding had before the floor
// existed.
func deliveredTo(mode DiscoveryMode) *ProjectBinding {
	return &ProjectBinding{ProjectID: testProject, Discovery: mode}
}

// prRequest is what a pull-request delivery resolves to.
func prRequest(number int) ReviewRequest {
	return ReviewRequest{TenantID: testTenant, ProjectID: testProject, PullRequest: number}
}

// checkRequest is what a pipeline delivery resolves to: a commit and nothing
// else.
func checkRequest(head string) ReviewRequest {
	return ReviewRequest{TenantID: testTenant, ProjectID: testProject, HeadSHA: head}
}

// greenBinding drops the green-checks requirement from the rig's project.
func (r *dispatchRig) ungate(t *testing.T) {
	t.Helper()
	binding, err := r.store.GetBinding(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	binding.Review.RequireGreenChecks = false
	if err := r.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
}

func TestReviewDispatcher_StartsAReviewJobCarryingTheSubject(t *testing.T) {
	rig := newDispatchRig(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	started, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41))
	if err != nil || !started {
		t.Fatalf("TriggerReview: started=%v err=%v", started, err)
	}
	reqs := rig.jobs.requests()
	if len(reqs) != 1 {
		t.Fatalf("started %d jobs", len(reqs))
	}
	if reqs[0].Runner != ReviewJobRunnerName {
		t.Fatalf("runner = %q", reqs[0].Runner)
	}
	if reqs[0].ProjectID != testProject || reqs[0].TenantID != testTenant {
		t.Fatalf("request = %+v", reqs[0])
	}
	// THE SUBJECT TRAVELS TYPED, IN Params. Nothing parses the description.
	subject, err := ReviewJobSubjectFrom(&orchestration.JobConfig{Params: reqs[0].Params})
	if err != nil {
		t.Fatalf("the started job carries no readable subject: %v", err)
	}
	if subject.PullRequest != 41 {
		t.Fatalf("subject = %+v", subject)
	}
	// A review job is not an attempt at a task, and must not be recorded as
	// one: a TaskID here would spend a delivery's attempt budget on a review.
	if reqs[0].TaskID != "" || reqs[0].Attempt != 0 {
		t.Fatalf("the review job was bound to a task: %+v", reqs[0])
	}
}

func TestReviewDispatcher_StartsNothingForAProjectThatBindsNoReviewer(t *testing.T) {
	rig := newDispatchRig(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	binding, err := rig.store.GetBinding(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	binding.Reviewer = ReviewerConfig{}
	if err := rig.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	started, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41))
	if err != nil {
		t.Fatalf("TriggerReview: %v", err)
	}
	if started || len(rig.jobs.requests()) != 0 {
		t.Fatal("a job was started for a project that will never produce a verdict")
	}
}

// TestReviewDispatcher_GatesOnTheHeadsPipeline is go-quicbaas#927's first
// acceptance: a reviewer is spent only on a head CI has already passed.
func TestReviewDispatcher_GatesOnTheHeadsPipeline(t *testing.T) {
	for name, tc := range map[string]struct {
		runs []ForgeCheckRun
		want bool
	}{
		// A pull request that has just opened has no run against its head at
		// all. It is the state the old dispatcher reviewed in, and it is the
		// one that had the reviewer run the suite itself.
		"no check covers the head": {nil, false},
		"a check is still running": {[]ForgeCheckRun{pendingRun("ci", dispatchHead)}, false},
		"one of two is running": {[]ForgeCheckRun{
			greenRun("build", dispatchHead), pendingRun("test", dispatchHead),
		}, false},
		"a check finished red": {[]ForgeCheckRun{redRun("ci", dispatchHead)}, false},
		"one of two finished red": {[]ForgeCheckRun{
			greenRun("build", dispatchHead), redRun("test", dispatchHead),
		}, false},
		"every check finished green": {[]ForgeCheckRun{
			greenRun("build", dispatchHead), greenRun("test", dispatchHead),
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newDispatchRig(t)
			rig.setChecks(dispatchHead, tc.runs...)
			started, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41))
			if err != nil {
				t.Fatalf("TriggerReview: %v", err)
			}
			if started != tc.want {
				t.Fatalf("started = %v, want %v", started, tc.want)
			}
			if got := len(rig.jobs.requests()); (got > 0) != tc.want {
				t.Fatalf("%d jobs started, want started=%v", got, tc.want)
			}
		})
	}
}

// TestReviewDispatcher_GreenOnOneHeadReviewsOnlyThatHead is the other way the
// gate could be wrong: a repository has several changes in flight at once, and
// a pipeline finishing on one of them must not spend a reviewer on the others.
func TestReviewDispatcher_GreenOnOneHeadReviewsOnlyThatHead(t *testing.T) {
	rig := newDispatchRig(t)
	otherHead := "2222222222222222222222222222222222222222"
	rig.forge.prs[42] = &ForgePullRequest{
		Number: 42, State: "open", HeadRef: "issue/42-other", BaseRef: "main", HeadSHA: otherHead,
	}
	// #42's pipeline passed. #41's has not run at all.
	rig.setChecks(otherHead, greenRun("ci", otherHead))

	// The delivery that says #42 went green reviews #42.
	if started, err := rig.dispatcher.TriggerReview(context.Background(), checkRequest(otherHead)); err != nil || !started {
		t.Fatalf("the green head was not reviewed: started=%v err=%v", started, err)
	}
	// And #41, whose head has no run against it, is not reviewed by the same
	// delivery or by a delivery of its own.
	if started, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41)); err != nil || started {
		t.Fatalf("an untested head was reviewed: started=%v err=%v", started, err)
	}
	reqs := rig.jobs.requests()
	if len(reqs) != 1 {
		t.Fatalf("started %d jobs, want exactly the green head's", len(reqs))
	}
	subject, err := ReviewJobSubjectFrom(&orchestration.JobConfig{Params: reqs[0].Params})
	if err != nil || subject.PullRequest != 42 {
		t.Fatalf("the wrong pull request was reviewed: subject=%+v err=%v", subject, err)
	}
}

// TestReviewDispatcher_APipelineDeliveryFindsItsPullRequest is the other half:
// the event that says CI finished names a COMMIT, so the dispatcher has to find
// the pull request the commit heads or the green answer reaches nothing.
func TestReviewDispatcher_APipelineDeliveryFindsItsPullRequest(t *testing.T) {
	rig := newDispatchRig(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	started, err := rig.dispatcher.TriggerReview(context.Background(), checkRequest(dispatchHead))
	if err != nil || !started {
		t.Fatalf("TriggerReview: started=%v err=%v", started, err)
	}
	reqs := rig.jobs.requests()
	if len(reqs) != 1 {
		t.Fatalf("started %d jobs", len(reqs))
	}
	subject, err := ReviewJobSubjectFrom(&orchestration.JobConfig{Params: reqs[0].Params})
	if err != nil || subject.PullRequest != 41 {
		t.Fatalf("the commit did not resolve to its pull request: subject=%+v err=%v", subject, err)
	}
}

func TestReviewDispatcher_APipelineDeliveryForNoPullRequestStartsNothing(t *testing.T) {
	rig := newDispatchRig(t)
	// A push to a branch with no pull request open on it is most pushes, and
	// its CI going green must start nothing at all.
	orphan := "3333333333333333333333333333333333333333"
	rig.setChecks(orphan, greenRun("ci", orphan))
	started, err := rig.dispatcher.TriggerReview(context.Background(), checkRequest(orphan))
	if err != nil {
		t.Fatalf("TriggerReview: %v", err)
	}
	if started || len(rig.jobs.requests()) != 0 {
		t.Fatal("a review was started for a commit that heads no pull request")
	}
}

// TestReviewDispatcher_RedThenGreenReviewsOnce is the #927 proof that the gate
// does not merely delay the waste: the whole sequence a real head goes through
// — opened with nothing run, a failed pipeline, a re-run that passes — must end
// with exactly ONE review.
func TestReviewDispatcher_RedThenGreenReviewsOnce(t *testing.T) {
	rig := newDispatchRig(t)

	// 1. Opened. Nothing has run.
	if started, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41)); err != nil || started {
		t.Fatalf("opened: started=%v err=%v", started, err)
	}
	// 2. The pipeline starts.
	rig.setChecks(dispatchHead, pendingRun("ci", dispatchHead))
	if started, err := rig.dispatcher.TriggerReview(context.Background(), checkRequest(dispatchHead)); err != nil || started {
		t.Fatalf("pending: started=%v err=%v", started, err)
	}
	// 3. It fails.
	rig.setChecks(dispatchHead, redRun("ci", dispatchHead))
	if started, err := rig.dispatcher.TriggerReview(context.Background(), checkRequest(dispatchHead)); err != nil || started {
		t.Fatalf("red: started=%v err=%v", started, err)
	}
	// 4. It is re-run and passes.
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	if started, err := rig.dispatcher.TriggerReview(context.Background(), checkRequest(dispatchHead)); err != nil || !started {
		t.Fatalf("green: started=%v err=%v", started, err)
	}
	if got := rig.jobs.requests(); len(got) != 1 {
		t.Fatalf("the head was reviewed %d times; red-then-green must produce exactly one review", len(got))
	}
}

// TestReviewDispatcher_AProjectThatDoesNotRequireGreenChecksReadsNoForge keeps
// the gate from becoming a tax on the projects it does not concern.
func TestReviewDispatcher_AProjectThatDoesNotRequireGreenChecksReadsNoForge(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	// No check is published at all, and the forge is rigged to fail every
	// listing: a dispatcher that read one would be visible as an error.
	rig.forge.listChecksErr = errors.New("no check should be read for this project")
	rig.forge.listOpenPRsErr = errors.New("no pull request listing should be read for this project")
	started, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41))
	if err != nil || !started {
		t.Fatalf("TriggerReview: started=%v err=%v", started, err)
	}
	if len(rig.jobs.requests()) != 1 {
		t.Fatalf("started %d jobs", len(rig.jobs.requests()))
	}
}

// TestReviewDispatcher_AnUngatedProjectIgnoresAPipelineDelivery: the check-run
// delivery exists FOR the gate. A project whose policy does not gate on checks
// learns nothing from it, and acting on it anyway would turn every job of every
// run into a review job — dozens per head, each reading the forge to decide
// against a standing verdict it never needed to ask about. Its pull-request
// deliveries still dispatch exactly as they always did.
func TestReviewDispatcher_AnUngatedProjectIgnoresAPipelineDelivery(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	// Rigged to fail: reaching the forge at all on this path is the defect.
	rig.forge.listChecksErr = errors.New("no check should be read for this project")
	rig.forge.listOpenPRsErr = errors.New("no pull request listing should be read for this project")

	started, err := rig.dispatcher.TriggerReview(context.Background(), checkRequest(dispatchHead))
	if err != nil {
		t.Fatalf("TriggerReview: %v", err)
	}
	if started || len(rig.jobs.requests()) != 0 {
		t.Fatal("a pipeline delivery started a review for a project that does not gate on checks")
	}
}

func TestReviewDispatcher_Refusals(t *testing.T) {
	rig := newDispatchRig(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	ctx := context.Background()

	if _, err := rig.dispatcher.TriggerReview(ctx, prRequest(0)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a request naming neither pull request nor commit was accepted: %v", err)
	}
	if _, err := rig.dispatcher.TriggerReview(ctx, prRequest(-1)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a negative pull request was accepted: %v", err)
	}
	if _, err := rig.dispatcher.TriggerReview(ctx,
		ReviewRequest{ProjectID: testProject, PullRequest: 41}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a request naming no tenant was accepted: %v", err)
	}
	if _, err := rig.dispatcher.TriggerReview(ctx,
		ReviewRequest{TenantID: testTenant, PullRequest: 41}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a request naming no project was accepted: %v", err)
	}
	if _, err := rig.dispatcher.TriggerReview(ctx,
		ReviewRequest{TenantID: testTenant, ProjectID: "no-such-project", PullRequest: 41}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
	// A gated dispatch that cannot read the pipeline must not guess at it.
	rig.forge.listChecksErr = errors.New("forge is down")
	if _, err := rig.dispatcher.TriggerReview(ctx, prRequest(41)); err == nil {
		t.Fatal("a forge that could not answer about the checks was read as green")
	}
	rig.forge.listChecksErr = nil
	// Nor may a commit that cannot be resolved to a pull request be dropped
	// silently.
	rig.forge.listOpenPRsErr = errors.New("forge is down")
	if _, err := rig.dispatcher.TriggerReview(ctx, checkRequest(dispatchHead)); err == nil {
		t.Fatal("a forge that could not list pull requests was read as naming none")
	}
	rig.forge.listOpenPRsErr = nil

	rig.jobs.err = errors.New("engine is down")
	if _, err := rig.dispatcher.TriggerReview(ctx, prRequest(41)); !errors.Is(err, ErrBackend) {
		t.Fatalf("engine failure was not reported as a backend fault: %v", err)
	}
	rig.jobs.err = nil
	rig.jobs.noID = true
	started, err := rig.dispatcher.TriggerReview(ctx, prRequest(41))
	if err != nil || started {
		t.Fatalf("an engine answering with no job id reported started=%v err=%v", started, err)
	}
}

// TestReviewDispatcher_RefusesAPullRequestTheForgeDoesNotHave covers the
// adapter contract breach: neither a pull request nor an error.
func TestReviewDispatcher_RefusesAPullRequestTheForgeDoesNotHave(t *testing.T) {
	rig := newDispatchRig(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	rig.forge.getPRNil = true
	if _, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an adapter answering neither a pull request nor an error was dereferenced: %v", err)
	}
}

func TestReviewDispatcher_RefusesAForgeItCannotResolve(t *testing.T) {
	store := newTestStore(t)
	binding := testBinding()
	binding.Reviewer = ReviewerConfig{Backend: "codex", Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	// A resolver that registers no adapter for the binding's forge answers
	// nil, nil — a gated dispatch must refuse rather than proceed blind.
	dispatcher, err := NewReviewDispatcher(store, &fakeJobStarter{}, &fakeForges{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.TriggerReview(context.Background(), prRequest(41)); err == nil {
		t.Fatal("an unregistered forge was accepted")
	}
	failing, err := NewReviewDispatcher(store, &fakeJobStarter{}, &fakeForges{err: errors.New("no token")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.TriggerReview(context.Background(), prRequest(41)); err == nil {
		t.Fatal("a forge resolver failure was swallowed")
	}
}

func TestNewReviewDispatcher_RefusesIncompleteWiring(t *testing.T) {
	store := newTestStore(t)
	jobs := &fakeJobStarter{}
	forges := &fakeForges{forge: newFakeForge()}
	if _, err := NewReviewDispatcher(store, jobs, forges); err != nil {
		t.Fatal(err)
	}
	if _, err := NewReviewDispatcher(nil, jobs, forges); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil bindings accepted: %v", err)
	}
	if _, err := NewReviewDispatcher(store, nil, forges); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil jobs accepted: %v", err)
	}
	if _, err := NewReviewDispatcher(store, jobs, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil forges accepted: %v", err)
	}
}

func TestDispatchReview_OnlyPullRequestLifecycleEventsAskForAReview(t *testing.T) {
	for kind, want := range map[ForgeEventKind]bool{
		EventPullRequestOpened:  true,
		EventPullRequestUpdated: true,
		// THE DELIVERY THAT SAYS CI FINISHED. It used to be discarded here,
		// which left only the events that arrive before any test has run.
		EventCheckRunUpdated:   true,
		EventPullRequestMerged: false,
		EventPullRequestClosed: false,
		// A review landing is this module's own verdict coming back through
		// the hook; dispatching on it would have the reviewer answer itself.
		EventReviewSubmitted: false,
		EventIssueOpened:     false,
		EventUnsupported:     false,
	} {
		t.Run(string(kind), func(t *testing.T) {
			trigger := &fakeReviewTrigger{started: true}
			event := &ForgeEvent{Kind: kind, PullRequestNumber: 41, HeadSHA: dispatchHead}
			started, err := DispatchReview(context.Background(), trigger, testTenant, deliveredTo(""), event)
			if err != nil {
				t.Fatalf("DispatchReview: %v", err)
			}
			if started != want {
				t.Fatalf("started = %v, want %v", started, want)
			}
			if got := len(trigger.requests()); (got > 0) != want {
				t.Fatalf("trigger requests = %v", trigger.requests())
			}
		})
	}
}

// TestDispatchReview_CarriesTheHeadAPipelineEventNames proves the commit
// survives the ingestor's half: a check delivery names no pull request, so a
// dispatch that dropped the head would reach the port with nothing at all.
func TestDispatchReview_CarriesTheHeadAPipelineEventNames(t *testing.T) {
	trigger := &fakeReviewTrigger{started: true}
	event := &ForgeEvent{Kind: EventCheckRunUpdated, HeadSHA: dispatchHead}
	started, err := DispatchReview(context.Background(), trigger, testTenant, deliveredTo(""), event)
	if err != nil || !started {
		t.Fatalf("DispatchReview: started=%v err=%v", started, err)
	}
	got := trigger.requests()
	if len(got) != 1 {
		t.Fatalf("trigger asked %d times", len(got))
	}
	want := ReviewRequest{TenantID: testTenant, ProjectID: testProject, HeadSHA: dispatchHead}
	if got[0] != want {
		t.Fatalf("request = %+v, want %+v", got[0], want)
	}
}

func TestDispatchReview_TolerantOfWhatItCannotAct(t *testing.T) {
	trigger := &fakeReviewTrigger{started: true}
	for name, tc := range map[string]struct {
		trigger ReviewTrigger
		event   *ForgeEvent
	}{
		// A node that reviews nothing is a real configuration.
		"no trigger": {nil, &ForgeEvent{Kind: EventPullRequestOpened, PullRequestNumber: 41}},
		"no event":   {trigger, nil},
		// A pull-request event naming neither a pull request nor a commit is
		// valid traffic this build cannot act on; ReviewSweeper reaches the
		// same head from the forge, which is the floor under this path.
		"no number and no head": {trigger, &ForgeEvent{Kind: EventPullRequestOpened}},
		// A check delivery with no commit on it names nothing to look up.
		"check with no head": {trigger, &ForgeEvent{Kind: EventCheckRunUpdated}},
		"negative number":    {trigger, &ForgeEvent{Kind: EventPullRequestOpened, PullRequestNumber: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			started, err := DispatchReview(context.Background(), tc.trigger, testTenant, deliveredTo(""), tc.event)
			if err != nil || started {
				t.Fatalf("started=%v err=%v", started, err)
			}
		})
	}
	failing := &fakeReviewTrigger{err: errors.New("nope")}
	if _, err := DispatchReview(context.Background(), failing, testTenant, deliveredTo(""),
		&ForgeEvent{Kind: EventPullRequestOpened, PullRequestNumber: 41}); err == nil {
		t.Fatal("a failing trigger was swallowed")
	}
}

func TestReviewRequest_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		req  ReviewRequest
		want bool
	}{
		"a pull request":              {prRequest(41), true},
		"a commit":                    {checkRequest(dispatchHead), true},
		"both":                        {ReviewRequest{TenantID: testTenant, ProjectID: testProject, PullRequest: 41, HeadSHA: dispatchHead}, true},
		"neither":                     {ReviewRequest{TenantID: testTenant, ProjectID: testProject}, false},
		"a blank head is not a head":  {ReviewRequest{TenantID: testTenant, ProjectID: testProject, HeadSHA: "  "}, false},
		"no tenant":                   {ReviewRequest{ProjectID: testProject, PullRequest: 41}, false},
		"a blank tenant is no tenant": {ReviewRequest{TenantID: " ", ProjectID: testProject, PullRequest: 41}, false},
		"no project":                  {ReviewRequest{TenantID: testTenant, PullRequest: 41}, false},
		"negative":                    {ReviewRequest{TenantID: testTenant, ProjectID: testProject, PullRequest: -2}, false},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err == nil) != tc.want {
				t.Fatalf("Validate() = %v, want ok=%v", err, tc.want)
			}
			if err != nil && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("refusal is not an invalid-input error: %v", err)
			}
		})
	}
}

// TestReviewDispatcher_RefusesAForgeThatCannotReadThePullRequest keeps the
// gated path fail-closed on the read it now makes: the head the gate is
// evaluated against comes from the forge, so a forge that cannot answer must
// not be treated as a head with no checks.
func TestReviewDispatcher_RefusesAForgeThatCannotReadThePullRequest(t *testing.T) {
	rig := newDispatchRig(t)
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	// #99 does not exist, so the fake answers NotFound — the shape a real
	// adapter uses for a pull request that is gone.
	if _, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(99)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a pull request the forge does not have was accepted: %v", err)
	}
	if len(rig.jobs.requests()) != 0 {
		t.Fatal("a review was started for a pull request that could not be read")
	}
}

// TestReviewDispatcher_APullRequestWithNoHeadIsNotGreen covers the input a gate
// cannot decide: "no head" must not read as "nothing failed".
func TestReviewDispatcher_APullRequestWithNoHeadIsNotGreen(t *testing.T) {
	rig := newDispatchRig(t)
	rig.forge.prs[41].HeadSHA = ""
	// Checks exist in the repository, just not attributable to this change.
	rig.setChecks(dispatchHead, greenRun("ci", dispatchHead))
	started, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41))
	if err != nil {
		t.Fatalf("TriggerReview: %v", err)
	}
	if started || len(rig.jobs.requests()) != 0 {
		t.Fatal("a pull request with no recorded head was reviewed")
	}
}

// TestReviewDispatcher_RefusesAnUnnumberedCandidate is defence in depth at the
// last step: whatever a forge listing contains, a job is started only for a
// pull request the review runner could read a subject from.
func TestReviewDispatcher_RefusesAnUnnumberedCandidate(t *testing.T) {
	rig := newDispatchRig(t)
	orphanHead := "4444444444444444444444444444444444444444"
	// A listing entry with no number: a forge answering this is broken, and the
	// dispatcher must refuse rather than start a job whose subject is invalid.
	rig.forge.prs[0] = &ForgePullRequest{Number: 0, State: "open", HeadRef: "broken", BaseRef: "main", HeadSHA: orphanHead}
	rig.setChecks(orphanHead, greenRun("ci", orphanHead))
	if _, err := rig.dispatcher.TriggerReview(context.Background(), checkRequest(orphanHead)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an unnumbered pull request was dispatched: %v", err)
	}
	if len(rig.jobs.requests()) != 0 {
		t.Fatal("a job was started for a pull request with no number")
	}
}
