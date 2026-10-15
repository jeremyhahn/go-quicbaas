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
	"testing"
)

const (
	glMergeRequests = glProject + "/merge_requests"
	glMergeRequest  = glMergeRequests + "/5"
	glMerge         = glMergeRequest + "/merge"
	glSourceBranch  = glProject + "/repository/branches/feature%2Fx"
)

// gitlabMergeStub serves an open merge request at gitlabHeadSHA, then the
// given answer to every later read, on a project that merges with merge
// commits.
func gitlabMergeStub(t *testing.T, after string) (*GitLabBackend, *forgeStub) {
	t.Helper()
	backend, stub := newGitLabStub(t)
	stub.sequence("GET "+glMergeRequest,
		forgeRaw(http.StatusOK, stub.load("mr_open.json")),
		forgeRaw(http.StatusOK, stub.load(after)))
	stub.fixture("GET "+glProject, http.StatusOK, "project.json")
	return backend, stub
}

// TestGitLabBackend_GetPullRequest_ReadsAMergeRequest proves a merge request
// is a pull request numbered by its iid, and a merged one carries the landed
// commit.
func TestGitLabBackend_GetPullRequest_ReadsAMergeRequest(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glMergeRequest, http.StatusOK, "mr_open.json")
	pull, err := backend.GetPullRequest(context.Background(), gitlabRepo, 5)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pull.Number != 5 || pull.State != "open" || pull.HeadRef != "feature/x" || pull.BaseRef != "main" ||
		pull.HeadSHA != gitlabHeadSHA || !pull.Mergeable || pull.HeadRepo != gitlabRepo || pull.Author != "fixer" {
		t.Fatalf("pull = %+v", pull)
	}

	stub.fixture("GET "+glMergeRequest, http.StatusOK, "mr_merged.json")
	merged, err := backend.GetPullRequest(context.Background(), gitlabRepo, 5)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if !merged.Merged || merged.State != "closed" || merged.MergeSHA != gitlabMergeSHA {
		t.Fatalf("merged = %+v", merged)
	}
}

// TestGitLabBackend_GetPullRequest_Missing proves a missing merge request is
// ErrNotFound.
func TestGitLabBackend_GetPullRequest_Missing(t *testing.T) {
	backend, _ := newGitLabStub(t)
	if _, err := backend.GetPullRequest(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestGitLabMergeRequest_LandedCommit proves the landed commit falls back to
// the squash commit, then the head, for merges GitLab records no merge
// commit for.
func TestGitLabMergeRequest_LandedCommit(t *testing.T) {
	squashed := gitlabMergeRequest{State: "merged", SHA: gitlabHeadSHA, SquashCommitSHA: gitlabMergeSHA}
	if got := squashed.toForgePullRequest(gitlabRepo).MergeSHA; got != gitlabMergeSHA {
		t.Errorf("squash MergeSHA = %q", got)
	}
	fastForward := gitlabMergeRequest{State: "merged", SHA: gitlabHeadSHA}
	if got := fastForward.toForgePullRequest(gitlabRepo).MergeSHA; got != gitlabHeadSHA {
		t.Errorf("fast-forward MergeSHA = %q", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("firstNonEmpty of nothing = %q", got)
	}
}

// TestGitLabBackend_ListOpenPullRequests_LeavesForkHeadsUnnamed proves a
// merge request from another project names no head repository, so nothing
// treats its branch as this project's.
func TestGitLabBackend_ListOpenPullRequests_LeavesForkHeadsUnnamed(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.handle("GET "+glMergeRequests, gitlabPaged(json.RawMessage(stub.load("mr_fork.json"))))
	pulls, err := backend.ListOpenPullRequests(context.Background(), gitlabRepo)
	if err != nil {
		t.Fatalf("ListOpenPullRequests: %v", err)
	}
	if len(pulls) != 1 || pulls[0].HeadRepo != (RepoRef{}) || !pulls[0].Draft || pulls[0].Mergeable {
		t.Fatalf("pulls = %+v", pulls)
	}
	if query := gitlabQuery(t, stub.requests("GET " + glMergeRequests)[0]); query.Get("state") != "opened" {
		t.Errorf("query = %v, want state opened", query)
	}
}

// TestGitLabBackend_ListOpenPullRequests_Refusals proves an endless listing
// is reported.
func TestGitLabBackend_ListOpenPullRequests_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.handle("GET "+glMergeRequests, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(gitlabNextPageHeader, "2")
		forgeRaw(http.StatusOK, stub.load("mr_fork.json"))(w, r)
	})
	if _, err := backend.ListOpenPullRequests(context.Background(), gitlabRepo); !errors.Is(err, errOpenPullPagesExhausted) {
		t.Fatalf("err = %v, want the page ceiling", err)
	}
}

// TestGitLabBackend_CreatePullRequest_OpensADraftWithLabels proves a draft
// is opened with GitLab's title prefix and labelled afterwards.
func TestGitLabBackend_CreatePullRequest_OpensADraftWithLabels(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glMergeRequests, http.StatusOK, []any{})
	stub.json("POST "+glMergeRequests, http.StatusCreated, map[string]any{"iid": 5})
	stub.json("GET "+glMergeRequest, http.StatusOK, map[string]any{
		"iid": 5, "state": "opened", "draft": true, "source_branch": "feature/x", "target_branch": "main",
	})
	stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")
	stub.json("PUT "+glMergeRequest, http.StatusOK, map[string]any{"labels": []string{"bug"}})

	pull, err := backend.CreatePullRequest(context.Background(), gitlabRepo, PullRequestSpec{
		Title: "Stop the drift", Body: "Fixes #7.", HeadRef: "feature/x", BaseRef: "main", Draft: true, Labels: []string{"BUG"},
	})
	if err != nil || pull.Number != 5 {
		t.Fatalf("CreatePullRequest = %+v, %v", pull, err)
	}
	created := decodeBody(t, stub.requests("POST " + glMergeRequests)[0])
	if created["title"] != "Draft: Stop the drift" || created["source_branch"] != "feature/x" || created["target_branch"] != "main" {
		t.Errorf("create body = %v", created)
	}
	if labelled := decodeBody(t, stub.requests("PUT " + glMergeRequest)[0]); labelled["labels"] != "bug" {
		t.Errorf("label body = %v", labelled)
	}
}

