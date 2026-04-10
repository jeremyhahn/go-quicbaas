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

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// Client is the main IAM client that provides access to all IAM sub-clients.
//
// The client composes all IAM functionality into a single interface:
//   - Tenant management (multi-tenant architecture)
//   - User management (CRUD, roles, groups)
//   - Group management (user organization)
//   - Role management (permission bundles)
//   - Policy management (ABAC policies)
//   - PEP (Policy Enforcement Point for authorization checks)
//   - App management (OAuth2 applications)
//   - Session management
//   - Audit logging
//   - GDPR compliance operations
//   - Delegated administration
//
// Example usage:
//
//	client := iam.NewClient(transport)
//
//	// Create a tenant
//	tenant, err := client.Tenant().Create(ctx, &iam.CreateTenantRequest{...})
//
//	// Create a user in the tenant
//	user, err := client.User().Create(ctx, tenant.ID, &iam.CreateUserRequest{...})
//
//	// Check authorization
//	result, err := client.PEP().Check(ctx, &iam.CheckRequest{...})
type Client interface {
	// Tenant returns the tenant management client.
	Tenant() TenantClient

	// User returns the user management client.
	User() UserClient

	// Group returns the group management client.
	Group() GroupClient

	// Role returns the role management client.
	Role() RoleClient

	// Policy returns the ABAC policy management client.
	Policy() PolicyClient

	// PEP returns the Policy Enforcement Point client.
	PEP() PEPClient

	// App returns the OAuth2 application management client.
	App() AppClient

	// Session returns the session management client.
	Session() SessionClient

	// Audit returns the audit log client.
	Audit() AuditClient

	// GDPR returns the GDPR compliance client.
	GDPR() GDPRClient

	// Admin returns the delegated administration client.
	Admin() AdminClient

	// Federation methods for identity provider management

	// CreateProvider creates a new external identity provider.
	CreateProvider(ctx context.Context, opts *cli.CreateProviderOptions) (*cli.Provider, error)

	// GetProvider retrieves an identity provider by ID.
	GetProvider(ctx context.Context, tenantID, providerID string) (*cli.Provider, error)

	// UpdateProvider updates an existing identity provider.
	UpdateProvider(ctx context.Context, opts *cli.UpdateProviderOptions) (*cli.Provider, error)

	// DeleteProvider removes an identity provider.
	DeleteProvider(ctx context.Context, tenantID, providerID string) error

	// ListProviders lists all identity providers for a tenant.
	ListProviders(ctx context.Context, tenantID string) ([]*cli.Provider, error)

	// Identity methods

	// LinkIdentity links an external identity to a user.
	LinkIdentity(ctx context.Context, opts *cli.LinkIdentityOptions) (*cli.FederatedIdentity, error)

	// UnlinkIdentity removes an external identity from a user.
	UnlinkIdentity(ctx context.Context, tenantID, userID, providerID string) error

	// ListUserIdentities lists all linked identities for a user.
	ListUserIdentities(ctx context.Context, tenantID, userID string) ([]*cli.FederatedIdentity, error)

	// MFA methods

	// GetMFAStatus retrieves the MFA status for a user.
	GetMFAStatus(ctx context.Context, tenantID, userID string) (*cli.MFAStatus, error)

	// BeginMFAEnrollment starts the MFA enrollment process for a user.
	BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (*cli.MFAEnrollment, error)

	// CompleteMFAEnrollment completes MFA enrollment by verifying a code.
	CompleteMFAEnrollment(ctx context.Context, enrollmentID, code string) error

	// DisableMFA disables MFA for a user.
	DisableMFA(ctx context.Context, tenantID, userID string) error

	// GetBackupCodeStatus retrieves the backup code status for a user.
	GetBackupCodeStatus(ctx context.Context, tenantID, userID string) (*cli.BackupCodeStatus, error)

	// GenerateBackupCodes generates new backup codes for a user.
	GenerateBackupCodes(ctx context.Context, tenantID, userID string) ([]string, error)

	// Close closes the client and releases any resources.
	Close() error
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
	tenant    TenantClient
	user      UserClient
	group     GroupClient
	role      RoleClient
	policy    PolicyClient
	pep       PEPClient
	app       AppClient
	session   SessionClient
	audit     AuditClient
	gdpr      GDPRClient
	admin     AdminClient
}

