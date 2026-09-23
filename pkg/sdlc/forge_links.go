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

// =============================================================================
// The forge's own issue-to-pull-request link
// =============================================================================
//
// A pull request's text is one way to learn that it closes an issue; the
// forge's own record of the link is the authoritative one. Gitea keeps it on
// the issue's timeline, as a cross-reference from the pull request that closes
// it. The timeline entry survives a restart, and it says whether the pull request
// is still open, which is exactly what go-quicbaas#1054 asks readiness to act
// on: only an open pull request holds its issue.
//
// AN OPTIONAL CAPABILITY, not a ForgeBackend method. The text reading in
// closing_refs.go works on every forge. A forge that can also answer this is
// asked as well, and one that cannot is not made to pretend. It costs one read
// per issue that nothing else has already claimed, so it runs as a readiness
// predicate after the cheap ones rather than once per sweep.

// ForgeIssueLink is one pull request the forge links to an issue as closing it.
type ForgeIssueLink struct {
	// PullRequest is the linked pull request's number in the issue's own
	// repository.
	PullRequest int

	// Open reports whether the pull request is still open.
	Open bool

	// Merged reports whether it merged.
	Merged bool
}

// IssueLinkReader is implemented by a forge that records which pull requests
// close an issue.
type IssueLinkReader interface {
	// ListClosingPullRequests returns every pull request of repo the forge
	// links to the issue as closing it, open, merged or closed. WHOLE, and a
	// prefix is a fault, for the reason every listing in this module says so:
	// a truncated answer reads as "nothing claims this issue", and the issue
	// is adopted twice.
	ListClosingPullRequests(ctx context.Context, repo RepoRef, issue int) ([]ForgeIssueLink, error)
}

// claimFromLinks reduces an issue's forge links to the claim that holds it: the
// lowest-numbered open pull request. ONLY AN OPEN PULL REQUEST CLAIMS. One
// closed without merging is abandoned, and one that merged is history: the
// link outlives the merge, so an issue reopened because that fix was
// incomplete or reverted would otherwise never be adopted again (see
// issue_source_claims.go).
func claimFromLinks(links []ForgeIssueLink) (ClosingClaim, bool) {
	var open ClosingClaim
	for _, link := range links {
		if link.Open && (open.PullRequest == 0 || link.PullRequest < open.PullRequest) {
			open = ClosingClaim{PullRequest: link.PullRequest}
		}
	}
	return open, open.PullRequest != 0
}