// TestGitLabBackend_CreatePullRequest_AdoptsTheExistingRequest proves an
// open merge request for the same refs is returned rather than duplicated,
// whether found before the write or after GitLab's 409.
func TestGitLabBackend_CreatePullRequest_AdoptsTheExistingRequest(t *testing.T) {
	spec := PullRequestSpec{Title: "Stop the drift", HeadRef: "feature/x", BaseRef: "main"}
	t.Run("before", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.handle("GET "+glMergeRequests, gitlabPaged(json.RawMessage(`[`+stub.load("mr_open.json")+`]`)))
		stub.fixture("GET "+glMergeRequest, http.StatusOK, "mr_open.json")
		pull, err := backend.CreatePullRequest(context.Background(), gitlabRepo, spec)
		if err != nil || pull.Number != 5 {
			t.Fatalf("CreatePullRequest = %+v, %v", pull, err)
		}
		if writes := stub.writes(); len(writes) != 0 {
			t.Errorf("writes = %v, want none", writes)
		}
	})
	t.Run("after a conflict", func(t *testing.T) {
		backend, stub := newGitLabStub(t)
		stub.sequence("GET "+glMergeRequests,
			forgeJSON(http.StatusOK, []any{}),
			forgeRaw(http.StatusOK, `[`+stub.load("mr_open.json")+`]`))
		stub.json("POST "+glMergeRequests, http.StatusConflict, map[string]any{"message": []string{"Another open merge request already exists"}})
		stub.fixture("GET "+glMergeRequest, http.StatusOK, "mr_open.json")
		pull, err := backend.CreatePullRequest(context.Background(), gitlabRepo, spec)
		if err != nil || pull.Number != 5 {
			t.Fatalf("CreatePullRequest = %+v, %v", pull, err)
		}
	})
}

