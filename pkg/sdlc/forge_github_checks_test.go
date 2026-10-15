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
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	githubCommitPath = "/repos/acme/widgets/commits/" + githubHeadSHA
	// Check run 502 is Actions job 7702: GitHub does not promise the two
	// share an id, so the fixtures give them different ones.
	githubCheckRunPath = "/repos/acme/widgets/check-runs/502"
	githubRunsPath     = "/repos/acme/widgets/actions/runs"
	githubRunPath      = githubRunsPath + "/9100"
	githubJobPath      = "/repos/acme/widgets/actions/jobs/7702"
)

// githubJobServed answers the reads that resolve check run 502 to its
// Actions job: the check run, the workflow run of its check suite, and that
// run's jobs.
func githubJobServed(stub *forgeStub) {
	stub.fixture("GET "+githubCheckRunPath, http.StatusOK, "check_run.json")
	stub.fixture("GET "+githubRunsPath, http.StatusOK, "actions_runs.json")
	stub.fixture("GET "+githubRunPath+"/jobs", http.StatusOK, "actions_run_jobs.json")
}

// githubChecksServed answers the head's check runs and statuses from the
// fixtures.
func githubChecksServed(stub *forgeStub) {
	stub.fixture("GET "+githubCommitPath+"/check-runs", http.StatusOK, "check_runs.json")
	stub.fixture("GET "+githubCommitPath+"/statuses", http.StatusOK, "statuses.json")
}

// githubActionsRunAt is a workflow run read.
func githubActionsRunAt(attempt int, status string) map[string]any {
	return map[string]any{"id": 9100, "run_attempt": attempt, "status": status}
}

// githubFailedUnit is the fixture's red Actions check run.
var githubFailedUnit = ForgeCheckRun{ID: "502", Name: "unit", HeadSHA: githubHeadSHA}

// =============================================================================
// Reviews and the gate
// =============================================================================

// TestGitHubBackend_ListReviews_FeedTheGate proves GitHub's review states
// reach the gate as the verdicts Gitea's do: an approval approves, a change
// request blocks, and a comment or a dismissed review decides nothing.
func TestGitHubBackend_ListReviews_FeedTheGate(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.fixture("GET "+githubPullPath+"/reviews", http.StatusOK, "reviews.json")

	reviews, err := backend.ListReviews(context.Background(), githubRepo, 12)
	if err != nil || len(reviews) != 4 {
		t.Fatalf("ListReviews = %+v, %v", reviews, err)
	}
	want := map[string]struct {
		acted   bool
		verdict ReviewVerdict
	}{
		"reviewer-approves":  {true, VerdictApprove},
		"reviewer-objects":   {true, VerdictRequestChanges},
		"reviewer-remarks":   {false, ""},
		"reviewer-dismissed": {false, ""},
	}
	for _, review := range reviews {
		record, acted := reviewRecordFor(review, ReviewPolicy{}, 1)
		expect := want[review.Reviewer]
		if acted != expect.acted || record.Verdict != expect.verdict {
			t.Errorf("%s (%s) = %v %q, want %v %q", review.Reviewer, review.State, acted, record.Verdict, expect.acted, expect.verdict)
		}
		if acted && record.HeadSHA != githubHeadSHA {
			t.Errorf("%s bound to %q, want the reviewed head", review.Reviewer, record.HeadSHA)
		}
	}

	stub.json("GET "+githubPullPath+"/reviews", http.StatusForbidden, nil)
	if _, err := backend.ListReviews(context.Background(), githubRepo, 12); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("err = %v, want the 403", err)
	}
}

// TestGitHubBackend_SubmitReview binds the verdict to the head it was formed
// against, and fills a head GitHub did not echo.
func TestGitHubBackend_SubmitReview(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("POST "+githubPullPath+"/reviews", http.StatusOK,
		map[string]any{"id": 900, "state": "CHANGES_REQUESTED", "user": map[string]string{"login": "agent-reviewer"}})

	review, err := backend.SubmitReview(context.Background(), githubRepo, 12,
		ReviewSubmission{Verdict: VerdictRequestChanges, Body: "fix the retry", HeadSHA: githubHeadSHA})
	if err != nil || review.State != "REQUEST_CHANGES" || review.HeadSHA != githubHeadSHA {
		t.Fatalf("SubmitReview = %+v, %v", review, err)
	}
	body := decodeBody(t, stub.requests("POST " + githubPullPath + "/reviews")[0])
	if body["event"] != "REQUEST_CHANGES" || body["commit_id"] != githubHeadSHA {
		t.Errorf("body = %v", body)
	}
}

