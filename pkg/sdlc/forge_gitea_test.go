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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Fixture-based: a real http.Client against an httptest server speaking Gitea's
// payloads. That exercises the adapter's transport, status handling, decoding
// and read-back for real, while letting a test stage the one thing a live Gitea
// will not reliably produce on demand — a 200 for a write that did nothing.
//
// Live-Gitea integration tests are a separate build-tagged suite.

// giteaFixture is a stubbed Gitea instance.
type giteaFixture struct {
	t      *testing.T
	server *httptest.Server

	// handlers maps "METHOD /path" to a responder.
	handlers map[string]func(w http.ResponseWriter, r *http.Request)

	// requests records what the adapter sent, so a test can assert the call
	// rather than only the result.
	requests []recordedRequest
}

type recordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
	Auth   string
}

// newGiteaFixture starts a stub server and an adapter pointed at it.
func newGiteaFixture(t *testing.T) (*GiteaBackend, *giteaFixture) {
	t.Helper()
	fixture := &giteaFixture{t: t, handlers: map[string]func(http.ResponseWriter, *http.Request){}}

	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := readAllString(r)
		fixture.requests = append(fixture.requests, recordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   body,
			Auth:   r.Header.Get("Authorization"),
		})

		key := r.Method + " " + r.URL.Path
		if handler, ok := fixture.handlers[key]; ok {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"no fixture for ` + key + `"}`))
	}))
	t.Cleanup(fixture.server.Close)

	backend, err := NewGiteaBackend(GiteaConfig{
		BaseURL: fixture.server.URL + "/api/v1",
		Token:   "test-token",
	})
	if err != nil {
		t.Fatalf("NewGiteaBackend: %v", err)
	}
	return backend, fixture
}

// readAllString drains a request body.
func readAllString(r *http.Request) (string, error) {
	if r.Body == nil {
		return "", nil
	}
	defer func() { _ = r.Body.Close() }()
	body, err := io.ReadAll(r.Body)
	return string(body), err
}

// respondJSON registers a handler returning status and a JSON body.
func (f *giteaFixture) respondJSON(key string, status int, payload any) {
	f.handlers[key] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}
}

var testRepo = RepoRef{Owner: "jeremyhahn", Repo: "go-quicbaas"}

// =============================================================================
// Construction and capabilities
// =============================================================================

// TestNewGiteaBackend_FailsClosedOnMissingConfig proves an adapter that could
// only fail on first use is refused at construction instead.
func TestNewGiteaBackend_FailsClosedOnMissingConfig(t *testing.T) {
	cases := map[string]GiteaConfig{
		"no base url": {Token: "t"},
		"no token":    {BaseURL: "http://gitea.example/api/v1"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			backend, err := NewGiteaBackend(cfg)
			if backend != nil {
				t.Fatal("an unusable adapter was constructed")
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// TestGiteaBackend_CapabilitiesMatchThePlatform pins the two flags that change
// engine behaviour.
//
// Gitea publishes no route to cancel an Actions run, so supersession there
// records an orphan rather than claiming a cancellation. If this flag ever
// flips to true without a cancel route existing, the engine starts believing
// runs stopped when they did not.
func TestGiteaBackend_CapabilitiesMatchThePlatform(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	caps := backend.Capabilities()

	if caps.CancelCheckRuns {
		t.Error("gitea claims it can cancel check runs; it publishes no such route")
	}
	if caps.ReliableLabelEvents {
		t.Error("gitea claims reliable label deliveries; they are not")
	}
	if !caps.SquashMerge {
		t.Error("gitea claims it cannot squash-merge, which would refuse the default merge strategy")
	}
	if backend.Type() != ForgeGitea {
		t.Errorf("Type() = %v, want gitea", backend.Type())
	}
}

// =============================================================================
// Reading issues
// =============================================================================

// TestListIssues_ExcludesPullRequests is a correctness property, not a filter
// preference.
//
// Gitea serves pull requests from the issues endpoint — a PR IS an issue there.
// A PR arriving where the dispatcher expects work items would be picked up as
// one and worked a second time, which is the duplicate-work class this module
// exists to stop.
func TestListIssues_ExcludesPullRequests(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues", http.StatusOK,
		[]map[string]any{{"number": 32, "title": "fix the thing", "state": "open"}})

	issues, err := backend.ListIssues(context.Background(), testRepo, IssueFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 32 {
		t.Fatalf("issues = %+v, want issue 32", issues)
	}

	got := fixture.requests[0].Query
	if !strings.Contains(got, "type=issues") {
		t.Errorf("query %q does not exclude pull requests; Gitea would return PRs as issues", got)
	}
	if !strings.Contains(got, "state=open") {
		t.Errorf("query %q does not default to open issues", got)
	}
}

// TestListIssues_CarriesTheFilterIntoTheQuery covers state and labels.
//
// Limit is NOT asserted here any more, and that is the point of the change it
// documents: it used to be handed straight to Gitea as a page size, so a caller
// that set nothing received one page and believed it had everything. It now
// bounds the TOTAL across pages — see TestListIssues_StopsAtTheCallersLimit —
// and the query always carries the adapter's own page size.
func TestListIssues_CarriesTheFilterIntoTheQuery(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues", http.StatusOK, []map[string]any{})

	_, err := backend.ListIssues(context.Background(), testRepo, IssueFilter{
		State:  "all",
		Labels: []string{"ready", "agent"},
		Limit:  7,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	query := fixture.requests[0].Query
	for _, want := range []string{"state=all", "labels=ready%2Cagent", "page=1"} {
		if !strings.Contains(query, want) {
			t.Errorf("query %q omits %q", query, want)
		}
	}
}

// issuePage builds one page of n issues numbered from start.
func issuePage(start, n int) []map[string]any {
	page := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		page = append(page, map[string]any{
			"number": start + i,
			"title":  "issue",
			"state":  "open",
		})
	}
	return page
}

// respondIssuePages serves a different page per `page` query parameter, which
// is what makes pagination testable at all.
func (f *giteaFixture) respondIssuePages(pages [][]map[string]any) {
	f.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/issues"] = func(w http.ResponseWriter, r *http.Request) {
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

// TestListIssues_PagesUntilAShortPage is the hole this closes.
//
// A single-page listing left the polling reconciler silently orphaning every
// issue past the first page — beneath the floor every other correctness claim
// in the module rests on, with no symptom but work that never starts. A short
// page is how a forge says "that is all"; anything else means keep asking.
func TestListIssues_PagesUntilAShortPage(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondIssuePages([][]map[string]any{
		issuePage(1, giteaDefaultPageSize),
		issuePage(giteaDefaultPageSize+1, giteaDefaultPageSize),
		issuePage(2*giteaDefaultPageSize+1, 3),
	})

	issues, err := backend.ListIssues(context.Background(), testRepo, IssueFilter{State: "open"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := 2*giteaDefaultPageSize + 3
	if len(issues) != want {
		t.Fatalf("got %d issues, want %d — the listing stopped early", len(issues), want)
	}
	// The last issue of the last page must be present: a listing that returned
	// the right COUNT from the wrong pages would otherwise pass.
	if issues[len(issues)-1].Number != want {
		t.Errorf("last issue is %d, want %d", issues[len(issues)-1].Number, want)
	}
	if len(fixture.requests) != 3 {
		t.Errorf("made %d requests, want 3 — one per page until the short one", len(fixture.requests))
	}
}

// TestListIssues_StopsAtTheCallersLimit proves Limit bounds the total rather
// than the page.
func TestListIssues_StopsAtTheCallersLimit(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondIssuePages([][]map[string]any{
		issuePage(1, giteaDefaultPageSize),
		issuePage(giteaDefaultPageSize+1, giteaDefaultPageSize),
	})

	issues, err := backend.ListIssues(context.Background(), testRepo, IssueFilter{State: "open", Limit: 60})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 60 {
		t.Fatalf("got %d issues, want the caller's 60", len(issues))
	}
	// It had to fetch a second page to reach 60, and must not have fetched a
	// third once it had them.
	if len(fixture.requests) != 2 {
		t.Errorf("made %d requests, want 2", len(fixture.requests))
	}
}

// TestListIssues_AnUnboundedListingEndingOnTheBoundaryIsNotAFault covers the
// third of the three cases IssueFilter.Limit documents.
//
// A repository that happens to hold exactly as many issues as the ceiling
// allows looks, from inside the loop, identical to one holding more: every page
// was full and the forge never said it had finished. Reporting that as a broken
// forge would fail a perfectly ordinary repository for ending on a page
// boundary, so the adapter asks once more before concluding anything.
func TestListIssues_AnUnboundedListingEndingOnTheBoundaryIsNotAFault(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// Full pages right up to the ceiling, then genuinely nothing.
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/issues"] = func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if page > maxIssuePages {
			_ = json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		_ = json.NewEncoder(w).Encode(issuePage((page-1)*giteaDefaultPageSize+1, giteaDefaultPageSize))
	}

	issues, err := backend.ListIssues(context.Background(), testRepo, IssueFilter{State: "open"})
	if err != nil {
		t.Fatalf("a repository ending exactly on the ceiling was reported as broken: %v", err)
	}
	want := maxIssuePages * giteaDefaultPageSize
	if len(issues) != want {
		t.Fatalf("got %d issues, want %d", len(issues), want)
	}
	// The extra request is what distinguishes this case from a truncation.
	if len(fixture.requests) != maxIssuePages+1 {
		t.Errorf("made %d requests, want %d — one past the ceiling to establish nothing was missed",
			len(fixture.requests), maxIssuePages+1)
	}
}

// TestListIssues_AnUnboundedListingThatNeverEndsIsAFault keeps a misbehaving
// forge from spinning forever, and from having its prefix presented as a whole
// listing.
func TestListIssues_AnUnboundedListingThatNeverEndsIsAFault(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// Every page is full, forever: the forge never says "that is all".
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/issues"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(issuePage(1, giteaDefaultPageSize))
	}

	issues, err := backend.ListIssues(context.Background(), testRepo, IssueFilter{State: "open"})
	if issues != nil {
		t.Error("a prefix was returned alongside the fault; it would be read as a complete listing")
	}
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want a BackendError", err)
	}
	if len(fixture.requests) != maxIssuePages+1 {
		t.Errorf("made %d requests, want the %d-page bound plus the one probe past it",
			len(fixture.requests), maxIssuePages)
	}
}

// TestIssuePageBound_DerivesFromTheCallersLimit is the fix for a real defect
// the first cut of this change had.
//
// Under a fixed page ceiling, a repository large enough to fill every page up
// to the cap is indistinguishable from a forge stuck in a loop, so a caller
// asking for more issues than the ceiling allowed got ErrBackend instead of its
// issues — a large project reported as an unmirrorable one. The bound is tested
// here as a function rather than through a listing because demonstrating it end
// to end would need a fixture serving a thousand full pages, and the arithmetic
// is the whole of the fix.
func TestIssuePageBound_DerivesFromTheCallersLimit(t *testing.T) {
	cases := []struct {
		limit int
		want  int
	}{
		// Unbounded keeps the absolute ceiling: it has no other way to stop.
		{0, maxIssuePages},
		{-1, maxIssuePages},

		// Bounded needs exactly enough pages to satisfy itself, and the
		// rounding matters at every page edge.
		{1, 1},
		{giteaDefaultPageSize, 1},
		{giteaDefaultPageSize + 1, 2},
		{120, 3},

		// The case the defect was about: a limit past the old fixed ceiling
		// must raise the bound rather than fail the listing.
		{20000, 400},
	}
	for _, tc := range cases {
		if got := issuePageBound(tc.limit); got != tc.want {
			t.Errorf("issuePageBound(%d) = %d, want %d", tc.limit, got, tc.want)
		}
	}

	// And the property that makes it safe, stated directly: a bounded listing
	// can never reach the ceiling an unbounded one is judged by.
	if issuePageBound(20000) >= maxIssuePages {
		t.Error("a bounded listing can still reach the ceiling; it would be reported as a broken forge")
	}
}

// TestListIssues_ABoundedListingStopsAtItsLimit covers the same bound through a
// real listing.
func TestListIssues_ABoundedListingStopsAtItsLimit(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// A repository with no end in sight, exactly as above.
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/issues"] = func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(issuePage((page-1)*giteaDefaultPageSize+1, giteaDefaultPageSize))
	}

	issues, err := backend.ListIssues(context.Background(), testRepo, IssueFilter{State: "open", Limit: 120})
	if err != nil {
		t.Fatalf("a bounded listing of a large repository was reported as a fault: %v", err)
	}
	if len(issues) != 120 {
		t.Fatalf("got %d issues, want the caller's 120", len(issues))
	}
	// Three pages of fifty is the fewest that reach 120, and it must not have
	// walked to the ceiling to get there.
	if len(fixture.requests) != 3 {
		t.Errorf("made %d requests, want 3", len(fixture.requests))
	}
}

// TestAddIssueLabels_AddsWithoutDisturbingTheRest is why the additive route
// exists.
//
// Gitea's PUT replaces the set, so a replacement computed from a read a moment
// earlier destroys whatever arrived in between. POST adds.
func TestAddIssueLabels_AddsWithoutDisturbingTheRest(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{
		{"id": 1, "name": "task:in-progress"},
		{"id": 2, "name": "priority:high"},
	})
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels", http.StatusOK, nil)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32, "state": "open",
		"labels": []map[string]any{{"name": "priority:high"}, {"name": "task:in-progress"}},
	})

	if err := backend.AddIssueLabels(context.Background(), testRepo, 32, []string{"task:in-progress"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wrote *recordedRequest
	for i := range fixture.requests {
		if fixture.requests[i].Method == http.MethodPost {
			wrote = &fixture.requests[i]
		}
	}
	if wrote == nil {
		t.Fatal("no POST was made; a PUT here would replace the set")
	}
	if !strings.Contains(wrote.Body, "1") {
		t.Errorf("body %q does not carry the resolved label id", wrote.Body)
	}
}

// TestAddIssueLabels_ChecksPresenceNotSetEquality keeps a correct write from
// being reported as failed.
//
// The labels this call did not name are supposed to still be there. Comparing
// whole sets would make every add fail on an issue that carries anything else.
func TestAddIssueLabels_ChecksPresenceNotSetEquality(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{
		{"id": 1, "name": "task:ready"},
	})
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels", http.StatusOK, nil)
	// The read-back carries a foreign label the caller never mentioned.
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32, "state": "open",
		"labels": []map[string]any{{"name": "task:ready"}, {"name": "someone-elses"}},
	})

	if err := backend.AddIssueLabels(context.Background(), testRepo, 32, []string{"task:ready"}); err != nil {
		t.Fatalf("a correct add was reported as failed: %v", err)
	}
}

// TestAddIssueLabels_ReadBackCatchesAWriteThatDidNothing is WP2's rule applied
// to the new route: Gitea has answered 200 for label writes that did not land.
func TestAddIssueLabels_ReadBackCatchesAWriteThatDidNothing(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{
		{"id": 1, "name": "task:ready"},
	})
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels", http.StatusOK, nil)
	// 200 above, and the label is not on the issue.
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32, "state": "open", "labels": []map[string]any{},
	})

	err := backend.AddIssueLabels(context.Background(), testRepo, 32, []string{"task:ready"})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestAddIssueLabels_AddingNothingTouchesTheForge keeps a no-op from costing a
// round trip and a read-back to prove it did nothing.
func TestAddIssueLabels_AddingNothingTouchesTheForge(t *testing.T) {
	backend, fixture := newGiteaFixture(t)

	if err := backend.AddIssueLabels(context.Background(), testRepo, 32, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fixture.requests) != 0 {
		t.Errorf("adding no labels made %d requests", len(fixture.requests))
	}
}

// TestRemoveIssueLabel_RemovesOneAndVerifies covers the delete route.
func TestRemoveIssueLabel_RemovesOneAndVerifies(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{
		{"id": 7, "name": "task:ready"},
	})
	fixture.respondJSON("DELETE /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels/7", http.StatusNoContent, nil)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32, "state": "open",
		"labels": []map[string]any{{"name": "priority:high"}},
	})

	if err := backend.RemoveIssueLabel(context.Background(), testRepo, 32, "task:ready"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRemoveIssueLabel_AnAlreadyAbsentLabelIsSuccess keeps an idempotent
// re-sync from failing because a human got there first.
//
// Gitea answers 404 for a label the issue does not carry. The honest question
// is "is this label off the issue", not "did I personally take it off".
func TestRemoveIssueLabel_AnAlreadyAbsentLabelIsSuccess(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{
		{"id": 7, "name": "task:ready"},
	})
	fixture.respondJSON("DELETE /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels/7", http.StatusNotFound,
		map[string]any{"message": "label does not exist on issue"})
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32, "state": "open", "labels": []map[string]any{},
	})

	if err := backend.RemoveIssueLabel(context.Background(), testRepo, 32, "task:ready"); err != nil {
		t.Fatalf("removing an already-absent label was reported as a failure: %v", err)
	}
}

// TestRemoveIssueLabel_ReadBackCatchesALabelStillPresent is the read-back rule
// on the delete route.
func TestRemoveIssueLabel_ReadBackCatchesALabelStillPresent(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{
		{"id": 7, "name": "task:ready"},
	})
	fixture.respondJSON("DELETE /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels/7", http.StatusNoContent, nil)
	// 204 above, and the label is still there.
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32, "state": "open",
		"labels": []map[string]any{{"name": "task:ready"}},
	})

	err := backend.RemoveIssueLabel(context.Background(), testRepo, 32, "task:ready")
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestRemoveIssueLabel_RefusesAnEmptyLabel covers the input guard.
func TestRemoveIssueLabel_RefusesAnEmptyLabel(t *testing.T) {
	backend, fixture := newGiteaFixture(t)

	err := backend.RemoveIssueLabel(context.Background(), testRepo, 32, "  ")
	var typed *InvalidInputError
	if !errors.As(err, &typed) || typed.Field != "label" {
		t.Fatalf("error = %v, want an InvalidInputError naming label", err)
	}
	if len(fixture.requests) != 0 {
		t.Errorf("an empty label made %d requests", len(fixture.requests))
	}
}

// TestCapabilities_ReportsAdditiveLabels pins the capability the mirror routes
// on.
func TestCapabilities_ReportsAdditiveLabels(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	if !backend.Capabilities().AdditiveLabels {
		t.Error("Gitea publishes both the add and the remove route; reporting otherwise sends display sync back to whole-set replacement")
	}
}

// TestGetIssue_MapsGiteaShapeOntoTheModuleS covers the field mapping, including
// the nested author and label objects.
func TestGetIssue_MapsGiteaShapeOntoTheModuleS(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number":   32,
		"title":    "fix the thing",
		"body":     "details",
		"state":    "open",
		"html_url": "http://gitea.example/jeremyhahn/go-quicbaas/issues/32",
		"user":     map[string]any{"login": "jeremyhahn"},
		"assignee": map[string]any{"login": "agent-go-quicbaas"},
		"labels": []map[string]any{
			{"id": 1, "name": "ready"},
			{"id": 2, "name": "agent"},
		},
	})

	issue, err := backend.GetIssue(context.Background(), testRepo, 32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Author != "jeremyhahn" {
		t.Errorf("Author = %q, want jeremyhahn", issue.Author)
	}
	if issue.Assignee != "agent-go-quicbaas" {
		t.Errorf("Assignee = %q, want agent-go-quicbaas", issue.Assignee)
	}
	if normalizedLabels(issue.Labels) != "[agent ready]" {
		t.Errorf("Labels = %v, want ready and agent", issue.Labels)
	}
}

// TestGetIssue_AbsentIsNotFoundNotABackendFailure keeps the two apart.
//
// A missing issue is ordinary — it was deleted, or the number is wrong — and
// the caller handles it. A backend failure means the forge could not answer,
// which an operator has to fix. Collapsing them would make a broken token look
// like an empty work queue.
func TestGetIssue_AbsentIsNotFoundNotABackendFailure(t *testing.T) {
	backend, _ := newGiteaFixture(t)

	_, err := backend.GetIssue(context.Background(), testRepo, 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if errors.Is(err, ErrBackend) {
		t.Error("an absent issue was reported as a backend failure")
	}
}

// TestGitea_ForbiddenIsABackendFailureNotAnAbsence is the same distinction from
// the other side: a token without access must not read as "no such issue",
// which would look like an empty queue instead of a credential to fix.
func TestGitea_ForbiddenIsABackendFailureNotAnAbsence(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusForbidden,
		map[string]any{"message": "token does not have permission"})

	_, err := backend.GetIssue(context.Background(), testRepo, 32)
	if errors.Is(err, ErrNotFound) {
		t.Fatal("a forbidden read was reported as an absent issue")
	}
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error %v does not name the status an operator needs", err)
	}
}

// TestGitea_SendsTheTokenAndNeverLogsIt covers the auth header and the rule
// that the token never appears in an error.
func TestGitea_SendsTheTokenAndNeverLogsIt(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusInternalServerError,
		map[string]any{"message": "boom"})

	_, err := backend.GetIssue(context.Background(), testRepo, 32)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Error("the token leaked into an error message")
	}
	if fixture.requests[0].Auth != "token test-token" {
		t.Errorf("Authorization = %q, want Gitea's token scheme", fixture.requests[0].Auth)
	}
}

// =============================================================================
// Writes and read-back
// =============================================================================

// TestSetIssueLabels_VerifiesByReadBack is the property this whole adapter is
// shaped around.
//
// Gitea answers 200 for label writes that do not take effect. An adapter that
// trusted the status code would report success while the forge showed something
// else — which is exactly how the shell scripts displayed state that was not
// real. The fixture stages that failure directly: the PUT succeeds and the
// read-back shows the old labels.
func TestSetIssueLabels_VerifiesByReadBack(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK,
		[]map[string]any{{"id": 1, "name": "ready"}, {"id": 2, "name": "in-progress"}})
	// The write is accepted...
	fixture.respondJSON("PUT /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels", http.StatusOK, nil)
	// ...and changed nothing.
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32,
		"labels": []map[string]any{{"id": 1, "name": "ready"}},
	})

	err := backend.SetIssueLabels(context.Background(), testRepo, 32, []string{"in-progress"})
	if err == nil {
		t.Fatal("a 200 that changed nothing was reported as success")
	}
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
	for _, want := range []string{"[in-progress]", "[ready]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %v does not show what was wanted against what was found", err)
		}
	}
}

// TestSetIssueLabels_SucceedsWhenTheWriteActuallyLanded covers the ordinary
// path, and pins that comparison ignores order and case — Gitea returns labels
// in its own order, and a read-back that cried wolf would get ignored.
func TestSetIssueLabels_SucceedsWhenTheWriteActuallyLanded(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK,
		[]map[string]any{{"id": 1, "name": "ready"}, {"id": 2, "name": "in-progress"}})
	fixture.respondJSON("PUT /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels", http.StatusOK, nil)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32,
		// Returned in a different order and case than requested.
		"labels": []map[string]any{{"id": 2, "name": "In-Progress"}, {"id": 1, "name": "Ready"}},
	})

	if err := backend.SetIssueLabels(context.Background(), testRepo, 32, []string{"ready", "in-progress"}); err != nil {
		t.Fatalf("a landed write was reported as failed: %v", err)
	}
}

// TestSetIssueLabels_SendsLabelIDsNotNames pins the wire form.
//
// Gitea's endpoint takes IDs. Sending names silently sets no labels — another
// 200 that does nothing — so this asserts the resolved IDs actually go out.
func TestSetIssueLabels_SendsLabelIDsNotNames(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK,
		[]map[string]any{{"id": 7, "name": "in-progress"}})
	fixture.respondJSON("PUT /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/labels", http.StatusOK, nil)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusOK, map[string]any{
		"number": 32,
		"labels": []map[string]any{{"id": 7, "name": "in-progress"}},
	})

	if err := backend.SetIssueLabels(context.Background(), testRepo, 32, []string{"in-progress"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var put string
	for _, req := range fixture.requests {
		if req.Method == http.MethodPut {
			put = req.Body
		}
	}
	if !strings.Contains(put, "[7]") {
		t.Errorf("PUT body %q does not carry the resolved label ID", put)
	}
}

// TestSetIssueLabels_RefusesALabelTheRepoDoesNotDefine proves the adapter does
// not invent labels.
//
// Creating a missing label on the fly would paper over a typo in a project's
// label vocabulary by adding it to the repository permanently. Failing names
// the offending label instead.
func TestSetIssueLabels_RefusesALabelTheRepoDoesNotDefine(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK,
		[]map[string]any{{"id": 1, "name": "ready"}})

	err := backend.SetIssueLabels(context.Background(), testRepo, 32, []string{"typoed-label"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if !strings.Contains(err.Error(), "typoed-label") {
		t.Errorf("error %v does not name the undefined label", err)
	}
}

// TestCommentOnIssue_TreatsTheEchoAsTheReadBack covers the write whose response
// IS its own verification.
func TestCommentOnIssue_TreatsTheEchoAsTheReadBack(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// Gitea echoes a DIFFERENT body: the write did not land as asked.
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/comments", http.StatusCreated,
		map[string]any{"id": 5, "body": "something else"})

	_, err := backend.CommentOnIssue(context.Background(), testRepo, 32, "the real comment")
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestCommentOnIssue_ReturnsTheStoredComment covers the ordinary path.
func TestCommentOnIssue_ReturnsTheStoredComment(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/comments", http.StatusCreated,
		map[string]any{"id": 5, "body": "the real comment", "user": map[string]any{"login": "agent-go-quicbaas"}})

	comment, err := backend.CommentOnIssue(context.Background(), testRepo, 32, "the real comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comment.ID != 5 || comment.Author != "agent-go-quicbaas" {
		t.Errorf("comment = %+v, want id 5 authored by the agent", comment)
	}
}

// respondCommentPages serves the discussion endpoint one page at a time.
// respondCommentThread serves an issue discussion the way Gitea's per-issue
// comment route actually behaves: `since` is honoured, `page` and `limit` are
// ignored, and no more than cap comments come back at once.
func (f *giteaFixture) respondCommentThread(total, cap int) {
	f.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/comments"] = func(w http.ResponseWriter, r *http.Request) {
		var since time.Time
		if raw := r.URL.Query().Get("since"); raw != "" {
			parsed, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			since = parsed
		}
		all := commentThread(1, total)
		out := make([]map[string]any, 0, cap)
		for _, c := range all {
			created, _ := time.Parse(time.RFC3339, c["created_at"].(string))
			if !since.IsZero() && created.Before(since) {
				continue
			}
			out = append(out, c)
			if len(out) == cap {
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(out)
	}
}

// commentThread builds n comments numbered from first, each a second apart so
// a `since` cursor can address them the way the real route requires.
func commentThread(first, n int) []map[string]any {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	out := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, map[string]any{
			"id":         first + i,
			"body":       "comment " + strconv.Itoa(first+i),
			"user":       map[string]any{"login": "jeremyhahn"},
			"created_at": base.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
		})
	}
	return out
}

// commentPage builds one page of n comments numbered from first.
func commentPage(first, n int) []map[string]any {
	page := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		page = append(page, map[string]any{
			"id":   first + i,
			"body": "comment " + strconv.Itoa(first+i),
			"user": map[string]any{"login": "jeremyhahn"},
		})
	}
	return page
}

// TestListIssueComments_ReturnsTheThreadWithItsAuthors covers the ordinary path.
//
// The author is asserted because it is the half that makes a thread readable:
// a readmitted worker reads the module's own park question and the human's
// answer out of one list, and unattributed text lets it mistake its own
// question for the reply (#263).
func TestListIssueComments_ReturnsTheThreadWithItsAuthors(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/comments", http.StatusOK,
		[]map[string]any{
			{"id": 1, "body": "the agent has a question", "user": map[string]any{"login": "agent-go-quicbaas"}},
			{"id": 2, "body": "divergence is not approved", "user": map[string]any{"login": "jeremyhahn"}},
		})

	comments, err := backend.ListIssueComments(context.Background(), testRepo, 32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("got %d comments, want the whole thread", len(comments))
	}
	// OLDEST FIRST, as the port promises: the answer is the LAST thing said,
	// and a reversed thread would put the question after it.
	if comments[0].ID != 1 || comments[1].ID != 2 {
		t.Errorf("comments = %+v, want them oldest first", comments)
	}
	if comments[0].Author != "agent-go-quicbaas" || comments[1].Author != "jeremyhahn" {
		t.Errorf("comments = %+v, want each attributed to its writer", comments)
	}
	// THE BODY IS THE ANSWER. Everything else about a comment is metadata; a
	// mapping that dropped or mis-assigned this would leave every end-to-end
	// test green — they run against a fake forge — and deliver an empty
	// discussion to the one run that needed it.
	if comments[0].Body != "the agent has a question" || comments[1].Body != "divergence is not approved" {
		t.Errorf("comments = %+v, want each carrying its own body", comments)
	}
}

// TestListIssueComments_ReadsPastTheServersWindow. The oldest comments come
// first, so a single-window read returns the original contract and drops the
// answer that re-scoped it — the defect, not a smaller version of it.
//
// THE WINDOW IS NOT A PAGE. This route takes `since` and `before`; `page` and
// `limit` belong to the repo-level /issues/comments route and are ignored here.
// The stub now behaves that way, which is what makes this test able to fail: a
// stub that honoured `page` passed against an adapter that could only ever read
// the first window (go-luks2#3, 2026-09-14).
func TestListIssueComments_ReadsPastTheServersWindow(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	const total = giteaDefaultPageSize + 2
	fixture.respondCommentThread(total, giteaDefaultPageSize)

	comments, err := backend.ListIssueComments(context.Background(), testRepo, 32)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(comments) != total {
		t.Fatalf("got %d comments, want %d — the thread was truncated", len(comments), total)
	}
	if comments[len(comments)-1].ID != int64(total) {
		t.Errorf("last comment is %d, want %d — the newest is the one an answer is in",
			comments[len(comments)-1].ID, total)
	}
}

// TestListIssueComments_AThreadThatNeverEndsIsAFault. A prefix returned with a
// nil error is a lie the caller cannot detect, and the missing part is exactly
// where an answer would be.
func TestListIssueComments_AThreadThatNeverEndsIsAFault(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// A forge that ignores `since` and re-sends the same window for ever. It
	// gives itself away by returning comments OLDER than the cursor it was
	// handed; the window can never move, so any answer would be a prefix.
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/comments"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(commentThread(1, giteaDefaultPageSize))
	}

	if _, err := backend.ListIssueComments(context.Background(), testRepo, 32); !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
}

// TestCommentOnIssue_RefusesAnEmptyBody keeps a no-op write off the forge.
func TestCommentOnIssue_RefusesAnEmptyBody(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	if _, err := backend.CommentOnIssue(context.Background(), testRepo, 32, "   "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// =============================================================================
// Label normalization
// =============================================================================

// TestNormalizedLabels_IgnoresOrderAndCase pins the comparison the read-back
// depends on. A read-back that reported a difference where there is none would
// be turned off, which costs more than not having one.
func TestNormalizedLabels_IgnoresOrderAndCase(t *testing.T) {
	if a, b := normalizedLabels([]string{"Ready", "agent"}), normalizedLabels([]string{"agent", "READY"}); a != b {
		t.Errorf("%s != %s: order or case changed the comparison", a, b)
	}
	if normalizedLabels(nil) != "[]" {
		t.Errorf("an empty label set renders as %q", normalizedLabels(nil))
	}
	if normalizedLabels([]string{"ready"}) == normalizedLabels([]string{"ready", "agent"}) {
		t.Error("different label sets compared equal")
	}
}

// =============================================================================
// Branches
// =============================================================================

// TestCreateBranch_ReportsAnAcceptedCreateThatDidNotHappen covers the read-back.
//
// A branch that was not really created surfaces later as a push to a ref that
// does not exist, where the failure gets attributed to the worker rather than
// to this call.
func TestCreateBranch_ReportsAnAcceptedCreateThatDidNotHappen(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/branches", http.StatusCreated, nil)
	// The read-back finds nothing: the default handler 404s.

	_, err := backend.CreateBranch(context.Background(), testRepo, "issue/32-slug", "main")
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("an accepted-but-absent create was reported as a puzzling NotFound")
	}
}

// TestCreateBranch_ReturnsTheBranchItVerified covers the ordinary path.
func TestCreateBranch_ReturnsTheBranchItVerified(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/branches", http.StatusCreated, nil)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/branches/issue/32-slug", http.StatusOK,
		map[string]any{"name": "issue/32-slug", "commit": map[string]any{"id": "abc123def456"}})

	branch, err := backend.CreateBranch(context.Background(), testRepo, "issue/32-slug", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch.SHA != "abc123def456" {
		t.Errorf("SHA = %q, want the commit the read-back reported", branch.SHA)
	}
}

// TestDeleteBranch_AnAlreadyAbsentBranchIsSuccess proves cleanup is idempotent.
//
// Branch deletion runs after a merge that may itself have deleted the branch. A
// reconciler that failed on "already gone" would report a problem in the exact
// case where the desired state holds.
func TestDeleteBranch_AnAlreadyAbsentBranchIsSuccess(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	// Both the DELETE and the read-back 404 from the default handler.
	if err := backend.DeleteBranch(context.Background(), testRepo, "issue/32-slug"); err != nil {
		t.Fatalf("deleting an absent branch reported failure: %v", err)
	}
}

// TestDeleteBranch_ReportsABranchThatSurvived covers the read-back's other
// direction: a delete Gitea accepted and did not perform.
func TestDeleteBranch_ReportsABranchThatSurvived(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("DELETE /api/v1/repos/jeremyhahn/go-quicbaas/branches/issue/32-slug", http.StatusNoContent, nil)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/branches/issue/32-slug", http.StatusOK,
		map[string]any{"name": "issue/32-slug", "commit": map[string]any{"id": "abc123"}})

	err := backend.DeleteBranch(context.Background(), testRepo, "issue/32-slug")
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
}

// =============================================================================
// Pull requests
// =============================================================================

// pullPayload builds a Gitea pull-request body.
func pullPayload(number int, head, base, sha, state string, merged bool) map[string]any {
	return map[string]any{
		"number": number,
		"state":  state,
		"merged": merged,
		"head":   map[string]any{"ref": head, "sha": sha},
		"base":   map[string]any{"ref": base},
	}
}

// TestCreatePullRequest_VerifiesTheRefsFromASecondRead covers the read-back on
// the fields a later merge is guarded on.
func TestCreatePullRequest_VerifiesTheRefsFromASecondRead(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls", http.StatusCreated,
		pullPayload(41, "issue/32-slug", "main", "abc123", "open", false))
	// The forge's own view disagrees about the base ref.
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41", http.StatusOK,
		pullPayload(41, "issue/32-slug", "develop", "abc123", "open", false))

	_, err := backend.CreatePullRequest(context.Background(), testRepo, PullRequestSpec{
		Title: "fix the thing", HeadRef: "issue/32-slug", BaseRef: "main",
	})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
	if !strings.Contains(err.Error(), "develop") {
		t.Errorf("error %v does not show the ref the forge actually recorded", err)
	}
}

// TestCreatePullRequest_RefusesAnIncompleteSpec keeps a malformed PR off the
// forge.
func TestCreatePullRequest_RefusesAnIncompleteSpec(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	cases := map[string]PullRequestSpec{
		"no title": {HeadRef: "issue/32", BaseRef: "main"},
		"no head":  {Title: "t", BaseRef: "main"},
		"no base":  {Title: "t", HeadRef: "issue/32"},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := backend.CreatePullRequest(context.Background(), testRepo, spec); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// =============================================================================
// Merge — the approved-head guard
// =============================================================================

// TestMergePullRequest_RefusesWhenTheBranchMovedAfterTheReview is the property
// the whole review gate rests on.
//
// A commit pushed after an approval must not land on that approval. The check
// is expressed against the SHA the caller approved, so a moved head is refused
// before any merge is proposed.
func TestMergePullRequest_RefusesWhenTheBranchMovedAfterTheReview(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41", http.StatusOK,
		pullPayload(41, "issue/32-slug", "main", "9999999999999999999999999999999999999999", "open", false))

	err := backend.MergePullRequest(context.Background(), testRepo, 41,
		MergeSpec{Method: "squash"}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err == nil {
		t.Fatal("a merge proceeded against a tree nobody approved")
	}
	if !errors.Is(err, ErrHeadMoved) {
		t.Fatalf("error = %v, want ErrHeadMoved", err)
	}
	// shortSHA renders twelve characters; the error must still identify both
	// trees so an operator can see which commit was approved and which is now
	// on the branch.
	for _, sha := range []string{strings.Repeat("9", 12), strings.Repeat("a", 12)} {
		if !strings.Contains(err.Error(), sha) {
			t.Errorf("error %v does not name both the approved and the current tree", err)
		}
	}

	for _, req := range fixture.requests {
		if strings.HasSuffix(req.Path, "/merge") {
			t.Error("a merge was proposed despite the head having moved")
		}
	}
}

// TestMergePullRequest_SendsTheApprovedHeadAsTheForgeSideGuard proves the
// authoritative check is the forge's, not this adapter's.
//
// The pre-read is check-then-act and has a window a push can land in; sending
// head_commit_id makes Gitea itself refuse a moved head, which decides the race
// where the race happens.
func TestMergePullRequest_SendsTheApprovedHeadAsTheForgeSideGuard(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41", http.StatusOK,
		pullPayload(41, "issue/32-slug", "main", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "open", true))
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/merge", http.StatusOK, nil)

	if err := backend.MergePullRequest(context.Background(), testRepo, 41,
		MergeSpec{Method: "squash", CommitTitle: "fix the thing"}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var merge string
	for _, req := range fixture.requests {
		if strings.HasSuffix(req.Path, "/merge") {
			merge = req.Body
		}
	}
	if !strings.Contains(merge, `"head_commit_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`) {
		t.Errorf("merge body %q does not carry the approved head as the forge-side guard", merge)
	}
	// Gitea's MergePullRequestOption is snake_case. "Do" is the Go field name
	// and binds to nothing, so a merge sent that way fails on the required
	// "do" — and the title and message would be dropped even if it did not.
	if !strings.Contains(merge, `"do":"squash"`) {
		t.Errorf("merge body %q does not request the strategy's method under Gitea's own field name", merge)
	}
	if !strings.Contains(merge, `"merge_title_field":"fix the thing"`) {
		t.Errorf("merge body %q does not carry the commit title under Gitea's own field name", merge)
	}
}