// TestGitLabBackend_CreatePullRequest_Refusals proves missing fields are
// refused and a merge request created other than asked is reported.
func TestGitLabBackend_CreatePullRequest_Refusals(t *testing.T) {
	ctx := context.Background()
	full := PullRequestSpec{Title: "t", HeadRef: "feature/x", BaseRef: "main"}
	backend, stub := newGitLabStub(t)
	for name, spec := range map[string]PullRequestSpec{
		"title": {HeadRef: "h", BaseRef: "b"},
		"head":  {Title: "t", BaseRef: "b"},
		"base":  {Title: "t", HeadRef: "h"},
	} {
		if _, err := backend.CreatePullRequest(ctx, gitlabRepo, spec); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("no %s err = %v, want ErrInvalidInput", name, err)
		}
	}

	stub.json("GET "+glMergeRequests, http.StatusOK, []any{})
	stub.json("POST "+glMergeRequests, http.StatusCreated, map[string]any{"iid": 0})
	if _, err := backend.CreatePullRequest(ctx, gitlabRepo, full); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("no iid err = %v, want ErrForgeWriteNotApplied", err)
	}

	stub.json("POST "+glMergeRequests, http.StatusCreated, map[string]any{"iid": 5})
	stub.json("GET "+glMergeRequest, http.StatusOK, map[string]any{"iid": 5, "source_branch": "feature/x", "target_branch": "develop"})
	if _, err := backend.CreatePullRequest(ctx, gitlabRepo, full); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("wrong target err = %v, want ErrForgeWriteNotApplied", err)
	}

	stub.fixture("GET "+glMergeRequest, http.StatusOK, "mr_open.json")
	draft := full
	draft.Draft = true
	if _, err := backend.CreatePullRequest(ctx, gitlabRepo, draft); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("not a draft err = %v, want ErrForgeWriteNotApplied", err)
	}

	labelled := full
	labelled.Labels = []string{"nope"}
	stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")
	if _, err := backend.CreatePullRequest(ctx, gitlabRepo, labelled); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("undefined label err = %v, want ErrInvalidInput", err)
	}
	labelled.Labels = []string{"bug"}
	stub.json("PUT "+glMergeRequest, http.StatusOK, map[string]any{"labels": []string{}})
	if _, err := backend.CreatePullRequest(ctx, gitlabRepo, labelled); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("dropped label err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("PUT "+glMergeRequest, http.StatusForbidden, nil)
	if _, err := backend.CreatePullRequest(ctx, gitlabRepo, labelled); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("label write err = %v, want 403", err)
	}

	stub.json("POST "+glMergeRequests, http.StatusForbidden, nil)
	if _, err := backend.CreatePullRequest(ctx, gitlabRepo, full); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("create err = %v, want 403", err)
	}
	stub.json("POST "+glMergeRequests, http.StatusCreated, map[string]any{"iid": 5})
	stub.json("GET "+glMergeRequest, http.StatusNotFound, nil)
	if _, err := backend.CreatePullRequest(ctx, gitlabRepo, full); !errors.Is(err, ErrNotFound) {
		t.Errorf("read-back err = %v, want ErrNotFound", err)
	}
}

// TestGitLabBackend_ClosePullRequest_ClosesAnOpenRequest proves a close is a
// state_event confirmed by read-back, and closing a closed request writes
// nothing.
func TestGitLabBackend_ClosePullRequest_ClosesAnOpenRequest(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.sequence("GET "+glMergeRequest,
		forgeRaw(http.StatusOK, stub.load("mr_open.json")),
		forgeJSON(http.StatusOK, map[string]any{"iid": 5, "state": "closed"}))
	stub.json("PUT "+glMergeRequest, http.StatusOK, map[string]any{"iid": 5, "state": "closed"})

	if err := backend.ClosePullRequest(context.Background(), gitlabRepo, 5); err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	if body := decodeBody(t, stub.requests("PUT " + glMergeRequest)[0]); body["state_event"] != "close" {
		t.Errorf("body = %v", body)
	}
	if err := backend.ClosePullRequest(context.Background(), gitlabRepo, 5); err != nil {
		t.Fatalf("closing a closed request: %v", err)
	}
	if n := len(stub.requests("PUT " + glMergeRequest)); n != 1 {
		t.Errorf("writes = %d, want 1", n)
	}
}

// TestGitLabBackend_ClosePullRequest_Refusals proves a bad number, a refused
// write and a request still open afterwards are all reported.
func TestGitLabBackend_ClosePullRequest_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	if err := backend.ClosePullRequest(context.Background(), gitlabRepo, 0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("zero err = %v, want ErrInvalidInput", err)
	}
	if err := backend.ClosePullRequest(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v, want ErrNotFound", err)
	}
	stub.fixture("GET "+glMergeRequest, http.StatusOK, "mr_open.json")
	stub.json("PUT "+glMergeRequest, http.StatusForbidden, nil)
	if err := backend.ClosePullRequest(context.Background(), gitlabRepo, 5); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v, want 403", err)
	}
	stub.json("PUT "+glMergeRequest, http.StatusOK, nil)
	if err := backend.ClosePullRequest(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("still open err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.sequence("GET "+glMergeRequest,
		forgeRaw(http.StatusOK, stub.load("mr_open.json")),
		forgeJSON(http.StatusNotFound, nil))
	if err := backend.ClosePullRequest(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrNotFound) {
		t.Errorf("read-back err = %v, want ErrNotFound", err)
	}
}

