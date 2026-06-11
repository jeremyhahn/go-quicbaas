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
	"fmt"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// PEP Operations Handlers - Direct Opcode Handlers
// =============================================================================

// PEPSubscribeResponse represents the initial response for a PEP subscribe operation.
// For QUIC, we return an initial snapshot since streaming is not directly supported.
type PEPSubscribeResponse struct {
	// SubscriptionID is the identifier for this subscription
	SubscriptionID string `json:"subscription_id"`

	// InitialPolicies contains the current policy snapshot
	InitialPolicies []*service.PolicyUpdate `json:"initial_policies,omitempty"`

	// Message contains status information
	Message string `json:"message,omitempty"`
}

// handlePEPCheck handles the OpPEPCheck opcode for single authorization checks.
// This handler performs a single policy check with sub-5us latency when policies are cached.
func (h *Handler) handlePEPCheck(ctx context.Context, req *Request) *Response {
	pepSvc, ok := h.service.(pepServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("PEP"))
	}

	var checkReq service.CheckRequest
	if err := DecodePayload(req.Payload, &checkReq); err != nil {
		return h.errorResponse(err)
	}

	// Validate the request
	if err := validateCheckRequest(&checkReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pepSvc.Check(ctx, &checkReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handlePEPCheckBatch handles the OpPEPCheckBatch opcode for batch authorization checks.
// This handler efficiently processes multiple authorization checks in a single request.
func (h *Handler) handlePEPCheckBatch(ctx context.Context, req *Request) *Response {
	pepSvc, ok := h.service.(pepServiceInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("PEP"))
	}

	var batchReq service.BatchCheckRequest
	if err := DecodePayload(req.Payload, &batchReq); err != nil {
		return h.errorResponse(err)
	}

	// Validate the batch request
	if err := validateBatchCheckRequest(&batchReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := pepSvc.CheckBatch(ctx, &batchReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handlePEPSubscribe handles the OpPEPSubscribe opcode for policy update subscriptions.
// For QUIC, this returns an initial policy snapshot since the protocol doesn't support
// long-lived streaming responses. Clients should poll or use a dedicated subscription
// mechanism for real-time updates.
func (h *Handler) handlePEPSubscribe(ctx context.Context, req *Request) *Response {
	pepSvc, ok := h.service.(pepSubscribeInterface)
	if !ok {
		return h.errorResponse(api.NewServiceUnavailableError("PEP subscribe"))
	}

	var subscribeReq service.SubscribePolicyUpdatesRequest
	if err := DecodePayload(req.Payload, &subscribeReq); err != nil {
		return h.errorResponse(err)
	}

	// Validate the subscribe request
	if err := validateSubscribeRequest(&subscribeReq); err != nil {
		return h.errorResponse(err)
	}

	// Subscribe and get initial policy updates
	updatesCh, err := pepSvc.Subscribe(ctx, &subscribeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	// Collect initial snapshot (non-blocking read of available updates)
	var initialPolicies []*service.PolicyUpdate
	done := false
	for !done {
		select {
		case update, ok := <-updatesCh:
			if !ok {
				done = true
				break
			}
			initialPolicies = append(initialPolicies, update)
			// Limit initial snapshot size
			if len(initialPolicies) >= 1000 {
				done = true
			}
		default:
			// No more updates available immediately
			done = true
		}
	}

	// Build response with subscription info and initial snapshot
	resp := &PEPSubscribeResponse{
		SubscriptionID:  fmt.Sprintf("pep-sub-%s-%d", subscribeReq.TenantID, ctx.Value("request_id")),
		InitialPolicies: initialPolicies,
		Message:         "subscription created with initial policy snapshot",
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// PEP Service Interfaces
// =============================================================================

// pepSubscribeInterface extends pepServiceInterface with Subscribe capability.
type pepSubscribeInterface interface {
	pepServiceInterface
	Subscribe(ctx context.Context, req *service.SubscribePolicyUpdatesRequest) (<-chan *service.PolicyUpdate, error)
}

// =============================================================================
// Validation Functions
// =============================================================================

// validateCheckRequest validates a CheckRequest.
func validateCheckRequest(req *service.CheckRequest) error {
	if req == nil {
		return api.NewNilInputError("quic.validateCheckRequest", "check request")
	}

	if req.Subject.ID == "" {
		return api.NewRequiredFieldError("subject_id")
	}

	if req.Action == "" {
		return api.NewRequiredFieldError("action")
	}

	if req.Resource == "" {
		return api.NewRequiredFieldError("resource")
	}

	return nil
}

// validateBatchCheckRequest validates a BatchCheckRequest.
func validateBatchCheckRequest(req *service.BatchCheckRequest) error {
	if req == nil {
		return api.NewNilInputError("quic.validateBatchCheckRequest", "batch check request")
	}

	if len(req.Checks) == 0 {
		return api.NewEmptyCollectionError("checks")
	}

	// Validate each check in the batch
	for i, check := range req.Checks {
		if err := validateCheckRequest(check); err != nil {
			return api.NewInvalidBatchCheckError("quic.validateBatchCheckRequest", i, err)
		}
	}

	return nil
}

// validateSubscribeRequest validates a SubscribePolicyUpdatesRequest.
func validateSubscribeRequest(req *service.SubscribePolicyUpdatesRequest) error {
	if req == nil {
		return api.NewNilInputError("quic.validateSubscribeRequest", "subscribe request")
	}

	if req.TenantID == "" {
		return api.NewRequiredFieldError("tenant_id")
	}

	return nil
}
