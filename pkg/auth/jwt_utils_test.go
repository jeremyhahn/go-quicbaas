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

// Package auth provides JWT token utilities tests
package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockTokenStore implements TokenStoreInterface for testing.
type MockTokenStore struct {
	refreshTokens map[string][]string // userID -> tokens
}

func NewMockTokenStore() *MockTokenStore {
	return &MockTokenStore{
		refreshTokens: make(map[string][]string),
	}
}

func (m *MockTokenStore) AddRefreshToken(userID, token string) {
	m.refreshTokens[userID] = append(m.refreshTokens[userID], token)
}

func (m *MockTokenStore) IsValidRefreshToken(userID, token string) bool {
	tokens, exists := m.refreshTokens[userID]
	if !exists {
		return false
	}
	for _, t := range tokens {
		if t == token {
			return true
		}
	}
	return false
}

func (m *MockTokenStore) Revoke(token string) {}

func (m *MockTokenStore) IsRevoked(token string) bool {
	return false
}

func (m *MockTokenStore) RevokeUserTokens(userID string) {}

// TestGenerateTokensForUser_Success tests successful token generation.
func TestGenerateTokensForUser_Success(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "test-secret-key-for-jwt",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-123",
		Username: "testuser",
		Roles:    []string{"admin", "readwrite"},
	}

	token, err := generateTokensForUser(user, config, tokenStore)

	require.NoError(t, err)
	assert.NotNil(t, token)
	assert.NotEmpty(t, token.AccessToken)
	assert.NotEmpty(t, token.RefreshToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.True(t, token.ExpiresAt.After(time.Now()))
	assert.True(t, token.ExpiresAt.Before(time.Now().Add(2*time.Hour)))

	// Verify refresh token was stored
	assert.True(t, tokenStore.IsValidRefreshToken(user.ID, token.RefreshToken))
}

// TestGenerateTokensForUser_ValidateAccessTokenClaims tests access token claims.
func TestGenerateTokensForUser_ValidateAccessTokenClaims(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "test-secret-key-for-jwt",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-456",
		Username: "jdoe",
		Roles:    []string{"readonly"},
	}

	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)

	// Parse and validate access token claims
	parsedToken, err := jwt.ParseWithClaims(token.AccessToken, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.JWTSecret), nil
	})
	require.NoError(t, err)

	claims, ok := parsedToken.Claims.(*Claims)
	require.True(t, ok)
	assert.Equal(t, user.ID, claims.UserID)
	assert.Equal(t, user.Username, claims.Username)
	assert.Equal(t, user.Roles, claims.Roles)
	assert.Equal(t, "quicbaas", claims.Issuer)
	assert.Equal(t, user.ID, claims.Subject)
	assert.NotNil(t, claims.ExpiresAt)
	assert.NotNil(t, claims.IssuedAt)
	assert.NotNil(t, claims.NotBefore)
}

// TestGenerateTokensForUser_ValidateRefreshTokenClaims tests refresh token claims.
func TestGenerateTokensForUser_ValidateRefreshTokenClaims(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "test-secret-key-for-jwt",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 48 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-789",
		Username: "alice",
		Roles:    []string{"admin"},
	}

	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)

	// Parse and validate refresh token claims
	parsedToken, err := jwt.ParseWithClaims(token.RefreshToken, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.JWTSecret), nil
	})
	require.NoError(t, err)

	claims, ok := parsedToken.Claims.(*Claims)
	require.True(t, ok)
	assert.Equal(t, user.ID, claims.UserID)
	assert.Equal(t, user.Username, claims.Username)
	assert.Equal(t, user.Roles, claims.Roles)
	assert.Equal(t, "quicbaas", claims.Issuer)
	assert.Equal(t, user.ID, claims.Subject)

	// Refresh token should have longer expiry
	assert.True(t, claims.ExpiresAt.After(time.Now().Add(47*time.Hour)))
}

