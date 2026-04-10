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

// handleTriggerJob handles the TriggerJob operation.
func (h *Handler) handleTriggerJob(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.TriggerSchedulerJobRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.TriggerJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handlePauseJob handles the PauseJob operation.
func (h *Handler) handlePauseJob(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.PauseSchedulerJobRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.PauseJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleResumeJob handles the ResumeJob operation.
func (h *Handler) handleResumeJob(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.ResumeSchedulerJobRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.ResumeJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleCancelJob handles the CancelJob operation.
func (h *Handler) handleCancelJob(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.CancelSchedulerJobRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.CancelJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
