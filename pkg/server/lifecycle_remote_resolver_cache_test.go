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

// lifecycle_remote_resolver_cache_test.go covers the backend cache in
// backendFor, which is keyed on BOTH axes that vary: the daemon host and the
// backend the caller asked for.
//
// The second axis is go-quicbaas#937. The cache used to key on the host alone,
// so the first request to a daemon decided which backend every later request to
// it would be served by: a session resolved for codex was handed the instance
// built for claude, which then asked the daemon for claude. It ran the wrong
// model family under the right name, and nothing reported the mismatch.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// remoteDaemonReg builds a remote daemon registration with the given host and
// id, serving the backends its capability set names. The primary is the first
// one, exactly as a daemon's heartbeat reconciliation sets it (see
// orchestration.reconcileAgentBackends).
func remoteDaemonReg(id, host string, backends ...string) *orchestration.AgentRegistration {
	if len(backends) == 0 {
		backends = []string{"ollama"}
	}
	return &orchestration.AgentRegistration{
		ID:           id,
		Name:         id,
		Host:         host,
		Backend:      backends[0],
		Capabilities: append([]string(nil), backends...),
		Kind:         orchestration.AgentKindDaemon,
		Status:       orchestration.AgentStatusIdle,
	}
}

// cachePool builds a pool over the given registrations with no embedded
// dialers, so every lookup takes the remote-daemon construction path.
func cachePool(regs ...*orchestration.AgentRegistration) *remoteBackendPool {
	return &remoteBackendPool{
		registry:    &strictMockRegistry{agents: regs},
		centralCred: &OrchestratorDaemonClient{ClientID: "id", ClientSecret: "sec"},
		tokenURL:    "https://127.0.0.1:19999/oauth2/token",
		logger:      strictNoopLogger{},
		entries:     map[string]*pooledDaemon{},
	}
}

// cacheCtx is a tenant-scoped context; the pool reads the tenant on resolve.
func cacheCtx() context.Context {
	return orchestration.ContextWithTenant(context.Background(), "default")
}

// remoteBackendOrFail asserts backendFor succeeded and returns the concrete
// backend so the name it serves can be read off it.
func remoteBackendOrFail(
	t *testing.T,
	pool *remoteBackendPool,
	reg *orchestration.AgentRegistration,
	backend string,
) *orchestration.RemoteACPBackend {
	t.Helper()

	got, ok := pool.backendFor(cacheCtx(), reg, backend)
	if !ok || got == nil {
		t.Fatalf("backendFor(%s, %q) must construct a backend", reg.Host, backend)
	}
	remote, isRemote := got.(*orchestration.RemoteACPBackend)
	if !isRemote {
		t.Fatalf("backendFor must return a *RemoteACPBackend, got %T", got)
	}
	return remote
}

// TestBackendFor_CacheHit covers the cache-hit branch: a second backendFor call
// for the same host + registration id + backend must return the identical
// cached backend without constructing a new one.
func TestBackendFor_CacheHit(t *testing.T) {
	t.Parallel()

	reg := remoteDaemonReg("daemon-1", "10.0.0.9:9090")
	pool := cachePool(reg)

	first := remoteBackendOrFail(t, pool, reg, "ollama")
	second := remoteBackendOrFail(t, pool, reg, "ollama")
	if first != second {
		t.Fatal("backendFor must return the cached backend on a repeat lookup")
	}
}

// TestBackendFor_BackendsOnOneHostDoNotCollide is the defect itself. One daemon
// serving claude, codex and opencode must yield three DISTINCT instances, each
// naming the backend it was asked for. Keyed on the host alone, all three
// lookups returned the first instance built — the one naming claude.
func TestBackendFor_BackendsOnOneHostDoNotCollide(t *testing.T) {
	t.Parallel()

	reg := remoteDaemonReg("vm-host-4-id", "198.51.100.91:9091", "claude", "codex", "opencode")
	pool := cachePool(reg)

	backends := []string{"claude", "codex", "opencode"}
	seen := make(map[string]*orchestration.RemoteACPBackend, len(backends))

	for _, backend := range backends {
		remote := remoteBackendOrFail(t, pool, reg, backend)
		if remote.Backend() != backend {
			t.Fatalf("backendFor(%q) returned an instance serving %q.\n"+
				"A daemon's non-primary backends are unreachable when the cache key omits the\n"+
				"requested backend: the request is silently served by the primary "+
				"(go-quicbaas#937).",
				backend, remote.Backend())
		}
		if remote.Registration().Host != reg.Host {
			t.Fatalf("backend %q routed to host %q, want %q",
				backend, remote.Registration().Host, reg.Host)
		}
		for prior, priorBackend := range seen {
			if priorBackend == remote {
				t.Fatalf("backends %q and %q share one instance; each must name its own backend",
					prior, backend)
			}
		}
		seen[backend] = remote
	}

	if len(seen) != len(backends) {
		t.Fatalf("expected %d cached instances, got %d", len(backends), len(seen))
	}
}

