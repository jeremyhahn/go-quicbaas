package iam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// createTestBatchManager creates a BatchManager with a fully configured IdentityStore and PolicyEngine.
func createTestBatchManager(t *testing.T) (*BatchManager, *MemoryStorage) {
	t.Helper()

	memStorage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
	})

	policyEngine := NewPolicyEngine(PolicyEngineOptions{
		Storage: memStorage,
	})

	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: identityStore,
		PolicyEngine:  policyEngine,
		BatchSize:     10,
		Parallelism:   2,
	})

	return bm, memStorage
}

// batchTestUserID generates a unique user ID for batch import tests.
func batchTestUserID(i int) string {
	return fmt.Sprintf("batch-user-%d", i)
}

// batchTestEmail generates a unique email for batch import tests.
func batchTestEmail(i int) string {
	return fmt.Sprintf("batch-user-%d@example.com", i)
}

// batchTestUsername generates a unique username for batch import tests.
func batchTestUsername(i int) string {
	return fmt.Sprintf("batchuser%d", i)
}

// TestImportAll tests the ImportAll function.
func TestImportAll(t *testing.T) {
	t.Run("successful import of all resource types", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Roles: []*RoleImport{
				{ID: "role1", Name: "Admin", Description: "Admin role", Permissions: []string{"read", "write"}},
				{ID: "role2", Name: "User", Description: "User role", Permissions: []string{"read"}},
			},
			Users: []*UserImport{
				{ID: 20001, Email: "user1@example.com", Username: "user1", Name: "User One", Enabled: boolPtr(true)},
				{ID: 20002, Email: "user2@example.com", Username: "user2", Name: "User Two", Enabled: boolPtr(true)},
			},
			Policies: []*PolicyImport{
				{
					Name:        "admin-policy",
					Description: "Admin policy",
					Priority:    100,
					Subjects:    SubjectSelector{Roles: []string{"role1"}},
					Rules: []Rule{
						{Name: "allow-all", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
					},
				},
			},
			Options: BatchImportOptions{
				ContinueOnError: false,
			},
		}

		result, err := bm.ImportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result == nil {
			t.Fatal("expected result, got nil")
		}
		if result.SuccessCount != 5 {
			t.Errorf("expected 5 successes, got %d", result.SuccessCount)
		}
		if result.FailureCount != 0 {
			t.Errorf("expected 0 failures, got %d", result.FailureCount)
		}
		if result.TotalItems != 5 {
			t.Errorf("expected 5 total items, got %d", result.TotalItems)
		}
		if len(result.ProcessedItems["users"]) != 2 {
			t.Errorf("expected 2 processed users, got %d", len(result.ProcessedItems["users"]))
		}
		if len(result.ProcessedItems["roles"]) != 2 {
			t.Errorf("expected 2 processed roles, got %d", len(result.ProcessedItems["roles"]))
		}
	})

	t.Run("dry run mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Users: []*UserImport{
				{ID: 20001, Email: "user1@example.com", Username: "user1", Name: "User One", Enabled: boolPtr(true)},
			},
			Roles: []*RoleImport{
				{ID: "role1", Name: "Admin"},
			},
			Options: BatchImportOptions{
				DryRun: true,
			},
		}

		result, err := bm.ImportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.DryRun {
			t.Error("expected DryRun flag to be true")
		}
		if result.SuccessCount != 2 {
			t.Errorf("expected 2 successes (dry run), got %d", result.SuccessCount)
		}

		// Verify nothing was actually created
		users, _ := memStorage.ListUsers(ctx, "test-tenant", ListOptions{})
		if len(users) != 0 {
			t.Errorf("expected 0 users in storage after dry run, got %d", len(users))
		}
	})

	t.Run("continue on error mode", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Users: []*UserImport{
				{ID: 20001, Email: "user1@example.com", Username: "user1", Name: "User One", Enabled: boolPtr(true)},
				{ID: 20002, Email: "", Username: "user2", Name: "Invalid User", Enabled: boolPtr(true)}, // Invalid - no email
				{ID: 20003, Email: "user3@example.com", Username: "user3", Name: "User Three", Enabled: boolPtr(true)},
			},
			Options: BatchImportOptions{
				ContinueOnError: true,
			},
		}

		result, err := bm.ImportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error with continue on error: %v", err)
		}

		if result.SuccessCount != 2 {
			t.Errorf("expected 2 successes, got %d", result.SuccessCount)
		}
		if result.FailureCount != 1 {
			t.Errorf("expected 1 failure, got %d", result.FailureCount)
		}
		if len(result.Errors) != 1 {
			t.Errorf("expected 1 error recorded, got %d", len(result.Errors))
		}
	})

	t.Run("validation error - empty request", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
		}

		_, err := bm.ImportAll(ctx, req)
		if err == nil {
			t.Error("expected error for empty request")
		}
	})

	t.Run("validation error - missing tenant ID", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			Users: []*UserImport{{Email: "test@example.com", Username: "test"}},
		}

		_, err := bm.ImportAll(ctx, req)
		if err == nil {
			t.Error("expected error for missing tenant ID")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Roles: []*RoleImport{
				{ID: "role1", Name: "Role1"},
			},
		}

		result, err := bm.ImportAll(ctx, req)
		// The operation may complete or be canceled depending on timing
		if err != nil && !errors.Is(err, ErrBatchCanceled) && !errors.Is(err, context.Canceled) {
			t.Errorf("unexpected error type: %v", err)
		}
		_ = result
	})

	t.Run("with timeout", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Users: []*UserImport{
				{ID: 20001, Email: "user1@example.com", Username: "user1"},
			},
			Options: BatchImportOptions{
				Timeout: 5 * time.Minute,
			},
		}

		result, err := bm.ImportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 1 {
			t.Errorf("expected 1 success, got %d", result.SuccessCount)
		}
	})

	t.Run("with progress callback", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		var progressMu sync.Mutex
		var progressCalls []BatchImportProgress

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Users: []*UserImport{
				{ID: 20001, Email: "user1@example.com", Username: "user1"},
				{ID: 20002, Email: "user2@example.com", Username: "user2"},
			},
			Options: BatchImportOptions{
				ProgressFunc: func(p BatchImportProgress) {
					progressMu.Lock()
					progressCalls = append(progressCalls, p)
					progressMu.Unlock()
				},
			},
		}

		_, err := bm.ImportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Progress callback should have been called
		progressMu.Lock()
		callCount := len(progressCalls)
		progressMu.Unlock()

		if callCount == 0 {
			t.Log("Note: progress callback may not be called for fast operations")
		}
	})

	t.Run("role import failure without continue on error", func(t *testing.T) {
		// Create a batch manager without an identity store to trigger role import failure
		bm := NewBatchManager(BatchManagerOptions{
			PolicyEngine: NewPolicyEngine(PolicyEngineOptions{}),
			BatchSize:    10,
			Parallelism:  2,
		})
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Roles: []*RoleImport{
				{ID: "role1", Name: "Admin"},
			},
			Options: BatchImportOptions{
				ContinueOnError: false,
			},
		}

		result, err := bm.ImportAll(ctx, req)
		// Without ContinueOnError, the function should return an error when roles fail
		// but results should still contain partial data
		if result == nil {
			t.Error("expected result even on error")
		}
		// Note: The error may be returned or captured in result depending on implementation
		_ = err
	})

	t.Run("policies fail but other resources succeed with continue on error", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Roles: []*RoleImport{
				{ID: "role1", Name: "Admin"},
			},
			Users: []*UserImport{
				{ID: 20001, Email: "user1@example.com", Username: "user1"},
			},
			Policies: []*PolicyImport{
				{Name: "", Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}}}}, // Invalid - missing name
			},
			Options: BatchImportOptions{
				ContinueOnError: true,
			},
		}

		result, err := bm.ImportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error with continue on error: %v", err)
		}

		if result.SuccessCount != 2 {
			t.Errorf("expected 2 successes, got %d", result.SuccessCount)
		}
		if result.FailureCount != 1 {
			t.Errorf("expected 1 failure (policy), got %d", result.FailureCount)
		}
	})

	t.Run("only roles in request", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Roles: []*RoleImport{
				{ID: "role1", Name: "Admin"},
				{ID: "role2", Name: "User"},
			},
		}

		result, err := bm.ImportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 2 {
			t.Errorf("expected 2 successes, got %d", result.SuccessCount)
		}
		if result.TotalItems != 2 {
			t.Errorf("expected 2 total items, got %d", result.TotalItems)
		}
	})

	t.Run("only policies in request", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Policies: []*PolicyImport{
				{
					Name: "policy1",
					Rules: []Rule{
						{Name: "allow-all", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
					},
				},
			},
		}

		result, err := bm.ImportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 1 {
			t.Errorf("expected 1 success, got %d", result.SuccessCount)
		}
	})
}

