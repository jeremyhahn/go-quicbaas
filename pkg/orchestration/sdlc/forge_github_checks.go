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
	"net/url"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// GitHub: reviews, commit statuses, check runs and Actions
// =============================================================================

// errCheckRunPagesExhausted reports a check-run or status listing that never
// ended. A fault rather than a truncation: the check that is red may be the
// one on the page that was not read.
var errCheckRunPagesExhausted = errors.New(
	"commit check listing never reached its last page")

// errActionsJobPagesExhausted reports a workflow run's job listing that never
// ended before the job a check run names was found.
var errActionsJobPagesExhausted = errors.New(
	"workflow run job listing never reached its last page")

// maxActionsJobPages bounds a workflow run's job listing. GitHub caps a
// workflow at 256 jobs, so 20 pages of 100 cover several attempts of the
// largest one.
const maxActionsJobPages = 20

// =============================================================================
// Reviews
// =============================================================================

type githubReview struct {
	ID          int64       `json:"id"`
	User        *githubUser `json:"user"`
	State       string      `json:"state"`
	Body        string      `json:"body"`
	CommitID    string      `json:"commit_id"`
	SubmittedAt time.Time   `json:"submitted_at"`
}

// githubReviewStateNames maps GitHub's stored review states onto the names
// the gate reads (gate.go). Only the two that differ are listed; every other
// state, APPROVED included, is already the gate's own name.
var githubReviewStateNames = map[string]string{
	"CHANGES_REQUESTED": "REQUEST_CHANGES",
	"COMMENTED":         "COMMENT",
}

// toForgeReview converts GitHub's shape to the module's, renaming the state
// so a GitHub rejection reads as the same verdict a Gitea one does.
func (r *githubReview) toForgeReview() ForgeReview {
	state := strings.ToUpper(r.State)
	if name, ok := githubReviewStateNames[state]; ok {
		state = name
	}
	review := ForgeReview{
		ID:        r.ID,
		State:     state,
		Body:      r.Body,
		HeadSHA:   r.CommitID,
		Submitted: r.SubmittedAt,
	}
	if r.User != nil {
		review.Reviewer = r.User.Login
	}
	return review
}

// ListReviews returns every review on one pull request, oldest first.
//
// commit_id is carried through as HeadSHA for the reason the Gitea adapter
// gives: the gate binds a verdict to the tree it was given.
func (g *GitHubBackend) ListReviews(ctx context.Context, repo RepoRef, number int) ([]ForgeReview, error) {
	var reviews []ForgeReview
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListReviews",
		path:      pullPath(repo, number) + "/reviews",
		maxPages:  maxIssuePages,
		exhausted: backendError("ListReviews", errReviewPagesExhausted),
	}, func(page []githubReview) bool {
		for i := range page {
			reviews = append(reviews, page[i].toForgeReview())
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return reviews, nil
}

// githubReviewEvents maps the module's verdict onto GitHub's review event. A
// verdict absent from it is refused rather than approximated.
var githubReviewEvents = map[ReviewVerdict]string{
	VerdictApprove:        "APPROVE",
	VerdictRequestChanges: "REQUEST_CHANGES",
	VerdictComment:        "COMMENT",
}

// githubReviewStored names the state each event is stored as, after
// toForgeReview's renaming: what the read-back expects to find.
var githubReviewStored = map[ReviewVerdict]string{
	VerdictApprove:        "APPROVED",
	VerdictRequestChanges: "REQUEST_CHANGES",
	VerdictComment:        "COMMENT",
}

// SubmitReview posts a verdict on one pull request, bound to the head it was
// formed against, and verifies what GitHub stored. See the Gitea adapter for
// why the head is both sent and checked.
func (g *GitHubBackend) SubmitReview(ctx context.Context, repo RepoRef, number int, submission ReviewSubmission) (*ForgeReview, error) {
	if err := submission.Validate(); err != nil {
		return nil, err
	}
	event, ok := githubReviewEvents[submission.Verdict]
	if !ok {
		return nil, &ForgeUnsupportedError{
			Type:       ForgeGitHub,
			Capability: "submitting a " + string(submission.Verdict) + " review",
		}
	}
	want := githubReviewStored[submission.Verdict]

	var raw githubReview
	if err := g.do(ctx, forgeRequest{
		method: http.MethodPost,
		path:   pullPath(repo, number) + "/reviews",
		body: map[string]string{
			"event":     event,
			"body":      submission.Body,
			"commit_id": submission.HeadSHA,
		},
		out:       &raw,
		operation: "SubmitReview",
	}); err != nil {
		return nil, err
	}

	stored := raw.toForgeReview()
	if stored.ID == 0 || stored.State != want {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitHub,
			Operation: "SubmitReview",
			Subject:   fmt.Sprintf("%s#%d@%s", repo, number, shortSHA(submission.HeadSHA)),
			Want:      want,
			Got:       stored.State,
		}
	}
	if stored.HeadSHA == "" {
		stored.HeadSHA = submission.HeadSHA
	} else if !strings.EqualFold(stored.HeadSHA, submission.HeadSHA) {
		return nil, &ForgeWriteNotAppliedError{
			Type:      ForgeGitHub,
			Operation: "SubmitReview",
			Subject:   fmt.Sprintf("%s#%d", repo, number),
			Want:      shortSHA(submission.HeadSHA),
			Got:       shortSHA(stored.HeadSHA),
		}
	}
	return &stored, nil
}

