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
)

// errBranchPagesExhausted reports a branch listing that never ended.
//
// This is a fault, not a truncation, for the same reason as
// errOpenPullPagesExhausted. A sweep that counted stranded branches from a
// prefix of the listing would report fewer than the forge holds.
var errBranchPagesExhausted = errors.New(
	"branch listing never returned a short page")

// The GitHub adapter serves the branch inventory a sweep needs.
var _ ForgeBranchInventory = (*GitHubBackend)(nil)

// ListBranches returns every branch in the repository with its tip. It
// returns all of them or an error, never a partial list.
func (g *GitHubBackend) ListBranches(ctx context.Context, repo RepoRef) ([]ForgeBranch, error) {
	var branches []ForgeBranch
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListBranches",
		path:      repoPath(repo) + "/branches",
		maxPages:  maxIssuePages,
		exhausted: backendError("ListBranches", errBranchPagesExhausted),
	}, func(page []githubBranch) bool {
		for i := range page {
			branches = append(branches, ForgeBranch{Name: page[i].Name, SHA: page[i].Commit.SHA})
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return branches, nil
}

// BranchContained reports whether base already carries headSHA. GitHub
// compares base...headSHA: "identical" and "behind" mean every commit of
// headSHA is in base, while "ahead" and "diverged" mean some are not.
// Any other answer is an error and is never read as containment.
func (g *GitHubBackend) BranchContained(ctx context.Context, repo RepoRef, base, headSHA string) (bool, error) {
	if base == "" {
		return false, &InvalidInputError{Field: "base", Message: "a containment check needs a base"}
	}
	if headSHA == "" {
		return false, &InvalidInputError{Field: "head_sha", Message: "a containment check needs a head commit"}
	}
	var compare struct {
		Status string `json:"status"`
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/compare/" + refPath(base) + "..." + headSHA,
		out:       &compare,
		operation: "BranchContained",
	}); err != nil {
		return false, err
	}
	switch compare.Status {
	case "identical", "behind":
		return true, nil
	case "ahead", "diverged":
		return false, nil
	default:
		return false, &IncompleteResponseError{
			Op:      "BranchContained " + repo.String() + " " + base + "..." + headSHA,
			Missing: "status",
		}
	}
}
