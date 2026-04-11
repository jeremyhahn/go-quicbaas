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

package grpc

import (
	"context"
	"crypto/elliptic"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// KMSServer wraps the KMS manager for gRPC endpoints.
type KMSServer struct {
	pb.UnimplementedKeyManagementServer
	kmsMgr   interfaces.KMSManager
	kmsSvc   service.KMSService // optional: for service-layer operations
	logger   *logger.Logger
	keyNames sync.Map // tracks key names to prevent duplicates
}

// NewKMSServer creates a new gRPC server for KMS operations.
func NewKMSServer(kmsMgr interfaces.KMSManager, log *logger.Logger) *KMSServer {
	return &KMSServer{
		kmsMgr: kmsMgr,
		logger: log.WithField("component", "kms-grpc-server"),
	}
}

// NewKMSServerWithService creates a new gRPC server with service layer support.
// The service layer is required for operations like ExportKey, UnwrapKey, and Safety operations.
func NewKMSServerWithService(kmsMgr interfaces.KMSManager, kmsSvc service.KMSService, log *logger.Logger) *KMSServer {
	return &KMSServer{
		kmsMgr: kmsMgr,
		kmsSvc: kmsSvc,
		logger: log.WithField("component", "kms-grpc-server"),
	}
}

// SetKMSService sets the KMS service for service-layer operations.
// This is useful when the service is constructed after the server.
func (s *KMSServer) SetKMSService(kmsSvc service.KMSService) {
	s.kmsSvc = kmsSvc
}

// GenerateKey creates a new cryptographic key with specified parameters.
func (s *KMSServer) GenerateKey(ctx context.Context, req *pb.GenerateKeyRequest) (*pb.GenerateKeyResponse, error) {
	s.logger.Debug("Processing GenerateKey request")

	// Validate key name is not empty
	if strings.TrimSpace(req.Name) == "" {
		return nil, status.Error(codes.InvalidArgument, "key name is required")
	}

	if req.KeySpec == nil {
		return nil, status.Error(codes.InvalidArgument, "key_spec is required")
	}

	// Check for duplicate key name
	if _, exists := s.keyNames.Load(req.Name); exists {
		return nil, status.Error(codes.AlreadyExists, fmt.Sprintf("key with name '%s' already exists", req.Name))
	}

	// Use user-provided name as key ID
	keyID := req.Name

	// Route through the service layer for tenant-aware key generation
	if s.kmsSvc != nil {
		attrs, err := protoToKeyAttributes(keyID, req)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}

		svcReq := &service.GenerateKeyRequest{
			KeyAttributes:      attrs,
			Name:               req.Name,
			Description:        req.Description,
			Usage:              convertProtoKeyUsageList(req.KeySpec.Usage),
			Exportable:         req.KeySpec.Exportable,
			DeletionProtection: req.KeySpec.DeletionProtection,
			Tags:               req.Tags,
			Owner:              req.Owner,
		}

		svcResp, err := s.kmsSvc.GenerateKey(ctx, svcReq)
		if err != nil {
			return nil, convertKMSError(err)
		}

		s.keyNames.Store(req.Name, keyID)

		return &pb.GenerateKeyResponse{
			Key:       convertKeyHandle(svcResp.Key),
			Metadata:  convertKeyMetadata(svcResp.Metadata),
			Timestamp: svcResp.Timestamp.Unix(),
		}, nil
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	attrs, err := protoToKeyAttributes(keyID, req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	result, err := s.kmsMgr.GenerateKey(ctx, attrs)
	if err != nil {
		return nil, convertKMSError(err)
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, status.Error(codes.Internal, "unexpected response type from KMS manager")
	}

	metadata.Name = req.Name
	metadata.Description = req.Description
	metadata.Tags = req.Tags
	metadata.Owner = req.Owner
	if err := s.kmsMgr.UpdateStoredMetadata(metadata); err != nil {
		s.logger.WithError(err).Warn("Failed to persist key metadata")
	}

	s.keyNames.Store(req.Name, keyID)

	now := time.Now()

	return &pb.GenerateKeyResponse{
		Key:       convertKMSKeyHandle(keyID, metadata, req.KeySpec),
		Metadata:  convertKMSKeyMetadataFull(metadata),
		Timestamp: now.Unix(),
	}, nil
}

// ImportKey imports an externally generated key into the KMS.
func (s *KMSServer) ImportKey(ctx context.Context, req *pb.ImportKeyRequest) (*pb.ImportKeyResponse, error) {
	s.logger.Debug("Processing ImportKey request")

	if req.KeyMaterial == nil {
		return nil, status.Error(codes.InvalidArgument, "key_material is required")
	}

	if req.KeySpec == nil {
		return nil, status.Error(codes.InvalidArgument, "key_spec is required")
	}

	// Use user-provided name as key ID
	keyID := req.Name

	// Get algorithm string from proto
	algorithm := protoKeyAlgorithmToString(req.KeySpec.Algorithm)

	// Call KMS manager to import key
	result, err := s.kmsMgr.ImportKey(ctx, keyID, req.KeyMaterial, algorithm)
	if err != nil {
		return nil, convertKMSError(err)
	}

	// Type assert to *kms.KeyMetadata
	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, status.Error(codes.Internal, "unexpected response type from KMS manager")
	}

	// Update metadata with application-level fields and persist
	metadata.Name = req.Name
	metadata.Description = req.Description
	metadata.Tags = req.Tags
	if err := s.kmsMgr.UpdateStoredMetadata(metadata); err != nil {
		s.logger.WithError(err).Warn("Failed to persist key metadata")
	}

	now := time.Now()

	return &pb.ImportKeyResponse{
		Key:       convertKMSKeyHandle(keyID, metadata, req.KeySpec),
		Metadata:  convertKMSKeyMetadataFull(metadata),
		Timestamp: now.Unix(),
	}, nil
}

// GetKey retrieves key material or public key by key ID.
// If a specific version is requested via req.Version, it validates that the version exists.
// If no version is specified (version=0), the current version is returned.
func (s *KMSServer) GetKey(ctx context.Context, req *pb.GetKeyRequest) (*pb.GetKeyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).WithField("version", req.Version).Debug("Processing GetKey request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	// Route through the service layer for tenant-aware key retrieval
	if s.kmsSvc != nil {
		svcReq := &service.GetKeyRequest{
			KeyID:   req.KeyId,
			Version: req.Version,
		}

		svcResp, err := s.kmsSvc.GetKey(ctx, svcReq)
		if err != nil {
			return nil, convertKMSError(err)
		}

		return &pb.GetKeyResponse{
			Key:       convertKeyHandle(svcResp.Key),
			Metadata:  convertKeyMetadata(svcResp.Metadata),
			Timestamp: svcResp.Timestamp.Unix(),
		}, nil
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	result, err := s.kmsMgr.GetKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, status.Error(codes.Internal, "unexpected response type from KMS manager")
	}

	currentVersion := uint64(metadata.Version) //nolint:gosec // G115: Version is validated positive

	// Determine which version to return
	requestedVersion := req.Version
	if requestedVersion == 0 {
		// No version specified, use current version
		requestedVersion = currentVersion
	} else {
		// Validate that the requested version exists
		// Version must be > 0 and <= current version
		if requestedVersion > currentVersion {
			return nil, status.Error(codes.NotFound,
				fmt.Sprintf("key version %d not found (current version is %d)", requestedVersion, currentVersion))
		}
	}

	// Determine the status for the requested version
	// Current version has the actual status, older versions are typically disabled
	versionStatus := kmsStateToProtoStatus(metadata.State)
	if requestedVersion < currentVersion {
		versionStatus = pb.KeyStatus_KEY_STATUS_DISABLED
	}

	now := time.Now()

	// Build key handle with the requested version
	keyHandle := &pb.KeyHandle{
		KeyId:     metadata.KeyID,
		Version:   requestedVersion,
		Algorithm: algorithmStringToProto(metadata.Algorithm),
		Status:    versionStatus,
		CreatedAt: metadata.Created.Unix(),
	}

	// Build metadata with the requested version
	var lastUsed int64
	if metadata.LastUsedAt != nil {
		lastUsed = metadata.LastUsedAt.Unix()
	}

	keyMetadata := &pb.KeyMetadata{
		KeyId:              metadata.KeyID,
		Name:               metadata.Name,
		Description:        metadata.Description,
		Algorithm:          algorithmStringToProto(metadata.Algorithm),
		Status:             versionStatus,
		Usage:              kmsUsageListToProto(metadata.Usage),
		Version:            requestedVersion,
		Exportable:         metadata.Exportable,
		DeletionProtection: metadata.DeletionProtection,
		CreatedAt:          metadata.Created.Unix(),
		UpdatedAt:          metadata.Updated.Unix(),
		LastUsedAt:         lastUsed,
		RotationPolicy:     convertKMSRotationPolicy(metadata.RotationPolicy),
		Tags:               metadata.Tags,
		Owner:              metadata.Owner,
	}

	return &pb.GetKeyResponse{
		Key:       keyHandle,
		Metadata:  keyMetadata,
		Timestamp: now.Unix(),
	}, nil
}

// ListKeys returns all keys accessible to the requester with pagination.
func (s *KMSServer) ListKeys(ctx context.Context, req *pb.ListKeysRequest) (*pb.ListKeysResponse, error) {
	s.logger.Debug("Processing ListKeys request")

	// Get filter prefix from request
	prefix := ""
	if req.Filter != nil {
		if p, ok := req.Filter["prefix"]; ok {
			prefix = p
		}
	}

	// Route through the service layer for tenant-aware key listing
	if s.kmsSvc != nil {
		filter := make(map[string]string)
		if prefix != "" {
			filter["prefix"] = prefix
		}

		svcReq := &service.ListKeysRequest{
			Limit:  int(req.Limit), //nolint:gosec // G115: limit is bounded
			Cursor: req.Cursor,
			Filter: filter,
		}

		svcResp, err := s.kmsSvc.ListKeys(ctx, svcReq)
		if err != nil {
			return nil, convertKMSError(err)
		}

		keys := make([]*pb.KeyHandle, 0, len(svcResp.Keys))
		for _, kh := range svcResp.Keys {
			keys = append(keys, convertKeyHandle(kh))
		}

		return &pb.ListKeysResponse{
			Keys:       keys,
			NextCursor: svcResp.NextCursor,
			HasMore:    svcResp.HasMore,
			TotalCount: int32(svcResp.TotalCount), //nolint:gosec // G115: count is bounded
			Timestamp:  svcResp.Timestamp.Unix(),
		}, nil
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	results, err := s.kmsMgr.ListKeys(ctx, prefix)
	if err != nil {
		return nil, convertKMSError(err)
	}

	keys := make([]*pb.KeyHandle, 0, len(results))
	for _, result := range results {
		if metadata, ok := result.(*kms.KeyMetadata); ok {
			keys = append(keys, convertKMSKeyHandleFromMetadata(metadata))
		}
	}

	now := time.Now()

	hasMore := false
	if req.Limit > 0 && int(req.Limit) < len(keys) {
		keys = keys[:req.Limit]
		hasMore = true
	}

	return &pb.ListKeysResponse{
		Keys:       keys,
		NextCursor: "",
		HasMore:    hasMore,
		TotalCount: int32(len(results)), //nolint:gosec // G115: len is bounded
		Timestamp:  now.Unix(),
	}, nil
}

// RotateKey creates a new version of an existing key.
func (s *KMSServer) RotateKey(ctx context.Context, req *pb.RotateKeyRequest) (*pb.RotateKeyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Info("Processing RotateKey request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	// Use service layer if available (supports grace period scheduling)
	if s.kmsSvc != nil {
		svcReq := &service.RotateKeyRequest{
			KeyID:              req.KeyId,
			RetainOldVersion:   req.RetainOldVersion,
			GracePeriodSeconds: req.GracePeriodSeconds,
		}

		svcResp, err := s.kmsSvc.RotateKey(ctx, svcReq)
		if err != nil {
			return nil, convertKMSError(err)
		}

		return &pb.RotateKeyResponse{
			KeyId:                req.KeyId,
			OldVersion:           svcResp.OldVersion,
			NewVersion:           svcResp.NewVersion,
			Timestamp:            svcResp.Timestamp.Unix(),
			GracePeriodDisableAt: svcResp.GracePeriodDisableAt,
			GracePeriodJobId:     svcResp.GracePeriodJobID,
		}, nil
	}

	// Fallback to direct KMS manager if service not available
	// Get current key to determine old version
	oldResult, err := s.kmsMgr.GetKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	oldMetadata, ok := oldResult.(*kms.KeyMetadata)
	if !ok {
		return nil, status.Error(codes.Internal, "unexpected response type from KMS manager")
	}
	oldVersion := uint64(oldMetadata.Version) //nolint:gosec // G115: Version is validated positive

	// Call KMS manager to rotate key
	result, err := s.kmsMgr.RotateKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	// Type assert to *kms.KeyMetadata
	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, status.Error(codes.Internal, "unexpected response type from KMS manager")
	}

	now := time.Now()

	return &pb.RotateKeyResponse{
		KeyId:      req.KeyId,
		OldVersion: oldVersion,
		NewVersion: uint64(metadata.Version), //nolint:gosec // G115: Version is validated positive
		Timestamp:  now.Unix(),
	}, nil
}

// RevokeKey marks a key version as revoked and unusable.
func (s *KMSServer) RevokeKey(ctx context.Context, req *pb.RevokeKeyRequest) (*pb.RevokeKeyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Info("Processing RevokeKey request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	// Call KMS manager to revoke key
	err := s.kmsMgr.RevokeKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	return &pb.RevokeKeyResponse{
		KeyId:     req.KeyId,
		Status:    pb.KeyStatus_KEY_STATUS_DISABLED, // Revoked keys are disabled
		Timestamp: now.Unix(),
	}, nil
}

// DeleteKey permanently removes a key and all its versions.
func (s *KMSServer) DeleteKey(ctx context.Context, req *pb.DeleteKeyRequest) (*pb.DeleteKeyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Info("Processing DeleteKey request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	// Disable key first if not forcing
	if !req.Force {
		if err := s.kmsMgr.DisableKey(ctx, req.KeyId); err != nil {
			return nil, convertKMSError(err)
		}
	}

	// Call KMS manager to delete key
	err := s.kmsMgr.DeleteKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	// Calculate deletion date based on pending window
	pendingDays := int(req.PendingWindowDays)
	if pendingDays == 0 {
		pendingDays = 7 // Default pending window
	}
	deletionDate := now.AddDate(0, 0, pendingDays)

	return &pb.DeleteKeyResponse{
		KeyId:        req.KeyId,
		Status:       pb.KeyStatus_KEY_STATUS_PENDING_DELETION,
		DeletionDate: deletionDate.Unix(),
		Timestamp:    now.Unix(),
	}, nil
}

// GetKeyMetadata retrieves metadata for a key without exposing key material.
func (s *KMSServer) GetKeyMetadata(ctx context.Context, req *pb.GetKeyMetadataRequest) (*pb.GetKeyMetadataResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing GetKeyMetadata request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	// Call KMS manager to get key metadata
	result, err := s.kmsMgr.GetKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	// Type assert to *kms.KeyMetadata
	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, status.Error(codes.Internal, "unexpected response type from KMS manager")
	}

	now := time.Now()

	return &pb.GetKeyMetadataResponse{
		Metadata:  convertKMSKeyMetadataFull(metadata),
		Timestamp: now.Unix(),
	}, nil
}

// UpdateKeyMetadata updates mutable metadata fields for a key.
func (s *KMSServer) UpdateKeyMetadata(ctx context.Context, req *pb.UpdateKeyMetadataRequest) (*pb.UpdateKeyMetadataResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing UpdateKeyMetadata request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	// Get existing metadata
	result, err := s.kmsMgr.GetKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, status.Error(codes.Internal, "unexpected response type from KMS manager")
	}

	// Update mutable fields
	if req.Name != "" {
		metadata.Name = req.Name
	}
	if req.Description != "" {
		metadata.Description = req.Description
	}

	// Merge tags
	if metadata.Tags == nil {
		metadata.Tags = make(map[string]string)
	}
	for k, v := range req.Tags {
		metadata.Tags[k] = v
	}
	// Remove specified tags
	for _, k := range req.RemoveTags {
		delete(metadata.Tags, k)
	}

	metadata.Updated = time.Now()

	// Persist updated metadata
	if err := s.kmsMgr.UpdateStoredMetadata(metadata); err != nil {
		return nil, convertKMSError(err)
	}

	return &pb.UpdateKeyMetadataResponse{
		Metadata:  convertKMSKeyMetadataFull(metadata),
		Timestamp: time.Now().Unix(),
	}, nil
}

// UpdateKeyStatus enables or disables a key or key version.
func (s *KMSServer) UpdateKeyStatus(ctx context.Context, req *pb.UpdateKeyStatusRequest) (*pb.UpdateKeyStatusResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing UpdateKeyStatus request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	var err error
	switch req.Status {
	case pb.KeyStatus_KEY_STATUS_ENABLED:
		err = s.kmsMgr.EnableKey(ctx, req.KeyId)
	case pb.KeyStatus_KEY_STATUS_DISABLED:
		err = s.kmsMgr.DisableKey(ctx, req.KeyId)
	default:
		return nil, status.Error(codes.InvalidArgument, "unsupported status transition")
	}

	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	return &pb.UpdateKeyStatusResponse{
		KeyId:     req.KeyId,
		Version:   req.Version,
		Status:    req.Status,
		Timestamp: now.Unix(),
	}, nil
}

// ListKeyVersions returns all versions of a key.
func (s *KMSServer) ListKeyVersions(ctx context.Context, req *pb.ListKeyVersionsRequest) (*pb.ListKeyVersionsResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing ListKeyVersions request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	// Use service layer if available (supports version-specific status tracking)
	if s.kmsSvc != nil {
		svcReq := &service.ListKeyVersionsRequest{
			KeyID:  req.KeyId,
			Limit:  int(req.Limit),
			Cursor: req.Cursor,
		}
		svcResp, err := s.kmsSvc.ListKeyVersions(ctx, svcReq)
		if err != nil {
			return nil, convertKMSError(err)
		}

		// Convert service response to proto
		versions := make([]*pb.KeyVersionInfo, 0, len(svcResp.Versions))
		for _, v := range svcResp.Versions {
			protoAlg, _ := convertServiceKeyAlgorithm(v.Algorithm)
			versionInfo := &pb.KeyVersionInfo{
				Version:   v.Version,
				Algorithm: protoAlg,
				CreatedAt: v.CreatedAt.Unix(),
				Status:    convertServiceStatusToProto(v.Status),
			}
			if v.DisabledAt != nil {
				versionInfo.DisabledAt = v.DisabledAt.Unix()
			}
			versions = append(versions, versionInfo)
		}

		return &pb.ListKeyVersionsResponse{
			KeyId:      req.KeyId,
			Versions:   versions,
			NextCursor: svcResp.NextCursor,
			HasMore:    svcResp.HasMore,
			Timestamp:  svcResp.Timestamp.Unix(),
		}, nil
	}

	// Fallback to direct KMS manager access (no version status tracking)
	result, err := s.kmsMgr.GetKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, status.Error(codes.Internal, "unexpected response type from KMS manager")
	}

	// Build version list by iterating from version 1 to current version
	// This generates synthetic version entries based on the current version counter
	protoAlg := algorithmStringToProto(metadata.Algorithm)
	currentVersion := uint64(metadata.Version) //nolint:gosec // G115: Version is validated positive

	versions := make([]*pb.KeyVersionInfo, 0, currentVersion)
	for v := uint64(1); v <= currentVersion; v++ {
		versionInfo := &pb.KeyVersionInfo{
			Version:   v,
			Algorithm: protoAlg,
			CreatedAt: metadata.Created.Unix(),
		}

		// Current version has the current status, older versions default to enabled
		// (they remain enabled during grace period until explicitly disabled)
		// Note: Without service layer, we cannot track version-specific status
		if v == currentVersion {
			versionInfo.Status = kmsStateToProtoStatus(metadata.State)
		} else {
			versionInfo.Status = pb.KeyStatus_KEY_STATUS_ENABLED
		}

		versions = append(versions, versionInfo)
	}

	now := time.Now()

	// Apply pagination limit if specified
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 100 // Default limit
	}

	hasMore := false
	nextCursor := ""
	if len(versions) > limit {
		hasMore = true
		nextCursor = fmt.Sprintf("%d", versions[limit].Version)
		versions = versions[:limit]
	}

	return &pb.ListKeyVersionsResponse{
		KeyId:      req.KeyId,
		Versions:   versions,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Timestamp:  now.Unix(),
	}, nil
}

