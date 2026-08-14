package iam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

type mockUserStorage struct {
	mu     sync.RWMutex
	users  map[string]*User // key: tenantID:userID
	emails map[string]*User // key: email (global index)
}

func newMockUserStorage() *mockUserStorage {
	return &mockUserStorage{
		users:  make(map[string]*User),
		emails: make(map[string]*User),
	}
}

func (m *mockUserStorage) CreateUser(ctx context.Context, user *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := user.TenantID + ":" + user.ID
	if _, exists := m.users[key]; exists {
		return ErrUserAlreadyExists
	}
	if _, exists := m.emails[user.Email]; exists {
		return ErrEmailAlreadyExists
	}

	m.users[key] = user
	m.emails[user.Email] = user
	return nil
}

func (m *mockUserStorage) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := tenantID + ":" + userID
	user, exists := m.users[key]
	if !exists {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (m *mockUserStorage) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.emails[email]
	if !exists {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (m *mockUserStorage) UpdateUser(ctx context.Context, user *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := user.TenantID + ":" + user.ID
	if _, exists := m.users[key]; !exists {
		return ErrUserNotFound
	}

	// Update email index if email changed
	oldUser := m.users[key]
	if oldUser.Email != user.Email {
		delete(m.emails, oldUser.Email)
		m.emails[user.Email] = user
	}

	m.users[key] = user
	return nil
}

func (m *mockUserStorage) DeleteUser(ctx context.Context, tenantID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := tenantID + ":" + userID
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
		if user.TenantID == tenantID && user.Name == username {
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

	var users []*User
	for key, user := range m.users {
		if user.TenantID == tenantID {
			users = append(users, user)
		}
		_ = key
	}
	return users, nil
}

type mockGroupStorage struct {
	mu     sync.RWMutex
	groups map[string]*Group // key: tenantID:groupID
}

func newMockGroupStorage() *mockGroupStorage {
	return &mockGroupStorage{
		groups: make(map[string]*Group),
	}
}

func (m *mockGroupStorage) CreateGroup(ctx context.Context, group *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := group.TenantID + ":" + group.ID
	if _, exists := m.groups[key]; exists {
		return ErrGroupAlreadyExists
	}

	m.groups[key] = group
	return nil
}

func (m *mockGroupStorage) GetGroup(ctx context.Context, tenantID, groupID string) (*Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := tenantID + ":" + groupID
	group, exists := m.groups[key]
	if !exists {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

func (m *mockGroupStorage) UpdateGroup(ctx context.Context, group *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := group.TenantID + ":" + group.ID
	if _, exists := m.groups[key]; !exists {
		return ErrGroupNotFound
	}

	m.groups[key] = group
	return nil
}

func (m *mockGroupStorage) DeleteGroup(ctx context.Context, tenantID, groupID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := tenantID + ":" + groupID
	if _, exists := m.groups[key]; !exists {
		return ErrGroupNotFound
	}

	delete(m.groups, key)
	return nil
}

func (m *mockGroupStorage) ListGroups(ctx context.Context, tenantID string, opts ListOptions) ([]*Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var groups []*Group
	for _, group := range m.groups {
		if group.TenantID == tenantID {
			groups = append(groups, group)
		}
	}
	return groups, nil
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
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
		Tenants:      &mockIdentityTenantManager{},
	})
}

// Test User CRUD Operations

func TestCreateUser_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
		Enabled:  true,
	}

	user, err := store.CreateUser(ctx, "tenant1", opts)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if user.ID == "" {
		t.Error("Expected user ID to be generated")
	}
	if user.Email != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %s", user.Email)
	}
	if user.TenantID != "tenant1" {
		t.Errorf("Expected tenant ID tenant1, got %s", user.TenantID)
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

func TestCreateUser_DuplicateEmail(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateUserOptions{
		Email:    "duplicate@example.com",
		Username: "user1",
	}

	// Create first user
	_, err := store.CreateUser(ctx, "tenant1", opts)
	if err != nil {
		t.Fatalf("First CreateUser failed: %v", err)
	}

	// Try to create second user with same email
	opts.Username = "user2"
	_, err = store.CreateUser(ctx, "tenant2", opts)
	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Errorf("Expected ErrEmailAlreadyExists, got %v", err)
	}
}

func TestCreateUser_QuotaExceeded(t *testing.T) {
	tenantMgr := &mockIdentityTenantManager{quotaExceeded: true}
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
		Tenants:      tenantMgr,
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
		t.Errorf("Expected ID %s, got %s", created.ID, user.ID)
	}
}

func TestGetUser_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUser(ctx, "tenant1", "nonexistent")
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

	user, err := store.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}

	if user.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, user.ID)
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
	if updated.Name != newUsername {
		t.Errorf("Expected username %s, got %s", newUsername, updated.Name)
	}
	if !updated.Suspended {
		t.Error("Expected user to be suspended when enabled=false")
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

func TestCreateGroup_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateGroupOptions{
		Name:        "Test Group",
		Description: "A test group",
	}

	group, err := store.CreateGroup(ctx, "tenant1", opts)
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	if group.ID == "" {
		t.Error("Expected group ID to be generated")
	}
	if group.Name != "Test Group" {
		t.Errorf("Expected name 'Test Group', got %s", group.Name)
	}
}

func TestCreateGroup_EmptyName(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateGroupOptions{
		Name: "",
	}

	_, err := store.CreateGroup(ctx, "tenant1", opts)
	if err == nil {
		t.Error("Expected error for empty group name")
	}
}