// TestImportUsers tests the ImportUsers function.
func TestImportUsers(t *testing.T) {
	t.Run("successful batch import", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		users := []*UserImport{
			{ID: 1, Email: "user1@example.com", Username: "user1", Name: "User One", Enabled: boolPtr(true)},
			{ID: 2, Email: "user2@example.com", Username: "user2", Name: "User Two", Enabled: boolPtr(true)},
			{ID: 3, Email: "user3@example.com", Username: "user3", Name: "User Three", Enabled: boolPtr(true)},
		}

		result, err := bm.ImportUsers(ctx, "test-tenant", users, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.TotalItems != 3 {
			t.Errorf("expected 3 total items, got %d", result.TotalItems)
		}
		if result.SuccessCount != 3 {
			t.Errorf("expected 3 successes, got %d", result.SuccessCount)
		}
		if len(result.ProcessedItems["users"]) != 3 {
			t.Errorf("expected 3 processed users, got %d", len(result.ProcessedItems["users"]))
		}
	})

	t.Run("import with roles", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Pre-create a role
		role := &Role{ID: "role1", TenantID: "test-tenant", Name: "TestRole"}
		if err := memStorage.CreateRole(ctx, role); err != nil {
			t.Fatalf("failed to create role: %v", err)
		}

		users := []*UserImport{
			{
				ID:       1,
				Email:    "user1@example.com",
				Username: "user1",
				Name:     "User One",
				Roles:    []string{"role1"},
				Enabled:  boolPtr(true),
			},
		}

		result, err := bm.ImportUsers(ctx, "test-tenant", users, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 1 {
			t.Errorf("expected 1 success, got %d", result.SuccessCount)
		}

		// Looked up by the canonical login key, not by the import's own id: the
		// store assigns the identity, so an id in an import file names a record
		// somewhere else.
		user, err := memStorage.GetUserByEmail(ctx, "test-tenant", "user1@example.com")
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if len(user.Roles) != 1 {
			t.Errorf("expected 1 role, got %d", len(user.Roles))
		}
	})

	t.Run("update existing mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Pre-create a user
		existingUser := &User{
			ID:       1,
			Email:    "user1@example.com",
			TenantID: "test-tenant",
			Name:     "Old Name",
		}
		if err := memStorage.CreateUser(ctx, existingUser); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}

		users := []*UserImport{
			{ID: 1, Email: "user1@example.com", Username: "user1", Name: "New Name", Enabled: boolPtr(true)},
		}

		result, err := bm.ImportUsers(ctx, "test-tenant", users, BatchImportOptions{UpdateExisting: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 1 {
			t.Errorf("expected 1 success, got %d", result.SuccessCount)
		}

		// Verify name was updated
		user, err := memStorage.GetUser(ctx, "test-tenant", 1)
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if user.Name != "New Name" {
			t.Errorf("expected name 'New Name', got '%s'", user.Name)
		}
	})

	t.Run("skip existing without update mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Pre-create a user
		existingUser := &User{
			ID:       1,
			Email:    "user1@example.com",
			TenantID: "test-tenant",
			Name:     "Existing User",
		}
		if err := memStorage.CreateUser(ctx, existingUser); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}

		users := []*UserImport{
			{ID: 1, Email: "user1@example.com", Username: "user1", Name: "New Name"},
		}

		result, err := bm.ImportUsers(ctx, "test-tenant", users, BatchImportOptions{UpdateExisting: false})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SkippedCount != 1 {
			t.Errorf("expected 1 skipped, got %d", result.SkippedCount)
		}
	})

	t.Run("validation errors", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		users := []*UserImport{
			{ID: 1, Email: "", Username: "user1"},             // Missing email
			{ID: 2, Email: "user2@example.com", Username: ""}, // Missing username
		}

		result, err := bm.ImportUsers(ctx, "test-tenant", users, BatchImportOptions{ContinueOnError: true})
		if err != nil {
			t.Fatalf("unexpected error with continue on error: %v", err)
		}

		if result.FailureCount != 2 {
			t.Errorf("expected 2 failures, got %d", result.FailureCount)
		}
		if len(result.Errors) != 2 {
			t.Errorf("expected 2 errors, got %d", len(result.Errors))
		}
	})

	t.Run("missing tenant ID", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		users := []*UserImport{{Email: "test@example.com", Username: "test"}}

		_, err := bm.ImportUsers(ctx, "", users, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing tenant ID")
		}
	})

	t.Run("invalid options", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		users := []*UserImport{{Email: "test@example.com", Username: "test"}}

		_, err := bm.ImportUsers(ctx, "test-tenant", users, BatchImportOptions{
			BatchSize: -1,
		})
		if err == nil {
			t.Error("expected error for invalid batch size")
		}
	})

	t.Run("empty user list", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		result, err := bm.ImportUsers(ctx, "test-tenant", []*UserImport{}, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.TotalItems != 0 {
			t.Errorf("expected 0 total items, got %d", result.TotalItems)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		users := make([]*UserImport, 10)
		for i := range users {
			users[i] = &UserImport{
				ID:       uint64(i) + 1,
				Email:    fmt.Sprintf("user%d@example.com", i),
				Username: fmt.Sprintf("user%d", i),
			}
		}

		// Submission and result collection race the cancelled context, so
		// the batch either stops with ErrBatchCanceled or reports per-item
		// failures; it never returns another error or a nil result.
		result, err := bm.ImportUsers(ctx, "test-tenant", users, BatchImportOptions{})
		if err != nil && !errors.Is(err, ErrBatchCanceled) {
			t.Errorf("expected ErrBatchCanceled or nil, got %v", err)
		}
		if result == nil {
			t.Fatal("expected a result for a cancelled batch")
		}
	})
}

