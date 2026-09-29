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

// session_host_pinning_test.go is the regression suite for the orchestrator
// session-lifecycle bug where a session spawned on one resolved daemon could
// not be prompted afterwards ("acp: session not found"). The follow-up
// Prompt / Status / Close re-resolve the backend via the RemoteBackendResolver
// using the *current* request context; when the host-affinity hint was present
// at Spawn but absent on the follow-up (the common SPA case), re-resolution
// landed on a different instance — the local fallback backend — where the live
// in-memory session does not exist.
//
// The fix records the resolved host on the session and replays it onto the
// follow-up context so re-resolution is deterministic. These tests exercise
// that contract end-to-end through the public AgentService API using two
// distinct backend instances (one "remote" host-bound, one local fallback)
// behind a host-affinity-filtering resolver.

package orchestration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// instanceBackend is a fake AgentBackend that keeps its own session set so a
// test can prove which instance a call landed on. Prompt / Status / Close
// return SessionNotFoundError when the session was not spawned on this
// instance — exactly the daemon-side behavior that produced the bug.
type instanceBackend struct {
	label        string
	resolvedHost string

	mu       sync.Mutex
	sessions map[string]bool
	spawns   int
}

func newInstanceBackend(label, host string) *instanceBackend {
	return &instanceBackend{label: label, resolvedHost: host, sessions: map[string]bool{}}
}

func (b *instanceBackend) Spawn(_ context.Context, _ *SpawnRequest) (*Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spawns++
	id := "ses-" + b.label
	b.sessions[id] = true
	return &Session{ID: id, Backend: "claude", Status: SessionStateActive, CreatedAt: 1000}, nil
}

func (b *instanceBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.sessions[req.SessionID] {
		return nil, &SessionNotFoundError{SessionID: req.SessionID}
	}
	return &PromptResponse{SessionID: req.SessionID, Content: []ContentBlock{{Type: "text", Text: b.label}}, StopReason: "end_turn"}, nil
}

func (b *instanceBackend) Status(_ context.Context, sessionID string) (*SessionStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.sessions[sessionID] {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	return &SessionStatus{SessionID: sessionID, State: SessionStateActive}, nil
}

func (b *instanceBackend) Close(_ context.Context, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.sessions[sessionID] {
		return &SessionNotFoundError{SessionID: sessionID}
	}
	delete(b.sessions, sessionID)
	return nil
}

func (b *instanceBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{Kind: BackendKindAgent, PersistentSessions: true, ACP: true}
}
func (b *instanceBackend) Type() string         { return "acp" }
func (b *instanceBackend) ToolGuidance() string { return "" }

// ResolvedHost makes the remote instance satisfy resolvedHostBackend.
func (b *instanceBackend) ResolvedHost() string { return b.resolvedHost }

// hostFilteringSvc wires an AgentService whose local backend is `local` and
// whose resolver returns `remote` only when the request carries the matching
// host affinity. This mirrors production: the resolver applies host-affinity
// filtering and falls through to the local backend when no daemon matches.
func hostFilteringSvc(t *testing.T, local, remote *instanceBackend, remoteHost string) *agentServiceImpl {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"claude": local}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetRemoteBackendResolver(func(ctx context.Context, name, _ string) (AgentBackend, bool) {
		if name != "claude" {
			return nil, false
		}
		// Only route to the remote daemon when the host hint matches.
		if HostAffinityFromContext(ctx) == remoteHost {
			return remote, true
		}
		return nil, false
	})
	return impl
}

// TestPromptSession_FindsSessionSpawnedOnRemoteHost is the exact bug: spawn a
// session on a host-affinity-selected daemon, then immediately prompt it
// WITHOUT the host hint (as the SPA does). Before the fix the follow-up
// re-resolved to the local fallback backend and returned SessionNotFoundError;
// after the fix the captured host is replayed and the prompt reaches the same
// remote instance.
func TestPromptSession_FindsSessionSpawnedOnRemoteHost(t *testing.T) {
	t.Parallel()

	local := newInstanceBackend("LOCAL", "")
	remote := newInstanceBackend("REMOTE", "daemon-1")
	impl := hostFilteringSvc(t, local, remote, "daemon-1")

	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawnResp, err := impl.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:      "claude",
		HostAffinity: "daemon-1",
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if remote.spawns != 1 || local.spawns != 0 {
		t.Fatalf("spawn must land on remote: remote=%d local=%d", remote.spawns, local.spawns)
	}

	// Follow-up prompt WITHOUT host affinity — the SPA omits it.
	promptResp, err := impl.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawnResp.SessionID,
		Prompt:    "hello",
	})
	if err != nil {
		t.Fatalf("PromptSession must find the session, got: %v", err)
	}
	if errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("must not be session-not-found")
	}
	if len(promptResp.Content) == 0 || promptResp.Content[0].Text != "REMOTE" {
		t.Fatalf("prompt must reach the REMOTE instance, got %+v", promptResp.Content)
	}
}

