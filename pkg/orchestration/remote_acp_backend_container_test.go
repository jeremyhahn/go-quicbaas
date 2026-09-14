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
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// The container a remote daemon reports (go-quicbaas#740)
// =============================================================================
//
// The daemon creates the container, so the response is the only place its
// identity can come from. RemoteACPBackend.Spawn builds the Session that the
// orchestrator writes to the live record, the durable record and the spawn
// response, in that order — so a mapping that drops the id here empties all
// four, and the SDLC runner, which refuses a worker whose delivery could never
// be committed inside a container, refuses every remote worker.
//
// pkg/agent's wire contract drives this through a real daemon end to end. These
// two pin the client-side mapping on its own, so a change that stops reading
// the field fails here first, naming it.

// remoteSpawnSession runs one Spawn against a daemon returning resp and hands
// back the Session the client built from it.
func remoteSpawnSession(t *testing.T, resp *proto.SpawnSessionResponse) *Session {
	t.Helper()

	daemon := startBufconnDaemon(t, &fakeDaemonServer{spawnResp: resp})
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

	session, spawnErr := backend.Spawn(context.Background(), &SpawnRequest{
		ProjectID: "proj-1",
		WorkDir:   "/work",
	})
	if spawnErr != nil {
		t.Fatalf("Spawn: %v", spawnErr)
	}
	return session
}

// TestRemoteACPBackend_SpawnCarriesTheDaemonsContainerID is the mapping itself.
func TestRemoteACPBackend_SpawnCarriesTheDaemonsContainerID(t *testing.T) {
	t.Parallel()

	const containerID = "ctr-daemon-0123456789abcdef"
	session := remoteSpawnSession(t, &proto.SpawnSessionResponse{
		SessionId:   "sess-remote-1",
		ContainerId: containerID,
	})

	if session.ContainerID != containerID {
		t.Errorf("Session.ContainerID = %q, want %q.\n"+
			"The daemon created the container and reported it; a client that does not read the\n"+
			"field records a remote worker as running none (go-quicbaas#740).",
			session.ContainerID, containerID)
	}
}

// TestRemoteACPBackend_SpawnInventsNoContainerID is the bad-input twin: a
// daemon that reports no container ran a HOST PROCESS, and the empty string
// must survive as the answer it is. Manufacturing an id would let the runner
// begin work whose commit and push have nowhere to run — and, worse, would
// point a later command at whatever container the invented id names.
func TestRemoteACPBackend_SpawnInventsNoContainerID(t *testing.T) {
	t.Parallel()

	session := remoteSpawnSession(t, &proto.SpawnSessionResponse{SessionId: "sess-remote-2"})

	if session.ContainerID != "" {
		t.Errorf("Session.ContainerID = %q for a daemon that reported none, want empty",
			session.ContainerID)
	}
	if session.ID != "sess-remote-2" {
		t.Errorf("Session.ID = %q, want %q", session.ID, "sess-remote-2")
	}
}

// TestSpawnSession_RemoteDaemonsContainerReachesEveryRecord is the whole seam
// #740 was measured on, with nothing doubled between the daemon's response and
// the answer the SDLC runner acts on: a fake daemon reports a container id over
// real gRPC, a real RemoteACPBackend reads it, and the orchestrator's own
// SpawnSession writes it to the three places anything downstream can read.
//
// session_container_id_test.go proves the same for a LOCAL backend. That test
// passed throughout #740 — the local path was never broken — which is exactly
// why the remote one needs its own: a Session built by hand from a proto
// response is a different mapping, and it was the one that dropped the field.
func TestSpawnSession_RemoteDaemonsContainerReachesEveryRecord(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		containerID string
	}{
		{name: "SandboxedRemoteWorker", containerID: "ctr-remote-0123456789abcdef"},
		{
			// The daemon ran a host process. Empty must stay empty all the way
			// through, because that is the evidence the SDLC runner refuses a
			// worker on — see #742 for why a required-mode spawn can still
			// produce one.
			name:        "RemoteHostProcessStaysUncontainerized",
			containerID: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			daemon := startBufconnDaemon(t, &fakeDaemonServer{
				spawnResp: &proto.SpawnSessionResponse{
					SessionId:   "sess-remote-e2e",
					Model:       "model-from-daemon",
					ContainerId: tc.containerID,
				},
			})
			remote, err := NewRemoteACPBackend(RemoteACPBackendDeps{
				Registration: newDaemonRegistration(),
				Backend:      "acp",
				Logger:       &agentTestLogger{},
				ConnDialer:   daemon.dialer(),
			})
			if err != nil {
				t.Fatalf("NewRemoteACPBackend: %v", err)
			}
			t.Cleanup(func() { _ = remote.CloseAll() })

			store := NewAgentStore(kvstore.NewMemory())
			svc, err := NewAgentService(store, map[string]AgentBackend{"acp": remote}, &agentTestLogger{})
			if err != nil {
				t.Fatalf("NewAgentService: %v", err)
			}
			impl, ok := svc.(*agentServiceImpl)
			if !ok {
				t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
			}
			// The daemon-kind resolution is what makes this spawn remote, and
			// it is also what tells the orchestrator the sandbox is the
			// AGENT's to provide rather than its own.
			impl.SetRemoteBackendResolver(func(context.Context, string, string) (AgentBackend, bool) {
				return remote, true
			})

			ctx := ContextWithTenant(context.Background(), execSessionTenant)
			resp, spawnErr := impl.SpawnSession(ctx, &SpawnSessionRequest{
				Backend:     "acp",
				SandboxMode: SandboxModeRequired,
			})
			if spawnErr != nil {
				t.Fatalf("SpawnSession: %v", spawnErr)
			}

			if resp.ContainerID != tc.containerID {
				t.Errorf("SpawnSessionResponse.ContainerID = %q, want %q.\n"+
					"This is the field pkg/sdlc/runner.go gates the worker on.",
					resp.ContainerID, tc.containerID)
			}

			live, found := impl.liveSessionRecord(resp.SessionID)
			if !found {
				t.Fatal("no live record for the session that was just spawned")
			}
			if live.ContainerID != tc.containerID {
				t.Errorf("live record ContainerID = %q, want %q", live.ContainerID, tc.containerID)
			}

			record, getErr := store.GetSession(ctx, execSessionTenant, resp.SessionID)
			if getErr != nil {
				t.Fatalf("GetSession: %v", getErr)
			}
			if record.ContainerID != tc.containerID {
				t.Errorf("durable record ContainerID = %q, want %q", record.ContainerID, tc.containerID)
			}
		})
	}
}
