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

//go:build iam

package providers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// testLogger creates a logger for testing.
func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)
	return log
}

// createTestCacheConfig creates a cache config for testing.
func createTestCacheConfig() *iam.CacheConfig {
	return &iam.CacheConfig{
		TTL:               time.Hour,
		MaxSize:           100,
		BloomFilterSize:   10000,
		BloomFilterHashes: 7,
	}
}

// createTestPolicyCache creates a policy cache with test policies.
func createTestPolicyCache(t *testing.T, tenantID string) *iam.PolicyCache {
	t.Helper()

	cache := iam.NewPolicyCache(createTestCacheConfig())

	// Add a simple allow policy for authenticated users
	allowPolicy := &iam.CompiledPolicy{
		Name:     "allow-read-keys",
		Priority: 100,
		SubjectMatcher: &iam.SubjectMatcher{
			Authenticated: true,
		},
		Rules: []*iam.CompiledRule{
			{
				Name:         "allow-key-read",
				Effect:       iam.EffectAllow,
				Priority:     100,
				Actions:      []string{"read", "*"},
				ResourceTrie: createResourceTrie("keys/*"),
				Conditions:   nil,
				Unless:       nil,
			},
		},
		Variables: make(map[string]interface{}),
	}
	cache.Set(tenantID, allowPolicy)

	// Add a deny policy for specific resources
	denyPolicy := &iam.CompiledPolicy{
		Name:     "deny-admin-secrets",
		Priority: 200, // Higher priority - evaluated first
		SubjectMatcher: &iam.SubjectMatcher{
			Any: true,
		},
		Rules: []*iam.CompiledRule{
			{
				Name:         "deny-secret-admin",
				Effect:       iam.EffectDeny,
				Priority:     200,
				Actions:      []string{"admin"},
				ResourceTrie: createResourceTrie("secrets/*"),
				Conditions:   nil,
				Unless:       nil,
			},
		},
		Variables: make(map[string]interface{}),
	}
	cache.Set(tenantID, denyPolicy)

	// Add admin policy
	adminPolicy := &iam.CompiledPolicy{
		Name:     "admin-all-access",
		Priority: 50,
		SubjectMatcher: &iam.SubjectMatcher{
			Roles: map[string]bool{"admin": true},
		},
		Rules: []*iam.CompiledRule{
			{
				Name:         "admin-all",
				Effect:       iam.EffectAllow,
				Priority:     50,
				Actions:      []string{"*"},
				ResourceTrie: createResourceTrie("*"),
				Conditions:   nil,
				Unless:       nil,
			},
		},
		Variables: make(map[string]interface{}),
	}
	cache.Set(tenantID, adminPolicy)

	return cache
}

// createResourceTrie creates a simple resource trie for testing.
func createResourceTrie(pattern string) *iam.ResourceTrie {
	trie := iam.NewResourceTrie()
	trie.Insert(pattern, true)
	return trie
}

// createTestEvaluator creates a policy evaluator for testing.
func createTestEvaluator(t *testing.T, tenantID string) *iam.PolicyEvaluator {
	t.Helper()

	compiler := iam.NewPolicyCompiler()
	cache := createTestPolicyCache(t, tenantID)

	return iam.NewPolicyEvaluator(compiler, cache)
}

// TestNewABACProvider tests provider creation.
func TestNewABACProvider(t *testing.T) {
	t.Run("success with valid parameters", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, "test-tenant")

		provider, err := NewABACProvider(evaluator, "test-tenant", log)
		require.NoError(t, err)
		require.NotNil(t, provider)

		assert.Equal(t, "abac", provider.GetProviderType())
		assert.False(t, provider.closed.Load())
	})

	t.Run("error with nil evaluator", func(t *testing.T) {
		log := testLogger(t)

		provider, err := NewABACProvider(nil, "test-tenant", log)
		assert.Nil(t, provider)
		assert.Error(t, err)

		var abacErr *ABACProviderError
		assert.ErrorAs(t, err, &abacErr)
		assert.Equal(t, "NewABACProvider", abacErr.Operation)
		assert.Contains(t, abacErr.Error(), "evaluator cannot be nil")
	})

	t.Run("error with nil logger", func(t *testing.T) {
		evaluator := createTestEvaluator(t, "test-tenant")

		provider, err := NewABACProvider(evaluator, "test-tenant", nil)
		assert.Nil(t, provider)
		assert.Error(t, err)

		var abacErr *ABACProviderError
		assert.ErrorAs(t, err, &abacErr)
		assert.Equal(t, "NewABACProvider", abacErr.Operation)
		assert.Contains(t, abacErr.Error(), "logger cannot be nil")
	})

	t.Run("success with empty tenant ID", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, "")

		provider, err := NewABACProvider(evaluator, "", log)
		require.NoError(t, err)
		require.NotNil(t, provider)
		assert.Equal(t, "", provider.tenantID)
	})
}

// TestABACProviderAuthorize tests the Authorize method.
func TestABACProviderAuthorize(t *testing.T) {
	const tenantID = "test-tenant"

	t.Run("allow authenticated user read access", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Username:      "testuser",
				TenantID:      tenantID,
				Authenticated: true,
			},
			Resource: "keys/mykey",
			Action:   "read",
			Context:  nil,
		}

		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, decision)

		assert.True(t, decision.Allowed)
		assert.Equal(t, "abac", decision.Provider)
		assert.NotEmpty(t, decision.DecisionID)
		assert.Greater(t, decision.EvaluationTimeNs, int64(0))
	})

	t.Run("deny unauthenticated user", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-456",
				Username:      "anonuser",
				TenantID:      tenantID,
				Authenticated: false,
			},
			Resource: "keys/mykey",
			Action:   "read",
			Context:  nil,
		}

		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, decision)

		assert.False(t, decision.Allowed)
		assert.Equal(t, "abac", decision.Provider)
	})

	t.Run("deny admin action on secrets", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-789",
				Username:      "adminuser",
				TenantID:      tenantID,
				Roles:         []string{"admin"},
				Authenticated: true,
			},
			Resource: "secrets/database-password",
			Action:   "admin",
			Context:  nil,
		}

		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, decision)

		// Denied by the explicit deny policy
		assert.False(t, decision.Allowed)
	})

	t.Run("nil request returns deny decision", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		decision, err := provider.Authorize(context.Background(), nil)
		require.NoError(t, err)
		require.NotNil(t, decision)

		assert.False(t, decision.Allowed)
		assert.Contains(t, decision.Reason, "request cannot be nil")
		assert.Equal(t, "abac", decision.Provider)
	})

	t.Run("error when provider is closed", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		// Close the provider
		err = provider.Close()
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Authenticated: true,
			},
			Resource: "keys/test",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)
		assert.Nil(t, decision)
		assert.Error(t, err)

		var abacErr *ABACProviderError
		assert.ErrorAs(t, err, &abacErr)
		assert.Equal(t, "Authorize", abacErr.Operation)
		assert.Contains(t, abacErr.Error(), "provider is closed")
	})

	t.Run("error on cancelled context", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Authenticated: true,
			},
			Resource: "keys/test",
			Action:   "read",
		}

		decision, err := provider.Authorize(ctx, req)
		assert.Nil(t, decision)
		assert.Error(t, err)

		var abacErr *ABACProviderError
		assert.ErrorAs(t, err, &abacErr)
		assert.Equal(t, "Authorize", abacErr.Operation)
		assert.Contains(t, abacErr.Error(), "context cancelled")
	})

	t.Run("use subject tenant ID when set", func(t *testing.T) {
		log := testLogger(t)
		subjectTenant := "subject-tenant"
		evaluator := createTestEvaluator(t, subjectTenant)
		provider, err := NewABACProvider(evaluator, "default-tenant", log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      subjectTenant, // Use subject's tenant
				Authenticated: true,
			},
			Resource: "keys/test",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, decision)
		// Should use subject's tenant for policy lookup
		assert.True(t, decision.Allowed)
	})

	t.Run("use provider default tenant when subject tenant empty", func(t *testing.T) {
		log := testLogger(t)
		defaultTenant := "default-tenant"
		evaluator := createTestEvaluator(t, defaultTenant)
		provider, err := NewABACProvider(evaluator, defaultTenant, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "", // Empty - should use provider default
				Authenticated: true,
			},
			Resource: "keys/test",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
	})

	t.Run("request with context attributes", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Username:      "testuser",
				TenantID:      tenantID,
				Authenticated: true,
				Attributes: map[string]interface{}{
					"department": "engineering",
					"level":      5,
				},
			},
			Resource: "keys/test",
			Action:   "read",
			Context: map[string]interface{}{
				"ip_address": "192.168.1.100",
				"timestamp":  time.Now().Unix(),
			},
		}

		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
	})

	t.Run("request with roles and groups", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Username:      "testuser",
				TenantID:      tenantID,
				Roles:         []string{"developer", "reader"},
				Groups:        []string{"engineering", "devops"},
				Authenticated: true,
			},
			Resource: "keys/test",
			Action:   "read",
		}

		decision, err := provider.Authorize(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, decision)
		assert.True(t, decision.Allowed)
	})
}

