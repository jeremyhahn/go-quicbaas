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
	"sort"
	"sync"
)

// MemoryHierarchyStorage is an in-memory implementation of HierarchyStorage.
// It uses adjacency lists to represent hierarchy graphs and provides thread-safe operations.
type MemoryHierarchyStorage struct {
	mu sync.RWMutex

	// Role hierarchy data structures
	roleHierarchies map[string]*RoleHierarchy            // key: tenantID:parentID:childID
	roleParents     map[string]map[string]*RoleHierarchy // key: tenantID:roleID -> parentID -> hierarchy
	roleChildren    map[string]map[string]*RoleHierarchy // key: tenantID:roleID -> childID -> hierarchy

	// Group hierarchy data structures
	groupHierarchies map[string]*GroupHierarchy            // key: tenantID:parentID:childID
	groupParents     map[string]map[string]*GroupHierarchy // key: tenantID:groupID -> parentID -> hierarchy
	groupChildren    map[string]map[string]*GroupHierarchy // key: tenantID:groupID -> childID -> hierarchy

	// Effective permissions cache
	permissionsCache map[string]*EffectivePermissions // key: tenantID:subjectID
}

// NewMemoryHierarchyStorage creates a new in-memory hierarchy storage.
func NewMemoryHierarchyStorage() *MemoryHierarchyStorage {
	return &MemoryHierarchyStorage{
		roleHierarchies:  make(map[string]*RoleHierarchy),
		roleParents:      make(map[string]map[string]*RoleHierarchy),
		roleChildren:     make(map[string]map[string]*RoleHierarchy),
		groupHierarchies: make(map[string]*GroupHierarchy),
		groupParents:     make(map[string]map[string]*GroupHierarchy),
		groupChildren:    make(map[string]map[string]*GroupHierarchy),
		permissionsCache: make(map[string]*EffectivePermissions),
	}
}

// CreateRoleHierarchy creates a parent-child relationship between roles.
func (s *MemoryHierarchyStorage) CreateRoleHierarchy(ctx context.Context, hierarchy *RoleHierarchy) error {
	if err := hierarchy.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for cycles before creating
	if s.wouldCreateRoleCycle(hierarchy.TenantID, hierarchy.ParentRoleID, hierarchy.ChildRoleID) {
		return NewIAMError(ErrHierarchyCycle, "iam.inheritance.role.create",
			fmt.Sprintf("adding parent %s to child %s would create a cycle", hierarchy.ParentRoleID, hierarchy.ChildRoleID), nil)
	}

	key := s.roleHierarchyKey(hierarchy.TenantID, hierarchy.ParentRoleID, hierarchy.ChildRoleID)
	if _, exists := s.roleHierarchies[key]; exists {
		return NewIAMError(ErrHierarchyAlreadyExists, "iam.inheritance.role.create",
			fmt.Sprintf("hierarchy relationship already exists: %s -> %s", hierarchy.ParentRoleID, hierarchy.ChildRoleID), nil)
	}

	// Store the hierarchy
	s.roleHierarchies[key] = hierarchy

	// Update adjacency lists
	childKey := s.roleKey(hierarchy.TenantID, hierarchy.ChildRoleID)
	if s.roleParents[childKey] == nil {
		s.roleParents[childKey] = make(map[string]*RoleHierarchy)
	}
	s.roleParents[childKey][hierarchy.ParentRoleID] = hierarchy

	parentKey := s.roleKey(hierarchy.TenantID, hierarchy.ParentRoleID)
	if s.roleChildren[parentKey] == nil {
		s.roleChildren[parentKey] = make(map[string]*RoleHierarchy)
	}
	s.roleChildren[parentKey][hierarchy.ChildRoleID] = hierarchy

	return nil
}

// GetRoleHierarchy retrieves a specific role hierarchy relationship.
func (s *MemoryHierarchyStorage) GetRoleHierarchy(ctx context.Context, tenantID, parentRoleID, childRoleID string) (*RoleHierarchy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.roleHierarchyKey(tenantID, parentRoleID, childRoleID)
	hierarchy, exists := s.roleHierarchies[key]
	if !exists {
		return nil, NewIAMError(ErrHierarchyNotFound, "iam.inheritance.role.get",
			fmt.Sprintf("hierarchy not found: %s -> %s", parentRoleID, childRoleID), nil)
	}

	return hierarchy, nil
}

