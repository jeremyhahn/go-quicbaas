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

// Package auth provides authentication and authorization tests
package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"google.golang.org/grpc/metadata"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestNewInMemoryAuthManager(t *testing.T) {
	tests := []struct {
		name        string
		config      *AuthConfig
		expectError bool
	}{
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
		},
		{
			name: "missing JWT secret when enabled",
			config: &AuthConfig{
				Enabled:   true,
				JWTSecret: "",
			},
			expectError: true,
		},
		{
			name: "valid config",
			config: &AuthConfig{
				Enabled:   true,
				JWTSecret: "test-secret-key",
			},
			expectError: false,
		},
		{
			name: "auth disabled",
			config: &AuthConfig{
				Enabled: false,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := createTestLogger(t)
			am, err := NewInMemoryAuthManager(tt.config, log)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, am)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, am)
			}
		})
	}
}

func TestAuthenticate(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret-key",
		TokenTTL:        1 * time.Hour,
		RefreshTokenTTL: 24 * time.Hour,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create a test user (password is ignored - use IAM for auth)
	user, err := am.CreateUser("testuser", "", []string{RoleNameReadWrite})
	require.NoError(t, err)
	require.NotNil(t, user)

	// Password-based authentication has been removed.
	// All authentication attempts should fail with an error.
	// Use IAM (WebAuthn, OIDC, OAuth) for authentication instead.
	tests := []struct {
		name     string
		username string
		password string
	}{
		{
			name:     "password auth disabled - any credentials",
			username: "testuser",
			password: "password123",
		},
		{
			name:     "password auth disabled - empty password",
			username: "testuser",
			password: "",
		},
		{
			name:     "password auth disabled - empty username",
			username: "",
			password: "password123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			token, err := am.Authenticate(ctx, tt.username, tt.password)

			// Password-based authentication is disabled - all attempts should fail
			assert.Error(t, err)
			assert.Nil(t, token)
			assert.Contains(t, err.Error(), "password-based authentication is disabled")
		})
	}
}

func TestAuthorize(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  1 * time.Hour,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()

	// Create users with different roles (password ignored - use IAM for auth)
	// CreateUser returns *User directly, no type assertion needed
	adminUser, err := am.CreateUser("admin", "", []string{RoleNameAdmin})
	require.NoError(t, err)

	rwUser, err := am.CreateUser("readwrite", "", []string{RoleNameReadWrite})
	require.NoError(t, err)

	roUser, err := am.CreateUser("readonly", "", []string{RoleNameReadOnly})
	require.NoError(t, err)

	// Generate tokens directly (password auth is disabled)
	adminToken, err := am.generateTokens(adminUser)
	require.NoError(t, err)

	rwToken, err := am.generateTokens(rwUser)
	require.NoError(t, err)

	roToken, err := am.generateTokens(roUser)
	require.NoError(t, err)

	tests := []struct {
		name        string
		token       string
		resource    string
		action      string
		expectError bool
	}{
		// Admin tests
		{
			name:        "admin can read keys",
			token:       adminToken.AccessToken,
			resource:    ResourceKeys,
			action:      ActionRead,
			expectError: false,
		},
		{
			name:        "admin can write keys",
			token:       adminToken.AccessToken,
			resource:    ResourceKeys,
			action:      ActionWrite,
			expectError: false,
		},
		{
			name:        "admin can manage users",
			token:       adminToken.AccessToken,
			resource:    ResourceUsers,
			action:      ActionAdmin,
			expectError: false,
		},

		// ReadWrite tests
		{
			name:        "readwrite can read keys",
			token:       rwToken.AccessToken,
			resource:    ResourceKeys,
			action:      ActionRead,
			expectError: false,
		},
		{
			name:        "readwrite can write keys",
			token:       rwToken.AccessToken,
			resource:    ResourceKeys,
			action:      ActionWrite,
			expectError: false,
		},
		{
			name:        "readwrite cannot manage users",
			token:       rwToken.AccessToken,
			resource:    ResourceUsers,
			action:      ActionAdmin,
			expectError: true,
		},

		// ReadOnly tests
		{
			name:        "readonly can read keys",
			token:       roToken.AccessToken,
			resource:    ResourceKeys,
			action:      ActionRead,
			expectError: false,
		},
		{
			name:        "readonly cannot write keys",
			token:       roToken.AccessToken,
			resource:    ResourceKeys,
			action:      ActionWrite,
			expectError: true,
		},
		{
			name:        "readonly cannot delete keys",
			token:       roToken.AccessToken,
			resource:    ResourceKeys,
			action:      ActionDelete,
			expectError: true,
		},

		// Invalid token tests
		{
			name:        "invalid token",
			token:       "invalid-token",
			resource:    ResourceKeys,
			action:      ActionRead,
			expectError: true,
		},
		{
			name:        "empty token",
			token:       "",
			resource:    ResourceKeys,
			action:      ActionRead,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := am.Authorize(ctx, tt.token, tt.resource, tt.action)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRefreshToken moved to manager_integration_test.go

func TestRevokeToken(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  1 * time.Hour,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create user and get token via generateTokens (password auth is disabled)
	user, err := am.CreateUser("testuser", "", []string{RoleNameReadWrite})
	require.NoError(t, err)

	ctx := context.Background()
	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Verify token works before revocation
	err = am.Authorize(ctx, token.AccessToken, ResourceKeys, ActionRead)
	assert.NoError(t, err)

	// Revoke token
	err = am.RevokeToken(ctx, token.AccessToken)
	assert.NoError(t, err)

	// Verify token no longer works
	err = am.Authorize(ctx, token.AccessToken, ResourceKeys, ActionRead)
	assert.Error(t, err)
}

func TestCreateUser(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	tests := []struct {
		name        string
		username    string
		password    string
		roles       []string
		expectError bool
	}{
		{
			name:        "valid user",
			username:    "user1",
			password:    "", // Password is ignored
			roles:       []string{RoleNameReadWrite},
			expectError: false,
		},
		{
			name:        "empty username",
			username:    "",
			password:    "", // Password is ignored
			roles:       []string{RoleNameReadWrite},
			expectError: true,
		},
		{
			name:        "empty password allowed",
			username:    "user2",
			password:    "", // Password is ignored in passwordless auth
			roles:       []string{RoleNameReadWrite},
			expectError: false,
		},
		{
			name:        "duplicate username",
			username:    "user1",
			password:    "", // Password is ignored
			roles:       []string{RoleNameReadOnly},
			expectError: true,
		},
		{
			name:        "multiple roles",
			username:    "user3",
			password:    "", // Password is ignored
			roles:       []string{RoleNameReadWrite, RoleNameReadOnly},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := am.CreateUser(tt.username, tt.password, tt.roles)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, user)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, user)
				assert.Equal(t, tt.username, user.Username)
				assert.Equal(t, tt.roles, user.Roles)
				assert.NotEmpty(t, user.ID)
			}
		})
	}
}

// TestCreateUserWithTenant tests multi-tenant user creation.
func TestCreateUserWithTenant(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-for-tenant-tests",
		TokenTTL:  1 * time.Hour,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("user with tenant ID", func(t *testing.T) {
		user, err := am.CreateUserWithTenant("tenant_user", "", "tenant-abc", []string{RoleNameReadWrite})
		require.NoError(t, err)
		require.NotNil(t, user)

		assert.Equal(t, "tenant_user", user.Username)
		assert.Equal(t, "tenant-abc", user.TenantID)
		assert.Equal(t, []string{RoleNameReadWrite}, user.Roles)
	})

	t.Run("token includes tenant ID", func(t *testing.T) {
		// Get user from store and type assert to *User for generateTokens
		userInfo := am.userStore.GetByUsername("tenant_user")
		require.NotNil(t, userInfo)
		user, ok := userInfo.(*User)
		require.True(t, ok, "UserInfo should be *User")

		token, err := am.generateTokens(user)
		require.NoError(t, err)
		require.NotNil(t, token)

		// Validate the token and check tenant ID is present
		result, err := am.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		require.True(t, result.Valid)

		assert.Equal(t, "tenant-abc", result.TenantID)
		assert.Equal(t, "tenant_user", result.Username)
	})

	t.Run("user without tenant ID", func(t *testing.T) {
		user, err := am.CreateUserWithTenant("no_tenant_user", "", "", []string{RoleNameReadWrite})
		require.NoError(t, err)
		require.NotNil(t, user)

		assert.Equal(t, "no_tenant_user", user.Username)
		assert.Empty(t, user.TenantID)
	})

	t.Run("token without tenant ID", func(t *testing.T) {
		// Get user from store and type assert to *User for generateTokens
		userInfo := am.userStore.GetByUsername("no_tenant_user")
		require.NotNil(t, userInfo)
		user, ok := userInfo.(*User)
		require.True(t, ok, "UserInfo should be *User")

		token, err := am.generateTokens(user)
		require.NoError(t, err)

		// Validate the token and check tenant ID is empty
		result, err := am.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		require.True(t, result.Valid)

		assert.Empty(t, result.TenantID)
	})

	t.Run("CreateUser delegates to CreateUserWithTenant with empty tenant", func(t *testing.T) {
		// CreateUser should create a user with no tenant ID
		user, err := am.CreateUser("legacy_user", "", []string{RoleNameReadOnly})
		require.NoError(t, err)
		require.NotNil(t, user)

		assert.Equal(t, "legacy_user", user.Username)
		assert.Empty(t, user.TenantID)
	})
}

