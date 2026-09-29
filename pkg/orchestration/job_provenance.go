// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package orchestration

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// =============================================================================
// Job provenance — who ran the attempt, which attempt it was, and what it left
// =============================================================================
//
// A JOB PAGE THAT CANNOT NAME THE RUN IS A LOG VIEWER. go-quicbaas#796 measured
// what was missing: an attempt at a task rendered its agent as an em-dash, its
// attempt as nothing at all, and its pull request not at all, because the job
// record carried none of them. The record is the only thing every transport
// serializes, so anything a reader needs about a run has to be ON IT.
//
// THREE KINDS OF FACT, three different sources, and the difference matters:
//
//   - RECORDED BY THE RUN — the fleet node the work was dispatched to and the
//     lifecycle moments the loop entered. Both are written by JobRun as they
//     happen and persist with the status, because nothing else observes them
//     and a finished job must still answer for them.
//
//   - DERIVED FROM THE TASK — the attempt history, the budget and the typed
//     conclusion. These belong to the TASK, which outlives every attempt at it,
//     and are copied onto the status when it is read rather than duplicated
//     into it when it is written: a copy taken at write time would be stale for
//     every attempt but the last.
//
//   - PUBLISHED AS TASK ARTIFACTS — the branch, the head, the pull request and
//     the review rounds. The module that runs the attempt (pkg/sdlc) publishes
//     them under the names below; this package READS them and never imports the
//     module. That is what keeps a generic job page working for a runner this
//     package has never heard of.

// =============================================================================
// Recorded by the run
// =============================================================================

// JobStageRecord is one lifecycle moment the job entered and the instant it
// entered it.
//
// The stage EVENTS already say this, but only as prose in an audit line. A
// reader wanting "how long did reviewable take" had to parse messages; the
// timeline the UI draws is arithmetic over this list instead.
type JobStageRecord struct {
	// Stage names the lifecycle moment.
	Stage JobStage `json:"stage"`

	// EnteredAt is the Unix-nanosecond instant the moment was entered. The
	// moment's duration is the next record's EnteredAt, or the job's
	// UpdatedAt for the last one.
	EnteredAt int64 `json:"entered_at"`
}

// =============================================================================
// Derived from the task
// =============================================================================

// JobAttemptRecord is one attempt at the job's task: which ordinal it was and
// which job ran it.
type JobAttemptRecord struct {
	// Attempt is the 1-based ordinal of the attempt.
	Attempt int `json:"attempt"`

	// JobID is the job that ran it.
	JobID string `json:"job_id"`

	// Current marks the attempt the status being read belongs to.
	Current bool `json:"current,omitempty"`
}

// =============================================================================
// Published as task artifacts
// =============================================================================

const (
	// ArtifactNamePullRequest is the task artifact naming the pull request an
	// attempt opened. Kind url; Ref.Path is the URL; Metadata carries
	// "number", "state" and "branch".
	ArtifactNamePullRequest = "pull_request"

	// ArtifactNameHead is the task artifact naming the commit an attempt
	// pushed. Kind commit; Ref.SHA is the head and Ref.Branch the branch.
	ArtifactNameHead = "head"

	// ArtifactNameReviews is the task artifact carrying the review rounds
	// recorded against the attempt's heads. Kind inline; Content is a JSON
	// array of JobReviewRound.
	ArtifactNameReviews = "reviews"
)

// ArtifactAttemptSuffix separates a delivery artifact's base name from the
// attempt it belongs to: "pull_request@a3".
//
// THE NAME CARRIES THE ATTEMPT BECAUSE THE STORE OVERWRITES BY NAME.
// SubmitTaskArtifacts indexes the task's artifacts by name and replaces a
// resubmission in place (task_store.go), while a retried task keeps the same
// record and merely increments Attempts (task_dispatcher.go). Published under
// one flat name, attempt 3's pull request therefore REPLACED attempt 1's, and
// attempt 1's job page then showed attempt 3's delivery as its own — which is
// the opposite of what a per-attempt page is for. Scoping the name keeps every
// attempt's delivery and lets each job read exactly its own.
const ArtifactAttemptSuffix = "@a"

// ArtifactNameForAttempt is the published name of a delivery artifact for one
// attempt. An attempt of zero — a job that is not an attempt at a task — keeps
// the bare base name, because there is nothing to distinguish it from.
func ArtifactNameForAttempt(base string, attempt int) string {
	if attempt <= 0 {
		return base
	}
	return base + ArtifactAttemptSuffix + strconv.Itoa(attempt)
}

// ArtifactBaseName strips the attempt scope from a published name, so a reader
// that wants every attempt's deliveries can group them.
func ArtifactBaseName(name string) string {
	if at := strings.LastIndex(name, ArtifactAttemptSuffix); at > 0 {
		return name[:at]
	}
	return name
}

