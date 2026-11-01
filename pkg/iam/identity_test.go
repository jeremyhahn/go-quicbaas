package iam

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// mockUserKey addresses one user record within one tenant.
type mockUserKey struct {
	tenantID string
	userID   uint64
}

type mockUserStorage struct {
	mu     sync.RWMutex
	users  map[mockUserKey]*User
	emails map[string]*User // key: tenantID:email
	nextID uint64
}

func newMockUserStorage() *mockUserStorage {
	return &mockUserStorage{
		users:  make(map[mockUserKey]*User),
		emails: make(map[string]*User),
	}
}

func (m *mockUserStorage) CreateUser(ctx context.Context, user *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// The store assigns the identity, exactly as MemoryStorage and the
	// qrdb-backed storage do.
	if user.ID == 0 {
		m.nextID++
		user.ID = m.nextID
	}

	key := mockUserKey{user.TenantID, user.ID}
	if _, exists := m.users[key]; exists {
		return ErrUserAlreadyExists
	}
	if _, exists := m.emails[user.TenantID+":"+user.Email]; exists {
		return ErrEmailAlreadyExists
	}

	m.users[key] = user
	m.emails[user.TenantID+":"+user.Email] = user
	return nil
}

func (m *mockUserStorage) GetUser(ctx context.Context, tenantID string, userID uint64) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := mockUserKey{tenantID, userID}
	user, exists := m.users[key]
	if !exists {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// MarkUserDeleting claims the record for deletion, mirroring the real stores.
func (m *mockUserStorage) MarkUserDeleting(ctx context.Context, tenantID string, userID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[mockUserKey{tenantID, userID}]
	if !exists {
		return ErrUserNotFound
	}
	user.Lifecycle = LifecycleDeleting
	return nil
}

func (m *mockUserStorage) GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.emails[tenantID+":"+email]
	if !exists {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (m *mockUserStorage) UpdateUser(ctx context.Context, user *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := mockUserKey{user.TenantID, user.ID}
	if _, exists := m.users[key]; !exists {
		return ErrUserNotFound
	}

	// Update email index if email changed
	oldUser := m.users[key]
	if oldUser.Email != user.Email {
		delete(m.emails, oldUser.Email)
		m.emails[user.TenantID+":"+user.Email] = user
	}

	m.users[key] = user
	return nil
}

func (m *mockUserStorage) DeleteUser(ctx context.Context, tenantID string, userID uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := mockUserKey{tenantID, userID}
	user, exists := m.users[key]
	if !exists {
		return ErrUserNotFound
	}

	delete(m.users, key)
	delete(m.emails, user.Email)
	return nil
}

func (m *mockUserStorage) GetUserByUsername(ctx context.Context, tenantID, username string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, user := range m.users {
		// LoginName owns the rule, exactly as both real UserStorage
		// implementations do. A hand-rolled copy here (this compared Name) is
		// how a mock answers a question production answers differently and
		// lets a test pass over a live failure.
		if user.TenantID == tenantID && user.LoginName() == username {
			return user, nil
		}
	}
	return nil, ErrUserNotFound
}

func (m *mockUserStorage) GetUserByPIVThumbprint(ctx context.Context, thumbprint string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, user := range m.users {
		if user.HasPIVThumbprint(thumbprint) {
			u := *user
			return &u, nil
		}
	}
	return nil, ErrUserNotFound
}

func (m *mockUserStorage) ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return copies in a stable, ID-sorted order and honour Limit/Offset,
	// as the qrdb-backed store does; callers that page rely on both.
	var users []*User
	for _, user := range m.users {
		if user.TenantID == tenantID {
			u := *user
			users = append(users, &u)
		}
	}
	slices.SortFunc(users, func(a, b *User) int { return cmp.Compare(a.ID, b.ID) })

	if opts.Offset >= len(users) {
		return []*User{}, nil
	}
	end := len(users)
	if opts.Limit > 0 && opts.Offset+opts.Limit < end {
		end = opts.Offset + opts.Limit
	}
	return users[opts.Offset:end], nil
}

type mockRoleStorage struct {
	mu    sync.RWMutex
	roles map[string]*Role // key: tenantID:roleID
}

func newMockRoleStorage() *mockRoleStorage {
	return &mockRoleStorage{
		roles: make(map[string]*Role),
	}
}

func (m *mockRoleStorage) CreateRole(ctx context.Context, role *Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := role.TenantID + ":" + role.ID
	if _, exists := m.roles[key]; exists {
		return ErrRoleAlreadyExists
	}

	m.roles[key] = role
	return nil
}

func (m *mockRoleStorage) GetRole(ctx context.Context, tenantID, roleID string) (*Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := tenantID + ":" + roleID
	role, exists := m.roles[key]
	if !exists {
		return nil, ErrRoleNotFound
	}
	return role, nil
}

func (m *mockRoleStorage) GetRoleByName(ctx context.Context, tenantID, name string) (*Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, role := range m.roles {
		if role.TenantID == tenantID && role.Name == name {
			return role, nil
		}
	}
	return nil, ErrRoleNotFound
}

func (m *mockRoleStorage) UpdateRole(ctx context.Context, role *Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := role.TenantID + ":" + role.ID
	if _, exists := m.roles[key]; !exists {
		return ErrRoleNotFound
	}

	m.roles[key] = role
	return nil
}

func (m *mockRoleStorage) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := tenantID + ":" + roleID
	if _, exists := m.roles[key]; !exists {
		return ErrRoleNotFound
	}

	delete(m.roles, key)
	return nil
}

func (m *mockRoleStorage) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var roles []*Role
	for _, role := range m.roles {
		if role.TenantID == tenantID {
			roles = append(roles, role)
		}
	}
	return roles, nil
}

type mockIdentityTenantManager struct {
	quotaExceeded bool
}

