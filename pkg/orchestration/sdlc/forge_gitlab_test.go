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
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	// gitlabTestToken is a fixture credential; every test asserting on an
	// error checks it never appears there.
	gitlabTestToken = "glpat-fixture-token-never-shown"

	// gitlabAPIRoot is where the stub serves, so a path built without the
	// configured root misses every handler.
	gitlabAPIRoot = "/api/v4"

	// glProject is the stub's encoded project path for gitlabRepo.
	glProject = "/projects/acme%2Fwidgets"

	gitlabHeadSHA  = "1111111111111111111111111111111111111111"
	gitlabMovedSHA = "2222222222222222222222222222222222222222"
	gitlabMergeSHA = "3333333333333333333333333333333333333333"
)

var gitlabRepo = RepoRef{Owner: "acme", Repo: "widgets"}

// newGitLabStub builds an adapter against a stubbed GitLab API keyed by the
// path as sent, so the encoded project path is checked.
func newGitLabStub(t *testing.T) (*GitLabBackend, *forgeStub) {
	t.Helper()
	stub := newForgeStub(t, gitlabAPIRoot, "gitlab")
	stub.escaped = true
	backend, err := NewGitLabBackend(GitLabConfig{
		BaseURL: stub.api(),
		Token:   gitlabTestToken,
		Waiter:  &immediateWaiter{},
	})
	if err != nil {
		t.Fatalf("NewGitLabBackend: %v", err)
	}
	return backend, stub
}

// gitlabPaged answers the listing's page N with pages[N-1], naming the next
// page in X-Next-Page while there is one.
func gitlabPaged(pages ...any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil {
			page = n
		}
		if page < 1 || page > len(pages) {
			forgeJSON(http.StatusOK, []any{})(w, r)
			return
		}
		if page < len(pages) {
			w.Header().Set(gitlabNextPageHeader, strconv.Itoa(page+1))
		}
		forgeJSON(http.StatusOK, pages[page-1])(w, r)
	}
}

// gitlabQuery parses the query string a call sent.
func gitlabQuery(t *testing.T, call forgeCall) url.Values {
	t.Helper()
	query, err := url.ParseQuery(call.Query)
	if err != nil {
		t.Fatalf("query %q: %v", call.Query, err)
	}
	return query
}

// gitlabErrorFree fails the test when err's text carries the fixture token.
func gitlabErrorFree(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), gitlabTestToken) {
		t.Fatalf("the token reached the error: %v", err)
	}
}

// TestNewGitLabBackend_DefaultsToGitLabCom proves a binding naming no base URL
// talks to GitLab.com, the one host the adapter writes down.
func TestNewGitLabBackend_DefaultsToGitLabCom(t *testing.T) {
	backend, err := NewGitLabBackend(GitLabConfig{Token: gitlabTestToken})
	if err != nil {
		t.Fatalf("NewGitLabBackend: %v", err)
	}
	if backend.baseURL != gitlabDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", backend.baseURL, gitlabDefaultBaseURL)
	}
	if backend.Type() != ForgeGitLab {
		t.Errorf("Type = %v, want gitlab", backend.Type())
	}
}

