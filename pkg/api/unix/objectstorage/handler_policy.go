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

// handleSetObjectPolicy handles the SetObjectPolicy operation.
func (h *Handler) handleSetObjectPolicy(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.SetObjectPolicyRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.SetObjectPolicy(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleGetObjectPolicy handles the GetObjectPolicy operation.
func (h *Handler) handleGetObjectPolicy(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.GetObjectPolicyRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.GetObjectPolicy(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
