//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// quicbaas_orchestrator_rotate_account
// =============================================================================

// rotatingMockAgentService is a mockAgentService that also implements the
// optional rotation verb. The bare mockAgentService deliberately does not,
// which is what makes the "deployment without rotation" branch reachable.
type rotatingMockAgentService struct {
	*mockAgentService

	rotateFn func(ctx context.Context, req *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error)

	lastRequest *orchestration.RotateSessionAccountRequest
	lastTenant  string
}

func (m *rotatingMockAgentService) RotateSessionAccount(
	ctx context.Context,
	req *orchestration.RotateSessionAccountRequest,
) (*orchestration.RotateSessionAccountResult, error) {
	m.lastRequest = req
	m.lastTenant = orchestration.TenantFromContext(ctx)
	if m.rotateFn != nil {
		return m.rotateFn(ctx, req)
	}
	return &orchestration.RotateSessionAccountResult{
		SessionID:         req.SessionID,
		PreviousAccountID: "acct-a",
		AccountID:         "acct-b",
		Strategy:          "least_usage",
		Mode:              orchestration.AccountRotationModeResume,
	}, nil
}

var _ orchestration.SessionAccountRotationService = (*rotatingMockAgentService)(nil)

// decodeToolResult normalizes an MCP tool result into a map. marshalResult may
// hand back either the decoded object or its JSON string form; the SDK-side
// decoder accepts both, so the test does too.
func decodeToolResult(t *testing.T, result interface{}) map[string]interface{} {
	t.Helper()
	switch typed := result.(type) {
	case map[string]interface{}:
		return typed
	case string:
		var decoded map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(typed), &decoded))
		return decoded
	default:
		raw, err := json.Marshal(result)
		require.NoErrorf(t, err, "tool result %T is not encodable", result)
		var decoded map[string]interface{}
		require.NoError(t, json.Unmarshal(raw, &decoded))
		return decoded
	}
}

func TestRotateAccountToolRegistered(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})
	tool := toolByName(t, reg, "quicbaas_orchestrator_rotate_account")

	assert.NotEmpty(t, tool.Description)
	assert.True(t, isAgentTool("quicbaas_orchestrator_rotate_account"),
		"isAgentTool must claim the tool or executeTool falls through to unknown-tool")

	props := schemaProperties(t, tool)
	for _, key := range []string{"tenant_id", "session_id", "reason", "hold_outgoing", "available_at"} {
		assert.Containsf(t, props, key, "rotate schema must declare %q", key)
	}
	assert.ElementsMatch(t, []string{"tenant_id", "session_id"}, requiredArgs(t, tool))
}

func TestExecuteRotateAccount_MovesTheSession(t *testing.T) {
	t.Parallel()

	agentSvc := &rotatingMockAgentService{mockAgentService: &mockAgentService{}}
	reg := newAgentRegistry(t, agentSvc)

	availableAt := time.Now().UTC().Add(15 * time.Minute).Truncate(time.Second)
	result, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_rotate_account",
		map[string]interface{}{
			"tenant_id":     "acme",
			"session_id":    "sess-42",
			"reason":        "seat retired",
			"hold_outgoing": true,
			"available_at":  availableAt.Format(time.RFC3339),
		})
	require.NoError(t, err)

	decoded := decodeToolResult(t, result)
	assert.Equal(t, "sess-42", decoded["session_id"])
	assert.Equal(t, "acct-a", decoded["previous_account_id"])
	assert.Equal(t, "acct-b", decoded["account_id"])
	assert.Equal(t, "resume", decoded["mode"])

	require.NotNil(t, agentSvc.lastRequest)
	assert.Equal(t, "sess-42", agentSvc.lastRequest.SessionID)
	assert.Equal(t, "seat retired", agentSvc.lastRequest.Reason)
	assert.True(t, agentSvc.lastRequest.HoldOutgoing)
	assert.True(t, availableAt.Equal(agentSvc.lastRequest.AvailableAt))
	assert.Equal(t, "acme", agentSvc.lastTenant, "tenant must reach the orchestrator tenant guard")
}

func TestExecuteRotateAccount_MissingRequiredArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"without tenant", map[string]interface{}{"session_id": "sess-42"}},
		{"without session", map[string]interface{}{"tenant_id": "acme"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentSvc := &rotatingMockAgentService{mockAgentService: &mockAgentService{}}
			reg := newAgentRegistry(t, agentSvc)

			_, err := reg.executeAgentTool(context.Background(),
				"quicbaas_orchestrator_rotate_account", tc.args)
			require.Error(t, err)
			assert.Nil(t, agentSvc.lastRequest, "a half-built request must not reach the orchestrator")
		})
	}
}

