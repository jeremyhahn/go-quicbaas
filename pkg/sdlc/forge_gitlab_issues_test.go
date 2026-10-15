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
	"slices"
	"testing"
	"time"
)

const glIssue = glProject + "/issues/7"

// gitlabIssueAnswer is an issue as GitLab returns it, with the given state
// and labels.
func gitlabIssueAnswer(state string, labels ...string) map[string]any {
	if labels == nil {
		labels = []string{}
	}
	return map[string]any{"iid": 7, "title": "Widgets drift under load", "state": state, "labels": labels}
}

// TestGitLabBackend_ListIssues_ReadsEveryPage proves both fixture pages are
// read, GitLab's states spoken in the module's words, and the filter sent.
func TestGitLabBackend_ListIssues_ReadsEveryPage(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.handle("GET "+glProject+"/issues", gitlabPaged(
		json.RawMessage(stub.load("issues_page1.json")),
		json.RawMessage(stub.load("issues_page2.json")),
	))

	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	issues, err := backend.ListIssues(context.Background(), gitlabRepo, IssueFilter{
		State: "all", Labels: []string{"bug", "factory:queued"}, Since: since,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %+v, want 2", issues)
	}
	first := issues[0]
	if first.Number != 7 || first.State != "open" || first.Milestone != "v1.2" ||
		first.Author != "reporter" || first.Assignee != "fixer" || !slices.Equal(first.Labels, []string{"bug", "factory:queued"}) {
		t.Errorf("first = %+v", first)
	}
	if issues[1].State != "open" {
		t.Errorf("a locked issue is %q, want open", issues[1].State)
	}

	query := gitlabQuery(t, stub.requests("GET " + glProject + "/issues")[0])
	if query.Get("state") != "all" || query.Get("labels") != "bug,factory:queued" || query.Get("updated_after") != "2026-10-01T00:00:00Z" {
		t.Errorf("query = %v", query)
	}
}

// TestGitLabBackend_ListIssues_StopsAtTheLimit proves the caller's bound ends
// the listing without reading further pages.
func TestGitLabBackend_ListIssues_StopsAtTheLimit(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.handle("GET "+glProject+"/issues", gitlabPaged(
		json.RawMessage(stub.load("issues_page1.json")),
		json.RawMessage(stub.load("issues_page2.json")),
	))
	issues, err := backend.ListIssues(context.Background(), gitlabRepo, IssueFilter{Limit: 1})
	if err != nil || len(issues) != 1 {
		t.Fatalf("ListIssues = %+v, %v; want one issue", issues, err)
	}
	if n := len(stub.requests("GET " + glProject + "/issues")); n != 1 {
		t.Errorf("requests = %d, want 1", n)
	}
	query := gitlabQuery(t, stub.requests("GET " + glProject + "/issues")[0])
	if query.Get("state") != "opened" {
		t.Errorf("default state = %q, want opened", query.Get("state"))
	}
}

// TestGitLabBackend_ListIssues_Refusals proves an unknown state is refused
// before any call and an unauthorized listing is a typed error.
func TestGitLabBackend_ListIssues_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	if _, err := backend.ListIssues(context.Background(), gitlabRepo, IssueFilter{State: "merged"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("state merged err = %v, want ErrInvalidInput", err)
	}
	stub.json("GET "+glProject+"/issues", http.StatusUnauthorized, map[string]string{"message": "401 Unauthorized"})
	_, err := backend.ListIssues(context.Background(), gitlabRepo, IssueFilter{})
	if forgeStatusOf(err) != http.StatusUnauthorized {
		t.Errorf("err = %v, want 401", err)
	}
	gitlabErrorFree(t, err)
}

// TestGitLabBackend_GetIssue_ReadsOne proves one issue is read by its iid.
func TestGitLabBackend_GetIssue_ReadsOne(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("GET "+glIssue, http.StatusOK, gitlabIssueAnswer("closed", "bug"))
	issue, err := backend.GetIssue(context.Background(), gitlabRepo, 7)
	if err != nil || issue.Number != 7 || issue.State != "closed" {
		t.Fatalf("GetIssue = %+v, %v", issue, err)
	}
}

// TestGitLabBackend_CreateIssue_UsesTheProjectsLabelSpelling proves the
// labels are resolved against the project's vocabulary and sent in its
// spelling.
func TestGitLabBackend_CreateIssue_UsesTheProjectsLabelSpelling(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")
	stub.json("POST "+glProject+"/issues", http.StatusCreated, map[string]any{
		"iid": 9, "title": "New work", "state": "opened", "labels": []string{"Factory:Queued"},
	})

	issue, err := backend.CreateIssue(context.Background(), gitlabRepo, IssueSpec{
		Title: "New work", Body: "Details.", Labels: []string{"factory:queued"},
	})
	if err != nil || issue.Number != 9 {
		t.Fatalf("CreateIssue = %+v, %v", issue, err)
	}
	body := decodeBody(t, stub.requests("POST " + glProject + "/issues")[0])
	if body["labels"] != "Factory:Queued" || body["description"] != "Details." {
		t.Errorf("body = %v", body)
	}
}

// TestGitLabBackend_CreateIssue_Refusals proves a title is required, an
// undefined label is refused before the write, and a write that came back
// without the labels or title asked for is reported.
func TestGitLabBackend_CreateIssue_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")

	if _, err := backend.CreateIssue(context.Background(), gitlabRepo, IssueSpec{Title: " "}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("no title err = %v, want ErrInvalidInput", err)
	}
	if _, err := backend.CreateIssue(context.Background(), gitlabRepo, IssueSpec{Title: "x", Labels: []string{"nope"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("undefined label err = %v, want ErrInvalidInput", err)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Fatalf("writes = %v, want none before a refusal", writes)
	}

	stub.json("POST "+glProject+"/issues", http.StatusCreated, map[string]any{"iid": 9, "title": "x", "labels": []string{}})
	if _, err := backend.CreateIssue(context.Background(), gitlabRepo, IssueSpec{Title: "x", Labels: []string{"bug"}}); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("dropped label err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("POST "+glProject+"/issues", http.StatusCreated, map[string]any{"iid": 0})
	if _, err := backend.CreateIssue(context.Background(), gitlabRepo, IssueSpec{Title: "x"}); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("no iid err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("GET "+glProject+"/labels", http.StatusInternalServerError, nil)
	if _, err := backend.CreateIssue(context.Background(), gitlabRepo, IssueSpec{Title: "x", Labels: []string{"bug"}}); !errors.Is(err, ErrBackend) {
		t.Errorf("label read err = %v, want ErrBackend", err)
	}
	stub.json("POST "+glProject+"/issues", http.StatusForbidden, nil)
	if _, err := backend.CreateIssue(context.Background(), gitlabRepo, IssueSpec{Title: "x"}); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("forbidden err = %v, want 403", err)
	}
}

// TestGitLabBackend_CommentOnIssue_PostsANote proves a comment is a note and
// the stored note is returned.
func TestGitLabBackend_CommentOnIssue_PostsANote(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("POST "+glIssue+"/notes", http.StatusCreated, map[string]any{
		"id": 601, "body": "On it.", "author": map[string]any{"username": "agent-widgets"},
	})
	comment, err := backend.CommentOnIssue(context.Background(), gitlabRepo, 7, "On it.")
	if err != nil || comment.ID != 601 || comment.Author != "agent-widgets" {
		t.Fatalf("CommentOnIssue = %+v, %v", comment, err)
	}
}

// TestGitLabBackend_CommentOnIssue_Refusals proves an empty comment is
// refused and a note stored differently is reported.
func TestGitLabBackend_CommentOnIssue_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	if _, err := backend.CommentOnIssue(context.Background(), gitlabRepo, 7, "  "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty err = %v, want ErrInvalidInput", err)
	}
	if _, err := backend.CommentOnIssue(context.Background(), gitlabRepo, 7, "On it."); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing issue err = %v, want ErrNotFound", err)
	}
	stub.json("POST "+glIssue+"/notes", http.StatusCreated, map[string]any{"id": 601, "body": "truncated"})
	if _, err := backend.CommentOnIssue(context.Background(), gitlabRepo, 7, "On it."); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("altered err = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestGitLabBackend_CloseIssue_SendsTheStateEvent proves a close is a
// state_event the response confirms.
func TestGitLabBackend_CloseIssue_SendsTheStateEvent(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.json("PUT "+glIssue, http.StatusOK, gitlabIssueAnswer("closed"))
	if err := backend.CloseIssue(context.Background(), gitlabRepo, 7); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if body := decodeBody(t, stub.requests("PUT " + glIssue)[0]); body["state_event"] != "close" {
		t.Errorf("body = %v", body)
	}
}

// TestGitLabBackend_CloseIssue_Refusals proves an issue still open after the
// write, or a missing one, is reported.
func TestGitLabBackend_CloseIssue_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	if err := backend.CloseIssue(context.Background(), gitlabRepo, 7); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v, want ErrNotFound", err)
	}
	stub.json("PUT "+glIssue, http.StatusOK, gitlabIssueAnswer("opened"))
	if err := backend.CloseIssue(context.Background(), gitlabRepo, 7); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("still open err = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestGitLabBackend_ListIssueComments_DropsSystemNotes proves GitLab's own
// narration is not mistaken for discussion.
func TestGitLabBackend_ListIssueComments_DropsSystemNotes(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glIssue+"/notes", http.StatusOK, "notes.json")
	comments, err := backend.ListIssueComments(context.Background(), gitlabRepo, 7)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(comments) != 2 || comments[0].ID != 501 || comments[1].ID != 503 || comments[0].Author != "fixer" {
		t.Fatalf("comments = %+v, want the two human notes", comments)
	}
	query := gitlabQuery(t, stub.requests("GET " + glIssue + "/notes")[0])
	if query.Get("sort") != "asc" || query.Get("order_by") != "created_at" {
		t.Errorf("query = %v, want oldest first", query)
	}
}

// TestGitLabBackend_ListIssueComments_MissingIssue proves a missing issue is
// ErrNotFound.
func TestGitLabBackend_ListIssueComments_MissingIssue(t *testing.T) {
	backend, _ := newGitLabStub(t)
	if _, err := backend.ListIssueComments(context.Background(), gitlabRepo, 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestGitLabBackend_IssueLabelEdits_SendTheirOwnField proves each label verb
// sends its GitLab field, in the project's spelling, and verifies by reading
// the issue back.
func TestGitLabBackend_IssueLabelEdits_SendTheirOwnField(t *testing.T) {
	cases := map[string]struct {
		edit  func(*GitLabBackend) error
		field string
		after []string
	}{
		"set": {
			edit: func(b *GitLabBackend) error {
				return b.SetIssueLabels(context.Background(), gitlabRepo, 7, []string{"factory:queued"})
			},
			field: "labels", after: []string{"Factory:Queued"},
		},
		"add": {
			edit: func(b *GitLabBackend) error {
				return b.AddIssueLabels(context.Background(), gitlabRepo, 7, []string{"bug"})
			},
			field: "add_labels", after: []string{"bug", "Factory:Queued"},
		},
		"remove": {
			edit:  func(b *GitLabBackend) error { return b.RemoveIssueLabel(context.Background(), gitlabRepo, 7, "BUG") },
			field: "remove_labels", after: []string{"Factory:Queued"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")
			stub.json("PUT "+glIssue, http.StatusOK, gitlabIssueAnswer("opened", tc.after...))
			stub.json("GET "+glIssue, http.StatusOK, gitlabIssueAnswer("opened", tc.after...))

			if err := tc.edit(backend); err != nil {
				t.Fatalf("edit: %v", err)
			}
			body := decodeBody(t, stub.requests("PUT " + glIssue)[0])
			if len(body) != 1 || body[tc.field] == nil {
				t.Errorf("body = %v, want only %s", body, tc.field)
			}
		})
	}
}

// TestGitLabBackend_IssueLabelEdits_Refusals proves a label edit that did not
// take is reported, an empty label is refused, and adding nothing calls
// nothing.
func TestGitLabBackend_IssueLabelEdits_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")
	stub.json("PUT "+glIssue, http.StatusOK, gitlabIssueAnswer("opened", "bug"))
	stub.json("GET "+glIssue, http.StatusOK, gitlabIssueAnswer("opened", "bug"))

	if err := backend.AddIssueLabels(context.Background(), gitlabRepo, 7, nil); err != nil {
		t.Errorf("adding nothing = %v", err)
	}
	if writes := stub.writes(); len(writes) != 0 {
		t.Errorf("writes = %v, want none for an empty add", writes)
	}
	if err := backend.RemoveIssueLabel(context.Background(), gitlabRepo, 7, ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty label err = %v, want ErrInvalidInput", err)
	}
	if err := backend.SetIssueLabels(context.Background(), gitlabRepo, 7, []string{"factory:queued"}); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("set err = %v, want ErrForgeWriteNotApplied", err)
	}
	if err := backend.AddIssueLabels(context.Background(), gitlabRepo, 7, []string{"factory:queued"}); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("add err = %v, want ErrForgeWriteNotApplied", err)
	}
	if err := backend.RemoveIssueLabel(context.Background(), gitlabRepo, 7, "bug"); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("remove err = %v, want ErrForgeWriteNotApplied", err)
	}
	if err := backend.SetIssueLabels(context.Background(), gitlabRepo, 7, []string{"nope"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("undefined err = %v, want ErrInvalidInput", err)
	}
	if err := backend.AddIssueLabels(context.Background(), gitlabRepo, 7, []string{"nope"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("undefined add err = %v, want ErrInvalidInput", err)
	}
	if err := backend.RemoveIssueLabel(context.Background(), gitlabRepo, 7, "nope"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("undefined remove err = %v, want ErrInvalidInput", err)
	}
}

// TestGitLabBackend_IssueLabelEdits_PropagateForgeFailures proves a refused
// write or a failed read-back reaches the caller from each verb.
func TestGitLabBackend_IssueLabelEdits_PropagateForgeFailures(t *testing.T) {
	edits := map[string]func(*GitLabBackend) error{
		"set": func(b *GitLabBackend) error {
			return b.SetIssueLabels(context.Background(), gitlabRepo, 7, []string{"bug"})
		},
		"add": func(b *GitLabBackend) error {
			return b.AddIssueLabels(context.Background(), gitlabRepo, 7, []string{"bug"})
		},
		"remove": func(b *GitLabBackend) error { return b.RemoveIssueLabel(context.Background(), gitlabRepo, 7, "bug") },
	}
	for name, edit := range edits {
		t.Run(name+" write", func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")
			stub.json("PUT "+glIssue, http.StatusForbidden, nil)
			if err := edit(backend); forgeStatusOf(err) != http.StatusForbidden {
				t.Errorf("err = %v, want 403", err)
			}
		})
		t.Run(name+" read-back", func(t *testing.T) {
			backend, stub := newGitLabStub(t)
			stub.fixture("GET "+glProject+"/labels", http.StatusOK, "labels.json")
			stub.json("PUT "+glIssue, http.StatusOK, gitlabIssueAnswer("opened"))
			if err := edit(backend); !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want the read-back's ErrNotFound", err)
			}
		})
	}
}

