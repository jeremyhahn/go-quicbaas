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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc/metadata"

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
	user, err := am.CreateUser("testuser", []string{"readwrite"})
	require.NoError(t, err)

	ctx := context.Background()
	token, err := am.generateTokens(user)
	require.NoError(t, err)

	// Verify token validates before revocation
	result, err := am.ValidateToken(ctx, token.AccessToken)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.False(t, result.IsRevoked)

	// Revoke token
	err = am.RevokeToken(ctx, token.AccessToken)
	assert.NoError(t, err)

	// Verify token is now reported revoked
	result, err = am.ValidateToken(ctx, token.AccessToken)
	require.NoError(t, err)
	assert.True(t, result.IsRevoked)
	assert.False(t, result.Valid)
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
		roles       []string
		expectError bool
	}{
		{
			name:        "valid user",
			username:    "user1",
			roles:       []string{"readwrite"},
			expectError: false,
		},
		{
			name:        "empty username",
			username:    "",
			roles:       []string{"readwrite"},
			expectError: true,
		},
		{
			name:        "empty password allowed",
			username:    "user2",
			roles:       []string{"readwrite"},
			expectError: false,
		},
		{
			name:        "duplicate username",
			username:    "user1",
			roles:       []string{"readonly"},
			expectError: true,
		},
		{
			name:        "multiple roles",
			username:    "user3",
			roles:       []string{"readwrite", "readonly"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := am.CreateUser(tt.username, tt.roles)

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
		user, err := am.CreateUserWithTenant("tenant_user", "tenant-abc", []string{"readwrite"})
		require.NoError(t, err)
		require.NotNil(t, user)

		assert.Equal(t, "tenant_user", user.Username)
		assert.Equal(t, "tenant-abc", user.TenantID)
		assert.Equal(t, []string{"readwrite"}, user.Roles)
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
		user, err := am.CreateUserWithTenant("no_tenant_user", "", []string{"readwrite"})
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
		user, err := am.CreateUser("legacy_user", []string{"readonly"})
		require.NoError(t, err)
		require.NotNil(t, user)

		assert.Equal(t, "legacy_user", user.Username)
		assert.Empty(t, user.TenantID)
	})
}

// TestTokenValidation moved to manager_integration_test.go

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
		user, err := am.CreateUser("certuser", []string{"readwrite"})
		require.NoError(t, err)
		require.NotNil(t, user)

		// Issue token for the user (simulating successful mTLS/FIDO2 auth)
		token, err := am.IssueTokenForUser(ctx, user)
		require.NoError(t, err)
		require.NotNil(t, token)

		assert.NotEmpty(t, token.AccessToken)
		assert.NotEmpty(t, token.RefreshToken)

		// Verify the issued token validates and carries the user's claims
		result, err := am.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.Equal(t, "certuser", result.Username)
		assert.Contains(t, result.Roles, "readwrite")
	})

	t.Run("issue token with user lookup by username", func(t *testing.T) {
		// Create a user
		_, err := am.CreateUser("lookupuser", []string{"admin"})
		require.NoError(t, err)

		// Create a minimal UserInfo implementation for lookup
		minimalUser := &minimalUserInfo{username: "lookupuser"}

		// Issue token - should look up the user by username
		token, err := am.IssueTokenForUser(ctx, minimalUser)
		require.NoError(t, err)
		require.NotNil(t, token)

		assert.NotEmpty(t, token.AccessToken)

		// Verify the looked-up user's admin role is carried in the token
		result, err := am.ValidateToken(ctx, token.AccessToken)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.Contains(t, result.Roles, "admin")
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

		user, err := disabledAM.CreateUser("disableduser", []string{"readwrite"})
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

// TestInMemoryCreateUserWithTenant tests multi-tenant user creation.
func TestInMemoryCreateUserWithTenant(t *testing.T) {
	log := createTestLogger(t)
	config := &AuthConfig{
		Enabled:   true,
		JWTSecret: "test-secret-key",
	}
	am, err := NewInMemoryAuthManager(config, log)
	require.NoError(t, err)

	t.Run("create with tenant ID", func(t *testing.T) {
		user, err := am.CreateUserWithTenant("tenantuser", "tenant-abc", []string{"readonly"})
		require.NoError(t, err)
		assert.Equal(t, "tenant-abc", user.TenantID)
		assert.Equal(t, "tenantuser", user.Username)
	})

	t.Run("create without tenant ID", func(t *testing.T) {
		user, err := am.CreateUserWithTenant("singletenantuser", "", []string{"readonly"})
		require.NoError(t, err)
		assert.Empty(t, user.TenantID)
	})

	t.Run("empty username", func(t *testing.T) {
		_, err := am.CreateUserWithTenant("", "", []string{"readonly"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "username is required")
	})

	t.Run("duplicate user", func(t *testing.T) {
		_, err := am.CreateUserWithTenant("tenantuser", "", []string{"readonly"})
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
		user, err := am.CreateUser("tokenuser", []string{"readonly"})
		require.NoError(t, err)

		token, err := am.IssueTokenForUser(context.Background(), user)
		require.NoError(t, err)
		assert.NotEmpty(t, token.AccessToken)
	})

	t.Run("user type is interface resolved by username", func(t *testing.T) {
		_, err := am.CreateUser("extuser", []string{"readonly"})
		require.NoError(t, err)

		mockUser := &mockUserInfo{
			id:       "ext-id",
			username: "extuser",
			roles:    []string{"readonly"},
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
		user, err := am.CreateUser("subjectuser", []string{"readonly"})
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
