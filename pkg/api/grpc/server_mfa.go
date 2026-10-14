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
	stderrors "errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// MFAServer implements the MFA gRPC service.
type MFAServer struct {
	iampb.UnimplementedMFAServiceServer
	mfaService MFAService
	logger     *logger.Logger
}

// MFAService defines the interface for MFA operations.
type MFAService interface {
	// Enrollment operations
	BeginEnrollment(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error)
	CompleteEnrollment(ctx context.Context, userID, tenantID, enrollmentID, code string) error
	CancelEnrollment(ctx context.Context, userID, tenantID, enrollmentID string) error

	// Challenge/verification operations
	BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error)
	VerifyChallenge(ctx context.Context, userID, tenantID, challengeID, code string) (*iam.MFAVerifyResponse, error)
	VerifyCode(ctx context.Context, userID, tenantID, code string) error

	// Status operations
	GetMFAStatus(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error)
	DisableMFA(ctx context.Context, userID, tenantID string) error

	// Backup code operations
	GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error)
	VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error
	GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error)
}

// NewMFAServer creates a new MFA gRPC server.
func NewMFAServer(mfaService MFAService, log *logger.Logger) *MFAServer {
	return &MFAServer{
		mfaService: mfaService,
		logger:     log.WithField("component", "mfa-grpc-server"),
	}
}

// =============================================================================
// Enrollment Operations
// =============================================================================

// BeginEnrollment starts the MFA enrollment process.
func (s *MFAServer) BeginEnrollment(ctx context.Context, req *iampb.BeginMFAEnrollmentRequest) (*iampb.BeginMFAEnrollmentResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
		"method":    req.Method.String(),
	}).Debug("Processing BeginEnrollment request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.MFA", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	method := convertProtoToMFAMethod(req.Method)
	resp, err := s.mfaService.BeginEnrollment(ctx, req.UserId, req.TenantId, method)
	if err != nil {
		return nil, convertMFAError(err)
	}

	return &iampb.BeginMFAEnrollmentResponse{
		EnrollmentId:    resp.EnrollmentID,
		Secret:          resp.Secret,
		ProvisioningUri: resp.QRCodeURI,
		ExpiresAt:       timestamppb.New(resp.ExpiresAt),
	}, nil
}

// CompleteEnrollment completes the MFA enrollment process.
func (s *MFAServer) CompleteEnrollment(ctx context.Context, req *iampb.CompleteMFAEnrollmentRequest) (*iampb.CompleteMFAEnrollmentResponse, error) {
	s.logger.WithField("enrollment_id", req.EnrollmentId).Debug("Processing CompleteEnrollment request")

	if req.EnrollmentId == "" {
		return nil, status.Error(codes.InvalidArgument, "enrollment_id is required")
	}
	if req.Code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	tenantID, err := targetTenant(ctx, "grpc.MFA", "")
	if err != nil {
		return nil, err
	}

	// The request names no user, so the enrollment is bound to the
	// caller's tenant only.
	err = s.mfaService.CompleteEnrollment(ctx, "", tenantID, req.EnrollmentId, req.Code)
	if err != nil {
		return nil, convertMFAError(err)
	}

	return &iampb.CompleteMFAEnrollmentResponse{
		Success: true,
	}, nil
}

// CancelEnrollment cancels a pending MFA enrollment.
func (s *MFAServer) CancelEnrollment(ctx context.Context, req *iampb.CancelMFAEnrollmentRequest) (*iampb.CancelMFAEnrollmentResponse, error) {
	s.logger.WithField("enrollment_id", req.EnrollmentId).Debug("Processing CancelEnrollment request")

	if req.EnrollmentId == "" {
		return nil, status.Error(codes.InvalidArgument, "enrollment_id is required")
	}

	tenantID, err := targetTenant(ctx, "grpc.MFA", "")
	if err != nil {
		return nil, err
	}

	// The request names no user, so the enrollment is bound to the
	// caller's tenant only.
	err = s.mfaService.CancelEnrollment(ctx, "", tenantID, req.EnrollmentId)
	if err != nil {
		return nil, convertMFAError(err)
	}

	return &iampb.CancelMFAEnrollmentResponse{
		Success: true,
	}, nil
}

