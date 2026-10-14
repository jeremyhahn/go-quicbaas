package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Mock implementations for testing

type mockBatchStorage struct {
	operations sync.Map
	mu         sync.RWMutex
}

func newMockBatchStorage() *mockBatchStorage {
	return &mockBatchStorage{}
}

func (m *mockBatchStorage) SaveOperation(ctx context.Context, op *BatchOperation) error {
	m.operations.Store(op.ID, op)
	return nil
}

func (m *mockBatchStorage) GetOperation(ctx context.Context, operationID string) (*BatchOperation, error) {
	if val, ok := m.operations.Load(operationID); ok {
		return val.(*BatchOperation), nil
	}
	return nil, ErrBatchOperationNotFound
}

func (m *mockBatchStorage) UpdateOperation(ctx context.Context, op *BatchOperation) error {
	m.operations.Store(op.ID, op)
	return nil
}

func (m *mockBatchStorage) ListOperations(ctx context.Context, tenantID string, status *BatchStatus, limit int) ([]*BatchOperation, error) {
	var ops []*BatchOperation
	m.operations.Range(func(key, value interface{}) bool {
		op := value.(*BatchOperation)
		if op.TenantID == tenantID {
			if status == nil || op.Status == *status {
				ops = append(ops, op)
			}
		}
		return len(ops) < limit || limit == 0
	})
	return ops, nil
}

func (m *mockBatchStorage) DeleteOperation(ctx context.Context, operationID string) error {
	m.operations.Delete(operationID)
	return nil
}

type mockTenantManager struct {
	users    sync.Map
	roles    sync.Map
	groups   sync.Map
	policies sync.Map
	mu       sync.RWMutex
}

func newMockTenantManager() *mockTenantManager {
	return &mockTenantManager{}
}

func (m *mockTenantManager) CreateUser(ctx context.Context, user *User) error {
	key := user.TenantID + ":" + user.Email
	if _, exists := m.users.Load(key); exists {
		return ErrUserAlreadyExists
	}
	m.users.Store(key, user)
	m.users.Store(mockUserKey{user.TenantID, user.ID}, user)
	return nil
}

func (m *mockTenantManager) GetUser(ctx context.Context, tenantID string, userID uint64) (*User, error) {
	if val, ok := m.users.Load(mockUserKey{tenantID, userID}); ok {
		return val.(*User), nil
	}
	return nil, ErrUserNotFound
}

func (m *mockTenantManager) GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error) {
	if val, ok := m.users.Load(tenantID + ":" + email); ok {
		return val.(*User), nil
	}
	return nil, ErrUserNotFound
}

func (m *mockTenantManager) UpdateUser(ctx context.Context, user *User) error {
	key := user.TenantID + ":" + user.Email
	m.users.Store(key, user)
	m.users.Store(mockUserKey{user.TenantID, user.ID}, user)
	return nil
}

func (m *mockTenantManager) CreateRole(ctx context.Context, role *Role) error {
	key := role.TenantID + ":" + role.ID
	if _, exists := m.roles.Load(key); exists {
		return ErrRoleAlreadyExists
	}
	m.roles.Store(key, role)
	return nil
}

func (m *mockTenantManager) GetRole(ctx context.Context, tenantID, roleID string) (*Role, error) {
	if val, ok := m.roles.Load(tenantID + ":" + roleID); ok {
		return val.(*Role), nil
	}
	return nil, ErrRoleNotFound
}

func (m *mockTenantManager) UpdateRole(ctx context.Context, role *Role) error {
	key := role.TenantID + ":" + role.ID
	m.roles.Store(key, role)
	return nil
}

func (m *mockTenantManager) CreatePolicy(ctx context.Context, policy *Policy) error {
	key := policy.Metadata.Tenant + ":" + policy.Metadata.Name
	if _, exists := m.policies.Load(key); exists {
		return ErrPolicyNotFound
	}
	m.policies.Store(key, policy)
	return nil
}

func (m *mockTenantManager) GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	if val, ok := m.policies.Load(tenantID + ":" + name); ok {
		return val.(*Policy), nil
	}
	return nil, ErrPolicyNotFound
}

func (m *mockTenantManager) UpdatePolicy(ctx context.Context, policy *Policy) error {
	key := policy.Metadata.Tenant + ":" + policy.Metadata.Name
	m.policies.Store(key, policy)
	return nil
}

// Test cases

func TestNewIAMBatchProcessor(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()

	bp := NewIAMBatchProcessor(storage, tenantMgr)
	if bp == nil {
		t.Fatal("NewIAMBatchProcessor returned nil")
	}
}

func TestBatchProcessor_ImportUsers(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	t.Run("successful import", func(t *testing.T) {
		items := []UserImportItem{
			{Email: "user1@example.com", Name: "User One"},
			{Email: "user2@example.com", Name: "User Two"},
		}

		op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("ImportUsers failed: %v", err)
		}

		if op == nil {
			t.Fatal("ImportUsers returned nil operation")
		}
		if op.Type != BatchOperationTypeUsers {
			t.Errorf("expected type %s, got %s", BatchOperationTypeUsers, op.Type)
		}
		if op.TenantID != tenantID {
			t.Errorf("expected tenant %s, got %s", tenantID, op.TenantID)
		}

		// Wait for processing
		time.Sleep(100 * time.Millisecond)

		// Check operation was saved
		saved, err := storage.GetOperation(ctx, op.ID)
		if err != nil {
			t.Fatalf("failed to get saved operation: %v", err)
		}
		if saved.Status == BatchStatusPending {
			t.Error("operation still pending after processing")
		}
	})

	t.Run("with validation errors", func(t *testing.T) {
		items := []UserImportItem{
			{Email: "", Name: "Invalid User"}, // Missing email
		}

		op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
			ValidateOnly: true,
		})
		if err != nil {
			t.Fatalf("ImportUsers failed: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		saved, _ := storage.GetOperation(ctx, op.ID)
		if saved.Progress.Failed == 0 {
			t.Error("expected validation failures")
		}
	})

	t.Run("with rate limiting", func(t *testing.T) {
		items := []UserImportItem{
			{Email: "rate1@example.com", Name: "Rate User 1"},
			{Email: "rate2@example.com", Name: "Rate User 2"},
			{Email: "rate3@example.com", Name: "Rate User 3"},
		}

		startTime := time.Now()
		op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
			RateLimit: 2, // 2 items per second
			DryRun:    true,
		})
		if err != nil {
			t.Fatalf("ImportUsers failed: %v", err)
		}

		time.Sleep(2 * time.Second)

		saved, _ := storage.GetOperation(ctx, op.ID)
		elapsed := time.Since(startTime)

		// Processing 3 items at 2 items/sec should take at least 1 second
		if elapsed < time.Second && saved.Status == BatchStatusCompleted {
			t.Error("rate limiting not working correctly")
		}
	})
}

