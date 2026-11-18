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
	glPipelines = glProject + "/pipelines"
	glJobs      = glPipelines + "/9/jobs"
	glStatuses  = glProject + "/repository/commits/" + gitlabHeadSHA + "/statuses"
	glJob       = glProject + "/jobs/100"
)

// gitlabJob renders one CI job against the test head.
func gitlabJob(id int64, name, status string, allowFailure bool) map[string]any {
	return map[string]any{
		"id": id, "name": name, "status": status, "allow_failure": allowFailure,
		"created_at":  "2026-10-04T10:01:00Z",
		"finished_at": "2026-10-04T10:05:00Z",
		"commit":      map[string]any{"id": gitlabHeadSHA},
	}
}

// gitlabChecksStub serves one pipeline with jobs, and the commit's statuses.
func gitlabChecksStub(t *testing.T, jobs, statuses any) (*GitLabBackend, *forgeStub) {
	t.Helper()
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glPipelines, http.StatusOK, []map[string]any{{"id": 9}})
	stub.json("GET "+glJobs, http.StatusOK, jobs)
	stub.json("GET "+glStatuses, http.StatusOK, statuses)
	return backend, stub
}

// =============================================================================
// Approvals
// =============================================================================

// gitlabApprovalStub serves a merge request's approvals, versions and notes
// from the fixtures, which hold only fields GitLab returns.
func gitlabApprovalStub(t *testing.T) (*GitLabBackend, *forgeStub) {
	t.Helper()
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glMergeRequest+"/approvals", http.StatusOK, "approvals.json")
	stub.fixture("GET "+glMergeRequest+"/versions", http.StatusOK, "versions.json")
	stub.fixture("GET "+glMergeRequest+"/notes", http.StatusOK, "mr_notes.json")
	return backend, stub
}

func TestGitLabBackend_ListReviews_BindsEachApprovalToTheHeadItSaw(t *testing.T) {
	backend, _ := gitlabApprovalStub(t)

	reviews, err := backend.ListReviews(context.Background(), gitlabRepo, 5)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(reviews) != 3 {
		t.Fatalf("reviews = %+v, want the three with a user", reviews)
	}
	// reviewer-a approved, withdrew and approved again after the newest push:
	// the latest approval note is the approval GitLab now holds.
	approvedAt := time.Date(2026, 10, 4, 10, 30, 0, 0, time.UTC)
	if got := reviews[0]; got.ID != 21 || got.Reviewer != "reviewer-a" || got.State != "APPROVED" ||
		got.HeadSHA != gitlabHeadSHA || !got.Submitted.Equal(approvedAt) {
		t.Errorf("approval after the newest push = %+v", got)
	}
	if got := reviews[1].HeadSHA; got != strings.Repeat("0", 40) {
		t.Errorf("approval before the newest push covers %q, want the older head", got)
	}
	// reviewer-c only typed the words: a user's comment is not GitLab's
	// record of an approval, so this approval binds to no commit.
	if got := reviews[2]; got.Reviewer != "reviewer-c" || got.HeadSHA != "" || !got.Submitted.IsZero() {
		t.Errorf("approval with no approval note = %+v, want no head", got)
	}
}

func TestGitLabBackend_ListReviews_ReadsApprovalNotesOfTheMergeRequest(t *testing.T) {
	backend, stub := gitlabApprovalStub(t)

	if _, err := backend.ListReviews(context.Background(), gitlabRepo, 5); err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if calls := stub.requests("GET " + glMergeRequest + "/notes"); len(calls) != 1 {
		t.Fatalf("merge request notes read %d times, want once", len(calls))
	}
	if calls := stub.requests("GET " + glProject + "/issues/5/notes"); len(calls) != 0 {
		t.Fatalf("issue notes read %d times; an issue sharing the number is not the merge request", len(calls))
	}
}

