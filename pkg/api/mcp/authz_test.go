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
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// MockAuthorizationService is a mock implementation of interfaces.AuthorizationService.
type MockAuthorizationService struct {
	mock.Mock
}

func (m *MockAuthorizationService) Authorize(ctx context.Context, req *interfaces.AuthorizationRequest) (*interfaces.AuthorizationDecision, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*interfaces.AuthorizationDecision), args.Error(1)
}

func (m *MockAuthorizationService) BatchAuthorize(ctx context.Context, reqs []*interfaces.AuthorizationRequest) ([]*interfaces.AuthorizationDecision, error) {
	args := m.Called(ctx, reqs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*interfaces.AuthorizationDecision), args.Error(1)
}

func (m *MockAuthorizationService) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockAuthorizationService) GetProvider() interfaces.AuthorizationProvider {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(interfaces.AuthorizationProvider)
}

func (m *MockAuthorizationService) GetProviderType() string {
	args := m.Called()
	return args.String(0)
}

// MockAuthManager is a mock implementation of interfaces.AuthManager.
type MockAuthManager struct {
	mock.Mock
}

func (m *MockAuthManager) Authenticate(ctx context.Context, username, password string) (interface{}, error) {
	args := m.Called(ctx, username, password)
	return args.Get(0), args.Error(1)
}

func (m *MockAuthManager) IssueTokenForUser(ctx context.Context, user interfaces.UserInfo) (interface{}, error) {
	args := m.Called(ctx, user)
	return args.Get(0), args.Error(1)
}

func (m *MockAuthManager) Authorize(ctx context.Context, token, resource, action string) error {
	args := m.Called(ctx, token, resource, action)
	return args.Error(0)
}

func (m *MockAuthManager) RefreshToken(ctx context.Context, refreshToken string) (interface{}, error) {
	args := m.Called(ctx, refreshToken)
	return args.Get(0), args.Error(1)
}

func (m *MockAuthManager) RevokeToken(ctx context.Context, token string) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *MockAuthManager) ValidateToken(ctx context.Context, token string) (interface{}, error) {
	args := m.Called(ctx, token)
	return args.Get(0), args.Error(1)
}

func (m *MockAuthManager) CreateUser(username, password string, roles []string) (interface{}, error) {
	args := m.Called(username, password, roles)
	return args.Get(0), args.Error(1)
}

func (m *MockAuthManager) GetUserStore() interfaces.UserStore {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(interfaces.UserStore)
}

func (m *MockAuthManager) GetRoleManager() interfaces.RoleManager {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(interfaces.RoleManager)
}

// MockTokenValidationResult implements the token validation result interface.
type MockTokenValidationResult struct {
	Valid    bool
	UserID   string
	Username string
	Roles    []string
}

func (r *MockTokenValidationResult) GetValid() bool      { return r.Valid }
func (r *MockTokenValidationResult) GetUserID() string   { return r.UserID }
func (r *MockTokenValidationResult) GetUsername() string { return r.Username }
func (r *MockTokenValidationResult) GetRoles() []string  { return r.Roles }