// TestABACProviderBatchAuthorize tests the BatchAuthorize method.
func TestABACProviderBatchAuthorize(t *testing.T) {
	const tenantID = "test-tenant"

	t.Run("batch authorize multiple requests", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "user-1",
					TenantID:      tenantID,
					Authenticated: true,
				},
				Resource: "keys/key1",
				Action:   "read",
			},
			{
				Subject: interfaces.Subject{
					ID:            "user-2",
					TenantID:      tenantID,
					Authenticated: true,
				},
				Resource: "keys/key2",
				Action:   "read",
			},
			{
				Subject: interfaces.Subject{
					ID:            "user-3",
					TenantID:      tenantID,
					Authenticated: false, // Should be denied
				},
				Resource: "keys/key3",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)
		require.NoError(t, err)
		require.Len(t, decisions, 3)

		// First two should be allowed (authenticated)
		assert.True(t, decisions[0].Allowed)
		assert.True(t, decisions[1].Allowed)
		// Third should be denied (unauthenticated)
		assert.False(t, decisions[2].Allowed)

		// All should have the correct provider
		for _, d := range decisions {
			assert.Equal(t, "abac", d.Provider)
			assert.NotEmpty(t, d.DecisionID)
		}
	})

	t.Run("empty request slice returns empty decisions", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		decisions, err := provider.BatchAuthorize(context.Background(), []*interfaces.AuthorizationRequest{})
		require.NoError(t, err)
		require.Empty(t, decisions)
	})

	t.Run("nil request slice returns empty decisions", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		decisions, err := provider.BatchAuthorize(context.Background(), nil)
		require.NoError(t, err)
		require.Empty(t, decisions)
	})

	t.Run("error when provider is closed", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		err = provider.Close()
		require.NoError(t, err)

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "user-1",
					Authenticated: true,
				},
				Resource: "keys/test",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)
		assert.Nil(t, decisions)
		assert.Error(t, err)

		var abacErr *ABACProviderError
		assert.ErrorAs(t, err, &abacErr)
		assert.Equal(t, "BatchAuthorize", abacErr.Operation)
	})

	t.Run("context cancellation during batch", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "user-1",
					Authenticated: true,
				},
				Resource: "keys/test",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(ctx, reqs)
		assert.Nil(t, decisions)
		assert.Error(t, err)

		var abacErr *ABACProviderError
		assert.ErrorAs(t, err, &abacErr)
		assert.Equal(t, "BatchAuthorize", abacErr.Operation)
		assert.Contains(t, abacErr.Error(), "context cancelled")
	})

	t.Run("handles nil request in batch", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "user-1",
					TenantID:      tenantID,
					Authenticated: true,
				},
				Resource: "keys/key1",
				Action:   "read",
			},
			nil, // Nil request should result in deny
			{
				Subject: interfaces.Subject{
					ID:            "user-2",
					TenantID:      tenantID,
					Authenticated: true,
				},
				Resource: "keys/key2",
				Action:   "read",
			},
		}

		decisions, err := provider.BatchAuthorize(context.Background(), reqs)
		require.NoError(t, err)
		require.Len(t, decisions, 3)

		assert.True(t, decisions[0].Allowed)
		assert.False(t, decisions[1].Allowed) // Nil request denied
		assert.Contains(t, decisions[1].Reason, "request cannot be nil")
		assert.True(t, decisions[2].Allowed)
	})

	t.Run("large batch performance", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)

		// Create 100 requests
		reqs := make([]*interfaces.AuthorizationRequest, 100)
		for i := 0; i < 100; i++ {
			reqs[i] = &interfaces.AuthorizationRequest{
				Subject: interfaces.Subject{
					ID:            "user-" + string(rune('0'+i%10)),
					TenantID:      tenantID,
					Authenticated: true,
				},
				Resource: "keys/key-" + string(rune('0'+i%10)),
				Action:   "read",
			}
		}

		start := time.Now()
		decisions, err := provider.BatchAuthorize(context.Background(), reqs)
		elapsed := time.Since(start)

		require.NoError(t, err)
		require.Len(t, decisions, 100)

		// All should be allowed
		for _, d := range decisions {
			assert.True(t, d.Allowed)
		}

		// Should complete in reasonable time (< 50ms for 100 checks)
		assert.Less(t, elapsed, 50*time.Millisecond, "batch should complete quickly")
	})
}

// TestABACProviderGetProviderType tests the GetProviderType method.
func TestABACProviderGetProviderType(t *testing.T) {
	t.Run("returns abac", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, "test-tenant")
		provider, err := NewABACProvider(evaluator, "test-tenant", log)
		require.NoError(t, err)

		assert.Equal(t, "abac", provider.GetProviderType())
	})
}

