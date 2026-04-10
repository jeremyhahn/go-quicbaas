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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewInMemoryAgentRegistry(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	require.NotNil(t, registry, "expected non-nil registry")
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

func TestListAgents_FilterByHost(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	hosts := []string{"host-1", "host-1", "host-2", "host-3"}
	for i, host := range hosts {
		err := registry.RegisterAgent(ctx, &AgentRegistration{
			ID:        "agent-" + uitoa(uint64(i)),
			TenantID:  "tenant-a",
			Name:      "agent",
			Host:      host,
			AgentType: "claude-code",
		})
		require.NoError(t, err)
	}

	agents, err := registry.ListAgents(ctx, "tenant-a", &AgentFilter{Host: "host-1"})
	require.NoError(t, err)
	assert.Len(t, agents, 2, "should return 2 agents on host-1")

	for _, a := range agents {
		assert.Equal(t, "host-1", a.Host)
	}
}

func TestListAgents_FilterByStatus(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	// Register 3 agents (all start as registered)
	for i := range 3 {
		err := registry.RegisterAgent(ctx, &AgentRegistration{
			ID:        "agent-" + uitoa(uint64(i)),
			TenantID:  "tenant-a",
			Name:      "agent",
			Host:      "host-1",
			AgentType: "claude-code",
		})
		require.NoError(t, err)
	}

	// Heartbeat agent-0 and agent-1 to transition them to running
	err := registry.Heartbeat(ctx, "tenant-a", "agent-0")
	require.NoError(t, err)
	err = registry.Heartbeat(ctx, "tenant-a", "agent-1")
	require.NoError(t, err)

	// Filter by running status
	running, err := registry.ListAgents(ctx, "tenant-a", &AgentFilter{Status: AgentStatusRunning})
	require.NoError(t, err)
	assert.Len(t, running, 2, "should have 2 running agents")

	for _, a := range running {
		assert.Equal(t, AgentStatusRunning, a.Status)
	}

	// Filter by registered status
	registered, err := registry.ListAgents(ctx, "tenant-a", &AgentFilter{Status: AgentStatusRegistered})
	require.NoError(t, err)
	assert.Len(t, registered, 1, "should have 1 registered agent")
	assert.Equal(t, AgentStatusRegistered, registered[0].Status)
}

func TestListAgents_FilterByAgentType(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	types := []string{"claude-code", "claude-code", "codex"}
	for i, agentType := range types {
		err := registry.RegisterAgent(ctx, &AgentRegistration{
			ID:        "agent-" + uitoa(uint64(i)),
			TenantID:  "tenant-a",
			Name:      "agent",
			Host:      "host-1",
			AgentType: agentType,
		})
		require.NoError(t, err)
	}

	claudeAgents, err := registry.ListAgents(ctx, "tenant-a", &AgentFilter{AgentType: "claude-code"})
	require.NoError(t, err)
	assert.Len(t, claudeAgents, 2, "should return 2 claude-code agents")

	for _, a := range claudeAgents {
		assert.Equal(t, "claude-code", a.AgentType)
	}

	codexAgents, err := registry.ListAgents(ctx, "tenant-a", &AgentFilter{AgentType: "codex"})
	require.NoError(t, err)
	assert.Len(t, codexAgents, 1, "should return 1 codex agent")
	assert.Equal(t, "codex", codexAgents[0].AgentType)
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
	err = registry.Heartbeat(ctx, "tenant-a", "agent-hb")
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

func TestHeartbeat_NotFound(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.Heartbeat(ctx, "tenant-a", "nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryNotFound),
		"expected ErrAgentRegistryNotFound, got: %v", err)
}

func TestHeartbeat_EmptyTenantID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.Heartbeat(ctx, "", "agent-1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput, got: %v", err)
}

func TestHeartbeat_EmptyAgentID(t *testing.T) {
	t.Parallel()

	registry := NewInMemoryAgentRegistry()
	ctx := context.Background()

	err := registry.Heartbeat(ctx, "tenant-a", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAgentRegistryInvalidInput),
		"expected ErrAgentRegistryInvalidInput, got: %v", err)
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
