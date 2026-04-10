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

// handleCreateJob handles the CreateJob operation.
func (h *Handler) handleCreateJob(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.CreateSchedulerJobRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.CreateJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleGetJob handles the GetJob operation.
func (h *Handler) handleGetJob(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.GetSchedulerJobRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.GetJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleUpdateJob handles the UpdateJob operation.
func (h *Handler) handleUpdateJob(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.UpdateSchedulerJobRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.UpdateJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleDeleteJob handles the DeleteJob operation.
func (h *Handler) handleDeleteJob(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.DeleteSchedulerJobRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.DeleteJob(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleListJobs handles the ListJobs operation.
func (h *Handler) handleListJobs(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.ListSchedulerJobsRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.ListJobs(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