// =============================================================================
// Check runs and commit statuses
// =============================================================================

type githubCheckRun struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	HeadSHA     string     `json:"head_sha"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	HTMLURL     string     `json:"html_url"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type githubStatus struct {
	ID          int64     `json:"id"`
	State       string    `json:"state"`
	Context     string    `json:"context"`
	Description string    `json:"description"`
	TargetURL   string    `json:"target_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// githubConclusions folds GitHub's check-run conclusions onto the four the
// gate reads (success, failure, cancelled, skipped). Absent ones pass through.
//
// Each fold keeps the gate's decision what GitHub's own branch protection
// decides: a run that timed out, wants an action, or never started has not
// passed; a stale run was superseded, not failed; a neutral one is
// informational and does not block.
var githubConclusions = map[string]string{
	"timed_out":       "failure",
	"action_required": "failure",
	"startup_failure": "failure",
	"stale":           "cancelled",
	"neutral":         "skipped",
}

// githubStatusRuns splits a commit-status state into the status and
// conclusion the module reasons about. GitHub's "error" is a failure; an
// unknown state is still pending rather than a state GitHub did not claim.
var githubStatusRuns = map[string][2]string{
	"pending": {"pending", ""},
	"success": {"completed", "success"},
	"failure": {"completed", "failure"},
	"error":   {"completed", "failure"},
}

// toForgeCheckRun converts GitHub's shape to the module's.
func (c *githubCheckRun) toForgeCheckRun(sha string) ForgeCheckRun {
	conclusion := strings.ToLower(c.Conclusion)
	if folded, ok := githubConclusions[conclusion]; ok {
		conclusion = folded
	}
	run := ForgeCheckRun{
		ID:         strconv.FormatInt(c.ID, 10),
		Name:       c.Name,
		HeadSHA:    c.HeadSHA,
		Status:     strings.ToLower(c.Status),
		Conclusion: conclusion,
		URL:        c.HTMLURL,
	}
	if run.HeadSHA == "" {
		run.HeadSHA = sha
	}
	if c.StartedAt != nil {
		run.Started = *c.StartedAt
	}
	if c.CompletedAt != nil {
		run.Completed = *c.CompletedAt
	}
	return run
}

// toForgeCheckRun converts one commit status to the module's check-run shape.
func (s *githubStatus) toForgeCheckRun(sha string) ForgeCheckRun {
	state, ok := githubStatusRuns[strings.ToLower(s.State)]
	if !ok {
		state = [2]string{"pending", ""}
	}
	return ForgeCheckRun{
		ID:         statusRunPrefix + strconv.FormatInt(s.ID, 10),
		Name:       s.Context,
		HeadSHA:    sha,
		Status:     state[0],
		Conclusion: state[1],
		URL:        s.TargetURL,
		Started:    s.CreatedAt,
		Completed:  s.UpdatedAt,
	}
}

// ListCheckRuns returns the CI runs against one commit: its check runs AND
// its commit statuses.
//
// BOTH, because GitHub keeps them apart and a project may publish either.
// Actions and every GitHub App write check runs; older CI and this module's
// own verdict write commit statuses. A gate that read one would never see a
// required check published through the other.
func (g *GitHubBackend) ListCheckRuns(ctx context.Context, repo RepoRef, sha string) ([]ForgeCheckRun, error) {
	if sha == "" {
		return nil, &InvalidInputError{Field: "sha", Message: "check runs are read against a commit"}
	}
	commit := repoPath(repo) + "/commits/" + url.PathEscape(sha)
	exhausted := backendError("ListCheckRuns", errCheckRunPagesExhausted)

	var runs []ForgeCheckRun
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListCheckRuns",
		path:      commit + "/check-runs",
		maxPages:  maxIssuePages,
		exhausted: exhausted,
	}, func(page struct {
		CheckRuns []githubCheckRun `json:"check_runs"`
	}) bool {
		for i := range page.CheckRuns {
			runs = append(runs, page.CheckRuns[i].toForgeCheckRun(sha))
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	err = forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "ListCheckRuns",
		path:      commit + "/statuses",
		maxPages:  maxIssuePages,
		exhausted: exhausted,
	}, func(page []githubStatus) bool {
		for i := range page {
			runs = append(runs, page[i].toForgeCheckRun(sha))
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return runs, nil
}

// githubCommitStates maps the module's CheckStatus onto GitHub's commit-status
// state. CheckCancelled is absent for the reason Gitea's table gives: GitHub
// publishes pending, success, failure and error, none of which means "never
// finished".
var githubCommitStates = map[CheckStatus]string{
	CheckPending: "pending",
	CheckSuccess: "success",
	CheckFailure: "failure",
}

// SetCommitStatus writes a named status against one commit and verifies it by
// reading the commit's checks back through the same conversion ListCheckRuns
// uses.
func (g *GitHubBackend) SetCommitStatus(ctx context.Context, repo RepoRef, sha string, status CommitStatus) error {
	if strings.TrimSpace(sha) == "" {
		return &InvalidInputError{Field: "sha", Message: "a status is written against a commit"}
	}
	if err := status.Validate(); err != nil {
		return err
	}
	state, ok := githubCommitStates[status.Status]
	if !ok {
		return &ForgeUnsupportedError{
			Type:       ForgeGitHub,
			Capability: "publishing a commit status of " + status.Status.String(),
		}
	}

	body := map[string]string{"state": state, "context": status.Context}
	if status.Description != "" {
		body["description"] = status.Description
	}
	if status.TargetURL != "" {
		body["target_url"] = status.TargetURL
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      repoPath(repo) + "/statuses/" + url.PathEscape(sha),
		body:      body,
		operation: "SetCommitStatus",
	}); err != nil {
		return err
	}

	runs, err := g.ListCheckRuns(ctx, repo, sha)
	if err != nil {
		return err
	}
	return verifyCommitStatus(ForgeGitHub, repo, sha, status, runs)
}

// =============================================================================
// GitHub Actions: cancel, rerun and logs
// =============================================================================
//
// Every Actions job publishes a check run, but a check-run id is not a job id:
// GitHub documents the two as separate resources and promises nothing about
// their numbers. A ForgeCheckRun from ListCheckRuns carries the check-run id,
// so the job behind it is resolved through GitHub's own data before any job
// route is called: the check run names its check suite, the workflow run
// that owns that suite is found by check_suite_id, and the job is the one in
// that run whose check_run_url names this check run. Cancel, rerun and logs
// then use the job's own id.
//
// A check run some other GitHub App published has no workflow run, and a
// commit status has neither, so both are refused per call with
// ForgeUnsupportedError — the capability flags hold for Actions and never
// pretend to hold for the rest. Every read is under THIS repository, and for
// a read or a rerun the job's head must be the commit the caller's run is
// about.

// githubActionsJob is the part of one Actions job this adapter reads.
type githubActionsJob struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"run_id"`
	RunAttempt  int    `json:"run_attempt"`
	HeadSHA     string `json:"head_sha"`
	Status      string `json:"status"`
	CheckRunURL string `json:"check_run_url"`
}