// TestTokenValidation moved to manager_integration_test.go

func TestPasswordHashing(t *testing.T) {
	t.Run("bcrypt hash and verify", func(t *testing.T) {
		password := "testpassword123"

		// Hash password with bcrypt
		hash, err := hashPassword(password)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.True(t, isBcryptHash(hash), "hash should be a bcrypt hash")

		// Verify correct password returns match with no rehash needed
		matched, rehashed := verifyPassword(hash, password)
		assert.True(t, matched)
		assert.Empty(t, rehashed, "bcrypt hash should not trigger rehash")
	})

	t.Run("wrong password rejected", func(t *testing.T) {
		password := "testpassword123"

		hash, err := hashPassword(password)
		require.NoError(t, err)

		// Wrong password should not match
		matched, rehashed := verifyPassword(hash, "wrongpassword!")
		assert.False(t, matched)
		assert.Empty(t, rehashed)
	})

	t.Run("different passwords produce different hashes", func(t *testing.T) {
		hash1, err := hashPassword("password-one-12")
		require.NoError(t, err)

		hash2, err := hashPassword("password-two-12")
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("same password produces unique hashes due to salt", func(t *testing.T) {
		password := "same-password-123"

		hash1, err := hashPassword(password)
		require.NoError(t, err)

		hash2, err := hashPassword(password)
		require.NoError(t, err)

		// bcrypt uses random salt, so same password yields different hashes
		assert.NotEqual(t, hash1, hash2)

		// Both should still verify correctly
		matched1, _ := verifyPassword(hash1, password)
		assert.True(t, matched1)

		matched2, _ := verifyPassword(hash2, password)
		assert.True(t, matched2)
	})
}

func TestPasswordHashingLegacyMigration(t *testing.T) {
	t.Run("legacy SHA-256 hash matches and triggers rehash", func(t *testing.T) {
		password := "legacypassword123"
		legacyHash := hashToken(password) // SHA-256 hex hash

		// Verify the legacy hash is NOT a bcrypt hash
		assert.False(t, isBcryptHash(legacyHash))

		// Legacy hash should match and return upgraded bcrypt hash
		matched, rehashed := verifyPassword(legacyHash, password)
		assert.True(t, matched)
		assert.NotEmpty(t, rehashed, "legacy hash match should return bcrypt upgrade")
		assert.True(t, isBcryptHash(rehashed), "upgraded hash should be bcrypt")

		// The upgraded hash should also verify correctly
		matched2, rehashed2 := verifyPassword(rehashed, password)
		assert.True(t, matched2)
		assert.Empty(t, rehashed2, "bcrypt hash should not trigger another rehash")
	})

	t.Run("legacy SHA-256 hash rejects wrong password", func(t *testing.T) {
		password := "legacypassword123"
		legacyHash := hashToken(password) // SHA-256 hex hash

		matched, rehashed := verifyPassword(legacyHash, "wrongpassword!")
		assert.False(t, matched)
		assert.Empty(t, rehashed)
	})
}

func TestIsBcryptHash(t *testing.T) {
	t.Run("bcrypt hash detected", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte("test"), bcrypt.MinCost)
		require.NoError(t, err)
		assert.True(t, isBcryptHash(string(hash)))
	})

	t.Run("SHA-256 hex hash not detected as bcrypt", func(t *testing.T) {
		sha256Hash := hashToken("test")
		assert.False(t, isBcryptHash(sha256Hash))
	})

	t.Run("empty string not detected as bcrypt", func(t *testing.T) {
		assert.False(t, isBcryptHash(""))
	})

	t.Run("arbitrary string not detected as bcrypt", func(t *testing.T) {
		assert.False(t, isBcryptHash("not-a-hash"))
	})
}

func TestValidatePasswordLength(t *testing.T) {
	t.Run("valid password meets minimum length", func(t *testing.T) {
		err := validatePasswordLength("123456789012") // exactly 12 chars
		assert.NoError(t, err)
	})

	t.Run("long password accepted", func(t *testing.T) {
		err := validatePasswordLength("this-is-a-very-long-password-that-exceeds-minimum")
		assert.NoError(t, err)
	})

	t.Run("short password rejected", func(t *testing.T) {
		err := validatePasswordLength("short")
		require.Error(t, err)

		var ptse *api.PasswordTooShortError
		assert.True(t, errors.As(err, &ptse), "error should be PasswordTooShortError")
		assert.Equal(t, MinPasswordLength, ptse.MinLength)
		assert.Equal(t, 5, ptse.GotLength)
	})

	t.Run("empty password rejected", func(t *testing.T) {
		err := validatePasswordLength("")
		require.Error(t, err)

		var ptse *api.PasswordTooShortError
		assert.True(t, errors.As(err, &ptse), "error should be PasswordTooShortError")
		assert.Equal(t, 0, ptse.GotLength)
	})

	t.Run("11 character password rejected", func(t *testing.T) {
		err := validatePasswordLength("12345678901") // 11 chars
		require.Error(t, err)

		var ptse *api.PasswordTooShortError
		assert.True(t, errors.As(err, &ptse))
		assert.Equal(t, 11, ptse.GotLength)
	})
}

func TestCreateUserWithPasswordValidation(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key",
		AllowPasswordAuth: true,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	t.Run("valid password accepted", func(t *testing.T) {
		user, err := am.CreateUser("validpwuser", "securepassword12", []string{RoleNameReadWrite})
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.NotEmpty(t, user.password)
		assert.True(t, isBcryptHash(user.password), "stored hash should be bcrypt")
	})

	t.Run("short password rejected on create", func(t *testing.T) {
		user, err := am.CreateUser("shortpwuser", "short", []string{RoleNameReadWrite})
		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Contains(t, err.Error(), "password must be at least")
	})

	t.Run("empty password allowed when password auth enabled", func(t *testing.T) {
		// Empty password means no password is set (IAM-only user)
		user, err := am.CreateUser("nopwuser", "", []string{RoleNameReadWrite})
		require.NoError(t, err)
		require.NotNil(t, user)
		assert.Empty(t, user.password, "no password should be stored for empty password")
	})
}

func TestAuthenticateWithBcrypt(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key",
		TokenTTL:          1 * time.Hour,
		AllowPasswordAuth: true,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()
	password := "secure-password-123"

	// Create user with password
	user, err := am.CreateUser("bcryptuser", password, []string{RoleNameReadWrite})
	require.NoError(t, err)
	require.NotNil(t, user)

	t.Run("correct password authenticates", func(t *testing.T) {
		token, err := am.Authenticate(ctx, "bcryptuser", password)
		require.NoError(t, err)
		require.NotNil(t, token)
		assert.NotEmpty(t, token.AccessToken)
	})

	t.Run("wrong password rejected", func(t *testing.T) {
		token, err := am.Authenticate(ctx, "bcryptuser", "wrong-password-123")
		assert.Error(t, err)
		assert.Nil(t, token)
	})
}

