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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestPolicyCRUD tests complete policy lifecycle.
func TestPolicyCRUD(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	policyName := "test-policy"

	// Create policy
	t.Run("Create", func(t *testing.T) {
		policy := map[string]interface{}{
			"apiVersion": "abac.quicbaas.io/v1",
			"kind":       "Policy",
			"metadata": map[string]interface{}{
				"name":        policyName,
				"tenant":      tenantID,
				"description": "Test policy for integration tests",
				"priority":    100,
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
						"resources": []string{"keys/users/**"},
					},
				},
			},
		}

		var created iam.Policy
		err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created)
		require.NoError(t, err)

		assert.Equal(t, policyName, created.Metadata.Name)
		assert.Equal(t, tenantID, created.Metadata.Tenant)
		assert.Len(t, created.Spec.Rules, 1)
		assert.Equal(t, iam.EffectAllow, created.Spec.Rules[0].Effect)
	})

	// Get policy
	t.Run("Get", func(t *testing.T) {
		var policy iam.Policy
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, policyName), &policy)
		require.NoError(t, err)

		assert.Equal(t, policyName, policy.Metadata.Name)
		assert.Equal(t, tenantID, policy.Metadata.Tenant)
	})

	// List policies
	t.Run("List", func(t *testing.T) {
		var resp struct {
			Policies []iam.Policy `json:"policies"`
			Cursor   string       `json:"cursor"`
		}
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), &resp)
		require.NoError(t, err)

		found := false
		for _, p := range resp.Policies {
			if p.Metadata.Name == policyName {
				found = true
				break
			}
		}
		assert.True(t, found, "Created policy should be in list")
	})

	// Update policy
	t.Run("Update", func(t *testing.T) {
		updateReq := map[string]interface{}{
			"apiVersion": "abac.quicbaas.io/v1",
			"kind":       "Policy",
			"metadata": map[string]interface{}{
				"name":        policyName,
				"tenant":      tenantID,
				"description": "Updated policy description",
				"priority":    200,
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
						"resources": []string{"keys/users/**"},
					},
					{
						"name":      "allow-write",
						"effect":    "allow",
						"actions":   []string{"write"},
						"resources": []string{"keys/users/${subject.id}/**"},
					},
				},
			},
		}

		var updated iam.Policy
		err := cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, policyName), updateReq, &updated)
		require.NoError(t, err)

		assert.Equal(t, "Updated policy description", updated.Metadata.Description)
		assert.Len(t, updated.Spec.Rules, 2)
		assert.Equal(t, 200, updated.Metadata.Priority)
	})

	// Delete policy
	t.Run("Delete", func(t *testing.T) {
		err := cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, policyName))
		require.NoError(t, err)

		// Verify deletion - use expectNotFound which handles eventual consistency
		err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, policyName))
		assert.NoError(t, err, "Deleted policy should not be found")
	})
}

