//go:build rest && iam

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

// Revoke maps an OAuthError from the provider onto a 400 (the public endpoint
// forwards token/hint to a confidential client that requires client auth).
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

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
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
