package iam

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestMemoryStorage_TenantOperations(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	// Test CreateTenant
	tenant := &Tenant{
		ID:      "test-tenant",
		Name:    "Test Tenant",
		Created: time.Now(),
		Updated: time.Now(),
		Quotas: TenantQuotas{
			MaxPolicies: 50, // MaxUsers is engine-level, owned by qrdb TenantServicer
		},
	}

	err := storage.CreateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Test CreateTenant duplicate
	err = storage.CreateTenant(ctx, tenant)
	if err != ErrTenantAlreadyExists {
		t.Errorf("Expected ErrTenantAlreadyExists, got: %v", err)
	}

	// Test GetTenant
	retrieved, err := storage.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant failed: %v", err)
	}
	if retrieved.ID != tenant.ID || retrieved.Name != tenant.Name {
		t.Errorf("Retrieved tenant does not match: %+v", retrieved)
	}

	// Test GetTenant not found
	_, err = storage.GetTenant(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent tenant")
	}

	// Test UpdateTenant
	tenant.Name = "Updated Tenant"
	err = storage.UpdateTenant(ctx, tenant)
	if err != nil {
		t.Fatalf("UpdateTenant failed: %v", err)
	}

	retrieved, _ = storage.GetTenant(ctx, tenant.ID)
	if retrieved.Name != "Updated Tenant" {
		t.Errorf("Tenant name not updated: %s", retrieved.Name)
	}

	// Test ListTenants
	tenants, err := storage.ListTenants(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}
	if len(tenants) != 1 {
		t.Errorf("Expected 1 tenant, got %d", len(tenants))
	}

	// Test CountTenants
	count, err := storage.CountTenants(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("CountTenants failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count=1, got %d", count)
	}

	// Test TenantExists
	exists, err := storage.TenantExists(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("TenantExists failed: %v", err)
	}
	if !exists {
		t.Error("Expected tenant to exist")
	}

	// Test DeleteTenant
	err = storage.DeleteTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("DeleteTenant failed: %v", err)
	}

	exists, _ = storage.TenantExists(ctx, tenant.ID)
	if exists {
		t.Error("Tenant should have been deleted")
	}
}

func TestMemoryStorage_QuotaOperations(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	tenant := &Tenant{
		ID:      "quota-tenant",
		Name:    "Quota Test",
		Created: time.Now(),
		Updated: time.Now(),
	}
	storage.CreateTenant(ctx, tenant)

	// Use BaaS-layer IAM quota (policies); users are engine-level owned by qrdb
	quota := &TenantQuota{
		MaxPolicies: 5,
	}
	err := storage.UpdateQuota(ctx, tenant.ID, quota)
	if err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Test quota increment within limits
	err = storage.IncrementQuotaUsage(ctx, tenant.ID, "policies", 3)
	if err != nil {
		t.Fatalf("IncrementQuotaUsage failed: %v", err)
	}

	retrieved, _ := storage.GetQuota(ctx, tenant.ID)
	if retrieved.CurrentPolicies != 3 {
		t.Errorf("Expected CurrentPolicies=3, got %d", retrieved.CurrentPolicies)
	}

	// Test quota exceeded
	err = storage.IncrementQuotaUsage(ctx, tenant.ID, "policies", 10)
	if err == nil {
		t.Error("Expected quota exceeded error")
	}

	// Test quota decrement
	err = storage.DecrementQuotaUsage(ctx, tenant.ID, "policies", 2)
	if err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	retrieved, _ = storage.GetQuota(ctx, tenant.ID)
	if retrieved.CurrentPolicies != 1 {
		t.Errorf("Expected CurrentPolicies=1, got %d", retrieved.CurrentPolicies)
	}
}

func TestMemoryStorage_UserOperations(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	// Create tenant first
	tenant := &Tenant{ID: "user-tenant", Name: "User Test", Created: time.Now(), Updated: time.Now()}
	storage.CreateTenant(ctx, tenant)

	user := &User{
		ID:       "11",
		TenantID: tenant.ID,
		Email:    "user1@example.com",
		Name:     "User One",
		Created:  time.Now(),
		Updated:  time.Now(),
	}

	// Test CreateUser
	err := storage.CreateUser(ctx, user)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Test CreateUser duplicate
	err = storage.CreateUser(ctx, user)
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) || qbErr.Code != dberrors.ErrAlreadyExists {
		t.Errorf("Expected ErrAlreadyExists, got: %v", err)
	}

	// Test GetUser
	retrieved, err := storage.GetUser(ctx, user.TenantID, user.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if retrieved.Email != user.Email {
		t.Errorf("Expected email %s, got %s", user.Email, retrieved.Email)
	}

	// Test GetUserByEmail
	retrieved, err = storage.GetUserByEmail(ctx, user.TenantID, user.Email)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if retrieved.ID != user.ID {
		t.Errorf("Expected user ID %s, got %s", user.ID, retrieved.ID)
	}

	// Test UpdateUser
	user.Name = "Updated Name"
	err = storage.UpdateUser(ctx, user)
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	retrieved, _ = storage.GetUser(ctx, user.TenantID, user.ID)
	if retrieved.Name != "Updated Name" {
		t.Errorf("User name not updated: %s", retrieved.Name)
	}

	// Test ListUsers
	users, err := storage.ListUsers(ctx, tenant.ID, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != 1 {
		t.Errorf("Expected 1 user, got %d", len(users))
	}

	// Test DeleteUser
	err = storage.DeleteUser(ctx, user.TenantID, user.ID)
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	_, err = storage.GetUser(ctx, user.TenantID, user.ID)
	if err == nil {
		t.Error("Expected error for deleted user")
	}
}

func TestMemoryStorage_RoleOperations(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	// Create tenant first
	tenant := &Tenant{ID: "role-tenant", Name: "Role Test", Created: time.Now(), Updated: time.Now()}
	storage.CreateTenant(ctx, tenant)

	role := &Role{
		ID:          "role1",
		TenantID:    tenant.ID,
		Name:        "Role One",
		Permissions: []string{"read", "write"},
		Created:     time.Now(),
		Updated:     time.Now(),
	}

	// Test CreateRole
	err := storage.CreateRole(ctx, role)
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	// Test GetRole
	retrieved, err := storage.GetRole(ctx, role.TenantID, role.ID)
	if err != nil {
		t.Fatalf("GetRole failed: %v", err)
	}
	if retrieved.Name != role.Name {
		t.Errorf("Expected role name %s, got %s", role.Name, retrieved.Name)
	}

	// Test UpdateRole
	role.Name = "Updated Role"
	err = storage.UpdateRole(ctx, role)
	if err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}

	retrieved, _ = storage.GetRole(ctx, role.TenantID, role.ID)
	if retrieved.Name != "Updated Role" {
		t.Errorf("Role name not updated: %s", retrieved.Name)
	}

	// Test ListRoles
	roles, err := storage.ListRoles(ctx, tenant.ID, ListOptions{})
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}
	if len(roles) != 1 {
		t.Errorf("Expected 1 role, got %d", len(roles))
	}

	// Test DeleteRole
	err = storage.DeleteRole(ctx, role.TenantID, role.ID)
	if err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}

	_, err = storage.GetRole(ctx, role.TenantID, role.ID)
	if err == nil {
		t.Error("Expected error for deleted role")
	}
}

