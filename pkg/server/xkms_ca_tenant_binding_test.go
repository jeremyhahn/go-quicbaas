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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"math/big"
	"sync/atomic"
	"testing"

	qrdbspiffe "github.com/jeremyhahn/go-qrdb/pkg/auth/spiffe"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	"github.com/jeremyhahn/go-xkms/pkg/ca/provider"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/caadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestCallerTenantCA_FillsTheCallersTenant pins that the wrapped CA issues a
// request naming no tenant for the caller's own tenant, keeps a named tenant,
// and keeps platform scope for a system or absent identity.
func TestCallerTenantCA_FillsTheCallersTenant(t *testing.T) {
	inner, ok := newRealSoftwareCA(t).(provider.CA)
	require.True(t, ok, "the fixture CA is a provider.CA")
	wrapped := withCallerTenant(inner, nil, "")

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
	wrapped := withCallerTenant(inner, nil, "")

	_, innerTCG := inner.(provider.TCGCA)
	_, wrappedTCG := wrapped.(provider.TCGCA)
	require.Equal(t, innerTCG, wrappedTCG, "the wrapper is a TCGCA exactly when the inner CA is")
	require.True(t, wrappedTCG, "the software CA issues TCG certificates")
	require.Equal(t, wrapped, withCallerTenant(wrapped, nil, ""), "wrapping twice is a no-op")
	require.Nil(t, withCallerTenant(nil, nil, ""))
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

// tcgRecordingCA is a provider.TCGCA that only counts TCG calls, so a test
// can prove the guard refused before the CA was reached.
type tcgRecordingCA struct {
	provider.CA
	calls *atomic.Int32
}

func (r tcgRecordingCA) IssueEKCertificateRaw(context.Context, string, string, []byte) ([]byte, error) {
	r.calls.Add(1)
	return nil, nil
}

func (r tcgRecordingCA) IssueAKCertificateRaw(context.Context, string, string, []byte) ([]byte, error) {
	r.calls.Add(1)
	return nil, nil
}

func (r tcgRecordingCA) SignTCGCSRIDevIDRaw(context.Context, string, string, []byte) ([]byte, []byte, error) {
	r.calls.Add(1)
	return nil, nil, nil
}

func (r tcgRecordingCA) EnrollDeviceRaw(context.Context, string, string, []byte) ([]byte, []byte, []byte, []byte, []byte, error) {
	r.calls.Add(1)
	return nil, nil, nil, nil, nil, nil
}

// tcgPublicKeyDER returns a fresh ECDSA P-256 public key in PKIX DER.
func tcgPublicKeyDER(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return der
}

// TestCallerTenantCA_TCGRefusesNonSystem pins that the wrapper refuses TCG
// issuance for a tenant caller and for an absent identity before the CA is
// reached, and admits the system tenant.
func TestCallerTenantCA_TCGRefusesNonSystem(t *testing.T) {
	inner, ok := newRealSoftwareCA(t).(provider.CA)
	require.True(t, ok)
	calls := &atomic.Int32{}
	wrapped, ok := withCallerTenant(tcgRecordingCA{CA: inner, calls: calls}, nil, "").(provider.TCGCA)
	require.True(t, ok, "the wrapper keeps the TCG CA")

	acmeCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "acme-user", TenantID: "acme"})
	systemCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "platform-admin", TenantID: qrdbsdk.SystemTenantID})
	pub := tcgPublicKeyDER(t)

	calls4 := func(ctx context.Context) []error {
		_, ekErr := wrapped.IssueEKCertificateRaw(ctx, "ek", "", pub)
		_, akErr := wrapped.IssueAKCertificateRaw(ctx, "ak", "", pub)
		_, _, idErr := wrapped.SignTCGCSRIDevIDRaw(ctx, "idevid", "", []byte{1})
		_, _, _, _, _, enErr := wrapped.EnrollDeviceRaw(ctx, "device", "", []byte{1})
		return []error{ekErr, akErr, idErr, enErr}
	}
	for name, ctx := range map[string]context.Context{"tenant": acmeCtx, "anonymous": context.Background()} {
		for i, err := range calls4(ctx) {
			require.ErrorIs(t, err, caadapter.ErrXKMSCATCGTenantUnsupported, "%s call %d", name, i)
		}
	}
	require.Zero(t, calls.Load(), "a refused TCG call never reaches the CA")

	for i, err := range calls4(systemCtx) {
		require.NoError(t, err, "system call %d", i)
	}
	require.Equal(t, int32(4), calls.Load(), "the system tenant reaches the CA")
}

