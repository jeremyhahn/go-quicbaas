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

package iam

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Test fixtures and helpers

// mockAuditLogger is a simple mock implementation of AuditLogger for testing.
type mockAuditLogger struct {
	events []*AuditEvent
}

func (m *mockAuditLogger) Log(ctx context.Context, event *AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *mockAuditLogger) LogAsync(event *AuditEvent) {
	m.events = append(m.events, event)
}

func (m *mockAuditLogger) Query(ctx context.Context, filter *AuditFilter) ([]*AuditEvent, int, error) {
	return m.events, len(m.events), nil
}

func (m *mockAuditLogger) GetEvent(ctx context.Context, tenantID, eventID string) (*AuditEvent, error) {
	for _, event := range m.events {
		if event.ID == eventID {
			return event, nil
		}
	}
	return nil, ErrAuditEventNotFound
}

func (m *mockAuditLogger) DeleteOlderThan(ctx context.Context, tenantID string, before time.Time) (int, error) {
	var filtered []*AuditEvent
	count := 0
	for _, event := range m.events {
		if event.Timestamp.After(before) {
			filtered = append(filtered, event)
		} else {
			count++
		}
	}
	m.events = filtered
	return count, nil
}

func (m *mockAuditLogger) Close() error {
	return nil
}

// Helper function to check if error code matches
func isErrorCode(err error, code dberrors.ErrorCode) bool {
	if err == nil {
		return false
	}
	var de *dberrors.QBError
	if dberrors.As(err, &de) {
		return de.Code == code
	}
	return false
}

// Helper function to check if a string slice contains a value
func containsString(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// allowAllTargetResolver resolves every (tenant, user) pair. Tests that
// exercise impersonation mechanics unrelated to target verification use
// it; the verification itself is tested with a strict resolver in
// admin_tenant_scope_test.go (#357).
type allowAllTargetResolver struct{}

func (allowAllTargetResolver) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	return &User{ID: userID, TenantID: tenantID}, nil
}

// createTestAdminManager creates a new AdminManager for testing.
func createTestAdminManager(t *testing.T) (*AdminManager, *mockAuditLogger) {
	adminStorage := NewMemoryAdminStorage()
	impersonationStorage := NewMemoryImpersonationStorage()
	auditLogger := &mockAuditLogger{}

	manager, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         adminStorage,
		ImpersonationStorage: impersonationStorage,
		AuditLogger:          auditLogger,
		TargetResolver:       allowAllTargetResolver{},
	})
	require.NoError(t, err)
	require.NotNil(t, manager)

	return manager, auditLogger
}

// testAssignmentCounter is used to generate unique IDs for test assignments.
var testAssignmentCounter int64

// createTestAssignment creates a test admin assignment.
// Note: TenantIDs is empty by default (global scope). Set it explicitly if needed.
func createTestAssignment(userID string, role AdminRole) *AdminAssignment {
	testAssignmentCounter++
	now := time.Now().UTC()
	return &AdminAssignment{
		ID:        fmt.Sprintf("adm_test_%s_%s_%d", userID, role, testAssignmentCounter),
		UserID:    userID,
		AdminRole: role,
		TenantIDs: []string{}, // Empty by default (global scope)
		GrantedBy: "admin_user",
		GrantedAt: now,
		Active:    true,
	}
}

// createTestAssignmentWithExpiry creates a test admin assignment with expiration.
func createTestAssignmentWithExpiry(userID string, role AdminRole, expiresAt time.Time) *AdminAssignment {
	assignment := createTestAssignment(userID, role)
	assignment.ExpiresAt = &expiresAt
	return assignment
}

// TestNewAdminManager tests AdminManager creation.
func TestNewAdminManager(t *testing.T) {
	tests := []struct {
		name        string
		opts        AdminManagerOptions
		expectError bool
		errorCheck  func(t *testing.T, err error)
	}{
		{
			name: "Valid options",
			opts: AdminManagerOptions{
				AdminStorage:         NewMemoryAdminStorage(),
				ImpersonationStorage: NewMemoryImpersonationStorage(),
			},
			expectError: false,
		},
		{
			name: "Valid options with audit logger",
			opts: AdminManagerOptions{
				AdminStorage:         NewMemoryAdminStorage(),
				ImpersonationStorage: NewMemoryImpersonationStorage(),
				AuditLogger:          &mockAuditLogger{},
			},
			expectError: false,
		},
		{
			name: "Missing admin storage",
			opts: AdminManagerOptions{
				ImpersonationStorage: NewMemoryImpersonationStorage(),
			},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Missing impersonation storage",
			opts: AdminManagerOptions{
				AdminStorage: NewMemoryAdminStorage(),
			},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, err := NewAdminManager(tt.opts)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, manager)
				if tt.errorCheck != nil {
					tt.errorCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, manager)
			}
		})
	}
}

// TestAdminManager_CreateAssignment tests creating admin assignments.
func TestAdminManager_CreateAssignment(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	futureTime := now.Add(24 * time.Hour)

	tests := []struct {
		name        string
		opts        CreateAdminAssignmentOptions
		expectError bool
		errorCheck  func(t *testing.T, err error)
		validate    func(t *testing.T, assignment *AdminAssignment)
	}{
		{
			name: "Valid super admin assignment",
			opts: CreateAdminAssignmentOptions{
				UserID:    "user123",
				AdminRole: AdminRoleSuperAdmin,
				GrantedBy: "admin456",
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.NotEmpty(t, assignment.ID)
				assert.Equal(t, "user123", assignment.UserID)
				assert.Equal(t, AdminRoleSuperAdmin, assignment.AdminRole)
				assert.Equal(t, "admin456", assignment.GrantedBy)
				assert.True(t, assignment.Active)
				assert.Nil(t, assignment.ExpiresAt)
			},
		},
		{
			name: "Valid tenant admin with tenant scope",
			opts: CreateAdminAssignmentOptions{
				UserID:    "user123",
				AdminRole: AdminRoleTenantAdmin,
				TenantIDs: []string{"tenant1", "tenant2"},
				GrantedBy: "admin456",
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Equal(t, []string{"tenant1", "tenant2"}, assignment.TenantIDs)
			},
		},
		{
			name: "Valid assignment with expiration",
			opts: CreateAdminAssignmentOptions{
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
				ExpiresAt: &futureTime,
				GrantedBy: "admin456",
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				require.NotNil(t, assignment.ExpiresAt)
				assert.True(t, assignment.ExpiresAt.Equal(futureTime))
			},
		},
		{
			name: "Valid assignment with custom permissions",
			opts: CreateAdminAssignmentOptions{
				UserID:      "user123",
				AdminRole:   AdminRoleUserAdmin,
				Permissions: []AdminPermission{PermUserCreate, PermUserRead},
				GrantedBy:   "admin456",
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Contains(t, assignment.Permissions, PermUserCreate)
				assert.Contains(t, assignment.Permissions, PermUserRead)
			},
		},
		{
			name: "Valid assignment with denied permissions",
			opts: CreateAdminAssignmentOptions{
				UserID:            "user123",
				AdminRole:         AdminRoleTenantAdmin,
				DeniedPermissions: []AdminPermission{PermTenantDelete},
				GrantedBy:         "admin456",
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Contains(t, assignment.DeniedPermissions, PermTenantDelete)
			},
		},
		{
			name: "Valid assignment with resource scopes",
			opts: CreateAdminAssignmentOptions{
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
				ResourceScopes: []ResourceScope{
					{ResourceType: "user", ResourcePattern: "user:*"},
					{ResourceType: "group", ResourcePattern: "group:engineering-*"},
				},
				GrantedBy: "admin456",
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Len(t, assignment.ResourceScopes, 2)
			},
		},
		{
			name: "Valid assignment with metadata",
			opts: CreateAdminAssignmentOptions{
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
				GrantedBy: "admin456",
				Metadata: map[string]string{
					"department": "engineering",
					"location":   "us-west",
				},
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Equal(t, "engineering", assignment.Metadata["department"])
				assert.Equal(t, "us-west", assignment.Metadata["location"])
			},
		},
		{
			name: "Missing user_id",
			opts: CreateAdminAssignmentOptions{
				AdminRole: AdminRoleUserAdmin,
				GrantedBy: "admin456",
			},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Missing admin_role",
			opts: CreateAdminAssignmentOptions{
				UserID:    "user123",
				GrantedBy: "admin456",
			},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Invalid admin_role",
			opts: CreateAdminAssignmentOptions{
				UserID:    "user123",
				AdminRole: "invalid_role",
				GrantedBy: "admin456",
			},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Missing granted_by",
			opts: CreateAdminAssignmentOptions{
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
			},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, auditLogger := createTestAdminManager(t)

			assignment, err := manager.CreateAssignment(ctx, tt.opts)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, assignment)
				if tt.errorCheck != nil {
					tt.errorCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				require.NotNil(t, assignment)

				// Verify audit log was created
				assert.Len(t, auditLogger.events, 1)
				assert.Equal(t, "admin.assignment.create", auditLogger.events[0].Action)

				if tt.validate != nil {
					tt.validate(t, assignment)
				}
			}
		})
	}
}