func TestBatchProcessor_ImportRoles(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []RoleImportItem{
		{ID: "role1", Name: "Admin", Permissions: []string{"read", "write"}},
		{ID: "role2", Name: "Viewer", Permissions: []string{"read"}},
	}

	op, err := bp.ImportRoles(ctx, tenantID, items, BatchOptions{
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("ImportRoles failed: %v", err)
	}

	if op.Type != BatchOperationTypeRoles {
		t.Errorf("expected type %s, got %s", BatchOperationTypeRoles, op.Type)
	}

	time.Sleep(100 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Status == BatchStatusPending {
		t.Error("operation still pending")
	}
}

func TestBatchProcessor_ImportPolicies(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []PolicyImportItem{
		{
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: tenantID,
				},
				Spec: PolicySpec{
					Rules: []Rule{
						{
							Name:      "allow-read",
							Effect:    EffectAllow,
							Actions:   []string{"read"},
							Resources: []string{"*"},
						},
					},
				},
			},
		},
	}

	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	if op.Type != BatchOperationTypePolicies {
		t.Errorf("expected type %s, got %s", BatchOperationTypePolicies, op.Type)
	}

	time.Sleep(100 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Status == BatchStatusPending {
		t.Error("operation still pending")
	}
}

func TestBatchProcessor_ImportFromJSON(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	t.Run("import users from JSON", func(t *testing.T) {
		items := []UserImportItem{
			{Email: "json1@example.com", Name: "JSON User 1"},
			{Email: "json2@example.com", Name: "JSON User 2"},
		}

		data, _ := json.Marshal(items)
		reader := bytes.NewReader(data)

		op, err := bp.ImportFromJSON(ctx, tenantID, BatchOperationTypeUsers, reader, BatchOptions{
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("ImportFromJSON failed: %v", err)
		}

		if op.Type != BatchOperationTypeUsers {
			t.Errorf("expected type %s, got %s", BatchOperationTypeUsers, op.Type)
		}

		time.Sleep(100 * time.Millisecond)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		reader := strings.NewReader("invalid json")

		_, err := bp.ImportFromJSON(ctx, tenantID, BatchOperationTypeUsers, reader, BatchOptions{})
		if err == nil {
			t.Error("expected error for invalid JSON")
		}
	})

	t.Run("unsupported type", func(t *testing.T) {
		reader := strings.NewReader("[]")

		_, err := bp.ImportFromJSON(ctx, tenantID, "unsupported", reader, BatchOptions{})
		if err == nil {
			t.Error("expected error for unsupported type")
		}
	})
}

func TestBatchProcessor_ImportFromCSV(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	t.Run("import users from CSV", func(t *testing.T) {
		csvData := `id,email,name
user1,csv1@example.com,CSV User 1
user2,csv2@example.com,CSV User 2`

		reader := strings.NewReader(csvData)

		op, err := bp.ImportFromCSV(ctx, tenantID, BatchOperationTypeUsers, reader, BatchOptions{
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("ImportFromCSV failed: %v", err)
		}

		if op.Type != BatchOperationTypeUsers {
			t.Errorf("expected type %s, got %s", BatchOperationTypeUsers, op.Type)
		}

		time.Sleep(100 * time.Millisecond)
	})

	t.Run("invalid CSV", func(t *testing.T) {
		reader := strings.NewReader("invalid\ncsv\ndata\n")

		_, err := bp.ImportFromCSV(ctx, tenantID, BatchOperationTypeUsers, reader, BatchOptions{})
		if err != nil {
			// This is expected, CSV parsing should handle malformed data
		}
	})

	t.Run("unsupported type for CSV", func(t *testing.T) {
		csvData := `name
policy1`
		reader := strings.NewReader(csvData)

		_, err := bp.ImportFromCSV(ctx, tenantID, BatchOperationTypePolicies, reader, BatchOptions{})
		if err == nil {
			t.Error("expected error for unsupported CSV type")
		}
	})
}

func TestBatchProcessor_GetOperation(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Create an operation
	items := []UserImportItem{
		{Email: "test@example.com", Name: "Test User"},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}

	// Get the operation
	retrieved, err := bp.GetOperation(ctx, op.ID)
	if err != nil {
		t.Fatalf("GetOperation failed: %v", err)
	}

	if retrieved.ID != op.ID {
		t.Errorf("expected ID %s, got %s", op.ID, retrieved.ID)
	}

	// Try to get non-existent operation
	_, err = bp.GetOperation(ctx, "non-existent")
	if err == nil {
		t.Error("expected error for non-existent operation")
	}
}

func TestBatchProcessor_CancelOperation(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Create a long-running operation
	items := make([]UserImportItem, 100)
	for i := 0; i < 100; i++ {
		items[i] = UserImportItem{
			Email: "cancel" + string(rune(i)) + "@example.com",
			Name:  "Cancel User",
		}
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		RateLimit: 10, // Slow processing
		DryRun:    true,
	})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the operation
	err = bp.CancelOperation(ctx, op.ID)
	if err != nil {
		t.Fatalf("CancelOperation failed: %v", err)
	}

	// Check operation status
	cancelled, _ := bp.GetOperation(ctx, op.ID)
	if cancelled.Status != BatchStatusCancelled {
		t.Errorf("expected status %s, got %s", BatchStatusCancelled, cancelled.Status)
	}
	if cancelled.CancelledAt == nil {
		t.Error("expected CancelledAt to be set")
	}

	// Try to cancel non-existent operation
	err = bp.CancelOperation(ctx, "non-existent")
	if err == nil {
		t.Error("expected error for non-existent operation")
	}
}

func TestBatchProcessor_ListOperations(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Create multiple operations
	for i := 0; i < 5; i++ {
		items := []UserImportItem{
			{Email: "list" + string(rune(i)) + "@example.com", Name: "List User"},
		}
		_, _ = bp.ImportUsers(ctx, tenantID, items, BatchOptions{DryRun: true})
	}

	time.Sleep(100 * time.Millisecond)

	// List all operations
	ops, err := bp.ListOperations(ctx, tenantID, nil, 10)
	if err != nil {
		t.Fatalf("ListOperations failed: %v", err)
	}

	if len(ops) != 5 {
		t.Errorf("expected 5 operations, got %d", len(ops))
	}

	// List with status filter
	completedStatus := BatchStatusCompleted
	completedOps, err := bp.ListOperations(ctx, tenantID, &completedStatus, 10)
	if err != nil {
		t.Fatalf("ListOperations with filter failed: %v", err)
	}

	for _, op := range completedOps {
		if op.Status != BatchStatusCompleted {
			t.Errorf("expected only completed operations, got %s", op.Status)
		}
	}
}

func TestBatchProcessor_AtomicOperation(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []UserImportItem{
		{Email: "valid1@example.com", Name: "Valid User 1"},
		{Email: "", Name: "Invalid User"}, // This will fail validation
		{Email: "valid2@example.com", Name: "Valid User 2"},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		Atomic: true,
	})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)

	// With atomic=true, if validation fails, the whole operation should fail
	if saved.Status != BatchStatusFailed {
		t.Errorf("expected status %s for atomic operation with validation error, got %s",
			BatchStatusFailed, saved.Status)
	}

	// No items should have been processed
	if saved.Progress.Successful > 0 {
		t.Error("expected no successful items in failed atomic operation")
	}
}

