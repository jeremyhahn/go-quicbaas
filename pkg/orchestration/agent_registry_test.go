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

package orchestration

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Test doubles
// =============================================================================

// agentAuditRecorder captures the store audit trail for assertions.
type agentAuditRecorder struct {
	mu     sync.Mutex
	events []StoreAuditEvent
}

// RecordStoreEvent implements StoreAuditor.
func (a *agentAuditRecorder) RecordStoreEvent(_ context.Context, event StoreAuditEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

// snapshot returns a copy of the recorded events.
func (a *agentAuditRecorder) snapshot() []StoreAuditEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]StoreAuditEvent, len(a.events))
	copy(out, a.events)
	return out
}

// agentKVFault names the KVStore verb that failingAgentKV injects an error on.
type agentKVFault string

const (
	agentFaultGet    agentKVFault = "get"
	agentFaultPut    agentKVFault = "put"
	agentFaultDelete agentKVFault = "delete"
	agentFaultExists agentKVFault = "exists"
	agentFaultScan   agentKVFault = "scan"
)

// errAgentKVDown is the injected backend failure.
var errAgentKVDown = errors.New("kv backend unavailable")

// failingAgentKV wraps a real store and fails exactly one verb, so a test can
// prove that the registry propagates backend failures rather than masking them.
type failingAgentKV struct {
	interfaces.KVStore
	fault agentKVFault
}

// newFailingAgentKV returns a memory-backed store whose fault verb always errors.
func newFailingAgentKV(fault agentKVFault) *failingAgentKV {
	return &failingAgentKV{KVStore: NewMemoryTaskKV(), fault: fault}
}

// PutIfAbsent forwards the guarded create, honouring the injected write
// failure. Embedding interfaces.KVStore does not carry this verb — see
// forwardPutIfAbsent — so without this method every registration is refused.
func (f *failingAgentKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if f.fault == agentFaultPut {
		return false, errAgentKVDown
	}
	return forwardPutIfAbsent(ctx, f.KVStore, key, value)
}

func (f *failingAgentKV) Get(ctx context.Context, key string) ([]byte, error) {
	if f.fault == agentFaultGet {
		return nil, errAgentKVDown
	}
	return f.KVStore.Get(ctx, key)
}

func (f *failingAgentKV) Put(ctx context.Context, key string, value []byte) error {
	if f.fault == agentFaultPut {
		return errAgentKVDown
	}
	return f.KVStore.Put(ctx, key, value)
}

func (f *failingAgentKV) Delete(ctx context.Context, key string) error {
	if f.fault == agentFaultDelete {
		return errAgentKVDown
	}
	return f.KVStore.Delete(ctx, key)
}

func (f *failingAgentKV) Exists(ctx context.Context, key string) (bool, error) {
	if f.fault == agentFaultExists {
		return false, errAgentKVDown
	}
	return f.KVStore.Exists(ctx, key)
}

func (f *failingAgentKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if f.fault == agentFaultScan {
		return errAgentKVDown
	}
	return f.KVStore.Scan(ctx, prefix, fn)
}

// seedAgent registers one agent and fails the test if registration fails.
func seedAgent(t *testing.T, registry AgentRegistry, tenantID, agentID string) *AgentRegistration {
	t.Helper()

	agent := &AgentRegistration{
		ID:        agentID,
		TenantID:  tenantID,
		Name:      agentID,
		Host:      "host-1",
		AgentType: "claude-code",
		Backend:   "acp",
	}
	require.NoError(t, registry.RegisterAgent(context.Background(), agent))
	return agent
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewInMemoryAgentRegistry(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	require.NotNil(t, registry, "expected non-nil registry")
}

func TestNewAgentRegistry_InjectedStore(t *testing.T) {
	t.Parallel()

	kv := NewMemoryTaskKV()
	registry := NewAgentRegistry(kv)
	require.NotNil(t, registry)

	ctx := context.Background()
	seedAgent(t, registry, "tenant-a", "agent-1")

	// The record must be addressable at the documented key layout, proving the
	// registry writes through to the injected store rather than to private state.
	raw, err := kv.Get(ctx, "orch/agent/tenant-a/agent-1")
	require.NoError(t, err)
	assert.NotEmpty(t, raw)
}

func TestNewAgentRegistry_NilStore(t *testing.T) {
	t.Parallel()

	registry := NewAgentRegistry(nil)
	ctx := context.Background()

	tests := map[string]func() error{
		"register": func() error {
			return registry.RegisterAgent(ctx, &AgentRegistration{ID: "a", TenantID: "t"})
		},
		"get": func() error {
			_, err := registry.GetAgent(ctx, "t", "a")
			return err
		},
		"list": func() error {
			_, err := registry.ListAgents(ctx, "t", nil)
			return err
		},
		"update": func() error {
			return registry.UpdateAgent(ctx, &AgentRegistration{ID: "a", TenantID: "t"})
		},
		"remove":       func() error { return registry.RemoveAgent(ctx, "t", "a") },
		"heartbeat":    func() error { return registry.Heartbeat(ctx, "t", "a", nil) },
		"deleteTenant": func() error { return registry.DeleteTenantAgents(ctx, "t") },
	}

	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := call()
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrAgentRegistryRequired),
				"expected ErrAgentRegistryRequired, got: %v", err)
		})
	}
}

// =============================================================================
// Durability Tests
// =============================================================================

