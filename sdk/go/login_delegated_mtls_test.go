package quicbaas

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// newLoginIdentityClient builds an embedded client over the real
// LoginService, running as identity, and returns the qrdb login client the
// service forwards to.
func newLoginIdentityClient(t *testing.T, identity EmbeddedIdentity) (Client, *peerCertLoginClient) {
	t.Helper()
	node := &peerCertLoginClient{}
	c, err := NewClient("embedded",
		WithEmbedded(&peerCertQuicBaaS{login: service.NewLoginService(node)}),
		WithEmbeddedIdentity(identity))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, node
}

// mtlsClaim is an mTLS FinishLogin that names another user's lookup key and
// carries no proof of possession.
func mtlsClaim() FinishLoginRequest {
	return FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		SessionID: "sess",
		LookupKey: []byte("victim-spki-sha256"),
	}
}

// TestFinishLogin_DelegatedMTLSClaimNeedsPossession proves a tenant delegation
// is not the in-process trust boundary: its mTLS FinishLogin carries no
// verified peer certificate, so the caller-supplied lookup key is refused
// before the node can mint another user's session.
func TestFinishLogin_DelegatedMTLSClaimNeedsPossession(t *testing.T) {
	c, node := newLoginIdentityClient(t,
		DelegatedIdentity(&recordingBinder{}, []*x509.Certificate{{}}))

	_, err := c.Security().Auth().FinishLogin(context.Background(), mtlsClaim())
	var needCert *service.MTLSPeerCertRequiredError
	if !stderrors.As(err, &needCert) {
		t.Fatalf("FinishLogin = %v, want MTLSPeerCertRequiredError", err)
	}
	if node.finishInvoked {
		t.Fatal("a delegated mTLS claim reached the node without possession proof")
	}
}

// TestFinishLogin_ServicePrincipalMTLSClaimIsTrusted pins the in-process
// trust the embedding service itself keeps: it runs inside the daemon, so its
// supplied lookup key is forwarded.
func TestFinishLogin_ServicePrincipalMTLSClaimIsTrusted(t *testing.T) {
	c, node := newLoginIdentityClient(t, ServicePrincipalIdentity(&recordingBinder{}))

	if _, err := c.Security().Auth().FinishLogin(context.Background(), mtlsClaim()); err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if !node.finishInvoked || string(node.finishReq.LookupKey) != "victim-spki-sha256" {
		t.Fatalf("node reached=%v with key %q, want the supplied key", node.finishInvoked, node.finishReq.LookupKey)
	}
}