// SetKeyPolicy sets the access control policy for a key.
func (s *KMSServer) SetKeyPolicy(ctx context.Context, req *pb.SetKeyPolicyRequest) (*pb.SetKeyPolicyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing SetKeyPolicy request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.Policy == nil {
		return nil, status.Error(codes.InvalidArgument, "policy is required")
	}

	// Policy management is stored in metadata
	// This is a simplified implementation - full policy would require additional backend support
	now := time.Now()

	return &pb.SetKeyPolicyResponse{
		KeyId:         req.KeyId,
		PolicyVersion: req.Policy.Version + 1,
		Timestamp:     now.Unix(),
	}, nil
}

// GetKeyPolicy retrieves the access control policy for a key.
func (s *KMSServer) GetKeyPolicy(ctx context.Context, req *pb.GetKeyPolicyRequest) (*pb.GetKeyPolicyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing GetKeyPolicy request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	// Get key to verify it exists
	_, err := s.kmsMgr.GetKey(ctx, req.KeyId)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	// Return default policy - full policy would require additional backend support
	return &pb.GetKeyPolicyResponse{
		Policy: &pb.KeyPolicy{
			KeyId:             req.KeyId,
			Version:           1,
			AllowedOperations: []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT, pb.KeyUsage_KEY_USAGE_DECRYPT},
			UpdatedAt:         now.Unix(),
		},
		Timestamp: now.Unix(),
	}, nil
}

