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

package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// jwtHeaderTyp decodes the JOSE header of a compact JWT and returns its "typ"
// value (empty when absent). It is used to assert RFC 9068 "at+jwt" stamping on
// access tokens without re-verifying the signature.
func jwtHeaderTyp(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed JWT: %d segments", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode JWT header: %v", err)
	}
	var hdr map[string]interface{}
	if err := json.Unmarshal(raw, &hdr); err != nil {
		t.Fatalf("unmarshal JWT header: %v", err)
	}
	typ, _ := hdr["typ"].(string)
	return typ
}

// Mock TokenStorage for testing
type mockTokenStorage struct {
	tokens      map[string]*Token
	authCodes   map[string]*AuthCodeData
	tokenValues map[string]*Token // For refresh tokens
}

func newMockTokenStorage() *mockTokenStorage {
	return &mockTokenStorage{
		tokens:      make(map[string]*Token),
		authCodes:   make(map[string]*AuthCodeData),
		tokenValues: make(map[string]*Token),
	}
}

func (m *mockTokenStorage) StoreAuthCode(ctx context.Context, code *AuthCodeData) error {
	m.authCodes[code.Code] = code
	return nil
}

func (m *mockTokenStorage) GetAuthCode(ctx context.Context, code string) (*AuthCodeData, error) {
	authCode, ok := m.authCodes[code]
	if !ok {
		return nil, ErrUserNotFound
	}
	return authCode, nil
}

func (m *mockTokenStorage) DeleteAuthCode(ctx context.Context, code string) error {
	delete(m.authCodes, code)
	return nil
}

func (m *mockTokenStorage) StoreToken(ctx context.Context, token *Token) error {
	m.tokens[token.JTI] = token
	return nil
}

func (m *mockTokenStorage) GetToken(ctx context.Context, jti string) (*Token, error) {
	token, ok := m.tokens[jti]
	if !ok {
		return nil, ErrUserNotFound
	}
	return token, nil
}

func (m *mockTokenStorage) GetTokenByValue(ctx context.Context, tokenValue string) (*Token, error) {
	token, ok := m.tokenValues[tokenValue]
	if !ok {
		return nil, ErrUserNotFound
	}
	return token, nil
}

func (m *mockTokenStorage) RevokeToken(ctx context.Context, jti string) error {
	if token, ok := m.tokens[jti]; ok {
		token.Revoked = true
	}
	return nil
}

func (m *mockTokenStorage) IsTokenRevoked(ctx context.Context, jti string) (bool, error) {
	if token, ok := m.tokens[jti]; ok {
		return token.Revoked, nil
	}
	return false, nil
}

func (m *mockTokenStorage) DeleteExpiredTokens(ctx context.Context) error {
	for jti, token := range m.tokens {
		if token.IsExpired() {
			delete(m.tokens, jti)
		}
	}
	return nil
}

func (m *mockTokenStorage) ListTokensByUser(ctx context.Context, userID string) ([]*Token, error) {
	var result []*Token
	for _, token := range m.tokens {
		if token.Subject == userID {
			result = append(result, token)
		}
	}
	return result, nil
}

func (m *mockTokenStorage) RevokeUserTokens(ctx context.Context, userID string) error {
	for _, token := range m.tokens {
		if token.Subject == userID {
			token.Revoked = true
		}
	}
	return nil
}

func createTestTokenService(t *testing.T) (*TokenService, *rsa.PrivateKey) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	config := &OIDCConfig{
		Issuer:           "https://test.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       privateKey,
		AccessTokenTTL:   1 * time.Hour,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       1 * time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}

	storage := newMockTokenStorage()
	service := NewTokenService(storage, privateKey, config)

	return service, privateKey
}

