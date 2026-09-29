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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Tenant-routing test double
// =============================================================================

// tenantRoutingKV reproduces the ONE property of the production store the
// registry runs on that a plain memory store does not have: it routes every
// operation to a namespace chosen by the TENANT BOUND TO THE CONTEXT, not by
// the tenant segment in the key (pkg/server tenantRoutingKVStore ->
// go-qrdb tenant.StoreResolver). A record written under a context naming no
// tenant is therefore invisible to a read whose context names the record's
// own tenant, which is exactly the divergence go-quicbaas#663 measured on the
// live node.
//
// The empty tenant and the system tenant share one namespace, matching
// go-qrdb's IdentityNamespace fold ("" and "system" are the same tenant), so
// the embedded agent registered under the system tenant is unaffected.
type tenantRoutingKV struct {
	mu     sync.Mutex
	stores map[string]map[string][]byte
}

// newTenantRoutingKV returns an empty routing store.
func newTenantRoutingKV() *tenantRoutingKV {
	return &tenantRoutingKV{stores: make(map[string]map[string][]byte)}
}

var _ interfaces.KVStore = (*tenantRoutingKV)(nil)

// namespaceFor resolves the keyspace a context addresses. Callers hold the lock.
func (s *tenantRoutingKV) namespaceFor(ctx context.Context) map[string][]byte {
	tenantID := TenantFromContext(ctx)
	if tenantID == "" {
		tenantID = "system"
	}
	store, ok := s.stores[tenantID]
	if !ok {
		store = make(map[string][]byte)
		s.stores[tenantID] = store
	}
	return store
}

// Put writes into the context tenant's namespace.
func (s *tenantRoutingKV) Put(ctx context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := make([]byte, len(value))
	copy(stored, value)
	s.namespaceFor(ctx)[key] = stored
	return nil
}

// PutIfAbsent is the guarded create the registry requires.
func (s *tenantRoutingKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ns := s.namespaceFor(ctx)
	if _, exists := ns[key]; exists {
		return false, nil
	}
	stored := make([]byte, len(value))
	copy(stored, value)
	ns[key] = stored
	return true, nil
}

// Get reads from the context tenant's namespace.
func (s *tenantRoutingKV) Get(ctx context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.namespaceFor(ctx)[key]
	if !ok {
		return nil, nil
	}
	out := make([]byte, len(value))
	copy(out, value)
	return out, nil
}

// Delete removes from the context tenant's namespace.
func (s *tenantRoutingKV) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.namespaceFor(ctx), key)
	return nil
}

// Exists reports presence in the context tenant's namespace.
func (s *tenantRoutingKV) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.namespaceFor(ctx)[key]
	return ok, nil
}

// Scan walks the context tenant's namespace.
func (s *tenantRoutingKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	for key, value := range s.snapshot(ctx, prefix) {
		if err := fn(key, value); err != nil {
			return err
		}
	}
	return nil
}

// List returns the keys under prefix in the context tenant's namespace.
func (s *tenantRoutingKV) List(ctx context.Context, prefix string) ([]string, error) {
	keys := make([]string, 0)
	for key := range s.snapshot(ctx, prefix) {
		keys = append(keys, key)
	}
	return keys, nil
}

// snapshot copies the matching entries so iteration never holds the lock.
func (s *tenantRoutingKV) snapshot(ctx context.Context, prefix string) map[string][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]byte)
	for key, value := range s.namespaceFor(ctx) {
		if len(key) < len(prefix) || key[:len(prefix)] != prefix {
			continue
		}
		copied := make([]byte, len(value))
		copy(copied, value)
		out[key] = copied
	}
	return out
}

// namespaces returns the tenant namespaces that hold at least one record.
func (s *tenantRoutingKV) namespaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.stores))
	for tenantID, records := range s.stores {
		if len(records) > 0 {
			out = append(out, tenantID)
		}
	}
	return out
}

// =============================================================================
// go-quicbaas#663 — the registration write must land in the record's tenant
// =============================================================================

// TestAgentRegistry_RegisterAgent_LandsInRecordTenant is the regression guard
// for the defect measured on the burn-in node: POST /api/v1/fleet/enroll is
// a public route, so it carries no identity and its context names no tenant,
// while every read of the fleet registry is made under the tenant the request
// names. The record was written into the system namespace and looked for in
// the tenant's, so `agent already exists` and `agent registry: not found`
// were true at the same time.
func TestAgentRegistry_RegisterAgent_LandsInRecordTenant(t *testing.T) {
	t.Parallel()

	kv := newTenantRoutingKV()
	registry := NewFleetRegistry(kv)

	// Registration exactly as the public register route makes it: a context
	// that names no tenant, and a record that names its own.
	require.NoError(t, registry.RegisterFleetNode(context.Background(), &FleetNode{
		ID:       "agent-automatethethings-agent-agent-04",
		TenantID: "automatethethings",
		Name:     "agent-automatethethings-agent-agent-04",
		Host:     "agent-04:9092",
		NodeType: "agent-runtime",
	}))

	// The record is readable under its own tenant, which is the only context
	// any reader of the fleet registry ever presents.
	tenantCtx := ContextWithTenant(context.Background(), "automatethethings")
	agent, err := registry.GetFleetNode(tenantCtx, "automatethethings", "agent-automatethethings-agent-agent-04")
	require.NoError(t, err, "the record a registration wrote must be readable under its own tenant")
	require.NotNil(t, agent)
	assert.Equal(t, "automatethethings", agent.TenantID)

	agents, err := registry.ListFleetNodes(tenantCtx, "automatethethings", &FleetNodeFilter{})
	require.NoError(t, err)
	require.Len(t, agents, 1, "the tenant's agent listing must contain the registered agent")

	// And it landed in that tenant's namespace ONLY: a system-tenant listing
	// must not see it, which is the second half of the live observation.
	systemCtx := ContextWithTenant(context.Background(), "system")
	systemAgents, err := registry.ListFleetNodes(systemCtx, "system", &FleetNodeFilter{})
	require.NoError(t, err)
	assert.Empty(t, systemAgents, "a tenant's agent must not appear in the system tenant")

	assert.Equal(t, []string{"automatethethings"}, kv.namespaces(),
		"the registration must write into exactly one namespace: the record's own tenant")
}