// TestMergePullRequest_RefusesAMethodGiteaDoesNotHave proves a merge is never
// substituted.
//
// A squash strategy that silently produced a merge commit would corrupt exactly
// the history the strategy exists to shape, so an unknown method is refused as
// an unsupported capability rather than defaulted.
func TestMergePullRequest_RefusesAMethodGiteaDoesNotHave(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	err := backend.MergePullRequest(context.Background(), testRepo, 41,
		MergeSpec{Method: "fast-forward-only"}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if !errors.Is(err, ErrForgeUnsupported) {
		t.Fatalf("error = %v, want ErrForgeUnsupported", err)
	}
}

// TestMergePullRequest_RefusesWithoutAnApprovedHead proves the guard cannot be
// opted out of by omission.
func TestMergePullRequest_RefusesWithoutAnApprovedHead(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	err := backend.MergePullRequest(context.Background(), testRepo, 41, MergeSpec{Method: "squash"}, "")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// TestMergePullRequest_ReportsAMergeThatDidNotHappen covers the read-back.
func TestMergePullRequest_ReportsAMergeThatDidNotHappen(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// Open before and still open after: the merge was accepted and did nothing.
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41", http.StatusOK,
		pullPayload(41, "issue/32-slug", "main", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "open", false))
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/merge", http.StatusOK, nil)

	err := backend.MergePullRequest(context.Background(), testRepo, 41,
		MergeSpec{Method: "squash"}, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
}

// =============================================================================
// Reviews
// =============================================================================

// TestListReviews_BindsEachVerdictToTheTreeItSaw is what stops a stale approval
// clearing a gate.
//
// Gitea reports the reviewed commit as commit_id; carrying it as HeadSHA is
// what lets the review gate refuse a verdict given to an earlier tree.
func TestListReviews_BindsEachVerdictToTheTreeItSaw(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/reviews", http.StatusOK,
		[]map[string]any{
			{"id": 1, "state": "APPROVED", "commit_id": "oldsha111", "user": map[string]any{"login": "codex"}},
			{"id": 2, "state": "REQUEST_CHANGES", "commit_id": "newsha222", "user": map[string]any{"login": "codex"}},
		})

	reviews, err := backend.ListReviews(context.Background(), testRepo, 41)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("reviews = %d, want 2", len(reviews))
	}
	if reviews[0].HeadSHA != "oldsha111" || reviews[1].HeadSHA != "newsha222" {
		t.Error("a verdict lost the tree it was given; a stale approval could clear the gate")
	}
	if reviews[0].Reviewer != "codex" {
		t.Errorf("Reviewer = %q, want codex", reviews[0].Reviewer)
	}
}

