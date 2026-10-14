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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// mockMFAService implements MFAService for testing
type mockMFAService struct {
	beginEnrollmentFn     func(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error)
	completeEnrollmentFn  func(ctx context.Context, enrollmentID, code string) error
	cancelEnrollmentFn    func(ctx context.Context, enrollmentID string) error
	beginChallengeFn      func(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error)
	verifyChallengeFn     func(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error)
	verifyCodeFn          func(ctx context.Context, userID, tenantID, code string) error
	getMFAStatusFn        func(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error)
	disableMFAFn          func(ctx context.Context, userID, tenantID string) error
	generateBackupCodesFn func(ctx context.Context, userID, tenantID string) ([]string, error)
	verifyBackupCodeFn    func(ctx context.Context, userID, tenantID, code string) error
	getBackupCodeStatusFn func(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error)
}

func (m *mockMFAService) BeginEnrollment(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	if m.beginEnrollmentFn != nil {
		return m.beginEnrollmentFn(ctx, userID, tenantID, method)
	}
	return &iam.MFAEnrollResponse{
		EnrollmentID: "enroll-123",
		Secret:       "secret",
		QRCodeURI:    "otpauth://totp/test",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}, nil
}

func (m *mockMFAService) CompleteEnrollment(ctx context.Context, _, _, enrollmentID, code string) error {
	if m.completeEnrollmentFn != nil {
		return m.completeEnrollmentFn(ctx, enrollmentID, code)
	}
	return nil
}

func (m *mockMFAService) CancelEnrollment(ctx context.Context, _, _, enrollmentID string) error {
	if m.cancelEnrollmentFn != nil {
		return m.cancelEnrollmentFn(ctx, enrollmentID)
	}
	return nil
}

func (m *mockMFAService) BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error) {
	if m.beginChallengeFn != nil {
		return m.beginChallengeFn(ctx, userID, tenantID, sessionToken)
	}
	return &iam.MFAChallenge{
		ID:        "challenge-123",
		Method:    iam.MFAMethodTOTP,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}, nil
}

func (m *mockMFAService) VerifyChallenge(ctx context.Context, _, _, challengeID, code string) (*iam.MFAVerifyResponse, error) {
	if m.verifyChallengeFn != nil {
		return m.verifyChallengeFn(ctx, challengeID, code)
	}
	return &iam.MFAVerifyResponse{
		Success: true,
		Token:   "session-token",
	}, nil
}

func (m *mockMFAService) VerifyCode(ctx context.Context, userID, tenantID, code string) error {
	if m.verifyCodeFn != nil {
		return m.verifyCodeFn(ctx, userID, tenantID, code)
	}
	return nil
}

func (m *mockMFAService) GetMFAStatus(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error) {
	if m.getMFAStatusFn != nil {
		return m.getMFAStatusFn(ctx, userID, tenantID)
	}
	enrolledAt := time.Now().Add(-24 * time.Hour)
	lastVerified := time.Now().Add(-1 * time.Hour)
	return &iam.MFAStatus{
		Enabled:              true,
		Method:               iam.MFAMethodTOTP,
		EnrolledAt:           &enrolledAt,
		LastVerified:         &lastVerified,
		BackupCodesRemaining: 5,
	}, nil
}

func (m *mockMFAService) DisableMFA(ctx context.Context, userID, tenantID string) error {
	if m.disableMFAFn != nil {
		return m.disableMFAFn(ctx, userID, tenantID)
	}
	return nil
}

func (m *mockMFAService) GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error) {
	if m.generateBackupCodesFn != nil {
		return m.generateBackupCodesFn(ctx, userID, tenantID)
	}
	return []string{"code1", "code2", "code3"}, nil
}

func (m *mockMFAService) VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error {
	if m.verifyBackupCodeFn != nil {
		return m.verifyBackupCodeFn(ctx, userID, tenantID, code)
	}
	return nil
}

func (m *mockMFAService) GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error) {
	if m.getBackupCodeStatusFn != nil {
		return m.getBackupCodeStatusFn(ctx, userID, tenantID)
	}
	return &iam.BackupCodeStatus{
		Total:       10,
		Remaining:   5,
		GeneratedAt: time.Now().Add(-24 * time.Hour),
	}, nil
}

