//go:build !integration

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
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// testAnchorCA is a throwaway CA that stands in for an operator's
// security.tls_ca_file anchor, or for an unlisted CA, and issues client certs.
type testAnchorCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newTestAnchorCA(t *testing.T, name string) *testAnchorCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &testAnchorCA{cert: cert, key: key}
}

// writePEM writes the CA certificate to a fresh temp file and returns its path.
func (ca *testAnchorCA) writePEM(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "operator-ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// issueClient issues a TLS client certificate with the given CommonName.
func (ca *testAnchorCA) issueClient(t *testing.T, commonName string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

func newTrustAnchorTestServer(t *testing.T, tlsCAFile string) *Server {
	t.Helper()
	s := newTestServer(t)
	s.config = &config.Config{}
	s.config.Storage.DataDir = t.TempDir()
	s.config.Security.TLSCAFile = tlsCAFile
	return s
}

func TestLoadOperatorClientTrustAnchors_LoadsOperatorFile(t *testing.T) {
	ca := newTestAnchorCA(t, "operator CA")
	s := newTrustAnchorTestServer(t, ca.writePEM(t))

	require.NoError(t, s.loadOperatorClientTrustAnchors())

	pool := s.clientTrustAnchors.certPool()
	require.NotNil(t, pool)
	client := ca.issueClient(t, "agent")
	_, err := client.Leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	require.NoError(t, err, "a client cert from the operator CA must verify against the shared pool")
}

func TestLoadOperatorClientTrustAnchors_MissingFileIsBootError(t *testing.T) {
	s := newTrustAnchorTestServer(t, filepath.Join(t.TempDir(), "absent.pem"))

	err := s.loadOperatorClientTrustAnchors()

	var anchorsErr *ClientTrustAnchorsError
	require.ErrorAs(t, err, &anchorsErr)
	var readErr *CertPoolReadError
	require.ErrorAs(t, err, &readErr)
	require.Nil(t, s.clientTrustAnchors)
}

func TestLoadOperatorClientTrustAnchors_EmptyFileIsBootError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.pem")
	require.NoError(t, os.WriteFile(path, []byte("# no certificates\n"), 0o600))
	s := newTrustAnchorTestServer(t, path)

	err := s.loadOperatorClientTrustAnchors()

	var anchorsErr *ClientTrustAnchorsError
	require.ErrorAs(t, err, &anchorsErr)
	var emptyErr *CertPoolEmptyError
	require.ErrorAs(t, err, &emptyErr)
	require.Nil(t, s.clientTrustAnchors)
}

func TestLoadOperatorClientTrustAnchors_DefaultPathIsNotAnAnchorSet(t *testing.T) {
	s := newTrustAnchorTestServer(t, "")
	// The server's own chain from an earlier boot sits at the default path.
	defaultPath := s.defaultTLSCAFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(defaultPath), 0o700))
	ca := newTestAnchorCA(t, "own CA")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
	require.NoError(t, os.WriteFile(defaultPath, data, 0o600))
	s.config.Security.TLSCAFile = defaultPath

	require.NoError(t, s.loadOperatorClientTrustAnchors())
	require.Nil(t, s.clientTrustAnchors, "the defaulted path must not load as operator anchors")
}

func TestLoadOperatorClientTrustAnchors_EmptyPathLeavesSetNil(t *testing.T) {
	s := newTrustAnchorTestServer(t, "")

	require.NoError(t, s.loadOperatorClientTrustAnchors())
	require.Nil(t, s.clientTrustAnchors.certPool())
}

func TestClientTrustAnchorsError_ErrorAndUnwrap(t *testing.T) {
	cause := &CertPoolEmptyError{Path: "ca.pem"}
	err := &ClientTrustAnchorsError{Path: "ca.pem", Cause: cause}

	require.Equal(t, "security.tls_ca_file ca.pem: "+cause.Error(), err.Error())
	require.True(t, errors.Is(err, cause))
}

// The RFC 8705 tls_client_auth authenticator trusts the shared pool: a client
// cert from the tls_ca_file CA authenticates, one from an unlisted CA does not.
func TestTLSClientAuth_TrustsTLSCAFile(t *testing.T) {
	operator := newTestAnchorCA(t, "operator CA")
	unlisted := newTestAnchorCA(t, "unlisted CA")
	s := newTrustAnchorTestServer(t, operator.writePEM(t))
	require.NoError(t, s.loadOperatorClientTrustAnchors())

	ctx := context.Background()
	storage := iam.NewMemoryStorage()
	accepted := operator.issueClient(t, "fleet-agent")
	app := &iam.Application{
		ClientID:                "app_fleet",
		TenantID:                "tenant1",
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		TokenEndpointAuthMethod: iam.AuthMethodTLSClientAuth,
		MTLSSubjectDN:           accepted.Leaf.Subject.String(),
	}
	require.NoError(t, storage.CreateApp(ctx, app))
	auth := iam.NewPKIClientAuth(s.clientTrustAnchors.certPool(), storage)

	got, err := auth.AuthenticateClient(ctx, accepted.Leaf, app.ClientID)
	require.NoError(t, err)
	require.Equal(t, app.ClientID, got.ClientID)

	refused := unlisted.issueClient(t, "fleet-agent")
	_, err = auth.AuthenticateClient(ctx, refused.Leaf, app.ClientID)
	var verifyErr *iam.MTLSCertVerificationError
	require.ErrorAs(t, err, &verifyErr, "a client cert from an unlisted CA must be refused")
}