// artifactMetadataNumber, artifactMetadataState and artifactMetadataBranch are
// the metadata keys the pull-request artifact carries.
const (
	artifactMetadataNumber = "number"
	artifactMetadataState  = "state"
	artifactMetadataBranch = "branch"
)

// JobReviewRound is one reviewer's verdict on one head of the job's branch.
//
// It is the wire shape of the review rounds a runner publishes, deliberately
// stringly-typed on Verdict: the vocabulary belongs to the module that reviews,
// and this package renders whatever it is told rather than gatekeeping a set it
// does not own.
type JobReviewRound struct {
	// Round is the review round the verdict was recorded in.
	Round int `json:"round"`

	// Reviewer is the forge login that submitted the verdict.
	Reviewer string `json:"reviewer"`

	// Verdict is what the reviewer said, in the reviewing module's own
	// vocabulary ("approve", "request_changes", "error").
	Verdict string `json:"verdict"`

	// HeadSHA is the head the verdict binds to.
	HeadSHA string `json:"head_sha,omitempty"`

	// Findings is the reviewer's body on a refusal.
	Findings string `json:"findings,omitempty"`

	// SubmittedAt is the RFC 3339 instant the verdict was submitted.
	SubmittedAt string `json:"submitted_at,omitempty"`
}

// JobDelivery is the git side of the job's attempt, read off the artifacts the
// runner published on the task.
type JobDelivery struct {
	// Branch is the branch the attempt worked on.
	Branch string `json:"branch,omitempty"`

	// HeadSHA is the last head the attempt pushed.
	HeadSHA string `json:"head_sha,omitempty"`

	// PullRequestNumber, PullRequestURL and PullRequestState describe the
	// pull request, all zero for a change that landed without one.
	PullRequestNumber int    `json:"pull_request_number,omitempty"`
	PullRequestURL    string `json:"pull_request_url,omitempty"`
	PullRequestState  string `json:"pull_request_state,omitempty"`

	// Reviews are the review rounds recorded against the attempt's heads,
	// in the order the runner published them.
	Reviews []JobReviewRound `json:"reviews,omitempty"`
}

// Empty reports whether the delivery carries nothing worth rendering.
func (d *JobDelivery) Empty() bool {
	return d == nil || (d.Branch == "" && d.HeadSHA == "" &&
		d.PullRequestNumber == 0 && d.PullRequestURL == "" && len(d.Reviews) == 0)
}

// JobDeliveryFromArtifacts reads the delivery ONE ATTEMPT published on a task.
//
// The attempt is part of the question, not a filter applied afterwards: a task
// accumulates one delivery per attempt and they are different deliveries —
// different branch heads, different pull requests, different review rounds.
// Reading them as a single set attributes the newest attempt's work to every
// job that ever ran for the task.
//
// It returns nil when the attempt published none of the names, so a job from a
// runner that publishes nothing — or an attempt that ended before it published
// anything — has no delivery panel rather than a blank one. An artifact whose
// content cannot be decoded is an ERROR rather than a silent omission: a review
// round that exists and is not shown is the failure go-quicbaas#796 is about.
func JobDeliveryFromArtifacts(artifacts []Artifact, attempt int) (*JobDelivery, error) {
	wantPullRequest := ArtifactNameForAttempt(ArtifactNamePullRequest, attempt)
	wantHead := ArtifactNameForAttempt(ArtifactNameHead, attempt)
	wantReviews := ArtifactNameForAttempt(ArtifactNameReviews, attempt)

	delivery := &JobDelivery{}
	for i := range artifacts {
		artifact := &artifacts[i]
		switch artifact.Name {
		case wantPullRequest:
			if err := applyPullRequestArtifact(delivery, artifact); err != nil {
				return nil, err
			}
		case wantHead:
			applyHeadArtifact(delivery, artifact)
		case wantReviews:
			if err := applyReviewsArtifact(delivery, artifact); err != nil {
				return nil, err
			}
		}
	}
	if delivery.Empty() {
		return nil, nil
	}
	return delivery, nil
}

// applyPullRequestArtifact reads the URL, the number, the state and the branch
// off the pull-request artifact.
func applyPullRequestArtifact(delivery *JobDelivery, artifact *Artifact) error {
	delivery.PullRequestURL = strings.TrimSpace(artifact.Ref.Path)
	delivery.PullRequestState = artifact.Metadata[artifactMetadataState]
	if branch := artifact.Metadata[artifactMetadataBranch]; branch != "" {
		delivery.Branch = branch
	}
	raw := strings.TrimSpace(artifact.Metadata[artifactMetadataNumber])
	if raw == "" {
		return nil
	}
	number, err := strconv.Atoi(raw)
	if err != nil {
		return &JobArtifactDecodeError{
			Artifact: ArtifactNamePullRequest,
			Field:    artifactMetadataNumber,
			Message:  "pull request number " + strconv.Quote(raw) + " is not a number",
		}
	}
	delivery.PullRequestNumber = number
	return nil
}

