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
	"strconv"
	"strings"
)

// =============================================================================
// Gitea write reconciliation
// =============================================================================
//
// The two writes giteaWritePolicies marks recoverReconcile. Neither is ever
// sent again on the strength of a timeout alone: the forge's state is read
// first, and only a read that proves the write did NOT land permits another.

// mergeLanded reports whether a pull request is merged with a merge commit to
// show for it — the whole of what a merge request asks for.
func mergeLanded(pull *ForgePullRequest) bool {
	return pull != nil && pull.Merged && pull.MergeSHA != ""
}

// giteaInternalError reports whether err is Gitea's own HTTP 500.
//
// A 500 IS AN ANSWER THAT SAYS NOTHING ABOUT A WRITE. Gitea has answered 500
// for a squash merge it had already landed in the same second
// (go-quicbaas#1071), so a merge that ends on one is settled by reading the
// pull request back, exactly as a merge nobody answered is, rather than
// charged to the work as a refusal.
func giteaInternalError(err error) bool {
	var apiErr *GiteaAPIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusInternalServerError
}

// reconcileMerge settles a merge the forge did not answer by reading the pull
// request back.
//
//	merged, with a merge commit  success: the forge finished what it was asked
//	open and mergeable           not ready, so the caller's bounded merge retry
//	                             decides; its next attempt re-reads first and
//	                             stops at "already merged" if the forge got there
//	anything else, or unreadable unconfirmed: nothing a read shows proves the
//	                             merge either happened or can safely be re-sent
//
// Every error it returns keeps the unanswered call as its cause, so a job that
// ends on one is attributed to the forge rather than charged to the work.
func (g *GiteaBackend) reconcileMerge(ctx context.Context, repo RepoRef, number int, unanswered error) error {
	subject := repo.String() + "#" + strconv.Itoa(number)
	pull, err := g.GetPullRequest(ctx, repo, number)
	if err != nil {
		return &ForgeWriteUnconfirmedError{
			Type: ForgeGitea, Operation: "MergePullRequest", Subject: subject,
			Observed: "the pull request could not be read back: " + err.Error(),
			Cause:    unanswered,
		}
	}
	if mergeLanded(pull) {
		return nil
	}
	if pull.State == "open" && pull.Mergeable {
		return &ForgeNotReadyError{
			Type: ForgeGitea, Operation: "MergePullRequest", Subject: subject,
			Detail: "the merge went unanswered and the pull request is still open",
			Cause:  unanswered,
		}
	}
	return &ForgeWriteUnconfirmedError{
		Type: ForgeGitea, Operation: "MergePullRequest", Subject: subject,
		Observed: "state " + pull.State + ", merged " + strconv.FormatBool(pull.Merged) +
			", mergeable " + strconv.FormatBool(pull.Mergeable),
		Cause: unanswered,
	}
}

// openPullRequest returns the number of the open pull request for spec's
// head -> base, creating it only when a read has just shown there is none.
//
// THE LOOKUP COMES FIRST, EVERY TIME. Before the first create, because an
// earlier attempt's create may have landed after its caller stopped waiting.
// After an unanswered create, because this one may have. After Gitea's 409,
// because that IS Gitea saying one exists. A create is re-sent only after an
// unanswered one AND a lookup that found nothing, bounded by the read retry
// budget; a create the forge refused for any other reason is final.
func (g *GiteaBackend) openPullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec, title string) (int, error) {
	subject := repo.String() + " " + spec.HeadRef + " -> " + spec.BaseRef
	var lastUnanswered error
	for attempt := 1; ; attempt++ {
		if lastUnanswered != nil {
			// Give a create the forge may still be finishing time to show.
			if err := g.waiter.Wait(ctx, attempt-1); err != nil {
				return 0, err
			}
		}
		existing, err := g.findOpenPullRequest(ctx, repo, spec.HeadRef, spec.BaseRef)
		switch {
		case err != nil && lastUnanswered != nil:
			return 0, &ForgeWriteUnconfirmedError{
				Type: ForgeGitea, Operation: "CreatePullRequest", Subject: subject,
				Observed: "the open pull requests could not be read: " + err.Error(),
				Cause:    lastUnanswered,
			}
		case err != nil:
			return 0, err
		case existing != nil:
			return existing.Number, nil
		case attempt > g.transport.ReadAttempts:
			return 0, &ForgeWriteUnconfirmedError{
				Type: ForgeGitea, Operation: "CreatePullRequest", Subject: subject,
				Observed: "no open pull request for these refs after " + strconv.Itoa(attempt-1) + " unanswered create(s)",
				Cause:    lastUnanswered,
			}
		}

		number, err := g.postPullRequest(ctx, repo, spec, title)
		switch {
		case err == nil:
			return number, nil
		case errors.Is(err, ErrForgeUnanswered):
			lastUnanswered = err
		case giteaStatusOf(err) == http.StatusConflict:
			return g.adoptAfterConflict(ctx, repo, spec, err)
		default:
			return 0, err
		}
	}
}

