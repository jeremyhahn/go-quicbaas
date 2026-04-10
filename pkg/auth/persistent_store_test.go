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

// Package auth provides comprehensive tests for persistent storage
package auth

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Test Helpers

func createTestStorageAndLogger(t *testing.T) (*MockStorage, *logger.Logger) {
	storage := NewMockStorage()
	log := createTestLogger(t)
	return storage, log
}

// =============================================================================
// PersistentUserStore Tests
// =============================================================================

func TestNewPersistentUserStore(t *testing.T) {
	tests := []struct {
		name        string
		storage     *MockStorage
		logger      *logger.Logger
		expectError bool
	}{
		{
			name:        "nil logger",
			storage:     NewMockStorage(),
			logger:      nil,
			expectError: true,
		},
		{
			name:        "valid parameters",
			storage:     NewMockStorage(),
			logger:      createTestLogger(t),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewPersistentUserStore(tt.storage, tt.logger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, store)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, store)
				assert.Equal(t, 0, store.Count())
			}
		})
	}
}

func TestPersistentUserStore_AddUser(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	user1 := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		password: "hashed_password",
		Created:  time.Now(),
	}

	user2 := &User{
		ID:       "user2",
		Username: "bob",
		Roles:    []string{RoleNameReadOnly},
		password: "hashed_password2",
		Created:  time.Now(),
	}

	// Add first user
	err = store.AddUser(user1)
	assert.NoError(t, err)
	assert.Equal(t, 1, store.Count())

	// Add second user
	err = store.AddUser(user2)
	assert.NoError(t, err)
	assert.Equal(t, 2, store.Count())

	// Try to add duplicate user ID
	duplicateID := &User{
		ID:       "user1",
		Username: "charlie",
		Roles:    []string{RoleNameAdmin},
		password: "hashed_password3",
		Created:  time.Now(),
	}
	err = store.AddUser(duplicateID)
	assert.Error(t, err)
	assert.Equal(t, 2, store.Count())

	// Try to add duplicate username
	duplicateUsername := &User{
		ID:       "user3",
		Username: "alice",
		Roles:    []string{RoleNameAdmin},
		password: "hashed_password4",
		Created:  time.Now(),
	}
	err = store.AddUser(duplicateUsername)
	assert.Error(t, err)
	assert.Equal(t, 2, store.Count())
}

func TestPersistentUserStore_GetByID(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	user := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		password: "hashed_password",
		Created:  time.Now(),
	}

	err = store.AddUser(user)
	require.NoError(t, err)

	// Get existing user (from cache)
	retrieved := store.GetByID("user1")
	assert.NotNil(t, retrieved)
	assert.Equal(t, user.ID, retrieved.ID)
	assert.Equal(t, user.Username, retrieved.Username)

	// Get non-existent user
	notFound := store.GetByID("nonexistent")
	assert.Nil(t, notFound)
}

func TestPersistentUserStore_GetByID_FromStorage(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	user := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		password: "hashed_password",
		Created:  time.Now(),
	}

	err = store.AddUser(user)
	require.NoError(t, err)

	// Clear cache to force load from storage
	store.mu.Lock()
	delete(store.cache, user.ID)
	store.mu.Unlock()

	// Get user (should load from storage)
	retrieved := store.GetByID("user1")
	assert.NotNil(t, retrieved)
	assert.Equal(t, user.ID, retrieved.ID)
	assert.Equal(t, user.Username, retrieved.Username)

	// Verify it's back in cache
	store.mu.RLock()
	_, inCache := store.cache["user1"]
	store.mu.RUnlock()
	assert.True(t, inCache)
}

func TestPersistentUserStore_GetByUsername(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	user := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		password: "hashed_password",
		Created:  time.Now(),
	}

	err = store.AddUser(user)
	require.NoError(t, err)

	// Get existing user (from cache)
	retrieved := store.GetByUsername("alice")
	assert.NotNil(t, retrieved)
	assert.Equal(t, user.ID, retrieved.ID)
	assert.Equal(t, user.Username, retrieved.Username)

	// Get non-existent user
	notFound := store.GetByUsername("nonexistent")
	assert.Nil(t, notFound)
}