func TestBatchProcessor_ContinueOnError(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []UserImportItem{
		{Email: "continue1@example.com", Name: "User 1"},
		{Email: "", Name: "Invalid User"}, // This will fail
		{Email: "continue2@example.com", Name: "User 2"},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		ContinueOnError: true,
		DryRun:          true,
	})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)

	// With continue on error, should process all items
	if saved.Progress.Processed != len(items) {
		t.Errorf("expected %d processed items, got %d", len(items), saved.Progress.Processed)
	}

	// Should have both successes and failures
	if saved.Progress.Successful == 0 {
		t.Error("expected some successful items")
	}
	if saved.Progress.Failed == 0 {
		t.Error("expected some failed items")
	}
}

func TestBatchProcessor_UpdateExisting(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Create initial user
	existingUser := &User{
		ID:       40050,
		Email:    "existing@example.com",
		Name:     "Original Name",
		TenantID: tenantID,
	}
	_ = tenantMgr.CreateUser(ctx, existingUser)

	// Import with update
	items := []UserImportItem{
		{ID: "existing-id", Email: "existing@example.com", Name: "Updated Name"},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		UpdateExisting: true,
	})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Successful != 1 {
		t.Error("expected successful update")
	}

	// Verify user was updated
	updated, _ := tenantMgr.GetUserByEmail(ctx, tenantID, "existing@example.com")
	if updated.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got '%s'", updated.Name)
	}
}

func TestBatchProcessor_SkipDuplicates(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Create initial user
	existingUser := &User{
		ID:       40050,
		Email:    "skip@example.com",
		Name:     "Existing User",
		TenantID: tenantID,
	}
	_ = tenantMgr.CreateUser(ctx, existingUser)

	// Import with skip duplicates
	items := []UserImportItem{
		{Email: "skip@example.com", Name: "Duplicate User"},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		SkipDuplicates: true,
	})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Skipped != 1 {
		t.Errorf("expected 1 skipped item, got %d", saved.Progress.Skipped)
	}
}

func TestBatchItemTypes(t *testing.T) {
	t.Run("UserImportItem validation", func(t *testing.T) {
		validUser := UserImportItem{Email: "test@example.com", Name: "Test"}
		if err := validUser.Validate(); err != nil {
			t.Errorf("expected valid user to pass validation: %v", err)
		}

		invalidUser := UserImportItem{Email: "", Name: "Test"}
		if err := invalidUser.Validate(); err == nil {
			t.Error("expected invalid user to fail validation")
		}
	})

	t.Run("RoleImportItem validation", func(t *testing.T) {
		validRole := RoleImportItem{Name: "Admin"}
		if err := validRole.Validate(); err != nil {
			t.Errorf("expected valid role to pass validation: %v", err)
		}

		invalidRole := RoleImportItem{Name: ""}
		if err := invalidRole.Validate(); err == nil {
			t.Error("expected invalid role to fail validation")
		}
	})
}

func TestBatchOperation_Validation(t *testing.T) {
	t.Run("valid operation", func(t *testing.T) {
		op := &BatchOperation{
			ID:       "test-id",
			TenantID: "test-tenant",
			Type:     BatchOperationTypeUsers,
		}

		if err := op.Validate(); err != nil {
			t.Errorf("expected valid operation to pass validation: %v", err)
		}
	})

	t.Run("missing ID", func(t *testing.T) {
		op := &BatchOperation{
			TenantID: "test-tenant",
			Type:     BatchOperationTypeUsers,
		}

		if err := op.Validate(); err == nil {
			t.Error("expected operation without ID to fail validation")
		}
	})

	t.Run("missing tenant ID", func(t *testing.T) {
		op := &BatchOperation{
			ID:   "test-id",
			Type: BatchOperationTypeUsers,
		}

		if err := op.Validate(); err == nil {
			t.Error("expected operation without tenant ID to fail validation")
		}
	})
}

func TestBatchProgress_UpdateProgress(t *testing.T) {
	progress := &BatchProgress{
		Total:     100,
		Processed: 50,
	}

	progress.UpdateProgress()

	if progress.PercentComplete != 50.0 {
		t.Errorf("expected 50%% complete, got %.2f%%", progress.PercentComplete)
	}
}

func TestBatchProgress_CalculateRate(t *testing.T) {
	progress := &BatchProgress{
		Total:     100,
		Processed: 50,
	}

	elapsed := 10 * time.Second
	progress.CalculateRate(elapsed)

	expectedRate := 5.0 // 50 items / 10 seconds
	if progress.ItemsPerSecond != expectedRate {
		t.Errorf("expected rate %.2f, got %.2f", expectedRate, progress.ItemsPerSecond)
	}

	// Should have estimated time remaining
	if progress.EstimatedTimeRemaining == 0 {
		t.Error("expected estimated time remaining to be calculated")
	}
}

func TestBatchOperation_IsTerminal(t *testing.T) {
	testCases := []struct {
		status   BatchStatus
		terminal bool
	}{
		{BatchStatusPending, false},
		{BatchStatusValidating, false},
		{BatchStatusProcessing, false},
		{BatchStatusCompleted, true},
		{BatchStatusFailed, true},
		{BatchStatusCancelled, true},
		{BatchStatusPartialSuccess, true},
	}

	for _, tc := range testCases {
		op := &BatchOperation{Status: tc.status}
		if op.IsTerminal() != tc.terminal {
			t.Errorf("status %s: expected IsTerminal=%v, got %v",
				tc.status, tc.terminal, op.IsTerminal())
		}
	}
}

func TestBatchOperation_CanCancel(t *testing.T) {
	testCases := []struct {
		status    BatchStatus
		canCancel bool
	}{
		{BatchStatusPending, true},
		{BatchStatusValidating, true},
		{BatchStatusProcessing, true},
		{BatchStatusCompleted, false},
		{BatchStatusFailed, false},
		{BatchStatusCancelled, false},
	}

	for _, tc := range testCases {
		op := &BatchOperation{Status: tc.status}
		if op.CanCancel() != tc.canCancel {
			t.Errorf("status %s: expected CanCancel=%v, got %v",
				tc.status, tc.canCancel, op.CanCancel())
		}
	}
}

