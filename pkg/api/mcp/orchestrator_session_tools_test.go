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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// This file guards the ACP agent-session surface on MCP: spawn, prompt, list
// sessions, status and close. These verbs exist on REST and gRPC
// (POST /api/v1/orchestrator/sessions and friends); they were registered on MCP
// under quicbaas_agent_* names that no authz map declared, so the default-deny
// gate filtered them out of tools/list and no MCP client could drive a session.
// The tests below pin the registered names, the argument contract, dispatch to
// the orchestration service seam, and the permission mapping.

// sessionToolNames is the ACP agent-session surface an MCP client needs to
// spawn, drive, inspect and tear down a session.
var sessionToolNames = []string{
	"quicbaas_orchestrator_spawn",
	"quicbaas_orchestrator_prompt",
	"quicbaas_orchestrator_list_sessions",
	"quicbaas_orchestrator_status",
	"quicbaas_orchestrator_close",
}

// toolByName returns the registered definition for name.
func toolByName(t *testing.T, reg *ToolRegistry, name string) ToolDefinition {
	t.Helper()
	for _, tool := range reg.tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q is not registered", name)
	return ToolDefinition{}
}

// requiredArgs returns the "required" list of a tool's input schema.
func requiredArgs(t *testing.T, tool ToolDefinition) []string {
	t.Helper()
	required, ok := tool.InputSchema["required"].([]string)
	if !ok {
		return nil
	}
	return required
}

// schemaProperties returns the "properties" object of a tool's input schema.
func schemaProperties(t *testing.T, tool ToolDefinition) map[string]interface{} {
	t.Helper()
	props, ok := tool.InputSchema["properties"].(map[string]interface{})
	require.Truef(t, ok, "tool %q has no properties object", tool.Name)
	return props
}

// =============================================================================
// Registration
// =============================================================================

// TestSessionToolsRegistered asserts every agent-session verb is published
// under its quicbaas_orchestrator_* name and routes through the orchestrator
// dispatch predicate. A tool that is registered but not claimed by isAgentTool
// would reach executeTool and fall through to an unknown-tool error.
func TestSessionToolsRegistered(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})

	for _, name := range sessionToolNames {
		t.Run(name, func(t *testing.T) {
			tool := toolByName(t, reg, name)
			assert.NotEmpty(t, tool.Description, "tool %q needs a description", name)
			assert.Truef(t, isAgentTool(name), "isAgentTool(%q) must claim the tool for dispatch", name)
		})
	}
}

// TestSessionToolsNotRegisteredUnderLegacyAgentNames pins the rename: the old
// quicbaas_agent_* names carry no permission mapping, so re-registering them
// would republish tools that the default-deny gate silently drops.
func TestSessionToolsNotRegisteredUnderLegacyAgentNames(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})

	legacy := map[string]bool{
		"quicbaas_agent_spawn":         true,
		"quicbaas_agent_prompt":        true,
		"quicbaas_agent_list_sessions": true,
		"quicbaas_agent_status":        true,
		"quicbaas_agent_close":         true,
	}
	for _, tool := range reg.tools {
		assert.Falsef(t, legacy[tool.Name], "legacy name %q must not be registered", tool.Name)
	}
	for name := range legacy {
		assert.Falsef(t, isAgentTool(name), "isAgentTool(%q) must not claim the legacy name", name)
	}
}

// TestSessionToolsRequireTenantArgument pins the explicit-tenant contract every
// session tool shares with the fleet and project tools: MCP injects the token
// tenant under the iam context key, which the orchestrator tenant guard does
// not read, so the tenant must be an argument.
func TestSessionToolsRequireTenantArgument(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})

	for _, name := range sessionToolNames {
		t.Run(name, func(t *testing.T) {
			tool := toolByName(t, reg, name)
			assert.Contains(t, requiredArgs(t, tool), "tenant_id")
			assert.Contains(t, schemaProperties(t, tool), "tenant_id")
		})
	}
}

// TestSpawnToolSchemaExposesDispatchControls asserts the spawn schema declares
// every knob the handler reads. An undeclared property is invisible to an MCP
// client, which is how sandbox_mode was silently unusable.
func TestSpawnToolSchemaExposesDispatchControls(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})
	tool := toolByName(t, reg, "quicbaas_orchestrator_spawn")
	props := schemaProperties(t, tool)

	for _, key := range []string{
		"tenant_id", "backend", "project_id", "model", "system_prompt",
		"work_dir", "sandbox_mode", "host_affinity", "host_affinity_strict",
		"gpu_required", "persistent", "mcp_servers",
	} {
		assert.Containsf(t, props, key, "spawn schema must declare %q", key)
	}
	assert.ElementsMatch(t, []string{"tenant_id", "backend", "project_id"}, requiredArgs(t, tool))
}

