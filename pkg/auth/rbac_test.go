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

// Package auth provides RBAC tests
package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRoleManager(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	assert.NotNil(t, rm)

	// Verify built-in roles exist
	assert.NotNil(t, rm.GetRole(RoleNameAdmin))
	assert.NotNil(t, rm.GetRole(RoleNameReadWrite))
	assert.NotNil(t, rm.GetRole(RoleNameReadOnly))
}

func TestHasPermission(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	tests := []struct {
		name          string
		roles         []string
		resource      string
		action        string
		hasPermission bool
	}{
		// Admin role tests
		{
			name:          "admin can read keys",
			roles:         []string{RoleNameAdmin},
			resource:      ResourceKeys,
			action:        ActionRead,
			hasPermission: true,
		},
		{
			name:          "admin can write keys",
			roles:         []string{RoleNameAdmin},
			resource:      ResourceKeys,
			action:        ActionWrite,
			hasPermission: true,
		},
		{
			name:          "admin can delete keys",
			roles:         []string{RoleNameAdmin},
			resource:      ResourceKeys,
			action:        ActionDelete,
			hasPermission: true,
		},
		{
			name:          "admin can manage users",
			roles:         []string{RoleNameAdmin},
			resource:      ResourceUsers,
			action:        ActionAdmin,
			hasPermission: true,
		},
		{
			name:          "admin can manage cluster",
			roles:         []string{RoleNameAdmin},
			resource:      ResourceCluster,
			action:        ActionAdmin,
			hasPermission: true,
		},

		// ReadWrite role tests
		{
			name:          "readwrite can read keys",
			roles:         []string{RoleNameReadWrite},
			resource:      ResourceKeys,
			action:        ActionRead,
			hasPermission: true,
		},
		{
			name:          "readwrite can write keys",
			roles:         []string{RoleNameReadWrite},
			resource:      ResourceKeys,
			action:        ActionWrite,
			hasPermission: true,
		},
		{
			name:          "readwrite can delete keys",
			roles:         []string{RoleNameReadWrite},
			resource:      ResourceKeys,
			action:        ActionDelete,
			hasPermission: true,
		},
		{
			name:          "readwrite cannot manage users",
			roles:         []string{RoleNameReadWrite},
			resource:      ResourceUsers,
			action:        ActionAdmin,
			hasPermission: false,
		},
		{
			name:          "readwrite can read metrics",
			roles:         []string{RoleNameReadWrite},
			resource:      ResourceMetrics,
			action:        ActionRead,
			hasPermission: true,
		},
		{
			name:          "readwrite can write transactions",
			roles:         []string{RoleNameReadWrite},
			resource:      ResourceTransactions,
			action:        ActionWrite,
			hasPermission: true,
		},

		// ReadOnly role tests
		{
			name:          "readonly can read keys",
			roles:         []string{RoleNameReadOnly},
			resource:      ResourceKeys,
			action:        ActionRead,
			hasPermission: true,
		},
		{
			name:          "readonly cannot write keys",
			roles:         []string{RoleNameReadOnly},
			resource:      ResourceKeys,
			action:        ActionWrite,
			hasPermission: false,
		},
		{
			name:          "readonly cannot delete keys",
			roles:         []string{RoleNameReadOnly},
			resource:      ResourceKeys,
			action:        ActionDelete,
			hasPermission: false,
		},
		{
			name:          "readonly can read metrics",
			roles:         []string{RoleNameReadOnly},
			resource:      ResourceMetrics,
			action:        ActionRead,
			hasPermission: true,
		},
		{
			name:          "readonly can read health",
			roles:         []string{RoleNameReadOnly},
			resource:      ResourceHealth,
			action:        ActionRead,
			hasPermission: true,
		},

		// Multiple roles tests
		{
			name:          "multiple roles combine permissions",
			roles:         []string{RoleNameReadOnly, RoleNameReadWrite},
			resource:      ResourceKeys,
			action:        ActionWrite,
			hasPermission: true,
		},

		// Non-existent role tests
		{
			name:          "non-existent role has no permissions",
			roles:         []string{"nonexistent"},
			resource:      ResourceKeys,
			action:        ActionRead,
			hasPermission: false,
		},

		// Empty roles tests
		{
			name:          "empty roles has no permissions",
			roles:         []string{},
			resource:      ResourceKeys,
			action:        ActionRead,
			hasPermission: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rm.HasPermission(tt.roles, tt.resource, tt.action)
			assert.Equal(t, tt.hasPermission, result)
		})
	}
}

