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
// The Gitea adapter's write surface: verdicts and commit statuses
// =============================================================================

// respondPages serves a paginated listing on one path.
func (f *giteaFixture) respondPages(path string, pages [][]map[string]any) {
	f.handlers["GET "+path] = func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if page >= 1 && page <= len(pages) {
			_ = json.NewEncoder(w).Encode(pages[page-1])
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}
}

func reviewPage(first, count int, head string) []map[string]any {
	page := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		page = append(page, map[string]any{
			"id":           first + i,
			"user":         map[string]any{"login": "codex"},
			"state":        "COMMENT",
			"body":         "round " + strconv.Itoa(first+i),
			"commit_id":    head,
			"submitted_at": "2026-01-01T00:00:00Z",
		})
	}
	return page
}

func filePage(first, count int) []map[string]any {
	page := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		page = append(page, map[string]any{
			"filename":  "pkg/thing/file" + strconv.Itoa(first+i) + ".go",
			"status":    "Modified",
			"additions": 3,
			"deletions": 1,
		})
	}
	return page
}

// =============================================================================
// FAILURE 5 (adapter half) — a capped page is not the whole listing
// =============================================================================

func TestListPullRequestFiles_PagesUntilAShortPage(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondPages("/api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/files", [][]map[string]any{
		filePage(1, giteaDefaultPageSize),
		filePage(giteaDefaultPageSize+1, giteaDefaultPageSize),
		filePage(2*giteaDefaultPageSize+1, 4),
	})

	files, err := backend.ListPullRequestFiles(context.Background(), testRepo, 41)
	if err != nil {
		t.Fatalf("ListPullRequestFiles: %v", err)
	}
	want := 2*giteaDefaultPageSize + 4
	if len(files) != want {
		t.Fatalf("files = %d, want %d; a capped page was treated as the whole change", len(files), want)
	}
	if files[0].Path != "pkg/thing/file1.go" || files[len(files)-1].Path != "pkg/thing/file104.go" {
		t.Fatalf("first = %q last = %q", files[0].Path, files[len(files)-1].Path)
	}
	// The forge's own casing is normalised so a caller can compare it.
	if files[0].Status != "modified" {
		t.Fatalf("status = %q", files[0].Status)
	}
	if files[0].Additions != 3 || files[0].Deletions != 1 {
		t.Fatalf("counts = +%d/-%d", files[0].Additions, files[0].Deletions)
	}
}

func TestListPullRequestFiles_AForgeThatNeverEndsIsAFault(t *testing.T) {
	// Reported rather than truncated: a caller cannot tell a truncated map
	// of a change from a complete one, and would review the prefix.
	backend, fixture := newGiteaFixture(t)
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/files"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(filePage(1, giteaDefaultPageSize))
	}
	files, err := backend.ListPullRequestFiles(context.Background(), testRepo, 41)
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("err = %v, want ErrBackend", err)
	}
	if files != nil {
		t.Fatal("a prefix was returned beside the error")
	}
}

func TestListPullRequestFiles_CarriesARename(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/files", http.StatusOK, []map[string]any{
		{"filename": "pkg/b.go", "previous_filename": "pkg/a.go", "status": "renamed"},
	})
	files, err := backend.ListPullRequestFiles(context.Background(), testRepo, 41)
	if err != nil {
		t.Fatalf("ListPullRequestFiles: %v", err)
	}
	if len(files) != 1 || files[0].PreviousPath != "pkg/a.go" {
		t.Fatalf("files = %+v", files)
	}
}

func TestListReviews_PagesUntilAShortPage(t *testing.T) {
	// The gate reduces reviews to each reviewer's verdict on the CURRENT
	// head, and the current head's verdicts are the NEWEST — the far end of
	// the listing. A capped first page hides exactly the reviews that decide
	// the merge.
	backend, fixture := newGiteaFixture(t)
	fixture.respondPages("/api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/reviews", [][]map[string]any{
		reviewPage(1, giteaDefaultPageSize, "old-head"),
		reviewPage(giteaDefaultPageSize+1, 2, "current-head"),
	})
	reviews, err := backend.ListReviews(context.Background(), testRepo, 41)
	if err != nil {
		t.Fatalf("ListReviews: %v", err)
	}
	if len(reviews) != giteaDefaultPageSize+2 {
		t.Fatalf("reviews = %d; the newest verdicts were dropped", len(reviews))
	}
	if reviews[len(reviews)-1].HeadSHA != "current-head" {
		t.Fatalf("last review head = %q", reviews[len(reviews)-1].HeadSHA)
	}
	if reviews[0].Reviewer != "codex" {
		t.Fatalf("reviewer = %q", reviews[0].Reviewer)
	}
}

