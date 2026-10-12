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
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// requireRemovalRefused asserts err is a WorkspaceRemovalError whose cause is
// a T.
func requireRemovalRefused[T error](t *testing.T, err error) {
	t.Helper()
	var removal *WorkspaceRemovalError
	if !errors.As(err, &removal) || !errors.Is(err, ErrWorkspaceRemoval) {
		t.Fatalf("err = %v, want a WorkspaceRemovalError", err)
	}
	var cause T
	if !errors.As(err, &cause) {
		t.Fatalf("err = %v, want cause %T", err, cause)
	}
}

// TestRemoveSessionWorkspace_RemovesAndIsIdempotent: a released workspace is
// removed with its checkout; removing it again, or one never made, is a no-op.
func TestRemoveSessionWorkspace_RemovesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	dirs := provisionKeys(t, m, "task-a1")
	if err := os.WriteFile(filepath.Join(dirs["task-a1"], "README"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	removed, err := m.RemoveSessionWorkspace("t1", "p1", "task-a1")
	if err != nil || !removed || pathExists(dirs["task-a1"]) {
		t.Fatalf("first removal: removed %v, err %v", removed, err)
	}
	for _, args := range [][3]string{{"t1", "p1", "task-a1"}, {"t1", "nope", "task-a1"}, {"t9", "p1", "task-a1"}} {
		if removed, err := m.RemoveSessionWorkspace(args[0], args[1], args[2]); err != nil || removed {
			t.Fatalf("RemoveSessionWorkspace%v = %v, %v; want false, nil", args, removed, err)
		}
	}
}

// TestRemoveSessionWorkspace_RefusesAHeldWorkspace: a workspace a live session
// was provisioned into stays until every session on it has released it.
func TestRemoveSessionWorkspace_RefusesAHeldWorkspace(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	ws := &SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: "task-a1"}
	first, err := m.ProvisionSession(ws)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.ProvisionSession(ws)
	if err != nil {
		t.Fatal(err)
	}

	first.Release()
	_, err = m.RemoveSessionWorkspace("t1", "p1", "task-a1")
	requireRemovalRefused[*WorkspaceHeldError](t, err)
	if !pathExists(first.Dir) {
		t.Fatal("a held workspace was removed")
	}

	second.Release()
	if removed, err := m.RemoveSessionWorkspace("t1", "p1", "task-a1"); err != nil || !removed {
		t.Fatalf("after release: removed %v, err %v", removed, err)
	}
}

// provisionResult is one ProvisionSession call's outcome.
type provisionResult struct {
	provisioned *ProvisionedWorkspace
	err         error
}

// TestWorkspaceLock_ProvisioningThatWinsKeepsItsDirectory: a removal that
// arrives while provisioning has placed the directory but not yet held it
// waits for the hold, then refuses — the session keeps its checkout.
func TestWorkspaceLock_ProvisioningThatWinsKeepsItsDirectory(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	removal := make(chan error, 1)
	removalWaits := make(chan struct{})
	m.hooks = &workspaceHooks{
		contended: func(string) { close(removalWaits) },
		provisioned: func(string) {
			go func() {
				_, err := m.RemoveSessionWorkspace("t1", "p1", "task-a1")
				removal <- err
			}()
			// Serialised, the removal waits for this section; unserialised,
			// it runs to completion here.
			select {
			case <-removalWaits:
			case err := <-removal:
				removal <- err
			}
		},
	}

	provisioned, err := m.ProvisionSession(&SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: "task-a1"})
	if err != nil {
		t.Fatal(err)
	}
	defer provisioned.Release()
	requireRemovalRefused[*WorkspaceHeldError](t, <-removal)
	if !pathExists(provisioned.Dir) {
		t.Fatal("a removal racing provisioning deleted the live session's workspace")
	}
}

// TestWorkspaceLock_RemovalThatWinsLeavesProvisioningToRecreate: provisioning
// that arrives while a removal has found the workspace unheld waits for the
// delete, then recreates the directory and holds it.
func TestWorkspaceLock_RemovalThatWinsLeavesProvisioningToRecreate(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	ws := &SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: "task-a1"}
	earlier, err := m.ProvisionSession(ws)
	if err != nil {
		t.Fatal(err)
	}
	earlier.Release()

	provision := make(chan provisionResult, 1)
	provisionWaits := make(chan struct{})
	m.hooks = &workspaceHooks{
		contended: func(string) { close(provisionWaits) },
		beforeRemove: func(string) {
			go func() {
				p, err := m.ProvisionSession(ws)
				provision <- provisionResult{provisioned: p, err: err}
			}()
			select {
			case <-provisionWaits:
			case r := <-provision:
				provision <- r
			}
		},
	}

	if removed, err := m.RemoveSessionWorkspace("t1", "p1", "task-a1"); err != nil || !removed {
		t.Fatalf("removed %v, err %v; want the unheld workspace removed", removed, err)
	}
	r := <-provision
	if r.err != nil {
		t.Fatalf("provisioning after the removal: %v", r.err)
	}
	defer r.provisioned.Release()
	if !pathExists(r.provisioned.Dir) {
		t.Fatal("provisioning succeeded but its workspace is gone")
	}
	m.hooks = nil
	_, err = m.RemoveSessionWorkspace("t1", "p1", "task-a1")
	requireRemovalRefused[*WorkspaceHeldError](t, err)
}