func TestAddRole(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	customRole := &Role{
		Name:        "custom",
		Description: "Custom role for testing",
		Permissions: []*Permission{
			{
				Resource: ResourceKeys,
				Actions:  []string{ActionRead},
			},
		},
	}

	err := rm.AddRole(customRole)
	assert.NoError(t, err)

	// Verify role was added
	retrieved := rm.GetRole("custom")
	assert.NotNil(t, retrieved)
	assert.Equal(t, customRole.Name, retrieved.GetName())

	// Verify role works
	assert.True(t, rm.HasPermission([]string{"custom"}, ResourceKeys, ActionRead))
	assert.False(t, rm.HasPermission([]string{"custom"}, ResourceKeys, ActionWrite))
}

func TestUpdateRole(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Add initial role
	customRole := &Role{
		Name:        "updatable",
		Description: "Role to be updated",
		Permissions: []*Permission{
			{
				Resource: ResourceKeys,
				Actions:  []string{ActionRead},
			},
		},
	}

	err := rm.AddRole(customRole)
	assert.NoError(t, err)

	// Update role
	updatedRole := &Role{
		Name:        "updatable",
		Description: "Updated role",
		Permissions: []*Permission{
			{
				Resource: ResourceKeys,
				Actions:  []string{ActionRead, ActionWrite},
			},
		},
	}

	err = rm.UpdateRole(updatedRole)
	assert.NoError(t, err)

	// Verify update
	retrieved := rm.GetRole("updatable")
	assert.NotNil(t, retrieved)
	assert.True(t, rm.HasPermission([]string{"updatable"}, ResourceKeys, ActionWrite))

	// Try to update built-in role (should be ignored)
	err = rm.UpdateRole(&Role{
		Name:        RoleNameAdmin,
		Description: "Modified admin",
		Permissions: []*Permission{},
	})
	assert.NoError(t, err) // No error, but change is ignored

	// Verify built-in role unchanged
	admin := rm.GetRole(RoleNameAdmin)
	assert.NotNil(t, admin)
	assert.NotEmpty(t, admin.GetPermissions())
}

func TestDeleteRole(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Add custom role
	customRole := &Role{
		Name:        "deletable",
		Description: "Role to be deleted",
		Permissions: []*Permission{},
	}

	err := rm.AddRole(customRole)
	assert.NoError(t, err)

	// Delete custom role
	err = rm.DeleteRole("deletable")
	assert.NoError(t, err)

	// Verify deletion
	assert.Nil(t, rm.GetRole("deletable"))

	// Try to delete built-in role (should be ignored)
	err = rm.DeleteRole(RoleNameAdmin)
	assert.NoError(t, err) // No error, but deletion is ignored

	// Verify built-in role still exists
	assert.NotNil(t, rm.GetRole(RoleNameAdmin))
}

func TestListRoles(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	roles := rm.ListRoles()
	// Built-in roles: admin, readwrite, readonly, vector-reader, vector-writer, vector-admin,
	// objstore-admin, objstore-writer, objstore-reader, objstore-operator,
	// parameters-reader, parameters-writer, parameters-admin
	assert.Len(t, roles, 13)

	// Add custom role
	rm.AddRole(&Role{
		Name:        "custom",
		Description: "Custom role",
		Permissions: []*Permission{},
	})

	roles = rm.ListRoles()
	assert.Len(t, roles, 14)
}

func TestValidateRoles(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	tests := []struct {
		name    string
		roles   []string
		isValid bool
	}{
		{
			name:    "valid built-in roles",
			roles:   []string{RoleNameAdmin, RoleNameReadWrite},
			isValid: true,
		},
		{
			name:    "single valid role",
			roles:   []string{RoleNameReadOnly},
			isValid: true,
		},
		{
			name:    "invalid role",
			roles:   []string{"nonexistent"},
			isValid: false,
		},
		{
			name:    "mix of valid and invalid",
			roles:   []string{RoleNameAdmin, "nonexistent"},
			isValid: false,
		},
		{
			name:    "empty roles",
			roles:   []string{},
			isValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rm.ValidateRoles(tt.roles)
			assert.Equal(t, tt.isValid, result)
		})
	}
}

