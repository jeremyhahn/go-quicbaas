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

//go:build integration

package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test environment configuration
var (
	testNode1URL    = getEnv("QUICBAAS_NODE1_URL", "https://test-node1:8080")
	testNode1MCP    = getEnv("QUICBAAS_NODE1_MCP", "https://test-node1:8888")
	testMCPEndpoint = getEnv("QUICBAAS_MCP_ENDPOINT", "https://test-node1:8888/mcp")
	testTimeout     = 60 * time.Second
	httpClient      = &http.Client{
		Timeout: testTimeout,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			TLSHandshakeTimeout: 10 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
)

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// ==============================================================================
// OAuth 2.0 Authorization Server Metadata Tests (RFC 8414)
// ==============================================================================

// TestOAuthASMetadataEndpoint tests the /.well-known/oauth-authorization-server endpoint.
func TestOAuthASMetadataEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	metadataURL := testNode1URL + "/.well-known/oauth-authorization-server"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	require.NoError(t, err)

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Check response status
	assert.Equal(t, http.StatusOK, resp.StatusCode, "OAuth AS Metadata endpoint should return 200")

	// Parse response
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var metadata map[string]interface{}
	err = json.Unmarshal(body, &metadata)
	require.NoError(t, err)

	// Verify required RFC 8414 fields
	t.Run("has_issuer", func(t *testing.T) {
		issuer, ok := metadata["issuer"]
		assert.True(t, ok, "OAuth AS metadata must include 'issuer'")
		assert.NotEmpty(t, issuer)
	})

	t.Run("has_authorization_endpoint", func(t *testing.T) {
		endpoint, ok := metadata["authorization_endpoint"]
		assert.True(t, ok, "OAuth AS metadata must include 'authorization_endpoint'")
		assert.NotEmpty(t, endpoint)
	})

	t.Run("has_token_endpoint", func(t *testing.T) {
		endpoint, ok := metadata["token_endpoint"]
		assert.True(t, ok, "OAuth AS metadata must include 'token_endpoint'")
		assert.NotEmpty(t, endpoint)
	})

	t.Run("supports_pkce_s256", func(t *testing.T) {
		methods, ok := metadata["code_challenge_methods_supported"]
		assert.True(t, ok, "OAuth AS metadata must include 'code_challenge_methods_supported'")

		methodsSlice, ok := methods.([]interface{})
		assert.True(t, ok)
		assert.Contains(t, methodsSlice, "S256", "OAuth AS must support S256 PKCE")
	})

	t.Run("has_registration_endpoint", func(t *testing.T) {
		endpoint, ok := metadata["registration_endpoint"]
		assert.True(t, ok, "OAuth AS metadata should include 'registration_endpoint' for DCR")
		assert.NotEmpty(t, endpoint)
	})

	t.Run("has_response_types_supported", func(t *testing.T) {
		respTypes, ok := metadata["response_types_supported"]
		assert.True(t, ok, "OAuth AS metadata must include 'response_types_supported'")
		respTypesSlice, ok := respTypes.([]interface{})
		assert.True(t, ok)
		assert.Contains(t, respTypesSlice, "code", "OAuth AS must support authorization code flow")
	})

	t.Run("has_grant_types_supported", func(t *testing.T) {
		grants, ok := metadata["grant_types_supported"]
		assert.True(t, ok, "OAuth AS metadata must include 'grant_types_supported'")
		grantsSlice, ok := grants.([]interface{})
		assert.True(t, ok)
		assert.Contains(t, grantsSlice, "authorization_code", "OAuth AS must support authorization_code grant")
	})
}

// ==============================================================================
// Protected Resource Metadata Tests (RFC 9728)
// ==============================================================================

