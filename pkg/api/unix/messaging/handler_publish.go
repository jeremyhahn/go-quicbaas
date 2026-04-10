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
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// handlePublish handles the Publish operation.
func (h *Handler) handlePublish(ctx context.Context, reqData []byte) ([]byte, error) {
	var req PublishRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	// Build options from request
	opts := make([]messaging.PublishOption, 0, 3)
	if req.MessageID != "" {
		opts = append(opts, messaging.WithMessageID(req.MessageID))
	}
	if req.DedupKey != "" {
		opts = append(opts, messaging.WithDedupKey(req.DedupKey))
	}
	if len(req.Metadata) > 0 {
		opts = append(opts, messaging.WithMetadata(req.Metadata))
	}

	// Publish the raw JSON payload directly
	result, err := h.service.Publish(ctx, req.Channel, []byte(req.Payload), opts...)
	if err != nil {
		return nil, err
	}

	resp := &PublishResponse{
		Success:     true,
		MessageID:   result.MessageID,
		IsDuplicate: result.IsDuplicate,
	}

	return EncodePayload(resp)
}

// handlePublishDelta handles the PublishDelta operation.
func (h *Handler) handlePublishDelta(ctx context.Context, reqData []byte) ([]byte, error) {
	var req PublishDeltaRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	// Build options from request
	opts := make([]messaging.PublishOption, 0, 3)
	if req.MessageID != "" {
		opts = append(opts, messaging.WithMessageID(req.MessageID))
	}
	if req.DedupKey != "" {
		opts = append(opts, messaging.WithDedupKey(req.DedupKey))
	}
	if len(req.Metadata) > 0 {
		opts = append(opts, messaging.WithMetadata(req.Metadata))
	}

	// Parse the old and new values from JSON
	var oldValue, newValue interface{}
	if len(req.OldValue) > 0 {
		if err := json.Unmarshal(req.OldValue, &oldValue); err != nil {
			return nil, err
		}
	}
	if len(req.NewValue) > 0 {
		if err := json.Unmarshal(req.NewValue, &newValue); err != nil {
			return nil, err
		}
	}

	// Publish the delta
	result, err := h.service.PublishDelta(ctx, req.Channel, oldValue, newValue, opts...)
	if err != nil {
		return nil, err
	}

	resp := &PublishDeltaResponse{
		Success:     true,
		MessageID:   result.MessageID,
		IsDuplicate: result.IsDuplicate,
	}

	return EncodePayload(resp)
}
