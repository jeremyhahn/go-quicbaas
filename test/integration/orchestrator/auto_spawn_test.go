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

//go:build integration

package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// registerOnlineAgent registers an agent so the auto-spawn matcher can find
// it. The agent's status remains "registered" by default which counts as
// online. Returns the registered agent.
func registerOnlineAgent(t *testing.T, ctx context.Context, tenantID, name, backend string, capabilities []string) *orchestration.FleetNode {
	t.Helper()
	resp, err := testService.Agent().RegisterFleetNode(ctx, &orchestration.RegisterFleetNodeRequest{
		TenantID:     tenantID,
		Name:         name,
		Host:         "host-" + name,
		NodeType:     "worker",
		Backend:      backend,
		Capabilities: capabilities,
	})
	require.NoError(t, err, "register agent")
	require.NotNil(t, resp.Node)
	return resp.Node
}

// TestAutoSpawn_DefaultFalse verifies that when AutoSpawn is omitted (false)
// the task is queued without spawning a job. SpawnedJobID must be empty.
func TestAutoSpawn_DefaultFalse(t *testing.T) {
	tenantID := newTenant(t, "tenant-noas-")
	ctx := tenantCtx(tenantID)

	task, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID: tenantID,
		Subject:  "no-auto-spawn",
	})
	require.NoError(t, err)
	require.NotNil(t, task)
	assert.False(t, task.AutoSpawn, "AutoSpawn defaults to false")
	assert.Empty(t, task.SpawnedJobID, "SpawnedJobID must be empty when AutoSpawn=false")
}

// TestAutoSpawn_NoMatchingAgent verifies that AutoSpawn=true with no
// registered agents returns *NoMatchingFleetNodeError and persists the task with
// an empty SpawnedJobID.
func TestAutoSpawn_NoMatchingAgent(t *testing.T) {
	tenantID := newTenant(t, "tenant-as-noagent-")
	ctx := tenantCtx(tenantID)

	task, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:  tenantID,
		Subject:   "auto-spawn-no-agent",
		AutoSpawn: true,
	})
	require.Error(t, err, "AutoSpawn with no agents should error")
	require.NotNil(t, task, "task is persisted even when spawn fails")

	var noMatch *orchestration.NoMatchingFleetNodeError
	assert.True(t, errors.As(err, &noMatch),
		"expected *orchestration.NoMatchingFleetNodeError, got %T: %v", err, err)
	assert.Empty(t, task.SpawnedJobID, "SpawnedJobID empty on no-match")

	// Verify task is queryable via GetTask.
	fetched, err := testService.Agent().GetTask(ctx, tenantID, task.ID)
	require.NoError(t, err)
	require.NotNil(t, fetched)
	assert.Empty(t, fetched.SpawnedJobID, "persisted task carries empty SpawnedJobID")
}

// TestAutoSpawn_GpuRequired_NoGpuAgent verifies that AutoSpawn with
// GpuRequired=true and only non-GPU agents online returns NoMatchingFleetNodeError.
func TestAutoSpawn_GpuRequired_NoGpuAgent(t *testing.T) {
	tenantID := newTenant(t, "tenant-as-gpu-")
	ctx := tenantCtx(tenantID)

	// Register a non-GPU agent.
	registerOnlineAgent(t, ctx, tenantID, "cpu-agent-"+uniqueSuffix(), "ollama", nil)

	_, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:    tenantID,
		Subject:     "gpu-required",
		AutoSpawn:   true,
		GpuRequired: true,
	})
	require.Error(t, err)
	var noMatch *orchestration.NoMatchingFleetNodeError
	assert.True(t, errors.As(err, &noMatch),
		"expected NoMatchingFleetNodeError for GPU-required, got %T: %v", err, err)
}

// TestAutoSpawn_BackendFilter verifies the just-fixed Backend propagation in
// autoSpawnTask. With Backend="ollama" but no ollama-capable agent, dispatch
// must be rejected with NoMatchingFleetNodeError.
func TestAutoSpawn_BackendFilter(t *testing.T) {
	tenantID := newTenant(t, "tenant-as-backend-")
	ctx := tenantCtx(tenantID)

	// Register an agent on a different backend.
	registerOnlineAgent(t, ctx, tenantID, "claude-agent-"+uniqueSuffix(), "claude", nil)

	_, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:  tenantID,
		Subject:   "backend-filter-mismatch",
		AutoSpawn: true,
		Backend:   "ollama",
	})
	require.Error(t, err, "Backend filter should reject when no agent matches")

	var noMatch *orchestration.NoMatchingFleetNodeError
	assert.True(t, errors.As(err, &noMatch),
		"expected NoMatchingFleetNodeError for Backend filter mismatch, got %T: %v", err, err)
}

// TestAutoSpawn_HappyPath verifies that AutoSpawn=true with a matching agent
// online produces a non-empty SpawnedJobID. The spawned job's lifecycle is
// not asserted (the control loop runs asynchronously and may fail because
// real backends aren't reachable in the test) — we only assert the spawn
// metadata is populated synchronously.
func TestAutoSpawn_HappyPath(t *testing.T) {
	tenantID := newTenant(t, "tenant-as-happy-")
	ctx := tenantCtx(tenantID)

	registerOnlineAgent(t, ctx, tenantID, "ollama-agent-"+uniqueSuffix(), "ollama", nil)

	task, err := testService.Agent().CreateTask(ctx, &orchestration.CreateTaskRequest{
		TenantID:  tenantID,
		Subject:   "auto-spawn-happy",
		AutoSpawn: true,
		Backend:   "ollama",
	})
	require.NoError(t, err, "AutoSpawn with matching agent should succeed")
	require.NotNil(t, task)
	assert.True(t, task.AutoSpawn, "AutoSpawn flag is preserved")
	assert.NotEmpty(t, task.SpawnedJobID, "SpawnedJobID populated when spawn succeeds")
	assert.NotZero(t, task.SpawnedAt, "SpawnedAt populated when spawn succeeds")
}
