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
	"strings"
	"time"
)

// =============================================================================
// Approvals — the module's reviews
// =============================================================================

// gitlabApprovalState is the review state an approval is reported as: the
// verdict the gate reads as an approval.
const gitlabApprovalState = "APPROVED"

// gitlabApprovedNote is the body of the system note GitLab writes on a merge
// request when a user approves it.
const gitlabApprovedNote = "approved this merge request"

type gitlabApprovals struct {
	ApprovedBy []struct {
		User *gitlabUser `json:"user"`
	} `json:"approved_by"`
}

// gitlabVersion is one push to a merge request: the head it moved to and
// when.
type gitlabVersion struct {
	HeadCommitSHA string    `json:"head_commit_sha"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListReviews returns a merge request's approvals as reviews.
//
// GitLab records an approval against the merge request, not a commit, and
// its approvals answer says only who approved — not when. The time comes from
// the system note GitLab writes on each approval: a user's latest such note
// is the approval GitLab now holds for them, because withdrawing and
// approving again writes a new one. The commit comes from the request's
// versions: an approval covers the head the newest version created before it
// moved to. An approval with no approval note, or no version older than its
// note, covers no commit the adapter can name; it is returned with an empty
// HeadSHA, which the review gate refuses to count — failing closed rather
// than letting an approval of an older head approve a newer one.
func (g *GitLabBackend) ListReviews(ctx context.Context, repo RepoRef, number int) ([]ForgeReview, error) {
	var approvals gitlabApprovals
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      gitlabMergeRequestPath(repo, number) + "/approvals",
		out:       &approvals,
		operation: "ListReviews",
	}); err != nil {
		return nil, err
	}
	if len(approvals.ApprovedBy) == 0 {
		return nil, nil
	}

	versions, err := g.mergeRequestVersions(ctx, repo, number)
	if err != nil {
		return nil, err
	}
	approvedAt, err := g.approvalTimes(ctx, repo, number)
	if err != nil {
		return nil, err
	}

	var reviews []ForgeReview
	for _, approval := range approvals.ApprovedBy {
		if approval.User == nil {
			continue
		}
		review := ForgeReview{
			ID:       approval.User.ID,
			Reviewer: approval.User.Username,
			State:    gitlabApprovalState,
		}
		if at, ok := approvedAt[approval.User.ID]; ok {
			review.Submitted = at
			review.HeadSHA = headAt(versions, at)
		}
		reviews = append(reviews, review)
	}
	return reviews, nil
}

// approvalTimes returns when each user last approved a merge request, keyed
// by user ID, from the system notes GitLab writes on approval. A note a user
// wrote is never GitLab's record, whatever it says.
func (g *GitLabBackend) approvalTimes(ctx context.Context, repo RepoRef, number int) (map[int64]time.Time, error) {
	notes, err := g.listNotes(ctx, "ListReviews", gitlabMergeRequestPath(repo, number), errReviewPagesExhausted)
	if err != nil {
		return nil, err
	}
	times := make(map[int64]time.Time)
	for _, note := range notes {
		if !note.System || note.Author == nil || strings.TrimSpace(note.Body) != gitlabApprovedNote {
			continue
		}
		if note.CreatedAt.After(times[note.Author.ID]) {
			times[note.Author.ID] = note.CreatedAt
		}
	}
	return times, nil
}

// mergeRequestVersions reads every version of a merge request.
func (g *GitLabBackend) mergeRequestVersions(ctx context.Context, repo RepoRef, number int) ([]gitlabVersion, error) {
	var versions []gitlabVersion
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListReviews",
		path:      gitlabMergeRequestPath(repo, number) + "/versions",
		maxPages:  maxIssuePages,
		exhausted: backendError("ListReviews", errReviewPagesExhausted),
	}, func(page []gitlabVersion) bool {
		versions = append(versions, page...)
		return true
	})
	if err != nil {
		return nil, err
	}
	return versions, nil
}

// headAt returns the head the newest version created before t moved to, or
// "" when no version is that old. A version created at t itself is not
// counted: which of the two heads an approval at that instant saw is unknown,
// so it is bound to the older one, which the gate then refuses for the newer.
func headAt(versions []gitlabVersion, t time.Time) string {
	var head string
	var newest time.Time
	for _, version := range versions {
		if !version.CreatedAt.Before(t) || version.HeadCommitSHA == "" {
			continue
		}
		if head == "" || version.CreatedAt.After(newest) {
			head, newest = version.HeadCommitSHA, version.CreatedAt
		}
	}
	return head
}

// SubmitReview refuses: GitLab cannot record a request-changes verdict, and
// a reviewer that can only approve cannot reject. See Capabilities.
func (g *GitLabBackend) SubmitReview(_ context.Context, _ RepoRef, _ int, submission ReviewSubmission) (*ForgeReview, error) {
	if err := submission.Validate(); err != nil {
		return nil, err
	}
	return nil, &ForgeUnsupportedError{
		Type:       ForgeGitLab,
		Capability: "submitting a review (GitLab records approvals but no request-changes verdict)",
	}
}

// =============================================================================
// Pipeline jobs and commit statuses — the module's check runs
// =============================================================================

// gitlabCommitStatus is a commit status, and also a CI job: GitLab keeps a
// job as a commit status under the same id, so the two listings overlap.
type gitlabCommitStatus struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	AllowFailure bool       `json:"allow_failure"`
	TargetURL    string     `json:"target_url"`
	WebURL       string     `json:"web_url"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	Commit       *struct {
		ID string `json:"id"`
	} `json:"commit"`
}

