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
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestTenantCRUD tests complete tenant lifecycle: create, read, update, delete.
func TestTenantCRUD(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()

	// Create tenant
	t.Run("Create", func(t *testing.T) {
		createReq := map[string]interface{}{
			"id":   tenantID,
			"name": "Integration Test Tenant",
			"settings": map[string]interface{}{
				"max_users":    100,
				"mfa_required": false,
			},
		}

		var tenant iam.Tenant
		err := cluster.postJSON("/api/v1/tenants", createReq, &tenant)
		require.NoError(t, err, "Failed to create tenant")

		assert.Equal(t, tenantID, tenant.ID)
		assert.Equal(t, "Integration Test Tenant", tenant.Name)
		assert.NotZero(t, tenant.Created)
	})

	// Get tenant
	t.Run("Get", func(t *testing.T) {
		var tenant iam.Tenant
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), &tenant)
		require.NoError(t, err, "Failed to get tenant")

		assert.Equal(t, tenantID, tenant.ID)
		assert.Equal(t, "Integration Test Tenant", tenant.Name)
	})

	// List tenants - with retry for eventual consistency
	t.Run("List", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		var found bool
		for !found {
			var resp struct {
				Tenants []iam.Tenant `json:"tenants"`
				Cursor  string       `json:"cursor"`
				Limit   int          `json:"limit"`
			}
			err := cluster.getJSON("/api/v1/tenants", &resp)
			require.NoError(t, err, "Failed to list tenants")

			for _, tenant := range resp.Tenants {
				if tenant.ID == tenantID {
					found = true
					break
				}
			}
			if found {
				break
			}

			select {
			case <-ctx.Done():
				t.Fatal("Timed out waiting for tenant to appear in list")
			case <-ticker.C:
			}
		}
		assert.True(t, found, "Created tenant should be in list")
	})

	// Update tenant
	t.Run("Update", func(t *testing.T) {
		updateReq := map[string]interface{}{
			"name": "Updated Test Tenant",
			"settings": map[string]interface{}{
				"max_users":    200,
				"mfa_required": true,
			},
		}

		var tenant iam.Tenant
		err := cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), updateReq, &tenant)
		require.NoError(t, err, "Failed to update tenant")

		assert.Equal(t, "Updated Test Tenant", tenant.Name)
		assert.True(t, tenant.Settings.MFARequired)
	})

	// Delete tenant
	t.Run("Delete", func(t *testing.T) {
		err := cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
		require.NoError(t, err, "Failed to delete tenant")

		// Verify deletion - use expectNotFound which handles eventual consistency
		err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
		assert.NoError(t, err, "Deleted tenant should not be found")
	})
}

// TestTenantIsolation verifies that tenants are completely isolated.
func TestTenantIsolation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	// Create two separate tenants
	tenantA := generateUniqueTenantID()
	tenantB := generateUniqueTenantID()

	createReqA := map[string]interface{}{
		"id":   tenantA,
		"name": "Isolation Tenant A",
	}
	createReqB := map[string]interface{}{
		"id":   tenantB,
		"name": "Isolation Tenant B",
	}

	require.NoError(t, cluster.postJSON("/api/v1/tenants", createReqA, nil))
	require.NoError(t, cluster.postJSON("/api/v1/tenants", createReqB, nil))

	defer func() {
		cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantA))
		cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantB))
	}()

	// Create a user in tenant A
	userReq := map[string]interface{}{
		"email":    generateUniqueEmail(),
		"username": generateUniqueUsername(),
		"name":     "Isolation Test User",
	}

	var userA struct {
		ID string `json:"id"`
	}
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantA), userReq, &userA)
	require.NoError(t, err, "Failed to create user in tenant A")

	// Verify user is not visible in tenant B
	var usersB struct {
		Users []map[string]interface{} `json:"users"`
	}
	err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantB), &usersB)
	require.NoError(t, err, "Failed to list users in tenant B")

	for _, user := range usersB.Users {
		if id, ok := user["id"].(string); ok {
			assert.NotEqual(t, userA.ID, id, "User from tenant A should not be visible in tenant B")
		}
	}
}