// TestGitHubBackend_SubmitReview_Refusals covers an invalid submission, a
// verdict stored as something else, one bound to another head, and a refusal.
func TestGitHubBackend_SubmitReview_Refusals(t *testing.T) {
	ctx := context.Background()
	approve := ReviewSubmission{Verdict: VerdictApprove, Body: "lgtm", HeadSHA: githubHeadSHA}
	backend, stub := newGitHubStub(t)

	if _, err := backend.SubmitReview(ctx, githubRepo, 12, ReviewSubmission{Verdict: VerdictApprove}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("invalid err = %v, want ErrInvalidInput", err)
	}
	stub.json("POST "+githubPullPath+"/reviews", http.StatusOK, map[string]any{"id": 900, "state": "PENDING"})
	if _, err := backend.SubmitReview(ctx, githubRepo, 12, approve); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("pending err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("POST "+githubPullPath+"/reviews", http.StatusOK,
		map[string]any{"id": 900, "state": "APPROVED", "commit_id": githubMovedSHA})
	if _, err := backend.SubmitReview(ctx, githubRepo, 12, approve); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("other head err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("POST "+githubPullPath+"/reviews", http.StatusUnprocessableEntity,
		map[string]string{"message": "Can not approve your own pull request"})
	if _, err := backend.SubmitReview(ctx, githubRepo, 12, approve); forgeStatusOf(err) != http.StatusUnprocessableEntity {
		t.Errorf("refused err = %v, want the 422", err)
	}
}

// =============================================================================
// Check runs, statuses and the gate
// =============================================================================

// TestGitHubBackend_ListCheckRuns_FeedTheGate proves both of GitHub's CI
// channels reach the gate, with every conclusion folded to the one GitHub's
// own branch protection would apply.
func TestGitHubBackend_ListCheckRuns_FeedTheGate(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubChecksServed(stub)

	runs, err := backend.ListCheckRuns(context.Background(), githubRepo, githubHeadSHA)
	if err != nil {
		t.Fatalf("ListCheckRuns: %v", err)
	}
	want := map[string]CheckStatus{
		"build":          CheckSuccess,
		"unit":           CheckFailure,
		"integration":    CheckFailure,
		"docs":           CheckSkipped,
		"deploy-preview": CheckCancelled,
		"lint":           CheckPending,
		"factory/review": CheckSuccess,
		"legacy-ci":      CheckFailure,
		"slow-ci":        CheckPending,
	}
	if len(runs) != len(want) {
		t.Fatalf("runs = %d, want %d", len(runs), len(want))
	}
	for _, run := range runs {
		if got := checkStatusFor(run); got != want[run.Name] {
			t.Errorf("%s = %v, want %v", run.Name, got, want[run.Name])
		}
		if run.HeadSHA != githubHeadSHA {
			t.Errorf("%s head = %q", run.Name, run.HeadSHA)
		}
		if isStatus := strings.HasPrefix(run.ID, statusRunPrefix); isStatus != strings.Contains("factory/review legacy-ci slow-ci", run.Name) {
			t.Errorf("%s id = %q, want a status id only for a commit status", run.Name, run.ID)
		}
	}

	policy := ReviewPolicy{RequireGreenChecks: true}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	if reason, _ := ChecksGreenForReview(policy, githubHeadSHA, runs, now); reason != GateBlockedChecksIncomplete {
		t.Errorf("gate = %v, want blocked on the unfinished lint", reason)
	}
}

