// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// Delivery — what one Task's attempt published
// =============================================================================
//
// A Delivery REFERENCES a Task by id and records the git side of one attempt
// at it: the branch, the head that was pushed, the pull request, and the
// reviews and checks the forge reported against that head. The Task itself —
// its status, attempts, budget, blocked reason — is orchestration's record
// and is never copied here.
//
// The pull request and the head are ALSO published as Task artifacts (kinds
// url and commit, names pull_request and head), so the generic UI shows them
// without a module client.

// PullRequestState is where a pull request stands on the forge.
type PullRequestState string

const (
	// PullRequestOpen is a pull request awaiting review or merge.
	PullRequestOpen PullRequestState = "open"

	// PullRequestMerged is a pull request that landed.
	PullRequestMerged PullRequestState = "merged"

	// PullRequestClosed is a pull request closed without merging.
	PullRequestClosed PullRequestState = "closed"
)

// String returns the state's name.
func (s PullRequestState) String() string { return string(s) }

// ReviewVerdict is what a reviewer said about one head.
type ReviewVerdict string

const (
	// VerdictApprove accepts the head.
	VerdictApprove ReviewVerdict = "approve"

	// VerdictRequestChanges refuses the head with findings.
	VerdictRequestChanges ReviewVerdict = "request_changes"

	// VerdictError is a review that could not be produced.
	VerdictError ReviewVerdict = "error"

	// VerdictComment is a review that says something without deciding
	// anything.
	//
	// IT IS A FIRST-CLASS VERDICT because the module has to be able to
	// POST one, not because the gate counts it: a reviewer that has read a
	// draft, or that has findings worth recording on a change it is not
	// the right judge of, must have a way to say so that is not an approval
	// and not a refusal. The gate treats it as what it is — neither — and
	// forgeReviewVerdicts deliberately declines to map a forge's COMMENT
	// onto any record at all, so a comment can neither clear a head nor
	// retract a standing refusal on one.
	VerdictComment ReviewVerdict = "comment"
)

// String returns the verdict's name.
func (v ReviewVerdict) String() string { return string(v) }

// CheckStatus is where a forge check run stands.
type CheckStatus string

const (
	// CheckPending is a check that has not finished.
	CheckPending CheckStatus = "pending"

	// CheckSuccess is a check that finished green.
	CheckSuccess CheckStatus = "success"

	// CheckFailure is a check that finished red.
	CheckFailure CheckStatus = "failure"

	// CheckCancelled is a check that was cancelled before it finished.
	CheckCancelled CheckStatus = "cancelled"
)

// String returns the status's name.
func (s CheckStatus) String() string { return string(s) }

// Finished reports whether the check reached a terminal status.
func (s CheckStatus) Finished() bool {
	switch s {
	case CheckSuccess, CheckFailure, CheckCancelled:
		return true
	default:
		return false
	}
}

// DeliveryPullRequest is the pull request an attempt opened.
type DeliveryPullRequest struct {
	Number int              `json:"number"`
	URL    string           `json:"url,omitempty"`
	State  PullRequestState `json:"state"`
	Title  string           `json:"title,omitempty"`
	Base   string           `json:"base,omitempty"`

	// Draft records that the AUTHOR has said the work is unfinished.
	//
	// It is separate from State rather than a fourth state because it is
	// orthogonal to open/merged/closed and because collapsing the two would
	// make "not open" mean two different things to the gate. What it is FOR
	// is the gate: a draft may not land, and it may not be approved, since
	// an approval on a draft contradicts the one person who has said the
	// change is not ready.
	Draft bool `json:"draft,omitempty"`
}

// Opened reports whether a pull request exists on the forge.
func (p DeliveryPullRequest) Opened() bool { return p.Number > 0 }

