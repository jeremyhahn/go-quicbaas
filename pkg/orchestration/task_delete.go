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
	"time"
)

// =============================================================================
// DeleteTask — the service's side of removing a task
// =============================================================================
//
// THE STORE ALREADY KNOWS HOW TO REMOVE THE RECORD. taskStore.remove deletes
// the record, forgets the claim mark, RELEASES THE SOURCE RESERVATION so the
// task's source reference can be adopted again, and sweeps the artifact
// payloads. What the store cannot know is whether a job is running the task
// right now, because the lease that says so is the service's state, not the
// store's. This file is that one guard and nothing else.
//
// A TASK A JOB HOLDS A LIVE LEASE ON IS NOT DELETABLE. The job would keep
// running against a task that no longer exists, and on its return would try
// to conclude a record that is gone; worse, the source reservation the delete
// released would let a second task adopt the same source while the first
// attempt is still in flight. Refusing is the only answer that keeps the
// running job and the deleted record from disagreeing. A stale or released
// lease is no obstacle: the first is a holder that stopped proving it was
// alive (the reaper will conclude it), the second is a holder that finished.
//
// JOB BOOKKEEPING IS NOT REWRITTEN. A job record's TaskID states what the job
// was an attempt at, and that is a fact about the past which deleting the task
// does not change. Rewriting or deleting those records to erase the reference
// would falsify the account of work that actually ran, and it buys nothing:
// nothing dispatches from a concluded job's TaskID, and the live-lease guard
// above is what keeps a RUNNING job's task from being deleted out from under
// it. So a job whose task was deleted keeps its TaskID, and a reader that
// resolves it gets ErrTaskNotFound — the accurate answer.

// DeleteTask removes a task, its artifact payloads, its claim mark and its
// source reservation, refusing while a job holds a live lease on it.
func (s *agentServiceImpl) DeleteTask(ctx context.Context, tenantID, taskID string) error {
	if err := s.validateTenant(ctx); err != nil {
		return err
	}

	if s.taskStore == nil {
		return &AgentServiceError{Sentinel: ErrTaskStoreRequired}
	}

	if tenantID == "" {
		return &AgentServiceError{Sentinel: ErrTenantIDRequired}
	}
	if taskID == "" {
		return &AgentServiceError{Sentinel: ErrTaskIDRequired}
	}

	if err := s.refuseLeasedTask(ctx, tenantID, taskID); err != nil {
		return err
	}

	if err := s.taskStore.DeleteTask(ctx, tenantID, taskID); err != nil {
		return err
	}

	s.logger.Debugf("deleted task: id=%s tenant=%s", taskID, tenantID)

	s.publishEvent(ctx, tenantID, "task_deleted", map[string]string{
		"task_id": taskID,
	})

	return nil
}

// refuseLeasedTask reports a TaskLeasedError when a live job lease names the
// task.
//
// THE LISTING IS THE TENANT'S WHOLE LEASE SUBTREE RATHER THAN ONE PROJECT'S,
// and it stays that way now that a project id is validated as an id (#613).
// The reason that survives the fix is a stronger one: a lease's project
// segment is pinned when the job acquires it, while the task's ProjectID is a
// field UpdateTask rewrites, so a task moved between projects mid-flight
// leaves its live lease under the FORMER project's subtree. A narrowed listing
// would find nothing there and delete the record of a task a job is still
// running — the one outcome this guard exists to prevent. See
// TestAgentService_DeleteTask_RefusedWhenTheRecordWasRescoped.
//
// The caller of a delete also knows a task ID and nothing else, so narrowing
// would additionally cost a read of the record to learn a project the lease
// may not agree with. A delete is rare and a tenant's live leases are few, so
// the wider scan costs nothing worth trading correctness for.
//
// A service with no lease store CANNOT PROVE the task is unleased, so it
// refuses rather than deleting on an unchecked assumption.
func (s *agentServiceImpl) refuseLeasedTask(ctx context.Context, tenantID, taskID string) error {
	if s.jobLeases == nil {
		return &AgentServiceError{
			Sentinel: ErrJobLeaseStoreRequired,
			Message:  "a delete must prove no job is running the task; without a lease store it cannot",
		}
	}

	leases, err := s.jobLeases.List(ctx, tenantID, "")
	if err != nil {
		return err
	}

	ttl := s.leaseTTL
	if ttl <= 0 {
		ttl = DefaultJobLeaseTTL
	}
	now := time.Now().UTC()

	for _, lease := range leases {
		if lease == nil || lease.TaskID != taskID {
			continue
		}
		if lease.Live(now, ttl) {
			return &TaskLeasedError{
				TenantID: tenantID,
				TaskID:   taskID,
				JobID:    lease.JobID,
				Node:     lease.Node,
			}
		}
	}
	return nil
}
