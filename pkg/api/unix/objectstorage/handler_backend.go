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

// handleListBackends handles the ListBackends operation.
func (h *Handler) handleListBackends(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.ListBackendsRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.ListBackends(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleGetBackendHealth handles the GetBackendHealth operation.
func (h *Handler) handleGetBackendHealth(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.GetBackendHealthRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.GetBackendHealth(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