func TestPersistentUserStore_GetByUsername_FromStorage(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	user := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		password: "hashed_password",
		Created:  time.Now(),
	}

	err = store.AddUser(user)
	require.NoError(t, err)

	// Clear cache to force load from storage
	store.mu.Lock()
	delete(store.cache, user.ID)
	delete(store.nameIndex, user.Username)
	store.mu.Unlock()

	// Get user by username (should load from storage)
	retrieved := store.GetByUsername("alice")
	assert.NotNil(t, retrieved)
	assert.Equal(t, user.ID, retrieved.ID)
	assert.Equal(t, user.Username, retrieved.Username)

	// Verify it's back in cache
	store.mu.RLock()
	_, inCache := store.cache["user1"]
	_, inIndex := store.nameIndex["alice"]
	store.mu.RUnlock()
	assert.True(t, inCache)
	assert.True(t, inIndex)
}

func TestPersistentUserStore_UpdateUser(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	user := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		password: "hashed_password",
		Created:  time.Now(),
	}

	err = store.AddUser(user)
	require.NoError(t, err)

	// Update user
	user.Roles = []string{RoleNameAdmin}
	user.LastAuth = time.Now()
	err = store.UpdateUser(user)
	assert.NoError(t, err)

	// Verify update
	retrieved := store.GetByID("user1")
	assert.NotNil(t, retrieved)
	assert.Equal(t, []string{RoleNameAdmin}, retrieved.Roles)

	// Try to update non-existent user
	nonExistent := &User{
		ID:       "nonexistent",
		Username: "bob",
		Roles:    []string{RoleNameReadOnly},
	}
	err = store.UpdateUser(nonExistent)
	assert.Error(t, err)
}

func TestPersistentUserStore_DeleteUser(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	user := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		password: "hashed_password",
		Created:  time.Now(),
	}

	err = store.AddUser(user)
	require.NoError(t, err)
	assert.Equal(t, 1, store.Count())

	// Delete user
	err = store.DeleteUser("user1")
	assert.NoError(t, err)
	assert.Equal(t, 0, store.Count())

	// Verify deletion
	retrieved := store.GetByID("user1")
	assert.Nil(t, retrieved)

	retrieved = store.GetByUsername("alice")
	assert.Nil(t, retrieved)

	// Try to delete non-existent user
	err = store.DeleteUser("nonexistent")
	assert.Error(t, err)
}

func TestPersistentUserStore_ListUsers(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Empty list
	users := store.ListUsers()
	assert.Equal(t, 0, len(users))

	// Add users
	user1 := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		Created:  time.Now(),
	}
	user2 := &User{
		ID:       "user2",
		Username: "bob",
		Roles:    []string{RoleNameReadOnly},
		Created:  time.Now(),
	}

	err = store.AddUser(user1)
	require.NoError(t, err)
	err = store.AddUser(user2)
	require.NoError(t, err)

	// List all users
	users = store.ListUsers()
	assert.Equal(t, 2, len(users))

	// Verify both users are in the list
	usernames := make(map[string]bool)
	for _, u := range users {
		usernames[u.Username] = true
	}
	assert.True(t, usernames["alice"])
	assert.True(t, usernames["bob"])
}

func TestPersistentUserStore_LoadAllUsers(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Pre-populate storage with users
	user1 := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		Created:  time.Now(),
	}
	user2 := &User{
		ID:       "user2",
		Username: "bob",
		Roles:    []string{RoleNameReadOnly},
		Created:  time.Now(),
	}

	kb := NewKeyBuilder()
	userData1, _ := json.Marshal(user1)
	userData2, _ := json.Marshal(user2)

	storage.Set(kb.UserKey(user1.ID), userData1)
	storage.Set(kb.UserByNameKey(user1.Username), []byte(user1.ID))
	storage.Set(kb.UserKey(user2.ID), userData2)
	storage.Set(kb.UserByNameKey(user2.Username), []byte(user2.ID))

	// Create store (should load users)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Verify users were loaded
	assert.Equal(t, 2, store.Count())
	assert.NotNil(t, store.GetByID("user1"))
	assert.NotNil(t, store.GetByID("user2"))
	assert.NotNil(t, store.GetByUsername("alice"))
	assert.NotNil(t, store.GetByUsername("bob"))
}

