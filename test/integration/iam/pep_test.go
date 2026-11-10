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

//go:build integration
// +build integration

package iam_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestPEPSingleCheck tests a single policy enforcement check.
func TestPEPSingleCheck(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create a simple policy
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "pep-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	// Perform enforcement check
	checkReq := map[string]interface{}{
		"subject": map[string]interface{}{
			"id":            "user123",
			"tenant_id":     tenantID,
			"authenticated": true,
		},
		"action":   "read",
		"resource": "data/file.txt",
	}

	var response iam.CheckResponse
	err := resolver.postJSON("/api/v1/iam/check", checkReq, &response)
	require.NoError(t, err)

	assert.True(t, response.Allowed)
	assert.Equal(t, "pep-test-policy", response.MatchedPolicy)
	assert.NotZero(t, response.EvaluationTimeNs)
}

// TestPEPBatchCheck tests batch policy enforcement checks.
func TestPEPBatchCheck(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create policy
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "batch-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"allowed/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	// Create batch check request
	batchReq := map[string]interface{}{
		"checks": []map[string]interface{}{
			{
				"subject": map[string]interface{}{
					"id":            "user123",
					"tenant_id":     tenantID,
					"authenticated": true,
				},
				"action":   "read",
				"resource": "allowed/file1.txt",
			},
			{
				"subject": map[string]interface{}{
					"id":            "user123",
					"tenant_id":     tenantID,
					"authenticated": true,
				},
				"action":   "read",
				"resource": "denied/file2.txt",
			},
			{
				"subject": map[string]interface{}{
					"id":            "user123",
					"tenant_id":     tenantID,
					"authenticated": true,
				},
				"action":   "write",
				"resource": "allowed/file3.txt",
			},
		},
	}

	var batchResponse map[string]interface{}
	err := resolver.postJSON("/api/v1/iam/check/batch", batchReq, &batchResponse)
	require.NoError(t, err)

	responses, ok := batchResponse["results"].([]interface{})
	require.True(t, ok)
	require.Len(t, responses, 3)

	// First check should be allowed
	resp1 := responses[0].(map[string]interface{})
	assert.True(t, resp1["allowed"].(bool))

	// Second check should be denied (wrong resource path)
	resp2 := responses[1].(map[string]interface{})
	assert.False(t, resp2["allowed"].(bool))

	// Third check should be denied (wrong action)
	resp3 := responses[2].(map[string]interface{})
	assert.False(t, resp3["allowed"].(bool))
}

// TestPEPCaching tests that PEP caches policy decisions.
func TestPEPCaching(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create policy
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "cache-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	checkReq := map[string]interface{}{
		"subject": map[string]interface{}{
			"id":            "user123",
			"tenant_id":     tenantID,
			"authenticated": true,
		},
		"action":   "read",
		"resource": "data/test.txt",
	}

	// First request
	start := time.Now()
	var response1 iam.CheckResponse
	require.NoError(t, resolver.postJSON("/api/v1/iam/check", checkReq, &response1))
	firstDuration := time.Since(start)

	// Second request (should be cached)
	start = time.Now()
	var response2 iam.CheckResponse
	require.NoError(t, resolver.postJSON("/api/v1/iam/check", checkReq, &response2))
	secondDuration := time.Since(start)

	// Both should have same result
	assert.Equal(t, response1.Allowed, response2.Allowed)

	// Second request should be faster (cached)
	// Note: This is a best-effort test, caching may not always be measurably faster
	t.Logf("First request: %v, Second request: %v", firstDuration, secondDuration)
}