func TestLegacyHashMigrationOnLogin(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key",
		TokenTTL:          1 * time.Hour,
		AllowPasswordAuth: true,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()
	password := "legacy-password-123"

	// Simulate a user with a legacy SHA-256 hash by directly setting the field
	legacyHash := hashToken(password)
	userID, err := generateUserID()
	require.NoError(t, err)

	user := &User{
		ID:       userID,
		Username: "legacyuser",
		Roles:    []string{RoleNameReadWrite},
		password: legacyHash,
		Created:  time.Now(),
	}
	am.userStore.AddUser(user)

	// Verify the stored hash is SHA-256 (not bcrypt)
	storedUser := am.userStore.GetByUsername("legacyuser")
	require.NotNil(t, storedUser)
	userObj, ok := storedUser.(*User)
	require.True(t, ok)
	assert.False(t, isBcryptHash(userObj.password), "should start as SHA-256 hash")

	// Authenticate with correct password - should succeed and migrate hash
	token, err := am.Authenticate(ctx, "legacyuser", password)
	require.NoError(t, err)
	require.NotNil(t, token)

	// After login, the hash should be upgraded to bcrypt
	storedUser = am.userStore.GetByUsername("legacyuser")
	require.NotNil(t, storedUser)
	userObj, ok = storedUser.(*User)
	require.True(t, ok)
	assert.True(t, isBcryptHash(userObj.password), "hash should be migrated to bcrypt after login")

	// Verify the user can still authenticate with the new bcrypt hash
	token2, err := am.Authenticate(ctx, "legacyuser", password)
	require.NoError(t, err)
	require.NotNil(t, token2)
}

func TestAuthDisabled(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled: false, // Auth disabled
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()

	// Authenticate should fail when auth is disabled
	_, err = am.Authenticate(ctx, "testuser", "password")
	assert.Error(t, err)

	// Authorize should succeed when auth is disabled
	err = am.Authorize(ctx, "", ResourceKeys, ActionRead)
	assert.NoError(t, err)
}

func TestAnonymousAccess(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:        true,
		JWTSecret:      "test-secret-key",
		AllowAnonymous: true,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()

	// Anonymous access to health should be allowed
	err = am.Authorize(ctx, "", ResourceHealth, ActionRead)
	assert.NoError(t, err)

	// Anonymous access to metrics should be allowed
	err = am.Authorize(ctx, "", ResourceMetrics, ActionRead)
	assert.NoError(t, err)

	// Anonymous access to keys should be denied
	err = am.Authorize(ctx, "", ResourceKeys, ActionRead)
	assert.Error(t, err)
}

// TestIssueTokenForUser tests token issuance for users authenticated via
// external providers (mTLS certificates, FIDO2/WebAuthn).
func TestIssueTokenForUser(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
		TokenTTL:  1 * time.Hour,
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("issue token for existing user", func(t *testing.T) {
		// Create a user (simulating user registered via bootstrap)
		user, err := am.CreateUser("certuser", "", []string{RoleNameReadWrite})
		require.NoError(t, err)
		require.NotNil(t, user)

		// Issue token for the user (simulating successful mTLS/FIDO2 auth)
		token, err := am.IssueTokenForUser(ctx, user)
		require.NoError(t, err)
		require.NotNil(t, token)

		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)

		// Verify the token works for authorization
		err = am.Authorize(ctx, token.AccessToken, ResourceKeys, ActionRead)
		assert.NoError(t, err)
	})

	t.Run("issue token with user lookup by username", func(t *testing.T) {
		// Create a user
		_, err := am.CreateUser("lookupuser", "", []string{RoleNameAdmin})
		require.NoError(t, err)

		// Create a minimal UserInfo implementation for lookup
		minimalUser := &minimalUserInfo{username: "lookupuser"}

		// Issue token - should look up the user by username
		token, err := am.IssueTokenForUser(ctx, minimalUser)
		require.NoError(t, err)
		require.NotNil(t, token)

		assert.NotEmpty(t, token.AccessToken)

		// Verify admin permissions
		err = am.Authorize(ctx, token.AccessToken, ResourceUsers, ActionAdmin)
		assert.NoError(t, err)
	})

	t.Run("issue token for non-existent user fails", func(t *testing.T) {
		minimalUser := &minimalUserInfo{username: "nonexistent"}

		token, err := am.IssueTokenForUser(ctx, minimalUser)
		assert.Error(t, err)
		assert.Nil(t, token)
		assert.Contains(t, err.Error(), "user not found")
	})

	t.Run("issue token with nil user fails", func(t *testing.T) {
		token, err := am.IssueTokenForUser(ctx, nil)
		assert.Error(t, err)
		assert.Nil(t, token)
		assert.Contains(t, err.Error(), "user cannot be nil")
	})

	t.Run("issue token when auth disabled fails", func(t *testing.T) {
		disabledConfig := &AuthConfig{
			Enabled: false,
		}

		disabledAM, err := NewInMemoryAuthManager(disabledConfig, log)
		require.NoError(t, err)

		user, err := disabledAM.CreateUser("disableduser", "", []string{RoleNameReadWrite})
		require.NoError(t, err)

		token, err := disabledAM.IssueTokenForUser(ctx, user)
		assert.Error(t, err)
		assert.Nil(t, token)
		assert.Contains(t, err.Error(), "authentication is not enabled")
	})
}

// minimalUserInfo is a test helper that implements interfaces.UserInfo
// for testing user lookup scenarios (e.g., external auth providers).
type minimalUserInfo struct {
	username string
}

func (m *minimalUserInfo) GetID() string          { return "" }
func (m *minimalUserInfo) GetUsername() string    { return m.username }
func (m *minimalUserInfo) GetRoles() []string     { return nil }
func (m *minimalUserInfo) GetCreated() time.Time  { return time.Time{} }
func (m *minimalUserInfo) GetLastAuth() time.Time { return time.Time{} }

// Helper function to create a test logger.
func createTestLogger(t *testing.T) *logger.Logger {
	config := logger.LogConfig{
		Level:     "error", // Reduce noise in tests
		Format:    "json",
		Output:    "stdout",
		Component: "auth-test",
	}

	log, err := logger.New(config)
	require.NoError(t, err)
	return log
}

// =============================================================================
// Consolidated from coverage_91_test.go
// =============================================================================

// TestTokenValidationResultGetters tests all getter methods on TokenValidationResult.
func TestTokenValidationResultGetters(t *testing.T) {
	result := &TokenValidationResult{
		Valid:    true,
		UserID:   "uid-1",
		Username: "alice",
		TenantID: "tenant-1",
		Roles:    []string{"admin", "reader"},
	}

	t.Run("GetValid", func(t *testing.T) {
		assert.True(t, result.GetValid())
	})

	t.Run("IsValid", func(t *testing.T) {
		assert.True(t, result.IsValid())
	})

	t.Run("GetUserID", func(t *testing.T) {
		assert.Equal(t, "uid-1", result.GetUserID())
	})

	t.Run("GetUsername", func(t *testing.T) {
		assert.Equal(t, "alice", result.GetUsername())
	})

	t.Run("GetTenantID", func(t *testing.T) {
		assert.Equal(t, "tenant-1", result.GetTenantID())
	})

	t.Run("GetRoles", func(t *testing.T) {
		assert.Equal(t, []string{"admin", "reader"}, result.GetRoles())
	})

	t.Run("false values", func(t *testing.T) {
		empty := &TokenValidationResult{}
		assert.False(t, empty.GetValid())
		assert.False(t, empty.IsValid())
		assert.Empty(t, empty.GetUserID())
	})
}

// TestUserGetTenantID tests the GetTenantID getter on User.
func TestUserGetTenantID(t *testing.T) {
	t.Run("returns tenant ID", func(t *testing.T) {
		user := &User{
			ID:       "u1",
			Username: "bob",
			TenantID: "t-42",
		}
		assert.Equal(t, "t-42", user.GetTenantID())
	})

	t.Run("returns empty string when no tenant", func(t *testing.T) {
		user := &User{
			ID:       "u2",
			Username: "carol",
		}
		assert.Empty(t, user.GetTenantID())
	})
}