// TestSessionStatusAndClose_PinToSpawnHost proves Status and Close also pin to
// the spawn-time host so they reach the same instance the session lives on.
func TestSessionStatusAndClose_PinToSpawnHost(t *testing.T) {
	t.Parallel()

	local := newInstanceBackend("LOCAL", "")
	remote := newInstanceBackend("REMOTE", "daemon-1")
	impl := hostFilteringSvc(t, local, remote, "daemon-1")

	ctx := ContextWithTenant(context.Background(), "tenant-a")

	spawnResp, err := impl.SpawnSession(ctx, &SpawnSessionRequest{
		Backend:      "claude",
		HostAffinity: "daemon-1",
	})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	// Status without host affinity must reach REMOTE.
	st, err := impl.SessionStatus(ctx, &SessionStatusRequest{SessionID: spawnResp.SessionID})
	if err != nil {
		t.Fatalf("SessionStatus must find the session, got: %v", err)
	}
	if st.State != SessionStateActive {
		t.Fatalf("unexpected state %q", st.State)
	}

	// Close without host affinity must reach REMOTE and remove it there.
	if _, err := impl.CloseSession(ctx, &CloseSessionRequest{SessionID: spawnResp.SessionID}); err != nil {
		t.Fatalf("CloseSession must find the session, got: %v", err)
	}
	remote.mu.Lock()
	stillThere := remote.sessions["ses-REMOTE"]
	remote.mu.Unlock()
	if stillThere {
		t.Fatal("Close must have removed the session from the REMOTE instance")
	}
}

// TestPromptSession_UnknownSessionIsTypedNotFound is the error-path test: a
// prompt against a genuinely unknown session id surfaces a typed
// SessionNotFoundError (mapped to 404 by the REST layer), not a generic error.
func TestPromptSession_UnknownSessionIsTypedNotFound(t *testing.T) {
	t.Parallel()

	local := newInstanceBackend("LOCAL", "")
	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"claude": local}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	ctx := ContextWithTenant(context.Background(), "tenant-a")

	_, err = impl.PromptSession(ctx, &PromptSessionRequest{
		Backend:   "claude",
		SessionID: "ses-does-not-exist",
		Prompt:    "hello",
	})
	if err == nil {
		t.Fatal("expected an error for an unknown session")
	}
	var nf *SessionNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("expected *SessionNotFoundError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected errors.Is(ErrSessionNotFound), got %v", err)
	}
}

// TestPromptSession_ExplicitHostAffinityNotOverridden verifies the pin does not
// clobber an operator-supplied per-request affinity: when the caller sets a
// host on the follow-up prompt, that value wins over the stored one.
func TestPromptSession_ExplicitHostAffinityNotOverridden(t *testing.T) {
	t.Parallel()

	rec := &agentServiceImpl{sessions: sync.Map{}}
	rec.sessions.Store("ses-x", &sessionRecord{Backend: "claude", HostAffinity: "stored-host"})

	ctx := WithHostAffinity(context.Background(), "explicit-host")
	got := rec.pinSessionBackend(ctx, "ses-x")
	if HostAffinityFromContext(got) != "explicit-host" {
		t.Fatalf("explicit affinity must win, got %q", HostAffinityFromContext(got))
	}
}

// TestPinSessionHostAffinity_UnknownAndLocalSessionsAreNoops covers the guard
// branches: an unknown session and a session with no stored host both leave the
// context unchanged.
func TestPinSessionHostAffinity_UnknownAndLocalSessionsAreNoops(t *testing.T) {
	t.Parallel()

	rec := &agentServiceImpl{sessions: sync.Map{}}
	rec.sessions.Store("ses-local", &sessionRecord{Backend: "claude"}) // no host

	base := context.Background()
	if got := rec.pinSessionBackend(base, "ses-unknown"); HostAffinityFromContext(got) != "" {
		t.Fatal("unknown session must not set host affinity")
	}
	if got := rec.pinSessionBackend(base, "ses-local"); HostAffinityFromContext(got) != "" {
		t.Fatal("local session (empty host) must not set host affinity")
	}
}