func TestMemoryStorage_AppOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage := NewMemoryStorage()

	// Create tenant first
	tenant := &Tenant{ID: "app-tenant", Name: "App Test", Created: time.Now(), Updated: time.Now()}
	storage.CreateTenant(ctx, tenant)

	// Seed the current PBKDF2 credential form (#37); this test's subject is
	// the app CRUD path, not the legacy digest.
	plaintextSecret := "secret123"
	hashedSecret := currentSecretHash(t, plaintextSecret)

	app := &Application{
		ClientID:      "client123",
		ClientSecret:  hashedSecret,
		Name:          "Test App",
		TenantID:      tenant.ID,
		RedirectURIs:  []string{"https://example.com/callback"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
		Created:       time.Now(),
		Updated:       time.Now(),
	}

	// Test CreateApp
	err := storage.CreateApp(ctx, app)
	if err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Test GetApp
	retrieved, err := storage.GetApp(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("GetApp failed: %v", err)
	}
	if retrieved.Name != app.Name {
		t.Errorf("Expected app name %s, got %s", app.Name, retrieved.Name)
	}

	// Test ValidateClientSecret
	valid, err := storage.ValidateClientSecret(ctx, app.ClientID, plaintextSecret)
	if err != nil {
		t.Fatalf("ValidateClientSecret failed: %v", err)
	}
	if !valid {
		t.Error("Expected valid client secret")
	}

	valid, _ = storage.ValidateClientSecret(ctx, app.ClientID, "wrong-secret")
	if valid {
		t.Error("Expected invalid client secret")
	}

	// Test UpdateApp
	app.Name = "Updated App"
	err = storage.UpdateApp(ctx, app)
	if err != nil {
		t.Fatalf("UpdateApp failed: %v", err)
	}

	// Test ListApps
	apps, err := storage.ListApps(ctx, tenant.ID, ListOptions{})
	if err != nil {
		t.Fatalf("ListApps failed: %v", err)
	}
	if len(apps) != 1 {
		t.Errorf("Expected 1 app, got %d", len(apps))
	}

	// Test DeleteApp
	err = storage.DeleteApp(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("DeleteApp failed: %v", err)
	}

	_, err = storage.GetApp(ctx, app.ClientID)
	if err == nil {
		t.Error("Expected error for deleted app")
	}
}

func TestMemoryStorage_TokenOperations(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	token := &Token{
		JTI:       "jti123",
		Subject:   "user1",
		TenantID:  "tenant1",
		Scopes:    []string{"read", "write"},
		Expires:   time.Now().Add(1 * time.Hour),
		Issued:    time.Now(),
		TokenType: TokenTypeAccess,
	}

	// Test StoreToken
	err := storage.StoreToken(ctx, token)
	if err != nil {
		t.Fatalf("StoreToken failed: %v", err)
	}

	// Test GetToken
	retrieved, err := storage.GetToken(ctx, token.JTI)
	if err != nil {
		t.Fatalf("GetToken failed: %v", err)
	}
	if retrieved.Subject != token.Subject {
		t.Errorf("Expected subject %s, got %s", token.Subject, retrieved.Subject)
	}

	// Test RevokeToken
	err = storage.RevokeToken(ctx, token.JTI)
	if err != nil {
		t.Fatalf("RevokeToken failed: %v", err)
	}

	// Test IsTokenRevoked
	revoked, err := storage.IsTokenRevoked(ctx, token.JTI)
	if err != nil {
		t.Fatalf("IsTokenRevoked failed: %v", err)
	}
	if !revoked {
		t.Error("Expected token to be revoked")
	}

	// Test ListTokensByUser
	tokens, err := storage.ListTokensByUser(ctx, "user1")
	if err != nil {
		t.Fatalf("ListTokensByUser failed: %v", err)
	}
	if len(tokens) != 1 {
		t.Errorf("Expected 1 token, got %d", len(tokens))
	}

	// Test RevokeUserTokens
	token2 := &Token{
		JTI:       "jti456",
		Subject:   "user1",
		TenantID:  "tenant1",
		TokenType: TokenTypeRefresh,
		Expires:   time.Now().Add(24 * time.Hour),
		Issued:    time.Now(),
	}
	storage.StoreToken(ctx, token2)

	err = storage.RevokeUserTokens(ctx, "user1")
	if err != nil {
		t.Fatalf("RevokeUserTokens failed: %v", err)
	}

	revoked, _ = storage.IsTokenRevoked(ctx, token2.JTI)
	if !revoked {
		t.Error("Expected all user tokens to be revoked")
	}
}

func TestMemoryStorage_TokenExpiration(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	expiredToken := &Token{
		JTI:       "expired123",
		Subject:   "user1",
		TenantID:  "tenant1",
		Expires:   time.Now().Add(-1 * time.Hour), // Already expired
		Issued:    time.Now().Add(-2 * time.Hour),
		TokenType: TokenTypeAccess,
	}

	storage.StoreToken(ctx, expiredToken)

	// Test DeleteExpiredTokens
	err := storage.DeleteExpiredTokens(ctx)
	if err != nil {
		t.Fatalf("DeleteExpiredTokens failed: %v", err)
	}

	// Verify expired token was deleted
	_, err = storage.GetToken(ctx, expiredToken.JTI)
	if err == nil {
		t.Error("Expected error for expired token that should have been deleted")
	}
}

func TestMemoryStorage_Concurrency(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	// Create tenant first
	tenant := &Tenant{ID: "concurrent-tenant", Name: "Concurrent Test", Created: time.Now(), Updated: time.Now()}
	storage.CreateTenant(ctx, tenant)

	// Test concurrent user creation
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			user := &User{
				ID:       FormatUserID(uint64(id) + 1),
				TenantID: tenant.ID,
				Email:    fmt.Sprintf("user%d@example.com", id),
				Name:     fmt.Sprintf("User %d", id),
				Created:  time.Now(),
				Updated:  time.Now(),
			}
			if err := storage.CreateUser(ctx, user); err != nil {
				t.Errorf("CreateUser failed: %v", err)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all users were created
	users, err := storage.ListUsers(ctx, tenant.ID, ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != 10 {
		t.Errorf("Expected 10 users, got %d", len(users))
	}
}

// TestMemoryStorage_GetAppByTenant tests the GetAppByTenant function
func TestMemoryStorage_GetAppByTenant(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	// Create tenant
	tenant := &Tenant{ID: "tenant1", Name: "Tenant 1", Created: time.Now(), Updated: time.Now()}
	storage.CreateTenant(ctx, tenant)

	tenant2 := &Tenant{ID: "tenant2", Name: "Tenant 2", Created: time.Now(), Updated: time.Now()}
	storage.CreateTenant(ctx, tenant2)

	// Create app
	app := &Application{
		ClientID:      "client123",
		ClientSecret:  "secret123",
		Name:          "Test App",
		TenantID:      tenant.ID,
		RedirectURIs:  []string{"https://example.com/callback"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
		Created:       time.Now(),
		Updated:       time.Now(),
	}
	storage.CreateApp(ctx, app)

	// Test happy path - correct tenant
	retrieved, err := storage.GetAppByTenant(ctx, tenant.ID, app.ClientID)
	if err != nil {
		t.Fatalf("GetAppByTenant failed: %v", err)
	}
	if retrieved.ClientID != app.ClientID {
		t.Errorf("Expected clientID %s, got %s", app.ClientID, retrieved.ClientID)
	}
	if retrieved.TenantID != tenant.ID {
		t.Errorf("Expected tenantID %s, got %s", tenant.ID, retrieved.TenantID)
	}

	// Test error path - wrong tenant
	_, err = storage.GetAppByTenant(ctx, tenant2.ID, app.ClientID)
	if err == nil {
		t.Error("Expected error when app belongs to different tenant")
	}
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) || qbErr.Code != dberrors.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got: %v", err)
	}

	// Test error path - app doesn't exist
	_, err = storage.GetAppByTenant(ctx, tenant.ID, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent app")
	}
	if !errors.As(err, &qbErr) || qbErr.Code != dberrors.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got: %v", err)
	}
}

