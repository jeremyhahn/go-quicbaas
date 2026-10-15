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

package quic

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/netport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// --- Constructor validation ---

func TestNew_NilConfig(t *testing.T) {
	tr, err := New(nil)
	require.Error(t, err)
	assert.Nil(t, tr)
}

func TestNew_EmptyAddress(t *testing.T) {
	tr, err := New(&transport.Config{})
	require.Error(t, err)
	assert.Nil(t, tr)
}

func TestNew_ValidConfigReturnsTransport(t *testing.T) {
	tr, err := New(&transport.Config{Address: "127.0.0.1:0"})
	require.NoError(t, err)
	require.NotNil(t, tr)
	assert.NoError(t, tr.Close())
}

// --- buildTLSConfig ---

func TestBuildTLSConfig_NilCallerConfigDefaults(t *testing.T) {
	tlsCfg := buildTLSConfig(&transport.Config{Address: "h:1"})
	assert.Equal(t, uint16(tls.VersionTLS13), tlsCfg.MinVersion)
	assert.Equal(t, []string{alpnHTTP3}, tlsCfg.NextProtos)
}

func TestBuildTLSConfig_MinVersionRaisedToTLS13(t *testing.T) {
	caller := &tls.Config{MinVersion: tls.VersionTLS12}
	tlsCfg := buildTLSConfig(&transport.Config{Address: "h:1", TLSConfig: caller})
	assert.Equal(t, uint16(tls.VersionTLS13), tlsCfg.MinVersion)
}

func TestBuildTLSConfig_DoesNotMutateCallerConfig(t *testing.T) {
	caller := &tls.Config{MinVersion: tls.VersionTLS12}
	_ = buildTLSConfig(&transport.Config{Address: "h:1", TLSConfig: caller})
	assert.Equal(t, uint16(tls.VersionTLS12), caller.MinVersion,
		"caller's tls.Config must not be mutated in place")
}

func TestBuildTLSConfig_DefaultsALPNWhenUnset(t *testing.T) {
	caller := &tls.Config{MinVersion: tls.VersionTLS13}
	tlsCfg := buildTLSConfig(&transport.Config{Address: "h:1", TLSConfig: caller})
	assert.Equal(t, []string{alpnHTTP3}, tlsCfg.NextProtos)
}

func TestBuildTLSConfig_PreservesExplicitALPN(t *testing.T) {
	caller := &tls.Config{MinVersion: tls.VersionTLS13, NextProtos: []string{"custom-alpn"}}
	tlsCfg := buildTLSConfig(&transport.Config{Address: "h:1", TLSConfig: caller})
	assert.Equal(t, []string{"custom-alpn"}, tlsCfg.NextProtos)
}

// --- End-to-end over a real loopback HTTP/3 listener ---

// testTLSCert generates a self-signed ECDSA certificate for 127.0.0.1, valid
// for the lifetime of the test process. Used to bring up a real http3.Server
// on loopback so the transport's round-trip behavior (success, error status
// mapping, header propagation) is exercised against real HTTP/3 framing
// rather than mocked out.
func testTLSCert(t *testing.T) tls.Certificate {
	t.Helper()

	cert, err := loopbackCert()
	require.NoError(t, err)
	return cert
}

// loopbackCert is the one self-signed loopback certificate that every test
// server presents and every test client trusts, so the client verifies the
// server's chain the way production does.
var loopbackCert = sync.OnceValues(newLoopbackCert)

func newLoopbackCert() (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:         true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
		Leaf:        cert,
	}, nil
}

// startTestHTTP3Server brings up a real http3.Server on loopback serving
// handler, returning the dialable address and a shutdown func.
func startTestHTTP3Server(t *testing.T, handler http.Handler) (addr string, shutdown func()) {
	t.Helper()

	cert := testTLSCert(t)
	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)

	srv := &http3.Server{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{alpnHTTP3},
		},
		Handler: handler,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(udpConn)
	}()

	return udpConn.LocalAddr().String(), func() {
		_ = srv.Close()
		_ = udpConn.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// testTransportConfig builds a transport.Config that trusts the test
// server's self-signed certificate.
func testTransportConfig(t *testing.T, addr string) *transport.Config {
	t.Helper()

	roots := x509.NewCertPool()
	roots.AddCert(testTLSCert(t).Leaf)
	return &transport.Config{
		Address: addr,
		Timeout: 5 * time.Second,
		TLSConfig: &tls.Config{
			RootCAs: roots,
		},
	}
}

func TestRequest_SuccessOverHTTP3(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "Health", "Check", nil)
	require.NoError(t, err)
	m, ok := resp.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "healthy", m["status"])
}

func TestRequest_PropagatesAuthTokenHeader(t *testing.T) {
	// gotAuth is handed off from the server handler goroutine to the test
	// goroutine over a buffered channel rather than a shared variable: a
	// channel send/receive is a synchronization primitive the race detector
	// recognizes, whereas relying on the HTTP/3 round trip itself to imply
	// ordering does not (quic-go multiplexes the handler and the client's
	// Do() return across several internal goroutines).
	gotAuth := make(chan string, 1)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr), WithAuthToken("test-token"))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "Health", "Check", nil)
	require.NoError(t, err)

	select {
	case got := <-gotAuth:
		assert.Equal(t, "Bearer test-token", got)
	case <-time.After(2 * time.Second):
		t.Fatal("server handler was not invoked")
	}
}

func TestRequest_WithAuthTokenOverridesConfigAuthToken(t *testing.T) {
	gotAuth := make(chan string, 1)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}))
	defer shutdown()

	cfg := testTransportConfig(t, addr)
	cfg.AuthToken = "config-token"
	tr, err := New(cfg, WithAuthToken("option-token"))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "Health", "Check", nil)
	require.NoError(t, err)

	select {
	case got := <-gotAuth:
		assert.Equal(t, "Bearer option-token", got)
	case <-time.After(2 * time.Second):
		t.Fatal("server handler was not invoked")
	}
}

func TestRequest_NonSuccessStatusMapsToError(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	_, err = tr.Request(context.Background(), "Health", "Check", nil)
	require.Error(t, err)
}

func TestRequest_ConnectionRefused(t *testing.T) {
	// Nothing is listening on this loopback port. It is reserved rather than
	// drawn from :0 and released, so nothing else can be handed it before the
	// dial: netport holds a host-wide lock on it and draws only from outside
	// the kernel's ephemeral range.
	addr := netport.MustReserveLoopbackAddr(t)

	cfg := testTransportConfig(t, addr)
	cfg.Timeout = 500 * time.Millisecond
	cfg.MaxRetries = 0
	tr, err := New(cfg)
	require.NoError(t, err)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err = tr.Request(ctx, "Health", "Check", nil)
	require.Error(t, err)
}

func TestRequest_ContextCancellation(t *testing.T) {
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = tr.Request(ctx, "Health", "Check", nil)
	require.Error(t, err)
}

func TestFactory_Create(t *testing.T) {
	f := &Factory{}
	tr, err := f.Create(&transport.Config{Address: "127.0.0.1:0"})
	require.NoError(t, err)
	require.NotNil(t, tr)
	assert.NoError(t, tr.Close())
}