// TestCreatePullRequest_HappyPathAppliesLabelsAndReturnsTheForgeSView covers
// the ordinary create, including the label pass a spec can carry.
func TestCreatePullRequest_HappyPathAppliesLabelsAndReturnsTheForgeSView(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls", http.StatusCreated,
		pullPayload(41, "issue/32-slug", "main", "abc123", "open", false))
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41", http.StatusOK,
		pullPayload(41, "issue/32-slug", "main", "abc123", "open", false))
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK,
		[]map[string]any{{"id": 3, "name": "in-review"}})
	fixture.respondJSON("PUT /api/v1/repos/jeremyhahn/go-quicbaas/issues/41/labels", http.StatusOK, nil)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/41", http.StatusOK, map[string]any{
		"number": 41,
		"labels": []map[string]any{{"id": 3, "name": "in-review"}},
	})

	pull, err := backend.CreatePullRequest(context.Background(), testRepo, PullRequestSpec{
		Title: "fix the thing", HeadRef: "issue/32-slug", BaseRef: "main",
		Labels: []string{"in-review"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pull.Number != 41 || pull.HeadSHA != "abc123" {
		t.Errorf("pull = %+v, want #41 at abc123", pull)
	}
}

// TestCreatePullRequest_ReportsACreateThatReturnedNoNumber covers the response
// that says nothing was created.
func TestCreatePullRequest_ReportsACreateThatReturnedNoNumber(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls", http.StatusCreated, map[string]any{})

	_, err := backend.CreatePullRequest(context.Background(), testRepo, PullRequestSpec{
		Title: "t", HeadRef: "issue/32-slug", BaseRef: "main",
	})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestGiteaMergeMethod_AcceptsEveryMethodGiteaHas covers the mapping, including
// the case- and space-insensitivity a config value may arrive with.
func TestGiteaMergeMethod_AcceptsEveryMethodGiteaHas(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	for _, method := range []string{"squash", "Squash", " merge ", "rebase"} {
		if _, err := backend.giteaMergeMethod(method); err != nil {
			t.Errorf("method %q was refused: %v", method, err)
		}
	}
	if _, err := backend.giteaMergeMethod(""); !errors.Is(err, ErrForgeUnsupported) {
		t.Errorf("an unset merge method returned %v, want ErrForgeUnsupported", err)
	}
}

// TestCreateBranch_RefusesAnUnusableRequest keeps a nameless or baseless branch
// off the forge.
func TestCreateBranch_RefusesAnUnusableRequest(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	if _, err := backend.CreateBranch(context.Background(), testRepo, "", "main"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("an unnamed branch returned %v, want ErrInvalidInput", err)
	}
	if _, err := backend.CreateBranch(context.Background(), testRepo, "issue/32", ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("a branch with no base returned %v, want ErrInvalidInput", err)
	}
}

// TestShortSHA_RendersTwelveCharactersOrTheWholeThing covers both branches of
// the error-formatting helper.
func TestShortSHA_RendersTwelveCharactersOrTheWholeThing(t *testing.T) {
	if got := shortSHA(strings.Repeat("a", 40)); got != strings.Repeat("a", 12) {
		t.Errorf("shortSHA(40 chars) = %q, want 12", got)
	}
	if got := shortSHA("abc123"); got != "abc123" {
		t.Errorf("shortSHA(short) = %q, want it unchanged", got)
	}
}

// TestGiteaErrorBodies_AreTruncatedBeforeReachingALog is a log-hygiene
// property: an error body is remote input, and an unbounded one would put an
// arbitrary amount of someone else's text into an operator's terminal.
func TestGiteaErrorBodies_AreTruncatedBeforeReachingALog(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32", http.StatusInternalServerError,
		map[string]any{"message": strings.Repeat("x", 5000)})

	_, err := backend.GetIssue(context.Background(), testRepo, 32)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(err.Error()) > 600 {
		t.Errorf("error message is %d bytes; a remote body reached a log unbounded", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "…") {
		t.Error("a truncated body is not marked as truncated")
	}
}

// TestGitea_ANonJSONErrorBodyStillReportsTheStatus covers the fallback when
// Gitea answers with something that is not its usual envelope.
func TestGitea_ANonJSONErrorBodyStillReportsTheStatus(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/issues/32"] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>gateway error</html>"))
	}

	_, err := backend.GetIssue(context.Background(), testRepo, 32)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error %v does not name the status", err)
	}
}