func TestGitLabBackend_ListReviews_NoApprovalsReadsNoVersions(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glMergeRequest+"/approvals", http.StatusOK, map[string]any{"approved_by": []any{}})

	reviews, err := backend.ListReviews(context.Background(), gitlabRepo, 5)
	if err != nil || reviews != nil {
		t.Fatalf("reviews = %+v, err = %v; want none", reviews, err)
	}
	if calls := stub.requests("GET " + glMergeRequest + "/versions"); len(calls) != 0 {
		t.Fatalf("versions read %d times for no approvals", len(calls))
	}
}

func TestGitLabBackend_ListReviews_Refusals(t *testing.T) {
	t.Run("missing merge request", func(t *testing.T) {
		backend, _ := newGitLabStub(t)
		if _, err := backend.ListReviews(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("versions refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.fixture("GET "+glMergeRequest+"/approvals", http.StatusOK, "approvals.json")
		stub.json("GET "+glMergeRequest+"/versions", http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
		if _, err := backend.ListReviews(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("notes refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.fixture("GET "+glMergeRequest+"/approvals", http.StatusOK, "approvals.json")
		stub.fixture("GET "+glMergeRequest+"/versions", http.StatusOK, "versions.json")
		stub.json("GET "+glMergeRequest+"/notes", http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		if _, err := backend.ListReviews(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
}

func TestHeadAt(t *testing.T) {
	early := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	versions := []gitlabVersion{
		{HeadCommitSHA: "old", CreatedAt: early},
		{HeadCommitSHA: "", CreatedAt: early.Add(time.Minute)},
		{HeadCommitSHA: "new", CreatedAt: early.Add(time.Hour)},
	}
	if got := headAt(versions, early.Add(30*time.Minute)); got != "old" {
		t.Errorf("headAt between pushes = %q, want old (a version with no head is skipped)", got)
	}
	if got := headAt(versions, early.Add(time.Hour)); got != "old" {
		t.Errorf("headAt the instant of a push = %q, want old: which head it saw is unknown", got)
	}
	if got := headAt(versions, early.Add(time.Hour+time.Millisecond)); got != "new" {
		t.Errorf("headAt just after a push = %q, want new", got)
	}
	if got := headAt(versions, early.Add(-time.Second)); got != "" {
		t.Errorf("headAt before every push = %q, want none", got)
	}
}

func TestGitLabBackend_SubmitReview_Refuses(t *testing.T) {
	backend, stub := newGitLabStub(t)
	valid := ReviewSubmission{Verdict: VerdictApprove, Body: "lgtm", HeadSHA: gitlabHeadSHA}
	if _, err := backend.SubmitReview(context.Background(), gitlabRepo, 5, valid); !errors.Is(err, ErrForgeUnsupported) {
		t.Fatalf("err = %v, want ErrForgeUnsupported", err)
	}
	if _, err := backend.SubmitReview(context.Background(), gitlabRepo, 5, ReviewSubmission{}); err == nil || errors.Is(err, ErrForgeUnsupported) {
		t.Fatalf("err = %v, want the submission's own validation error", err)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Fatalf("SubmitReview wrote %+v", writes)
	}
}

// =============================================================================
// Check runs
// =============================================================================

func TestGitLabCommitStatus_CheckState(t *testing.T) {
	cases := []struct {
		status       string
		allowFailure bool
		want         [2]string
	}{
		{"success", false, [2]string{"completed", "success"}},
		{"SUCCESS", false, [2]string{"completed", "success"}},
		{"failed", false, [2]string{"completed", "failure"}},
		{"failed", true, [2]string{"completed", "skipped"}},
		{"manual", true, [2]string{"completed", "skipped"}},
		{"manual", false, [2]string{"queued", ""}},
		{"canceled", false, [2]string{"completed", "cancelled"}},
		{"skipped", false, [2]string{"completed", "skipped"}},
		{"running", false, [2]string{"in_progress", ""}},
		{"pending", false, [2]string{"queued", ""}},
		{"created", false, [2]string{"queued", ""}},
	}
	for _, tc := range cases {
		s := gitlabCommitStatus{Status: tc.status, AllowFailure: tc.allowFailure}
		if got := s.checkState(); got != tc.want {
			t.Errorf("%s (allow_failure=%v) = %v, want %v", tc.status, tc.allowFailure, got, tc.want)
		}
	}
}

func TestGitLabBackend_ListCheckRuns_JobsThenOtherStatuses(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glPipelines, http.StatusOK, []map[string]any{{"id": 9}})
	stub.fixture("GET "+glJobs, http.StatusOK, "jobs.json")
	stub.fixture("GET "+glStatuses, http.StatusOK, "statuses.json")

	runs, err := backend.ListCheckRuns(context.Background(), gitlabRepo, gitlabHeadSHA)
	if err != nil {
		t.Fatalf("ListCheckRuns: %v", err)
	}
	if len(runs) != 4 {
		t.Fatalf("runs = %+v, want three jobs and one foreign status", runs)
	}
	if got := stub.requests("GET " + glPipelines); gitlabQuery(t, got[0]).Get("sha") != gitlabHeadSHA {
		t.Errorf("pipelines not filtered by the commit: %+v", got[0])
	}

	failed := runs[0]
	started := time.Date(2026, 10, 4, 10, 2, 0, 0, time.UTC)
	finished := time.Date(2026, 10, 4, 10, 5, 0, 0, time.UTC)
	if failed.ID != "100" || failed.Name != "test" || failed.Status != "completed" || failed.Conclusion != "failure" ||
		failed.HeadSHA != gitlabHeadSHA || !strings.HasSuffix(failed.URL, "/jobs/100") ||
		!failed.Started.Equal(started) || !failed.Completed.Equal(finished) {
		t.Errorf("failed job = %+v", failed)
	}
	if allowed := runs[2]; allowed.ID != "102" || allowed.Conclusion != "skipped" {
		t.Errorf("allowed failure = %+v, want skipped", allowed)
	}
	review := runs[3]
	created := time.Date(2026, 10, 4, 10, 31, 0, 0, time.UTC)
	if review.ID != statusRunPrefix+"555" || review.Name != "quicbaas/review" || review.URL != "https://review.example/5" ||
		review.HeadSHA != gitlabHeadSHA || !review.Started.Equal(created) {
		t.Errorf("foreign status = %+v", review)
	}
}

func TestGitLabBackend_ListCheckRuns_NoPipelineIsNoJobs(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glPipelines, http.StatusOK, []any{})
	stub.json("GET "+glStatuses, http.StatusOK, []any{})

	runs, err := backend.ListCheckRuns(context.Background(), gitlabRepo, gitlabHeadSHA)
	if err != nil || len(runs) != 0 {
		t.Fatalf("runs = %+v, err = %v; want none", runs, err)
	}
}

func TestGitLabBackend_ListCheckRuns_Refusals(t *testing.T) {
	t.Run("no commit", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		if _, err := backend.ListCheckRuns(context.Background(), gitlabRepo, ""); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("err = %v, want ErrInvalidInput", err)
		}
		if calls := stub.requests("GET " + glPipelines); len(calls) != 0 {
			t.Fatal("an empty commit reached GitLab")
		}
	})
	t.Run("pipelines refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glPipelines, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
		if _, err := backend.ListCheckRuns(context.Background(), gitlabRepo, gitlabHeadSHA); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
	t.Run("jobs missing", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glPipelines, http.StatusOK, []map[string]any{{"id": 9}})
		if _, err := backend.ListCheckRuns(context.Background(), gitlabRepo, gitlabHeadSHA); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("statuses missing", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glPipelines, http.StatusOK, []any{})
		if _, err := backend.ListCheckRuns(context.Background(), gitlabRepo, gitlabHeadSHA); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
	t.Run("pipelines never end", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.handle("GET "+glPipelines, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set(gitlabNextPageHeader, "2")
			forgeJSON(http.StatusOK, []any{})(w, nil)
		})
		if _, err := backend.ListCheckRuns(context.Background(), gitlabRepo, gitlabHeadSHA); !errors.Is(err, errCheckRunPagesExhausted) {
			t.Fatalf("err = %v, want the listing reported exhausted", err)
		}
	})
}

// =============================================================================
// Commit statuses
// =============================================================================

func TestGitLabBackend_SetCommitStatus_WritesAndReadsBack(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("POST "+glProject+"/statuses/"+gitlabHeadSHA, http.StatusCreated, map[string]any{"id": 700})
	stub.json("GET "+glPipelines, http.StatusOK, []any{})
	stub.json("GET "+glStatuses, http.StatusOK, []map[string]any{
		{"id": 700, "name": "quicbaas/review", "status": "success", "created_at": "2026-10-04T10:31:00Z"},
	})

	err := backend.SetCommitStatus(context.Background(), gitlabRepo, gitlabHeadSHA, CommitStatus{
		Context: "quicbaas/review", Status: CheckSuccess, Description: "approved", TargetURL: "https://review.example/5",
	})
	if err != nil {
		t.Fatalf("SetCommitStatus: %v", err)
	}
	calls := stub.requests("POST " + glProject + "/statuses/" + gitlabHeadSHA)
	if len(calls) != 1 {
		t.Fatalf("status written %d times", len(calls))
	}
	body := decodeBody(t, calls[0])
	if body["state"] != "success" || body["name"] != "quicbaas/review" ||
		body["description"] != "approved" || body["target_url"] != "https://review.example/5" {
		t.Errorf("status body = %+v", body)
	}
}

func TestGitLabBackend_SetCommitStatus_ReadBackDisagrees(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("POST "+glProject+"/statuses/"+gitlabHeadSHA, http.StatusCreated, map[string]any{"id": 700})
	stub.json("GET "+glPipelines, http.StatusOK, []any{})
	stub.json("GET "+glStatuses, http.StatusOK, []map[string]any{
		{"id": 700, "name": "quicbaas/review", "status": "pending", "created_at": "2026-10-04T10:31:00Z"},
	})

	err := backend.SetCommitStatus(context.Background(), gitlabRepo, gitlabHeadSHA, CommitStatus{Context: "quicbaas/review", Status: CheckFailure})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
	}
	if body := decodeBody(t, stub.requests("POST " + glProject + "/statuses/" + gitlabHeadSHA)[0]); body["state"] != "failed" {
		t.Errorf("failure written as %v, want failed", body["state"])
	}
}

func TestGitLabBackend_SetCommitStatus_Refusals(t *testing.T) {
	cases := map[string]struct {
		sha    string
		status CommitStatus
		want   error
	}{
		"no commit":       {"  ", CommitStatus{Context: "ci", Status: CheckSuccess}, ErrInvalidInput},
		"invalid status":  {gitlabHeadSHA, CommitStatus{Status: CheckSuccess}, ErrInvalidInput},
		"skipped":         {gitlabHeadSHA, CommitStatus{Context: "ci", Status: CheckSkipped}, ErrForgeUnsupported},
		"write refused":   {gitlabHeadSHA, CommitStatus{Context: "ci", Status: CheckPending}, ErrBackend},
		"read-back fails": {gitlabHeadSHA, CommitStatus{Context: "ci", Status: CheckCancelled}, ErrNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			if name == "write refused" {
				stub.json("POST "+glProject+"/statuses/"+gitlabHeadSHA, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
			} else {
				stub.json("POST "+glProject+"/statuses/"+gitlabHeadSHA, http.StatusCreated, map[string]any{"id": 700})
			}
			err := backend.SetCommitStatus(context.Background(), gitlabRepo, tc.sha, tc.status)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// =============================================================================
// Job control
// =============================================================================

func TestGitLabBackend_CancelCheckRun_WaitsForTheJobToFinish(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.sequence("GET "+glJob,
		forgeJSON(http.StatusOK, gitlabJob(100, "test", "running", false)),
		forgeJSON(http.StatusOK, gitlabJob(100, "test", "running", false)),
		forgeJSON(http.StatusOK, gitlabJob(100, "test", "canceled", false)))
	stub.json("POST "+glJob+"/cancel", http.StatusCreated, gitlabJob(100, "test", "canceling", false))

	if err := backend.CancelCheckRun(context.Background(), gitlabRepo, "100"); err != nil {
		t.Fatalf("CancelCheckRun: %v", err)
	}
	if calls := stub.requests("POST " + glJob + "/cancel"); len(calls) != 1 {
		t.Fatalf("cancel posted %d times", len(calls))
	}
}

func TestGitLabBackend_CancelCheckRun_NeverFinishes(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glJob, http.StatusOK, gitlabJob(100, "test", "running", false))
	stub.json("POST "+glJob+"/cancel", http.StatusCreated, gitlabJob(100, "test", "canceling", false))

	err := backend.CancelCheckRun(context.Background(), gitlabRepo, "100")
	if !errors.Is(err, ErrForgeWriteUnconfirmed) || !strings.Contains(err.Error(), "running") {
		t.Fatalf("err = %v, want ErrForgeWriteUnconfirmed naming the status", err)
	}
}

func TestGitLabBackend_CancelCheckRun_Refusals(t *testing.T) {
	cases := map[string]struct {
		id    string
		setup func(*forgeStub)
		want  error
	}{
		"a commit status": {statusRunPrefix + "555", func(*forgeStub) {}, ErrForgeUnsupported},
		"no such job":     {"100", func(*forgeStub) {}, ErrForgeUnsupported},
		"another job": {"100", func(s *forgeStub) {
			s.json("GET "+glJob, http.StatusOK, gitlabJob(999, "test", "running", false))
		}, ErrForgeUnsupported},
		"job read refused": {"100", func(s *forgeStub) {
			s.json("GET "+glJob, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
		}, ErrBackend},
		"cancel refused": {"100", func(s *forgeStub) {
			s.json("GET "+glJob, http.StatusOK, gitlabJob(100, "test", "running", false))
			s.json("POST "+glJob+"/cancel", http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		}, ErrBackend},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			tc.setup(stub)
			if err := backend.CancelCheckRun(context.Background(), gitlabRepo, tc.id); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGitLabBackend_RerunCheckRun_RetriesTheJob(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glJob, http.StatusOK, gitlabJob(100, "test", "failed", false))
	stub.json("POST "+glJob+"/retry", http.StatusCreated, gitlabJob(103, "test", "pending", false))

	run := ForgeCheckRun{ID: "100", Name: "test", HeadSHA: gitlabHeadSHA}
	if err := backend.RerunCheckRun(context.Background(), gitlabRepo, run); err != nil {
		t.Fatalf("RerunCheckRun: %v", err)
	}
	if calls := stub.requests("POST " + glJob + "/retry"); len(calls) != 1 {
		t.Fatalf("retry posted %d times", len(calls))
	}
}

func TestGitLabBackend_RerunCheckRun_Refusals(t *testing.T) {
	run := ForgeCheckRun{ID: "100", Name: "test", HeadSHA: gitlabHeadSHA}
	t.Run("no commit recorded", func(t *testing.T) {
		backend, _ := newGitLabStub(t)
		if err := backend.RerunCheckRun(context.Background(), gitlabRepo, ForgeCheckRun{ID: "100"}); !errors.Is(err, ErrForgeUnsupported) {
			t.Fatalf("err = %v, want ErrForgeUnsupported", err)
		}
	})
	t.Run("job ran against another commit", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		moved := gitlabJob(100, "test", "failed", false)
		moved["commit"] = map[string]any{"id": gitlabMovedSHA}
		stub.json("GET "+glJob, http.StatusOK, moved)
		if err := backend.RerunCheckRun(context.Background(), gitlabRepo, run); !errors.Is(err, ErrForgeUnsupported) {
			t.Fatalf("err = %v, want ErrForgeUnsupported", err)
		}
		if writes := stub.writes(); len(writes) != 0 {
			t.Fatalf("a job on another commit was retried: %+v", writes)
		}
	})
	t.Run("retry answered with the same job", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glJob, http.StatusOK, gitlabJob(100, "test", "failed", false))
		stub.json("POST "+glJob+"/retry", http.StatusCreated, gitlabJob(100, "test", "failed", false))
		if err := backend.RerunCheckRun(context.Background(), gitlabRepo, run); !errors.Is(err, ErrForgeWriteNotApplied) {
			t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
		}
	})
	t.Run("retry refused", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glJob, http.StatusOK, gitlabJob(100, "test", "failed", false))
		stub.json("POST "+glJob+"/retry", http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		if err := backend.RerunCheckRun(context.Background(), gitlabRepo, run); !errors.Is(err, ErrBackend) {
			t.Fatalf("err = %v, want ErrBackend", err)
		}
	})
}

func TestGitLabBackend_CheckRunLogs_ReturnsTheTail(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glJob, http.StatusOK, gitlabJob(100, "test", "failed", false))
	stub.handle("GET "+glJob+"/trace", forgeRaw(http.StatusOK, "setup\ntoken "+gitlabTestToken+"\nJob failed"))

	run := ForgeCheckRun{ID: "100", Name: "test", HeadSHA: gitlabHeadSHA}
	tail, err := backend.CheckRunLogs(context.Background(), gitlabRepo, run, 10)
	if err != nil || tail != "Job failed" {
		t.Fatalf("tail = %q, err = %v; want the last ten bytes", tail, err)
	}
	whole, err := backend.CheckRunLogs(context.Background(), gitlabRepo, run, 4096)
	if err != nil || !strings.HasPrefix(whole, "setup") || strings.Contains(whole, gitlabTestToken) {
		t.Fatalf("whole log = %q, err = %v; want it whole with the token redacted", whole, err)
	}
}

func TestGitLabBackend_CheckRunLogs_Refusals(t *testing.T) {
	run := ForgeCheckRun{ID: "100", Name: "test", HeadSHA: gitlabHeadSHA}
	t.Run("no budget", func(t *testing.T) {
		backend, _ := newGitLabStub(t)
		if _, err := backend.CheckRunLogs(context.Background(), gitlabRepo, run, 0); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("err = %v, want ErrInvalidInput", err)
		}
	})
	t.Run("no commit recorded", func(t *testing.T) {
		backend, _ := newGitLabStub(t)
		if _, err := backend.CheckRunLogs(context.Background(), gitlabRepo, ForgeCheckRun{ID: "100"}, 10); !errors.Is(err, ErrForgeUnsupported) {
			t.Fatalf("err = %v, want ErrForgeUnsupported", err)
		}
	})
	t.Run("a commit status", func(t *testing.T) {
		backend, _ := newGitLabStub(t)
		status := ForgeCheckRun{ID: statusRunPrefix + "555", Name: "quicbaas/review", HeadSHA: gitlabHeadSHA}
		if _, err := backend.CheckRunLogs(context.Background(), gitlabRepo, status, 10); !errors.Is(err, ErrForgeUnsupported) {
			t.Fatalf("err = %v, want ErrForgeUnsupported", err)
		}
	})
	t.Run("trace missing", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.json("GET "+glJob, http.StatusOK, gitlabJob(100, "test", "failed", false))
		if _, err := backend.CheckRunLogs(context.Background(), gitlabRepo, run, 10); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	})
}

// =============================================================================
// Review gate inputs
// =============================================================================

// gitlabGateDecision reads a merge request's approvals and pipeline through
// the adapter and decides the review gate on them, as the module does.
func gitlabGateDecision(t *testing.T, backend *GitLabBackend, policy ReviewPolicy) GateDecision {
	t.Helper()
	ctx := context.Background()
	reviews, err := backend.ListReviews(ctx, gitlabRepo, 5)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	runs, err := backend.ListCheckRuns(ctx, gitlabRepo, gitlabHeadSHA)
	if err != nil {
		t.Fatalf("ListCheckRuns: %v", err)
	}
	in := GateInput{
		Policy:           policy,
		PullRequestState: PullRequestOpen,
		HeadSHA:          gitlabHeadSHA,
		Checks:           checkRecordsFor(runs, gitlabHeadSHA, time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)),
	}
	for _, review := range reviews {
		if record, ok := reviewRecordFor(review, policy, 1); ok {
			in.Reviews = append(in.Reviews, record)
		}
	}
	return EvaluateReviewGate(in)
}

func TestGitLabBackend_ReviewGateReadsApprovalsAndPipelines(t *testing.T) {
	policy := ReviewPolicy{RequiredApprovals: 1, RequireGreenChecks: true}
	cases := map[string]struct {
		jobs     []map[string]any
		statuses []map[string]any
		want     GateBlockReason
	}{
		"a green pipeline": {
			jobs: []map[string]any{gitlabJob(101, "test", "success", false)},
			want: GateClear,
		},
		"an allowed failure passes over": {
			jobs: []map[string]any{gitlabJob(101, "test", "success", false), gitlabJob(102, "lint", "failed", true)},
			want: GateClear,
		},
		"only an allowed failure": {
			jobs: []map[string]any{gitlabJob(102, "lint", "failed", true)},
			want: GateBlockedNoChecks,
		},
		"a failed job": {
			jobs: []map[string]any{gitlabJob(101, "build", "success", false), gitlabJob(100, "test", "failed", false)},
			want: GateBlockedChecksFailed,
		},
		"a running job": {
			jobs: []map[string]any{gitlabJob(101, "build", "success", false), gitlabJob(103, "test", "running", false)},
			want: GateBlockedChecksIncomplete,
		},
		"a failed foreign status": {
			jobs:     []map[string]any{gitlabJob(101, "test", "success", false)},
			statuses: []map[string]any{{"id": 555, "name": "security/scan", "status": "failed", "created_at": "2026-10-04T10:31:00Z"}},
			want:     GateBlockedChecksFailed,
		},
		"no pipeline": {want: GateBlockedNoChecks},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			jobs, statuses := tc.jobs, tc.statuses
			if jobs == nil {
				jobs = []map[string]any{}
			}
			if statuses == nil {
				statuses = []map[string]any{}
			}
			backend, stub := gitlabChecksStub(t, jobs, statuses)
			stub.fixture("GET "+glMergeRequest+"/approvals", http.StatusOK, "approvals.json")
			stub.fixture("GET "+glMergeRequest+"/versions", http.StatusOK, "versions.json")
			stub.fixture("GET "+glMergeRequest+"/notes", http.StatusOK, "mr_notes.json")

			got := gitlabGateDecision(t, backend, policy)
			if got.Reason != tc.want {
				t.Fatalf("reason = %s (%s), want %s", got.Reason, got.Detail, tc.want)
			}
			if got.Mergeable() && got.Approvals != 1 {
				t.Fatalf("approvals = %d, want only the one covering the head", got.Approvals)
			}
		})
	}
}

