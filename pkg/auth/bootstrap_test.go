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

// Package auth provides bootstrap tests
package auth

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// FailableMockStorage wraps MockStorage to add failure injection for testing error paths.
type FailableMockStorage struct {
	*MockStorage
	failGet    bool
	failSet    bool
	failDelete bool
	mu         sync.RWMutex
}

// NewFailableMockStorage creates a new failable mock storage.
func NewFailableMockStorage() *FailableMockStorage {
	return &FailableMockStorage{
		MockStorage: NewMockStorage(),
	}
}

// SetFailGet sets whether Get operations should fail.
func (m *FailableMockStorage) SetFailGet(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failGet = fail
}

// SetFailSet sets whether Set operations should fail.
func (m *FailableMockStorage) SetFailSet(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failSet = fail
}

// SetFailDelete sets whether Delete operations should fail.
func (m *FailableMockStorage) SetFailDelete(fail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failDelete = fail
}

// Get overrides to add failure injection.
func (m *FailableMockStorage) Get(key []byte) ([]byte, error) {
	m.mu.RLock()
	shouldFail := m.failGet
	m.mu.RUnlock()

	if shouldFail {
		return nil, &dberrors.MessageError{Message: "mock get error"}
	}

	return m.MockStorage.Get(key)
}

// Set overrides to add failure injection.
func (m *FailableMockStorage) Set(key, value []byte) error {
	m.mu.RLock()
	shouldFail := m.failSet
	m.mu.RUnlock()

	if shouldFail {
		return &dberrors.MessageError{Message: "mock set error"}
	}

	return m.MockStorage.Set(key, value)
}

// Delete overrides to add failure injection.
func (m *FailableMockStorage) Delete(key []byte) error {
	m.mu.RLock()
	shouldFail := m.failDelete
	m.mu.RUnlock()

	if shouldFail {
		return &dberrors.MessageError{Message: "mock delete error"}
	}

	return m.MockStorage.Delete(key)
}

// Helper functions

func createTestPersistentAuthManager(t *testing.T) (*PersistentAuthManager, *MockStorage) {
	mockStorage := NewMockStorage()
	log := createTestLogger(t)

	config := newTestAuthConfig(t)

	authManager, err := NewPersistentAuthManager(config, mockStorage, log)
	require.NoError(t, err)
	require.NotNil(t, authManager)

	return authManager, mockStorage
}

// Test Bootstrap function

