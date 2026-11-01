package iam

import (
	"context"
	"errors"
	"testing"
	"time"
)

// setupTestBatchManager creates a BatchManager with populated test data.
func setupTestBatchManager(t *testing.T) (*BatchManager, *MemoryStorage) {
	t.Helper()

	storage := NewMemoryStorage()

	// Create identity store
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage: storage,
		RoleStorage: storage,
	})

	// Create policy engine
	policyEngine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
	})

	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: identityStore,
		PolicyEngine:  policyEngine,
	})

	return bm, storage
}

// populateTestData adds test data to the storage.
func populateTestData(t *testing.T, storage *MemoryStorage, tenantID string) {
	t.Helper()

	ctx := context.Background()
	now := time.Now()

	// Create tenant
	tenant := &Tenant{
		ID:      tenantID,
		Name:    "Test Tenant",
		Created: now,
		Updated: now,
	}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("failed to create tenant: %v", err)
	}

	// Create users
	for i := 0; i < 5; i++ {
		user := &User{
			ID:       generateTestUserID(i),
			Email:    generateTestEmail(i),
			Name:     generateTestName(i),
			TenantID: tenantID,
			Roles:    []string{},
			Created:  now.Add(-time.Duration(i) * time.Hour),
			Updated:  now.Add(-time.Duration(i) * time.Minute),
		}
		if err := storage.CreateUser(ctx, user); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}
	}

	// Create roles
	for i := 0; i < 3; i++ {
		role := &Role{
			ID:          generateTestRoleID(i),
			Name:        generateTestRoleName(i),
			Description: "Test role description",
			TenantID:    tenantID,
			Permissions: []string{"read", "write"},
			Created:     now.Add(-time.Duration(i) * time.Hour),
			Updated:     now.Add(-time.Duration(i) * time.Minute),
		}
		if err := storage.CreateRole(ctx, role); err != nil {
			t.Fatalf("failed to create role: %v", err)
		}
	}

	// Create policies
	for i := 0; i < 2; i++ {
		policy := &Policy{
			APIVersion: "iam/v1",
			Kind:       "Policy",
			Metadata: PolicyMetadata{
				Name:        generateTestPolicyName(i),
				Tenant:      tenantID,
				Description: "Test policy description",
				Priority:    i * 10,
				Created:     now.Add(-time.Duration(i) * time.Hour),
				Updated:     now.Add(-time.Duration(i) * time.Minute),
			},
			Spec: PolicySpec{
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
			},
		}
		if err := storage.CreatePolicy(ctx, tenantID, policy); err != nil {
			t.Fatalf("failed to create policy: %v", err)
		}
	}
}

// generateTestUserID renders the canonical qrdb user id for fixture i. A user
// id is the decimal rendering of a Snowflake, so the fixtures are numbered from
// 1 — zero is the store's invalid sentinel and names no user.
func generateTestUserID(i int) uint64 {
	return uint64(i) + 1
}

func generateTestEmail(i int) string {
	return "user" + string(rune('a'+i)) + "@example.com"
}

func generateTestName(i int) string {
	return "User " + string(rune('A'+i))
}

func generateTestGroupID(i int) string {
	return "group-" + string(rune('a'+i))
}

func generateTestGroupName(i int) string {
	return "Group " + string(rune('A'+i))
}

func generateTestRoleID(i int) string {
	return "role-" + string(rune('a'+i))
}

func generateTestRoleName(i int) string {
	return "Role " + string(rune('A'+i))
}

func generateTestPolicyName(i int) string {
	return "policy-" + string(rune('a'+i))
}

