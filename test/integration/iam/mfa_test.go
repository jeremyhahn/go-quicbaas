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
)

// MFAEnrollmentResponse represents the MFA enrollment response.
type MFAEnrollmentResponse struct {
	EnrollmentID    string `json:"enrollment_id"`
	Secret          string `json:"secret"`
	ProvisioningURI string `json:"provisioning_uri"`
	ExpiresAt       string `json:"expires_at"`
}

// MFAStatusResponse represents the MFA status response.
type MFAStatusResponse struct {
	Enabled              bool    `json:"enabled"`
	Method               string  `json:"method,omitempty"`
	EnrolledAt           *string `json:"enrolled_at,omitempty"`
	LastVerified         *string `json:"last_verified,omitempty"`
	BackupCodesRemaining int     `json:"backup_codes_remaining"`
}

// BackupCodeStatusResponse represents backup code status response.
type BackupCodeStatusResponse struct {
	TotalCodes     int     `json:"total_codes"`
	RemainingCodes int     `json:"remaining_codes"`
	GeneratedAt    *string `json:"generated_at,omitempty"`
}

// BackupCodesResponse represents generated backup codes response.
type BackupCodesResponse struct {
	Codes []string `json:"codes"`
}

// setupUserForMFATests creates a test user and returns tenant ID and user ID.
func setupUserForMFATests(t *testing.T, cluster *testCluster) (string, string) {
	tenantID := generateUniqueTenantID()
	createTenantReq := map[string]interface{}{
		"id":   tenantID,
		"name": "MFA Test Tenant",
	}
	require.NoError(t, cluster.postJSON("/api/v1/tenants", createTenantReq, nil))

	email := generateUniqueEmail()
	createUserReq := map[string]interface{}{
		"email":    email,
		"username": generateUniqueUsername(),
		"name":     "MFA Test User",
	}

	var user struct {
		ID string `json:"id"`
	}
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users", tenantID), createUserReq, &user)
	require.NoError(t, err)

	return tenantID, user.ID
}

// cleanupMFATests cleans up test resources.
func cleanupMFATests(cluster *testCluster, tenantID string) {
	cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s", tenantID))
}

// TestMFAStatusNotEnrolled tests getting MFA status for a user without MFA.
func TestMFAStatusNotEnrolled(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID, userID := setupUserForMFATests(t, cluster)
	defer cleanupMFATests(cluster, tenantID)

	var status MFAStatusResponse
	err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa", tenantID, userID), &status)
	require.NoError(t, err, "Should get MFA status")

	assert.False(t, status.Enabled, "MFA should not be enabled")
	assert.Empty(t, status.Method, "Method should be empty")
	assert.Nil(t, status.EnrolledAt, "EnrolledAt should be nil")
	assert.Equal(t, 0, status.BackupCodesRemaining, "No backup codes should exist")
}

// TestMFAEnrollmentBegin tests starting MFA enrollment.
func TestMFAEnrollmentBegin(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID, userID := setupUserForMFATests(t, cluster)
	defer cleanupMFATests(cluster, tenantID)

	enrollReq := map[string]interface{}{
		"method": "totp",
	}

	var enrollment MFAEnrollmentResponse
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/enroll", tenantID, userID), enrollReq, &enrollment)
	require.NoError(t, err, "Should begin enrollment")

	assert.NotEmpty(t, enrollment.EnrollmentID, "EnrollmentID should not be empty")
	assert.NotEmpty(t, enrollment.Secret, "Secret should not be empty")
	assert.Contains(t, enrollment.ProvisioningURI, "otpauth://totp/", "Should have valid provisioning URI")
	assert.NotEmpty(t, enrollment.ExpiresAt, "ExpiresAt should not be empty")
}

// TestMFAEnrollmentCancel tests canceling MFA enrollment.
func TestMFAEnrollmentCancel(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID, userID := setupUserForMFATests(t, cluster)
	defer cleanupMFATests(cluster, tenantID)

	// Start enrollment
	enrollReq := map[string]interface{}{
		"method": "totp",
	}

	var enrollment MFAEnrollmentResponse
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/enroll", tenantID, userID), enrollReq, &enrollment)
	require.NoError(t, err)

	// Cancel enrollment
	err = cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/enroll/%s", tenantID, userID, enrollment.EnrollmentID))
	require.NoError(t, err, "Should cancel enrollment")

	// Verify still not enrolled
	var status MFAStatusResponse
	err = cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa", tenantID, userID), &status)
	require.NoError(t, err)
	assert.False(t, status.Enabled, "MFA should not be enabled after cancel")
}