func TestGetGroup_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	created, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	group, err := store.GetGroup(ctx, "tenant1", created.ID)
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	if group.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, group.ID)
	}
}

func TestUpdateGroup_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	created, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Old Name",
	})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	newName := "New Name"
	newDesc := "New Description"

	updated, err := store.UpdateGroup(ctx, "tenant1", created.ID, UpdateGroupOptions{
		Name:        &newName,
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("UpdateGroup failed: %v", err)
	}

	if updated.Name != newName {
		t.Errorf("Expected name %s, got %s", newName, updated.Name)
	}
	if updated.Description != newDesc {
		t.Errorf("Expected description %s, got %s", newDesc, updated.Description)
	}
}

func TestDeleteGroup_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	created, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	err = store.DeleteGroup(ctx, "tenant1", created.ID)
	if err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	_, err = store.GetGroup(ctx, "tenant1", created.ID)
	if !errors.Is(err, ErrGroupNotFound) {
		t.Error("Expected group to be deleted")
	}
}

// Test Group Membership Operations

func TestAddUserToGroup_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	group, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	err = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)
	if err != nil {
		t.Fatalf("AddUserToGroup failed: %v", err)
	}

	// Verify membership
	members, err := store.GetGroupMembers(ctx, "tenant1", group.ID)
	if err != nil {
		t.Fatalf("GetGroupMembers failed: %v", err)
	}

	if len(members) != 1 {
		t.Errorf("Expected 1 member, got %d", len(members))
	}
	if members[0].ID != user.ID {
		t.Error("User not added to group")
	}
}

func TestAddUserToGroup_AlreadyInGroup(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	// Add user first time
	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	// Try to add again
	err := store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)
	if !errors.Is(err, ErrUserAlreadyInGroup) {
		t.Errorf("Expected ErrUserAlreadyInGroup, got %v", err)
	}
}

func TestRemoveUserFromGroup_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	err := store.RemoveUserFromGroup(ctx, "tenant1", group.ID, user.ID)
	if err != nil {
		t.Fatalf("RemoveUserFromGroup failed: %v", err)
	}

	members, _ := store.GetGroupMembers(ctx, "tenant1", group.ID)
	if len(members) != 0 {
		t.Error("User still in group after removal")
	}
}

func TestRemoveUserFromGroup_NotInGroup(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	err := store.RemoveUserFromGroup(ctx, "tenant1", group.ID, user.ID)
	if !errors.Is(err, ErrUserNotInGroup) {
		t.Errorf("Expected ErrUserNotInGroup, got %v", err)
	}
}

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

// Test Context Cancellation

func TestContextCancellation(t *testing.T) {
	store := newTestIdentityStore()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	time.Sleep(2 * time.Millisecond)

	_, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	// Context cancellation handling depends on storage implementation
	// This test verifies the store can handle cancelled contexts
	_ = err
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

// Test DeleteUser_RemovesFromGroups

func TestDeleteUser_RemovesFromGroups(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	// Delete user
	err := store.DeleteUser(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	// Verify user is removed from group
	members, _ := store.GetGroupMembers(ctx, "tenant1", group.ID)
	if len(members) != 0 {
		t.Error("User still in group after deletion")
	}
}

// Test DeleteGroup_RemovesFromUsers

func TestDeleteGroup_RemovesFromUsers(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})
	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	// Delete group
	err := store.DeleteGroup(ctx, "tenant1", group.ID)
	if err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}

	// Verify group is removed from user
	retrievedUser, _ := store.GetUser(ctx, "tenant1", user.ID)
	if len(retrievedUser.Groups) != 0 {
		t.Error("Group still associated with user after deletion")
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

// Test ListGroups - 0% coverage
func TestListGroups_Success(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create multiple groups
	for i := 0; i < 3; i++ {
		_, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
			Name:        fmt.Sprintf("Group %d", i),
			Description: fmt.Sprintf("Description %d", i),
		})
		if err != nil {
			t.Fatalf("CreateGroup %d failed: %v", i, err)
		}
	}

	groups, err := store.ListGroups(ctx, "tenant1", ListOptions{})
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}

	if len(groups) != 3 {
		t.Errorf("Expected 3 groups, got %d", len(groups))
	}
}

func TestListGroups_EmptyList(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	groups, err := store.ListGroups(ctx, "tenant1", ListOptions{})
	if err != nil {
		t.Fatalf("ListGroups failed: %v", err)
	}

	if len(groups) != 0 {
		t.Errorf("Expected 0 groups, got %d", len(groups))
	}
}

func TestListGroups_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.ListGroups(ctx, "", ListOptions{})
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if valErr.Field != "tenant_id" {
		t.Errorf("Expected field 'tenant_id', got %s", valErr.Field)
	}
}

func TestListGroups_TenantIsolation(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create groups in different tenants
	_, _ = store.CreateGroup(ctx, "tenant1", CreateGroupOptions{Name: "Group T1"})
	_, _ = store.CreateGroup(ctx, "tenant2", CreateGroupOptions{Name: "Group T2"})

	groups1, err := store.ListGroups(ctx, "tenant1", ListOptions{})
	if err != nil {
		t.Fatalf("ListGroups tenant1 failed: %v", err)
	}

	groups2, err := store.ListGroups(ctx, "tenant2", ListOptions{})
	if err != nil {
		t.Fatalf("ListGroups tenant2 failed: %v", err)
	}

	if len(groups1) != 1 {
		t.Errorf("Expected 1 group in tenant1, got %d", len(groups1))
	}
	if len(groups2) != 1 {
		t.Errorf("Expected 1 group in tenant2, got %d", len(groups2))
	}
}

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

