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
	"context"
	"encoding/json"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	appServiceName = "quicbaas.IAM.App"
)

// AppClient provides OAuth2 application management operations.
//
// The client supports the full OAuth2 application lifecycle:
//   - Application registration with configurable grant types
//   - Client credential management and rotation
//   - Application metadata updates
//   - Application deregistration
type AppClient interface {
	// Register creates a new OAuth2 application.
	Register(ctx context.Context, tenantID string, req *RegisterAppRequest) (*Application, error)

	// Get retrieves an application by client ID.
	Get(ctx context.Context, tenantID, clientID string) (*Application, error)

	// Update modifies an existing application.
	Update(ctx context.Context, tenantID, clientID string, req *UpdateAppRequest) (*Application, error)

	// Delete removes an application.
	Delete(ctx context.Context, tenantID, clientID string) error

	// List returns all applications for a tenant.
	List(ctx context.Context, tenantID string) (*ListAppsResponse, error)

	// RotateSecret generates a new client secret for an application.
	RotateSecret(ctx context.Context, tenantID, clientID string) (*RotateSecretResponse, error)
}

// GrantType represents an OAuth2 grant type.
type GrantType int

const (
	// GrantTypeAuthorizationCode is the authorization code flow.
	GrantTypeAuthorizationCode GrantType = iota
	// GrantTypeClientCredentials is the client credentials flow.
	GrantTypeClientCredentials
	// GrantTypeRefreshToken allows token refresh.
	GrantTypeRefreshToken
	// GrantTypeDeviceCode is the device authorization flow.
	GrantTypeDeviceCode
	// GrantTypeImplicit is the implicit flow (deprecated).
	GrantTypeImplicit
	// GrantTypePassword is the resource owner password flow (deprecated).
	GrantTypePassword
)

// String returns the string representation of the grant type.
func (g GrantType) String() string {
	switch g {
	case GrantTypeAuthorizationCode:
		return "authorization_code"
	case GrantTypeClientCredentials:
		return "client_credentials"
	case GrantTypeRefreshToken:
		return "refresh_token"
	case GrantTypeDeviceCode:
		return "urn:ietf:params:oauth:grant-type:device_code"
	case GrantTypeImplicit:
		return "implicit"
	case GrantTypePassword:
		return "password"
	default:
		return "unknown"
	}
}

// ParseGrantType parses a string into a GrantType.
func ParseGrantType(s string) GrantType {
	switch s {
	case "authorization_code":
		return GrantTypeAuthorizationCode
	case "client_credentials":
		return GrantTypeClientCredentials
	case "refresh_token":
		return GrantTypeRefreshToken
	case "urn:ietf:params:oauth:grant-type:device_code":
		return GrantTypeDeviceCode
	case "implicit":
		return GrantTypeImplicit
	case "password":
		return GrantTypePassword
	default:
		return GrantTypeAuthorizationCode
	}
}

// MarshalJSON serializes GrantType as its string representation.
func (g GrantType) MarshalJSON() ([]byte, error) {
	return json.Marshal(g.String())
}

// UnmarshalJSON deserializes GrantType from both string and numeric JSON values.
// The REST server returns string values like "client_credentials", while gRPC
// uses integer enum values.
func (g *GrantType) UnmarshalJSON(data []byte) error {
	// Try string first (REST response format)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*g = ParseGrantType(s)
		return nil
	}

	// Fall back to numeric (gRPC/internal format)
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*g = GrantType(n)
		return nil
	}

	return errors.NewInvalidInput("GrantType.UnmarshalJSON", "invalid grant type value: "+string(data))
}

// TokenEndpointAuthMethod represents the client authentication method.
type TokenEndpointAuthMethod int