func TestListReviews_AForgeThatNeverEndsIsAFault(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/reviews"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(reviewPage(1, giteaDefaultPageSize, "h"))
	}
	if _, err := backend.ListReviews(context.Background(), testRepo, 41); !errors.Is(err, ErrBackend) {
		t.Fatalf("err = %v, want ErrBackend", err)
	}
}

// =============================================================================
// SubmitReview
// =============================================================================

func TestSubmitReview_PostsTheVerdictBoundToTheHead(t *testing.T) {
	for verdict, want := range map[ReviewVerdict]string{
		VerdictApprove:        "APPROVED",
		VerdictRequestChanges: "REQUEST_CHANGES",
		VerdictComment:        "COMMENT",
	} {
		t.Run(string(verdict), func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/reviews", http.StatusOK, map[string]any{
				"id":           7,
				"user":         map[string]any{"login": "quicbaas-reviewer"},
				"state":        want,
				"body":         "findings",
				"commit_id":    "head-1",
				"submitted_at": "2026-01-01T00:00:00Z",
			})
			stored, err := backend.SubmitReview(context.Background(), testRepo, 41,
				ReviewSubmission{Verdict: verdict, Body: "findings", HeadSHA: "head-1"})
			if err != nil {
				t.Fatalf("SubmitReview: %v", err)
			}
			if stored.ID != 7 || stored.HeadSHA != "head-1" || stored.Reviewer != "quicbaas-reviewer" {
				t.Fatalf("stored = %+v", stored)
			}
			sent := fixture.requests[len(fixture.requests)-1]
			if !strings.Contains(sent.Body, `"event":"`+want+`"`) {
				t.Fatalf("body = %s", sent.Body)
			}
			// THE HEAD IS SENT. A verdict the forge stored against no commit
			// could never be attributed to a head by the gate.
			if !strings.Contains(sent.Body, `"commit_id":"head-1"`) {
				t.Fatalf("the head was not sent: %s", sent.Body)
			}
		})
	}
}

func TestSubmitReview_RefusesAWriteThatDidNotTakeEffect(t *testing.T) {
	// Gitea has documented cases of answering 200 for a write that did
	// nothing, and a review is the one write whose absence is invisible:
	// the module would publish a green status for an approval that is not
	// there.
	for name, response := range map[string]map[string]any{
		"no id":          {"state": "APPROVED", "commit_id": "head-1"},
		"another state":  {"id": 7, "state": "COMMENT", "commit_id": "head-1"},
		"another commit": {"id": 7, "state": "APPROVED", "commit_id": "head-2"},
	} {
		t.Run(name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/reviews", http.StatusOK, response)
			_, err := backend.SubmitReview(context.Background(), testRepo, 41,
				ReviewSubmission{Verdict: VerdictApprove, Body: "findings", HeadSHA: "head-1"})
			if !errors.Is(err, ErrForgeWriteNotApplied) {
				t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
			}
		})
	}
}

func TestSubmitReview_AServerThatOmitsTheCommitIsNotAServerThatDisagreed(t *testing.T) {
	// Older Gitea omits commit_id from the create response while storing it.
	// An empty value is filled from what was sent; only a DIFFERENT value is
	// a refusal.
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/reviews", http.StatusOK, map[string]any{
		"id": 7, "state": "APPROVED", "body": "findings",
	})
	stored, err := backend.SubmitReview(context.Background(), testRepo, 41,
		ReviewSubmission{Verdict: VerdictApprove, Body: "findings", HeadSHA: "head-1"})
	if err != nil {
		t.Fatalf("SubmitReview: %v", err)
	}
	if stored.HeadSHA != "head-1" {
		t.Fatalf("head = %q", stored.HeadSHA)
	}
}