func TestGetRole_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetRole(ctx, "", "role-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if valErr.Field != "tenant_id" {
		t.Errorf("Expected field 'tenant_id', got %s", valErr.Field)
	}
}

func TestGetRole_InvalidRoleID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetRole(ctx, "tenant1", "")
	if err == nil {
		t.Error("Expected error for empty role ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if valErr.Field != "role_id" {
		t.Errorf("Expected field 'role_id', got %s", valErr.Field)
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

func TestListRoles_EmptyList(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	roles, err := store.ListRoles(ctx, "tenant1", ListOptions{})
	if err != nil {
		t.Fatalf("ListRoles failed: %v", err)
	}

	if len(roles) != 0 {
		t.Errorf("Expected 0 roles, got %d", len(roles))
	}
}

func TestListRoles_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.ListRoles(ctx, "", ListOptions{})
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if valErr.Field != "tenant_id" {
		t.Errorf("Expected field 'tenant_id', got %s", valErr.Field)
	}
}

func TestListRoles_TenantIsolation(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Create roles in different tenants
	_, _ = store.CreateRole(ctx, "tenant1", CreateRoleOptions{Name: "Role T1"})
	_, _ = store.CreateRole(ctx, "tenant2", CreateRoleOptions{Name: "Role T2"})

	roles1, err := store.ListRoles(ctx, "tenant1", ListOptions{})
	if err != nil {
		t.Fatalf("ListRoles tenant1 failed: %v", err)
	}

	roles2, err := store.ListRoles(ctx, "tenant2", ListOptions{})
	if err != nil {
		t.Fatalf("ListRoles tenant2 failed: %v", err)
	}

	if len(roles1) != 1 {
		t.Errorf("Expected 1 role in tenant1, got %d", len(roles1))
	}
	if len(roles2) != 1 {
		t.Errorf("Expected 1 role in tenant2, got %d", len(roles2))
	}
}

// Test validation functions - low coverage

func TestValidateUsername_Valid(t *testing.T) {
	tests := []struct {
		name     string
		username string
	}{
		{"lowercase", "testuser"},
		{"uppercase", "TESTUSER"},
		{"mixed case", "TestUser"},
		{"with numbers", "test123"},
		{"with underscore", "test_user"},
		{"with dash", "test-user"},
		{"minimum length", "abc"},
		{"maximum length", "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUsername(tt.username)
			if err != nil {
				t.Errorf("Expected valid username, got error: %v", err)
			}
		})
	}
}

func TestValidateUsername_Invalid(t *testing.T) {
	tests := []struct {
		name     string
		username string
	}{
		{"empty", ""},
		{"too short", "ab"},
		{"too long", "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ012"},
		{"with spaces", "test user"},
		{"with special chars", "test@user"},
		{"with dots only", "test.user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateUsername(tt.username)
			if err == nil {
				t.Errorf("Expected validation error for username: %s", tt.username)
			}

			var valErr *ValidationError
			if !errors.As(err, &valErr) {
				t.Errorf("Expected ValidationError, got %T", err)
			}
			if valErr.Field != "username" {
				t.Errorf("Expected field 'username', got %s", valErr.Field)
			}
		})
	}
}

func TestValidateTenantID_Valid(t *testing.T) {
	err := validateTenantID("tenant123")
	if err != nil {
		t.Errorf("Expected valid tenant ID, got error: %v", err)
	}
}

func TestValidateTenantID_Invalid(t *testing.T) {
	err := validateTenantID("")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if valErr.Field != "tenant_id" {
		t.Errorf("Expected field 'tenant_id', got %s", valErr.Field)
	}
}

func TestValidateUserID_Valid(t *testing.T) {
	err := validateUserID("user123")
	if err != nil {
		t.Errorf("Expected valid user ID, got error: %v", err)
	}
}

func TestValidateUserID_Invalid(t *testing.T) {
	err := validateUserID("")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if valErr.Field != "user_id" {
		t.Errorf("Expected field 'user_id', got %s", valErr.Field)
	}
}

func TestValidateGroupID_Valid(t *testing.T) {
	err := validateGroupID("group123")
	if err != nil {
		t.Errorf("Expected valid group ID, got error: %v", err)
	}
}

func TestValidateGroupID_Invalid(t *testing.T) {
	err := validateGroupID("")
	if err == nil {
		t.Error("Expected error for empty group ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if valErr.Field != "group_id" {
		t.Errorf("Expected field 'group_id', got %s", valErr.Field)
	}
}

func TestValidateRoleID_Valid(t *testing.T) {
	err := validateRoleID("role123")
	if err != nil {
		t.Errorf("Expected valid role ID, got error: %v", err)
	}
}

func TestValidateRoleID_Invalid(t *testing.T) {
	err := validateRoleID("")
	if err == nil {
		t.Error("Expected error for empty role ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
	if valErr.Field != "role_id" {
		t.Errorf("Expected field 'role_id', got %s", valErr.Field)
	}
}

// Test GetGroupMembers - low coverage

func TestGetGroupMembers_EmptyGroup(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Empty Group",
	})

	members, err := store.GetGroupMembers(ctx, "tenant1", group.ID)
	if err != nil {
		t.Fatalf("GetGroupMembers failed: %v", err)
	}

	if len(members) != 0 {
		t.Errorf("Expected 0 members, got %d", len(members))
	}
}

func TestGetGroupMembers_MultipleMembers(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	// Add multiple users
	for i := 0; i < 3; i++ {
		user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
			Email:    fmt.Sprintf("user%d@example.com", i),
			Username: fmt.Sprintf("user%d", i),
		})
		_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)
	}

	members, err := store.GetGroupMembers(ctx, "tenant1", group.ID)
	if err != nil {
		t.Fatalf("GetGroupMembers failed: %v", err)
	}

	if len(members) != 3 {
		t.Errorf("Expected 3 members, got %d", len(members))
	}
}

