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
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SecretsServer wraps the Secrets service for gRPC endpoints.
type SecretsServer struct {
	pb.UnimplementedSecretsManagementServer
	secretsSvc service.SecretsService
	logger     *logger.Logger
}

// NewSecretsServer creates a new gRPC server for Secrets operations.
func NewSecretsServer(secretsSvc service.SecretsService, log *logger.Logger) *SecretsServer {
	return &SecretsServer{
		secretsSvc: secretsSvc,
		logger:     log.WithField("component", "secrets-grpc-server"),
	}
}

// StoreSecret stores a secret with optional metadata and encryption.
func (s *SecretsServer) StoreSecret(ctx context.Context, req *pb.StoreSecretRequest) (*pb.StoreSecretResponse, error) {
	s.logger.Debug("Processing StoreSecret request")

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	if req.Data == nil {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}

	var expiresAt *time.Time
	if req.ExpiresAt > 0 {
		t := time.Unix(req.ExpiresAt, 0)
		expiresAt = &t
	}

	svcReq := &service.StoreSecretRequest{
		SecretID:    req.SecretId,
		Name:        req.Name,
		Description: req.Description,
		Data:        req.Data,
		Metadata:    req.Metadata,
		Tags:        req.Tags,
		ExpiresAt:   expiresAt,
	}

	resp, err := s.secretsSvc.StoreSecret(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.StoreSecretResponse{
		SecretId:  resp.SecretID,
		Version:   resp.Version,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetSecret retrieves the latest version of a secret.
func (s *SecretsServer) GetSecret(ctx context.Context, req *pb.GetSecretRequest) (*pb.GetSecretResponse, error) {
	s.logger.WithField("secret_id", req.SecretId).Debug("Processing GetSecret request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	svcReq := &service.GetSecretRequest{
		SecretID: req.SecretId,
		Version:  req.Version,
	}

	resp, err := s.secretsSvc.GetSecret(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GetSecretResponse{
		SecretId:  resp.SecretID,
		Version:   resp.Version,
		Data:      resp.Data,
		Metadata:  resp.Metadata,
		CreatedAt: resp.CreatedAt.Unix(),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// ListSecrets returns all secrets accessible to the authenticated user.
func (s *SecretsServer) ListSecrets(ctx context.Context, req *pb.ListSecretsRequest) (*pb.ListSecretsResponse, error) {
	s.logger.Debug("Processing ListSecrets request")

	svcReq := &service.ListSecretsRequest{
		Limit:  int(req.Limit),
		Cursor: req.Cursor,
		Filter: req.Filter,
	}

	resp, err := s.secretsSvc.ListSecrets(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	secrets := make([]*pb.Secret, len(resp.Secrets))
	for i, sec := range resp.Secrets {
		secrets[i] = convertSecret(sec)
	}

	return &pb.ListSecretsResponse{
		Secrets:    secrets,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		TotalCount: int32(resp.TotalCount), //nolint:gosec // G115: TotalCount represents count, safe conversion
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// DeleteSecret permanently removes a secret and all its versions.
func (s *SecretsServer) DeleteSecret(ctx context.Context, req *pb.DeleteSecretRequest) (*pb.DeleteSecretResponse, error) {
	s.logger.WithField("secret_id", req.SecretId).Info("Processing DeleteSecret request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	svcReq := &service.DeleteSecretRequest{
		SecretID:          req.SecretId,
		PermanentlyDelete: req.PermanentlyDelete,
	}

	resp, err := s.secretsSvc.DeleteSecret(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.DeleteSecretResponse{
		SecretId:  resp.SecretID,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetSecretVersion retrieves a specific version of a secret.
func (s *SecretsServer) GetSecretVersion(ctx context.Context, req *pb.GetSecretVersionRequest) (*pb.GetSecretVersionResponse, error) {
	s.logger.WithFields(map[string]interface{}{"secret_id": req.SecretId, "version": req.Version}).Debug("Processing GetSecretVersion request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	if req.Version == 0 {
		return nil, status.Error(codes.InvalidArgument, "version is required")
	}

	svcReq := &service.GetSecretVersionRequest{
		SecretID: req.SecretId,
		Version:  req.Version,
	}

	resp, err := s.secretsSvc.GetSecretVersion(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GetSecretVersionResponse{
		Version:   convertSecretVersion(resp.Version),
		Data:      resp.Version.Data,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// ListSecretVersions returns all versions of a secret.
func (s *SecretsServer) ListSecretVersions(ctx context.Context, req *pb.ListSecretVersionsRequest) (*pb.ListSecretVersionsResponse, error) {
	s.logger.WithField("secret_id", req.SecretId).Debug("Processing ListSecretVersions request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	svcReq := &service.ListSecretVersionsRequest{
		SecretID: req.SecretId,
		Limit:    int(req.Limit),
		Cursor:   req.Cursor,
	}

	resp, err := s.secretsSvc.ListSecretVersions(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	versions := make([]*pb.SecretVersion, len(resp.Versions))
	for i, ver := range resp.Versions {
		versions[i] = convertSecretVersion(ver)
	}

	return &pb.ListSecretVersionsResponse{
		Versions:   versions,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// RollbackSecret rolls back to a previous version of a secret.
func (s *SecretsServer) RollbackSecret(ctx context.Context, req *pb.RollbackSecretRequest) (*pb.RollbackSecretResponse, error) {
	s.logger.WithFields(map[string]interface{}{"secret_id": req.SecretId, "target_version": req.TargetVersion}).Info("Processing RollbackSecret request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	if req.TargetVersion == 0 {
		return nil, status.Error(codes.InvalidArgument, "target_version is required")
	}

	svcReq := &service.RollbackSecretRequest{
		SecretID:      req.SecretId,
		TargetVersion: req.TargetVersion,
	}

	resp, err := s.secretsSvc.RollbackSecret(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.RollbackSecretResponse{
		SecretId:   resp.SecretID,
		NewVersion: resp.NewVersion,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// RotateSecret manually rotates a secret to a new value.
func (s *SecretsServer) RotateSecret(ctx context.Context, req *pb.RotateSecretRequest) (*pb.RotateSecretResponse, error) {
	s.logger.WithField("secret_id", req.SecretId).Info("Processing RotateSecret request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	if req.NewData == nil {
		return nil, status.Error(codes.InvalidArgument, "new_data is required")
	}

	svcReq := &service.RotateSecretRequest{
		SecretID: req.SecretId,
		NewData:  req.NewData,
		Metadata: req.Metadata,
	}

	resp, err := s.secretsSvc.RotateSecret(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.RotateSecretResponse{
		SecretId:   resp.SecretID,
		OldVersion: resp.OldVersion,
		NewVersion: resp.NewVersion,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// SetRotationPolicy sets or updates the automatic rotation policy.
func (s *SecretsServer) SetRotationPolicy(ctx context.Context, req *pb.SetRotationPolicyRequest) (*pb.SetRotationPolicyResponse, error) {
	s.logger.WithField("secret_id", req.SecretId).Debug("Processing SetRotationPolicy request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	if req.Policy == nil {
		return nil, status.Error(codes.InvalidArgument, "policy is required")
	}

	svcReq := &service.SetRotationPolicyRequest{
		SecretID: req.SecretId,
		Policy:   convertProtoSecretRotationPolicy(req.Policy),
	}

	resp, err := s.secretsSvc.SetRotationPolicy(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.SetRotationPolicyResponse{
		SecretId:  resp.SecretID,
		Policy:    convertSecretRotationPolicy(resp.Policy),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetRotationPolicy retrieves the current rotation policy for a secret.
func (s *SecretsServer) GetRotationPolicy(ctx context.Context, req *pb.GetRotationPolicyRequest) (*pb.GetRotationPolicyResponse, error) {
	s.logger.WithField("secret_id", req.SecretId).Debug("Processing GetRotationPolicy request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	svcReq := &service.GetRotationPolicyRequest{
		SecretID: req.SecretId,
	}

	resp, err := s.secretsSvc.GetRotationPolicy(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GetRotationPolicyResponse{
		SecretId:  resp.SecretID,
		Policy:    convertSecretRotationPolicy(resp.Policy),
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// UpdateSecretStatus enables or disables a secret.
func (s *SecretsServer) UpdateSecretStatus(ctx context.Context, req *pb.UpdateSecretStatusRequest) (*pb.UpdateSecretStatusResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"secret_id": req.SecretId,
		"status":    req.Status.String(),
	}).Info("Processing UpdateSecretStatus request")

	if req.SecretId == "" {
		return nil, status.Error(codes.InvalidArgument, "secret_id is required")
	}

	// Convert proto status to service status
	var svcStatus service.SecretStatus
	switch req.Status {
	case pb.SecretStatus_SECRET_STATUS_ENABLED:
		svcStatus = service.SecretStatusEnabled
	case pb.SecretStatus_SECRET_STATUS_DISABLED:
		svcStatus = service.SecretStatusDisabled
	default:
		return nil, status.Error(codes.InvalidArgument, "invalid status: must be ENABLED or DISABLED")
	}

	svcReq := &service.UpdateSecretStatusRequest{
		SecretID: req.SecretId,
		Status:   svcStatus,
	}

	resp, err := s.secretsSvc.UpdateSecretStatus(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	// Convert service status back to proto status
	var pbStatus pb.SecretStatus
	switch resp.NewStatus {
	case service.SecretStatusEnabled:
		pbStatus = pb.SecretStatus_SECRET_STATUS_ENABLED
	case service.SecretStatusDisabled:
		pbStatus = pb.SecretStatus_SECRET_STATUS_DISABLED
	default:
		pbStatus = pb.SecretStatus_SECRET_STATUS_UNSPECIFIED
	}

	return &pb.UpdateSecretStatusResponse{
		SecretId:  resp.SecretID,
		Status:    pbStatus,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GetSecretsByPrefix retrieves all secrets matching a path prefix.
func (s *SecretsServer) GetSecretsByPrefix(ctx context.Context, req *pb.GetSecretsByPrefixRequest) (*pb.GetSecretsByPrefixResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"prefix":       req.Prefix,
		"include_data": req.IncludeData,
		"limit":        req.Limit,
	}).Debug("Processing GetSecretsByPrefix request")

	svcReq := &service.GetSecretsByPrefixRequest{
		Prefix:      req.Prefix,
		IncludeData: req.IncludeData,
		Limit:       int(req.Limit),
		Cursor:      req.Cursor,
	}

	resp, err := s.secretsSvc.GetSecretsByPrefix(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	// Convert secrets map to proto format
	pbSecrets := make(map[string]*pb.SecretData)
	for path, data := range resp.Secrets {
		pbSecrets[path] = &pb.SecretData{
			Data:     data,
			Metadata: resp.Metadata[path],
			Version:  resp.Version,
		}
	}

	return &pb.GetSecretsByPrefixResponse{
		Secrets:    pbSecrets,
		NextCursor: resp.NextCursor,
		HasMore:    resp.HasMore,
		TotalCount: int32(resp.TotalCount), //nolint:gosec // G115: TotalCount is bounded
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// WatchSecretsByPrefix streams secret changes for a prefix.
func (s *SecretsServer) WatchSecretsByPrefix(req *pb.WatchSecretsRequest, stream pb.SecretsManagement_WatchSecretsByPrefixServer) error {
	s.logger.WithFields(map[string]interface{}{
		"prefix":        req.Prefix,
		"start_version": req.StartVersion,
	}).Info("Starting WatchSecretsByPrefix stream")

	ctx := stream.Context()

	svcReq := &service.WatchSecretsRequest{
		Prefix:       req.Prefix,
		StartVersion: req.StartVersion,
	}

	changes, err := s.secretsSvc.WatchSecretsByPrefix(ctx, svcReq)
	if err != nil {
		return ToGRPCError(err)
	}

	for {
		select {
		case <-ctx.Done():
			s.logger.Debug("WatchSecretsByPrefix stream cancelled")
			return ctx.Err()

		case change, ok := <-changes:
			if !ok {
				s.logger.Debug("WatchSecretsByPrefix channel closed")
				return nil
			}

			// Convert service change type to proto
			var changeType pb.SecretChangeType
			switch change.ChangeType {
			case service.SecretChangeCreated:
				changeType = pb.SecretChangeType_SECRET_CHANGE_TYPE_CREATED
			case service.SecretChangeUpdated:
				changeType = pb.SecretChangeType_SECRET_CHANGE_TYPE_UPDATED
			case service.SecretChangeDeleted:
				changeType = pb.SecretChangeType_SECRET_CHANGE_TYPE_DELETED
			default:
				changeType = pb.SecretChangeType_SECRET_CHANGE_TYPE_UNSPECIFIED
			}

			pbChange := &pb.SecretChange{
				Path:       change.Path,
				ChangeType: changeType,
				Data:       change.Data,
				Metadata:   change.Metadata,
				Version:    change.Version,
				Timestamp:  change.Timestamp.Unix(),
			}

			if err := stream.Send(pbChange); err != nil {
				s.logger.WithError(err).Warn("Failed to send change to stream")
				return err
			}
		}
	}
}

// =============================================================================
// Conversion Helpers - Service to Proto Types
// =============================================================================

func convertSecret(sec *service.Secret) *pb.Secret {
	if sec == nil {
		return nil
	}

	return &pb.Secret{
		SecretId:       sec.SecretID,
		Name:           sec.Name,
		Description:    sec.Description,
		CurrentVersion: sec.CurrentVersion,
		CreatedAt:      sec.CreatedAt.Unix(),
		UpdatedAt:      sec.UpdatedAt.Unix(),
		RotationPolicy: convertSecretRotationPolicy(sec.RotationPolicy),
		Tags:           sec.Tags,
		Owner:          sec.Owner,
	}
}

func convertSecretVersion(ver *service.SecretVersion) *pb.SecretVersion {
	if ver == nil {
		return nil
	}

	var deletedAt, expiresAt int64
	if ver.DeletedAt != nil {
		deletedAt = ver.DeletedAt.Unix()
	}
	if ver.ExpiresAt != nil {
		expiresAt = ver.ExpiresAt.Unix()
	}

	return &pb.SecretVersion{
		SecretId:  ver.SecretID,
		Version:   ver.Version,
		Metadata:  ver.Metadata,
		CreatedAt: ver.CreatedAt.Unix(),
		CreatedBy: ver.CreatedBy,
		DeletedAt: deletedAt,
		ExpiresAt: expiresAt,
	}
}

func convertSecretRotationPolicy(policy *service.SecretRotationPolicy) *pb.SecretRotationPolicy {
	if policy == nil {
		return nil
	}

	return &pb.SecretRotationPolicy{
		Enabled:         policy.Enabled,
		IntervalSeconds: int64(policy.Interval.Seconds()),
		NextRotation:    policy.NextRotation.Unix(),
		RetainVersions:  int32(policy.RetainVersions), //nolint:gosec // G115: RetainVersions represents count, safe conversion
		RotationLambda:  policy.RotationLambda,
	}
}

func convertProtoSecretRotationPolicy(policy *pb.SecretRotationPolicy) *service.SecretRotationPolicy {
	if policy == nil {
		return nil
	}

	return &service.SecretRotationPolicy{
		Enabled:        policy.Enabled,
		Interval:       time.Duration(policy.IntervalSeconds) * time.Second,
		NextRotation:   time.Unix(policy.NextRotation, 0),
		RetainVersions: int(policy.RetainVersions),
		RotationLambda: policy.RotationLambda,
	}
}
