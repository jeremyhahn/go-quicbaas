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

package dek

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.DEK"
)

// Client provides Data Encryption Key management operations.
type Client interface {
	// CreateDEK creates a new Data Encryption Key at the specified path.
	CreateDEK(ctx context.Context, req *CreateDEKRequest) (*CreateDEKResponse, error)

	// GetDEK retrieves DEK metadata by path.
	GetDEK(ctx context.Context, path string) (*DEKInfo, error)

	// ListDEKs returns all DEKs matching the optional path prefix.
	ListDEKs(ctx context.Context, req *ListDEKsRequest) (*ListDEKsResponse, error)

	// RotateDEK creates a new version of an existing DEK.
	RotateDEK(ctx context.Context, req *RotateDEKRequest) (*RotateDEKResponse, error)

	// DeleteDEK removes a DEK (requires force or migration).
	DeleteDEK(ctx context.Context, req *DeleteDEKRequest) (*DeleteDEKResponse, error)

	// GetDEKStatus retrieves detailed DEK status including AEAD usage.
	GetDEKStatus(ctx context.Context, path string) (*DEKStatusResponse, error)

	// SetDefaultDEK marks a DEK as the default for its path level.
	SetDefaultDEK(ctx context.Context, path string) (*SetDefaultDEKResponse, error)

	// SetDEKSafety configures AEAD safety limits for a DEK.
	SetDEKSafety(ctx context.Context, req *SetDEKSafetyRequest) error

	// ClearDEKSafety removes custom AEAD safety limits from a DEK.
	ClearDEKSafety(ctx context.Context, path string) error

	// StartReencryption begins background re-encryption of data with new DEK version.
	StartReencryption(ctx context.Context, req *StartReencryptionRequest) (*StartReencryptionResponse, error)

	// GetReencryptionProgress returns the current progress of re-encryption.
	GetReencryptionProgress(ctx context.Context, req *GetReencryptionProgressRequest) (*ReencryptionProgressResponse, error)
}

// =============================================================================
// Types
// =============================================================================

// DEKInfo contains information about a Data Encryption Key.
type DEKInfo struct {
	Path           string
	Algorithm      string
	Version        int
	Status         string
	IsDefault      bool
	CreatedAt      time.Time
	RotatedAt      time.Time
	UsageCount     uint64
	BytesUsed      uint64
	MaxUsageCount  uint64
	MaxBytesUsed   uint64
	WarningPercent int
	Description    string
	BackendType    string
}

// CreateDEKRequest is a request to create a new DEK.
type CreateDEKRequest struct {
	// Path is the DEK path/identifier.
	Path string

	// Algorithm is the encryption algorithm (e.g., "AES-256-GCM").
	Algorithm string

	// Description provides context about the DEK.
	Description string

	// MaxOperations is the maximum number of encrypt operations.
	MaxOperations uint64

	// MaxBytes is the maximum bytes to encrypt.
	MaxBytes uint64

	// WarningPercent is the percentage threshold for warnings.
	WarningPercent int
}

// CreateDEKResponse is the response from creating a DEK.
type CreateDEKResponse struct {
	DEK *DEKInfo
}

// ListDEKsRequest is a request to list DEKs.
type ListDEKsRequest struct {
	// Prefix filters DEKs by path prefix.
	Prefix string

	// Recursive indicates whether to list recursively.
	Recursive bool

	// Limit is the maximum number of results.
	Limit int
}

// ListDEKsResponse is the response from listing DEKs.
type ListDEKsResponse struct {
	DEKs       []DEKInfo
	TotalCount int
}

// RotateDEKRequest is a request to rotate a DEK.
type RotateDEKRequest struct {
	// Path is the DEK path to rotate.
	Path string

	// Background indicates whether to run rotation in background.
	Background bool
}

// RotateDEKResponse is the response from rotating a DEK.
type RotateDEKResponse struct {
	Path       string
	OldVersion int
	NewVersion int
}

// DeleteDEKRequest is a request to delete a DEK.
type DeleteDEKRequest struct {
	// Path is the DEK path to delete.
	Path string

	// Force bypasses deletion protection.
	Force bool

	// MigrateTo optionally specifies a path to migrate data to.
	MigrateTo string
}

// DeleteDEKResponse is the response from deleting a DEK.
type DeleteDEKResponse struct {
	Path    string
	Deleted bool
}

// DEKStatusResponse contains detailed DEK status.
type DEKStatusResponse struct {
	DEK                  *DEKInfo
	ReencryptionProgress *ReencryptionProgress
}

