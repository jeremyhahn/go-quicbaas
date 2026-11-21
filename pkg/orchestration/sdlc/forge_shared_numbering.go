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

import "context"

// issueRoutes is the part of ForgeBackend that writes and reads an issue's
// discussion and labels.
type issueRoutes interface {
	CommentOnIssue(ctx context.Context, repo RepoRef, number int, body string) (*ForgeComment, error)
	ListIssueComments(ctx context.Context, repo RepoRef, number int) ([]ForgeComment, error)
	AddIssueLabels(ctx context.Context, repo RepoRef, number int, labels []string) error
	RemoveIssueLabel(ctx context.Context, repo RepoRef, number int, label string) error
}

// sharedNumbering serves a pull request's discussion and labels from the
// issue endpoints, for a forge that numbers issues and pull requests in one
// sequence and keeps one discussion and label set per number: Gitea and
// GitHub. It is the one place that decision is made. An adapter embeds it,
// pointing at itself; a forge that numbers them apart, as GitLab does,
// implements the pull request operations against its own endpoints instead.
type sharedNumbering struct {
	issues issueRoutes
}

// CommentOnPullRequest implements ForgeBackend through CommentOnIssue.
func (s sharedNumbering) CommentOnPullRequest(ctx context.Context, repo RepoRef, number int, body string) (*ForgeComment, error) {
	return s.issues.CommentOnIssue(ctx, repo, number, body)
}

// ListPullRequestComments implements ForgeBackend through ListIssueComments.
func (s sharedNumbering) ListPullRequestComments(ctx context.Context, repo RepoRef, number int) ([]ForgeComment, error) {
	return s.issues.ListIssueComments(ctx, repo, number)
}

// AddPullRequestLabels implements ForgeBackend through AddIssueLabels.
func (s sharedNumbering) AddPullRequestLabels(ctx context.Context, repo RepoRef, number int, labels []string) error {
	return s.issues.AddIssueLabels(ctx, repo, number, labels)
}

// RemovePullRequestLabel implements ForgeBackend through RemoveIssueLabel.
func (s sharedNumbering) RemovePullRequestLabel(ctx context.Context, repo RepoRef, number int, label string) error {
	return s.issues.RemoveIssueLabel(ctx, repo, number, label)
}