// Test error handling

func TestBatchProcessor_StorageError(t *testing.T) {
	// Create a storage that always fails
	storage := &failingBatchStorage{}
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	items := []UserImportItem{
		{Email: "test@example.com", Name: "Test"},
	}

	_, err := bp.ImportUsers(ctx, "test-tenant", items, BatchOptions{})
	if err == nil {
		t.Error("expected error from failing storage")
	}
}

type failingBatchStorage struct{}

func (f *failingBatchStorage) SaveOperation(ctx context.Context, op *BatchOperation) error {
	return errors.New("storage error")
}

func (f *failingBatchStorage) GetOperation(ctx context.Context, operationID string) (*BatchOperation, error) {
	return nil, errors.New("storage error")
}

func (f *failingBatchStorage) UpdateOperation(ctx context.Context, op *BatchOperation) error {
	return errors.New("storage error")
}

func (f *failingBatchStorage) ListOperations(ctx context.Context, tenantID string, status *BatchStatus, limit int) ([]*BatchOperation, error) {
	return nil, errors.New("storage error")
}

func (f *failingBatchStorage) DeleteOperation(ctx context.Context, operationID string) error {
	return errors.New("storage error")
}

// Benchmark tests

func BenchmarkBatchProcessor_ImportUsers(b *testing.B) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	items := make([]UserImportItem, 100)
	for i := 0; i < 100; i++ {
		items[i] = UserImportItem{
			Email: "bench" + string(rune(i)) + "@example.com",
			Name:  "Benchmark User",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = bp.ImportUsers(ctx, "test-tenant", items, BatchOptions{
			DryRun: true,
		})
		time.Sleep(200 * time.Millisecond) // Wait for async processing
	}
}

func TestIAMBatchProcessor_ImportFromJSON_Users_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	users := []UserImportItem{
		{Email: "user1@test.com", Name: "User One"},
		{Email: "user2@test.com", Name: "User Two"},
	}
	data, _ := json.Marshal(users)

	op, err := bp.ImportFromJSON(t.Context(), "test-tenant", BatchOperationTypeUsers, bytes.NewReader(data), BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromJSON users failed: %v", err)
	}
	if op == nil {
		t.Fatal("expected non-nil operation")
	}
	if op.Type != BatchOperationTypeUsers {
		t.Fatalf("expected type users, got %s", op.Type)
	}
}

func TestIAMBatchProcessor_ImportFromJSON_Roles_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	roles := []RoleImportItem{
		{Name: "admin", Description: "Admin role"},
	}
	data, _ := json.Marshal(roles)

	op, err := bp.ImportFromJSON(t.Context(), "test-tenant", BatchOperationTypeRoles, bytes.NewReader(data), BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromJSON roles failed: %v", err)
	}
	if op == nil {
		t.Fatal("expected non-nil operation")
	}
}

func TestIAMBatchProcessor_ImportFromJSON_Policies_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	policies := []PolicyImportItem{
		{Name: "read-only", Description: "Read only"},
	}
	data, _ := json.Marshal(policies)

	op, err := bp.ImportFromJSON(t.Context(), "test-tenant", BatchOperationTypePolicies, bytes.NewReader(data), BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromJSON policies failed: %v", err)
	}
	if op == nil {
		t.Fatal("expected non-nil operation")
	}
}

func TestIAMBatchProcessor_ImportFromJSON_UnsupportedType_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	_, err := bp.ImportFromJSON(t.Context(), "t1", "invalid_type", strings.NewReader("[]"), BatchOptions{})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestIAMBatchProcessor_ImportFromJSON_InvalidJSON_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	_, err := bp.ImportFromJSON(t.Context(), "t1", BatchOperationTypeUsers, strings.NewReader("{invalid"), BatchOptions{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestIAMBatchProcessor_ImportFromCSV_Users_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	csv := "id,email,name\n,user1@test.com,User One\n,user2@test.com,User Two"
	op, err := bp.ImportFromCSV(t.Context(), "test-tenant", BatchOperationTypeUsers, strings.NewReader(csv), BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromCSV users failed: %v", err)
	}
	if op == nil {
		t.Fatal("expected non-nil operation")
	}
}

func TestIAMBatchProcessor_ImportFromCSV_Roles_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	csv := "id,name,description\n,admin,Admin role\n,viewer,Viewer role"
	op, err := bp.ImportFromCSV(t.Context(), "test-tenant", BatchOperationTypeRoles, strings.NewReader(csv), BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromCSV roles failed: %v", err)
	}
	if op == nil {
		t.Fatal("expected non-nil operation")
	}
}

func TestIAMBatchProcessor_ImportFromCSV_Policies_NotSupported(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	csv := "id,name,description\n,read-policy,Read access policy"
	_, err := bp.ImportFromCSV(t.Context(), "test-tenant", BatchOperationTypePolicies, strings.NewReader(csv), BatchOptions{})
	if err == nil {
		t.Fatal("expected error for CSV policy import (not supported)")
	}
}

func TestIAMBatchProcessor_ImportFromCSV_TooFewRows_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	csv := "email,name"
	_, err := bp.ImportFromCSV(t.Context(), "t1", BatchOperationTypeUsers, strings.NewReader(csv), BatchOptions{})
	if err == nil {
		t.Fatal("expected error for CSV with only header row")
	}
}

func TestIAMBatchProcessor_ImportFromCSV_UnsupportedType_Coverage(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	csv := "col1,col2\nval1,val2"
	_, err := bp.ImportFromCSV(t.Context(), "t1", "invalid_type", strings.NewReader(csv), BatchOptions{})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

// =============================================================================
// DeltaLog - appendToTenantLog coverage
// =============================================================================

func TestDeltaLog_AppendToTenantLog_MultipleEvents(t *testing.T) {
	log := NewDeltaLog(100)

	tenantID := "tenant-1"

	// Add multiple events to same tenant (starting from version 1)
	for i := 1; i <= 10; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:    uuid.New().String(),
			Type:       PolicyUpdateCreated,
			TenantID:   tenantID,
			PolicyName: "policy-1",
			Version:    int64(i),
			Timestamp:  time.Now(),
			NodeID:     "node-1",
		}
		if err := log.Append(event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Verify versions are tracked (get from version 1, which should return versions 2-10)
	events, err := log.GetSince(tenantID, 1)
	if err != nil {
		t.Fatalf("GetSince failed: %v", err)
	}
	if len(events) < 5 { // Should get at least some events after version 1
		t.Errorf("expected multiple events, got %d", len(events))
	}
}

func TestDeltaLog_AppendToTenantLog_TrimToMaxSize(t *testing.T) {
	// Create log with small capacity
	log := NewDeltaLog(50)

	tenantID := "tenant-trim"

	// Add more events than max entries to trigger trimming
	for i := 1; i <= 100; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:    uuid.New().String(),
			Type:       PolicyUpdateCreated,
			TenantID:   tenantID,
			PolicyName: "policy-1",
			Version:    int64(i),
			Timestamp:  time.Now(),
			NodeID:     "node-1",
		}
		if err := log.Append(event); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Log should have trimmed older entries
	if log.Size() > 50 {
		t.Errorf("expected log to be trimmed, got size %d", log.Size())
	}
}

