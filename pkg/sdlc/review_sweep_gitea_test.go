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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
)

// =============================================================================
// The floor, end to end, against a forge with no webhook at all
// =============================================================================
//
// THIS IS THE BURN-IN INCIDENT AS A TEST. A repository with zero webhooks
// provisioned delivers nothing, so the whole delivery path is absent rather
// than merely slow; two finished pull requests sat green and unmergeable
// because the status branch protection requires was never written. Every part
// of the chain is real here — the Gitea adapter, its HTTP transport, its
// pagination, the dispatcher, the check evaluation — and only the engine is a
// double, because starting a job is what is being counted.

// backendForges resolves every binding to one already-built adapter, which is
// what lets a test drive the REAL forge adapter through the sweeper.
type backendForges struct{ backend ForgeBackend }

func (f *backendForges) ForgeFor(context.Context, string, ForgeConfig) (ForgeBackend, error) {
	return f.backend, nil
}

// ciContext is the pipeline's own status name on this fleet, unrelated to the
// reviewer's.
const ciContext = "CI / CI Gate (pull_request)"

// floorRig is the whole review path over a stateful Gitea: the real adapter,
// the real dispatcher, and a fake engine that records what was started.
type floorRig struct {
	gitea   *giteafake.Server
	backend *GiteaBackend
	sweeper *ReviewSweeper
	jobs    *fakeJobStarter
	repo    RepoRef
}

func newFloorRig(t *testing.T) *floorRig {
	t.Helper()
	backend, gitea := newStatefulGitea(t)

	store := newTestStore(t)
	binding := testBinding()
	binding.Forge.Owner, binding.Forge.Repo = testRepo.Owner, testRepo.Repo
	binding.Forge.BaseURL = gitea.APIURL()
	binding.Reviewer = ReviewerConfig{Backend: "codex", Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatal(err)
	}

	jobs := &fakeJobStarter{}
	forges := &backendForges{backend: backend}
	dispatcher, err := NewReviewDispatcher(store, jobs, forges)
	if err != nil {
		t.Fatal(err)
	}
	sweeper, err := NewReviewSweeper(ReviewSweeperConfig{Bindings: store, Forges: forges, Reviews: dispatcher})
	if err != nil {
		t.Fatal(err)
	}
	return &floorRig{gitea: gitea, backend: backend, sweeper: sweeper, jobs: jobs, repo: testRepo}
}

// pullRequests reads the pull request numbers the engine was asked to review.
func (r *floorRig) pullRequests(t *testing.T) []int {
	t.Helper()
	numbers := make([]int, 0, len(r.jobs.requests()))
	for _, req := range r.jobs.requests() {
		subject, err := ReviewJobSubjectFrom(&orchestration.JobConfig{Params: req.Params})
		if err != nil {
			t.Fatalf("a started job carries no readable subject: %v", err)
		}
		if req.Runner != ReviewJobRunnerName {
			t.Fatalf("runner = %q", req.Runner)
		}
		numbers = append(numbers, subject.PullRequest)
	}
	return numbers
}

// TestReviewFloor_ReviewsGreenPullRequestsWithNoWebhookAtAll is go-quicbaas#933
// end to end: with no delivery path in existence, the accumulated backlog is
// reviewed from the sweep alone, and nothing has to re-run CI to be noticed.
func TestReviewFloor_ReviewsGreenPullRequestsWithNoWebhookAtAll(t *testing.T) {
	rig := newFloorRig(t)
	if hooks := rig.gitea.HookURLs(); len(hooks) != 0 {
		t.Fatalf("the fixture must model a repository with NO webhook, got %v", hooks)
	}

	// Two finished pull requests, green for some time, nobody told about
	// either — the burn-in node's #64 and #65.
	rig.gitea.PutPullRequest(64, "issue/64-a", "aaaa111122223333444455556666777788889999")
	rig.gitea.PutStatus("aaaa111122223333444455556666777788889999", ciContext, "success")
	rig.gitea.PutPullRequest(65, "issue/65-b", "bbbb111122223333444455556666777788889999")
	rig.gitea.PutStatus("bbbb111122223333444455556666777788889999", ciContext, "success")

	started, err := rig.sweeper.Sweep(context.Background(), &fakeTenants{ids: []string{testTenant}})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if failures := rig.sweeper.SweepFailures(); len(failures) != 0 {
		t.Fatalf("the pass deferred failures: %v", failures)
	}
	if started != 2 {
		t.Fatalf("started %d reviews, want 2", started)
	}
	if got := rig.pullRequests(t); len(got) != 2 || got[0] != 64 || got[1] != 65 {
		t.Fatalf("reviewed %v, want [64 65]", got)
	}
	// The engine guards StartJob on the tenant bound to the CONTEXT.
	for _, tenant := range rig.jobs.contextTenants() {
		if tenant != testTenant {
			t.Fatalf("a review job was started with context tenant %q", tenant)
		}
	}
}

