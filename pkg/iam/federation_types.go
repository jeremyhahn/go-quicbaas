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
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Default claim names used by OIDC providers.
const (
	DefaultSubjectClaim = "sub"
	DefaultEmailClaim   = "email"
	DefaultNameClaim    = "name"
)

// Federation typed error types for sentinel error values.

// ProviderNotFoundError indicates an external provider was not found.
type ProviderNotFoundError struct{}

// Error implements the error interface.
func (e *ProviderNotFoundError) Error() string {
	return "external provider not found"
}

// ProviderAlreadyExistsError indicates an external provider already exists.
type ProviderAlreadyExistsError struct{}

// Error implements the error interface.
func (e *ProviderAlreadyExistsError) Error() string {
	return "external provider already exists"
}

// ProviderInvalidError indicates an external provider configuration is invalid.
type ProviderInvalidError struct{}

// Error implements the error interface.
func (e *ProviderInvalidError) Error() string {
	return "invalid external provider"
}

// StateNotFoundError indicates a state token was not found.
type StateNotFoundError struct{}

// Error implements the error interface.
func (e *StateNotFoundError) Error() string {
	return "state token not found"
}

// StateExpiredError indicates a state token has expired.
type StateExpiredError struct{}

// Error implements the error interface.
func (e *StateExpiredError) Error() string {
	return "state token expired"
}

// StateInvalidError indicates a state token is invalid.
type StateInvalidError struct{}

// Error implements the error interface.
func (e *StateInvalidError) Error() string {
	return "invalid state token"
}

// TokenExchangeFailedError indicates an OAuth token exchange failure.
type TokenExchangeFailedError struct{}

// Error implements the error interface.
func (e *TokenExchangeFailedError) Error() string {
	return "token exchange failed"
}

// ClaimsExtractionFailedError indicates a claims extraction failure.
type ClaimsExtractionFailedError struct{}

// Error implements the error interface.
func (e *ClaimsExtractionFailedError) Error() string {
	return "failed to extract claims"
}

// IdentityNotLinkedError indicates a federated identity is not linked.
type IdentityNotLinkedError struct{}

// Error implements the error interface.
func (e *IdentityNotLinkedError) Error() string {
	return "federated identity not linked"
}

// IdentityAlreadyLinkedError indicates a federated identity is already linked.
type IdentityAlreadyLinkedError struct{}

// Error implements the error interface.
func (e *IdentityAlreadyLinkedError) Error() string {
	return "federated identity already linked"
}

// AutoProvisionDisabledError indicates auto-provisioning is disabled.
type AutoProvisionDisabledError struct{}

// Error implements the error interface.
func (e *AutoProvisionDisabledError) Error() string {
	return "auto-provisioning is disabled for this provider"
}

// LinkingDisabledError indicates account linking is disabled.
type LinkingDisabledError struct{}

// Error implements the error interface.
func (e *LinkingDisabledError) Error() string {
	return "account linking is disabled for this provider"
}

// ProviderDisabledError indicates a provider is disabled.
type ProviderDisabledError struct{}

// Error implements the error interface.
func (e *ProviderDisabledError) Error() string {
	return "external provider is disabled"
}

// OIDCDiscoveryFailedError indicates an OIDC discovery failure.
type OIDCDiscoveryFailedError struct{}

// Error implements the error interface.
func (e *OIDCDiscoveryFailedError) Error() string {
	return "OIDC discovery failed"
}

// FederatedIdentityNotFoundError indicates a federated identity was not found.
type FederatedIdentityNotFoundError struct{}

// Error implements the error interface.
func (e *FederatedIdentityNotFoundError) Error() string {
	return "federated identity not found"
}

// FederatedIdentityAlreadyLinkedError indicates a federated identity is already linked to a user.
type FederatedIdentityAlreadyLinkedError struct{}

// Error implements the error interface.
func (e *FederatedIdentityAlreadyLinkedError) Error() string {
	return "federated identity already linked to a user"
}

// AccountLinkingDisabledError indicates account linking is disabled for a provider.
type AccountLinkingDisabledError struct{}