// TestAdminManager_GetAssignment tests retrieving admin assignments.
func TestAdminManager_GetAssignment(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	// Create a test assignment
	created, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user123",
		AdminRole: AdminRoleTenantAdmin,
		GrantedBy: "admin456",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		id          string
		expectError bool
		errorCheck  func(t *testing.T, err error)
		validate    func(t *testing.T, assignment *AdminAssignment)
	}{
		{
			name:        "Get existing assignment",
			id:          created.ID,
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Equal(t, created.ID, assignment.ID)
				assert.Equal(t, "user123", assignment.UserID)
			},
		},
		{
			name:        "Get non-existent assignment",
			id:          "non_existent_id",
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.Equal(t, ErrAdminAssignmentNotFound, err)
			},
		},
		{
			name:        "Empty ID",
			id:          "",
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assignment, err := manager.GetAssignment(ctx, tt.id)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, assignment)
				if tt.errorCheck != nil {
					tt.errorCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				require.NotNil(t, assignment)
				if tt.validate != nil {
					tt.validate(t, assignment)
				}
			}
		})
	}
}

// TestAdminManager_UpdateAssignment tests updating admin assignments.
func TestAdminManager_UpdateAssignment(t *testing.T) {
	ctx := context.Background()
	manager, auditLogger := createTestAdminManager(t)

	// Create a test assignment
	created, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user123",
		AdminRole: AdminRoleUserAdmin,
		TenantIDs: []string{"tenant1"},
		GrantedBy: "admin456",
	})
	require.NoError(t, err)

	// Clear audit log from creation
	auditLogger.events = nil

	tests := []struct {
		name        string
		id          string
		opts        UpdateAdminAssignmentOptions
		expectError bool
		errorCheck  func(t *testing.T, err error)
		validate    func(t *testing.T, assignment *AdminAssignment)
	}{
		{
			name: "Update admin role",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				AdminRole: func() *AdminRole { r := AdminRoleTenantAdmin; return &r }(),
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Equal(t, AdminRoleTenantAdmin, assignment.AdminRole)
			},
		},
		{
			name: "Update tenant IDs",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				TenantIDs: []string{"tenant1", "tenant2"},
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Equal(t, []string{"tenant1", "tenant2"}, assignment.TenantIDs)
			},
		},
		{
			name: "Update permissions",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				Permissions: []AdminPermission{PermUserCreate, PermUserUpdate},
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Contains(t, assignment.Permissions, PermUserCreate)
			},
		},
		{
			name: "Update denied permissions",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				DeniedPermissions: []AdminPermission{PermUserDelete},
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Contains(t, assignment.DeniedPermissions, PermUserDelete)
			},
		},
		{
			name: "Update resource scopes",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				ResourceScopes: []ResourceScope{
					{ResourceType: "user", ResourcePattern: "user:eng-*"},
				},
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Len(t, assignment.ResourceScopes, 1)
			},
		},
		{
			name: "Set expiration",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				ExpiresAt: func() *time.Time { t := time.Now().UTC().Add(24 * time.Hour); return &t }(),
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.NotNil(t, assignment.ExpiresAt)
			},
		},
		{
			name: "Deactivate assignment",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				Active: func() *bool { b := false; return &b }(),
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.False(t, assignment.Active)
			},
		},
		{
			name: "Update metadata",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				Metadata: map[string]string{"updated": "true"},
			},
			expectError: false,
			validate: func(t *testing.T, assignment *AdminAssignment) {
				assert.Equal(t, "true", assignment.Metadata["updated"])
			},
		},
		{
			name: "Update with invalid role",
			id:   created.ID,
			opts: UpdateAdminAssignmentOptions{
				AdminRole: func() *AdminRole { r := AdminRole("invalid"); return &r }(),
			},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name:        "Update non-existent assignment",
			id:          "non_existent_id",
			opts:        UpdateAdminAssignmentOptions{},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.Equal(t, ErrAdminAssignmentNotFound, err)
			},
		},
		{
			name:        "Empty ID",
			id:          "",
			opts:        UpdateAdminAssignmentOptions{},
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear audit log before each test
			auditLogger.events = nil

			assignment, err := manager.UpdateAssignment(ctx, tt.id, tt.opts)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, assignment)
				if tt.errorCheck != nil {
					tt.errorCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				require.NotNil(t, assignment)

				// Verify audit log was created
				assert.Len(t, auditLogger.events, 1)
				assert.Equal(t, "admin.assignment.update", auditLogger.events[0].Action)

				if tt.validate != nil {
					tt.validate(t, assignment)
				}
			}
		})
	}
}

// TestAdminManager_DeleteAssignment tests deleting admin assignments.
func TestAdminManager_DeleteAssignment(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		setupFunc   func(manager *AdminManager) string
		id          string
		expectError bool
		errorCheck  func(t *testing.T, err error)
	}{
		{
			name: "Delete existing assignment",
			setupFunc: func(manager *AdminManager) string {
				assignment, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
					UserID:    "user123",
					AdminRole: AdminRoleUserAdmin,
					GrantedBy: "admin456",
				})
				require.NoError(t, err)
				return assignment.ID
			},
			expectError: false,
		},
		{
			name:        "Delete non-existent assignment",
			id:          "non_existent_id",
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.Equal(t, ErrAdminAssignmentNotFound, err)
			},
		},
		{
			name:        "Empty ID",
			id:          "",
			expectError: true,
			errorCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager, auditLogger := createTestAdminManager(t)

			id := tt.id
			if tt.setupFunc != nil {
				id = tt.setupFunc(manager)
				// Clear audit log from setup
				auditLogger.events = nil
			}

			err := manager.DeleteAssignment(ctx, id)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorCheck != nil {
					tt.errorCheck(t, err)
				}
			} else {
				assert.NoError(t, err)

				// Verify assignment is deleted
				_, err := manager.GetAssignment(ctx, id)
				assert.Equal(t, ErrAdminAssignmentNotFound, err)

				// Verify audit log was created
				assert.Len(t, auditLogger.events, 1)
				assert.Equal(t, "admin.assignment.delete", auditLogger.events[0].Action)
			}
		})
	}
}

