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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The merge train, driven end to end
// =============================================================================
//
// THE DECISIONS ARE NOT DOUBLED. Every test here runs the real runner, the
// real planner, the real store and the real gate definitions of green and red
// against the fake forge; only the git half and the review job are stood in
// for — by a builder that records what it was asked to merge, and by an engine
// that leaves the verdict a review job would leave on the forge. Both have
// their own tests (merge_train_build_test.go, review_runner_test.go), so what
// is proven here is what the train decides and what it writes.

// trainRig is one project's train and everything it reaches.
type trainRig struct {
	t       *testing.T
	store   *Store
	forge   *fakeForge
	forges  *fakeForges
	agents  *fakeAgents
	reviews *fakeTrainReviews
	runner  *TrainRunner
	builder *fakeTrainBuilder
	root    string
	queued  int

	// prompts is the set a job is launched with, as the envelope hands it
	// in; nil renders from the build.
	prompts *prompts.ResolvedSet

	auditMu sync.Mutex
	audits  []orchestration.JobAuditEvent
}

// fakeTrainReviews stands in for the engine starting the review job the train
// asks for, and for what that job leaves behind: a verdict by the train's
// reviewer on the head it read. It writes the verdict straight into the
// forge's review record rather than through SubmitReview, so every
// submission and status the forge records is one the TRAIN made — which is
// how a test proves the train makes none.
type fakeTrainReviews struct {
	fakeJobStarter
	forge *fakeForge
	store *Store

	mu sync.Mutex

	// decide is the verdict the review reaches on the train it is asked
	// about, and its findings. An empty verdict is a review still running.
	decide func(t *MergeTrain) (ReviewVerdict, string)

	// leaseTaken refuses this many starts as a review already running, and
	// refused counts the starts it refused.
	leaseTaken int
	refused    int

	// verdictless makes this many accepted jobs end with no verdict at all,
	// before decide is consulted for any.
	verdictless int

	// states is each started job's state: completed once it has posted a
	// verdict or ended without one, executing while it is still reading.
	// statusErr is what GetJobStatus answers instead.
	states    map[string]orchestration.JobState
	statusErr error

	// unusable answers this many availability asks with no usable reviewer
	// account, giving holdReason, before answering usable.
	unusable   int
	holdReason string
}

// AgentAccountAvailability makes the fake the engine's availability verb too,
// as orchestration.AgentService is.
func (f *fakeTrainReviews) AgentAccountAvailability(context.Context, string, string, orchestration.AgentRef, orchestration.AgentRole) (orchestration.AccountAvailability, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unusable > 0 {
		f.unusable--
		return orchestration.AccountAvailability{Reason: f.holdReason}, nil
	}
	return orchestration.AccountAvailability{Usable: true}, nil
}

func (f *fakeTrainReviews) GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	state, ok := f.states[jobID]
	if !ok {
		return nil, orchestration.ErrJobNotFound
	}
	return &orchestration.JobStatus{JobID: jobID, TenantID: tenantID, State: state}, nil
}

// setState moves one started job to state.
func (f *fakeTrainReviews) setState(jobID string, state orchestration.JobState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[jobID] = state
}

func (f *fakeTrainReviews) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	f.mu.Lock()
	if f.leaseTaken > 0 {
		f.leaseTaken--
		f.refused++
		f.mu.Unlock()
		return nil, orchestration.ErrJobLeaseTaken
	}
	decide := f.decide
	verdictless := f.verdictless > 0
	if verdictless {
		f.verdictless--
	}
	f.mu.Unlock()
	resp, err := f.fakeJobStarter.StartJob(ctx, req)
	if err != nil {
		return resp, err
	}
	if verdictless || decide == nil {
		f.setState(resp.JobID, orchestration.JobStateCompleted)
		return resp, nil
	}
	ledger, err := f.store.GetTrainLedger(ctx, testTenant, testProject)
	if err != nil {
		return nil, err
	}
	verdict, findings := decide(ledger.Active)
	if verdict == "" {
		f.setState(resp.JobID, orchestration.JobStateExecuting)
		return resp, nil
	}
	f.setState(resp.JobID, orchestration.JobStateCompleted)
	number, err := strconv.Atoi(req.Params[ParamPullRequest])
	if err != nil {
		return nil, err
	}
	f.forge.mu.Lock()
	defer f.forge.mu.Unlock()
	f.forge.reviews[number] = append(f.forge.reviews[number], ForgeReview{
		ID: int64(1000 + len(f.forge.reviews[number])), Reviewer: fakeReviewerLogin, State: giteaReviewStates[verdict],
		Body: findings, HeadSHA: f.forge.prs[number].HeadSHA, Submitted: time.Now().UTC(),
	})
	return resp, nil
}

// approveEvery is a reviewer that approves every train it is asked about.
func approveEvery(*MergeTrain) (ReviewVerdict, string) {
	return VerdictApprove, "The changes compose cleanly."
}

// fakeTrainBuilder stands in for the container's git. Each change merges to a
// commit named for its train and pull request, so a test knows in advance the
// head of every prefix and can script that head's pipeline.
type fakeTrainBuilder struct {
	mu        sync.Mutex
	forge     *fakeForge
	train     int
	head      string
	conflicts map[int]bool
	pushErr   error
	added     []int
	pushed    []string
}

// Base is the target's tip as the forge holds it when the build starts,
// which is what a fresh checkout of it would be.
func (b *fakeTrainBuilder) Base() (string, error) {
	branch, err := b.forge.GetBranch(context.Background(), testRepoRef, "main")
	if err != nil {
		return "", err
	}
	return branch.SHA, nil
}

func (b *fakeTrainBuilder) Add(car TrainCar) (string, *TrainEjection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.added = append(b.added, car.PullRequest)
	if b.conflicts[car.PullRequest] {
		return "", &TrainEjection{Reason: TrainEjectConflict, Detail: "Conflicted: `pkg/widget.go`."}, nil
	}
	b.head = trainCommit(b.train, car.PullRequest)
	return b.head, nil, nil
}

func (b *fakeTrainBuilder) Push(branch string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pushErr != nil {
		return "", b.pushErr
	}
	b.pushed = append(b.pushed, branch)
	b.forge.mu.Lock()
	if b.forge.branches == nil {
		b.forge.branches = map[string]string{}
	}
	b.forge.branches[branch] = b.head
	b.forge.mu.Unlock()
	return b.head, nil
}

func (b *fakeTrainBuilder) Evidence() string { return "fake build" }

// trainCommit is the commit a fake build makes merging pr onto train number.
func trainCommit(train, pr int) string {
	return "train" + strconv.Itoa(train) + "-pr" + strconv.Itoa(pr)
}

func newTrainRig(t *testing.T) *trainRig {
	t.Helper()
	store := newTestStore(t)
	binding := testBinding()
	binding.Reviewer = trainTestReviewer()
	binding.Forge.Labels[LabelTrainEjected] = "factory:train-ejected"
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge()
	forge.onlyKnownBranches = true
	// The train's own pull requests are numbered well clear of the changes
	// a test queues, as they would be on a forge with history.
	forge.nextPR = 900
	forges := &fakeForges{forge: forge}
	root := testRoot(t)
	container := &fakeContainer{checkout: filepath.Join(orchestration.WorkspaceCheckoutRoot, testOrchProject().Name)}
	agents := &fakeAgents{container: container, host: executingHost{root: root}}
	reviews := &fakeTrainReviews{forge: forge, store: store, decide: approveEvery, states: map[string]orchestration.JobState{}}
	runner, err := NewTrainRunner(TrainRunnerConfig{
		Bindings: store, Forges: forges, Trains: store, Jobs: reviews,
		Tokens: &fakeTokens{token: []byte("t0ken")}, Signing: &fakeSigningAgent{},
		CheckoutRoot: orchestration.WorkspaceCheckoutRoot, Guard: testRepoGuard(t), SocketsRoot: testSocketsRoot(t),
		PollInterval: time.Millisecond, Waiter: &immediateWaiter{}, MergeAttempts: 2,
	})
	if err != nil {
		t.Fatalf("NewTrainRunner: %v", err)
	}
	builder := &fakeTrainBuilder{forge: forge, conflicts: map[int]bool{}}
	runner.newBuilder = func(_ context.Context, run *trainRun) trainBuilder {
		builder.mu.Lock()
		builder.train = run.train().Number
		builder.mu.Unlock()
		return builder
	}
	return &trainRig{
		t: t, store: store, forge: forge, forges: forges, agents: agents, reviews: reviews, runner: runner,
		builder: builder, root: root,
	}
}

