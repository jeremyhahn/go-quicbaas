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

package grpc

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// fakeLoginClient is a test double for the in-process qrdbtransport.LoginClient
// that the LoginService facade forwards to. It records the FinishLogin request
// so tests can assert the lookup key that survived the facade's mTLS model.
type fakeLoginClient struct {
	finishReq     qrdbtransport.FinishLoginRequest
	finishResp    *qrdbtransport.FinishLoginResponse
	finishInvoked bool
}

func (f *fakeLoginClient) ResolveIdentity(_ context.Context, _ string) (*qrdbtransport.Identity, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeLoginClient) BeginLogin(_ context.Context, _ qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	return &qrdbtransport.BeginLoginResponse{SessionID: "s1", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (f *fakeLoginClient) FinishLogin(_ context.Context, req qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	f.finishInvoked = true
	f.finishReq = req
	return f.finishResp, nil
}

func (f *fakeLoginClient) Logout(_ context.Context, _ string) error { return nil }

// newGRPCLoginServer builds a LoginServer whose facade is the REAL
// service.LoginService backed by the supplied fake login client, so the
// handler -> facade -> client chain (including the mTLS security model) runs
// end to end.
func newGRPCLoginServer(client *fakeLoginClient) *LoginServer {
	return NewLoginServer(service.NewLoginService(client), nil)
}

func grpcTestCert(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "grpc-user"},
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

func mtlsPeerContext(cert *x509.Certificate) context.Context {
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{cert}},
		},
	}
	p := &peer.Peer{AuthInfo: tlsInfo}
	return peer.NewContext(context.Background(), p)
}

// TestLoginServer_FinishLogin_MTLS_NoPeerCert_FailsClosed asserts the gRPC
// handler over a connection with NO verified peer cert fails closed
// (codes.Unauthenticated) for an mtls factor, and never reaches the client.
func TestLoginServer_FinishLogin_MTLS_NoPeerCert_FailsClosed(t *testing.T) {
	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "should-not-issue"}}
	srv := newGRPCLoginServer(fake)

	_, err := srv.FinishLogin(context.Background(), &pb.FinishLoginRequest{
		Type:      string(qrdbtransport.FactorTypeMTLS),
		LookupKey: []byte("attacker-key"),
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected Unauthenticated, got %s: %v", status.Code(err), err)
	}
	if fake.finishInvoked {
		t.Fatal("login client must not be reached on fail-closed")
	}
}

// TestLoginServer_FinishLogin_MTLS_PeerCertWins asserts the gRPC handler
// derives the lookup key from the verified peer cert, overriding a spoofed
// request value (anti-spoofing). The primary key is the renewal-stable SPKI
// thumbprint; the legacy SHA-256(cert.Raw) key rides in AltLookupKeys.
func TestLoginServer_FinishLogin_MTLS_PeerCertWins(t *testing.T) {
	cert := grpcTestCert(t)
	wantKey := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	wantAlt := sha256.Sum256(cert.Raw)

	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{
		Bearer:    "ok",
		Identity:  &qrdbtransport.Identity{Subject: "grpc-user", TenantID: "t1", Roles: []string{"admin"}},
		ExpiresAt: time.Now().Add(time.Minute),
		FactorID:  "f1",
	}}
	srv := newGRPCLoginServer(fake)

	ctx := mtlsPeerContext(cert)
	resp, err := srv.FinishLogin(ctx, &pb.FinishLoginRequest{
		Type:      string(qrdbtransport.FactorTypeMTLS),
		LookupKey: []byte("spoofed-victim-hash"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.finishInvoked {
		t.Fatal("login client was not reached")
	}
	if bytes.Equal(fake.finishReq.LookupKey, []byte("spoofed-victim-hash")) {
		t.Fatal("spoofed key was forwarded")
	}
	if !bytes.Equal(fake.finishReq.LookupKey, wantKey[:]) {
		t.Fatalf("lookup key = %x, want %x", fake.finishReq.LookupKey, wantKey[:])
	}
	if len(fake.finishReq.AltLookupKeys) != 1 ||
		!bytes.Equal(fake.finishReq.AltLookupKeys[0], wantAlt[:]) {
		t.Fatalf("alt lookup keys = %x, want one legacy SHA-256(cert.Raw) = %x",
			fake.finishReq.AltLookupKeys, wantAlt[:])
	}
	if resp.GetBearer() != "ok" || resp.GetIdentity().GetSubject() != "grpc-user" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

// TestLoginServer_FinishLogin_PIV_ForwardsUnchanged asserts PIV requests
// forward the lookup key intact without any peer cert.
func TestLoginServer_FinishLogin_PIV_ForwardsUnchanged(t *testing.T) {
	fake := &fakeLoginClient{finishResp: &qrdbtransport.FinishLoginResponse{Bearer: "ok", ExpiresAt: time.Now().Add(time.Minute)}}
	srv := newGRPCLoginServer(fake)

	key := []byte("piv-slot-hash")
	_, err := srv.FinishLogin(context.Background(), &pb.FinishLoginRequest{
		Type:      string(qrdbtransport.FactorTypePIV),
		LookupKey: key,
		Signature: []byte("sig"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(fake.finishReq.LookupKey, key) {
		t.Fatalf("PIV key altered: got %x want %x", fake.finishReq.LookupKey, key)
	}
}

// TestLoginServer_Unavailable asserts a nil facade yields codes.Unavailable.
func TestLoginServer_Unavailable(t *testing.T) {
	srv := NewLoginServer(nil, nil)
	if _, err := srv.BeginLogin(context.Background(), &pb.BeginLoginRequest{Type: "mtls"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("BeginLogin: expected Unavailable, got %s", status.Code(err))
	}
	if _, err := srv.FinishLogin(context.Background(), &pb.FinishLoginRequest{Type: "piv"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("FinishLogin: expected Unavailable, got %s", status.Code(err))
	}
}
