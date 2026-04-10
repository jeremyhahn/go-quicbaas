//go:build unix

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

package messaging

import (
	"context"
	"fmt"
	"sync/atomic"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// HandlerFunc is the signature for request handlers.
type HandlerFunc func(ctx context.Context, req []byte) ([]byte, error)

// Handler handles Push Unix socket requests.
type Handler struct {
	service  messaging.Service
	handlers map[byte]HandlerFunc
	logger   interfaces.Logger
	closed   atomic.Bool
}

// NewHandler creates a new Push Unix socket handler.
func NewHandler(
	svc messaging.Service,
	logger interfaces.Logger,
) (*Handler, error) {
	if svc == nil {
		return nil, api.NewRequiredFieldError("msg service")
	}

	if logger == nil {
		return nil, api.NewRequiredFieldError("logger")
	}

	h := &Handler{
		service: svc,
		logger:  logger.WithField("component", "unix_msg_handler"),
	}

	h.registerHandlers()

	h.logger.Info("unix msg handler initialized")

	return h, nil
}

// registerHandlers registers all operation handlers using map-based dispatch.
func (h *Handler) registerHandlers() {
	h.handlers = map[byte]HandlerFunc{
		// Channel Operations
		OpPushCreateChannel: h.handleCreateChannel,
		OpPushDeleteChannel: h.handleDeleteChannel,
		OpPushGetChannel:    h.handleGetChannel,
		OpPushListChannels:  h.handleListChannels,

		// Subscription Operations
		OpPushSubscribe:   h.handleSubscribe,
		OpPushUnsubscribe: h.handleUnsubscribe,

		// Publish Operations
		OpPushPublish:      h.handlePublish,
		OpPushPublishDelta: h.handlePublishDelta,

		// Stats Operations
		OpPushGetStats:   h.handleGetStats,
		OpPushDedupStats: h.handleDedupStats,
	}
}

// Handle processes a request and returns a response.
func (h *Handler) Handle(ctx context.Context, opcode byte, req []byte) ([]byte, error) {
	if h.closed.Load() {
		return nil, api.NewHandlerClosedError("unix.messaging.Handle")
	}

	handler, ok := h.handlers[opcode]
	if !ok {
		return nil, api.NewUnknownOpcodeError("unix.messaging.Handle", opcode)
	}

	h.logger.WithFields(map[string]interface{}{
		"opcode": fmt.Sprintf("0x%02X", opcode),
		"op":     OpcodeToString(opcode),
	}).Debug("handling request")

	return handler(ctx, req)
}

// HandleRequest processes a full Request struct and returns a Response.
func (h *Handler) HandleRequest(ctx context.Context, req *Request) *Response {
	if req == nil {
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: encodeErrorResponse("request cannot be nil"),
		}
	}

	payload, err := h.Handle(ctx, req.Opcode, req.Payload)
	if err != nil {
		return &Response{
			Status:  ErrorToStatus(err),
			Payload: encodeErrorResponse(err.Error()),
		}
	}

	return &Response{
		Status:  StatusOK,
		Payload: payload,
	}
}

// Close closes the handler.
func (h *Handler) Close() error {
	if h.closed.Swap(true) {
		return nil
	}
	h.logger.Info("unix msg handler closed")
	return nil
}

// encodeErrorResponse encodes an error message as a response payload.
func encodeErrorResponse(msg string) []byte {
	resp := &ErrorResponse{
		Error: msg,
	}
	data, _ := EncodePayload(resp)
	return data
}

// Service returns the msg service.
func (h *Handler) Service() messaging.Service {
	return h.service
}
