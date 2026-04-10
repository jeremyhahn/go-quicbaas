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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ParameterStoreServer wraps the ParameterStore service for gRPC endpoints.
type ParameterStoreServer struct {
	pb.UnimplementedParameterStoreServer
	parameterSvc service.ParameterStoreService
	logger       *logger.Logger
}

// NewParameterStoreServer creates a new gRPC server for ParameterStore operations.
func NewParameterStoreServer(
	parameterSvc service.ParameterStoreService,
	log *logger.Logger,
) *ParameterStoreServer {
	return &ParameterStoreServer{
		parameterSvc: parameterSvc,
		logger:       log.WithField("component", "parameter-store-grpc-server"),
	}
}

// GetParameter retrieves a single parameter by path.
func (s *ParameterStoreServer) GetParameter(
	ctx context.Context,
	req *pb.GetParameterRequest,
) (*pb.GetParameterResponse, error) {
	s.logger.WithField("path", req.Path).Debug("Processing GetParameter request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &interfaces.GetParameterRequest{
		Path:           req.Path,
		WithDecryption: req.WithDecryption,
	}

	resp, err := s.parameterSvc.GetParameter(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GetParameterResponse{
		Parameter: convertParameterToProto(resp.Parameter),
		Timestamp: resp.Timestamp.UnixNano(),
	}, nil
}

// SetParameter creates or updates a parameter.
func (s *ParameterStoreServer) SetParameter(
	ctx context.Context,
	req *pb.SetParameterRequest,
) (*pb.SetParameterResponse, error) {
	s.logger.WithField("path", req.Path).Debug("Processing SetParameter request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	if req.Value == nil {
		return nil, status.Error(codes.InvalidArgument, "value is required")
	}

	svcReq := &interfaces.SetParameterRequest{
		Path:         req.Path,
		Value:        req.Value,
		Type:         protoParameterTypeToService(req.Type),
		Description:  req.Description,
		KMSKeyID:     req.KmsKeyId,
		Tags:         req.Tags,
		TTLSeconds:   req.TtlSeconds,
		AuditEnabled: req.AuditEnabled,
		Overwrite:    req.Overwrite,
	}

	resp, err := s.parameterSvc.SetParameter(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.SetParameterResponse{
		Path:      resp.Path,
		Created:   resp.Created,
		Timestamp: resp.Timestamp.UnixNano(),
	}, nil
}

// DeleteParameter removes a parameter by path.
func (s *ParameterStoreServer) DeleteParameter(
	ctx context.Context,
	req *pb.DeleteParameterRequest,
) (*pb.DeleteParameterResponse, error) {
	s.logger.WithField("path", req.Path).Info("Processing DeleteParameter request")

	if req.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "path is required")
	}

	svcReq := &interfaces.DeleteParameterRequest{
		Path: req.Path,
	}

	resp, err := s.parameterSvc.DeleteParameter(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.DeleteParameterResponse{
		Path:      resp.Path,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp.UnixNano(),
	}, nil
}

// GetParameters retrieves multiple parameters by paths (batch get).
func (s *ParameterStoreServer) GetParameters(
	ctx context.Context,
	req *pb.GetParametersRequest,
) (*pb.GetParametersResponse, error) {
	s.logger.WithField("path_count", len(req.Paths)).Debug("Processing GetParameters request")

	if len(req.Paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "paths is required")
	}

	svcReq := &interfaces.GetParametersRequest{
		Paths:          req.Paths,
		WithDecryption: req.WithDecryption,
	}

	resp, err := s.parameterSvc.GetParameters(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	parameters := make([]*pb.Parameter, len(resp.Parameters))
	for i, param := range resp.Parameters {
		parameters[i] = convertParameterToProto(param)
	}

	return &pb.GetParametersResponse{
		Parameters: parameters,
		Errors:     resp.Errors,
		Timestamp:  resp.Timestamp.UnixNano(),
	}, nil
}

// SetParameters creates or updates multiple parameters atomically.
func (s *ParameterStoreServer) SetParameters(
	ctx context.Context,
	req *pb.SetParametersRequest,
) (*pb.SetParametersResponse, error) {
	s.logger.WithField("parameter_count", len(req.Parameters)).Debug("Processing SetParameters request")

	if len(req.Parameters) == 0 {
		return nil, status.Error(codes.InvalidArgument, "parameters is required")
	}

	// Convert proto items to service requests
	svcParams := make([]*interfaces.SetParameterRequest, len(req.Parameters))
	for i, param := range req.Parameters {
		if param.Path == "" {
			return nil, status.Errorf(codes.InvalidArgument, "parameter %d: path is required", i)
		}
		if param.Value == nil {
			return nil, status.Errorf(codes.InvalidArgument, "parameter %d: value is required", i)
		}
		svcParams[i] = &interfaces.SetParameterRequest{
			Path:         param.Path,
			Value:        param.Value,
			Type:         protoParameterTypeToService(param.Type),
			Description:  param.Description,
			KMSKeyID:     param.KmsKeyId,
			Tags:         param.Tags,
			TTLSeconds:   param.TtlSeconds,
			AuditEnabled: param.AuditEnabled,
			Overwrite:    param.Overwrite,
		}
	}

	svcReq := &interfaces.SetParametersRequest{
		Parameters: svcParams,
	}

	resp, err := s.parameterSvc.SetParameters(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	results := make([]*pb.SetParameterResult, len(resp.Results))
	for i, result := range resp.Results {
		results[i] = &pb.SetParameterResult{
			Path:    result.Path,
			Created: result.Created,
			Success: result.Success,
			Error:   result.Error,
		}
	}

	return &pb.SetParametersResponse{
		Results:   results,
		Timestamp: resp.Timestamp.UnixNano(),
	}, nil
}

// DeleteParameters removes multiple parameters by paths (batch delete).
func (s *ParameterStoreServer) DeleteParameters(
	ctx context.Context,
	req *pb.DeleteParametersRequest,
) (*pb.DeleteParametersResponse, error) {
	s.logger.WithField("path_count", len(req.Paths)).Info("Processing DeleteParameters request")

	if len(req.Paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "paths is required")
	}

	svcReq := &interfaces.DeleteParametersRequest{
		Paths: req.Paths,
	}

	resp, err := s.parameterSvc.DeleteParameters(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	results := make([]*pb.DeleteParameterResult, len(resp.Results))
	for i, result := range resp.Results {
		results[i] = &pb.DeleteParameterResult{
			Path:    result.Path,
			Deleted: result.Deleted,
			Error:   result.Error,
		}
	}

	return &pb.DeleteParametersResponse{
		Results:      results,
		DeletedCount: int32(resp.DeletedCount), //nolint:gosec // G115: DeletedCount represents count, safe conversion
		Timestamp:    resp.Timestamp.UnixNano(),
	}, nil
}

// ListParameters returns parameters matching a path prefix.
func (s *ParameterStoreServer) ListParameters(
	ctx context.Context,
	req *pb.ListParametersRequest,
) (*pb.ListParametersResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"prefix":    req.Prefix,
		"recursive": req.Recursive,
		"limit":     req.Limit,
	}).Debug("Processing ListParameters request")

	svcReq := &interfaces.ListParametersRequest{
		Prefix:         req.Prefix,
		Recursive:      req.Recursive,
		WithDecryption: req.WithDecryption,
		WithValues:     req.WithValues,
		Limit:          int(req.Limit),
		Cursor:         req.Cursor,
	}

	resp, err := s.parameterSvc.ListParameters(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	parameters := make([]*pb.Parameter, len(resp.Parameters))
	for i, param := range resp.Parameters {
		parameters[i] = convertParameterToProto(param)
	}

	return &pb.ListParametersResponse{
		Parameters: parameters,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		TotalCount: resp.TotalCount,
		Timestamp:  resp.Timestamp.UnixNano(),
	}, nil
}

// SearchByTags finds parameters matching tag key-value pairs.
func (s *ParameterStoreServer) SearchByTags(
	ctx context.Context,
	req *pb.SearchByTagsRequest,
) (*pb.SearchByTagsResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tag_count": len(req.Tags),
		"prefix":    req.Prefix,
		"limit":     req.Limit,
	}).Debug("Processing SearchByTags request")

	if len(req.Tags) == 0 {
		return nil, status.Error(codes.InvalidArgument, "tags is required")
	}

	svcReq := &interfaces.SearchByTagsRequest{
		Tags:           req.Tags,
		Prefix:         req.Prefix,
		WithValues:     req.WithValues,
		WithDecryption: req.WithDecryption,
		Limit:          int(req.Limit),
		Cursor:         req.Cursor,
	}

	resp, err := s.parameterSvc.SearchByTags(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	parameters := make([]*pb.Parameter, len(resp.Parameters))
	for i, param := range resp.Parameters {
		parameters[i] = convertParameterToProto(param)
	}

	return &pb.SearchByTagsResponse{
		Parameters: parameters,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		Timestamp:  resp.Timestamp.UnixNano(),
	}, nil
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// convertParameterToProto converts a service ParameterValue to proto Parameter.
func convertParameterToProto(param *interfaces.ParameterValue) *pb.Parameter {
	if param == nil {
		return nil
	}

	var expiresAt int64
	if param.ExpiresAt != nil {
		expiresAt = param.ExpiresAt.UnixNano()
	}

	return &pb.Parameter{
		Path:         param.Path,
		Value:        param.Value,
		Type:         serviceParameterTypeToProto(param.Type),
		Description:  param.Description,
		Tags:         param.Tags,
		Encrypted:    param.IsEncrypted,
		KmsKeyId:     param.KMSKeyID,
		ExpiresAt:    expiresAt,
		AuditEnabled: param.AuditEnabled,
		CreatedAt:    param.CreatedAt.UnixNano(),
		UpdatedAt:    param.UpdatedAt.UnixNano(),
		CreatedBy:    param.CreatedBy,
		UpdatedBy:    param.UpdatedBy,
	}
}

// protoParameterTypeToService converts proto ParameterType to service string type.
func protoParameterTypeToService(t pb.ParameterType) string {
	switch t {
	case pb.ParameterType_PARAMETER_TYPE_STRING:
		return "string"
	case pb.ParameterType_PARAMETER_TYPE_NUMBER:
		return "number"
	case pb.ParameterType_PARAMETER_TYPE_JSON:
		return "json"
	case pb.ParameterType_PARAMETER_TYPE_BINARY:
		return "binary"
	default:
		return "string"
	}
}

// serviceParameterTypeToProto converts service string type to proto ParameterType.
func serviceParameterTypeToProto(t string) pb.ParameterType {
	switch t {
	case "string":
		return pb.ParameterType_PARAMETER_TYPE_STRING
	case "number":
		return pb.ParameterType_PARAMETER_TYPE_NUMBER
	case "json":
		return pb.ParameterType_PARAMETER_TYPE_JSON
	case "binary":
		return pb.ParameterType_PARAMETER_TYPE_BINARY
	default:
		return pb.ParameterType_PARAMETER_TYPE_STRING
	}
}

// timeToUnixNano converts a time.Time to Unix nanoseconds, handling zero time.
func timeToUnixNano(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}
