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
	"testing"
	"time"
)

// githubIssueAt is issue n as GitHub answers it, carrying labels.
func githubIssueAt(number int, title, state string, labels ...string) map[string]any {
	named := make([]map[string]string, 0, len(labels))
	for _, label := range labels {
		named = append(named, map[string]string{"name": label})
	}
	return map[string]any{"number": number, "title": title, "state": state, "labels": named}
}

// githubLabelsDefined answers the label listing with the given names.
func githubLabelsDefined(stub *forgeStub, names ...string) {
	labels := make([]map[string]string, 0, len(names))
	for _, name := range names {
		labels = append(labels, map[string]string{"name": name, "color": "0e8a16"})
	}
	stub.json("GET /repos/acme/widgets/labels", http.StatusOK, labels)
}

// =============================================================================
// ListIssues / GetIssue
// =============================================================================

// TestGitHubBackend_ListIssues_MapsTheFilterAndTheIssue proves the filter
// reaches GitHub's query and an issue reaches the module whole.
func TestGitHubBackend_ListIssues_MapsTheFilterAndTheIssue(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.fixture("GET /repos/acme/widgets/issues", http.StatusOK, "issues_page1.json")
	since := time.Date(2026, 10, 1, 8, 0, 0, 0, time.FixedZone("x", 3600))

	issues, err := backend.ListIssues(context.Background(), githubRepo, IssueFilter{
		State: "all", Labels: []string{"task:ready", "bug"}, Since: since, Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(issues))
	}
	got := issues[0]
	if got.Number != 41 || got.Milestone != "v1.2" || got.Author != "reporter" ||
		got.Assignee != "agent-widgets" || !slices.Equal(got.Labels, []string{"task:ready"}) {
		t.Errorf("issue = %+v", got)
	}

	query, err := url.ParseQuery(stub.requests("GET /repos/acme/widgets/issues")[0].Query)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if query.Get("state") != "all" || query.Get("labels") != "task:ready,bug" ||
		query.Get("since") != "2026-10-01T07:00:00Z" {
		t.Errorf("query = %v", query)
	}
}

// TestGitHubBackend_ListIssues_PropagatesARefusal proves a refused listing is
// an error, not an empty queue.
func TestGitHubBackend_ListIssues_PropagatesARefusal(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/issues", http.StatusForbidden, map[string]string{"message": "forbidden"})

	issues, err := backend.ListIssues(context.Background(), githubRepo, IssueFilter{})
	if issues != nil || forgeStatusOf(err) != http.StatusForbidden {
		t.Fatalf("ListIssues = %v, %v; want the 403", issues, err)
	}
}

// TestGitHubBackend_GetIssue reads one issue.
func TestGitHubBackend_GetIssue(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/issues/7", http.StatusOK, githubIssueAt(7, "Seven", "open", "bug"))

	issue, err := backend.GetIssue(context.Background(), githubRepo, 7)
	if err != nil || issue.Number != 7 || issue.Title != "Seven" {
		t.Fatalf("GetIssue = %+v, %v", issue, err)
	}
}

// =============================================================================
// CreateIssue
// =============================================================================

// TestGitHubBackend_CreateIssue_UsesTheRepositorySpelling proves labels are
// checked against the repository first and sent in its spelling.
func TestGitHubBackend_CreateIssue_UsesTheRepositorySpelling(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubLabelsDefined(stub, "Task:Ready")
	stub.json("POST /repos/acme/widgets/issues", http.StatusCreated,
		githubIssueAt(50, "New work", "open", "Task:Ready"))

	issue, err := backend.CreateIssue(context.Background(), githubRepo,
		IssueSpec{Title: "New work", Body: "details", Labels: []string{"task:ready"}})
	if err != nil || issue.Number != 50 {
		t.Fatalf("CreateIssue = %+v, %v", issue, err)
	}
	body := decodeBody(t, stub.requests("POST /repos/acme/widgets/issues")[0])
	if body["body"] != "details" || !slices.Equal(anyStrings(body["labels"]), []string{"Task:Ready"}) {
		t.Errorf("body = %v", body)
	}
}