func TestAllowsAnonymous(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	tests := []struct {
		name     string
		resource string
		action   string
		allowed  bool
	}{
		{
			name:     "health read allowed",
			resource: ResourceHealth,
			action:   ActionRead,
			allowed:  true,
		},
		{
			name:     "metrics read allowed",
			resource: ResourceMetrics,
			action:   ActionRead,
			allowed:  true,
		},
		{
			name:     "keys read not allowed",
			resource: ResourceKeys,
			action:   ActionRead,
			allowed:  false,
		},
		{
			name:     "health write not allowed",
			resource: ResourceHealth,
			action:   ActionWrite,
			allowed:  false,
		},
		{
			name:     "cluster read not allowed",
			resource: ResourceCluster,
			action:   ActionRead,
			allowed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rm.AllowsAnonymous(tt.resource, tt.action)
			assert.Equal(t, tt.allowed, result)
		})
	}
}

func TestCustomPermissions(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Create a custom role with specific permissions
	developerRole := &Role{
		Name:        "developer",
		Description: "Developer with limited cluster access",
		Permissions: []*Permission{
			{
				Resource: ResourceKeys,
				Actions:  []string{ActionRead, ActionWrite},
			},
			{
				Resource: ResourceIndexes,
				Actions:  []string{ActionRead},
			},
			{
				Resource: ResourceMetrics,
				Actions:  []string{ActionRead},
			},
		},
	}

	err := rm.AddRole(developerRole)
	require.NoError(t, err)

	// Test permissions
	tests := []struct {
		resource      string
		action        string
		hasPermission bool
	}{
		{ResourceKeys, ActionRead, true},
		{ResourceKeys, ActionWrite, true},
		{ResourceKeys, ActionDelete, false},
		{ResourceIndexes, ActionRead, true},
		{ResourceIndexes, ActionWrite, false},
		{ResourceMetrics, ActionRead, true},
		{ResourceCluster, ActionRead, false},
		{ResourceUsers, ActionAdmin, false},
	}

	for _, tt := range tests {
		t.Run(tt.resource+"_"+tt.action, func(t *testing.T) {
			result := rm.HasPermission([]string{"developer"}, tt.resource, tt.action)
			assert.Equal(t, tt.hasPermission, result)
		})
	}
}

func TestWildcardPermissions(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Admin role has wildcard permissions
	assert.True(t, rm.HasPermission([]string{RoleNameAdmin}, "any-resource", "any-action"))
	assert.True(t, rm.HasPermission([]string{RoleNameAdmin}, ResourceKeys, ActionDelete))
	assert.True(t, rm.HasPermission([]string{RoleNameAdmin}, ResourceUsers, ActionAdmin))
}

// TestVectorBuiltInRoles tests that vector-specific built-in roles exist.
func TestVectorBuiltInRoles(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Verify vector roles exist
	assert.NotNil(t, rm.GetRole(RoleNameVectorReader))
	assert.NotNil(t, rm.GetRole(RoleNameVectorWriter))
	assert.NotNil(t, rm.GetRole(RoleNameVectorAdmin))

	// Verify role descriptions
	vectorReader := rm.GetRole(RoleNameVectorReader)
	assert.Contains(t, vectorReader.GetDescription(), "Read-only")

	vectorWriter := rm.GetRole(RoleNameVectorWriter)
	assert.Contains(t, vectorWriter.GetDescription(), "Read and write")

	vectorAdmin := rm.GetRole(RoleNameVectorAdmin)
	assert.Contains(t, vectorAdmin.GetDescription(), "Full administrative")
}

// TestVectorReaderPermissions tests vector-reader role permissions.
func TestVectorReaderPermissions(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	tests := []struct {
		name          string
		action        string
		hasPermission bool
	}{
		// Allowed actions
		{"can read vectors", ActionVectorRead, true},
		{"can search vectors", ActionVectorSearch, true},
		{"can view stats", ActionVectorStats, true},

		// Denied actions
		{"cannot write vectors", ActionVectorWrite, false},
		{"cannot delete vectors", ActionVectorDelete, false},
		{"cannot manage namespaces", ActionVectorNamespace, false},
		{"cannot admin vectors", ActionVectorAdmin, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rm.HasPermission([]string{RoleNameVectorReader}, ResourceVector, tt.action)
			assert.Equal(t, tt.hasPermission, result)
		})
	}
}