const (
	// TokenEndpointAuthClientSecretBasic uses HTTP Basic authentication.
	TokenEndpointAuthClientSecretBasic TokenEndpointAuthMethod = iota
	// TokenEndpointAuthClientSecretPost uses POST body parameters.
	TokenEndpointAuthClientSecretPost
	// TokenEndpointAuthClientSecretJWT uses a signed JWT.
	TokenEndpointAuthClientSecretJWT
	// TokenEndpointAuthPrivateKeyJWT uses a private key JWT.
	TokenEndpointAuthPrivateKeyJWT
	// TokenEndpointAuthNone is for public clients.
	TokenEndpointAuthNone
	// TokenEndpointAuthTLSClientAuth uses PKI-validated mTLS (RFC 8705 §2.1).
	// The client cert chain must validate against the operator CA pool and the
	// cert's Subject DN or SAN dNSName must match the registered client metadata.
	TokenEndpointAuthTLSClientAuth
	// TokenEndpointAuthSelfSignedTLSClientAuth uses a pinned SHA-256 cert
	// thumbprint for mTLS authentication (RFC 8705 §2.2). No CA chain is
	// required; the thumbprint registered at client creation is the binding.
	TokenEndpointAuthSelfSignedTLSClientAuth
)

// String returns the string representation of the auth method.
func (m TokenEndpointAuthMethod) String() string {
	switch m {
	case TokenEndpointAuthClientSecretBasic:
		return "client_secret_basic"
	case TokenEndpointAuthClientSecretPost:
		return "client_secret_post"
	case TokenEndpointAuthClientSecretJWT:
		return "client_secret_jwt"
	case TokenEndpointAuthPrivateKeyJWT:
		return "private_key_jwt"
	case TokenEndpointAuthNone:
		return "none"
	case TokenEndpointAuthTLSClientAuth:
		return "tls_client_auth"
	case TokenEndpointAuthSelfSignedTLSClientAuth:
		return "self_signed_tls_client_auth"
	default:
		return "unknown"
	}
}

// MarshalJSON serializes TokenEndpointAuthMethod as its string representation.
func (m TokenEndpointAuthMethod) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON deserializes TokenEndpointAuthMethod from string or numeric values.
func (m *TokenEndpointAuthMethod) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch s {
		case "client_secret_basic":
			*m = TokenEndpointAuthClientSecretBasic
		case "client_secret_post":
			*m = TokenEndpointAuthClientSecretPost
		case "client_secret_jwt":
			*m = TokenEndpointAuthClientSecretJWT
		case "private_key_jwt":
			*m = TokenEndpointAuthPrivateKeyJWT
		case "none":
			*m = TokenEndpointAuthNone
		case "tls_client_auth":
			*m = TokenEndpointAuthTLSClientAuth
		case "self_signed_tls_client_auth":
			*m = TokenEndpointAuthSelfSignedTLSClientAuth
		default:
			*m = TokenEndpointAuthClientSecretBasic
		}
		return nil
	}

	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*m = TokenEndpointAuthMethod(n)
		return nil
	}

	return errors.NewInvalidInput("TokenEndpointAuthMethod.UnmarshalJSON",
		"invalid auth method value: "+string(data))
}

// ApplicationType represents the type of OAuth2 application.
type ApplicationType int

const (
	// ApplicationTypeWeb is a web application with a server-side component.
	ApplicationTypeWeb ApplicationType = iota
	// ApplicationTypeNative is a native/mobile application.
	ApplicationTypeNative
	// ApplicationTypeSPA is a single-page application.
	ApplicationTypeSPA
	// ApplicationTypeService is a machine-to-machine service.
	ApplicationTypeService
)

// String returns the string representation of the application type.
func (t ApplicationType) String() string {
	switch t {
	case ApplicationTypeWeb:
		return "web"
	case ApplicationTypeNative:
		return "native"
	case ApplicationTypeSPA:
		return "spa"
	case ApplicationTypeService:
		return "service"
	default:
		return "unknown"
	}
}

// MarshalJSON serializes ApplicationType as its string representation.
func (t ApplicationType) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// UnmarshalJSON deserializes ApplicationType from string or numeric values.
func (t *ApplicationType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		switch s {
		case "web":
			*t = ApplicationTypeWeb
		case "native":
			*t = ApplicationTypeNative
		case "spa":
			*t = ApplicationTypeSPA
		case "service":
			*t = ApplicationTypeService
		default:
			*t = ApplicationTypeWeb
		}
		return nil
	}

	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*t = ApplicationType(n)
		return nil
	}

	return errors.NewInvalidInput("ApplicationType.UnmarshalJSON",
		"invalid application type value: "+string(data))
}

