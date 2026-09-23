// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

type hooksRig struct {
	hooks  *Hooks
	store  *Store
	forge  *fakeForge
	tasks  *fakeTasks
	waiter *immediateWaiter
}

func newHooksRig(t *testing.T) *hooksRig {
	t.Helper()
	store := newTestStore(t)
	if err := store.PutBinding(context.Background(), testTenant, testBinding()); err != nil {
		t.Fatal(err)
	}
	forge := newFakeForge()
	forge.putIssue(25, "Fix the thing", "task:ready", "bug")
	tasks := newFakeTasks(testTaskRecord(25))
	waiter := &immediateWaiter{}
	hooks, err := NewHooks(HooksConfig{
		Projects: &fakeProjects{projects: map[string]*orchestration.Project{testProject: testOrchProject()}},
		Bindings: store, Deliveries: store, Tasks: tasks, Forges: &fakeForges{forge: forge},
		MergeAttempts: 3, Waiter: waiter, Surfacing: SurfacePolicy{RetryAttempts: 2, RetryInterval: 1, PostTimeout: 1e9},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &hooksRig{hooks: hooks, store: store, forge: forge, tasks: tasks, waiter: waiter}
}

// rebind replaces the rig's binding with a mutated copy, so a test can run
// the same hooks under a different SDLC without a second rig.
func (r *hooksRig) rebind(ctx context.Context, mutate func(*ProjectBinding)) error {
	binding := testBinding()
	mutate(binding)
	return r.store.PutBinding(ctx, testTenant, binding)
}

// containerWorkDir stands in for the directory the worker's container
// decides for itself; the daemon never creates one.
func containerWorkDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "workspace")
}

// testPublishedHead is the head the fake forge derives for the attempt's
// branch. A Complete test has to publish THAT head, because the head guard
// compares what the review approved against what the forge reports — which is
// exactly the check a real forge makes real.
const testPublishedHead = "head-issue/25-fix-the-thing"

// publishHead records the head the WORKER'S CONTAINER pushed, which is what
// the runner does between the container's publish and the forge writes.
//
// It stands in for the container here because these tests are about the forge
// half: the git half has no daemon-side implementation left to fake, and its
// own tests drive the real command builder through a scripted container.
func (r *hooksRig) publishHead(t *testing.T, ctx context.Context, head string) {
	t.Helper()
	delivery, err := r.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	delivery.HeadSHA = head
	delivery.WorkDir = containerWorkDir(t)
	if err := r.store.UpdateDelivery(ctx, testTenant, delivery); err != nil {
		t.Fatal(err)
	}
}

func TestHooks_PrepareWritesTheDeliveryAndTouchesNoFilesystem(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	before := t.TempDir()
	entriesBefore, err := os.ReadDir(before)
	if err != nil {
		t.Fatal(err)
	}
	workDir, err := rig.hooks.Prepare(ctx, testJob(1))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	// The container decides its own workdir, so the hook names none.
	if workDir != "" {
		t.Fatalf("workDir = %q, want empty: the daemon materialises nothing", workDir)
	}
	entriesAfter, err := os.ReadDir(before)
	if err != nil {
		t.Fatal(err)
	}
	if len(entriesAfter) != len(entriesBefore) {
		t.Fatalf("Prepare wrote to the filesystem: %v", entriesAfter)
	}
	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil || delivery.Branch != "issue/25-fix-the-thing" || delivery.Attempt != 1 || delivery.Base != "main" ||
		delivery.HeadSHA != "" || delivery.WorkDir != "" {
		t.Fatalf("delivery = %+v, %v", delivery, err)
	}
	labels := rig.forge.issueLabels(25)
	if strings.Join(labels, ",") != "bug,factory:in-progress" {
		t.Fatalf("labels after Prepare = %v (the trigger goes, the foreign label stays, in-progress arrives)", labels)
	}
}

