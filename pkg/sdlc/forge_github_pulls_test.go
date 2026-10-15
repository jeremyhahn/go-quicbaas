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
	"strings"
	"testing"
)

const (
	githubPullPath   = "/repos/acme/widgets/pulls/12"
	githubMergePath  = "PUT " + githubPullPath + "/merge"
	githubHeadBranch = "/repos/acme/widgets/branches/issue/41-retry-upload"
	githubHeadRef    = "/repos/acme/widgets/git/refs/heads/issue/41-retry-upload"
)

// githubPullAs loads a pull request fixture with fields replaced.
func githubPullAs(t *testing.T, stub *forgeStub, name string, fields map[string]any) map[string]any {
	t.Helper()
	var pull map[string]any
	if err := json.Unmarshal([]byte(stub.load(name)), &pull); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	for key, value := range fields {
		pull[key] = value
	}
	return pull
}

// githubBranchAt is a branch read answering sha.
func githubBranchAt(name, sha string) map[string]any {
	return map[string]any{"name": name, "commit": map[string]string{"sha": sha}}
}

// squash is the merge most callers make.
var squash = MergeSpec{Method: "squash", CommitTitle: "Retry the flaky upload (#12)", CommitMessage: "Closes #41"}

// =============================================================================
// Tags and branches
// =============================================================================

// TestGitHubBackend_GetTag_PeelsAnAnnotatedTag proves a release check sees
// the commit, not the tag object.
func TestGitHubBackend_GetTag_PeelsAnAnnotatedTag(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/git/ref/tags/v1.2.0", http.StatusOK,
		map[string]any{"ref": "refs/tags/v1.2.0", "object": map[string]string{"sha": "aaa", "type": "tag"}})
	stub.json("GET /repos/acme/widgets/git/tags/aaa", http.StatusOK,
		map[string]any{"object": map[string]string{"sha": githubHeadSHA, "type": "commit"}})

	tag, err := backend.GetTag(context.Background(), githubRepo, "v1.2.0")
	if err != nil || tag.SHA != githubHeadSHA || tag.Name != "v1.2.0" {
		t.Fatalf("GetTag = %+v, %v", tag, err)
	}
}

// TestGitHubBackend_GetTag_Refusals covers an empty name, a missing tag, a
// tag object that cannot be read and a chain that never reaches a commit.
func TestGitHubBackend_GetTag_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	if _, err := backend.GetTag(context.Background(), githubRepo, " "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty err = %v, want ErrInvalidInput", err)
	}
	if _, err := backend.GetTag(context.Background(), githubRepo, "v9"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v, want ErrNotFound", err)
	}

	stub.json("GET /repos/acme/widgets/git/ref/tags/v1.2.0", http.StatusOK,
		map[string]any{"object": map[string]string{"sha": "aaa", "type": "tag"}})
	if _, err := backend.GetTag(context.Background(), githubRepo, "v1.2.0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unreadable tag object err = %v, want ErrNotFound", err)
	}

	stub.json("GET /repos/acme/widgets/git/tags/aaa", http.StatusOK,
		map[string]any{"object": map[string]string{"sha": "aaa", "type": "tag"}})
	_, err := backend.GetTag(context.Background(), githubRepo, "v1.2.0")
	if !errors.Is(err, errGitHubTagChain) {
		t.Errorf("loop err = %v, want errGitHubTagChain", err)
	}
	if n := len(stub.requests("GET /repos/acme/widgets/git/tags/aaa")); n != 1+maxTagPeels {
		t.Errorf("peels = %d, want %d", n, 1+maxTagPeels)
	}
}

// TestGitHubBackend_CreateBranch resolves the source to a commit, creates
// the ref and reads it back.
func TestGitHubBackend_CreateBranch(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/commits/main", http.StatusOK, map[string]string{"sha": githubHeadSHA})
	stub.json("POST /repos/acme/widgets/git/refs", http.StatusCreated, nil)
	stub.json("GET "+githubHeadBranch, http.StatusOK, githubBranchAt("issue/41-retry-upload", githubHeadSHA))

	branch, err := backend.CreateBranch(context.Background(), githubRepo, "issue/41-retry-upload", "main")
	if err != nil || branch.SHA != githubHeadSHA {
		t.Fatalf("CreateBranch = %+v, %v", branch, err)
	}
	body := decodeBody(t, stub.requests("POST /repos/acme/widgets/git/refs")[0])
	if body["ref"] != "refs/heads/issue/41-retry-upload" || body["sha"] != githubHeadSHA {
		t.Errorf("body = %v", body)
	}
}

