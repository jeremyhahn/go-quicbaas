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

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// handleSubscribe handles the Subscribe operation.
func (h *Handler) handleSubscribe(ctx context.Context, reqData []byte) ([]byte, error) {
	var req SubscribeRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	// Build options from request
	opts := make([]messaging.SubscribeOption, 0, 3)
	if req.SessionID != "" {
		opts = append(opts, messaging.WithSession(req.SessionID))
	}
	if req.BufferSize > 0 {
		opts = append(opts, messaging.WithSubscriptionBufferSize(req.BufferSize))
	}
	if req.Filter != nil {
		filter := &messaging.SubscriptionFilter{
			KeyPrefix: req.Filter.KeyPrefix,
		}
		if len(req.Filter.EventTypes) > 0 {
			filter.EventTypes = make([]messaging.EventType, 0, len(req.Filter.EventTypes))
			for _, et := range req.Filter.EventTypes {
				switch et {
				case "PUT":
					filter.EventTypes = append(filter.EventTypes, messaging.EventTypePut)
				case "DELETE":
					filter.EventTypes = append(filter.EventTypes, messaging.EventTypeDelete)
				}
			}
		}
		opts = append(opts, messaging.WithFilter(filter))
	}

	subscription, err := h.service.Subscribe(ctx, req.Channel, opts...)
	if err != nil {
		return nil, err
	}

	resp := subscriptionToResponse(subscription)

	return EncodePayload(resp)
}

// handleUnsubscribe handles the Unsubscribe operation.
func (h *Handler) handleUnsubscribe(ctx context.Context, reqData []byte) ([]byte, error) {
	var req UnsubscribeRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	if err := h.service.Unsubscribe(ctx, req.SubscriptionID); err != nil {
		return nil, err
	}

	resp := &UnsubscribeResponse{
		Success: true,
	}

	return EncodePayload(resp)
}
