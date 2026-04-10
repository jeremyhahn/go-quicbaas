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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// handleGetStats handles the GetStats operation.
func (h *Handler) handleGetStats(ctx context.Context, _ []byte) ([]byte, error) {
	stats := h.service.GetStats()

	resp := &GetStatsResponse{
		Stats: statsToInfo(stats),
	}

	return EncodePayload(resp)
}

// handleDedupStats handles the DedupStats operation.
func (h *Handler) handleDedupStats(ctx context.Context, reqData []byte) ([]byte, error) {
	var req DedupStatsRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	// Get the concrete PushService to access the dedup store
	msgService, ok := h.service.(*messaging.PushService)
	if !ok {
		return nil, api.NewFeatureUnsupportedError(
			"unix.messaging.handleDedupStats", "dedup stats")
	}

	dedupStore := msgService.GetDedupStore()
	if dedupStore == nil {
		return nil, api.NewComponentUnavailableError("deduplication")
	}

	stats, err := dedupStore.GetStats(ctx, req.Channel)
	if err != nil {
		return nil, err
	}

	resp := &DedupStatsResponse{
		Stats: dedupStatsToInfo(stats),
	}

	return EncodePayload(resp)
}
