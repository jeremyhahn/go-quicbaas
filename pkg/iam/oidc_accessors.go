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

package iam

import "github.com/jeremyhahn/go-quicbaas/pkg/interfaces"

// Getter methods for Claims to implement interfaces.OIDCClaims

// GetSubject returns the subject (user ID) from the token.
func (c *Claims) GetSubject() string {
	return c.Subject
}

// GetIssuer returns the token issuer.
func (c *Claims) GetIssuer() string {
	return c.Issuer
}

// GetAudience returns the intended audience(s) for the token.
func (c *Claims) GetAudience() []string {
	return c.Audience
}

// GetTenantID returns the tenant context.
func (c *Claims) GetTenantID() string {
	return c.TenantID
}

// GetRoles returns the user's roles.
func (c *Claims) GetRoles() []string {
	return c.Roles
}

// GetGroups returns the user's groups.
func (c *Claims) GetGroups() []string {
	return c.Groups
}

// GetEmail returns the user's email.
func (c *Claims) GetEmail() string {
	return c.Email
}

// GetName returns the user's name.
func (c *Claims) GetName() string {
	return c.Name
}

// GetScope returns the space-separated list of scopes.
func (c *Claims) GetScope() string {
	return c.Scope
}

// GetClientID returns the OAuth client ID.
func (c *Claims) GetClientID() string {
	return c.ClientID
}

// IsMFAVerified returns true if MFA was completed.
func (c *Claims) IsMFAVerified() bool {
	return c.MFAVerified
}

// Getter methods for OAuthASMetadata to implement interfaces.OAuthASMetadata

// GetIssuer returns the authorization server's issuer identifier.
func (m *OAuthASMetadata) GetIssuer() string {
	return m.Issuer
}

// GetAuthorizationEndpoint returns the URL of the authorization endpoint.
func (m *OAuthASMetadata) GetAuthorizationEndpoint() string {
	return m.AuthorizationEndpoint
}

// GetTokenEndpoint returns the URL of the token endpoint.
func (m *OAuthASMetadata) GetTokenEndpoint() string {
	return m.TokenEndpoint
}

// GetJWKSURI returns the URL of the JWK Set document.
func (m *OAuthASMetadata) GetJWKSURI() string {
	return m.JWKSURI
}

// GetRegistrationEndpoint returns the URL of the Dynamic Client Registration endpoint.
func (m *OAuthASMetadata) GetRegistrationEndpoint() string {
	return m.RegistrationEndpoint
}

// GetRevocationEndpoint returns the URL of the token revocation endpoint.
func (m *OAuthASMetadata) GetRevocationEndpoint() string {
	return m.RevocationEndpoint
}

// GetResponseTypesSupported returns the list of OAuth 2.0 response_type values.
func (m *OAuthASMetadata) GetResponseTypesSupported() []string {
	return m.ResponseTypesSupported
}

// GetGrantTypesSupported returns the list of OAuth 2.0 grant_type values.
func (m *OAuthASMetadata) GetGrantTypesSupported() []string {
	return m.GrantTypesSupported
}

// GetScopesSupported returns the list of OAuth 2.0 scope values.
func (m *OAuthASMetadata) GetScopesSupported() []string {
	return m.ScopesSupported
}

// GetCodeChallengeMethodsSupported returns the list of PKCE code challenge methods.
func (m *OAuthASMetadata) GetCodeChallengeMethodsSupported() []string {
	return m.CodeChallengeMethodsSupported
}

// IsResourceIndicatorsSupported returns true if Resource Indicators (RFC 8707) is supported.
func (m *OAuthASMetadata) IsResourceIndicatorsSupported() bool {
	return m.ResourceIndicatorsSupported
}

// IsAuthorizationResponseIssParameterSupported returns true if authorization
// responses carry the iss parameter (RFC 9207).
func (m *OAuthASMetadata) IsAuthorizationResponseIssParameterSupported() bool {
	return m.AuthorizationResponseIssParameterSupported
}

// Compile-time interface compliance checks.
var (
	_ interfaces.OIDCClaims      = (*Claims)(nil)
	_ interfaces.OAuthASMetadata = (*OAuthASMetadata)(nil)
)
