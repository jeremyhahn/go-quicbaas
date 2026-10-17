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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// Helper function to add a user to memory storage
func addUserToMemStorage(ctx context.Context, storage *MemoryStorage, user *User) error {
	return storage.CreateUser(ctx, user)
}

func createTestOIDCProvider(t *testing.T) (*OIDCProvider, *rsa.PrivateKey, *mockAppStorage, *mockTokenStorage, *MemoryStorage) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	config := &OIDCConfig{
		Issuer:            "https://test.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    1 * time.Hour,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        1 * time.Hour,
		AuthCodeTTL:       10 * time.Minute,
		PKCERequired:      true,
		AllowedMethods:    []string{CodeChallengeMethodS256}, // OAuth 2.1: S256 only
	}

	appStorage := newMockAppStorage()
	tokenStorage := newMockTokenStorage()
	memStorage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	deps := OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    appStorage,
		TokenStorage:  tokenStorage,
	}

	provider, err := NewOIDCProvider(config, deps)
	if err != nil {
		t.Fatalf("failed to create OIDC provider: %v", err)
	}

	return provider, privateKey, appStorage, tokenStorage, memStorage
}

// Test NewOIDCProvider
func TestOIDCNewOIDCProvider(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}

	config := &OIDCConfig{
		Issuer:            "https://test.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    1 * time.Hour,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        1 * time.Hour,
		AuthCodeTTL:       10 * time.Minute,
	}

	memStorage := NewMemoryStorage()
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	tests := []struct {
		name    string
		config  *OIDCConfig
		deps    OIDCDependencies
		wantErr bool
		errMsg  string
	}{
		{
			name:   "valid config",
			config: config,
			deps: OIDCDependencies{
				IdentityStore: identityStore,
				AppStorage:    newMockAppStorage(),
				TokenStorage:  newMockTokenStorage(),
			},
			wantErr: false,
		},
		{
			name:   "nil config",
			config: nil,
			deps: OIDCDependencies{
				IdentityStore: identityStore,
				AppStorage:    newMockAppStorage(),
				TokenStorage:  newMockTokenStorage(),
			},
			wantErr: true,
			errMsg:  "config is required",
		},
		{
			name:   "nil identity store",
			config: config,
			deps: OIDCDependencies{
				IdentityStore: nil,
				AppStorage:    newMockAppStorage(),
				TokenStorage:  newMockTokenStorage(),
			},
			wantErr: true,
			errMsg:  "identity store is required",
		},
		{
			name:   "nil app storage",
			config: config,
			deps: OIDCDependencies{
				IdentityStore: identityStore,
				AppStorage:    nil,
				TokenStorage:  newMockTokenStorage(),
			},
			wantErr: true,
			errMsg:  "app storage is required",
		},
		{
			name:   "nil token storage",
			config: config,
			deps: OIDCDependencies{
				IdentityStore: identityStore,
				AppStorage:    newMockAppStorage(),
				TokenStorage:  nil,
			},
			wantErr: true,
			errMsg:  "token storage is required",
		},
		{
			name: "nil signing key manager",
			config: &OIDCConfig{
				Issuer:           "https://test.example.com",
				SigningAlgorithm: "RS256",
				AccessTokenTTL:   1 * time.Hour,
			},
			deps: OIDCDependencies{
				IdentityStore: identityStore,
				AppStorage:    newMockAppStorage(),
				TokenStorage:  newMockTokenStorage(),
			},
			wantErr: true,
			errMsg:  "signing key manager is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOIDCProvider(tt.config, tt.deps)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewOIDCProvider() expected error, got nil")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("NewOIDCProvider() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("NewOIDCProvider() unexpected error = %v", err)
				}
				if provider == nil {
					t.Errorf("NewOIDCProvider() returned nil provider")
				}
			}
		})
	}
}

// Test GetConfiguration
func TestOIDCGetConfiguration(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	config := provider.GetConfiguration()

	if config["issuer"] != "https://test.example.com" {
		t.Errorf("GetConfiguration() issuer = %v, want https://test.example.com", config["issuer"])
	}

	if config["authorization_endpoint"] != "https://test.example.com/oauth2/authorize" {
		t.Errorf("GetConfiguration() authorization_endpoint mismatch")
	}

	if config["token_endpoint"] != "https://test.example.com/oauth2/token" {
		t.Errorf("GetConfiguration() token_endpoint mismatch")
	}

	if config["userinfo_endpoint"] != "https://test.example.com/oauth2/userinfo" {
		t.Errorf("GetConfiguration() userinfo_endpoint mismatch")
	}

	responseTypes := config["response_types_supported"].([]string)
	if len(responseTypes) != 1 || responseTypes[0] != "code" {
		t.Errorf("GetConfiguration() response_types_supported = %v, want [code]", responseTypes)
	}

	grantTypes := config["grant_types_supported"].([]string)
	if !contains(grantTypes, "authorization_code") {
		t.Errorf("GetConfiguration() missing authorization_code grant type")
	}

	// Verify OAuth 2.1: Only S256 method is advertised
	codeChallengeMethodsSupported := config["code_challenge_methods_supported"].([]string)
	if len(codeChallengeMethodsSupported) != 1 || codeChallengeMethodsSupported[0] != CodeChallengeMethodS256 {
		t.Errorf("GetConfiguration() code_challenge_methods_supported = %v, want [S256] only per OAuth 2.1", codeChallengeMethodsSupported)
	}
}

