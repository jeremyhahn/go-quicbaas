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
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
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
		"Health":                 h.handleHealth,
		"ListBackends":           h.handleListBackends,
		"ListRunners":            h.handleListRunners,
		"ListBackendModels":      h.handleListBackendModels,
		"SpawnSession":           h.handleSpawnSession,
		"PromptSession":          h.handlePromptSession,
		"SessionStatus":          h.handleSessionStatus,
		"CloseSession":           h.handleCloseSession,
		"RotateSessionAccount":   h.handleRotateSessionAccount,
		"ListSessions":           h.handleListSessions,
		"GetSessionConversation": h.handleGetSessionConversation,
		"GetSpendReport":         h.handleGetSpendReport,
		"GetTenantSpendPolicy":   h.handleGetTenantSpendPolicy,
		"SetTenantSpendPolicy":   h.handleSetTenantSpendPolicy,
		"CreateTask":             h.handleCreateTask,
		"GetTask":                h.handleGetTask,
		"ListTasks":              h.handleListTasks,
		"ClaimTask":              h.handleClaimTask,
		"CompleteTask":           h.handleCompleteTask,
		"BlockTask":              h.handleBlockTask,
		"DeleteTask":             h.handleDeleteTask,
		"ReadmitTask":            h.handleReadmitTask,
		"AttentionList":          h.handleAttentionList,
		"AttentionResolve":       h.handleAttentionResolve,
		"RegisterFleetNode":      h.handleRegisterFleetNode,
		"GetFleetNode":           h.handleGetFleetNode,
		"ListFleetNodes":         h.handleListFleetNodes,
		"RemoveFleetNode":        h.handleRemoveFleetNode,
		"FleetNodeHeartbeat":     h.handleFleetNodeHeartbeat,
		"DecomposeTask":          h.handleDecomposeTask,
		"SpawnChildJob":          h.handleSpawnChildJob,
		"StartJob":               h.handleStartJob,
		"GetJobStatus":           h.handleGetJobStatus,
		"ListJobs":               h.handleListJobs,
		"CancelJob":              h.handleCancelJob,
		"SubscribeJobEvents":     h.handleSubscribeJobEvents,
		"Inference":              h.handleInference,
		"ListWorkflows":          h.handleListWorkflows,
		"DisableFleetNode":       h.handleDisableFleetNode,
		"EnableFleetNode":        h.handleEnableFleetNode,
		"SubmitTaskArtifacts":    h.handleSubmitTaskArtifacts,
		"GetTaskArtifact":        h.handleGetTaskArtifact,
		"ListTaskArtifacts":      h.handleListTaskArtifacts,
		"GetCostSettings":        h.handleGetCostSettings,
		"SetCostSettings":        h.handleSetCostSettings,
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
	sdkReq, _ := req.(*orchestrator.ListBackendsRequest)
	query := neturl.Values{}
	if tenantID := transport.TenantFromContext(ctx); tenantID != "" {
		query.Set("tenant_id", tenantID)
	}
	if sdkReq != nil {
		setPageQuery(query, sdkReq.Params)
	}
	url := "/api/v1/orchestrator/backends"
	if encoded := query.Encode(); encoded != "" {
		url += "?" + encoded
	}
	var resp orchestrator.ListBackendsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListRunners handles the ListRunners orchestrator request: every
// registered job runner and the states it declares.
func (h *orchestratorHandler) handleListRunners(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, _ := req.(*orchestrator.ListRunnersRequest)
	url := "/api/v1/orchestrator/runners"
	if sdkReq != nil {
		query := neturl.Values{}
		setPageQuery(query, sdkReq.Params)
		if encoded := query.Encode(); encoded != "" {
			url += "?" + encoded
		}
	}
	var resp orchestrator.ListJobRunnersResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListBackendModels handles the ListBackendModels orchestrator request.
// The REST endpoint is keyed by both agent ID and backend name; the agent
// segment is replaced with "any" when the caller did not supply one so the
// server-side router can skip host affinity.
func (h *orchestratorHandler) handleListBackendModels(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListBackendModelsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListBackendModels", "invalid request type")
	}
	if sdkReq.BackendName == "" {
		return nil, errors.NewInvalidInput("orchestrator.ListBackendModels", "backend name is required")
	}
	agentID := sdkReq.NodeID
	if agentID == "" {
		agentID = "any"
	}
	url := "/api/v1/fleet/" + agentID + "/backends/" + sdkReq.BackendName + "/models"
	if sdkReq.TenantID != "" {
		url += "?tenant_id=" + sdkReq.TenantID
	}
	var resp orchestrator.ListBackendModelsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Models == nil {
		resp.Models = []string{}
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
	var wire orchestrator.PromptSessionWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &wire); err != nil {
		return nil, err
	}
	return wire.Response()
}

