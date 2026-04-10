//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// MockOIDCClaims implements interfaces.OIDCClaims for testing.
type MockOIDCClaims struct {
	Subject     string
	Issuer      string
	Audience    []string
	TenantID    string
	Roles       []string
	Groups      []string
	Email       string
	Name        string
	Scope       string
	ClientID    string
	MFAVerified bool
}

func (m *MockOIDCClaims) GetSubject() string    { return m.Subject }
func (m *MockOIDCClaims) GetIssuer() string     { return m.Issuer }
func (m *MockOIDCClaims) GetAudience() []string { return m.Audience }
func (m *MockOIDCClaims) GetTenantID() string   { return m.TenantID }
func (m *MockOIDCClaims) GetRoles() []string    { return m.Roles }
func (m *MockOIDCClaims) GetGroups() []string   { return m.Groups }
func (m *MockOIDCClaims) GetEmail() string      { return m.Email }
func (m *MockOIDCClaims) GetName() string       { return m.Name }
func (m *MockOIDCClaims) GetScope() string      { return m.Scope }
func (m *MockOIDCClaims) GetClientID() string   { return m.ClientID }
func (m *MockOIDCClaims) IsMFAVerified() bool   { return m.MFAVerified }

// MockOAuthASMetadata implements interfaces.OAuthASMetadata for testing.
type MockOAuthASMetadata struct {
	Issuer                        string
	AuthorizationEndpoint         string
	TokenEndpoint                 string
	JWKSURI                       string
	RegistrationEndpoint          string
	RevocationEndpoint            string
	ResponseTypesSupported        []string
	GrantTypesSupported           []string
	ScopesSupported               []string
	CodeChallengeMethodsSupported []string
	ResourceIndicatorsSupported   bool
}

func (m *MockOAuthASMetadata) GetIssuer() string                   { return m.Issuer }
func (m *MockOAuthASMetadata) GetAuthorizationEndpoint() string    { return m.AuthorizationEndpoint }
func (m *MockOAuthASMetadata) GetTokenEndpoint() string            { return m.TokenEndpoint }
func (m *MockOAuthASMetadata) GetJWKSURI() string                  { return m.JWKSURI }
func (m *MockOAuthASMetadata) GetRegistrationEndpoint() string     { return m.RegistrationEndpoint }
func (m *MockOAuthASMetadata) GetRevocationEndpoint() string       { return m.RevocationEndpoint }
func (m *MockOAuthASMetadata) GetResponseTypesSupported() []string { return m.ResponseTypesSupported }
func (m *MockOAuthASMetadata) GetGrantTypesSupported() []string    { return m.GrantTypesSupported }
func (m *MockOAuthASMetadata) GetScopesSupported() []string        { return m.ScopesSupported }
func (m *MockOAuthASMetadata) GetCodeChallengeMethodsSupported() []string {
	return m.CodeChallengeMethodsSupported
}
func (m *MockOAuthASMetadata) IsResourceIndicatorsSupported() bool {
	return m.ResourceIndicatorsSupported
}

// MockOIDCProvider implements interfaces.OIDCProvider for testing.
type MockOIDCProvider struct {
	mock.Mock
}

func (m *MockOIDCProvider) ValidateAccessToken(ctx context.Context, tokenString string) (interfaces.OIDCClaims, error) {
	args := m.Called(ctx, tokenString)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(interfaces.OIDCClaims), args.Error(1)
}

func (m *MockOIDCProvider) GetConfiguration() map[string]interface{} {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(map[string]interface{})
}

func (m *MockOIDCProvider) GetOAuthASMetadata() interfaces.OAuthASMetadata {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(interfaces.OAuthASMetadata)
}

// =============================================================================
// OIDC Provider Integration Tests
// =============================================================================