// TestBackendFor_RefusesUnservedBackend is the bad-input twin: a registration
// asked for a backend it does not serve is refused, not served by its primary.
// The refusal is the typed NoMatchingAgentError the resolver already speaks,
// stashed on the context so the public API surfaces it rather than the request
// falling through to a local backend.
func TestBackendFor_RefusesUnservedBackend(t *testing.T) {
	t.Parallel()

	reg := remoteDaemonReg("daemon-serves-two", "10.0.0.11:9090", "claude", "codex")

	tests := map[string]string{
		"a backend the daemon does not serve": "gemini",
		"an empty backend name":               "",
	}

	for name, requested := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			pool := cachePool(reg)

			backend, ok := pool.backendFor(cacheCtx(), reg, requested)
			if ok || backend != nil {
				t.Fatalf("backendFor must refuse backend %q, which the registration does not serve",
					requested)
			}

			pool.mu.Lock()
			entry := pool.entries[reg.Host]
			pool.mu.Unlock()
			if entry != nil {
				if _, cached := entry.backends[requested]; cached {
					t.Fatal("a refused backend must not be cached")
				}
			}
		})
	}
}

// TestBackendFor_StashesTheTypedRefusal pins the surfacing half of the refusal:
// the caller gets the resolver's own NoMatchingAgentError rather than a silent
// (nil, false) that the dispatcher would read as "fall through to a local
// backend" — which is how a wrongly-routed session used to start at all.
func TestBackendFor_StashesTheTypedRefusal(t *testing.T) {
	t.Parallel()

	reg := remoteDaemonReg("daemon-primary-only", "10.0.0.12:9090", "claude")
	pool := cachePool(reg)

	holder := orchestration.NewResolveErrorHolder()
	ctx := orchestration.WithResolveErrorHolder(cacheCtx(), holder)

	if _, ok := pool.backendFor(ctx, reg, "codex"); ok {
		t.Fatal("backendFor must refuse a backend the daemon does not serve")
	}

	stashed := holder.Err()
	if stashed == nil {
		t.Fatal("the refusal must be stashed so the API can surface it, not dropped")
	}
	var noMatch *orchestration.NoMatchingAgentError
	if !errors.As(stashed, &noMatch) {
		t.Fatalf("stashed error must be *NoMatchingAgentError, got %T: %v", stashed, stashed)
	}
	if noMatch.Constraint != "backend" {
		t.Errorf("constraint = %q, want %q", noMatch.Constraint, "backend")
	}
	if !errors.Is(stashed, orchestration.ErrNoMatchingAgent) {
		t.Error("the refusal must unwrap to ErrNoMatchingAgent for the API's error mapping")
	}
}