// TestAgentRegistry_RestartDurability proves the registry keeps zero private
// state: a brand-new registry over the same store observes every record with
// identical field values.
func TestAgentRegistry_RestartDurability(t *testing.T) {
	t.Parallel()

	kv := NewMemoryTaskKV()
	first := NewAgentRegistry(kv)
	ctx := context.Background()

	original := &AgentRegistration{
		ID:           "agent-durable",
		TenantID:     "tenant-a",
		Name:         "durable",
		Host:         "host-9",
		Backend:      "acp",
		Model:        "sonnet",
		AgentType:    "claude-code",
		Kind:         AgentKindDaemon,
		Capabilities: []string{"code", "test"},
		WorkDir:      "/workspace",
		Metadata:     map[string]string{"env": "prod"},
	}
	require.NoError(t, first.RegisterAgent(ctx, original))
	seedAgent(t, first, "tenant-a", "agent-durable-2")

	// A second registry constructed over the same store is a stand-in for a
	// process restart.
	second := NewAgentRegistry(kv)

	got, err := second.GetAgent(ctx, "tenant-a", "agent-durable")
	require.NoError(t, err)
	assert.Equal(t, original, got, "restarted registry must observe identical field values")

	listed, err := second.ListAgents(ctx, "tenant-a", nil)
	require.NoError(t, err)
	assert.Len(t, listed, 2, "restarted registry must observe every record")

	// Mutations from the second registry are visible to the first.
	require.NoError(t, second.RemoveAgent(ctx, "tenant-a", "agent-durable-2"))
	remaining, err := first.ListAgents(ctx, "tenant-a", nil)
	require.NoError(t, err)
	assert.Len(t, remaining, 1)
}

// =============================================================================
// RegisterAgent Tests
// =============================================================================

func TestRegisterAgent_Success(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-1",
		TenantID:  "tenant-a",
		Name:      "test-agent",
		Host:      "host-1",
		AgentType: "claude-code",
		Backend:   "acp",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.NoError(t, err)

	assert.Equal(t, uint64(1), agent.Version, "version should be 1 after registration")
	assert.Equal(t, AgentStatusRegistered, agent.Status, "status should be registered")
	assert.NotZero(t, agent.RegisteredAt, "registered_at should be set")
	assert.NotZero(t, agent.LastSeen, "last_seen should be set")

	// Verify retrieval returns matching fields
	got, err := registry.GetAgent(ctx, "tenant-a", "agent-1")
	require.NoError(t, err)
	assert.Equal(t, "agent-1", got.ID)
	assert.Equal(t, "tenant-a", got.TenantID)
	assert.Equal(t, "test-agent", got.Name)
	assert.Equal(t, "host-1", got.Host)
	assert.Equal(t, "claude-code", got.AgentType)
	assert.Equal(t, "acp", got.Backend)
}

func TestRegisterAgent_PreservesExplicitStatus(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-idle",
		TenantID:  "tenant-a",
		Name:      "idler",
		Host:      "host-1",
		AgentType: "claude-code",
		Status:    AgentStatusIdle,
	}
	require.NoError(t, registry.RegisterAgent(ctx, agent))

	got, err := registry.GetAgent(ctx, "tenant-a", "agent-idle")
	require.NoError(t, err)
	assert.Equal(t, AgentStatusIdle, got.Status, "explicit status must not be overwritten")
}

func TestRegisterAgent_AlreadyExists(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-dup",
		TenantID:  "tenant-a",
		Name:      "first",
		Host:      "host-1",
		AgentType: "claude-code",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.NoError(t, err)

	// Attempt to register the same agent ID again
	duplicate := &AgentRegistration{
		ID:        "agent-dup",
		TenantID:  "tenant-a",
		Name:      "second",
		Host:      "host-2",
		AgentType: "codex",
	}

	err = registry.RegisterAgent(ctx, duplicate)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryAlreadyExists),
		"expected ErrAgentRegistryAlreadyExists, got: %v", err)

	// The original record must survive the rejected registration.
	got, err := registry.GetAgent(ctx, "tenant-a", "agent-dup")
	require.NoError(t, err)
	assert.Equal(t, "first", got.Name)
}

func TestRegisterAgent_MissingTenantID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-1",
		TenantID:  "",
		Name:      "test-agent",
		Host:      "host-1",
		AgentType: "claude-code",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput, got: %v", err)
}

func TestRegisterAgent_MissingID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "",
		TenantID:  "tenant-a",
		Name:      "test-agent",
		Host:      "host-1",
		AgentType: "claude-code",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput, got: %v", err)
}

func TestRegisterAgent_NilAgent(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.RegisterAgent(ctx, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput for nil agent, got: %v", err)
}

func TestRegisterAgent_StoreFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// No "exists" case, and its absence is the point: the guarded create issues
	// ONE operation, so there is no probe left to fail independently of the
	// write. The put fault covers the whole registration path.
	tests := map[string]agentKVFault{
		"put": agentFaultPut,
	}

	for name, fault := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry := NewAgentRegistry(newFailingAgentKV(fault))
			err := registry.RegisterAgent(ctx, &AgentRegistration{
				ID:        "agent-fail",
				TenantID:  "tenant-a",
				Name:      "failer",
				Host:      "host-1",
				AgentType: "claude-code",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, errAgentKVDown)
		})
	}
}

// =============================================================================
// GetAgent Tests
// =============================================================================

func TestGetAgent_Success(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:           "agent-get",
		TenantID:     "tenant-a",
		Name:         "getter",
		Host:         "host-1",
		AgentType:    "claude-code",
		Backend:      "acp",
		Capabilities: []string{"code", "test"},
		WorkDir:      "/workspace",
		Metadata:     map[string]string{"env": "dev"},
	}

	err := registry.RegisterAgent(ctx, agent)
	require.NoError(t, err)

	got, err := registry.GetAgent(ctx, "tenant-a", "agent-get")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "agent-get", got.ID)
	assert.Equal(t, "tenant-a", got.TenantID)
	assert.Equal(t, "getter", got.Name)
	assert.Equal(t, "host-1", got.Host)
	assert.Equal(t, "claude-code", got.AgentType)
	assert.Equal(t, "acp", got.Backend)
	assert.Equal(t, []string{"code", "test"}, got.Capabilities)
	assert.Equal(t, "/workspace", got.WorkDir)
	assert.Equal(t, "dev", got.Metadata["env"])
	assert.Equal(t, uint64(1), got.Version)
	assert.NotZero(t, got.RegisteredAt)
	assert.NotZero(t, got.LastSeen)
}

func TestGetAgent_NotFound(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	got, err := registry.GetAgent(ctx, "tenant-a", "nonexistent")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, ErrAgentRegistryNotFound),
		"expected ErrAgentRegistryNotFound, got: %v", err)
}

func TestGetAgent_EmptyTenantID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	got, err := registry.GetAgent(ctx, "", "agent-1")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput for empty tenant, got: %v", err)
}