// ListRoleParents retrieves all parent roles for a given role.
func (s *MemoryHierarchyStorage) ListRoleParents(ctx context.Context, tenantID, roleID string) ([]*RoleHierarchy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.roleKey(tenantID, roleID)
	parents := s.roleParents[key]
	if parents == nil {
		return []*RoleHierarchy{}, nil
	}

	// Convert map to slice and sort by priority
	result := make([]*RoleHierarchy, 0, len(parents))
	for _, h := range parents {
		result = append(result, h)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Priority < result[j].Priority
	})

	return result, nil
}

// ListRoleChildren retrieves all child roles for a given role.
func (s *MemoryHierarchyStorage) ListRoleChildren(ctx context.Context, tenantID, roleID string) ([]*RoleHierarchy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.roleKey(tenantID, roleID)
	children := s.roleChildren[key]
	if children == nil {
		return []*RoleHierarchy{}, nil
	}

	// Convert map to slice
	result := make([]*RoleHierarchy, 0, len(children))
	for _, h := range children {
		result = append(result, h)
	}

	return result, nil
}

// DeleteRoleHierarchy removes a parent-child relationship.
func (s *MemoryHierarchyStorage) DeleteRoleHierarchy(ctx context.Context, tenantID, parentRoleID, childRoleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.roleHierarchyKey(tenantID, parentRoleID, childRoleID)
	if _, exists := s.roleHierarchies[key]; !exists {
		return NewIAMError(ErrHierarchyNotFound, "iam.inheritance.role.delete",
			fmt.Sprintf("hierarchy not found: %s -> %s", parentRoleID, childRoleID), nil)
	}

	// Remove from main storage
	delete(s.roleHierarchies, key)

	// Remove from adjacency lists
	childKey := s.roleKey(tenantID, childRoleID)
	if parents := s.roleParents[childKey]; parents != nil {
		delete(parents, parentRoleID)
		if len(parents) == 0 {
			delete(s.roleParents, childKey)
		}
	}

	parentKey := s.roleKey(tenantID, parentRoleID)
	if children := s.roleChildren[parentKey]; children != nil {
		delete(children, childRoleID)
		if len(children) == 0 {
			delete(s.roleChildren, parentKey)
		}
	}

	return nil
}

// GetRoleAncestors retrieves all ancestor roles (transitive closure) for a role.
func (s *MemoryHierarchyStorage) GetRoleAncestors(ctx context.Context, tenantID, roleID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getRoleAncestorsRecursive(tenantID, roleID, make(map[string]bool)), nil
}

// getRoleAncestorsRecursive performs DFS to find all ancestors.
func (s *MemoryHierarchyStorage) getRoleAncestorsRecursive(tenantID, roleID string, visited map[string]bool) []string {
	if visited[roleID] {
		return nil
	}
	visited[roleID] = true

	var ancestors []string
	key := s.roleKey(tenantID, roleID)
	parents := s.roleParents[key]

	for parentID := range parents {
		ancestors = append(ancestors, parentID)
		// Recursively get ancestors of this parent
		ancestors = append(ancestors, s.getRoleAncestorsRecursive(tenantID, parentID, visited)...)
	}

	return ancestors
}

// DetectRoleCycle checks if adding a parent-child relationship would create a cycle.
func (s *MemoryHierarchyStorage) DetectRoleCycle(ctx context.Context, tenantID, parentRoleID, childRoleID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.wouldCreateRoleCycle(tenantID, parentRoleID, childRoleID), nil
}

// wouldCreateRoleCycle checks if adding parent->child would create a cycle.
// A cycle exists if parent is a descendant of child.
func (s *MemoryHierarchyStorage) wouldCreateRoleCycle(tenantID, parentRoleID, childRoleID string) bool {
	// If parent is a descendant of child, adding this edge creates a cycle
	descendants := s.getRoleDescendantsRecursive(tenantID, childRoleID, make(map[string]bool))
	for _, desc := range descendants {
		if desc == parentRoleID {
			return true
		}
	}
	return false
}

// getRoleDescendantsRecursive performs DFS to find all descendants.
func (s *MemoryHierarchyStorage) getRoleDescendantsRecursive(tenantID, roleID string, visited map[string]bool) []string {
	if visited[roleID] {
		return nil
	}
	visited[roleID] = true

	var descendants []string
	key := s.roleKey(tenantID, roleID)
	children := s.roleChildren[key]

	for childID := range children {
		descendants = append(descendants, childID)
		// Recursively get descendants of this child
		descendants = append(descendants, s.getRoleDescendantsRecursive(tenantID, childID, visited)...)
	}

	return descendants
}

