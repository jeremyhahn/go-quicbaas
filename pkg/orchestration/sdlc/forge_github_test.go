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
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Fixture-based, as the Gitea adapter's tests are: a real http.Client against
// an httptest server speaking GitHub's REST v3 payloads, served under a GitHub
// Enterprise style API root so every path the adapter builds is checked
// against a base that is not the host's root. No call leaves the process.

// githubTestToken is the adapter's token in every test. Distinctive, so a test
// can prove it never reaches an error or a log body.
const githubTestToken = "ghp-fixture-token-never-shown"

// githubAPIRoot is the path the stub serves the API under.
const githubAPIRoot = "/api/v3"

// The commits the fixtures in testdata/github name.
const (
	githubHeadSHA  = "4a4f5c9d1e2b3a4c5d6e7f8091a2b3c4d5e6f708"
	githubMovedSHA = "9f8e7d6c5b4a39281706f5e4d3c2b1a09f8e7d6c"
	githubMergeSHA = "0c1d2e3f405162738495a6b7c8d9e0f1a2b3c4d5"
)

// githubRepo is the repository the fixtures describe.
var githubRepo = RepoRef{Owner: "acme", Repo: "widgets"}

// newGitHubStub starts a stub and an adapter pointed at it.
func newGitHubStub(t *testing.T) (*GitHubBackend, *forgeStub) {
	t.Helper()
	stub := newForgeStub(t, githubAPIRoot, "github")
	backend, err := NewGitHubBackend(GitHubConfig{
		BaseURL: stub.api(),
		Token:   githubTestToken,
		Waiter:  &immediateWaiter{},
	})
	if err != nil {
		t.Fatalf("NewGitHubBackend: %v", err)
	}
	return backend, stub
}

