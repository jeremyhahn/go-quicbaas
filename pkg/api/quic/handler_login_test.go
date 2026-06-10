//go:build quic

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

package quic

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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// quicLoginFakeClient backs a real service.LoginService so the QUIC handler ->
// facade -> client chain runs end to end.
type quicLoginFakeClient struct {
	finishReq     qrdbtransport.FinishLoginRequest
	finishInvoked bool
}

func (f *quicLoginFakeClient) ResolveIdentity(_ context.Context, _ string) (*qrdbtransport.Identity, error) {
	return nil, errors.New("not implemented")
}

func (f *quicLoginFakeClient) BeginLogin(_ context.Context, _ qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	return &qrdbtransport.BeginLoginResponse{SessionID: "quic-sess", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (f *quicLoginFakeClient) FinishLogin(_ context.Context, req qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	f.finishInvoked = true
	f.finishReq = req
	return &qrdbtransport.FinishLoginResponse{Bearer: "quic-bearer"}, nil
}

func (f *quicLoginFakeClient) Logout(_ context.Context, _ string) error { return nil }

func quicLoginCert(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "quic-user"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func newQUICLoginHandler(fake *quicLoginFakeClient) *Handler {
	svc := &mockService{loginSvc: service.NewLoginService(fake)}
	return NewHandler(svc, nil, nil, nil, createTestLogger())
}

func encodeQUICReq(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := EncodePayload(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestQUICHandleFinishLogin_MTLS_NoPeerCert_FailsClosed asserts the QUIC
// handler fails closed for mtls when the context carries no verified peer cert.
func TestQUICHandleFinishLogin_MTLS_NoPeerCert_FailsClosed(t *testing.T) {
	fake := &quicLoginFakeClient{}
	h := newQUICLoginHandler(fake)

	payload := encodeQUICReq(t, &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		LookupKey: []byte("attacker-key"),
	})
	resp := h.handleFinishLogin(context.Background(), &Request{Opcode: OpAuthFinishLogin, Payload: payload})

	if resp.Status == StatusOK {
		t.Fatal("expected non-OK status for fail-closed mtls")
	}
	if fake.finishInvoked {
		t.Fatal("login client must not be reached on fail-closed")
	}
}

// TestQUICHandleFinishLogin_MTLS_PeerCertWins asserts the QUIC handler derives
// the lookup key from the verified peer cert (placed on ctx by the server),
// overriding the spoofed request value. The primary key is the renewal-stable
// SPKI thumbprint; the legacy SHA-256(cert.Raw) key rides in AltLookupKeys.
func TestQUICHandleFinishLogin_MTLS_PeerCertWins(t *testing.T) {
	cert := quicLoginCert(t)
	wantKey := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	wantAlt := sha256.Sum256(cert.Raw)

	fake := &quicLoginFakeClient{}
	h := newQUICLoginHandler(fake)

	ctx := service.WithVerifiedPeerCert(context.Background(), cert)
	payload := encodeQUICReq(t, &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		LookupKey: []byte("spoofed-victim-hash"),
	})
	resp := h.handleFinishLogin(ctx, &Request{Opcode: OpAuthFinishLogin, Payload: payload})

	if resp.Status != StatusOK {
		t.Fatalf("expected OK, got status %d: %s", resp.Status, string(resp.Payload))
	}
	if !fake.finishInvoked {
		t.Fatal("login client not reached")
	}
	if bytes.Equal(fake.finishReq.LookupKey, []byte("spoofed-victim-hash")) {
		t.Fatal("spoofed key forwarded")
	}
	if !bytes.Equal(fake.finishReq.LookupKey, wantKey[:]) {
		t.Fatalf("lookup key = %x, want %x", fake.finishReq.LookupKey, wantKey[:])
	}
	if len(fake.finishReq.AltLookupKeys) != 1 ||
		!bytes.Equal(fake.finishReq.AltLookupKeys[0], wantAlt[:]) {
		t.Fatalf("alt lookup keys = %x, want one legacy SHA-256(cert.Raw) = %x",
			fake.finishReq.AltLookupKeys, wantAlt[:])
	}
}

// TestQUICHandleFinishLogin_PIV_ForwardsUnchanged asserts a PIV request
// forwards the lookup key intact.
func TestQUICHandleFinishLogin_PIV_ForwardsUnchanged(t *testing.T) {
	fake := &quicLoginFakeClient{}
	h := newQUICLoginHandler(fake)

	key := []byte("piv-slot-hash")
	payload := encodeQUICReq(t, &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypePIV,
		LookupKey: key,
		Signature: []byte("sig"),
	})
	resp := h.handleFinishLogin(context.Background(), &Request{Opcode: OpAuthFinishLogin, Payload: payload})

	if resp.Status != StatusOK {
		t.Fatalf("expected OK, got %d: %s", resp.Status, string(resp.Payload))
	}
	if !bytes.Equal(fake.finishReq.LookupKey, key) {
		t.Fatalf("PIV key altered: got %x want %x", fake.finishReq.LookupKey, key)
	}
}

// TestQUICHandleBeginLogin_RoutesToFacade asserts the BeginLogin handler
// reaches the facade and is registered as a public op.
func TestQUICHandleBeginLogin_RoutesToFacade(t *testing.T) {
	fake := &quicLoginFakeClient{}
	h := newQUICLoginHandler(fake)

	payload := encodeQUICReq(t, &qrdbtransport.BeginLoginRequest{Type: qrdbtransport.FactorTypeMTLS})
	resp := h.handleBeginLogin(context.Background(), &Request{Opcode: OpAuthBeginLogin, Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("expected OK, got %d: %s", resp.Status, string(resp.Payload))
	}

	if !isPublicOperation(OpAuthBeginLogin) || !isPublicOperation(OpAuthFinishLogin) {
		t.Fatal("login opcodes must be public (ceremony authenticates the caller)")
	}
}
