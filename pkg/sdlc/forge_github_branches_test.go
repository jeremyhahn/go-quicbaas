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
	"slices"
	"strings"
	"testing"
)

func githubBranchJSON(name, sha string) map[string]any {
	return map[string]any{"name": name, "commit": map[string]string{"sha": sha}}
}

func TestGitHubBackend_ListBranches_FollowsPages(t *testing.T) {
	backend, stub := newGitHubStub(t)
	page2 := stub.api() + "/repos/acme/widgets/branches?page=2&per_page=100"
	first := githubPaged([]any{githubBranchJSON("main", "m1"), githubBranchJSON("issue/1", "a1")}, page2)
	second := forgeJSON(http.StatusOK, []any{githubBranchJSON("issue/2", "b2")})
	stub.handle("GET /repos/acme/widgets/branches", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			second(w, r)
			return
		}
		first(w, r)
	})

	branches, err := backend.ListBranches(context.Background(), githubRepo)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	want := []ForgeBranch{{Name: "main", SHA: "m1"}, {Name: "issue/1", SHA: "a1"}, {Name: "issue/2", SHA: "b2"}}
	if !slices.Equal(branches, want) {
		t.Errorf("branches = %+v, want %+v", branches, want)
	}
	calls := stub.requests("GET /repos/acme/widgets/branches")
	if len(calls) != 2 || !strings.Contains(calls[1].Query, "page=2") {
		t.Errorf("calls = %+v, want page 1 then page 2", calls)
	}

	stub.json("GET /repos/acme/widgets/branches", http.StatusForbidden, nil)
	if got, err := backend.ListBranches(context.Background(), githubRepo); forgeStatusOf(err) != http.StatusForbidden || got != nil {
		t.Errorf("ListBranches = %v, %v, want the 403 and no branches", got, err)
	}
}

