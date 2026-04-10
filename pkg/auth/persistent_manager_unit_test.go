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

// Package auth provides additional unit tests for persistent_manager.go
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPersistentRefreshTokenErrorPaths tests error paths in RefreshToken.
func TestPersistentRefreshTokenErrorPaths(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	t.Run("auth disabled", func(t *testing.T) {
		config := &AuthConfig{
			Enabled: false,
		}
		storage := newAuthMockStorage()
		pam, err := NewPersistentAuthManager(config, storage, log)
		require.NoError(t, err)
		defer pam.Close()

		_, err = pam.RefreshToken(ctx, "any-token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "authentication is not enabled")
	})

	t.Run("invalid refresh token", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
		}
		storage := newAuthMockStorage()
		pam, err := NewPersistentAuthManager(config, storage, log)
		require.NoError(t, err)
		defer pam.Close()

		_, err = pam.RefreshToken(ctx, "invalid.token.format")
		assert.Error(t, err)
	})

	t.Run("refresh token not in store", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:         true,
			JWTSecret:       "test-secret-key",
			TokenTTL:        1 * time.Hour,
			RefreshTokenTTL: 24 * time.Hour,
		}
		storage := newAuthMockStorage()
		pam, err := NewPersistentAuthManager(config, storage, log)
		require.NoError(t, err)
		defer pam.Close()

		// Create user and get token via generateTokens (password auth is disabled)
		user, err := pam.CreateUser("testuser", "", []string{RoleNameReadWrite})
		require.NoError(t, err)

		// Manually generate a valid JWT that's not in the token store
		token, err := pam.generateTokens(user)
		require.NoError(t, err)

		// Clear the token store to simulate token not being found
		pam.tokenStore.(*PersistentTokenStore).cache.mu.Lock()
		pam.tokenStore.(*PersistentTokenStore).cache.refreshTokens = make(map[string]map[string]time.Time)
		pam.tokenStore.(*PersistentTokenStore).cache.mu.Unlock()

		_, err = pam.RefreshToken(ctx, token.RefreshToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid refresh token")
	})

	t.Run("user not found", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:         true,
			JWTSecret:       "test-secret-key",
			TokenTTL:        1 * time.Hour,
			RefreshTokenTTL: 24 * time.Hour,
		}
		storage := newAuthMockStorage()
		pam, err := NewPersistentAuthManager(config, storage, log)
		require.NoError(t, err)
		defer pam.Close()

		// Create user and get token via generateTokens (password auth is disabled)
		user, err := pam.CreateUser("testuser", "", []string{RoleNameReadWrite})
		require.NoError(t, err)

		token, err := pam.generateTokens(user)
		require.NoError(t, err)

		// Delete the user
		err = pam.userStore.DeleteUser(user.ID)
		require.NoError(t, err)

		_, err = pam.RefreshToken(ctx, token.RefreshToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})
}

// TestPersistentRevokeTokenErrorPaths tests error paths in RevokeToken.
func TestPersistentRevokeTokenErrorPaths(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	t.Run("auth disabled", func(t *testing.T) {
		config := &AuthConfig{
			Enabled: false,
		}
		storage := newAuthMockStorage()
		pam, err := NewPersistentAuthManager(config, storage, log)
		require.NoError(t, err)
		defer pam.Close()

		err = pam.RevokeToken(ctx, "any-token")
		assert.NoError(t, err) // Should not error when disabled
	})
}

// TestPersistentDeleteUserErrorPaths tests error paths in DeleteUser.
func TestPersistentDeleteUserErrorPaths(t *testing.T) {
	log := createPersistentTestLogger(t)

	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	t.Run("delete non-existent user", func(t *testing.T) {
		err := pam.DeleteUser("nonexistent-user-id")
		assert.Error(t, err)
	})

	t.Run("delete user with tokens", func(t *testing.T) {
		// Create user and generate token (password auth is disabled)
		user, err := pam.CreateUser("user-with-tokens", "", []string{RoleNameReadWrite})
		require.NoError(t, err)

		// Generate tokens directly
		_, err = pam.generateTokens(user)
		require.NoError(t, err)

		// Delete user (should also revoke tokens)
		err = pam.DeleteUser(user.ID)
		assert.NoError(t, err)

		// Verify user is deleted
		deletedUser := pam.userStore.GetByID(user.ID)
		assert.Nil(t, deletedUser)
	})
}