// ReviewRecord is one reviewer's verdict on one head.
type ReviewRecord struct {
	// Reviewer is the forge login.
	Reviewer string `json:"reviewer"`

	// ModelFamily is the family behind the reviewer, when the binding's
	// policy records one, for the independence rule.
	ModelFamily string `json:"model_family,omitempty"`

	Verdict ReviewVerdict `json:"verdict"`

	// HeadSHA is the head the verdict binds to; a verdict never carries over
	// to a later head.
	HeadSHA string `json:"head_sha"`

	// ExternalID is the forge's own id for the submission, so a re-read of
	// the forge never records the same submission twice.
	ExternalID string `json:"external_id,omitempty"`

	// Round is the job iteration the verdict was recorded in.
	Round int `json:"round"`

	// Findings is the reviewer's body, delivered to the worker on a
	// request-changes.
	Findings string `json:"findings,omitempty"`

	SubmittedAt time.Time `json:"submitted_at"`
}

// AppliesTo reports whether the verdict binds to headSHA.
func (r ReviewRecord) AppliesTo(headSHA string) bool {
	return r.HeadSHA != "" && r.HeadSHA == headSHA
}

// CheckRecord is the latest state of one check context on one head.
type CheckRecord struct {
	Name       string      `json:"name"`
	HeadSHA    string      `json:"head_sha"`
	Status     CheckStatus `json:"status"`
	ForgeRunID string      `json:"forge_run_id,omitempty"`
	FinishedAt time.Time   `json:"finished_at,omitempty"`
}

// CIRerunRecord is one automatic re-run of one check on one head.
type CIRerunRecord struct {
	// HeadSHA and Check identify what was re-run. The budget is per pair: a
	// head with six failing checks gets its own allowance for each, because
	// six checks dying on six runners is six faults, not one.
	HeadSHA string `json:"head_sha"`
	Check   string `json:"check"`

	// ForgeRunID is the run the engine asked to be restarted, for the audit
	// trail. Nothing decides from it — see Delivery.RerunPending for why the
	// decision is made on time instead.
	ForgeRunID string `json:"forge_run_id,omitempty"`

	// Signature is the pkg/cifault signature that earned the re-run.
	Signature string `json:"signature"`

	// At is when the engine asked.
	At time.Time `json:"at"`

	// Failure is why the ask did not land, empty when it did. A re-run the
	// forge refused still spends budget — otherwise a platform whose route is
	// broken would be asked forever — but it does not make the engine WAIT,
	// because nothing was restarted to wait for.
	Failure string `json:"failure,omitempty"`
}