func TestIsPublicTool(t *testing.T) {
	testCases := []struct {
		name     string
		toolName string
		expected bool
	}{
		{"health tool is public", "quicbaas_health", true},
		{"login tool is public", "quicbaas_login", true},
		{"version tool is public", "quicbaas_version", true},
		{"liveness tool is public", "quicbaas_liveness", true},
		{"get tool is not public", "quicbaas_get", false},
		{"put tool is not public", "quicbaas_put", false},
		{"delete tool is not public", "quicbaas_delete", false},
		{"cluster_status is not public", "quicbaas_cluster_status", false},
		{"unknown tool is not public", "unknown_tool", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsPublicTool(tc.toolName)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestGetToolResourceAction(t *testing.T) {
	testCases := []struct {
		name             string
		toolName         string
		expectedResource string
		expectedAction   string
	}{
		// KV Read Operations
		{"get tool", "quicbaas_get", interfaces.ResourceKeys, interfaces.ActionRead},
		{"list tool", "quicbaas_list", interfaces.ResourceKeys, interfaces.ActionRead},
		{"query tool", "quicbaas_query", interfaces.ResourceKeys, interfaces.ActionRead},
		{"batch_get tool", "quicbaas_batch_get", interfaces.ResourceKeys, interfaces.ActionRead},

		// KV Write Operations
		{"put tool", "quicbaas_put", interfaces.ResourceKeys, interfaces.ActionWrite},
		{"batch_put tool", "quicbaas_batch_put", interfaces.ResourceKeys, interfaces.ActionWrite},

		// KV Delete Operations
		{"delete tool", "quicbaas_delete", interfaces.ResourceKeys, interfaces.ActionDelete},
		{"batch_delete tool", "quicbaas_batch_delete", interfaces.ResourceKeys, interfaces.ActionDelete},

		// Cluster Operations
		{"cluster_status tool", "quicbaas_cluster_status", interfaces.ResourceCluster, interfaces.ActionRead},
		{"add_node tool", "quicbaas_add_node", interfaces.ResourceCluster, interfaces.ActionAdmin},
		{"remove_node tool", "quicbaas_remove_node", interfaces.ResourceCluster, interfaces.ActionAdmin},

		// KMS Operations
		{"kms_get_key tool", "quicbaas_kms_get_key", interfaces.ResourceKMS, interfaces.ActionRead},
		{"kms_list_keys tool", "quicbaas_kms_list_keys", interfaces.ResourceKMS, interfaces.ActionRead},
		{"kms_create_key tool", "quicbaas_kms_create_key", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"kms_rotate_key tool", "quicbaas_kms_rotate_key", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"kms_delete_key tool", "quicbaas_kms_delete_key", interfaces.ResourceKMS, interfaces.ActionDelete},

		// Secrets Operations
		{"secrets_get tool", "quicbaas_secrets_get", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"secrets_list tool", "quicbaas_secrets_list", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"secrets_store tool", "quicbaas_secrets_store", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"secrets_delete tool", "quicbaas_secrets_delete", interfaces.ResourceSecrets, interfaces.ActionDelete},

		// Crypto Operations
		{"crypto_encrypt tool", "quicbaas_crypto_encrypt", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"crypto_decrypt tool", "quicbaas_crypto_decrypt", interfaces.ResourceCrypto, interfaces.ActionWrite},

		// Seal Operations
		{"seal tool", "quicbaas_seal", interfaces.ResourceSeal, interfaces.ActionAdmin},
		{"unseal tool", "quicbaas_unseal", interfaces.ResourceSeal, interfaces.ActionAdmin},

		// Vector Operations
		{"vector_search tool", "quicbaas_vector_search", interfaces.ResourceVector, interfaces.ActionRead},
		{"vector_upsert tool", "quicbaas_vector_upsert", interfaces.ResourceVector, interfaces.ActionWrite},
		{"vector_delete tool", "quicbaas_vector_delete", interfaces.ResourceVector, interfaces.ActionDelete},

		// Timeseries Operations
		{"timeseries_query tool", "quicbaas_timeseries_query", interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"timeseries_write tool", "quicbaas_timeseries_write", interfaces.ResourceTimeseries, interfaces.ActionWrite},

		// Unknown tool defaults to admin
		{"unknown tool", "unknown_tool", interfaces.ResourceCluster, interfaces.ActionAdmin},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resource, action := getToolResourceAction(tc.toolName)
			assert.Equal(t, tc.expectedResource, resource, "resource mismatch for %s", tc.toolName)
			assert.Equal(t, tc.expectedAction, action, "action mismatch for %s", tc.toolName)
		})
	}
}

func TestAuthorizationChecker_PublicToolBypass(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	// Public tools should bypass auth completely
	publicToolNames := []string{"quicbaas_health", "quicbaas_login", "quicbaas_version", "quicbaas_liveness"}

	for _, toolName := range publicToolNames {
		t.Run(toolName, func(t *testing.T) {
			err := checker.CheckToolAuthorization(context.Background(), toolName, "")
			assert.Nil(t, err, "public tool %s should not require auth", toolName)
		})
	}

	// Mocks should not be called for public tools
	mockAuthService.AssertNotCalled(t, "Authorize")
	mockAuthManager.AssertNotCalled(t, "ValidateToken")
}

func TestAuthorizationChecker_NoAuthServiceConfigured(t *testing.T) {
	// When no auth service is configured, all tools should be allowed
	checker := NewAuthorizationChecker(nil, nil)

	testTools := []string{"quicbaas_get", "quicbaas_put", "quicbaas_delete", "quicbaas_cluster_status"}

	for _, toolName := range testTools {
		t.Run(toolName, func(t *testing.T) {
			err := checker.CheckToolAuthorization(context.Background(), toolName, "")
			assert.Nil(t, err, "tool %s should be allowed when auth is disabled", toolName)
		})
	}
}

func TestAuthorizationChecker_OnlyAuthManagerConfigured(t *testing.T) {
	// When only authManager is configured (no authService, no oidcProvider),
	// non-public tools should still require authentication
	mockAuthManager := new(MockAuthManager)

	checker := NewAuthorizationChecker(nil, mockAuthManager)

	// Non-public tool without token should fail
	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "")
	require.NotNil(t, err)
	assert.Equal(t, AuthErrorMissingToken, err.Type)
	assert.Equal(t, "authentication required", err.Message)

	// Public tools should still bypass auth
	err = checker.CheckToolAuthorization(context.Background(), "quicbaas_login", "")
	assert.Nil(t, err, "quicbaas_login should be public even with authManager configured")

	// With a valid token, authManager should be called for validation
	mockAuthManager.On("ValidateToken", mock.Anything, "valid-token").
		Return(&MockTokenValidationResult{
			Valid:    true,
			UserID:   "user-123",
			Username: "testuser",
			Roles:    []string{"admin"},
		}, nil)

	// Non-public tool with valid token should be allowed (no authService to check permissions)
	err = checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "valid-token")
	assert.Nil(t, err, "tool should be allowed with valid token when only authManager is configured")
	mockAuthManager.AssertExpectations(t)
}

