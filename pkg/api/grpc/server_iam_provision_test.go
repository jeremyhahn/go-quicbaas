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
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newProvisionIAMServer(t *testing.T, svc IAMService) *IAMServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return NewIAMServer(svc, log)
}

func TestIAMServer_ProvisionTenant_InviteMode(t *testing.T) {
	svc := &mockIAMService{}
	expires := time.Now().Add(time.Hour)
	svc.On("ProvisionTenant", mock.Anything, mock.MatchedBy(func(r *service.IAMProvisionTenantRequest) bool {
		return r.TenantID == "acme" && r.AdminEnrollment == service.IAMTenantAdminEnrollInvite && r.InviteExpiryHours == 12
	})).Return(&service.IAMProvisionTenantResponse{
		TenantID: "acme", AdminUserID: 7, AdminPending: true,
		InviteToken: "tok", InviteExpiresAt: &expires, Timestamp: time.Now(),
	}, nil)
	srv := newProvisionIAMServer(t, svc)

	resp, err := srv.ProvisionTenant(context.Background(), &iampb.ProvisionTenantRequest{
		TenantId: "acme", AdminEnrollment: "invite", InviteExpiryHours: 12,
	})
	require.NoError(t, err)
	assert.Equal(t, "tok", resp.InviteToken)
	assert.Equal(t, "7", resp.AdminUserId)
	assert.True(t, resp.AdminPending)
	require.NotNil(t, resp.InviteExpiresAt)
	assert.True(t, resp.InviteExpiresAt.AsTime().Equal(expires))
	svc.AssertExpectations(t)
}

func TestIAMServer_ProvisionTenant_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("missing tenant id never reaches the service", func(t *testing.T) {
		svc := &mockIAMService{}
		_, err := newProvisionIAMServer(t, svc).ProvisionTenant(ctx, &iampb.ProvisionTenantRequest{})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		svc.AssertNotCalled(t, "ProvisionTenant", mock.Anything, mock.Anything)
	})

	t.Run("service errors keep their class", func(t *testing.T) {
		svc := &mockIAMService{}
		svc.On("ProvisionTenant", mock.Anything, mock.Anything).
			Return(nil, errors.New(errors.ErrInvalidInput, "iam.ProvisionTenant", "admin CSR required"))
		_, err := newProvisionIAMServer(t, svc).ProvisionTenant(ctx, &iampb.ProvisionTenantRequest{TenantId: "acme"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

// validatingTenantProvisioner stands in for the server's provisioner at its
// boundary: it applies the request's own validation and wraps a refusal as
// invalid input, exactly as tenantProvisioner.Provision does before any
// write, and answers a valid cert-mode request with an issued certificate.
type validatingTenantProvisioner struct {
	got     *service.IAMProvisionTenantRequest
	refusal error
}

func (p *validatingTenantProvisioner) Provision(_ context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	p.got = req
	if err := req.Validate(); err != nil {
		p.refusal = err
		return nil, errors.Wrap(err, errors.ErrInvalidInput, "iam.ProvisionTenant")
	}
	return &service.IAMProvisionTenantResponse{
		TenantID: req.TenantID, AdminUserID: 7, AdminRoleID: "role-1",
		AdminCertPEM: "issued-cert", AdminCertThumbprint: "spki", Timestamp: time.Now(),
	}, nil
}

func newCertModeIAMServer(t *testing.T) (*IAMServer, *validatingTenantProvisioner) {
	t.Helper()
	prov := &validatingTenantProvisioner{}
	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	adapter.SetTenantProvisioner(prov)
	return newProvisionIAMServer(t, adapter), prov
}

// newProvisionCSR returns a PEM certificate request signed by key.
func newProvisionCSR(t *testing.T, key crypto.Signer) string {
	t.Helper()
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "admin"},
	}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func TestIAMServer_ProvisionTenant_CertMode(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	csrPEM := newProvisionCSR(t, key)
	srv, prov := newCertModeIAMServer(t)

	resp, err := srv.ProvisionTenant(context.Background(), &iampb.ProvisionTenantRequest{
		TenantId: "acme", IsolationType: "prefix", AdminEnrollment: "cert", AdminCsrPem: csrPEM,
	})
	require.NoError(t, err)

	require.NotNil(t, prov.got)
	assert.Equal(t, service.IAMTenantAdminEnrollCert, prov.got.AdminEnrollment)
	assert.Equal(t, csrPEM, prov.got.AdminCSRPEM, "the CSR reaches the provisioner unchanged")
	assert.Equal(t, "acme", resp.TenantId)
	assert.Equal(t, "issued-cert", resp.AdminCertPem)
	assert.Equal(t, "spki", resp.AdminCertThumbprint)
	assert.False(t, resp.AdminPending, "a cert-enrolled admin is active at once")
	assert.Empty(t, resp.InviteToken, "cert mode issues no invite")
	assert.Nil(t, resp.InviteExpiresAt)
}

func TestIAMServer_ProvisionTenant_CertModeRefusesBadCSR(t *testing.T) {
	signer, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	weak, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)

	tests := []struct {
		name string
		csr  string
		want error
	}{
		{
			name: "missing CSR",
			csr:  "",
			want: &service.ProvisionAdminCSRRequiredError{},
		},
		{
			name: "not a certificate request",
			csr:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("x")})),
			want: &service.ProvisionAdminCSRInvalidError{Reason: "not a PEM CERTIFICATE REQUEST block"},
		},
		{
			name: "signature from a different key",
			csr:  mismatchedProvisionCSR(t, signer, &other.PublicKey),
			want: &service.ProvisionAdminCSRInvalidError{Reason: "signature does not verify"},
		},
		{
			name: "key below policy",
			csr:  newProvisionCSR(t, weak),
			want: &service.ProvisionAdminCSRKeyError{Key: "RSA-1024"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, prov := newCertModeIAMServer(t)
			_, err := srv.ProvisionTenant(context.Background(), &iampb.ProvisionTenantRequest{
				TenantId: "acme", IsolationType: "prefix", AdminEnrollment: "cert", AdminCsrPem: tt.csr,
			})
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err), "a bad CSR is the caller's fault: %v", err)

			// The refusal is the typed error for the check that failed, and the
			// status message carries it so the caller learns which one.
			require.IsType(t, tt.want, prov.refusal)
			assert.Contains(t, status.Convert(err).Message(), prov.refusal.Error())
			if want, ok := tt.want.(*service.ProvisionAdminCSRInvalidError); ok {
				got, isInvalid := prov.refusal.(*service.ProvisionAdminCSRInvalidError)
				require.True(t, isInvalid)
				assert.Equal(t, want.Reason, got.Reason, "the failing check")
			}
		})
	}
}

