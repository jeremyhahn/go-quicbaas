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
// Forge write reconciliation
// =============================================================================
//
// The two writes forgeWritePolicies marks recoverReconcile. Neither is ever
// sent again on the strength of a timeout alone: the forge's state is read
// first, and only a read that proves the write did NOT land permits another.
// Every REST adapter settles them the same way; only the create itself and the
// status a forge refuses a duplicate with differ.

// forgePullReader is what reconciliation reads the forge's pull requests
// through: the adapter's own read methods, so a reconcile read takes the same
// mapping and paging as any other.
type forgePullReader interface {
	GetPullRequest(ctx context.Context, repo RepoRef, number int) (*ForgePullRequest, error)
	ListOpenPullRequests(ctx context.Context, repo RepoRef) ([]ForgePullRequest, error)
}

// mergeLanded reports whether a pull request is merged with a merge commit to
// show for it — the whole of what a merge request asks for.
func mergeLanded(pull *ForgePullRequest) bool {
	return pull != nil && pull.Merged && pull.MergeSHA != ""
}

// forgeInternalError reports whether err is the forge's own HTTP 500.
//
// A 500 IS AN ANSWER THAT SAYS NOTHING ABOUT A WRITE. Gitea has answered 500
// for a squash merge it had already landed in the same second
// (go-quicbaas#1071), so a merge that ends on one is settled by reading the
// pull request back, exactly as a merge nobody answered is, rather than
// charged to the work as a refusal.
func forgeInternalError(err error) bool {
	return forgeStatusOf(err) == http.StatusInternalServerError
}

