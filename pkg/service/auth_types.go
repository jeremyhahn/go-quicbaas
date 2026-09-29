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

package service

import "time"

// =============================================================================
// MFA Types and Constants
// =============================================================================

// MFAType represents the type of multi-factor authentication.
type MFAType int

const (
	// MFATypeTOTP is Time-based One-Time Password.
	MFATypeTOTP MFAType = iota
	// MFATypeWebAuthn is WebAuthn/FIDO2 authentication.
	MFATypeWebAuthn
	// MFATypeSMS is SMS-based verification.
	MFATypeSMS
	// MFATypeEmail is email-based verification.
	MFATypeEmail
	// MFATypeBackupCode is a backup recovery code.
	MFATypeBackupCode
)

// String returns the string representation of the MFA type.
func (t MFAType) String() string {
	switch t {
	case MFATypeTOTP:
		return "TOTP"
	case MFATypeWebAuthn:
		return "WEBAUTHN"
	case MFATypeSMS:
		return "SMS"
	case MFATypeEmail:
		return "EMAIL"
	case MFATypeBackupCode:
		return "BACKUP_CODE"
	default:
		return "UNKNOWN"
	}
}

// MFAChallenge contains MFA verification requirements.
type MFAChallenge struct {
	// ChallengeID is the unique identifier for this MFA challenge.
	ChallengeID string

	// Type is the MFA type (TOTP, WebAuthn, SMS, etc.).
	Type MFAType

	// ExpiresAt is when this challenge expires.
	ExpiresAt time.Time
}

// =============================================================================
// Extended Auth Request/Response Types
// =============================================================================

// ExtendedLoginRequest contains authentication request fields.
// Password-based login is not supported; use WebAuthn or PIV smartcard.
type ExtendedLoginRequest struct {
	// Username is the user's username or email.
	Username string

	// MFACode is the optional MFA code if MFA is enabled.
	MFACode string

	// TenantID is the optional tenant ID for multi-tenant deployments.
	TenantID string
}

// ExtendedLoginResponse contains the authentication tokens with MFA support.
type ExtendedLoginResponse struct {
	// AccessToken is the JWT access token for API authentication.
	AccessToken string

	// RefreshToken is the token used to obtain new access tokens.
	RefreshToken string

	// ExpiresAt is when the access token expires.
	ExpiresAt time.Time

	// TokenType is the type of token (typically "Bearer").
	TokenType string

	// MFARequired indicates if MFA verification is needed.
	MFARequired bool

	// MFAChallenge contains MFA challenge details if MFA is required.
	MFAChallenge *MFAChallenge
}

// ExtendedRefreshTokenResponse contains the new access token with rotation support.
type ExtendedRefreshTokenResponse struct {
	// AccessToken is the new JWT access token.
	AccessToken string

	// ExpiresAt is when the new access token expires.
	ExpiresAt time.Time

	// TokenType is the type of token (typically "Bearer").
	TokenType string

	// RefreshToken is optionally a new refresh token (token rotation).
	RefreshToken string
}

// TokenClaims contains the validated token claims.
type TokenClaims struct {
	// Valid indicates if the token is valid.
	Valid bool

	// UserID is the user's unique identifier.
	UserID string

	// Username is the user's username.
	Username string

	// Email is the user's email address.
	Email string

	// TenantID is the tenant the user belongs to.
	TenantID string

	// Roles are the roles assigned to the user.
	Roles []string

	// Groups are the groups the user belongs to.
	Groups []string

	// Permissions are the specific permissions granted.
	Permissions []string

	// IssuedAt is when the token was issued.
	IssuedAt time.Time

	// ExpiresAt is when the token expires.
	ExpiresAt time.Time

	// Issuer is the token issuer.
	Issuer string

	// Audience is the intended audience for the token.
	Audience []string

	// Subject is the token subject (usually user ID).
	Subject string

	// SessionID is the session this token belongs to.
	SessionID string

	// CustomClaims contains any additional custom claims.
	CustomClaims map[string]interface{}
}

// =============================================================================
// OIDC/Federation Types
// =============================================================================

// ProviderType represents the type of identity provider.
type ProviderType int

