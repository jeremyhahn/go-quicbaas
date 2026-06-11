package iam

import (
	"context"
	"testing"
	"time"
)

// setupTestBatchManager creates a BatchManager with populated test data.
func setupTestBatchManager(t *testing.T) (*BatchManager, *MemoryStorage) {
	t.Helper()

	storage := NewMemoryStorage()

	// Create identity store
	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
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
			Groups:   []string{},
			Roles:    []string{},
			Created:  now.Add(-time.Duration(i) * time.Hour),
			Updated:  now.Add(-time.Duration(i) * time.Minute),
		}
		if err := storage.CreateUser(ctx, user); err != nil {
			t.Fatalf("failed to create user: %v", err)
		}
	}

	// Create groups
	for i := 0; i < 3; i++ {
		group := &Group{
			ID:          generateTestGroupID(i),
			Name:        generateTestGroupName(i),
			Description: "Test group description",
			TenantID:    tenantID,
			Members:     []string{},
			Created:     now.Add(-time.Duration(i) * time.Hour),
			Updated:     now.Add(-time.Duration(i) * time.Minute),
		}
		if err := storage.CreateGroup(ctx, group); err != nil {
			t.Fatalf("failed to create group: %v", err)
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

func generateTestUserID(i int) string {
	return "user-" + string(rune('a'+i))
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
			IncludeGroups:   true,
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
		if len(result.Groups) != 3 {
			t.Errorf("expected 3 groups, got %d", len(result.Groups))
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
		if len(result.Groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(result.Groups))
		}
		if len(result.Roles) != 0 {
			t.Errorf("expected 0 roles, got %d", len(result.Roles))
		}
		if len(result.Policies) != 0 {
			t.Errorf("expected 0 policies, got %d", len(result.Policies))
		}
	})

	t.Run("export only groups", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		req := &BatchExportRequest{
			TenantID:      tenantID,
			IncludeGroups: true,
		}

		result, err := bm.ExportAll(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Users) != 0 {
			t.Errorf("expected 0 users, got %d", len(result.Users))
		}
		if len(result.Groups) != 3 {
			t.Errorf("expected 3 groups, got %d", len(result.Groups))
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
			IncludeGroups:   true,
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
		if len(result.Groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(result.Groups))
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
			IncludeGroups:   true,
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
			IncludeGroups:   true,
			IncludeRoles:    true,
			IncludePolicies: true,
			Filter: ExportFilter{
				UserIDs:     []string{"user-a"},
				GroupIDs:    []string{"group-a"},
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
		if len(result.Groups) != 1 {
			t.Errorf("expected 1 group, got %d", len(result.Groups))
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
			UserIDs: []string{"user-a", "user-c"},
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
				t.Errorf("user %s was created after the filter time", user.ID)
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
				t.Errorf("user %s was updated before the filter time", user.ID)
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
				t.Errorf("user %s was updated after the filter time", user.ID)
			}
		}
	})

	t.Run("export users with non-matching filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		filter := ExportFilter{
			UserIDs: []string{"nonexistent-user"},
		}

		users, err := bm.ExportUsers(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(users) != 0 {
			t.Errorf("expected 0 users, got %d", len(users))
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := bm.ExportUsers(ctx, tenantID, ExportFilter{})
		if err == nil {
			t.Error("expected cancellation error")
		}
	})
}

