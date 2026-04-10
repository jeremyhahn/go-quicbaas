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

// Package auth provides persistent RBAC tests
package auth

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TestNewPersistentRoleManager tests the constructor.
func TestNewPersistentRoleManager(t *testing.T) {
	t.Run("valid initialization", func(t *testing.T) {
		storage := newAuthMockStorage()
		log := createTestLogger(t)

		prm, err := NewPersistentRoleManager(storage, log)
		assert.NoError(t, err)
		assert.NotNil(t, prm)
		assert.NotNil(t, prm.GetRole(RoleNameAdmin))
		assert.NotNil(t, prm.GetRole(RoleNameReadWrite))
		assert.NotNil(t, prm.GetRole(RoleNameReadOnly))
		assert.Equal(t, 3, prm.Count())
	})

	t.Run("nil storage", func(t *testing.T) {
		log := createTestLogger(t)

		prm, err := NewPersistentRoleManager(nil, log)
		assert.Error(t, err)
		assert.Nil(t, prm)
	})

	t.Run("nil logger", func(t *testing.T) {
		storage := newAuthMockStorage()

		prm, err := NewPersistentRoleManager(storage, nil)
		assert.Error(t, err)
		assert.Nil(t, prm)
	})

	t.Run("storage error on load", func(t *testing.T) {
		storage := newAuthMockStorage()
		log := createTestLogger(t)

		storage.Close()

		prm, err := NewPersistentRoleManager(storage, log)
		assert.Error(t, err)
		assert.Nil(t, prm)
	})
}

// TestPersistentRoleCount tests role counting.
func TestPersistentRoleCount(t *testing.T) {
	storage := newAuthMockStorage()
	log := createTestLogger(t)

	prm, err := NewPersistentRoleManager(storage, log)
	require.NoError(t, err)

	assert.Equal(t, 3, prm.Count())

	for i := 0; i < 5; i++ {
		role := &Role{
			Name:        fmt.Sprintf("custom%d", i),
			Description: fmt.Sprintf("Custom role %d", i),
			Permissions: []*Permission{},
		}
		err := prm.AddRole(role)
		require.NoError(t, err)
	}

	assert.Equal(t, 8, prm.Count())
}

// TestPersistentRBACCoverage tests all PersistentRoleManager methods.
func TestPersistentRBACCoverage(t *testing.T) {
	storage := newAuthMockStorage()
	log := createTestLogger(t)

	prm, err := NewPersistentRoleManager(storage, log)
	require.NoError(t, err)
	require.NotNil(t, prm)

	// Test built-in roles
	admin := prm.GetRole(RoleNameAdmin)
	require.NotNil(t, admin)
	assert.Equal(t, RoleNameAdmin, admin.Name)

	readwrite := prm.GetRole(RoleNameReadWrite)
	require.NotNil(t, readwrite)

	readonly := prm.GetRole(RoleNameReadOnly)
	require.NotNil(t, readonly)

	// Test AddRole
	customRole := &Role{
		Name:        "developer",
		Description: "Developer role",
		Permissions: []*Permission{
			{Resource: ResourceKeys, Actions: []string{ActionRead, ActionWrite}},
		},
	}
	err = prm.AddRole(customRole)
	assert.NoError(t, err)

	// Test duplicate role
	err = prm.AddRole(customRole)
	assert.Error(t, err)

	// Test GetRole
	retrieved := prm.GetRole("developer")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "developer", retrieved.Name)

	// Test ListRoles
	roles := prm.ListRoles()
	assert.Len(t, roles, 4)

	// Test UpdateRole
	customRole.Description = "Updated description"
	err = prm.UpdateRole(customRole)
	assert.NoError(t, err)

	updated := prm.GetRole("developer")
	assert.Equal(t, "Updated description", updated.Description)

	// Test updating built-in role (should fail)
	adminUpdate := &Role{
		Name:        RoleNameAdmin,
		Description: "Modified",
		Permissions: []*Permission{},
	}
	err = prm.UpdateRole(adminUpdate)
	assert.Error(t, err)

	// Test DeleteRole
	err = prm.DeleteRole("developer")
	assert.NoError(t, err)
	assert.Nil(t, prm.GetRole("developer"))

	// Test deleting built-in role (should fail)
	err = prm.DeleteRole(RoleNameAdmin)
	assert.Error(t, err)
	assert.NotNil(t, prm.GetRole(RoleNameAdmin))

	// Test HasPermission
	assert.True(t, prm.HasPermission([]string{RoleNameAdmin}, ResourceKeys, ActionRead))
	assert.True(t, prm.HasPermission([]string{RoleNameReadWrite}, ResourceKeys, ActionWrite))
	assert.False(t, prm.HasPermission([]string{RoleNameReadOnly}, ResourceKeys, ActionWrite))
	assert.False(t, prm.HasPermission([]string{}, ResourceKeys, ActionRead))

	// Test AllowsAnonymous
	assert.True(t, prm.AllowsAnonymous(ResourceHealth, ActionRead))
	assert.True(t, prm.AllowsAnonymous(ResourceMetrics, ActionRead))
	assert.False(t, prm.AllowsAnonymous(ResourceKeys, ActionRead))

	// Test ValidateRoles
	assert.True(t, prm.ValidateRoles([]string{RoleNameAdmin, RoleNameReadWrite}))
	assert.False(t, prm.ValidateRoles([]string{"nonexistent"}))
	assert.True(t, prm.ValidateRoles([]string{}))

	// Test matchesResource
	assert.True(t, prm.matchesResource(ResourceAll, "anything"))
	assert.True(t, prm.matchesResource(ResourceKeys, ResourceKeys))
	assert.False(t, prm.matchesResource(ResourceKeys, ResourceIndexes))

	// Test hasAction
	assert.True(t, prm.hasAction([]string{ActionAll}, "anything"))
	assert.True(t, prm.hasAction([]string{ActionAdmin}, ActionRead))
	assert.True(t, prm.hasAction([]string{ActionRead, ActionWrite}, ActionRead))
	assert.False(t, prm.hasAction([]string{ActionRead}, ActionWrite))
}