// queue opens pull request pr for task, approved at its head, and queues it.
func (r *trainRig) queue(pr int, task string) {
	r.t.Helper()
	head := "head-" + strconv.Itoa(pr)
	r.forge.mu.Lock()
	r.forge.prs[pr] = &ForgePullRequest{Number: pr, Title: "change " + strconv.Itoa(pr), State: "open",
		HeadRef: "issue/" + strconv.Itoa(pr), HeadSHA: head, BaseRef: "main", HeadRepo: testRepoRef, Mergeable: true}
	r.forge.checks[head] = []ForgeCheckRun{{ID: "r" + strconv.Itoa(pr), Name: DefaultReviewStatusContext, HeadSHA: head,
		Status: "completed", Conclusion: "success"}}
	r.forge.mu.Unlock()
	r.queued++
	entry := &TrainEntry{
		ProjectID: testProject, TaskID: task, PullRequest: pr, Title: "change " + strconv.Itoa(pr),
		Branch: "issue/" + strconv.Itoa(pr), Target: "main", HeadSHA: head, Closes: []int{pr - 100},
		State: TrainEntryQueued, EnqueuedAt: time.Date(2026, 9, 24, 12, r.queued, 0, 0, time.UTC),
	}
	if err := r.store.SaveTrainEntry(context.Background(), testTenant, entry); err != nil {
		r.t.Fatalf("queue #%d: %v", pr, err)
	}
}

// pipeline scripts what CI answers for one commit.
func (r *trainRig) pipeline(sha string, conclusion, log string) {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	r.forge.checks[sha] = []ForgeCheckRun{{ID: "ci-" + sha, Name: "ci", HeadSHA: sha, Status: "completed",
		Conclusion: conclusion, Completed: time.Now().Add(-time.Hour)}}
	if log != "" {
		r.forge.logs["ci"] = log
	}
}

// genuineRed is a log the classifier reads as the change's own failure.
const genuineRed = "--- FAIL: TestWidget (0.01s)\n    widget_test.go:12: got 1, want 2\nFAIL\nmake: *** [test] Error 1\n"

// job builds a train job entered at start.
func (r *trainRig) job(ctx context.Context, start orchestration.JobState) *orchestration.JobRun {
	r.t.Helper()
	base, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	if err != nil {
		r.t.Fatal(err)
	}
	project := testOrchProject()
	project.Workspace = orchestration.ProjectWorkspace{Path: r.root}
	return orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:      "job-train-1",
		Runner:     TrainRunnerName,
		Config:     &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject},
		Project:    project,
		StartState: start,
		Prompts:    r.prompts,
		Agents:     r.agents,
		Logger:     logger.NewAdapter(base),
		AuditFn: func(eventType, details string) {
			r.auditMu.Lock()
			r.audits = append(r.audits, orchestration.JobAuditEvent{Type: eventType, Details: details})
			r.auditMu.Unlock()
		},
	})
}

// run drives one train job to its end.
func (r *trainRig) run(start orchestration.JobState) error {
	r.t.Helper()
	ctx := context.Background()
	return r.runner.Run(ctx, r.job(ctx, start))
}

// entry reads one task's queue entry.
func (r *trainRig) entry(task string) *TrainEntry {
	r.t.Helper()
	entry, err := r.store.GetTrainEntry(context.Background(), testTenant, testProject, task)
	if err != nil {
		r.t.Fatalf("entry %s: %v", task, err)
	}
	return entry
}

// ledger reads the project's train ledger.
func (r *trainRig) ledger() *TrainLedger {
	r.t.Helper()
	ledger, err := r.store.GetTrainLedger(context.Background(), testTenant, testProject)
	if err != nil {
		r.t.Fatal(err)
	}
	return ledger
}

// audited is the details of every audit event of one type.
func (r *trainRig) audited(eventType string) []string {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	var out []string
	for _, event := range r.audits {
		if event.Type == eventType {
			out = append(out, event.Details)
		}
	}
	return out
}

// trainPRs are the pull requests the train opened, by head branch.
func (r *trainRig) trainPRs() map[string]*ForgePullRequest {
	r.forge.mu.Lock()
	defer r.forge.mu.Unlock()
	out := map[string]*ForgePullRequest{}
	for _, pr := range r.forge.prs {
		if IsTrainBranch(pr.HeadRef) {
			copied := *pr
			out[pr.HeadRef] = &copied
		}
	}
	return out
}

// =============================================================================
// Green
// =============================================================================

// TestTrain_AGreenTrainLandsEveryChangeThroughOneMerge is the case the train
// exists for: two approved changes, one pipeline, one merge — and the forge's
// ordinary merge, so every protection rule is enforced on what lands.
func TestTrain_AGreenTrainLandsEveryChangeThroughOneMerge(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.queue(102, "task-b")
	head := trainCommit(1, 102)
	rig.pipeline(head, "success", "")

	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := rig.builder.added; len(got) != 2 || got[0] != 101 || got[1] != 102 {
		t.Fatalf("merged onto the train in order %v, want [101 102]", got)
	}
	train := rig.trainPRs()["queue/1"]
	if train == nil {
		t.Fatalf("no train pull request opened; train PRs = %v", rig.trainPRs())
	}
	if !train.Merged || train.HeadSHA != head {
		t.Fatalf("train PR = %+v, want merged at %s", train, head)
	}
	merges := rig.forge.mergeCalls()
	if len(merges) != 1 || merges[0].number != train.Number || merges[0].head != head {
		t.Fatalf("merges = %+v, want one merge of #%d bound to %s", merges, train.Number, head)
	}
	if merges[0].spec.Method != MergeMethodSquash {
		t.Fatalf("the train landed by %q, not the project's strategy", merges[0].spec.Method)
	}
	for _, want := range []string{"Closes #1", "Closes #2", "(#101)", "(#102)"} {
		if !strings.Contains(merges[0].spec.CommitMessage, want) {
			t.Fatalf("merge message lacks %q:\n%s", want, merges[0].spec.CommitMessage)
		}
	}
	for _, task := range []string{"task-a", "task-b"} {
		entry := rig.entry(task)
		if entry.State != TrainEntryLanded || entry.MergeSHA != train.MergeSHA || entry.TrainPullRequest != train.Number {
			t.Fatalf("%s = %+v, want landed in %s by #%d", task, entry, train.MergeSHA, train.Number)
		}
	}
	if rig.ledger().Active != nil {
		t.Fatal("the landed train is still on the ledger")
	}
	if rig.ledger().Next != 2 {
		t.Fatalf("next train number = %d, want 2", rig.ledger().Next)
	}
}

// TestTrain_TheTrainIsReviewedByItsReviewerAndNeverAttests is the owner's
// ruling: the train's pull request lands on a genuine verdict, asked of the
// project's counting reviewer through the ordinary review job, on exactly the
// head that lands — and the train itself writes no review and no status.
func TestTrain_TheTrainIsReviewedByItsReviewerAndNeverAttests(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.queue(102, "task-b")
	head := trainCommit(1, 102)
	rig.pipeline(head, "success", "")
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}

	train := rig.trainPRs()["queue/1"]
	requests := rig.reviews.requests()
	if len(requests) != 1 {
		t.Fatalf("review jobs = %+v, want exactly one", requests)
	}
	req := requests[0]
	if req.Runner != ReviewJobRunnerName || req.ProjectID != testProject || req.Lead.AgentID != testReviewerAgent ||
		req.Params[ParamPullRequest] != strconv.Itoa(train.Number) || req.Params[ParamReviewerProfile] != DefaultReviewerProfileName {
		t.Fatalf("review job = %+v, want the default profile reviewing #%d", req, train.Number)
	}
	if tenants := rig.reviews.contextTenants(); len(tenants) != 1 || tenants[0] != testTenant {
		t.Fatalf("the review was started with context tenants %v; the engine guards StartJob on it", tenants)
	}
	if submissions := rig.forge.submissions(); len(submissions) != 0 {
		t.Fatalf("the train posted its own review: %+v", submissions)
	}
	if statuses, _ := rig.forge.publishedStatuses(); len(statuses) != 0 {
		t.Fatalf("the train posted its own status: %+v", statuses)
	}
	if !train.Merged || train.HeadSHA != head {
		t.Fatalf("train PR = %+v, want merged at the reviewed head %s", train, head)
	}
	if audits := rig.audited(auditTrainReview); len(audits) != 1 || !strings.Contains(audits[0], "head="+head) {
		t.Fatalf("review requests audited = %v", audits)
	}
}

