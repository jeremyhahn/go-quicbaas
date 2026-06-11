package iam

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupInheritanceTest(t *testing.T) (*InheritanceManager, *MemoryStorage, context.Context) {
	ctx := context.Background()

	// Use the existing MemoryStorage which implements all storage interfaces
	storage := NewMemoryStorage()

	// Create hierarchy storage
	hierarchyStorage := NewMemoryHierarchyStorage()

	// Create manager
	manager, err := NewInheritanceManager(InheritanceManagerOptions{
		HierarchyStorage: hierarchyStorage,
		RoleStorage:      storage,
		GroupStorage:     storage,
		UserStorage:      storage,
	})
	require.NoError(t, err)
	require.NotNil(t, manager)

	return manager, storage, ctx
}

func TestNewInheritanceManager(t *testing.T) {
	tests := []struct {
		name        string
		opts        InheritanceManagerOptions
		expectError bool
	}{
		{
			name: "valid options",
			opts: InheritanceManagerOptions{
				HierarchyStorage: NewMemoryHierarchyStorage(),
				RoleStorage:      NewMemoryStorage(),
				GroupStorage:     NewMemoryStorage(),
				UserStorage:      NewMemoryStorage(),
			},
			expectError: false,
		},
		{
			name: "missing hierarchy storage",
			opts: InheritanceManagerOptions{
				RoleStorage:  NewMemoryStorage(),
				GroupStorage: NewMemoryStorage(),
				UserStorage:  NewMemoryStorage(),
			},
			expectError: true,
		},
		{
			name: "missing role storage",
			opts: InheritanceManagerOptions{
				HierarchyStorage: NewMemoryHierarchyStorage(),
				GroupStorage:     NewMemoryStorage(),
				UserStorage:      NewMemoryStorage(),
			},
			expectError: true,
		},
		{
			name: "missing group storage",
			opts: InheritanceManagerOptions{
				HierarchyStorage: NewMemoryHierarchyStorage(),
				RoleStorage:      NewMemoryStorage(),
				UserStorage:      NewMemoryStorage(),
			},
			expectError: true,
		},
		{
			name: "missing user storage",
			opts: InheritanceManagerOptions{
				HierarchyStorage: NewMemoryHierarchyStorage(),
				RoleStorage:      NewMemoryStorage(),
				GroupStorage:     NewMemoryStorage(),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewInheritanceManager(tt.opts)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, manager)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manager)
			}
		})
	}
}

func TestRoleHierarchyCreation(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create roles
	adminRole := &Role{
		ID:          "admin",
		Name:        "Administrator",
		TenantID:    tenantID,
		Permissions: []string{"users:read", "users:write", "users:delete"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, adminRole))

	moderatorRole := &Role{
		ID:          "moderator",
		Name:        "Moderator",
		TenantID:    tenantID,
		Permissions: []string{"posts:read", "posts:write"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, moderatorRole))

	// Add parent role
	err := manager.AddRoleParent(ctx, tenantID, "moderator", "admin", 1)
	assert.NoError(t, err)

	// Verify hierarchy
	hierarchy, err := manager.GetRoleHierarchy(ctx, tenantID, "moderator")
	assert.NoError(t, err)
	assert.Len(t, hierarchy, 1)
	assert.Equal(t, "admin", hierarchy[0].ParentRoleID)
	assert.Equal(t, "moderator", hierarchy[0].ChildRoleID)
}

func TestRoleHierarchyCycleDetection(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create roles
	roles := []string{"role-a", "role-b", "role-c"}
	for _, roleID := range roles {
		role := &Role{
			ID:          roleID,
			Name:        roleID,
			TenantID:    tenantID,
			Permissions: []string{},
			Created:     time.Now(),
			Updated:     time.Now(),
		}
		require.NoError(t, storage.CreateRole(ctx, role))
	}

	// Create chain: role-c -> role-b -> role-a
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "role-c", "role-b", 1))
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "role-b", "role-a", 1))

	// Attempt to create cycle: role-a -> role-c (would create cycle)
	err := manager.AddRoleParent(ctx, tenantID, "role-a", "role-c", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestRoleHierarchyRemoval(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create roles
	parentRole := &Role{
		ID:          "parent",
		Name:        "Parent",
		TenantID:    tenantID,
		Permissions: []string{"perm:parent"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, parentRole))

	childRole := &Role{
		ID:          "child",
		Name:        "Child",
		TenantID:    tenantID,
		Permissions: []string{"perm:child"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, childRole))

	// Add parent
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "child", "parent", 1))

	// Verify hierarchy exists
	hierarchy, err := manager.GetRoleHierarchy(ctx, tenantID, "child")
	require.NoError(t, err)
	assert.Len(t, hierarchy, 1)

	// Remove parent
	err = manager.RemoveRoleParent(ctx, tenantID, "child", "parent")
	assert.NoError(t, err)

	// Verify hierarchy removed
	hierarchy, err = manager.GetRoleHierarchy(ctx, tenantID, "child")
	assert.NoError(t, err)
	assert.Len(t, hierarchy, 0)
}

func TestGroupHierarchyCreation(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create groups
	allUsersGroup := &Group{
		ID:       "all-users",
		Name:     "All Users",
		TenantID: tenantID,
		Members:  []string{"user1", "user2"},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateGroup(ctx, allUsersGroup))

	engineersGroup := &Group{
		ID:       "engineers",
		Name:     "Engineers",
		TenantID: tenantID,
		Members:  []string{"user3"},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateGroup(ctx, engineersGroup))

	// Add parent group with member inheritance
	err := manager.AddGroupParent(ctx, tenantID, "engineers", "all-users", true)
	assert.NoError(t, err)

	// Verify hierarchy
	hierarchy, err := manager.GetGroupHierarchy(ctx, tenantID, "engineers")
	assert.NoError(t, err)
	assert.Len(t, hierarchy, 1)
	assert.Equal(t, "all-users", hierarchy[0].ParentGroupID)
	assert.Equal(t, "engineers", hierarchy[0].ChildGroupID)
	assert.True(t, hierarchy[0].InheritMembers)
}

