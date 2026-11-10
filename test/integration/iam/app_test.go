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

// TestAppRegistration tests OAuth application registration.
func TestAppRegistration(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Register application
	t.Run("Register", func(t *testing.T) {
		appReq := map[string]interface{}{
			"name": "Test Application",
			"redirect_uris": []string{
				"https://app.example.com/callback",
				"https://app.example.com/callback2",
			},
			"grant_types": []string{
				"authorization_code",
				"refresh_token",
			},
			"response_types": []string{"code"},
		}

		var app iam.Application
		err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), appReq, &app)
		require.NoError(t, err)

		assert.NotEmpty(t, app.ClientID)
		assert.NotEmpty(t, app.ClientSecret)
		assert.Equal(t, "Test Application", app.Name)
		assert.Equal(t, tenantID, app.TenantID)
		assert.Len(t, app.RedirectURIs, 2)
		assert.Contains(t, app.RedirectURIs, "https://app.example.com/callback")
		assert.NotZero(t, app.Created)
	})

	// List applications
	t.Run("List", func(t *testing.T) {
		var resp struct {
			Apps []iam.Application `json:"apps"`
		}
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), &resp)
		require.NoError(t, err)

		assert.NotEmpty(t, resp.Apps)
		found := false
		for _, app := range resp.Apps {
			if app.Name == "Test Application" {
				found = true
				break
			}
		}
		assert.True(t, found, "Registered app should be in list")
	})

	// Get application
	t.Run("Get", func(t *testing.T) {
		var resp struct {
			Apps []iam.Application `json:"apps"`
		}
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), &resp)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Apps)

		clientID := resp.Apps[0].ClientID

		var app iam.Application
		err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, clientID), &app)
		require.NoError(t, err)

		assert.Equal(t, clientID, app.ClientID)
		assert.Equal(t, "Test Application", app.Name)
	})

	// Update application
	t.Run("Update", func(t *testing.T) {
		var resp struct {
			Apps []iam.Application `json:"apps"`
		}
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), &resp)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Apps)

		clientID := resp.Apps[0].ClientID

		updateReq := map[string]interface{}{
			"name": "Updated Application Name",
			"redirect_uris": []string{
				"https://app.example.com/callback",
				"https://app.example.com/callback2",
				"https://app.example.com/callback3",
			},
		}

		var updated iam.Application
		err = cluster.putJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, clientID), updateReq, &updated)
		require.NoError(t, err)

		assert.Equal(t, "Updated Application Name", updated.Name)
		assert.Len(t, updated.RedirectURIs, 3)
	})

	// Delete application
	t.Run("Delete", func(t *testing.T) {
		var resp struct {
			Apps []iam.Application `json:"apps"`
		}
		err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), &resp)
		require.NoError(t, err)
		require.NotEmpty(t, resp.Apps)

		clientID := resp.Apps[0].ClientID

		err = cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, clientID))
		require.NoError(t, err)

		// Verify deletion - use expectNotFound which handles eventual consistency
		err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, clientID))
		assert.NoError(t, err, "Deleted app should not be found")
	})
}

// TestAppSecretRotation tests rotating application client secrets.
func TestAppSecretRotation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Register application
	appReq := map[string]interface{}{
		"name":          "Secret Rotation Test App",
		"redirect_uris": []string{"https://app.example.com/callback"},
		"grant_types":   []string{"client_credentials"},
	}

	var app iam.Application
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), appReq, &app))
	originalSecret := app.ClientSecret

	// Rotate secret
	var rotated iam.Application
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s/rotate-secret", tenantID, app.ClientID), nil, &rotated)
	require.NoError(t, err)

	// New secret should be different
	assert.NotEqual(t, originalSecret, rotated.ClientSecret)
	assert.NotEmpty(t, rotated.ClientSecret)
}