func (m *mockIdentityTenantManager) CreateTenant(ctx context.Context, opts CreateTenantOptions) (*Tenant, error) {
	return nil, errors.New("not implemented")
}

func (m *mockIdentityTenantManager) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	return nil, errors.New("not implemented")
}

func (m *mockIdentityTenantManager) UpdateTenant(ctx context.Context, id string, opts UpdateTenantOptions) (*Tenant, error) {
	return nil, errors.New("not implemented")
}

func (m *mockIdentityTenantManager) DeleteTenant(ctx context.Context, id string) error {
	return errors.New("not implemented")
}

func (m *mockIdentityTenantManager) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	return nil, errors.New("not implemented")
}

func (m *mockIdentityTenantManager) CheckQuota(ctx context.Context, tenantID, resourceType string) error {
	if m.quotaExceeded {
		return ErrQuotaExceeded
	}
	return nil
}

func (m *mockIdentityTenantManager) UpdateEngineQuotas(_ context.Context, _ string, _ qrdbsdk.Quotas) error {
	return nil
}

func (m *mockIdentityTenantManager) UpdateEngineEntitlements(_ context.Context, _ string, _ qrdbsdk.Entitlements) error {
	return nil
}

func newTestIdentityStore() *IdentityStore {
	return NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: newMockRoleStorage(),
		Tenants:     &mockIdentityTenantManager{},
	})
}

// Test User CRUD Operations

func TestCreateUser_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	}

	user, err := store.CreateUser(ctx, "tenant1", opts)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if user.ID == 0 {
		t.Error("Expected user ID to be generated")
	}
	if user.Email != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %s", user.Email)
	}
	if user.TenantID != "tenant1" {
		t.Errorf("Expected tenant ID tenant1, got %s", user.TenantID)
	}
	// No Roles in opts yields an empty, non-nil slice so JSON stays [].
	if user.Roles == nil || len(user.Roles) != 0 {
		t.Errorf("Expected non-nil empty Roles, got %#v", user.Roles)
	}
}

func TestCreateUser_InvalidEmail(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	tests := []struct {
		name  string
		email string
	}{
		{"empty email", ""},
		{"invalid format", "notanemail"},
		{"missing domain", "test@"},
		{"missing @", "testexample.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := CreateUserOptions{
				Email:    tt.email,
				Username: "testuser",
			}

			_, err := store.CreateUser(ctx, "tenant1", opts)
			if err == nil {
				t.Error("Expected validation error for invalid email")
			}

			var valErr *ValidationError
			if !errors.As(err, &valErr) {
				t.Errorf("Expected ValidationError, got %T", err)
			}
		})
	}
}

// TestCreateUser_DuplicateEmail pins the SCOPE of the login key's uniqueness.
//
// It is tenant-scoped, which is qrdb's ruling and not an accident: the same
// person may legitimately hold an account in two tenants, and every login path
// supplies the tenant alongside the address. A global index would forbid that
// while buying nothing — the login already knows which tenant it authenticated
// into, so it was never disambiguating anything.
func TestCreateUser_DuplicateEmail(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateUserOptions{
		Email:    "duplicate@example.com",
		Username: "user1",
	}

	if _, err := store.CreateUser(ctx, "tenant1", opts); err != nil {
		t.Fatalf("First CreateUser failed: %v", err)
	}

	// Same address, same tenant: refused.
	opts.Username = "user2"
	if _, err := store.CreateUser(ctx, "tenant1", opts); !errors.Is(err, ErrEmailAlreadyExists) {
		t.Errorf("Expected ErrEmailAlreadyExists within one tenant, got %v", err)
	}

	// Same address, different tenant: allowed.
	opts.Username = "user3"
	if _, err := store.CreateUser(ctx, "tenant2", opts); err != nil {
		t.Errorf("an address must be holdable in a second tenant, got %v", err)
	}
}

func TestCreateUser_QuotaExceeded(t *testing.T) {
	tenantMgr := &mockIdentityTenantManager{quotaExceeded: true}
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: newMockRoleStorage(),
		Tenants:     tenantMgr,
	})
	ctx := context.Background()

	opts := CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	}

	_, err := store.CreateUser(ctx, "tenant1", opts)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("Expected ErrQuotaExceeded, got %v", err)
	}
}

func TestGetUser_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create a user first
	created, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Get the user
	user, err := store.GetUser(ctx, "tenant1", created.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}

	if user.ID != created.ID {
		t.Errorf("Expected ID %d, got %d", created.ID, user.ID)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUser(ctx, "tenant1", 99999)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestGetUserByEmail_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	email := "test@example.com"
	created, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    email,
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "tenant1", email)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}

	if user.ID != created.ID {
		t.Errorf("Expected ID %d, got %d", created.ID, user.ID)
	}
}

func TestUpdateUser_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	created, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "old@example.com",
		Username: "olduser",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	newEmail := "new@example.com"
	newUsername := "newuser"
	enabled := false

	updated, err := store.UpdateUser(ctx, "tenant1", created.ID, UpdateUserOptions{
		Email:    &newEmail,
		Username: &newUsername,
		Enabled:  &enabled,
	})
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	if updated.Email != newEmail {
		t.Errorf("Expected email %s, got %s", newEmail, updated.Email)
	}
	// The username lands on Username, not over the display name.
	if updated.Username != newUsername {
		t.Errorf("Expected username %s, got %s", newUsername, updated.Username)
	}
	if updated.CanAuthenticate() {
		t.Error("Expected user to be unable to authenticate when enabled=false")
	}
}

