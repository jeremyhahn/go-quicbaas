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
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// stubInviteActivation records the scope each verb receives.
type stubInviteActivation struct {
	gotTenant, gotToken string
	gotCredential       map[string]interface{}
	gotCert             []byte
	meta                *service.InviteMetadata
	activation          *service.InviteActivation
	err                 error
}

func (s *stubInviteActivation) GetInvite(_ context.Context, tenantID, token string) (*service.InviteMetadata, error) {
	s.gotTenant, s.gotToken = tenantID, token
	return s.meta, s.err
}

func (s *stubInviteActivation) ActivateFIDO2Begin(_ context.Context, tenantID, token string) (map[string]interface{}, error) {
	s.gotTenant, s.gotToken = tenantID, token
	if s.err != nil {
		return nil, s.err
	}
	return map[string]interface{}{"publicKey": map[string]interface{}{"challenge": "c"}}, nil
}

func (s *stubInviteActivation) ActivateFIDO2Finish(_ context.Context, tenantID, token string, credential map[string]interface{}) (*service.InviteActivation, error) {
	s.gotTenant, s.gotToken, s.gotCredential = tenantID, token, credential
	return s.activation, s.err
}

func (s *stubInviteActivation) ActivatePIV(_ context.Context, tenantID, token string, certPEM []byte) (*service.InviteActivation, error) {
	s.gotTenant, s.gotToken, s.gotCert = tenantID, token, certPEM
	return s.activation, s.err
}

func newInviteIAMServer(t *testing.T, activation service.InviteActivationService) *IAMServer {
	t.Helper()
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	adapter.SetInviteActivation(activation)
	return newProvisionIAMServer(t, adapter)
}

func TestIAMServer_GetInvite(t *testing.T) {
	expires := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	stub := &stubInviteActivation{meta: &service.InviteMetadata{
		UserID: 30001, Email: "a@example.com", AuthMethods: []string{"fido2", "piv"}, ExpiresAt: expires,
	}}

	resp, err := newInviteIAMServer(t, stub).GetInvite(context.Background(),
		&iampb.GetInviteRequest{TenantId: "t1", Token: "tok"})
	require.NoError(t, err)
	assert.Equal(t, "t1", stub.gotTenant)
	assert.Equal(t, "tok", stub.gotToken)
	assert.Equal(t, "30001", resp.UserId)
	assert.Equal(t, []string{"fido2", "piv"}, resp.AuthMethods)
	assert.True(t, resp.ExpiresAt.AsTime().Equal(expires))
}