func TestAuthorizationChecker_WithOIDCProvider_ValidToken(t *testing.T) {
	mockOIDCProvider := new(MockOIDCProvider)
	mockAuthService := new(MockAuthorizationService)

	claims := &MockOIDCClaims{
		Subject:  "user-123",
		Name:     "Test User",
		Roles:    []string{"admin"},
		Audience: []string{"https://api.example.com/mcp"},
	}

	// OIDC provider validates token successfully
	mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "valid-oidc-token").
		Return(claims, nil)

	// Authorization allowed
	mockAuthService.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return req.Subject.ID == "user-123" &&
			req.Subject.Username == "Test User" &&
			req.Resource == interfaces.ResourceKeys &&
			req.Action == interfaces.ActionRead
	})).Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	checker := NewAuthorizationChecker(mockAuthService, nil)
	checker.SetOIDCProvider(mockOIDCProvider)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "valid-oidc-token")

	assert.Nil(t, err)
	mockOIDCProvider.AssertExpectations(t)
	mockAuthService.AssertExpectations(t)
}

func TestAuthorizationChecker_WithOIDCProvider_InvalidToken(t *testing.T) {
	mockOIDCProvider := new(MockOIDCProvider)
	mockAuthManager := new(MockAuthManager)

	// OIDC provider rejects token
	mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "invalid-token").
		Return(nil, errors.New("token expired"))

	// Fall back to AuthManager which also rejects
	mockAuthManager.On("ValidateToken", mock.Anything, "invalid-token").
		Return(nil, errors.New("invalid token"))

	checker := NewAuthorizationChecker(nil, mockAuthManager)
	checker.SetOIDCProvider(mockOIDCProvider)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "invalid-token")

	require.NotNil(t, err)
	assert.Equal(t, AuthErrorInvalidToken, err.Type)
	mockOIDCProvider.AssertExpectations(t)
	mockAuthManager.AssertExpectations(t)
}

func TestAuthorizationChecker_WithOIDCProvider_FallbackToAuthManager(t *testing.T) {
	mockOIDCProvider := new(MockOIDCProvider)
	mockAuthManager := new(MockAuthManager)
	mockAuthService := new(MockAuthorizationService)

	// OIDC provider fails to validate
	mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "legacy-token").
		Return(nil, errors.New("unknown token format"))

	// AuthManager validates successfully
	mockAuthManager.On("ValidateToken", mock.Anything, "legacy-token").
		Return(&MockTokenValidationResult{
			Valid:    true,
			UserID:   "user-456",
			Username: "legacy-user",
			Roles:    []string{"reader"},
		}, nil)

	// Authorization allowed
	mockAuthService.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return req.Subject.ID == "user-456" &&
			req.Subject.Username == "legacy-user"
	})).Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)
	checker.SetOIDCProvider(mockOIDCProvider)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "legacy-token")

	assert.Nil(t, err)
	mockOIDCProvider.AssertExpectations(t)
	mockAuthManager.AssertExpectations(t)
	mockAuthService.AssertExpectations(t)
}

func TestAuthorizationChecker_WithOIDCProvider_AudienceValidation(t *testing.T) {
	mockOIDCProvider := new(MockOIDCProvider)
	mockAuthService := new(MockAuthorizationService)

	resourceURI := "https://api.example.com/mcp"

	claims := &MockOIDCClaims{
		Subject:  "user-123",
		Name:     "Test User",
		Roles:    []string{"admin"},
		Audience: []string{"https://other.example.com"}, // Wrong audience
	}

	mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "wrong-audience-token").
		Return(claims, nil)

	checker := NewAuthorizationChecker(mockAuthService, nil)
	checker.SetOIDCProvider(mockOIDCProvider)
	checker.SetMCPAuthConfig(&MCPAuthConfig{
		Enabled:     true,
		ResourceURI: resourceURI,
	})

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "wrong-audience-token")

	require.NotNil(t, err)
	assert.Equal(t, AuthErrorInvalidToken, err.Type)
	assert.Contains(t, err.Message, "audience")
	mockOIDCProvider.AssertExpectations(t)
}