func TestUpdateUser_DuplicateEmail(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create two users
	_, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "user1@example.com",
		Username: "user1",
	})
	if err != nil {
		t.Fatalf("CreateUser 1 failed: %v", err)
	}

	user2, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "user2@example.com",
		Username: "user2",
	})
	if err != nil {
		t.Fatalf("CreateUser 2 failed: %v", err)
	}

	// Try to update user2 email to user1's email
	existingEmail := "user1@example.com"
	_, err = store.UpdateUser(ctx, "tenant1", user2.ID, UpdateUserOptions{
		Email: &existingEmail,
	})
	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Errorf("Expected ErrEmailAlreadyExists, got %v", err)
	}
}

func TestDeleteUser_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	created, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	err = store.DeleteUser(ctx, "tenant1", created.ID)
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	// Verify user is deleted
	_, err = store.GetUser(ctx, "tenant1", created.ID)
	if !errors.Is(err, ErrUserNotFound) {
		t.Error("Expected user to be deleted")
	}
}

func TestListUsers_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create multiple users
	for i := 0; i < 3; i++ {
		_, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
			Email:    fmt.Sprintf("user%d@example.com", i),
			Username: fmt.Sprintf("user%d", i),
		})
		if err != nil {
			t.Fatalf("CreateUser %d failed: %v", i, err)
		}
	}

	users, err := store.ListUsers(ctx, "tenant1", ListOptions{})
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}

	if len(users) != 3 {
		t.Errorf("Expected 3 users, got %d", len(users))
	}
}

// Test Group CRUD Operations

// Test Group Membership Operations

// Test Role CRUD Operations

func TestCreateRole_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateRoleOptions{
		Name:        "Admin",
		Description: "Administrator role",
		Permissions: []string{"read", "write", "delete"},
	}

	role, err := store.CreateRole(ctx, "tenant1", opts)
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	if role.ID == "" {
		t.Error("Expected role ID to be generated")
	}
	if role.Name != "Admin" {
		t.Errorf("Expected name 'Admin', got %s", role.Name)
	}
	if len(role.Permissions) != 3 {
		t.Errorf("Expected 3 permissions, got %d", len(role.Permissions))
	}
}

func TestUpdateRole_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	created, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Old Role",
	})

	newName := "New Role"
	newPerms := []string{"read"}

	updated, err := store.UpdateRole(ctx, "tenant1", created.ID, UpdateRoleOptions{
		Name:        &newName,
		Permissions: &newPerms,
	})
	if err != nil {
		t.Fatalf("UpdateRole failed: %v", err)
	}

	if updated.Name != newName {
		t.Errorf("Expected name %s, got %s", newName, updated.Name)
	}
	if len(updated.Permissions) != 1 {
		t.Errorf("Expected 1 permission, got %d", len(updated.Permissions))
	}
}

// Test Role Assignment Operations

func TestAssignRole_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Admin",
	})

	err := store.AssignRole(ctx, "tenant1", user.ID, role.ID)
	if err != nil {
		t.Fatalf("AssignRole failed: %v", err)
	}

	// Verify assignment
	roles, err := store.GetUserRoles(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles failed: %v", err)
	}

	if len(roles) != 1 {
		t.Errorf("Expected 1 role, got %d", len(roles))
	}
	if roles[0].ID != role.ID {
		t.Error("Role not assigned to user")
	}
}

func TestAssignRole_AlreadyAssigned(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Admin",
	})

	_ = store.AssignRole(ctx, "tenant1", user.ID, role.ID)

	err := store.AssignRole(ctx, "tenant1", user.ID, role.ID)
	if !errors.Is(err, ErrRoleAlreadyAssigned) {
		t.Errorf("Expected ErrRoleAlreadyAssigned, got %v", err)
	}
}

func TestRevokeRole_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Admin",
	})

	_ = store.AssignRole(ctx, "tenant1", user.ID, role.ID)

	err := store.RevokeRole(ctx, "tenant1", user.ID, role.ID)
	if err != nil {
		t.Fatalf("RevokeRole failed: %v", err)
	}

	roles, _ := store.GetUserRoles(ctx, "tenant1", user.ID)
	if len(roles) != 0 {
		t.Error("Role still assigned after revocation")
	}
}

func TestRevokeRole_NotAssigned(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Admin",
	})

	err := store.RevokeRole(ctx, "tenant1", user.ID, role.ID)
	if !errors.Is(err, ErrRoleNotAssigned) {
		t.Errorf("Expected ErrRoleNotAssigned, got %v", err)
	}
}

// Test Cross-tenant Isolation

func TestCrossTenantIsolation(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create user in tenant1
	user1, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "user1@example.com",
		Username: "user1",
	})

	// Create user in tenant2
	user2, _ := store.CreateUser(ctx, "tenant2", CreateUserOptions{
		Email:    "user2@example.com",
		Username: "user2",
	})

	// Try to get tenant1 user from tenant2
	_, err := store.GetUser(ctx, "tenant2", user1.ID)
	if !errors.Is(err, ErrUserNotFound) {
		t.Error("Cross-tenant access should be denied")
	}

	// Verify correct user can be retrieved from correct tenant
	retrieved, err := store.GetUser(ctx, "tenant1", user1.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if retrieved.ID != user1.ID {
		t.Error("Failed to retrieve user from correct tenant")
	}

	_ = user2
}

// Test Validation Errors

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Field:   "email",
		Message: "invalid format",
	}

	expected := "validation error: email: invalid format"
	if err.Error() != expected {
		t.Errorf("Expected error message %s, got %s", expected, err.Error())
	}
}

// Test DeleteRole_RemovesFromUsers

func TestDeleteRole_RemovesFromUsers(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Admin",
	})

	_ = store.AssignRole(ctx, "tenant1", user.ID, role.ID)

	// Delete role
	err := store.DeleteRole(ctx, "tenant1", role.ID)
	if err != nil {
		t.Fatalf("DeleteRole failed: %v", err)
	}

	// Verify role is removed from user
	roles, _ := store.GetUserRoles(ctx, "tenant1", user.ID)
	if len(roles) != 0 {
		t.Error("Role still assigned to user after deletion")
	}
}