// TestTenantQuotas tests tenant quota enforcement and persistence.
func TestTenantQuotas(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()

	// Create a tenant with max_users quota
	createReq := map[string]interface{}{
		"id":   tenantID,
		"name": "Quota Test Tenant",
		"settings": map[string]interface{}{
			"max_users": 5,
		},
	}

	var tenant iam.Tenant
	err := cluster.postJSON("/api/v1/tenants", createReq, &tenant)
	require.NoError(t, err, "Failed to create tenant with quotas")

	defer func() {
		cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	}()

	assert.Equal(t, tenantID, tenant.ID)
	assert.Equal(t, "Quota Test Tenant", tenant.Name)
	assert.Equal(t, 5, tenant.Settings.MaxUsers)

	// Verify quota is persisted by fetching the tenant
	var fetched iam.Tenant
	err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), &fetched)
	require.NoError(t, err, "Failed to get tenant")

	assert.Equal(t, 5, fetched.Settings.MaxUsers, "MaxUsers quota should be persisted")

	// Update the quota
	updateReq := map[string]interface{}{
		"settings": map[string]interface{}{
			"max_users": 10,
		},
	}

	var updated iam.Tenant
	err = cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), updateReq, &updated)
	require.NoError(t, err, "Failed to update tenant quotas")

	assert.Equal(t, 10, updated.Settings.MaxUsers, "MaxUsers quota should be updated")

	// Verify updated quota is persisted
	var refetched iam.Tenant
	err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), &refetched)
	require.NoError(t, err, "Failed to get tenant after quota update")

	assert.Equal(t, 10, refetched.Settings.MaxUsers, "Updated MaxUsers quota should be persisted")
}

