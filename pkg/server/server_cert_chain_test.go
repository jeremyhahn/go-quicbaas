package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// chainTestCA is a CA certificate and the key that signs under it.
type chainTestCA struct {
	cert *x509.Certificate
	key  crypto.Signer
}

// newChainTestCA returns a CA named cn, signed by parent, or self-signed
// when parent is nil.
func newChainTestCA(t *testing.T, cn string, parent *chainTestCA) *chainTestCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	issuerCert, issuerKey := tmpl, crypto.Signer(key)
	if parent != nil {
		issuerCert, issuerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, issuerCert, &key.PublicKey, issuerKey)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &chainTestCA{cert: cert, key: key}
}

// newChainedSANCAMock returns a sanCAMock that issues as intermediate B of
// root -> intermediate A -> intermediate B. Like a go-xkms intermediate CA,
// its CACertificate is B and its CABundle is B then its parent A: the root
// sits above anything the issuing CA holds.
func newChainedSANCAMock(t *testing.T) (*sanCAMock, *chainTestCA) {
	root := newChainTestCA(t, "Chain Root CA", nil)
	a := newChainTestCA(t, "Chain Intermediate A", root)
	b := newChainTestCA(t, "Chain Intermediate B", a)
	ca := newSANCAMock(t)
	ca.issuer = b
	ca.caCert = b.cert
	ca.bundle = []*x509.Certificate{b.cert, a.cert}
	return ca, root
}

// verifyServedChain verifies the chain ServerTLSConfig serves for cn
// against root alone, as a client trusting only the root would.
func verifyServedChain(t *testing.T, ca *sanCAMock, cn string, root *x509.Certificate) {
	t.Helper()
	tlsConfig, err := ca.ServerTLSConfig(context.Background(), &xkmssdk.KeyAttributes{CN: cn}, false)
	require.NoError(t, err)
	served := tlsConfig.Certificates[0].Certificate
	intermediates := x509.NewCertPool()
	for _, der := range served[1:] {
		cert, err := x509.ParseCertificate(der)
		require.NoError(t, err)
		intermediates.AddCert(cert)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	leaf := tlsConfig.Certificates[0].Leaf
	_, err = leaf.Verify(x509.VerifyOptions{DNSName: cn, Roots: roots, Intermediates: intermediates})
	require.NoError(t, err, "served chain has %d certificates", len(served))
}

// The served chain carries every issuer between the leaf and the root, on
// the first boot and on a restart that keeps the certificate.
func TestIssueServerCert_ServedChainKeepsEveryIssuer(t *testing.T) {
	ca, root := newChainedSANCAMock(t)
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	s := newSANTestServer(t)

	_, err := s.issueServerCert(context.Background(), ca, sanRequest("localhost", sans))
	require.NoError(t, err)
	verifyServedChain(t, ca, "localhost", root.cert)

	_, err = s.issueServerCert(context.Background(), ca, sanRequest("localhost", sans))
	require.NoError(t, err)
	verifyServedChain(t, ca, "localhost", root.cert)
	assert.Equal(t, 1, ca.issued)
}

// A restart whose CA reports fewer issuers than the stored chain carries
// never replaces that complete chain with the shorter one.
func TestIssueServerCert_RestartNeverShortensStoredChain(t *testing.T) {
	ca, root := newChainedSANCAMock(t)
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	s := newSANTestServer(t)
	_, err := s.issueServerCert(context.Background(), ca, sanRequest("localhost", sans))
	require.NoError(t, err)
	complete := ca.keys.chains["localhost"]

	ca.bundle = []*x509.Certificate{ca.caCert}
	_, err = s.issueServerCert(context.Background(), ca, sanRequest("localhost", sans))
	require.NoError(t, err)

	assert.Equal(t, complete, ca.keys.chains["localhost"])
	verifyServedChain(t, ca, "localhost", root.cert)
}

// A stored chain from a different issuer is stale, so a restart replaces
// it even though it is longer.
func TestIssueServerCert_RestartReplacesStaleLongerChain(t *testing.T) {
	ca, root := newChainedSANCAMock(t)
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}
	s := newSANTestServer(t)
	_, err := s.issueServerCert(context.Background(), ca, sanRequest("localhost", sans))
	require.NoError(t, err)
	leaf := ca.keys.chains["localhost"][0]
	other := newChainTestCA(t, "Other CA", nil)
	ca.keys.chains["localhost"] = []*x509.Certificate{leaf, other.cert, other.cert, other.cert}

	_, err = s.issueServerCert(context.Background(), ca, sanRequest("localhost", sans))
	require.NoError(t, err)

	assert.Equal(t, []*x509.Certificate{leaf, ca.bundle[0], ca.bundle[1]}, ca.keys.chains["localhost"])
	verifyServedChain(t, ca, "localhost", root.cert)
}

// A CA bundle that cannot be read stops the chain before the keystore.
func TestIssueServerCert_CABundleFailureStoresNothing(t *testing.T) {
	ca, _ := newChainedSANCAMock(t)
	ca.bundleErr = errSANTestCA
	sans := &xkmssdk.TrustedCASubjectAlternativeNames{DNS: []string{"localhost"}}

	_, err := newSANTestServer(t).issueServerCert(context.Background(), ca, sanRequest("localhost", sans))

	require.ErrorIs(t, err, errSANTestCA)
	assert.Empty(t, ca.keys.writes)
}
