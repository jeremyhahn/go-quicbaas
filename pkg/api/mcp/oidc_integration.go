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
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// OIDCProviderTokenValidationError represents an error during OIDC token validation.
type OIDCProviderTokenValidationError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *OIDCProviderTokenValidationError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap returns the underlying error.
func (e *OIDCProviderTokenValidationError) Unwrap() error {
	return e.Cause
}

// SetOIDCProvider sets the IAM OIDC provider for token validation.
// When set, the MCP server will use the OIDC provider for validating tokens,
// enabling support for federated identities and IAM-based authentication.
//
// The OIDCProvider takes precedence over AuthManager for token validation,
// but AuthManager is used as fallback if OIDCProvider validation fails.
func (s *Server) SetOIDCProvider(provider interfaces.OIDCProvider) {
	s.authChecker.SetOIDCProvider(provider)

	// If MCPAuth is not configured, derive endpoints from OIDC provider
	if s.config.MCPAuth != nil && provider != nil {
		metadata := provider.GetOAuthASMetadata()
		if metadata != nil {
			s.deriveAuthConfigFromOIDC(metadata)
		}
	}
}

// deriveAuthConfigFromOIDC populates MCPAuthConfig fields from OIDC metadata
// if they are not already set. This allows automatic configuration when using
// IAM's built-in OIDC provider.
func (s *Server) deriveAuthConfigFromOIDC(metadata interfaces.OAuthASMetadata) {
	if s.config.MCPAuth == nil {
		return
	}

	// Only derive values that are not already set
	if s.config.MCPAuth.Issuer == "" {
		s.config.MCPAuth.Issuer = metadata.GetIssuer()
	}
	if s.config.MCPAuth.AuthorizationEndpoint == "" {
		s.config.MCPAuth.AuthorizationEndpoint = metadata.GetAuthorizationEndpoint()
	}
	if s.config.MCPAuth.TokenEndpoint == "" {
		s.config.MCPAuth.TokenEndpoint = metadata.GetTokenEndpoint()
	}
	if s.config.MCPAuth.JwksURI == "" {
		s.config.MCPAuth.JwksURI = metadata.GetJWKSURI()
	}
	if s.config.MCPAuth.RegistrationEndpoint == "" {
		s.config.MCPAuth.RegistrationEndpoint = metadata.GetRegistrationEndpoint()
	}
	if s.config.MCPAuth.RevocationEndpoint == "" {
		s.config.MCPAuth.RevocationEndpoint = metadata.GetRevocationEndpoint()
	}
	if len(s.config.MCPAuth.ScopesSupported) == 0 {
		s.config.MCPAuth.ScopesSupported = metadata.GetScopesSupported()
	}
}

// GetOIDCProvider returns the configured OIDC provider, or nil if not set.
func (s *Server) GetOIDCProvider() interfaces.OIDCProvider {
	if s.authChecker == nil {
		return nil
	}
	return s.authChecker.GetOIDCProvider()
}

// SetOIDCProvider sets the OIDC provider for token validation.
// When set, ValidateWithOIDCProvider will be used before falling back to AuthManager.
func (ac *AuthorizationChecker) SetOIDCProvider(provider interfaces.OIDCProvider) {
	ac.oidcProvider = provider
}

// GetOIDCProvider returns the configured OIDC provider.
func (ac *AuthorizationChecker) GetOIDCProvider() interfaces.OIDCProvider {
	return ac.oidcProvider
}

// ValidateWithOIDCProvider validates a token using the configured OIDC provider.
// Returns the claims if valid, or an error if validation fails.
func (ac *AuthorizationChecker) ValidateWithOIDCProvider(ctx context.Context, token string) (interfaces.OIDCClaims, error) {
	if ac.oidcProvider == nil {
		return nil, &OIDCProviderTokenValidationError{Message: "OIDC provider not configured"}
	}

	claims, err := ac.oidcProvider.ValidateAccessToken(ctx, token)
	if err != nil {
		return nil, &OIDCProviderTokenValidationError{
			Message: "token validation failed",
			Cause:   err,
		}
	}

	return claims, nil
}