// TestExportAll tests the ExportAll function with various scenarios.
func TestExportAll(t *testing.T) {
	tenantID := "test-tenant"

	t.Run("export all resource types successfully", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		req := &BatchExportRequest{
			TenantID:        tenantID,
			IncludeUsers:    true,
			IncludeRoles:    true,
			IncludePolicies: true,
		}

		result, err := bm.ExportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result.TenantID != tenantID {
			t.Errorf("expected tenant ID %s, got %s", tenantID, result.TenantID)
		}
		if len(result.Users) != 5 {
			t.Errorf("expected 5 users, got %d", len(result.Users))
		}
		if len(result.Roles) != 3 {
			t.Errorf("expected 3 roles, got %d", len(result.Roles))
		}
		if len(result.Policies) != 2 {
			t.Errorf("expected 2 policies, got %d", len(result.Policies))
		}
		if result.Duration <= 0 {
			t.Error("expected positive duration")
		}
	})

	t.Run("export only users", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		req := &BatchExportRequest{
			TenantID:     tenantID,
			IncludeUsers: true,
		}

		result, err := bm.ExportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Users) != 5 {
			t.Errorf("expected 5 users, got %d", len(result.Users))
		}
		if len(result.Roles) != 0 {
			t.Errorf("expected 0 roles, got %d", len(result.Roles))
		}
		if len(result.Policies) != 0 {
			t.Errorf("expected 0 policies, got %d", len(result.Policies))
		}
	})

	t.Run("export only roles", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		req := &BatchExportRequest{
			TenantID:     tenantID,
			IncludeRoles: true,
		}

		result, err := bm.ExportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Roles) != 3 {
			t.Errorf("expected 3 roles, got %d", len(result.Roles))
		}
	})

	t.Run("export only policies", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		req := &BatchExportRequest{
			TenantID:        tenantID,
			IncludePolicies: true,
		}

		result, err := bm.ExportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Policies) != 2 {
			t.Errorf("expected 2 policies, got %d", len(result.Policies))
		}
	})

	t.Run("export with empty tenant", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		emptyTenant := "empty-tenant"

		// Create empty tenant
		ctx := context.Background()
		tenant := &Tenant{
			ID:      emptyTenant,
			Name:    "Empty Tenant",
			Created: time.Now(),
			Updated: time.Now(),
		}
		if err := storage.CreateTenant(ctx, tenant); err != nil {
			t.Fatalf("failed to create tenant: %v", err)
		}

		req := &BatchExportRequest{
			TenantID:        emptyTenant,
			IncludeUsers:    true,
			IncludeRoles:    true,
			IncludePolicies: true,
		}

		result, err := bm.ExportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Users) != 0 {
			t.Errorf("expected 0 users, got %d", len(result.Users))
		}
	})

	t.Run("validation error - missing tenant ID", func(t *testing.T) {
		bm, _ := setupTestBatchManager(t)

		ctx := context.Background()
		req := &BatchExportRequest{
			IncludeUsers: true,
		}

		_, err := bm.ExportAll(ctx, req)
		if err == nil {
			t.Error("expected validation error")
		}
	})

	t.Run("validation error - no resource types selected", func(t *testing.T) {
		bm, _ := setupTestBatchManager(t)

		ctx := context.Background()
		req := &BatchExportRequest{
			TenantID: tenantID,
		}

		_, err := bm.ExportAll(ctx, req)
		if err == nil {
			t.Error("expected validation error")
		}
	})

	t.Run("context cancellation during export", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		req := &BatchExportRequest{
			TenantID:        tenantID,
			IncludeUsers:    true,
			IncludeRoles:    true,
			IncludePolicies: true,
		}

		_, err := bm.ExportAll(ctx, req)
		if err == nil {
			t.Error("expected cancellation error")
		}
	})

	t.Run("export with filter applied", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		req := &BatchExportRequest{
			TenantID:        tenantID,
			IncludeUsers:    true,
			IncludeRoles:    true,
			IncludePolicies: true,
			Filter: ExportFilter{
				UserIDs:     []uint64{1},
				RoleIDs:     []string{"role-a"},
				PolicyNames: []string{"policy-a"},
			},
		}

		result, err := bm.ExportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
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
	})
}

