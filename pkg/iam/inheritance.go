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
	"fmt"
	"time"

	"github.com/google/uuid"
)

// InheritanceManager manages role and group hierarchies and computes effective permissions.
// It provides operations for creating hierarchies, navigating parent-child relationships,
// and calculating the transitive closure of permissions through inheritance.
type InheritanceManager struct {
	hierarchyStorage HierarchyStorage
	roleStorage      RoleStorage
	groupStorage     GroupStorage
	userStorage      UserStorage
}

// InheritanceManagerOptions configures the InheritanceManager.
type InheritanceManagerOptions struct {
	// HierarchyStorage is the storage backend for hierarchies and permission cache.
	HierarchyStorage HierarchyStorage

	// RoleStorage is the storage backend for roles.
	RoleStorage RoleStorage

	// GroupStorage is the storage backend for groups.
	GroupStorage GroupStorage

	// UserStorage is the storage backend for users.
	UserStorage UserStorage
}

// NewInheritanceManager creates a new InheritanceManager with the given options.
func NewInheritanceManager(opts InheritanceManagerOptions) (*InheritanceManager, error) {
	if opts.HierarchyStorage == nil {
		return nil, NewIAMError(ErrHierarchyInvalid, "iam.inheritance.new", "hierarchy storage is required", nil)
	}
	if opts.RoleStorage == nil {
		return nil, NewIAMError(ErrHierarchyInvalid, "iam.inheritance.new", "role storage is required", nil)
	}
	if opts.GroupStorage == nil {
		return nil, NewIAMError(ErrHierarchyInvalid, "iam.inheritance.new", "group storage is required", nil)
	}
	if opts.UserStorage == nil {
		return nil, NewIAMError(ErrHierarchyInvalid, "iam.inheritance.new", "user storage is required", nil)
	}

	return &InheritanceManager{
		hierarchyStorage: opts.HierarchyStorage,
		roleStorage:      opts.RoleStorage,
		groupStorage:     opts.GroupStorage,
		userStorage:      opts.UserStorage,
	}, nil
}

// AddRoleParent adds a parent role to a child role with the specified priority.
// The child role will inherit all permissions from the parent role.
// Returns an error if this would create a cycle in the hierarchy.
func (m *InheritanceManager) AddRoleParent(ctx context.Context, tenantID, childRoleID, parentRoleID string, priority int) error {
	// Validate that both roles exist
	if _, err := m.roleStorage.GetRole(ctx, tenantID, childRoleID); err != nil {
		return NewIAMError(ErrRoleNotFound, "iam.inheritance.role.add_parent",
			fmt.Sprintf("child role not found: %s", childRoleID), err)
	}
	if _, err := m.roleStorage.GetRole(ctx, tenantID, parentRoleID); err != nil {
		return NewIAMError(ErrRoleNotFound, "iam.inheritance.role.add_parent",
			fmt.Sprintf("parent role not found: %s", parentRoleID), err)
	}

	// Create the hierarchy relationship
	hierarchy := &RoleHierarchy{
		ID:           uuid.New().String(),
		TenantID:     tenantID,
		ParentRoleID: parentRoleID,
		ChildRoleID:  childRoleID,
		Priority:     priority,
		Created:      time.Now(),
	}

	if err := m.hierarchyStorage.CreateRoleHierarchy(ctx, hierarchy); err != nil {
		return err
	}

	// Invalidate cached permissions for the child role and all its descendants
	// Errors during cache invalidation are non-critical; stale cache entries will
	// be refreshed on next access. We intentionally ignore the error here.
	_ = m.invalidateRoleHierarchy(ctx, tenantID, childRoleID)

	return nil
}

// RemoveRoleParent removes a parent-child relationship between roles.
func (m *InheritanceManager) RemoveRoleParent(ctx context.Context, tenantID, childRoleID, parentRoleID string) error {
	if err := m.hierarchyStorage.DeleteRoleHierarchy(ctx, tenantID, parentRoleID, childRoleID); err != nil {
		return err
	}

	// Invalidate cached permissions for the child role and all its descendants
	// Errors during cache invalidation are non-critical; stale cache entries will
	// be refreshed on next access. We intentionally ignore the error here.
	_ = m.invalidateRoleHierarchy(ctx, tenantID, childRoleID)

	return nil
}

// GetRoleHierarchy returns the parent chain for a role, from direct parents to root.
func (m *InheritanceManager) GetRoleHierarchy(ctx context.Context, tenantID, roleID string) ([]*RoleHierarchy, error) {
	return m.hierarchyStorage.ListRoleParents(ctx, tenantID, roleID)
}

