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

package providers

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// mockRoleManager implements interfaces.RoleManager for testing.
type mockRoleManager struct {
	permissions     map[string]map[string][]string // role -> resource -> actions
	anonymousAccess map[string][]string            // resource -> actions
	roles           map[string]interfaces.RoleInfo
	mu              sync.RWMutex
}

func newMockRoleManager() *mockRoleManager {
	return &mockRoleManager{
		permissions: map[string]map[string][]string{
			"admin": {
				"*": {"*"},
			},
			"readwrite": {
				"keys":         {"read", "write", "delete"},
				"transactions": {"read", "write"},
			},
			"readonly": {
				"keys":    {"read"},
				"metrics": {"read"},
			},
		},
		anonymousAccess: map[string][]string{
			"health":  {"read"},
			"metrics": {"read"},
		},
		roles: make(map[string]interfaces.RoleInfo),
	}
}

func (m *mockRoleManager) HasPermission(userRoles []string, resource, action string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, role := range userRoles {
		perms, exists := m.permissions[role]
		if !exists {
			continue
		}

		// Check wildcard resource
		if actions, ok := perms["*"]; ok {
			for _, a := range actions {
				if a == "*" || a == action {
					return true
				}
			}
		}

		// Check specific resource
		if actions, ok := perms[resource]; ok {
			for _, a := range actions {
				if a == "*" || a == action {
					return true
				}
			}
		}
	}

	return false
}

func (m *mockRoleManager) AllowsAnonymous(resource, action string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if actions, ok := m.anonymousAccess[resource]; ok {
		for _, a := range actions {
			if a == action {
				return true
			}
		}
	}
	return false
}

func (m *mockRoleManager) AddRole(role interfaces.RoleInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roles[role.GetName()] = role
	return nil
}

func (m *mockRoleManager) GetRole(name string) interfaces.RoleInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.roles[name]
}

func (m *mockRoleManager) ListRoles() []interfaces.RoleInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]interfaces.RoleInfo, 0, len(m.roles))
	for _, r := range m.roles {
		result = append(result, r)
	}
	return result
}

func (m *mockRoleManager) DeleteRole(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.roles, name)
	return nil
}

func (m *mockRoleManager) UpdateRole(role interfaces.RoleInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roles[role.GetName()] = role
	return nil
}

func (m *mockRoleManager) ValidateRoles(roleNames []string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, name := range roleNames {
		if _, exists := m.permissions[name]; !exists {
			if _, exists := m.roles[name]; !exists {
				return false
			}
		}
	}
	return true
}

func createTestLogger(t *testing.T) *logger.Logger {
	log, err := logger.New(logger.LogConfig{
		Level:     "debug",
		Format:    "json",
		Output:    "stdout",
		Component: "test",
	})
	require.NoError(t, err)
	return log
}

// TestNewRBACProvider tests provider creation.
func TestNewRBACProvider(t *testing.T) {
	t.Run("success with valid role manager", func(t *testing.T) {
		rm := newMockRoleManager()
		log := createTestLogger(t)

		provider, err := NewRBACProvider(rm, log)

		require.NoError(t, err)
		require.NotNil(t, provider)
		assert.Equal(t, ProviderTypeRBAC, provider.GetProviderType())
	})

	t.Run("success with nil logger creates default", func(t *testing.T) {
		rm := newMockRoleManager()

		provider, err := NewRBACProvider(rm, nil)

		require.NoError(t, err)
		require.NotNil(t, provider)
		assert.NotNil(t, provider.logger)
	})

	t.Run("error with nil role manager", func(t *testing.T) {
		log := createTestLogger(t)

		provider, err := NewRBACProvider(nil, log)

		require.Error(t, err)
		assert.Nil(t, provider)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
		assert.ErrorIs(t, qbErr.Err, ErrNilRoleManager)
	})
}

