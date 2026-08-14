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

import (
	"encoding/json"
	"time"
)

// UnixTime is a time.Time that can unmarshal from JWT's Unix timestamp format (numeric).
// JWT stores exp, iat, nbf as Unix seconds, not RFC3339 strings.
type UnixTime struct {
	time.Time
}

// UnmarshalJSON handles both Unix timestamp (number) and RFC3339 string formats.
func (u *UnixTime) UnmarshalJSON(data []byte) error {
	// Try to parse as number first (Unix timestamp)
	var timestamp int64
	if err := json.Unmarshal(data, &timestamp); err == nil {
		u.Time = time.Unix(timestamp, 0)
		return nil
	}

	// Try to parse as string (RFC3339)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return &TimeParseError{Detail: "failed to parse time string", Cause: err}
		}
		u.Time = t
		return nil
	}

	return &UnixTimeFormatError{}
}

// MarshalJSON outputs the time as a Unix timestamp for JWT compatibility.
func (u UnixTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.Unix())
}

// NewUnixTime creates a UnixTime from a time.Time.
func NewUnixTime(t time.Time) UnixTime {
	return UnixTime{Time: t}
}

// OIDC grant types constants.
const (
	GrantTypeAuthorizationCode = "authorization_code"
	GrantTypeClientCredentials = "client_credentials"
	GrantTypeRefreshToken      = "refresh_token"
	GrantTypeDeviceCode        = "device_code"
)

// OIDC response types constants.
const (
	ResponseTypeCode = "code"
)

// OIDC token types constants.
const (
	TokenTypeBearer  = "Bearer"
	TokenTypeDPoP    = "DPoP"
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
	TokenTypeID      = "id"
)

// PKCE code challenge methods.
// Note: OAuth 2.1 mandates S256 only. Plain is deprecated and rejected.
const (
	// CodeChallengeMethodPlain is prohibited per OAuth 2.1.
	//
	// Deprecated: This method is rejected. Use CodeChallengeMethodS256 instead.
	CodeChallengeMethodPlain = "plain"

	// CodeChallengeMethodS256 is the only supported PKCE method per OAuth 2.1.
	CodeChallengeMethodS256 = "S256"
)

// AuthorizeRequest represents an OAuth2 authorization request.
type AuthorizeRequest struct {
	// ClientID is the OAuth2 client identifier
	ClientID string `json:"client_id" form:"client_id"`

	// RedirectURI is the callback URL after authorization
	RedirectURI string `json:"redirect_uri" form:"redirect_uri"`

	// ResponseType specifies the OAuth2 flow ("code" for authorization code flow)
	ResponseType string `json:"response_type" form:"response_type"`

	// Scope contains space-separated or array of requested scopes
	Scope []string `json:"scope" form:"scope"`

	// State is an opaque value for CSRF protection
	State string `json:"state" form:"state"`

	// CodeChallenge is the PKCE code challenge (base64url-encoded SHA256 hash)
	CodeChallenge string `json:"code_challenge" form:"code_challenge"`

	// CodeChallengeMethod is the PKCE method (must be "S256" per OAuth 2.1)
	CodeChallengeMethod string `json:"code_challenge_method" form:"code_challenge_method"`

	// Nonce is used to associate a client session with an ID token
	Nonce string `json:"nonce" form:"nonce"`

	// Resource is the target resource server URI (RFC 8707)
	// This binds the resulting token to a specific resource server via the audience claim.
	Resource string `json:"resource" form:"resource"`

	// UserID is the authenticated user (set internally after authentication)
	UserID string `json:"-"`

	// TenantID is the tenant context (set internally)
	TenantID string `json:"-"`
}

// AuthorizeResponse represents an OAuth2 authorization response.
type AuthorizeResponse struct {
	// Code is the authorization code
	Code string `json:"code"`

	// State echoes the state parameter from the request
	State string `json:"state,omitempty"`

	// RedirectURI is the redirect URI to send the response to
	RedirectURI string `json:"redirect_uri"`
}