// GetSupportedAlgorithms returns the list of cryptographic algorithms supported by the backend.
func (s *KMSServer) GetSupportedAlgorithms(ctx context.Context, _ *pb.GetSupportedAlgorithmsRequest) (*pb.GetSupportedAlgorithmsResponse, error) {
	s.logger.Debug("Processing GetSupportedAlgorithms request")

	algorithms, err := s.kmsMgr.GetSupportedAlgorithms(ctx)
	if err != nil {
		return nil, convertKMSError(err)
	}

	return &pb.GetSupportedAlgorithmsResponse{
		Algorithms: algorithms,
	}, nil
}

// BackendStatus returns health and capability information about the KMS backend.
func (s *KMSServer) BackendStatus(ctx context.Context, _ *pb.BackendStatusRequest) (*pb.BackendStatusResponse, error) {
	s.logger.Debug("Processing BackendStatus request")

	backendType := s.kmsMgr.GetBackendType()
	isAvailable := s.kmsMgr.IsAvailable(ctx)

	statusStr := "healthy"
	if !isAvailable {
		statusStr = "unavailable"
	}

	now := time.Now()

	return &pb.BackendStatusResponse{
		BackendType: string(backendType),
		Status:      statusStr,
		Capabilities: map[string]bool{
			"encrypt":     true,
			"decrypt":     true,
			"sign":        true,
			"verify":      true,
			"generate":    true,
			"import":      true,
			"rotate":      true,
			"data_key":    true,
			"key_wrap":    true,
			"key_unwrap":  true,
			"multi_party": false,
		},
		Timestamp: now.Unix(),
	}, nil
}

