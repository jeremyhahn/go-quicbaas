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

// Package auth provides additional unit tests for manager.go
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateToken tests the public ValidateToken method.
func TestValidateToken(t *testing.T) {
	log := createTestLogger(t)

	t.Run("auth disabled", func(t *testing.T) {
		config := &AuthConfig{
			Enabled: false,
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		ctx := context.Background()
		result, err := am.ValidateToken(ctx, "any-token")

		assert.Error(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.Valid)
		assert.Contains(t, err.Error(), "authentication is not enabled")
	})

	t.Run("empty token", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		ctx := context.Background()
		result, err := am.ValidateToken(ctx, "")

		assert.Error(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.Valid)
		assert.Contains(t, err.Error(), "token is required")
	})

	t.Run("valid token", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
			TokenTTL:  1 * time.Hour,
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		// Create user and get token via generateTokens (password auth is disabled)
		user, err := am.CreateUser("testuser", []string{RoleNameReadWrite})
		require.NoError(t, err)

		ctx := context.Background()
		token, err := am.generateTokens(user)
		require.NoError(t, err)

		result, err := am.ValidateToken(ctx, token.AccessToken)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.True(t, result.Valid)
		assert.Equal(t, "testuser", result.Username)
		assert.Contains(t, result.Roles, RoleNameReadWrite)
		assert.False(t, result.IsRevoked)
		assert.True(t, result.ExpiresAt.After(time.Now()))
	})

	t.Run("invalid token", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		ctx := context.Background()
		result, err := am.ValidateToken(ctx, "invalid.token.here")

		assert.Error(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.Valid)
	})

	t.Run("revoked token", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
			TokenTTL:  1 * time.Hour,
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		// Create user and get token via generateTokens (password auth is disabled)
		user, err := am.CreateUser("testuser", []string{RoleNameReadWrite})
		require.NoError(t, err)

		ctx := context.Background()
		token, err := am.generateTokens(user)
		require.NoError(t, err)

		// Revoke the token
		err = am.RevokeToken(ctx, token.AccessToken)
		require.NoError(t, err)

		result, err := am.ValidateToken(ctx, token.AccessToken)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.Valid)
		assert.True(t, result.IsRevoked)
	})
}

// TestGetUserStore tests the GetUserStore method.
func TestGetUserStore(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	userStore := am.GetUserStore()
	assert.NotNil(t, userStore)

	// Verify it's the actual user store by adding a user
	user := &User{
		ID:       "test-id",
		Username: "testuser",
		Roles:    []string{RoleNameReadOnly},
	}
	userStore.AddUser(user)

	retrieved := userStore.GetByID("test-id")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "testuser", retrieved.GetUsername())
}

// TestRefreshTokenErrorPaths tests error paths in RefreshToken.
func TestRefreshTokenErrorPaths(t *testing.T) {
	log := createTestLogger(t)
	ctx := context.Background()

	t.Run("auth disabled", func(t *testing.T) {
		config := &AuthConfig{
			Enabled: false,
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		_, err = am.RefreshToken(ctx, "any-token")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "authentication is not enabled")
	})

	t.Run("invalid refresh token", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:   true,
			JWTSecret: "test-secret-key",
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		_, err = am.RefreshToken(ctx, "invalid.token.format")
		assert.Error(t, err)
	})

	t.Run("refresh token not in store", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:         true,
			JWTSecret:       "test-secret-key",
			TokenTTL:        1 * time.Hour,
			RefreshTokenTTL: 24 * time.Hour,
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		// Create user and get token via generateTokens (password auth is disabled)
		user, err := am.CreateUser("testuser", []string{RoleNameReadWrite})
		require.NoError(t, err)

		// Manually generate a valid JWT that's not in the token store
		token, err := am.generateTokens(user)
		require.NoError(t, err)

		// Remove from token store
		am.tokenStore = NewTokenStore()

		_, err = am.RefreshToken(ctx, token.RefreshToken)
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
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		// Create user and get token via generateTokens (password auth is disabled)
		user, err := am.CreateUser("testuser", []string{RoleNameReadWrite})
		require.NoError(t, err)

		token, err := am.generateTokens(user)
		require.NoError(t, err)

		// Delete the user
		am.userStore.DeleteUser(user.ID)

		_, err = am.RefreshToken(ctx, token.RefreshToken)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})
}

// TestRevokeTokenErrorPaths tests error paths in RevokeToken.
func TestRevokeTokenErrorPaths(t *testing.T) {
	log := createTestLogger(t)
	ctx := context.Background()

	t.Run("auth disabled", func(t *testing.T) {
		config := &AuthConfig{
			Enabled: false,
		}
		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		err = am.RevokeToken(ctx, "any-token")
		assert.NoError(t, err) // Should not error when disabled
	})
}

// TestValidateTokenInternalErrorPaths tests internal validateToken error paths.
func TestValidateTokenInternalErrorPaths(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  1 * time.Hour,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create user and get token via generateTokens (password auth is disabled)
	user, err := am.CreateUser("testuser", []string{RoleNameReadWrite})
	require.NoError(t, err)

	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Now change the JWT secret to make the token invalid
	am.config.JWTSecret = "different-secret"

	_, err = am.validateToken(token.AccessToken)
	assert.Error(t, err)
}

// TestGenerateTokensErrorPaths tests error paths in generateTokens.
func TestGenerateTokensErrorPaths(t *testing.T) {
	log := createTestLogger(t)

	t.Run("valid token generation", func(t *testing.T) {
		config := &AuthConfig{
			Enabled:         true,
			JWTSecret:       "test-secret-key",
			TokenTTL:        1 * time.Hour,
			RefreshTokenTTL: 24 * time.Hour,
		}

		am, err := NewInMemoryAuthManager(config, log)
		require.NoError(t, err)

		user := &User{
			ID:       "test-id",
			Username: "testuser",
			Roles:    []string{RoleNameReadWrite},
		}

		token, err := am.generateTokens(user)
		assert.NoError(t, err)
		assert.NotNil(t, token)
		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)
		assert.Equal(t, "Bearer", token.TokenType)
		assert.True(t, token.ExpiresAt.After(time.Now()))
	})
}
