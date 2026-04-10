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

// handleCopyObject handles the CopyObject operation.
func (h *Handler) handleCopyObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.CopyObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.CopyObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleReplicateObject handles the ReplicateObject operation.
func (h *Handler) handleReplicateObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.ReplicateObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.ReplicateObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleMigrateObject handles the MigrateObject operation.
func (h *Handler) handleMigrateObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.MigrateObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.MigrateObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleArchiveObject handles the ArchiveObject operation.
func (h *Handler) handleArchiveObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.ArchiveObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.ArchiveObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleRestoreObject handles the RestoreObject operation.
func (h *Handler) handleRestoreObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.RestoreObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.RestoreObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleVerifyObject handles the VerifyObject operation.
func (h *Handler) handleVerifyObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req service.VerifyObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.service.VerifyObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