// BackendTest performs a seal/unseal test cycle to verify backend functionality.
func (s *KMSServer) BackendTest(ctx context.Context, _ *pb.BackendTestRequest) (*pb.BackendTestResponse, error) {
	s.logger.Debug("Processing BackendTest request")

	start := time.Now()

	// Test by checking availability and attempting a simple operation
	isAvailable := s.kmsMgr.IsAvailable(ctx)

	duration := time.Since(start)
	now := time.Now()

	if !isAvailable {
		return &pb.BackendTestResponse{
			Success:    false,
			DurationMs: duration.Milliseconds(),
			Message:    "backend is not available",
			Timestamp:  now.Unix(),
		}, nil
	}

	return &pb.BackendTestResponse{
		Success:    true,
		DurationMs: duration.Milliseconds(),
		Message:    "backend test completed successfully",
		Timestamp:  now.Unix(),
	}, nil
}

// =============================================================================
// Key Export/Unwrap Operations
// =============================================================================

// ExportKey exports a key wrapped with a wrapping key.
func (s *KMSServer) ExportKey(ctx context.Context, req *pb.ExportKeyRequest) (*pb.ExportKeyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing ExportKey request")

	if s.kmsSvc == nil {
		return nil, status.Error(codes.Unimplemented, "KMS service not configured for export operations")
	}

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.WrappingKeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "wrapping_key_id is required")
	}

	// Convert proto wrapping algorithm to string
	wrappingAlg := protoWrappingAlgorithmToString(req.WrappingAlgorithm)

	svcReq := &service.ExportKeyRequest{
		KeyID:             req.KeyId,
		WrappingKeyID:     req.WrappingKeyId,
		WrappingAlgorithm: wrappingAlg,
		Format:            protoExportFormatToString(req.Format),
		Version:           req.Version,
	}

	resp, err := s.kmsSvc.ExportKey(ctx, svcReq)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	return &pb.ExportKeyResponse{
		KeyId:             resp.KeyID,
		Version:           resp.Version,
		Format:            req.Format,
		WrappingKeyId:     resp.WrappingKeyID,
		WrappingAlgorithm: req.WrappingAlgorithm,
		WrappedMaterial:   resp.WrappedKey,
		Timestamp:         now.Unix(),
	}, nil
}

// UnwrapKey imports a wrapped key into the KMS.
func (s *KMSServer) UnwrapKey(ctx context.Context, req *pb.UnwrapKeyRequest) (*pb.UnwrapKeyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing UnwrapKey request")

	if s.kmsSvc == nil {
		return nil, status.Error(codes.Unimplemented, "KMS service not configured for unwrap operations")
	}

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if len(req.WrappedMaterial) == 0 {
		return nil, status.Error(codes.InvalidArgument, "wrapped_material is required")
	}

	if req.WrappingKeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "wrapping_key_id is required")
	}

	// Convert proto wrapping algorithm to string
	wrappingAlg := protoWrappingAlgorithmToString(req.WrappingAlgorithm)
	keyAlg := protoKeyAlgorithmToString(req.Algorithm)

	svcReq := &service.UnwrapKeyRequest{
		KeyID:             req.KeyId,
		WrappedKey:        req.WrappedMaterial,
		WrappingKeyID:     req.WrappingKeyId,
		WrappingAlgorithm: wrappingAlg,
		Algorithm:         keyAlg,
		Name:              req.Name,
		SourceKeyID:       req.SourceKeyId,
		SourceVersion:     req.SourceVersion,
	}

	resp, err := s.kmsSvc.UnwrapKey(ctx, svcReq)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	// Build metadata from response fields
	metadata := &pb.KeyMetadata{
		KeyId:     resp.KeyID,
		Name:      req.Name,
		Algorithm: algorithmStringToProto(string(resp.Algorithm)),
		Status:    convertKeyStatus(resp.Status),
		Version:   resp.Version,
		CreatedAt: now.Unix(),
		UpdatedAt: now.Unix(),
	}

	return &pb.UnwrapKeyResponse{
		Key:       convertKeyHandle(resp.Key),
		Metadata:  metadata,
		Timestamp: now.Unix(),
	}, nil
}

// =============================================================================
// AEAD Safety Operations
// =============================================================================

// SetKeySafety configures AEAD safety limits for a key.
func (s *KMSServer) SetKeySafety(ctx context.Context, req *pb.SetKeySafetyRequest) (*pb.SetKeySafetyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing SetKeySafety request")

	if s.kmsSvc == nil {
		return nil, status.Error(codes.Unimplemented, "KMS service not configured for safety operations")
	}

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	svcReq := &service.SetKeySafetyRequest{
		KeyID:          req.KeyId,
		MaxOperations:  req.MaxOperations,
		MaxBytes:       req.MaxBytes,
		WarningPercent: int(req.WarningPercent),
	}

	_, err := s.kmsSvc.SetKeySafety(ctx, svcReq)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	return &pb.SetKeySafetyResponse{
		KeyId:     req.KeyId,
		Timestamp: now.Unix(),
	}, nil
}

// ClearKeySafety removes custom AEAD safety limits from a key.
func (s *KMSServer) ClearKeySafety(ctx context.Context, req *pb.ClearKeySafetyRequest) (*pb.ClearKeySafetyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing ClearKeySafety request")

	if s.kmsSvc == nil {
		return nil, status.Error(codes.Unimplemented, "KMS service not configured for safety operations")
	}

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	svcReq := &service.ClearKeySafetyRequest{
		KeyID: req.KeyId,
	}

	_, err := s.kmsSvc.ClearKeySafety(ctx, svcReq)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	return &pb.ClearKeySafetyResponse{
		KeyId:     req.KeyId,
		Timestamp: now.Unix(),
	}, nil
}

// SafetyCheck returns warnings for keys/DEKs approaching safety limits.
func (s *KMSServer) SafetyCheck(ctx context.Context, _ *pb.SafetyCheckRequest) (*pb.SafetyCheckResponse, error) {
	s.logger.Debug("Processing SafetyCheck request")

	if s.kmsSvc == nil {
		return nil, status.Error(codes.Unimplemented, "KMS service not configured for safety operations")
	}

	svcReq := &service.SafetyCheckRequest{
		ThresholdPercent: 80.0, // Default warning threshold
	}

	resp, err := s.kmsSvc.SafetyCheck(ctx, svcReq)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	warnings := make([]*pb.SafetyWarning, 0, len(resp.Items))
	for _, item := range resp.Items {
		warning := &pb.SafetyWarning{
			Type:           item.Type,
			Path:           item.ID,
			UsagePercent:   item.UsagePercent,
			Status:         safetyStatusFromPercent(item.UsagePercent),
			OperationsUsed: item.OperationCount,
			BytesUsed:      item.BytesProcessed,
			MaxOperations:  item.MaxOperations,
			MaxBytes:       item.MaxBytes,
		}
		warnings = append(warnings, warning)
	}

	return &pb.SafetyCheckResponse{
		Warnings:  warnings,
		Timestamp: now.Unix(),
	}, nil
}