// TestAdminManager_ListAssignments tests listing admin assignments with filters.
func TestAdminManager_ListAssignments(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	// Create test assignments
	pastTime := time.Now().UTC().Add(-24 * time.Hour)
	futureTime := time.Now().UTC().Add(24 * time.Hour)

	// User1: active super admin
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user1",
		AdminRole: AdminRoleSuperAdmin,
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// User2: active tenant admin for tenant1
	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user2",
		AdminRole: AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant1"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// User2: another assignment for tenant2
	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user2",
		AdminRole: AdminRoleUserAdmin,
		TenantIDs: []string{"tenant2"},
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// User3: inactive assignment
	assignment, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user3",
		AdminRole: AdminRoleGroupAdmin,
		GrantedBy: "admin",
	})
	require.NoError(t, err)
	activeFlag := false
	_, err = manager.UpdateAssignment(ctx, assignment.ID, UpdateAdminAssignmentOptions{
		Active: &activeFlag,
	})
	require.NoError(t, err)

	// User4: expired assignment
	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user4",
		AdminRole: AdminRoleAuditAdmin,
		ExpiresAt: &pastTime,
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	// User5: future expiry
	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user5",
		AdminRole: AdminRolePolicyAdmin,
		ExpiresAt: &futureTime,
		GrantedBy: "admin",
	})
	require.NoError(t, err)

	tests := []struct {
		name           string
		opts           ListAdminAssignmentsOptions
		expectedCount  int
		validateResult func(t *testing.T, assignments []*AdminAssignment)
	}{
		{
			name:          "List all assignments",
			opts:          ListAdminAssignmentsOptions{},
			expectedCount: 6,
		},
		{
			name: "Filter by user_id",
			opts: ListAdminAssignmentsOptions{
				UserID: "user2",
			},
			expectedCount: 2,
			validateResult: func(t *testing.T, assignments []*AdminAssignment) {
				for _, a := range assignments {
					assert.Equal(t, "user2", a.UserID)
				}
			},
		},
		{
			name: "Filter by tenant_id",
			opts: ListAdminAssignmentsOptions{
				TenantID: "tenant1",
			},
			// Only the assignment explicitly naming tenant1 matches: empty
			// TenantIDs is system-tenant scope, not all-tenants, so the
			// system-scope assignments no longer appear under a tenant
			// filter (#357).
			expectedCount: 1,
			validateResult: func(t *testing.T, assignments []*AdminAssignment) {
				for _, a := range assignments {
					assert.True(t, containsString(a.TenantIDs, "tenant1"),
						"every listed assignment must explicitly name tenant1")
				}
			},
		},
		{
			name: "Filter by admin_role",
			opts: ListAdminAssignmentsOptions{
				AdminRole: AdminRoleSuperAdmin,
			},
			expectedCount: 1,
			validateResult: func(t *testing.T, assignments []*AdminAssignment) {
				assert.Equal(t, AdminRoleSuperAdmin, assignments[0].AdminRole)
			},
		},
		{
			name: "Filter active only",
			opts: ListAdminAssignmentsOptions{
				ActiveOnly: true,
			},
			expectedCount: 4, // Excludes user3 (inactive) and user4 (expired)
			validateResult: func(t *testing.T, assignments []*AdminAssignment) {
				for _, a := range assignments {
					assert.True(t, a.Active)
					if a.ExpiresAt != nil {
						assert.True(t, time.Now().UTC().Before(*a.ExpiresAt))
					}
				}
			},
		},
		{
			name: "Pagination - limit",
			opts: ListAdminAssignmentsOptions{
				Limit: 2,
			},
			expectedCount: 2,
		},
		{
			name: "Pagination - offset",
			opts: ListAdminAssignmentsOptions{
				Offset: 2,
			},
			expectedCount: 4,
		},
		{
			name: "Pagination - limit and offset",
			opts: ListAdminAssignmentsOptions{
				Limit:  2,
				Offset: 1,
			},
			expectedCount: 2,
		},
		{
			name: "Combined filters",
			opts: ListAdminAssignmentsOptions{
				UserID:     "user2",
				TenantID:   "tenant1",
				ActiveOnly: true,
			},
			expectedCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assignments, err := manager.ListAssignments(ctx, tt.opts)
			assert.NoError(t, err)
			assert.Len(t, assignments, tt.expectedCount)

			if tt.validateResult != nil {
				tt.validateResult(t, assignments)
			}
		})
	}
}

// TestAdminManager_CheckPermission tests permission checking with various scenarios.
func TestAdminManager_CheckPermission(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	// Create test assignments
	// Super admin
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "super_admin",
		AdminRole: AdminRoleSuperAdmin,
		GrantedBy: "system",
	})
	require.NoError(t, err)

	// Tenant admin with tenant scope
	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "tenant_admin",
		AdminRole: AdminRoleTenantAdmin,
		TenantIDs: []string{"tenant1"},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	// User admin with denied permission
	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:            "user_admin",
		AdminRole:         AdminRoleUserAdmin,
		DeniedPermissions: []AdminPermission{PermUserDelete},
		GrantedBy:         "system",
	})
	require.NoError(t, err)

	// Admin with resource scope
	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "scoped_admin",
		AdminRole: AdminRoleUserAdmin,
		ResourceScopes: []ResourceScope{
			{ResourceType: "user", ResourcePattern: "user:eng-*"},
		},
		GrantedBy: "system",
	})
	require.NoError(t, err)

	// Expired assignment
	pastTime := time.Now().UTC().Add(-1 * time.Hour)
	_, err = manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "expired_admin",
		AdminRole: AdminRoleUserAdmin,
		ExpiresAt: &pastTime,
		GrantedBy: "system",
	})
	require.NoError(t, err)

	// Inactive assignment
	assignment, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "inactive_admin",
		AdminRole: AdminRoleUserAdmin,
		GrantedBy: "system",
	})
	require.NoError(t, err)
	activeFlag := false
	_, err = manager.UpdateAssignment(ctx, assignment.ID, UpdateAdminAssignmentOptions{
		Active: &activeFlag,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		req      *AdminCheckRequest
		wantErr  bool
		errCheck func(t *testing.T, err error)
		validate func(t *testing.T, resp *AdminCheckResponse)
	}{
		{
			name: "Super admin allowed",
			req: &AdminCheckRequest{
				UserID:     "super_admin",
				Permission: PermUserCreate,
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.True(t, resp.Allowed)
				assert.Equal(t, AdminRoleSuperAdmin, resp.AdminRole)
			},
		},
		{
			name: "Tenant admin with matching tenant",
			req: &AdminCheckRequest{
				UserID:     "tenant_admin",
				Permission: PermUserCreate,
				TenantID:   "tenant1",
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.True(t, resp.Allowed)
			},
		},
		{
			name: "Tenant admin with non-matching tenant",
			req: &AdminCheckRequest{
				UserID:     "tenant_admin",
				Permission: PermUserCreate,
				TenantID:   "tenant2",
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.False(t, resp.Allowed)
			},
		},
		{
			name: "Permission explicitly denied",
			req: &AdminCheckRequest{
				UserID:     "user_admin",
				Permission: PermUserDelete,
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.False(t, resp.Allowed)
				assert.Contains(t, resp.Reason, "explicitly denied")
			},
		},
		{
			name: "Permission not granted",
			req: &AdminCheckRequest{
				UserID:     "user_admin",
				Permission: PermTenantCreate,
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.False(t, resp.Allowed)
				assert.Contains(t, resp.Reason, "not granted")
			},
		},
		{
			name: "Resource scope matches",
			req: &AdminCheckRequest{
				UserID:       "scoped_admin",
				Permission:   PermUserRead,
				ResourceType: "user",
				ResourceID:   "user:eng-123",
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.True(t, resp.Allowed)
			},
		},
		{
			name: "Resource scope does not match",
			req: &AdminCheckRequest{
				UserID:       "scoped_admin",
				Permission:   PermUserRead,
				ResourceType: "user",
				ResourceID:   "user:sales-123",
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.False(t, resp.Allowed)
			},
		},
		{
			name: "Expired assignment denied",
			req: &AdminCheckRequest{
				UserID:     "expired_admin",
				Permission: PermUserRead,
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.False(t, resp.Allowed)
			},
		},
		{
			name: "Inactive assignment denied",
			req: &AdminCheckRequest{
				UserID:     "inactive_admin",
				Permission: PermUserRead,
			},
			wantErr: false,
			validate: func(t *testing.T, resp *AdminCheckResponse) {
				assert.False(t, resp.Allowed)
			},
		},
		{
			name:    "Nil request",
			req:     nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Empty user_id",
			req: &AdminCheckRequest{
				Permission: PermUserRead,
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Empty permission",
			req: &AdminCheckRequest{
				UserID: "user_admin",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := manager.CheckPermission(ctx, tt.req)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
		})
	}
}

// TestAdminManager_StartImpersonation tests starting impersonation sessions.
func TestAdminManager_StartImpersonation(t *testing.T) {
	ctx := context.Background()
	manager, auditLogger := createTestAdminManager(t)

	// Create admin with impersonation permission
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	// Clear audit log
	auditLogger.events = nil

	tests := []struct {
		name     string
		opts     *StartImpersonationOptions
		wantErr  bool
		errCheck func(t *testing.T, err error)
		validate func(t *testing.T, session *ImpersonationSession)
	}{
		{
			name: "Valid impersonation",
			opts: &StartImpersonationOptions{
				AdminUserID:  "admin_user",
				TargetUserID: "target_user",
				TenantID:     "tenant1",
				Reason:       "Support ticket #12345",
			},
			wantErr: false,
			validate: func(t *testing.T, session *ImpersonationSession) {
				assert.NotEmpty(t, session.ID)
				assert.Equal(t, "admin_user", session.AdminUserID)
				assert.Equal(t, "target_user", session.TargetUserID)
				assert.Equal(t, "tenant1", session.TenantID)
				assert.Equal(t, "Support ticket #12345", session.Reason)
				assert.Nil(t, session.EndedAt)
				assert.True(t, session.ExpiresAt.After(time.Now().UTC()))
			},
		},
		{
			name: "Valid with custom duration",
			opts: &StartImpersonationOptions{
				AdminUserID:  "admin_user",
				TargetUserID: "target_user",
				TenantID:     "tenant1",
				Reason:       "Emergency access",
				Duration:     30 * time.Minute,
			},
			wantErr: false,
			validate: func(t *testing.T, session *ImpersonationSession) {
				expectedExpiry := time.Now().UTC().Add(30 * time.Minute)
				assert.WithinDuration(t, expectedExpiry, session.ExpiresAt, 5*time.Second)
			},
		},
		{
			name: "Permission denied",
			opts: &StartImpersonationOptions{
				AdminUserID:  "non_admin_user",
				TargetUserID: "target_user",
				TenantID:     "tenant1",
				Reason:       "Test",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.Equal(t, ErrImpersonationNotAllowed, err)
			},
		},
		{
			name:    "Nil options",
			opts:    nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Missing admin_user_id",
			opts: &StartImpersonationOptions{
				TargetUserID: "target_user",
				TenantID:     "tenant1",
				Reason:       "Test",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Missing target_user_id",
			opts: &StartImpersonationOptions{
				AdminUserID: "admin_user",
				TenantID:    "tenant1",
				Reason:      "Test",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Missing tenant_id",
			opts: &StartImpersonationOptions{
				AdminUserID:  "admin_user",
				TargetUserID: "target_user",
				Reason:       "Test",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Missing reason",
			opts: &StartImpersonationOptions{
				AdminUserID:  "admin_user",
				TargetUserID: "target_user",
				TenantID:     "tenant1",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear audit log before each test
			auditLogger.events = nil

			session, err := manager.StartImpersonation(ctx, tt.opts)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, session)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				require.NotNil(t, session)

				// Verify audit log was created
				assert.Len(t, auditLogger.events, 1)
				assert.Equal(t, "admin.impersonation.start", auditLogger.events[0].Action)

				if tt.validate != nil {
					tt.validate(t, session)
				}
			}
		})
	}
}

// TestAdminManager_EndImpersonation tests ending impersonation sessions.
func TestAdminManager_EndImpersonation(t *testing.T) {
	ctx := context.Background()
	manager, auditLogger := createTestAdminManager(t)

	// Create admin with impersonation permission
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	// Start a test session
	session, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "admin_user",
		TargetUserID: "target_user",
		TenantID:     "tenant1",
		Reason:       "Test",
	})
	require.NoError(t, err)

	// Clear audit log
	auditLogger.events = nil

	tests := []struct {
		name     string
		setupFn  func() string
		id       string
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name: "End active session",
			id:   session.ID,
			setupFn: func() string {
				s, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
					AdminUserID:  "admin_user",
					TargetUserID: "target_user",
					TenantID:     "tenant1",
					Reason:       "Test",
				})
				require.NoError(t, err)
				return s.ID
			},
			wantErr: false,
		},
		{
			name: "End already ended session",
			setupFn: func() string {
				s, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
					AdminUserID:  "admin_user",
					TargetUserID: "target_user",
					TenantID:     "tenant1",
					Reason:       "Test",
				})
				require.NoError(t, err)
				err = manager.EndImpersonation(ctx, s.ID)
				require.NoError(t, err)
				return s.ID
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name:    "Non-existent session",
			id:      "non_existent_id",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.Equal(t, ErrImpersonationSessionNotFound, err)
			},
		},
		{
			name:    "Empty session ID",
			id:      "",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := tt.id
			if tt.setupFn != nil {
				id = tt.setupFn()
			}

			// Clear audit log after setup (before the action we're testing)
			auditLogger.events = nil

			err := manager.EndImpersonation(ctx, id)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)

				// Verify audit log was created
				assert.Len(t, auditLogger.events, 1)
				assert.Equal(t, "admin.impersonation.end", auditLogger.events[0].Action)
			}
		})
	}
}

