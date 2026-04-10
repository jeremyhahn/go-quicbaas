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

// Package objectstorageadmin provides the SDK client for ObjectStorage admin operations.
package objectstorageadmin

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.ObjectStorageAdmin"
)

// Client provides admin operations for ObjectStorage.
type Client interface {
	// Backend Management

	// CreateBackend registers a new storage backend.
	CreateBackend(ctx context.Context, req *CreateBackendRequest) (*CreateBackendResponse, error)

	// UpdateBackend updates backend configuration.
	UpdateBackend(ctx context.Context, req *UpdateBackendRequest) (*UpdateBackendResponse, error)

	// DeleteBackend removes a storage backend (requires migration of objects).
	DeleteBackend(ctx context.Context, req *DeleteBackendRequest) (*DeleteBackendResponse, error)

	// SetBackendStatus enables or disables a backend.
	SetBackendStatus(ctx context.Context, req *SetBackendStatusRequest) (*SetBackendStatusResponse, error)

	// Storage Stats & Maintenance

	// GetStorageStats returns cluster-wide storage statistics.
	GetStorageStats(ctx context.Context, req *GetStorageStatsRequest) (*GetStorageStatsResponse, error)

	// RunGarbageCollection initiates garbage collection on specified backends.
	RunGarbageCollection(ctx context.Context, req *RunGarbageCollectionRequest) (*RunGarbageCollectionResponse, error)

	// GetGarbageCollectionProgress returns GC progress.
	GetGarbageCollectionProgress(ctx context.Context, req *GetGarbageCollectionProgressRequest) (*GetGarbageCollectionProgressResponse, error)

	// Object Repair & Compaction

	// RepairObject attempts to repair an object with inconsistent replicas.
	RepairObject(ctx context.Context, req *RepairObjectRequest) (*RepairObjectResponse, error)

	// CompactBackend triggers compaction on a backend.
	CompactBackend(ctx context.Context, req *CompactBackendRequest) (*CompactBackendResponse, error)
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new ObjectStorage Admin client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// =============================================================================
// Backend Management Operations
// =============================================================================

// CreateBackend registers a new storage backend.
func (c *client) CreateBackend(ctx context.Context, req *CreateBackendRequest) (*CreateBackendResponse, error) {
	if err := validateCreateBackendRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateBackend", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorageadmin.CreateBackend", "nil response from server")
	}

	createResp, ok := resp.(*CreateBackendResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.CreateBackend", "invalid response type")
	}

	return createResp, nil
}

// UpdateBackend updates backend configuration.
func (c *client) UpdateBackend(ctx context.Context, req *UpdateBackendRequest) (*UpdateBackendResponse, error) {
	if err := validateUpdateBackendRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateBackend", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorageadmin.UpdateBackend", "nil response from server")
	}

	updateResp, ok := resp.(*UpdateBackendResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.UpdateBackend", "invalid response type")
	}

	return updateResp, nil
}

// DeleteBackend removes a storage backend (requires migration of objects).
func (c *client) DeleteBackend(ctx context.Context, req *DeleteBackendRequest) (*DeleteBackendResponse, error) {
	if err := validateDeleteBackendRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteBackend", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorageadmin.DeleteBackend", "nil response from server")
	}

	deleteResp, ok := resp.(*DeleteBackendResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.DeleteBackend", "invalid response type")
	}

	return deleteResp, nil
}

// SetBackendStatus enables or disables a backend.
func (c *client) SetBackendStatus(ctx context.Context, req *SetBackendStatusRequest) (*SetBackendStatusResponse, error) {
	if err := validateSetBackendStatusRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetBackendStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorageadmin.SetBackendStatus", "nil response from server")
	}

	statusResp, ok := resp.(*SetBackendStatusResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.SetBackendStatus", "invalid response type")
	}

	return statusResp, nil
}

// =============================================================================
// Storage Stats & Maintenance Operations
// =============================================================================

// GetStorageStats returns cluster-wide storage statistics.
func (c *client) GetStorageStats(ctx context.Context, req *GetStorageStatsRequest) (*GetStorageStatsResponse, error) {
	if err := validateGetStorageStatsRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetStorageStats", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &GetStorageStatsResponse{BackendStats: []*BackendInfo{}, TenantStats: []*TenantUsage{}}, nil
	}

	statsResp, ok := resp.(*GetStorageStatsResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.GetStorageStats", "invalid response type")
	}

	return statsResp, nil
}