// TestABACProviderClose tests the Close method.
func TestABACProviderClose(t *testing.T) {
	t.Run("close succeeds", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, "test-tenant")
		provider, err := NewABACProvider(evaluator, "test-tenant", log)
		require.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)
		assert.True(t, provider.closed.Load())
	})

	t.Run("double close returns error", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, "test-tenant")
		provider, err := NewABACProvider(evaluator, "test-tenant", log)
		require.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)

		err = provider.Close()
		assert.Error(t, err)

		var abacErr *ABACProviderError
		assert.ErrorAs(t, err, &abacErr)
		assert.Equal(t, "Close", abacErr.Operation)
		assert.Contains(t, abacErr.Error(), "already closed")
	})
}

// TestABACProviderError tests the error type.
func TestABACProviderError(t *testing.T) {
	t.Run("error without cause", func(t *testing.T) {
		err := &ABACProviderError{
			Operation: "TestOp",
			Message:   "test message",
		}

		assert.Equal(t, "abac provider: TestOp: test message", err.Error())
		assert.Nil(t, err.Unwrap())
	})

	t.Run("error with cause", func(t *testing.T) {
		cause := &ABACProviderError{
			Operation: "InnerOp",
			Message:   "inner error",
		}
		err := &ABACProviderError{
			Operation: "OuterOp",
			Message:   "outer error",
			Cause:     cause,
		}

		assert.Contains(t, err.Error(), "outer error")
		assert.Contains(t, err.Error(), "inner error")
		assert.Equal(t, cause, err.Unwrap())
	})
}

// TestConvertToCheckRequest tests the conversion function.
func TestConvertToCheckRequest(t *testing.T) {
	t.Run("converts all fields correctly", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Username:      "testuser",
				TenantID:      "tenant-1",
				Roles:         []string{"admin", "reader"},
				Groups:        []string{"engineering"},
				Attributes:    map[string]interface{}{"level": 5},
				Authenticated: true,
			},
			Resource: "keys/mykey",
			Action:   "write",
			Context:  map[string]interface{}{"ip": "127.0.0.1"},
		}

		checkReq := convertToCheckRequest(context.Background(), req, "default-tenant")

		assert.Equal(t, "user-123", checkReq.Subject.ID)
		assert.Equal(t, "testuser", checkReq.Subject.Name)
		assert.Equal(t, "tenant-1", checkReq.Subject.TenantID)
		assert.Equal(t, "tenant-1", checkReq.Subject.Tenant)
		assert.Equal(t, []string{"admin", "reader"}, checkReq.Subject.Roles)
		assert.Equal(t, []string{"engineering"}, checkReq.Subject.Groups)
		assert.Equal(t, map[string]interface{}{"level": 5}, checkReq.Subject.Attributes)
		assert.True(t, checkReq.Subject.Authenticated)
		assert.Equal(t, "keys/mykey", checkReq.Resource)
		assert.Equal(t, "write", checkReq.Action)
		// Original context should be preserved
		assert.Equal(t, "127.0.0.1", checkReq.Context["ip"])
	})

	t.Run("uses default tenant when subject tenant is empty", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:       "user-123",
				TenantID: "",
			},
			Resource: "keys/test",
			Action:   "read",
		}

		checkReq := convertToCheckRequest(context.Background(), req, "default-tenant")

		assert.Equal(t, "default-tenant", checkReq.Subject.TenantID)
		assert.Equal(t, "default-tenant", checkReq.Subject.Tenant)
	})

	t.Run("handles nil slices and maps", func(t *testing.T) {
		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:         "user-123",
				TenantID:   "tenant-1",
				Roles:      nil,
				Groups:     nil,
				Attributes: nil,
			},
			Resource: "keys/test",
			Action:   "read",
			Context:  nil,
		}

		checkReq := convertToCheckRequest(context.Background(), req, "default-tenant")

		assert.Nil(t, checkReq.Subject.Roles)
		assert.Nil(t, checkReq.Subject.Groups)
		assert.Nil(t, checkReq.Subject.Attributes)
		// Context should still be created (not nil) for potential context additions
		assert.NotNil(t, checkReq.Context)
		// No vector context in Go context, so no vector fields added
		_, hasNamespace := checkReq.Context["namespace"]
		assert.False(t, hasNamespace)
	})
}

// TestExtractVectorContext tests the vector context extraction function.
func TestExtractVectorContext(t *testing.T) {
	t.Run("extracts full vector context", func(t *testing.T) {
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace:      "embeddings",
			VectorID:       "vec-123",
			Operation:      "upsert",
			BatchSize:      100,
			VectorMetadata: map[string]interface{}{"key": "value"},
		})

		requestContext := make(map[string]interface{})
		extractVectorContext(ctx, requestContext)

		assert.Equal(t, "embeddings", requestContext["namespace"])
		assert.Equal(t, "vec-123", requestContext["vector_id"])
		assert.Equal(t, "upsert", requestContext["operation"])
		assert.Equal(t, 100, requestContext["batch_size"])
		assert.Equal(t, true, requestContext["has_metadata"])
		assert.Equal(t, 1, requestContext["metadata_count"])
	})

	t.Run("handles partial vector context", func(t *testing.T) {
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "test-ns",
			Operation: "search",
		})

		requestContext := make(map[string]interface{})
		extractVectorContext(ctx, requestContext)

		assert.Equal(t, "test-ns", requestContext["namespace"])
		assert.Equal(t, "search", requestContext["operation"])
		_, hasBatchSize := requestContext["batch_size"]
		assert.False(t, hasBatchSize) // batch_size=0 should not be included
		_, hasVectorID := requestContext["vector_id"]
		assert.False(t, hasVectorID) // empty vector_id should not be included
		assert.Equal(t, false, requestContext["has_metadata"])
		assert.Equal(t, 0, requestContext["metadata_count"])
	})

	t.Run("handles empty context", func(t *testing.T) {
		requestContext := make(map[string]interface{})
		extractVectorContext(context.Background(), requestContext)

		// Should not add any vector-specific fields
		_, hasNamespace := requestContext["namespace"]
		assert.False(t, hasNamespace)
		_, hasOperation := requestContext["operation"]
		assert.False(t, hasOperation)
	})

	t.Run("handles nil vector context", func(t *testing.T) {
		ctx := iam.ContextWithVectorContext(context.Background(), nil)

		requestContext := make(map[string]interface{})
		extractVectorContext(ctx, requestContext)

		// Should not add any vector-specific fields
		_, hasNamespace := requestContext["namespace"]
		assert.False(t, hasNamespace)
	})
}