// ========== NEW TESTS FOR 0% AND LOW COVERAGE FUNCTIONS ==========

// Test GetRole - 0% coverage
func TestGetRole_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	created, err := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name:        "Test Role",
		Description: "A test role",
		Permissions: []string{"read", "write"},
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	role, err := store.GetRole(ctx, "tenant1", created.ID)
	if err != nil {
		t.Fatalf("GetRole failed: %v", err)
	}

	if role.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, role.ID)
	}
	if role.Name != "Test Role" {
		t.Errorf("Expected name 'Test Role', got %s", role.Name)
	}
}

func TestGetRole_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetRole(ctx, "tenant1", "nonexistent")
	if !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("Expected ErrRoleNotFound, got %v", err)
	}
}

// Test ListRoles - 0% coverage
func TestListRoles_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create multiple roles
	for i := 0; i < 3; i++ {
		_, err := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
			Name:        fmt.Sprintf("Role %d", i),
			Description: fmt.Sprintf("Description %d", i),
			Permissions: []string{fmt.Sprintf("perm%d", i)},
		})
		if err != nil {
			t.Fatalf("CreateRole %d failed: %v", i, err)
		}
	}

	roles, err := store.ListRoles(ctx, "tenant1", ListOptions{})
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}

	if len(roles) != 3 {
		t.Errorf("Expected 3 roles, got %d", len(roles))
	}
}

func TestListRoles_TenantIsolation(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create roles in different tenants
	for _, tenantID := range []string{"tenant1", "tenant2"} {
		if _, err := store.CreateRole(ctx, tenantID, CreateRoleOptions{Name: "Role " + tenantID}); err != nil {
			t.Fatalf("CreateRole %s failed: %v", tenantID, err)
		}
	}

	// A tenant with no roles lists none; each other tenant sees only its own.
	for tenantID, want := range map[string]int{"tenant1": 1, "tenant2": 1, "tenant3": 0} {
		roles, err := store.ListRoles(ctx, tenantID, ListOptions{})
		if err != nil {
			t.Fatalf("ListRoles %s failed: %v", tenantID, err)
		}
		if len(roles) != want {
			t.Errorf("Expected %d roles in %s, got %d", want, tenantID, len(roles))
		}
	}
}

// Test validation functions - low coverage

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{"mixed case", "TestUser", false},
		{"with numbers", "test123", false},
		{"with underscore", "test_user", false},
		{"with dash", "test-user", false},
		{"minimum length", "abc", false},
		{"maximum length", "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01", false},
		{"empty", "", true},
		{"too short", "ab", true},
		{"too long", "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ012", true},
		{"with spaces", "test user", true},
		{"with special chars", "test@user", true},
		{"with dots", "test.user", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFieldValidation(t, validateUsername(tt.username), tt.wantErr, "username")
		})
	}
}

func TestValidateIDs(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantErr   bool
		wantField string
	}{
		{"valid tenant ID", validateTenantID("tenant123"), false, ""},
		{"empty tenant ID", validateTenantID(""), true, "tenant_id"},
		{"valid user ID", validateUserID(20123), false, ""},
		{"zero user ID", validateUserID(0), true, "user_id"},
		{"valid role ID", validateRoleID("role123"), false, ""},
		{"empty role ID", validateRoleID(""), true, "role_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertFieldValidation(t, tt.err, tt.wantErr, tt.wantField)
		})
	}
}

// assertFieldValidation checks that err is nil when wantErr is false, and a
// *ValidationError naming field otherwise.
func assertFieldValidation(t *testing.T, err error, wantErr bool, field string) {
	t.Helper()
	if !wantErr {
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		return
	}
	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("Expected ValidationError, got %T (%v)", err, err)
	}
	if valErr.Field != field {
		t.Errorf("Expected field %q, got %q", field, valErr.Field)
	}
}