func TestGroupHierarchyCycleDetection(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create groups
	groups := []string{"group-a", "group-b", "group-c"}
	for _, groupID := range groups {
		group := &Group{
			ID:       groupID,
			Name:     groupID,
			TenantID: tenantID,
			Members:  []string{},
			Created:  time.Now(),
			Updated:  time.Now(),
		}
		require.NoError(t, storage.CreateGroup(ctx, group))
	}

	// Create chain: group-c -> group-b -> group-a
	require.NoError(t, manager.AddGroupParent(ctx, tenantID, "group-c", "group-b", false))
	require.NoError(t, manager.AddGroupParent(ctx, tenantID, "group-b", "group-a", false))

	// Attempt to create cycle: group-a -> group-c
	err := manager.AddGroupParent(ctx, tenantID, "group-a", "group-c", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestEffectiveRolePermissions(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create role hierarchy: viewer -> editor -> admin
	viewerRole := &Role{
		ID:          "viewer",
		Name:        "Viewer",
		TenantID:    tenantID,
		Permissions: []string{"read"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, viewerRole))

	editorRole := &Role{
		ID:          "editor",
		Name:        "Editor",
		TenantID:    tenantID,
		Permissions: []string{"write"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, editorRole))

	adminRole := &Role{
		ID:          "admin",
		Name:        "Admin",
		TenantID:    tenantID,
		Permissions: []string{"delete"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, adminRole))

	// Build hierarchy: editor inherits from viewer, admin inherits from editor
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "editor", "viewer", 1))
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "admin", "editor", 1))

	// Get effective permissions for admin role
	effective, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "admin")
	assert.NoError(t, err)
	assert.NotNil(t, effective)

	// Admin should have all permissions: delete (direct), write (from editor), read (from viewer)
	assert.Contains(t, effective.AllPermissions, "delete")
	assert.Contains(t, effective.AllPermissions, "write")
	assert.Contains(t, effective.AllPermissions, "read")
	assert.Len(t, effective.AllPermissions, 3)
}

func TestTransitiveClosureInheritance(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create 4-level hierarchy: leaf -> level2 -> level1 -> root
	roles := map[string][]string{
		"root":   {"perm:root"},
		"level1": {"perm:level1"},
		"level2": {"perm:level2"},
		"leaf":   {"perm:leaf"},
	}

	for roleID, perms := range roles {
		role := &Role{
			ID:          roleID,
			Name:        roleID,
			TenantID:    tenantID,
			Permissions: perms,
			Created:     time.Now(),
			Updated:     time.Now(),
		}
		require.NoError(t, storage.CreateRole(ctx, role))
	}

	// Build hierarchy
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "level1", "root", 1))
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "level2", "level1", 1))
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "leaf", "level2", 1))

	// Leaf should inherit from all ancestors
	effective, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "leaf")
	assert.NoError(t, err)

	assert.Contains(t, effective.AllPermissions, "perm:leaf")
	assert.Contains(t, effective.AllPermissions, "perm:level2")
	assert.Contains(t, effective.AllPermissions, "perm:level1")
	assert.Contains(t, effective.AllPermissions, "perm:root")
	assert.Len(t, effective.AllPermissions, 4)
}

func TestMultiParentInheritance(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create diamond pattern:
	//        root
	//       /    \
	//   parent1  parent2
	//       \    /
	//        child

	roles := map[string][]string{
		"root":    {"perm:root"},
		"parent1": {"perm:parent1"},
		"parent2": {"perm:parent2"},
		"child":   {"perm:child"},
	}

	for roleID, perms := range roles {
		role := &Role{
			ID:          roleID,
			Name:        roleID,
			TenantID:    tenantID,
			Permissions: perms,
			Created:     time.Now(),
			Updated:     time.Now(),
		}
		require.NoError(t, storage.CreateRole(ctx, role))
	}

	// Build diamond
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "parent1", "root", 1))
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "parent2", "root", 2))
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "child", "parent1", 1))
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "child", "parent2", 2))

	// Child should have all permissions (deduplicated)
	effective, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "child")
	assert.NoError(t, err)

	assert.Contains(t, effective.AllPermissions, "perm:child")
	assert.Contains(t, effective.AllPermissions, "perm:parent1")
	assert.Contains(t, effective.AllPermissions, "perm:parent2")
	assert.Contains(t, effective.AllPermissions, "perm:root")
	assert.Len(t, effective.AllPermissions, 4)
}

func TestEffectiveUserGroups(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create groups
	allGroup := &Group{
		ID:       "all",
		Name:     "All",
		TenantID: tenantID,
		Members:  []string{},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateGroup(ctx, allGroup))

	engineersGroup := &Group{
		ID:       "engineers",
		Name:     "Engineers",
		TenantID: tenantID,
		Members:  []string{},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateGroup(ctx, engineersGroup))

	backendGroup := &Group{
		ID:       "backend",
		Name:     "Backend",
		TenantID: tenantID,
		Members:  []string{},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateGroup(ctx, backendGroup))

	// Build hierarchy: backend -> engineers -> all
	require.NoError(t, manager.AddGroupParent(ctx, tenantID, "engineers", "all", true))
	require.NoError(t, manager.AddGroupParent(ctx, tenantID, "backend", "engineers", true))

	// Create user in backend group
	user := &User{
		ID:       "user1",
		Email:    "user1@test.com",
		Name:     "User 1",
		TenantID: tenantID,
		Groups:   []string{"backend"},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateUser(ctx, user))

	// Get effective groups
	groups, err := manager.GetEffectiveUserGroups(ctx, tenantID, "user1")
	assert.NoError(t, err)

	// User should be in all three groups
	assert.Contains(t, groups, "backend")
	assert.Contains(t, groups, "engineers")
	assert.Contains(t, groups, "all")
}

func TestEffectiveUserPermissions(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create roles
	viewerRole := &Role{
		ID:          "viewer",
		Name:        "Viewer",
		TenantID:    tenantID,
		Permissions: []string{"read"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, viewerRole))

	editorRole := &Role{
		ID:          "editor",
		Name:        "Editor",
		TenantID:    tenantID,
		Permissions: []string{"write"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, editorRole))

	// Build role hierarchy
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "editor", "viewer", 1))

	// Create user with editor role
	user := &User{
		ID:       "user1",
		Email:    "user1@test.com",
		Name:     "User 1",
		TenantID: tenantID,
		Roles:    []string{"editor"},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateUser(ctx, user))

	// Get effective permissions
	effective, err := manager.GetEffectiveUserPermissions(ctx, tenantID, "user1")
	assert.NoError(t, err)

	// User should have both read and write permissions
	assert.Contains(t, effective.AllPermissions, "read")
	assert.Contains(t, effective.AllPermissions, "write")
}