// ReencryptionProgress tracks re-encryption operation progress.
type ReencryptionProgress struct {
	Status              string
	InProgress          bool
	TotalKeys           int
	ProcessedKeys       int
	FailedKeys          int
	PercentComplete     float64
	StartedAt           time.Time
	EstimatedCompletion time.Time
	ErrorMessage        string
}

// SetDefaultDEKResponse is the response from setting a default DEK.
type SetDefaultDEKResponse struct {
	Path            string
	PreviousDefault string
}

// SetDEKSafetyRequest is a request to set DEK safety limits.
type SetDEKSafetyRequest struct {
	// Path is the DEK path.
	Path string

	// MaxOperations is the maximum number of operations.
	MaxOperations uint64

	// MaxBytes is the maximum bytes to encrypt.
	MaxBytes uint64

	// WarningPercent is the percentage threshold for warnings.
	WarningPercent int
}

// StartReencryptionRequest is a request to start re-encryption.
type StartReencryptionRequest struct {
	// Path is the DEK path to re-encrypt.
	Path string

	// FromVersion is the old DEK version to re-encrypt from.
	FromVersion int

	// ToVersion is the new DEK version to re-encrypt to.
	ToVersion int

	// BatchSize is entities to process per batch.
	BatchSize int

	// Force re-encryption even if already in progress.
	Force bool
}

// StartReencryptionResponse is the response from starting re-encryption.
type StartReencryptionResponse struct {
	OperationID    string
	Path           string
	Started        bool
	EstimatedItems int
}

// GetReencryptionProgressRequest is a request to get re-encryption progress.
type GetReencryptionProgressRequest struct {
	// Path is the DEK path to check.
	Path string

	// OperationID is a specific operation ID to check.
	OperationID string
}

// ReencryptionProgressResponse is the response from getting re-encryption progress.
type ReencryptionProgressResponse struct {
	OperationID string
	Path        string
	Status      string
	Progress    *ReencryptionProgress
	Error       string
}

// =============================================================================
// Internal request types
// =============================================================================

type getDEKRequest struct {
	Path string
}

type getDEKStatusRequest struct {
	Path string
}

type setDefaultDEKRequest struct {
	Path string
}

type clearDEKSafetyRequest struct {
	Path string
}

type setDEKSafetyResponse struct{}

type clearDEKSafetyResponse struct{}

// =============================================================================
// Client implementation
// =============================================================================

type client struct {
	transport transport.Transport
}

// NewClient creates a new DEK client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// CreateDEK creates a new Data Encryption Key at the specified path.
func (c *client) CreateDEK(ctx context.Context, req *CreateDEKRequest) (*CreateDEKResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("dek.CreateDEK", "request is required")
	}
	if req.Path == "" {
		return nil, errors.NewInvalidInput("dek.CreateDEK", "path is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.CreateDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "CreateDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dek.CreateDEK", "nil response from server")
	}

	switch r := resp.(type) {
	case *CreateDEKResponse:
		return r, nil
	case *service.CreateDEKResponse:
		return &CreateDEKResponse{
			DEK: serviceDEKToSDK(r.DEK),
		}, nil
	default:
		return nil, errors.NewInternal("dek.CreateDEK", "invalid response type")
	}
}

// GetDEK retrieves DEK metadata by path.
func (c *client) GetDEK(ctx context.Context, path string) (*DEKInfo, error) {
	if path == "" {
		return nil, errors.NewInvalidInput("dek.GetDEK", "path is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.GetDEK", "context is required")
	}

	req := &getDEKRequest{Path: path}

	resp, err := c.transport.Request(ctx, serviceName, "GetDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("dek.GetDEK", "DEK not found")
	}

	switch r := resp.(type) {
	case *DEKInfo:
		return r, nil
	case *service.GetDEKResponse:
		return serviceDEKToSDK(r.DEK), nil
	default:
		return nil, errors.NewInternal("dek.GetDEK", "invalid response type")
	}
}

// ListDEKs returns all DEKs matching the optional path prefix.
func (c *client) ListDEKs(ctx context.Context, req *ListDEKsRequest) (*ListDEKsResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("dek.ListDEKs", "request is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.ListDEKs", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListDEKs", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListDEKsResponse{DEKs: []DEKInfo{}}, nil
	}

	switch r := resp.(type) {
	case *ListDEKsResponse:
		return r, nil
	case *service.ListDEKsResponse:
		deks := make([]DEKInfo, len(r.DEKs))
		for i, d := range r.DEKs {
			if info := serviceDEKToSDK(d); info != nil {
				deks[i] = *info
			}
		}
		return &ListDEKsResponse{
			DEKs:       deks,
			TotalCount: r.TotalCount,
		}, nil
	default:
		return nil, errors.NewInternal("dek.ListDEKs", "invalid response type")
	}
}