// TestIdentityStore_InvalidIDs checks that every public method taking a
// tenant, user or role ID rejects an empty or zero ID with a ValidationError
// naming that field, before touching storage.
func TestIdentityStore_InvalidIDs(t *testing.T) {
	newEmail := "test@example.com"
	newName := "New Name"
	createUser := CreateUserOptions{Email: "test@example.com", Username: "testuser"}

	tests := []struct {
		name      string
		call      func(ctx context.Context, store *IdentityStore) error
		wantField string
	}{
		{"CreateUser tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.CreateUser(ctx, "", createUser)
			return err
		}, "tenant_id"},
		{"GetUser tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.GetUser(ctx, "", 40001)
			return err
		}, "tenant_id"},
		{"GetUser user", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.GetUser(ctx, "tenant1", 0)
			return err
		}, "user_id"},
		{"UpdateUser tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.UpdateUser(ctx, "", 40001, UpdateUserOptions{Email: &newEmail})
			return err
		}, "tenant_id"},
		{"UpdateUser user", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.UpdateUser(ctx, "tenant1", 0, UpdateUserOptions{Email: &newEmail})
			return err
		}, "user_id"},
		{"DeleteUser tenant", func(ctx context.Context, s *IdentityStore) error {
			return s.DeleteUser(ctx, "", 40001)
		}, "tenant_id"},
		{"DeleteUser user", func(ctx context.Context, s *IdentityStore) error {
			return s.DeleteUser(ctx, "tenant1", 0)
		}, "user_id"},
		{"ListUsers tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.ListUsers(ctx, "", ListOptions{})
			return err
		}, "tenant_id"},
		{"CreateRole tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.CreateRole(ctx, "", CreateRoleOptions{Name: "Test Role"})
			return err
		}, "tenant_id"},
		{"GetRole tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.GetRole(ctx, "", "role-id")
			return err
		}, "tenant_id"},
		{"GetRole role", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.GetRole(ctx, "tenant1", "")
			return err
		}, "role_id"},
		{"UpdateRole tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.UpdateRole(ctx, "", "role-id", UpdateRoleOptions{Name: &newName})
			return err
		}, "tenant_id"},
		{"UpdateRole role", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.UpdateRole(ctx, "tenant1", "", UpdateRoleOptions{Name: &newName})
			return err
		}, "role_id"},
		{"DeleteRole tenant", func(ctx context.Context, s *IdentityStore) error {
			return s.DeleteRole(ctx, "", "role-id")
		}, "tenant_id"},
		{"DeleteRole role", func(ctx context.Context, s *IdentityStore) error {
			return s.DeleteRole(ctx, "tenant1", "")
		}, "role_id"},
		{"ListRoles tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.ListRoles(ctx, "", ListOptions{})
			return err
		}, "tenant_id"},
		{"AssignRole tenant", func(ctx context.Context, s *IdentityStore) error {
			return s.AssignRole(ctx, "", 40001, "role-id")
		}, "tenant_id"},
		{"AssignRole user", func(ctx context.Context, s *IdentityStore) error {
			return s.AssignRole(ctx, "tenant1", 0, "role-id")
		}, "user_id"},
		{"AssignRole role", func(ctx context.Context, s *IdentityStore) error {
			return s.AssignRole(ctx, "tenant1", 40001, "")
		}, "role_id"},
		{"RevokeRole tenant", func(ctx context.Context, s *IdentityStore) error {
			return s.RevokeRole(ctx, "", 40001, "role-id")
		}, "tenant_id"},
		{"RevokeRole user", func(ctx context.Context, s *IdentityStore) error {
			return s.RevokeRole(ctx, "tenant1", 0, "role-id")
		}, "user_id"},
		{"RevokeRole role", func(ctx context.Context, s *IdentityStore) error {
			return s.RevokeRole(ctx, "tenant1", 40001, "")
		}, "role_id"},
		{"GetUserRoles tenant", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.GetUserRoles(ctx, "", 40001)
			return err
		}, "tenant_id"},
		{"GetUserRoles user", func(ctx context.Context, s *IdentityStore) error {
			_, err := s.GetUserRoles(ctx, "tenant1", 0)
			return err
		}, "user_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(context.Background(), newTestIdentityStore())
			assertFieldValidation(t, err, true, tt.wantField)
		})
	}
}

// Test GetUserRoles - low coverage

func TestGetUserRoles_MultipleRoles(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	assertRoleCount := func(want int) {
		t.Helper()
		roles, err := store.GetUserRoles(ctx, "tenant1", user.ID)
		if err != nil {
			t.Fatalf("GetUserRoles failed: %v", err)
		}
		if len(roles) != want {
			t.Errorf("Expected %d roles, got %d", want, len(roles))
		}
	}

	// A new user has no roles.
	assertRoleCount(0)

	for i := 0; i < 3; i++ {
		role, err := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
			Name: fmt.Sprintf("Role %d", i),
		})
		if err != nil {
			t.Fatalf("CreateRole %d failed: %v", i, err)
		}
		if err := store.AssignRole(ctx, "tenant1", user.ID, role.ID); err != nil {
			t.Fatalf("AssignRole %d failed: %v", i, err)
		}
	}
	assertRoleCount(3)
}

func TestGetUserRoles_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUserRoles(ctx, "tenant1", 99999)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestGetUserRoles_SkipsDeletedRoles(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	role1, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Role 1",
	})
	role2, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Role 2",
	})

	_ = store.AssignRole(ctx, "tenant1", user.ID, role1.ID)
	_ = store.AssignRole(ctx, "tenant1", user.ID, role2.ID)

	// Manually add a deleted role ID to user
	usr, _ := store.GetUser(ctx, "tenant1", user.ID)
	usr.Roles = append(usr.Roles, "deleted-role-id")
	store.userStorage.UpdateUser(ctx, usr)

	roles, err := store.GetUserRoles(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles failed: %v", err)
	}

	// Should only get the 2 valid roles, skipping the deleted one
	if len(roles) != 2 {
		t.Errorf("Expected 2 roles (skipping deleted), got %d", len(roles))
	}
}

// Additional edge case tests

func TestCreateUser_InvalidUsername(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	tests := []struct {
		name     string
		username string
	}{
		{"empty", ""},
		{"too short", "ab"},
		{"with spaces", "test user"},
		{"with special", "test@user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := CreateUserOptions{
				Email:    "test@example.com",
				Username: tt.username,
			}

			_, err := store.CreateUser(ctx, "tenant1", opts)
			if err == nil {
				t.Error("Expected validation error for invalid username")
			}

			var valErr *ValidationError
			if !errors.As(err, &valErr) {
				t.Errorf("Expected ValidationError, got %T", err)
			}
		})
	}
}

func TestUpdateUser_InvalidUsername(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "validuser",
	})

	invalidUsername := "x"
	_, err := store.UpdateUser(ctx, "tenant1", user.ID, UpdateUserOptions{
		Username: &invalidUsername,
	})

	if err == nil {
		t.Error("Expected validation error for invalid username")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestIdentityStore_GetTenant_NilManager(t *testing.T) {
	store := NewIdentityStore(IdentityStoreOptions{})
	_, err := store.GetTenant(context.Background(), "tenant1")
	if err == nil {
		t.Fatal("expected error when tenant manager is nil")
	}
}

func TestIdentityStore_GetTenant_WithTenantManager(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	tenantMgr := NewDefaultTenantManager(storage)

	// Create a tenant
	tenant, err := tenantMgr.CreateTenant(ctx, CreateTenantOptions{
		Name: "test-tenant",
	})
	if err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: storage,
		Tenants:     tenantMgr,
	})

	result, err := store.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant failed: %v", err)
	}
	if result.Name != "test-tenant" {
		t.Fatalf("expected name test-tenant, got %s", result.Name)
	}
}

