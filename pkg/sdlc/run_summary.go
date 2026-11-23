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

// The run summary is what a run publishes about itself on the forge.
//
// THE RULE: every field is a number, a word of a fixed vocabulary, or an
// identifier the engine minted. No free text from a subprocess, a worker, a
// configuration value or an error string ever enters it, which is why it is
// safe on a public forge without relying on redaction.
//
// Deliberately left out: the forge base URL, host, clone URL and pull
// request URL; token and secret refs; the tenant ID; the worker agent, model
// and account; node and host names; the host work dir and the container dir;
// command lines and their arguments; raw Output, Message and Details; the
// worker's reply; conflicting path names; park reason text; and any error
// string. The output a summary withholds is counted, and named by the audit
// record of the job that holds it.

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const (
	// runSummaryArtifactName is the base name of the run summary artifact.
	runSummaryArtifactName = "run_summary"

	// runSummaryVersion is the version of the summary's JSON shape.
	runSummaryVersion = 1

	// runSummaryMaxChecks bounds the failed checks a summary lists.
	runSummaryMaxChecks = 8

	// auditRunSummary is the audit event of a summary that was not recorded.
	auditRunSummary = "sdlc:run_summary"
)

// RunOutcome is how a run ended.
type RunOutcome string

const (
	// RunOutcomeCompleted is a run that ended without an error.
	RunOutcomeCompleted RunOutcome = "completed"
	// RunOutcomeParked is a run that parked its task.
	RunOutcomeParked RunOutcome = "parked"
	// RunOutcomeFailed is a run that ended on an error.
	RunOutcomeFailed RunOutcome = "failed"
)

// RunSummary is a run's structured public summary. Its JSON keys are the
// whole of what it publishes.
type RunSummary struct {
	Version       int               `json:"version"`
	Outcome       RunOutcome        `json:"outcome"`
	TaskID        string            `json:"task_id"`
	JobID         string            `json:"job_id"`
	Attempt       int               `json:"attempt"`
	Stage         ArtifactStage     `json:"stage,omitempty"`
	Cause         string            `json:"cause,omitempty"`
	FaultClass    string            `json:"fault_class,omitempty"`
	PullRequest   int               `json:"pull_request,omitempty"`
	ReviewRounds  int               `json:"review_rounds,omitempty"`
	Checks        []RunSummaryCheck `json:"checks,omitempty"`
	ChecksOmitted int               `json:"checks_omitted,omitempty"`
	Git           *RunSummaryGit    `json:"git,omitempty"`
	ConflictCount int               `json:"conflict_count,omitempty"`
	AuditRecord   string            `json:"audit_record,omitempty"`
}

// RunSummaryCheck is one failed verification check: its position, its
// program's base name, its exit code and how much output it withheld.
type RunSummaryCheck struct {
	Index         int    `json:"index"`
	Program       string `json:"program"`
	ExitCode      int    `json:"exit_code"`
	WithheldBytes int    `json:"withheld_bytes"`
}

// RunSummaryGit is the git command a run failed on: an allowlisted
// subcommand, its exit code and how much output it withheld.
type RunSummaryGit struct {
	Subcommand    string `json:"subcommand"`
	ExitCode      int    `json:"exit_code"`
	WithheldBytes int    `json:"withheld_bytes"`
}

// runSummaryInput is the typed state a summary is built from.
type runSummaryInput struct {
	TaskID, JobID string
	Attempt       int
	Err           error
	LastState     orchestration.JobState
	ParkKind      orchestration.TaskBlockKind
	FaultClass    string
	FailedChecks  []orchestration.ValidationResult
	Delivery      *Delivery
}

