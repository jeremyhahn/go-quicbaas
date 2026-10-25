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

package apimodule

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// fixtureTrustDomain is the SPIFFE trust domain the integration fixture CA
// issues under (test/integration/certfixture/topology.json).
const fixtureTrustDomain = "quicbaas.integration"

// fixtureAdminSPIFFE is the SAN the fixture's admin client certificate
// carries: tenant "system" (the trust domain), user admin.
const fixtureAdminSPIFFE = "spiffe://system/user/admin"

// fixtureAdminTenant is the first Subject OU the admin certificate pairs with
// that SAN; go-qrdb names a tenant only when the two agree.
const fixtureAdminTenant = "system"

// testCA signs the server and client certificates of one mTLS test.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "QuicBaaS Integration CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

// issue signs a leaf for tmpl and returns it as a TLS certificate.
func (ca *testCA) issue(t *testing.T, tmpl *x509.Certificate) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	require.NoError(t, err)
	tmpl.SerialNumber = serial
	tmpl.NotBefore = time.Now().Add(-time.Hour)
	tmpl.NotAfter = time.Now().Add(time.Hour)
	tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// clientCert issues a client certificate shaped like the fixture's admin
// certificate, naming spiffeURI as its SPIFFE SAN and ou as its first OU.
func (ca *testCA) clientCert(t *testing.T, spiffeURI, ou string) tls.Certificate {
	t.Helper()
	uri, err := url.Parse(spiffeURI)
	require.NoError(t, err)
	return ca.issue(t, &x509.Certificate{
		Subject:        pkix.Name{CommonName: "admin@integration.test", OrganizationalUnit: []string{ou}},
		EmailAddresses: []string{"admin@integration.test"},
		URIs:           []*url.URL{uri},
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
}

// startMTLSModule serves the module over TLS on bufconn the way the unified
// server serves it when the embedded node runs with auth disabled
// (security.require_authz: false): the server verifies a client certificate
// if one is given and runs extra, the interceptors of the node's chain. With
// auth disabled go-qrdb installs no auth interceptor at all. client is the
// certificate the caller presents; nil presents none.
func startMTLSModule(t *testing.T, m *Module, ca *testCA, client *tls.Certificate, extra ...grpc.UnaryServerInterceptor) *grpc.ClientConn {
	t.Helper()
	serverCert := ca.issue(t, &x509.Certificate{
		Subject:     pkix.Name{CommonName: "test-node1"},
		DNSNames:    []string{"bufconn"},
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	serverTLS := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    ca.pool,
		ClientAuth:   tls.VerifyClientCertIfGiven,
	}
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(serverTLS)), grpc.ChainUnaryInterceptor(extra...))
	m.RegisterGRPC(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	clientTLS := &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    ca.pool,
		ServerName: "bufconn",
	}
	if client != nil {
		clientTLS.Certificates = []tls.Certificate{*client}
	}
	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(credentials.NewTLS(clientTLS)),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// schedulerModule builds a module whose scheduler holds one system job and
// one tenant-b job, configured with the fixture's trust domain.
func schedulerModule(t *testing.T) (m *Module, systemJob, tenantBJob uint64) {
	t.Helper()
	sched, systemJob, tenantBJob := seededScheduler(t)
	m, err := NewModule(func() service.QuicBaaS {
		return &mocks.MockQuicBaaS{SchedulerServiceFunc: func() service.SchedulerService { return sched }}
	}, testLogger(t), WithSPIFFETrustDomain(fixtureTrustDomain))
	require.NoError(t, err)
	return m, systemJob, tenantBJob
}

// The integration failure of go-quicbaas#1699: an admin-certificate gRPC
// call on the unified server with auth disabled reached the scheduler with no
// tenant bound and was refused. The verified certificate names tenant
// "system", so the call must reach the service scoped to that tenant.
func TestModule_UnifiedGRPC_FixtureAdminCertBindsItsTenant(t *testing.T) {
	m, systemJob, tenantBJob := schedulerModule(t)
	ca := newTestCA(t)
	admin := ca.clientCert(t, fixtureAdminSPIFFE, fixtureAdminTenant)
	client := pb.NewSchedulerServiceClient(startMTLSModule(t, m, ca, &admin))
	ctx := context.Background()

	resp, err := client.GetJob(ctx, &pb.GetJobRequest{JobId: systemJob})
	require.NoError(t, err)
	assert.Equal(t, systemJob, resp.GetJob().GetId())

	_, err = client.GetJob(ctx, &pb.GetJobRequest{JobId: tenantBJob, TenantId: "tenant-b"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// A caller presenting no certificate has no tenant to bind. It is refused;
// it is never served in the system scope.
func TestModule_UnifiedGRPC_CertlessCallIsRefused(t *testing.T) {
	m, systemJob, _ := schedulerModule(t)
	client := pb.NewSchedulerServiceClient(startMTLSModule(t, m, newTestCA(t), nil))

	_, err := client.GetJob(context.Background(), &pb.GetJobRequest{JobId: systemJob})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "no tenant is bound to the request")
}

// A verified certificate whose SPIFFE SAN names a trust domain other than the
// tenant its OU names is refused by qrdb's rule; the call never reaches the service.
func TestModule_UnifiedGRPC_ForeignTrustDomainCertIsRefused(t *testing.T) {
	m, systemJob, _ := schedulerModule(t)
	ca := newTestCA(t)
	foreign := ca.clientCert(t, "spiffe://other.example/user/admin", fixtureAdminTenant)
	client := pb.NewSchedulerServiceClient(startMTLSModule(t, m, ca, &foreign))

	_, err := client.GetJob(context.Background(), &pb.GetJobRequest{JobId: systemJob})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

// When go-qrdb's auth chain has already bound the caller's tenant, the
// certificate's tenant does not replace it.
func TestModule_UnifiedGRPC_AuthenticatedTenantIsNotReplacedByCert(t *testing.T) {
	m, systemJob, tenantBJob := schedulerModule(t)
	ca := newTestCA(t)
	admin := ca.clientCert(t, fixtureAdminSPIFFE, fixtureAdminTenant)
	client := pb.NewSchedulerServiceClient(startMTLSModule(t, m, ca, &admin, qrdbAuthenticated("tenant-b")))
	ctx := context.Background()

	resp, err := client.GetJob(ctx, &pb.GetJobRequest{JobId: tenantBJob, TenantId: "tenant-b"})
	require.NoError(t, err)
	assert.Equal(t, tenantBJob, resp.GetJob().GetId())

	_, err = client.GetJob(ctx, &pb.GetJobRequest{JobId: systemJob})
	require.Error(t, err)
}

// Without a pinned trust domain the module binds nothing from a certificate:
// qrdb reads an empty pin as "accept a SPIFFE SAN from any trust domain".
func TestModule_UnifiedGRPC_UnpinnedModuleBindsNoCertTenant(t *testing.T) {
	sched, systemJob, _ := seededScheduler(t)
	m, err := NewModule(func() service.QuicBaaS {
		return &mocks.MockQuicBaaS{SchedulerServiceFunc: func() service.SchedulerService { return sched }}
	}, testLogger(t))
	require.NoError(t, err)
	ca := newTestCA(t)
	admin := ca.clientCert(t, fixtureAdminSPIFFE, fixtureAdminTenant)
	client := pb.NewSchedulerServiceClient(startMTLSModule(t, m, ca, &admin))

	_, err = client.GetJob(context.Background(), &pb.GetJobRequest{JobId: systemJob})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestPeerCertificateTenantError_IsUnauthenticatedAndUnwraps(t *testing.T) {
	err := &PeerCertificateTenantError{Cause: qrdbsdk.ErrInvalidClientCertificate}

	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.ErrorIs(t, err, qrdbsdk.ErrInvalidClientCertificate)
	assert.Contains(t, err.Error(), "client certificate names no acceptable tenant")
}