// =============================================================================
// Webhook verification
// =============================================================================

// signGitea produces the signature Gitea would send for payload.
func signGitea(t *testing.T, payload, secret []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// TestVerifyWebhook_AcceptsOnlyAGenuineSignature covers the one delivery that
// should be processed.
func TestVerifyWebhook_AcceptsOnlyAGenuineSignature(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	payload := []byte(`{"action":"opened"}`)
	secret := []byte("s3cret")

	if err := backend.VerifyWebhook(payload, signGitea(t, payload, secret), secret); err != nil {
		t.Fatalf("a genuine delivery was rejected: %v", err)
	}
}

// TestVerifyWebhook_FailsClosedOnEveryUnverifiablePath is the security
// property.
//
// Each case is a way a delivery can fail to authenticate, and NONE of them may
// be treated as acceptable. The missing-secret case is the dangerous one: a
// deployment that forgot to configure signing must refuse deliveries, not
// accept every unsigned one — that would turn a config mistake into an open
// door for anyone who can reach the endpoint.
func TestVerifyWebhook_FailsClosedOnEveryUnverifiablePath(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	payload := []byte(`{"action":"opened"}`)
	secret := []byte("s3cret")
	genuine := signGitea(t, payload, secret)

	cases := map[string]struct {
		payload   []byte
		signature string
		secret    []byte
	}{
		"no secret configured":    {payload, genuine, nil},
		"no signature sent":       {payload, "", secret},
		"signature is not hex":    {payload, "not-hex-at-all", secret},
		"signature of a lie":      {[]byte(`{"action":"tampered"}`), genuine, secret},
		"signed by the wrong key": {payload, signGitea(t, payload, []byte("other")), secret},
		"truncated signature":     {payload, genuine[:len(genuine)-2], secret},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := backend.VerifyWebhook(tc.payload, tc.signature, tc.secret)
			if err == nil {
				t.Fatal("an unverifiable delivery was accepted")
			}
			if !errors.Is(err, ErrWebhookUnverified) {
				t.Errorf("error = %v, want ErrWebhookUnverified", err)
			}
		})
	}
}

