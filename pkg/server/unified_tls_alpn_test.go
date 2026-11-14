//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package server

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
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	qrdbrest "github.com/jeremyhahn/go-qrdb/sdk/go/transport/rest"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	sdkrest "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// TestUnifiedServerTLS_SDKRESTClientSpeaksTheNegotiatedProtocol serves HTTP the
// way the embedded node's REST listener does: an http.Server over
// tls.NewListener with the unified-server TLS config, which offers h2 and
// http/1.1 through ALPN. The client side is built the way quicbaas.NewClient
// builds a REST client: the go-qrdb sub-client connects first with the same
// *tls.Config, then the SDK REST transport makes its request. The SDK REST
// transport speaks HTTP/1.1 only, so it must negotiate http/1.1; before
// go-quicbaas#1734 it inherited the "h2" the sub-client's HTTP/2 transport
// had added to the shared config, and read the server's HTTP/2 SETTINGS
// frame as a malformed HTTP/1.x response.
func TestUnifiedServerTLS_SDKRESTClientSpeaksTheNegotiatedProtocol(t *testing.T) {
	t.Parallel()

	cert, pool := newLoopbackTLSCert(t)

	cfg := createFTConfig(t)
	cfg.Security.TLSEnabled = true
	cfg.Storage.DataDir = t.TempDir()
	srv, err := NewServer(cfg)
	require.NoError(t, err)
	srv.opts.tlsCertificate = &cert
	require.NoError(t, srv.issueTLSCertFromExternalCA())

	var lastProto atomic.Pointer[string]
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	httpSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			proto := r.Proto
			lastProto.Store(&proto)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = httpSrv.Serve(tls.NewListener(ln, srv.unifiedServerTLSConfig())) }()
	t.Cleanup(func() { _ = httpSrv.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	baseURL := "https://" + ln.Addr().String()
	shared := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13}

	qrdbClient, err := qrdbrest.New(
		qrdbtransport.WithAddress(baseURL),
		qrdbtransport.WithTLSConfig(shared),
		qrdbtransport.WithTimeout(10*time.Second),
	)
	require.NoError(t, err)
	require.NoError(t, qrdbClient.Connect(ctx))
	t.Cleanup(func() { _ = qrdbClient.Close() })
	require.Equal(t, "HTTP/2.0", *lastProto.Load(), "the go-qrdb sub-client negotiates h2")

	client, err := sdkrest.New(&transport.Config{
		Address:   baseURL,
		TLSConfig: shared,
		Timeout:   10 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.Request(ctx, "health", "Check", nil)
	require.NoError(t, err, "the SDK REST client must read the server's response")
	require.Equal(t, "HTTP/1.1", *lastProto.Load())
}

// newLoopbackTLSCert returns a self-signed server certificate for 127.0.0.1
// and a pool that trusts it.
func newLoopbackTLSCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "alpn-loopback"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv, Leaf: leaf}, pool
}
