//go:build !integration

package server

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"
)

// delegatingTenantID is the tenant whose login CA issues the delegations.
const delegatingTenantID = "deleg-kms"

// testLoginCA is a tenant's login CA, the CA its delegations are issued under.
type testLoginCA struct {
	cert   *x509.Certificate
	signer crypto.Signer
}

// testLoginCAs hands each tenant its own login CA.
type testLoginCAs map[string]*testLoginCA

func (k testLoginCAs) ProviderForTenant(tenantID string) (qrdbsdk.CAKeyProvider, error) {
	ca, ok := k[tenantID]
	if !ok {
		return nil, &qrdbsdk.CAKeyNotInitializedError{}
	}
	return qrdbsdk.CAKeyAdapter{
		SignerFunc:      func(context.Context) (crypto.Signer, error) { return ca.signer, nil },
		CertificateFunc: func(context.Context) (*x509.Certificate, error) { return ca.cert, nil },
	}, nil
}

func (k testLoginCAs) TenantIDs(context.Context) ([]string, error) {
	ids := make([]string, 0, len(k))
	for id := range k {
		ids = append(ids, id)
	}
	return ids, nil
}

func newTestLoginCA(t *testing.T, cn string) *testLoginCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	c, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &testLoginCA{cert: c, signer: key}
}

// newDelegatingServer boots an authorized node running as the production
// service principal, with one tenant holding its own login CA.
func newDelegatingServer(t *testing.T) *Server {
	t.Helper()
	cas := testLoginCAs{delegatingTenantID: newTestLoginCA(t, delegatingTenantID+"-login-ca")}
	cfg := principalNodeConfig(t)
	// Certificate enrolment, and so IssueDelegation, rides the node barrier.
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "none"
	// A delegated request is never served unaudited; the server's default
	// sink is the same ring buffer.
	audit := []qrdbsdk.ServerOption{qrdbsdk.WithAuditLogger(qrdbsdk.NewAuditRingBuffer(defaultQRDBAuditRingCapacity))}
	s := serverOverNode(startPrincipalNode(t, cfg, audit,
		qrdbnode.WithCertKeyProviderFactory(cas), qrdbnode.WithTenantIsolation()))
	createNodeTenant(t, s, delegatingTenantID, qrdbsdk.TenantIsolationPrefix)
	return s
}

// issueTestDelegation has the tenant's own caller, holding certs:delegate,
// issue the production principal a delegation for scope through the node's
// certificate client. The principal keeps its key.
func issueTestDelegation(t *testing.T, s *Server, scope ...string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "quicbaas"}}, key)
	require.NoError(t, err)
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	caller := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{
		Subject:          delegatingTenantID + "-admin",
		TenantID:         delegatingTenantID,
		ScopePermissions: []string{"certs:delegate"},
	})
	caller = qrdbsdk.ContextWithTenant(caller, delegatingTenantID)
	issued, err := s.getQRDBNode().Client().Certs().IssueDelegation(caller, &qrdbtransport.IssueDelegationRequest{
		Principal: "quicbaas", Scope: scope, CSRPEM: string(csrPEM),
	})
	require.NoError(t, err)
	block, _ := pem.Decode([]byte(issued.CertPEM))
	require.NotNil(t, block, "IssueDelegation returned no PEM certificate")
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return leaf
}

// The SDK client half of these tests lives in package server_test, because
// sdk/go imports this package. These hooks hand it the real node and the
// real issuance.
var (
	NewDelegatingServer = newDelegatingServer
	IssueTestDelegation = issueTestDelegation
	DelegatingTenantID  = delegatingTenantID
)

// QRDBNodeForTest is the server's embedded qrdb node.
func (s *Server) QRDBNodeForTest() *qrdbnode.Node { return s.getQRDBNode() }