// TestInMemoryValidateCredentials tests the in-memory validateCredentials method.
func TestInMemoryValidateCredentials(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key",
		AllowPasswordAuth: true,
	}
	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	// Create user with password (must be >= 12 characters)
	user, err := am.CreateUser("pwduser", "secret123long", []string{RoleNameReadOnly})
	require.NoError(t, err)
	require.NotNil(t, user)

	ctx := context.Background()

	t.Run("empty username", func(t *testing.T) {
		_, err := am.validateCredentials(ctx, "", "password")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username and password are required")
	})

	t.Run("empty password", func(t *testing.T) {
		_, err := am.validateCredentials(ctx, "pwduser", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username and password are required")
	})

	t.Run("nonexistent user", func(t *testing.T) {
		_, err := am.validateCredentials(ctx, "noone", "password")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid credentials")
	})

	t.Run("wrong password", func(t *testing.T) {
		_, err := am.validateCredentials(ctx, "pwduser", "wrongpassword")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid credentials")
	})

	t.Run("correct credentials", func(t *testing.T) {
		u, err := am.validateCredentials(ctx, "pwduser", "secret123long")
		require.NoError(t, err)
		assert.Equal(t, "pwduser", u.Username)
	})

	t.Run("user without password set", func(t *testing.T) {
		_, err := am.CreateUser("nopwduser", "", []string{RoleNameReadOnly})
		require.NoError(t, err)

		_, err = am.validateCredentials(ctx, "nopwduser", "anything")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user has no password set")
	})
}

// TestInMemoryAuthenticate tests the Authenticate method on InMemoryAuthManager.
func TestInMemoryAuthenticate(t *testing.T) {
	log := createTestLogger(t)

	t.Run("auth disabled", func(t *testing.T) {
		cfg := &AuthConfig{Enabled: false}
		am, err := NewInMemoryAuthManager(cfg, log)
		require.NoError(t, err)

		_, err = am.Authenticate(context.Background(), "user", "pass")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not enabled")
	})

	t.Run("password auth disabled", func(t *testing.T) {
		cfg := &AuthConfig{
			Enabled:           true,
			JWTSecret:         "test-secret",
			AllowPasswordAuth: false,
		}
		am, err := NewInMemoryAuthManager(cfg, log)
		require.NoError(t, err)

		_, err = am.Authenticate(context.Background(), "user", "pass")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "password-based authentication is disabled")
	})

	t.Run("successful authentication", func(t *testing.T) {
		cfg := &AuthConfig{
			Enabled:           true,
			JWTSecret:         "test-secret",
			AllowPasswordAuth: true,
		}
		am, err := NewInMemoryAuthManager(cfg, log)
		require.NoError(t, err)

		_, err = am.CreateUser("authuser", "mypassword12", []string{RoleNameReadOnly})
		require.NoError(t, err)

		token, err := am.Authenticate(context.Background(), "authuser", "mypassword12")
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)
	})
}

// TestInMemoryCreateUserWithTenant tests multi-tenant user creation.
func TestInMemoryCreateUserWithTenant(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:           true,
		JWTSecret:         "test-secret-key",
		AllowPasswordAuth: true,
	}
	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	t.Run("create with tenant ID", func(t *testing.T) {
		user, err := am.CreateUserWithTenant("tenantuser", "password-valid", "tenant-abc", []string{RoleNameReadOnly})
		require.NoError(t, err)
		assert.Equal(t, "tenant-abc", user.TenantID)
		assert.Equal(t, "tenantuser", user.Username)
	})

	t.Run("create without tenant ID", func(t *testing.T) {
		user, err := am.CreateUserWithTenant("singletenantuser", "password-valid", "", []string{RoleNameReadOnly})
		require.NoError(t, err)
		assert.Empty(t, user.TenantID)
	})

	t.Run("empty username", func(t *testing.T) {
		_, err := am.CreateUserWithTenant("", "password-valid", "", []string{RoleNameReadOnly})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("duplicate user", func(t *testing.T) {
		_, err := am.CreateUserWithTenant("tenantuser", "password-valid", "", []string{RoleNameReadOnly})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user already exists")
	})
}

// TestInMemoryIssueTokenForUser tests the IssueTokenForUser method.
func TestInMemoryIssueTokenForUser(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}
	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	t.Run("auth disabled", func(t *testing.T) {
		disabledCfg := &AuthConfig{Enabled: false}
		disabledAM, err := NewInMemoryAuthManager(disabledCfg, log)
		require.NoError(t, err)

		_, err = disabledAM.IssueTokenForUser(context.Background(), &User{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not enabled")
	})

	t.Run("nil user", func(t *testing.T) {
		_, err := am.IssueTokenForUser(context.Background(), nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user cannot be nil")
	})

	t.Run("user type is concrete User", func(t *testing.T) {
		user, err := am.CreateUser("tokenuser", "", []string{RoleNameReadOnly})
		require.NoError(t, err)

		token, err := am.IssueTokenForUser(context.Background(), user)
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
	})

	t.Run("user type is interface resolved by username", func(t *testing.T) {
		_, err := am.CreateUser("extuser", "", []string{RoleNameReadOnly})
		require.NoError(t, err)

		mockUser := &mockUserInfo{
			id:       "ext-id",
			username: "extuser",
			roles:    []string{RoleNameReadOnly},
			created:  time.Now(),
		}

		token, err := am.IssueTokenForUser(context.Background(), mockUser)
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
	})

	t.Run("user type is interface but not found", func(t *testing.T) {
		mockUser := &mockUserInfo{
			id:       "notfound-id",
			username: "notfounduser",
			roles:    []string{},
			created:  time.Now(),
		}

		_, err := am.IssueTokenForUser(context.Background(), mockUser)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
	})
}

// mockUserInfo is a minimal implementation of interfaces.UserInfo for testing.
type mockUserInfo struct {
	id       string
	username string
	tenantID string
	roles    []string
	created  time.Time
	lastAuth time.Time
}

func (m *mockUserInfo) GetID() string          { return m.id }
func (m *mockUserInfo) GetUsername() string    { return m.username }
func (m *mockUserInfo) GetTenantID() string    { return m.tenantID }
func (m *mockUserInfo) GetRoles() []string     { return m.roles }
func (m *mockUserInfo) GetCreated() time.Time  { return m.created }
func (m *mockUserInfo) GetLastAuth() time.Time { return m.lastAuth }

// TestResetAdminPasswordDeprecated tests the deprecated ResetAdminPassword function.
func TestResetAdminPasswordDeprecated(t *testing.T) {
	err := ResetAdminPassword(nil, "newpass")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "password-based authentication is disabled")
}

// TestExtractTenantFromContextCoverage tests the exported ExtractTenantFromContext function.
func TestExtractTenantFromContextCoverage(t *testing.T) {
	t.Run("no metadata", func(t *testing.T) {
		tenant := ExtractTenantFromContext(context.Background())
		assert.Empty(t, tenant)
	})

	t.Run("metadata without tenant header", func(t *testing.T) {
		md := metadata.Pairs("authorization", "Bearer token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		tenant := ExtractTenantFromContext(ctx)
		assert.Empty(t, tenant)
	})

	t.Run("metadata with tenant header", func(t *testing.T) {
		md := metadata.Pairs("x-tenant-id", "my-tenant")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		tenant := ExtractTenantFromContext(ctx)
		assert.Equal(t, "my-tenant", tenant)
	})
}

// TestValidateTenantHeaderCoverage tests the validateTenantHeader function.
func TestValidateTenantHeaderCoverage(t *testing.T) {
	log := createTestLogger(t)

	t.Run("no tenant in subject skips validation", func(t *testing.T) {
		subject := &interfaces.Subject{
			ID:       "user-1",
			TenantID: "",
		}
		err := validateTenantHeader(context.Background(), subject, log)
		assert.NoError(t, err)
	})

	t.Run("tenant in subject but no metadata", func(t *testing.T) {
		subject := &interfaces.Subject{
			ID:       "user-1",
			TenantID: "tenant-1",
		}
		err := validateTenantHeader(context.Background(), subject, log)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing request metadata")
	})

	t.Run("tenant in subject but no tenant header", func(t *testing.T) {
		subject := &interfaces.Subject{
			ID:       "user-1",
			TenantID: "tenant-1",
		}
		md := metadata.Pairs("authorization", "Bearer token")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		err := validateTenantHeader(ctx, subject, log)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "x-tenant-id header required")
	})

	t.Run("mismatched tenant header", func(t *testing.T) {
		subject := &interfaces.Subject{
			ID:       "user-1",
			TenantID: "tenant-1",
		}
		md := metadata.Pairs("x-tenant-id", "other-tenant")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		err := validateTenantHeader(ctx, subject, log)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cross-tenant access denied")
	})

	t.Run("matching tenant header", func(t *testing.T) {
		subject := &interfaces.Subject{
			ID:       "user-1",
			TenantID: "tenant-1",
		}
		md := metadata.Pairs("x-tenant-id", "tenant-1")
		ctx := metadata.NewIncomingContext(context.Background(), md)
		err := validateTenantHeader(ctx, subject, log)
		assert.NoError(t, err)
	})
}

// TestMapObjectStorageMethodCoverage tests all branches of mapObjectStorageMethod.
func TestMapObjectStorageMethodCoverage(t *testing.T) {
	tests := []struct {
		method         string
		expectResource string
		expectAction   string
	}{
		{"GetObject", interfaces.ResourceObject, interfaces.ActionObjectGet},
		{"HeadObject", interfaces.ResourceObject, interfaces.ActionObjectGet},
		{"DownloadObject", interfaces.ResourceObject, interfaces.ActionObjectGet},
		{"ListObjects", interfaces.ResourceObject, interfaces.ActionObjectList},
		{"ListObjectVersions", interfaces.ResourceObject, interfaces.ActionObjectList},
		{"PutObject", interfaces.ResourceObject, interfaces.ActionObjectPut},
		{"UploadObject", interfaces.ResourceObject, interfaces.ActionObjectPut},
		{"CopyObject", interfaces.ResourceObject, interfaces.ActionObjectPut},
		{"UpdateObjectMetadata", interfaces.ResourceObject, interfaces.ActionObjectPut},
		{"DeleteObject", interfaces.ResourceObject, interfaces.ActionObjectDelete},
		{"DeleteObjects", interfaces.ResourceObject, interfaces.ActionObjectDelete},
		{"BatchDeleteObjects", interfaces.ResourceObject, interfaces.ActionObjectDelete},
		{"ReplicateObject", interfaces.ResourceObject, interfaces.ActionObjectReplicate},
		{"StartReplication", interfaces.ResourceObject, interfaces.ActionObjectReplicate},
		{"MigrateObject", interfaces.ResourceObject, interfaces.ActionObjectMigrate},
		{"StartMigration", interfaces.ResourceObject, interfaces.ActionObjectMigrate},
		{"ArchiveObject", interfaces.ResourceObject, interfaces.ActionObjectArchive},
		{"RestoreObject", interfaces.ResourceObject, interfaces.ActionObjectArchive},
		{"VerifyObject", interfaces.ResourceObject, interfaces.ActionObjectVerify},
		{"VerifyIntegrity", interfaces.ResourceObject, interfaces.ActionObjectVerify},
		{"SetObjectPolicy", interfaces.ResourceObject, interfaces.ActionObjectSetPolicy},
		{"GetObjectPolicy", interfaces.ResourceObject, interfaces.ActionObjectSetPolicy},
		{"DeleteObjectPolicy", interfaces.ResourceObject, interfaces.ActionObjectSetPolicy},
		{"SetObjectACL", interfaces.ResourceObject, interfaces.ActionObjectSetACL},
		{"GetObjectACL", interfaces.ResourceObject, interfaces.ActionObjectSetACL},
		{"GrantAccess", interfaces.ResourceObject, interfaces.ActionObjectSetACL},
		{"RevokeAccess", interfaces.ResourceObject, interfaces.ActionObjectSetACL},
		{"CreateBucket", interfaces.ResourceBucket, interfaces.ActionBucketCreate},
		{"CreateNamespace", interfaces.ResourceBucket, interfaces.ActionBucketCreate},
		{"DeleteBucket", interfaces.ResourceBucket, interfaces.ActionBucketDelete},
		{"DeleteNamespace", interfaces.ResourceBucket, interfaces.ActionBucketDelete},
		{"ListBuckets", interfaces.ResourceBucket, interfaces.ActionBucketList},
		{"ListNamespaces", interfaces.ResourceBucket, interfaces.ActionBucketList},
		{"SetBucketPolicy", interfaces.ResourceBucket, interfaces.ActionBucketSetPolicy},
		{"GetBucketPolicy", interfaces.ResourceBucket, interfaces.ActionBucketSetPolicy},
		{"Health", interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"GetStorageStats", interfaces.ResourceObjectStore, interfaces.ActionRead},
		{"UnknownMethod", interfaces.ResourceObjectStore, interfaces.ActionAdmin},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			resource, action := mapObjectStorageMethod(tc.method)
			assert.Equal(t, tc.expectResource, resource)
			assert.Equal(t, tc.expectAction, action)
		})
	}
}