func TestSubmitReview_RefusesASubmissionNoForgeShouldStore(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	for name, submission := range map[string]ReviewSubmission{
		"empty body": {Verdict: VerdictApprove, HeadSHA: "head-1"},
		"no head":    {Verdict: VerdictApprove, Body: "findings"},
		"no verdict": {Body: "findings", HeadSHA: "head-1"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := backend.SubmitReview(context.Background(), testRepo, 41, submission); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
	if len(fixture.requests) != 0 {
		t.Fatalf("a refused submission still reached the forge: %+v", fixture.requests)
	}
}

// =============================================================================
// SetCommitStatus
// =============================================================================

// respondStatus serves the status write and the read-back it verifies with.
func (f *giteaFixture) respondStatus(state, context string) {
	f.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/statuses/head-1", http.StatusOK,
		map[string]any{"id": 1, "status": state, "context": context})
	f.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/commits/head-1/statuses", http.StatusOK,
		[]map[string]any{{"id": 1, "status": state, "context": context}})
}

func TestSetCommitStatus_WritesAndVerifiesByReadBack(t *testing.T) {
	for status, state := range map[CheckStatus]string{
		CheckSuccess: "success",
		CheckFailure: "failure",
		CheckPending: "pending",
	} {
		t.Run(status.String(), func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			fixture.respondStatus(state, "quicbaas/review")
			err := backend.SetCommitStatus(context.Background(), testRepo, "head-1", CommitStatus{
				Context: "quicbaas/review", Status: status, Description: "why", TargetURL: "http://forge.test/pulls/41",
			})
			if err != nil {
				t.Fatalf("SetCommitStatus: %v", err)
			}
			sent := fixture.requests[0]
			if !strings.Contains(sent.Body, `"state":"`+state+`"`) ||
				!strings.Contains(sent.Body, `"context":"quicbaas/review"`) ||
				!strings.Contains(sent.Body, `"description":"why"`) ||
				!strings.Contains(sent.Body, `"target_url":"http://forge.test/pulls/41"`) {
				t.Fatalf("body = %s", sent.Body)
			}
		})
	}
}

func TestSetCommitStatus_RefusesAWriteThatDidNotTakeEffect(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// The forge accepts the write and reports a different state.
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/statuses/head-1", http.StatusOK,
		map[string]any{"id": 1, "status": "failure", "context": "quicbaas/review"})
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/commits/head-1/statuses", http.StatusOK,
		[]map[string]any{{"id": 1, "status": "pending", "context": "quicbaas/review"}})
	err := backend.SetCommitStatus(context.Background(), testRepo, "head-1",
		CommitStatus{Context: "quicbaas/review", Status: CheckFailure})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
	}
}

func TestSetCommitStatus_RefusesAStatusTheCommitDoesNotCarry(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/statuses/head-1", http.StatusOK,
		map[string]any{"id": 1, "status": "success", "context": "quicbaas/review"})
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/commits/head-1/statuses", http.StatusOK,
		[]map[string]any{{"id": 1, "status": "success", "context": "something/else"}})
	err := backend.SetCommitStatus(context.Background(), testRepo, "head-1",
		CommitStatus{Context: "quicbaas/review", Status: CheckSuccess})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("err = %v, want ErrForgeWriteNotApplied", err)
	}
}

func TestSetCommitStatus_RefusesAStateGiteaCannotExpress(t *testing.T) {
	// A cancellation mapped onto failure would report a run nobody finished
	// as a change that failed, and mapped onto pending would leave a
	// required check waiting for ever.
	backend, fixture := newGiteaFixture(t)
	err := backend.SetCommitStatus(context.Background(), testRepo, "head-1",
		CommitStatus{Context: "quicbaas/review", Status: CheckCancelled})
	if !errors.Is(err, ErrForgeUnsupported) {
		t.Fatalf("err = %v, want ErrForgeUnsupported", err)
	}
	if len(fixture.requests) != 0 {
		t.Fatal("an unexpressible state still reached the forge")
	}
}