// TestVerifyWebhook_NeverQuotesTheUnverifiedPayload is a log-injection
// property.
//
// At the point verification fails the payload is attacker-controlled, so
// echoing any of it into an error puts chosen text in an operator's terminal
// and in whatever ingests those logs.
func TestVerifyWebhook_NeverQuotesTheUnverifiedPayload(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	marker := "ATTACKER-CONTROLLED-MARKER"
	payload := []byte(`{"action":"` + marker + `"}`)

	err := backend.VerifyWebhook(payload, "deadbeef", []byte("s3cret"))
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(err.Error(), marker) {
		t.Error("the unverified payload was quoted into an error message")
	}
}

// =============================================================================
// Webhook parsing
// =============================================================================

// TestParseWebhook_MapsTheEventsTheModuleActsOn covers the deliveries that
// prompt a reconcile.
func TestParseWebhook_MapsTheEventsTheModuleActsOn(t *testing.T) {
	backend, _ := newGiteaFixture(t)

	cases := []struct {
		name      string
		eventType string
		payload   string
		want      ForgeEventKind
	}{
		{"issue opened", "issues", `{"action":"opened","issue":{"number":32}}`, EventIssueOpened},
		{"issue closed", "issues", `{"action":"closed","issue":{"number":32}}`, EventIssueClosed},
		{"pr opened", "pull_request", `{"action":"opened","pull_request":{"number":41}}`, EventPullRequestOpened},
		{"pr updated", "pull_request", `{"action":"synchronized","pull_request":{"number":41}}`, EventPullRequestUpdated},
		{"pr merged", "pull_request", `{"action":"closed","pull_request":{"number":41,"merged":true}}`, EventPullRequestMerged},
		{"pr closed unmerged", "pull_request", `{"action":"closed","pull_request":{"number":41}}`, EventPullRequestClosed},
		{"review", "pull_request_review", `{"number":41}`, EventReviewSubmitted},
		{"ci status", "status", `{"sha":"abc123"}`, EventCheckRunUpdated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event, err := backend.ParseWebhook(tc.eventType, []byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if event.Kind != tc.want {
				t.Errorf("kind = %q, want %q", event.Kind, tc.want)
			}
		})
	}
}

// TestParseWebhook_APipelineDeliveryCarriesItsCommit is load-bearing since
// go-quicbaas#927: a review is dispatched on the delivery that says CI finished,
// and that delivery names a COMMIT and no pull request. The dispatcher resolves
// the commit to the pull request it heads, so a parser that dropped the head
// would silently stop every gated review — a reviewer that never runs, with no
// error anywhere to say why.
//
// All three of Gitea's spellings are covered, because the module subscribes to
// `status` and the forge may deliver any of them.
func TestParseWebhook_APipelineDeliveryCarriesItsCommit(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	head := "0123456789abcdef0123456789abcdef01234567"
	for name, tc := range map[string]struct {
		eventType string
		payload   string
	}{
		// A commit status names the commit as `sha`.
		"status": {"status", `{"sha":"` + head + `"}`},
		// A check run names it as `commit_id`, which takes precedence because it
		// is the commit the RUN is about rather than whatever else the payload
		// happens to carry.
		"check_run":    {"check_run", `{"commit_id":"` + head + `"}`},
		"workflow_job": {"workflow_job", `{"commit_id":"` + head + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			event, err := backend.ParseWebhook(tc.eventType, []byte(tc.payload))
			if err != nil {
				t.Fatalf("ParseWebhook: %v", err)
			}
			if event.Kind != EventCheckRunUpdated {
				t.Fatalf("kind = %q, want %q", event.Kind, EventCheckRunUpdated)
			}
			if event.HeadSHA != head {
				t.Fatalf("head = %q, want %q; a pipeline delivery with no commit resolves to no pull request", event.HeadSHA, head)
			}
			if event.PullRequestNumber != 0 {
				t.Fatalf("pull request = %d, want 0; a pipeline delivery names none", event.PullRequestNumber)
			}
		})
	}
}