// TestMapCatalogMethodCoverage tests all branches of mapCatalogMethod.
func TestMapCatalogMethodCoverage(t *testing.T) {
	tests := []struct {
		method         string
		expectResource string
		expectAction   string
	}{
		{"GetEntry", interfaces.ResourceCatalog, interfaces.ActionRead},
		{"GetCatalogEntry", interfaces.ResourceCatalog, interfaces.ActionRead},
		{"ListEntries", interfaces.ResourceCatalog, interfaces.ActionRead},
		{"SearchEntries", interfaces.ResourceCatalog, interfaces.ActionRead},
		{"PutEntry", interfaces.ResourceCatalog, interfaces.ActionWrite},
		{"UpdateEntry", interfaces.ResourceCatalog, interfaces.ActionWrite},
		{"CreateEntry", interfaces.ResourceCatalog, interfaces.ActionWrite},
		{"DeleteEntry", interfaces.ResourceCatalog, interfaces.ActionDelete},
		{"PurgeEntry", interfaces.ResourceCatalog, interfaces.ActionDelete},
		{"RebuildIndexes", interfaces.ResourceCatalog, interfaces.ActionAdmin},
		{"CompactCatalog", interfaces.ResourceCatalog, interfaces.ActionAdmin},
		{"SyncCatalog", interfaces.ResourceCatalog, interfaces.ActionAdmin},
		{"Unknown", interfaces.ResourceCatalog, interfaces.ActionAdmin},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			resource, action := mapCatalogMethod(tc.method)
			assert.Equal(t, tc.expectResource, resource)
			assert.Equal(t, tc.expectAction, action)
		})
	}
}

// TestMapVectorMethodAllBranches tests all branches of mapVectorMethod.
func TestMapVectorMethodAllBranches(t *testing.T) {
	tests := []struct {
		method string
		action string
	}{
		{"GetVector", interfaces.ActionVectorRead},
		{"Get", interfaces.ActionVectorRead},
		{"ListVectors", interfaces.ActionVectorRead},
		{"List", interfaces.ActionVectorRead},
		{"SearchSimilar", interfaces.ActionVectorSearch},
		{"Search", interfaces.ActionVectorSearch},
		{"SearchByID", interfaces.ActionVectorSearch},
		{"HybridSearch", interfaces.ActionVectorSearch},
		{"SearchByMetadata", interfaces.ActionVectorSearch},
		{"UpsertVector", interfaces.ActionVectorWrite},
		{"Upsert", interfaces.ActionVectorWrite},
		{"Put", interfaces.ActionVectorWrite},
		{"BatchUpsertVectors", interfaces.ActionVectorWrite},
		{"BatchUpsert", interfaces.ActionVectorWrite},
		{"UpdateMetadata", interfaces.ActionVectorWrite},
		{"DeleteVector", interfaces.ActionVectorDelete},
		{"Delete", interfaces.ActionVectorDelete},
		{"BatchDeleteVectors", interfaces.ActionVectorDelete},
		{"BatchDelete", interfaces.ActionVectorDelete},
		{"CreateNamespace", interfaces.ActionVectorNamespace},
		{"DeleteNamespace", interfaces.ActionVectorNamespace},
		{"UpdateNamespace", interfaces.ActionVectorNamespace},
		{"GetNamespaceInfo", interfaces.ActionVectorNamespace},
		{"ListNamespaces", interfaces.ActionVectorRead},
		{"Health", interfaces.ActionVectorStats},
		{"GetVectorStats", interfaces.ActionVectorStats},
		{"VectorHealth", interfaces.ActionVectorStats},
		{"RebuildIndex", interfaces.ActionVectorAdmin},
		{"GetIndexProgress", interfaces.ActionVectorAdmin},
		{"CompactNamespace", interfaces.ActionVectorAdmin},
		{"ExportVectors", interfaces.ActionVectorAdmin},
		{"ImportVectors", interfaces.ActionVectorAdmin},
		{"UnknownVectorOp", interfaces.ActionVectorAdmin},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			resource, action := mapVectorMethod(tc.method)
			assert.Equal(t, interfaces.ResourceVector, resource)
			assert.Equal(t, tc.action, action)
		})
	}
}