func TestGetAgent_EmptyAgentID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	got, err := registry.GetAgent(ctx, "tenant-a", "")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput for empty agent ID, got: %v", err)
}

func TestGetAgent_CorruptRecord(t *testing.T) {
	t.Parallel()

	kv := NewMemoryTaskKV()
	registry := NewAgentRegistry(kv)
	ctx := context.Background()

	require.NoError(t, kv.Put(ctx, "orch/agent/tenant-a/broken", []byte("{not json")))

	got, err := registry.GetAgent(ctx, "tenant-a", "broken")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, ErrAgentRegistryMarshal),
		"expected ErrAgentRegistryMarshal, got: %v", err)
}

func TestGetAgent_StoreFailure(t *testing.T) {
	t.Parallel()

	registry := NewAgentRegistry(newFailingAgentKV(agentFaultGet))

	got, err := registry.GetAgent(context.Background(), "tenant-a", "agent-1")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, errAgentKVDown)
}

// =============================================================================
// ListAgents Tests
// =============================================================================

func TestListAgents_TenantIsolation(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	// Register agents in tenant-A
	for i, id := range []string{"a1", "a2", "a3"} {
		err := registry.RegisterAgent(ctx, &AgentRegistration{
			ID:        id,
			TenantID:  "tenant-a",
			Name:      "agent-a-" + id,
			Host:      "host-1",
			AgentType: "claude-code",
			Backend:   "acp",
			Metadata:  map[string]string{"idx": uitoa(uint64(i))},
		})
		require.NoError(t, err)
	}

	// Register agents in tenant-B
	for i, id := range []string{"b1", "b2"} {
		err := registry.RegisterAgent(ctx, &AgentRegistration{
			ID:        id,
			TenantID:  "tenant-b",
			Name:      "agent-b-" + id,
			Host:      "host-2",
			AgentType: "codex",
			Backend:   "acp",
			Metadata:  map[string]string{"idx": uitoa(uint64(i))},
		})
		require.NoError(t, err)
	}

	// List tenant-A agents
	agentsA, err := registry.ListAgents(ctx, "tenant-a", nil)
	require.NoError(t, err)
	assert.Len(t, agentsA, 3, "tenant-a should have 3 agents")

	for _, a := range agentsA {
		assert.Equal(t, "tenant-a", a.TenantID, "all listed agents should belong to tenant-a")
	}

	// List tenant-B agents
	agentsB, err := registry.ListAgents(ctx, "tenant-b", nil)
	require.NoError(t, err)
	assert.Len(t, agentsB, 2, "tenant-b should have 2 agents")

	for _, b := range agentsB {
		assert.Equal(t, "tenant-b", b.TenantID, "all listed agents should belong to tenant-b")
	}
}

func TestListAgents_Filters(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	fleet := []*AgentRegistration{
		{ID: "f1", TenantID: "tenant-f", Host: "host-1", AgentType: "claude-code", Backend: "acp", Model: "sonnet", Status: AgentStatusIdle},
		{ID: "f2", TenantID: "tenant-f", Host: "host-1", AgentType: "codex", Backend: "ollama", Model: "sonnet", Status: AgentStatusRunning},
		{ID: "f3", TenantID: "tenant-f", Host: "host-2", AgentType: "claude-code", Backend: "acp", Model: "opus", Status: AgentStatusOffline},
	}
	for _, agent := range fleet {
		require.NoError(t, registry.RegisterAgent(ctx, agent))
	}

	tests := map[string]struct {
		filter  *AgentFilter
		wantIDs []string
	}{
		"nil filter":       {nil, []string{"f1", "f2", "f3"}},
		"empty filter":     {&AgentFilter{}, []string{"f1", "f2", "f3"}},
		"host":             {&AgentFilter{Host: "host-1"}, []string{"f1", "f2"}},
		"status":           {&AgentFilter{Status: AgentStatusRunning}, []string{"f2"}},
		"agent type":       {&AgentFilter{AgentType: "claude-code"}, []string{"f1", "f3"}},
		"backend":          {&AgentFilter{Backend: "acp"}, []string{"f1", "f3"}},
		"model":            {&AgentFilter{Model: "opus"}, []string{"f3"}},
		"combined":         {&AgentFilter{Host: "host-1", Backend: "acp"}, []string{"f1"}},
		"limit":            {&AgentFilter{Limit: 2}, []string{"f1", "f2"}},
		"limit over size":  {&AgentFilter{Limit: 10}, []string{"f1", "f2", "f3"}},
		"limit with match": {&AgentFilter{AgentType: "claude-code", Limit: 1}, []string{"f1"}},
		"no match":         {&AgentFilter{Host: "host-404"}, []string{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := registry.ListAgents(ctx, "tenant-f", tc.filter)
			require.NoError(t, err)

			ids := make([]string, 0, len(got))
			for _, agent := range got {
				ids = append(ids, agent.ID)
			}
			assert.Equal(t, tc.wantIDs, ids)
		})
	}
}

func TestListAgents_Empty(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agents, err := registry.ListAgents(ctx, "tenant-empty", nil)
	require.NoError(t, err)
	require.NotNil(t, agents, "should return non-nil empty slice")
	assert.Len(t, agents, 0, "should return zero agents")
}

func TestListAgents_EmptyTenantID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agents, err := registry.ListAgents(ctx, "", nil)
	require.Error(t, err)
	assert.Nil(t, agents)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput, got: %v", err)
}

// TestListAgents_SkipsCorruptRecords proves one unreadable record cannot hide
// the rest of the fleet from an operator.
func TestListAgents_SkipsCorruptRecords(t *testing.T) {
	t.Parallel()

	kv := NewMemoryTaskKV()
	registry := NewAgentRegistry(kv)
	ctx := context.Background()

	seedAgent(t, registry, "tenant-a", "good-1")
	seedAgent(t, registry, "tenant-a", "good-2")
	require.NoError(t, kv.Put(ctx, "orch/agent/tenant-a/corrupt", []byte("}{")))

	agents, err := registry.ListAgents(ctx, "tenant-a", nil)
	require.NoError(t, err)
	require.Len(t, agents, 2)
	assert.Equal(t, "good-1", agents[0].ID)
	assert.Equal(t, "good-2", agents[1].ID)
}