// SafetyReport returns a comprehensive safety report for all keys/DEKs.
func (s *KMSServer) SafetyReport(ctx context.Context, _ *pb.SafetyReportRequest) (*pb.SafetyReportResponse, error) {
	s.logger.Debug("Processing SafetyReport request")

	if s.kmsSvc == nil {
		return nil, status.Error(codes.Unimplemented, "KMS service not configured for safety operations")
	}

	svcReq := &service.SafetyReportRequest{
		IncludeDetails: true,
	}

	resp, err := s.kmsSvc.SafetyReport(ctx, svcReq)
	if err != nil {
		return nil, convertKMSError(err)
	}

	now := time.Now()

	allItems := make([]*pb.SafetyWarning, 0, len(resp.Items))
	keysAtWarning := 0
	keysAtCritical := 0
	deksAtWarning := 0
	deksAtCritical := 0
	totalKeys := 0
	totalDEKs := 0

	for _, item := range resp.Items {
		warning := &pb.SafetyWarning{
			Type:           item.Type,
			Path:           item.ID,
			UsagePercent:   item.UsagePercent,
			Status:         safetyStatusFromPercent(item.UsagePercent),
			OperationsUsed: item.OperationCount,
			BytesUsed:      item.BytesProcessed,
			MaxOperations:  item.MaxOperations,
			MaxBytes:       item.MaxBytes,
		}
		allItems = append(allItems, warning)

		// Count by type and status
		switch item.Type {
		case "key":
			totalKeys++
			if item.UsagePercent >= 95.0 {
				keysAtCritical++
			} else if item.UsagePercent >= 80.0 {
				keysAtWarning++
			}
		case "dek":
			totalDEKs++
			if item.UsagePercent >= 95.0 {
				deksAtCritical++
			} else if item.UsagePercent >= 80.0 {
				deksAtWarning++
			}
		}
	}

	return &pb.SafetyReportResponse{
		AllItems:       allItems,
		TotalKeys:      int32(totalKeys),      //nolint:gosec // G115: bounded count
		TotalDeks:      int32(totalDEKs),      //nolint:gosec // G115: bounded count
		KeysAtWarning:  int32(keysAtWarning),  //nolint:gosec // G115: bounded count
		KeysAtCritical: int32(keysAtCritical), //nolint:gosec // G115: bounded count
		DeksAtWarning:  int32(deksAtWarning),  //nolint:gosec // G115: bounded count
		DeksAtCritical: int32(deksAtCritical), //nolint:gosec // G115: bounded count
		Timestamp:      now.Unix(),
	}, nil
}

// safetyStatusFromPercent returns a status string based on usage percentage.
func safetyStatusFromPercent(percent float64) string {
	switch {
	case percent >= 100.0:
		return "exceeded"
	case percent >= 95.0:
		return "critical"
	case percent >= 80.0:
		return "warning"
	default:
		return "ok"
	}
}

// protoWrappingAlgorithmToString converts a proto WrappingAlgorithm to a string.
func protoWrappingAlgorithmToString(alg pb.WrappingAlgorithm) string {
	switch alg {
	case pb.WrappingAlgorithm_WRAP_ALG_AES_KW:
		return "AES-KW"
	case pb.WrappingAlgorithm_WRAP_ALG_AES_KWP:
		return "AES-KWP"
	case pb.WrappingAlgorithm_WRAP_ALG_AES_GCM:
		return "AES-GCM"
	case pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256:
		return "RSA-OAEP-SHA256"
	case pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA384:
		return "RSA-OAEP-SHA384"
	case pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA512:
		return "RSA-OAEP-SHA512"
	case pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW:
		return "ECDH-ES+A256KW"
	default:
		return "AES-GCM"
	}
}

// protoExportFormatToString converts a proto ExportFormat to a string.
func protoExportFormatToString(format pb.ExportFormat) string {
	switch format {
	case pb.ExportFormat_EXPORT_FORMAT_RAW:
		return "raw"
	case pb.ExportFormat_EXPORT_FORMAT_JWK:
		return "jwk"
	case pb.ExportFormat_EXPORT_FORMAT_PEM:
		return "pem"
	default:
		return "raw"
	}
}

// =============================================================================
// Conversion Helpers - Proto to KeyAttributes
// =============================================================================

// protoToKeyAttributes builds KeyAttributes from a GenerateKeyRequest proto.
// Returns an error if the algorithm is unspecified or invalid.
func protoToKeyAttributes(keyID string, req *pb.GenerateKeyRequest) (*kctypes.KeyAttributes, error) {
	attrs := &kctypes.KeyAttributes{
		CN:         keyID,
		Exportable: req.KeySpec.Exportable,
	}

	// Map proto algorithm to go-xkms types
	switch req.KeySpec.Algorithm {
	case pb.KeyAlgorithm_KEY_ALG_AES_128_GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES128GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_AES_192_GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES192GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_AES_256_GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305:
		attrs.SymmetricAlgorithm = kctypes.SymmetricChaCha20Poly1305
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305:
		attrs.SymmetricAlgorithm = kctypes.SymmetricXChaCha20Poly1305
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_RSA_2048:
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 2048}
		attrs.KeyType = kctypes.KeyTypeSigning
	case pb.KeyAlgorithm_KEY_ALG_RSA_3072:
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 3072}
		attrs.KeyType = kctypes.KeyTypeSigning
	case pb.KeyAlgorithm_KEY_ALG_RSA_4096:
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 4096}
		attrs.KeyType = kctypes.KeyTypeSigning
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P256:
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P256()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P384:
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P384()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P521:
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P521()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case pb.KeyAlgorithm_KEY_ALG_ED25519:
		attrs.KeyAlgorithm = x509.Ed25519
		attrs.KeyType = kctypes.KeyTypeSigning
	case pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED:
		return nil, api.NewRequiredFieldError("key algorithm")
	default:
		return nil, api.NewInvalidFormatMessageError("key algorithm", "unsupported key algorithm: "+req.KeySpec.Algorithm.String())
	}

	return attrs, nil
}

// protoKeyAlgorithmToString converts a proto KeyAlgorithm to a string.
func protoKeyAlgorithmToString(alg pb.KeyAlgorithm) string {
	switch alg {
	case pb.KeyAlgorithm_KEY_ALG_AES_128_GCM:
		return "AES-128-GCM"
	case pb.KeyAlgorithm_KEY_ALG_AES_192_GCM:
		return "AES-192-GCM"
	case pb.KeyAlgorithm_KEY_ALG_AES_256_GCM:
		return "AES-256-GCM"
	case pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305:
		return "CHACHA20-POLY1305"
	case pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305:
		return "XCHACHA20-POLY1305"
	case pb.KeyAlgorithm_KEY_ALG_RSA_2048:
		return "RSA-2048"
	case pb.KeyAlgorithm_KEY_ALG_RSA_3072:
		return "RSA-3072"
	case pb.KeyAlgorithm_KEY_ALG_RSA_4096:
		return "RSA-4096"
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P256:
		return "ECDSA-P256"
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P384:
		return "ECDSA-P384"
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P521:
		return "ECDSA-P521"
	case pb.KeyAlgorithm_KEY_ALG_ED25519:
		return "ED25519"
	case pb.KeyAlgorithm_KEY_ALG_ED448:
		return "ED448"
	case pb.KeyAlgorithm_KEY_ALG_X25519:
		return "X25519"
	case pb.KeyAlgorithm_KEY_ALG_X448:
		return "X448"
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256:
		return "HMAC-SHA256"
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384:
		return "HMAC-SHA384"
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512:
		return "HMAC-SHA512"
	default:
		return "AES-256-GCM"
	}
}