// Error implements the error interface.
func (e *AccountLinkingDisabledError) Error() string {
	return "account linking is disabled for this provider"
}

// JITProvisioningDisabledError indicates JIT provisioning is disabled for a provider.
type JITProvisioningDisabledError struct{}

// Error implements the error interface.
func (e *JITProvisioningDisabledError) Error() string {
	return "JIT provisioning is disabled for this provider"
}

// OIDCTokenExchangeFailedError indicates an OIDC token exchange failure.
type OIDCTokenExchangeFailedError struct{}

// Error implements the error interface.
func (e *OIDCTokenExchangeFailedError) Error() string {
	return "OIDC token exchange failed"
}

// OIDCUserInfoFailedError indicates a failure fetching user info from an OIDC provider.
type OIDCUserInfoFailedError struct{}

// Error implements the error interface.
func (e *OIDCUserInfoFailedError) Error() string {
	return "failed to fetch user info from OIDC provider"
}

// OIDCClaimMappingFailedError indicates a failure mapping OIDC claims.
type OIDCClaimMappingFailedError struct{}

// Error implements the error interface.
func (e *OIDCClaimMappingFailedError) Error() string {
	return "failed to map OIDC claims"
}

// FederationStateNotFoundError indicates a federation state was not found.
type FederationStateNotFoundError struct{}

// Error implements the error interface.
func (e *FederationStateNotFoundError) Error() string {
	return "federation state not found"
}

// FederationStateExpiredError indicates a federation state has expired.
type FederationStateExpiredError struct{}

// Error implements the error interface.
func (e *FederationStateExpiredError) Error() string {
	return "federation state expired"
}