// handleSessionStatus handles the SessionStatus orchestrator request.
func (h *orchestratorHandler) handleSessionStatus(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SessionStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.SessionStatus", "invalid request type")
	}
	url := "/api/v1/orchestrator/sessions/" + sdkReq.SessionID
	var wire orchestrator.SessionStatusWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &wire); err != nil {
		return nil, err
	}
	return wire.Response(), nil
}

// handleGetSessionConversation handles the GetSessionConversation request,
// which maps to GET /api/v1/orchestrator/sessions/{id}/history.
//
// The route says "history" and the method says "conversation" because the SDK
// already has a GetSessionHistory that pages the raw EVENT log: the path names
// the resource an operator reaches for, the method names the shape it returns.
func (h *orchestratorHandler) handleGetSessionConversation(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetSessionConversationRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetSessionConversation", "invalid request type")
	}
	if sdkReq.SessionID == "" {
		return nil, errors.NewInvalidInput("orchestrator.GetSessionConversation", "session_id is required")
	}
	params := neturl.Values{}
	if sdkReq.TenantID != "" {
		params.Set("tenant_id", sdkReq.TenantID)
	}
	if sdkReq.Cursor > 0 {
		params.Set("cursor", strconv.FormatUint(sdkReq.Cursor, 10))
	}
	if sdkReq.Limit > 0 {
		params.Set("limit", strconv.Itoa(sdkReq.Limit))
	}
	path := "/api/v1/orchestrator/sessions/" + neturl.PathEscape(sdkReq.SessionID) + "/history"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var resp orchestrator.GetSessionConversationResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGetSpendReport handles the GetSpendReport request, which maps to
// GET /api/v1/orchestrator/cost.
//
// The scope travels as query parameters rather than as a path, because the
// ledger's attribution chain is a PATH the caller supplies contiguously and
// one route over one prefix scan is what keeps a drill-down's arithmetic
// agreeing with itself at every depth.
func (h *orchestratorHandler) handleGetSpendReport(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetSpendReportRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetSpendReport", "invalid request type")
	}
	params := neturl.Values{}
	for key, value := range map[string]string{
		"tenant_id":  sdkReq.TenantID,
		"project_id": sdkReq.ProjectID,
		"task_id":    sdkReq.TaskID,
		"job_id":     sdkReq.JobID,
		"session_id": sdkReq.SessionID,
		"host":       sdkReq.Host,
		"agent":      sdkReq.Agent,
	} {
		if value != "" {
			params.Set(key, value)
		}
	}
	// Presence, not value: round 0 is a real level, so an unset RoundSet must
	// leave the parameter off entirely rather than send "0".
	if sdkReq.RoundSet {
		params.Set("round", strconv.Itoa(sdkReq.Round))
	}
	if sdkReq.From != nil {
		params.Set("from", sdkReq.From.UTC().Format(time.RFC3339Nano))
	}
	if sdkReq.To != nil {
		params.Set("to", sdkReq.To.UTC().Format(time.RFC3339Nano))
	}
	path := "/api/v1/orchestrator/cost"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var resp orchestrator.GetSpendReportResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGetTenantSpendPolicy handles GET /api/v1/orchestrator/spend-policy.
func (h *orchestratorHandler) handleGetTenantSpendPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetTenantSpendPolicyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetTenantSpendPolicy", "invalid request type")
	}
	path := tenantSpendPolicyPath(sdkReq.TenantID)
	var resp orchestrator.TenantSpendPolicy
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleSetTenantSpendPolicy handles PUT /api/v1/orchestrator/spend-policy.
func (h *orchestratorHandler) handleSetTenantSpendPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SetTenantSpendPolicyRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.SetTenantSpendPolicy", "invalid request type")
	}
	path := tenantSpendPolicyPath(sdkReq.TenantID)
	body := struct {
		Limits spend.Limits `json:"limits"`
	}{Limits: sdkReq.Limits}
	var resp orchestrator.TenantSpendPolicy
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, path, &body, &resp); err != nil {
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
	var wire orchestrator.CloseSessionWire
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, url, nil, &wire); err != nil {
		return nil, err
	}
	return wire.Response(), nil
}