func TestCacheInvalidation(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create roles
	role := &Role{
		ID:          "role1",
		Name:        "Role 1",
		TenantID:    tenantID,
		Permissions: []string{"perm:1"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, role))

	// Compute effective permissions (should cache)
	effective1, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "role1")
	assert.NoError(t, err)
	time1 := effective1.ComputedAt

	// Get again (should be cached)
	effective2, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "role1")
	assert.NoError(t, err)
	time2 := effective2.ComputedAt

	// Should be same cached result
	assert.Equal(t, time1, time2)

	// Invalidate cache
	err = manager.InvalidateCache(tenantID, "role1")
	assert.NoError(t, err)

	// Get again (should recompute)
	effective3, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "role1")
	assert.NoError(t, err)
	time3 := effective3.ComputedAt

	// Should be newly computed
	assert.True(t, time3.After(time2) || time3.Equal(time2))
}

func TestCacheInvalidationOnHierarchyChange(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create roles
	parentRole := &Role{
		ID:          "parent",
		Name:        "Parent",
		TenantID:    tenantID,
		Permissions: []string{"perm:parent"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, parentRole))

	childRole := &Role{
		ID:          "child",
		Name:        "Child",
		TenantID:    tenantID,
		Permissions: []string{"perm:child"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, childRole))

	// Compute initial effective permissions
	effective1, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "child")
	assert.NoError(t, err)
	assert.Len(t, effective1.AllPermissions, 1)
	assert.Contains(t, effective1.AllPermissions, "perm:child")

	// Add parent (should invalidate cache)
	require.NoError(t, manager.AddRoleParent(ctx, tenantID, "child", "parent", 1))

	// Get effective permissions again (should be recomputed with parent)
	effective2, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "child")
	assert.NoError(t, err)
	assert.Len(t, effective2.AllPermissions, 2)
	assert.Contains(t, effective2.AllPermissions, "perm:child")
	assert.Contains(t, effective2.AllPermissions, "perm:parent")
}

func TestRoleHierarchyValidation(t *testing.T) {
	hierarchy := &RoleHierarchy{
		ID:           uuid.New().String(),
		TenantID:     "tenant1",
		ParentRoleID: "parent",
		ChildRoleID:  "child",
		Priority:     1,
		Created:      time.Now(),
	}

	err := hierarchy.Validate()
	assert.NoError(t, err)

	// Test missing ID
	h := *hierarchy
	h.ID = ""
	assert.Error(t, h.Validate())

	// Test missing tenant
	h = *hierarchy
	h.TenantID = ""
	assert.Error(t, h.Validate())

	// Test missing parent
	h = *hierarchy
	h.ParentRoleID = ""
	assert.Error(t, h.Validate())

	// Test missing child
	h = *hierarchy
	h.ChildRoleID = ""
	assert.Error(t, h.Validate())

	// Test self-parent
	h = *hierarchy
	h.ParentRoleID = h.ChildRoleID
	assert.Error(t, h.Validate())

	// Test negative priority
	h = *hierarchy
	h.Priority = -1
	assert.Error(t, h.Validate())
}

func TestGroupHierarchyValidation(t *testing.T) {
	hierarchy := &GroupHierarchy{
		ID:             uuid.New().String(),
		TenantID:       "tenant1",
		ParentGroupID:  "parent",
		ChildGroupID:   "child",
		InheritMembers: true,
		Created:        time.Now(),
	}

	err := hierarchy.Validate()
	assert.NoError(t, err)

	// Test missing ID
	h := *hierarchy
	h.ID = ""
	assert.Error(t, h.Validate())

	// Test missing tenant
	h = *hierarchy
	h.TenantID = ""
	assert.Error(t, h.Validate())

	// Test missing parent
	h = *hierarchy
	h.ParentGroupID = ""
	assert.Error(t, h.Validate())

	// Test missing child
	h = *hierarchy
	h.ChildGroupID = ""
	assert.Error(t, h.Validate())

	// Test self-parent
	h = *hierarchy
	h.ParentGroupID = h.ChildGroupID
	assert.Error(t, h.Validate())
}

func TestEffectivePermissionsValidation(t *testing.T) {
	perms := &EffectivePermissions{
		SubjectID:            "subject1",
		TenantID:             "tenant1",
		DirectPermissions:    []string{"perm1"},
		InheritedPermissions: make(map[string][]string),
		AllPermissions:       []string{"perm1"},
		ComputedAt:           time.Now(),
	}

	err := perms.Validate()
	assert.NoError(t, err)

	// Test missing subject ID
	p := *perms
	p.SubjectID = ""
	assert.Error(t, p.Validate())

	// Test missing tenant ID
	p = *perms
	p.TenantID = ""
	assert.Error(t, p.Validate())
}

func TestAddRoleParentNonexistentRoles(t *testing.T) {
	manager, _, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Try to add parent with nonexistent child
	err := manager.AddRoleParent(ctx, tenantID, "nonexistent-child", "nonexistent-parent", 1)
	assert.Error(t, err)
}

func TestRemoveNonexistentRoleParent(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Create roles
	role1 := &Role{
		ID:       "role1",
		Name:     "Role 1",
		TenantID: tenantID,
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, role1))

	role2 := &Role{
		ID:       "role2",
		Name:     "Role 2",
		TenantID: tenantID,
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateRole(ctx, role2))

	// Try to remove nonexistent hierarchy
	err := manager.RemoveRoleParent(ctx, tenantID, "role1", "role2")
	assert.Error(t, err)
}

func TestGetEffectiveRolePermissionsNonexistent(t *testing.T) {
	manager, _, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Try to get permissions for nonexistent role
	_, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "nonexistent")
	assert.Error(t, err)
}

func TestGetEffectiveUserGroupsNonexistentUser(t *testing.T) {
	manager, _, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Try to get groups for nonexistent user
	_, err := manager.GetEffectiveUserGroups(ctx, tenantID, "nonexistent")
	assert.Error(t, err)
}

func TestGetEffectiveUserPermissionsNonexistentUser(t *testing.T) {
	manager, _, ctx := setupInheritanceTest(t)
	tenantID := "test-tenant"

	// Try to get permissions for nonexistent user
	_, err := manager.GetEffectiveUserPermissions(ctx, tenantID, "nonexistent")
	assert.Error(t, err)
}

