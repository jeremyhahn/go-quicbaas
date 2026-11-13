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

package fleet

import (
	"context"
	"errors"
	"os"
	"testing"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// provisionAgentWorkspace provisions one session workspace for tenant-a on the agent's
// manager and returns it with its directory, still held.
func provisionAgentWorkspace(t *testing.T, m *orchestration.WorkspaceManager, key string) (*orchestration.SessionWorkspace, *orchestration.ProvisionedWorkspace) {
	t.Helper()
	ws := &orchestration.SessionWorkspace{TenantID: "tenant-a", ProjectID: "proj-1", Key: key}
	provisioned, err := m.ProvisionSession(ws)
	if err != nil {
		t.Fatalf("ProvisionSession: %v", err)
	}
	return ws, provisioned
}

// TestRemoveWorkspace_TheControlPlaneRemovesTheAgentsWorkspace: a control
// plane's RemoteACPBackend removes a workspace under the AGENT's projects root,
// once no session there holds it, and a second removal finds nothing.
func TestRemoveWorkspace_TheControlPlaneRemovesTheAgentsWorkspace(t *testing.T) {
	t.Parallel()
	workspaces, err := orchestration.NewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote, cleanup := newContractBoundary(t, containerBackend(), &orchestration.MountPolicy{}, nil, workspaces)
	defer cleanup()
	ws, provisioned := provisionAgentWorkspace(t, workspaces, "task-1-a1")

	_, err = remote.RemoveWorkspace(context.Background(), ws)
	var rpcErr *orchestration.RemoteBackendRPCError
	if !errors.As(err, &rpcErr) || grpcstatus.Code(rpcErr.Err) != codes.FailedPrecondition {
		t.Fatalf("held workspace: %v, want FailedPrecondition", err)
	}

	provisioned.Release()
	removed, err := remote.RemoveWorkspace(context.Background(), ws)
	if err != nil || !removed {
		t.Fatalf("RemoveWorkspace = %v, %v; want true, nil", removed, err)
	}
	if _, statErr := os.Stat(provisioned.Dir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("the workspace is still there: %v", statErr)
	}
	if removed, err := remote.RemoveWorkspace(context.Background(), ws); err != nil || removed {
		t.Fatalf("second removal = %v, %v; want false, nil", removed, err)
	}
}

// TestRemoveWorkspace_RefusesWhatTheCallerDoesNotOwn: another tenant's
// workspace is refused before the call and by the runtime, and a runtime with
// no workspace manager removes nothing.
func TestRemoveWorkspace_RefusesWhatTheCallerDoesNotOwn(t *testing.T) {
	t.Parallel()
	workspaces, err := orchestration.NewWorkspaceManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	remote, cleanup := newContractBoundary(t, containerBackend(), &orchestration.MountPolicy{}, nil, workspaces)
	defer cleanup()
	ws, provisioned := provisionAgentWorkspace(t, workspaces, "task-1-a1")
	provisioned.Release()

	foreign := &orchestration.SessionWorkspace{TenantID: "tenant-b", ProjectID: "proj-1", Key: "task-1-a1"}
	var tenantErr *orchestration.SessionWorkspaceTenantError
	if _, err := remote.RemoveWorkspace(context.Background(), foreign); !errors.As(err, &tenantErr) {
		t.Fatalf("remote, another tenant's workspace: %v", err)
	}

	rt, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": containerBackend()},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		MountPolicy:   &orchestration.MountPolicy{},
		Logger:        &testLogger{t: t},
		Workspaces:    workspaces,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.RemoveWorkspace("tenant-b", ws); !errors.As(err, &tenantErr) {
		t.Fatalf("runtime, another tenant's workspace: %v", err)
	}
	if _, err := rt.RemoveWorkspace("tenant-a", nil); err == nil {
		t.Fatal("a nil workspace was accepted")
	}
	if _, statErr := os.Stat(provisioned.Dir); statErr != nil {
		t.Fatalf("a refused removal touched the workspace: %v", statErr)
	}

	bare, err := NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": containerBackend()},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		MountPolicy:   &orchestration.MountPolicy{},
		Logger:        &testLogger{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	var unsupported *orchestration.SessionWorkspaceUnsupportedError
	if _, err := bare.RemoveWorkspace("tenant-a", ws); !errors.As(err, &unsupported) {
		t.Fatalf("no workspace manager: %v", err)
	}
}
