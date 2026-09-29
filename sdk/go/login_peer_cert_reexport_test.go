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

package quicbaas

import (
	"bytes"
	"context"
	"errors"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pkgiam "github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
)

// peerCertLoginClient is the node LoginClient behind a real
// service.LoginService; it records the FinishLogin request the facade forwards
// so the test can see which lookup key reached the node.
type peerCertLoginClient struct {
	finishReq     qrdbtransport.FinishLoginRequest
	finishInvoked bool
}

func (f *peerCertLoginClient) ResolveIdentity(context.Context, string) (*qrdbtransport.Identity, error) {
	return nil, errors.New("not implemented")
}

func (f *peerCertLoginClient) BeginLogin(context.Context, qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	return &qrdbtransport.BeginLoginResponse{SessionID: "sess"}, nil
}

func (f *peerCertLoginClient) FinishLogin(_ context.Context, req qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	f.finishInvoked = true
	f.finishReq = req
	return &qrdbtransport.FinishLoginResponse{Bearer: "bearer"}, nil
}

func (f *peerCertLoginClient) Logout(context.Context, string) error { return nil }

// peerCertQuicBaaS is a service.QuicBaaS whose only live method is the
// LoginCeremony facade the embedded login handlers route to.
type peerCertQuicBaaS struct {
	service.QuicBaaS
	login service.LoginServicer
}

func (m *peerCertQuicBaaS) LoginCeremony() service.LoginServicer { return m.login }

// newPeerCertAuthService wires the SDK AuthService over the real embedded
// transport, so FinishLogin runs SDK -> embedded handler -> facade -> node.
func newPeerCertAuthService(t *testing.T) (AuthService, *peerCertLoginClient) {
	t.Helper()
	node := &peerCertLoginClient{}
	tr, err := embedded.New(&peerCertQuicBaaS{login: service.NewLoginService(node)})
	if err != nil {
		t.Fatalf("embedded.New: %v", err)
	}
	return &authServiceImpl{t: tr}, node
}

// TestWithVerifiedPeerCert_EmbeddedMTLSFinishLoginDerivesKeyFromCert is the
// go-trusted-platform case: an embedded caller that holds the verified client
// certificate hands it over through the SDK and sends NO lookup key. The
// facade must derive the SPKI-first key and the legacy cert-DER fallback from
// the certificate — the same derivation the enrollment side writes.
func TestWithVerifiedPeerCert_EmbeddedMTLSFinishLoginDerivesKeyFromCert(t *testing.T) {
	auth, node := newPeerCertAuthService(t)
	cert := makeSelfSignedCert(t)

	ctx := WithVerifiedPeerCert(context.Background(), cert)
	resp, err := auth.FinishLogin(ctx, FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		SessionID: "sess",
	})
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if resp == nil || resp.Bearer != "bearer" {
		t.Fatalf("unexpected response %+v", resp)
	}
	if !node.finishInvoked {
		t.Fatal("login client not reached")
	}
	if want := pkgiam.MTLSSPKICredentialKey(cert); !bytes.Equal(node.finishReq.LookupKey, want) {
		t.Fatalf("LookupKey = %x, want SPKI key %x", node.finishReq.LookupKey, want)
	}
	if len(node.finishReq.AltLookupKeys) != 1 || !bytes.Equal(node.finishReq.AltLookupKeys[0], pkgiam.MTLSCredentialKey(cert)) {
		t.Fatalf("AltLookupKeys = %x, want the legacy cert-DER key %x", node.finishReq.AltLookupKeys, pkgiam.MTLSCredentialKey(cert))
	}
}

// TestWithVerifiedPeerCert_CertOverridesSuppliedKey asserts that once a
// certificate is supplied, a caller-supplied lookup key is ignored: the
// certificate is authoritative even on the in-process trusted path.
func TestWithVerifiedPeerCert_CertOverridesSuppliedKey(t *testing.T) {
	auth, node := newPeerCertAuthService(t)
	cert := makeSelfSignedCert(t)

	ctx := WithVerifiedPeerCert(context.Background(), cert)
	if _, err := auth.FinishLogin(ctx, FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		SessionID: "sess",
		LookupKey: []byte("caller-claimed-key"),
	}); err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if want := pkgiam.MTLSSPKICredentialKey(cert); !bytes.Equal(node.finishReq.LookupKey, want) {
		t.Fatalf("LookupKey = %x, want SPKI key %x", node.finishReq.LookupKey, want)
	}
}

// TestWithVerifiedPeerCert_MatchesServiceContract asserts the re-export stores
// the certificate exactly where the facade reads it, and that a nil
// certificate is a no-op (the facade then decides, never a panic).
func TestWithVerifiedPeerCert_MatchesServiceContract(t *testing.T) {
	cert := makeSelfSignedCert(t)
	if got := service.VerifiedPeerCertFromContext(WithVerifiedPeerCert(context.Background(), cert)); got != cert {
		t.Fatalf("VerifiedPeerCertFromContext = %v, want the supplied certificate", got)
	}

	base := context.Background()
	if got := WithVerifiedPeerCert(base, nil); got != base {
		t.Fatal("a nil certificate must return the context unchanged")
	}
}
