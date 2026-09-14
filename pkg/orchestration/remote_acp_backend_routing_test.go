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

// remote_acp_backend_routing_test.go pins WHICH BACKEND a remote session is
// served by (go-quicbaas#937).
//
// A daemon registers one primary backend and advertises the whole set it
// serves; resolution picks it for any of them. The backend object then named the
// registration's PRIMARY on every RPC, so a session resolved for codex was
// served by claude — a live session running the wrong model family under the
// right name, with the session record still saying codex and nothing reporting
// the mismatch. It matters past the label: RequireIndependentReviewer refuses an
// approval from the model family that wrote the code, and it checks the
// CONFIGURED reviewer, so the gate was satisfiable in configuration while
// violated in fact.
//
// Every test here reads the backend name off the WIRE, because that is the only
// place the answer is authoritative: the name on the request is the backend the
// daemon runs.

package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// multiBackendRegistration is the fleet daemon #937 was measured on: it
// registers with claude as its primary and advertises codex and opencode
// beside it, exactly as a daemon started with several ACP backends heartbeats
// (see reconcileAgentBackends).
func multiBackendRegistration() *AgentRegistration {
	return &AgentRegistration{
		ID:           "vm-host-4-id",
		TenantID:     "tenant-a",
		Name:         "vm-host-4",
		Host:         "daemon.test:9091",
		Backend:      "claude",
		Capabilities: []string{"claude", "codex", "opencode"},
		Kind:         AgentKindDaemon,
		Status:       AgentStatusRunning,
	}
}

// routedBackend builds a RemoteACPBackend bound to the given backend name and
// wired to a bufconn daemon whose captured requests the caller inspects.
func routedBackend(t *testing.T, reg *AgentRegistration, backend string) (*RemoteACPBackend, *fakeDaemonServer) {
	t.Helper()

	daemon := startBufconnDaemon(t, &fakeDaemonServer{})
	remote, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg,
		Backend:      backend,
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend(backend=%q): %v", backend, err)
	}
	t.Cleanup(func() { _ = remote.CloseAll() })
	return remote, daemon.svc
}

// =============================================================================
// Construction — the refusal the old code could not express
// =============================================================================

// TestNewRemoteACPBackend_RequiresServedBackend is the success/guard pair for
// the served-backend name: a registration's primary and any backend in its
// capability set are accepted, and an unnamed backend is refused rather than
// defaulting to the primary (which is the defect).
func TestNewRemoteACPBackend_RequiresServedBackend(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		backend string
		wantErr error
	}{
		"the registration's primary":     {backend: "claude"},
		"a backend it also serves":       {backend: "codex"},
		"a third backend it also serves": {backend: "opencode"},
		"no backend named":               {backend: "", wantErr: ErrRemoteBackendNoBackend},
		"whitespace only":                {backend: "   ", wantErr: ErrRemoteBackendNoBackend},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
				Registration: multiBackendRegistration(),
				Backend:      tc.backend,
				Logger:       &agentTestLogger{},
			})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				if backend != nil {
					t.Fatal("no backend may be returned alongside an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if backend.Backend() != tc.backend {
				t.Fatalf("Backend() = %q, want %q", backend.Backend(), tc.backend)
			}
		})
	}
}

// TestNewRemoteACPBackend_RefusesUnservedBackend is the refusal itself: a
// registration asked for a backend it does not serve yields a typed
// NoMatchingAgentError and no backend, instead of an instance that would have
// asked the daemon for something else.
func TestNewRemoteACPBackend_RefusesUnservedBackend(t *testing.T) {
	t.Parallel()

	for _, requested := range []string{"gemini", "ollama", "CLAUDE"} {
		t.Run(requested, func(t *testing.T) {
			t.Parallel()

			reg := multiBackendRegistration()
			backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
				Registration: reg,
				Backend:      requested,
				Logger:       &agentTestLogger{},
			})
			if backend != nil {
				t.Fatal("a backend the registration does not serve must not be constructed")
			}
			var noMatch *NoMatchingAgentError
			if !errors.As(err, &noMatch) {
				t.Fatalf("error = %T (%v), want *NoMatchingAgentError", err, err)
			}
			if noMatch.Constraint != "backend" {
				t.Errorf("Constraint = %q, want %q", noMatch.Constraint, "backend")
			}
			if !errors.Is(err, ErrNoMatchingAgent) {
				t.Error("the refusal must unwrap to ErrNoMatchingAgent")
			}
		})
	}
}

