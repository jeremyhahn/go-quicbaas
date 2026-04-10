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

package rest

import (
	"context"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// orchestratorHandler handles Orchestrator REST requests with proper response type conversion.
type orchestratorHandler struct {
	transport *Transport
}

// newOrchestratorHandler creates a new orchestrator handler.
func newOrchestratorHandler(t *Transport) *orchestratorHandler {
	return &orchestratorHandler{transport: t}
}

// isOrchestratorService returns true if the service is an Orchestrator service.
func isOrchestratorService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Orchestrator"
}

// orchestratorMethodHandler defines a handler function for orchestrator methods.
type orchestratorMethodHandler func(ctx context.Context, req interface{}) (interface{}, error)

// handleRequest routes orchestrator requests to appropriate handlers using map-based dispatch.
func (h *orchestratorHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	handlers := map[string]orchestratorMethodHandler{
		"Health":             h.handleHealth,
		"ListBackends":       h.handleListBackends,
		"SpawnSession":       h.handleSpawnSession,
		"PromptSession":      h.handlePromptSession,
		"SessionStatus":      h.handleSessionStatus,
		"CloseSession":       h.handleCloseSession,
		"ListSessions":       h.handleListSessions,
		"CreateTask":         h.handleCreateTask,
		"GetTask":            h.handleGetTask,
		"ListTasks":          h.handleListTasks,
		"ClaimTask":          h.handleClaimTask,
		"CompleteTask":       h.handleCompleteTask,
		"RegisterAgent":      h.handleRegisterAgent,
		"GetAgent":           h.handleGetAgent,
		"ListAgents":         h.handleListAgents,
		"RemoveAgent":        h.handleRemoveAgent,
		"AgentHeartbeat":     h.handleAgentHeartbeat,
		"DecomposeTask":      h.handleDecomposeTask,
		"StartJob":           h.handleStartJob,
		"GetJobStatus":       h.handleGetJobStatus,
		"ListJobs":           h.handleListJobs,
		"CancelJob":          h.handleCancelJob,
		"SubscribeJobEvents": h.handleSubscribeJobEvents,
	}

	handler, ok := handlers[method]
	if !ok {
		return h.transport.requestWithRetry(ctx, service, method, req)
	}

	return handler(ctx, req)
}