// NewClient creates a new IAM client.
//
// The client provides access to all IAM functionality through sub-clients.
// Returns nil if the transport is nil.
//
// Example:
//
//	t := transport.NewHTTPTransport("http://localhost:8080")
//	client := iam.NewClient(t)
//
//	// Use the client to manage IAM resources
//	tenants, err := client.Tenant().List(ctx, &iam.ListTenantsRequest{})
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}

	return &client{
		transport: t,
		tenant:    NewTenantClient(t),
		user:      NewUserClient(t),
		group:     NewGroupClient(t),
		role:      NewRoleClient(t),
		policy:    NewPolicyClient(t),
		pep:       NewPEPClient(t),
		app:       NewAppClient(t),
		session:   NewSessionClient(t),
		audit:     NewAuditClient(t),
		gdpr:      NewGDPRClient(t),
		admin:     NewAdminClient(t),
	}
}

// Tenant returns the tenant management client.
func (c *client) Tenant() TenantClient {
	return c.tenant
}

// User returns the user management client.
func (c *client) User() UserClient {
	return c.user
}

// Group returns the group management client.
func (c *client) Group() GroupClient {
	return c.group
}

// Role returns the role management client.
func (c *client) Role() RoleClient {
	return c.role
}

// Policy returns the ABAC policy management client.
func (c *client) Policy() PolicyClient {
	return c.policy
}

// PEP returns the Policy Enforcement Point client.
func (c *client) PEP() PEPClient {
	return c.pep
}

// App returns the OAuth2 application management client.
func (c *client) App() AppClient {
	return c.app
}

// Session returns the session management client.
func (c *client) Session() SessionClient {
	return c.session
}

// Audit returns the audit log client.
func (c *client) Audit() AuditClient {
	return c.audit
}

// GDPR returns the GDPR compliance client.
func (c *client) GDPR() GDPRClient {
	return c.gdpr
}

// Admin returns the delegated administration client.
func (c *client) Admin() AdminClient {
	return c.admin
}

const federationServiceName = "quicbaas.IAM.Federation"

// getProviderRequest is the request for GetProvider.
type getProviderRequest struct {
	TenantID   string `json:"tenant_id"`
	ProviderID string `json:"provider_id"`
}

// deleteProviderRequest is the request for DeleteProvider.
type deleteProviderRequest struct {
	TenantID   string `json:"tenant_id"`
	ProviderID string `json:"provider_id"`
}

// listProvidersRequest is the request for ListProviders.
type listProvidersRequest struct {
	TenantID string `json:"tenant_id"`
}