func TestMemoryHierarchyStorage_GetRoleHierarchy_Success(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	hierarchy := &RoleHierarchy{
		ID:           "rh1",
		TenantID:     "t1",
		ParentRoleID: "parent-role",
		ChildRoleID:  "child-role",
		Priority:     1,
		Created:      time.Now(),
	}
	if err := storage.CreateRoleHierarchy(ctx, hierarchy); err != nil {
		t.Fatalf("CreateRoleHierarchy failed: %v", err)
	}

	result, err := storage.GetRoleHierarchy(ctx, "t1", "parent-role", "child-role")
	if err != nil {
		t.Fatalf("GetRoleHierarchy failed: %v", err)
	}
	if result.ID != "rh1" {
		t.Fatalf("expected ID rh1, got %s", result.ID)
	}
}

func TestMemoryHierarchyStorage_GetRoleHierarchy_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	_, err := storage.GetRoleHierarchy(ctx, "t1", "nonexistent-parent", "nonexistent-child")
	if err == nil {
		t.Fatal("expected error for nonexistent hierarchy")
	}
}

func TestMemoryHierarchyStorage_DetectRoleCycle_NoCycle(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	// Create A -> B hierarchy
	h1 := &RoleHierarchy{
		ID:           "rh_ab",
		TenantID:     "t1",
		ParentRoleID: "roleA",
		ChildRoleID:  "roleB",
		Priority:     1,
		Created:      time.Now(),
	}
	if err := storage.CreateRoleHierarchy(ctx, h1); err != nil {
		t.Fatalf("CreateRoleHierarchy failed: %v", err)
	}

	// Check that adding B -> C does not create a cycle
	hasCycle, err := storage.DetectRoleCycle(ctx, "t1", "roleB", "roleC")
	if err != nil {
		t.Fatalf("DetectRoleCycle failed: %v", err)
	}
	if hasCycle {
		t.Fatal("expected no cycle for B -> C")
	}
}

func TestMemoryHierarchyStorage_DetectRoleCycle_WithCycle(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	// Create A -> B and B -> C
	h1 := &RoleHierarchy{
		ID:           "rh_ab",
		TenantID:     "t1",
		ParentRoleID: "roleA",
		ChildRoleID:  "roleB",
		Priority:     1,
		Created:      time.Now(),
	}
	h2 := &RoleHierarchy{
		ID:           "rh_bc",
		TenantID:     "t1",
		ParentRoleID: "roleB",
		ChildRoleID:  "roleC",
		Priority:     1,
		Created:      time.Now(),
	}
	if err := storage.CreateRoleHierarchy(ctx, h1); err != nil {
		t.Fatalf("CreateRoleHierarchy A->B failed: %v", err)
	}
	if err := storage.CreateRoleHierarchy(ctx, h2); err != nil {
		t.Fatalf("CreateRoleHierarchy B->C failed: %v", err)
	}

	// Detect cycle: adding C -> A would create A -> B -> C -> A
	hasCycle, err := storage.DetectRoleCycle(ctx, "t1", "roleC", "roleA")
	if err != nil {
		t.Fatalf("DetectRoleCycle failed: %v", err)
	}
	if !hasCycle {
		t.Fatal("expected cycle detected for C -> A")
	}
}

func TestMemoryHierarchyStorage_GetGroupHierarchy_Success(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	hierarchy := &GroupHierarchy{
		ID:             "gh1",
		TenantID:       "t1",
		ParentGroupID:  "parent-group",
		ChildGroupID:   "child-group",
		InheritMembers: true,
		Created:        time.Now(),
	}
	if err := storage.CreateGroupHierarchy(ctx, hierarchy); err != nil {
		t.Fatalf("CreateGroupHierarchy failed: %v", err)
	}

	result, err := storage.GetGroupHierarchy(ctx, "t1", "parent-group", "child-group")
	if err != nil {
		t.Fatalf("GetGroupHierarchy failed: %v", err)
	}
	if result.ID != "gh1" {
		t.Fatalf("expected ID gh1, got %s", result.ID)
	}
	if !result.InheritMembers {
		t.Fatal("expected InheritMembers to be true")
	}
}

func TestMemoryHierarchyStorage_GetGroupHierarchy_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	_, err := storage.GetGroupHierarchy(ctx, "t1", "no-parent", "no-child")
	if err == nil {
		t.Fatal("expected error for nonexistent group hierarchy")
	}
}

