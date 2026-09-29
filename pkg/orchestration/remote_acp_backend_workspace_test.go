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
	"testing"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// remoteWorkspaceSpawn runs one Spawn naming ws against a daemon answering
// resp, and returns the session the client built and the request the daemon
// received.
func remoteWorkspaceSpawn(t *testing.T, ws *SessionWorkspace, resp *proto.SpawnSessionResponse) (*Session, *proto.SpawnSessionRequest) {
	t.Helper()
	server := &fakeDaemonServer{spawnResp: resp}
	daemon := startBufconnDaemon(t, server)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Backend:      "acp",
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	session, spawnErr := backend.Spawn(context.Background(), &SpawnRequest{ProjectID: "p1", Workspace: ws})
	if spawnErr != nil {
		t.Fatalf("Spawn: %v", spawnErr)
	}
	server.spawnReqMu.Lock()
	defer server.spawnReqMu.Unlock()
	return session, server.lastSpawnReq
}

// TestRemoteACPBackend_SpawnNamesTheWorkspaceAndReadsItsDirectory is the
// wire half of go-quicbaas#981: the workspace crosses by name with its spec,
// no host path goes with it, and the session's work directory is the one the
// DAEMON reported — the only party that knows it.
func TestRemoteACPBackend_SpawnNamesTheWorkspaceAndReadsItsDirectory(t *testing.T) {
	t.Parallel()
	const agentDir = "/var/lib/quicbaas-agent/projects/t1/p1/workspace/task-a1"
	session, got := remoteWorkspaceSpawn(t, namedWorkspace(),
		&proto.SpawnSessionResponse{SessionId: "s1", WorkDir: agentDir})

	ws := got.GetWorkspace()
	if ws.GetTenantId() != "t1" || ws.GetProjectId() != "p1" || ws.GetKey() != "task-a1" {
		t.Fatalf("workspace on the wire = %+v", ws)
	}
	if string(ws.GetSpecFiles()[WorkspaceForgeTokenFile]) != "forge-secret" {
		t.Fatal("the spec files must reach the daemon that writes them")
	}
	if got.GetWorkDir() != "" || ws.GetPinnedDir() != "" {
		t.Fatalf("an unpinned named spawn carries no host path: work_dir=%q pinned=%q",
			got.GetWorkDir(), ws.GetPinnedDir())
	}
	if session.WorkDir != agentDir {
		t.Fatalf("Session.WorkDir = %q, want the daemon's %q", session.WorkDir, agentDir)
	}
}

// TestRemoteACPBackend_OldDaemonReportsNoWorkspace: a daemon that does not
// know the field answers with no directory, and the client reports exactly
// that rather than echoing a path — which is what lets the orchestrator refuse
// the session (requireAgentWorkspace).
func TestRemoteACPBackend_OldDaemonReportsNoWorkspace(t *testing.T) {
	t.Parallel()
	session, _ := remoteWorkspaceSpawn(t, namedWorkspace(), &proto.SpawnSessionResponse{SessionId: "s1"})
	if session.WorkDir != "" {
		t.Fatalf("Session.WorkDir = %q, want empty for a daemon that provisioned nothing", session.WorkDir)
	}
}

// TestSessionWorkspaceToProto_PinTravelsOnlyForItsOwnKey: the pinned
// directory crosses only when the pin is for this very workspace.
func TestSessionWorkspaceToProto_PinTravelsOnlyForItsOwnKey(t *testing.T) {
	t.Parallel()
	if sessionWorkspaceToProto(nil) != nil {
		t.Fatal("no workspace is no wire workspace")
	}
	own := namedWorkspace()
	own.Pin = &WorkspacePlacement{AgentHost: "agent-09:9091", Key: own.Key, Dir: "/d"}
	if got := sessionWorkspaceToProto(own).GetPinnedDir(); got != "/d" {
		t.Fatalf("pinned_dir = %q, want /d", got)
	}
	other := namedWorkspace()
	other.Spec = nil
	other.Pin = &WorkspacePlacement{AgentHost: "agent-09:9091", Key: "review-1-abc", Dir: "/d"}
	wire := sessionWorkspaceToProto(other)
	if wire.GetPinnedDir() != "" || wire.GetSpecFiles() != nil {
		t.Fatalf("a pin for another key names no directory, and no spec means no files: %+v", wire)
	}
}
