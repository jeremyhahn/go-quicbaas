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
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"

	"github.com/google/uuid"
)

// Email validation regex (RFC 5322 simplified).
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// Username validation: alphanumeric, dash, underscore, 3-64 chars.
var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,64}$`)

// IdentityStore manages users, groups, and roles with multi-tenant support.
type IdentityStore struct {
	userStorage  UserStorage
	groupStorage GroupStorage
	roleStorage  RoleStorage
	tenants      TenantManager
}

// NewIdentityStore creates a new identity store with the provided options.
func NewIdentityStore(opts IdentityStoreOptions) *IdentityStore {
	return &IdentityStore{
		userStorage:  opts.UserStorage,
		groupStorage: opts.GroupStorage,
		roleStorage:  opts.RoleStorage,
		tenants:      opts.Tenants,
	}
}

// GetTenant retrieves a tenant by ID. This is used by REST handlers to validate
// tenant existence before listing resources.
func (s *IdentityStore) GetTenant(ctx context.Context, tenantID string) (*Tenant, error) {
	if s.tenants == nil {
		return nil, api.NewServiceUnavailableError("tenant manager")
	}
	return s.tenants.GetTenant(ctx, tenantID)
}

// validateEmail checks if an email address is valid.
func validateEmail(email string) error {
	if email == "" {
		return &ValidationError{Field: "email", Message: "email cannot be empty"}
	}
	if !emailRegex.MatchString(email) {
		return &ValidationError{Field: "email", Message: "invalid email format"}
	}
	return nil
}

// validateUsername checks if a username is valid.
func validateUsername(username string) error {
	if username == "" {
		return &ValidationError{Field: "username", Message: "username cannot be empty"}
	}
	if !usernameRegex.MatchString(username) {
		return &ValidationError{Field: "username", Message: "username must be 3-64 alphanumeric characters, dash, or underscore"}
	}
	return nil
}

// validateTenantID checks if a tenant ID is valid and not empty.
func validateTenantID(tenantID string) error {
	if tenantID == "" {
		return &ValidationError{Field: "tenant_id", Message: "tenant ID cannot be empty"}
	}
	return nil
}

// validateUserID checks if a user ID is valid and not empty.
func validateUserID(userID string) error {
	if userID == "" {
		return &ValidationError{Field: "user_id", Message: "user ID cannot be empty"}
	}
	return nil
}

// validateGroupID checks if a group ID is valid and not empty.
func validateGroupID(groupID string) error {
	if groupID == "" {
		return &ValidationError{Field: "group_id", Message: "group ID cannot be empty"}
	}
	return nil
}

// validateRoleID checks if a role ID is valid and not empty.
func validateRoleID(roleID string) error {
	if roleID == "" {
		return &ValidationError{Field: "role_id", Message: "role ID cannot be empty"}
	}
	return nil
}

// CreateUser creates a new user in the specified tenant.
func (s *IdentityStore) CreateUser(ctx context.Context, tenantID string, opts CreateUserOptions) (*User, error) {
	// Validate inputs
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateEmail(opts.Email); err != nil {
		return nil, err
	}
	if err := validateUsername(opts.Username); err != nil {
		return nil, err
	}

	// Check tenant quota
	if s.tenants != nil {
		if err := s.tenants.CheckQuota(ctx, tenantID, QuotaResourceUsers); err != nil {
			return nil, err
		}
	}

	// Check email uniqueness (global)
	existing, err := s.userStorage.GetUserByEmail(ctx, opts.Email)
	if err == nil && existing != nil {
		return nil, ErrEmailAlreadyExists
	}
	// Ignore not-found errors (expected for new user creation)
	if err != nil && !dberrors.IsNotFound(err) {
		return nil, api.NewOperationFailedError("check email uniqueness", err)
	}

	// Create user
	now := time.Now().UTC()
	userID := opts.ID
	if userID == "" {
		userID = uuid.New().String()
	}
	// Use Name if provided, otherwise fall back to Username
	displayName := strings.TrimSpace(opts.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(opts.Username)
	}
	user := &User{
		ID:         userID,
		TenantID:   tenantID,
		Email:      strings.ToLower(strings.TrimSpace(opts.Email)),
		Name:       displayName,
		Attributes: opts.Attributes,
		Groups:     []string{},
		Roles:      []string{},
		Created:    now,
		Updated:    now,
	}

	if err := s.userStorage.CreateUser(ctx, user); err != nil {
		return nil, api.NewOperationFailedError("create user", err)
	}

	return user, nil
}

// GetUser retrieves a user by ID within a tenant.
func (s *IdentityStore) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateUserID(userID); err != nil {
		return nil, err
	}

	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// GetUserByEmail retrieves a user by email address (global lookup).
func (s *IdentityStore) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	if err := validateEmail(email); err != nil {
		return nil, err
	}

	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.userStorage.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// UpdateUser updates an existing user.
func (s *IdentityStore) UpdateUser(ctx context.Context, tenantID, userID string, opts UpdateUserOptions) (*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateUserID(userID); err != nil {
		return nil, err
	}

	// Get existing user
	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if opts.Email != nil {
		newEmail := strings.ToLower(strings.TrimSpace(*opts.Email))
		if err := validateEmail(newEmail); err != nil {
			return nil, err
		}

		// Check email uniqueness if changing
		if newEmail != user.Email {
			existing, err := s.userStorage.GetUserByEmail(ctx, newEmail)
			if err == nil && existing != nil {
				return nil, ErrEmailAlreadyExists
			}
			// Ignore not-found errors (expected when email is available)
			if err != nil && !dberrors.IsNotFound(err) {
				return nil, api.NewOperationFailedError("check email uniqueness", err)
			}
			user.Email = newEmail
		}
	}

	// Name takes precedence over Username for display name
	if opts.Name != nil {
		user.Name = strings.TrimSpace(*opts.Name)
	} else if opts.Username != nil {
		username := strings.TrimSpace(*opts.Username)
		if err := validateUsername(username); err != nil {
			return nil, err
		}
		user.Name = username
	}

	if opts.Attributes != nil {
		user.Attributes = *opts.Attributes
	}

	if opts.Enabled != nil {
		// The User struct uses Suspended and Locked instead of Enabled
		// Enabled=true means Suspended=false and Locked=false
		user.Suspended = !*opts.Enabled
	}

	if opts.Roles != nil {
		user.Roles = *opts.Roles
	}

	if opts.Groups != nil {
		user.Groups = *opts.Groups
	}

	user.Updated = time.Now().UTC()

	if err := s.userStorage.UpdateUser(ctx, user); err != nil {
		return nil, api.NewOperationFailedError("update user", err)
	}

	return user, nil
}

// DeleteUser deletes a user from the tenant.
func (s *IdentityStore) DeleteUser(ctx context.Context, tenantID, userID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateUserID(userID); err != nil {
		return err
	}

	// Get user to remove from groups
	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	// Remove user from all groups
	for _, groupID := range user.Groups {
		group, err := s.groupStorage.GetGroup(ctx, tenantID, groupID)
		if err != nil {
			continue // Group might have been deleted
		}

		// Remove user from group members
		newMembers := make([]string, 0, len(group.Members))
		for _, memberID := range group.Members {
			if memberID != userID {
				newMembers = append(newMembers, memberID)
			}
		}
		group.Members = newMembers
		group.Updated = time.Now().UTC()

		if err := s.groupStorage.UpdateGroup(ctx, group); err != nil {
			return api.NewOperationFailedError("update group "+groupID, err)
		}
	}

	if err := s.userStorage.DeleteUser(ctx, tenantID, userID); err != nil {
		return api.NewOperationFailedError("delete user", err)
	}

	return nil
}

// ListUsers returns a list of users in the tenant.
func (s *IdentityStore) ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}

	users, err := s.userStorage.ListUsers(ctx, tenantID, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("list users", err)
	}

	return users, nil
}

// CreateGroup creates a new group in the specified tenant.
func (s *IdentityStore) CreateGroup(ctx context.Context, tenantID string, opts CreateGroupOptions) (*Group, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if opts.Name == "" {
		return nil, &ValidationError{Field: "name", Message: "group name cannot be empty"}
	}

	// Check tenant quota
	if s.tenants != nil {
		if err := s.tenants.CheckQuota(ctx, tenantID, QuotaResourceGroups); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	groupID := opts.ID
	if groupID == "" {
		groupID = uuid.New().String()
	}
	group := &Group{
		ID:          groupID,
		TenantID:    tenantID,
		Name:        strings.TrimSpace(opts.Name),
		Description: strings.TrimSpace(opts.Description),
		Members:     []string{},
		Created:     now,
		Updated:     now,
	}

	if err := s.groupStorage.CreateGroup(ctx, group); err != nil {
		return nil, api.NewOperationFailedError("create group", err)
	}

	return group, nil
}

// GetGroup retrieves a group by ID within a tenant.
func (s *IdentityStore) GetGroup(ctx context.Context, tenantID, groupID string) (*Group, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateGroupID(groupID); err != nil {
		return nil, err
	}

	group, err := s.groupStorage.GetGroup(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}

	return group, nil
}

// UpdateGroup updates an existing group.
func (s *IdentityStore) UpdateGroup(ctx context.Context, tenantID, groupID string, opts UpdateGroupOptions) (*Group, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateGroupID(groupID); err != nil {
		return nil, err
	}

	group, err := s.groupStorage.GetGroup(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}

	if opts.Name != nil {
		name := strings.TrimSpace(*opts.Name)
		if name == "" {
			return nil, &ValidationError{Field: "name", Message: "group name cannot be empty"}
		}
		group.Name = name
	}

	if opts.Description != nil {
		group.Description = strings.TrimSpace(*opts.Description)
	}

	if opts.Members != nil {
		group.Members = *opts.Members
	}

	group.Updated = time.Now().UTC()

	if err := s.groupStorage.UpdateGroup(ctx, group); err != nil {
		return nil, api.NewOperationFailedError("update group", err)
	}

	return group, nil
}

// DeleteGroup deletes a group from the tenant.
func (s *IdentityStore) DeleteGroup(ctx context.Context, tenantID, groupID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateGroupID(groupID); err != nil {
		return err
	}

	// Get group to remove from users
	group, err := s.groupStorage.GetGroup(ctx, tenantID, groupID)
	if err != nil {
		return err
	}

	// Remove group from all member users
	for _, userID := range group.Members {
		user, err := s.userStorage.GetUser(ctx, tenantID, userID)
		if err != nil {
			continue // User might have been deleted
		}

		// Remove group from user's groups
		newGroupIDs := make([]string, 0, len(user.Groups))
		for _, gid := range user.Groups {
			if gid != groupID {
				newGroupIDs = append(newGroupIDs, gid)
			}
		}
		user.Groups = newGroupIDs
		user.Updated = time.Now().UTC()

		if err := s.userStorage.UpdateUser(ctx, user); err != nil {
			return api.NewOperationFailedError("update user "+userID, err)
		}
	}

	if err := s.groupStorage.DeleteGroup(ctx, tenantID, groupID); err != nil {
		return api.NewOperationFailedError("delete group", err)
	}

	return nil
}

// ListGroups returns a list of groups in the tenant.
func (s *IdentityStore) ListGroups(ctx context.Context, tenantID string, opts ListOptions) ([]*Group, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}

	groups, err := s.groupStorage.ListGroups(ctx, tenantID, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("list groups", err)
	}

	return groups, nil
}

// AddUserToGroup adds a user to a group.
func (s *IdentityStore) AddUserToGroup(ctx context.Context, tenantID, groupID, userID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateGroupID(groupID); err != nil {
		return err
	}
	if err := validateUserID(userID); err != nil {
		return err
	}

	// Get user and group
	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	group, err := s.groupStorage.GetGroup(ctx, tenantID, groupID)
	if err != nil {
		return err
	}

	// Check if user is already in group
	for _, gid := range user.Groups {
		if gid == groupID {
			return ErrUserAlreadyInGroup
		}
	}

	// Add group to user
	user.Groups = append(user.Groups, groupID)
	user.Updated = time.Now().UTC()

	if err := s.userStorage.UpdateUser(ctx, user); err != nil {
		return api.NewOperationFailedError("update user", err)
	}

	// Add user to group
	group.Members = append(group.Members, userID)
	group.Updated = time.Now().UTC()

	if err := s.groupStorage.UpdateGroup(ctx, group); err != nil {
		// Rollback user update
		user.Groups = user.Groups[:len(user.Groups)-1]
		_ = s.userStorage.UpdateUser(ctx, user)
		return api.NewOperationFailedError("update group", err)
	}

	return nil
}

// RemoveUserFromGroup removes a user from a group.
func (s *IdentityStore) RemoveUserFromGroup(ctx context.Context, tenantID, groupID, userID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateGroupID(groupID); err != nil {
		return err
	}
	if err := validateUserID(userID); err != nil {
		return err
	}

	// Get user and group
	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	group, err := s.groupStorage.GetGroup(ctx, tenantID, groupID)
	if err != nil {
		return err
	}

	// Check if user is in group
	found := false
	newGroupIDs := make([]string, 0, len(user.Groups))
	for _, gid := range user.Groups {
		if gid == groupID {
			found = true
		} else {
			newGroupIDs = append(newGroupIDs, gid)
		}
	}

	if !found {
		return ErrUserNotInGroup
	}

	// Remove group from user
	user.Groups = newGroupIDs
	user.Updated = time.Now().UTC()

	if err := s.userStorage.UpdateUser(ctx, user); err != nil {
		return api.NewOperationFailedError("update user", err)
	}

	// Remove user from group
	newMemberIDs := make([]string, 0, len(group.Members))
	for _, memberID := range group.Members {
		if memberID != userID {
			newMemberIDs = append(newMemberIDs, memberID)
		}
	}
	group.Members = newMemberIDs
	group.Updated = time.Now().UTC()

	if err := s.groupStorage.UpdateGroup(ctx, group); err != nil {
		return api.NewOperationFailedError("update group", err)
	}

	return nil
}

// GetGroupMembers retrieves all users that are members of a group.
func (s *IdentityStore) GetGroupMembers(ctx context.Context, tenantID, groupID string) ([]*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateGroupID(groupID); err != nil {
		return nil, err
	}

	group, err := s.groupStorage.GetGroup(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}

	users := make([]*User, 0, len(group.Members))
	for _, userID := range group.Members {
		user, err := s.userStorage.GetUser(ctx, tenantID, userID)
		if err != nil {
			if errors.Is(err, ErrUserNotFound) {
				continue // Skip deleted users
			}
			return nil, api.NewOperationFailedError("get user "+userID, err)
		}
		users = append(users, user)
	}

	return users, nil
}

// CreateRole creates a new role in the specified tenant.
func (s *IdentityStore) CreateRole(ctx context.Context, tenantID string, opts CreateRoleOptions) (*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if opts.Name == "" {
		return nil, &ValidationError{Field: "name", Message: "role name cannot be empty"}
	}

	// Check tenant quota
	if s.tenants != nil {
		if err := s.tenants.CheckQuota(ctx, tenantID, QuotaResourceRoles); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	roleID := opts.ID
	if roleID == "" {
		roleID = uuid.New().String()
	}
	role := &Role{
		ID:          roleID,
		TenantID:    tenantID,
		Name:        strings.TrimSpace(opts.Name),
		Description: strings.TrimSpace(opts.Description),
		Permissions: opts.Permissions,
		Created:     now,
		Updated:     now,
	}

	if role.Permissions == nil {
		role.Permissions = []string{}
	}

	if err := s.roleStorage.CreateRole(ctx, role); err != nil {
		return nil, api.NewOperationFailedError("create role", err)
	}

	return role, nil
}

// GetRole retrieves a role by ID within a tenant.
func (s *IdentityStore) GetRole(ctx context.Context, tenantID, roleID string) (*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateRoleID(roleID); err != nil {
		return nil, err
	}

	role, err := s.roleStorage.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}

	return role, nil
}

// UpdateRole updates an existing role.
func (s *IdentityStore) UpdateRole(ctx context.Context, tenantID, roleID string, opts UpdateRoleOptions) (*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateRoleID(roleID); err != nil {
		return nil, err
	}

	role, err := s.roleStorage.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}

	if opts.Name != nil {
		name := strings.TrimSpace(*opts.Name)
		if name == "" {
			return nil, &ValidationError{Field: "name", Message: "role name cannot be empty"}
		}
		role.Name = name
	}

	if opts.Description != nil {
		role.Description = strings.TrimSpace(*opts.Description)
	}

	if opts.Permissions != nil {
		role.Permissions = *opts.Permissions
	}

	role.Updated = time.Now().UTC()

	if err := s.roleStorage.UpdateRole(ctx, role); err != nil {
		return nil, api.NewOperationFailedError("update role", err)
	}

	return role, nil
}

// DeleteRole deletes a role from the tenant.
func (s *IdentityStore) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateRoleID(roleID); err != nil {
		return err
	}

	// List all users and remove this role from them
	opts := ListOptions{Limit: 1000}
	users, err := s.userStorage.ListUsers(ctx, tenantID, opts)
	if err != nil {
		return api.NewOperationFailedError("list users", err)
	}

	for _, user := range users {
		// Check if user has this role
		hasRole := false
		newRoleIDs := make([]string, 0, len(user.Roles))
		for _, rid := range user.Roles {
			if rid == roleID {
				hasRole = true
			} else {
				newRoleIDs = append(newRoleIDs, rid)
			}
		}

		if hasRole {
			user.Roles = newRoleIDs
			user.Updated = time.Now().UTC()
			if err := s.userStorage.UpdateUser(ctx, user); err != nil {
				return api.NewOperationFailedError("update user "+user.ID, err)
			}
		}
	}

	if err := s.roleStorage.DeleteRole(ctx, tenantID, roleID); err != nil {
		return api.NewOperationFailedError("delete role", err)
	}

	return nil
}

// ListRoles returns a list of roles in the tenant.
func (s *IdentityStore) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}

	roles, err := s.roleStorage.ListRoles(ctx, tenantID, opts)
	if err != nil {
		return nil, api.NewOperationFailedError("list roles", err)
	}

	return roles, nil
}

// AssignRole assigns a role to a user.
func (s *IdentityStore) AssignRole(ctx context.Context, tenantID, userID, roleID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateUserID(userID); err != nil {
		return err
	}
	if err := validateRoleID(roleID); err != nil {
		return err
	}

	// Verify user and role exist
	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	if _, err := s.roleStorage.GetRole(ctx, tenantID, roleID); err != nil {
		return err
	}

	// Check if role is already assigned
	for _, rid := range user.Roles {
		if rid == roleID {
			return ErrRoleAlreadyAssigned
		}
	}

	// Assign role
	user.Roles = append(user.Roles, roleID)
	user.Updated = time.Now().UTC()

	if err := s.userStorage.UpdateUser(ctx, user); err != nil {
		return api.NewOperationFailedError("update user", err)
	}

	return nil
}

// RevokeRole revokes a role from a user.
func (s *IdentityStore) RevokeRole(ctx context.Context, tenantID, userID, roleID string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	if err := validateUserID(userID); err != nil {
		return err
	}
	if err := validateRoleID(roleID); err != nil {
		return err
	}

	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return err
	}

	// Check if role is assigned
	found := false
	newRoleIDs := make([]string, 0, len(user.Roles))
	for _, rid := range user.Roles {
		if rid == roleID {
			found = true
		} else {
			newRoleIDs = append(newRoleIDs, rid)
		}
	}

	if !found {
		return ErrRoleNotAssigned
	}

	// Revoke role
	user.Roles = newRoleIDs
	user.Updated = time.Now().UTC()

	if err := s.userStorage.UpdateUser(ctx, user); err != nil {
		return api.NewOperationFailedError("update user", err)
	}

	return nil
}

// GetUserRoles retrieves all roles assigned to a user.
func (s *IdentityStore) GetUserRoles(ctx context.Context, tenantID, userID string) ([]*Role, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if err := validateUserID(userID); err != nil {
		return nil, err
	}

	user, err := s.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	roles := make([]*Role, 0, len(user.Roles))
	for _, roleID := range user.Roles {
		role, err := s.roleStorage.GetRole(ctx, tenantID, roleID)
		if err != nil {
			if errors.Is(err, ErrRoleNotFound) {
				continue // Skip deleted roles
			}
			return nil, api.NewOperationFailedError("get role "+roleID, err)
		}
		roles = append(roles, role)
	}

	return roles, nil
}
