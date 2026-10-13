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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
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

	dek, err := convertDEKMetadata(resp.DEK)
	if err != nil {
		return nil, err
	}

	return &pb.CreateDEKResponse{
		Dek:       dek,
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

	dek, err := convertDEKMetadata(resp.DEK)
	if err != nil {
		return nil, err
	}

	return &pb.GetDEKResponse{
		Dek:       dek,
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
		dek, err := convertDEKMetadata(d)
		if err != nil {
			return nil, err
		}
		deks[i] = dek
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

	dek, err := convertDEKMetadata(resp.DEK)
	if err != nil {
		return nil, err
	}

	return &pb.GetDEKStatusResponse{
		Dek:                  dek,
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

	return &pb.StartReencryptionResponse{
		OperationId: resp.OperationID,
		Path:        req.Path,
		Started:     true,
		// estimated_items is an estimate on an int32 wire field, so a larger
		// entity count saturates rather than failing an operation that has
		// already started.
		EstimatedItems: conv.ClampInt64ToInt32(resp.TotalEntities),
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

func convertDEKMetadata(d *service.DEKMetadata) (*pb.DEKMetadata, error) {
	if d == nil {
		return nil, nil
	}

	warningPercent, err := responseInt32("warning_percent", d.WarningPercent)
	if err != nil {
		return nil, err
	}

	var rotatedAt int64
	if d.RotatedAt != nil {
		rotatedAt = d.RotatedAt.Unix()
	}

	var n responseNarrower
	alg := n.keyAlgorithm("algorithm", d.Algorithm)
	if err := n.err(); err != nil {
		return nil, err
	}

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
		WarningPercent: warningPercent,
		Description:    d.Description,
		BackendType:    d.BackendType,
	}, nil
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

	// The progress counters are int32 on the wire. A job beyond that size
	// reports saturated counts instead of failing the progress query, so
	// callers can still see that it is running.
	return &pb.ReencryptionProgress{
		InProgress:          p.InProgress,
		TotalKeys:           conv.ClampUint64ToInt32(p.TotalKeys),
		ProcessedKeys:       conv.ClampUint64ToInt32(p.ProcessedKeys),
		FailedKeys:          conv.ClampUint64ToInt32(p.FailedKeys),
		StartedAt:           startedAt,
		EstimatedCompletion: estimatedCompletion,
	}
}

// Ensure DEKServer implements the interface.
var _ pb.DEKManagementServer = (*DEKServer)(nil)