// TestGitHubBackend_CreateIssue_Refusals covers every way a create fails: bad
// input and unknown labels before any write, and a write that did not land.
func TestGitHubBackend_CreateIssue_Refusals(t *testing.T) {
	cases := []struct {
		name    string
		spec    IssueSpec
		answer  map[string]any
		want    error
		creates int
	}{
		{name: "no title", spec: IssueSpec{Title: " "}, want: ErrInvalidInput},
		{name: "unknown label", spec: IssueSpec{Title: "t", Labels: []string{"typo"}}, want: ErrInvalidInput},
		{name: "no number", spec: IssueSpec{Title: "t"}, answer: githubIssueAt(0, "t", "open"),
			want: ErrForgeWriteNotApplied, creates: 1},
		{name: "other title", spec: IssueSpec{Title: "t"}, answer: githubIssueAt(9, "u", "open"),
			want: ErrForgeWriteNotApplied, creates: 1},
		{name: "label dropped", spec: IssueSpec{Title: "t", Labels: []string{"bug"}},
			answer: githubIssueAt(9, "t", "open"), want: ErrForgeWriteNotApplied, creates: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend, stub := newGitHubStub(t)
			githubLabelsDefined(stub, "bug")
			stub.json("POST /repos/acme/widgets/issues", http.StatusCreated, tc.answer)

			_, err := backend.CreateIssue(context.Background(), githubRepo, tc.spec)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if n := len(stub.requests("POST /repos/acme/widgets/issues")); n != tc.creates {
				t.Errorf("creates = %d, want %d", n, tc.creates)
			}
		})
	}
}

// TestGitHubBackend_CreateIssue_IsNeverSentTwice proves an unanswered create
// is unconfirmed rather than repeated into a second issue.
func TestGitHubBackend_CreateIssue_IsNeverSentTwice(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("POST /repos/acme/widgets/issues", http.StatusGatewayTimeout, nil)

	_, err := backend.CreateIssue(context.Background(), githubRepo, IssueSpec{Title: "t"})
	if !errors.Is(err, ErrForgeWriteUnconfirmed) {
		t.Fatalf("err = %v, want ErrForgeWriteUnconfirmed", err)
	}
	if n := len(stub.requests("POST /repos/acme/widgets/issues")); n != 1 {
		t.Errorf("creates = %d, want 1", n)
	}
}

// =============================================================================
// Comments
// =============================================================================

// TestGitHubBackend_CommentOnIssue posts and returns the stored comment.
func TestGitHubBackend_CommentOnIssue(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("POST /repos/acme/widgets/issues/41/comments", http.StatusCreated,
		map[string]any{"id": 77, "body": "on it", "user": map[string]string{"login": "agent-widgets"}})

	comment, err := backend.CommentOnIssue(context.Background(), githubRepo, 41, "on it")
	if err != nil || comment.ID != 77 || comment.Author != "agent-widgets" {
		t.Fatalf("CommentOnIssue = %+v, %v", comment, err)
	}
}

// TestGitHubBackend_CommentOnIssue_Refusals covers an empty comment and one
// GitHub stored differently.
func TestGitHubBackend_CommentOnIssue_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	if _, err := backend.CommentOnIssue(context.Background(), githubRepo, 41, "  "); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty body err = %v, want ErrInvalidInput", err)
	}

	stub.json("POST /repos/acme/widgets/issues/41/comments", http.StatusCreated, map[string]any{"id": 77, "body": "other"})
	if _, err := backend.CommentOnIssue(context.Background(), githubRepo, 41, "on it"); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("mismatch err = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestGitHubBackend_ListIssueComments reads the discussion in order.
func TestGitHubBackend_ListIssueComments(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/issues/41/comments", http.StatusOK, []map[string]any{
		{"id": 1, "body": "first"}, {"id": 2, "body": "second"},
	})

	comments, err := backend.ListIssueComments(context.Background(), githubRepo, 41)
	if err != nil || len(comments) != 2 || comments[1].Body != "second" {
		t.Fatalf("ListIssueComments = %+v, %v", comments, err)
	}

	stub.json("GET /repos/acme/widgets/issues/41/comments", http.StatusUnauthorized, nil)
	if _, err := backend.ListIssueComments(context.Background(), githubRepo, 41); forgeStatusOf(err) != http.StatusUnauthorized {
		t.Errorf("err = %v, want the 401", err)
	}
}

// =============================================================================
// CloseIssue
// =============================================================================

// TestGitHubBackend_CloseIssue proves the close is a PATCH whose answer is
// the proof.
func TestGitHubBackend_CloseIssue(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("PATCH /repos/acme/widgets/issues/41", http.StatusOK, githubIssueAt(41, "t", "closed"))

	if err := backend.CloseIssue(context.Background(), githubRepo, 41); err != nil {
		t.Fatalf("CloseIssue: %v", err)
	}
	if body := decodeBody(t, stub.requests("PATCH /repos/acme/widgets/issues/41")[0]); body["state"] != "closed" {
		t.Errorf("body = %v", body)
	}
}