func TestAuthorizationChecker_WithOIDCProvider_CorrectAudience(t *testing.T) {
	mockOIDCProvider := new(MockOIDCProvider)
	mockAuthService := new(MockAuthorizationService)

	resourceURI := "https://api.example.com/mcp"

	claims := &MockOIDCClaims{
		Subject:  "user-123",
		Name:     "Test User",
		Roles:    []string{"admin"},
		Audience: []string{"https://issuer.example.com", resourceURI},
	}

	mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "correct-audience-token").
		Return(claims, nil)

	mockAuthService.On("Authorize", mock.Anything, mock.Anything).
		Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	checker := NewAuthorizationChecker(mockAuthService, nil)
	checker.SetOIDCProvider(mockOIDCProvider)
	checker.SetMCPAuthConfig(&MCPAuthConfig{
		Enabled:     true,
		ResourceURI: resourceURI,
	})

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "correct-audience-token")

	assert.Nil(t, err)
	mockOIDCProvider.AssertExpectations(t)
	mockAuthService.AssertExpectations(t)
}

func TestAuthorizationChecker_OIDCOnly_NoAuthService(t *testing.T) {
	mockOIDCProvider := new(MockOIDCProvider)

	claims := &MockOIDCClaims{
		Subject:  "user-123",
		Name:     "Test User",
		Roles:    []string{"admin"},
		Audience: []string{"https://api.example.com/mcp"},
	}

	mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "valid-token").
		Return(claims, nil)

	// No AuthService, but OIDC provider is configured
	checker := NewAuthorizationChecker(nil, nil)
	checker.SetOIDCProvider(mockOIDCProvider)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "valid-token")

	// Should succeed - token is valid and no authorization service to check permissions
	assert.Nil(t, err)
	mockOIDCProvider.AssertExpectations(t)
}

func TestAuthorizationChecker_SetOIDCProvider(t *testing.T) {
	mockOIDCProvider := new(MockOIDCProvider)

	checker := NewAuthorizationChecker(nil, nil)
	assert.Nil(t, checker.GetOIDCProvider())

	checker.SetOIDCProvider(mockOIDCProvider)
	assert.Equal(t, mockOIDCProvider, checker.GetOIDCProvider())

	checker.SetOIDCProvider(nil)
	assert.Nil(t, checker.GetOIDCProvider())
}

func TestAuthorizationChecker_ValidateWithOIDCProvider(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mockOIDCProvider := new(MockOIDCProvider)
		claims := &MockOIDCClaims{
			Subject: "user-123",
			Roles:   []string{"admin"},
		}

		mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "valid-token").
			Return(claims, nil)

		checker := NewAuthorizationChecker(nil, nil)
		checker.SetOIDCProvider(mockOIDCProvider)

		result, err := checker.ValidateWithOIDCProvider(context.Background(), "valid-token")

		require.Nil(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "user-123", result.GetSubject())
		mockOIDCProvider.AssertExpectations(t)
	})

	t.Run("no provider configured", func(t *testing.T) {
		checker := NewAuthorizationChecker(nil, nil)

		_, err := checker.ValidateWithOIDCProvider(context.Background(), "any-token")

		require.NotNil(t, err)
		var validationErr *OIDCProviderTokenValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Contains(t, validationErr.Message, "not configured")
	})

	t.Run("validation failure", func(t *testing.T) {
		mockOIDCProvider := new(MockOIDCProvider)
		mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "invalid-token").
			Return(nil, errors.New("token expired"))

		checker := NewAuthorizationChecker(nil, nil)
		checker.SetOIDCProvider(mockOIDCProvider)

		_, err := checker.ValidateWithOIDCProvider(context.Background(), "invalid-token")

		require.NotNil(t, err)
		var validationErr *OIDCProviderTokenValidationError
		assert.True(t, errors.As(err, &validationErr))
		assert.Contains(t, validationErr.Message, "validation failed")
		mockOIDCProvider.AssertExpectations(t)
	})
}