// TestTrain_TheTrainWaitsForTheVerdictAndAsksOnce proves the train does not
// land while its reviewer is still reading, and does not ask again on every
// pass while it waits.
func TestTrain_TheTrainWaitsForTheVerdictAndAsksOnce(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	head := trainCommit(1, 101)
	rig.pipeline(head, "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) { return "", "" }
	passes := 0
	rig.forge.onListReviews = func(number int) {
		passes++
		if passes == 4 {
			if merged := len(rig.forge.merges); merged != 0 {
				t.Errorf("the train merged before its reviewer answered: %d merge(s)", merged)
			}
			rig.forge.reviews[number] = append(rig.forge.reviews[number], ForgeReview{
				ID: 7, Reviewer: fakeReviewerLogin, State: giteaReviewStates[VerdictApprove], Body: "fine", HeadSHA: head,
			})
		}
	}
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if passes < 4 {
		t.Fatalf("the train read the verdict %d time(s); it did not wait", passes)
	}
	if requests := rig.reviews.requests(); len(requests) != 1 {
		t.Fatalf("review jobs = %d, want one across every pass of the wait", len(requests))
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed once approved", got)
	}
}

// TestTrain_AVerdictOnAnotherHeadIsNotTheTrains proves the verdict binds to
// the head that lands: an approval of some other commit on the same pull
// request is not read as one.
func TestTrain_AVerdictOnAnotherHeadIsNotTheTrains(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	head := trainCommit(1, 101)
	rig.pipeline(head, "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) { return "", "" }
	ctx, cancel := context.WithCancel(context.Background())
	passes := 0
	rig.forge.onListReviews = func(number int) {
		passes++
		rig.forge.reviews[number] = []ForgeReview{{ID: 1, Reviewer: fakeReviewerLogin,
			State: giteaReviewStates[VerdictApprove], Body: "fine", HeadSHA: "some-other-head"}}
		if passes == 3 {
			cancel()
		}
	}
	if err := rig.runner.Run(ctx, rig.job(ctx, orchestration.JobStateInitializing)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want still waiting when cancelled", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a verdict on another head landed the train: %+v", merges)
	}
}

// TestTrain_AReviewAlreadyRunningIsWaitedOnNotFailed proves a start refused
// because a review of the pull request already holds its lease is neither a
// failure nor recorded as the train's own request: the next pass asks again.
func TestTrain_AReviewAlreadyRunningIsWaitedOnNotFailed(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.leaseTaken = 2
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rig.reviews.refused != 2 || len(rig.reviews.requests()) != 1 {
		t.Fatalf("starts refused = %d, accepted = %d; want two refused then one accepted",
			rig.reviews.refused, len(rig.reviews.requests()))
	}
	if audits := rig.audited(auditTrainReview); len(audits) != 1 {
		t.Fatalf("review requests audited = %v, want only the accepted one", audits)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed", got)
	}
}

// TestTrain_AReviewWithNoUsableAccountHoldsAndEjectsNoOne: while the train's
// reviewer has no usable account the train starts no review job, says why
// once (audit and the pull request's review status), ejects nobody, and
// reviews and lands once an account is usable.
func TestTrain_AReviewWithNoUsableAccountHoldsAndEjectsNoOne(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.unusable = 3
	rig.reviews.holdReason = "no usable codex account: re-login needed on 1"
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if requests := rig.reviews.requests(); len(requests) != 1 {
		t.Fatalf("review jobs = %d, want none while held and one once usable", len(requests))
	}
	held := rig.audited(auditTrainReviewHeld)
	if len(held) != 1 || !strings.Contains(held[0], "re-login needed") {
		t.Fatalf("holds audited = %v, want one carrying the reason", held)
	}
	posted := 0
	for _, status := range rig.forge.statuses {
		if status.Status == CheckPending && status.Description == rig.reviews.holdReason {
			posted++
		}
	}
	if posted != 1 {
		t.Fatalf("hold statuses = %+v, want the reason posted once", rig.forge.statuses)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed once the account was usable, never ejected", got)
	}
}

// TestTrain_AReviewThatCannotBeStartedFailsTheJob proves an engine that
// refuses the review for any other reason is a failure an operator sees, and
// the train stays on the ledger, still waiting, for the next job.
func TestTrain_AReviewThatCannotBeStartedFailsTheJob(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.err = errors.New("engine unavailable")
	err := rig.run(orchestration.JobStateInitializing)
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("Run = %v, want a backend failure", err)
	}
	if ledger := rig.ledger(); ledger.Active == nil || ledger.Active.Phase != TrainReviewing ||
		!ledger.Active.Current().ReviewRequestedAt.IsZero() {
		t.Fatalf("ledger = %+v, want the train still waiting with no review recorded", ledger.Active)
	}
}

// TestTrain_TheQueueIsBoundedByThePolicy proves a train carries at most
// max_size changes and the rest ride the next train in the same job.
func TestTrain_TheQueueIsBoundedByThePolicy(t *testing.T) {
	rig := newTrainRig(t)
	if err := rig.store.PutBinding(context.Background(), testTenant, func() *ProjectBinding {
		b := testBinding()
		b.Reviewer = trainTestReviewer()
		b.MergeTrain.MaxSize = 2
		return b
	}()); err != nil {
		t.Fatal(err)
	}
	rig.queue(101, "task-a")
	rig.queue(102, "task-b")
	rig.queue(103, "task-c")
	rig.pipeline(trainCommit(1, 102), "success", "")
	rig.pipeline(trainCommit(2, 103), "success", "")

	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.forge.mergeCalls()) != 2 {
		t.Fatalf("merges = %+v, want two trains", rig.forge.mergeCalls())
	}
	if got := rig.entry("task-c"); got.State != TrainEntryLanded || got.Train != 2 {
		t.Fatalf("the change past the bound = %+v, want landed by train 2", got)
	}
	if got := rig.entry("task-a"); got.Train != 1 {
		t.Fatalf("the first change rode train %d, want 1", got.Train)
	}
}

// TestTrain_AnEmptyQueueStartsNothing proves the cheap path: no container, no
// train number spent.
func TestTrain_AnEmptyQueueStartsNothing(t *testing.T) {
	rig := newTrainRig(t)
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.agents.spawns) != 0 {
		t.Fatalf("an empty queue spawned %d session(s)", len(rig.agents.spawns))
	}
	if rig.ledger().Version != 0 {
		t.Fatal("an empty queue wrote the ledger")
	}
}

// =============================================================================
// A review that ends with no verdict
// =============================================================================

// TestTrain_AReviewThatEndsWithNoVerdictIsAskedAgain proves a train does not
// wait for ever on a review job that finished without deciding: it asks again
// through the same start path, records the re-ask, and lands on the verdict
// the second job gives.
func TestTrain_AReviewThatEndsWithNoVerdictIsAskedAgain(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.verdictless = 1
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requests := rig.reviews.requests()
	if len(requests) != 2 || requests[0].Params[ParamPullRequest] != requests[1].Params[ParamPullRequest] ||
		requests[1].Runner != ReviewJobRunnerName {
		t.Fatalf("review jobs = %+v, want the same review asked twice", requests)
	}
	reasked := rig.audited(auditTrainReasked)
	if len(reasked) != 1 || !strings.Contains(reasked[0], "ask=2") || !strings.Contains(reasked[0], "previous_job=job-1") ||
		!strings.Contains(reasked[0], "previous_state=completed") {
		t.Fatalf("re-asks audited = %v", reasked)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed on the second review's verdict", got)
	}
}

// TestTrain_AReviewJobTheEngineNoLongerKnowsHasEnded proves a recorded job
// that has vanished is not waited on: nothing will ever post its verdict.
func TestTrain_AReviewJobTheEngineNoLongerKnowsHasEnded(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) { return "", "" }
	rig.forge.onListReviews = func(int) {
		if len(rig.reviews.requests()) == 1 {
			rig.reviews.mu.Lock()
			delete(rig.reviews.states, "job-1")
			rig.reviews.decide = approveEvery
			rig.reviews.mu.Unlock()
		}
	}
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reasked := rig.audited(auditTrainReasked); len(reasked) != 1 || !strings.Contains(reasked[0], "previous_state=not_found") {
		t.Fatalf("re-asks audited = %v, want one after the job vanished", reasked)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed", got)
	}
}