// TestGitLabBackend_MergePullRequest_MergesAtTheReviewedHead proves the merge
// carries the reviewed sha, asks GitLab to remove the source branch, and
// confirms the branch is gone.
func TestGitLabBackend_MergePullRequest_MergesAtTheReviewedHead(t *testing.T) {
	backend, stub := gitlabMergeStub(t, "mr_merged.json")
	stub.json("PUT "+glMerge, http.StatusOK, map[string]any{"iid": 5, "state": "merged"})

	err := backend.MergePullRequest(context.Background(), gitlabRepo, 5, MergeSpec{
		Method: "merge", CommitTitle: "Stop the drift", CommitMessage: "Fixes #7.", ExpectSourceDeleted: true,
	}, gitlabHeadSHA)
	if err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	body := decodeBody(t, stub.requests("PUT " + glMerge)[0])
	if body["sha"] != gitlabHeadSHA || body["squash"] != false || body["should_remove_source_branch"] != true ||
		body["merge_commit_message"] != "Stop the drift\n\nFixes #7." {
		t.Errorf("merge body = %v", body)
	}
	if n := len(stub.requests("GET " + glSourceBranch)); n != 1 {
		t.Errorf("branch reads = %d, want the one confirming deletion", n)
	}
}

// TestGitLabBackend_MergePullRequest_SquashesAndDeletesALeftoverBranch proves
// a squash sends its message as the squash commit's, and a source branch
// GitLab left behind at the merged head is deleted.
func TestGitLabBackend_MergePullRequest_SquashesAndDeletesALeftoverBranch(t *testing.T) {
	backend, stub := gitlabMergeStub(t, "mr_merged.json")
	stub.json("PUT "+glMerge, http.StatusOK, nil)
	stub.sequence("GET "+glSourceBranch,
		forgeJSON(http.StatusOK, map[string]any{"name": "feature/x", "commit": map[string]any{"id": gitlabHeadSHA}}),
		forgeJSON(http.StatusNotFound, nil))
	stub.json("DELETE "+glSourceBranch, http.StatusNoContent, nil)

	err := backend.MergePullRequest(context.Background(), gitlabRepo, 5, MergeSpec{
		Method: "Squash", CommitTitle: "Stop the drift", ExpectSourceDeleted: true,
	}, gitlabHeadSHA)
	if err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	body := decodeBody(t, stub.requests("PUT " + glMerge)[0])
	if body["squash"] != true || body["squash_commit_message"] != "Stop the drift" {
		t.Errorf("merge body = %v", body)
	}
	if n := len(stub.requests("DELETE " + glSourceBranch)); n != 1 {
		t.Errorf("deletes = %d, want 1", n)
	}
}

// TestGitLabBackend_MergePullRequest_RefusesAMovedHead proves a head that
// moved since review is refused before the merge, and GitLab's 409 at the
// merge is HeadMovedError and never retried.
func TestGitLabBackend_MergePullRequest_RefusesAMovedHead(t *testing.T) {
	t.Run("before the merge", func(t *testing.T) {
		backend, stub := gitlabMergeStub(t, "mr_open.json")
		err := backend.MergePullRequest(context.Background(), gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabMovedSHA)
		var moved *HeadMovedError
		if !errors.As(err, &moved) {
			t.Fatalf("err = %v, want HeadMovedError", err)
		}
		if writes := stub.writes(); len(writes) != 0 {
			t.Errorf("writes = %v, want none", writes)
		}
	})
	t.Run("at the merge", func(t *testing.T) {
		backend, stub := gitlabMergeStub(t, "mr_open.json")
		stub.json("PUT "+glMerge, http.StatusConflict, map[string]string{"message": "SHA does not match HEAD of source branch"})
		err := backend.MergePullRequest(context.Background(), gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA)
		var moved *HeadMovedError
		if !errors.As(err, &moved) || !errors.Is(err, ErrHeadMoved) {
			t.Fatalf("err = %v, want HeadMovedError", err)
		}
		if n := len(stub.requests("PUT " + glMerge)); n != 1 {
			t.Errorf("merge attempts = %d, want exactly 1", n)
		}
	})
}

// TestGitLabBackend_MergePullRequest_NotMergeableIsNotReady proves GitLab's
// not-mergeable answers are ErrForgeNotReady, carrying GitLab's reason.
func TestGitLabBackend_MergePullRequest_NotMergeableIsNotReady(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			backend, stub := gitlabMergeStub(t, "mr_open.json")
			stub.json("PUT "+glMerge, status, map[string]string{"message": "Branch cannot be merged"})
			err := backend.MergePullRequest(context.Background(), gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA)
			if !errors.Is(err, ErrForgeNotReady) {
				t.Fatalf("err = %v, want ErrForgeNotReady", err)
			}
			gitlabErrorFree(t, err)
		})
	}
}

