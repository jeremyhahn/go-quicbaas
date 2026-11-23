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

package server

import (
	"context"
	"math/big"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	"github.com/jeremyhahn/go-xkms/pkg/ca/provider"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestCallerTenantCA_FillsTheCallersTenant pins that the wrapped CA issues a
// request naming no tenant for the caller's own tenant, keeps a named tenant,
// and keeps platform scope for a system or absent identity.
func TestCallerTenantCA_FillsTheCallersTenant(t *testing.T) {
	inner, ok := newRealSoftwareCA(t).(provider.CA)
	require.True(t, ok, "the fixture CA is a provider.CA")
	wrapped := withCallerTenant(inner)

	acmeCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "acme-user", TenantID: "acme"})
	systemCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "platform-admin", TenantID: qrdbsdk.SystemTenantID})

	cases := []struct {
		name     string
		ctx      context.Context
		tenantID string
		want     string
	}{
		{name: "tenant-empty", ctx: acmeCtx, tenantID: "", want: "acme"},
		{name: "tenant-named", ctx: acmeCtx, tenantID: "acme", want: "acme"},
		{name: "system-empty", ctx: systemCtx, tenantID: "", want: ""},
		{name: "anonymous-empty", ctx: context.Background(), tenantID: "", want: ""},
	}
	for _, tc := range cases {
		t.Run("SignCSRWithSPIFFE/"+tc.name, func(t *testing.T) {
			leaf, err := wrapped.SignCSRWithSPIFFE(tc.ctx, newCSR(t, tc.name+"-user"), "client", 0, "", tc.tenantID, "", "")
			require.NoError(t, err)
			got, err := inner.CertificateTenant(context.Background(), leaf.SerialNumber)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
		t.Run("IssueCertificateRaw/"+tc.name, func(t *testing.T) {
			_, _, _, serialHex, err := wrapped.IssueCertificateRaw(tc.ctx, tc.name+"-svc", "", nil, 0, "client", "", tc.tenantID)
			require.NoError(t, err)
			serial, ok := new(big.Int).SetString(serialHex, 16)
			require.True(t, ok, "serial %q is hex", serialHex)
			got, err := inner.CertificateTenant(context.Background(), serial)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestCallerTenantCA_KeepsTCGCA pins that wrapping keeps a TCG-capable CA
// TCG-capable, so the go-xkms TCG servicer still finds it, and that wrapping
// twice is a no-op.
func TestCallerTenantCA_KeepsTCGCA(t *testing.T) {
	inner, ok := newRealSoftwareCA(t).(provider.CA)
	require.True(t, ok)
	wrapped := withCallerTenant(inner)

	_, innerTCG := inner.(provider.TCGCA)
	_, wrappedTCG := wrapped.(provider.TCGCA)
	require.Equal(t, innerTCG, wrappedTCG, "the wrapper is a TCGCA exactly when the inner CA is")
	require.True(t, wrappedTCG, "the software CA issues TCG certificates")
	require.Equal(t, wrapped, withCallerTenant(wrapped), "wrapping twice is a no-op")
	require.Nil(t, withCallerTenant(nil))
}

// callAs invokes service/method on the captured gRPC services as the caller
// in ctx.
func callAs(t *testing.T, services capturedGRPCServices, ctx context.Context, service, method string, req proto.Message) (any, error) {
	t.Helper()
	svc, ok := services[service]
	require.True(t, ok, "module registers %s", service)
	for _, m := range svc.desc.Methods {
		if m.MethodName == method {
			return m.Handler(svc.impl, ctx, func(v any) error {
				proto.Merge(v.(proto.Message), req)
				return nil
			}, nil)
		}
	}
	t.Fatalf("%s has no method %s", service, method)
	return nil, nil
}

// TestKeystoreCA_TenantIssuesThenRevokes: a tenant that requests a
// certificate over the TCP KeystoreService owns it, so it may revoke it, and
// another tenant may not.
func TestKeystoreCA_TenantIssuesThenRevokes(t *testing.T) {
	initOIDCSigningDefault(t)
	t.Cleanup(dbxkms.Reset)

	srv := &Server{config: &config.Config{}}
	module, err := xkmssdk.NewServiceModule(
		srv.xkmsModuleService,
		xkmssdk.WithServiceModuleQRDBClient(srv.GetQRDBNodeClient),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
	)
	require.NoError(t, err)
	services := capturedGRPCServices{}
	module.RegisterGRPC(services)

	srv.adoptCA(newRealSoftwareCA(t))
	srv.caSettled.Store(true)
	svc, err := xkmssdk.Get()
	require.NoError(t, err)

	const keystore = "xkms.v1.KeystoreService"
	acmeCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "acme-user", TenantID: "acme"})
	globexCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "globex-user", TenantID: "globex"})

	issuers := []struct {
		name  string
		issue func(t *testing.T) string
	}{
		{name: "SignCSR", issue: func(t *testing.T) string {
			resp, err := callAs(t, services, acmeCtx, keystore, "SignCSR",
				&xkmsv1.SignCSRRequest{CsrPem: newCSR(t, "acme-user"), Profile: "client"})
			require.NoError(t, err)
			got, ok := resp.(*xkmsv1.SignCSRResponse)
			require.True(t, ok, "got %T", resp)
			return parseLeaf(t, got.CertificatePem).SerialNumber.Text(16)
		}},
		{name: "IssueCertificate", issue: func(t *testing.T) string {
			resp, err := callAs(t, services, acmeCtx, keystore, "IssueCertificate",
				&xkmsv1.IssueCertificateRequest{CommonName: "acme-svc", Profile: "client"})
			require.NoError(t, err)
			got, ok := resp.(*xkmsv1.IssueCertificateResponse)
			require.True(t, ok, "got %T", resp)
			return parseLeaf(t, got.CertificatePem).SerialNumber.Text(16)
		}},
	}
	for _, is := range issuers {
		t.Run(is.name, func(t *testing.T) {
			serialHex := is.issue(t)
			serial, ok := new(big.Int).SetString(serialHex, 16)
			require.True(t, ok)
			owner, err := svc.CA().CertificateTenant(context.Background(), serial)
			require.NoError(t, err)
			require.Equal(t, "acme", owner, "the certificate is issued for the requesting tenant")

			_, err = callAs(t, services, globexCtx, keystore, "RevokeCertificate",
				&xkmsv1.RevokeCertificateRequest{SerialNumber: serialHex, Reason: 1})
			require.Error(t, err, "another tenant may not revoke it")

			_, err = callAs(t, services, acmeCtx, keystore, "RevokeCertificate",
				&xkmsv1.RevokeCertificateRequest{SerialNumber: serialHex, Reason: 1})
			require.NoError(t, err, "the issuing tenant revokes its own certificate")
		})
	}
}