// TestExecuteRotateAccount_MalformedAvailableAtIsRejected keeps a bad hold
// window from being silently dropped, which would put the exhausted account
// straight back in the pool.
func TestExecuteRotateAccount_MalformedAvailableAtIsRejected(t *testing.T) {
	t.Parallel()

	agentSvc := &rotatingMockAgentService{mockAgentService: &mockAgentService{}}
	reg := newAgentRegistry(t, agentSvc)

	_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_rotate_account",
		map[string]interface{}{
			"tenant_id":     "acme",
			"session_id":    "sess-42",
			"hold_outgoing": true,
			"available_at":  "in about an hour",
		})
	require.Error(t, err)
	assert.Nil(t, agentSvc.lastRequest)
}

func TestExecuteRotateAccount_ServiceErrorPropagates(t *testing.T) {
	t.Parallel()

	agentSvc := &rotatingMockAgentService{mockAgentService: &mockAgentService{}}
	agentSvc.rotateFn = func(context.Context, *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error) {
		return nil, &orchestration.SessionNotRotatableError{
			SessionID: "sess-42",
			Reason:    "the session is pinned to a named account",
		}
	}
	reg := newAgentRegistry(t, agentSvc)

	_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_rotate_account",
		map[string]interface{}{"tenant_id": "acme", "session_id": "sess-42"})
	require.Error(t, err)
	assert.ErrorIs(t, err, orchestration.ErrSessionNotRotatable)
}

// TestExecuteRotateAccount_UnsupportedDeployment covers the wrong-type branch:
// an agent service that predates the pooled account subsystem is answered with
// a typed error instead of panicking on a type assertion.
func TestExecuteRotateAccount_UnsupportedDeployment(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})

	_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_rotate_account",
		map[string]interface{}{"tenant_id": "acme", "session_id": "sess-42"})
	require.Error(t, err)
	assert.ErrorIs(t, err, orchestration.ErrSessionRotationUnsupported)
}

// TestRotateAccountToolAuthorizesAsSpawn pins the deliberate choice of verb:
// rotation closes the agent process and reconstitutes it against a newly
// selected pooled credential, which is a spawn's authority. The value must
// match the REST PEP, gRPC and QUIC tables exactly.
func TestRotateAccountToolAuthorizesAsSpawn(t *testing.T) {
	t.Parallel()

	resource, action, ok := getToolQRDBPermission("quicbaas_orchestrator_rotate_account")
	require.True(t, ok, "the tool is unmapped — default-deny hides it from tools/list")
	assert.Equal(t, "orchestrator", resource)
	assert.Equal(t, "spawn", action)
}

// =============================================================================
// account_allow on the spawn tool
// =============================================================================

// TestSpawnToolCarriesAccountAllow is the MCP half of gap 2: an agent that
// enumerates its rotation pool must actually get a rotatable session.
func TestSpawnToolCarriesAccountAllow(t *testing.T) {
	t.Parallel()

	var captured *orchestration.SpawnSessionRequest
	agentSvc := &mockAgentService{
		spawnFn: func(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			captured = req
			return &orchestration.SpawnSessionResponse{SessionID: "sess-42", Backend: req.Backend}, nil
		},
	}
	reg := newAgentRegistry(t, agentSvc)

	_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_spawn",
		map[string]interface{}{
			"tenant_id":     "acme",
			"backend":       "claude",
			"project_id":    "proj-1",
			"account_id":    "acct-a",
			"account_allow": []interface{}{"acct-a", "acct-b"},
		})
	require.NoError(t, err)

	require.NotNil(t, captured)
	assert.Equal(t, []string{"acct-a", "acct-b"}, captured.AccountAllow, "MCP spawn dropped account_allow")
	assert.Equal(t, "acct-a", captured.AccountID)
}

// TestSpawnToolSchemaDeclaresAccountAllow keeps the argument visible to MCP
// clients — an undeclared property is unusable no matter what the handler reads.
func TestSpawnToolSchemaDeclaresAccountAllow(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})
	props := schemaProperties(t, toolByName(t, reg, "quicbaas_orchestrator_spawn"))
	assert.Contains(t, props, "account_allow")
}