// TestExportUsersWithFilters tests ExportUsers with various filter combinations.
func TestExportUsersWithFilters(t *testing.T) {
	tenantID := "test-tenant"

	t.Run("export all users without filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		users, err := bm.ExportUsers(ctx, tenantID, ExportFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(users) != 5 {
			t.Errorf("expected 5 users, got %d", len(users))
		}
	})

	t.Run("export users with ID filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		filter := ExportFilter{
			UserIDs: []uint64{1, 3},
		}

		users, err := bm.ExportUsers(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(users) != 2 {
			t.Errorf("expected 2 users, got %d", len(users))
		}
	})

	t.Run("export users with time filter - created after", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		oneHourAgo := time.Now().Add(-1 * time.Hour)
		filter := ExportFilter{
			CreatedAfter: &oneHourAgo,
		}

		users, err := bm.ExportUsers(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Only users created within the last hour should be returned
		if len(users) == 0 {
			t.Error("expected at least 1 user created within the last hour")
		}
	})

	t.Run("export users with time filter - created before", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		twoHoursAgo := time.Now().Add(-2 * time.Hour)
		filter := ExportFilter{
			CreatedBefore: &twoHoursAgo,
		}

		users, err := bm.ExportUsers(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Only users created before 2 hours ago should be returned
		for _, user := range users {
			if user.Created.After(twoHoursAgo) {
				t.Errorf("user %d was created after the filter time", user.ID)
			}
		}
	})

	t.Run("export users with time filter - updated after", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		twoMinutesAgo := time.Now().Add(-2 * time.Minute)
		filter := ExportFilter{
			UpdatedAfter: &twoMinutesAgo,
		}

		users, err := bm.ExportUsers(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Only recently updated users should be returned
		for _, user := range users {
			if user.Updated.Before(twoMinutesAgo) {
				t.Errorf("user %d was updated before the filter time", user.ID)
			}
		}
	})

	t.Run("export users with time filter - updated before", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		oneMinuteAgo := time.Now().Add(-1 * time.Minute)
		filter := ExportFilter{
			UpdatedBefore: &oneMinuteAgo,
		}

		users, err := bm.ExportUsers(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Only users updated before one minute ago should be returned
		for _, user := range users {
			if user.Updated.After(oneMinuteAgo) {
				t.Errorf("user %d was updated after the filter time", user.ID)
			}
		}
	})

	t.Run("export users with non-matching filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		filter := ExportFilter{
			UserIDs: []uint64{99999},
		}

		users, err := bm.ExportUsers(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(users) != 0 {
			t.Errorf("expected 0 users, got %d", len(users))
		}
	})
}

// TestExportRolesWithFilters tests ExportRoles with various filter combinations.
func TestExportRolesWithFilters(t *testing.T) {
	tenantID := "test-tenant"

	t.Run("export all roles without filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		roles, err := bm.ExportRoles(ctx, tenantID, ExportFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(roles) != 3 {
			t.Errorf("expected 3 roles, got %d", len(roles))
		}
	})

	t.Run("export roles with ID filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		filter := ExportFilter{
			RoleIDs: []string{"role-a", "role-b"},
		}

		roles, err := bm.ExportRoles(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(roles) != 2 {
			t.Errorf("expected 2 roles, got %d", len(roles))
		}
	})

}

// TestExportPoliciesWithFilters tests ExportPolicies with various filter combinations.
func TestExportPoliciesWithFilters(t *testing.T) {
	tenantID := "test-tenant"

	t.Run("export all policies without filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		policies, err := bm.ExportPolicies(ctx, tenantID, ExportFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(policies) != 2 {
			t.Errorf("expected 2 policies, got %d", len(policies))
		}
	})

	t.Run("export policies with name filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		filter := ExportFilter{
			PolicyNames: []string{"policy-a"},
		}

		policies, err := bm.ExportPolicies(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(policies) != 1 {
			t.Errorf("expected 1 policy, got %d", len(policies))
		}
	})

	t.Run("export policies with non-matching name filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		filter := ExportFilter{
			PolicyNames: []string{"nonexistent-policy"},
		}

		policies, err := bm.ExportPolicies(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(policies) != 0 {
			t.Errorf("expected 0 policies, got %d", len(policies))
		}
	})

	t.Run("export policies with created before filter that excludes all", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		longTimeAgo := time.Now().Add(-1000 * time.Hour)
		filter := ExportFilter{
			CreatedBefore: &longTimeAgo,
		}

		policies, err := bm.ExportPolicies(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(policies) != 0 {
			t.Errorf("expected 0 policies, got %d", len(policies))
		}
	})

	t.Run("export policies with updated before filter that excludes all", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		longTimeAgo := time.Now().Add(-1000 * time.Hour)
		filter := ExportFilter{
			UpdatedBefore: &longTimeAgo,
		}

		policies, err := bm.ExportPolicies(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(policies) != 0 {
			t.Errorf("expected 0 policies, got %d", len(policies))
		}
	})
}

