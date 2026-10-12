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
	"os"
	"path/filepath"
	"testing"
)

// failingConclusions is a resolver that cannot tell.
type failingConclusions struct{}

var errConclusionUnknown = errors.New("store unavailable")

func (failingConclusions) WorkspaceConcluded(context.Context, string, string, string) (bool, error) {
	return false, errConclusionUnknown
}

// seedTask stores a task of the project and drives it to the status.
func seedConclusionTask(t *testing.T, store TaskStore, id string, status TaskStatus) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateTask(ctx, &Task{ID: id, TenantID: "t1", ProjectID: "p1", Subject: id}); err != nil {
		t.Fatalf("CreateTask %s: %v", id, err)
	}
	switch status {
	case TaskStatusCompleted:
		if err := store.ClaimTask(ctx, "t1", id, "agent"); err != nil {
			t.Fatalf("ClaimTask: %v", err)
		}
		if err := store.CompleteTask(ctx, "t1", id, &TaskResult{Output: "ok"}); err != nil {
			t.Fatalf("CompleteTask: %v", err)
		}
	case TaskStatusFailed:
		if err := store.ClaimTask(ctx, "t1", id, "agent"); err != nil {
			t.Fatalf("ClaimTask: %v", err)
		}
		if err := store.FailTask(ctx, "t1", id, "boom"); err != nil {
			t.Fatalf("FailTask: %v", err)
		}
	}
}

func provisionKeys(t *testing.T, m *WorkspaceManager, keys ...string) map[string]string {
	t.Helper()
	dirs := make(map[string]string, len(keys))
	for _, key := range keys {
		p, err := m.ProvisionSession(&SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: key})
		if err != nil {
			t.Fatalf("ProvisionSession %s: %v", key, err)
		}
		// The session that made it has ended: nothing holds the workspace.
		p.Release()
		dirs[key] = p.Dir
	}
	return dirs
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// reviewKeyLikeAttempt is a review checkout's key whose trailing hash reads
// as an attempt suffix.
const reviewKeyLikeAttempt = "review-7-abcdef0-default-a1234567"

// TestSweepSessionSpecs_RemovesConcludedWorkspacesOnly: a completed and a
// failed task's workspaces go, and so does that of a task the store no longer
// holds; a pending task's and a key that is no task's attempt stay.
func TestSweepSessionSpecs_RemovesConcludedWorkspacesOnly(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())
	seedConclusionTask(t, store, "done", TaskStatusCompleted)
	seedConclusionTask(t, store, "dead", TaskStatusFailed)
	seedConclusionTask(t, store, "live", TaskStatusPending)
	dirs := provisionKeys(t, m, "done-a1", "done-a2", "dead-a1", "live-a1", "ghost-a1", "review-pr7", reviewKeyLikeAttempt)
	m.SetConclusions(TaskWorkspaceConclusions{Store: store})

	removed, err := m.SweepSessionSpecs()
	if err != nil {
		t.Fatalf("SweepSessionSpecs: %v", err)
	}
	if len(removed) != 4 {
		t.Fatalf("removed %v, want the four concluded workspaces", removed)
	}
	for _, key := range []string{"done-a1", "done-a2", "dead-a1", "ghost-a1"} {
		if pathExists(dirs[key]) {
			t.Errorf("workspace %s of a concluded or deleted task survived", key)
		}
	}
	for _, key := range []string{"live-a1", "review-pr7", reviewKeyLikeAttempt} {
		if !pathExists(dirs[key]) {
			t.Errorf("workspace %s was removed though it belongs to no concluded task", key)
		}
	}
}