// TestGitLabBackend_ReviewGateRetriedJobSupersedes reads the fixture
// pipeline: a failed job retried green, an allowed failure, and a foreign
// status. A retry supersedes the run it replaced, so the head is green.
func TestGitLabBackend_ReviewGateRetriedJobSupersedes(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glPipelines, http.StatusOK, []map[string]any{{"id": 9}})
	stub.fixture("GET "+glJobs, http.StatusOK, "jobs.json")
	stub.fixture("GET "+glStatuses, http.StatusOK, "statuses.json")
	stub.fixture("GET "+glMergeRequest+"/approvals", http.StatusOK, "approvals.json")
	stub.fixture("GET "+glMergeRequest+"/versions", http.StatusOK, "versions.json")
	stub.fixture("GET "+glMergeRequest+"/notes", http.StatusOK, "mr_notes.json")

	if got := gitlabGateDecision(t, backend, ReviewPolicy{RequiredApprovals: 1, RequireGreenChecks: true}); !got.Mergeable() {
		t.Fatalf("reason = %s (%s), want clear", got.Reason, got.Detail)
	}
	// An approval of an older head, and one GitLab gave no time for, cover
	// nothing: two required approvals are not met by the one on this head.
	got := gitlabGateDecision(t, backend, ReviewPolicy{RequiredApprovals: 2, RequireGreenChecks: true})
	if got.Reason != GateBlockedInsufficientApprovals || got.Approvals != 1 {
		t.Fatalf("decision = %+v, want one of two approvals", got)
	}
}
