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

package orchestration

import (
	"context"
	"errors"
)

// =============================================================================
// Discarding a job's workspace — removed on the host that holds it
// =============================================================================
//
// A job's workspace lives on one host, recorded as the job's pin, so it is
// removed THERE: on the daemon through its own WorkspaceManager, on a fleet
// node through that node's RemoveWorkspace RPC, reached the way a pinned spawn
// is — strict host affinity to the pinned agent, over the backend the session
// ran on. The node removes it under its own projects root with the same
// confinement and mount-policy check the daemon applies.

// WorkspaceRemover is a backend that can remove a session workspace on the
// host it fronts. RemoteACPBackend implements it for fleet nodes.
type WorkspaceRemover interface {
	// RemoveWorkspace removes the named workspace on the backend's host,
	// reporting whether there was one to remove.
	RemoveWorkspace(ctx context.Context, ws *SessionWorkspace) (bool, error)
}

// WorkspaceDiscarder removes the session workspaces jobs provisioned. The
// agent service implements it; a runner reaches it through JobRun.
type WorkspaceDiscarder interface {
	// DiscardWorkspace removes the workspace a pin places, on the host it
	// names.
	DiscardWorkspace(ctx context.Context, ws *SessionWorkspace) error

	// DiscardJobWorkspaces removes the workspace each job's record pins. A
	// job with no record or no pin has nothing to remove.
	DiscardJobWorkspaces(ctx context.Context, tenantID string, jobIDs []string) error
}

var _ WorkspaceDiscarder = (*agentServiceImpl)(nil)

// DiscardWorkspace implements WorkspaceDiscarder. Every failure is a
// WorkspaceRemovalError.
func (s *agentServiceImpl) DiscardWorkspace(ctx context.Context, ws *SessionWorkspace) error {
	if err := ws.Validate(); err != nil {
		return &WorkspaceRemovalError{Cause: err}
	}
	if ws.Pin == nil {
		return &WorkspaceRemovalError{Key: ws.Key, Cause: &SessionWorkspaceInvalidError{
			Field:  "pin",
			Reason: "is required to locate the workspace",
		}}
	}
	if ws.Pin.OnDaemon() {
		if s.workspaces == nil {
			return &WorkspaceRemovalError{Key: ws.Key, Cause: &SessionWorkspaceUnsupportedError{Host: "daemon"}}
		}
		_, err := s.workspaces.RemoveSessionWorkspace(ws.TenantID, ws.ProjectID, ws.Key)
		return err
	}
	return s.discardOnAgent(ctx, ws)
}

// discardOnAgent asks the fleet node the pin names to remove the workspace.
func (s *agentServiceImpl) discardOnAgent(ctx context.Context, ws *SessionWorkspace) error {
	unavailable := &WorkspaceFleetNodeUnavailableError{AgentHost: ws.Pin.AgentHost, Key: ws.Key, Backend: ws.Pin.Backend}
	if ws.Pin.Backend == "" {
		return &WorkspaceRemovalError{Key: ws.Key, Path: ws.Pin.Dir, Cause: unavailable}
	}
	// Resolved through the fleet only: a fall-through to the daemon's own
	// backend would remove on the wrong host.
	if s.remoteResolver == nil {
		return &WorkspaceRemovalError{Key: ws.Key, Path: ws.Pin.Dir, Cause: unavailable}
	}
	routed := WithHostAffinityStrict(WithHostAffinity(ContextWithTenant(ctx, ws.TenantID), ws.Pin.AgentHost))
	backend, ok := s.remoteResolver(routed, ws.Pin.Backend, ws.ProjectID)
	if !ok || backend == nil {
		return &WorkspaceRemovalError{Key: ws.Key, Path: ws.Pin.Dir, Cause: unavailable}
	}
	remover, ok := backend.(WorkspaceRemover)
	if !ok {
		return &WorkspaceRemovalError{Key: ws.Key, Path: ws.Pin.Dir, Cause: unavailable}
	}
	if _, err := remover.RemoveWorkspace(routed, ws); err != nil {
		return &WorkspaceRemovalError{Key: ws.Key, Path: ws.Pin.Dir, Cause: err}
	}
	return nil
}