// TestExportGroupsWithFilters tests ExportGroups with various filter combinations.
func TestExportGroupsWithFilters(t *testing.T) {
	tenantID := "test-tenant"

	t.Run("export all groups without filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		groups, err := bm.ExportGroups(ctx, tenantID, ExportFilter{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(groups) != 3 {
			t.Errorf("expected 3 groups, got %d", len(groups))
		}
	})

	t.Run("export groups with ID filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		filter := ExportFilter{
			GroupIDs: []string{"group-a"},
		}

		groups, err := bm.ExportGroups(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(groups) != 1 {
			t.Errorf("expected 1 group, got %d", len(groups))
		}
	})

	t.Run("export groups with time filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		oneHourAgo := time.Now().Add(-1 * time.Hour)
		tomorrow := time.Now().Add(24 * time.Hour)
		filter := ExportFilter{
			CreatedAfter:  &oneHourAgo,
			CreatedBefore: &tomorrow,
		}

		groups, err := bm.ExportGroups(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(groups) == 0 {
			t.Error("expected at least 1 group matching time filter")
		}
	})

	t.Run("export groups with updated time filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		twoMinutesAgo := time.Now().Add(-2 * time.Minute)
		tomorrow := time.Now().Add(24 * time.Hour)
		filter := ExportFilter{
			UpdatedAfter:  &twoMinutesAgo,
			UpdatedBefore: &tomorrow,
		}

		groups, err := bm.ExportGroups(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should include at least some groups
		if len(groups) == 0 {
			t.Error("expected at least 1 group matching updated time filter")
		}
	})

	t.Run("export groups with non-matching ID filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		filter := ExportFilter{
			GroupIDs: []string{"nonexistent-group"},
		}

		groups, err := bm.ExportGroups(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(groups))
		}
	})

	t.Run("export groups with created before filter that excludes all", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		longTimeAgo := time.Now().Add(-1000 * time.Hour)
		filter := ExportFilter{
			CreatedBefore: &longTimeAgo,
		}

		groups, err := bm.ExportGroups(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(groups))
		}
	})

	t.Run("export groups with updated before filter that excludes all", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		longTimeAgo := time.Now().Add(-1000 * time.Hour)
		filter := ExportFilter{
			UpdatedBefore: &longTimeAgo,
		}

		groups, err := bm.ExportGroups(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(groups))
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

	t.Run("export roles with time filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		oneHourAgo := time.Now().Add(-1 * time.Hour)
		filter := ExportFilter{
			CreatedAfter: &oneHourAgo,
		}

		roles, err := bm.ExportRoles(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(roles) == 0 {
			t.Error("expected at least 1 role matching time filter")
		}
	})

	t.Run("export roles with updated before filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		twoMinutesAgo := time.Now().Add(-2 * time.Minute)
		filter := ExportFilter{
			UpdatedBefore: &twoMinutesAgo,
		}

		roles, err := bm.ExportRoles(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify all returned roles were updated before the filter time
		for _, role := range roles {
			if role.Updated.After(twoMinutesAgo) {
				t.Errorf("role %s was updated after the filter time", role.ID)
			}
		}
	})

	t.Run("export roles with updated after filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		oneMinuteAgo := time.Now().Add(-1 * time.Minute)
		filter := ExportFilter{
			UpdatedAfter: &oneMinuteAgo,
		}

		roles, err := bm.ExportRoles(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify all returned roles were updated after the filter time
		for _, role := range roles {
			if role.Updated.Before(oneMinuteAgo) {
				t.Errorf("role %s was updated before the filter time", role.ID)
			}
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

	t.Run("export policies with time filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		oneHourAgo := time.Now().Add(-1 * time.Hour)
		tomorrow := time.Now().Add(24 * time.Hour)
		filter := ExportFilter{
			CreatedAfter:  &oneHourAgo,
			CreatedBefore: &tomorrow,
		}

		policies, err := bm.ExportPolicies(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(policies) == 0 {
			t.Error("expected at least 1 policy matching time filter")
		}
	})

	t.Run("export policies with updated time filter", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx := context.Background()
		twoMinutesAgo := time.Now().Add(-2 * time.Minute)
		tomorrow := time.Now().Add(24 * time.Hour)
		filter := ExportFilter{
			UpdatedAfter:  &twoMinutesAgo,
			UpdatedBefore: &tomorrow,
		}

		policies, err := bm.ExportPolicies(ctx, tenantID, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(policies) == 0 {
			t.Error("expected at least 1 policy matching updated time filter")
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
			ID:      "user-1",
			Created: now,
			Updated: now,
		}

		// Should match - ID in list and within time range
		filter := ExportFilter{
			UserIDs:       []string{"user-1", "user-2"},
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
			UserIDs: []string{"user-2", "user-3"},
		}
		if bm.userMatchesFilter(user, filter) {
			t.Error("user should not match filter with wrong ID")
		}
	})

	t.Run("groupMatchesFilter with combined filters", func(t *testing.T) {
		group := &Group{
			ID:      "group-1",
			Created: now,
			Updated: now,
		}

		// Should match - ID in list
		filter := ExportFilter{
			GroupIDs:      []string{"group-1"},
			CreatedAfter:  &yesterday,
			UpdatedBefore: &tomorrow,
		}
		if !bm.groupMatchesFilter(group, filter) {
			t.Error("group should match combined filter")
		}

		// Should not match - created before filter
		createdAfterTomorrow := tomorrow
		filter = ExportFilter{
			CreatedAfter: &createdAfterTomorrow,
		}
		if bm.groupMatchesFilter(group, filter) {
			t.Error("group should not match filter - created before filter time")
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

	t.Run("policyMatchesFilter with name filter", func(t *testing.T) {
		policy := &Policy{
			Metadata: PolicyMetadata{
				Name:    "test-policy",
				Created: now,
				Updated: now,
			},
		}

		// Should match - name in list
		filter := ExportFilter{
			PolicyNames: []string{"test-policy", "other-policy"},
		}
		if !bm.policyMatchesFilter(policy, filter) {
			t.Error("policy should match name filter")
		}

		// Should not match - name not in list
		filter = ExportFilter{
			PolicyNames: []string{"other-policy"},
		}
		if bm.policyMatchesFilter(policy, filter) {
			t.Error("policy should not match filter with wrong name")
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

	t.Run("groupMatchesFilter with all filter types", func(t *testing.T) {
		group := &Group{
			ID:      "group-1",
			Created: now,
			Updated: now,
		}

		// Test additional group filter combinations
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
				filter:   ExportFilter{GroupIDs: []string{"group-1"}},
				expected: true,
			},
			{
				name:     "non-matching ID filter",
				filter:   ExportFilter{GroupIDs: []string{"group-2"}},
				expected: false,
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
				result := bm.groupMatchesFilter(group, tt.filter)
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
					ID:         "u1",
					Email:      "user1@example.com",
					Name:       "User One",
					TenantID:   "source-tenant",
					Groups:     []string{"g1"},
					Roles:      []string{"r1"},
					Attributes: map[string]interface{}{"dept": "engineering"},
					Created:    now,
					Updated:    now,
					Suspended:  false,
					Locked:     false,
				},
				{
					ID:        "u2",
					Email:     "user2@example.com",
					Name:      "User Two",
					TenantID:  "source-tenant",
					Created:   now,
					Updated:   now,
					Suspended: true, // Suspended user
				},
			},
			Groups: []*Group{
				{
					ID:          "g1",
					Name:        "Group One",
					Description: "First group",
					TenantID:    "source-tenant",
					Members:     []string{"u1"},
					Created:     now,
					Updated:     now,
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
		if importReq.Users[0].ID != "u1" {
			t.Errorf("expected user ID u1, got %s", importReq.Users[0].ID)
		}
		if importReq.Users[0].Email != "user1@example.com" {
			t.Errorf("expected email user1@example.com, got %s", importReq.Users[0].Email)
		}
		if !importReq.Users[0].Enabled {
			t.Error("expected first user to be enabled")
		}
		if importReq.Users[1].Enabled {
			t.Error("expected second user (suspended) to be disabled")
		}
		if len(importReq.Users[0].Groups) != 1 {
			t.Errorf("expected 1 group, got %d", len(importReq.Users[0].Groups))
		}

		// Verify groups
		if len(importReq.Groups) != 1 {
			t.Errorf("expected 1 group, got %d", len(importReq.Groups))
		}
		if importReq.Groups[0].Name != "Group One" {
			t.Errorf("expected group name 'Group One', got '%s'", importReq.Groups[0].Name)
		}

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
		if importReq.Groups != nil {
			t.Error("expected nil groups")
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
					ID:       "u1",
					Email:    "locked@example.com",
					Name:     "Locked User",
					TenantID: "source-tenant",
					Locked:   true,
					Created:  now,
					Updated:  now,
				},
			},
		}

		importReq := exportResult.ConvertToImportRequest("target-tenant")

		if importReq.Users[0].Enabled {
			t.Error("expected locked user to be disabled in import")
		}
	})

	t.Run("convert preserves user attributes", func(t *testing.T) {
		attrs := map[string]interface{}{
			"department": "engineering",
			"level":      3,
		}
		exportResult := &BatchExportResult{
			TenantID: "source-tenant",
			Users: []*User{
				{
					ID:         "u1",
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

	t.Run("cancel during groups export", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := bm.ExportGroups(ctx, tenantID, ExportFilter{})
		if err == nil {
			t.Error("expected cancellation error")
		}
	})

	t.Run("cancel during roles export", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := bm.ExportRoles(ctx, tenantID, ExportFilter{})
		if err == nil {
			t.Error("expected cancellation error")
		}
	})

	t.Run("cancel during policies export", func(t *testing.T) {
		bm, storage := setupTestBatchManager(t)
		populateTestData(t, storage, tenantID)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := bm.ExportPolicies(ctx, tenantID, ExportFilter{})
		if err == nil {
			t.Error("expected cancellation error")
		}
	})
}

// TestExportErrorCases tests various error scenarios.
func TestExportErrorCases(t *testing.T) {
	t.Run("export users with nil identity store", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})
		ctx := context.Background()

		_, err := bm.ExportUsers(ctx, "tenant", ExportFilter{})
		if err == nil {
			t.Error("expected error for nil identity store")
		}
	})

	t.Run("export groups with nil identity store", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})
		ctx := context.Background()

		_, err := bm.ExportGroups(ctx, "tenant", ExportFilter{})
		if err == nil {
			t.Error("expected error for nil identity store")
		}
	})

	t.Run("export roles with nil identity store", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})
		ctx := context.Background()

		_, err := bm.ExportRoles(ctx, "tenant", ExportFilter{})
		if err == nil {
			t.Error("expected error for nil identity store")
		}
	})

	t.Run("export policies with nil policy engine", func(t *testing.T) {
		bm := NewBatchManager(BatchManagerOptions{})
		ctx := context.Background()

		_, err := bm.ExportPolicies(ctx, "tenant", ExportFilter{})
		if err == nil {
			t.Error("expected error for nil policy engine")
		}
	})

	t.Run("export users with empty tenant ID", func(t *testing.T) {
		bm, _ := setupTestBatchManager(t)
		ctx := context.Background()

		_, err := bm.ExportUsers(ctx, "", ExportFilter{})
		if err == nil {
			t.Error("expected validation error for empty tenant ID")
		}
	})

	t.Run("export groups with empty tenant ID", func(t *testing.T) {
		bm, _ := setupTestBatchManager(t)
		ctx := context.Background()

		_, err := bm.ExportGroups(ctx, "", ExportFilter{})
		if err == nil {
			t.Error("expected validation error for empty tenant ID")
		}
	})

	t.Run("export roles with empty tenant ID", func(t *testing.T) {
		bm, _ := setupTestBatchManager(t)
		ctx := context.Background()

		_, err := bm.ExportRoles(ctx, "", ExportFilter{})
		if err == nil {
			t.Error("expected validation error for empty tenant ID")
		}
	})

	t.Run("export policies with empty tenant ID", func(t *testing.T) {
		bm, _ := setupTestBatchManager(t)
		ctx := context.Background()

		_, err := bm.ExportPolicies(ctx, "", ExportFilter{})
		if err == nil {
			t.Error("expected validation error for empty tenant ID")
		}
	})
}

// =============================================================================
// BatchManager - ExportAll more coverage
// =============================================================================

func TestBatchManager_ExportAll_AllIncludes(t *testing.T) {
	storage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
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

	// Create tenant
	tenant := &Tenant{ID: tenantID, Name: "Export All Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	// Create some data
	user := &User{ID: "user-1", TenantID: tenantID, Email: "user1@test.com", Name: "User One", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	group := &Group{ID: "group-1", TenantID: tenantID, Name: "Group One", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateGroup(ctx, group); err != nil {
		t.Fatalf("CreateGroup failed: %v", err)
	}

	role := &Role{ID: "role-1", TenantID: tenantID, Name: "Role One", Created: time.Now(), Updated: time.Now()}
	if err := storage.CreateRole(ctx, role); err != nil {
		t.Fatalf("CreateRole failed: %v", err)
	}

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "policy-1", Tenant: tenantID},
		Spec:       PolicySpec{Rules: []Rule{{Name: "r1", Effect: EffectAllow, Actions: []string{"read"}, Resources: []string{"*"}}}},
	}
	if err := policyEngine.CreatePolicy(ctx, tenantID, policy); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	// Export all
	result, err := bm.ExportAll(ctx, &BatchExportRequest{
		TenantID:        tenantID,
		IncludeUsers:    true,
		IncludeGroups:   true,
		IncludeRoles:    true,
		IncludePolicies: true,
	})
	if err != nil {
		t.Fatalf("ExportAll failed: %v", err)
	}
	if len(result.Users) != 1 {
		t.Errorf("expected 1 user, got %d", len(result.Users))
	}
	if len(result.Groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(result.Groups))
	}
	if len(result.Roles) != 1 {
		t.Errorf("expected 1 role, got %d", len(result.Roles))
	}
	if len(result.Policies) != 1 {
		t.Errorf("expected 1 policy, got %d", len(result.Policies))
	}
}

func TestBatchManager_ExportAll_ValidationError(t *testing.T) {
	storage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})

	policyEngine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
	})

	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: identityStore,
		PolicyEngine:  policyEngine,
	})

	ctx := context.Background()

	// Export with empty tenant ID should fail
	_, err := bm.ExportAll(ctx, &BatchExportRequest{
		TenantID:     "", // Missing tenant ID
		IncludeUsers: true,
	})
	if err == nil {
		t.Error("expected validation error for empty tenant ID")
	}
}