// TestAdminManager_GetActiveImpersonations tests retrieving active impersonation sessions.
func TestAdminManager_GetActiveImpersonations(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	// Create admin with impersonation permission
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	// Start multiple sessions
	session1, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "admin_user",
		TargetUserID: "target_user1",
		TenantID:     "tenant1",
		Reason:       "Test 1",
	})
	require.NoError(t, err)

	session2, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "admin_user",
		TargetUserID: "target_user2",
		TenantID:     "tenant1",
		Reason:       "Test 2",
	})
	require.NoError(t, err)

	// End one session
	err = manager.EndImpersonation(ctx, session2.ID)
	require.NoError(t, err)

	tests := []struct {
		name        string
		adminUserID string
		wantErr     bool
		errCheck    func(t *testing.T, err error)
		validate    func(t *testing.T, sessions []*ImpersonationSession)
	}{
		{
			name:        "Get active sessions",
			adminUserID: "admin_user",
			wantErr:     false,
			validate: func(t *testing.T, sessions []*ImpersonationSession) {
				assert.Len(t, sessions, 1)
				assert.Equal(t, session1.ID, sessions[0].ID)
				assert.Nil(t, sessions[0].EndedAt)
			},
		},
		{
			name:        "No sessions for user",
			adminUserID: "other_admin",
			wantErr:     false,
			validate: func(t *testing.T, sessions []*ImpersonationSession) {
				assert.Len(t, sessions, 0)
			},
		},
		{
			name:        "Empty admin_user_id",
			adminUserID: "",
			wantErr:     true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions, err := manager.GetActiveImpersonations(ctx, tt.adminUserID)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, sessions)
				}
			}
		})
	}
}

// Helper function tests

// TestGetDefaultPermissions tests the default permissions for each admin role.
func TestGetDefaultPermissions(t *testing.T) {
	tests := []struct {
		name              string
		role              AdminRole
		expectedContains  []AdminPermission
		expectedExcludes  []AdminPermission
		expectedMinLength int
	}{
		{
			name: "SuperAdmin has all permissions",
			role: AdminRoleSuperAdmin,
			expectedContains: []AdminPermission{
				PermTenantCreate, PermUserImpersonate, PermAuditRead, PermGDPRExport,
			},
			expectedMinLength: 20,
		},
		{
			name: "TenantAdmin has broad permissions",
			role: AdminRoleTenantAdmin,
			expectedContains: []AdminPermission{
				PermUserCreate, PermGroupCreate, PermPolicyCreate, PermAuditRead,
			},
			expectedExcludes: []AdminPermission{
				PermTenantCreate, PermUserImpersonate,
			},
		},
		{
			name: "UserAdmin has user management permissions",
			role: AdminRoleUserAdmin,
			expectedContains: []AdminPermission{
				PermUserCreate, PermUserRead, PermUserUpdate, PermUserDelete,
			},
			expectedExcludes: []AdminPermission{
				PermTenantCreate, PermGroupCreate, PermPolicyCreate,
			},
		},
		{
			name: "GroupAdmin has group management permissions",
			role: AdminRoleGroupAdmin,
			expectedContains: []AdminPermission{
				PermGroupCreate, PermGroupRead, PermGroupUpdate, PermGroupDelete,
			},
			expectedExcludes: []AdminPermission{
				PermUserCreate, PermPolicyCreate, PermTenantCreate,
			},
		},
		{
			name: "PolicyAdmin has policy management permissions",
			role: AdminRolePolicyAdmin,
			expectedContains: []AdminPermission{
				PermPolicyCreate, PermPolicyRead, PermPolicyUpdate, PermPolicyDelete,
			},
			expectedExcludes: []AdminPermission{
				PermUserCreate, PermGroupCreate, PermTenantCreate,
			},
		},
		{
			name: "AuditAdmin has audit permissions",
			role: AdminRoleAuditAdmin,
			expectedContains: []AdminPermission{
				PermAuditRead, PermAuditExport,
			},
			expectedExcludes: []AdminPermission{
				PermUserCreate, PermGroupCreate, PermPolicyCreate,
			},
		},
		{
			name: "ReadOnly has read permissions only",
			role: AdminRoleReadOnly,
			expectedContains: []AdminPermission{
				PermTenantRead, PermUserRead, PermGroupRead, PermRoleRead, PermPolicyRead,
			},
			expectedExcludes: []AdminPermission{
				PermUserCreate, PermUserUpdate, PermUserDelete, PermGroupCreate,
			},
		},
		{
			name: "AgentAdmin has all agent permissions",
			role: AdminRoleAgentAdmin,
			expectedContains: []AdminPermission{
				PermAgentDiscover, PermAgentRegister, PermAgentSpawn,
				PermAgentPrompt, PermAgentClose, PermAgentDecompose,
				PermAgentCacheRead, PermAgentCacheWrite, PermAgentAdmin,
			},
			expectedMinLength: 9,
		},
		{
			name: "AgentOperator has all agent permissions except admin",
			role: AdminRoleAgentOperator,
			expectedContains: []AdminPermission{
				PermAgentDiscover, PermAgentRegister, PermAgentSpawn,
				PermAgentPrompt, PermAgentClose, PermAgentDecompose,
				PermAgentCacheRead, PermAgentCacheWrite,
			},
			expectedExcludes: []AdminPermission{
				PermAgentAdmin,
			},
		},
		{
			name: "AgentViewer has only discover and cache read",
			role: AdminRoleAgentViewer,
			expectedContains: []AdminPermission{
				PermAgentDiscover, PermAgentCacheRead,
			},
			expectedExcludes: []AdminPermission{
				PermAgentRegister, PermAgentSpawn, PermAgentPrompt,
				PermAgentClose, PermAgentDecompose, PermAgentCacheWrite, PermAgentAdmin,
			},
		},
		{
			name:             "Invalid role returns nil",
			role:             "invalid_role",
			expectedContains: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := GetDefaultPermissions(tt.role)

			if tt.expectedContains == nil {
				assert.Nil(t, perms)
				return
			}

			if tt.expectedMinLength > 0 {
				assert.GreaterOrEqual(t, len(perms), tt.expectedMinLength)
			}

			for _, perm := range tt.expectedContains {
				assert.Contains(t, perms, perm, "Expected permission %s for role %s", perm, tt.role)
			}

			for _, perm := range tt.expectedExcludes {
				assert.NotContains(t, perms, perm, "Did not expect permission %s for role %s", perm, tt.role)
			}
		})
	}
}

