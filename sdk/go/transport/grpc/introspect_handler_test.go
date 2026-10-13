// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// IntrospectToken answers like the server does: an empty token is invalid
// input, "garbage" is inactive and anything else is a live attested session.
func (f *fakeQuicBaaSServer) IntrospectToken(_ context.Context, req *pb.IntrospectTokenRequest) (*pb.IntrospectTokenResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	switch req.GetToken() {
	case "":
		return nil, status.Error(codes.InvalidArgument, "token is required")
	case "garbage":
		return &pb.IntrospectTokenResponse{Active: false}, nil
	}
	return &pb.IntrospectTokenResponse{
		Active:       true,
		Subject:      "user-1",
		Username:     "alice",
		ExpiresAt:    3000,
		AttestLevel:  "hardware",
		AttestAaguid: "cb69481e8ff7403993ec0a2729a154a8",
	}, nil
}

func introspectOverGRPC(t *testing.T, token string) (any, error) {
	t.Helper()
	h := newAuthHandler(newQuicBaaSConn(t, false))
	return h.handleRequest(context.Background(), "quicbaas.Auth", "IntrospectToken",
		&service.IntrospectTokenRequest{Token: token})
}

func TestAuthHandler_IntrospectToken_Active(t *testing.T) {
	resp, err := introspectOverGRPC(t, "live")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := resp.(*service.TokenIntrospection)
	if !ok {
		t.Fatalf("expected *service.TokenIntrospection, got %T", resp)
	}
	if !got.Active || got.Subject != "user-1" || got.AttestLevel != "hardware" {
		t.Errorf("active introspection lost claims: %+v", got)
	}
	if got.AttestAAGUID != "cb69481e8ff7403993ec0a2729a154a8" {
		t.Errorf("expected AAGUID to round-trip, got %q", got.AttestAAGUID)
	}
	if !got.ExpiresAt.Equal(time.Unix(3000, 0)) {
		t.Errorf("expected expiry %v, got %v", time.Unix(3000, 0), got.ExpiresAt)
	}
	if !got.IssuedAt.IsZero() {
		t.Errorf("an unset issued_at must stay the zero time, got %v", got.IssuedAt)
	}
}

func TestAuthHandler_IntrospectToken_InactiveCarriesOnlyActive(t *testing.T) {
	resp, err := introspectOverGRPC(t, "garbage")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(resp, &service.TokenIntrospection{Active: false}) {
		t.Errorf("inactive introspection must carry only active=false, got %+v", resp)
	}
}

func TestAuthHandler_IntrospectToken_MissingTokenIsInvalidInput(t *testing.T) {
	_, err := introspectOverGRPC(t, "")
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error, got %v", err)
	}
}

func TestAuthHandler_IntrospectToken_Errors(t *testing.T) {
	h := newAuthHandler(newQuicBaaSConn(t, true))
	_, err := h.handleRequest(context.Background(), "quicbaas.Auth", "IntrospectToken",
		&service.IntrospectTokenRequest{Token: "live"})
	if err == nil {
		t.Error("expected server error to surface")
	}

	_, err = newAuthHandler(nil).handleRequest(context.Background(), "quicbaas.Auth", "IntrospectToken", "wrong")
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid-input error for wrong request type, got %v", err)
	}
}