// validateTokenAudienceFromClaims validates that the claims contain the MCP resource URI
// in the audience. This is an alternative to parsing the JWT again when we already
// have the claims from OIDC provider validation.
func (ac *AuthorizationChecker) validateTokenAudienceFromClaims(claims interfaces.OIDCClaims) *AuthorizationError {
	// Skip audience validation if not configured
	if ac.mcpAuthConfig == nil || !ac.mcpAuthConfig.Enabled || ac.mcpAuthConfig.ResourceURI == "" {
		return nil
	}

	audience := claims.GetAudience()
	if len(audience) == 0 {
		return &AuthorizationError{
			Type:    AuthErrorInvalidToken,
			Message: "token audience does not include MCP resource URI",
		}
	}

	// Check if MCP resource URI is in the audience
	resourceURI := ac.mcpAuthConfig.ResourceURI
	for _, aud := range audience {
		if aud == resourceURI {
			return nil
		}
	}

	return &AuthorizationError{
		Type:    AuthErrorInvalidToken,
		Message: "token audience does not include MCP resource URI",
	}
}

// MCPAuthConfigFromOIDCProvider creates an MCPAuthConfig populated from an OIDC provider's metadata.
// This is useful for automatically configuring MCP authorization when using IAM's built-in OIDC.
//
// Parameters:
//   - provider: The OIDC provider to get metadata from
//   - resourceURI: The MCP server's resource URI for token audience validation
//
// Returns an MCPAuthConfig with enabled=true and fields populated from the OIDC provider.
func MCPAuthConfigFromOIDCProvider(provider interfaces.OIDCProvider, resourceURI string) *MCPAuthConfig {
	if provider == nil {
		return nil
	}

	metadata := provider.GetOAuthASMetadata()
	if metadata == nil {
		return nil
	}

	issuer := metadata.GetIssuer()
	return &MCPAuthConfig{
		Enabled:               true,
		ResourceURI:           resourceURI,
		AuthorizationServers:  []string{issuer},
		Issuer:                issuer,
		AuthorizationEndpoint: metadata.GetAuthorizationEndpoint(),
		TokenEndpoint:         metadata.GetTokenEndpoint(),
		JwksURI:               metadata.GetJWKSURI(),
		RegistrationEndpoint:  metadata.GetRegistrationEndpoint(),
		RevocationEndpoint:    metadata.GetRevocationEndpoint(),
		ScopesSupported:       metadata.GetScopesSupported(),
	}
}

// ExtractScopesFromClaims extracts individual scopes from an OIDCClaims object.
// Scopes are typically stored as a space-separated string in the "scope" claim.
func ExtractScopesFromClaims(claims interfaces.OIDCClaims) []string {
	scopeStr := claims.GetScope()
	if scopeStr == "" {
		return nil
	}
	return strings.Fields(scopeStr)
}

// HasScope checks if the claims include a specific scope.
func HasScope(claims interfaces.OIDCClaims, scope string) bool {
	scopes := ExtractScopesFromClaims(claims)
	for _, s := range scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// HasAnyScope checks if the claims include any of the specified scopes.
func HasAnyScope(claims interfaces.OIDCClaims, scopes ...string) bool {
	claimScopes := ExtractScopesFromClaims(claims)
	scopeSet := make(map[string]bool, len(claimScopes))
	for _, s := range claimScopes {
		scopeSet[s] = true
	}

	for _, scope := range scopes {
		if scopeSet[scope] {
			return true
		}
	}
	return false
}

// HasAllScopes checks if the claims include all of the specified scopes.
func HasAllScopes(claims interfaces.OIDCClaims, scopes ...string) bool {
	claimScopes := ExtractScopesFromClaims(claims)
	scopeSet := make(map[string]bool, len(claimScopes))
	for _, s := range claimScopes {
		scopeSet[s] = true
	}

	for _, scope := range scopes {
		if !scopeSet[scope] {
			return false
		}
	}
	return true
}
