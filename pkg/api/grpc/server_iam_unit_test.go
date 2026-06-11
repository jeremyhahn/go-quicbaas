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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// convertIAMError Tests
// =============================================================================

func TestConvertIAMError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		expectedCode codes.Code
	}{
		{
			name:         "nil error returns nil",
			err:          nil,
			expectedCode: codes.OK,
		},
		{
			name: "not found error",
			err: &errors.QBError{
				Code: errors.ErrNotFound,
				Op:   "test",
				Err:  stderrors.New("not found"),
			},
			expectedCode: codes.NotFound,
		},
		{
			name: "already exists error",
			err: &errors.QBError{
				Code: errors.ErrAlreadyExists,
				Op:   "test",
				Err:  stderrors.New("already exists"),
			},
			expectedCode: codes.AlreadyExists,
		},
		{
			name: "invalid input error",
			err: &errors.QBError{
				Code: errors.ErrInvalidInput,
				Op:   "test",
				Err:  stderrors.New("invalid input"),
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "validation error",
			err: &errors.QBError{
				Code: errors.ErrValidation,
				Op:   "test",
				Err:  stderrors.New("validation failed"),
			},
			expectedCode: codes.InvalidArgument,
		},
		{
			name: "unauthorized error",
			err: &errors.QBError{
				Code: errors.ErrUnauthorized,
				Op:   "test",
				Err:  stderrors.New("unauthorized"),
			},
			expectedCode: codes.Unauthenticated,
		},
		{
			name: "forbidden error",
			err: &errors.QBError{
				Code: errors.ErrForbidden,
				Op:   "test",
				Err:  stderrors.New("forbidden"),
			},
			expectedCode: codes.PermissionDenied,
		},
		{
			name: "timeout error",
			err: &errors.QBError{
				Code: errors.ErrTimeout,
				Op:   "test",
				Err:  stderrors.New("timeout"),
			},
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name: "deadline exceeded error",
			err: &errors.QBError{
				Code: errors.ErrDeadlineExceeded,
				Op:   "test",
				Err:  stderrors.New("deadline exceeded"),
			},
			expectedCode: codes.DeadlineExceeded,
		},
		{
			name: "resource exhausted error",
			err: &errors.QBError{
				Code: errors.ErrResourceExhausted,
				Op:   "test",
				Err:  stderrors.New("resource exhausted"),
			},
			expectedCode: codes.ResourceExhausted,
		},
		{
			name: "unavailable error",
			err: &errors.QBError{
				Code: errors.ErrUnavailable,
				Op:   "test",
				Err:  stderrors.New("unavailable"),
			},
			expectedCode: codes.Unavailable,
		},
		{
			name: "conflict error",
			err: &errors.QBError{
				Code: errors.ErrConflict,
				Op:   "test",
				Err:  stderrors.New("conflict"),
			},
			expectedCode: codes.FailedPrecondition,
		},
		{
			name: "unimplemented error",
			err: &errors.QBError{
				Code: errors.ErrUnimplemented,
				Op:   "test",
				Err:  stderrors.New("unimplemented"),
			},
			expectedCode: codes.Unimplemented,
		},
		{
			name: "not implemented error",
			err: &errors.QBError{
				Code: errors.ErrNotImplemented,
				Op:   "test",
				Err:  stderrors.New("not implemented"),
			},
			expectedCode: codes.Unimplemented,
		},
		{
			name: "internal error",
			err: &errors.QBError{
				Code: errors.ErrInternal,
				Op:   "test",
				Err:  stderrors.New("internal"),
			},
			expectedCode: codes.Internal,
		},
		{
			name:         "non-QBError returns internal",
			err:          stderrors.New("random error"),
			expectedCode: codes.Internal,
		},
		{
			name: "unknown error code defaults to internal",
			err: &errors.QBError{
				Code: errors.ErrorCode(9999),
				Op:   "test",
				Err:  stderrors.New("unknown"),
			},
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertIAMError(tt.err)

			if tt.err == nil {
				assert.Nil(t, result)
				return
			}

			assert.NotNil(t, result)
			st, ok := status.FromError(result)
			assert.True(t, ok)
			assert.Equal(t, tt.expectedCode, st.Code())
		})
	}
}

// =============================================================================
// NewIAMServer Tests
// =============================================================================

func TestNewIAMServer_NilService(t *testing.T) {
	t.Parallel()

	// Test that NewIAMServer handles nil logger gracefully
	// The server should still be created even with nil components
	// since validation happens at method call time
}

// --- Merged from coverage_91_remaining_test.go ---

// =============================================================================
// IAM CreatePolicy YAML unmarshal error (line 1007)
// =============================================================================