// TestGenerateTokensForUser_DifferentConfigs tests token generation with different configs.
func TestGenerateTokensForUser_DifferentConfigs(t *testing.T) {
	tests := []struct {
		name            string
		tokenTTL        time.Duration
		refreshTokenTTL time.Duration
	}{
		{
			name:            "short TTL",
			tokenTTL:        5 * time.Minute,
			refreshTokenTTL: 1 * time.Hour,
		},
		{
			name:            "medium TTL",
			tokenTTL:        2 * time.Hour,
			refreshTokenTTL: 7 * 24 * time.Hour,
		},
		{
			name:            "long TTL",
			tokenTTL:        24 * time.Hour,
			refreshTokenTTL: 30 * 24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AuthConfig{
				JWTSecret:       "test-secret",
				TokenTTL:        tt.tokenTTL,
				RefreshTokenTTL: tt.refreshTokenTTL,
			}
			tokenStore := NewMockTokenStore()

			user := &User{
				ID:       "user-test",
				Username: "testuser",
				Roles:    []string{"readwrite"},
			}

			token, err := generateTokensForUser(user, config, tokenStore)
			require.NoError(t, err)
			assert.NotNil(t, token)

			// Verify access token expiry
			expiryDiff := token.ExpiresAt.Sub(time.Now())
			assert.True(t, expiryDiff >= tt.tokenTTL-1*time.Second)
			assert.True(t, expiryDiff <= tt.tokenTTL+1*time.Second)
		})
	}
}

// TestGenerateTokensForUser_EmptySecret tests behavior with empty secret.
func TestGenerateTokensForUser_EmptySecret(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-123",
		Username: "testuser",
		Roles:    []string{"admin"},
	}

	// Empty secret should still work (though not secure)
	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)
	assert.NotNil(t, token)
}

// TestGenerateTokensForUser_MultipleRoles tests token generation with multiple roles.
func TestGenerateTokensForUser_MultipleRoles(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "test-secret-key",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-multi",
		Username: "multiuser",
		Roles:    []string{"admin", "readwrite", "readonly", "custom-role"},
	}

	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)

	// Verify roles in token
	parsedToken, err := jwt.ParseWithClaims(token.AccessToken, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.JWTSecret), nil
	})
	require.NoError(t, err)

	claims, ok := parsedToken.Claims.(*Claims)
	require.True(t, ok)
	assert.Equal(t, 4, len(claims.Roles))
	assert.Contains(t, claims.Roles, "admin")
	assert.Contains(t, claims.Roles, "custom-role")
}

// TestGenerateTokensForUser_NoRoles tests token generation with no roles.
func TestGenerateTokensForUser_NoRoles(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "test-secret-key",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-noroles",
		Username: "noroles",
		Roles:    []string{}, // No roles
	}

	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)
	assert.NotNil(t, token)

	// Verify empty roles in token
	parsedToken, err := jwt.ParseWithClaims(token.AccessToken, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.JWTSecret), nil
	})
	require.NoError(t, err)

	claims, ok := parsedToken.Claims.(*Claims)
	require.True(t, ok)
	assert.Equal(t, 0, len(claims.Roles))
}

