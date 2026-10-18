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

// clientCredentialsQuery carries the client credentials in the URL query, which
// draft-ietf-oauth-v2-1-16 §3.2 forbids for token-endpoint parameters.
func clientCredentialsQuery() string {
	query := url.Values{}
	query.Set("client_id", introspectClientID)
	query.Set("client_secret", introspectSecret)
	return query.Encode()
}

// A client_secret supplied only in the URL query is ignored by the token
// endpoint, so client authentication fails (#1437).
func TestToken_ClientSecretInQueryIgnored(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/oauth2/token?"+clientCredentialsQuery(),
		strings.NewReader(tokenForm().Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "invalid_client", resp["error"])
}

// The same client credentials in the form body authenticate the client (#1437).
func TestToken_ClientSecretInBodyAccepted(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := tokenForm()
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", introspectSecret)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.NotEmpty(t, resp["access_token"])
}

// A grant_type supplied only in the URL query is ignored, so the request is
// missing its grant_type (#1437).
func TestToken_GrantTypeInQueryIgnored(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", introspectSecret)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/token?grant_type=client_credentials",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Token(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "invalid_request", resp["error"])
}

// The introspection endpoint also ignores client credentials in the URL query
// (#1437).
func TestIntrospect_ClientSecretInQueryIgnored(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	form.Set("token", "any-token")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect?"+clientCredentialsQuery(),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "body: %s", w.Body.String())
}

// The introspection endpoint authenticates client credentials in the form body
// (#1437).
func TestIntrospect_ClientSecretInBodyAccepted(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	form.Set("token", "any-token")
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", introspectSecret)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, false, resp["active"])
}
