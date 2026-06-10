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

package service

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// fakeLoginClient is a test double for qrdbtransport.LoginClient. It records
// the FinishLogin request it received so tests can assert which lookup key the
// facade forwarded.
type fakeLoginClient struct {
	beginResp     *qrdbtransport.BeginLoginResponse
	finishResp    *qrdbtransport.FinishLoginResponse
	beginErr      error
	finishErr     error
	gotFinishReq  qrdbtransport.FinishLoginRequest
	gotBeginReq   qrdbtransport.BeginLoginRequest
	finishInvoked bool
}

func (f *fakeLoginClient) ResolveIdentity(_ context.Context, _ string) (*qrdbtransport.Identity, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeLoginClient) BeginLogin(_ context.Context, req qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	f.gotBeginReq = req
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.beginResp, nil
}

func (f *fakeLoginClient) FinishLogin(_ context.Context, req qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	f.finishInvoked = true
	f.gotFinishReq = req
	if f.finishErr != nil {
		return nil, f.finishErr
	}
	return f.finishResp, nil
}

func (f *fakeLoginClient) Logout(_ context.Context, _ string) error { return nil }

// newTestCert returns a freshly generated self-signed certificate for use as a
// verified peer certificate in tests.
func newTestCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

// TestLoginService_FinishLogin_MTLS_NetworkNoCert_FailsClosed asserts that an
// mtls FinishLogin over a network transport (no embedded marker, no verified
// peer cert in context) fails closed with MTLSPeerCertRequiredError and never
// reaches the backing login client.
func TestLoginService_FinishLogin_MTLS_NetworkNoCert_FailsClosed(t *testing.T) {
	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "should-not-issue"}}
	svc := NewLoginService(fake)

	_, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		LookupKey: []byte("attacker-supplied-key"),
	})
	if err == nil {
		t.Fatal("expected MTLSPeerCertRequiredError, got nil")
	}
	var want *MTLSPeerCertRequiredError
	if !errors.As(err, &want) {
		t.Fatalf("expected *MTLSPeerCertRequiredError, got %T: %v", err, err)
	}
	if fake.finishInvoked {
		t.Fatal("backing login client must NOT be invoked when failing closed")
	}
}

// TestLoginService_FinishLogin_MTLS_PeerCertWins is the anti-spoofing test: the
// request carries a DIFFERENT lookup key than the verified peer certificate.
// The facade MUST overwrite the request key with the renewal-stable SPKI
// thumbprint SHA-256(peerCert.RawSubjectPublicKeyInfo) — carrying the legacy
// SHA-256(peerCert.Raw) key in AltLookupKeys — so the spoofed value is rejected.
func TestLoginService_FinishLogin_MTLS_PeerCertWins(t *testing.T) {
	cert := newTestCert(t, "real-user")
	expectedKey := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	expectedAlt := sha256.Sum256(cert.Raw)

	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "ok"}}
	svc := NewLoginService(fake)

	ctx := WithVerifiedPeerCert(context.Background(), cert)
	spoofed := []byte("attacker-chosen-victim-cert-hash")

	_, err := svc.FinishLogin(ctx, &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		LookupKey: spoofed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.finishInvoked {
		t.Fatal("backing login client should have been invoked")
	}
	if bytes.Equal(fake.gotFinishReq.LookupKey, spoofed) {
		t.Fatal("spoofed lookup key was forwarded; peer cert did NOT win")
	}
	if !bytes.Equal(fake.gotFinishReq.LookupKey, expectedKey[:]) {
		t.Fatalf("forwarded lookup key = %x, want SHA-256(cert.RawSubjectPublicKeyInfo) = %x",
			fake.gotFinishReq.LookupKey, expectedKey[:])
	}
	if len(fake.gotFinishReq.AltLookupKeys) != 1 ||
		!bytes.Equal(fake.gotFinishReq.AltLookupKeys[0], expectedAlt[:]) {
		t.Fatalf("alt lookup keys = %x, want one legacy SHA-256(cert.Raw) = %x",
			fake.gotFinishReq.AltLookupKeys, expectedAlt[:])
	}
}