// TestWorkspaceLock_IsPerWorkspace: a lock taken on one workspace does not
// stop work on another, and the entry goes when its last user leaves.
func TestWorkspaceLock_IsPerWorkspace(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	unlock := m.lockWorkspace(filepath.Join(m.Root(), "t1", "p1", "other"))

	provisioned, err := m.ProvisionSession(&SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: "task-a1"})
	if err != nil {
		t.Fatal(err)
	}
	provisioned.Release()
	if removed, err := m.RemoveSessionWorkspace("t1", "p1", "task-a1"); err != nil || !removed {
		t.Fatalf("removed %v, err %v", removed, err)
	}

	unlock()
	m.heldMu.Lock()
	defer m.heldMu.Unlock()
	if len(m.locks) != 0 {
		t.Fatalf("locks %v outlived their users", m.locks)
	}
}

// TestRemoveSessionWorkspace_NeverFollowsALink: a link planted at the key or
// at the project level is refused, and what it points at is untouched.
func TestRemoveSessionWorkspace_NeverFollowsALink(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	provisionKeys(t, m, "real-a1")
	victim := t.TempDir()
	bait := filepath.Join(victim, "keep")
	if err := os.WriteFile(bait, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace, err := m.WorkspacePath("t1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(workspace, "link-a1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(m.root, "t1", "p1"), filepath.Join(m.root, "t1", "alias")); err != nil {
		t.Fatal(err)
	}

	_, err = m.RemoveSessionWorkspace("t1", "p1", "link-a1")
	requireRemovalRefused[*WorkspaceLinkError](t, err)
	_, err = m.RemoveSessionWorkspace("t1", "alias", "real-a1")
	requireRemovalRefused[*WorkspaceLinkError](t, err)
	if !pathExists(bait) || !pathExists(filepath.Join(workspace, "real-a1")) {
		t.Fatal("a removal followed a link")
	}
}

// TestRemoveSessionWorkspace_RefusesWhatTheMountPolicyRefuses: the path is
// checked as the bind-mount source it was, so a manager whose policy does not
// admit its own root removes nothing.
func TestRemoveSessionWorkspace_RefusesWhatTheMountPolicyRefuses(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	dirs := provisionKeys(t, m, "task-a1")

	m.SetMountPolicy(&MountPolicy{ProjectsRoot: t.TempDir()})
	_, err := m.RemoveSessionWorkspace("t1", "p1", "task-a1")
	requireRemovalRefused[*MountNotAllowedError](t, err)
	if !pathExists(dirs["task-a1"]) {
		t.Fatal("a workspace the policy refuses was removed")
	}

	m.SetMountPolicy(&MountPolicy{ProjectsRoot: m.root})
	if removed, err := m.RemoveSessionWorkspace("t1", "p1", "task-a1"); err != nil || !removed {
		t.Fatalf("admitted by policy: removed %v, err %v", removed, err)
	}
}

// TestRemoveSessionWorkspace_RefusesABadName: a name that is no single path
// segment, or that names a spec directory, is refused before the filesystem
// is touched.
func TestRemoveSessionWorkspace_RefusesABadName(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	for _, args := range [][3]string{
		{"t1", "p1", "../p2"},
		{"t1", "..", "task-a1"},
		{"", "p1", "task-a1"},
		{"t1", "p1", workspaceSpecDirName + "00"},
	} {
		if _, err := m.RemoveSessionWorkspace(args[0], args[1], args[2]); !errors.Is(err, ErrWorkspaceRemoval) {
			t.Fatalf("RemoveSessionWorkspace%v = %v, want ErrWorkspaceRemoval", args, err)
		}
	}
}

// TestRemoveSessionWorkspace_RefusesANonDirectory: a file where the workspace
// should be is reported, not removed.
func TestRemoveSessionWorkspace_RefusesANonDirectory(t *testing.T) {
	t.Parallel()
	m := newTestWorkspaceManager(t)
	provisionKeys(t, m, "real-a1")
	workspace, err := m.WorkspacePath("t1", "p1")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(workspace, "file-a1")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = m.RemoveSessionWorkspace("t1", "p1", "file-a1")
	requireRemovalRefused[*WorkspaceIOError](t, err)
	if !pathExists(file) {
		t.Fatal("a file was removed as a workspace")
	}
}
