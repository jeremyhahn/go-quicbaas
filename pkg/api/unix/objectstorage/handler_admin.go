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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// checkAdminService verifies that the admin service is available.
func (h *Handler) checkAdminService() error {
	if h.adminService == nil {
		return api.NewComponentUnavailableError("admin service")
	}
	return nil
}

// handleAdminCreateBackend handles the AdminCreateBackend operation.
func (h *Handler) handleAdminCreateBackend(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.CreateBackendRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.CreateBackend(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleAdminUpdateBackend handles the AdminUpdateBackend operation.
func (h *Handler) handleAdminUpdateBackend(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.UpdateBackendRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.UpdateBackend(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleAdminDeleteBackend handles the AdminDeleteBackend operation.
func (h *Handler) handleAdminDeleteBackend(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.DeleteBackendRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.DeleteBackend(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleAdminSetBackendStatus handles the AdminSetBackendStatus operation.
func (h *Handler) handleAdminSetBackendStatus(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.SetBackendStatusRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.SetBackendStatus(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleAdminGetStorageStats handles the AdminGetStorageStats operation.
func (h *Handler) handleAdminGetStorageStats(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.GetStorageStatsRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.GetStorageStats(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleAdminRunGC handles the AdminRunGC operation.
func (h *Handler) handleAdminRunGC(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.RunGarbageCollectionRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.RunGarbageCollection(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleAdminGetGCProgress handles the AdminGetGCProgress operation.
func (h *Handler) handleAdminGetGCProgress(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.GetGarbageCollectionProgressRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.GetGarbageCollectionProgress(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleAdminRepairObject handles the AdminRepairObject operation.
func (h *Handler) handleAdminRepairObject(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.RepairObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.RepairObject(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}

// handleAdminCompactBackend handles the AdminCompactBackend operation.
func (h *Handler) handleAdminCompactBackend(ctx context.Context, reqData []byte) ([]byte, error) {
	if err := h.checkAdminService(); err != nil {
		return nil, err
	}

	var req service.CompactBackendRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	resp, err := h.adminService.CompactBackend(ctx, &req)
	if err != nil {
		return nil, err
	}

	return EncodePayload(resp)
}