func TestAuthorizationChecker_MissingToken(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	// Non-public tool without token should fail
	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "")

	require.NotNil(t, err)
	assert.Equal(t, AuthErrorMissingToken, err.Type)
	assert.Equal(t, "authentication required", err.Message)
}

func TestAuthorizationChecker_InvalidToken(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	// Token validation returns error
	mockAuthManager.On("ValidateToken", mock.Anything, "invalid-token").
		Return(nil, assert.AnError)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "invalid-token")

	require.NotNil(t, err)
	assert.Equal(t, AuthErrorInvalidToken, err.Type)
	assert.Equal(t, "invalid token", err.Message)
	mockAuthManager.AssertExpectations(t)
}

func TestAuthorizationChecker_TokenNotValid(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	// Token validation returns result with Valid=false
	mockAuthManager.On("ValidateToken", mock.Anything, "expired-token").
		Return(&MockTokenValidationResult{Valid: false}, nil)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "expired-token")

	require.NotNil(t, err)
	assert.Equal(t, AuthErrorInvalidToken, err.Type)
	assert.Equal(t, "invalid token", err.Message)
	mockAuthManager.AssertExpectations(t)
}

func TestAuthorizationChecker_PermissionDenied(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	// Token is valid
	mockAuthManager.On("ValidateToken", mock.Anything, "valid-token").
		Return(&MockTokenValidationResult{
			Valid:    true,
			UserID:   "user-123",
			Username: "testuser",
			Roles:    []string{"reader"},
		}, nil)

	// Authorization denied
	mockAuthService.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return req.Resource == interfaces.ResourceKeys && req.Action == interfaces.ActionWrite
	})).Return(&interfaces.AuthorizationDecision{
		Allowed: false,
		Reason:  "user lacks write permission",
	}, nil)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_put", "valid-token")

	require.NotNil(t, err)
	assert.Equal(t, AuthErrorPermissionDenied, err.Type)
	assert.Contains(t, err.Message, "permission denied")
	assert.Contains(t, err.Message, "user lacks write permission")
	mockAuthManager.AssertExpectations(t)
	mockAuthService.AssertExpectations(t)
}