// =============================================================================
// Every RPC names the backend the caller asked for
// =============================================================================

// TestRemoteACPBackend_RPCsNameTheRequestedBackend is the defect measured on
// the wire. For each backend the daemon serves — primary or not — Spawn, the
// Session it returns, Prompt and ListModels must all name that backend. Before
// the fix every one of them named claude.
func TestRemoteACPBackend_RPCsNameTheRequestedBackend(t *testing.T) {
	t.Parallel()

	for _, backend := range []string{"claude", "codex", "opencode"} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()

			remote, daemon := routedBackend(t, multiBackendRegistration(), backend)
			ctx := context.Background()

			session, err := remote.Spawn(ctx, &SpawnRequest{ProjectID: "proj-1", WorkDir: "/work"})
			if err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			if got := daemon.spawnRequest().GetBackend(); got != backend {
				t.Errorf("Spawn named backend %q on the wire, want %q.\n"+
					"The daemon runs the backend the request names, so this is the model family\n"+
					"that actually ran (go-quicbaas#937).", got, backend)
			}
			if session.Backend != backend {
				t.Errorf("Session.Backend = %q, want %q — the recorded backend must equal the one on the wire",
					session.Backend, backend)
			}

			if _, err := remote.Prompt(ctx, &PromptRequest{
				SessionID: session.ID,
				Prompt:    "hello",
			}); err != nil {
				t.Fatalf("Prompt: %v", err)
			}
			if got := daemon.promptRequest().GetBackend(); got != backend {
				t.Errorf("Prompt named backend %q on the wire, want %q", got, backend)
			}

			if _, err := remote.ListModels(ctx); err != nil {
				t.Fatalf("ListModels: %v", err)
			}
			if got := daemon.listModelsRequest().GetBackend(); got != backend {
				t.Errorf("ListModels named backend %q on the wire, want %q — a daemon's model\n"+
					"catalog differs per backend", got, backend)
			}
		})
	}
}

// TestRemoteACPBackend_InferNamesTheBoundBackend covers Infer's two inputs: a
// request naming the bound backend (or naming none, deferring to the binding)
// reaches the daemon under that name.
func TestRemoteACPBackend_InferNamesTheBoundBackend(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"the request names the bound backend": "codex",
		"the request names none":              "",
	}

	for name, requested := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			remote, daemon := routedBackend(t, multiBackendRegistration(), "codex")
			chunks, err := remote.Infer(context.Background(), &InferRequest{
				Backend: requested,
				Prompt:  "hello",
			})
			if err != nil {
				t.Fatalf("Infer: %v", err)
			}
			for range chunks {
				// Drain so the stream goroutine finishes before the assertion.
			}
			if got := daemon.inferRequest().GetBackend(); got != "codex" {
				t.Errorf("Infer named backend %q on the wire, want %q", got, "codex")
			}
		})
	}
}

