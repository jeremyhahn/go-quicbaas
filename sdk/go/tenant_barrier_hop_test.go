package quicbaas

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// The tests in this file pin go-quicbaas#2554: TenantMgmt()'s tenant
// barrier verbs ride the SDK's own REST transport, so a secret-carrying
// call refuses every redirect hop and every http:// target, and every call
// carries the platform bearer set by WithAuthToken.

// tlsClientFor builds a client of the given transport that trusts srv.
func tlsClientFor(t *testing.T, srv *httptest.Server, transport Option, extra ...Option) Client {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	opts := append([]Option{
		transport,
		WithTLSConfig(&tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}),
		WithTimeout(5 * time.Second),
	}, extra...)
	c, err := NewClient(strings.TrimPrefix(srv.URL, "https://"), opts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// countingPlainServer is a plain http:// server that counts every request
// except GET /health.
func countingPlainServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			hits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	require.True(t, strings.HasPrefix(srv.URL, "http://"))
	return srv, &hits
}

// redirectingTLSServer answers /health with 200 and redirects a POST to
// path with code to the same path on target.
func redirectingTLSServer(t *testing.T, path string, code int, target string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == path {
			w.Header().Set("Location", target+path)
			w.WriteHeader(code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hopTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestTenantMgmt_TenantBarrierUnseal_RefusesRedirect(t *testing.T) {
	plain, hits := countingPlainServer(t)
	srv := redirectingTLSServer(t, "/v1/tenants/t1/barrier/unseal", http.StatusTemporaryRedirect, plain.URL)
	c := tlsClientFor(t, srv, WithREST())

	tc, err := c.TenantMgmt()
	require.NoError(t, err)
	err = tc.TenantBarrierUnseal(hopTestContext(t), &QRDBTenantBarrierUnsealRequest{TenantID: "t1", Key: []byte("k")})
	require.Error(t, err, "a 307 on a tenant barrier unseal must fail the call")
	assert.Zero(t, hits.Load(), "the unseal key must never follow a redirect to an http:// target")
}

func TestTenantMgmt_TenantBarrierUnsealShare_RefusesRedirect(t *testing.T) {
	plain, hits := countingPlainServer(t)
	srv := redirectingTLSServer(t, "/v1/tenants/t1/barrier/unseal-share", http.StatusPermanentRedirect, plain.URL)
	c := tlsClientFor(t, srv, WithREST())

	tc, err := c.TenantMgmt()
	require.NoError(t, err)
	_, err = tc.TenantBarrierUnsealShare(hopTestContext(t), &QRDBTenantBarrierUnsealShareRequest{TenantID: "t1", Share: "s"})
	require.Error(t, err, "a 308 on a tenant barrier unseal-share must fail the call")
	assert.Zero(t, hits.Load(), "the share must never follow a redirect to an http:// target")
}

func TestTenantMgmt_TenantBarrierSecrets_RefuseHTTPBase(t *testing.T) {
	plain, hits := countingPlainServer(t)
	c, err := NewClient(plain.URL, WithREST(), WithTimeout(2*time.Second))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	tc, err := c.TenantMgmt()
	require.NoError(t, err)
	ctx := hopTestContext(t)

	err = tc.TenantBarrierUnseal(ctx, &QRDBTenantBarrierUnsealRequest{TenantID: "t1", Key: []byte("k")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs https")

	_, err = tc.TenantBarrierUnsealShare(ctx, &QRDBTenantBarrierUnsealShareRequest{TenantID: "t1", Share: "s"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs https")

	assert.Zero(t, hits.Load(), "no tenant barrier secret may be sent to an http:// base")
}

func TestTenantMgmt_QUIC_UsesSDKRESTTenantClient(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c := tlsClientFor(t, srv, WithQUIC())
	tc, err := c.TenantMgmt()
	require.NoError(t, err)
	assert.IsType(t, &rest.TenantClient{}, tc, "a QUIC client's tenant barrier calls must ride the SDK's REST tenant client")

	embedded := tlsClientFor(t, srv, WithQUIC(), WithEmbeddedQRDB(QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
	}))
	etc, err := embedded.TenantMgmt()
	require.NoError(t, err)
	_, isREST := etc.(*rest.TenantClient)
	assert.False(t, isREST, "a client wired WithEmbeddedQRDB keeps the qrdb sub-client")
}

// statusRecorder is a TLS server that answers GET
// /v1/tenants/t1/barrier/status and records, as bools only, whether the
// route was reached and whether it carried the test bearer.
func statusRecorder(t *testing.T) (*httptest.Server, *atomic.Bool, *atomic.Bool, *atomic.Bool) {
	t.Helper()
	var reached, bearer, anyAuth atomic.Bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/tenants/t1/barrier/status" {
			reached.Store(true)
			bearer.Store(r.Header.Get("Authorization") == "Bearer tok-test")
			anyAuth.Store(r.Header.Get("Authorization") != "")
			_, _ = w.Write([]byte(`{"tenant_id":"t1","sealed":true,"initialized":true,"strategy":"software"}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &reached, &bearer, &anyAuth
}

func TestTenantMgmt_TenantBarrierStatus_CarriesBearer(t *testing.T) {
	srv, reached, bearer, _ := statusRecorder(t)
	c := tlsClientFor(t, srv, WithREST(), WithAuthToken("tok-test"))

	tc, err := c.TenantMgmt()
	require.NoError(t, err)
	st, err := tc.TenantBarrierStatus(hopTestContext(t), &QRDBTenantBarrierStatusRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.True(t, st.Sealed)
	assert.True(t, reached.Load(), "the status route must be reached")
	assert.True(t, bearer.Load(), "a tenant barrier call must carry the platform bearer set by WithAuthToken")
}

func TestTenantMgmt_TenantBarrierStatus_NoBearerWithoutAuthToken(t *testing.T) {
	srv, reached, _, anyAuth := statusRecorder(t)
	c := tlsClientFor(t, srv, WithREST())

	tc, err := c.TenantMgmt()
	require.NoError(t, err)
	_, err = tc.TenantBarrierStatus(hopTestContext(t), &QRDBTenantBarrierStatusRequest{TenantID: "t1"})
	require.NoError(t, err)
	assert.True(t, reached.Load(), "the status route must be reached")
	assert.False(t, anyAuth.Load(), "without WithAuthToken no Authorization header is sent")
}