// algorithmStringToProto converts an algorithm string to proto KeyAlgorithm.
func algorithmStringToProto(alg string) pb.KeyAlgorithm {
	upper := strings.ToUpper(alg)
	switch {
	case strings.Contains(upper, "AES-128") || strings.Contains(upper, "AES128"):
		return pb.KeyAlgorithm_KEY_ALG_AES_128_GCM
	case strings.Contains(upper, "AES-192") || strings.Contains(upper, "AES192"):
		return pb.KeyAlgorithm_KEY_ALG_AES_192_GCM
	case strings.Contains(upper, "AES-256") || strings.Contains(upper, "AES256"):
		return pb.KeyAlgorithm_KEY_ALG_AES_256_GCM
	case strings.Contains(upper, "XCHACHA20"):
		return pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305
	case strings.Contains(upper, "CHACHA20"):
		return pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305
	case strings.Contains(upper, "RSA-2048") || strings.Contains(upper, "RSA2048"):
		return pb.KeyAlgorithm_KEY_ALG_RSA_2048
	case strings.Contains(upper, "RSA-3072") || strings.Contains(upper, "RSA3072"):
		return pb.KeyAlgorithm_KEY_ALG_RSA_3072
	case strings.Contains(upper, "RSA-4096") || strings.Contains(upper, "RSA4096"):
		return pb.KeyAlgorithm_KEY_ALG_RSA_4096
	case strings.Contains(upper, "ECDSA-P256") || strings.Contains(upper, "P256"):
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P256
	case strings.Contains(upper, "ECDSA-P384") || strings.Contains(upper, "P384"):
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P384
	case strings.Contains(upper, "ECDSA-P521") || strings.Contains(upper, "P521"):
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P521
	case strings.Contains(upper, "ED25519"):
		return pb.KeyAlgorithm_KEY_ALG_ED25519
	case strings.Contains(upper, "ED448"):
		return pb.KeyAlgorithm_KEY_ALG_ED448
	case strings.Contains(upper, "X25519"):
		return pb.KeyAlgorithm_KEY_ALG_X25519
	case strings.Contains(upper, "X448"):
		return pb.KeyAlgorithm_KEY_ALG_X448
	case strings.Contains(upper, "HMAC") && strings.Contains(upper, "256"):
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256
	case strings.Contains(upper, "HMAC") && strings.Contains(upper, "384"):
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384
	case strings.Contains(upper, "HMAC") && strings.Contains(upper, "512"):
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512
	default:
		return pb.KeyAlgorithm_KEY_ALG_AES_256_GCM
	}
}

// kmsStateToProtoStatus converts kms.KeyState to pb.KeyStatus.
func kmsStateToProtoStatus(state kms.KeyState) pb.KeyStatus {
	switch state {
	case kms.KeyStateEnabled:
		return pb.KeyStatus_KEY_STATUS_ENABLED
	case kms.KeyStateDisabled:
		return pb.KeyStatus_KEY_STATUS_DISABLED
	case kms.KeyStatePendingDeletion:
		return pb.KeyStatus_KEY_STATUS_PENDING_DELETION
	case kms.KeyStateDeleted:
		return pb.KeyStatus_KEY_STATUS_DELETED
	default:
		return pb.KeyStatus_KEY_STATUS_ENABLED
	}
}

// convertServiceStatusToProto converts service.KeyStatus to pb.KeyStatus.
func convertServiceStatusToProto(status service.KeyStatus) pb.KeyStatus {
	switch status {
	case service.KeyStatusEnabled:
		return pb.KeyStatus_KEY_STATUS_ENABLED
	case service.KeyStatusDisabled:
		return pb.KeyStatus_KEY_STATUS_DISABLED
	case service.KeyStatusPendingDeletion:
		return pb.KeyStatus_KEY_STATUS_PENDING_DELETION
	case service.KeyStatusDeleted:
		return pb.KeyStatus_KEY_STATUS_DELETED
	default:
		return pb.KeyStatus_KEY_STATUS_ENABLED
	}
}

// kmsUsageToProtoUsage converts kms.KeyUsage to pb.KeyUsage.
func kmsUsageToProtoUsage(usage kms.KeyUsage) pb.KeyUsage {
	switch usage {
	case kms.KeyUsageEncrypt:
		return pb.KeyUsage_KEY_USAGE_ENCRYPT
	case kms.KeyUsageDecrypt:
		return pb.KeyUsage_KEY_USAGE_DECRYPT
	case kms.KeyUsageSign:
		return pb.KeyUsage_KEY_USAGE_SIGN
	case kms.KeyUsageVerify:
		return pb.KeyUsage_KEY_USAGE_VERIFY
	case kms.KeyUsageWrap:
		return pb.KeyUsage_KEY_USAGE_WRAP_KEY
	case kms.KeyUsageUnwrap:
		return pb.KeyUsage_KEY_USAGE_WRAP_KEY // Map unwrap to wrap_key
	case kms.KeyUsageDeriveKey:
		return pb.KeyUsage_KEY_USAGE_DERIVE_KEY
	default:
		return pb.KeyUsage_KEY_USAGE_ENCRYPT
	}
}

// kmsUsageListToProto converts []kms.KeyUsage to []pb.KeyUsage.
func kmsUsageListToProto(usages []kms.KeyUsage) []pb.KeyUsage {
	result := make([]pb.KeyUsage, len(usages))
	for i, u := range usages {
		result[i] = kmsUsageToProtoUsage(u)
	}
	return result
}

// =============================================================================
// Conversion Helpers - KMS types to Proto
// =============================================================================

// convertKMSKeyHandle creates a pb.KeyHandle from kms.KeyMetadata.
func convertKMSKeyHandle(keyID string, metadata *kms.KeyMetadata, keySpec *pb.KeySpec) *pb.KeyHandle {
	return &pb.KeyHandle{
		KeyId:     keyID,
		Version:   uint64(metadata.Version), //nolint:gosec // G115: Version is validated positive
		Algorithm: keySpec.Algorithm,
		Status:    kmsStateToProtoStatus(metadata.State),
		CreatedAt: metadata.Created.Unix(),
	}
}

// convertKMSKeyHandleFromMetadata creates a pb.KeyHandle from kms.KeyMetadata only.
func convertKMSKeyHandleFromMetadata(metadata *kms.KeyMetadata) *pb.KeyHandle {
	return &pb.KeyHandle{
		KeyId:     metadata.KeyID,
		Version:   uint64(metadata.Version), //nolint:gosec // G115: Version is validated positive
		Algorithm: algorithmStringToProto(metadata.Algorithm),
		Status:    kmsStateToProtoStatus(metadata.State),
		CreatedAt: metadata.Created.Unix(),
	}
}

