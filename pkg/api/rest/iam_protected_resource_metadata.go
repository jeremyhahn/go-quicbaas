//go:build rest

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

package rest

import (
	"context"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ProtectedResourceMetadata represents OAuth 2.0 Protected Resource Metadata
// per RFC 9728. MCP clients fetch it from /.well-known/oauth-protected-resource
// to discover which authorization server(s) to use and what resource
// identifier to bind into the token audience.
type ProtectedResourceMetadata struct {
	// Resource is the protected resource's identifier (REQUIRED). This is
	// the MCP server's resource URI that clients must include in the token
	// audience.
	Resource string `json:"resource"`

	// AuthorizationServers is a list of authorization server issuer URIs
	// (REQUIRED). Clients can obtain access tokens from any of these
	// servers.
	AuthorizationServers []string `json:"authorization_servers"`

	// ScopesSupported is a list of scope values for this resource
	// (OPTIONAL).
	ScopesSupported []string `json:"scopes_supported,omitempty"`

	// BearerMethodsSupported is a list of bearer token methods (OPTIONAL).
	// Per RFC 6750, this indicates how tokens can be provided. Only
	// "header" is supported.
	BearerMethodsSupported []string `json:"bearer_methods_supported,omitempty"`

	// ResourceName is a human-readable name for this protected resource
	// (OPTIONAL).
	ResourceName string `json:"resource_name,omitempty"`

	// ResourceDocumentation is a URL to API documentation (OPTIONAL).
	ResourceDocumentation string `json:"resource_documentation,omitempty"`
}

// defaultProtectedResourceScopes returns the default scopes advertised in
// the protected resource metadata: the base MCP scopes plus the
// orchestrator-specific scopes GetOAuthASMetadata (iam_oauth_metadata.go)
// already advertises for the authorization server, so an MCP client sees
// the identical scope vocabulary from both documents.
func defaultProtectedResourceScopes() []string {
	return []string{
		"openid",
		"mcp:tools",
		"mcp:resources",
		"mcp:admin",
		"agent:runtime:spawn",
		"agent:runtime:prompt",
		"agent:runtime:status",
	}
}

// GetProtectedResourceMetadata handles GET /.well-known/oauth-protected-resource
// per RFC 9728. It is the MCP client-auth discovery counterpart to
// GetOAuthASMetadata (RFC 8414, iam_oauth_metadata.go): where that endpoint
// describes the AUTHORIZATION server, this one describes this node as the
// PROTECTED RESOURCE (the MCP server) so a client knows which authorization
// server(s) to use and what resource identifier to bind into the token
// audience.
//
// Sourced from Config.MCPAuthorization (threaded from
// s.config.Server.MCP.Authorization by pkg/server/lifecycle_rest.go): disabled
// (the default posture, mirroring every other MCP-auth surface) answers 404,
// matching the pre-unified-server behavior of the retired pkg/api/mcp
// listener this endpoint restores. AuthorizationServers defaults to the
// built-in IAM OIDC provider's issuer when the config list is empty, exactly
// as MCPAuthorizationConfig.AuthorizationServers documents.
func (h *IAMHandler) GetProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	cfg := h.server.config
	if cfg == nil || !cfg.MCPAuthorization.Enabled {
		RespondIAMError(w, r, http.StatusNotFound,
			NewIAMAPIError(ErrNotFound, "protected resource metadata not configured"))
		return
	}
	mcpAuth := cfg.MCPAuthorization

	authServers := mcpAuth.AuthorizationServers
	if len(authServers) == 0 {
		issuer, ok := h.resolveIssuer(r.Context(), w, r)
		if !ok {
			return
		}
		authServers = []string{issuer}
	}

	metadata := &ProtectedResourceMetadata{
		Resource:               mcpAuth.ResourceURI,
		AuthorizationServers:   authServers,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "QuicBaaS MCP Server",
		ResourceDocumentation:  mcpAuth.ResourceDocumentation,
		ScopesSupported:        defaultProtectedResourceScopes(),
	}

	RespondJSON(w, r, http.StatusOK, metadata)
}

// resolveIssuer returns the built-in IAM OIDC provider's issuer, the same
// fallback source of truth GetOAuthASMetadata reads from
// (iam_oauth_metadata.go), for the case where the operator has not
// overridden Server.MCP.Authorization.AuthorizationServers explicitly. On
// failure it writes the error response itself (mirroring GetOAuthASMetadata)
// and returns ok=false so the caller returns without a second write.
func (h *IAMHandler) resolveIssuer(ctx context.Context, w http.ResponseWriter, r *http.Request) (issuer string, ok bool) {
	oidcProvider, isProvider := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !isProvider {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
		return "", false
	}
	provider, err := oidcProvider.GetOIDCProvider(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return "", false
	}
	if provider == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not initialized"))
		return "", false
	}
	return provider.Issuer(), true
}
