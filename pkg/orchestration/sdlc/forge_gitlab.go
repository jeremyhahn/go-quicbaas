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
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// GitLab adapter
// =============================================================================
//
// The module's third forge, written against GitLab's REST API v4 over the
// shared forge HTTP core (forge_http.go). It keeps the other adapters'
// rules: every write is read back, every listing is paged to its end or
// reported as incomplete, and a capability GitLab lacks is refused with a
// typed error rather than quietly skipped.
//
// GitLab's vocabulary stops at this adapter's edge. A merge request is a pull
// request and its iid the pull request's number; an approval is a review; a
// pipeline's jobs, and the commit statuses beside them, are the check runs.
// Nothing outside these files learns the other words.
//
//	forge_gitlab.go             construction, dialect, identity
//	forge_gitlab_issues.go      issues, notes, labels, members
//	forge_gitlab_pulls.go       branches, tags, merge requests, merges
//	forge_gitlab_checks.go      approvals, commit statuses, pipeline jobs
//	forge_gitlab_webhook.go     delivery verification, parsing, configuration
//	forge_gitlab_onboarding.go  the ForgeOnboarding surface

const (
	// gitlabDefaultBaseURL is GitLab.com's API root, used when a binding
	// names none. The only host this adapter ever writes down: a
	// self-managed instance names its own root ("https://gitlab.example/api/v4").
	gitlabDefaultBaseURL = "https://gitlab.com/api/v4"

	// gitlabPageSize is GitLab's maximum page size.
	gitlabPageSize = 100

	// gitlabNextPageHeader names the next page of a listing. GitLab omits
	// it (and the Link header) past 10,000 rows of some listings, which the
	// pager then reads as the end — the page ceilings sit far below that.
	gitlabNextPageHeader = "X-Next-Page"
)

// GitLabConfig configures the adapter.
type GitLabConfig struct {
	// BaseURL is the API root, ending in /api/v4. Empty means GitLab.com.
	BaseURL string

	// Token authenticates every call: a personal, group or project access
	// token, or an OAuth token. Never logged, never placed in an error.
	Token string

	// HTTPClient is optional; see GiteaConfig.HTTPClient.
	HTTPClient *http.Client

	// Transport bounds each call and paces the retry of unanswered ones. A
	// zero field takes DefaultForgeTransport's.
	Transport ForgeTransport

	// Waiter paces the retries; nil takes exponential backoff from
	// Transport.RetryBackoff. Tests inject one that does not wait.
	Waiter RetryWaiter
}

// GitLabBackend talks to one GitLab.com or self-managed GitLab instance.
type GitLabBackend struct {
	forgeHTTP
}

// NewGitLabBackend builds the adapter, failing closed on missing
// configuration.
func NewGitLabBackend(cfg GitLabConfig) (*GitLabBackend, error) {
	base := cfg.BaseURL
	if strings.TrimSpace(base) == "" {
		base = gitlabDefaultBaseURL
	}
	core, err := newForgeHTTP(ForgeGitLab, gitlabDialect, forgeHTTPConfig{
		baseURL: base, token: cfg.Token,
		client: cfg.HTTPClient, transport: cfg.Transport, waiter: cfg.Waiter,
	})
	if err != nil {
		return nil, err
	}
	return &GitLabBackend{forgeHTTP: core}, nil
}

// Compile-time proof the adapter is complete.
var _ ForgeBackend = (*GitLabBackend)(nil)

// Registered as a FACTORY, like Gitea and GitHub: each project brings its own
// base URL and its own token.
//
// No custody credential is passed and ForgeTokenLifecycle is not implemented.
// GitLab can rotate an access token in place, but only the token's own
// rotate route mints its successor, so a custody token could not issue or
// revoke a project's agent token the way Gitea's admin routes do. GitLab
// tokens are rotated by hand.
func init() {
	RegisterForgeFactory(ForgeGitLab, func(cfg ForgeConfig, creds ForgeCredentials, transport ForgeTransport) (ForgeBackend, error) {
		return NewGitLabBackend(GitLabConfig{
			BaseURL:   cfg.BaseURL,
			Token:     creds.Token,
			Transport: transport,
		})
	})
}

// Type reports the platform this adapter serves.
func (g *GitLabBackend) Type() ForgeType { return ForgeGitLab }