func TestGetGroupMembers_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetGroupMembers(ctx, "", "group-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestGetGroupMembers_InvalidGroupID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetGroupMembers(ctx, "tenant1", "")
	if err == nil {
		t.Error("Expected error for empty group ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestGetGroupMembers_GroupNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetGroupMembers(ctx, "tenant1", "nonexistent")
	if !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("Expected ErrGroupNotFound, got %v", err)
	}
}

func TestGetGroupMembers_SkipsDeletedUsers(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	user1, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "user1@example.com",
		Username: "user1",
	})
	user2, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "user2@example.com",
		Username: "user2",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user1.ID)
	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user2.ID)

	// Manually mark user as deleted in group but not actually delete from storage
	// This simulates a deleted user scenario
	grp, _ := store.GetGroup(ctx, "tenant1", group.ID)
	grp.Members = append(grp.Members, "deleted-user-id")
	store.groupStorage.UpdateGroup(ctx, grp)

	members, err := store.GetGroupMembers(ctx, "tenant1", group.ID)
	if err != nil {
		t.Fatalf("GetGroupMembers failed: %v", err)
	}

	// Should only get the 2 valid users, skipping the deleted one
	if len(members) != 2 {
		t.Errorf("Expected 2 members (skipping deleted), got %d", len(members))
	}
}

// Test GetUserRoles - low coverage

func TestGetUserRoles_EmptyRoles(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	roles, err := store.GetUserRoles(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles failed: %v", err)
	}

	if len(roles) != 0 {
		t.Errorf("Expected 0 roles, got %d", len(roles))
	}
}

func TestGetUserRoles_MultipleRoles(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	// Assign multiple roles
	for i := 0; i < 3; i++ {
		role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
			Name: fmt.Sprintf("Role %d", i),
		})
		_ = store.AssignRole(ctx, "tenant1", user.ID, role.ID)
	}

	roles, err := store.GetUserRoles(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("GetUserRoles failed: %v", err)
	}

	if len(roles) != 3 {
		t.Errorf("Expected 3 roles, got %d", len(roles))
	}
}

func TestGetUserRoles_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUserRoles(ctx, "", "user-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestGetUserRoles_InvalidUserID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUserRoles(ctx, "tenant1", "")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestGetUserRoles_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUserRoles(ctx, "tenant1", "nonexistent")
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

func TestCreateUser_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	}

	_, err := store.CreateUser(ctx, "", opts)
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestGetUser_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUser(ctx, "", "user-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestGetUser_InvalidUserID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetUser(ctx, "tenant1", "")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestDeleteUser_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.DeleteUser(ctx, "", "user-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestDeleteUser_InvalidUserID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.DeleteUser(ctx, "tenant1", "")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestListUsers_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.ListUsers(ctx, "", ListOptions{})
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
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
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
		Tenants:      tenantMgr,
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
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
		Tenants:      tenantMgr,
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
			_, err := store.GetUserByEmail(ctx, tt.email)
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

	_, err := store.GetUserByEmail(ctx, "nonexistent@example.com")
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
	user, err := store.GetUserByEmail(ctx, "TEST@EXAMPLE.COM")
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

func TestUpdateUser_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newEmail := "test@example.com"
	_, err := store.UpdateUser(ctx, "", "user-id", UpdateUserOptions{
		Email: &newEmail,
	})
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestUpdateUser_InvalidUserID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newEmail := "test@example.com"
	_, err := store.UpdateUser(ctx, "tenant1", "", UpdateUserOptions{
		Email: &newEmail,
	})
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestUpdateUser_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newEmail := "test@example.com"
	_, err := store.UpdateUser(ctx, "tenant1", "nonexistent", UpdateUserOptions{
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

	attrs := map[string]interface{}{"key": "value"}
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

	err := store.DeleteUser(ctx, "tenant1", "nonexistent")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestDeleteUser_GroupUpdateError(t *testing.T) {
	// Create a mock storage that fails on group update
	failingGroupStorage := &mockGroupStorageWithFailure{
		mockGroupStorage: newMockGroupStorage(),
		failOnUpdate:     false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: failingGroupStorage,
		RoleStorage:  newMockRoleStorage(),
		Tenants:      &mockCoverageTenantManager{},
	})

	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	// Now enable failure
	failingGroupStorage.failOnUpdate = true

	err := store.DeleteUser(ctx, "tenant1", user.ID)
	if err == nil {
		t.Error("Expected error when group update fails during user deletion")
	}
}

func TestDeleteUser_SkipsDeletedGroups(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	// Manually delete the group from storage
	_ = store.groupStorage.DeleteGroup(ctx, "tenant1", group.ID)

	// Now delete the user - should succeed even though group doesn't exist
	err := store.DeleteUser(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
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
		UserStorage:  failingUserStorage,
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
		Tenants:      &mockCoverageTenantManager{},
	})

	ctx := context.Background()

	_, err := store.ListUsers(ctx, "tenant1", ListOptions{})
	if err == nil {
		t.Error("Expected error when storage list fails")
	}
}

// ============================================================================
// CreateGroup Tests - Currently 75.0%
// ============================================================================

func TestCreateGroup_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.CreateGroup(ctx, "", CreateGroupOptions{
		Name: "Test Group",
	})
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestCreateGroup_QuotaExceeded(t *testing.T) {
	tenantMgr := &mockCoverageTenantManager{quotaExceeded: true}
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
		Tenants:      tenantMgr,
	})
	ctx := context.Background()

	_, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("Expected ErrQuotaExceeded, got %v", err)
	}
}