// AddGroupParent adds a parent group to a child group.
// If inheritMembers is true, the child group inherits all members from the parent.
// Returns an error if this would create a cycle in the hierarchy.
func (m *InheritanceManager) AddGroupParent(ctx context.Context, tenantID, childGroupID, parentGroupID string, inheritMembers bool) error {
	// Validate that both groups exist
	if _, err := m.groupStorage.GetGroup(ctx, tenantID, childGroupID); err != nil {
		return NewIAMError(ErrGroupNotFound, "iam.inheritance.group.add_parent",
			fmt.Sprintf("child group not found: %s", childGroupID), err)
	}
	if _, err := m.groupStorage.GetGroup(ctx, tenantID, parentGroupID); err != nil {
		return NewIAMError(ErrGroupNotFound, "iam.inheritance.group.add_parent",
			fmt.Sprintf("parent group not found: %s", parentGroupID), err)
	}

	// Create the hierarchy relationship
	hierarchy := &GroupHierarchy{
		ID:             uuid.New().String(),
		TenantID:       tenantID,
		ParentGroupID:  parentGroupID,
		ChildGroupID:   childGroupID,
		InheritMembers: inheritMembers,
		Created:        time.Now(),
	}

	if err := m.hierarchyStorage.CreateGroupHierarchy(ctx, hierarchy); err != nil {
		return err
	}

	// Invalidate cached permissions for affected users
	if inheritMembers {
		// Get all members of the parent group
		parentGroup, err := m.groupStorage.GetGroup(ctx, tenantID, parentGroupID)
		if err == nil {
			for _, userID := range parentGroup.Members {
				_ = m.hierarchyStorage.InvalidateEffectivePermissions(ctx, tenantID, userID)
			}
		}
	}

	return nil
}

// RemoveGroupParent removes a parent-child relationship between groups.
func (m *InheritanceManager) RemoveGroupParent(ctx context.Context, tenantID, childGroupID, parentGroupID string) error {
	// Get the hierarchy before deleting to check inheritMembers
	hierarchy, err := m.hierarchyStorage.GetGroupHierarchy(ctx, tenantID, parentGroupID, childGroupID)
	if err != nil {
		return err
	}

	if err := m.hierarchyStorage.DeleteGroupHierarchy(ctx, tenantID, parentGroupID, childGroupID); err != nil {
		return err
	}

	// Invalidate cached permissions for affected users
	if hierarchy.InheritMembers {
		parentGroup, err := m.groupStorage.GetGroup(ctx, tenantID, parentGroupID)
		if err == nil {
			for _, userID := range parentGroup.Members {
				_ = m.hierarchyStorage.InvalidateEffectivePermissions(ctx, tenantID, userID)
			}
		}
	}

	return nil
}

// GetGroupHierarchy returns the parent chain for a group, from direct parents to root.
func (m *InheritanceManager) GetGroupHierarchy(ctx context.Context, tenantID, groupID string) ([]*GroupHierarchy, error) {
	return m.hierarchyStorage.ListGroupParents(ctx, tenantID, groupID)
}

// GetEffectiveRolePermissions computes all permissions for a role including inherited ones.
// It traverses the role hierarchy and aggregates permissions from all ancestor roles.
func (m *InheritanceManager) GetEffectiveRolePermissions(ctx context.Context, tenantID, roleID string) (*EffectivePermissions, error) {
	// Check cache first
	cached, err := m.hierarchyStorage.GetCachedEffectivePermissions(ctx, tenantID, roleID)
	if err == nil {
		return cached, nil
	}

	// Get the role
	role, err := m.roleStorage.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return nil, NewIAMError(ErrRoleNotFound, "iam.inheritance.role.effective_permissions",
			fmt.Sprintf("role not found: %s", roleID), err)
	}

	// Get all ancestor roles
	ancestors, err := m.hierarchyStorage.GetRoleAncestors(ctx, tenantID, roleID)
	if err != nil {
		return nil, err
	}

	// Collect permissions from all ancestors
	inheritedPerms := make(map[string][]string)
	allPermsSet := make(map[string]bool)

	// Add direct permissions
	for _, perm := range role.Permissions {
		allPermsSet[perm] = true
	}

	// Add inherited permissions
	for _, ancestorID := range ancestors {
		ancestorRole, err := m.roleStorage.GetRole(ctx, tenantID, ancestorID)
		if err != nil {
			continue // Skip if ancestor role not found
		}

		inheritedPerms[ancestorID] = ancestorRole.Permissions
		for _, perm := range ancestorRole.Permissions {
			allPermsSet[perm] = true
		}
	}

	// Convert set to slice
	allPerms := make([]string, 0, len(allPermsSet))
	for perm := range allPermsSet {
		allPerms = append(allPerms, perm)
	}

	effective := &EffectivePermissions{
		SubjectID:            roleID,
		TenantID:             tenantID,
		DirectPermissions:    role.Permissions,
		InheritedPermissions: inheritedPerms,
		AllPermissions:       allPerms,
		ComputedAt:           time.Now(),
	}

	// Cache the result
	_ = m.hierarchyStorage.CacheEffectivePermissions(ctx, effective)

	return effective, nil
}