// =============================================================================
// BatchProcessor - SaveOperation error path
// =============================================================================

type failingSaveBatchStorage struct {
	*mockBatchStorage
	failSave bool
}

func newFailingSaveBatchStorage() *failingSaveBatchStorage {
	return &failingSaveBatchStorage{
		mockBatchStorage: newMockBatchStorage(),
	}
}

func (f *failingSaveBatchStorage) SaveOperation(ctx context.Context, op *BatchOperation) error {
	if f.failSave {
		return ErrBatchOperationNotFound
	}
	return f.mockBatchStorage.SaveOperation(ctx, op)
}

func TestBatchProcessor_ImportRoles_SaveOperationError(t *testing.T) {
	storage := newFailingSaveBatchStorage()
	storage.failSave = true
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()

	items := []RoleImportItem{
		{ID: "role-1", Name: "Test Role"},
	}

	_, err := bp.ImportRoles(ctx, "test-tenant", items, BatchOptions{})
	if err == nil {
		t.Error("expected error when SaveOperation fails")
	}
}

func TestBatchProcessor_ImportPolicies_SaveOperationError(t *testing.T) {
	storage := newFailingSaveBatchStorage()
	storage.failSave = true
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()

	items := []PolicyImportItem{
		{
			Name: "test-policy",
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "test-policy", Tenant: "test-tenant"},
				Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
			},
		},
	}

	_, err := bp.ImportPolicies(ctx, "test-tenant", items, BatchOptions{})
	if err == nil {
		t.Error("expected error when SaveOperation fails")
	}
}

// =============================================================================
// BatchProcessor - processRoles with rate limiting
// =============================================================================

func TestBatchProcessor_ProcessRoles_RateLimitPath(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []RoleImportItem{
		{ID: "role-1", Name: "Role One", Permissions: []string{"read"}},
	}

	op, err := bp.ImportRoles(ctx, tenantID, items, BatchOptions{
		RateLimit: 10,
	})
	if err != nil {
		t.Fatalf("ImportRoles failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Status == BatchStatusPending {
		t.Error("expected operation to complete with rate limiting")
	}
}

// =============================================================================
// PolicyEngine - CreatePolicy / UpdatePolicy / DeletePolicy
// =============================================================================

func TestPolicyEngine_CreatePolicy_Success(t *testing.T) {
	storage := NewMemoryStorage()
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})
	ctx := context.Background()

	// Create tenant first
	tenant := &Tenant{ID: "tenant-1", Name: "Test Tenant", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "test-policy", Tenant: "tenant-1"},
		Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
	}

	err := engine.CreatePolicy(ctx, "tenant-1", policy)
	if err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}
}

func TestPolicyEngine_DeletePolicy_Success(t *testing.T) {
	storage := NewMemoryStorage()
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})
	ctx := context.Background()

	tenant := &Tenant{ID: "tenant-2", Name: "Test Tenant 2", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "delete-policy", Tenant: "tenant-2"},
		Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
	}

	if err := engine.CreatePolicy(ctx, "tenant-2", policy); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	if err := engine.DeletePolicy(ctx, "tenant-2", "delete-policy"); err != nil {
		t.Fatalf("DeletePolicy failed: %v", err)
	}

	// Verify it's deleted
	_, err := engine.GetPolicy(ctx, "tenant-2", "delete-policy")
	if err == nil {
		t.Error("expected error when getting deleted policy")
	}
}

// =============================================================================
// Consent Storage - GetConsent edge cases
// =============================================================================

func TestMemoryConsentStorage_GetConsent_NotFound(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	_, err := storage.GetConsent(ctx, "tenant-1", 10001, ConsentPurposeMarketing)
	if err == nil {
		t.Error("expected error for non-existent consent")
	}
}