// TestTrain_AReviewJobThatCannotBeReadFailsTheJob proves the train does not
// guess whether its review has ended: an unreadable job fails the train job,
// and the train stays on the ledger, still waiting, for the next.
func TestTrain_AReviewJobThatCannotBeReadFailsTheJob(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) { return "", "" }
	rig.reviews.statusErr = errors.New("engine unavailable")
	if err := rig.run(orchestration.JobStateInitializing); !errors.Is(err, ErrBackend) {
		t.Fatalf("Run = %v, want a backend failure", err)
	}
	if ledger := rig.ledger(); ledger.Active == nil || ledger.Active.Phase != TrainReviewing ||
		ledger.Active.Current().ReviewJobID != "job-1" {
		t.Fatalf("ledger = %+v, want still waiting on job-1", ledger.Active)
	}
}

// TestTrain_AReviewThatNeverDecidesAbandonsTheTrainAndEjectsNoOne is the
// bound: after MaxTrainReviewAsks review jobs of one head end with no verdict,
// the train is abandoned. No change is ejected or bounded — nothing was found
// wrong — every change is told why, and each rides the next train on its own
// standing approval.
func TestTrain_AReviewThatNeverDecidesAbandonsTheTrainAndEjectsNoOne(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.queue(102, "task-b")
	rig.pipeline(trainCommit(1, 102), "success", "")
	rig.pipeline(trainCommit(2, 102), "success", "")
	rig.reviews.verdictless = MaxTrainReviewAsks
	var abandoned []*TrainEntry
	rig.forge.onCreatePullRequest = func(pr *ForgePullRequest) {
		if pr.HeadRef != TrainBranch(2) {
			return
		}
		for _, task := range []string{"task-a", "task-b"} {
			entry, err := rig.store.GetTrainEntry(context.Background(), testTenant, testProject, task)
			if err != nil {
				t.Errorf("entry %s: %v", task, err)
				return
			}
			abandoned = append(abandoned, entry)
		}
	}
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(abandoned) != 2 {
		t.Fatal("train 2 never formed after train 1 was abandoned")
	}
	// Read as train 2 opens: each change went back to the queue unbounded and
	// not ejected, and has boarded the next train on its standing approval.
	for _, entry := range abandoned {
		if entry.State != TrainEntryRiding || entry.Train != 2 || entry.MaxTrain != 0 || entry.Ejection != nil {
			t.Fatalf("after the abandonment %s = %+v, want riding train 2, unbounded and not ejected", entry.TaskID, entry)
		}
	}
	exhausted := rig.audited(auditTrainNoVerdict)
	if len(exhausted) != 1 || !strings.Contains(exhausted[0], "asks=3") {
		t.Fatalf("exhaustions audited = %v", exhausted)
	}
	if reasked := rig.audited(auditTrainReasked); len(reasked) != MaxTrainReviewAsks-1 {
		t.Fatalf("re-asks audited = %v, want %d", reasked, MaxTrainReviewAsks-1)
	}
	if requests := rig.reviews.requests(); len(requests) != MaxTrainReviewAsks+1 {
		t.Fatalf("review jobs = %d, want %d for train 1 and one for train 2", len(requests), MaxTrainReviewAsks+1)
	}
	if ejected := rig.audited(auditTrainEjectedPR); len(ejected) != 0 {
		t.Fatalf("a review with no verdict ejected a change: %v", ejected)
	}
	train1 := rig.trainPRs()["queue/1"]
	if train1 == nil || train1.Merged || train1.State != "closed" {
		t.Fatalf("the abandoned train's pull request = %+v, want closed unmerged", train1)
	}
	for _, pr := range []int{101, 102} {
		comments := rig.forge.commentsOn(pr)
		if len(comments) != 1 || !strings.Contains(comments[0].Body, "never produced a verdict") ||
			!strings.Contains(comments[0].Body, "#"+strconv.Itoa(train1.Number)) ||
			!strings.Contains(comments[0].Body, "still stands") {
			t.Fatalf("#%d comments = %+v, want one saying the review never decided", pr, comments)
		}
	}
	for _, task := range []string{"task-a", "task-b"} {
		if got := rig.entry(task); got.State != TrainEntryLanded || got.Train != 2 {
			t.Fatalf("%s = %+v, want landed by train 2", task, got)
		}
	}
}

// TestTrain_ARestartDuringAReaskResumesTheSecondReview proves the re-ask is
// on the ledger before anything waits on it: a train interrupted while its
// second review runs resumes waiting on that review, asking no third.
func TestTrain_ARestartDuringAReaskResumesTheSecondReview(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	head := trainCommit(1, 101)
	rig.pipeline(head, "success", "")
	rig.reviews.verdictless = 1
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) { return "", "" }
	ctx, cancel := context.WithCancel(context.Background())
	rig.forge.onListReviews = func(int) {
		if len(rig.reviews.requests()) == 2 {
			cancel()
		}
	}
	if err := rig.runner.Run(ctx, rig.job(ctx, orchestration.JobStateInitializing)); !errors.Is(err, context.Canceled) {
		t.Fatalf("first run = %v, want cancelled while the second review runs", err)
	}
	probe := rig.ledger().Active.Current()
	if probe.ReviewAsks != 2 || probe.ReviewJobID != "job-2" {
		t.Fatalf("interrupted probe = %+v, want the re-ask recorded as job-2, ask 2", probe)
	}

	// The second review finishes while nothing is running.
	rig.forge.onListReviews = nil
	rig.forge.mu.Lock()
	rig.forge.reviews[probe.PullRequest] = append(rig.forge.reviews[probe.PullRequest], ForgeReview{
		ID: 9, Reviewer: fakeReviewerLogin, State: giteaReviewStates[VerdictApprove], Body: "fine", HeadSHA: head,
	})
	rig.forge.mu.Unlock()
	rig.reviews.setState("job-2", orchestration.JobStateCompleted)
	if err := rig.run(TrainResumeState(orchestration.JobStateValidating)); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if requests := rig.reviews.requests(); len(requests) != 2 {
		t.Fatalf("review jobs = %d, want the two asked before the restart", len(requests))
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed on the second review's verdict", got)
	}
}

// =============================================================================
// Refused by the reviewer
// =============================================================================

// TestTrain_ARefusedTrainIsAbandonedAndReformsSmaller is the default the
// owner ruled: a reviewer who requests changes on the combination does not
// eject anyone. Every change keeps its own approval, is told what was found
// and where, and goes back to the queue — and the next train is half the size,
// so the combination is judged again in parts. Here the reviewer refuses any
// train of more than one change, so the three changes end up landing one by
// one, each on its own verdict.
func TestTrain_ARefusedTrainIsAbandonedAndReformsSmaller(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.queue(102, "task-b")
	rig.queue(103, "task-c")
	for train, prs := range map[int][]int{1: {103}, 2: {102}, 3: {101}, 4: {102}, 5: {103}} {
		for _, pr := range prs {
			rig.pipeline(trainCommit(train, pr), "success", "")
		}
	}
	var refusedFirst []*TrainEntry
	rig.reviews.decide = func(train *MergeTrain) (ReviewVerdict, string) {
		if train.Land > 1 {
			return VerdictRequestChanges, "The cache added by one change is never invalidated by the other."
		}
		return VerdictApprove, "Fine on its own."
	}
	rig.forge.onCreatePullRequest = func(pr *ForgePullRequest) {
		// The moment train 2 opens, train 1's refusal has been written.
		if pr.HeadRef == TrainBranch(2) && refusedFirst == nil {
			for _, task := range []string{"task-a", "task-b", "task-c"} {
				entry, err := rig.store.GetTrainEntry(context.Background(), testTenant, testProject, task)
				if err != nil {
					t.Errorf("entry %s: %v", task, err)
					return
				}
				refusedFirst = append(refusedFirst, entry)
			}
		}
	}

	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(refusedFirst) != 3 {
		t.Fatalf("train 2 never formed after train 1 was refused")
	}
	for _, entry := range refusedFirst {
		if entry.MaxTrain != 2 || entry.Ejection != nil {
			t.Fatalf("after the refusal %s = %+v, want back in the queue bounded to 2 and not ejected", entry.TaskID, entry)
		}
	}
	for _, task := range []string{"task-a", "task-b", "task-c"} {
		if got := rig.entry(task); got.State != TrainEntryLanded {
			t.Fatalf("%s = %+v, want landed on a verdict of its own", task, got)
		}
	}
	merges := rig.forge.mergeCalls()
	if len(merges) != 3 {
		t.Fatalf("merges = %+v, want three single-change trains", merges)
	}
	if refused := rig.audited(auditTrainRefused); len(refused) != 2 {
		t.Fatalf("refusals = %v, want train 1 (three changes) and train 2 (two)", refused)
	}
	if ejected := rig.audited(auditTrainEjectedPR); len(ejected) != 0 {
		t.Fatalf("a refused combination ejected a change: %v", ejected)
	}
	train1 := rig.trainPRs()["queue/1"]
	if train1 == nil || train1.State != "closed" || train1.Merged {
		t.Fatalf("the refused train's pull request = %+v, want closed unmerged", train1)
	}
	comments := rig.forge.commentsOn(101)
	if len(comments) != 2 {
		t.Fatalf("#101 comments = %+v, want one per refused train it rode", comments)
	}
	if !rig.forge.discussedOnlyAsPullRequest(101) {
		t.Fatal("the refusal comment went through the issue discussion, which on GitLab is another object")
	}
	for _, want := range []string{"Merge train #1", "#102 and #103", "#" + strconv.Itoa(train1.Number),
		"own approval at `head-101` still stands", "at most 2 change(s)", "> The cache added by one change"} {
		if !strings.Contains(comments[0].Body, want) {
			t.Fatalf("#101's refusal comment lacks %q:\n%s", want, comments[0].Body)
		}
	}
	if len(rig.forge.submissions()) != 0 {
		t.Fatal("the train wrote a review of its own")
	}
}