func TestMemoryHierarchyStorage_ListGroupChildren_Success(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	// Create parent -> child1 and parent -> child2
	h1 := &GroupHierarchy{
		ID:             "gh_pc1",
		TenantID:       "t1",
		ParentGroupID:  "parent-group",
		ChildGroupID:   "child1",
		InheritMembers: false,
		Created:        time.Now(),
	}
	h2 := &GroupHierarchy{
		ID:             "gh_pc2",
		TenantID:       "t1",
		ParentGroupID:  "parent-group",
		ChildGroupID:   "child2",
		InheritMembers: false,
		Created:        time.Now(),
	}
	if err := storage.CreateGroupHierarchy(ctx, h1); err != nil {
		t.Fatalf("CreateGroupHierarchy 1 failed: %v", err)
	}
	if err := storage.CreateGroupHierarchy(ctx, h2); err != nil {
		t.Fatalf("CreateGroupHierarchy 2 failed: %v", err)
	}

	children, err := storage.ListGroupChildren(ctx, "t1", "parent-group")
	if err != nil {
		t.Fatalf("ListGroupChildren failed: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
}

func TestMemoryHierarchyStorage_ListGroupChildren_Empty(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	children, err := storage.ListGroupChildren(ctx, "t1", "no-children")
	if err != nil {
		t.Fatalf("ListGroupChildren failed: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestMemoryHierarchyStorage_DeleteGroupHierarchy_Success(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	hierarchy := &GroupHierarchy{
		ID:             "gh_del",
		TenantID:       "t1",
		ParentGroupID:  "parent",
		ChildGroupID:   "child",
		InheritMembers: false,
		Created:        time.Now(),
	}
	if err := storage.CreateGroupHierarchy(ctx, hierarchy); err != nil {
		t.Fatalf("CreateGroupHierarchy failed: %v", err)
	}

	if err := storage.DeleteGroupHierarchy(ctx, "t1", "parent", "child"); err != nil {
		t.Fatalf("DeleteGroupHierarchy failed: %v", err)
	}

	// Verify it's gone
	_, err := storage.GetGroupHierarchy(ctx, "t1", "parent", "child")
	if err == nil {
		t.Fatal("expected error after deleting hierarchy")
	}

	// Verify parent has no children
	children, err := storage.ListGroupChildren(ctx, "t1", "parent")
	if err != nil {
		t.Fatalf("ListGroupChildren failed: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("expected 0 children after delete, got %d", len(children))
	}
}

func TestMemoryHierarchyStorage_DeleteGroupHierarchy_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	err := storage.DeleteGroupHierarchy(ctx, "t1", "no-parent", "no-child")
	if err == nil {
		t.Fatal("expected error deleting nonexistent group hierarchy")
	}
}

func TestMemoryHierarchyStorage_DetectGroupCycle_NoCycle(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	h := &GroupHierarchy{
		ID:             "gh_cycle1",
		TenantID:       "t1",
		ParentGroupID:  "groupA",
		ChildGroupID:   "groupB",
		InheritMembers: false,
		Created:        time.Now(),
	}
	if err := storage.CreateGroupHierarchy(ctx, h); err != nil {
		t.Fatalf("CreateGroupHierarchy failed: %v", err)
	}

	hasCycle, err := storage.DetectGroupCycle(ctx, "t1", "groupB", "groupC")
	if err != nil {
		t.Fatalf("DetectGroupCycle failed: %v", err)
	}
	if hasCycle {
		t.Fatal("expected no cycle for B -> C")
	}
}

func TestMemoryHierarchyStorage_DetectGroupCycle_WithCycle(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	// Create A -> B and B -> C
	h1 := &GroupHierarchy{
		ID:             "gh_cyc_ab",
		TenantID:       "t1",
		ParentGroupID:  "groupA",
		ChildGroupID:   "groupB",
		InheritMembers: false,
		Created:        time.Now(),
	}
	h2 := &GroupHierarchy{
		ID:             "gh_cyc_bc",
		TenantID:       "t1",
		ParentGroupID:  "groupB",
		ChildGroupID:   "groupC",
		InheritMembers: false,
		Created:        time.Now(),
	}
	if err := storage.CreateGroupHierarchy(ctx, h1); err != nil {
		t.Fatalf("CreateGroupHierarchy A->B failed: %v", err)
	}
	if err := storage.CreateGroupHierarchy(ctx, h2); err != nil {
		t.Fatalf("CreateGroupHierarchy B->C failed: %v", err)
	}

	hasCycle, err := storage.DetectGroupCycle(ctx, "t1", "groupC", "groupA")
	if err != nil {
		t.Fatalf("DetectGroupCycle failed: %v", err)
	}
	if !hasCycle {
		t.Fatal("expected cycle detected for C -> A")
	}
}

func TestMemoryHierarchyStorage_CacheEffectivePermissions_InvalidInput(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	// Missing SubjectID
	p1 := &EffectivePermissions{
		TenantID: "t1",
	}
	if err := storage.CacheEffectivePermissions(ctx, p1); err == nil {
		t.Fatal("expected error for missing SubjectID")
	}

	// Missing TenantID
	p2 := &EffectivePermissions{
		SubjectID: "user1",
	}
	if err := storage.CacheEffectivePermissions(ctx, p2); err == nil {
		t.Fatal("expected error for missing TenantID")
	}
}

// =============================================================================
// InheritanceManager - RemoveGroupParent coverage (0% -> 100%)
// =============================================================================

func TestInheritanceManager_RemoveGroupParent_Success(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)

	tenantID := "tenant-1"

	// Create a tenant first
	tenant := &Tenant{ID: tenantID, Name: "Test Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create parent and child groups
	parentGroup := &Group{
		ID:       "parent-group",
		TenantID: tenantID,
		Name:     "Parent Group",
		Members:  []string{"user-1", "user-2"},
	}
	childGroup := &Group{
		ID:       "child-group",
		TenantID: tenantID,
		Name:     "Child Group",
	}

	if err := storage.CreateGroup(ctx, parentGroup); err != nil {
		t.Fatalf("CreateGroup (parent) failed: %v", err)
	}
	if err := storage.CreateGroup(ctx, childGroup); err != nil {
		t.Fatalf("CreateGroup (child) failed: %v", err)
	}

	// Add parent relationship with inheritMembers = true
	err := manager.AddGroupParent(ctx, tenantID, childGroup.ID, parentGroup.ID, true)
	if err != nil {
		t.Fatalf("AddGroupParent failed: %v", err)
	}

	// Now remove the relationship
	err = manager.RemoveGroupParent(ctx, tenantID, childGroup.ID, parentGroup.ID)
	if err != nil {
		t.Fatalf("RemoveGroupParent failed: %v", err)
	}

	// Verify relationship is removed
	_, err = manager.GetGroupHierarchy(ctx, tenantID, childGroup.ID)
	// Should return empty or the relationship should be gone
}

func TestInheritanceManager_RemoveGroupParent_NonExistent(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)

	tenantID := "tenant-1"
	tenant := &Tenant{ID: tenantID, Name: "Test Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Try to remove a non-existent relationship
	err := manager.RemoveGroupParent(ctx, tenantID, "child", "parent")
	if err == nil {
		t.Error("expected error when removing non-existent relationship")
	}
}

func TestInheritanceManager_RemoveGroupParent_NoInheritMembers(t *testing.T) {
	manager, storage, ctx := setupInheritanceTest(t)

	tenantID := "tenant-1"
	tenant := &Tenant{ID: tenantID, Name: "Test Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	parentGroup := &Group{
		ID:       "parent-group-2",
		TenantID: tenantID,
		Name:     "Parent Group 2",
		Members:  []string{},
	}
	childGroup := &Group{
		ID:       "child-group-2",
		TenantID: tenantID,
		Name:     "Child Group 2",
	}

	if err := storage.CreateGroup(ctx, parentGroup); err != nil {
		t.Fatalf("CreateGroup (parent) failed: %v", err)
	}
	if err := storage.CreateGroup(ctx, childGroup); err != nil {
		t.Fatalf("CreateGroup (child) failed: %v", err)
	}

	// Add parent relationship with inheritMembers = false
	err := manager.AddGroupParent(ctx, tenantID, childGroup.ID, parentGroup.ID, false)
	if err != nil {
		t.Fatalf("AddGroupParent failed: %v", err)
	}

	// Remove the relationship
	err = manager.RemoveGroupParent(ctx, tenantID, childGroup.ID, parentGroup.ID)
	if err != nil {
		t.Fatalf("RemoveGroupParent failed: %v", err)
	}
}

// =============================================================================
// SessionExt.Validate - Additional coverage for error paths
// =============================================================================

func TestSessionExtValidate_MissingUserID(t *testing.T) {
	session := &SessionExt{
		ID:       "session-1",
		TenantID: "tenant-1",
		// Missing UserID
		TokenHash: "hash123",
	}

	err := session.Validate()
	if err == nil {
		t.Error("expected validation error for missing user ID")
	}
}

func TestSessionExtValidate_MissingTokenHash(t *testing.T) {
	session := &SessionExt{
		ID:       "session-1",
		UserID:   "user-1",
		TenantID: "tenant-1",
		// Missing TokenHash
	}

	err := session.Validate()
	if err == nil {
		t.Error("expected validation error for missing token hash")
	}
}

func TestSessionExtValidate_MissingSessionID(t *testing.T) {
	session := &SessionExt{
		// Missing ID
		UserID:    "user-1",
		TenantID:  "tenant-1",
		TokenHash: "hash123",
	}

	err := session.Validate()
	if err == nil {
		t.Error("expected validation error for missing session ID")
	}
}

func TestSessionExtValidate_Valid(t *testing.T) {
	session := &SessionExt{
		ID:        "session-1",
		UserID:    "user-1",
		TenantID:  "tenant-1",
		TokenHash: "hash123",
	}

	err := session.Validate()
	if err != nil {
		t.Errorf("expected no error for valid session, got: %v", err)
	}
}

// =============================================================================
// MemoryStorage - DeleteTenant coverage (50% -> 100%)
// =============================================================================

func TestMemoryStorage_DeleteTenant_WithCascade(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenantID := "cascade-tenant"

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Cascade Test Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create users with emails
	user1 := &User{ID: "user-1", TenantID: tenantID, Email: "user1@test.com", Name: "User 1"}
	user2 := &User{ID: "user-2", TenantID: tenantID, Email: "user2@test.com", Name: "User 2"}
	if err := storage.CreateUser(ctx, user1); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := storage.CreateUser(ctx, user2); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Create groups
	group := &Group{ID: "group-1", TenantID: tenantID, Name: "Test Group"}
	if err := storage.CreateGroup(ctx, group); err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	// Create roles
	role := &Role{ID: "role-1", TenantID: tenantID, Name: "Test Role"}
	if err := storage.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	// Create policy
	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test-policy", Tenant: tenantID},
		Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
	}
	if err := storage.CreatePolicy(ctx, tenantID, policy); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	// Delete tenant - should cascade delete all resources
	err := storage.DeleteTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("DeleteTenant failed: %v", err)
	}

	// Verify cascade deletion
	_, err = storage.GetUser(ctx, tenantID, "user-1")
	if err == nil {
		t.Error("user should be deleted after tenant deletion")
	}

	_, err = storage.GetGroup(ctx, tenantID, "group-1")
	if err == nil {
		t.Error("group should be deleted after tenant deletion")
	}

	_, err = storage.GetRole(ctx, tenantID, "role-1")
	if err == nil {
		t.Error("role should be deleted after tenant deletion")
	}

	_, err = storage.GetPolicy(ctx, tenantID, "test-policy")
	if err == nil {
		t.Error("policy should be deleted after tenant deletion")
	}
}

func TestMemoryStorage_DeleteTenant_NotFound(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	err := storage.DeleteTenant(ctx, "non-existent-tenant")
	if err == nil {
		t.Error("expected error when deleting non-existent tenant")
	}
}

// =============================================================================
// matchesConsentFilter - Additional coverage
// =============================================================================

func TestMemoryConsentStorage_ListConsents_FilterByStatus(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	expires := time.Now().Add(24 * time.Hour)

	// Create consents with different statuses using CreateConsent
	_, err := storage.CreateConsent(ctx, CreateConsentOptions{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Purpose:   "marketing",
		Status:    ConsentStatusGranted,
		Version:   "1.0",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}

	_, err = storage.CreateConsent(ctx, CreateConsentOptions{
		TenantID:  "tenant-1",
		UserID:    "user-2",
		Purpose:   "analytics",
		Status:    ConsentStatusWithdrawn,
		Version:   "1.0",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}

	// Filter by status
	consents, err := storage.ListConsents(ctx, ListConsentsOptions{
		TenantID: "tenant-1",
		Status:   ConsentStatusGranted,
	})
	if err != nil {
		t.Fatalf("ListConsents failed: %v", err)
	}

	if len(consents) != 1 {
		t.Errorf("expected 1 consent with granted status, got %d", len(consents))
	}
}

func TestMemoryConsentStorage_ListConsents_FilterByPurpose(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	expires := time.Now().Add(24 * time.Hour)

	_, err := storage.CreateConsent(ctx, CreateConsentOptions{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Purpose:   "marketing",
		Status:    ConsentStatusGranted,
		Version:   "1.0",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}

	_, err = storage.CreateConsent(ctx, CreateConsentOptions{
		TenantID:  "tenant-1",
		UserID:    "user-2",
		Purpose:   "analytics",
		Status:    ConsentStatusGranted,
		Version:   "1.0",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}

	// Filter by purpose
	consents, err := storage.ListConsents(ctx, ListConsentsOptions{
		TenantID: "tenant-1",
		Purpose:  "marketing",
	})
	if err != nil {
		t.Fatalf("ListConsents failed: %v", err)
	}

	if len(consents) != 1 {
		t.Errorf("expected 1 consent with marketing purpose, got %d", len(consents))
	}
}

// =============================================================================
// UpdateConsent - Additional coverage
// =============================================================================

func TestMemoryConsentStorage_UpdateConsent_Success(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	expires := time.Now().Add(24 * time.Hour)

	consent, err := storage.CreateConsent(ctx, CreateConsentOptions{
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Purpose:   "marketing",
		Status:    ConsentStatusGranted,
		Version:   "1.0",
		ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}

	// Update the consent
	newExpires := time.Now().Add(48 * time.Hour)
	updated, err := storage.UpdateConsent(ctx, consent.ID, UpdateConsentOptions{
		Status:    ConsentStatusWithdrawn,
		Version:   "2.0",
		ExpiresAt: &newExpires,
		IPAddress: "192.168.1.1",
		UserAgent: "TestAgent/1.0",
		Metadata:  map[string]string{"key": "value"},
	})
	if err != nil {
		t.Fatalf("UpdateConsent failed: %v", err)
	}

	if updated.Status != ConsentStatusWithdrawn {
		t.Errorf("expected status %s, got %s", ConsentStatusWithdrawn, updated.Status)
	}
	if updated.Version != "2.0" {
		t.Errorf("expected version 2.0, got %s", updated.Version)
	}
	if updated.IPAddress != "192.168.1.1" {
		t.Errorf("expected IP 192.168.1.1, got %s", updated.IPAddress)
	}
}

func TestMemoryConsentStorage_UpdateConsent_NotFound(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	_, err := storage.UpdateConsent(ctx, "non-existent", UpdateConsentOptions{
		Status: ConsentStatusWithdrawn,
	})
	if err == nil {
		t.Error("expected error when updating non-existent consent")
	}
}

func TestMemoryConsentStorage_UpdateConsent_ToGranted(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	// Create a withdrawn consent
	consent, err := storage.CreateConsent(ctx, CreateConsentOptions{
		TenantID: "tenant-1",
		UserID:   "user-1",
		Purpose:  "marketing",
		Status:   ConsentStatusWithdrawn,
		Version:  "1.0",
	})
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}

	// Update to granted
	updated, err := storage.UpdateConsent(ctx, consent.ID, UpdateConsentOptions{
		Status: ConsentStatusGranted,
	})
	if err != nil {
		t.Fatalf("UpdateConsent failed: %v", err)
	}

	if updated.Status != ConsentStatusGranted {
		t.Errorf("expected status %s, got %s", ConsentStatusGranted, updated.Status)
	}
	if updated.GrantedAt == nil {
		t.Error("expected GrantedAt to be set")
	}
}

// =============================================================================
// BatchProcessor - processUsers with UpdateExisting and SkipDuplicates
// =============================================================================

type updateCapableTenantMgr struct {
	*mockTenantManager
	users map[string]*User
}

func newUpdateCapableTenantMgr() *updateCapableTenantMgr {
	return &updateCapableTenantMgr{
		mockTenantManager: newMockTenantManager(),
		users:             make(map[string]*User),
	}
}

func (m *updateCapableTenantMgr) CreateUser(ctx context.Context, user *User) error {
	key := user.TenantID + ":" + user.Email
	if _, exists := m.users[key]; exists {
		return ErrUserAlreadyExists
	}
	m.users[key] = user
	return nil
}

func (m *updateCapableTenantMgr) GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error) {
	key := tenantID + ":" + email
	if user, exists := m.users[key]; exists {
		return user, nil
	}
	return nil, ErrUserNotFound
}

func (m *updateCapableTenantMgr) UpdateUser(ctx context.Context, user *User) error {
	key := user.TenantID + ":" + user.Email
	if _, exists := m.users[key]; !exists {
		return ErrUserNotFound
	}
	m.users[key] = user
	return nil
}

func TestBatchProcessor_ProcessUsers_SkipDuplicates(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newUpdateCapableTenantMgr()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// First import - create the user
	items := []UserImportItem{
		{Email: "duplicate@test.com", Name: "Original Name"},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Succeeded != 1 {
		t.Errorf("expected 1 success, got %d", saved.Progress.Succeeded)
	}

	// Second import with SkipDuplicates - should skip
	op2, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		SkipDuplicates: true,
	})
	if err != nil {
		t.Fatalf("ImportUsers (skip duplicates) failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	saved2, _ := storage.GetOperation(ctx, op2.ID)
	if saved2.Progress.Skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", saved2.Progress.Skipped)
	}
}

func TestBatchProcessor_ProcessUsers_UpdateExisting(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newUpdateCapableTenantMgr()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// First import - create the user
	items := []UserImportItem{
		{Email: "update@test.com", Name: "Original Name"},
	}

	_, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Second import with UpdateExisting - should update
	updatedItems := []UserImportItem{
		{Email: "update@test.com", Name: "Updated Name"},
	}

	op2, err := bp.ImportUsers(ctx, tenantID, updatedItems, BatchOptions{
		UpdateExisting: true,
	})
	if err != nil {
		t.Fatalf("ImportUsers (update existing) failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	saved2, _ := storage.GetOperation(ctx, op2.ID)
	if saved2.Progress.Succeeded != 1 {
		t.Errorf("expected 1 success for update, got %d", saved2.Progress.Succeeded)
	}

	// Verify user was updated
	user, _ := tenantMgr.GetUserByEmail(ctx, tenantID, "update@test.com")
	if user.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %s", user.Name)
	}
}

func TestBatchProcessor_ProcessUsers_AtomicMode(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newUpdateCapableTenantMgr()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Mix of valid and invalid items - atomic should fail on first invalid
	items := []UserImportItem{
		{Email: "valid@test.com", Name: "Valid User"},
		{Email: "", Name: "Invalid User"}, // Invalid - missing email
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		Atomic: true,
	})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	// In atomic mode, should fail on first validation error
	if saved.Progress.Failed == 0 {
		t.Error("expected failure in atomic mode for invalid item")
	}
}

// =============================================================================
// SessionPolicy.Validate - Additional coverage
// =============================================================================

func TestSessionPolicy_Validate_AllErrors(t *testing.T) {
	testCases := []struct {
		name    string
		policy  *SessionPolicy
		wantErr bool
	}{
		{
			name:    "valid policy",
			policy:  &SessionPolicy{MaxConcurrent: 5, IdleTimeout: time.Hour},
			wantErr: false,
		},
		{
			name:    "negative max concurrent",
			policy:  &SessionPolicy{MaxConcurrent: -1},
			wantErr: true,
		},
		{
			name:    "negative idle timeout",
			policy:  &SessionPolicy{IdleTimeout: -1},
			wantErr: true,
		},
		{
			name:    "negative absolute timeout",
			policy:  &SessionPolicy{AbsoluteTimeout: -1},
			wantErr: true,
		},
		{
			name:    "negative remember device days",
			policy:  &SessionPolicy{RememberDeviceDays: -1},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// =============================================================================
// SessionExt.IsIdle - Coverage
// =============================================================================

func TestSessionExt_IsIdle(t *testing.T) {
	testCases := []struct {
		name     string
		session  *SessionExt
		policy   *SessionPolicy
		expected bool
	}{
		{
			name: "nil policy",
			session: &SessionExt{
				LastActive: time.Now().Add(-time.Hour),
			},
			policy:   nil,
			expected: false,
		},
		{
			name: "zero idle timeout",
			session: &SessionExt{
				LastActive: time.Now().Add(-time.Hour),
			},
			policy:   &SessionPolicy{IdleTimeout: 0},
			expected: false,
		},
		{
			name: "session is idle",
			session: &SessionExt{
				LastActive: time.Now().Add(-2 * time.Hour),
			},
			policy:   &SessionPolicy{IdleTimeout: time.Hour},
			expected: true,
		},
		{
			name: "session is active",
			session: &SessionExt{
				LastActive: time.Now().Add(-30 * time.Minute),
			},
			policy:   &SessionPolicy{IdleTimeout: time.Hour},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.session.IsIdle(tc.policy)
			if result != tc.expected {
				t.Errorf("IsIdle() = %v, expected %v", result, tc.expected)
			}
		})
	}
}

// =============================================================================
// OIDCProviderAdapter coverage - renamed to avoid duplicate
// =============================================================================

func TestOIDCProviderAdapter_Creation(t *testing.T) {
	// Create a mock OIDC provider
	adapter := NewOIDCProviderAdapter(nil)
	if adapter == nil {
		t.Error("NewOIDCProviderAdapter should not return nil")
	}

	// GetProvider should return nil when initialized with nil
	if adapter.GetProvider() != nil {
		t.Error("GetProvider should return nil when initialized with nil")
	}
}

// =============================================================================
// User import with roles and groups
// =============================================================================

func TestBatchProcessor_ProcessUsers_WithRolesAndGroups(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newUpdateCapableTenantMgr()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []UserImportItem{
		{
			Email:  "rolesgroups@test.com",
			Name:   "User With Roles",
			Roles:  []string{"admin", "viewer"},
			Groups: []string{"engineering", "qa"},
		},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Succeeded != 1 {
		t.Errorf("expected 1 success, got %d", saved.Progress.Succeeded)
	}

	// Verify user has roles and groups
	user, _ := tenantMgr.GetUserByEmail(ctx, tenantID, "rolesgroups@test.com")
	if len(user.Roles) != 2 {
		t.Errorf("expected 2 roles, got %d", len(user.Roles))
	}
	if len(user.Groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(user.Groups))
	}
}

// =============================================================================
// BatchProgress.CalculateRate edge case
// =============================================================================

func TestBatchProgress_CalculateRate_WithElapsed(t *testing.T) {
	progress := &BatchProgress{
		Total:     100,
		Processed: 50,
	}

	// Calculate with 5 seconds elapsed
	progress.CalculateRate(5 * time.Second)

	expectedRate := 10.0 // 50 processed / 5 seconds
	if progress.ItemsPerSecond != expectedRate {
		t.Errorf("expected rate %.2f, got %.2f", expectedRate, progress.ItemsPerSecond)
	}
}

// =============================================================================
// matchesExportFilter coverage
// =============================================================================

func TestMemoryDataExportStorage_ListExportRequests_FilterByStatus(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	// Create export requests with different statuses
	_, err := storage.CreateExportRequest(ctx, CreateDataExportOptions{
		TenantID:    "tenant-1",
		UserID:      "user-1",
		RequestedBy: "user-1",
		Format:      DataExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("CreateExportRequest failed: %v", err)
	}

	// List with status filter
	requests, err := storage.ListExportRequests(ctx, ListExportRequestsOptions{
		TenantID: "tenant-1",
		Status:   DataExportStatusPending,
	})
	if err != nil {
		t.Fatalf("ListExportRequests failed: %v", err)
	}

	if len(requests) != 1 {
		t.Errorf("expected 1 request with pending status, got %d", len(requests))
	}
}

func TestMemoryDataExportStorage_ListExportRequests_FilterByUser(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	// Create export requests for different users
	_, err := storage.CreateExportRequest(ctx, CreateDataExportOptions{
		TenantID:    "tenant-1",
		UserID:      "user-1",
		RequestedBy: "user-1",
		Format:      DataExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("CreateExportRequest failed: %v", err)
	}

	_, err = storage.CreateExportRequest(ctx, CreateDataExportOptions{
		TenantID:    "tenant-1",
		UserID:      "user-2",
		RequestedBy: "user-2",
		Format:      DataExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("CreateExportRequest failed: %v", err)
	}

	// List with user filter
	requests, err := storage.ListExportRequests(ctx, ListExportRequestsOptions{
		TenantID: "tenant-1",
		UserID:   "user-1",
	})
	if err != nil {
		t.Fatalf("ListExportRequests failed: %v", err)
	}

	if len(requests) != 1 {
		t.Errorf("expected 1 request for user-1, got %d", len(requests))
	}
}

// =============================================================================
// matchesDeletionFilter coverage
// =============================================================================

func TestMemoryDeletionStorage_ListDeletionRequests_FilterByStatus(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	// Create deletion requests
	_, err := storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		TenantID:    "tenant-1",
		UserID:      "user-1",
		RequestedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest failed: %v", err)
	}

	// List with status filter
	requests, err := storage.ListDeletionRequests(ctx, ListDeletionRequestsOptions{
		TenantID: "tenant-1",
		Status:   DeletionStatusPending,
	})
	if err != nil {
		t.Fatalf("ListDeletionRequests failed: %v", err)
	}

	if len(requests) != 1 {
		t.Errorf("expected 1 request with pending status, got %d", len(requests))
	}
}

func TestMemoryDeletionStorage_ListDeletionRequests_FilterByUser(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	// Create deletion requests for different users
	_, err := storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		TenantID:    "tenant-1",
		UserID:      "user-1",
		RequestedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest failed: %v", err)
	}

	_, err = storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		TenantID:    "tenant-1",
		UserID:      "user-2",
		RequestedBy: "user-2",
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest failed: %v", err)
	}

	// List with user filter
	requests, err := storage.ListDeletionRequests(ctx, ListDeletionRequestsOptions{
		TenantID: "tenant-1",
		UserID:   "user-1",
	})
	if err != nil {
		t.Fatalf("ListDeletionRequests failed: %v", err)
	}

	if len(requests) != 1 {
		t.Errorf("expected 1 request for user-1, got %d", len(requests))
	}
}