// RegisterAppRequest contains the parameters for registering an OAuth2 application.
type RegisterAppRequest struct {
	// Name is the display name of the application.
	Name string `json:"name"`

	// Description is an optional description of the application.
	Description string `json:"description,omitempty"`

	// Type is the application type.
	Type ApplicationType `json:"type,omitempty"`

	// RedirectURIs are the allowed redirect URIs for authorization flows.
	RedirectURIs []string `json:"redirect_uris,omitempty"`

	// ResourcePrefix is the resource namespace this application owns. It scopes
	// the ABAC policies evaluated for tokens issued to the client.
	ResourcePrefix string `json:"resource_prefix,omitempty"`

	// PostLogoutRedirectURIs are the allowed URIs for post-logout redirects.
	PostLogoutRedirectURIs []string `json:"post_logout_redirect_uris,omitempty"`

	// GrantTypes are the allowed OAuth2 grant types.
	GrantTypes []GrantType `json:"grant_types,omitempty"`

	// ResponseTypes are the allowed OAuth2 response types.
	ResponseTypes []string `json:"response_types,omitempty"`

	// Scopes are the allowed OAuth2 scopes.
	Scopes []string `json:"scopes,omitempty"`

	// TokenEndpointAuthMethod is the client authentication method.
	TokenEndpointAuthMethod TokenEndpointAuthMethod `json:"token_endpoint_auth_method,omitempty"`

	// Contacts are the contact emails for the application.
	Contacts []string `json:"contacts,omitempty"`

	// LogoURI is the URL to the application logo.
	LogoURI string `json:"logo_uri,omitempty"`

	// PolicyURI is the URL to the application privacy policy.
	PolicyURI string `json:"policy_uri,omitempty"`

	// TOSURI is the URL to the application terms of service.
	TOSURI string `json:"tos_uri,omitempty"`

	// JWKSURI is the URL to the application's JWKS.
	JWKSURI string `json:"jwks_uri,omitempty"`

	// JWKS is the inline JWKS for the application.
	JWKS string `json:"jwks,omitempty"`

	// AccessTokenTTL is the access token lifetime.
	AccessTokenTTL time.Duration `json:"access_token_ttl,omitempty"`

	// RefreshTokenTTL is the refresh token lifetime.
	RefreshTokenTTL time.Duration `json:"refresh_token_ttl,omitempty"`

	// IDTokenTTL is the ID token lifetime.
	IDTokenTTL time.Duration `json:"id_token_ttl,omitempty"`

	// MTLSCertThumbprint is the SHA-256 thumbprint of the client certificate
	// for self_signed_tls_client_auth registration (RFC 8705 §2.2).
	// Mutually exclusive with MTLSSubjectDN and MTLSSANDNS.
	// Set by the CLI when --cert-pem is provided; the thumbprint is computed
	// locally and sent instead of the raw PEM.
	MTLSCertThumbprint string `json:"mtls_cert_thumbprint,omitempty"`

	// MTLSSubjectDN is the Subject Distinguished Name used to identify the
	// client for tls_client_auth (RFC 8705 §2.1).
	MTLSSubjectDN string `json:"mtls_subject_dn,omitempty"`

	// MTLSSANDNS is the SAN dNSName used to identify the client for
	// tls_client_auth (RFC 8705 §2.1). Alternative to MTLSSubjectDN.
	MTLSSANDNS string `json:"mtls_san_dns,omitempty"`

	// Metadata contains custom application metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdateAppRequest contains the parameters for updating an OAuth2 application.
type UpdateAppRequest struct {
	// Name is the new display name of the application.
	Name string `json:"name,omitempty"`

	// Description is the new description of the application.
	Description string `json:"description,omitempty"`

	// RedirectURIs are the new allowed redirect URIs.
	RedirectURIs []string `json:"redirect_uris,omitempty"`

	// ResourcePrefix is the new resource namespace for the application.
	ResourcePrefix string `json:"resource_prefix,omitempty"`

	// PostLogoutRedirectURIs are the new allowed post-logout redirect URIs.
	PostLogoutRedirectURIs []string `json:"post_logout_redirect_uris,omitempty"`

	// GrantTypes are the new allowed grant types.
	GrantTypes []GrantType `json:"grant_types,omitempty"`

	// ResponseTypes are the new allowed response types.
	ResponseTypes []string `json:"response_types,omitempty"`

	// Scopes are the new allowed scopes.
	Scopes []string `json:"scopes,omitempty"`

	// TokenEndpointAuthMethod is the new client authentication method.
	TokenEndpointAuthMethod TokenEndpointAuthMethod `json:"token_endpoint_auth_method,omitempty"`

	// Contacts are the new contact emails.
	Contacts []string `json:"contacts,omitempty"`

	// LogoURI is the new logo URL.
	LogoURI string `json:"logo_uri,omitempty"`

	// PolicyURI is the new privacy policy URL.
	PolicyURI string `json:"policy_uri,omitempty"`

	// TOSURI is the new terms of service URL.
	TOSURI string `json:"tos_uri,omitempty"`

	// JWKSURI is the new JWKS URL.
	JWKSURI string `json:"jwks_uri,omitempty"`

	// JWKS is the new inline JWKS.
	JWKS string `json:"jwks,omitempty"`

	// AccessTokenTTL is the new access token lifetime.
	AccessTokenTTL time.Duration `json:"access_token_ttl,omitempty"`

	// RefreshTokenTTL is the new refresh token lifetime.
	RefreshTokenTTL time.Duration `json:"refresh_token_ttl,omitempty"`

	// IDTokenTTL is the new ID token lifetime.
	IDTokenTTL time.Duration `json:"id_token_ttl,omitempty"`

	// Metadata contains updated custom metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Enabled controls whether the application is active.
	Enabled *bool `json:"enabled,omitempty"`

	// MTLSCertThumbprint replaces the pinned SHA-256 cert thumbprint for
	// self_signed_tls_client_auth or tls_client_auth clients (RFC 8705).
	// Used by the pin-cert CLI subcommand to rotate the bound certificate.
	MTLSCertThumbprint string `json:"mtls_cert_thumbprint,omitempty"`
}

// Application represents an OAuth2 application.
type Application struct {
	// ClientID is the unique identifier for the application.
	ClientID string `json:"client_id"`

	// TenantID is the tenant this application belongs to.
	TenantID string `json:"tenant_id"`

	// Name is the display name of the application.
	Name string `json:"name"`

	// Description is the application description.
	Description string `json:"description,omitempty"`

	// Type is the application type.
	Type ApplicationType `json:"type,omitempty"`

	// ClientSecret is the client secret (only returned on creation/rotation).
	ClientSecret string `json:"client_secret,omitempty"`

	// ClientSecretExpiresAt is when the client secret expires.
	ClientSecretExpiresAt time.Time `json:"client_secret_expires_at,omitempty"`

	// RedirectURIs are the allowed redirect URIs.
	RedirectURIs []string `json:"redirect_uris,omitempty"`

	// ResourcePrefix is the resource namespace the application owns.
	ResourcePrefix string `json:"resource_prefix,omitempty"`

	// PostLogoutRedirectURIs are the allowed post-logout redirect URIs.
	PostLogoutRedirectURIs []string `json:"post_logout_redirect_uris,omitempty"`

	// GrantTypes are the allowed grant types.
	GrantTypes []GrantType `json:"grant_types,omitempty"`

	// ResponseTypes are the allowed response types.
	ResponseTypes []string `json:"response_types,omitempty"`

	// Scopes are the allowed scopes.
	Scopes []string `json:"scopes,omitempty"`

	// TokenEndpointAuthMethod is the client authentication method.
	TokenEndpointAuthMethod TokenEndpointAuthMethod `json:"token_endpoint_auth_method,omitempty"`

	// Contacts are the contact emails.
	Contacts []string `json:"contacts,omitempty"`

	// LogoURI is the logo URL.
	LogoURI string `json:"logo_uri,omitempty"`

	// PolicyURI is the privacy policy URL.
	PolicyURI string `json:"policy_uri,omitempty"`

	// TOSURI is the terms of service URL.
	TOSURI string `json:"tos_uri,omitempty"`

	// JWKSURI is the JWKS URL.
	JWKSURI string `json:"jwks_uri,omitempty"`

	// AccessTokenTTL is the access token lifetime.
	AccessTokenTTL time.Duration `json:"access_token_ttl,omitempty"`

	// RefreshTokenTTL is the refresh token lifetime.
	RefreshTokenTTL time.Duration `json:"refresh_token_ttl,omitempty"`

	// IDTokenTTL is the ID token lifetime.
	IDTokenTTL time.Duration `json:"id_token_ttl,omitempty"`

	// Enabled indicates if the application is active.
	Enabled bool `json:"enabled,omitempty"`

	// MTLSCertThumbprint is the SHA-256 thumbprint of the pinned client
	// certificate for self_signed_tls_client_auth clients (RFC 8705 §2.2).
	MTLSCertThumbprint string `json:"mtls_cert_thumbprint,omitempty"`

	// MTLSSubjectDN is the registered Subject DN for tls_client_auth
	// clients (RFC 8705 §2.1).
	MTLSSubjectDN string `json:"mtls_subject_dn,omitempty"`

	// MTLSSANDNS is the registered SAN dNSName for tls_client_auth
	// clients (RFC 8705 §2.1).
	MTLSSANDNS string `json:"mtls_san_dns,omitempty"`

	// Metadata contains custom application metadata.
	Metadata map[string]string `json:"metadata,omitempty"`

	// CreatedAt is when the application was created.
	CreatedAt time.Time `json:"created_at,omitempty"`

	// UpdatedAt is when the application was last updated.
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// ListAppsResponse contains the list of applications.
type ListAppsResponse struct {
	// Applications is the list of OAuth2 applications.
	Applications []Application `json:"applications"`

	// TotalCount is the total number of applications.
	TotalCount int `json:"total_count"`
}

// RotateSecretResponse contains the new client secret.
type RotateSecretResponse struct {
	// ClientID is the application's client ID.
	ClientID string `json:"client_id"`

	// ClientSecret is the new client secret.
	ClientSecret string `json:"client_secret"`

	// ClientSecretExpiresAt is when the new secret expires.
	ClientSecretExpiresAt time.Time `json:"client_secret_expires_at,omitempty"`

	// PreviousSecretValidUntil is when the previous secret stops working.
	PreviousSecretValidUntil time.Time `json:"previous_secret_valid_until,omitempty"`
}

// Internal request/response types for transport.

type registerAppRequest struct {
	TenantID string              `json:"tenant_id"`
	App      *RegisterAppRequest `json:"app"`
}

type getAppRequest struct {
	TenantID string `json:"tenant_id"`
	ClientID string `json:"client_id"`
}

type updateAppRequest struct {
	TenantID string            `json:"tenant_id"`
	ClientID string            `json:"client_id"`
	App      *UpdateAppRequest `json:"app"`
}

type deleteAppRequest struct {
	TenantID string `json:"tenant_id"`
	ClientID string `json:"client_id"`
}

type deleteAppResponse struct{}

type listAppsRequest struct {
	TenantID string `json:"tenant_id"`
}

type rotateSecretRequest struct {
	TenantID string `json:"tenant_id"`
	ClientID string `json:"client_id"`
}

// appClient implements the AppClient interface.
type appClient struct {
	transport transport.Transport
}

// NewAppClient creates a new OAuth2 application management client.
//
// Returns nil if the transport is nil.
func NewAppClient(t transport.Transport) AppClient {
	if t == nil {
		return nil
	}
	return &appClient{
		transport: t,
	}
}

// Register creates a new OAuth2 application.
func (c *appClient) Register(ctx context.Context, tenantID string, req *RegisterAppRequest) (*Application, error) {
	if err := validateRegisterAppRequest(tenantID, req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.app.Register", "context is required")
	}

	resp, err := c.transport.Request(ctx, appServiceName, "Register", &registerAppRequest{
		TenantID: tenantID,
		App:      req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.app.Register", "nil response from server")
	}

	// If already the expected type, return directly.
	if app, ok := resp.(*Application); ok {
		return app, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[Application](resp, "iam.app.Register")
}

// Get retrieves an application by client ID.
func (c *appClient) Get(ctx context.Context, tenantID, clientID string) (*Application, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.app.Get", "tenant ID is required")
	}
	if clientID == "" {
		return nil, errors.NewInvalidInput("iam.app.Get", "client ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.app.Get", "context is required")
	}

	resp, err := c.transport.Request(ctx, appServiceName, "Get", &getAppRequest{
		TenantID: tenantID,
		ClientID: clientID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.app.Get", "application not found")
	}

	// If already the expected type, return directly.
	if app, ok := resp.(*Application); ok {
		return app, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[Application](resp, "iam.app.Get")
}

// Update modifies an existing application.
func (c *appClient) Update(ctx context.Context, tenantID, clientID string, req *UpdateAppRequest) (*Application, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.app.Update", "tenant ID is required")
	}
	if clientID == "" {
		return nil, errors.NewInvalidInput("iam.app.Update", "client ID is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("iam.app.Update", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.app.Update", "context is required")
	}

	resp, err := c.transport.Request(ctx, appServiceName, "Update", &updateAppRequest{
		TenantID: tenantID,
		ClientID: clientID,
		App:      req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.app.Update", "nil response from server")
	}

	// If already the expected type, return directly.
	if app, ok := resp.(*Application); ok {
		return app, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[Application](resp, "iam.app.Update")
}

// Delete removes an application.
func (c *appClient) Delete(ctx context.Context, tenantID, clientID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.app.Delete", "tenant ID is required")
	}
	if clientID == "" {
		return errors.NewInvalidInput("iam.app.Delete", "client ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.app.Delete", "context is required")
	}

	resp, err := c.transport.Request(ctx, appServiceName, "Delete", &deleteAppRequest{
		TenantID: tenantID,
		ClientID: clientID,
	})
	if err != nil {
		return err
	}

	// Accept nil response, *deleteAppResponse, or map response.
	if resp == nil {
		return nil
	}

	// If already the expected type, return.
	if _, ok := resp.(*deleteAppResponse); ok {
		return nil
	}

	// Accept map responses from REST/gRPC transports.
	if _, ok := resp.(map[string]interface{}); ok {
		return nil
	}

	return errors.NewInternal("iam.app.Delete", "invalid response type")
}

// List returns all applications for a tenant.
func (c *appClient) List(ctx context.Context, tenantID string) (*ListAppsResponse, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.app.List", "tenant ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.app.List", "context is required")
	}

	resp, err := c.transport.Request(ctx, appServiceName, "List", &listAppsRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListAppsResponse{Applications: []Application{}}, nil
	}

	// If already the expected type, return directly.
	if listResp, ok := resp.(*ListAppsResponse); ok {
		return listResp, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[ListAppsResponse](resp, "iam.app.List")
}

// RotateSecret generates a new client secret for an application.
func (c *appClient) RotateSecret(ctx context.Context, tenantID, clientID string) (*RotateSecretResponse, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.app.RotateSecret", "tenant ID is required")
	}
	if clientID == "" {
		return nil, errors.NewInvalidInput("iam.app.RotateSecret", "client ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.app.RotateSecret", "context is required")
	}

	resp, err := c.transport.Request(ctx, appServiceName, "RotateSecret", &rotateSecretRequest{
		TenantID: tenantID,
		ClientID: clientID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.app.RotateSecret", "nil response from server")
	}

	// If already the expected type, return directly.
	if rotateResp, ok := resp.(*RotateSecretResponse); ok {
		return rotateResp, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[RotateSecretResponse](resp, "iam.app.RotateSecret")
}

// Validation functions.

func validateRegisterAppRequest(tenantID string, req *RegisterAppRequest) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.app.Register", "tenant ID is required")
	}
	if req == nil {
		return errors.NewInvalidInput("iam.app.Register", "request is required")
	}
	if req.Name == "" {
		return errors.NewInvalidInput("iam.app.Register", "application name is required")
	}
	if len(req.GrantTypes) == 0 {
		return errors.NewInvalidInput("iam.app.Register", "at least one grant type is required")
	}
	// Authorization code and implicit grants require redirect URIs
	for _, gt := range req.GrantTypes {
		if gt == GrantTypeAuthorizationCode || gt == GrantTypeImplicit {
			if len(req.RedirectURIs) == 0 {
				return errors.NewInvalidInput("iam.app.Register", "redirect URIs are required for authorization code or implicit grants")
			}
			break
		}
	}
	return nil
}