func TestHooks_PrepareContinuesAPushedBranchOnTheNextAttempt(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatal(err)
	}
	prior, _ := rig.store.GetDelivery(ctx, testTenant, testTask)
	prior.HeadSHA = "h1"
	prior.PullRequest = DeliveryPullRequest{Number: 100, State: PullRequestOpen}
	prior.Reviews = []ReviewRecord{approve("codex", ModelFamilyOpenAI, "h1", 1)}
	if err := rig.store.UpdateDelivery(ctx, testTenant, prior); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.hooks.Prepare(ctx, testJob(2)); err != nil {
		t.Fatal(err)
	}
	delivery, _ := rig.store.GetDelivery(ctx, testTenant, testTask)
	if delivery.Attempt != 2 || delivery.Branch != "issue/25-fix-the-thing" || delivery.HeadSHA != "h1" ||
		delivery.PullRequest.Number != 100 || len(delivery.Reviews) != 1 {
		t.Fatalf("carry-over lost: %+v", delivery)
	}
}

// TestHooks_ReviewableOpensThePullRequestOnThePublishedHead: the container
// already committed, signed and pushed, and recorded the head it produced.
// What is left for the daemon is the forge — open or refresh the pull
// request, record it, publish it, move the label — and nothing else.
func TestHooks_ReviewableOpensThePullRequestOnThePublishedHead(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}

	// Nothing was published: there is no head to open a pull request against,
	// and opening one anyway would point it at a branch the remote has never
	// seen.
	err := rig.hooks.Reviewable(ctx, job)
	var noHead *NoPublishedHeadError
	if !errors.As(err, &noHead) || !errors.Is(err, ErrRunner) {
		t.Fatalf("no published head: %v", err)
	}
	if len(rig.forge.prs) != 0 {
		t.Fatalf("a pull request was opened for an unpublished branch: %v", rig.forge.prs)
	}

	rig.publishHead(t, ctx, "head-1")
	if err := rig.hooks.Reviewable(ctx, job); err != nil {
		t.Fatalf("Reviewable: %v", err)
	}
	delivery, _ := rig.store.GetDelivery(ctx, testTenant, testTask)
	if delivery.HeadSHA != "head-1" || delivery.PullRequest.Number != 100 ||
		delivery.PullRequest.State != PullRequestOpen || delivery.PullRequest.Base != "main" {
		t.Fatalf("delivery = %+v", delivery)
	}
	pr := rig.forge.prs[100]
	if pr.Title != "Fix the thing" || !strings.HasPrefix(pr.Body, "Closes #25") || pr.BaseRef != "main" ||
		pr.HeadRef != "issue/25-fix-the-thing" {
		t.Fatalf("pull request = %+v", pr)
	}
	// The names are scoped to the attempt: the store overwrites by name and a
	// retried task keeps one record, so a flat name would have a later
	// attempt's delivery replace this one's.
	artifacts := rig.tasks.submitted(testTask)
	wantPR := orchestration.ArtifactNameForAttempt(orchestration.ArtifactNamePullRequest, job.Attempt)
	wantHead := orchestration.ArtifactNameForAttempt(orchestration.ArtifactNameHead, job.Attempt)
	if len(artifacts) != 2 || artifacts[0].Kind != orchestration.ArtifactKindURL || artifacts[0].Name != wantPR || artifacts[0].Ref.Path != pr.URL ||
		artifacts[1].Kind != orchestration.ArtifactKindCommit || artifacts[1].Name != wantHead || artifacts[1].Ref.SHA != delivery.HeadSHA {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	if labels := rig.forge.issueLabels(25); strings.Join(labels, ",") != "bug,factory:in-review" {
		t.Fatalf("labels after Reviewable = %v", labels)
	}

	// A second round refreshes the pull request rather than opening another.
	rig.publishHead(t, ctx, "head-2")
	if err := rig.hooks.Reviewable(ctx, job); err != nil {
		t.Fatal(err)
	}
	if len(rig.forge.prs) != 1 {
		t.Fatalf("a second pull request was opened: %d", len(rig.forge.prs))
	}
	delivery, _ = rig.store.GetDelivery(ctx, testTenant, testTask)
	if delivery.HeadSHA != "head-2" {
		t.Fatalf("head not advanced: %q", delivery.HeadSHA)
	}
}