// summarizeRun builds a run's summary from typed values only.
func summarizeRun(in runSummaryInput) RunSummary {
	s := RunSummary{
		Version: runSummaryVersion,
		TaskID:  in.TaskID,
		JobID:   in.JobID,
		Attempt: in.Attempt,
	}
	var escalated *EscalatedError
	switch {
	case in.Err == nil && in.ParkKind == "":
		s.Outcome = RunOutcomeCompleted
	case in.ParkKind != "" || errors.As(in.Err, &escalated):
		s.Outcome = RunOutcomeParked
	default:
		s.Outcome = RunOutcomeFailed
	}

	var (
		checkout *CheckoutCommandError
		noChange *NoChangesError
		conflict *MergeResultConflictError
		publish  *PublishCommandError
	)
	switch {
	case errors.As(in.Err, &checkout):
		s.Cause, s.Stage = "checkout_failed", StageBranch
		s.Git = &RunSummaryGit{Subcommand: gitSubcommand(checkout.Command), ExitCode: checkout.ExitCode, WithheldBytes: len(checkout.Output)}
		s.AuditRecord = auditCheckout
	case errors.As(in.Err, &noChange):
		s.Cause, s.Stage = "no_changes", StageCommit
	case errors.As(in.Err, &conflict):
		s.Cause, s.Stage = "merge_conflict", StageVerification
		s.ConflictCount = len(conflict.Paths)
		s.Git = &RunSummaryGit{Subcommand: "merge", ExitCode: 0, WithheldBytes: len(conflict.Output)}
		s.AuditRecord = auditMergeConflict
	case errors.As(in.Err, &publish):
		s.Cause, s.Stage = "publish_failed", StagePullRequest
		s.Git = &RunSummaryGit{Subcommand: gitSubcommand(publish.Command), ExitCode: publish.ExitCode, WithheldBytes: len(publish.Output)}
	case s.Outcome == RunOutcomeParked:
		s.Cause = string(in.ParkKind)
		if s.Cause == "" {
			s.Cause = "escalated"
		}
		s.Stage = stageOfState(in.LastState)
	case in.Err != nil:
		s.Cause, s.Stage = "error", stageOfState(in.LastState)
	default:
		s.Stage = stageOfState(in.LastState)
	}

	for i, result := range in.FailedChecks {
		if i >= runSummaryMaxChecks {
			s.ChecksOmitted = len(in.FailedChecks) - runSummaryMaxChecks
			break
		}
		s.Checks = append(s.Checks, RunSummaryCheck{
			Index:         i + 1,
			Program:       checkProgram(result.Name),
			ExitCode:      result.ExitCode,
			WithheldBytes: len(result.Message) + len(result.Details),
		})
	}
	if len(s.Checks) > 0 && s.AuditRecord == "" {
		s.AuditRecord = auditVerification
	}

	if orchestration.OperatorFaultClass(in.FaultClass) {
		s.FaultClass = in.FaultClass
	}
	if in.Delivery != nil {
		if in.Delivery.PullRequest.Opened() {
			s.PullRequest = in.Delivery.PullRequest.Number
		}
		s.ReviewRounds = len(in.Delivery.Reviews)
	}
	return s
}

// runStages maps the state a run last handled to the stage it names.
var runStages = map[orchestration.JobState]ArtifactStage{
	orchestration.JobStateInitializing: StageBranch,
	orchestration.JobStateExecuting:    StageCommit,
	orchestration.JobStateRedelegating: StageCommit,
	orchestration.JobStateValidating:   StageVerification,
	orchestration.JobStateReviewing:    StageReview,
	orchestration.JobStateCompleting:   StageMerge,
	orchestration.JobStateCleanup:      StageMerge,
}

// stageOfState is the artifact stage a run state works on, empty for a
// state that works on none.
func stageOfState(state orchestration.JobState) ArtifactStage {
	return runStages[state]
}

var (
	// envAssignmentName is the name of a leading VAR=value token.
	envAssignmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	// programName is a base name a summary may publish.
	programName = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)
)

// checkProgram reduces a verification command to its program's base name,
// or to "command" when that name is not a plain one.
func checkProgram(command string) string {
	for _, token := range strings.Fields(command) {
		if name, _, ok := strings.Cut(token, "="); ok && envAssignmentName.MatchString(name) {
			continue
		}
		if base := path.Base(token); programName.MatchString(base) {
			return base
		}
		return "command"
	}
	return "command"
}

// gitSubcommands are the git subcommands a summary may name.
var gitSubcommands = map[string]struct{}{
	"clone": {}, "fetch": {}, "checkout": {}, "switch": {}, "branch": {},
	"merge": {}, "push": {}, "commit": {}, "rev-parse": {}, "log": {},
	"diff": {}, "status": {}, "worktree": {}, "remote": {}, "config": {},
	"reset": {}, "ls-remote": {}, "init": {}, "add": {},
}