// TestMemoryStorage_StoreAuthCode tests the StoreAuthCode function
func TestMemoryStorage_StoreAuthCode(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	authCode := &AuthCodeData{
		Code:                "code123",
		UserID:              "user1",
		TenantID:            "tenant1",
		ClientID:            "client1",
		RedirectURI:         "https://example.com/callback",
		Scope:               []string{"openid", "profile"},
		CodeChallenge:       "challenge123",
		CodeChallengeMethod: "S256",
		Nonce:               "nonce123",
		ExpiresAt:           time.Now().Add(10 * time.Minute),
		CreatedAt:           time.Now(),
	}

	// Test storing auth code
	err := storage.StoreAuthCode(ctx, authCode)
	if err != nil {
		t.Fatalf("StoreAuthCode failed: %v", err)
	}

	// Verify it was stored by retrieving it
	retrieved, err := storage.GetAuthCode(ctx, authCode.Code)
	if err != nil {
		t.Fatalf("GetAuthCode failed after StoreAuthCode: %v", err)
	}
	if retrieved.Code != authCode.Code {
		t.Errorf("Expected code %s, got %s", authCode.Code, retrieved.Code)
	}
	if retrieved.UserID != authCode.UserID {
		t.Errorf("Expected userID %s, got %s", authCode.UserID, retrieved.UserID)
	}
	if retrieved.ClientID != authCode.ClientID {
		t.Errorf("Expected clientID %s, got %s", authCode.ClientID, retrieved.ClientID)
	}
}

// TestMemoryStorage_GetAuthCode tests the GetAuthCode function
func TestMemoryStorage_GetAuthCode(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	authCode := &AuthCodeData{
		Code:      "code456",
		UserID:    "user2",
		TenantID:  "tenant2",
		ClientID:  "client2",
		ExpiresAt: time.Now().Add(10 * time.Minute),
		CreatedAt: time.Now(),
	}
	storage.StoreAuthCode(ctx, authCode)

	// Test happy path - code exists
	retrieved, err := storage.GetAuthCode(ctx, authCode.Code)
	if err != nil {
		t.Fatalf("GetAuthCode failed: %v", err)
	}
	if retrieved.Code != authCode.Code {
		t.Errorf("Expected code %s, got %s", authCode.Code, retrieved.Code)
	}

	// Test error path - code doesn't exist
	_, err = storage.GetAuthCode(ctx, "nonexistent")
	if err == nil {
		t.Error("Expected error for nonexistent auth code")
	}
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) || qbErr.Code != dberrors.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got: %v", err)
	}
}

// TestMemoryStorage_DeleteAuthCode tests the DeleteAuthCode function
func TestMemoryStorage_DeleteAuthCode(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	authCode := &AuthCodeData{
		Code:      "code789",
		UserID:    "user3",
		TenantID:  "tenant3",
		ClientID:  "client3",
		ExpiresAt: time.Now().Add(10 * time.Minute),
		CreatedAt: time.Now(),
	}
	storage.StoreAuthCode(ctx, authCode)

	// Test deleting existing auth code
	err := storage.DeleteAuthCode(ctx, authCode.Code)
	if err != nil {
		t.Fatalf("DeleteAuthCode failed: %v", err)
	}

	// Verify it was deleted
	_, err = storage.GetAuthCode(ctx, authCode.Code)
	if err == nil {
		t.Error("Expected error for deleted auth code")
	}

	// Test deleting nonexistent auth code (should not error)
	err = storage.DeleteAuthCode(ctx, "nonexistent")
	if err != nil {
		t.Errorf("DeleteAuthCode should not error for nonexistent code: %v", err)
	}
}

// TestMemoryStorage_GetTokenByValue tests the GetTokenByValue function
func TestMemoryStorage_GetTokenByValue(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	// Test with refresh token (indexed in tokenValues by the hash of the
	// opaque secret the client presents, not by JTI).
	const secret = "opaque-refresh-secret"
	refreshToken := &Token{
		JTI:       "refresh-jti-123",
		Subject:   "user1",
		TenantID:  "tenant1",
		Scopes:    []string{"offline_access"},
		Expires:   time.Now().Add(24 * time.Hour),
		Issued:    time.Now(),
		TokenType: TokenTypeRefresh,
		ValueHash: HashTokenValue(secret),
	}
	storage.StoreToken(ctx, refreshToken)

	// Test happy path - token exists
	retrieved, err := storage.GetTokenByValue(ctx, secret)
	if err != nil {
		t.Fatalf("GetTokenByValue failed: %v", err)
	}
	if retrieved.JTI != refreshToken.JTI {
		t.Errorf("Expected JTI %s, got %s", refreshToken.JTI, retrieved.JTI)
	}
	if retrieved.TokenType != TokenTypeRefresh {
		t.Errorf("Expected TokenType %s, got %s", TokenTypeRefresh, retrieved.TokenType)
	}

	// Test error path - token doesn't exist
	_, err = storage.GetTokenByValue(ctx, "nonexistent-token-value")
	if err == nil {
		t.Error("Expected error for nonexistent token value")
	}
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) || qbErr.Code != dberrors.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got: %v", err)
	}

	// Test with access token (not stored in tokenValues)
	accessToken := &Token{
		JTI:       "access-jti-456",
		Subject:   "user2",
		TenantID:  "tenant2",
		Scopes:    []string{"read", "write"},
		Expires:   time.Now().Add(1 * time.Hour),
		Issued:    time.Now(),
		TokenType: TokenTypeAccess,
	}
	storage.StoreToken(ctx, accessToken)

	// Access tokens are not stored by value, so this should return not found
	_, err = storage.GetTokenByValue(ctx, accessToken.JTI)
	if err == nil {
		t.Error("Expected error for access token lookup by value")
	}
}