// TestGitHubBackend_ListCheckRuns_GateOutcomes proves a head with no CI is
// the missing-check refusal, a red check blocks, and green clears.
func TestGitHubBackend_ListCheckRuns_GateOutcomes(t *testing.T) {
	policy := ReviewPolicy{RequireGreenChecks: true}
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		checkRuns []map[string]any
		statuses  []map[string]any
		want      GateBlockReason
	}{
		"missing check": {want: GateBlockedNoChecks},
		"red status": {
			checkRuns: []map[string]any{{"id": 1, "name": "build", "status": "completed", "conclusion": "success"}},
			statuses:  []map[string]any{{"id": 2, "context": "legacy-ci", "state": "error"}},
			want:      GateBlockedChecksFailed,
		},
		"green": {
			checkRuns: []map[string]any{{"id": 1, "name": "build", "status": "completed", "conclusion": "success"}},
			statuses:  []map[string]any{{"id": 2, "context": "docs", "state": "success"}},
			want:      GateClear,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitHubStub(t)
			stub.json("GET "+githubCommitPath+"/check-runs", http.StatusOK, map[string]any{"check_runs": tc.checkRuns})
			stub.json("GET "+githubCommitPath+"/statuses", http.StatusOK, tc.statuses)

			runs, err := backend.ListCheckRuns(context.Background(), githubRepo, githubHeadSHA)
			if err != nil {
				t.Fatalf("ListCheckRuns: %v", err)
			}
			if reason, _ := ChecksGreenForReview(policy, githubHeadSHA, runs, now); reason != tc.want {
				t.Errorf("gate = %v, want %v", reason, tc.want)
			}
		})
	}
}

// TestGitHubBackend_ListCheckRuns_Refusals proves neither channel failing can
// pass for an empty one.
func TestGitHubBackend_ListCheckRuns_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	if _, err := backend.ListCheckRuns(context.Background(), githubRepo, ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no sha err = %v, want ErrInvalidInput", err)
	}
	stub.json("GET "+githubCommitPath+"/check-runs", http.StatusForbidden, nil)
	if _, err := backend.ListCheckRuns(context.Background(), githubRepo, githubHeadSHA); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("check runs err = %v, want the 403", err)
	}
	stub.fixture("GET "+githubCommitPath+"/check-runs", http.StatusOK, "check_runs.json")
	stub.json("GET "+githubCommitPath+"/statuses", http.StatusForbidden, nil)
	if _, err := backend.ListCheckRuns(context.Background(), githubRepo, githubHeadSHA); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("statuses err = %v, want the 403", err)
	}
}

// =============================================================================
// SetCommitStatus
// =============================================================================

// TestGitHubBackend_SetCommitStatus publishes and reads the status back.
func TestGitHubBackend_SetCommitStatus(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubChecksServed(stub)
	stub.json("POST /repos/acme/widgets/statuses/"+githubHeadSHA, http.StatusCreated, nil)

	err := backend.SetCommitStatus(context.Background(), githubRepo, githubHeadSHA, CommitStatus{
		Context: "factory/review", Status: CheckSuccess, Description: "approved", TargetURL: "https://factory.example/r/1",
	})
	if err != nil {
		t.Fatalf("SetCommitStatus: %v", err)
	}
	body := decodeBody(t, stub.requests("POST /repos/acme/widgets/statuses/" + githubHeadSHA)[0])
	if body["state"] != "success" || body["context"] != "factory/review" ||
		body["description"] != "approved" || body["target_url"] != "https://factory.example/r/1" {
		t.Errorf("body = %v", body)
	}
}

// TestGitHubBackend_SetCommitStatus_Refusals covers bad input, a state GitHub
// cannot publish, a refusal, a failed read-back, and a status that did not
// land — including one a check run of the same name would have hidden.
func TestGitHubBackend_SetCommitStatus_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitHubStub(t)
	if err := backend.SetCommitStatus(ctx, githubRepo, " ", CommitStatus{Context: "c", Status: CheckSuccess}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no sha err = %v", err)
	}
	if err := backend.SetCommitStatus(ctx, githubRepo, githubHeadSHA, CommitStatus{Status: CheckSuccess}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no context err = %v", err)
	}
	if err := backend.SetCommitStatus(ctx, githubRepo, githubHeadSHA, CommitStatus{Context: "c", Status: CheckCancelled}); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("cancelled err = %v, want ErrForgeUnsupported", err)
	}

	statusPath := "POST /repos/acme/widgets/statuses/" + githubHeadSHA
	stub.json(statusPath, http.StatusForbidden, nil)
	if err := backend.SetCommitStatus(ctx, githubRepo, githubHeadSHA, CommitStatus{Context: "c", Status: CheckSuccess}); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v", err)
	}

	stub.json(statusPath, http.StatusCreated, nil)
	if err := backend.SetCommitStatus(ctx, githubRepo, githubHeadSHA, CommitStatus{Context: "c", Status: CheckSuccess}); !errors.Is(err, ErrNotFound) {
		t.Errorf("read-back err = %v, want ErrNotFound", err)
	}

	githubChecksServed(stub)
	for name, status := range map[string]CommitStatus{
		"absent":               {Context: "never-written", Status: CheckSuccess},
		"other state":          {Context: "slow-ci", Status: CheckSuccess},
		"shadowed by a check":  {Context: "build", Status: CheckSuccess},
		"failure read pending": {Context: "slow-ci", Status: CheckFailure},
	} {
		if err := backend.SetCommitStatus(ctx, githubRepo, githubHeadSHA, status); !errors.Is(err, ErrForgeWriteNotApplied) {
			t.Errorf("%s err = %v, want ErrForgeWriteNotApplied", name, err)
		}
	}
}