type gitlabPipeline struct {
	ID int64 `json:"id"`
}

// gitlabFinishedStates are the job and status states that will not change
// again without a retry.
var gitlabFinishedStates = map[string][2]string{
	"success":  {"completed", "success"},
	"failed":   {"completed", "failure"},
	"canceled": {"completed", "cancelled"},
	"skipped":  {"completed", "skipped"},
}

// checkState maps a GitLab state to the module's status and conclusion.
//
// A failure GitLab allows is a skip: the pipeline passes over it, and so
// must the gate. A manual job is skipped while nothing requires it, and
// pending while it blocks the pipeline. Every other state is still running.
func (s *gitlabCommitStatus) checkState() [2]string {
	state := strings.ToLower(s.Status)
	switch {
	case state == "failed" && s.AllowFailure:
		return [2]string{"completed", "skipped"}
	case state == "manual" && s.AllowFailure:
		return [2]string{"completed", "skipped"}
	case state == "running":
		return [2]string{"in_progress", ""}
	}
	if finished, ok := gitlabFinishedStates[state]; ok {
		return finished
	}
	return [2]string{"queued", ""}
}

// toForgeCheckRun converts GitLab's shape to the module's under id.
func (s *gitlabCommitStatus) toForgeCheckRun(id, sha string) ForgeCheckRun {
	state := s.checkState()
	run := ForgeCheckRun{
		ID:         id,
		Name:       s.Name,
		HeadSHA:    sha,
		Status:     state[0],
		Conclusion: state[1],
		URL:        firstNonEmpty(s.WebURL, s.TargetURL),
		Started:    s.CreatedAt,
	}
	if s.Commit != nil && s.Commit.ID != "" {
		run.HeadSHA = s.Commit.ID
	}
	if s.StartedAt != nil {
		run.Started = *s.StartedAt
	}
	if s.FinishedAt != nil {
		run.Completed = *s.FinishedAt
	}
	return run
}

