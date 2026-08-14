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

// Package auth provides integration tests for persistent authentication manager
package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPersistentRefreshToken tests the RefreshToken method with time.Sleep
func TestPersistentRefreshToken(t *testing.T) {
	log := createPersistentTestLogger(t)
	config := newTestAuthConfig(t)
	config.TokenTTL = 1 * time.Second
	config.RefreshTokenTTL = 1 * time.Hour

	storage := newAuthMockStorage()
	pam, err := NewPersistentAuthManager(config, storage, log)
	require.NoError(t, err)
	defer pam.Close()

	// Create user (password is ignored - use IAM for auth)
	user, err := pam.CreateUser("testuser", []string{"readwrite"})
	require.NoError(t, err)

	ctx := context.Background()

	// Generate token directly (password auth is disabled)
	initialToken, err := pam.generateTokens(user)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	newToken, err := pam.RefreshToken(ctx, initialToken.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, newToken.AccessToken)
}
