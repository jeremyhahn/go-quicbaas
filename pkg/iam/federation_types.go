//go:build iam

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
	ErrProviderNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.provider",
		Err:  &ProviderNotFoundError{},
	}

	// ErrProviderAlreadyExists is returned when attempting to create a duplicate provider.
	ErrProviderAlreadyExists = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.federation.provider",
		Err:  &ProviderAlreadyExistsError{},
	}

	// ErrProviderInvalid is returned when provider validation fails.
	ErrProviderInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.federation.provider",
		Err:  &ProviderInvalidError{},
	}

	// ErrStateNotFound is returned when a state token is not found.
	ErrStateNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.state",
		Err:  &StateNotFoundError{},
	}

	// ErrStateExpired is returned when a state token has expired.
	ErrStateExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.state",
		Err:  &StateExpiredError{},
	}

	// ErrStateInvalid is returned when state validation fails.
	ErrStateInvalid = &dberrors.DragonError{
		Code: dberrors.ErrValidation,
		Op:   "iam.federation.state",
		Err:  &StateInvalidError{},
	}

	// ErrFederationStateInvalid is an alias for ErrStateInvalid for backward compatibility.
	ErrFederationStateInvalid = ErrStateInvalid

	// ErrTokenExchangeFailed is returned when OAuth token exchange fails.
	ErrTokenExchangeFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.token",
		Err:  &TokenExchangeFailedError{},
	}

	// ErrClaimsExtractionFailed is returned when claims extraction fails.
	ErrClaimsExtractionFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.claims",
		Err:  &ClaimsExtractionFailedError{},
	}

	// ErrIdentityNotLinked is returned when a federated identity is not linked.
	ErrIdentityNotLinked = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.identity",
		Err:  &IdentityNotLinkedError{},
	}

	// ErrIdentityAlreadyLinked is returned when an identity is already linked.
	ErrIdentityAlreadyLinked = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.federation.identity",
		Err:  &IdentityAlreadyLinkedError{},
	}

	// ErrAutoProvisionDisabled is returned when auto-provisioning is disabled.
	ErrAutoProvisionDisabled = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.provision",
		Err:  &AutoProvisionDisabledError{},
	}

	// ErrLinkingDisabled is returned when account linking is disabled.
	ErrLinkingDisabled = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.link",
		Err:  &LinkingDisabledError{},
	}

	// ErrProviderDisabled is returned when a provider is disabled.
	ErrProviderDisabled = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.provider",
		Err:  &ProviderDisabledError{},
	}

	// ErrOIDCDiscoveryFailed is returned when OIDC discovery fails.
	ErrOIDCDiscoveryFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.oidc",
		Err:  &OIDCDiscoveryFailedError{},
	}

	// ErrFederatedIdentityNotFound is returned when a federated identity is not found.
	ErrFederatedIdentityNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.identity",
		Err:  &FederatedIdentityNotFoundError{},
	}

	// ErrFederatedIdentityAlreadyLinked is returned when identity is already linked.
	ErrFederatedIdentityAlreadyLinked = &dberrors.DragonError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.federation.identity",
		Err:  &FederatedIdentityAlreadyLinkedError{},
	}

	// ErrAccountLinkingDisabled is returned when account linking is disabled.
	ErrAccountLinkingDisabled = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.linking",
		Err:  &AccountLinkingDisabledError{},
	}

	// ErrJITProvisioningDisabled is returned when JIT provisioning is disabled.
	ErrJITProvisioningDisabled = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.provisioning",
		Err:  &JITProvisioningDisabledError{},
	}

	// ErrOIDCTokenExchangeFailed is returned when OIDC token exchange fails.
	ErrOIDCTokenExchangeFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.oidc.token",
		Err:  &OIDCTokenExchangeFailedError{},
	}

	// ErrOIDCUserInfoFailed is returned when fetching user info fails.
	ErrOIDCUserInfoFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.oidc.userinfo",
		Err:  &OIDCUserInfoFailedError{},
	}

	// ErrOIDCClaimMappingFailed is returned when claim mapping fails.
	ErrOIDCClaimMappingFailed = &dberrors.DragonError{
		Code: dberrors.ErrInternal,
		Op:   "iam.federation.oidc.claims",
		Err:  &OIDCClaimMappingFailedError{},
	}

	// ErrFederationStateNotFound is returned when callback state is not found.
	ErrFederationStateNotFound = &dberrors.DragonError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.federation.state",
		Err:  &FederationStateNotFoundError{},
	}

	// ErrFederationStateExpired is returned when callback state has expired.
	ErrFederationStateExpired = &dberrors.DragonError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.federation.state",
		Err:  &FederationStateExpiredError{},
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
)

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
