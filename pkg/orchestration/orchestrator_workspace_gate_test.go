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
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newWorkspaceGateService builds an agent service with a mock backend, a
// durable project store, and one stored project in the given provisioning
// state. It returns the service, the stored project, and a pointer that
// captures the last backend-level spawn request.
func newWorkspaceGateService(t *testing.T, provisioning ProjectProvisioning) (AgentService, *Project, **SpawnRequest) {
	t.Helper()

	captured := new(*SpawnRequest)
	backend := &mockAgentBackend{
		spawnFn: func(ctx context.Context, req *SpawnRequest) (*Session, error) {
			*captured = req
			return &Session{
				ID:        "session-ws",
				ProjectID: req.ProjectID,
				Backend:   "mock",
				Status:    SessionStateActive,
				WorkDir:   req.WorkDir,
				CreatedAt: 1000,
			}, nil
		},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService error: %v", err)
	}

	store := NewInMemoryProjectStore()
	project := &Project{
		ID:           "proj-managed-1",
		TenantID:     SystemTenantID,
		Name:         "managed",
		Workspace:    ProjectWorkspace{Path: "/projects/system/proj-managed-1/workspace"},
		Provisioning: provisioning,
	}
	if err := store.Create(context.Background(), project); err != nil {
		t.Fatalf("project store Create error: %v", err)
	}

	wirer, ok := svc.(interface{ SetProjectStore(ProjectStore) })
	if !ok {
		t.Fatal("agent service does not implement SetProjectStore")
	}
	wirer.SetProjectStore(store)
	return svc, project, captured
}

func TestSpawnSession_ManagedWorkspaceReady_ResolvesWorkDir(t *testing.T) {
	t.Parallel()

	svc, project, captured := newWorkspaceGateService(t, ProjectProvisioning{
		Managed: true,
		State:   ProvisioningStateReady,
	})

	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: project.ID,
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if *captured == nil {
		t.Fatal("backend spawn was never called")
	}
	if (*captured).WorkDir != project.Workspace.Path {
		t.Errorf("backend WorkDir = %q, want managed workspace %q", (*captured).WorkDir, project.Workspace.Path)
	}
}

func TestSpawnSession_ManagedWorkspaceNotReady_Refused(t *testing.T) {
	t.Parallel()

	states := []ProjectProvisioningState{
		ProvisioningStatePending,
		ProvisioningStateCloning,
		ProvisioningStateFailed,
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()

			svc, project, _ := newWorkspaceGateService(t, ProjectProvisioning{
				Managed: true,
				State:   state,
				Error:   "clone exploded",
			})

			_, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
				Backend:   "mock",
				ProjectID: project.ID,
			})
			var notReady *ProjectWorkspaceNotReadyError
			if !errors.As(err, &notReady) {
				t.Fatalf("expected ProjectWorkspaceNotReadyError, got %T: %v", err, err)
			}
			if notReady.State != state {
				t.Errorf("notReady.State = %q, want %q", notReady.State, state)
			}
			if notReady.ProjectID != project.ID {
				t.Errorf("notReady.ProjectID = %q, want %q", notReady.ProjectID, project.ID)
			}
		})
	}
}

func TestSpawnSession_ManagedWorkspace_ExplicitWorkDirBypassesGate(t *testing.T) {
	t.Parallel()

	// An explicit work dir means the session does not run in the managed
	// workspace, so its provisioning state does not gate the spawn.
	svc, project, _ := newWorkspaceGateService(t, ProjectProvisioning{
		Managed: true,
		State:   ProvisioningStateCloning,
	})

	resp, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: project.ID,
		WorkDir:   "/somewhere/else",
	})
	if err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("expected a session ID")
	}
}

func TestSpawnSession_UnmanagedDurableProject_NotGated(t *testing.T) {
	t.Parallel()

	// A durable project without a managed workspace never gates the spawn
	// and never contributes a work directory.
	svc, project, _ := newWorkspaceGateService(t, ProjectProvisioning{})

	if _, err := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:   "mock",
		ProjectID: project.ID,
	}); err != nil {
		t.Fatalf("SpawnSession error: %v", err)
	}
}

func TestSpawnSession_ExtraMounts_DeniedWithoutPolicy(t *testing.T) {
	t.Parallel()

	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"mock": &mockAgentBackend{}}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService error: %v", err)
	}

	_, err = svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:     "mock",
		ExtraMounts: []ContainerMount{{Source: "/data/shared", Target: "/mnt"}},
	})
	var denied *MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
	}
}

func TestSpawnSession_ExtraMounts_PolicyEnforced(t *testing.T) {
	t.Parallel()

	var captured *SpawnRequest
	backend := &mockAgentBackend{
		spawnFn: func(ctx context.Context, req *SpawnRequest) (*Session, error) {
			captured = req
			return &Session{ID: "s-mounts", Backend: "mock", Status: SessionStateActive, CreatedAt: 1}, nil
		},
	}
	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService error: %v", err)
	}
	wirer, ok := svc.(interface{ SetMountPolicy(*MountPolicy) })
	if !ok {
		t.Fatal("agent service does not implement SetMountPolicy")
	}

	// The policy authorizes real directories: a source it cannot resolve on
	// disk is refused, and a source under the shared projects root is only
	// reachable inside the requesting tenant's own subtree. An untenanted
	// caller resolves to the system tenant (StorageTenant).
	base := t.TempDir()
	projectsRoot := filepath.Join(base, "projects")
	sharedRoot := filepath.Join(base, "shared")
	workspace := filepath.Join(projectsRoot, SystemTenantID, "p", workspaceDirName)
	corpus := filepath.Join(sharedRoot, "corpus")
	for _, dir := range []string{workspace, corpus} {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, mkErr)
		}
	}
	wirer.SetMountPolicy(&MountPolicy{
		ProjectsRoot: projectsRoot,
		AllowedRoots: []string{sharedRoot},
	})

	// Allowed mounts flow through to the backend spawn request.
	mounts := []ContainerMount{
		{Source: workspace, Target: "/workspace2"},
		{Source: corpus, Target: "/corpus", ReadOnly: true},
	}
	if _, spawnErr := svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:     "mock",
		ExtraMounts: mounts,
	}); spawnErr != nil {
		t.Fatalf("SpawnSession error: %v", spawnErr)
	}
	if captured == nil || len(captured.ExtraMounts) != 2 {
		t.Fatalf("backend spawn request ExtraMounts = %+v, want 2 mounts", captured)
	}
	if captured.ExtraMounts[1].Source != corpus || !captured.ExtraMounts[1].ReadOnly {
		t.Errorf("ExtraMounts[1] = %+v, want read-only %q", captured.ExtraMounts[1], corpus)
	}

	// A mount outside every root is refused before the backend is called.
	captured = nil
	_, err = svc.SpawnSession(context.Background(), &SpawnSessionRequest{
		Backend:     "mock",
		ExtraMounts: []ContainerMount{{Source: "/etc", Target: "/host-etc"}},
	})
	var denied *MountNotAllowedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected MountNotAllowedError, got %T: %v", err, err)
	}
	if captured != nil {
		t.Error("backend was called despite the mount denial")
	}
}