// TestGitLabBackend_MergePullRequest_ReconcilesAnUnansweredMerge proves a
// merge GitLab failed to answer is settled by reading the request back.
func TestGitLabBackend_MergePullRequest_ReconcilesAnUnansweredMerge(t *testing.T) {
	backend, stub := gitlabMergeStub(t, "mr_merged.json")
	stub.json("PUT "+glMerge, http.StatusInternalServerError, nil)
	if err := backend.MergePullRequest(context.Background(), gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA); err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}

	backend, stub = gitlabMergeStub(t, "mr_open.json")
	stub.json("PUT "+glMerge, http.StatusInternalServerError, nil)
	err := backend.MergePullRequest(context.Background(), gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA)
	if !errors.Is(err, ErrForgeNotReady) {
		t.Fatalf("still open err = %v, want ErrForgeNotReady", err)
	}
}

// TestGitLabBackend_MergePullRequest_FinishesALandedMerge proves a request an
// earlier attempt already merged is not merged again; only its branch
// cleanup runs.
func TestGitLabBackend_MergePullRequest_FinishesALandedMerge(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glMergeRequest, http.StatusOK, "mr_merged.json")
	err := backend.MergePullRequest(context.Background(), gitlabRepo, 5, MergeSpec{Method: "merge", ExpectSourceDeleted: true}, gitlabHeadSHA)
	if err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	if n := len(stub.requests("PUT " + glMerge)); n != 0 {
		t.Errorf("merge attempts = %d, want none", n)
	}
}

// TestGitLabBackend_MergePullRequest_Refusals proves an unknown method, a
// missing head, a project configured against the method, a refused read and
// a merge that did not land are each reported.
func TestGitLabBackend_MergePullRequest_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := gitlabMergeStub(t, "mr_open.json")
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "octopus"}, gitlabHeadSHA); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("octopus err = %v, want ErrForgeUnsupported", err)
	}
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "merge"}, ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no head err = %v, want ErrInvalidInput", err)
	}

	stub.json("GET "+glProject, http.StatusOK, map[string]string{"merge_method": "ff"})
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("merge on ff err = %v, want ErrForgeUnsupported", err)
	}
	stub.json("GET "+glProject, http.StatusOK, map[string]string{"merge_method": "merge", "squash_option": "never"})
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "squash"}, gitlabHeadSHA); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("squash on never err = %v, want ErrForgeUnsupported", err)
	}
	stub.json("GET "+glProject, http.StatusForbidden, nil)
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("project read err = %v, want 403", err)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Fatalf("writes = %v, want none before any refusal", writes)
	}

	stub.fixture("GET "+glProject, http.StatusOK, "project.json")
	stub.json("PUT "+glMerge, http.StatusOK, nil)
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("not merged err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("PUT "+glMerge, http.StatusUnauthorized, map[string]string{"message": "401 Unauthorized"})
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA); forgeStatusOf(err) != http.StatusUnauthorized {
		t.Errorf("unauthorized err = %v, want 401", err)
	}

	stub.sequence("GET "+glMergeRequest,
		forgeRaw(http.StatusOK, stub.load("mr_open.json")),
		forgeJSON(http.StatusNotFound, nil))
	stub.json("PUT "+glMerge, http.StatusOK, nil)
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA); !errors.Is(err, ErrNotFound) {
		t.Errorf("read-back err = %v, want ErrNotFound", err)
	}
	stub.json("GET "+glMergeRequest, http.StatusNotFound, nil)
	if err := backend.MergePullRequest(ctx, gitlabRepo, 5, MergeSpec{Method: "merge"}, gitlabHeadSHA); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v, want ErrNotFound", err)
	}
}

// TestGitLabBackend_ListPullRequestFiles_CountsEachDiff proves each diff is a
// changed file with its kind and line counts.
func TestGitLabBackend_ListPullRequestFiles_CountsEachDiff(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glMergeRequest+"/diffs", http.StatusOK, "diffs.json")
	files, err := backend.ListPullRequestFiles(context.Background(), gitlabRepo, 5)
	if err != nil {
		t.Fatalf("ListPullRequestFiles: %v", err)
	}
	want := []ForgeChangedFile{
		{Path: "widget.go", Status: "modified", Additions: 1, Deletions: 1},
		{Path: "anchor.go", Status: "added", Additions: 2},
		{Path: "legacy.go", Status: "deleted", Deletions: 1},
		{Path: "new_name.go", Status: "renamed", PreviousPath: "old_name.go"},
	}
	if len(files) != len(want) {
		t.Fatalf("files = %+v", files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Errorf("file %d = %+v, want %+v", i, files[i], want[i])
		}
	}
}

