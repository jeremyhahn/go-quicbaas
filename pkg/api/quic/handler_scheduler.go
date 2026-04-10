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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Scheduler Service Interface
// =============================================================================

// schedulerProviderInterface defines the method to get the scheduler service.
// The QuicBaaS service implements this interface via Scheduler() method.
type schedulerProviderInterface interface {
	Scheduler() service.SchedulerService
}

// getSchedulerService retrieves the scheduler service from the handler's service.
// Returns nil if the service doesn't implement schedulerProviderInterface or
// if Scheduler() returns nil (scheduler not configured).
func (h *Handler) getSchedulerService() service.SchedulerService {
	provider, ok := h.service.(schedulerProviderInterface)
	if !ok {
		return nil
	}
	return provider.Scheduler()
}

// =============================================================================
// Scheduler Dispatch Handler
// =============================================================================

// handleSchedulerDispatch routes scheduler operations based on sub-opcode in payload.
// The scheduler uses a single opcode (OpScheduler = 0x00) with sub-operation
// dispatch via the first byte of the payload.
//
// Sub-opcode ranges:
//   - 0x01-0x1F: Scheduler operations
//   - 0x20-0x2F: Messaging operations (delegated to handleMsgDispatch)
func (h *Handler) handleSchedulerDispatch(ctx context.Context, req *Request) *Response {
	// Payload must contain at least sub-opcode byte
	if len(req.Payload) < 1 {
		return h.errorResponse(api.NewMissingSubOpcodeError(
			"quic.handleSchedulerDispatch", "scheduler"))
	}

	// Extract sub-opcode
	subOpcode := req.Payload[0]

	// Check if sub-opcode is in messaging range (0x20-0x2F)
	// Delegate to messaging handler for messaging operations
	if subOpcode >= 0x20 && subOpcode <= 0x2F {
		return h.handleMsgDispatch(ctx, req)
	}

	// Check if sub-opcode is in vector extended range (0x40-0x4F)
	// Delegate to vector extended handler for extended vector operations
	if subOpcode >= 0x40 && subOpcode <= 0x4F {
		return h.handleVectorExtendedDispatch(ctx, req)
	}

	// Handle scheduler operations
	subReq := &Request{
		Opcode:  req.Opcode,
		Token:   req.Token,
		Payload: req.Payload[1:], // Skip sub-opcode byte
	}

	switch subOpcode {
	case SubOpSchedulerCreateJob:
		return h.handleSchedulerCreateJob(ctx, subReq)
	case SubOpSchedulerGetJob:
		return h.handleSchedulerGetJob(ctx, subReq)
	case SubOpSchedulerUpdateJob:
		return h.handleSchedulerUpdateJob(ctx, subReq)
	case SubOpSchedulerDeleteJob:
		return h.handleSchedulerDeleteJob(ctx, subReq)
	case SubOpSchedulerListJobs:
		return h.handleSchedulerListJobs(ctx, subReq)
	case SubOpSchedulerTriggerJob:
		return h.handleSchedulerTriggerJob(ctx, subReq)
	case SubOpSchedulerPauseJob:
		return h.handleSchedulerPauseJob(ctx, subReq)
	case SubOpSchedulerResumeJob:
		return h.handleSchedulerResumeJob(ctx, subReq)
	case SubOpSchedulerCancelJob:
		return h.handleSchedulerCancelJob(ctx, subReq)
	case SubOpSchedulerGetJobHistory:
		return h.handleSchedulerGetJobHistory(ctx, subReq)
	case SubOpSchedulerGetStatus:
		return h.handleSchedulerGetStatus(ctx, subReq)
	default:
		return h.errorResponse(api.NewUnknownSubOpcodeError(
			"quic.handleSchedulerDispatch", "scheduler", subOpcode))
	}
}

// =============================================================================
// Scheduler Job CRUD Operations
// =============================================================================

// handleSchedulerCreateJob creates a new scheduled job.
func (h *Handler) handleSchedulerCreateJob(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var createReq service.CreateSchedulerJobRequest
	if err := DecodePayload(req.Payload, &createReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.CreateJob(ctx, &createReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSchedulerGetJob retrieves a job by ID.
func (h *Handler) handleSchedulerGetJob(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var getReq service.GetSchedulerJobRequest
	if err := DecodePayload(req.Payload, &getReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.GetJob(ctx, &getReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSchedulerUpdateJob updates an existing job.
func (h *Handler) handleSchedulerUpdateJob(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var updateReq service.UpdateSchedulerJobRequest
	if err := DecodePayload(req.Payload, &updateReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.UpdateJob(ctx, &updateReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSchedulerDeleteJob removes a scheduled job.
func (h *Handler) handleSchedulerDeleteJob(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var deleteReq service.DeleteSchedulerJobRequest
	if err := DecodePayload(req.Payload, &deleteReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.DeleteJob(ctx, &deleteReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSchedulerListJobs returns jobs matching the query criteria.
func (h *Handler) handleSchedulerListJobs(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var listReq service.ListSchedulerJobsRequest
	if err := DecodePayload(req.Payload, &listReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.ListJobs(ctx, &listReq)
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
// Scheduler Job Control Operations
// =============================================================================

// handleSchedulerTriggerJob immediately executes a job.
func (h *Handler) handleSchedulerTriggerJob(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var triggerReq service.TriggerSchedulerJobRequest
	if err := DecodePayload(req.Payload, &triggerReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.TriggerJob(ctx, &triggerReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSchedulerPauseJob pauses a scheduled job.
func (h *Handler) handleSchedulerPauseJob(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var pauseReq service.PauseSchedulerJobRequest
	if err := DecodePayload(req.Payload, &pauseReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.PauseJob(ctx, &pauseReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSchedulerResumeJob resumes a paused job.
func (h *Handler) handleSchedulerResumeJob(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var resumeReq service.ResumeSchedulerJobRequest
	if err := DecodePayload(req.Payload, &resumeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.ResumeJob(ctx, &resumeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSchedulerCancelJob cancels a scheduled job.
func (h *Handler) handleSchedulerCancelJob(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var cancelReq service.CancelSchedulerJobRequest
	if err := DecodePayload(req.Payload, &cancelReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.CancelJob(ctx, &cancelReq)
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
// Scheduler Status and History Operations
// =============================================================================

// handleSchedulerGetJobHistory returns execution history for a job.
func (h *Handler) handleSchedulerGetJobHistory(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	var historyReq service.GetSchedulerJobHistoryRequest
	if err := DecodePayload(req.Payload, &historyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := schedulerSvc.GetJobHistory(ctx, &historyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleSchedulerGetStatus returns scheduler status and statistics.
func (h *Handler) handleSchedulerGetStatus(ctx context.Context, req *Request) *Response {
	schedulerSvc := h.getSchedulerService()
	if schedulerSvc == nil {
		return h.errorResponse(api.NewServiceUnavailableError("scheduler"))
	}

	resp, err := schedulerSvc.GetStatus(ctx)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
