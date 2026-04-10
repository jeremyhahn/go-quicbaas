//go:build mcp

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

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestOAuthASMetadataNotConfiguredError(t *testing.T) {
	t.Run("error message", func(t *testing.T) {
		err := &OAuthASMetadataNotConfiguredError{}
		assert.Equal(t, "OAuth authorization server metadata not configured", err.Error())
	})
}

func TestDefaultOAuthASMetadata(t *testing.T) {
	t.Run("returns correct default values", func(t *testing.T) {
		metadata := DefaultOAuthASMetadata()

		// Verify response types
		assert.Equal(t, []string{"code"}, metadata.ResponseTypesSupported)

		// Verify grant types
		expectedGrantTypes := []string{"authorization_code", "client_credentials", "refresh_token"}
		assert.Equal(t, expectedGrantTypes, metadata.GrantTypesSupported)

		// Verify code challenge methods
		assert.Equal(t, []string{"S256"}, metadata.CodeChallengeMethodsSupported)

		// Verify token endpoint auth methods
		expectedAuthMethods := []string{"client_secret_basic", "client_secret_post"}
		assert.Equal(t, expectedAuthMethods, metadata.TokenEndpointAuthMethodsSupported)

		// Verify scopes
		expectedScopes := []string{
			"openid",
			"profile",
			"email",
			"offline_access",
			"mcp:tools",
			"mcp:resources",
			"mcp:admin",
			"orchestrator:spawn",
			"orchestrator:prompt",
			"orchestrator:task",
			"orchestrator:broadcast",
			"orchestrator:admin",
			"orchestrator:read",
		}
		assert.Equal(t, expectedScopes, metadata.ScopesSupported)

		// Verify resource indicators support
		assert.True(t, metadata.ResourceIndicatorsSupported)
	})
}