// TestNewGitLabBackend_FailsClosedOnMissingConfig proves no adapter is built
// without a token or with an unusable transport.
func TestNewGitLabBackend_FailsClosedOnMissingConfig(t *testing.T) {
	cases := map[string]GitLabConfig{
		"no token":      {BaseURL: "https://gitlab.example/api/v4"},
		"bad transport": {Token: gitlabTestToken, Transport: ForgeTransport{ReadAttempts: -1}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			backend, err := NewGitLabBackend(cfg)
			if backend != nil {
				t.Fatal("an unusable adapter was constructed")
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// TestGitLabBackend_IsRegistered proves the factory is registered and fails
// closed without a token.
func TestGitLabBackend_IsRegistered(t *testing.T) {
	if !slices.Contains(RegisteredForgeTypes(), ForgeGitLab) {
		t.Fatalf("registered = %v, want gitlab among them", RegisteredForgeTypes())
	}
	backend, err := NewForgeBackend(ForgeConfig{Type: ForgeGitLab, BaseURL: "https://gitlab.example/api/v4"},
		ForgeCredentials{Token: gitlabTestToken}, ForgeTransport{})
	if err != nil {
		t.Fatalf("NewForgeBackend: %v", err)
	}
	if _, ok := backend.(*GitLabBackend); !ok || backend.Type() != ForgeGitLab {
		t.Fatalf("backend = %T (%v), want *GitLabBackend", backend, backend.Type())
	}
	if _, ok := backend.(ForgeOnboarding); !ok {
		t.Error("the GitLab adapter does not serve ForgeOnboarding")
	}

	_, err = NewForgeBackend(ForgeConfig{Type: ForgeGitLab}, ForgeCredentials{}, ForgeTransport{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("tokenless registration error = %v, want ErrInvalidInput", err)
	}
}

// TestGitLabBackend_CapabilitiesAreHonest proves the two flags GitLab cannot
// honour are false, and that their verbs refuse with ForgeUnsupportedError
// rather than quietly doing nothing.
func TestGitLabBackend_CapabilitiesAreHonest(t *testing.T) {
	backend, stub := newGitLabStub(t)
	caps := backend.Capabilities()
	if caps.BranchProtectionContexts || caps.SubmitReviews {
		t.Fatalf("capabilities = %+v, want branch protection contexts and review submission false", caps)
	}
	if !caps.CancelCheckRuns || !caps.RerunCheckRuns || !caps.CheckRunLogs || !caps.CommitStatuses ||
		!caps.AdditiveLabels || !caps.DraftPullRequests || !caps.SquashMerge || !caps.DeleteBranchOnMerge {
		t.Fatalf("capabilities = %+v, want every implemented verb reported", caps)
	}

	_, err := backend.BranchProtection(context.Background(), gitlabRepo, "main")
	if !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("BranchProtection err = %v, want ErrForgeUnsupported", err)
	}
	_, err = backend.SubmitReview(context.Background(), gitlabRepo, 5, ReviewSubmission{
		Verdict: VerdictApprove, HeadSHA: gitlabHeadSHA, Body: "looks right",
	})
	if !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("SubmitReview err = %v, want ErrForgeUnsupported", err)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Errorf("writes = %v, want none", writes)
	}
}

// TestGitLabBackend_RefusalsNeedAValidRequest proves the refused verbs still
// reject malformed input first, so a caller learns of its own mistake.
func TestGitLabBackend_RefusalsNeedAValidRequest(t *testing.T) {
	backend, _ := newGitLabStub(t)
	if _, err := backend.BranchProtection(context.Background(), gitlabRepo, " "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("BranchProtection err = %v, want ErrInvalidInput", err)
	}
	if _, err := backend.SubmitReview(context.Background(), gitlabRepo, 5, ReviewSubmission{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("SubmitReview err = %v, want ErrInvalidInput", err)
	}
}

// TestGitLabBackend_SendsBearerToken proves the token travels as a Bearer
// credential to the configured root.
func TestGitLabBackend_SendsBearerToken(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET /user", http.StatusOK, map[string]any{"id": 9, "username": "agent-widgets"})

	identity, err := backend.AuthenticatedUser(context.Background())
	if err != nil || identity.Username != "agent-widgets" {
		t.Fatalf("AuthenticatedUser = %+v, %v", identity, err)
	}
	if got := stub.requests("GET /user")[0].Header.Get("Authorization"); got != "Bearer "+gitlabTestToken {
		t.Errorf("Authorization = %q, want the Bearer token", got)
	}
}

// TestGitLabBackend_AuthenticatedUser_Refusals proves a 401 is a typed backend
// error that never carries the token, and an anonymous answer is refused.
func TestGitLabBackend_AuthenticatedUser_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET /user", http.StatusUnauthorized, map[string]string{"message": "401 Unauthorized " + gitlabTestToken})

	_, err := backend.AuthenticatedUser(context.Background())
	if !errors.Is(err, ErrBackend) || forgeStatusOf(err) != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a 401 backend error", err)
	}
	gitlabErrorFree(t, err)

	stub.json("GET /user", http.StatusOK, map[string]any{"id": 9, "username": ""})
	_, err = backend.AuthenticatedUser(context.Background())
	var unverified *TokenVerificationError
	if !errors.As(err, &unverified) {
		t.Fatalf("err = %v, want TokenVerificationError", err)
	}
}

// TestGitLabBackend_NotFoundIsTyped proves a 404 is ErrNotFound.
func TestGitLabBackend_NotFoundIsTyped(t *testing.T) {
	backend, _ := newGitLabStub(t)
	_, err := backend.GetIssue(context.Background(), gitlabRepo, 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestGitLabBackend_RetriesAnUnansweredRead proves a gateway failure on a read
// is retried.
func TestGitLabBackend_RetriesAnUnansweredRead(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.sequence("GET /user",
		forgeJSON(http.StatusBadGateway, nil),
		forgeJSON(http.StatusOK, map[string]any{"id": 9, "username": "agent-widgets"}))

	if _, err := backend.AuthenticatedUser(context.Background()); err != nil {
		t.Fatalf("AuthenticatedUser: %v", err)
	}
	if n := len(stub.requests("GET /user")); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
}

// TestGitLabPages_FollowsXNextPage proves a listing follows X-Next-Page to
// its end at GitLab's page size.
func TestGitLabPages_FollowsXNextPage(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.handle("GET "+glProject+"/issues", gitlabPaged(
		[]map[string]any{{"iid": 1, "title": "one", "state": "opened"}},
		[]map[string]any{{"iid": 2, "title": "two", "state": "opened"}},
	))

	issues, err := backend.ListIssues(context.Background(), gitlabRepo, IssueFilter{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 || issues[1].Number != 2 {
		t.Fatalf("issues = %+v, want both pages", issues)
	}
	calls := stub.requests("GET " + glProject + "/issues")
	if len(calls) != 2 || !strings.Contains(calls[0].Query, "per_page=100") || !strings.Contains(calls[1].Query, "page=2") {
		t.Errorf("calls = %+v, want per_page=100 then page 2", calls)
	}
}

// TestGitLabPages_FollowsLinkNext proves a listing that names its next page
// only in a Link header is followed too.
func TestGitLabPages_FollowsLinkNext(t *testing.T) {
	backend, stub := newGitLabStub(t)
	next := stub.api() + glProject + "/labels?page=2&per_page=100"
	stub.handle("GET "+glProject+"/labels", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			forgeJSON(http.StatusOK, []gitlabLabel{{Name: "second", Color: "#000000"}})(w, r)
			return
		}
		w.Header().Set("Link", `<`+next+`>; rel="next"`)
		forgeJSON(http.StatusOK, []gitlabLabel{{Name: "first", Color: "#FFFFFF"}})(w, r)
	})

	labels, err := backend.Labels(context.Background(), gitlabRepo)
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	if len(labels) != 2 || labels[0].Color != "ffffff" {
		t.Fatalf("labels = %+v, want both pages, colours as bare lowercase hex", labels)
	}
}

// TestGitLabPages_ReportsAListingThatNeverEnds proves a listing still naming
// a next page at the ceiling is reported, not silently truncated.
func TestGitLabPages_ReportsAListingThatNeverEnds(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.handle("GET "+glProject+"/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(gitlabNextPageHeader, "2")
		forgeJSON(http.StatusOK, []map[string]any{{"iid": 1, "title": "again", "state": "opened"}})(w, r)
	})
	_, err := backend.ListIssues(context.Background(), gitlabRepo, IssueFilter{})
	if !errors.Is(err, errIssuePagesExhausted) {
		t.Fatalf("err = %v, want the page ceiling reported", err)
	}
}

// TestGitLabRefusal_MapsOnlyTheMergeRoute proves the merge statuses carry
// their meaning on the merge route and nowhere else.
func TestGitLabRefusal_MapsOnlyTheMergeRoute(t *testing.T) {
	merge := forgeRequest{operation: "MergePullRequest", path: glProject + "/merge_requests/5/merge"}
	var moved *HeadMovedError
	if err := gitlabRefusal(merge, http.StatusConflict, nil); !errors.As(err, &moved) {
		t.Errorf("409 = %v, want HeadMovedError", err)
	}
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusUnprocessableEntity} {
		if err := gitlabRefusal(merge, status, []byte(`{"message":"Branch cannot be merged"}`)); !errors.Is(err, ErrForgeNotReady) {
			t.Errorf("%d = %v, want ErrForgeNotReady", status, err)
		}
	}
	if err := gitlabRefusal(merge, http.StatusTeapot, nil); err != nil {
		t.Errorf("418 = %v, want no mapping", err)
	}
	if err := gitlabRefusal(forgeRequest{operation: "CreateIssue"}, http.StatusConflict, nil); err != nil {
		t.Errorf("a 409 elsewhere = %v, want no mapping", err)
	}
}

// TestGitLabProjectPath_EncodesNestedGroups proves a nested group's slashes
// travel encoded inside one path segment.
func TestGitLabProjectPath_EncodesNestedGroups(t *testing.T) {
	if got := gitlabProjectPath(RepoRef{Owner: "acme/platform", Repo: "widgets"}); got != "/projects/acme%2Fplatform%2Fwidgets" {
		t.Errorf("path = %q", got)
	}
	if got := gitlabRefPath("feature/x"); got != "feature%2Fx" {
		t.Errorf("ref = %q", got)
	}
}