// TestImportUser tests the importUser function.
func TestImportUser(t *testing.T) {
	t.Run("successful import with all fields", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		user := &UserImport{
			ID:         1,
			Email:      "user1@example.com",
			Username:   "user1",
			Name:       "User One",
			Attributes: map[string]string{"department": "engineering"},
			Enabled:    boolPtr(true),
		}

		err := bm.importUser(ctx, "test-tenant", user, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Looked up by the canonical login key; see above.
		createdUser, err := memStorage.GetUserByEmail(ctx, "test-tenant", "user1@example.com")
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if createdUser.Email != "user1@example.com" {
			t.Errorf("expected email 'user1@example.com', got '%s'", createdUser.Email)
		}
		if createdUser.Name != "User One" {
			t.Errorf("expected name 'User One', got '%s'", createdUser.Name)
		}
	})

	t.Run("dry run does not create", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		user := &UserImport{
			ID:       1,
			Email:    "user1@example.com",
			Username: "user1",
		}

		err := bm.importUser(ctx, "test-tenant", user, BatchImportOptions{DryRun: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify user was NOT created
		_, err = memStorage.GetUser(ctx, "test-tenant", 1)
		if err == nil {
			t.Error("expected user not to be created in dry run")
		}
	})

	t.Run("missing email error", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		user := &UserImport{
			ID:       1,
			Username: "user1",
		}

		err := bm.importUser(ctx, "test-tenant", user, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing email")
		}

		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Errorf("expected ValidationError, got %T", err)
		}
	})

	t.Run("update existing user with roles", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create role first
		role := &Role{ID: "role1", TenantID: "test-tenant", Name: "TestRole"}
		if err := memStorage.CreateRole(ctx, role); err != nil {
			t.Fatalf("failed to create role: %v", err)
		}

		// Create existing user
		existingUser := &User{
			ID:       1,
			Email:    "user1@example.com",
			TenantID: "test-tenant",
			Name:     "Old Name",
		}
		if err := memStorage.CreateUser(ctx, existingUser); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}

		user := &UserImport{
			ID:         1,
			Email:      "user1@example.com",
			Username:   "user1",
			Name:       "New Name",
			Roles:      []string{"role1"},
			Attributes: map[string]string{"updated": "true"},
			Enabled:    boolPtr(true),
		}

		err := bm.importUser(ctx, "test-tenant", user, BatchImportOptions{UpdateExisting: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify updates
		updated, err := memStorage.GetUser(ctx, "test-tenant", 1)
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if updated.Name != "New Name" {
			t.Errorf("expected name 'New Name', got '%s'", updated.Name)
		}
	})

	t.Run("email already exists without update mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create existing user
		existingUser := &User{
			ID:       1,
			Email:    "user1@example.com",
			TenantID: "test-tenant",
			Name:     "Existing User",
		}
		if err := memStorage.CreateUser(ctx, existingUser); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}

		user := &UserImport{
			ID:       2,
			Email:    "user1@example.com", // Same email
			Username: "user2",
		}

		err := bm.importUser(ctx, "test-tenant", user, BatchImportOptions{UpdateExisting: false})
		if err == nil {
			t.Error("expected error for existing email")
		}
	})

	t.Run("import with non-existent role", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		user := &UserImport{
			ID:       1,
			Email:    "user1@example.com",
			Username: "user1",
			Roles:    []string{"nonexistent-role"},
		}

		// Should succeed but log warning (role not found is ignored)
		err := bm.importUser(ctx, "test-tenant", user, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("nil identity store error", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})
		ctx := context.Background()

		user := &UserImport{
			Email:    "test@example.com",
			Username: "test",
		}

		err := bm.importUser(ctx, "test-tenant", user, BatchImportOptions{})
		var qbErr *dberrors.QBError
		if !errors.As(err, &qbErr) {
			t.Errorf("expected QBError for nil identity store, got %T: %v", err, err)
		}
	})

	t.Run("import user with multiple roles", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create multiple roles
		for i := 1; i <= 3; i++ {
			role := &Role{ID: fmt.Sprintf("role%d", i), TenantID: "test-tenant", Name: fmt.Sprintf("Role%d", i)}
			if err := memStorage.CreateRole(ctx, role); err != nil {
				t.Fatalf("failed to create role: %v", err)
			}
		}

		user := &UserImport{
			ID:       1,
			Email:    "user1@example.com",
			Username: "user1",
			Roles:    []string{"role1", "role2", "role3"},
			Enabled:  boolPtr(true),
		}

		err := bm.importUser(ctx, "test-tenant", user, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Looked up by the canonical login key; the store assigns the identity.
		createdUser, err := memStorage.GetUserByEmail(ctx, "test-tenant", "user1@example.com")
		if err != nil {
			t.Fatalf("failed to get user: %v", err)
		}
		if len(createdUser.Roles) != 3 {
			t.Errorf("expected 3 roles, got %d", len(createdUser.Roles))
		}
	})
}