// newKeystoreHarness mounts the go-xkms service module the way the server
// does, adopts a real software CA, and returns the captured gRPC services.
func newKeystoreHarness(t *testing.T, srv *Server) capturedGRPCServices {
	t.Helper()
	initOIDCSigningDefault(t)
	t.Cleanup(dbxkms.Reset)
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
	return services
}

// TestKeystoreCA_TCGRefusesNonSystemTenant: a tenant caller over the TCP
// KeystoreService cannot issue EK, AK, IDevID or device-enrollment
// certificates, as on every other transport.
func TestKeystoreCA_TCGRefusesNonSystemTenant(t *testing.T) {
	services := newKeystoreHarness(t, &Server{config: &config.Config{}})
	const keystore = "xkms.v1.KeystoreService"
	acmeCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "acme-user", TenantID: "acme"})
	pub := tcgPublicKeyDER(t)

	calls := []struct {
		method string
		req    proto.Message
	}{
		{"IssueEKCertificate", &xkmsv1.IssueEKCertificateRequest{CommonName: "ek", EkPublicKey: pub}},
		{"IssueAKCertificate", &xkmsv1.IssueAKCertificateRequest{CommonName: "ak", PublicKey: pub}},
		{"SignTCGCSR", &xkmsv1.SignTCGCSRRequest{CommonName: "idevid", TcgCsr: []byte{1}}},
		{"EnrollDevice", &xkmsv1.EnrollDeviceRequest{CommonName: "device", PackedCsr: []byte{1}}},
	}
	for _, c := range calls {
		t.Run(c.method, func(t *testing.T) {
			_, err := callAs(t, services, acmeCtx, keystore, c.method, c.req)
			require.Error(t, err, "a tenant may not issue TCG certificates")
		})
	}
	for _, c := range calls[:2] {
		_, err := callAs(t, services, acmeCtx, keystore, c.method, c.req)
		require.ErrorContains(t, err, caadapter.ErrXKMSCATCGTenantUnsupported.Error(), "%s is refused by the tenant guard", c.method)
	}
}

// TestKeystoreCA_TCGSystemTenantIssues: the guard admits the system tenant,
// so an EK certificate is issued over the TCP KeystoreService.
func TestKeystoreCA_TCGSystemTenantIssues(t *testing.T) {
	services := newKeystoreHarness(t, &Server{config: &config.Config{}})
	systemCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "platform-admin", TenantID: qrdbsdk.SystemTenantID})
	resp, err := callAs(t, services, systemCtx, "xkms.v1.KeystoreService", "IssueEKCertificate",
		&xkmsv1.IssueEKCertificateRequest{CommonName: "ek-device", EkPublicKey: tcgPublicKeyDER(t)})
	require.NoError(t, err)
	got, ok := resp.(*xkmsv1.IssueEKCertificateResponse)
	require.True(t, ok, "got %T", resp)
	require.NotEmpty(t, got.CertificateDer)
}

// requireNoSPIFFEURI fails when leaf carries any spiffe:// URI SAN.
func requireNoSPIFFEURI(t *testing.T, leaf *x509.Certificate) {
	t.Helper()
	for _, u := range leaf.URIs {
		require.NotEqual(t, "spiffe", u.Scheme, "unexpected SPIFFE URI SAN %s", u)
	}
}