func TestTokenService_GenerateAccessToken(t *testing.T) {
	service, privateKey := createTestTokenService(t)
	ctx := context.Background()

	now := time.Now()
	claims := &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "user-123",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "token-jti-123",
		TenantID:  "tenant-1",
		Roles:     []string{"admin"},
		Groups:    []string{"developers"},
		Email:     "test@example.com",
		Name:      "Test User",
		Scope:     "openid profile email",
		ClientID:  "client-123",
	}

	tokenString, err := service.GenerateAccessToken(ctx, claims)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	if tokenString == "" {
		t.Fatal("GenerateAccessToken() returned empty token")
	}

	// Verify token can be parsed using XKMS
	mapClaims, err := xkms.JWTVerifyWithKeyAndClaims(tokenString, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}

	if mapClaims["sub"] != "user-123" {
		t.Errorf("subject = %v, want user-123", mapClaims["sub"])
	}
	if mapClaims["tenant_id"] != "tenant-1" {
		t.Errorf("tenant_id = %v, want tenant-1", mapClaims["tenant_id"])
	}

	// RFC 9068: the access token MUST carry typ "at+jwt".
	if typ := jwtHeaderTyp(t, tokenString); typ != AccessTokenJWTType {
		t.Errorf("access token typ = %q, want %q", typ, AccessTokenJWTType)
	}
}

