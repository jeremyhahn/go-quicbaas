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

package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// MFAServiceAdapter wraps a service.MFAManager to implement the MFAService
// interface for gRPC. The gRPC wire carries user IDs as decimal strings; the
// manager keys users by uint64, so each verb parses the id before delegating.
// Object verbs (complete, cancel, verify-challenge) name no user on the wire;
// their empty id parses to zero, which binds the object to the tenant only.
type MFAServiceAdapter struct {
	mgr service.MFAManager
}

// NewMFAServiceAdapter creates a new MFA service adapter. It returns nil when
// mgr is not a service.MFAManager.
func NewMFAServiceAdapter(mgr interface{}) *MFAServiceAdapter {
	if svc, ok := mgr.(service.MFAManager); ok {
		return &MFAServiceAdapter{mgr: svc}
	}
	return nil
}

func (a *MFAServiceAdapter) BeginEnrollment(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	uid, err := parseUserID("user_id", userID)
	if err != nil {
		return nil, err
	}
	return a.mgr.BeginEnrollment(ctx, uid, tenantID, method)
}

func (a *MFAServiceAdapter) CompleteEnrollment(ctx context.Context, userID, tenantID, enrollmentID, code string) error {
	uid, err := parseOptionalUserID("user_id", userID)
	if err != nil {
		return err
	}
	return a.mgr.CompleteEnrollment(ctx, uid, tenantID, enrollmentID, code)
}

func (a *MFAServiceAdapter) CancelEnrollment(ctx context.Context, userID, tenantID, enrollmentID string) error {
	uid, err := parseOptionalUserID("user_id", userID)
	if err != nil {
		return err
	}
	return a.mgr.CancelEnrollment(ctx, uid, tenantID, enrollmentID)
}

func (a *MFAServiceAdapter) BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error) {
	uid, err := parseUserID("user_id", userID)
	if err != nil {
		return nil, err
	}
	return a.mgr.BeginChallenge(ctx, uid, tenantID, sessionToken)
}

func (a *MFAServiceAdapter) VerifyChallenge(ctx context.Context, userID, tenantID, challengeID, code string) (*iam.MFAVerifyResponse, error) {
	uid, err := parseOptionalUserID("user_id", userID)
	if err != nil {
		return nil, err
	}
	return a.mgr.VerifyChallenge(ctx, uid, tenantID, challengeID, code)
}

func (a *MFAServiceAdapter) VerifyCode(ctx context.Context, userID, tenantID, code string) error {
	uid, err := parseUserID("user_id", userID)
	if err != nil {
		return err
	}
	return a.mgr.VerifyCode(ctx, uid, tenantID, code)
}

func (a *MFAServiceAdapter) GetMFAStatus(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error) {
	uid, err := parseUserID("user_id", userID)
	if err != nil {
		return nil, err
	}
	return a.mgr.GetMFAStatus(ctx, uid, tenantID)
}

func (a *MFAServiceAdapter) DisableMFA(ctx context.Context, userID, tenantID string) error {
	uid, err := parseUserID("user_id", userID)
	if err != nil {
		return err
	}
	return a.mgr.DisableMFA(ctx, uid, tenantID)
}

func (a *MFAServiceAdapter) GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error) {
	uid, err := parseUserID("user_id", userID)
	if err != nil {
		return nil, err
	}
	return a.mgr.GenerateBackupCodes(ctx, uid, tenantID)
}

func (a *MFAServiceAdapter) VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error {
	uid, err := parseUserID("user_id", userID)
	if err != nil {
		return err
	}
	return a.mgr.VerifyBackupCode(ctx, uid, tenantID, code)
}

func (a *MFAServiceAdapter) GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error) {
	uid, err := parseUserID("user_id", userID)
	if err != nil {
		return nil, err
	}
	return a.mgr.GetBackupCodeStatus(ctx, uid, tenantID)
}