// Test GetJWKS
func TestOIDCGetJWKS(t *testing.T) {
	provider, privateKey, _, _, _ := createTestOIDCProvider(t)

	jwks := provider.GetJWKS()

	if jwks == nil {
		t.Fatal("GetJWKS() returned nil")
	}

	if len(jwks.Keys) != 1 {
		t.Errorf("GetJWKS() keys count = %d, want 1", len(jwks.Keys))
	}

	key := jwks.Keys[0]
	wantKID, err := computeKID(privateKey.Public())
	if err != nil {
		t.Fatalf("computeKID() error = %v", err)
	}
	if key.KeyID != wantKID {
		t.Errorf("GetJWKS() key ID = %s, want derived kid %s", key.KeyID, wantKID)
	}

	if key.Algorithm != string(jose.RS256) {
		t.Errorf("GetJWKS() algorithm = %s, want RS256", key.Algorithm)
	}

	if key.Use != "sig" {
		t.Errorf("GetJWKS() use = %s, want sig", key.Use)
	}

	// Verify the public key matches
	pubKey, ok := key.Key.(*rsa.PublicKey)
	if !ok {
		t.Fatal("GetJWKS() key is not RSA public key")
	}

	if pubKey.N.Cmp(privateKey.PublicKey.N) != 0 {
		t.Error("GetJWKS() public key does not match private key")
	}
}

// Test validateAuthorizeRequest
func TestOIDCValidateAuthorizeRequest(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	tests := []struct {
		name    string
		req     *AuthorizeRequest
		wantErr bool
		errCode string
	}{
		{
			name: "valid request",
			req: &AuthorizeRequest{
				ClientID:     "client-123",
				RedirectURI:  "https://example.com/callback",
				ResponseType: ResponseTypeCode,
			},
			wantErr: false,
		},
		{
			name: "missing response_type",
			req: &AuthorizeRequest{
				ClientID:    "client-123",
				RedirectURI: "https://example.com/callback",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidRequest,
		},
		{
			name: "unsupported response_type",
			req: &AuthorizeRequest{
				ClientID:     "client-123",
				RedirectURI:  "https://example.com/callback",
				ResponseType: "token",
			},
			wantErr: true,
			errCode: ErrOAuthUnsupportedResponseType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.validateAuthorizeRequest(tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("validateAuthorizeRequest() expected error, got nil")
				} else {
					oauthErr, ok := err.(*OAuthError)
					if !ok {
						t.Errorf("validateAuthorizeRequest() error type = %T, want *OAuthError", err)
					} else if oauthErr.ErrorCode != tt.errCode {
						t.Errorf("validateAuthorizeRequest() error code = %s, want %s", oauthErr.ErrorCode, tt.errCode)
					}
				}
			} else {
				if err != nil {
					t.Errorf("validateAuthorizeRequest() unexpected error = %v", err)
				}
			}
		})
	}
}

