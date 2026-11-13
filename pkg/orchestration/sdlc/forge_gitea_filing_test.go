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
	"testing"
)

const giteaIssuesPath = "/api/v1/repos/jeremyhahn/go-quicbaas/issues"

func TestGiteaCreateIssue_FilesWithResolvedLabelIDs(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{
		{"id": 4, "name": "task:ready"},
	})
	fixture.respondJSON("POST "+giteaIssuesPath, http.StatusCreated, map[string]any{
		"number": 88, "title": "Follow-up", "body": "b\n\nParent: #12", "state": "open",
		"labels": []map[string]any{{"id": 4, "name": "task:ready"}},
		"user":   map[string]any{"login": "agent-quicbaas"},
	})

	issue, err := backend.CreateIssue(context.Background(), testRepo, IssueSpec{
		Title: "Follow-up", Body: "b\n\nParent: #12", Labels: []string{"task:ready"},
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if issue.Number != 88 || issue.Author != "agent-quicbaas" {
		t.Fatalf("issue = %+v, want #88 by the project's account", issue)
	}
	var sent giteaCreateIssueBody
	for _, req := range fixture.requests {
		if req.Method == http.MethodPost {
			if err := json.Unmarshal([]byte(req.Body), &sent); err != nil {
				t.Fatalf("decode body: %v", err)
			}
		}
	}
	if sent.Title != "Follow-up" || len(sent.Labels) != 1 || sent.Labels[0] != 4 {
		t.Fatalf("sent = %+v, want the title and label id 4", sent)
	}
}

func TestGiteaCreateIssue_RefusesAnUndefinedLabelBeforeWriting(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{})

	_, err := backend.CreateIssue(context.Background(), testRepo, IssueSpec{Title: "t", Labels: []string{"nope"}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateIssue = %v, want ErrInvalidInput", err)
	}
	for _, req := range fixture.requests {
		if req.Method == http.MethodPost {
			t.Fatal("an issue was filed with a label the repository does not define")
		}
	}
}

func TestGiteaCreateIssue_RefusesAnEmptyTitle(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	if _, err := backend.CreateIssue(context.Background(), testRepo, IssueSpec{Title: " "}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateIssue = %v, want ErrInvalidInput", err)
	}
	if len(fixture.requests) != 0 {
		t.Fatalf("requests = %+v, want none", fixture.requests)
	}
}

func TestGiteaCreateIssue_ReadBackCatchesAWriteThatDidNotLand(t *testing.T) {
	cases := map[string]map[string]any{
		"no number":       {"number": 0, "title": "t"},
		"another title":   {"number": 5, "title": "other"},
		"a dropped label": {"number": 5, "title": "t", "labels": []map[string]any{}},
	}
	for name, echo := range cases {
		t.Run(name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{
				{"id": 4, "name": "task:ready"},
			})
			fixture.respondJSON("POST "+giteaIssuesPath, http.StatusCreated, echo)
			_, err := backend.CreateIssue(context.Background(), testRepo, IssueSpec{Title: "t", Labels: []string{"task:ready"}})
			if !errors.Is(err, ErrForgeWriteNotApplied) {
				t.Fatalf("CreateIssue = %v, want ErrForgeWriteNotApplied", err)
			}
		})
	}
}

func TestGiteaCreateIssue_IsNeverRepeated(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST "+giteaIssuesPath, http.StatusBadGateway, nil)

	if _, err := backend.CreateIssue(context.Background(), testRepo, IssueSpec{Title: "t"}); err == nil {
		t.Fatal("CreateIssue succeeded against a gateway failure")
	}
	posts := 0
	for _, req := range fixture.requests {
		if req.Method == http.MethodPost {
			posts++
		}
	}
	if posts != 1 {
		t.Fatalf("posted %d times, want exactly once: a second create is a second issue", posts)
	}
}

func collaboratorPage(first, count int) []map[string]any {
	page := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		page = append(page, map[string]any{"login": "user" + strconv.Itoa(first+i), "full_name": "User"})
	}
	return page
}

func TestGiteaListCollaborators_ReadsEveryPage(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondPages("/api/v1/repos/jeremyhahn/go-quicbaas/collaborators", [][]map[string]any{
		collaboratorPage(0, giteaDefaultPageSize), collaboratorPage(giteaDefaultPageSize, 2),
	})
	users, err := backend.ListCollaborators(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("ListCollaborators: %v", err)
	}
	if len(users) != giteaDefaultPageSize+2 || users[0].Login != "user0" || users[0].FullName != "User" {
		t.Fatalf("users = %d (%+v...), want every page", len(users), users[:1])
	}
}

func TestGiteaListCollaborators_ReportsAPrefixAsIncomplete(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	pages := make([][]map[string]any, maxCollaboratorPages+1)
	for i := range pages {
		pages[i] = collaboratorPage(i*giteaDefaultPageSize, giteaDefaultPageSize)
	}
	fixture.respondPages("/api/v1/repos/jeremyhahn/go-quicbaas/collaborators", pages)
	var incomplete *IncompleteResponseError
	if _, err := backend.ListCollaborators(context.Background(), testRepo); !errors.As(err, &incomplete) {
		t.Fatalf("ListCollaborators = %v, want IncompleteResponseError", err)
	}
}

func TestGiteaListCollaborators_SurfacesAForbiddenRepository(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/collaborators", http.StatusForbidden, map[string]any{"message": "forbidden"})
	if _, err := backend.ListCollaborators(context.Background(), testRepo); err == nil {
		t.Fatal("ListCollaborators succeeded against a 403")
	}
}