// Capabilities reports what GITLAB can do, measured against its REST API v4.
//
// Two flags are false, each because GitLab has no equivalent:
//
//   - BranchProtectionContexts: a protected branch carries push and merge
//     access levels only. GitLab's nearest setting is the project-wide
//     "pipelines must succeed", which names no status context, so "this
//     branch must require quicbaas/review" cannot be expressed.
//     BranchProtection refuses with ForgeUnsupportedError.
//   - SubmitReviews: REST v4 can approve a merge request at a commit and
//     withdraw an approval, but it cannot record a request-changes verdict.
//     A reviewer that can only approve cannot reject, so SubmitReview refuses
//     with ForgeUnsupportedError rather than post half a verdict.
//
// RequiredReviewers (approval rules) and NativeMergeQueue (merge trains) are
// true because the REST routes exist; both are paid tiers on GitLab, and an
// instance without them answers the routes with a refusal.
//
// CancelCheckRuns, RerunCheckRuns and CheckRunLogs hold for CI jobs, whose
// ids are their check runs' ids. A commit status some other publisher wrote
// has no job behind it and is refused per call with ForgeUnsupportedError.
func (g *GitLabBackend) Capabilities() ForgeCapabilities {
	return ForgeCapabilities{
		CancelCheckRuns: true,
		RerunCheckRuns:  true,
		CheckRunLogs:    true,

		// The issue hook carries changes.labels for every label change.
		ReliableLabelEvents: true,

		// PUT .../issues/:iid takes add_labels and remove_labels beside the
		// replacing labels.
		AdditiveLabels: true,

		DraftPullRequests: true,
		RequiredReviewers: true,
		SquashMerge:       true,

		// The merge route takes should_remove_source_branch.
		DeleteBranchOnMerge: true,

		BranchProtectionContexts: false,
		SubmitReviews:            false,
		CommitStatuses:           true,

		// POST /projects/:id/merge_trains/merge_requests/:iid enqueues.
		NativeMergeQueue: true,
	}
}

// =============================================================================
// Transport
// =============================================================================

// gitlabDialect is GitLab's wire scheme over the shared forge HTTP core.
//
// The token travels as a Bearer credential. ForgeConfig records no token
// kind, and Bearer is the one header GitLab accepts for every kind —
// personal, group and project access tokens as well as OAuth tokens — where
// PRIVATE-TOKEN refuses an OAuth token.
var gitlabDialect = forgeDialect{
	tokenScheme: "Bearer ",
	refusal:     gitlabRefusal,
	pageSize:    gitlabPageSize,
	pageHeader:  gitlabNextPageHeader,

	// GitLab refuses a second open merge request for the same source and
	// target with 409 "Another open merge request already exists".
	duplicatePullStatus: http.StatusConflict,
}

// gitlabRefusal gives GitLab's merge refusals their meaning.
//
// Only the merge route is mapped: these statuses mean something specific
// there and something generic everywhere else.
func gitlabRefusal(req forgeRequest, status int, body []byte) error {
	if req.operation != "MergePullRequest" {
		return nil
	}
	switch status {
	case http.StatusConflict:
		// "SHA does not match HEAD of source branch": the sha guard refused.
		// Never retried — a moved head needs a new review.
		return &HeadMovedError{Subject: req.path}
	case http.StatusMethodNotAllowed, http.StatusNotAcceptable, http.StatusUnprocessableEntity:
		// Not mergeable yet: a draft, a conflict, a pipeline still running,
		// approvals outstanding, or a rebase the project's merge method
		// requires. The merge was neither performed nor finally refused.
		return &ForgeNotReadyError{
			Type:      ForgeGitLab,
			Operation: req.operation,
			Subject:   req.path,
			Detail:    forgeMessage(body),
		}
	}
	return nil
}

// gitlabProjectPath renders the API path of one project. GitLab addresses a
// project by its URL-encoded full path, so a nested group's slashes travel
// as %2F inside one segment.
func gitlabProjectPath(repo RepoRef) string {
	return "/projects/" + url.PathEscape(repo.Owner+"/"+repo.Repo)
}

// gitlabRefPath renders a branch or tag name as one path segment, slashes
// encoded, which is how GitLab's repository routes address a ref.
func gitlabRefPath(name string) string {
	return url.PathEscape(name)
}

// gitlabID renders a numeric id for a path.
func gitlabID(id int64) string {
	return strconv.FormatInt(id, 10)
}

// =============================================================================
// Wire shapes shared across the adapter
// =============================================================================
//
// Only the fields the module reads, for the reason the Gitea adapter gives.

type gitlabUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

type gitlabLabel struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type gitlabNote struct {
	ID        int64       `json:"id"`
	Body      string      `json:"body"`
	System    bool        `json:"system"`
	Author    *gitlabUser `json:"author"`
	CreatedAt time.Time   `json:"created_at"`
}

// toForgeComment converts GitLab's shape to the module's.
func (n *gitlabNote) toForgeComment() ForgeComment {
	comment := ForgeComment{ID: n.ID, Body: n.Body, Created: n.CreatedAt}
	if n.Author != nil {
		comment.Author = n.Author.Username
	}
	return comment
}

// =============================================================================
// Identity
// =============================================================================

// AuthenticatedUser implements ForgeBackend: the account the adapter's token
// authenticates as.
func (g *GitLabBackend) AuthenticatedUser(ctx context.Context) (*TokenIdentity, error) {
	var out gitlabUser
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      "/user",
		out:       &out,
		operation: "gitlab.AuthenticatedUser",
	}); err != nil {
		return nil, err
	}
	if out.Username == "" {
		return nil, &TokenVerificationError{
			Name:     "(adapter token)",
			Expected: "an authenticated account",
			Actual:   "none",
		}
	}
	return &TokenIdentity{Username: out.Username}, nil
}