func TestPersistentUserStore_LoadAllUsers_SkipsIndexKeys(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Pre-populate with a user and its index
	user := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		Created:  time.Now(),
	}

	kb := NewKeyBuilder()
	userData, _ := json.Marshal(user)

	storage.Set(kb.UserKey(user.ID), userData)
	storage.Set(kb.UserByNameKey(user.Username), []byte(user.ID))
	storage.Set(kb.UserMetadataKey(user.ID), []byte("metadata"))

	// Create store (should load only user data, not index/metadata keys)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Should have exactly 1 user
	assert.Equal(t, 1, store.Count())
	assert.NotNil(t, store.GetByID("user1"))
}

func TestPersistentUserStore_Count(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	assert.Equal(t, 0, store.Count())

	user1 := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		Created:  time.Now(),
	}

	err = store.AddUser(user1)
	require.NoError(t, err)
	assert.Equal(t, 1, store.Count())

	user2 := &User{
		ID:       "user2",
		Username: "bob",
		Roles:    []string{RoleNameReadOnly},
		Created:  time.Now(),
	}

	err = store.AddUser(user2)
	require.NoError(t, err)
	assert.Equal(t, 2, store.Count())

	err = store.DeleteUser("user1")
	require.NoError(t, err)
	assert.Equal(t, 1, store.Count())
}

func TestPersistentUserStore_ConcurrentAccess(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent writes
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			user := &User{
				ID:       fmt.Sprintf("user%d", idx),
				Username: fmt.Sprintf("user%d", idx),
				Roles:    []string{RoleNameReadWrite},
				Created:  time.Now(),
			}
			store.AddUser(user)
		}(i)
	}
	wg.Wait()

	// Verify all users were added
	assert.Equal(t, numGoroutines, store.Count())

	// Concurrent reads
	wg.Add(numGoroutines * 2)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			store.GetByID(fmt.Sprintf("user%d", idx))
		}(i)

		go func(idx int) {
			defer wg.Done()
			store.GetByUsername(fmt.Sprintf("user%d", idx))
		}(i)
	}
	wg.Wait()

	// Concurrent updates
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			user := store.GetByID(fmt.Sprintf("user%d", idx))
			if user != nil {
				user.Roles = []string{RoleNameAdmin}
				store.UpdateUser(user)
			}
		}(i)
	}
	wg.Wait()
}

// =============================================================================
// PersistentTokenStore Tests
// =============================================================================

func TestNewPersistentTokenStore(t *testing.T) {
	tests := []struct {
		name        string
		storage     *MockStorage
		logger      *logger.Logger
		expectError bool
	}{
		{
			name:        "nil logger",
			storage:     NewMockStorage(),
			logger:      nil,
			expectError: true,
		},
		{
			name:        "valid parameters",
			storage:     NewMockStorage(),
			logger:      createTestLogger(t),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewPersistentTokenStore(tt.storage, tt.logger)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, store)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, store)
				// Cleanup
				if store != nil {
					store.Close()
				}
			}
		})
	}
}

func TestPersistentTokenStore_AddRefreshToken(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	userID := "user1"
	token := "refresh_token_123"

	// Add refresh token
	store.AddRefreshToken(userID, token)

	// Verify token is valid
	assert.True(t, store.IsValidRefreshToken(userID, token))

	// Add another token for the same user
	token2 := "refresh_token_456"
	store.AddRefreshToken(userID, token2)

	assert.True(t, store.IsValidRefreshToken(userID, token))
	assert.True(t, store.IsValidRefreshToken(userID, token2))
}

func TestPersistentTokenStore_IsValidRefreshToken(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	userID := "user1"
	token := "refresh_token_123"

	// Non-existent token
	assert.False(t, store.IsValidRefreshToken(userID, token))

	// Add token
	store.AddRefreshToken(userID, token)
	assert.True(t, store.IsValidRefreshToken(userID, token))

	// Wrong user
	assert.False(t, store.IsValidRefreshToken("user2", token))

	// Wrong token
	assert.False(t, store.IsValidRefreshToken(userID, "wrong_token"))
}

func TestPersistentTokenStore_Revoke(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	token := "access_token_123"

	// Token not revoked initially
	assert.False(t, store.IsRevoked(token))

	// Revoke token
	store.Revoke(token)

	// Token is now revoked
	assert.True(t, store.IsRevoked(token))
}

func TestPersistentTokenStore_IsRevoked(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	token1 := "token1"
	token2 := "token2"

	// Neither token revoked
	assert.False(t, store.IsRevoked(token1))
	assert.False(t, store.IsRevoked(token2))

	// Revoke token1
	store.Revoke(token1)
	assert.True(t, store.IsRevoked(token1))
	assert.False(t, store.IsRevoked(token2))

	// Revoke token2
	store.Revoke(token2)
	assert.True(t, store.IsRevoked(token1))
	assert.True(t, store.IsRevoked(token2))
}