// TestHooks_ReviewableRunsNoGit is the boundary this phase moved: the daemon
// has no git side left at all, so the hook set is constructible and usable
// without one and a forge failure is the only kind of failure it can have.
func TestHooks_ReviewableRunsNoGit(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, "head-1")
	rig.forge.createPRErr = errors.New("the forge refused")
	if err := rig.hooks.Reviewable(ctx, job); err == nil || !strings.Contains(err.Error(), "the forge refused") {
		t.Fatalf("forge failure swallowed: %v", err)
	}
}

func TestHooks_CompleteMergesTheReviewedHeadOnly(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, job); err != nil {
		t.Fatal(err)
	}
	// Someone pushed after the review: refuse.
	rig.forge.prs[100].HeadSHA = "moved"
	err := rig.hooks.Complete(ctx, job)
	var moved *HeadMovedError
	if !errors.As(err, &moved) || !errors.Is(err, ErrHeadMoved) || moved.Current != "moved" {
		t.Fatalf("head moved: %v", err)
	}
	if len(rig.forge.merges) != 0 {
		t.Fatal("a merge was issued against a moved head")
	}
	// Back on the reviewed head, not ready twice, then merged.
	rig.forge.prs[100].HeadSHA = testPublishedHead
	rig.forge.mergeNotReadyN = 2
	if err := rig.hooks.Complete(ctx, job); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(rig.forge.merges) != 3 || rig.forge.merges[2].head != testPublishedHead || rig.forge.merges[2].spec.Method != "squash" ||
		!strings.Contains(rig.forge.merges[2].spec.CommitMessage, "Closes #25") || rig.waiter.waits != 2 {
		t.Fatalf("merges = %+v waits=%d", rig.forge.merges, rig.waiter.waits)
	}
	delivery, _ := rig.store.GetDelivery(ctx, testTenant, testTask)
	if delivery.PullRequest.State != PullRequestMerged {
		t.Fatalf("state = %s", delivery.PullRequest.State)
	}
	if labels := rig.forge.issueLabels(25); strings.Join(labels, ",") != "bug,factory:merged" {
		t.Fatalf("labels after Complete = %v", labels)
	}
	artifacts := rig.tasks.submitted(testTask)
	if artifacts[len(artifacts)-2].Metadata["state"] != "merged" {
		t.Fatalf("merged state not published: %+v", artifacts[len(artifacts)-2])
	}
	// Already merged on the forge: idempotent.
	if err := rig.hooks.Complete(ctx, job); err != nil || len(rig.forge.merges) != 3 {
		t.Fatalf("second Complete: %v merges=%d", err, len(rig.forge.merges))
	}
}

// completeRig is a hooks rig whose attempt has published and opened its pull
// request, so Complete has a reviewed head to merge.
func completeRig(t *testing.T) (*hooksRig, *orchestration.JobContext) {
	t.Helper()
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, job); err != nil {
		t.Fatal(err)
	}
	return rig, job
}