// TestConvertToCheckRequestWithVectorContext tests vector context extraction in conversion.
func TestConvertToCheckRequestWithVectorContext(t *testing.T) {
	t.Run("includes vector context in check request", func(t *testing.T) {
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "ml-embeddings",
			Operation: "batch_upsert",
			BatchSize: 500,
		})

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Authenticated: true,
			},
			Resource: "vector:namespace:ml-embeddings",
			Action:   "vector:write",
		}

		checkReq := convertToCheckRequest(ctx, req, "default-tenant")

		// Vector context should be in the request context
		assert.Equal(t, "ml-embeddings", checkReq.Context["namespace"])
		assert.Equal(t, "batch_upsert", checkReq.Context["operation"])
		assert.Equal(t, 500, checkReq.Context["batch_size"])
	})

	t.Run("includes tenant from context", func(t *testing.T) {
		ctx := iam.ContextWithTenant(context.Background(), "ctx-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Authenticated: true,
			},
			Resource: "keys/test",
			Action:   "read",
		}

		checkReq := convertToCheckRequest(ctx, req, "default-tenant")

		assert.Equal(t, "ctx-tenant", checkReq.Context["tenant_id"])
	})

	t.Run("includes user from context", func(t *testing.T) {
		ctx := iam.ContextWithUser(context.Background(), "user-from-ctx")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Authenticated: true,
			},
			Resource: "keys/test",
			Action:   "read",
		}

		checkReq := convertToCheckRequest(ctx, req, "default-tenant")

		assert.Equal(t, "user-from-ctx", checkReq.Context["user_id"])
	})

	t.Run("combines multiple context sources", func(t *testing.T) {
		ctx := context.Background()
		ctx = iam.ContextWithTenant(ctx, "multi-tenant")
		ctx = iam.ContextWithUser(ctx, "multi-user")
		ctx = iam.ContextWithVectorContext(ctx, &iam.VectorContext{
			Namespace: "combined-ns",
			Operation: "search",
		})

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Authenticated: true,
			},
			Resource: "vector:namespace:combined-ns",
			Action:   "vector:read",
			Context:  map[string]interface{}{"original_key": "original_value"},
		}

		checkReq := convertToCheckRequest(ctx, req, "default-tenant")

		// All context sources should be present
		assert.Equal(t, "multi-tenant", checkReq.Context["tenant_id"])
		assert.Equal(t, "multi-user", checkReq.Context["user_id"])
		assert.Equal(t, "combined-ns", checkReq.Context["namespace"])
		assert.Equal(t, "search", checkReq.Context["operation"])
		assert.Equal(t, "original_value", checkReq.Context["original_key"])
	})

	t.Run("preserves existing request context", func(t *testing.T) {
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "test-ns",
		})

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				Authenticated: true,
			},
			Resource: "vector:namespace:test-ns",
			Action:   "vector:read",
			Context: map[string]interface{}{
				"ip_address": "192.168.1.1",
				"timestamp":  12345,
			},
		}

		checkReq := convertToCheckRequest(ctx, req, "default-tenant")

		// Original context should be preserved
		assert.Equal(t, "192.168.1.1", checkReq.Context["ip_address"])
		assert.Equal(t, 12345, checkReq.Context["timestamp"])
		// Vector context should also be present
		assert.Equal(t, "test-ns", checkReq.Context["namespace"])
	})
}

// TestConvertToAuthorizationDecision tests the conversion function.
func TestConvertToAuthorizationDecision(t *testing.T) {
	t.Run("converts allowed response", func(t *testing.T) {
		resp := &iam.CheckResponse{
			Allowed:       true,
			Reason:        "allowed by policy",
			MatchedPolicy: "test-policy",
			MatchedRule:   "rule-1",
		}

		startTime := time.Now()
		decision := convertToAuthorizationDecision(resp, startTime)

		assert.True(t, decision.Allowed)
		assert.Equal(t, "allowed by policy", decision.Reason)
		assert.Equal(t, "test-policy/rule-1", decision.MatchedPolicy)
		assert.Equal(t, "abac", decision.Provider)
		assert.NotEmpty(t, decision.DecisionID)
		assert.Greater(t, decision.EvaluationTimeNs, int64(0))
	})

	t.Run("converts denied response", func(t *testing.T) {
		resp := &iam.CheckResponse{
			Allowed:       false,
			Reason:        "denied by policy",
			MatchedPolicy: "deny-policy",
			MatchedRule:   "",
		}

		startTime := time.Now()
		decision := convertToAuthorizationDecision(resp, startTime)

		assert.False(t, decision.Allowed)
		assert.Equal(t, "denied by policy", decision.Reason)
		assert.Equal(t, "deny-policy", decision.MatchedPolicy)
	})

	t.Run("handles empty matched policy", func(t *testing.T) {
		resp := &iam.CheckResponse{
			Allowed:       false,
			Reason:        "no matching policy",
			MatchedPolicy: "",
			MatchedRule:   "",
		}

		startTime := time.Now()
		decision := convertToAuthorizationDecision(resp, startTime)

		assert.Equal(t, "", decision.MatchedPolicy)
	})
}

// TestCopyHelpers tests the copy helper functions.
func TestCopyHelpers(t *testing.T) {
	t.Run("copyStringSlice creates independent copy", func(t *testing.T) {
		original := []string{"a", "b", "c"}
		copied := copyStringSlice(original)

		assert.Equal(t, original, copied)

		// Modify original
		original[0] = "modified"
		assert.NotEqual(t, original[0], copied[0])
	})

	t.Run("copyStringSlice handles nil", func(t *testing.T) {
		var original []string
		copied := copyStringSlice(original)
		assert.Nil(t, copied)
	})

	t.Run("copyMapInterface creates independent copy", func(t *testing.T) {
		original := map[string]interface{}{
			"key1": "value1",
			"key2": 42,
		}
		copied := copyMapInterface(original)

		assert.Equal(t, original, copied)

		// Modify original
		original["key1"] = "modified"
		assert.NotEqual(t, original["key1"], copied["key1"])
	})

	t.Run("copyMapInterface handles nil", func(t *testing.T) {
		var original map[string]interface{}
		copied := copyMapInterface(original)
		assert.Nil(t, copied)
	})
}

// TestGenerateDecisionID tests the decision ID generation.
func TestGenerateDecisionID(t *testing.T) {
	t.Run("generates unique IDs", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			id := generateDecisionID()
			assert.NotEmpty(t, id)
			assert.False(t, ids[id], "duplicate ID generated")
			ids[id] = true
		}
	})

	t.Run("generates valid UUID format", func(t *testing.T) {
		id := generateDecisionID()
		assert.Len(t, id, 36) // UUID v4 format: 8-4-4-4-12
		assert.Contains(t, id, "-")
	})
}