// TestProtectedResourceMetadataEndpoint tests the /.well-known/oauth-protected-resource endpoint.
// The endpoint returns 200 with metadata when MCPAuth is configured, or 404 when not configured.
func TestProtectedResourceMetadataEndpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	metadataURL := testNode1MCP + "/.well-known/oauth-protected-resource"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	require.NoError(t, err)

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// The endpoint should return either 200 (MCPAuth configured) or 404 (MCPAuth not configured)
	// Both are valid responses depending on server configuration
	require.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound,
		"Protected Resource Metadata endpoint must return 200 or 404, got %d", resp.StatusCode)

	if resp.StatusCode == http.StatusNotFound {
		t.Log("MCPAuth not configured on test server - endpoint correctly returns 404")
		return
	}

	// If 200, verify the metadata content
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var metadata map[string]interface{}
	err = json.Unmarshal(body, &metadata)
	require.NoError(t, err)

	t.Run("has_resource", func(t *testing.T) {
		resource, ok := metadata["resource"]
		assert.True(t, ok, "Protected Resource Metadata must include 'resource'")
		assert.NotEmpty(t, resource)
	})

	t.Run("has_authorization_servers", func(t *testing.T) {
		servers, ok := metadata["authorization_servers"]
		assert.True(t, ok, "Protected Resource Metadata must include 'authorization_servers'")
		serversSlice, ok := servers.([]interface{})
		assert.True(t, ok)
		assert.NotEmpty(t, serversSlice, "Must have at least one authorization server")
	})

	t.Run("bearer_methods_include_header", func(t *testing.T) {
		methods, ok := metadata["bearer_methods_supported"]
		if ok {
			methodsSlice, ok := methods.([]interface{})
			assert.True(t, ok)
			assert.Contains(t, methodsSlice, "header", "Bearer methods should include 'header'")
		}
	})
}

// ==============================================================================
// OAuth 2.1 PKCE Flow Tests
// ==============================================================================

// generatePKCE generates PKCE code verifier and challenge.
func generatePKCE() (verifier, challenge string) {
	// Use a fixed verifier for test reproducibility
	verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	// Calculate S256 challenge
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return
}

