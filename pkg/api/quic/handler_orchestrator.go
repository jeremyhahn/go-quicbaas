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
	"encoding/json"

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
//   - 0x0D: Stateless inference
//   - 0x20: Session pooled-account rotation (authorizes as orchestrator:spawn)
//   - 0x30-0x38: Pooled provider accounts (handler_account.go)
//   - 0x40-0x47: Projects (handler_project.go)
//   - 0x50-0x5D: SSH (handler_ssh.go)
//   - 0x60-0x62: Certificate extensions (handler_certificate_extensions.go)
//   - 0x70-0x72: Attestation (handler_attestation.go)
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

	// Inject the tenant ID from the wire payload into the context so that
	// every sub-handler and business-logic call that uses
	// orchestration.TenantFromContext(ctx) receives the caller's tenant scope.
	// This mirrors what the REST, gRPC, and MCP handlers do by calling
	// orchestration.ContextWithTenant(ctx, tenantID) before invoking services.
	if tenantID := peekTenantID(subReq.Payload); tenantID != "" {
		ctx = orchestration.ContextWithTenant(ctx, tenantID)
	}

	// Pooled provider account sub-ops share this dispatch opcode in a
	// disjoint 0x30-0x38 range (see protocol.go). They are routed to their
	// own O(1) table before the orchestrator switch so the two vocabularies
	// stay separate — including for authorization, which resolves the
	// account range against credentials:* rather than orchestrator:*.
	if isAccountSubOp(subOpcode) {
		return h.handleAccountDispatch(ctx, subOpcode, subReq)
	}

	// Project sub-ops share this dispatch opcode in a disjoint 0x40-0x47
	// range (see protocol.go), routed to their own O(1) table for the same
	// reason as accounts: they authorize against project:* rather than
	// orchestrator:*.
	if isProjectSubOp(subOpcode) {
		return h.handleProjectDispatch(ctx, subOpcode, subReq)
	}

	// Certificate extension sub-ops share this dispatch opcode in a disjoint
	// 0x60-0x62 range (see protocol.go). They authorize against ca:create /
	// certs:generate rather than orchestrator:*, so they get their own table
	// for the same reason accounts and projects do.
	if isCertExtSubOp(subOpcode) {
		return h.handleCertExtDispatch(ctx, subOpcode, subReq)
	}

	// Attestation sub-ops share this dispatch opcode in a disjoint
	// 0x70-0x72 range (see protocol.go). They authorize against
	// attestation:verify / attestation:attest rather than orchestrator:*,
	// so they get their own table for the same reason accounts, projects
	// and certificate extensions do.
	if isAttestationSubOp(subOpcode) {
		return h.handleAttestationDispatch(ctx, subOpcode, subReq)
	}

	// PIV sub-ops share this dispatch opcode in a disjoint 0x80-0x87 range
	// (see protocol.go). They authorize against the piv:* family rather
	// than orchestrator:*, so they get their own table for the same reason
	// as the other security surfaces.
	if isPIVSubOp(subOpcode) {
		return h.handlePIVDispatch(ctx, subOpcode, subReq)
	}

	// FIDO2 sub-ops share this dispatch opcode in a disjoint 0x90-0x93
	// range (see protocol.go). Registration authorizes against
	// user_authfactors:add; the authentication ceremony is pre-auth (see
	// isPublicSubOperation in handler_fido2.go).
	if isFIDO2SubOp(subOpcode) {
		return h.handleFIDO2Dispatch(ctx, subOpcode, subReq)
	}

	// SSH sub-ops share this dispatch opcode in a disjoint 0x50-0x5D range
	// (see protocol.go), routed to their own O(1) table for the same reason
	// as accounts and projects: they authorize against keys:* rather than
	// orchestrator:*.
	if isSSHSubOp(subOpcode) {
		return h.handleSSHDispatch(ctx, subOpcode, subReq)
	}

	// Session account rotation shares this dispatch opcode at 0x20, outside the
	// contiguous orchestrator range, because it is a write that must resolve
	// orchestrator:spawn rather than the surface-level orchestrator:status the
	// rest of the range authorizes with. It is still an orchestrator verb, so
	// it dispatches here rather than into a separate table.
	switch subOpcode {
	case SubOpOrchestratorRotateAccount:
		return h.handleOrchestratorRotateAccount(ctx, subReq)

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

	// Inference
	case SubOpOrchestratorInference:
		return h.handleOrchestratorInference(ctx, subReq)

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

// handleOrchestratorPrompt dispatches a prompt to an agent session.
//
// A prompt naming a session is asynchronous: the orchestrator records the
// prompt and a turn_started marker on the session event log, runs the turn on
// a context detached from this stream, and answers with the turn ID. The
// agent's output is read back over the session event stream, and the turn ends
// with a turn_completed or turn_failed event carrying that ID.
//
// A prompt with no session is a stateless one-shot: it has no event log to
// publish to, so it stays synchronous and returns the completed turn inline.
func (h *Handler) handleOrchestratorPrompt(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var promptReq orchestration.PromptSessionRequest
	if err := DecodePayload(req.Payload, &promptReq); err != nil {
		return h.errorResponse(err)
	}

	if promptReq.SessionID != "" {
		ack, ackErr := agentSvc.PromptSessionAsync(ctx, &promptReq)
		if ackErr != nil {
			return h.errorResponse(ackErr)
		}
		payload, encErr := EncodePayload(ack)
		if encErr != nil {
			return h.errorResponse(encErr)
		}
		return &Response{Status: StatusOK, Payload: payload}
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

// handleOrchestratorRotateAccount moves a session onto a different pooled
// provider account. The same verb runs automatically when a turn fails on a
// classified provider limit; this is the operator's door to it.
func (h *Handler) handleOrchestratorRotateAccount(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	rotator, err := orchestration.RotationServiceFrom(agentSvc, "quic")
	if err != nil {
		return h.errorResponse(err)
	}

	var rotateReq orchestration.RotateSessionAccountRequest
	if err := DecodePayload(req.Payload, &rotateReq); err != nil {
		return h.errorResponse(err)
	}

	result, err := rotator.RotateSessionAccount(ctx, &rotateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(result)
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
// Inference Handler
// =============================================================================

// orchestratorInferenceRequest is the QUIC wire type for an inference call.
type orchestratorInferenceRequest struct {
	Backend      string            `json:"backend" msgpack:"backend"`
	Model        string            `json:"model,omitempty" msgpack:"model,omitempty"`
	Prompt       string            `json:"prompt" msgpack:"prompt"`
	SystemPrompt string            `json:"system_prompt,omitempty" msgpack:"system_prompt,omitempty"`
	HostAffinity string            `json:"host_affinity,omitempty" msgpack:"host_affinity,omitempty"`
	GpuRequired  bool              `json:"gpu_required,omitempty" msgpack:"gpu_required,omitempty"`
	TenantID     string            `json:"tenant_id,omitempty" msgpack:"tenant_id,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty" msgpack:"metadata,omitempty"`
}

// handleOrchestratorInference handles the stateless inference sub-opcode. It
// calls agentSvc.Infer, accumulates the chunk channel, and encodes the
// aggregated InferResponse as the QUIC payload.
func (h *Handler) handleOrchestratorInference(ctx context.Context, req *Request) *Response {
	agentSvc := h.getAgentService()
	if agentSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Orchestrator"))
	}

	var wireReq orchestratorInferenceRequest
	if err := DecodePayload(req.Payload, &wireReq); err != nil {
		return h.errorResponse(err)
	}

	inferReq := &orchestration.InferRequest{
		Backend:      wireReq.Backend,
		Model:        wireReq.Model,
		Prompt:       wireReq.Prompt,
		SystemPrompt: wireReq.SystemPrompt,
		TenantID:     wireReq.TenantID,
		HostAffinity: wireReq.HostAffinity,
		GpuRequired:  wireReq.GpuRequired,
		Metadata:     wireReq.Metadata,
	}
	if wireReq.HostAffinity != "" {
		ctx = orchestration.WithHostAffinity(ctx, wireReq.HostAffinity)
	}
	if wireReq.GpuRequired {
		ctx = orchestration.WithGpuRequired(ctx)
	}

	chunks, err := agentSvc.Infer(ctx, inferReq)
	if err != nil {
		return h.errorResponse(err)
	}

	resp, err := orchestration.AccumulateChunks(chunks)
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
// Tenant context helper
// =============================================================================

// peekTenantID extracts the "tenant_id" field from a JSON object payload
// without fully decoding the message. Returns an empty string if the payload
// is not a JSON object or if the field is absent or empty.
func peekTenantID(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}

	var probe struct {
		TenantID string `json:"tenant_id"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return ""
	}

	return probe.TenantID
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