// TestHooks_CompleteReportsASettledConflictAsAConflict is the go-truststrap#64
// defect: an approved pull request whose base moved under it into a real
// conflict. Gitea answers its merge "Please try again later" forever, and the
// hook used to retry that to exhaustion. The attempt was then charged as a
// failure of the work, with nothing ever asking for the rebase that is the only
// fix.
func TestHooks_CompleteReportsASettledConflictAsAConflict(t *testing.T) {
	rig, job := completeRig(t)
	ctx := context.Background()
	rig.forge.prs[100].Mergeable = false
	rig.forge.mergeNotReadyN = 10

	err := rig.hooks.Complete(ctx, job)
	var conflict *MergeConflictError
	if !errors.As(err, &conflict) || !errors.Is(err, ErrMergeConflict) {
		t.Fatalf("Complete = %v, want a MergeConflictError", err)
	}
	if errors.Is(err, ErrMergeNotReadyExhausted) {
		t.Fatal("a settled conflict is still reported as a forge that was not ready")
	}
	if conflict.Base != "main" || conflict.Head != testPublishedHead || conflict.Attempts != 3 {
		t.Fatalf("conflict = %+v, want base main, the approved head and 3 attempts", conflict)
	}
	// The bound is unchanged: the merge was asked for exactly as many times
	// as a not-ready forge is given, and the conflict was decided after it.
	if len(rig.forge.merges) != 3 || rig.waiter.waits != 2 {
		t.Fatalf("merges=%d waits=%d, want the full bounded retry before the verdict", len(rig.forge.merges), rig.waiter.waits)
	}
	delivery, _ := rig.store.GetDelivery(ctx, testTenant, testTask)
	if delivery.PullRequest.State == PullRequestMerged {
		t.Fatal("a conflicting pull request was recorded as merged")
	}
	if labels := rig.forge.issueLabels(25); strings.Contains(strings.Join(labels, ","), "factory:merged") {
		t.Fatalf("a conflicting pull request labelled the issue merged: %v", labels)
	}
}

// TestHooks_CompleteMergesAPullRequestWhoseCheckWasStillPending guards the
// other side of the rule. While Gitea is still checking a PR it reports the PR
// not mergeable, with the same message it uses for a conflict. That answer is
// only read as a conflict once the bound is spent, so a check that settles
// inside the bound merges as it always did.
func TestHooks_CompleteMergesAPullRequestWhoseCheckWasStillPending(t *testing.T) {
	rig, job := completeRig(t)
	ctx := context.Background()
	rig.forge.prs[100].Mergeable = false
	rig.forge.mergeNotReadyN = 2

	if err := rig.hooks.Complete(ctx, job); err != nil {
		t.Fatalf("Complete = %v, want the merge that succeeded on the third attempt", err)
	}
	if len(rig.forge.merges) != 3 || !rig.forge.prs[100].Merged {
		t.Fatalf("merges=%d merged=%v", len(rig.forge.merges), rig.forge.prs[100].Merged)
	}
}

// TestHooks_SettledRefusalKeepsEveryAnswerThatIsNotAConflict makes the
// conflict verdict narrow. It is given only to an open PR that is still on the
// approved head and still not mergeable. Every other final read keeps the
// answer it would have had without the classification.
func TestHooks_SettledRefusalKeepsEveryAnswerThatIsNotAConflict(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fakeForge)
		check func(t *testing.T, err error)
	}{
		{
			name:  "mergeable yet refused is a forge that is not ready",
			setup: func(*fakeForge) {},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrMergeNotReadyExhausted) || errors.Is(err, ErrMergeConflict) {
					t.Fatalf("err = %v, want exhaustion", err)
				}
			},
		},
		{
			name:  "a closed pull request is not a conflict a rebase resolves",
			setup: func(f *fakeForge) { f.prs[100].Mergeable, f.prs[100].State = false, "closed" },
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrMergeNotReadyExhausted) {
					t.Fatalf("err = %v, want exhaustion", err)
				}
			},
		},
		{
			name:  "a head that moved is refused as moved",
			setup: func(f *fakeForge) { f.prs[100].Mergeable, f.prs[100].HeadSHA = false, "pushed-later" },
			check: func(t *testing.T, err error) {
				var moved *HeadMovedError
				if !errors.As(err, &moved) || moved.Current != "pushed-later" {
					t.Fatalf("err = %v, want HeadMovedError to pushed-later", err)
				}
			},
		},
		{
			name:  "a pull request merged meanwhile is the outcome asked for",
			setup: func(f *fakeForge) { f.prs[100].Mergeable, f.prs[100].Merged = false, true },
			check: func(t *testing.T, err error) {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			},
		},
		{
			name:  "a pull request the forge no longer returns is not found",
			setup: func(f *fakeForge) { f.getPRNil = true },
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
			},
		},
		{
			name:  "a read that fails is that failure",
			setup: func(f *fakeForge) { delete(f.prs, 100) },
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want the read's NotFoundError", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newHooksRig(t)
			pr, err := rig.forge.CreatePullRequest(context.Background(), testRepo, PullRequestSpec{
				Title: "t", HeadRef: "issue/25-fix-the-thing", BaseRef: "main",
			})
			if err != nil {
				t.Fatal(err)
			}
			rig.forge.mergeNotReadyN = 10
			tc.setup(rig.forge)
			tc.check(t, rig.hooks.merge(context.Background(), rig.forge, testRepo, pr.Number,
				MergeSpec{Method: MergeMethodSquash}, testPublishedHead))
		})
	}
}

