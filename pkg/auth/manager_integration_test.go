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

//go:build integration
// +build integration

// Package auth provides integration tests for authentication manager
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRefreshToken(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key",
		TokenTTL:        1 * time.Second, // Short TTL for testing
		RefreshTokenTTL: 1 * time.Hour,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create user (password is ignored - use IAM for auth)
	user, err := am.CreateUser("testuser", []string{RoleNameReadWrite})
	require.NoError(t, err)

	ctx := context.Background()

	// Generate token directly (password auth is disabled)
	initialToken, err := am.generateTokens(user)
	require.NoError(t, err)

	// Wait for access token to expire
	time.Sleep(2 * time.Second)

	// Refresh the token
	newToken, err := am.RefreshToken(ctx, initialToken.RefreshToken)
	require.NoError(t, err)
	require.NotNil(t, newToken)
	assert.NotEmpty(t, newToken.AccessToken)
	assert.NotEqual(t, initialToken.AccessToken, newToken.AccessToken)

	// Verify new token works
	err = am.Authorize(ctx, newToken.AccessToken, ResourceKeys, ActionRead)
	assert.NoError(t, err)

	// Test invalid refresh token
	_, err = am.RefreshToken(ctx, "invalid-refresh-token")
	assert.Error(t, err)
}

func TestTokenValidation(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  1 * time.Second, // Short TTL for expiration testing
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create user (password is ignored - use IAM for auth)
	user, err := am.CreateUser("testuser", []string{RoleNameReadWrite})
	require.NoError(t, err)

	ctx := context.Background()

	// Generate token directly (password auth is disabled)
	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Test valid token
	claims, err := am.validateToken(token.AccessToken)
	assert.NoError(t, err)
	assert.Equal(t, "testuser", claims.Username)

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	// Test expired token
	_, err = am.validateToken(token.AccessToken)
	assert.Error(t, err)

	// Test invalid token
	_, err = am.validateToken("invalid.token.here")
	assert.Error(t, err)
}