// TestGitHubBackend_CreateBranch_Refusals covers missing names, a source that
// does not resolve, a refused create and a branch that is not there after.
func TestGitHubBackend_CreateBranch_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	if _, err := backend.CreateBranch(context.Background(), githubRepo, "", "main"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no name err = %v", err)
	}
	if _, err := backend.CreateBranch(context.Background(), githubRepo, "b", ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no source err = %v", err)
	}
	if _, err := backend.CreateBranch(context.Background(), githubRepo, "b", "nowhere"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unresolved source err = %v, want ErrNotFound", err)
	}

	stub.json("GET /repos/acme/widgets/commits/main", http.StatusOK, map[string]string{})
	if _, err := backend.CreateBranch(context.Background(), githubRepo, "b", "main"); !errors.Is(err, ErrIncompleteResponse) {
		t.Errorf("no sha err = %v, want ErrIncompleteResponse", err)
	}

	stub.json("GET /repos/acme/widgets/commits/main", http.StatusOK, map[string]string{"sha": githubHeadSHA})
	stub.json("POST /repos/acme/widgets/git/refs", http.StatusUnprocessableEntity, map[string]string{"message": "Reference already exists"})
	if _, err := backend.CreateBranch(context.Background(), githubRepo, "b", "main"); forgeStatusOf(err) != http.StatusUnprocessableEntity {
		t.Errorf("conflict err = %v, want the 422", err)
	}

	stub.json("POST /repos/acme/widgets/git/refs", http.StatusCreated, nil)
	if _, err := backend.CreateBranch(context.Background(), githubRepo, "b", "main"); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("absent after err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("GET /repos/acme/widgets/branches/b", http.StatusForbidden, nil)
	if _, err := backend.CreateBranch(context.Background(), githubRepo, "b", "main"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("read-back err = %v, want the 403", err)
	}
}

// TestGitHubBackend_DeleteBranch accepts a branch already gone, by either of
// GitHub's answers for it.
func TestGitHubBackend_DeleteBranch(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound, http.StatusUnprocessableEntity} {
		backend, stub := newGitHubStub(t)
		stub.json("DELETE "+githubHeadRef, status, nil)
		if err := backend.DeleteBranch(context.Background(), githubRepo, "issue/41-retry-upload"); err != nil {
			t.Errorf("delete answered %d: %v", status, err)
		}
	}
}

// TestGitHubBackend_DeleteBranch_Refusals covers a refused delete, a branch
// still present and a read-back that fails.
func TestGitHubBackend_DeleteBranch_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("DELETE "+githubHeadRef, http.StatusForbidden, nil)
	if err := backend.DeleteBranch(context.Background(), githubRepo, "issue/41-retry-upload"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v, want the 403", err)
	}

	stub.json("DELETE "+githubHeadRef, http.StatusNoContent, nil)
	stub.json("GET "+githubHeadBranch, http.StatusOK, githubBranchAt("issue/41-retry-upload", githubHeadSHA))
	if err := backend.DeleteBranch(context.Background(), githubRepo, "issue/41-retry-upload"); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("still present err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("GET "+githubHeadBranch, http.StatusUnauthorized, nil)
	if err := backend.DeleteBranch(context.Background(), githubRepo, "issue/41-retry-upload"); forgeStatusOf(err) != http.StatusUnauthorized {
		t.Errorf("read-back err = %v, want the 401", err)
	}
}

// =============================================================================
// Reading pull requests
// =============================================================================

// TestGitHubBackend_GetPullRequest_OpenCarriesNoMergeSHA proves the test
// merge GitHub reports on an open pull request is never read as a landing.
func TestGitHubBackend_GetPullRequest_OpenCarriesNoMergeSHA(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_open.json")

	pull, err := backend.GetPullRequest(context.Background(), githubRepo, 12)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if pull.MergeSHA != "" || pull.Merged || mergeLanded(pull) {
		t.Errorf("open pull = %+v, want no merge", pull)
	}
	if !pull.Mergeable || pull.HeadSHA != githubHeadSHA || pull.HeadRepo != githubRepo || pull.Author != "agent-widgets" {
		t.Errorf("pull = %+v", pull)
	}

	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_merged.json")
	merged, err := backend.GetPullRequest(context.Background(), githubRepo, 12)
	if err != nil || merged.MergeSHA != githubMergeSHA || !mergeLanded(merged) {
		t.Errorf("merged pull = %+v, %v", merged, err)
	}
}