// RotateDEK creates a new version of an existing DEK.
func (c *client) RotateDEK(ctx context.Context, req *RotateDEKRequest) (*RotateDEKResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("dek.RotateDEK", "request is required")
	}
	if req.Path == "" {
		return nil, errors.NewInvalidInput("dek.RotateDEK", "path is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.RotateDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RotateDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dek.RotateDEK", "nil response from server")
	}

	switch r := resp.(type) {
	case *RotateDEKResponse:
		return r, nil
	case *service.RotateDEKResponse:
		return &RotateDEKResponse{
			Path:       r.Path,
			OldVersion: int(r.OldVersion),
			NewVersion: int(r.NewVersion),
		}, nil
	default:
		return nil, errors.NewInternal("dek.RotateDEK", "invalid response type")
	}
}

// DeleteDEK removes a DEK (requires force or migration).
func (c *client) DeleteDEK(ctx context.Context, req *DeleteDEKRequest) (*DeleteDEKResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("dek.DeleteDEK", "request is required")
	}
	if req.Path == "" {
		return nil, errors.NewInvalidInput("dek.DeleteDEK", "path is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.DeleteDEK", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "DeleteDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dek.DeleteDEK", "nil response from server")
	}

	switch r := resp.(type) {
	case *DeleteDEKResponse:
		return r, nil
	case *service.DeleteDEKResponse:
		return &DeleteDEKResponse{
			Path:    r.Path,
			Deleted: r.Deleted,
		}, nil
	default:
		return nil, errors.NewInternal("dek.DeleteDEK", "invalid response type")
	}
}

// GetDEKStatus retrieves detailed DEK status including AEAD usage.
func (c *client) GetDEKStatus(ctx context.Context, path string) (*DEKStatusResponse, error) {
	if path == "" {
		return nil, errors.NewInvalidInput("dek.GetDEKStatus", "path is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.GetDEKStatus", "context is required")
	}

	req := &getDEKStatusRequest{Path: path}

	resp, err := c.transport.Request(ctx, serviceName, "GetDEKStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("dek.GetDEKStatus", "DEK status not found")
	}

	switch r := resp.(type) {
	case *DEKStatusResponse:
		return r, nil
	case *service.GetDEKStatusResponse:
		statusResp := &DEKStatusResponse{
			DEK: serviceDEKToSDK(r.DEK),
		}
		if r.ReencryptionProgress != nil {
			statusResp.ReencryptionProgress = serviceReencryptionToSDK(r.ReencryptionProgress)
		}
		return statusResp, nil
	default:
		return nil, errors.NewInternal("dek.GetDEKStatus", "invalid response type")
	}
}

// SetDefaultDEK marks a DEK as the default for its path level.
func (c *client) SetDefaultDEK(ctx context.Context, path string) (*SetDefaultDEKResponse, error) {
	if path == "" {
		return nil, errors.NewInvalidInput("dek.SetDefaultDEK", "path is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.SetDefaultDEK", "context is required")
	}

	req := &setDefaultDEKRequest{Path: path}

	resp, err := c.transport.Request(ctx, serviceName, "SetDefaultDEK", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dek.SetDefaultDEK", "nil response from server")
	}

	switch r := resp.(type) {
	case *SetDefaultDEKResponse:
		return r, nil
	case *service.SetDefaultDEKResponse:
		return &SetDefaultDEKResponse{
			Path:            r.Path,
			PreviousDefault: r.PreviousDefault,
		}, nil
	default:
		return nil, errors.NewInternal("dek.SetDefaultDEK", "invalid response type")
	}
}