// =============================================================================
// Challenge/Verification Operations
// =============================================================================

// BeginChallenge starts an MFA challenge for verification.
func (s *MFAServer) BeginChallenge(ctx context.Context, req *iampb.BeginMFAChallengeRequest) (*iampb.BeginMFAChallengeResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing BeginChallenge request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.MFA", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	challenge, err := s.mfaService.BeginChallenge(ctx, req.UserId, req.TenantId, req.SessionToken)
	if err != nil {
		return nil, convertMFAError(err)
	}

	return &iampb.BeginMFAChallengeResponse{
		ChallengeId: challenge.ID,
		Method:      convertMFAMethodToProto(challenge.Method),
		ExpiresAt:   timestamppb.New(challenge.ExpiresAt),
	}, nil
}

// VerifyChallenge verifies an MFA challenge response.
func (s *MFAServer) VerifyChallenge(ctx context.Context, req *iampb.VerifyMFAChallengeRequest) (*iampb.VerifyMFAChallengeResponse, error) {
	s.logger.WithField("challenge_id", req.ChallengeId).Debug("Processing VerifyChallenge request")

	if req.ChallengeId == "" {
		return nil, status.Error(codes.InvalidArgument, "challenge_id is required")
	}
	if req.Code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	tenantID, err := targetTenant(ctx, "grpc.MFA", "")
	if err != nil {
		return nil, err
	}

	// The request names no user, so the challenge is bound to the
	// caller's tenant only.
	resp, err := s.mfaService.VerifyChallenge(ctx, "", tenantID, req.ChallengeId, req.Code)
	if err != nil {
		return nil, convertMFAError(err)
	}

	return &iampb.VerifyMFAChallengeResponse{
		Success:      resp.Success,
		SessionToken: resp.Token,
	}, nil
}

// VerifyCode performs stateless MFA code verification.
func (s *MFAServer) VerifyCode(ctx context.Context, req *iampb.VerifyMFACodeRequest) (*iampb.VerifyMFACodeResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing VerifyCode request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.MFA", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	err := s.mfaService.VerifyCode(ctx, req.UserId, req.TenantId, req.Code)
	if err != nil {
		return nil, convertMFAError(err)
	}

	return &iampb.VerifyMFACodeResponse{
		Success: true,
	}, nil
}

// =============================================================================
// Status Operations
// =============================================================================

// GetStatus returns the MFA status for a user.
func (s *MFAServer) GetStatus(ctx context.Context, req *iampb.GetMFAStatusRequest) (*iampb.GetMFAStatusResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing GetStatus request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.MFA", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	mfaStatus, err := s.mfaService.GetMFAStatus(ctx, req.UserId, req.TenantId)
	if err != nil {
		return nil, convertMFAError(err)
	}

	backupCodesRemaining, err := responseInt32("backup_codes_remaining", mfaStatus.BackupCodesRemaining)
	if err != nil {
		return nil, err
	}

	resp := &iampb.GetMFAStatusResponse{
		Enabled:              mfaStatus.Enabled,
		Method:               convertMFAMethodToProto(mfaStatus.Method),
		BackupCodesRemaining: backupCodesRemaining,
	}

	if mfaStatus.EnrolledAt != nil && !mfaStatus.EnrolledAt.IsZero() {
		resp.EnrolledAt = timestamppb.New(*mfaStatus.EnrolledAt)
	}
	if mfaStatus.LastVerified != nil && !mfaStatus.LastVerified.IsZero() {
		resp.LastVerified = timestamppb.New(*mfaStatus.LastVerified)
	}

	return resp, nil
}