// TestGitHubBackend_GetPullRequest_CarriesItsLabels proves a pull request
// reads back with its label names, which unparking matches against, and that a
// refused read is reported.
func TestGitHubBackend_GetPullRequest_CarriesItsLabels(t *testing.T) {
	backend, stub := newGitHubStub(t)
	labels := []map[string]any{{"name": "Factory:Parked"}, {"name": "bug"}}
	stub.json("GET "+githubPullPath, http.StatusOK, githubPullAs(t, stub, "pull_open.json", map[string]any{"labels": labels}))

	pull, err := backend.GetPullRequest(context.Background(), githubRepo, 12)
	if err != nil {
		t.Fatalf("GetPullRequest: %v", err)
	}
	if len(pull.Labels) != 2 || pull.Labels[0] != "Factory:Parked" || pull.Labels[1] != "bug" {
		t.Errorf("labels = %v, want Factory:Parked and bug", pull.Labels)
	}

	stub.json("GET "+githubPullPath, http.StatusNotFound, nil)
	if _, err := backend.GetPullRequest(context.Background(), githubRepo, 12); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestGitHubBackend_ListOpenPullRequests reads the listing, which reports a
// merge only by merged_at, and refuses a refused listing.
func TestGitHubBackend_ListOpenPullRequests(t *testing.T) {
	backend, stub := newGitHubStub(t)
	listed := githubPullAs(t, stub, "pull_merged.json", nil)
	delete(listed, "merged")
	stub.json("GET /repos/acme/widgets/pulls", http.StatusOK, []any{githubPullAs(t, stub, "pull_open.json", nil), listed})

	pulls, err := backend.ListOpenPullRequests(context.Background(), githubRepo)
	if err != nil || len(pulls) != 2 || pulls[0].Merged || !pulls[1].Merged {
		t.Fatalf("ListOpenPullRequests = %+v, %v", pulls, err)
	}
	if q := stub.requests("GET /repos/acme/widgets/pulls")[0].Query; !strings.Contains(q, "state=open") {
		t.Errorf("query = %q", q)
	}

	stub.json("GET /repos/acme/widgets/pulls", http.StatusForbidden, nil)
	if _, err := backend.ListOpenPullRequests(context.Background(), githubRepo); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("err = %v, want the 403", err)
	}
}

// TestGitHubBackend_ListPullRequestFiles maps a changed file.
func TestGitHubBackend_ListPullRequestFiles(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET "+githubPullPath+"/files", http.StatusOK, []map[string]any{
		{"filename": "b.go", "previous_filename": "a.go", "status": "Renamed", "additions": 3, "deletions": 1},
	})

	files, err := backend.ListPullRequestFiles(context.Background(), githubRepo, 12)
	if err != nil || len(files) != 1 {
		t.Fatalf("ListPullRequestFiles = %+v, %v", files, err)
	}
	if f := files[0]; f.Path != "b.go" || f.PreviousPath != "a.go" || f.Status != "renamed" || f.Additions != 3 || f.Deletions != 1 {
		t.Errorf("file = %+v", f)
	}

	stub.json("GET "+githubPullPath+"/files", http.StatusNotFound, nil)
	if _, err := backend.ListPullRequestFiles(context.Background(), githubRepo, 12); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// =============================================================================
// CreatePullRequest
// =============================================================================

var githubPullSpec = PullRequestSpec{
	Title: "Retry the flaky upload (#41)", Body: "Closes #41",
	HeadRef: "issue/41-retry-upload", BaseRef: "main",
}