func TestHooks_CompleteExhaustsNotReadyAndRefusesOtherFailures(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, testPublishedHead)
	if err := rig.hooks.Reviewable(ctx, job); err != nil {
		t.Fatal(err)
	}
	rig.forge.mergeNotReadyN = 10
	err := rig.hooks.Complete(ctx, job)
	var exhausted *MergeNotReadyExhaustedError
	if !errors.As(err, &exhausted) || exhausted.Attempts != 3 || len(rig.forge.merges) != 3 {
		t.Fatalf("exhaustion: %v merges=%d", err, len(rig.forge.merges))
	}
	rig.forge.mergeNotReadyN = 0
	rig.forge.merges = nil
	rig.forge.mergeErr = &ForgeWriteNotAppliedError{Type: ForgeGitea, Operation: "merge"}
	if err := rig.hooks.Complete(ctx, job); !errors.Is(err, ErrForgeWriteNotApplied) || len(rig.forge.merges) != 1 {
		t.Fatalf("permanent refusal retried: %v merges=%d", err, len(rig.forge.merges))
	}
	noPR := testJob(1)
	noPR.TaskID = "task-2"
	rig.tasks.tasks["task-2"] = testTaskRecord(26)
	rig.tasks.tasks["task-2"].ID = "task-2"
	if err := rig.store.PutDelivery(ctx, testTenant, &Delivery{
		TaskID: "task-2", Attempt: 1, Branch: "b", RequiresPullRequest: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := rig.hooks.Complete(ctx, noPR); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no pull request: %v", err)
	}
}

func TestHooks_DiscardKeepsTheDeliveryAndTouchesNothing(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	// Nothing prepared: nothing to discard.
	if err := rig.hooks.Discard(ctx, job, false); err != nil {
		t.Fatalf("discard before prepare: %v", err)
	}
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := rig.hooks.Discard(ctx, job, false); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	// The delivery is the record of the attempt and survives the discard.
	if _, err := rig.store.GetDelivery(ctx, testTenant, testTask); err != nil {
		t.Fatalf("delivery gone after discard: %v", err)
	}
	if err := rig.hooks.Discard(ctx, testJob(2), true); err != nil {
		t.Fatalf("later attempt: %v", err)
	}
	if err := rig.hooks.Discard(ctx, nil, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil job: %v", err)
	}
	unbound := testJob(1)
	unbound.TaskID = ""
	if err := rig.hooks.Discard(ctx, unbound, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unbound job: %v", err)
	}
}

func TestHooks_BlockedPostsTheReasonAndLabels(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	rig.forge.postFailures = 1 // the first post fails; the retry lands
	reason := orchestration.BlockReason{Kind: orchestration.TaskBlockKindNeedsInfo, Message: "which database? token ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ", Iterations: 2}
	if err := rig.hooks.Blocked(ctx, job, reason); err != nil {
		t.Fatalf("Blocked: %v", err)
	}
	comments := rig.forge.commentsOn(25)
	if len(comments) != 1 {
		t.Fatalf("comments = %d", len(comments))
	}
	body := comments[0].Body
	for _, want := range []string{"The agent has a question", "which database?", "`needs_info`", "**Task:** `task-1`", "**Review rounds:** 2", "orchestrator task readmit task-1", "`task:ready` → `factory:needs-info`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("comment lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Fatal("a credential reached the issue")
	}
	if labels := rig.forge.issueLabels(25); strings.Join(labels, ",") != "bug,factory:needs-info" {
		t.Fatalf("labels after Blocked = %v", labels)
	}
	// The forge refusing every post is the hook failing.
	rig.forge.postFailures = 5
	err := rig.hooks.Blocked(ctx, job, orchestration.BlockReason{Kind: orchestration.TaskBlockKindEscalated})
	var undelivered *SurfacingUndeliveredError
	if !errors.As(err, &undelivered) || undelivered.Attempts != 2 {
		t.Fatalf("undelivered: %v", err)
	}
}