// Delivery records the git side of one attempt at a Task.
type Delivery struct {
	// ID is the DAO entity id, derived from TaskID. Never set by callers.
	ID uint64 `json:"-"`

	// TaskID references the orchestration Task.
	TaskID string `json:"task_id"`

	// ProjectID references the orchestration Project, for listing by project.
	ProjectID string `json:"project_id"`

	// Attempt is the Task attempt this delivery belongs to.
	Attempt int `json:"attempt"`

	// Branch is the branch the attempt works on, Base the ref it was cut
	// from and Target the ref it lands on. Base and Target are separate
	// because a strategy may cut from one and target another — a gitflow
	// hotfix comes off the release line, a feature off develop.
	Branch string `json:"branch"`
	Base   string `json:"base"`
	Target string `json:"target"`

	// RequiresPullRequest records the branch plan's review decision, so the
	// merge reads the decision made when the branch was cut rather than
	// re-deriving it from labels that may have moved since.
	RequiresPullRequest bool `json:"requires_pull_request"`

	// DeleteAfterMerge records whether the branch goes once it has landed.
	DeleteAfterMerge bool `json:"delete_after_merge,omitempty"`

	// HeadSHA is the last head pushed; empty until Reviewable ran.
	HeadSHA string `json:"head_sha,omitempty"`

	// WorkDir is the directory INSIDE THE WORKER'S CONTAINER the attempt
	// worked in. The daemon never creates it and Prepare never sets it; it
	// is recorded so a human reading a delivery knows where the work
	// happened.
	//
	// The SDLC runner writes it when it spawns the container.
	WorkDir string `json:"work_dir,omitempty"`

	PullRequest DeliveryPullRequest `json:"pull_request"`
	Reviews     []ReviewRecord      `json:"reviews,omitempty"`
	Checks      []CheckRecord       `json:"checks,omitempty"`

	// CIReruns is every automatic re-run the engine asked the forge for, and
	// it is what BOUNDS them.
	//
	// A void red — a runner that died holding the job — is answered by running
	// the job again rather than by waking a coding agent, and that is only
	// safe while the asking stops. A permanently broken host answers void
	// every time, so the budget has to survive the daemon restarting mid-wait,
	// which means it lives on the record rather than in the validator's
	// memory. It is also the audit trail: an operator reading an escalation
	// sees what was tried, when, and against which signature.
	CIReruns []CIRerunRecord `json:"ci_reruns,omitempty"`
	// SupersededRuns are the CI runs a moved head left behind, bounded by
	// MaxSupersededRuns.
	//
	// Checks holds the LATEST head only — applyForgeState replaces it whole
	// on every head move — so before this field the displaced run was simply
	// dropped, and the only record of a run still burning a runner was the
	// run itself, on the forge, indistinguishable from a live one. See
	// supersession.go for why that cost two lanes a CI cycle each.
	SupersededRuns []SupersessionRecord `json:"superseded_runs,omitempty"`

	// LastTurn is the worker turn an attempt ended on when it changed
	// nothing, and it is the ONE thing that survives that attempt.
	//
	// IT IS WHAT MAKES THE NEXT PROMPT DIFFERENT. A fresh attempt renders
	// the same issue contract as the last one, so without this the engine
	// hands a new worker the identical prompt that already produced nothing
	// — three times, until the budget was gone (go-quicbaas#787). Carried on
	// the Delivery rather than the Task because it is the module's own
	// record of the module's own attempt, and because it is written in the
	// same update the attempt's other git-side state is.
	//
	// It is cleared the moment a turn reaches the checkout: a turn that
	// changed a file is not something to warn the next worker about.
	LastTurn *AgentTurn `json:"last_turn,omitempty"`

	// Version is the optimistic-concurrency token every update compares
	// against; a write that finds it moved is refused.
	Version int64 `json:"version"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityID implements the DAO entity contract.
func (d *Delivery) EntityID() uint64 { return d.ID }

// SetEntityID implements the DAO entity contract.
func (d *Delivery) SetEntityID(id uint64) { d.ID = id }

// GetVersion implements the DAO's Versioned contract.
func (d *Delivery) GetVersion() int64 { return d.Version }

// SetVersion implements the DAO's Versioned contract.
func (d *Delivery) SetVersion(version int64) { d.Version = version }

// Subject names the delivery for logs and errors: owner/repo#number when a
// pull request exists, else the branch.
func (d *Delivery) Subject(repo RepoRef) string {
	if d.PullRequest.Opened() {
		return repo.String() + "#" + strconv.Itoa(d.PullRequest.Number)
	}
	return repo.String() + "@" + d.Branch
}

// RerunAttempts counts every automatic re-run asked for against one check on
// one head, landed or refused. It is what the budget is spent from.
func (d *Delivery) RerunAttempts(headSHA, check string) int {
	attempts := 0
	for _, record := range d.CIReruns {
		if record.HeadSHA == headSHA && record.Check == check {
			attempts++
		}
	}
	return attempts
}

// RerunPending reports whether a re-run has already been asked for SINCE this
// result was recorded — the forge has been told and has not answered yet.
//
// DECIDED ON TIME, NOT ON THE RUN ID, and the difference is a job that hangs.
// A forge is free to restart a run in place and report the result under the
// same identifier, so "have we already re-run this id" would answer yes
// forever and the engine would wait out a result it had already been given.
// A finish that is NEWER than the last ask is a new answer whatever it is
// called; one that is older is the answer we asked to replace.
func (d *Delivery) RerunPending(check CheckRecord) bool {
	for _, record := range d.CIReruns {
		if record.HeadSHA != check.HeadSHA || record.Check != check.Name || record.Failure != "" {
			continue
		}
		if record.At.After(check.FinishedAt) {
			return true
		}
	}
	return false
}

// RecordRerun appends one re-run attempt.
func (d *Delivery) RecordRerun(record CIRerunRecord) {
	d.CIReruns = append(d.CIReruns, record)
}

// ReviewRecorded reports whether a review with the same forge submission id
// (or, for a submission without one, the same reviewer/head/verdict) is
// already recorded.
func (d *Delivery) ReviewRecorded(candidate ReviewRecord) bool {
	for _, held := range d.Reviews {
		if candidate.ExternalID != "" {
			if held.ExternalID == candidate.ExternalID {
				return true
			}
			continue
		}
		if held.ExternalID == "" && held.Reviewer == candidate.Reviewer &&
			held.HeadSHA == candidate.HeadSHA && held.Verdict == candidate.Verdict {
			return true
		}
	}
	return false
}

// HeadRefused reports whether the newest review binding to the delivery's
// current head refused it.
//
// IT IS THE DURABLE FORM OF "a round was requested". The runner's own
// roundRequested flag lives for one job; this fact lives on the delivery, and
// the two answer the same question across a restart, a readmission or a job
// that ended before the worker addressed the findings.
//
// A REFUSED HEAD IS NOT REVIEWABLE. Reviewing it again judges the identical
// tree and returns the identical verdict, so a resume that treats it as
// reviewable spends a reviewer per attempt until the budget is gone and the
// pull request is left open, refused, with no worker ever sent to fix it.
// Measured on go-truststrap PRs #64, #65 and #66.
//
// NEWEST WINS RATHER THAN ANY. A head can carry a refusal and then an
// approval from a second reviewer, or a re-review after a force-push that
// kept the sha; the last word is the one in force. Reviews that bind to an
// earlier head say nothing about this one and are ignored, which is the rule
// AppliesTo exists to state.
func (d *Delivery) HeadRefused() bool { return d.HeadRefusal() != nil }

// HeadRefusal returns the refusal in force on the delivery's current head, or
// nil when the head is not refused.
//
// IT CARRIES THE FINDINGS, which is the half that makes the resume useful. A
// readmitted task has a fresh job: no iteration and no memory, so the runner's
// two live sources of round findings are both empty and the worker is handed
// the bare issue contract. It then reads a branch that already holds the work,
// correctly answers "the branch already carries the complete implementation",
// and the attempt is charged as a failure -- with the reviewer's actual
// objection never delivered to anyone. Measured on go-truststrap task
// 61e3aa53 / PR #66, 2026-09-16.
//
// NEWEST WINS RATHER THAN ANY. A head can carry a refusal and then an approval
// from a second reviewer, or a re-review after a force-push that kept the sha;
// the last word is the one in force. Reviews that bind to an earlier head say
// nothing about this one and are ignored, which is the rule AppliesTo exists
// to state.
func (d *Delivery) HeadRefusal() *ReviewRecord {
	if d == nil || strings.TrimSpace(d.HeadSHA) == "" {
		return nil
	}
	var newest *ReviewRecord
	for i := range d.Reviews {
		review := &d.Reviews[i]
		if !review.AppliesTo(d.HeadSHA) {
			continue
		}
		if newest == nil || review.SubmittedAt.After(newest.SubmittedAt) {
			newest = review
		}
	}
	if newest == nil || newest.Verdict != VerdictRequestChanges {
		return nil
	}
	return newest
}

// Validate checks the delivery names its task and attempt.
func (d *Delivery) Validate() error {
	if d == nil {
		return &InvalidInputError{Field: "delivery", Message: "must not be nil"}
	}
	if err := validateKeySegment("task_id", d.TaskID); err != nil {
		return err
	}
	if d.Attempt <= 0 {
		return &InvalidInputError{Field: "attempt", Message: "attempts are numbered from one"}
	}
	if d.Branch == "" {
		return &InvalidInputError{Field: "branch", Message: "a delivery works on a branch"}
	}
	return nil
}
