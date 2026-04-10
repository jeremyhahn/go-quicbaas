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

//go:build iam

package service

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// IAM interfaces for service layer - these match the interfaces expected by pkg/api/rest.

// TenantManager interface defines tenant management operations.
type TenantManager interface {
	CreateTenant(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error)
	GetTenant(ctx context.Context, tenantID string) (*iam.Tenant, error)
	UpdateTenant(ctx context.Context, tenantID string, opts iam.UpdateTenantOptions) (*iam.Tenant, error)
	DeleteTenant(ctx context.Context, tenantID string) error
	ListTenants(ctx context.Context, opts iam.ListTenantsOptions) ([]*iam.Tenant, string, error)
}

// IdentityStore interface defines identity management operations.
type IdentityStore interface {
	// User operations
	CreateUser(ctx context.Context, tenantID string, opts iam.CreateUserOptions) (*iam.User, error)
	GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error)
	UpdateUser(ctx context.Context, tenantID, userID string, opts iam.UpdateUserOptions) (*iam.User, error)
	DeleteUser(ctx context.Context, tenantID, userID string) error
	ListUsers(ctx context.Context, tenantID string, opts iam.ListUsersOptions) ([]*iam.User, string, error)
	AssignRole(ctx context.Context, tenantID, userID, roleID string) error
	RevokeRole(ctx context.Context, tenantID, userID, roleID string) error

	// Group operations
	CreateGroup(ctx context.Context, tenantID string, opts iam.CreateGroupOptions) (*iam.Group, error)
	GetGroup(ctx context.Context, tenantID, groupID string) (*iam.Group, error)
	UpdateGroup(ctx context.Context, tenantID, groupID string, opts iam.UpdateGroupOptions) (*iam.Group, error)
	DeleteGroup(ctx context.Context, tenantID, groupID string) error
	ListGroups(ctx context.Context, tenantID string, opts iam.ListGroupsOptions) ([]*iam.Group, string, error)
	AddMember(ctx context.Context, tenantID, groupID, userID string) error
	RemoveMember(ctx context.Context, tenantID, groupID, userID string) error

	// Role operations
	CreateRole(ctx context.Context, tenantID string, opts iam.CreateRoleOptions) (*iam.Role, error)
	GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error)
	UpdateRole(ctx context.Context, tenantID, roleID string, opts iam.UpdateRoleOptions) (*iam.Role, error)
	DeleteRole(ctx context.Context, tenantID, roleID string) error
	ListRoles(ctx context.Context, tenantID string, opts iam.ListRolesOptions) ([]*iam.Role, string, error)
}

// PolicyEngine interface defines policy management and evaluation operations.
// This interface matches the concrete *iam.PolicyEngine implementation.
type PolicyEngine interface {
	CreatePolicy(ctx context.Context, tenantID string, policy *iam.Policy) error
	GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error)
	UpdatePolicy(ctx context.Context, tenantID string, policy *iam.Policy) error
	DeletePolicy(ctx context.Context, tenantID, policyName string) error
	ListPolicies(ctx context.Context, tenantID string, opts iam.ListOptions) ([]*iam.Policy, error)
	ValidatePolicy(policy *iam.Policy) error
	Evaluate(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error)
}

// AppManager interface defines OAuth2 application management operations.
type AppManager interface {
	RegisterApp(ctx context.Context, tenantID string, opts iam.RegisterAppOptions) (*iam.Application, error)
	GetApp(ctx context.Context, tenantID, clientID string) (*iam.Application, error)
	UpdateApp(ctx context.Context, tenantID, clientID string, opts iam.UpdateAppOptions) (*iam.Application, error)
	DeleteApp(ctx context.Context, tenantID, clientID string) error
	ListApps(ctx context.Context, tenantID string, opts iam.ListAppsOptions) ([]*iam.Application, string, error)
	RotateSecret(ctx context.Context, tenantID, clientID string) (*iam.Application, error)
	ValidateClientCredentials(ctx context.Context, clientID, clientSecret string) (bool, error)
}

// OIDCProvider interface defines OIDC operations.
type OIDCProvider interface {
	GetDiscoveryDocument(ctx context.Context) (map[string]interface{}, error)
	GetJWKS(ctx context.Context) (map[string]interface{}, error)
	Authorize(ctx context.Context, req *iam.AuthorizeRequest) (*iam.AuthorizeResponse, error)
	Token(ctx context.Context, req *iam.TokenRequest) (*iam.TokenResponse, error)
	UserInfo(ctx context.Context, accessToken string) (map[string]interface{}, error)
	RevokeToken(ctx context.Context, token string, tokenTypeHint string) error
}

// WebAuthnManager interface defines WebAuthn operations.
type WebAuthnManager interface {
	BeginRegistration(ctx context.Context, tenantID, userID string) (map[string]interface{}, error)
	FinishRegistration(ctx context.Context, tenantID, userID string, credential map[string]interface{}) error
	BeginLogin(ctx context.Context, tenantID, email string) (map[string]interface{}, error)
	FinishLogin(ctx context.Context, tenantID string, credential map[string]interface{}) (*iam.User, error)
}

// MFAManager interface defines MFA (Multi-Factor Authentication) operations.
type MFAManager interface {
	// Enrollment operations
	BeginEnrollment(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error)
	CompleteEnrollment(ctx context.Context, enrollmentID, code string) error
	CancelEnrollment(ctx context.Context, enrollmentID string) error

	// Challenge/verification operations
	BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error)
	VerifyChallenge(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error)
	VerifyCode(ctx context.Context, userID, tenantID, code string) error

	// Status operations
	GetMFAStatus(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error)
	IsMFARequired(ctx context.Context, userID, tenantID string) (bool, error)
	DisableMFA(ctx context.Context, userID, tenantID string) error

	// Backup code operations
	GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error)
	VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error
	GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error)
}

