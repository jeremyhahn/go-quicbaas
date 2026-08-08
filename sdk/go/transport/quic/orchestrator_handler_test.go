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

package quic

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// isOrchestratorService
// =============================================================================

func TestIsOrchestratorService(t *testing.T) {
	tests := []struct {
		name     string
		svc      string
		expected bool
	}{
		{"fully qualified service", "quicbaas.Orchestrator", true},
		{"bare service name", "Orchestrator", true},
		{"extended prefix", "quicbaas.OrchestratorExtended", true},
		{"KV service", "quicbaas.KV", false},
		{"Embedding service", "quicbaas.Embedding", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isOrchestratorService(tt.svc))
		})
	}
}

// =============================================================================
// OrchestratorMethodToSubOp
// =============================================================================

func TestOrchestratorMethodToSubOp_AllMethods(t *testing.T) {
	tests := []struct {
		method string
		subOp  byte
	}{
		{"SpawnSession", SubOpOrchestratorSpawn},
		{"PromptSession", SubOpOrchestratorPrompt},
		{"SessionStatus", SubOpOrchestratorSessionStatus},
		{"CloseSession", SubOpOrchestratorCloseSession},
		{"ListSessions", SubOpOrchestratorListSessions},
		{"CreateTask", SubOpOrchestratorCreateTask},
		{"GetTask", SubOpOrchestratorGetTask},
		{"ListTasks", SubOpOrchestratorListTasks},
		{"ClaimTask", SubOpOrchestratorClaimTask},
		{"CompleteTask", SubOpOrchestratorCompleteTask},
		{"ListBackends", SubOpOrchestratorListBackends},
		{"Health", SubOpOrchestratorHealth},
		{"Inference", SubOpOrchestratorInference},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got, ok := OrchestratorMethodToSubOp(tt.method)
			require.True(t, ok, "expected subop for method %q", tt.method)
			assert.Equal(t, tt.subOp, got)
		})
	}
}

func TestOrchestratorMethodToSubOp_UnknownMethod(t *testing.T) {
	_, ok := OrchestratorMethodToSubOp("NonExistentMethod")
	assert.False(t, ok, "expected false for unknown method")
}

// =============================================================================
// MethodToOpcode — Orchestrator entries
// =============================================================================

func TestMethodToOpcode_OrchestratorMethods(t *testing.T) {
	methods := []string{
		"SpawnSession",
		"PromptSession",
		"SessionStatus",
		"CloseSession",
		"ListSessions",
		"CreateTask",
		"GetTask",
		"ListTasks",
		"ClaimTask",
		"CompleteTask",
		"ListBackends",
		"Health",
		"Inference",
	}

	for _, m := range methods {
		t.Run("quicbaas.Orchestrator/"+m, func(t *testing.T) {
			opcode, ok := MethodToOpcode("quicbaas.Orchestrator", m)
			require.True(t, ok, "expected opcode for quicbaas.Orchestrator/%s", m)
			assert.Equal(t, OpOrchestrator, opcode,
				"expected OpOrchestrator (0x8F) for method %s", m)
		})

		t.Run("Orchestrator/"+m, func(t *testing.T) {
			opcode, ok := MethodToOpcode("Orchestrator", m)
			require.True(t, ok, "expected opcode for Orchestrator/%s", m)
			assert.Equal(t, OpOrchestrator, opcode,
				"expected OpOrchestrator (0x8F) for legacy method %s", m)
		})
	}
}

// =============================================================================
// convertOrchestratorResponse — happy path per method
// =============================================================================

func TestConvertOrchestratorResponse_SpawnSession(t *testing.T) {
	t.Run("converts SpawnSession response", func(t *testing.T) {
		data := map[string]interface{}{
			"session_id":   "sess-001",
			"backend":      "ollama",
			"container_id": "c123",
		}
		result, err := convertOrchestratorResponse("SpawnSession", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.SpawnSessionResponse)
		require.True(t, ok)
		assert.Equal(t, "sess-001", resp.SessionID)
		assert.Equal(t, "ollama", resp.Backend)
		assert.Equal(t, "c123", resp.ContainerID)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"session_id": []interface{}{"not", "a", "string"},
		}
		_, err := convertOrchestratorResponse("SpawnSession", data)
		require.Error(t, err)
	})
}