// gitSubcommand reduces a git command to its allowlisted subcommand, or to
// "git" when it names none.
func gitSubcommand(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "git"
	}
	if _, ok := gitSubcommands[fields[0]]; ok {
		return fields[0]
	}
	return "git"
}

// Render renders the summary as the Markdown list an outcome comment quotes.
func (s RunSummary) Render() string {
	var lines []string
	head := "- outcome: `" + string(s.Outcome) + "`"
	if s.Stage != "" {
		head += ", stage: `" + string(s.Stage) + "`"
	}
	if s.Cause != "" {
		head += ", cause: `" + s.Cause + "`"
	}
	lines = append(lines, head)
	if s.FaultClass != "" {
		lines = append(lines, "- fault class: `"+s.FaultClass+"`")
	}
	if s.PullRequest > 0 {
		lines = append(lines, "- pull request: #"+strconv.Itoa(s.PullRequest))
	}
	if s.ReviewRounds > 0 {
		lines = append(lines, "- review rounds: "+strconv.Itoa(s.ReviewRounds))
	}
	for _, check := range s.Checks {
		status := "exited " + strconv.Itoa(check.ExitCode)
		if check.ExitCode == 0 {
			status = "failed"
		}
		lines = append(lines, "- check "+strconv.Itoa(check.Index)+": `"+check.Program+"` "+status+", "+
			strconv.Itoa(check.WithheldBytes)+" bytes of output withheld")
	}
	if s.ChecksOmitted > 0 {
		lines = append(lines, "- and "+strconv.Itoa(s.ChecksOmitted)+" more failed check(s)")
	}
	if s.Git != nil {
		lines = append(lines, "- git `"+s.Git.Subcommand+"` exited "+strconv.Itoa(s.Git.ExitCode)+", "+
			strconv.Itoa(s.Git.WithheldBytes)+" bytes of output withheld")
	}
	if s.ConflictCount > 0 {
		lines = append(lines, "- conflicting paths: "+strconv.Itoa(s.ConflictCount))
	}
	if s.AuditRecord != "" {
		lines = append(lines, "- the output is in the `"+s.AuditRecord+"` audit record of job `"+s.JobID+"`")
	}
	return strings.Join(lines, "\n")
}

// recordRunSummary submits the summary as the attempt's inline run_summary
// artifact. A nil job, or one with no task, records nothing.
func (h *Hooks) recordRunSummary(ctx context.Context, job *orchestration.JobContext, summary RunSummary) error {
	if job == nil || job.TaskID == "" {
		return nil
	}
	content, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return h.tasks.SubmitTaskArtifacts(ctx, job.TenantID, job.TaskID, []orchestration.Artifact{{
		Kind:     orchestration.ArtifactKindInline,
		Name:     orchestration.ArtifactNameForAttempt(runSummaryArtifactName, job.Attempt),
		MIMEType: "application/json",
		Size:     int64(len(content)),
		Content:  content,
		Metadata: map[string]string{
			"attempt": strconv.Itoa(job.Attempt),
			"outcome": string(summary.Outcome),
		},
	}})
}

// latestRunSummary decodes the task's run summary of its highest attempt.
func latestRunSummary(task *orchestration.Task) (RunSummary, bool) {
	if task == nil {
		return RunSummary{}, false
	}
	var (
		latest *orchestration.Artifact
		best   = -1
	)
	for i := range task.OutputArtifacts {
		artifact := &task.OutputArtifacts[i]
		if artifact.Kind != orchestration.ArtifactKindInline || orchestration.ArtifactBaseName(artifact.Name) != runSummaryArtifactName {
			continue
		}
		attempt, err := strconv.Atoi(artifact.Metadata["attempt"])
		if err != nil {
			attempt = 0
		}
		if attempt > best {
			latest, best = artifact, attempt
		}
	}
	if latest == nil {
		return RunSummary{}, false
	}
	var summary RunSummary
	if err := json.Unmarshal(latest.Content, &summary); err != nil {
		return RunSummary{}, false
	}
	return summary, true
}
