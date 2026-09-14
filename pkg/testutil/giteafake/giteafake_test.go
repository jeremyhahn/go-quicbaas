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

package giteafake

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// The fixture's siblings under pkg/testutil carry no tests: they are helpers
// whose failures surface in the suites that use them. These two are the
// exception, because they are the behaviours whose being WRONG would make a
// caller's test pass vacuously rather than fail.

// TestListContents_AnswersAnArrayForADirectoryAndAnObjectForAFile.
//
// Gitea's contents route does this, and an adapter has to decode both. A
// fixture that always answered an array would let an adapter that handles
// only arrays pass every test and then fail on the first real file.
func TestListContents_AnswersAnArrayForADirectoryAndAnObjectForAFile(t *testing.T) {
	server := New(t, "acme", "api")
	server.PutDir(".github/workflows", "ci.yml", "automerge.yml")
	server.PutFile("Makefile")

	dir := get(t, server, "/api/v1/repos/acme/api/contents/.github/workflows")
	var entries []Content
	if err := json.Unmarshal(dir, &entries); err != nil {
		t.Fatalf("a directory did not answer with an array: %v (%s)", err, dir)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want two", entries)
	}

	file := get(t, server, "/api/v1/repos/acme/api/contents/Makefile")
	var single Content
	if err := json.Unmarshal(file, &single); err != nil {
		t.Fatalf("a file did not answer with an object: %v (%s)", err, file)
	}
	if single.Path != "Makefile" || single.Type != "file" {
		t.Fatalf("file = %+v", single)
	}

	// An absent path is a 404, which is how "the directory is not there" is
	// told from "the forge could not be read".
	resp, err := http.Get(server.URL() + "/api/v1/repos/acme/api/contents/nope")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("an absent path answered %d, want 404", resp.StatusCode)
	}
}

// TestExpandEvents_MirrorsGiteasFamilies.
//
// Gitea EXPANDS a coarse event into its family on write. A fixture that
// stored what it was given would let a read-back comparing sets for equality
// pass here and fail against a real instance — exactly the case the adapter's
// subset comparison exists for.
func TestExpandEvents_MirrorsGiteasFamilies(t *testing.T) {
	expanded := ExpandEvents([]string{"issues"})
	if len(expanded) < 2 {
		t.Fatalf("issues expanded to %v; Gitea returns its whole family", expanded)
	}

	// An event with no family is its own, and the result is sorted and
	// de-duplicated so two reads compare equal.
	if got := ExpandEvents([]string{"status", "status"}); !reflect.DeepEqual(got, []string{"status"}) {
		t.Errorf("ExpandEvents = %v, want [status] de-duplicated", got)
	}
	if got := ExpandEvents(nil); len(got) != 0 {
		t.Errorf("ExpandEvents(nil) = %v, want empty", got)
	}
}

// TestCreateLabel_RefusesADuplicateTheWayGiteaDoes keeps a caller that
// creates unconditionally from looking correct here.
func TestCreateLabel_RefusesADuplicateTheWayGiteaDoes(t *testing.T) {
	server := New(t, "acme", "api")
	server.PutLabel("factory:blocked", "d93f0b", "")

	body := `{"name":"Factory:Blocked","color":"#ffffff"}`
	resp, err := http.Post(server.URL()+"/api/v1/repos/acme/api/labels", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("a duplicate label answered %d, want 409", resp.StatusCode)
	}
	if names := server.LabelNames(); len(names) != 1 {
		t.Errorf("labels = %v, want the original untouched", names)
	}
}