// TestTenantSettings tests tenant settings configuration and persistence.
func TestTenantSettings(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	t.Run("MFARequired", func(t *testing.T) {
		tenantID := generateUniqueTenantID()

		// Create tenant with MFA disabled
		createReq := map[string]interface{}{
			"id":   tenantID,
			"name": "MFA Test Tenant",
			"settings": map[string]interface{}{
				"mfa_required": false,
			},
		}

		var tenant iam.Tenant
		err := cluster.postJSON("/api/v1/tenants", createReq, &tenant)
		require.NoError(t, err, "Failed to create tenant")

		defer func() {
			cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
		}()

		assert.False(t, tenant.Settings.MFARequired, "MFA should initially be disabled")

		// Enable MFA
		updateReq := map[string]interface{}{
			"settings": map[string]interface{}{
				"mfa_required": true,
			},
		}

		var updated iam.Tenant
		err = cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), updateReq, &updated)
		require.NoError(t, err, "Failed to update tenant MFA setting")

		assert.True(t, updated.Settings.MFARequired, "MFA should be enabled after update")

		// Verify persistence
		var fetched iam.Tenant
		err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), &fetched)
		require.NoError(t, err, "Failed to get tenant after MFA update")

		assert.True(t, fetched.Settings.MFARequired, "MFA setting should be persisted")

		// Disable MFA again
		disableReq := map[string]interface{}{
			"settings": map[string]interface{}{
				"mfa_required": false,
			},
		}

		var disabled iam.Tenant
		err = cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), disableReq, &disabled)
		require.NoError(t, err, "Failed to disable MFA")

		assert.False(t, disabled.Settings.MFARequired, "MFA should be disabled after toggle")
	})

	t.Run("NamedNetworks", func(t *testing.T) {
		tenantID := generateUniqueTenantID()

		// Create tenant with named networks
		createReq := map[string]interface{}{
			"id":   tenantID,
			"name": "Network Test Tenant",
			"settings": map[string]interface{}{
				"named_networks": map[string]interface{}{
					"office":     []string{"10.0.0.0/8", "172.16.0.0/12"},
					"vpn":        []string{"198.51.100.0/24"},
					"datacenter": []string{"10.10.0.0/16"},
				},
			},
		}

		var tenant iam.Tenant
		err := cluster.postJSON("/api/v1/tenants", createReq, &tenant)
		require.NoError(t, err, "Failed to create tenant with named networks")

		defer func() {
			cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
		}()

		require.NotNil(t, tenant.Settings.NamedNetworks, "Named networks should be set")
		assert.Contains(t, tenant.Settings.NamedNetworks, "office", "Should have office network")
		assert.Contains(t, tenant.Settings.NamedNetworks, "vpn", "Should have vpn network")
		assert.Contains(t, tenant.Settings.NamedNetworks, "datacenter", "Should have datacenter network")
		assert.Equal(t, []string{"10.0.0.0/8", "172.16.0.0/12"}, tenant.Settings.NamedNetworks["office"])
		assert.Equal(t, []string{"198.51.100.0/24"}, tenant.Settings.NamedNetworks["vpn"])

		// Verify persistence
		var fetched iam.Tenant
		err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), &fetched)
		require.NoError(t, err, "Failed to get tenant after named networks creation")

		require.NotNil(t, fetched.Settings.NamedNetworks, "Named networks should be persisted")
		assert.Len(t, fetched.Settings.NamedNetworks, 3, "Should have 3 named networks")

		// Update named networks
		updateReq := map[string]interface{}{
			"settings": map[string]interface{}{
				"named_networks": map[string]interface{}{
					"office": []string{"10.0.0.0/8"},
					"remote": []string{"203.0.113.0/24"},
				},
			},
		}

		var updated iam.Tenant
		err = cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), updateReq, &updated)
		require.NoError(t, err, "Failed to update named networks")

		require.NotNil(t, updated.Settings.NamedNetworks, "Updated named networks should be set")
		assert.Contains(t, updated.Settings.NamedNetworks, "office", "Should still have office network")
		assert.Contains(t, updated.Settings.NamedNetworks, "remote", "Should have new remote network")
	})

	t.Run("SessionTimeouts", func(t *testing.T) {
		tenantID := generateUniqueTenantID()

		// Create tenant with session timeout settings
		createReq := map[string]interface{}{
			"id":   tenantID,
			"name": "Session Timeout Test Tenant",
			"settings": map[string]interface{}{
				"session_timeout":      int64(30 * time.Minute),
				"session_idle_timeout": int64(10 * time.Minute),
				"token_ttl":            int64(15 * time.Minute),
				"refresh_token_ttl":    int64(24 * time.Hour),
			},
		}

		var tenant iam.Tenant
		err := cluster.postJSON("/api/v1/tenants", createReq, &tenant)
		require.NoError(t, err, "Failed to create tenant with session timeouts")

		defer func() {
			cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
		}()

		assert.Equal(t, 30*time.Minute, tenant.Settings.SessionTimeout, "Session timeout should be 30 minutes")
		assert.Equal(t, 10*time.Minute, tenant.Settings.SessionIdleTimeout, "Session idle timeout should be 10 minutes")
		assert.Equal(t, 15*time.Minute, tenant.Settings.TokenTTL, "Token TTL should be 15 minutes")
		assert.Equal(t, 24*time.Hour, tenant.Settings.RefreshTokenTTL, "Refresh token TTL should be 24 hours")

		// Verify persistence
		var fetched iam.Tenant
		err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), &fetched)
		require.NoError(t, err, "Failed to get tenant after session timeout creation")

		assert.Equal(t, 30*time.Minute, fetched.Settings.SessionTimeout, "Session timeout should be persisted")
		assert.Equal(t, 10*time.Minute, fetched.Settings.SessionIdleTimeout, "Session idle timeout should be persisted")
		assert.Equal(t, 15*time.Minute, fetched.Settings.TokenTTL, "Token TTL should be persisted")
		assert.Equal(t, 24*time.Hour, fetched.Settings.RefreshTokenTTL, "Refresh token TTL should be persisted")

		// Update session timeouts
		updateReq := map[string]interface{}{
			"settings": map[string]interface{}{
				"session_timeout":      int64(60 * time.Minute),
				"session_idle_timeout": int64(20 * time.Minute),
			},
		}

		var updated iam.Tenant
		err = cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID), updateReq, &updated)
		require.NoError(t, err, "Failed to update session timeouts")

		assert.Equal(t, 60*time.Minute, updated.Settings.SessionTimeout, "Session timeout should be updated to 60 minutes")
		assert.Equal(t, 20*time.Minute, updated.Settings.SessionIdleTimeout, "Session idle timeout should be updated to 20 minutes")
	})
}

