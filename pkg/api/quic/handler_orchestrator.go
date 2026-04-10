//go:build quic

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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Orchestrator Service Accessor
// =============================================================================

// orchestratorProvider defines the method to get the agent service.
type orchestratorProvider interface {
	Agent() orchestration.AgentService
}

// getAgentService retrieves the AgentService from the handler's service.
// Returns nil if the service does not implement orchestratorProvider or
// if Agent() returns nil.
func (h *Handler) getAgentService() orchestration.AgentService {
	provider, ok := h.service.(orchestratorProvider)
	if !ok {
		return nil
	}
	return provider.Agent()
}

// =============================================================================
// Orchestrator Dispatch Handler
// =============================================================================

// handleOrchestratorDispatch routes orchestrator operations based on sub-opcode
// in the first byte of the payload. Uses the same dispatch pattern as the
// scheduler handler.
//
// Sub-opcode ranges:
//   - 0x01-0x05: Session operations
//   - 0x06-0x0A: Task operations
//   - 0x0B: Backend listing
//   - 0x0C: Health check
func (h *Handler) handleOrchestratorDispatch(ctx context.Context, req *Request) *Response {
	if len(req.Payload) < 1 {
		return h.errorResponse(api.NewMissingSubOpcodeError(
			"quic.handleOrchestratorDispatch", "orchestrator"))
	}

	subOpcode := req.Payload[0]

	subReq := &Request{
		Opcode:  req.Opcode,
		Token:   req.Token,
		Payload: req.Payload[1:], // Skip sub-opcode byte
	}

	switch subOpcode {
	// Session operations
	case SubOpOrchestratorSpawn:
		return h.handleOrchestratorSpawn(ctx, subReq)
	case SubOpOrchestratorPrompt:
		return h.handleOrchestratorPrompt(ctx, subReq)
	case SubOpOrchestratorSessionStatus:
		return h.handleOrchestratorSessionStatus(ctx, subReq)
	case SubOpOrchestratorCloseSession:
		return h.handleOrchestratorCloseSession(ctx, subReq)
	case SubOpOrchestratorListSessions:
		return h.handleOrchestratorListSessions(ctx, subReq)

	// Task operations
	case SubOpOrchestratorCreateTask:
		return h.handleOrchestratorCreateTask(ctx, subReq)
	case SubOpOrchestratorGetTask:
		return h.handleOrchestratorGetTask(ctx, subReq)
	case SubOpOrchestratorListTasks:
		return h.handleOrchestratorListTasks(ctx, subReq)
	case SubOpOrchestratorClaimTask:
		return h.handleOrchestratorClaimTask(ctx, subReq)
	case SubOpOrchestratorCompleteTask:
		return h.handleOrchestratorCompleteTask(ctx, subReq)

	// Backend operations
	case SubOpOrchestratorListBackends:
		return h.handleOrchestratorListBackends(ctx, subReq)

	// Health
	case SubOpOrchestratorHealth:
		return h.handleOrchestratorHealth(ctx, subReq)

	default:
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleOrchestratorDispatch", "orchestrator", subOpcode))
	}
}

// =============================================================================
// Session Handlers
// =============================================================================

func (h *Handler) handleOrchestratorSpawn(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var spawnReq orchestration.SpawnSessionRequest
	if err := DecodePayload(req.Payload, &spawnReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := agentSvc.SpawnSession(ctx, &spawnReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleOrchestratorPrompt(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var promptReq orchestration.PromptSessionRequest
	if err := DecodePayload(req.Payload, &promptReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := agentSvc.PromptSession(ctx, &promptReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleOrchestratorSessionStatus(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var statusReq orchestration.SessionStatusRequest
	if err := DecodePayload(req.Payload, &statusReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := agentSvc.SessionStatus(ctx, &statusReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleOrchestratorCloseSession(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var closeReq orchestration.CloseSessionRequest
	if err := DecodePayload(req.Payload, &closeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := agentSvc.CloseSession(ctx, &closeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleOrchestratorListSessions(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var listReq orchestration.ListSessionsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := agentSvc.ListSessions(ctx, &listReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Task Handlers
// =============================================================================

// orchestratorGetTaskRequest is used for getting a single task.
type orchestratorGetTaskRequest struct {
	TenantID string `json:"tenant_id" msgpack:"tenant_id"`
	TaskID   string `json:"task_id" msgpack:"task_id"`
}

// orchestratorListTasksRequest is used for listing tasks with filter.
type orchestratorListTasksRequest struct {
	TenantID string                    `json:"tenant_id" msgpack:"tenant_id"`
	Filter   *orchestration.TaskFilter `json:"filter,omitempty" msgpack:"filter,omitempty"`
}

// orchestratorClaimTaskRequest is used for claiming a task.
type orchestratorClaimTaskRequest struct {
	TenantID string `json:"tenant_id" msgpack:"tenant_id"`
	TaskID   string `json:"task_id" msgpack:"task_id"`
	AgentID  string `json:"agent_id" msgpack:"agent_id"`
}

// orchestratorCompleteTaskRequest is used for completing a task.
type orchestratorCompleteTaskRequest struct {
	TenantID string                    `json:"tenant_id" msgpack:"tenant_id"`
	TaskID   string                    `json:"task_id" msgpack:"task_id"`
	Result   *orchestration.TaskResult `json:"result,omitempty" msgpack:"result,omitempty"`
}

// orchestratorSuccessResponse is the response for void task operations.
type orchestratorSuccessResponse struct {
	Success bool `json:"success" msgpack:"success"`
}

func (h *Handler) handleOrchestratorCreateTask(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var createReq orchestration.CreateTaskRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	task, err := agentSvc.CreateTask(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(task)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleOrchestratorGetTask(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var getReq orchestratorGetTaskRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	task, err := agentSvc.GetTask(ctx, getReq.TenantID, getReq.TaskID)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(task)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleOrchestratorListTasks(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var listReq orchestratorListTasksRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	tasks, err := agentSvc.ListTasks(ctx, listReq.TenantID, listReq.Filter)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(tasks)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleOrchestratorClaimTask(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var claimReq orchestratorClaimTaskRequest
	if err := DecodePayload(req.Payload, &claimReq); err != nil {
		return h.errorResponse(err)
	}

	if err := agentSvc.ClaimTask(ctx, claimReq.TenantID, claimReq.TaskID, claimReq.AgentID); err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&orchestratorSuccessResponse{Success: true})
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

func (h *Handler) handleOrchestratorCompleteTask(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var completeReq orchestratorCompleteTaskRequest
	if err := DecodePayload(req.Payload, &completeReq); err != nil {
		return h.errorResponse(err)
	}

	if err := agentSvc.CompleteTask(ctx, completeReq.TenantID, completeReq.TaskID, completeReq.Result); err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(&orchestratorSuccessResponse{Success: true})
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Backend Handlers
// =============================================================================

func (h *Handler) handleOrchestratorListBackends(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	backends, err := agentSvc.ListBackends(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(backends)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Health Handlers
// =============================================================================

func (h *Handler) handleOrchestratorHealth(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	report, err := agentSvc.HealthCheck(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(report)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