// TestFilterMatchFunctions tests the individual filter matching functions.
func TestFilterMatchFunctions(t *testing.T) {
	bm := NewBatchManager(BatchManagerOptions{})
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)

	t.Run("userMatchesFilter with combined filters", func(t *testing.T) {
		user := &User{
			ID:      10001,
			Created: now,
			Updated: now,
		}

		// Should match - ID in list and within time range
		filter := ExportFilter{
			UserIDs:       []uint64{10001, 10002},
			CreatedAfter:  &yesterday,
			CreatedBefore: &tomorrow,
			UpdatedAfter:  &yesterday,
			UpdatedBefore: &tomorrow,
		}
		if !bm.userMatchesFilter(user, filter) {
			t.Error("user should match combined filter")
		}

		// Should not match - ID not in list
		filter = ExportFilter{
			UserIDs: []uint64{10002, 10003},
		}
		if bm.userMatchesFilter(user, filter) {
			t.Error("user should not match filter with wrong ID")
		}
	})

	t.Run("roleMatchesFilter with all filter types", func(t *testing.T) {
		role := &Role{
			ID:      "role-1",
			Created: now,
			Updated: now,
		}

		// Test all filter combinations
		tests := []struct {
			name     string
			filter   ExportFilter
			expected bool
		}{
			{
				name:     "empty filter matches",
				filter:   ExportFilter{},
				expected: true,
			},
			{
				name:     "matching ID filter",
				filter:   ExportFilter{RoleIDs: []string{"role-1"}},
				expected: true,
			},
			{
				name:     "non-matching ID filter",
				filter:   ExportFilter{RoleIDs: []string{"role-2"}},
				expected: false,
			},
			{
				name:     "created after yesterday matches",
				filter:   ExportFilter{CreatedAfter: &yesterday},
				expected: true,
			},
			{
				name:     "created before tomorrow matches",
				filter:   ExportFilter{CreatedBefore: &tomorrow},
				expected: true,
			},
			{
				name:     "created after tomorrow does not match",
				filter:   ExportFilter{CreatedAfter: &tomorrow},
				expected: false,
			},
			{
				name:     "created before yesterday does not match",
				filter:   ExportFilter{CreatedBefore: &yesterday},
				expected: false,
			},
			{
				name:     "updated after yesterday matches",
				filter:   ExportFilter{UpdatedAfter: &yesterday},
				expected: true,
			},
			{
				name:     "updated before tomorrow matches",
				filter:   ExportFilter{UpdatedBefore: &tomorrow},
				expected: true,
			},
			{
				name:     "updated after tomorrow does not match",
				filter:   ExportFilter{UpdatedAfter: &tomorrow},
				expected: false,
			},
			{
				name:     "updated before yesterday does not match",
				filter:   ExportFilter{UpdatedBefore: &yesterday},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := bm.roleMatchesFilter(role, tt.filter)
				if result != tt.expected {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			})
		}
	})

	t.Run("policyMatchesFilter with all time filters", func(t *testing.T) {
		policy := &Policy{
			Metadata: PolicyMetadata{
				Name:    "test-policy",
				Created: now,
				Updated: now,
			},
		}

		// Test all time filter combinations
		tests := []struct {
			name     string
			filter   ExportFilter
			expected bool
		}{
			{
				name:     "empty filter matches",
				filter:   ExportFilter{},
				expected: true,
			},
			{
				name:     "created after yesterday matches",
				filter:   ExportFilter{CreatedAfter: &yesterday},
				expected: true,
			},
			{
				name:     "created before tomorrow matches",
				filter:   ExportFilter{CreatedBefore: &tomorrow},
				expected: true,
			},
			{
				name:     "created after tomorrow does not match",
				filter:   ExportFilter{CreatedAfter: &tomorrow},
				expected: false,
			},
			{
				name:     "created before yesterday does not match",
				filter:   ExportFilter{CreatedBefore: &yesterday},
				expected: false,
			},
			{
				name:     "updated after yesterday matches",
				filter:   ExportFilter{UpdatedAfter: &yesterday},
				expected: true,
			},
			{
				name:     "updated before tomorrow matches",
				filter:   ExportFilter{UpdatedBefore: &tomorrow},
				expected: true,
			},
			{
				name:     "updated after tomorrow does not match",
				filter:   ExportFilter{UpdatedAfter: &tomorrow},
				expected: false,
			},
			{
				name:     "updated before yesterday does not match",
				filter:   ExportFilter{UpdatedBefore: &yesterday},
				expected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := bm.policyMatchesFilter(policy, tt.filter)
				if result != tt.expected {
					t.Errorf("expected %v, got %v", tt.expected, result)
				}
			})
		}
	})
}