// =============================================================================
// BatchManager - ImportAll more coverage
// =============================================================================

func TestBatchManager_ImportAll_ValidationError(t *testing.T) {
	storage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
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
			{ID: "user-1", Email: "user1@test.com", Name: "User One"},
		},
	})
	if err == nil {
		t.Error("expected validation error for empty tenant ID")
	}
}

func TestBatchManager_ImportAll_NoItems(t *testing.T) {
	storage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
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

func TestAdminManager_CreateAssignment_InvalidRole(t *testing.T) {
	adminStorage := NewMemoryAdminStorage()
	impersonationStorage := NewMemoryImpersonationStorage()
	auditLogger, err := NewAuditLogger(WithAuditStorage(NewMemoryAuditStorage()))
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}

	mgr, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         adminStorage,
		ImpersonationStorage: impersonationStorage,
		AuditLogger:          auditLogger,
	})
	if err != nil {
		t.Fatalf("NewAdminManager failed: %v", err)
	}

	ctx := context.Background()

	// Create assignment with invalid role
	_, err = mgr.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user-1",
		AdminRole: AdminRole("invalid-role"), // Invalid role
		GrantedBy: "admin-1",
	})
	if err == nil {
		t.Error("expected error for invalid role")
	}
}

func TestAdminManager_CreateAssignment_MissingUserID(t *testing.T) {
	adminStorage := NewMemoryAdminStorage()
	impersonationStorage := NewMemoryImpersonationStorage()
	auditLogger, err := NewAuditLogger(WithAuditStorage(NewMemoryAuditStorage()))
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}

	mgr, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         adminStorage,
		ImpersonationStorage: impersonationStorage,
		AuditLogger:          auditLogger,
	})
	if err != nil {
		t.Fatalf("NewAdminManager failed: %v", err)
	}

	ctx := context.Background()

	// Create assignment with missing user ID
	_, err = mgr.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "", // Missing user ID
		AdminRole: AdminRoleTenantAdmin,
		GrantedBy: "admin-1",
	})
	if err == nil {
		t.Error("expected error for missing user ID")
	}
}

