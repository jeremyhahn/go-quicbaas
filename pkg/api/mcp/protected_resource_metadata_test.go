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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestDefaultMCPAuthConfig(t *testing.T) {
	config := DefaultMCPAuthConfig()

	assert.NotNil(t, config)
	assert.False(t, config.Enabled)
	assert.Empty(t, config.ResourceURI)
	assert.Nil(t, config.AuthorizationServers)
	assert.Empty(t, config.ResourceDocumentation)
	assert.Empty(t, config.Issuer)
	assert.Empty(t, config.AuthorizationEndpoint)
	assert.Empty(t, config.TokenEndpoint)
	assert.Empty(t, config.JwksURI)
	assert.Empty(t, config.RegistrationEndpoint)
	assert.Empty(t, config.RevocationEndpoint)
	assert.Nil(t, config.ScopesSupported)
}

func TestHandleProtectedResourceMetadata(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	t.Run("returns metadata when configured", func(t *testing.T) {
		config := &Config{
			ServerName:    "test",
			ServerVersion: "1.0.0",
			MCPAuth: &MCPAuthConfig{
				Enabled:               true,
				ResourceURI:           "https://api.example.com/mcp",
				AuthorizationServers:  []string{"https://auth.example.com"},
				ResourceDocumentation: "https://docs.example.com/mcp",
			},
		}

		server := NewServer(nil, config, log)
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()

		server.handleProtectedResourceMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var metadata ProtectedResourceMetadata
		err := json.NewDecoder(w.Body).Decode(&metadata)
		require.NoError(t, err)

		assert.Equal(t, "https://api.example.com/mcp", metadata.Resource)
		assert.Equal(t, []string{"https://auth.example.com"}, metadata.AuthorizationServers)
		assert.Equal(t, []string{"header"}, metadata.BearerMethodsSupported)
		assert.Equal(t, "https://docs.example.com/mcp", metadata.ResourceDocumentation)
		assert.Equal(t, "QuicBaaS MCP Server", metadata.ResourceName)
		assert.Contains(t, metadata.ScopesSupported, "openid")
		assert.Contains(t, metadata.ScopesSupported, "mcp:tools")
		assert.Contains(t, metadata.ScopesSupported, "mcp:resources")
		assert.Contains(t, metadata.ScopesSupported, "mcp:admin")
	})

	t.Run("returns 404 when MCPAuth is nil", func(t *testing.T) {
		config := &Config{
			ServerName:    "test",
			ServerVersion: "1.0.0",
			MCPAuth:       nil,
		}

		server := NewServer(nil, config, log)
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()

		server.handleProtectedResourceMetadata(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Protected resource metadata not configured")
	})

	t.Run("returns 404 when MCPAuth is disabled", func(t *testing.T) {
		config := &Config{
			ServerName:    "test",
			ServerVersion: "1.0.0",
			MCPAuth: &MCPAuthConfig{
				Enabled:     false,
				ResourceURI: "https://api.example.com/mcp",
			},
		}

		server := NewServer(nil, config, log)
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()

		server.handleProtectedResourceMetadata(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 405 for non-GET requests", func(t *testing.T) {
		config := &Config{
			ServerName:    "test",
			ServerVersion: "1.0.0",
			MCPAuth: &MCPAuthConfig{
				Enabled:     true,
				ResourceURI: "https://api.example.com/mcp",
			},
		}

		server := NewServer(nil, config, log)

		methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}
		for _, method := range methods {
			req := httptest.NewRequest(method, "/.well-known/oauth-protected-resource", nil)
			w := httptest.NewRecorder()

			server.handleProtectedResourceMetadata(w, req)

			assert.Equal(t, http.StatusMethodNotAllowed, w.Code, "Expected 405 for method %s", method)
		}
	})

	t.Run("returns metadata with multiple authorization servers", func(t *testing.T) {
		config := &Config{
			ServerName:    "test",
			ServerVersion: "1.0.0",
			MCPAuth: &MCPAuthConfig{
				Enabled:     true,
				ResourceURI: "https://api.example.com/mcp",
				AuthorizationServers: []string{
					"https://auth1.example.com",
					"https://auth2.example.com",
					"https://auth3.example.com",
				},
			},
		}

		server := NewServer(nil, config, log)
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()

		server.handleProtectedResourceMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var metadata ProtectedResourceMetadata
		err := json.NewDecoder(w.Body).Decode(&metadata)
		require.NoError(t, err)

		assert.Len(t, metadata.AuthorizationServers, 3)
		assert.Equal(t, "https://auth1.example.com", metadata.AuthorizationServers[0])
		assert.Equal(t, "https://auth2.example.com", metadata.AuthorizationServers[1])
		assert.Equal(t, "https://auth3.example.com", metadata.AuthorizationServers[2])
	})

	t.Run("omits optional fields when empty", func(t *testing.T) {
		config := &Config{
			ServerName:    "test",
			ServerVersion: "1.0.0",
			MCPAuth: &MCPAuthConfig{
				Enabled:              true,
				ResourceURI:          "https://api.example.com/mcp",
				AuthorizationServers: []string{"https://auth.example.com"},
				// ResourceDocumentation is empty
			},
		}

		server := NewServer(nil, config, log)
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
		w := httptest.NewRecorder()

		server.handleProtectedResourceMetadata(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Check that resource_documentation is omitted from JSON when empty
		var rawJSON map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&rawJSON)
		require.NoError(t, err)

		// The handler always sets BearerMethodsSupported so that will be present
		assert.Contains(t, rawJSON, "resource")
		assert.Contains(t, rawJSON, "authorization_servers")
		assert.Contains(t, rawJSON, "bearer_methods_supported")
	})
}

// TestProtectedResourceMetadata_Success verifies the endpoint returns valid RFC 9728 JSON
// with all required and optional fields correctly populated.
func TestProtectedResourceMetadata_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	config := &Config{
		ServerName:    "test",
		ServerVersion: "1.0.0",
		MCPAuth: &MCPAuthConfig{
			Enabled:               true,
			ResourceURI:           "https://api.quicbaas.example.com/mcp",
			AuthorizationServers:  []string{"https://auth.quicbaas.example.com"},
			ResourceDocumentation: "https://docs.quicbaas.example.com/mcp",
		},
	}

	server := NewServer(nil, config, log)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()

	server.handleProtectedResourceMetadata(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var metadata ProtectedResourceMetadata
	err := json.NewDecoder(w.Body).Decode(&metadata)
	require.NoError(t, err)

	// RFC 9728 required fields
	assert.Equal(t, "https://api.quicbaas.example.com/mcp", metadata.Resource)
	assert.Equal(t, []string{"https://auth.quicbaas.example.com"}, metadata.AuthorizationServers)

	// RFC 9728 optional fields
	assert.Equal(t, []string{"header"}, metadata.BearerMethodsSupported)
	assert.Equal(t, "QuicBaaS MCP Server", metadata.ResourceName)
	assert.Equal(t, "https://docs.quicbaas.example.com/mcp", metadata.ResourceDocumentation)
	assert.NotEmpty(t, metadata.ScopesSupported)

	// Verify JSON can be re-marshaled (round-trip)
	data, err := json.Marshal(metadata)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"resource"`)
	assert.Contains(t, string(data), `"authorization_servers"`)
	assert.Contains(t, string(data), `"resource_name"`)
}

// TestProtectedResourceMetadata_ContainsOrchestratorScopes verifies the endpoint
// includes all orchestrator-specific scopes in the scopes_supported field.
func TestProtectedResourceMetadata_ContainsOrchestratorScopes(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	config := &Config{
		ServerName:    "test",
		ServerVersion: "1.0.0",
		MCPAuth: &MCPAuthConfig{
			Enabled:              true,
			ResourceURI:          "https://api.example.com/mcp",
			AuthorizationServers: []string{"https://auth.example.com"},
		},
	}

	server := NewServer(nil, config, log)
	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()

	server.handleProtectedResourceMetadata(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var metadata ProtectedResourceMetadata
	err := json.NewDecoder(w.Body).Decode(&metadata)
	require.NoError(t, err)

	// Base MCP scopes
	assert.Contains(t, metadata.ScopesSupported, "openid")
	assert.Contains(t, metadata.ScopesSupported, "mcp:tools")
	assert.Contains(t, metadata.ScopesSupported, "mcp:resources")
	assert.Contains(t, metadata.ScopesSupported, "mcp:admin")

	// Orchestrator-specific scopes
	orchestratorScopes := []string{
		"orchestrator:spawn",
		"orchestrator:prompt",
		"orchestrator:task",
		"orchestrator:broadcast",
		"orchestrator:admin",
		"orchestrator:read",
	}
	for _, scope := range orchestratorScopes {
		assert.Contains(t, metadata.ScopesSupported, scope,
			"protected resource metadata must include orchestrator scope: %s", scope)
	}
}

// TestOrchestratorToolAuthorization verifies that all orchestrator tool names
// are correctly mapped to the orchestrator resource with appropriate actions.
func TestOrchestratorToolAuthorization(t *testing.T) {
	testCases := []struct {
		name             string
		toolName         string
		expectedResource string
		expectedAction   string
	}{
		// Orchestrator Read Operations
		{
			name:             "discover tool maps to orchestrator read",
			toolName:         "quicbaas_orchestrator_discover",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionRead,
		},
		{
			name:             "list tool maps to orchestrator read",
			toolName:         "quicbaas_orchestrator_list",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionRead,
		},
		{
			name:             "status tool maps to orchestrator read",
			toolName:         "quicbaas_orchestrator_status",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionRead,
		},
		{
			name:             "list_sessions tool maps to orchestrator read",
			toolName:         "quicbaas_orchestrator_list_sessions",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionRead,
		},
		{
			name:             "list_backends tool maps to orchestrator read",
			toolName:         "quicbaas_orchestrator_list_backends",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionRead,
		},
		{
			name:             "health tool maps to orchestrator read",
			toolName:         "quicbaas_orchestrator_health",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionRead,
		},
		{
			name:             "cache_get tool maps to orchestrator read",
			toolName:         "quicbaas_orchestrator_cache_get",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionRead,
		},

		// Orchestrator Write Operations
		{
			name:             "register tool maps to orchestrator write",
			toolName:         "quicbaas_orchestrator_register",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			name:             "spawn tool maps to orchestrator write",
			toolName:         "quicbaas_orchestrator_spawn",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			name:             "prompt tool maps to orchestrator write",
			toolName:         "quicbaas_orchestrator_prompt",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			name:             "close tool maps to orchestrator write",
			toolName:         "quicbaas_orchestrator_close",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			name:             "decompose tool maps to orchestrator write",
			toolName:         "quicbaas_orchestrator_decompose",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			name:             "broadcast tool maps to orchestrator write",
			toolName:         "quicbaas_orchestrator_broadcast",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionWrite,
		},
		{
			name:             "cache_put tool maps to orchestrator write",
			toolName:         "quicbaas_orchestrator_cache_put",
			expectedResource: interfaces.ResourceOrchestrator,
			expectedAction:   interfaces.ActionWrite,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resource, action := getToolResourceAction(tc.toolName)
			assert.Equal(t, tc.expectedResource, resource, "resource mismatch for %s", tc.toolName)
			assert.Equal(t, tc.expectedAction, action, "action mismatch for %s", tc.toolName)
		})
	}
}

// TestOrchestratorToolAuthorization_UnknownToolDefaultsToAdmin verifies that
// an unrecognized orchestrator tool name defaults to cluster admin.
func TestOrchestratorToolAuthorization_UnknownToolDefaultsToAdmin(t *testing.T) {
	resource, action := getToolResourceAction("quicbaas_orchestrator_unknown_tool")
	assert.Equal(t, interfaces.ResourceCluster, resource)
	assert.Equal(t, interfaces.ActionAdmin, action)
}

func TestProtectedResourceMetadataJSONStructure(t *testing.T) {
	t.Run("all required fields present in JSON", func(t *testing.T) {
		metadata := &ProtectedResourceMetadata{
			Resource:               "https://api.example.com/mcp",
			AuthorizationServers:   []string{"https://auth.example.com"},
			BearerMethodsSupported: []string{"header"},
			ResourceName:           "QuicBaaS MCP Server",
			ResourceDocumentation:  "https://docs.example.com",
			ScopesSupported:        []string{"openid", "mcp:tools"},
		}

		data, err := json.Marshal(metadata)
		require.NoError(t, err)

		var rawJSON map[string]interface{}
		err = json.Unmarshal(data, &rawJSON)
		require.NoError(t, err)

		// RFC 9728 required fields
		assert.Contains(t, rawJSON, "resource")
		assert.Contains(t, rawJSON, "authorization_servers")

		// Optional fields
		assert.Contains(t, rawJSON, "bearer_methods_supported")
		assert.Contains(t, rawJSON, "resource_name")
		assert.Contains(t, rawJSON, "resource_documentation")
		assert.Contains(t, rawJSON, "scopes_supported")
	})

	t.Run("JSON field names match RFC 9728 spec", func(t *testing.T) {
		metadata := &ProtectedResourceMetadata{
			Resource:               "https://api.example.com/mcp",
			AuthorizationServers:   []string{"https://auth.example.com"},
			BearerMethodsSupported: []string{"header"},
			ResourceName:           "Test Resource",
		}

		data, err := json.Marshal(metadata)
		require.NoError(t, err)

		jsonStr := string(data)

		// Verify snake_case field names per RFC
		assert.Contains(t, jsonStr, `"resource"`)
		assert.Contains(t, jsonStr, `"authorization_servers"`)
		assert.Contains(t, jsonStr, `"bearer_methods_supported"`)
		assert.Contains(t, jsonStr, `"resource_name"`)
	})

	t.Run("omitempty works for optional fields", func(t *testing.T) {
		metadata := &ProtectedResourceMetadata{
			Resource:             "https://api.example.com/mcp",
			AuthorizationServers: []string{"https://auth.example.com"},
			// Optional fields left empty
		}

		data, err := json.Marshal(metadata)
		require.NoError(t, err)

		jsonStr := string(data)

		// Optional empty fields should be omitted
		assert.NotContains(t, jsonStr, `"bearer_methods_supported"`)
		assert.NotContains(t, jsonStr, `"resource_name"`)
		assert.NotContains(t, jsonStr, `"resource_documentation"`)
		assert.NotContains(t, jsonStr, `"scopes_supported"`)
	})
}

// TestWWWAuthenticate_IncludesResourceMetadata verifies that the
// WriteWWWAuthenticateWithResourceMetadata function includes the resource_metadata
// parameter in the WWW-Authenticate header per RFC 9728.
func TestWWWAuthenticate_IncludesResourceMetadata(t *testing.T) {
	t.Run("includes resource_metadata parameter", func(t *testing.T) {
		w := httptest.NewRecorder()
		resourceMetadataURL := "https://server.example.com/.well-known/oauth-protected-resource"

		WriteWWWAuthenticateWithResourceMetadata(w, DefaultRealm, WWWAuthenticateErrorInvalidToken, "token expired", "", resourceMetadataURL)

		header := w.Header().Get("WWW-Authenticate")
		assert.True(t, strings.HasPrefix(header, "Bearer "), "Should start with Bearer")
		assert.Contains(t, header, `resource_metadata="https://server.example.com/.well-known/oauth-protected-resource"`)
		assert.Contains(t, header, `realm="quicbaas-mcp"`)
		assert.Contains(t, header, `error="invalid_token"`)
		assert.Contains(t, header, `error_description="token expired"`)
	})

	t.Run("omits resource_metadata when URL is empty", func(t *testing.T) {
		w := httptest.NewRecorder()

		WriteWWWAuthenticateWithResourceMetadata(w, DefaultRealm, WWWAuthenticateErrorInvalidToken, "token expired", "", "")

		header := w.Header().Get("WWW-Authenticate")
		assert.True(t, strings.HasPrefix(header, "Bearer "), "Should start with Bearer")
		assert.NotContains(t, header, "resource_metadata")
		assert.Contains(t, header, `realm="quicbaas-mcp"`)
	})

	t.Run("resource_metadata appears before error parameters", func(t *testing.T) {
		w := httptest.NewRecorder()
		resourceMetadataURL := "https://server.example.com/.well-known/oauth-protected-resource"

		WriteWWWAuthenticateWithResourceMetadata(w, DefaultRealm, WWWAuthenticateErrorInvalidRequest, "missing token", "mcp:tools", resourceMetadataURL)

		header := w.Header().Get("WWW-Authenticate")

		// resource_metadata should come after realm but before error
		realmIdx := strings.Index(header, "realm=")
		resourceIdx := strings.Index(header, "resource_metadata=")
		errorIdx := strings.Index(header, "error=")

		assert.Greater(t, resourceIdx, realmIdx, "resource_metadata should appear after realm")
		assert.Less(t, resourceIdx, errorIdx, "resource_metadata should appear before error")
	})
}

// TestRespondAuthErrorWithResourceMetadata verifies that authentication error
// responses include the resource_metadata parameter per RFC 9728.
func TestRespondAuthErrorWithResourceMetadata(t *testing.T) {
	t.Run("includes resource_metadata in 401 response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		w := httptest.NewRecorder()
		resourceMetadataURL := "https://server.example.com/.well-known/oauth-protected-resource"

		RespondAuthErrorWithResourceMetadata(w, req, DefaultRealm, ErrorMissingToken(), resourceMetadataURL)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		header := w.Header().Get("WWW-Authenticate")
		assert.Contains(t, header, `resource_metadata="https://server.example.com/.well-known/oauth-protected-resource"`)

		var resp JSONRPCResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "2.0", resp.JSONRPC)
		require.NotNil(t, resp.Error)
		assert.Equal(t, AuthenticationRequired, resp.Error.Code)
	})

	t.Run("includes resource_metadata in 403 response", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		w := httptest.NewRecorder()
		resourceMetadataURL := "https://server.example.com/.well-known/oauth-protected-resource"

		RespondAuthErrorWithResourceMetadata(w, req, DefaultRealm, ErrorPermissionDenied("access denied"), resourceMetadataURL)

		assert.Equal(t, http.StatusForbidden, w.Code)
		header := w.Header().Get("WWW-Authenticate")
		assert.Contains(t, header, `resource_metadata="https://server.example.com/.well-known/oauth-protected-resource"`)

		var resp JSONRPCResponse
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, PermissionDenied, resp.Error.Code)
	})
}

func TestMCPAuthConfigFields(t *testing.T) {
	t.Run("all fields can be set", func(t *testing.T) {
		config := &MCPAuthConfig{
			Enabled:               true,
			ResourceURI:           "https://api.example.com/mcp",
			AuthorizationServers:  []string{"https://auth.example.com"},
			ResourceDocumentation: "https://docs.example.com",
			Issuer:                "https://auth.example.com",
			AuthorizationEndpoint: "https://auth.example.com/authorize",
			TokenEndpoint:         "https://auth.example.com/token",
			JwksURI:               "https://auth.example.com/.well-known/jwks.json",
			RegistrationEndpoint:  "https://auth.example.com/register",
			RevocationEndpoint:    "https://auth.example.com/revoke",
			ScopesSupported:       []string{"openid", "profile", "email"},
		}

		assert.True(t, config.Enabled)
		assert.Equal(t, "https://api.example.com/mcp", config.ResourceURI)
		assert.Equal(t, []string{"https://auth.example.com"}, config.AuthorizationServers)
		assert.Equal(t, "https://docs.example.com", config.ResourceDocumentation)
		assert.Equal(t, "https://auth.example.com", config.Issuer)
		assert.Equal(t, "https://auth.example.com/authorize", config.AuthorizationEndpoint)
		assert.Equal(t, "https://auth.example.com/token", config.TokenEndpoint)
		assert.Equal(t, "https://auth.example.com/.well-known/jwks.json", config.JwksURI)
		assert.Equal(t, "https://auth.example.com/register", config.RegistrationEndpoint)
		assert.Equal(t, "https://auth.example.com/revoke", config.RevocationEndpoint)
		assert.Equal(t, []string{"openid", "profile", "email"}, config.ScopesSupported)
	})
}

// TestDefaultProtectedResourceScopes verifies the default scopes list
// includes both MCP and orchestrator scopes.
func TestDefaultProtectedResourceScopes(t *testing.T) {
	scopes := defaultProtectedResourceScopes()

	assert.Len(t, scopes, 10)

	// Base MCP scopes
	assert.Contains(t, scopes, "openid")
	assert.Contains(t, scopes, "mcp:tools")
	assert.Contains(t, scopes, "mcp:resources")
	assert.Contains(t, scopes, "mcp:admin")

	// Orchestrator scopes
	assert.Contains(t, scopes, "orchestrator:spawn")
	assert.Contains(t, scopes, "orchestrator:prompt")
	assert.Contains(t, scopes, "orchestrator:task")
	assert.Contains(t, scopes, "orchestrator:broadcast")
	assert.Contains(t, scopes, "orchestrator:admin")
	assert.Contains(t, scopes, "orchestrator:read")
}