// handleHealth handles the Health orchestrator request.
func (h *orchestratorHandler) handleHealth(ctx context.Context, req interface{}) (interface{}, error) {
	var resp orchestrator.HealthCheckReport
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/orchestrator/health", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListBackends handles the ListBackends orchestrator request.
func (h *orchestratorHandler) handleListBackends(ctx context.Context, req interface{}) (interface{}, error) {
	var resp orchestrator.ListBackendsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/orchestrator/backends", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleSpawnSession handles the SpawnSession orchestrator request.
func (h *orchestratorHandler) handleSpawnSession(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SpawnSessionRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.SpawnSession", "invalid request type")
	}
	var resp orchestrator.SpawnSessionResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/orchestrator/sessions", sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handlePromptSession handles the PromptSession orchestrator request.
func (h *orchestratorHandler) handlePromptSession(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.PromptSessionRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.PromptSession", "invalid request type")
	}
	url := "/api/v1/orchestrator/sessions/" + sdkReq.SessionID + "/prompt"
	var resp orchestrator.PromptSessionResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleSessionStatus handles the SessionStatus orchestrator request.
func (h *orchestratorHandler) handleSessionStatus(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SessionStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.SessionStatus", "invalid request type")
	}
	url := "/api/v1/orchestrator/sessions/" + sdkReq.SessionID
	var resp orchestrator.SessionStatusResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleCloseSession handles the CloseSession orchestrator request.
func (h *orchestratorHandler) handleCloseSession(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.CloseSessionRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.CloseSession", "invalid request type")
	}
	url := "/api/v1/orchestrator/sessions/" + sdkReq.SessionID
	var resp orchestrator.CloseSessionResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListSessions handles the ListSessions orchestrator request.
func (h *orchestratorHandler) handleListSessions(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListSessionsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListSessions", "invalid request type")
	}
	url := "/api/v1/orchestrator/sessions"
	sep := "?"
	if sdkReq.Backend != "" {
		url += sep + "backend=" + sdkReq.Backend
		sep = "&"
	}
	if sdkReq.Status != "" {
		url += sep + "status=" + sdkReq.Status
	}
	var resp orchestrator.ListSessionsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleCreateTask handles the CreateTask orchestrator request.
func (h *orchestratorHandler) handleCreateTask(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.CreateTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.CreateTask", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.Task
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGetTask handles the GetTask orchestrator request.
func (h *orchestratorHandler) handleGetTask(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetTask", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + sdkReq.TaskID + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.Task
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListTasks handles the ListTasks orchestrator request.
func (h *orchestratorHandler) handleListTasks(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListTasksRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListTasks", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks?tenant_id=" + sdkReq.TenantID
	if sdkReq.Status != "" {
		url += "&status=" + sdkReq.Status
	}
	if sdkReq.TeamName != "" {
		url += "&team_name=" + sdkReq.TeamName
	}
	if sdkReq.AssignedTo != "" {
		url += "&assigned_to=" + sdkReq.AssignedTo
	}
	var resp orchestrator.ListTasksResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleClaimTask handles the ClaimTask orchestrator request.
func (h *orchestratorHandler) handleClaimTask(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ClaimTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ClaimTask", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + sdkReq.TaskID + "/claim?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.ClaimTaskResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleCompleteTask handles the CompleteTask orchestrator request.
func (h *orchestratorHandler) handleCompleteTask(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.CompleteTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.CompleteTask", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + sdkReq.TaskID + "/complete?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.CompleteTaskResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleRegisterAgent handles the RegisterAgent orchestrator request.
func (h *orchestratorHandler) handleRegisterAgent(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.RegisterAgentRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.RegisterAgent", "invalid request type")
	}
	url := "/api/v1/orchestrator/agents?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.RegisterAgentResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGetAgent handles the GetAgent orchestrator request.
func (h *orchestratorHandler) handleGetAgent(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetAgentRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetAgent", "invalid request type")
	}
	url := "/api/v1/orchestrator/agents/" + sdkReq.AgentID + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.AgentRegistration
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListAgents handles the ListAgents orchestrator request.
func (h *orchestratorHandler) handleListAgents(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListAgentsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListAgents", "invalid request type")
	}
	url := "/api/v1/orchestrator/agents?tenant_id=" + sdkReq.TenantID
	if sdkReq.Host != "" {
		url += "&host=" + sdkReq.Host
	}
	if sdkReq.Status != "" {
		url += "&status=" + sdkReq.Status
	}
	if sdkReq.AgentType != "" {
		url += "&agent_type=" + sdkReq.AgentType
	}
	if sdkReq.Backend != "" {
		url += "&backend=" + sdkReq.Backend
	}
	var resp orchestrator.ListAgentsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleRemoveAgent handles the RemoveAgent orchestrator request.
func (h *orchestratorHandler) handleRemoveAgent(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.RemoveAgentRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.RemoveAgent", "invalid request type")
	}
	url := "/api/v1/orchestrator/agents/" + sdkReq.AgentID + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.RemoveAgentResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleAgentHeartbeat handles the AgentHeartbeat orchestrator request.
func (h *orchestratorHandler) handleAgentHeartbeat(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.AgentHeartbeatRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.AgentHeartbeat", "invalid request type")
	}
	url := "/api/v1/orchestrator/agents/" + sdkReq.AgentID + "/heartbeat?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.AgentHeartbeatResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleDecomposeTask handles the DecomposeTask orchestrator request.
func (h *orchestratorHandler) handleDecomposeTask(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.DecomposeTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.DecomposeTask", "invalid request type")
	}
	var resp orchestrator.DecomposeTaskResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/orchestrator/tasks/decompose", sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// =============================================================================
// Job Operations
// =============================================================================

// handleStartJob handles the StartJob orchestrator request.
func (h *orchestratorHandler) handleStartJob(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.StartJobRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.StartJob", "invalid request type")
	}
	var resp orchestrator.StartJobResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/orchestrator/jobs", sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGetJobStatus handles the GetJobStatus orchestrator request.
func (h *orchestratorHandler) handleGetJobStatus(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetJobStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetJobStatus", "invalid request type")
	}
	url := "/api/v1/orchestrator/jobs/" + sdkReq.JobID + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.JobStatus
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListJobs handles the ListJobs orchestrator request.
func (h *orchestratorHandler) handleListJobs(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListJobsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListJobs", "invalid request type")
	}
	url := "/api/v1/orchestrator/jobs?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.ListJobsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleCancelJob handles the CancelJob orchestrator request.
func (h *orchestratorHandler) handleCancelJob(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.CancelJobRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.CancelJob", "invalid request type")
	}
	url := "/api/v1/orchestrator/jobs/" + sdkReq.JobID + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.CancelJobResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleSubscribeJobEvents returns a stream-not-supported error because
// REST transport does not support server-streaming RPCs via the unary handler.
// Clients should use the gRPC or embedded transport for streaming operations.
func (h *orchestratorHandler) handleSubscribeJobEvents(ctx context.Context, req interface{}) (interface{}, error) {
	return nil, errors.NewUnimplemented("rest.orchestrator.SubscribeJobEvents",
		"streaming not supported over REST transport; use gRPC transport")
}
