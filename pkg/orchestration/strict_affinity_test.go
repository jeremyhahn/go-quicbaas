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
	"errors"
	"sync/atomic"
	"testing"
)

// newStrictAffinityService is a service whose LOCAL table serves opencode and
// whose fleet places nothing: the pinned host is offline.
func newStrictAffinityService(t *testing.T, recorded error) (*agentServiceImpl, *atomic.Int32) {
	t.Helper()
	var localSpawns atomic.Int32
	local := &mockAgentBackend{backendType: "acp", spawnFn: func(_ context.Context, req *SpawnRequest) (*Session, error) {
		localSpawns.Add(1)
		return &Session{ID: "local-1", Backend: "opencode", Status: SessionStateActive, WorkDir: req.WorkDir}, nil
	}}
	svc, err := NewAgentService(NewInMemoryAgentStore(), map[string]AgentBackend{"opencode": local}, &agentTestLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	impl := svc.(*agentServiceImpl)
	impl.SetRemoteBackendResolver(func(ctx context.Context, _, _ string) (AgentBackend, bool) {
		StashResolveError(ctx, recorded)
		return nil, false
	})
	return impl, &localSpawns
}

// TestSpawnSession_StrictAffinityMissRefusesTheLocalBackend: a reviewer pinned
// strictly to the host that can reach its model server must not run on this
// daemon's own opencode when that host is offline. It is refused, with the
// pin named, and the local backend is never spawned.
func TestSpawnSession_StrictAffinityMissRefusesTheLocalBackend(t *testing.T) {
	svc, localSpawns := newStrictAffinityService(t, nil)
	ctx := ContextWithTenant(context.Background(), "tenant-1")

	_, err := svc.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "opencode", HostAffinity: "gpu-host", HostAffinityStrict: true,
	})
	var refusal *NoMatchingFleetNodeError
	if !errors.As(err, &refusal) || refusal.Constraint != "host_affinity" || !errors.Is(err, ErrNoMatchingFleetNode) {
		t.Fatalf("SpawnSession() error = %v, want *NoMatchingFleetNodeError on host_affinity", err)
	}
	if n := localSpawns.Load(); n != 0 {
		t.Fatalf("the local backend was spawned %d times under a strict pin to another host", n)
	}
}

// TestSpawnSession_StrictAffinityMissKeepsTheResolversAccount: when the
// resolver recorded why nothing matched, that more specific refusal is the
// one returned.
func TestSpawnSession_StrictAffinityMissKeepsTheResolversAccount(t *testing.T) {
	recorded := &NoMatchingFleetNodeError{Constraint: "host_affinity", Detail: "gpu-host is offline"}
	svc, localSpawns := newStrictAffinityService(t, recorded)
	ctx := WithResolveErrorHolder(ContextWithTenant(context.Background(), "tenant-1"), NewResolveErrorHolder())

	_, err := svc.SpawnSession(ctx, &SpawnSessionRequest{
		Backend: "opencode", HostAffinity: "gpu-host", HostAffinityStrict: true,
	})
	if !errors.Is(err, recorded) {
		t.Fatalf("SpawnSession() error = %v, want the resolver's recorded refusal", err)
	}
	if localSpawns.Load() != 0 {
		t.Fatal("the local backend was spawned under a strict pin")
	}
}

// TestSpawnSession_SoftAffinityStillFallsThrough: a pin that is only a hint
// keeps the local fall-through it always had.
func TestSpawnSession_SoftAffinityStillFallsThrough(t *testing.T) {
	svc, localSpawns := newStrictAffinityService(t, nil)
	ctx := ContextWithTenant(context.Background(), "tenant-1")

	if _, err := svc.SpawnSession(ctx, &SpawnSessionRequest{Backend: "opencode", HostAffinity: "gpu-host"}); err != nil {
		t.Fatalf("SpawnSession() error = %v", err)
	}
	if localSpawns.Load() != 1 {
		t.Fatalf("local spawns = %d, want the soft pin to fall through once", localSpawns.Load())
	}
}

func TestStrictAffinityRefusal_NoPinNoRefusal(t *testing.T) {
	if err := strictAffinityRefusal(context.Background(), "opencode"); err != nil {
		t.Fatalf("strictAffinityRefusal() = %v with no pin", err)
	}
	ctx := WithHostAffinityStrict(context.Background())
	if err := strictAffinityRefusal(ctx, "opencode"); err != nil {
		t.Fatalf("strictAffinityRefusal() = %v with a strict bit but no host", err)
	}
}