// TestPersistentUpdateUserRolesErrorPaths tests error paths in UpdateUserRoles.
func TestPersistentUpdateUserRolesErrorPaths(t *testing.T) {
	log := createPersistentTestLogger(t)

	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	t.Run("invalid roles", func(t *testing.T) {
		// Create user
		user, err := pam.CreateUser("testuser", "", []string{RoleNameReadOnly})
		require.NoError(t, err)

		// Try to update with invalid roles
		err = pam.UpdateUserRoles(user.ID, []string{"nonexistent-role"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "one or more roles do not exist")
	})

	t.Run("user not found", func(t *testing.T) {
		err := pam.UpdateUserRoles("nonexistent-user-id", []string{RoleNameAdmin})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("successful update", func(t *testing.T) {
		// Create user
		user, err := pam.CreateUser("updateuser", "", []string{RoleNameReadOnly})
		require.NoError(t, err)

		// Update roles
		err = pam.UpdateUserRoles(user.ID, []string{RoleNameAdmin, RoleNameReadWrite})
		assert.NoError(t, err)

		// Verify update
		updatedUser := pam.userStore.GetByID(user.ID)
		assert.Len(t, updatedUser.Roles, 2)
		assert.Contains(t, updatedUser.Roles, RoleNameAdmin)
		assert.Contains(t, updatedUser.Roles, RoleNameReadWrite)
	})
}

// TestPersistentCloseWithNonPersistentTokenStore tests Close with non-persistent token store.
func TestPersistentCloseWithNonPersistentTokenStore(t *testing.T) {
	log := createPersistentTestLogger(t)

	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)

	// Replace with non-persistent token store
	pam.tokenStore = NewTokenStore()

	// Close should not panic
	err = pam.Close()
	assert.NoError(t, err)
}

// TestPersistentValidateCredentialsErrorPaths tests validateCredentials error paths.
// Note: Password-based authentication has been removed, so these tests now verify
// that the validateCredentials method returns errors as expected.
func TestPersistentValidateCredentialsErrorPaths(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	t.Run("validateCredentials is no longer supported", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
		}
		storage := newAuthMockStorage()
		pam, err := NewPersistentAuthManager(config, storage, log)
		require.NoError(t, err)
		defer pam.Close()

		// Create user (password is ignored)
		_, err = pam.CreateUser("testuser", "", []string{RoleNameReadWrite})
		require.NoError(t, err)

		// Password-based authentication is disabled
		// Authenticate should fail with password auth disabled error
		_, err = pam.Authenticate(ctx, "testuser", "password123")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "password-based authentication is disabled")
	})
}

// TestPersistentCreateUserErrorPaths tests CreateUser error paths.
func TestPersistentCreateUserErrorPaths(t *testing.T) {
	log := createPersistentTestLogger(t)

	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	t.Run("user storage error", func(t *testing.T) {
		// Create user successfully first
		user1, err := pam.CreateUser("user1", "", []string{RoleNameReadWrite})
		require.NoError(t, err)
		require.NotNil(t, user1)

		// Try to create with same username
		user2, err := pam.CreateUser("user1", "", []string{RoleNameReadOnly})
		assert.Error(t, err)
		assert.Nil(t, user2)
		assert.Contains(t, err.Error(), "user already exists")
	})
}

// =============================================================================
// Consolidated from persistent_coverage_test.go
// =============================================================================