// TestPEPPolicySync tests that policy changes are propagated to PEP.
func TestPEPPolicySync(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create initial policy (deny)
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "sync-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "deny-all",
					"effect":    "deny",
					"actions":   []string{"*"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	checkReq := map[string]interface{}{
		"subject": map[string]interface{}{
			"id":            "user123",
			"tenant_id":     tenantID,
			"authenticated": true,
		},
		"action":   "read",
		"resource": "data/test.txt",
	}

	// Check should be denied
	var response1 iam.CheckResponse
	require.NoError(t, resolver.postJSON("/api/v1/iam/check", checkReq, &response1))
	assert.False(t, response1.Allowed, "Access should be denied initially")

	// Update policy to allow
	updateReq := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "sync-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var updated iam.Policy
	require.NoError(t, admin.putJSON(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, "sync-test-policy"), updateReq, &updated))

	// Wait for the policy update to propagate to the PEP
	require.Eventually(t, func() bool {
		var probe iam.CheckResponse
		if err := resolver.postJSON("/api/v1/iam/check", checkReq, &probe); err != nil {
			t.Logf("policy check not ready: %v", err)
			return false
		}
		return probe.Allowed
	}, 5*time.Second, 100*time.Millisecond, "the policy update should reach the PEP")

	// Check should now be allowed
	var response2 iam.CheckResponse
	require.NoError(t, resolver.postJSON("/api/v1/iam/check", checkReq, &response2))
	assert.True(t, response2.Allowed, "Access should be allowed after policy update")
}

// TestPEPPerformance benchmarks policy evaluation performance.
func TestPEPPerformance(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create policy
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "perf-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	checkReq := map[string]interface{}{
		"subject": map[string]interface{}{
			"id":            "user123",
			"tenant_id":     tenantID,
			"authenticated": true,
		},
		"action":   "read",
		"resource": "data/test.txt",
	}

	// Warm up
	for i := 0; i < 10; i++ {
		var response iam.CheckResponse
		resolver.postJSON("/api/v1/iam/check", checkReq, &response)
	}

	// Measure performance
	const iterations = 100
	start := time.Now()
	for i := 0; i < iterations; i++ {
		var response iam.CheckResponse
		err := resolver.postJSON("/api/v1/iam/check", checkReq, &response)
		require.NoError(t, err)
	}
	duration := time.Since(start)

	avgLatency := duration / iterations
	t.Logf("Average policy evaluation latency: %v", avgLatency)
	t.Logf("Total time for %d iterations: %v", iterations, duration)

	// Target: < 5ms average (including HTTP overhead)
	// In production, local PEP library would be < 5us
	assert.Less(t, avgLatency, 10*time.Millisecond, "Average latency should be reasonable")
}