// IAMServiceAdapter adapts IAM components to implement the IAMService interface.
// It bridges between the iam package types and the gRPC service layer.
type IAMServiceAdapter struct {
	tenantManager iam.TenantManager
	identityStore *iam.IdentityStore
	policyEngine  *iam.PolicyEngine
	appManager    *iam.AppManager
	adminManager  *iam.AdminManager
	gdprManager   *iam.GDPRManager

	// webauthnCredentials is the user-scoped WebAuthn credential
	// administration service. Optional and set after construction
	// (SetWebAuthnCredentials) because the WebAuthn manager is built later
	// in the server lifecycle than the IAM components this adapter wraps;
	// nil means the node reports the verbs as unavailable.
	webauthnCredentials service.WebAuthnCredentialService

	// provisioner is the control-plane tenant provisioner. Optional and set
	// after construction (SetTenantProvisioner).
	provisioner service.TenantProvisioner

	// inviteStore issues the activation invite of a user created pending. Set
	// after construction (SetInviteStore); nil means a pending create is
	// refused with iam.ErrInviteStoreUnavailable before anything is written.
	inviteStore iam.InviteStore

	// inviteActivation redeems invites for the public activation verbs. Set
	// after construction (SetInviteActivation); nil means the node reports
	// the verbs as unavailable.
	inviteActivation service.InviteActivationService
}

// SetInviteActivation wires the invite activation service the public gRPC
// activation verbs delegate to — the same service REST and the embedded
// transport use.
func (a *IAMServiceAdapter) SetInviteActivation(svc service.InviteActivationService) {
	a.inviteActivation = svc
}

// InviteActivation implements the optional capability the gRPC IAM server
// probes for (server_iam_invite.go inviteActivationProvider).
func (a *IAMServiceAdapter) InviteActivation() service.InviteActivationService {
	return a.inviteActivation
}

// SetInviteStore wires the invite store CreateUser issues activation invites
// from — the same store REST and the activation endpoints use.
func (a *IAMServiceAdapter) SetInviteStore(store iam.InviteStore) {
	a.inviteStore = store
}

// SetWebAuthnCredentials wires the WebAuthn credential administration service
// the gRPC IAM server exposes. It is a setter rather than a constructor
// parameter so the adapter keeps its signature for callers that predate the
// verbs, matching how the identity store late-binds its own credential cascade.
func (a *IAMServiceAdapter) SetWebAuthnCredentials(svc service.WebAuthnCredentialService) {
	a.webauthnCredentials = svc
}

// WebAuthnCredentials implements the optional capability the gRPC IAM server
// probes for (server_iam_webauthn.go webAuthnCredentialProvider).
func (a *IAMServiceAdapter) WebAuthnCredentials() service.WebAuthnCredentialService {
	return a.webauthnCredentials
}

// NewIAMServiceAdapter creates a new IAM service adapter.
func NewIAMServiceAdapter(
	tenantManager iam.TenantManager,
	identityStore *iam.IdentityStore,
	policyEngine *iam.PolicyEngine,
	appManager *iam.AppManager,
	adminManager *iam.AdminManager,
	gdprManager *iam.GDPRManager,
) *IAMServiceAdapter {
	return &IAMServiceAdapter{
		tenantManager: tenantManager,
		identityStore: identityStore,
		policyEngine:  policyEngine,
		appManager:    appManager,
		adminManager:  adminManager,
		gdprManager:   gdprManager,
	}
}

// =============================================================================
// Tenant Operations
// =============================================================================

// CreateTenant creates a new tenant. It forwards the caller's
// CreateTenantOptions untouched, so the engine-side provisioning knobs
// (isolation strategy, engine quotas, engine entitlements) reach the tenant
// manager exactly as the gRPC caller sent them.
func (a *IAMServiceAdapter) CreateTenant(ctx context.Context, opts iam.CreateTenantOptions) (*iam.Tenant, error) {
	if opts.Name == "" {
		return nil, api.NewNilInputError("iam.grpc.CreateTenant", "name")
	}

	return a.tenantManager.CreateTenant(ctx, opts)
}

// GetTenant retrieves a tenant by ID.
func (a *IAMServiceAdapter) GetTenant(ctx context.Context, id string) (*iam.Tenant, error) {
	return a.tenantManager.GetTenant(ctx, id)
}

// UpdateTenant updates an existing tenant.
func (a *IAMServiceAdapter) UpdateTenant(ctx context.Context, tenant *iam.Tenant) (*iam.Tenant, error) {
	if tenant == nil {
		return nil, api.NewNilInputError("iam.grpc.UpdateTenant", "tenant")
	}

	opts := iam.UpdateTenantOptions{
		Name:     &tenant.Name,
		Settings: &tenant.Settings,
	}

	return a.tenantManager.UpdateTenant(ctx, tenant.ID, opts)
}