// TestMemoryStorage_IncrementQuotaUsage_Comprehensive tests all resource types
func TestMemoryStorage_IncrementQuotaUsage_Comprehensive(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	tenant := &Tenant{
		ID:      "quota-tenant",
		Name:    "Quota Test",
		Created: time.Now(),
		Updated: time.Now(),
	}
	storage.CreateTenant(ctx, tenant)

	// BaaS-layer IAM quotas only; users and roles are engine-level (qrdb-owned).
	quota := &TenantQuota{
		MaxPolicies: 8,
		MaxApps:     4,
	}
	storage.UpdateQuota(ctx, tenant.ID, quota)

	tests := []struct {
		name         string
		resource     string
		delta        int64
		expectError  bool
		expectedUsed int64
	}{
		{"users no-op (engine-level)", "users", 5, false, 0}, // no-op: engine-owned
		{"roles no-op (engine-level)", "roles", 2, false, 0}, // no-op: engine-owned
		{"policies within limit", "policies", 4, false, 4},
		{"apps within limit", "apps", 2, false, 2},
		{"policies exceed limit", "policies", 5, true, 4}, // Would go to 9, exceeds 8
		{"unknown resource", "unknown", 1, false, 0},      // Unknown resources don't trigger quota
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.IncrementQuotaUsage(ctx, tenant.ID, tt.resource, tt.delta)
			if tt.expectError && err == nil {
				t.Error("Expected quota exceeded error")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			// Verify the current usage for BaaS-layer resources
			if !tt.expectError && tt.resource != "unknown" && tt.resource != "users" && tt.resource != "roles" {
				retrieved, _ := storage.GetQuota(ctx, tenant.ID)
				var currentUsage int64
				switch tt.resource {
				case "policies":
					currentUsage = retrieved.CurrentPolicies
				case "apps":
					currentUsage = retrieved.CurrentApps
				}
				if currentUsage != tt.expectedUsed {
					t.Errorf("Expected %s usage %d, got %d", tt.resource, tt.expectedUsed, currentUsage)
				}
			}
		})
	}
}

// TestMemoryStorage_IncrementQuotaUsage_NoQuota tests incrementing when quota doesn't exist
func TestMemoryStorage_IncrementQuotaUsage_NoQuota(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	tenant := &Tenant{
		ID:      "no-quota-tenant",
		Name:    "No Quota Test",
		Created: time.Now(),
		Updated: time.Now(),
	}
	storage.CreateTenant(ctx, tenant)

	// Don't set any quota; increment a BaaS-layer resource (policies)
	err := storage.IncrementQuotaUsage(ctx, tenant.ID, "policies", 5)
	if err != nil {
		t.Fatalf("IncrementQuotaUsage failed when quota doesn't exist: %v", err)
	}

	retrieved, _ := storage.GetQuota(ctx, tenant.ID)
	if retrieved.CurrentPolicies != 5 {
		t.Errorf("Expected CurrentPolicies=5, got %d", retrieved.CurrentPolicies)
	}
}

// TestMemoryStorage_DecrementQuotaUsage_Comprehensive tests all resource types
func TestMemoryStorage_DecrementQuotaUsage_Comprehensive(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	tenant := &Tenant{
		ID:      "decrement-tenant",
		Name:    "Decrement Test",
		Created: time.Now(),
		Updated: time.Now(),
	}
	storage.CreateTenant(ctx, tenant)

	// BaaS-layer IAM quotas only; users and roles are engine-level (qrdb-owned).
	quota := &TenantQuota{
		MaxPolicies:     8,
		CurrentPolicies: 6,
		MaxApps:         4,
		CurrentApps:     3,
	}
	storage.UpdateQuota(ctx, tenant.ID, quota)

	tests := []struct {
		name         string
		resource     string
		delta        int64
		expectedUsed int64
	}{
		{"users no-op (engine-level)", "users", 3, 0},
		{"roles no-op (engine-level)", "roles", 1, 0},
		{"policies decrement", "policies", 4, 2},
		{"apps decrement", "apps", 2, 1},
		{"policies decrement below zero", "policies", 10, 0}, // Clamped to 0
		{"unknown resource", "unknown", 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.DecrementQuotaUsage(ctx, tenant.ID, tt.resource, tt.delta)
			if err != nil {
				t.Errorf("DecrementQuotaUsage failed: %v", err)
			}

			if tt.resource != "unknown" && tt.resource != "users" && tt.resource != "roles" {
				retrieved, _ := storage.GetQuota(ctx, tenant.ID)
				var currentUsage int64
				switch tt.resource {
				case "policies":
					currentUsage = retrieved.CurrentPolicies
				case "apps":
					currentUsage = retrieved.CurrentApps
				}
				if currentUsage != tt.expectedUsed {
					t.Errorf("Expected %s usage %d, got %d", tt.resource, tt.expectedUsed, currentUsage)
				}
			}
		})
	}
}

// TestMemoryStorage_DecrementQuotaUsage_NoQuota tests decrementing when quota doesn't exist
func TestMemoryStorage_DecrementQuotaUsage_NoQuota(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	tenant := &Tenant{
		ID:      "no-quota-dec-tenant",
		Name:    "No Quota Decrement Test",
		Created: time.Now(),
		Updated: time.Now(),
	}
	storage.CreateTenant(ctx, tenant)

	// Don't set any quota, should not error (no-op for engine-level resources)
	err := storage.DecrementQuotaUsage(ctx, tenant.ID, "policies", 5)
	if err != nil {
		t.Errorf("DecrementQuotaUsage should not error when quota doesn't exist: %v", err)
	}

	// Verify quota was not created (or current policies is still 0)
	retrieved, _ := storage.GetQuota(ctx, tenant.ID)
	if retrieved.CurrentPolicies != 0 {
		t.Errorf("Expected CurrentPolicies=0 (quota should not exist), got %d", retrieved.CurrentPolicies)
	}
}

// TestMemoryStorage_AuthCodeExpiration tests auth code expiration handling
func TestMemoryStorage_AuthCodeExpiration(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	// Create an expired auth code
	expiredAuthCode := &AuthCodeData{
		Code:      "expired-code",
		UserID:    "user1",
		TenantID:  "tenant1",
		ClientID:  "client1",
		ExpiresAt: time.Now().Add(-5 * time.Minute), // Expired 5 minutes ago
		CreatedAt: time.Now().Add(-15 * time.Minute),
	}
	storage.StoreAuthCode(ctx, expiredAuthCode)

	// Create a valid auth code
	validAuthCode := &AuthCodeData{
		Code:      "valid-code",
		UserID:    "user2",
		TenantID:  "tenant2",
		ClientID:  "client2",
		ExpiresAt: time.Now().Add(10 * time.Minute),
		CreatedAt: time.Now(),
	}
	storage.StoreAuthCode(ctx, validAuthCode)

	// Retrieve expired code (should still exist in storage until explicitly deleted)
	retrieved, err := storage.GetAuthCode(ctx, expiredAuthCode.Code)
	if err != nil {
		t.Errorf("Expired auth code should still be retrievable: %v", err)
	}
	if retrieved != nil && !retrieved.IsExpired() {
		t.Error("Retrieved auth code should be marked as expired")
	}

	// Retrieve valid code
	retrieved, err = storage.GetAuthCode(ctx, validAuthCode.Code)
	if err != nil {
		t.Fatalf("Valid auth code should be retrievable: %v", err)
	}
	if retrieved.IsExpired() {
		t.Error("Valid auth code should not be expired")
	}
}