// githubActionsRun is the part of one workflow run this adapter reads.
type githubActionsRun struct {
	ID           int64  `json:"id"`
	RunAttempt   int    `json:"run_attempt"`
	Status       string `json:"status"`
	CheckSuiteID int64  `json:"check_suite_id"`
}

// githubCheckSuiteOf is the part of one check run that names its suite.
type githubCheckSuiteOf struct {
	CheckSuite *struct {
		ID int64 `json:"id"`
	} `json:"check_suite"`
}

// jobPath renders the API path of one Actions job.
func (j *githubActionsJob) jobPath(repo RepoRef) string {
	return repoPath(repo) + "/actions/jobs/" + strconv.FormatInt(j.ID, 10)
}

// runPath renders the API path of one workflow run.
func (j *githubActionsJob) runPath(repo RepoRef) string {
	return repoPath(repo) + "/actions/runs/" + strconv.FormatInt(j.RunID, 10)
}

// actionsJob returns the Actions job behind one check-run id, or
// ForgeUnsupportedError when the id is not an Actions job of this repository.
// what names the verb for that refusal; headSHA, when non-empty, must be the
// job's head.
func (g *GitHubBackend) actionsJob(ctx context.Context, repo RepoRef, id, what, headSHA string) (*githubActionsJob, error) {
	unsupported := func(why string) error {
		return &ForgeUnsupportedError{Type: ForgeGitHub, Capability: what + " (" + why + ")"}
	}
	if !isDecimal(id) {
		return nil, unsupported("it is not a check run, and only GitHub Actions jobs have this route")
	}

	suite, err := g.checkSuiteOf(ctx, repo, id)
	if errors.Is(err, ErrNotFound) {
		return nil, unsupported("it is not a check run on this repository")
	}
	if err != nil {
		return nil, err
	}
	if suite == 0 {
		return nil, unsupported("GitHub names no check suite for check run " + id)
	}

	runID, err := g.workflowRunOf(ctx, repo, suite)
	if err != nil {
		return nil, err
	}
	if runID == 0 {
		return nil, unsupported("no GitHub Actions workflow run published check run " + id)
	}

	job, err := g.jobOf(ctx, repo, runID, g.baseURL+repoPath(repo)+"/check-runs/"+id)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, unsupported("no job of workflow run " + strconv.FormatInt(runID, 10) + " is check run " + id)
	}
	if headSHA != "" && !strings.EqualFold(job.HeadSHA, headSHA) {
		return nil, unsupported("the job ran against " + shortSHA(job.HeadSHA) + ", not " + shortSHA(headSHA))
	}
	return job, nil
}