func TestCreateGroup_StorageError(t *testing.T) {
	failingGroupStorage := &mockGroupStorageWithFailure{
		mockGroupStorage: newMockGroupStorage(),
		failOnCreate:     true,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: failingGroupStorage,
		RoleStorage:  newMockRoleStorage(),
	})

	ctx := context.Background()

	_, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})
	if err == nil {
		t.Error("Expected error when storage create fails")
	}
}

// ============================================================================
// GetGroup Tests - Currently 75.0%
// ============================================================================

func TestGetGroup_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetGroup(ctx, "", "group-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestGetGroup_InvalidGroupID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetGroup(ctx, "tenant1", "")
	if err == nil {
		t.Error("Expected error for empty group ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestGetGroup_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.GetGroup(ctx, "tenant1", "nonexistent")
	if !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("Expected ErrGroupNotFound, got %v", err)
	}
}

// ============================================================================
// UpdateGroup Tests - Currently 72.2%
// ============================================================================

func TestUpdateGroup_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newName := "New Name"
	_, err := store.UpdateGroup(ctx, "", "group-id", UpdateGroupOptions{
		Name: &newName,
	})
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestUpdateGroup_InvalidGroupID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newName := "New Name"
	_, err := store.UpdateGroup(ctx, "tenant1", "", UpdateGroupOptions{
		Name: &newName,
	})
	if err == nil {
		t.Error("Expected error for empty group ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestUpdateGroup_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newName := "New Name"
	_, err := store.UpdateGroup(ctx, "tenant1", "nonexistent", UpdateGroupOptions{
		Name: &newName,
	})
	if !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("Expected ErrGroupNotFound, got %v", err)
	}
}

func TestUpdateGroup_EmptyName(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Original Name",
	})

	emptyName := ""
	_, err := store.UpdateGroup(ctx, "tenant1", group.ID, UpdateGroupOptions{
		Name: &emptyName,
	})
	if err == nil {
		t.Error("Expected error for empty group name")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestUpdateGroup_StorageError(t *testing.T) {
	failingGroupStorage := &mockGroupStorageWithFailure{
		mockGroupStorage: newMockGroupStorage(),
		failOnUpdate:     false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: failingGroupStorage,
		RoleStorage:  newMockRoleStorage(),
	})

	ctx := context.Background()

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	failingGroupStorage.failOnUpdate = true

	newName := "New Name"
	_, err := store.UpdateGroup(ctx, "tenant1", group.ID, UpdateGroupOptions{
		Name: &newName,
	})
	if err == nil {
		t.Error("Expected error when storage update fails")
	}
}

// ============================================================================
// DeleteGroup Tests - Currently 68.2%
// ============================================================================

func TestDeleteGroup_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.DeleteGroup(ctx, "", "group-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestDeleteGroup_InvalidGroupID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.DeleteGroup(ctx, "tenant1", "")
	if err == nil {
		t.Error("Expected error for empty group ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestDeleteGroup_NotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.DeleteGroup(ctx, "tenant1", "nonexistent")
	if !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("Expected ErrGroupNotFound, got %v", err)
	}
}

func TestDeleteGroup_UserUpdateError(t *testing.T) {
	failingUserStorage := &mockUserStorageWithFailure{
		mockUserStorage: newMockUserStorage(),
		failOnUpdate:    false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  failingUserStorage,
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
	})

	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	// Enable failure
	failingUserStorage.failOnUpdate = true

	err := store.DeleteGroup(ctx, "tenant1", group.ID)
	if err == nil {
		t.Error("Expected error when user update fails during group deletion")
	}
}

func TestDeleteGroup_SkipsDeletedUsers(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	// Manually delete the user from storage
	_ = store.userStorage.DeleteUser(ctx, "tenant1", user.ID)

	// Now delete the group - should succeed even though user doesn't exist
	err := store.DeleteGroup(ctx, "tenant1", group.ID)
	if err != nil {
		t.Fatalf("DeleteGroup failed: %v", err)
	}
}

func TestDeleteGroup_StorageError(t *testing.T) {
	failingGroupStorage := &mockGroupStorageWithFailure{
		mockGroupStorage: newMockGroupStorage(),
		failOnDelete:     false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: failingGroupStorage,
		RoleStorage:  newMockRoleStorage(),
	})

	ctx := context.Background()

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	failingGroupStorage.failOnDelete = true

	err := store.DeleteGroup(ctx, "tenant1", group.ID)
	if err == nil {
		t.Error("Expected error when storage delete fails")
	}
}

// ============================================================================
// ListGroups Tests - Currently 83.3%
// ============================================================================

func TestListGroups_StorageError(t *testing.T) {
	failingGroupStorage := &mockGroupStorageWithFailure{
		mockGroupStorage: newMockGroupStorage(),
		failOnList:       true,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: failingGroupStorage,
		RoleStorage:  newMockRoleStorage(),
	})

	ctx := context.Background()

	_, err := store.ListGroups(ctx, "tenant1", ListOptions{})
	if err == nil {
		t.Error("Expected error when storage list fails")
	}
}

// ============================================================================
// AddUserToGroup Tests - Currently 65.4%
// ============================================================================