// TestLoginService_FinishLogin_MTLS_Embedded_TrustsClientKey asserts that the
// embedded-trusted path forwards the caller-supplied lookup key as-is (no peer
// cert is available in-process).
func TestLoginService_FinishLogin_MTLS_Embedded_TrustsClientKey(t *testing.T) {
	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "ok"}}
	svc := NewLoginService(fake)

	clientKey := []byte("in-process-trusted-key")
	ctx := WithEmbeddedTrustedContext(context.Background())

	_, err := svc.FinishLogin(ctx, &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		LookupKey: clientKey,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(fake.gotFinishReq.LookupKey, clientKey) {
		t.Fatalf("embedded path should forward client key as-is; got %x want %x",
			fake.gotFinishReq.LookupKey, clientKey)
	}
}

// TestLoginService_FinishLogin_PIV_ForwardsUnchanged asserts that a PIV factor
// request forwards the lookup key unchanged (the signature is the proof; no
// peer cert is consulted).
func TestLoginService_FinishLogin_PIV_ForwardsUnchanged(t *testing.T) {
	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "ok"}}
	svc := NewLoginService(fake)

	key := []byte("piv-slot-cert-hash")
	sig := []byte("piv-signature")
	_, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypePIV,
		LookupKey: key,
		Signature: sig,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(fake.gotFinishReq.LookupKey, key) {
		t.Fatalf("PIV lookup key altered: got %x want %x", fake.gotFinishReq.LookupKey, key)
	}
	if !bytes.Equal(fake.gotFinishReq.Signature, sig) {
		t.Fatal("PIV signature altered")
	}
}

