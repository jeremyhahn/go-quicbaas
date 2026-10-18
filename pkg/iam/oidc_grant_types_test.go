package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grantTypesFixture is a provider with one confidential client registered
// for exactly the grant types the test names.
type grantTypesFixture struct {
	provider     *OIDCProvider
	tokenStorage *mockTokenStorage
	clientID     string
	secret       string
}

func newGrantTypesFixture(t *testing.T, grantTypes ...string) *grantTypesFixture {
	t.Helper()
	ctx := context.Background()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	memStorage := NewMemoryStorage()
	appStorage := newMockAppStorage()
	tokenStorage := newMockTokenStorage()

	secret := "grant-types-secret"
	app := &Application{
		ClientID:      "grant-types-client",
		ClientSecret:  hashClientSecretFinal(t, secret),
		TenantID:      "tenant-grant-types",
		Name:          "Grant Types App",
		GrantTypes:    grantTypes,
		ResponseTypes: []string{ResponseTypeCode},
		RedirectURIs:  []string{"https://app.example/cb"},
		Created:       time.Now(),
		Updated:       time.Now(),
	}
	require.NoError(t, appStorage.CreateApp(ctx, app))

	provider, err := NewOIDCProvider(&OIDCConfig{
		Issuer:            "https://auth.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testSigningKeyManager(t, privateKey, "RS256"),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       10 * time.Minute,
	}, OIDCDependencies{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{UserStorage: memStorage, RoleStorage: memStorage}),
		AppStorage:    appStorage,
		TokenStorage:  tokenStorage,
	})
	require.NoError(t, err)

	return &grantTypesFixture{provider: provider, tokenStorage: tokenStorage, clientID: app.ClientID, secret: secret}
}

func (f *grantTypesFixture) authorizeRequest() *AuthorizeRequest {
	return &AuthorizeRequest{
		ClientID:            f.clientID,
		RedirectURI:         "https://app.example/cb",
		ResponseType:        ResponseTypeCode,
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: CodeChallengeMethodS256,
		UserID:              1,
	}
}

func requireUnauthorizedClient(t *testing.T, err error) {
	t.Helper()
	var oauthErr *OAuthError
	require.ErrorAs(t, err, &oauthErr)
	assert.Equal(t, ErrOAuthUnauthorizedClient, oauthErr.ErrorCode)
}

func TestOIDCProvider_RefreshGrant_RefusedWithoutRegisteredGrant(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeAuthorizationCode)

	stored := &Token{
		JTI:       "refresh-jti",
		Subject:   "1",
		TenantID:  "tenant-grant-types",
		TokenType: TokenTypeRefresh,
		ClientID:  f.clientID,
		Expires:   time.Now().Add(time.Hour),
	}
	require.NoError(t, f.tokenStorage.StoreToken(context.Background(), stored))

	resp, err := f.provider.Token(context.Background(), &TokenRequest{
		GrantType:    GrantTypeRefreshToken,
		ClientID:     f.clientID,
		ClientSecret: f.secret,
		RefreshToken: "presented-refresh-token",
	})

	requireUnauthorizedClient(t, err)
	assert.Nil(t, resp, "no token may be issued")
	assert.False(t, stored.Revoked, "a refused grant must not rotate the refresh token")
}

func TestOIDCProvider_AuthorizationCodeGrant_RefusedWithoutRegisteredGrant(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeRefreshToken)

	resp, err := f.provider.Token(context.Background(), &TokenRequest{
		GrantType:    GrantTypeAuthorizationCode,
		ClientID:     f.clientID,
		ClientSecret: f.secret,
		Code:         "any-code",
		RedirectURI:  "https://app.example/cb",
	})

	requireUnauthorizedClient(t, err)
	assert.Nil(t, resp)
}

func TestOIDCProvider_Authorize_RefusedWithoutRegisteredGrant(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeClientCredentials)

	resp, err := f.provider.Authorize(context.Background(), f.authorizeRequest())

	requireUnauthorizedClient(t, err)
	assert.Nil(t, resp)
	assert.Empty(t, f.tokenStorage.authCodes, "no authorization code may be minted")
}

func TestOIDCProvider_Authorize_AllowedWithRegisteredGrant(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeAuthorizationCode)

	resp, err := f.provider.Authorize(context.Background(), f.authorizeRequest())

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Code)
}

func TestOIDCProvider_RequireGrantType_UnknownClient(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeRefreshToken)

	err := f.provider.requireGrantType(context.Background(), "no-such-client", GrantTypeRefreshToken)

	var oauthErr *OAuthError
	require.ErrorAs(t, err, &oauthErr)
	assert.Equal(t, ErrOAuthInvalidClient, oauthErr.ErrorCode)
}