// CreateProvider creates a new external identity provider.
func (c *client) CreateProvider(ctx context.Context, opts *cli.CreateProviderOptions) (*cli.Provider, error) {
	if opts == nil {
		return nil, errors.NewInvalidInput("iam.federation.CreateProvider", "options cannot be nil")
	}
	if opts.TenantID == "" {
		return nil, errors.NewInvalidInput("iam.federation.CreateProvider", "tenant ID is required")
	}

	resp, err := c.transport.Request(ctx, federationServiceName, "CreateProvider", opts)
	if err != nil {
		return nil, err
	}

	if provider, ok := resp.(*cli.Provider); ok {
		return provider, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[cli.Provider](resp, "iam.federation.CreateProvider")
}

// GetProvider retrieves an identity provider by ID.
func (c *client) GetProvider(ctx context.Context, tenantID, providerID string) (*cli.Provider, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.federation.GetProvider", "tenant ID is required")
	}
	if providerID == "" {
		return nil, errors.NewInvalidInput("iam.federation.GetProvider", "provider ID is required")
	}

	resp, err := c.transport.Request(ctx, federationServiceName, "GetProvider", &getProviderRequest{
		TenantID:   tenantID,
		ProviderID: providerID,
	})
	if err != nil {
		return nil, err
	}

	if provider, ok := resp.(*cli.Provider); ok {
		return provider, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[cli.Provider](resp, "iam.federation.GetProvider")
}

// UpdateProvider updates an existing identity provider.
func (c *client) UpdateProvider(ctx context.Context, opts *cli.UpdateProviderOptions) (*cli.Provider, error) {
	if opts == nil {
		return nil, errors.NewInvalidInput("iam.federation.UpdateProvider", "options cannot be nil")
	}
	if opts.TenantID == "" {
		return nil, errors.NewInvalidInput("iam.federation.UpdateProvider", "tenant ID is required")
	}
	if opts.ProviderID == "" {
		return nil, errors.NewInvalidInput("iam.federation.UpdateProvider", "provider ID is required")
	}

	resp, err := c.transport.Request(ctx, federationServiceName, "UpdateProvider", opts)
	if err != nil {
		return nil, err
	}

	if provider, ok := resp.(*cli.Provider); ok {
		return provider, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[cli.Provider](resp, "iam.federation.UpdateProvider")
}

// DeleteProvider removes an identity provider.
func (c *client) DeleteProvider(ctx context.Context, tenantID, providerID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.federation.DeleteProvider", "tenant ID is required")
	}
	if providerID == "" {
		return errors.NewInvalidInput("iam.federation.DeleteProvider", "provider ID is required")
	}

	_, err := c.transport.Request(ctx, federationServiceName, "DeleteProvider", &deleteProviderRequest{
		TenantID:   tenantID,
		ProviderID: providerID,
	})
	return err
}

// ListProviders lists all identity providers for a tenant.
func (c *client) ListProviders(ctx context.Context, tenantID string) ([]*cli.Provider, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.federation.ListProviders", "tenant ID is required")
	}

	resp, err := c.transport.Request(ctx, federationServiceName, "ListProviders", &listProvidersRequest{
		TenantID: tenantID,
	})
	if err != nil {
		return nil, err
	}

	if providers, ok := resp.([]*cli.Provider); ok {
		return providers, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	result, err := convertMapResponse[[]*cli.Provider](resp, "iam.federation.ListProviders")
	if err != nil {
		return nil, err
	}
	return *result, nil
}

const identityServiceName = "quicbaas.IAM.Identity"

// unlinkIdentityRequest is the request for UnlinkIdentity.
type unlinkIdentityRequest struct {
	TenantID   string `json:"tenant_id"`
	UserID     string `json:"user_id"`
	ProviderID string `json:"provider_id"`
}

// listUserIdentitiesRequest is the request for ListUserIdentities.
type listUserIdentitiesRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

// LinkIdentity links an external identity to a user.
func (c *client) LinkIdentity(ctx context.Context, opts *cli.LinkIdentityOptions) (*cli.FederatedIdentity, error) {
	if opts == nil {
		return nil, errors.NewInvalidInput("iam.identity.LinkIdentity", "options cannot be nil")
	}
	if opts.TenantID == "" {
		return nil, errors.NewInvalidInput("iam.identity.LinkIdentity", "tenant ID is required")
	}
	if opts.UserID == "" {
		return nil, errors.NewInvalidInput("iam.identity.LinkIdentity", "user ID is required")
	}

	resp, err := c.transport.Request(ctx, identityServiceName, "LinkIdentity", opts)
	if err != nil {
		return nil, err
	}

	if identity, ok := resp.(*cli.FederatedIdentity); ok {
		return identity, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[cli.FederatedIdentity](resp, "iam.identity.LinkIdentity")
}

// UnlinkIdentity removes an external identity from a user.
func (c *client) UnlinkIdentity(ctx context.Context, tenantID, userID, providerID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.identity.UnlinkIdentity", "tenant ID is required")
	}
	if userID == "" {
		return errors.NewInvalidInput("iam.identity.UnlinkIdentity", "user ID is required")
	}
	if providerID == "" {
		return errors.NewInvalidInput("iam.identity.UnlinkIdentity", "provider ID is required")
	}

	_, err := c.transport.Request(ctx, identityServiceName, "UnlinkIdentity", &unlinkIdentityRequest{
		TenantID:   tenantID,
		UserID:     userID,
		ProviderID: providerID,
	})
	return err
}

// ListUserIdentities lists all linked identities for a user.
func (c *client) ListUserIdentities(ctx context.Context, tenantID, userID string) ([]*cli.FederatedIdentity, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.identity.ListUserIdentities", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.identity.ListUserIdentities", "user ID is required")
	}

	resp, err := c.transport.Request(ctx, identityServiceName, "ListUserIdentities", &listUserIdentitiesRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}

	if identities, ok := resp.([]*cli.FederatedIdentity); ok {
		return identities, nil
	}

	// Handle []interface{} response (array directly from transport).
	if arr, ok := resp.([]interface{}); ok {
		wrapped := map[string]interface{}{"items": arr}
		type identityList struct {
			Items []*cli.FederatedIdentity `json:"items"`
		}
		result, err := convertMapResponse[identityList](wrapped, "iam.identity.ListUserIdentities")
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	}

	// Handle map[string]interface{} wrapper (e.g., {"identities": [...]}).
	if respMap, ok := resp.(map[string]interface{}); ok {
		if arr, ok := respMap["identities"].([]interface{}); ok {
			wrapped := map[string]interface{}{"items": arr}
			type identityList struct {
				Items []*cli.FederatedIdentity `json:"items"`
			}
			result, err := convertMapResponse[identityList](wrapped, "iam.identity.ListUserIdentities")
			if err != nil {
				return nil, err
			}
			return result.Items, nil
		}
		// If no identities key, try to unmarshal the map as-is (empty response).
		if links, ok := respMap["links"].([]interface{}); ok {
			wrapped := map[string]interface{}{"items": links}
			type identityList struct {
				Items []*cli.FederatedIdentity `json:"items"`
			}
			result, err := convertMapResponse[identityList](wrapped, "iam.identity.ListUserIdentities")
			if err != nil {
				return nil, err
			}
			return result.Items, nil
		}
	}

	// Fallback: try direct conversion.
	result, err := convertMapResponse[[]*cli.FederatedIdentity](resp, "iam.identity.ListUserIdentities")
	if err != nil {
		// Return empty list instead of error for empty responses.
		return []*cli.FederatedIdentity{}, nil
	}
	return *result, nil
}

