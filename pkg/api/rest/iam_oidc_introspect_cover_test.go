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
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A missing token form field yields 400 invalid_request before any provider
// work happens.
func TestIntrospect_MissingToken(t *testing.T) {
	handler, _, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "invalid_request", resp["error"])
}

// A caller authenticated via registered client credentials (no mTLS cert) is
// accepted and the active token is returned.
func TestIntrospect_ClientCredentialsCaller(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", introspectSecret)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["active"])
}

// A caller presenting a valid client_id with a wrong secret is rejected (401).
func TestIntrospect_ClientCredentialsBadSecret(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", introspectClientID)
	form.Set("client_secret", "wrong-secret")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "invalid_client", resp["error"])
}

// TestIntrospect_BasicBadSecretChallenge pins draft-ietf-oauth-v2-1-16
// §3.2.4 on the introspection endpoint: a caller that failed HTTP Basic
// client authentication gets 401 with a WWW-Authenticate Basic challenge.
func TestIntrospect_BasicBadSecretChallenge(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	form := url.Values{}
	form.Set("token", mintToken(t, provider))
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(introspectClientID, "wrong-secret")
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, basicAuthChallenge, w.Header().Get("WWW-Authenticate"))
}

// A caller using HTTP Basic credentials is accepted (the hasBasic branch of
// authenticateIntrospectionCaller).
func TestIntrospect_BasicAuthCaller(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	form := url.Values{}
	form.Set("token", token)
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(introspectClientID, introspectSecret)
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["active"])
}

// An unknown token_type_hint is tolerated; the provider still returns a result.
func TestIntrospect_WithTokenTypeHint(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	form := url.Values{}
	form.Set("token", token)
	form.Set("token_type_hint", "access_token")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(introspectClientID, introspectSecret)
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["active"])
}

// A caller presenting BOTH a verified peer cert AND a client_id binds the two;
// an unregistered client_id with a cert fails the mTLS bind -> 401.
func TestIntrospect_CertWithUnknownClientID(t *testing.T) {
	handler, provider, _, cleanup := setupIntrospectHandler(t)
	defer cleanup()

	token := mintToken(t, provider)
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", "app_tenant-a_unregistered")
	req := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cert := &x509.Certificate{
		Subject:      pkix.Name{CommonName: "rs.example.com"},
		SerialNumber: big.NewInt(9),
	}
	req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}}
	w := httptest.NewRecorder()
	handler.Introspect(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}