// handleRotateSessionAccount handles the RotateSessionAccount orchestrator
// request. The session is addressed by path segment and the rotation options
// travel in the body, matching the server route registered in
// pkg/api/rest/handlers_orchestrator.go.
func (h *orchestratorHandler) handleRotateSessionAccount(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.RotateSessionAccountRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.RotateSessionAccount", "invalid request type")
	}
	if sdkReq.SessionID == "" {
		return nil, errors.NewInvalidInput("orchestrator.RotateSessionAccount", "session ID is required")
	}
	url := "/api/v1/orchestrator/sessions/" + sdkReq.SessionID + "/rotate-account"
	var resp orchestrator.RotateSessionAccountResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
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
	// THE TENANT TRAVELS ON THE CONTEXT AND THE SERVER READS IT FROM THE QUERY.
	// resolveOrchestratorTenant falls back to the authenticated tenant and then
	// to the system tenant when ?tenant_id= is absent, so omitting it did not
	// fail — it quietly listed a DIFFERENT tenant's sessions than the one the
	// caller scoped the context to, and answered the gRPC caller and the REST
	// caller differently for the identical request.
	//
	// Built with url.Values rather than concatenation so every value is
	// escaped: a backend name carrying an & or a = silently truncated the
	// query, and the status filter behind it went missing.
	query := neturl.Values{}
	if tenantID := transport.TenantFromContext(ctx); tenantID != "" {
		query.Set("tenant_id", tenantID)
	}
	if sdkReq.Backend != "" {
		query.Set("backend", sdkReq.Backend)
	}
	if sdkReq.Status != "" {
		query.Set("status", sdkReq.Status)
	}
	if sdkReq.ProjectID != "" {
		query.Set("project_id", sdkReq.ProjectID)
	}
	if sdkReq.JobID != "" {
		query.Set("job_id", sdkReq.JobID)
	}
	if sdkReq.Runner != "" {
		query.Set("runner", sdkReq.Runner)
	}
	if sdkReq.AgentID != "" {
		query.Set("agent_id", sdkReq.AgentID)
	}
	setPageQuery(query, sdkReq.Params)
	url := "/api/v1/orchestrator/sessions"
	if encoded := query.Encode(); encoded != "" {
		url += "?" + encoded
	}
	var wire orchestrator.ListSessionsWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &wire); err != nil {
		return nil, err
	}
	return wire.Response(), nil
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
	if sdkReq.ProjectID != "" {
		url += "&project_id=" + neturl.QueryEscape(sdkReq.ProjectID)
	}
	if len(sdkReq.Statuses) > 0 {
		url += "&statuses=" + neturl.QueryEscape(strings.Join(sdkReq.Statuses, ","))
	}
	if sdkReq.AutoSpawn != nil {
		url += "&auto_spawn=" + strconv.FormatBool(*sdkReq.AutoSpawn)
	}
	if sdkReq.AgentID != "" {
		url += "&agent_id=" + neturl.QueryEscape(sdkReq.AgentID)
	}
	if sdkReq.JobID != "" {
		url += "&job_id=" + neturl.QueryEscape(sdkReq.JobID)
	}
	url += pageQuerySuffix(sdkReq.Params)
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