// mismatchedProvisionCSR returns a CSR signed by signer that claims pub, so
// its self-signature does not verify: a request whose key is not the one
// that signed it.
func mismatchedProvisionCSR(t *testing.T, signer *ecdsa.PrivateKey, pub *ecdsa.PublicKey) string {
	t.Helper()
	block, _ := pem.Decode([]byte(newProvisionCSR(t, signer)))
	require.NotNil(t, block)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	require.NoError(t, err)

	claimed, err := x509.MarshalPKIXPublicKey(pub)
	require.NoError(t, err)
	original, err := x509.MarshalPKIXPublicKey(csr.PublicKey)
	require.NoError(t, err)
	tampered := bytes.Replace(block.Bytes, original, claimed, 1)
	require.NotEqual(t, block.Bytes, tampered, "the claimed key must replace the signing key")
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: tampered}))
}

type recordingTenantProvisioner struct {
	got *service.IAMProvisionTenantRequest
	err error
}

func (p *recordingTenantProvisioner) Provision(_ context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	p.got = req
	if p.err != nil {
		return nil, p.err
	}
	return &service.IAMProvisionTenantResponse{TenantID: req.TenantID}, nil
}

func TestIAMServiceAdapter_ProvisionTenant(t *testing.T) {
	ctx := context.Background()
	req := &service.IAMProvisionTenantRequest{TenantID: "acme"}

	adapter := NewIAMServiceAdapter(nil, nil, nil, nil, nil, nil)
	_, err := adapter.ProvisionTenant(ctx, req)
	require.Error(t, err, "no provisioner wired")

	prov := &recordingTenantProvisioner{}
	adapter.SetTenantProvisioner(prov)
	resp, err := adapter.ProvisionTenant(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, "acme", resp.TenantID)
	assert.Same(t, req, prov.got)

	want := stderrors.New("provision failed")
	adapter.SetTenantProvisioner(&recordingTenantProvisioner{err: want})
	_, err = adapter.ProvisionTenant(ctx, req)
	assert.ErrorIs(t, err, want)
}

func TestAuthzMapping_ProvisionTenantRequiresTenantCreate(t *testing.T) {
	perm, ok := grpcMethodPermissions["/quicbaas.iam.v1.IAMService/ProvisionTenant"]
	require.True(t, ok, "ProvisionTenant must be mapped or RequireAuthz denies it")
	assert.Equal(t, "tenants", perm.resource)
	assert.Equal(t, "create", perm.action)
}