func TestBootstrap(t *testing.T) {
	t.Run("first time bootstrap with default config", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		result, err := Bootstrap(authManager, nil)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.True(t, result.AdminUserCreated)
		assert.Equal(t, "admin", result.AdminUsername)
		assert.NotEmpty(t, result.AdminUserID)
		assert.False(t, result.AlreadyBootstrapped)
		assert.Equal(t, 0, result.RolesCreated) // roles are seeded and owned by qrdb, not this composer
		assert.Equal(t, "Bootstrap completed successfully", result.Message)

		// Verify admin user was created
		adminUser := authManager.userStore.GetByUsername("admin")
		require.NotNil(t, adminUser)
		assert.Equal(t, "admin", adminUser.Username)
		assert.Contains(t, adminUser.Roles, "admin")

		// Verify bootstrap marker was set
		isBootstrapped, err := IsBootstrapped(mockStorage)
		require.NoError(t, err)
		assert.True(t, isBootstrapped)

		// Verify admin user ID was stored
		adminUserID, err := GetAdminUserID(mockStorage)
		require.NoError(t, err)
		assert.Equal(t, result.AdminUserID, adminUserID)
	})

	t.Run("bootstrap with custom admin username", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		config := &BootstrapConfig{
			CreateAdminUser: true,
			AdminUsername:   "superuser",
		}

		result, err := Bootstrap(authManager, config)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.True(t, result.AdminUserCreated)
		assert.Equal(t, "superuser", result.AdminUsername)

		// Verify custom admin user was created
		adminUser := authManager.userStore.GetByUsername("superuser")
		require.NotNil(t, adminUser)
		assert.Equal(t, "superuser", adminUser.Username)
	})

	t.Run("bootstrap without creating admin user", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		config := &BootstrapConfig{
			CreateAdminUser: false,
		}

		result, err := Bootstrap(authManager, config)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.False(t, result.AdminUserCreated)
		assert.Empty(t, result.AdminUserID)

		// Verify no admin user was created
		adminUser := authManager.userStore.GetByUsername("admin")
		assert.Nil(t, adminUser)

		// But bootstrap should still be marked complete
		isBootstrapped, err := IsBootstrapped(mockStorage)
		require.NoError(t, err)
		assert.True(t, isBootstrapped)
	})

	t.Run("bootstrap already completed without force recreate", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		// First bootstrap
		result1, err := Bootstrap(authManager, nil)
		require.NoError(t, err)
		firstAdminID := result1.AdminUserID

		// Second bootstrap should detect it's already done
		result2, err := Bootstrap(authManager, nil)
		require.NoError(t, err)
		require.NotNil(t, result2)

		assert.True(t, result2.AlreadyBootstrapped)
		assert.False(t, result2.AdminUserCreated)
		assert.Equal(t, "System already bootstrapped. Use ForceRecreate=true to reinitialize.", result2.Message)

		// Admin user should still exist
		adminUser := authManager.userStore.GetByUsername("admin")
		require.NotNil(t, adminUser)
		assert.Equal(t, firstAdminID, adminUser.ID)
	})

	t.Run("bootstrap with force recreate", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		// First bootstrap
		result1, err := Bootstrap(authManager, nil)
		require.NoError(t, err)
		firstAdminID := result1.AdminUserID

		// Force recreate
		config := &BootstrapConfig{
			CreateAdminUser: true,
			AdminUsername:   "admin",
			ForceRecreate:   true,
		}

		result2, err := Bootstrap(authManager, config)
		require.NoError(t, err)
		require.NotNil(t, result2)

		assert.True(t, result2.AdminUserCreated)
		assert.NotEqual(t, firstAdminID, result2.AdminUserID) // New ID

		// Old admin should be deleted, new one created
		adminUser := authManager.userStore.GetByUsername("admin")
		require.NotNil(t, adminUser)
		assert.Equal(t, result2.AdminUserID, adminUser.ID)
	})

	t.Run("bootstrap with existing admin user but not bootstrapped yet", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		// Manually create admin user before bootstrap (no password - certificate auth)
		_, err := authManager.CreateUser("admin", []string{"admin"})
		require.NoError(t, err)

		// Bootstrap should detect existing user and not recreate
		result, err := Bootstrap(authManager, nil)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.False(t, result.AdminUserCreated)
		assert.Contains(t, result.Message, "already exists")
	})

	t.Run("bootstrap empty admin username defaults to admin", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		config := &BootstrapConfig{
			CreateAdminUser: true,
			AdminUsername:   "", // Empty should default to "admin"
		}

		result, err := Bootstrap(authManager, config)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.Equal(t, "admin", result.AdminUsername)

		adminUser := authManager.userStore.GetByUsername("admin")
		require.NotNil(t, adminUser)
	})

	t.Run("bootstrap failure when storage Set fails for bootstrap marker", func(t *testing.T) {
		failableStorage := NewFailableMockStorage()
		defer failableStorage.Close()

		log := createTestLogger(t)
		config := newTestAuthConfig(t)

		authManager, err := NewPersistentAuthManager(config, failableStorage, log)
		require.NoError(t, err)

		// Make storage fail for Set operations after creation
		failableStorage.SetFailSet(true)

		bootstrapConfig := &BootstrapConfig{
			CreateAdminUser: true,
			AdminUsername:   "admin",
		}

		_, err = Bootstrap(authManager, bootstrapConfig)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to mark bootstrap complete")
	})

	t.Run("bootstrap failure when admin user creation fails", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		// Create a user with the same name to cause conflict
		_, err := authManager.CreateUser("admin", []string{"readonly"})
		require.NoError(t, err)

		config := &BootstrapConfig{
			CreateAdminUser: true,
			AdminUsername:   "admin",
			ForceRecreate:   false, // Don't force recreate
		}

		result, err := Bootstrap(authManager, config)
		require.NoError(t, err) // Should not error, just return message

		assert.False(t, result.AdminUserCreated)
		assert.Contains(t, result.Message, "already exists")
	})
}

func TestIsBootstrapped(t *testing.T) {
	t.Run("not bootstrapped", func(t *testing.T) {
		mockStorage := NewMockStorage()
		defer mockStorage.Close()

		isBootstrapped, err := IsBootstrapped(mockStorage)
		require.NoError(t, err)
		assert.False(t, isBootstrapped)
	})

	t.Run("bootstrapped", func(t *testing.T) {
		mockStorage := NewMockStorage()
		defer mockStorage.Close()

		kb := NewKeyBuilder()
		bootstrapKey := kb.BootstrapCompleteKey()
		err := mockStorage.Set(bootstrapKey, []byte(time.Now().Format(time.RFC3339)))
		require.NoError(t, err)

		isBootstrapped, err := IsBootstrapped(mockStorage)
		require.NoError(t, err)
		assert.True(t, isBootstrapped)
	})

	t.Run("bootstrapped with empty value", func(t *testing.T) {
		mockStorage := NewMockStorage()
		defer mockStorage.Close()

		kb := NewKeyBuilder()
		bootstrapKey := kb.BootstrapCompleteKey()
		err := mockStorage.Set(bootstrapKey, []byte(""))
		require.NoError(t, err)

		isBootstrapped, err := IsBootstrapped(mockStorage)
		require.NoError(t, err)
		assert.False(t, isBootstrapped) // Empty value means not bootstrapped
	})
}