// TestIsValidAdminRole tests role validation.
func TestIsValidAdminRole(t *testing.T) {
	tests := []struct {
		name     string
		role     AdminRole
		expected bool
	}{
		{"SuperAdmin is valid", AdminRoleSuperAdmin, true},
		{"TenantAdmin is valid", AdminRoleTenantAdmin, true},
		{"UserAdmin is valid", AdminRoleUserAdmin, true},
		{"GroupAdmin is valid", AdminRoleGroupAdmin, true},
		{"PolicyAdmin is valid", AdminRolePolicyAdmin, true},
		{"AuditAdmin is valid", AdminRoleAuditAdmin, true},
		{"ReadOnly is valid", AdminRoleReadOnly, true},
		{"AgentAdmin is valid", AdminRoleAgentAdmin, true},
		{"AgentOperator is valid", AdminRoleAgentOperator, true},
		{"AgentViewer is valid", AdminRoleAgentViewer, true},
		{"Empty role is invalid", "", false},
		{"Invalid role is invalid", "invalid_role", false},
		{"Custom role is invalid", "custom_admin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidAdminRole(tt.role)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestAgentPermissionsDistinct verifies all agent permissions are unique values.
func TestAgentPermissionsDistinct(t *testing.T) {
	agentPerms := []AdminPermission{
		PermAgentDiscover, PermAgentRegister, PermAgentSpawn,
		PermAgentPrompt, PermAgentClose, PermAgentDecompose,
		PermAgentCacheRead, PermAgentCacheWrite, PermAgentAdmin,
	}

	seen := make(map[AdminPermission]bool, len(agentPerms))
	for _, perm := range agentPerms {
		assert.False(t, seen[perm], "duplicate agent permission: %s", perm)
		seen[perm] = true
	}

	assert.Len(t, seen, 9, "expected exactly 9 distinct agent permissions")
}

// TestSuperAdminHasAgentPermissions verifies SuperAdmin includes all agent permissions.
func TestSuperAdminHasAgentPermissions(t *testing.T) {
	perms := GetDefaultPermissions(AdminRoleSuperAdmin)

	agentPerms := []AdminPermission{
		PermAgentDiscover, PermAgentRegister, PermAgentSpawn,
		PermAgentPrompt, PermAgentClose, PermAgentDecompose,
		PermAgentCacheRead, PermAgentCacheWrite, PermAgentAdmin,
	}

	for _, ap := range agentPerms {
		assert.Contains(t, perms, ap, "SuperAdmin missing agent permission: %s", ap)
	}
}

// TestAgentRolePermissionCounts verifies the exact permission counts for agent roles.
func TestAgentRolePermissionCounts(t *testing.T) {
	tests := []struct {
		name          string
		role          AdminRole
		expectedCount int
	}{
		{"AgentAdmin has 9 permissions", AdminRoleAgentAdmin, 9},
		{"AgentOperator has 8 permissions", AdminRoleAgentOperator, 8},
		{"AgentViewer has 2 permissions", AdminRoleAgentViewer, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			perms := GetDefaultPermissions(tt.role)
			assert.Len(t, perms, tt.expectedCount)
		})
	}
}

// TestAgentAdminAssignmentValidation verifies agent roles pass assignment validation.
func TestAgentAdminAssignmentValidation(t *testing.T) {
	roles := []AdminRole{AdminRoleAgentAdmin, AdminRoleAgentOperator, AdminRoleAgentViewer}

	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			assignment := &AdminAssignment{
				ID:        "adm_agent_" + string(role),
				UserID:    "user123",
				AdminRole: role,
				GrantedBy: "admin456",
				GrantedAt: time.Now().UTC(),
				Active:    true,
			}
			err := assignment.Validate()
			assert.NoError(t, err)
		})
	}
}

// TestAdminAssignment_Validate tests assignment validation.
func TestAdminAssignment_Validate(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name       string
		assignment *AdminAssignment
		wantErr    bool
		errCheck   func(t *testing.T, err error)
	}{
		{
			name: "Valid assignment",
			assignment: &AdminAssignment{
				ID:        "adm_123",
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
				GrantedBy: "admin456",
				GrantedAt: now,
				Active:    true,
			},
			wantErr: false,
		},
		{
			name: "Missing user_id",
			assignment: &AdminAssignment{
				ID:        "adm_123",
				AdminRole: AdminRoleUserAdmin,
				GrantedBy: "admin456",
				GrantedAt: now,
				Active:    true,
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "user_id is required")
			},
		},
		{
			name: "Invalid admin_role",
			assignment: &AdminAssignment{
				ID:        "adm_123",
				UserID:    "user123",
				AdminRole: "invalid_role",
				GrantedBy: "admin456",
				GrantedAt: now,
				Active:    true,
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "invalid admin_role")
			},
		},
		{
			name: "Missing granted_by",
			assignment: &AdminAssignment{
				ID:        "adm_123",
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
				GrantedAt: now,
				Active:    true,
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "granted_by is required")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.assignment.Validate()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// MemoryAdminStorage tests

// TestMemoryAdminStorage_CreateAssignment tests creating assignments in storage.
func TestMemoryAdminStorage_CreateAssignment(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAdminStorage()

	assignment := createTestAssignment("user123", AdminRoleUserAdmin)

	tests := []struct {
		name       string
		assignment *AdminAssignment
		wantErr    bool
		errCheck   func(t *testing.T, err error)
	}{
		{
			name:       "Create valid assignment",
			assignment: assignment,
			wantErr:    false,
		},
		{
			name:       "Create duplicate assignment",
			assignment: assignment,
			wantErr:    true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, isErrorCode(err, dberrors.ErrAlreadyExists))
			},
		},
		{
			name:       "Nil assignment",
			assignment: nil,
			wantErr:    true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Assignment with empty ID",
			assignment: &AdminAssignment{
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
				GrantedBy: "admin456",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.CreateAssignment(ctx, tt.assignment)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMemoryAdminStorage_GetAssignment tests retrieving assignments from storage.
func TestMemoryAdminStorage_GetAssignment(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAdminStorage()

	assignment := createTestAssignment("user123", AdminRoleUserAdmin)
	err := storage.CreateAssignment(ctx, assignment)
	require.NoError(t, err)

	tests := []struct {
		name     string
		id       string
		wantErr  bool
		errCheck func(t *testing.T, err error)
		validate func(t *testing.T, a *AdminAssignment)
	}{
		{
			name:    "Get existing assignment",
			id:      assignment.ID,
			wantErr: false,
			validate: func(t *testing.T, a *AdminAssignment) {
				assert.Equal(t, assignment.ID, a.ID)
				assert.Equal(t, "user123", a.UserID)
			},
		},
		{
			name:    "Get non-existent assignment",
			id:      "non_existent",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsNotFound(err))
			},
		},
		{
			name:    "Empty ID",
			id:      "",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := storage.GetAssignment(ctx, tt.id)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, a)
				}
			}
		})
	}
}