// TestGitLabBackend_ListPullRequestFiles_Missing proves a missing merge
// request is ErrNotFound.
func TestGitLabBackend_ListPullRequestFiles_Missing(t *testing.T) {
	backend, _ := newGitLabStub(t)
	if _, err := backend.ListPullRequestFiles(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestGitLabBackend_Branches_CreateReadDelete proves a branch is created from
// a ref and confirmed, read with its encoded name, and deleted and confirmed
// gone.
func TestGitLabBackend_Branches_CreateReadDelete(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("POST "+glProject+"/repository/branches", http.StatusCreated, nil)
	stub.sequence("GET "+glSourceBranch,
		forgeJSON(http.StatusOK, map[string]any{"name": "feature/x", "commit": map[string]any{"id": gitlabHeadSHA}}),
		forgeJSON(http.StatusNotFound, nil))
	stub.json("DELETE "+glSourceBranch, http.StatusNoContent, nil)

	branch, err := backend.CreateBranch(context.Background(), gitlabRepo, "feature/x", "main")
	if err != nil || branch.SHA != gitlabHeadSHA {
		t.Fatalf("CreateBranch = %+v, %v", branch, err)
	}
	if body := decodeBody(t, stub.requests("POST " + glProject + "/repository/branches")[0]); body["branch"] != "feature/x" || body["ref"] != "main" {
		t.Errorf("body = %v", body)
	}
	if err := backend.DeleteBranch(context.Background(), gitlabRepo, "feature/x"); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
}

// TestGitLabBackend_Branches_Refusals proves missing names, a branch absent
// after creation, a branch present after deletion and refused calls are
// reported.
func TestGitLabBackend_Branches_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitLabStub(t)
	if _, err := backend.CreateBranch(ctx, gitlabRepo, "", "main"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no name err = %v, want ErrInvalidInput", err)
	}
	if _, err := backend.CreateBranch(ctx, gitlabRepo, "feature/x", ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no ref err = %v, want ErrInvalidInput", err)
	}
	stub.json("POST "+glProject+"/repository/branches", http.StatusBadRequest, map[string]string{"message": "Invalid reference name"})
	if _, err := backend.CreateBranch(ctx, gitlabRepo, "feature/x", "main"); forgeStatusOf(err) != http.StatusBadRequest {
		t.Errorf("refused err = %v, want 400", err)
	}
	stub.json("POST "+glProject+"/repository/branches", http.StatusCreated, nil)
	if _, err := backend.CreateBranch(ctx, gitlabRepo, "feature/x", "main"); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("absent err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("GET "+glSourceBranch, http.StatusForbidden, nil)
	if _, err := backend.CreateBranch(ctx, gitlabRepo, "feature/x", "main"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("read-back err = %v, want 403", err)
	}
	if err := backend.DeleteBranch(ctx, gitlabRepo, "feature/x"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("delete read-back err = %v, want 403", err)
	}

	stub.json("GET "+glSourceBranch, http.StatusOK, map[string]any{"name": "feature/x", "commit": map[string]any{"id": gitlabHeadSHA}})
	stub.json("DELETE "+glSourceBranch, http.StatusNoContent, nil)
	if err := backend.DeleteBranch(ctx, gitlabRepo, "feature/x"); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("still present err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("DELETE "+glSourceBranch, http.StatusForbidden, nil)
	if err := backend.DeleteBranch(ctx, gitlabRepo, "feature/x"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused delete err = %v, want 403", err)
	}
}

// TestGitLabBackend_GetTag_ReadsTheTaggedCommit proves a tag resolves to its
// commit, and a nameless, missing or commitless tag is refused.
func TestGitLabBackend_GetTag_ReadsTheTaggedCommit(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glProject+"/repository/tags/v1.2", http.StatusOK, map[string]any{"name": "v1.2", "commit": map[string]any{"id": gitlabMergeSHA}})
	tag, err := backend.GetTag(context.Background(), gitlabRepo, "v1.2")
	if err != nil || tag.SHA != gitlabMergeSHA {
		t.Fatalf("GetTag = %+v, %v", tag, err)
	}
}

// TestGitLabBackend_GetTag_Refusals proves a nameless, missing or commitless
// tag is refused.
func TestGitLabBackend_GetTag_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	if _, err := backend.GetTag(context.Background(), gitlabRepo, " "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("nameless err = %v, want ErrInvalidInput", err)
	}
	if _, err := backend.GetTag(context.Background(), gitlabRepo, "v9"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v, want ErrNotFound", err)
	}
	stub.json("GET "+glProject+"/repository/tags/v1.2", http.StatusOK, map[string]any{"name": "v1.2"})
	if _, err := backend.GetTag(context.Background(), gitlabRepo, "v1.2"); !errors.Is(err, ErrIncompleteResponse) {
		t.Errorf("commitless err = %v, want ErrIncompleteResponse", err)
	}
}

// glSameNumberIssue is the issue that shares merge request 5's number: a
// different object on GitLab, which numbers the two apart.
const glSameNumberIssue = glProject + "/issues/5"

// TestGitLabBackend_CommentOnPullRequest_NeverReachesTheIssue proves a merge
// request comment is a merge request note, even when an issue with the same
// number exists and would accept it.
func TestGitLabBackend_CommentOnPullRequest_NeverReachesTheIssue(t *testing.T) {
	backend, stub := newGitLabStub(t)
	note := map[string]any{"id": 611, "body": "Landed by train #1.", "author": map[string]any{"username": "agent-widgets"}}
	stub.json("POST "+glMergeRequest+"/notes", http.StatusCreated, note)
	stub.json("POST "+glSameNumberIssue+"/notes", http.StatusCreated, note)

	comment, err := backend.CommentOnPullRequest(context.Background(), gitlabRepo, 5, "Landed by train #1.")
	if err != nil || comment.ID != 611 || comment.Author != "agent-widgets" {
		t.Fatalf("CommentOnPullRequest = %+v, %v", comment, err)
	}
	if calls := stub.requests("POST " + glMergeRequest + "/notes"); len(calls) != 1 || decodeBody(t, calls[0])["body"] != "Landed by train #1." {
		t.Fatalf("merge request notes posted %d times, want once with the body", len(calls))
	}
	if calls := stub.requests("POST " + glSameNumberIssue + "/notes"); len(calls) != 0 {
		t.Fatalf("issue notes posted %d times, want never", len(calls))
	}
}

// TestGitLabBackend_CommentOnPullRequest_Refusals proves an empty comment, a
// missing merge request and a note stored differently are each reported.
func TestGitLabBackend_CommentOnPullRequest_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	if _, err := backend.CommentOnPullRequest(context.Background(), gitlabRepo, 5, " "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty err = %v, want ErrInvalidInput", err)
	}
	if _, err := backend.CommentOnPullRequest(context.Background(), gitlabRepo, 5, "On it."); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing merge request err = %v, want ErrNotFound", err)
	}
	stub.json("POST "+glMergeRequest+"/notes", http.StatusCreated, map[string]any{"id": 611, "body": "truncated"})
	var notApplied *ForgeWriteNotAppliedError
	_, err := backend.CommentOnPullRequest(context.Background(), gitlabRepo, 5, "On it.")
	if !errors.As(err, &notApplied) || notApplied.Operation != "CommentOnPullRequest" || notApplied.Subject != "acme/widgets!5" {
		t.Errorf("altered err = %v, want ErrForgeWriteNotApplied naming the merge request", err)
	}
}