// TestConvertToImportRequest tests conversion of export results to import requests.
func TestConvertToImportRequest(t *testing.T) {
	now := time.Now()

	t.Run("convert full export result", func(t *testing.T) {
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
			Users: []*User{
				{
					ID:         30001,
					Email:      "user1@example.com",
					Name:       "User One",
					TenantID:   "source-tenant",
					Lifecycle:  LifecycleActive,
					Roles:      []string{"r1"},
					Attributes: map[string]string{"dept": "engineering"},
					Created:    now,
					Updated:    now,
				},
				{
					ID:        30002,
					Email:     "user2@example.com",
					Name:      "User Two",
					TenantID:  "source-tenant",
					Created:   now,
					Updated:   now,
					Lifecycle: LifecycleSuspended,
				},
			},
			Roles: []*Role{
				{
					ID:          "r1",
					Name:        "Role One",
					Description: "First role",
					TenantID:    "source-tenant",
					Permissions: []string{"read", "write"},
					Created:     now,
					Updated:     now,
				},
			},
			Policies: []*Policy{
				{
					APIVersion: "iam/v1",
					Kind:       "Policy",
					Metadata: PolicyMetadata{
						Name:        "policy-1",
						Tenant:      "source-tenant",
						Description: "First policy",
						Priority:    10,
						Created:     now,
						Updated:     now,
					},
					Spec: PolicySpec{
						Subjects: SubjectSelector{
							Roles: []string{"admin"},
						},
						Rules: []Rule{
							{
								Name:      "allow-read",
								Effect:    EffectAllow,
								Actions:   []string{"read"},
								Resources: []string{"*"},
							},
						},
						Variables: map[string]string{
							"env": "prod",
						},
					},
				},
			},
			Duration: time.Second,
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		// Verify tenant ID
		if importReq.TenantID != "target-tenant" {
			t.Errorf("expected target-tenant, got %s", importReq.TenantID)
		}

		// Verify users
		if len(importReq.Users) != 2 {
			t.Errorf("expected 2 users, got %d", len(importReq.Users))
		}
		if importReq.Users[0].ID != 30001 {
			t.Errorf("expected user ID 30001, got %d", importReq.Users[0].ID)
		}
		if importReq.Users[0].Email != "user1@example.com" {
			t.Errorf("expected email user1@example.com, got %s", importReq.Users[0].Email)
		}
		assertEnabledStated(t, importReq.Users[0].Enabled, true, "first user")
		assertEnabledStated(t, importReq.Users[1].Enabled, false, "second user (suspended)")

		// Verify roles
		if len(importReq.Roles) != 1 {
			t.Errorf("expected 1 role, got %d", len(importReq.Roles))
		}
		if len(importReq.Roles[0].Permissions) != 2 {
			t.Errorf("expected 2 permissions, got %d", len(importReq.Roles[0].Permissions))
		}

		// Verify policies
		if len(importReq.Policies) != 1 {
			t.Errorf("expected 1 policy, got %d", len(importReq.Policies))
		}
		if importReq.Policies[0].Name != "policy-1" {
			t.Errorf("expected policy name 'policy-1', got '%s'", importReq.Policies[0].Name)
		}
		if importReq.Policies[0].Priority != 10 {
			t.Errorf("expected priority 10, got %d", importReq.Policies[0].Priority)
		}
	})

	t.Run("convert empty export result", func(t *testing.T) {
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		if importReq.TenantID != "target-tenant" {
			t.Errorf("expected target-tenant, got %s", importReq.TenantID)
		}
		if importReq.Users != nil {
			t.Error("expected nil users")
		}
		if importReq.Roles != nil {
			t.Error("expected nil roles")
		}
		if importReq.Policies != nil {
			t.Error("expected nil policies")
		}
	})

	t.Run("convert with locked user", func(t *testing.T) {
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
			Users: []*User{
				{
					ID:        30001,
					Email:     "locked@example.com",
					Name:      "Locked User",
					TenantID:  "source-tenant",
					Lifecycle: LifecycleLocked,
					Created:   now,
					Updated:   now,
				},
			},
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		assertEnabledStated(t, importReq.Users[0].Enabled, false, "locked user")
	})

	t.Run("convert preserves user attributes", func(t *testing.T) {
		attrs := map[string]string{
			"department": "engineering",
			"level":      "3",
		}
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
			Users: []*User{
				{
					ID:         30001,
					Email:      "user@example.com",
					Name:       "User",
					TenantID:   "source-tenant",
					Attributes: attrs,
					Created:    now,
					Updated:    now,
				},
			},
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		if importReq.Users[0].Attributes == nil {
			t.Error("expected attributes to be preserved")
		}
		if importReq.Users[0].Attributes["department"] != "engineering" {
			t.Error("expected department attribute to be preserved")
		}
	})
}