// TokenRequest represents an OAuth2 token request.
type TokenRequest struct {
	// GrantType specifies the OAuth2 grant type
	GrantType string `json:"grant_type" form:"grant_type"`

	// Code is the authorization code (for authorization_code grant)
	Code string `json:"code" form:"code"`

	// RedirectURI must match the redirect URI from the authorization request
	RedirectURI string `json:"redirect_uri" form:"redirect_uri"`

	// ClientID is the OAuth2 client identifier
	ClientID string `json:"client_id" form:"client_id"`

	// ClientSecret is the OAuth2 client secret
	ClientSecret string `json:"client_secret" form:"client_secret"`

	// CodeVerifier is the PKCE code verifier
	CodeVerifier string `json:"code_verifier" form:"code_verifier"`

	// Scope contains requested scopes (for client_credentials grant)
	Scope []string `json:"scope" form:"scope"`

	// RefreshToken is the refresh token (for refresh_token grant)
	RefreshToken string `json:"refresh_token" form:"refresh_token"`

	// Resource is the target resource server URI (RFC 8707)
	// This binds the token to a specific resource server via the audience claim.
	Resource string `json:"resource" form:"resource"`

	// DPoPProof is the RFC 9449 DPoP proof JWT presented in the DPoP request
	// header. When non-empty the token endpoint verifies it and binds the
	// resulting access token to the proof key via cnf.jkt (sender-constrained).
	// The HTTP transport populates this from the DPoP header; it is never a
	// form/JSON body field.
	DPoPProof string `json:"-" form:"-"`

	// HTTPMethod and HTTPURI are the request method and canonical target URI of
	// the token endpoint, supplied by the transport for DPoP htm/htu binding.
	// They are only consulted when DPoPProof is set.
	HTTPMethod string `json:"-" form:"-"`
	HTTPURI    string `json:"-" form:"-"`
}

// TokenResponse represents an OAuth2 token response.
type TokenResponse struct {
	// AccessToken is the access token
	AccessToken string `json:"access_token"`

	// TokenType is the token type (always "Bearer")
	TokenType string `json:"token_type"`

	// ExpiresIn is the token lifetime in seconds
	ExpiresIn int64 `json:"expires_in"`

	// RefreshToken is the refresh token (optional)
	RefreshToken string `json:"refresh_token,omitempty"`

	// IDToken is the OpenID Connect ID token (optional)
	IDToken string `json:"id_token,omitempty"`

	// Scope is the space-separated list of granted scopes
	Scope string `json:"scope,omitempty"`
}

// RefreshRequest represents a token refresh request.
type RefreshRequest struct {
	// RefreshToken is the refresh token to exchange
	RefreshToken string `json:"refresh_token"`

	// ClientID is the OAuth2 client identifier
	ClientID string `json:"client_id"`

	// ClientSecret is the OAuth2 client secret
	ClientSecret string `json:"client_secret"`

	// Scope is the requested scope (optional, must be subset of original)
	Scope []string `json:"scope,omitempty"`
}

// RevokeRequest represents a token revocation request.
type RevokeRequest struct {
	// Token is the token to revoke
	Token string `json:"token"`

	// TokenTypeHint hints at the token type ("access_token" or "refresh_token")
	TokenTypeHint string `json:"token_type_hint,omitempty"`

	// ClientID is the OAuth2 client identifier
	ClientID string `json:"client_id"`

	// ClientSecret is the OAuth2 client secret
	ClientSecret string `json:"client_secret"`
}

// IntrospectRequest represents an RFC 7662 token introspection request.
type IntrospectRequest struct {
	// Token is the token whose status is being introspected.
	Token string `json:"token"`

	// TokenTypeHint hints at the token type ("access_token" or "refresh_token").
	TokenTypeHint string `json:"token_type_hint,omitempty"`
}