// Test Authorize
func TestOIDCAuthorize(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// Setup test application
	app := &Application{
		ClientID:      "test-client",
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://example.com/callback"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	}
	appStorage.CreateApp(ctx, app)
	appStorage.CreateApp(ctx, &Application{
		ClientID:      "multi-client",
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://example.com/a", "https://example.com/b"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	})

	tests := []struct {
		name    string
		req     *AuthorizeRequest
		wantErr bool
		errCode string
		// direct marks an error that must not be redirected
		// (draft-ietf-oauth-v2-1-16 §4.1.2.1).
		direct bool
	}{
		{
			name: "valid authorization with PKCE",
			req: &AuthorizeRequest{
				ClientID:            "test-client",
				RedirectURI:         "https://example.com/callback",
				ResponseType:        ResponseTypeCode,
				UserID:              123,
				TenantID:            "tenant-1",
				State:               "state-abc",
				CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
				CodeChallengeMethod: CodeChallengeMethodS256,
			},
			wantErr: false,
		},
		{
			name: "missing PKCE (OAuth 2.1)",
			req: &AuthorizeRequest{
				ClientID:     "test-client",
				RedirectURI:  "https://example.com/callback",
				ResponseType: ResponseTypeCode,
				UserID:       123,
				TenantID:     "tenant-1",
				State:        "state-abc",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidRequest,
		},
		{
			name: "invalid client_id",
			req: &AuthorizeRequest{
				ClientID:     "invalid-client",
				RedirectURI:  "https://example.com/callback",
				ResponseType: ResponseTypeCode,
			},
			wantErr: true,
			errCode: ErrOAuthInvalidClient,
			direct:  true,
		},
		{
			name: "invalid redirect_uri",
			req: &AuthorizeRequest{
				ClientID:     "test-client",
				RedirectURI:  "https://malicious.com/callback",
				ResponseType: ResponseTypeCode,
			},
			wantErr: true,
			errCode: ErrOAuthInvalidRequest,
			direct:  true,
		},
		{
			name: "missing client_id",
			req: &AuthorizeRequest{
				RedirectURI:  "https://example.com/callback",
				ResponseType: ResponseTypeCode,
			},
			wantErr: true,
			errCode: ErrOAuthInvalidRequest,
			direct:  true,
		},
		{
			name: "missing redirect_uri with several registered",
			req: &AuthorizeRequest{
				ClientID:     "multi-client",
				ResponseType: ResponseTypeCode,
			},
			wantErr: true,
			errCode: ErrOAuthInvalidRequest,
			direct:  true,
		},
		{
			name: "invalid client_id with missing response_type",
			req: &AuthorizeRequest{
				ClientID:    "invalid-client",
				RedirectURI: "https://example.com/callback",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidClient,
			direct:  true,
		},
		{
			name: "missing response_type after valid client is redirectable",
			req: &AuthorizeRequest{
				ClientID:    "test-client",
				RedirectURI: "https://example.com/callback",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidRequest,
		},
		{
			name: "unsupported response_type after valid client is redirectable",
			req: &AuthorizeRequest{
				ClientID:     "test-client",
				RedirectURI:  "https://example.com/callback",
				ResponseType: "token",
			},
			wantErr: true,
			errCode: ErrOAuthUnsupportedResponseType,
		},
		{
			name: "valid with PKCE S256",
			req: &AuthorizeRequest{
				ClientID:            "test-client",
				RedirectURI:         "https://example.com/callback",
				ResponseType:        ResponseTypeCode,
				UserID:              123,
				TenantID:            "tenant-1",
				CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
				CodeChallengeMethod: CodeChallengeMethodS256,
			},
			wantErr: false,
		},
		{
			name: "plain method rejected per OAuth 2.1",
			req: &AuthorizeRequest{
				ClientID:            "test-client",
				RedirectURI:         "https://example.com/callback",
				ResponseType:        ResponseTypeCode,
				UserID:              123,
				TenantID:            "tenant-1",
				CodeChallenge:       "some-plain-challenge",
				CodeChallengeMethod: CodeChallengeMethodPlain,
			},
			wantErr: true,
			errCode: ErrOAuthInvalidRequest,
		},
		{
			name: "empty method rejected per OAuth 2.1 §4.1.1",
			req: &AuthorizeRequest{
				ClientID:            "test-client",
				RedirectURI:         "https://example.com/callback",
				ResponseType:        ResponseTypeCode,
				UserID:              123,
				TenantID:            "tenant-1",
				CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
				CodeChallengeMethod: "",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := provider.Authorize(ctx, tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("Authorize() expected error, got nil")
				} else {
					var oauthErr *OAuthError
					if !errors.As(err, &oauthErr) {
						t.Errorf("Authorize() error type = %T, want *OAuthError", err)
					} else if oauthErr.ErrorCode != tt.errCode {
						t.Errorf("Authorize() error code = %s, want %s", oauthErr.ErrorCode, tt.errCode)
					}
					var directErr *AuthorizeDirectError
					if got := errors.As(err, &directErr); got != tt.direct {
						t.Errorf("Authorize() direct error = %v, want %v", got, tt.direct)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Authorize() unexpected error = %v", err)
				}
				if resp == nil {
					t.Error("Authorize() returned nil response")
				} else {
					if resp.Code == "" {
						t.Error("Authorize() returned empty code")
					}
					if tt.req.State != "" && resp.State != tt.req.State {
						t.Errorf("Authorize() state = %s, want %s", resp.State, tt.req.State)
					}
				}
			}
		})
	}
}

// Test Token with authorization code grant
func TestOIDCTokenAuthorizationCode(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, tokenStorage, memStorage := createTestOIDCProvider(t)
	ctx := context.Background()

	// Hash the client secret
	hashedSecret := currentSecretHash(t, "test-secret")

	// Setup test data
	app := &Application{
		ClientID:      "test-client",
		ClientSecret:  hashedSecret,
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://example.com/callback"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	}
	appStorage.CreateApp(ctx, app)

	user := &User{
		ID:        123,
		TenantID:  "tenant-1",
		Email:     "test@example.com",
		Lifecycle: LifecycleActive,
		Name:      "Test User",
		Roles:     []string{"user"},
	}
	addUserToMemStorage(ctx, memStorage, user)

	// Create authorization code
	authCode := &AuthCodeData{
		Code:        "test-code",
		UserID:      123,
		TenantID:    "tenant-1",
		ClientID:    "test-client",
		RedirectURI: "https://example.com/callback",
		Scope:       []string{"openid", "profile", "email"},
		ExpiresAt:   time.Now().Add(10 * time.Minute),
		CreatedAt:   time.Now(),
	}
	tokenStorage.StoreAuthCode(ctx, authCode)

	tests := []struct {
		name    string
		req     *TokenRequest
		wantErr bool
		errCode string
	}{
		{
			name: "valid token request",
			req: &TokenRequest{
				GrantType:    GrantTypeAuthorizationCode,
				Code:         "test-code",
				RedirectURI:  "https://example.com/callback",
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
			wantErr: false,
		},
		{
			name: "invalid client credentials",
			req: &TokenRequest{
				GrantType:    GrantTypeAuthorizationCode,
				Code:         "test-code",
				RedirectURI:  "https://example.com/callback",
				ClientID:     "test-client",
				ClientSecret: "wrong-secret",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidClient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Re-store auth code for each test
			if tt.req.Code == "test-code" {
				tokenStorage.StoreAuthCode(ctx, authCode)
			}

			resp, err := provider.Token(ctx, tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("Token() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Token() unexpected error = %v", err)
				}
				if resp == nil {
					t.Fatal("Token() returned nil response")
				}
				if resp.AccessToken == "" {
					t.Error("Token() returned empty access token")
				}
				if resp.RefreshToken == "" {
					t.Error("Token() returned empty refresh token")
				}
				if resp.IDToken == "" {
					t.Error("Token() returned empty ID token (openid scope present)")
				}
			}
		})
	}
}

// Test Refresh function
func TestOIDCRefresh(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, tokenStorage, memStorage := createTestOIDCProvider(t)
	ctx := context.Background()

	// Hash the client secret
	hashedSecret := currentSecretHash(t, "test-secret")

	// Setup test data
	app := &Application{
		ClientID:     "test-client",
		ClientSecret: hashedSecret,
		TenantID:     "tenant-1",
		GrantTypes:   []string{GrantTypeRefreshToken},
	}
	appStorage.CreateApp(ctx, app)

	user := &User{
		ID:        123,
		TenantID:  "tenant-1",
		Email:     "test@example.com",
		Lifecycle: LifecycleActive,
		Name:      "Test User",
		Roles:     []string{"user"},
	}
	addUserToMemStorage(ctx, memStorage, user)

	// Create refresh token
	refreshToken := &Token{
		JTI:       "refresh-token-123",
		Subject:   "123",
		TenantID:  "tenant-1",
		ClientID:  "test-client",
		Scopes:    []string{"openid", "profile"},
		TokenType: TokenTypeRefresh,
		Expires:   time.Now().Add(24 * time.Hour),
		Issued:    time.Now(),
		Revoked:   false,
	}
	tokenStorage.StoreToken(ctx, refreshToken)
	tokenStorage.tokenValues["refresh-token-value"] = refreshToken

	tests := []struct {
		name    string
		req     *RefreshRequest
		wantErr bool
		errCode string
	}{
		{
			name: "valid refresh",
			req: &RefreshRequest{
				RefreshToken: "refresh-token-value",
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
			wantErr: false,
		},
		{
			name: "invalid client secret",
			req: &RefreshRequest{
				RefreshToken: "refresh-token-value",
				ClientID:     "test-client",
				ClientSecret: "wrong-secret",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidClient,
		},
		{
			name: "invalid refresh token",
			req: &RefreshRequest{
				RefreshToken: "invalid-token",
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidGrant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := provider.Refresh(ctx, tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("Refresh() expected error, got nil")
				} else {
					oauthErr, ok := err.(*OAuthError)
					if !ok {
						t.Errorf("Refresh() error type = %T, want *OAuthError", err)
					} else if oauthErr.ErrorCode != tt.errCode {
						t.Errorf("Refresh() error code = %s, want %s", oauthErr.ErrorCode, tt.errCode)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Refresh() unexpected error = %v", err)
				}
				if resp == nil {
					t.Fatal("Refresh() returned nil response")
				}
				if resp.AccessToken == "" {
					t.Error("Refresh() returned empty access token")
				}
			}
		})
	}
}

// Test Revoke function
func TestOIDCRevoke(t *testing.T) {
	t.Parallel()

	provider, _, appStorage, tokenStorage, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// Hash the client secret
	hashedSecret := currentSecretHash(t, "test-secret")

	// Setup test data
	app := &Application{
		ClientID:     "test-client",
		ClientSecret: hashedSecret,
		TenantID:     "tenant-1",
	}
	appStorage.CreateApp(ctx, app)

	// Generate access token for testing
	now := time.Now()
	claims := &Claims{
		Issuer:    provider.config.Issuer,
		Subject:   "123",
		Audience:  []string{provider.config.Issuer},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "token-jti-123",
		TenantID:  "tenant-1",
		ClientID:  "test-client",
	}
	accessToken, _ := provider.tokenService.GenerateAccessToken(ctx, claims)

	tests := []struct {
		name    string
		req     *RevokeRequest
		wantErr bool
		errCode string
	}{
		{
			name: "valid revocation",
			req: &RevokeRequest{
				Token:        accessToken,
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
			wantErr: false,
		},
		{
			name: "invalid client secret",
			req: &RevokeRequest{
				Token:        accessToken,
				ClientID:     "test-client",
				ClientSecret: "wrong-secret",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidClient,
		},
		{
			name: "invalid client id",
			req: &RevokeRequest{
				Token:        accessToken,
				ClientID:     "invalid-client",
				ClientSecret: "test-secret",
			},
			wantErr: true,
			errCode: ErrOAuthInvalidClient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.Revoke(ctx, tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("Revoke() expected error, got nil")
				} else {
					oauthErr, ok := err.(*OAuthError)
					if !ok {
						t.Errorf("Revoke() error type = %T, want *OAuthError", err)
					} else if oauthErr.ErrorCode != tt.errCode {
						t.Errorf("Revoke() error code = %s, want %s", oauthErr.ErrorCode, tt.errCode)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Revoke() unexpected error = %v", err)
				}

				// Verify token is revoked
				revoked, _ := tokenStorage.IsTokenRevoked(ctx, claims.JTI)
				if !revoked {
					t.Error("Revoke() did not revoke the token")
				}
			}
		})
	}
}

// TestOIDCRevoke_RefreshTokenOwnership covers RFC 7009 §2.1 for opaque refresh
// tokens: the stored record names the client, and only that client may revoke.
func TestOIDCRevoke_RefreshTokenOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	newProvider := func(t *testing.T) (*OIDCProvider, *Token) {
		t.Helper()
		provider, _, appStorage, tokenStorage, _ := createTestOIDCProvider(t)
		for _, clientID := range []string{"client-a", "client-b"} {
			if err := appStorage.CreateApp(ctx, &Application{
				ClientID:     clientID,
				ClientSecret: currentSecretHash(t, clientID+"-secret"),
				TenantID:     "tenant-1",
			}); err != nil {
				t.Fatalf("CreateApp(%s): %v", clientID, err)
			}
		}
		refresh := &Token{
			JTI:       "refresh-jti-a",
			Subject:   "123",
			TenantID:  "tenant-1",
			TokenType: TokenTypeRefresh,
			ClientID:  "client-a",
			Expires:   time.Now().Add(time.Hour),
		}
		tokenStorage.tokens[refresh.JTI] = refresh
		tokenStorage.tokenValues["refresh-value-a"] = refresh
		return provider, refresh
	}

	t.Run("owner revokes its refresh token", func(t *testing.T) {
		provider, refresh := newProvider(t)
		err := provider.Revoke(ctx, &RevokeRequest{
			Token: "refresh-value-a", ClientID: "client-a", ClientSecret: "client-a-secret",
		})
		if err != nil {
			t.Fatalf("Revoke() unexpected error = %v", err)
		}
		if !refresh.Revoked {
			t.Error("Revoke() did not revoke the owner's refresh token")
		}
	})

	t.Run("another client is refused", func(t *testing.T) {
		provider, refresh := newProvider(t)
		err := provider.Revoke(ctx, &RevokeRequest{
			Token: "refresh-value-a", ClientID: "client-b", ClientSecret: "client-b-secret",
		})
		if !errors.Is(err, ErrRevokeTokenNotOwned) {
			t.Fatalf("Revoke() error = %v, want ErrRevokeTokenNotOwned", err)
		}
		if refresh.Revoked {
			t.Error("Revoke() revoked a token issued to another client")
		}
	})

	t.Run("unknown token is not an error", func(t *testing.T) {
		provider, _ := newProvider(t)
		err := provider.Revoke(ctx, &RevokeRequest{
			Token: "never-issued", ClientID: "client-a", ClientSecret: "client-a-secret",
		})
		if err != nil {
			t.Fatalf("Revoke() unexpected error = %v", err)
		}
	})
}

// Test UserInfo function
func TestOIDCUserInfo(t *testing.T) {
	provider, _, _, _, memStorage := createTestOIDCProvider(t)
	ctx := context.Background()

	now := time.Now()

	// Setup test user
	user := &User{
		ID:         123,
		TenantID:   "tenant-1",
		Email:      "test@example.com",
		Lifecycle:  LifecycleActive,
		Name:       "Test User",
		Roles:      []string{"admin", "user"},
		Attributes: map[string]string{"department": "engineering"},
		Created:    now,
		Updated:    now,
	}
	addUserToMemStorage(ctx, memStorage, user)

	// Generate valid access token
	claims := &Claims{
		Issuer:    provider.config.Issuer,
		Subject:   "123",
		Audience:  []string{provider.config.Issuer},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "token-jti-456",
		TenantID:  "tenant-1",
		// The uid claim names the record. UserInfo requires it (#390): the
		// subject-string fallback it used to fall back to resolved a non-email
		// subject by id and then by USERNAME, which let a service token's client
		// id match a user who had taken that string as their handle.
		UserID:   123,
		Roles:    []string{"admin", "user"},
		Groups:   []string{"developers", "team-a"},
		ClientID: "test-client",
	}
	accessToken, _ := provider.tokenService.GenerateAccessToken(ctx, claims)

	tests := []struct {
		name        string
		accessToken string
		wantErr     bool
		errCode     string
	}{
		{
			name:        "valid access token",
			accessToken: accessToken,
			wantErr:     false,
		},
		{
			name:        "invalid access token",
			accessToken: "invalid.token.here",
			wantErr:     true,
			errCode:     ErrOAuthInvalidGrant,
		},
		{
			name:        "empty access token",
			accessToken: "",
			wantErr:     true,
			errCode:     ErrOAuthInvalidGrant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userInfo, err := provider.UserInfo(ctx, tt.accessToken)
			if tt.wantErr {
				if err == nil {
					t.Error("UserInfo() expected error, got nil")
				} else {
					oauthErr, ok := err.(*OAuthError)
					if !ok {
						t.Errorf("UserInfo() error type = %T, want *OAuthError", err)
					} else if oauthErr.ErrorCode != tt.errCode {
						t.Errorf("UserInfo() error code = %s, want %s", oauthErr.ErrorCode, tt.errCode)
					}
				}
			} else {
				if err != nil {
					t.Errorf("UserInfo() unexpected error = %v", err)
				}
				if userInfo == nil {
					t.Fatal("UserInfo() returned nil response")
				}
				if userInfo.Sub != "123" {
					t.Errorf("UserInfo() sub = %s, want user-123", userInfo.Sub)
				}
				if userInfo.Email != "test@example.com" {
					t.Errorf("UserInfo() email = %s, want test@example.com", userInfo.Email)
				}
				if userInfo.Name != "Test User" {
					t.Errorf("UserInfo() name = %s, want Test User", userInfo.Name)
				}
				if !userInfo.EmailVerified {
					t.Error("UserInfo() email_verified should be true")
				}
				if len(userInfo.Roles) != 2 {
					t.Errorf("UserInfo() roles length = %d, want 2", len(userInfo.Roles))
				}
				if len(userInfo.Groups) != 2 {
					t.Errorf("UserInfo() groups length = %d, want 2", len(userInfo.Groups))
				}
			}
		})
	}
}

// Test ValidateAccessToken function
func TestOIDCValidateAccessToken(t *testing.T) {
	provider, _, _, tokenStorage, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// Generate valid access token
	now := time.Now()
	claims := &Claims{
		Issuer:    provider.config.Issuer,
		Subject:   "123",
		Audience:  []string{provider.config.Issuer},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "token-jti-789",
		TenantID:  "tenant-1",
		Email:     "test@example.com",
		Name:      "Test User",
		Scope:     "openid profile email",
		ClientID:  "test-client",
	}
	accessToken, _ := provider.tokenService.GenerateAccessToken(ctx, claims)

	// Create a revoked token
	revokedClaims := &Claims{
		Issuer:    provider.config.Issuer,
		Subject:   "user-456",
		Audience:  []string{provider.config.Issuer},
		ExpiresAt: NewUnixTime(now.Add(1 * time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "revoked-token-jti",
		TenantID:  "tenant-1",
		ClientID:  "test-client",
	}
	revokedToken, _ := provider.tokenService.GenerateAccessToken(ctx, revokedClaims)
	tokenStorage.RevokeToken(ctx, "revoked-token-jti")

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "valid access token",
			token:   accessToken,
			wantErr: false,
		},
		{
			name:    "revoked token",
			token:   revokedToken,
			wantErr: true,
		},
		{
			name:    "invalid token format",
			token:   "invalid.token.here",
			wantErr: true,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validatedClaims, err := provider.ValidateAccessToken(ctx, tt.token)
			if tt.wantErr {
				if err == nil {
					t.Error("ValidateAccessToken() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("ValidateAccessToken() unexpected error = %v", err)
				}
				if validatedClaims == nil {
					t.Fatal("ValidateAccessToken() returned nil claims")
				}
				if validatedClaims.Subject != claims.Subject {
					t.Errorf("ValidateAccessToken() subject = %s, want %s", validatedClaims.Subject, claims.Subject)
				}
				if validatedClaims.JTI != claims.JTI {
					t.Errorf("ValidateAccessToken() jti = %s, want %s", validatedClaims.JTI, claims.JTI)
				}
			}
		})
	}
}

// Test ValidateIDToken function
func TestOIDCValidateIDToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// Generate valid ID token
	now := time.Now()
	idClaims := &IDTokenClaims{
		Issuer:        provider.config.Issuer,
		Subject:       "123",
		Audience:      []string{"test-client"},
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
	idToken, _ := provider.tokenService.GenerateIDToken(ctx, idClaims)

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "valid ID token",
			token:   idToken,
			wantErr: false,
		},
		{
			name:    "invalid token format",
			token:   "invalid.token.here",
			wantErr: true,
		},
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validatedClaims, err := provider.ValidateIDToken(ctx, tt.token)
			if tt.wantErr {
				if err == nil {
					t.Error("ValidateIDToken() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("ValidateIDToken() unexpected error = %v", err)
				}
				if validatedClaims == nil {
					t.Fatal("ValidateIDToken() returned nil claims")
				}
				if validatedClaims.Subject != idClaims.Subject {
					t.Errorf("ValidateIDToken() subject = %s, want %s", validatedClaims.Subject, idClaims.Subject)
				}
				// Note: Custom claims like Nonce are not automatically populated in the parsed token
				// They would need to be extracted manually if needed
			}
		})
	}
}

// Test contains helper
func TestOIDCContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		item  string
		want  bool
	}{
		{
			name:  "item present",
			slice: []string{"a", "b", "c"},
			item:  "b",
			want:  true,
		},
		{
			name:  "item not present",
			slice: []string{"a", "b", "c"},
			item:  "d",
			want:  false,
		},
		{
			name:  "empty slice",
			slice: []string{},
			item:  "a",
			want:  false,
		},
		{
			name:  "nil slice",
			slice: nil,
			item:  "a",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.slice, tt.item)
			if got != tt.want {
				t.Errorf("contains() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test joinScopes helper
func TestOIDCJoinScopes(t *testing.T) {
	tests := []struct {
		name   string
		scopes []string
		want   string
	}{
		{
			name:   "multiple scopes",
			scopes: []string{"openid", "profile", "email"},
			want:   "openid profile email",
		},
		{
			name:   "single scope",
			scopes: []string{"openid"},
			want:   "openid",
		},
		{
			name:   "empty scopes",
			scopes: []string{},
			want:   "",
		},
		{
			name:   "nil scopes",
			scopes: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinScopes(tt.scopes)
			if got != tt.want {
				t.Errorf("joinScopes() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Test edge case with base64 encoding
func TestOIDCAuthCodeWithPKCEBase64(t *testing.T) {
	// Test valid base64url encoded challenge/verifier
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	challenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	// Decode to verify they're valid base64url
	_, err := base64.RawURLEncoding.DecodeString(verifier)
	if err != nil {
		t.Errorf("failed to decode verifier: %v", err)
	}

	_, err = base64.RawURLEncoding.DecodeString(challenge)
	if err != nil {
		t.Errorf("failed to decode challenge: %v", err)
	}
}

// Test GetOAuthASMetadata returns only S256 per OAuth 2.1
func TestOIDCGetOAuthASMetadata(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	metadata := provider.GetOAuthASMetadata()

	if metadata == nil {
		t.Fatal("GetOAuthASMetadata() returned nil")
	}

	// Verify OAuth 2.1: Only S256 method is advertised
	if len(metadata.CodeChallengeMethodsSupported) != 1 {
		t.Errorf("GetOAuthASMetadata() CodeChallengeMethodsSupported length = %d, want 1", len(metadata.CodeChallengeMethodsSupported))
	}

	if metadata.CodeChallengeMethodsSupported[0] != CodeChallengeMethodS256 {
		t.Errorf("GetOAuthASMetadata() CodeChallengeMethodsSupported[0] = %s, want %s", metadata.CodeChallengeMethodsSupported[0], CodeChallengeMethodS256)
	}
}

// Test ErrPKCEPlainMethodNotAllowed error
func TestErrPKCEPlainMethodNotAllowed(t *testing.T) {
	// Verify the error is properly defined
	if ErrPKCEPlainMethodNotAllowed == nil {
		t.Fatal("ErrPKCEPlainMethodNotAllowed is nil")
	}

	if ErrPKCEPlainMethodNotAllowed.ErrorCode != ErrOAuthInvalidRequest {
		t.Errorf("ErrPKCEPlainMethodNotAllowed.ErrorCode = %s, want %s", ErrPKCEPlainMethodNotAllowed.ErrorCode, ErrOAuthInvalidRequest)
	}

	if !strings.Contains(ErrPKCEPlainMethodNotAllowed.ErrorDescription, "plain") {
		t.Errorf("ErrPKCEPlainMethodNotAllowed.ErrorDescription should mention 'plain', got: %s", ErrPKCEPlainMethodNotAllowed.ErrorDescription)
	}

	if !strings.Contains(ErrPKCEPlainMethodNotAllowed.ErrorDescription, "OAuth 2.1") {
		t.Errorf("ErrPKCEPlainMethodNotAllowed.ErrorDescription should mention 'OAuth 2.1', got: %s", ErrPKCEPlainMethodNotAllowed.ErrorDescription)
	}

	if !strings.Contains(ErrPKCEPlainMethodNotAllowed.ErrorDescription, "S256") {
		t.Errorf("ErrPKCEPlainMethodNotAllowed.ErrorDescription should mention 'S256', got: %s", ErrPKCEPlainMethodNotAllowed.ErrorDescription)
	}

	// Test the Error() method
	errStr := ErrPKCEPlainMethodNotAllowed.Error()
	if !strings.Contains(errStr, "invalid_request") {
		t.Errorf("ErrPKCEPlainMethodNotAllowed.Error() should contain 'invalid_request', got: %s", errStr)
	}
}

// TestOIDCAuthorizeRequiresCodeChallengeMethod pins draft-ietf-oauth-v2-1-16
// §4.1.1: code_challenge_method is REQUIRED, so a request that omits it is
// refused with ErrPKCEMethodRequired instead of defaulting to S256.
func TestOIDCAuthorizeRequiresCodeChallengeMethod(t *testing.T) {
	t.Parallel()
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()
	appStorage.CreateApp(ctx, &Application{
		ClientID:      "test-client",
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://example.com/callback"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	})

	req := &AuthorizeRequest{
		ClientID:      "test-client",
		RedirectURI:   "https://example.com/callback",
		ResponseType:  ResponseTypeCode,
		UserID:        123,
		TenantID:      "tenant-1",
		CodeChallenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
	}

	resp, err := provider.Authorize(ctx, req)
	if !errors.Is(err, ErrPKCEMethodRequired) {
		t.Fatalf("Authorize() error = %v, want ErrPKCEMethodRequired", err)
	}
	if resp != nil {
		t.Errorf("Authorize() response = %+v, want nil", resp)
	}
	if req.CodeChallengeMethod != "" {
		t.Errorf("Authorize() rewrote CodeChallengeMethod to %q", req.CodeChallengeMethod)
	}
	if ErrPKCEMethodRequired.ErrorCode != ErrOAuthInvalidRequest {
		t.Errorf("ErrPKCEMethodRequired.ErrorCode = %s, want %s", ErrPKCEMethodRequired.ErrorCode, ErrOAuthInvalidRequest)
	}
}

// =============================================================================
// RFC 8707 Resource Indicators Tests
// =============================================================================

// TestOIDCAuthorizeWithResourceIndicator tests authorization with RFC 8707 resource parameter.
func TestOIDCAuthorizeWithResourceIndicator(t *testing.T) {
	provider, _, appStorage, tokenStorage, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// Setup test application
	app := &Application{
		ClientID:      "test-client",
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://example.com/callback"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	}
	appStorage.CreateApp(ctx, app)

	// Create authorization request with resource parameter
	req := &AuthorizeRequest{
		ClientID:            "test-client",
		RedirectURI:         "https://example.com/callback",
		ResponseType:        ResponseTypeCode,
		UserID:              123,
		TenantID:            "tenant-1",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: CodeChallengeMethodS256,
		Resource:            "https://api.example.com/mcp",
	}

	resp, err := provider.Authorize(ctx, req)
	if err != nil {
		t.Fatalf("Authorize() unexpected error = %v", err)
	}
	if resp == nil || resp.Code == "" {
		t.Fatal("Authorize() returned nil or empty response")
	}

	// Verify the resource is stored with the auth code
	authCode, err := tokenStorage.GetAuthCode(ctx, resp.Code)
	if err != nil {
		t.Fatalf("GetAuthCode() error = %v", err)
	}
	if authCode.Resource != "https://api.example.com/mcp" {
		t.Errorf("AuthCode.Resource = %s, want https://api.example.com/mcp", authCode.Resource)
	}
}

// TestOIDCTokenWithResourceIndicator tests token exchange with RFC 8707 resource parameter.
func TestOIDCTokenWithResourceIndicator(t *testing.T) {
	t.Parallel()

	provider, privateKey, appStorage, tokenStorage, memStorage := createTestOIDCProvider(t)
	ctx := context.Background()

	// Hash the client secret
	hashedSecret := currentSecretHash(t, "test-secret")

	// Setup test data
	app := &Application{
		ClientID:      "test-client",
		ClientSecret:  hashedSecret,
		TenantID:      "tenant-1",
		RedirectURIs:  []string{"https://example.com/callback"},
		ResponseTypes: []string{ResponseTypeCode},
		GrantTypes:    []string{GrantTypeAuthorizationCode},
	}
	appStorage.CreateApp(ctx, app)

	user := &User{
		ID:        123,
		TenantID:  "tenant-1",
		Email:     "test@example.com",
		Lifecycle: LifecycleActive,
		Name:      "Test User",
		Roles:     []string{"user"},
	}
	addUserToMemStorage(ctx, memStorage, user)

	resourceURI := "https://api.example.com/mcp"

	tests := []struct {
		name             string
		authCodeResource string
		tokenReqResource string
		wantErr          bool
		errCode          string
		checkAudience    bool
		expectedAudience []string
	}{
		{
			name:             "resource in auth code, no resource in token request",
			authCodeResource: resourceURI,
			tokenReqResource: "",
			wantErr:          false,
			checkAudience:    true,
			expectedAudience: []string{"https://test.example.com", resourceURI},
		},
		{
			name:             "resource matches between auth code and token request",
			authCodeResource: resourceURI,
			tokenReqResource: resourceURI,
			wantErr:          false,
			checkAudience:    true,
			expectedAudience: []string{"https://test.example.com", resourceURI},
		},
		{
			name:             "resource mismatch between auth code and token request",
			authCodeResource: resourceURI,
			tokenReqResource: "https://other.example.com/mcp",
			wantErr:          true,
			errCode:          ErrOAuthInvalidGrant,
		},
		{
			name:             "no resource in either",
			authCodeResource: "",
			tokenReqResource: "",
			wantErr:          false,
			checkAudience:    true,
			expectedAudience: []string{"https://test.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create authorization code with resource
			authCode := &AuthCodeData{
				Code:        "test-code-" + tt.name,
				UserID:      123,
				TenantID:    "tenant-1",
				ClientID:    "test-client",
				RedirectURI: "https://example.com/callback",
				Scope:       []string{"openid", "profile"},
				Resource:    tt.authCodeResource,
				ExpiresAt:   time.Now().Add(10 * time.Minute),
				CreatedAt:   time.Now(),
			}
			tokenStorage.StoreAuthCode(ctx, authCode)

			// Make token request
			tokenReq := &TokenRequest{
				GrantType:    GrantTypeAuthorizationCode,
				Code:         authCode.Code,
				RedirectURI:  "https://example.com/callback",
				ClientID:     "test-client",
				ClientSecret: "test-secret",
				Resource:     tt.tokenReqResource,
			}

			resp, err := provider.Token(ctx, tokenReq)

			if tt.wantErr {
				if err == nil {
					t.Error("Token() expected error, got nil")
				} else {
					oauthErr, ok := err.(*OAuthError)
					if !ok {
						t.Errorf("Token() error type = %T, want *OAuthError", err)
					} else if oauthErr.ErrorCode != tt.errCode {
						t.Errorf("Token() error code = %s, want %s", oauthErr.ErrorCode, tt.errCode)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Token() unexpected error = %v", err)
					return
				}
				if resp == nil {
					t.Fatal("Token() returned nil response")
				}

				// Verify audience in token
				if tt.checkAudience {
					claims, err := xkms.JWTVerifyWithKeyAndClaims(resp.AccessToken, &privateKey.PublicKey, "RS256")
					if err != nil {
						t.Fatalf("Failed to parse access token: %v", err)
					}

					audience, ok := claims["aud"].([]interface{})
					if !ok {
						t.Fatalf("Token audience claim is not []interface{}: %T", claims["aud"])
					}

					if len(audience) != len(tt.expectedAudience) {
						t.Errorf("Token audience length = %d, want %d", len(audience), len(tt.expectedAudience))
					}

					for i, expected := range tt.expectedAudience {
						if i < len(audience) {
							if aud, ok := audience[i].(string); ok {
								if aud != expected {
									t.Errorf("Token audience[%d] = %s, want %s", i, aud, expected)
								}
							}
						}
					}
				}
			}
		})
	}
}

// TestOIDCClientCredentialsWithResourceIndicator tests client credentials grant with resource parameter.
func TestOIDCClientCredentialsWithResourceIndicator(t *testing.T) {
	t.Parallel()

	provider, privateKey, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	// Hash the client secret
	hashedSecret := currentSecretHash(t, "test-secret")

	// Setup test application with client_credentials grant
	app := &Application{
		ClientID:     "service-client",
		ClientSecret: hashedSecret,
		TenantID:     "tenant-1",
		GrantTypes:   []string{GrantTypeClientCredentials},
		// The permission this fixture requests. A client_credentials token
		// carries the grant on its application record, not the scope the
		// client named (go-quicbaas#653).
		Scopes: []string{"mcp:tools"},
	}
	appStorage.CreateApp(ctx, app)

	tests := []struct {
		name             string
		resource         string
		expectedAudience []string
	}{
		{
			name:             "with resource parameter",
			resource:         "https://api.example.com/mcp",
			expectedAudience: []string{"https://test.example.com", "https://api.example.com/mcp"},
		},
		{
			name:             "without resource parameter",
			resource:         "",
			expectedAudience: []string{"https://test.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenReq := &TokenRequest{
				GrantType:    GrantTypeClientCredentials,
				ClientID:     "service-client",
				ClientSecret: "test-secret",
				Scope:        []string{"mcp:tools"},
				Resource:     tt.resource,
			}

			resp, err := provider.Token(ctx, tokenReq)
			if err != nil {
				t.Fatalf("Token() unexpected error = %v", err)
			}
			if resp == nil {
				t.Fatal("Token() returned nil response")
			}

			// Verify audience in token
			claims, err := xkms.JWTVerifyWithKeyAndClaims(resp.AccessToken, &privateKey.PublicKey, "RS256")
			if err != nil {
				t.Fatalf("Failed to parse access token: %v", err)
			}

			audience, ok := claims["aud"].([]interface{})
			if !ok {
				t.Fatalf("Token audience claim is not []interface{}: %T", claims["aud"])
			}

			if len(audience) != len(tt.expectedAudience) {
				t.Errorf("Token audience length = %d, want %d", len(audience), len(tt.expectedAudience))
			}

			for i, expected := range tt.expectedAudience {
				if i < len(audience) {
					if aud, ok := audience[i].(string); ok {
						if aud != expected {
							t.Errorf("Token audience[%d] = %s, want %s", i, aud, expected)
						}
					}
				}
			}
		})
	}
}

// TestOIDCResourceIndicatorsSupportedInMetadata tests that resource indicators are advertised in metadata.
func TestOIDCResourceIndicatorsSupportedInMetadata(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	metadata := provider.GetOAuthASMetadata()
	if metadata == nil {
		t.Fatal("GetOAuthASMetadata() returned nil")
	}

	if !metadata.ResourceIndicatorsSupported {
		t.Error("GetOAuthASMetadata().ResourceIndicatorsSupported should be true")
	}
}

// TestOIDCIssParameterSupportedInMetadata covers RFC 9207 §3 (#1444): both
// metadata documents advertise the iss authorization response parameter.
func TestOIDCIssParameterSupportedInMetadata(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)

	if got := provider.GetConfiguration()["authorization_response_iss_parameter_supported"]; got != true {
		t.Errorf("GetConfiguration() authorization_response_iss_parameter_supported = %v, want true", got)
	}
	if !provider.GetOAuthASMetadata().IsAuthorizationResponseIssParameterSupported() {
		t.Error("GetOAuthASMetadata().AuthorizationResponseIssParameterSupported should be true")
	}
}

// TestOIDCUserInfo_CeremonyBearerWithoutUIDResolvesNobody replaces a test that
// asserted the opposite: that a ceremony bearer whose sub is a USERNAME resolves
// through the username index.
//
// That fallback is gone (#390). It resolved a non-email subject by id and then
// by username, and the only subject that reaches it today belongs to a SERVICE
// token — so a user who took the client id as their handle had this endpoint
// answer that client's request with their own profile. Every current ceremony
// bearer carries uid (oidc_session.go), so nothing legitimate depended on it.
func TestOIDCUserInfo_CeremonyBearerWithoutUIDResolvesNobody(t *testing.T) {
	provider, _, _, _, memStorage := createTestOIDCProvider(t)
	ctx := context.Background()

	now := time.Now()
	user := &User{
		ID:        999,
		TenantID:  "system",
		Email:     "admin@example.com",
		Username:  "admin",
		Name:      "Admin",
		Lifecycle: LifecycleActive,
		Roles:     []string{"admin"},
		Created:   now,
		Updated:   now,
	}
	if err := addUserToMemStorage(ctx, memStorage, user); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	claims := &Claims{
		Issuer:    provider.config.Issuer,
		Subject:   "admin", // a username sub, with no uid — the pre-contract shape
		Audience:  []string{provider.config.Issuer},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "ceremony-jti-1",
		TenantID:  "system",
		Roles:     []string{"admin"},
	}
	accessToken, err := provider.tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		t.Fatalf("mint ceremony-style bearer: %v", err)
	}

	if _, err := provider.UserInfo(ctx, accessToken); err == nil {
		t.Fatal("a bearer carrying no uid must not have a user guessed for it from its subject")
	}
}

// TestOIDCUserInfo_UnknownSubjectFailsClosed proves UserInfo still fails
// closed when the subject matches neither a user ID nor a username.
func TestOIDCUserInfo_UnknownSubjectFailsClosed(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	now := time.Now()
	claims := &Claims{
		Issuer:    provider.config.Issuer,
		Subject:   "ghost",
		Audience:  []string{provider.config.Issuer},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       "ceremony-jti-2",
		TenantID:  "system",
	}
	accessToken, err := provider.tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		t.Fatalf("mint bearer: %v", err)
	}

	if _, err := provider.UserInfo(ctx, accessToken); err == nil {
		t.Fatal("UserInfo() must fail closed for an unknown subject")
	}
}