// TestRBACProviderAuthorize tests single authorization requests.
func TestRBACProviderAuthorize(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	t.Run("admin role allows all actions", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user1",
				Username:      "admin-user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "admin",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
		assert.Equal(t, ProviderTypeRBAC, decision.Provider)
		assert.Equal(t, "admin", decision.MatchedPolicy)
		assert.NotEmpty(t, decision.DecisionID)
		assert.Greater(t, decision.EvaluationTimeNs, int64(0))
	})

	t.Run("readwrite role allows read on keys", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user2",
				Username:      "readwrite-user",
				Roles:         []string{"readwrite"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "readwrite", decision.MatchedPolicy)
	})

	t.Run("readwrite role allows write on keys", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user2",
				Username:      "readwrite-user",
				Roles:         []string{"readwrite"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "write",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("readonly role denies write on keys", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user3",
				Username:      "readonly-user",
				Roles:         []string{"readonly"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "write",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.Reason, "roles [readonly] do not have write permission on keys")
		assert.Empty(t, decision.MatchedPolicy)
	})

	t.Run("multiple roles uses first matching", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user4",
				Username:      "multi-role-user",
				Roles:         []string{"readonly", "readwrite"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "write",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "readwrite", decision.MatchedPolicy)
	})

	t.Run("no roles assigned denies access", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user5",
				Username:      "no-role-user",
				Roles:         []string{},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Equal(t, "no roles assigned", decision.Reason)
	})

	t.Run("unknown role denies access", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user6",
				Username:      "unknown-role-user",
				Roles:         []string{"nonexistent"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.Reason, "roles [nonexistent] do not have read permission on keys")
	})

	t.Run("nil request returns error", func(t *testing.T) {
		decision, err := provider.Authorize(context.Background(), nil)

		require.Error(t, err)
		assert.Nil(t, decision)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
	})

	t.Run("canceled context returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(ctx, req)

		require.Error(t, err)
		assert.Nil(t, decision)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrCanceled, qbErr.Code)
	})
}

// TestRBACProviderAnonymousAccess tests anonymous access handling.
func TestRBACProviderAnonymousAccess(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	t.Run("anonymous access to health allowed", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				Authenticated: false,
			},
			Resource: "health",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.True(t, decision.Allowed)
		assert.Equal(t, "anonymous access allowed", decision.Reason)
		assert.Equal(t, "anonymous", decision.MatchedPolicy)
	})

	t.Run("anonymous access to metrics allowed", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				Authenticated: false,
			},
			Resource: "metrics",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})

	t.Run("anonymous access to keys denied", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				Authenticated: false,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.False(t, decision.Allowed)
		assert.Equal(t, "authentication required", decision.Reason)
	})

	t.Run("anonymous write to health denied", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				Authenticated: false,
			},
			Resource: "health",
			Action:   "write",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.False(t, decision.Allowed)
	})
}