func TestValidateTokenAudienceFromClaims(t *testing.T) {
	resourceURI := "https://api.example.com/mcp"

	tests := []struct {
		name      string
		config    *MCPAuthConfig
		claims    interfaces.OIDCClaims
		wantError bool
	}{
		{
			name:   "no config - skip validation",
			config: nil,
			claims: &MockOIDCClaims{
				Audience: []string{"https://other.example.com"},
			},
			wantError: false,
		},
		{
			name:   "config disabled - skip validation",
			config: &MCPAuthConfig{Enabled: false, ResourceURI: resourceURI},
			claims: &MockOIDCClaims{
				Audience: []string{"https://other.example.com"},
			},
			wantError: false,
		},
		{
			name:   "no resource URI - skip validation",
			config: &MCPAuthConfig{Enabled: true, ResourceURI: ""},
			claims: &MockOIDCClaims{
				Audience: []string{"https://other.example.com"},
			},
			wantError: false,
		},
		{
			name:   "audience matches",
			config: &MCPAuthConfig{Enabled: true, ResourceURI: resourceURI},
			claims: &MockOIDCClaims{
				Audience: []string{"https://issuer.example.com", resourceURI},
			},
			wantError: false,
		},
		{
			name:   "audience does not match",
			config: &MCPAuthConfig{Enabled: true, ResourceURI: resourceURI},
			claims: &MockOIDCClaims{
				Audience: []string{"https://other.example.com"},
			},
			wantError: true,
		},
		{
			name:   "empty audience",
			config: &MCPAuthConfig{Enabled: true, ResourceURI: resourceURI},
			claims: &MockOIDCClaims{
				Audience: []string{},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewAuthorizationChecker(nil, nil)
			checker.SetMCPAuthConfig(tt.config)

			err := checker.validateTokenAudienceFromClaims(tt.claims)

			if tt.wantError {
				require.NotNil(t, err)
				assert.Equal(t, AuthErrorInvalidToken, err.Type)
			} else {
				assert.Nil(t, err)
			}
		})
	}
}

// =============================================================================
// MCPAuthConfig Helper Tests
// =============================================================================

func TestMCPAuthConfigFromOIDCProvider(t *testing.T) {
	t.Run("with valid provider", func(t *testing.T) {
		mockOIDCProvider := new(MockOIDCProvider)
		metadata := &MockOAuthASMetadata{
			Issuer:                "https://auth.example.com",
			AuthorizationEndpoint: "https://auth.example.com/authorize",
			TokenEndpoint:         "https://auth.example.com/token",
			JWKSURI:               "https://auth.example.com/.well-known/jwks.json",
			RegistrationEndpoint:  "https://auth.example.com/register",
			RevocationEndpoint:    "https://auth.example.com/revoke",
			ScopesSupported:       []string{"openid", "profile", "mcp:tools"},
		}

		mockOIDCProvider.On("GetOAuthASMetadata").Return(metadata)

		config := MCPAuthConfigFromOIDCProvider(mockOIDCProvider, "https://api.example.com/mcp")

		require.NotNil(t, config)
		assert.True(t, config.Enabled)
		assert.Equal(t, "https://api.example.com/mcp", config.ResourceURI)
		assert.Equal(t, "https://auth.example.com", config.Issuer)
		assert.Equal(t, "https://auth.example.com/authorize", config.AuthorizationEndpoint)
		assert.Equal(t, "https://auth.example.com/token", config.TokenEndpoint)
		assert.Equal(t, "https://auth.example.com/.well-known/jwks.json", config.JwksURI)
		assert.Equal(t, []string{"https://auth.example.com"}, config.AuthorizationServers)
		mockOIDCProvider.AssertExpectations(t)
	})

	t.Run("with nil provider", func(t *testing.T) {
		config := MCPAuthConfigFromOIDCProvider(nil, "https://api.example.com/mcp")
		assert.Nil(t, config)
	})

	t.Run("with nil metadata", func(t *testing.T) {
		mockOIDCProvider := new(MockOIDCProvider)
		mockOIDCProvider.On("GetOAuthASMetadata").Return(nil)

		config := MCPAuthConfigFromOIDCProvider(mockOIDCProvider, "https://api.example.com/mcp")
		assert.Nil(t, config)
		mockOIDCProvider.AssertExpectations(t)
	})
}