// TestGitHubBackend_CreatePullRequest opens, reads back and labels.
func TestGitHubBackend_CreatePullRequest(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/pulls", http.StatusOK, []any{})
	stub.json("POST /repos/acme/widgets/pulls", http.StatusCreated, map[string]int{"number": 12})
	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_open.json")
	githubLabelsDefined(stub, "task:review")
	stub.json("PUT /repos/acme/widgets/issues/12/labels", http.StatusOK, nil)
	stub.json("GET /repos/acme/widgets/issues/12", http.StatusOK, githubIssueAt(12, "t", "open", "task:review"))

	spec := githubPullSpec
	spec.Labels = []string{"task:review"}
	pull, err := backend.CreatePullRequest(context.Background(), githubRepo, spec)
	if err != nil || pull.Number != 12 {
		t.Fatalf("CreatePullRequest = %+v, %v", pull, err)
	}
	body := decodeBody(t, stub.requests("POST /repos/acme/widgets/pulls")[0])
	if body["head"] != spec.HeadRef || body["base"] != "main" || body["draft"] != false {
		t.Errorf("body = %v", body)
	}
}

// TestGitHubBackend_CreatePullRequest_AdoptsTheOpenOne proves a pull request
// already open for the same refs is adopted, before the create and after a
// 422 duplicate refusal alike.
func TestGitHubBackend_CreatePullRequest_AdoptsTheOpenOne(t *testing.T) {
	backend, stub := newGitHubStub(t)
	open := githubPullAs(t, stub, "pull_open.json", nil)
	stub.json("GET /repos/acme/widgets/pulls", http.StatusOK, []any{open})
	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_open.json")

	pull, err := backend.CreatePullRequest(context.Background(), githubRepo, githubPullSpec)
	if err != nil || pull.Number != 12 || len(stub.writes()) != 0 {
		t.Fatalf("adopt before = %+v, %v with %d writes", pull, err, len(stub.writes()))
	}

	stub.sequence("GET /repos/acme/widgets/pulls", forgeJSON(http.StatusOK, []any{}), forgeJSON(http.StatusOK, []any{open}))
	stub.json("POST /repos/acme/widgets/pulls", http.StatusUnprocessableEntity,
		map[string]string{"message": "A pull request already exists"})
	pull, err = backend.CreatePullRequest(context.Background(), githubRepo, githubPullSpec)
	if err != nil || pull.Number != 12 {
		t.Fatalf("adopt after 422 = %+v, %v", pull, err)
	}
}

// TestGitHubBackend_CreatePullRequest_Refusals covers bad input, a refused
// create, a create naming no number and read-backs that disagree.
func TestGitHubBackend_CreatePullRequest_Refusals(t *testing.T) {
	ctx := context.Background()
	for name, spec := range map[string]PullRequestSpec{
		"no title": {HeadRef: "h", BaseRef: "b"},
		"no head":  {Title: "t", BaseRef: "b"},
		"no base":  {Title: "t", HeadRef: "h"},
	} {
		backend, _ := newGitHubStub(t)
		if _, err := backend.CreatePullRequest(ctx, githubRepo, spec); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s err = %v, want ErrInvalidInput", name, err)
		}
	}

	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/pulls", http.StatusOK, []any{})
	stub.json("POST /repos/acme/widgets/pulls", http.StatusForbidden, nil)
	if _, err := backend.CreatePullRequest(ctx, githubRepo, githubPullSpec); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v, want the 403", err)
	}

	stub.json("POST /repos/acme/widgets/pulls", http.StatusCreated, map[string]int{"number": 0})
	if _, err := backend.CreatePullRequest(ctx, githubRepo, githubPullSpec); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("no number err = %v, want ErrForgeWriteNotApplied", err)
	}

	stub.json("POST /repos/acme/widgets/pulls", http.StatusCreated, map[string]int{"number": 12})
	if _, err := backend.CreatePullRequest(ctx, githubRepo, githubPullSpec); !errors.Is(err, ErrNotFound) {
		t.Errorf("unreadable err = %v, want ErrNotFound", err)
	}

	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_open.json")
	draft := githubPullSpec
	draft.Draft = true
	if _, err := backend.CreatePullRequest(ctx, githubRepo, draft); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("not draft err = %v, want ErrForgeWriteNotApplied", err)
	}
	otherBase := githubPullSpec
	otherBase.BaseRef = "release"
	if _, err := backend.CreatePullRequest(ctx, githubRepo, otherBase); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("other refs err = %v, want ErrForgeWriteNotApplied", err)
	}
	labelled := githubPullSpec
	labelled.Labels = []string{"typo"}
	githubLabelsDefined(stub, "task:review")
	if _, err := backend.CreatePullRequest(ctx, githubRepo, labelled); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown label err = %v, want ErrInvalidInput", err)
	}
}