// TestExportWithContextCancellation tests context cancellation during various export operations.
func TestExportWithContextCancellation(t *testing.T) {
	tenantID := "test-tenant"

	tests := []struct {
		name   string
		export func(bm *BatchManager, ctx context.Context) error
	}{
		{
			name: "users",
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportUsers(ctx, tenantID, ExportFilter{})
				return err
			},
		},
		{
			name: "roles",
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportRoles(ctx, tenantID, ExportFilter{})
				return err
			},
		},
		{
			name: "policies",
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportPolicies(ctx, tenantID, ExportFilter{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run("cancel during "+tt.name+" export", func(t *testing.T) {
			bm, storage := setupTestBatchManager(t)
			populateTestData(t, storage, tenantID)

			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			if err := tt.export(bm, ctx); err == nil {
				t.Error("expected cancellation error")
			}
		})
	}
}

// TestExportErrorCases tests various error scenarios.
func TestExportErrorCases(t *testing.T) {
	unconfigured := func(t *testing.T) *BatchManager {
		return NewBatchManager(BatchManagerOptions{})
	}
	configured := func(t *testing.T) *BatchManager {
		bm, _ := setupTestBatchManager(t)
		return bm
	}

	tests := []struct {
		name    string
		manager func(t *testing.T) *BatchManager
		export  func(bm *BatchManager, ctx context.Context) error
	}{
		{
			name:    "export users with nil identity store",
			manager: unconfigured,
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportUsers(ctx, "tenant", ExportFilter{})
				return err
			},
		},
		{
			name:    "export roles with nil identity store",
			manager: unconfigured,
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportRoles(ctx, "tenant", ExportFilter{})
				return err
			},
		},
		{
			name:    "export policies with nil policy engine",
			manager: unconfigured,
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportPolicies(ctx, "tenant", ExportFilter{})
				return err
			},
		},
		{
			name:    "export users with empty tenant ID",
			manager: configured,
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportUsers(ctx, "", ExportFilter{})
				return err
			},
		},
		{
			name:    "export roles with empty tenant ID",
			manager: configured,
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportRoles(ctx, "", ExportFilter{})
				return err
			},
		},
		{
			name:    "export policies with empty tenant ID",
			manager: configured,
			export: func(bm *BatchManager, ctx context.Context) error {
				_, err := bm.ExportPolicies(ctx, "", ExportFilter{})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.export(tt.manager(t), context.Background()); err == nil {
				t.Errorf("expected error: %s", tt.name)
			}
		})
	}
}