// TestKeystoreCA_StampsConfiguredSPIFFETrustDomain: with a SPIFFE trust
// domain configured, a tenant's certificate issued over the TCP
// KeystoreService carries the tenant SPIFFE SAN, as on every other transport,
// and is still owned by the tenant.
func TestKeystoreCA_StampsConfiguredSPIFFETrustDomain(t *testing.T) {
	srv := &Server{config: &config.Config{}}
	srv.config.IAM.SPIFFETrustDomain = "example.com"
	services := newKeystoreHarness(t, srv)
	svc, err := xkmssdk.Get()
	require.NoError(t, err)
	const keystore = "xkms.v1.KeystoreService"
	acmeCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "acme-user", TenantID: "acme"})

	issuers := []struct {
		name     string
		wantKind string
		issue    func(t *testing.T) []byte
	}{
		{name: "SignCSR", wantKind: qrdbspiffe.KindUser, issue: func(t *testing.T) []byte {
			resp, err := callAs(t, services, acmeCtx, keystore, "SignCSR",
				&xkmsv1.SignCSRRequest{CsrPem: newCSR(t, "alice"), Profile: "client"})
			require.NoError(t, err)
			got, ok := resp.(*xkmsv1.SignCSRResponse)
			require.True(t, ok, "got %T", resp)
			return got.CertificatePem
		}},
		{name: "IssueCertificate", wantKind: qrdbspiffe.KindService, issue: func(t *testing.T) []byte {
			resp, err := callAs(t, services, acmeCtx, keystore, "IssueCertificate",
				&xkmsv1.IssueCertificateRequest{CommonName: "acme-svc", Profile: "server"})
			require.NoError(t, err)
			got, ok := resp.(*xkmsv1.IssueCertificateResponse)
			require.True(t, ok, "got %T", resp)
			return got.CertificatePem
		}},
	}
	for _, is := range issuers {
		t.Run(is.name, func(t *testing.T) {
			leaf := parseLeaf(t, is.issue(t))
			id, err := qrdbspiffe.TenantFromCert(leaf, "example.com")
			require.NoError(t, err)
			require.Equal(t, "acme", id.TenantID)
			require.Equal(t, is.wantKind, id.Kind)
			owner, err := svc.CA().CertificateTenant(context.Background(), leaf.SerialNumber)
			require.NoError(t, err)
			require.Equal(t, "acme", owner)
		})
	}
}

// TestKeystoreCA_NoTrustDomainOmitsSPIFFE: with no trust domain configured, a
// tenant's certificate over the TCP KeystoreService has no SPIFFE URI SAN.
func TestKeystoreCA_NoTrustDomainOmitsSPIFFE(t *testing.T) {
	services := newKeystoreHarness(t, &Server{config: &config.Config{}})
	acmeCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "acme-user", TenantID: "acme"})
	resp, err := callAs(t, services, acmeCtx, "xkms.v1.KeystoreService", "SignCSR",
		&xkmsv1.SignCSRRequest{CsrPem: newCSR(t, "alice"), Profile: "client"})
	require.NoError(t, err)
	got, ok := resp.(*xkmsv1.SignCSRResponse)
	require.True(t, ok, "got %T", resp)
	requireNoSPIFFEURI(t, parseLeaf(t, got.CertificatePem))
}

// TestCallerTenantCA_SystemCallerGetsNoTenantSAN: a system caller keeps
// platform scope, so even with a trust domain set its certificates carry no
// tenant SPIFFE SAN.
func TestCallerTenantCA_SystemCallerGetsNoTenantSAN(t *testing.T) {
	raw := newRealSoftwareCA(t)
	inner, ok := raw.(provider.CA)
	require.True(t, ok)
	wrapped := withCallerTenant(inner, raw, "example.com")
	systemCtx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "platform-admin", TenantID: qrdbsdk.SystemTenantID})

	leaf, err := wrapped.SignCSRWithSPIFFE(systemCtx, newCSR(t, "platform-admin"), "client", 0, "", "", "", "")
	require.NoError(t, err)
	requireNoSPIFFEURI(t, leaf)

	certPEM, _, _, _, err := wrapped.IssueCertificateRaw(systemCtx, "platform-svc", "", nil, 0, "server", "", "")
	require.NoError(t, err)
	requireNoSPIFFEURI(t, parseLeaf(t, certPEM))
}
