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
	"strconv"
)

// Gitea records a pull request that closes an issue as a timeline entry on
// the issue: type "pull_ref", with ref_action "closes", and the pull request
// itself as ref_issue. An edit that removes the keyword turns the action to
// "neutered", which is no longer a claim.
const (
	giteaTimelinePullRef    = "pull_ref"
	giteaRefActionCloses    = "closes"
	giteaIssueStateOpenName = "open"
)

// giteaTimelineEntry is the part of a Gitea timeline comment the link reader
// needs.
type giteaTimelineEntry struct {
	Type      string `json:"type"`
	RefAction string `json:"ref_action"`
	RefIssue  *struct {
		Number      int    `json:"number"`
		State       string `json:"state"`
		PullRequest *struct {
			Merged bool `json:"merged"`
		} `json:"pull_request"`
		Repository *struct {
			Owner string `json:"owner"`
			Name  string `json:"name"`
		} `json:"repository"`
	} `json:"ref_issue"`
}

// errTimelinePagesExhausted reports a timeline that never returned a short
// page. A fault rather than a truncation: see IssueLinkReader.
var errTimelinePagesExhausted = errors.New("issue timeline never returned a short page")

var _ IssueLinkReader = (*GiteaBackend)(nil)

// ListClosingPullRequests implements IssueLinkReader from the issue's
// timeline. Only pull requests of repo itself are returned: a claim names a
// pull request by number, and a number means nothing in another repository.
func (g *GiteaBackend) ListClosingPullRequests(ctx context.Context, repo RepoRef, issue int) ([]ForgeIssueLink, error) {
	var links []ForgeIssueLink
	for page := 1; page <= maxIssuePages; page++ {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(giteaDefaultPageSize))
		query.Set("page", strconv.Itoa(page))

		var raw []giteaTimelineEntry
		if err := g.do(ctx, forgeRequest{
			method:    http.MethodGet,
			path:      repoPath(repo) + "/issues/" + strconv.Itoa(issue) + "/timeline",
			query:     query,
			out:       &raw,
			operation: "ListClosingPullRequests",
		}); err != nil {
			return nil, err
		}
		for i := range raw {
			if link, ok := raw[i].closingLink(repo); ok {
				links = append(links, link)
			}
		}
		if len(raw) < giteaDefaultPageSize {
			return links, nil
		}
	}
	return nil, backendError("ListClosingPullRequests", errTimelinePagesExhausted)
}

// closingLink reads one timeline entry as a closing link from a pull request
// of repo, reporting false for anything else.
func (e *giteaTimelineEntry) closingLink(repo RepoRef) (ForgeIssueLink, bool) {
	ref := e.RefIssue
	if e.Type != giteaTimelinePullRef || e.RefAction != giteaRefActionCloses || ref == nil || ref.PullRequest == nil || ref.Number <= 0 {
		return ForgeIssueLink{}, false
	}
	if ref.Repository != nil && !sameRepo(repo, RepoRef{Owner: ref.Repository.Owner, Repo: ref.Repository.Name}) {
		return ForgeIssueLink{}, false
	}
	return ForgeIssueLink{
		PullRequest: ref.Number,
		Open:        ref.State == giteaIssueStateOpenName && !ref.PullRequest.Merged,
		Merged:      ref.PullRequest.Merged,
	}, true
}