// TestAgentRegistry_Heartbeat_AfterPublicRouteRegistration proves the whole
// live sequence end to end: register through a context with no tenant, then
// heartbeat as the daemon does — through a context carrying the tenant from
// the request — and get a live record rather than a 404.
func TestAgentRegistry_Heartbeat_AfterPublicRouteRegistration(t *testing.T) {
	t.Parallel()

	registry := NewFleetRegistry(newTenantRoutingKV())
	require.NoError(t, registry.RegisterFleetNode(context.Background(), &FleetNode{
		ID:       "agent-acme-runner",
		TenantID: "acme",
		Name:     "agent-acme-runner",
		Host:     "runner:9092",
	}))

	tenantCtx := ContextWithTenant(context.Background(), "acme")
	require.NoError(t, registry.Heartbeat(tenantCtx, "acme", "agent-acme-runner", nil),
		"the heartbeat that follows a registration must find the record it wrote")

	agent, err := registry.GetFleetNode(tenantCtx, "acme", "agent-acme-runner")
	require.NoError(t, err)
	assert.Equal(t, FleetNodeStatusRunning, agent.Status)
}

// TestAgentRegistry_TenantScoping_NeverRedirectsAnAdmittedCaller proves the
// scoping only ever FILLS IN an absent tenant. A caller whose context already
// names a tenant keeps that namespace, so this helper can never redirect an
// admitted caller's write into a tenant they were not admitted to (the rule
// go-quicbaas#634 established for the project store).
func TestAgentRegistry_TenantScoping_NeverRedirectsAnAdmittedCaller(t *testing.T) {
	t.Parallel()

	kv := newTenantRoutingKV()
	registry := NewFleetRegistry(kv)

	// A caller admitted as tenant "acme" registers a record that names
	// "other". The write stays in the caller's namespace.
	callerCtx := ContextWithTenant(context.Background(), "acme")
	require.NoError(t, registry.RegisterFleetNode(callerCtx, &FleetNode{
		ID:       "agent-x",
		TenantID: "other",
		Name:     "agent-x",
	}))

	assert.Equal(t, []string{"acme"}, kv.namespaces(),
		"a context that already names a tenant must decide the namespace")
}

// TestAgentRegistry_TenantScoping_SystemTenantUnchanged proves the embedded
// agent's registration path is untouched: go-qrdb folds the empty tenant onto
// the system tenant (IdentityNamespace), so a record whose tenant is "system"
// occupies the same namespace an unscoped write always did.
func TestAgentRegistry_TenantScoping_SystemTenantUnchanged(t *testing.T) {
	t.Parallel()

	kv := newTenantRoutingKV()
	registry := NewFleetRegistry(kv)

	require.NoError(t, registry.RegisterFleetNode(context.Background(), &FleetNode{
		ID:       "embedded-fleet-node",
		TenantID: "system",
		Name:     "embedded-fleet-node",
	}))

	agents, err := registry.ListFleetNodes(context.Background(), "system", &FleetNodeFilter{})
	require.NoError(t, err)
	assert.Len(t, agents, 1)
	assert.Equal(t, []string{"system"}, kv.namespaces())
}

// TestAgentRegistry_DispatchSeesARegisteredAgent covers the read the task
// dispatcher makes: it lists a task's tenant from a loop context that names no
// tenant of its own (orchestrator.go dispatch). The agent a registration
// published must be visible there, or work is never routed to it.
func TestAgentRegistry_DispatchSeesARegisteredAgent(t *testing.T) {
	t.Parallel()

	registry := NewFleetRegistry(newTenantRoutingKV())
	require.NoError(t, registry.RegisterFleetNode(context.Background(), &FleetNode{
		ID:       "agent-automatethethings-agent-agent-04",
		TenantID: "automatethethings",
		Name:     "agent-automatethethings-agent-agent-04",
		Host:     "agent-04:9092",
		Backend:  "acp",
		NodeType: "agent-runtime",
	}))

	// The dispatcher's own call shape: a background context, the task's tenant
	// as the parameter.
	agents, err := registry.ListFleetNodes(context.Background(), "automatethethings", &FleetNodeFilter{})
	require.NoError(t, err)
	require.Len(t, agents, 1, "dispatch cannot route work to an agent it cannot list")
	assert.Equal(t, "acp", agents[0].Backend)
}