// Federation-specific error types.
var (
	// ErrProviderNotFound is returned when a provider is not found.
	ErrProviderNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.provider",
		Err:  &ProviderNotFoundError{},
	}

	// ErrProviderAlreadyExists is returned when attempting to create a duplicate provider.
	ErrProviderAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.federation.provider",
		Err:  &ProviderAlreadyExistsError{},
	}

	// ErrProviderInvalid is returned when provider validation fails.
	ErrProviderInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.federation.provider",
		Err:  &ProviderInvalidError{},
	}

	// ErrStateNotFound is returned when a state token is not found.
	ErrStateNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.state",
		Err:  &StateNotFoundError{},
	}

	// ErrStateExpired is returned when a state token has expired.
	ErrStateExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.state",
		Err:  &StateExpiredError{},
	}

	// ErrStateInvalid is returned when state validation fails.
	ErrStateInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.federation.state",
		Err:  &StateInvalidError{},
	}

	// ErrFederationStateInvalid is an alias for ErrStateInvalid for backward compatibility.
	ErrFederationStateInvalid = ErrStateInvalid

	// ErrTokenExchangeFailed is returned when OAuth token exchange fails.
	ErrTokenExchangeFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.token",
		Err:  &TokenExchangeFailedError{},
	}

	// ErrClaimsExtractionFailed is returned when claims extraction fails.
	ErrClaimsExtractionFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.claims",
		Err:  &ClaimsExtractionFailedError{},
	}

	// ErrIdentityNotLinked is returned when a federated identity is not linked.
	ErrIdentityNotLinked = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.identity",
		Err:  &IdentityNotLinkedError{},
	}

	// ErrIdentityAlreadyLinked is returned when an identity is already linked.
	ErrIdentityAlreadyLinked = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.federation.identity",
		Err:  &IdentityAlreadyLinkedError{},
	}

	// ErrAutoProvisionDisabled is returned when auto-provisioning is disabled.
	ErrAutoProvisionDisabled = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.provision",
		Err:  &AutoProvisionDisabledError{},
	}

	// ErrLinkingDisabled is returned when account linking is disabled.
	ErrLinkingDisabled = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.link",
		Err:  &LinkingDisabledError{},
	}

	// ErrProviderDisabled is returned when a provider is disabled.
	ErrProviderDisabled = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.provider",
		Err:  &ProviderDisabledError{},
	}

	// ErrOIDCDiscoveryFailed is returned when OIDC discovery fails.
	ErrOIDCDiscoveryFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.oidc",
		Err:  &OIDCDiscoveryFailedError{},
	}

	// ErrFederatedIdentityNotFound is returned when a federated identity is not found.
	ErrFederatedIdentityNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.identity",
		Err:  &FederatedIdentityNotFoundError{},
	}

	// ErrFederatedIdentityAlreadyLinked is returned when identity is already linked.
	ErrFederatedIdentityAlreadyLinked = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.federation.identity",
		Err:  &FederatedIdentityAlreadyLinkedError{},
	}

	// ErrAccountLinkingDisabled is returned when account linking is disabled.
	ErrAccountLinkingDisabled = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.linking",
		Err:  &AccountLinkingDisabledError{},
	}

	// ErrJITProvisioningDisabled is returned when JIT provisioning is disabled.
	ErrJITProvisioningDisabled = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.provisioning",
		Err:  &JITProvisioningDisabledError{},
	}

	// ErrOIDCTokenExchangeFailed is returned when OIDC token exchange fails.
	ErrOIDCTokenExchangeFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.oidc.token",
		Err:  &OIDCTokenExchangeFailedError{},
	}

	// ErrOIDCUserInfoFailed is returned when fetching user info fails.
	ErrOIDCUserInfoFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.oidc.userinfo",
		Err:  &OIDCUserInfoFailedError{},
	}

	// ErrOIDCClaimMappingFailed is returned when claim mapping fails.
	ErrOIDCClaimMappingFailed = &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.oidc.claims",
		Err:  &OIDCClaimMappingFailedError{},
	}

	// ErrFederationStateNotFound is returned when callback state is not found.
	ErrFederationStateNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.state",
		Err:  &FederationStateNotFoundError{},
	}

	// ErrFederationStateExpired is returned when callback state has expired.
	ErrFederationStateExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.state",
		Err:  &FederationStateExpiredError{},
	}

	// ErrEmailNotVerified is returned when an email-link/JIT login presents an
	// IdP token whose email is not verified (account-takeover guard).
	ErrEmailNotVerified = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.email",
		Err:  &EmailNotVerifiedError{},
	}

	// ErrMissingEmailClaim is returned when email linking/JIT is in play but the
	// IdP token has no email claim.
	ErrMissingEmailClaim = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.email",
		Err:  &MissingEmailClaimError{},
	}

	// ErrEmailDomainNotAllowed is returned when the email's domain is not in the
	// provider's AllowedEmailDomains allowlist.
	ErrEmailDomainNotAllowed = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.email",
		Err:  &EmailDomainNotAllowedError{},
	}

	// ErrNoRolesResolvable is returned when RequireExplicitRoleMapping is set and
	// a login resolves to zero roles (fail-closed, no zero-role user).
	ErrNoRolesResolvable = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.roles",
		Err:  &NoRolesResolvableError{},
	}
)

// ProviderType represents the type of external identity provider.
type ProviderType string

const (
	// ProviderTypeOIDC is an OpenID Connect provider.
	ProviderTypeOIDC ProviderType = "oidc"

	// ProviderTypeSAML is a SAML 2.0 provider.
	ProviderTypeSAML ProviderType = "saml"

	// ProviderTypeOAuth2 is a generic OAuth2 provider.
	ProviderTypeOAuth2 ProviderType = "oauth2"

	// ProviderTypeLocal is the tenant's local-factor authentication path
	// (WebAuthn/FIDO2/PIV/password — whatever the tenant has configured). It is
	// NOT a new authentication mechanism: it is a first-class home-realm ROUTING
	// SELECTOR so home-realm discovery can route a login to the tenant's built-in
	// factor stack instead of an external IdP. A login routed to the local realm
	// bypasses the OAuth2 federation flow entirely.
	ProviderTypeLocal ProviderType = "local"
)

// LocalRealmProviderID is the reserved provider identifier home-realm discovery
// returns when a login routes to a tenant's local-factor path rather than an
// external IdP. It is a routing sentinel, not a stored ExternalProvider.
const LocalRealmProviderID = "local"