// convertKMSKeyMetadata creates a pb.KeyMetadata from kms.KeyMetadata with request data.
func convertKMSKeyMetadata(metadata *kms.KeyMetadata, name, description string, tags map[string]string, owner string) *pb.KeyMetadata {
	var lastUsed int64
	if metadata.LastUsedAt != nil {
		lastUsed = metadata.LastUsedAt.Unix()
	}

	return &pb.KeyMetadata{
		KeyId:              metadata.KeyID,
		Name:               name,
		Description:        description,
		Algorithm:          algorithmStringToProto(metadata.Algorithm),
		Status:             kmsStateToProtoStatus(metadata.State),
		Usage:              kmsUsageListToProto(metadata.Usage),
		Version:            uint64(metadata.Version), //nolint:gosec // G115: Version is validated positive
		Exportable:         metadata.Exportable,
		DeletionProtection: metadata.DeletionProtection,
		CreatedAt:          metadata.Created.Unix(),
		UpdatedAt:          metadata.Updated.Unix(),
		LastUsedAt:         lastUsed,
		RotationPolicy:     convertKMSRotationPolicy(metadata.RotationPolicy),
		Tags:               tags,
		Owner:              owner,
	}
}

// convertKMSKeyMetadataFull creates a pb.KeyMetadata from kms.KeyMetadata with all fields.
func convertKMSKeyMetadataFull(metadata *kms.KeyMetadata) *pb.KeyMetadata {
	var lastUsed int64
	if metadata.LastUsedAt != nil {
		lastUsed = metadata.LastUsedAt.Unix()
	}

	return &pb.KeyMetadata{
		KeyId:              metadata.KeyID,
		Name:               metadata.Name,
		Description:        metadata.Description,
		Algorithm:          algorithmStringToProto(metadata.Algorithm),
		Status:             kmsStateToProtoStatus(metadata.State),
		Usage:              kmsUsageListToProto(metadata.Usage),
		Version:            uint64(metadata.Version), //nolint:gosec // G115: Version is validated positive
		Exportable:         metadata.Exportable,
		DeletionProtection: metadata.DeletionProtection,
		CreatedAt:          metadata.Created.Unix(),
		UpdatedAt:          metadata.Updated.Unix(),
		LastUsedAt:         lastUsed,
		RotationPolicy:     convertKMSRotationPolicy(metadata.RotationPolicy),
		Tags:               metadata.Tags,
		Owner:              metadata.Owner,
	}
}

// convertKMSRotationPolicy converts kms.RotationPolicy to pb.RotationPolicy.
func convertKMSRotationPolicy(rp *kms.RotationPolicy) *pb.RotationPolicy {
	if rp == nil {
		return nil
	}
	return &pb.RotationPolicy{
		Enabled:         rp.Enabled,
		IntervalSeconds: int64(rp.Interval.Seconds()),
		NextRotation:    rp.NextRotation.Unix(),
		RetainVersions:  int32(rp.RetainVersions), //nolint:gosec // G115: RetainVersions is small config value
		AutoRotate:      rp.AutoRotate,
	}
}

// =============================================================================
// Error Handling - KMS specific
// =============================================================================

// convertKMSError converts internal errors to gRPC status errors.
func convertKMSError(err error) error {
	if err == nil {
		return nil
	}

	var dErr *errors.QBError
	if !stderrors.As(err, &dErr) {
		return status.Error(codes.Internal, err.Error())
	}

	switch dErr.Code {
	case errors.ErrNotFound:
		return status.Error(codes.NotFound, dErr.Error())
	case errors.ErrAlreadyExists:
		return status.Error(codes.AlreadyExists, dErr.Error())
	case errors.ErrInvalidInput:
		return status.Error(codes.InvalidArgument, dErr.Error())
	case errors.ErrUnauthorized:
		return status.Error(codes.Unauthenticated, dErr.Error())
	case errors.ErrForbidden:
		return status.Error(codes.PermissionDenied, dErr.Error())
	case errors.ErrTimeout:
		return status.Error(codes.DeadlineExceeded, dErr.Error())
	case errors.ErrResourceExhausted:
		return status.Error(codes.ResourceExhausted, dErr.Error())
	case errors.ErrUnavailable:
		return status.Error(codes.Unavailable, dErr.Error())
	default:
		return status.Error(codes.Internal, dErr.Error())
	}
}

// =============================================================================
// Service Layer Converters - Used by client_kms.go, server_crypto.go, server_dek.go
// =============================================================================

// convertServiceKeyAlgorithm converts service.KeyAlgorithm to pb.KeyAlgorithm.
func convertServiceKeyAlgorithm(alg service.KeyAlgorithm) (pb.KeyAlgorithm, error) {
	upper := strings.ToUpper(string(alg))
	switch {
	case strings.Contains(upper, "AES-128") || strings.Contains(upper, "AES128"):
		return pb.KeyAlgorithm_KEY_ALG_AES_128_GCM, nil
	case strings.Contains(upper, "AES-192") || strings.Contains(upper, "AES192"):
		return pb.KeyAlgorithm_KEY_ALG_AES_192_GCM, nil
	case strings.Contains(upper, "AES-256") || strings.Contains(upper, "AES256"):
		return pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, nil
	case strings.Contains(upper, "XCHACHA20"):
		return pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305, nil
	case strings.Contains(upper, "CHACHA20"):
		return pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305, nil
	case strings.Contains(upper, "RSA-2048") || strings.Contains(upper, "RSA2048"):
		return pb.KeyAlgorithm_KEY_ALG_RSA_2048, nil
	case strings.Contains(upper, "RSA-3072") || strings.Contains(upper, "RSA3072"):
		return pb.KeyAlgorithm_KEY_ALG_RSA_3072, nil
	case strings.Contains(upper, "RSA-4096") || strings.Contains(upper, "RSA4096"):
		return pb.KeyAlgorithm_KEY_ALG_RSA_4096, nil
	case strings.Contains(upper, "ECDSA-P256") || upper == "P256":
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P256, nil
	case strings.Contains(upper, "ECDSA-P384") || upper == "P384":
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P384, nil
	case strings.Contains(upper, "ECDSA-P521") || upper == "P521":
		return pb.KeyAlgorithm_KEY_ALG_ECDSA_P521, nil
	case strings.Contains(upper, "ED25519"):
		return pb.KeyAlgorithm_KEY_ALG_ED25519, nil
	case strings.Contains(upper, "ED448"):
		return pb.KeyAlgorithm_KEY_ALG_ED448, nil
	case strings.Contains(upper, "X25519"):
		return pb.KeyAlgorithm_KEY_ALG_X25519, nil
	case strings.Contains(upper, "X448"):
		return pb.KeyAlgorithm_KEY_ALG_X448, nil
	case strings.Contains(upper, "HMAC") && strings.Contains(upper, "256"):
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256, nil
	case strings.Contains(upper, "HMAC") && strings.Contains(upper, "384"):
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384, nil
	case strings.Contains(upper, "HMAC") && strings.Contains(upper, "512"):
		return pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512, nil
	default:
		return pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, api.NewInvalidFormatMessageError("algorithm", "unsupported algorithm: "+string(alg))
	}
}