// TestPolicyValidation tests policy validation rules.
func TestPolicyValidation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	tests := []struct {
		name        string
		policy      map[string]interface{}
		expectError bool
	}{
		{
			name: "Valid policy",
			policy: map[string]interface{}{
				"apiVersion": "abac.quicbaas.io/v1",
				"kind":       "Policy",
				"metadata": map[string]interface{}{
					"name":   "valid-policy",
					"tenant": tenantID,
				},
				"spec": map[string]interface{}{
					"subjects": map[string]interface{}{
						"authenticated": true,
					},
					"rules": []map[string]interface{}{
						{
							"name":      "test-rule",
							"effect":    "allow",
							"actions":   []string{"read"},
							"resources": []string{"keys/**"},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "Missing apiVersion",
			policy: map[string]interface{}{
				"kind": "Policy",
				"metadata": map[string]interface{}{
					"name":   "invalid-policy-1",
					"tenant": tenantID,
				},
				"spec": map[string]interface{}{
					"rules": []map[string]interface{}{
						{
							"name":      "test-rule",
							"effect":    "allow",
							"actions":   []string{"read"},
							"resources": []string{"keys/**"},
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "Wrong kind",
			policy: map[string]interface{}{
				"apiVersion": "abac.quicbaas.io/v1",
				"kind":       "WrongKind",
				"metadata": map[string]interface{}{
					"name":   "invalid-policy-2",
					"tenant": tenantID,
				},
				"spec": map[string]interface{}{
					"rules": []map[string]interface{}{
						{
							"name":      "test-rule",
							"effect":    "allow",
							"actions":   []string{"read"},
							"resources": []string{"keys/**"},
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "No rules",
			policy: map[string]interface{}{
				"apiVersion": "abac.quicbaas.io/v1",
				"kind":       "Policy",
				"metadata": map[string]interface{}{
					"name":   "invalid-policy-3",
					"tenant": tenantID,
				},
				"spec": map[string]interface{}{
					"rules": []map[string]interface{}{},
				},
			},
			expectError: true,
		},
		{
			name: "Invalid effect",
			policy: map[string]interface{}{
				"apiVersion": "abac.quicbaas.io/v1",
				"kind":       "Policy",
				"metadata": map[string]interface{}{
					"name":   "invalid-policy-4",
					"tenant": tenantID,
				},
				"spec": map[string]interface{}{
					"rules": []map[string]interface{}{
						{
							"name":      "test-rule",
							"effect":    "invalid",
							"actions":   []string{"read"},
							"resources": []string{"keys/**"},
						},
					},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var policy iam.Policy
			err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), tt.policy, &policy)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Clean up valid policy
				if err == nil {
					cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/policies/%s", tenantID, policy.Metadata.Name))
				}
			}
		})
	}
}

// TestPolicyEvaluation tests basic policy evaluation.
func TestPolicyEvaluation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Create a policy
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "eval-test-policy",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "allow-read-own-data",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"keys/users/${subject.id}/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	// Test policy evaluation
	checkReq := map[string]interface{}{
		"subject": map[string]interface{}{
			"id":            "user123",
			"tenant_id":     tenantID,
			"authenticated": true,
		},
		"action":   "read",
		"resource": "keys/users/user123/profile",
	}

	var response iam.CheckResponse
	err := cluster.postJSON("/api/v1/iam/check", checkReq, &response)
	require.NoError(t, err)

	assert.True(t, response.Allowed, "Access should be allowed for own data")
	assert.NotEmpty(t, response.MatchedPolicy)

	// Test denial (different user)
	checkReq["resource"] = "keys/users/user456/profile"
	err = cluster.postJSON("/api/v1/iam/check", checkReq, &response)
	require.NoError(t, err)

	assert.False(t, response.Allowed, "Access should be denied for other user's data")
}

// TestPolicyPriority tests policy priority evaluation.
func TestPolicyPriority(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Create high-priority deny policy
	denyPolicy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":     "deny-all",
			"tenant":   tenantID,
			"priority": 200,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"any": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "deny-everything",
					"effect":    "deny",
					"actions":   []string{"*"},
					"resources": []string{"sensitive/**"},
				},
			},
		},
	}

	var deny iam.Policy
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), denyPolicy, &deny))

	// Create lower-priority allow policy
	allowPolicy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":     "allow-read",
			"tenant":   tenantID,
			"priority": 100,
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
					"resources": []string{"sensitive/**"},
				},
			},
		},
	}

	var allow iam.Policy
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), allowPolicy, &allow))

	// Test that deny takes precedence
	checkReq := map[string]interface{}{
		"subject": map[string]interface{}{
			"id":            "user123",
			"tenant_id":     tenantID,
			"authenticated": true,
		},
		"action":   "read",
		"resource": "sensitive/data",
	}

	var response iam.CheckResponse
	err := cluster.postJSON("/api/v1/iam/check", checkReq, &response)
	require.NoError(t, err)

	assert.False(t, response.Allowed, "Deny should take precedence over allow")
}