func TestAdminManager_CreateAssignment_MissingGrantedBy(t *testing.T) {
	adminStorage := NewMemoryAdminStorage()
	impersonationStorage := NewMemoryImpersonationStorage()
	auditLogger, err := NewAuditLogger(WithAuditStorage(NewMemoryAuditStorage()))
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}

	mgr, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         adminStorage,
		ImpersonationStorage: impersonationStorage,
		AuditLogger:          auditLogger,
	})
	if err != nil {
		t.Fatalf("NewAdminManager failed: %v", err)
	}

	ctx := context.Background()

	// Create assignment with missing granted by
	_, err = mgr.CreateAssignment(ctx, CreateAdminAssignmentOptions{
		UserID:    "user-1",
		AdminRole: AdminRoleTenantAdmin,
		GrantedBy: "", // Missing granted by
	})
	if err == nil {
		t.Error("expected error for missing granted by")
	}
}

// =============================================================================
// AdminManager - StartImpersonation error paths
// =============================================================================

func TestAdminManager_StartImpersonation_MissingAdminUserID(t *testing.T) {
	adminStorage := NewMemoryAdminStorage()
	impersonationStorage := NewMemoryImpersonationStorage()
	auditLogger, err := NewAuditLogger(WithAuditStorage(NewMemoryAuditStorage()))
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}

	mgr, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         adminStorage,
		ImpersonationStorage: impersonationStorage,
		AuditLogger:          auditLogger,
	})
	if err != nil {
		t.Fatalf("NewAdminManager failed: %v", err)
	}

	ctx := context.Background()

	_, err = mgr.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "", // Missing
		TargetUserID: "target-1",
		TenantID:     "tenant-1",
		Reason:       "Testing",
	})
	if err == nil {
		t.Error("expected error for missing admin user ID")
	}
}

