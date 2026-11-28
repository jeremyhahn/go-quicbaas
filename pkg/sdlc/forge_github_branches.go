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
	"fmt"
	"net/http"
	"strings"
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

// githubZeroOID is the object ID a ref update names to delete the ref.
const githubZeroOID = "0000000000000000000000000000000000000000"

// githubDeleteRefMutation applies the ref updates it is given atomically,
// refusing any whose ref is not at its beforeOid.
const githubDeleteRefMutation = `mutation($input: UpdateRefsInput!) { updateRefs(input: $input) { clientMutationId } }`

// githubGraphQLURL is the GraphQL endpoint of the GitHub whose REST root is
// base: https://api.github.com/graphql for github.com, and
// https://host/api/graphql for a GitHub Enterprise Server at
// https://host/api/v3.
func githubGraphQLURL(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/api/v3") {
		return strings.TrimSuffix(base, "/v3") + "/graphql"
	}
	return base + "/graphql"
}

// repositoryNodeID returns the repository's GraphQL node ID.
func (g *GitHubBackend) repositoryNodeID(ctx context.Context, repo RepoRef) (string, error) {
	var raw struct {
		NodeID string `json:"node_id"`
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo),
		out:       &raw,
		operation: "RepositoryNodeID",
	}); err != nil {
		return "", err
	}
	if raw.NodeID == "" {
		return "", &IncompleteResponseError{Op: "RepositoryNodeID " + repo.String(), Missing: "node_id"}
	}
	return raw.NodeID, nil
}

// DeleteBranchAt deletes name only while it points at sha.
//
// GitHub's REST API has no conditional ref delete, so this is the GraphQL
// updateRefs mutation with beforeOid set to sha: GitHub refuses the update
// when the ref is anywhere else, in the same operation as the delete. A
// refusal answers 200 with an errors array, so the outcome is decided by
// reading the branch back, never by the wording of the message: absent is
// a delete, a different tip is ErrBranchMoved, and the same tip is an
// error. It never falls back to an unconditional delete.
func (g *GitHubBackend) DeleteBranchAt(ctx context.Context, repo RepoRef, name, sha string) error {
	if name == "" {
		return &InvalidInputError{Field: "name", Message: "a conditional delete needs a branch name"}
	}
	if sha == "" {
		return &InvalidInputError{Field: "sha", Message: "a conditional delete needs the expected tip"}
	}
	id, err := g.repositoryNodeID(ctx, repo)
	if err != nil {
		return err
	}
	var answer struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	postErr := g.do(ctx, forgeRequest{
		method:   http.MethodPost,
		path:     "/graphql",
		endpoint: githubGraphQLURL(g.baseURL),
		headers:  map[string]string{"GraphQL-Features": "update_refs_preview"},
		body: map[string]any{
			"query": githubDeleteRefMutation,
			"variables": map[string]any{
				"input": map[string]any{
					"repositoryId": id,
					"refUpdates": []map[string]any{{
						"name":      "refs/heads/" + name,
						"beforeOid": sha,
						"afterOid":  githubZeroOID,
						"force":     false,
					}},
				},
			},
		},
		out:       &answer,
		operation: "DeleteBranchAt",
	})
	if status := forgeStatusOf(postErr); status == http.StatusUnauthorized || status == http.StatusForbidden {
		return postErr
	}

	current, err := g.GetBranch(ctx, repo, name)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil
	case err != nil:
		return err
	case current.SHA != sha:
		return fmt.Errorf("%s %s: %w", repo, name, ErrBranchMoved)
	}
	got := "branch present at the expected tip"
	switch {
	case postErr != nil:
		got += ": " + postErr.Error()
	case len(answer.Errors) > 0:
		got += ": " + answer.Errors[0].Message
	}
	return &ForgeWriteNotAppliedError{
		Type:      ForgeGitHub,
		Operation: "DeleteBranchAt",
		Subject:   repo.String() + " " + name,
		Want:      "absent",
		Got:       got,
	}
}