// handleBlockTask handles the BlockTask orchestrator request.
func (h *orchestratorHandler) handleBlockTask(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.BlockTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.BlockTask", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + sdkReq.TaskID + "/block?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.BlockTaskResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleReadmitTask handles the ReadmitTask orchestrator request.
func (h *orchestratorHandler) handleReadmitTask(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ReadmitTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ReadmitTask", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + sdkReq.TaskID + "/readmit?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.ReadmitTaskResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleAttentionList handles the AttentionList orchestrator request.
func (h *orchestratorHandler) handleAttentionList(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.AttentionListRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.AttentionList", "invalid request type")
	}
	url := "/api/v1/orchestrator/attention?tenant_id=" + sdkReq.TenantID
	if sdkReq.ProjectID != "" {
		url += "&project_id=" + neturl.QueryEscape(sdkReq.ProjectID)
	}
	if sdkReq.Kind != "" {
		url += "&kind=" + neturl.QueryEscape(sdkReq.Kind)
	}
	url += pageQuerySuffix(sdkReq.Params)
	var resp orchestrator.AttentionListResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleAttentionResolve handles the AttentionResolve orchestrator request.
func (h *orchestratorHandler) handleAttentionResolve(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.AttentionResolveRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.AttentionResolve", "invalid request type")
	}
	url := "/api/v1/orchestrator/attention/" + sdkReq.TaskID + "/resolve?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.AttentionItem
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleDeleteTask handles the DeleteTask orchestrator request.
func (h *orchestratorHandler) handleDeleteTask(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.DeleteTaskRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.DeleteTask", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + sdkReq.TaskID + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.DeleteTaskResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, url, nil, &resp); err != nil {
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

// handleRegisterFleetNode handles the RegisterFleetNode orchestrator request.
func (h *orchestratorHandler) handleRegisterFleetNode(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.RegisterFleetNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.RegisterFleetNode", "invalid request type")
	}
	url := "/api/v1/fleet?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.RegisterFleetNodeResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGetFleetNode handles the GetFleetNode orchestrator request.
func (h *orchestratorHandler) handleGetFleetNode(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetFleetNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetFleetNode", "invalid request type")
	}
	url := "/api/v1/fleet/" + sdkReq.NodeID + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.FleetNode
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListFleetNodes handles the ListFleetNodes orchestrator request.
func (h *orchestratorHandler) handleListFleetNodes(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListFleetNodesRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListFleetNodes", "invalid request type")
	}
	url := "/api/v1/fleet?tenant_id=" + sdkReq.TenantID
	if sdkReq.Host != "" {
		url += "&host=" + sdkReq.Host
	}
	if sdkReq.Status != "" {
		url += "&status=" + sdkReq.Status
	}
	if sdkReq.NodeType != "" {
		url += "&node_type=" + sdkReq.NodeType
	}
	if sdkReq.Backend != "" {
		url += "&backend=" + neturl.QueryEscape(sdkReq.Backend)
	}
	url += pageQuerySuffix(sdkReq.Params)
	var resp orchestrator.ListFleetNodesResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleRemoveFleetNode handles the RemoveFleetNode orchestrator request.
func (h *orchestratorHandler) handleRemoveFleetNode(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.RemoveFleetNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.RemoveFleetNode", "invalid request type")
	}
	url := "/api/v1/fleet/" + sdkReq.NodeID + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.RemoveFleetNodeResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleFleetNodeHeartbeat handles the FleetNodeHeartbeat orchestrator request.
func (h *orchestratorHandler) handleFleetNodeHeartbeat(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.FleetNodeHeartbeatRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.FleetNodeHeartbeat", "invalid request type")
	}
	url := "/api/v1/fleet/" + sdkReq.NodeID + "/heartbeat?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.FleetNodeHeartbeatResponse
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

// handleSpawnChildJob handles the SpawnChildJob orchestrator request.
func (h *orchestratorHandler) handleSpawnChildJob(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SpawnChildJobRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.SpawnChildJob", "invalid request type")
	}
	url := "/api/v1/orchestrator/jobs/" + sdkReq.ParentJobID + "/children"
	var resp orchestrator.SpawnChildJobResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

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
	url := "/api/v1/orchestrator/jobs?tenant_id=" + neturl.QueryEscape(sdkReq.TenantID)
	if sdkReq.TaskID != "" {
		url += "&task_id=" + neturl.QueryEscape(sdkReq.TaskID)
	}
	if sdkReq.Runner != "" {
		url += "&runner=" + neturl.QueryEscape(sdkReq.Runner)
	}
	if sdkReq.ProjectID != "" {
		url += "&project_id=" + neturl.QueryEscape(sdkReq.ProjectID)
	}
	if len(sdkReq.Statuses) > 0 {
		url += "&statuses=" + neturl.QueryEscape(strings.Join(sdkReq.Statuses, ","))
	}
	if len(sdkReq.JobIDs) > 0 {
		url += "&job_ids=" + neturl.QueryEscape(strings.Join(sdkReq.JobIDs, ","))
	}
	if sdkReq.AgentID != "" {
		url += "&agent_id=" + neturl.QueryEscape(sdkReq.AgentID)
	}
	url += pageQuerySuffix(sdkReq.Params)
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

// handleDisableFleetNode handles POST /api/v1/agents/{id}/disable.
func (h *orchestratorHandler) handleListWorkflows(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListWorkflowsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListWorkflows", "invalid request type")
	}
	url := "/api/v1/orchestrator/workflows?tenant_id=" + neturl.QueryEscape(sdkReq.TenantID)
	if sdkReq.ProjectID != "" {
		url += "&project_id=" + neturl.QueryEscape(sdkReq.ProjectID)
	}
	if sdkReq.Mode != "" {
		url += "&mode=" + neturl.QueryEscape(string(sdkReq.Mode))
	}
	url += pageQuerySuffix(sdkReq.Params)
	var resp orchestrator.ListWorkflowsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (h *orchestratorHandler) handleDisableFleetNode(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.DisableFleetNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.DisableFleetNode", "invalid request type")
	}
	url := "/api/v1/agents/" + sdkReq.NodeID + "/disable?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.DisableFleetNodeResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleEnableFleetNode handles POST /api/v1/agents/{id}/enable.
func (h *orchestratorHandler) handleEnableFleetNode(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.EnableFleetNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.EnableFleetNode", "invalid request type")
	}
	url := "/api/v1/agents/" + sdkReq.NodeID + "/enable?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.EnableFleetNodeResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleSubmitTaskArtifacts handles POST /api/v1/orchestrator/tasks/{taskID}/artifacts.
func (h *orchestratorHandler) handleSubmitTaskArtifacts(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.SubmitTaskArtifactsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.SubmitTaskArtifacts", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + sdkReq.TaskID + "/artifacts?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.SubmitTaskArtifactsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, url, sdkReq.Artifacts, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleGetTaskArtifact handles GET /api/v1/orchestrator/tasks/{taskID}/artifacts/{name}.
func (h *orchestratorHandler) handleGetTaskArtifact(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.GetTaskArtifactRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.GetTaskArtifact", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + sdkReq.TaskID + "/artifacts/" + sdkReq.Name + "?tenant_id=" + sdkReq.TenantID
	var resp orchestrator.TaskArtifact
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleListTaskArtifacts handles GET /api/v1/orchestrator/tasks/{taskID}/artifacts.
func (h *orchestratorHandler) handleListTaskArtifacts(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.ListTaskArtifactsRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.ListTaskArtifacts", "invalid request type")
	}
	url := "/api/v1/orchestrator/tasks/" + neturl.PathEscape(sdkReq.TaskID) + "/artifacts?tenant_id=" + neturl.QueryEscape(sdkReq.TenantID)
	url += pageQuerySuffix(sdkReq.Params)
	var resp orchestrator.ListTaskArtifactsResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, url, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// handleInference handles the Inference orchestrator request — a stateless
// fleet-routed prompt. The REST endpoint accepts {backend, model, prompt,
// system_prompt, host_affinity, metadata, stream=false} and returns an
// aggregated response with {content, model, stop_reason}.
func (h *orchestratorHandler) handleInference(ctx context.Context, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*orchestrator.InferenceRequest)
	if !ok {
		return nil, errors.NewInvalidInput("orchestrator.Inference", "invalid request type")
	}
	wire := struct {
		Backend      string            `json:"backend"`
		Model        string            `json:"model,omitempty"`
		Prompt       string            `json:"prompt"`
		SystemPrompt string            `json:"system_prompt,omitempty"`
		HostAffinity string            `json:"host_affinity,omitempty"`
		Metadata     map[string]string `json:"metadata,omitempty"`
		Stream       bool              `json:"stream"`
	}{
		Backend:      sdkReq.Backend,
		Model:        sdkReq.Model,
		Prompt:       sdkReq.Prompt,
		SystemPrompt: sdkReq.SystemPrompt,
		HostAffinity: sdkReq.HostAffinity,
		Metadata:     sdkReq.Metadata,
		Stream:       false,
	}
	var resp orchestrator.InferenceResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/orchestrator/inference", &wire, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// tenantSpendPolicyPath is the spend-policy route, scoped to tenantID when the
// caller named one; otherwise the server takes the authenticated principal's.
func tenantSpendPolicyPath(tenantID string) string {
	path := "/api/v1/orchestrator/spend-policy"
	if tenantID != "" {
		path += "?" + neturl.Values{"tenant_id": {tenantID}}.Encode()
	}
	return path
}
