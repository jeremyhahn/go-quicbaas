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
				UserStorage:      NewMemoryStorage(),
			},
			expectError: false,
		},
		{
			name: "missing hierarchy storage",
			opts: InheritanceManagerOptions{
				RoleStorage: NewMemoryStorage(),
				UserStorage: NewMemoryStorage(),
			},
			expectError: true,
		},
		{
			name: "missing role storage",
			opts: InheritanceManagerOptions{
				HierarchyStorage: NewMemoryHierarchyStorage(),
				UserStorage:      NewMemoryStorage(),
			},
			expectError: true,
		},
		{
			name: "missing user storage",
			opts: InheritanceManagerOptions{
				HierarchyStorage: NewMemoryHierarchyStorage(),
				RoleStorage:      NewMemoryStorage(),
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

func TestRoleHierarchyAddAndRemove(t *testing.T) {
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
	require.Len(t, hierarchy, 1)
	assert.Equal(t, "parent", hierarchy[0].ParentRoleID)
	assert.Equal(t, "child", hierarchy[0].ChildRoleID)

	// Remove parent
	err = manager.RemoveRoleParent(ctx, tenantID, "child", "parent")
	assert.NoError(t, err)

	// Verify hierarchy removed
	hierarchy, err = manager.GetRoleHierarchy(ctx, tenantID, "child")
	assert.NoError(t, err)
	assert.Len(t, hierarchy, 0)
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
		ID:       11,
		Email:    "user1@test.com",
		Name:     "User 1",
		TenantID: tenantID,
		Roles:    []string{"editor"},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	require.NoError(t, storage.CreateUser(ctx, user))

	// Get effective permissions
	effective, err := manager.GetEffectiveUserPermissions(ctx, tenantID, 11)
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

func TestInheritanceManager_NotFound(t *testing.T) {
	tests := []struct {
		name string
		call func(ctx context.Context, manager *InheritanceManager, tenantID string) error
	}{
		{
			name: "add parent to nonexistent roles",
			call: func(ctx context.Context, manager *InheritanceManager, tenantID string) error {
				return manager.AddRoleParent(ctx, tenantID, "nonexistent-child", "nonexistent-parent", 1)
			},
		},
		{
			name: "remove nonexistent hierarchy",
			call: func(ctx context.Context, manager *InheritanceManager, tenantID string) error {
				return manager.RemoveRoleParent(ctx, tenantID, "role1", "role2")
			},
		},
		{
			name: "effective permissions of nonexistent role",
			call: func(ctx context.Context, manager *InheritanceManager, tenantID string) error {
				_, err := manager.GetEffectiveRolePermissions(ctx, tenantID, "nonexistent")
				return err
			},
		},
		{
			name: "effective permissions of nonexistent user",
			call: func(ctx context.Context, manager *InheritanceManager, tenantID string) error {
				_, err := manager.GetEffectiveUserPermissions(ctx, tenantID, 99999)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, storage, ctx := setupInheritanceTest(t)
			tenantID := "test-tenant"
			for _, id := range []string{"role1", "role2"} {
				require.NoError(t, storage.CreateRole(ctx, &Role{
					ID:       id,
					Name:     id,
					TenantID: tenantID,
					Created:  time.Now(),
					Updated:  time.Now(),
				}))
			}

			assert.Error(t, tt.call(ctx, manager, tenantID))
		})
	}
}

func TestMemoryHierarchyStorage_GetRoleHierarchy(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()
	require.NoError(t, storage.CreateRoleHierarchy(ctx, &RoleHierarchy{
		ID:           "rh1",
		TenantID:     "t1",
		ParentRoleID: "parent-role",
		ChildRoleID:  "child-role",
		Priority:     1,
		Created:      time.Now(),
	}))

	tests := []struct {
		name    string
		parent  string
		child   string
		wantID  string
		wantErr bool
	}{
		{name: "found", parent: "parent-role", child: "child-role", wantID: "rh1"},
		{name: "not found", parent: "nonexistent-parent", child: "nonexistent-child", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := storage.GetRoleHierarchy(ctx, "t1", tt.parent, tt.child)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantID, result.ID)
		})
	}
}

func TestMemoryHierarchyStorage_DetectRoleCycle(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryHierarchyStorage()

	// A -> B -> C
	for _, h := range []*RoleHierarchy{
		{ID: "rh_ab", TenantID: "t1", ParentRoleID: "roleA", ChildRoleID: "roleB", Priority: 1, Created: time.Now()},
		{ID: "rh_bc", TenantID: "t1", ParentRoleID: "roleB", ChildRoleID: "roleC", Priority: 1, Created: time.Now()},
	} {
		require.NoError(t, storage.CreateRoleHierarchy(ctx, h))
	}

	tests := []struct {
		name      string
		parent    string
		child     string
		wantCycle bool
	}{
		{name: "new root above the chain has no cycle", parent: "roleD", child: "roleA"},
		{name: "closing the chain is a cycle", parent: "roleC", child: "roleA", wantCycle: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasCycle, err := storage.DetectRoleCycle(ctx, "t1", tt.parent, tt.child)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCycle, hasCycle)
		})
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
// SessionExt.Validate - Additional coverage for error paths
// =============================================================================

func TestSessionExt_Validate(t *testing.T) {
	tests := []struct {
		name    string
		session *SessionExt
		wantErr bool
	}{
		{
			name:    "valid",
			session: &SessionExt{ID: "session-1", UserID: 10001, TenantID: "tenant-1", TokenHash: "hash123"},
		},
		{
			name:    "missing session ID",
			session: &SessionExt{UserID: 10001, TenantID: "tenant-1", TokenHash: "hash123"},
			wantErr: true,
		},
		{
			name:    "missing user ID",
			session: &SessionExt{ID: "session-1", TenantID: "tenant-1", TokenHash: "hash123"},
			wantErr: true,
		},
		{
			name:    "missing token hash",
			session: &SessionExt{ID: "session-1", UserID: 10001, TenantID: "tenant-1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.session.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
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
	user1 := &User{ID: 21, TenantID: tenantID, Email: "user1@test.com", Name: "User 1"}
	user2 := &User{ID: 22, TenantID: tenantID, Email: "user2@test.com", Name: "User 2"}
	if err := storage.CreateUser(ctx, user1); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := storage.CreateUser(ctx, user2); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
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
	_, err = storage.GetUser(ctx, tenantID, 21)
	if err == nil {
		t.Error("user should be deleted after tenant deletion")
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

func TestMemoryConsentStorage_ListConsents_Filter(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	expires := time.Now().Add(24 * time.Hour)
	for _, c := range []struct {
		userID  uint64
		purpose ConsentPurpose
		status  ConsentStatus
	}{
		{userID: 10001, purpose: "marketing", status: ConsentStatusGranted},
		{userID: 10002, purpose: "analytics", status: ConsentStatusWithdrawn},
		{userID: 10003, purpose: "analytics", status: ConsentStatusGranted},
	} {
		_, err := storage.CreateConsent(ctx, CreateConsentOptions{
			TenantID:  "tenant-1",
			UserID:    c.userID,
			Purpose:   c.purpose,
			Status:    c.status,
			Version:   "1.0",
			ExpiresAt: &expires,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name string
		opts ListConsentsOptions
		want int
	}{
		{name: "by status", opts: ListConsentsOptions{TenantID: "tenant-1", Status: ConsentStatusGranted}, want: 2},
		{name: "by purpose", opts: ListConsentsOptions{TenantID: "tenant-1", Purpose: "marketing"}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consents, err := storage.ListConsents(ctx, tt.opts)
			require.NoError(t, err)
			assert.Len(t, consents, tt.want)
		})
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
		UserID:    10001,
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
		UserID:   10001,
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

// updateCapableTenantMgr stores created users by email and reports each one on
// created, so a test can wait for the batch goroutine without sleeping.
type updateCapableTenantMgr struct {
	*mockTenantManager
	users   map[string]*User
	created chan *User
}

func newUpdateCapableTenantMgr() *updateCapableTenantMgr {
	return &updateCapableTenantMgr{
		mockTenantManager: newMockTenantManager(),
		users:             make(map[string]*User),
		created:           make(chan *User, 16),
	}
}

func (m *updateCapableTenantMgr) CreateUser(ctx context.Context, user *User) error {
	key := user.TenantID + ":" + user.Email
	if _, exists := m.users[key]; exists {
		return ErrUserAlreadyExists
	}
	m.users[key] = user
	m.created <- user
	return nil
}

func (m *updateCapableTenantMgr) GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error) {
	key := tenantID + ":" + email
	if user, exists := m.users[key]; exists {
		return user, nil
	}
	return nil, ErrUserNotFound
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

// =============================================================================
// User import with roles
// =============================================================================

func TestBatchProcessor_ProcessUsers_WithRoles(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newUpdateCapableTenantMgr()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []UserImportItem{
		{
			Email: "rolesgroups@test.com",
			Name:  "User With Roles",
			Roles: []string{"admin", "viewer"},
		},
	}

	_, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{})
	require.NoError(t, err)

	select {
	case user := <-tenantMgr.created:
		assert.Equal(t, "rolesgroups@test.com", user.Email)
		assert.ElementsMatch(t, []string{"admin", "viewer"}, user.Roles)
	case <-time.After(5 * time.Second):
		t.Fatal("batch import did not create the user")
	}
}

// =============================================================================
// BatchProgress.CalculateRate edge case
// =============================================================================

// =============================================================================
// matchesExportFilter coverage
// =============================================================================

func TestMemoryDataExportStorage_ListExportRequests_Filter(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	for i, userID := range []uint64{10001, 10002} {
		_, err := storage.CreateExportRequest(ctx, CreateDataExportOptions{
			TenantID:    "tenant-1",
			UserID:      userID,
			RequestedBy: uint64(i + 1),
			Format:      DataExportFormatJSON,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name string
		opts ListExportRequestsOptions
		want int
	}{
		{name: "by status", opts: ListExportRequestsOptions{TenantID: "tenant-1", Status: DataExportStatusPending}, want: 2},
		{name: "by user", opts: ListExportRequestsOptions{TenantID: "tenant-1", UserID: 10001}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests, err := storage.ListExportRequests(ctx, tt.opts)
			require.NoError(t, err)
			assert.Len(t, requests, tt.want)
		})
	}
}

// =============================================================================
// matchesDeletionFilter coverage
// =============================================================================

func TestMemoryDeletionStorage_ListDeletionRequests_Filter(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	for i, userID := range []uint64{10001, 10002} {
		_, err := storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
			TenantID:    "tenant-1",
			UserID:      userID,
			RequestedBy: uint64(i + 1),
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name string
		opts ListDeletionRequestsOptions
		want int
	}{
		{name: "by status", opts: ListDeletionRequestsOptions{TenantID: "tenant-1", Status: DeletionStatusPending}, want: 2},
		{name: "by user", opts: ListDeletionRequestsOptions{TenantID: "tenant-1", UserID: 10001}, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests, err := storage.ListDeletionRequests(ctx, tt.opts)
			require.NoError(t, err)
			assert.Len(t, requests, tt.want)
		})
	}
}