// =============================================================================
// Actions: cancel, rerun, logs
// =============================================================================

// TestGitHubBackend_CancelCheckRun cancels the job's run and waits for it to
// finish, accepting GitHub's 409 for a run that already had.
func TestGitHubBackend_CancelCheckRun(t *testing.T) {
	for _, status := range []int{http.StatusAccepted, http.StatusConflict} {
		backend, stub := newGitHubStub(t)
		githubJobServed(stub)
		stub.json("POST "+githubRunPath+"/cancel", status, nil)
		stub.sequence("GET "+githubRunPath,
			forgeJSON(http.StatusOK, githubActionsRunAt(1, "in_progress")),
			forgeJSON(http.StatusOK, githubActionsRunAt(1, "completed")))

		if err := backend.CancelCheckRun(context.Background(), githubRepo, "502"); err != nil {
			t.Fatalf("cancel answered %d: %v", status, err)
		}
		if n := len(stub.requests("GET " + githubRunPath)); n != 2 {
			t.Errorf("run reads = %d, want 2", n)
		}
	}
}

// TestGitHubBackend_CancelCheckRun_Refusals covers ids that are not Actions
// jobs of this repository, a refused cancel, an unreadable run, and a run
// still going after the bound.
func TestGitHubBackend_CancelCheckRun_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitHubStub(t)
	for _, id := range []string{"status:9001", "502"} {
		if err := backend.CancelCheckRun(ctx, githubRepo, id); !errors.Is(err, ErrForgeUnsupported) {
			t.Errorf("%s err = %v, want ErrForgeUnsupported", id, err)
		}
	}

	stub.json("GET "+githubCheckRunPath, http.StatusForbidden, nil)
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("check run read err = %v", err)
	}
	stub.json("GET "+githubCheckRunPath, http.StatusOK, map[string]any{"id": 502, "head_sha": githubHeadSHA})
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("no check suite err = %v, want ErrForgeUnsupported", err)
	}

	stub.fixture("GET "+githubCheckRunPath, http.StatusOK, "check_run.json")
	stub.json("GET "+githubRunsPath, http.StatusForbidden, nil)
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("workflow runs read err = %v", err)
	}
	// A check run another GitHub App published: no workflow run owns its
	// suite, and one for a different suite is not its run.
	stub.json("GET "+githubRunsPath, http.StatusOK, map[string]any{"workflow_runs": []map[string]any{{"id": 9100, "check_suite_id": 8801}}})
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("not an Actions check run err = %v, want ErrForgeUnsupported", err)
	}

	stub.fixture("GET "+githubRunsPath, http.StatusOK, "actions_runs.json")
	stub.json("GET "+githubRunPath+"/jobs", http.StatusForbidden, nil)
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("jobs read err = %v", err)
	}
	stub.json("GET "+githubRunPath+"/jobs", http.StatusOK, map[string]any{"jobs": []map[string]any{
		{"id": 7702, "run_id": 9100, "check_run_url": "https://elsewhere.example/repos/acme/widgets/check-runs/502"},
		{"id": 7703, "run_id": 9100, "check_run_url": stub.api() + "/repos/acme/widgets/check-runs/5020"},
	}})
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("no job naming the check run err = %v, want ErrForgeUnsupported", err)
	}

	githubJobServed(stub)
	stub.json("POST "+githubRunPath+"/cancel", http.StatusForbidden, nil)
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v", err)
	}
	stub.json("POST "+githubRunPath+"/cancel", http.StatusAccepted, nil)
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); !errors.Is(err, ErrNotFound) {
		t.Errorf("run read err = %v, want ErrNotFound", err)
	}
	stub.json("GET "+githubRunPath, http.StatusOK, githubActionsRunAt(1, "in_progress"))
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); !errors.Is(err, ErrForgeWriteUnconfirmed) {
		t.Errorf("still running err = %v, want ErrForgeWriteUnconfirmed", err)
	}
}