// SetDEKSafety configures AEAD safety limits for a DEK.
func (c *client) SetDEKSafety(ctx context.Context, req *SetDEKSafetyRequest) error {
	if req == nil {
		return errors.NewInvalidInput("dek.SetDEKSafety", "request is required")
	}
	if req.Path == "" {
		return errors.NewInvalidInput("dek.SetDEKSafety", "path is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput("dek.SetDEKSafety", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetDEKSafety", req)
	if err != nil {
		return err
	}

	if resp == nil {
		return nil
	}

	switch resp.(type) {
	case *setDEKSafetyResponse:
		return nil
	case *service.SetDEKSafetyResponse:
		return nil
	default:
		return errors.NewInternal("dek.SetDEKSafety", "invalid response type")
	}
}

// ClearDEKSafety removes custom AEAD safety limits from a DEK.
func (c *client) ClearDEKSafety(ctx context.Context, path string) error {
	if path == "" {
		return errors.NewInvalidInput("dek.ClearDEKSafety", "path is required")
	}
	if ctx == nil {
		return errors.NewInvalidInput("dek.ClearDEKSafety", "context is required")
	}

	req := &clearDEKSafetyRequest{Path: path}

	resp, err := c.transport.Request(ctx, serviceName, "ClearDEKSafety", req)
	if err != nil {
		return err
	}

	if resp == nil {
		return nil
	}

	switch resp.(type) {
	case *clearDEKSafetyResponse:
		return nil
	case *service.ClearDEKSafetyResponse:
		return nil
	default:
		return errors.NewInternal("dek.ClearDEKSafety", "invalid response type")
	}
}

// StartReencryption begins background re-encryption of data with new DEK version.
func (c *client) StartReencryption(ctx context.Context, req *StartReencryptionRequest) (*StartReencryptionResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("dek.StartReencryption", "request is required")
	}
	if req.Path == "" {
		return nil, errors.NewInvalidInput("dek.StartReencryption", "path is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.StartReencryption", "context is required")
	}

	svcReq := &service.StartReencryptionRequest{
		Path:        req.Path,
		FromVersion: uint64(req.FromVersion),
		ToVersion:   uint64(req.ToVersion),
		BatchSize:   req.BatchSize,
		Force:       req.Force,
	}

	resp, err := c.transport.Request(ctx, serviceName, "StartReencryption", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dek.StartReencryption", "nil response from server")
	}

	switch r := resp.(type) {
	case *StartReencryptionResponse:
		return r, nil
	case *service.StartReencryptionResponse:
		return &StartReencryptionResponse{
			OperationID:    r.OperationID,
			Path:           r.Path,
			Started:        r.Started,
			EstimatedItems: r.EstimatedItems,
		}, nil
	default:
		return nil, errors.NewInternal("dek.StartReencryption", "invalid response type")
	}
}

// GetReencryptionProgress returns the current progress of re-encryption.
func (c *client) GetReencryptionProgress(ctx context.Context, req *GetReencryptionProgressRequest) (*ReencryptionProgressResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("dek.GetReencryptionProgress", "request is required")
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput("dek.GetReencryptionProgress", "context is required")
	}

	svcReq := &service.GetReencryptionProgressRequest{
		Path:        req.Path,
		OperationID: req.OperationID,
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetReencryptionProgress", svcReq)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("dek.GetReencryptionProgress", "nil response from server")
	}

	switch r := resp.(type) {
	case *ReencryptionProgressResponse:
		return r, nil
	case *service.GetReencryptionProgressResponse:
		progressResp := &ReencryptionProgressResponse{
			OperationID: r.OperationID,
			Path:        r.Path,
			Status:      r.Status,
			Error:       r.Error,
		}
		if r.Progress != nil {
			progressResp.Progress = serviceReencryptionToSDK(r.Progress)
		}
		return progressResp, nil
	default:
		return nil, errors.NewInternal("dek.GetReencryptionProgress", "invalid response type")
	}
}

// =============================================================================
// Conversion helpers
// =============================================================================

// serviceDEKToSDK converts service DEKMetadata to SDK DEKInfo.
func serviceDEKToSDK(d *service.DEKMetadata) *DEKInfo {
	if d == nil {
		return nil
	}
	info := &DEKInfo{
		Path:           d.Path,
		Algorithm:      string(d.Algorithm),
		Version:        int(d.Version),
		Status:         d.Status,
		IsDefault:      d.IsDefault,
		CreatedAt:      d.CreatedAt,
		UsageCount:     d.UsageCount,
		BytesUsed:      d.BytesUsed,
		MaxUsageCount:  d.MaxUsageCount,
		MaxBytesUsed:   d.MaxBytesUsed,
		WarningPercent: d.WarningPercent,
		Description:    d.Description,
		BackendType:    d.BackendType,
	}
	if d.RotatedAt != nil {
		info.RotatedAt = *d.RotatedAt
	}
	return info
}

// serviceReencryptionToSDK converts service ReencryptionProgress to SDK.
func serviceReencryptionToSDK(p *service.ReencryptionProgress) *ReencryptionProgress {
	if p == nil {
		return nil
	}
	progress := &ReencryptionProgress{
		Status:          p.Status,
		InProgress:      p.InProgress,
		TotalKeys:       int(p.TotalKeys),
		ProcessedKeys:   int(p.ProcessedKeys),
		FailedKeys:      int(p.FailedKeys),
		PercentComplete: p.PercentComplete,
		ErrorMessage:    p.ErrorMessage,
	}
	if p.StartedAt != nil {
		progress.StartedAt = *p.StartedAt
	}
	if p.EstimatedCompletion != nil {
		progress.EstimatedCompletion = *p.EstimatedCompletion
	}
	return progress
}