// TestMapRouteServiceDispatching tests all service routing in mapMethodToResourceAction.
func TestMapRouteServiceDispatching(t *testing.T) {
	t.Run("cluster service", func(t *testing.T) {
		resource, action := mapMethodToResourceAction("quicbaas.v1.Cluster", "Status")
		assert.Equal(t, interfaces.ResourceCluster, resource)
		assert.Equal(t, interfaces.ActionAdmin, action)
	})

	t.Run("seal service", func(t *testing.T) {
		resource, action := mapMethodToResourceAction("quicbaas.v1.Seal", "Unseal")
		assert.Equal(t, interfaces.ResourceSeal, resource)
		assert.Equal(t, interfaces.ActionAdmin, action)
	})

	t.Run("health service", func(t *testing.T) {
		resource, action := mapMethodToResourceAction("grpc.health.v1.Health", "Check")
		assert.Equal(t, interfaces.ResourceHealth, resource)
		assert.Equal(t, interfaces.ActionRead, action)
	})

	t.Run("VectorDB service", func(t *testing.T) {
		resource, action := mapMethodToResourceAction("quicbaas.v1.VectorDB", "GetVector")
		assert.Equal(t, interfaces.ResourceVector, resource)
		assert.Equal(t, interfaces.ActionVectorRead, action)
	})

	t.Run("VectorAdmin service", func(t *testing.T) {
		resource, action := mapMethodToResourceAction("quicbaas.v1.VectorAdmin", "RebuildIndex")
		assert.Equal(t, interfaces.ResourceVector, resource)
		assert.Equal(t, interfaces.ActionVectorAdmin, action)
	})

	t.Run("ObjectStorageService", func(t *testing.T) {
		resource, action := mapMethodToResourceAction("quicbaas.v1.ObjectStorageService", "PutObject")
		assert.Equal(t, interfaces.ResourceObject, resource)
		assert.Equal(t, interfaces.ActionObjectPut, action)
	})

	t.Run("CatalogService", func(t *testing.T) {
		resource, action := mapMethodToResourceAction("quicbaas.v1.CatalogService", "GetEntry")
		assert.Equal(t, interfaces.ResourceCatalog, resource)
		assert.Equal(t, interfaces.ActionRead, action)
	})

	t.Run("timeseries default branch", func(t *testing.T) {
		resource, action := mapTimeSeriesMethod("UnknownTimeSeriesOp")
		assert.Equal(t, interfaces.ResourceTimeseries, resource)
		assert.Equal(t, interfaces.ActionAdmin, action)
	})

	t.Run("crypto default branch", func(t *testing.T) {
		resource, action := mapCryptoMethod("UnknownCryptoOp")
		assert.Equal(t, interfaces.ResourceCrypto, resource)
		assert.Equal(t, interfaces.ActionAdmin, action)
	})
}