func TestAuthorizationChecker_AuthorizationError(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	// Token is valid
	mockAuthManager.On("ValidateToken", mock.Anything, "valid-token").
		Return(&MockTokenValidationResult{
			Valid:    true,
			UserID:   "user-123",
			Username: "testuser",
			Roles:    []string{"admin"},
		}, nil)

	// Authorization service returns error
	mockAuthService.On("Authorize", mock.Anything, mock.Anything).
		Return(nil, assert.AnError)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "valid-token")

	require.NotNil(t, err)
	assert.Equal(t, AuthErrorPermissionDenied, err.Type)
	assert.Contains(t, err.Message, "authorization check failed")
	mockAuthManager.AssertExpectations(t)
	mockAuthService.AssertExpectations(t)
}

func TestAuthorizationChecker_AllowedAccess(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	// Token is valid
	mockAuthManager.On("ValidateToken", mock.Anything, "valid-token").
		Return(&MockTokenValidationResult{
			Valid:    true,
			UserID:   "user-123",
			Username: "adminuser",
			Roles:    []string{"admin"},
		}, nil)

	// Authorization allowed
	mockAuthService.On("Authorize", mock.Anything, mock.MatchedBy(func(req *interfaces.AuthorizationRequest) bool {
		return req.Subject.Username == "adminuser" &&
			req.Resource == interfaces.ResourceKeys &&
			req.Action == interfaces.ActionWrite
	})).Return(&interfaces.AuthorizationDecision{
		Allowed: true,
	}, nil)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_put", "valid-token")

	assert.Nil(t, err)
	mockAuthManager.AssertExpectations(t)
	mockAuthService.AssertExpectations(t)
}

func TestExtractTokenInfo(t *testing.T) {
	t.Run("with MockTokenValidationResult", func(t *testing.T) {
		result := &MockTokenValidationResult{
			Valid:    true,
			UserID:   "user-456",
			Username: "testuser",
			Roles:    []string{"admin", "operator"},
		}

		valid, userID, username, roles := ExtractTokenInfo(result)

		assert.True(t, valid)
		assert.Equal(t, "user-456", userID)
		assert.Equal(t, "testuser", username)
		assert.Equal(t, []string{"admin", "operator"}, roles)
	})

	t.Run("with nil result", func(t *testing.T) {
		valid, userID, username, roles := ExtractTokenInfo(nil)

		assert.False(t, valid)
		assert.Empty(t, userID)
		assert.Empty(t, username)
		assert.Nil(t, roles)
	})
}

func TestAuthorizationError_Error(t *testing.T) {
	err := &AuthorizationError{
		Type:    AuthErrorPermissionDenied,
		Message: "permission denied: insufficient permissions",
	}

	assert.Equal(t, "permission denied: insufficient permissions", err.Error())
}