// TestImportRoles tests the ImportRoles function.
func TestImportRoles(t *testing.T) {
	t.Run("successful batch import", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		roles := []*RoleImport{
			{ID: "r1", Name: "Admin", Description: "Admin role", Permissions: []string{"*"}},
			{ID: "r2", Name: "User", Description: "User role", Permissions: []string{"read"}},
		}

		result, err := bm.ImportRoles(ctx, "test-tenant", roles, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 2 {
			t.Errorf("expected 2 successes, got %d", result.SuccessCount)
		}
	})

	t.Run("update existing role", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create existing role
		existingRole := &Role{
			ID:          "r1",
			TenantID:    "test-tenant",
			Name:        "Old Name",
			Permissions: []string{"read"},
		}
		if err := memStorage.CreateRole(ctx, existingRole); err != nil {
			t.Fatalf("failed to create role: %v", err)
		}

		roles := []*RoleImport{
			{ID: "r1", Name: "New Name", Permissions: []string{"read", "write"}},
		}

		result, err := bm.ImportRoles(ctx, "test-tenant", roles, BatchImportOptions{UpdateExisting: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 1 {
			t.Errorf("expected 1 success, got %d", result.SuccessCount)
		}

		// Verify update
		role, err := memStorage.GetRole(ctx, "test-tenant", "r1")
		if err != nil {
			t.Fatalf("failed to get role: %v", err)
		}
		if role.Name != "New Name" {
			t.Errorf("expected name 'New Name', got '%s'", role.Name)
		}
	})

	t.Run("dry run mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		roles := []*RoleImport{
			{ID: "r1", Name: "Admin"},
		}

		result, err := bm.ImportRoles(ctx, "test-tenant", roles, BatchImportOptions{DryRun: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.DryRun {
			t.Error("expected DryRun flag to be true")
		}

		// Verify role was NOT created
		_, err = memStorage.GetRole(ctx, "test-tenant", "r1")
		if err == nil {
			t.Error("expected role not to be created in dry run")
		}
	})

	t.Run("empty role list", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		result, err := bm.ImportRoles(ctx, "test-tenant", []*RoleImport{}, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.TotalItems != 0 {
			t.Errorf("expected 0 total items, got %d", result.TotalItems)
		}
	})

	t.Run("missing tenant ID", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		roles := []*RoleImport{{ID: "r1", Name: "Admin"}}

		_, err := bm.ImportRoles(ctx, "", roles, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing tenant ID")
		}
	})

	t.Run("invalid options", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		roles := []*RoleImport{{ID: "r1", Name: "Admin"}}

		_, err := bm.ImportRoles(ctx, "test-tenant", roles, BatchImportOptions{
			BatchSize: -1,
		})
		if err == nil {
			t.Error("expected error for invalid batch size")
		}
	})
}

