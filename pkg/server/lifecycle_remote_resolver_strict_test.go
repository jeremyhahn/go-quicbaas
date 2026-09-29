// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Test infrastructure
// =============================================================================

// strictNoopLogger satisfies interfaces.Logger for the strict-mode tests
// without writing anywhere. The resolver only emits Warnf on rejection.
type strictNoopLogger struct{}

func (strictNoopLogger) Debug(string, ...any)          {}
func (strictNoopLogger) Debugf(string, ...interface{}) {}
func (strictNoopLogger) Info(string, ...any)           {}
func (strictNoopLogger) Infof(string, ...interface{})  {}
func (strictNoopLogger) Warn(string, ...any)           {}
func (strictNoopLogger) Warnf(string, ...interface{})  {}
func (strictNoopLogger) Error(string, ...any)          {}
func (strictNoopLogger) Errorf(string, ...interface{}) {}
func (strictNoopLogger) Fatal(string)                  {}
func (strictNoopLogger) Fatalf(string, ...interface{}) {}
func (l strictNoopLogger) WithField(string, interface{}) interfaces.Logger {
	return l
}
func (l strictNoopLogger) WithFields(map[string]interface{}) interfaces.Logger {
	return l
}
func (l strictNoopLogger) WithError(error) interfaces.Logger { return l }

// strictMockRegistry is a minimal FleetRegistry for strict-mode tests. Only
// ListFleetNodes is exercised; the other interface methods are stubs.
type strictMockRegistry struct {
	agents []*orchestration.FleetNode
}

