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

// Package auth provides additional unit tests for persistent_store.go
package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPersistentUserStore_SetRoleManager tests SetRoleManager method.
func TestPersistentUserStore_SetRoleManager(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	ps, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	rm := NewRoleManager(log)
	ps.SetRoleManager(rm)

	// Role manager should be set
	assert.NotNil(t, ps.roleManager)
}

// TestPersistentUserStore_AssignRole tests AssignRole method.
func TestPersistentUserStore_AssignRole(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	ps, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	rm := NewRoleManager(log)
	ps.SetRoleManager(rm)

	t.Run("success", func(t *testing.T) {
		user := &User{
			ID:       "user1",
			Username: "testuser",
			Roles:    []string{RoleNameReadOnly},
			Created:  time.Now(),
		}
		err := ps.AddUser(user)
		require.NoError(t, err)

		err = ps.AssignRole("user1", RoleNameReadWrite)
		assert.NoError(t, err)

		retrieved := ps.GetByID("user1")
		assert.Contains(t, retrieved.Roles, RoleNameReadOnly)
		assert.Contains(t, retrieved.Roles, RoleNameReadWrite)
		assert.True(t, retrieved.LastAuth.After(user.Created))
	})

	t.Run("user not found", func(t *testing.T) {
		err := ps.AssignRole("nonexistent", RoleNameAdmin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("role not found", func(t *testing.T) {
		user := &User{
			ID:       "user2",
			Username: "testuser2",
			Roles:    []string{RoleNameReadOnly},
		}
		err := ps.AddUser(user)
		require.NoError(t, err)

		err = ps.AssignRole("user2", "nonexistent-role")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "role not found")
	})

	t.Run("user already has role", func(t *testing.T) {
		user := &User{
			ID:       "user3",
			Username: "testuser3",
			Roles:    []string{RoleNameReadOnly},
		}
		err := ps.AddUser(user)
		require.NoError(t, err)

		err = ps.AssignRole("user3", RoleNameReadOnly)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user already has role")
	})

	t.Run("no role manager", func(t *testing.T) {
		storage2 := newAuthMockStorage()
		psNoRM, err := NewPersistentUserStore(storage2, log)
		require.NoError(t, err)

		user := &User{
			ID:       "user4",
			Username: "testuser4",
			Roles:    []string{},
		}
		err = psNoRM.AddUser(user)
		require.NoError(t, err)

		// Should succeed without role manager
		err = psNoRM.AssignRole("user4", "any-role")
		assert.NoError(t, err)

		retrieved := psNoRM.GetByID("user4")
		assert.Contains(t, retrieved.Roles, "any-role")
	})
}

// TestPersistentUserStore_RevokeRole tests RevokeRole method.
func TestPersistentUserStore_RevokeRole(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	ps, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		user := &User{
			ID:       "user1",
			Username: "testuser",
			Roles:    []string{RoleNameReadOnly, RoleNameReadWrite},
			Created:  time.Now(),
		}
		err := ps.AddUser(user)
		require.NoError(t, err)

		err = ps.RevokeRole("user1", RoleNameReadWrite)
		assert.NoError(t, err)

		retrieved := ps.GetByID("user1")
		assert.Contains(t, retrieved.Roles, RoleNameReadOnly)
		assert.NotContains(t, retrieved.Roles, RoleNameReadWrite)
		assert.Len(t, retrieved.Roles, 1)
		assert.True(t, retrieved.LastAuth.After(user.Created))
	})

	t.Run("user not found", func(t *testing.T) {
		err := ps.RevokeRole("nonexistent", RoleNameAdmin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("user does not have role", func(t *testing.T) {
		user := &User{
			ID:       "user2",
			Username: "testuser2",
			Roles:    []string{RoleNameReadOnly},
		}
		err := ps.AddUser(user)
		require.NoError(t, err)

		err = ps.RevokeRole("user2", RoleNameAdmin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user does not have role")
	})

	t.Run("revoke first role", func(t *testing.T) {
		user := &User{
			ID:       "user3",
			Username: "testuser3",
			Roles:    []string{RoleNameAdmin, RoleNameReadWrite, RoleNameReadOnly},
		}
		err := ps.AddUser(user)
		require.NoError(t, err)

		err = ps.RevokeRole("user3", RoleNameAdmin)
		assert.NoError(t, err)

		retrieved := ps.GetByID("user3")
		assert.NotContains(t, retrieved.Roles, RoleNameAdmin)
		assert.Len(t, retrieved.Roles, 2)
		assert.Equal(t, RoleNameReadWrite, retrieved.Roles[0])
	})

	t.Run("revoke middle role", func(t *testing.T) {
		user := &User{
			ID:       "user4",
			Username: "testuser4",
			Roles:    []string{RoleNameAdmin, RoleNameReadWrite, RoleNameReadOnly},
		}
		err := ps.AddUser(user)
		require.NoError(t, err)

		err = ps.RevokeRole("user4", RoleNameReadWrite)
		assert.NoError(t, err)

		retrieved := ps.GetByID("user4")
		assert.NotContains(t, retrieved.Roles, RoleNameReadWrite)
		assert.Len(t, retrieved.Roles, 2)
	})

	t.Run("revoke last role", func(t *testing.T) {
		user := &User{
			ID:       "user5",
			Username: "testuser5",
			Roles:    []string{RoleNameAdmin},
		}
		err := ps.AddUser(user)
		require.NoError(t, err)

		err = ps.RevokeRole("user5", RoleNameAdmin)
		assert.NoError(t, err)

		retrieved := ps.GetByID("user5")
		assert.Empty(t, retrieved.Roles)
	})
}

// TestPersistentTokenStore_ErrorPaths tests error handling in token store.
func TestPersistentTokenStore_ErrorPaths(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	pts, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer pts.Close()

	t.Run("add refresh token with storage error", func(t *testing.T) {
		// Storage errors are logged but don't fail the operation
		pts.AddRefreshToken("user1", "token1")
		// Should still add to cache even if storage fails
		assert.True(t, pts.IsValidRefreshToken("user1", "token1"))
	})

	t.Run("revoke token with storage error", func(t *testing.T) {
		// Storage errors are logged but don't fail the operation
		pts.Revoke("token1")
		// Should still mark as revoked in cache
		assert.True(t, pts.IsRevoked("token1"))
	})

	t.Run("revoke user tokens with no tokens", func(t *testing.T) {
		// Should not panic or error
		pts.RevokeUserTokens("nonexistent-user")
	})
}

// TestPersistentUserStore_ErrorPaths tests error handling in user store.
func TestPersistentUserStore_ErrorPaths(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	ps, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	t.Run("add duplicate user ID", func(t *testing.T) {
		user1 := &User{
			ID:       "duplicate-id",
			Username: "user1",
			Roles:    []string{RoleNameReadOnly},
		}
		err := ps.AddUser(user1)
		require.NoError(t, err)

		user2 := &User{
			ID:       "duplicate-id",
			Username: "user2",
			Roles:    []string{RoleNameReadWrite},
		}
		err = ps.AddUser(user2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("add duplicate username", func(t *testing.T) {
		user1 := &User{
			ID:       "id1",
			Username: "duplicate-username",
			Roles:    []string{RoleNameReadOnly},
		}
		err := ps.AddUser(user1)
		require.NoError(t, err)

		user2 := &User{
			ID:       "id2",
			Username: "duplicate-username",
			Roles:    []string{RoleNameReadWrite},
		}
		err = ps.AddUser(user2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("update non-existent user", func(t *testing.T) {
		user := &User{
			ID:       "nonexistent",
			Username: "testuser",
			Roles:    []string{RoleNameReadOnly},
		}
		err := ps.UpdateUser(user)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("delete non-existent user", func(t *testing.T) {
		err := ps.DeleteUser("nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// TestPersistentTokenStore_CleanupDetails tests cleanup functionality.
func TestPersistentTokenStore_CleanupDetails(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	pts, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer pts.Close()

	now := time.Now()

	// Add valid and expired tokens
	pts.cache.mu.Lock()
	pts.cache.refreshTokens["user1"] = make(map[string]time.Time)
	pts.cache.refreshTokens["user1"]["valid"] = now.Add(1 * time.Hour)
	pts.cache.refreshTokens["user1"]["expired"] = now.Add(-1 * time.Hour)

	pts.cache.revokedTokens["recent"] = now.Add(-1 * time.Hour)
	pts.cache.revokedTokens["old"] = now.Add(-8 * 24 * time.Hour)
	pts.cache.mu.Unlock()

	// Run cleanup
	pts.performCleanup()

	// Check results
	pts.cache.mu.RLock()
	assert.Contains(t, pts.cache.refreshTokens["user1"], "valid")
	assert.NotContains(t, pts.cache.refreshTokens["user1"], "expired")
	assert.Contains(t, pts.cache.revokedTokens, "recent")
	assert.NotContains(t, pts.cache.revokedTokens, "old")
	pts.cache.mu.RUnlock()
}

// TestPersistentUserStore_ConcurrentAccessDetails tests concurrent access to persistent store.
func TestPersistentUserStore_ConcurrentAccessDetails(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	ps, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Add initial user
	user := &User{
		ID:       "concurrent-user",
		Username: "testuser",
		Roles:    []string{RoleNameReadOnly},
	}
	err = ps.AddUser(user)
	require.NoError(t, err)

	// Concurrent reads and writes
	done := make(chan bool, 10)
	for i := 0; i < 5; i++ {
		go func() {
			ps.GetByID("concurrent-user")
			done <- true
		}()
		go func() {
			ps.GetByUsername("testuser")
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestPersistentTokenStore_RevokeAllUserTokens tests revoking all tokens for a user.
func TestPersistentTokenStore_RevokeAllUserTokens(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	pts, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer pts.Close()

	// Add multiple tokens for user
	pts.AddRefreshToken("user1", "token1")
	pts.AddRefreshToken("user1", "token2")
	pts.AddRefreshToken("user1", "token3")

	// Verify tokens are valid
	assert.True(t, pts.IsValidRefreshToken("user1", "token1"))
	assert.True(t, pts.IsValidRefreshToken("user1", "token2"))
	assert.True(t, pts.IsValidRefreshToken("user1", "token3"))

	// Revoke all user tokens
	pts.RevokeUserTokens("user1")

	// User should have no refresh tokens - they are removed from the refresh token map
	assert.False(t, pts.IsValidRefreshToken("user1", "token1"))
	assert.False(t, pts.IsValidRefreshToken("user1", "token2"))
	assert.False(t, pts.IsValidRefreshToken("user1", "token3"))
}

// TestPersistentUserStore_GetByID_CacheLogic tests cache fallback logic.
func TestPersistentUserStore_GetByID_CacheLogic(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	ps, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Add user
	user := &User{
		ID:       "cache-test",
		Username: "cacheuser",
		Roles:    []string{RoleNameReadOnly},
	}
	err = ps.AddUser(user)
	require.NoError(t, err)

	// Get from cache
	retrieved := ps.GetByID("cache-test")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "cacheuser", retrieved.Username)

	// Clear cache to test storage fallback
	ps.mu.Lock()
	delete(ps.cache, "cache-test")
	ps.mu.Unlock()

	// Should load from storage
	retrieved = ps.GetByID("cache-test")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "cacheuser", retrieved.Username)
}

// TestPersistentUserStore_GetByUsername_CacheLogic tests username lookup cache fallback.
func TestPersistentUserStore_GetByUsername_CacheLogic(t *testing.T) {
	storage := newAuthMockStorage()
	log := createPersistentTestLogger(t)

	ps, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Add user
	user := &User{
		ID:       "username-cache-test",
		Username: "uniqueuser",
		Roles:    []string{RoleNameReadOnly},
	}
	err = ps.AddUser(user)
	require.NoError(t, err)

	// Get from cache
	retrieved := ps.GetByUsername("uniqueuser")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "username-cache-test", retrieved.ID)

	// Clear cache to test storage fallback
	ps.mu.Lock()
	delete(ps.cache, "username-cache-test")
	delete(ps.nameIndex, "uniqueuser")
	ps.mu.Unlock()

	// Should load from storage
	retrieved = ps.GetByUsername("uniqueuser")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "username-cache-test", retrieved.ID)
}