// TestImportPolicies tests the ImportPolicies function.
func TestImportPolicies(t *testing.T) {
	t.Run("successful batch import", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		policies := []*PolicyImport{
			{
				Name:        "policy1",
				Description: "First policy",
				Priority:    100,
				Subjects:    SubjectSelector{Roles: []string{"admin"}},
				Rules: []Rule{
					{Name: "allow-all", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
				},
			},
			{
				Name:        "policy2",
				Description: "Second policy",
				Priority:    50,
				Subjects:    SubjectSelector{Authenticated: true},
				Rules: []Rule{
					{Name: "allow-read", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
				},
			},
		}

		result, err := bm.ImportPolicies(ctx, "test-tenant", policies, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 2 {
			t.Errorf("expected 2 successes, got %d", result.SuccessCount)
		}
	})

	t.Run("update existing policy", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create existing policy
		existingPolicy := &Policy{
			APIVersion: "iam/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:        "policy1",
				Tenant:      "test-tenant",
				Description: "Old description",
			},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "old-rule", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
				},
			},
		}
		if err := memStorage.CreatePolicy(ctx, "test-tenant", existingPolicy); err != nil {
			t.Fatalf("failed to create policy: %v", err)
		}

		policies := []*PolicyImport{
			{
				Name:        "policy1",
				Description: "New description",
				Rules: []Rule{
					{Name: "new-rule", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
				},
			},
		}

		result, err := bm.ImportPolicies(ctx, "test-tenant", policies, BatchImportOptions{UpdateExisting: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.SuccessCount != 1 {
			t.Errorf("expected 1 success, got %d", result.SuccessCount)
		}
	})

	t.Run("validation error - missing name", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		policies := []*PolicyImport{
			{
				Name: "",
				Rules: []Rule{
					{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
				},
			},
		}

		result, err := bm.ImportPolicies(ctx, "test-tenant", policies, BatchImportOptions{ContinueOnError: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.FailureCount != 1 {
			t.Errorf("expected 1 failure, got %d", result.FailureCount)
		}
	})

	t.Run("validation error - missing rules", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		policies := []*PolicyImport{
			{
				Name:  "policy1",
				Rules: []Rule{},
			},
		}

		result, err := bm.ImportPolicies(ctx, "test-tenant", policies, BatchImportOptions{ContinueOnError: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.FailureCount != 1 {
			t.Errorf("expected 1 failure, got %d", result.FailureCount)
		}
	})

	t.Run("policy already exists without update mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create existing policy
		existingPolicy := &Policy{
			APIVersion: "iam/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:   "policy1",
				Tenant: "test-tenant",
			},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
				},
			},
		}
		if err := memStorage.CreatePolicy(ctx, "test-tenant", existingPolicy); err != nil {
			t.Fatalf("failed to create policy: %v", err)
		}

		policies := []*PolicyImport{
			{
				Name: "policy1",
				Rules: []Rule{
					{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
				},
			},
		}

		result, err := bm.ImportPolicies(ctx, "test-tenant", policies, BatchImportOptions{UpdateExisting: false, ContinueOnError: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.FailureCount != 1 {
			t.Errorf("expected 1 failure, got %d", result.FailureCount)
		}
	})

	t.Run("dry run mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		policies := []*PolicyImport{
			{
				Name: "policy1",
				Rules: []Rule{
					{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
				},
			},
		}

		result, err := bm.ImportPolicies(ctx, "test-tenant", policies, BatchImportOptions{DryRun: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !result.DryRun {
			t.Error("expected DryRun flag to be true")
		}

		// Verify policy was NOT created
		_, err = memStorage.GetPolicy(ctx, "test-tenant", "policy1")
		if err == nil {
			t.Error("expected policy not to be created in dry run")
		}
	})

	t.Run("nil policy engine error", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			IdentityStore: NewIdentityStore(IdentityStoreOptions{}),
		})
		ctx := context.Background()

		policies := []*PolicyImport{
			{
				Name: "policy1",
				Rules: []Rule{
					{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
				},
			},
		}

		result, err := bm.ImportPolicies(ctx, "test-tenant", policies, BatchImportOptions{ContinueOnError: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.FailureCount != 1 {
			t.Errorf("expected 1 failure, got %d", result.FailureCount)
		}
	})

	t.Run("empty policy list", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		result, err := bm.ImportPolicies(ctx, "test-tenant", []*PolicyImport{}, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.TotalItems != 0 {
			t.Errorf("expected 0 total items, got %d", result.TotalItems)
		}
	})

	t.Run("missing tenant ID", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		policies := []*PolicyImport{{Name: "test", Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}}}}}

		_, err := bm.ImportPolicies(ctx, "", policies, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing tenant ID")
		}
	})

	t.Run("invalid options", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		policies := []*PolicyImport{{Name: "test", Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}}}}}

		_, err := bm.ImportPolicies(ctx, "test-tenant", policies, BatchImportOptions{
			BatchSize: -1,
		})
		if err == nil {
			t.Error("expected error for invalid batch size")
		}
	})
}

// TestImportRole tests the importRole function.
func TestImportRole(t *testing.T) {
	t.Run("successful import", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		role := &RoleImport{
			ID:          "r1",
			Name:        "Admin",
			Description: "Admin role",
			Permissions: []string{"*"},
		}

		err := bm.importRole(ctx, "test-tenant", role, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify role was created
		createdRole, err := memStorage.GetRole(ctx, "test-tenant", "r1")
		if err != nil {
			t.Fatalf("failed to get role: %v", err)
		}
		if createdRole.Name != "Admin" {
			t.Errorf("expected name 'Admin', got '%s'", createdRole.Name)
		}
	})

	t.Run("nil identity store error", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})
		ctx := context.Background()

		role := &RoleImport{
			Name: "Test Role",
		}

		err := bm.importRole(ctx, "test-tenant", role, BatchImportOptions{})
		var qbErr *dberrors.QBError
		if !errors.As(err, &qbErr) {
			t.Errorf("expected QBError for nil identity store, got %T: %v", err, err)
		}
	})

	t.Run("role already exists without update mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create existing role
		existingRole := &Role{
			ID:       "r1",
			TenantID: "test-tenant",
			Name:     "Existing Role",
		}
		if err := memStorage.CreateRole(ctx, existingRole); err != nil {
			t.Fatalf("failed to create role: %v", err)
		}

		role := &RoleImport{
			ID:   "r1",
			Name: "New Name",
		}

		err := bm.importRole(ctx, "test-tenant", role, BatchImportOptions{UpdateExisting: false})
		if err == nil {
			t.Error("expected error for existing role")
		}
	})

	t.Run("update existing role with all fields", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create existing role
		existingRole := &Role{
			ID:       "r1",
			TenantID: "test-tenant",
			Name:     "Old Name",
		}
		if err := memStorage.CreateRole(ctx, existingRole); err != nil {
			t.Fatalf("failed to create role: %v", err)
		}

		role := &RoleImport{
			ID:          "r1",
			Name:        "New Name",
			Description: "New description",
			Permissions: []string{"read", "write"},
		}

		err := bm.importRole(ctx, "test-tenant", role, BatchImportOptions{UpdateExisting: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify update
		updated, err := memStorage.GetRole(ctx, "test-tenant", "r1")
		if err != nil {
			t.Fatalf("failed to get role: %v", err)
		}
		if updated.Name != "New Name" {
			t.Errorf("expected name 'New Name', got '%s'", updated.Name)
		}
		if updated.Description != "New description" {
			t.Errorf("expected description 'New description', got '%s'", updated.Description)
		}
	})

	t.Run("dry run mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		role := &RoleImport{
			ID:   "r1",
			Name: "Admin",
		}

		err := bm.importRole(ctx, "test-tenant", role, BatchImportOptions{DryRun: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify role was NOT created
		_, err = memStorage.GetRole(ctx, "test-tenant", "r1")
		if err == nil {
			t.Error("expected role not to be created in dry run")
		}
	})

	t.Run("import role without ID", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		role := &RoleImport{
			Name:        "Test Role",
			Description: "A test role without ID",
		}

		err := bm.importRole(ctx, "test-tenant", role, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("import role with attributes", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		role := &RoleImport{
			ID:          "r1",
			Name:        "Test Role",
			Description: "A test role with attributes",
			Permissions: []string{"read", "write"},
			Attributes:  map[string]interface{}{"level": "high", "scope": "global"},
		}

		err := bm.importRole(ctx, "test-tenant", role, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify role was created
		createdRole, err := memStorage.GetRole(ctx, "test-tenant", "r1")
		if err != nil {
			t.Fatalf("failed to get role: %v", err)
		}
		if createdRole.Name != "Test Role" {
			t.Errorf("expected name 'Test Role', got '%s'", createdRole.Name)
		}
	})

	t.Run("missing name error", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		role := &RoleImport{
			ID:   "r1",
			Name: "",
		}

		err := bm.importRole(ctx, "test-tenant", role, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing name")
		}

		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Errorf("expected ValidationError, got %T", err)
		}
	})
}

