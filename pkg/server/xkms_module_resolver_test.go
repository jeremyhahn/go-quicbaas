//go:build !integration

package server

import (
	"path/filepath"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-xkms/pkg/api/grpc/proto/xkmsv1"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newResolverKeystoreModule builds the go-xkms module the way the node does,
// with the node's own resolver, and captures its gRPC services.
func newResolverKeystoreModule(t *testing.T, srv *Server) capturedGRPCServices {
	t.Helper()
	module, err := xkmssdk.NewServiceModule(
		srv.xkmsServiceForModule,
		xkmssdk.WithServiceModuleQRDBClient(srv.GetQRDBNodeClient),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
	)
	require.NoError(t, err)
	var _ qrdbsdk.ServiceModule = module
	services := capturedGRPCServices{}
	module.RegisterGRPC(services)
	return services
}

// TestXKMSModuleResolver_KeystoreServesCAAfterLateAdoption covers a keystore
// call that arrives after the node's listeners are up but before the CA is
// adopted. go-xkms caches its keystore service on the first resolve and only
// binds a CA that is already present, so that early call must not resolve;
// once initializeCA has adopted the CA, the canonical CA RPCs serve it.
func TestXKMSModuleResolver_KeystoreServesCAAfterLateAdoption(t *testing.T) {
	x := resolveXKMSServiceForCABinding(t)
	x.SetCA(nil) // at boot the XKMS service has no CA yet

	srv := withTempServer(t)
	srv.config.Security.TLSEnabled = true
	srv.config.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	srv.config.Security.TLSCAFile = filepath.Join(t.TempDir(), "ca-chain.crt")

	services := newResolverKeystoreModule(t, srv)

	_, err := services.call(t, "xkms.v1.KeystoreService", "Health", &xkmsv1.HealthRequest{})
	require.Equal(t, codes.Unavailable, status.Code(err),
		"before CA initialization the keystore must not resolve: %v", err)

	require.NoError(t, srv.initializeCA(), "initializeCA must succeed")
	require.NotNil(t, srv.xkmsCA, "initializeCA must adopt a CA")

	resp, err := services.call(t, "xkms.v1.KeystoreService", "GetCACertificate", &xkmsv1.GetCACertificateRequest{})
	require.NoError(t, err, "GetCACertificate after CA adoption")
	cert, ok := resp.(*xkmsv1.GetCACertificateResponse)
	require.True(t, ok, "GetCACertificate response type %T", resp)
	require.NotEmpty(t, cert.GetCertificatePem(), "the keystore must serve the adopted CA certificate")
}

// TestXKMSModuleResolver_NoCAStillResolvesAfterSettle covers a node with no
// CA: once CA initialization has settled, the keystore surface is served
// without one.
func TestXKMSModuleResolver_NoCAStillResolvesAfterSettle(t *testing.T) {
	x := resolveXKMSServiceForCABinding(t)
	x.SetCA(nil)

	srv := withTempServer(t)
	require.Nil(t, srv.xkmsCA, "this node adopts no CA")
	// Production sets this when initializeCA returns; this node has no CA to
	// initialize, so the test records that boot has passed that point.
	srv.caSettled.Store(true)

	require.Same(t, x, srv.xkmsServiceForModule(), "a settled node without a CA resolves the XKMS service")

	services := newResolverKeystoreModule(t, srv)
	_, err := services.call(t, "xkms.v1.KeystoreService", "Health", &xkmsv1.HealthRequest{})
	require.NoError(t, err, "Health on a settled node without a CA")
}
