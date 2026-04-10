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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestIsAuthService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{
			name:     "auth service - Login",
			service:  "quicbaas.Auth",
			expected: true,
		},
		{
			name:     "auth service with method suffix",
			service:  "quicbaas.Auth.Login",
			expected: true,
		},
		{
			name:     "not auth service - IAM",
			service:  "quicbaas.IAM.User",
			expected: false,
		},
		{
			name:     "not auth service - KV",
			service:  "quicbaas.KV",
			expected: false,
		},
		{
			name:     "empty service",
			service:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAuthService(tt.service)
			if result != tt.expected {
				t.Errorf("isAuthService(%q) = %v, want %v", tt.service, result, tt.expected)
			}
		})
	}
}

func TestAuthHandlerHandleRequest_UnsupportedMethod(t *testing.T) {
	handler := newAuthHandler(nil)

	_, err := handler.handleRequest(nil, "quicbaas.Auth", "UnsupportedMethod", nil)
	if err == nil {
		t.Error("expected error for unsupported method")
	}
}

func TestAuthHandlerHandleLogin_InvalidRequestType(t *testing.T) {
	handler := newAuthHandler(nil)

	// Pass an invalid type
	_, err := handler.handleLogin(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestAuthHandlerHandleLogout_InvalidRequestType(t *testing.T) {
	handler := newAuthHandler(nil)

	// Pass an invalid type
	_, err := handler.handleLogout(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestAuthHandlerHandleRefreshToken_InvalidRequestType(t *testing.T) {
	handler := newAuthHandler(nil)

	// Pass an invalid type
	_, err := handler.handleRefreshToken(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestAuthHandlerHandleValidateToken_InvalidRequestType(t *testing.T) {
	handler := newAuthHandler(nil)

	// Pass an invalid type
	_, err := handler.handleValidateToken(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestExtendedLoginRequestType(t *testing.T) {
	req := &service.ExtendedLoginRequest{
		Username: "testuser",
		Password: "testpass",
		MFACode:  "123456",
		TenantID: "tenant1",
	}

	if req.Username != "testuser" {
		t.Errorf("Username = %q, want %q", req.Username, "testuser")
	}
	if req.Password != "testpass" {
		t.Errorf("Password = %q, want %q", req.Password, "testpass")
	}
	if req.MFACode != "123456" {
		t.Errorf("MFACode = %q, want %q", req.MFACode, "123456")
	}
	if req.TenantID != "tenant1" {
		t.Errorf("TenantID = %q, want %q", req.TenantID, "tenant1")
	}
}

func TestRefreshTokenRequestType(t *testing.T) {
	req := &service.RefreshTokenRequest{
		RefreshToken: "test-refresh-token",
	}

	if req.RefreshToken != "test-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", req.RefreshToken, "test-refresh-token")
	}
}

func TestValidateTokenRequestType(t *testing.T) {
	req := &service.ValidateTokenRequest{
		AccessToken: "test-access-token",
	}

	if req.AccessToken != "test-access-token" {
		t.Errorf("AccessToken = %q, want %q", req.AccessToken, "test-access-token")
	}
}

func TestLogoutRequestType(t *testing.T) {
	req := &service.LogoutRequest{
		AccessToken: "test-access-token",
	}

	if req.AccessToken != "test-access-token" {
		t.Errorf("AccessToken = %q, want %q", req.AccessToken, "test-access-token")
	}
}