// checkSuiteOf returns the id of the check suite one check run belongs to,
// or zero when GitHub names none.
func (g *GitHubBackend) checkSuiteOf(ctx context.Context, repo RepoRef, id string) (int64, error) {
	var run githubCheckSuiteOf
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/check-runs/" + id,
		out:       &run,
		operation: "actionsJob",
	}); err != nil {
		return 0, err
	}
	if run.CheckSuite == nil {
		return 0, nil
	}
	return run.CheckSuite.ID, nil
}

// workflowRunOf returns the id of the workflow run that owns one check suite,
// or zero when none does: the suite then belongs to another GitHub App. The
// filter is re-checked on each run, so a run GitHub returned for another
// suite is never taken for this one.
func (g *GitHubBackend) workflowRunOf(ctx context.Context, repo RepoRef, suite int64) (int64, error) {
	var page struct {
		WorkflowRuns []githubActionsRun `json:"workflow_runs"`
	}
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      repoPath(repo) + "/actions/runs",
		query:     url.Values{"check_suite_id": {strconv.FormatInt(suite, 10)}},
		out:       &page,
		operation: "actionsJob",
	}); err != nil {
		return 0, err
	}
	for _, run := range page.WorkflowRuns {
		if run.CheckSuiteID == suite && run.ID != 0 {
			return run.ID, nil
		}
	}
	return 0, nil
}