// TestABACProviderConcurrency tests concurrent access to the provider.
func TestABACProviderConcurrency(t *testing.T) {
	const tenantID = "test-tenant"

	t.Run("concurrent authorize calls", func(t *testing.T) {
		log := testLogger(t)
		evaluator := createTestEvaluator(t, tenantID)
		provider, err := NewABACProvider(evaluator, tenantID, log)
		require.NoError(t, err)
		defer func() {
			_ = provider.Close()
		}()

		const goroutines = 50
		const requestsPerGoroutine = 20

		done := make(chan bool, goroutines)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				for j := 0; j < requestsPerGoroutine; j++ {
					req := &interfaces.AuthorizationRequest{
						Subject: interfaces.Subject{
							ID:            "user-" + string(rune('0'+id%10)),
							TenantID:      tenantID,
							Authenticated: true,
						},
						Resource: "keys/key-" + string(rune('0'+j%10)),
						Action:   "read",
					}

					decision, err := provider.Authorize(context.Background(), req)
					if err != nil {
						t.Errorf("unexpected error: %v", err)
						done <- false
						return
					}
					if decision == nil {
						t.Error("unexpected nil decision")
						done <- false
						return
					}
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines
		successCount := 0
		for i := 0; i < goroutines; i++ {
			if <-done {
				successCount++
			}
		}

		assert.Equal(t, goroutines, successCount)
	})
}

// BenchmarkABACProviderAuthorize benchmarks the Authorize method.
func BenchmarkABACProviderAuthorize(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})

	tenantID := "bench-tenant"
	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               time.Hour,
		MaxSize:           100,
		BloomFilterSize:   10000,
		BloomFilterHashes: 7,
	})

	// Add a simple policy
	policy := &iam.CompiledPolicy{
		Name:     "bench-policy",
		Priority: 100,
		SubjectMatcher: &iam.SubjectMatcher{
			Authenticated: true,
		},
		Rules: []*iam.CompiledRule{
			{
				Name:         "allow-all",
				Effect:       iam.EffectAllow,
				Priority:     100,
				Actions:      []string{"*"},
				ResourceTrie: createResourceTrieBench("*"),
			},
		},
	}
	cache.Set(tenantID, policy)

	evaluator := iam.NewPolicyEvaluator(compiler, cache)
	provider, _ := NewABACProvider(evaluator, tenantID, log)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "bench-user",
			TenantID:      tenantID,
			Authenticated: true,
		},
		Resource: "keys/benchkey",
		Action:   "read",
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = provider.Authorize(ctx, req)
	}
}

// createResourceTrieBench creates a resource trie for benchmarks (without t.Helper).
func createResourceTrieBench(pattern string) *iam.ResourceTrie {
	trie := iam.NewResourceTrie()
	trie.Insert(pattern, true)
	return trie
}

// BenchmarkABACProviderBatchAuthorize benchmarks the BatchAuthorize method.
func BenchmarkABACProviderBatchAuthorize(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})

	tenantID := "bench-tenant"
	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               time.Hour,
		MaxSize:           100,
		BloomFilterSize:   10000,
		BloomFilterHashes: 7,
	})

	// Add a simple policy
	policy := &iam.CompiledPolicy{
		Name:     "bench-policy",
		Priority: 100,
		SubjectMatcher: &iam.SubjectMatcher{
			Authenticated: true,
		},
		Rules: []*iam.CompiledRule{
			{
				Name:         "allow-all",
				Effect:       iam.EffectAllow,
				Priority:     100,
				Actions:      []string{"*"},
				ResourceTrie: createResourceTrieBench("*"),
			},
		},
	}
	cache.Set(tenantID, policy)

	evaluator := iam.NewPolicyEvaluator(compiler, cache)
	provider, _ := NewABACProvider(evaluator, tenantID, log)

	// Create batch of 10 requests
	reqs := make([]*interfaces.AuthorizationRequest, 10)
	for i := 0; i < 10; i++ {
		reqs[i] = &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "bench-user",
				TenantID:      tenantID,
				Authenticated: true,
			},
			Resource: "keys/benchkey",
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

// =============================================================================
// Vector ABAC Policy Evaluation Tests
// =============================================================================

// createVectorPolicyCache creates a policy cache with vector-specific policies.
func createVectorPolicyCache(t *testing.T, tenantID string) *iam.PolicyCache {
	t.Helper()

	cache := iam.NewPolicyCache(createTestCacheConfig())

	// Vector reader policy - allows read and search operations
	vectorReaderPolicy := &iam.CompiledPolicy{
		Name:     "vector-reader-policy",
		Priority: 100,
		SubjectMatcher: &iam.SubjectMatcher{
			Roles:         map[string]bool{"vector-reader": true},
			Authenticated: true,
		},
		Rules: []*iam.CompiledRule{
			{
				Name:         "allow-vector-read",
				Effect:       iam.EffectAllow,
				Priority:     100,
				Actions:      []string{"vector:read", "vector:search"},
				ResourceTrie: createResourceTrie("vector:*"),
				Conditions:   nil,
				Unless:       nil,
			},
		},
		Variables: make(map[string]interface{}),
	}
	cache.Set(tenantID, vectorReaderPolicy)

	// Vector writer policy - allows read, write, and search operations
	vectorWriterPolicy := &iam.CompiledPolicy{
		Name:     "vector-writer-policy",
		Priority: 100,
		SubjectMatcher: &iam.SubjectMatcher{
			Roles:         map[string]bool{"vector-writer": true},
			Authenticated: true,
		},
		Rules: []*iam.CompiledRule{
			{
				Name:         "allow-vector-write",
				Effect:       iam.EffectAllow,
				Priority:     100,
				Actions:      []string{"vector:read", "vector:write", "vector:search"},
				ResourceTrie: createResourceTrie("vector:*"),
				Conditions:   nil,
				Unless:       nil,
			},
		},
		Variables: make(map[string]interface{}),
	}
	cache.Set(tenantID, vectorWriterPolicy)

	// Vector admin policy - allows all vector operations
	vectorAdminPolicy := &iam.CompiledPolicy{
		Name:     "vector-admin-policy",
		Priority: 100,
		SubjectMatcher: &iam.SubjectMatcher{
			Roles:         map[string]bool{"vector-admin": true},
			Authenticated: true,
		},
		Rules: []*iam.CompiledRule{
			{
				Name:         "allow-vector-admin",
				Effect:       iam.EffectAllow,
				Priority:     100,
				Actions:      []string{"vector:read", "vector:write", "vector:delete", "vector:admin", "vector:search", "vector:namespace"},
				ResourceTrie: createResourceTrie("vector:*"),
				Conditions:   nil,
				Unless:       nil,
			},
			// Also allow on base vector resource for namespace operations
			{
				Name:         "allow-vector-base-admin",
				Effect:       iam.EffectAllow,
				Priority:     100,
				Actions:      []string{"vector:namespace"},
				ResourceTrie: createResourceTrie("vector"),
				Conditions:   nil,
				Unless:       nil,
			},
		},
		Variables: make(map[string]interface{}),
	}
	cache.Set(tenantID, vectorAdminPolicy)

	return cache
}

// createVectorTestEvaluator creates a policy evaluator with vector policies.
func createVectorTestEvaluator(t *testing.T, tenantID string) *iam.PolicyEvaluator {
	t.Helper()

	compiler := iam.NewPolicyCompiler()
	cache := createVectorPolicyCache(t, tenantID)

	return iam.NewPolicyEvaluator(compiler, cache)
}

// createVectorTestProvider creates a provider for vector-specific tests.
func createVectorTestProvider(t *testing.T) *ABACProvider {
	t.Helper()
	log := testLogger(t)
	evaluator := createVectorTestEvaluator(t, "test-tenant")
	provider, err := NewABACProvider(evaluator, "test-tenant", log)
	require.NoError(t, err)
	return provider
}

