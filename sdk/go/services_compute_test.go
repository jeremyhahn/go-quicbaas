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

package quicbaas

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// registryRecorder answers GET /v2/ with 200 and every other path with 404,
// recording the paths and peer certificate subjects it saw.
type registryRecorder struct {
	mu       sync.Mutex
	paths    []string
	subjects [][]string
}

func (r *registryRecorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.paths = append(r.paths, req.URL.Path)
	if req.TLS != nil {
		var subs []string
		for _, cert := range req.TLS.PeerCertificates {
			subs = append(subs, cert.Subject.CommonName)
		}
		r.subjects = append(r.subjects, subs)
	}
	r.mu.Unlock()
	if req.Method == http.MethodGet && req.URL.Path == "/v2/" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.NotFound(w, req)
}

func (r *registryRecorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...)
}

func (r *registryRecorder) peerSubjects() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.subjects...)
}

func trustServer(srv *httptest.Server) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

func TestCompute_Images_REST(t *testing.T) {
	rec := &registryRecorder{}
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL+"/api", WithTLSConfig(trustServer(srv)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ic, err := c.Compute().Images()
	require.NoError(t, err)
	require.NotNil(t, ic)
	require.NoError(t, ic.Ping(context.Background()))
	require.Contains(t, rec.seen(), "/v2/")
}

func TestCompute_Images_Cached(t *testing.T) {
	srv := httptest.NewTLSServer(&registryRecorder{})
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithTLSConfig(trustServer(srv)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	first, err := c.Compute().Images()
	require.NoError(t, err)
	second, err := c.Compute().Images()
	require.NoError(t, err)
	require.Same(t, first, second)
}

// newTestClientCert generates an ECDSA P-256 CA and a client leaf it signs.
func newTestClientCert(t *testing.T, cn string) (*x509.CertPool, tls.Certificate) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "compute-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return pool, tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: leafKey}
}

func TestCompute_Images_MutualTLS(t *testing.T) {
	caPool, leaf := newTestClientCert(t, "workload-compute-test")
	rec := &registryRecorder{}
	srv := httptest.NewUnstartedServer(rec)
	srv.TLS = &tls.Config{
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  caPool,
		MinVersion: tls.VersionTLS12,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	cfg := trustServer(srv)
	cfg.Certificates = []tls.Certificate{leaf}
	c, err := NewClient(srv.URL, WithTLSConfig(cfg))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ic, err := c.Compute().Images()
	require.NoError(t, err)
	require.NoError(t, ic.Ping(context.Background()))

	subjects := rec.peerSubjects()
	require.NotEmpty(t, subjects)
	require.Equal(t, []string{"workload-compute-test"}, subjects[0])
}

func TestCompute_Images_WithImagesURL(t *testing.T) {
	srvRec := &registryRecorder{}
	srv := httptest.NewTLSServer(srvRec)
	t.Cleanup(srv.Close)
	otherRec := &registryRecorder{}
	other := httptest.NewTLSServer(otherRec)
	t.Cleanup(other.Close)

	// Both httptest servers share one certificate, so one pool trusts both.
	c, err := NewClient(srv.URL, WithTLSConfig(trustServer(srv)), WithImagesURL(other.URL))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ic, err := c.Compute().Images()
	require.NoError(t, err)
	require.NoError(t, ic.Ping(context.Background()))
	require.Contains(t, otherRec.seen(), "/v2/")
	require.NotContains(t, srvRec.seen(), "/v2/")

	for _, bad := range []string{"", "ftp://x", "/relative", "http://[::1"} {
		_, err := NewClient(srv.URL, WithTLSConfig(trustServer(srv)), WithImagesURL(bad))
		require.Error(t, err, "WithImagesURL(%q)", bad)
		require.True(t, sdkerrors.IsInvalidInput(err), "WithImagesURL(%q): %v", bad, err)
	}
}

func TestCompute_ImagesBaseURL(t *testing.T) {
	withType := func(tt TransportType) *clientOptions {
		o := defaultOptions()
		o.transportType = tt
		return o
	}
	withURL := func(u string) *clientOptions {
		o := defaultOptions()
		o.imagesURL = u
		return o
	}
	ok := []struct {
		name    string
		address string
		opts    *clientOptions
		want    string
	}{
		{"https with path", "https://h:8443/api/v1", withType(TransportREST), "https://h:8443"},
		{"http", "http://127.0.0.1:9", withType(TransportREST), "http://127.0.0.1:9"},
		{"bare grpc", "h:8443", withType(TransportGRPC), "https://h:8443"},
		{"bare quic", "h:8443", withType(TransportQUIC), "https://h:8443"},
		{"bare mcp", "h:8443", withType(TransportMCP), "https://h:8443"},
		{"override", "h:8443", withURL("https://reg:9443/"), "https://reg:9443"},
		{"override unix", "/run/q.sock", func() *clientOptions {
			o := withURL("http://127.0.0.1:5000")
			o.transportType = TransportUnix
			return o
		}(), "http://127.0.0.1:5000"},
		{"override empty address", "", withURL("https://reg:9443"), "https://reg:9443"},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			got, err := imagesBaseURL(tc.address, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	bad := []struct {
		name    string
		address string
		opts    *clientOptions
	}{
		{"unix", "/run/q.sock", withType(TransportUnix)},
		{"embedded", "embedded", withType(TransportEmbedded)},
		{"empty address", "", defaultOptions()},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, err := imagesBaseURL(tc.address, tc.opts)
			require.Error(t, err)
			require.True(t, sdkerrors.IsInvalidInput(err), "%v", err)
			require.Contains(t, err.Error(), "WithImagesURL")
		})
	}
}

func TestCompute_Images_Unsupported(t *testing.T) {
	c := &client{transport: &mockTransport{}, opts: defaultOptions()}

	ic, err := c.Compute().Images()
	require.Nil(t, ic)
	require.Error(t, err)
	require.True(t, sdkerrors.IsInvalidInput(err), "%v", err)

	ic2, err2 := c.Compute().Images()
	require.Nil(t, ic2)
	require.Equal(t, err, err2)
}