// TestListPulls_PaginatesAndFiltersByState.
//
// Both properties decide whether a caller's test proves anything: a fixture
// that ignored `state` would let a sweep that reviews CLOSED pull requests
// pass, and one that ignored `limit`/`page` would let an adapter that reads
// only the first page pass while silently missing every later change.
func TestListPulls_PaginatesAndFiltersByState(t *testing.T) {
	server := New(t, "acme", "api")
	server.PutPullRequest(1, "issue/1", "aaaa")
	server.PutPullRequest(2, "issue/2", "bbbb")
	server.PutPullRequestRaw(PullRequest{
		Number: 3, State: "closed",
		Head: Commit{Ref: "issue/3", SHA: "cccc"}, Base: Commit{Ref: "main"},
	})

	var open []PullRequest
	if err := json.Unmarshal(get(t, server, "/api/v1/repos/acme/api/pulls?state=open&limit=50&page=1"), &open); err != nil {
		t.Fatalf("open listing: %v", err)
	}
	if len(open) != 2 || open[0].Number != 1 || open[1].Number != 2 {
		t.Fatalf("open = %+v, want the two open pull requests", open)
	}

	var firstPage []PullRequest
	if err := json.Unmarshal(get(t, server, "/api/v1/repos/acme/api/pulls?state=open&limit=1&page=1"), &firstPage); err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(firstPage) != 1 || firstPage[0].Number != 1 {
		t.Fatalf("first page = %+v", firstPage)
	}
	var secondPage []PullRequest
	if err := json.Unmarshal(get(t, server, "/api/v1/repos/acme/api/pulls?state=open&limit=1&page=2"), &secondPage); err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].Number != 2 {
		t.Fatalf("second page = %+v", secondPage)
	}
	var beyond []PullRequest
	if err := json.Unmarshal(get(t, server, "/api/v1/repos/acme/api/pulls?state=open&limit=1&page=9"), &beyond); err != nil {
		t.Fatalf("page past the end: %v", err)
	}
	if len(beyond) != 0 {
		t.Fatalf("a page past the end answered %+v, want empty", beyond)
	}

	// Seeding the same number again REPLACES it, so a test can move a head
	// without accumulating two pull requests under one number.
	server.PutPullRequest(1, "issue/1", "aaaa2")
	var replaced []PullRequest
	if err := json.Unmarshal(get(t, server, "/api/v1/repos/acme/api/pulls?state=open"), &replaced); err != nil {
		t.Fatalf("after replacement: %v", err)
	}
	if len(replaced) != 2 || replaced[0].Head.SHA != "aaaa2" {
		t.Fatalf("replaced = %+v", replaced)
	}
}

// TestGetPull_AnswersOneOr404 proves the single-pull-request route a
// dispatcher's re-read depends on, and its refusal.
func TestGetPull_AnswersOneOr404(t *testing.T) {
	server := New(t, "acme", "api")
	server.PutPullRequest(41, "issue/41", "aaaa")

	var pull PullRequest
	if err := json.Unmarshal(get(t, server, "/api/v1/repos/acme/api/pulls/41"), &pull); err != nil {
		t.Fatalf("GET one: %v", err)
	}
	if pull.Number != 41 || pull.Head.SHA != "aaaa" {
		t.Fatalf("pull = %+v", pull)
	}
	for path, want := range map[string]int{
		"/api/v1/repos/acme/api/pulls/42":    http.StatusNotFound,
		"/api/v1/repos/acme/api/pulls/forty": http.StatusBadRequest,
	} {
		resp, err := http.Get(server.URL() + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("GET %s answered %d, want %d", path, resp.StatusCode, want)
		}
	}
}

// TestCommitStatuses_AppendRatherThanReplace.
//
// Gitea keeps a context's history and the newest entry is operative. A
// fixture that replaced would hide whether a caller takes the newest per
// context — and the review floor's whole "has this head been judged" question
// is read from this listing.
func TestCommitStatuses_AppendRatherThanReplace(t *testing.T) {
	server := New(t, "acme", "api")
	server.PutStatus("aaaa", "CI / CI Gate (pull_request)", "pending")
	server.PutStatus("aaaa", "CI / CI Gate (pull_request)", "success")

	var statuses []CommitStatus
	if err := json.Unmarshal(get(t, server, "/api/v1/repos/acme/api/commits/aaaa/statuses"), &statuses); err != nil {
		t.Fatalf("statuses: %v", err)
	}
	if len(statuses) != 2 || statuses[1].State != "success" {
		t.Fatalf("statuses = %+v, want the history with success last", statuses)
	}
	if got := server.StatusContexts("aaaa"); len(got) != 2 {
		t.Fatalf("StatusContexts = %v", got)
	}
	if got := server.StatusContexts("bbbb"); len(got) != 0 {
		t.Fatalf("a commit with no status answered %v", got)
	}

	// A posted status joins the same history, and one naming no context is
	// refused rather than silently recorded as an unnamed check.
	post(t, server, "/api/v1/repos/acme/api/statuses/aaaa",
		`{"state":"success","context":"quicbaas/review","description":"approved"}`, http.StatusCreated)
	if got := server.StatusContexts("aaaa"); len(got) != 3 {
		t.Fatalf("StatusContexts = %v, want the review context among them", got)
	}
	post(t, server, "/api/v1/repos/acme/api/statuses/aaaa", `{"state":"success"}`, http.StatusBadRequest)
	post(t, server, "/api/v1/repos/acme/api/statuses/aaaa", `not json`, http.StatusBadRequest)
}

// post sends one JSON body and asserts the status code.
func post(t *testing.T, server *Server, path, body string, want int) {
	t.Helper()
	resp, err := http.Post(server.URL()+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		t.Fatalf("POST %s answered %d, want %d", path, resp.StatusCode, want)
	}
}

func get(t *testing.T, server *Server, path string) []byte {
	t.Helper()
	resp, err := http.Get(server.URL() + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", path, resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return raw
}