func TestPersistentTokenStore_RevokeUserTokens(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	userID := "user1"
	token1 := "refresh_token_1"
	token2 := "refresh_token_2"

	// Add tokens
	store.AddRefreshToken(userID, token1)
	store.AddRefreshToken(userID, token2)

	// Verify tokens are valid
	assert.True(t, store.IsValidRefreshToken(userID, token1))
	assert.True(t, store.IsValidRefreshToken(userID, token2))

	// Revoke all user tokens
	store.RevokeUserTokens(userID)

	// Tokens should no longer be valid
	assert.False(t, store.IsValidRefreshToken(userID, token1))
	assert.False(t, store.IsValidRefreshToken(userID, token2))

	// Tokens should be in revoked list
	assert.True(t, store.IsRevoked(token1))
	assert.True(t, store.IsRevoked(token2))

	// User's refresh tokens should be cleared
	store.cache.mu.RLock()
	_, exists := store.cache.refreshTokens[userID]
	store.cache.mu.RUnlock()
	assert.False(t, exists)

	// Revoking non-existent user should not panic
	store.RevokeUserTokens("nonexistent_user")
}

func TestPersistentTokenStore_LoadRefreshTokens(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Pre-populate storage with tokens
	userID := "user1"
	tokenHash := hashToken("test_token")
	expiry := time.Now().Add(7 * 24 * time.Hour)
	expiryBytes, _ := expiry.MarshalBinary()

	kb := NewKeyBuilder()
	storage.Set(kb.RefreshTokenKey(userID, tokenHash), expiryBytes)

	// Create store (should load tokens)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	// Verify token was loaded
	assert.True(t, store.IsValidRefreshToken(userID, "test_token"))
}

func TestPersistentTokenStore_LoadRevokedTokens(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Pre-populate storage with revoked token
	token := "revoked_token"
	tokenHash := hashToken(token)
	revokedAt := time.Now()
	revokedBytes, _ := revokedAt.MarshalBinary()

	kb := NewKeyBuilder()
	storage.Set(kb.RevokedTokenKey(tokenHash), revokedBytes)

	// Create store (should load revoked tokens)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	// Verify token is marked as revoked
	assert.True(t, store.IsRevoked(token))
}

func TestPersistentTokenStore_LoadTokens_SkipsExpired(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Add expired refresh token
	userID := "user1"
	expiredHash := hashToken("expired_token")
	expiredTime := time.Now().Add(-24 * time.Hour) // Expired
	expiredBytes, _ := expiredTime.MarshalBinary()

	kb := NewKeyBuilder()
	storage.Set(kb.RefreshTokenKey(userID, expiredHash), expiredBytes)

	// Add valid refresh token
	validHash := hashToken("valid_token")
	validTime := time.Now().Add(24 * time.Hour) // Valid
	validBytes, _ := validTime.MarshalBinary()
	storage.Set(kb.RefreshTokenKey(userID, validHash), validBytes)

	// Create store (should load only valid token)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	// Expired token should not be loaded
	assert.False(t, store.IsValidRefreshToken(userID, "expired_token"))

	// Valid token should be loaded
	assert.True(t, store.IsValidRefreshToken(userID, "valid_token"))
}

func TestPersistentTokenStore_LoadTokens_SkipsOldRevoked(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Add old revoked token (more than 7 days)
	oldToken := "old_revoked_token"
	oldHash := hashToken(oldToken)
	oldTime := time.Now().Add(-8 * 24 * time.Hour)
	oldBytes, _ := oldTime.MarshalBinary()

	kb := NewKeyBuilder()
	storage.Set(kb.RevokedTokenKey(oldHash), oldBytes)

	// Add recent revoked token
	recentToken := "recent_revoked_token"
	recentHash := hashToken(recentToken)
	recentTime := time.Now().Add(-1 * time.Hour)
	recentBytes, _ := recentTime.MarshalBinary()
	storage.Set(kb.RevokedTokenKey(recentHash), recentBytes)

	// Create store
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	// Old token should not be loaded
	assert.False(t, store.IsRevoked(oldToken))

	// Recent token should be loaded
	assert.True(t, store.IsRevoked(recentToken))
}

