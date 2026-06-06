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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestAuthRESTHandler_Logout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/logout" {
			t.Errorf("expected /api/v1/auth/logout, got %s", r.URL.Path)
		}

		// Decode request body
		var req authLogoutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.AccessToken != "access-token-123" {
			t.Errorf("expected access token 'access-token-123', got %s", req.AccessToken)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"message": "logged out successfully"}`))
		if err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	tr, err := New(&transport.Config{
		Address: server.URL,
	})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	result, err := tr.Request(ctx, "quicbaas.Auth", "Logout", map[string]interface{}{
		"access_token": "access-token-123",
	})
	if err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	// Logout returns nil on success
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestAuthRESTHandler_RefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/refresh" {
			t.Errorf("expected /api/v1/auth/refresh, got %s", r.URL.Path)
		}

		var req authRefreshTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.RefreshToken != "refresh-token-456" {
			t.Errorf("expected refresh token 'refresh-token-456', got %s", req.RefreshToken)
		}

		expiresAt := time.Now().Add(time.Hour)
		resp := authLoginResponse{
			AccessToken: "new-access-token-789",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
			ExpiresAt:   expiresAt.Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(resp)
		if err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	tr, err := New(&transport.Config{
		Address: server.URL,
	})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	result, err := tr.Request(ctx, "quicbaas.Auth", "RefreshToken", map[string]interface{}{
		"refresh_token": "refresh-token-456",
	})
	if err != nil {
		t.Fatalf("refresh token failed: %v", err)
	}

	refreshResp, ok := result.(*service.ExtendedRefreshTokenResponse)
	if !ok {
		t.Fatalf("expected *service.ExtendedRefreshTokenResponse, got %T", result)
	}
	if refreshResp.AccessToken != "new-access-token-789" {
		t.Errorf("expected access token 'new-access-token-789', got %s", refreshResp.AccessToken)
	}
	if refreshResp.TokenType != "Bearer" {
		t.Errorf("expected token type 'Bearer', got %s", refreshResp.TokenType)
	}
}

func TestAuthRESTHandler_ValidateToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/validate" {
			t.Errorf("expected /api/v1/auth/validate, got %s", r.URL.Path)
		}

		// Check authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer access-token-123" {
			t.Errorf("expected Authorization 'Bearer access-token-123', got %s", authHeader)
		}

		expiresAt := time.Now().Add(time.Hour)
		resp := authValidateTokenResponse{
			Valid:     true,
			UserID:    "user-id-1",
			Username:  "testuser",
			TenantID:  "tenant-1",
			Roles:     []string{"admin", "user"},
			ExpiresAt: expiresAt.Format(time.RFC3339),
			IsRevoked: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		err := json.NewEncoder(w).Encode(resp)
		if err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	tr, err := New(&transport.Config{
		Address: server.URL,
	})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	result, err := tr.Request(ctx, "quicbaas.Auth", "ValidateToken", map[string]interface{}{
		"access_token": "access-token-123",
	})
	if err != nil {
		t.Fatalf("validate token failed: %v", err)
	}

	claims, ok := result.(*service.TokenClaims)
	if !ok {
		t.Fatalf("expected *service.TokenClaims, got %T", result)
	}
	if !claims.Valid {
		t.Error("expected valid claims")
	}
	if claims.UserID != "user-id-1" {
		t.Errorf("expected user ID 'user-id-1', got %s", claims.UserID)
	}
	if claims.Username != "testuser" {
		t.Errorf("expected username 'testuser', got %s", claims.Username)
	}
	if len(claims.Roles) != 2 || claims.Roles[0] != "admin" {
		t.Errorf("expected roles ['admin', 'user'], got %v", claims.Roles)
	}
}

// TestAuthRESTHandler_Login_LegacyPasswordRetired asserts the legacy
// username/password Login verb fails closed with a typed
// PasswordLoginUnsupportedError and never reaches the network. The unified
// login ceremony (BeginLogin/FinishLogin) is the canonical login.
func TestAuthRESTHandler_Login_LegacyPasswordRetired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("legacy password Login must not reach the network; got request to %s", r.URL.Path)
	}))
	defer server.Close()

	tr, err := New(&transport.Config{Address: server.URL})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()

	cases := []struct {
		name string
		req  interface{}
	}{
		{"ExtendedLoginRequest", &service.ExtendedLoginRequest{Username: "testuser"}},
		{"map with password", map[string]interface{}{"username": "u", "password": "secret"}},
		{"nil request", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tr.Request(ctx, "quicbaas.Auth", "Login", tc.req)
			if err == nil {
				t.Fatal("expected PasswordLoginUnsupportedError, got nil")
			}
			if !errors.IsPasswordLoginUnsupported(err) {
				t.Fatalf("expected PasswordLoginUnsupportedError, got %T: %v", err, err)
			}
		})
	}
}

func TestAuthRESTHandler_UnsupportedMethod(t *testing.T) {
	tr, err := New(&transport.Config{
		Address: "http://localhost:8080",
	})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer tr.Close()

	ctx := context.Background()
	_, err = tr.Request(ctx, "quicbaas.Auth", "UnsupportedMethod", nil)
	if err == nil {
		t.Fatal("expected error for unsupported method")
	}
}

func TestIsAuthService(t *testing.T) {
	tests := []struct {
		service  string
		expected bool
	}{
		{"quicbaas.Auth", true},
		{"quicbaas.Auth.Login", true},
		{"quicbaas.KV", false},
		{"Auth", false},
		{"quicbaas.IAM.User", false},
	}

	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			if got := isAuthService(tt.service); got != tt.expected {
				t.Errorf("isAuthService(%s) = %v, want %v", tt.service, got, tt.expected)
			}
		})
	}
}