// TestTrain_ARefusedSingleChangeIsEjectedWithTheFindings is the one case the
// findings single out a change: the train carried nothing else. It goes back
// to its worker with the review, as a red pipeline's culprit does.
func TestTrain_ARefusedSingleChangeIsEjectedWithTheFindings(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) {
		return VerdictRequestChanges, "On the current main this double-closes the file."
	}
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a refused train merged: %+v", merges)
	}
	entry := rig.entry("task-a")
	if entry.State != TrainEntryEjected || entry.Ejection == nil || entry.Ejection.Reason != TrainEjectReviewRefused ||
		!strings.Contains(entry.Ejection.Findings, "double-closes") {
		t.Fatalf("entry = %+v, want ejected with the reviewer's findings", entry)
	}
	comments := rig.forge.commentsOn(101)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "> On the current main") ||
		!strings.Contains(comments[0].Body, "#"+strconv.Itoa(rig.trainPRs()["queue/1"].Number)) {
		t.Fatalf("#101 comments = %+v, want the findings and the train's pull request", comments)
	}
	if !rig.forge.discussedOnlyAsPullRequest(101) {
		t.Fatal("the ejection comment went through the issue discussion, which on GitLab is another object")
	}
	if labels := rig.forge.pullRequestLabels(101); len(labels) != 1 || labels[0] != "factory:train-ejected" {
		t.Fatalf("#101 labels = %v", labels)
	}
	if !rig.forge.labelledOnlyAsPullRequest(101) {
		t.Fatal("the ejection label reached #101 through an issue operation")
	}
	if rig.ledger().Active != nil {
		t.Fatal("the refused train is still on the ledger")
	}
}

// TestTrain_AnEjectionLabelTheForgeRefusesFailsThePass is the error half: a
// pull request the forge would not label is not recorded as ejected, so the
// next pass labels it again rather than leaving it reading as queued.
func TestTrain_AnEjectionLabelTheForgeRefusesFailsThePass(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) {
		return VerdictRequestChanges, "On the current main this double-closes the file."
	}
	rig.forge.mu.Lock()
	rig.forge.addLabelErr = errors.New("the forge refused the label")
	rig.forge.mu.Unlock()

	if err := rig.run(orchestration.JobStateInitializing); err == nil || !strings.Contains(err.Error(), "the forge refused the label") {
		t.Fatalf("Run err = %v, want the refused label", err)
	}
	if entry := rig.entry("task-a"); entry.State == TrainEntryEjected {
		t.Fatalf("entry = %+v, recorded ejected though its label never landed", entry)
	}
}

// TestTrain_ARefusalResumedHalfWayPostsNothingTwice proves the refusal is
// idempotent: a train interrupted after some of its comments were posted
// finishes the same refusal on resume, and no change's pull request is told
// twice.
func TestTrain_ARefusalResumedHalfWayPostsNothingTwice(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.queue(102, "task-b")
	rig.pipeline(trainCommit(1, 102), "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) { return VerdictRequestChanges, "They collide." }
	ctx, cancel := context.WithCancel(context.Background())
	rig.forge.onCommentOnIssue = func(number int) {
		if number == 102 {
			cancel()
		}
	}
	if err := rig.runner.Run(ctx, rig.job(ctx, orchestration.JobStateInitializing)); err == nil {
		t.Fatal("the first run finished; it was meant to be interrupted during the refusal")
	}
	if ledger := rig.ledger(); ledger.Active == nil || ledger.Active.Phase != TrainReviewing {
		t.Fatalf("interrupted ledger = %+v, want the train still reviewing", ledger.Active)
	}
	// The resumed job finishes the refusal, then forms the smaller trains it
	// asked for — one change each — and these the reviewer approves.
	rig.forge.onCommentOnIssue = nil
	rig.reviews.decide = approveEvery
	rig.pipeline(trainCommit(2, 101), "success", "")
	rig.pipeline(trainCommit(3, 102), "success", "")
	if err := rig.run(TrainResumeState(orchestration.JobStateValidating)); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	for _, pr := range []int{101, 102} {
		if comments := rig.forge.commentsOn(pr); len(comments) != 1 {
			t.Fatalf("#%d comments = %d, want the refusal once", pr, len(comments))
		}
	}
	merges := rig.forge.mergeCalls()
	if len(merges) != 2 || merges[0].head != trainCommit(2, 101) || merges[1].head != trainCommit(3, 102) {
		t.Fatalf("merges = %+v, want the two changes landed by a train each", merges)
	}
	if refused := rig.audited(auditTrainRefused); len(refused) != 1 {
		t.Fatalf("refusals recorded = %v, want the one refusal, finished once", refused)
	}
}

// TestTrain_ARestartWhileAwaitingReviewResumesTheWait proves a train
// interrupted while its reviewer reads resumes the wait: the same pull
// request, the same head, and no second review asked for.
func TestTrain_ARestartWhileAwaitingReviewResumesTheWait(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	head := trainCommit(1, 101)
	rig.pipeline(head, "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) { return "", "" }
	ctx, cancel := context.WithCancel(context.Background())
	rig.forge.onListReviews = func(int) {
		if len(rig.reviews.started) > 0 {
			cancel()
		}
	}
	if err := rig.runner.Run(ctx, rig.job(ctx, orchestration.JobStateInitializing)); !errors.Is(err, context.Canceled) {
		t.Fatalf("first run = %v, want cancelled while the reviewer reads", err)
	}
	ledger := rig.ledger()
	if ledger.Active == nil || ledger.Active.Phase != TrainReviewing || ledger.Active.Current().ReviewRequestedAt.IsZero() {
		t.Fatalf("interrupted ledger = %+v, want reviewing with the request recorded", ledger.Active)
	}
	trainPR := ledger.Active.Current().PullRequest

	// The reviewer the first run asked finishes while nothing is running.
	rig.forge.onListReviews = nil
	rig.forge.mu.Lock()
	rig.forge.reviews[trainPR] = append(rig.forge.reviews[trainPR], ForgeReview{
		ID: 9, Reviewer: fakeReviewerLogin, State: giteaReviewStates[VerdictApprove], Body: "fine", HeadSHA: head,
	})
	rig.forge.mu.Unlock()
	if err := rig.run(TrainResumeState(orchestration.JobStateValidating)); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if requests := rig.reviews.requests(); len(requests) != 1 {
		t.Fatalf("review jobs = %d, want the one asked before the restart", len(requests))
	}
	if len(rig.builder.pushed) != 1 || len(rig.trainPRs()) != 1 {
		t.Fatalf("the resumed train rebuilt or reopened: pushes %v, pull requests %v", rig.builder.pushed, rig.trainPRs())
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed by the resumed train", got)
	}
}

// TestTrain_AProjectThatLosesItsCountingReviewerMidTrainIsNotLanded proves a
// train waiting on a reviewer the binding no longer has is abandoned rather
// than landed or left waiting: its changes go back to the queue, and no train
// is formed for a project that can no longer review one.
func TestTrain_AProjectThatLosesItsCountingReviewerMidTrainIsNotLanded(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.reviews.decide = func(*MergeTrain) (ReviewVerdict, string) { return "", "" }
	ctx, cancel := context.WithCancel(context.Background())
	rig.forge.onListReviews = func(int) { cancel() }
	if err := rig.runner.Run(ctx, rig.job(ctx, orchestration.JobStateInitializing)); !errors.Is(err, context.Canceled) {
		t.Fatalf("first run = %v, want cancelled while reviewing", err)
	}
	rig.forge.onListReviews = nil
	no := false
	binding := testBinding()
	binding.Reviewer = trainTestReviewer()
	binding.Reviewer.CountsTowardMerge = &no
	if err := rig.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	if err := rig.run(TrainResumeState(orchestration.JobStateValidating)); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a train with no counting reviewer landed: %+v", merges)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryQueued {
		t.Fatalf("entry = %+v, want back in the queue", got)
	}
	if rig.ledger().Active != nil || rig.ledger().Next != 2 {
		t.Fatalf("ledger = %+v, want the train ended and no new one formed", rig.ledger())
	}
}

// TestTrain_AProjectWithNoCountingReviewerIsNeverTrained keeps the behaviour
// from before trains were reviewed: a project with no reviewer whose verdict
// counts forms no train, and the job says why.
func TestTrain_AProjectWithNoCountingReviewerIsNeverTrained(t *testing.T) {
	rig := newTrainRig(t)
	no := false
	binding := testBinding()
	binding.Reviewer = trainTestReviewer()
	binding.Reviewer.CountsTowardMerge = &no
	if err := rig.store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}
	rig.queue(101, "task-a")
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rig.agents.spawns) != 0 || len(rig.reviews.requests()) != 0 || rig.ledger().Version != 0 {
		t.Fatal("a project with no counting reviewer formed a train")
	}
	if off := rig.audited(auditTrainOff); len(off) != 1 || !strings.Contains(off[0], "no reviewer profile that counts toward merge") {
		t.Fatalf("the job did not record why no train formed: %v", off)
	}
}

