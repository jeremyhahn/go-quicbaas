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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

type authorizeService struct {
	*testutil.MockQuicBaaS
	provider *iam.OIDCProvider
}

func (m *authorizeService) GetOIDCProvider(_ context.Context) (*iam.OIDCProvider, error) {
	return m.provider, nil
}

func setupAuthorizeHandler(t *testing.T) *IAMHandler {
	t.Helper()
	const tenant = "tenant-auth"
	const clientID = "app_tenant-auth_web"

	backend := iam.NewBackendStorage(newIntrospectMemKV())
	require.NoError(t, backend.CreateApp(context.Background(), &iam.Application{
		ClientID:      clientID,
		TenantID:      tenant,
		RedirectURIs:  []string{"https://app.example/callback"},
		GrantTypes:    []string{iam.GrantTypeAuthorizationCode},
		ResponseTypes: []string{"code"},
	}))

	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            "https://broker.example.com",
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testOIDCKeyManager(t, privateKey),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        15 * time.Minute,
		AuthCodeTTL:       10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: iam.NewIdentityStore(iam.IdentityStoreOptions{
			UserStorage: iam.NewMemoryStorage(),
			RoleStorage: iam.NewMemoryStorage(),
		}),
		AppStorage:   backend,
		TokenStorage: backend,
	})
	require.NoError(t, err)

	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	mockSvc := &authorizeService{MockQuicBaaS: testutil.NewMockQuicBaaS(), provider: provider}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	return NewIAMHandler(IAMHandlerDependencies{Server: server})
}

// Authorize with a registered code-grant client and matching redirect URI
// issues a 302 redirect carrying the authorization code.
func TestAuthorize_Success(t *testing.T) {
	handler := setupAuthorizeHandler(t)

	req := httptest.NewRequest(http.MethodGet,
		"/oauth2/authorize?client_id=app_tenant-auth_web&redirect_uri=https://app.example/callback&response_type=code&state=xyz&scope=openid&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256", nil)
	w := httptest.NewRecorder()
	handler.Authorize(w, req)

	require.Equal(t, http.StatusFound, w.Code, "body: %s", w.Body.String())
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "code=")
	assert.Contains(t, loc, "state=xyz")
}

// Authorize with an unregistered redirect URI is rejected with a 400 OAuth
// error (provider-level invalid_request).
func TestAuthorize_BadRedirect(t *testing.T) {
	handler := setupAuthorizeHandler(t)

	req := httptest.NewRequest(http.MethodGet,
		"/oauth2/authorize?client_id=app_tenant-auth_web&redirect_uri=https://evil.example/cb&response_type=code", nil)
	w := httptest.NewRecorder()
	handler.Authorize(w, req)

	assert.NotEqual(t, http.StatusFound, w.Code)
}