func TestGetAdminUserID(t *testing.T) {
	t.Run("admin user exists", func(t *testing.T) {
		mockStorage := NewMockStorage()
		defer mockStorage.Close()

		kb := NewKeyBuilder()
		adminKey := kb.AdminUserKey()
		expectedID := "admin-user-123"
		err := mockStorage.Set(adminKey, []byte(expectedID))
		require.NoError(t, err)

		adminUserID, err := GetAdminUserID(mockStorage)
		require.NoError(t, err)
		assert.Equal(t, expectedID, adminUserID)
	})

	t.Run("admin user not found", func(t *testing.T) {
		mockStorage := NewMockStorage()
		defer mockStorage.Close()

		adminUserID, err := GetAdminUserID(mockStorage)
		assert.Error(t, err)
		assert.Empty(t, adminUserID)

		// Should be a QBError with ErrNotFound code
		var qbErr *dberrors.QBError
		ok := dberrors.As(err, &qbErr)
		require.True(t, ok)
		assert.Equal(t, dberrors.ErrNotFound, qbErr.Code)
		assert.Contains(t, qbErr.Error(), "admin user not found")
	})

	t.Run("storage error", func(t *testing.T) {
		failableStorage := NewFailableMockStorage()
		failableStorage.SetFailGet(true)
		defer failableStorage.Close()

		adminUserID, err := GetAdminUserID(failableStorage)
		assert.Error(t, err)
		assert.Empty(t, adminUserID)
	})
}

func TestBootstrapIntegration(t *testing.T) {
	t.Run("full bootstrap and admin user creation", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		// 1. Bootstrap the system
		result, err := Bootstrap(authManager, nil)
		require.NoError(t, err)
		require.NotNil(t, result)

		// 2. Verify admin user was created
		assert.True(t, result.AdminUserCreated)
		assert.Equal(t, "admin", result.AdminUsername)
		assert.NotEmpty(t, result.AdminUserID)

		// 3. Verify admin user exists in store
		adminUser := authManager.userStore.GetByUsername("admin")
		require.NotNil(t, adminUser)
		assert.Contains(t, adminUser.Roles, "admin")
	})

	t.Run("bootstrap persistence across restarts", func(t *testing.T) {
		mockStorage := NewMockStorage()
		defer mockStorage.Close()

		log := createTestLogger(t)
		config := newTestAuthConfig(t)

		// First instance - bootstrap
		authManager1, err := NewPersistentAuthManager(config, mockStorage, log)
		require.NoError(t, err)

		result, err := Bootstrap(authManager1, nil)
		require.NoError(t, err)
		adminUserID := result.AdminUserID

		// Simulate restart - create new auth manager with same storage
		authManager2, err := NewPersistentAuthManager(config, mockStorage, log)
		require.NoError(t, err)

		// Verify bootstrap status persists
		isBootstrapped, err := IsBootstrapped(mockStorage)
		require.NoError(t, err)
		assert.True(t, isBootstrapped)

		// Verify admin user ID persists
		retrievedAdminUserID, err := GetAdminUserID(mockStorage)
		require.NoError(t, err)
		assert.Equal(t, adminUserID, retrievedAdminUserID)

		// Verify admin user data persists
		adminUser := authManager2.userStore.GetByUsername("admin")
		require.NotNil(t, adminUser)
		assert.Equal(t, adminUserID, adminUser.ID)

	})
}

func TestBootstrapEdgeCases(t *testing.T) {
	t.Run("bootstrap with very long username", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		config := &BootstrapConfig{
			CreateAdminUser: true,
			AdminUsername:   "admin_with_very_long_username_that_might_cause_issues_in_some_systems",
		}

		result, err := Bootstrap(authManager, config)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.True(t, result.AdminUserCreated)

		adminUser := authManager.userStore.GetByUsername(config.AdminUsername)
		require.NotNil(t, adminUser)
	})

	t.Run("bootstrap with special characters in username", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		config := &BootstrapConfig{
			CreateAdminUser: true,
			AdminUsername:   "admin@example.com",
		}

		result, err := Bootstrap(authManager, config)
		require.NoError(t, err)
		require.NotNil(t, result)

		assert.True(t, result.AdminUserCreated)

		adminUser := authManager.userStore.GetByUsername(config.AdminUsername)
		require.NotNil(t, adminUser)
	})

	t.Run("concurrent bootstrap attempts", func(t *testing.T) {
		authManager, mockStorage := createTestPersistentAuthManager(t)
		defer mockStorage.Close()

		// Launch multiple bootstrap attempts concurrently
		const numGoroutines = 5
		results := make([]*BootstrapResult, numGoroutines)
		errs := make([]error, numGoroutines)

		var wg sync.WaitGroup
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				results[index], errs[index] = Bootstrap(authManager, nil)
			}(i)
		}

		wg.Wait()

		// At least one should succeed
		successCount := 0
		for i := 0; i < numGoroutines; i++ {
			if errs[i] == nil && results[i] != nil {
				successCount++
			}
		}
		assert.Greater(t, successCount, 0)

		// Verify only one admin user was created
		adminUsers := authManager.userStore.ListUsers()
		adminCount := 0
		for _, user := range adminUsers {
			if user.Username == "admin" {
				adminCount++
			}
		}
		assert.Equal(t, 1, adminCount)
	})
}