// TestMemoryAdminStorage_UpdateAssignment tests updating assignments in storage.
func TestMemoryAdminStorage_UpdateAssignment(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAdminStorage()

	assignment := createTestAssignment("user123", AdminRoleUserAdmin)
	err := storage.CreateAssignment(ctx, assignment)
	require.NoError(t, err)

	updatedAssignment := *assignment
	updatedAssignment.AdminRole = AdminRoleTenantAdmin

	tests := []struct {
		name       string
		assignment *AdminAssignment
		wantErr    bool
		errCheck   func(t *testing.T, err error)
	}{
		{
			name:       "Update existing assignment",
			assignment: &updatedAssignment,
			wantErr:    false,
		},
		{
			name: "Update non-existent assignment",
			assignment: &AdminAssignment{
				ID:        "non_existent",
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
				GrantedBy: "admin456",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsNotFound(err))
			},
		},
		{
			name:       "Nil assignment",
			assignment: nil,
			wantErr:    true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Empty ID",
			assignment: &AdminAssignment{
				UserID:    "user123",
				AdminRole: AdminRoleUserAdmin,
				GrantedBy: "admin456",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.UpdateAssignment(ctx, tt.assignment)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMemoryAdminStorage_DeleteAssignment tests deleting assignments from storage.
func TestMemoryAdminStorage_DeleteAssignment(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAdminStorage()

	assignment := createTestAssignment("user123", AdminRoleUserAdmin)
	err := storage.CreateAssignment(ctx, assignment)
	require.NoError(t, err)

	tests := []struct {
		name     string
		id       string
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "Delete existing assignment",
			id:      assignment.ID,
			wantErr: false,
		},
		{
			name:    "Delete non-existent assignment",
			id:      "non_existent",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsNotFound(err))
			},
		},
		{
			name:    "Empty ID",
			id:      "",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.DeleteAssignment(ctx, tt.id)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMemoryAdminStorage_ListAssignments tests listing assignments with filters.
func TestMemoryAdminStorage_ListAssignments(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAdminStorage()

	// Create test data
	pastTime := time.Now().UTC().Add(-24 * time.Hour)
	futureTime := time.Now().UTC().Add(24 * time.Hour)

	assignments := []*AdminAssignment{
		createTestAssignment("user1", AdminRoleSuperAdmin),
		createTestAssignment("user2", AdminRoleTenantAdmin),
		createTestAssignmentWithExpiry("user3", AdminRoleUserAdmin, pastTime),
		createTestAssignmentWithExpiry("user4", AdminRoleGroupAdmin, futureTime),
	}

	// Set different tenant IDs
	assignments[1].TenantIDs = []string{"tenant1"}
	assignments[2].TenantIDs = []string{"tenant2"}

	// Mark one as inactive
	assignments[3].Active = false

	for _, a := range assignments {
		err := storage.CreateAssignment(ctx, a)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		opts          ListAdminAssignmentsOptions
		expectedCount int
		validate      func(t *testing.T, result []*AdminAssignment)
	}{
		{
			name:          "List all",
			opts:          ListAdminAssignmentsOptions{},
			expectedCount: 4,
		},
		{
			name: "Filter by user_id",
			opts: ListAdminAssignmentsOptions{
				UserID: "user2",
			},
			expectedCount: 1,
			validate: func(t *testing.T, result []*AdminAssignment) {
				assert.Equal(t, "user2", result[0].UserID)
			},
		},
		{
			name: "Filter by admin_role",
			opts: ListAdminAssignmentsOptions{
				AdminRole: AdminRoleSuperAdmin,
			},
			expectedCount: 1,
		},
		{
			name: "Filter by tenant_id",
			opts: ListAdminAssignmentsOptions{
				TenantID: "tenant1",
			},
			// Only user2's assignment names tenant1. Empty TenantIDs is
			// system-tenant scope, not all-tenants, so the unscoped
			// assignments no longer appear under a tenant filter (#357).
			expectedCount: 1,
		},
		{
			name: "Filter active only",
			opts: ListAdminAssignmentsOptions{
				ActiveOnly: true,
			},
			expectedCount: 2, // Excludes expired and inactive
		},
		{
			name: "Pagination with limit",
			opts: ListAdminAssignmentsOptions{
				Limit: 2,
			},
			expectedCount: 2,
		},
		{
			name: "Pagination with offset",
			opts: ListAdminAssignmentsOptions{
				Offset: 2,
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := storage.ListAssignments(ctx, tt.opts)
			assert.NoError(t, err)
			assert.Len(t, result, tt.expectedCount)

			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

// TestMemoryAdminStorage_GetUserAssignments tests getting assignments for a user.
func TestMemoryAdminStorage_GetUserAssignments(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryAdminStorage()

	// Create test data
	pastTime := time.Now().UTC().Add(-24 * time.Hour)

	user1Assignments := []*AdminAssignment{
		createTestAssignment("user1", AdminRoleSuperAdmin),
		createTestAssignment("user1", AdminRoleTenantAdmin),
	}

	// One expired
	expiredAssignment := createTestAssignmentWithExpiry("user1", AdminRoleUserAdmin, pastTime)
	expiredAssignment.ID = "expired_assignment"

	// One inactive
	inactiveAssignment := createTestAssignment("user1", AdminRoleGroupAdmin)
	inactiveAssignment.ID = "inactive_assignment"
	inactiveAssignment.Active = false

	allAssignments := append(user1Assignments, expiredAssignment, inactiveAssignment)

	for _, a := range allAssignments {
		err := storage.CreateAssignment(ctx, a)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		userID        string
		expectedCount int
		wantErr       bool
		errCheck      func(t *testing.T, err error)
	}{
		{
			name:          "Get user assignments",
			userID:        "user1",
			expectedCount: 3, // Active assignments (including expired, for proper error messaging)
			wantErr:       false,
		},
		{
			name:          "User with no assignments",
			userID:        "user999",
			expectedCount: 0,
			wantErr:       false,
		},
		{
			name:    "Empty user_id",
			userID:  "",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assignments, err := storage.GetUserAssignments(ctx, tt.userID)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				assert.Len(t, assignments, tt.expectedCount)
			}
		})
	}
}

// MemoryImpersonationStorage tests

// TestMemoryImpersonationStorage_CreateSession tests creating impersonation sessions.
func TestMemoryImpersonationStorage_CreateSession(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryImpersonationStorage()

	now := time.Now().UTC()
	session := &ImpersonationSession{
		ID:           "imp_123",
		AdminUserID:  "admin1",
		TargetUserID: "user1",
		TenantID:     "tenant1",
		Reason:       "Test",
		StartedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
		AuditLogID:   "audit_123",
	}

	tests := []struct {
		name     string
		session  *ImpersonationSession
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "Create valid session",
			session: session,
			wantErr: false,
		},
		{
			name:    "Create duplicate session",
			session: session,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, isErrorCode(err, dberrors.ErrAlreadyExists))
			},
		},
		{
			name:    "Nil session",
			session: nil,
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name: "Session with empty ID",
			session: &ImpersonationSession{
				AdminUserID:  "admin1",
				TargetUserID: "user1",
			},
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.CreateImpersonationSession(ctx, tt.session)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMemoryImpersonationStorage_GetSession tests retrieving impersonation sessions.
func TestMemoryImpersonationStorage_GetSession(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryImpersonationStorage()

	now := time.Now().UTC()
	session := &ImpersonationSession{
		ID:           "imp_123",
		AdminUserID:  "admin1",
		TargetUserID: "user1",
		TenantID:     "tenant1",
		Reason:       "Test",
		StartedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
		AuditLogID:   "audit_123",
	}

	err := storage.CreateImpersonationSession(ctx, session)
	require.NoError(t, err)

	tests := []struct {
		name     string
		id       string
		wantErr  bool
		errCheck func(t *testing.T, err error)
		validate func(t *testing.T, s *ImpersonationSession)
	}{
		{
			name:    "Get existing session",
			id:      session.ID,
			wantErr: false,
			validate: func(t *testing.T, s *ImpersonationSession) {
				assert.Equal(t, session.ID, s.ID)
				assert.Equal(t, "admin1", s.AdminUserID)
			},
		},
		{
			name:    "Get non-existent session",
			id:      "non_existent",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsNotFound(err))
			},
		},
		{
			name:    "Empty ID",
			id:      "",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := storage.GetImpersonationSession(ctx, tt.id)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, s)
				}
			}
		})
	}
}