func TestIdentityStore_GetTenant_NotFound(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	tenantMgr := NewDefaultTenantManager(storage)

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: storage,
		Tenants:     tenantMgr,
	})

	_, err := store.GetTenant(ctx, "nonexistent-tenant")
	if err == nil {
		t.Fatal("expected error for nonexistent tenant")
	}
}

// mockCoverageTenantManager is a stub implementation of TenantManager for testing.
type mockCoverageTenantManager struct {
	quotaExceeded bool
}

func (m *mockCoverageTenantManager) CreateTenant(ctx context.Context, opts CreateTenantOptions) (*Tenant, error) {
	return nil, nil
}

func (m *mockCoverageTenantManager) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	return nil, nil
}

func (m *mockCoverageTenantManager) UpdateTenant(ctx context.Context, id string, opts UpdateTenantOptions) (*Tenant, error) {
	return nil, nil
}

func (m *mockCoverageTenantManager) DeleteTenant(ctx context.Context, id string) error {
	return nil
}

func (m *mockCoverageTenantManager) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	return nil, nil
}

func (m *mockCoverageTenantManager) CheckQuota(ctx context.Context, tenantID, resourceType string) error {
	if m.quotaExceeded {
		return ErrQuotaExceeded
	}
	return nil
}

func (m *mockCoverageTenantManager) UpdateEngineQuotas(_ context.Context, _ string, _ qrdbsdk.Quotas) error {
	return nil
}

func (m *mockCoverageTenantManager) UpdateEngineEntitlements(_ context.Context, _ string, _ qrdbsdk.Entitlements) error {
	return nil
}

// ============================================================================
// GetUserByEmail Tests - Currently 71.4%
// ============================================================================

func TestGetUserByEmail_InvalidEmail(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	tests := []struct {
		name  string
		email string
	}{
		{"empty email", ""},
		{"invalid format", "notanemail"},
		{"missing domain", "test@"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.GetUserByEmail(ctx, "tenant1", tt.email)
			if err == nil {
				t.Error("Expected validation error for invalid email")
			}

			var valErr *ValidationError
			if !errors.As(err, &valErr) {
				t.Errorf("Expected ValidationError, got %T", err)
			}
			if valErr.Field != "email" {
				t.Errorf("Expected field 'email', got %s", valErr.Field)
			}
		})
	}
}

func TestGetUserByEmail_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUserByEmail(ctx, "tenant1", "nonexistent@example.com")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestGetUserByEmail_EmailNormalization(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create user with lowercase email
	_, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "Test@Example.com",
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	// Query with mixed case - should normalize and find it
	user, err := store.GetUserByEmail(ctx, "tenant1", "TEST@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}

	if user.Email != "test@example.com" {
		t.Errorf("Expected email to be normalized to lowercase: got %s", user.Email)
	}
}

// ============================================================================
// UpdateUser Tests - Currently 77.4%
// ============================================================================

func TestUpdateUser_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newEmail := "test@example.com"
	_, err := store.UpdateUser(ctx, "tenant1", 99999, UpdateUserOptions{
		Email: &newEmail,
	})
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestUpdateUser_InvalidEmail(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "valid@example.com",
		Username: "testuser",
	})

	invalidEmail := "invalid-email"
	_, err := store.UpdateUser(ctx, "tenant1", user.ID, UpdateUserOptions{
		Email: &invalidEmail,
	})
	if err == nil {
		t.Error("Expected validation error for invalid email")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestUpdateUser_SameEmailNoCheck(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "same@example.com",
		Username: "testuser",
	})

	// Update with same email should succeed
	sameEmail := "same@example.com"
	updated, err := store.UpdateUser(ctx, "tenant1", user.ID, UpdateUserOptions{
		Email: &sameEmail,
	})
	if err != nil {
		t.Fatalf("UpdateUser with same email failed: %v", err)
	}

	if updated.Email != "same@example.com" {
		t.Errorf("Email changed unexpectedly: got %s", updated.Email)
	}
}

func TestUpdateUser_OnlyAttributes(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	attrs := map[string]string{"key": "value"}
	updated, err := store.UpdateUser(ctx, "tenant1", user.ID, UpdateUserOptions{
		Attributes: &attrs,
	})
	if err != nil {
		t.Fatalf("UpdateUser failed: %v", err)
	}

	if updated.Attributes["key"] != "value" {
		t.Error("Attributes not updated")
	}
}

// ============================================================================
// DeleteUser Tests - Currently 77.3%
// ============================================================================

func TestDeleteUser_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.DeleteUser(ctx, "tenant1", 99999)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

// ============================================================================
// ListUsers Tests - Currently 83.3%
// ============================================================================

func TestListUsers_StorageError(t *testing.T) {
	// Create a mock storage that fails on list
	failingUserStorage := &mockUserStorageWithFailure{
		mockUserStorage: newMockUserStorage(),
		failOnList:      true,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: failingUserStorage,
		RoleStorage: newMockRoleStorage(),
		Tenants:     &mockCoverageTenantManager{},
	})

	ctx := context.Background()

	_, err := store.ListUsers(ctx, "tenant1", ListOptions{})
	if err == nil {
		t.Error("Expected error when storage list fails")
	}
}

// ============================================================================
// CreateRole Tests - Currently 71.4%
// ============================================================================

func TestCreateRole_EmptyName(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "",
	})
	if err == nil {
		t.Error("Expected error for empty role name")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestCreateRole_QuotaExceeded(t *testing.T) {
	tenantMgr := &mockCoverageTenantManager{quotaExceeded: true}
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: newMockRoleStorage(),
		Tenants:     tenantMgr,
	})
	ctx := context.Background()

	_, err := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("Expected ErrQuotaExceeded, got %v", err)
	}
}