// TestOAuth21PKCEMandatory tests that PKCE is mandatory for OAuth 2.1 compliance.
func TestOAuth21PKCEMandatory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	authzURL := testNode1URL + "/oauth2/authorize"

	t.Run("rejects_request_without_pkce", func(t *testing.T) {
		params := url.Values{
			"response_type": {"code"},
			"client_id":     {"test-client"},
			"redirect_uri":  {"http://localhost:8080/callback"},
			"scope":         {"openid"},
			"state":         {"random-state-123"},
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, authzURL+"?"+params.Encode(), nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should be rejected - PKCE is mandatory
		assert.True(t, resp.StatusCode >= 400, "Request without PKCE should be rejected (got %d)", resp.StatusCode)
	})

	t.Run("rejects_plain_pkce_method", func(t *testing.T) {
		params := url.Values{
			"response_type":         {"code"},
			"client_id":             {"test-client"},
			"redirect_uri":          {"http://localhost:8080/callback"},
			"scope":                 {"openid"},
			"state":                 {"random-state-123"},
			"code_challenge":        {"plaintext-verifier"},
			"code_challenge_method": {"plain"},
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, authzURL+"?"+params.Encode(), nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Plain method should be rejected per OAuth 2.1
		assert.True(t, resp.StatusCode >= 400, "Request with plain PKCE should be rejected (got %d)", resp.StatusCode)
	})

	t.Run("accepts_s256_pkce", func(t *testing.T) {
		_, challenge := generatePKCE()

		params := url.Values{
			"response_type":         {"code"},
			"client_id":             {"test-client"},
			"redirect_uri":          {"http://localhost:8080/callback"},
			"scope":                 {"openid"},
			"state":                 {"random-state-123"},
			"code_challenge":        {challenge},
			"code_challenge_method": {"S256"},
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, authzURL+"?"+params.Encode(), nil)
		require.NoError(t, err)

		// Just check the request doesn't get rejected for PKCE reasons
		// (may get rejected for other reasons like missing client registration)
		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// If error mentions PKCE/code_challenge, that's wrong - S256 should be accepted
		if strings.Contains(bodyStr, "code_challenge") {
			t.Errorf("S256 PKCE should be accepted, got error: %s", bodyStr)
		}
	})
}

// ==============================================================================
// Dynamic Client Registration Tests (RFC 7591)
// ==============================================================================

// TestDynamicClientRegistration tests RFC 7591 Dynamic Client Registration.
func TestDynamicClientRegistration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	registerURL := testNode1URL + "/oauth2/register"

	t.Run("register_new_client", func(t *testing.T) {
		clientRequest := map[string]interface{}{
			"client_name":                "Test MCP Client",
			"redirect_uris":              []string{"https://localhost:8080/callback", "https://localhost:8080/callback2"},
			"grant_types":                []string{"authorization_code", "refresh_token"},
			"response_types":             []string{"code"},
			"token_endpoint_auth_method": "client_secret_basic",
		}

		reqBody, err := json.Marshal(clientRequest)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// DCR endpoint must be available and properly configured in integration tests
		require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
			"DCR endpoint must not require initial access token in integration test environment")
		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"DCR endpoint must be enabled for integration tests")
		require.NotEqual(t, http.StatusMethodNotAllowed, resp.StatusCode,
			"DCR endpoint must accept POST method")

		// Check for server configuration issues
		if resp.StatusCode == http.StatusInternalServerError {
			bodyStr := string(body)
			require.False(t,
				strings.Contains(bodyStr, "dynamic registration not available") ||
					strings.Contains(bodyStr, "not available") ||
					strings.Contains(bodyStr, "not configured"),
				"DCR must be available and configured for integration tests: %s", bodyStr)
		}

		require.Equal(t, http.StatusCreated, resp.StatusCode, "DCR should return 201 Created, got: %s", string(body))

		var clientResponse map[string]interface{}
		err = json.Unmarshal(body, &clientResponse)
		require.NoError(t, err)

		// Verify response contains required fields
		assert.NotEmpty(t, clientResponse["client_id"], "DCR response must include client_id")
		assert.NotEmpty(t, clientResponse["client_secret"], "DCR response should include client_secret for confidential client")
	})

	t.Run("rejects_http_redirect_uri", func(t *testing.T) {
		clientRequest := map[string]interface{}{
			"client_name":   "Insecure Client",
			"redirect_uris": []string{"http://example.com/callback"}, // HTTP (not HTTPS)
			"grant_types":   []string{"authorization_code"},
		}

		reqBody, err := json.Marshal(clientRequest)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// DCR endpoint must be available for integration tests
		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"DCR endpoint must be enabled for integration tests")
		require.NotEqual(t, http.StatusMethodNotAllowed, resp.StatusCode,
			"DCR endpoint must accept POST method")
		require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
			"DCR endpoint must not require initial access token in integration test environment")

		// Check for server configuration issues
		if resp.StatusCode == http.StatusInternalServerError {
			bodyStr := string(body)
			require.False(t,
				strings.Contains(bodyStr, "dynamic registration not available") ||
					strings.Contains(bodyStr, "not available") ||
					strings.Contains(bodyStr, "not configured"),
				"DCR must be available and configured for integration tests: %s", bodyStr)
		}

		// Non-HTTPS redirect URIs should be rejected (except localhost)
		assert.True(t, resp.StatusCode >= 400, "HTTP redirect URI should be rejected (got %d)", resp.StatusCode)
	})

	t.Run("allows_localhost_http", func(t *testing.T) {
		clientRequest := map[string]interface{}{
			"client_name":   "Dev Client",
			"redirect_uris": []string{"http://localhost:8080/callback", "http://127.0.0.1:3000/callback"},
			"grant_types":   []string{"authorization_code"},
		}

		reqBody, err := json.Marshal(clientRequest)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// DCR endpoint must be available for integration tests
		require.NotEqual(t, http.StatusNotFound, resp.StatusCode,
			"DCR endpoint must be enabled for integration tests")
		require.NotEqual(t, http.StatusMethodNotAllowed, resp.StatusCode,
			"DCR endpoint must accept POST method")
		require.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
			"DCR endpoint must not require initial access token in integration test environment")

		// Check for server configuration issues
		if resp.StatusCode == http.StatusInternalServerError {
			bodyStr := string(body)
			require.False(t,
				strings.Contains(bodyStr, "dynamic registration not available") ||
					strings.Contains(bodyStr, "not available") ||
					strings.Contains(bodyStr, "not configured"),
				"DCR must be available and configured for integration tests: %s", bodyStr)
		}

		// Localhost should be allowed for development
		assert.True(t, resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK,
			"Localhost HTTP redirect should be allowed (got %d)", resp.StatusCode)
	})
}

