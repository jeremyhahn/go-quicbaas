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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newServerUnixMTLSCert returns a self-signed ECDSA P-256 client certificate
// with the given common name.
func newServerUnixMTLSCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate key")
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err, "create certificate")
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err, "parse certificate")
	return cert
}

// serverUnixPeerContext returns a context whose gRPC peer carries the given
// TLS connection state, as the Unix-socket listener presents it.
func serverUnixPeerContext(state tls.ConnectionState) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: state},
	})
}

// TestGRPCAuthorizationInterceptors_UnixVerifiedAdminCertificate drives the
// production gate construction (grpcAuthorizationInterceptors, backed by the
// platform-token Validator) as the Unix listener runs it, with no test-only
// certificate authorizer. A caller that presents a CA-verified administrator
// certificate and no bearer token must be authenticated, as the node's TCP
// listener authenticates it; the first subtest fails until the gate does so.
// The other subtests pin the fail-closed baseline.
func TestGRPCAuthorizationInterceptors_UnixVerifiedAdminCertificate(t *testing.T) {
	provider := newTestOIDCProvider(t)
	s := &Server{
		logger:          newGRPCAuthzTestLogger(t),
		config:          &config.Config{Security: config.SecurityConfig{RequireAuthz: true}},
		quicBaaSService: &grpcOIDCGetterService{provider: provider},
	}
	unary, _, err := s.grpcAuthorizationInterceptors()
	require.NoError(t, err, "gate construction succeeds with a live provider")
	require.NotNil(t, unary, "unary interceptor installed with a live provider")

	const method = "/qrdb.v1.QRDBService/GetUserByUsername"
	info := &ggrpc.UnaryServerInfo{FullMethod: method}
	admin := newServerUnixMTLSCert(t, "admin")

	t.Run("verified administrator certificate is authenticated", func(t *testing.T) {
		called := false
		handler := func(context.Context, any) (any, error) {
			called = true
			return "ok", nil
		}
		ctx := serverUnixPeerContext(tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{admin},
			VerifiedChains:   [][]*x509.Certificate{{admin}},
		})
		_, err := unary(ctx, nil, info, handler)
		assert.NotEqual(t, codes.Unauthenticated, status.Code(err),
			"production gate refused a CA-verified administrator certificate with no bearer: %v", err)
		t.Logf("handler called: %v", called)
	})

	t.Run("no certificate and no bearer is refused", func(t *testing.T) {
		called := false
		handler := func(context.Context, any) (any, error) {
			called = true
			return "ok", nil
		}
		_, err := unary(context.Background(), nil, info, handler)
		assert.Equal(t, codes.Unauthenticated, status.Code(err), "missing credentials must be refused: %v", err)
		assert.False(t, called, "handler must not run without credentials")
	})

	t.Run("certificate without a verified chain is refused", func(t *testing.T) {
		called := false
		handler := func(context.Context, any) (any, error) {
			called = true
			return "ok", nil
		}
		ctx := serverUnixPeerContext(tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{admin},
		})
		_, err := unary(ctx, nil, info, handler)
		assert.Equal(t, codes.Unauthenticated, status.Code(err), "an unverified certificate must be refused: %v", err)
		assert.False(t, called, "handler must not run for an unverified certificate")
	})
}