// TestAppClientValidation tests client validation during OAuth flows.
func TestAppClientValidation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Register valid application
	appReq := map[string]interface{}{
		"name":          "Validation Test App",
		"redirect_uris": []string{"https://app.example.com/callback"},
		"grant_types":   []string{"client_credentials"},
	}

	var app iam.Application
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), appReq, &app))

	// Test validation scenarios
	tests := []struct {
		name         string
		clientID     string
		clientSecret string
		expectError  bool
	}{
		{
			name:         "Valid credentials",
			clientID:     app.ClientID,
			clientSecret: app.ClientSecret,
			expectError:  false,
		},
		{
			name:         "Invalid client ID",
			clientID:     "invalid-client-id",
			clientSecret: app.ClientSecret,
			expectError:  true,
		},
		{
			name:         "Invalid client secret",
			clientID:     app.ClientID,
			clientSecret: "invalid-secret",
			expectError:  true,
		},
		{
			name:         "Empty client ID",
			clientID:     "",
			clientSecret: app.ClientSecret,
			expectError:  true,
		},
		{
			name:         "Empty client secret",
			clientID:     app.ClientID,
			clientSecret: "",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validateReq := map[string]interface{}{
				"client_id":     tt.clientID,
				"client_secret": tt.clientSecret,
			}

			var result map[string]interface{}
			err := cluster.postJSON("/api/v1/iam/validate-client", validateReq, &result)

			if tt.expectError {
				assert.Error(t, err, "Client validation should fail")
			} else {
				assert.NoError(t, err, "Client validation should succeed")
				if err == nil {
					assert.True(t, result["valid"].(bool))
				}
			}
		})
	}
}

// TestAppRedirectURIValidation tests redirect URI validation.
func TestAppRedirectURIValidation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	tests := []struct {
		name         string
		redirectURIs []string
		expectError  bool
	}{
		{
			name:         "Valid HTTPS URIs",
			redirectURIs: []string{"https://app.example.com/callback"},
			expectError:  false,
		},
		{
			name: "Multiple valid URIs",
			redirectURIs: []string{
				"https://app.example.com/callback",
				"https://app.example.com/callback2",
			},
			expectError: false,
		},
		{
			name:         "Localhost allowed for development",
			redirectURIs: []string{"http://localhost:8080/callback"},
			expectError:  false,
		},
		{
			name:         "Invalid HTTP URI (non-localhost)",
			redirectURIs: []string{"http://app.example.com/callback"},
			expectError:  true,
		},
		{
			name:         "Empty redirect URI",
			redirectURIs: []string{},
			expectError:  true,
		},
		{
			name:         "Invalid URI format",
			redirectURIs: []string{"not-a-valid-uri"},
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appReq := map[string]interface{}{
				"name":          "URI Validation Test",
				"redirect_uris": tt.redirectURIs,
				"grant_types":   []string{"authorization_code"},
			}

			var app iam.Application
			err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), appReq, &app)

			if tt.expectError {
				assert.Error(t, err, "Invalid redirect URI should be rejected")
			} else {
				assert.NoError(t, err, "Valid redirect URI should be accepted")
				if err == nil {
					// Clean up
					cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, app.ClientID))
				}
			}
		})
	}
}

