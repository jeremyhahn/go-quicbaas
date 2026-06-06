// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// authStructReq is a typed request neither map nor *service.ExtendedLoginRequest;
// it exercises the toMap default branch of the auth handlers.
type authStructReq struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	MFACode      string `json:"mfa_code"`
	TenantID     string `json:"tenant_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// TestAuthHandlerTypedLoginRequest asserts the *service.ExtendedLoginRequest
// branch of handleLogin fails closed with the typed
// PasswordLoginUnsupportedError and never reaches the network.
func TestAuthHandlerTypedLoginRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("legacy password Login must not reach the network; got %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), authServicePrefix, "Login",
		&service.ExtendedLoginRequest{Username: "alice", MFACode: "123", TenantID: "t1"})
	if err == nil {
		t.Fatal("expected PasswordLoginUnsupportedError, got nil")
	}
	if !sdkerrors.IsPasswordLoginUnsupported(err) {
		t.Fatalf("expected PasswordLoginUnsupportedError, got %T: %v", err, err)
	}
}

// TestAuthHandlerExpiresInFallback exercises the expires_in fallback branch when
// expires_at fails to parse, across Login and RefreshToken.
func TestAuthHandlerExpiresInFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","token_type":"Bearer",` +
			`"expires_at":"","expires_in":3600}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("Login fail-closed", func(t *testing.T) {
		_, err := tr.Request(ctx, authServicePrefix, "Login",
			map[string]interface{}{"username": "alice", "password": "p"})
		if err == nil {
			t.Fatal("expected PasswordLoginUnsupportedError, got nil")
		}
		if !sdkerrors.IsPasswordLoginUnsupported(err) {
			t.Fatalf("expected PasswordLoginUnsupportedError, got %T: %v", err, err)
		}
	})

	t.Run("RefreshToken fallback", func(t *testing.T) {
		resp, err := tr.Request(ctx, authServicePrefix, "RefreshToken",
			map[string]interface{}{"refresh_token": "rt"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out := resp.(*service.ExtendedRefreshTokenResponse)
		if out.ExpiresAt.IsZero() {
			t.Error("expected expires_at computed from expires_in fallback")
		}
	})
}

// TestAuthHandlerTypedStructRequests exercises the toMap default branch on each
// auth handler using a typed (non-map) request struct.
func TestAuthHandlerTypedStructRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/validate":
			_, _ = w.Write([]byte(`{"valid":true,"user_id":"u1"}`))
		default:
			_, _ = w.Write([]byte(`{"access_token":"at","token_type":"Bearer","expires_at":"2030-01-01T00:00:00Z"}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	methods := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"LogoutStruct", "Logout", &authStructReq{AccessToken: "at"}},
		{"RefreshStruct", "RefreshToken", &authStructReq{RefreshToken: "rt"}},
		{"ValidateStruct", "ValidateToken", &authStructReq{AccessToken: "at"}},
	}
	for _, tc := range methods {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tr.Request(ctx, authServicePrefix, tc.method, tc.req); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