// TestTenantDeletionCascade verifies that deleting a tenant also deletes associated resources.
func TestTenantDeletionCascade(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := generateUniqueTenantID()

	// Create tenant
	createReq := map[string]interface{}{
		"id":   tenantID,
		"name": "Cascade Delete Test Tenant",
	}
	require.NoError(t, cluster.postJSON("/api/v1/tenants", createReq, nil))

	// Create users in the tenant
	userReq := map[string]interface{}{
		"email":    generateUniqueEmail(),
		"username": generateUniqueUsername(),
		"name":     "Cascade Test User",
	}
	var user struct {
		ID string `json:"id"`
	}
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), userReq, &user)
	require.NoError(t, err, "Failed to create user in tenant")
	require.NotEmpty(t, user.ID, "User ID should not be empty")

	// Create a group in the tenant
	groupReq := map[string]interface{}{
		"name":        "Cascade Test Group",
		"description": "Group for cascade deletion testing",
	}
	var group struct {
		ID string `json:"id"`
	}
	err = cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/groups", tenantID), groupReq, &group)
	require.NoError(t, err, "Failed to create group in tenant")
	require.NotEmpty(t, group.ID, "Group ID should not be empty")

	// Delete the tenant
	err = cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	require.NoError(t, err, "Failed to delete tenant")

	// Verify tenant is deleted
	err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
	assert.NoError(t, err, "Deleted tenant should not be found")

	// Verify users are deleted (tenant endpoint should return 404)
	err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s/users/%s", tenantID, user.ID))
	assert.NoError(t, err, "User should be deleted with tenant")

	// Verify groups are deleted (tenant endpoint should return 404)
	err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s/groups/%s", tenantID, group.ID))
	assert.NoError(t, err, "Group should be deleted with tenant")
}

// TestTenantValidation tests input validation for tenant creation.
func TestTenantValidation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	t.Run("Valid_tenant", func(t *testing.T) {
		tenantID := generateUniqueTenantID()

		createReq := map[string]interface{}{
			"id":   tenantID,
			"name": "Valid Tenant",
		}

		var tenant iam.Tenant
		err := cluster.postJSON("/api/v1/tenants", createReq, &tenant)
		require.NoError(t, err, "Valid tenant creation should succeed")

		defer func() {
			cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
		}()

		assert.Equal(t, tenantID, tenant.ID)
		assert.Equal(t, "Valid Tenant", tenant.Name)
		assert.NotZero(t, tenant.Created)
	})

	t.Run("Missing_ID", func(t *testing.T) {
		createReq := map[string]interface{}{
			"name": "Tenant Without ID",
		}

		resp, err := cluster.doHTTPRequest(http.MethodPost, "/api/v1/tenants", createReq)
		require.NoError(t, err, "HTTP request should succeed")
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"Creating tenant without ID should return 400 Bad Request, got %d: %s",
			resp.StatusCode, string(body))
	})

	t.Run("Missing_name", func(t *testing.T) {
		createReq := map[string]interface{}{
			"id": generateUniqueTenantID(),
		}

		resp, err := cluster.doHTTPRequest(http.MethodPost, "/api/v1/tenants", createReq)
		require.NoError(t, err, "HTTP request should succeed")
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"Creating tenant without name should return 400 Bad Request, got %d: %s",
			resp.StatusCode, string(body))
	})

	t.Run("Empty_ID", func(t *testing.T) {
		createReq := map[string]interface{}{
			"id":   "",
			"name": "Tenant With Empty ID",
		}

		resp, err := cluster.doHTTPRequest(http.MethodPost, "/api/v1/tenants", createReq)
		require.NoError(t, err, "HTTP request should succeed")
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
			"Creating tenant with empty ID should return 400 Bad Request, got %d: %s",
			resp.StatusCode, string(body))
	})
}