// TestGitHubBackend_CreatePullRequest_UnansweredIsUnconfirmed proves an
// unanswered create that never shows up is reported, not repeated forever.
func TestGitHubBackend_CreatePullRequest_UnansweredIsUnconfirmed(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/pulls", http.StatusOK, []any{})
	stub.json("POST /repos/acme/widgets/pulls", http.StatusBadGateway, nil)

	_, err := backend.CreatePullRequest(context.Background(), githubRepo, githubPullSpec)
	if !errors.Is(err, ErrForgeWriteUnconfirmed) {
		t.Fatalf("err = %v, want ErrForgeWriteUnconfirmed", err)
	}
}

// =============================================================================
// ClosePullRequest
// =============================================================================

// TestGitHubBackend_ClosePullRequest closes an open pull request and leaves a
// merged one alone.
func TestGitHubBackend_ClosePullRequest(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.sequence("GET "+githubPullPath,
		forgeRaw(http.StatusOK, stub.load("pull_open.json")),
		forgeJSON(http.StatusOK, githubPullAs(t, stub, "pull_open.json", map[string]any{"state": "closed"})))
	stub.json("PATCH "+githubPullPath, http.StatusOK, nil)

	if err := backend.ClosePullRequest(context.Background(), githubRepo, 12); err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	if body := decodeBody(t, stub.requests("PATCH " + githubPullPath)[0]); body["state"] != "closed" {
		t.Errorf("body = %v", body)
	}

	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_merged.json")
	if err := backend.ClosePullRequest(context.Background(), githubRepo, 12); err != nil {
		t.Fatalf("ClosePullRequest on a merged pull: %v", err)
	}
	if n := len(stub.requests("PATCH " + githubPullPath)); n != 1 {
		t.Errorf("patches = %d, want only the first", n)
	}
}

// TestGitHubBackend_ClosePullRequest_Refusals covers a bad number, an
// unreadable pull, a refused PATCH, a pull still open and a failed re-read.
func TestGitHubBackend_ClosePullRequest_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitHubStub(t)
	if err := backend.ClosePullRequest(ctx, githubRepo, 0); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("zero err = %v", err)
	}
	if err := backend.ClosePullRequest(ctx, githubRepo, 12); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v", err)
	}

	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_open.json")
	stub.json("PATCH "+githubPullPath, http.StatusForbidden, nil)
	if err := backend.ClosePullRequest(ctx, githubRepo, 12); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v", err)
	}
	stub.json("PATCH "+githubPullPath, http.StatusOK, nil)
	if err := backend.ClosePullRequest(ctx, githubRepo, 12); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("still open err = %v", err)
	}
	stub.sequence("GET "+githubPullPath,
		forgeRaw(http.StatusOK, stub.load("pull_open.json")), forgeJSON(http.StatusUnauthorized, nil))
	if err := backend.ClosePullRequest(ctx, githubRepo, 12); forgeStatusOf(err) != http.StatusUnauthorized {
		t.Errorf("re-read err = %v", err)
	}
}

// =============================================================================
// MergePullRequest
// =============================================================================

// TestGitHubBackend_MergePullRequest_SendsTheReviewedHead proves the merge
// carries the approved head as GitHub's sha guard, then deletes the merged
// source branch that still points at it.
func TestGitHubBackend_MergePullRequest_SendsTheReviewedHead(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.sequence("GET "+githubPullPath,
		forgeRaw(http.StatusOK, stub.load("pull_open.json")),
		forgeRaw(http.StatusOK, stub.load("pull_merged.json")))
	stub.json(githubMergePath, http.StatusOK, map[string]any{"merged": true, "sha": githubMergeSHA})
	stub.sequence("GET "+githubHeadBranch,
		forgeJSON(http.StatusOK, githubBranchAt("issue/41-retry-upload", githubHeadSHA)),
		forgeJSON(http.StatusNotFound, nil))
	stub.json("DELETE "+githubHeadRef, http.StatusNoContent, nil)

	spec := squash
	spec.ExpectSourceDeleted = true
	if err := backend.MergePullRequest(context.Background(), githubRepo, 12, spec, githubHeadSHA); err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	body := decodeBody(t, stub.requests(githubMergePath)[0])
	if body["sha"] != githubHeadSHA || body["merge_method"] != "squash" ||
		body["commit_title"] != squash.CommitTitle || body["commit_message"] != squash.CommitMessage {
		t.Errorf("merge body = %v", body)
	}
	if n := len(stub.requests("DELETE " + githubHeadRef)); n != 1 {
		t.Errorf("branch deletes = %d, want 1", n)
	}
}