// TestRBACProviderBatchAuthorize tests batch authorization.
func TestRBACProviderBatchAuthorize(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	t.Run("batch with mixed results", func(t *testing.T) {
		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "user1",
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "admin",
			},
			{
				Subject: interfaces.Subject{
					ID:            "user2",
					Roles:         []string{"readonly"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "write",
			},
			{
				Subject: interfaces.Subject{
					ID:            "user3",
					Roles:         []string{"readwrite"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)

		require.NoError(t, err)
		require.Len(t, decisions, 3)
		assert.True(t, decisions[0].Allowed)
		assert.False(t, decisions[1].Allowed)
		assert.True(t, decisions[2].Allowed)
	})

	t.Run("empty batch returns empty slice", func(t *testing.T) {
		decisions, err := provider.BatchAuthorize(context.Background(), []*interfaces.AuthorizationRequest{})

		require.NoError(t, err)
		assert.Empty(t, decisions)
	})

	t.Run("batch with nil request handles gracefully", func(t *testing.T) {
		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
			nil,
			{
				Subject: interfaces.Subject{
					Roles:         []string{"readonly"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)

		require.NoError(t, err)
		require.Len(t, decisions, 3)
		assert.True(t, decisions[0].Allowed)
		assert.False(t, decisions[1].Allowed)
		assert.Equal(t, "nil request", decisions[1].Reason)
		assert.True(t, decisions[2].Allowed)
	})

	t.Run("canceled context returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(ctx, reqs)

		require.Error(t, err)
		assert.Nil(t, decisions)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrCanceled, qbErr.Code)
	})
}

// TestRBACProviderClose tests provider close behavior.
func TestRBACProviderClose(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	t.Run("close succeeds", func(t *testing.T) {
		err := provider.Close()
		assert.NoError(t, err)
	})

	t.Run("double close is idempotent", func(t *testing.T) {
		err := provider.Close()
		assert.NoError(t, err)
	})

	t.Run("authorize after close returns error", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.Error(t, err)
		assert.Nil(t, decision)

		var qbErr *errors.QBError
		require.ErrorAs(t, err, &qbErr)
		assert.Equal(t, errors.ErrUnavailable, qbErr.Code)
	})

	t.Run("batch authorize after close returns error", func(t *testing.T) {
		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					Roles:         []string{"admin"},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)

		require.Error(t, err)
		assert.Nil(t, decisions)
	})
}

// TestRBACProviderGetProviderType tests the provider type getter.
func TestRBACProviderGetProviderType(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	assert.Equal(t, "rbac", provider.GetProviderType())
	assert.Equal(t, ProviderTypeRBAC, provider.GetProviderType())
}

// TestRBACProviderDecisionIDUniqueness tests that decision IDs are unique.
func TestRBACProviderDecisionIDUniqueness(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)

		if ids[decision.DecisionID] {
			t.Fatalf("duplicate decision ID: %s", decision.DecisionID)
		}
		ids[decision.DecisionID] = true
	}

	assert.Len(t, ids, 100)
}

// TestRBACProviderConcurrentAccess tests thread safety.
func TestRBACProviderConcurrentAccess(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	const numGoroutines = 50
	const numRequests = 100

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*numRequests)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			roles := []string{"admin", "readwrite", "readonly"}
			role := roles[id%len(roles)]

			req := &interfaces.AuthorizationRequest{
				Subject: interfaces.Subject{
					ID:            "user",
					Roles:         []string{role},
					Authenticated: true,
				},
				Resource: "keys",
				Action:   "read",
			}

			for j := 0; j < numRequests; j++ {
				_, err := provider.Authorize(context.Background(), req)
				if err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("unexpected error during concurrent access: %v", err)
	}
}

// TestRBACProviderPerformance tests authorization latency and logs timing.
// The actual performance assertion is done via benchmarks to avoid race detector overhead.
func TestRBACProviderPerformance(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	// Warm up
	for i := 0; i < 1000; i++ {
		_, _ = provider.Authorize(context.Background(), req)
	}

	// Measure
	const iterations = 10000
	start := time.Now()
	for i := 0; i < iterations; i++ {
		_, _ = provider.Authorize(context.Background(), req)
	}
	duration := time.Since(start)

	avgLatency := duration / iterations
	t.Logf("Average authorization latency: %v", avgLatency)

	// Use a lenient threshold that works with race detector (100us)
	// Real performance is verified via benchmarks
	assert.Less(t, avgLatency, 100*time.Microsecond,
		"Authorization latency should be under 100us (with race detector), got %v", avgLatency)
}

// TestRBACProviderDenyReasonFormat tests the format of denial reasons.
func TestRBACProviderDenyReasonFormat(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	testCases := []struct {
		name           string
		roles          []string
		resource       string
		action         string
		expectedReason string
	}{
		{
			name:           "single role",
			roles:          []string{"readonly"},
			resource:       "keys",
			action:         "write",
			expectedReason: "roles [readonly] do not have write permission on keys",
		},
		{
			name:           "multiple roles",
			roles:          []string{"readonly", "viewer"},
			resource:       "cluster",
			action:         "admin",
			expectedReason: "roles [readonly, viewer] do not have admin permission on cluster",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &interfaces.AuthorizationRequest{
				Subject: interfaces.Subject{
					Roles:         tc.roles,
					Authenticated: true,
				},
				Resource: tc.resource,
				Action:   tc.action,
			}

			decision, err := provider.Authorize(context.Background(), req)

			require.NoError(t, err)
			assert.False(t, decision.Allowed)
			assert.Equal(t, tc.expectedReason, decision.Reason)
		})
	}
}