// TestPersistentAuthManager_IssueTokenForUser_Success verifies IssueTokenForUser
// works end-to-end with a valid user that was created via CreateUser.
func TestPersistentAuthManager_IssueTokenForUser_Success(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-32chars-minimum!",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Create a user
	user, err := pam.CreateUser("issuetoken-user", "", []string{RoleNameReadWrite})
	require.NoError(t, err)

	// Issue a token for the user
	token, err := pam.IssueTokenForUser(ctx, user)
	require.NoError(t, err)
	assert.NotEmpty(t, token.AccessToken, "should issue an access token")
	assert.NotEmpty(t, token.RefreshToken, "should issue a refresh token")
}

// TestPersistentAuthManager_IssueTokenForUser_AuthDisabled verifies
// IssueTokenForUser returns an error when authentication is disabled.
func TestPersistentAuthManager_IssueTokenForUser_AuthDisabled(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled: false,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	user := &User{ID: "user1", Username: "test"}
	_, err = pam.IssueTokenForUser(ctx, user)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authentication is not enabled")
}

// TestPersistentAuthManager_IssueTokenForUser_NilUser verifies
// IssueTokenForUser returns an error when user is nil.
func TestPersistentAuthManager_IssueTokenForUser_NilUser(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-32chars-minimum!",
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	_, err = pam.IssueTokenForUser(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// TestPersistentAuthManager_ValidateToken_Success verifies ValidateToken
// returns correct validation information for a valid token.
func TestPersistentAuthManager_ValidateToken_Success(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-32chars-minimum!",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Create user and issue a token
	user, err := pam.CreateUser("validate-user", "", []string{RoleNameAdmin})
	require.NoError(t, err)

	token, err := pam.IssueTokenForUser(ctx, user)
	require.NoError(t, err)

	// Validate the access token
	result, err := pam.ValidateToken(ctx, token.AccessToken)
	require.NoError(t, err)
	assert.True(t, result.Valid, "token should be valid")
	assert.Equal(t, user.Username, result.Username)
	assert.False(t, result.IsRevoked)
}

// TestPersistentAuthManager_ValidateToken_AuthDisabled verifies ValidateToken
// returns an error when authentication is disabled.
func TestPersistentAuthManager_ValidateToken_AuthDisabled(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled: false,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	result, err := pam.ValidateToken(ctx, "any-token")
	assert.Error(t, err)
	assert.False(t, result.Valid)
}

// TestPersistentAuthManager_ValidateToken_EmptyToken verifies ValidateToken
// returns an error for an empty token string.
func TestPersistentAuthManager_ValidateToken_EmptyToken(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-32chars-minimum!",
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	result, err := pam.ValidateToken(ctx, "")
	assert.Error(t, err)
	assert.False(t, result.Valid)
	assert.Contains(t, err.Error(), "token")
}

// TestPersistentAuthManager_ValidateToken_RevokedToken verifies that a
// revoked token is flagged as invalid in validation results.
func TestPersistentAuthManager_ValidateToken_RevokedToken(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key-32chars-minimum!",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Create user and issue a token
	user, err := pam.CreateUser("revoke-user", "", []string{RoleNameReadOnly})
	require.NoError(t, err)

	token, err := pam.IssueTokenForUser(ctx, user)
	require.NoError(t, err)

	// Revoke the token
	err = pam.RevokeToken(ctx, token.AccessToken)
	require.NoError(t, err)

	// Validate the revoked token
	result, err := pam.ValidateToken(ctx, token.AccessToken)
	require.NoError(t, err)
	assert.False(t, result.Valid, "revoked token should not be valid")
	assert.True(t, result.IsRevoked, "revoked flag should be set")
}

// TestPersistentAuthManager_ValidateCredentials_EmptyUsername verifies
// validateCredentials returns an error for empty username.
func TestPersistentAuthManager_ValidateCredentials_EmptyUsername(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key-32chars-minimum!",
		AllowPasswordAuth: true,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	_, err = pam.validateCredentials(ctx, "", "password")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "username and password are required")
}

// TestPersistentAuthManager_ValidateCredentials_EmptyPassword verifies
// validateCredentials returns an error for empty password.
func TestPersistentAuthManager_ValidateCredentials_EmptyPassword(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key-32chars-minimum!",
		AllowPasswordAuth: true,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	_, err = pam.validateCredentials(ctx, "user", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "username and password are required")
}

// TestPersistentAuthManager_ValidateCredentials_UserNotFound verifies
// validateCredentials returns an error when the user does not exist.
func TestPersistentAuthManager_ValidateCredentials_UserNotFound(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key-32chars-minimum!",
		AllowPasswordAuth: true,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	_, err = pam.validateCredentials(ctx, "nonexistent", "password")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
}

// TestPersistentAuthManager_ValidateCredentials_NoPasswordSet verifies
// validateCredentials returns an error when the user has no password set.
func TestPersistentAuthManager_ValidateCredentials_NoPasswordSet(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key-32chars-minimum!",
		AllowPasswordAuth: true,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Create user without password
	_, err = pam.CreateUser("nopassword-user", "", []string{RoleNameReadOnly})
	require.NoError(t, err)

	_, err = pam.validateCredentials(ctx, "nopassword-user", "somepassword")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no password set")
}

// TestPersistentAuthManager_ValidateCredentials_WrongPassword verifies
// validateCredentials returns an error for incorrect password.
func TestPersistentAuthManager_ValidateCredentials_WrongPassword(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key-32chars-minimum!",
		AllowPasswordAuth: true,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Create user with password
	_, err = pam.CreateUser("passworduser", "correct-password", []string{RoleNameReadOnly})
	require.NoError(t, err)

	_, err = pam.validateCredentials(ctx, "passworduser", "wrong-password")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid credentials")
}

// TestPersistentAuthManager_Authenticate_FullFlow verifies the full
// authentication flow when password auth is enabled.
func TestPersistentAuthManager_Authenticate_FullFlow(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key-32chars-minimum!",
		TokenTTL:          1 * time.Hour,
		RefreshTokenTTL:   24 * time.Hour,
		AllowPasswordAuth: true,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Create user with password
	_, err = pam.CreateUser("authuser", "secure-password-123", []string{RoleNameReadWrite})
	require.NoError(t, err)

	// Authenticate
	token, err := pam.Authenticate(ctx, "authuser", "secure-password-123")
	require.NoError(t, err)
	assert.NotEmpty(t, token.AccessToken)
	assert.NotEmpty(t, token.RefreshToken)
}

// TestPersistentAuthManager_Authenticate_AuthDisabled verifies Authenticate
// returns an error when authentication is disabled.
func TestPersistentAuthManager_Authenticate_AuthDisabled(t *testing.T) {
	log := createPersistentTestLogger(t)
	ctx := context.Background()

	config := &AuthConfig{
		Enabled: false,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	_, err = pam.Authenticate(ctx, "user", "pass")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authentication is not enabled")
}

// TestPersistentAuthManager_CreateUser_WithPasswordAuth verifies CreateUser
// hashes the password when AllowPasswordAuth is enabled.
func TestPersistentAuthManager_CreateUser_WithPasswordAuth(t *testing.T) {
	log := createPersistentTestLogger(t)

	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key-32chars-minimum!",
		AllowPasswordAuth: true,
	}
	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	user, err := pam.CreateUser("pwduser", "mypassword!!", []string{RoleNameReadOnly})
	require.NoError(t, err)
	assert.NotEmpty(t, user.password, "password hash should be stored")
	assert.NotEqual(t, "mypassword!!", user.password, "password should be hashed")
}

// TestUserStore_GetByUsername_InconsistentState verifies GetByUsername handles
// the edge case where usernameMap has a mapping but the user map does not.
func TestUserStore_GetByUsername_InconsistentState(t *testing.T) {
	us := NewUserStore()

	// Manually create an inconsistent state: usernameMap has entry but users map does not
	us.mu.Lock()
	us.usernameMap["ghost-user"] = "nonexistent-id"
	us.mu.Unlock()

	result := us.GetByUsername("ghost-user")
	assert.Nil(t, result, "should return nil for inconsistent state")
}