// DeleteTenant deletes a tenant.
func (a *IAMServiceAdapter) DeleteTenant(ctx context.Context, id string) error {
	return a.tenantManager.DeleteTenant(ctx, id)
}

// ListTenants lists all tenants with pagination.
func (a *IAMServiceAdapter) ListTenants(ctx context.Context, limit, offset int) ([]*iam.Tenant, int, error) {
	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	tenants, err := a.tenantManager.ListTenants(ctx, opts)
	if err != nil {
		return nil, 0, err
	}

	return tenants, len(tenants), nil
}

// =============================================================================
// User Operations
// =============================================================================

// CreateUser creates a new user.
func (a *IAMServiceAdapter) CreateUser(ctx context.Context, user *iam.User, inviteTTL time.Duration) (*iam.User, string, *iam.InviteRecord, error) {
	if user == nil {
		return nil, "", nil, api.NewNilInputError("iam.grpc.CreateUser", "user")
	}

	// The record's own handle wins; the email's local part is only the
	// default for a caller that sent none, because CreateUser requires one.
	username := strings.TrimSpace(user.Username)
	if username == "" && user.Email != "" {
		// Use the part before @ as username
		if idx := strings.Index(user.Email, "@"); idx > 0 {
			username = user.Email[:idx]
		} else {
			username = user.Email
		}
	}

	// Roles go in with the create, which validates every reference before
	// any write, so a bad role leaves no partial user behind.
	opts := iam.CreateUserOptions{
		Email:       user.Email,
		Username:    username,
		Name:        user.Name,
		Attributes:  user.Attributes,
		AuthMethods: user.AuthMethods,
		Lifecycle:   user.Lifecycle,
		Roles:       user.Roles,
	}

	// The same service-layer path REST uses: a pending user comes back with
	// the one-time invite that activates it, or is not created at all.
	return a.identityStore.CreateInvitedUser(ctx, user.TenantID, opts, a.inviteStore, inviteTTL)
}

// GetUser retrieves a user by ID or email.
// If the userID looks like an email address (contains @), it will attempt
// to look up the user by email first. If that fails or if it's not an email,
// it will fall back to looking up by user ID.
func (a *IAMServiceAdapter) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	// An identifier containing "@" is an email address. The lookup is
	// tenant-scoped, so a record in another tenant is never read.
	if strings.Contains(userID, "@") {
		return a.identityStore.GetUserByEmail(ctx, tenantID, userID)
	}

	id, err := parseUserID("user_id", userID)
	if err != nil {
		return nil, err
	}
	return a.identityStore.GetUser(ctx, tenantID, id)
}

// UpdateUser updates an existing user.
func (a *IAMServiceAdapter) UpdateUser(ctx context.Context, user *iam.User) (*iam.User, error) {
	if user == nil {
		return nil, api.NewNilInputError("iam.grpc.UpdateUser", "user")
	}

	// Only pass non-empty fields for update (partial update support)
	opts := iam.UpdateUserOptions{}
	if user.Email != "" {
		opts.Email = &user.Email
	}
	if user.Name != "" {
		opts.Name = &user.Name
	}
	// Forwarded now that the handle is its own field: it used to be written
	// into Name by UpdateUser's precedence rule, so setting one changed the
	// other.
	if user.Username != "" {
		opts.Username = &user.Username
	}
	if len(user.Attributes) > 0 {
		opts.Attributes = &user.Attributes
	}
	// Presence, not length: an explicitly empty list clears the restriction,
	// which the model defines as "no restriction" and which a length check
	// silently discards.
	if user.AuthMethods != nil {
		opts.AuthMethods = &user.AuthMethods
	}
	if user.Lifecycle != "" {
		opts.Lifecycle = &user.Lifecycle
	}

	updated, err := a.identityStore.UpdateUser(ctx, user.TenantID, user.ID, opts)
	if err != nil {
		return nil, err
	}

	// Update roles if provided - get current roles first
	currentUser, err := a.identityStore.GetUser(ctx, user.TenantID, user.ID)
	if err != nil {
		return nil, err
	}

	// Simple update: if roles are different, update them
	if len(user.Roles) > 0 {
		// Remove old roles not in new set
		roleMap := make(map[string]bool)
		for _, r := range user.Roles {
			roleMap[r] = true
		}
		for _, oldRole := range currentUser.Roles {
			if !roleMap[oldRole] {
				if err := a.identityStore.RevokeRole(ctx, user.TenantID, user.ID, oldRole); err != nil {
					return nil, err
				}
			}
		}
		// Add new roles not in old set
		oldRoleMap := make(map[string]bool)
		for _, r := range currentUser.Roles {
			oldRoleMap[r] = true
		}
		for _, newRole := range user.Roles {
			if !oldRoleMap[newRole] {
				if err := a.identityStore.AssignRole(ctx, user.TenantID, user.ID, newRole); err != nil {
					return nil, err
				}
			}
		}
	}

	return updated, nil
}