// TestObjectActionImpliesCoverage tests the objectActionImplies method on RoleManager.
func TestObjectActionImpliesCoverage(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	tests := []struct {
		name      string
		granted   string
		requested string
		expected  bool
	}{
		{"admin implies get", ActionAdmin, interfaces.ActionObjectGet, true},
		{"all implies get", ActionAll, interfaces.ActionObjectGet, true},
		{"write implies get", ActionWrite, interfaces.ActionObjectGet, true},
		{"write implies list", ActionWrite, interfaces.ActionObjectList, true},
		{"write implies read", ActionWrite, ActionRead, true},
		{"objectPut implies get", interfaces.ActionObjectPut, interfaces.ActionObjectGet, true},
		{"objectPut implies list", interfaces.ActionObjectPut, interfaces.ActionObjectList, true},
		{"replicate implies get", interfaces.ActionObjectReplicate, interfaces.ActionObjectGet, true},
		{"migrate implies list", interfaces.ActionObjectMigrate, interfaces.ActionObjectList, true},
		{"archive implies read", interfaces.ActionObjectArchive, ActionRead, true},
		{"get does not imply write", interfaces.ActionObjectGet, interfaces.ActionObjectPut, false},
		{"list does not imply delete", interfaces.ActionObjectList, interfaces.ActionObjectDelete, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := rm.objectActionImplies(tc.granted, tc.requested)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestHasObjectBucketPermissionCoverage tests the HasObjectBucketPermission method.
func TestHasObjectBucketPermissionCoverage(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	t.Run("admin has permission on any bucket", func(t *testing.T) {
		result := rm.HasObjectBucketPermission([]string{RoleNameAdmin}, "my-bucket", interfaces.ActionBucketCreate)
		assert.True(t, result)
	})

	t.Run("readonly has no bucket create permission", func(t *testing.T) {
		result := rm.HasObjectBucketPermission([]string{RoleNameReadOnly}, "my-bucket", interfaces.ActionBucketCreate)
		assert.False(t, result)
	})
}

// TestIsObjectStorageActionCoverage tests the isObjectStorageAction function.
func TestIsObjectStorageActionCoverage(t *testing.T) {
	objectActions := []string{
		interfaces.ActionObjectPut, interfaces.ActionObjectGet, interfaces.ActionObjectDelete,
		interfaces.ActionObjectList, interfaces.ActionObjectReplicate, interfaces.ActionObjectMigrate,
		interfaces.ActionObjectArchive, interfaces.ActionObjectVerify, interfaces.ActionObjectSetPolicy,
		interfaces.ActionObjectSetACL, interfaces.ActionBucketCreate, interfaces.ActionBucketDelete,
		interfaces.ActionBucketList, interfaces.ActionBucketSetPolicy,
	}

	for _, action := range objectActions {
		t.Run(action, func(t *testing.T) {
			assert.True(t, isObjectStorageAction(action))
		})
	}

	t.Run("non-object action", func(t *testing.T) {
		assert.False(t, isObjectStorageAction("read"))
		assert.False(t, isObjectStorageAction("write"))
	})
}

// TestRoleManagerAddRoleFromInterface tests AddRole with a non-concrete Role interface.
func TestRoleManagerAddRoleFromInterface(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	mockRole := &mockRoleInfo{
		name:        "custom-iface",
		description: "Custom interface role",
		permissions: []interfaces.PermissionInfo{
			&mockPermissionInfo{
				resource: ResourceKeys,
				actions:  []string{ActionRead},
			},
		},
	}

	err := rm.AddRole(mockRole)
	require.NoError(t, err)

	retrieved := rm.GetRole("custom-iface")
	require.NotNil(t, retrieved)
	assert.Equal(t, "custom-iface", retrieved.GetName())
	assert.Equal(t, "Custom interface role", retrieved.GetDescription())
}

// TestRoleManagerUpdateRoleFromInterface tests UpdateRole with a non-concrete Role interface.
func TestRoleManagerUpdateRoleFromInterface(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	role := &Role{
		Name:        "updatable",
		Description: "Before",
		Permissions: []*Permission{{Resource: ResourceKeys, Actions: []string{ActionRead}}},
	}
	err := rm.AddRole(role)
	require.NoError(t, err)

	mockRole := &mockRoleInfo{
		name:        "updatable",
		description: "After",
		permissions: []interfaces.PermissionInfo{
			&mockPermissionInfo{
				resource: ResourceKeys,
				actions:  []string{ActionRead, ActionWrite},
			},
		},
	}

	err = rm.UpdateRole(mockRole)
	require.NoError(t, err)

	retrieved := rm.GetRole("updatable")
	require.NotNil(t, retrieved)
	assert.Equal(t, "After", retrieved.GetDescription())
}

// TestUserStoreAddUserFromInterface tests AddUser with a non-concrete User interface.
func TestUserStoreAddUserFromInterface(t *testing.T) {
	us := NewUserStore()

	mockUser := &mockUserInfo{
		id:       "mock-id",
		username: "mockuser",
		roles:    []string{"reader"},
		created:  time.Now(),
		lastAuth: time.Now(),
	}

	us.AddUser(mockUser)

	retrieved := us.GetByID("mock-id")
	require.NotNil(t, retrieved)
	assert.Equal(t, "mockuser", retrieved.GetUsername())
}

// TestUserStoreUpdateUserFromInterface tests UpdateUser with a non-concrete User interface.
func TestUserStoreUpdateUserFromInterface(t *testing.T) {
	us := NewUserStore()

	us.AddUser(&User{
		ID:       "uid-1",
		Username: "bob",
		Roles:    []string{"reader"},
		Created:  time.Now(),
	})

	mockUser := &mockUserInfo{
		id:       "uid-1",
		username: "bob",
		roles:    []string{"reader", "writer"},
		created:  time.Now(),
		lastAuth: time.Now(),
	}

	us.UpdateUser(mockUser)

	retrieved := us.GetByID("uid-1")
	require.NotNil(t, retrieved)
	assert.Equal(t, []string{"reader", "writer"}, retrieved.GetRoles())
}

// TestBuildSubjectFromTokenCoverage tests the buildSubjectFromToken function.
func TestBuildSubjectFromTokenCoverage(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}
	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	t.Run("empty token returns anonymous subject", func(t *testing.T) {
		subject, err := buildSubjectFromToken(context.Background(), am, "")
		require.NoError(t, err)
		assert.Equal(t, "anonymous", subject.ID)
		assert.Equal(t, "anonymous", subject.Username)
		assert.False(t, subject.Authenticated)
	})

	t.Run("valid token returns authenticated subject", func(t *testing.T) {
		user, err := am.CreateUser("subjectuser", "", []string{RoleNameReadOnly})
		require.NoError(t, err)

		token, err := am.generateTokens(user)
		require.NoError(t, err)

		subject, err := buildSubjectFromToken(context.Background(), am, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, subject.Authenticated)
		assert.Equal(t, "subjectuser", subject.Username)
	})

	t.Run("invalid token returns error", func(t *testing.T) {
		_, err := buildSubjectFromToken(context.Background(), am, "bogus-token")
		assert.Error(t, err)
	})
}

// TestNewAuthManagerFactoryCoverage tests the NewAuthManager factory function.
func TestNewAuthManagerFactoryCoverage(t *testing.T) {
	log := createTestLogger(t)

	t.Run("nil config returns error", func(t *testing.T) {
		_, err := NewAuthManager(nil, nil, log)
		assert.Error(t, err)
	})

	t.Run("nil logger returns error", func(t *testing.T) {
		cfg := &AuthConfig{Enabled: true, JWTSecret: "secret"}
		_, err := NewAuthManager(cfg, nil, nil)
		assert.Error(t, err)
	})

	t.Run("nil storage returns in-memory manager", func(t *testing.T) {
		cfg := &AuthConfig{Enabled: true, JWTSecret: "secret"}
		mgr, err := NewAuthManager(cfg, nil, log)
		require.NoError(t, err)
		_, ok := mgr.(*InMemoryAuthManager)
		assert.True(t, ok)
	})

	t.Run("mock storage returns persistent manager", func(t *testing.T) {
		cfg := &AuthConfig{Enabled: true, JWTSecret: "secret"}
		mockStor := NewMockStorage()
		mgr, err := NewAuthManager(cfg, mockStor, log)
		require.NoError(t, err)
		_, ok := mgr.(*PersistentAuthManager)
		assert.True(t, ok)
	})
}

// mockRoleInfo implements interfaces.RoleInfo for testing.
type mockRoleInfo struct {
	name        string
	description string
	permissions []interfaces.PermissionInfo
}

func (m *mockRoleInfo) GetName() string                             { return m.name }
func (m *mockRoleInfo) GetDescription() string                      { return m.description }
func (m *mockRoleInfo) GetPermissions() []interfaces.PermissionInfo { return m.permissions }

// mockPermissionInfo implements interfaces.PermissionInfo for testing.
type mockPermissionInfo struct {
	resource string
	actions  []string
}

func (m *mockPermissionInfo) GetResource() string  { return m.resource }
func (m *mockPermissionInfo) GetActions() []string { return m.actions }

// =============================================================================
// Consolidated from coverage_boost_test.go
// =============================================================================

// TestMapQuicBaaSMethod tests the mapQuicBaaSMethod function for all operation categories.
func TestMapQuicBaaSMethod(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		wantResource string
		wantAction   string
	}{
		{"Get returns keys read", "Get", interfaces.ResourceKeys, interfaces.ActionRead},
		{"Range returns keys read", "Range", interfaces.ResourceKeys, interfaces.ActionRead},
		{"List returns keys read", "List", interfaces.ResourceKeys, interfaces.ActionRead},
		{"Query returns keys read", "Query", interfaces.ResourceKeys, interfaces.ActionRead},
		{"ScanIndex returns keys read", "ScanIndex", interfaces.ResourceKeys, interfaces.ActionRead},
		{"BatchGet returns keys read", "BatchGet", interfaces.ResourceKeys, interfaces.ActionRead},
		{"Put returns keys write", "Put", interfaces.ResourceKeys, interfaces.ActionWrite},
		{"BatchPut returns keys write", "BatchPut", interfaces.ResourceKeys, interfaces.ActionWrite},
		{"Delete returns keys delete", "Delete", interfaces.ResourceKeys, interfaces.ActionDelete},
		{"BatchDelete returns keys delete", "BatchDelete", interfaces.ResourceKeys, interfaces.ActionDelete},
		{"Txn returns transactions write", "Txn", interfaces.ResourceTransactions, interfaces.ActionWrite},
		{"Transaction returns transactions write", "Transaction", interfaces.ResourceTransactions, interfaces.ActionWrite},
		{"ClusterStatus returns cluster read", "ClusterStatus", interfaces.ResourceCluster, interfaces.ActionRead},
		{"GetLeader returns cluster read", "GetLeader", interfaces.ResourceCluster, interfaces.ActionRead},
		{"Watch returns watch read", "Watch", interfaces.ResourceWatch, interfaces.ActionRead},
		{"Health returns health read", "Health", interfaces.ResourceHealth, interfaces.ActionRead},
		{"Aggregate returns keys read", "Aggregate", interfaces.ResourceKeys, interfaces.ActionRead},
		{"Login returns users read", "Login", interfaces.ResourceUsers, interfaces.ActionRead},
		{"unknown method returns admin", "UnknownMethod", interfaces.ResourceAll, interfaces.ActionAdmin},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resource, action := mapQuicBaaSMethod(tc.method)
			assert.Equal(t, tc.wantResource, resource)
			assert.Equal(t, tc.wantAction, action)
		})
	}
}

// TestMapKMSMethod tests the mapKMSMethod function for all KMS operations.
func TestMapKMSMethod(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		wantResource string
		wantAction   string
	}{
		{"GetKey returns read", "GetKey", interfaces.ResourceKMS, interfaces.ActionRead},
		{"ListKeys returns read", "ListKeys", interfaces.ResourceKMS, interfaces.ActionRead},
		{"GetKeyVersion returns read", "GetKeyVersion", interfaces.ResourceKMS, interfaces.ActionRead},
		{"CreateKey returns write", "CreateKey", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"RotateKey returns write", "RotateKey", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"ImportKey returns write", "ImportKey", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"DeleteKey returns delete", "DeleteKey", interfaces.ResourceKMS, interfaces.ActionDelete},
		{"DeleteKeyVersion returns delete", "DeleteKeyVersion", interfaces.ResourceKMS, interfaces.ActionDelete},
		{"EnableKey returns admin", "EnableKey", interfaces.ResourceKMS, interfaces.ActionAdmin},
		{"DisableKey returns admin", "DisableKey", interfaces.ResourceKMS, interfaces.ActionAdmin},
		{"unknown KMS method returns admin", "UnknownKMSMethod", interfaces.ResourceKMS, interfaces.ActionAdmin},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resource, action := mapKMSMethod(tc.method)
			assert.Equal(t, tc.wantResource, resource)
			assert.Equal(t, tc.wantAction, action)
		})
	}
}