func TestAdminManager_StartImpersonation_MissingTargetUserID(t *testing.T) {
	adminStorage := NewMemoryAdminStorage()
	impersonationStorage := NewMemoryImpersonationStorage()
	auditLogger, err := NewAuditLogger(WithAuditStorage(NewMemoryAuditStorage()))
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}

	mgr, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         adminStorage,
		ImpersonationStorage: impersonationStorage,
		AuditLogger:          auditLogger,
	})
	if err != nil {
		t.Fatalf("NewAdminManager failed: %v", err)
	}

	ctx := context.Background()

	_, err = mgr.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "admin-1",
		TargetUserID: "", // Missing
		TenantID:     "tenant-1",
		Reason:       "Testing",
	})
	if err == nil {
		t.Error("expected error for missing target user ID")
	}
}

func TestAdminManager_StartImpersonation_MissingReason(t *testing.T) {
	adminStorage := NewMemoryAdminStorage()
	impersonationStorage := NewMemoryImpersonationStorage()
	auditLogger, err := NewAuditLogger(WithAuditStorage(NewMemoryAuditStorage()))
	if err != nil {
		t.Fatalf("NewAuditLogger failed: %v", err)
	}

	mgr, err := NewAdminManager(AdminManagerOptions{
		AdminStorage:         adminStorage,
		ImpersonationStorage: impersonationStorage,
		AuditLogger:          auditLogger,
	})
	if err != nil {
		t.Fatalf("NewAdminManager failed: %v", err)
	}

	ctx := context.Background()

	_, err = mgr.StartImpersonation(ctx, &StartImpersonationOptions{
		AdminUserID:  "admin-1",
		TargetUserID: "target-1",
		TenantID:     "tenant-1",
		Reason:       "", // Missing
	})
	if err == nil {
		t.Error("expected error for missing reason")
	}
}

