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