// ExternalProvider represents an external identity provider configuration.
type ExternalProvider struct {
	// ID is the unique provider identifier.
	ID string `json:"id"`

	// TenantID is the owning tenant.
	TenantID string `json:"tenant_id"`

	// Name is the human-readable provider name.
	Name string `json:"name"`

	// Type is the provider type (oidc, saml, oauth2).
	Type ProviderType `json:"type"`

	// Enabled indicates whether the provider is active.
	Enabled bool `json:"enabled"`

	// ClientID is the OAuth2 client identifier.
	ClientID string `json:"client_id"`

	// ClientSecret is the OAuth2 client secret.
	ClientSecret string `json:"client_secret"`

	// IssuerURL is the OIDC issuer URL for discovery.
	IssuerURL string `json:"issuer_url"`

	// AuthURL is the OAuth2 authorization endpoint (optional override).
	AuthURL string `json:"auth_url,omitempty"`

	// TokenURL is the OAuth2 token endpoint.
	TokenURL string `json:"token_url,omitempty"`

	// UserInfoURL is the OAuth2 userinfo endpoint.
	UserInfoURL string `json:"userinfo_url,omitempty"`

	// JWKSURL is the explicit JWKS endpoint used to verify ID-token signatures.
	// When empty it is discovered from the issuer's OIDC discovery document.
	JWKSURL string `json:"jwks_url,omitempty"`

	// Scopes are the OAuth2 scopes to request.
	Scopes []string `json:"scopes"`

	// ClaimMappings maps IdP claims to QuicBaaS user attributes.
	ClaimMappings *ClaimMappings `json:"claim_mappings,omitempty"`

	// AutoProvision enables JIT user provisioning.
	AutoProvision bool `json:"auto_provision"`

	// AllowLinking enables account linking by email.
	AllowLinking bool `json:"allow_linking"`

	// DefaultRoles are assigned to auto-provisioned users.
	DefaultRoles []string `json:"default_roles,omitempty"`

	// DefaultGroups are assigned to auto-provisioned users.
	DefaultGroups []string `json:"default_groups,omitempty"`

	// DefaultScopes are the platform token scopes (the `scp` catalog, e.g.
	// "<svc>.<verb>") granted to a federated login when no GroupScopeMappings are
	// configured (the back-compatible fallback). When GroupScopeMappings ARE
	// configured, scope resolution is fail-closed and DefaultScopes are ignored.
	DefaultScopes []string `json:"default_scopes,omitempty"`

	// AllowedEmailDomains restricts which IdP-asserted email domains may
	// authenticate through this provider. When non-empty, a federated login
	// whose verified email's domain is not in the list (case-insensitive) is
	// rejected. An empty list allows all domains (back-compatible default).
	AllowedEmailDomains []string `json:"allowed_email_domains,omitempty"`

	// RequireExplicitRoleMapping, when true, fails the login closed if neither
	// the GroupRoleMappings produced a match NOR DefaultRoles are configured —
	// no zero-role user is ever provisioned or linked. When false (the default)
	// the provider DefaultRoles are used as a fallback, preserving the legacy
	// behaviour.
	RequireExplicitRoleMapping bool `json:"require_explicit_role_mapping,omitempty"`

	// Created is the provider creation timestamp.
	Created time.Time `json:"created"`

	// Updated is the last modification timestamp.
	Updated time.Time `json:"updated"`
}

// Validate validates an ExternalProvider for correctness.
func (p *ExternalProvider) Validate() error {
	if p.ID == "" {
		return NewIAMError(ErrProviderInvalid, "iam.federation.provider.validate", "provider ID is required", nil)
	}
	if p.TenantID == "" {
		return NewIAMError(ErrProviderInvalid, "iam.federation.provider.validate", "tenant ID is required", nil)
	}
	if p.Name == "" {
		return NewIAMError(ErrProviderInvalid, "iam.federation.provider.validate", "provider name is required", nil)
	}
	if p.Type == "" {
		return NewIAMError(ErrProviderInvalid, "iam.federation.provider.validate", "provider type is required", nil)
	}
	if p.Type == ProviderTypeOIDC {
		if p.ClientID == "" {
			return NewIAMError(ErrProviderInvalid, "iam.federation.provider.validate", "client ID is required for OIDC providers", nil)
		}
		if p.IssuerURL == "" {
			return NewIAMError(ErrProviderInvalid, "iam.federation.provider.validate", "issuer URL is required for OIDC providers", nil)
		}
	}
	return nil
}