func TestToolResourceMappingCoverage(t *testing.T) {
	// Ensure all tool prefixes have mappings
	prefixes := []string{
		"quicbaas_kms_get",
		"quicbaas_kms_list",
		"quicbaas_kms_create",
		"quicbaas_kms_rotate",
		"quicbaas_kms_delete",
		"quicbaas_secrets_get",
		"quicbaas_secrets_list",
		"quicbaas_secrets_store",
		"quicbaas_secrets_delete",
		"quicbaas_crypto_encrypt",
		"quicbaas_crypto_decrypt",
		"quicbaas_seal_status",
		"quicbaas_unseal_",
		"quicbaas_vector_search",
		"quicbaas_vector_upsert",
		"quicbaas_vector_delete",
		"quicbaas_timeseries_query",
		"quicbaas_timeseries_write",
	}

	for _, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			resource, action := getToolResourceAction(prefix + "_test")
			assert.NotEmpty(t, resource, "resource should not be empty for prefix %s", prefix)
			assert.NotEmpty(t, action, "action should not be empty for prefix %s", prefix)
		})
	}
}

// =============================================================================
// RFC 8707 Token Audience Validation Tests
// =============================================================================

// createTestJWT creates a test JWT token with the given claims for testing.
// The token is not signed and should only be used for testing audience extraction.
func createTestJWT(claims map[string]interface{}) string {
	header := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}
	headerBytes, _ := json.Marshal(header)
	claimsBytes, _ := json.Marshal(claims)

	headerEncoded := base64.RawURLEncoding.EncodeToString(headerBytes)
	claimsEncoded := base64.RawURLEncoding.EncodeToString(claimsBytes)
	// Fake signature for testing (not verified)
	signatureEncoded := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))

	return headerEncoded + "." + claimsEncoded + "." + signatureEncoded
}

func TestValidateTokenAudience(t *testing.T) {
	resourceURI := "https://api.example.com/mcp"

	tests := []struct {
		name      string
		config    *MCPAuthConfig
		token     string
		wantError bool
		errMsg    string
	}{
		{
			name:      "no config - skip validation",
			config:    nil,
			token:     createTestJWT(map[string]interface{}{"sub": "user-123"}),
			wantError: false,
		},
		{
			name:      "config disabled - skip validation",
			config:    &MCPAuthConfig{Enabled: false, ResourceURI: resourceURI},
			token:     createTestJWT(map[string]interface{}{"sub": "user-123"}),
			wantError: false,
		},
		{
			name:      "config enabled but no resource URI - skip validation",
			config:    &MCPAuthConfig{Enabled: true, ResourceURI: ""},
			token:     createTestJWT(map[string]interface{}{"sub": "user-123"}),
			wantError: false,
		},
		{
			name:   "audience matches resource URI (string)",
			config: &MCPAuthConfig{Enabled: true, ResourceURI: resourceURI},
			token: createTestJWT(map[string]interface{}{
				"sub": "user-123",
				"aud": resourceURI,
			}),
			wantError: false,
		},
		{
			name:   "audience matches resource URI (array)",
			config: &MCPAuthConfig{Enabled: true, ResourceURI: resourceURI},
			token: createTestJWT(map[string]interface{}{
				"sub": "user-123",
				"aud": []string{"https://issuer.example.com", resourceURI},
			}),
			wantError: false,
		},
		{
			name:   "audience does not match resource URI",
			config: &MCPAuthConfig{Enabled: true, ResourceURI: resourceURI},
			token: createTestJWT(map[string]interface{}{
				"sub": "user-123",
				"aud": []string{"https://issuer.example.com", "https://other.example.com"},
			}),
			wantError: true,
			errMsg:    "audience",
		},
		{
			name:   "no audience claim in token - RFC 8707 requires audience when resource bound",
			config: &MCPAuthConfig{Enabled: true, ResourceURI: resourceURI},
			token: createTestJWT(map[string]interface{}{
				"sub": "user-123",
			}),
			wantError: true, // Per RFC 8707, tokens must have audience matching resource when bound
			errMsg:    "audience",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewAuthorizationChecker(nil, nil)
			checker.SetMCPAuthConfig(tt.config)

			err := checker.ValidateTokenAudience(tt.token)

			if tt.wantError {
				require.NotNil(t, err, "expected error but got nil")
				assert.Equal(t, AuthErrorInvalidToken, err.Type)
				if tt.errMsg != "" {
					assert.Contains(t, err.Message, tt.errMsg)
				}
			} else {
				assert.Nil(t, err, "expected no error but got: %v", err)
			}
		})
	}
}