// TestVectorWriterPermissions tests vector-writer role permissions.
func TestVectorWriterPermissions(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	tests := []struct {
		name          string
		action        string
		hasPermission bool
	}{
		// Allowed actions
		{"can read vectors", ActionVectorRead, true},
		{"can write vectors", ActionVectorWrite, true},
		{"can search vectors", ActionVectorSearch, true},
		{"can view stats", ActionVectorStats, true},

		// Denied actions
		{"cannot delete vectors", ActionVectorDelete, false},
		{"cannot manage namespaces", ActionVectorNamespace, false},
		{"cannot admin vectors", ActionVectorAdmin, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rm.HasPermission([]string{RoleNameVectorWriter}, ResourceVector, tt.action)
			assert.Equal(t, tt.hasPermission, result)
		})
	}
}

// TestVectorAdminPermissions tests vector-admin role has all permissions.
func TestVectorAdminPermissions(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Vector-admin should have all vector actions
	allVectorActions := []string{
		ActionVectorRead,
		ActionVectorWrite,
		ActionVectorDelete,
		ActionVectorAdmin,
		ActionVectorNamespace,
		ActionVectorSearch,
		ActionVectorStats,
	}

	for _, action := range allVectorActions {
		t.Run("can_"+action, func(t *testing.T) {
			result := rm.HasPermission([]string{RoleNameVectorAdmin}, ResourceVector, action)
			assert.True(t, result, "vector-admin should have %s permission", action)
		})
	}
}

// TestVectorAdminActionIncludesAllVectorActions tests that having vector:admin
// grants access to all other vector actions via hasAction logic.
func TestVectorAdminActionIncludesAllVectorActions(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Create a custom role with only vector:admin action
	adminOnlyRole := &Role{
		Name:        "vector-admin-only",
		Description: "Role with only vector:admin action",
		Permissions: []*Permission{
			{
				Resource: ResourceVector,
				Actions:  []string{ActionVectorAdmin},
			},
		},
	}
	rm.AddRole(adminOnlyRole)

	// vector:admin should grant access to all vector operations
	vectorActions := []string{
		ActionVectorRead,
		ActionVectorWrite,
		ActionVectorDelete,
		ActionVectorNamespace,
		ActionVectorSearch,
		ActionVectorStats,
		ActionVectorAdmin,
	}

	for _, action := range vectorActions {
		t.Run("admin_includes_"+action, func(t *testing.T) {
			result := rm.HasPermission([]string{"vector-admin-only"}, ResourceVector, action)
			assert.True(t, result, "vector:admin should include %s", action)
		})
	}
}

// TestIsVectorAction tests the isVectorAction helper function.
func TestIsVectorAction(t *testing.T) {
	vectorActions := []string{
		ActionVectorRead,
		ActionVectorWrite,
		ActionVectorDelete,
		ActionVectorAdmin,
		ActionVectorNamespace,
		ActionVectorSearch,
		ActionVectorStats,
	}

	for _, action := range vectorActions {
		t.Run(action+"_is_vector_action", func(t *testing.T) {
			assert.True(t, isVectorAction(action))
		})
	}

	// Non-vector actions
	nonVectorActions := []string{
		ActionRead,
		ActionWrite,
		ActionDelete,
		ActionAdmin,
		"unknown",
		"",
	}

	for _, action := range nonVectorActions {
		name := action
		if name == "" {
			name = "empty"
		}
		t.Run(name+"_is_not_vector_action", func(t *testing.T) {
			assert.False(t, isVectorAction(action))
		})
	}
}

// TestIsBuiltInRole tests the isBuiltInRole helper function.
func TestIsBuiltInRole(t *testing.T) {
	builtInRoles := []string{
		RoleNameAdmin,
		RoleNameReadWrite,
		RoleNameReadOnly,
		RoleNameVectorReader,
		RoleNameVectorWriter,
		RoleNameVectorAdmin,
	}

	for _, role := range builtInRoles {
		t.Run(role+"_is_built_in", func(t *testing.T) {
			assert.True(t, isBuiltInRole(role))
		})
	}

	// Non-built-in roles
	customRoles := []string{
		"custom",
		"my-role",
		"",
		"unknown",
	}

	for _, role := range customRoles {
		name := role
		if name == "" {
			name = "empty"
		}
		t.Run(name+"_is_not_built_in", func(t *testing.T) {
			assert.False(t, isBuiltInRole(role))
		})
	}
}

