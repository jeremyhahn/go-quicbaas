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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// =============================================================================
// A container nobody can name is a container nothing can act in
// =============================================================================
//
// A backend that owns its own sandbox is the only party that knows which
// container it put the agent in. If that id stops at the backend's return
// value, the engine's publish path (ExecSession) has nowhere to run the
// delivery's commit and push, and it fails for a session that is otherwise
// perfectly healthy. These tests follow the id along every hop it has to
// survive: the backend's Session, the spawn response the caller reads, the
// live record the engine arbitrates on, and the durable record a restart
// rebuilds everything else from.

// containerBackendID is the container the fake backend reports having put the
// agent in.
const containerBackendID = "ctr-backend-0123456789abcdef"

// newContainerIDService builds a service over kv whose "mock" backend reports
// containerID from Spawn. Reusing kv across two calls is a daemon restart: the
// live registry starts empty and only the durable records survive.
func newContainerIDService(t *testing.T, kv interfaces.KVStore, containerID string) (*agentServiceImpl, AgentStore) {
	t.Helper()

	backend := &mockAgentBackend{
		spawnFn: func(_ context.Context, req *SpawnRequest) (*Session, error) {
			return &Session{
				ID:          "sess-container-1",
				ProjectID:   req.ProjectID,
				Backend:     "mock",
				Model:       req.Model,
				Status:      SessionStateActive,
				WorkDir:     req.WorkDir,
				CreatedAt:   time.Now().UnixNano(),
				ContainerID: containerID,
			}, nil
		},
	}
	store := NewAgentStore(kv)
	svc, err := NewAgentService(store, map[string]AgentBackend{"mock": backend}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	return impl, store
}

// TestSpawnSession_BackendContainerIDReachesEveryRecord walks the id through
// all three surfaces a later caller can read it from. Any one of them losing
// it is a session whose delivery cannot be published.
func TestSpawnSession_BackendContainerIDReachesEveryRecord(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		containerID string
	}{
		{name: "BackendOwnedContainer", containerID: containerBackendID},
		{
			// A host-process session must not acquire a container id from
			// anywhere: ExecSession refuses it precisely BECAUSE the field is
			// empty, so an invented value would send a tenant's git into
			// someone else's sandbox.
			name:        "HostProcessSessionStaysUncontainerized",
			containerID: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			impl, store := newContainerIDService(t, kvstore.NewMemory(), tc.containerID)
			ctx := ContextWithTenant(context.Background(), execSessionTenant)

			resp, err := impl.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock"})
			if err != nil {
				t.Fatalf("SpawnSession: %v", err)
			}
			if resp.ContainerID != tc.containerID {
				t.Errorf("SpawnSessionResponse.ContainerID = %q, want %q", resp.ContainerID, tc.containerID)
			}

			live, ok := impl.liveSessionRecord(resp.SessionID)
			if !ok {
				t.Fatal("no live record for the session that was just spawned")
			}
			if live.ContainerID != tc.containerID {
				t.Errorf("live record ContainerID = %q, want %q", live.ContainerID, tc.containerID)
			}

			record, err := store.GetSession(ctx, execSessionTenant, resp.SessionID)
			if err != nil {
				t.Fatalf("GetSession: %v", err)
			}
			if record.ContainerID != tc.containerID {
				t.Errorf("durable record ContainerID = %q, want %q", record.ContainerID, tc.containerID)
			}
		})
	}
}

// TestSpawnSession_ContainerIDSurvivesARestart is the reason the durable copy
// exists. The live registry is process-local, so after a restart the only
// thing that still knows where the agent runs is the record on disk — and a
// resumed session whose delivery cannot be published is a session that
// silently stops finishing its work.
func TestSpawnSession_ContainerIDSurvivesARestart(t *testing.T) {
	t.Parallel()

	kv := kvstore.NewMemory()
	ctx := ContextWithTenant(context.Background(), execSessionTenant)

	// Persistent, because that is the shape of session a restart has anything
	// to restore: a non-persistent one ran a fresh process per prompt and is
	// closed at boot rather than resumed.
	first, _ := newContainerIDService(t, kv, containerBackendID)
	resp, err := first.SpawnSession(ctx, &SpawnSessionRequest{Backend: "mock", Persistent: true})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	// A second service over the same durable store is the restarted daemon.
	second, _ := newContainerIDService(t, kv, containerBackendID)
	if _, ok := second.liveSessionRecord(resp.SessionID); ok {
		t.Fatal("the restarted daemon started with a live record; the registry is not process-local")
	}
	if err := second.LoadPersistedSessions(context.Background()); err != nil {
		t.Fatalf("LoadPersistedSessions: %v", err)
	}

	restored, ok := second.liveSessionRecord(resp.SessionID)
	if !ok {
		t.Fatal("the session was not restored from its durable record")
	}
	if restored.ContainerID != containerBackendID {
		t.Errorf("restored ContainerID = %q, want %q", restored.ContainerID, containerBackendID)
	}

	// And the id is not merely stored: it is what the engine's publish path
	// resolves and runs against.
	runtime := &mockContainerRuntime{available: true}
	second.SetContainerRuntime(runtime)
	if _, err := second.ExecSession(ctx, &ExecSessionRequest{
		SessionID: resp.SessionID,
		Spec:      commitSpec(),
	}); err != nil {
		t.Fatalf("ExecSession after restart: %v", err)
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if len(runtime.execCalls) != 1 {
		t.Fatalf("runtime saw %d executions, want exactly 1", len(runtime.execCalls))
	}
	if runtime.execCalls[0].ContainerID != containerBackendID {
		t.Errorf("ran in container %q, want %q", runtime.execCalls[0].ContainerID, containerBackendID)
	}
}

// TestACPBackend_SpawnReportsItsContainer is the first hop of the chain: the
// ACP backend launches the container itself, so the id exists nowhere else
// until Spawn reports it.
func TestACPBackend_SpawnReportsItsContainer(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{
		Command:      "opencode",
		Args:         []string{"acp"},
		DefaultModel: "test-model",
	})
	backend.SetContainerRuntime(&ctxBoundContainerRuntime{t: t})

	sess, err := backend.Spawn(context.Background(), &SpawnRequest{
		ProjectID:      "p",
		WorkDir:        "/tmp",
		Model:          "test-model",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "img:latest",
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	// "test-ctr" is what ctxBoundContainerRuntime names its interactive
	// process; the assertion is that Spawn reports the runtime's id rather
	// than keeping it inside the backend's own session bookkeeping.
	if sess.ContainerID != "test-ctr" {
		t.Errorf("Session.ContainerID = %q, want the container the runtime started", sess.ContainerID)
	}
}