func TestListAgents_StoreFailure(t *testing.T) {
	t.Parallel()

	registry := NewAgentRegistry(newFailingAgentKV(agentFaultScan))

	agents, err := registry.ListAgents(context.Background(), "tenant-a", nil)
	require.Error(t, err)
	assert.Nil(t, agents)
	assert.ErrorIs(t, err, errAgentKVDown)
}

// =============================================================================
// UpdateAgent Tests
// =============================================================================

func TestUpdateAgent_Success(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-upd",
		TenantID:  "tenant-a",
		Name:      "original-name",
		Host:      "host-1",
		AgentType: "claude-code",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), agent.Version)

	// Retrieve to get a clean copy for update
	fetched, err := registry.GetAgent(ctx, "tenant-a", "agent-upd")
	require.NoError(t, err)

	fetched.Name = "updated-name"
	err = registry.UpdateAgent(ctx, fetched)
	require.NoError(t, err)

	// Verify the update persisted
	got, err := registry.GetAgent(ctx, "tenant-a", "agent-upd")
	require.NoError(t, err)
	assert.Equal(t, "updated-name", got.Name)
	assert.Equal(t, uint64(2), got.Version, "version should be incremented to 2")
	assert.NotZero(t, got.LastSeen)
}

func TestUpdateAgent_VersionConflict(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-conflict",
		TenantID:  "tenant-a",
		Name:      "original",
		Host:      "host-1",
		AgentType: "claude-code",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.NoError(t, err)

	// Simulate a stale read by using a wrong version
	stale := &AgentRegistration{
		ID:        "agent-conflict",
		TenantID:  "tenant-a",
		Name:      "stale-update",
		Host:      "host-1",
		AgentType: "claude-code",
		Version:   999, // stale version
	}

	err = registry.UpdateAgent(ctx, stale)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput for version conflict, got: %v", err)

	// The stored record must be untouched by the rejected update.
	got, err := registry.GetAgent(ctx, "tenant-a", "agent-conflict")
	require.NoError(t, err)
	assert.Equal(t, "original", got.Name)
	assert.Equal(t, uint64(1), got.Version)
}

func TestUpdateAgent_NotFound(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "nonexistent",
		TenantID:  "tenant-a",
		Name:      "ghost",
		Host:      "host-1",
		AgentType: "claude-code",
		Version:   1,
	}

	err := registry.UpdateAgent(ctx, agent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryNotFound),
		"expected ErrAgentRegistryNotFound, got: %v", err)
}

func TestUpdateAgent_NilAgent(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.UpdateAgent(ctx, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput for nil agent, got: %v", err)
}

func TestUpdateAgent_StoreFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := newFailingAgentKV(agentFaultPut)

	// Seed through a healthy registry over the same backing store.
	healthy := NewAgentRegistry(kv.KVStore)
	agent := seedAgent(t, healthy, "tenant-a", "agent-put")

	registry := NewAgentRegistry(kv)
	err := registry.UpdateAgent(ctx, agent)
	require.Error(t, err)
	assert.ErrorIs(t, err, errAgentKVDown)
}

// =============================================================================
// RemoveAgent Tests
// =============================================================================

func TestRemoveAgent_Success(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-rm",
		TenantID:  "tenant-a",
		Name:      "removable",
		Host:      "host-1",
		AgentType: "claude-code",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.NoError(t, err)

	// Verify agent exists
	got, err := registry.GetAgent(ctx, "tenant-a", "agent-rm")
	require.NoError(t, err)
	assert.Equal(t, "agent-rm", got.ID)

	// Remove and verify not found
	err = registry.RemoveAgent(ctx, "tenant-a", "agent-rm")
	require.NoError(t, err)

	got, err = registry.GetAgent(ctx, "tenant-a", "agent-rm")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, ErrAgentRegistryNotFound),
		"expected ErrAgentRegistryNotFound after removal, got: %v", err)
}

func TestRemoveAgent_NotFound(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.RemoveAgent(ctx, "tenant-a", "nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryNotFound),
		"expected ErrAgentRegistryNotFound, got: %v", err)
}

func TestRemoveAgent_StoreFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := newFailingAgentKV(agentFaultDelete)

	healthy := NewAgentRegistry(kv.KVStore)
	seedAgent(t, healthy, "tenant-a", "agent-del")

	registry := NewAgentRegistry(kv)
	err := registry.RemoveAgent(ctx, "tenant-a", "agent-del")
	require.Error(t, err)
	assert.ErrorIs(t, err, errAgentKVDown)
}

// =============================================================================
// Heartbeat Tests
// =============================================================================

func TestHeartbeat_Success(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-hb",
		TenantID:  "tenant-a",
		Name:      "heartbeater",
		Host:      "host-1",
		AgentType: "claude-code",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.NoError(t, err)

	// Capture the initial last_seen
	before, err := registry.GetAgent(ctx, "tenant-a", "agent-hb")
	require.NoError(t, err)
	assert.Equal(t, AgentStatusRegistered, before.Status)
	initialLastSeen := before.LastSeen

	// Heartbeat
	err = registry.Heartbeat(ctx, "tenant-a", "agent-hb", nil)
	require.NoError(t, err)

	// Verify status changed to running and last_seen updated
	after, err := registry.GetAgent(ctx, "tenant-a", "agent-hb")
	require.NoError(t, err)
	assert.Equal(t, AgentStatusRunning, after.Status,
		"heartbeat should transition registered agent to running")
	assert.GreaterOrEqual(t, after.LastSeen, initialLastSeen,
		"last_seen should be updated after heartbeat")
	assert.Equal(t, uint64(2), after.Version,
		"version should be incremented after heartbeat")
}