// =============================================================================
// Argument validation
// =============================================================================

// TestSessionToolsMissingRequiredArgs asserts each session tool rejects a
// missing required argument with the typed RequiredFieldError rather than
// dispatching a half-built request at the orchestrator.
func TestSessionToolsMissingRequiredArgs(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})
	ctx := context.Background()

	cases := []struct {
		name  string
		tool  string
		args  map[string]interface{}
		field string
	}{
		{"spawn without tenant", "quicbaas_orchestrator_spawn",
			map[string]interface{}{"backend": "claude", "project_id": "p1"}, "tenant_id"},
		{"spawn without backend", "quicbaas_orchestrator_spawn",
			map[string]interface{}{"tenant_id": "t1", "project_id": "p1"}, "backend"},
		{"spawn without project", "quicbaas_orchestrator_spawn",
			map[string]interface{}{"tenant_id": "t1", "backend": "claude"}, "project_id"},
		{"prompt without tenant", "quicbaas_orchestrator_prompt",
			map[string]interface{}{"session_id": "s1", "prompt": "hi"}, "tenant_id"},
		{"prompt without prompt", "quicbaas_orchestrator_prompt",
			map[string]interface{}{"tenant_id": "t1", "session_id": "s1"}, "prompt"},
		{"status without tenant", "quicbaas_orchestrator_status",
			map[string]interface{}{"session_id": "s1"}, "tenant_id"},
		{"status without session", "quicbaas_orchestrator_status",
			map[string]interface{}{"tenant_id": "t1"}, "session_id"},
		{"close without tenant", "quicbaas_orchestrator_close",
			map[string]interface{}{"session_id": "s1"}, "tenant_id"},
		{"close without session", "quicbaas_orchestrator_close",
			map[string]interface{}{"tenant_id": "t1"}, "session_id"},
		{"list sessions without tenant", "quicbaas_orchestrator_list_sessions",
			map[string]interface{}{"backend": "claude"}, "tenant_id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reg.executeAgentTool(ctx, tc.tool, tc.args)
			require.Error(t, err)
			assert.Truef(t, isRequiredFieldError(err, tc.field),
				"expected RequiredFieldError(%q), got %v", tc.field, err)
		})
	}
}

// TestSpawnToolRejectsInvalidSandboxMode asserts an unparsable sandbox_mode
// fails before dispatch instead of silently degrading to the server default.
func TestSpawnToolRejectsInvalidSandboxMode(t *testing.T) {
	t.Parallel()

	var dispatched bool
	agentSvc := &mockAgentService{
		spawnFn: func(context.Context, *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			dispatched = true
			return &orchestration.SpawnSessionResponse{SessionID: "s1"}, nil
		},
	}
	reg := newAgentRegistry(t, agentSvc)

	_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_spawn", map[string]interface{}{
		"tenant_id":    "t1",
		"backend":      "claude",
		"project_id":   "p1",
		"sandbox_mode": "not-a-mode",
	})
	require.Error(t, err)
	assert.False(t, dispatched, "an invalid sandbox_mode must not reach the orchestrator")
}

// =============================================================================
// Dispatch to the orchestration service seam
// =============================================================================