// ApplyDefaults applies default values to the provider configuration.
func (p *ExternalProvider) ApplyDefaults() {
	if p.Type == "" {
		p.Type = ProviderTypeOIDC
	}
	if len(p.Scopes) == 0 {
		p.Scopes = []string{"openid", "email", "profile"}
	}
	if p.ClaimMappings == nil {
		p.ClaimMappings = &ClaimMappings{}
	}
	p.ClaimMappings.ApplyDefaults()
}

// ClaimMappings defines how IdP claims map to QuicBaaS user attributes.
type ClaimMappings struct {
	// Subject is the claim for user ID (default: "sub").
	Subject string `json:"subject,omitempty"`

	// Email is the claim for email (default: "email").
	Email string `json:"email,omitempty"`

	// Name is the claim for display name (default: "name").
	Name string `json:"name,omitempty"`

	// Groups is the claim for group memberships.
	Groups string `json:"groups,omitempty"`

	// Roles is the claim for role assignments.
	Roles string `json:"roles,omitempty"`

	// Attributes maps custom claims to user attributes.
	Attributes map[string]string `json:"attributes,omitempty"`

	// GroupRoleMappings maps IdP group/role values (from the Groups/Roles
	// claims) to tenant-scoped quicbaas roles. When non-empty, role assignment
	// is fail-closed: a federated login whose IdP values match no entry is
	// denied. When empty, the provider DefaultRoles are used instead.
	GroupRoleMappings []GroupRoleMapping `json:"group_role_mappings,omitempty"`

	// GroupScopeMappings maps IdP group/role values (from the Groups/Roles
	// claims) to platform token scopes (the `scp` catalog, e.g. "<svc>.<verb>").
	// When non-empty, scope assignment is fail-closed: a federated login whose
	// IdP values match no entry is denied. When empty, the provider DefaultScopes
	// are used instead.
	GroupScopeMappings []GroupScopeMapping `json:"group_scope_mappings,omitempty"`
}

// GroupRoleMapping maps a single IdP group/role value to a tenant role.
type GroupRoleMapping struct {
	// Group is the IdP group or role value to match (case-insensitive).
	Group string `json:"group"`

	// RoleID is the tenant-scoped quicbaas role ID (or name) to assign when the
	// IdP value matches.
	RoleID string `json:"role_id"`

	// Prefix, when true, matches any IdP value that starts with Group
	// (STARTS_WITH) instead of requiring an exact match.
	Prefix bool `json:"prefix,omitempty"`
}

// GroupScopeMapping maps a single IdP group/role value to a platform token
// scope. It mirrors GroupRoleMapping but targets the `scp` catalog rather than
// tenant roles, keeping scope grants explicit and decoupled from role grants.
type GroupScopeMapping struct {
	// Group is the IdP group or role value to match (case-insensitive).
	Group string `json:"group"`

	// Scope is the platform token scope (e.g. "<svc>.<verb>") to grant when the
	// IdP value matches.
	Scope string `json:"scope"`

	// Prefix, when true, matches any IdP value that starts with Group
	// (STARTS_WITH) instead of requiring an exact match.
	Prefix bool `json:"prefix,omitempty"`
}

// ApplyDefaults applies default values to the claim mappings.
func (c *ClaimMappings) ApplyDefaults() {
	if c.Subject == "" {
		c.Subject = DefaultSubjectClaim
	}
	if c.Email == "" {
		c.Email = DefaultEmailClaim
	}
	if c.Name == "" {
		c.Name = DefaultNameClaim
	}
}

