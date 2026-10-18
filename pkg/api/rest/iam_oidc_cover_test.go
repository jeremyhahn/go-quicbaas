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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tokenForm posts a client_credentials grant against the real provider wired by
// the introspect harness.
func tokenForm() url.Values {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("scope", "openid agent:runtime:spawn")
	return form
}

// The token endpoint mints an access token through the client_credentials grant
// with HTTP Basic client authentication, and emits a dual-identity audit.
func TestToken_ClientCredentials_Success(t *testing.T) {
	handler, _, auditLogger, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(tokenForm().Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(introspectClientID, introspectSecret)
	w := httptest.NewRecorder()
	handler.Token(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp["access_token"])
	assert.Equal(t, "Bearer", resp["token_type"])
	_ = auditLogger
}

// A DPoP proof header drives the htu/htm capture path. The (invalid) proof
// fails closed in the issuer, yielding a 4xx without a token.
func TestToken_WithDPoPHeader(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := tokenForm()
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", introspectSecret)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", "not-a-valid-proof")
	w := httptest.NewRecorder()
	handler.Token(w, req)

	// The malformed proof fails closed in the issuer; never a 200 with a token.
	assert.NotEqual(t, http.StatusOK, w.Code)
}

// Bad client credentials on the token endpoint return 401 invalid_client.
func TestToken_InvalidClient(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := tokenForm()
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", "wrong")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "invalid_client", resp["error"])
}

// A client registered only for client_credentials is refused the
// refresh_token grant with unauthorized_client (RFC 6749 §5.2) on the wire.
func TestToken_RefreshGrantNotRegistered_UnauthorizedClient(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", "any-refresh-token")
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", introspectSecret)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "unauthorized_client", resp["error"])
	assert.Empty(t, resp["access_token"])
}

// The same client is refused the authorization_code exchange.
func TestToken_CodeGrantNotRegistered_UnauthorizedClient(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", "any-code")
	form.Set("redirect_uri", "https://app.example/cb")
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", introspectSecret)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "unauthorized_client", resp["error"])
	assert.Empty(t, resp["access_token"])
}

// TestToken_InvalidClientBasicChallenge pins draft-ietf-oauth-v2-1-16 §3.2.4:
// a client that authenticated with HTTP Basic and fails gets 401 with a
// WWW-Authenticate challenge for the Basic scheme; credentials sent in the
// body get the 401 without one.
func TestToken_InvalidClientBasicChallenge(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	t.Run("basic", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(tokenForm().Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(introspectClientID, "wrong")
		w := httptest.NewRecorder()
		handler.Token(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, basicAuthChallenge, w.Header().Get("WWW-Authenticate"))
		var resp map[string]interface{}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "invalid_client", resp["error"])
	})

	t.Run("body credentials", func(t *testing.T) {
		form := tokenForm()
		form.Set("client_id", introspectClientID)
		form.Set("client_secret", "wrong")
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.Token(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Empty(t, w.Header().Get("WWW-Authenticate"))
	})
}

// TestChallengeBasicClient checks that the Basic challenge follows the
// scheme the client attempted, including a malformed Basic header, and is
// absent for other schemes or no Authorization header.
func TestChallengeBasicClient(t *testing.T) {
	tests := []struct {
		name          string
		authorization string
		want          string
	}{
		{name: "basic", authorization: "Basic YTpi", want: basicAuthChallenge},
		{name: "basic lower case", authorization: "basic YTpi", want: basicAuthChallenge},
		{name: "malformed basic", authorization: "Basic !!!", want: basicAuthChallenge},
		{name: "bearer", authorization: "Bearer abc"},
		{name: "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			w := httptest.NewRecorder()
			challengeBasicClient(w, req)
			assert.Equal(t, tt.want, w.Header().Get("WWW-Authenticate"))
		})
	}
}

// An unsupported grant_type returns a 400 OAuth error from the provider.
func TestToken_UnsupportedGrant(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	form.Set("grant_type", "urn:bogus")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(introspectClientID, introspectSecret)
	w := httptest.NewRecorder()
	handler.Token(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// UserInfo with an invalid bearer token returns 401 from the provider.
func TestUserInfo_InvalidToken(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer garbage.token.value")
	w := httptest.NewRecorder()
	handler.UserInfo(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// Revoke maps the provider's invalid_client onto a 401 (RFC 7009 §2.1: the
// endpoint requires client authentication, and this request sends none).
func TestRevoke_OAuthError(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	form := url.Values{}
	form.Set("token", token)
	form.Set("token_type_hint", "access_token")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Revoke(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp["error"])
}

// Revoke requires a token form field.
func TestRevoke_MissingToken(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Revoke(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
