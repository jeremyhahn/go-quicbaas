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
					ID:       30001,
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
		if importReq.Users[0].ID != 30001 {
			t.Errorf("expected user ID 30001, got %d", importReq.Users[0].ID)
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
					ID:        30001,
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
					ID:        30001,
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
		ID:      30001,
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
		filter := ExportFilter{UserIDs: []uint64{30001, 30002}}
		if !bm.userMatchesFilter(user, filter) {
			t.Error("expected user to match ID filter")
		}
	})

	t.Run("does not match wrong ID filter", func(t *testing.T) {
		filter := ExportFilter{UserIDs: []uint64{30002, 30003}}
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

// TestBatchManagerSingleTypeImport covers the input checks each single-type
// import makes before it touches a store: an empty slice is a successful
// no-op, and a missing tenant ID is a validation error.
func TestBatchManagerSingleTypeImport(t *testing.T) {
	importers := map[string]func(bm *BatchManager, tenantID string, empty bool) (*BatchImportResult, error){
		"users": func(bm *BatchManager, tenantID string, empty bool) (*BatchImportResult, error) {
			items := []*UserImport{{Email: "test@example.com"}}
			if empty {
				items = []*UserImport{}
			}
			return bm.ImportUsers(context.Background(), tenantID, items, BatchImportOptions{})
		},
		"roles": func(bm *BatchManager, tenantID string, empty bool) (*BatchImportResult, error) {
			items := []*RoleImport{{Name: "test"}}
			if empty {
				items = []*RoleImport{}
			}
			return bm.ImportRoles(context.Background(), tenantID, items, BatchImportOptions{})
		},
		"policies": func(bm *BatchManager, tenantID string, empty bool) (*BatchImportResult, error) {
			items := []*PolicyImport{{Name: "test"}}
			if empty {
				items = []*PolicyImport{}
			}
			return bm.ImportPolicies(context.Background(), tenantID, items, BatchImportOptions{})
		},
	}

	for name, importFn := range importers {
		t.Run(name+"/empty slice", func(t *testing.T) {
			result, err := importFn(NewBatchManager(BatchManagerOptions{}), "test-tenant", true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == nil {
				t.Fatal("expected result, got nil")
			}
			if result.TotalItems != 0 {
				t.Errorf("expected 0 total items, got %d", result.TotalItems)
			}
		})
		t.Run(name+"/missing tenant ID", func(t *testing.T) {
			_, err := importFn(NewBatchManager(BatchManagerOptions{}), "", false)
			var valErr *ValidationError
			if !errors.As(err, &valErr) || valErr.Field != "tenant_id" {
				t.Errorf("expected tenant_id ValidationError, got %v", err)
			}
		})
	}
}

// TestBatchManagerSingleTypeExport covers the checks each single-type export
// makes: a missing tenant ID is a validation error, and a missing backing
// store is a batch operation failure.
func TestBatchManagerSingleTypeExport(t *testing.T) {
	exporters := map[string]func(bm *BatchManager, tenantID string) error{
		"users": func(bm *BatchManager, tenantID string) error {
			_, err := bm.ExportUsers(context.Background(), tenantID, ExportFilter{})
			return err
		},
		"roles": func(bm *BatchManager, tenantID string) error {
			_, err := bm.ExportRoles(context.Background(), tenantID, ExportFilter{})
			return err
		},
		"policies": func(bm *BatchManager, tenantID string) error {
			_, err := bm.ExportPolicies(context.Background(), tenantID, ExportFilter{})
			return err
		},
	}

	for name, exportFn := range exporters {
		t.Run(name+"/missing tenant ID", func(t *testing.T) {
			err := exportFn(NewBatchManager(BatchManagerOptions{}), "")
			var valErr *ValidationError
			if !errors.As(err, &valErr) || valErr.Field != "tenant_id" {
				t.Errorf("expected tenant_id ValidationError, got %v", err)
			}
		})
		t.Run(name+"/no backing store", func(t *testing.T) {
			err := exportFn(NewBatchManager(BatchManagerOptions{}), "test-tenant")
			if !errors.Is(err, errBatchOperationFailed) {
				t.Errorf("expected errBatchOperationFailed, got %v", err)
			}
		})
	}
}

// TestBatchManagerImportItemNoBackend checks that importing a single valid
// item without its backing store fails as a batch operation failure.
func TestBatchManagerImportItemNoBackend(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		importFn func(bm *BatchManager) error
	}{
		{
			name: "user without identity store",
			importFn: func(bm *BatchManager) error {
				return bm.importUser(ctx, "test-tenant", &UserImport{
					Email:    "test@example.com",
					Username: "testuser",
				}, BatchImportOptions{})
			},
		},
		{
			name: "role without identity store",
			importFn: func(bm *BatchManager) error {
				return bm.importRole(ctx, "test-tenant", &RoleImport{Name: "test-role"}, BatchImportOptions{})
			},
		},
		{
			name: "policy without policy engine",
			importFn: func(bm *BatchManager) error {
				return bm.importPolicy(ctx, "test-tenant", &PolicyImport{
					Name: "test-policy",
					Rules: []Rule{{
						Name:      "test-rule",
						Effect:    EffectAllow,
						Actions:   []string{"*"},
						Resources: []string{"*"},
					}},
				}, BatchImportOptions{})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.importFn(NewBatchManager(BatchManagerOptions{}))
			if !errors.Is(err, errBatchOperationFailed) {
				t.Errorf("expected errBatchOperationFailed, got %v", err)
			}
		})
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

// TestBatchOptionsValidate tests BatchOptions validation.
func TestBatchOptionsValidate(t *testing.T) {
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
				if !errors.As(err, &vErr) || vErr.Field != tt.field {
					t.Errorf("Validate() error = %v, want ValidationError on field %s", err, tt.field)
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

// TestUserImportItemValidate tests UserImportItem validation.
func TestUserImportItemValidate(t *testing.T) {
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

// TestRoleImportItemValidate tests RoleImportItem validation.
func TestRoleImportItemValidate(t *testing.T) {
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