// TestParseWebhook_AnUnmappedEventIsOrdinaryNotAnError proves ordinary forge
// traffic does not look like an outage.
//
// Forges send plenty the module does not act on. Reporting those as failures
// would fill the log with errors for a healthy webhook, and the real failures
// would be lost in them.
func TestParseWebhook_AnUnmappedEventIsOrdinaryNotAnError(t *testing.T) {
	backend, _ := newGiteaFixture(t)

	event, err := backend.ParseWebhook("release", []byte(`{"action":"published"}`))
	if err != nil {
		t.Fatalf("an unmapped event was reported as a failure: %v", err)
	}
	if event.Kind != EventUnsupported {
		t.Errorf("kind = %q, want unsupported", event.Kind)
	}
}

// TestParseWebhook_CarriesTheSubjectSoAReconcileCanReReadIt covers the fields
// the reconciler uses. The event is a hint; these are what it re-reads.
func TestParseWebhook_CarriesTheSubjectSoAReconcileCanReReadIt(t *testing.T) {
	backend, _ := newGiteaFixture(t)

	event, err := backend.ParseWebhook("pull_request", []byte(`{
		"action":"synchronized",
		"pull_request":{"number":41,"head":{"sha":"abc123"}},
		"repository":{"name":"go-quicbaas","owner":{"login":"jeremyhahn"}}
	}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Repo.String() != "jeremyhahn/go-quicbaas" {
		t.Errorf("repo = %q, want jeremyhahn/go-quicbaas", event.Repo)
	}
	if event.PullRequestNumber != 41 || event.HeadSHA != "abc123" {
		t.Errorf("event = %+v, want PR 41 at abc123", event)
	}
}

// TestParseWebhook_RejectsAMalformedBody keeps undecodable input from becoming
// a zero-valued event that names repository "" and issue 0.
func TestParseWebhook_RejectsAMalformedBody(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	if _, err := backend.ParseWebhook("issues", []byte("{not json")); !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
}

// =============================================================================
// CI runs
// =============================================================================

// TestCancelCheckRun_RefusesRatherThanPretending is the capability flag made
// real.
//
// Gitea publishes no cancel route. Returning nil would tell a caller the run
// stopped, so it would stop tracking capacity the run is still consuming and
// the supersession record would claim a cancellation that never happened.
func TestCancelCheckRun_RefusesRatherThanPretending(t *testing.T) {
	backend, _ := newGiteaFixture(t)

	err := backend.CancelCheckRun(context.Background(), testRepo, "7")
	if err == nil {
		t.Fatal("gitea reported a cancellation it cannot perform")
	}
	if !errors.Is(err, ErrForgeUnsupported) {
		t.Fatalf("error = %v, want ErrForgeUnsupported", err)
	}
	if backend.Capabilities().CancelCheckRuns {
		t.Error("the capability flag disagrees with the method's behaviour")
	}
}

// TestListCheckRuns_SeparatesRunningFromFinished is what supersession reads.
//
// A still-running displaced run is worth acting on; a finished one needs
// nothing. An unrecognised state is reported as still running, erring toward
// "might still be consuming capacity" rather than assuming a state Gitea did
// not claim.
func TestListCheckRuns_SeparatesRunningFromFinished(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/commits/abc123/statuses", http.StatusOK,
		[]map[string]any{
			{"id": 1, "status": "pending", "context": "lint"},
			{"id": 2, "status": "success", "context": "test"},
			{"id": 3, "status": "failure", "context": "build"},
			{"id": 4, "status": "something-new", "context": "future"},
			{"id": 5, "status": "skipped", "context": "docker"},
		})

	runs, err := backend.ListCheckRuns(context.Background(), testRepo, "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	byName := map[string]ForgeCheckRun{}
	for _, run := range runs {
		byName[run.Name] = run
	}
	if byName["test"].Conclusion != "success" || byName["test"].Status != "completed" {
		t.Errorf("a successful run reported as %+v", byName["test"])
	}
	if byName["build"].Conclusion != "failure" {
		t.Errorf("a failed run reported as %+v", byName["build"])
	}
	if byName["future"].Status != "in_progress" {
		t.Error("an unrecognised state was assumed finished; a live run could stop being tracked")
	}
	// A job the pipeline skipped is finished with its own conclusion — not a
	// run still consuming capacity, which is what blocked every review of a
	// pull request whose Docker job is skipped (go-quicbaas PR #970).
	if byName["docker"].Status != "completed" || byName["docker"].Conclusion != "skipped" {
		t.Errorf("a skipped run reported as %+v", byName["docker"])
	}
	if byName["lint"].HeadSHA != "abc123" {
		t.Error("a run lost the commit it belongs to")
	}
}

// TestListCheckRuns_RefusesWithoutACommit keeps a repo-wide query from standing
// in for a per-commit one.
func TestListCheckRuns_RefusesWithoutACommit(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	if _, err := backend.ListCheckRuns(context.Background(), testRepo, ""); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// =============================================================================
// EnsureWebhook
// =============================================================================

// TestEnsureWebhook_RefusesAnUnsignableDelivery proves a webhook cannot be
// configured without a secret.
//
// A delivery nothing can verify is discarded on arrival, so configuring one is
// building an endpoint whose traffic is guaranteed to be thrown away.
func TestEnsureWebhook_RefusesAnUnsignableDelivery(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	err := backend.EnsureWebhook(context.Background(), testRepo,
		WebhookSpec{URL: "http://module/hook", Events: []string{"issues"}}, nil)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// TestEnsureWebhook_CreatesThenVerifiesByReadBack covers the create path.
func TestEnsureWebhook_CreatesThenVerifiesByReadBack(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	created := false
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/hooks"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !created {
			_, _ = w.Write([]byte(`[]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":9,"active":true,"events":["issues"],"config":{"url":"http://module/hook"}}]`))
	}
	fixture.handlers["POST /api/v1/repos/jeremyhahn/go-quicbaas/hooks"] = func(w http.ResponseWriter, _ *http.Request) {
		created = true
		w.WriteHeader(http.StatusCreated)
	}

	if err := backend.EnsureWebhook(context.Background(), testRepo,
		WebhookSpec{URL: "http://module/hook", Events: []string{"issues"}}, []byte("s3cret")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestEnsureWebhook_ReportsADeliveryThatNeverAppeared covers the read-back.
func TestEnsureWebhook_ReportsADeliveryThatNeverAppeared(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/hooks", http.StatusOK, []map[string]any{})
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/hooks", http.StatusCreated, nil)

	err := backend.EnsureWebhook(context.Background(), testRepo,
		WebhookSpec{URL: "http://module/hook", Events: []string{"issues"}}, []byte("s3cret"))
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestEnsureWebhook_TheSpecCarriesNoSecret is a leak-prevention property.
//
// A WebhookSpec is stored, logged and compared. The secret travels as a
// separate argument so it cannot ride along into any of those.
func TestEnsureWebhook_TheSpecCarriesNoSecret(t *testing.T) {
	spec := WebhookSpec{URL: "http://module/hook", Events: []string{"issues"}, SecretRef: "webhook/go-quicbaas"}
	encoded, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshalling the spec: %v", err)
	}
	if strings.Contains(string(encoded), "s3cret") {
		t.Error("a secret value reached the spec")
	}
	if !strings.Contains(string(encoded), "webhook/go-quicbaas") {
		t.Error("the spec lost the reference naming where the secret lives")
	}
}

// TestGiteaDeliveryHeaders_AreTheIngestContract pins the header names the HTTP
// ingest layer reads before calling VerifyWebhook and ParseWebhook.
//
// They are exported rather than internal because verification takes the
// signature as an argument: that is what lets it be tested and reused without
// an http.Request, and it means the header names have to be stated somewhere
// the caller can see.
func TestGiteaDeliveryHeaders_AreTheIngestContract(t *testing.T) {
	headers := map[string]string{
		GiteaSignatureHeader: "X-Gitea-Signature",
		GiteaEventHeader:     "X-Gitea-Event",
		GiteaDeliveryHeader:  "X-Gitea-Delivery",
	}
	for got, want := range headers {
		if got != want {
			t.Errorf("header constant = %q, want %q", got, want)
		}
	}
}

// TestGiteaBackend_IsRegisteredAsAFactory proves the adapter is selectable now
// that it is complete, and that the factory honours the project's own config.
func TestGiteaBackend_IsRegisteredAsAFactory(t *testing.T) {
	backend, err := NewForgeBackend(ForgeConfig{
		Type:    ForgeGitea,
		BaseURL: "http://gitea.example/api/v1",
	}, ForgeCredentials{Token: "a-token"})
	if err != nil {
		t.Fatalf("the completed gitea adapter is not selectable: %v", err)
	}
	if backend.Type() != ForgeGitea {
		t.Errorf("Type() = %v, want gitea", backend.Type())
	}
	// The factory must fail closed on a project that supplies no credential.
	if _, err := NewForgeBackend(ForgeConfig{Type: ForgeGitea, BaseURL: "http://x/api/v1"}, ForgeCredentials{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("an untokened project built an adapter (err = %v)", err)
	}
}

// TestEnsureWebhook_UpdatesTheExistingDeliveryRatherThanAddingASecond covers
// the update path.
//
// Creating a second delivery to the same URL would double every event, and the
// reconciler would do its work twice for as long as nobody noticed.
func TestEnsureWebhook_UpdatesTheExistingDeliveryRatherThanAddingASecond(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	patched := false
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/hooks"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !patched {
			// Present, but subscribed to the wrong events.
			_, _ = w.Write([]byte(`[{"id":9,"active":true,"events":["push"],"config":{"url":"http://module/hook"}}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":9,"active":true,"events":["issues","pull_request"],"config":{"url":"http://module/hook"}}]`))
	}
	fixture.handlers["PATCH /api/v1/repos/jeremyhahn/go-quicbaas/hooks/9"] = func(w http.ResponseWriter, _ *http.Request) {
		patched = true
		w.WriteHeader(http.StatusOK)
	}

	if err := backend.EnsureWebhook(context.Background(), testRepo,
		WebhookSpec{URL: "http://module/hook", Events: []string{"issues", "pull_request"}}, []byte("s3cret")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, req := range fixture.requests {
		if req.Method == http.MethodPost && strings.HasSuffix(req.Path, "/hooks") {
			t.Error("a second delivery was created for a URL that already had one; every event would arrive twice")
		}
	}
	if !patched {
		t.Error("the existing delivery was never updated")
	}
}

// TestEnsureWebhook_ReportsADeliveryLeftInactive covers the read-back's other
// direction: a hook that exists but delivers nothing.
func TestEnsureWebhook_ReportsADeliveryLeftInactive(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/hooks", http.StatusOK,
		[]map[string]any{{"id": 9, "active": false, "events": []string{"issues"},
			"config": map[string]string{"url": "http://module/hook"}}})
	fixture.respondJSON("PATCH /api/v1/repos/jeremyhahn/go-quicbaas/hooks/9", http.StatusOK, nil)

	err := backend.EnsureWebhook(context.Background(), testRepo,
		WebhookSpec{URL: "http://module/hook", Events: []string{"issues"}}, []byte("s3cret"))
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
	if !strings.Contains(err.Error(), "inactive") {
		t.Errorf("error %v does not say the delivery is inactive", err)
	}
}

// TestEnsureWebhook_RefusesASpecThatCannotDeliver covers the remaining
// fail-closed validation.
func TestEnsureWebhook_RefusesASpecThatCannotDeliver(t *testing.T) {
	backend, _ := newGiteaFixture(t)
	cases := map[string]WebhookSpec{
		"no url":    {Events: []string{"issues"}},
		"no events": {URL: "http://module/hook"},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if err := backend.EnsureWebhook(context.Background(), testRepo, spec, []byte("s")); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// TestCreatePullRequest_DraftUsesGiteaSTitlePrefixAndIsVerified covers a
// capability Gitea implements differently than its API shape suggests.
//
// There is no `draft` field on CreatePullRequestOption in 1.27.1 — Gitea marks
// a draft by a title prefix. Sending a draft boolean sets nothing and quietly
// produces an ordinary pull request, which is worse than lacking the feature:
// the caller believes work is held back when it is open for review.
func TestCreatePullRequest_DraftUsesGiteaSTitlePrefixAndIsVerified(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls", http.StatusCreated,
		pullPayload(41, "issue/32-slug", "main", "abc123", "open", false))
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		payload := pullPayload(41, "issue/32-slug", "main", "abc123", "open", false)
		payload["draft"] = true
		_ = json.NewEncoder(w).Encode(payload)
	}

	if _, err := backend.CreatePullRequest(context.Background(), testRepo, PullRequestSpec{
		Title: "fix the thing", HeadRef: "issue/32-slug", BaseRef: "main", Draft: true,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var create string
	for _, req := range fixture.requests {
		if req.Method == http.MethodPost {
			create = req.Body
		}
	}
	if !strings.Contains(create, giteaDraftPrefix) {
		t.Errorf("create body %q does not mark the pull request draft the way Gitea recognises", create)
	}
	if strings.Contains(create, `"draft":true`) {
		t.Error("a draft boolean was sent; CreatePullRequestOption has no such field and it sets nothing")
	}
}

// TestCreatePullRequest_ADraftThatCameBackReadyFailsLoudly proves the read-back
// catches a server whose WIP prefix differs from the one we send.
//
// The prefix is server-configurable, so this is the case that turns a silent
// "held work is actually open" into a reported failure.
func TestCreatePullRequest_ADraftThatCameBackReadyFailsLoudly(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls", http.StatusCreated,
		pullPayload(41, "issue/32-slug", "main", "abc123", "open", false))
	// draft absent from the response: the prefix was not recognised.
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41", http.StatusOK,
		pullPayload(41, "issue/32-slug", "main", "abc123", "open", false))

	_, err := backend.CreatePullRequest(context.Background(), testRepo, PullRequestSpec{
		Title: "fix the thing", HeadRef: "issue/32-slug", BaseRef: "main", Draft: true,
	})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
	if !strings.Contains(err.Error(), "draft") {
		t.Errorf("error %v does not say the draft state was not applied", err)
	}
}

// TestGitea_NotReadyIsRetryableNotAFailure covers the one error class in this
// package a caller should retry.
//
// Gitea computes a pull request's mergeability asynchronously and answers 405
// "Please try again later" until it has. The merge was neither performed nor
// refused, so reporting it as a backend failure would make the module abandon
// merges that were about to become possible.
func TestGitea_NotReadyIsRetryableNotAFailure(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41", http.StatusOK,
		pullPayload(41, "issue/32-slug", "main", "approvedsha111", "open", false))
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/merge",
		http.StatusMethodNotAllowed, map[string]any{"message": "Please try again later"})

	err := backend.MergePullRequest(context.Background(), testRepo, 41,
		MergeSpec{Method: "squash"}, "approvedsha111")
	if !errors.Is(err, ErrForgeNotReady) {
		t.Fatalf("error = %v, want ErrForgeNotReady", err)
	}
	if errors.Is(err, ErrForgeWriteNotApplied) {
		t.Error("a not-yet-computed merge was reported as a write that did not apply")
	}
}

// TestGitea_MergeConflictIsTheSettledNotMergeable classifies a real Gitea
// conflict end to end, from the adapter's wire decoding up to the hooks'
// verdict.
//
// Gitea 1.27.1 does not tell a conflict from a check in progress on the merge
// endpoint. CheckPullMergeable returns ErrNotMergeableState for every status
// except mergeable, and the API maps that to 405 "Please try again later". The
// PR's `mergeable` field is also false for checking, conflict and error alike
// (models/issues/pull.go). So the adapter correctly reports each merge as not
// ready, and it decodes the `mergeable` field faithfully. The settled read
// after the bound is what turns an answer that never changes into a conflict.
// The measured case is go-truststrap#64 (2026-09-22): `mergeable: false`, head
// cf56c2ef, base main.
func TestGitea_MergeConflictIsTheSettledNotMergeable(t *testing.T) {
	cases := []struct {
		name      string
		mergeable bool
		want      error
		reject    error
	}{
		{name: "not mergeable for the whole bound is a conflict", mergeable: false,
			want: ErrMergeConflict, reject: ErrMergeNotReadyExhausted},
		{name: "mergeable yet refused is a forge that is not ready", mergeable: true,
			want: ErrMergeNotReadyExhausted, reject: ErrMergeConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			pull := pullPayload(64, "issue/62-slug", "main", "cf56c2ef978063904fc0050373efba6d97608615", "open", false)
			pull["mergeable"] = tc.mergeable
			fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/64", http.StatusOK, pull)
			fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/64/merge",
				http.StatusMethodNotAllowed, map[string]any{"message": "Please try again later", "url": "http://gitea/api/swagger"})

			// The adapter's half: every merge is not ready, and the field
			// that separates the two cases is decoded as the forge sent it.
			head := "cf56c2ef978063904fc0050373efba6d97608615"
			if err := backend.MergePullRequest(context.Background(), testRepo, 64,
				MergeSpec{Method: MergeMethodSquash}, head); !errors.Is(err, ErrForgeNotReady) {
				t.Fatalf("MergePullRequest = %v, want ErrForgeNotReady", err)
			}
			read, err := backend.GetPullRequest(context.Background(), testRepo, 64)
			if err != nil {
				t.Fatal(err)
			}
			if read.Mergeable != tc.mergeable || read.BaseRef != "main" {
				t.Fatalf("GetPullRequest = mergeable %v base %q, want %v main", read.Mergeable, read.BaseRef, tc.mergeable)
			}

			// The hooks' half, over the real adapter.
			fixture.requests = nil
			waiter := &immediateWaiter{}
			hooks := &Hooks{attempts: 3, waiter: waiter}
			err = hooks.merge(context.Background(), backend, testRepo, 64, MergeSpec{Method: MergeMethodSquash}, head)
			if !errors.Is(err, tc.want) || errors.Is(err, tc.reject) {
				t.Fatalf("merge = %v, want %v and not %v", err, tc.want, tc.reject)
			}
			merges := 0
			for _, req := range fixture.requests {
				if req.Method == http.MethodPost && strings.HasSuffix(req.Path, "/pulls/64/merge") {
					merges++
				}
			}
			if merges != 3 || waiter.waits != 2 {
				t.Fatalf("merges=%d waits=%d, want the full bound spent before the verdict", merges, waiter.waits)
			}
		})
	}
}