func newTestMFAServer(svc MFAService) *MFAServer {
	log, _ := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	return NewMFAServer(svc, log)
}

func TestNewMFAServer(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	svc := &mockMFAService{}
	server := NewMFAServer(svc, log)

	assert.NotNil(t, server)
	assert.NotNil(t, server.logger)
	assert.Equal(t, svc, server.mfaService)
}

func TestMFAServer_BeginEnrollment(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.BeginEnrollment(ctx, &iampb.BeginMFAEnrollmentRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
			Method:   iampb.MFAMethod_MFA_METHOD_TOTP,
		})

		require.NoError(t, err)
		assert.Equal(t, "enroll-123", resp.EnrollmentId)
		assert.Equal(t, "secret", resp.Secret)
		assert.NotNil(t, resp.ExpiresAt)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.BeginEnrollment(ctx, &iampb.BeginMFAEnrollmentRequest{
			UserId: "user-1",
			Method: iampb.MFAMethod_MFA_METHOD_TOTP,
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing user_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.BeginEnrollment(ctx, &iampb.BeginMFAEnrollmentRequest{
			TenantId: "tenant-1",
			Method:   iampb.MFAMethod_MFA_METHOD_TOTP,
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("service error", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{
			beginEnrollmentFn: func(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
				return nil, &errors.QBError{Code: errors.ErrInternal, Err: stderrors.New("internal failure")}
			},
		})

		_, err := server.BeginEnrollment(ctx, &iampb.BeginMFAEnrollmentRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
			Method:   iampb.MFAMethod_MFA_METHOD_TOTP,
		})

		require.Error(t, err)
	})
}