// FederatedIdentity represents a link between a QuicBaaS user and an external identity.
type FederatedIdentity struct {
	// ID is the unique identity link identifier.
	ID string `json:"id"`

	// UserID is the QuicBaaS user ID.
	UserID string `json:"user_id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id"`

	// ProviderID is the external provider ID.
	ProviderID string `json:"provider_id"`

	// ExternalID is the user's ID at the external provider.
	ExternalID string `json:"external_id"`

	// ExternalEmail is the user's email at the external provider.
	ExternalEmail string `json:"external_email,omitempty"`

	// ExternalName is the display name from the external IdP.
	ExternalName string `json:"external_name,omitempty"`

	// Claims contains the raw claims from the last authentication.
	Claims map[string]interface{} `json:"claims,omitempty"`

	// LastLogin is the last successful login via this provider.
	LastLogin time.Time `json:"last_login"`

	// Created is the link creation timestamp.
	Created time.Time `json:"created"`

	// Updated is the last modification timestamp.
	Updated time.Time `json:"updated"`
}

// Validate validates a FederatedIdentity for correctness.
func (i *FederatedIdentity) Validate() error {
	if i.ID == "" {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.identity.validate", "identity ID is required", nil)
	}
	if i.UserID == "" {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.identity.validate", "user ID is required", nil)
	}
	if i.TenantID == "" {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.identity.validate", "tenant ID is required", nil)
	}
	if i.ProviderID == "" {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.identity.validate", "provider ID is required", nil)
	}
	if i.ExternalID == "" {
		return NewIAMError(ErrFederatedIdentityNotFound, "iam.federation.identity.validate", "external ID is required", nil)
	}
	return nil
}

// LoginRedirect contains information for redirecting to the external IdP.
type LoginRedirect struct {
	// AuthURL is the authorization URL to redirect to.
	AuthURL string `json:"auth_url"`

	// State is the CSRF protection state token.
	State string `json:"state"`

	// ExpiresAt is when this redirect expires.
	ExpiresAt time.Time `json:"expires_at"`

	// ProviderID is the provider being used.
	ProviderID string `json:"provider_id,omitempty"`
}

// FederationCallbackState represents OAuth state parameter data.
type FederationCallbackState struct {
	// Nonce is the unique nonce for CSRF protection.
	Nonce string `json:"nonce"`

	// ProviderID is the provider being used.
	ProviderID string `json:"provider_id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id"`

	// RedirectURI is the URI to redirect to after authentication.
	RedirectURI string `json:"redirect_uri"`

	// ExpiresAt is when this state expires.
	ExpiresAt time.Time `json:"expires_at"`

	// OIDCNonce is the OIDC nonce sent to the IdP in the authorize request. It
	// is matched against the `nonce` claim in the returned ID token for replay
	// protection (OIDC core 3.1.2.7). Distinct from Nonce, which is the CSRF
	// state token.
	OIDCNonce string `json:"oidc_nonce,omitempty"`

	// CodeVerifier is the PKCE code verifier (optional, for PKCE flow).
	CodeVerifier string `json:"code_verifier,omitempty"`

	// LinkUserID is set when linking to an existing user account.
	LinkUserID string `json:"link_user_id,omitempty"`
}

// Validate validates a FederationCallbackState for correctness.
func (s *FederationCallbackState) Validate() error {
	if s.Nonce == "" {
		return NewIAMError(ErrFederationStateInvalid, "iam.federation.state.validate", "nonce is required", nil)
	}
	if s.ProviderID == "" {
		return NewIAMError(ErrFederationStateInvalid, "iam.federation.state.validate", "provider ID is required", nil)
	}
	if s.TenantID == "" {
		return NewIAMError(ErrFederationStateInvalid, "iam.federation.state.validate", "tenant ID is required", nil)
	}
	if s.RedirectURI == "" {
		return NewIAMError(ErrFederationStateInvalid, "iam.federation.state.validate", "redirect URI is required", nil)
	}
	return nil
}

// IsExpired returns true if the state has expired.
func (s *FederationCallbackState) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// LoginState is an alias for FederationCallbackState for storage compatibility.
type LoginState = FederationCallbackState