// ListCheckRuns returns the CI runs against one commit: the jobs of every
// pipeline for it AND the commit statuses other publishers wrote.
//
// A job's id is its check run's id, which the job routes take. A commit
// status that is not a job — this module's own verdict among them — has no
// job behind it and carries the status prefix, so a job verb refuses it. A
// commit with no pipeline has no jobs; that is an empty list, which the gate
// reads as checks not yet reported.
func (g *GitLabBackend) ListCheckRuns(ctx context.Context, repo RepoRef, sha string) ([]ForgeCheckRun, error) {
	if sha == "" {
		return nil, &InvalidInputError{Field: "sha", Message: "check runs are read against a commit"}
	}
	project := gitlabProjectPath(repo)
	exhausted := backendError("ListCheckRuns", errCheckRunPagesExhausted)

	var pipelines []gitlabPipeline
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListCheckRuns",
		path:      project + "/pipelines",
		query:     url.Values{"sha": {sha}},
		maxPages:  maxIssuePages,
		exhausted: exhausted,
	}, func(page []gitlabPipeline) bool {
		pipelines = append(pipelines, page...)
		return true
	})
	if err != nil {
		return nil, err
	}

	var runs []ForgeCheckRun
	jobs := map[int64]bool{}
	for _, pipeline := range pipelines {
		err := forgePages(ctx, &g.forgeHTTP, forgeListing{
			operation: "ListCheckRuns",
			path:      project + "/pipelines/" + gitlabID(pipeline.ID) + "/jobs",
			maxPages:  maxIssuePages,
			exhausted: exhausted,
		}, func(page []gitlabCommitStatus) bool {
			for i := range page {
				jobs[page[i].ID] = true
				runs = append(runs, page[i].toForgeCheckRun(gitlabID(page[i].ID), sha))
			}
			return true
		})
		if err != nil {
			return nil, err
		}
	}

	err = forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListCheckRuns",
		path:      project + "/repository/commits/" + url.PathEscape(sha) + "/statuses",
		maxPages:  maxIssuePages,
		exhausted: exhausted,
	}, func(page []gitlabCommitStatus) bool {
		for i := range page {
			if jobs[page[i].ID] {
				continue
			}
			runs = append(runs, page[i].toForgeCheckRun(statusRunPrefix+gitlabID(page[i].ID), sha))
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// gitlabCommitStates maps the statuses this module publishes to GitLab's.
// Skipped has no writable GitLab state.
var gitlabCommitStates = map[CheckStatus]string{
	CheckPending:   "pending",
	CheckSuccess:   "success",
	CheckFailure:   "failed",
	CheckCancelled: "canceled",
}

// SetCommitStatus writes a named status against one commit and verifies it by
// reading the commit's checks back through the same conversion ListCheckRuns
// uses.
func (g *GitLabBackend) SetCommitStatus(ctx context.Context, repo RepoRef, sha string, status CommitStatus) error {
	if strings.TrimSpace(sha) == "" {
		return &InvalidInputError{Field: "sha", Message: "a status is written against a commit"}
	}
	if err := status.Validate(); err != nil {
		return err
	}
	state, ok := gitlabCommitStates[status.Status]
	if !ok {
		return &ForgeUnsupportedError{
			Type:       ForgeGitLab,
			Capability: "publishing a commit status of " + status.Status.String(),
		}
	}

	body := map[string]string{"state": state, "name": status.Context}
	if status.Description != "" {
		body["description"] = status.Description
	}
	if status.TargetURL != "" {
		body["target_url"] = status.TargetURL
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      gitlabProjectPath(repo) + "/statuses/" + url.PathEscape(sha),
		body:      body,
		operation: "SetCommitStatus",
	}); err != nil {
		return err
	}

	runs, err := g.ListCheckRuns(ctx, repo, sha)
	if err != nil {
		return err
	}
	return verifyCommitStatus(ForgeGitLab, repo, sha, status, runs)
}

// =============================================================================
// Job control
// =============================================================================

// gitlabJobPath renders the API path of one CI job.
func gitlabJobPath(repo RepoRef, id string) string {
	return gitlabProjectPath(repo) + "/jobs/" + id
}

// job reads the CI job behind check run id, refusing an id that names no job
// of this repository, or a job that ran against another commit than headSHA
// when one is given.
func (g *GitLabBackend) job(ctx context.Context, repo RepoRef, id, what, headSHA string) (*gitlabCommitStatus, error) {
	unsupported := func(why string) error {
		return &ForgeUnsupportedError{Type: ForgeGitLab, Capability: what + " (" + why + ")"}
	}
	if !isDecimal(id) {
		return nil, unsupported("it is not a CI job, and only CI jobs have this route")
	}

	var job gitlabCommitStatus
	err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      gitlabJobPath(repo, id),
		out:       &job,
		operation: "gitlab.job",
	})
	if errors.Is(err, ErrNotFound) {
		return nil, unsupported("it is not a CI job on this project")
	}
	if err != nil {
		return nil, err
	}
	if gitlabID(job.ID) != id {
		return nil, unsupported("the job GitLab returned is not job " + id)
	}
	if headSHA != "" {
		ran := ""
		if job.Commit != nil {
			ran = job.Commit.ID
		}
		if !strings.EqualFold(ran, headSHA) {
			return nil, unsupported("the job ran against " + shortSHA(ran) + ", not " + shortSHA(headSHA))
		}
	}
	return &job, nil
}