// DiscardJobWorkspaces implements WorkspaceDiscarder. It tries every job and
// joins the failures, each a WorkspaceRemovalError naming its job.
func (s *agentServiceImpl) DiscardJobWorkspaces(ctx context.Context, tenantID string, jobIDs []string) error {
	var errs []error
	for _, jobID := range jobIDs {
		if err := s.discardJobWorkspace(ctx, tenantID, jobID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// attemptJobs lists the jobs a task's attempts ran as, oldest first, for the
// readmit and delete verbs to discard their workspaces. A task that cannot be
// read lists none: the verb's own read reports the failure, and the sweep
// removes what this misses.
func (s *agentServiceImpl) attemptJobs(ctx context.Context, tenantID, taskID string) []string {
	task, err := s.taskStore.GetTask(ctx, tenantID, taskID)
	if err != nil {
		s.logger.Debugf("orchestration: task %s: attempt workspaces not listed: %s", taskID, err.Error())
		return nil
	}
	return taskAttemptJobs(task)
}

// taskAttemptJobs lists the jobs a task's attempts ran as, oldest first, the
// latest attempt's included.
func taskAttemptJobs(task *Task) []string {
	jobs := task.AttemptHistory()
	if task.SpawnedJobID != "" {
		jobs = append(jobs, task.SpawnedJobID)
	}
	return jobs
}

// discardAttemptWorkspaces removes the workspaces a readmitted or deleted
// task's attempts kept — the escalated attempt's above all, which was kept for
// the person the task waited on (go-quicbaas#777). A failure is logged and
// never fails the verb: the sweep retries it.
func (s *agentServiceImpl) discardAttemptWorkspaces(ctx context.Context, tenantID, taskID string, jobIDs []string) {
	if err := s.DiscardJobWorkspaces(ctx, tenantID, jobIDs); err != nil {
		s.logger.Warnf("orchestration: task %s: attempt workspaces not removed: %s", taskID, err.Error())
	}
}

// discardJobWorkspace removes the workspace one job's record pins.
func (s *agentServiceImpl) discardJobWorkspace(ctx context.Context, tenantID, jobID string) error {
	ws, err := s.pinnedJobWorkspace(ctx, tenantID, jobID)
	if err != nil || ws == nil {
		return err
	}
	return s.discardPinnedJobWorkspace(ctx, jobID, ws)
}

// pinnedJobWorkspace is the workspace one job's record pins, nil when the job
// has no record or pins none.
func (s *agentServiceImpl) pinnedJobWorkspace(ctx context.Context, tenantID, jobID string) (*SessionWorkspace, error) {
	status, err := s.GetJobStatus(ctx, tenantID, jobID)
	var missing *JobNotFoundError
	if errors.As(err, &missing) {
		return nil, nil
	}
	if err != nil {
		return nil, &WorkspaceRemovalError{JobID: jobID, Cause: err}
	}
	return jobWorkspace(tenantID, status), nil
}

// discardPinnedJobWorkspace removes a job's pinned workspace, naming the job
// on the failure.
func (s *agentServiceImpl) discardPinnedJobWorkspace(ctx context.Context, jobID string, ws *SessionWorkspace) error {
	err := s.DiscardWorkspace(ctx, ws)
	var removal *WorkspaceRemovalError
	if errors.As(err, &removal) && removal.JobID == "" {
		removal.JobID = jobID
	}
	return err
}

// jobWorkspace is the workspace a job's status pins, nil when it pins none.
func jobWorkspace(tenantID string, status *JobStatus) *SessionWorkspace {
	if status == nil || status.WorkspacePin == nil || status.Config == nil {
		return nil
	}
	pin := *status.WorkspacePin
	return &SessionWorkspace{TenantID: tenantID, ProjectID: status.Config.ProjectID, Key: pin.Key, Pin: &pin}
}

// DiscardWorkspace removes the workspace this job provisioned, on the host
// that holds it. A job that provisioned none, or whose service cannot remove
// one, has nothing to discard.
func (r *JobRun) DiscardWorkspace(ctx context.Context) error {
	discarder, ok := r.Agents.(WorkspaceDiscarder)
	if !ok {
		return nil
	}
	pin := r.WorkspacePin()
	if pin == nil || r.Config == nil {
		return nil
	}
	return discarder.DiscardWorkspace(ctx, &SessionWorkspace{
		TenantID: r.TenantID, ProjectID: r.Config.ProjectID, Key: pin.Key, Pin: pin,
	})
}

// DiscardJobWorkspaces removes the workspaces other jobs — the task's
// earlier attempts — provisioned.
func (r *JobRun) DiscardJobWorkspaces(ctx context.Context, jobIDs []string) error {
	discarder, ok := r.Agents.(WorkspaceDiscarder)
	if !ok || len(jobIDs) == 0 {
		return nil
	}
	return discarder.DiscardJobWorkspaces(ContextWithTenant(ctx, r.TenantID), r.TenantID, jobIDs)
}