// TestReviewFloor_StopsOnceTheVerdictIsOnTheHead proves the pass is not a
// job generator: once the review job has published under the reviewer's
// context — which it does on every ending it has — the same pull request is
// passed over on every later pass, with no record of this node's own.
func TestReviewFloor_StopsOnceTheVerdictIsOnTheHead(t *testing.T) {
	rig := newFloorRig(t)
	const head = "cccc111122223333444455556666777788889999"
	rig.gitea.PutPullRequest(64, "issue/64-a", head)
	rig.gitea.PutStatus(head, ciContext, "success")

	ctx := context.Background()
	if started, err := rig.sweeper.SweepProject(ctx, testTenant, testProject); err != nil || started != 1 {
		t.Fatalf("first pass: started=%d err=%v", started, err)
	}

	// What the review job does at the end of its run, through the same
	// adapter, verified by the adapter's own read-back.
	if err := rig.backend.SetCommitStatus(ctx, rig.repo, head, CommitStatus{
		Context: DefaultReviewStatusContext, Status: CheckSuccess, Description: "approved",
	}); err != nil {
		t.Fatalf("SetCommitStatus: %v", err)
	}
	if got := rig.gitea.StatusContexts(head); len(got) != 2 {
		t.Fatalf("the head carries %v, want the pipeline's and the reviewer's", got)
	}

	if started, err := rig.sweeper.SweepProject(ctx, testTenant, testProject); err != nil || started != 0 {
		t.Fatalf("a judged head was reviewed again: started=%d err=%v", started, err)
	}
	if got := rig.pullRequests(t); len(got) != 1 {
		t.Fatalf("started %d review jobs across two passes, want 1", len(got))
	}
}

// TestReviewFloor_LeavesAlonePullRequestsThePipelineHasNotFinished proves the
// floor is bounded by the same gate the delivery path is: a red head and a
// head still running are both passed over, and a closed pull request is never
// even listed.
func TestReviewFloor_LeavesAlonePullRequestsThePipelineHasNotFinished(t *testing.T) {
	rig := newFloorRig(t)
	rig.gitea.PutPullRequest(70, "issue/70-red", "dddd1111")
	rig.gitea.PutStatus("dddd1111", ciContext, "failure")
	rig.gitea.PutPullRequest(71, "issue/71-running", "eeee1111")
	rig.gitea.PutStatus("eeee1111", ciContext, "pending")
	rig.gitea.PutPullRequestRaw(giteafake.PullRequest{
		Number: 72, State: "closed",
		Head: giteafake.Commit{Ref: "issue/72-done", SHA: "ffff1111"},
		Base: giteafake.Commit{Ref: "main"},
	})
	rig.gitea.PutStatus("ffff1111", ciContext, "success")

	started, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if started != 0 {
		t.Fatalf("started %d reviews, want none", started)
	}
	if got := rig.gitea.Calls("GET /commits/ffff1111/statuses"); got != 0 {
		t.Fatalf("a closed pull request cost %d status reads", got)
	}
}

// TestReviewFloor_CostsOneListingAndOneStatusReadPerOpenPullRequest pins the
// budget against the REAL adapter, where the paging and the routes are what
// they are in production rather than what a double was told to do.
func TestReviewFloor_CostsOneListingAndOneStatusReadPerOpenPullRequest(t *testing.T) {
	rig := newFloorRig(t)
	heads := []string{"1111aaaa", "2222aaaa", "3333aaaa"}
	for i, head := range heads {
		rig.gitea.PutPullRequest(80+i, "issue/"+head, head)
		rig.gitea.PutStatus(head, ciContext, "success")
		// Two of the three have already been judged, so only one is a
		// candidate — and the judged ones must still cost exactly one read.
		if i > 0 {
			rig.gitea.PutStatus(head, DefaultReviewStatusContext, "success")
		}
	}

	if _, err := rig.sweeper.SweepProject(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if got := rig.gitea.Calls("GET /pulls"); got != 1 {
		t.Fatalf("listed the open pull requests %d times, want 1", got)
	}
	// The one candidate is re-read by the dispatcher before its job starts;
	// the two judged ones are not read at all.
	if got := rig.gitea.Calls("GET /pulls/80"); got != 1 {
		t.Fatalf("the candidate was re-read %d times, want 1", got)
	}
	for _, judged := range []string{"GET /pulls/81", "GET /pulls/82"} {
		if got := rig.gitea.Calls(judged); got != 0 {
			t.Fatalf("%s cost %d reads, want none", judged, got)
		}
	}
	for _, head := range heads {
		// One read from the pass. The candidate's head is read a second time
		// by the dispatcher, which re-reads the forge before it starts a job
		// — see ReviewSweeper.sweepBinding.
		want := 1
		if head == heads[0] {
			want = 2
		}
		if got := rig.gitea.Calls("GET /commits/" + head + "/statuses"); got != want {
			t.Fatalf("head %s was read %d times, want %d", head, got, want)
		}
	}
}