// TestCannotDeleteVectorBuiltInRoles tests that vector built-in roles cannot be deleted.
func TestCannotDeleteVectorBuiltInRoles(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	vectorRoles := []string{
		RoleNameVectorReader,
		RoleNameVectorWriter,
		RoleNameVectorAdmin,
	}

	for _, role := range vectorRoles {
		t.Run("cannot_delete_"+role, func(t *testing.T) {
			// Attempt to delete
			err := rm.DeleteRole(role)
			assert.NoError(t, err) // No error returned, but deletion is silently ignored

			// Verify role still exists
			assert.NotNil(t, rm.GetRole(role))
		})
	}
}

// TestCannotUpdateVectorBuiltInRoles tests that vector built-in roles cannot be modified.
func TestCannotUpdateVectorBuiltInRoles(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Get original permissions count
	original := rm.GetRole(RoleNameVectorReader)
	originalPermCount := len(original.GetPermissions())

	// Attempt to update with different permissions
	modified := &Role{
		Name:        RoleNameVectorReader,
		Description: "Modified description",
		Permissions: []*Permission{
			{
				Resource: ResourceVector,
				Actions:  []string{ActionVectorAdmin}, // Try to escalate privileges
			},
		},
	}
	err := rm.UpdateRole(modified)
	assert.NoError(t, err) // No error returned, but update is silently ignored

	// Verify role is unchanged
	current := rm.GetRole(RoleNameVectorReader)
	assert.Equal(t, originalPermCount, len(current.GetPermissions()))
}

// TestHasVectorNamespacePermission tests the HasVectorNamespacePermission convenience method.
func TestHasVectorNamespacePermission(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Create a custom role with specific namespace access
	nsRole := &Role{
		Name:        "embeddings-team",
		Description: "Access to embeddings namespace only",
		Permissions: []*Permission{
			{
				Resource: "vector:namespace:embeddings",
				Actions:  []string{ActionVectorRead, ActionVectorWrite, ActionVectorSearch},
			},
		},
	}
	rm.AddRole(nsRole)

	tests := []struct {
		name          string
		roles         []string
		namespace     string
		action        string
		hasPermission bool
	}{
		// Custom namespace role tests
		{"embeddings team can read embeddings", []string{"embeddings-team"}, "embeddings", ActionVectorRead, true},
		{"embeddings team can write embeddings", []string{"embeddings-team"}, "embeddings", ActionVectorWrite, true},
		{"embeddings team can search embeddings", []string{"embeddings-team"}, "embeddings", ActionVectorSearch, true},
		{"embeddings team cannot delete embeddings", []string{"embeddings-team"}, "embeddings", ActionVectorDelete, false},
		{"embeddings team cannot access other ns", []string{"embeddings-team"}, "other-ns", ActionVectorRead, false},

		// Vector-admin gets all namespaces
		{"vector-admin gets all namespaces", []string{RoleNameVectorAdmin}, "any-namespace", ActionVectorRead, true},
		{"vector-admin gets embeddings", []string{RoleNameVectorAdmin}, "embeddings", ActionVectorWrite, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rm.HasVectorNamespacePermission(tt.roles, tt.namespace, tt.action)
			assert.Equal(t, tt.hasPermission, result)
		})
	}
}