// TestGitHubBackend_CloseIssue_Refusals covers an issue left open and a
// refused PATCH.
func TestGitHubBackend_CloseIssue_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("PATCH /repos/acme/widgets/issues/41", http.StatusOK, githubIssueAt(41, "t", "open"))
	if err := backend.CloseIssue(context.Background(), githubRepo, 41); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("still open err = %v, want ErrForgeWriteNotApplied", err)
	}

	stub.json("PATCH /repos/acme/widgets/issues/41", http.StatusGone, nil)
	if err := backend.CloseIssue(context.Background(), githubRepo, 41); forgeStatusOf(err) != http.StatusGone {
		t.Errorf("refused err = %v, want the 410", err)
	}
}

// =============================================================================
// Labels on an issue
// =============================================================================

// TestGitHubBackend_SetIssueLabels replaces the set and reads it back.
func TestGitHubBackend_SetIssueLabels(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubLabelsDefined(stub, "task:ready", "bug")
	stub.json("PUT /repos/acme/widgets/issues/41/labels", http.StatusOK, nil)
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusOK, githubIssueAt(41, "t", "open", "bug", "task:ready"))

	if err := backend.SetIssueLabels(context.Background(), githubRepo, 41, []string{"bug", "task:ready"}); err != nil {
		t.Fatalf("SetIssueLabels: %v", err)
	}
	body := decodeBody(t, stub.requests("PUT /repos/acme/widgets/issues/41/labels")[0])
	if !slices.Equal(anyStrings(body["labels"]), []string{"bug", "task:ready"}) {
		t.Errorf("body = %v", body)
	}
}

// TestGitHubBackend_SetIssueLabels_Refusals covers a set GitHub did not keep,
// an unknown label, and a refused write.
func TestGitHubBackend_SetIssueLabels_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubLabelsDefined(stub, "bug", "task:ready")
	stub.json("PUT /repos/acme/widgets/issues/41/labels", http.StatusOK, nil)
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusOK, githubIssueAt(41, "t", "open", "bug"))

	if err := backend.SetIssueLabels(context.Background(), githubRepo, 41, []string{"task:ready"}); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("not kept err = %v, want ErrForgeWriteNotApplied", err)
	}
	if err := backend.SetIssueLabels(context.Background(), githubRepo, 41, []string{"typo"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown err = %v, want ErrInvalidInput", err)
	}
	stub.json("PUT /repos/acme/widgets/issues/41/labels", http.StatusForbidden, nil)
	if err := backend.SetIssueLabels(context.Background(), githubRepo, 41, []string{"bug"}); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v, want the 403", err)
	}
	stub.json("PUT /repos/acme/widgets/issues/41/labels", http.StatusOK, nil)
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusInternalServerError, nil)
	if err := backend.SetIssueLabels(context.Background(), githubRepo, 41, []string{"bug"}); !errors.Is(err, ErrBackend) {
		t.Errorf("read-back err = %v, want ErrBackend", err)
	}
}

// TestGitHubBackend_AddIssueLabels adds and reads back; adding nothing makes
// no call at all.
func TestGitHubBackend_AddIssueLabels(t *testing.T) {
	backend, stub := newGitHubStub(t)
	if err := backend.AddIssueLabels(context.Background(), githubRepo, 41, nil); err != nil || len(stub.writes()) != 0 {
		t.Fatalf("empty add = %v with %d writes", err, len(stub.writes()))
	}

	githubLabelsDefined(stub, "bug")
	stub.json("POST /repos/acme/widgets/issues/41/labels", http.StatusOK, nil)
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusOK, githubIssueAt(41, "t", "open", "bug", "other"))
	if err := backend.AddIssueLabels(context.Background(), githubRepo, 41, []string{"BUG"}); err != nil {
		t.Fatalf("AddIssueLabels: %v", err)
	}
}

// TestGitHubBackend_AddIssueLabels_Refusals covers a label that did not stick,
// an unknown label, a refused write and a failed read-back.
func TestGitHubBackend_AddIssueLabels_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubLabelsDefined(stub, "bug")
	stub.json("POST /repos/acme/widgets/issues/41/labels", http.StatusOK, nil)
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusOK, githubIssueAt(41, "t", "open"))

	if err := backend.AddIssueLabels(context.Background(), githubRepo, 41, []string{"bug"}); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("not added err = %v, want ErrForgeWriteNotApplied", err)
	}
	if err := backend.AddIssueLabels(context.Background(), githubRepo, 41, []string{"typo"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown err = %v, want ErrInvalidInput", err)
	}
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusNotFound, nil)
	if err := backend.AddIssueLabels(context.Background(), githubRepo, 41, []string{"bug"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("read-back err = %v, want ErrNotFound", err)
	}
	stub.json("POST /repos/acme/widgets/issues/41/labels", http.StatusUnprocessableEntity, nil)
	if err := backend.AddIssueLabels(context.Background(), githubRepo, 41, []string{"bug"}); forgeStatusOf(err) != http.StatusUnprocessableEntity {
		t.Errorf("refused err = %v, want the 422", err)
	}
}