func TestHandleOAuthASMetadata(t *testing.T) {
	t.Run("returns metadata when configured", func(t *testing.T) {
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled:               true,
			Issuer:                "https://auth.quicbaas.example.com",
			AuthorizationEndpoint: "https://auth.quicbaas.example.com/authorize",
			TokenEndpoint:         "https://auth.quicbaas.example.com/token",
			JwksURI:               "https://auth.quicbaas.example.com/.well-known/jwks.json",
			ResourceURI:           "https://quicbaas.example.com/mcp",
			AuthorizationServers:  []string{"https://auth.quicbaas.example.com"},
		})

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var metadata OAuthASMetadata
		err := json.NewDecoder(w.Body).Decode(&metadata)
		require.NoError(t, err)

		// Verify required fields
		assert.Equal(t, "https://auth.quicbaas.example.com", metadata.Issuer)
		assert.Equal(t, "https://auth.quicbaas.example.com/authorize", metadata.AuthorizationEndpoint)
		assert.Equal(t, "https://auth.quicbaas.example.com/token", metadata.TokenEndpoint)
		assert.Equal(t, "https://auth.quicbaas.example.com/.well-known/jwks.json", metadata.JwksURI)

		// Verify default values are applied
		assert.Equal(t, []string{"code"}, metadata.ResponseTypesSupported)
		assert.Contains(t, metadata.GrantTypesSupported, "authorization_code")
		assert.Contains(t, metadata.GrantTypesSupported, "client_credentials")
		assert.Contains(t, metadata.GrantTypesSupported, "refresh_token")
		assert.Equal(t, []string{"S256"}, metadata.CodeChallengeMethodsSupported)
		assert.True(t, metadata.ResourceIndicatorsSupported)
	})

	t.Run("returns 404 when MCPAuth is nil", func(t *testing.T) {
		server := createOAuthTestServer(t, nil)

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "OAuth authorization server metadata not configured")
	})

	t.Run("returns 404 when MCPAuth is disabled", func(t *testing.T) {
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled: false,
			Issuer:  "https://auth.quicbaas.example.com",
		})

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "OAuth authorization server metadata not configured")
	})

	t.Run("returns 404 when Issuer is empty", func(t *testing.T) {
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled: true,
			Issuer:  "",
		})

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "OAuth authorization server metadata not configured")
	})

	t.Run("returns 405 for non-GET requests", func(t *testing.T) {
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled: true,
			Issuer:  "https://auth.quicbaas.example.com",
		})

		methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
		for _, method := range methods {
			req := httptest.NewRequest(method, "/.well-known/oauth-authorization-server", nil)
			w := httptest.NewRecorder()

			server.handleOAuthASMetadata(w, req)

			assert.Equal(t, http.StatusMethodNotAllowed, w.Code, "Expected 405 for method %s", method)
		}
	})

	t.Run("includes optional endpoints when configured", func(t *testing.T) {
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled:               true,
			Issuer:                "https://auth.quicbaas.example.com",
			AuthorizationEndpoint: "https://auth.quicbaas.example.com/authorize",
			TokenEndpoint:         "https://auth.quicbaas.example.com/token",
			JwksURI:               "https://auth.quicbaas.example.com/.well-known/jwks.json",
			RegistrationEndpoint:  "https://auth.quicbaas.example.com/register",
			RevocationEndpoint:    "https://auth.quicbaas.example.com/revoke",
		})

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var metadata OAuthASMetadata
		err := json.NewDecoder(w.Body).Decode(&metadata)
		require.NoError(t, err)

		assert.Equal(t, "https://auth.quicbaas.example.com/register", metadata.RegistrationEndpoint)
		assert.Equal(t, "https://auth.quicbaas.example.com/revoke", metadata.RevocationEndpoint)
	})

	t.Run("omits optional endpoints when not configured", func(t *testing.T) {
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled:               true,
			Issuer:                "https://auth.quicbaas.example.com",
			AuthorizationEndpoint: "https://auth.quicbaas.example.com/authorize",
			TokenEndpoint:         "https://auth.quicbaas.example.com/token",
			JwksURI:               "https://auth.quicbaas.example.com/.well-known/jwks.json",
		})

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var metadata OAuthASMetadata
		err := json.NewDecoder(w.Body).Decode(&metadata)
		require.NoError(t, err)

		assert.Empty(t, metadata.RegistrationEndpoint)
		assert.Empty(t, metadata.RevocationEndpoint)
	})

	t.Run("uses custom scopes when configured", func(t *testing.T) {
		customScopes := []string{"custom:scope1", "custom:scope2"}
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled:               true,
			Issuer:                "https://auth.quicbaas.example.com",
			AuthorizationEndpoint: "https://auth.quicbaas.example.com/authorize",
			TokenEndpoint:         "https://auth.quicbaas.example.com/token",
			JwksURI:               "https://auth.quicbaas.example.com/.well-known/jwks.json",
			ScopesSupported:       customScopes,
		})

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var metadata OAuthASMetadata
		err := json.NewDecoder(w.Body).Decode(&metadata)
		require.NoError(t, err)

		assert.Equal(t, customScopes, metadata.ScopesSupported)
	})

	t.Run("uses default scopes when not configured", func(t *testing.T) {
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled:               true,
			Issuer:                "https://auth.quicbaas.example.com",
			AuthorizationEndpoint: "https://auth.quicbaas.example.com/authorize",
			TokenEndpoint:         "https://auth.quicbaas.example.com/token",
			JwksURI:               "https://auth.quicbaas.example.com/.well-known/jwks.json",
			ScopesSupported:       nil, // Not configured
		})

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var metadata OAuthASMetadata
		err := json.NewDecoder(w.Body).Decode(&metadata)
		require.NoError(t, err)

		// Should have default scopes
		expectedScopes := []string{
			"openid",
			"profile",
			"email",
			"offline_access",
			"mcp:tools",
			"mcp:resources",
			"mcp:admin",
			"orchestrator:spawn",
			"orchestrator:prompt",
			"orchestrator:task",
			"orchestrator:broadcast",
			"orchestrator:admin",
			"orchestrator:read",
		}
		assert.Equal(t, expectedScopes, metadata.ScopesSupported)
	})
}