// TestTokenService_GenerateIDToken_NotAccessTokenType asserts an ID token does
// NOT carry the RFC 9068 access-token media type (token-type confusion guard).
func TestTokenService_GenerateIDToken_NotAccessTokenType(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	now := time.Now()
	idToken, err := service.GenerateIDToken(ctx, &IDTokenClaims{
		Issuer:    "https://test.example.com",
		Subject:   "user-123",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		AuthTime:  NewUnixTime(now),
		TenantID:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("GenerateIDToken() error = %v", err)
	}
	if typ := jwtHeaderTyp(t, idToken); typ == AccessTokenJWTType {
		t.Errorf("ID token typ = %q, must not be %q", typ, AccessTokenJWTType)
	}
}

func TestTokenService_GenerateIDToken(t *testing.T) {
	service, privateKey := createTestTokenService(t)
	ctx := context.Background()

	now := time.Now()
	claims := &IDTokenClaims{
		Issuer:        "https://test.example.com",
		Subject:       "user-123",
		Audience:      []string{"client-123"},
		ExpiresAt:     NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:      NewUnixTime(now),
		AuthTime:      NewUnixTime(now),
		Nonce:         "nonce-123",
		Email:         "test@example.com",
		EmailVerified: true,
		Name:          "Test User",
		TenantID:      "tenant-1",
		Roles:         []string{"admin"},
		Groups:        []string{"developers"},
		MFAVerified:   true,
		AuthMethod:    "webauthn",
	}

	tokenString, err := service.GenerateIDToken(ctx, claims)
	if err != nil {
		t.Fatalf("GenerateIDToken() error = %v", err)
	}

	if tokenString == "" {
		t.Fatal("GenerateIDToken() returned empty token")
	}

	// Verify token can be parsed using XKMS
	mapClaims, err := xkms.JWTVerifyWithKeyAndClaims(tokenString, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}

	if mapClaims["nonce"] != "nonce-123" {
		t.Errorf("nonce = %v, want nonce-123", mapClaims["nonce"])
	}
	if mapClaims["email_verified"] != true {
		t.Errorf("email_verified = %v, want true", mapClaims["email_verified"])
	}
}

func TestTokenService_GenerateRefreshToken(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	tokenString, err := service.GenerateRefreshToken(ctx, SessionRefreshOptions{Subject: "user-123", ClientID: "client-123"})
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	if tokenString == "" {
		t.Fatal("GenerateRefreshToken() returned empty token")
	}

	// Refresh tokens should be opaque (not JWT)
	if strings.HasPrefix(tokenString, "eyJ") {
		t.Error("refresh token should not be a JWT")
	}

	// Verify token is stored
	tokens, err := service.storage.ListTokensByUser(ctx, "user-123")
	if err != nil {
		t.Fatalf("ListTokensByUser() error = %v", err)
	}

	if len(tokens) == 0 {
		t.Fatal("refresh token was not stored")
	}

	found := false
	for _, token := range tokens {
		if token.TokenType == TokenTypeRefresh && token.ClientID == "client-123" {
			found = true
			break
		}
	}

	if !found {
		t.Error("refresh token not found in storage")
	}
}

func TestTokenService_GenerateAuthCode(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	req := &AuthorizeRequest{
		ClientID:            "client-123",
		RedirectURI:         "https://app.example.com/callback",
		ResponseType:        ResponseTypeCode,
		Scope:               []string{"openid", "profile"},
		State:               "state-123",
		CodeChallenge:       "challenge-123",
		CodeChallengeMethod: CodeChallengeMethodS256,
		Nonce:               "nonce-123",
		UserID:              1000123,
		TenantID:            "tenant-1",
	}

	code, err := service.GenerateAuthCode(ctx, req, 1000123)
	if err != nil {
		t.Fatalf("GenerateAuthCode() error = %v", err)
	}

	if code == "" {
		t.Fatal("GenerateAuthCode() returned empty code")
	}

	// Verify code is stored
	authCode, err := service.storage.GetAuthCode(ctx, code)
	if err != nil {
		t.Fatalf("GetAuthCode() error = %v", err)
	}

	if authCode.UserID != 1000123 {
		t.Errorf("UserID = %v, want user-123", authCode.UserID)
	}
	if authCode.ClientID != "client-123" {
		t.Errorf("ClientID = %v, want client-123", authCode.ClientID)
	}
	if authCode.CodeChallenge != "challenge-123" {
		t.Errorf("CodeChallenge = %v, want challenge-123", authCode.CodeChallenge)
	}
}

func TestTokenService_ValidateAuthCode(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	// Generate a valid auth code
	req := &AuthorizeRequest{
		ClientID:     "client-123",
		RedirectURI:  "https://app.example.com/callback",
		ResponseType: ResponseTypeCode,
		Scope:        []string{"openid"},
		UserID:       1000123,
		TenantID:     "tenant-1",
	}

	code, err := service.GenerateAuthCode(ctx, req, 1000123)
	if err != nil {
		t.Fatalf("GenerateAuthCode() error = %v", err)
	}

	// Validate the code
	authCode, err := service.ValidateAuthCode(ctx, code)
	if err != nil {
		t.Fatalf("ValidateAuthCode() error = %v", err)
	}

	if authCode.UserID != 1000123 {
		t.Errorf("UserID = %v, want user-123", authCode.UserID)
	}

	// Verify code is deleted after validation (one-time use)
	_, err = service.storage.GetAuthCode(ctx, code)
	if err == nil {
		t.Error("auth code should be deleted after validation")
	}
}

func TestTokenService_ValidateAuthCode_Expired(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	// Create an expired auth code
	now := time.Now()
	authCode := &AuthCodeData{
		Code:        "expired-code",
		UserID:      1000123,
		ClientID:    "client-123",
		RedirectURI: "https://app.example.com/callback",
		ExpiresAt:   now.Add(-10 * time.Minute), // Expired 10 minutes ago
		CreatedAt:   now.Add(-15 * time.Minute),
	}

	if err := service.storage.StoreAuthCode(ctx, authCode); err != nil {
		t.Fatalf("StoreAuthCode() error = %v", err)
	}

	// Try to validate expired code
	_, err := service.ValidateAuthCode(ctx, "expired-code")
	if err == nil {
		t.Error("ValidateAuthCode() should fail for expired code")
	}
}

// Test RevokeToken with JWT access token
func TestTokenService_RevokeToken_JWT(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	// Generate an access token
	now := time.Now()
	claims := &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "user-123",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "test-jti-456",
		TenantID:  "tenant-1",
		ClientID:  "client-123",
	}

	accessToken, err := service.GenerateAccessToken(ctx, claims)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// Revoke the token
	err = service.RevokeToken(ctx, accessToken)
	if err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}

	// Verify token is revoked
	revoked, err := service.storage.IsTokenRevoked(ctx, claims.JTI)
	if err != nil {
		t.Fatalf("IsTokenRevoked() error = %v", err)
	}

	if !revoked {
		t.Error("token should be revoked")
	}
}

// Test RevokeToken with refresh token
func TestTokenService_RevokeToken_RefreshToken(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	// Generate a refresh token
	refreshToken, err := service.GenerateRefreshToken(ctx, SessionRefreshOptions{Subject: "user-123", ClientID: "client-123"})
	if err != nil {
		t.Fatalf("GenerateRefreshToken() error = %v", err)
	}

	// Get the JTI from storage to verify later
	tokens, _ := service.storage.ListTokensByUser(ctx, "user-123")
	var tokenJTI string
	for _, token := range tokens {
		if token.TokenType == TokenTypeRefresh {
			tokenJTI = token.JTI
			// Store in tokenValues map for lookup
			service.storage.(*mockTokenStorage).tokenValues[refreshToken] = token
			break
		}
	}

	// Revoke the token
	err = service.RevokeToken(ctx, refreshToken)
	if err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}

	// Verify token is revoked
	revoked, err := service.storage.IsTokenRevoked(ctx, tokenJTI)
	if err != nil {
		t.Fatalf("IsTokenRevoked() error = %v", err)
	}

	if !revoked {
		t.Error("refresh token should be revoked")
	}
}