// =============================================================================
// PolicyCompiler - compileAttributeMatcher coverage
// =============================================================================

func TestPolicyCompiler_CompileWithMatchExpressions(t *testing.T) {
	compiler := NewPolicyCompiler()

	policy := &Policy{
		APIVersion: "v1",
		Kind:       "Policy",
		Metadata:   PolicyMetadata{Name: "match-expr-test", Tenant: "tenant-1"},
		Spec: PolicySpec{
			Subjects: SubjectSelector{
				Match: map[string]interface{}{
					"department": "engineering",
					"level":      "senior",
				},
			},
			Rules: []Rule{
				{
					Name:      "allow-senior-eng",
					Effect:    EffectAllow,
					Actions:   []string{"*"},
					Resources: []string{"*"},
				},
			},
		},
	}

	compiled, err := compiler.Compile(policy)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if compiled == nil {
		t.Error("expected compiled policy")
	}
	if compiled.SubjectMatcher == nil {
		t.Error("expected subject matcher")
	}
}

// =============================================================================
// BatchProcessor - more rate limit and error paths
// =============================================================================

func TestBatchProcessor_ProcessGroups_WithRateLimit(t *testing.T) {
	storage := newMockBatchStorage()
	tenantMgr := newMockTenantManager()
	bp := NewIAMBatchProcessor(storage, tenantMgr)

	ctx := context.Background()
	tenantID := "groups-tenant"

	items := []GroupImportItem{
		{ID: "group-1", Name: "Group One"},
		{ID: "group-2", Name: "Group Two"},
	}

	// Verify we can create an operation with rate limit option
	op, err := bp.ImportGroups(ctx, tenantID, items, BatchOptions{
		RateLimit: 5,
	})
	if err != nil {
		t.Fatalf("ImportGroups failed: %v", err)
	}

	// Verify operation was created with correct attributes
	if op.ID == "" {
		t.Error("expected operation to have ID")
	}
	if op.TenantID != tenantID {
		t.Errorf("expected tenantID %s, got %s", tenantID, op.TenantID)
	}
	if op.TotalItems != 2 {
		t.Errorf("expected 2 total items, got %d", op.TotalItems)
	}
}

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
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
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
// ImportGroups - more coverage
// =============================================================================

