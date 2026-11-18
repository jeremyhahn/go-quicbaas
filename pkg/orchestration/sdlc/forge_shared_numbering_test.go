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
	"testing"
)

// TestSharedNumbering_ServesThePullRequestFromTheIssue proves both pull
// request operations reach the issue operations with the same number.
func TestSharedNumbering_ServesThePullRequestFromTheIssue(t *testing.T) {
	forge := newFakeForge()
	shared := sharedNumbering{issues: forge}

	posted, err := shared.CommentOnPullRequest(context.Background(), testRepoRef, 41, "Landed.")
	if err != nil || posted.Body != "Landed." {
		t.Fatalf("CommentOnPullRequest = %+v, %v", posted, err)
	}
	comments, err := shared.ListPullRequestComments(context.Background(), testRepoRef, 41)
	if err != nil || len(comments) != 1 || comments[0].ID != posted.ID {
		t.Fatalf("ListPullRequestComments = %+v, %v; want the comment just posted", comments, err)
	}
	if len(forge.issueDiscussions) != 2 || forge.issueDiscussions[0] != 41 || forge.issueDiscussions[1] != 41 {
		t.Fatalf("issue discussions = %v, want #41 written then read", forge.issueDiscussions)
	}
}

// TestSharedNumbering_ReturnsTheIssueErrors proves a refusal of the issue
// operation is the pull request operation's refusal, unchanged.
func TestSharedNumbering_ReturnsTheIssueErrors(t *testing.T) {
	forge := newFakeForge()
	forge.commentErr = backendError("CommentOnIssue", errors.New("forge down"))
	forge.listCommentsErr = backendError("ListIssueComments", errors.New("forge down"))
	shared := sharedNumbering{issues: forge}

	if _, err := shared.CommentOnPullRequest(context.Background(), testRepoRef, 41, "Landed."); !errors.Is(err, ErrBackend) {
		t.Errorf("CommentOnPullRequest err = %v, want ErrBackend", err)
	}
	if _, err := shared.ListPullRequestComments(context.Background(), testRepoRef, 41); !errors.Is(err, ErrBackend) {
		t.Errorf("ListPullRequestComments err = %v, want ErrBackend", err)
	}
}

// TestSharedNumbering_LabelsThePullRequestThroughTheIssue proves both pull
// request label operations reach the issue label operations with the same
// number.
func TestSharedNumbering_LabelsThePullRequestThroughTheIssue(t *testing.T) {
	forge := newFakeForge()
	forge.issues[41] = &ForgeIssue{Number: 41, State: "open"}
	shared := sharedNumbering{issues: forge}

	if err := shared.AddPullRequestLabels(context.Background(), testRepoRef, 41, []string{"factory:escalated"}); err != nil {
		t.Fatalf("AddPullRequestLabels: %v", err)
	}
	if err := shared.RemovePullRequestLabel(context.Background(), testRepoRef, 41, "factory:escalated"); err != nil {
		t.Fatalf("RemovePullRequestLabel: %v", err)
	}
	if len(forge.issueLabelled) != 2 || forge.issueLabelled[0] != 41 || forge.issueLabelled[1] != 41 {
		t.Fatalf("issue label writes = %v, want #41 added then removed", forge.issueLabelled)
	}
	if forge.labelAdds[0] != "factory:escalated" || forge.labelRemovals[0] != "factory:escalated" {
		t.Fatalf("labels = +%v -%v", forge.labelAdds, forge.labelRemovals)
	}
}

// TestSharedNumbering_ReturnsTheIssueLabelErrors proves a refused issue label
// write is the pull request label write's refusal, unchanged.
func TestSharedNumbering_ReturnsTheIssueLabelErrors(t *testing.T) {
	forge := newFakeForge()
	forge.addLabelErr = backendError("AddIssueLabels", errors.New("forge down"))
	forge.removeLabelErr = backendError("RemoveIssueLabel", errors.New("forge down"))
	shared := sharedNumbering{issues: forge}

	if err := shared.AddPullRequestLabels(context.Background(), testRepoRef, 41, []string{"x"}); !errors.Is(err, ErrBackend) {
		t.Errorf("AddPullRequestLabels err = %v, want ErrBackend", err)
	}
	if err := shared.RemovePullRequestLabel(context.Background(), testRepoRef, 41, "x"); !errors.Is(err, ErrBackend) {
		t.Errorf("RemovePullRequestLabel err = %v, want ErrBackend", err)
	}
}

// TestGitHubBackend_PullRequestDiscussionIsTheIssues proves the constructor
// wires the shared numbering: GitHub serves a pull request's conversation
// from its issue comments.
func TestGitHubBackend_PullRequestDiscussionIsTheIssues(t *testing.T) {
	backend, stub := newGitHubStub(t)
	stub.json("POST /repos/acme/widgets/issues/41/comments", http.StatusCreated, map[string]any{"id": 77, "body": "Landed."})
	stub.json("GET /repos/acme/widgets/issues/41/comments", http.StatusOK, []map[string]any{{"id": 77, "body": "Landed."}})

	if _, err := backend.CommentOnPullRequest(context.Background(), githubRepo, 41, "Landed."); err != nil {
		t.Fatalf("CommentOnPullRequest: %v", err)
	}
	if comments, err := backend.ListPullRequestComments(context.Background(), githubRepo, 41); err != nil || len(comments) != 1 {
		t.Fatalf("ListPullRequestComments = %+v, %v", comments, err)
	}
}

// TestGiteaBackend_PullRequestDiscussionIsTheIssues proves the same for Gitea.
func TestGiteaBackend_PullRequestDiscussionIsTheIssues(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/issues/32/comments", http.StatusCreated,
		map[string]any{"id": 5, "body": "Landed."})

	if _, err := backend.CommentOnPullRequest(context.Background(), testRepo, 32, "Landed."); err != nil {
		t.Fatalf("CommentOnPullRequest: %v", err)
	}
}