func TestMFAServer_CompleteEnrollment(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.CompleteEnrollment(ctx, &iampb.CompleteMFAEnrollmentRequest{
			EnrollmentId: "enroll-123",
			Code:         "123456",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("missing enrollment_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.CompleteEnrollment(ctx, &iampb.CompleteMFAEnrollmentRequest{
			Code: "123456",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing code", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.CompleteEnrollment(ctx, &iampb.CompleteMFAEnrollmentRequest{
			EnrollmentId: "enroll-123",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_CancelEnrollment(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.CancelEnrollment(ctx, &iampb.CancelMFAEnrollmentRequest{
			EnrollmentId: "enroll-123",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("missing enrollment_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.CancelEnrollment(ctx, &iampb.CancelMFAEnrollmentRequest{})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_BeginChallenge(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.BeginChallenge(ctx, &iampb.BeginMFAChallengeRequest{
			TenantId:     "tenant-1",
			UserId:       "user-1",
			SessionToken: "session-token",
		})

		require.NoError(t, err)
		assert.Equal(t, "challenge-123", resp.ChallengeId)
		assert.Equal(t, iampb.MFAMethod_MFA_METHOD_TOTP, resp.Method)
		assert.NotNil(t, resp.ExpiresAt)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.BeginChallenge(ctx, &iampb.BeginMFAChallengeRequest{
			UserId: "user-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing user_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.BeginChallenge(ctx, &iampb.BeginMFAChallengeRequest{
			TenantId: "tenant-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_VerifyChallenge(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.VerifyChallenge(ctx, &iampb.VerifyMFAChallengeRequest{
			ChallengeId: "challenge-123",
			Code:        "123456",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, "session-token", resp.SessionToken)
	})

	t.Run("missing challenge_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.VerifyChallenge(ctx, &iampb.VerifyMFAChallengeRequest{
			Code: "123456",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing code", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.VerifyChallenge(ctx, &iampb.VerifyMFAChallengeRequest{
			ChallengeId: "challenge-123",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_VerifyCode(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.VerifyCode(ctx, &iampb.VerifyMFACodeRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
			Code:     "123456",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.VerifyCode(ctx, &iampb.VerifyMFACodeRequest{
			UserId: "user-1",
			Code:   "123456",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing user_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.VerifyCode(ctx, &iampb.VerifyMFACodeRequest{
			TenantId: "tenant-1",
			Code:     "123456",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing code", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.VerifyCode(ctx, &iampb.VerifyMFACodeRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_GetStatus(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.GetStatus(ctx, &iampb.GetMFAStatusRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
		})

		require.NoError(t, err)
		assert.True(t, resp.Enabled)
		assert.Equal(t, iampb.MFAMethod_MFA_METHOD_TOTP, resp.Method)
		assert.NotNil(t, resp.EnrolledAt)
		assert.NotNil(t, resp.LastVerified)
		assert.Equal(t, int32(5), resp.BackupCodesRemaining)
	})

	t.Run("success with nil timestamps", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{
			getMFAStatusFn: func(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error) {
				return &iam.MFAStatus{
					Enabled:      false,
					EnrolledAt:   nil,
					LastVerified: nil,
				}, nil
			},
		})

		resp, err := server.GetStatus(ctx, &iampb.GetMFAStatusRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
		})

		require.NoError(t, err)
		assert.False(t, resp.Enabled)
		assert.Nil(t, resp.EnrolledAt)
		assert.Nil(t, resp.LastVerified)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.GetStatus(ctx, &iampb.GetMFAStatusRequest{
			UserId: "user-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing user_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.GetStatus(ctx, &iampb.GetMFAStatusRequest{
			TenantId: "tenant-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_Disable(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.Disable(ctx, &iampb.DisableMFARequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.Disable(ctx, &iampb.DisableMFARequest{
			UserId: "user-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing user_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.Disable(ctx, &iampb.DisableMFARequest{
			TenantId: "tenant-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_GenerateBackupCodes(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.GenerateBackupCodes(ctx, &iampb.GenerateBackupCodesRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
		})

		require.NoError(t, err)
		assert.Len(t, resp.Codes, 3)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.GenerateBackupCodes(ctx, &iampb.GenerateBackupCodesRequest{
			UserId: "user-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing user_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.GenerateBackupCodes(ctx, &iampb.GenerateBackupCodesRequest{
			TenantId: "tenant-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_VerifyBackupCode(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.VerifyBackupCode(ctx, &iampb.VerifyBackupCodeRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
			Code:     "backup-code",
		})

		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, int32(5), resp.RemainingCodes)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.VerifyBackupCode(ctx, &iampb.VerifyBackupCodeRequest{
			UserId: "user-1",
			Code:   "code",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing user_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.VerifyBackupCode(ctx, &iampb.VerifyBackupCodeRequest{
			TenantId: "tenant-1",
			Code:     "code",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing code", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.VerifyBackupCode(ctx, &iampb.VerifyBackupCodeRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestMFAServer_GetBackupCodeStatus(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-1")

	t.Run("success", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		resp, err := server.GetBackupCodeStatus(ctx, &iampb.GetBackupCodeStatusRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
		})

		require.NoError(t, err)
		assert.Equal(t, int32(10), resp.TotalCodes)
		assert.Equal(t, int32(5), resp.RemainingCodes)
		assert.NotNil(t, resp.GeneratedAt)
	})

	t.Run("success with zero generated at", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{
			getBackupCodeStatusFn: func(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error) {
				return &iam.BackupCodeStatus{
					Total:       10,
					Remaining:   10,
					GeneratedAt: time.Time{}, // zero value
				}, nil
			},
		})

		resp, err := server.GetBackupCodeStatus(ctx, &iampb.GetBackupCodeStatusRequest{
			TenantId: "tenant-1",
			UserId:   "user-1",
		})

		require.NoError(t, err)
		assert.Nil(t, resp.GeneratedAt)
	})

	t.Run("missing tenant_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.GetBackupCodeStatus(ctx, &iampb.GetBackupCodeStatusRequest{
			UserId: "user-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing user_id", func(t *testing.T) {
		server := newTestMFAServer(&mockMFAService{})

		_, err := server.GetBackupCodeStatus(ctx, &iampb.GetBackupCodeStatusRequest{
			TenantId: "tenant-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

func TestConvertProtoToMFAMethod(t *testing.T) {
	tests := []struct {
		name     string
		proto    iampb.MFAMethod
		expected iam.MFAMethod
	}{
		{
			name:     "TOTP method",
			proto:    iampb.MFAMethod_MFA_METHOD_TOTP,
			expected: iam.MFAMethodTOTP,
		},
		{
			name:     "WebAuthn method",
			proto:    iampb.MFAMethod_MFA_METHOD_WEBAUTHN,
			expected: iam.MFAMethodWebAuthn,
		},
		{
			name:     "unspecified defaults to TOTP",
			proto:    iampb.MFAMethod_MFA_METHOD_UNSPECIFIED,
			expected: iam.MFAMethodTOTP,
		},
		{
			name:     "unknown value defaults to TOTP",
			proto:    iampb.MFAMethod(999),
			expected: iam.MFAMethodTOTP,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToMFAMethod(tt.proto)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertMFAMethodToProto(t *testing.T) {
	tests := []struct {
		name     string
		method   iam.MFAMethod
		expected iampb.MFAMethod
	}{
		{
			name:     "TOTP method",
			method:   iam.MFAMethodTOTP,
			expected: iampb.MFAMethod_MFA_METHOD_TOTP,
		},
		{
			name:     "WebAuthn method",
			method:   iam.MFAMethodWebAuthn,
			expected: iampb.MFAMethod_MFA_METHOD_WEBAUTHN,
		},
		{
			name:     "unknown defaults to unspecified",
			method:   iam.MFAMethod("unknown"),
			expected: iampb.MFAMethod_MFA_METHOD_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMFAMethodToProto(tt.method)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertMFAError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected codes.Code
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: codes.OK, // nil error returns nil
		},
		{
			name:     "not found error",
			err:      &errors.QBError{Code: errors.ErrNotFound},
			expected: codes.NotFound,
		},
		{
			name:     "already exists error",
			err:      &errors.QBError{Code: errors.ErrAlreadyExists},
			expected: codes.AlreadyExists,
		},
		{
			name:     "invalid input error",
			err:      &errors.QBError{Code: errors.ErrInvalidInput},
			expected: codes.InvalidArgument,
		},
		{
			name:     "validation error",
			err:      &errors.QBError{Code: errors.ErrValidation},
			expected: codes.InvalidArgument,
		},
		{
			name:     "unauthorized error",
			err:      &errors.QBError{Code: errors.ErrUnauthorized},
			expected: codes.Unauthenticated,
		},
		{
			name:     "forbidden error",
			err:      &errors.QBError{Code: errors.ErrForbidden},
			expected: codes.PermissionDenied,
		},
		{
			name:     "timeout error",
			err:      &errors.QBError{Code: errors.ErrTimeout},
			expected: codes.DeadlineExceeded,
		},
		{
			name:     "deadline exceeded error",
			err:      &errors.QBError{Code: errors.ErrDeadlineExceeded},
			expected: codes.DeadlineExceeded,
		},
		{
			name:     "resource exhausted error",
			err:      &errors.QBError{Code: errors.ErrResourceExhausted},
			expected: codes.ResourceExhausted,
		},
		{
			name:     "unavailable error",
			err:      &errors.QBError{Code: errors.ErrUnavailable},
			expected: codes.Unavailable,
		},
		{
			name:     "conflict error",
			err:      &errors.QBError{Code: errors.ErrConflict},
			expected: codes.FailedPrecondition,
		},
		{
			name:     "unimplemented error",
			err:      &errors.QBError{Code: errors.ErrUnimplemented},
			expected: codes.Unimplemented,
		},
		{
			name:     "not implemented error",
			err:      &errors.QBError{Code: errors.ErrNotImplemented},
			expected: codes.Unimplemented,
		},
		{
			name:     "internal error",
			err:      &errors.QBError{Code: errors.ErrInternal},
			expected: codes.Internal,
		},
		{
			name:     "non-QBError",
			err:      stderrors.New("internal failure"),
			expected: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMFAError(tt.err)
			if tt.err == nil {
				assert.Nil(t, result)
			} else {
				st, ok := status.FromError(result)
				require.True(t, ok)
				assert.Equal(t, tt.expected, st.Code())
			}
		})
	}
}
