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

// RemoveWorkspace makes a fleet instance a WorkspaceRemover: it removes the
// workspace under its OWN projects root, as a fleet node's RPC does.
func (b *workspaceBackend) RemoveWorkspace(_ context.Context, ws *SessionWorkspace) (bool, error) {
	if b.agentRoot == nil {
		return false, &SessionWorkspaceUnsupportedError{Host: b.host}
	}
	return b.agentRoot.RemoveSessionWorkspace(ws.TenantID, ws.ProjectID, ws.Key)
}

// releasedWorkspace provisions key for t1/p1 on m and releases it, as a
// concluded session leaves it.
func releasedWorkspace(t *testing.T, m *WorkspaceManager, key string) string {
	t.Helper()
	provisioned, err := m.ProvisionSession(&SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: key})
	if err != nil {
		t.Fatalf("ProvisionSession: %v", err)
	}
	provisioned.Release()
	return provisioned.Dir
}

// pinnedWorkspace is the workspace a pin places for t1/p1.
func pinnedWorkspace(pin WorkspacePlacement) *SessionWorkspace {
	return &SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: pin.Key, Pin: &pin}
}

// TestDiscardWorkspace_RemovesOnTheDaemon: a workspace the daemon holds is
// removed from the daemon's own projects root.
func TestDiscardWorkspace_RemovesOnTheDaemon(t *testing.T) {
	t.Parallel()
	impl, daemonRoot := workspaceSvc(t, newWorkspaceBackend("local", "", nil), nil)
	dir := releasedWorkspace(t, daemonRoot, "task-a1")

	if err := impl.DiscardWorkspace(tenantCtx("t1"), pinnedWorkspace(WorkspacePlacement{Key: "task-a1", Dir: dir})); err != nil {
		t.Fatalf("DiscardWorkspace: %v", err)
	}
	if pathExists(dir) {
		t.Fatal("the daemon's workspace is still there")
	}
}

// TestDiscardWorkspace_RemovesOnTheFleetNodeThatHoldsIt: a workspace a fleet
// node holds is removed by THAT node, reached with strict host affinity, and
// the daemon's root is never touched.
func TestDiscardWorkspace_RemovesOnTheFleetNodeThatHoldsIt(t *testing.T) {
	t.Parallel()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	f := &fleet{agents: []*workspaceBackend{agent09}}
	impl, daemonRoot := workspaceSvc(t, newWorkspaceBackend("local", "", nil), f)
	agentDir := releasedWorkspace(t, agent09.agentRoot, "task-a1")
	daemonDir := releasedWorkspace(t, daemonRoot, "task-a1")

	pin := WorkspacePlacement{AgentHost: "agent-09:9091", Backend: "codex", Key: "task-a1", Dir: agentDir}
	if err := impl.DiscardWorkspace(tenantCtx("t1"), pinnedWorkspace(pin)); err != nil {
		t.Fatalf("DiscardWorkspace: %v", err)
	}
	if pathExists(agentDir) {
		t.Fatal("the fleet node's workspace is still there")
	}
	if !pathExists(daemonDir) {
		t.Fatal("a fleet node's removal touched the daemon's root")
	}
	if len(f.strict) == 0 || !f.strict[len(f.strict)-1] {
		t.Fatalf("the removal was not routed with strict host affinity: %v", f.strict)
	}
}

// TestDiscardWorkspace_RefusesWhatItCannotPlace: no pin, a pin recorded
// without its backend, and a node the fleet cannot reach are each a
// WorkspaceRemovalError, and nothing is removed.
func TestDiscardWorkspace_RefusesWhatItCannotPlace(t *testing.T) {
	t.Parallel()
	agent09 := newWorkspaceBackend("agent-09", "agent-09:9091", newTestWorkspaceManager(t))
	f := &fleet{agents: []*workspaceBackend{agent09}}
	impl, _ := workspaceSvc(t, newWorkspaceBackend("local", "", nil), f)
	agentDir := releasedWorkspace(t, agent09.agentRoot, "task-a1")

	unpinned := &SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: "task-a1"}
	requireRemovalRefused[*SessionWorkspaceInvalidError](t, impl.DiscardWorkspace(tenantCtx("t1"), unpinned))

	legacy := WorkspacePlacement{AgentHost: "agent-09:9091", Key: "task-a1", Dir: agentDir}
	requireRemovalRefused[*WorkspaceFleetNodeUnavailableError](t, impl.DiscardWorkspace(tenantCtx("t1"), pinnedWorkspace(legacy)))

	gone := WorkspacePlacement{AgentHost: "agent-77:9091", Backend: "codex", Key: "task-a1", Dir: agentDir}
	requireRemovalRefused[*WorkspaceFleetNodeUnavailableError](t, impl.DiscardWorkspace(tenantCtx("t1"), pinnedWorkspace(gone)))

	if !pathExists(agentDir) {
		t.Fatal("a refused discard removed the workspace")
	}
}