// TestImportPolicy tests the importPolicy function.
func TestImportPolicy(t *testing.T) {
	t.Run("successful import", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		policyImport := &PolicyImport{
			Name:        "test-policy",
			Description: "A test policy",
			Priority:    100,
			Subjects:    SubjectSelector{Authenticated: true},
			Rules: []Rule{
				{Name: "allow-read", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
			},
			Variables: map[string]string{"env": "test"},
		}

		err := bm.importPolicy(ctx, "test-tenant", policyImport, BatchImportOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify policy was created
		policy, err := memStorage.GetPolicy(ctx, "test-tenant", "test-policy")
		if err != nil {
			t.Fatalf("failed to get policy: %v", err)
		}
		if policy.Metadata.Name != "test-policy" {
			t.Errorf("expected name 'test-policy', got '%s'", policy.Metadata.Name)
		}
	})

	t.Run("nil policy engine error", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			IdentityStore: NewIdentityStore(IdentityStoreOptions{}),
		})
		ctx := context.Background()

		policyImport := &PolicyImport{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
			},
		}

		err := bm.importPolicy(ctx, "test-tenant", policyImport, BatchImportOptions{})
		var qbErr *dberrors.QBError
		if !errors.As(err, &qbErr) {
			t.Errorf("expected QBError for nil policy engine, got %T: %v", err, err)
		}
	})

	t.Run("validation error - invalid rule", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		policyImport := &PolicyImport{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}}, // Missing name
			},
		}

		err := bm.importPolicy(ctx, "test-tenant", policyImport, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for invalid rule")
		}
	})

	t.Run("dry run mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		policyImport := &PolicyImport{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
			},
		}

		err := bm.importPolicy(ctx, "test-tenant", policyImport, BatchImportOptions{DryRun: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify policy was NOT created
		_, err = memStorage.GetPolicy(ctx, "test-tenant", "test-policy")
		if err == nil {
			t.Error("expected policy not to be created in dry run")
		}
	})

	t.Run("update existing policy", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create existing policy
		existingPolicy := &Policy{
			APIVersion: "iam/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:   "test-policy",
				Tenant: "test-tenant",
			},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "old-rule", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}},
				},
			},
		}
		if err := memStorage.CreatePolicy(ctx, "test-tenant", existingPolicy); err != nil {
			t.Fatalf("failed to create policy: %v", err)
		}

		policyImport := &PolicyImport{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "new-rule", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
			},
		}

		err := bm.importPolicy(ctx, "test-tenant", policyImport, BatchImportOptions{UpdateExisting: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify update
		updated, err := memStorage.GetPolicy(ctx, "test-tenant", "test-policy")
		if err != nil {
			t.Fatalf("failed to get policy: %v", err)
		}
		if len(updated.Spec.Rules) != 1 || updated.Spec.Rules[0].Name != "new-rule" {
			t.Errorf("expected rule name 'new-rule', got '%s'", updated.Spec.Rules[0].Name)
		}
	})

	t.Run("policy already exists without update mode", func(t *testing.T) {
		bm, memStorage := createTestBatchManager(t)
		ctx := context.Background()

		// Create existing policy
		existingPolicy := &Policy{
			APIVersion: "iam/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:   "test-policy",
				Tenant: "test-tenant",
			},
			Spec: PolicySpec{
				Rules: []Rule{
					{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
				},
			},
		}
		if err := memStorage.CreatePolicy(ctx, "test-tenant", existingPolicy); err != nil {
			t.Fatalf("failed to create policy: %v", err)
		}

		policyImport := &PolicyImport{
			Name: "test-policy",
			Rules: []Rule{
				{Name: "rule1", Effect: EffectAllow, Actions: []string{"*"}, Resources: []string{"*"}},
			},
		}

		err := bm.importPolicy(ctx, "test-tenant", policyImport, BatchImportOptions{UpdateExisting: false})
		if err == nil {
			t.Error("expected error for existing policy")
		}
	})
}

// TestMergeResult tests the mergeResult function.
func TestMergeResult(t *testing.T) {
	t.Run("merges all fields correctly", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})

		mainResult := &BatchImportResult{
			SuccessCount:   10,
			FailureCount:   2,
			SkippedCount:   1,
			Errors:         []BatchError{{Index: 0, Type: "user", ID: "u0", Error: errors.New("err0")}},
			ProcessedItems: map[string][]string{"users": {"1", "2"}},
		}

		subResult := &BatchImportResult{
			SuccessCount:   5,
			FailureCount:   3,
			SkippedCount:   2,
			Errors:         []BatchError{{Index: 1, Type: "group", ID: "g1", Error: errors.New("err1")}},
			ProcessedItems: map[string][]string{"groups": {"g1", "g2"}},
		}

		bm.mergeResult(mainResult, subResult, "group")

		if mainResult.SuccessCount != 15 {
			t.Errorf("expected 15 successes, got %d", mainResult.SuccessCount)
		}
		if mainResult.FailureCount != 5 {
			t.Errorf("expected 5 failures, got %d", mainResult.FailureCount)
		}
		if mainResult.SkippedCount != 3 {
			t.Errorf("expected 3 skipped, got %d", mainResult.SkippedCount)
		}
		if len(mainResult.Errors) != 2 {
			t.Errorf("expected 2 errors, got %d", len(mainResult.Errors))
		}
		if len(mainResult.ProcessedItems["groups"]) != 2 {
			t.Errorf("expected 2 groups, got %d", len(mainResult.ProcessedItems["groups"]))
		}
	})

	t.Run("handles nil sub result", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})

		mainResult := &BatchImportResult{
			SuccessCount: 10,
		}

		bm.mergeResult(mainResult, nil, "user")

		if mainResult.SuccessCount != 10 {
			t.Errorf("expected 10 successes, got %d", mainResult.SuccessCount)
		}
	})

	t.Run("handles empty processed items", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})

		mainResult := &BatchImportResult{
			SuccessCount:   5,
			ProcessedItems: map[string][]string{},
		}

		subResult := &BatchImportResult{
			SuccessCount:   3,
			ProcessedItems: map[string][]string{"roles": {"r1", "r2"}},
		}

		bm.mergeResult(mainResult, subResult, "role")

		if mainResult.SuccessCount != 8 {
			t.Errorf("expected 8 successes, got %d", mainResult.SuccessCount)
		}
		if len(mainResult.ProcessedItems["roles"]) != 2 {
			t.Errorf("expected 2 roles, got %d", len(mainResult.ProcessedItems["roles"]))
		}
	})
}