func TestCreateRole_StorageError(t *testing.T) {
	failingRoleStorage := &mockRoleStorageWithFailure{
		mockRoleStorage: newMockRoleStorage(),
		failOnCreate:    true,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: failingRoleStorage,
	})

	ctx := context.Background()

	_, err := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})
	if err == nil {
		t.Error("Expected error when storage create fails")
	}
}

func TestCreateRole_NilPermissions(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	role, err := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name:        "Test Role",
		Permissions: nil,
	})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	if role.Permissions == nil {
		t.Error("Expected permissions to be initialized to empty slice")
	}
	if len(role.Permissions) != 0 {
		t.Error("Expected empty permissions slice")
	}
}

// ============================================================================
// UpdateRole Tests - Currently 70.0%
// ============================================================================

func TestUpdateRole_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newName := "New Name"
	_, err := store.UpdateRole(ctx, "tenant1", "nonexistent", UpdateRoleOptions{
		Name: &newName,
	})
	if !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("Expected ErrRoleNotFound, got %v", err)
	}
}

func TestUpdateRole_EmptyName(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Original Name",
	})

	emptyName := ""
	_, err := store.UpdateRole(ctx, "tenant1", role.ID, UpdateRoleOptions{
		Name: &emptyName,
	})
	if err == nil {
		t.Error("Expected error for empty role name")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestUpdateRole_StorageError(t *testing.T) {
	failingRoleStorage := &mockRoleStorageWithFailure{
		mockRoleStorage: newMockRoleStorage(),
		failOnUpdate:    false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: failingRoleStorage,
	})

	ctx := context.Background()

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})

	failingRoleStorage.failOnUpdate = true

	newName := "New Name"
	_, err := store.UpdateRole(ctx, "tenant1", role.ID, UpdateRoleOptions{
		Name: &newName,
	})
	if err == nil {
		t.Error("Expected error when storage update fails")
	}
}

// ============================================================================
// DeleteRole Tests - Currently 73.9%
// ============================================================================

func TestDeleteRole_ListUsersError(t *testing.T) {
	failingUserStorage := &mockUserStorageWithFailure{
		mockUserStorage: newMockUserStorage(),
		failOnList:      true,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: failingUserStorage,
		RoleStorage: newMockRoleStorage(),
	})

	ctx := context.Background()

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})

	err := store.DeleteRole(ctx, "tenant1", role.ID)
	if err == nil {
		t.Error("Expected error when user list fails")
	}
}

func TestDeleteRole_UserUpdateError(t *testing.T) {
	failingUserStorage := &mockUserStorageWithFailure{
		mockUserStorage: newMockUserStorage(),
		failOnUpdate:    false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: failingUserStorage,
		RoleStorage: newMockRoleStorage(),
	})

	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})

	_ = store.AssignRole(ctx, "tenant1", user.ID, role.ID)

	// Enable failure
	failingUserStorage.failOnUpdate = true

	err := store.DeleteRole(ctx, "tenant1", role.ID)
	if err == nil {
		t.Error("Expected error when user update fails during role deletion")
	}
}

func TestDeleteRole_StorageError(t *testing.T) {
	failingRoleStorage := &mockRoleStorageWithFailure{
		mockRoleStorage: newMockRoleStorage(),
		failOnDelete:    false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: failingRoleStorage,
	})

	ctx := context.Background()

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})

	failingRoleStorage.failOnDelete = true

	err := store.DeleteRole(ctx, "tenant1", role.ID)
	if err == nil {
		t.Error("Expected error when storage delete fails")
	}
}

// ============================================================================
// ListRoles Tests - Currently 83.3%
// ============================================================================

func TestListRoles_StorageError(t *testing.T) {
	failingRoleStorage := &mockRoleStorageWithFailure{
		mockRoleStorage: newMockRoleStorage(),
		failOnList:      true,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: failingRoleStorage,
	})

	ctx := context.Background()

	_, err := store.ListRoles(ctx, "tenant1", ListOptions{})
	if err == nil {
		t.Error("Expected error when storage list fails")
	}
}

// ============================================================================
// AssignRole Tests - Currently 68.4%
// ============================================================================

func TestAssignRole_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})

	err := store.AssignRole(ctx, "tenant1", 99999, role.ID)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestAssignRole_RoleNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	err := store.AssignRole(ctx, "tenant1", user.ID, "nonexistent")
	if !errors.Is(err, ErrRoleNotFound) {
		t.Errorf("Expected ErrRoleNotFound, got %v", err)
	}
}

func TestAssignRole_UpdateError(t *testing.T) {
	failingUserStorage := &mockUserStorageWithFailure{
		mockUserStorage: newMockUserStorage(),
		failOnUpdate:    false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: failingUserStorage,
		RoleStorage: newMockRoleStorage(),
	})

	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})

	// Enable failure
	failingUserStorage.failOnUpdate = true

	err := store.AssignRole(ctx, "tenant1", user.ID, role.ID)
	if err == nil {
		t.Error("Expected error when user update fails")
	}
}

// ============================================================================
// RevokeRole Tests - Currently 72.7%
// ============================================================================

func TestRevokeRole_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.RevokeRole(ctx, "tenant1", 99999, "role-id")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestRevokeRole_UpdateError(t *testing.T) {
	failingUserStorage := &mockUserStorageWithFailure{
		mockUserStorage: newMockUserStorage(),
		failOnUpdate:    false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: failingUserStorage,
		RoleStorage: newMockRoleStorage(),
	})

	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})

	_ = store.AssignRole(ctx, "tenant1", user.ID, role.ID)

	// Enable failure
	failingUserStorage.failOnUpdate = true

	err := store.RevokeRole(ctx, "tenant1", user.ID, role.ID)
	if err == nil {
		t.Error("Expected error when user update fails")
	}
}

