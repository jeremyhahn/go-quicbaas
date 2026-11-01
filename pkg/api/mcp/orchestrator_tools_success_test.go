//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Agent Mock Service with real agent service
// =============================================================================

type agentMockQuicBaaS struct {
	mockService
	agentSvc orchestration.AgentService
	msgSvc   messaging.Service
}

func (m *agentMockQuicBaaS) Agent() orchestration.AgentService {
	return m.agentSvc
}

func (m *agentMockQuicBaaS) Messaging() messaging.Service {
	return m.msgSvc
}

func newFleetRegistry(t *testing.T, agentSvc orchestration.AgentService) *ToolRegistry {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	svc := &agentMockQuicBaaS{agentSvc: agentSvc}
	return NewToolRegistry(svc, log)
}

func newFleetRegistryWithMessaging(t *testing.T, agentSvc orchestration.AgentService, msgSvc messaging.Service) *ToolRegistry {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	svc := &agentMockQuicBaaS{agentSvc: agentSvc, msgSvc: msgSvc}
	return NewToolRegistry(svc, log)
}

// =============================================================================
// Agent Discover Tests
// =============================================================================

func TestExecuteAgentDiscover_Success(t *testing.T) {
	agentSvc := &mockAgentService{
		discoverFn: func(_ context.Context, req *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error) {
			return &orchestration.DiscoverProjectsResponse{
				Projects:  []*orchestration.DiscoveredProject{{Name: "my-project", Path: req.RootPath}},
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_discover", map[string]interface{}{
		"root_path": "/home/user/projects",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestExecuteAgentTools_MissingArgument(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		args  map[string]interface{}
		field string
	}{
		{"discover root_path", "quicbaas_orchestrator_discover", map[string]interface{}{}, "root_path"},
		{"register name", "quicbaas_orchestrator_register", map[string]interface{}{"path": "/tmp"}, "name"},
		{"register path", "quicbaas_orchestrator_register", map[string]interface{}{"name": "proj"}, "path"},
		{"spawn backend", "quicbaas_orchestrator_spawn", map[string]interface{}{"tenant_id": "t1", "project_id": "proj-1"}, "agent_id, pool_id or backend"},
		{"spawn project_id", "quicbaas_orchestrator_spawn", map[string]interface{}{"tenant_id": "t1", "backend": "claude"}, "project_id"},
		{"prompt prompt", "quicbaas_orchestrator_prompt", map[string]interface{}{"tenant_id": "t1"}, "prompt"},
		{"status session_id", "quicbaas_orchestrator_status", map[string]interface{}{"tenant_id": "t1"}, "session_id"},
		{"close session_id", "quicbaas_orchestrator_close", map[string]interface{}{"tenant_id": "t1"}, "session_id"},
		{"decompose backend", "quicbaas_orchestrator_decompose", map[string]interface{}{"task": "test"}, "backend"},
		{"decompose task", "quicbaas_orchestrator_decompose", map[string]interface{}{"backend": "claude"}, "task"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := newFleetRegistry(t, &mockAgentService{})

			_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), tt.tool, tt.args)
			require.Error(t, err)
			assert.True(t, isRequiredFieldError(err, tt.field))
		})
	}
}

