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

package objectstorage

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// handlePutObject handles the PutObject operation.
func (h *Handler) handlePutObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.PutObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.PutObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleGetObject handles the GetObject operation.
func (h *Handler) handleGetObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.GetObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.GetObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleDeleteObject handles the DeleteObject operation.
func (h *Handler) handleDeleteObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.DeleteObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.DeleteObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleHeadObject handles the HeadObject operation.
func (h *Handler) handleHeadObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.HeadObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.HeadObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