// DeleteUser deletes a user by ID or email.
// If the userID looks like an email address (contains @), it will attempt
// to resolve the email to a user ID first.
func (a *IAMServiceAdapter) DeleteUser(ctx context.Context, tenantID, userID string) error {
	// An identifier containing "@" is an email address; resolve it to the
	// user's id first.
	if strings.Contains(userID, "@") {
		user, err := a.identityStore.GetUserByEmail(ctx, tenantID, userID)
		if err != nil {
			return err
		}
		return a.identityStore.DeleteUser(ctx, tenantID, user.ID)
	}

	id, err := parseUserID("user_id", userID)
	if err != nil {
		return err
	}
	return a.identityStore.DeleteUser(ctx, tenantID, id)
}

// ListUsers lists users with pagination and optional filters.
func (a *IAMServiceAdapter) ListUsers(ctx context.Context, tenantID string, limit, offset int, filters map[string]string) ([]*iam.User, int, error) {
	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
		Filter: make(map[string]interface{}),
	}

	// Add filters
	if email, ok := filters["email"]; ok && email != "" {
		opts.Filter["email"] = email
	}
	if role, ok := filters["role"]; ok && role != "" {
		opts.Filter["role"] = role
	}

	users, err := a.identityStore.ListUsers(ctx, tenantID, opts)
	if err != nil {
		return nil, 0, err
	}

	return users, len(users), nil
}

// =============================================================================
// Role Operations
// =============================================================================

// CreateRole creates a new role.
func (a *IAMServiceAdapter) CreateRole(ctx context.Context, role *iam.Role) (*iam.Role, error) {
	if role == nil {
		return nil, api.NewNilInputError("iam.grpc.CreateRole", "role")
	}

	opts := iam.CreateRoleOptions{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		Permissions: role.Permissions,
	}

	return a.identityStore.CreateRole(ctx, role.TenantID, opts)
}

// GetRole retrieves a role by ID.
func (a *IAMServiceAdapter) GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error) {
	return a.identityStore.GetRole(ctx, tenantID, roleID)
}

// UpdateRole updates an existing role.
func (a *IAMServiceAdapter) UpdateRole(ctx context.Context, role *iam.Role) (*iam.Role, error) {
	if role == nil {
		return nil, api.NewNilInputError("iam.grpc.UpdateRole", "role")
	}

	opts := iam.UpdateRoleOptions{
		Name:        &role.Name,
		Description: &role.Description,
		Permissions: &role.Permissions,
	}

	return a.identityStore.UpdateRole(ctx, role.TenantID, role.ID, opts)
}

// DeleteRole deletes a role.
func (a *IAMServiceAdapter) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	return a.identityStore.DeleteRole(ctx, tenantID, roleID)
}

// ListRoles lists roles with pagination.
func (a *IAMServiceAdapter) ListRoles(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Role, int, error) {
	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	roles, err := a.identityStore.ListRoles(ctx, tenantID, opts)
	if err != nil {
		return nil, 0, err
	}

	return roles, len(roles), nil
}

// AssignRole assigns a role to a user.
func (a *IAMServiceAdapter) AssignRole(ctx context.Context, tenantID string, userID uint64, role string) error {
	return a.identityStore.AssignRole(ctx, tenantID, userID, role)
}

