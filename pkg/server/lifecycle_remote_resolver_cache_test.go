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

// lifecycle_remote_resolver_cache_test.go covers the per-host backend cache in
// backendFor: a repeat lookup for the same registration returns the cached
// backend, and a lookup for the same host with a NEW registration id evicts the
// stale entry and constructs a fresh backend (daemon-replaced semantics).

package server

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// remoteDaemonReg builds a remote daemon registration with the given host and id.
func remoteDaemonReg(id, host string) *orchestration.AgentRegistration {
	return &orchestration.AgentRegistration{
		ID:      id,
		Name:    id,
		Host:    host,
		Backend: "ollama",
		Kind:    orchestration.AgentKindDaemon,
		Status:  orchestration.AgentStatusIdle,
	}
}

// TestBackendFor_CacheHit covers the cache-hit branch: a second backendFor call
// for the same host + registration id must return the identical cached backend
// without constructing a new one.
func TestBackendFor_CacheHit(t *testing.T) {
	t.Parallel()

	reg := remoteDaemonReg("daemon-1", "10.0.0.9:9090")
	pool := &remoteBackendPool{
		registry:    &strictMockRegistry{agents: []*orchestration.AgentRegistration{reg}},
		centralCred: &OrchestratorDaemonClient{ClientID: "id", ClientSecret: "sec"},
		tokenURL:    "https://127.0.0.1:19999/oauth2/token",
		logger:      strictNoopLogger{},
		entries:     map[string]*pooledBackend{},
	}

	ctx := orchestration.ContextWithTenant(context.Background(), "default")

	first, ok := pool.backendFor(ctx, reg)
	if !ok || first == nil {
		t.Fatal("first backendFor must construct a backend")
	}
	second, ok := pool.backendFor(ctx, reg)
	if !ok || second == nil {
		t.Fatal("second backendFor must return the cached backend")
	}
	if first != second {
		t.Fatal("backendFor must return the cached backend on a repeat lookup")
	}
}

// TestBackendFor_StaleEvictionOnNewRegistration covers the stale-cache eviction
// branch: a lookup for the same host but a DIFFERENT registration id (the daemon
// was replaced) must evict the stale entry and build a fresh backend.
func TestBackendFor_StaleEvictionOnNewRegistration(t *testing.T) {
	t.Parallel()

	host := "10.0.0.10:9090"
	old := remoteDaemonReg("daemon-old", host)
	replacement := remoteDaemonReg("daemon-new", host) // same host, new id

	pool := &remoteBackendPool{
		registry:    &strictMockRegistry{agents: []*orchestration.AgentRegistration{old}},
		centralCred: &OrchestratorDaemonClient{ClientID: "id", ClientSecret: "sec"},
		tokenURL:    "https://127.0.0.1:19999/oauth2/token",
		logger:      strictNoopLogger{},
		entries:     map[string]*pooledBackend{},
	}

	ctx := orchestration.ContextWithTenant(context.Background(), "default")

	firstBackend, ok := pool.backendFor(ctx, old)
	if !ok || firstBackend == nil {
		t.Fatal("first backendFor must construct a backend for the original daemon")
	}

	secondBackend, ok := pool.backendFor(ctx, replacement)
	if !ok || secondBackend == nil {
		t.Fatal("backendFor must construct a fresh backend after the daemon is replaced")
	}
	if firstBackend == secondBackend {
		t.Fatal("a new registration id for the same host must evict the stale backend")
	}
	// The cache must now hold the replacement's registration id.
	pool.mu.Lock()
	entry := pool.entries[host]
	pool.mu.Unlock()
	if entry == nil || entry.registrationID != replacement.ID {
		t.Fatalf("cache must hold the replacement registration id, got %+v", entry)
	}
}