// RunGarbageCollection initiates garbage collection on specified backends.
func (c *client) RunGarbageCollection(ctx context.Context, req *RunGarbageCollectionRequest) (*RunGarbageCollectionResponse, error) {
	if err := validateRunGarbageCollectionRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "RunGarbageCollection", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorageadmin.RunGarbageCollection", "nil response from server")
	}

	gcResp, ok := resp.(*RunGarbageCollectionResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.RunGarbageCollection", "invalid response type")
	}

	return gcResp, nil
}

// GetGarbageCollectionProgress returns GC progress.
func (c *client) GetGarbageCollectionProgress(ctx context.Context, req *GetGarbageCollectionProgressRequest) (*GetGarbageCollectionProgressResponse, error) {
	if err := validateGetGarbageCollectionProgressRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetGarbageCollectionProgress", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("objectstorageadmin.GetGarbageCollectionProgress", "operation not found")
	}

	progressResp, ok := resp.(*GetGarbageCollectionProgressResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.GetGarbageCollectionProgress", "invalid response type")
	}

	return progressResp, nil
}

// =============================================================================
// Object Repair & Compaction Operations
// =============================================================================

// RepairObject attempts to repair an object with inconsistent replicas.
func (c *client) RepairObject(ctx context.Context, req *RepairObjectRequest) (*RepairObjectResponse, error) {
	if err := validateRepairObjectRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "RepairObject", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorageadmin.RepairObject", "nil response from server")
	}

	repairResp, ok := resp.(*RepairObjectResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.RepairObject", "invalid response type")
	}

	return repairResp, nil
}

// CompactBackend triggers compaction on a backend.
func (c *client) CompactBackend(ctx context.Context, req *CompactBackendRequest) (*CompactBackendResponse, error) {
	if err := validateCompactBackendRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "CompactBackend", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("objectstorageadmin.CompactBackend", "nil response from server")
	}

	compactResp, ok := resp.(*CompactBackendResponse)
	if !ok {
		return nil, errors.NewInternal("objectstorageadmin.CompactBackend", "invalid response type")
	}

	return compactResp, nil
}

// =============================================================================
// Validation Functions
// =============================================================================

func validateCreateBackendRequest(req *CreateBackendRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.CreateBackend", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("objectstorageadmin.CreateBackend", "name is required")
	}
	if req.Type == BackendTypeUnspecified {
		return errors.NewInvalidInput("objectstorageadmin.CreateBackend", "type is required")
	}
	if req.Tier == StorageTierUnspecified {
		return errors.NewInvalidInput("objectstorageadmin.CreateBackend", "tier is required")
	}
	if req.Config == nil || len(req.Config) == 0 {
		return errors.NewInvalidInput("objectstorageadmin.CreateBackend", "config is required")
	}
	return nil
}

func validateUpdateBackendRequest(req *UpdateBackendRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.UpdateBackend", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("objectstorageadmin.UpdateBackend", "name is required")
	}
	if req.Config == nil && req.Tier == StorageTierUnspecified {
		return errors.NewInvalidInput("objectstorageadmin.UpdateBackend", "at least one update field is required")
	}
	return nil
}

func validateDeleteBackendRequest(req *DeleteBackendRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.DeleteBackend", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("objectstorageadmin.DeleteBackend", "name is required")
	}
	return nil
}

func validateSetBackendStatusRequest(req *SetBackendStatusRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.SetBackendStatus", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("objectstorageadmin.SetBackendStatus", "name is required")
	}
	return nil
}

func validateGetStorageStatsRequest(req *GetStorageStatsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.GetStorageStats", "request is required")
	}
	return nil
}

func validateRunGarbageCollectionRequest(req *RunGarbageCollectionRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.RunGarbageCollection", "request is required")
	}
	return nil
}

func validateGetGarbageCollectionProgressRequest(req *GetGarbageCollectionProgressRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.GetGarbageCollectionProgress", "request is required")
	}
	if req.OperationID == "" {
		return errors.NewInvalidInput("objectstorageadmin.GetGarbageCollectionProgress", "operation ID is required")
	}
	return nil
}

func validateRepairObjectRequest(req *RepairObjectRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.RepairObject", "request is required")
	}
	if req.Key == "" {
		return errors.NewInvalidInput("objectstorageadmin.RepairObject", "key is required")
	}
	return nil
}

func validateCompactBackendRequest(req *CompactBackendRequest) error {
	if req == nil {
		return errors.NewInvalidInput("objectstorageadmin.CompactBackend", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("objectstorageadmin.CompactBackend", "name is required")
	}
	return nil
}