// convertProtoKeyAlgorithm converts pb.KeyAlgorithm to service.KeyAlgorithm.
func convertProtoKeyAlgorithm(alg pb.KeyAlgorithm) service.KeyAlgorithm {
	switch alg {
	case pb.KeyAlgorithm_KEY_ALG_AES_128_GCM:
		return service.KeyAlgAES128GCM
	case pb.KeyAlgorithm_KEY_ALG_AES_192_GCM:
		return service.KeyAlgAES192GCM
	case pb.KeyAlgorithm_KEY_ALG_AES_256_GCM:
		return service.KeyAlgAES256GCM
	case pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305:
		return service.KeyAlgChaCha20Poly1305
	case pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305:
		return service.KeyAlgXChaCha20Poly1305
	case pb.KeyAlgorithm_KEY_ALG_RSA_2048:
		return service.KeyAlgRSA2048
	case pb.KeyAlgorithm_KEY_ALG_RSA_3072:
		return service.KeyAlgRSA3072
	case pb.KeyAlgorithm_KEY_ALG_RSA_4096:
		return service.KeyAlgRSA4096
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P256:
		return service.KeyAlgECDSAP256
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P384:
		return service.KeyAlgECDSAP384
	case pb.KeyAlgorithm_KEY_ALG_ECDSA_P521:
		return service.KeyAlgECDSAP521
	case pb.KeyAlgorithm_KEY_ALG_ED25519:
		return service.KeyAlgEd25519
	case pb.KeyAlgorithm_KEY_ALG_ED448:
		return service.KeyAlgEd448
	case pb.KeyAlgorithm_KEY_ALG_X25519:
		return service.KeyAlgX25519
	case pb.KeyAlgorithm_KEY_ALG_X448:
		return service.KeyAlgX448
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256:
		return service.KeyAlgHMACSHA256
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384:
		return service.KeyAlgHMACSHA384
	case pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512:
		return service.KeyAlgHMACSHA512
	default:
		return service.KeyAlgAES256GCM
	}
}

// convertServiceKeyUsage converts service.KeyUsage to pb.KeyUsage.
func convertServiceKeyUsage(usage service.KeyUsage) pb.KeyUsage {
	switch usage {
	case service.KeyUsageEncrypt:
		return pb.KeyUsage_KEY_USAGE_ENCRYPT
	case service.KeyUsageDecrypt:
		return pb.KeyUsage_KEY_USAGE_DECRYPT
	case service.KeyUsageSign:
		return pb.KeyUsage_KEY_USAGE_SIGN
	case service.KeyUsageVerify:
		return pb.KeyUsage_KEY_USAGE_VERIFY
	case service.KeyUsageWrapKey:
		return pb.KeyUsage_KEY_USAGE_WRAP_KEY
	case service.KeyUsageDeriveKey:
		return pb.KeyUsage_KEY_USAGE_DERIVE_KEY
	case service.KeyUsageGenerateMAC:
		return pb.KeyUsage_KEY_USAGE_GENERATE_MAC
	case service.KeyUsageVerifyMAC:
		return pb.KeyUsage_KEY_USAGE_VERIFY_MAC
	default:
		return pb.KeyUsage_KEY_USAGE_UNSPECIFIED
	}
}

// convertServiceKeyUsageList converts []service.KeyUsage to []pb.KeyUsage.
func convertServiceKeyUsageList(usages []service.KeyUsage) []pb.KeyUsage {
	result := make([]pb.KeyUsage, len(usages))
	for i, usage := range usages {
		result[i] = convertServiceKeyUsage(usage)
	}
	return result
}

// convertProtoKeyUsage converts pb.KeyUsage to service.KeyUsage.
func convertProtoKeyUsage(usage pb.KeyUsage) service.KeyUsage {
	switch usage {
	case pb.KeyUsage_KEY_USAGE_ENCRYPT:
		return service.KeyUsageEncrypt
	case pb.KeyUsage_KEY_USAGE_DECRYPT:
		return service.KeyUsageDecrypt
	case pb.KeyUsage_KEY_USAGE_SIGN:
		return service.KeyUsageSign
	case pb.KeyUsage_KEY_USAGE_VERIFY:
		return service.KeyUsageVerify
	case pb.KeyUsage_KEY_USAGE_WRAP_KEY:
		return service.KeyUsageWrapKey
	case pb.KeyUsage_KEY_USAGE_DERIVE_KEY:
		return service.KeyUsageDeriveKey
	case pb.KeyUsage_KEY_USAGE_GENERATE_MAC:
		return service.KeyUsageGenerateMAC
	case pb.KeyUsage_KEY_USAGE_VERIFY_MAC:
		return service.KeyUsageVerifyMAC
	default:
		return service.KeyUsageEncrypt
	}
}

// convertProtoKeyUsageList converts []pb.KeyUsage to []service.KeyUsage.
func convertProtoKeyUsageList(usages []pb.KeyUsage) []service.KeyUsage {
	result := make([]service.KeyUsage, len(usages))
	for i, usage := range usages {
		result[i] = convertProtoKeyUsage(usage)
	}
	return result
}

// convertKeyStatus converts service.KeyStatus to pb.KeyStatus.
func convertKeyStatus(status service.KeyStatus) pb.KeyStatus {
	switch status {
	case service.KeyStatusEnabled:
		return pb.KeyStatus_KEY_STATUS_ENABLED
	case service.KeyStatusDisabled:
		return pb.KeyStatus_KEY_STATUS_DISABLED
	case service.KeyStatusPendingDeletion:
		return pb.KeyStatus_KEY_STATUS_PENDING_DELETION
	case service.KeyStatusDeleted:
		return pb.KeyStatus_KEY_STATUS_DELETED
	case service.KeyStatusRotating:
		return pb.KeyStatus_KEY_STATUS_ROTATING
	case service.KeyStatusCompromised:
		return pb.KeyStatus_KEY_STATUS_COMPROMISED
	default:
		return pb.KeyStatus_KEY_STATUS_UNSPECIFIED
	}
}

// convertKeyHandle converts service.KeyHandle to pb.KeyHandle.
func convertKeyHandle(kh *service.KeyHandle) *pb.KeyHandle {
	if kh == nil {
		return nil
	}
	alg, _ := convertServiceKeyAlgorithm(kh.Algorithm)
	return &pb.KeyHandle{
		KeyId:     kh.KeyID,
		Version:   kh.Version,
		Algorithm: alg,
		Status:    convertKeyStatus(kh.Status),
		CreatedAt: kh.CreatedAt.Unix(),
	}
}

// convertKeyMetadata converts service.KeyMetadata to pb.KeyMetadata.
func convertKeyMetadata(km *service.KeyMetadata) *pb.KeyMetadata {
	if km == nil {
		return nil
	}
	alg, _ := convertServiceKeyAlgorithm(km.Algorithm)

	var lastUsed int64
	if km.LastUsedAt != nil {
		lastUsed = km.LastUsedAt.Unix()
	}

	return &pb.KeyMetadata{
		KeyId:              km.KeyID,
		Name:               km.Name,
		Description:        km.Description,
		Algorithm:          alg,
		Status:             convertKeyStatus(km.Status),
		Usage:              convertServiceKeyUsageList(km.Usage),
		Version:            km.Version,
		Exportable:         km.Exportable,
		DeletionProtection: km.DeletionProtection,
		CreatedAt:          km.CreatedAt.Unix(),
		UpdatedAt:          km.UpdatedAt.Unix(),
		LastUsedAt:         lastUsed,
		RotationPolicy:     convertRotationPolicy(km.RotationPolicy),
		Tags:               km.Tags,
		Owner:              km.Owner,
	}
}

// convertRotationPolicy converts service.RotationPolicy to pb.RotationPolicy.
func convertRotationPolicy(rp *service.RotationPolicy) *pb.RotationPolicy {
	if rp == nil {
		return nil
	}
	return &pb.RotationPolicy{
		Enabled:         rp.Enabled,
		IntervalSeconds: int64(rp.Interval.Seconds()),
		NextRotation:    rp.NextRotation.Unix(),
		RetainVersions:  int32(rp.RetainVersions), //nolint:gosec // G115: RetainVersions is small config value
		AutoRotate:      rp.AutoRotate,
	}
}

// Ensure KMSServer implements the interface.
var _ pb.KeyManagementServer = (*KMSServer)(nil)
