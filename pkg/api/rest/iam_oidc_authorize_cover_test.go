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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// setupAuthorizeHandler registers one client for grantTypes, or for
// authorization_code alone when none are given.
func setupAuthorizeHandler(t *testing.T, grantTypes ...string) *IAMHandler {
	t.Helper()
	const tenant = "tenant-auth"
	const clientID = "app_tenant-auth_web"
	if len(grantTypes) == 0 {
		grantTypes = []string{iam.GrantTypeAuthorizationCode}
	}

	backend := iam.NewBackendStorage(newIntrospectMemKV())
	require.NoError(t, backend.CreateApp(context.Background(), &iam.Application{
		ClientID:      clientID,
		TenantID:      tenant,
		RedirectURIs:  []string{"https://app.example/callback", "https://app.example/cb?app=web"},
		GrantTypes:    grantTypes,
		ResponseTypes: []string{"code"},
	}))
	require.NoError(t, backend.CreateApp(context.Background(), &iam.Application{
		ClientID:      authorizeNativeClientID,
		TenantID:      tenant,
		RedirectURIs:  []string{"http://127.0.0.1/callback"},
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

// authorizeMissingResponseTypeTarget is an authorization request from the
// client registered by setupAuthorizeHandler, with a valid redirect URI and
// state but no response_type.
const authorizeMissingResponseTypeTarget = "/oauth2/authorize?client_id=app_tenant-auth_web" +
	"&redirect_uri=https://app.example/callback&state=st-1"

// assertAuthorizeErrorRedirect checks a draft-ietf-oauth-v2-1-16 §4.1.2.1
// error response: a 302 to the registered redirect URI carrying errCode and,
// when the request had one, its state.
func assertAuthorizeErrorRedirect(t *testing.T, w *httptest.ResponseRecorder, errCode, state string) {
	t.Helper()
	require.Equal(t, http.StatusFound, w.Code, "body: %s", w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/callback", loc.Scheme+"://"+loc.Host+loc.Path)
	assert.Equal(t, errCode, loc.Query().Get("error"))
	assert.Equal(t, state, loc.Query().Get("state"))
	assert.Empty(t, loc.Query().Get("code"))
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
	assert.Contains(t, loc, "iss="+url.QueryEscape("https://broker.example.com"))
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

// A client not registered for authorization_code cannot start the flow. Its
// redirect URI is validated, so unauthorized_client goes back to it as an
// error redirect with the state echoed and no code (draft-ietf-oauth-v2-1-16
// §4.1.2.1).
func TestAuthorize_GrantNotRegistered_UnauthorizedClient(t *testing.T) {
	handler := setupAuthorizeHandler(t, iam.GrantTypeRefreshToken)

	req := httptest.NewRequest(http.MethodGet,
		"/oauth2/authorize?client_id=app_tenant-auth_web&redirect_uri=https://app.example/callback"+
			"&response_type=code&state=xyz&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", nil)
	w := httptest.NewRecorder()
	handler.Authorize(w, req)

	require.Equal(t, http.StatusFound, w.Code, "body: %s", w.Body.String())
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	got := loc.Query()
	assert.Equal(t, "unauthorized_client", got.Get("error"))
	assert.Equal(t, "xyz", got.Get("state"))
	assert.Empty(t, got.Get("code"))
}

// TestAuthorize_UnvalidatedClientIsNotRedirected pins draft-ietf-oauth-v2-1-16
// §4.1.2.1: an unknown client_id or a missing or unregistered redirect_uri is
// reported to the user agent directly and never redirected, even when the
// rest of the request is also invalid.
func TestAuthorize_UnvalidatedClientIsNotRedirected(t *testing.T) {
	handler := setupAuthorizeHandler(t)

	tests := []struct {
		name    string
		query   string
		errCode string
	}{
		{
			name:    "unknown client_id",
			query:   "client_id=app_tenant-auth_nope&redirect_uri=https://app.example/callback&state=s1",
			errCode: ErrInvalidClient,
		},
		{
			name:    "missing redirect_uri",
			query:   "client_id=app_tenant-auth_web&response_type=code&state=s1",
			errCode: ErrInvalidRequest,
		},
		{
			name:    "unregistered redirect_uri",
			query:   "client_id=app_tenant-auth_web&redirect_uri=https://evil.example/cb&state=s1",
			errCode: ErrInvalidRequest,
		},
		{
			name:    "redirect_uri differing only in query",
			query:   "client_id=app_tenant-auth_web&redirect_uri=" + url.QueryEscape("https://app.example/cb?app=evil") + "&state=s1",
			errCode: ErrInvalidRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.Authorize(w, httptest.NewRequest(http.MethodGet, "/oauth2/authorize?"+tt.query, nil))

			require.Equal(t, http.StatusBadRequest, w.Code)
			assert.Empty(t, w.Header().Get("Location"))
			var resp map[string]interface{}
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.Equal(t, tt.errCode, resp["error"])
		})
	}
}

// TestAuthorize_ErrorAfterValidationRedirects pins draft-ietf-oauth-v2-1-16
// §4.1.2.1: once the client and its redirect URI are validated, an error goes
// back to the client as a 302 carrying error, error_description, state and
// iss, keeping any query the registered redirect URI already has.
func TestAuthorize_ErrorAfterValidationRedirects(t *testing.T) {
	handler := setupAuthorizeHandler(t)

	tests := []struct {
		name        string
		query       string
		redirectURI string
		errCode     string
		wantState   string
		wantQuery   url.Values
	}{
		{
			name:        "missing PKCE",
			query:       "response_type=code&state=xyz",
			redirectURI: "https://app.example/callback",
			errCode:     "invalid_request",
			wantState:   "xyz",
		},
		{
			name:        "missing response_type",
			query:       "state=xyz",
			redirectURI: "https://app.example/callback",
			errCode:     "invalid_request",
			wantState:   "xyz",
		},
		{
			name:        "unsupported response_type without state",
			query:       "response_type=token",
			redirectURI: "https://app.example/callback",
			errCode:     "unsupported_response_type",
		},
		{
			name:        "registered query is kept",
			query:       "response_type=token&state=abc",
			redirectURI: "https://app.example/cb?app=web",
			errCode:     "unsupported_response_type",
			wantState:   "abc",
			wantQuery:   url.Values{"app": {"web"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := "/oauth2/authorize?client_id=app_tenant-auth_web&redirect_uri=" +
				url.QueryEscape(tt.redirectURI) + "&" + tt.query
			w := httptest.NewRecorder()
			handler.Authorize(w, httptest.NewRequest(http.MethodGet, target, nil))

			require.Equal(t, http.StatusFound, w.Code, "body: %s", w.Body.String())
			loc, err := url.Parse(w.Header().Get("Location"))
			require.NoError(t, err)
			assert.Equal(t, "app.example", loc.Host)
			got := loc.Query()
			assert.Equal(t, tt.errCode, got.Get("error"))
			assert.NotEmpty(t, got.Get("error_description"))
			assert.Equal(t, "https://broker.example.com", got.Get("iss"))
			assert.Empty(t, got.Get("code"))
			if tt.wantState == "" {
				assert.NotContains(t, got, "state")
			} else {
				assert.Equal(t, tt.wantState, got.Get("state"))
			}
			for name := range tt.wantQuery {
				assert.Equal(t, tt.wantQuery.Get(name), got.Get(name))
			}
		})
	}
}

// authorizeNativeClientID registers exactly one redirect URI, on the IPv4
// loopback literal.
const authorizeNativeClientID = "app_tenant-auth_cli"

// authorizePKCEQuery is a valid PKCE code request without client_id or
// redirect_uri.
const authorizePKCEQuery = "response_type=code&state=xyz" +
	"&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&code_challenge_method=S256"

// TestAuthorize_RedirectURIResolution covers draft-ietf-oauth-v2-1-16 §4.1.1
// (#1439) and the loopback port rule of §8.4.2 (#1438) through the handler.
func TestAuthorize_RedirectURIResolution(t *testing.T) {
	handler := setupAuthorizeHandler(t)

	t.Run("omitted with one registered redirects there", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.Authorize(w, httptest.NewRequest(http.MethodGet,
			"/oauth2/authorize?client_id="+authorizeNativeClientID+"&"+authorizePKCEQuery, nil))

		require.Equal(t, http.StatusFound, w.Code, "body: %s", w.Body.String())
		loc, err := url.Parse(w.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "http://127.0.0.1/callback", loc.Scheme+"://"+loc.Host+loc.Path)
		assert.NotEmpty(t, loc.Query().Get("code"))
	})

	t.Run("omitted with several registered is not redirected", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.Authorize(w, httptest.NewRequest(http.MethodGet,
			"/oauth2/authorize?client_id=app_tenant-auth_web&"+authorizePKCEQuery, nil))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Empty(t, w.Header().Get("Location"))
		assert.Contains(t, w.Body.String(), "redirect_uri is required")
	})

	t.Run("omitted with unknown client is not redirected", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.Authorize(w, httptest.NewRequest(http.MethodGet,
			"/oauth2/authorize?client_id=unknown-client&"+authorizePKCEQuery, nil))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Empty(t, w.Header().Get("Location"))
		assert.Contains(t, w.Body.String(), "invalid_client")
	})

	t.Run("loopback redirect keeps the requested port", func(t *testing.T) {
		w := httptest.NewRecorder()
		handler.Authorize(w, httptest.NewRequest(http.MethodGet,
			"/oauth2/authorize?client_id="+authorizeNativeClientID+
				"&redirect_uri="+url.QueryEscape("http://127.0.0.1:49152/callback")+"&"+authorizePKCEQuery, nil))

		require.Equal(t, http.StatusFound, w.Code, "body: %s", w.Body.String())
		loc, err := url.Parse(w.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:49152", loc.Host)
		assert.NotEmpty(t, loc.Query().Get("code"))
	})
}

// TestAuthorize_IssMatchesAdvertisedIssuer covers RFC 9207 (#1444): both
// metadata documents advertise authorization_response_iss_parameter_supported,
// and success and error redirects carry iss equal to the advertised issuer.
func TestAuthorize_IssMatchesAdvertisedIssuer(t *testing.T) {
	handler := setupAuthorizeHandler(t)

	var issuer string
	documents := map[string]http.HandlerFunc{
		"openid-configuration":       handler.GetOIDCConfig,
		"oauth-authorization-server": handler.GetOAuthASMetadata,
	}
	for name, serve := range documents {
		w := httptest.NewRecorder()
		serve(w, httptest.NewRequest(http.MethodGet, "/.well-known/"+name, nil))
		require.Equal(t, http.StatusOK, w.Code, "%s body: %s", name, w.Body.String())
		var doc map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc), name)
		assert.Equal(t, true, doc["authorization_response_iss_parameter_supported"], name)
		docIssuer, ok := doc["issuer"].(string)
		require.True(t, ok && docIssuer != "", "%s issuer: %v", name, doc["issuer"])
		if issuer != "" {
			assert.Equal(t, issuer, docIssuer, "documents advertise one issuer")
		}
		issuer = docIssuer
	}

	redirects := map[string]string{
		"success": "/oauth2/authorize?client_id=" + authorizeNativeClientID + "&" + authorizePKCEQuery,
		"error":   "/oauth2/authorize?client_id=" + authorizeNativeClientID + "&response_type=token&state=xyz",
	}
	for name, target := range redirects {
		w := httptest.NewRecorder()
		handler.Authorize(w, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusFound, w.Code, "%s body: %s", name, w.Body.String())
		loc, err := url.Parse(w.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, issuer, loc.Query().Get("iss"), name)
	}
}
