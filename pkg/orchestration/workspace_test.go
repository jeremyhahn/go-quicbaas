// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

func TestNewWorkspaceManager(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	m, err := NewWorkspaceManager(root)
	if err != nil {
		t.Fatalf("NewWorkspaceManager(%q) error: %v", root, err)
	}
	if m.Root() != filepath.Clean(root) {
		t.Errorf("Root() = %q, want %q", m.Root(), root)
	}
}

func TestNewWorkspaceManager_InvalidRoot(t *testing.T) {
	t.Parallel()

	for _, root := range []string{"", "relative/projects"} {
		if _, err := NewWorkspaceManager(root); err == nil {
			t.Errorf("NewWorkspaceManager(%q): expected error, got nil", root)
		} else {
			var rootErr *WorkspaceRootError
			if !errors.As(err, &rootErr) {
				t.Errorf("NewWorkspaceManager(%q): expected WorkspaceRootError, got %T", root, err)
			}
		}
	}
}

func TestWorkspaceManager_Provision(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	m, err := NewWorkspaceManager(root)
	if err != nil {
		t.Fatalf("NewWorkspaceManager error: %v", err)
	}

	workspace, err := m.Provision("tenant-a", "project-1")
	if err != nil {
		t.Fatalf("Provision error: %v", err)
	}
	want := filepath.Join(root, "tenant-a", "project-1", "workspace")
	if workspace != want {
		t.Errorf("Provision path = %q, want %q", workspace, want)
	}

	// Every managed level must be 0700.
	for _, dir := range []string{
		filepath.Join(root, "tenant-a"),
		filepath.Join(root, "tenant-a", "project-1"),
		workspace,
	} {
		info, statErr := os.Stat(dir)
		if statErr != nil {
			t.Fatalf("Stat(%q) error: %v", dir, statErr)
		}
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("perm(%q) = %o, want 0700", dir, perm)
		}
	}
}

func TestWorkspaceManager_Provision_Idempotent(t *testing.T) {
	t.Parallel()

	m, err := NewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspaceManager error: %v", err)
	}

	first, err := m.Provision("tenant-a", "project-1")
	if err != nil {
		t.Fatalf("first Provision error: %v", err)
	}
	// A file placed in the workspace must survive re-provisioning.
	marker := filepath.Join(first, "marker")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	second, err := m.Provision("tenant-a", "project-1")
	if err != nil {
		t.Fatalf("second Provision error: %v", err)
	}
	if second != first {
		t.Errorf("second Provision path = %q, want %q", second, first)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker file lost on re-provision: %v", err)
	}
}

func TestWorkspaceManager_Provision_TraversalGuard(t *testing.T) {
	t.Parallel()

	m, err := NewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspaceManager error: %v", err)
	}

	tests := []struct {
		name      string
		tenantID  string
		projectID string
	}{
		{"empty tenant", "", "project-1"},
		{"empty project", "tenant-a", ""},
		{"dot tenant", ".", "project-1"},
		{"dotdot tenant", "..", "project-1"},
		{"dotdot project", "tenant-a", ".."},
		{"slash in tenant", "tenant/../../etc", "project-1"},
		{"slash in project", "tenant-a", "p/../../../etc"},
		{"backslash in project", "tenant-a", `p\..\evil`},
		{"nul byte", "tenant-a", "p\x00evil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, provErr := m.Provision(tt.tenantID, tt.projectID); provErr == nil {
				t.Fatalf("Provision(%q, %q): expected error, got nil", tt.tenantID, tt.projectID)
			} else {
				var segErr *WorkspaceSegmentError
				if !errors.As(provErr, &segErr) {
					t.Fatalf("expected WorkspaceSegmentError, got %T: %v", provErr, provErr)
				}
			}
			if rmErr := m.Remove(tt.tenantID, tt.projectID); rmErr == nil {
				t.Fatalf("Remove(%q, %q): expected error, got nil", tt.tenantID, tt.projectID)
			}
		})
	}
}

func TestWorkspaceManager_Remove(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	m, err := NewWorkspaceManager(root)
	if err != nil {
		t.Fatalf("NewWorkspaceManager error: %v", err)
	}

	workspace, err := m.Provision("tenant-a", "project-1")
	if err != nil {
		t.Fatalf("Provision error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "file"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	if err := m.Remove("tenant-a", "project-1"); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "tenant-a", "project-1")); !os.IsNotExist(statErr) {
		t.Errorf("project tree still exists after Remove (stat err: %v)", statErr)
	}

	// The sibling tenant directory survives; Remove is scoped to the project.
	if _, statErr := os.Stat(filepath.Join(root, "tenant-a")); statErr != nil {
		t.Errorf("tenant directory removed unexpectedly: %v", statErr)
	}

	// Idempotent: removing an absent tree succeeds.
	if err := m.Remove("tenant-a", "project-1"); err != nil {
		t.Errorf("second Remove error: %v", err)
	}
}
