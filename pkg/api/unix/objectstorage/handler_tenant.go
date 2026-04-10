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

// handleGetTenantUsage handles the GetTenantUsage operation.
func (h *Handler) handleGetTenantUsage(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.GetTenantUsageRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.GetTenantUsage(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleGetTenantQuotas handles the GetTenantQuotas operation.
func (h *Handler) handleGetTenantQuotas(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.GetTenantQuotasRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.GetTenantQuotas(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleSetTenantQuotas handles the SetTenantQuotas operation.
func (h *Handler) handleSetTenantQuotas(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.SetTenantQuotasRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.SetTenantQuotas(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