// TestGitLabBackend_ListPullRequestComments_ReadsTheMergeRequest proves the
// discussion read is the merge request's, oldest first, with GitLab's system
// notes left out, and never the issue sharing its number.
func TestGitLabBackend_ListPullRequestComments_ReadsTheMergeRequest(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glMergeRequest+"/notes", http.StatusOK, "mr_notes.json")
	stub.fixture("GET "+glSameNumberIssue+"/notes", http.StatusOK, "notes.json")

	comments, err := backend.ListPullRequestComments(context.Background(), gitlabRepo, 5)
	if err != nil {
		t.Fatalf("ListPullRequestComments: %v", err)
	}
	if len(comments) != 2 || comments[0].ID != 606 || comments[1].ID != 607 || comments[1].Author != "reviewer-b" {
		t.Fatalf("comments = %+v, want the two notes people wrote on the merge request", comments)
	}
	query := gitlabQuery(t, stub.requests("GET " + glMergeRequest + "/notes")[0])
	if query.Get("sort") != "asc" || query.Get("order_by") != "created_at" {
		t.Errorf("query = %v, want oldest first", query)
	}
	if calls := stub.requests("GET " + glSameNumberIssue + "/notes"); len(calls) != 0 {
		t.Fatalf("issue notes read %d times, want never", len(calls))
	}
}