// TestGitea_AGenuine405IsStillAFailure keeps the retryable class narrow.
//
// The status alone is ambiguous — only Gitea's message distinguishes "still
// computing" from a real method refusal — and treating every 405 as retryable
// would turn a permanent refusal into a timeout.
func TestGitea_AGenuine405IsStillAFailure(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41", http.StatusOK,
		pullPayload(41, "issue/32-slug", "main", "approvedsha111", "open", false))
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/pulls/41/merge",
		http.StatusMethodNotAllowed, map[string]any{"message": "merging is not allowed on this branch"})

	err := backend.MergePullRequest(context.Background(), testRepo, 41,
		MergeSpec{Method: "squash"}, "approvedsha111")
	if errors.Is(err, ErrForgeNotReady) {
		t.Fatal("a permanent refusal was reported as retryable; the module would retry it until it timed out")
	}
	if !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
}

// TestEnsureWebhook_RejectsASubscriptionNobodyAskedFor is the correction to an
// over-broad read-back.
//
// Accepting any superset would call a failed REMOVAL a success: a hook left
// carrying `push` after an update that should have dropped it keeps delivering
// events outside the spec. Gitea's own family expansions are still accepted,
// because those are the server's doing and were measured.
func TestEnsureWebhook_RejectsASubscriptionNobodyAskedFor(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/hooks"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The requested family IS present, plus a stale `push` an update
		// should have removed.
		_, _ = w.Write([]byte(`[{"id":9,"active":true,"events":["issues","issue_comment","push"],
			"config":{"url":"http://module/hook"}}]`))
	}
	fixture.respondJSON("PATCH /api/v1/repos/jeremyhahn/go-quicbaas/hooks/9", http.StatusOK, nil)

	err := backend.EnsureWebhook(context.Background(), testRepo,
		WebhookSpec{URL: "http://module/hook", Events: []string{"issues"}}, []byte("s3cret"))
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
	if !strings.Contains(err.Error(), "push") {
		t.Errorf("error %v does not name the subscription nobody asked for", err)
	}
}

