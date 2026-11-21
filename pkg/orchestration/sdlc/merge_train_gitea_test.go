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
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// =============================================================================
// What the merge train asks of Gitea
// =============================================================================

const closePRPath = "/api/v1/repos/jeremyhahn/go-quicbaas/pulls/41"

// respondPullStates answers the pull request's GET with each state in turn,
// the last one for every read after.
func respondPullStates(fixture *giteaFixture, states ...string) {
	reads := 0
	fixture.handlers["GET "+closePRPath] = func(w http.ResponseWriter, _ *http.Request) {
		state := states[len(states)-1]
		if reads < len(states) {
			state = states[reads]
		}
		reads++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pullPayload(41, "queue/3", "main", strings.Repeat("a", 40), state, false))
	}
}

func TestClosePullRequest_ClosesThroughThePullsRouteAndReadsBack(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	respondPullStates(fixture, "open", "closed")
	fixture.respondJSON("PATCH "+closePRPath, http.StatusCreated, map[string]any{"number": 41, "state": "closed"})

	if err := backend.ClosePullRequest(context.Background(), testRepo, 41); err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	var patched *recordedRequest
	for i := range fixture.requests {
		if fixture.requests[i].Method == http.MethodPatch {
			patched = &fixture.requests[i]
		}
	}
	if patched == nil || patched.Path != closePRPath || !strings.Contains(patched.Body, `"state":"closed"`) {
		t.Fatalf("patch = %+v, want the pull request's state closed", patched)
	}
}

func TestClosePullRequest_AClosedPullRequestIsLeftAlone(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	respondPullStates(fixture, "closed")
	if err := backend.ClosePullRequest(context.Background(), testRepo, 41); err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	for _, req := range fixture.requests {
		if req.Method == http.MethodPatch {
			t.Fatal("a closed pull request was written to")
		}
	}
}

func TestClosePullRequest_AWriteThatDidNotLandIsReported(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	respondPullStates(fixture, "open")
	fixture.respondJSON("PATCH "+closePRPath, http.StatusCreated, map[string]any{"number": 41, "state": "open"})
	err := backend.ClosePullRequest(context.Background(), testRepo, 41)
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("ClosePullRequest = %v, want a write-not-applied", err)
	}
}

func TestClosePullRequest_RefusesANumberThatNamesNothing(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	if err := backend.ClosePullRequest(context.Background(), testRepo, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ClosePullRequest(0) = %v, want invalid input", err)
	}
}

func TestGiteaCapabilities_HasNoNativeMergeQueue(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	if backend.Capabilities().NativeMergeQueue {
		t.Fatal("Gitea declared a merge queue it does not have")
	}
}

// awaitTrain records train 3 on the ledger, in phase, carrying two changes
// and landing both through pull request 41 at head.
func (r *reviewRunnerRig) awaitTrain(phase TrainPhase, head string) {
	r.t.Helper()
	ctx := context.Background()
	ledger, err := r.store.GetTrainLedger(ctx, testTenant, testProject)
	if err != nil {
		r.t.Fatal(err)
	}
	train := &MergeTrain{Number: 3, Target: "main", BaseSHA: "base-sha", Phase: phase, Land: 2, Good: 2}
	for _, pr := range []int{101, 102} {
		train.Cars = append(train.Cars, TrainCar{
			TaskID: "task-" + strconv.Itoa(pr), PullRequest: pr, Title: "change " + strconv.Itoa(pr),
			Branch: "issue/" + strconv.Itoa(pr-100), HeadSHA: "head-" + strconv.Itoa(pr), Closes: []int{pr - 100},
		})
	}
	train.Probes = []TrainProbe{{Length: 2, Branch: TrainBranch(3), PullRequest: 41, HeadSHA: head, Verdict: probeGreen}}
	ledger.Next, ledger.Active = 4, train
	if err := r.store.SaveTrainLedger(ctx, testTenant, ledger); err != nil {
		r.t.Fatal(err)
	}
	r.forge.prs[41].HeadRef = TrainBranch(3)
	r.forge.prs[41].HeadRepo = testRepoRef
}