// CreateGroupHierarchy creates a parent-child relationship between groups.
func (s *MemoryHierarchyStorage) CreateGroupHierarchy(ctx context.Context, hierarchy *GroupHierarchy) error {
	if err := hierarchy.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for cycles before creating
	if s.wouldCreateGroupCycle(hierarchy.TenantID, hierarchy.ParentGroupID, hierarchy.ChildGroupID) {
		return NewIAMError(ErrHierarchyCycle, "iam.inheritance.group.create",
			fmt.Sprintf("adding parent %s to child %s would create a cycle", hierarchy.ParentGroupID, hierarchy.ChildGroupID), nil)
	}

	key := s.groupHierarchyKey(hierarchy.TenantID, hierarchy.ParentGroupID, hierarchy.ChildGroupID)
	if _, exists := s.groupHierarchies[key]; exists {
		return NewIAMError(ErrHierarchyAlreadyExists, "iam.inheritance.group.create",
			fmt.Sprintf("hierarchy relationship already exists: %s -> %s", hierarchy.ParentGroupID, hierarchy.ChildGroupID), nil)
	}

	// Store the hierarchy
	s.groupHierarchies[key] = hierarchy

	// Update adjacency lists
	childKey := s.groupKey(hierarchy.TenantID, hierarchy.ChildGroupID)
	if s.groupParents[childKey] == nil {
		s.groupParents[childKey] = make(map[string]*GroupHierarchy)
	}
	s.groupParents[childKey][hierarchy.ParentGroupID] = hierarchy

	parentKey := s.groupKey(hierarchy.TenantID, hierarchy.ParentGroupID)
	if s.groupChildren[parentKey] == nil {
		s.groupChildren[parentKey] = make(map[string]*GroupHierarchy)
	}
	s.groupChildren[parentKey][hierarchy.ChildGroupID] = hierarchy

	return nil
}

// GetGroupHierarchy retrieves a specific group hierarchy relationship.
func (s *MemoryHierarchyStorage) GetGroupHierarchy(ctx context.Context, tenantID, parentGroupID, childGroupID string) (*GroupHierarchy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.groupHierarchyKey(tenantID, parentGroupID, childGroupID)
	hierarchy, exists := s.groupHierarchies[key]
	if !exists {
		return nil, NewIAMError(ErrHierarchyNotFound, "iam.inheritance.group.get",
			fmt.Sprintf("hierarchy not found: %s -> %s", parentGroupID, childGroupID), nil)
	}

	return hierarchy, nil
}

// ListGroupParents retrieves all parent groups for a given group.
func (s *MemoryHierarchyStorage) ListGroupParents(ctx context.Context, tenantID, groupID string) ([]*GroupHierarchy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.groupKey(tenantID, groupID)
	parents := s.groupParents[key]
	if parents == nil {
		return []*GroupHierarchy{}, nil
	}

	// Convert map to slice
	result := make([]*GroupHierarchy, 0, len(parents))
	for _, h := range parents {
		result = append(result, h)
	}

	return result, nil
}

// ListGroupChildren retrieves all child groups for a given group.
func (s *MemoryHierarchyStorage) ListGroupChildren(ctx context.Context, tenantID, groupID string) ([]*GroupHierarchy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.groupKey(tenantID, groupID)
	children := s.groupChildren[key]
	if children == nil {
		return []*GroupHierarchy{}, nil
	}

	// Convert map to slice
	result := make([]*GroupHierarchy, 0, len(children))
	for _, h := range children {
		result = append(result, h)
	}

	return result, nil
}

// DeleteGroupHierarchy removes a parent-child relationship.
func (s *MemoryHierarchyStorage) DeleteGroupHierarchy(ctx context.Context, tenantID, parentGroupID, childGroupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.groupHierarchyKey(tenantID, parentGroupID, childGroupID)
	if _, exists := s.groupHierarchies[key]; !exists {
		return NewIAMError(ErrHierarchyNotFound, "iam.inheritance.group.delete",
			fmt.Sprintf("hierarchy not found: %s -> %s", parentGroupID, childGroupID), nil)
	}

	// Remove from main storage
	delete(s.groupHierarchies, key)

	// Remove from adjacency lists
	childKey := s.groupKey(tenantID, childGroupID)
	if parents := s.groupParents[childKey]; parents != nil {
		delete(parents, parentGroupID)
		if len(parents) == 0 {
			delete(s.groupParents, childKey)
		}
	}

	parentKey := s.groupKey(tenantID, parentGroupID)
	if children := s.groupChildren[parentKey]; children != nil {
		delete(children, childGroupID)
		if len(children) == 0 {
			delete(s.groupChildren, parentKey)
		}
	}

	return nil
}