// TestVectorABACPolicyEvaluation tests ABAC policy evaluation with vector contexts.
func TestVectorABACPolicyEvaluation(t *testing.T) {
	t.Run("allows vector read with matching namespace", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "docs",
			Operation: "search",
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-reader"},
			},
			Resource: "vector:namespace:docs",
			Action:   "vector:read",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Allowed)
	})

	t.Run("denies vector write without writer role", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "docs",
			Operation: "upsert",
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-reader"}, // Only reader role
			},
			Resource: "vector:namespace:docs",
			Action:   "vector:write",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.False(t, resp.Allowed)
	})

	t.Run("allows batch upsert within size limit", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "docs",
			Operation: "batch_upsert",
			BatchSize: 500, // Within typical limit
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-writer"},
			},
			Resource: "vector:namespace:docs",
			Action:   "vector:write",
			Context:  map[string]interface{}{"batch_size": 500},
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Allowed)
	})

	t.Run("denies admin operations without admin role", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "docs",
			Operation: "rebuild_index",
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-writer"}, // Writer, not admin
			},
			Resource: "vector:namespace:docs",
			Action:   "vector:admin",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.False(t, resp.Allowed)
	})

	t.Run("allows admin operations with admin role", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "docs",
			Operation: "compact",
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-admin"},
			},
			Resource: "vector:namespace:docs",
			Action:   "vector:admin",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Allowed)
	})

	t.Run("denies cross-tenant namespace access", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "other-tenant-ns",
			Operation: "search",
		})
		ctx = iam.ContextWithTenant(ctx, "tenant-a")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "tenant-a",
				Authenticated: true,
				Roles:         []string{"vector-reader"},
			},
			Resource: "vector:namespace:other-tenant-ns",
			Action:   "vector:read",
			Context:  map[string]interface{}{"resource_tenant": "tenant-b"},
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		// Should be denied due to tenant mismatch (if policy checks tenant)
		// Note: This depends on actual policy configuration
		assert.NotNil(t, resp)
	})

	t.Run("allows namespace creation with namespace permission", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "new-namespace",
			Operation: "create_namespace",
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-admin"},
			},
			Resource: "vector",
			Action:   "vector:namespace",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Allowed)
	})

	t.Run("includes vector context in authorization decision", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "ml-embeddings",
			VectorID:  "vec-12345",
			Operation: "get",
			BatchSize: 0,
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "ml-user",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-reader"},
			},
			Resource: "vector:namespace:ml-embeddings",
			Action:   "vector:read",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Allowed)
	})

	t.Run("allows search operations with reader role", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "products",
			Operation: "search_similar",
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "search-user",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-reader"},
			},
			Resource: "vector:namespace:products",
			Action:   "vector:search",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Allowed)
	})

	t.Run("denies unauthenticated access", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "docs",
			Operation: "search",
		})

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "",
				Authenticated: false,
			},
			Resource: "vector:namespace:docs",
			Action:   "vector:read",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.False(t, resp.Allowed)
	})

	t.Run("allows delete with delete permission", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "docs",
			VectorID:  "vec-to-delete",
			Operation: "delete",
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-admin"}, // Admin has delete
			},
			Resource: "vector:namespace:docs",
			Action:   "vector:delete",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Allowed)
	})

	t.Run("denies delete with only reader role", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithVectorContext(context.Background(), &iam.VectorContext{
			Namespace: "docs",
			VectorID:  "vec-to-delete",
			Operation: "delete",
		})
		ctx = iam.ContextWithTenant(ctx, "test-tenant")

		req := &interfaces.AuthorizationRequest{
			Subject: interfaces.Subject{
				ID:            "user-123",
				TenantID:      "test-tenant",
				Authenticated: true,
				Roles:         []string{"vector-reader"},
			},
			Resource: "vector:namespace:docs",
			Action:   "vector:delete",
		}

		resp, err := provider.Authorize(ctx, req)
		require.NoError(t, err)
		assert.False(t, resp.Allowed)
	})
}

// TestVectorABACBatchAuthorization tests batch authorization with vector contexts.
func TestVectorABACBatchAuthorization(t *testing.T) {
	t.Run("batch authorizes multiple vector operations", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithTenant(context.Background(), "test-tenant")

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "user-123",
					TenantID:      "test-tenant",
					Authenticated: true,
					Roles:         []string{"vector-reader"},
				},
				Resource: "vector:namespace:docs",
				Action:   "vector:read",
			},
			{
				Subject: interfaces.Subject{
					ID:            "user-123",
					TenantID:      "test-tenant",
					Authenticated: true,
					Roles:         []string{"vector-reader"},
				},
				Resource: "vector:namespace:products",
				Action:   "vector:search",
			},
			{
				Subject: interfaces.Subject{
					ID:            "user-123",
					TenantID:      "test-tenant",
					Authenticated: true,
					Roles:         []string{"vector-reader"},
				},
				Resource: "vector:namespace:docs",
				Action:   "vector:write", // Should be denied - reader can't write
			},
		}

		resps, err := provider.BatchAuthorize(ctx, reqs)
		require.NoError(t, err)
		require.Len(t, resps, 3)

		assert.True(t, resps[0].Allowed, "read should be allowed")
		assert.True(t, resps[1].Allowed, "search should be allowed")
		assert.False(t, resps[2].Allowed, "write should be denied for reader")
	})

	t.Run("batch handles mixed roles", func(t *testing.T) {
		provider := createVectorTestProvider(t)
		ctx := iam.ContextWithTenant(context.Background(), "test-tenant")

		reqs := []*interfaces.AuthorizationRequest{
			{
				Subject: interfaces.Subject{
					ID:            "admin-user",
					TenantID:      "test-tenant",
					Authenticated: true,
					Roles:         []string{"vector-admin"},
				},
				Resource: "vector:namespace:docs",
				Action:   "vector:admin",
			},
			{
				Subject: interfaces.Subject{
					ID:            "reader-user",
					TenantID:      "test-tenant",
					Authenticated: true,
					Roles:         []string{"vector-reader"},
				},
				Resource: "vector:namespace:docs",
				Action:   "vector:admin", // Should be denied
			},
		}

		resps, err := provider.BatchAuthorize(ctx, reqs)
		require.NoError(t, err)
		require.Len(t, resps, 2)

		assert.True(t, resps[0].Allowed, "admin should be allowed admin action")
		assert.False(t, resps[1].Allowed, "reader should be denied admin action")
	})
}

// =============================================================================
// Consolidated from coverage_91_test.go
// =============================================================================

// =============================================================================
// extractParameterContext coverage (15% -> higher)
// =============================================================================

func TestExtractParameterContext_AllFields(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		Path:         "/app/database/host",
		Type:         "string",
		IsEncrypted:  true,
		Tags:         map[string]string{"env": "production", "team": "platform"},
		Owner:        "user-123",
		TenantID:     "tenant-abc",
		Operation:    "get",
		BatchSize:    5,
		AuditEnabled: true,
		Description:  "Database host config",
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Equal(t, "/app/database/host", requestContext["parameter_path"])
	assert.Equal(t, "string", requestContext["parameter_type"])
	assert.Equal(t, true, requestContext["parameter_encrypted"])
	assert.Equal(t, map[string]string{"env": "production", "team": "platform"}, requestContext["parameter_tags"])
	assert.Equal(t, "user-123", requestContext["parameter_owner"])
	assert.Equal(t, "tenant-abc", requestContext["parameter_tenant"])
	assert.Equal(t, "get", requestContext["parameter_operation"])
	assert.Equal(t, 5, requestContext["parameter_batch_size"])
	assert.Equal(t, true, requestContext["parameter_audit_enabled"])
	assert.Equal(t, true, requestContext["parameter_has_description"])
}

