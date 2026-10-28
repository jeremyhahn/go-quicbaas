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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Session renewal, end to end against a live server.
//
// Access tokens are deliberately short-lived (15m, hard-capped at 60m). That
// posture is only usable if a session can be renewed, and for a long time it
// could not be: /api/v1/auth/refresh was wired to an auth manager that is nil
// in every running daemon, and refresh tokens were indexed by JTI rather than
// by the value the client presents, so the credential could never be resolved.
// The SPA died 15 minutes after login and `quicbaas mcp serve --stdio` started
// answering "authentication required".
//
// These tests exercise the whole chain through real HTTP: a hardware (PIV
// mTLS) login, the renewal endpoint, an authenticated API call with the
// RENEWED bearer, and rejection of a replayed credential.
// =============================================================================

// sessionTokens is the token bundle a renewal returns.
type sessionTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	ExpiresAt    string `json:"expires_at"`
}

// pivLoginSession performs an uncached PIV mTLS login and returns its token
// bundle. The bundle — not just the bearer — is what makes this test
// meaningful: a login that hands back no refresh credential cannot produce a
// session that outlives its 15-minute access token.
func pivLoginSession(t *testing.T, cluster *testCluster) *helpers.PIVSession {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	session, err := helpers.AuthPIVFullSession(ctx, cluster.httpEndpoint, "", "", "")
	require.NoError(t, err, "PIV login")
	require.NotEmpty(t, session.AccessToken, "login returned no access token")
	return session
}

// postRefresh redeems a refresh credential against the public renewal endpoint.
// It posts to /api/v1/iam/refresh, the route served by iam.OIDCProvider, which
// is the provider that minted the credential at /api/v1/iam/login/piv.
// /api/v1/auth/refresh belongs to xkms and redeems against its own refresh
// store, which has never seen a credential issued by the IAM login.
func postRefresh(t *testing.T, cluster *testCluster, refreshToken string) (int, sessionTokens, string) {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"refresh_token": refreshToken})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost,
		cluster.httpEndpoint+"/api/v1/iam/refresh", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cluster.client.Do(req)
	require.NoError(t, err, "refresh request")
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var tokens sessionTokens
	_ = json.Unmarshal(body, &tokens)
	return resp.StatusCode, tokens, string(body)
}

// getWithBearer issues an authenticated GET and returns the status code.
func getWithBearer(t *testing.T, cluster *testCluster, path, bearer string) (int, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, cluster.httpEndpoint+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := cluster.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// TestSessionRefreshEndToEnd proves a login yields a renewable session and that
// the renewed bearer authenticates a real API call.
func TestSessionRefreshEndToEnd(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	login := pivLoginSession(t, cluster)

	// (1) The login MUST hand back a renewal credential. Without it the client
	// has nothing to refresh with and the session dies at the first expiry —
	// the root cause of both reported failures.
	require.NotEmpty(t, login.RefreshToken,
		"login returned no refresh_token; the session cannot outlive the access token")

	// (2) Renewal must succeed. Before the fix this endpoint answered 503
	// "authentication service not configured" for every request.
	status, renewed, body := postRefresh(t, cluster, login.RefreshToken)
	require.Equal(t, http.StatusOK, status, "refresh failed: %s", body)
	require.NotEmpty(t, renewed.AccessToken, "refresh returned no access token: %s", body)
	assert.NotEqual(t, login.AccessToken, renewed.AccessToken,
		"refresh returned the same access token")

	// (3) The credential must be ROTATED, and the rotated value returned, or
	// the client is locked out at the next renewal.
	require.NotEmpty(t, renewed.RefreshToken, "refresh did not return a rotated credential")
	assert.NotEqual(t, login.RefreshToken, renewed.RefreshToken,
		"refresh reused the presented credential instead of rotating it")

	// (4) expires_in must be a sane, short lifetime in SECONDS. The previous
	// implementation returned nanoseconds, making this arithmetic nonsense.
	assert.Greater(t, renewed.ExpiresIn, int64(0), "expires_in must be positive")
	assert.LessOrEqual(t, renewed.ExpiresIn, int64(3600),
		"expires_in %d exceeds the 60m access-token cap", renewed.ExpiresIn)

	// (5) The renewed bearer must authenticate a protected endpoint. This is
	// the assertion that maps directly to the operator's symptom: a page that
	// previously returned {"error":"Invalid or expired token"}.
	apiStatus, apiBody := getWithBearer(t, cluster, "/api/v1/tenants", renewed.AccessToken)
	assert.Equal(t, http.StatusOK, apiStatus,
		"renewed bearer was rejected by /api/v1/tenants: %s", apiBody)

	// (6) The renewed session must itself be renewable — a session survives
	// many 15-minute boundaries, not just the first.
	status2, renewed2, body2 := postRefresh(t, cluster, renewed.RefreshToken)
	require.Equal(t, http.StatusOK, status2, "second refresh failed: %s", body2)
	assert.NotEmpty(t, renewed2.AccessToken)
	assert.NotEqual(t, renewed.RefreshToken, renewed2.RefreshToken)
}

// TestSessionRefreshRejectsReplayedCredential proves rotation is enforced: a
// credential that has already been redeemed is refused, which is the standard
// OAuth 2.1 detection for a stolen refresh token.
func TestSessionRefreshRejectsReplayedCredential(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	login := pivLoginSession(t, cluster)
	require.NotEmpty(t, login.RefreshToken)

	status, _, body := postRefresh(t, cluster, login.RefreshToken)
	require.Equal(t, http.StatusOK, status, "first refresh failed: %s", body)

	replayStatus, _, replayBody := postRefresh(t, cluster, login.RefreshToken)
	assert.Equal(t, http.StatusUnauthorized, replayStatus,
		"a rotated credential must not be redeemable twice: %s", replayBody)
}

// TestSessionRefreshRejectsUnknownCredential proves the endpoint is LIVE and
// makes a real decision. The regression it guards: the handler used to answer
// 503 "authentication service not configured" no matter what was presented, so
// a client could never tell a dead endpoint from a dead session.
func TestSessionRefreshRejectsUnknownCredential(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	status, _, body := postRefresh(t, cluster, "this-credential-was-never-issued")

	assert.Equal(t, http.StatusUnauthorized, status,
		"unknown credential should be 401, got %d: %s", status, body)
	assert.NotContains(t, body, "authentication service not configured",
		"the refresh endpoint is not wired to a live token authority")
}

// TestSessionRefreshRequiresCredential proves input validation.
func TestSessionRefreshRequiresCredential(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	status, _, body := postRefresh(t, cluster, "")

	assert.Equal(t, http.StatusBadRequest, status,
		"missing refresh_token should be 400, got %d: %s", status, body)
}
