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
	"math"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// DEKServer implements the DEKManagement gRPC service.
type DEKServer struct {
	pb.UnimplementedDEKManagementServer
	kmsSvc service.KMSService
	logger *logger.Logger
}

// NewDEKServer creates a new gRPC server for DEK operations.
func NewDEKServer(kmsSvc service.KMSService, log *logger.Logger) *DEKServer {
	return &DEKServer{
		kmsSvc: kmsSvc,
		logger: log.WithField("component", "dek-grpc-server"),
	}
}

// CreateDEK creates a new Data Encryption Key at the specified path.
func (s *DEKServer) CreateDEK(ctx context.Context, req *pb.CreateDEKRequest) (*pb.CreateDEKResponse, error) {
	s.logger.WithField("path", req.Path).Debug("Processing CreateDEK request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.CreateDEKRequest{
		Path:           req.Path,
		Algorithm:      convertProtoKeyAlgorithm(req.Algorithm),
		Description:    req.Description,
		MaxOperations:  req.MaxOperations,
		MaxBytes:       req.MaxBytes,
		WarningPercent: int(req.WarningPercent),
	}

	resp, err := s.kmsSvc.CreateDEK(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.CreateDEKResponse{
		Dek:       convertDEKMetadata(resp.DEK),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetDEK retrieves DEK metadata by path.
func (s *DEKServer) GetDEK(ctx context.Context, req *pb.GetDEKRequest) (*pb.GetDEKResponse, error) {
	s.logger.WithField("path", req.Path).Debug("Processing GetDEK request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.GetDEKRequest{
		Path: req.Path,
	}

	resp, err := s.kmsSvc.GetDEK(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GetDEKResponse{
		Dek:       convertDEKMetadata(resp.DEK),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// ListDEKs returns all DEKs matching the optional path prefix.
func (s *DEKServer) ListDEKs(ctx context.Context, req *pb.ListDEKsRequest) (*pb.ListDEKsResponse, error) {
	s.logger.Debug("Processing ListDEKs request")

	svcReq := &service.ListDEKsRequest{
		Prefix:    req.Prefix,
		Recursive: req.Recursive,
		Limit:     int(req.Limit),
		Cursor:    req.Cursor,
	}

	resp, err := s.kmsSvc.ListDEKs(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	deks := make([]*pb.DEKMetadata, len(resp.DEKs))
	for i, d := range resp.DEKs {
		deks[i] = convertDEKMetadata(d)
	}

	return &pb.ListDEKsResponse{
		Deks:       deks,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// RotateDEK creates a new version of an existing DEK.
func (s *DEKServer) RotateDEK(ctx context.Context, req *pb.RotateDEKRequest) (*pb.RotateDEKResponse, error) {
	s.logger.WithField("path", req.Path).Info("Processing RotateDEK request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.RotateDEKRequest{
		Path:       req.Path,
		Background: req.Background,
	}

	resp, err := s.kmsSvc.RotateDEK(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.RotateDEKResponse{
		Path:       resp.Path,
		OldVersion: resp.OldVersion,
		NewVersion: resp.NewVersion,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// DeleteDEK removes a DEK (requires force or migration).
func (s *DEKServer) DeleteDEK(ctx context.Context, req *pb.DeleteDEKRequest) (*pb.DeleteDEKResponse, error) {
	s.logger.WithField("path", req.Path).Info("Processing DeleteDEK request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.DeleteDEKRequest{
		Path:      req.Path,
		Force:     req.Force,
		MigrateTo: req.MigrateTo,
	}

	resp, err := s.kmsSvc.DeleteDEK(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.DeleteDEKResponse{
		Path:      resp.Path,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetDEKStatus retrieves detailed DEK status including AEAD usage.
func (s *DEKServer) GetDEKStatus(ctx context.Context, req *pb.GetDEKStatusRequest) (*pb.GetDEKStatusResponse, error) {
	s.logger.WithField("path", req.Path).Debug("Processing GetDEKStatus request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.GetDEKStatusRequest{
		Path: req.Path,
	}

	resp, err := s.kmsSvc.GetDEKStatus(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GetDEKStatusResponse{
		Dek:                  convertDEKMetadata(resp.DEK),
		ReencryptionProgress: convertReencryptionProgress(resp.ReencryptionProgress),
		Timestamp:            resp.Timestamp.Unix(),
	}, nil
}

// SetDefaultDEK marks a DEK as the default for its path level.
func (s *DEKServer) SetDefaultDEK(ctx context.Context, req *pb.SetDefaultDEKRequest) (*pb.SetDefaultDEKResponse, error) {
	s.logger.WithField("path", req.Path).Info("Processing SetDefaultDEK request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.SetDefaultDEKRequest{
		Path: req.Path,
	}

	resp, err := s.kmsSvc.SetDefaultDEK(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.SetDefaultDEKResponse{
		Path:            resp.Path,
		PreviousDefault: resp.PreviousDefault,
		Timestamp:       resp.Timestamp.Unix(),
	}, nil
}

// SetDEKSafety configures AEAD safety limits for a DEK.
func (s *DEKServer) SetDEKSafety(ctx context.Context, req *pb.SetDEKSafetyRequest) (*pb.SetDEKSafetyResponse, error) {
	s.logger.WithField("path", req.Path).Debug("Processing SetDEKSafety request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.SetDEKSafetyRequest{
		Path:           req.Path,
		MaxOperations:  req.MaxOperations,
		MaxBytes:       req.MaxBytes,
		WarningPercent: int(req.WarningPercent),
	}

	resp, err := s.kmsSvc.SetDEKSafety(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.SetDEKSafetyResponse{
		Path:      resp.Path,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// ClearDEKSafety removes custom AEAD safety limits from a DEK.
func (s *DEKServer) ClearDEKSafety(ctx context.Context, req *pb.ClearDEKSafetyRequest) (*pb.ClearDEKSafetyResponse, error) {
	s.logger.WithField("path", req.Path).Debug("Processing ClearDEKSafety request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.ClearDEKSafetyRequest{
		Path: req.Path,
	}

	resp, err := s.kmsSvc.ClearDEKSafety(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.ClearDEKSafetyResponse{
		Path:      resp.Path,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// StartReencryption begins background re-encryption of data with new DEK version.
func (s *DEKServer) StartReencryption(ctx context.Context, req *pb.StartReencryptionRequest) (*pb.StartReencryptionResponse, error) {
	s.logger.WithField("path", req.Path).Info("Processing StartReencryption request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &service.StartReencryptionRequest{
		FromVersion: req.FromVersion,
		ToVersion:   req.ToVersion,
		BatchSize:   int(req.BatchSize),
	}

	resp, err := s.kmsSvc.StartReencryption(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	// Bounds check for int64 to int32 conversion
	estimatedItems := resp.TotalEntities
	if estimatedItems > math.MaxInt32 {
		estimatedItems = math.MaxInt32
	}

	return &pb.StartReencryptionResponse{
		OperationId:    resp.OperationID,
		Path:           req.Path,
		Started:        true,
		EstimatedItems: int32(estimatedItems), //nolint:gosec // G115: bounds checked above
		Timestamp:      resp.StartedAt.Unix(),
	}, nil
}

// GetReencryptionProgress returns the current progress of re-encryption operation.
func (s *DEKServer) GetReencryptionProgress(ctx context.Context, req *pb.GetReencryptionProgressRequest) (*pb.GetReencryptionProgressResponse, error) {
	s.logger.WithField("path", req.Path).Debug("Processing GetReencryptionProgress request")

	svcReq := &service.GetReencryptionProgressRequest{
		Verbose: true,
	}

	resp, err := s.kmsSvc.GetReencryptionProgress(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	// Determine status from progress
	status := "not_started"
	if resp.Progress != nil {
		status = resp.Progress.Status
		if status == "" {
			switch {
			case resp.Progress.InProgress:
				status = "running"
			case resp.Progress.ProcessedKeys >= resp.Progress.TotalKeys && resp.Progress.TotalKeys > 0:
				status = "completed"
			case resp.Progress.FailedKeys > 0:
				status = "failed"
			}
		}
	}

	return &pb.GetReencryptionProgressResponse{
		Path:        req.Path,
		OperationId: req.OperationId,
		Progress:    convertReencryptionProgress(resp.Progress),
		Status:      status,
		Error:       "",
		Timestamp:   resp.Timestamp.Unix(),
	}, nil
}

// =============================================================================
// Conversion Helpers
// =============================================================================

func convertDEKMetadata(d *service.DEKMetadata) *pb.DEKMetadata {
	if d == nil {
		return nil
	}

	var rotatedAt int64
	if d.RotatedAt != nil {
		rotatedAt = d.RotatedAt.Unix()
	}

	// Algorithm conversion should not fail for validated service data
	alg, _ := convertServiceKeyAlgorithm(d.Algorithm)
	return &pb.DEKMetadata{
		Path:           d.Path,
		Algorithm:      alg,
		Version:        d.Version,
		Status:         d.Status,
		IsDefault:      d.IsDefault,
		CreatedAt:      d.CreatedAt.Unix(),
		RotatedAt:      rotatedAt,
		UsageCount:     d.UsageCount,
		BytesUsed:      d.BytesUsed,
		MaxUsageCount:  d.MaxUsageCount,
		MaxBytesUsed:   d.MaxBytesUsed,
		WarningPercent: int32(d.WarningPercent), //nolint:gosec // G115: WarningPercent is 0-100
		Description:    d.Description,
		BackendType:    d.BackendType,
	}
}

func convertReencryptionProgress(p *service.ReencryptionProgress) *pb.ReencryptionProgress {
	if p == nil {
		return nil
	}

	var startedAt int64
	if p.StartedAt != nil {
		startedAt = p.StartedAt.Unix()
	}

	var estimatedCompletion int64
	if p.EstimatedCompletion != nil {
		estimatedCompletion = p.EstimatedCompletion.Unix()
	}

	// Bounds check for uint64 to int32 conversions
	totalKeys := p.TotalKeys
	if totalKeys > math.MaxInt32 {
		totalKeys = math.MaxInt32
	}
	processedKeys := p.ProcessedKeys
	if processedKeys > math.MaxInt32 {
		processedKeys = math.MaxInt32
	}
	failedKeys := p.FailedKeys
	if failedKeys > math.MaxInt32 {
		failedKeys = math.MaxInt32
	}

	return &pb.ReencryptionProgress{
		InProgress:          p.InProgress,
		TotalKeys:           int32(totalKeys),     //nolint:gosec // G115: bounds checked above
		ProcessedKeys:       int32(processedKeys), //nolint:gosec // G115: bounds checked above
		FailedKeys:          int32(failedKeys),    //nolint:gosec // G115: bounds checked above
		StartedAt:           startedAt,
		EstimatedCompletion: estimatedCompletion,
	}
}

// Ensure DEKServer implements the interface.
var _ pb.DEKManagementServer = (*DEKServer)(nil)
