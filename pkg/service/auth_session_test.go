// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// Session token lifecycle (logout / refresh / validate).
//
// These verbs are backed by the OIDC provider — the authority that minted the
// session bearer. They previously routed through an interfaces.AuthManager
// that is nil in every running daemon, so /api/v1/auth/refresh answered
// "authentication service not configured" and no client could ever renew a
// session. The suite below runs against a REAL provider so a regression back
// to a non-functional path fails here rather than in a browser 15 minutes
// after login.
// =============================================================================

// newSessionAuthService builds an authService whose IAM provider exposes a real
// OIDC provider, plus the provider itself so tests can mint tokens directly.
func newSessionAuthService(t *testing.T) (*authService, *iam.OIDCProvider) {
	t.Helper()
	svc, provider, _ := newSessionAuthServiceWithUsers(t)
	return svc, provider
}

// newSessionAuthServiceWithUsers is newSessionAuthService with the user store
// exposed. Session issuance joins the IAM user record for the principal's
// roles and refuses a subject it cannot resolve (go-quicbaas #274), so a test
// that mints a bearer must seed one.
func newSessionAuthServiceWithUsers(t *testing.T) (*authService, *iam.OIDCProvider, *iam.MemoryStorage) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	storage := iam.NewMemoryStorage()
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})

	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:           "https://session.example.com",
		SigningAlgorithm: "RS256",
		SigningKey:       key,
		AccessTokenTTL:   15 * time.Minute,
		RefreshTokenTTL:  24 * time.Hour,
		IDTokenTTL:       time.Hour,
		AuthCodeTTL:      10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    storage,
		TokenStorage:  storage,
	})
	require.NoError(t, err)

	svc := &quicBaaS{
		logger:      createTestLogger(t),
		iamProvider: &iamServiceAdapter{components: &mockIAMComponents{oidcProvider: provider}},
	}
	return &authService{svc: svc}, provider, storage
}

// issueSession mints an access token plus its refresh credential for a seeded
// principal. It returns the store so callers can build one; seedSessionUser
// must have run for the subject first.
func issueSession(t *testing.T, provider *iam.OIDCProvider) (accessToken, refreshToken string) {
	t.Helper()
	ctx := context.Background()

	accessToken, _, err := provider.IssueSessionToken(ctx, iam.SessionIdentity{
		Subject:     sessionSubject,
		TenantID:    sessionTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	})
	require.NoError(t, err)

	refreshToken, _, err = provider.IssueRefreshForSessionBearer(ctx, accessToken)
	require.NoError(t, err)

	return accessToken, refreshToken
}

const (
	sessionTenant  = "acme"
	sessionSubject = "user-1"
)

// seedSessionUser writes the IAM user record the session subject resolves to.
// The ceremony's subject is the tenant-scoped qrdb username, which the broker
// mirrors as User.Name.
func seedSessionUser(t *testing.T, storage *iam.MemoryStorage) {
	t.Helper()
	require.NoError(t, storage.CreateUser(context.Background(), &iam.User{
		ID:        "2",
		TenantID:  sessionTenant,
		Username:  sessionSubject,
		Name:      sessionSubject,
		Email:     "user-1@example.test",
		Roles:     []string{"admin"},
		Lifecycle: iam.LifecycleActive,
	}))
}

// =============================================================================
// RefreshToken
// =============================================================================