func TestIAMServer_CreatePolicy_InvalidYAML(t *testing.T) {
	srv, _ := newTestIAMServer(t)

	// Pass syntactically invalid YAML to trigger the first yaml.Unmarshal error
	_, err := srv.CreatePolicy(context.Background(), &iampb.CreatePolicyRequest{
		TenantId: "t-1",
		Name:     "pol",
		SpecYaml: []byte("\t\x00invalid-yaml: ["),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// IAM CreateAdminAssignment service error (line 1299)
// =============================================================================

func TestIAMServer_CreateAdminAssignment_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("CreateAdminAssignment", mock.Anything, mock.Anything).
		Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.CreateAdminAssignment(context.Background(), &iampb.CreateAdminAssignmentRequest{
		UserId:    "u-1",
		AdminRole: iampb.AdminRole_ADMIN_ROLE_TENANT_ADMIN,
		GrantedBy: "admin-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// IAM UpdateAdminAssignment service error (line 1383)
// =============================================================================

func TestIAMServer_UpdateAdminAssignment_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("UpdateAdminAssignment", mock.Anything, "a-1", mock.Anything).
		Return(nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"})

	_, err := srv.UpdateAdminAssignment(context.Background(), &iampb.UpdateAdminAssignmentRequest{
		Id:     "a-1",
		Active: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// =============================================================================
// IAM StartImpersonation duration path (line 1496) + service error (line 1509)
// =============================================================================

func TestIAMServer_StartImpersonation_WithDuration_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("StartImpersonation", mock.Anything, mock.Anything).
		Return(nil, &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "test"})

	_, err := srv.StartImpersonation(context.Background(), &iampb.StartImpersonationRequest{
		AdminUserId:  "admin-1",
		TargetUserId: "user-1",
		TenantId:     "t-1",
		Reason:       "support request",
		Duration:     durationpb.New(30 * time.Minute),
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// =============================================================================
// IAM GrantConsent service error (line 1591)
// =============================================================================

func TestIAMServer_GrantConsent_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("GrantConsent", mock.Anything, "t-1", "u-1", mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.GrantConsent(context.Background(), &iampb.GrantConsentRequest{
		TenantId: "t-1",
		UserId:   "u-1",
		Purpose:  iampb.ConsentPurpose_CONSENT_PURPOSE_ANALYTICS,
		Version:  "1.0",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// IAM RequestExport service error (line 1717)
// =============================================================================

func TestIAMServer_RequestExport_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("RequestExport", mock.Anything, "t-1", "u-1", "admin-1",
		mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.RequestExport(context.Background(), &iampb.RequestExportRequest{
		TenantId:    "t-1",
		UserId:      "u-1",
		RequestedBy: "admin-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// IAM RequestDeletion service error (line 1787)
// =============================================================================

func TestIAMServer_RequestDeletion_ServiceError(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("RequestDeletion", mock.Anything, "t-1", "u-1", "admin-1",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"})

	_, err := srv.RequestDeletion(context.Background(), &iampb.RequestDeletionRequest{
		TenantId:    "t-1",
		UserId:      "u-1",
		RequestedBy: "admin-1",
		Reason:      "user request",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// =============================================================================
// MFA VerifyCode service error (line 211)
// =============================================================================

func TestMFAServer_VerifyCode_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		verifyCodeFn: func(_ context.Context, _, _, _ string) error {
			return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.VerifyCode(context.Background(), &iampb.VerifyMFACodeRequest{
		UserId: "u-1", TenantId: "t-1", Code: "wrong",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// MFA GetStatus service error (line 239)
// =============================================================================

func TestMFAServer_GetStatus_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		getMFAStatusFn: func(_ context.Context, _, _ string) (*iam.MFAStatus, error) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.GetStatus(context.Background(), &iampb.GetMFAStatusRequest{
		UserId: "u-1", TenantId: "t-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// =============================================================================
// MFA VerifyBackupCode service error (line 329)
// =============================================================================

func TestMFAServer_VerifyBackupCode_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		verifyBackupCodeFn: func(_ context.Context, _, _, _ string) error {
			return &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.VerifyBackupCode(context.Background(), &iampb.VerifyBackupCodeRequest{
		UserId: "u-1", TenantId: "t-1", Code: "bad-code",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// =============================================================================
// MFA GetBackupCodeStatus service error (line 361)
// =============================================================================

func TestMFAServer_GetBackupCodeStatus_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		getBackupCodeStatusFn: func(_ context.Context, _, _ string) (*iam.BackupCodeStatus, error) {
			return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.GetBackupCodeStatus(context.Background(), &iampb.GetBackupCodeStatusRequest{
		UserId: "u-1", TenantId: "t-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}