// Disable disables MFA for a user.
func (s *MFAServer) Disable(ctx context.Context, req *iampb.DisableMFARequest) (*iampb.DisableMFAResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Info("Processing Disable request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.MFA", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	err := s.mfaService.DisableMFA(ctx, req.UserId, req.TenantId)
	if err != nil {
		return nil, convertMFAError(err)
	}

	return &iampb.DisableMFAResponse{
		Success: true,
	}, nil
}

// =============================================================================
// Backup Code Operations
// =============================================================================

// GenerateBackupCodes generates new backup codes for a user.
func (s *MFAServer) GenerateBackupCodes(ctx context.Context, req *iampb.GenerateBackupCodesRequest) (*iampb.GenerateBackupCodesResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Info("Processing GenerateBackupCodes request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.MFA", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	codes, err := s.mfaService.GenerateBackupCodes(ctx, req.UserId, req.TenantId)
	if err != nil {
		return nil, convertMFAError(err)
	}

	return &iampb.GenerateBackupCodesResponse{
		Codes: codes,
	}, nil
}

// VerifyBackupCode verifies a backup code.
func (s *MFAServer) VerifyBackupCode(ctx context.Context, req *iampb.VerifyBackupCodeRequest) (*iampb.VerifyBackupCodeResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing VerifyBackupCode request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.MFA", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Code == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	err := s.mfaService.VerifyBackupCode(ctx, req.UserId, req.TenantId, req.Code)
	if err != nil {
		return nil, convertMFAError(err)
	}

	// Get remaining codes after verification
	backupStatus, err := s.mfaService.GetBackupCodeStatus(ctx, req.UserId, req.TenantId)
	remaining := 0
	if err == nil {
		remaining = backupStatus.Remaining
	}

	remainingCodes, err := responseInt32("remaining_codes", remaining)
	if err != nil {
		return nil, err
	}

	return &iampb.VerifyBackupCodeResponse{
		Success:        true,
		RemainingCodes: remainingCodes,
	}, nil
}

// GetBackupCodeStatus returns the backup code status for a user.
func (s *MFAServer) GetBackupCodeStatus(ctx context.Context, req *iampb.GetBackupCodeStatusRequest) (*iampb.GetBackupCodeStatusResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing GetBackupCodeStatus request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if err := requireCallerTenant(ctx, "grpc.MFA", req.TenantId); err != nil {
		return nil, err
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	backupStatus, err := s.mfaService.GetBackupCodeStatus(ctx, req.UserId, req.TenantId)
	if err != nil {
		return nil, convertMFAError(err)
	}

	var narrow responseNarrower
	resp := &iampb.GetBackupCodeStatusResponse{
		TotalCodes:     narrow.int32("total_codes", backupStatus.Total),
		RemainingCodes: narrow.int32("remaining_codes", backupStatus.Remaining),
	}
	if err := narrow.err(); err != nil {
		return nil, err
	}

	if !backupStatus.GeneratedAt.IsZero() {
		resp.GeneratedAt = timestamppb.New(backupStatus.GeneratedAt)
	}

	return resp, nil
}

// =============================================================================
// Helper Functions
// =============================================================================

// convertProtoToMFAMethod converts a proto MFA method to domain type.
func convertProtoToMFAMethod(method iampb.MFAMethod) iam.MFAMethod {
	switch method {
	case iampb.MFAMethod_MFA_METHOD_TOTP:
		return iam.MFAMethodTOTP
	case iampb.MFAMethod_MFA_METHOD_WEBAUTHN:
		return iam.MFAMethodWebAuthn
	default:
		return iam.MFAMethodTOTP
	}
}

// convertMFAMethodToProto converts a domain MFA method to proto type.
func convertMFAMethodToProto(method iam.MFAMethod) iampb.MFAMethod {
	switch method {
	case iam.MFAMethodTOTP:
		return iampb.MFAMethod_MFA_METHOD_TOTP
	case iam.MFAMethodWebAuthn:
		return iampb.MFAMethod_MFA_METHOD_WEBAUTHN
	default:
		return iampb.MFAMethod_MFA_METHOD_UNSPECIFIED
	}
}

// convertMFAError converts MFA domain errors to gRPC status errors.
func convertMFAError(err error) error {
	if err == nil {
		return nil
	}

	var dErr *errors.QBError
	if !stderrors.As(err, &dErr) {
		return status.Error(codes.Internal, err.Error())
	}

	// These services report conflict as a failed precondition, not AlreadyExists.
	if dErr.Code == errors.ErrConflict {
		return status.Error(codes.FailedPrecondition, dErr.Error())
	}
	return status.Error(errorCodeToGRPCCode(dErr.Code), dErr.Error())
}