// IntrospectionResponse is the RFC 7662 §2.2 introspection response. When the
// token is not active, only the Active field is populated (all other fields
// are omitted) so the endpoint never leaks claims for invalid/expired/revoked
// tokens. When active, the standard claim set is returned.
type IntrospectionResponse struct {
	// Active indicates whether the token is currently valid (not expired and
	// not revoked). RFC 7662 requires this field on every response.
	Active bool `json:"active"`

	// Scope is the space-separated list of scopes (RFC 7662 §2.2).
	Scope string `json:"scope,omitempty"`

	// SCP is the array-form scope list matching the platform access-token
	// contract. Retained alongside Scope for downstream RBAC mapping.
	SCP []string `json:"scp,omitempty"`

	// ClientID is the OAuth client the token was issued to.
	ClientID string `json:"client_id,omitempty"`

	// Subject is the token subject (the qrdb user / service identifier).
	Subject string `json:"sub,omitempty"`

	// TenantID is the tenant the token is scoped to (platform contract).
	TenantID string `json:"tenant_id,omitempty"`

	// Audience is the intended audience(s) of the token.
	Audience []string `json:"aud,omitempty"`

	// Issuer is the token issuer (the single broker issuer).
	Issuer string `json:"iss,omitempty"`

	// ExpiresAt is the token expiration time (Unix seconds).
	ExpiresAt int64 `json:"exp,omitempty"`

	// IssuedAt is the token issuance time (Unix seconds).
	IssuedAt int64 `json:"iat,omitempty"`

	// NotBefore is the time before which the token is not valid (Unix seconds).
	NotBefore int64 `json:"nbf,omitempty"`

	// JTI is the unique token identifier.
	JTI string `json:"jti,omitempty"`

	// TokenType is the token type ("Bearer").
	TokenType string `json:"token_type,omitempty"`

	// Username is the human-readable subject name when available.
	Username string `json:"username,omitempty"`

	// Confirmation carries the RFC 7800 cnf claim (DPoP jkt / mTLS x5t#S256)
	// so a resource server can enforce sender-constrained presentation.
	Confirmation *ConfirmationClaim `json:"cnf,omitempty"`

	// Roles is the list of user roles carried by the token.
	Roles []string `json:"roles,omitempty"`
}