// TestGitHubBackend_CancelCheckRun_StopsOnCancellation proves a caller that
// gives up while the run is read back is not kept waiting.
func TestGitHubBackend_CancelCheckRun_StopsOnCancellation(t *testing.T) {
	backend, stub := newGitHubStub(t)
	ctx, cancel := context.WithCancel(context.Background())
	githubJobServed(stub)
	stub.json("POST "+githubRunPath+"/cancel", http.StatusAccepted, nil)
	stub.handle("GET "+githubRunPath, func(w http.ResponseWriter, r *http.Request) {
		cancel()
		forgeJSON(http.StatusOK, githubActionsRunAt(1, "in_progress"))(w, r)
	})
	backend.waiter = ctxWaiter{}

	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the caller's cancellation", err)
	}
}

// ctxWaiter returns at once unless the context has ended.
type ctxWaiter struct{}

func (ctxWaiter) Wait(ctx context.Context, _ int) error { return ctx.Err() }

// TestGitHubBackend_RerunCheckRun proves a new attempt, or a run no longer
// complete, is a rerun that took.
func TestGitHubBackend_RerunCheckRun(t *testing.T) {
	for name, after := range map[string]map[string]any{
		"new attempt": githubActionsRunAt(2, "completed"),
		"restarted":   githubActionsRunAt(1, "queued"),
	} {
		backend, stub := newGitHubStub(t)
		githubJobServed(stub)
		stub.json("POST "+githubJobPath+"/rerun", http.StatusCreated, nil)
		stub.json("GET "+githubRunPath, http.StatusOK, after)

		if err := backend.RerunCheckRun(context.Background(), githubRepo, githubFailedUnit); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestGitHubBackend_RerunCheckRun_Refusals covers a run naming no commit, a
// job on another head, a refused rerun, an unreadable run and a rerun that
// did not take.
func TestGitHubBackend_RerunCheckRun_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitHubStub(t)
	if err := backend.RerunCheckRun(ctx, githubRepo, ForgeCheckRun{ID: "502"}); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("no head err = %v", err)
	}
	githubJobServed(stub)
	moved := githubFailedUnit
	moved.HeadSHA = githubMovedSHA
	if err := backend.RerunCheckRun(ctx, githubRepo, moved); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("other head err = %v", err)
	}

	stub.json("POST "+githubJobPath+"/rerun", http.StatusForbidden, nil)
	if err := backend.RerunCheckRun(ctx, githubRepo, githubFailedUnit); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v", err)
	}
	stub.json("POST "+githubJobPath+"/rerun", http.StatusCreated, nil)
	if err := backend.RerunCheckRun(ctx, githubRepo, githubFailedUnit); !errors.Is(err, ErrNotFound) {
		t.Errorf("run read err = %v", err)
	}
	stub.json("GET "+githubRunPath, http.StatusOK, githubActionsRunAt(1, "completed"))
	if err := backend.RerunCheckRun(ctx, githubRepo, githubFailedUnit); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("not rerun err = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestGitHubBackend_ActionsRoutesUseTheJobsOwnID proves cancel, rerun and
// logs resolve check run 502 to the job whose check_run_url names it, and
// send job 7702's id to the job routes, never the check run's.
func TestGitHubBackend_ActionsRoutesUseTheJobsOwnID(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitHubStub(t)
	githubJobServed(stub)
	stub.json("POST "+githubRunPath+"/cancel", http.StatusAccepted, nil)
	stub.json("POST "+githubJobPath+"/rerun", http.StatusCreated, nil)
	stub.handle("GET "+githubJobPath+"/logs", forgeRaw(http.StatusOK, "FAIL TestUpload\n"))
	stub.sequence("GET "+githubRunPath,
		forgeJSON(http.StatusOK, githubActionsRunAt(1, "completed")),
		forgeJSON(http.StatusOK, githubActionsRunAt(2, "queued")))

	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); err != nil {
		t.Fatalf("CancelCheckRun: %v", err)
	}
	if err := backend.RerunCheckRun(ctx, githubRepo, githubFailedUnit); err != nil {
		t.Fatalf("RerunCheckRun: %v", err)
	}
	if logs, err := backend.CheckRunLogs(ctx, githubRepo, githubFailedUnit, 64); err != nil || logs != "FAIL TestUpload\n" {
		t.Fatalf("CheckRunLogs = %q, %v", logs, err)
	}

	runs := stub.requests("GET " + githubRunsPath)
	if len(runs) != 3 || !strings.Contains(runs[0].Query, "check_suite_id=8800") {
		t.Fatalf("workflow run lookups = %d, want one per call by the check run's suite", len(runs))
	}
	if jobs := stub.requests("GET " + githubRunPath + "/jobs"); len(jobs) != 3 || !strings.Contains(jobs[0].Query, "filter=all") {
		t.Fatalf("job listings = %d, want one per call across every attempt", len(jobs))
	}
	for _, key := range []string{"GET /repos/acme/widgets/actions/jobs/502", "POST /repos/acme/widgets/actions/jobs/502/rerun",
		"GET /repos/acme/widgets/actions/jobs/502/logs"} {
		if calls := stub.requests(key); len(calls) != 0 {
			t.Errorf("%s called %d times: a check run id is not a job id", key, len(calls))
		}
	}
}