func TestHeartbeat_StatusTransitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := map[string]struct {
		start AgentStatus
		want  AgentStatus
	}{
		"registered becomes running": {AgentStatusRegistered, AgentStatusRunning},
		"idle becomes running":       {AgentStatusIdle, AgentStatusRunning},
		"running stays running":      {AgentStatusRunning, AgentStatusRunning},
		"offline stays offline":      {AgentStatusOffline, AgentStatusOffline},
		"error stays error":          {AgentStatusError, AgentStatusError},
		"disabled stays disabled":    {AgentStatusDisabled, AgentStatusDisabled},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry := NewInMemoryAgentRegistry()
			require.NoError(t, registry.RegisterAgent(ctx, &AgentRegistration{
				ID:        "hb",
				TenantID:  "tenant-a",
				Name:      "hb",
				Host:      "host-1",
				AgentType: "claude-code",
				Status:    tc.start,
			}))

			require.NoError(t, registry.Heartbeat(ctx, "tenant-a", "hb", nil))

			got, err := registry.GetAgent(ctx, "tenant-a", "hb")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Status)
		})
	}
}

func TestHeartbeat_NotFound(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.Heartbeat(ctx, "tenant-a", "nonexistent", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryNotFound),
		"expected ErrAgentRegistryNotFound, got: %v", err)
}

func TestHeartbeat_EmptyTenantID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.Heartbeat(ctx, "", "agent-1", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput, got: %v", err)
}

func TestHeartbeat_EmptyAgentID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.Heartbeat(ctx, "tenant-a", "", nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput, got: %v", err)
}

func TestHeartbeat_StoreFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	kv := newFailingAgentKV(agentFaultPut)

	healthy := NewAgentRegistry(kv.KVStore)
	seedAgent(t, healthy, "tenant-a", "agent-hb")

	registry := NewAgentRegistry(kv)
	err := registry.Heartbeat(ctx, "tenant-a", "agent-hb", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, errAgentKVDown)
}

// =============================================================================
// DeleteTenantAgents Tests
// =============================================================================

func TestDeleteTenantAgents_Success(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	// Register 3 agents in tenant-A
	for i := range 3 {
		err := registry.RegisterAgent(ctx, &AgentRegistration{
			ID:        "a-" + uitoa(uint64(i)),
			TenantID:  "tenant-a",
			Name:      "agent-a",
			Host:      "host-1",
			AgentType: "claude-code",
		})
		require.NoError(t, err)
	}

	// Register 2 agents in tenant-B
	for i := range 2 {
		err := registry.RegisterAgent(ctx, &AgentRegistration{
			ID:        "b-" + uitoa(uint64(i)),
			TenantID:  "tenant-b",
			Name:      "agent-b",
			Host:      "host-2",
			AgentType: "codex",
		})
		require.NoError(t, err)
	}

	// Delete all tenant-A agents
	err := registry.DeleteTenantAgents(ctx, "tenant-a")
	require.NoError(t, err)

	// Verify tenant-A has 0 agents
	agentsA, err := registry.ListAgents(ctx, "tenant-a", nil)
	require.NoError(t, err)
	assert.Len(t, agentsA, 0, "tenant-a should have 0 agents after delete")

	// Verify tenant-B still has 2 agents
	agentsB, err := registry.ListAgents(ctx, "tenant-b", nil)
	require.NoError(t, err)
	assert.Len(t, agentsB, 2, "tenant-b should still have 2 agents")
}

func TestDeleteTenantAgents_EmptyTenantID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.DeleteTenantAgents(ctx, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput, got: %v", err)
}

func TestDeleteTenantAgents_NoAgents(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	// Deleting agents for a tenant with no agents should succeed without error
	err := registry.DeleteTenantAgents(ctx, "tenant-empty")
	require.NoError(t, err)
}

func TestDeleteTenantAgents_StoreFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("scan", func(t *testing.T) {
		t.Parallel()

		registry := NewAgentRegistry(newFailingAgentKV(agentFaultScan))
		err := registry.DeleteTenantAgents(ctx, "tenant-a")
		require.Error(t, err)
		assert.ErrorIs(t, err, errAgentKVDown)
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()

		kv := newFailingAgentKV(agentFaultDelete)
		healthy := NewAgentRegistry(kv.KVStore)
		seedAgent(t, healthy, "tenant-a", "agent-1")

		registry := NewAgentRegistry(kv)
		err := registry.DeleteTenantAgents(ctx, "tenant-a")
		require.Error(t, err)
		assert.ErrorIs(t, err, errAgentKVDown)
	})
}

// =============================================================================
// Tenant Isolation Tests (store layer)
// =============================================================================

// TestAgentRegistry_TenantIsolation asserts isolation at the store layer: with
// two tenants' records in one KV store, every verb scoped to tenant A must be
// unable to observe or mutate a record owned by tenant B. The lexically
// overlapping tenant pair (t1 / t10) additionally proves prefix scans do not
// bleed across a tenant boundary.
func TestAgentRegistry_TenantIsolation(t *testing.T) {
	t.Parallel()

	tenantPairs := map[string]struct {
		tenantA string
		tenantB string
	}{
		"distinct tenants":       {"tenant-a", "tenant-b"},
		"lexical prefix tenants": {"t1", "t10"},
	}

	for name, pair := range tenantPairs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			kv := NewMemoryTaskKV()
			registry := NewAgentRegistry(kv)
			ctx := context.Background()

			seedAgent(t, registry, pair.tenantA, "agent-a")
			agentB := seedAgent(t, registry, pair.tenantB, "agent-b")

			t.Run("get across tenants", func(t *testing.T) {
				got, err := registry.GetAgent(ctx, pair.tenantA, "agent-b")
				require.Error(t, err)
				assert.Nil(t, got)
				assert.ErrorIs(t, err, ErrAgentRegistryNotFound)
			})

			t.Run("update across tenants", func(t *testing.T) {
				hijack := &AgentRegistration{
					ID:       "agent-b",
					TenantID: pair.tenantA,
					Name:     "hijacked",
					Version:  agentB.Version,
				}
				err := registry.UpdateAgent(ctx, hijack)
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrAgentRegistryNotFound)
			})

			t.Run("remove across tenants", func(t *testing.T) {
				err := registry.RemoveAgent(ctx, pair.tenantA, "agent-b")
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrAgentRegistryNotFound)
			})

			t.Run("heartbeat across tenants", func(t *testing.T) {
				err := registry.Heartbeat(ctx, pair.tenantA, "agent-b", nil)
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrAgentRegistryNotFound)
			})

			t.Run("list is tenant scoped", func(t *testing.T) {
				listed, err := registry.ListAgents(ctx, pair.tenantA, nil)
				require.NoError(t, err)
				require.Len(t, listed, 1)
				assert.Equal(t, "agent-a", listed[0].ID)
			})

			// Tenant B's record must be byte-for-byte intact after every
			// cross-tenant attempt above.
			survivor, err := registry.GetAgent(ctx, pair.tenantB, "agent-b")
			require.NoError(t, err)
			assert.Equal(t, agentB, survivor)

			t.Run("cascade delete is tenant scoped", func(t *testing.T) {
				require.NoError(t, registry.DeleteTenantAgents(ctx, pair.tenantA))

				remaining, err := registry.ListAgents(ctx, pair.tenantB, nil)
				require.NoError(t, err)
				require.Len(t, remaining, 1, "tenant B records must survive tenant A cascade delete")
				assert.Equal(t, agentB, remaining[0])
			})
		})
	}
}