func TestValidateTokenAudience_InvalidTokenFormat(t *testing.T) {
	config := &MCPAuthConfig{
		Enabled:     true,
		ResourceURI: "https://api.example.com/mcp",
	}

	checker := NewAuthorizationChecker(nil, nil)
	checker.SetMCPAuthConfig(config)

	tests := []struct {
		name  string
		token string
	}{
		{"not a JWT", "not-a-jwt"},
		{"too few parts", "header.payload"},
		{"invalid base64 in payload", "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.!!!invalid!!.signature"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For invalid tokens, validation should gracefully skip
			// (backwards compatibility - token signature validation happens elsewhere)
			err := checker.ValidateTokenAudience(tt.token)
			assert.Nil(t, err, "invalid token format should be skipped for backwards compatibility")
		})
	}
}

func TestExtractJWTAudience(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantAud []string
		wantErr bool
	}{
		{
			name: "string audience",
			token: createTestJWT(map[string]interface{}{
				"aud": "https://api.example.com",
			}),
			wantAud: []string{"https://api.example.com"},
			wantErr: false,
		},
		{
			name: "array audience",
			token: createTestJWT(map[string]interface{}{
				"aud": []string{"https://issuer.example.com", "https://api.example.com"},
			}),
			wantAud: []string{"https://issuer.example.com", "https://api.example.com"},
			wantErr: false,
		},
		{
			name: "no audience",
			token: createTestJWT(map[string]interface{}{
				"sub": "user-123",
			}),
			wantAud: nil,
			wantErr: false,
		},
		{
			name:    "invalid JWT format",
			token:   "invalid",
			wantAud: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audience, err := extractJWTAudience(tt.token)

			if tt.wantErr {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
				assert.Equal(t, tt.wantAud, audience)
			}
		})
	}
}

func TestNewAuthorizationCheckerWithConfig(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)
	config := &MCPAuthConfig{
		Enabled:     true,
		ResourceURI: "https://api.example.com/mcp",
	}

	checker := NewAuthorizationCheckerWithConfig(mockAuthService, mockAuthManager, config)

	require.NotNil(t, checker)
	assert.Equal(t, mockAuthService, checker.authService)
	assert.Equal(t, mockAuthManager, checker.authManager)
	assert.Equal(t, config, checker.mcpAuthConfig)
}

func TestSetMCPAuthConfig(t *testing.T) {
	checker := NewAuthorizationChecker(nil, nil)
	assert.Nil(t, checker.mcpAuthConfig)

	config := &MCPAuthConfig{
		Enabled:     true,
		ResourceURI: "https://api.example.com/mcp",
	}
	checker.SetMCPAuthConfig(config)

	assert.Equal(t, config, checker.mcpAuthConfig)
}

func TestBase64URLDecode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "valid base64url",
			input:   "SGVsbG8gV29ybGQ",
			want:    "Hello World",
			wantErr: false,
		},
		{
			name:    "valid base64url with URL-safe characters",
			input:   "YWJjZGVmZ2hpams-Xw",
			want:    "abcdefghijk>_",
			wantErr: false,
		},
		{
			name:    "invalid base64url",
			input:   "!!!invalid!!!",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := base64URLDecode(tt.input)
			if tt.wantErr {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
				assert.Equal(t, tt.want, string(got))
			}
		})
	}
}

func TestParseJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid JSON",
			input:   `{"key": "value", "number": 123}`,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result map[string]interface{}
			err := parseJSON([]byte(tt.input), &result)
			if tt.wantErr {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
		})
	}
}