func TestAuthService_RefreshTokenRenewsSession(t *testing.T) {
	svc, provider, users := newSessionAuthServiceWithUsers(t)
	seedSessionUser(t, users)
	_, refreshToken := issueSession(t, provider)

	resp, err := svc.RefreshToken(context.Background(), &RefreshTokenRequest{
		RefreshToken: refreshToken,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.NotEmpty(t, resp.AccessToken, "renewal must return a new access token")
	assert.NotEmpty(t, resp.RefreshToken, "renewal must rotate the refresh credential")
	assert.NotEqual(t, refreshToken, resp.RefreshToken)
	assert.Equal(t, "Bearer", resp.TokenType)

	// Timestamps are Unix SECONDS. The previous implementation returned
	// nanoseconds, which made the REST layer's expires_in arithmetic nonsense.
	assert.InDelta(t, time.Now().Add(15*time.Minute).Unix(), resp.ExpiresAt, 5)
	assert.Greater(t, resp.RefreshExpiresAt, resp.ExpiresAt)

	// The renewed bearer must actually authenticate.
	claims, err := provider.ValidateAccessToken(context.Background(), resp.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.Subject)
	assert.Equal(t, "acme", claims.TenantID)
}

func TestAuthService_RefreshTokenRejectsBadInput(t *testing.T) {
	svc, provider, users := newSessionAuthServiceWithUsers(t)
	seedSessionUser(t, users)

	t.Run("nil request", func(t *testing.T) {
		_, err := svc.RefreshToken(context.Background(), nil)
		require.Error(t, err)
		qbErr, ok := err.(*errors.QBError)
		require.True(t, ok, "expected *errors.QBError, got %T", err)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("empty credential", func(t *testing.T) {
		_, err := svc.RefreshToken(context.Background(), &RefreshTokenRequest{})
		require.Error(t, err)
		qbErr, ok := err.(*errors.QBError)
		require.True(t, ok)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("unknown credential", func(t *testing.T) {
		_, err := svc.RefreshToken(context.Background(), &RefreshTokenRequest{
			RefreshToken: "never-issued",
		})
		require.Error(t, err)
		qbErr, ok := err.(*errors.QBError)
		require.True(t, ok)
		assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
	})

	t.Run("replayed credential", func(t *testing.T) {
		_, refreshToken := issueSession(t, provider)
		_, err := svc.RefreshToken(context.Background(), &RefreshTokenRequest{RefreshToken: refreshToken})
		require.NoError(t, err)

		_, err = svc.RefreshToken(context.Background(), &RefreshTokenRequest{RefreshToken: refreshToken})
		require.Error(t, err, "a rotated credential must not be redeemable twice")
	})
}

func TestAuthService_RefreshTokenWithoutProvider(t *testing.T) {
	svc := &authService{svc: &quicBaaS{logger: createTestLogger(t)}}

	_, err := svc.RefreshToken(context.Background(), &RefreshTokenRequest{RefreshToken: "x"})
	require.Error(t, err)
	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrConfiguration, qbErr.Code)
}

// =============================================================================
// ValidateToken
// =============================================================================

func TestAuthService_ValidateTokenAcceptsIssuedBearer(t *testing.T) {
	svc, provider, users := newSessionAuthServiceWithUsers(t)
	seedSessionUser(t, users)
	accessToken, _ := issueSession(t, provider)

	resp, err := svc.ValidateToken(context.Background(), &ValidateTokenRequest{
		AccessToken: accessToken,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Valid)
	assert.Equal(t, "user-1", resp.UserID)
	assert.Equal(t, []string{"admin"}, resp.Roles)
	// Unix seconds, so the REST layer's time.Unix(expiresAt, 0) is correct.
	assert.InDelta(t, time.Now().Add(15*time.Minute).Unix(), resp.ExpiresAt, 5)
}

func TestAuthService_ValidateTokenRejectsGarbage(t *testing.T) {
	svc, _ := newSessionAuthService(t)

	_, err := svc.ValidateToken(context.Background(), &ValidateTokenRequest{
		AccessToken: "not.a.token",
	})
	require.Error(t, err)
	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
}

// =============================================================================
// Logout
// =============================================================================

func TestAuthService_LogoutRevokesSession(t *testing.T) {
	svc, provider, users := newSessionAuthServiceWithUsers(t)
	seedSessionUser(t, users)
	accessToken, _ := issueSession(t, provider)

	resp, err := svc.Logout(context.Background(), &LogoutRequest{AccessToken: accessToken})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)

	// The revoked bearer must no longer validate.
	_, err = svc.ValidateToken(context.Background(), &ValidateTokenRequest{AccessToken: accessToken})
	require.Error(t, err, "a revoked bearer must not validate")
}

func TestAuthService_LogoutRejectsUnknownToken(t *testing.T) {
	svc, _ := newSessionAuthService(t)

	resp, err := svc.Logout(context.Background(), &LogoutRequest{AccessToken: "never-issued"})
	require.Error(t, err)
	require.NotNil(t, resp, "a response is returned even on failure so callers can report it")
	assert.False(t, resp.Success)
}

func TestAuthService_LogoutWithoutProvider(t *testing.T) {
	svc := &authService{svc: &quicBaaS{logger: createTestLogger(t)}}

	_, err := svc.Logout(context.Background(), &LogoutRequest{AccessToken: "x"})
	require.Error(t, err)
	qbErr, ok := err.(*errors.QBError)
	require.True(t, ok)
	assert.Equal(t, errors.ErrConfiguration, qbErr.Code)
}
