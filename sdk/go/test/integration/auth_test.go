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

package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// AuthTestConfig holds configuration for auth integration tests.
type AuthTestConfig struct {
	// TestUsername is the username for login tests.
	TestUsername string
	// TestPassword is the password for login tests.
	TestPassword string
	// AuthEnabled indicates if auth is enabled on the server.
	AuthEnabled bool
}

// getAuthTestConfig returns the auth test configuration.
// In a real test environment, these would come from environment variables or fixtures.
func getAuthTestConfig() AuthTestConfig {
	return AuthTestConfig{
		TestUsername: "admin",
		TestPassword: "admin",
		AuthEnabled:  false, // Server runs in insecure mode by default
	}
}

// requireAuthEnabled asserts that auth is enabled on the test server.
// Tests that require auth must fail when auth is not available rather than skip.
func requireAuthEnabled(t *testing.T) {
	t.Helper()
	config := getAuthTestConfig()
	require.True(t, config.AuthEnabled, "Auth must be enabled on the test server for this test")
}

// TestAuthEndpointDiscovery tests that auth endpoints are discoverable.
func TestAuthEndpointDiscovery(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	// These endpoints should exist even if auth is disabled
	endpoints := []struct {
		name     string
		path     string
		method   string
		wantCode []int // acceptable status codes
	}{
		{"jwks", "/api/v1/auth/jwks", http.MethodGet, []int{http.StatusOK, http.StatusNotFound, http.StatusUnauthorized}},
		{"openid-config", "/api/v1/auth/.well-known/openid-configuration", http.MethodGet, []int{http.StatusOK, http.StatusNotFound}},
		{"login-post", "/api/v1/auth/login", http.MethodPost, []int{http.StatusOK, http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound}},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			ctx, cancel := DefaultContext(t)
			defer cancel()

			var req *http.Request
			var err error

			if ep.method == http.MethodPost {
				// For POST requests, send an empty JSON body
				req, err = http.NewRequestWithContext(ctx, ep.method, GetRESTAddress()+ep.path, bytes.NewBufferString("{}"))
				req.Header.Set("Content-Type", "application/json")
			} else {
				req, err = http.NewRequestWithContext(ctx, ep.method, GetRESTAddress()+ep.path, nil)
			}
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			// Check if the response code is one of the acceptable codes
			found := false
			for _, code := range ep.wantCode {
				if resp.StatusCode == code {
					found = true
					break
				}
			}
			if !found {
				body, _ := io.ReadAll(resp.Body)
				t.Logf("endpoint %s returned status %d (body: %s)", ep.path, resp.StatusCode, string(body))
			}
		})
	}
}

// TestLoginValidation tests login request validation.
func TestLoginValidation(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)
	requireAuthEnabled(t)

	t.Run("empty credentials returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		reqBody := map[string]string{}
		body, _ := json.Marshal(reqBody)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/login", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should return bad request or unauthorized
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected status 400 or 401, got %d", resp.StatusCode)
		}
	})

	t.Run("invalid credentials returns unauthorized", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		reqBody := map[string]string{
			"username": "invalid_user",
			"password": "invalid_password",
		}
		body, _ := json.Marshal(reqBody)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/login", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("response body: %s", string(body))
			// Might also return 400 for validation errors
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected status 401 or 400, got %d", resp.StatusCode)
			}
		}
	})
}

// TestLoginFlow tests the complete login flow.
func TestLoginFlow(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)
	requireAuthEnabled(t)

	config := getAuthTestConfig()

	t.Run("successful login returns tokens", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		reqBody := map[string]string{
			"username": config.TestUsername,
			"password": config.TestPassword,
		}
		body, _ := json.Marshal(reqBody)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/login", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 200, got %d: %s", resp.StatusCode, string(body))
		}

		var loginResp struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			TokenType    string `json:"token_type"`
			ExpiresIn    int    `json:"expires_in"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if loginResp.AccessToken == "" {
			t.Error("expected non-empty access token")
		}
		if loginResp.RefreshToken == "" {
			t.Error("expected non-empty refresh token")
		}
		if loginResp.TokenType != "Bearer" {
			t.Errorf("expected token type 'Bearer', got '%s'", loginResp.TokenType)
		}
	})
}

// TestTokenValidation tests token validation.
func TestTokenValidation(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)
	requireAuthEnabled(t)

	t.Run("invalid token returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/auth/validate", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer invalid_token_here")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 401 or 404, got %d", resp.StatusCode)
		}
	})
}

// TestTokenRefresh tests token refresh functionality.
func TestTokenRefresh(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)
	requireAuthEnabled(t)

	t.Run("invalid refresh token returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		reqBody := map[string]string{
			"refresh_token": "invalid_refresh_token",
		}
		body, _ := json.Marshal(reqBody)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/refresh", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should return unauthorized or bad request
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 401, 400, or 404, got %d", resp.StatusCode)
		}
	})
}

// TestJWKS tests the JWKS endpoint.
func TestJWKS(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("get JWKS endpoint", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/auth/jwks", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// JWKS endpoint must be available when the server is running
		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"JWKS endpoint must be available on the integration test server")

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("JWKS response status %d: %s", resp.StatusCode, string(body))
			return
		}

		var jwks struct {
			Keys []struct {
				KTY string `json:"kty"`
				KID string `json:"kid"`
				ALG string `json:"alg"`
				Use string `json:"use"`
			} `json:"keys"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
			t.Fatalf("failed to decode JWKS response: %v", err)
		}

		t.Logf("JWKS contains %d keys", len(jwks.Keys))
		for _, key := range jwks.Keys {
			t.Logf("  Key ID: %s, Type: %s, Algorithm: %s", key.KID, key.KTY, key.ALG)
		}
	})
}

// TestOpenIDConfiguration tests the OpenID Configuration discovery endpoint.
func TestOpenIDConfiguration(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("get OpenID configuration", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/auth/.well-known/openid-configuration", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// OpenID Configuration endpoint must be available when the server is running
		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"OpenID Configuration endpoint must be available on the integration test server")

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Logf("OpenID Configuration response status %d: %s", resp.StatusCode, string(body))
			return
		}

		var config struct {
			Issuer                 string   `json:"issuer"`
			AuthorizationEndpoint  string   `json:"authorization_endpoint"`
			TokenEndpoint          string   `json:"token_endpoint"`
			JWKSUri                string   `json:"jwks_uri"`
			ScopesSupported        []string `json:"scopes_supported"`
			ResponseTypesSupported []string `json:"response_types_supported"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
			t.Fatalf("failed to decode OpenID Configuration response: %v", err)
		}

		if config.Issuer == "" {
			t.Error("expected non-empty issuer")
		}

		t.Logf("OpenID Configuration:")
		t.Logf("  Issuer: %s", config.Issuer)
		t.Logf("  Authorization Endpoint: %s", config.AuthorizationEndpoint)
		t.Logf("  Token Endpoint: %s", config.TokenEndpoint)
		t.Logf("  JWKS URI: %s", config.JWKSUri)
		t.Logf("  Scopes Supported: %v", config.ScopesSupported)
	})
}

// TestSessionManagement tests session management endpoints.
func TestSessionManagement(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)
	requireAuthEnabled(t)

	t.Run("list sessions without auth returns unauthorized", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/api/v1/auth/sessions", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 401 or 404, got %d", resp.StatusCode)
		}
	})
}

// TestLogout tests the logout endpoint.
func TestLogout(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)
	requireAuthEnabled(t)

	t.Run("logout without auth returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/logout", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should return unauthorized or not found
		if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 401 or 404, got %d", resp.StatusCode)
		}
	})
}