// TestAgentRegistry_ForeignRecordAtTenantKey covers the defence-in-depth check:
// a record whose TenantID disagrees with the key it sits under is never handed
// back to the caller.
func TestAgentRegistry_ForeignRecordAtTenantKey(t *testing.T) {
	t.Parallel()

	kv := NewMemoryTaskKV()
	registry := NewAgentRegistry(kv)
	ctx := context.Background()

	// Write a tenant-b record under tenant-a's subtree, simulating key
	// corruption or an out-of-band writer.
	require.NoError(t, kv.Put(ctx, "orch/agent/tenant-a/foreign",
		[]byte(`{"id":"foreign","tenant_id":"tenant-b","status":"running","last_seen":1,"registered_at":1,"version":1}`)))

	got, err := registry.GetAgent(ctx, "tenant-a", "foreign")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, ErrAgentRegistryNotFound)

	listed, err := registry.ListAgents(ctx, "tenant-a", nil)
	require.NoError(t, err)
	assert.Empty(t, listed, "a foreign record must never appear in a tenant listing")

	assert.ErrorIs(t, registry.Heartbeat(ctx, "tenant-a", "foreign", nil), ErrAgentRegistryNotFound)
	assert.ErrorIs(t, registry.RemoveAgent(ctx, "tenant-a", "foreign"), ErrAgentRegistryNotFound)
}

// =============================================================================
// Key Segment Validation Tests
// =============================================================================

// TestAgentRegistry_RejectsInvalidKeySegments proves an identifier that is
// empty or carries the key separator cannot escape its tenant subtree.
func TestAgentRegistry_RejectsInvalidKeySegments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := map[string]struct {
		tenantID string
		agentID  string
	}{
		"empty tenant":          {"", "agent-1"},
		"empty agent":           {"tenant-a", ""},
		"separator in tenant":   {"tenant-a/evil", "agent-1"},
		"separator in agent":    {"tenant-a", "../tenant-b/agent-1"},
		"separator in both":     {"a/b", "c/d"},
		"agent id escapes only": {"tenant-a", "nested/id"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			registry := NewInMemoryAgentRegistry()

			calls := map[string]func() error{
				"register": func() error {
					return registry.RegisterAgent(ctx, &AgentRegistration{ID: tc.agentID, TenantID: tc.tenantID})
				},
				"get": func() error {
					_, err := registry.GetAgent(ctx, tc.tenantID, tc.agentID)
					return err
				},
				"update": func() error {
					return registry.UpdateAgent(ctx, &AgentRegistration{ID: tc.agentID, TenantID: tc.tenantID})
				},
				"remove":    func() error { return registry.RemoveAgent(ctx, tc.tenantID, tc.agentID) },
				"heartbeat": func() error { return registry.Heartbeat(ctx, tc.tenantID, tc.agentID, nil) },
			}

			for verb, call := range calls {
				err := call()
				require.Error(t, err, "%s must reject the identifier", verb)
				assert.ErrorIs(t, err, ErrInvalidKeySegment, "%s: %v", verb, err)
				assert.ErrorIs(t, err, ErrAgentRegistryInvalidInput, "%s: %v", verb, err)
			}
		})
	}
}

func TestAgentRegistry_ListRejectsInvalidTenant(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	for _, tenantID := range []string{"", "tenant-a/nested"} {
		agents, err := registry.ListAgents(ctx, tenantID, nil)
		require.Error(t, err)
		assert.Nil(t, agents)
		assert.ErrorIs(t, err, ErrInvalidKeySegment)
		assert.ErrorIs(t, err, ErrAgentRegistryInvalidInput)

		err = registry.DeleteTenantAgents(ctx, tenantID)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidKeySegment)
		assert.ErrorIs(t, err, ErrAgentRegistryInvalidInput)
	}
}

// =============================================================================
// Audit Tests
// =============================================================================

func TestAgentRegistry_AuditsMutations(t *testing.T) {
	t.Parallel()

	auditor := &agentAuditRecorder{}
	registry := NewAgentRegistry(NewMemoryTaskKV(), WithStoreAuditor(auditor))
	ctx := context.Background()

	agent := &AgentRegistration{
		ID:        "agent-audit",
		TenantID:  "tenant-a",
		Name:      "audited",
		Host:      "host-1",
		AgentType: "claude-code",
	}
	require.NoError(t, registry.RegisterAgent(ctx, agent))
	require.NoError(t, registry.UpdateAgent(ctx, agent))
	require.NoError(t, registry.RemoveAgent(ctx, "tenant-a", "agent-audit"))

	events := auditor.snapshot()
	require.Len(t, events, 3, "expected exactly one event per mutation")

	wantActions := []string{AuditActionCreate, AuditActionUpdate, AuditActionDelete}
	for i, event := range events {
		assert.Equal(t, wantActions[i], event.Action)
		assert.Equal(t, AuditResourceAgent, event.Resource)
		assert.Equal(t, "agent-audit", event.ResourceID)
		assert.Equal(t, "tenant-a", event.TenantID)
		assert.True(t, event.Success)
	}
}