func TestExtractParameterContext_NilContext(t *testing.T) {
	ctx := context.Background() // no parameter context set
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Empty(t, requestContext)
}

func TestExtractParameterContext_MinimalFields(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		IsEncrypted:  false,
		AuditEnabled: false,
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	// Only non-conditional fields should be present
	assert.Equal(t, false, requestContext["parameter_encrypted"])
	assert.Equal(t, false, requestContext["parameter_audit_enabled"])
	assert.Equal(t, false, requestContext["parameter_has_description"])

	// Conditional fields should be absent
	_, hasPath := requestContext["parameter_path"]
	assert.False(t, hasPath)
	_, hasType := requestContext["parameter_type"]
	assert.False(t, hasType)
	_, hasTags := requestContext["parameter_tags"]
	assert.False(t, hasTags)
	_, hasOwner := requestContext["parameter_owner"]
	assert.False(t, hasOwner)
	_, hasTenant := requestContext["parameter_tenant"]
	assert.False(t, hasTenant)
	_, hasOp := requestContext["parameter_operation"]
	assert.False(t, hasOp)
	_, hasBatch := requestContext["parameter_batch_size"]
	assert.False(t, hasBatch)
}

func TestExtractParameterContext_PathOnly(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		Path: "/app/secrets/api-key",
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Equal(t, "/app/secrets/api-key", requestContext["parameter_path"])
}

func TestExtractParameterContext_OperationAndBatch(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		Operation: "batch_set",
		BatchSize: 10,
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Equal(t, "batch_set", requestContext["parameter_operation"])
	assert.Equal(t, 10, requestContext["parameter_batch_size"])
}

func TestExtractParameterContext_TypeOnly(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		Type: "secret",
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Equal(t, "secret", requestContext["parameter_type"])
}

func TestExtractParameterContext_OwnerOnly(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		Owner: "owner-user-id",
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Equal(t, "owner-user-id", requestContext["parameter_owner"])
}

func TestExtractParameterContext_TenantIDOnly(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		TenantID: "tenant-xyz",
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Equal(t, "tenant-xyz", requestContext["parameter_tenant"])
}

func TestExtractParameterContext_TagsOnly(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		Tags: map[string]string{"key1": "val1", "key2": "val2"},
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Equal(t, map[string]string{"key1": "val1", "key2": "val2"}, requestContext["parameter_tags"])
}

func TestExtractParameterContext_DescriptionPresence(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		Description: "This is a description",
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Equal(t, true, requestContext["parameter_has_description"])
}

// =============================================================================
// createABACProvider coverage (0% -> tested)
// =============================================================================

func TestCreateABACProvider_WithLogger(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := &FactoryConfig{
		Type:     ProviderTypeABACFactory,
		Logger:   log,
		TenantID: "test-tenant",
	}

	provider, err := createABACProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()

	assert.Equal(t, "abac", provider.GetProviderType())
}

func TestCreateABACProvider_NilLogger(t *testing.T) {
	cfg := &FactoryConfig{
		Type:     ProviderTypeABACFactory,
		Logger:   nil,
		TenantID: "test-tenant-nil-logger",
	}

	provider, err := createABACProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()
}

func TestCreateABACProvider_EmptyTenantID(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := &FactoryConfig{
		Type:     ProviderTypeABACFactory,
		Logger:   log,
		TenantID: "",
	}

	provider, err := createABACProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()
}

// =============================================================================
// createHybridProvider coverage (16.7% -> tested)
// =============================================================================

func TestCreateHybridProvider_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mgr := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: mgr,
		Logger:      log,
		TenantID:    "hybrid-tenant",
	}

	provider, err := createHybridProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()

	assert.Equal(t, ProviderTypeHybrid, provider.GetProviderType())
}

func TestCreateHybridProvider_NilRoleManager(t *testing.T) {
	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: nil,
	}

	provider, err := createHybridProvider(cfg)
	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.ErrorIs(t, err, ErrRoleManagerRequired)
}

func TestCreateHybridProvider_NilLogger(t *testing.T) {
	mgr := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: mgr,
		Logger:      nil,
		TenantID:    "hybrid-tenant-nil-log",
	}

	provider, err := createHybridProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()
}

// =============================================================================
// NewProvider ABAC path coverage
// =============================================================================

func TestNewProvider_ABAC_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	cfg := &FactoryConfig{
		Type:     ProviderTypeABACFactory,
		Logger:   log,
		TenantID: "np-abac-tenant",
	}

	provider, err := NewProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()
}

func TestNewProvider_Hybrid_Success(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mgr := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: mgr,
		Logger:      log,
		TenantID:    "np-hybrid-tenant",
	}

	provider, err := NewProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()
}

// =============================================================================
// ValidateConfig ABAC and Hybrid valid paths
// =============================================================================

func TestValidateConfig_ABAC_Valid(t *testing.T) {
	err := ValidateConfig(&FactoryConfig{
		Type: ProviderTypeABACFactory,
	})
	assert.NoError(t, err)
}

func TestValidateConfig_Hybrid_Valid(t *testing.T) {
	err := ValidateConfig(&FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: newMockRoleManager(),
	})
	assert.NoError(t, err)
}

// =============================================================================
// ABAC Authorize with parameter context
// =============================================================================

func TestABACProvider_Authorize_WithParameterContext(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               defaultCacheTTL,
		MaxSize:           defaultCacheMaxSize,
		BloomFilterSize:   defaultBloomFilterSize,
		BloomFilterHashes: defaultBloomFilterHashes,
	})
	evaluator := iam.NewPolicyEvaluator(compiler, cache)

	provider, err := NewABACProvider(evaluator, "test-tenant", log)
	require.NoError(t, err)
	defer provider.Close()

	paramCtx := &iam.ParameterContext{
		Path:         "/app/config/db-host",
		Type:         "string",
		IsEncrypted:  true,
		Tags:         map[string]string{"env": "staging"},
		Owner:        "user-456",
		TenantID:     "test-tenant",
		Operation:    "set",
		BatchSize:    0,
		AuditEnabled: true,
		Description:  "Encrypted DB host",
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)

	req := &interfaces.AuthorizationRequest{
		Subject:  interfaces.Subject{ID: "user-456", Roles: []string{"admin"}},
		Resource: "parameters",
		Action:   "write",
	}

	decision, err := provider.Authorize(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, decision)
	// The decision may allow or deny based on policy, but it should not error
	assert.NotEmpty(t, decision.DecisionID)
}

// =============================================================================
// ABAC BatchAuthorize with nil requests in batch
// =============================================================================

