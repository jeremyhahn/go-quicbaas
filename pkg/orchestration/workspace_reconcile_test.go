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
	"testing"
)

// remoteReconcileService is a service over one fleet node, agent-09, with a
// task of tenant t1 in status whose attempts ran as jobIDs, the last one as
// the task's latest attempt. Each attempt pinned a released workspace on the
// node; the directories are returned in attempt order.
func remoteReconcileService(t *testing.T, status TaskStatus, jobIDs ...string) (*agentServiceImpl, *fleet, []string) {
	t.Helper()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	f := &fleet{agents: []*workspaceBackend{agent09}}
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), f)
	impl.kvStore = newRoutingKVStore()
	store := NewInMemoryTaskStore()
	impl.SetTaskStore(store)
	impl.SetTenantLister(&fakeTenantLister{ids: []string{"t1"}})

	task := seedTaskInStatus(t, store, "t1", "task-1", status)
	if task.Metadata == nil {
		task.Metadata = map[string]string{}
	}
	last := len(jobIDs) - 1
	for _, earlier := range jobIDs[:last] {
		if task.Metadata[attemptHistoryKey] != "" {
			task.Metadata[attemptHistoryKey] += ","
		}
		task.Metadata[attemptHistoryKey] += earlier
	}
	task.SpawnedJobID = jobIDs[last]
	if err := store.UpdateTask(context.Background(), task); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	dirs := make([]string, 0, len(jobIDs))
	for i, jobID := range jobIDs {
		key := "task-1-a" + string(rune('1'+i))
		dir := releasedWorkspace(t, agent09.agentRoot, key)
		job := concludedJob(jobID, "t1", "task-1", i+1, int64(100*(i+1)))
		job.Config.ProjectID = "p1"
		job.WorkspacePin = &WorkspacePlacement{AgentHost: "agent-09:9091", Backend: "codex", Key: key, Dir: dir}
		impl.persistJobStatus("t1", job)
		dirs = append(dirs, dir)
	}
	return impl, f, dirs
}

// setOffline takes the fleet node off the fleet, or brings it back.
func (f *fleet) setOffline(offline bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offline = offline
}

// resolutions counts the fleet lookups made so far.
func (f *fleet) resolutions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.strict)
}

// TestReconcileRemoteWorkspaces_RetriesAFailedRemovalNextSweep: a removal
// the node was down for fails the sweep and is asked again on the next one,
// and once the node confirms it the job is settled and never asked again.
func TestReconcileRemoteWorkspaces_RetriesAFailedRemovalNextSweep(t *testing.T) {
	t.Parallel()
	impl, f, dirs := remoteReconcileService(t, TaskStatusCompleted, "job-1")

	f.setOffline(true)
	report, err := impl.ReconcileRemoteWorkspaces(context.Background())
	var removal *WorkspaceRemovalError
	if !errors.As(err, &removal) || removal.JobID != "job-1" {
		t.Fatalf("an unreachable node's removal was not reported for its job: %v", err)
	}
	if report.Removed != 0 || !pathExists(dirs[0]) {
		t.Fatalf("an unreachable node's workspace was reported removed: %+v", report)
	}

	f.setOffline(false)
	report, err = impl.ReconcileRemoteWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if report.Removed != 1 || pathExists(dirs[0]) {
		t.Fatalf("the retried removal did not remove the workspace: %+v", report)
	}

	asked := f.resolutions()
	report, err = impl.ReconcileRemoteWorkspaces(context.Background())
	if err != nil || report.Removed != 0 {
		t.Fatalf("a settled job was reconciled again: %+v, %v", report, err)
	}
	if f.resolutions() != asked {
		t.Fatal("a settled job's node was asked again")
	}
}

// TestReconcileRemoteWorkspaces_RemovesEveryAttemptOfAFailedTask: a failed
// task's workspaces go too, its latest attempt's as well as the earlier ones.
func TestReconcileRemoteWorkspaces_RemovesEveryAttemptOfAFailedTask(t *testing.T) {
	t.Parallel()
	impl, _, dirs := remoteReconcileService(t, TaskStatusFailed, "job-1", "job-2")

	report, err := impl.ReconcileRemoteWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("ReconcileRemoteWorkspaces: %v", err)
	}
	if report.Removed != 2 {
		t.Fatalf("Removed = %d, want 2", report.Removed)
	}
	for i, dir := range dirs {
		if pathExists(dir) {
			t.Fatalf("attempt %d's workspace survived the reconcile", i+1)
		}
	}
}

// TestReconcileRemoteWorkspaces_LeavesLiveTasksAndDaemonWorkspaces: a task
// still running keeps its workspace, and a workspace on the daemon is left
// to the daemon's own sweep.
func TestReconcileRemoteWorkspaces_LeavesLiveTasksAndDaemonWorkspaces(t *testing.T) {
	t.Parallel()
	impl, f, dirs := remoteReconcileService(t, TaskStatusInProgress, "job-1")

	report, err := impl.ReconcileRemoteWorkspaces(context.Background())
	if err != nil || report.Removed != 0 || !pathExists(dirs[0]) {
		t.Fatalf("a running task's workspace was reconciled: %+v, %v", report, err)
	}

	daemon, daemonFleet, daemonDirs := remoteReconcileService(t, TaskStatusCompleted, "job-1")
	onDaemon := concludedJob("job-1", "t1", "task-1", 1, 100)
	onDaemon.Config.ProjectID = "p1"
	onDaemon.WorkspacePin = &WorkspacePlacement{Key: "task-1-a1", Dir: daemonDirs[0]}
	daemon.persistJobStatus("t1", onDaemon)
	report, err = daemon.ReconcileRemoteWorkspaces(context.Background())
	if err != nil || report.Removed != 0 {
		t.Fatalf("a daemon workspace was reconciled as a fleet node's: %+v, %v", report, err)
	}
	if f.resolutions() != 0 || daemonFleet.resolutions() != 0 {
		t.Fatal("a fleet node was asked about a workspace it does not hold")
	}
}

// TestReconcileRemoteWorkspaces_ReportsATenantListFailure: a tenant manager
// that cannot list its tenants fails the reconcile with its own error, and a
// service with no task store has nothing to reconcile.
func TestReconcileRemoteWorkspaces_ReportsATenantListFailure(t *testing.T) {
	t.Parallel()
	impl, _, _ := remoteReconcileService(t, TaskStatusCompleted, "job-1")
	listErr := errors.New("tenant manager unavailable")
	impl.SetTenantLister(&fakeTenantLister{err: listErr})
	if _, err := impl.ReconcileRemoteWorkspaces(context.Background()); !errors.Is(err, listErr) {
		t.Fatalf("err = %v, want the tenant list failure", err)
	}

	bare, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), nil)
	bare.taskStore = nil
	if report, err := bare.ReconcileRemoteWorkspaces(context.Background()); err != nil || report.Removed != 0 {
		t.Fatalf("a service with no task store reconciled: %+v, %v", report, err)
	}
}
