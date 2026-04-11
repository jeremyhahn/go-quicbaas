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

// Package auth provides tests for internal key schema
package auth

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeyConstants(t *testing.T) {
	// Verify all keys have the correct prefix
	assert.True(t, strings.HasPrefix(KeyPrefixUsers, KeyPrefixInternal))
	assert.True(t, strings.HasPrefix(KeyPrefixUsersByName, KeyPrefixInternal))
	assert.True(t, strings.HasPrefix(KeyPrefixRoles, KeyPrefixInternal))
	assert.True(t, strings.HasPrefix(KeyPrefixRefreshTokens, KeyPrefixInternal))
	assert.True(t, strings.HasPrefix(KeyPrefixRevokedTokens, KeyPrefixInternal))
	assert.True(t, strings.HasPrefix(KeyBootstrapComplete, KeyPrefixInternal))
	assert.True(t, strings.HasPrefix(KeyAdminUser, KeyPrefixInternal))

	// Verify the internal prefix
	assert.Equal(t, "__quicbaas:", KeyPrefixInternal)
}

func TestNewKeyBuilder(t *testing.T) {
	kb := NewKeyBuilder()
	assert.NotNil(t, kb)
}

func TestKeyBuilderUserKey(t *testing.T) {
	kb := NewKeyBuilder()

	tests := []struct {
		name     string
		userID   string
		expected string
	}{
		{
			name:     "simple user ID",
			userID:   "user123",
			expected: "__quicbaas:users:user123",
		},
		{
			name:     "UUID user ID",
			userID:   "550e8400-e29b-41d4-a716-446655440000",
			expected: "__quicbaas:users:550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:     "empty user ID",
			userID:   "",
			expected: "__quicbaas:users:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := kb.UserKey(tt.userID)
			assert.Equal(t, tt.expected, string(key))
		})
	}
}

func TestKeyBuilderUserByNameKey(t *testing.T) {
	kb := NewKeyBuilder()

	tests := []struct {
		name     string
		username string
		expected string
	}{
		{
			name:     "simple username",
			username: "alice",
			expected: "__quicbaas:users:byname:alice",
		},
		{
			name:     "email username",
			username: "alice@example.com",
			expected: "__quicbaas:users:byname:alice@example.com",
		},
		{
			name:     "empty username",
			username: "",
			expected: "__quicbaas:users:byname:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := kb.UserByNameKey(tt.username)
			assert.Equal(t, tt.expected, string(key))
		})
	}
}

func TestKeyBuilderUserMetadataKey(t *testing.T) {
	kb := NewKeyBuilder()

	tests := []struct {
		name     string
		userID   string
		expected string
	}{
		{
			name:     "simple user ID",
			userID:   "user123",
			expected: "__quicbaas:users:meta:user123",
		},
		{
			name:     "empty user ID",
			userID:   "",
			expected: "__quicbaas:users:meta:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := kb.UserMetadataKey(tt.userID)
			assert.Equal(t, tt.expected, string(key))
		})
	}
}

func TestKeyBuilderRoleKey(t *testing.T) {
	kb := NewKeyBuilder()

	tests := []struct {
		name     string
		roleName string
		expected string
	}{
		{
			name:     "admin role",
			roleName: "admin",
			expected: "__quicbaas:roles:admin",
		},
		{
			name:     "readonly role",
			roleName: "readonly",
			expected: "__quicbaas:roles:readonly",
		},
		{
			name:     "empty role",
			roleName: "",
			expected: "__quicbaas:roles:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := kb.RoleKey(tt.roleName)
			assert.Equal(t, tt.expected, string(key))
		})
	}
}

func TestKeyBuilderRoleMetadataKey(t *testing.T) {
	kb := NewKeyBuilder()

	tests := []struct {
		name     string
		roleName string
		expected string
	}{
		{
			name:     "admin role metadata",
			roleName: "admin",
			expected: "__quicbaas:roles:meta:admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := kb.RoleMetadataKey(tt.roleName)
			assert.Equal(t, tt.expected, string(key))
		})
	}
}