// RevokeRole revokes a role from a user.
func (a *IAMServiceAdapter) RevokeRole(ctx context.Context, tenantID string, userID uint64, role string) error {
	return a.identityStore.RevokeRole(ctx, tenantID, userID, role)
}

// ReconcileBuiltInRoles converges a tenant's stored built-in roles onto the
// definitions the running build carries.
func (a *IAMServiceAdapter) ReconcileBuiltInRoles(ctx context.Context, tenantID string) (*iam.RoleReconcileReport, error) {
	return a.identityStore.ReconcileBuiltInRoles(ctx, tenantID)
}

// =============================================================================
// Application Operations
// =============================================================================

// RegisterApp registers a new OAuth application.
func (a *IAMServiceAdapter) RegisterApp(ctx context.Context, app *iam.Application) (*iam.Application, error) {
	if app == nil {
		return nil, api.NewNilInputError("iam.grpc.RegisterApp", "application")
	}

	opts := iam.RegisterAppOptions{
		Name:           app.Name,
		RedirectURIs:   app.RedirectURIs,
		ResourcePrefix: app.ResourcePrefix,
		GrantTypes:     app.GrantTypes,
		ResponseTypes:  app.ResponseTypes,
	}

	return a.appManager.RegisterApp(ctx, app.TenantID, opts)
}

// GetApp retrieves an application by client ID inside tenantID. The app
// verbs below are tenant scoped so a client id from another tenant answers
// not-found, the same as a missing client.
func (a *IAMServiceAdapter) GetApp(ctx context.Context, tenantID, clientID string) (*iam.Application, error) {
	return a.appManager.GetAppInTenant(ctx, tenantID, clientID)
}

// UpdateApp updates an existing application.
func (a *IAMServiceAdapter) UpdateApp(ctx context.Context, app *iam.Application) (*iam.Application, error) {
	if app == nil {
		return nil, api.NewNilInputError("iam.grpc.UpdateApp", "application")
	}

	// Only set fields that have non-zero values to avoid overwriting
	// existing data with empty values when the caller didn't intend to change them.
	opts := iam.UpdateAppOptions{}
	if app.Name != "" {
		opts.Name = &app.Name
	}
	if len(app.RedirectURIs) > 0 {
		opts.RedirectURIs = &app.RedirectURIs
	}
	if app.ResourcePrefix != "" {
		opts.ResourcePrefix = &app.ResourcePrefix
	}
	if len(app.GrantTypes) > 0 {
		opts.GrantTypes = &app.GrantTypes
	}
	if len(app.ResponseTypes) > 0 {
		opts.ResponseTypes = &app.ResponseTypes
	}

	return a.appManager.UpdateAppInTenant(ctx, app.TenantID, app.ClientID, opts)
}

// DeleteApp deletes an application.
func (a *IAMServiceAdapter) DeleteApp(ctx context.Context, tenantID, clientID string) error {
	return a.appManager.DeleteAppInTenant(ctx, tenantID, clientID)
}

// ListApps lists applications with pagination.
func (a *IAMServiceAdapter) ListApps(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Application, int, error) {
	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	apps, err := a.appManager.ListApps(ctx, tenantID, opts)
	if err != nil {
		return nil, 0, err
	}

	return apps, len(apps), nil
}

// RotateAppSecret rotates an application's client secret.
func (a *IAMServiceAdapter) RotateAppSecret(ctx context.Context, tenantID, clientID string) (*iam.Application, error) {
	return a.appManager.RotateSecretInTenant(ctx, tenantID, clientID)
}

// =============================================================================
// Policy Operations
// =============================================================================

// CreatePolicy creates a new policy.
func (a *IAMServiceAdapter) CreatePolicy(ctx context.Context, policy *iam.Policy) (*iam.Policy, error) {
	if policy == nil {
		return nil, api.NewNilInputError("iam.grpc.CreatePolicy", "policy")
	}

	// PolicyEngine.CreatePolicy needs tenantID explicitly
	err := a.policyEngine.CreatePolicy(ctx, policy.Metadata.Tenant, policy)
	if err != nil {
		return nil, err
	}

	return policy, nil
}

// GetPolicy retrieves a policy by name.
func (a *IAMServiceAdapter) GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error) {
	return a.policyEngine.GetPolicy(ctx, tenantID, policyName)
}