func TestOAuthASMetadataJSONStructure(t *testing.T) {
	t.Run("all required fields present in JSON", func(t *testing.T) {
		server := createOAuthTestServer(t, &MCPAuthConfig{
			Enabled:               true,
			Issuer:                "https://auth.quicbaas.example.com",
			AuthorizationEndpoint: "https://auth.quicbaas.example.com/authorize",
			TokenEndpoint:         "https://auth.quicbaas.example.com/token",
			JwksURI:               "https://auth.quicbaas.example.com/.well-known/jwks.json",
		})

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		server.handleOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var rawJSON map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&rawJSON)
		require.NoError(t, err)

		// Check all required RFC 8414 fields
		assert.Contains(t, rawJSON, "issuer")
		assert.Contains(t, rawJSON, "authorization_endpoint")
		assert.Contains(t, rawJSON, "token_endpoint")
		assert.Contains(t, rawJSON, "jwks_uri")
		assert.Contains(t, rawJSON, "response_types_supported")
		assert.Contains(t, rawJSON, "grant_types_supported")
		assert.Contains(t, rawJSON, "code_challenge_methods_supported")
		assert.Contains(t, rawJSON, "token_endpoint_auth_methods_supported")
		assert.Contains(t, rawJSON, "scopes_supported")
		assert.Contains(t, rawJSON, "resource_indicators_supported")
	})

	t.Run("JSON field names match RFC 8414 spec", func(t *testing.T) {
		metadata := &OAuthASMetadata{
			Issuer:                            "https://auth.example.com",
			AuthorizationEndpoint:             "https://auth.example.com/authorize",
			TokenEndpoint:                     "https://auth.example.com/token",
			RegistrationEndpoint:              "https://auth.example.com/register",
			RevocationEndpoint:                "https://auth.example.com/revoke",
			JwksURI:                           "https://auth.example.com/.well-known/jwks.json",
			ResponseTypesSupported:            []string{"code"},
			GrantTypesSupported:               []string{"authorization_code"},
			CodeChallengeMethodsSupported:     []string{"S256"},
			TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
			ScopesSupported:                   []string{"openid"},
			ResourceIndicatorsSupported:       true,
		}

		jsonBytes, err := json.Marshal(metadata)
		require.NoError(t, err)

		var rawJSON map[string]interface{}
		err = json.Unmarshal(jsonBytes, &rawJSON)
		require.NoError(t, err)

		// Verify exact field names per RFC 8414
		assert.Equal(t, "https://auth.example.com", rawJSON["issuer"])
		assert.Equal(t, "https://auth.example.com/authorize", rawJSON["authorization_endpoint"])
		assert.Equal(t, "https://auth.example.com/token", rawJSON["token_endpoint"])
		assert.Equal(t, "https://auth.example.com/register", rawJSON["registration_endpoint"])
		assert.Equal(t, "https://auth.example.com/revoke", rawJSON["revocation_endpoint"])
		assert.Equal(t, "https://auth.example.com/.well-known/jwks.json", rawJSON["jwks_uri"])
	})
}

func TestOAuthASMetadataIntegration(t *testing.T) {
	t.Run("endpoint registered in HTTP server", func(t *testing.T) {
		mockService := new(MockQuicBaaS)
		log, err := logger.New(logger.LogConfig{
			Level:  "error",
			Format: "json",
			Output: "stdout",
		})
		require.NoError(t, err)

		config := &Config{
			ServerName:     "Test MCP Server",
			ServerVersion:  "1.0.0-test",
			Transport:      "http",
			HTTPAddr:       "127.0.0.1:0",
			RequestTimeout: 5 * time.Second,
			MCPAuth: &MCPAuthConfig{
				Enabled:               true,
				Issuer:                "https://auth.quicbaas.example.com",
				AuthorizationEndpoint: "https://auth.quicbaas.example.com/authorize",
				TokenEndpoint:         "https://auth.quicbaas.example.com/token",
				JwksURI:               "https://auth.quicbaas.example.com/.well-known/jwks.json",
			},
		}

		server := NewServer(mockService, config, log)
		err = server.Start()
		require.NoError(t, err)
		defer server.Stop()

		// Give server time to start
		time.Sleep(50 * time.Millisecond)

		// The endpoint should be registered
		assert.NotNil(t, server.httpServer.Load())
	})
}

// Helper function to create a test server with OAuth config
func createOAuthTestServer(t *testing.T, mcpAuth *MCPAuthConfig) *Server {
	mockService := new(MockQuicBaaS)
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	config := &Config{
		ServerName:            "Test MCP Server",
		ServerVersion:         "1.0.0-test",
		Transport:             "http",
		HTTPAddr:              "127.0.0.1:0",
		RequestTimeout:        5 * time.Second,
		MaxConcurrentRequests: 10,
		MCPAuth:               mcpAuth,
	}

	return NewServer(mockService, config, log)
}