// applyHeadArtifact reads the pushed head and, when the pull request did not
// already name one, the branch it is on.
func applyHeadArtifact(delivery *JobDelivery, artifact *Artifact) {
	delivery.HeadSHA = artifact.Ref.SHA
	if delivery.Branch == "" {
		delivery.Branch = artifact.Ref.Branch
	}
}

// applyReviewsArtifact decodes the review rounds carried inline.
//
// AN UNRESOLVED REFERENCE IS AN ERROR, NOT AN ABSENCE. SubmitTaskArtifacts
// promotes an inline artifact past the store's inline limit to a kv_key and
// clears Content, so the rounds of a long review arrive here as a reference
// with nothing in it. Read as "no content, nothing to show", that silently
// dropped the real findings and did so precisely on the biggest ones — the
// reviews most worth reading. The caller resolves the reference before
// handing it over; this refuses rather than papering over a caller that did
// not.
func applyReviewsArtifact(delivery *JobDelivery, artifact *Artifact) error {
	if len(artifact.Content) == 0 {
		if artifact.Kind == ArtifactKindKVKey {
			return &JobArtifactDecodeError{
				Artifact: ArtifactNameReviews,
				Field:    "content",
				Message: "the rounds are stored at " + strconv.Quote(artifact.Ref.Path) +
					" and were not resolved before the delivery was read",
			}
		}
		return nil
	}
	var rounds []JobReviewRound
	if err := json.Unmarshal(artifact.Content, &rounds); err != nil {
		return &JobArtifactDecodeError{
			Artifact: ArtifactNameReviews,
			Field:    "content",
			Message:  err.Error(),
		}
	}
	delivery.Reviews = rounds
	return nil
}

// =============================================================================
// The attempt history a task carries
// =============================================================================

// JobAttemptsOfTask lists every attempt the task records, oldest first, marking
// the one currentJobID ran.
//
// The task's own history holds the CONCLUDED attempts; the attempt still open
// is named by SpawnedJobID and is appended after them. A job whose id appears
// in neither — a status read after another attempt has already replaced it —
// still gets its own entry at its recorded ordinal, so the page never says
// "attempt —" for a job that plainly had one.
func JobAttemptsOfTask(task *Task, currentJobID string, currentAttempt int) []JobAttemptRecord {
	if task == nil {
		return nil
	}
	ids := task.AttemptHistory()
	if task.SpawnedJobID != "" {
		ids = append(ids, task.SpawnedJobID)
	}
	records := make([]JobAttemptRecord, 0, len(ids)+1)
	found := false
	for i, id := range ids {
		current := id != "" && id == currentJobID
		found = found || current
		records = append(records, JobAttemptRecord{Attempt: i + 1, JobID: id, Current: current})
	}
	if found || currentJobID == "" || currentAttempt <= 0 {
		return records
	}
	return insertAttempt(records, JobAttemptRecord{Attempt: currentAttempt, JobID: currentJobID, Current: true})
}

// insertAttempt places the current attempt at its own ordinal, replacing an
// entry the history recorded at that ordinal under another job id.
func insertAttempt(records []JobAttemptRecord, current JobAttemptRecord) []JobAttemptRecord {
	for i := range records {
		if records[i].Attempt == current.Attempt {
			records[i] = current
			return records
		}
	}
	records = append(records, current)
	for i := len(records) - 1; i > 0 && records[i-1].Attempt > records[i].Attempt; i-- {
		records[i-1], records[i] = records[i], records[i-1]
	}
	return records
}

// =============================================================================
// Errors
// =============================================================================

// ErrJobArtifactDecode indicates a published task artifact whose content the
// job record could not read.
var ErrJobArtifactDecode = errors.New("orchestration: job artifact could not be decoded")

// JobArtifactDecodeError reports a published artifact the job record could not
// read. The job's own status is unaffected; the read that hit it fails so the
// omission is never silent.
type JobArtifactDecodeError struct {
	Artifact string
	Field    string
	Message  string
}

// Error implements the error interface.
func (e *JobArtifactDecodeError) Error() string {
	return ErrJobArtifactDecode.Error() + ": artifact=" + e.Artifact +
		" field=" + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *JobArtifactDecodeError) Unwrap() error { return ErrJobArtifactDecode }