// UpdatePolicy updates an existing policy.
func (a *IAMServiceAdapter) UpdatePolicy(ctx context.Context, policy *iam.Policy) (*iam.Policy, error) {
	if policy == nil {
		return nil, api.NewNilInputError("iam.grpc.UpdatePolicy", "policy")
	}

	// PolicyEngine.UpdatePolicy needs tenantID explicitly
	err := a.policyEngine.UpdatePolicy(ctx, policy.Metadata.Tenant, policy)
	if err != nil {
		return nil, err
	}

	return policy, nil
}

// DeletePolicy deletes a policy.
func (a *IAMServiceAdapter) DeletePolicy(ctx context.Context, tenantID, policyName string) error {
	return a.policyEngine.DeletePolicy(ctx, tenantID, policyName)
}

// ListPolicies lists policies with pagination.
func (a *IAMServiceAdapter) ListPolicies(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Policy, int, error) {
	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	policies, err := a.policyEngine.ListPolicies(ctx, tenantID, opts)
	if err != nil {
		return nil, 0, err
	}

	return policies, len(policies), nil
}

// ValidatePolicy validates a policy YAML without creating it.
func (a *IAMServiceAdapter) ValidatePolicy(ctx context.Context, tenantID string, specYAML []byte) (bool, []string, error) {
	// Parse YAML into Policy struct
	var policy iam.Policy
	if err := yaml.Unmarshal(specYAML, &policy); err != nil {
		return false, []string{fmt.Sprintf("failed to parse YAML: %v", err)}, nil
	}

	// Ensure tenant is set
	policy.Metadata.Tenant = tenantID

	// Validate the policy
	if err := a.policyEngine.ValidatePolicy(&policy); err != nil {
		return false, []string{err.Error()}, nil
	}

	return true, nil, nil
}

// TestPolicy tests a policy against a specific request.
func (a *IAMServiceAdapter) TestPolicy(ctx context.Context, tenantID string, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	if req == nil {
		return nil, api.NewNilInputError("iam.grpc.TestPolicy", "check request")
	}

	// Ensure tenant ID is set in the request
	req.Subject.TenantID = tenantID

	// PolicyEngine has an Evaluate method for checking requests
	return a.policyEngine.Evaluate(ctx, req)
}

// =============================================================================
// Delegated Administration Operations
// =============================================================================

