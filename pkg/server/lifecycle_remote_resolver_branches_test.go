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

// lifecycle_remote_resolver_branches_test.go covers the resolve()/pickDaemon()
// guard branches that the strict-mode fixtures do not reach: the empty-tenant
// short-circuit, the registry-list-error continue, and the nil / backend-
// mismatch skips inside pickDaemon. These reuse the strictMockRegistry and
// onlineDaemon fixtures from lifecycle_remote_resolver_strict_test.go.

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// erroringRegistry is an AgentRegistry whose ListAgents always fails, driving
// the list-error continue branch of resolve(). The remaining methods embed the
// strictMockRegistry stubs so the interface stays satisfied.
type erroringRegistry struct {
	strictMockRegistry
	err error
}

func (e *erroringRegistry) ListAgents(context.Context, string, *orchestration.AgentFilter) ([]*orchestration.AgentRegistration, error) {
	return nil, e.err
}

// TestResolve_EmptyTenant_ShortCircuits covers the empty-tenant guard: with no
// tenant on the context, resolve must return (nil, false) without consulting
// the registry at all.
func TestResolve_EmptyTenant_ShortCircuits(t *testing.T) {
	t.Parallel()

	pool := newStrictTestPool(config.HostAffinityModeSoft,
		onlineDaemon("alpha", "10.0.0.1:9090", "ollama"))

	// No tenant bound on the context.
	backend, ok := pool.resolve(context.Background(), "ollama", "")
	if backend != nil || ok {
		t.Fatalf("expected (nil, false) when no tenant is bound, got (%v, %v)", backend, ok)
	}
}

// TestResolve_ListAgentsError_FallsThrough covers the list-error continue
// branch: when ListAgents fails for every status value, resolve collects no
// registrations and returns (nil, false) so the dispatcher falls back to local
// backends.
func TestResolve_ListAgentsError_FallsThrough(t *testing.T) {
	t.Parallel()

	pool := &remoteBackendPool{
		registry:        &erroringRegistry{err: errors.New("registry unavailable")},
		hostAffinityCfg: config.SchedulerHostAffinityConfig{Mode: config.HostAffinityModeSoft},
		logger:          strictNoopLogger{},
		entries:         map[string]*pooledDaemon{},
	}

	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")
	backend, ok := pool.resolve(ctx, "ollama", "")
	if backend != nil || ok {
		t.Fatalf("expected (nil, false) when the registry list fails, got (%v, %v)", backend, ok)
	}
}

// TestPickDaemon_SkipsNilAndBackendMismatch covers the nil-registration skip
// and the backend-mismatch skip inside pickDaemon: a nil entry and an agent
// serving a different backend are both filtered out, leaving the backend
// constraint as the reason no daemon matched.
func TestPickDaemon_SkipsNilAndBackendMismatch(t *testing.T) {
	t.Parallel()

	pool := newStrictTestPool(config.HostAffinityModeSoft)

	regs := []*orchestration.AgentRegistration{
		nil, // skipped: nil entry
		onlineDaemon("beta", "10.0.0.2:9090", "vllm"), // skipped: different backend
	}

	reg, constraint := pool.pickDaemon(regs, "ollama", "", false, "")
	if reg != nil {
		t.Fatalf("expected no matching daemon, got %v", reg)
	}
	if constraint != "backend" {
		t.Fatalf("expected the backend constraint to be the blocker, got %q", constraint)
	}
}