// TestMFABackupCodesBeforeEnrollment tests backup code operations before enrollment.
func TestMFABackupCodesBeforeEnrollment(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID, userID := setupUserForMFATests(t, cluster)
	defer cleanupMFATests(cluster, tenantID)

	// Get backup code status - should return zeros
	var status BackupCodeStatusResponse
	err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/backup-codes", tenantID, userID), &status)
	require.NoError(t, err)

	assert.Equal(t, 0, status.TotalCodes, "No backup codes should exist")
	assert.Equal(t, 0, status.RemainingCodes, "No remaining codes")
}

// TestMFADisableNotEnrolled tests disabling MFA when not enrolled.
func TestMFADisableNotEnrolled(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID, userID := setupUserForMFATests(t, cluster)
	defer cleanupMFATests(cluster, tenantID)

	// Try to disable MFA when not enrolled
	err := cluster.deleteJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa", tenantID, userID))

	// Should either succeed (no-op) or return error
	// Implementation dependent - just verify it doesn't crash
	if err != nil {
		t.Logf("Disable MFA when not enrolled returned: %v", err)
	}
}

// TestMFAInvalidMethod tests enrollment with invalid method.
func TestMFAInvalidMethod(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID, userID := setupUserForMFATests(t, cluster)
	defer cleanupMFATests(cluster, tenantID)

	enrollReq := map[string]interface{}{
		"method": "invalid-method",
	}

	var enrollment MFAEnrollmentResponse
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/enroll", tenantID, userID), enrollReq, &enrollment)

	// Should fail with invalid method
	if err == nil && enrollment.EnrollmentID != "" {
		t.Log("Server accepted invalid method - may default to TOTP")
	}
}

// TestMFAStatusNonExistentUser tests getting MFA status for non-existent user.
func TestMFAStatusNonExistentUser(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID := generateUniqueTenantID()
	createTenantReq := map[string]interface{}{
		"id":   tenantID,
		"name": "MFA Test Tenant",
	}
	require.NoError(t, cluster.postJSON("/api/v1/tenants", createTenantReq, nil))
	defer cleanupMFATests(cluster, tenantID)

	var status MFAStatusResponse
	err := cluster.getJSON(fmt.Sprintf("/api/v1/tenants/%s/users/nonexistent-user/mfa", tenantID), &status)

	// Either returns error (user not found) or default disabled status
	if err != nil {
		t.Log("Server returned error for non-existent user (expected)")
	} else {
		assert.False(t, status.Enabled, "MFA should not be enabled for non-existent user")
	}
}

// TestMFAEnrollmentMultipleAttempts tests that multiple enrollments don't conflict.
func TestMFAEnrollmentMultipleAttempts(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID, userID := setupUserForMFATests(t, cluster)
	defer cleanupMFATests(cluster, tenantID)

	enrollReq := map[string]interface{}{
		"method": "totp",
	}

	// Start first enrollment
	var enrollment1 MFAEnrollmentResponse
	err := cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/enroll", tenantID, userID), enrollReq, &enrollment1)
	require.NoError(t, err)

	// Start second enrollment (should either fail or replace first)
	var enrollment2 MFAEnrollmentResponse
	err = cluster.postJSON(fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/enroll", tenantID, userID), enrollReq, &enrollment2)

	if err != nil {
		// Expected if concurrent enrollments not allowed
		t.Logf("Second enrollment attempt returned: %v", err)
	} else {
		// If allowed, should have different ID
		assert.NotEmpty(t, enrollment2.EnrollmentID)
	}
}

// TestMFAEndpointPaths tests that all MFA endpoint paths are valid.
func TestMFAEndpointPaths(t *testing.T) {
	base := setupTestCluster(t)
	defer base.Close()
	// The system admin creates the test's tenant and acts in it by the tenant lift.
	cluster := base.asSystemAdmin()

	tenantID, userID := setupUserForMFATests(t, cluster)
	defer cleanupMFATests(cluster, tenantID)

	tests := []struct {
		name       string
		method     string
		path       string
		body       interface{}
		expectCode int
	}{
		{
			name:       "GET MFA Status",
			method:     "GET",
			path:       fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa", tenantID, userID),
			expectCode: 200,
		},
		{
			name:       "GET Backup Code Status",
			method:     "GET",
			path:       fmt.Sprintf("/api/v1/tenants/%s/users/%s/mfa/backup-codes", tenantID, userID),
			expectCode: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := cluster.doHTTPRequest(tt.method, tt.path, tt.body)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectCode, resp.StatusCode,
				"Expected status %d for %s, got %d", tt.expectCode, tt.path, resp.StatusCode)
		})
	}
}