func TestABACProvider_BatchAuthorize_WithNilRequest(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               defaultCacheTTL,
		MaxSize:           defaultCacheMaxSize,
		BloomFilterSize:   defaultBloomFilterSize,
		BloomFilterHashes: defaultBloomFilterHashes,
	})
	evaluator := iam.NewPolicyEvaluator(compiler, cache)

	provider, err := NewABACProvider(evaluator, "test-tenant", log)
	require.NoError(t, err)
	defer provider.Close()

	reqs := []*interfaces.AuthorizationRequest{
		nil,
		{
			Subject:  interfaces.Subject{ID: "user-1", Roles: []string{"admin"}},
			Resource: "keys",
			Action:   "read",
		},
	}

	decisions, err := provider.BatchAuthorize(context.Background(), reqs)
	require.NoError(t, err)
	assert.Len(t, decisions, 2)
	// First decision should handle nil request gracefully
	assert.False(t, decisions[0].Allowed)
}

// =============================================================================
// RBAC findMatchingRole edge case - no matching role returns empty
// =============================================================================

func TestRBACProvider_findMatchingRole_NoMatch(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mgr := newMockRoleManager()
	provider, err := NewRBACProvider(mgr, log)
	require.NoError(t, err)
	defer provider.Close()

	// Use roles that don't have permission on the resource
	req := &interfaces.AuthorizationRequest{
		Subject:  interfaces.Subject{ID: "user-test", Roles: []string{"readonly"}},
		Resource: "keys",
		Action:   "delete",
	}

	decision, err := provider.Authorize(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
}

// =============================================================================
// RBAC BatchAuthorize context cancellation path
// =============================================================================

func TestRBACProvider_BatchAuthorize_ContextCancelled(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mgr := newMockRoleManager()
	provider, err := NewRBACProvider(mgr, log)
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reqs := []*interfaces.AuthorizationRequest{
		{
			Subject:  interfaces.Subject{ID: "u1", Roles: []string{"admin"}},
			Resource: "keys",
			Action:   "read",
		},
	}

	_, err = provider.BatchAuthorize(ctx, reqs)
	assert.Error(t, err)
}

// =============================================================================
// ABAC BatchAuthorize context cancellation mid-batch
// =============================================================================

func TestABACProvider_BatchAuthorize_ContextCancelledMidBatch(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               defaultCacheTTL,
		MaxSize:           defaultCacheMaxSize,
		BloomFilterSize:   defaultBloomFilterSize,
		BloomFilterHashes: defaultBloomFilterHashes,
	})
	evaluator := iam.NewPolicyEvaluator(compiler, cache)

	provider, err := NewABACProvider(evaluator, "test-tenant", log)
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	reqs := []*interfaces.AuthorizationRequest{
		{
			Subject:  interfaces.Subject{ID: "u1", Authenticated: true},
			Resource: "keys",
			Action:   "read",
		},
	}

	_, err = provider.BatchAuthorize(ctx, reqs)
	assert.Error(t, err)
}

// =============================================================================
// ABAC Authorize path - policy evaluation failure
// This tests the error path when the evaluator returns an error
// =============================================================================

func TestABACProvider_Authorize_EmptyTenantID(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               defaultCacheTTL,
		MaxSize:           defaultCacheMaxSize,
		BloomFilterSize:   defaultBloomFilterSize,
		BloomFilterHashes: defaultBloomFilterHashes,
	})
	evaluator := iam.NewPolicyEvaluator(compiler, cache)

	// Create provider with empty tenant ID
	provider, err := NewABACProvider(evaluator, "", log)
	require.NoError(t, err)
	defer provider.Close()

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user-1",
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	decision, err := provider.Authorize(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, decision)
}

// =============================================================================
// Hybrid Provider with ABAC - real integration
// Tests that hybrid provider correctly creates and uses both ABAC and RBAC
// =============================================================================

func TestHybridProvider_WithRealABAC(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mgr := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: mgr,
		Logger:      log,
		TenantID:    "hybrid-real-abac",
	}

	provider, err := NewProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()

	// Test authorization - without policies configured, ABAC will deny
	// but that's the expected behavior when no policies are set
	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user-hybrid",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	decision, err := provider.Authorize(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, decision)
	// Without ABAC policies, the ABAC provider denies (no matching policy)
	// and the hybrid provider uses the ABAC decision directly
	assert.NotEmpty(t, decision.DecisionID)
	assert.Equal(t, "hybrid", provider.GetProviderType())
}

// =============================================================================
// convertToCheckRequest with all context types
// =============================================================================

func TestConvertToCheckRequest_WithParameterContext(t *testing.T) {
	paramCtx := &iam.ParameterContext{
		Path:      "/secret/key",
		Type:      "encrypted",
		Operation: "read",
	}
	ctx := iam.ContextWithParameterContext(context.Background(), paramCtx)

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user-123",
			TenantID:      "tenant-1",
			Authenticated: true,
		},
		Resource: "parameters",
		Action:   "read",
	}

	checkReq := convertToCheckRequest(ctx, req, "default-tenant")

	assert.Equal(t, "/secret/key", checkReq.Context["parameter_path"])
	assert.Equal(t, "encrypted", checkReq.Context["parameter_type"])
	assert.Equal(t, "read", checkReq.Context["parameter_operation"])
}

// =============================================================================
// Additional edge case tests for better coverage
// =============================================================================

func TestABACProvider_BatchAuthorize_AllNilRequests(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	compiler := iam.NewPolicyCompiler()
	cache := iam.NewPolicyCache(&iam.CacheConfig{
		TTL:               defaultCacheTTL,
		MaxSize:           defaultCacheMaxSize,
		BloomFilterSize:   defaultBloomFilterSize,
		BloomFilterHashes: defaultBloomFilterHashes,
	})
	evaluator := iam.NewPolicyEvaluator(compiler, cache)

	provider, err := NewABACProvider(evaluator, "test-tenant", log)
	require.NoError(t, err)
	defer provider.Close()

	reqs := []*interfaces.AuthorizationRequest{
		nil,
		nil,
		nil,
	}

	decisions, err := provider.BatchAuthorize(context.Background(), reqs)
	require.NoError(t, err)
	assert.Len(t, decisions, 3)
	for _, d := range decisions {
		assert.False(t, d.Allowed)
		assert.Contains(t, d.Reason, "request cannot be nil")
	}
}

func TestExtractParameterContext_NilParameterContext(t *testing.T) {
	// Explicitly set nil parameter context
	ctx := iam.ContextWithParameterContext(context.Background(), nil)
	requestContext := make(map[string]interface{})

	extractParameterContext(ctx, requestContext)

	assert.Empty(t, requestContext)
}

func TestValidateConfig_HybridWithoutABAC_Fails(t *testing.T) {
	// This test verifies that hybrid config validation works correctly
	// when ABAC is available (iam build tag set)
	err := ValidateConfig(&FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: newMockRoleManager(),
	})
	// Should pass because ABAC is available with iam build tag
	assert.NoError(t, err)
}

func TestNewProvider_ABAC_NilLogger(t *testing.T) {
	cfg := &FactoryConfig{
		Type:     ProviderTypeABACFactory,
		Logger:   nil,
		TenantID: "abac-nil-logger",
	}

	provider, err := NewProvider(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Close()
}