// TestSessionHostAffinity_BackendVsRequestFallback covers the helper's two
// sources: a host-bound backend's ResolvedHost takes precedence; otherwise the
// request affinity is used.
func TestSessionHostAffinity_BackendVsRequestFallback(t *testing.T) {
	t.Parallel()

	hostBound := newInstanceBackend("REMOTE", "daemon-9")
	if got := sessionHostAffinity(hostBound, "ignored"); got != "daemon-9" {
		t.Fatalf("host-bound backend host must win, got %q", got)
	}

	localOnly := newInstanceBackend("LOCAL", "")
	if got := sessionHostAffinity(localOnly, "req-host"); got != "req-host" {
		t.Fatalf("request affinity must be the fallback, got %q", got)
	}
	if got := sessionHostAffinity(localOnly, ""); got != "" {
		t.Fatalf("no host anywhere must yield empty, got %q", got)
	}
}

// lateRemoteResolver is a RemoteBackendResolver whose remote daemon becomes
// available only AFTER it is armed. This reproduces the registration-timing
// race: at Spawn the daemon is not yet registered (resolver declines, spawn
// lands LOCAL); by Prompt the daemon has registered (resolver would now claim
// "claude"). The fix must keep the locally-spawned session on the local
// backend regardless.
type lateRemoteResolver struct {
	remote *instanceBackend
	armed  atomic.Bool
}

func (r *lateRemoteResolver) arm() { r.armed.Store(true) }

func (r *lateRemoteResolver) resolve(_ context.Context, name, _ string) (AgentBackend, bool) {
	if name != "claude" {
		return nil, false
	}
	if !r.armed.Load() {
		return nil, false // daemon not registered yet → fall through to local
	}
	return r.remote, true
}

// newLocalSpawnSvc wires an AgentService with a local "claude" backend and a
// resolver that is initially disarmed, so Spawn deterministically lands on the
// local backend. The returned resolver can be armed mid-test to simulate a
// remote daemon registering after Spawn.
func newLocalSpawnSvc(t *testing.T, local, remote *instanceBackend) (*agentServiceImpl, *lateRemoteResolver) {
	t.Helper()
	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"claude": local}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	res := &lateRemoteResolver{remote: remote}
	impl.SetRemoteBackendResolver(res.resolve)
	return impl, res
}