// =============================================================================
// Red
// =============================================================================

// TestTrain_ARedTrainIsBisectedTheCulpritEjectedAndTheRestLand is the red
// path whole: three changes, the second breaks the build. The full train is
// red, the first alone green, the first two red — so the second is ejected
// with the failure, the first lands, and the third rides the next train.
func TestTrain_ARedTrainIsBisectedTheCulpritEjectedAndTheRestLand(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.queue(102, "task-b")
	rig.queue(103, "task-c")
	rig.pipeline(trainCommit(1, 103), "failure", genuineRed)
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.pipeline(trainCommit(1, 102), "failure", genuineRed)
	rig.pipeline(trainCommit(2, 103), "success", "")

	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}

	ejected := rig.entry("task-b")
	if ejected.State != TrainEntryEjected || ejected.Ejection == nil || ejected.Ejection.Reason != TrainEjectChecksFailed {
		t.Fatalf("culprit = %+v, want ejected for its checks", ejected)
	}
	if !strings.Contains(ejected.Ejection.Findings, "got 1, want 2") {
		t.Fatalf("the worker is not told what failed:\n%s", ejected.Ejection.Findings)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded || got.Train != 1 {
		t.Fatalf("the verified prefix = %+v, want landed by train 1", got)
	}
	if got := rig.entry("task-c"); got.State != TrainEntryLanded || got.Train != 2 {
		t.Fatalf("the change after the culprit = %+v, want requeued and landed by train 2", got)
	}
	probes := rig.audited(auditTrainProbe)
	if len(probes) != 4 {
		t.Fatalf("probes = %v, want the full train, two prefixes, and train 2", probes)
	}
	// Only what lands is reviewed: the verified prefix of train 1, then
	// train 2 — never the red full train or the red two-change prefix.
	reviewed := map[string]bool{}
	for _, req := range rig.reviews.requests() {
		number, err := strconv.Atoi(req.Params[ParamPullRequest])
		if err != nil {
			t.Fatal(err)
		}
		for branch, pr := range rig.trainPRs() {
			if pr.Number == number {
				reviewed[branch] = true
			}
		}
	}
	if len(reviewed) != 2 || !reviewed[TrainProbeBranch(1, 1)] || !reviewed[TrainBranch(2)] {
		t.Fatalf("reviewed %v, want only queue/1-1 and queue/2", reviewed)
	}
	comments := rig.forge.commentsOn(102)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "Ejected from merge train #1") {
		t.Fatalf("culprit's pull request comments = %+v", comments)
	}
	if labels := rig.forge.pullRequestLabels(102); len(labels) != 1 || labels[0] != "factory:train-ejected" {
		t.Fatalf("culprit's pull request labels = %v", labels)
	}
	if !rig.forge.labelledOnlyAsPullRequest(102) {
		t.Fatal("the ejection label reached the culprit through an issue operation")
	}
	// Every probe that did not land is closed and its branch removed.
	for branch, pr := range rig.trainPRs() {
		if pr.State != "closed" {
			t.Fatalf("train pull request %s is still %s", branch, pr.State)
		}
	}
}

// TestTrain_ARedSingleChangeIsEjectedAndNothingLands is the smallest red:
// nothing was verified, so nothing is merged.
func TestTrain_ARedSingleChangeIsEjectedAndNothingLands(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "failure", genuineRed)
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("a red train merged: %+v", merges)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryEjected {
		t.Fatalf("entry = %+v, want ejected", got)
	}
}

// TestTrain_AVoidRedIsRerunNotEjected proves a runner that died is not the
// change's fault: the check is re-run and the train waits for the new answer.
func TestTrain_AVoidRedIsRerunNotEjected(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	head := trainCommit(1, 101)
	rig.pipeline(head, "failure", logRunnerDied)
	// The re-run the train asks for comes back green, as a re-run of a job
	// whose runner died does.
	rig.forge.onListCheckRuns = func(sha string) {
		if sha == head && len(rig.forge.reruns) > 0 {
			rig.forge.checks[head] = []ForgeCheckRun{{ID: "ci-2", Name: "ci", HeadSHA: head, Status: "completed",
				Conclusion: "success", Completed: time.Now()}}
		}
	}
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reruns := rig.forge.rerunCalls(); len(reruns) != 1 || reruns[0].HeadSHA != head {
		t.Fatalf("reruns = %+v, want one of %s", reruns, head)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed once the rerun went green", got)
	}
	if ejected := rig.audited(auditTrainEjectedPR); len(ejected) != 0 {
		t.Fatalf("a void red ejected a change: %v", ejected)
	}
}

// TestTrain_AHostDefectStopsTheTrainWithoutEjectingAnyone proves a pipeline
// that cannot answer is an operator's problem: the changes wait in the queue,
// and the job fails with the defect.
func TestTrain_AHostDefectStopsTheTrainWithoutEjectingAnyone(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "failure", logHostDefect)
	err := rig.run(orchestration.JobStateInitializing)
	if !errors.Is(err, ErrCIHostDefect) {
		t.Fatalf("Run = %v, want a CI host defect", err)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryQueued {
		t.Fatalf("entry = %+v, want back in the queue", got)
	}
	if rig.ledger().Active != nil {
		t.Fatal("the stopped train is still on the ledger")
	}
}

// =============================================================================
// Build-time ejections
// =============================================================================

// TestTrain_AConflictingChangeIsEjectedBeforeAnyPipelineRuns proves a change
// that does not merge onto the train costs the train nothing: it is ejected at
// build, and the train is tested without it.
func TestTrain_AConflictingChangeIsEjectedBeforeAnyPipelineRuns(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.queue(102, "task-b")
	rig.builder.conflicts[101] = true
	rig.pipeline(trainCommit(1, 102), "success", "")
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ejected := rig.entry("task-a")
	if ejected.State != TrainEntryEjected || ejected.Ejection.Reason != TrainEjectConflict ||
		!strings.Contains(ejected.Ejection.Detail, "pkg/widget.go") {
		t.Fatalf("conflicting change = %+v", ejected)
	}
	if got := rig.entry("task-b"); got.State != TrainEntryLanded {
		t.Fatalf("the other change = %+v, want landed", got)
	}
}

// TestTrain_AMovedPullRequestIsEjectedForTheGate proves a change pushed to
// after its approval does not ride on that approval.
func TestTrain_AMovedPullRequestIsEjectedForTheGate(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.forge.mu.Lock()
	rig.forge.prs[101].HeadSHA = "pushed-later"
	rig.forge.mu.Unlock()
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryEjected || got.Ejection.Reason != TrainEjectHeadMoved {
		t.Fatalf("entry = %+v, want ejected because its head moved", got)
	}
	if len(rig.builder.pushed) != 0 {
		t.Fatalf("a train with no changes left was pushed: %v", rig.builder.pushed)
	}
}