const mfaServiceName = "quicbaas.IAM.MFA"

// getMFAStatusRequest is the request for GetMFAStatus.
type getMFAStatusRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

// beginMFAEnrollmentRequest is the request for BeginMFAEnrollment.
type beginMFAEnrollmentRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Method   string `json:"method"`
}

// completeMFAEnrollmentRequest is the request for CompleteMFAEnrollment.
type completeMFAEnrollmentRequest struct {
	EnrollmentID string `json:"enrollment_id"`
	Code         string `json:"code"`
}

// GetMFAStatus retrieves the MFA status for a user.
func (c *client) GetMFAStatus(ctx context.Context, tenantID, userID string) (*cli.MFAStatus, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.mfa.GetMFAStatus", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.mfa.GetMFAStatus", "user ID is required")
	}

	resp, err := c.transport.Request(ctx, mfaServiceName, "GetMFAStatus", &getMFAStatusRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}

	// If already the expected type, return directly.
	if status, ok := resp.(*cli.MFAStatus); ok {
		return status, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[cli.MFAStatus](resp, "iam.mfa.GetMFAStatus")
}

// BeginMFAEnrollment starts the MFA enrollment process for a user.
func (c *client) BeginMFAEnrollment(ctx context.Context, tenantID, userID, method string) (*cli.MFAEnrollment, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.mfa.BeginMFAEnrollment", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.mfa.BeginMFAEnrollment", "user ID is required")
	}

	resp, err := c.transport.Request(ctx, mfaServiceName, "BeginMFAEnrollment", &beginMFAEnrollmentRequest{
		TenantID: tenantID,
		UserID:   userID,
		Method:   method,
	})
	if err != nil {
		return nil, err
	}

	// If already the expected type, return directly.
	if enrollment, ok := resp.(*cli.MFAEnrollment); ok {
		return enrollment, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[cli.MFAEnrollment](resp, "iam.mfa.BeginMFAEnrollment")
}