// TestReviewRunner_AnAwaitedTrainIsReviewedAsATrain proves the owner's ruling
// from the reviewer's side: the train's pull request is reviewed like any
// other, and the reviewer is told it is a train — which changes it carries,
// that each was approved on its own head, and that how they combine is what
// is left to judge.
func TestReviewRunner_AnAwaitedTrainIsReviewedAsATrain(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.awaitTrain(TrainReviewing, reviewHead)
	rig.reviewerSays(reviewSaying("The two changes compose.", ReviewCodeApprove))
	rig.mustRun(41)

	submissions := rig.forge.submissions()
	if len(submissions) != 1 || submissions[0].Verdict != VerdictApprove || submissions[0].HeadSHA != reviewHead {
		t.Fatalf("submissions = %+v, want one approval of the train's head", submissions)
	}
	if status, ok := rig.forge.lastStatus(); !ok || status.Status != CheckSuccess || status.Context != DefaultReviewStatusContext {
		t.Fatalf("status = %+v, want the reviewer's own success under the required context", status)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 1 {
		t.Fatalf("prompts = %d, want one", len(prompts))
	}
	for _, want := range []string{
		"merge train #3", "- #101 change 101 (branch issue/1), approved at `head-101`", "- #102 change 102",
		"onto `main` at `base-sha`", "#1, #2", "Those approvals\nstand", "how the changes interact",
	} {
		if !strings.Contains(prompts[0], want) {
			t.Fatalf("the train prompt lacks %q:\n%s", want, prompts[0])
		}
	}
	if strings.Contains(prompts[0], "The issue this change must satisfy") {
		t.Fatalf("a train was reviewed against an issue it does not have:\n%s", prompts[0])
	}
}

// TestReviewRunner_ATrainItsTrainIsNotWaitingOnIsNotReviewed proves only the
// prefix the train decided to land is reviewed: a train pull request whose
// train is still testing, or has ended, spends no reviewer and writes no
// status.
func TestReviewRunner_ATrainItsTrainIsNotWaitingOnIsNotReviewed(t *testing.T) {
	cases := map[string]func(*reviewRunnerRig){
		"no train in flight": func(r *reviewRunnerRig) {
			r.forge.prs[41].HeadRef = TrainBranch(3)
			r.forge.prs[41].HeadRepo = testRepoRef
		},
		"train still testing":     func(r *reviewRunnerRig) { r.awaitTrain(TrainTesting, reviewHead) },
		"train awaits other head": func(r *reviewRunnerRig) { r.awaitTrain(TrainReviewing, "another-head") },
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			rig := newReviewRunnerRig(t)
			arrange(rig)
			rig.reviewerSays(reviewSaying("looks fine", ReviewCodeApprove))
			rig.mustRun(41)
			if rig.agents.sessionCount() != 0 {
				t.Fatal("a reviewer was spawned for a train that is not waiting on one")
			}
			if got := rig.forge.submissions(); len(got) != 0 {
				t.Fatalf("a train that is not waiting was reviewed: %+v", got)
			}
			if _, ok := rig.forge.lastStatus(); ok {
				t.Fatal("a status was written on a train nobody is waiting on")
			}
		})
	}
}

// TestReviewRunner_ATrainIsReviewedOnlyByItsTrainReviewer proves a train's
// verdict comes from the profile the train reads: a job started under another
// profile — here the advisory default of a project whose counting reviewer is
// an alternate — skips it.
func TestReviewRunner_ATrainIsReviewedOnlyByItsTrainReviewer(t *testing.T) {
	rig := newReviewRunnerRig(t)
	no, yes := false, true
	rig.rebind(func(b *ProjectBinding) {
		b.Reviewer.CountsTowardMerge = &no
		b.Reviewers = []ReviewerConfig{{Name: "gate", Agent: testReviewerAgent, Login: "gatekeeper",
			TokenSecretRef: "gate-tok", CountsTowardMerge: &yes}}
		b.Review.Reviewers["gatekeeper"] = ModelFamilyOpenAI
	})
	rig.awaitTrain(TrainReviewing, reviewHead)
	rig.reviewerSays(reviewSaying("looks fine", ReviewCodeApprove))
	rig.mustRun(41)
	if rig.agents.sessionCount() != 0 || len(rig.forge.submissions()) != 0 {
		t.Fatal("an advisory profile reviewed a merge train")
	}
}

// TestReviewRunner_ATrainLedgerThatCannotBeReadFailsTheReview proves the
// runner does not guess whether a train is waiting: an unreadable ledger fails
// the job for the engine's retry.
func TestReviewRunner_ATrainLedgerThatCannotBeReadFailsTheReview(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.awaitTrain(TrainReviewing, reviewHead)
	runner, err := NewReviewRunner(ReviewRunnerConfig{Agents: testAgentCatalog(),
		Bindings: rig.store, Forges: rig.forges, Trains: failingTrainLedger{Store: rig.store}, Tokens: rig.tokens,
		Signing: rig.signing, Guard: testRepoGuard(t), SocketsRoot: testSocketsRoot(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(context.Background(), rig.job(41)); !errors.Is(err, ErrBackend) {
		t.Fatalf("Run = %v, want the ledger's failure", err)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("a reviewer was spawned without knowing whether the train waits on it")
	}
}

// failingTrainLedger is a train store whose ledger cannot be read.
type failingTrainLedger struct{ *Store }

func (failingTrainLedger) GetTrainLedger(context.Context, string, string) (*TrainLedger, error) {
	return nil, backendError("GetTrainLedger", errors.New("store unavailable"))
}

// TestReviewRunner_AForkBranchNamedLikeATrainIsStillReviewed proves the skip
// is for the train's OWN branch: a fork's branch of the same name is somebody
// else's change.
func TestReviewRunner_AForkBranchNamedLikeATrainIsStillReviewed(t *testing.T) {
	rig := newReviewRunnerRig(t)
	rig.forge.prs[41].HeadRef = TrainBranch(3)
	rig.forge.prs[41].HeadRepo = RepoRef{Owner: "somebody", Repo: "fork"}
	rig.reviewerSays(reviewSaying("looks fine", ReviewCodeApprove))
	rig.mustRun(41)
	if got := rig.forge.submissions(); len(got) != 1 {
		t.Fatalf("a fork's change was not reviewed: %+v", got)
	}
}
