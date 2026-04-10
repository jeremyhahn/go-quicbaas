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

package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// MFAServiceAdapter wraps an MFAManager to implement the MFAService interface for gRPC.
// Since the interfaces are compatible, this is a simple type assertion wrapper.
type MFAServiceAdapter struct {
	mgr MFAService
}

// NewMFAServiceAdapter creates a new MFA service adapter.
// The mgr must implement the MFAService interface (service.MFAManager does).
func NewMFAServiceAdapter(mgr interface{}) *MFAServiceAdapter {
	// Type assert to MFAService - service.MFAManager implements all required methods
	if svc, ok := mgr.(MFAService); ok {
		return &MFAServiceAdapter{mgr: svc}
	}
	return nil
}

// Implement MFAService interface by delegating to the wrapped manager

func (a *MFAServiceAdapter) BeginEnrollment(ctx context.Context, userID, tenantID string, method iam.MFAMethod) (*iam.MFAEnrollResponse, error) {
	return a.mgr.BeginEnrollment(ctx, userID, tenantID, method)
}

func (a *MFAServiceAdapter) CompleteEnrollment(ctx context.Context, enrollmentID, code string) error {
	return a.mgr.CompleteEnrollment(ctx, enrollmentID, code)
}

func (a *MFAServiceAdapter) CancelEnrollment(ctx context.Context, enrollmentID string) error {
	return a.mgr.CancelEnrollment(ctx, enrollmentID)
}

func (a *MFAServiceAdapter) BeginChallenge(ctx context.Context, userID, tenantID, sessionToken string) (*iam.MFAChallenge, error) {
	return a.mgr.BeginChallenge(ctx, userID, tenantID, sessionToken)
}

func (a *MFAServiceAdapter) VerifyChallenge(ctx context.Context, challengeID, code string) (*iam.MFAVerifyResponse, error) {
	return a.mgr.VerifyChallenge(ctx, challengeID, code)
}

func (a *MFAServiceAdapter) VerifyCode(ctx context.Context, userID, tenantID, code string) error {
	return a.mgr.VerifyCode(ctx, userID, tenantID, code)
}

func (a *MFAServiceAdapter) GetMFAStatus(ctx context.Context, userID, tenantID string) (*iam.MFAStatus, error) {
	return a.mgr.GetMFAStatus(ctx, userID, tenantID)
}

func (a *MFAServiceAdapter) DisableMFA(ctx context.Context, userID, tenantID string) error {
	return a.mgr.DisableMFA(ctx, userID, tenantID)
}

func (a *MFAServiceAdapter) GenerateBackupCodes(ctx context.Context, userID, tenantID string) ([]string, error) {
	return a.mgr.GenerateBackupCodes(ctx, userID, tenantID)
}

func (a *MFAServiceAdapter) VerifyBackupCode(ctx context.Context, userID, tenantID, code string) error {
	return a.mgr.VerifyBackupCode(ctx, userID, tenantID, code)
}