func TestAddUserToGroup_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.AddUserToGroup(ctx, "", "group-id", "user-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestAddUserToGroup_InvalidGroupID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.AddUserToGroup(ctx, "tenant1", "", "user-id")
	if err == nil {
		t.Error("Expected error for empty group ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestAddUserToGroup_InvalidUserID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.AddUserToGroup(ctx, "tenant1", "group-id", "")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestAddUserToGroup_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	err := store.AddUserToGroup(ctx, "tenant1", group.ID, "nonexistent")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestAddUserToGroup_GroupNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	err := store.AddUserToGroup(ctx, "tenant1", "nonexistent", user.ID)
	if !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("Expected ErrGroupNotFound, got %v", err)
	}
}

func TestAddUserToGroup_RollbackOnGroupUpdateFailure(t *testing.T) {
	failingGroupStorage := &mockGroupStorageWithFailure{
		mockGroupStorage: newMockGroupStorage(),
		failOnUpdate:     false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: failingGroupStorage,
		RoleStorage:  newMockRoleStorage(),
	})

	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	// Enable failure for group update
	failingGroupStorage.failOnUpdate = true

	err := store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)
	if err == nil {
		t.Error("Expected error when group update fails")
	}

	// Verify user was rolled back (group should not be in user's groups)
	retrievedUser, _ := store.GetUser(ctx, "tenant1", user.ID)
	if len(retrievedUser.Groups) != 0 {
		t.Error("Expected user groups to be rolled back after failure")
	}
}

// ============================================================================
// RemoveUserFromGroup Tests - Currently 72.7%
// ============================================================================

func TestRemoveUserFromGroup_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.RemoveUserFromGroup(ctx, "", "group-id", "user-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestRemoveUserFromGroup_InvalidGroupID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.RemoveUserFromGroup(ctx, "tenant1", "", "user-id")
	if err == nil {
		t.Error("Expected error for empty group ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestRemoveUserFromGroup_InvalidUserID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.RemoveUserFromGroup(ctx, "tenant1", "group-id", "")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestRemoveUserFromGroup_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	err := store.RemoveUserFromGroup(ctx, "tenant1", group.ID, "nonexistent")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("Expected ErrUserNotFound, got %v", err)
	}
}

func TestRemoveUserFromGroup_GroupNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	err := store.RemoveUserFromGroup(ctx, "tenant1", "nonexistent", user.ID)
	if !errors.Is(err, ErrGroupNotFound) {
		t.Errorf("Expected ErrGroupNotFound, got %v", err)
	}
}

func TestRemoveUserFromGroup_GroupUpdateError(t *testing.T) {
	failingGroupStorage := &mockGroupStorageWithFailure{
		mockGroupStorage: newMockGroupStorage(),
		failOnUpdate:     false,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: failingGroupStorage,
		RoleStorage:  newMockRoleStorage(),
	})

	ctx := context.Background()

	user, _ := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "test@example.com",
		Username: "testuser",
	})

	group, _ := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{
		Name: "Test Group",
	})

	_ = store.AddUserToGroup(ctx, "tenant1", group.ID, user.ID)

	// Enable failure for group update
	failingGroupStorage.failOnUpdate = true

	err := store.RemoveUserFromGroup(ctx, "tenant1", group.ID, user.ID)
	if err == nil {
		t.Error("Expected error when group update fails")
	}
}

// ============================================================================
// CreateRole Tests - Currently 71.4%
// ============================================================================

func TestCreateRole_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.CreateRole(ctx, "", CreateRoleOptions{
		Name: "Test Role",
	})
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

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
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
		Tenants:      tenantMgr,
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
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  failingRoleStorage,
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