// TestLoginService_FinishLogin_FIDO2_ForwardsUnchanged mirrors the PIV case for
// the fido2/webauthn factor.
func TestLoginService_FinishLogin_FIDO2_ForwardsUnchanged(t *testing.T) {
	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "ok"}}
	svc := NewLoginService(fake)

	key := []byte("webauthn-credential-id")
	_, err := svc.FinishLogin(context.Background(), &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeFIDO2,
		LookupKey: key,
		Signature: []byte("assertion"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(fake.gotFinishReq.LookupKey, key) {
		t.Fatalf("fido2 lookup key altered: got %x want %x", fake.gotFinishReq.LookupKey, key)
	}
}

// TestLoginService_FinishLogin_MTLS_CertWinsEvenWhenEmbeddedMarked asserts that
// when BOTH a verified peer cert and the embedded marker are present, the
// verified cert takes precedence (defense in depth: a real handshake always
// overrides the in-process trust shortcut).
func TestLoginService_FinishLogin_MTLS_CertWinsEvenWhenEmbeddedMarked(t *testing.T) {
	cert := newTestCert(t, "real-user")
	expectedKey := sha256.Sum256(cert.RawSubjectPublicKeyInfo)

	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "ok"}}
	svc := NewLoginService(fake)

	ctx := WithEmbeddedTrustedContext(context.Background())
	ctx = WithVerifiedPeerCert(ctx, cert)

	_, err := svc.FinishLogin(ctx, &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		LookupKey: []byte("spoofed"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(fake.gotFinishReq.LookupKey, expectedKey[:]) {
		t.Fatalf("cert must win over embedded marker; got %x want %x",
			fake.gotFinishReq.LookupKey, expectedKey[:])
	}
}

// TestLoginService_Unwired_FailsClosed asserts both verbs fail closed with
// LoginCeremonyUnavailableError when no backing client is set.
func TestLoginService_Unwired_FailsClosed(t *testing.T) {
	svc := NewLoginService(nil)

	if _, err := svc.BeginLogin(context.Background(), &qrdbtransport.BeginLoginRequest{Type: qrdbtransport.FactorTypeMTLS}); err == nil {
		t.Fatal("BeginLogin: expected error on unwired service")
	} else {
		var want *LoginCeremonyUnavailableError
		if !errors.As(err, &want) {
			t.Fatalf("BeginLogin: expected *LoginCeremonyUnavailableError, got %T", err)
		}
	}

	if _, err := svc.FinishLogin(WithEmbeddedTrustedContext(context.Background()), &qrdbtransport.FinishLoginRequest{Type: qrdbtransport.FactorTypePIV}); err == nil {
		t.Fatal("FinishLogin: expected error on unwired service")
	} else {
		var want *LoginCeremonyUnavailableError
		if !errors.As(err, &want) {
			t.Fatalf("FinishLogin: expected *LoginCeremonyUnavailableError, got %T", err)
		}
	}
}

// TestLoginService_NilRequest_FailsClosed asserts nil requests are rejected.
func TestLoginService_NilRequest_FailsClosed(t *testing.T) {
	svc := NewLoginService(&fakeLoginClient{})
	if _, err := svc.BeginLogin(context.Background(), nil); err == nil {
		t.Fatal("BeginLogin: expected error on nil request")
	}
	if _, err := svc.FinishLogin(context.Background(), nil); err == nil {
		t.Fatal("FinishLogin: expected error on nil request")
	}
}

// TestLoginService_BeginLogin_Forwards asserts BeginLogin forwards the request
// and returns the backing response.
func TestLoginService_BeginLogin_Forwards(t *testing.T) {
	fake := &fakeLoginClient{beginResp: &qrdbtransport.BeginLoginResponse{SessionID: "sess-1", Nonce: []byte("n")}}
	svc := NewLoginService(fake)
	resp, err := svc.BeginLogin(context.Background(), &qrdbtransport.BeginLoginRequest{
		Type: qrdbtransport.FactorTypeMTLS,
		Hint: "alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.SessionID != "sess-1" {
		t.Fatalf("unexpected session id: %q", resp.SessionID)
	}
	if fake.gotBeginReq.Hint != "alice" {
		t.Fatalf("hint not forwarded: %q", fake.gotBeginReq.Hint)
	}
}

// TestLoginService_SetLoginClient_InstallsBackingClient asserts that a facade
// constructed with no client fails closed, then forwards BeginLogin to the
// client installed via SetLoginClient. This proves the late-wiring path the
// server uses once the qrdb node is constructed.
func TestLoginService_SetLoginClient_InstallsBackingClient(t *testing.T) {
	svc := NewLoginService(nil)

	// Before wiring: fails closed.
	var unavailable *LoginCeremonyUnavailableError
	if _, err := svc.BeginLogin(context.Background(), &qrdbtransport.BeginLoginRequest{}); !errors.As(err, &unavailable) {
		t.Fatalf("pre-wire BeginLogin: expected *LoginCeremonyUnavailableError, got %T", err)
	}

	fake := &fakeLoginClient{beginResp: &qrdbtransport.BeginLoginResponse{}}
	svc.SetLoginClient(fake)

	if _, err := svc.BeginLogin(context.Background(), &qrdbtransport.BeginLoginRequest{Hint: "alice"}); err != nil {
		t.Fatalf("post-wire BeginLogin: unexpected error: %v", err)
	}
	if fake.gotBeginReq.Hint != "alice" {
		t.Fatalf("SetLoginClient did not install forwarding client; hint=%q", fake.gotBeginReq.Hint)
	}
}

// TestLoginErrors_Messages pins the user-facing message contract of the login
// facade's typed errors. Transports surface these strings to clients, so the
// exact wording is part of the API and is asserted directly here.
func TestLoginErrors_Messages(t *testing.T) {
	if got := (&LoginCeremonyUnavailableError{}).Error(); got != "login: ceremony unavailable (no in-process login client wired)" {
		t.Fatalf("LoginCeremonyUnavailableError.Error() = %q", got)
	}
	if got := (&MTLSPeerCertRequiredError{}).Error(); got != "login: mtls factor requires a verified peer certificate; refusing client-supplied lookup key" {
		t.Fatalf("MTLSPeerCertRequiredError.Error() = %q", got)
	}
	if got := (&LoginRequestNilError{Op: "BeginLogin"}).Error(); got != "login: nil request for BeginLogin" {
		t.Fatalf("LoginRequestNilError.Error() = %q", got)
	}
}