const (
	// ProviderTypeUnspecified is an unspecified provider type.
	ProviderTypeUnspecified ProviderType = iota
	// ProviderTypeOIDC is an OpenID Connect provider.
	ProviderTypeOIDC
	// ProviderTypeSAML is a SAML 2.0 provider.
	ProviderTypeSAML
	// ProviderTypeOAuth2 is a generic OAuth2 provider.
	ProviderTypeOAuth2
)

// String returns the string representation of the provider type.
func (t ProviderType) String() string {
	switch t {
	case ProviderTypeOIDC:
		return "OIDC"
	case ProviderTypeSAML:
		return "SAML"
	case ProviderTypeOAuth2:
		return "OAUTH2"
	default:
		return "UNSPECIFIED"
	}
}

// BeginFederatedLoginRequest initiates an OIDC/OAuth2 flow.
type BeginFederatedLoginRequest struct {
	// TenantID is the tenant for this authentication.
	TenantID string

	// ProviderID is the identity provider to use.
	ProviderID string

	// RedirectURI is where to redirect after authentication.
	RedirectURI string

	// State is optional client-provided state to include in the callback.
	State string

	// Scopes are additional OAuth2 scopes to request.
	Scopes []string

	// Prompt controls the IdP authentication UI behavior.
	Prompt string

	// LoginHint suggests the user identity to the IdP.
	LoginHint string
}

// BeginFederatedLoginResponse contains the authorization URL.
type BeginFederatedLoginResponse struct {
	// AuthorizationURL is the URL to redirect the user to.
	AuthorizationURL string

	// State is the state parameter to verify in the callback.
	State string

	// Nonce is the nonce for OIDC token verification.
	Nonce string
}

// HandleFederatedCallbackRequest processes the IdP callback.
type HandleFederatedCallbackRequest struct {
	// TenantID is the tenant for this authentication.
	TenantID string

	// ProviderID is the identity provider that sent the callback.
	ProviderID string

	// Code is the authorization code from the IdP.
	Code string

	// State is the state parameter to verify.
	State string

	// RedirectURI is the redirect URI used in the original request.
	RedirectURI string
}

// HandleFederatedCallbackResponse contains the authentication result.
type HandleFederatedCallbackResponse struct {
	// AccessToken is the JWT access token.
	AccessToken string

	// RefreshToken is the refresh token.
	RefreshToken string

	// ExpiresAt is when the access token expires.
	ExpiresAt time.Time

	// TokenType is the type of token.
	TokenType string

	// UserID is the authenticated user's ID.
	UserID string

	// Username is the authenticated user's username.
	Username string

	// Email is the authenticated user's email.
	Email string

	// NewUser indicates if a new user account was created.
	NewUser bool

	// LinkedAccount indicates if the IdP account was linked to existing user.
	LinkedAccount bool

	// IDToken is the raw OIDC ID token from the IdP.
	IDToken string
}

// AuthProvider represents an identity provider configuration.
type AuthProvider struct {
	// ID is the unique identifier for this provider.
	ID string

	// TenantID is the tenant this provider belongs to.
	TenantID string

	// Name is the display name for this provider.
	Name string

	// Type is the provider type (OIDC, SAML, OAuth2).
	Type ProviderType

	// Enabled indicates if this provider is active.
	Enabled bool

	// IssuerURL is the OIDC issuer URL.
	IssuerURL string

	// AuthURL is the authorization endpoint.
	AuthURL string

	// TokenURL is the token endpoint.
	TokenURL string

	// UserInfoURL is the userinfo endpoint.
	UserInfoURL string

	// Scopes are the OAuth2 scopes to request.
	Scopes []string

	// AutoProvision enables automatic user creation.
	AutoProvision bool

	// AllowLinking enables account linking.
	AllowLinking bool

	// DefaultRoles are roles assigned to new users.
	DefaultRoles []string

	// CreatedAt is when this provider was created.
	CreatedAt time.Time

	// UpdatedAt is when this provider was last updated.
	UpdatedAt time.Time
}

// AuthListProvidersResponse contains the list of providers.
type AuthListProvidersResponse struct {
	// Providers is the list of identity providers.
	Providers []AuthProvider
}

