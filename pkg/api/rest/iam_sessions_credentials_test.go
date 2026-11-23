//go:build rest

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
	cryptorand "crypto/rand"
	"crypto/rsa"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const (
	logoutSubject = "30001"
	logoutTenant  = "t1"
	logoutUserID  = uint64(30001)
	logoutIssuer  = "https://logout.example.com"
)

// failingRevokeStorage is token storage whose RevokeToken always fails, so a
// logout cannot end the credentials it holds.
type failingRevokeStorage struct {
	*iam.MemoryStorage
}

const failingRevokeText = "token store write refused at shard-7"

func (s *failingRevokeStorage) RevokeToken(ctx context.Context, jti string) error {
	return stderrors.New(failingRevokeText)
}

// newLogoutOIDCProvider builds a real OIDC provider over storage, with the
// logout caller seeded as a user.
func newLogoutOIDCProvider(t *testing.T, storage *iam.MemoryStorage) *iam.OIDCProvider {
	t.Helper()
	return newLogoutOIDCProviderWithTokens(t, storage, storage)
}

// newLogoutOIDCProviderWithTokens builds a real OIDC provider whose users and
// apps live in storage and whose tokens live in tokens.
func newLogoutOIDCProviderWithTokens(t *testing.T, storage *iam.MemoryStorage, tokens iam.TokenStorage) *iam.OIDCProvider {
	t.Helper()
	key, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)
	keys, err := iam.NewSigningKeyManagerFromSigner(key, "RS256")
	require.NoError(t, err)

	require.NoError(t, storage.CreateUser(context.Background(), &iam.User{
		ID:        logoutUserID,
		TenantID:  logoutTenant,
		Username:  logoutSubject,
		Name:      "Logout User",
		Email:     "user30001@example.com",
		Lifecycle: iam.LifecycleActive,
	}))
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            logoutIssuer,
		SigningAlgorithm:  "RS256",
		SigningKeyManager: keys,
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: storage, RoleStorage: storage}),
		AppStorage:    storage,
		TokenStorage:  tokens,
	})
	require.NoError(t, err)
	return provider
}

// issueLogoutCredentials issues the caller an access token and a refresh
// credential, as a ceremony login does, and checks the access token works.
func issueLogoutCredentials(t *testing.T, provider *iam.OIDCProvider) (access, refresh string) {
	t.Helper()
	ctx := context.Background()
	access, _, err := provider.IssueSessionToken(ctx, iam.SessionIdentity{
		Subject:    logoutSubject,
		TenantID:   logoutTenant,
		UserID:     logoutUserID,
		AuthMethod: "fido2",
	})
	require.NoError(t, err)
	refresh, _, err = provider.IssueSessionRefresh(ctx, iam.SessionRefreshOptions{
		Subject:  logoutSubject,
		TenantID: logoutTenant,
		Scopes:   []string{"openid"},
		Session: iam.TokenSessionContext{
			UserID:     logoutUserID,
			Audience:   []string{logoutIssuer},
			AuthMethod: "fido2",
		},
	})
	require.NoError(t, err)
	_, err = provider.ValidateAccessToken(ctx, access)
	require.NoError(t, err, "the access token must validate before logout")
	return access, refresh
}

func logoutAllRequest(target string) *http.Request {
	return httptest.NewRequest(http.MethodDelete, target, nil).
		WithContext(ctxWithTestSubject(context.Background(), logoutSubject, logoutTenant))
}

// Logging out everywhere over REST ends the caller's access token and refresh
// credential, not only its session records.
func TestRESTRevokeAll_EndsBearerAndRefresh(t *testing.T) {
	provider := newLogoutOIDCProvider(t, iam.NewMemoryStorage())
	access, refresh := issueLogoutCredentials(t, provider)

	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr, oidc: provider}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, logoutAllRequest("/api/v1/iam/sessions"))

	require.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, logoutUserID, mgr.revokedUserID)

	ctx := context.Background()
	_, err := provider.ValidateAccessToken(ctx, access)
	require.Error(t, err, "the access token still validates after logout-all")
	_, err = provider.RefreshSession(ctx, iam.SessionRefreshRequest{RefreshToken: refresh})
	require.Error(t, err, "the refresh credential still redeems after logout-all")
}

// Without an OIDC provider logout-all cannot end the credentials, so it
// refuses and leaves the session records alone.
func TestRESTRevokeAll_WithoutProviderIsUnavailable(t *testing.T) {
	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, logoutAllRequest("/api/v1/iam/sessions"))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Zero(t, mgr.revokedUserID, "session records must stay when credentials were not revoked")
}

// A credential that could not be revoked fails the logout, keeps the session
// records and does not leak the storage error.
func TestRESTRevokeAll_RevocationFailureKeepsSessionRecords(t *testing.T) {
	storage := iam.NewMemoryStorage()
	provider := newLogoutOIDCProviderWithTokens(t, storage, &failingRevokeStorage{MemoryStorage: storage})
	issueLogoutCredentials(t, provider)

	mgr := &mockSessionManager{}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr, oidc: provider}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, logoutAllRequest("/api/v1/iam/sessions"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Zero(t, mgr.revokedUserID, "session records must stay when a credential was not revoked")
	assert.NotContains(t, w.Body.String(), failingRevokeText)
}

// Keeping an except session keeps its credentials, as on gRPC.
func TestRESTRevokeAll_ExceptLeavesCredentials(t *testing.T) {
	provider := newLogoutOIDCProvider(t, iam.NewMemoryStorage())
	access, _ := issueLogoutCredentials(t, provider)

	mgr := &mockSessionManager{sessions: []*iam.SessionExt{
		{ID: "keep", UserID: logoutUserID, TenantID: logoutTenant},
	}}
	svc := &mockServiceWithSessions{QuicBaaS: testutil.NewMockQuicBaaS(), mgr: mgr, oidc: provider}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.RevokeAllSessions(w, logoutAllRequest("/api/v1/iam/sessions?except=keep"))

	require.Equal(t, http.StatusNoContent, w.Code)
	_, err := provider.ValidateAccessToken(context.Background(), access)
	assert.NoError(t, err, "the except branch must leave the caller's credentials")
}