// TestEnsureWebhook_AcceptsGiteaSOwnFamilyExpansion pins the other side.
//
// Asking for "issues" yields five events on a real server. Measured, not
// guessed — and a read-back that reported that as a failed write would be
// switched off, which costs more than having none.
func TestEnsureWebhook_AcceptsGiteaSOwnFamilyExpansion(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/hooks"] = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":9,"active":true,
			"events":["issues","issue_assign","issue_comment","issue_label","issue_milestone"],
			"config":{"url":"http://module/hook"}}]`))
	}
	fixture.respondJSON("PATCH /api/v1/repos/jeremyhahn/go-quicbaas/hooks/9", http.StatusOK, nil)

	if err := backend.EnsureWebhook(context.Background(), testRepo,
		WebhookSpec{URL: "http://module/hook", Events: []string{"issues"}}, []byte("s3cret")); err != nil {
		t.Fatalf("gitea's own expansion was reported as a failed write: %v", err)
	}
}

// TestResolveLabelIDs_PaginatesBeyondOnePage covers the lie a single page told.
//
// Reading only the first page reported "repository defines no label named X"
// for a label the repository plainly defines, sending whoever read it to fix a
// vocabulary that was already correct.
func TestResolveLabelIDs_PaginatesBeyondOnePage(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/labels"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		labels := make([]map[string]any, 0, giteaDefaultPageSize)
		if r.URL.Query().Get("page") == "1" {
			for i := 0; i < giteaDefaultPageSize; i++ {
				labels = append(labels, map[string]any{"id": i + 1, "name": fmt.Sprintf("filler-%d", i)})
			}
		} else {
			labels = append(labels, map[string]any{"id": 999, "name": "on-the-second-page"})
		}
		_ = json.NewEncoder(w).Encode(labels)
	}

	ids, err := backend.resolveLabelIDs(context.Background(), testRepo, []string{"on-the-second-page"})
	if err != nil {
		t.Fatalf("a label on the second page was reported as undefined: %v", err)
	}
	if len(ids) != 1 || ids[0] != 999 {
		t.Errorf("ids = %v, want the second page's label", ids)
	}
}

// =============================================================================
// GetTag — the published-artifact signal
// =============================================================================

// TestGiteaBackend_GetTagReadsThePublishedTag asserts the route as well as the
// decode: the adapter must ask Gitea's own tag endpoint, because that is the
// record a module version pin resolves against.
func TestGiteaBackend_GetTagReadsThePublishedTag(t *testing.T) {
	t.Parallel()
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/tags/v0.9.5-alpha",
		http.StatusOK, map[string]any{
			"name":   "v0.9.5-alpha",
			"commit": map[string]any{"sha": "deadbeefcafe"},
		})

	tag, err := backend.GetTag(context.Background(), testRepo, "v0.9.5-alpha")
	if err != nil {
		t.Fatalf("GetTag: %v", err)
	}
	if tag.Name != "v0.9.5-alpha" || tag.SHA != "deadbeefcafe" {
		t.Fatalf("GetTag = %+v", tag)
	}
	if len(fixture.requests) != 1 || fixture.requests[0].Method != http.MethodGet {
		t.Fatalf("requests = %+v, want one GET", fixture.requests)
	}
}

// TestGiteaBackend_GetTagCarriesAPathBearingTagIntact: a release tag may contain
// a slash ("release/v1.0.0"), and the whole name has to reach the forge as the
// tag it is rather than being truncated at the separator.
func TestGiteaBackend_GetTagCarriesAPathBearingTagIntact(t *testing.T) {
	t.Parallel()
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/tags/release/v1.0.0",
		http.StatusOK, map[string]any{"name": "release/v1.0.0"})

	tag, err := backend.GetTag(context.Background(), testRepo, "release/v1.0.0")
	if err != nil {
		t.Fatalf("GetTag: %v", err)
	}
	if tag.Name != "release/v1.0.0" {
		t.Fatalf("GetTag = %+v", tag)
	}
	if got := fixture.requests[0].Path; got != "/api/v1/repos/jeremyhahn/go-quicbaas/tags/release/v1.0.0" {
		t.Fatalf("path = %q; the whole tag name must reach the forge", got)
	}
}

// TestGiteaBackend_GetTagAbsenceIsTypedNotAnOutage is the distinction the whole
// kind turns on: "this version has not been released" must arrive as the same
// typed absence every other missing record does, so a caller cannot mistake it
// for an unreachable forge.
func TestGiteaBackend_GetTagAbsenceIsTypedNotAnOutage(t *testing.T) {
	t.Parallel()
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/tags/v9.9.9",
		http.StatusNotFound, map[string]any{"message": "tag does not exist"})

	tag, err := backend.GetTag(context.Background(), testRepo, "v9.9.9")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if errors.Is(err, ErrBackend) {
		t.Fatal("an absent tag is an answer, not a backend failure")
	}
	if tag != nil {
		t.Fatalf("an absent tag must yield no record, got %+v", tag)
	}
}

// TestGiteaBackend_GetTagRefusesAnEmptyName: an empty tag would address the
// repository's whole tag listing, whose 200 would read as a release that exists.
func TestGiteaBackend_GetTagRefusesAnEmptyName(t *testing.T) {
	t.Parallel()
	backend, fixture := newGiteaFixture(t)
	for _, name := range []string{"", "   ", "\t"} {
		if _, err := backend.GetTag(context.Background(), testRepo, name); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("GetTag(%q): want ErrInvalidInput, got %v", name, err)
		}
	}
	if len(fixture.requests) != 0 {
		t.Fatalf("a refused read must make no request: %+v", fixture.requests)
	}
}

// TestGiteaBackend_GetTagServerFailureIsABackendError: a 500 is an outage, and
// must not be read as an unreleased version.
func TestGiteaBackend_GetTagServerFailureIsABackendError(t *testing.T) {
	t.Parallel()
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/tags/v1.0.0",
		http.StatusInternalServerError, map[string]any{"message": "boom"})

	_, err := backend.GetTag(context.Background(), testRepo, "v1.0.0")
	if err == nil {
		t.Fatal("a 500 must not be read as a tag")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("a server failure must not be read as an unreleased version: %v", err)
	}
}

// =============================================================================
// ListOpenPullRequests — which pull request is this commit the head of
// =============================================================================

// TestGiteaBackend_ListOpenPullRequests_PagesUntilAShortPage proves the listing
// is WHOLE. It is what a green pipeline's commit is resolved to a pull request
// through, and a capped first page would answer "no pull request has this head"
// for any repository with more open changes than fit on one page — so the change
// whose CI just passed would silently never be reviewed.
func TestGiteaBackend_ListOpenPullRequests_PagesUntilAShortPage(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	first := make([]map[string]any, giteaDefaultPageSize)
	for i := range first {
		first[i] = map[string]any{
			"number": i + 1,
			"state":  "open",
			"head":   map[string]any{"ref": "issue/" + strconv.Itoa(i+1), "sha": "sha-" + strconv.Itoa(i+1)},
			"base":   map[string]any{"ref": "main"},
		}
	}
	second := []map[string]any{{
		"number": giteaDefaultPageSize + 1,
		"state":  "open",
		"head":   map[string]any{"ref": "issue/last", "sha": "sha-last"},
		"base":   map[string]any{"ref": "main"},
	}}
	pages := [][]map[string]any{first, second}
	served := 0
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls"] = func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state = %q, want open; a listing of everything ever merged grows without bound", got)
		}
		if got := r.URL.Query().Get("page"); got != strconv.Itoa(served+1) {
			t.Errorf("page = %q, want %d", got, served+1)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pages[served])
		served++
	}

	pulls, err := backend.ListOpenPullRequests(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("ListOpenPullRequests: %v", err)
	}
	if served != 2 {
		t.Fatalf("the adapter made %d page requests, want 2", served)
	}
	if len(pulls) != giteaDefaultPageSize+1 {
		t.Fatalf("listed %d pull requests, want %d", len(pulls), giteaDefaultPageSize+1)
	}
	last := pulls[len(pulls)-1]
	if last.Number != giteaDefaultPageSize+1 || last.HeadSHA != "sha-last" || last.BaseRef != "main" {
		t.Fatalf("the last page's pull request did not survive conversion: %+v", last)
	}
}

// TestGiteaBackend_ListOpenPullRequests_Refusals: a repository with nothing open
// is an empty listing and not an error, and a forge that cannot answer is a
// fault rather than an empty listing — the two must never arrive the same way,
// because one means "this commit heads no pull request" and the other means "we
// do not know".
func TestGiteaBackend_ListOpenPullRequests_Refusals(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls", http.StatusOK, []map[string]any{})
	pulls, err := backend.ListOpenPullRequests(context.Background(), testRepo)
	if err != nil || len(pulls) != 0 {
		t.Fatalf("an empty repository: pulls=%v err=%v", pulls, err)
	}

	broken, brokenFixture := newGiteaFixture(t)
	brokenFixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/pulls",
		http.StatusInternalServerError, map[string]any{"message": "boom"})
	if _, err := broken.ListOpenPullRequests(context.Background(), testRepo); err == nil {
		t.Fatal("a forge that could not answer was read as naming no pull request")
	}

	// An unroutable path is the adapter's own transport failure, and it must
	// not be reported as an empty listing either.
	missing, _ := newGiteaFixture(t)
	if _, err := missing.ListOpenPullRequests(context.Background(), RepoRef{Owner: "nobody", Repo: "nothing"}); err == nil {
		t.Fatal("a 404 was read as naming no pull request")
	}
}
