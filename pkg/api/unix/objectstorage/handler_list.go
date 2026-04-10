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

// handleListObjects handles the ListObjects operation.
func (h *Handler) handleListObjects(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.ListObjectsRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.ListObjects(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleListByTag handles the ListByTag operation.
func (h *Handler) handleListByTag(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.ListByTagRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.ListByTag(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleListByTenant handles the ListByTenant operation.
func (h *Handler) handleListByTenant(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.ListByTenantRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.ListByTenant(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