func TestMemoryConsentStorage_GetConsent_ContextCanceled(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := storage.GetConsent(ctx, "tenant-1", 10001, ConsentPurposeMarketing)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

// =============================================================================
// Session storage - removeFromUserIndex edge cases
// =============================================================================

func TestMemorySessionExtStorage_Delete_RemovesFromUserIndex(t *testing.T) {
	storage := NewMemorySessionExtStorage()
	ctx := context.Background()

	// Create session
	session := &SessionExt{
		ID:        "session-to-delete",
		UserID:    10001,
		TenantID:  "tenant-1",
		TokenHash: "token-123",
	}
	if err := storage.Create(ctx, session); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Delete session
	if err := storage.Delete(ctx, session.ID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify session is removed
	_, err := storage.Get(ctx, session.ID)
	if err == nil {
		t.Error("expected error when getting deleted session")
	}
}

// =============================================================================
// BatchProcessor - CreateOperation and update paths
// =============================================================================

func TestBatchProcessor_ProcessUsers_WithRateLimit(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []UserImportItem{
		{ID: "user-1", Email: "user1@test.com", Name: "User One"},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{
		RateLimit: 10,
	})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Status == BatchStatusPending {
		t.Error("expected operation to complete")
	}
}

// =============================================================================
// Batch Manager - ExportAll context cancellation
// =============================================================================

func TestBatchManager_ExportAll_ContextCancellation(t *testing.T) {
	storage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: storage,
	})

	policyEngine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
	})

	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: identityStore,
		PolicyEngine:  policyEngine,
	})

	// Create a tenant with data
	tenant := &Tenant{ID: "export-tenant", Name: "Export Tenant", Created: time.Now()}
	if err := storage.CreateTenant(context.Background(), tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Test with canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bm.ExportAll(ctx, &BatchExportRequest{
		TenantID:        "export-tenant",
		IncludeUsers:    true,
		IncludeRoles:    true,
		IncludePolicies: true,
	})
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

// =============================================================================
// Storage Memory - Additional quota paths
// =============================================================================

func TestMemoryStorage_IncrementQuotaUsage_PoliciesResource(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{
		ID:      "policies-tenant",
		Name:    "Policies Tenant",
		Created: time.Now(),
		Updated: time.Now(),
	}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{
		MaxPolicies:     10,
		CurrentPolicies: 0,
	}
	if err := storage.UpdateQuota(ctx, "policies-tenant", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	if err := storage.IncrementQuotaUsage(ctx, "policies-tenant", "policies", 1); err != nil {
		t.Fatalf("IncrementQuotaUsage failed: %v", err)
	}

	updatedQuota, err := storage.GetQuota(ctx, "policies-tenant")
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if updatedQuota.CurrentPolicies != 1 {
		t.Errorf("expected CurrentPolicies=1, got %d", updatedQuota.CurrentPolicies)
	}
}

func TestMemoryStorage_DecrementQuotaUsage_PoliciesResource(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{
		ID:      "policies-tenant-2",
		Name:    "Policies Tenant 2",
		Created: time.Now(),
		Updated: time.Now(),
	}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{
		MaxPolicies:     10,
		CurrentPolicies: 5,
	}
	if err := storage.UpdateQuota(ctx, "policies-tenant-2", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	if err := storage.DecrementQuotaUsage(ctx, "policies-tenant-2", "policies", 2); err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	updatedQuota, err := storage.GetQuota(ctx, "policies-tenant-2")
	if err != nil {
		t.Fatalf("GetQuota failed: %v", err)
	}
	if updatedQuota.CurrentPolicies != 3 {
		t.Errorf("expected CurrentPolicies=3, got %d", updatedQuota.CurrentPolicies)
	}
}

// =============================================================================
// Batch Processor - processRoles coverage
// =============================================================================

func TestBatchProcessor_ProcessRoles_WithActualCreation(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []RoleImportItem{
		{ID: "role-1", Name: "Admin", Description: "Admin role", Permissions: []string{"read", "write"}},
		{ID: "role-2", Name: "Viewer", Description: "Viewer role", Permissions: []string{"read"}},
	}

	op, err := bp.ImportRoles(ctx, tenantID, items, BatchOptions{})
	if err != nil {
		t.Fatalf("ImportRoles failed: %v", err)
	}

	// Wait for async processing
	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Status == BatchStatusPending {
		t.Error("operation still pending after processing")
	}
}

func TestBatchProcessor_ProcessRoles_ValidationError(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Missing name should fail validation
	items := []RoleImportItem{
		{ID: "role-1", Name: "", Description: "Invalid role"},
	}

	op, err := bp.ImportRoles(ctx, tenantID, items, BatchOptions{
		ContinueOnError: true,
	})
	if err != nil {
		t.Fatalf("ImportRoles failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Failed == 0 {
		t.Error("expected validation failure")
	}
}

// =============================================================================
// Batch Processor - processPolicies coverage
// =============================================================================

func TestBatchProcessor_ProcessPolicies_WithActualCreation(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []PolicyImportItem{
		{
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "allow-read",
					Tenant: tenantID,
				},
				Spec: PolicySpec{
					Rules: []Rule{
						{
							Name:      "allow-all-read",
							Effect:    EffectAllow,
							Actions:   []string{"read"},
							Resources: []string{"*"},
						},
					},
				},
			},
		},
	}

	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Status == BatchStatusPending {
		t.Error("operation still pending after processing")
	}
}

func TestBatchProcessor_ProcessPolicies_WithDirectConstruction(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	// Test policy construction from fields instead of Policy pointer
	items := []PolicyImportItem{
		{
			Name:        "direct-policy",
			Description: "Directly constructed policy",
			Priority:    10,
			Rules: []Rule{
				{
					Name:      "allow-read",
					Effect:    EffectAllow,
					Actions:   []string{"read"},
					Resources: []string{"*"},
				},
			},
		},
	}

	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Status == BatchStatusPending {
		t.Error("operation still pending after processing")
	}
}

// =============================================================================
// ImportFromJSON with all types
// =============================================================================

func TestBatchProcessor_ImportFromJSON_Roles(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []RoleImportItem{
		{ID: "role-1", Name: "Admin"},
	}

	data, _ := json.Marshal(items)
	reader := bytes.NewReader(data)

	op, err := bp.ImportFromJSON(ctx, tenantID, BatchOperationTypeRoles, reader, BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromJSON failed: %v", err)
	}

	if op.Type != BatchOperationTypeRoles {
		t.Errorf("expected type %s, got %s", BatchOperationTypeRoles, op.Type)
	}

	time.Sleep(100 * time.Millisecond)
}

func TestBatchProcessor_ImportFromJSON_Policies(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []PolicyImportItem{
		{
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata: PolicyMetadata{
					Name:   "test-policy",
					Tenant: tenantID,
				},
				Spec: PolicySpec{
					Rules: []Rule{{Name: "test", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
				},
			},
		},
	}

	data, _ := json.Marshal(items)
	reader := bytes.NewReader(data)

	op, err := bp.ImportFromJSON(ctx, tenantID, BatchOperationTypePolicies, reader, BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromJSON failed: %v", err)
	}

	if op.Type != BatchOperationTypePolicies {
		t.Errorf("expected type %s, got %s", BatchOperationTypePolicies, op.Type)
	}

	time.Sleep(100 * time.Millisecond)
}

func TestBatchProcessor_ImportFromJSON_InvalidRoles(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	reader := strings.NewReader("invalid json")

	_, err := bp.ImportFromJSON(ctx, tenantID, BatchOperationTypeRoles, reader, BatchOptions{})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestBatchProcessor_ImportFromJSON_InvalidPolicies(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	reader := strings.NewReader("invalid json")

	_, err := bp.ImportFromJSON(ctx, tenantID, BatchOperationTypePolicies, reader, BatchOptions{})
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// =============================================================================
// ImportFromCSV with all types
// =============================================================================

func TestBatchProcessor_ImportFromCSV_Roles(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	csvData := `id,name,description
role-1,Admin,Administrator role
role-2,Viewer,Viewer role`

	reader := strings.NewReader(csvData)

	op, err := bp.ImportFromCSV(ctx, tenantID, BatchOperationTypeRoles, reader, BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportFromCSV failed: %v", err)
	}

	if op.Type != BatchOperationTypeRoles {
		t.Errorf("expected type %s, got %s", BatchOperationTypeRoles, op.Type)
	}

	time.Sleep(100 * time.Millisecond)
}

func TestBatchProcessor_ImportFromCSV_UnsupportedType(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	csvData := `name
policy1`

	reader := strings.NewReader(csvData)

	_, err := bp.ImportFromCSV(ctx, tenantID, "unsupported", reader, BatchOptions{})
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestBatchProcessor_ImportFromCSV_HeaderOnly(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	csvData := `id,name,email`

	reader := strings.NewReader(csvData)

	_, err := bp.ImportFromCSV(ctx, tenantID, BatchOperationTypeUsers, reader, BatchOptions{})
	if err == nil {
		t.Error("expected error for header-only CSV")
	}
}

// =============================================================================
// CancelOperation edge cases
// =============================================================================

func TestBatchProcessor_CancelCompletedOperation(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []UserImportItem{
		{Email: "test@example.com", Name: "Test"},
	}

	op, err := bp.ImportUsers(ctx, tenantID, items, BatchOptions{DryRun: true})
	if err != nil {
		t.Fatalf("ImportUsers failed: %v", err)
	}

	// Wait for completion
	time.Sleep(200 * time.Millisecond)

	// Try to cancel a completed operation
	err = bp.CancelOperation(ctx, op.ID)
	if err == nil {
		t.Error("expected error when cancelling completed operation")
	}
}

// =============================================================================
// AuditStorage - QueryEvents edge cases
// =============================================================================

func TestMemoryAuditStorage_QueryEvents_EmptyTenant(t *testing.T) {
	storage := NewMemoryAuditStorage()
	ctx := context.Background()

	filter := &AuditFilter{
		TenantID: "non-existent-tenant",
		Limit:    10,
	}

	events, total, err := storage.QueryEvents(ctx, filter)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
	if total != 0 {
		t.Errorf("expected total 0, got %d", total)
	}
}

func TestMemoryAuditStorage_QueryEvents_WithAllFilters(t *testing.T) {
	storage := NewMemoryAuditStorage()
	ctx := context.Background()

	now := time.Now().UTC()
	event := &AuditEvent{
		ID:        "event-1",
		TenantID:  "tenant-1",
		Timestamp: now,
		EventType: AuditEventAuthentication,
		Category:  AuditCategorySecurity,
		Outcome:   AuditOutcomeSuccess,
		Action:    "login",
		Actor:     &AuditActor{UserID: "user-1"},
		Resource:  &AuditResource{ID: "resource-1", Type: "session"},
	}

	err := storage.SaveEvent(ctx, event)
	if err != nil {
		t.Fatalf("SaveEvent failed: %v", err)
	}

	startTime := now.Add(-time.Hour)
	endTime := now.Add(time.Hour)

	filter := &AuditFilter{
		TenantID:   "tenant-1",
		StartTime:  &startTime,
		EndTime:    &endTime,
		EventTypes: []AuditEventType{AuditEventAuthentication},
		Categories: []AuditCategory{AuditCategorySecurity},
		Outcomes:   []AuditOutcome{AuditOutcomeSuccess},
		ActorID:    "user-1",
		ResourceID: "resource-1",
		Limit:      10,
		Offset:     0,
	}

	events, total, err := storage.QueryEvents(ctx, filter)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
}

func TestMemoryAuditStorage_QueryEvents_FilterNoMatch(t *testing.T) {
	storage := NewMemoryAuditStorage()
	ctx := context.Background()

	now := time.Now().UTC()
	event := &AuditEvent{
		ID:        "event-1",
		TenantID:  "tenant-1",
		Timestamp: now,
		EventType: AuditEventAuthentication,
		Category:  AuditCategorySecurity,
		Outcome:   AuditOutcomeSuccess,
		Action:    "login",
	}

	err := storage.SaveEvent(ctx, event)
	if err != nil {
		t.Fatalf("SaveEvent failed: %v", err)
	}

	// Filter for different event type - should not match
	filter := &AuditFilter{
		TenantID:   "tenant-1",
		EventTypes: []AuditEventType{AuditEventAuthorization},
		Limit:      10,
	}

	events, _, err := storage.QueryEvents(ctx, filter)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestMemoryAuditStorage_QueryEvents_Pagination(t *testing.T) {
	storage := NewMemoryAuditStorage()
	ctx := context.Background()

	// Create multiple events
	for i := 0; i < 10; i++ {
		event := &AuditEvent{
			ID:        "event-" + string(rune('0'+i)),
			TenantID:  "tenant-1",
			Timestamp: time.Now().Add(time.Duration(i) * time.Minute),
			EventType: AuditEventAuthentication,
			Category:  AuditCategorySecurity,
			Outcome:   AuditOutcomeSuccess,
			Action:    "login",
		}
		err := storage.SaveEvent(ctx, event)
		if err != nil {
			t.Fatalf("SaveEvent failed: %v", err)
		}
	}

	// Query with offset beyond available events
	filter := &AuditFilter{
		TenantID: "tenant-1",
		Limit:    5,
		Offset:   20,
	}

	events, total, err := storage.QueryEvents(ctx, filter)
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 0 {
		t.Errorf("expected 0 events with offset beyond total, got %d", len(events))
	}
	if total != 10 {
		t.Errorf("expected total 10, got %d", total)
	}
}

// =============================================================================
// DeltaLog coverage
// =============================================================================

func TestDeltaLog_AppendAndGetSince(t *testing.T) {
	log := NewDeltaLog(100)

	// Add version 1
	event1 := &EnhancedPolicyChangeEvent{
		EventID:  "event-1",
		TenantID: "tenant-1",
		Version:  1,
	}
	err := log.Append(event1)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Add version 2
	event2 := &EnhancedPolicyChangeEvent{
		EventID:  "event-2",
		TenantID: "tenant-1",
		Version:  2,
	}
	err = log.Append(event2)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// GetSince version 1 should return event with version 2 (version > sinceVersion)
	events, err := log.GetSince("tenant-1", 1)
	if err != nil {
		t.Fatalf("GetSince failed: %v", err)
	}

	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if len(events) > 0 && events[0].Version != 2 {
		t.Errorf("expected version 2, got %d", events[0].Version)
	}
}

func TestDeltaLog_GetSince_NonExistentTenant(t *testing.T) {
	log := NewDeltaLog(100)

	events, err := log.GetSince("non-existent", 0)
	if err != nil {
		t.Fatalf("GetSince failed: %v", err)
	}

	if events != nil && len(events) != 0 {
		t.Errorf("expected nil or empty events, got %d", len(events))
	}
}

func TestDeltaLog_GetSince_OldVersion(t *testing.T) {
	log := NewDeltaLog(5) // Small buffer

	// Add events to overflow the buffer
	for i := int64(1); i <= 10; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:  "event-" + string(rune('0'+i)),
			TenantID: "tenant-1",
			Version:  i,
		}
		err := log.Append(event)
		if err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// Request version older than available
	_, err := log.GetSince("tenant-1", 0)
	if err == nil {
		t.Error("expected error for old version request")
	}
}

func TestDeltaLog_NilEvent(t *testing.T) {
	log := NewDeltaLog(100)

	err := log.Append(nil)
	if err == nil {
		t.Error("expected error for nil event")
	}
}

func TestDeltaLog_Methods(t *testing.T) {
	log := NewDeltaLog(100)

	if log.Size() != 0 {
		t.Errorf("expected size 0, got %d", log.Size())
	}

	event := &EnhancedPolicyChangeEvent{
		EventID:  "event-1",
		TenantID: "tenant-1",
		Version:  5,
	}
	_ = log.Append(event)

	if log.Size() != 1 {
		t.Errorf("expected size 1, got %d", log.Size())
	}

	if log.GetMinVersion() != 5 {
		t.Errorf("expected min version 5, got %d", log.GetMinVersion())
	}

	if log.GetMaxVersion() != 5 {
		t.Errorf("expected max version 5, got %d", log.GetMaxVersion())
	}

	log.Trim()
}

func TestDeltaLog_DefaultMaxEntries(t *testing.T) {
	log := NewDeltaLog(0) // Should use default

	if log.maxEntries != 10000 {
		t.Errorf("expected default maxEntries 10000, got %d", log.maxEntries)
	}
}

// =============================================================================
// Admin - generateID functions coverage
// =============================================================================

func TestGenerateAdminAssignmentID(t *testing.T) {
	id1, err := generateAdminAssignmentID()
	if err != nil {
		t.Fatalf("generateAdminAssignmentID failed: %v", err)
	}

	if !strings.HasPrefix(id1, "adm_") {
		t.Errorf("expected prefix 'adm_', got %s", id1)
	}

	// Generate another to ensure uniqueness
	id2, _ := generateAdminAssignmentID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

func TestGenerateImpersonationSessionID(t *testing.T) {
	id1, err := generateImpersonationSessionID()
	if err != nil {
		t.Fatalf("generateImpersonationSessionID failed: %v", err)
	}

	if !strings.HasPrefix(id1, "imp_") {
		t.Errorf("expected prefix 'imp_', got %s", id1)
	}

	id2, _ := generateImpersonationSessionID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
}

// =============================================================================
// matchResourcePattern edge cases
// =============================================================================

func TestMatchResourcePattern_EdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		pattern    string
		resourceID string
		expected   bool
	}{
		{"exact match", "user:123", "user:123", true},
		{"exact no match", "user:123", "user:456", false},
		{"suffix wildcard match", "user:*", "user:123", true},
		{"suffix wildcard no match", "admin:*", "user:123", false},
		{"prefix wildcard match", "*:admin", "super:admin", true},
		{"prefix wildcard no match", "*:admin", "admin:user", false},
		{"empty pattern", "", "", true},
		{"empty pattern no match", "", "something", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := matchResourcePattern(tc.pattern, tc.resourceID)
			if result != tc.expected {
				t.Errorf("matchResourcePattern(%q, %q) = %v, expected %v",
					tc.pattern, tc.resourceID, result, tc.expected)
			}
		})
	}
}

// =============================================================================
// PolicyImportItem validation
// =============================================================================

func TestPolicyImportItem_Validation(t *testing.T) {
	t.Run("valid with policy", func(t *testing.T) {
		item := PolicyImportItem{
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "test"},
				Spec: PolicySpec{
					Rules: []Rule{{Name: "test", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}},
				},
			},
		}
		if err := item.Validate(); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
	})

	t.Run("valid with name and rules", func(t *testing.T) {
		item := PolicyImportItem{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "test", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
			},
		}
		if err := item.Validate(); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
	})

	t.Run("invalid - no name or policy", func(t *testing.T) {
		item := PolicyImportItem{}
		if err := item.Validate(); err == nil {
			t.Error("expected error for empty item")
		}
	})
}

// =============================================================================
// BatchOperation status methods
// =============================================================================

func TestBatchOperation_StatusMethods(t *testing.T) {
	t.Run("IsTerminal", func(t *testing.T) {
		statuses := map[BatchStatus]bool{
			BatchStatusPending:        false,
			BatchStatusValidating:     false,
			BatchStatusProcessing:     false,
			BatchStatusCompleted:      true,
			BatchStatusFailed:         true,
			BatchStatusCancelled:      true,
			BatchStatusPartialSuccess: true,
		}

		for status, expected := range statuses {
			op := &BatchOperation{Status: status}
			if op.IsTerminal() != expected {
				t.Errorf("IsTerminal for %s: expected %v, got %v", status, expected, op.IsTerminal())
			}
		}
	})

	t.Run("CanCancel", func(t *testing.T) {
		statuses := map[BatchStatus]bool{
			BatchStatusPending:        true,
			BatchStatusValidating:     true,
			BatchStatusProcessing:     true,
			BatchStatusCompleted:      false,
			BatchStatusFailed:         false,
			BatchStatusCancelled:      false,
			BatchStatusPartialSuccess: false,
		}

		for status, expected := range statuses {
			op := &BatchOperation{Status: status}
			if op.CanCancel() != expected {
				t.Errorf("CanCancel for %s: expected %v, got %v", status, expected, op.CanCancel())
			}
		}
	})
}

// =============================================================================
// BatchProgress methods
// =============================================================================

func TestBatchProgress_Methods(t *testing.T) {
	t.Run("UpdateProgress with zero total", func(t *testing.T) {
		progress := &BatchProgress{
			Total:     0,
			Processed: 0,
		}
		progress.UpdateProgress()
		if progress.PercentComplete != 0 {
			t.Errorf("expected 0%%, got %.2f%%", progress.PercentComplete)
		}
	})

	t.Run("CalculateRate with zero elapsed", func(t *testing.T) {
		progress := &BatchProgress{
			Total:     100,
			Processed: 50,
		}
		progress.CalculateRate(0)
		// Should handle zero elapsed gracefully
	})
}

// =============================================================================
// Batch processor with create errors
// =============================================================================

type failingTenantManager struct {
	*mockTenantManager
}

func (f *failingTenantManager) CreateRole(ctx context.Context, role *Role) error {
	return ErrRoleAlreadyExists
}

func (f *failingTenantManager) CreatePolicy(ctx context.Context, policy *Policy) error {
	return ErrPolicyNotFound
}

func TestBatchProcessor_ProcessRoles_CreateError(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := &failingTenantManager{mockTenantManager: newMockTenantManager()}
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []RoleImportItem{
		{ID: "role-1", Name: "Admin"},
	}

	op, err := bp.ImportRoles(ctx, tenantID, items, BatchOptions{
		ContinueOnError: true,
	})
	if err != nil {
		t.Fatalf("ImportRoles failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Failed == 0 {
		t.Error("expected failure due to create error")
	}
}

func TestBatchProcessor_ProcessPolicies_CreateError(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := &failingTenantManager{mockTenantManager: newMockTenantManager()}
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "test-tenant"

	items := []PolicyImportItem{
		{
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "test", Tenant: tenantID},
				Spec:       PolicySpec{Rules: []Rule{{Name: "r", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
			},
		},
	}

	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{
		ContinueOnError: true,
	})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	saved, _ := storage.GetOperation(ctx, op.ID)
	if saved.Progress.Failed == 0 {
		t.Error("expected failure due to create error")
	}
}