// jobOf returns the job of one workflow run, across all its attempts, whose
// check_run_url is checkRunURL, or nil when none is.
func (g *GitHubBackend) jobOf(ctx context.Context, repo RepoRef, runID int64, checkRunURL string) (*githubActionsJob, error) {
	var found *githubActionsJob
	err := forgePages(ctx, &g.forgeHTTP, forgeListing{
		operation: "actionsJob",
		path:      repoPath(repo) + "/actions/runs/" + strconv.FormatInt(runID, 10) + "/jobs",
		query:     url.Values{"filter": {"all"}},
		maxPages:  maxActionsJobPages,
		exhausted: backendError("actionsJob", errActionsJobPagesExhausted),
	}, func(page struct {
		Jobs []githubActionsJob `json:"jobs"`
	}) bool {
		for i := range page.Jobs {
			job := page.Jobs[i]
			if job.ID != 0 && job.RunID == runID && strings.EqualFold(job.CheckRunURL, checkRunURL) {
				found = &job
				return false
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// actionsRun reads one workflow run.
func (g *GitHubBackend) actionsRun(ctx context.Context, repo RepoRef, job *githubActionsJob) (*githubActionsRun, error) {
	var run githubActionsRun
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      job.runPath(repo),
		out:       &run,
		operation: "actionsRun",
	}); err != nil {
		return nil, err
	}
	return &run, nil
}

// CancelCheckRun cancels the workflow run an Actions job belongs to and waits,
// bounded, for GitHub to report it finished.
//
// THE WHOLE RUN, because GitHub publishes no route that cancels one job. The
// caller is supersession, and a superseded head's run is wanted gone entire.
//
// GitHub accepts a cancel with 202 and applies it asynchronously, so the run
// is read back up to the transport's read-attempt bound. A run still going
// after that is ForgeWriteUnconfirmedError — the cancel may yet land — never
// nil, because a caller told a run had stopped stops tracking its capacity.
// A 409 is GitHub refusing to cancel a run that has already finished, which
// the same read-back confirms.
func (g *GitHubBackend) CancelCheckRun(ctx context.Context, repo RepoRef, id string) error {
	job, err := g.actionsJob(ctx, repo, id, "cancelling check run "+id, "")
	if err != nil {
		return err
	}
	err = g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      job.runPath(repo) + "/cancel",
		operation: "CancelCheckRun",
	})
	if err != nil && forgeStatusOf(err) != http.StatusConflict {
		return err
	}

	var observed string
	for attempt := 1; attempt <= g.transport.ReadAttempts; attempt++ {
		run, err := g.actionsRun(ctx, repo, job)
		if err != nil {
			return err
		}
		if strings.EqualFold(run.Status, "completed") {
			return nil
		}
		observed = "status " + run.Status
		if attempt < g.transport.ReadAttempts {
			if err := g.waiter.Wait(ctx, attempt); err != nil {
				return err
			}
		}
	}
	return &ForgeWriteUnconfirmedError{
		Type:      ForgeGitHub,
		Operation: "CancelCheckRun",
		Subject:   repo.String() + " check run " + id,
		Observed:  observed,
	}
}

// RerunCheckRun reruns one finished Actions job, verified by read-back.
//
// GitHub reruns a job as a new attempt of its workflow run, under a new job
// id, so the read-back is the RUN: a higher run_attempt, or a run that is no
// longer complete, proves the rerun took — the Gitea adapter gives why both
// are accepted.
func (g *GitHubBackend) RerunCheckRun(ctx context.Context, repo RepoRef, run ForgeCheckRun) error {
	what := "re-running check run " + run.Name
	if run.HeadSHA == "" {
		return &ForgeUnsupportedError{
			Type:       ForgeGitHub,
			Capability: what + " (nothing records which commit it covers)",
		}
	}
	job, err := g.actionsJob(ctx, repo, run.ID, what, run.HeadSHA)
	if err != nil {
		return err
	}

	if err := g.do(ctx, forgeRequest{
		method:    http.MethodPost,
		path:      job.jobPath(repo) + "/rerun",
		operation: "RerunCheckRun",
	}); err != nil {
		return err
	}

	after, err := g.actionsRun(ctx, repo, job)
	if err != nil {
		return err
	}
	if after.RunAttempt > job.RunAttempt || !strings.EqualFold(after.Status, "completed") {
		return nil
	}
	return &ForgeWriteNotAppliedError{
		Type:      ForgeGitHub,
		Operation: "RerunCheckRun",
		Subject:   repo.String() + "@" + shortSHA(run.HeadSHA) + " " + run.Name,
		Want:      "a restarted job",
		Got:       "attempt " + strconv.Itoa(after.RunAttempt) + " still " + after.Status,
	}
}

// CheckRunLogs returns the tail of one Actions job's log.
//
// GitHub answers the log route with a redirect to a short-lived download URL
// on another host. The HTTP client follows it and, crossing hosts, drops the
// Authorization header — the signed URL is its own credential — so the
// adapter's token never leaves for the storage host.
func (g *GitHubBackend) CheckRunLogs(ctx context.Context, repo RepoRef, run ForgeCheckRun, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		return "", &InvalidInputError{
			Field:   "max_bytes",
			Message: "a log excerpt must state how much of it is wanted",
		}
	}
	what := "reading logs for check run " + run.Name
	if run.HeadSHA == "" {
		return "", &ForgeUnsupportedError{
			Type:       ForgeGitHub,
			Capability: what + " (nothing records which commit it covers)",
		}
	}
	job, err := g.actionsJob(ctx, repo, run.ID, what, run.HeadSHA)
	if err != nil {
		return "", err
	}

	var body []byte
	if err := g.do(ctx, forgeRequest{
		method:    http.MethodGet,
		path:      job.jobPath(repo) + "/logs",
		raw:       &body,
		maxBody:   maxBytes,
		operation: "CheckRunLogs",
	}); err != nil {
		return "", err
	}
	return string(body), nil
}
