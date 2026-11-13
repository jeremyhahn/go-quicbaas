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
	"strings"
	"time"
)

// =============================================================================
// GitHub adapter
// =============================================================================
//
// The module's second forge, written against GitHub's REST API v3 over the
// shared forge HTTP core (forge_http.go). It mirrors the Gitea adapter's
// structure: every write is read back, every listing is paged to its end or
// reported as incomplete, and a capability GitHub lacks is refused with a typed
// error rather than quietly skipped.
//
// Split by concern the way the Gitea adapter is:
//
//	forge_github.go             construction, dialect, paging, identity
//	forge_github_issues.go      issues, comments, labels, collaborators
//	forge_github_pulls.go       branches, tags, pull requests, merges
//	forge_github_checks.go      reviews, commit statuses, check runs, Actions
//	forge_github_webhook.go     delivery verification, parsing, configuration
//	forge_github_onboarding.go  the ForgeOnboarding surface

const (
	// githubDefaultBaseURL is GitHub.com's API root, used when a binding names
	// none. The only host this adapter ever writes down: a GitHub Enterprise
	// Server binding names its own root ("https://ghe.example/api/v3").
	githubDefaultBaseURL = "https://api.github.com"

	// githubMediaType is the media type GitHub documents for its REST API.
	githubMediaType = "application/vnd.github+json"

	// githubAPIVersion pins the REST API version. Pinned rather than left to
	// the server's default so a version GitHub introduces later cannot change
	// a response shape under the adapter.
	githubAPIVersion = "2022-11-28"

	// githubPageSize is GitHub's maximum page size; the larger the page, the
	// fewer requests a listing costs against the rate limit.
	githubPageSize = 100
)