// TestPromptSession_LocalSpawnNotHijackedByLateRemote is the confirmed bug:
// Spawn resolves to the LOCAL backend (the embedded daemon is not yet
// registered), then a remote daemon becomes available, and the follow-up Prompt
// must STILL reach the local backend where the live session lives — not the
// freshly-registered (empty) remote instance.
//
// Before the fix the follow-up re-ran the resolver, which now claimed "claude"
// for the remote daemon, and Prompt returned SessionNotFoundError. After the
// fix the session is pinned local at Spawn and the resolver is bypassed.
func TestPromptSession_LocalSpawnNotHijackedByLateRemote(t *testing.T) {
	t.Parallel()

	local := newInstanceBackend("LOCAL", "")
	remote := newInstanceBackend("REMOTE", "embedded-agent-01")
	impl, res := newLocalSpawnSvc(t, local, remote)

	ctx := ContextWithTenant(context.Background(), "system")

	// Spawn with no host affinity; resolver is disarmed → lands LOCAL.
	spawnResp, err := impl.SpawnSession(ctx, &SpawnSessionRequest{Backend: "claude"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}
	if local.spawns != 1 || remote.spawns != 0 {
		t.Fatalf("spawn must land LOCAL: local=%d remote=%d", local.spawns, remote.spawns)
	}

	// The embedded daemon registers AFTER spawn (registration-timing race).
	res.arm()

	// Follow-up prompt must STILL reach LOCAL — not the empty remote.
	promptResp, err := impl.PromptSession(ctx, &PromptSessionRequest{
		SessionID: spawnResp.SessionID,
		Prompt:    "hello",
	})
	if err != nil {
		t.Fatalf("PromptSession must find the local session, got: %v", err)
	}
	if errors.Is(err, ErrSessionNotFound) {
		t.Fatal("must not be session-not-found")
	}
	if len(promptResp.Content) == 0 || promptResp.Content[0].Text != "LOCAL" {
		t.Fatalf("prompt must reach the LOCAL instance, got %+v", promptResp.Content)
	}
	if remote.spawns != 0 {
		t.Fatalf("remote must never have been touched, spawns=%d", remote.spawns)
	}
}

// TestSessionStatusAndClose_LocalSpawnPinned proves Status and Close also stay
// on the local backend after a remote daemon registers, mirroring the Prompt
// path for the full session lifecycle.
func TestSessionStatusAndClose_LocalSpawnPinned(t *testing.T) {
	t.Parallel()

	local := newInstanceBackend("LOCAL", "")
	remote := newInstanceBackend("REMOTE", "embedded-agent-01")
	impl, res := newLocalSpawnSvc(t, local, remote)

	ctx := ContextWithTenant(context.Background(), "system")

	spawnResp, err := impl.SpawnSession(ctx, &SpawnSessionRequest{Backend: "claude"})
	if err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	res.arm() // remote daemon registers after spawn

	st, err := impl.SessionStatus(ctx, &SessionStatusRequest{SessionID: spawnResp.SessionID})
	if err != nil {
		t.Fatalf("SessionStatus must find the local session, got: %v", err)
	}
	if st.State != SessionStateActive {
		t.Fatalf("unexpected state %q", st.State)
	}

	if _, err := impl.CloseSession(ctx, &CloseSessionRequest{SessionID: spawnResp.SessionID}); err != nil {
		t.Fatalf("CloseSession must find the local session, got: %v", err)
	}
	local.mu.Lock()
	stillLocal := local.sessions["ses-LOCAL"]
	local.mu.Unlock()
	if stillLocal {
		t.Fatal("Close must have removed the session from the LOCAL instance")
	}
	remote.mu.Lock()
	remoteTouched := remote.spawns
	remote.mu.Unlock()
	if remoteTouched != 0 {
		t.Fatalf("remote must never have been touched, spawns=%d", remoteTouched)
	}
}

// TestPinSessionBackend_LocalPinOverridesExplicitAffinity verifies that a
// local-spawned session is pinned local even when the caller supplies an
// explicit per-request host affinity: the live session physically lives only on
// the local instance, so the local pin must win to avoid a guaranteed miss.
func TestPinSessionBackend_LocalPinOverridesExplicitAffinity(t *testing.T) {
	t.Parallel()

	rec := &agentServiceImpl{sessions: sync.Map{}}
	rec.sessions.Store("ses-local", &sessionRecord{Backend: "claude", BackendLocal: true})

	ctx := WithHostAffinity(context.Background(), "some-daemon")
	got := rec.pinSessionBackend(ctx, "ses-local")
	if !localBackendPinned(got) {
		t.Fatal("local-spawned session must carry the local backend pin")
	}
}

// TestLookupBackendCtx_LocalPinBypassesResolver proves the local pin makes
// lookupBackendCtx skip the resolver and return the local backend even when the
// resolver would otherwise claim the name.
func TestLookupBackendCtx_LocalPinBypassesResolver(t *testing.T) {
	t.Parallel()

	local := newInstanceBackend("LOCAL", "")
	remote := newInstanceBackend("REMOTE", "daemon-1")
	svc, err := NewAgentService(NewInMemoryAgentStore(),
		map[string]AgentBackend{"claude": local}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetRemoteBackendResolver(func(_ context.Context, name, _ string) (AgentBackend, bool) {
		if name == "claude" {
			return remote, true
		}
		return nil, false
	})

	// Without the pin the resolver wins (returns remote).
	got, err := impl.lookupBackendCtx(context.Background(), "claude", "")
	if err != nil {
		t.Fatalf("lookupBackendCtx: %v", err)
	}
	if got != AgentBackend(remote) {
		t.Fatal("without pin the resolver must win")
	}

	// With the pin the local backend is returned.
	got, err = impl.lookupBackendCtx(withLocalBackendPin(context.Background()), "claude", "")
	if err != nil {
		t.Fatalf("lookupBackendCtx pinned: %v", err)
	}
	if got != AgentBackend(local) {
		t.Fatal("local pin must bypass the resolver and return the local backend")
	}
}