// TestTrain_AFailedPushAbandonsTheTrainAndKeepsTheChangesQueued proves a
// failure that is no change's leaves every change where it was.
func TestTrain_AFailedPushAbandonsTheTrainAndKeepsTheChangesQueued(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.builder.pushErr = errors.New("remote rejected")
	err := rig.run(orchestration.JobStateInitializing)
	if !errors.Is(err, ErrTrainBuild) {
		t.Fatalf("Run = %v, want a train build error", err)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryQueued || got.Train != 0 {
		t.Fatalf("entry = %+v, want queued again", got)
	}
	if rig.ledger().Active != nil {
		t.Fatal("the abandoned train is still on the ledger")
	}
}

// =============================================================================
// Landing guards
// =============================================================================

// TestTrain_ATargetThatMovedIsNotLandedOnto proves the train lands only onto
// the commit it was tested on: a target that moved abandons the train, ejects
// no one, and the change rides the next train, built on the target as it now
// is.
func TestTrain_ATargetThatMovedIsNotLandedOnto(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.pipeline(trainCommit(2, 101), "success", "")
	var once sync.Once
	rig.forge.onCreatePullRequest = func(*ForgePullRequest) {
		once.Do(func() { rig.forge.branches["main"] = "someone-else-merged" })
	}
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	merges := rig.forge.mergeCalls()
	if len(merges) != 1 || merges[0].head != trainCommit(2, 101) {
		t.Fatalf("merges = %+v, want only train 2, built on the moved target", merges)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded || got.Train != 2 {
		t.Fatalf("entry = %+v, want landed by train 2", got)
	}
	if len(rig.audited(auditTrainAbandoned)) != 1 {
		t.Fatalf("abandonments = %v", rig.audited(auditTrainAbandoned))
	}
	if ejected := rig.audited(auditTrainEjectedPR); len(ejected) != 0 {
		t.Fatalf("a moved target ejected a change: %v", ejected)
	}
}

// TestTrain_AChangeThatLostItsApprovalIsEjectedAtLanding proves the train
// lands nothing it did not re-read: a change whose review status is no longer
// success is ejected, and nothing lands on its approval.
func TestTrain_AChangeThatLostItsApprovalIsEjectedAtLanding(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.forge.mu.Lock()
	rig.forge.checks["head-101"] = []ForgeCheckRun{{ID: "r2", Name: DefaultReviewStatusContext, HeadSHA: "head-101",
		Status: "completed", Conclusion: "failure"}}
	rig.forge.mu.Unlock()
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if merges := rig.forge.mergeCalls(); len(merges) != 0 {
		t.Fatalf("an unapproved change landed: %+v", merges)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryEjected || got.Ejection.Reason != TrainEjectUnreviewed {
		t.Fatalf("entry = %+v, want ejected as unreviewed", got)
	}
	if len(rig.forge.submissions()) != 0 {
		t.Fatal("the train wrote a review of its own")
	}
}

// TestTrain_AMergeTheForgeRefusesIsATypedFailure proves a refused landing is
// named, and leaves the train on the ledger for the next job to finish.
func TestTrain_AMergeTheForgeRefusesIsATypedFailure(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	rig.pipeline(trainCommit(1, 101), "success", "")
	rig.forge.mergeErr = errors.New("protected branch: status check missing")
	err := rig.run(orchestration.JobStateInitializing)
	if !errors.Is(err, ErrTrainLanding) {
		t.Fatalf("Run = %v, want a landing error", err)
	}
	if ledger := rig.ledger(); ledger.Active == nil || ledger.Active.Phase != TrainLanding {
		t.Fatalf("ledger = %+v, want the train still landing", ledger.Active)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryRiding {
		t.Fatalf("entry = %+v, want still riding", got)
	}
}

// =============================================================================
// Restart
// =============================================================================

// TestTrain_ARestartInTestingResumesTheSameProbe proves a train interrupted
// while CI runs resumes where it was: the same probe, the same pull request,
// no second build and no second pull request.
func TestTrain_ARestartInTestingResumesTheSameProbe(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	head := trainCommit(1, 101)

	ctx, cancel := context.WithCancel(context.Background())
	rig.forge.onListCheckRuns = func(sha string) {
		if sha == head {
			cancel()
		}
	}
	if err := rig.runner.Run(ctx, rig.job(ctx, orchestration.JobStateInitializing)); !errors.Is(err, context.Canceled) {
		t.Fatalf("first run = %v, want cancelled while testing", err)
	}
	if ledger := rig.ledger(); ledger.Active == nil || ledger.Active.Phase != TrainTesting || ledger.Active.Current().PullRequest == 0 {
		t.Fatalf("interrupted ledger = %+v", ledger.Active)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryRiding {
		t.Fatalf("entry = %+v, want still riding across the restart", got)
	}

	rig.forge.onListCheckRuns = nil
	rig.pipeline(head, "success", "")
	if err := rig.run(TrainResumeState(orchestration.JobStateValidating)); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if len(rig.builder.pushed) != 1 {
		t.Fatalf("pushes = %v, want the one build before the restart", rig.builder.pushed)
	}
	if prs := rig.trainPRs(); len(prs) != 1 {
		t.Fatalf("train pull requests = %v, want the one opened before the restart", prs)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded {
		t.Fatalf("entry = %+v, want landed by the resumed train", got)
	}
}

// TestTrain_ARestartMidBuildAbandonsAndRebuildsUnderANewNumber proves a build
// is never resumed: its container is gone, and its branch name is never pushed
// twice.
func TestTrain_ARestartMidBuildAbandonsAndRebuildsUnderANewNumber(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	ctx := context.Background()
	ledger := rig.ledger()
	ledger.Next = 2
	ledger.Active = &MergeTrain{Number: 1, Target: "main", Phase: TrainBuilding,
		Cars: []TrainCar{{TaskID: "task-a", PullRequest: 101, HeadSHA: "head-101", Branch: "issue/101"}}}
	if err := rig.store.SaveTrainLedger(ctx, testTenant, ledger); err != nil {
		t.Fatal(err)
	}
	entry := rig.entry("task-a")
	entry.State, entry.Train = TrainEntryRiding, 1
	if err := rig.store.SaveTrainEntry(ctx, testTenant, entry); err != nil {
		t.Fatal(err)
	}
	rig.pipeline(trainCommit(2, 101), "success", "")

	if err := rig.run(TrainResumeState(orchestration.JobStateExecuting)); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded || got.Train != 2 {
		t.Fatalf("entry = %+v, want landed by train 2", got)
	}
	if len(rig.builder.pushed) != 1 || rig.builder.pushed[0] != "queue/2" {
		t.Fatalf("pushes = %v, want only queue/2", rig.builder.pushed)
	}
}

// TestTrain_ARestartMidLandingFinishesWithoutMergingTwice proves the landing
// is idempotent: a train that merged and died before recording it is
// recorded, not merged again.
func TestTrain_ARestartMidLandingFinishesWithoutMergingTwice(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	head := trainCommit(1, 101)
	rig.pipeline(head, "success", "")
	rig.forge.mergeErr = errors.New("unanswered")
	if err := rig.run(orchestration.JobStateInitializing); !errors.Is(err, ErrTrainLanding) {
		t.Fatalf("first run = %v, want the landing to fail", err)
	}
	// The forge had in fact merged it.
	rig.forge.mu.Lock()
	for _, pr := range rig.forge.prs {
		if pr.HeadRef == "queue/1" {
			pr.Merged, pr.State, pr.MergeSHA = true, "closed", "landed-sha"
		}
	}
	rig.forge.mergeErr = nil
	calls := len(rig.forge.merges)
	rig.forge.mu.Unlock()

	if err := rig.run(TrainResumeState(orchestration.JobStateCompleting)); err != nil {
		t.Fatalf("resumed run: %v", err)
	}
	if len(rig.forge.mergeCalls()) != calls {
		t.Fatal("the resumed landing merged a train the forge had already merged")
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded || got.MergeSHA != "landed-sha" {
		t.Fatalf("entry = %+v, want landed at landed-sha", got)
	}
}

// TestTrain_StrayRidersReturnToTheQueue proves an entry left riding a train
// that is no longer in flight is not stranded.
func TestTrain_StrayRidersReturnToTheQueue(t *testing.T) {
	rig := newTrainRig(t)
	rig.queue(101, "task-a")
	entry := rig.entry("task-a")
	entry.State, entry.Train = TrainEntryRiding, 7
	if err := rig.store.SaveTrainEntry(context.Background(), testTenant, entry); err != nil {
		t.Fatal(err)
	}
	rig.pipeline(trainCommit(1, 101), "success", "")
	if err := rig.run(orchestration.JobStateInitializing); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := rig.entry("task-a"); got.State != TrainEntryLanded || got.Train != 1 {
		t.Fatalf("stray entry = %+v, want recovered and landed by train 1", got)
	}
}

// =============================================================================
// Shape and wiring
// =============================================================================

func TestTrainRunner_ShapeAndPolicy(t *testing.T) {
	rig := newTrainRig(t)
	if rig.runner.Name() != TrainRunnerName {
		t.Fatalf("name = %q", rig.runner.Name())
	}
	if !rig.runner.OffProjectBudget() {
		t.Fatal("a train spends the project's worker budget")
	}
	if got := rig.runner.LeaseSubject(&orchestration.JobConfig{ProjectID: testProject}); got != TrainLeaseSubject(testProject) {
		t.Fatalf("lease subject = %q", got)
	}
	if rig.runner.LeaseSubject(nil) != "" {
		t.Fatal("a nil config has a lease subject")
	}
	backed := 0
	for _, spec := range rig.runner.States() {
		if spec.SessionBacked {
			backed++
			if spec.State != orchestration.JobStateExecuting {
				t.Fatalf("%s is session-backed; only the build holds a container", spec.State)
			}
		}
	}
	if backed != 1 {
		t.Fatalf("%d session-backed states, want the build alone", backed)
	}
	for interrupted, want := range map[orchestration.JobState]orchestration.JobState{
		orchestration.JobStateExecuting:  orchestration.JobStateInitializing,
		orchestration.JobStateValidating: orchestration.JobStateValidating,
		orchestration.JobStateCompleting: orchestration.JobStateCompleting,
		orchestration.JobStateFailed:     orchestration.JobStateInitializing,
	} {
		if got := rig.runner.ResumeState(interrupted); got != want {
			t.Fatalf("resume from %s = %s, want %s", interrupted, got, want)
		}
	}
}

func TestTrainRunner_ValidateRefusesATaskOrNoProject(t *testing.T) {
	cases := map[string]*orchestration.JobConfig{
		"nil":        nil,
		"no project": {TenantID: testTenant},
		"a task":     {TenantID: testTenant, ProjectID: testProject, TaskID: "task-1"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTrainConfig(cfg); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("Validate = %v, want an invalid input", err)
			}
		})
	}
	if err := ValidateTrainConfig(&orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject}); err != nil {
		t.Fatalf("a project train was refused: %v", err)
	}
}

func TestNewTrainRunner_RefusesAnIncompleteWiring(t *testing.T) {
	store := newTestStore(t)
	forges := &fakeForges{forge: newFakeForge()}
	base := TrainRunnerConfig{
		Bindings: store, Forges: forges, Trains: store, Jobs: &fakeTrainReviews{}, Tokens: &fakeTokens{token: []byte("t")},
		Signing: &fakeSigningAgent{}, Guard: testRepoGuard(t), SocketsRoot: testSocketsRoot(t),
	}
	cases := map[string]func(*TrainRunnerConfig){
		"no bindings":            func(c *TrainRunnerConfig) { c.Bindings = nil },
		"no forges":              func(c *TrainRunnerConfig) { c.Forges = nil },
		"no trains":              func(c *TrainRunnerConfig) { c.Trains = nil },
		"no jobs":                func(c *TrainRunnerConfig) { c.Jobs = nil },
		"no tokens":              func(c *TrainRunnerConfig) { c.Tokens = nil },
		"no signing":             func(c *TrainRunnerConfig) { c.Signing = nil },
		"negative merge retries": func(c *TrainRunnerConfig) { c.MergeAttempts = -1 },
		"negative poll":          func(c *TrainRunnerConfig) { c.PollInterval = -time.Second },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			if _, err := NewTrainRunner(cfg); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("NewTrainRunner = %v, want an invalid input", err)
			}
		})
	}
	if _, err := NewTrainRunner(base); err != nil {
		t.Fatalf("a complete wiring was refused: %v", err)
	}
}

// TestTrainRunnerStates_NeverLoosenTheDeliveryRunnersBounds proves the train
// shares engine states without changing what the stall detector does to a
// delivery: bounds merge by taking the longest, and session-backing by taking
// either, so a train state that declared more would silently apply to every
// delivery job too.
func TestTrainRunnerStates_NeverLoosenTheDeliveryRunnersBounds(t *testing.T) {
	delivery := map[orchestration.JobState]orchestration.JobStateSpec{}
	for _, spec := range RunnerStates() {
		delivery[spec.State] = spec
	}
	for _, spec := range TrainRunnerStates() {
		shared, ok := delivery[spec.State]
		if !ok {
			continue
		}
		if shared.IdleThreshold > 0 && spec.IdleThreshold > shared.IdleThreshold {
			t.Errorf("%s: the train's bound %s widens the delivery runner's %s", spec.State, spec.IdleThreshold, shared.IdleThreshold)
		}
		if spec.State != orchestration.JobStateExecuting && shared.SessionBacked {
			t.Errorf("%s: the train waits in a state the delivery runner declares session-backed; "+
				"a train there with no session would be ended as stalled", spec.State)
		}
	}
}

// TestTrainRun_TheBuildsPushIsRefusedOnceTheLedgerNamesAnotherTrain proves the
// guard the container's push asks before it publishes: a train another job
// has abandoned or replaced is not pushed over the one now in flight.
func TestTrainRun_TheBuildsPushIsRefusedOnceTheLedgerNamesAnotherTrain(t *testing.T) {
	rig := newTrainRig(t)
	ctx := context.Background()
	ledger := rig.ledger()
	ledger.Active = planTrain(1)
	if err := rig.store.SaveTrainLedger(ctx, testTenant, ledger); err != nil {
		t.Fatal(err)
	}
	run := &trainRun{runner: rig.runner, job: rig.job(ctx, orchestration.JobStateExecuting)}
	if err := run.resolve(ctx); err != nil {
		t.Fatal(err)
	}
	run.containerDir = "/workspace/api"
	builder, ok := run.containerBuilder(ctx).(*containerTrainBuilder)
	if !ok {
		t.Fatalf("the container builder is %T", run.containerBuilder(ctx))
	}
	if builder.workDir != "/workspace/api" || builder.target != "main" {
		t.Fatalf("builder = dir %q target %q", builder.workDir, builder.target)
	}
	if err := builder.publisher.admit(); err != nil {
		t.Fatalf("the train in flight was refused its own push: %v", err)
	}

	replaced := rig.ledger()
	replaced.Active.Number = 10
	if err := rig.store.SaveTrainLedger(ctx, testTenant, replaced); err != nil {
		t.Fatal(err)
	}
	if err := builder.publisher.admit(); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("a replaced train's push = %v, want refused", err)
	}
}

