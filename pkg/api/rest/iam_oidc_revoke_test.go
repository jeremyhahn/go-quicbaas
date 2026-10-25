//go:build rest

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	revokeOtherClientID = "app_tenant-a_other"
	revokeOtherSecret   = "other-secret"
)

// registerRevokeClient registers a second confidential client in the fixture's
// tenant, so a test can present a token issued to someone else.
func registerRevokeClient(t *testing.T, f *introspectFixture, clientID, secret string) {
	t.Helper()
	sec, err := secure.NewSecret(t.Context(), []byte(secret))
	require.NoError(t, err)
	hashed, err := iam.HashClientSecret(t.Context(), sec)
	sec.Destroy()
	require.NoError(t, err)
	require.NoError(t, f.appStorage.CreateApp(context.Background(), &iam.Application{
		ClientID:     clientID,
		ClientSecret: hashed,
		TenantID:     introspectTenant,
		GrantTypes:   []string{iam.GrantTypeClientCredentials},
		Scopes:       iam.DefaultFleetRuntimeScopes(),
	}))
}

// revokeRequest builds a form-encoded revocation request against target
// (which may carry a query string).
func revokeRequest(target string, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// tokenActive reports whether the provider still introspects token as active.
func tokenActive(t *testing.T, provider *iam.OIDCProvider, token string) bool {
	t.Helper()
	ctx := tenantctx.WithTenant(context.Background(), introspectTenant)
	resp, err := provider.Introspect(ctx, &iam.IntrospectRequest{Token: token})
	require.NoError(t, err)
	return resp.Active
}

// oauthErrorCode decodes the "error" member of an OAuth error body.
func oauthErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	code, ok := resp["error"].(string)
	require.True(t, ok, "error member missing: %v", resp)
	return code
}

// TestRevoke_ClientAuthentication covers RFC 7009 §2.1: the client authenticates
// the way it does at the token endpoint, and only the client the token was
// issued to may revoke it.
func TestRevoke_ClientAuthentication(t *testing.T) {
	t.Run("owner with Basic revokes the token", func(t *testing.T) {
		f := newIntrospectFixture(t)
		defer f.cleanup()
		token := mintToken(t, f.provider)

		req := revokeRequest("/oauth2/revoke", url.Values{"token": {token}})
		req.SetBasicAuth(introspectClientID, introspectSecret)
		w := httptest.NewRecorder()
		f.handler.Revoke(w, req)

		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		assert.False(t, tokenActive(t, f.provider, token))
	})

	t.Run("owner with body credentials revokes the token", func(t *testing.T) {
		f := newIntrospectFixture(t)
		defer f.cleanup()
		token := mintToken(t, f.provider)

		req := revokeRequest("/oauth2/revoke", url.Values{
			"token":         {token},
			"client_id":     {introspectClientID},
			"client_secret": {introspectSecret},
		})
		w := httptest.NewRecorder()
		f.handler.Revoke(w, req)

		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		assert.False(t, tokenActive(t, f.provider, token))
	})

	t.Run("wrong Basic secret is 401 with challenge", func(t *testing.T) {
		f := newIntrospectFixture(t)
		defer f.cleanup()
		token := mintToken(t, f.provider)

		req := revokeRequest("/oauth2/revoke", url.Values{"token": {token}})
		req.SetBasicAuth(introspectClientID, "wrong-secret")
		w := httptest.NewRecorder()
		f.handler.Revoke(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Header().Get("WWW-Authenticate"), "Basic")
		assert.Equal(t, ErrInvalidClient, oauthErrorCode(t, w))
		assert.True(t, tokenActive(t, f.provider, token))
	})

	t.Run("no credentials is 401 without challenge", func(t *testing.T) {
		f := newIntrospectFixture(t)
		defer f.cleanup()
		token := mintToken(t, f.provider)

		req := revokeRequest("/oauth2/revoke", url.Values{"token": {token}})
		w := httptest.NewRecorder()
		f.handler.Revoke(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Empty(t, w.Header().Get("WWW-Authenticate"))
		assert.Equal(t, ErrInvalidClient, oauthErrorCode(t, w))
		assert.True(t, tokenActive(t, f.provider, token))
	})

	t.Run("client_secret in the query string does not authenticate", func(t *testing.T) {
		f := newIntrospectFixture(t)
		defer f.cleanup()
		token := mintToken(t, f.provider)

		query := url.Values{"client_secret": {introspectSecret}}
		req := revokeRequest("/oauth2/revoke?"+query.Encode(), url.Values{
			"token":     {token},
			"client_id": {introspectClientID},
		})
		w := httptest.NewRecorder()
		f.handler.Revoke(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, ErrInvalidClient, oauthErrorCode(t, w))
		assert.True(t, tokenActive(t, f.provider, token))
	})

	t.Run("another client cannot revoke the token", func(t *testing.T) {
		f := newIntrospectFixture(t)
		defer f.cleanup()
		registerRevokeClient(t, f, revokeOtherClientID, revokeOtherSecret)
		token := mintToken(t, f.provider)

		req := revokeRequest("/oauth2/revoke", url.Values{"token": {token}})
		req.SetBasicAuth(revokeOtherClientID, revokeOtherSecret)
		w := httptest.NewRecorder()
		f.handler.Revoke(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
		assert.Equal(t, iam.ErrOAuthUnauthorizedClient, oauthErrorCode(t, w))
		assert.True(t, tokenActive(t, f.provider, token))
	})

	t.Run("unknown token answers 200", func(t *testing.T) {
		f := newIntrospectFixture(t)
		defer f.cleanup()

		req := revokeRequest("/oauth2/revoke", url.Values{"token": {"not-a-token-we-issued"}})
		req.SetBasicAuth(introspectClientID, introspectSecret)
		w := httptest.NewRecorder()
		f.handler.Revoke(w, req)

		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	})

	t.Run("already revoked token answers 200", func(t *testing.T) {
		f := newIntrospectFixture(t)
		defer f.cleanup()
		token := mintToken(t, f.provider)

		for range 2 {
			req := revokeRequest("/oauth2/revoke", url.Values{"token": {token}})
			req.SetBasicAuth(introspectClientID, introspectSecret)
			w := httptest.NewRecorder()
			f.handler.Revoke(w, req)
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		}
	})
}