// =============================================================================
// Session Management Types
// =============================================================================

// AuthSession represents a user session.
type AuthSession struct {
	// ID is the unique session identifier.
	ID string

	// UserID is the user this session belongs to.
	UserID string

	// TenantID is the tenant this session is in.
	TenantID string

	// CreatedAt is when the session was created.
	CreatedAt time.Time

	// LastActive is when the session was last used.
	LastActive time.Time

	// ExpiresAt is when the session expires.
	ExpiresAt time.Time

	// MFAVerified indicates if MFA was completed for this session.
	MFAVerified bool

	// IPAddress is the IP address that created the session.
	IPAddress string

	// UserAgent is the user agent string.
	UserAgent string

	// DeviceInfo contains device identification info.
	DeviceInfo string

	// AuthMethod is how the user authenticated.
	AuthMethod string
}

// AuthListSessionsResponse contains the list of sessions.
type AuthListSessionsResponse struct {
	// Sessions is the list of active sessions.
	Sessions []AuthSession
}

// =============================================================================
// Token Introspection Types
// =============================================================================

// TokenIntrospection contains detailed token information.
type TokenIntrospection struct {
	// Active indicates if the token is valid and active.
	Active bool

	// Scope contains the token scopes.
	Scope string

	// ClientID is the client that requested the token.
	ClientID string

	// Username is the resource owner username.
	Username string

	// TokenType is the type of token.
	TokenType string

	// ExpiresAt is when the token expires.
	ExpiresAt time.Time

	// IssuedAt is when the token was issued.
	IssuedAt time.Time

	// NotBefore is the earliest time the token is valid.
	NotBefore time.Time

	// Subject is the token subject.
	Subject string

	// Audience is the token audience.
	Audience []string

	// Issuer is the token issuer.
	Issuer string

	// JTI is the JWT ID.
	JTI string
}

// =============================================================================
// JWKS Types
// =============================================================================

// JWKS represents a JSON Web Key Set.
type JWKS struct {
	// Keys is the list of JSON Web Keys.
	Keys []JWK
}

// JWK represents a JSON Web Key.
type JWK struct {
	// KTY is the key type (RSA, EC, etc.).
	KTY string

	// Use is the intended use (sig, enc).
	Use string

	// KID is the key ID.
	KID string

	// ALG is the algorithm.
	ALG string

	// N is the RSA modulus (for RSA keys).
	N string

	// E is the RSA public exponent (for RSA keys).
	E string

	// CRV is the curve (for EC keys).
	CRV string

	// X is the X coordinate (for EC keys).
	X string

	// Y is the Y coordinate (for EC keys).
	Y string

	// X5C is the X.509 certificate chain.
	X5C []string

	// X5T is the X.509 thumbprint.
	X5T string
}

// =============================================================================
// OpenID Configuration Types
// =============================================================================