func TestExecuteAgentTools_ServiceError(t *testing.T) {
	failure := errors.New("agent service failed")
	tests := []struct {
		name     string
		tool     string
		agentSvc *mockAgentService
		args     map[string]interface{}
	}{
		{"discover", "quicbaas_orchestrator_discover", &mockAgentService{
			discoverFn: func(_ context.Context, _ *orchestration.DiscoverProjectsRequest) (*orchestration.DiscoverProjectsResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{"root_path": "/tmp"}},
		{"register", "quicbaas_orchestrator_register", &mockAgentService{
			registerFn: func(_ context.Context, _ *orchestration.RegisterProjectRequest) (*orchestration.RegisterProjectResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{"name": "proj", "path": "/tmp"}},
		{"list", "quicbaas_orchestrator_list", &mockAgentService{
			listProjectsFn: func(_ context.Context, _ *orchestration.ListProjectsRequest) (*orchestration.ListProjectsResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{}},
		{"spawn", "quicbaas_orchestrator_spawn", &mockAgentService{
			spawnFn: func(_ context.Context, _ *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{"tenant_id": "t1", "backend": "claude", "project_id": "proj-1"}},
		{"prompt", "quicbaas_orchestrator_prompt", &mockAgentService{
			promptFn: func(_ context.Context, _ *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{"tenant_id": "t1", "prompt": "test"}},
		{"status", "quicbaas_orchestrator_status", &mockAgentService{
			statusFn: func(_ context.Context, _ *orchestration.SessionStatusRequest) (*orchestration.SessionStatusResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{"tenant_id": "t1", "session_id": "sess-1"}},
		{"close", "quicbaas_orchestrator_close", &mockAgentService{
			closeFn: func(_ context.Context, _ *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{"tenant_id": "t1", "session_id": "sess-1"}},
		{"list sessions", "quicbaas_orchestrator_list_sessions", &mockAgentService{
			listSessionsFn: func(_ context.Context, _ *orchestration.ListSessionsRequest) (*orchestration.ListSessionsResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{"tenant_id": "t1"}},
		{"list backends", "quicbaas_orchestrator_list_backends", &mockAgentService{
			listBackendsFn: func(_ context.Context) ([]*orchestration.AgentBackendInfo, error) {
				return nil, failure
			},
		}, map[string]interface{}{}},
		{"decompose", "quicbaas_orchestrator_decompose", &mockAgentService{
			decomposeFn: func(_ context.Context, _ *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error) {
				return nil, failure
			},
		}, map[string]interface{}{"backend": "claude", "task": "test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := newFleetRegistry(t, tt.agentSvc)

			_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), tt.tool, tt.args)
			require.Error(t, err)
		})
	}
}

// =============================================================================
// Agent Register Tests
// =============================================================================

func TestExecuteAgentRegister_Success(t *testing.T) {
	agentSvc := &mockAgentService{}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_register", map[string]interface{}{
		"name":            "my-project",
		"path":            "/home/user/my-project",
		"dependencies":    []interface{}{"dep-1"},
		"default_backend": "claude",
		"default_model":   "opus",
		"labels":          map[string]interface{}{"env": "dev", "team": "backend"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent List Tests
// =============================================================================

func TestExecuteAgentList_Success(t *testing.T) {
	agentSvc := &mockAgentService{}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_list", map[string]interface{}{
		"page_size":  float64(10),
		"page_token": "<account>",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent Spawn Tests
// =============================================================================

func TestExecuteAgentSpawn_Success(t *testing.T) {
	agentSvc := &mockAgentService{
		spawnFn: func(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			return &orchestration.SpawnSessionResponse{
				SessionID: "sess-1",
				Backend:   req.Backend,
				Model:     req.Model,
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_spawn", map[string]interface{}{
		"tenant_id":     "t1",
		"backend":       "claude",
		"project_id":    "proj-1",
		"model":         "opus",
		"system_prompt": "You are a test agent",
		"mcp_servers": []interface{}{
			map[string]interface{}{
				"name":    "server-1",
				"command": "npx",
				"args":    []interface{}{"-y", "server"},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent Prompt Tests
// =============================================================================

func TestExecuteAgentPrompt_Success(t *testing.T) {
	agentSvc := &mockAgentService{
		promptFn: func(_ context.Context, _ *orchestration.PromptSessionRequest) (*orchestration.PromptSessionResponse, error) {
			return &orchestration.PromptSessionResponse{
				SessionID:  "sess-1",
				Content:    []orchestration.ContentBlock{{Type: "text", Text: "Hello, I am an agent"}},
				Model:      "opus",
				StopReason: "end_turn",
				Timestamp:  time.Now().UnixNano(),
			}, nil
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_prompt", map[string]interface{}{
		"tenant_id":  "t1",
		"prompt":     "Hello agent",
		"backend":    "claude",
		"session_id": "sess-1",
		"project_id": "proj-1",
		"model":      "opus",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent Status Tests
// =============================================================================

func TestExecuteAgentStatus_Success(t *testing.T) {
	agentSvc := &mockAgentService{}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_status", map[string]interface{}{
		"tenant_id": "t1", "session_id": "sess-1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent Close Tests
// =============================================================================

func TestExecuteAgentClose_Success(t *testing.T) {
	agentSvc := &mockAgentService{}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_close", map[string]interface{}{
		"tenant_id": "t1", "session_id": "sess-1",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent List Sessions Tests
// =============================================================================

func TestExecuteAgentListSessions_Success(t *testing.T) {
	agentSvc := &mockAgentService{}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_list_sessions", map[string]interface{}{
		"tenant_id": "t1", "backend": "claude",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent List Backends Tests
// =============================================================================

func TestExecuteAgentListBackends_Success(t *testing.T) {
	agentSvc := &mockAgentService{
		listBackendsFn: func(_ context.Context) ([]*orchestration.AgentBackendInfo, error) {
			return []*orchestration.AgentBackendInfo{
				{Name: "claude", Type: "anthropic"},
			}, nil
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_list_backends", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Orchestrator ListRunners Tests
// =============================================================================

// TestExecuteOrchestratorListRunners_ReportsTheRegisteredRunners proves the
// tool answers from the orchestration registry rather than from a service:
// the runners a build can drive are the same for every caller.
func TestExecuteOrchestratorListRunners_ReportsTheRegisteredRunners(t *testing.T) {
	reg := newFleetRegistry(t, &mockAgentService{})

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_list_runners", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)

	// marshalResult normalizes the payload through JSON, so re-encoding it is
	// how a test reads it back with the field names the wire carries.
	payload, err := json.Marshal(result)
	require.NoError(t, err)

	var decoded struct {
		Runners []struct {
			Name   string `json:"name"`
			States []struct {
				State         string `json:"state"`
				Label         string `json:"label"`
				SessionBacked bool   `json:"session_backed"`
			} `json:"states"`
		} `json:"runners"`
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(payload, &decoded))

	require.Equal(t, len(decoded.Runners), decoded.Count)
	require.NotEmpty(t, decoded.Runners, "the engine's own runner is always registered")

	var workflow *struct {
		Name   string `json:"name"`
		States []struct {
			State         string `json:"state"`
			Label         string `json:"label"`
			SessionBacked bool   `json:"session_backed"`
		} `json:"states"`
	}
	for i := range decoded.Runners {
		if decoded.Runners[i].Name == orchestration.JobRunnerWorkflow {
			workflow = &decoded.Runners[i]
		}
	}
	require.NotNil(t, workflow, "the workflow runner is registered by the engine's init")
	require.NotEmpty(t, workflow.States)
	assert.Equal(t, string(orchestration.JobStateInitializing), workflow.States[0].State)
	assert.NotEmpty(t, workflow.States[0].Label, "every declared state names itself to a person")

	var sessionBacked bool
	for _, state := range workflow.States {
		if state.SessionBacked {
			sessionBacked = true
		}
	}
	assert.True(t, sessionBacked, "the workflow runner declares at least one session-backed state")
}

// TestExecuteOrchestratorListRunners_RefusesWithoutAnOrchestrator pins the
// same rule the REST and embedded surfaces apply: a node that runs no
// orchestrator drives no jobs, so it must not answer with a runner list that
// describes a capability it does not have.
func TestExecuteOrchestratorListRunners_RefusesWithoutAnOrchestrator(t *testing.T) {
	reg := newFleetRegistry(t, nil)

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_list_runners", map[string]interface{}{})
	require.Error(t, err)
}

// =============================================================================
// Agent Decompose Tests
// =============================================================================

func TestExecuteAgentDecompose_Success(t *testing.T) {
	agentSvc := &mockAgentService{
		decomposeFn: func(_ context.Context, req *orchestration.DecomposeTaskRequest) (*orchestration.DecomposeTaskResponse, error) {
			return &orchestration.DecomposeTaskResponse{
				WorkItems: []*orchestration.WorkItem{{Title: req.Task, ProjectID: "proj-1"}},
				Timestamp: time.Now().UnixNano(),
			}, nil
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_decompose", map[string]interface{}{
		"backend":     "claude",
		"task":        "Refactor the auth module",
		"model":       "opus",
		"project_ids": []interface{}{"proj-1"},
		"use_rag":     true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent Health Tests
// =============================================================================

func TestExecuteAgentHealth_Success(t *testing.T) {
	agentSvc := &mockAgentService{}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_health", map[string]interface{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Agent Broadcast Tests
// =============================================================================

func TestExecuteAgentBroadcast_Success(t *testing.T) {
	msgSvc := &mockMessagingService{}
	reg := newFleetRegistryWithMessaging(t, nil, msgSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_broadcast", map[string]interface{}{
		"channel":  "events",
		"message":  "Hello agents",
		"metadata": map[string]interface{}{"source": "orchestrator"},
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, true, resultMap["published"])
	assert.Equal(t, "events", resultMap["channel"])
}

func TestExecuteAgentBroadcast_NilMessagingService(t *testing.T) {
	reg := newFleetRegistryWithMessaging(t, nil, nil)

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_broadcast", map[string]interface{}{
		"channel": "events",
		"message": "test",
	})
	require.Error(t, err)
}

func TestExecuteAgentBroadcast_PublishError(t *testing.T) {
	msgSvc := &mockMessagingService{
		publishFunc: func(_ context.Context, _ string, _ interface{}, _ ...messaging.PublishOption) (*messaging.PublishResult, error) {
			return nil, errors.New("publish failed")
		},
	}
	reg := newFleetRegistryWithMessaging(t, nil, msgSvc)

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_broadcast", map[string]interface{}{
		"channel": "events",
		"message": "test",
	})
	require.Error(t, err)
}

// =============================================================================
// Agent Cache Put/Get Success Tests
// =============================================================================

func TestExecuteAgentCachePut_Success(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)

	result, err := registry.ExecuteTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_cache_put", map[string]interface{}{
		"project_id": "proj-123",
		"key":        "arch-analysis",
		"value":      `{"result": "module is well structured"}`,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "proj-123", resultMap["project_id"])
	assert.Equal(t, "arch-analysis", resultMap["key"])
	assert.Equal(t, true, resultMap["stored"])
}

func TestExecuteAgentCachePut_ServiceError(t *testing.T) {
	svc := &mockService{
		putFunc: func(_ context.Context, _ *service.PutRequest) (*service.PutResponse, error) {
			return nil, errors.New("put failed")
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)

	_, err := registry.ExecuteTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_cache_put", map[string]interface{}{
		"project_id": "proj-123",
		"key":        "analysis",
		"value":      "data",
	})
	require.Error(t, err)
}

func TestExecuteAgentCacheGet_Success(t *testing.T) {
	svc := &mockService{}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)

	result, err := registry.ExecuteTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_cache_get", map[string]interface{}{
		"project_id": "proj-123",
		"key":        "arch-analysis",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "proj-123", resultMap["project_id"])
	assert.Equal(t, true, resultMap["found"])
}

func TestExecuteAgentCacheGet_NotFound(t *testing.T) {
	svc := &mockService{
		getFunc: func(_ context.Context, _ *service.GetRequest) (*service.GetResponse, error) {
			return &service.GetResponse{Found: false}, nil
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)

	result, err := registry.ExecuteTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_cache_get", map[string]interface{}{
		"project_id": "proj-123",
		"key":        "nonexistent",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, false, resultMap["found"])
}

func TestExecuteAgentCacheGet_ServiceError(t *testing.T) {
	svc := &mockService{
		getFunc: func(_ context.Context, _ *service.GetRequest) (*service.GetResponse, error) {
			return nil, errors.New("get failed")
		},
	}
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)

	_, err := registry.ExecuteTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_cache_get", map[string]interface{}{
		"project_id": "proj-123",
		"key":        "analysis",
	})
	require.Error(t, err)
}

// =============================================================================
// Agent Health Error Tests
// =============================================================================

// healthCheckErrorAgent wraps mockAgentService to return an error from HealthCheck.
type healthCheckErrorAgent struct {
	mockAgentService
}

func (m *healthCheckErrorAgent) HealthCheck(_ context.Context) (*orchestration.HealthCheckReport, error) {
	return nil, errors.New("health check failed")
}

func TestExecuteAgentHealth_Error(t *testing.T) {
	agentSvc := &healthCheckErrorAgent{}
	reg := newFleetRegistry(t, agentSvc)

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_health", map[string]interface{}{})
	require.Error(t, err)
}

func TestExecuteAgentBroadcast_WithoutMetadata(t *testing.T) {
	msgSvc := &mockMessagingService{}
	reg := newFleetRegistryWithMessaging(t, nil, msgSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_broadcast", map[string]interface{}{
		"channel": "events",
		"message": "Hello agents",
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// =============================================================================
// Inference Tool Tests
// =============================================================================

func TestExecuteOrchestratorInference_Success(t *testing.T) {
	t.Parallel()

	agentSvc := &mockAgentService{
		inferFn: func(_ context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			assert.Equal(t, "ollama", req.Backend)
			assert.Equal(t, "llama3", req.Model)
			assert.Equal(t, "what is 2+2?", req.Prompt)
			ch := make(chan *orchestration.InferChunk, 2)
			ch <- &orchestration.InferChunk{
				Delta: []orchestration.ContentBlock{{Type: "text", Text: "The answer is "}},
			}
			ch <- &orchestration.InferChunk{
				Delta:      []orchestration.ContentBlock{{Type: "text", Text: "4."}},
				Model:      "llama3",
				StopReason: "end_turn",
				Final:      true,
			}
			close(ch)
			return ch, nil
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	result, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_inference", map[string]interface{}{
		"backend": "ollama",
		"model":   "llama3",
		"prompt":  "what is 2+2?",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// The tool returns a JSON string.
	raw, ok := result.(string)
	require.True(t, ok, "expected string result, got %T", result)
	assert.Contains(t, raw, "end_turn")
	assert.Contains(t, raw, "llama3")
	assert.Contains(t, raw, "4.")
}

// TestExecuteOrchestratorInference_NoTenantDefaultsToSystem verifies the
// tenant-fallback migration for the MCP inference tool: when the caller omits
// tenant_id, the tool resolves it to qrdbsdk.SystemTenantID (NOT the retired
// "default" tenant) in both the InferRequest and the request context.
func TestExecuteOrchestratorInference_NoTenantDefaultsToSystem(t *testing.T) {
	t.Parallel()

	var gotReqTenant, gotCtxTenant string
	agentSvc := &mockAgentService{
		inferFn: func(ctx context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			gotReqTenant = req.TenantID
			gotCtxTenant = orchestration.TenantFromContext(ctx)
			ch := make(chan *orchestration.InferChunk, 1)
			ch <- &orchestration.InferChunk{
				Delta:      []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
				StopReason: "end_turn",
				Final:      true,
			}
			close(ch)
			return ch, nil
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	// No tenant_id arg → fallback path.
	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_inference", map[string]interface{}{
		"backend": "ollama",
		"prompt":  "say hello",
	})
	require.NoError(t, err)
	assert.Equal(t, qrdbsdk.SystemTenantID, gotReqTenant,
		"omitted tenant_id must resolve to the system tenant in the request")
	assert.Equal(t, qrdbsdk.SystemTenantID, gotCtxTenant,
		"omitted tenant_id must resolve to the system tenant in the context")
}

func TestExecuteOrchestratorInference_MissingBackend(t *testing.T) {
	t.Parallel()

	reg := newFleetRegistry(t, &mockAgentService{})

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_inference", map[string]interface{}{
		"prompt": "hello",
	})
	require.Error(t, err)
	assert.True(t, isRequiredFieldError(err, "backend"))
}

func TestExecuteOrchestratorInference_MissingPrompt(t *testing.T) {
	t.Parallel()

	reg := newFleetRegistry(t, &mockAgentService{})

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_inference", map[string]interface{}{
		"backend": "ollama",
	})
	require.Error(t, err)
	assert.True(t, isRequiredFieldError(err, "prompt"))
}

func TestExecuteOrchestratorInference_NoAgentService(t *testing.T) {
	t.Parallel()

	// newFleetRegistry with nil agent service
	reg := newFleetRegistry(t, nil)

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_inference", map[string]interface{}{
		"backend": "ollama",
		"prompt":  "hi",
	})
	require.Error(t, err)
}

func TestExecuteOrchestratorInference_InferError(t *testing.T) {
	t.Parallel()

	agentSvc := &mockAgentService{
		inferFn: func(_ context.Context, _ *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			return nil, errors.New("backend unavailable")
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_inference", map[string]interface{}{
		"backend": "ollama",
		"prompt":  "hi",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backend unavailable")
}

func TestExecuteOrchestratorInference_WithMetadata(t *testing.T) {
	t.Parallel()

	var captured *orchestration.InferRequest
	agentSvc := &mockAgentService{
		inferFn: func(_ context.Context, req *orchestration.InferRequest) (<-chan *orchestration.InferChunk, error) {
			captured = req
			ch := make(chan *orchestration.InferChunk, 1)
			ch <- &orchestration.InferChunk{
				Delta: []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
				Final: true,
			}
			close(ch)
			return ch, nil
		},
	}
	reg := newFleetRegistry(t, agentSvc)

	_, err := reg.executeAgentTool(tenantctx.WithTenant(context.Background(), "t1"), "quicbaas_orchestrator_inference", map[string]interface{}{
		"backend":       "ollama",
		"prompt":        "test",
		"system_prompt": "you are a helper",
		"host_affinity": "node-2",
		"metadata":      map[string]interface{}{"key": "val"},
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "you are a helper", captured.SystemPrompt)
	assert.Equal(t, "node-2", captured.HostAffinity)
	assert.Equal(t, "val", captured.Metadata["key"])
}

func TestIsOrchestratorInferenceTool(t *testing.T) {
	t.Parallel()

	assert.True(t, isAgentTool("quicbaas_orchestrator_inference"),
		"quicbaas_orchestrator_inference should be recognized as an agent/orchestrator tool")
}