func TestSetCommitStatus_RefusesWhatPublishesNothingUsable(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	for name, tc := range map[string]struct {
		sha    string
		status CommitStatus
	}{
		"no sha":     {"", CommitStatus{Context: "c", Status: CheckSuccess}},
		"no context": {"head-1", CommitStatus{Status: CheckSuccess}},
		"no state":   {"head-1", CommitStatus{Context: "c"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := backend.SetCommitStatus(context.Background(), testRepo, tc.sha, tc.status); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
	if len(fixture.requests) != 0 {
		t.Fatal("a refused status still reached the forge")
	}
}

func TestGiteaCapabilities_DeclaresTheWriteSurfaceItImplements(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	caps := backend.Capabilities()
	if !caps.SubmitReviews {
		t.Fatal("gitea accepts review verdicts and the adapter implements them")
	}
	if !caps.CommitStatuses {
		t.Fatal("gitea accepts commit statuses and the adapter implements them")
	}
}

func TestCommitStatus_AndCheckStatusRoundTripThroughOneMapping(t *testing.T) {
	// Going out through giteaCommitStates and back through giteaStatusToRun
	// and checkStatusFor is what makes the write's read-back meaningful; a
	// state that did not survive the round trip would fail every write.
	for status, state := range giteaCommitStates {
		phase, conclusion := giteaStatusToRun(state)
		if got := checkStatusFor(ForgeCheckRun{Status: phase, Conclusion: conclusion}); got != status {
			t.Fatalf("%s → %q → %s", status, state, got)
		}
	}
}

func TestSubmitReview_EverySubmittableVerdictHasAGiteaEvent(t *testing.T) {
	// The unsupported-verdict branch in SubmitReview is unreachable exactly
	// while these two tables agree, and this is what keeps them agreeing: a
	// verdict added to ReviewSubmission's closed set without a Gitea event
	// would otherwise be refused only at runtime, on a real pull request.
	for verdict := range submittableVerdicts {
		if _, ok := giteaReviewStates[verdict]; !ok {
			t.Fatalf("verdict %s is submittable but gitea has no event for it", verdict)
		}
	}
	for verdict := range giteaReviewStates {
		if !submittableVerdicts[verdict] {
			t.Fatalf("gitea has an event for %s but it is not submittable", verdict)
		}
	}
}

func TestSubmitReview_ATransportFailureIsReported(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/reviews", http.StatusInternalServerError,
		map[string]any{"message": "boom"})
	if _, err := backend.SubmitReview(context.Background(), testRepo, 41,
		ReviewSubmission{Verdict: VerdictApprove, Body: "findings", HeadSHA: "head-1"}); err == nil {
		t.Fatal("a 500 was reported as a posted verdict")
	}
}

func TestSetCommitStatus_ATransportFailureIsReported(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/statuses/head-1", http.StatusInternalServerError,
		map[string]any{"message": "boom"})
	if err := backend.SetCommitStatus(context.Background(), testRepo, "head-1",
		CommitStatus{Context: "quicbaas/review", Status: CheckSuccess}); err == nil {
		t.Fatal("a 500 was reported as a published status")
	}
	// And a read-back that itself fails is a fault, not a verified write.
	backend2, fixture2 := newGiteaFixture(t)
	fixture2.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/statuses/head-1", http.StatusOK,
		map[string]any{"id": 1, "status": "success", "context": "quicbaas/review"})
	if err := backend2.SetCommitStatus(context.Background(), testRepo, "head-1",
		CommitStatus{Context: "quicbaas/review", Status: CheckSuccess}); err == nil {
		t.Fatal("an unverifiable write was reported as verified")
	}
}

func TestListPullRequestFiles_ATransportFailureIsReported(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/files", http.StatusInternalServerError,
		map[string]any{"message": "boom"})
	if _, err := backend.ListPullRequestFiles(context.Background(), testRepo, 41); err == nil {
		t.Fatal("a 500 was reported as an empty change")
	}
}

func TestListReviews_ATransportFailureIsReported(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/reviews", http.StatusInternalServerError,
		map[string]any{"message": "boom"})
	if _, err := backend.ListReviews(context.Background(), testRepo, 41); err == nil {
		t.Fatal("a 500 was reported as a pull request with no reviews")
	}
}