// ============================================================================
// Mock storage implementations with failure injection
// ============================================================================

type mockUserStorageWithFailure struct {
	*mockUserStorage
	failOnCreate bool
	failOnUpdate bool
	failOnDelete bool
	failOnList   bool
}

func (m *mockUserStorageWithFailure) CreateUser(ctx context.Context, user *User) error {
	if m.failOnCreate {
		return &dberrors.MessageError{Message: "simulated create failure"}
	}
	return m.mockUserStorage.CreateUser(ctx, user)
}

func (m *mockUserStorageWithFailure) UpdateUser(ctx context.Context, user *User) error {
	if m.failOnUpdate {
		return &dberrors.MessageError{Message: "simulated update failure"}
	}
	return m.mockUserStorage.UpdateUser(ctx, user)
}

func (m *mockUserStorageWithFailure) DeleteUser(ctx context.Context, tenantID string, userID uint64) error {
	if m.failOnDelete {
		return &dberrors.MessageError{Message: "simulated delete failure"}
	}
	return m.mockUserStorage.DeleteUser(ctx, tenantID, userID)
}

func (m *mockUserStorageWithFailure) ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error) {
	if m.failOnList {
		return nil, &dberrors.MessageError{Message: "simulated list failure"}
	}
	return m.mockUserStorage.ListUsers(ctx, tenantID, opts)
}

type mockRoleStorageWithFailure struct {
	*mockRoleStorage
	failOnCreate bool
	failOnUpdate bool
	failOnDelete bool
	failOnList   bool
}

func (m *mockRoleStorageWithFailure) CreateRole(ctx context.Context, role *Role) error {
	if m.failOnCreate {
		return &dberrors.MessageError{Message: "simulated create failure"}
	}
	return m.mockRoleStorage.CreateRole(ctx, role)
}

func (m *mockRoleStorageWithFailure) UpdateRole(ctx context.Context, role *Role) error {
	if m.failOnUpdate {
		return &dberrors.MessageError{Message: "simulated update failure"}
	}
	return m.mockRoleStorage.UpdateRole(ctx, role)
}

func (m *mockRoleStorageWithFailure) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	if m.failOnDelete {
		return &dberrors.MessageError{Message: "simulated delete failure"}
	}
	return m.mockRoleStorage.DeleteRole(ctx, tenantID, roleID)
}

func (m *mockRoleStorageWithFailure) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error) {
	if m.failOnList {
		return nil, &dberrors.MessageError{Message: "simulated list failure"}
	}
	return m.mockRoleStorage.ListRoles(ctx, tenantID, opts)
}

// ---------------------------------------------------------------------------
// Regression tests: CreateUser must honour opts.Roles (Issue 1 fix)
// ---------------------------------------------------------------------------

// TestCreateUser_WithRoles ensures that roles specified in CreateUserOptions are
// persisted to the created user and surfaced in the GET response. This is the
// direct regression gate for the bootstrap-admin-empty-ROLES-column bug.
// =============================================================================
// CreateUser role validation (validate-before-create shared seam)
// =============================================================================

// TestCreateUser_WithRoles_ValidReferences verifies that role IDs supplied in
// CreateUserOptions are validated against the tenant role store and persisted
// on the created user.
func TestCreateUser_WithRoles_ValidReferences(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	role, err := store.CreateRole(ctx, "tenant1", CreateRoleOptions{Name: "auditor"})
	if err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	user, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "roles@example.com",
		Username: "roleuser",
		Roles:    []string{role.ID},
	})
	if err != nil {
		t.Fatalf("CreateUser with valid roles failed: %v", err)
	}

	if len(user.Roles) != 1 || user.Roles[0] != role.ID {
		t.Errorf("Expected user.Roles [%s], got %v", role.ID, user.Roles)
	}

	// The persisted record must carry the roles too.
	stored, err := store.GetUser(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if len(stored.Roles) != 1 || stored.Roles[0] != role.ID {
		t.Errorf("Expected persisted roles [%s], got %v", role.ID, stored.Roles)
	}
}

// TestCreateUser_WithRoles_UnknownRoleFailsWithoutCreate verifies the
// validate-before-create contract shared by ALL transports: an unknown role
// reference fails fast with a typed not-found error and NO user record is
// written (a retry must not fail with already-exists).
func TestCreateUser_WithRoles_UnknownRoleFailsWithoutCreate(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "phantom@example.com",
		Username: "phantomuser",
		Roles:    []string{"no-such-role"},
	})
	if err == nil {
		t.Fatal("Expected error for unknown role reference")
	}
	if !dberrors.IsNotFound(err) {
		t.Errorf("Expected typed not-found error, got %T: %v", err, err)
	}

	// Zero side effects: the user must NOT exist.
	if _, err := store.GetUserByEmail(ctx, "tenant1", "phantom@example.com"); err == nil {
		t.Error("Expected no user to be created after failed role validation")
	}
}

// TestCreateUser_WithRoles_ValidatedBeforeQuota verifies role references are
// checked before the tenant quota, mirroring the gRPC/Unix adapter ordering
// (roles first, then quota) so all transports fail identically.
func TestCreateUser_WithRoles_ValidatedBeforeQuota(t *testing.T) {
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage: newMockUserStorage(),
		RoleStorage: newMockRoleStorage(),
		Tenants:     &mockIdentityTenantManager{quotaExceeded: true},
	})
	ctx := context.Background()

	_, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "order@example.com",
		Username: "orderuser",
		Roles:    []string{"no-such-role"},
	})
	if !dberrors.IsNotFound(err) {
		t.Errorf("Expected role not-found before quota error, got %v", err)
	}
}