// =============================================================================
// Scope Helper Tests
// =============================================================================

func TestExtractScopesFromClaims(t *testing.T) {
	tests := []struct {
		name     string
		scope    string
		expected []string
	}{
		{
			name:     "multiple scopes",
			scope:    "openid profile email mcp:tools",
			expected: []string{"openid", "profile", "email", "mcp:tools"},
		},
		{
			name:     "single scope",
			scope:    "openid",
			expected: []string{"openid"},
		},
		{
			name:     "empty scope",
			scope:    "",
			expected: nil,
		},
		{
			name:     "scopes with extra whitespace",
			scope:    "openid  profile   email",
			expected: []string{"openid", "profile", "email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &MockOIDCClaims{Scope: tt.scope}
			result := ExtractScopesFromClaims(claims)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasScope(t *testing.T) {
	claims := &MockOIDCClaims{Scope: "openid profile mcp:tools mcp:resources"}

	assert.True(t, HasScope(claims, "openid"))
	assert.True(t, HasScope(claims, "mcp:tools"))
	assert.False(t, HasScope(claims, "mcp:admin"))
	assert.False(t, HasScope(claims, ""))
}

func TestHasAnyScope(t *testing.T) {
	claims := &MockOIDCClaims{Scope: "openid profile mcp:tools"}

	assert.True(t, HasAnyScope(claims, "mcp:admin", "mcp:tools"))
	assert.True(t, HasAnyScope(claims, "openid"))
	assert.False(t, HasAnyScope(claims, "mcp:admin", "mcp:resources"))
	assert.False(t, HasAnyScope(claims))
}

func TestHasAllScopes(t *testing.T) {
	claims := &MockOIDCClaims{Scope: "openid profile mcp:tools mcp:resources"}

	assert.True(t, HasAllScopes(claims, "openid", "profile"))
	assert.True(t, HasAllScopes(claims, "mcp:tools", "mcp:resources"))
	assert.True(t, HasAllScopes(claims)) // Empty requirement
	assert.False(t, HasAllScopes(claims, "openid", "mcp:admin"))
	assert.False(t, HasAllScopes(claims, "nonexistent"))
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestOIDCProviderTokenValidationError(t *testing.T) {
	t.Run("error with cause", func(t *testing.T) {
		cause := errors.New("token expired")
		err := &OIDCProviderTokenValidationError{
			Message: "token validation failed",
			Cause:   cause,
		}

		assert.Equal(t, "token validation failed: token expired", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("error without cause", func(t *testing.T) {
		err := &OIDCProviderTokenValidationError{
			Message: "OIDC provider not configured",
		}

		assert.Equal(t, "OIDC provider not configured", err.Error())
		assert.Nil(t, err.Unwrap())
	})
}

// =============================================================================
// Server Integration Tests
// =============================================================================

func TestServer_SetOIDCProvider(t *testing.T) {
	server := NewServer(nil, DefaultConfig(), nil)
	mockOIDCProvider := new(MockOIDCProvider)

	assert.Nil(t, server.GetOIDCProvider())

	server.SetOIDCProvider(mockOIDCProvider)
	assert.Equal(t, mockOIDCProvider, server.GetOIDCProvider())
}

func TestServer_SetOIDCProvider_DeriveConfig(t *testing.T) {
	config := DefaultConfig()
	config.MCPAuth = &MCPAuthConfig{
		Enabled:     true,
		ResourceURI: "https://api.example.com/mcp",
	}

	server := NewServer(nil, config, nil)

	mockOIDCProvider := new(MockOIDCProvider)
	metadata := &MockOAuthASMetadata{
		Issuer:                "https://auth.example.com",
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
		JWKSURI:               "https://auth.example.com/.well-known/jwks.json",
		ScopesSupported:       []string{"openid", "mcp:tools"},
	}

	mockOIDCProvider.On("GetOAuthASMetadata").Return(metadata)

	server.SetOIDCProvider(mockOIDCProvider)

	// Verify config was derived from OIDC metadata
	assert.Equal(t, "https://auth.example.com", server.config.MCPAuth.Issuer)
	assert.Equal(t, "https://auth.example.com/authorize", server.config.MCPAuth.AuthorizationEndpoint)
	assert.Equal(t, "https://auth.example.com/token", server.config.MCPAuth.TokenEndpoint)
	mockOIDCProvider.AssertExpectations(t)
}

func TestServer_SetOIDCProvider_PreserveExistingConfig(t *testing.T) {
	config := DefaultConfig()
	config.MCPAuth = &MCPAuthConfig{
		Enabled:               true,
		ResourceURI:           "https://api.example.com/mcp",
		Issuer:                "https://custom-issuer.example.com",
		AuthorizationEndpoint: "https://custom-auth.example.com/authorize",
	}

	server := NewServer(nil, config, nil)

	mockOIDCProvider := new(MockOIDCProvider)
	metadata := &MockOAuthASMetadata{
		Issuer:                "https://auth.example.com",
		AuthorizationEndpoint: "https://auth.example.com/authorize",
		TokenEndpoint:         "https://auth.example.com/token",
	}

	mockOIDCProvider.On("GetOAuthASMetadata").Return(metadata)

	server.SetOIDCProvider(mockOIDCProvider)

	// Existing values should be preserved
	assert.Equal(t, "https://custom-issuer.example.com", server.config.MCPAuth.Issuer)
	assert.Equal(t, "https://custom-auth.example.com/authorize", server.config.MCPAuth.AuthorizationEndpoint)
	// Only unset values should be derived
	assert.Equal(t, "https://auth.example.com/token", server.config.MCPAuth.TokenEndpoint)
	mockOIDCProvider.AssertExpectations(t)
}

// =============================================================================
// Federation Support Tests
// =============================================================================

func TestAuthorizationChecker_FederatedToken(t *testing.T) {
	mockOIDCProvider := new(MockOIDCProvider)
	mockAuthService := new(MockAuthorizationService)

	// Simulates a federated token from an external IdP
	federatedClaims := &MockOIDCClaims{
		Subject:  "federated-user-789",
		Issuer:   "https://external-idp.example.com",
		Name:     "Federated User",
		Email:    "federated@external.com",
		Roles:    []string{"external-reader"},
		Audience: []string{"https://api.example.com/mcp"},
		TenantID: "tenant-abc",
	}

	mockOIDCProvider.On("ValidateAccessToken", mock.Anything, "federated-token").
		Return(federatedClaims, nil)

	mockAuthService.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return req.Subject.ID == "federated-user-789" &&
			req.Subject.Username == "Federated User" &&
			len(req.Subject.Roles) == 1 &&
			req.Subject.Roles[0] == "external-reader"
	})).Return(&interfaces.AuthorizationDecision{Allowed: true}, nil)

	checker := NewAuthorizationChecker(mockAuthService, nil)
	checker.SetOIDCProvider(mockOIDCProvider)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "federated-token")

	assert.Nil(t, err)
	mockOIDCProvider.AssertExpectations(t)
	mockAuthService.AssertExpectations(t)
}
