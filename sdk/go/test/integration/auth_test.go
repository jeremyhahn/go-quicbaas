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

// The SDK integration dev server (sdk/go/test/integration/docker-compose.yml)
// runs in the stack's insecure mode: cleartext REST/gRPC and no IAM auth
// session backend. The only authentication mechanism in the stack is PIV
// (mTLS client cert) / WebAuthn / FIDO2 — username/password login was removed.
//
// These tests therefore exercise the auth-related endpoints that ARE reachable
// without an established session on the insecure dev server:
//
//   - /.well-known/jwks.json                 (public JWKS)
//   - /.well-known/openid-configuration      (public OIDC discovery)
//   - POST /api/v1/auth/login/piv            (PIV mTLS login; rejects non-mTLS)
//   - POST /api/v1/auth/refresh              (rejects invalid refresh tokens)
//   - POST /api/v1/auth/validate             (rejects requests without a bearer)
//   - POST /api/v1/auth/logout               (rejects requests without a token)
//
// The removed username/password login flow (/api/v1/auth/login) is intentionally
// not tested — that route no longer exists.

// TestAuthEndpointDiscovery verifies that the public auth discovery and
// token-management endpoints are reachable on the running server.
func TestAuthEndpointDiscovery(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	endpoints := []struct {
		name     string
		path     string
		method   string
		wantCode []int // acceptable status codes
	}{
		// Public OIDC/JWKS discovery endpoints are served at root level
		// regardless of whether IAM auth is enabled (see pkg/api/rest/iam.go).
		{"jwks", "/.well-known/jwks.json", http.MethodGet, []int{http.StatusOK}},
		{"openid-config", "/.well-known/openid-configuration", http.MethodGet, []int{http.StatusOK}},
		// PIV login is the canonical login route. Without a presented mTLS
		// client certificate it must reject the request rather than 404.
		{"login-piv", "/api/v1/auth/login/piv", http.MethodPost, []int{http.StatusBadRequest, http.StatusUnauthorized}},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			ctx, cancel := DefaultContext(t)
			defer cancel()

			var req *http.Request
			var err error

			if ep.method == http.MethodPost {
				req, err = http.NewRequestWithContext(ctx, ep.method, GetRESTAddress()+ep.path, bytes.NewBufferString("{}"))
				if err == nil {
					req.Header.Set("Content-Type", "application/json")
				}
			} else {
				req, err = http.NewRequestWithContext(ctx, ep.method, GetRESTAddress()+ep.path, nil)
			}
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			client := newHTTPClient(10 * time.Second)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
				"endpoint %s must be registered on the server", ep.path)

			found := false
			for _, code := range ep.wantCode {
				if resp.StatusCode == code {
					found = true
					break
				}
			}
			if !found {
				body, _ := io.ReadAll(resp.Body)
				t.Errorf("endpoint %s returned status %d, want one of %v (body: %s)",
					ep.path, resp.StatusCode, ep.wantCode, string(body))
			}
		})
	}
}

// TestPIVLoginRejectsNonMTLS verifies that the PIV login endpoint exists and
// rejects callers that do not present a valid mTLS client certificate. This is
// the replacement for the removed username/password login validation: the
// stack authenticates via PIV / WebAuthn / FIDO2 only.
func TestPIVLoginRejectsNonMTLS(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("login without client cert is rejected", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/login/piv", bytes.NewBufferString("{}"))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// The route must exist (not 404) and must refuse a cert-less caller.
		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"PIV login endpoint must be registered on the server")
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status 400 or 401 for PIV login without a client cert, got %d (body: %s)",
				resp.StatusCode, string(body))
		}
	})
}

// TestTokenValidation verifies that the token validation endpoint exists and
// reports failure for requests that present no bearer token or an invalid one.
func TestTokenValidation(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("missing bearer token returns bad request", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/validate", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"token validation endpoint must be registered on the server")
		if resp.StatusCode != http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status 400 for validate without a bearer token, got %d (body: %s)",
				resp.StatusCode, string(body))
		}
	})

	t.Run("invalid token reports not valid", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/validate", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer invalid_token_here")

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		// ValidateToken returns 200 with {"valid": false} for an unparseable
		// token rather than an HTTP error (see pkg/api/rest/iam_login.go).
		require.Equal(t, http.StatusOK, resp.StatusCode,
			"validate with a malformed bearer token should return 200 with valid=false")

		var result struct {
			Valid bool `json:"valid"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode validate response: %v", err)
		}
		require.False(t, result.Valid, "an invalid token must not validate")
	})
}

// TestTokenRefresh verifies that the token refresh endpoint exists and rejects
// invalid refresh tokens.
func TestTokenRefresh(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("invalid refresh token returns error", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		reqBody := map[string]string{
			"refresh_token": "invalid_refresh_token",
		}
		body, err := json.Marshal(reqBody)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/refresh", bytes.NewBuffer(body))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"token refresh endpoint must be registered on the server")
		// An invalid token yields 401; a server without a configured auth
		// service yields 503; a malformed body yields 400.
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusBadRequest, http.StatusServiceUnavailable:
			// expected
		default:
			body, _ := io.ReadAll(resp.Body)
			t.Errorf("expected status 401, 400, or 503, got %d (body: %s)", resp.StatusCode, string(body))
		}
	})

	t.Run("missing refresh token returns bad request", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/refresh", bytes.NewBufferString("{}"))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		require.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"refresh without a refresh_token must return 400")
	})
}

// TestJWKS verifies that the public JWKS endpoint is available and returns a
// well-formed key set.
func TestJWKS(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("get JWKS endpoint", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/.well-known/jwks.json", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode,
			"JWKS endpoint must return 200 on the integration test server")

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

// TestOpenIDConfiguration verifies the OpenID Configuration discovery endpoint
// is available and advertises a non-empty issuer.
func TestOpenIDConfiguration(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("get OpenID configuration", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, GetRESTAddress()+"/.well-known/openid-configuration", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode,
			"OpenID Configuration endpoint must return 200 on the integration test server")

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

		require.NotEmpty(t, config.Issuer, "OpenID Configuration must advertise an issuer")

		t.Logf("OpenID Configuration:")
		t.Logf("  Issuer: %s", config.Issuer)
		t.Logf("  Authorization Endpoint: %s", config.AuthorizationEndpoint)
		t.Logf("  Token Endpoint: %s", config.TokenEndpoint)
		t.Logf("  JWKS URI: %s", config.JWKSUri)
		t.Logf("  Scopes Supported: %v", config.ScopesSupported)
	})
}

// TestLogout verifies the logout endpoint exists and rejects requests that
// present no token.
func TestLogout(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	t.Run("logout without a token returns bad request", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GetRESTAddress()+"/api/v1/auth/logout", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}

		client := newHTTPClient(10 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"logout endpoint must be registered on the server")
		// Logout with no Authorization header and no body token returns 400
		// (see pkg/api/rest/iam_login.go Logout).
		require.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"logout without a token must return 400")
	})
}