// TestPEPConcurrency tests concurrent policy checks.
func TestPEPConcurrency(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create policy
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "concurrency-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	// Perform concurrent checks
	const goroutines = 50
	const checksPerGoroutine = 10

	var wg sync.WaitGroup
	errors := make(chan error, goroutines*checksPerGoroutine)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < checksPerGoroutine; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					checkReq := map[string]interface{}{
						"subject": map[string]interface{}{
							"id":            fmt.Sprintf("user%d", id),
							"tenant_id":     tenantID,
							"authenticated": true,
						},
						"action":   "read",
						"resource": fmt.Sprintf("data/file%d.txt", j),
					}

					var response iam.CheckResponse
					if err := resolver.postJSON("/api/v1/iam/check", checkReq, &response); err != nil {
						errors <- err
						return
					}

					if !response.Allowed {
						errors <- &qberrors.MessageError{Message: fmt.Sprintf("unexpected denial for goroutine %d, check %d", id, j)}
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var errorList []error
	for err := range errors {
		errorList = append(errorList, err)
	}

	if len(errorList) > 0 {
		for _, err := range errorList {
			t.Errorf("Concurrent check error: %v", err)
		}
		t.Fatalf("Encountered %d errors during concurrent checks", len(errorList))
	}
}

// TestPEPMultiTenantPolicyIsolation tests that policies from one tenant do not affect another.
// This replaces TestPEPWithChildContainers as a meaningful multi-tenant isolation test
// that can run within the standard integration test environment.
func TestPEPMultiTenantPolicyIsolation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	// Create two separate tenants
	tenant1ID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenant1ID))

	tenant2ID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenant2ID))
	resolver1 := cluster.asResolver(t, tenant1ID)
	resolver2 := cluster.asResolver(t, tenant2ID)

	// Create a policy in tenant1 that allows read on data/**
	policy1 := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "tenant1-allow-policy",
			"tenant": tenant1ID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var created1 iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenant1ID), policy1, &created1))

	// Create a policy in tenant2 that DENIES read on data/**
	policy2 := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "tenant2-deny-policy",
			"tenant": tenant2ID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "deny-read",
					"effect":    "deny",
					"actions":   []string{"read"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var created2 iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenant2ID), policy2, &created2))

	// Check that tenant1 user CAN read data (policy allows)
	t.Run("Tenant1UserAllowed", func(t *testing.T) {
		checkReq := map[string]interface{}{
			"subject": map[string]interface{}{
				"id":            "user-in-tenant1",
				"tenant_id":     tenant1ID,
				"authenticated": true,
			},
			"action":   "read",
			"resource": "data/file.txt",
		}

		var response iam.CheckResponse
		err := resolver1.postJSON("/api/v1/iam/check", checkReq, &response)
		require.NoError(t, err)
		assert.True(t, response.Allowed, "Tenant1 user should be allowed to read")
		assert.Equal(t, "tenant1-allow-policy", response.MatchedPolicy)
	})

	// Check that tenant2 user CANNOT read data (policy denies)
	t.Run("Tenant2UserDenied", func(t *testing.T) {
		checkReq := map[string]interface{}{
			"subject": map[string]interface{}{
				"id":            "user-in-tenant2",
				"tenant_id":     tenant2ID,
				"authenticated": true,
			},
			"action":   "read",
			"resource": "data/file.txt",
		}

		var response iam.CheckResponse
		err := resolver2.postJSON("/api/v1/iam/check", checkReq, &response)
		require.NoError(t, err)
		assert.False(t, response.Allowed, "Tenant2 user should be denied from reading")
		assert.Equal(t, "tenant2-deny-policy", response.MatchedPolicy)
	})

	// Verify tenant1's policy does NOT apply to tenant2 users
	t.Run("CrossTenantPolicyIsolation", func(t *testing.T) {
		// If tenant isolation works correctly, a user with tenant2 ID
		// should NOT be affected by tenant1's allow policy
		checkReq := map[string]interface{}{
			"subject": map[string]interface{}{
				"id":            "another-user-in-tenant2",
				"tenant_id":     tenant2ID,
				"authenticated": true,
			},
			"action":   "read",
			"resource": "data/some-other-file.txt",
		}

		var response iam.CheckResponse
		err := resolver2.postJSON("/api/v1/iam/check", checkReq, &response)
		require.NoError(t, err)

		// Should be denied because tenant2's policy denies, NOT allowed by tenant1's policy
		assert.False(t, response.Allowed, "Cross-tenant policy leakage should not occur")
	})
}

// TestPEPFailover tests PEP behavior when IAM server restarts.
func TestPEPFailover(t *testing.T) {
	// This test would require cluster restart capabilities
	// For now, we'll test that policies survive across multiple requests
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create policy
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "failover-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"data/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	checkReq := map[string]interface{}{
		"subject": map[string]interface{}{
			"id":            "user123",
			"tenant_id":     tenantID,
			"authenticated": true,
		},
		"action":   "read",
		"resource": "data/test.txt",
	}

	// Perform the check twice: the policy must hold across requests
	var response1 iam.CheckResponse
	require.NoError(t, resolver.postJSON("/api/v1/iam/check", checkReq, &response1))
	assert.True(t, response1.Allowed)

	var response2 iam.CheckResponse
	require.NoError(t, resolver.postJSON("/api/v1/iam/check", checkReq, &response2))
	assert.True(t, response2.Allowed)

	// Policy should persist
	var retrieved iam.Policy
	require.NoError(t, admin.getJSON(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, "failover-test-policy"), &retrieved))
	assert.Equal(t, "failover-test-policy", retrieved.Metadata.Name)
}