func TestPersistentTokenStore_CleanupExpiredTokens(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	userID := "user1"

	// Add tokens that will expire soon
	store.cache.mu.Lock()
	if _, exists := store.cache.refreshTokens[userID]; !exists {
		store.cache.refreshTokens[userID] = make(map[string]time.Time)
	}
	expiredHash := hashToken("expired_token")
	validHash := hashToken("valid_token")
	store.cache.refreshTokens[userID][expiredHash] = time.Now().Add(-1 * time.Hour)
	store.cache.refreshTokens[userID][validHash] = time.Now().Add(24 * time.Hour)

	// Add old revoked token
	oldRevokedHash := hashToken("old_revoked")
	store.cache.revokedTokens[oldRevokedHash] = time.Now().Add(-8 * 24 * time.Hour)

	// Add recent revoked token
	recentRevokedHash := hashToken("recent_revoked")
	store.cache.revokedTokens[recentRevokedHash] = time.Now()
	store.cache.mu.Unlock()

	// Perform cleanup
	store.performCleanup()

	// Verify expired refresh token was removed
	store.cache.mu.RLock()
	tokens, exists := store.cache.refreshTokens[userID]
	assert.True(t, exists)
	_, hasExpired := tokens[expiredHash]
	_, hasValid := tokens[validHash]
	assert.False(t, hasExpired)
	assert.True(t, hasValid)

	// Verify old revoked token was removed
	_, hasOldRevoked := store.cache.revokedTokens[oldRevokedHash]
	assert.False(t, hasOldRevoked)

	// Verify recent revoked token remains
	_, hasRecentRevoked := store.cache.revokedTokens[recentRevokedHash]
	assert.True(t, hasRecentRevoked)
	store.cache.mu.RUnlock()
}

func TestPersistentTokenStore_CleanupExpiredTokens_RemovesEmptyUserMaps(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	userID := "user1"

	// Add only expired tokens for a user
	store.cache.mu.Lock()
	store.cache.refreshTokens[userID] = make(map[string]time.Time)
	expiredHash := hashToken("expired_token")
	store.cache.refreshTokens[userID][expiredHash] = time.Now().Add(-1 * time.Hour)
	store.cache.mu.Unlock()

	// Perform cleanup
	store.performCleanup()

	// Verify user entry was removed entirely
	store.cache.mu.RLock()
	_, exists := store.cache.refreshTokens[userID]
	store.cache.mu.RUnlock()
	assert.False(t, exists)
}

func TestPersistentTokenStore_Close(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)

	// Close should succeed
	err = store.Close()
	assert.NoError(t, err)

	// Cleanup goroutine should have stopped
	// (Verified by the test not hanging)
}

func TestPersistentTokenStore_ConcurrentAccess(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent token additions
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			userID := fmt.Sprintf("user%d", idx)
			token := fmt.Sprintf("token%d", idx)
			store.AddRefreshToken(userID, token)
		}(i)
	}
	wg.Wait()

	// Concurrent reads
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			userID := fmt.Sprintf("user%d", idx)
			token := fmt.Sprintf("token%d", idx)
			store.IsValidRefreshToken(userID, token)
		}(i)
	}
	wg.Wait()

	// Concurrent revocations
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			token := fmt.Sprintf("token%d", idx)
			store.Revoke(token)
		}(i)
	}
	wg.Wait()
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestHashToken_PersistentStore(t *testing.T) {
	token1 := "test_token_1"
	token2 := "test_token_2"

	hash1 := hashToken(token1)
	hash2 := hashToken(token2)

	// Different tokens should produce different hashes
	assert.NotEqual(t, hash1, hash2)

	// Same token should produce same hash
	hash1Again := hashToken(token1)
	assert.Equal(t, hash1, hash1Again)

	// Hash should not be empty
	assert.NotEmpty(t, hash1)
}

func TestPersistentUserStore_LoadAllUsers_InvalidJSON(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Pre-populate with invalid JSON
	kb := NewKeyBuilder()
	storage.Set(kb.UserKey("invalid"), []byte("not valid json"))

	// Create store (should skip invalid entry and log warning)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Should have 0 users due to invalid JSON
	assert.Equal(t, 0, store.Count())
}