// TestSpawnToolDispatch asserts the spawn tool translates MCP arguments into a
// SpawnSessionRequest with every dispatch control preserved, stamps the tenant
// onto the orchestration context the tenant guard reads, and defaults the
// session to persistent (multi-turn) exactly like the REST spawn endpoint.
func TestSpawnToolDispatch(t *testing.T) {
	t.Parallel()

	var (
		gotReq    *orchestration.SpawnSessionRequest
		gotTenant string
		gotHost   string
		gotGPU    bool
	)
	agentSvc := &mockAgentService{
		spawnFn: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			gotReq = req
			gotTenant = orchestration.TenantFromContext(ctx)
			gotHost = orchestration.HostAffinityFromContext(ctx)
			gotGPU = orchestration.GpuRequiredFromContext(ctx)
			return &orchestration.SpawnSessionResponse{
				SessionID: "sess-42",
				Backend:   req.Backend,
				Model:     req.Model,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	reg := newAgentRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_spawn", map[string]interface{}{
		"tenant_id":            "acme",
		"backend":              "claude",
		"project_id":           "proj-1",
		"model":                "opus",
		"system_prompt":        "you are a test agent",
		"work_dir":             "/srv/proj-1",
		"sandbox_mode":         "required",
		"host_affinity":        "gpu-box",
		"host_affinity_strict": true,
		"gpu_required":         true,
		"mcp_servers": []interface{}{
			map[string]interface{}{
				"name":    "quicbaas",
				"command": "quicbaas",
				"args":    []interface{}{"mcp", "serve"},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, gotReq)
	assert.Equal(t, "acme", gotTenant, "tenant must reach the orchestrator tenant guard")
	assert.Equal(t, "claude", gotReq.Backend)
	assert.Equal(t, "proj-1", gotReq.ProjectID)
	assert.Equal(t, "opus", gotReq.Model)
	assert.Equal(t, "you are a test agent", gotReq.SystemPrompt)
	assert.Equal(t, "/srv/proj-1", gotReq.WorkDir)
	assert.Equal(t, orchestration.SandboxModeRequired, gotReq.SandboxMode)
	assert.Equal(t, "gpu-box", gotReq.HostAffinity)
	assert.True(t, gotReq.HostAffinityStrict)
	assert.True(t, gotReq.GpuRequired)
	assert.True(t, gotReq.Persistent, "MCP spawn must default to a multi-turn session")
	require.Len(t, gotReq.MCPServers, 1)
	assert.Equal(t, "quicbaas", gotReq.MCPServers[0].Name)

	assert.Equal(t, "gpu-box", gotHost, "host affinity must travel on the context for the remote resolver")
	assert.True(t, gotGPU, "gpu requirement must travel on the context for the remote resolver")
}

// TestSpawnToolHonorsExplicitNonPersistent asserts an explicit
// "persistent": false is preserved. A plain bool decode cannot tell an absent
// field from an explicit false, which is why the default is applied by key
// presence.
func TestSpawnToolHonorsExplicitNonPersistent(t *testing.T) {
	t.Parallel()

	var gotPersistent = true
	agentSvc := &mockAgentService{
		spawnFn: func(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			gotPersistent = req.Persistent
			return &orchestration.SpawnSessionResponse{SessionID: "s1"}, nil
		},
	}
	reg := newAgentRegistry(t, agentSvc)

	_, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_spawn", map[string]interface{}{
		"tenant_id":  "acme",
		"backend":    "claude",
		"project_id": "proj-1",
		"persistent": false,
	})
	require.NoError(t, err)
	assert.False(t, gotPersistent)
}

// TestPromptToolDispatch asserts the prompt tool routes to an existing session
// with the tenant stamped on the context.
func TestPromptToolDispatch(t *testing.T) {
	t.Parallel()

	var (
		gotReq    *orchestration.PromptSessionRequest
		gotTenant string
	)
	// A session-scoped prompt is asynchronous on every transport, MCP
	// included: the tool returns the acceptance acknowledgement and the
	// turn's output is read back from the session transcript.
	agentSvc := &mockAgentService{
		promptAsyncFn: func(ctx context.Context, req *orchestration.PromptSessionRequest) (*orchestration.PromptTurnAck, error) {
			gotReq = req
			gotTenant = orchestration.TenantFromContext(ctx)
			return &orchestration.PromptTurnAck{
				SessionID: req.SessionID,
				TurnID:    "turn-1",
				Status:    orchestration.TurnStatusAccepted,
				Sequence:  3,
				Model:     "opus",
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	reg := newAgentRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(context.Background(), "quicbaas_orchestrator_prompt", map[string]interface{}{
		"tenant_id":  "acme",
		"session_id": "sess-42",
		"prompt":     "summarize the repo",
		"use_rag":    true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, gotReq)
	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "sess-42", gotReq.SessionID)
	assert.Equal(t, "summarize the repo", gotReq.Prompt)
	assert.True(t, gotReq.UseRAG)
}

// TestSessionInspectionToolsDispatch asserts status, close and list-sessions
// reach the service seam with the tenant and their identifying arguments.
func TestSessionInspectionToolsDispatch(t *testing.T) {
	t.Parallel()

	var (
		statusReq  *orchestration.SessionStatusRequest
		closeReq   *orchestration.CloseSessionRequest
		listReq    *orchestration.ListSessionsRequest
		tenants    []string
		agentSvcFn = func(ctx context.Context) { tenants = append(tenants, orchestration.TenantFromContext(ctx)) }
	)

	agentSvc := &mockAgentService{
		statusFn: func(ctx context.Context, req *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
			agentSvcFn(ctx)
			statusReq = req
			return &orchestration.SessionStatusResponse{
				SessionID: req.SessionID,
				State:     orchestration.SessionStateActive,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
		closeFn: func(ctx context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
			agentSvcFn(ctx)
			closeReq = req
			return &orchestration.CloseSessionResponse{SessionID: req.SessionID, Timestamp: time.Now().UnixNano()}, nil
		},
		listSessionsFn: func(ctx context.Context, req *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
			agentSvcFn(ctx)
			listReq = req
			return &orchestration.ListSessionsResponse{Timestamp: time.Now().UnixNano()}, nil
		},
	}
	reg := newAgentRegistry(t, agentSvc)
	ctx := context.Background()

	statusResult, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_status", map[string]interface{}{
		"tenant_id": "acme", "session_id": "sess-42",
	})
	require.NoError(t, err)
	require.NotNil(t, statusResult)

	closeResult, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_close", map[string]interface{}{
		"tenant_id": "acme", "session_id": "sess-42",
	})
	require.NoError(t, err)
	require.NotNil(t, closeResult)

	listResult, err := reg.executeAgentTool(ctx, "quicbaas_orchestrator_list_sessions", map[string]interface{}{
		"tenant_id": "acme", "backend": "claude",
	})
	require.NoError(t, err)
	require.NotNil(t, listResult)

	require.NotNil(t, statusReq)
	require.NotNil(t, closeReq)
	require.NotNil(t, listReq)
	assert.Equal(t, "sess-42", statusReq.SessionID)
	assert.Equal(t, "sess-42", closeReq.SessionID)
	assert.Equal(t, "claude", listReq.Backend)
	assert.Equal(t, []string{"acme", "acme", "acme"}, tenants)
}

// =============================================================================
// Authorization mapping
// =============================================================================

// TestSessionToolsQRDBPermissionMapping pins each session tool to the same
// qrdb-canonical permission the REST PEP demands for the equivalent endpoint
// (pkg/api/rest/authz_pep_mapping.go: orchestrator spawn/prompt/status/close/
// list). A drift here silently changes who can drive an agent session.
func TestSessionToolsQRDBPermissionMapping(t *testing.T) {
	t.Parallel()

	want := map[string]qrdbToolResource{
		"quicbaas_orchestrator_spawn":         {"orchestrator", "spawn"},
		"quicbaas_orchestrator_prompt":        {"orchestrator", "prompt"},
		"quicbaas_orchestrator_status":        {"orchestrator", "status"},
		"quicbaas_orchestrator_close":         {"orchestrator", "close"},
		"quicbaas_orchestrator_list_sessions": {"orchestrator", "list"},
	}

	for name, expect := range want {
		t.Run(name, func(t *testing.T) {
			resource, action, ok := getToolQRDBPermission(name)
			require.Truef(t, ok, "tool %q is unmapped — default-deny hides it from tools/list", name)
			assert.Equal(t, expect.resource, resource)
			assert.Equal(t, expect.action, action)
		})
	}
}

// TestOrchestratorSurfacePermissionMappingIsComplete is the bidirectional guard
// for the orchestrator surface, mirroring TestBarrierToolsGranularAuthzMapping.
//
// Forward: every registered tool the orchestrator dispatchers claim must resolve
// to a qrdb permission. An unmapped tool is default-denied by the authz gate, so
// it never reaches tools/list — the exact failure that hid the session verbs.
//
// Reverse: every orchestrator-resource entry in the permission map must belong
// to a registered tool, so a rename cannot leave a stale mapping behind that
// makes the surface look wired when it is not.
func TestOrchestratorSurfacePermissionMappingIsComplete(t *testing.T) {
	t.Parallel()

	reg := newAgentRegistry(t, &mockAgentService{})

	registered := make(map[string]bool, len(reg.tools))
	for _, tool := range reg.tools {
		registered[tool.Name] = true
	}

	var surface []string
	for name := range registered {
		if isAgentTool(name) || isAgentCacheTool(name) {
			surface = append(surface, name)
		}
	}
	require.NotEmpty(t, surface)

	for _, name := range surface {
		resource, action, ok := getToolQRDBPermission(name)
		assert.Truef(t, ok, "registered orchestrator tool %q has no qrdb permission mapping; the default-deny gate will hide it", name)
		if ok {
			assert.Equalf(t, "orchestrator", resource, "tool %q must map onto the orchestrator resource", name)
			assert.NotEmptyf(t, action, "tool %q maps to an empty action", name)
		}
	}

	for name, mapping := range qrdbToolExactMapping {
		if mapping.resource != "orchestrator" {
			continue
		}
		assert.Truef(t, registered[name],
			"permission map declares %q but no such tool is registered; remove the stale entry or register the tool", name)
	}
}

// TestSessionToolsAreNotPublic asserts the session verbs stay behind
// authentication. A public session tool would let an unauthenticated caller
// spawn agent processes.
func TestSessionToolsAreNotPublic(t *testing.T) {
	t.Parallel()

	for _, name := range sessionToolNames {
		assert.Falsef(t, IsPublicTool(name), "tool %q must require authentication", name)
	}
}