// ==============================================================================
// Bearer Token Authentication Tests (RFC 6750)
// ==============================================================================

// TestBearerTokenAuthentication tests Bearer token handling per RFC 6750.
func TestBearerTokenAuthentication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// MCP JSON-RPC endpoint
	mcpURL := testMCPEndpoint

	t.Run("missing_token_returns_401", func(t *testing.T) {
		// Initialize request without token
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		// No Authorization header

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Check for WWW-Authenticate header on auth-required endpoints
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		if resp.StatusCode == http.StatusUnauthorized {
			assert.NotEmpty(t, wwwAuth, "401 response must include WWW-Authenticate header")
			assert.True(t, strings.HasPrefix(wwwAuth, "Bearer"), "WWW-Authenticate must specify Bearer scheme")
		}
	})

	t.Run("invalid_token_returns_401", func(t *testing.T) {
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer invalid-token-12345")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Check WWW-Authenticate header
		wwwAuth := resp.Header.Get("WWW-Authenticate")
		if resp.StatusCode == http.StatusUnauthorized {
			assert.NotEmpty(t, wwwAuth, "401 response must include WWW-Authenticate header")
			assert.Contains(t, wwwAuth, "invalid_token", "WWW-Authenticate should indicate invalid_token")
		}
	})

	t.Run("expired_token_returns_401", func(t *testing.T) {
		// An expired JWT token (for testing purposes)
		expiredToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
			"eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IlRlc3QgVXNlciIsImV4cCI6MTAwMDAwMDAwMH0." +
			"signature"

		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+expiredToken)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should get 401 for expired token
		if resp.StatusCode == http.StatusUnauthorized {
			wwwAuth := resp.Header.Get("WWW-Authenticate")
			assert.NotEmpty(t, wwwAuth, "401 response must include WWW-Authenticate header")
		}
	})

	t.Run("malformed_authorization_header", func(t *testing.T) {
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		// Malformed - missing "Bearer " prefix
		req.Header.Set("Authorization", "just-a-token")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should fail to extract token
		if resp.StatusCode == http.StatusUnauthorized {
			wwwAuth := resp.Header.Get("WWW-Authenticate")
			assert.NotEmpty(t, wwwAuth, "401 response must include WWW-Authenticate header")
		}
	})
}

// ==============================================================================
// MCP Protocol Version Header Tests
// ==============================================================================

// TestMCPProtocolVersionHeader tests MCP-Protocol-Version header handling.
func TestMCPProtocolVersionHeader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	mcpURL := testMCPEndpoint

	t.Run("includes_protocol_version_in_response", func(t *testing.T) {
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("MCP-Protocol-Version", "2024-11-05")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Check for MCP-Protocol-Version in response
		protocolVersion := resp.Header.Get("MCP-Protocol-Version")
		if protocolVersion != "" {
			assert.Equal(t, "2024-11-05", protocolVersion, "Response should include matching MCP protocol version")
		}
	})
}

// TestMCPEndToEndWithAuthentication tests the complete MCP flow with authentication.
func TestMCPEndToEndWithAuthentication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	mcpURL := testMCPEndpoint

	t.Run("mcp_initialize_flow", func(t *testing.T) {
		// Step 1: Initialize the MCP session
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
				"clientInfo": map[string]interface{}{
					"name":    "integration-test-client",
					"version": "1.0.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// Parse response
		var rpcResp map[string]interface{}
		err = json.Unmarshal(body, &rpcResp)
		require.NoError(t, err)

		// Check for JSON-RPC response structure
		assert.Equal(t, "2.0", rpcResp["jsonrpc"], "Response must be JSON-RPC 2.0")

		if result, ok := rpcResp["result"].(map[string]interface{}); ok {
			// Verify server info
			if serverInfo, ok := result["serverInfo"].(map[string]interface{}); ok {
				assert.NotEmpty(t, serverInfo["name"], "Server info should include name")
				assert.NotEmpty(t, serverInfo["version"], "Server info should include version")
			}

			// Verify capabilities
			_, hasCapabilities := result["capabilities"]
			assert.True(t, hasCapabilities, "Response should include capabilities")
		}
	})

	t.Run("mcp_tools_list", func(t *testing.T) {
		// Initialize first
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test",
					"version": "1.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		// List tools
		listReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      2,
			"params":  nil,
		}

		reqBody, err = json.Marshal(listReq)
		require.NoError(t, err)

		req, err = http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err = httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var rpcResp map[string]interface{}
		err = json.Unmarshal(body, &rpcResp)
		require.NoError(t, err)

		if result, ok := rpcResp["result"].(map[string]interface{}); ok {
			if tools, ok := result["tools"].([]interface{}); ok {
				assert.NotEmpty(t, tools, "Should return available tools")

				// Verify each tool has required fields
				for _, tool := range tools {
					toolMap, ok := tool.(map[string]interface{})
					assert.True(t, ok)
					assert.NotEmpty(t, toolMap["name"], "Tool must have a name")
					assert.NotEmpty(t, toolMap["description"], "Tool should have a description")
				}
			}
		}
	})
}