func TestPersistentUserStore_GetByID_InvalidJSON(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Manually insert invalid JSON into storage
	kb := NewKeyBuilder()
	storage.Set(kb.UserKey("bad_user"), []byte("invalid json"))

	// Try to get user with invalid JSON (should return nil and log error)
	user := store.GetByID("bad_user")
	assert.Nil(t, user)
}

func TestPersistentUserStore_GetByUsername_InvalidJSON(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	// Manually insert username index and invalid user data
	kb := NewKeyBuilder()
	storage.Set(kb.UserByNameKey("baduser"), []byte("user_bad"))
	storage.Set(kb.UserKey("user_bad"), []byte("invalid json"))

	// Try to get user with invalid JSON (should return nil and log error)
	user := store.GetByUsername("baduser")
	assert.Nil(t, user)
}

func TestPersistentTokenStore_LoadTokens_InvalidExpiry(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Add token with invalid expiry bytes
	kb := NewKeyBuilder()
	storage.Set(kb.RefreshTokenKey("user1", "tokenhash"), []byte("invalid"))

	// Create store (should skip invalid token and log warning)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	// Token should not be loaded
	store.cache.mu.RLock()
	tokens, exists := store.cache.refreshTokens["user1"]
	store.cache.mu.RUnlock()
	assert.False(t, exists || len(tokens) > 0)
}

func TestPersistentTokenStore_LoadTokens_InvalidRevokedTime(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)

	// Add revoked token with invalid time bytes
	kb := NewKeyBuilder()
	storage.Set(kb.RevokedTokenKey("tokenhash"), []byte("invalid"))

	// Create store (should skip invalid entry and log warning)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	// Token should not be in revoked list
	store.cache.mu.RLock()
	_, exists := store.cache.revokedTokens["tokenhash"]
	store.cache.mu.RUnlock()
	assert.False(t, exists)
}

// Test edge case: Multiple users with same token hash (shouldn't happen but test robustness).
func TestPersistentTokenStore_MultipleUsersWithTokens(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentTokenStore(storage, log)
	require.NoError(t, err)
	defer store.Close()

	user1 := "user1"
	user2 := "user2"
	token1 := "token_for_user1"
	token2 := "token_for_user2"

	store.AddRefreshToken(user1, token1)
	store.AddRefreshToken(user2, token2)

	// Each user should only have their own token
	assert.True(t, store.IsValidRefreshToken(user1, token1))
	assert.False(t, store.IsValidRefreshToken(user1, token2))
	assert.True(t, store.IsValidRefreshToken(user2, token2))
	assert.False(t, store.IsValidRefreshToken(user2, token1))

	// Revoke user1's tokens
	store.RevokeUserTokens(user1)

	// User1's tokens should be invalid
	assert.False(t, store.IsValidRefreshToken(user1, token1))
	// User2's tokens should still be valid
	assert.True(t, store.IsValidRefreshToken(user2, token2))
}

// Test cache consistency after storage operations.
func TestPersistentUserStore_CacheConsistency(t *testing.T) {
	storage, log := createTestStorageAndLogger(t)
	store, err := NewPersistentUserStore(storage, log)
	require.NoError(t, err)

	user := &User{
		ID:       "user1",
		Username: "alice",
		Roles:    []string{RoleNameReadWrite},
		Created:  time.Now(),
	}

	// Add user
	err = store.AddUser(user)
	require.NoError(t, err)

	// Verify cache has user
	store.mu.RLock()
	cachedUser, inCache := store.cache["user1"]
	cachedID, inIndex := store.nameIndex["alice"]
	store.mu.RUnlock()

	assert.True(t, inCache)
	assert.True(t, inIndex)
	assert.Equal(t, "user1", cachedID)
	assert.Equal(t, "alice", cachedUser.Username)

	// Update user
	user.Roles = []string{RoleNameAdmin}
	err = store.UpdateUser(user)
	require.NoError(t, err)

	// Verify cache is updated
	store.mu.RLock()
	cachedUser = store.cache["user1"]
	store.mu.RUnlock()
	assert.Equal(t, []string{RoleNameAdmin}, cachedUser.Roles)

	// Delete user
	err = store.DeleteUser("user1")
	require.NoError(t, err)

	// Verify cache is cleared
	store.mu.RLock()
	_, inCache = store.cache["user1"]
	_, inIndex = store.nameIndex["alice"]
	store.mu.RUnlock()

	assert.False(t, inCache)
	assert.False(t, inIndex)
}