func TestGitHubBackend_BranchContained_Statuses(t *testing.T) {
	const sha = "0123abcd"
	key := "GET /repos/acme/widgets/compare/main..." + sha
	cases := []struct {
		status string
		want   bool
		fails  bool
	}{
		{status: "identical", want: true},
		{status: "behind", want: true},
		{status: "ahead", want: false},
		{status: "diverged", want: false},
		{status: "", fails: true},
		{status: "sideways", fails: true},
	}
	for _, tc := range cases {
		t.Run("status="+tc.status, func(t *testing.T) {
			backend, stub := newGitHubStub(t)
			stub.json(key, http.StatusOK, map[string]string{"status": tc.status})

			got, err := backend.BranchContained(context.Background(), githubRepo, "main", sha)
			if tc.fails {
				if !errors.Is(err, ErrIncompleteResponse) || got {
					t.Fatalf("BranchContained = %v, %v, want an incomplete response", got, err)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("BranchContained = %v, %v, want %v", got, err, tc.want)
			}
			if n := len(stub.requests(key)); n != 1 {
				t.Errorf("requests to %s = %d, want 1", key, n)
			}
		})
	}
}

func TestGitHubBackend_BranchContained_RefusesEmptyInput(t *testing.T) {
	backend, stub := newGitHubStub(t)
	for name, args := range map[string][2]string{
		"no base": {"", "0123abcd"},
		"no head": {"main", ""},
	} {
		if _, err := backend.BranchContained(context.Background(), githubRepo, args[0], args[1]); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
	if calls := stub.writes(); len(calls) != 0 {
		t.Errorf("writes = %+v, want none", calls)
	}
}

// deleteAtGraphQLKey is the stub key of a POST to the stub's GraphQL
// endpoint, which lives beside the REST root rather than under it.
const deleteAtGraphQLKey = "POST /api/graphql"

func stubDeleteAt(stub *forgeStub, graphQL any, readBack http.HandlerFunc) {
	stub.json("GET /repos/acme/widgets", http.StatusOK, map[string]string{"node_id": "R_1"})
	stub.json(deleteAtGraphQLKey, http.StatusOK, graphQL)
	stub.handle("GET /repos/acme/widgets/branches/issue/4", readBack)
}

func TestGitHubBackend_DeleteBranchAt_DeletesAtTheExpectedTip(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stubDeleteAt(stub, map[string]any{"data": map[string]any{"updateRefs": map[string]any{"clientMutationId": nil}}},
		forgeJSON(http.StatusNotFound, map[string]string{"message": "Branch not found"}))

	if err := backend.DeleteBranchAt(context.Background(), sweepRepo, "issue/4", githubHeadSHA); err != nil {
		t.Fatalf("DeleteBranchAt: %v", err)
	}
	posts := stub.requests(deleteAtGraphQLKey)
	if len(posts) != 1 {
		t.Fatalf("GraphQL posts = %d, want 1", len(posts))
	}
	for _, want := range []string{
		`"beforeOid":"` + githubHeadSHA + `"`,
		`"afterOid":"` + githubZeroOID + `"`,
		`"force":false`,
		`"repositoryId":"R_1"`,
		`"name":"refs/heads/issue/4"`,
		`updateRefs(input: $input)`,
	} {
		if !strings.Contains(posts[0].Body, want) {
			t.Errorf("body %s lacks %s", posts[0].Body, want)
		}
	}
	if got := posts[0].Header.Get("GraphQL-Features"); got != "update_refs_preview" {
		t.Errorf("GraphQL-Features = %q, want update_refs_preview", got)
	}
	if writes := stub.writes(); len(writes) != 1 || writes[0].Method != http.MethodPost {
		t.Errorf("writes = %+v, want only the GraphQL POST and no DELETE", writes)
	}
}

func TestGitHubBackend_DeleteBranchAt_MovedBranchIsKept(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stubDeleteAt(stub, map[string]any{"errors": []map[string]string{{"message": "ref was not at the expected oid"}}},
		forgeJSON(http.StatusOK, githubBranchJSON("issue/4", githubMovedSHA)))

	err := backend.DeleteBranchAt(context.Background(), sweepRepo, "issue/4", githubHeadSHA)
	if !errors.Is(err, ErrBranchMoved) {
		t.Fatalf("err = %v, want ErrBranchMoved", err)
	}
	if n := len(stub.writes()); n != 1 {
		t.Errorf("writes = %d, want only the GraphQL POST", n)
	}
}

func TestGitHubBackend_DeleteBranchAt_RefusedAtSameTipIsAnError(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stubDeleteAt(stub, map[string]any{"errors": []map[string]string{{"message": "refusing to update the ref"}}},
		forgeJSON(http.StatusOK, githubBranchJSON("issue/4", githubHeadSHA)))

	err := backend.DeleteBranchAt(context.Background(), sweepRepo, "issue/4", githubHeadSHA)
	if err == nil || errors.Is(err, ErrBranchMoved) || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want a failure that is neither moved nor not found", err)
	}
	if !strings.Contains(err.Error(), "refusing to update the ref") {
		t.Errorf("err = %v, want the GraphQL message", err)
	}
	if strings.Contains(err.Error(), githubTestToken) {
		t.Errorf("err carries the token: %v", err)
	}
}

func TestGitHubBackend_DeleteBranchAt_RefusesEmptyInput(t *testing.T) {
	backend, stub := newGitHubStub(t)
	for _, tc := range []struct{ name, sha string }{{"", githubHeadSHA}, {"issue/4", ""}} {
		err := backend.DeleteBranchAt(context.Background(), sweepRepo, tc.name, tc.sha)
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("DeleteBranchAt(%q, %q) = %v, want ErrInvalidInput", tc.name, tc.sha, err)
		}
	}
	if calls := stub.requests("GET /repos/acme/widgets"); len(calls) != 0 || len(stub.writes()) != 0 {
		t.Errorf("requests were sent for an empty input")
	}
}

func TestGitHubGraphQLURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://api.github.com":          "https://api.github.com/graphql",
		"https://ghes.example.com/api/v3": "https://ghes.example.com/api/graphql",
		"https://proxy.example.com/gh":    "https://proxy.example.com/gh/graphql",
	} {
		if got := githubGraphQLURL(base); got != want {
			t.Errorf("githubGraphQLURL(%q) = %q, want %q", base, got, want)
		}
	}
}