// TestGitLabBackend_ListCollaborators_ReadsInheritedMembers proves members
// come from /members/all.
func TestGitLabBackend_ListCollaborators_ReadsInheritedMembers(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.fixture("GET "+glProject+"/members/all", http.StatusOK, "members.json")
	users, err := backend.ListCollaborators(context.Background(), gitlabRepo)
	if err != nil {
		t.Fatalf("ListCollaborators: %v", err)
	}
	if len(users) != 2 || users[1] != (ForgeUser{Login: "fixer", FullName: "Fix Er"}) {
		t.Fatalf("users = %+v", users)
	}
}

// TestGitLabBackend_ListCollaborators_Refusals proves a missing project and
// an endless listing are both reported.
func TestGitLabBackend_ListCollaborators_Refusals(t *testing.T) {
	backend, stub := newGitLabStub(t)
	if _, err := backend.ListCollaborators(context.Background(), gitlabRepo); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing err = %v, want ErrNotFound", err)
	}
	stub.handle("GET "+glProject+"/members/all", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(gitlabNextPageHeader, "2")
		forgeRaw(http.StatusOK, stub.load("members.json"))(w, r)
	})
	if _, err := backend.ListCollaborators(context.Background(), gitlabRepo); !errors.Is(err, ErrIncompleteResponse) {
		t.Errorf("endless err = %v, want ErrIncompleteResponse", err)
	}
}

// TestGitLabBackend_Labels_ReportsAnEndlessListing proves the label ceiling
// is reported rather than truncating the vocabulary.
func TestGitLabBackend_Labels_ReportsAnEndlessListing(t *testing.T) {
	backend, stub := newGitLabStub(t)
	stub.handle("GET "+glProject+"/labels", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(gitlabNextPageHeader, "2")
		forgeRaw(http.StatusOK, stub.load("labels.json"))(w, r)
	})
	if _, err := backend.Labels(context.Background(), gitlabRepo); !errors.Is(err, ErrIncompleteResponse) {
		t.Fatalf("err = %v, want ErrIncompleteResponse", err)
	}
}