// GetGroupAncestors retrieves all ancestor groups (transitive closure) for a group.
func (s *MemoryHierarchyStorage) GetGroupAncestors(ctx context.Context, tenantID, groupID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.getGroupAncestorsRecursive(tenantID, groupID, make(map[string]bool)), nil
}

// getGroupAncestorsRecursive performs DFS to find all ancestors.
func (s *MemoryHierarchyStorage) getGroupAncestorsRecursive(tenantID, groupID string, visited map[string]bool) []string {
	if visited[groupID] {
		return nil
	}
	visited[groupID] = true

	var ancestors []string
	key := s.groupKey(tenantID, groupID)
	parents := s.groupParents[key]

	for parentID := range parents {
		ancestors = append(ancestors, parentID)
		// Recursively get ancestors of this parent
		ancestors = append(ancestors, s.getGroupAncestorsRecursive(tenantID, parentID, visited)...)
	}

	return ancestors
}

// DetectGroupCycle checks if adding a parent-child relationship would create a cycle.
func (s *MemoryHierarchyStorage) DetectGroupCycle(ctx context.Context, tenantID, parentGroupID, childGroupID string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.wouldCreateGroupCycle(tenantID, parentGroupID, childGroupID), nil
}

// wouldCreateGroupCycle checks if adding parent->child would create a cycle.
func (s *MemoryHierarchyStorage) wouldCreateGroupCycle(tenantID, parentGroupID, childGroupID string) bool {
	// If parent is a descendant of child, adding this edge creates a cycle
	descendants := s.getGroupDescendantsRecursive(tenantID, childGroupID, make(map[string]bool))
	for _, desc := range descendants {
		if desc == parentGroupID {
			return true
		}
	}
	return false
}

// getGroupDescendantsRecursive performs DFS to find all descendants.
func (s *MemoryHierarchyStorage) getGroupDescendantsRecursive(tenantID, groupID string, visited map[string]bool) []string {
	if visited[groupID] {
		return nil
	}
	visited[groupID] = true

	var descendants []string
	key := s.groupKey(tenantID, groupID)
	children := s.groupChildren[key]

	for childID := range children {
		descendants = append(descendants, childID)
		// Recursively get descendants of this child
		descendants = append(descendants, s.getGroupDescendantsRecursive(tenantID, childID, visited)...)
	}

	return descendants
}

// CacheEffectivePermissions stores computed effective permissions for a subject.
func (s *MemoryHierarchyStorage) CacheEffectivePermissions(ctx context.Context, permissions *EffectivePermissions) error {
	if err := permissions.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.permissionsKey(permissions.TenantID, permissions.SubjectID)
	s.permissionsCache[key] = permissions

	return nil
}

// GetCachedEffectivePermissions retrieves cached effective permissions for a subject.
func (s *MemoryHierarchyStorage) GetCachedEffectivePermissions(ctx context.Context, tenantID, subjectID string) (*EffectivePermissions, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.permissionsKey(tenantID, subjectID)
	permissions, exists := s.permissionsCache[key]
	if !exists {
		return nil, NewIAMError(ErrEffectivePermissionsNotFound, "iam.inheritance.permissions.get",
			fmt.Sprintf("cached permissions not found for subject %s", subjectID), nil)
	}

	return permissions, nil
}

// InvalidateEffectivePermissions removes cached permissions for a subject.
func (s *MemoryHierarchyStorage) InvalidateEffectivePermissions(ctx context.Context, tenantID, subjectID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.permissionsKey(tenantID, subjectID)
	delete(s.permissionsCache, key)

	return nil
}

// Helper methods for key generation

func (s *MemoryHierarchyStorage) roleHierarchyKey(tenantID, parentID, childID string) string {
	return fmt.Sprintf("%s:%s:%s", tenantID, parentID, childID)
}

func (s *MemoryHierarchyStorage) roleKey(tenantID, roleID string) string {
	return fmt.Sprintf("%s:%s", tenantID, roleID)
}

func (s *MemoryHierarchyStorage) groupHierarchyKey(tenantID, parentID, childID string) string {
	return fmt.Sprintf("%s:%s:%s", tenantID, parentID, childID)
}

func (s *MemoryHierarchyStorage) groupKey(tenantID, groupID string) string {
	return fmt.Sprintf("%s:%s", tenantID, groupID)
}

func (s *MemoryHierarchyStorage) permissionsKey(tenantID, subjectID string) string {
	return fmt.Sprintf("%s:%s", tenantID, subjectID)
}