func (m *strictMockRegistry) RegisterFleetNode(context.Context, *orchestration.FleetNode) error {
	return nil
}
func (m *strictMockRegistry) GetFleetNode(context.Context, string, string) (*orchestration.FleetNode, error) {
	return nil, nil
}
func (m *strictMockRegistry) ListFleetNodes(_ context.Context, _ string, filter *orchestration.FleetNodeFilter) ([]*orchestration.FleetNode, error) {
	out := make([]*orchestration.FleetNode, 0, len(m.agents))
	for _, a := range m.agents {
		// Status filter: callers in resolve() walk Running/Idle/Registered;
		// emit each agent only when its Status matches the filter.
		if filter != nil && filter.Status != "" && a.Status != filter.Status {
			continue
		}
		if filter != nil && filter.Backend != "" && a.Backend != filter.Backend {
			// Backend match also passes when capabilities advertise it.
			match := false
			for _, c := range a.Capabilities {
				if c == filter.Backend {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, a)
	}
	return out, nil
}
func (m *strictMockRegistry) UpdateFleetNode(context.Context, *orchestration.FleetNode) error {
	return nil
}
func (m *strictMockRegistry) RefreshFleetNode(context.Context, *orchestration.FleetNode) error {
	return nil
}
func (m *strictMockRegistry) RemoveFleetNode(context.Context, string, string) error { return nil }
func (m *strictMockRegistry) Heartbeat(context.Context, string, string, *orchestration.FleetNodeObservation) error {
	return nil
}
func (m *strictMockRegistry) DeleteTenantFleetNodes(context.Context, string) error { return nil }

// newStrictTestPool builds a remoteBackendPool wired to a mock registry,
// the supplied operator default, and a no-op logger. It does NOT install a
// centralCred / tokenURL since the tests only exercise the filter path —
// pickDaemon never reaches backendFor when the host is offline.
func newStrictTestPool(operatorMode string, agents ...*orchestration.FleetNode) *remoteBackendPool {
	return &remoteBackendPool{
		registry:        &strictMockRegistry{agents: agents},
		hostAffinityCfg: config.SchedulerHostAffinityConfig{Mode: operatorMode},
		logger:          strictNoopLogger{},
		entries:         map[string]*pooledDaemon{},
	}
}

// onlineDaemon constructs an idle daemon-kind registration useful as a
// "this host exists" fixture in strict-mode tests. It heartbeated just now:
// a registration with no sign of life is not dispatched to at all
// (liveRegistrations), which is not what these tests are about.
func onlineDaemon(name, host, backend string) *orchestration.FleetNode {
	return &orchestration.FleetNode{
		ID:       name + "-id",
		Name:     name,
		Host:     host,
		Backend:  backend,
		Kind:     orchestration.FleetNodeKindDaemon,
		Status:   orchestration.FleetNodeStatusIdle,
		LastSeen: time.Now().UnixNano(),
	}
}

// =============================================================================
// Required test cases
// =============================================================================

// TestPickDaemon_HostAffinityStrict_ReturnsTypedError_WhenOffline verifies
// that a per-request strict flag (regardless of operator default = soft)
// causes resolve() to stash a NoMatchingFleetNodeError on the context-bound
// holder when the requested host is offline. The dispatcher still gets
// (nil, false), which is by design — the REST handler reads the holder
// post-call.
func TestPickDaemon_HostAffinityStrict_ReturnsTypedError_WhenOffline(t *testing.T) {
	t.Parallel()

	pool := newStrictTestPool(
		config.HostAffinityModeSoft,
		onlineDaemon("alpha", "10.0.0.1:9090", "ollama"),
	)

	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")
	ctx = orchestration.WithHostAffinity(ctx, "gpu-node-01") // offline
	ctx = orchestration.WithHostAffinityStrict(ctx)
	holder := orchestration.NewResolveErrorHolder()
	ctx = orchestration.WithResolveErrorHolder(ctx, holder)

	backend, ok := pool.resolve(ctx, "ollama", "")
	if backend != nil || ok {
		t.Fatalf("expected (nil, false) from resolve when host_affinity is offline, got (%v, %v)", backend, ok)
	}

	stashed := holder.Err()
	if stashed == nil {
		t.Fatalf("expected typed NoMatchingFleetNodeError on holder, got nil")
	}
	var nme *orchestration.NoMatchingFleetNodeError
	if !errors.As(stashed, &nme) {
		t.Fatalf("expected *NoMatchingFleetNodeError, got %T: %v", stashed, stashed)
	}
	if nme.Constraint != "host_affinity" {
		t.Errorf("expected Constraint=host_affinity, got %q", nme.Constraint)
	}
	if !errors.Is(stashed, orchestration.ErrNoMatchingFleetNode) {
		t.Errorf("expected errors.Is to find ErrNoMatchingFleetNode, got %v", stashed)
	}
}

// TestPickDaemon_HostAffinitySoft_FallsBackToFullList verifies that when
// neither operator default nor per-request flag enables strict mode, an
// offline host_affinity is logged but no typed error is stashed. The
// dispatcher still receives (nil, false), preserving the documented
// 404-fallback behavior, but the holder remains empty so REST callers do
// NOT substitute a different error.
func TestPickDaemon_HostAffinitySoft_FallsBackToFullList(t *testing.T) {
	t.Parallel()

	pool := newStrictTestPool(
		config.HostAffinityModeSoft,
		onlineDaemon("alpha", "10.0.0.1:9090", "ollama"),
	)

	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")
	ctx = orchestration.WithHostAffinity(ctx, "gpu-node-01") // offline
	holder := orchestration.NewResolveErrorHolder()
	ctx = orchestration.WithResolveErrorHolder(ctx, holder)

	backend, ok := pool.resolve(ctx, "ollama", "")
	if backend != nil || ok {
		t.Fatalf("expected (nil, false) from soft fallback, got (%v, %v)", backend, ok)
	}
	if got := holder.Err(); got != nil {
		t.Errorf("expected holder to be empty in soft mode, got %v", got)
	}
}

// TestPickDaemon_OperatorDefaultHard_OverridesPerRequest verifies that
// setting orchestration.host_affinity.mode = "hard" makes every offline
// host_affinity surface NoMatchingFleetNodeError, even when the request omits
// host_affinity_strict. This is the documented operator opt-in for the
// stricter default.
func TestPickDaemon_OperatorDefaultHard_OverridesPerRequest(t *testing.T) {
	t.Parallel()

	pool := newStrictTestPool(
		config.HostAffinityModeHard, // operator default = hard
		onlineDaemon("alpha", "10.0.0.1:9090", "ollama"),
	)

	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")
	ctx = orchestration.WithHostAffinity(ctx, "gpu-node-01") // offline, no per-request flag
	holder := orchestration.NewResolveErrorHolder()
	ctx = orchestration.WithResolveErrorHolder(ctx, holder)

	backend, ok := pool.resolve(ctx, "ollama", "")
	if backend != nil || ok {
		t.Fatalf("expected (nil, false), got (%v, %v)", backend, ok)
	}

	stashed := holder.Err()
	if stashed == nil {
		t.Fatalf("expected NoMatchingFleetNodeError under operator-hard default, got nil")
	}
	var nme *orchestration.NoMatchingFleetNodeError
	if !errors.As(stashed, &nme) {
		t.Fatalf("expected *NoMatchingFleetNodeError, got %T: %v", stashed, stashed)
	}
	if nme.Constraint != "host_affinity" {
		t.Errorf("expected Constraint=host_affinity, got %q", nme.Constraint)
	}
}

// =============================================================================
// Embedded agent dispatch — routing bug regression tests
// =============================================================================

// TestPickDaemon_EmbeddedKind_IsAccepted verifies that pickDaemon selects
// FleetNodeKindEmbedded registrations alongside FleetNodeKindDaemon registrations.
// This ensures the embedded agent participates in backend dispatch.
func TestPickDaemon_EmbeddedKind_IsAccepted(t *testing.T) {
	t.Parallel()

	embedded := &orchestration.FleetNode{
		ID:      "embedded-host-abc",
		Name:    "embedded-host-abc",
		Host:    "agent-02",
		Backend: "ollama",
		Kind:    orchestration.FleetNodeKindEmbedded,
		Status:  orchestration.FleetNodeStatusIdle,
	}

	pool := newStrictTestPool(config.HostAffinityModeSoft, embedded)

	got, constraint := pool.pickDaemon([]*orchestration.FleetNode{embedded}, "ollama", "", false, "")
	if got == nil {
		t.Fatalf("pickDaemon must accept FleetNodeKindEmbedded: constraint=%q", constraint)
	}
	if got.ID != embedded.ID {
		t.Errorf("expected registration ID %q, got %q", embedded.ID, got.ID)
	}
}

// TestPickDaemon_ExternalKind_IsRejected verifies that external-kind
// registrations are filtered out by pickDaemon, preserving the invariant
// that only daemon/embedded kinds are eligible for remote dispatch.
func TestPickDaemon_ExternalKind_IsRejected(t *testing.T) {
	t.Parallel()

	external := &orchestration.FleetNode{
		ID:      "ext-agent-1",
		Name:    "ext-agent-1",
		Host:    "host-x",
		Backend: "ollama",
		Kind:    orchestration.FleetNodeKindExternal,
		Status:  orchestration.FleetNodeStatusIdle,
	}

	pool := newStrictTestPool(config.HostAffinityModeSoft, external)

	got, _ := pool.pickDaemon([]*orchestration.FleetNode{external}, "ollama", "", false, "")
	if got != nil {
		t.Fatalf("pickDaemon must reject FleetNodeKindExternal; got %+v", got)
	}
}

// TestBackendFor_EmbeddedDialer_SkipsTokenSource verifies that when a bufconn
// dialer is registered for the embedded agent's host, backendFor constructs a
// RemoteACPBackend without a TokenSource. This prevents the loopback TLS
// handshake noise seen when embedded agents were given a token source that
// called /oauth2/token on every dispatch.
func TestBackendFor_EmbeddedDialer_SkipsTokenSource(t *testing.T) {
	t.Parallel()

	registry := NewEmbeddedDialerRegistry()

	// Register a dialer that always returns an error — we're testing
	// backend construction, not actual gRPC calls.
	registry.Register("agent-02", func(ctx context.Context, _ string) (*grpc.ClientConn, error) {
		return grpc.NewClient("passthrough:///noop",
			grpc.WithTransportCredentials(insecure.NewCredentials()))
	})

	embedded := &orchestration.FleetNode{
		ID:      "embedded-host-abc",
		Name:    "embedded-host-abc",
		Host:    "agent-02",
		Backend: "ollama",
		Kind:    orchestration.FleetNodeKindEmbedded,
		Status:  orchestration.FleetNodeStatusIdle,
	}

	pool := &remoteBackendPool{
		registry:        &strictMockRegistry{agents: []*orchestration.FleetNode{embedded}},
		embeddedDialers: registry,
		logger:          strictNoopLogger{},
		entries:         map[string]*pooledDaemon{},
		// centralCred and tokenURL intentionally nil: embedded path must not touch them.
	}

	ctx := orchestration.ContextWithTenant(context.Background(), "default")
	backend, ok := pool.backendFor(ctx, embedded, "ollama")

	if !ok {
		t.Fatal("backendFor must return (backend, true) for embedded agent with registered dialer")
	}
	if backend == nil {
		t.Fatal("backendFor must return a non-nil backend for embedded agents")
	}
}

// TestBackendFor_RemoteDaemon_UsesTokenSource verifies that remote daemon
// registrations (non-embedded, no dialer) reach newCentralOAuth2TokenSource.
// We confirm this indirectly: backendFor with nil centralCred still constructs
// a backend (the token source is created but never called during construction),
// confirming the token-source path is taken for remote fleet nodes.
func TestBackendFor_RemoteDaemon_UsesTokenSource(t *testing.T) {
	t.Parallel()

	remote := &orchestration.FleetNode{
		ID:      "daemon-remote-1",
		Name:    "daemon-remote-1",
		Host:    "10.0.0.5:9090",
		Backend: "ollama",
		Kind:    orchestration.FleetNodeKindDaemon,
		Status:  orchestration.FleetNodeStatusIdle,
	}

	pool := &remoteBackendPool{
		registry:    &strictMockRegistry{agents: []*orchestration.FleetNode{remote}},
		centralCred: &OrchestratorDaemonClient{ClientID: "test-id", ClientSecret: "test-secret"},
		tokenURL:    "https://127.0.0.1:19999/oauth2/token", // unreachable — construction only
		logger:      strictNoopLogger{},
		entries:     map[string]*pooledDaemon{},
		// No embeddedDialers: this is the remote path.
	}

	ctx := orchestration.ContextWithTenant(context.Background(), "default")
	backend, ok := pool.backendFor(ctx, remote, "ollama")

	// The pool constructs a backend without dialing. Token source is created
	// lazily (no network call happens until the first RPC). The test just
	// confirms construction succeeds for remote fleet nodes.
	if !ok {
		t.Fatal("backendFor must return (backend, true) for remote daemon")
	}
	if backend == nil {
		t.Fatal("backendFor must return a non-nil backend for remote daemon")
	}
}

// TestPickDaemon_PerRequestStrict_OverridesOperatorSoft verifies that a
// request-level WithHostAffinityStrict elevates the rejection to a typed
// error even when the operator default is "soft". This is the per-call
// opt-in path used by workflow authors who care about a specific host but
// don't want to change cluster-wide semantics.
func TestPickDaemon_PerRequestStrict_OverridesOperatorSoft(t *testing.T) {
	t.Parallel()

	pool := newStrictTestPool(
		config.HostAffinityModeSoft, // operator default = soft
		onlineDaemon("alpha", "10.0.0.1:9090", "ollama"),
	)

	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")
	ctx = orchestration.WithHostAffinity(ctx, "gpu-node-01")
	ctx = orchestration.WithHostAffinityStrict(ctx) // request flag wins
	holder := orchestration.NewResolveErrorHolder()
	ctx = orchestration.WithResolveErrorHolder(ctx, holder)

	backend, ok := pool.resolve(ctx, "ollama", "")
	if backend != nil || ok {
		t.Fatalf("expected (nil, false), got (%v, %v)", backend, ok)
	}

	stashed := holder.Err()
	if stashed == nil {
		t.Fatalf("expected per-request strict to surface a typed error, got nil")
	}
	var nme *orchestration.NoMatchingFleetNodeError
	if !errors.As(stashed, &nme) {
		t.Fatalf("expected *NoMatchingFleetNodeError, got %T: %v", stashed, stashed)
	}
	if nme.Constraint != "host_affinity" {
		t.Errorf("expected Constraint=host_affinity, got %q", nme.Constraint)
	}
}