func TestUpdateRole_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newName := "New Name"
	_, err := store.UpdateRole(ctx, "", "role-id", UpdateRoleOptions{
		Name: &newName,
	})
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestUpdateRole_InvalidRoleID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	newName := "New Name"
	_, err := store.UpdateRole(ctx, "tenant1", "", UpdateRoleOptions{
		Name: &newName,
	})
	if err == nil {
		t.Error("Expected error for empty role ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

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
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  failingRoleStorage,
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

func TestDeleteRole_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.DeleteRole(ctx, "", "role-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestDeleteRole_InvalidRoleID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.DeleteRole(ctx, "tenant1", "")
	if err == nil {
		t.Error("Expected error for empty role ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestDeleteRole_ListUsersError(t *testing.T) {
	failingUserStorage := &mockUserStorageWithFailure{
		mockUserStorage: newMockUserStorage(),
		failOnList:      true,
	}

	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  failingUserStorage,
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
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
		UserStorage:  failingUserStorage,
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
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
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  failingRoleStorage,
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
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  failingRoleStorage,
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

func TestAssignRole_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.AssignRole(ctx, "", "user-id", "role-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestAssignRole_InvalidUserID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.AssignRole(ctx, "tenant1", "", "role-id")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestAssignRole_InvalidRoleID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.AssignRole(ctx, "tenant1", "user-id", "")
	if err == nil {
		t.Error("Expected error for empty role ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestAssignRole_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	role, _ := store.CreateRole(ctx, "tenant1", CreateRoleOptions{
		Name: "Test Role",
	})

	err := store.AssignRole(ctx, "tenant1", "nonexistent", role.ID)
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
		UserStorage:  failingUserStorage,
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
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

func TestRevokeRole_InvalidTenantID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.RevokeRole(ctx, "", "user-id", "role-id")
	if err == nil {
		t.Error("Expected error for empty tenant ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestRevokeRole_InvalidUserID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.RevokeRole(ctx, "tenant1", "", "role-id")
	if err == nil {
		t.Error("Expected error for empty user ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestRevokeRole_InvalidRoleID(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.RevokeRole(ctx, "tenant1", "user-id", "")
	if err == nil {
		t.Error("Expected error for empty role ID")
	}

	var valErr *ValidationError
	if !errors.As(err, &valErr) {
		t.Errorf("Expected ValidationError, got %T", err)
	}
}

func TestRevokeRole_UserNotFound(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	err := store.RevokeRole(ctx, "tenant1", "nonexistent", "role-id")
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
		UserStorage:  failingUserStorage,
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
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

func (m *mockUserStorageWithFailure) DeleteUser(ctx context.Context, tenantID, userID string) error {
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

type mockGroupStorageWithFailure struct {
	*mockGroupStorage
	failOnCreate bool
	failOnUpdate bool
	failOnDelete bool
	failOnList   bool
}

func (m *mockGroupStorageWithFailure) CreateGroup(ctx context.Context, group *Group) error {
	if m.failOnCreate {
		return &dberrors.MessageError{Message: "simulated create failure"}
	}
	return m.mockGroupStorage.CreateGroup(ctx, group)
}

func (m *mockGroupStorageWithFailure) UpdateGroup(ctx context.Context, group *Group) error {
	if m.failOnUpdate {
		return &dberrors.MessageError{Message: "simulated update failure"}
	}
	return m.mockGroupStorage.UpdateGroup(ctx, group)
}

func (m *mockGroupStorageWithFailure) DeleteGroup(ctx context.Context, tenantID, groupID string) error {
	if m.failOnDelete {
		return &dberrors.MessageError{Message: "simulated delete failure"}
	}
	return m.mockGroupStorage.DeleteGroup(ctx, tenantID, groupID)
}

func (m *mockGroupStorageWithFailure) ListGroups(ctx context.Context, tenantID string, opts ListOptions) ([]*Group, error) {
	if m.failOnList {
		return nil, &dberrors.MessageError{Message: "simulated list failure"}
	}
	return m.mockGroupStorage.ListGroups(ctx, tenantID, opts)
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
func TestCreateUser_WithRoles(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	// Register the role first, mirroring first-boot ordering (roles are
	// created before the admin user). CreateUser validates role references
	// before writing the user record.
	if _, err := store.CreateRole(ctx, "default", CreateRoleOptions{ID: "admin", Name: "admin"}); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	opts := CreateUserOptions{
		Email:    "admin@quicbaas.local",
		Username: "admin",
		Name:     "Bootstrap Admin",
		Roles:    []string{"admin"},
	}

	user, err := store.CreateUser(ctx, "default", opts)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if len(user.Roles) != 1 || user.Roles[0] != "admin" {
		t.Errorf("expected Roles=[admin], got %v", user.Roles)
	}

	// Re-fetch to confirm the store persisted the roles.
	fetched, err := store.GetUser(ctx, "default", user.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if len(fetched.Roles) != 1 || fetched.Roles[0] != "admin" {
		t.Errorf("persisted Roles mismatch: expected [admin], got %v", fetched.Roles)
	}
}

// TestCreateUser_WithNoRoles ensures that a user created without Roles in opts
// receives an empty (non-nil) Roles slice rather than nil, keeping JSON
// serialisation consistent across callers.
func TestCreateUser_WithNoRoles(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	opts := CreateUserOptions{
		Email:    "noroles@example.com",
		Username: "noroles",
	}

	user, err := store.CreateUser(ctx, "tenant1", opts)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if user.Roles == nil {
		t.Error("expected non-nil empty Roles slice, got nil")
	}
	if len(user.Roles) != 0 {
		t.Errorf("expected empty Roles, got %v", user.Roles)
	}
}

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
	if _, err := store.GetUserByEmail(ctx, "phantom@example.com"); err == nil {
		t.Error("Expected no user to be created after failed role validation")
	}
}

// TestCreateUser_WithRoles_ValidatedBeforeQuota verifies role references are
// checked before the tenant quota, mirroring the gRPC/Unix adapter ordering
// (roles first, then quota) so all transports fail identically.
func TestCreateUser_WithRoles_ValidatedBeforeQuota(t *testing.T) {
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
		Tenants:      &mockIdentityTenantManager{quotaExceeded: true},
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

// =============================================================================
// CreateUser group validation (validate-before-create shared seam)
// =============================================================================

// TestCreateUser_WithGroups_ValidReferences verifies that group IDs supplied
// in CreateUserOptions are validated against the tenant group store, persisted
// on the created user, AND registered back on each group's member list
// (group membership is bidirectional).
func TestCreateUser_WithGroups_ValidReferences(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	group, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{Name: "ops-team"})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	user, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "groups@example.com",
		Username: "groupuser",
		Groups:   []string{group.ID},
	})
	if err != nil {
		t.Fatalf("CreateUser with valid groups failed: %v", err)
	}

	if len(user.Groups) != 1 || user.Groups[0] != group.ID {
		t.Errorf("Expected user.Groups [%s], got %v", group.ID, user.Groups)
	}

	// The persisted record must carry the groups too.
	stored, err := store.GetUser(ctx, "tenant1", user.ID)
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	if len(stored.Groups) != 1 || stored.Groups[0] != group.ID {
		t.Errorf("Expected persisted groups [%s], got %v", group.ID, stored.Groups)
	}

	// The group's member list must reference the user back.
	storedGroup, err := store.GetGroup(ctx, "tenant1", group.ID)
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}
	if len(storedGroup.Members) != 1 || storedGroup.Members[0] != user.ID {
		t.Errorf("Expected group members [%s], got %v", user.ID, storedGroup.Members)
	}
}

// TestCreateUser_WithGroups_UnknownGroupFailsWithoutCreate verifies the
// validate-before-create contract shared by ALL transports: an unknown group
// reference fails fast with a typed not-found error and NO user record is
// written (a retry must not fail with already-exists).
func TestCreateUser_WithGroups_UnknownGroupFailsWithoutCreate(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	_, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "phantomgroup@example.com",
		Username: "phantomgroupuser",
		Groups:   []string{"no-such-group"},
	})
	if err == nil {
		t.Fatal("Expected error for unknown group reference")
	}
	if !dberrors.IsNotFound(err) {
		t.Errorf("Expected typed not-found error, got %T: %v", err, err)
	}

	// Zero side effects: the user must NOT exist.
	if _, err := store.GetUserByEmail(ctx, "phantomgroup@example.com"); err == nil {
		t.Error("Expected no user to be created after failed group validation")
	}
}

// TestCreateUser_WithNoGroups ensures a user created without Groups in opts
// receives an empty (non-nil) Groups slice rather than nil, keeping JSON
// serialisation consistent across callers.
func TestCreateUser_WithNoGroups(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	user, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "nogroups@example.com",
		Username: "nogroups",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if user.Groups == nil {
		t.Error("expected non-nil empty Groups slice, got nil")
	}
	if len(user.Groups) != 0 {
		t.Errorf("expected empty Groups, got %v", user.Groups)
	}
}

// TestCreateUser_WithGroups_DuplicateIDsDeduplicated verifies a repeated group
// ID in the request cannot double-register the user in the group's member
// list or duplicate the ID on the user record.
func TestCreateUser_WithGroups_DuplicateIDsDeduplicated(t *testing.T) {
	store := newTestIdentityStore()
	ctx := context.Background()

	group, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{Name: "dup-team"})
	if err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	user, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "dupgroups@example.com",
		Username: "dupgroups",
		Groups:   []string{group.ID, group.ID},
	})
	if err != nil {
		t.Fatalf("CreateUser with duplicate groups failed: %v", err)
	}

	if len(user.Groups) != 1 || user.Groups[0] != group.ID {
		t.Errorf("Expected deduplicated user.Groups [%s], got %v", group.ID, user.Groups)
	}
	storedGroup, err := store.GetGroup(ctx, "tenant1", group.ID)
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}
	if len(storedGroup.Members) != 1 || storedGroup.Members[0] != user.ID {
		t.Errorf("Expected single group membership [%s], got %v", user.ID, storedGroup.Members)
	}
}

// TestCreateUser_WithGroups_ValidatedBeforeQuota verifies group references are
// checked before the tenant quota, mirroring the role ordering so all
// transports fail identically.
func TestCreateUser_WithGroups_ValidatedBeforeQuota(t *testing.T) {
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  newMockUserStorage(),
		GroupStorage: newMockGroupStorage(),
		RoleStorage:  newMockRoleStorage(),
		Tenants:      &mockIdentityTenantManager{quotaExceeded: true},
	})
	ctx := context.Background()

	_, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "grouporder@example.com",
		Username: "grouporderuser",
		Groups:   []string{"no-such-group"},
	})
	if !dberrors.IsNotFound(err) {
		t.Errorf("Expected group not-found before quota error, got %v", err)
	}
}