// TestGitLabBackend_ListPullRequestComments_MissingMergeRequest proves a
// missing merge request is ErrNotFound.
func TestGitLabBackend_ListPullRequestComments_MissingMergeRequest(t *testing.T) {
	backend, _ := newGitLabStub(t)
	if _, err := backend.ListPullRequestComments(context.Background(), gitlabRepo, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// gitlabMergeRequestLabelled answers merge request 5 carrying labels.
func gitlabMergeRequestLabelled(labels ...string) map[string]any {
	return map[string]any{"iid": 5, "state": "opened", "labels": append([]string{}, labels...)}
}

// TestGitLabBackend_PullRequestLabelEdits_NeverReachTheIssue proves a merge
// request's labels are edited on the merge request, in the project's
// spelling, and verified by reading the merge request back, even when an
// issue with the same number exists and would accept the edit.
func TestGitLabBackend_PullRequestLabelEdits_NeverReachTheIssue(t *testing.T) {
	cases := map[string]struct {
		edit  func(*GitLabBackend) error
		field string
		after []string
	}{
		"add": {
			edit: func(b *GitLabBackend) error {
				return b.AddPullRequestLabels(context.Background(), gitlabRepo, 5, []string{"factory:queued"})
			},
			field: "add_labels", after: []string{"bug", "Factory:Queued"},
		},
		"remove": {
			edit: func(b *GitLabBackend) error {
				return b.RemovePullRequestLabel(context.Background(), gitlabRepo, 5, "BUG")
			},
			field: "remove_labels", after: []string{"Factory:Queued"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")
			stub.json("PUT "+glMergeRequest, http.StatusOK, gitlabMergeRequestLabelled(tc.after...))
			stub.json("GET "+glMergeRequest, http.StatusOK, gitlabMergeRequestLabelled(tc.after...))
			stub.json("PUT "+glSameNumberIssue, http.StatusOK, gitlabIssueAnswer("opened", tc.after...))
			stub.json("GET "+glSameNumberIssue, http.StatusOK, gitlabIssueAnswer("opened", tc.after...))

			if err := tc.edit(backend); err != nil {
				t.Fatalf("edit: %v", err)
			}
			writes := stub.requests("PUT " + glMergeRequest)
			if len(writes) != 1 {
				t.Fatalf("merge request edits = %d, want one", len(writes))
			}
			if body := decodeBody(t, writes[0]); len(body) != 1 || body[tc.field] == nil {
				t.Errorf("body = %v, want only %s", body, tc.field)
			}
			for _, key := range []string{"PUT " + glSameNumberIssue, "GET " + glSameNumberIssue} {
				if calls := stub.requests(key); len(calls) != 0 {
					t.Errorf("%s called %d times, want never", key, len(calls))
				}
			}
		})
	}
}

// TestGitLabBackend_PullRequestLabelEdits_Refusals proves a label edit that
// did not take, an empty or undefined label, a refused write and a missing
// merge request are each reported.
func TestGitLabBackend_PullRequestLabelEdits_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")

	if err := backend.AddPullRequestLabels(ctx, gitlabRepo, 5, nil); err != nil {
		t.Errorf("adding nothing = %v", err)
	}
	if err := backend.RemovePullRequestLabel(ctx, gitlabRepo, 5, " "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty label err = %v, want ErrInvalidInput", err)
	}
	if err := backend.AddPullRequestLabels(ctx, gitlabRepo, 5, []string{"nope"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("undefined add err = %v, want ErrInvalidInput", err)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Errorf("writes = %v, want none before the labels are known", writes)
	}

	stub.json("PUT "+glMergeRequest, http.StatusForbidden, nil)
	if err := backend.AddPullRequestLabels(ctx, gitlabRepo, 5, []string{"bug"}); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused write err = %v, want 403", err)
	}
	stub.json("PUT "+glMergeRequest, http.StatusOK, gitlabMergeRequestLabelled("bug"))
	if err := backend.RemovePullRequestLabel(ctx, gitlabRepo, 5, "bug"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing read-back err = %v, want ErrNotFound", err)
	}
	stub.json("GET "+glMergeRequest, http.StatusOK, gitlabMergeRequestLabelled("bug"))
	if err := backend.AddPullRequestLabels(ctx, gitlabRepo, 5, []string{"factory:queued"}); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("add not applied err = %v, want ErrForgeWriteNotApplied", err)
	}
	if err := backend.RemovePullRequestLabel(ctx, gitlabRepo, 5, "bug"); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("remove not applied err = %v, want ErrForgeWriteNotApplied", err)
	}
}