// CancelCheckRun cancels a CI job and waits, within the transport's read
// attempts, for GitLab to report it finished.
func (g *GitLabBackend) CancelCheckRun(ctx context.Context, repo RepoRef, id string) error {
	what := "cancelling check run " + id
	if _, err := g.job(ctx, repo, id, what, ""); err != nil {
		return err
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      gitlabJobPath(repo, id) + "/cancel",
		operation: "CancelCheckRun",
	}); err != nil {
		return err
	}

	var observed string
	for attempt := 1; attempt <= g.transport.ReadAttempts; attempt++ {
		job, err := g.job(ctx, repo, id, what, "")
		if err != nil {
			return err
		}
		if _, finished := gitlabFinishedStates[strings.ToLower(job.Status)]; finished {
			return nil
		}
		observed = "status " + job.Status
		if attempt < g.transport.ReadAttempts {
			if err := g.waiter.Wait(ctx, attempt); err != nil {
				return err
			}
		}
	}
	return &ForgeWriteUnconfirmedError{
		Type:      ForgeGitLab,
		Operation: "CancelCheckRun",
		Subject:   repo.String() + " check run " + id,
		Observed:  observed,
	}
}

// RerunCheckRun retries a CI job. GitLab answers with the new job, which must
// be a different job against the same commit.
func (g *GitLabBackend) RerunCheckRun(ctx context.Context, repo RepoRef, run ForgeCheckRun) error {
	what := "re-running check run " + run.Name
	if run.HeadSHA == "" {
		return &ForgeUnsupportedError{
			Type:       ForgeGitLab,
			Capability: what + " (nothing records which commit it covers)",
		}
	}
	if _, err := g.job(ctx, repo, run.ID, what, run.HeadSHA); err != nil {
		return err
	}

	var retried gitlabCommitStatus
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      gitlabJobPath(repo, run.ID) + "/retry",
		out:       &retried,
		operation: "RerunCheckRun",
	}); err != nil {
		return err
	}
	ran := ""
	if retried.Commit != nil {
		ran = retried.Commit.ID
	}
	if retried.ID != 0 && gitlabID(retried.ID) != run.ID && strings.EqualFold(ran, run.HeadSHA) {
		return nil
	}
	return &ForgeWriteNotAppliedError{
		Type:      ForgeGitLab,
		Operation: "RerunCheckRun",
		Subject:   repo.String() + "@" + shortSHA(run.HeadSHA) + " " + run.Name,
		Want:      "a new job against " + shortSHA(run.HeadSHA),
		Got:       "job " + gitlabID(retried.ID) + " against " + shortSHA(ran),
	}
}

// CheckRunLogs returns the last maxBytes of a CI job's trace.
func (g *GitLabBackend) CheckRunLogs(ctx context.Context, repo RepoRef, run ForgeCheckRun, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		return "", &InvalidInputError{
			Field:   "max_bytes",
			Message: "a log excerpt must state how much of it is wanted",
		}
	}
	what := "reading logs for check run " + run.Name
	if run.HeadSHA == "" {
		return "", &ForgeUnsupportedError{
			Type:       ForgeGitLab,
			Capability: what + " (nothing records which commit it covers)",
		}
	}
	if _, err := g.job(ctx, repo, run.ID, what, run.HeadSHA); err != nil {
		return "", err
	}

	var body []byte
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      gitlabJobPath(repo, run.ID) + "/trace",
		raw:       &body,
		maxBody:   maxBytes,
		operation: "CheckRunLogs",
	}); err != nil {
		return "", err
	}
	return string(body), nil
}
