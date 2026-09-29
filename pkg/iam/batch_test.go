package iam

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// boolPtr states an explicit true/false where nil would mean "unspecified".
func boolPtr(b bool) *bool { return &b }

// assertEnabledStated checks that an import record says something definite
// about Enabled, and says the expected thing. The nil check is the load-bearing
// half: a converter that stopped setting the field at all would leave every
// user enabled on import, and an assertion that only dereferenced would panic
// rather than report it.
func assertEnabledStated(t *testing.T, got *bool, want bool, context string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: Enabled is unspecified; the export must state it", context)
		return
	}
	if *got != want {
		t.Errorf("%s: Enabled = %v, want %v", context, *got, want)
	}
}

func TestNewBatchManager(t *testing.T) {
	t.Run("with default options", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})

		if bm.batchSize != DefaultBatchSize {
			t.Errorf("expected batch size %d, got %d", DefaultBatchSize, bm.batchSize)
		}
		if bm.parallelism != DefaultParallelism {
			t.Errorf("expected parallelism %d, got %d", DefaultParallelism, bm.parallelism)
		}
	})

	t.Run("with custom options", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			BatchSize:   50,
			Parallelism: 8,
		})

		if bm.batchSize != 50 {
			t.Errorf("expected batch size 50, got %d", bm.batchSize)
		}
		if bm.parallelism != 8 {
			t.Errorf("expected parallelism 8, got %d", bm.parallelism)
		}
	})

	t.Run("with excessive batch size", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			BatchSize: MaxBatchSize + 1000,
		})

		if bm.batchSize != MaxBatchSize {
			t.Errorf("expected batch size to be capped at %d, got %d", MaxBatchSize, bm.batchSize)
		}
	})

	t.Run("with excessive parallelism", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			Parallelism: MaxParallelism + 10,
		})

		if bm.parallelism != MaxParallelism {
			t.Errorf("expected parallelism to be capped at %d, got %d", MaxParallelism, bm.parallelism)
		}
	})

	t.Run("with negative values", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{
			BatchSize:   -10,
			Parallelism: -5,
		})

		if bm.batchSize != DefaultBatchSize {
			t.Errorf("expected batch size to default to %d, got %d", DefaultBatchSize, bm.batchSize)
		}
		if bm.parallelism != DefaultParallelism {
			t.Errorf("expected parallelism to default to %d, got %d", DefaultParallelism, bm.parallelism)
		}
	})
}

func TestBatchImportRequestValidate(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Users: []*UserImport{
				{Email: "user@example.com", Username: "user1"},
			},
		}

		if err := req.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing tenant ID", func(t *testing.T) {
		req := &BatchImportRequest{
			Users: []*UserImport{
				{Email: "user@example.com", Username: "user1"},
			},
		}

		err := req.Validate()
		if err == nil {
			t.Error("expected error for missing tenant ID")
		}

		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Errorf("expected ValidationError, got %T", err)
		}
	})

	t.Run("empty batch", func(t *testing.T) {
		req := &BatchImportRequest{
			TenantID: "test-tenant",
		}

		err := req.Validate()
		if err == nil {
			t.Error("expected error for empty batch")
		}

		if !errors.Is(err, ErrBatchEmpty) {
			t.Errorf("expected ErrBatchEmpty, got %v", err)
		}
	})

	t.Run("invalid batch size", func(t *testing.T) {
		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Users: []*UserImport{
				{Email: "user@example.com", Username: "user1"},
			},
			Options: BatchImportOptions{
				BatchSize: MaxBatchSize + 1,
			},
		}

		err := req.Validate()
		if err == nil {
			t.Error("expected error for invalid batch size")
		}
	})

	t.Run("invalid parallelism", func(t *testing.T) {
		req := &BatchImportRequest{
			TenantID: "test-tenant",
			Users: []*UserImport{
				{Email: "user@example.com", Username: "user1"},
			},
			Options: BatchImportOptions{
				Parallelism: MaxParallelism + 1,
			},
		}

		err := req.Validate()
		if err == nil {
			t.Error("expected error for invalid parallelism")
		}
	})
}

