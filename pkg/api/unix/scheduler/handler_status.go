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

package scheduler

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// handleGetJobHistory handles the GetJobHistory operation.
func (h *Handler) handleGetJobHistory(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.GetSchedulerJobHistoryRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.GetJobHistory(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleGetStatus handles the GetStatus operation.
func (h *Handler) handleGetStatus(ctx context.Context, _ []byte) ([]byte, error) {
	resp, err := h.service.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