func TestIAMServer_ActivateFIDO2(t *testing.T) {
	stub := &stubInviteActivation{activation: &service.InviteActivation{UserID: 30001, Warning: "w"}}
	srv := newInviteIAMServer(t, stub)
	ctx := context.Background()

	begin, err := srv.ActivateFIDO2Begin(ctx, &iampb.ActivateInviteRequest{TenantId: "t1", Token: "tok"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"publicKey":{"challenge":"c"}}`, begin.OptionsJson)

	finish, err := srv.ActivateFIDO2Finish(ctx, &iampb.ActivateFIDO2FinishRequest{
		TenantId: "t1", Token: "tok", CredentialJson: `{"id":"cred"}`,
	})
	require.NoError(t, err)
	assert.Equal(t, "cred", stub.gotCredential["id"])
	assert.Equal(t, "30001", finish.UserId)
	assert.Equal(t, "activation successful", finish.Message)
	assert.Equal(t, "w", finish.Warning)
}

// TestIAMServer_ActivateFIDO2Finish_Credential keeps a malformed credential an
// input error and lets an absent one reach the service, which owns the
// "credential is required" rule.
func TestIAMServer_ActivateFIDO2Finish_Credential(t *testing.T) {
	stub := &stubInviteActivation{err: service.ErrInviteActivationCredentialRequired}
	srv := newInviteIAMServer(t, stub)
	ctx := context.Background()

	_, err := srv.ActivateFIDO2Finish(ctx, &iampb.ActivateFIDO2FinishRequest{
		TenantId: "t1", Token: "tok", CredentialJson: "not json",
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Empty(t, stub.gotToken, "a malformed credential must not reach the service")

	_, err = srv.ActivateFIDO2Finish(ctx, &iampb.ActivateFIDO2FinishRequest{TenantId: "t1", Token: "tok"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Nil(t, stub.gotCredential)
}

func TestIAMServer_ActivatePIV(t *testing.T) {
	stub := &stubInviteActivation{activation: &service.InviteActivation{UserID: 30001}}

	resp, err := newInviteIAMServer(t, stub).ActivatePIV(context.Background(),
		&iampb.ActivatePIVRequest{TenantId: "t1", Token: "tok", CertPem: "PEM"})
	require.NoError(t, err)
	assert.Equal(t, "PEM", string(stub.gotCert))
	assert.Equal(t, "PIV activation successful", resp.Message)
}

// TestIAMServer_InviteErrors pins the code each failure class maps to. The
// unusable case is opaque: absent, expired and spent tokens are one answer.
func TestIAMServer_InviteErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want codes.Code
	}{
		"Unusable":        {&iam.InviteUnusableError{Reason: iam.InviteAbsent}, codes.NotFound},
		"NotPending":      {&iam.UserNotPendingError{}, codes.FailedPrecondition},
		"CertRejected":    {&iam.PIVCertRejectedError{}, codes.InvalidArgument},
		"TokenRequired":   {service.ErrInviteActivationTokenRequired, codes.InvalidArgument},
		"PIVUnconfigured": {service.ErrInviteActivationPIVNotConfigured, codes.Unimplemented},
		"Untyped":         {stderrors.New("store down"), codes.Internal},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newInviteIAMServer(t, &stubInviteActivation{err: tc.err}).ActivatePIV(
				context.Background(), &iampb.ActivatePIVRequest{TenantId: "t1", Token: "tok", CertPem: "PEM"})
			assert.Equal(t, tc.want, status.Code(err))
		})
	}
}

// TestIAMServer_ActivateFIDO2_TenantRefusedIsFailedPrecondition: a non-system
// tenant's passkey activation is refused until go-quicbaas #1816, and both
// verbs answer FailedPrecondition with the refusal's guidance, as login does.
func TestIAMServer_ActivateFIDO2_TenantRefusedIsFailedPrecondition(t *testing.T) {
	srv := newInviteIAMServer(t, &stubInviteActivation{err: service.RefuseTenantLogin("acme")})
	ctx := context.Background()

	_, beginErr := srv.ActivateFIDO2Begin(ctx, &iampb.ActivateInviteRequest{TenantId: "acme", Token: "tok"})
	_, finishErr := srv.ActivateFIDO2Finish(ctx, &iampb.ActivateFIDO2FinishRequest{
		TenantId: "acme", Token: "tok", CredentialJson: `{"id":"x"}`,
	})

	for name, err := range map[string]error{"begin": beginErr, "finish": finishErr} {
		assert.Equal(t, codes.FailedPrecondition, status.Code(err), name)
		assert.Equal(t, (&sdkerrors.TenantLoginRefusedError{}).Error(), status.Convert(err).Message(), name)
	}
}

func TestIAMServer_InviteUnconfigured(t *testing.T) {
	ctx := context.Background()
	req := &iampb.GetInviteRequest{TenantId: "t1", Token: "tok"}

	_, err := newInviteIAMServer(t, nil).GetInvite(ctx, req)
	assert.Equal(t, codes.Unavailable, status.Code(err), "adapter without a service")

	_, err = newProvisionIAMServer(t, &mockIAMService{}).GetInvite(ctx, req)
	assert.Equal(t, codes.Unimplemented, status.Code(err), "IAM service without the capability")
}

// TestAuthzMapping_InviteVerbsArePublic keeps the invite verbs pre-auth: an
// invitee has no token to present until activation completes.
func TestAuthzMapping_InviteVerbsArePublic(t *testing.T) {
	for _, m := range []string{"GetInvite", "ActivateFIDO2Begin", "ActivateFIDO2Finish", "ActivatePIV"} {
		full := "/quicbaas.iam.v1.IAMService/" + m
		assert.True(t, isPublicGRPCMethod(full), "%s must be public", full)
		_, mapped := grpcMethodPermissions[full]
		assert.False(t, mapped, "%s must not also require a permission", full)
	}
}