// GitHubConfig configures the adapter.
type GitHubConfig struct {
	// BaseURL is the API root. Empty means GitHub.com.
	BaseURL string

	// Token authenticates every call: a personal access token or an
	// installation token. Never logged, never placed in an error.
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

// GitHubBackend talks to one GitHub or GitHub Enterprise Server instance.
type GitHubBackend struct {
	forgeHTTP
	sharedNumbering
}

// NewGitHubBackend builds the adapter, failing closed on missing
// configuration.
func NewGitHubBackend(cfg GitHubConfig) (*GitHubBackend, error) {
	base := cfg.BaseURL
	if strings.TrimSpace(base) == "" {
		base = githubDefaultBaseURL
	}
	core, err := newForgeHTTP(ForgeGitHub, githubDialect, forgeHTTPConfig{
		baseURL: base, token: cfg.Token,
		client: cfg.HTTPClient, transport: cfg.Transport, waiter: cfg.Waiter,
	})
	if err != nil {
		return nil, err
	}
	backend := &GitHubBackend{forgeHTTP: core}
	backend.sharedNumbering = sharedNumbering{issues: backend}
	return backend, nil
}

// Compile-time proof the adapter is complete.
var _ ForgeBackend = (*GitHubBackend)(nil)

// Registered as a FACTORY, like Gitea: each project brings its own base URL
// and its own token.
//
// No custody credential is passed: GitHub's REST API publishes no route to
// mint or revoke a personal access token, so this adapter does not implement
// ForgeTokenLifecycle and GitHub tokens are rotated by hand.
func init() {
	RegisterForgeFactory(ForgeGitHub, func(cfg ForgeConfig, creds ForgeCredentials, transport ForgeTransport) (ForgeBackend, error) {
		return NewGitHubBackend(GitHubConfig{
			BaseURL:   cfg.BaseURL,
			Token:     creds.Token,
			Transport: transport,
		})
	})
}

// Type reports the platform this adapter serves.
func (g *GitHubBackend) Type() ForgeType { return ForgeGitHub }

// Capabilities reports what GITHUB can do, measured against its REST API.
//
// Every flag is true but one:
//
//   - NativeMergeQueue: GitHub's merge queue is configured through branch
//     rulesets, and REST v3 publishes no route to enqueue a pull request or
//     read its position. The merge train stays quicbaas's own.
//
// CancelCheckRuns, RerunCheckRuns and CheckRunLogs hold for GitHub Actions
// jobs, whose ids are their check runs' ids. A check run some other app
// published, or a commit status, has no such route and is refused per call
// with ForgeUnsupportedError.
//
// DeleteBranchOnMerge is true although GitHub's merge route takes no such
// parameter: MergePullRequest deletes the source branch itself once the merge
// is verified, and only while the branch still points at the merged head.
func (g *GitHubBackend) Capabilities() ForgeCapabilities {
	return ForgeCapabilities{
		CancelCheckRuns: true,
		RerunCheckRuns:  true,
		CheckRunLogs:    true,

		// GitHub delivers "labeled" and "unlabeled" actions on the issues
		// event for every change.
		ReliableLabelEvents: true,

		// POST .../labels adds, DELETE .../labels/{name} removes one, and PUT
		// replaces the set.
		AdditiveLabels: true,

		DraftPullRequests:   true,
		RequiredReviewers:   true,
		SquashMerge:         true,
		DeleteBranchOnMerge: true,

		// required_status_checks on a branch protection rule names contexts.
		BranchProtectionContexts: true,
		SubmitReviews:            true,
		CommitStatuses:           true,

		NativeMergeQueue: false,
	}
}

// =============================================================================
// Transport
// =============================================================================

// githubDialect is GitHub's wire scheme over the shared forge HTTP core.
var githubDialect = forgeDialect{
	tokenScheme: "Bearer ",
	headers: map[string]string{
		"Accept":               githubMediaType,
		"X-GitHub-Api-Version": githubAPIVersion,
	},
	refusal:  githubRefusal,
	pageSize: githubPageSize,

	// GitHub refuses a second pull request for the same head and base with
	// 422 "A pull request already exists".
	duplicatePullStatus: http.StatusUnprocessableEntity,
}

// githubRefusal gives GitHub's merge refusals their meaning.
//
// Only the merge route is mapped: 405 and 409 mean something specific there
// and something generic everywhere else.
func githubRefusal(req forgeRequest, status int, body []byte) error {
	if req.operation != "MergePullRequest" {
		return nil
	}
	switch status {
	case http.StatusConflict:
		// The sha guard refused: the head is no longer the commit the caller
		// approved. MergePullRequest re-reads the pull request to name both
		// commits. Never retried — a moved head needs a new review.
		return &HeadMovedError{Subject: req.path}
	case http.StatusMethodNotAllowed:
		// "Pull Request is not mergeable": a conflict, a failing required
		// check, or mergeability GitHub has not finished computing. Retryable
		// for the same reason Gitea's 405 is — the merge was neither performed
		// nor finally refused.
		return &ForgeNotReadyError{
			Type:      ForgeGitHub,
			Operation: req.operation,
			Subject:   req.path,
			Detail:    forgeMessage(body),
		}
	}
	return nil
}

// =============================================================================
// Wire shapes shared across the adapter
// =============================================================================
//
// Only the fields the module reads, for the reason the Gitea adapter gives.

type githubUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
}

type githubLabel struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type githubComment struct {
	ID        int64       `json:"id"`
	Body      string      `json:"body"`
	User      *githubUser `json:"user"`
	CreatedAt time.Time   `json:"created_at"`
}

// toForgeComment converts GitHub's shape to the module's.
func (c *githubComment) toForgeComment() ForgeComment {
	comment := ForgeComment{ID: c.ID, Body: c.Body, Created: c.CreatedAt}
	if c.User != nil {
		comment.Author = c.User.Login
	}
	return comment
}

// =============================================================================
// Identity
// =============================================================================

// AuthenticatedUser implements ForgeBackend: the account the adapter's token
// authenticates as.
func (g *GitHubBackend) AuthenticatedUser(ctx context.Context) (*TokenIdentity, error) {
	var out githubUser
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      "/user",
		out:       &out,
		operation: "github.AuthenticatedUser",
	}); err != nil {
		return nil, err
	}
	if out.Login == "" {
		return nil, &TokenVerificationError{
			Name:     "(adapter token)",
			Expected: "an authenticated account",
			Actual:   "none",
		}
	}
	return &TokenIdentity{Username: out.Login}, nil
}