// TestConvertOrchestratorResponse_PromptSession pins the converter against the
// payload the orchestrator service actually puts on the wire: content blocks
// plus a stop reason. The service never sends "response"/"done" — decoding
// straight into the SDK type is what produced a contentless "Done: false".
func TestConvertOrchestratorResponse_PromptSession(t *testing.T) {
	t.Run("converts the service turn payload into a done response", func(t *testing.T) {
		data := map[string]interface{}{
			"session_id": "sess-001",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hello world"},
			},
			"model":       "claude-fable-5",
			"stop_reason": "end_turn",
			"timestamp":   float64(1786207030175886792),
		}
		result, err := convertOrchestratorResponse("PromptSession", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.PromptSessionResponse)
		require.True(t, ok)
		assert.Equal(t, "sess-001", resp.SessionID)
		assert.Equal(t, "hello world", resp.Response)
		assert.Equal(t, "claude-fable-5", resp.Model)
		assert.Equal(t, "end_turn", resp.StopReason)
		assert.True(t, resp.Done)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"stop_reason": 42,
		}
		_, err := convertOrchestratorResponse("PromptSession", data)
		require.Error(t, err)
	})

	t.Run("returns a typed error for a turn with no content and no stop reason", func(t *testing.T) {
		data := map[string]interface{}{"session_id": "sess-001"}
		_, err := convertOrchestratorResponse("PromptSession", data)
		require.Error(t, err)
		var empty *orchestrator.EmptyTurnError
		require.ErrorAs(t, err, &empty)
	})
}