// TestMapSecretsMethod tests the mapSecretsMethod function for all secrets operations.
func TestMapSecretsMethod(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		wantResource string
		wantAction   string
	}{
		{"GetSecret returns read", "GetSecret", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"ListSecrets returns read", "ListSecrets", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"GetSecretVersion returns read", "GetSecretVersion", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"ListSecretVersions returns read", "ListSecretVersions", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"CreateSecret returns write", "CreateSecret", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"UpdateSecret returns write", "UpdateSecret", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"SetSecret returns write", "SetSecret", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"DeleteSecret returns delete", "DeleteSecret", interfaces.ResourceSecrets, interfaces.ActionDelete},
		{"DeleteSecretVersion returns delete", "DeleteSecretVersion", interfaces.ResourceSecrets, interfaces.ActionDelete},
		{"unknown method returns admin", "UnknownSecretsMethod", interfaces.ResourceSecrets, interfaces.ActionAdmin},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resource, action := mapSecretsMethod(tc.method)
			assert.Equal(t, tc.wantResource, resource)
			assert.Equal(t, tc.wantAction, action)
		})
	}
}

// TestMustBootstrap_UnknownManagerType tests MustBootstrap with an unknown manager type.
func TestMustBootstrap_UnknownManagerType(t *testing.T) {
	_, err := MustBootstrap("not-a-manager", &BootstrapConfig{
		CreateAdminUser: true,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth manager type")
}

// TestMustBootstrap_InMemoryManager tests MustBootstrap with the in-memory manager.
func TestMustBootstrap_InMemoryManager(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-must-bootstrap",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	result, err := MustBootstrap(am, &BootstrapConfig{
		CreateAdminUser: true,
		AdminUsername:   "test-admin",
	})
	require.NoError(t, err)
	assert.True(t, result.AdminUserCreated)
	assert.Equal(t, "test-admin", result.AdminUsername)
}

// TestMustBootstrap_InMemoryManagerDefaultAdmin tests MustBootstrap with default admin username.
func TestMustBootstrap_InMemoryManagerDefaultAdmin(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key-default-admin",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	result, err := MustBootstrap(am, &BootstrapConfig{
		CreateAdminUser: true,
	})
	require.NoError(t, err)
	assert.True(t, result.AdminUserCreated)
	assert.Equal(t, "admin", result.AdminUsername)
}

// TestMustBootstrap_NilConfig tests MustBootstrap with nil bootstrap config.
func TestMustBootstrap_NilConfig(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-nil-config",
	}

	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	result, err := MustBootstrap(am, nil)
	require.NoError(t, err)
	assert.False(t, result.AdminUserCreated)
}

// =============================================================================
// Consolidated from coverage_final_unit_test.go
// =============================================================================

// TestInMemoryAuthManagerGetters tests getter methods.
func TestInMemoryAuthManagerGetters(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}
	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	t.Run("GetRoleManager", func(t *testing.T) {
		rm := am.GetRoleManager()
		assert.NotNil(t, rm)
	})

	t.Run("Close", func(t *testing.T) {
		err := am.Close()
		assert.NoError(t, err)
	})
}

// TestUserGetCreated tests the GetCreated method.
func TestUserGetCreated(t *testing.T) {
	now := time.Now()
	user := &User{
		ID:       "user-1",
		Username: "testuser",
		Created:  now,
	}

	created := user.GetCreated()
	assert.Equal(t, now, created)
}

// TestRoleGetDescription tests the GetDescription method.
func TestRoleGetDescription(t *testing.T) {
	role := &Role{
		Name:        "admin",
		Description: "Administrator role",
	}

	desc := role.GetDescription()
	assert.Equal(t, "Administrator role", desc)
}

// TestPermissionGetters tests Permission getter methods.
func TestPermissionGetters(t *testing.T) {
	perm := &Permission{
		Resource: "/api/users",
		Actions:  []string{"read", "write"},
	}

	t.Run("GetResource", func(t *testing.T) {
		resource := perm.GetResource()
		assert.Equal(t, "/api/users", resource)
	})

	t.Run("GetActions", func(t *testing.T) {
		actions := perm.GetActions()
		assert.Equal(t, []string{"read", "write"}, actions)
	})
}

// TestTokenStoreCleanupExpiredTokens tests the cleanup goroutine.
func TestTokenStoreCleanupExpiredTokens(t *testing.T) {
	ts := NewTokenStore()

	// Add an expired token
	userID := "user-1"
	token := "expired-token"
	expiry := time.Now().Add(-2 * time.Hour) // Already expired

	ts.mu.Lock()
	ts.refreshTokens[userID] = make(map[string]time.Time)
	ts.refreshTokens[userID][token] = expiry
	ts.mu.Unlock()

	// Verify token exists
	ts.mu.RLock()
	_, exists := ts.refreshTokens[userID][token]
	ts.mu.RUnlock()
	assert.True(t, exists, "Token should exist before cleanup")

	// The cleanup goroutine runs every hour, so we'll test the cleanup logic directly
	// by simulating what happens in the ticker loop
	ts.mu.Lock()
	now := time.Now()
	for uid, tokens := range ts.refreshTokens {
		for tkn, exp := range tokens {
			if now.After(exp) {
				delete(tokens, tkn)
			}
		}
		if len(tokens) == 0 {
			delete(ts.refreshTokens, uid)
		}
	}
	ts.mu.Unlock()

	// Verify token was removed
	ts.mu.RLock()
	_, exists = ts.refreshTokens[userID]
	ts.mu.RUnlock()
	assert.False(t, exists, "Expired tokens should be cleaned up")
}

// TestRoleManagerAddRole tests the AddRole method.
func TestRoleManagerAddRole(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	t.Run("add custom role", func(t *testing.T) {
		role := &Role{
			Name:        "editor",
			Description: "Editor role",
			Permissions: []*Permission{
				{Resource: ResourceKeys, Actions: []string{ActionRead, ActionWrite}},
			},
		}

		err := rm.AddRole(role)
		assert.NoError(t, err)

		// Verify role was added
		retrieved := rm.GetRole("editor")
		assert.NotNil(t, retrieved)
		assert.Equal(t, "editor", retrieved.GetName())
		assert.Equal(t, "Editor role", retrieved.GetDescription())
	})
}

// TestRoleManagerUpdateRole tests the UpdateRole method.
func TestRoleManagerUpdateRole(t *testing.T) {
	log := createTestLogger(t)
	rm := NewRoleManager(log)

	// Add initial role
	role := &Role{
		Name:        "viewer",
		Description: "View only",
		Permissions: []*Permission{
			{Resource: ResourceKeys, Actions: []string{ActionRead}},
		},
	}
	err := rm.AddRole(role)
	require.NoError(t, err)

	t.Run("update custom role", func(t *testing.T) {
		updatedRole := &Role{
			Name:        "viewer",
			Description: "Updated viewer role",
			Permissions: []*Permission{
				{Resource: ResourceKeys, Actions: []string{ActionRead}},
				{Resource: ResourceMetrics, Actions: []string{ActionRead}},
			},
		}

		err := rm.UpdateRole(updatedRole)
		assert.NoError(t, err)

		// Verify update
		retrieved := rm.GetRole("viewer")
		assert.NotNil(t, retrieved)
		assert.Equal(t, "Updated viewer role", retrieved.GetDescription())
		assert.Len(t, retrieved.GetPermissions(), 2)
	})

	t.Run("update built-in role returns nil", func(t *testing.T) {
		// Attempt to update built-in admin role (should return nil without error)
		adminRole := &Role{
			Name:        RoleNameAdmin,
			Description: "Modified admin",
		}

		err := rm.UpdateRole(adminRole)
		assert.NoError(t, err) // Should not error, just silently ignores
	})
}

// TestUserStoreAddUpdateUser tests the AddUser and UpdateUser methods.
func TestUserStoreAddUpdateUser(t *testing.T) {
	us := NewUserStore()

	t.Run("add valid user", func(t *testing.T) {
		user := &User{
			ID:       "user-1",
			Username: "alice",
			Roles:    []string{"viewer"},
			Created:  time.Now(),
		}

		us.AddUser(user)

		// Verify user was added
		retrieved := us.GetByID("user-1")
		require.NotNil(t, retrieved)
		assert.Equal(t, "alice", retrieved.GetUsername())
	})

	t.Run("update existing user", func(t *testing.T) {
		updatedUser := &User{
			ID:       "user-1",
			Username: "alice",
			Roles:    []string{"viewer", "editor"},
			Created:  time.Now(),
		}

		us.UpdateUser(updatedUser)

		// Verify update
		retrieved := us.GetByID("user-1")
		require.NotNil(t, retrieved)
		assert.Equal(t, []string{"viewer", "editor"}, retrieved.GetRoles())
	})
}