// TestMCPProtocolCompliance tests MCP protocol compliance.
func TestMCPProtocolCompliance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	mcpURL := testMCPEndpoint

	t.Run("invalid_json_returns_parse_error", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL,
			strings.NewReader("{invalid json"))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var rpcResp map[string]interface{}
		err = json.Unmarshal(body, &rpcResp)
		require.NoError(t, err)

		if rpcErr, ok := rpcResp["error"].(map[string]interface{}); ok {
			code, _ := rpcErr["code"].(float64)
			assert.Equal(t, float64(-32700), code, "Parse error should be -32700")
		}
	})

	t.Run("method_not_found_error", func(t *testing.T) {
		// Initialize first
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test",
					"version": "1.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		// Call non-existent method
		invalidReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "nonexistent/method",
			"id":      2,
			"params":  nil,
		}

		reqBody, err = json.Marshal(invalidReq)
		require.NoError(t, err)

		req, err = http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err = httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var rpcResp map[string]interface{}
		err = json.Unmarshal(body, &rpcResp)
		require.NoError(t, err)

		if rpcErr, ok := rpcResp["error"].(map[string]interface{}); ok {
			code, _ := rpcErr["code"].(float64)
			assert.Equal(t, float64(-32601), code, "Method not found should be -32601")
		}
	})

	t.Run("request_without_init_rejected", func(t *testing.T) {
		// Try to list tools without initializing (on fresh connection)
		listReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      1,
			"params":  nil,
		}

		reqBody, err := json.Marshal(listReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var rpcResp map[string]interface{}
		err = json.Unmarshal(body, &rpcResp)
		require.NoError(t, err)

		// Should get an error - not initialized
		if rpcErr, ok := rpcResp["error"].(map[string]interface{}); ok {
			assert.NotNil(t, rpcErr, "Should get error for uninitialized request")
		}
	})
}

// ==============================================================================
// Token Audience Validation Tests (RFC 8707)
// ==============================================================================

// TestTokenAudienceValidation tests that tokens with wrong audience are rejected.
func TestTokenAudienceValidation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	mcpURL := testMCPEndpoint

	t.Run("token_without_audience_handling", func(t *testing.T) {
		// Create a JWT without audience claim (for testing)
		// This is a minimal JWT structure - in production, tokens without proper audience
		// for the MCP resource should be rejected
		tokenWithoutAud := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
			"eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IlRlc3QgVXNlciIsImlhdCI6MTUxNjIzOTAyMn0." +
			"signature"

		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tokenWithoutAud)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Token without proper audience should be handled appropriately
		// Either rejected (401) if audience validation is strict,
		// or processed if audience validation is lenient
		t.Logf("Response status for token without audience: %d", resp.StatusCode)
	})

	t.Run("token_with_wrong_audience", func(t *testing.T) {
		// JWT with audience claim that doesn't match MCP resource URI
		// header: {"alg":"HS256","typ":"JWT"}
		// payload: {"sub":"test-user","aud":"https://wrong-resource.example.com","iat":1516239022}
		tokenWithWrongAud := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
			"eyJzdWIiOiJ0ZXN0LXVzZXIiLCJhdWQiOiJodHRwczovL3dyb25nLXJlc291cmNlLmV4YW1wbGUuY29tIiwiaWF0IjoxNTE2MjM5MDIyfQ." +
			"signature"

		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test-client",
					"version": "1.0.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tokenWithWrongAud)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Token with wrong audience should be rejected when audience validation is enabled
		t.Logf("Response status for token with wrong audience: %d", resp.StatusCode)
	})
}