// TestAppGrantTypes tests different OAuth grant type configurations.
func TestAppGrantTypes(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	tests := []struct {
		name            string
		grantTypes      []string
		valid           bool
		expectsDefaults bool // If true, expect default grant types to be set
	}{
		{
			name:       "Authorization code",
			grantTypes: []string{"authorization_code"},
			valid:      true,
		},
		{
			name:       "Client credentials",
			grantTypes: []string{"client_credentials"},
			valid:      true,
		},
		{
			name:       "Refresh token",
			grantTypes: []string{"refresh_token"},
			valid:      true,
		},
		{
			name:       "Multiple grant types",
			grantTypes: []string{"authorization_code", "refresh_token"},
			valid:      true,
		},
		{
			name:       "Invalid grant type",
			grantTypes: []string{"invalid_grant"},
			valid:      false,
		},
		{
			name:            "Empty grant types uses defaults",
			grantTypes:      []string{},
			valid:           true,
			expectsDefaults: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appReq := map[string]interface{}{
				"name":          "Grant Type Test App",
				"redirect_uris": []string{"https://app.example.com/callback"},
				"grant_types":   tt.grantTypes,
			}

			var app iam.Application
			err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), appReq, &app)

			if tt.valid {
				assert.NoError(t, err)
				if err == nil {
					if tt.expectsDefaults {
						// When empty grant types are provided, defaults should be applied
						assert.NotEmpty(t, app.GrantTypes, "Empty grant types should get defaults")
						assert.Contains(t, app.GrantTypes, "authorization_code", "Default should include authorization_code")
						assert.Contains(t, app.GrantTypes, "refresh_token", "Default should include refresh_token")
					} else {
						assert.Equal(t, tt.grantTypes, app.GrantTypes)
					}
					cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, app.ClientID))
				}
			} else {
				assert.Error(t, err)
			}
		})
	}
}

// TestAppResourcePrefix tests application resource prefix for scoping.
func TestAppResourcePrefix(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Create app with resource prefix
	appReq := map[string]interface{}{
		"name":            "Scoped App",
		"redirect_uris":   []string{"https://app.example.com/callback"},
		"grant_types":     []string{"client_credentials"},
		"resource_prefix": "app/data",
	}

	var app iam.Application
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), appReq, &app))

	assert.Equal(t, "app/data", app.ResourcePrefix)
}

// TestAppDeletion tests application deletion and cleanup.
func TestAppDeletion(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenantID := setupTenantForTests(t, cluster)
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))

	// Create application
	appReq := map[string]interface{}{
		"name":          "App to Delete",
		"redirect_uris": []string{"https://app.example.com/callback"},
		"grant_types":   []string{"client_credentials"},
	}

	var app iam.Application
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenantID), appReq, &app))

	clientID := app.ClientID

	// Delete application
	err := cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, clientID))
	require.NoError(t, err)

	// Verify cannot get deleted app - use expectNotFound which handles eventual consistency
	err = cluster.getJSONExpectNotFound(fmt.Sprintf("/api/v1/tenants/%s/apps/%s", tenantID, clientID))
	assert.NoError(t, err, "Deleted app should not be retrievable")
}

// TestAppTenantIsolation tests that apps from different tenants are isolated.
func TestAppTenantIsolation(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	tenant1ID := generateUniqueTenantID()
	tenant2ID := generateUniqueTenantID()

	// Create two tenants
	cluster.provisionTenant(t, tenant1ID)
	cluster.provisionTenant(t, tenant2ID)

	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenant1ID))
	defer cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenant2ID))

	// Create app in tenant 1
	appReq := map[string]interface{}{
		"name":          "Tenant 1 App",
		"redirect_uris": []string{"https://app1.example.com/callback"},
		"grant_types":   []string{"client_credentials"},
	}

	var app1 iam.Application
	require.NoError(t, cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenant1ID), appReq, &app1))

	// Verify app is NOT in tenant 2
	var tenant2Resp struct {
		Apps []iam.Application `json:"apps"`
	}
	err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenant2ID), &tenant2Resp)
	require.NoError(t, err)

	for _, app := range tenant2Resp.Apps {
		assert.NotEqual(t, app1.ClientID, app.ClientID, "Tenant 1 app should not appear in tenant 2")
	}

	// Verify app IS in tenant 1
	var tenant1Resp struct {
		Apps []iam.Application `json:"apps"`
	}
	err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/apps", tenant1ID), &tenant1Resp)
	require.NoError(t, err)

	found := false
	for _, app := range tenant1Resp.Apps {
		if app.ClientID == app1.ClientID {
			found = true
			break
		}
	}
	assert.True(t, found, "App should be visible in its own tenant")
}