func TestBatchOptionsValidate(t *testing.T) {
	t.Run("valid options", func(t *testing.T) {
		opts := BatchOptions{
			BatchSize:       100,
			Parallelism:     4,
			ContinueOnError: true,
			Timeout:         5 * time.Minute,
		}

		if err := opts.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("zero values are valid", func(t *testing.T) {
		opts := BatchImportOptions{}

		if err := opts.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("negative batch size", func(t *testing.T) {
		opts := BatchOptions{
			BatchSize: -1,
		}

		err := opts.Validate()
		if err == nil {
			t.Error("expected error for negative batch size")
		}
	})

	t.Run("negative parallelism", func(t *testing.T) {
		opts := BatchOptions{
			Parallelism: -1,
		}

		err := opts.Validate()
		if err == nil {
			t.Error("expected error for negative parallelism")
		}
	})
}

func TestBatchExportRequestValidate(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		req := &BatchExportRequest{
			TenantID:     "test-tenant",
			IncludeUsers: true,
		}

		if err := req.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing tenant ID", func(t *testing.T) {
		req := &BatchExportRequest{
			IncludeUsers: true,
		}

		err := req.Validate()
		if err == nil {
			t.Error("expected error for missing tenant ID")
		}
	})

	t.Run("no resource types selected", func(t *testing.T) {
		req := &BatchExportRequest{
			TenantID: "test-tenant",
		}

		err := req.Validate()
		if err == nil {
			t.Error("expected error when no resource types selected")
		}
	})

	t.Run("all resource types selected", func(t *testing.T) {
		req := &BatchExportRequest{
			TenantID:        "test-tenant",
			IncludeUsers:    true,
			IncludeRoles:    true,
			IncludePolicies: true,
		}

		if err := req.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestWorkerPool(t *testing.T) {
	t.Run("basic job processing", func(t *testing.T) {
		ctx := context.Background()
		wp := newWorkerPool(ctx, 2)
		wp.start()

		var processed int64

		// Submit jobs
		for i := 0; i < 10; i++ {
			idx := i
			submitted := wp.submit(workerJob{
				index:    idx,
				itemType: "test",
				itemID:   "id",
				fn: func(ctx context.Context) error {
					atomic.AddInt64(&processed, 1)
					return nil
				},
			})
			if !submitted {
				t.Error("failed to submit job")
			}
		}

		// Collect results
		resultCount := 0
		for resultCount < 10 {
			select {
			case res := <-wp.resultChan:
				resultCount++
				if res.err != nil {
					t.Errorf("unexpected error: %v", res.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for results")
			}
		}

		wp.stop()

		if atomic.LoadInt64(&processed) != 10 {
			t.Errorf("expected 10 processed, got %d", processed)
		}
	})

	t.Run("job with error", func(t *testing.T) {
		ctx := context.Background()
		wp := newWorkerPool(ctx, 1)
		wp.start()

		testErr := errors.New("test error")

		wp.submit(workerJob{
			index:    0,
			itemType: "test",
			itemID:   "id",
			fn: func(ctx context.Context) error {
				return testErr
			},
		})

		select {
		case res := <-wp.resultChan:
			if res.err == nil {
				t.Error("expected error")
			}
			if !errors.Is(res.err, testErr) {
				t.Errorf("expected test error, got %v", res.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for result")
		}

		wp.stop()
	})

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		wp := newWorkerPool(ctx, 2)
		wp.start()

		// Cancel immediately
		cancel()

		// Trying to submit after cancel should fail
		submitted := wp.submit(workerJob{
			index:    0,
			itemType: "test",
			itemID:   "id",
			fn: func(ctx context.Context) error {
				return nil
			},
		})

		// The submit might succeed or fail depending on timing,
		// but stop should complete without hanging
		wp.stop()
		_ = submitted // Ignore the result
	})
}

func TestProgressTracker(t *testing.T) {
	t.Run("basic progress tracking", func(t *testing.T) {
		var lastProgress BatchImportProgress
		progressFunc := func(p BatchImportProgress) {
			lastProgress = p
		}

		pt := newProgressTracker(10, progressFunc)

		pt.setCurrentType("user")
		pt.recordSuccess()
		pt.recordSuccess()
		pt.recordFailure()

		progress := pt.getProgress()

		if progress.TotalItems != 10 {
			t.Errorf("expected total 10, got %d", progress.TotalItems)
		}
		if progress.ProcessedItems != 3 {
			t.Errorf("expected processed 3, got %d", progress.ProcessedItems)
		}
		if progress.SuccessCount != 2 {
			t.Errorf("expected success 2, got %d", progress.SuccessCount)
		}
		if progress.FailureCount != 1 {
			t.Errorf("expected failure 1, got %d", progress.FailureCount)
		}
		if progress.CurrentType != "user" {
			t.Errorf("expected type 'user', got '%s'", progress.CurrentType)
		}
		if progress.PercentComplete != 30.0 {
			t.Errorf("expected 30%%, got %.2f%%", progress.PercentComplete)
		}

		// Verify callback was called
		if lastProgress.ProcessedItems != 3 {
			t.Error("progress callback was not called correctly")
		}
	})

	t.Run("no progress callback", func(t *testing.T) {
		pt := newProgressTracker(5, nil)
		pt.recordSuccess()
		pt.recordSuccess()

		progress := pt.getProgress()
		if progress.SuccessCount != 2 {
			t.Errorf("expected success 2, got %d", progress.SuccessCount)
		}
	})

	t.Run("zero total items", func(t *testing.T) {
		pt := newProgressTracker(0, nil)
		pt.recordSuccess()

		progress := pt.getProgress()
		if progress.PercentComplete != 0 {
			t.Errorf("expected 0%% for zero total, got %.2f%%", progress.PercentComplete)
		}
	})
}

func TestBatchResult(t *testing.T) {
	t.Run("batch result fields", func(t *testing.T) {
		result := &BatchResult{
			TotalItems:   100,
			SuccessCount: 95,
			FailureCount: 3,
			SkippedCount: 2,
			Duration:     5 * time.Second,
			ProcessedItems: map[string][]string{
				"users":  {"u1", "u2"},
				"groups": {"g1"},
			},
			DryRun: false,
			Errors: []BatchError{
				{Index: 5, Type: "user", ID: "u5", Error: errors.New("test error")},
			},
		}

		if result.TotalItems != 100 {
			t.Errorf("expected total 100, got %d", result.TotalItems)
		}
		if result.SuccessCount != 95 {
			t.Errorf("expected success 95, got %d", result.SuccessCount)
		}
		if result.FailureCount != 3 {
			t.Errorf("expected failure 3, got %d", result.FailureCount)
		}
		if result.SkippedCount != 2 {
			t.Errorf("expected skipped 2, got %d", result.SkippedCount)
		}
		if len(result.Errors) != 1 {
			t.Errorf("expected 1 error, got %d", len(result.Errors))
		}
		if result.ProcessedItems["users"][0] != "u1" {
			t.Errorf("expected first user 'u1', got '%s'", result.ProcessedItems["users"][0])
		}
	})
}

func TestBatchError(t *testing.T) {
	t.Run("batch error fields", func(t *testing.T) {
		testErr := errors.New("test error")
		batchErr := BatchError{
			Index: 42,
			Type:  "user",
			ID:    "user-123",
			Error: testErr,
		}

		if batchErr.Index != 42 {
			t.Errorf("expected index 42, got %d", batchErr.Index)
		}
		if batchErr.Type != "user" {
			t.Errorf("expected type 'user', got '%s'", batchErr.Type)
		}
		if batchErr.ID != "user-123" {
			t.Errorf("expected ID 'user-123', got '%s'", batchErr.ID)
		}
		if batchErr.Error != testErr {
			t.Error("error not preserved correctly")
		}
	})
}

func TestUserImport(t *testing.T) {
	t.Run("user import fields", func(t *testing.T) {
		user := &UserImport{
			ID:       "u1",
			Email:    "test@example.com",
			Username: "testuser",
			Name:     "Test User",
			Attributes: map[string]string{
				"department": "engineering",
			},
			Roles:   []string{"r1"},
			Enabled: boolPtr(true),
		}

		if user.ID != "u1" {
			t.Errorf("expected ID 'u1', got '%s'", user.ID)
		}
		if user.Email != "test@example.com" {
			t.Errorf("expected email 'test@example.com', got '%s'", user.Email)
		}
		assertEnabledStated(t, user.Enabled, true, "user")
	})
}

func TestRoleImport(t *testing.T) {
	t.Run("role import fields", func(t *testing.T) {
		role := &RoleImport{
			ID:          "r1",
			Name:        "Admin",
			Description: "Administrator role",
			Permissions: []string{"read", "write", "delete"},
			Attributes: map[string]interface{}{
				"level": "high",
			},
		}

		if role.ID != "r1" {
			t.Errorf("expected ID 'r1', got '%s'", role.ID)
		}
		if role.Name != "Admin" {
			t.Errorf("expected name 'Admin', got '%s'", role.Name)
		}
		if len(role.Permissions) != 3 {
			t.Errorf("expected 3 permissions, got %d", len(role.Permissions))
		}
	})
}

func TestPolicyImport(t *testing.T) {
	t.Run("policy import fields", func(t *testing.T) {
		policy := &PolicyImport{
			Name:        "admin-policy",
			Description: "Admin access policy",
			Priority:    100,
			Subjects: SubjectSelector{
				Roles: []string{"admin"},
			},
			Rules: []Rule{
				{
					Name:      "allow-all",
					Effect:    EffectAllow,
					Actions:   []string{"*"},
					Resources: []string{"*"},
				},
			},
			Variables: map[string]string{
				"env": "production",
			},
		}

		if policy.Name != "admin-policy" {
			t.Errorf("expected name 'admin-policy', got '%s'", policy.Name)
		}
		if policy.Priority != 100 {
			t.Errorf("expected priority 100, got %d", policy.Priority)
		}
		if len(policy.Rules) != 1 {
			t.Errorf("expected 1 rule, got %d", len(policy.Rules))
		}
	})
}

func TestExportFilter(t *testing.T) {
	t.Run("export filter with all fields", func(t *testing.T) {
		now := time.Now()
		yesterday := now.Add(-24 * time.Hour)

		filter := ExportFilter{
			UserIDs:       []string{"u1", "u2"},
			RoleIDs:       []string{"r1", "r2", "r3"},
			PolicyNames:   []string{"policy1"},
			CreatedAfter:  &yesterday,
			CreatedBefore: &now,
			UpdatedAfter:  &yesterday,
			UpdatedBefore: &now,
		}

		if len(filter.UserIDs) != 2 {
			t.Errorf("expected 2 user IDs, got %d", len(filter.UserIDs))
		}
		if filter.CreatedAfter == nil {
			t.Error("expected CreatedAfter to be set")
		}
	})
}

func TestBatchProgress(t *testing.T) {
	t.Run("batch progress fields", func(t *testing.T) {
		progress := BatchProgress{
			TotalItems:      100,
			ProcessedItems:  50,
			SuccessCount:    45,
			FailureCount:    5,
			CurrentType:     "user",
			PercentComplete: 50.0,
		}

		if progress.TotalItems != 100 {
			t.Errorf("expected total 100, got %d", progress.TotalItems)
		}
		if progress.ProcessedItems != 50 {
			t.Errorf("expected processed 50, got %d", progress.ProcessedItems)
		}
		if progress.PercentComplete != 50.0 {
			t.Errorf("expected 50%%, got %.2f%%", progress.PercentComplete)
		}
	})
}

func TestBatchErrorTypes(t *testing.T) {
	t.Run("sentinel errors", func(t *testing.T) {
		if errBatchOperationFailed.Error() != "batch operation failed" {
			t.Errorf("unexpected error message: %s", errBatchOperationFailed.Error())
		}
		if errBatchTimeout.Error() != "batch operation timed out" {
			t.Errorf("unexpected error message: %s", errBatchTimeout.Error())
		}
		if errBatchValidationFailed.Error() != "batch validation failed" {
			t.Errorf("unexpected error message: %s", errBatchValidationFailed.Error())
		}
		if errBatchCanceled.Error() != "batch operation canceled" {
			t.Errorf("unexpected error message: %s", errBatchCanceled.Error())
		}
		if errBatchEmpty.Error() != "batch operation has no items" {
			t.Errorf("unexpected error message: %s", errBatchEmpty.Error())
		}
		if errInvalidBatchOptions.Error() != "invalid batch options" {
			t.Errorf("unexpected error message: %s", errInvalidBatchOptions.Error())
		}
	})
}

func TestBatchManagerGetBatchSize(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{
		BatchSize: 50,
	})

	t.Run("uses manager default when option is zero", func(t *testing.T) {
		opts := BatchImportOptions{}
		if bm.getBatchSize(opts) != 50 {
			t.Errorf("expected 50, got %d", bm.getBatchSize(opts))
		}
	})

	t.Run("uses option when specified", func(t *testing.T) {
		opts := BatchImportOptions{BatchSize: 25}
		if bm.getBatchSize(opts) != 25 {
			t.Errorf("expected 25, got %d", bm.getBatchSize(opts))
		}
	})
}

func TestBatchManagerGetParallelism(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{
		Parallelism: 8,
	})

	t.Run("uses manager default when option is zero", func(t *testing.T) {
		opts := BatchImportOptions{}
		if bm.getParallelism(opts) != 8 {
			t.Errorf("expected 8, got %d", bm.getParallelism(opts))
		}
	})

	t.Run("uses option when specified", func(t *testing.T) {
		opts := BatchImportOptions{Parallelism: 2}
		if bm.getParallelism(opts) != 2 {
			t.Errorf("expected 2, got %d", bm.getParallelism(opts))
		}
	})
}

func TestBatchManagerMergeResult(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})

	t.Run("merges results correctly", func(t *testing.T) {
		mainResult := &BatchImportResult{
			SuccessCount: 5,
			FailureCount: 1,
			SkippedCount: 0,
			Errors:       []BatchError{{Index: 0, Type: "user", ID: "u0", Error: errors.New("err0")}},
			ProcessedItems: map[string][]string{
				"users": {"u1", "u2"},
			},
		}

		subResult := &BatchImportResult{
			SuccessCount: 3,
			FailureCount: 2,
			SkippedCount: 1,
			Errors: []BatchError{
				{Index: 1, Type: "user", ID: "u3", Error: errors.New("err1")},
				{Index: 2, Type: "user", ID: "u4", Error: errors.New("err2")},
			},
			ProcessedItems: map[string][]string{
				"users": {"u5", "u6", "u7"},
			},
		}

		bm.mergeResult(mainResult, subResult, "user")

		if mainResult.SuccessCount != 8 {
			t.Errorf("expected success 8, got %d", mainResult.SuccessCount)
		}
		if mainResult.FailureCount != 3 {
			t.Errorf("expected failure 3, got %d", mainResult.FailureCount)
		}
		if mainResult.SkippedCount != 1 {
			t.Errorf("expected skipped 1, got %d", mainResult.SkippedCount)
		}
		if len(mainResult.Errors) != 3 {
			t.Errorf("expected 3 errors, got %d", len(mainResult.Errors))
		}
		if len(mainResult.ProcessedItems["users"]) != 5 {
			t.Errorf("expected 5 users, got %d", len(mainResult.ProcessedItems["users"]))
		}
	})

	t.Run("handles nil subResult", func(t *testing.T) {
		mainResult := &BatchImportResult{
			SuccessCount: 5,
		}

		bm.mergeResult(mainResult, nil, "user")

		if mainResult.SuccessCount != 5 {
			t.Errorf("expected success 5, got %d", mainResult.SuccessCount)
		}
	})
}

func TestBatchExportResultConvertToImportRequest(t *testing.T) {
	t.Run("converts export to import request", func(t *testing.T) {
		now := time.Now()
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
			Users: []*User{
				{
					ID:       "u1",
					Email:    "user1@example.com",
					Name:     "User One",
					TenantID: "source-tenant",
					Roles:    []string{"r1"},
					Created:  now,
					Updated:  now,
				},
			},
			Roles: []*Role{
				{
					ID:          "r1",
					Name:        "Role One",
					Description: "Test role",
					TenantID:    "source-tenant",
					Permissions: []string{"read"},
					Created:     now,
					Updated:     now,
				},
			},
			Policies: []*Policy{
				{
					APIVersion: "iam/v1",
					Kind:       "Policy",
					Metadata: PolicyMetadata{
						Name:        "policy1",
						Tenant:      "source-tenant",
						Description: "Test policy",
						Priority:    10,
					},
					Spec: PolicySpec{
						Subjects: SubjectSelector{
							Roles: []string{"r1"},
						},
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
			Duration: time.Second,
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		if importReq.TenantID != "target-tenant" {
			t.Errorf("expected target tenant, got '%s'", importReq.TenantID)
		}
		if len(importReq.Users) != 1 {
			t.Errorf("expected 1 user, got %d", len(importReq.Users))
		}
		if len(importReq.Roles) != 1 {
			t.Errorf("expected 1 role, got %d", len(importReq.Roles))
		}
		if len(importReq.Policies) != 1 {
			t.Errorf("expected 1 policy, got %d", len(importReq.Policies))
		}

		// Verify user conversion
		if importReq.Users[0].ID != "u1" {
			t.Errorf("expected user ID 'u1', got '%s'", importReq.Users[0].ID)
		}
		if importReq.Users[0].Email != "user1@example.com" {
			t.Errorf("expected email 'user1@example.com', got '%s'", importReq.Users[0].Email)
		}

		// Verify role conversion
		if len(importReq.Roles[0].Permissions) != 1 {
			t.Errorf("expected 1 permission, got %d", len(importReq.Roles[0].Permissions))
		}

		// Verify policy conversion
		if importReq.Policies[0].Name != "policy1" {
			t.Errorf("expected policy name 'policy1', got '%s'", importReq.Policies[0].Name)
		}
	})

	t.Run("handles empty export result", func(t *testing.T) {
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		if importReq.TenantID != "target-tenant" {
			t.Errorf("expected target tenant, got '%s'", importReq.TenantID)
		}
		if importReq.Users != nil {
			t.Error("expected nil users")
		}
	})

	t.Run("handles suspended user", func(t *testing.T) {
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
			Users: []*User{
				{
					ID:        "u1",
					Email:     "suspended@example.com",
					Name:      "Suspended User",
					TenantID:  "source-tenant",
					Lifecycle: LifecycleSuspended,
				},
			},
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		assertEnabledStated(t, importReq.Users[0].Enabled, false, "suspended user")
	})

	t.Run("handles locked user", func(t *testing.T) {
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
			Users: []*User{
				{
					ID:        "u1",
					Email:     "locked@example.com",
					Name:      "Locked User",
					TenantID:  "source-tenant",
					Lifecycle: LifecycleLocked,
				},
			},
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		assertEnabledStated(t, importReq.Users[0].Enabled, false, "locked user")
	})
}

func TestUserMatchesFilter(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)

	user := &User{
		ID:      "u1",
		Email:   "test@example.com",
		Name:    "Test User",
		Created: now,
		Updated: now,
	}

	t.Run("matches with empty filter", func(t *testing.T) {
		filter := ExportFilter{}
		if !bm.userMatchesFilter(user, filter) {
			t.Error("expected user to match empty filter")
		}
	})

	t.Run("matches with ID filter", func(t *testing.T) {
		filter := ExportFilter{UserIDs: []string{"u1", "u2"}}
		if !bm.userMatchesFilter(user, filter) {
			t.Error("expected user to match ID filter")
		}
	})

	t.Run("does not match wrong ID filter", func(t *testing.T) {
		filter := ExportFilter{UserIDs: []string{"u2", "u3"}}
		if bm.userMatchesFilter(user, filter) {
			t.Error("expected user to not match ID filter")
		}
	})

	t.Run("matches created time filter", func(t *testing.T) {
		filter := ExportFilter{
			CreatedAfter:  &yesterday,
			CreatedBefore: &tomorrow,
		}
		if !bm.userMatchesFilter(user, filter) {
			t.Error("expected user to match time filter")
		}
	})

	t.Run("does not match created after filter", func(t *testing.T) {
		filter := ExportFilter{CreatedAfter: &tomorrow}
		if bm.userMatchesFilter(user, filter) {
			t.Error("expected user to not match created after filter")
		}
	})

	t.Run("does not match created before filter", func(t *testing.T) {
		filter := ExportFilter{CreatedBefore: &yesterday}
		if bm.userMatchesFilter(user, filter) {
			t.Error("expected user to not match created before filter")
		}
	})

	t.Run("matches updated time filter", func(t *testing.T) {
		filter := ExportFilter{
			UpdatedAfter:  &yesterday,
			UpdatedBefore: &tomorrow,
		}
		if !bm.userMatchesFilter(user, filter) {
			t.Error("expected user to match updated time filter")
		}
	})
}

func TestRoleMatchesFilter(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	now := time.Now()

	role := &Role{
		ID:      "r1",
		Name:    "Test Role",
		Created: now,
		Updated: now,
	}

	t.Run("matches with empty filter", func(t *testing.T) {
		filter := ExportFilter{}
		if !bm.roleMatchesFilter(role, filter) {
			t.Error("expected role to match empty filter")
		}
	})

	t.Run("matches with ID filter", func(t *testing.T) {
		filter := ExportFilter{RoleIDs: []string{"r1", "r2"}}
		if !bm.roleMatchesFilter(role, filter) {
			t.Error("expected role to match ID filter")
		}
	})

	t.Run("does not match wrong ID filter", func(t *testing.T) {
		filter := ExportFilter{RoleIDs: []string{"r2"}}
		if bm.roleMatchesFilter(role, filter) {
			t.Error("expected role to not match ID filter")
		}
	})
}

func TestPolicyMatchesFilter(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)

	policy := &Policy{
		Metadata: PolicyMetadata{
			Name:    "test-policy",
			Created: now,
			Updated: now,
		},
	}

	t.Run("matches with empty filter", func(t *testing.T) {
		filter := ExportFilter{}
		if !bm.policyMatchesFilter(policy, filter) {
			t.Error("expected policy to match empty filter")
		}
	})

	t.Run("matches with name filter", func(t *testing.T) {
		filter := ExportFilter{PolicyNames: []string{"test-policy", "other-policy"}}
		if !bm.policyMatchesFilter(policy, filter) {
			t.Error("expected policy to match name filter")
		}
	})

	t.Run("does not match wrong name filter", func(t *testing.T) {
		filter := ExportFilter{PolicyNames: []string{"other-policy"}}
		if bm.policyMatchesFilter(policy, filter) {
			t.Error("expected policy to not match name filter")
		}
	})

	t.Run("does not match created before filter", func(t *testing.T) {
		filter := ExportFilter{CreatedBefore: &yesterday}
		if bm.policyMatchesFilter(policy, filter) {
			t.Error("expected policy to not match created before filter")
		}
	})
}

func TestImportUsersEmptySlice(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	result, err := bm.ImportUsers(ctx, "test-tenant", []*UserImport{}, BatchImportOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.TotalItems != 0 {
		t.Errorf("expected 0 total items, got %d", result.TotalItems)
	}
}

func TestImportRolesEmptySlice(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	result, err := bm.ImportRoles(ctx, "test-tenant", []*RoleImport{}, BatchImportOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.TotalItems != 0 {
		t.Errorf("expected 0 total items, got %d", result.TotalItems)
	}
}

func TestImportPoliciesEmptySlice(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	result, err := bm.ImportPolicies(ctx, "test-tenant", []*PolicyImport{}, BatchImportOptions{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.TotalItems != 0 {
		t.Errorf("expected 0 total items, got %d", result.TotalItems)
	}
}

func TestImportUsersMissingTenantID(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ImportUsers(ctx, "", []*UserImport{{Email: "test@example.com"}}, BatchImportOptions{})
	if err == nil {
		t.Error("expected error for missing tenant ID")
	}
}

func TestImportRolesMissingTenantID(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ImportRoles(ctx, "", []*RoleImport{{Name: "test"}}, BatchImportOptions{})
	if err == nil {
		t.Error("expected error for missing tenant ID")
	}
}

func TestImportPoliciesMissingTenantID(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ImportPolicies(ctx, "", []*PolicyImport{{Name: "test"}}, BatchImportOptions{})
	if err == nil {
		t.Error("expected error for missing tenant ID")
	}
}

func TestExportUsersMissingTenantID(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ExportUsers(ctx, "", ExportFilter{})
	if err == nil {
		t.Error("expected error for missing tenant ID")
	}
}

func TestExportRolesMissingTenantID(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ExportRoles(ctx, "", ExportFilter{})
	if err == nil {
		t.Error("expected error for missing tenant ID")
	}
}

func TestExportPoliciesMissingTenantID(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ExportPolicies(ctx, "", ExportFilter{})
	if err == nil {
		t.Error("expected error for missing tenant ID")
	}
}

func TestExportUsersNoIdentityStore(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ExportUsers(ctx, "test-tenant", ExportFilter{})
	if err == nil {
		t.Error("expected error for missing identity store")
	}
}

func TestExportRolesNoIdentityStore(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ExportRoles(ctx, "test-tenant", ExportFilter{})
	if err == nil {
		t.Error("expected error for missing identity store")
	}
}

func TestExportPoliciesNoPolicyEngine(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	_, err := bm.ExportPolicies(ctx, "test-tenant", ExportFilter{})
	if err == nil {
		t.Error("expected error for missing policy engine")
	}
}

func TestImportUserNoIdentityStore(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	err := bm.importUser(ctx, "test-tenant", &UserImport{
		Email:    "test@example.com",
		Username: "testuser",
	}, BatchImportOptions{})
	if err == nil {
		t.Error("expected error for missing identity store")
	}
}

func TestImportRoleNoIdentityStore(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	err := bm.importRole(ctx, "test-tenant", &RoleImport{
		Name: "test-role",
	}, BatchImportOptions{})
	if err == nil {
		t.Error("expected error for missing identity store")
	}
}

func TestImportPolicyNoPolicyEngine(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	err := bm.importPolicy(ctx, "test-tenant", &PolicyImport{
		Name: "test-policy",
		Rules: []Rule{
			{
				Name:      "test-rule",
				Effect:    EffectAllow,
				Actions:   []string{"*"},
				Resources: []string{"*"},
			},
		},
	}, BatchImportOptions{})
	if err == nil {
		t.Error("expected error for missing policy engine")
	}
}

func TestImportUserValidation(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{}),
	})
	ctx := context.Background()

	t.Run("missing email", func(t *testing.T) {
		err := bm.importUser(ctx, "test-tenant", &UserImport{
			Username: "testuser",
		}, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing email")
		}
	})

	t.Run("missing username", func(t *testing.T) {
		err := bm.importUser(ctx, "test-tenant", &UserImport{
			Email: "test@example.com",
		}, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing username")
		}
	})

	t.Run("dry run validates only", func(t *testing.T) {
		err := bm.importUser(ctx, "test-tenant", &UserImport{
			Email:    "test@example.com",
			Username: "testuser",
		}, BatchImportOptions{DryRun: true})
		if err != nil {
			t.Errorf("unexpected error in dry run: %v", err)
		}
	})
}

func TestImportRoleValidation(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{}),
	})
	ctx := context.Background()

	t.Run("missing name", func(t *testing.T) {
		err := bm.importRole(ctx, "test-tenant", &RoleImport{}, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing name")
		}
	})

	t.Run("dry run validates only", func(t *testing.T) {
		err := bm.importRole(ctx, "test-tenant", &RoleImport{
			Name: "test-role",
		}, BatchImportOptions{DryRun: true})
		if err != nil {
			t.Errorf("unexpected error in dry run: %v", err)
		}
	})
}

func TestImportPolicyValidation(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{
		PolicyEngine: NewPolicyEngine(PolicyEngineOptions{}),
	})
	ctx := context.Background()

	t.Run("missing name", func(t *testing.T) {
		err := bm.importPolicy(ctx, "test-tenant", &PolicyImport{
			Rules: []Rule{
				{
					Name:      "test-rule",
					Effect:    EffectAllow,
					Actions:   []string{"*"},
					Resources: []string{"*"},
				},
			},
		}, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing name")
		}
	})

	t.Run("missing rules", func(t *testing.T) {
		err := bm.importPolicy(ctx, "test-tenant", &PolicyImport{
			Name: "test-policy",
		}, BatchImportOptions{})
		if err == nil {
			t.Error("expected error for missing rules")
		}
	})

	t.Run("dry run validates only", func(t *testing.T) {
		err := bm.importPolicy(ctx, "test-tenant", &PolicyImport{
			Name: "test-policy",
			Rules: []Rule{
				{
					Name:      "test-rule",
					Effect:    EffectAllow,
					Actions:   []string{"*"},
					Resources: []string{"*"},
				},
			},
		}, BatchImportOptions{DryRun: true})
		if err != nil {
			t.Errorf("unexpected error in dry run: %v", err)
		}
	})
}

func TestImportAllValidation(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	t.Run("invalid request", func(t *testing.T) {
		_, err := bm.ImportAll(ctx, &BatchImportRequest{})
		if err == nil {
			t.Error("expected error for invalid request")
		}
	})
}

func TestExportAllValidation(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	ctx := context.Background()

	t.Run("invalid request", func(t *testing.T) {
		_, err := bm.ExportAll(ctx, &BatchExportRequest{})
		if err == nil {
			t.Error("expected error for invalid request")
		}
	})
}

func TestBatchConstants(t *testing.T) {
	if DefaultBatchSize <= 0 {
		t.Error("DefaultBatchSize must be positive")
	}
	if DefaultParallelism <= 0 {
		t.Error("DefaultParallelism must be positive")
	}
	if MaxBatchSize <= DefaultBatchSize {
		t.Error("MaxBatchSize must be greater than DefaultBatchSize")
	}
	if MaxParallelism <= DefaultParallelism {
		t.Error("MaxParallelism must be greater than DefaultParallelism")
	}
	if DefaultTimeout <= 0 {
		t.Error("DefaultTimeout must be positive")
	}
}

func TestPolicyImportItem_Validate(t *testing.T) {
	tests := []struct {
		name    string
		item    *PolicyImportItem
		wantErr bool
	}{
		{
			name: "valid with name",
			item: &PolicyImportItem{
				Name: "test-policy",
			},
			wantErr: false,
		},
		{
			name: "valid with policy object",
			item: &PolicyImportItem{
				Policy: &Policy{
					APIVersion: "v1",
					Kind:       "Policy",
				},
			},
			wantErr: false,
		},
		{
			name:    "invalid - no name or policy",
			item:    &PolicyImportItem{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.item.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestBatchOptions_Validate_Coverage tests BatchOptions validation edge cases.
func TestBatchOptions_Validate_Coverage(t *testing.T) {
	tests := []struct {
		name    string
		opts    *BatchOptions
		wantErr bool
		field   string
	}{
		{
			name:    "valid - empty options",
			opts:    &BatchOptions{},
			wantErr: false,
		},
		{
			name: "valid - all positive values",
			opts: &BatchOptions{
				BatchSize:   100,
				Parallelism: 4,
				RateLimit:   10,
			},
			wantErr: false,
		},
		{
			name: "invalid - negative batch size",
			opts: &BatchOptions{
				BatchSize: -1,
			},
			wantErr: true,
			field:   "batch_size",
		},
		{
			name: "invalid - negative parallelism",
			opts: &BatchOptions{
				Parallelism: -1,
			},
			wantErr: true,
			field:   "parallelism",
		},
		{
			name: "invalid - negative rate limit",
			opts: &BatchOptions{
				RateLimit: -1,
			},
			wantErr: true,
			field:   "rate_limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.field != "" {
				var vErr *ValidationError
				if errors.As(err, &vErr) {
					if vErr.Field != tt.field {
						t.Errorf("Validate() field = %s, want %s", vErr.Field, tt.field)
					}
				}
			}
		})
	}
}

// TestBatchOperation_Validate_Coverage tests BatchOperation validation edge cases.
func TestBatchOperation_Validate_Coverage(t *testing.T) {
	tests := []struct {
		name    string
		op      *BatchOperation
		wantErr bool
		field   string
	}{
		{
			name: "valid operation",
			op: &BatchOperation{
				ID:       "op-123",
				TenantID: "tenant-1",
				Type:     "user_import",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			op: &BatchOperation{
				TenantID: "tenant-1",
				Type:     "user_import",
			},
			wantErr: true,
			field:   "id",
		},
		{
			name: "missing tenant ID",
			op: &BatchOperation{
				ID:   "op-123",
				Type: "user_import",
			},
			wantErr: true,
			field:   "tenant_id",
		},
		{
			name: "missing type",
			op: &BatchOperation{
				ID:       "op-123",
				TenantID: "tenant-1",
			},
			wantErr: true,
			field:   "type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && tt.field != "" {
				var vErr *ValidationError
				if errors.As(err, &vErr) {
					if vErr.Field != tt.field {
						t.Errorf("Validate() field = %s, want %s", vErr.Field, tt.field)
					}
				}
			}
		})
	}
}

// TestUserImportItem_ValidateCoverage tests UserImportItem validation.
func TestUserImportItem_ValidateCoverage(t *testing.T) {
	tests := []struct {
		name    string
		item    *UserImportItem
		wantErr bool
	}{
		{
			name: "valid user with email",
			item: &UserImportItem{
				Email: "test@example.com",
			},
			wantErr: false,
		},
		{
			name: "valid user with email and username",
			item: &UserImportItem{
				Username: "testuser",
				Email:    "test@example.com",
			},
			wantErr: false,
		},
		{
			name:    "missing email",
			item:    &UserImportItem{},
			wantErr: true,
		},
		{
			name: "username without email",
			item: &UserImportItem{
				Username: "testuser",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.item.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestRoleImportItem_ValidateCoverage tests RoleImportItem validation.
func TestRoleImportItem_ValidateCoverage(t *testing.T) {
	tests := []struct {
		name    string
		item    *RoleImportItem
		wantErr bool
	}{
		{
			name: "valid role",
			item: &RoleImportItem{
				Name: "admin",
			},
			wantErr: false,
		},
		{
			name: "valid role with description",
			item: &RoleImportItem{
				Name:        "admin",
				Description: "Administrator role",
			},
			wantErr: false,
		},
		{
			name:    "missing name",
			item:    &RoleImportItem{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.item.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestBatchError_ErrorMessage tests BatchError error message.
func TestBatchError_ErrorMessage(t *testing.T) {
	err := errors.New("test error")
	be := &BatchError{
		Index: 5,
		Type:  "user",
		ID:    "user-123",
		Error: err,
	}

	msg := be.Error.Error()
	if msg != "test error" {
		t.Errorf("Error() = %s, want %s", msg, "test error")
	}
}
