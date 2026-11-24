//go:build !integration

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmsauth "github.com/jeremyhahn/go-xkms/pkg/auth"
	xkmscaprovider "github.com/jeremyhahn/go-xkms/pkg/ca/provider"
	xkmslib "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// callCtx is capturedGRPCServices.call with the caller's context, so a test
// can drive a handler as an authenticated tenant.
func (c capturedGRPCServices) callCtx(t *testing.T, ctx context.Context, service, method string, req proto.Message) (any, error) {
	t.Helper()
	svc, ok := c[service]
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

// authenticatedTenant is the context the node's auth middleware hands a
// handler for a caller authenticated in tenant: the identity go-xkms checks
// ownership against, and the tenant binding quicbaas reads.
func authenticatedTenant(tenant string) context.Context {
	ctx := xkmsauth.WithIdentity(context.Background(), &xkmsauth.Identity{Subject: "alice", TenantID: tenant})
	return tenantctx.WithTenant(ctx, tenant)
}

// newXKMSCATenantModule builds the xkms service module the way the node
// hosts it, without the PIV login option.
func newXKMSCATenantModule(t *testing.T, srv *Server) qrdbsdk.ServiceModule {
	t.Helper()
	module, err := xkmssdk.NewServiceModule(
		func() *xkmslib.XKMSService {
			svc, getErr := xkmssdk.Get()
			if getErr != nil {
				return nil
			}
			return svc
		},
		xkmssdk.WithServiceModuleQRDBClient(srv.GetQRDBNodeClient),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
	)
	require.NoError(t, err)
	return module
}

// signCanonicalCSR signs a fresh ECDSA P-256 CSR for CN "alice" through the
// module's canonical KeystoreService SignCSR handler under ctx.
func signCanonicalCSR(t *testing.T, ctx context.Context, services capturedGRPCServices) (*x509.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "alice"},
	}, key)
	require.NoError(t, err)
	csr := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})

	out, err := services.callCtx(t, ctx, "xkms.v1.KeystoreService", "SignCSR",
		&xkmsv1.SignCSRRequest{CsrPem: csr, Profile: "client", ValidityDays: 1})
	require.NoError(t, err, "canonical SignCSR must succeed")
	resp, ok := out.(*xkmsv1.SignCSRResponse)
	require.True(t, ok, "SignCSR returns a SignCSRResponse")
	block, _ := pem.Decode(resp.GetCertificatePem())
	require.NotNil(t, block, "SignCSR returns a PEM certificate")
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return leaf, resp.GetSerialNumber()
}

// newXKMSCATenantServices adopts a node CA on a server with the operator
// trust domain set and registers the xkms module's gRPC services.
func newXKMSCATenantServices(t *testing.T) (xkmscaprovider.CA, capturedGRPCServices) {
	t.Helper()
	x := resolveXKMSServiceForCABinding(t)
	ca := newNodeCA(t, x)
	srv := withTempServer(t)
	srv.config.IAM.SPIFFETrustDomain = "example.org"
	srv.adoptCA(ca)

	services := capturedGRPCServices{}
	newXKMSCATenantModule(t, srv).RegisterGRPC(services)
	pc, ok := ca.(xkmscaprovider.CA)
	require.True(t, ok, "the node CA implements the XKMS CA provider")
	return pc, services
}

// A CSR signed through the canonical KeystoreService by a tenant is recorded
// for that tenant and carries its SPIFFE SAN, so that tenant, and no other,
// can revoke it.
func TestXKMSCATenant_CanonicalSignCSRRecordsCallerTenantAndTenantRevokes(t *testing.T) {
	pc, services := newXKMSCATenantServices(t)
	acme := authenticatedTenant("acme")

	leaf, serial := signCanonicalCSR(t, acme, services)

	owner, err := pc.CertificateTenant(acme, leaf.SerialNumber)
	require.NoError(t, err)
	require.Equal(t, "acme", owner, "the certificate is recorded for the signing tenant")

	var spiffe string
	for _, u := range leaf.URIs {
		if strings.HasPrefix(u.String(), "spiffe://example.org/") {
			spiffe = u.String()
		}
	}
	require.NotEmpty(t, spiffe, "the leaf carries a SPIFFE SAN in the operator trust domain")
	require.Contains(t, spiffe, "acme", "the SPIFFE SAN names the signing tenant")

	globex := authenticatedTenant("globex")
	_, err = services.callCtx(t, globex, "xkms.v1.KeystoreService", "RevokeCertificate",
		&xkmsv1.RevokeCertificateRequest{SerialNumber: serial})
	require.Error(t, err, "another tenant must not revoke the certificate")

	_, err = services.callCtx(t, acme, "xkms.v1.KeystoreService", "RevokeCertificate",
		&xkmsv1.RevokeCertificateRequest{SerialNumber: serial})
	require.NoError(t, err, "the signing tenant revokes its own certificate")
}

// A system caller keeps today's behaviour: the certificate is recorded for
// the system scope, which CertificateTenant reports as "".
func TestXKMSCATenant_CanonicalSignCSRAsSystemStaysSystemScoped(t *testing.T) {
	pc, services := newXKMSCATenantServices(t)
	sys := tenantctx.WithSystem(context.Background())

	leaf, _ := signCanonicalCSR(t, sys, services)

	owner, err := pc.CertificateTenant(sys, leaf.SerialNumber)
	require.NoError(t, err)
	require.Equal(t, "", owner, "a system-signed certificate stays in the system scope")
	for _, u := range leaf.URIs {
		require.NotContains(t, u.String(), "/tenant/", "a system-signed certificate carries no tenant SPIFFE SAN")
	}
}