// adoptAfterConflict answers Gitea's 409 on a create: the pull request the
// forge says already exists is found and adopted. A conflict with no open pull
// request for these refs is not the duplicate case, and the forge's refusal is
// returned as it was.
func (g *GiteaBackend) adoptAfterConflict(ctx context.Context, repo RepoRef, spec PullRequestSpec, conflict error) (int, error) {
	existing, err := g.findOpenPullRequest(ctx, repo, spec.HeadRef, spec.BaseRef)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		return 0, conflict
	}
	return existing.Number, nil
}

// postPullRequest sends one create and returns the number the forge assigned.
func (g *GiteaBackend) postPullRequest(ctx context.Context, repo RepoRef, spec PullRequestSpec, title string) (int, error) {
	var created giteaPull
	if err := g.do(ctx, giteaRequest{
		method: http.MethodPost,
		path:   repoPath(repo) + "/pulls",
		body: map[string]any{
			"title": title,
			"body":  spec.Body,
			"head":  spec.HeadRef,
			"base":  spec.BaseRef,
		},
		out:       &created,
		operation: "CreatePullRequest",
	}); err != nil {
		return 0, err
	}
	if created.Number == 0 {
		return 0, &ForgeWriteNotAppliedError{
			Type:      ForgeGitea,
			Operation: "CreatePullRequest",
			Subject:   repo.String() + " " + spec.HeadRef + " -> " + spec.BaseRef,
			Want:      "a pull request number",
			Got:       "none",
		}
	}
	return created.Number, nil
}

// findOpenPullRequest returns the open pull request from head — a branch OF
// repo itself — into base, or nil when there is none.
//
// THE SOURCE REPOSITORY IS PART OF THE MATCH. A fork can carry a branch with
// the same name, and its pull request into the same base is a different
// change: adopting it would record, and later merge, somebody else's work
// under this task's review (the back-merge path merges an adopted pull
// request at its own head, so no head guard would catch it). Gitea's own
// duplicate check keys on both repositories for the same reason. A pull
// request whose head repository the forge does not report — a deleted fork —
// is never adopted.
func (g *GiteaBackend) findOpenPullRequest(ctx context.Context, repo RepoRef, head, base string) (*ForgePullRequest, error) {
	pulls, err := g.ListOpenPullRequests(ctx, repo)
	if err != nil {
		return nil, err
	}
	for i := range pulls {
		pull := &pulls[i]
		if pull.HeadRef == head && pull.BaseRef == base && sameRepo(pull.HeadRepo, repo) {
			return pull, nil
		}
	}
	return nil, nil
}

// sameRepo compares two repository references the way the forge does: owner
// and repository names are case-insensitive on Gitea.
func sameRepo(a, b RepoRef) bool {
	return a.Owner != "" && a.Repo != "" &&
		strings.EqualFold(a.Owner, b.Owner) && strings.EqualFold(a.Repo, b.Repo)
}

// giteaStatusOf is the HTTP status Gitea answered with, zero when the error
// carries none.
func giteaStatusOf(err error) int {
	var apiErr *GiteaAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}