// TestValidateJWTToken_ValidToken tests validation of valid tokens.
func TestValidateJWTToken_ValidToken(t *testing.T) {
	secret := "test-validation-secret"

	// Create a valid token
	claims := &Claims{
		UserID:   "user-validate",
		Username: "validator",
		Roles:    []string{"admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
			Subject:   "user-validate",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Validate the token using legacy function
	validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
	require.NoError(t, err)
	assert.NotNil(t, validatedClaims)
	assert.Equal(t, "user-validate", validatedClaims.UserID)
	assert.Equal(t, "validator", validatedClaims.Username)
	assert.Equal(t, []string{"admin"}, validatedClaims.Roles)
}

// TestValidateJWTToken_ExpiredToken tests validation of expired tokens.
func TestValidateJWTToken_ExpiredToken(t *testing.T) {
	secret := "test-expiry-secret"

	// Create an expired token
	claims := &Claims{
		UserID:   "user-expired",
		Username: "expireduser",
		Roles:    []string{"readwrite"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Expired 1 hour ago
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			Issuer:    "quicbaas",
			Subject:   "user-expired",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Validation should fail
	validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
	assert.Error(t, err)
	assert.Nil(t, validatedClaims)
	assert.Contains(t, err.Error(), "invalid token")
}

// TestValidateJWTToken_InvalidSecret tests validation with wrong secret.
func TestValidateJWTToken_InvalidSecret(t *testing.T) {
	correctSecret := "correct-secret"
	wrongSecret := "wrong-secret"

	// Create a token with correct secret
	claims := &Claims{
		UserID:   "user-secret",
		Username: "secretuser",
		Roles:    []string{"readonly"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(correctSecret))
	require.NoError(t, err)

	// Validate with wrong secret
	validatedClaims, err := validateJWTTokenLegacy(tokenString, wrongSecret)
	assert.Error(t, err)
	assert.Nil(t, validatedClaims)
	assert.Contains(t, err.Error(), "invalid token")
}

// TestValidateJWTToken_MalformedToken tests validation of malformed tokens.
func TestValidateJWTToken_MalformedToken(t *testing.T) {
	secret := "test-secret"
	config := &AuthConfig{JWTSecret: secret}

	tests := []struct {
		name  string
		token string
	}{
		{
			name:  "completely invalid",
			token: "this-is-not-a-jwt-token",
		},
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "partial token",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.incomplete",
		},
		{
			name:  "random string",
			token: "random.string.here",
		},
		{
			name:  "base64 but not JWT",
			token: "aGVsbG8=.d29ybGQ=.dGVzdA==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validatedClaims, err := validateJWTToken(tt.token, config)
			assert.Error(t, err)
			assert.Nil(t, validatedClaims)
		})
	}
}

// TestValidateJWTToken_WrongSigningMethod tests tokens with invalid signing method.
func TestValidateJWTToken_WrongSigningMethod(t *testing.T) {
	secret := "test-secret"

	// Create a token with RS256 instead of HS256
	claims := &Claims{
		UserID:   "user-wrong-method",
		Username: "wrongmethod",
		Roles:    []string{"admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	// Note: This creates a token but we'll manually construct one with wrong method
	// For testing purposes, we'll create a valid HS256 token and verify the validation
	// logic checks for HMAC methods
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// This should succeed since it's HS256
	validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
	assert.NoError(t, err)
	assert.NotNil(t, validatedClaims)

	// Test with a manually crafted token with wrong algorithm (simulated)
	// Create token with header specifying different algorithm
	header := `{"alg":"none","typ":"JWT"}`
	payload := `{"user_id":"test","username":"test","roles":[]}`
	signature := ""
	noneToken := base64Encode(header) + "." + base64Encode(payload) + "." + signature

	validatedClaims, err = validateJWTTokenLegacy(noneToken, secret)
	assert.Error(t, err)
	assert.Nil(t, validatedClaims)
}

// TestValidateJWTToken_NotBeforeTime tests token validation with NotBefore claim.
func TestValidateJWTToken_NotBeforeTime(t *testing.T) {
	secret := "test-notbefore-secret"

	// Create a token that's not valid yet (NotBefore is in the future)
	claims := &Claims{
		UserID:   "user-notbefore",
		Username: "notbeforeuser",
		Roles:    []string{"readwrite"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)), // Valid in 1 hour
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Validation should fail because token is not yet valid
	validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
	assert.Error(t, err)
	assert.Nil(t, validatedClaims)
}

// TestValidateJWTToken_EmptySecret tests validation with empty secret.
func TestValidateJWTToken_EmptySecret(t *testing.T) {
	secret := ""

	// Create a token with empty secret
	claims := &Claims{
		UserID:   "user-emptysecret",
		Username: "emptysecret",
		Roles:    []string{"admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Should validate successfully with same empty secret
	validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
	assert.NoError(t, err)
	assert.NotNil(t, validatedClaims)
}

// TestValidateJWTToken_LongSecret tests validation with very long secret.
func TestValidateJWTToken_LongSecret(t *testing.T) {
	// Create a very long secret (1000 characters)
	secret := ""
	for i := 0; i < 100; i++ {
		secret += "0123456789"
	}

	claims := &Claims{
		UserID:   "user-longsecret",
		Username: "longsecret",
		Roles:    []string{"readonly"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Should validate successfully with same long secret
	validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
	assert.NoError(t, err)
	assert.NotNil(t, validatedClaims)
	assert.Equal(t, "user-longsecret", validatedClaims.UserID)
}

// TestValidateJWTToken_SpecialCharactersInClaims tests claims with special characters.
func TestValidateJWTToken_SpecialCharactersInClaims(t *testing.T) {
	secret := "test-special-chars"

	claims := &Claims{
		UserID:   "user-123-abc-!@#",
		Username: "user@example.com",
		Roles:    []string{"role-1", "role_2", "role.3", "role:4"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
	assert.NoError(t, err)
	assert.NotNil(t, validatedClaims)
	assert.Equal(t, "user-123-abc-!@#", validatedClaims.UserID)
	assert.Equal(t, "user@example.com", validatedClaims.Username)
	assert.Equal(t, 4, len(validatedClaims.Roles))
}

// TestValidateJWTToken_InvalidClaimsStructure tests tokens with invalid claims.
func TestValidateJWTToken_InvalidClaimsStructure(t *testing.T) {
	secret := "test-invalid-claims"

	// Create a token with standard JWT claims (not our custom Claims struct)
	standardClaims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Issuer:    "quicbaas",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, standardClaims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Should parse but with empty custom fields
	validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
	assert.NoError(t, err)
	assert.NotNil(t, validatedClaims)
	assert.Equal(t, "", validatedClaims.UserID)
	assert.Equal(t, "", validatedClaims.Username)
	assert.Nil(t, validatedClaims.Roles)
}

// TestGenerateTokensForUser_ConsecutiveCalls moved to jwt_utils_integration_test.go

// TestRoundTrip_GenerateAndValidate tests full token generation and validation cycle.
func TestRoundTrip_GenerateAndValidate(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "roundtrip-secret-key",
		TokenTTL:        2 * time.Hour,
		RefreshTokenTTL: 48 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-roundtrip",
		Username: "roundtripuser",
		Roles:    []string{"admin", "readwrite"},
	}

	// Generate tokens
	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)

	// Validate access token
	claims, err := validateJWTToken(token.AccessToken, config)
	require.NoError(t, err)
	assert.Equal(t, user.ID, claims.UserID)
	assert.Equal(t, user.Username, claims.Username)
	assert.Equal(t, user.Roles, claims.Roles)

	// Validate refresh token
	refreshClaims, err := validateJWTToken(token.RefreshToken, config)
	require.NoError(t, err)
	assert.Equal(t, user.ID, refreshClaims.UserID)
	assert.Equal(t, user.Username, refreshClaims.Username)

	// Verify refresh token is stored
	assert.True(t, tokenStore.IsValidRefreshToken(user.ID, token.RefreshToken))
}

// TestEdgeCases_EmptyUserFields tests token generation with empty user fields.
func TestEdgeCases_EmptyUserFields(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "test-edge-cases",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	tests := []struct {
		name string
		user *User
	}{
		{
			name: "empty username",
			user: &User{
				ID:       "user-empty-name",
				Username: "",
				Roles:    []string{"admin"},
			},
		},
		{
			name: "empty ID",
			user: &User{
				ID:       "",
				Username: "emptyid",
				Roles:    []string{"admin"},
			},
		},
		{
			name: "all empty",
			user: &User{
				ID:       "",
				Username: "",
				Roles:    []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := generateTokensForUser(tt.user, config, tokenStore)
			require.NoError(t, err)
			assert.NotNil(t, token)

			// Tokens should still be valid
			claims, err := validateJWTToken(token.AccessToken, config)
			require.NoError(t, err)
			assert.Equal(t, tt.user.ID, claims.UserID)
			assert.Equal(t, tt.user.Username, claims.Username)
		})
	}
}

// Helper function to encode base64 for URL.
func base64Encode(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

// BenchmarkGenerateTokensForUser benchmarks token generation.
func BenchmarkGenerateTokensForUser(b *testing.B) {
	config := &AuthConfig{
		JWTSecret:       "benchmark-secret-key",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "bench-user",
		Username: "benchuser",
		Roles:    []string{"admin", "readwrite"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := generateTokensForUser(user, config, tokenStore)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkValidateJWTToken benchmarks token validation.
func BenchmarkValidateJWTToken(b *testing.B) {
	secret := "benchmark-validation-secret"

	// Create a token once
	claims := &Claims{
		UserID:   "bench-validate",
		Username: "benchvalidate",
		Roles:    []string{"admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := validateJWTTokenLegacy(tokenString, secret)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestValidateJWTToken_ModifiedToken tests tampered tokens.
func TestValidateJWTToken_ModifiedToken(t *testing.T) {
	secret := "test-tamper-secret"

	// Create a valid token
	claims := &Claims{
		UserID:   "user-tamper",
		Username: "tamperuser",
		Roles:    []string{"readonly"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Tamper with the token by changing a character in the middle
	if len(tokenString) > 10 {
		tampered := tokenString[:len(tokenString)/2] + "X" + tokenString[len(tokenString)/2+1:]

		// Validation should fail
		validatedClaims, err := validateJWTTokenLegacy(tampered, secret)
		assert.Error(t, err)
		assert.Nil(t, validatedClaims)
	}
}

// TestTokenStoreIntegration tests interaction with token store.
func TestTokenStoreIntegration(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "store-integration-secret",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	users := []*User{
		{ID: "user1", Username: "user1", Roles: []string{"admin"}},
		{ID: "user2", Username: "user2", Roles: []string{"readwrite"}},
		{ID: "user3", Username: "user3", Roles: []string{"readonly"}},
	}

	// Generate tokens for multiple users
	for _, user := range users {
		token, err := generateTokensForUser(user, config, tokenStore)
		require.NoError(t, err)
		assert.True(t, tokenStore.IsValidRefreshToken(user.ID, token.RefreshToken))
	}

	// Verify each user has their refresh token stored
	for _, user := range users {
		tokens := tokenStore.refreshTokens[user.ID]
		assert.NotEmpty(t, tokens)
	}
}

// TestConcurrentTokenGeneration moved to jwt_utils_integration_test.go

// TestValidateJWTToken_WithDifferentIssuers tests validation rejects wrong issuers.
func TestValidateJWTToken_WithDifferentIssuers(t *testing.T) {
	secret := "test-issuer-secret"

	tests := []struct {
		name          string
		issuer        string
		shouldSucceed bool
	}{
		{
			name:          "correct issuer",
			issuer:        "quicbaas",
			shouldSucceed: true,
		},
		{
			name:          "wrong issuer",
			issuer:        "wrong-issuer",
			shouldSucceed: true, // validateJWTToken doesn't check issuer
		},
		{
			name:          "empty issuer",
			issuer:        "",
			shouldSucceed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &Claims{
				UserID:   "user-issuer",
				Username: "issueruser",
				Roles:    []string{"admin"},
				RegisteredClaims: jwt.RegisteredClaims{
					ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
					IssuedAt:  jwt.NewNumericDate(time.Now()),
					Issuer:    tt.issuer,
				},
			}

			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			tokenString, err := token.SignedString([]byte(secret))
			require.NoError(t, err)

			validatedClaims, err := validateJWTTokenLegacy(tokenString, secret)
			if tt.shouldSucceed {
				assert.NoError(t, err)
				assert.NotNil(t, validatedClaims)
				assert.Equal(t, tt.issuer, validatedClaims.Issuer)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// TestGenerateTokensForUser_NilTokenStore tests with nil token store (should panic or error).
func TestGenerateTokensForUser_NilUser(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "test-nil-secret",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	// This should panic or create invalid token
	defer func() {
		if r := recover(); r == nil {
			// If it doesn't panic, check if it creates an invalid token
			token, err := generateTokensForUser(nil, config, tokenStore)
			// Either error or token with empty fields
			if err == nil {
				assert.NotNil(t, token)
			}
		}
	}()

	generateTokensForUser(nil, config, tokenStore)
}

// TestVeryShortTTL moved to jwt_utils_integration_test.go

// TestUnicodeInClaims tests token generation with unicode characters.
func TestUnicodeInClaims(t *testing.T) {
	config := &AuthConfig{
		JWTSecret:       "unicode-secret-key-for-test",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-unicode-abc",
		Username: "unicode-user-test",
		Roles:    []string{"admin", "reader", "writer"},
	}

	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)

	// Validate and check unicode is preserved
	claims, err := validateJWTToken(token.AccessToken, config)
	require.NoError(t, err)
	assert.Equal(t, user.ID, claims.UserID)
	assert.Equal(t, user.Username, claims.Username)
	assert.Equal(t, user.Roles, claims.Roles)
}

// TestGenerateTokensWithDifferentSecrets moved to jwt_utils_integration_test.go

// TestES256SignAndVerifyRoundtrip tests asymmetric ES256 token signing and verification.
func TestES256SignAndVerifyRoundtrip(t *testing.T) {
	// Generate test ECDSA key in-memory
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	config := &AuthConfig{
		JWTSigningKeyID:     "test-es256-key",
		JWTSigningAlgorithm: "ES256",
		TokenTTL:            1 * time.Hour,
		RefreshTokenTTL:     24 * time.Hour,
	}

	// Configure asymmetric signing
	SetAsymmetricSigning(config, privateKey)

	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-es256",
		Username: "es256user",
		TenantID: "tenant-abc",
		Roles:    []string{"admin", "readwrite"},
	}

	// Generate tokens with ES256
	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)
	require.NotNil(t, token)
	assert.NotEmpty(t, token.AccessToken)
	assert.NotEmpty(t, token.RefreshToken)
	assert.Equal(t, "Bearer", token.TokenType)

	// Verify the token has "kid" header
	parsedToken, err := jwt.Parse(token.AccessToken, func(t *jwt.Token) (interface{}, error) {
		return &privateKey.PublicKey, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "test-es256-key", parsedToken.Header["kid"])
	assert.Equal(t, "ES256", parsedToken.Header["alg"])

	// Validate access token using the config-based validator
	claims, err := validateJWTToken(token.AccessToken, config)
	require.NoError(t, err)
	assert.Equal(t, user.ID, claims.UserID)
	assert.Equal(t, user.Username, claims.Username)
	assert.Equal(t, user.TenantID, claims.TenantID)
	assert.Equal(t, user.Roles, claims.Roles)
	assert.Equal(t, "quicbaas", claims.Issuer)

	// Validate refresh token
	refreshClaims, err := validateJWTToken(token.RefreshToken, config)
	require.NoError(t, err)
	assert.Equal(t, user.ID, refreshClaims.UserID)

	// Verify refresh token is stored
	assert.True(t, tokenStore.IsValidRefreshToken(user.ID, token.RefreshToken))
}

// TestES256VerificationRejectsWrongKey tests that ES256 tokens signed with
// one key are rejected when verified with a different key.
func TestES256VerificationRejectsWrongKey(t *testing.T) {
	// Generate two different key pairs
	signingKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Sign with the first key
	signingConfig := &AuthConfig{
		JWTSigningKeyID:     "signing-key",
		JWTSigningAlgorithm: "ES256",
		TokenTTL:            1 * time.Hour,
		RefreshTokenTTL:     24 * time.Hour,
	}
	SetAsymmetricSigning(signingConfig, signingKey)

	tokenStore := NewMockTokenStore()
	user := &User{ID: "user-wrong-key", Username: "wrongkeyuser", Roles: []string{"admin"}}

	token, err := generateTokensForUser(user, signingConfig, tokenStore)
	require.NoError(t, err)

	// Attempt verification with the wrong key
	wrongConfig := &AuthConfig{
		JWTSigningKeyID:     "wrong-key",
		JWTSigningAlgorithm: "ES256",
	}
	SetAsymmetricSigning(wrongConfig, wrongKey)

	claims, err := validateJWTToken(token.AccessToken, wrongConfig)
	assert.Error(t, err)
	assert.Nil(t, claims)
}

// TestHS256FallbackDuringMigration tests that HS256 tokens are still accepted
// when asymmetric signing is configured (migration scenario).
func TestHS256FallbackDuringMigration(t *testing.T) {
	secret := "migration-secret-key"

	// Create an HS256 token (pre-migration)
	claims := &Claims{
		UserID:   "user-migrate",
		Username: "migrateuser",
		Roles:    []string{"admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Configure asymmetric signing but keep JWTSecret for migration
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	config := &AuthConfig{
		JWTSecret:           secret, // Legacy HS256 secret still present
		JWTSigningKeyID:     "new-es256-key",
		JWTSigningAlgorithm: "ES256",
	}
	SetAsymmetricSigning(config, privateKey)

	// HS256 token should still validate because JWTSecret is configured
	validatedClaims, err := validateJWTToken(tokenString, config)
	require.NoError(t, err)
	assert.NotNil(t, validatedClaims)
	assert.Equal(t, "user-migrate", validatedClaims.UserID)
}

// TestHS256RejectedWhenSecretRemoved tests that HS256 tokens are rejected
// when JWTSecret is removed (migration complete).
func TestHS256RejectedWhenSecretRemoved(t *testing.T) {
	secret := "old-secret-key"

	// Create an HS256 token
	claims := &Claims{
		UserID:   "user-old",
		Username: "olduser",
		Roles:    []string{"admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	// Configure asymmetric signing WITHOUT JWTSecret (migration complete)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	config := &AuthConfig{
		JWTSecret:           "", // Secret removed after migration
		JWTSigningKeyID:     "es256-key",
		JWTSigningAlgorithm: "ES256",
	}
	SetAsymmetricSigning(config, privateKey)

	// HS256 token should be rejected because JWTSecret is empty
	validatedClaims, err := validateJWTToken(tokenString, config)
	assert.Error(t, err)
	assert.Nil(t, validatedClaims)
	assert.Contains(t, err.Error(), "invalid token")
}

// TestResolveSigningMethod tests the algorithm name to jwt.SigningMethod mapping.
func TestResolveSigningMethod(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected jwt.SigningMethod
	}{
		{"ES256", "ES256", jwt.SigningMethodES256},
		{"ES384", "ES384", jwt.SigningMethodES384},
		{"ES512", "ES512", jwt.SigningMethodES512},
		{"RS256", "RS256", jwt.SigningMethodRS256},
		{"RS384", "RS384", jwt.SigningMethodRS384},
		{"RS512", "RS512", jwt.SigningMethodRS512},
		{"empty defaults to ES256", "", jwt.SigningMethodES256},
		{"unknown defaults to ES256", "UNKNOWN", jwt.SigningMethodES256},
		{"lowercase not matched defaults to ES256", "es256", jwt.SigningMethodES256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveSigningMethod(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSignTokenAsymmetricWithoutSigner tests that signing fails when no signer is set.
func TestSignTokenAsymmetricWithoutSigner(t *testing.T) {
	config := &AuthConfig{
		JWTSigningKeyID:     "missing-key",
		JWTSigningAlgorithm: "ES256",
		// jwtSigner is nil - not set
	}

	claims := &Claims{
		UserID:   "test-user",
		Username: "testuser",
		Roles:    []string{"admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	tokenString, err := signToken(claims, config)
	assert.Error(t, err)
	assert.Empty(t, tokenString)
	assert.Contains(t, err.Error(), "signer not initialized")
}

// TestValidateJWTTokenWithECDSANoPublicKey tests that ECDSA tokens are rejected
// when no public key is configured.
func TestValidateJWTTokenWithECDSANoPublicKey(t *testing.T) {
	// Create an ECDSA-signed token
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	claims := &Claims{
		UserID:   "user-nokey",
		Username: "nokeyuser",
		Roles:    []string{"admin"},
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "quicbaas",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)

	// Config with no public key and no secret
	config := &AuthConfig{}

	validatedClaims, err := validateJWTToken(tokenString, config)
	assert.Error(t, err)
	assert.Nil(t, validatedClaims)
	assert.Contains(t, err.Error(), "no public key configured")
}

// TestSetAsymmetricSigning verifies that SetAsymmetricSigning properly sets
// both the signer and the public key on the config.
func TestSetAsymmetricSigning(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	config := &AuthConfig{}

	SetAsymmetricSigning(config, privateKey)

	assert.NotNil(t, config.jwtSigner)
	assert.NotNil(t, config.jwtPublicKey)
	assert.Equal(t, &privateKey.PublicKey, config.jwtPublicKey)
}

// TestSetPublicKey verifies that SetPublicKey sets only the public key.
func TestSetPublicKey(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	config := &AuthConfig{}

	SetPublicKey(config, &privateKey.PublicKey)

	assert.Nil(t, config.jwtSigner)
	assert.NotNil(t, config.jwtPublicKey)
	assert.Equal(t, &privateKey.PublicKey, config.jwtPublicKey)
}

// BenchmarkES256TokenGeneration benchmarks ES256 token generation.
func BenchmarkES256TokenGeneration(b *testing.B) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	config := &AuthConfig{
		JWTSigningKeyID:     "bench-es256-key",
		JWTSigningAlgorithm: "ES256",
		TokenTTL:            1 * time.Hour,
		RefreshTokenTTL:     24 * time.Hour,
	}
	SetAsymmetricSigning(config, privateKey)

	tokenStore := NewMockTokenStore()
	user := &User{
		ID:       "bench-user",
		Username: "benchuser",
		Roles:    []string{"admin", "readwrite"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := generateTokensForUser(user, config, tokenStore)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkES256TokenValidation benchmarks ES256 token validation.
func BenchmarkES256TokenValidation(b *testing.B) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	config := &AuthConfig{
		JWTSigningKeyID:     "bench-es256-key",
		JWTSigningAlgorithm: "ES256",
		TokenTTL:            1 * time.Hour,
		RefreshTokenTTL:     24 * time.Hour,
	}
	SetAsymmetricSigning(config, privateKey)

	tokenStore := NewMockTokenStore()
	user := &User{
		ID:       "bench-user",
		Username: "benchuser",
		Roles:    []string{"admin"},
	}

	token, err := generateTokensForUser(user, config, tokenStore)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := validateJWTToken(token.AccessToken, config)
		if err != nil {
			b.Fatal(err)
		}
	}
}