func TestHooks_ResolutionRefusals(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	if _, err := rig.hooks.Prepare(ctx, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil job: %v", err)
	}
	unbound := testJob(1)
	unbound.TaskID = ""
	if _, err := rig.hooks.Prepare(ctx, unbound); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unbound: %v", err)
	}
	noProject := testJob(1)
	noProject.ProjectID = "nope"
	if _, err := rig.hooks.Prepare(ctx, noProject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	noTask := testJob(1)
	noTask.TaskID = "nope"
	if _, err := rig.hooks.Prepare(ctx, noTask); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing task: %v", err)
	}
	rig.tasks.tasks[testTask].Source.Kind = "operator"
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); !errors.Is(err, ErrTaskNotForgeIssue) {
		t.Fatalf("operator task: %v", err)
	}
	rig.tasks.tasks[testTask].Source.Kind = TaskSourceKindForgeIssue
	zero := testJob(0)
	if _, err := rig.hooks.Prepare(ctx, zero); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("attempt zero: %v", err)
	}
	if err := rig.hooks.Blocked(ctx, unbound, orchestration.BlockReason{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blocked unbound: %v", err)
	}
	if err := rig.hooks.Complete(ctx, unbound); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("complete unbound: %v", err)
	}
}

func TestNewHooks_RefusesIncompleteWiring(t *testing.T) {
	complete := func() HooksConfig {
		return HooksConfig{
			Projects: &fakeProjects{}, Bindings: newTestStore(t), Deliveries: newTestStore(t), Tasks: newFakeTasks(),
			Forges: &fakeForges{},
		}
	}
	if _, err := NewHooks(complete()); err != nil {
		t.Fatalf("complete config refused: %v", err)
	}
	for name, mutate := range map[string]func(*HooksConfig){
		"projects":       func(c *HooksConfig) { c.Projects = nil },
		"bindings":       func(c *HooksConfig) { c.Bindings = nil },
		"deliveries":     func(c *HooksConfig) { c.Deliveries = nil },
		"tasks":          func(c *HooksConfig) { c.Tasks = nil },
		"forges":         func(c *HooksConfig) { c.Forges = nil },
		"merge attempts": func(c *HooksConfig) { c.MergeAttempts = -1 },
		"surfacing":      func(c *HooksConfig) { c.Surfacing.RetryAttempts = -1 },
	} {
		cfg := complete()
		mutate(&cfg)
		if _, err := NewHooks(cfg); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	hooks, _ := NewHooks(complete())
	if hooks.attempts != DefaultMergeAttempts || hooks.surfacing.RetryAttempts != DefaultSurfacePolicy.RetryAttempts {
		t.Fatal("defaults not applied")
	}
	if _, ok := hooks.waiter.(backoffWaiter); !ok {
		t.Fatal("default waiter")
	}
}

func TestBackoffWaiter(t *testing.T) {
	w := backoffWaiter{base: 1}
	if err := w.Wait(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := w.Wait(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (backoffWaiter{base: 1e9}).Wait(ctx, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v", err)
	}
}

// =============================================================================
// The branch plan is honoured, whole
// =============================================================================
//
// Before the Strategy seam, branchPlanFor overwrote Base AND Target with the
// project's default branch whenever it was set, and RequiresPullRequest and
// DeleteAfterMerge were declared and never read. These tests pin the fix: the
// project's default branch is a DEFAULT, and every field of the plan reaches
// the git and forge calls that act on it.

// TestHooks_PrepareKeepsTheRefsTheStrategyNamed inverts the old
// default-branch-override test. A strategy that cut from develop must still be
// on develop after Prepare, whatever the project's default branch is.
func TestHooks_PrepareKeepsTheRefsTheStrategyNamed(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	if err := rig.rebind(ctx, func(b *ProjectBinding) { b.Strategy = GitFlowName }); err != nil {
		t.Fatal(err)
	}
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	// The Delivery is where the plan's refs land now that the daemon
	// materialises nothing, so it is what proves the project's default
	// branch (main) did not overwrite what the strategy named.
	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Branch != "feature/25-fix-the-thing" {
		t.Fatalf("branch = %q, want the strategy's feature branch", delivery.Branch)
	}
	if delivery.Base != GitFlowDevelopBranch || delivery.Target != GitFlowDevelopBranch {
		t.Fatalf("delivery = %q → %q, want develop → develop", delivery.Base, delivery.Target)
	}
	if !delivery.RequiresPullRequest || !delivery.DeleteAfterMerge {
		t.Fatalf("the plan's review and lifecycle decisions were not recorded: %+v", delivery)
	}
}

// TestHooks_PrepareRoutesAHotfixToTheReleaseLine: the same binding, a
// different label, and the plan comes out with the project's default branch as
// its base — because gitflow left it open, not because anything overwrote it.
func TestHooks_PrepareRoutesAHotfixToTheReleaseLine(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	if err := rig.rebind(ctx, func(b *ProjectBinding) { b.Strategy = GitFlowName }); err != nil {
		t.Fatal(err)
	}
	rig.forge.putIssue(25, "Fix the thing", "task:ready", "bug", "hotfix")
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Branch != "hotfix/25-fix-the-thing" || delivery.Base != "main" {
		t.Fatalf("planned %q from %q, want hotfix/25-fix-the-thing from main", delivery.Branch, delivery.Base)
	}
}

// TestHooks_ReviewableTargetsTheRefTheStrategyChose: with base and target
// apart, the pull request must target the TARGET while the ahead-check reads
// the BASE. Conflating them is what the single-ref override forced.
func TestHooks_ReviewableTargetsTheRefTheStrategyChose(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	job.WorkDir = containerWorkDir(t)
	// A delivery whose base and target differ, as a release-line hotfix's do.
	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	delivery.Base = "release-1.x"
	delivery.Target = "main"
	if err := rig.store.UpdateDelivery(ctx, testTenant, delivery); err != nil {
		t.Fatal(err)
	}
	rig.publishHead(t, ctx, "head-1")
	if err := rig.hooks.Reviewable(ctx, job); err != nil {
		t.Fatalf("Reviewable: %v", err)
	}
	pr := rig.forge.pullRequest(100)
	if pr == nil || pr.BaseRef != "main" {
		t.Fatalf("pull request targets %v, want main", pr)
	}
}

// TestHooks_ReviewableCarveOutPushesToTheTargetWithoutAPullRequest is the
// trunk-based direct-push path. A change class the strategy exempted still
// works on its own signed branch; what the carve-out removes is the pull
// request and its review round.
func TestHooks_ReviewableCarveOutPushesToTheTargetWithoutAPullRequest(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	if err := rig.rebind(ctx, func(b *ProjectBinding) { b.Strategy = TrunkBasedName }); err != nil {
		t.Fatal(err)
	}
	rig.forge.putIssue(25, "Fix the thing", "task:ready", "docs")
	job := testJob(1)
	if _, err := rig.hooks.Prepare(ctx, job); err != nil {
		t.Fatal(err)
	}
	job.WorkDir = containerWorkDir(t)
	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.RequiresPullRequest {
		t.Fatal("a docs change under trunk-based was planned for review")
	}
	rig.publishHead(t, ctx, "head-1")
	if err := rig.hooks.Reviewable(ctx, job); err != nil {
		t.Fatalf("Reviewable: %v", err)
	}
	if len(rig.forge.prs) != 0 {
		t.Errorf("a carve-out opened a pull request: %v", rig.forge.prs)
	}
	if labels := rig.forge.issueLabels(25); strings.Join(labels, ",") != "docs,factory:merged" {
		t.Errorf("labels = %v, want the change shown as merged", labels)
	}
	// The head is published; no pull-request artifact is, because there is
	// no pull request to open.
	artifacts := rig.tasks.submitted(testTask)
	wantHead := orchestration.ArtifactNameForAttempt(orchestration.ArtifactNameHead, job.Attempt)
	if len(artifacts) != 1 || artifacts[0].Name != wantHead || artifacts[0].Ref.SHA == "" {
		t.Fatalf("artifacts = %+v, want only the head", artifacts)
	}
	// Complete has nothing to land: the carve-out already landed it.
	if err := rig.hooks.Complete(ctx, job); err != nil {
		t.Fatalf("Complete on a carve-out: %v", err)
	}
	if len(rig.forge.merges) != 0 {
		t.Errorf("Complete merged something for a change that never had a pull request")
	}
}

// TestHooks_CompleteCarriesTheBranchLifecycleFromThePlan: DeleteAfterMerge is
// decided once, when the branch is cut, and the merge spec only carries it to
// the forge. A strategy deciding it twice is how the two halves drift.
func TestHooks_CompleteCarriesTheBranchLifecycleFromThePlan(t *testing.T) {
	for _, deleteAfterMerge := range []bool{true, false} {
		t.Run(map[bool]string{true: "deleted", false: "kept"}[deleteAfterMerge], func(t *testing.T) {
			rig := newHooksRig(t)
			ctx := context.Background()
			job := testJob(1)
			if _, err := rig.hooks.Prepare(ctx, job); err != nil {
				t.Fatal(err)
			}
			rig.publishHead(t, ctx, testPublishedHead)
			if err := rig.hooks.Reviewable(ctx, job); err != nil {
				t.Fatal(err)
			}
			delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
			if err != nil {
				t.Fatal(err)
			}
			delivery.DeleteAfterMerge = deleteAfterMerge
			if err := rig.store.UpdateDelivery(ctx, testTenant, delivery); err != nil {
				t.Fatal(err)
			}
			if err := rig.hooks.Complete(ctx, job); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if len(rig.forge.merges) != 1 {
				t.Fatalf("merges = %d, want one", len(rig.forge.merges))
			}
			if got := rig.forge.merges[0].spec.ExpectSourceDeleted; got != deleteAfterMerge {
				t.Errorf("merge asked ExpectSourceDeleted=%v, want the plan's %v", got, deleteAfterMerge)
			}
		})
	}
}

// TestHooks_ABindingNamingAnUnregisteredStrategyNeverReachesTheHooks: the
// refusal is fail-closed at the store, before any git or forge work could
// happen under an SDLC nobody implements, and it names what IS available.
func TestHooks_ABindingNamingAnUnregisteredStrategyNeverReachesTheHooks(t *testing.T) {
	rig := newHooksRig(t)
	ctx := context.Background()
	err := rig.rebind(ctx, func(b *ProjectBinding) { b.Strategy = "no-such-sdlc" })
	if !errors.Is(err, ErrStrategyNotRegistered) {
		t.Fatalf("PutBinding err = %v, want ErrStrategyNotRegistered", err)
	}
	if !strings.Contains(err.Error(), GitHubFlowName) {
		t.Errorf("refusal %q does not name a strategy the operator could choose", err.Error())
	}
	// The stored binding is untouched, so the hooks still run the SDLC the
	// project actually selected.
	if _, err := rig.hooks.Prepare(ctx, testJob(1)); err != nil {
		t.Fatalf("Prepare under the surviving binding: %v", err)
	}
	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if delivery.Branch != "issue/25-fix-the-thing" {
		t.Errorf("branch = %q", delivery.Branch)
	}
}
