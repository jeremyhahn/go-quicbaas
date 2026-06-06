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

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	responses map[string]interface{}
	err       error
	closed    bool
}

func newMockTransport() *mockTransport {
	return &mockTransport{
		responses: make(map[string]interface{}),
	}
}

func (m *mockTransport) Request(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
	if m.closed {
		return nil, errors.New("transport closed")
	}
	if m.err != nil {
		return nil, m.err
	}
	key := svc + "." + method
	return m.responses[key], nil
}

func (m *mockTransport) RequestStream(ctx context.Context, svc, method string, req interface{}) (transport.Stream, error) {
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	m.closed = true
	return nil
}

func (m *mockTransport) Healthy() bool {
	return !m.closed
}

func (m *mockTransport) SetResponse(svc, method string, resp interface{}) {
	m.responses[svc+"."+method] = resp
}

func (m *mockTransport) SetError(err error) {
	m.err = err
}

// TestNewClient tests client creation.
func TestNewClient(t *testing.T) {
	t.Run("with valid transport returns non-nil client", func(t *testing.T) {
		mockT := newMockTransport()
		client := NewClient(mockT)
		if client == nil {
			t.Error("expected non-nil client for valid transport")
		}
	})

	t.Run("with nil transport returns nil", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

func TestClient_InterfaceCompliance(t *testing.T) {
	t.Run("client implements Client interface", func(t *testing.T) {
		var _ Client = (*client)(nil)
	})
}

// TestLogin tests the Login method.
func TestLogin(t *testing.T) {
	t.Run("successful login returns tokens", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "Login", &service.ExtendedLoginResponse{
			AccessToken:  "access-token-123",
			RefreshToken: "refresh-token-456",
			ExpiresAt:    time.Now().Add(time.Hour),
			TokenType:    "Bearer",
		})

		c := NewClient(mockT)
		resp, err := c.Login(context.Background(), &service.ExtendedLoginRequest{
			Username: "testuser",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.AccessToken != "access-token-123" {
			t.Errorf("expected access-token-123, got %s", resp.AccessToken)
		}
		if resp.RefreshToken != "refresh-token-456" {
			t.Errorf("expected refresh-token-456, got %s", resp.RefreshToken)
		}
		if resp.TokenType != "Bearer" {
			t.Errorf("expected Bearer, got %s", resp.TokenType)
		}
	})

	t.Run("login with MFA returns challenge", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "Login", &service.ExtendedLoginResponse{
			MFARequired: true,
			MFAChallenge: &service.MFAChallenge{
				ChallengeID: "challenge-123",
				Type:        service.MFATypeTOTP,
				ExpiresAt:   time.Now().Add(5 * time.Minute),
			},
		})

		c := NewClient(mockT)
		resp, err := c.Login(context.Background(), &service.ExtendedLoginRequest{
			Username: "testuser",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.MFARequired {
			t.Error("expected MFARequired to be true")
		}
		if resp.MFAChallenge == nil {
			t.Fatal("expected MFAChallenge to be non-nil")
		}
		if resp.MFAChallenge.Type != service.MFATypeTOTP {
			t.Errorf("expected TOTP, got %v", resp.MFAChallenge.Type)
		}
	})

	t.Run("nil request returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.Login(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty username returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.Login(context.Background(), &service.ExtendedLoginRequest{})
		if err == nil {
			t.Error("expected error for empty username")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.Login(nil, &service.ExtendedLoginRequest{ //nolint:staticcheck
			Username: "testuser",
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("transport error is propagated", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetError(errors.New("connection failed"))

		c := NewClient(mockT)
		_, err := c.Login(context.Background(), &service.ExtendedLoginRequest{
			Username: "testuser",
		})
		if err == nil {
			t.Error("expected error for transport failure")
		}
	})

	t.Run("nil response returns error", func(t *testing.T) {
		mockT := newMockTransport()
		// Response is nil by default

		c := NewClient(mockT)
		_, err := c.Login(context.Background(), &service.ExtendedLoginRequest{
			Username: "testuser",
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "Login", "invalid-type")

		c := NewClient(mockT)
		_, err := c.Login(context.Background(), &service.ExtendedLoginRequest{
			Username: "testuser",
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestRefreshToken tests the RefreshToken method.
func TestRefreshToken(t *testing.T) {
	t.Run("successful refresh returns new tokens", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "RefreshToken", &service.ExtendedRefreshTokenResponse{
			AccessToken:  "new-access-token",
			ExpiresAt:    time.Now().Add(time.Hour),
			TokenType:    "Bearer",
			RefreshToken: "new-refresh-token",
		})

		c := NewClient(mockT)
		resp, err := c.RefreshToken(context.Background(), "old-refresh-token")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.AccessToken != "new-access-token" {
			t.Errorf("expected new-access-token, got %s", resp.AccessToken)
		}
	})

	t.Run("empty refresh token returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.RefreshToken(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty refresh token")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.RefreshToken(nil, "token") //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.RefreshToken(context.Background(), "token")
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "RefreshToken", "invalid")

		c := NewClient(mockT)
		_, err := c.RefreshToken(context.Background(), "token")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestValidateToken tests the ValidateToken method.
func TestValidateToken(t *testing.T) {
	t.Run("valid token returns claims", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "ValidateToken", &service.TokenClaims{
			Valid:    true,
			UserID:   "user-123",
			Username: "testuser",
			Email:    "test@example.com",
			Roles:    []string{"admin", "user"},
		})

		c := NewClient(mockT)
		claims, err := c.ValidateToken(context.Background(), "access-token")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !claims.Valid {
			t.Error("expected token to be valid")
		}
		if claims.UserID != "user-123" {
			t.Errorf("expected user-123, got %s", claims.UserID)
		}
		if len(claims.Roles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(claims.Roles))
		}
	})

	t.Run("empty token returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.ValidateToken(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty token")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.ValidateToken(nil, "token") //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.ValidateToken(context.Background(), "token")
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "ValidateToken", "invalid")

		c := NewClient(mockT)
		_, err := c.ValidateToken(context.Background(), "token")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestLogout tests the Logout method.
func TestLogout(t *testing.T) {
	t.Run("successful logout", func(t *testing.T) {
		mockT := newMockTransport()
		// The auth handler returns nil on success, so we don't set a response.

		c := NewClient(mockT)
		err := c.Logout(context.Background(), "access-token")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("logout with nil response succeeds", func(t *testing.T) {
		c := NewClient(newMockTransport())
		err := c.Logout(context.Background(), "access-token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty token returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		err := c.Logout(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty token")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		err := c.Logout(nil, "token") //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("transport error is propagated", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetError(errors.New("transport error"))

		c := NewClient(mockT)
		err := c.Logout(context.Background(), "token")
		if err == nil {
			t.Error("expected transport error to be propagated")
		}
	})
}

// TestBeginFederatedLogin tests the BeginFederatedLogin method.
func TestBeginFederatedLogin(t *testing.T) {
	t.Run("successful begin federated login", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "BeginFederatedLogin", &service.BeginFederatedLoginResponse{
			AuthorizationURL: "https://idp.example.com/auth?client_id=xyz",
			State:            "state-123",
			Nonce:            "nonce-456",
		})

		c := NewClient(mockT)
		resp, err := c.BeginFederatedLogin(context.Background(), &service.BeginFederatedLoginRequest{
			TenantID:    "tenant-1",
			ProviderID:  "google",
			RedirectURI: "https://myapp.com/callback",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.AuthorizationURL == "" {
			t.Error("expected non-empty authorization URL")
		}
		if resp.State != "state-123" {
			t.Errorf("expected state-123, got %s", resp.State)
		}
	})

	t.Run("nil request returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.BeginFederatedLogin(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty provider ID returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.BeginFederatedLogin(context.Background(), &service.BeginFederatedLoginRequest{
			RedirectURI: "https://myapp.com/callback",
		})
		if err == nil {
			t.Error("expected error for empty provider ID")
		}
	})

	t.Run("empty redirect URI returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.BeginFederatedLogin(context.Background(), &service.BeginFederatedLoginRequest{
			ProviderID: "google",
		})
		if err == nil {
			t.Error("expected error for empty redirect URI")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.BeginFederatedLogin(nil, &service.BeginFederatedLoginRequest{ //nolint:staticcheck
			ProviderID:  "google",
			RedirectURI: "https://myapp.com/callback",
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.BeginFederatedLogin(context.Background(), &service.BeginFederatedLoginRequest{
			ProviderID:  "google",
			RedirectURI: "https://myapp.com/callback",
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "BeginFederatedLogin", "invalid")

		c := NewClient(mockT)
		_, err := c.BeginFederatedLogin(context.Background(), &service.BeginFederatedLoginRequest{
			ProviderID:  "google",
			RedirectURI: "https://myapp.com/callback",
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestHandleFederatedCallback tests the HandleFederatedCallback method.
func TestHandleFederatedCallback(t *testing.T) {
	t.Run("successful callback handling", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "HandleFederatedCallback", &service.HandleFederatedCallbackResponse{
			AccessToken:  "access-token",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(time.Hour),
			TokenType:    "Bearer",
			UserID:       "user-123",
			Username:     "testuser",
			Email:        "test@example.com",
			NewUser:      true,
		})

		c := NewClient(mockT)
		resp, err := c.HandleFederatedCallback(context.Background(), &service.HandleFederatedCallbackRequest{
			ProviderID:  "google",
			Code:        "auth-code",
			State:       "state-123",
			RedirectURI: "https://myapp.com/callback",
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.AccessToken != "access-token" {
			t.Errorf("expected access-token, got %s", resp.AccessToken)
		}
		if !resp.NewUser {
			t.Error("expected NewUser to be true")
		}
	})

	t.Run("nil request returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.HandleFederatedCallback(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty provider ID returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.HandleFederatedCallback(context.Background(), &service.HandleFederatedCallbackRequest{
			Code:  "auth-code",
			State: "state-123",
		})
		if err == nil {
			t.Error("expected error for empty provider ID")
		}
	})

	t.Run("empty code returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.HandleFederatedCallback(context.Background(), &service.HandleFederatedCallbackRequest{
			ProviderID: "google",
			State:      "state-123",
		})
		if err == nil {
			t.Error("expected error for empty code")
		}
	})

	t.Run("empty state returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.HandleFederatedCallback(context.Background(), &service.HandleFederatedCallbackRequest{
			ProviderID: "google",
			Code:       "auth-code",
		})
		if err == nil {
			t.Error("expected error for empty state")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.HandleFederatedCallback(nil, &service.HandleFederatedCallbackRequest{ //nolint:staticcheck
			ProviderID: "google",
			Code:       "auth-code",
			State:      "state-123",
		})
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.HandleFederatedCallback(context.Background(), &service.HandleFederatedCallbackRequest{
			ProviderID: "google",
			Code:       "auth-code",
			State:      "state-123",
		})
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "HandleFederatedCallback", "invalid")

		c := NewClient(mockT)
		_, err := c.HandleFederatedCallback(context.Background(), &service.HandleFederatedCallbackRequest{
			ProviderID: "google",
			Code:       "auth-code",
			State:      "state-123",
		})
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestListProviders tests the ListProviders method.
func TestListProviders(t *testing.T) {
	t.Run("successful list providers", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "ListProviders", &service.AuthListProvidersResponse{
			Providers: []service.AuthProvider{
				{ID: "google", Name: "Google", Type: service.ProviderTypeOIDC, Enabled: true},
				{ID: "okta", Name: "Okta", Type: service.ProviderTypeOIDC, Enabled: true},
			},
		})

		c := NewClient(mockT)
		resp, err := c.ListProviders(context.Background(), "tenant-1")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Providers) != 2 {
			t.Errorf("expected 2 providers, got %d", len(resp.Providers))
		}
	})

	t.Run("nil response returns empty list", func(t *testing.T) {
		c := NewClient(newMockTransport())
		resp, err := c.ListProviders(context.Background(), "tenant-1")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Providers == nil {
			t.Error("expected non-nil providers list")
		}
		if len(resp.Providers) != 0 {
			t.Errorf("expected 0 providers, got %d", len(resp.Providers))
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.ListProviders(nil, "tenant-1") //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "ListProviders", "invalid")

		c := NewClient(mockT)
		_, err := c.ListProviders(context.Background(), "tenant-1")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestGetProvider tests the GetProvider method.
func TestGetProvider(t *testing.T) {
	t.Run("successful get provider", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "GetProvider", &service.AuthProvider{
			ID:            "google",
			Name:          "Google",
			Type:          service.ProviderTypeOIDC,
			Enabled:       true,
			IssuerURL:     "https://accounts.google.com",
			AutoProvision: true,
		})

		c := NewClient(mockT)
		provider, err := c.GetProvider(context.Background(), "tenant-1", "google")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider.ID != "google" {
			t.Errorf("expected google, got %s", provider.ID)
		}
		if provider.Type != service.ProviderTypeOIDC {
			t.Errorf("expected OIDC, got %v", provider.Type)
		}
	})

	t.Run("empty provider ID returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetProvider(context.Background(), "tenant-1", "")
		if err == nil {
			t.Error("expected error for empty provider ID")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetProvider(nil, "tenant-1", "google") //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns not found error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetProvider(context.Background(), "tenant-1", "google")
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "GetProvider", "invalid")

		c := NewClient(mockT)
		_, err := c.GetProvider(context.Background(), "tenant-1", "google")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestGetSession tests the GetSession method.
func TestGetSession(t *testing.T) {
	t.Run("successful get session", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "GetSession", &service.AuthSession{
			ID:          "session-123",
			UserID:      "user-456",
			TenantID:    "tenant-1",
			MFAVerified: true,
			IPAddress:   "192.168.1.1",
			UserAgent:   "Mozilla/5.0",
		})

		c := NewClient(mockT)
		session, err := c.GetSession(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if session.ID != "session-123" {
			t.Errorf("expected session-123, got %s", session.ID)
		}
		if !session.MFAVerified {
			t.Error("expected MFAVerified to be true")
		}
	})

	t.Run("empty session ID returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetSession(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetSession(nil, "session-123") //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns not found error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetSession(context.Background(), "session-123")
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "GetSession", "invalid")

		c := NewClient(mockT)
		_, err := c.GetSession(context.Background(), "session-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestListSessions tests the ListSessions method.
func TestListSessions(t *testing.T) {
	t.Run("successful list sessions", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "ListSessions", &service.AuthListSessionsResponse{
			Sessions: []service.AuthSession{
				{ID: "session-1", IPAddress: "192.168.1.1"},
				{ID: "session-2", IPAddress: "10.0.0.1"},
			},
		})

		c := NewClient(mockT)
		resp, err := c.ListSessions(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Sessions) != 2 {
			t.Errorf("expected 2 sessions, got %d", len(resp.Sessions))
		}
	})

	t.Run("nil response returns empty list", func(t *testing.T) {
		c := NewClient(newMockTransport())
		resp, err := c.ListSessions(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Sessions == nil {
			t.Error("expected non-nil sessions list")
		}
		if len(resp.Sessions) != 0 {
			t.Errorf("expected 0 sessions, got %d", len(resp.Sessions))
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.ListSessions(nil) //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "ListSessions", "invalid")

		c := NewClient(mockT)
		_, err := c.ListSessions(context.Background())
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestRevokeSession tests the RevokeSession method.
func TestRevokeSession(t *testing.T) {
	t.Run("successful revoke session", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "RevokeSession", &revokeSessionResponse{})

		c := NewClient(mockT)
		err := c.RevokeSession(context.Background(), "session-123")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil response succeeds", func(t *testing.T) {
		c := NewClient(newMockTransport())
		err := c.RevokeSession(context.Background(), "session-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty session ID returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		err := c.RevokeSession(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		err := c.RevokeSession(nil, "session-123") //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "RevokeSession", "invalid")

		c := NewClient(mockT)
		err := c.RevokeSession(context.Background(), "session-123")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestRevokeAllSessions tests the RevokeAllSessions method.
func TestRevokeAllSessions(t *testing.T) {
	t.Run("successful revoke all sessions", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "RevokeAllSessions", &revokeAllSessionsResponse{})

		c := NewClient(mockT)
		err := c.RevokeAllSessions(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil response succeeds", func(t *testing.T) {
		c := NewClient(newMockTransport())
		err := c.RevokeAllSessions(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		err := c.RevokeAllSessions(nil) //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "RevokeAllSessions", "invalid")

		c := NewClient(mockT)
		err := c.RevokeAllSessions(context.Background())
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestIntrospectToken tests the IntrospectToken method.
func TestIntrospectToken(t *testing.T) {
	t.Run("successful introspect active token", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "IntrospectToken", &service.TokenIntrospection{
			Active:    true,
			Username:  "testuser",
			TokenType: "Bearer",
			Subject:   "user-123",
			Issuer:    "https://auth.example.com",
		})

		c := NewClient(mockT)
		intro, err := c.IntrospectToken(context.Background(), "access-token")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !intro.Active {
			t.Error("expected token to be active")
		}
		if intro.Username != "testuser" {
			t.Errorf("expected testuser, got %s", intro.Username)
		}
	})

	t.Run("introspect inactive token", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "IntrospectToken", &service.TokenIntrospection{
			Active: false,
		})

		c := NewClient(mockT)
		intro, err := c.IntrospectToken(context.Background(), "expired-token")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if intro.Active {
			t.Error("expected token to be inactive")
		}
	})

	t.Run("empty token returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.IntrospectToken(context.Background(), "")
		if err == nil {
			t.Error("expected error for empty token")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.IntrospectToken(nil, "token") //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.IntrospectToken(context.Background(), "token")
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "IntrospectToken", "invalid")

		c := NewClient(mockT)
		_, err := c.IntrospectToken(context.Background(), "token")
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestGetJWKS tests the GetJWKS method.
func TestGetJWKS(t *testing.T) {
	t.Run("successful get JWKS", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "GetJWKS", &service.JWKS{
			Keys: []service.JWK{
				{KTY: "RSA", KID: "key-1", ALG: "RS256", Use: "sig"},
				{KTY: "EC", KID: "key-2", ALG: "ES256", Use: "sig", CRV: "P-256"},
			},
		})

		c := NewClient(mockT)
		jwks, err := c.GetJWKS(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(jwks.Keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(jwks.Keys))
		}
		if jwks.Keys[0].KTY != "RSA" {
			t.Errorf("expected RSA, got %s", jwks.Keys[0].KTY)
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetJWKS(nil) //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetJWKS(context.Background())
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "GetJWKS", "invalid")

		c := NewClient(mockT)
		_, err := c.GetJWKS(context.Background())
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestGetOpenIDConfiguration tests the GetOpenIDConfiguration method.
func TestGetOpenIDConfiguration(t *testing.T) {
	t.Run("successful get OpenID configuration", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "GetOpenIDConfiguration", &service.OpenIDConfiguration{
			Issuer:                           "https://auth.example.com",
			AuthorizationEndpoint:            "https://auth.example.com/authorize",
			TokenEndpoint:                    "https://auth.example.com/token",
			UserInfoEndpoint:                 "https://auth.example.com/userinfo",
			JWKSURI:                          "https://auth.example.com/.well-known/jwks.json",
			ScopesSupported:                  []string{"openid", "profile", "email"},
			ResponseTypesSupported:           []string{"code", "token", "id_token"},
			GrantTypesSupported:              []string{"authorization_code", "refresh_token"},
			SubjectTypesSupported:            []string{"public"},
			IDTokenSigningAlgValuesSupported: []string{"RS256", "ES256"},
			ClaimsSupported:                  []string{"sub", "iss", "aud", "exp", "iat", "email", "name"},
			MFASupported:                     true,
			MFAMethodsSupported:              []string{"totp", "webauthn"},
		})

		c := NewClient(mockT)
		config, err := c.GetOpenIDConfiguration(context.Background())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if config.Issuer != "https://auth.example.com" {
			t.Errorf("expected https://auth.example.com, got %s", config.Issuer)
		}
		if config.AuthorizationEndpoint != "https://auth.example.com/authorize" {
			t.Errorf("expected https://auth.example.com/authorize, got %s", config.AuthorizationEndpoint)
		}
		if config.TokenEndpoint != "https://auth.example.com/token" {
			t.Errorf("expected https://auth.example.com/token, got %s", config.TokenEndpoint)
		}
		if config.JWKSURI != "https://auth.example.com/.well-known/jwks.json" {
			t.Errorf("expected https://auth.example.com/.well-known/jwks.json, got %s", config.JWKSURI)
		}
		if len(config.ScopesSupported) != 3 {
			t.Errorf("expected 3 scopes, got %d", len(config.ScopesSupported))
		}
		if !config.MFASupported {
			t.Error("expected MFASupported to be true")
		}
	})

	t.Run("nil context returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetOpenIDConfiguration(nil) //nolint:staticcheck
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil response returns error", func(t *testing.T) {
		c := NewClient(newMockTransport())
		_, err := c.GetOpenIDConfiguration(context.Background())
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type returns error", func(t *testing.T) {
		mockT := newMockTransport()
		mockT.SetResponse(serviceName, "GetOpenIDConfiguration", "invalid")

		c := NewClient(mockT)
		_, err := c.GetOpenIDConfiguration(context.Background())
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})
}

// TestMFAType_String tests the MFAType String method.
func TestMFAType_String(t *testing.T) {
	tests := []struct {
		mfaType  service.MFAType
		expected string
	}{
		{service.MFATypeTOTP, "TOTP"},
		{service.MFATypeWebAuthn, "WEBAUTHN"},
		{service.MFATypeSMS, "SMS"},
		{service.MFATypeEmail, "EMAIL"},
		{service.MFATypeBackupCode, "BACKUP_CODE"},
		{service.MFAType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.mfaType.String(); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

// TestProviderType_String tests the ProviderType String method.
func TestProviderType_String(t *testing.T) {
	tests := []struct {
		providerType service.ProviderType
		expected     string
	}{
		{service.ProviderTypeUnspecified, "UNSPECIFIED"},
		{service.ProviderTypeOIDC, "OIDC"},
		{service.ProviderTypeSAML, "SAML"},
		{service.ProviderTypeOAuth2, "OAUTH2"},
		{service.ProviderType(99), "UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.providerType.String(); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

// TestTransportError tests that transport errors are propagated.
func TestTransportError(t *testing.T) {
	methods := []struct {
		name string
		call func(c Client) error
	}{
		{"Login", func(c Client) error {
			_, err := c.Login(context.Background(), &service.ExtendedLoginRequest{Username: "u"})
			return err
		}},
		{"RefreshToken", func(c Client) error {
			_, err := c.RefreshToken(context.Background(), "token")
			return err
		}},
		{"ValidateToken", func(c Client) error {
			_, err := c.ValidateToken(context.Background(), "token")
			return err
		}},
		{"Logout", func(c Client) error {
			return c.Logout(context.Background(), "token")
		}},
		{"BeginFederatedLogin", func(c Client) error {
			_, err := c.BeginFederatedLogin(context.Background(), &service.BeginFederatedLoginRequest{
				ProviderID: "p", RedirectURI: "r",
			})
			return err
		}},
		{"HandleFederatedCallback", func(c Client) error {
			_, err := c.HandleFederatedCallback(context.Background(), &service.HandleFederatedCallbackRequest{
				ProviderID: "p", Code: "c", State: "s",
			})
			return err
		}},
		{"ListProviders", func(c Client) error {
			_, err := c.ListProviders(context.Background(), "t")
			return err
		}},
		{"GetProvider", func(c Client) error {
			_, err := c.GetProvider(context.Background(), "t", "p")
			return err
		}},
		{"GetSession", func(c Client) error {
			_, err := c.GetSession(context.Background(), "s")
			return err
		}},
		{"ListSessions", func(c Client) error {
			_, err := c.ListSessions(context.Background())
			return err
		}},
		{"RevokeSession", func(c Client) error {
			return c.RevokeSession(context.Background(), "s")
		}},
		{"RevokeAllSessions", func(c Client) error {
			return c.RevokeAllSessions(context.Background())
		}},
		{"IntrospectToken", func(c Client) error {
			_, err := c.IntrospectToken(context.Background(), "token")
			return err
		}},
		{"GetJWKS", func(c Client) error {
			_, err := c.GetJWKS(context.Background())
			return err
		}},
		{"GetOpenIDConfiguration", func(c Client) error {
			_, err := c.GetOpenIDConfiguration(context.Background())
			return err
		}},
	}

	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			mockT := newMockTransport()
			mockT.SetError(errors.New("transport error"))

			c := NewClient(mockT)
			err := m.call(c)
			if err == nil {
				t.Error("expected transport error to be propagated")
			}
		})
	}
}