func TestConvertOrchestratorResponse_SessionStatus(t *testing.T) {
	t.Run("converts SessionStatus response", func(t *testing.T) {
		// The service names the lifecycle field "state", not "status".
		data := map[string]interface{}{
			"session_id":    "sess-001",
			"backend":       "ollama",
			"state":         "active",
			"created_at":    float64(1700000000),
			"last_activity": float64(1700000100),
		}
		result, err := convertOrchestratorResponse("SessionStatus", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.SessionStatusResponse)
		require.True(t, ok)
		assert.Equal(t, "sess-001", resp.SessionID)
		assert.Equal(t, "ollama", resp.Backend)
		assert.Equal(t, "active", resp.Status)
		assert.Equal(t, int64(1700000100), resp.LastActive)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"created_at": "not-a-number",
		}
		_, err := convertOrchestratorResponse("SessionStatus", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_CloseSession(t *testing.T) {
	t.Run("converts CloseSession response", func(t *testing.T) {
		// The service acknowledges with the id and a timestamp only; the
		// acknowledgement itself is what means "closed".
		data := map[string]interface{}{
			"session_id": "sess-001",
			"timestamp":  float64(1786207020331543767),
		}
		result, err := convertOrchestratorResponse("CloseSession", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.CloseSessionResponse)
		require.True(t, ok)
		assert.Equal(t, "sess-001", resp.SessionID)
		assert.True(t, resp.Closed)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"session_id": 42,
		}
		_, err := convertOrchestratorResponse("CloseSession", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_ListSessions(t *testing.T) {
	t.Run("converts ListSessions response", func(t *testing.T) {
		data := map[string]interface{}{
			"sessions": []interface{}{},
			"count":    float64(0),
		}
		result, err := convertOrchestratorResponse("ListSessions", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.ListSessionsResponse)
		require.True(t, ok)
		assert.Equal(t, 0, resp.Count)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"sessions": "not-an-array",
		}
		_, err := convertOrchestratorResponse("ListSessions", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_CreateTask(t *testing.T) {
	t.Run("converts CreateTask response", func(t *testing.T) {
		data := map[string]interface{}{
			"id":        "task-001",
			"tenant_id": "tenant-abc",
			"subject":   "build feature X",
			"status":    "pending",
		}
		result, err := convertOrchestratorResponse("CreateTask", data)
		require.NoError(t, err)
		task, ok := result.(*orchestrator.Task)
		require.True(t, ok)
		assert.Equal(t, "task-001", task.ID)
		assert.Equal(t, "build feature X", task.Subject)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"dependencies": "not-an-array",
		}
		_, err := convertOrchestratorResponse("CreateTask", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_GetTask(t *testing.T) {
	t.Run("converts GetTask response", func(t *testing.T) {
		data := map[string]interface{}{
			"id":        "task-002",
			"tenant_id": "tenant-abc",
			"subject":   "deploy service",
			"status":    "claimed",
		}
		result, err := convertOrchestratorResponse("GetTask", data)
		require.NoError(t, err)
		task, ok := result.(*orchestrator.Task)
		require.True(t, ok)
		assert.Equal(t, "task-002", task.ID)
		assert.Equal(t, "claimed", task.Status)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"dependencies": "not-an-array",
		}
		_, err := convertOrchestratorResponse("GetTask", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_ListTasks(t *testing.T) {
	t.Run("converts ListTasks response", func(t *testing.T) {
		data := map[string]interface{}{
			"tasks": []interface{}{},
			"count": float64(0),
		}
		result, err := convertOrchestratorResponse("ListTasks", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.ListTasksResponse)
		require.True(t, ok)
		assert.Equal(t, 0, resp.Count)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"tasks": "not-an-array",
		}
		_, err := convertOrchestratorResponse("ListTasks", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_ClaimTask(t *testing.T) {
	t.Run("converts ClaimTask response", func(t *testing.T) {
		data := map[string]interface{}{
			"task_id":  "task-003",
			"agent_id": "agent-7",
			"claimed":  true,
		}
		result, err := convertOrchestratorResponse("ClaimTask", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.ClaimTaskResponse)
		require.True(t, ok)
		assert.Equal(t, "task-003", resp.TaskID)
		assert.True(t, resp.Claimed)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"claimed": "not-a-bool",
		}
		_, err := convertOrchestratorResponse("ClaimTask", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_CompleteTask(t *testing.T) {
	t.Run("converts CompleteTask response", func(t *testing.T) {
		data := map[string]interface{}{
			"task_id":   "task-004",
			"completed": true,
		}
		result, err := convertOrchestratorResponse("CompleteTask", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.CompleteTaskResponse)
		require.True(t, ok)
		assert.Equal(t, "task-004", resp.TaskID)
		assert.True(t, resp.Completed)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"completed": "not-a-bool",
		}
		_, err := convertOrchestratorResponse("CompleteTask", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_ListBackends(t *testing.T) {
	t.Run("converts ListBackends response", func(t *testing.T) {
		data := map[string]interface{}{
			"backends": []interface{}{
				map[string]interface{}{
					"name":      "ollama",
					"type":      "local",
					"status":    "available",
					"available": true,
				},
			},
			"count": float64(1),
		}
		result, err := convertOrchestratorResponse("ListBackends", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.ListBackendsResponse)
		require.True(t, ok)
		assert.Equal(t, 1, resp.Count)
		require.Len(t, resp.Backends, 1)
		assert.Equal(t, "ollama", resp.Backends[0].Name)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"backends": "not-an-array",
		}
		_, err := convertOrchestratorResponse("ListBackends", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_Health(t *testing.T) {
	t.Run("converts Health response with bool healthy field", func(t *testing.T) {
		data := map[string]interface{}{
			"healthy":   true,
			"status":    "ok",
			"timestamp": float64(1700000000),
		}
		result, err := convertOrchestratorResponse("Health", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.HealthCheckReport)
		require.True(t, ok)
		assert.True(t, resp.Healthy)
		assert.Equal(t, "ok", resp.Status)
	})

	t.Run("converts Health response with server integer-count shape", func(t *testing.T) {
		// Server returns pkg/orchestration.HealthCheckReport with int counts.
		data := map[string]interface{}{
			"timestamp":      float64(1700000000),
			"total_sessions": float64(3),
			"healthy":        float64(3),
			"unhealthy":      float64(0),
			"closed":         float64(0),
		}
		result, err := convertOrchestratorResponse("Health", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.HealthCheckReport)
		require.True(t, ok)
		assert.True(t, resp.Healthy, "all sessions healthy means report is healthy")
		assert.Equal(t, "ok", resp.Status)
	})

	t.Run("reports unhealthy when unhealthy count > 0", func(t *testing.T) {
		data := map[string]interface{}{
			"timestamp":      float64(1700000000),
			"total_sessions": float64(3),
			"healthy":        float64(2),
			"unhealthy":      float64(1),
		}
		result, err := convertOrchestratorResponse("Health", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.HealthCheckReport)
		require.True(t, ok)
		assert.False(t, resp.Healthy)
		assert.Equal(t, "degraded", resp.Status)
	})
}

func TestConvertOrchestratorResponse_Inference(t *testing.T) {
	t.Run("converts Inference response", func(t *testing.T) {
		data := map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{
					"type": "text",
					"text": "ok-quic",
				},
			},
			"model":       "qwen3:1.7b",
			"stop_reason": "end_turn",
		}
		result, err := convertOrchestratorResponse("Inference", data)
		require.NoError(t, err)
		resp, ok := result.(*orchestrator.InferenceResponse)
		require.True(t, ok)
		require.Len(t, resp.Content, 1)
		assert.Equal(t, "text", resp.Content[0].Type)
		assert.Equal(t, "ok-quic", resp.Content[0].Text)
		assert.Equal(t, "qwen3:1.7b", resp.Model)
		assert.Equal(t, "end_turn", resp.StopReason)
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		data := map[string]interface{}{
			"content": "not-an-array",
		}
		_, err := convertOrchestratorResponse("Inference", data)
		require.Error(t, err)
	})
}

func TestConvertOrchestratorResponse_UnknownMethod(t *testing.T) {
	data := map[string]interface{}{"key": "value"}
	result, err := convertOrchestratorResponse("UnknownMethod", data)
	require.NoError(t, err, "unknown method should pass through without error")
	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "value", m["key"])
}

func TestConvertOrchestratorResponse_NilData(t *testing.T) {
	result, err := convertOrchestratorResponse("Inference", nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// SubOp constants — wire value parity with server
// =============================================================================

func TestOrchestratorSubOpConstants(t *testing.T) {
	// These values must match pkg/api/quic/protocol.go exactly.
	assert.Equal(t, byte(0x01), SubOpOrchestratorSpawn)
	assert.Equal(t, byte(0x02), SubOpOrchestratorPrompt)
	assert.Equal(t, byte(0x03), SubOpOrchestratorSessionStatus)
	assert.Equal(t, byte(0x04), SubOpOrchestratorCloseSession)
	assert.Equal(t, byte(0x05), SubOpOrchestratorListSessions)
	assert.Equal(t, byte(0x06), SubOpOrchestratorCreateTask)
	assert.Equal(t, byte(0x07), SubOpOrchestratorGetTask)
	assert.Equal(t, byte(0x08), SubOpOrchestratorListTasks)
	assert.Equal(t, byte(0x09), SubOpOrchestratorClaimTask)
	assert.Equal(t, byte(0x0A), SubOpOrchestratorCompleteTask)
	assert.Equal(t, byte(0x0B), SubOpOrchestratorListBackends)
	assert.Equal(t, byte(0x0C), SubOpOrchestratorHealth)
	assert.Equal(t, byte(0x0D), SubOpOrchestratorInference)
}

func TestOpOrchestratorConstant(t *testing.T) {
	// Must match server-side OpOrchestrator in pkg/api/quic/protocol.go.
	assert.Equal(t, byte(0x8F), OpOrchestrator)
}

// =============================================================================
// injectTenantID
// =============================================================================

func TestInjectTenantID_InjectsWhenContextHasTenant(t *testing.T) {
	ctx := sdktransport.ContextWithTenant(context.Background(), "acme")
	payload := []byte(`{"backend":"ollama","model":"qwen3:1.7b","prompt":"hello"}`)

	result := injectTenantID(ctx, payload)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &body))
	assert.Equal(t, "acme", body["tenant_id"])
	// Original fields must be preserved.
	assert.Equal(t, "ollama", body["backend"])
	assert.Equal(t, "qwen3:1.7b", body["model"])
}

func TestInjectTenantID_NoOpWhenContextHasNoTenant(t *testing.T) {
	payload := []byte(`{"backend":"ollama","prompt":"hello"}`)

	result := injectTenantID(context.Background(), payload)

	// Payload must be unchanged when context carries no tenant.
	assert.Equal(t, payload, result)
}

func TestInjectTenantID_DoesNotOverwriteExistingTenantID(t *testing.T) {
	ctx := sdktransport.ContextWithTenant(context.Background(), "acme")
	// Caller already set tenant_id in the request body.
	payload := []byte(`{"tenant_id":"other-tenant","backend":"ollama"}`)

	result := injectTenantID(ctx, payload)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &body))
	// Pre-existing non-empty tenant_id must not be overwritten.
	assert.Equal(t, "other-tenant", body["tenant_id"])
}

func TestInjectTenantID_OverwritesEmptyTenantID(t *testing.T) {
	ctx := sdktransport.ContextWithTenant(context.Background(), "acme")
	// Empty tenant_id in body should be replaced by context value.
	payload := []byte(`{"tenant_id":"","backend":"ollama"}`)

	result := injectTenantID(ctx, payload)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &body))
	assert.Equal(t, "acme", body["tenant_id"])
}

func TestInjectTenantID_HandlesEmptyPayload(t *testing.T) {
	ctx := sdktransport.ContextWithTenant(context.Background(), "acme")
	// Empty JSON object — minimal valid body.
	payload := []byte(`{}`)

	result := injectTenantID(ctx, payload)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &body))
	assert.Equal(t, "acme", body["tenant_id"])
}

func TestInjectTenantID_ReturnsOriginalOnInvalidJSON(t *testing.T) {
	ctx := sdktransport.ContextWithTenant(context.Background(), "acme")
	// Non-object JSON (array) cannot have tenant_id injected.
	payload := []byte(`["a","b"]`)

	result := injectTenantID(ctx, payload)

	// Must return original payload unchanged.
	assert.Equal(t, payload, result)
}
