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
	"context"
	"testing"

	"google.golang.org/grpc"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeFIDO2Server is an in-memory pb.FIDO2ServiceServer that echoes
// deterministic ceremony material and records the tenant it received, so the
// tests can pin the context-tenant -> proto tenant_id plumbing.
type fakeFIDO2Server struct {
	fail       bool
	lastTenant string
}

func (f *fakeFIDO2Server) BeginRegistration(_ context.Context, req *pb.FIDO2BeginRegistrationRequest) (*pb.FIDO2BeginRegistrationResponse, error) {
	f.lastTenant = req.TenantId
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.FIDO2BeginRegistrationResponse{SessionId: "sess-reg", OptionsJson: `{"publicKey":{"user":"` + req.Username + `"}}`}, nil
}

func (f *fakeFIDO2Server) FinishRegistration(_ context.Context, req *pb.FIDO2FinishRegistrationRequest) (*pb.FIDO2FinishRegistrationResponse, error) {
	f.lastTenant = req.TenantId
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.FIDO2FinishRegistrationResponse{Success: true, CredentialId: "cred-1", Token: "tok-reg"}, nil
}

func (f *fakeFIDO2Server) BeginAuthentication(_ context.Context, req *pb.FIDO2BeginAuthenticationRequest) (*pb.FIDO2BeginAuthenticationResponse, error) {
	f.lastTenant = req.TenantId
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.FIDO2BeginAuthenticationResponse{SessionId: "sess-auth", OptionsJson: `{"publicKey":{}}`}, nil
}

func (f *fakeFIDO2Server) FinishAuthentication(_ context.Context, req *pb.FIDO2FinishAuthenticationRequest) (*pb.FIDO2FinishAuthenticationResponse, error) {
	f.lastTenant = req.TenantId
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.FIDO2FinishAuthenticationResponse{Success: true, Token: "tok-auth", Username: req.Username}, nil
}

// newFIDO2TestConn starts an in-memory FIDO2Service and returns a dialed
// connection plus the fake for assertions.
func newFIDO2TestConn(t *testing.T, fail bool) (*grpc.ClientConn, *fakeFIDO2Server) {
	t.Helper()
	fake := &fakeFIDO2Server{fail: fail}
	hs := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterFIDO2ServiceServer(s, fake)
	})
	return hs.dial(t), fake
}

func TestIsFIDO2Service(t *testing.T) {
	tests := []struct {
		service  string
		expected bool
	}{
		{"quicbaas.FIDO2", true},
		{"FIDO2", true},
		{"quicbaas.PIV", false},
		{"quicbaas.Trust", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isFIDO2Service(tt.service); got != tt.expected {
			t.Errorf("isFIDO2Service(%q) = %v, want %v", tt.service, got, tt.expected)
		}
	}
}

func TestFIDO2Handler_BeginRegistration_RoundTrip(t *testing.T) {
	conn, fake := newFIDO2TestConn(t, false)
	handler := newFIDO2Handler(conn)

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	resp, err := handler.handleRequest(ctx, "quicbaas.FIDO2", "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{Username: "alice@example.com", DisplayName: "Alice"})
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	out, ok := resp.(*xkmstransport.BeginRegistrationResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if out.SessionID != "sess-reg" {
		t.Errorf("session id = %q, want sess-reg", out.SessionID)
	}
	if out.OptionsJSON == "" {
		t.Error("options JSON is empty")
	}
	if fake.lastTenant != "tenant-a" {
		t.Errorf("server saw tenant %q, want tenant-a", fake.lastTenant)
	}
}

func TestFIDO2Handler_FinishRegistration_RoundTrip(t *testing.T) {
	conn, fake := newFIDO2TestConn(t, false)
	handler := newFIDO2Handler(conn)

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	resp, err := handler.handleRequest(ctx, "quicbaas.FIDO2", "FinishRegistration",
		&xkmstransport.FinishRegistrationRequest{SessionID: "sess-reg", AttestationJSON: `{"id":"cred-1"}`})
	if err != nil {
		t.Fatalf("FinishRegistration: %v", err)
	}
	out, ok := resp.(*xkmstransport.FinishRegistrationResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if !out.Success || out.CredentialID != "cred-1" || out.Token != "tok-reg" {
		t.Errorf("unexpected response: %+v", out)
	}
	if fake.lastTenant != "tenant-a" {
		t.Errorf("server saw tenant %q, want tenant-a", fake.lastTenant)
	}
}

func TestFIDO2Handler_BeginAuthentication_RoundTrip(t *testing.T) {
	conn, _ := newFIDO2TestConn(t, false)
	handler := newFIDO2Handler(conn)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.FIDO2", "BeginAuthentication",
		&xkmstransport.BeginAuthenticationRequest{Username: "alice@example.com"})
	if err != nil {
		t.Fatalf("BeginAuthentication: %v", err)
	}
	out, ok := resp.(*xkmstransport.BeginAuthenticationResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if out.SessionID != "sess-auth" {
		t.Errorf("session id = %q, want sess-auth", out.SessionID)
	}
}

func TestFIDO2Handler_FinishAuthentication_RoundTrip(t *testing.T) {
	conn, _ := newFIDO2TestConn(t, false)
	handler := newFIDO2Handler(conn)

	resp, err := handler.handleRequest(context.Background(), "quicbaas.FIDO2", "FinishAuthentication",
		&xkmstransport.FinishAuthenticationRequest{SessionID: "sess-auth", Username: "alice@example.com", AssertionJSON: `{"id":"cred-1"}`})
	if err != nil {
		t.Fatalf("FinishAuthentication: %v", err)
	}
	out, ok := resp.(*xkmstransport.FinishAuthenticationResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if !out.Success || out.Token != "tok-auth" || out.Username != "alice@example.com" {
		t.Errorf("unexpected response: %+v", out)
	}
}

func TestFIDO2Handler_ServerError(t *testing.T) {
	conn, _ := newFIDO2TestConn(t, true)
	handler := newFIDO2Handler(conn)

	_, err := handler.handleRequest(context.Background(), "quicbaas.FIDO2", "BeginRegistration",
		&xkmstransport.BeginRegistrationRequest{Username: "alice@example.com"})
	if err == nil {
		t.Fatal("expected server error to propagate")
	}
}

func TestFIDO2Handler_UnsupportedMethod(t *testing.T) {
	handler := newFIDO2Handler(nil)
	_, err := handler.handleRequest(context.Background(), "quicbaas.FIDO2", "ListDevices", nil)
	if err == nil {
		t.Fatal("expected error for method not exposed by FIDO2Service proto")
	}
}

func TestFIDO2Handler_InvalidRequestTypes(t *testing.T) {
	conn, _ := newFIDO2TestConn(t, false)
	handler := newFIDO2Handler(conn)

	for _, method := range []string{"BeginRegistration", "FinishRegistration", "BeginAuthentication", "FinishAuthentication"} {
		if _, err := handler.handleRequest(context.Background(), "quicbaas.FIDO2", method, 42); err == nil {
			t.Errorf("%s: expected invalid request type error", method)
		}
	}
}