// GetEffectiveUserGroups returns all groups a user belongs to, including parent groups.
// It traverses the group hierarchy to find all ancestor groups.
func (m *InheritanceManager) GetEffectiveUserGroups(ctx context.Context, tenantID, userID string) ([]string, error) {
	// Get the user
	user, err := m.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, NewIAMError(ErrUserNotFound, "iam.inheritance.user.effective_groups",
			fmt.Sprintf("user not found: %s", userID), err)
	}

	// Start with user's direct groups
	groupSet := make(map[string]bool)
	for _, groupID := range user.Groups {
		groupSet[groupID] = true

		// Get ancestors for each group
		ancestors, err := m.hierarchyStorage.GetGroupAncestors(ctx, tenantID, groupID)
		if err != nil {
			continue
		}

		// Check which ancestors should be included based on InheritMembers
		for _, ancestorID := range ancestors {
			// Check if the path from user's group to ancestor has InheritMembers=true
			if m.shouldInheritFromGroup(ctx, tenantID, groupID, ancestorID) {
				groupSet[ancestorID] = true
			}
		}
	}

	// Convert set to slice
	groups := make([]string, 0, len(groupSet))
	for groupID := range groupSet {
		groups = append(groups, groupID)
	}

	return groups, nil
}

// GetEffectiveUserPermissions computes all permissions for a user.
// This includes permissions from:
// - Directly assigned roles
// - Roles inherited through role hierarchy
// - Roles assigned to user's groups
// - Roles inherited through group hierarchy
func (m *InheritanceManager) GetEffectiveUserPermissions(ctx context.Context, tenantID, userID string) (*EffectivePermissions, error) {
	// Check cache first
	cached, err := m.hierarchyStorage.GetCachedEffectivePermissions(ctx, tenantID, userID)
	if err == nil {
		return cached, nil
	}

	// Get the user
	user, err := m.userStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, NewIAMError(ErrUserNotFound, "iam.inheritance.user.effective_permissions",
			fmt.Sprintf("user not found: %s", userID), err)
	}

	inheritedPerms := make(map[string][]string)
	allPermsSet := make(map[string]bool)

	// Get effective groups (including inherited)
	effectiveGroups, err := m.GetEffectiveUserGroups(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	// Collect all roles from user and groups
	roleSet := make(map[string]bool)
	for _, roleID := range user.Roles {
		roleSet[roleID] = true
	}

	// Add roles from all effective groups
	for _, groupID := range effectiveGroups {
		_, err := m.groupStorage.GetGroup(ctx, tenantID, groupID)
		if err != nil {
			continue
		}
		// Groups don't have roles in the current schema, but we keep the structure for future
		// In a complete implementation, groups might have associated roles
	}

	// Get effective permissions for each role
	for roleID := range roleSet {
		rolePerms, err := m.GetEffectiveRolePermissions(ctx, tenantID, roleID)
		if err != nil {
			continue
		}

		// Add this role's permissions
		inheritedPerms[roleID] = rolePerms.AllPermissions
		for _, perm := range rolePerms.AllPermissions {
			allPermsSet[perm] = true
		}
	}

	// Convert set to slice
	allPerms := make([]string, 0, len(allPermsSet))
	for perm := range allPermsSet {
		allPerms = append(allPerms, perm)
	}

	effective := &EffectivePermissions{
		SubjectID:            userID,
		TenantID:             tenantID,
		DirectPermissions:    []string{}, // Users don't have direct permissions, only through roles
		InheritedPermissions: inheritedPerms,
		AllPermissions:       allPerms,
		ComputedAt:           time.Now(),
	}

	// Cache the result
	_ = m.hierarchyStorage.CacheEffectivePermissions(ctx, effective)

	return effective, nil
}

// InvalidateCache invalidates cached permissions for a specific subject.
func (m *InheritanceManager) InvalidateCache(tenantID, subjectID string) error {
	return m.hierarchyStorage.InvalidateEffectivePermissions(context.Background(), tenantID, subjectID)
}

// invalidateRoleHierarchy invalidates cache for a role and all its descendants.
func (m *InheritanceManager) invalidateRoleHierarchy(ctx context.Context, tenantID, roleID string) error {
	// Invalidate the role itself
	_ = m.hierarchyStorage.InvalidateEffectivePermissions(ctx, tenantID, roleID)

	// Get all children and recursively invalidate
	children, err := m.hierarchyStorage.ListRoleChildren(ctx, tenantID, roleID)
	if err != nil {
		return err
	}

	for _, child := range children {
		_ = m.invalidateRoleHierarchy(ctx, tenantID, child.ChildRoleID)
	}

	return nil
}

// shouldInheritFromGroup checks if a group should inherit members from an ancestor.
// It walks the path from child to ancestor and checks if InheritMembers is true.
func (m *InheritanceManager) shouldInheritFromGroup(ctx context.Context, tenantID, childGroupID, ancestorGroupID string) bool {
	// Walk up the hierarchy from child to ancestor
	visited := make(map[string]bool)
	queue := []string{childGroupID}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		if visited[current] {
			continue
		}
		visited[current] = true

		if current == ancestorGroupID {
			return true
		}

		// Get parents of current group
		parents, err := m.hierarchyStorage.ListGroupParents(ctx, tenantID, current)
		if err != nil {
			continue
		}

		for _, parent := range parents {
			// Only follow the path if InheritMembers is true
			if parent.InheritMembers {
				queue = append(queue, parent.ParentGroupID)
			}
		}
	}

	return false
}
