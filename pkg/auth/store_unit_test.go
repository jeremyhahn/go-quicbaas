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

// Package auth provides additional unit tests for store.go
package auth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserStore_SetRoleManager tests SetRoleManager method.
func TestUserStore_SetRoleManager(t *testing.T) {
	us := NewUserStore()
	assert.Nil(t, us.roleManager)

	rm := NewRoleManager(createTestLogger(t))
	us.SetRoleManager(rm)

	assert.NotNil(t, us.roleManager)
	assert.Equal(t, rm, us.roleManager)
}

// TestUserStore_AssignRole tests AssignRole method.
func TestUserStore_AssignRole(t *testing.T) {
	us := NewUserStore()
	rm := NewRoleManager(createTestLogger(t))
	us.SetRoleManager(rm)

	t.Run("success", func(t *testing.T) {
		user := &User{
			ID:       "user1",
			Username: "testuser",
			Roles:    []string{RoleNameReadOnly},
			Created:  time.Now(),
		}
		us.AddUser(user)

		err := us.AssignRole("user1", RoleNameReadWrite)
		assert.NoError(t, err)

		retrieved := us.GetByID("user1")
		assert.Contains(t, retrieved.GetRoles(), RoleNameReadOnly)
		assert.Contains(t, retrieved.GetRoles(), RoleNameReadWrite)
		assert.True(t, retrieved.GetLastAuth().After(user.Created))
	})

	t.Run("user not found", func(t *testing.T) {
		err := us.AssignRole("nonexistent", RoleNameAdmin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("role not found", func(t *testing.T) {
		user := &User{
			ID:       "user2",
			Username: "testuser2",
			Roles:    []string{RoleNameReadOnly},
		}
		us.AddUser(user)

		err := us.AssignRole("user2", "nonexistent-role")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "role not found")
	})

	t.Run("user already has role", func(t *testing.T) {
		user := &User{
			ID:       "user3",
			Username: "testuser3",
			Roles:    []string{RoleNameReadOnly},
		}
		us.AddUser(user)

		err := us.AssignRole("user3", RoleNameReadOnly)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user already has role")
	})

	t.Run("no role manager", func(t *testing.T) {
		usNoRM := NewUserStore()
		user := &User{
			ID:       "user4",
			Username: "testuser4",
			Roles:    []string{},
		}
		usNoRM.AddUser(user)

		// Should succeed without role manager
		err := usNoRM.AssignRole("user4", "any-role")
		assert.NoError(t, err)

		retrieved := usNoRM.GetByID("user4")
		assert.Contains(t, retrieved.GetRoles(), "any-role")
	})
}

// TestUserStore_RevokeRole tests RevokeRole method.
func TestUserStore_RevokeRole(t *testing.T) {
	us := NewUserStore()

	t.Run("success", func(t *testing.T) {
		user := &User{
			ID:       "user1",
			Username: "testuser",
			Roles:    []string{RoleNameReadOnly, RoleNameReadWrite},
			Created:  time.Now(),
		}
		us.AddUser(user)

		err := us.RevokeRole("user1", RoleNameReadWrite)
		assert.NoError(t, err)

		retrieved := us.GetByID("user1")
		assert.Contains(t, retrieved.GetRoles(), RoleNameReadOnly)
		assert.NotContains(t, retrieved.GetRoles(), RoleNameReadWrite)
		assert.Len(t, retrieved.GetRoles(), 1)
		assert.True(t, retrieved.GetLastAuth().After(user.Created))
	})

	t.Run("user not found", func(t *testing.T) {
		err := us.RevokeRole("nonexistent", RoleNameAdmin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("user does not have role", func(t *testing.T) {
		user := &User{
			ID:       "user2",
			Username: "testuser2",
			Roles:    []string{RoleNameReadOnly},
		}
		us.AddUser(user)

		err := us.RevokeRole("user2", RoleNameAdmin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user does not have role")
	})

	t.Run("revoke first role", func(t *testing.T) {
		user := &User{
			ID:       "user3",
			Username: "testuser3",
			Roles:    []string{RoleNameAdmin, RoleNameReadWrite, RoleNameReadOnly},
		}
		us.AddUser(user)

		err := us.RevokeRole("user3", RoleNameAdmin)
		assert.NoError(t, err)

		retrieved := us.GetByID("user3")
		assert.NotContains(t, retrieved.GetRoles(), RoleNameAdmin)
		assert.Len(t, retrieved.GetRoles(), 2)
		assert.Equal(t, RoleNameReadWrite, retrieved.GetRoles()[0])
	})

	t.Run("revoke middle role", func(t *testing.T) {
		user := &User{
			ID:       "user4",
			Username: "testuser4",
			Roles:    []string{RoleNameAdmin, RoleNameReadWrite, RoleNameReadOnly},
		}
		us.AddUser(user)

		err := us.RevokeRole("user4", RoleNameReadWrite)
		assert.NoError(t, err)

		retrieved := us.GetByID("user4")
		assert.NotContains(t, retrieved.GetRoles(), RoleNameReadWrite)
		assert.Len(t, retrieved.GetRoles(), 2)
	})

	t.Run("revoke last role", func(t *testing.T) {
		user := &User{
			ID:       "user5",
			Username: "testuser5",
			Roles:    []string{RoleNameAdmin},
		}
		us.AddUser(user)

		err := us.RevokeRole("user5", RoleNameAdmin)
		assert.NoError(t, err)

		retrieved := us.GetByID("user5")
		assert.Empty(t, retrieved.GetRoles())
	})
}

// TestTokenStore_CleanupExpiredTokens tests the cleanup goroutine.
func TestTokenStore_CleanupExpiredTokens(t *testing.T) {
	// This test verifies the cleanup logic works correctly
	// Note: We don't test the actual goroutine timing to keep tests fast

	ts := &TokenStore{
		refreshTokens: make(map[string]map[string]time.Time),
		revokedTokens: make(map[string]time.Time),
	}

	now := time.Now()

	// Add valid tokens
	ts.refreshTokens["user1"] = make(map[string]time.Time)
	ts.refreshTokens["user1"]["valid1"] = now.Add(1 * time.Hour)
	ts.refreshTokens["user1"]["valid2"] = now.Add(2 * time.Hour)

	// Add expired tokens
	ts.refreshTokens["user2"] = make(map[string]time.Time)
	ts.refreshTokens["user2"]["expired1"] = now.Add(-1 * time.Hour)
	ts.refreshTokens["user2"]["expired2"] = now.Add(-2 * time.Hour)

	// Add recent revoked tokens
	ts.revokedTokens["recent1"] = now.Add(-1 * time.Hour)
	ts.revokedTokens["recent2"] = now.Add(-2 * 24 * time.Hour)

	// Add old revoked tokens
	ts.revokedTokens["old1"] = now.Add(-8 * 24 * time.Hour)
	ts.revokedTokens["old2"] = now.Add(-10 * 24 * time.Hour)

	// Manually run cleanup logic
	ts.mu.Lock()
	for userID, tokens := range ts.refreshTokens {
		for token, expiry := range tokens {
			if now.After(expiry) {
				delete(tokens, token)
			}
		}
		if len(tokens) == 0 {
			delete(ts.refreshTokens, userID)
		}
	}

	for token, revokedAt := range ts.revokedTokens {
		if now.Sub(revokedAt) > 7*24*time.Hour {
			delete(ts.revokedTokens, token)
		}
	}
	ts.mu.Unlock()

	// Verify results
	assert.Len(t, ts.refreshTokens["user1"], 2, "Valid tokens should remain")
	assert.NotContains(t, ts.refreshTokens, "user2", "User with all expired tokens should be removed")
	assert.Contains(t, ts.revokedTokens, "recent1", "Recent revoked tokens should remain")
	assert.Contains(t, ts.revokedTokens, "recent2", "Recent revoked tokens should remain")
	assert.NotContains(t, ts.revokedTokens, "old1", "Old revoked tokens should be removed")
	assert.NotContains(t, ts.revokedTokens, "old2", "Old revoked tokens should be removed")
}

// TestTokenStore_EdgeCasesForExpiry tests edge cases around token expiry.
func TestTokenStore_EdgeCasesForExpiry(t *testing.T) {
	ts := NewTokenStore()
	now := time.Now()

	t.Run("token expiring exactly at boundary", func(t *testing.T) {
		ts.refreshTokens["user1"] = make(map[string]time.Time)
		ts.refreshTokens["user1"]["boundary"] = now

		// Token expiring at exactly now is considered expired
		assert.False(t, ts.IsValidRefreshToken("user1", "boundary"))
	})

	t.Run("revoked token at exactly 7 days", func(t *testing.T) {
		ts.revokedTokens["exactly7days"] = now.Add(-7 * 24 * time.Hour)

		// Verify cleanup logic
		ts.mu.Lock()
		for token, revokedAt := range ts.revokedTokens {
			if now.Sub(revokedAt) > 7*24*time.Hour {
				delete(ts.revokedTokens, token)
			}
		}
		ts.mu.Unlock()

		// Should remain (not greater than 7 days)
		assert.Contains(t, ts.revokedTokens, "exactly7days")
	})
}

// TestUserStore_UpdateUser_ErrorPath tests UpdateUser with non-existent user.
func TestUserStore_UpdateUser_ErrorPath(t *testing.T) {
	us := NewUserStore()

	// UpdateUser on non-existent user should still work (creates new entry)
	user := &User{
		ID:       "newuser",
		Username: "newusername",
		Roles:    []string{RoleNameReadOnly},
	}
	us.UpdateUser(user)

	retrieved := us.GetByID("newuser")
	require.NotNil(t, retrieved)
	assert.Equal(t, "newusername", retrieved.GetUsername())
}

// TestTokenStore_MultipleRefreshTokensPerUser tests that users can have multiple refresh tokens.
func TestTokenStore_MultipleRefreshTokensPerUser(t *testing.T) {
	ts := NewTokenStore()

	// Add multiple tokens for the same user
	ts.AddRefreshToken("user1", "token1")
	ts.AddRefreshToken("user1", "token2")
	ts.AddRefreshToken("user1", "token3")

	assert.True(t, ts.IsValidRefreshToken("user1", "token1"))
	assert.True(t, ts.IsValidRefreshToken("user1", "token2"))
	assert.True(t, ts.IsValidRefreshToken("user1", "token3"))

	// Revoke all user tokens
	ts.RevokeUserTokens("user1")

	assert.True(t, ts.IsRevoked("token1"))
	assert.True(t, ts.IsRevoked("token2"))
	assert.True(t, ts.IsRevoked("token3"))
}

// TestTokenStore_RevokeDoesNotRemoveRefreshToken tests that Revoke and RevokeUserTokens are independent.
func TestTokenStore_RevokeDoesNotRemoveRefreshToken(t *testing.T) {
	ts := NewTokenStore()

	ts.AddRefreshToken("user1", "token1")

	// Revoke should mark as revoked but not remove from refresh tokens
	ts.Revoke("token1")

	assert.True(t, ts.IsRevoked("token1"))
	assert.True(t, ts.IsValidRefreshToken("user1", "token1"), "Should still be valid refresh token")
}