// OpenIDConfiguration represents the OIDC discovery document.
// See: https://openid.net/specs/openid-connect-discovery-1_0.html
type OpenIDConfiguration struct {
	// Issuer is the identifier for the OIDC provider.
	Issuer string

	// AuthorizationEndpoint is the URL of the authorization endpoint.
	AuthorizationEndpoint string

	// TokenEndpoint is the URL of the token endpoint.
	TokenEndpoint string

	// UserInfoEndpoint is the URL of the userinfo endpoint.
	UserInfoEndpoint string

	// JWKSUri is the URL of the JWKS endpoint.
	JWKSURI string

	// RegistrationEndpoint is the URL for dynamic client registration.
	RegistrationEndpoint string

	// ScopesSupported lists the OAuth 2.0 scopes supported.
	ScopesSupported []string

	// ResponseTypesSupported lists the OAuth 2.0 response types supported.
	ResponseTypesSupported []string

	// ResponseModesSupported lists the OAuth 2.0 response modes supported.
	ResponseModesSupported []string

	// GrantTypesSupported lists the OAuth 2.0 grant types supported.
	GrantTypesSupported []string

	// ACRValuesSupported lists the Authentication Context Class References supported.
	ACRValuesSupported []string

	// SubjectTypesSupported lists the subject identifier types supported.
	SubjectTypesSupported []string

	// IDTokenSigningAlgValuesSupported lists the signing algorithms for ID tokens.
	IDTokenSigningAlgValuesSupported []string

	// IDTokenEncryptionAlgValuesSupported lists encryption algorithms for ID tokens.
	IDTokenEncryptionAlgValuesSupported []string

	// IDTokenEncryptionEncValuesSupported lists encryption encodings for ID tokens.
	IDTokenEncryptionEncValuesSupported []string

	// UserInfoSigningAlgValuesSupported lists signing algorithms for userinfo.
	UserInfoSigningAlgValuesSupported []string

	// UserInfoEncryptionAlgValuesSupported lists encryption algorithms for userinfo.
	UserInfoEncryptionAlgValuesSupported []string

	// UserInfoEncryptionEncValuesSupported lists encryption encodings for userinfo.
	UserInfoEncryptionEncValuesSupported []string

	// RequestObjectSigningAlgValuesSupported lists signing algorithms for request objects.
	RequestObjectSigningAlgValuesSupported []string

	// TokenEndpointAuthMethodsSupported lists authentication methods for the token endpoint.
	TokenEndpointAuthMethodsSupported []string

	// TokenEndpointAuthSigningAlgValuesSupported lists signing algorithms for token auth.
	TokenEndpointAuthSigningAlgValuesSupported []string

	// DisplayValuesSupported lists the display parameter values supported.
	DisplayValuesSupported []string

	// ClaimTypesSupported lists the claim types supported.
	ClaimTypesSupported []string

	// ClaimsSupported lists the claims supported.
	ClaimsSupported []string

	// ServiceDocumentation is the URL to service documentation.
	ServiceDocumentation string

	// ClaimsLocalesSupported lists the supported claim locales.
	ClaimsLocalesSupported []string

	// UILocalesSupported lists the supported UI locales.
	UILocalesSupported []string

	// ClaimsParameterSupported indicates if the claims parameter is supported.
	ClaimsParameterSupported bool

	// RequestParameterSupported indicates if the request parameter is supported.
	RequestParameterSupported bool

	// RequestURIParameterSupported indicates if the request_uri parameter is supported.
	RequestURIParameterSupported bool

	// RequireRequestURIRegistration indicates if request_uri must be pre-registered.
	RequireRequestURIRegistration bool

	// OPPolicyURI is the URL to the provider's policy documentation.
	OPPolicyURI string

	// OPTOSUri is the URL to the provider's terms of service.
	OPTOSURI string

	// RevocationEndpoint is the URL of the token revocation endpoint.
	RevocationEndpoint string

	// RevocationEndpointAuthMethodsSupported lists auth methods for revocation endpoint.
	RevocationEndpointAuthMethodsSupported []string

	// IntrospectionEndpoint is the URL of the token introspection endpoint.
	IntrospectionEndpoint string

	// IntrospectionEndpointAuthMethodsSupported lists auth methods for introspection.
	IntrospectionEndpointAuthMethodsSupported []string

	// CodeChallengeMethodsSupported lists PKCE code challenge methods supported.
	CodeChallengeMethodsSupported []string

	// EndSessionEndpoint is the URL for logout/end session.
	EndSessionEndpoint string

	// BackchannelLogoutSupported indicates if backchannel logout is supported.
	BackchannelLogoutSupported bool

	// BackchannelLogoutSessionSupported indicates if backchannel logout sessions are supported.
	BackchannelLogoutSessionSupported bool

	// FrontchannelLogoutSupported indicates if frontchannel logout is supported.
	FrontchannelLogoutSupported bool

	// FrontchannelLogoutSessionSupported indicates if frontchannel logout sessions are supported.
	FrontchannelLogoutSessionSupported bool

	// MFASupported indicates if multi-factor authentication is supported.
	MFASupported bool

	// MFAMethodsSupported lists the MFA methods supported.
	MFAMethodsSupported []string

	// DeviceAuthorizationEndpoint is the URL for device authorization flow.
	DeviceAuthorizationEndpoint string
}