// TestMemoryStorage_GetAppByTenant_EdgeCases tests edge cases for GetAppByTenant
func TestMemoryStorage_GetAppByTenant_EdgeCases(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	tenant := &Tenant{ID: "edge-tenant", Name: "Edge Tenant", Created: time.Now(), Updated: time.Now()}
	storage.CreateTenant(ctx, tenant)

	// Test with empty tenant ID
	_, err := storage.GetAppByTenant(ctx, "", "client123")
	if err == nil {
		t.Error("Expected error with empty tenant ID")
	}

	// Test with empty client ID
	_, err = storage.GetAppByTenant(ctx, tenant.ID, "")
	if err == nil {
		t.Error("Expected error with empty client ID")
	}

	// Create app and verify isolation
	app := &Application{
		ClientID:     "isolated-client",
		Name:         "Isolated App",
		TenantID:     tenant.ID,
		RedirectURIs: []string{"https://example.com/callback"},
		Created:      time.Now(),
		Updated:      time.Now(),
	}
	storage.CreateApp(ctx, app)

	// Verify app is accessible with correct tenant
	retrieved, err := storage.GetAppByTenant(ctx, tenant.ID, app.ClientID)
	if err != nil {
		t.Fatalf("Failed to retrieve app with correct tenant: %v", err)
	}
	if retrieved.ClientID != app.ClientID {
		t.Errorf("Expected clientID %s, got %s", app.ClientID, retrieved.ClientID)
	}
}

// TestMemoryStorage_GetSettings tests the GetSettings function
func TestMemoryStorage_GetSettings(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	tenant := &Tenant{
		ID:      "settings-tenant",
		Name:    "Settings Test",
		Created: time.Now(),
		Updated: time.Now(),
	}
	storage.CreateTenant(ctx, tenant)

	// Test getting settings when none exist - should return empty settings
	settings, err := storage.GetSettings(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetSettings failed when no settings exist: %v", err)
	}
	if settings == nil {
		t.Fatal("GetSettings returned nil settings")
	}

	// Create settings
	testSettings := &TenantSettings{
		MaxUsers:           100,
		MFARequired:        true,
		SessionTimeout:     time.Hour,
		SessionIdleTimeout: 30 * time.Minute,
		TokenTTL:           15 * time.Minute,
	}
	err = storage.UpdateSettings(ctx, tenant.ID, testSettings)
	if err != nil {
		t.Fatalf("UpdateSettings failed: %v", err)
	}

	// Test getting existing settings
	retrieved, err := storage.GetSettings(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	if retrieved.MaxUsers != testSettings.MaxUsers {
		t.Errorf("Expected MaxUsers=%v, got %v", testSettings.MaxUsers, retrieved.MaxUsers)
	}
	if retrieved.MFARequired != testSettings.MFARequired {
		t.Errorf("Expected MFARequired=%v, got %v", testSettings.MFARequired, retrieved.MFARequired)
	}
	if retrieved.SessionTimeout != testSettings.SessionTimeout {
		t.Errorf("Expected SessionTimeout=%v, got %v", testSettings.SessionTimeout, retrieved.SessionTimeout)
	}
	if retrieved.SessionIdleTimeout != testSettings.SessionIdleTimeout {
		t.Errorf("Expected SessionIdleTimeout=%v, got %v", testSettings.SessionIdleTimeout, retrieved.SessionIdleTimeout)
	}

	// Test getting settings for non-existent tenant
	_, err = storage.GetSettings(ctx, "nonexistent-tenant")
	if err == nil {
		t.Error("Expected error when getting settings for non-existent tenant")
	}
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("Expected QBError, got: %v", err)
	}
	if qbErr.Code != dberrors.ErrNotFound {
		t.Errorf("Expected ErrNotFound code, got: %v", qbErr.Code)
	}
}

// TestMemoryStorage_UpdateSettings tests the UpdateSettings function
func TestMemoryStorage_UpdateSettings(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	tenant := &Tenant{
		ID:      "update-settings-tenant",
		Name:    "Update Settings Test",
		Created: time.Now(),
		Updated: time.Now(),
	}
	storage.CreateTenant(ctx, tenant)

	// Test creating new settings
	newSettings := &TenantSettings{
		MaxUsers:           200,
		MFARequired:        false,
		SessionTimeout:     2 * time.Hour,
		SessionIdleTimeout: time.Hour,
	}
	err := storage.UpdateSettings(ctx, tenant.ID, newSettings)
	if err != nil {
		t.Fatalf("UpdateSettings failed for new settings: %v", err)
	}

	// Verify settings were stored
	retrieved, _ := storage.GetSettings(ctx, tenant.ID)
	if retrieved.SessionTimeout != newSettings.SessionTimeout {
		t.Errorf("Expected SessionTimeout=%v, got %v", newSettings.SessionTimeout, retrieved.SessionTimeout)
	}

	// Test updating existing settings
	updatedSettings := &TenantSettings{
		MaxUsers:           50,
		MFARequired:        true,
		SessionTimeout:     30 * time.Minute,
		SessionIdleTimeout: 15 * time.Minute,
	}
	err = storage.UpdateSettings(ctx, tenant.ID, updatedSettings)
	if err != nil {
		t.Fatalf("UpdateSettings failed for update: %v", err)
	}

	// Verify settings were updated
	retrieved, _ = storage.GetSettings(ctx, tenant.ID)
	if retrieved.MFARequired != updatedSettings.MFARequired {
		t.Errorf("Expected MFARequired=%v, got %v", updatedSettings.MFARequired, retrieved.MFARequired)
	}
	if retrieved.SessionTimeout != updatedSettings.SessionTimeout {
		t.Errorf("Expected SessionTimeout=%v, got %v", updatedSettings.SessionTimeout, retrieved.SessionTimeout)
	}

	// Test updating settings for non-existent tenant
	err = storage.UpdateSettings(ctx, "nonexistent-tenant", updatedSettings)
	if err == nil {
		t.Error("Expected error when updating settings for non-existent tenant")
	}
	var qbErr *dberrors.QBError
	if !errors.As(err, &qbErr) {
		t.Errorf("Expected QBError, got: %v", err)
	}
	if qbErr.Code != dberrors.ErrNotFound {
		t.Errorf("Expected ErrNotFound code, got: %v", qbErr.Code)
	}
}

func TestMemoryStorage_UpdateTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	err := storage.UpdateTenant(ctx, &Tenant{ID: "nonexistent", Name: "updated"})
	if err == nil {
		t.Fatal("expected error updating nonexistent tenant")
	}
}

func TestMemoryStorage_UpdateUser_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	err := storage.UpdateUser(ctx, &User{ID: "none", TenantID: "t1", Email: "a@b.com"})
	if err == nil {
		t.Fatal("expected error updating nonexistent user")
	}
}

func TestMemoryStorage_UpdateUser_EmailChange(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	user := &User{ID: "1", TenantID: "t1", Email: "old@test.com", Name: "Test User"}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	user.Email = "new@test.com"
	if err := storage.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	found, err := storage.GetUserByEmail(ctx, "t1", "new@test.com")
	if err != nil {
		t.Fatalf("GetUserByEmail with new email failed: %v", err)
	}
	if found.Email != "new@test.com" {
		t.Fatalf("expected new@test.com, got %s", found.Email)
	}
}