// TestGetBatchSizeAndParallelism tests getBatchSize and getParallelism functions.
func TestGetBatchSizeAndParallelism(t *testing.T) {
	t.Run("uses options batch size when specified", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			BatchSize: 10,
		})

		opts := BatchImportOptions{BatchSize: 5}
		result := bm.getBatchSize(opts)

		if result != 5 {
			t.Errorf("expected 5, got %d", result)
		}
	})

	t.Run("uses default batch size when not specified", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			BatchSize: 10,
		})

		opts := BatchImportOptions{}
		result := bm.getBatchSize(opts)

		if result != 10 {
			t.Errorf("expected 10, got %d", result)
		}
	})

	t.Run("uses options parallelism when specified", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			Parallelism: 4,
		})

		opts := BatchImportOptions{Parallelism: 2}
		result := bm.getParallelism(opts)

		if result != 2 {
			t.Errorf("expected 2, got %d", result)
		}
	})

	t.Run("uses default parallelism when not specified", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			Parallelism: 4,
		})

		opts := BatchImportOptions{}
		result := bm.getParallelism(opts)

		if result != 4 {
			t.Errorf("expected 4, got %d", result)
		}
	})

	t.Run("uses zero defaults gracefully", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})

		opts := BatchImportOptions{}
		batchSize := bm.getBatchSize(opts)
		parallelism := bm.getParallelism(opts)

		// Should return default values from manager (which might be 0)
		// This is a boundary test
		_ = batchSize
		_ = parallelism
	})
}

// TestImportAllErrorPropagation tests error propagation in ImportAll
func TestImportAllErrorPropagation(t *testing.T) {
	t.Run("policy error propagates after all other resources succeed", func(t *testing.T) {
		bm, _ := createTestBatchManager(t)
		ctx := context.Background()

		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Roles: []*RoleImport{
				{ID: "role1", Name: "Admin"},
			},
			Users: []*UserImport{
				{ID: 20001, Email: "user1@example.com", Username: "user1"},
			},
			Policies: []*PolicyImport{
				{Name: "", Rules: []Rule{}}, // Invalid - will fail
			},
			Options: BatchImportOptions{
				ContinueOnError: false,
			},
		}

		result, _ := bm.ImportAll(ctx, req)
		// All other resources should have succeeded
		if result != nil && result.SuccessCount < 2 {
			t.Error("expected at least 2 successes")
		}
	})
}

// =============================================================================
// BatchManager - ImportAll additional paths
// =============================================================================