// SessionManager interface for session management operations.
// This interface matches what REST handlers need for session CRUD operations.
type SessionManager interface {
	// Create creates a new session
	Create(ctx context.Context, session *iam.SessionExt) error
	// List lists all sessions for a user in a tenant
	List(ctx context.Context, userID, tenantID string) ([]*iam.SessionExt, error)
	// Get retrieves a session by ID
	Get(ctx context.Context, sessionID string) (*iam.SessionExt, error)
	// Revoke revokes a specific session
	Revoke(ctx context.Context, sessionID string) error
	// RevokeAllForUser revokes all sessions for a user
	RevokeAllForUser(ctx context.Context, userID, tenantID string) error
	// RevokeAllExcept revokes all sessions for a user except the specified one
	RevokeAllExcept(ctx context.Context, userID, tenantID, exceptSessionID string) error
}

// AuthManagerInterface defines authentication operations for password-based login.
// This is used when AllowPasswordAuth is enabled for testing/development.
type AuthManagerInterface interface {
	// Authenticate validates credentials and issues authentication tokens.
	// Only works when AllowPasswordAuth is enabled.
	Authenticate(ctx context.Context, username, password string) (*auth.Token, error)

	// ValidateToken validates a token and returns validation information.
	ValidateToken(ctx context.Context, token string) (*auth.TokenValidationResult, error)

	// RefreshToken generates a new access token from a refresh token.
	RefreshToken(ctx context.Context, refreshToken string) (*auth.Token, error)

	// RevokeToken revokes an authentication token.
	RevokeToken(ctx context.Context, token string) error

	// CreateUser creates a new user with specified roles.
	CreateUser(username, password string, roles []string) (*auth.User, error)

	// IsPasswordAuthEnabled returns whether password authentication is enabled.
	IsPasswordAuthEnabled() bool
}

// IAMComponents aggregates all IAM component interfaces for the service layer.
// This interface is implemented by the server package to provide IAM functionality.
type IAMComponents interface {
	// GetTenantManager returns the tenant manager.
	GetTenantManager() iam.TenantManager
	// GetIdentityStore returns the identity store.
	GetIdentityStore() *iam.IdentityStore
	// GetPolicyEngine returns the policy engine.
	GetPolicyEngine() *iam.PolicyEngine
	// GetAppManager returns the app manager.
	GetAppManager() *iam.AppManager
	// GetOIDCProvider returns the OIDC provider.
	GetOIDCProvider() *iam.OIDCProvider
	// GetFederationManager returns the federation manager for external identity providers.
	GetFederationManager() iam.FederationManager
	// GetMFAManager returns the MFA manager.
	GetMFAManager() MFAManager
	// GetWebAuthnManager returns the WebAuthn manager.
	GetWebAuthnManager() WebAuthnManager
	// GetAuditLogger returns the audit logger.
	GetAuditLogger() iam.AuditLogger
	// GetSessionStorage returns the session storage.
	GetSessionStorage() iam.SessionStorage
	// GetSessionManager returns the session manager.
	GetSessionManager() iam.SessionManager
	// GetAdminManager returns the admin manager for delegated administration.
	GetAdminManager() *iam.AdminManager
	// GetGDPRManager returns the GDPR compliance manager.
	GetGDPRManager() *iam.GDPRManager
	// GetAuthManager returns the authentication manager for password-based login.
	// Returns nil if password authentication is not configured.
	GetAuthManager() AuthManagerInterface
	// GetInternalAuthManager returns the interfaces.AuthManager for internal service use.
	// This is used to wire up the auth manager in the QuicBaaS service for MCP transport
	// and other internal operations that require the full interfaces.AuthManager interface.
	// Returns nil if password authentication is not configured.
	GetInternalAuthManager() interfaces.AuthManager
}

// IAMServiceProvider provides access to IAM components through the service layer.
// This interface enables testing and mocking of IAM functionality.
type IAMServiceProvider interface {
	// GetTenantManager returns the tenant manager.
	GetTenantManager(ctx context.Context) (iam.TenantManager, error)
	// GetIdentityStore returns the identity store.
	GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	// GetPolicyEngine returns the policy engine.
	GetPolicyEngine(ctx context.Context) (*iam.PolicyEngine, error)
	// GetAppManager returns the app manager.
	GetAppManager(ctx context.Context) (*iam.AppManager, error)
	// GetOIDCProvider returns the OIDC provider.
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	// GetFederationManager returns the federation manager.
	GetFederationManager(ctx context.Context) (iam.FederationManager, error)
	// GetMFAManager returns the MFA manager.
	GetMFAManager(ctx context.Context) (MFAManager, error)
	// GetWebAuthnManager returns the WebAuthn manager.
	GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	// GetAuditLogger returns the audit logger.
	GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	// GetSessionStorage returns the session storage.
	GetSessionStorage(ctx context.Context) (iam.SessionStorage, error)
	// GetSessionManager returns the session manager.
	GetSessionManager(ctx context.Context) (SessionManager, error)
	// GetAdminManager returns the admin manager.
	GetAdminManager(ctx context.Context) (*iam.AdminManager, error)
	// GetGDPRManager returns the GDPR manager.
	GetGDPRManager(ctx context.Context) (*iam.GDPRManager, error)
	// GetAuthManager returns the authentication manager.
	GetAuthManager(ctx context.Context) (AuthManagerInterface, error)
}