func TestAgentRegistry_DoesNotAuditReadsOrHeartbeats(t *testing.T) {
	t.Parallel()

	auditor := &agentAuditRecorder{}
	registry := NewAgentRegistry(NewMemoryTaskKV(), WithStoreAuditor(auditor))
	ctx := context.Background()

	seedAgent(t, registry, "tenant-a", "agent-quiet")
	require.Len(t, auditor.snapshot(), 1, "registration is the only audited call so far")

	_, err := registry.GetAgent(ctx, "tenant-a", "agent-quiet")
	require.NoError(t, err)
	_, err = registry.ListAgents(ctx, "tenant-a", nil)
	require.NoError(t, err)
	require.NoError(t, registry.Heartbeat(ctx, "tenant-a", "agent-quiet", nil))

	assert.Len(t, auditor.snapshot(), 1,
		"reads and heartbeats must not append to the audit trail")
}

func TestAgentRegistry_AuditsCascadeDeletePerAgent(t *testing.T) {
	t.Parallel()

	auditor := &agentAuditRecorder{}
	registry := NewAgentRegistry(NewMemoryTaskKV(), WithStoreAuditor(auditor))
	ctx := context.Background()

	seedAgent(t, registry, "tenant-a", "cascade-1")
	seedAgent(t, registry, "tenant-a", "cascade-2")
	seedAgent(t, registry, "tenant-b", "other-1")

	require.NoError(t, registry.DeleteTenantAgents(ctx, "tenant-a"))

	deletes := make([]StoreAuditEvent, 0, 2)
	for _, event := range auditor.snapshot() {
		if event.Action == AuditActionDelete {
			deletes = append(deletes, event)
		}
	}

	require.Len(t, deletes, 2, "cascade delete emits one event per removed agent")
	assert.Equal(t, "cascade-1", deletes[0].ResourceID)
	assert.Equal(t, "cascade-2", deletes[1].ResourceID)
	for _, event := range deletes {
		assert.Equal(t, "tenant-a", event.TenantID)
		assert.Equal(t, AuditResourceAgent, event.Resource)
		assert.True(t, event.Success)
	}
}

func TestAgentRegistry_AuditsFailedMutations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := map[string]struct {
		mutate     func(AgentRegistry) error
		wantAction string
		wantID     string
	}{
		"duplicate registration": {
			mutate: func(r AgentRegistry) error {
				return r.RegisterAgent(ctx, &AgentRegistration{
					ID: "dup", TenantID: "tenant-a", Name: "second", Host: "h", AgentType: "t",
				})
			},
			wantAction: AuditActionCreate,
			wantID:     "dup",
		},
		"version conflict": {
			mutate: func(r AgentRegistry) error {
				return r.UpdateAgent(ctx, &AgentRegistration{ID: "dup", TenantID: "tenant-a", Version: 42})
			},
			wantAction: AuditActionUpdate,
			wantID:     "dup",
		},
		"remove missing": {
			mutate:     func(r AgentRegistry) error { return r.RemoveAgent(ctx, "tenant-a", "ghost") },
			wantAction: AuditActionDelete,
			wantID:     "ghost",
		},
		"invalid key segment": {
			mutate:     func(r AgentRegistry) error { return r.RemoveAgent(ctx, "tenant-a", "bad/id") },
			wantAction: AuditActionDelete,
			wantID:     "bad/id",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			auditor := &agentAuditRecorder{}
			registry := NewAgentRegistry(NewMemoryTaskKV(), WithStoreAuditor(auditor))
			seedAgent(t, registry, "tenant-a", "dup")

			require.Error(t, tc.mutate(registry))

			events := auditor.snapshot()
			require.Len(t, events, 2, "the seed create plus one failure event")

			failure := events[1]
			assert.Equal(t, tc.wantAction, failure.Action)
			assert.Equal(t, AuditResourceAgent, failure.Resource)
			assert.Equal(t, tc.wantID, failure.ResourceID)
			assert.Equal(t, "tenant-a", failure.TenantID)
			assert.False(t, failure.Success)
		})
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestConcurrentRegistration(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	const numAgents = 100
	var wg sync.WaitGroup
	errs := make(chan error, numAgents)

	wg.Add(numAgents)
	for i := range numAgents {
		go func(idx int) {
			defer wg.Done()
			agent := &AgentRegistration{
				ID:        "agent-" + uitoa(uint64(idx)),
				TenantID:  "tenant-concurrent",
				Name:      "concurrent-agent",
				Host:      "host-1",
				AgentType: "claude-code",
			}
			if err := registry.RegisterAgent(ctx, agent); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected registration error: %v", err)
	}

	// Verify all agents were registered
	agents, err := registry.ListAgents(ctx, "tenant-concurrent", nil)
	require.NoError(t, err)
	assert.Len(t, agents, numAgents, "should have registered all %d agents", numAgents)
}

func TestConcurrentRegistration_DuplicateID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	const numGoroutines = 50
	var wg sync.WaitGroup
	successCount := make(chan struct{}, numGoroutines)
	failCount := make(chan struct{}, numGoroutines)

	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			agent := &AgentRegistration{
				ID:        "same-id",
				TenantID:  "tenant-race",
				Name:      "racer",
				Host:      "host-1",
				AgentType: "claude-code",
			}
			err := registry.RegisterAgent(ctx, agent)
			if err == nil {
				successCount <- struct{}{}
			} else {
				failCount <- struct{}{}
			}
		}()
	}

	wg.Wait()
	close(successCount)
	close(failCount)

	successes := 0
	for range successCount {
		successes++
	}

	failures := 0
	for range failCount {
		failures++
	}

	assert.Equal(t, 1, successes, "exactly one registration should succeed")
	assert.Equal(t, numGoroutines-1, failures,
		"all other registrations should fail with already-exists")
}

