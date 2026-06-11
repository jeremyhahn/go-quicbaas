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
)

// HierarchyStorage defines the interface for hierarchy and effective permissions persistence.
// Implementations must provide thread-safe operations for hierarchy graphs and permission caching.
type HierarchyStorage interface {
	// CreateRoleHierarchy creates a parent-child relationship between roles.
	// Returns ErrAlreadyExists if the relationship already exists.
	// Returns ErrHierarchyCycle if this would create a cycle in the hierarchy.
	CreateRoleHierarchy(ctx context.Context, hierarchy *RoleHierarchy) error

	// GetRoleHierarchy retrieves a specific role hierarchy relationship.
	// Returns ErrNotFound if the relationship does not exist.
	GetRoleHierarchy(ctx context.Context, tenantID, parentRoleID, childRoleID string) (*RoleHierarchy, error)

	// ListRoleParents retrieves all parent roles for a given role.
	// Returns roles in priority order (lower priority values first).
	ListRoleParents(ctx context.Context, tenantID, roleID string) ([]*RoleHierarchy, error)

	// ListRoleChildren retrieves all child roles for a given role.
	ListRoleChildren(ctx context.Context, tenantID, roleID string) ([]*RoleHierarchy, error)

	// DeleteRoleHierarchy removes a parent-child relationship.
	// Returns ErrNotFound if the relationship does not exist.
	DeleteRoleHierarchy(ctx context.Context, tenantID, parentRoleID, childRoleID string) error

	// GetRoleAncestors retrieves all ancestor roles (transitive closure) for a role.
	// Returns roles from direct parents to root ancestors.
	GetRoleAncestors(ctx context.Context, tenantID, roleID string) ([]string, error)

	// DetectRoleCycle checks if adding a parent-child relationship would create a cycle.
	// Returns true if a cycle would be created, false otherwise.
	DetectRoleCycle(ctx context.Context, tenantID, parentRoleID, childRoleID string) (bool, error)

	// CreateGroupHierarchy creates a parent-child relationship between groups.
	// Returns ErrAlreadyExists if the relationship already exists.
	// Returns ErrHierarchyCycle if this would create a cycle in the hierarchy.
	CreateGroupHierarchy(ctx context.Context, hierarchy *GroupHierarchy) error

	// GetGroupHierarchy retrieves a specific group hierarchy relationship.
	// Returns ErrNotFound if the relationship does not exist.
	GetGroupHierarchy(ctx context.Context, tenantID, parentGroupID, childGroupID string) (*GroupHierarchy, error)

	// ListGroupParents retrieves all parent groups for a given group.
	ListGroupParents(ctx context.Context, tenantID, groupID string) ([]*GroupHierarchy, error)

	// ListGroupChildren retrieves all child groups for a given group.
	ListGroupChildren(ctx context.Context, tenantID, groupID string) ([]*GroupHierarchy, error)

	// DeleteGroupHierarchy removes a parent-child relationship.
	// Returns ErrNotFound if the relationship does not exist.
	DeleteGroupHierarchy(ctx context.Context, tenantID, parentGroupID, childGroupID string) error

	// GetGroupAncestors retrieves all ancestor groups (transitive closure) for a group.
	// Returns groups from direct parents to root ancestors.
	GetGroupAncestors(ctx context.Context, tenantID, groupID string) ([]string, error)

	// DetectGroupCycle checks if adding a parent-child relationship would create a cycle.
	// Returns true if a cycle would be created, false otherwise.
	DetectGroupCycle(ctx context.Context, tenantID, parentGroupID, childGroupID string) (bool, error)

	// CacheEffectivePermissions stores computed effective permissions for a subject.
	// This cache improves performance by avoiding repeated permission calculations.
	CacheEffectivePermissions(ctx context.Context, permissions *EffectivePermissions) error

	// GetCachedEffectivePermissions retrieves cached effective permissions for a subject.
	// Returns ErrNotFound if no cached permissions exist.
	GetCachedEffectivePermissions(ctx context.Context, tenantID, subjectID string) (*EffectivePermissions, error)

	// InvalidateEffectivePermissions removes cached permissions for a subject.
	// This should be called when a role/group hierarchy or permissions change.
	InvalidateEffectivePermissions(ctx context.Context, tenantID, subjectID string) error
}