// TestMemoryStorage_UpdateUser_PIVThumbprintIndex exercises the bootstrap-admin
// rebind path. UpdateUser must wire the PIV thumbprint into the lookup index so
// GetUserByPIVThumbprint can resolve the user after the binding is added,
// rotated, or cleared. Regression coverage for the bug that broke integration
// test PIV logins.
func TestMemoryStorage_UpdateUser_PIVThumbprintIndex(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	user := &User{ID: "1", TenantID: "t1", Email: "admin@test.com", Name: "Admin"}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Bind thumbprint via UpdateUser (the bootstrap-admin path).
	user.SetPIVThumbprint("thumb-1")
	if err := storage.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}
	found, err := storage.GetUserByPIVThumbprint(ctx, "thumb-1")
	if err != nil {
		t.Fatalf("GetUserByPIVThumbprint after bind failed: %v", err)
	}
	if found.ID != "1" {
		t.Fatalf("expected user 1, got %s", found.ID)
	}

	// Rotate thumbprint; the old binding must be removed.
	user.SetPIVThumbprint("thumb-2")
	if err := storage.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser rotate failed: %v", err)
	}
	if _, err := storage.GetUserByPIVThumbprint(ctx, "thumb-1"); err == nil {
		t.Fatal("expected old thumbprint to be unbound")
	}
	found, err = storage.GetUserByPIVThumbprint(ctx, "thumb-2")
	if err != nil {
		t.Fatalf("GetUserByPIVThumbprint after rotate failed: %v", err)
	}
	if found.ID != "1" {
		t.Fatalf("expected user 1 after rotate, got %s", found.ID)
	}

	// Clear the thumbprint; lookup must fail.
	user.SetPIVThumbprint("")
	if err := storage.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser clear failed: %v", err)
	}
	if _, err := storage.GetUserByPIVThumbprint(ctx, "thumb-2"); err == nil {
		t.Fatal("expected cleared thumbprint to be unbound")
	}
}

// TestMemoryStorage_DeleteUser_PIVThumbprintCleanup ensures deletion sweeps the
// PIV index entry so a recycled thumbprint can be bound to a new user.
func TestMemoryStorage_DeleteUser_PIVThumbprintCleanup(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}
	user := &User{ID: "1", TenantID: "t1", Email: "a@test.com"}
	user.SetPIVThumbprint("thumb-1")
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if err := storage.DeleteUser(ctx, "t1", "1"); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}
	if _, err := storage.GetUserByPIVThumbprint(ctx, "thumb-1"); err == nil {
		t.Fatal("expected PIV index to be cleared after DeleteUser")
	}
}

func TestMemoryStorage_UpdateRole_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	err := storage.UpdateRole(ctx, &Role{ID: "none", TenantID: "t1", Name: "Test"})
	if err == nil {
		t.Fatal("expected error updating nonexistent role")
	}
}

func TestMemoryStorage_DeleteRole_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	err := storage.DeleteRole(ctx, "t1", "nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent role")
	}
}

func TestMemoryStorage_GetQuota_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	_, err := storage.GetQuota(ctx, "nonexistent-tenant")
	if err == nil {
		t.Fatal("expected error for nonexistent quota")
	}
}

func TestMemoryStorage_UpdateQuota_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	err := storage.UpdateQuota(ctx, "nonexistent", &TenantQuota{})
	if err == nil {
		t.Fatal("expected error updating quota for nonexistent tenant")
	}
}

func TestMemoryStorage_IncrementQuotaUsage_ExceedsLimit(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	// Create tenant with quota
	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Use BaaS-layer IAM quota (apps); users are engine-level owned by qrdb.
	quota := &TenantQuota{MaxApps: 2}
	if err := storage.UpdateQuota(ctx, "t1", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Increment within limit
	if err := storage.IncrementQuotaUsage(ctx, "t1", "apps", 2); err != nil {
		t.Fatalf("IncrementQuotaUsage within limit failed: %v", err)
	}

	// Increment beyond limit
	err := storage.IncrementQuotaUsage(ctx, "t1", "apps", 1)
	if err == nil {
		t.Fatal("expected error when exceeding quota")
	}
}

func TestMemoryStorage_DecrementQuotaUsage_ExistingQuota(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{MaxApps: 10}
	if err := storage.UpdateQuota(ctx, "t1", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Increment first
	if err := storage.IncrementQuotaUsage(ctx, "t1", "apps", 5); err != nil {
		t.Fatalf("IncrementQuotaUsage failed: %v", err)
	}

	// Decrement
	if err := storage.DecrementQuotaUsage(ctx, "t1", "apps", 3); err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	// Verify quota
	q, err := storage.GetQuota(ctx, "t1")
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if q.CurrentApps != 2 {
		t.Fatalf("expected 2 apps after decrement, got %d", q.CurrentApps)
	}
}

func TestMemoryStorage_DecrementQuotaUsage_FloorAtZero(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{MaxApps: 10}
	if err := storage.UpdateQuota(ctx, "t1", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Decrement more than current (should floor at 0)
	if err := storage.DecrementQuotaUsage(ctx, "t1", "apps", 100); err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	q, err := storage.GetQuota(ctx, "t1")
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if q.CurrentApps != 0 {
		t.Fatalf("expected 0 apps after excessive decrement, got %d", q.CurrentApps)
	}
}

func TestMemoryStorage_CreateApp_DuplicateClientID(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	app1 := &Application{
		ClientID:     "client1",
		ClientSecret: "secret1",
		Name:         "App One",
	}
	if err := storage.CreateApp(ctx, app1); err != nil {
		t.Fatalf("CreateApp 1 failed: %v", err)
	}

	app2 := &Application{
		ClientID:     "client1",
		ClientSecret: "secret2",
		Name:         "App Two",
	}
	err := storage.CreateApp(ctx, app2)
	if err == nil {
		t.Fatal("expected error for duplicate client ID")
	}
}

func TestMemoryStorage_UpdateApp_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	err := storage.UpdateApp(ctx, &Application{ClientID: "nonexistent"})
	if err == nil {
		t.Fatal("expected error updating nonexistent app")
	}
}

func TestMemoryStorage_DeleteApp_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	err := storage.DeleteApp(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent app")
	}
}

func TestMemoryStorage_ValidateClientSecret_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage := NewMemoryStorage()

	_, err := storage.ValidateClientSecret(ctx, "nonexistent", "secret")
	if err == nil {
		t.Fatal("expected error validating secret for nonexistent app")
	}
}

func TestMemoryStorage_GetAppByTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	_, err := storage.GetAppByTenant(ctx, "t1", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent app by tenant")
	}
}

// =============================================================================
// MemoryStorage - IncrementQuotaUsage / DecrementQuotaUsage Additional Coverage
// =============================================================================

