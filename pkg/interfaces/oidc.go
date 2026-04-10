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

package interfaces

import "context"

// OIDCClaims represents the claims extracted from an OIDC access token.
// This interface abstracts the concrete iam.Claims type.
type OIDCClaims interface {
	// GetSubject returns the subject (user ID) from the token.
	GetSubject() string

	// GetIssuer returns the token issuer.
	GetIssuer() string

	// GetAudience returns the intended audience(s) for the token.
	GetAudience() []string

	// GetTenantID returns the tenant context.
	GetTenantID() string

	// GetRoles returns the user's roles.
	GetRoles() []string

	// GetGroups returns the user's groups.
	GetGroups() []string

	// GetEmail returns the user's email.
	GetEmail() string

	// GetName returns the user's name.
	GetName() string

	// GetScope returns the space-separated list of scopes.
	GetScope() string

	// GetClientID returns the OAuth client ID.
	GetClientID() string

	// IsMFAVerified returns true if MFA was completed.
	IsMFAVerified() bool
}

// OIDCProvider defines the interface for OIDC token validation.
// This allows MCP and other services to validate tokens without
// direct dependency on the IAM package.
type OIDCProvider interface {
	// ValidateAccessToken validates an access token and returns its claims.
	// Returns an error if the token is invalid, expired, or revoked.
	ValidateAccessToken(ctx context.Context, tokenString string) (OIDCClaims, error)

	// GetConfiguration returns the OIDC discovery configuration as a map.
	GetConfiguration() map[string]interface{}

	// GetOAuthASMetadata returns the OAuth 2.0 Authorization Server Metadata.
	// Returns nil if not supported.
	GetOAuthASMetadata() OAuthASMetadata
}

// OAuthASMetadata represents OAuth 2.0 Authorization Server Metadata per RFC 8414.
type OAuthASMetadata interface {
	// GetIssuer returns the authorization server's issuer identifier.
	GetIssuer() string

	// GetAuthorizationEndpoint returns the URL of the authorization endpoint.
	GetAuthorizationEndpoint() string

	// GetTokenEndpoint returns the URL of the token endpoint.
	GetTokenEndpoint() string

	// GetJWKSURI returns the URL of the JWK Set document.
	GetJWKSURI() string

	// GetRegistrationEndpoint returns the URL of the Dynamic Client Registration endpoint.
	GetRegistrationEndpoint() string

	// GetRevocationEndpoint returns the URL of the token revocation endpoint.
	GetRevocationEndpoint() string

	// GetResponseTypesSupported returns the list of OAuth 2.0 response_type values.
	GetResponseTypesSupported() []string

	// GetGrantTypesSupported returns the list of OAuth 2.0 grant_type values.
	GetGrantTypesSupported() []string

	// GetScopesSupported returns the list of OAuth 2.0 scope values.
	GetScopesSupported() []string

	// GetCodeChallengeMethodsSupported returns the list of PKCE code challenge methods.
	GetCodeChallengeMethodsSupported() []string

	// IsResourceIndicatorsSupported returns true if Resource Indicators (RFC 8707) is supported.
	IsResourceIndicatorsSupported() bool
}
