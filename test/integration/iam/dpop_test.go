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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TestDPoPSenderConstrainedToken proves the RFC 9449 sender-constraint end to
// end through the real REST OAuth2 token endpoint and the reusable SDK
// validator: a DPoP proof presented in the request header binds the issued
// platform token to the proof key (cnf.jkt), a downstream validator accepts the
// token only when presented with a matching proof, and a stolen token replayed
// with a different key is rejected. This is the broker's Layer-3 guarantee that
// unit tests exercise at the provider API but only an E2E run proves across the
// transport (the token endpoint must extract the DPoP header into the request).
func TestDPoPSenderConstrainedToken(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// A confidential client for the client-credentials grant.
	appReq := map[string]interface{}{
		"name":          "DPoP Service Account",
		"redirect_uris": []string{"https://app.example.com/callback"},
		"grant_types":   []string{"client_credentials"},
	}
	var app iam.Application
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), appReq, &app),
		"register client-credentials app")
	require.NotEmpty(t, app.ClientID)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, app.ClientID))

	// Resolve the issuer for the validator; fetch the JWKS from this cluster so
	// the key source is guaranteed reachable regardless of the configured
	// issuer URL.
	issuer := discoverIssuer(t, cluster)
	const audience = "api://app"

	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:     issuer,
		Audience:   audience,
		JWKSURL:    cluster.httpEndpoint + "/.well-known/jwks.json",
		HTTPClient: cluster.client,
	})
	require.NoError(t, err, "build SDK platform-token validator")

	// The sender's DPoP key. jkt is the RFC 7638 thumbprint the token binds to.
	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	jkt, err := dpop.Thumbprint(dpopKey.Public())
	require.NoError(t, err)

	tokenURL := cluster.httpEndpoint + "/oauth2/token"

	t.Run("DPoP header binds cnf.jkt and downstream proof validates", func(t *testing.T) {
		// Proof for the token request itself (htm/htu = POST + token endpoint).
		tokenProof, err := dpop.NewProof(dpopKey, http.MethodPost, tokenURL)
		require.NoError(t, err)

		accessToken, tokenType := requestToken(t, cluster, tokenURL, app, audience, tokenProof)

		// RFC 9449 §5: a sender-constrained token is typed "DPoP", not "Bearer".
		assert.Equal(t, "DPoP", tokenType, "sender-constrained token must be token_type=DPoP")

		// A downstream service validates the token with a fresh proof for its own
		// resource call. The proof key matches cnf.jkt, so it is accepted.
		const resourceHTU = "https://api.app.example/v1/records"
		callProof, err := dpop.NewProof(dpopKey, http.MethodGet, resourceHTU)
		require.NoError(t, err)

		cache := dpop.NewMemoryReplayCache(dpop.SystemClock{})
		claims, err := validator.ValidateWithDPoP(
			context.Background(), accessToken, callProof, http.MethodGet, resourceHTU, cache)
		require.NoError(t, err, "downstream DPoP validation should succeed")
		require.NotNil(t, claims.Confirmation, "validated token must carry cnf")
		assert.Equal(t, jkt, claims.Confirmation.Jkt, "cnf.jkt must equal the proof key thumbprint")
		assert.Equal(t, tenantID, claims.TenantID, "tenant_id claim must round-trip")
	})

	t.Run("stolen token replayed with a different key is rejected", func(t *testing.T) {
		tokenProof, err := dpop.NewProof(dpopKey, http.MethodPost, tokenURL)
		require.NoError(t, err)
		accessToken, _ := requestToken(t, cluster, tokenURL, app, audience, tokenProof)

		// Attacker captured the token but holds a different key.
		attackerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		const resourceHTU = "https://api.app.example/v1/records"
		attackerProof, err := dpop.NewProof(attackerKey, http.MethodGet, resourceHTU)
		require.NoError(t, err)

		cache := dpop.NewMemoryReplayCache(dpop.SystemClock{})
		_, err = validator.ValidateWithDPoP(
			context.Background(), accessToken, attackerProof, http.MethodGet, resourceHTU, cache)
		var mismatch *dpop.ThumbprintMismatchError
		assert.True(t, errors.As(err, &mismatch),
			"stolen token + attacker key must fail with ThumbprintMismatchError, got %v", err)
	})

	t.Run("no DPoP header yields a plain bearer with no cnf", func(t *testing.T) {
		accessToken, tokenType := requestToken(t, cluster, tokenURL, app, audience, "")
		assert.Equal(t, "Bearer", tokenType, "a request without a proof must yield a bearer token")

		claims, err := validator.ValidatePlatformToken(context.Background(), accessToken)
		require.NoError(t, err, "bearer token should still validate as a platform token")
		assert.Nil(t, claims.Confirmation, "bearer token must carry no cnf binding")

		// And the DPoP path must refuse a bearer (not sender-constrained).
		const resourceHTU = "https://api.app.example/v1/records"
		proof, err := dpop.NewProof(dpopKey, http.MethodGet, resourceHTU)
		require.NoError(t, err)
		cache := dpop.NewMemoryReplayCache(dpop.SystemClock{})
		_, err = validator.ValidateWithDPoP(
			context.Background(), accessToken, proof, http.MethodGet, resourceHTU, cache)
		var notSC *platformtoken.NotSenderConstrainedError
		assert.True(t, errors.As(err, &notSC),
			"bearer on the DPoP path must fail NotSenderConstrainedError, got %v", err)
	})
}

// requestToken performs a client-credentials token request, optionally with a
// DPoP proof header, and returns the access token and its token_type. The
// resource indicator (RFC 8707) sets the token audience so the validator can
// pin it.
func requestToken(t *testing.T, cluster *testCluster, tokenURL string, app iam.Application, resource, dpopProof string) (string, string) {
	t.Helper()

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", app.ClientID)
	form.Set("client_secret", app.ClientSecret)
	form.Set("scope", "api")
	form.Set("resource", resource)

	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if dpopProof != "" {
		req.Header.Set("DPoP", dpopProof)
	}

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"token endpoint should issue a token, got %d: %s", resp.StatusCode, string(body))

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	require.NoError(t, json.Unmarshal(body, &tokenResp), "parse token response")
	require.NotEmpty(t, tokenResp.AccessToken, "access_token must not be empty")
	return tokenResp.AccessToken, tokenResp.TokenType
}

// discoverIssuer reads the issuer from the OIDC discovery document.
func discoverIssuer(t *testing.T, cluster *testCluster) string {
	t.Helper()
	resp, err := cluster.doHTTPRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var disc map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&disc))
	issuer, ok := disc["issuer"].(string)
	require.True(t, ok && issuer != "", "discovery must advertise an issuer")
	return issuer
}