func TestMemoryStorage_IncrementQuotaUsage_AppsResource(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	// Create tenant first
	tenant := &Tenant{
		ID:      "test-tenant",
		Name:    "Test Tenant",
		Created: time.Now(),
		Updated: time.Now(),
	}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Set initial quota with apps limit
	quota := &TenantQuota{
		MaxApps:     5,
		CurrentApps: 0,
	}
	if err := storage.UpdateQuota(ctx, "test-tenant", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Increment apps count
	err := storage.IncrementQuotaUsage(ctx, "test-tenant", "apps", 1)
	if err != nil {
		t.Fatalf("IncrementQuotaUsage failed: %v", err)
	}

	// Verify increment
	updatedQuota, err := storage.GetQuota(ctx, "test-tenant")
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if updatedQuota.CurrentApps != 1 {
		t.Errorf("expected CurrentApps=1, got %d", updatedQuota.CurrentApps)
	}
}

func TestMemoryStorage_IncrementQuotaUsage_AppsExceedsLimit(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{
		ID:      "test-tenant-2",
		Name:    "Test Tenant 2",
		Created: time.Now(),
		Updated: time.Now(),
	}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{
		MaxApps:     2,
		CurrentApps: 2,
	}
	if err := storage.UpdateQuota(ctx, "test-tenant-2", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Try to increment beyond limit
	err := storage.IncrementQuotaUsage(ctx, "test-tenant-2", "apps", 1)
	if err == nil {
		t.Error("expected error when exceeding apps quota")
	}
}

func TestMemoryStorage_DecrementQuotaUsage_AppsResource(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{
		ID:      "test-tenant-3",
		Name:    "Test Tenant 3",
		Created: time.Now(),
		Updated: time.Now(),
	}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{
		MaxApps:     10,
		CurrentApps: 5,
	}
	if err := storage.UpdateQuota(ctx, "test-tenant-3", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	err := storage.DecrementQuotaUsage(ctx, "test-tenant-3", "apps", 2)
	if err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	updatedQuota, err := storage.GetQuota(ctx, "test-tenant-3")
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if updatedQuota.CurrentApps != 3 {
		t.Errorf("expected CurrentApps=3, got %d", updatedQuota.CurrentApps)
	}
}

func TestMemoryStorage_DecrementQuotaUsage_AppsFloorAtZero(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{
		ID:      "test-tenant-4",
		Name:    "Test Tenant 4",
		Created: time.Now(),
		Updated: time.Now(),
	}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{
		MaxApps:     10,
		CurrentApps: 1,
	}
	if err := storage.UpdateQuota(ctx, "test-tenant-4", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	// Decrement more than current
	err := storage.DecrementQuotaUsage(ctx, "test-tenant-4", "apps", 10)
	if err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	updatedQuota, err := storage.GetQuota(ctx, "test-tenant-4")
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if updatedQuota.CurrentApps != 0 {
		t.Errorf("expected CurrentApps=0 (floor), got %d", updatedQuota.CurrentApps)
	}
}

// =============================================================================
// MemorySessionExtStorage - GetByToken edge cases
// =============================================================================

func TestMemorySessionExtStorage_GetByToken_EmptyToken(t *testing.T) {
	storage := NewMemorySessionExtStorage()
	ctx := context.Background()

	_, err := storage.GetByToken(ctx, "")
	if err == nil {
		t.Error("expected error for empty token hash")
	}
}

func TestMemorySessionExtStorage_GetByToken_ContextCanceled(t *testing.T) {
	storage := NewMemorySessionExtStorage()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := storage.GetByToken(ctx, "some-token")
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestMemorySessionExtStorage_GetByToken_IndexInconsistency(t *testing.T) {
	storage := NewMemorySessionExtStorage()
	ctx := context.Background()

	// Create a valid session first
	session := &SessionExt{
		ID:        "session-1",
		UserID:    "user-1",
		TenantID:  "tenant-1",
		TokenHash: "token-hash-123",
	}
	if err := storage.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Manually corrupt the index by removing the session but keeping the token index
	storage.mu.Lock()
	delete(storage.sessions, "session-1")
	storage.mu.Unlock()

	// Try to get by token - should detect inconsistency and return not found
	_, err := storage.GetByToken(ctx, "token-hash-123")
	if err == nil {
		t.Error("expected error for index inconsistency")
	}
}

// =============================================================================
// PolicyCache - EnforceMaxSize with eviction loop
// =============================================================================

func TestPolicyCache_EnforceMaxSize_MultipleCycles(t *testing.T) {
	config := &CacheConfig{
		MaxSize:           3,
		TTL:               time.Hour,
		BloomFilterSize:   1000,
		BloomFilterHashes: 3,
	}
	cache := NewPolicyCache(config)

	// Add more policies than max size
	for i := 0; i < 5; i++ {
		policy := &Policy{Metadata: PolicyMetadata{Name: stringFromInt(i)}}
		cache.Set("tenant-1", policy)
		time.Sleep(10 * time.Millisecond) // Different timestamps for eviction order
	}

	// Enforce max size
	cache.EnforceMaxSize()

	// Should have at most MaxSize items
	if cache.Size() > 3 {
		t.Errorf("expected size <= 3, got %d", cache.Size())
	}
}

// =============================================================================
// PEPMetrics - GetAverageLatency coverage
// =============================================================================

func TestPEPMetrics_GetAverageLatency_WithData(t *testing.T) {
	metrics := NewPEPMetrics()

	// Record some latencies (duration, allowed)
	metrics.RecordCheck(100*time.Millisecond, true)
	metrics.RecordCheck(200*time.Millisecond, false)
	metrics.RecordCheck(150*time.Millisecond, true)

	avg := metrics.GetAverageLatency()
	if avg == 0 {
		t.Error("expected non-zero average latency")
	}

	// Average should be (100 + 200 + 150) / 3 = 150ms
	expected := 150 * time.Millisecond
	if avg < expected-10*time.Millisecond || avg > expected+10*time.Millisecond {
		t.Errorf("expected avg latency ~%v, got %v", expected, avg)
	}
}

func TestPEPMetrics_GetAverageLatency_Empty(t *testing.T) {
	metrics := NewPEPMetrics()

	avg := metrics.GetAverageLatency()
	if avg != 0 {
		t.Errorf("expected zero average latency for empty metrics, got %v", avg)
	}
}

// =============================================================================
// GDPR Storage - matchesExportFilter and matchesDeletionFilter edge cases
// =============================================================================

func TestMemoryDataExportStorage_ListExportRequests_FilterByAllCriteria(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	// Create multiple export requests
	_, err := storage.CreateExportRequest(ctx, CreateDataExportOptions{
		TenantID:    "tenant-1",
		UserID:      "user-1",
		RequestedBy: "user-1",
		Format:      DataExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("CreateExportRequest failed: %v", err)
	}

	_, err = storage.CreateExportRequest(ctx, CreateDataExportOptions{
		TenantID:    "tenant-2",
		UserID:      "user-2",
		RequestedBy: "user-2",
		Format:      DataExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("CreateExportRequest failed: %v", err)
	}

	// Filter by all criteria: tenant, user, and status
	requests, err := storage.ListExportRequests(ctx, ListExportRequestsOptions{
		TenantID: "tenant-1",
		UserID:   "user-1",
		Status:   DataExportStatusPending,
	})
	if err != nil {
		t.Fatalf("ListExportRequests failed: %v", err)
	}

	if len(requests) != 1 {
		t.Errorf("expected 1 request matching all criteria, got %d", len(requests))
	}
}

func TestMemoryDataExportStorage_ListExportRequests_NoMatchingStatus(t *testing.T) {
	storage := NewMemoryDataExportStorage()
	ctx := context.Background()

	_, err := storage.CreateExportRequest(ctx, CreateDataExportOptions{
		TenantID:    "tenant-1",
		UserID:      "user-1",
		RequestedBy: "user-1",
		Format:      DataExportFormatJSON,
	})
	if err != nil {
		t.Fatalf("CreateExportRequest failed: %v", err)
	}

	// Filter by non-matching status
	requests, err := storage.ListExportRequests(ctx, ListExportRequestsOptions{
		TenantID: "tenant-1",
		Status:   DataExportStatusCompleted, // Should not match pending
	})
	if err != nil {
		t.Fatalf("ListExportRequests failed: %v", err)
	}

	if len(requests) != 0 {
		t.Errorf("expected 0 requests with completed status, got %d", len(requests))
	}
}

func TestMemoryDeletionStorage_ListDeletionRequests_FilterByAllCriteria(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	_, err := storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		TenantID:    "tenant-1",
		UserID:      "user-1",
		RequestedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest failed: %v", err)
	}

	_, err = storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		TenantID:    "tenant-2",
		UserID:      "user-2",
		RequestedBy: "user-2",
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest failed: %v", err)
	}

	// Filter by all criteria
	requests, err := storage.ListDeletionRequests(ctx, ListDeletionRequestsOptions{
		TenantID: "tenant-1",
		UserID:   "user-1",
		Status:   DeletionStatusPending,
	})
	if err != nil {
		t.Fatalf("ListDeletionRequests failed: %v", err)
	}

	if len(requests) != 1 {
		t.Errorf("expected 1 request matching all criteria, got %d", len(requests))
	}
}

func TestMemoryDeletionStorage_ListDeletionRequests_NoMatchingTenant(t *testing.T) {
	storage := NewMemoryDeletionStorage()
	ctx := context.Background()

	_, err := storage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		TenantID:    "tenant-1",
		UserID:      "user-1",
		RequestedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest failed: %v", err)
	}

	requests, err := storage.ListDeletionRequests(ctx, ListDeletionRequestsOptions{
		TenantID: "tenant-999",
	})
	if err != nil {
		t.Fatalf("ListDeletionRequests failed: %v", err)
	}

	if len(requests) != 0 {
		t.Errorf("expected 0 requests for non-existent tenant, got %d", len(requests))
	}
}

// =============================================================================
// BatchProcessor - ImportGroups and ImportPolicies path coverage
// =============================================================================

func TestBatchProcessor_ImportPolicies_NoPolicyObject(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Import with Name but no Policy object - should construct policy from fields
	items := []PolicyImportItem{
		{
			Name:        "inline-policy",
			Description: "Policy from fields",
			Subjects:    SubjectSelector{Users: []string{"user-1"}},
			Rules:       []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
		},
	}

	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Succeeded != 1 {
		t.Errorf("expected 1 successful import, got %d (failed: %d)", saved.Progress.Succeeded, saved.Progress.Failed)
	}
}

func TestBatchProcessor_ImportPolicies_ContinueOnErrorBreak(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// First item invalid, second valid - with ContinueOnError=false should break
	items := []PolicyImportItem{
		{Name: "", Description: "Invalid policy - no name"},
		{
			Name: "valid-policy",
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "valid-policy", Tenant: tenantID},
				Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
			},
		},
	}

	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{
		ContinueOnError: false,
	})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	// Should stop after first error
	if saved.Progress.Failed != 1 {
		t.Errorf("expected 1 failed (break on error), got %d", saved.Progress.Failed)
	}
}

// =============================================================================
// TenantManager - ListTenants and DeleteTenant error paths
// =============================================================================

func TestDefaultTenantManager_DeleteTenant_InvalidID(t *testing.T) {
	storage := NewMemoryStorage()
	manager := NewDefaultTenantManager(storage)
	ctx := context.Background()

	err := manager.DeleteTenant(ctx, "a") // Too short
	if err == nil {
		t.Error("expected error for invalid tenant ID")
	}
}

func TestDefaultTenantManager_ListTenants_Success(t *testing.T) {
	storage := NewMemoryStorage()
	manager := NewDefaultTenantManager(storage)
	ctx := context.Background()

	// Create tenants
	for i := 0; i < 3; i++ {
		_, err := manager.CreateTenant(ctx, CreateTenantOptions{
			Name: stringFromInt(i),
		})
		if err != nil {
			t.Fatalf("CreateTenant failed: %v", err)
		}
	}

	tenants, err := manager.ListTenants(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListTenants failed: %v", err)
	}

	if len(tenants) != 3 {
		t.Errorf("expected 3 tenants, got %d", len(tenants))
	}
}

// TestMemoryStorage_CreateUser_RejectsAMalformedSeededID pins the fix for a
// silent-corruption defect: CreateUser used to try ParseUserID on a
// caller-supplied id and, when it failed, IGNORE the error and store the record
// under the string it was handed.
//
// A user id is a qrdb Snowflake in decimal, and everything downstream of the
// record assumes it — QRDBID, the auth-factor binding, the session join. So the
// create reported success and produced an account that every one of those
// operations then refused, with no error naming the create that caused it. It
// must fail at the write instead, and leave nothing behind: a partially stored
// user would be reachable by email or PIV thumbprint while its id named nobody.
func TestMemoryStorage_CreateUser_RejectsAMalformedSeededID(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	for _, id := range []string{
		"user-1",               // not a number at all
		"007",                  // a leading zero: one user, two spellings
		"0",                    // the store's invalid sentinel
		"-1",                   // negative
		"1 ",                   // trailing space
		"0x10",                 // not decimal
		"18446744073709551616", // one past the top of uint64
	} {
		t.Run(id, func(t *testing.T) {
			user := &User{ID: id, TenantID: "t1", Email: "seed@test.com", Name: "Seeded"}
			user.SetPIVThumbprint("thumb-seed")

			err := storage.CreateUser(ctx, user)
			if !errors.Is(err, ErrInvalidUserID) {
				t.Fatalf("CreateUser(%q) error = %v, want ErrInvalidUserID", id, err)
			}

			// Nothing stored: not under the id, not in either index.
			if _, err := storage.GetUser(ctx, "t1", id); err == nil {
				t.Fatalf("GetUser(%q) resolved a user the create refused", id)
			}
			if _, err := storage.GetUserByEmail(ctx, "t1", "seed@test.com"); err == nil {
				t.Fatal("the refused user is still reachable by email")
			}
			if _, err := storage.GetUserByPIVThumbprint(ctx, "thumb-seed"); err == nil {
				t.Fatal("the refused user is still reachable by PIV thumbprint")
			}
		})
	}
}

// TestMemoryStorage_CreateUser_MintsWhenNoIDIsSupplied is the other half of the
// rule above: an absent id is not an error, it is the normal path. The store
// assigns the Snowflake, and the assigned id must itself be canonical — the
// same value ParseUserID accepts — or the record would be as unusable as the
// malformed one the create refuses.
func TestMemoryStorage_CreateUser_MintsWhenNoIDIsSupplied(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()

	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	user := &User{TenantID: "t1", Email: "minted@test.com", Name: "Minted"}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if _, err := ParseUserID(user.ID); err != nil {
		t.Fatalf("minted id %q is not canonical: %v", user.ID, err)
	}
	if _, err := storage.GetUser(ctx, "t1", user.ID); err != nil {
		t.Fatalf("GetUser on the minted id failed: %v", err)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func stringFromInt(i int) string {
	return string(rune('A' + i))
}