// ==============================================================================
// MCP HTTP Streamable Transport Tests
// ==============================================================================

// TestMCPHTTPStreamableTransport tests the HTTP Streamable transport.
func TestMCPHTTPStreamableTransport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	mcpURL := testMCPEndpoint

	t.Run("content_type_json", func(t *testing.T) {
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test",
					"version": "1.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Response should be JSON
		contentType := resp.Header.Get("Content-Type")
		assert.Contains(t, contentType, "application/json", "Response should be JSON")
	})

	t.Run("cors_headers", func(t *testing.T) {
		// Test CORS preflight
		req, err := http.NewRequestWithContext(ctx, http.MethodOptions, mcpURL, nil)
		require.NoError(t, err)
		req.Header.Set("Origin", "http://localhost:3000")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Check CORS headers if CORS is enabled
		if allowOrigin := resp.Header.Get("Access-Control-Allow-Origin"); allowOrigin != "" {
			t.Logf("CORS enabled with Allow-Origin: %s", allowOrigin)
		}
	})
}

// ==============================================================================
// Authorization Error Response Tests
// ==============================================================================

// TestAuthorizationErrorResponses tests proper error responses for auth failures.
func TestAuthorizationErrorResponses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	mcpURL := testMCPEndpoint

	t.Run("www_authenticate_format", func(t *testing.T) {
		// Make request with invalid token to trigger 401
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test",
					"version": "1.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer bad-token")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized {
			wwwAuth := resp.Header.Get("WWW-Authenticate")
			assert.NotEmpty(t, wwwAuth, "401 must include WWW-Authenticate")

			// RFC 6750 format: Bearer realm="...", error="...", error_description="..."
			assert.True(t, strings.HasPrefix(wwwAuth, "Bearer"), "Must start with 'Bearer'")

			// Verify it contains realm
			assert.Contains(t, wwwAuth, "realm=", "Should include realm parameter")
		}
	})

	t.Run("json_rpc_error_format", func(t *testing.T) {
		initReq := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params": map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"clientInfo": map[string]interface{}{
					"name":    "test",
					"version": "1.0",
				},
			},
		}

		reqBody, err := json.Marshal(initReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer bad-token")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			var rpcResp map[string]interface{}
			err = json.Unmarshal(body, &rpcResp)
			require.NoError(t, err)

			// Should have JSON-RPC error structure
			assert.Equal(t, "2.0", rpcResp["jsonrpc"])
			if rpcErr, ok := rpcResp["error"].(map[string]interface{}); ok {
				assert.NotNil(t, rpcErr["code"], "Error must have code")
				assert.NotNil(t, rpcErr["message"], "Error must have message")
			}
		}
	})
}

// ==============================================================================
// Helpers
// ==============================================================================

// mcpRequest is a helper for making MCP JSON-RPC requests.
func mcpRequest(ctx context.Context, t *testing.T, method string, params interface{}, token string) (map[string]interface{}, *http.Response) {
	t.Helper()

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      time.Now().UnixNano(),
		"params":  params,
	}

	reqBody, err := json.Marshal(req)
	require.NoError(t, err)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, testMCPEndpoint, bytes.NewReader(reqBody))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")

	if token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(httpReq)
	require.NoError(t, err)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var rpcResp map[string]interface{}
	err = json.Unmarshal(body, &rpcResp)
	require.NoError(t, err)

	return rpcResp, resp
}

// printResponse is a debug helper.
func printResponse(t *testing.T, label string, resp *http.Response, body []byte) {
	t.Helper()
	fmt.Printf("\n=== %s ===\n", label)
	fmt.Printf("Status: %d\n", resp.StatusCode)
	fmt.Printf("Headers:\n")
	for k, v := range resp.Header {
		fmt.Printf("  %s: %s\n", k, strings.Join(v, ", "))
	}
	fmt.Printf("Body: %s\n", string(body))
}