func TestKeyBuilderRefreshTokenKey(t *testing.T) {
	kb := NewKeyBuilder()

	tests := []struct {
		name     string
		userID   string
		tokenID  string
		expected string
	}{
		{
			name:     "simple token",
			userID:   "user123",
			tokenID:  "token456",
			expected: "__quicbaas:tokens:refresh:user123:token456",
		},
		{
			name:     "empty token ID",
			userID:   "user123",
			tokenID:  "",
			expected: "__quicbaas:tokens:refresh:user123:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := kb.RefreshTokenKey(tt.userID, tt.tokenID)
			assert.Equal(t, tt.expected, string(key))
		})
	}
}

func TestKeyBuilderRevokedTokenKey(t *testing.T) {
	kb := NewKeyBuilder()

	tests := []struct {
		name     string
		tokenID  string
		expected string
	}{
		{
			name:     "simple token",
			tokenID:  "token123",
			expected: "__quicbaas:tokens:revoked:token123",
		},
		{
			name:     "JWT token",
			tokenID:  "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
			expected: "__quicbaas:tokens:revoked:eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := kb.RevokedTokenKey(tt.tokenID)
			assert.Equal(t, tt.expected, string(key))
		})
	}
}

func TestKeyBuilderBootstrapCompleteKey(t *testing.T) {
	kb := NewKeyBuilder()
	key := kb.BootstrapCompleteKey()
	assert.Equal(t, "__quicbaas:bootstrap:complete", string(key))
}

func TestKeyBuilderAdminUserKey(t *testing.T) {
	kb := NewKeyBuilder()
	key := kb.AdminUserKey()
	assert.Equal(t, "__quicbaas:bootstrap:admin", string(key))
}

func TestIsInternalKey(t *testing.T) {
	tests := []struct {
		name     string
		key      []byte
		expected bool
	}{
		{
			name:     "user key is internal",
			key:      []byte("__quicbaas:users:user123"),
			expected: true,
		},
		{
			name:     "role key is internal",
			key:      []byte("__quicbaas:roles:admin"),
			expected: true,
		},
		{
			name:     "regular key is not internal",
			key:      []byte("mykey"),
			expected: false,
		},
		{
			name:     "empty key is not internal",
			key:      []byte(""),
			expected: false,
		},
		{
			name:     "partial prefix is not internal",
			key:      []byte("__qrdb"),
			expected: false,
		},
		{
			name:     "similar prefix is not internal",
			key:      []byte("__quicbaas"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsInternalKey(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateUserKey(t *testing.T) {
	tests := []struct {
		name        string
		key         []byte
		expectError bool
	}{
		{
			name:        "valid user key (no reserved prefix)",
			key:         []byte("myapp:users:user123"),
			expectError: false,
		},
		{
			name:        "valid simple key",
			key:         []byte("mykey"),
			expectError: false,
		},
		{
			name:        "invalid - uses internal prefix (user key)",
			key:         []byte("__quicbaas:users:user123"),
			expectError: true,
		},
		{
			name:        "invalid - uses internal prefix (role key)",
			key:         []byte("__quicbaas:roles:admin"),
			expectError: true,
		},
		{
			name:        "valid - empty key is allowed",
			key:         []byte(""),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUserKey(tt.key)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKeyPrefixConsistency(t *testing.T) {
	kb := NewKeyBuilder()

	// Verify that keys built with KeyBuilder match the constants
	userKey := string(kb.UserKey("test"))
	assert.True(t, strings.HasPrefix(userKey, KeyPrefixUsers))

	roleKey := string(kb.RoleKey("test"))
	assert.True(t, strings.HasPrefix(roleKey, KeyPrefixRoles))

	refreshTokenKey := string(kb.RefreshTokenKey("user", "token"))
	assert.True(t, strings.HasPrefix(refreshTokenKey, KeyPrefixRefreshTokens))

	revokedTokenKey := string(kb.RevokedTokenKey("token"))
	assert.True(t, strings.HasPrefix(revokedTokenKey, KeyPrefixRevokedTokens))
}

func TestKeyUniqueness(t *testing.T) {
	kb := NewKeyBuilder()

	// Verify that different keys are unique
	user1 := string(kb.UserKey("user1"))
	user2 := string(kb.UserKey("user2"))
	assert.NotEqual(t, user1, user2)

	role1 := string(kb.RoleKey("role1"))
	role2 := string(kb.RoleKey("role2"))
	assert.NotEqual(t, role1, role2)

	// Verify that different key types are unique even with same ID
	userKey := string(kb.UserKey("test"))
	roleKey := string(kb.RoleKey("test"))
	assert.NotEqual(t, userKey, roleKey)
}
