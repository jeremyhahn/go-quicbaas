//go:build rest

package rest

// HTTP/3 interop leg (go-quicbaas#203).
//
// In production this router is mounted on go-qrdb's unified REST server,
// whose WithTLS serves HTTP/2 over TCP and HTTP/3 over QUIC on the same port
// and advertises the latter in Alt-Svc. That listener is go-qrdb's and is
// tested there. What this repository owns is the router, so this leg serves
// the full router over HTTP/3 with the same posture (TLS 1.3, ALPN "h3",
// RFC 9114 §3.1) and drives it with quic-go's own HTTP/3 client, never our
// SDK or RESTClient.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2/clientcredentials"
)

// http3InteropALPN is the HTTP/3 ALPN token (RFC 9114 §3.1).
const http3InteropALPN = "h3"

// quicNoApplicationProtocol is the QUIC CRYPTO_ERROR carrying the TLS
// no_application_protocol alert (RFC 9001 §4.8: 0x0100 + alert 120).
const quicNoApplicationProtocol = quic.TransportErrorCode(0x0100 + 120)

// http3InteropFixture is the OIDC interop fixture served over HTTP/3, plus
// the address and trust a raw QUIC client needs to dial it.
type http3InteropFixture struct {
	*interopFixture
	addr  string
	roots *x509.CertPool
}

// newHTTP3InteropFixture serves the full REST router on a loopback UDP
// socket with quic-go's HTTP/3 server, and gives the fixture an http.Client
// whose RoundTripper is quic-go's HTTP/3 transport.
func newHTTP3InteropFixture(t *testing.T) *http3InteropFixture {
	t.Helper()

	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := packetConn.LocalAddr().String()
	issuer := "https://" + addr
	router, provider, user := newInteropRouter(t, issuer)

	cert, roots := newHTTP3InteropCertificate(t)
	server := &http3.Server{
		Handler: router,
		TLSConfig: http3.ConfigureTLSConfig(&tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{cert},
		}),
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(packetConn) }()

	transport := &http3.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
	}}
	t.Cleanup(func() {
		require.NoError(t, transport.Close())
		require.NoError(t, server.Close())
		assert.ErrorIs(t, <-served, http.ErrServerClosed)
		require.NoError(t, packetConn.Close())
	})

	f := &interopFixture{
		issuer:   issuer,
		client:   &http.Client{Transport: transport},
		provider: provider,
		user:     user,
	}
	f.discovery = fetchInteropDiscovery(t, f)
	return &http3InteropFixture{interopFixture: f, addr: addr, roots: roots}
}

// newHTTP3InteropCertificate issues a self-signed loopback server
// certificate and returns it with a pool that trusts it.
func newHTTP3InteropCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "http3-interop"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(cryptorand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, roots
}

// TestHTTP3Interop_DiscoveryIsServedOverHTTP3 is the leg's baseline: the
// fixture already fetched the discovery document through the HTTP/3
// transport; this pins that the exchange really was HTTP/3.
func TestHTTP3Interop_DiscoveryIsServedOverHTTP3(t *testing.T) {
	f := newHTTP3InteropFixture(t)

	resp, err := f.client.Get(f.issuer + "/.well-known/openid-configuration")
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 3, resp.ProtoMajor, "response protocol %q is not HTTP/3", resp.Proto)
	assert.True(t, strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json"),
		"discovery content type %q", resp.Header.Get("Content-Type"))
}

// TestHTTP3Interop_ClientCredentialsAndIntrospection runs an authenticated
// REST operation over HTTP/3: x/oauth2 obtains a token through quic-go's
// transport, and the client-authenticated introspection endpoint answers it.
func TestHTTP3Interop_ClientCredentialsAndIntrospection(t *testing.T) {
	f := newHTTP3InteropFixture(t)

	cc := &clientcredentials.Config{
		ClientID:     interopClientID,
		ClientSecret: interopSecret,
		TokenURL:     f.discovery.TokenEndpoint,
	}
	token, err := cc.Token(f.context())
	require.NoError(t, err, "x/oauth2 client credentials grant over HTTP/3")
	require.NotEmpty(t, token.AccessToken)

	body := f.introspect(t, token.AccessToken)
	assert.Equal(t, true, body["active"])
	assert.Equal(t, interopClientID, body["client_id"])

	inactive := f.introspect(t, token.AccessToken+"tampered")
	assert.Equal(t, false, inactive["active"])
}

// TestHTTP3Interop_ALPNNegotiation dials the listener with a raw QUIC
// client. Only "h3" is served; a client offering HTTP/2 or HTTP/1.1 alone
// must be refused in the handshake with no_application_protocol.
func TestHTTP3Interop_ALPNNegotiation(t *testing.T) {
	f := newHTTP3InteropFixture(t)

	tests := []struct {
		name     string
		protos   []string
		accepted bool
	}{
		{name: "h3 is negotiated", protos: []string{http3InteropALPN}, accepted: true},
		{name: "h2 only is refused", protos: []string{"h2"}},
		{name: "http/1.1 only is refused", protos: []string{"http/1.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := quic.DialAddr(t.Context(), f.addr, &tls.Config{
				MinVersion: tls.VersionTLS13,
				RootCAs:    f.roots,
				NextProtos: tt.protos,
			}, nil)
			if !tt.accepted {
				var transportErr *quic.TransportError
				require.True(t, errors.As(err, &transportErr), "want a QUIC transport error, got %v", err)
				assert.Equal(t, quicNoApplicationProtocol, transportErr.ErrorCode)
				return
			}
			require.NoError(t, err)
			defer func() { require.NoError(t, conn.CloseWithError(0, "")) }()
			assert.Equal(t, http3InteropALPN, conn.ConnectionState().TLS.NegotiatedProtocol)
		})
	}
}