// TestGitHubBackend_RemoveIssueLabel proves the label is removed by its
// repository spelling, and a label already absent (404) is success.
func TestGitHubBackend_RemoveIssueLabel(t *testing.T) {
	backend, stub := newGitHubStub(t)
	githubLabelsDefined(stub, "task:in progress")
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusOK, githubIssueAt(41, "t", "open", "bug"))

	if err := backend.RemoveIssueLabel(context.Background(), githubRepo, 41, "Task:In Progress"); err != nil {
		t.Fatalf("RemoveIssueLabel on an absent label: %v", err)
	}
	stub.json("DELETE /repos/acme/widgets/issues/41/labels/task:in progress", http.StatusOK, nil)
	if err := backend.RemoveIssueLabel(context.Background(), githubRepo, 41, "task:in progress"); err != nil {
		t.Fatalf("RemoveIssueLabel: %v", err)
	}
	if n := len(stub.requests("DELETE /repos/acme/widgets/issues/41/labels/task:in progress")); n != 2 {
		t.Errorf("deletes = %d, want 2", n)
	}
}

// TestGitHubBackend_RemoveIssueLabel_Refusals covers an empty name, an
// unknown label, a refused delete, a failed read-back and a label still on.
func TestGitHubBackend_RemoveIssueLabel_Refusals(t *testing.T) {
	backend, stub := newGitHubStub(t)
	if err := backend.RemoveIssueLabel(context.Background(), githubRepo, 41, ""); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty err = %v, want ErrInvalidInput", err)
	}
	githubLabelsDefined(stub, "bug")
	if err := backend.RemoveIssueLabel(context.Background(), githubRepo, 41, "typo"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown err = %v, want ErrInvalidInput", err)
	}

	stub.json("DELETE /repos/acme/widgets/issues/41/labels/bug", http.StatusOK, nil)
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusOK, githubIssueAt(41, "t", "open", "bug"))
	if err := backend.RemoveIssueLabel(context.Background(), githubRepo, 41, "bug"); !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Errorf("still on err = %v, want ErrForgeWriteNotApplied", err)
	}
	stub.json("GET /repos/acme/widgets/issues/41", http.StatusForbidden, nil)
	if err := backend.RemoveIssueLabel(context.Background(), githubRepo, 41, "bug"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("read-back err = %v, want the 403", err)
	}
	stub.json("DELETE /repos/acme/widgets/issues/41/labels/bug", http.StatusForbidden, nil)
	if err := backend.RemoveIssueLabel(context.Background(), githubRepo, 41, "bug"); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("refused err = %v, want the 403", err)
	}
}

// TestGitHubBackend_LabelVocabularyFailures proves a vocabulary that cannot be
// read stops a label write before it is sent.
func TestGitHubBackend_LabelVocabularyFailures(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/labels", http.StatusForbidden, nil)

	if err := backend.SetIssueLabels(context.Background(), githubRepo, 41, []string{"bug"}); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("err = %v, want the 403", err)
	}
	if _, err := backend.CreateIssue(context.Background(), githubRepo, IssueSpec{Title: "t", Labels: []string{"bug"}}); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("err = %v, want the 403", err)
	}
	if len(stub.writes()) != 0 {
		t.Errorf("writes = %+v, want none", stub.writes())
	}
}

// =============================================================================
// Collaborators
// =============================================================================

// TestGitHubBackend_ListCollaborators maps logins.
func TestGitHubBackend_ListCollaborators(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("GET /repos/acme/widgets/collaborators", http.StatusOK, []map[string]string{{"login": "a"}, {"login": "b"}})

	users, err := backend.ListCollaborators(context.Background(), githubRepo)
	if err != nil || len(users) != 2 || users[1].Login != "b" {
		t.Fatalf("ListCollaborators = %+v, %v", users, err)
	}

	stub.json("GET /repos/acme/widgets/collaborators", http.StatusForbidden, nil)
	if _, err := backend.ListCollaborators(context.Background(), githubRepo); forgeStatusOf(err) != http.StatusForbidden {
		t.Errorf("err = %v, want the 403", err)
	}
}

// anyStrings converts a decoded JSON array to strings.
func anyStrings(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}