func (a *MFAServiceAdapter) GetBackupCodeStatus(ctx context.Context, userID, tenantID string) (*iam.BackupCodeStatus, error) {
	return a.mgr.GetBackupCodeStatus(ctx, userID, tenantID)
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

// CreateTenant creates a new tenant.
func (a *IAMServiceAdapter) CreateTenant(ctx context.Context, tenant *iam.Tenant) (*iam.Tenant, error) {
	if tenant == nil {
		return nil, api.NewNilInputError("iam.grpc.CreateTenant", "tenant")
	}

	opts := iam.CreateTenantOptions{
		ID:       tenant.ID,
		Name:     tenant.Name,
		Settings: &tenant.Settings,
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
func (a *IAMServiceAdapter) CreateUser(ctx context.Context, user *iam.User) (*iam.User, error) {
	if user == nil {
		return nil, api.NewNilInputError("iam.grpc.CreateUser", "user")
	}

	// Derive username from email (User struct doesn't have Username field)
	var username string
	if user.Email != "" {
		// Use the part before @ as username
		if idx := strings.Index(user.Email, "@"); idx > 0 {
			username = user.Email[:idx]
		} else {
			username = user.Email
		}
	}

	opts := iam.CreateUserOptions{
		Email:      user.Email,
		Username:   username,
		Name:       user.Name,
		Attributes: user.Attributes,
	}

	created, err := a.identityStore.CreateUser(ctx, user.TenantID, opts)
	if err != nil {
		return nil, err
	}

	// Track successfully assigned roles
	assignedRoles := make([]string, 0, len(user.Roles))
	for _, role := range user.Roles {
		if err := a.identityStore.AssignRole(ctx, user.TenantID, created.ID, role); err != nil {
			// If role assignment fails, return the created user with partial roles
			// This avoids losing the user creation on role assignment failure
			created.Roles = assignedRoles
			return created, err
		}
		assignedRoles = append(assignedRoles, role)
	}

	// Track successfully added groups
	addedGroups := make([]string, 0, len(user.Groups))
	for _, group := range user.Groups {
		if err := a.identityStore.AddUserToGroup(ctx, user.TenantID, group, created.ID); err != nil {
			// If group addition fails, return the created user with partial groups
			created.Roles = assignedRoles
			created.Groups = addedGroups
			return created, err
		}
		addedGroups = append(addedGroups, group)
	}

	// Populate the created user with the successfully assigned roles and groups.
	// This avoids an unnecessary re-fetch which could fail due to read-your-own-writes
	// consistency issues with Raft replication.
	created.Roles = assignedRoles
	created.Groups = addedGroups

	return created, nil
}

// GetUser retrieves a user by ID or email.
// If the userID looks like an email address (contains @), it will attempt
// to look up the user by email first. If that fails or if it's not an email,
// it will fall back to looking up by user ID.
func (a *IAMServiceAdapter) GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error) {
	// Check if userID looks like an email address
	if strings.Contains(userID, "@") {
		// Try to look up by email first
		user, err := a.identityStore.GetUserByEmail(ctx, userID)
		if err == nil {
			// Verify the user belongs to the expected tenant
			if user.TenantID == tenantID {
				return user, nil
			}
			// User exists but in a different tenant - return not found for security
			return nil, api.NewNotFoundError("user", "tenant "+tenantID)
		}
		// Email lookup failed, fall through to ID lookup
	}

	// Look up by user ID
	return a.identityStore.GetUser(ctx, tenantID, userID)
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
	if len(user.Attributes) > 0 {
		opts.Attributes = &user.Attributes
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
				_ = a.identityStore.RevokeRole(ctx, user.TenantID, user.ID, oldRole)
			}
		}
		// Add new roles not in old set
		oldRoleMap := make(map[string]bool)
		for _, r := range currentUser.Roles {
			oldRoleMap[r] = true
		}
		for _, newRole := range user.Roles {
			if !oldRoleMap[newRole] {
				_ = a.identityStore.AssignRole(ctx, user.TenantID, user.ID, newRole)
			}
		}
	}

	// Similar for groups
	if len(user.Groups) > 0 {
		groupMap := make(map[string]bool)
		for _, g := range user.Groups {
			groupMap[g] = true
		}
		for _, oldGroup := range currentUser.Groups {
			if !groupMap[oldGroup] {
				_ = a.identityStore.RemoveUserFromGroup(ctx, user.TenantID, oldGroup, user.ID)
			}
		}
		oldGroupMap := make(map[string]bool)
		for _, g := range currentUser.Groups {
			oldGroupMap[g] = true
		}
		for _, newGroup := range user.Groups {
			if !oldGroupMap[newGroup] {
				_ = a.identityStore.AddUserToGroup(ctx, user.TenantID, newGroup, user.ID)
			}
		}
	}

	return updated, nil
}

// DeleteUser deletes a user by ID or email.
// If the userID looks like an email address (contains @), it will attempt
// to resolve the email to a user ID first.
func (a *IAMServiceAdapter) DeleteUser(ctx context.Context, tenantID, userID string) error {
	// Check if userID looks like an email address
	if strings.Contains(userID, "@") {
		// Try to look up by email first to get the actual user ID
		user, err := a.identityStore.GetUserByEmail(ctx, userID)
		if err == nil && user.TenantID == tenantID {
			// Use the actual user ID for deletion
			userID = user.ID
		}
		// If email lookup fails, fall through to try deletion by ID
	}

	return a.identityStore.DeleteUser(ctx, tenantID, userID)
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
	if group, ok := filters["group"]; ok && group != "" {
		opts.Filter["group"] = group
	}

	users, err := a.identityStore.ListUsers(ctx, tenantID, opts)
	if err != nil {
		return nil, 0, err
	}

	return users, len(users), nil
}

// =============================================================================
// Group Operations
// =============================================================================

// CreateGroup creates a new group.
func (a *IAMServiceAdapter) CreateGroup(ctx context.Context, group *iam.Group) (*iam.Group, error) {
	if group == nil {
		return nil, api.NewNilInputError("iam.grpc.CreateGroup", "group")
	}

	opts := iam.CreateGroupOptions{
		ID:          group.ID,
		Name:        group.Name,
		Description: group.Description,
	}

	return a.identityStore.CreateGroup(ctx, group.TenantID, opts)
}

// GetGroup retrieves a group by ID.
func (a *IAMServiceAdapter) GetGroup(ctx context.Context, tenantID, groupID string) (*iam.Group, error) {
	return a.identityStore.GetGroup(ctx, tenantID, groupID)
}

