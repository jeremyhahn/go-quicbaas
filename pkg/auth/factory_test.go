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

// Package auth provides tests for the unified auth factory
package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// mockStorage is a mock storage for testing (not memory storage).
type mockStorage struct{}

func (m *mockStorage) Get(key []byte) ([]byte, error) { return nil, nil }
func (m *mockStorage) Set(key, value []byte) error    { return nil }
func (m *mockStorage) Delete(key []byte) error        { return nil }
func (m *mockStorage) GetWithContext(ctx context.Context, key []byte) ([]byte, error) {
	return nil, nil
}
func (m *mockStorage) SetWithContext(ctx context.Context, key, value []byte) error { return nil }
func (m *mockStorage) Batch() storage.Batch                                        { return nil }
func (m *mockStorage) NewIterator(opts *storage.IteratorOptions) storage.Iterator  { return nil }
func (m *mockStorage) Snapshot() (storage.Snapshot, error)                         { return nil, nil }
func (m *mockStorage) Compact(start, end []byte) error                             { return nil }
func (m *mockStorage) Flush() error                                                { return nil }
func (m *mockStorage) Close() error                                                { return nil }

func TestNewAuthManager(t *testing.T) {
	log := createTestLogger(t)

	tests := []struct {
		name           string
		config         *AuthConfig
		storage        interfaces.Storage
		expectError    bool
		expectInMemory bool
	}{
		{
			name:        "nil config should error",
			config:      nil,
			storage:     nil,
			expectError: true,
		},
		{
			name:           "nil storage creates in-memory auth",
			config:         &AuthConfig{Enabled: true, JWTSecret: "test-secret"},
			storage:        nil,
			expectError:    false,
			expectInMemory: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authManager, err := NewAuthManager(tt.config, tt.storage, log)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, authManager)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, authManager)

				// Check type
				if tt.expectInMemory {
					_, ok := authManager.(*InMemoryAuthManager)
					assert.True(t, ok, "Expected InMemoryAuthManager")
				} else {
					_, ok := authManager.(*PersistentAuthManager)
					assert.True(t, ok, "Expected PersistentAuthManager")
				}
			}
		})
	}
}

func TestNewAuthManagerWithNilLogger(t *testing.T) {
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret",
	}

	authManager, err := NewAuthManager(config, nil, nil)
	assert.Error(t, err)
	assert.Nil(t, authManager)
	assert.Contains(t, err.Error(), "logger cannot be nil")
}

func TestIsMemoryOnlyMode(t *testing.T) {
	tests := []struct {
		name       string
		storage    interfaces.Storage
		expectTrue bool
	}{
		{
			name:       "nil storage is memory-only",
			storage:    nil,
			expectTrue: true,
		},
		{
			name:       "non-memory storage is not memory-only",
			storage:    &mockStorage{},
			expectTrue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsMemoryOnlyMode(tt.storage)
			assert.Equal(t, tt.expectTrue, result)
		})
	}
}

func TestMustBootstrapInMemory(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	tests := []struct {
		name            string
		bootstrapConfig *BootstrapConfig
		expectError     bool
		expectAdmin     bool
	}{
		{
			name: "create admin user",
			bootstrapConfig: &BootstrapConfig{
				CreateAdminUser: true,
				AdminUsername:   "admin",
			},
			expectError: false,
			expectAdmin: true,
		},
		{
			name: "no admin creation",
			bootstrapConfig: &BootstrapConfig{
				CreateAdminUser: false,
			},
			expectError: false,
			expectAdmin: false,
		},
		{
			name: "admin with custom username",
			bootstrapConfig: &BootstrapConfig{
				CreateAdminUser: true,
				AdminUsername:   "admin2",
			},
			expectError: false,
			expectAdmin: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh manager for each test
			am, err := NewInMemoryAuthManager(config, log)
			require.NoError(t, err)

			result, err := MustBootstrap(am, tt.bootstrapConfig)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectAdmin, result.AdminUserCreated)

				if tt.expectAdmin {
					assert.NotEmpty(t, result.AdminUsername)
					assert.NotEmpty(t, result.AdminUserID)
					assert.Contains(t, result.Message, "In-memory")
				}
			}
		})
	}
}

func TestMustBootstrapUnknownType(t *testing.T) {
	// Pass something that's not an auth manager
	_, err := MustBootstrap("not-an-auth-manager", &BootstrapConfig{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth manager type")
}

func TestInMemoryAuthManagerMethods(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}

	// Create auth manager via factory
	authManager, err := NewAuthManager(config, nil, log)
	require.NoError(t, err)

	// Bootstrap with admin
	result, err := MustBootstrap(authManager, &BootstrapConfig{
		CreateAdminUser: true,
		AdminUsername:   "admin",
	})
	require.NoError(t, err)
	require.True(t, result.AdminUserCreated)

	// Type assert to access methods
	am, ok := authManager.(*InMemoryAuthManager)
	require.True(t, ok)

	// Get admin user and generate token directly (certificate auth is used)
	adminUserInfo := am.userStore.GetByUsername("admin")
	require.NotNil(t, adminUserInfo)

	// Type assert UserInfo to *User for generateTokens
	adminUser, ok := adminUserInfo.(*User)
	require.True(t, ok, "UserInfo should be a *User")

	ctx := context.Background()
	token, err := am.generateTokens(adminUser)
	require.NoError(t, err)
	assert.NotEmpty(t, token.AccessToken)
	assert.NotEmpty(t, token.RefreshToken)

	// Test authorization
	err = am.Authorize(ctx, token.AccessToken, ResourceKeys, ActionRead)
	assert.NoError(t, err)

	// Test create user (password is ignored)
	user, err := am.CreateUser("testuser", []string{RoleNameReadWrite})
	require.NoError(t, err)
	assert.Equal(t, "testuser", user.Username)

	// Test refresh token
	newToken, err := am.RefreshToken(ctx, token.RefreshToken)
	require.NoError(t, err)
	assert.NotEmpty(t, newToken.AccessToken)

	// Test revoke token
	err = am.RevokeToken(ctx, token.AccessToken)
	assert.NoError(t, err)

	// Verify token is revoked
	err = am.Authorize(ctx, token.AccessToken, ResourceKeys, ActionRead)
	assert.Error(t, err)
}

func TestFactoryDetectsStorageType(t *testing.T) {
	// Test that isMemoryStorage correctly identifies storage types
	assert.True(t, isMemoryStorage(nil), "nil should be memory storage")
	assert.False(t, isMemoryStorage(&mockStorage{}), "mockStorage should not be memory storage")
}