// TestMemoryImpersonationStorage_EndSession tests ending impersonation sessions.
func TestMemoryImpersonationStorage_EndSession(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryImpersonationStorage()

	now := time.Now().UTC()
	session := &ImpersonationSession{
		ID:           "imp_123",
		AdminUserID:  "admin1",
		TargetUserID: "user1",
		TenantID:     "tenant1",
		Reason:       "Test",
		StartedAt:    now,
		ExpiresAt:    now.Add(time.Hour),
		AuditLogID:   "audit_123",
	}

	err := storage.CreateImpersonationSession(ctx, session)
	require.NoError(t, err)

	tests := []struct {
		name     string
		id       string
		wantErr  bool
		errCheck func(t *testing.T, err error)
	}{
		{
			name:    "End existing session",
			id:      session.ID,
			wantErr: false,
		},
		{
			name:    "End non-existent session",
			id:      "non_existent",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsNotFound(err))
			},
		},
		{
			name:    "Empty ID",
			id:      "",
			wantErr: true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.EndImpersonationSession(ctx, tt.id)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)

				// Verify session is marked as ended
				s, err := storage.GetImpersonationSession(ctx, tt.id)
				require.NoError(t, err)
				assert.NotNil(t, s.EndedAt)
			}
		})
	}
}

// TestMemoryImpersonationStorage_ListActiveImpersonations tests listing active sessions.
func TestMemoryImpersonationStorage_ListActiveImpersonations(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryImpersonationStorage()

	now := time.Now().UTC()

	// Create sessions for different admins
	sessions := []*ImpersonationSession{
		{
			ID:           "imp_1",
			AdminUserID:  "admin1",
			TargetUserID: "user1",
			TenantID:     "tenant1",
			Reason:       "Test 1",
			StartedAt:    now,
			ExpiresAt:    now.Add(time.Hour),
			AuditLogID:   "audit_1",
		},
		{
			ID:           "imp_2",
			AdminUserID:  "admin1",
			TargetUserID: "user2",
			TenantID:     "tenant1",
			Reason:       "Test 2",
			StartedAt:    now,
			ExpiresAt:    now.Add(time.Hour),
			AuditLogID:   "audit_2",
		},
		{
			ID:           "imp_3",
			AdminUserID:  "admin2",
			TargetUserID: "user3",
			TenantID:     "tenant1",
			Reason:       "Test 3",
			StartedAt:    now,
			ExpiresAt:    now.Add(time.Hour),
			AuditLogID:   "audit_3",
		},
	}

	for _, s := range sessions {
		err := storage.CreateImpersonationSession(ctx, s)
		require.NoError(t, err)
	}

	// End one session
	err := storage.EndImpersonationSession(ctx, "imp_2")
	require.NoError(t, err)

	tests := []struct {
		name          string
		adminUserID   string
		expectedCount int
		wantErr       bool
		errCheck      func(t *testing.T, err error)
	}{
		{
			name:          "Get active sessions for admin1",
			adminUserID:   "admin1",
			expectedCount: 1, // Only imp_1, imp_2 is ended
			wantErr:       false,
		},
		{
			name:          "Get active sessions for admin2",
			adminUserID:   "admin2",
			expectedCount: 1,
			wantErr:       false,
		},
		{
			name:          "No sessions for admin",
			adminUserID:   "admin999",
			expectedCount: 0,
			wantErr:       false,
		},
		{
			name:        "Empty admin_user_id",
			adminUserID: "",
			wantErr:     true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessions, err := storage.ListActiveImpersonations(ctx, tt.adminUserID)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)
				assert.Len(t, sessions, tt.expectedCount)
			}
		})
	}
}

// TestMemoryImpersonationStorage_DeleteExpiredSessions tests cleanup of expired sessions.
func TestMemoryImpersonationStorage_DeleteExpiredSessions(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryImpersonationStorage()

	now := time.Now().UTC()
	pastTime := now.Add(-2 * time.Hour)

	sessions := []*ImpersonationSession{
		{
			ID:           "imp_active",
			AdminUserID:  "admin1",
			TargetUserID: "user1",
			TenantID:     "tenant1",
			Reason:       "Active",
			StartedAt:    now,
			ExpiresAt:    now.Add(time.Hour),
			AuditLogID:   "audit_1",
		},
		{
			ID:           "imp_expired",
			AdminUserID:  "admin1",
			TargetUserID: "user2",
			TenantID:     "tenant1",
			Reason:       "Expired",
			StartedAt:    pastTime,
			ExpiresAt:    pastTime.Add(time.Hour),
			AuditLogID:   "audit_2",
		},
		{
			ID:           "imp_ended",
			AdminUserID:  "admin1",
			TargetUserID: "user3",
			TenantID:     "tenant1",
			Reason:       "Ended",
			StartedAt:    now,
			ExpiresAt:    now.Add(time.Hour),
			EndedAt:      &now,
			AuditLogID:   "audit_3",
		},
	}

	for _, s := range sessions {
		err := storage.CreateImpersonationSession(ctx, s)
		require.NoError(t, err)
	}

	// Delete expired sessions
	err := storage.DeleteExpiredSessions(ctx)
	assert.NoError(t, err)

	// Verify only active session remains
	_, err = storage.GetImpersonationSession(ctx, "imp_active")
	assert.NoError(t, err)

	_, err = storage.GetImpersonationSession(ctx, "imp_expired")
	assert.True(t, dberrors.IsNotFound(err))

	_, err = storage.GetImpersonationSession(ctx, "imp_ended")
	assert.True(t, dberrors.IsNotFound(err))
}