// TestConcurrentHeartbeat proves per-key serialization: every heartbeat is
// applied, so the version counter advances exactly once per call.
func TestConcurrentHeartbeat(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()
	seedAgent(t, registry, "tenant-a", "agent-hot")

	const beats = 50
	var wg sync.WaitGroup
	errs := make(chan error, beats)

	wg.Add(beats)
	for range beats {
		go func() {
			defer wg.Done()
			if err := registry.Heartbeat(ctx, "tenant-a", "agent-hot", nil); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected heartbeat error: %v", err)
	}

	got, err := registry.GetAgent(ctx, "tenant-a", "agent-hot")
	require.NoError(t, err)
	assert.Equal(t, uint64(1+beats), got.Version,
		"no heartbeat may be lost to an interleaved read-modify-write")
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

func TestRegisterAgent_CancelledContext(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	agent := &AgentRegistration{
		ID:        "agent-ctx",
		TenantID:  "tenant-a",
		Name:      "ctx-agent",
		Host:      "host-1",
		AgentType: "claude-code",
	}

	err := registry.RegisterAgent(ctx, agent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"expected context.Canceled, got: %v", err)
}

func TestGetAgent_CancelledContext(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := registry.GetAgent(ctx, "tenant-a", "agent-1")
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, context.Canceled),
		"expected context.Canceled, got: %v", err)
}

func TestAgentRegistry_CancelledContext(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := map[string]func() error{
		"list": func() error {
			_, err := registry.ListAgents(ctx, "tenant-a", nil)
			return err
		},
		"update": func() error {
			return registry.UpdateAgent(ctx, &AgentRegistration{ID: "a", TenantID: "tenant-a"})
		},
		"remove":       func() error { return registry.RemoveAgent(ctx, "tenant-a", "a") },
		"heartbeat":    func() error { return registry.Heartbeat(ctx, "tenant-a", "a", nil) },
		"deleteTenant": func() error { return registry.DeleteTenantAgents(ctx, "tenant-a") },
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := call()
			require.Error(t, err)
			assert.ErrorIs(t, err, context.Canceled)
		})
	}
}

// =============================================================================
// Multi-node registration exclusivity
// =============================================================================

// registerBarrierKV gathers every contender at the point it is about to write
// the agent record, releasing them only once all have arrived.
//
// Same dual-route rule as the create-race harness, and for the same reason: the
// broken registry arrives via Exists and the fixed one via PutIfAbsent, so
// gathering at whichever verb a node uses is what lets one test judge both. A
// harness hooked only to Exists would hold nothing once the probe was gone and
// would pass vacuously against the fix.
type registerBarrierKV struct {
	interfaces.KVStore

	key   string
	nodes int

	mu      sync.Mutex
	arrived int
	gate    chan struct{}
}

func newRegisterBarrierKV(inner interfaces.KVStore, key string, nodes int) *registerBarrierKV {
	return &registerBarrierKV{KVStore: inner, key: key, nodes: nodes, gate: make(chan struct{})}
}

func (k *registerBarrierKV) arrive() {
	k.mu.Lock()
	k.arrived++
	// Equality rather than >=, so a later arrival cannot close a closed gate.
	if k.arrived == k.nodes {
		close(k.gate)
	}
	k.mu.Unlock()

	select {
	case <-k.gate:
	case <-time.After(barrierWait):
	}
}

// Exists serves the probe and then gathers — the broken registry's route.
func (k *registerBarrierKV) Exists(ctx context.Context, key string) (bool, error) {
	exists, err := k.KVStore.Exists(ctx, key)
	if key == k.key {
		k.arrive()
	}
	return exists, err
}

// PutIfAbsent gathers and then writes — the fixed registry's route.
func (k *registerBarrierKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if key == k.key {
		k.arrive()
	}
	return forwardPutIfAbsent(ctx, k.KVStore, key, value)
}

// TestRegisterAgent_NineNodesRegisteringOneAgentProduceOneRecord proves
// registration is decided by the store rather than by a process-local lock.
//
// The damage this prevents is not a duplicate row. A losing write was
// UNCONDITIONAL, so it replaced a LIVE registration with a fresh one — resetting
// Status, RegisteredAt and LastSeen, and re-pointing Host at the loser's node.
// Dispatch then reads an agent record describing a worker that is not the one
// running, which is the same class of erasure the task create had.
func TestRegisterAgent_NineNodesRegisteringOneAgentProduceOneRecord(t *testing.T) {
	const nodes = 9

	ctx := context.Background()
	shared := NewMemoryTaskKV()
	key, err := agentKey("tenant-a", "agent-contested")
	if err != nil {
		t.Fatalf("agentKey: %v", err)
	}
	barrier := newRegisterBarrierKV(shared, key, nodes)

	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		verdicts = make([]error, nodes)
	)
	for i := 0; i < nodes; i++ {
		wg.Add(1)
		go func(node int) {
			defer wg.Done()
			// One registry per node, as separate processes would have: they
			// agree only because the store decides, not because they share a
			// lock.
			registry := NewAgentRegistry(barrier)
			<-start
			verdicts[node] = registry.RegisterAgent(ctx, &AgentRegistration{
				ID:        "agent-contested",
				TenantID:  "tenant-a",
				Name:      "worker",
				Host:      "host-" + strconv.Itoa(node),
				AgentType: "claude-code",
			})
		}(i)
	}
	close(start)
	wg.Wait()

	winner := -1
	for node, err := range verdicts {
		if err == nil {
			if winner >= 0 {
				t.Fatalf("two nodes registered one agent ID: host-%d and host-%d both succeeded",
					winner, node)
			}
			winner = node
		}
	}
	if winner < 0 {
		t.Fatalf("no node registered the agent; verdicts: %v", verdicts)
	}

	for node, err := range verdicts {
		if node == winner {
			continue
		}
		var exists *AgentAlreadyExistsError
		if !errors.As(err, &exists) {
			t.Fatalf("node %d lost the race but got %T: %v, want *AgentAlreadyExistsError",
				node, err, err)
		}
	}

	// The surviving record must be the winner's. A record naming a loser means
	// a late write replaced a live registration — the damage, as distinct from
	// the error.
	registry := NewAgentRegistry(shared)
	stored, err := registry.GetAgent(ctx, "tenant-a", "agent-contested")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if want := "host-" + strconv.Itoa(winner); stored.Host != want {
		t.Errorf("the surviving registration names %q, but %q won it", stored.Host, want)
	}
}