// Test RevokeToken with invalid token
func TestTokenService_RevokeToken_Invalid(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	tests := []struct {
		name       string
		tokenValue string
		wantErr    bool
	}{
		{
			name:       "invalid JWT",
			tokenValue: "invalid.jwt.token",
			wantErr:    true,
		},
		{
			name:       "nonexistent token",
			tokenValue: "nonexistent-token",
			wantErr:    true,
		},
		{
			name:       "empty token",
			tokenValue: "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.RevokeToken(ctx, tt.tokenValue)
			if (err != nil) != tt.wantErr {
				t.Errorf("RevokeToken() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Test RevokeToken error handling
func TestTokenService_RevokeToken_ErrorHandling(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	// Generate an access token
	now := time.Now()
	claims := &Claims{
		Issuer:    "https://test.example.com",
		Subject:   "user-123",
		Audience:  []string{"https://test.example.com"},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "test-jti-789",
		TenantID:  "tenant-1",
		ClientID:  "client-123",
	}

	accessToken, err := service.GenerateAccessToken(ctx, claims)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	// First revocation should succeed
	err = service.RevokeToken(ctx, accessToken)
	if err != nil {
		t.Fatalf("RevokeToken() first call error = %v", err)
	}

	// Second revocation should still succeed (idempotent)
	err = service.RevokeToken(ctx, accessToken)
	if err != nil {
		t.Errorf("RevokeToken() second call should be idempotent, got error = %v", err)
	}
}

// mustGenerateJTI supplies a token identifier to a test that needs one for some
// other reason. It fails the test at the point of the failure rather than
// letting an empty identifier propagate into an assertion further down, where
// the resulting failure would name something that is not the defect.
func mustGenerateJTI(t *testing.T) string {
	t.Helper()
	jti, err := generateJTI()
	if err != nil {
		t.Fatalf("generateJTI() failed: %v", err)
	}
	return jti
}

func TestGenerateJTI(t *testing.T) {
	jti1, err := generateJTI()
	if err != nil {
		t.Fatalf("generateJTI() error = %v", err)
	}
	jti2, err := generateJTI()
	if err != nil {
		t.Fatalf("generateJTI() error = %v", err)
	}

	if jti1 == "" {
		t.Error("generateJTI() returned empty string")
	}

	if jti1 == jti2 {
		t.Error("generateJTI() should return unique values")
	}

	// Should be hex encoded
	if len(jti1) != 32 {
		t.Errorf("generateJTI() length = %d, want 32", len(jti1))
	}
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		name  string
		scope string
		want  []string
	}{
		{
			name:  "single scope",
			scope: "openid",
			want:  []string{"openid"},
		},
		{
			name:  "multiple scopes",
			scope: "openid profile email",
			want:  []string{"openid", "profile", "email"},
		},
		{
			name:  "empty scope",
			scope: "",
			want:  []string{},
		},
		{
			name:  "scope with extra spaces",
			scope: "openid  profile   email",
			want:  []string{"openid", "profile", "email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScopes(tt.scope)
			if len(got) != len(tt.want) {
				t.Errorf("parseScopes() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseScopes()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestValidatePKCES256(t *testing.T) {
	validChallenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	validVerifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	tests := []struct {
		name      string
		verifier  string
		challenge string
		want      bool
	}{
		{
			name:      "valid PKCE",
			verifier:  validVerifier,
			challenge: validChallenge,
			want:      true,
		},
		{
			name:      "invalid verifier",
			verifier:  "wrong-verifier",
			challenge: validChallenge,
			want:      false,
		},
		{
			name:      "empty verifier",
			verifier:  "",
			challenge: validChallenge,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validatePKCES256(tt.verifier, tt.challenge); got != tt.want {
				t.Errorf("validatePKCES256() = %v, want %v", got, tt.want)
			}
		})
	}
}