// TestRemoteACPBackend_InferRefusesAnotherBackendsRequest is the bad-input
// twin: an Infer request naming a backend other than the bound one is the #937
// class recurring at the dispatch seam, and it is refused before any RPC runs
// rather than silently answered by the bound backend.
func TestRemoteACPBackend_InferRefusesAnotherBackendsRequest(t *testing.T) {
	t.Parallel()

	remote, daemon := routedBackend(t, multiBackendRegistration(), "codex")

	chunks, err := remote.Infer(context.Background(), &InferRequest{
		Backend: "claude",
		Prompt:  "hello",
	})
	if chunks != nil {
		t.Error("no stream may be opened for a refused request")
	}
	var mismatch *RemoteBackendMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %T (%v), want *RemoteBackendMismatchError", err, err)
	}
	if mismatch.Bound != "codex" || mismatch.Requested != "claude" {
		t.Errorf("mismatch = %+v, want Bound=codex Requested=claude", mismatch)
	}
	// The message names both, because an operator reading it needs to know
	// which of the two is wrong.
	for _, want := range []string{"Infer", "codex", "claude"} {
		if !strings.Contains(mismatch.Error(), want) {
			t.Errorf("error %q must name %q", mismatch.Error(), want)
		}
	}
	if daemon.inferCalls.Load() != 0 {
		t.Errorf("Infer RPCs = %d, want 0 — a refused request must not reach the daemon",
			daemon.inferCalls.Load())
	}
}

// =============================================================================
// The invariant end to end: the session record and its RPCs agree
// =============================================================================

// TestSpawnSession_RecordedBackendEqualsTheBackendOnItsRPCs is the whole seam
// the defect was measured on, through the orchestrator that writes the record:
// a session spawned on codex against a daemon whose primary is claude must be
// recorded as codex AND have named codex on its Spawn and on its Prompt.
//
// The burn-in evidence was exactly this triple disagreeing — record codex,
// wire claude — so the test asserts all three rather than any one of them.
func TestSpawnSession_RecordedBackendEqualsTheBackendOnItsRPCs(t *testing.T) {
	t.Parallel()

	const (
		tenant      = "tenant-a"
		wantBackend = "codex"
	)

	daemon := startBufconnDaemon(t, &fakeDaemonServer{
		spawnResp: &proto.SpawnSessionResponse{SessionId: "sess-codex-1"},
	})
	remote, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: multiBackendRegistration(),
		Backend:      wantBackend,
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = remote.CloseAll() })

	store := NewAgentStore(kvstore.NewMemory())
	svc, err := NewAgentService(store, map[string]AgentBackend{wantBackend: remote}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl, ok := svc.(*agentServiceImpl)
	if !ok {
		t.Fatalf("NewAgentService returned %T, want *agentServiceImpl", svc)
	}
	// The resolver stands in for pkg/server's remoteBackendPool, which now
	// constructs one instance per requested backend name.
	impl.SetRemoteBackendResolver(func(_ context.Context, name, _ string) (AgentBackend, bool) {
		if name != wantBackend {
			return nil, false
		}
		return remote, true
	})

	ctx := ContextWithTenant(context.Background(), tenant)
	spawn, err := impl.SpawnSession(ctx, &SpawnSessionRequest{Backend: wantBackend})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	if got := daemon.svc.spawnRequest().GetBackend(); got != wantBackend {
		t.Errorf("Spawn named backend %q on the wire, want %q", got, wantBackend)
	}
	if spawn.Backend != wantBackend {
		t.Errorf("SpawnSessionResponse.Backend = %q, want %q", spawn.Backend, wantBackend)
	}

	live, found := impl.liveSessionRecord(spawn.SessionID)
	if !found {
		t.Fatal("no live record for the session that was just spawned")
	}
	if live.Backend != wantBackend {
		t.Errorf("live record Backend = %q, want %q", live.Backend, wantBackend)
	}

	record, err := store.GetSession(ctx, tenant, spawn.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if record.Backend != wantBackend {
		t.Errorf("durable record Backend = %q, want %q", record.Backend, wantBackend)
	}

	if _, err := impl.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawn.SessionID,
		Prompt:    "review this",
	}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	if got := daemon.svc.promptRequest().GetBackend(); got != wantBackend {
		t.Errorf("Prompt named backend %q on the wire while the record says %q.\n"+
			"This is the measured defect: the session record and the process that ran\n"+
			"disagreed, so an independence gate could pass on the record and be violated\n"+
			"in fact (go-quicbaas#937).", got, record.Backend)
	}
}