// =============================================================================
// BatchManager - ImportAll more coverage
// =============================================================================

func TestBatchManager_ImportAll_ValidationError(t *testing.T) {
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

	// Import with empty tenant ID should fail
	_, err := bm.ImportAll(ctx, &BatchImportRequest{
		TenantID: "", // Missing tenant ID
		Users: []*UserImport{
			{ID: 10001, Email: "user1@test.com", Name: "User One"},
		},
	})
	if err == nil {
		t.Error("expected validation error for empty tenant ID")
	}
}

func TestBatchManager_ImportAll_NoItems(t *testing.T) {
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
	tenantID := "empty-import-tenant"

	tenant := &Tenant{ID: tenantID, Name: "Empty Import Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Import with no data should fail validation
	_, err := bm.ImportAll(ctx, &BatchImportRequest{
		TenantID: tenantID,
		// No users, groups, roles, or policies
	})
	if err == nil {
		t.Error("expected error for empty batch import")
	}
}

// =============================================================================
// AdminManager - CreateAssignment error paths
// =============================================================================

// newErrorPathAdminManager builds an AdminManager over in-memory storage
// for the validation tests below.
func newErrorPathAdminManager(t *testing.T) *AdminManager {
	t.Helper()
	auditLogger, err := NewAuditLogger(WithAuditStorage(NewMemoryAuditStorage()))
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}

	mgr, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         NewMemoryAdminStorage(),
		ImpersonationStorage: NewMemoryImpersonationStorage(),
		AuditLogger:          auditLogger,
	})
	if err != nil {
		t.Fatalf("NewAdminManager failed: %v", err)
	}
	return mgr
}

func TestAdminManager_CreateAssignment_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		opts CreateAdminAssignmentOptions
	}{
		{
			name: "invalid role",
			opts: CreateAdminAssignmentOptions{UserID: 1, AdminRole: AdminRole("invalid-role"), GrantedBy: "admin-1"},
		},
		{
			name: "missing user ID",
			opts: CreateAdminAssignmentOptions{UserID: 0, AdminRole: AdminRoleTenantAdmin, GrantedBy: "admin-1"},
		},
		{
			name: "missing granted by",
			opts: CreateAdminAssignmentOptions{UserID: 1, AdminRole: AdminRoleTenantAdmin, GrantedBy: ""},
		},
	}

	mgr := newErrorPathAdminManager(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := mgr.CreateAssignment(context.Background(), tt.opts); err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}
}

// =============================================================================
// AdminManager - StartImpersonation error paths
// =============================================================================

