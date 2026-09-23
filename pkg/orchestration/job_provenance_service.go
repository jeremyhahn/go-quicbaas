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

import "context"

// =============================================================================
// The service's side of job provenance
// =============================================================================
//
// A status the run published is IMMUTABLE — it is read by every poll and by
// the next snapshot, from goroutines the reader does not own — so the task's
// facts are laid over a COPY. That copy is what the transports serialize; the
// record in the store and in the run is untouched.

// withTaskProvenance returns status enriched with the facts its task holds:
// the attempt history and budget, the typed conclusion, whether a person may
// readmit it, and the delivery the runner published as task artifacts.
//
// A job that is not an attempt at a task, or a service with no task store, is
// returned unchanged. A task that cannot be READ is not an error: the job's
// own record is complete and useful without the task's, and failing the whole
// read would take the page away over the part of it that is missing. A task
// that reads but whose published artifacts cannot be DECODED is an error,
// because that is a delivery the page would silently omit.
func (s *agentServiceImpl) withTaskProvenance(ctx context.Context, tenantID string, status *JobStatus) (*JobStatus, error) {
	taskID := jobStatusTaskID(status)
	if taskID == "" || s.taskStore == nil {
		return status, nil
	}
	task, err := s.taskStore.GetTask(ctx, tenantID, taskID)
	if err != nil {
		s.logger.Debugf("orchestration: job %s: task %s not readable for provenance: %s",
			status.JobID, taskID, err.Error())
		return status, nil
	}
	artifacts, err := s.resolveDeliveryArtifacts(ctx, tenantID, task, status.Attempt)
	if err != nil {
		return nil, err
	}
	delivery, err := JobDeliveryFromArtifacts(artifacts, status.Attempt)
	if err != nil {
		return nil, err
	}
	enriched := *status
	enriched.Source = task.Source
	enriched.Attempts = JobAttemptsOfTask(task, status.JobID, status.Attempt)
	enriched.AttemptBudget = task.EffectiveAttemptBudget()
	enriched.TerminalReason = task.ConclusionReason
	enriched.ReadmitAllowed = task.Status == TaskStatusBlocked
	enriched.Delivery = delivery
	return &enriched, nil
}

// resolveDeliveryArtifacts returns the task's artifacts with this attempt's
// delivery references dereferenced.
//
// THE STORE OFFLOADS WHAT IT CANNOT HOLD INLINE. SubmitTaskArtifacts rewrites
// an inline artifact past its limit to a kv_key and clears Content, so a
// review with enough findings to matter arrives on the task as a reference to
// bytes elsewhere. Reading the record alone therefore showed every SHORT
// review and silently dropped every long one — the inverse of useful.
//
// Only the reviews artifact carries bytes; the pull request and the head are
// a URL and a SHA in the ref, which the record always holds. So exactly one
// name can need this, and nothing else is re-read.
func (s *agentServiceImpl) resolveDeliveryArtifacts(
	ctx context.Context, tenantID string, task *Task, attempt int,
) ([]Artifact, error) {
	reviewsName := ArtifactNameForAttempt(ArtifactNameReviews, attempt)
	out := task.OutputArtifacts
	for i := range task.OutputArtifacts {
		artifact := task.OutputArtifacts[i]
		if artifact.Name != reviewsName || artifact.Kind != ArtifactKindKVKey {
			continue
		}
		resolved, err := s.taskStore.GetTaskArtifact(ctx, tenantID, task.ID, artifact.Name)
		if err != nil {
			return nil, err
		}
		// Copy before writing: the task record belongs to the store, and the
		// slice may be shared with whatever else is reading it.
		out = append([]Artifact(nil), task.OutputArtifacts...)
		out[i] = *resolved
		break
	}
	return out, nil
}

// =============================================================================
// The source projection, for a page of jobs
// =============================================================================

// resolveJobSources names the forge issue behind each job in a page, read off
// the task the job is an attempt at.
//
// IT RUNS OVER A PAGE, NOT OVER A LISTING, for the reason resolveSessionRunners
// does: a tenant with two thousand jobs names nearly as many tasks, and reading
// every one of them to answer a page of twenty-five would make the listing pay
// for records nobody asked to see. The read is MEMOISED BY TASK because a task
// has several attempts and every one of them resolves to the same record, so
// the whole attempt history of one task costs one read.
//
// THE STATUS IS COPIED BEFORE IT IS WRITTEN TO. A running job's status is
// published by its own goroutine and read by every poll in flight; writing the
// source into that published struct would be a data race with readers holding
// the same pointer. The page gets copies, which is what withTaskProvenance does
// for the same reason and what the transports serialize.
//
// A task that cannot be read leaves the job with no source, and a job that is
// not an attempt at a task never had one: the consumer renders no chip rather
// than a guess, which is the rule every other unobservable field here follows.
func (s *agentServiceImpl) resolveJobSources(ctx context.Context, tenantID string, jobs []*JobStatus) {
	if s.taskStore == nil {
		return
	}
	sources := make(map[string]*TaskSourceRef, len(jobs))
	for i, job := range jobs {
		if job == nil || job.Source != nil {
			continue
		}
		taskID := jobStatusTaskID(job)
		if taskID == "" {
			continue
		}
		source, resolved := sources[taskID]
		if !resolved {
			source = s.taskSource(ctx, tenantID, job.JobID, taskID)
			sources[taskID] = source
		}
		if source == nil {
			continue
		}
		enriched := *job
		enriched.Source = source
		jobs[i] = &enriched
	}
}

// taskSource is the origin recorded on one job's task, nil when the task
// cannot be read or records none.
func (s *agentServiceImpl) taskSource(ctx context.Context, tenantID, jobID, taskID string) *TaskSourceRef {
	task, err := s.taskStore.GetTask(ctx, tenantID, taskID)
	if err != nil {
		s.logger.Debugf("orchestration: job %s: task %s not readable for its source: %s",
			jobID, taskID, err.Error())
		return nil
	}
	if task == nil {
		return nil
	}
	return task.Source
}
