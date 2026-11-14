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

//go:build integration
// +build integration

package iam_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// A downstream daemon's REST plane (go-trusted-platform#644, owner ruling 133)
// mounts the sdk/go route guard over tokens a live node issues, with the
// node's published JWKS as its only key source: the token's tenant reaches
// the handler whatever tenant the request names, a permission the token was
// not granted is 403, and a DPoP-bound token needs its holder's proof.
func TestDaemonRESTGuard_LiveNodeTokens(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	var app iam.Application
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), map[string]interface{}{
		"name":          "Daemon REST plane client",
		"redirect_uris": []string{"https://app.example.com/callback"},
		"grant_types":   []string{"client_credentials"},
	}, &app), "register client-credentials app")
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, app.ClientID))

	const audience = "api://daemon"
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:       discoverIssuer(t, cluster),
		Audience:     audience,
		JWKSURL:      cluster.httpEndpoint + "/.well-known/jwks.json",
		HTTPClient:   cluster.client,
		ReplayCache:  dpop.NewMemoryReplayCache(dpop.SystemClock{}),
		ModeResolver: func(string) platformtoken.AuthzMode { return platformtoken.AuthzModeOffline },
	})
	require.NoError(t, err)

	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ac, ok := platformtoken.FromContext(r.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(ac.TenantID))
	})
	const resource = "https://daemon.example/v1/keys"
	call := func(permission, auth, proof string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, resource+"?tenant_id=system",
			strings.NewReader(`{"tenant_id":"system","user_id":"1"}`))
		req.Header.Set("Authorization", auth)
		req.Header.Set("X-Tenant-ID", "system")
		if proof != "" {
			req.Header.Set("DPoP", proof)
		}
		w := httptest.NewRecorder()
		validator.Middleware(permission)(echo).ServeHTTP(w, req)
		return w
	}
	tokenURL := cluster.httpEndpoint + "/oauth2/token"

	t.Run("bearer token binds the token's tenant", func(t *testing.T) {
		token, _ := requestToken(t, cluster, tokenURL, app, audience, "")
		w := call("", "Bearer "+token, "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, tenantID, w.Body.String())
	})

	t.Run("a permission the token was not granted is refused", func(t *testing.T) {
		token, _ := requestToken(t, cluster, tokenURL, app, audience, "")
		w := call("keys:delete", "Bearer "+token, "")
		assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	})

	t.Run("DPoP-bound token needs its holder's proof", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		tokenProof, err := dpop.NewProof(key, http.MethodPost, tokenURL)
		require.NoError(t, err)
		token, tokenType := requestToken(t, cluster, tokenURL, app, audience, tokenProof)
		require.Equal(t, "DPoP", tokenType)

		assert.Equal(t, http.StatusUnauthorized, call("", "DPoP "+token, "").Code, "no proof")

		attacker, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		stolen, err := dpop.NewProof(attacker, http.MethodGet, resource)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, call("", "DPoP "+token, stolen).Code, "another key's proof")

		proof, err := dpop.NewProof(key, http.MethodGet, resource)
		require.NoError(t, err)
		w := call("", "DPoP "+token, proof)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, tenantID, w.Body.String())
	})
}
