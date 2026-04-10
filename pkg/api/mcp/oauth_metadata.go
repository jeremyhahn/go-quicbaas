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

// OAuthASMetadata represents RFC 8414 OAuth 2.0 Authorization Server Metadata.
// This metadata document describes the configuration of an OAuth 2.0
// Authorization Server and is served at the /.well-known/oauth-authorization-server endpoint.
type OAuthASMetadata struct {
	// Issuer is the authorization server's issuer identifier (REQUIRED).
	// This is a URL using the https scheme with no query or fragment components.
	Issuer string `json:"issuer"`

	// AuthorizationEndpoint is the URL of the authorization endpoint (REQUIRED for authorization code grant).
	AuthorizationEndpoint string `json:"authorization_endpoint"`

	// TokenEndpoint is the URL of the token endpoint (REQUIRED).
	TokenEndpoint string `json:"token_endpoint"`

	// RegistrationEndpoint is the URL of the dynamic client registration endpoint (OPTIONAL).
	RegistrationEndpoint string `json:"registration_endpoint,omitempty"`

	// RevocationEndpoint is the URL of the token revocation endpoint (OPTIONAL).
	RevocationEndpoint string `json:"revocation_endpoint,omitempty"`

	// JwksURI is the URL of the JSON Web Key Set document (REQUIRED for JWT validation).
	JwksURI string `json:"jwks_uri"`

	// ResponseTypesSupported is a list of OAuth 2.0 response_type values (REQUIRED).
	ResponseTypesSupported []string `json:"response_types_supported"`

	// GrantTypesSupported is a list of OAuth 2.0 grant type values (OPTIONAL).
	// Default is ["authorization_code", "implicit"] per RFC 8414.
	GrantTypesSupported []string `json:"grant_types_supported"`

	// CodeChallengeMethodsSupported is a list of PKCE code challenge methods (OPTIONAL).
	// Values: "plain", "S256"
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`

	// TokenEndpointAuthMethodsSupported is a list of client authentication methods (OPTIONAL).
	// Values: "client_secret_basic", "client_secret_post", "client_secret_jwt", "private_key_jwt", "none"
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`

	// ScopesSupported is a list of OAuth 2.0 scope values (RECOMMENDED).
	ScopesSupported []string `json:"scopes_supported"`

	// ResourceIndicatorsSupported indicates if the authorization server supports
	// resource indicators per RFC 8707 (OPTIONAL).
	ResourceIndicatorsSupported bool `json:"resource_indicators_supported"`
}

// OAuthASMetadataNotConfiguredError indicates the OAuth authorization server metadata is not configured.
type OAuthASMetadataNotConfiguredError struct{}

// Error implements the error interface.
func (e *OAuthASMetadataNotConfiguredError) Error() string {
	return "OAuth authorization server metadata not configured"
}

// DefaultOAuthASMetadata returns default OAuth authorization server metadata values.
// These are the standard values for QuicBaaS MCP server OAuth configuration.
// Includes orchestrator-specific scopes for LLM agent session management.
func DefaultOAuthASMetadata() *OAuthASMetadata {
	return &OAuthASMetadata{
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "client_credentials", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
		ScopesSupported: []string{
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
		},
		ResourceIndicatorsSupported: true,
	}
}

// handleOAuthASMetadata handles GET /.well-known/oauth-authorization-server - RFC 8414.
// This endpoint provides OAuth 2.0 Authorization Server Metadata to clients,
// enabling them to discover the authorization server's capabilities and endpoints.
func (s *Server) handleOAuthASMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if MCP authorization is configured
	if s.config.MCPAuth == nil || !s.config.MCPAuth.Enabled {
		http.Error(w, "OAuth authorization server metadata not configured", http.StatusNotFound)
		return
	}

	// Validate required configuration
	if s.config.MCPAuth.Issuer == "" {
		http.Error(w, "OAuth authorization server metadata not configured", http.StatusNotFound)
		return
	}

	// Build metadata response from config with defaults
	metadata := DefaultOAuthASMetadata()
	metadata.Issuer = s.config.MCPAuth.Issuer
	metadata.AuthorizationEndpoint = s.config.MCPAuth.AuthorizationEndpoint
	metadata.TokenEndpoint = s.config.MCPAuth.TokenEndpoint
	metadata.JwksURI = s.config.MCPAuth.JwksURI

	// Set optional endpoints if configured
	if s.config.MCPAuth.RegistrationEndpoint != "" {
		metadata.RegistrationEndpoint = s.config.MCPAuth.RegistrationEndpoint
	}
	if s.config.MCPAuth.RevocationEndpoint != "" {
		metadata.RevocationEndpoint = s.config.MCPAuth.RevocationEndpoint
	}

	// Override scopes if custom scopes are configured
	if len(s.config.MCPAuth.ScopesSupported) > 0 {
		metadata.ScopesSupported = s.config.MCPAuth.ScopesSupported
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(metadata); err != nil {
		s.logger.WithError(err).Error("Failed to write OAuth authorization server metadata response")
	}
}
