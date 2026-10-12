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
	"slices"
	"sync"
	"time"
)

// =============================================================================
// Reconciling fleet node workspaces of concluded tasks
// =============================================================================
//
// A job's workspace on a fleet node is removed when its task concludes, but
// that removal is one RPC to a node that may be down. The daemon's own sweep
// only sees its own root, so a failed removal would leave the workspace on
// the node for good. The pin is durable on the job's record, so the sweep
// reads it back from there and asks the node again, every tick, until the
// node confirms the workspace gone. Nothing new is persisted: what this
// remembers in memory only saves repeat work, and a restart re-derives it.

const (
	// remoteWorkspaceReconcileWindow is how long after a task concludes its
	// fleet node workspaces are still reconciled. A node away for longer is
	// left to its own operator.
	remoteWorkspaceReconcileWindow = 7 * 24 * time.Hour

	// remoteWorkspaceReconcileLimit bounds the jobs one sweep looks up and
	// asks a node about, so a fleet-wide outage cannot stall a tick.
	remoteWorkspaceReconcileLimit = 64

	// remoteWorkspaceRemovalTimeout bounds one RemoveWorkspace RPC.
	remoteWorkspaceRemovalTimeout = 30 * time.Second
)

// concludedTaskStatuses are the statuses whose workspaces are no longer
// needed.
var concludedTaskStatuses = []TaskStatus{TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled}

// RemoteWorkspaceReport is what one reconcile did.
type RemoteWorkspaceReport struct {
	// Removed counts the fleet node workspaces the nodes confirmed removed.
	Removed int
}

// remoteJob names one job of one tenant.
type remoteJob struct {
	tenantID string
	jobID    string
}

// remoteWorkspaceLedger is the in-memory memo of a reconcile. Its zero value
// is ready to use.
type remoteWorkspaceLedger struct {
	// mu serialises reconciles and guards the maps.
	mu sync.Mutex

	// settled holds the jobs with nothing left to remove.
	settled map[remoteJob]struct{}

	// tried holds when a removal last failed, so the oldest failures go
	// first once the limit bites.
	tried map[remoteJob]time.Time
}

// ReconcileRemoteWorkspaces asks fleet nodes again to remove the workspaces
// of tasks that concluded within remoteWorkspaceReconcileWindow, failed ones
// included. A job is settled once its node confirms the removal, or once it
// pins nothing on a fleet node; the daemon's own workspaces are its own
// sweep's. Every failure is joined into the error and retried next call.
func (s *agentServiceImpl) ReconcileRemoteWorkspaces(ctx context.Context) (RemoteWorkspaceReport, error) {
	var report RemoteWorkspaceReport
	if s.taskStore == nil {
		return report, nil
	}
	ledger := &s.remoteWorkspaces
	ledger.mu.Lock()
	defer ledger.mu.Unlock()

	pending, err := s.unsettledRemoteJobs(ctx, ledger)
	errs := []error{err}
	for _, job := range pending {
		if ctx.Err() != nil {
			errs = append(errs, ctx.Err())
			break
		}
		removed, removeErr := s.reconcileRemoteJob(ctx, job)
		if removeErr != nil {
			ledger.tried[job] = time.Now()
			errs = append(errs, removeErr)
			continue
		}
		ledger.settled[job] = struct{}{}
		delete(ledger.tried, job)
		if removed {
			report.Removed++
		}
	}
	return report, errors.Join(errs...)
}

// unsettledRemoteJobs lists the attempts of recently concluded tasks that are
// not yet settled, never-tried and longest-failed first, up to
// remoteWorkspaceReconcileLimit. It forgets every job no longer listed.
func (s *agentServiceImpl) unsettledRemoteJobs(ctx context.Context, ledger *remoteWorkspaceLedger) ([]remoteJob, error) {
	tenantIDs, err := s.maintenanceTenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-remoteWorkspaceReconcileWindow).UnixNano()
	listed := make(map[remoteJob]struct{})
	var pending []remoteJob
	var errs []error
	for _, tenantID := range tenantIDs {
		tasks, listErr := s.taskStore.ListTasks(ContextWithTenant(ctx, tenantID), tenantID,
			&TaskFilter{Statuses: concludedTaskStatuses})
		if listErr != nil {
			errs = append(errs, listErr)
			continue
		}
		for _, task := range tasks {
			if task.UpdatedAt < since {
				continue
			}
			for _, jobID := range taskAttemptJobs(task) {
				job := remoteJob{tenantID: tenantID, jobID: jobID}
				listed[job] = struct{}{}
				if _, done := ledger.settled[job]; !done {
					pending = append(pending, job)
				}
			}
		}
	}
	// A tenant that failed to list keeps what the ledger knew of it.
	if len(errs) == 0 {
		ledger.forgetExcept(listed)
	}
	ledger.init()
	slices.SortStableFunc(pending, func(a, b remoteJob) int {
		return ledger.tried[a].Compare(ledger.tried[b])
	})
	if len(pending) > remoteWorkspaceReconcileLimit {
		pending = pending[:remoteWorkspaceReconcileLimit]
	}
	return pending, errors.Join(errs...)
}

// reconcileRemoteJob removes the fleet node workspace one job pins, reporting
// whether there was one to ask the node about.
func (s *agentServiceImpl) reconcileRemoteJob(ctx context.Context, job remoteJob) (bool, error) {
	tenantCtx := ContextWithTenant(ctx, job.tenantID)
	ws, err := s.pinnedJobWorkspace(tenantCtx, job.tenantID, job.jobID)
	if err != nil {
		return false, err
	}
	if ws == nil || ws.Pin.OnDaemon() {
		return false, nil
	}
	rpcCtx, cancel := context.WithTimeout(tenantCtx, remoteWorkspaceRemovalTimeout)
	defer cancel()
	if err := s.discardPinnedJobWorkspace(rpcCtx, job.jobID, ws); err != nil {
		return false, err
	}
	return true, nil
}

// maintenanceTenantIDs lists the tenants a maintenance pass visits: the
// system tenant, which the tenant manager does not list, and every tenant it
// does. Without a tenant lister the system tenant is the only one.
func (s *agentServiceImpl) maintenanceTenantIDs(ctx context.Context) ([]string, error) {
	if s.tenants == nil {
		return []string{SystemTenantID}, nil
	}
	listed, err := s.tenants.ListTenantIDs(ctx)
	if err != nil {
		return nil, err
	}
	return dedupeTenants(append([]string{SystemTenantID}, listed...)), nil
}

// init makes the ledger's maps.
func (l *remoteWorkspaceLedger) init() {
	if l.settled == nil {
		l.settled = make(map[remoteJob]struct{})
	}
	if l.tried == nil {
		l.tried = make(map[remoteJob]time.Time)
	}
}

// forgetExcept drops every job not in keep, so the ledger never outgrows the
// window.
func (l *remoteWorkspaceLedger) forgetExcept(keep map[remoteJob]struct{}) {
	for job := range l.settled {
		if _, ok := keep[job]; !ok {
			delete(l.settled, job)
		}
	}
	for job := range l.tried {
		if _, ok := keep[job]; !ok {
			delete(l.tried, job)
		}
	}
}