// githubPaged answers with a JSON payload and a Link header naming next.
func githubPaged(payload any, next string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<`+next+`>; rel="next", <`+next+`>; rel="last"`)
		forgeJSON(http.StatusOK, payload)(w, r)
	}
}

// =============================================================================
// Construction, registration and capabilities
// =============================================================================

// TestNewGitHubBackend_DefaultsToGitHubCom proves the one host the adapter
// writes down is used only when the binding names none, and that a binding's
// own root wins.
func TestNewGitHubBackend_DefaultsToGitHubCom(t *testing.T) {
	backend, err := NewGitHubBackend(GitHubConfig{Token: githubTestToken})
	if err != nil {
		t.Fatalf("NewGitHubBackend: %v", err)
	}
	if backend.baseURL != githubDefaultBaseURL {
		t.Errorf("base = %q, want %q", backend.baseURL, githubDefaultBaseURL)
	}

	enterprise, err := NewGitHubBackend(GitHubConfig{BaseURL: "https://ghe.example/api/v3/", Token: githubTestToken})
	if err != nil {
		t.Fatalf("NewGitHubBackend: %v", err)
	}
	if enterprise.baseURL != "https://ghe.example/api/v3" {
		t.Errorf("base = %q, want the binding's own root without its trailing slash", enterprise.baseURL)
	}
}

// TestNewGitHubBackend_FailsClosedOnMissingConfig refuses an adapter that
// could only fail on first use.
func TestNewGitHubBackend_FailsClosedOnMissingConfig(t *testing.T) {
	cases := map[string]GitHubConfig{
		"no token":      {BaseURL: "https://ghe.example/api/v3"},
		"bad transport": {Token: githubTestToken, Transport: ForgeTransport{ReadAttempts: -1}},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			backend, err := NewGitHubBackend(cfg)
			if backend != nil {
				t.Fatal("an unusable adapter was constructed")
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// TestGitHubBackend_IsRegistered proves a binding of type github now reaches
// an adapter instead of ForgeNotRegisteredError.
func TestGitHubBackend_IsRegistered(t *testing.T) {
	if !slices.Contains(RegisteredForgeTypes(), ForgeGitHub) {
		t.Fatalf("registered = %v, want github among them", RegisteredForgeTypes())
	}
	backend, err := NewForgeBackend(ForgeConfig{Type: ForgeGitHub, BaseURL: "https://ghe.example/api/v3"},
		ForgeCredentials{Token: githubTestToken}, ForgeTransport{})
	if err != nil {
		t.Fatalf("NewForgeBackend: %v", err)
	}
	if _, ok := backend.(*GitHubBackend); !ok || backend.Type() != ForgeGitHub {
		t.Fatalf("backend = %T (%v), want *GitHubBackend", backend, backend.Type())
	}
	if _, ok := backend.(ForgeCollaborators); !ok {
		t.Error("the GitHub adapter does not serve ForgeCollaborators")
	}

	_, err = NewForgeBackend(ForgeConfig{Type: ForgeGitHub}, ForgeCredentials{}, ForgeTransport{})
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("tokenless registration error = %v, want ErrInvalidInput", err)
	}
}

// TestGitHubBackend_CapabilitiesAreHonest pins every flag. Only the merge
// queue is false — REST v3 has no route to enqueue a pull request — and a new
// flag left at false would be a claim nobody checked, so this test fails until
// the new flag is decided here.
func TestGitHubBackend_CapabilitiesAreHonest(t *testing.T) {
	backend, _ := newGitHubStub(t)
	if backend.Type() != ForgeGitHub {
		t.Errorf("Type() = %v, want github", backend.Type())
	}
	caps := reflect.ValueOf(backend.Capabilities())
	for i := 0; i < caps.NumField(); i++ {
		name := caps.Type().Field(i).Name
		want := name != "NativeMergeQueue"
		if got := caps.Field(i).Bool(); got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

// =============================================================================
// Transport
// =============================================================================

// TestGitHubBackend_SendsVersionedHeadersAndBearerToken proves every call
// carries GitHub's media type, the pinned API version and the token.
func TestGitHubBackend_SendsVersionedHeadersAndBearerToken(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /user", http.StatusOK, map[string]string{"login": "agent-widgets"})

	identity, err := backend.AuthenticatedUser(context.Background())
	if err != nil || identity.Username != "agent-widgets" {
		t.Fatalf("AuthenticatedUser = %+v, %v", identity, err)
	}
	header := stub.requests("GET /user")[0].Header
	checks := map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": githubAPIVersion,
		"Authorization":        "Bearer " + githubTestToken,
	}
	for name, want := range checks {
		if got := header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// TestGitHubBackend_AuthenticatedUser_Refusals covers a rejected token — whose
// error must not carry the token even when the server echoes it — and an
// answer naming no account.
func TestGitHubBackend_AuthenticatedUser_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /user", http.StatusUnauthorized,
		map[string]string{"message": "Bad credentials: " + githubTestToken})

	_, err := backend.AuthenticatedUser(context.Background())
	if !errors.Is(err, ErrBackend) || forgeStatusOf(err) != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a 401 backend error", err)
	}
	if strings.Contains(err.Error(), githubTestToken) {
		t.Fatalf("the token reached the error: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("err = %v, want the echoed token redacted rather than dropped", err)
	}

	stub.json("GET /user", http.StatusOK, map[string]string{"login": ""})
	_, err = backend.AuthenticatedUser(context.Background())
	var unverified *TokenVerificationError
	if !errors.As(err, &unverified) {
		t.Fatalf("err = %v, want TokenVerificationError", err)
	}
}

// TestGitHubBackend_NotFoundIsTyped proves a 404 is ErrNotFound rather than a
// generic backend failure, on GitHub as on Gitea.
func TestGitHubBackend_NotFoundIsTyped(t *testing.T) {
	backend, _ := newGitHubStub(t)
	_, err := backend.GetIssue(context.Background(), githubRepo, 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestGitHubBackend_RetriesAnUnansweredRead proves a gateway answer is no
// answer: a read is sent again and the forge's eventual answer is used.
func TestGitHubBackend_RetriesAnUnansweredRead(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.sequence("GET /user",
		forgeJSON(http.StatusBadGateway, nil),
		forgeJSON(http.StatusOK, map[string]string{"login": "agent-widgets"}))

	if _, err := backend.AuthenticatedUser(context.Background()); err != nil {
		t.Fatalf("AuthenticatedUser: %v", err)
	}
	if n := len(stub.requests("GET /user")); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
}

// =============================================================================
// Paging
// =============================================================================

// TestGitHubPages_FollowsLinkNext proves a listing is read to the page the
// Link header says is last, with GitHub's largest page size.
func TestGitHubPages_FollowsLinkNext(t *testing.T) {
	backend, stub := newGitHubStub(t)
	page2 := stub.api() + "/repos/acme/widgets/issues?page=2&per_page=100&state=open"
	first := githubPaged(json.RawMessage(stub.load("issues_page1.json")), page2)
	second := forgeRaw(http.StatusOK, stub.load("issues_page2.json"))
	stub.handle("GET /repos/acme/widgets/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			second(w, r)
			return
		}
		first(w, r)
	})

	issues, err := backend.ListIssues(context.Background(), githubRepo, IssueFilter{})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	var numbers []int
	for _, issue := range issues {
		numbers = append(numbers, issue.Number)
	}
	if !slices.Equal(numbers, []int{41, 43}) {
		t.Errorf("issues = %v, want [41 43]: both pages, pull request 42 dropped", numbers)
	}
	calls := stub.requests("GET /repos/acme/widgets/issues")
	if len(calls) != 2 || !strings.Contains(calls[0].Query, "per_page=100") || !strings.Contains(calls[1].Query, "page=2") {
		t.Errorf("calls = %+v, want page 1 then page 2 at 100 per page", calls)
	}
}

// TestGitHubPages_RefusesAForeignLink proves the token is never sent to a host
// a Link header names: the listing fails before the request is made.
func TestGitHubPages_RefusesAForeignLink(t *testing.T) {
	backend, stub := newGitHubStub(t)
	foreign := map[string]string{
		"other host":           "https://collector.example/api/v3/repos/acme/widgets/issues?page=2",
		"scheme-relative path": stub.api() + "//collector.example/issues?page=2",
	}
	for name, next := range foreign {
		t.Run(name, func(t *testing.T) {
			stub.handle("GET /repos/acme/widgets/issues", githubPaged([]githubIssue{}, next))
			_, err := backend.ListIssues(context.Background(), githubRepo, IssueFilter{})
			if !errors.Is(err, ErrBackend) || !errors.Is(err, errForeignPageLink) {
				t.Fatalf("err = %v, want the foreign link refused", err)
			}
		})
	}
}

// TestGitHubPages_ReportsAListingThatNeverEnds proves a listing still naming a
// next page at the adapter's ceiling is a fault, not a truncated answer.
func TestGitHubPages_ReportsAListingThatNeverEnds(t *testing.T) {
	backend, stub := newGitHubStub(t)
	self := stub.api() + "/repos/acme/widgets/collaborators?page=2&per_page=100"
	stub.handle("GET /repos/acme/widgets/collaborators", githubPaged([]githubUser{{Login: "a"}}, self))

	_, err := backend.ListCollaborators(context.Background(), githubRepo)
	if !errors.Is(err, ErrIncompleteResponse) {
		t.Fatalf("err = %v, want ErrIncompleteResponse", err)
	}
	if n := len(stub.requests("GET /repos/acme/widgets/collaborators")); n != maxCollaboratorPages {
		t.Errorf("pages read = %d, want the ceiling %d", n, maxCollaboratorPages)
	}
}
