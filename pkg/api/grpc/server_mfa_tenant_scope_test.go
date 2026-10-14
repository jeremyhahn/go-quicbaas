// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestMFAServer_ObjectVerbsStayInCallerTenant proves the gRPC MFA
// complete/cancel/verify verbs bind the enrollment or challenge id to the
// caller's tenant: another tenant gets NotFound and the record survives, a
// request with no tenant is refused, and the owning tenant reaches it.
func TestMFAServer_ObjectVerbsStayInCallerTenant(t *testing.T) {
	t.Parallel()

	storage := iam.NewMemoryMFAStorage()
	mgr, err := iam.NewMFAManager(iam.WithMFAStorage(storage))
	require.NoError(t, err)
	srv := newTestMFAServer(NewMFAServiceAdapter(mgr))

	enrollment, err := mgr.BeginEnrollment(context.Background(), 20002, "tenant-b", iam.MFAMethodTOTP)
	require.NoError(t, err)

	intruder := tenantctx.WithTenant(context.Background(), "tenant-a")
	_, err = srv.CompleteEnrollment(intruder, &iampb.CompleteMFAEnrollmentRequest{EnrollmentId: enrollment.EnrollmentID, Code: "000000"})
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-tenant complete: %v", err)
	_, err = srv.CancelEnrollment(intruder, &iampb.CancelMFAEnrollmentRequest{EnrollmentId: enrollment.EnrollmentID})
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-tenant cancel: %v", err)
	_, err = srv.VerifyChallenge(intruder, &iampb.VerifyMFAChallengeRequest{ChallengeId: "challenge-b", Code: "000000"})
	assert.Equal(t, codes.NotFound, status.Code(err), "cross-tenant verify: %v", err)

	_, err = srv.CancelEnrollment(context.Background(), &iampb.CancelMFAEnrollmentRequest{EnrollmentId: enrollment.EnrollmentID})
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "a request with no tenant must be refused: %v", err)

	stored, err := storage.GetEnrollment(context.Background(), enrollment.EnrollmentID)
	require.NoError(t, err, "refused verbs must leave the enrollment in place")
	assert.Zero(t, stored.Attempts)

	owner := tenantctx.WithTenant(context.Background(), "tenant-b")
	resp, err := srv.CancelEnrollment(owner, &iampb.CancelMFAEnrollmentRequest{EnrollmentId: enrollment.EnrollmentID})
	require.NoError(t, err)
	assert.True(t, resp.Success)
}

// TestMFAServer_UserVerbsNameOnlyCallerTenant proves the gRPC MFA verbs that
// carry a tenant_id honour it only when it is the caller's own tenant: naming
// another tenant is PermissionDenied before the MFA manager is reached, and
// the owning tenant still succeeds.
func TestMFAServer_UserVerbsNameOnlyCallerTenant(t *testing.T) {
	t.Parallel()

	mgr, err := iam.NewMFAManager(iam.WithMFAStorage(iam.NewMemoryMFAStorage()))
	require.NoError(t, err)
	srv := newTestMFAServer(NewMFAServiceAdapter(mgr))

	const user, victim = "20002", "tenant-b"
	verbs := map[string]func(context.Context) error{
		"BeginEnrollment": func(ctx context.Context) error {
			_, err := srv.BeginEnrollment(ctx, &iampb.BeginMFAEnrollmentRequest{TenantId: victim, UserId: user})
			return err
		},
		"BeginChallenge": func(ctx context.Context) error {
			_, err := srv.BeginChallenge(ctx, &iampb.BeginMFAChallengeRequest{TenantId: victim, UserId: user, SessionToken: "session-b"})
			return err
		},
		"VerifyCode": func(ctx context.Context) error {
			_, err := srv.VerifyCode(ctx, &iampb.VerifyMFACodeRequest{TenantId: victim, UserId: user, Code: "000000"})
			return err
		},
		"GetStatus": func(ctx context.Context) error {
			_, err := srv.GetStatus(ctx, &iampb.GetMFAStatusRequest{TenantId: victim, UserId: user})
			return err
		},
		"Disable": func(ctx context.Context) error {
			_, err := srv.Disable(ctx, &iampb.DisableMFARequest{TenantId: victim, UserId: user})
			return err
		},
		"GenerateBackupCodes": func(ctx context.Context) error {
			_, err := srv.GenerateBackupCodes(ctx, &iampb.GenerateBackupCodesRequest{TenantId: victim, UserId: user})
			return err
		},
		"VerifyBackupCode": func(ctx context.Context) error {
			_, err := srv.VerifyBackupCode(ctx, &iampb.VerifyBackupCodeRequest{TenantId: victim, UserId: user, Code: "00000000"})
			return err
		},
		"GetBackupCodeStatus": func(ctx context.Context) error {
			_, err := srv.GetBackupCodeStatus(ctx, &iampb.GetBackupCodeStatusRequest{TenantId: victim, UserId: user})
			return err
		},
	}

	intruder := tenantctx.WithTenant(context.Background(), "tenant-a")
	for name, verb := range verbs {
		err := verb(intruder)
		assert.Equal(t, codes.PermissionDenied, status.Code(err), "%s naming another tenant: %v", name, err)
	}

	owner := tenantctx.WithTenant(context.Background(), victim)
	require.NoError(t, verbs["BeginEnrollment"](owner))
	require.NoError(t, verbs["GetStatus"](owner))
}