func TestBatchManager_ImportGroups_WithDescription(t *testing.T) {
	storage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})

	policyEngine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
	})

	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: identityStore,
		PolicyEngine:  policyEngine,
	})

	ctx := context.Background()
	tenantID := "group-import-tenant"

	tenant := &Tenant{ID: tenantID, Name: "Group Import Tenant", Created: time.Now()}
	if err := storage.CreateTenant(ctx, tenant); err != nil {
		t.Fatalf("CreateTenant failed: %v", err)
	}

	result, err := bm.ImportGroups(ctx, tenantID, []*GroupImport{
		{
			ID:          "group-1",
			Name:        "Engineering Group",
			Description: "Engineering team group",
		},
	}, BatchImportOptions{})
	if err != nil {
		t.Fatalf("ImportGroups failed: %v", err)
	}
	if result.SuccessCount != 1 {
		t.Errorf("expected 1 success, got %d", result.SuccessCount)
	}
}

// =============================================================================
// ExportUsers - context cancellation
// =============================================================================

func TestBatchManager_ExportUsers_ContextCanceled(t *testing.T) {
	storage := NewMemoryStorage()

	identityStore := NewIdentityStore(IdentityStoreOptions{
		UserStorage:  storage,
		GroupStorage: storage,
		RoleStorage:  storage,
	})

	policyEngine := NewPolicyEngine(PolicyEngineOptions{
		Storage: storage,
	})

	bm := NewBatchManager(BatchManagerOptions{
		IdentityStore: identityStore,
		PolicyEngine:  policyEngine,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := bm.ExportUsers(ctx, "tenant-1", ExportFilter{})
	if err == nil {
		t.Error("expected error for canceled context")
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