// TestPolicyConditions tests various policy conditions.
func TestPolicyConditions(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	t.Run("AttributeCondition", func(t *testing.T) {
		policy := map[string]interface{}{
			"apiVersion": "abac.quicbaas.io/v1",
			"kind":       "Policy",
			"metadata": map[string]interface{}{
				"name":   "department-access",
				"tenant": tenantID,
			},
			"spec": map[string]interface{}{
				"subjects": map[string]interface{}{
					"authenticated": true,
				},
				"rules": []map[string]interface{}{
					{
						"name":      "engineering-only",
						"effect":    "allow",
						"actions":   []string{"read", "write"},
						"resources": []string{"engineering/**"},
						"when": []map[string]interface{}{
							{
								"expression": "subject.attributes.department == 'engineering'",
							},
						},
					},
				},
			},
		}

		var created iam.Policy
		require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

		// Test with matching attribute
		checkReq := map[string]interface{}{
			"subject": map[string]interface{}{
				"id":            "user123",
				"tenant_id":     tenantID,
				"authenticated": true,
				"attributes": map[string]interface{}{
					"department": "engineering",
				},
			},
			"action":   "read",
			"resource": "engineering/docs/design.md",
		}

		var response iam.CheckResponse
		err := cluster.postJSON("/api/v1/iam/check", checkReq, &response)
		require.NoError(t, err)
		assert.True(t, response.Allowed, "Access should be allowed with matching department")

		// Test with non-matching attribute
		checkReq["subject"].(map[string]interface{})["attributes"] = map[string]interface{}{
			"department": "sales",
		}
		err = cluster.postJSON("/api/v1/iam/check", checkReq, &response)
		require.NoError(t, err)
		assert.False(t, response.Allowed, "Access should be denied with non-matching department")
	})
}

// TestPolicyResourcePatterns tests resource pattern matching with wildcards.
func TestPolicyResourcePatterns(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "pattern-test",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "single-wildcard",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"docs/*/public"},
				},
				{
					"name":      "double-wildcard",
					"effect":    "allow",
					"actions":   []string{"read"},
					"resources": []string{"public/**"},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	tests := []struct {
		resource string
		allowed  bool
	}{
		{"docs/team1/public", true},           // Matches single wildcard
		{"docs/team2/public", true},           // Matches single wildcard
		{"docs/team1/private", false},         // No match
		{"docs/team1/public/file.txt", false}, // Single wildcard doesn't match subdirectories
		{"public/file.txt", true},             // Matches double wildcard
		{"public/sub/dir/file.txt", true},     // Matches double wildcard
	}

	for _, tt := range tests {
		t.Run(tt.resource, func(t *testing.T) {
			checkReq := map[string]interface{}{
				"subject": map[string]interface{}{
					"id":            "user123",
					"tenant_id":     tenantID,
					"authenticated": true,
				},
				"action":   "read",
				"resource": tt.resource,
			}

			var response iam.CheckResponse
			err := cluster.postJSON("/api/v1/iam/check", checkReq, &response)
			require.NoError(t, err)
			assert.Equal(t, tt.allowed, response.Allowed, "Resource: %s", tt.resource)
		})
	}
}

// TestPolicyTimeConditions tests time-based policy conditions.
func TestPolicyTimeConditions(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "business-hours-only",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "office-hours",
					"effect":    "allow",
					"actions":   []string{"write"},
					"resources": []string{"data/**"},
					"when": []map[string]interface{}{
						{
							"expression": "context.time.hour >= 9 && context.time.hour < 17",
						},
					},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	// Note: Actual time-based evaluation would require passing current time in context
	// This is a structural test to ensure the policy is created correctly
	assert.Equal(t, "business-hours-only", created.Metadata.Name)
	assert.Len(t, created.Spec.Rules, 1)
	assert.NotEmpty(t, created.Spec.Rules[0].When)
}

// TestPolicyNetworkConditions tests network CIDR-based conditions.
func TestPolicyNetworkConditions(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// First, update tenant with named networks
	tenantUpdate := map[string]interface{}{
		"settings": map[string]interface{}{
			"named_networks": map[string][]string{
				"office": {"192.0.2.0/24", "10.0.0.0/8"},
			},
		},
	}
	var tenant iam.Tenant
	require.NoError(t, cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), tenantUpdate, &tenant))

	// Create policy with network condition
	policy := map[string]interface{}{
		"apiVersion": "abac.quicbaas.io/v1",
		"kind":       "Policy",
		"metadata": map[string]interface{}{
			"name":   "office-network-only",
			"tenant": tenantID,
		},
		"spec": map[string]interface{}{
			"subjects": map[string]interface{}{
				"authenticated": true,
			},
			"rules": []map[string]interface{}{
				{
					"name":      "internal-access",
					"effect":    "allow",
					"actions":   []string{"admin"},
					"resources": []string{"admin/**"},
					"when": []map[string]interface{}{
						{
							"expression": "context.ip in tenant.named_networks.office",
						},
					},
				},
			},
		},
	}

	var created iam.Policy
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/policies", tenantID), policy, &created))

	assert.Equal(t, "office-network-only", created.Metadata.Name)
	assert.Len(t, created.Spec.Rules, 1)
}