// TestGitHubBackend_MergePullRequest_HeadMovedBeforeTheMerge proves a head
// already past the review is refused without a merge being sent.
func TestGitHubBackend_MergePullRequest_HeadMovedBeforeTheMerge(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_open.json")

	err := backend.MergePullRequest(context.Background(), githubRepo, 12, squash, githubMovedSHA)
	var moved *HeadMovedError
	if !errors.As(err, &moved) || moved.Current != shortSHA(githubHeadSHA) {
		t.Fatalf("err = %v, want HeadMovedError naming the current head", err)
	}
	if len(stub.writes()) != 0 {
		t.Errorf("writes = %+v, want none", stub.writes())
	}
}

// TestGitHubBackend_MergePullRequest_409IsHeadMovedAndNeverRetried proves
// GitHub's own sha guard — the head moved between the read and the merge —
// is HeadMovedError naming both commits, sent once.
func TestGitHubBackend_MergePullRequest_409IsHeadMovedAndNeverRetried(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.sequence("GET "+githubPullPath,
		forgeRaw(http.StatusOK, stub.load("pull_open.json")),
		forgeJSON(http.StatusOK, githubPullAs(t, stub, "pull_open.json", map[string]any{
			"head": map[string]any{"ref": "issue/41-retry-upload", "sha": githubMovedSHA},
		})))
	stub.json(githubMergePath, http.StatusConflict, map[string]string{"message": "Head branch was modified"})

	err := backend.MergePullRequest(context.Background(), githubRepo, 12, squash, githubHeadSHA)
	var moved *HeadMovedError
	if !errors.Is(err, ErrHeadMoved) || !errors.As(err, &moved) {
		t.Fatalf("err = %v, want HeadMovedError", err)
	}
	if moved.Reviewed != shortSHA(githubHeadSHA) || moved.Current != shortSHA(githubMovedSHA) {
		t.Errorf("moved = %+v, want both commits named", moved)
	}
	if n := len(stub.requests(githubMergePath)); n != 1 {
		t.Errorf("merges = %d, want 1", n)
	}

	stub.sequence("GET "+githubPullPath,
		forgeRaw(http.StatusOK, stub.load("pull_open.json")), forgeJSON(http.StatusBadRequest, nil))
	err = backend.MergePullRequest(context.Background(), githubRepo, 12, squash, githubHeadSHA)
	if !errors.As(err, &moved) || moved.Current != "" {
		t.Errorf("err = %v, want HeadMovedError naming only the reviewed head", err)
	}
}

// TestGitHubBackend_MergePullRequest_405IsNotReady proves a pull request
// GitHub will not merge yet is ForgeNotReadyError for the bounded retry.
func TestGitHubBackend_MergePullRequest_405IsNotReady(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_open.json")
	stub.json(githubMergePath, http.StatusMethodNotAllowed, map[string]string{"message": "Pull Request is not mergeable"})

	err := backend.MergePullRequest(context.Background(), githubRepo, 12, squash, githubHeadSHA)
	if !errors.Is(err, ErrForgeNotReady) {
		t.Fatalf("err = %v, want ErrForgeNotReady", err)
	}
}

// TestGitHubBackend_MergePullRequest_Reconciles proves an answer that says
// nothing is settled by reading the pull request: landed is success, still
// open and mergeable is not-ready, and the merge is never sent twice.
func TestGitHubBackend_MergePullRequest_Reconciles(t *testing.T) {
	cases := []struct {
		name  string
		merge int
		after string
		want  error
	}{
		{name: "500 then landed", merge: http.StatusInternalServerError, after: "pull_merged.json"},
		{name: "502 then open", merge: http.StatusBadGateway, after: "pull_open.json", want: ErrForgeNotReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, stub := newGitHubStub(t)
			stub.sequence("GET "+githubPullPath,
				forgeRaw(http.StatusOK, stub.load("pull_open.json")),
				forgeRaw(http.StatusOK, stub.load(tc.after)))
			stub.json(githubMergePath, tc.merge, nil)

			err := backend.MergePullRequest(context.Background(), githubRepo, 12, squash, githubHeadSHA)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if n := len(stub.requests(githubMergePath)); n != 1 {
				t.Errorf("merges = %d, want 1", n)
			}
		})
	}
}

