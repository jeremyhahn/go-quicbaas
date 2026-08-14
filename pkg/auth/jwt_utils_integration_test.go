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

// Package auth provides integration tests for JWT token utilities
package auth

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateTokensForUser_ConsecutiveCalls tests multiple token generations
func TestGenerateTokensForUser_ConsecutiveCalls(t *testing.T) {
	config := newTestAuthConfig(t)
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-consecutive",
		Username: "consecutive",
		Roles:    []string{"admin"},
	}

	// Generate multiple tokens with small delays to ensure different timestamps
	tokens := make([]*Token, 5)
	for i := 0; i < 5; i++ {
		token, err := generateTokensForUser(user, config, tokenStore)
		require.NoError(t, err)
		tokens[i] = token
		// Small delay to ensure different issued-at times
		time.Sleep(10 * time.Millisecond)
	}

	// Verify at least some tokens are different (they may be same if generated in same second)
	differentCount := 0
	for i := 0; i < 5; i++ {
		for j := i + 1; j < 5; j++ {
			if tokens[i].AccessToken != tokens[j].AccessToken {
				differentCount++
			}
		}
	}
	assert.Greater(t, differentCount, 0, "Expected at least some tokens to be different")

	// All tokens should be valid
	for _, token := range tokens {
		claims, err := validateJWTToken(token.AccessToken, config)
		assert.NoError(t, err)
		assert.Equal(t, user.ID, claims.UserID)
	}
}

// TestConcurrentTokenGeneration tests concurrent token generation
func TestConcurrentTokenGeneration(t *testing.T) {
	config := newTestAuthConfig(t)
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-concurrent",
		Username: "concurrent",
		Roles:    []string{"admin"},
	}

	// Generate tokens concurrently
	numGoroutines := 10
	results := make(chan *Token, numGoroutines)
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			// Small delay to spread out timestamps
			time.Sleep(time.Duration(index) * time.Millisecond)
			token, err := generateTokensForUser(user, config, tokenStore)
			if err != nil {
				errors <- err
				return
			}
			results <- token
		}(i)
	}

	// Collect results
	tokens := make([]*Token, 0, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		select {
		case token := <-results:
			tokens = append(tokens, token)
		case err := <-errors:
			t.Fatal(err)
		}
	}

	// All tokens should be valid
	assert.Equal(t, numGoroutines, len(tokens))
	for i := 0; i < len(tokens); i++ {
		// Validate token
		claims, err := validateJWTToken(tokens[i].AccessToken, config)
		assert.NoError(t, err)
		assert.Equal(t, user.ID, claims.UserID)
	}

	// Verify at least some tokens are unique (may have duplicates if generated at exact same millisecond)
	uniqueTokens := make(map[string]bool)
	for _, token := range tokens {
		uniqueTokens[token.AccessToken] = true
	}
	assert.Greater(t, len(uniqueTokens), 1, "Expected at least some unique tokens from concurrent generation")
}

// TestVeryShortTTL tests token generation and validation with very short TTL
func TestVeryShortTTL(t *testing.T) {
	config := newTestAuthConfig(t)
	// JWT exp claims are truncated to whole seconds (jwt.TimePrecision), so the
	// TTL must exceed one second or the token can be expired at issue time.
	config.TokenTTL = 1500 * time.Millisecond
	config.RefreshTokenTTL = 2 * time.Second
	tokenStore := NewMockTokenStore()

	user := &User{
		ID:       "user-short-ttl",
		Username: "shortttl",
		Roles:    []string{"admin"},
	}

	// Generate token
	token, err := generateTokensForUser(user, config, tokenStore)
	require.NoError(t, err)

	// Validate immediately (should work)
	claims, err := validateJWTToken(token.AccessToken, config)
	require.NoError(t, err, "Token should be valid immediately after creation")
	assert.NotNil(t, claims)

	// Wait for token to expire
	time.Sleep(1700 * time.Millisecond)

	// Validate again (should fail)
	claims, err = validateJWTToken(token.AccessToken, config)
	assert.Error(t, err, "Token should be expired after TTL")
	assert.Nil(t, claims)
}

// TestGenerateTokensWithDifferentKeys ensures different signing keys produce
// different tokens and each token only validates with its own key.
func TestGenerateTokensWithDifferentKeys(t *testing.T) {
	user := &User{
		ID:       "user-diff-keys",
		Username: "diffkeys",
		Roles:    []string{"admin"},
	}

	numKeys := 3
	configs := make([]*AuthConfig, numKeys)
	tokens := make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		configs[i] = newTestAuthConfig(t)
		tokenStore := NewMockTokenStore()

		token, err := generateTokensForUser(user, configs[i], tokenStore)
		require.NoError(t, err)
		tokens[i] = token.AccessToken
	}

	// All tokens should be different
	for i := 0; i < len(tokens); i++ {
		for j := i + 1; j < len(tokens); j++ {
			assert.NotEqual(t, tokens[i], tokens[j],
				fmt.Sprintf("Tokens signed with different keys should be different: %d vs %d", i, j))
		}
	}

	// Each token should only validate with its corresponding key
	for i := range configs {
		claims, err := validateJWTToken(tokens[i], configs[i])
		assert.NoError(t, err)
		assert.Equal(t, user.ID, claims.UserID)

		// Should fail with other keys
		for j := range configs {
			if i != j {
				_, err := validateJWTToken(tokens[i], configs[j])
				assert.Error(t, err, fmt.Sprintf("Token %d should not validate with key %d", i, j))
			}
		}
	}
}