// CreateAdminAssignment creates a new admin assignment.
func (a *IAMServiceAdapter) CreateAdminAssignment(ctx context.Context, opts iam.CreateAdminAssignmentOptions) (*iam.AdminAssignment, error) {
	if a.adminManager == nil {
		return nil, api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.CreateAssignment(ctx, opts)
}

// GetAdminAssignment retrieves an admin assignment by ID.
func (a *IAMServiceAdapter) GetAdminAssignment(ctx context.Context, id string) (*iam.AdminAssignment, error) {
	if a.adminManager == nil {
		return nil, api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.GetAssignment(ctx, id)
}

// UpdateAdminAssignment updates an existing admin assignment.
func (a *IAMServiceAdapter) UpdateAdminAssignment(ctx context.Context, id string, opts iam.UpdateAdminAssignmentOptions) (*iam.AdminAssignment, error) {
	if a.adminManager == nil {
		return nil, api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.UpdateAssignment(ctx, id, opts)
}

// DeleteAdminAssignment deletes an admin assignment.
func (a *IAMServiceAdapter) DeleteAdminAssignment(ctx context.Context, id string) error {
	if a.adminManager == nil {
		return api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.DeleteAssignment(ctx, id)
}

// ListAdminAssignments lists admin assignments with filters.
func (a *IAMServiceAdapter) ListAdminAssignments(ctx context.Context, opts iam.ListAdminAssignmentsOptions) ([]*iam.AdminAssignment, error) {
	if a.adminManager == nil {
		return nil, api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.ListAssignments(ctx, opts)
}

// CheckAdminPermission checks if an admin has a specific permission.
func (a *IAMServiceAdapter) CheckAdminPermission(ctx context.Context, req *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error) {
	if a.adminManager == nil {
		return nil, api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.CheckPermission(ctx, req)
}

// StartImpersonation starts an impersonation session.
func (a *IAMServiceAdapter) StartImpersonation(ctx context.Context, opts *iam.StartImpersonationOptions) (*iam.ImpersonationSession, error) {
	if a.adminManager == nil {
		return nil, api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.StartImpersonation(ctx, opts)
}

// EndImpersonation ends an impersonation session.
func (a *IAMServiceAdapter) EndImpersonation(ctx context.Context, sessionID string) error {
	if a.adminManager == nil {
		return api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.EndImpersonation(ctx, sessionID)
}

// GetActiveImpersonations retrieves active impersonation sessions.
func (a *IAMServiceAdapter) GetActiveImpersonations(ctx context.Context, adminUserID uint64) ([]*iam.ImpersonationSession, error) {
	if a.adminManager == nil {
		return nil, api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.GetActiveImpersonations(ctx, adminUserID)
}

// =============================================================================
// GDPR Consent Operations
// =============================================================================

// GrantConsent grants consent for a specific purpose.
func (a *IAMServiceAdapter) GrantConsent(ctx context.Context, tenantID string, userID uint64, purpose iam.ConsentPurpose, version string, expiresAt *time.Time, ipAddress, userAgent string) (*iam.Consent, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.GrantConsent(ctx, tenantID, userID, purpose, version, expiresAt, ipAddress, userAgent)
}

// WithdrawConsent withdraws a previously granted consent.
func (a *IAMServiceAdapter) WithdrawConsent(ctx context.Context, tenantID string, userID uint64, purpose iam.ConsentPurpose, ipAddress, userAgent string) error {
	if a.gdprManager == nil {
		return api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.WithdrawConsent(ctx, tenantID, userID, purpose, ipAddress, userAgent)
}

// GetConsent retrieves a specific consent.
func (a *IAMServiceAdapter) GetConsent(ctx context.Context, tenantID string, userID uint64, purpose iam.ConsentPurpose) (*iam.Consent, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.GetConsent(ctx, tenantID, userID, purpose)
}

// ListUserConsents lists all consents for a user.
func (a *IAMServiceAdapter) ListUserConsents(ctx context.Context, tenantID string, userID uint64) ([]*iam.Consent, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.ListUserConsents(ctx, tenantID, userID)
}

// =============================================================================
// GDPR Data Export Operations
// =============================================================================

// RequestExport creates a new data export request.
func (a *IAMServiceAdapter) RequestExport(ctx context.Context, tenantID string, userID, requestedBy uint64, format iam.DataExportFormat, includeCategories, excludeCategories []string) (*iam.DataExportRequest, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.RequestExport(ctx, tenantID, userID, requestedBy, format, includeCategories, excludeCategories)
}

// GetExportStatus retrieves the status of an export request.
func (a *IAMServiceAdapter) GetExportStatus(ctx context.Context, exportID string) (*iam.DataExportRequest, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.GetExportStatus(ctx, exportID)
}

// GenerateExport generates a user data export.
func (a *IAMServiceAdapter) GenerateExport(ctx context.Context, exportID string) (*iam.UserDataExport, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.GenerateExport(ctx, exportID)
}

// =============================================================================
// GDPR Data Deletion Operations
// =============================================================================

// RequestDeletion creates a new data deletion request.
func (a *IAMServiceAdapter) RequestDeletion(ctx context.Context, tenantID string, userID, requestedBy uint64, reason string, deleteCategories, preserveCategories []string, gracePeriodDays int) (*iam.DataDeletionRequest, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.RequestDeletion(ctx, tenantID, userID, requestedBy, reason, deleteCategories, preserveCategories, gracePeriodDays)
}

// CancelDeletion cancels a pending deletion request.
func (a *IAMServiceAdapter) CancelDeletion(ctx context.Context, deletionID, cancelledBy string) error {
	if a.gdprManager == nil {
		return api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.CancelDeletion(ctx, deletionID, cancelledBy)
}

// ProcessDeletion processes a deletion request.
func (a *IAMServiceAdapter) ProcessDeletion(ctx context.Context, deletionID string) (*iam.DeletionSummary, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.ProcessDeletion(ctx, deletionID)
}

// GetDeletionStatus retrieves the status of a deletion request.
func (a *IAMServiceAdapter) GetDeletionStatus(ctx context.Context, deletionID string) (*iam.DataDeletionRequest, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.GetDeletionStatus(ctx, deletionID)
}