// TestRBACProviderAllResources tests authorization for all defined resources.
func TestRBACProviderAllResources(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	resources := []string{
		interfaces.ResourceKeys,
		interfaces.ResourceTransactions,
		interfaces.ResourceCluster,
		interfaces.ResourceBackups,
		interfaces.ResourceUsers,
		interfaces.ResourceRoles,
		interfaces.ResourceSecrets,
		interfaces.ResourceKMS,
		interfaces.ResourceCrypto,
		interfaces.ResourceSeal,
		interfaces.ResourceHealth,
		interfaces.ResourceMetrics,
		interfaces.ResourceWatch,
		interfaces.ResourceTimeseries,
		interfaces.ResourceVector,
	}

	actions := []string{
		interfaces.ActionRead,
		interfaces.ActionWrite,
		interfaces.ActionDelete,
		interfaces.ActionAdmin,
	}

	// Admin should have access to all resources and actions
	for _, resource := range resources {
		for _, action := range actions {
			t.Run(resource+"/"+action, func(t *testing.T) {
				req := &interfaces.AuthorizationRequest{
					Subject: interfaces.Subject{
						Roles:         []string{"admin"},
						Authenticated: true,
					},
					Resource: resource,
					Action:   action,
				}

				decision, err := provider.Authorize(context.Background(), req)

				require.NoError(t, err)
				assert.True(t, decision.Allowed, "admin should have %s access to %s", action, resource)
			})
		}
	}
}

// TestRBACProviderWildcardAction tests wildcard action permissions.
func TestRBACProviderWildcardAction(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	// Admin has wildcard access
	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "custom-resource",
		Action:   "custom-action",
	}

	decision, err := provider.Authorize(context.Background(), req)

	require.NoError(t, err)
	assert.True(t, decision.Allowed, "admin should have wildcard access")
}

// TestRBACProviderSubjectContext tests that subject context is properly used.
func TestRBACProviderSubjectContext(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)
	provider, err := NewRBACProvider(rm, log)
	require.NoError(t, err)

	t.Run("uses subject roles for authorization", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Username:      "testuser",
				TenantID:      "tenant-456",
				Roles:         []string{"readwrite"},
				Groups:        []string{"engineering"},
				Attributes:    map[string]interface{}{"department": "dev"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "write",
			Context:  map[string]interface{}{"ip": "192.168.1.1"},
		}

		decision, err := provider.Authorize(context.Background(), req)

		require.NoError(t, err)
		assert.True(t, decision.Allowed)
	})
}

// BenchmarkRBACProviderAuthorize benchmarks single authorization performance.
// Target: < 1us per check.
func BenchmarkRBACProviderAuthorize(b *testing.B) {
	rm := newMockRoleManager()
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "bench",
	})
	provider, _ := NewRBACProvider(rm, log)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = provider.Authorize(ctx, req)
	}
}

// BenchmarkRBACProviderBatchAuthorize benchmarks batch authorization performance.
func BenchmarkRBACProviderBatchAuthorize(b *testing.B) {
	rm := newMockRoleManager()
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "bench",
	})
	provider, _ := NewRBACProvider(rm, log)

	reqs := make([]*interfaces.AuthorizationRequest, 100)
	for i := 0; i < 100; i++ {
		reqs[i] = &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user",
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		}
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = provider.BatchAuthorize(ctx, reqs)
	}
}

// BenchmarkRBACProviderAuthorizeParallel benchmarks concurrent authorization.
func BenchmarkRBACProviderAuthorizeParallel(b *testing.B) {
	rm := newMockRoleManager()
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "bench",
	})
	provider, _ := NewRBACProvider(rm, log)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = provider.Authorize(ctx, req)
		}
	})
}