// TestGitHubBackend_ActionsJobResolution_Refusals proves a check run that is
// not on this repository is unsupported, and a job listing that never ends
// is a fault rather than a job that does not exist.
func TestGitHubBackend_ActionsJobResolution_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitHubStub(t)

	stub.json("GET "+githubCheckRunPath, http.StatusNotFound, nil)
	if err := backend.CancelCheckRun(ctx, githubRepo, "502"); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("missing check run err = %v, want ErrForgeUnsupported", err)
	}

	stub.fixture("GET "+githubCheckRunPath, http.StatusOK, "check_run.json")
	stub.fixture("GET "+githubRunsPath, http.StatusOK, "actions_runs.json")
	stub.handle("GET "+githubRunPath+"/jobs", githubPaged(map[string]any{"jobs": []map[string]any{}},
		stub.api()+githubRunPath+"/jobs?filter=all&page=2"))
	err := backend.CancelCheckRun(ctx, githubRepo, "502")
	if !errors.Is(err, ErrBackend) || !errors.Is(err, errActionsJobPagesExhausted) {
		t.Fatalf("endless job listing err = %v, want errActionsJobPagesExhausted", err)
	}
	if pages := len(stub.requests("GET " + githubRunPath + "/jobs")); pages != maxActionsJobPages {
		t.Errorf("job pages read = %d, want the bound %d", pages, maxActionsJobPages)
	}
}

// TestGitHubBackend_CheckRunLogs returns the log's tail, bounded, with the
// token scrubbed if a step printed it.
func TestGitHubBackend_CheckRunLogs(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubJobServed(stub)
	stub.handle("GET "+githubJobPath+"/logs", forgeRaw(http.StatusOK,
		strings.Repeat("noise\n", 100)+"token "+githubTestToken+"\nFAIL TestUpload\n"))

	logs, err := backend.CheckRunLogs(context.Background(), githubRepo, githubFailedUnit, 64)
	if err != nil {
		t.Fatalf("CheckRunLogs: %v", err)
	}
	if len(logs) > 64 || !strings.HasSuffix(logs, "FAIL TestUpload\n") || strings.Contains(logs, githubTestToken) {
		t.Errorf("logs = %q, want the scrubbed tail within 64 bytes", logs)
	}
}

// TestGitHubBackend_CheckRunLogs_Refusals covers no bound, a run naming no
// commit, a check run that is not an Actions job and a refused log read.
func TestGitHubBackend_CheckRunLogs_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitHubStub(t)
	if _, err := backend.CheckRunLogs(ctx, githubRepo, githubFailedUnit, 0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no bound err = %v", err)
	}
	if _, err := backend.CheckRunLogs(ctx, githubRepo, ForgeCheckRun{ID: "502"}, 64); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("no head err = %v", err)
	}
	if _, err := backend.CheckRunLogs(ctx, githubRepo, githubFailedUnit, 64); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("not a job err = %v", err)
	}
	githubJobServed(stub)
	stub.json("GET "+githubJobPath+"/logs", http.StatusGone, nil)
	if _, err := backend.CheckRunLogs(ctx, githubRepo, githubFailedUnit, 64); forgeStatusOf(err) != http.StatusGone {
		t.Errorf("refused err = %v, want the 410", err)
	}
}