// UserInfoResponse represents the OIDC UserInfo endpoint response.
type UserInfoResponse struct {
	// Sub is the subject identifier (user ID)
	Sub string `json:"sub"`

	// Name is the user's full name
	Name string `json:"name,omitempty"`

	// Email is the user's email address
	Email string `json:"email,omitempty"`

	// EmailVerified indicates if the email is verified
	EmailVerified bool `json:"email_verified,omitempty"`

	// TenantID is the user's tenant
	TenantID string `json:"tenant_id,omitempty"`

	// Roles is the list of user roles
	Roles []string `json:"roles,omitempty"`

	// Groups is the list of user groups
	Groups []string `json:"groups,omitempty"`

	// Attributes contains custom user attributes
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// ConfirmationClaim is the RFC 7800 cnf claim. For DPoP sender-constrained
// tokens (RFC 9449) it carries jkt, the base64url SHA-256 JWK thumbprint of the
// DPoP proof-of-possession key. For mTLS-bound tokens (RFC 8705) it carries
// x5t#S256, the client-certificate thumbprint. The two are mutually exclusive
// on a given token.
type ConfirmationClaim struct {
	// Jkt is the RFC 9449 DPoP key thumbprint (base64url SHA-256 of the JWK).
	Jkt string `json:"jkt,omitempty"`

	// X5TS256 is the RFC 8705 mTLS client-certificate SHA-256 thumbprint.
	X5TS256 string `json:"x5t#S256,omitempty"`
}

// Claims represents JWT access token claims.
type Claims struct {
	// Issuer is the token issuer
	Issuer string `json:"iss"`

	// Subject is the user or service identifier
	Subject string `json:"sub"`

	// Audience is the list of intended audiences
	Audience []string `json:"aud"`

	// ExpiresAt is the token expiration time (Unix timestamp in JWT)
	ExpiresAt UnixTime `json:"exp"`

	// IssuedAt is the token issuance time (Unix timestamp in JWT)
	IssuedAt UnixTime `json:"iat"`

	// NotBefore is the time before which the token is not valid (Unix timestamp in JWT)
	NotBefore UnixTime `json:"nbf,omitempty"`

	// JTI is the unique token identifier
	JTI string `json:"jti"`

	// TenantID is the tenant context
	TenantID string `json:"tenant_id,omitempty"`

	// Roles is the list of user roles
	Roles []string `json:"roles,omitempty"`

	// Groups is the list of user groups
	Groups []string `json:"groups,omitempty"`

	// Email is the user's email
	Email string `json:"email,omitempty"`

	// Name is the user's name
	Name string `json:"name,omitempty"`

	// Scope is the space-separated list of scopes
	Scope string `json:"scope,omitempty"`

	// SCP is the array-form scope list used by the platform access-token
	// contract (scp). It is the authoritative scope set for downstream RBAC
	// mapping; Scope (space-separated) is retained for OIDC back-compat.
	SCP []string `json:"scp,omitempty"`

	// ClientID is the OAuth client ID
	ClientID string `json:"client_id,omitempty"`

	// MFAVerified indicates if MFA was completed
	MFAVerified bool `json:"mfa_verified,omitempty"`

	// MFATimestamp is the MFA verification timestamp
	MFATimestamp *UnixTime `json:"mfa_timestamp,omitempty"`

	// AuthMethod is the authentication method used
	AuthMethod string `json:"auth_method,omitempty"`

	// AMR is the Authentication Methods References (RFC 8176).
	// Examples: ["webauthn","hwk"], ["smartcard","mfa"]
	AMR []string `json:"amr,omitempty"`

	// ACR is the Authentication Context Class Reference.
	// For hardware-attested logins: "urn:quicbaas:auth:hardware-attested"
	ACR string `json:"acr,omitempty"`

	// Confirmation is the RFC 7800 cnf claim binding the token to a
	// proof-of-possession key (DPoP jkt or mTLS x5t#S256). When set, the token
	// is sender-constrained: a presenter must prove possession of the bound key.
	Confirmation *ConfirmationClaim `json:"cnf,omitempty"`
}

// IDTokenClaims represents OpenID Connect ID token claims.
type IDTokenClaims struct {
	// Issuer is the token issuer
	Issuer string `json:"iss"`

	// Subject is the user identifier
	Subject string `json:"sub"`

	// Audience is the list of intended audiences
	Audience []string `json:"aud"`

	// ExpiresAt is the token expiration time (Unix timestamp in JWT)
	ExpiresAt UnixTime `json:"exp"`

	// IssuedAt is the token issuance time (Unix timestamp in JWT)
	IssuedAt UnixTime `json:"iat"`

	// AuthTime is the time when authentication occurred (Unix timestamp in JWT)
	AuthTime UnixTime `json:"auth_time,omitempty"`

	// Nonce is the nonce value from the authorization request
	Nonce string `json:"nonce,omitempty"`

	// Email is the user's email
	Email string `json:"email,omitempty"`

	// EmailVerified indicates if email is verified
	EmailVerified bool `json:"email_verified,omitempty"`

	// Name is the user's display name
	Name string `json:"name,omitempty"`

	// TenantID is the user's tenant
	TenantID string `json:"tenant_id,omitempty"`

	// Roles is the list of user roles
	Roles []string `json:"roles,omitempty"`

	// Groups is the list of user groups
	Groups []string `json:"groups,omitempty"`

	// MFAVerified indicates if MFA was completed
	MFAVerified bool `json:"mfa_verified,omitempty"`

	// AuthMethod is the authentication method
	AuthMethod string `json:"auth_method,omitempty"`

	// AMR is the Authentication Methods References (RFC 8176).
	// Examples: ["webauthn","hwk"], ["smartcard","mfa"]
	AMR []string `json:"amr,omitempty"`

	// ACR is the Authentication Context Class Reference.
	// For hardware-attested logins: "urn:quicbaas:auth:hardware-attested"
	ACR string `json:"acr,omitempty"`

	// AttestAAGUID is the FIDO2 authenticator AAGUID in hex string form (WebAuthn logins only).
	AttestAAGUID string `json:"attest_aaguid,omitempty"`

	// AttestLevel is a human-readable attestation level (e.g. "hardware", "software", "none").
	AttestLevel string `json:"attest_level,omitempty"`
}

// AuthCodeData represents stored authorization code data.
type AuthCodeData struct {
	// Code is the authorization code
	Code string

	// UserID is the authenticated user
	UserID string

	// TenantID is the tenant context
	TenantID string

	// ClientID is the requesting OAuth client
	ClientID string

	// RedirectURI is the authorized redirect URI
	RedirectURI string

	// Scope contains granted scopes
	Scope []string

	// CodeChallenge is the PKCE code challenge
	CodeChallenge string

	// CodeChallengeMethod is the PKCE method (must be S256 per OAuth 2.1)
	CodeChallengeMethod string

	// Nonce is the nonce for ID token
	Nonce string

	// Resource is the target resource server URI (RFC 8707)
	Resource string

	// ExpiresAt is the code expiration time
	ExpiresAt time.Time

	// CreatedAt is the code creation time
	CreatedAt time.Time
}

// IsExpired checks if the authorization code has expired.
func (a *AuthCodeData) IsExpired() bool {
	return time.Now().After(a.ExpiresAt)
}

// ValidatePKCE validates the code verifier against the stored challenge.
// Per OAuth 2.1, only S256 method is supported. Plain method is rejected.
func (a *AuthCodeData) ValidatePKCE(verifier string) error {
	if a.CodeChallenge == "" {
		return nil // PKCE not required for this code
	}

	if verifier == "" {
		return NewOAuthError(ErrOAuthInvalidRequest, "code_verifier is required")
	}

	switch a.CodeChallengeMethod {
	case CodeChallengeMethodPlain:
		// OAuth 2.1 prohibits the plain method
		return ErrPKCEPlainMethodNotAllowed
	case CodeChallengeMethodS256:
		// Verify SHA256(verifier) == challenge
		if !validatePKCES256(verifier, a.CodeChallenge) {
			return NewOAuthError(ErrOAuthInvalidGrant, "invalid code_verifier")
		}
	default:
		return NewOAuthError(ErrOAuthInvalidRequest, "unsupported code_challenge_method")
	}

	return nil
}

// RegisterAppOptions contains options for registering a new OAuth2 application.
type RegisterAppOptions struct {
	// Name is the application name
	Name string

	// RedirectURIs is the list of allowed redirect URIs
	RedirectURIs []string

	// ResourcePrefix is the resource namespace for this app
	ResourcePrefix string

	// DefaultAudience is the per-app default service audience stamped into the
	// token `aud` claim when the client sends no RFC 8707 `resource` indicator.
	// Empty means no default (backward compatible). Validated as an absolute
	// "api://<service>" or https URI.
	DefaultAudience string

	// GrantTypes is the list of allowed grant types
	GrantTypes []string

	// ResponseTypes is the list of allowed response types
	ResponseTypes []string

	// PKCERequired indicates if PKCE is mandatory
	PKCERequired bool
}

// UpdateAppOptions contains options for updating an OAuth2 application.
type UpdateAppOptions struct {
	// Name is the application name
	Name *string

	// RedirectURIs is the list of allowed redirect URIs
	RedirectURIs *[]string

	// ResourcePrefix is the resource namespace
	ResourcePrefix *string

	// DefaultAudience updates the per-app default service audience. A pointer to
	// the empty string clears the default; nil leaves it unchanged.
	DefaultAudience *string

	// GrantTypes is the list of allowed grant types
	GrantTypes *[]string

	// ResponseTypes is the list of allowed response types
	ResponseTypes *[]string

	// PKCERequired indicates if PKCE is mandatory
	PKCERequired *bool
}

// OIDCDependencies contains dependencies for the OIDC provider.
type OIDCDependencies struct {
	// IdentityStore manages users, groups, and roles
	IdentityStore *IdentityStore

	// AppStorage manages OAuth applications
	AppStorage AppStorage

	// TokenStorage manages tokens and auth codes
	TokenStorage TokenStorage

	// PermissionResolver, when set, resolves a subject's effective qrdb
	// permissions at access-token issuance so the scp claim carries the user's
	// RBAC authority (colon "resource:action"). Optional: when nil the scp claim
	// falls back to the OIDC scope (today's behavior), so non-server callers and
	// tests need not wire it.
	PermissionResolver PermissionResolver
}