// TestSweepConcludedWorkspaces_KeepsSpecsAndHeldWorkspaces: the periodic sweep
// runs while sessions are live, so it leaves spec directories and every
// workspace a session holds, and removes the rest of the concluded ones.
func TestSweepConcludedWorkspaces_KeepsSpecsAndHeldWorkspaces(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())
	seedConclusionTask(t, store, "done", TaskStatusCompleted)
	dirs := provisionKeys(t, m, "done-a1")
	held, err := m.ProvisionSession(&SessionWorkspace{
		TenantID: "t1", ProjectID: "p1", Key: "done-a2",
		Spec: &WorkspaceSpecFiles{Files: map[string][]byte{WorkspaceSpecFile: []byte("{}")}},
	})
	if err != nil {
		t.Fatalf("ProvisionSession: %v", err)
	}
	m.SetConclusions(TaskWorkspaceConclusions{Store: store})

	removed, err := m.SweepConcludedWorkspaces()
	if err != nil {
		t.Fatalf("SweepConcludedWorkspaces: %v", err)
	}
	if len(removed) != 1 || pathExists(dirs["done-a1"]) {
		t.Fatalf("removed %v, want only the released done-a1", removed)
	}
	if !pathExists(held.Dir) || !pathExists(held.SpecDir) {
		t.Fatal("the periodic sweep took a held workspace or its spec directory")
	}

	held.Release()
	if removed, err := m.SweepConcludedWorkspaces(); err != nil || len(removed) != 1 || pathExists(held.Dir) {
		t.Fatalf("after release: removed %v, err %v; want done-a2 removed", removed, err)
	}
}

// TestSweepSessionSpecs_KeepsEveryWorkspaceWhenItCannotTell: fail closed — no
// resolver, or one that errors, removes nothing.
func TestSweepSessionSpecs_KeepsEveryWorkspaceWhenItCannotTell(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	dirs := provisionKeys(t, m, "done-a1")

	for name, c := range map[string]WorkspaceConclusions{"none": nil, "erroring": failingConclusions{}} {
		m.SetConclusions(c)
		removed, err := m.SweepSessionSpecs()
		if err != nil || len(removed) != 0 {
			t.Fatalf("%s resolver: removed %v, err %v; want nothing", name, removed, err)
		}
		if !pathExists(dirs["done-a1"]) {
			t.Fatalf("%s resolver: workspace removed", name)
		}
	}
}

// TestTaskWorkspaceConclusions_ProjectMismatchAndBlockedAreKept: a key whose
// task belongs to another project, and a parked (blocked) task, are not over.
func TestTaskWorkspaceConclusions_ProjectMismatchAndBlockedAreKept(t *testing.T) {
	t.Parallel()
	store := NewTaskStore(NewMemoryTaskKV(), NewProcessClaimArbiter())
	seedConclusionTask(t, store, "done", TaskStatusCompleted)
	c := TaskWorkspaceConclusions{Store: store}
	ctx := context.Background()

	if got, err := c.WorkspaceConcluded(ctx, "t1", "p1", "done-a1"); err != nil || !got {
		t.Fatalf("own project: got %v, %v; want true", got, err)
	}
	if got, err := c.WorkspaceConcluded(ctx, "t1", "other", "done-a1"); err != nil || got {
		t.Fatalf("other project: got %v, %v; want false", got, err)
	}
	if got, err := c.WorkspaceConcluded(ctx, "t1", "p1", "ghost-a1"); err != nil || !got {
		t.Fatalf("task the store does not hold: got %v, %v; want true", got, err)
	}
	for _, key := range []string{"done", "-a1", "done-a0", "done-ax", reviewKeyLikeAttempt} {
		if got, err := c.WorkspaceConcluded(ctx, "t1", "p1", key); err != nil || got {
			t.Fatalf("key %q: got %v, %v; want false", key, got, err)
		}
	}
	if got, err := (TaskWorkspaceConclusions{}).WorkspaceConcluded(ctx, "t1", "p1", "done-a1"); err != nil || got {
		t.Fatalf("no store: got %v, %v; want false", got, err)
	}
}

// TestRemoveConfined_RefusesAPathOutsideTheRoot: a path that climbs out of the
// root is refused and the file it names is untouched.
func TestRemoveConfined_RefusesAPathOutsideTheRoot(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	inside := filepath.Join(base, "root")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(inside)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	for _, rel := range []string{"../outside", outside, ".."} {
		var escape *WorkspaceEscapeError
		if err := removeConfined(root, rel); !errors.As(err, &escape) {
			t.Fatalf("removeConfined(%q) = %v, want WorkspaceEscapeError", rel, err)
		}
	}
	if !pathExists(outside) {
		t.Fatal("a directory outside the root was removed")
	}

	if err := os.Mkdir(filepath.Join(inside, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removeConfined(root, "x"); err != nil || pathExists(filepath.Join(inside, "x")) {
		t.Fatalf("removeConfined of a local path: err %v", err)
	}
}