// TestGitHubBackend_MergePullRequest_AlreadyLanded proves a merge that landed
// under an earlier attempt is success without a second merge.
func TestGitHubBackend_MergePullRequest_AlreadyLanded(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_merged.json")

	if err := backend.MergePullRequest(context.Background(), githubRepo, 12, squash, githubHeadSHA); err != nil {
		t.Fatalf("MergePullRequest: %v", err)
	}
	if len(stub.writes()) != 0 {
		t.Errorf("writes = %+v, want none", stub.writes())
	}
}

// TestGitHubBackend_MergePullRequest_Refusals covers a merge method GitHub
// lacks, no reviewed head, an unreadable pull, another refusal, and a merge
// the read-back says did not happen.
func TestGitHubBackend_MergePullRequest_Refusals(t *testing.T) {
	ctx := context.Background()
	backend, stub := newGitHubStub(t)
	if err := backend.MergePullRequest(ctx, githubRepo, 12, MergeSpec{Method: "fast-forward-only"}, githubHeadSHA); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("method err = %v, want ErrForgeUnsupported", err)
	}
	if err := backend.MergePullRequest(ctx, githubRepo, 12, squash, ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no head err = %v, want ErrInvalidInput", err)
	}
	if err := backend.MergePullRequest(ctx, githubRepo, 12, squash, githubHeadSHA); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing pull err = %v, want ErrNotFound", err)
	}

	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_open.json")
	stub.json(githubMergePath, http.StatusForbidden, nil)
	if err := backend.MergePullRequest(ctx, githubRepo, 12, squash, githubHeadSHA); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v, want the 403", err)
	}

	stub.json(githubMergePath, http.StatusOK, nil)
	if err := backend.MergePullRequest(ctx, githubRepo, 12, squash, githubHeadSHA); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("not merged err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.sequence("GET "+githubPullPath,
		forgeRaw(http.StatusOK, stub.load("pull_open.json")), forgeJSON(http.StatusUnauthorized, nil))
	if err := backend.MergePullRequest(ctx, githubRepo, 12, squash, githubHeadSHA); forgeStatusOf(err) != http.StatusUnauthorized {
		t.Errorf("read-back err = %v, want the 401", err)
	}
}

// TestGitHubBackend_DeleteMergedSource_KeepsWorkNobodyMerged proves the
// source branch survives when it moved after the merge, belongs to a fork, or
// is already gone — and that a failed branch read is reported.
func TestGitHubBackend_DeleteMergedSource_KeepsWorkNobodyMerged(t *testing.T) {
	ctx := context.Background()
	spec := squash
	spec.ExpectSourceDeleted = true

	backend, stub := newGitHubStub(t)
	stub.fixture("GET "+githubPullPath, http.StatusOK, "pull_merged.json")
	if err := backend.MergePullRequest(ctx, githubRepo, 12, spec, githubHeadSHA); err != nil {
		t.Fatalf("branch already gone: %v", err)
	}
	stub.json("GET "+githubHeadBranch, http.StatusOK, githubBranchAt("issue/41-retry-upload", githubMovedSHA))
	if err := backend.MergePullRequest(ctx, githubRepo, 12, spec, githubHeadSHA); err != nil {
		t.Fatalf("branch moved: %v", err)
	}
	stub.json("GET "+githubHeadBranch, http.StatusForbidden, nil)
	if err := backend.MergePullRequest(ctx, githubRepo, 12, spec, githubHeadSHA); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("unreadable branch err = %v, want the 403", err)
	}

	stub.json("GET "+githubPullPath, http.StatusOK, githubPullAs(t, stub, "pull_merged.json", map[string]any{
		"head": map[string]any{"ref": "issue/41-retry-upload", "sha": githubHeadSHA,
			"repo": map[string]any{"name": "widgets", "owner": map[string]string{"login": "contributor"}}},
	}))
	if err := backend.MergePullRequest(ctx, githubRepo, 12, spec, githubHeadSHA); err != nil {
		t.Fatalf("fork branch: %v", err)
	}
	if n := len(stub.requests("DELETE " + githubHeadRef)); n != 0 {
		t.Errorf("branch deletes = %d, want none", n)
	}
}