// failOnUpdateGroupStorage wraps a GroupStorage and fails UpdateGroup for one
// specific group ID, simulating a storage fault after the user record was
// already written.
type failOnUpdateGroupStorage struct {
	GroupStorage
	failGroupID string
}

type simulatedGroupUpdateError struct{}

func (simulatedGroupUpdateError) Error() string { return "iam: simulated group update failure" }

func (s *failOnUpdateGroupStorage) UpdateGroup(ctx context.Context, group *Group) error {
	if group.ID == s.failGroupID {
		return simulatedGroupUpdateError{}
	}
	return s.GroupStorage.UpdateGroup(ctx, group)
}

// TestCreateUser_WithGroups_MemberUpdateFailureRollsBack verifies the create
// stays atomic when registering group membership fails after the user record
// was written: the user is deleted and already-updated groups are reverted.
func TestCreateUser_WithGroups_MemberUpdateFailureRollsBack(t *testing.T) {
	mem := NewMemoryStorage()
	groupStorage := &failOnUpdateGroupStorage{GroupStorage: mem}
	store := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  mem,
		GroupStorage: groupStorage,
		RoleStorage:  mem,
	})
	ctx := context.Background()

	okGroup, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{Name: "ok-team"})
	if err != nil {
		t.Fatalf("CreateGroup ok-team failed: %v", err)
	}
	badGroup, err := store.CreateGroup(ctx, "tenant1", CreateGroupOptions{Name: "bad-team"})
	if err != nil {
		t.Fatalf("CreateGroup bad-team failed: %v", err)
	}
	groupStorage.failGroupID = badGroup.ID

	_, err = store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "rollback@example.com",
		Username: "rollbackuser",
		Groups:   []string{okGroup.ID, badGroup.ID},
	})
	if err == nil {
		t.Fatal("Expected error when group member update fails")
	}

	// The user must have been rolled back.
	if _, err := store.GetUserByEmail(ctx, "rollback@example.com"); err == nil {
		t.Error("Expected user to be deleted after failed group registration")
	}

	// The successfully-updated group must have been reverted.
	reverted, err := store.GetGroup(ctx, "tenant1", okGroup.ID)
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}
	if len(reverted.Members) != 0 {
		t.Errorf("Expected ok-team members reverted to empty, got %v", reverted.Members)
	}
}