// TestTrainErrors_NameTheTrainAndUnwrap proves an operator reading a failed
// train job learns which train, which project and why, and a caller can test
// the class.
func TestTrainErrors_NameTheTrainAndUnwrap(t *testing.T) {
	cause := errors.New("remote rejected")
	cases := []struct {
		err      error
		sentinel error
		cause    error
		want     []string
	}{
		{&TrainUnavailableError{ProjectID: "api"}, ErrTrainUnavailable, nil, []string{"api", "merge train"}},
		{&TrainBuildError{ProjectID: "api", Train: 7, Reason: "the train could not be pushed", Cause: cause},
			ErrTrainBuild, cause, []string{"#7", "api", "could not be pushed", "remote rejected"}},
		{&TrainBuildError{ProjectID: "api", Train: 7, Reason: "no cause"}, ErrTrainBuild, nil, []string{"#7", "no cause"}},
		{&TrainLandingError{ProjectID: "api", Train: 7, PullRequest: 901, Reason: "refused", Cause: cause},
			ErrTrainLanding, cause, []string{"#7", "#901", "refused", "remote rejected"}},
		{&TrainLandingError{ProjectID: "api", Train: 7, PullRequest: 901, Reason: "no cause"}, ErrTrainLanding, nil, []string{"#901"}},
	}
	for _, tc := range cases {
		if !errors.Is(tc.err, tc.sentinel) {
			t.Fatalf("%T does not unwrap to %v", tc.err, tc.sentinel)
		}
		if tc.cause != nil && !errors.Is(tc.err, tc.cause) {
			t.Fatalf("%T hides its cause", tc.err)
		}
		for _, want := range tc.want {
			if !strings.Contains(tc.err.Error(), want) {
				t.Fatalf("%q lacks %q", tc.err.Error(), want)
			}
		}
	}
}