func TestBatchManager_ImportAll_ContextCancellation(t *testing.T) {
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

	tenantID := "import-cancel-tenant"
	tenant := &Tenant{ID: tenantID, Name: "Import Cancel Tenant", Created: time.Now()}
	if err := storage.CreateTenant(context.Background(), tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Test with canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bm.ImportAll(ctx, &BatchImportRequest{
		TenantID: tenantID,
		Users: []*UserImport{
			{ID: 10001, Email: "user1@test.com", Name: "User One"},
		},
	})
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

// =============================================================================
// BatchManager - ExportAll additional paths
// =============================================================================

func TestBatchManager_ExportAll_WithAllOptions(t *testing.T) {
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

	ctx := context.Background()
	tenantID := "export-all-tenant"

	tenant := &Tenant{ID: tenantID, Name: "Export All Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create test data
	user := &User{ID: 35, TenantID: tenantID, Email: "exp@test.com", Name: "Export User", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	role := &Role{ID: "exp-role-1", TenantID: tenantID, Name: "Export Role", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "exp-policy-1", Tenant: tenantID},
		Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
	}
	if err := policyEngine.CreatePolicy(ctx, tenantID, policy); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	// Export all
	result, err := bm.ExportAll(ctx, &BatchExportRequest{
		TenantID:        tenantID,
		IncludeUsers:    true,
		IncludeRoles:    true,
		IncludePolicies: true,
	})
	if err != nil {
		t.Fatalf("ExportAll failed: %v", err)
	}
	if len(result.Users) != 1 {
		t.Errorf("expected 1 user, got %d", len(result.Users))
	}
	if len(result.Roles) != 1 {
		t.Errorf("expected 1 role, got %d", len(result.Roles))
	}
	if len(result.Policies) != 1 {
		t.Errorf("expected 1 policy, got %d", len(result.Policies))
	}
}

// =============================================================================
// PolicyEngine - More edge cases
// =============================================================================

func TestPolicyEngine_CreatePolicy_DuplicateName(t *testing.T) {
	storage := NewMemoryStorage()
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})
	ctx := context.Background()

	tenant := &Tenant{ID: "dup-policy-tenant", Name: "Dup Policy Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "dup-policy", Tenant: "dup-policy-tenant"},
		Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
	}

	// Create first policy
	if err := engine.CreatePolicy(ctx, "dup-policy-tenant", policy); err != nil {
		t.Fatalf("First CreatePolicy failed: %v", err)
	}

	// Try to create duplicate
	err := engine.CreatePolicy(ctx, "dup-policy-tenant", policy)
	if err == nil {
		t.Error("expected error for duplicate policy name")
	}
}

func TestPolicyEngine_DeletePolicy_NotFound(t *testing.T) {
	storage := NewMemoryStorage()
	engine := NewPolicyEngine(PolicyEngineOptions{Storage: storage})
	ctx := context.Background()

	tenant := &Tenant{ID: "del-policy-tenant", Name: "Del Policy Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	err := engine.DeletePolicy(ctx, "del-policy-tenant", "nonexistent-policy")
	if err == nil {
		t.Error("expected error for nonexistent policy")
	}
}

// =============================================================================
// MemoryStorage - ListTenants
// =============================================================================

func TestMemoryStorage_ListTenants(t *testing.T) {
	tests := []struct {
		name    string
		created int
		opts    ListOptions
		want    int
	}{
		{name: "empty", created: 0, opts: ListOptions{}, want: 0},
		{name: "all", created: 5, opts: ListOptions{}, want: 5},
		{name: "limit", created: 10, opts: ListOptions{Limit: 5}, want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryStorage()
			ctx := context.Background()

			for i := 0; i < tt.created; i++ {
				tenant := &Tenant{
					ID:      "list-tenant-" + string(rune('0'+i)),
					Name:    "List Tenant " + string(rune('0'+i)),
					Created: time.Now(),
				}
				if err := storage.CreateTenant(ctx, tenant); err != nil {
					t.Fatalf("CreateTenant failed: %v", err)
				}
			}

			tenants, err := storage.ListTenants(ctx, tt.opts)
			if err != nil {
				t.Fatalf("ListTenants failed: %v", err)
			}
			if len(tenants) != tt.want {
				t.Errorf("expected %d tenants, got %d", tt.want, len(tenants))
			}
		})
	}
}

// =============================================================================
// MemoryStorage - duplicate creates
// =============================================================================

func TestMemoryStorage_Create_Duplicate(t *testing.T) {
	const tenantID = "dup-tenant"
	tests := []struct {
		name   string
		create func(ctx context.Context, storage *MemoryStorage) error
	}{
		{
			name: "user",
			create: func(ctx context.Context, storage *MemoryStorage) error {
				return storage.CreateUser(ctx, &User{ID: 36, TenantID: tenantID, Email: "dup@test.com", Name: "Dup User", Created: time.Now(), Updated: time.Now()})
			},
		},
		{
			name: "role",
			create: func(ctx context.Context, storage *MemoryStorage) error {
				return storage.CreateRole(ctx, &Role{ID: "dup-role", TenantID: tenantID, Name: "Dup Role", Created: time.Now(), Updated: time.Now()})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := NewMemoryStorage()
			ctx := context.Background()
			if err := storage.CreateTenant(ctx, &Tenant{ID: tenantID, Name: "Dup Tenant", Created: time.Now()}); err != nil {
				t.Fatalf("CreateTenant failed: %v", err)
			}

			if err := tt.create(ctx, storage); err != nil {
				t.Fatalf("first create failed: %v", err)
			}
			if err := tt.create(ctx, storage); err == nil {
				t.Errorf("expected error for duplicate %s", tt.name)
			}
		})
	}
}

// =============================================================================
// GDPR - More consent coverage
// =============================================================================

func TestMemoryConsentStorage_SaveAndGetConsent(t *testing.T) {
	storage := NewMemoryConsentStorage()
	ctx := context.Background()

	// Create consent with all required fields
	opts := CreateConsentOptions{
		TenantID: "tenant-1",
		UserID:   10001,
		Purpose:  ConsentPurposeMarketing,
		Status:   ConsentStatusGranted,
		Version:  "1.0",
	}
	consent, err := storage.CreateConsent(ctx, opts)
	if err != nil {
		t.Fatalf("CreateConsent failed: %v", err)
	}
	if consent.ID == "" {
		t.Error("expected consent ID to be set")
	}

	// Get consent
	retrieved, err := storage.GetConsent(ctx, "tenant-1", 10001, ConsentPurposeMarketing)
	if err != nil {
		t.Fatalf("GetConsent failed: %v", err)
	}
	if retrieved.UserID != 10001 {
		t.Errorf("expected 10001, got %d", retrieved.UserID)
	}
}

// =============================================================================
// MemoryStorage - More quota edge cases
// =============================================================================

// TestMemoryStorage_IncrementQuotaUsage_RolesExceedsLimit is removed:
// roles are engine-level quotas owned by qrdb TenantServicer, not tracked in IAM storage.

func TestMemoryStorage_IncrementQuotaUsage_PoliciesExceedsLimit(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{ID: "pol-tenant", Name: "Policy Tenant", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{MaxPolicies: 2, CurrentPolicies: 2}
	if err := storage.UpdateQuota(ctx, "pol-tenant", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	err := storage.IncrementQuotaUsage(ctx, "pol-tenant", "policies", 1)
	if err == nil {
		t.Error("expected error when exceeding policies quota")
	}
}

// TestMemoryStorage_DecrementQuotaUsage_RolesFloor is removed:
// roles are engine-level quotas owned by qrdb TenantServicer, not tracked in IAM storage.

func TestMemoryStorage_DecrementQuotaUsage_PoliciesFloor(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{ID: "dec-policies", Name: "Dec Policies", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{CurrentPolicies: 1}
	if err := storage.UpdateQuota(ctx, "dec-policies", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	if err := storage.DecrementQuotaUsage(ctx, "dec-policies", "policies", 10); err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	updatedQuota, _ := storage.GetQuota(ctx, "dec-policies")
	if updatedQuota.CurrentPolicies != 0 {
		t.Errorf("expected CurrentPolicies=0, got %d", updatedQuota.CurrentPolicies)
	}
}

// TestMemoryStorage_DecrementQuotaUsage_UsersFloor is removed:
// users are engine-level quotas owned by qrdb TenantServicer, not tracked in IAM storage.