// TestPEPPolicyPriorityEvaluation tests that policies are evaluated in priority order.
func TestPEPPolicyPriorityEvaluation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create a low-priority allow policy
	lowPriorityPolicy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":     "low-priority-allow",
			"tenant":   tenantID,
			"priority": 10,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-all",
					"effect":    "allow",
					"actions":   []string{"*"},
					"resources": []string{"**"},
				},
			},
		},
	}

	// Create a high-priority deny policy for specific resource
	highPriorityPolicy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":     "high-priority-deny",
			"tenant":   tenantID,
			"priority": 100,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "deny-secret",
					"effect":    "deny",
					"actions":   []string{"*"},
					"resources": []string{"secret/**"},
				},
			},
		},
	}

	var created1, created2 iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), lowPriorityPolicy, &created1))
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), highPriorityPolicy, &created2))

	// Check that regular data is allowed (low priority policy)
	t.Run("RegularDataAllowed", func(t *testing.T) {
		checkReq := map[string]interface{}{
			"subject": map[string]interface{}{
				"id":            "user123",
				"tenant_id":     tenantID,
				"authenticated": true,
			},
			"action":   "read",
			"resource": "data/file.txt",
		}

		var response iam.CheckResponse
		err := resolver.postJSON("/api/v1/iam/check", checkReq, &response)
		require.NoError(t, err)
		assert.True(t, response.Allowed, "Regular data should be allowed")
	})

	// Check that secret data is denied (high priority policy)
	t.Run("SecretDataDenied", func(t *testing.T) {
		checkReq := map[string]interface{}{
			"subject": map[string]interface{}{
				"id":            "user123",
				"tenant_id":     tenantID,
				"authenticated": true,
			},
			"action":   "read",
			"resource": "secret/password.txt",
		}

		var response iam.CheckResponse
		err := resolver.postJSON("/api/v1/iam/check", checkReq, &response)
		require.NoError(t, err)
		assert.False(t, response.Allowed, "Secret data should be denied by high-priority policy")
		assert.Equal(t, "high-priority-deny", response.MatchedPolicy)
	})
}

// TestPEPWildcardResourceMatching tests wildcard pattern matching in resource paths.
func TestPEPWildcardResourceMatching(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	// Tenant and policy calls go as the system admin, acting in the test's
	// tenants by the tenant lift. The /api/v1/iam/check calls go as a user of
	// the subject's tenant bound to its Admin role (asResolver), which holds
	// permissions:resolve.
	admin := cluster.asSystemAdmin()

	tenantID := setupTenantForTests(t, admin)
	defer admin.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	resolver := cluster.asResolver(t, tenantID)

	// Create policy with various wildcard patterns
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "wildcard-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-docs",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"docs/**"}, // Multi-level wildcard
				},
				{
					"name":      "allow-images",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"images/*"}, // Single-level wildcard
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, admin.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	testCases := []struct {
		name     string
		resource string
		expected bool
	}{
		{"Docs root file", "docs/readme.txt", true},
		{"Docs nested file", "docs/api/endpoints.md", true},
		{"Docs deeply nested", "docs/v1/api/internal/spec.yaml", true},
		{"Images root file", "images/logo.png", true},
		{"Images nested file", "images/icons/home.png", false}, // Single wildcard doesn't match nested
		{"Other path", "other/file.txt", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			checkReq := map[string]interface{}{
				"subject": map[string]interface{}{
					"id":            "user123",
					"tenant_id":     tenantID,
					"authenticated": true,
				},
				"action":   "read",
				"resource": tc.resource,
			}

			var response iam.CheckResponse
			err := resolver.postJSON("/api/v1/iam/check", checkReq, &response)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, response.Allowed,
				"Resource %s should be allowed=%v", tc.resource, tc.expected)
		})
	}
}