// UpdateGroup updates an existing group.
func (a *IAMServiceAdapter) UpdateGroup(ctx context.Context, group *iam.Group) (*iam.Group, error) {
	if group == nil {
		return nil, api.NewNilInputError("iam.grpc.UpdateGroup", "group")
	}

	opts := iam.UpdateGroupOptions{
		Name:        &group.Name,
		Description: &group.Description,
	}

	return a.identityStore.UpdateGroup(ctx, group.TenantID, group.ID, opts)
}

// DeleteGroup deletes a group.
func (a *IAMServiceAdapter) DeleteGroup(ctx context.Context, tenantID, groupID string) error {
	return a.identityStore.DeleteGroup(ctx, tenantID, groupID)
}

// ListGroups lists groups with pagination.
func (a *IAMServiceAdapter) ListGroups(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Group, int, error) {
	opts := iam.ListOptions{
		Limit:  limit,
		Offset: offset,
	}

	groups, err := a.identityStore.ListGroups(ctx, tenantID, opts)
	if err != nil {
		return nil, 0, err
	}

	return groups, len(groups), nil
}

// AddUserToGroup adds a user to a group.
func (a *IAMServiceAdapter) AddUserToGroup(ctx context.Context, tenantID, groupID, userID string) error {
	return a.identityStore.AddUserToGroup(ctx, tenantID, groupID, userID)
}

// RemoveUserFromGroup removes a user from a group.
func (a *IAMServiceAdapter) RemoveUserFromGroup(ctx context.Context, tenantID, groupID, userID string) error {
	return a.identityStore.RemoveUserFromGroup(ctx, tenantID, groupID, userID)
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
func (a *IAMServiceAdapter) AssignRole(ctx context.Context, tenantID, userID, role string) error {
	return a.identityStore.AssignRole(ctx, tenantID, userID, role)
}

// RevokeRole revokes a role from a user.
func (a *IAMServiceAdapter) RevokeRole(ctx context.Context, tenantID, userID, role string) error {
	return a.identityStore.RevokeRole(ctx, tenantID, userID, role)
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

// GetApp retrieves an application by client ID.
func (a *IAMServiceAdapter) GetApp(ctx context.Context, tenantID, clientID string) (*iam.Application, error) {
	// AppManager.GetApp only takes clientID
	return a.appManager.GetApp(ctx, clientID)
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

	// AppManager.UpdateApp only takes clientID, not tenantID
	return a.appManager.UpdateApp(ctx, app.ClientID, opts)
}

// DeleteApp deletes an application.
func (a *IAMServiceAdapter) DeleteApp(ctx context.Context, tenantID, clientID string) error {
	// AppManager.DeleteApp only takes clientID
	return a.appManager.DeleteApp(ctx, clientID)
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
	// AppManager.RotateSecret only takes clientID
	return a.appManager.RotateSecret(ctx, clientID)
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
func (a *IAMServiceAdapter) GetActiveImpersonations(ctx context.Context, adminUserID string) ([]*iam.ImpersonationSession, error) {
	if a.adminManager == nil {
		return nil, api.NewServiceNotConfiguredError("admin manager")
	}
	return a.adminManager.GetActiveImpersonations(ctx, adminUserID)
}

// =============================================================================
// GDPR Consent Operations
// =============================================================================

// GrantConsent grants consent for a specific purpose.
func (a *IAMServiceAdapter) GrantConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose, version string, expiresAt *time.Time, ipAddress, userAgent string) (*iam.Consent, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.GrantConsent(ctx, tenantID, userID, purpose, version, expiresAt, ipAddress, userAgent)
}

// WithdrawConsent withdraws a previously granted consent.
func (a *IAMServiceAdapter) WithdrawConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose, ipAddress, userAgent string) error {
	if a.gdprManager == nil {
		return api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.WithdrawConsent(ctx, tenantID, userID, purpose, ipAddress, userAgent)
}

// GetConsent retrieves a specific consent.
func (a *IAMServiceAdapter) GetConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose) (*iam.Consent, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.GetConsent(ctx, tenantID, userID, purpose)
}

// ListUserConsents lists all consents for a user.
func (a *IAMServiceAdapter) ListUserConsents(ctx context.Context, tenantID, userID string) ([]*iam.Consent, error) {
	if a.gdprManager == nil {
		return nil, api.NewServiceNotConfiguredError("GDPR manager")
	}
	return a.gdprManager.ListUserConsents(ctx, tenantID, userID)
}

// =============================================================================
// GDPR Data Export Operations
// =============================================================================

// RequestExport creates a new data export request.
func (a *IAMServiceAdapter) RequestExport(ctx context.Context, tenantID, userID, requestedBy string, format iam.DataExportFormat, includeCategories, excludeCategories []string) (*iam.DataExportRequest, error) {
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
func (a *IAMServiceAdapter) RequestDeletion(ctx context.Context, tenantID, userID, requestedBy, reason string, deleteCategories, preserveCategories []string, gracePeriodDays int) (*iam.DataDeletionRequest, error) {
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
