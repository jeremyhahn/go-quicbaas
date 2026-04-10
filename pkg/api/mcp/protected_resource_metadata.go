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
)

// ProtectedResourceMetadata represents OAuth 2.0 Protected Resource Metadata per RFC 9728.
// This is used for the /.well-known/oauth-protected-resource endpoint.
type ProtectedResourceMetadata struct {
	// Resource is the protected resource's identifier (REQUIRED).
	// This is the MCP server's resource URI that clients must include in token audience.
	Resource string `json:"resource"`

	// AuthorizationServers is a list of authorization server issuer URIs (REQUIRED).
	// Clients can obtain access tokens from any of these servers.
	AuthorizationServers []string `json:"authorization_servers"`

	// ScopesSupported is a list of scope values for this resource (OPTIONAL).
	ScopesSupported []string `json:"scopes_supported,omitempty"`

	// BearerMethodsSupported is a list of bearer token methods (OPTIONAL).
	// Per RFC 6750, this indicates how tokens can be provided.
	// Values: "header", "form", "query" (we only support "header")
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`

	// ResourceName is a human-readable name for this protected resource (OPTIONAL).
	ResourceName string `json:"resource_name,omitempty"`

	// ResourceDocumentation is a URL to API documentation (OPTIONAL).
	ResourceDocumentation string `json:"resource_documentation,omitempty"`
}

// MCPAuthConfig contains MCP authorization configuration.
type MCPAuthConfig struct {
	// Enabled indicates whether MCP authorization is enabled.
	Enabled bool

	// ResourceURI is this MCP server's resource identifier.
	// Example: "https://quicbaas.example.com/mcp"
	ResourceURI string

	// AuthorizationServers is a list of authorization server issuer URIs.
	// Example: ["https://quicbaas.example.com"]
	AuthorizationServers []string

	// ResourceDocumentation is a URL to API documentation.
	ResourceDocumentation string

	// Issuer is the authorization server's issuer identifier (RFC 8414).
	// This is used for the OAuth authorization server metadata endpoint.
	// Example: "https://auth.quicbaas.example.com"
	Issuer string

	// AuthorizationEndpoint is the URL of the authorization endpoint.
	// Example: "https://auth.quicbaas.example.com/authorize"
	AuthorizationEndpoint string

	// TokenEndpoint is the URL of the token endpoint.
	// Example: "https://auth.quicbaas.example.com/token"
	TokenEndpoint string

	// JwksURI is the URL of the JSON Web Key Set document.
	// Example: "https://auth.quicbaas.example.com/.well-known/jwks.json"
	JwksURI string

	// RegistrationEndpoint is the URL of the dynamic client registration endpoint (optional).
	// Example: "https://auth.quicbaas.example.com/register"
	RegistrationEndpoint string

	// RevocationEndpoint is the URL of the token revocation endpoint (optional).
	// Example: "https://auth.quicbaas.example.com/revoke"
	RevocationEndpoint string

	// ScopesSupported is a list of OAuth 2.0 scope values supported.
	// If empty, defaults are used.
	ScopesSupported []string
}

// DefaultMCPAuthConfig returns default MCP authorization configuration.
func DefaultMCPAuthConfig() *MCPAuthConfig {
	return &MCPAuthConfig{
		Enabled:               false,
		ResourceURI:           "",
		AuthorizationServers:  nil,
		ResourceDocumentation: "",
		Issuer:                "",
		AuthorizationEndpoint: "",
		TokenEndpoint:         "",
		JwksURI:               "",
		RegistrationEndpoint:  "",
		RevocationEndpoint:    "",
		ScopesSupported:       nil,
	}
}

// defaultProtectedResourceScopes returns the default scopes for the protected resource metadata.
// This includes both base MCP scopes and orchestrator-specific scopes.
func defaultProtectedResourceScopes() []string {
	return []string{
		"openid",
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
}

// handleProtectedResourceMetadata handles GET /.well-known/oauth-protected-resource - RFC 9728.
// This endpoint tells MCP clients which authorization server to use and
// what resource URI to include in token audience claims.
func (s *Server) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if MCP authorization is configured
	if s.config.MCPAuth == nil || !s.config.MCPAuth.Enabled {
		http.Error(w, "Protected resource metadata not configured", http.StatusNotFound)
		return
	}

	// Build metadata response
	metadata := &ProtectedResourceMetadata{
		Resource:               s.config.MCPAuth.ResourceURI,
		AuthorizationServers:   s.config.MCPAuth.AuthorizationServers,
		BearerMethodsSupported: []string{"header"}, // Only header-based per MCP spec
		ResourceName:           "QuicBaaS MCP Server",
		ResourceDocumentation:  s.config.MCPAuth.ResourceDocumentation,
		ScopesSupported:        defaultProtectedResourceScopes(),
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		s.logger.WithError(err).Error("Failed to write protected resource metadata response")
	}
}