// TestAdminManager_StartImpersonation_ValidationErrors covers the request
// checks. The actor is no longer a request field but the authenticated
// principal in context (go-quicbaas#383), so a caller with no subject in ctx
// is refused with ErrImpersonationActorRequired: it fails closed.
func TestAdminManager_StartImpersonation_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		opts    StartImpersonationOptions
		wantErr error
	}{
		{
			name:    "no authenticated caller",
			ctx:     context.Background(),
			opts:    StartImpersonationOptions{TargetUserID: 40021, TenantID: "tenant-1", Reason: "Testing"},
			wantErr: ErrImpersonationActorRequired,
		},
		{
			name: "missing target user ID",
			ctx:  ctxWithActor(30021),
			opts: StartImpersonationOptions{TargetUserID: 0, TenantID: "tenant-1", Reason: "Testing"},
		},
		{
			name: "missing reason",
			ctx:  ctxWithActor(30021),
			opts: StartImpersonationOptions{TargetUserID: 40021, TenantID: "tenant-1", Reason: ""},
		},
	}

	mgr := newErrorPathAdminManager(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.StartImpersonation(tt.ctx, &tt.opts)
			if err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

// =============================================================================
// BatchProcessor - more rate limit and error paths
// =============================================================================

func TestBatchProcessor_ProcessPolicies_WithRateLimit(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "policies-tenant"

	items := []PolicyImportItem{
		{
			Name: "policy-1",
			Policy: &Policy{
				APIVersion: "v1",
				Kind:       "Policy",
				Metadata:   PolicyMetadata{Name: "policy-1", Tenant: tenantID},
				Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
			},
		},
	}

	// Verify we can create an operation with rate limit option
	op, err := bp.ImportPolicies(ctx, tenantID, items, BatchOptions{
		RateLimit: 5,
	})
	if err != nil {
		t.Fatalf("ImportPolicies failed: %v", err)
	}

	// Verify operation was created with correct attributes
	if op.ID == "" {
		t.Error("expected operation to have ID")
	}
	if op.TenantID != tenantID {
		t.Errorf("expected tenantID %s, got %s", tenantID, op.TenantID)
	}
	if op.TotalItems != 1 {
		t.Errorf("expected 1 total item, got %d", op.TotalItems)
	}
}

// =============================================================================
// ImportRoles - more coverage
// =============================================================================

func TestBatchManager_ImportRoles_WithPermissions(t *testing.T) {
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
	tenantID := "role-import-tenant"

	tenant := &Tenant{ID: tenantID, Name: "Role Import Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	result, err := bm.ImportRoles(ctx, tenantID, []*RoleImport{
		{
			ID:          "role-1",
			Name:        "Admin Role",
			Description: "Administrative role with full permissions",
			Permissions: []string{"read", "write", "delete"},
		},
	}, BatchImportOptions{})
	if err != nil {
		t.Fatalf("ImportRoles failed: %v", err)
	}
	if result.SuccessCount != 1 {
		t.Errorf("expected 1 success, got %d", result.SuccessCount)
	}
}

// =============================================================================
// IncrementQuotaUsage - Apps resource (renamed to avoid duplicate)
// =============================================================================

func TestMemoryStorage_IncrementQuotaUsage_AppsLimit(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{ID: "apps-quota-tenant", Name: "Apps Quota Tenant", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{MaxApps: 2, CurrentApps: 2}
	if err := storage.UpdateQuota(ctx, "apps-quota-tenant", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	err := storage.IncrementQuotaUsage(ctx, "apps-quota-tenant", "apps", 1)
	if err == nil {
		t.Error("expected error when exceeding apps quota")
	}
}

func TestMemoryStorage_DecrementQuotaUsage_AppsToFloor(t *testing.T) {
	storage := NewMemoryStorage()
	ctx := context.Background()

	tenant := &Tenant{ID: "dec-apps-floor", Name: "Dec Apps Floor", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	quota := &TenantQuota{CurrentApps: 1}
	if err := storage.UpdateQuota(ctx, "dec-apps-floor", quota); err != nil {
		t.Fatalf("UpdateQuota failed: %v", err)
	}

	if err := storage.DecrementQuotaUsage(ctx, "dec-apps-floor", "apps", 10); err != nil {
		t.Fatalf("DecrementQuotaUsage failed: %v", err)
	}

	updatedQuota, _ := storage.GetQuota(ctx, "dec-apps-floor")
	if updatedQuota.CurrentApps != 0 {
		t.Errorf("expected CurrentApps=0, got %d", updatedQuota.CurrentApps)
	}
}