// forgeStatusOf is the HTTP status the forge answered with, zero when the
// error carries none.
func forgeStatusOf(err error) int {
	var apiErr *ForgeAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
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
func (h *forgeHTTP) reconcileMerge(ctx context.Context, pulls forgePullReader, repo RepoRef, number int, unanswered error) error {
	subject := repo.String() + "#" + strconv.Itoa(number)
	pull, err := pulls.GetPullRequest(ctx, repo, number)
	if err != nil {
		return &ForgeWriteUnconfirmedError{
			Type: h.forge, Operation: "MergePullRequest", Subject: subject,
			Observed: "the pull request could not be read back: " + err.Error(),
			Cause:    unanswered,
		}
	}
	if mergeLanded(pull) {
		return nil
	}
	if pull.State == "open" && pull.Mergeable {
		return &ForgeNotReadyError{
			Type: h.forge, Operation: "MergePullRequest", Subject: subject,
			Detail: "the merge went unanswered and the pull request is still open",
			Cause:  unanswered,
		}
	}
	return &ForgeWriteUnconfirmedError{
		Type: h.forge, Operation: "MergePullRequest", Subject: subject,
		Observed: "state " + pull.State + ", merged " + strconv.FormatBool(pull.Merged) +
			", mergeable " + strconv.FormatBool(pull.Mergeable),
		Cause: unanswered,
	}
}

// headMoved names both commits in the refusal a forge's sha guard produced on
// a merge. The pull request is re-read for the current head; if it cannot be,
// the refusal still stands, naming only the approved one.
func (h *forgeHTTP) headMoved(ctx context.Context, pulls forgePullReader, repo RepoRef, number int, expectedHead string) error {
	moved := &HeadMovedError{
		Subject:  repo.String() + "#" + strconv.Itoa(number),
		Reviewed: shortSHA(expectedHead),
	}
	if pull, err := pulls.GetPullRequest(ctx, repo, number); err == nil {
		moved.Current = shortSHA(pull.HeadSHA)
	}
	return moved
}

// forgeBranches is what merge cleanup reads and deletes branches through: the
// adapter's own branch methods, so the delete is verified like any other.
type forgeBranches interface {
	GetBranch(ctx context.Context, repo RepoRef, name string) (*ForgeBranch, error)
	DeleteBranch(ctx context.Context, repo RepoRef, name string) error
}

// deleteMergedSource deletes a merged pull request's source branch when the
// merge spec asks for it, for a forge whose merge did not — or may not have —
// done so itself.
//
// Only a branch of this repository that still points at the merged head is
// deleted: a fork's branch is not this repository's to delete, and a branch
// somebody pushed to after the merge carries work nobody has merged.
func deleteMergedSource(ctx context.Context, branches forgeBranches, repo RepoRef, pull *ForgePullRequest, spec MergeSpec) error {
	if !spec.ExpectSourceDeleted || !sameRepo(pull.HeadRepo, repo) {
		return nil
	}
	branch, err := branches.GetBranch(ctx, repo, pull.HeadRef)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if branch.SHA != pull.HeadSHA {
		return nil
	}
	return branches.DeleteBranch(ctx, repo, pull.HeadRef)
}

// verifyCommitStatus reads back a commit status write: the newest status
// recorded under the written context must carry the written state.
//
// Statuses only: a check run sharing the context's name is a different
// publisher's answer and says nothing about whether this write landed.
func verifyCommitStatus(forge ForgeType, repo RepoRef, sha string, status CommitStatus, runs []ForgeCheckRun) error {
	var statuses []ForgeCheckRun
	for _, run := range runs {
		if strings.HasPrefix(run.ID, statusRunPrefix) {
			statuses = append(statuses, run)
		}
	}
	subject := repo.String() + "@" + shortSHA(sha) + " " + status.Context
	for _, run := range latestCheckPerContext(statuses) {
		if run.Name != status.Context {
			continue
		}
		if got := checkStatusFor(run); got != status.Status {
			return &ForgeWriteNotAppliedError{
				Type:      forge,
				Operation: "SetCommitStatus",
				Subject:   subject,
				Want:      status.Status.String(),
				Got:       got.String(),
			}
		}
		return nil
	}
	return &ForgeWriteNotAppliedError{
		Type:      forge,
		Operation: "SetCommitStatus",
		Subject:   subject,
		Want:      status.Status.String(),
		Got:       "no status with that name is recorded on the commit",
	}
}

// openPullRequest returns the number of the open pull request for spec's
// head -> base, creating it with post only when a read has just shown there is
// none.
//
// THE LOOKUP COMES FIRST, EVERY TIME. Before the first create, because an
// earlier attempt's create may have landed after its caller stopped waiting.
// After an unanswered create, because this one may have. After the forge's
// duplicate refusal (the dialect's duplicatePullStatus), because that IS the
// forge saying one exists. A create is re-sent only after an unanswered one
// AND a lookup that found nothing, bounded by the read retry budget; a create
// the forge refused for any other reason is final.
func (h *forgeHTTP) openPullRequest(ctx context.Context, pulls forgePullReader, repo RepoRef, spec PullRequestSpec, post func(context.Context) (int, error)) (int, error) {
	subject := repo.String() + " " + spec.HeadRef + " -> " + spec.BaseRef
	var lastUnanswered error
	for attempt := 1; ; attempt++ {
		if lastUnanswered != nil {
			// Give a create the forge may still be finishing time to show.
			if err := h.waiter.Wait(ctx, attempt-1); err != nil {
				return 0, err
			}
		}
		existing, err := findOpenPullRequest(ctx, pulls, repo, spec.HeadRef, spec.BaseRef)
		switch {
		case err != nil && lastUnanswered != nil:
			return 0, &ForgeWriteUnconfirmedError{
				Type: h.forge, Operation: "CreatePullRequest", Subject: subject,
				Observed: "the open pull requests could not be read: " + err.Error(),
				Cause:    lastUnanswered,
			}
		case err != nil:
			return 0, err
		case existing != nil:
			return existing.Number, nil
		case attempt > h.transport.ReadAttempts:
			return 0, &ForgeWriteUnconfirmedError{
				Type: h.forge, Operation: "CreatePullRequest", Subject: subject,
				Observed: "no open pull request for these refs after " + strconv.Itoa(attempt-1) + " unanswered create(s)",
				Cause:    lastUnanswered,
			}
		}

		number, err := post(ctx)
		switch {
		case err == nil:
			return number, nil
		case errors.Is(err, ErrForgeUnanswered):
			lastUnanswered = err
		case forgeStatusOf(err) == h.dialect.duplicatePullStatus:
			return adoptAfterConflict(ctx, pulls, repo, spec, err)
		default:
			return 0, err
		}
	}
}

// adoptAfterConflict answers the forge's duplicate refusal on a create: the
// pull request the forge says already exists is found and adopted. A refusal
// with no open pull request for these refs is not the duplicate case, and the
// forge's refusal is returned as it was.
func adoptAfterConflict(ctx context.Context, pulls forgePullReader, repo RepoRef, spec PullRequestSpec, conflict error) (int, error) {
	existing, err := findOpenPullRequest(ctx, pulls, repo, spec.HeadRef, spec.BaseRef)
	if err != nil {
		return 0, err
	}
	if existing == nil {
		return 0, conflict
	}
	return existing.Number, nil
}

// findOpenPullRequest returns the open pull request from head — a branch OF
// repo itself — into base, or nil when there is none.
//
// THE SOURCE REPOSITORY IS PART OF THE MATCH. A fork can carry a branch with
// the same name, and its pull request into the same base is a different
// change: adopting it would record, and later merge, somebody else's work
// under this task's review (the back-merge path merges an adopted pull
// request at its own head, so no head guard would catch it). The forges' own
// duplicate checks key on both repositories for the same reason. A pull
// request whose head repository the forge does not report — a deleted fork —
// is never adopted.
func findOpenPullRequest(ctx context.Context, pulls forgePullReader, repo RepoRef, head, base string) (*ForgePullRequest, error) {
	open, err := pulls.ListOpenPullRequests(ctx, repo)
	if err != nil {
		return nil, err
	}
	for i := range open {
		pull := &open[i]
		if pull.HeadRef == head && pull.BaseRef == base && sameRepo(pull.HeadRepo, repo) {
			return pull, nil
		}
	}
	return nil, nil
}

// sameRepo compares two repository references the way the forges do: owner
// and repository names are case-insensitive on Gitea and GitHub alike.
func sameRepo(a, b RepoRef) bool {
	return a.Owner != "" && a.Repo != "" &&
		strings.EqualFold(a.Owner, b.Owner) && strings.EqualFold(a.Repo, b.Repo)
}