// TestPersistentRolePersistence tests persistence across manager instances.
func TestPersistentRolePersistence(t *testing.T) {
	storage := newAuthMockStorage()
	log := createTestLogger(t)

	// Create first manager
	prm1, err := NewPersistentRoleManager(storage, log)
	require.NoError(t, err)

	customRole := &Role{
		Name:        "persistent",
		Description: "Persistent role",
		Permissions: []*Permission{
			{Resource: ResourceKeys, Actions: []string{ActionRead}},
		},
	}
	err = prm1.AddRole(customRole)
	require.NoError(t, err)

	// Create second manager - should load from storage
	prm2, err := NewPersistentRoleManager(storage, log)
	require.NoError(t, err)

	assert.Equal(t, prm1.Count(), prm2.Count())
	loaded := prm2.GetRole("persistent")
	require.NotNil(t, loaded)
	assert.Equal(t, "persistent", loaded.Name)
}

// TestPersistentRoleStorageErrors tests error handling.
func TestPersistentRoleStorageErrors(t *testing.T) {
	storage := newAuthMockStorage()
	log := createTestLogger(t)

	prm, err := NewPersistentRoleManager(storage, log)
	require.NoError(t, err)

	// Test AddRole with storage error
	storage.InjectError("Set", &dberrors.MessageError{Message: "storage error"})
	role := &Role{Name: "testerror", Description: "Test", Permissions: []*Permission{}}
	err = prm.AddRole(role)
	assert.Error(t, err)
	assert.Nil(t, prm.GetRole("testerror"))

	// Test DeleteRole with storage error
	role2 := &Role{Name: "todelete", Description: "Test", Permissions: []*Permission{}}
	err = prm.AddRole(role2)
	require.NoError(t, err)

	storage.InjectError("Delete", &dberrors.MessageError{Message: "delete error"})
	err = prm.DeleteRole("todelete")
	assert.Error(t, err)
	assert.NotNil(t, prm.GetRole("todelete"))

	// Test UpdateRole with storage error
	role3 := &Role{Name: "toupdate", Description: "Original", Permissions: []*Permission{}}
	err = prm.AddRole(role3)
	require.NoError(t, err)

	storage.InjectError("Set", &dberrors.MessageError{Message: "update error"})
	updatedRole := &Role{Name: "toupdate", Description: "Updated", Permissions: []*Permission{}}
	err = prm.UpdateRole(updatedRole)
	assert.Error(t, err)
	// Cache should not be updated when storage fails
	assert.Equal(t, "Original", prm.GetRole("toupdate").Description)
}

// TestPersistentRoleLoadCorruptedData tests handling of corrupted data.
func TestPersistentRoleLoadCorruptedData(t *testing.T) {
	storage := newAuthMockStorage()
	log := createTestLogger(t)

	// Insert corrupted role data
	kb := NewKeyBuilder()
	corruptedKey := kb.RoleKey("corrupted")
	err := storage.Set(corruptedKey, []byte("invalid json"))
	require.NoError(t, err)

	// Create manager - should skip corrupted role
	prm, err := NewPersistentRoleManager(storage, log)
	require.NoError(t, err)
	require.NotNil(t, prm)

	assert.Equal(t, 3, prm.Count())
	assert.Nil(t, prm.GetRole("corrupted"))
}

// TestPersistentRoleMetadataSkip tests that metadata keys are skipped.
func TestPersistentRoleMetadataSkip(t *testing.T) {
	storage := newAuthMockStorage()
	log := createTestLogger(t)

	// Insert metadata key
	metadataKey := []byte(KeyPrefixRoleMetadata + "test:metadata")
	err := storage.Set(metadataKey, []byte("metadata value"))
	require.NoError(t, err)

	// Create manager - should skip metadata key
	prm, err := NewPersistentRoleManager(storage, log)
	require.NoError(t, err)
	require.NotNil(t, prm)

	assert.Equal(t, 3, prm.Count())
}

// TestPersistentRoleConcurrency tests concurrent access.
func TestPersistentRoleConcurrency(t *testing.T) {
	storage := newAuthMockStorage()
	log := createTestLogger(t)

	prm, err := NewPersistentRoleManager(storage, log)
	require.NoError(t, err)

	// Add initial roles
	for i := 0; i < 10; i++ {
		role := &Role{
			Name:        fmt.Sprintf("role%d", i),
			Description: fmt.Sprintf("Role %d", i),
			Permissions: []*Permission{},
		}
		err := prm.AddRole(role)
		require.NoError(t, err)
	}

	// Concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			roleName := fmt.Sprintf("role%d", id%10)
			for j := 0; j < 100; j++ {
				role := prm.GetRole(roleName)
				assert.NotNil(t, role)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestPrefixUpperBoundUtility tests the prefix upper bound utility.
func TestPrefixUpperBoundUtility(t *testing.T) {
	tests := []struct {
		name     string
		prefix   []byte
		expected []byte
	}{
		{
			name:     "simple prefix",
			prefix:   []byte("test"),
			expected: []byte("tesu"),
		},
		{
			name:     "empty prefix",
			prefix:   []byte{},
			expected: nil,
		},
		{
			name:     "all 0xFF bytes",
			prefix:   []byte{0xFF, 0xFF},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prefixUpperBound(tt.prefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}