// CompleteMFAEnrollment completes MFA enrollment by verifying a code.
func (c *client) CompleteMFAEnrollment(ctx context.Context, enrollmentID, code string) error {
	if enrollmentID == "" {
		return errors.NewInvalidInput("iam.mfa.CompleteMFAEnrollment", "enrollment ID is required")
	}
	if code == "" {
		return errors.NewInvalidInput("iam.mfa.CompleteMFAEnrollment", "code is required")
	}

	_, err := c.transport.Request(ctx, mfaServiceName, "CompleteMFAEnrollment", &completeMFAEnrollmentRequest{
		EnrollmentID: enrollmentID,
		Code:         code,
	})
	return err
}

// disableMFARequest is the request for DisableMFA.
type disableMFARequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

// DisableMFA disables MFA for a user.
func (c *client) DisableMFA(ctx context.Context, tenantID, userID string) error {
	if tenantID == "" {
		return errors.NewInvalidInput("iam.mfa.DisableMFA", "tenant ID is required")
	}
	if userID == "" {
		return errors.NewInvalidInput("iam.mfa.DisableMFA", "user ID is required")
	}

	_, err := c.transport.Request(ctx, mfaServiceName, "DisableMFA", &disableMFARequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	return err
}

// GetBackupCodeStatus retrieves the backup code status for a user.
func (c *client) GetBackupCodeStatus(ctx context.Context, tenantID, userID string) (*cli.BackupCodeStatus, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.mfa.GetBackupCodeStatus", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.mfa.GetBackupCodeStatus", "user ID is required")
	}

	resp, err := c.transport.Request(ctx, mfaServiceName, "GetBackupCodeStatus", &getMFAStatusRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}

	// If already the expected type, return directly.
	if status, ok := resp.(*cli.BackupCodeStatus); ok {
		return status, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[cli.BackupCodeStatus](resp, "iam.mfa.GetBackupCodeStatus")
}

// GenerateBackupCodes generates new backup codes for a user.
func (c *client) GenerateBackupCodes(ctx context.Context, tenantID, userID string) ([]string, error) {
	if tenantID == "" {
		return nil, errors.NewInvalidInput("iam.mfa.GenerateBackupCodes", "tenant ID is required")
	}
	if userID == "" {
		return nil, errors.NewInvalidInput("iam.mfa.GenerateBackupCodes", "user ID is required")
	}

	resp, err := c.transport.Request(ctx, mfaServiceName, "GenerateBackupCodes", &getMFAStatusRequest{
		TenantID: tenantID,
		UserID:   userID,
	})
	if err != nil {
		return nil, err
	}

	if codes, ok := resp.([]string); ok {
		return codes, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	if respMap, ok := resp.(map[string]interface{}); ok {
		if codesRaw, ok := respMap["codes"]; ok {
			switch c := codesRaw.(type) {
			case []string:
				return c, nil
			case []interface{}:
				codes := make([]string, 0, len(c))
				for _, item := range c {
					if s, ok := item.(string); ok {
						codes = append(codes, s)
					}
				}
				return codes, nil
			}
		}
	}

	// Handle []interface{} response directly.
	if rawCodes, ok := resp.([]interface{}); ok {
		codes := make([]string, 0, len(rawCodes))
		for _, item := range rawCodes {
			if s, ok := item.(string); ok {
				codes = append(codes, s)
			}
		}
		return codes, nil
	}

	return nil, errors.NewInternal("iam.mfa.GenerateBackupCodes", "unexpected response type")
}

// Close closes the client and releases any resources.
func (c *client) Close() error {
	// The transport is shared and doesn't need to be closed by this client.
	// Individual sub-clients don't hold resources that need closing.
	return nil
}