// TestBackendFor_StaleEvictionOnNewRegistration covers the stale-cache eviction
// branch: a lookup for the same host but a DIFFERENT registration id (the daemon
// was replaced) must evict the stale entry and build a fresh backend — and must
// do so for EVERY backend the host had cached, not only the one being looked up.
func TestBackendFor_StaleEvictionOnNewRegistration(t *testing.T) {
	t.Parallel()

	const host = "10.0.0.10:9090"
	old := remoteDaemonReg("daemon-old", host, "claude", "codex")
	replacement := remoteDaemonReg("daemon-new", host, "claude", "codex") // same host, new id

	pool := cachePool(old)

	oldClaude := remoteBackendOrFail(t, pool, old, "claude")
	oldCodex := remoteBackendOrFail(t, pool, old, "codex")

	newClaude := remoteBackendOrFail(t, pool, replacement, "claude")
	if oldClaude == newClaude {
		t.Fatal("a new registration id for the same host must evict the stale backend")
	}

	// The codex instance belonged to the replaced daemon too. Invalidation is
	// per HOST, so it must be gone even though the lookup that triggered the
	// eviction asked for claude.
	newCodex := remoteBackendOrFail(t, pool, replacement, "codex")
	if oldCodex == newCodex {
		t.Fatal("replacing the daemon must invalidate every backend cached for its host")
	}
	if newCodex.Backend() != "codex" {
		t.Fatalf("rebuilt instance serves %q, want %q", newCodex.Backend(), "codex")
	}
	if newCodex.Registration().ID != replacement.ID {
		t.Fatalf("rebuilt instance bound to registration %q, want %q",
			newCodex.Registration().ID, replacement.ID)
	}

	// The cache must now hold the replacement's registration id and both of
	// its freshly built backends.
	pool.mu.Lock()
	entry := pool.entries[host]
	pool.mu.Unlock()
	if entry == nil || entry.registrationID != replacement.ID {
		t.Fatalf("cache must hold the replacement registration id, got %+v", entry)
	}
	if len(entry.backends) != 2 {
		t.Fatalf("cache must hold both rebuilt backends, got %d", len(entry.backends))
	}
}

// newPooledRemoteBackend builds a RemoteACPBackend outside the pool, standing
// in for the instance a concurrent caller constructed before the pool's own
// store landed.
func newPooledRemoteBackend(
	t *testing.T,
	reg *orchestration.AgentRegistration,
	backend string,
) *orchestration.RemoteACPBackend {
	t.Helper()

	built, err := orchestration.NewRemoteACPBackend(orchestration.RemoteACPBackendDeps{
		Registration: reg,
		Backend:      backend,
		Logger:       strictNoopLogger{},
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend(backend=%q): %v", backend, err)
	}
	t.Cleanup(func() { _ = built.CloseAll() })
	return built
}

// TestStoreBackend_FirstWriterWinsForOnePair pins the loser's path of the
// construct-then-store race: two callers can both miss the cache and both
// build, and the pool must hand both of them the SAME instance so one
// (daemon, backend) pair keeps one gRPC conn pool.
func TestStoreBackend_FirstWriterWinsForOnePair(t *testing.T) {
	t.Parallel()

	reg := remoteDaemonReg("daemon-store", "10.0.0.13:9090", "claude", "codex")
	pool := cachePool(reg)

	first := remoteBackendOrFail(t, pool, reg, "claude")
	loser := newPooledRemoteBackend(t, reg, "claude")

	if got := pool.storeBackend(reg, "claude", loser); got != first {
		t.Fatal("storeBackend must return the instance already cached for the pair")
	}

	// A different backend on the same host is stored BESIDE it, not over it.
	codex := newPooledRemoteBackend(t, reg, "codex")
	if got := pool.storeBackend(reg, "codex", codex); got != codex {
		t.Fatal("a second backend on one host must be stored, not collapsed onto the first")
	}
	if cached := remoteBackendOrFail(t, pool, reg, "claude"); cached != first {
		t.Fatal("storing a second backend must not disturb the first")
	}
}

// TestStoreBackend_StaleEntryIsReplaced is the bad-input twin: storing against
// a registration id the host's entry does not hold means the daemon was
// replaced, so the entry is rebuilt rather than the new instance being filed
// under the dead daemon's id.
func TestStoreBackend_StaleEntryIsReplaced(t *testing.T) {
	t.Parallel()

	const host = "10.0.0.14:9090"
	old := remoteDaemonReg("daemon-gone", host, "claude")
	replacement := remoteDaemonReg("daemon-fresh", host, "claude")

	pool := cachePool(old)
	stale := remoteBackendOrFail(t, pool, old, "claude")

	fresh := newPooledRemoteBackend(t, replacement, "claude")
	if got := pool.storeBackend(replacement, "claude", fresh); got != fresh {
		t.Fatal("a store under a new registration id must not return the stale instance")
	}

	pool.mu.Lock()
	entry := pool.entries[host]
	pool.mu.Unlock()
	if entry == nil || entry.registrationID != replacement.ID {
		t.Fatalf("entry must be rebuilt for the replacement, got %+v", entry)
	}
	if len(entry.backends) != 1 {
		t.Fatalf("the rebuilt entry must hold only the fresh instance, got %d", len(entry.backends))
	}
	if entry.backends["claude"] == stale {
		t.Fatal("the stale instance must not survive the rebuild")
	}
}