// TestMatchResourcePattern tests the resource pattern matching function.
func TestMatchResourcePattern(t *testing.T) {
	tests := []struct {
		name       string
		pattern    string
		resourceID string
		expected   bool
	}{
		{
			name:       "Exact match",
			pattern:    "user:john",
			resourceID: "user:john",
			expected:   true,
		},
		{
			name:       "Exact match - no match",
			pattern:    "user:john",
			resourceID: "user:jane",
			expected:   false,
		},
		{
			name:       "Suffix wildcard match",
			pattern:    "user:*",
			resourceID: "user:john",
			expected:   true,
		},
		{
			name:       "Suffix wildcard match complex",
			pattern:    "group:engineering-*",
			resourceID: "group:engineering-frontend",
			expected:   true,
		},
		{
			name:       "Suffix wildcard - no match",
			pattern:    "user:*",
			resourceID: "group:admins",
			expected:   false,
		},
		{
			name:       "Prefix wildcard match",
			pattern:    "*:admin",
			resourceID: "user:admin",
			expected:   true,
		},
		{
			name:       "Prefix wildcard match complex",
			pattern:    "*-manager",
			resourceID: "project-manager",
			expected:   true,
		},
		{
			name:       "Prefix wildcard - no match",
			pattern:    "*:admin",
			resourceID: "user:guest",
			expected:   false,
		},
		{
			name:       "Empty pattern",
			pattern:    "",
			resourceID: "user:john",
			expected:   false,
		},
		{
			name:       "Empty resource ID",
			pattern:    "user:*",
			resourceID: "",
			expected:   false,
		},
		{
			name:       "Both empty",
			pattern:    "",
			resourceID: "",
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchResourcePattern(tt.pattern, tt.resourceID)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestAdminManager_CheckPermission_ResourceScopeMatching tests resource scope matching in permission checks.
func TestAdminManager_CheckPermission_ResourceScopeMatching(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	// Create admin with resource scope restrictions
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "scoped_admin",
		AdminRole: AdminRoleUserAdmin,
		TenantIDs: []string{"tenant1"},
		GrantedBy: "system",
		ResourceScopes: []ResourceScope{
			{ResourceType: "user", ResourcePattern: "user:engineering-*"},
			{ResourceType: "group", ResourcePattern: "*-team"},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name          string
		request       *AdminCheckRequest
		expectAllowed bool
	}{
		{
			name: "Resource scope matches - suffix wildcard",
			request: &AdminCheckRequest{
				UserID:       "scoped_admin",
				Permission:   PermUserUpdate,
				TenantID:     "tenant1",
				ResourceType: "user",
				ResourceID:   "user:engineering-lead",
			},
			expectAllowed: true,
		},
		{
			name: "Resource scope matches - prefix wildcard",
			request: &AdminCheckRequest{
				UserID:       "scoped_admin",
				Permission:   PermGroupRead,
				TenantID:     "tenant1",
				ResourceType: "group",
				ResourceID:   "frontend-team",
			},
			expectAllowed: true,
		},
		{
			name: "Resource scope does not match",
			request: &AdminCheckRequest{
				UserID:       "scoped_admin",
				Permission:   PermUserUpdate,
				TenantID:     "tenant1",
				ResourceType: "user",
				ResourceID:   "user:marketing-lead",
			},
			expectAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := manager.CheckPermission(ctx, tt.request)
			require.NoError(t, err)
			assert.Equal(t, tt.expectAllowed, resp.Allowed)
		})
	}
}

func TestAdminManager_RecordImpersonatedOperation(t *testing.T) {
	ctx := context.Background()
	manager, auditLogger := createTestAdminManager(t)

	// Create admin with impersonation permission
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	// Start impersonation session
	session, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:    "admin_user",
		AdminUsername:  "admin@example.com",
		TargetUserID:   "target_user",
		TargetUsername: "target@example.com",
		TenantID:       "tenant1",
		Reason:         "test operation recording",
		Duration:       time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, session)

	// Clear audit log
	auditLogger.events = nil

	tests := []struct {
		name      string
		sessionID string
		operation string
		resource  *AuditResource
		outcome   AuditOutcome
		details   map[string]interface{}
		wantErr   bool
		errCheck  func(t *testing.T, err error)
	}{
		{
			name:      "Valid operation recording",
			sessionID: session.ID,
			operation: "document.read",
			resource: &AuditResource{
				Type: "document",
				ID:   "doc123",
				Name: "Test Document",
			},
			outcome: AuditOutcomeSuccess,
			details: map[string]interface{}{
				"file_size": 1024,
			},
			wantErr: false,
		},
		{
			name:      "Empty session ID",
			sessionID: "",
			operation: "test.action",
			wantErr:   true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name:      "Empty operation",
			sessionID: session.ID,
			operation: "",
			wantErr:   true,
			errCheck: func(t *testing.T, err error) {
				assert.True(t, dberrors.IsInvalidInput(err))
			},
		},
		{
			name:      "Non-existent session",
			sessionID: "non_existent_session",
			operation: "test.action",
			wantErr:   true,
			errCheck: func(t *testing.T, err error) {
				assert.Equal(t, ErrImpersonationSessionNotFound, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auditLogger.events = nil

			err := manager.RecordImpersonatedOperation(ctx, tt.sessionID, tt.operation, tt.resource, tt.outcome, tt.details)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errCheck != nil {
					tt.errCheck(t, err)
				}
			} else {
				assert.NoError(t, err)

				// Verify audit log was created
				require.Len(t, auditLogger.events, 1)
				event := auditLogger.events[0]
				assert.Equal(t, tt.operation, event.Action)
				assert.Equal(t, session.ID, event.ImpersonationSessionID)
				assert.Equal(t, session.ID, event.CorrelationID)
				assert.Equal(t, AuditCategorySecurity, event.Category)

				// Verify actor shows impersonation
				require.NotNil(t, event.Actor)
				assert.Equal(t, "target_user", event.Actor.UserID)
				assert.Equal(t, "target@example.com", event.Actor.Username)
				assert.Equal(t, "admin_user", event.Actor.ImpersonatingUserID)
				assert.Equal(t, "admin@example.com", event.Actor.ImpersonatingUsername)

				// Verify details include operation number
				require.NotNil(t, event.Details)
				assert.Contains(t, event.Details, "operation_number")
				assert.Contains(t, event.Details, "impersonated_user_id")
				assert.Contains(t, event.Details, "impersonated_username")
			}
		})
	}
}

func TestAdminManager_RecordImpersonatedOperation_EndedSession(t *testing.T) {
	ctx := context.Background()
	manager, _ := createTestAdminManager(t)

	// Create admin with impersonation permission
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	// Start impersonation session
	session, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "admin_user",
		TargetUserID: "target_user",
		TenantID:     "tenant1",
		Reason:       "test ended session",
		Duration:     time.Hour,
	})
	require.NoError(t, err)

	// End the session
	err = manager.EndImpersonation(ctx, session.ID)
	require.NoError(t, err)

	// Try to record operation on ended session
	err = manager.RecordImpersonatedOperation(ctx, session.ID, "test.action", nil, AuditOutcomeSuccess, nil)
	assert.Error(t, err)
	assert.True(t, dberrors.IsInvalidInput(err))
}

func TestAdminManager_ImpersonationAuditLogging(t *testing.T) {
	ctx := context.Background()
	manager, auditLogger := createTestAdminManager(t)

	// Create admin with impersonation permission
	_, err := manager.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:      "admin_user",
		AdminRole:   AdminRoleSuperAdmin,
		TenantIDs:   []string{"tenant1"},
		Permissions: []AdminPermission{PermUserImpersonate},
		GrantedBy:   "system",
	})
	require.NoError(t, err)

	// Clear audit log
	auditLogger.events = nil

	// Start impersonation session
	session, err := manager.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:    "admin_user",
		AdminUsername:  "admin@example.com",
		TargetUserID:   "target_user",
		TargetUsername: "target@example.com",
		TenantID:       "tenant1",
		Reason:         "test audit logging",
		Duration:       time.Hour,
	})
	require.NoError(t, err)

	// Verify start event
	require.Len(t, auditLogger.events, 1)
	startEvent := auditLogger.events[0]
	assert.Equal(t, "admin.impersonation.start", startEvent.Action)
	assert.Equal(t, session.ID, startEvent.ImpersonationSessionID)
	assert.Equal(t, session.ID, startEvent.CorrelationID)
	assert.Equal(t, AuditCategorySecurity, startEvent.Category)
	assert.Equal(t, AuditEventSessionEvent, startEvent.EventType)

	// Verify start event details
	require.NotNil(t, startEvent.Details)
	assert.Equal(t, "target_user", startEvent.Details["target_user_id"])
	assert.Equal(t, "target@example.com", startEvent.Details["target_username"])
	assert.Equal(t, "test audit logging", startEvent.Details["reason"])
	assert.Contains(t, startEvent.Details, "duration")
	assert.Contains(t, startEvent.Details, "expires_at")

	// Record some operations
	for i := 0; i < 3; i++ {
		err = manager.RecordImpersonatedOperation(ctx, session.ID, "test.operation", nil, AuditOutcomeSuccess, nil)
		require.NoError(t, err)
	}

	// Verify operation count increased
	updatedSession, err := manager.GetImpersonationSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), updatedSession.OperationCount)

	// Clear audit log before ending
	auditLogger.events = nil

	// End the session
	err = manager.EndImpersonation(ctx, session.ID)
	require.NoError(t, err)

	// Verify end event
	require.Len(t, auditLogger.events, 1)
	endEvent := auditLogger.events[0]
	assert.Equal(t, "admin.impersonation.end", endEvent.Action)
	assert.Equal(t, session.ID, endEvent.ImpersonationSessionID)
	assert.Equal(t, session.ID, endEvent.CorrelationID)
	assert.Equal(t, AuditCategorySecurity, endEvent.Category)

	// Verify end event details include summary
	require.NotNil(t, endEvent.Details)
	assert.Equal(t, "target_user", endEvent.Details["target_user_id"])
	assert.Equal(t, "target@example.com", endEvent.Details["target_username"])
	assert.Equal(t, "test audit logging", endEvent.Details["reason"])
	assert.Contains(t, endEvent.Details, "started_at")
	assert.Contains(t, endEvent.Details, "ended_at")
	assert.Contains(t, endEvent.Details, "duration_seconds")
	assert.Contains(t, endEvent.Details, "duration_human")
	assert.Equal(t, int64(3), endEvent.Details["operation_count"])
}

func TestMemoryImpersonationStorage_IncrementOperationCount(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryImpersonationStorage()

	// Create a session
	session := &ImpersonationSession{
		ID:             "test_session",
		AdminUserID:    "admin1",
		TargetUserID:   "user1",
		TenantID:       "tenant1",
		Reason:         "test",
		StartedAt:      time.Now().UTC(),
		ExpiresAt:      time.Now().UTC().Add(time.Hour),
		OperationCount: 0,
	}
	err := storage.CreateImpersonationSession(ctx, session)
	require.NoError(t, err)

	// Test incrementing operation count
	count, err := storage.IncrementOperationCount(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Increment again
	count, err = storage.IncrementOperationCount(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Verify the stored value
	retrieved, err := storage.GetImpersonationSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), retrieved.OperationCount)
}

func TestMemoryImpersonationStorage_IncrementOperationCount_Errors(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryImpersonationStorage()

	t.Run("EmptyID", func(t *testing.T) {
		count, err := storage.IncrementOperationCount(ctx, "")
		assert.Error(t, err)
		assert.True(t, dberrors.IsInvalidInput(err))
		assert.Equal(t, int64(0), count)
	})

	t.Run("NonExistentSession", func(t *testing.T) {
		count, err := storage.IncrementOperationCount(ctx, "non_existent")
		assert.Error(t, err)
		assert.True(t, dberrors.IsNotFound(err))
		assert.Equal(t, int64(0), count)
	})
}
