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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// PreflightJobs is the slice of the job engine a preflight needs: start the
// job, then follow it to its end.
type PreflightJobs interface {
	JobStarter
	GetJobStatus(ctx context.Context, tenantID, jobID string) (*orchestration.JobStatus, error)
	SubscribeJobEvents(ctx context.Context, tenantID, jobID string) (<-chan *orchestration.JobEvent, error)
}

// PreflightResult is what a finished preflight job said.
type PreflightResult struct {
	// JobID is the preflight job.
	JobID string

	// State is the job's terminal state.
	State orchestration.JobState

	// Error is the job's failure, empty when it completed.
	Error string

	// Results is each verification command's answer, in the order they ran.
	Results []orchestration.ValidationResult
}

// Passed reports whether the preflight is green: the job completed and every
// command it ran passed. A job that ran no command is not green; it proved
// nothing.
func (r *PreflightResult) Passed() bool {
	if r == nil || r.State != orchestration.JobStateCompleted || r.Error != "" || len(r.Results) == 0 {
		return false
	}
	for _, result := range r.Results {
		if !result.Passed {
			return false
		}
	}
	return true
}

// FailureReport states why the preflight is red: the job's own failure, then
// each failing command with its failing lines. It is empty for a green one.
func (r *PreflightResult) FailureReport() string {
	if r == nil || r.Passed() {
		return ""
	}
	var lines []string
	if r.Error != "" {
		lines = append(lines, "job "+r.JobID+" "+string(r.State)+": "+r.Error)
	}
	for _, result := range r.Results {
		if result.Passed {
			continue
		}
		lines = append(lines,
			"command="+result.Name+" exit_code="+strconv.Itoa(result.ExitCode),
			verificationSummary(result))
	}
	if len(lines) == 0 {
		lines = append(lines, "job "+r.JobID+" "+string(r.State)+" without running a command")
	}
	return strings.Join(lines, "\n")
}

// RunPreflight starts a preflight of the bound project and waits for it to
// end. It returns an error only when no verdict was reached; a red preflight
// is a result whose Passed is false.
func RunPreflight(ctx context.Context, jobs PreflightJobs, tenantID string, binding *ProjectBinding) (*PreflightResult, error) {
	if jobs == nil {
		return nil, &InvalidInputError{Field: "jobs", Message: "the preflight runs as a job and no job engine is wired"}
	}
	if binding == nil {
		return nil, &InvalidInputError{Field: "binding", Message: "the preflight runs the binding's verification commands"}
	}
	ctx = orchestration.ContextWithTenant(ctx, tenantID)
	started, err := jobs.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:    tenantID,
		ProjectID:   binding.ProjectID,
		Runner:      PreflightRunnerName,
		Description: "Preflight the verification commands of " + binding.Repo().String() + " on its default branch",
		Lead:        binding.Worker,
	})
	if err != nil {
		return nil, &PreflightError{ProjectID: binding.ProjectID, Reason: "the job did not start", Cause: err}
	}
	if started == nil || started.JobID == "" {
		return nil, &PreflightError{ProjectID: binding.ProjectID, Reason: "the job engine returned no job"}
	}
	status, err := awaitPreflight(ctx, jobs, tenantID, binding.ProjectID, started.JobID)
	if err != nil {
		return nil, err
	}
	return &PreflightResult{
		JobID:   started.JobID,
		State:   status.State,
		Error:   status.Error,
		Results: status.Verification,
	}, nil
}

// awaitPreflight follows the job until it reaches a terminal state. It
// subscribes BEFORE it reads the status, so an end that lands between the two
// is still seen: either the read finds it or an event follows it.
func awaitPreflight(ctx context.Context, jobs PreflightJobs, tenantID, projectID, jobID string) (*orchestration.JobStatus, error) {
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := jobs.SubscribeJobEvents(watchCtx, tenantID, jobID)
	if err != nil {
		return nil, &PreflightError{ProjectID: projectID, JobID: jobID, Reason: "the job could not be followed", Cause: err}
	}
	for {
		status, err := jobs.GetJobStatus(watchCtx, tenantID, jobID)
		if err != nil {
			return nil, &PreflightError{ProjectID: projectID, JobID: jobID, Reason: "the job's status could not be read", Cause: err}
		}
		if status == nil {
			return nil, &PreflightError{ProjectID: projectID, JobID: jobID, Reason: "the job engine returned no status"}
		}
		if orchestration.IsTerminalJobState(status.State) {
			return status, nil
		}
		if events == nil {
			return nil, &PreflightError{ProjectID: projectID, JobID: jobID, Reason: "the job's event stream ended before the job did"}
		}
		select {
		case <-ctx.Done():
			return nil, &PreflightError{ProjectID: projectID, JobID: jobID, Reason: "stopped waiting for the job", Cause: ctx.Err()}
		case _, open := <-events:
			if !open {
				// One last read: the stream may close because the job ended.
				events = nil
			}
		}
	}
}
