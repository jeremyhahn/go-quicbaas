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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func TestMFAServer_CompleteEnrollment_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		completeEnrollmentFn: func(_ context.Context, _, _ string) error {
			return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.CompleteEnrollment(tenantctx.WithTenant(context.Background(), "t-1"), &iampb.CompleteMFAEnrollmentRequest{
		EnrollmentId: "enroll-1", Code: "123456",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestMFAServer_CancelEnrollment_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		cancelEnrollmentFn: func(_ context.Context, _ string) error {
			return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.CancelEnrollment(tenantctx.WithTenant(context.Background(), "t-1"), &iampb.CancelMFAEnrollmentRequest{
		EnrollmentId: "enroll-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestMFAServer_BeginChallenge_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		beginChallengeFn: func(_ context.Context, _, _, _ string) (*iam.MFAChallenge, error) {
			return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.BeginChallenge(tenantctx.WithTenant(context.Background(), "t-1"), &iampb.BeginMFAChallengeRequest{
		UserId: "u-1", TenantId: "t-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestMFAServer_VerifyChallenge_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		verifyChallengeFn: func(_ context.Context, _, _ string) (*iam.MFAVerifyResponse, error) {
			return nil, &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.VerifyChallenge(tenantctx.WithTenant(context.Background(), "t-1"), &iampb.VerifyMFAChallengeRequest{
		ChallengeId: "ch-1", Code: "123456",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestMFAServer_Disable_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		disableMFAFn: func(_ context.Context, _, _ string) error {
			return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.Disable(tenantctx.WithTenant(context.Background(), "t-1"), &iampb.DisableMFARequest{
		UserId: "u-1", TenantId: "t-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestMFAServer_GenerateBackupCodes_ServiceError(t *testing.T) {
	svc := &mockMFAService{
		generateBackupCodesFn: func(_ context.Context, _, _ string) ([]string, error) {
			return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"}
		},
	}
	srv := newTestMFAServer(svc)
	_, err := srv.GenerateBackupCodes(tenantctx.WithTenant(context.Background(), "t-1"), &iampb.GenerateBackupCodesRequest{
		UserId: "u-1", TenantId: "t-1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}
