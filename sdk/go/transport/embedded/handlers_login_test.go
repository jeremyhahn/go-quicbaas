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

package embedded

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// loginFakeClient backs a real service.LoginService so the embedded handler ->
// facade -> client chain runs end to end (including the embedded-trusted path).
type loginFakeClient struct {
	finishReq     qrdbtransport.FinishLoginRequest
	finishInvoked bool
}

func (f *loginFakeClient) ResolveIdentity(_ context.Context, _ string) (*qrdbtransport.Identity, error) {
	return nil, errors.New("not implemented")
}

func (f *loginFakeClient) BeginLogin(_ context.Context, _ qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	return &qrdbtransport.BeginLoginResponse{SessionID: "embedded-sess", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (f *loginFakeClient) FinishLogin(_ context.Context, req qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	f.finishInvoked = true
	f.finishReq = req
	return &qrdbtransport.FinishLoginResponse{Bearer: "embedded-bearer"}, nil
}

func (f *loginFakeClient) Logout(_ context.Context, _ string) error { return nil }

// mockQuicBaaSForLogin is a minimal service.QuicBaaS whose LoginCeremony()
// returns a real LoginService backed by loginFakeClient.
type mockQuicBaaSForLogin struct {
	service.QuicBaaS
	login service.LoginServicer
}

func (m *mockQuicBaaSForLogin) LoginCeremony() service.LoginServicer { return m.login }

// TestHandleBeginLogin_RoutesToFacade asserts the embedded BeginLogin handler
// reaches the LoginCeremony facade and returns the qrdb response type.
func TestHandleBeginLogin_RoutesToFacade(t *testing.T) {
	fake := &loginFakeClient{}
	svc := &mockQuicBaaSForLogin{login: service.NewLoginService(fake)}

	out, err := handleBeginLogin(context.Background(), svc, &qrdbtransport.BeginLoginRequest{
		Type: qrdbtransport.FactorTypeMTLS,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, ok := out.(*qrdbtransport.BeginLoginResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", out)
	}
	if resp.SessionID != "embedded-sess" {
		t.Fatalf("unexpected session id %q", resp.SessionID)
	}
}

// TestHandleFinishLogin_MTLS_EmbeddedTrustsClientKey asserts the embedded
// FinishLogin handler marks the context embedded-trusted so an mtls factor
// uses the caller-supplied lookup key (in-process trust) WITHOUT a peer cert.
func TestHandleFinishLogin_MTLS_EmbeddedTrustsClientKey(t *testing.T) {
	fake := &loginFakeClient{}
	svc := &mockQuicBaaSForLogin{login: service.NewLoginService(fake)}

	clientKey := []byte("in-process-key")
	out, err := handleFinishLogin(context.Background(), svc, &qrdbtransport.FinishLoginRequest{
		Type:      qrdbtransport.FactorTypeMTLS,
		LookupKey: clientKey,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The embedded path returns the SDK transport shape, exactly as REST /
	// gRPC / QUIC / MCP do, so a caller sees one type on every transport.
	resp, ok := out.(*sdktransport.FinishLoginResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", out)
	}
	if resp.Bearer != "embedded-bearer" {
		t.Fatalf("unexpected bearer %q", resp.Bearer)
	}
	if !fake.finishInvoked {
		t.Fatal("login client not reached")
	}
	if !bytes.Equal(fake.finishReq.LookupKey, clientKey) {
		t.Fatalf("embedded path should forward client key; got %x want %x", fake.finishReq.LookupKey, clientKey)
	}
}

// TestHandleFinishLogin_Unwired asserts a nil facade yields a typed error.
func TestHandleFinishLogin_Unwired(t *testing.T) {
	svc := &mockQuicBaaSForLogin{login: nil}
	_, err := handleFinishLogin(context.Background(), svc, &qrdbtransport.FinishLoginRequest{Type: qrdbtransport.FactorTypePIV})
	if err == nil {
		t.Fatal("expected error when facade is unwired")
	}
}