// TestDiscardJobWorkspaces_RemovesWhatEachJobPinned: every job's pinned
// workspace is removed; a job with no record or no pin has nothing to remove,
// and a failure names its job without stopping the others.
func TestDiscardJobWorkspaces_RemovesWhatEachJobPinned(t *testing.T) {
	t.Parallel()
	daemonRoot := newTestWorkspaceManager(t)
	first := releasedWorkspace(t, daemonRoot, "task-a1")
	second := releasedWorkspace(t, daemonRoot, "task-a2")

	pinned := func(jobID, key, dir string) *JobStatus {
		status := concludedJob(jobID, "t1", "task", 1, 100)
		status.Config.ProjectID = "p1"
		status.WorkspacePin = &WorkspacePlacement{Key: key, Dir: dir}
		return status
	}
	unpinned := concludedJob("job-3", "t1", "task", 3, 300)
	unreachable := pinned("job-4", "task-a4", "/elsewhere")
	unreachable.WorkspacePin.AgentHost = "agent-77:9091"

	svc, _ := concludedJobService(t, "t1", pinned("job-1", "task-a1", first), pinned("job-2", "task-a2", second), unpinned, unreachable)
	svc.SetWorkspaceProvisioner(daemonRoot)

	err := svc.DiscardJobWorkspaces(tenantCtx("t1"), "t1", []string{"job-1", "job-4", "job-2", "job-3", "job-missing"})
	var removal *WorkspaceRemovalError
	if !errors.As(err, &removal) || removal.JobID != "job-4" {
		t.Fatalf("err = %v, want a WorkspaceRemovalError naming job-4", err)
	}
	if pathExists(first) || pathExists(second) {
		t.Fatal("a pinned workspace is still there")
	}
	if err := svc.DiscardJobWorkspaces(tenantCtx("t1"), "t1", nil); err != nil {
		t.Fatalf("no jobs: %v", err)
	}
}

// escalatedTaskService is a service whose task "task-1", blocked after two
// attempts, kept the workspace of each on the daemon. It returns the
// directories, oldest first.
func escalatedTaskService(t *testing.T) (*agentServiceImpl, []string) {
	t.Helper()
	store := NewInMemoryTaskStore()
	seedTaskInStatus(t, store, "t1", "task-1", TaskStatusBlocked)
	task, err := store.GetTask(context.Background(), "t1", "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Metadata == nil {
		task.Metadata = map[string]string{}
	}
	task.Metadata[attemptHistoryKey] = "job-1,job-2"
	if err := store.UpdateTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	_, impl := newTaskServiceWithStore(t, store)
	impl.kvStore = newRoutingKVStore()
	daemonRoot := newTestWorkspaceManager(t)
	impl.SetWorkspaceProvisioner(daemonRoot)
	var dirs []string
	for i, jobID := range []string{"job-1", "job-2"} {
		key := "task-1-a" + string(rune('1'+i))
		dir := releasedWorkspace(t, daemonRoot, key)
		status := concludedJob(jobID, "t1", "task-1", i+1, int64(100*(i+1)))
		status.Config.ProjectID = "p1"
		status.WorkspacePin = &WorkspacePlacement{Key: key, Dir: dir}
		impl.persistJobStatus("t1", status)
		dirs = append(dirs, dir)
	}
	return impl, dirs
}

// TestReadmitTask_RemovesTheWorkspacesTheAttemptsKept: the escalated
// attempt's workspace, kept for the person the task waited on, goes when the
// task is readmitted — the next attempt checks out under a key of its own.
func TestReadmitTask_RemovesTheWorkspacesTheAttemptsKept(t *testing.T) {
	t.Parallel()
	impl, dirs := escalatedTaskService(t)

	if err := impl.ReadmitTask(tenantCtx("t1"), "t1", "task-1"); err != nil {
		t.Fatalf("ReadmitTask: %v", err)
	}
	for _, dir := range dirs {
		if pathExists(dir) {
			t.Fatalf("%s survived the readmission", dir)
		}
	}
}

// TestDeleteTask_RemovesTheWorkspacesTheAttemptsKept: a deleted task's kept
// workspaces go with it, and a removal that fails does not fail the delete.
func TestDeleteTask_RemovesTheWorkspacesTheAttemptsKept(t *testing.T) {
	t.Parallel()
	impl, dirs := escalatedTaskService(t)
	unreachable := concludedJob("job-2", "t1", "task-1", 2, 200)
	unreachable.Config.ProjectID = "p1"
	unreachable.WorkspacePin = &WorkspacePlacement{AgentHost: "agent-77:9091", Backend: "codex", Key: "task-1-a2", Dir: dirs[1]}
	impl.persistJobStatus("t1", unreachable)

	if err := impl.DeleteTask(tenantCtx("t1"), "t1", "task-1"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if pathExists(dirs[0]) {
		t.Fatal("the first attempt's workspace survived the delete")
	}
	if _, err := impl.GetTask(tenantCtx("t1"), "t1", "task-1"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("the task survived a delete whose removal failed: %v", err)
	}
}
