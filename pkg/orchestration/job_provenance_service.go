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
	if status == nil || status.TaskID == "" || s.taskStore == nil {
		return status, nil
	}
	task, err := s.taskStore.GetTask(ctx, tenantID, status.TaskID)
	if err != nil {
		s.logger.Debugf("orchestration: job %s: task %s not readable for provenance: %s",
			status.JobID, status.TaskID, err.Error())
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