// TestNamespaceWildcardAccess tests wildcard namespace access patterns.
func TestNamespaceWildcardAccess(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Create role with namespace wildcard
	wildcardRole := &Role{
		Name:        "vector-all-ns",
		Description: "Access to all vector namespaces",
		Permissions: []*Permission{
			{
				Resource: "vector:namespace:*",
				Actions:  []string{ActionVectorRead, ActionVectorSearch},
			},
		},
	}
	rm.AddRole(wildcardRole)

	// Create role with prefix wildcard
	prefixRole := &Role{
		Name:        "prod-team",
		Description: "Access to production namespaces",
		Permissions: []*Permission{
			{
				Resource: "vector:namespace:prod-*",
				Actions:  []string{ActionVectorRead, ActionVectorWrite, ActionVectorSearch},
			},
		},
	}
	rm.AddRole(prefixRole)

	tests := []struct {
		name          string
		roles         []string
		namespace     string
		action        string
		hasPermission bool
	}{
		// Wildcard role tests
		{"wildcard can read any ns", []string{"vector-all-ns"}, "embeddings", ActionVectorRead, true},
		{"wildcard can read any ns 2", []string{"vector-all-ns"}, "random-ns", ActionVectorRead, true},
		{"wildcard can search any ns", []string{"vector-all-ns"}, "test", ActionVectorSearch, true},
		{"wildcard cannot write", []string{"vector-all-ns"}, "embeddings", ActionVectorWrite, false},

		// Prefix wildcard role tests
		{"prefix can access prod-embeddings", []string{"prod-team"}, "prod-embeddings", ActionVectorRead, true},
		{"prefix can access prod-vectors", []string{"prod-team"}, "prod-vectors", ActionVectorWrite, true},
		{"prefix cannot access dev-embeddings", []string{"prod-team"}, "dev-embeddings", ActionVectorRead, false},
		{"prefix cannot access staging", []string{"prod-team"}, "staging-ns", ActionVectorRead, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rm.HasVectorNamespacePermission(tt.roles, tt.namespace, tt.action)
			assert.Equal(t, tt.hasPermission, result)
		})
	}
}

// TestMatchesResourceWithNamespace tests the matchesResource function with namespace resources.
func TestMatchesResourceWithNamespace(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	tests := []struct {
		name              string
		permResource      string
		requestedResource string
		expected          bool
	}{
		// Standard resource matching
		{"exact match keys", ResourceKeys, ResourceKeys, true},
		{"exact match vector", ResourceVector, ResourceVector, true},
		{"wildcard matches all", ResourceAll, ResourceKeys, true},
		{"no match different resources", ResourceKeys, "secrets", false},

		// Namespace resource matching
		{"namespace resource exact match", "vector:namespace:embeddings", "vector:namespace:embeddings", true},
		{"namespace resource no match", "vector:namespace:embeddings", "vector:namespace:other", false},
		{"vector resource matches namespace request", ResourceVector, "vector:namespace:embeddings", true},
		{"wildcard matches namespace request", ResourceAll, "vector:namespace:embeddings", true},
		{"namespace wildcard matches request", "vector:namespace:*", "vector:namespace:embeddings", true},
		{"prefix wildcard matches", "vector:namespace:prod-*", "vector:namespace:prod-embeddings", true},
		{"prefix wildcard no match", "vector:namespace:prod-*", "vector:namespace:dev-embeddings", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rm.matchesResource(tt.permResource, tt.requestedResource)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestMultipleRolesWithNamespaceAccess tests that users with multiple roles get combined access.
func TestMultipleRolesWithNamespaceAccess(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Create two namespace-specific roles
	role1 := &Role{
		Name:        "ns-role-1",
		Description: "Access to namespace-1",
		Permissions: []*Permission{
			{
				Resource: "vector:namespace:namespace-1",
				Actions:  []string{ActionVectorRead},
			},
		},
	}
	rm.AddRole(role1)

	role2 := &Role{
		Name:        "ns-role-2",
		Description: "Access to namespace-2",
		Permissions: []*Permission{
			{
				Resource: "vector:namespace:namespace-2",
				Actions:  []string{ActionVectorRead, ActionVectorWrite},
			},
		},
	}
	rm.AddRole(role2)

	// User with both roles
	userRoles := []string{"ns-role-1", "ns-role-2"}

	// Should have access to both namespaces
	assert.True(t, rm.HasVectorNamespacePermission(userRoles, "namespace-1", ActionVectorRead))
	assert.True(t, rm.HasVectorNamespacePermission(userRoles, "namespace-2", ActionVectorRead))
	assert.True(t, rm.HasVectorNamespacePermission(userRoles, "namespace-2", ActionVectorWrite))

	// Should not have write on namespace-1 (only role2 has write, but not for namespace-1)
	assert.False(t, rm.HasVectorNamespacePermission(userRoles, "namespace-1", ActionVectorWrite))

	// Should not have access to other namespaces
	assert.False(t, rm.HasVectorNamespacePermission(userRoles, "namespace-3", ActionVectorRead))
}
